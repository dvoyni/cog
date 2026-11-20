package canvas

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/render"
)

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
	// Clip is a rectangle of the world outside which the shape draws
	// nothing, zero meaning none, seen through each camera as the shape is:
	// its bounding box in the target's pixels, a pixel drawn where its
	// centre lies inside, under a turned camera. A change of clip from one
	// entry to the next in sort order breaks a batch.
	Clip m.Rect
	// Layer and Order are the shape's sort key: every entry of a lower
	// layer is drawn under every entry of a higher one, and within a layer
	// by order. Entries of equal keys are drawn in the order they were
	// recorded, unless their layer is in Config.RegroupedLayers.
	Layer int32
	Order int32
}

// batchKey answers what the shape shares with the entries it batches with:
// it draws over the white texel, in the sprite atlas seen through texture,
// and takes no filter, which changes no pixel of it.
func (s *Shape) batchKey(texture render.TextureView) batchKey {
	return batchKey{material: s.Material, texture: texture, clip: s.Clip}
}

// findSquare answers the transform from the unit square onto the shape in
// the world, its own transform, a zero one read as the identity, after its
// rectangle's or its line's band's, and the square's size in the shape's own
// space: its rectangle's, or its line's, a hairline's one unit across.
func (s *Shape) findSquare() (m.Mat2x3, m.Vec2) {
	// The rectangle's own transform, column by column: the unit square
	// scaled to its size, then moved to its origin.
	square := m.Mat2x3{s.Rect.Width, 0, 0, s.Rect.Height, s.Rect.X, s.Rect.Y}
	size := s.Rect.Size()
	if s.Kind == Line {
		square, size = makeLineSquare(s.From, s.To, max(s.Width, 0))
	}
	if s.Transform == (m.Mat2x3{}) {
		return square, size
	}
	return s.Transform.Mul(square), size
}

// findBounds answers the world rectangle the shape draws within: the
// bounding box of its unit square in the world.
func (s *Shape) findBounds() m.Rect {
	square, _ := s.findSquare()
	return square.TransformRect(m.Rect{Width: 1, Height: 1})
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
