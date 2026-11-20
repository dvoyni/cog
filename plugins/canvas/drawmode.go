package canvas

// DrawMode is how a sprite fills its rectangle with its image. A sprite
// has one, so it is never sliced and tiled at once.
type DrawMode uint8

const (
	// Stretched stretches the image over the sprite's rectangle.
	Stretched DrawMode = iota
	// Sliced cuts the image into nine by its ImageOptions.Slice insets and
	// keeps its corners at their texels' size, times the sprite's
	// TexelScale, stretching its edges between them along one axis and its
	// centre along both, so a panel of any size keeps its frame. It is one
	// instance, remapped as it is sampled, so it turns and mirrors whole.
	// Corners larger than the sprite are shrunk, keeping their ratio, until
	// they meet. ImageOptions.SliceHollow leaves the centre empty.
	Sliced
	// Tiled repeats the image from the sprite's top left corner, each tile
	// its texels' size times the sprite's TexelScale. An image that wraps
	// along an axis, by its ImageOptions.Wrap, tiles along it without
	// seams; along any other, each tile's edge meets the next unblended.
	Tiled
)
