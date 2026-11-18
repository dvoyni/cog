package canvas

import (
	"math"

	"github.com/dvoyni/cog/libs/m"
	"prototype.invalid/canvasframe/mx"
)

// Sprite is an entry that draws an image's region through a transform about its
// image's pivot. It is a plain value, so an ECS component can be exactly it.
type Sprite struct {
	Image Image
	// Frame is a sheet's frame, counted along its rows.
	Frame uint32
	// Transform maps the sprite into the world; zero is identity. A negative
	// scale mirrors it.
	Transform mx.Mat2x3
	// Size is in world units, zero meaning the frame's size in texels; a zero
	// on one axis keeps the aspect ratio. It is apart from the transform's
	// scale so a sliced or tiled sprite keeps its corners and tiles.
	Size m.Vec2
	// Tint multiplies: straight alpha, premultiplied by canvas, zero meaning
	// white.
	Tint     m.Color
	Material Material
	// Crop leaves out part of the sprite where it lies, as a fill bar empties
	// from one side; zero draws all of it.
	Crop Crop
	// Clip is a rectangle of the world outside which the sprite draws
	// nothing, zero meaning none; a change of clip breaks a batch.
	Clip     m.Rect
	DrawMode DrawMode
	// TexelScale is how large a sliced sprite's corners or a tiled sprite's
	// tiles take their texels, zero meaning 1; a stretched sprite ignores it.
	TexelScale float32
	Filter     Filter
	// Layer, then Order, then the list's order, then the call order, is the
	// sort key.
	Layer, Order int32
}

// Crop is the fraction of a sprite's rectangle left out from each side.
type Crop struct{ Left, Top, Right, Bottom float32 }

// DrawMode is how a sprite fills its rectangle with its region.
type DrawMode uint8

const (
	Stretched DrawMode = iota
	// Sliced keeps the corners of the image's Slice insets at their size.
	Sliced
	// Tiled repeats the region, at TexelScale.
	Tiled
)

// Filter is how texels are read between their centres.
type Filter uint8

const (
	// FilterDefault is Config.DefaultFilter.
	FilterDefault Filter = iota
	FilterLinear
	FilterNearest
)

// OrderOf maps every float onto its own int32 in the same order, for
// y-sorting with no scale to choose: Order: canvas.OrderOf(position.Y). −0
// sorts as +0 and NaN after every number. OrderOf(y)-1 sorts just under y, as
// a unit's shadow does.
func OrderOf(value float32) int32 {
	if value != value {
		return math.MaxInt32
	}
	bits := math.Float32bits(value)
	magnitude := int32(bits &^ (1 << 31))
	if bits>>31 != 0 {
		return -magnitude
	}
	return magnitude
}

// DrawSprite records sprite.
func (l DrawList) DrawSprite(sprite Sprite) { l.recordSprite(sprite, nil) }

// DrawSpriteWith records sprite with its material's argument, of the type the
// material declares; another type is refused and reported once.
func (l DrawList) DrawSpriteWith[T any](sprite Sprite, argument T) { l.recordSprite(sprite, argument) }

// DrawSprites records sprites in their order, as an ECS chunk's column.
func (l DrawList) DrawSprites(sprites []Sprite) {
	for _, sprite := range sprites {
		l.recordSprite(sprite, nil)
	}
}

// DrawSpritesWith records sprites with their arguments, parallel slices of
// one length, as an ECS chunk's columns.
func (l DrawList) DrawSpritesWith[T any](sprites []Sprite, arguments []T) {
	if len(sprites) != len(arguments) {
		tables.report("DrawSpritesWith: slices of different lengths are refused")
		return
	}
	for index, sprite := range sprites {
		l.recordSprite(sprite, arguments[index])
	}
}
