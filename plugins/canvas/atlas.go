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
	// it, which it logged.
	texture       render.Texture
	format        render.TextureFormat
	width, height uint32
	// gutter is how many texels each cell reaches past its image: a sprite
	// atlas's atlasGutter, and none for an atlas of one image's own, whose
	// sampler clamps at the texture's edge as a gutter would continue it.
	gutter uint32
	pages  []shelfAllocator
}

// newAtlas answers an atlas of format, of one page width by height texels,
// its cells reaching gutter texels past their images, labelled label, its
// texture made through inventory, transparent black.
func newAtlas(inventory render.Inventory, label string, format render.TextureFormat, width, height, gutter uint32) *atlas {
	return &atlas{
		texture: inventory.MakeTexture[byte](render.TextureDescriptor{Label: label, Width: width, Height: height, Format: format}, nil),
		format:  format,
		width:   width,
		height:  height,
		gutter:  gutter,
		pages:   []shelfAllocator{makeShelfAllocator(int32(width), int32(height))},
	}
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
