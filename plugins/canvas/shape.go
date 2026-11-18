package canvas

import "github.com/dvoyni/cog/libs/m"

// Shape is an entry that fills or strokes a rectangle, or draws a line. It
// is a literal, like every other entry: canvas.Shape{Kind: canvas.Fill,
// Rect: r, Tint: c, Layer: l}.
//
// Its rectangle, its line's ends, its width and its radius are in the
// shape's own space, which Transform places in the world, so they are world
// units under a transform that does not scale.
type Shape struct {
	Kind ShapeKind
	// Rect is a fill's or a stroke's rectangle.
	Rect m.Rect
	// From and To are a line's ends.
	From, To m.Vec2
	// Width is a stroke's or a line's width: a stroke lies inside its
	// rectangle, and a line is centred on its segment. Zero, or less, is a
	// hairline one target pixel wide at any zoom and any transform. A fill
	// ignores it.
	Width float32
	// Radius rounds a fill's or a stroke's corners, at most half the
	// rectangle's shorter side; zero, or less, leaves them square. A line
	// ignores it.
	Radius    float32
	Transform m.Mat2x3
	// Tint is the shape's colour.
	Tint m.Color
	// Material is the 2D material the shape is drawn with, the zero one
	// the default material.
	Material Material
	// Layer and Order are the shape's sort key: every entry of a lower
	// layer is drawn under every entry of a higher one, and within a layer
	// by order. Entries of equal keys are drawn in the order they were
	// recorded, unless their layer is in Config.RegroupedLayers.
	Layer int32
	Order int32
}

// batchKey answers what the shape shares with the entries it batches with.
func (s Shape) batchKey() batchKey {
	return batchKey{material: s.Material}
}

// ShapeKind is what a shape draws.
type ShapeKind uint8

const (
	// Fill fills the shape's rectangle with its tint.
	Fill ShapeKind = iota
	// Stroke draws the edge of the shape's rectangle, Width wide inside
	// it, as four fills that do not overlap, so a translucent tint does not
	// darken its corners.
	Stroke
	// Line draws the segment from From to To as one fill Width wide,
	// centred on it, its ends square and not extended.
	Line
)
