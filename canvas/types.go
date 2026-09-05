package canvas

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// Layer orders canvas drawing, and is a gfx pass order: canvas declares one
// pass per non-empty layer at that order, so anything else recording into the
// same frame - a scene camera, say - interleaves by taking an order between two
// layer values, with no canvas API for it at all.
type Layer = gfx.Order

type Vertex struct {
	Position m.Vec2
	Color    m.Color
	UV       m.Vec2
}

// VertexLayout is implemented by plain-data vertex types accepted by
// OpQueue.DrawTriangles. The returned attributes map struct byte offsets to
// shader locations in order.
type VertexLayout interface {
	VertexLayout() []gfx.VertexAttr
}

// VertexLayout returns the built-in position/color/UV layout.
func (Vertex) VertexLayout() []gfx.VertexAttr { return triangleVertexLayout[:] }

type AspectMode uint8

const (
	AspectInscribe AspectMode = iota
	AspectOverlap
	AspectStretch
)

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

type SpriteFrame struct {
	Left, Top, Right, Bottom int
}

type SpriteTransform struct {
	Position m.Vec2
	// Size is the drawn size in logical pixels. A zero component is unset: when both
	// are unset the size is the texture size times Scale; when exactly one is set the
	// other is derived from it preserving the texture's aspect ratio.
	Size m.Vec2
	// Scale multiplies the texture size when Size is fully unset. Its zero value
	// means 1 (natural texture size). It is resolved at flush, so a lazy path sprite
	// needs no preloaded dimensions.
	Scale    float32
	Rotation float32
	Origin   m.Vec2
	Frame    SpriteFrame
	// NineSlice splits the source into corners, sides, and center using pixel
	// insets resolved after the texture dimensions are known. NineSliceScale
	// controls destination border thickness; zero means one.
	NineSlice         SpriteFrame
	NineSliceScale    float32
	NineSliceNoCenter bool
	// FlipX and FlipY mirror the sprite horizontally or vertically by reversing
	// texture sampling within the drawn rectangle; geometry, Origin, and Rotation
	// are unaffected.
	FlipX bool
	FlipY bool
	// TileX and TileY repeat the texture across the drawn Size on that axis. A
	// tiled axis requires an explicit Size; the other axis falls back to the
	// texture's natural pixel size when Size is unset. Tiling draws through a
	// standalone repeat texture and the textured-triangle path rather than the
	// atlas, so Scale, Frame, and Flip are ignored.
	TileX bool
	TileY bool
	// Filter selects sampler minification/magnification filtering. Its zero value
	// is linear; set FilterNearest for crisp pixel art.
	Filter gfx.FilterMode
}

type TextAlign uint8

const (
	AlignLeft TextAlign = iota
	AlignCenter
	AlignRight
)

type TextDraw struct {
	Position     m.Vec2
	Size         float32
	Color        m.Color
	Align        TextAlign
	WordWrapping bool
	WrapWidth    float32
}
