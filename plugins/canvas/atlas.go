package canvas

import "github.com/dvoyni/cog/plugins/render"

// atlasGutter is how many texels a cell of a sprite atlas reaches past its
// image on every side, holding the image's edge continued, so linear
// filtering at its edge never reads a neighbour: a page of one mip level's.
const atlasGutter = 2

// atlas is images of one format packed into pages: one 2D-array texture, a
// layer for each page, so entries over any of its images can share a
// batch. Each image is packed as a cell of its own, its gutter around it,
// taken from its page by the page's shelf allocator.
type atlas struct {
	// texture holds the pages, the zero Texture when render could not make
	// it, which it logged. Growing replaces it.
	texture       render.Texture
	label         string
	format        render.TextureFormat
	width, height uint32
	// gutter is how many texels each cell reaches past its image: a sprite
	// atlas's atlasGutter, and none for an atlas of one image's own, whose
	// sampler clamps at the texture's edge as a gutter would continue it.
	gutter uint32
	// pages are a sprite atlas's, none for an atlas of one image's own,
	// which holdsOneImage marks.
	pages         []shelfAllocator
	holdsOneImage bool
}

// newAtlas answers an atlas of format, of one page width by height texels,
// its cells reaching gutter texels past their images, labelled label, its
// texture made through inventory, transparent black.
func newAtlas(inventory render.Inventory, label string, format render.TextureFormat, width, height, gutter uint32) *atlas {
	created := &atlas{label: label, format: format, width: width, height: height, gutter: gutter}
	created.texture = created.makeTexture(inventory, 1)
	created.pages = []shelfAllocator{makeShelfAllocator(int32(width), int32(height))}
	return created
}

// newOwnAtlas answers an atlas of one image's own, of format, exactly its
// width by height texels with no gutter, labelled label, its texture made
// through inventory, transparent black, and the region the image lies in:
// the whole of its one page.
func newOwnAtlas(inventory render.Inventory, label string, format render.TextureFormat, width, height uint32) (*atlas, render.TextureRegion) {
	own := &atlas{label: label, format: format, width: width, height: height, holdsOneImage: true}
	own.texture = own.makeTexture(inventory, 1)
	return own, render.TextureRegion{Width: width, Height: height}
}

// pack answers where an image of width by height texels lies once packed
// as a cell of its own, on the first page holding one, or false when none
// does: its page as the region's layer, and its rectangle there in texels,
// its cell reaching the atlas's gutter past it.
func (a *atlas) pack(width, height uint32) (render.TextureRegion, bool) {
	for page := range a.pages {
		x, y, ok := a.pages[page].allocate(int32(width+2*a.gutter), int32(height+2*a.gutter))
		if ok {
			return render.TextureRegion{Layer: uint32(page), X: uint32(x) + a.gutter, Y: uint32(y) + a.gutter, Width: width, Height: height}, true
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
		a.pages[region.Layer].free(int32(region.X-a.gutter), int32(region.Y-a.gutter))
	case a.texture != (render.Texture{}):
		inventory.ReleaseTexture(a.texture)
	}
}

// grow doubles the atlas's pages, to no more than layerCeiling, the new
// ones empty, into a new texture made through inventory, and answers the
// copy of the old texture's pages into it, which a command list is still
// to record before anything reads the new one.
func (a *atlas) grow(inventory render.Inventory, layerCeiling uint32) atlasGrowth {
	growth := atlasGrowth{source: a.texture, layerCount: uint32(len(a.pages))}
	layerCount := min(2*growth.layerCount, layerCeiling)
	a.texture = a.makeTexture(inventory, layerCount)
	growth.destination = a.texture
	for len(a.pages) < int(layerCount) {
		a.pages = append(a.pages, makeShelfAllocator(int32(a.width), int32(a.height)))
	}
	return growth
}

// makeTexture makes, through inventory, the atlas's texture of layerCount
// pages, transparent black.
func (a *atlas) makeTexture(inventory render.Inventory, layerCount uint32) render.Texture {
	return inventory.MakeTexture[byte](render.TextureDescriptor{Label: a.label, Width: a.width, Height: a.height, LayerCount: layerCount, Format: a.format}, nil)
}

// atlasGrowth is an atlas grown whose pages are still to be copied into
// its new texture: the texture it had, its pages from the first, and the
// texture that replaced it.
type atlasGrowth struct {
	source      render.Texture
	layerCount  uint32
	destination render.Texture
}
