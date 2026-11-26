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
// first image needing it; the atlas of each converter and the format it
// answers, made at the first image it converts; the glyph atlas, made at the first frame drawing
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
	// until an image needs it, and glyphAtlas until a run of text does;
	// convertedAtlases are the atlas of each converter and format, in the
	// order made; madeAtlases is every atlas made, in the order made.
	packMaterial     render.Material
	srgbAtlas        *atlas.Atlas
	linearAtlas      *atlas.Atlas
	glyphAtlas       *atlas.Atlas
	convertedAtlases []convertedAtlas
	madeAtlases      []*atlas.Atlas
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
	// first draw, its field put into the glyph atlas then, and again after
	// a full glyph atlas evicted it.
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
	// packInstances their quads, written over as recordUploads fills them;
	// unmippedTextures are the textures of their own render made for images
	// packed, whose mips a command list is still to generate.
	packings         []atlasPacking
	packInstances    []instanceData
	unmippedTextures []render.Texture
	// missedFrames are the frames of images find found not placed this
	// frame, and missedGlyphs the glyphs findGlyphs found not packed, each
	// once, both forgotten as the frame begins; atlasMoved is whether
	// placeMissed grew an atlas, replacing its texture; evictionCandidates are the
	// images a full atlas may evict, in the order it evicts them, written
	// over as they are needed; glyphEvictions are the glyphs the full glyph
	// atlas may evict, each its field's index, the last evicted first,
	// ordered once a frame, when packing a glyph first needs them, which
	// glyphEvictionsOrdered says, and taken as it evicts them.
	missedFrames          []imageFrame
	missedGlyphs          []uint32
	atlasMoved            bool
	evictionCandidates    []evictionCandidate
	glyphEvictions        []uint32
	glyphEvictionsOrdered bool
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
	a.srgbAtlas = a.makeAtlas(inventory, a.makeSpriteAtlasDescriptor(ColorSpaceSrgb))
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

// makeSpriteAtlasDescriptor answers what the plain sprite atlas of
// colorSpace is made of: an atlas of images, as makeImageAtlasDescriptor
// answers, of RGBA8 texels encoded as colorSpace says.
func (a *atlasSet) makeSpriteAtlasDescriptor(colorSpace ColorSpace) atlas.Descriptor {
	if colorSpace == ColorSpaceLinear {
		return a.makeImageAtlasDescriptor(linearAtlasLabel, findRGBA8Format(colorSpace))
	}
	return a.makeImageAtlasDescriptor(srgbAtlasLabel, findRGBA8Format(colorSpace))
}

// makeImageAtlasDescriptor answers what an atlas of images, a plain sprite
// atlas or a converter's, labelled label, is made of: pages of
// atlasPageSize texels, growing to atlasLayerCeiling, of mipLevelCount
// levels, holding texels of format, each cell's gutter the one
// findSpriteAtlasGrid answers, as the plugin fitted the page side to.
func (a *atlasSet) makeImageAtlasDescriptor(label string, format render.TextureFormat) atlas.Descriptor {
	_, gutter := findSpriteAtlasGrid(a.mipLevelCount)
	return atlas.Descriptor{
		Label:         label,
		Format:        format,
		PageSize:      a.atlasPageSize,
		MipLevelCount: a.mipLevelCount,
		Gutter:        gutter,
		LayerCeiling:  a.atlasLayerCeiling,
	}
}

// makeAtlas answers an atlas of descriptor made through inventory, kept
// among the atlases made.
func (a *atlasSet) makeAtlas(inventory render.Inventory, descriptor atlas.Descriptor) *atlas.Atlas {
	made := atlas.New(inventory, descriptor)
	a.madeAtlases = append(a.madeAtlases, made)
	return made
}

// findConvertedAtlas answers the atlas of the images converter converts
// into texels of format, made through inventory at the first of them, and
// its label.
func (a *atlasSet) findConvertedAtlas(inventory render.Inventory, converter Converter, format render.TextureFormat) (*atlas.Atlas, string) {
	for _, converted := range a.convertedAtlases {
		if converted.converter == converter && converted.format == format {
			return converted.atlas, converted.label
		}
	}
	label := fmt.Sprintf("canvas sprites converted by %#v, of format %d", converter, format)
	made := a.makeAtlas(inventory, a.makeImageAtlasDescriptor(label, format))
	a.convertedAtlases = append(a.convertedAtlases, convertedAtlas{converter: converter, format: format, atlas: made, label: label})
	return made, label
}

// beginFrame begins a frame: it counts it, so what it draws is marked
// drawn in it, and forgets what the frame before missed.
func (a *atlasSet) beginFrame() {
	a.drawnFrameCount++
	a.missedFrames = a.missedFrames[:0]
	a.missedGlyphs = a.missedGlyphs[:0]
	a.glyphEvictionsOrdered = false
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
// field at its field's index in the FontLibrary's glyph table: it marks each
// drawn this frame, so the glyph atlas evicts none of them in it, and notes
// one missed when it is not packed, once a frame, so placeMissed tries it
// once. The glyph atlas, which the run is keyed by even when it draws icons
// alone, is made at the first run found.
func (a *atlasSet) findGlyphs(inventory render.Inventory, glyphs []placedGlyph) {
	if a.glyphAtlas == nil {
		a.glyphAtlas = a.makeAtlas(inventory, atlas.Descriptor{
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
		drawn := &a.drawnGlyphs[glyph.field]
		if drawn.lastDrawnFrame == a.drawnFrameCount {
			continue
		}
		drawn.lastDrawnFrame = a.drawnFrameCount
		if !drawn.packed {
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
// it; else on a new one, the atlas doubling its pages while under its
// ceiling; else where the glyphs it evicts leave room, the least lately
// drawn first, those last drawn in one frame in their fields' order, the
// one made first going first, never one drawn this frame. A glyph whose
// cell no page holds, or one the atlas at its ceiling has no room for with
// nothing to evict, draws nothing, reported once, and is tried again at the
// next frame drawing it, findGlyphs noting it once a frame.
func (a *atlasSet) packGlyph(inventory render.Inventory, reporter *reporter, drawn *drawnGlyph, field glyphField) {
	width := uint32(field.Width)
	height := uint32(field.Height)
	region, err := a.pack(inventory, a.glyphAtlas, width, height)
	if errors.Is(err, atlas.ErrFull) {
		region, err = a.evictGlyphsFor(inventory, width, height)
	}
	if err != nil {
		reason := "is full at its ceiling, with nothing to evict that was not drawn this frame"
		if errors.Is(err, atlas.ErrTooLarge) {
			reason = "holds no page its cell fits on"
		}
		fullReport := Report{Kind: ReportKindAtlasFull, Err: fmt.Errorf("canvas: the glyph atlas, of %d pages of %d texels a side, %s, so a glyph's field of %d by %d texels draws nothing", a.glyphAtlas.PageCount(), a.atlasPageSize, reason, width, height)}
		reporter.reportOnce(atlasFullLogKey+glyphAtlasLabel, fullReport)
		return
	}
	a.glyphAtlas.Put(region, field.Texels, atlas.WrapNone)
	*drawn = makeDrawnGlyph(a.glyphAtlas, region, field, a.drawnFrameCount)
}

// evictGlyphsFor evicts glyphs from the full glyph atlas, in the order
// packGlyph says, until a cell of width by height texels is packed, and
// answers where, or ErrFull when evicting every glyph not drawn this frame
// leaves no room. Each glyph evicted is left unpacked, its cell freed, so
// its next draw packs it again.
func (a *atlasSet) evictGlyphsFor(inventory render.Inventory, width, height uint32) (render.TextureRegion, error) {
	if !a.glyphEvictionsOrdered {
		a.orderGlyphEvictions()
	}
	for len(a.glyphEvictions) > 0 {
		last := len(a.glyphEvictions) - 1
		evicted := &a.drawnGlyphs[a.glyphEvictions[last]]
		a.glyphEvictions = a.glyphEvictions[:last]
		a.glyphAtlas.Free(evicted.packedRegion)
		*evicted = drawnGlyph{}
		if region, err := a.pack(inventory, a.glyphAtlas, width, height); err == nil {
			return region, nil
		}
	}
	return render.TextureRegion{}, atlas.ErrFull
}

// orderGlyphEvictions orders the glyphs packed and not drawn this frame
// into glyphEvictions, the last to evict first: the most lately drawn, then
// of those last drawn in one frame the one made last.
func (a *atlasSet) orderGlyphEvictions() {
	a.glyphEvictions = a.glyphEvictions[:0]
	for field := range a.drawnGlyphs {
		if drawn := &a.drawnGlyphs[field]; drawn.packed && drawn.lastDrawnFrame != a.drawnFrameCount {
			a.glyphEvictions = append(a.glyphEvictions, uint32(field))
		}
	}
	slices.SortFunc(a.glyphEvictions, func(first, second uint32) int {
		return cmp.Or(cmp.Compare(a.drawnGlyphs[second].lastDrawnFrame, a.drawnGlyphs[first].lastDrawnFrame), cmp.Compare(second, first))
	})
	a.glyphEvictionsOrdered = true
}

// packImage packs frame of the image of key, marked drawn this frame, and
// answers where, and whether it draws texels: false for a frame whose
// rectangle does not lie within its file, which is not packed and is
// reported once for its path, and for a converted frame whose
// converter answered no texel; and whether it packed the frame now, not one
// sharing the cell of one packed before. A converted image's file is read
// from files and decoded, as decodeConvertedFile does, and its frame packed
// as packConverted packs it; any other's, or one that could not be decoded,
// which render draws magenta, is acquired from render with its colour
// space, sharing the texture Assets.Preload acquired, which it then
// releases from requests, of the size render's descriptor of it gives, its
// file's header's, a texture render could not make one texel, and its
// frame packed as packPlain packs it. A frame of the texels of one packed
// already, of the same rectangle of the file, converter and wrap, shares
// its cell, its source released.
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
	packed = &packedImage{lastDrawnFrame: a.drawnFrameCount}
	if converter == nil {
		a.packPlain(inventory, reporter, key, packed, source, frameRect)
	} else if !a.packConverted(inventory, reporter, key, packed, picture, frameRect) {
		return nil, false, false
	}
	a.packedImages[packedKey] = packed
	return packed, true, true
}

// packPlain packs into packed the frame at frameRect of source, the
// texture of key's file: into the plain sprite atlas of its colour space,
// as packIntoImageAtlas packs it, or into a texture of its own, as
// makeOwnTexture makes it; its cell kept for the next command list to fill.
func (a *atlasSet) packPlain(inventory render.Inventory, reporter *reporter, key imageKey, packed *packedImage, source render.Texture, frameRect m.Recti) {
	descriptor := a.makeSpriteAtlasDescriptor(key.options.ColorSpace)
	spriteAtlas := a.srgbAtlas
	if key.options.ColorSpace == ColorSpaceLinear {
		if a.linearAtlas == nil {
			a.linearAtlas = a.makeAtlas(inventory, descriptor)
		}
		spriteAtlas = a.linearAtlas
	}
	width := uint32(frameRect.Width)
	height := uint32(frameRect.Height)
	if region, fits := a.packIntoImageAtlas(inventory, reporter, spriteAtlas, descriptor.Label, key.path, width, height); fits {
		packed.atlas = spriteAtlas
		packed.region = region
	} else {
		a.makeOwnTexture(inventory, packed, key.path, width, height, descriptor.Format, nil)
	}
	a.packings = append(a.packings, atlasPacking{packed: packed, source: source, frameRect: frameRect, wrap: key.options.Wrap})
}

// packConverted packs into packed the frame at frameRect of picture, the
// file of key decoded, converted by key's converter, and answers whether
// its output holds any texel: into the atlas of its converter and the
// format it answers for key's colour space, as packIntoImageAtlas packs
// it, its texels put there with the image's wrap, or into a texture of its
// own made of them, as makeOwnTexture makes it. Its frame's bounds within
// the output place it.
func (a *atlasSet) packConverted(inventory render.Inventory, reporter *reporter, key imageKey, packed *packedImage, picture image.Image, frameRect m.Recti) bool {
	converter := key.options.Convert
	format := converter.Format(key.options.ColorSpace)
	texels, bounds := convertFrame(picture, frameRect, converter, key.options.ColorSpace)
	if bounds.Empty() {
		return false
	}
	packed.converted = true
	packed.frameBounds = m.Recti{X: -bounds.Min.X, Y: -bounds.Min.Y, Width: frameRect.Width, Height: frameRect.Height}.Float()
	width := uint32(bounds.Dx())
	height := uint32(bounds.Dy())
	// An image a texture of its own holds makes no atlas.
	if width <= a.ownTextureSize && height <= a.ownTextureSize {
		convertedAtlas, label := a.findConvertedAtlas(inventory, converter, format)
		if region, fits := a.packIntoImageAtlas(inventory, reporter, convertedAtlas, label, key.path, width, height); fits {
			packed.atlas = convertedAtlas
			packed.region = region
			convertedAtlas.Put(region, texels, atlas.Wrap(key.options.Wrap))
			return true
		}
	}
	a.makeOwnTexture(inventory, packed, key.path, width, height, format, texels)
	return true
}

// makeOwnTexture makes packed's texture of its own, of an image at path of
// width by height texels of format, filled with texels, none leaving it
// for a pack pass to fill: exactly its size, of no gutter, so an image as
// large as a page fills a texture of a page's size, of the pages' mip
// levels or as many as its size has, left for the next command list to
// generate once filled.
func (a *atlasSet) makeOwnTexture(inventory render.Inventory, packed *packedImage, path string, width, height uint32, format render.TextureFormat, texels []byte) {
	packed.region = render.TextureRegion{Width: width, Height: height}
	packed.ownTexture = inventory.MakeTexture(render.TextureDescriptor{
		Label:         path,
		Width:         width,
		Height:        height,
		LayerCount:    1,
		MipLevelCount: min(a.mipLevelCount, uint32(bits.Len32(max(width, height)))),
		Format:        format,
	}, texels)
	if packed.ownTexture != (render.Texture{}) {
		a.unmippedTextures = append(a.unmippedTextures, packed.ownTexture)
	}
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

// packIntoImageAtlas packs an image of width by height texels, at path,
// into imageAtlas, labelled label, and answers where, or false when it
// belongs in a texture of its own: on the first page holding it; else on a
// new page, the atlas doubling its pages while under its ceiling; else
// where the images it evicts leave room, the least lately drawn first,
// never one drawn this frame. An image wider or taller than
// ownTextureSize, and one whose cell no page holds, are not packed, and an
// atlas at its ceiling with nothing to evict is reported, once for the
// atlas.
func (a *atlasSet) packIntoImageAtlas(inventory render.Inventory, reporter *reporter, imageAtlas *atlas.Atlas, label, path string, width, height uint32) (render.TextureRegion, bool) {
	if width > a.ownTextureSize || height > a.ownTextureSize {
		return render.TextureRegion{}, false
	}
	region, err := a.pack(inventory, imageAtlas, width, height)
	if !errors.Is(err, atlas.ErrFull) {
		return region, err == nil
	}
	a.evictionCandidates = a.evictionCandidates[:0]
	for key, candidate := range a.packedImages {
		if candidate.atlas == imageAtlas && candidate.lastDrawnFrame != a.drawnFrameCount {
			a.evictionCandidates = append(a.evictionCandidates, evictionCandidate{key: key, packed: candidate})
		}
	}
	// Those last drawn in one frame go in their paths' order, then their
	// rectangles' rows and columns.
	slices.SortFunc(a.evictionCandidates, func(first, second evictionCandidate) int {
		return cmp.Or(cmp.Compare(first.packed.lastDrawnFrame, second.packed.lastDrawnFrame), strings.Compare(first.key.source.path, second.key.source.path), cmp.Compare(first.key.rect.Y, second.key.rect.Y), cmp.Compare(first.key.rect.X, second.key.rect.X))
	})
	for _, candidate := range a.evictionCandidates {
		a.dropPackedImage(inventory, candidate.key, candidate.packed)
		if region, err := a.pack(inventory, imageAtlas, width, height); err == nil {
			return region, true
		}
	}
	fullReport := Report{Kind: ReportKindAtlasFull, Err: fmt.Errorf("canvas: the sprite atlas %q is full at its ceiling of %d pages, with nothing to evict that was not drawn this frame, so image %q is drawn from a texture of its own", label, imageAtlas.PageCount(), path)}
	reporter.reportOnce(atlasFullLogKey+label, fullReport)
	return render.TextureRegion{}, false
}

// recordUploads records into list what puts the frame's images and glyphs
// where they are placed, before any pass reads them: first each atlas's
// copies, the pages of a growth and the texels put; then the cells of the
// plain images packed, each image's source drawn premultiplied into its
// cell from its quad in a temporary buffer of their own, its edge
// continued into the gutter, a pass for each run of cells on one page, a
// draw for each cell; then the mips of each image's own texture, filled;
// then each atlas's mips of the pages packed into. It then releases the
// sources, which the frame's draws still read, and forgets the packings. A
// cell of a texture render could not make is not drawn. With nothing to
// upload, it records nothing.
func (a *atlasSet) recordUploads(list *render.CommandList, inventory render.Inventory) {
	for _, madeAtlas := range a.madeAtlases {
		madeAtlas.RecordCopies(list, inventory)
	}
	a.fillPackings(list)
	for _, texture := range a.unmippedTextures {
		list.GenerateMips(texture, 0, 1)
	}
	a.unmippedTextures = a.unmippedTextures[:0]
	for _, madeAtlas := range a.madeAtlases {
		madeAtlas.RecordMips(list)
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
// source, its mips still to generate forgotten. A frame drawing nothing,
// outside its file or converted to no texel, and an image of a render
// texture, whose maker fills it again, are kept.
func (a *atlasSet) startAfresh(inventory render.Inventory) {
	a.releasePackings(inventory)
	a.unmippedTextures = a.unmippedTextures[:0]
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
	for _, madeAtlas := range a.madeAtlases {
		madeAtlas.Empty(inventory)
	}
	if a.srgbAtlas != nil {
		a.packWhiteTexel(inventory)
	}
}

// dropPackedImage forgets packed, the frame of key packed: freed from its
// atlas, or its own texture released through inventory, its mips still to
// generate forgotten; a cell still to fill forgotten, its source released;
// and each frame drawn of it left unpacked, so its next draw packs it
// again.
func (a *atlasSet) dropPackedImage(inventory render.Inventory, key packedKey, packed *packedImage) {
	delete(a.packedImages, key)
	a.unmippedTextures = slices.DeleteFunc(a.unmippedTextures, func(texture render.Texture) bool { return texture == packed.ownTexture })
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

// convertedAtlas is the atlas of the images a converter converts into
// texels of a format, and its label.
type convertedAtlas struct {
	converter Converter
	format    render.TextureFormat
	atlas     *atlas.Atlas
	label     string
}

// evictionCandidate is a frame packed that a full atlas may evict: its key,
// the texels it holds, and where it is packed.
type evictionCandidate struct {
	key    packedKey
	packed *packedImage
}
