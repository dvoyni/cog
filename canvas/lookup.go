package canvas

import (
	"fmt"
	"image"
	pathpkg "path"
	"strings"

	"github.com/dvoyni/cog/m"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/storage"
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

// Lookup is the single Canvas-owned resource that holds the sprite atlas, glyph
// atlas, font store, and the cached sprite metadata behind sizing and text
// measurement. Callers acquire it as a write dependency and operate on it through
// a scoped LookupAccess; the resource itself never retains filesystem or GPU
// handles. Deferred unloads are applied by the Canvas flush at the frame boundary.
type Lookup struct {
	config      Config
	sprites     *atlas
	fonts       *atlas
	fontStore   *fontStore
	spriteSizes map[string]m.Vec2i
	// reported suppresses repeated error reports for the same resource until it
	// next loads successfully or is unloaded.
	reported             map[string]struct{}
	unloadSprites        []string
	unloadFonts          []string
	lastFramebufferScale float32
}

func newLookup(config Config) *Lookup {
	return &Lookup{
		config:      config,
		sprites:     newAtlas(config),
		fonts:       newAtlas(config),
		fontStore:   newFontStore(),
		spriteSizes: map[string]m.Vec2i{},
		reported:    map[string]struct{}{},
	}
}

// NewLookup builds an empty Lookup resource for the given configuration. The
// Canvas plugin creates one internally; this constructor also lets tests and
// embedders build a Lookup to drive a LookupAccess directly.
func NewLookup(config Config) *Lookup { return newLookup(config) }

// applyUnloads evicts sprites and fonts queued for release since the last frame.
// It runs at the Canvas flush boundary, after any draw ops recorded this frame
// have already been resolved, so a same-frame unload never dangles a live draw.
func (l *Lookup) applyUnloads(resources *gfx.ResourceQueue) {
	for _, path := range l.unloadSprites {
		l.sprites.releasePath(path, resources)
		delete(l.spriteSizes, path)
		delete(l.reported, spriteReportKey(path))
	}
	l.unloadSprites = l.unloadSprites[:0]
	for _, path := range l.unloadFonts {
		l.unloadFont(path)
		delete(l.reported, fontReportKey(path))
	}
	l.unloadFonts = l.unloadFonts[:0]
}

// unloadFont drops a font's baked faces and parsed source so their CPU memory is
// reclaimed and a later load re-reads the file. The glyph atlas pages are not
// freed per font; the whole glyph atlas is released together on a framebuffer
// scale change (invalidateFontsOnResize), which is the only time glyph pages
// need reclaiming.
func (l *Lookup) unloadFont(path string) {
	for key, cached := range l.fontStore.fonts {
		if key.path == path {
			_ = cached.face.Close()
			delete(l.fontStore.fonts, key)
		}
	}
	delete(l.fontStore.sources, path)
}

// invalidateFontsOnResize drops the glyph atlas and baked faces when the
// framebuffer/logical scale changes, so glyphs re-rasterize at full resolution.
func (l *Lookup) invalidateFontsOnResize(resources *gfx.ResourceQueue, view *app.Viewport) {
	scale := float32(1)
	if view.Width > 0 && view.FramebufferWidth > 0 {
		scale = view.FramebufferWidth / view.Width
	}
	if l.lastFramebufferScale != 0 && scale != l.lastFramebufferScale {
		l.fonts.releaseAll(resources)
		clearFontFaces(l.fontStore)
	}
	l.lastFramebufferScale = scale
}

// LookupAccess is a handler-scoped facade over a Lookup. It carries the kernel
// (for error reporting) and the read filesystem needed to lazily load sprites and
// fonts, without ever retaining them past the handler's lock scope. Acquire a
// *Lookup write dependency plus storage.FileSystem in a handler, build a LookupAccess
// with NewLookupAccess, and pass it to consumers for the duration of that handler.
type LookupAccess struct {
	kernel kernel.Kernel
	lookup *Lookup
	fs     storage.FileSystem
}

// NewLookupAccess builds a scoped facade. Call it inside a handler that holds the
// *Lookup write lock and the storage.FileSystem read lock; never store the result.
func NewLookupAccess(k kernel.Kernel, lookup *Lookup, filesystem storage.FileSystem) LookupAccess {
	return LookupAccess{kernel: k, lookup: lookup, fs: filesystem}
}

// Valid reports whether the facade is backed by a live Lookup.
func (la LookupAccess) Valid() bool { return la.lookup != nil }

func spriteReportKey(path string) string { return "sprite:" + path }
func fontReportKey(path string) string   { return "font:" + path }

func (la LookupAccess) report(key string, err error) {
	if la.lookup == nil {
		return
	}
	if _, done := la.lookup.reported[key]; done {
		return
	}
	la.lookup.reported[key] = struct{}{}
	la.kernel.ReportError(err)
}

func (la LookupAccess) clearReport(key string) {
	if la.lookup != nil {
		delete(la.lookup.reported, key)
	}
}

// SpriteSize returns a sprite's intrinsic pixel size, loading its header on first
// use. It does not require the GPU queue to be ready. On an invalid path or a
// missing/undecodable image it reports once and returns a zero size.
func (la LookupAccess) SpriteSize(path string) m.Vec2 {
	if la.lookup == nil {
		return m.Vec2{}
	}
	clean, ok := validateResourcePath(path)
	if !ok {
		la.report(spriteReportKey(path), fmt.Errorf("canvas: invalid sprite path %q", path))
		return m.Vec2{}
	}
	// A resident atlas entry carries authoritative decoded dimensions; prefer it
	// so layout tracks the pixels actually drawn even if the file changed.
	if entry, ok := la.lookup.sprites.entries[clean]; ok {
		la.clearReport(spriteReportKey(clean))
		return m.Vec2{X: float32(entry.width), Y: float32(entry.height)}
	}
	if meta, ok := la.lookup.spriteSizes[clean]; ok {
		return m.Vec2{X: float32(meta.X), Y: float32(meta.Y)}
	}
	width, height, ok := decodeImageHeader(la.fs, clean)
	if !ok {
		la.report(spriteReportKey(clean), fmt.Errorf("canvas: sprite %q could not be loaded", clean))
		return m.Vec2{}
	}
	la.lookup.spriteSizes[clean] = m.Vec2i{X: width, Y: height}
	la.clearReport(spriteReportKey(clean))
	return m.Vec2{X: float32(width), Y: float32(height)}
}

// FontMetrics returns a font's vertical metrics at size (logical pixels). On an
// invalid path, non-positive size, or an unreadable font it reports once and
// returns zero metrics.
func (la LookupAccess) FontMetrics(path string, size int) FontMetrics {
	face := la.face(path, size)
	if face == nil {
		return FontMetrics{}
	}
	metrics := face.face.Metrics()
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
	lineHeight := face.lineHeight
	if text == "" {
		return m.Vec2{Y: lineHeight}
	}
	capHeight := float32(face.face.Metrics().CapHeight) / 64
	measure := func(line []inlineSegment) float32 {
		return la.measureInlineLine(face, line, capHeight)
	}
	lines := parseInlineText(text)
	if validWrapWidth(width) {
		lines = wrapInlineText(lines, width, measure)
	}
	var maxWidth float32
	for _, line := range lines {
		maxWidth = max(maxWidth, measure(line))
	}
	return m.Vec2{X: maxWidth, Y: float32(len(lines)) * lineHeight}
}

func (la LookupAccess) measureInlineLine(face *canvasFont, line []inlineSegment, capHeight float32) float32 {
	var width float32
	for _, segment := range line {
		if segment.icon {
			width += la.iconWidth(segment.text, capHeight)
			continue
		}
		width += measureLine(face, segment.text)
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

// UnloadSprite queues a sprite for release at the next frame boundary. Unloading
// an absent sprite is a no-op; an invalid path is reported once.
func (la LookupAccess) UnloadSprite(path string) {
	if la.lookup == nil {
		return
	}
	clean, ok := validateResourcePath(path)
	if !ok {
		la.report(spriteReportKey(path), fmt.Errorf("canvas: invalid sprite path %q", path))
		return
	}
	la.lookup.unloadSprites = append(la.lookup.unloadSprites, clean)
}

// UnloadFont queues a font (all sizes) for release at the next frame boundary.
// Unloading an absent font is a no-op; an invalid path is reported once.
func (la LookupAccess) UnloadFont(path string) {
	if la.lookup == nil {
		return
	}
	clean, ok := validateResourcePath(path)
	if !ok {
		la.report(fontReportKey(path), fmt.Errorf("canvas: invalid font path %q", path))
		return
	}
	la.lookup.unloadFonts = append(la.lookup.unloadFonts, clean)
}

// face bakes (or reuses) a font face at the given logical size, reporting once on
// failure. It needs only the filesystem, never the GPU queue.
func (la LookupAccess) face(path string, size int) *canvasFont {
	if la.lookup == nil {
		return nil
	}
	clean, ok := validateResourcePath(path)
	if !ok || size <= 0 {
		la.report(fontReportKey(path), fmt.Errorf("canvas: invalid font path %q or size %d", path, size))
		return nil
	}
	face := la.lookup.fontStore.face(la.fs, clean, size)
	if face == nil {
		la.report(fontReportKey(clean), fmt.Errorf("canvas: font %q could not be loaded", clean))
		return nil
	}
	la.clearReport(fontReportKey(clean))
	return face
}

// measureLine returns the logical advance width of one line (kerning included).
func measureLine(face *canvasFont, text string) float32 {
	var width float32
	var previous rune
	first := true
	for _, character := range text {
		if !first {
			width += float32(face.face.Kern(previous, character)) / 64
		}
		if advance, ok := face.face.GlyphAdvance(character); ok {
			width += float32(advance) / 64
		}
		previous = character
		first = false
	}
	return width
}

// decodeImageHeader reads only an image's header to get its pixel size, avoiding a
// full decode or any GPU upload.
func decodeImageHeader(filesystem storage.FileSystem, path string) (width, height int, ok bool) {
	file, err := filesystem.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0, false
	}
	return config.Width, config.Height, true
}

// validateResourcePath normalizes a resource path and rejects empty, absolute,
// NUL-bearing, or root-escaping inputs so every Lookup method shares one cache key
// and security boundary.
func validateResourcePath(path string) (string, bool) {
	if path == "" || strings.ContainsRune(path, 0) {
		return "", false
	}
	cleaned := pathpkg.Clean(strings.ReplaceAll(path, "\\", "/"))
	if cleaned == "" || cleaned == "." {
		return "", false
	}
	if strings.HasPrefix(cleaned, "/") {
		return "", false
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}
