package canvas

import (
	"github.com/dvoyni/cog/libs/m"
	"prototype.invalid/canvasframe/mx"
)

// Shape is an entry that fills or strokes a rectangle, or draws a line: a
// sprite over the white texel, batching with sprites. It is a literal like
// every other entry: canvas.Shape{Kind: canvas.Fill, Rect: r, Tint: c,
// Layer: l}.
type Shape struct {
	Kind ShapeKind
	// Rect is a fill's or a stroke's rectangle, From and To a line's ends,
	// in the shape's own space.
	Rect     m.Rect
	From, To m.Vec2
	// Width is a stroke's or a line's, in world units; zero is a hairline one
	// target pixel wide at any zoom.
	Width float32
	// Radius rounds a fill's or a stroke's corners, in world units.
	Radius    float32
	Transform mx.Mat2x3
	// Tint is the shape's colour.
	Tint         m.Color
	Material     Material
	Clip         m.Rect
	Layer, Order int32
}

// ShapeKind is what a shape draws.
type ShapeKind uint8

const (
	Fill ShapeKind = iota
	Stroke
	Line
)

// DrawShape records shape.
func (l DrawList) DrawShape(shape Shape) { l.recordShape(shape, nil) }

// DrawShapeWith records shape with its material's argument.
func (l DrawList) DrawShapeWith[T any](shape Shape, argument T) { l.recordShape(shape, argument) }
