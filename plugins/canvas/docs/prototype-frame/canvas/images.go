package canvas

import (
	"image"

	"github.com/dvoyni/cog/libs/m"
	"prototype.invalid/canvasframe/render"
)

// ImageOptions are what an image is drawn with. Options that change no pixel,
// the pivot and the slice insets, are not packed, so images differing only in
// them share their regions.
type ImageOptions struct {
	ColorSpace ColorSpace
	// Wrap is the axes the image tiles along, whose gutters wrap.
	Wrap Wrap
	// Sheet is a sheet's grid of frames, each packed as a region of its own;
	// zero is one frame.
	Sheet Grid
	// Pivot is the point the image's entries are placed, turned and scaled
	// about; zero is its centre.
	Pivot Pivot
	// SourceRect is the rectangle of the file the image is, in texels, zero
	// meaning all of it, packed as a region of its own.
	SourceRect m.Rect
	// Slice is a sliced sprite's nine-slice insets, in texels; SliceHollow
	// draws nothing in the centre cell.
	Slice       Insets
	SliceHollow bool
	// Convert, a comparable value, converts the image on the CPU before it is
	// packed, such as DistanceField.
	Convert Converter
}

// ColorSpace is the colour space an image's texels are in.
type ColorSpace uint8

const (
	ColorSpaceSrgb ColorSpace = iota
	ColorSpaceLinear
)

// Wrap is the axes an image tiles along.
type Wrap uint8

const (
	WrapNone Wrap = iota
	WrapX
	WrapY
	WrapBoth
)

// Grid is a sheet's columns and rows.
type Grid struct{ Columns, Rows uint32 }

// Insets are distances in from each edge.
type Insets struct{ Left, Top, Right, Bottom float32 }

// Pivot is a point of an image, a layout's box or a camera's viewport:
// Fraction of its size from its centre, ±½ at its edges, plus Texels. The zero
// Pivot is the centre. "Bottom centre, 3 texels up" is
// Pivot{Fraction: m.Vec2{Y: 0.5}, Texels: m.Vec2{Y: -3}}.
type Pivot struct{ Fraction, Texels m.Vec2 }

// The named pivots.
var (
	PivotCenter      = Pivot{}
	PivotTopLeft     = Pivot{Fraction: m.Vec2{X: -0.5, Y: -0.5}}
	PivotTop         = Pivot{Fraction: m.Vec2{Y: -0.5}}
	PivotBottom      = Pivot{Fraction: m.Vec2{Y: 0.5}}
	PivotBottomLeft  = Pivot{Fraction: m.Vec2{X: -0.5, Y: 0.5}}
	PivotBottomRight = Pivot{Fraction: m.Vec2{X: 0.5, Y: 0.5}}
)

// Converter converts an image on the CPU before it is packed. Its values must
// be comparable; equal converters share an image.
type Converter interface {
	Convert(source image.Image) image.Image
}

// DistanceField bakes an image's alpha into a distance field reaching Spread
// texels beyond its edges, for the SDF material.
type DistanceField struct{ Spread float32 }

// Convert answers source's distance field.
func (DistanceField) Convert(source image.Image) image.Image { return source }

// FromFile interns path and its options, at most one set, into an Image, so
// the common case reads FromFile("water.png"). A second set is reported once
// and ignored. It loads nothing.
func FromFile(path string, options ...ImageOptions) Image {
	var chosen ImageOptions
	if len(options) > 0 {
		chosen = options[0]
	}
	if len(options) > 1 {
		tables.report("FromFile: " + path + " is given more than one set of options")
	}
	return tables.internImage(imageKey{path: path, options: chosen})
}

// FromTexture interns a render texture, such as a camera's target, into an
// Image. It is not packed: it is drawn through a view of its own, and takes
// its colour space from options as any image does, so a Unorm texture
// holding encoded colour is marked ColorSpaceSrgb.
func FromTexture(texture render.Texture, options ...ImageOptions) Image {
	var chosen ImageOptions
	if len(options) > 0 {
		chosen = options[0]
	}
	return tables.internImage(imageKey{texture: texture, options: chosen})
}
