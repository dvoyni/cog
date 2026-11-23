package canvas

import (
	"fmt"
	"math/bits"

	"github.com/dvoyni/cog/plugins/render"
)

// A sprite atlas's gutters and mip levels are bounded by these.
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

// findSpriteAtlasGrid answers the atlas grid the cells of a sprite atlas whose pages
// have mipLevelCount levels sit on, 2^(mipLevelCount−1) texels, and the
// gutter each cell reaches past its image, the atlas grid but no fewer than
// smallestAtlasGutter texels: every texel of every level then lies within
// one cell, so a page's mips mix no images, and linear filtering at its
// deepest level reads only an image's own texels and its gutter.
func findSpriteAtlasGrid(mipLevelCount uint32) (atlasGrid, gutter uint32) {
	atlasGrid = 1 << (mipLevelCount - 1)
	return atlasGrid, max(atlasGrid, smallestAtlasGutter)
}

// roundUpToMultiple answers the smallest multiple of step no smaller than
// value.
func roundUpToMultiple(value, step uint32) uint32 {
	return (value + step - 1) / step * step
}

// atlas is images of one format packed into pages: one 2D-array texture, a
// layer for each page, so entries over any of its images can share a
// batch. Each image is packed as a cell of its own, its gutter around it,
// taken from its page by the page's shelf allocator. The glyph atlas packs
// glyphs' distance fields the same way.
type atlas struct {
	// texture holds the pages, the zero Texture when render could not make
	// it, which it logged. Growing replaces it.
	texture       render.Texture
	label         string
	format        render.TextureFormat
	width, height uint32
	// mipLevelCount is how many mip levels its pages have.
	mipLevelCount uint32
	// pages are a sprite atlas's or the glyph atlas's, none for an atlas of
	// one image's own, which holdsOneImage marks; holdsGlyphs marks the
	// glyph atlas.
	pages         []shelfAllocator
	holdsOneImage bool
	holdsGlyphs   bool
}

// newAtlas answers a sprite atlas of format, of one page width by height
// texels, each a multiple of its atlas grid, of mipLevelCount levels, its cells
// on the atlas grid and reaching the gutter findSpriteAtlasGrid answers, labelled
// label, its texture made through inventory, transparent black. A page side
// that is no multiple of the atlas grid panics.
func newAtlas(inventory render.Inventory, label string, format render.TextureFormat, width, height, mipLevelCount uint32) *atlas {
	if atlasGrid, _ := findSpriteAtlasGrid(mipLevelCount); width%atlasGrid != 0 || height%atlasGrid != 0 {
		panic(fmt.Sprintf("canvas: an atlas's page of %d by %d texels is no multiple of its atlas grid of %d", width, height, atlasGrid))
	}
	created := &atlas{label: label, format: format, width: width, height: height, mipLevelCount: mipLevelCount}
	created.texture = created.makeTexture(inventory, 1)
	created.pages = []shelfAllocator{makeShelfAllocator(int32(width), int32(height))}
	return created
}

// newOwnAtlas answers an atlas of one image's own, of format, exactly its
// width by height texels with no gutter, of mipLevelCount levels or as
// many as its size has, labelled label, its texture made through
// inventory, transparent black, and the region the image lies in: the
// whole of its one page.
func newOwnAtlas(inventory render.Inventory, label string, format render.TextureFormat, width, height, mipLevelCount uint32) (*atlas, render.TextureRegion) {
	ownMipLevelCount := min(mipLevelCount, uint32(bits.Len32(max(width, height))))
	own := &atlas{label: label, format: format, width: width, height: height, mipLevelCount: ownMipLevelCount, holdsOneImage: true}
	own.texture = own.makeTexture(inventory, 1)
	return own, render.TextureRegion{Width: width, Height: height}
}

// newGlyphAtlas answers the glyph atlas, of glyphs' distance fields: R8Unorm,
// of one page width by height texels of one mip level, its cells on no
// atlas grid and reaching glyphAtlasGutter past their glyphs, its texture
// made through inventory, transparent.
func newGlyphAtlas(inventory render.Inventory, width, height uint32) *atlas {
	created := &atlas{label: "canvas glyphs", format: render.TextureFormatR8Unorm, width: width, height: height, mipLevelCount: 1, holdsGlyphs: true}
	created.texture = created.makeTexture(inventory, 1)
	created.pages = []shelfAllocator{makeShelfAllocator(int32(width), int32(height))}
	return created
}

// findAtlasGrid answers what each cell's image is rounded up to, so every cell
// sits on it, and how many texels each cell reaches past that: a sprite
// atlas's, as findSpriteAtlasGrid answers them; an atlas grid of 1 and no gutter
// for an atlas of one image's own, whose sampler clamps at the texture's
// edge as a gutter would continue it, and whose mips are the whole
// texture's; and an atlas grid of 1 and glyphAtlasGutter for the glyph
// atlas, of one mip level.
func (a *atlas) findAtlasGrid() (atlasGrid, gutter uint32) {
	switch {
	case a.holdsOneImage:
		return 1, 0
	case a.holdsGlyphs:
		return 1, glyphAtlasGutter
	}
	return findSpriteAtlasGrid(a.mipLevelCount)
}

// findCellSize answers the size of the cell an image of width by height
// texels is packed as: the image rounded up to the atlas grid, then the gutter on
// every side.
func (a *atlas) findCellSize(width, height uint32) (uint32, uint32) {
	atlasGrid, gutter := a.findAtlasGrid()
	return roundUpToMultiple(width, atlasGrid) + 2*gutter, roundUpToMultiple(height, atlasGrid) + 2*gutter
}

// findCell answers the cell of the image that lies at region, which pack
// answered region for: on its page, from the gutter above and left of it.
func (a *atlas) findCell(region render.TextureRegion) render.TextureRegion {
	_, gutter := a.findAtlasGrid()
	width, height := a.findCellSize(region.Width, region.Height)
	return render.TextureRegion{Layer: region.Layer, X: region.X - gutter, Y: region.Y - gutter, Width: width, Height: height}
}

// pack answers where an image of width by height texels lies once packed
// as a cell of its own, on the first page holding one, or false when none
// does: its page as the region's layer, and its rectangle there in texels,
// the gutter in from its cell's top left, on the atlas grid.
func (a *atlas) pack(width, height uint32) (render.TextureRegion, bool) {
	_, gutter := a.findAtlasGrid()
	cellWidth, cellHeight := a.findCellSize(width, height)
	for page := range a.pages {
		x, y, ok := a.pages[page].allocate(int32(cellWidth), int32(cellHeight))
		if ok {
			return render.TextureRegion{Layer: uint32(page), X: uint32(x) + gutter, Y: uint32(y) + gutter, Width: width, Height: height}, true
		}
	}
	return render.TextureRegion{}, false
}

// free gives back the image that lies at region: its cell, which pack
// answered region for, so another image can be packed there, or, for an
// atlas of one image's own, its texture, released through inventory.
func (a *atlas) free(inventory render.Inventory, region render.TextureRegion) {
	switch {
	case !a.holdsOneImage:
		cell := a.findCell(region)
		a.pages[region.Layer].free(int32(cell.X), int32(cell.Y))
	case a.texture != (render.Texture{}):
		inventory.ReleaseTexture(a.texture)
	}
}

// empty frees every cell of the atlas's pages, as a lost device empties
// its texture, keeping as many pages.
func (a *atlas) empty() {
	for page := range a.pages {
		a.pages[page] = makeShelfAllocator(int32(a.width), int32(a.height))
	}
}

// grow doubles the atlas's pages, to no more than layerCeiling, the new
// ones empty, into a new texture made through inventory, and answers the
// copy of the old texture's pages into it, every mip level, which a
// command list is still to record before anything reads the new one.
func (a *atlas) grow(inventory render.Inventory, layerCeiling uint32) atlasGrowth {
	growth := atlasGrowth{source: a.texture, layerCount: uint32(len(a.pages)), mipLevelCount: a.mipLevelCount}
	layerCount := min(2*growth.layerCount, layerCeiling)
	a.texture = a.makeTexture(inventory, layerCount)
	growth.destination = a.texture
	for len(a.pages) < int(layerCount) {
		a.pages = append(a.pages, makeShelfAllocator(int32(a.width), int32(a.height)))
	}
	return growth
}

// makeTexture makes, through inventory, the atlas's texture of layerCount
// pages, of its mip levels, transparent black.
func (a *atlas) makeTexture(inventory render.Inventory, layerCount uint32) render.Texture {
	return inventory.MakeTexture[byte](render.TextureDescriptor{Label: a.label, Width: a.width, Height: a.height, LayerCount: layerCount, MipLevelCount: a.mipLevelCount, Format: a.format}, nil)
}

// atlasGrowth is an atlas grown whose pages are still to be copied into
// its new texture: the texture it had, its pages from the first and their
// mip levels, and the texture that replaced it.
type atlasGrowth struct {
	source        render.Texture
	layerCount    uint32
	mipLevelCount uint32
	destination   render.Texture
}
