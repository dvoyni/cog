package canvas

import (
	"cmp"
	"fmt"
	"reflect"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/render"
)

// ImageOptions are what an image is loaded and placed with, of a file, or
// of a render texture, measured in its texels. Images of one path that
// differ only in options that change no pixel, the pivot, the slice insets
// and SliceHollow, share their region of the atlas.
//
// They are compared as an image is interned, so a Converter they hold is
// comparable.
type ImageOptions struct {
	// ColorSpace is how the file encodes its colours.
	ColorSpace ColorSpace
	// Wrap is the axes the image tiles along, whose gutters hold its
	// opposite edge, so a Tiled sprite of it tiles without seams; along any
	// other, its gutter continues its edge. An image drawn from a texture
	// of its own, or of a render texture, has no gutter, and its sampler
	// repeats along them instead, a change of which breaks a batch. A
	// tiled and a plain image of one file are different images.
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
	// Slice is the insets, in texels of each frame, a Sliced sprite cuts
	// the image into nine by: its corners, kept at their size, its edges
	// and its centre. Insets meeting past the frame are shrunk, keeping
	// their ratio, until they meet, and a negative one is zero.
	Slice Insets
	// SliceHollow leaves a Sliced sprite's centre empty, for a frame.
	SliceHollow bool
	// Convert converts each frame of the image on the CPU before it is
	// packed, as Converter says; zero converts nothing.
	Convert Converter
}

// ColorSpace is how an image's file, or its render texture, encodes its
// colours.
type ColorSpace uint8

const (
	// ColorSpaceSrgb is colour encoded in sRGB, as most pictures are, and
	// as a camera draws into a texture of a format encoding nothing: it is
	// decoded to linear colour as it is sampled.
	ColorSpaceSrgb ColorSpace = iota
	// ColorSpaceLinear is data or linear colour, sampled as it is.
	ColorSpaceLinear
)

// findRGBA8Format answers the RGBA8 format that encodes colour as space
// does: RGBA8UnormSrgb for ColorSpaceSrgb and RGBA8Unorm for
// ColorSpaceLinear, a plain sprite atlas's.
func findRGBA8Format(space ColorSpace) render.TextureFormat {
	if space == ColorSpaceLinear {
		return render.TextureFormatRGBA8Unorm
	}
	return render.TextureFormatRGBA8UnormSrgb
}

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
// an Image: every call of the same path and options answers the same one, a
// Converter compared by value.
// It loads nothing: a sprite's first draw of it loads it. Each call takes
// the table's lock and hashes path, so a game makes an Image once, as it
// names its art, and keeps it, rather than calling ImageFromFile for each entry
// it draws. It takes at most
// one set of options, so the common case reads ImageFromFile("water.png"); a
// second set is ignored, and reported once, as
// ReportKindImageOptionsIgnored, when the Image answered is first drawn.
func ImageFromFile(path string, options ...ImageOptions) Image {
	return internImage(imageKey{path: path}, options)
}

// ImageFromTexture interns texture, a texture made with render, with
// options, into an Image: every call of the same texture and options
// answers the same one, so a game makes it once, as it makes the texture.
// The image is not packed: its entries draw the texture's mip level 0 and
// layer 0, where a camera into it draws, through a 2D-array view of them
// alone, so a change to or from it breaks a batch, and a minimap camera's
// texture drawn as a sprite shows what the camera drew, when it draws
// first, at a lower order, or at the same order into a texture. Its texels
// are taken as premultiplied, as a camera draws them. Its options are an
// ImageFromFile's, measured in the texture's texels: its ColorSpace is how
// the texels encode colour, so a texture of a format encoding nothing that
// holds encoded colour, as a camera's into it does, is marked
// ColorSpaceSrgb, the zero, and decoded as it is sampled, after filtering,
// so linear filtering blends its encoded colour, looking as the world drawn
// directly; a texture of an sRGB format is decoded by its
// format, however it is marked. A texture render does not hold at the
// image's first draw draws nothing, reported once as
// ReportKindImageTextureRefused. It takes at most one set of options, a
// second ignored and reported once, as ImageFromFile's is.
func ImageFromTexture(texture render.Texture, options ...ImageOptions) Image {
	return internImage(imageKey{texture: texture, ofTexture: true}, options)
}

// internImage interns key, its options the first of options, marked as
// having ignored the rest, and as having refused their converter, which it
// drops, when it is not comparable, since interning compares it, or is of
// an image of a render texture, whose texels are never on the CPU.
func internImage(key imageKey, options []ImageOptions) Image {
	if len(options) > 0 {
		key.options = options[0]
	}
	key.ignoredOptions = len(options) > 1
	if converter := key.options.Convert; converter != nil {
		switch {
		case key.ofTexture:
			key.converterRefusal = fmt.Sprintf("a converter of %T, which an image of a render texture cannot run", converter)
		case !reflect.ValueOf(converter).Comparable():
			key.converterRefusal = fmt.Sprintf("a converter of %T, which is not comparable", converter)
		}
		if key.converterRefusal != "" {
			key.options.Convert = nil
		}
	}
	return Image{index: imageNames.internKey(key)}
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
