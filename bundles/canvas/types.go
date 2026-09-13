package canvas

import (
	"github.com/dvoyni/cog/bundles/canvas/internal"
	"github.com/dvoyni/cog/libs/m"
)

// Layer orders canvas drawing, and is a gfx pass order: canvas declares one
// pass per non-empty layer at that order, so anything else recording into the
// same frame - a scene camera, say - interleaves by taking an order between two
// layer values, with no canvas API for it at all.
type Layer = internal.Layer

// Vertex is the built-in position/color/UV vertex, and implements VertexLayout.
type Vertex = internal.Vertex

// VertexLayout is implemented by plain-data vertex types accepted by
// OpQueue.DrawTriangles. The returned attributes map struct byte offsets to
// shader locations in order.
type VertexLayout = internal.VertexLayout

// AspectMode is how SetLayerTransform fits a layer's world window into its
// surface.
type AspectMode = internal.AspectMode

const (
	AspectInscribe = internal.AspectInscribe
	AspectOverlap  = internal.AspectOverlap
	AspectStretch  = internal.AspectStretch
)

// SpriteFrame is a rectangle of source pixels given as insets from each edge.
type SpriteFrame = internal.SpriteFrame

// SpriteTransform places one sprite: its position, size or scale, rotation,
// origin, source frame, nine-slice, flips, tiling and sampler filter.
type SpriteTransform = internal.SpriteTransform

// TextAlign is how a text draw's lines align to its position.
type TextAlign = internal.TextAlign

const (
	AlignLeft   = internal.AlignLeft
	AlignCenter = internal.AlignCenter
	AlignRight  = internal.AlignRight
)

// TextDraw describes one Text draw: its layout, colour and shading.
type TextDraw = internal.TextDraw

// ShapeDraw describes a FillRect, StrokeRect or Line: what colour it is, how
// thick, and what shades it. A zero Color is opaque white.
type ShapeDraw = internal.ShapeDraw

// LayerTransform returns the scale and offset mapping a layer's world
// coordinates to logical viewport coordinates as world*scale + offset, matching
// how SetLayerTransform renders. A zero-area window yields the identity. Callers
// bake destination sub-rectangles and min/max scale caps into the window they
// pass; this reports the resulting transform.
func LayerTransform(window m.Rect, aspect AspectMode, viewport m.Vec2) (scale, offset m.Vec2) {
	if window.Width <= 0 || window.Height <= 0 {
		return m.Vec2{X: 1, Y: 1}, m.Vec2{}
	}
	scale = m.Vec2{X: viewport.X / window.Width, Y: viewport.Y / window.Height}
	switch aspect {
	case AspectInscribe:
		scale.X = min(scale.X, scale.Y)
		scale.Y = scale.X
	case AspectOverlap:
		scale.X = max(scale.X, scale.Y)
		scale.Y = scale.X
	}
	offset = m.Vec2{
		X: (viewport.X-window.Width*scale.X)/2 - window.X*scale.X,
		Y: (viewport.Y-window.Height*scale.Y)/2 - window.Y*scale.Y,
	}
	return scale, offset
}

// WorldToScreen maps a layer world point to logical viewport coordinates.
func WorldToScreen(window m.Rect, aspect AspectMode, viewport, world m.Vec2) m.Vec2 {
	scale, offset := LayerTransform(window, aspect, viewport)
	return m.Vec2{X: world.X*scale.X + offset.X, Y: world.Y*scale.Y + offset.Y}
}

// ScreenToWorld inverts WorldToScreen so input code can hit-test in world space.
func ScreenToWorld(window m.Rect, aspect AspectMode, viewport, screen m.Vec2) m.Vec2 {
	scale, offset := LayerTransform(window, aspect, viewport)
	return m.Vec2{X: (screen.X - offset.X) / scale.X, Y: (screen.Y - offset.Y) / scale.Y}
}
