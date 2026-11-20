package canvas

import (
	"cmp"

	"github.com/dvoyni/cog/libs/m"
)

// ImageOptions are what an image is loaded and placed with. Images of one
// path that differ only in options that change no pixel, such as the
// pivot, share their region of the atlas.
type ImageOptions struct {
	// ColorSpace is how the file encodes its colours.
	ColorSpace ColorSpace
	// Wrap is the axes the image tiles along, whose gutters hold its
	// opposite edge, so it tiles without seams; along any other, its
	// gutter continues its edge. A tiled and a plain image of one file are
	// different images.
	Wrap Wrap
	// Sheet is the grid of frames the image is cut into, each packed as a
	// region of its own with its own gutter, so filtering and mips never
	// mix two frames, and drawn by a Sprite's Frame; zero is one frame, and
	// zero columns or rows is one. Each frame is the image's size divided
	// by its columns and rows, texels left over unused.
	Sheet Grid
	// SourceRect is the rectangle of the file the image is, in texels,
	// zero meaning the whole file: its size is the rectangle's, or its
	// frame's, and its pivot is measured within it. Images of several
	// rectangles of one file decode it once. A SourceRect reaching past
	// its file, or a sprite's Frame past its sheet's last, draws nothing,
	// reported once for its path as ReportKindFrameOutOfBounds.
	SourceRect m.Recti
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

// Wrap is the axes an image tiles along.
type Wrap uint8

const (
	// WrapNone tiles along neither axis.
	WrapNone Wrap = iota
	// WrapX tiles along x, WrapY along y and WrapBoth along both.
	WrapX
	WrapY
	WrapBoth
)

// Grid is a sheet's columns and rows of frames, its frames counted along
// its rows.
type Grid struct {
	Columns uint32
	Rows    uint32
}

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

// findFrameRect answers the rectangle of a file fileWidth by fileHeight
// texels that frame of an image of o is, and whether it lies within the
// file: its SourceRect, the whole file when zero, cut by its sheet into
// frames counted along its rows, each the rectangle's size divided by the
// sheet's columns and rows. A frame past the sheet's last, of no texels, or
// a SourceRect reaching past the file answers the zero rectangle, and
// false.
func (o ImageOptions) findFrameRect(fileWidth, fileHeight int, frame uint32) (m.Recti, bool) {
	file := m.Recti{Width: fileWidth, Height: fileHeight}
	sheetRect := o.SourceRect
	if sheetRect == (m.Recti{}) {
		sheetRect = file
	}
	columns := cmp.Or(o.Sheet.Columns, 1)
	rows := cmp.Or(o.Sheet.Rows, 1)
	frameWidth := sheetRect.Width / int(columns)
	frameHeight := sheetRect.Height / int(rows)
	withinFile := file.Contains(sheetRect.Min()) && file.Contains(sheetRect.Max())
	if !withinFile || frame >= columns*rows || frameWidth <= 0 || frameHeight <= 0 {
		return m.Recti{}, false
	}
	return m.Recti{X: sheetRect.X + int(frame%columns)*frameWidth, Y: sheetRect.Y + int(frame/columns)*frameHeight, Width: frameWidth, Height: frameHeight}, true
}
