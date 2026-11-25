package atlas

// Wrap is how Put fills a cell's gutter along each axis: from the opposite
// edge along an axis the image tiles along, so it tiles without a seam, and
// its edge continued along any other.
type Wrap uint8

const (
	// WrapNone continues the image's edge along both axes.
	WrapNone Wrap = iota
	// WrapX wraps along x, WrapY along y and WrapBoth along both.
	WrapX
	WrapY
	WrapBoth
)
