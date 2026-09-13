package internal

import (
	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/dvoyni/cog/libs/m"
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
	Filter gpu.FilterMode
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
	// Material and Params shade the glyphs and inline icons this draw produces.
	//
	// Text is a sprite draw twice over - glyphs and inline icons both reach the
	// sprite batcher - so a text material is a sprite material: it samples a
	// texture_2d_array and keys exactly as any other sprite does. It costs no
	// extra draw, because the batch key already separates glyphs from sprites
	// through the texture, the font atlas not being the sprite atlas.
	//
	// Text already spends both reserved names: Color becomes the instance tint
	// and glyphs carry the default key colour, so a text material may not
	// reclaim TintSlot or KeyColorSlot.
	Material *gfx.MaterialDescr
	Params   []gfx.ParameterDescr
}

// ShapeDraw describes a FillRect, StrokeRect or Line: what colour it is, how
// thick, and what shades it. It is the shape TextDraw already has, and it is a
// struct rather than trailing arguments for the same reason - these calls
// describe a whole draw, where Sprite and DrawTriangles take a transform or a
// path and then say how to shade it.
//
// FillRect ignores Thickness, exactly as TextDraw ignores WrapWidth without
// WordWrapping.
//
// A zero Color is opaque white, matching ui's default tint and canvas's habit of
// reading a zero scale as 1. Without that rule a ShapeDraw naming only a
// material would draw nothing at all, which is the likelier mistake in the world
// this type creates.
//
// A fill is a sprite draw, so Material is a sprite material and the reserved
// names apply to it as they do to text: Color becomes the instance tint.
type ShapeDraw struct {
	Color     m.Color
	Thickness float32
	Material  *gfx.MaterialDescr
	Params    []gfx.ParameterDescr
}

// tint reports the colour a shape draws at, reading a zero colour as opaque
// white.
func (d ShapeDraw) tint() m.Color {
	if d.Color == (m.Color{}) {
		return m.Color{R: 1, G: 1, B: 1, A: 1}
	}
	return d.Color
}
