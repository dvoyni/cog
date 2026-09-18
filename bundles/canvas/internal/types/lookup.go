package types

import (
	"fmt"
	"io/fs"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/storage"
)

// FontMetrics reports a font's vertical metrics at a given size, in logical
// pixels, for baseline placement and inline-icon alignment.
type FontMetrics struct {
	Ascent     float32
	Descent    float32
	LineHeight float32
	XHeight    float32
	CapHeight  float32
}

// Lookup is the single Canvas-owned resource that holds canvas's asset caches,
// the two packers behind them, and the font store. Callers acquire it as a write
// dependency and operate on it through a scoped LookupAccess or
// LookupDeviceAccess; the resource itself never retains filesystem or GPU
// handles.
//
// Three of its tables are assets.Cache instances - sprites, the standalone
// textures tiled sprites sample, and the header-only sizes every layout measures
// against. Each loads through the Library, each caches whatever its load
// produced, and none of them retries: a sprite that failed is reported once and
// stays failed until it is unloaded.
type Lookup struct {
	// spritePacker and glyphPacker are the shelf allocators, free lists,
	// tombstoned array indices and byte counters the sprite and glyph sides pack
	// into. They are persistent state outliving every handler, which is why they
	// live here and travel to the sprite loader in its user value.
	spritePacker *packer
	glyphPacker  *packer

	sprites     *assets.Cache[spriteDescrParams, spriteUser, AtlasEntry]
	tiled       *assets.Cache[tiledDescrParams, *gfx.ResourceQueue, StandaloneEntry]
	spriteSizes *assets.Cache[sizeDescrParams, struct{}, m.Vec2i]

	fontStore            *FontStore
	lastFramebufferScale float32
}

// NewLookup builds an empty Lookup resource with canvas's default atlas sizes;
// see canvas.NewLookup.
func NewLookup() *Lookup { return NewSizedLookup(WithDefaults(Config{})) }

// NewSizedLookup builds an empty Lookup resource sized by config, which is
// already complete.
//
// The three loaders are built here, once, and outlive every handler: they are
// stateless, and everything lock-bound - the kernel, the filesystem, the packer
// and the resource queue - arrives on the call that needs it.
func NewSizedLookup(config Config) *Lookup {
	return &Lookup{
		spritePacker: newPacker(config),
		glyphPacker:  newPacker(config),
		sprites:      assets.New[spriteDescrParams, spriteUser, AtlasEntry](spriteLoader{}),
		tiled:        assets.New[tiledDescrParams, *gfx.ResourceQueue, StandaloneEntry](standaloneLoader{}),
		spriteSizes:  assets.New[sizeDescrParams, struct{}, m.Vec2i](spriteSizeLoader{}),
		fontStore:    NewFontStore(),
	}
}

// spriteDescr names one sprite. A path names a file; the empty path names the
// white texel canvas generates, which is blob-named because there is no file to
// name it by and a Name the Library would try to open.
func spriteDescr(path string) assets.Descr[spriteDescrParams] {
	if path == "" {
		return assets.Descr[spriteDescrParams]{
			Blob:   assets.NewBlobFromString(whiteTexel),
			Params: spriteDescrParams{generated: true},
		}
	}
	return assets.Descr[spriteDescrParams]{Name: path}
}

// unloadFont drops a font's baked faces and parsed source so their CPU memory is
// reclaimed and a later load re-reads the file. The glyph atlas pages are not
// freed per font; the whole glyph atlas is released together on a framebuffer
// scale change (invalidateFontsOnResize), which is the only time glyph pages
// need reclaiming.
func (l *Lookup) unloadFont(path string) {
	for key, cached := range l.fontStore.fonts {
		if key.path == path {
			_ = cached.Face.Close()
			delete(l.fontStore.fonts, key)
		}
	}
	delete(l.fontStore.sources, path)
}

// invalidateFontsOnResize drops the glyph atlas and baked faces when the
// framebuffer/logical scale changes, so glyphs re-rasterize at full resolution.
func (l *Lookup) invalidateFontsOnResize(resources *gfx.ResourceQueue, view *gfx.Viewport) {
	scale := float32(1)
	if view.Width > 0 && view.FramebufferWidth > 0 {
		scale = view.FramebufferWidth / view.Width
	}
	if l.lastFramebufferScale != 0 && scale != l.lastFramebufferScale {
		l.glyphPacker.releaseAll(resources)
		ClearFontFaces(l.fontStore)
	}
	l.lastFramebufferScale = scale
}

// LookupAccess is a handler-scoped facade over a Lookup. It carries the kernel
// (for error reporting) and the read filesystem needed to lazily read sprite
// headers and font files, without ever retaining them past the handler's lock
// scope. Acquire a *Lookup write dependency plus storage.FileSystem in a
// handler, build a LookupAccess with NewLookupAccess, and pass it to consumers
// for the duration of that handler.
//
// It measures and it never loads onto the device: every verb it carries answers
// from a tier whose loader holds no GPU handle at all, which is what lets a
// handler that lays a page out declare no resource queue.
type LookupAccess struct {
	kernel kernel.Kernel
	lookup *Lookup
	fsys   fs.FS
}

// NewLookupAccess builds a scoped facade. Call it inside a handler that holds the
// *Lookup write lock and the storage.FileSystem read lock; never store the result.
//
// The filesystem is boxed into its interface here, once per handler, rather than
// at each read: storage.FileSystem is a struct, and handing one to an interface
// parameter costs an allocation every time it is done.
func NewLookupAccess(k kernel.Kernel, lookup *Lookup, filesystem storage.FileSystem) LookupAccess {
	return LookupAccess{kernel: k, lookup: lookup, fsys: filesystem}
}

// Valid reports whether the facade is backed by a live Lookup.
func (la LookupAccess) Valid() bool { return la.lookup != nil }

func fontReportKey(path string) string { return "font:" + path }

// report says a resource's fault once per episode. The kernel holds the keys,
// so the quiet outlives any one handler scope and a successful load or an
// unload is what ends it.
func (la LookupAccess) report(key string, err error) {
	if la.lookup == nil {
		return
	}
	la.kernel.ReportErrorOnce(key, err)
}

func (la LookupAccess) clearReport(key string) {
	if la.lookup != nil {
		la.kernel.ForgetReportedError(key)
	}
}

// SpriteSize returns a sprite's intrinsic pixel size, reading its header on first
// use. It does not require the GPU queue to be ready. On an invalid path or a
// missing/undecodable image it reports once and returns a zero size.
//
// It answers from the header tier and from nothing else. It used to prefer a
// resident atlas entry, so that layout tracked the pixels actually drawn; that
// read was a non-loading probe of the sprite table, and an asset cache has none -
// its one read loads. So layout tracks the header, and a file that changed on
// disk between the header read and the decode is measured as its header said.
func (la LookupAccess) SpriteSize(path string) m.Vec2 {
	if la.lookup == nil {
		return m.Vec2{}
	}
	clean, ok := ValidateResourcePath(path)
	if !ok {
		ReportInvalidSpritePath(la.kernel, path)
		return m.Vec2{}
	}
	size := la.lookup.spriteSizes.Get(la.kernel, assets.Descr[sizeDescrParams]{Name: clean}, la.fsys, struct{}{})
	return m.Vec2{X: float32(size.X), Y: float32(size.Y)}
}

// FontMetrics returns a font's vertical metrics at size (logical pixels). On an
// invalid path, non-positive size, or an unreadable font it reports once and
// returns zero metrics.
func (la LookupAccess) FontMetrics(path string, size int) FontMetrics {
	face := la.face(path, size)
	if face == nil {
		return FontMetrics{}
	}
	metrics := face.Face.Metrics()
	return FontMetrics{
		Ascent:     float32(metrics.Ascent) / 64,
		Descent:    float32(metrics.Descent) / 64,
		LineHeight: float32(metrics.Height) / 64,
		XHeight:    float32(metrics.XHeight) / 64,
		CapHeight:  float32(metrics.CapHeight) / 64,
	}
}

// MeasureTextSize returns the unwrapped logical size of text at the given font
// and size. Newlines split lines; ${path} tokens are measured as inline icons.
func (la LookupAccess) MeasureTextSize(path string, size int, text string) m.Vec2 {
	return la.measureTextSize(path, size, text, 0)
}

// MeasureWrappedTextSize returns the logical size after wrapping text at width.
// Non-positive or non-finite widths leave the text unwrapped.
func (la LookupAccess) MeasureWrappedTextSize(path string, size int, text string, width float32) m.Vec2 {
	return la.measureTextSize(path, size, text, width)
}

func (la LookupAccess) measureTextSize(path string, size int, text string, width float32) m.Vec2 {
	face := la.face(path, size)
	if face == nil {
		return m.Vec2{}
	}
	lineHeight := face.LineHeight
	if text == "" {
		return m.Vec2{Y: lineHeight}
	}
	capHeight := float32(face.Face.Metrics().CapHeight) / 64
	measure := func(line []InlineSegment) float32 {
		return la.measureInlineLine(face, line, capHeight)
	}
	lines := ParseInlineText(text)
	if ValidWrapWidth(width) {
		lines = WrapInlineText(lines, width, measure)
	}
	var maxWidth float32
	for _, line := range lines {
		maxWidth = max(maxWidth, measure(line))
	}
	return m.Vec2{X: maxWidth, Y: float32(len(lines)) * lineHeight}
}

func (la LookupAccess) measureInlineLine(face *Font, line []InlineSegment, capHeight float32) float32 {
	var width float32
	for _, segment := range line {
		if segment.Icon {
			width += la.iconWidth(segment.Text, capHeight)
			continue
		}
		width += MeasureLine(face, segment.Text)
	}
	return width
}

// iconWidth resolves an inline icon to its cap-height-scaled width using header
// metadata. A missing or empty icon reports once (through SpriteSize) and yields
// zero width so the surrounding text still measures.
func (la LookupAccess) iconWidth(path string, capHeight float32) float32 {
	size := la.SpriteSize(path)
	if size.Y <= 0 {
		return 0
	}
	return capHeight * size.X / size.Y
}

// LookupDeviceAccess is the other half of the facade: the verbs that need the
// device. Freeing an asset is immediate and hands its texture, its atlas slot or
// its array back to gfx at the call, so an unload needs the resource queue where
// a measurement needs only the filesystem.
//
// It is split off rather than folded in so that a handler which only measures -
// ui's, laying a page out - declares no *gfx.ResourceQueue and serialises against
// nothing that draws. scene names the same constraint with the same word: what
// separates the two facades in both plugins is the device.
type LookupDeviceAccess struct {
	kernel    kernel.Kernel
	lookup    *Lookup
	resources *gfx.ResourceQueue
}

// NewLookupDeviceAccess builds a scoped facade over the unload verbs. Call it
// inside a handler that holds the *Lookup and *gfx.ResourceQueue write locks;
// never store the result. There is no filesystem, because freeing reads nothing.
func NewLookupDeviceAccess(k kernel.Kernel, lookup *Lookup, resources *gfx.ResourceQueue) LookupDeviceAccess {
	return LookupDeviceAccess{kernel: k, lookup: lookup, resources: resources}
}

// Valid reports whether the facade is backed by a live Lookup.
func (la LookupDeviceAccess) Valid() bool { return la.lookup != nil }

// UnloadSprite releases a sprite from every tier that may hold it - the atlas,
// the standalone texture a tiled draw samples, and the measured header - and
// forgets what each of them reported. Unloading an absent sprite is a no-op; an
// invalid path is reported once.
//
// It frees at the call, with no queue in front of it: asking for the sprite
// again is a reload rather than an error, and that is also the one lever a
// terminal failure has.
//
// It names a file and only a file. The white texel is named by its bytes and has
// no path to pass here, so the one sprite this cannot free is the one canvas
// generates - a texel that failed to pack stays failed, and UnloadAll at a level
// boundary is what frees it. A texel only fails to pack when the atlas is
// already over its byte budget, which is the development-stage error the whole
// refusal is.
func (la LookupDeviceAccess) UnloadSprite(path string) {
	if la.lookup == nil {
		return
	}
	clean, ok := ValidateResourcePath(path)
	if !ok {
		ReportInvalidSpritePath(la.kernel, path)
		return
	}
	l := la.lookup
	l.sprites.Free(la.kernel, spriteDescr(clean),
		spriteUser{packer: l.spritePacker, resources: la.resources})
	l.tiled.Free(la.kernel, assets.Descr[tiledDescrParams]{Name: clean}, la.resources)
	l.spriteSizes.Free(la.kernel, assets.Descr[sizeDescrParams]{Name: clean}, struct{}{})
}

// UnloadFont releases a font's parsed source and every face baked from it, so a
// later draw re-reads the file. Unloading an absent font is a no-op; an invalid
// path is reported once. An empty path names the built-in default, the same as
// everywhere else.
//
// The glyph atlas slots those faces rasterized into are not reclaimed: the glyph
// side is a packer with no table, and nothing but a framebuffer scale change
// knows which slots a font owned. It is the same leak the deferred unload had.
func (la LookupDeviceAccess) UnloadFont(path string) {
	if la.lookup == nil {
		return
	}
	path = ResolveFontPath(path)
	clean, ok := ValidateResourcePath(path)
	if !ok {
		la.kernel.ReportErrorOnce(fontReportKey(path), fmt.Errorf("canvas: invalid font path %q", path))
		return
	}
	la.lookup.unloadFont(clean)
	la.kernel.ForgetReportedError(fontReportKey(clean))
}

// face bakes (or reuses) a font face at the given logical size, reporting once on
// failure. It needs only the filesystem, never the GPU queue. An empty path bakes
// the built-in default font, so measurement matches what Text will draw.
func (la LookupAccess) face(path string, size int) *Font {
	if la.lookup == nil {
		return nil
	}
	path = ResolveFontPath(path)
	clean, ok := ValidateResourcePath(path)
	if !ok || size <= 0 {
		la.report(fontReportKey(path), fmt.Errorf("canvas: invalid font path %q or size %d", path, size))
		return nil
	}
	face := la.lookup.fontStore.Face(la.fsys, clean, size)
	if face == nil {
		la.report(fontReportKey(clean), fmt.Errorf("canvas: font %q could not be loaded", clean))
		return nil
	}
	la.clearReport(fontReportKey(clean))
	return face
}

// MeasureLine returns the logical advance width of one line (kerning included).
func MeasureLine(face *Font, text string) float32 {
	var width float32
	var previous rune
	first := true
	for _, character := range text {
		if !first {
			width += float32(face.Face.Kern(previous, character)) / 64
		}
		if advance, ok := face.Face.GlyphAdvance(character); ok {
			width += float32(advance) / 64
		}
		previous = character
		first = false
	}
	return width
}
