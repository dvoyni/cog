package types

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
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
// coordinates to logical viewport coordinates; see canvas.LayerTransform.
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
	// are unset the size is the source size times Scale; when exactly one is set the
	// other is derived from it preserving the source's aspect ratio. The source is
	// what Frame selects, not the whole texture.
	Size m.Vec2
	// Scale multiplies the source size when Size is fully unset. Its zero value
	// means 1 (natural source size). It is resolved at flush, so a lazy path sprite
	// needs no preloaded dimensions.
	Scale    float32
	Rotation float32
	Origin   m.Vec2
	// Frame selects the sub-rectangle of the source the sprite samples, as pixel
	// insets from each edge. It narrows the source rather than windowing a sprite
	// of fixed size: an unsized sprite's natural size is the frame's extent, so
	// Scale means one framed texel per world unit. A frame that leaves no source
	// draws nothing and is reported. NineSlice measures its insets into the frame;
	// TileX and TileY ignore it.
	Frame SpriteFrame
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
	// TileX and TileY repeat the sprite across the drawn Size on that axis. A
	// tiled axis requires an explicit Size - it has no natural length to fall
	// back to - and the other axis falls back to one tile when Size is unset.
	//
	// What repeats is a window onto the atlas, wrapped in the fragment stage, so
	// Scale, Frame and the flips all apply: the tile is the framed sub-rect at
	// the given scale, and a tiled sprite batches with every other sprite in the
	// atlas. An image too large to pack keeps a standalone repeat texture and the
	// textured-triangle path.
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
	Position m.Vec2
	Size     float32
	// Color is the run's ink. A glyph takes it whole; an inline icon takes only
	// its alpha.
	//
	// The field carries two things at once, because m.Color does. Rgb says what
	// colour the ink is, and for a glyph that is the whole mark: the font atlas
	// holds RGB=255 with coverage in alpha, so multiplying by tint is literally
	// what colours the text. An icon's texel is already the artwork, so the same
	// multiply is a modulation that would destroy it, and white stays the
	// identity. Alpha says how present the run is, which is true of anything
	// drawn - a label fading out takes its icon down with it rather than leaving
	// it riding fully opaque over faded text.
	//
	// An inline icon therefore has no way to be given the ink colour, and that is
	// deliberate rather than missing: a mark that wants the ink colour is a
	// glyph, and belongs in the font, where it also gets kerning and baseline
	// handling. Failing that it is a Sprite draw carrying
	// gfx.ColorParam(TintSlot, ...) beside the text rather than inside it.
	//
	// Effects read tint, so this is what they see of an icon: an inline icon
	// haloes white at the run's alpha - a band that follows the run in presence
	// and not in colour.
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
