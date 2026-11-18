package canvas

import "github.com/dvoyni/cog/libs/m"

// Shape is an entry that fills a rectangle. It is a literal, like every
// other entry: canvas.Shape{Kind: canvas.Fill, Rect: r, Tint: c, Layer: l}.
type Shape struct {
	Kind ShapeKind
	// Rect is a fill's rectangle, in the shape's own space, which Transform
	// places in the world.
	Rect      m.Rect
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
)
