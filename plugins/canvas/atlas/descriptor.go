package atlas

import "github.com/dvoyni/cog/plugins/render"

// Descriptor is what an atlas is made of, which New checks.
type Descriptor struct {
	// Label names its texture.
	Label string
	// Format is its texels': R8Unorm, RG8Unorm, RGBA8Unorm, RGBA8UnormSrgb,
	// BGRA8Unorm or BGRA8UnormSrgb. Any other panics.
	Format render.TextureFormat
	// PageSize is each page's side in texels, a multiple of the atlas grid,
	// 2^(MipLevelCount−1), or New panics.
	PageSize uint32
	// MipLevelCount is how many mip levels its pages have, zero meaning one.
	MipLevelCount uint32
	// Gutter is the fewest texels a cell reaches past its image on every
	// side. At more than one mip level it is raised to a multiple of the
	// atlas grid, and the atlas grid at least, so every cell lies on it and
	// linear filtering at the deepest level reads only a cell's own texels.
	Gutter uint32
	// ClearGutter makes Put leave the gutter transparent black, its wrap
	// ignored: for images whose edge texels must not be continued, such as
	// a glyph's coverage.
	ClearGutter bool
	// LayerCeiling is the most pages it grows to, zero meaning one.
	LayerCeiling uint32
}
