package canvas

import (
	"cmp"
	"errors"
	"fmt"
	"image"
	"io/fs"
	"math/bits"
	"slices"
	"strings"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas/atlas"
	"github.com/dvoyni/cog/plugins/render"
)

// canvas's atlases' gutters and mip levels are bounded by these.
const (
	// smallestAtlasGutter is how many texels a cell of a sprite atlas
	// reaches past its image on every side at least, holding the image's
	// edge continued, so linear filtering at its edge never reads a
	// neighbour: the whole gutter of a page of one mip level, packed with
	// no atlas grid.
	smallestAtlasGutter = 2
	// largestAtlasMipLevelCount is the most mip levels a sprite atlas's
	// pages are made with: the most whose smallest page, the white texel's
	// cell of three times the atlas grid, fits WebGPU's default limit of 8192
	// texels on a texture's side.
	largestAtlasMipLevelCount = 12
	// glyphAtlasGutter is how many texels a cell of the glyph atlas
	// reaches past its glyph on every side, left transparent: a distance
	// field fades to nothing at its own edge, so linear filtering there
	// reads nothing of a neighbour.
	glyphAtlasGutter = 1
)

// The labels of canvas's atlases' textures.
const (
	srgbAtlasLabel   = "canvas sprites sRGB"
	linearAtlasLabel = "canvas sprites linear"
	glyphAtlasLabel  = "canvas glyphs"
)

// findSpriteAtlasGrid answers the atlas grid the cells of a sprite atlas
// whose pages have mipLevelCount levels sit on, 2^(mipLevelCount−1)
// texels, and the gutter each cell reaches past its image, the atlas grid
// but no fewer than smallestAtlasGutter texels, which the sprite atlases'
// descriptors give and the plugin fits their page side to: every texel of
// every level then lies within one cell, so a page's mips mix no images,
// and linear filtering at its deepest level reads only an image's own
// texels and its gutter.
func findSpriteAtlasGrid(mipLevelCount uint32) (atlasGrid, gutter uint32) {
	atlasGrid = 1 << (mipLevelCount - 1)
	return atlasGrid, max(atlasGrid, smallestAtlasGutter)
}

// atlasSet is where canvas keeps what its entries draw from, the drawing
// job's alone: the plain sprite atlas of each colour space, the sRGB one
// holding the white texel shapes sample, made with the pack material that
// fills a plain image's cell at canvas's first draw, the linear one at the
// first image needing it; the glyph atlas, made at the first frame drawing
// text; each image's drawn frames, packed, of a render texture drawn
// through a view, or found to draw nothing; the frames packed, into an
// atlas or a texture of their own; the glyphs packed; and the frame count
// that eviction reads.
//
// A frame begins with beginFrame, and unload for each path given up; find
// and findGlyphs then mark what each entry draws, noting what they miss, and
// placeMissed packs only that, so packing evicts nothing the frame draws;
// recordUploads then records what puts it there, before any pass reads it.
type atlasSet struct {
	// atlasPageSize is the side of a sprite atlas's pages,
	// Config.AtlasPageSize's, atlasLayerCeiling the most pages it grows to,
	// Config.AtlasLayerCeiling's, and ownTextureSize the side past which an
	// image is drawn from a texture of its own, Config.OwnTextureSize's,
	// none of them zero; mipLevelCount is how many mip levels an atlas's
	// pages have, one when Config.DoNotGenerateMipMaps is set and
	// Config.MipLevels otherwise.
	atlasPageSize     uint32
	atlasLayerCeiling uint32
	ownTextureSize    uint32
	mipLevelCount     uint32
	// packMaterial and srgbAtlas are zero until init makes them, linearAtlas
	// until an image needs it, and glyphAtlas until a run of text does.
	packMaterial render.Material
	srgbAtlas    *atlas.Atlas
	linearAtlas  *atlas.Atlas
	glyphAtlas   *atlas.Atlas
	// drawnImages are the frames of the images drawn, each image's at its
	// Image's index, made at its first draw, then each frame at its
	// number, and one more after its sheet's last, for the frames past it;
	// the zero Image's one frame is the white texel's, and one not placed
	// is of no size. packedImages are the frames packed, by the texels
	// they are packed from, which every frame and image of the same
	// rectangle of one file and wrap shares, images differing only in their
	// pivot among them; the white texel is none of them, so nothing evicts
	// it.
	drawnImages  [][]drawnImage
	packedImages map[packedKey]*packedImage
	// drawnGlyphs are the glyphs drawn, each glyph's field as drawn at its
	// index in the FontLibrary's glyph table, of no size until packed at its
	// first draw, its field put into the glyph atlas then.
	drawnGlyphs []drawnGlyph
	// decodedFiles are the files of converted images placeMissed read and
	// decoded as it packed the frame's images, each by its key, nil for one
	// that could not be, so the frames and rectangles of one file decode it
	// once, forgotten once they are packed.
	decodedFiles map[sourceKey]image.Image
	// drawnFrameCount counts the frames begun, the frame being drawn
	// included, so a packed image last drawn in it is not evicted.
	drawnFrameCount uint64
	// packings are the images packed whose cells a command list is still
	// to fill, kept from the frame that made them until one does, and
	// packInstances their quads, written over as recordUploads fills them.
	packings      []atlasPacking
	packInstances []instanceData
	// missedFrames are the frames of images find found not placed this
	// frame, and missedGlyphs the glyphs findGlyphs found not packed, each
	// once, both forgotten as the frame begins; atlasMoved is whether
	// placeMissed grew an atlas, replacing its texture; evictionCandidates are the
	// images a full atlas may evict, in the order it evicts them, written
	// over as they are needed.
	missedFrames       []imageFrame
	missedGlyphs       []uint32
	atlasMoved         bool
	evictionCandidates []evictionCandidate
	// colorTargets is a pack pass's one target.
	colorTargets [1]render.ColorTarget
}

// newAtlasSet answers an atlas set that holds nothing, its sprite atlases'
// pages atlasPageSize texels square, growing to atlasLayerCeiling pages, an
// image wider or taller than ownTextureSize drawn from a texture of its
// own, every atlas's pages of mipLevelCount levels, holding the zero
// Image's drawn image, placed in the white texel once init makes it.
func newAtlasSet(atlasPageSize, atlasLayerCeiling, ownTextureSize, mipLevelCount uint32) atlasSet {
	return atlasSet{
		atlasPageSize:     atlasPageSize,
		atlasLayerCeiling: atlasLayerCeiling,
		ownTextureSize:    ownTextureSize,
		mipLevelCount:     mipLevelCount,
		drawnImages:       [][]drawnImage{{{}}},
		packedImages:      map[packedKey]*packedImage{},
		decodedFiles:      map[sourceKey]image.Image{},
	}
}

// init makes the pack material, over viewportShader, replacing what an
// atlas's cell holds with an image premultiplied, and the sRGB sprite
// atlas, the white texel packed into it first.
func (a *atlasSet) init(inventory render.Inventory, viewportShader render.Shader) {
	a.packMaterial = inventory.MakeMaterial(render.MaterialDescriptor{
		Label:              "canvas pack",
		Shader:             viewportShader,
		VertexEntryPoint:   viewportVertexEntryPoint,
		FragmentEntryPoint: packFragmentEntryPoint,
	})
	a.srgbAtlas = atlas.New(inventory, a.makeSpriteAtlasDescriptor(ColorSpaceSrgb))
	a.packWhiteTexel(inventory)
}

// packWhiteTexel packs the white texel, the zero Image, into the sRGB
// sprite atlas, empty, and puts it there as any image's texels are put, its
// cell continuing it: the cell sits on the atlas grid, so each level of its
// page's mips halves it exactly, white throughout.
func (a *atlasSet) packWhiteTexel(inventory render.Inventory) {
	// A page holds the white texel's cell, as the plugin raised its side to.
	whiteRegion, _ := a.srgbAtlas.Pack(inventory, 1, 1)
	a.srgbAtlas.Put(whiteRegion, []byte{255, 255, 255, 255}, atlas.WrapNone)
	a.drawnImages[0][0] = makeDrawnImage(&packedImage{atlas: a.srgbAtlas, region: whiteRegion}, ImageOptions{})
}

// makeSpriteAtlasDescriptor answers what the sprite atlas of colorSpace is
// made of: pages of atlasPageSize texels, growing to atlasLayerCeiling, of
// mipLevelCount levels, holding RGBA8 texels encoded as colorSpace says,
// each cell's gutter the one findSpriteAtlasGrid answers, as the plugin
// fitted the page side to.
func (a *atlasSet) makeSpriteAtlasDescriptor(colorSpace ColorSpace) atlas.Descriptor {
	_, gutter := findSpriteAtlasGrid(a.mipLevelCount)
	descriptor := atlas.Descriptor{
		Label:         srgbAtlasLabel,
		Format:        render.TextureFormatRGBA8UnormSrgb,
		PageSize:      a.atlasPageSize,
		MipLevelCount: a.mipLevelCount,
		Gutter:        gutter,
		LayerCeiling:  a.atlasLayerCeiling,
	}
	if colorSpace == ColorSpaceLinear {
		descriptor.Label = linearAtlasLabel
		descriptor.Format = render.TextureFormatRGBA8Unorm
	}
	return descriptor
}

// atlases answers the set's atlases, each nil until made: the sRGB and
// linear sprite atlases and the glyph atlas.
func (a *atlasSet) atlases() [3]*atlas.Atlas {
	return [3]*atlas.Atlas{a.srgbAtlas, a.linearAtlas, a.glyphAtlas}
}

// beginFrame begins a frame: it counts it, so what it draws is marked
// drawn in it, and forgets what the frame before missed.
func (a *atlasSet) beginFrame() {
	a.drawnFrameCount++
	a.missedFrames = a.missedFrames[:0]
	a.missedGlyphs = a.missedGlyphs[:0]
}

// unload drops each frame packed of the paths Unload(prefix) unloads, so
// its next draw packs it again, and forgets each frame of theirs found
// outside its file, so its next draw looks again; an image of a render
// texture has no path.
func (a *atlasSet) unload(inventory render.Inventory, prefix string) {
	for key, packed := range a.packedImages {
		if isUnder(key.source.path, prefix) {
			a.dropPackedImage(inventory, key, packed)
		}
	}
	for index, frames := range a.drawnImages {
		if !slices.ContainsFunc(frames, func(drawn drawnImage) bool { return drawn.drawsNothing }) {
			continue
		}
		if key := imageNames.findKey(Image{index: uint32(index)}); !key.ofTexture && isUnder(key.path, prefix) {
			for frame := range frames {
				frames[frame].drawsNothing = false
			}
		}
	}
}

// find marks frame of image drawn this frame, so no atlas evicts it in it,
// its image's drawn frames made at its first draw, and notes it missed when
// it is not placed, so placeMissed places it.
func (a *atlasSet) find(image Image, frame uint32) {
	for int(image.index) >= len(a.drawnImages) {
		a.drawnImages = append(a.drawnImages, nil)
	}
	if a.drawnImages[image.index] == nil {
		sheet := imageNames.findKey(image).options.Sheet
		a.drawnImages[image.index] = make([]drawnImage, cmp.Or(sheet.Columns, 1)*cmp.Or(sheet.Rows, 1)+1)
	}
	drawn := a.placed(image, frame)
	switch {
	case drawn.packed != nil:
		drawn.packed.lastDrawnFrame = a.drawnFrameCount
	case drawn.view.Texture == (render.Texture{}) && !drawn.drawsNothing:
		a.missedFrames = append(a.missedFrames, imageFrame{image: image, frame: frame})
	}
}

// findGlyphs finds the glyphs of a run of text, those of a font, each its
// field at its field's index in the FontLibrary's glyph table: it notes one
// missed when it is not packed, once a frame, so placeMissed tries it once.
// The glyph atlas, which the run is keyed by even when it draws icons
// alone, is made at the first run found.
func (a *atlasSet) findGlyphs(inventory render.Inventory, glyphs []placedGlyph) {
	if a.glyphAtlas == nil {
		a.glyphAtlas = atlas.New(inventory, atlas.Descriptor{
			Label:        glyphAtlasLabel,
			Format:       render.TextureFormatR8Unorm,
			PageSize:     a.atlasPageSize,
			Gutter:       glyphAtlasGutter,
			ClearGutter:  true,
			LayerCeiling: a.atlasLayerCeiling,
		})
	}
	for _, glyph := range glyphs {
		if glyph.font == nil {
			continue
		}
		if int(glyph.field) >= len(a.drawnGlyphs) {
			a.drawnGlyphs = slices.Grow(a.drawnGlyphs, int(glyph.field)+1-len(a.drawnGlyphs))[:glyph.field+1]
		}
		if drawn := &a.drawnGlyphs[glyph.field]; !drawn.packed && drawn.triedFrame != a.drawnFrameCount {
			drawn.triedFrame = a.drawnFrameCount
			a.missedGlyphs = append(a.missedGlyphs, glyph.field)
		}
	}
}

// findGlyphAtlas answers the view of the glyph atlas, as it is now, once
// findGlyphs has made it.
func (a *atlasSet) findGlyphAtlas() render.TextureView {
	if a.glyphAtlas == nil {
		panic("canvas: reading the glyph atlas before findGlyphs made it")
	}
	return render.TextureView{Texture: a.glyphAtlas.Texture()}
}

// findSpriteAtlas answers the view of the sRGB sprite atlas, as it is now:
// what shapes and clears sample, its white texel, and runs of text their
// icons.
func (a *atlasSet) findSpriteAtlas() render.TextureView {
	return render.TextureView{Texture: a.srgbAtlas.Texture()}
}

// missed answers whether find or findGlyphs noted anything missed this
// frame.
func (a *atlasSet) missed() bool {
	return len(a.missedFrames) > 0 || len(a.missedGlyphs) > 0
}

// placed answers frame of image as drawn, as find made it and placeMissed
// placed it, marking nothing: its own, or, past its sheet's last, the one
// after the last, and the zero Image's one frame, the white texel,
// whatever frame.
func (a *atlasSet) placed(image Image, frame uint32) *drawnImage {
	frames := a.drawnImages[image.index]
	if len(frames) == 0 {
		panic("canvas: finding a frame of an image whose drawn frames were never made")
	}
	return &frames[min(frame, uint32(len(frames)-1))]
}

// placedGlyph answers the glyph whose field is at field as drawn, as
// findGlyphs made it and placeMissed packed it, marking nothing.
func (a *atlasSet) placedGlyph(field uint32) *drawnGlyph {
	return &a.drawnGlyphs[field]
}

// placeMissed places what the frame missed, and answers whether an atlas
// grew, replacing its texture, so every key found before it is found
// again. It packs each glyph missed, its field read from glyphs. Each frame missed is placed: an image of a render
// texture given a view of it, any other packed as packImage packs it, its
// file stamped in stamps, and noted in inspector when packed now, or found
// to draw nothing.
func (a *atlasSet) placeMissed(inventory render.Inventory, files fs.FS, glyphs *glyphTable, reporter *reporter, requests *residencyRequestQueue, stamps *fileStampTable, inspector *inspector) (moved bool) {
	a.atlasMoved = false
	for _, field := range a.missedGlyphs {
		a.packGlyph(inventory, reporter, &a.drawnGlyphs[field], glyphs.findField(field))
	}
	for _, missed := range a.missedFrames {
		drawn := a.placed(missed.image, missed.frame)
		// An earlier sprite or icon of the same frame may have placed it:
		// packed it, given it a view of its texture, or found it draws
		// nothing.
		if drawn.packed != nil || drawn.view.Texture != (render.Texture{}) || drawn.drawsNothing {
			continue
		}
		key := imageNames.findKey(missed.image)
		reportIgnoredOptions(reporter, key)
		reportRefusedConverter(reporter, key)
		if key.ofTexture {
			placeTextureImage(inventory, reporter, drawn, key, missed.frame)
			continue
		}
		stamps.add(files, key.path)
		packed, drawsTexels, packedNow := a.packImage(inventory, files, reporter, requests, key, missed.frame)
		if !drawsTexels {
			drawn.drawsNothing = true
			continue
		}
		if packedNow {
			inspector.addPackedImage(missed.image, missed.frame)
		}
		*drawn = makeDrawnImage(packed, key.options)
	}
	clear(a.decodedFiles)
	return a.atlasMoved
}

// pack packs a cell of width by height texels into packedAtlas, as Pack
// does, noting it moved when growing replaced the atlas's texture.
func (a *atlasSet) pack(inventory render.Inventory, packedAtlas *atlas.Atlas, width, height uint32) (render.TextureRegion, error) {
	texture := packedAtlas.Texture()
	region, err := packedAtlas.Pack(inventory, width, height)
	a.atlasMoved = a.atlasMoved || packedAtlas.Texture() != texture
	return region, err
}

// packGlyph packs drawn, a glyph of field, into the glyph atlas, its field
// put into its cell, the gutter left transparent: on its first page holding
// it, else on a new one, the atlas doubling its pages while under its
// ceiling. A glyph whose cell no page holds, or one the atlas at its
// ceiling has no room for, draws nothing, reported once, and is tried again
// at the next frame drawing it, findGlyphs noting it once a frame.
func (a *atlasSet) packGlyph(inventory render.Inventory, reporter *reporter, drawn *drawnGlyph, field glyphField) {
	width := uint32(field.width)
	height := uint32(field.height)
	region, err := a.pack(inventory, a.glyphAtlas, width, height)
	if err != nil {
		fullReport := Report{Kind: ReportKindAtlasFull, Err: fmt.Errorf("canvas: the glyph atlas, of %d pages of %d texels a side, has no room for a glyph's field of %d by %d texels, so it draws nothing", a.glyphAtlas.PageCount(), a.atlasPageSize, width, height)}
		reporter.reportOnce(atlasFullLogKey+glyphAtlasLabel, fullReport)
		return
	}
	a.glyphAtlas.Put(region, field.texels, atlas.WrapNone)
	*drawn = makeDrawnGlyph(a.glyphAtlas, region, field)
}

// packImage packs frame of the image of key, marked drawn this frame, and
// answers where, and whether it draws texels: false for a frame whose
// rectangle does not lie within its file, which is not packed and is
// reported once for its path, and for a converted frame whose
// converter answered no texel; and whether it packed the frame now, not one
// sharing the cell of one packed before. A converted image's file is read
// from files and decoded, as decodeConvertedFile does; any other's, or one that could
// not be decoded, which render draws magenta, is acquired from render with
// its colour space, sharing the texture Assets.Preload acquired, which it
// then releases from requests, of the size render's descriptor of it
// gives, its file's header's, a texture render could not make one texel.
// A frame of the texels of one packed already, of the same rectangle of
// the file, converter and wrap, shares its cell, its source released. Any
// other is packed into the sprite atlas of its colour space, a converted
// one as a texture made of its converter's output, placed by its bounds
// within the frame, grown or evicting images when full, or into a texture
// of its own, exactly its size with no gutter, when it is wider or taller
// than ownTextureSize, when no page holds its cell, or when the atlas is
// at its ceiling with nothing to evict, reported, its cell kept
// for the next command list to fill.
func (a *atlasSet) packImage(inventory render.Inventory, files fs.FS, reporter *reporter, requests *residencyRequestQueue, key imageKey, frame uint32) (packed *packedImage, drawsTexels, packedNow bool) {
	sourceKey := sourceKey{path: key.path, colorSpace: key.options.ColorSpace}
	converter := key.options.Convert
	var picture image.Image
	if converter != nil {
		picture = a.decodeConvertedFile(files, requests, sourceKey)
		if picture == nil {
			converter = nil
		}
	}
	var source render.Texture
	fileWidth := 1
	fileHeight := 1
	if picture != nil {
		fileWidth = picture.Bounds().Dx()
		fileHeight = picture.Bounds().Dy()
	} else {
		source = inventory.AcquireTexture(sourceKey.path, sourceKey.makeTextureOptions())
		requests.releasePreload(inventory, sourceKey)
		if descriptor, held := inventory.TextureDescriptor(source); held {
			fileWidth = int(descriptor.Width)
			fileHeight = int(descriptor.Height)
		}
	}
	frameRect, withinFile := key.options.findFrameRect(fileWidth, fileHeight, frame)
	if !withinFile {
		reportFrameOutOfBounds(reporter, key, frame, "file", fileWidth, fileHeight)
	}
	packedKey := packedKey{source: sourceKey, rect: frameRect, converter: converter, wrap: key.options.Wrap}
	packed, shared := a.packedImages[packedKey]
	if shared || !withinFile {
		if source != (render.Texture{}) {
			inventory.ReleaseTexture(source)
		}
		if shared {
			packed.lastDrawnFrame = a.drawnFrameCount
		}
		return packed, withinFile, false
	}
	descriptor := a.makeSpriteAtlasDescriptor(key.options.ColorSpace)
	spriteAtlas := a.srgbAtlas
	if key.options.ColorSpace == ColorSpaceLinear {
		if a.linearAtlas == nil {
			a.linearAtlas = atlas.New(inventory, descriptor)
		}
		spriteAtlas = a.linearAtlas
	}
	packed = &packedImage{lastDrawnFrame: a.drawnFrameCount}
	sourceRect := frameRect
	if converter != nil {
		output := convertFrame(picture, frameRect, converter)
		if output.Rect.Empty() {
			return nil, false, false
		}
		sourceRect = m.Recti{Width: output.Rect.Dx(), Height: output.Rect.Dy()}
		source = inventory.MakeTexture(render.TextureDescriptor{Label: key.path, Width: uint32(sourceRect.Width), Height: uint32(sourceRect.Height), Format: descriptor.Format}, output.Pix)
		packed.frameBounds = m.Recti{X: -output.Rect.Min.X, Y: -output.Rect.Min.Y, Width: frameRect.Width, Height: frameRect.Height}.Float()
	}
	width := uint32(sourceRect.Width)
	height := uint32(sourceRect.Height)
	fits := false
	if width <= a.ownTextureSize && height <= a.ownTextureSize {
		packed.region, fits = a.packIntoSpriteAtlas(inventory, reporter, spriteAtlas, descriptor.Label, key.path, width, height)
	}
	if fits {
		packed.atlas = spriteAtlas
	} else {
		// A texture of its own is exactly its size, of no gutter, so an image
		// as large as a page fills a texture of a page's size, of the page's
		// mip levels or as many as its size has.
		packed.region = render.TextureRegion{Width: width, Height: height}
		packed.ownTexture = inventory.MakeTexture[byte](render.TextureDescriptor{
			Label:         key.path,
			Width:         width,
			Height:        height,
			LayerCount:    1,
			MipLevelCount: min(a.mipLevelCount, uint32(bits.Len32(max(width, height)))),
			Format:        descriptor.Format,
		}, nil)
	}
	a.packedImages[packedKey] = packed
	a.packings = append(a.packings, atlasPacking{packed: packed, source: source, frameRect: sourceRect, wrap: key.options.Wrap})
	return packed, true, true
}

// decodeConvertedFile answers the file of key, a converted image's, read
// from files and decoded, once for each frame, taking the picture
// Assets.Preload decoded from requests, or nil when it cannot be decoded,
// as decodeImageFile answers it.
func (a *atlasSet) decodeConvertedFile(files fs.FS, requests *residencyRequestQueue, key sourceKey) image.Image {
	if picture, decoded := a.decodedFiles[key]; decoded {
		return picture
	}
	picture := requests.takePreloadedFile(key)
	if picture == nil {
		picture = decodeImageFile(files, key.path)
	}
	a.decodedFiles[key] = picture
	return picture
}

// packIntoSpriteAtlas packs an image of width by height texels, at path,
// into spriteAtlas, labelled label, and answers where, or false when it
// does not fit: on the first page holding it; else on a new page, the
// atlas doubling its pages while under its ceiling; else where the images
// it evicts leave room, the least lately drawn first, never one drawn this
// frame. An image whose cell no page holds is not packed, and an atlas at
// its ceiling with nothing to evict is reported, once for the atlas.
func (a *atlasSet) packIntoSpriteAtlas(inventory render.Inventory, reporter *reporter, spriteAtlas *atlas.Atlas, label, path string, width, height uint32) (render.TextureRegion, bool) {
	region, err := a.pack(inventory, spriteAtlas, width, height)
	if !errors.Is(err, atlas.ErrFull) {
		return region, err == nil
	}
	a.evictionCandidates = a.evictionCandidates[:0]
	for key, packed := range a.packedImages {
		if packed.atlas == spriteAtlas && packed.lastDrawnFrame != a.drawnFrameCount {
			a.evictionCandidates = append(a.evictionCandidates, evictionCandidate{key: key, packed: packed})
		}
	}
	// Those last drawn in one frame go in their paths' order, then their
	// rectangles' rows and columns.
	slices.SortFunc(a.evictionCandidates, func(first, second evictionCandidate) int {
		return cmp.Or(cmp.Compare(first.packed.lastDrawnFrame, second.packed.lastDrawnFrame), strings.Compare(first.key.source.path, second.key.source.path), cmp.Compare(first.key.rect.Y, second.key.rect.Y), cmp.Compare(first.key.rect.X, second.key.rect.X))
	})
	for _, candidate := range a.evictionCandidates {
		a.dropPackedImage(inventory, candidate.key, candidate.packed)
		if region, err := a.pack(inventory, spriteAtlas, width, height); err == nil {
			return region, true
		}
	}
	fullReport := Report{Kind: ReportKindAtlasFull, Err: fmt.Errorf("canvas: the sprite atlas %q is full at its ceiling of %d pages, with nothing to evict that was not drawn this frame, so image %q is drawn from a texture of its own", label, spriteAtlas.PageCount(), path)}
	reporter.reportOnce(atlasFullLogKey+label, fullReport)
	return render.TextureRegion{}, false
}

// recordUploads records into list what puts the frame's images and glyphs
// where they are placed, before any pass reads them: first each atlas's
// copies, the pages of a growth and the texels put; then the cells of the
// images packed, each image's source drawn premultiplied into its cell
// from its quad in a temporary buffer of their own, its edge continued
// into the gutter, a pass for each run of cells on one page, a draw for
// each cell, an image's own texture's mips generated after its pass; then
// each atlas's mips of the pages packed into. It then releases the
// sources, which the frame's draws still read, and forgets the packings. A
// cell of a texture render could not make is not drawn. With nothing to
// upload, it records nothing.
func (a *atlasSet) recordUploads(list *render.CommandList, inventory render.Inventory) {
	for _, madeAtlas := range a.atlases() {
		if madeAtlas != nil {
			madeAtlas.RecordCopies(list, inventory)
		}
	}
	a.fillPackings(list)
	for _, madeAtlas := range a.atlases() {
		if madeAtlas != nil {
			madeAtlas.RecordMips(list)
		}
	}
	a.releasePackings(inventory)
}

// fillPackings fills the cells of the images packed, as recordUploads
// says, each packing's quad at its index in a temporary buffer of list.
func (a *atlasSet) fillPackings(list *render.CommandList) {
	if len(a.packings) == 0 {
		return
	}
	a.packInstances = a.packInstances[:0]
	for index := range a.packings {
		a.packInstances = append(a.packInstances, a.packings[index].makeInstance())
	}
	instances := list.AcquireTemporaryBuffer(a.packInstances)
	for runStart := 0; runStart < len(a.packings); {
		firstPacked := a.packings[runStart].packed
		target := firstPacked.findTexture()
		runEnd := runStart + 1
		for runEnd < len(a.packings) && a.packings[runEnd].packed.findTexture() == target && a.packings[runEnd].packed.region.Layer == firstPacked.region.Layer {
			runEnd++
		}
		if target != (render.Texture{}) {
			a.colorTargets[0] = render.ColorTarget{Texture: target, Layer: firstPacked.region.Layer}
			pass := list.BeginPass(render.PassDescriptor{Label: "canvas pack", ColorTargets: a.colorTargets[:]})
			for index := runStart; index < runEnd; index++ {
				packing := &a.packings[index]
				cell := packing.packed.findCell()
				pass.SetViewport(render.Viewport{X: float32(cell.X), Y: float32(cell.Y), Width: float32(cell.Width), Height: float32(cell.Height), MaxDepth: 1})
				drawInstances(&pass, a.packMaterial, instances, uint32(index), 1, render.TextureView{Texture: packing.source}, render.TextureView{}, FilterNearest, WrapNone)
			}
			pass.End()
			if firstPacked.atlas == nil {
				list.GenerateMips(target, 0, 1)
			}
		}
		runStart = runEnd
	}
}

// releasePackings releases the source of each image packed whose cell is
// still to fill, and forgets them.
func (a *atlasSet) releasePackings(inventory render.Inventory) {
	for _, packing := range a.packings {
		if packing.source != (render.Texture{}) {
			inventory.ReleaseTexture(packing.source)
		}
	}
	a.packings = a.packings[:0]
}

// startAfresh starts the set afresh once render has recovered a lost
// device, which left every texture canvas made empty under its handle:
// each atlas is emptied, what it had still to record forgotten, the sRGB
// atlas's white texel put again, and each frame and glyph packed left
// unpacked, a cell still to fill forgotten, so each packs again at its next
// draw; an image's own texture is released, as is each pending packing's
// source. A frame drawing nothing, outside its file or converted to no
// texel, and an image of a render texture, whose maker fills it again, are
// kept.
func (a *atlasSet) startAfresh(inventory render.Inventory) {
	a.releasePackings(inventory)
	for _, packed := range a.packedImages {
		packed.free(inventory)
	}
	clear(a.packedImages)
	for _, frames := range a.drawnImages {
		for index := range frames {
			if frames[index].packed != nil {
				frames[index] = drawnImage{}
			}
		}
	}
	clear(a.drawnGlyphs)
	for _, madeAtlas := range a.atlases() {
		if madeAtlas != nil {
			madeAtlas.Empty(inventory)
		}
	}
	if a.srgbAtlas != nil {
		a.packWhiteTexel(inventory)
	}
}

// dropPackedImage forgets packed, the frame of key packed: freed from its
// atlas, or its own texture released through inventory; a cell still to
// fill forgotten, its source released; and each frame drawn of it left
// unpacked, so its next draw packs it again.
func (a *atlasSet) dropPackedImage(inventory render.Inventory, key packedKey, packed *packedImage) {
	delete(a.packedImages, key)
	packed.free(inventory)
	for index, packing := range a.packings {
		if packing.packed == packed {
			if packing.source != (render.Texture{}) {
				inventory.ReleaseTexture(packing.source)
			}
			a.packings = slices.Delete(a.packings, index, index+1)
			break
		}
	}
	for _, frames := range a.drawnImages {
		for index := range frames {
			if frames[index].packed == packed {
				frames[index] = drawnImage{}
			}
		}
	}
}

// imageFrame is a frame of an image, which a sprite or an icon draws.
type imageFrame struct {
	image Image
	frame uint32
}

// evictionCandidate is a frame packed that a full atlas may evict: its key,
// the texels it holds, and where it is packed.
type evictionCandidate struct {
	key    packedKey
	packed *packedImage
}
