package canvas

import "github.com/dvoyni/cog/libs/m"

// ImageOptions are what an image is loaded and placed with. Images of one
// path that differ only in options that change no pixel, such as the
// pivot, share their texture.
type ImageOptions struct {
	// ColorSpace is how the file encodes its colours.
	ColorSpace ColorSpace
	// Pivot is the point of the image its entries are placed, turned and
	// scaled about; zero is its centre.
	Pivot Pivot
}

// ColorSpace is how an image's file encodes its colours.
type ColorSpace uint8

const (
	// ColorSpaceSrgb is colour encoded in sRGB, as most pictures are: it is
	// decoded to linear colour as it is sampled.
	ColorSpaceSrgb ColorSpace = iota
	// ColorSpaceLinear is data or linear colour, sampled as it is.
	ColorSpaceLinear
)

// Pivot is a point of an image: Fraction of its size from its centre, ±½ at
// its edges, plus Texels, in the image's own space, x right and y down as
// its rows run. The zero Pivot is the centre. "Bottom centre, 3 texels up"
// is Pivot{Fraction: m.Vec2{Y: 0.5}, Texels: m.Vec2{Y: -3}}.
type Pivot struct {
	Fraction m.Vec2
	Texels   m.Vec2
}

// The named pivots: an image's centre, its top left corner, the middle of
// its top edge, the middle of its bottom edge, and its bottom corners.
var (
	PivotCenter      = Pivot{}
	PivotTopLeft     = Pivot{Fraction: m.Vec2{X: -0.5, Y: -0.5}}
	PivotTop         = Pivot{Fraction: m.Vec2{Y: -0.5}}
	PivotBottom      = Pivot{Fraction: m.Vec2{Y: 0.5}}
	PivotBottomLeft  = Pivot{Fraction: m.Vec2{X: -0.5, Y: 0.5}}
	PivotBottomRight = Pivot{Fraction: m.Vec2{X: 0.5, Y: 0.5}}
)

// ImageFromFile interns the image at path in platform's FS, with options, into
// an Image: every call of the same path and options answers the same one.
// It loads nothing: a sprite's first draw of it loads it. Each call takes
// the table's lock and hashes path, so a game makes an Image once, as it
// names its art, and keeps it, rather than calling ImageFromFile for each entry
// it draws. It takes at most
// one set of options, so the common case reads ImageFromFile("water.png"); a
// second set is ignored, and reported once, as
// ReportKindImageOptionsIgnored, when the Image answered is first drawn.
func ImageFromFile(path string, options ...ImageOptions) Image {
	key := imageKey{path: path}
	if len(options) > 0 {
		key.options = options[0]
	}
	key.ignoredOptions = len(options) > 1
	return imageNames.intern(key)
}
