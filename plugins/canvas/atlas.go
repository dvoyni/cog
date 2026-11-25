package canvas

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

// roundUpToMultiple answers the smallest multiple of step no smaller than
// value.
func roundUpToMultiple(value, step uint32) uint32 {
	return (value + step - 1) / step * step
}
