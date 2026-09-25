package internal

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

const (
	// UniformsPath declares struct CanvasUniforms and the group 0 binding.
	// Include it only if you are NOT extending the block: an extending material
	// hand-writes those six lines, which is exactly why the block is its own
	// source.
	UniformsPath = "builtin/canvas/uniforms.wgsl"
	// ClipPath declares canvasClipped, and nothing else. Calling it is offered
	// rather than required, and a hand-written fs_main that omits it draws
	// outside the clip rectangle with no error anywhere.
	ClipPath = "builtin/canvas/clip.wgsl"
	// SpriteBindingsPath declares the sprite family's group 1 sampler and array
	// texture, its group 2 instance buffer, and the SpriteInstance, Instances and
	// VertexOut structs.
	SpriteBindingsPath = "builtin/canvas/spritebindings.wgsl"
	// SpriteVertexPath includes SpriteBindingsPath and declares vs_main. Include
	// it to replace only fs_main.
	SpriteVertexPath = "builtin/canvas/spritevertex.wgsl"
	// TrianglesBindingsPath declares the triangles family's group 1 sampler and
	// texture and its VertexOut.
	TrianglesBindingsPath = "builtin/canvas/trianglesbindings.wgsl"
	// TrianglesVertexPath includes TrianglesBindingsPath and declares vs_main.
	TrianglesVertexPath = "builtin/canvas/trianglesvertex.wgsl"
	// KeyColorPath declares keyColorRamp, the sRGB transfer functions it is
	// written in, and the three key* constants, so a custom material wears the
	// exact ramp the built-ins do rather than a re-typed approximation.
	KeyColorPath = "builtin/canvas/keycolor.wgsl"
)

// SpriteInstance is one per-instance record the sprite shader reads from its
// storage buffer. Field order and size must match the shader's SpriteInstance
// (6 vec4, 96 bytes, no padding), so a []SpriteInstance uploads directly as the
// instance buffer for the instanced draw.
//
// The record is frozen. A custom sprite material may replace both entry points
// and append members to the uniform block, but it may not change this: the Go
// struct is hand-mirrored against the WGSL one and uploaded by direct
// reinterpretation, so a divergence is a silent misread rather than a compile
// error. TestSpriteInstanceMatchesTheShaderRecord is what catches it.
type SpriteInstance struct {
	Transform0 m.Vec4 // position.xy, size.xy
	Transform1 m.Vec4 // origin.xy, sine, cosine
	Frame      m.Vec4 // uv rect (x0, y0, x1, y1)
	Tint       m.Vec4
	Misc       m.Vec4 // atlasLayer, repeatX, repeatY, unused
	KeyColor   m.Vec4
}

// DrawsSnapshot is one produced snapshot, or the reason there is none. One
// struct carries both so that a caller cannot handle one and forget the other.
type DrawsSnapshot struct {
	Draws DrawsView
	// Tick is app.UpdateEvent.Tick of the tick the snapshot was taken in. It
	// travels with the snapshot rather than being asked for afterwards,
	// because only the tick itself knows which one it was.
	Tick int64
	Err  error
}

// DrawsView is one tick's recorded canvas operations, rendered while they are
// still alive. It is not a copy of the queue: no queue outlives the tick that
// filled it, and between ticks the queue is empty rather than stale, so the
// view is produced inside the tick and shaped by the request that asked for
// it. Nothing in it aliases the queue's storage - OpQueue.Ops documents that
// the slices it hands out die at the next reset, and every value here is
// already its own.
//
// Every index in it is a source index - a position in canvas flush order,
// never a position in the emitted array. Filtering makes the emitted array a
// subset, and if indices were positions in that subset a vertex drill-down
// would name a different op on the second call. With source indices the index
// is the address, and an elided op stays addressable for free.
type DrawsView struct {
	// Layers are the frame's layers in flush order, ascending, each with the
	// world window its ops are measured in. They are not optional and they are
	// not filtered by kind: without the window an op's coordinates mean
	// nothing.
	Layers []LayerView `json:"layers,omitempty"`
	// Ops are the recorded operations in flush order - layers ascending, then
	// recording order within a layer - which is the order they reach the GPU
	// in.
	Ops []OpView `json:"ops,omitempty"`
	// OpCount and LayerCount are the whole frame's, whatever the filter kept,
	// so a filtered response still says how much of the frame it is describing.
	OpCount    int `json:"opCount"`
	LayerCount int `json:"layerCount"`
	// FromLayer, ToLayer and Kinds echo the filter the request armed, and
	// OmittedOps and OmittedLayers are what it dropped. Whatever a snapshot
	// omits it says it omitted: a tool that silently truncates cannot be told
	// from a game that drew nothing.
	FromLayer     m.Maybe[int] `json:"fromLayer,omitzero"`
	ToLayer       m.Maybe[int] `json:"toLayer,omitzero"`
	Kinds         []string     `json:"kinds,omitempty"`
	OmittedOps    int          `json:"omittedOps,omitempty"`
	OmittedLayers int          `json:"omittedLayers,omitempty"`
}

// LayerView is one canvas layer: the world window its ops are written in, what
// it draws into, and whether it clears.
type LayerView struct {
	// Layer is the layer's own number, which is also its gfx pass order.
	Layer int `json:"layer"`
	// Window is the world rectangle SetLayerTransform mapped onto the layer's
	// surface, and Aspect how it was fitted. A layer that set neither has
	// neither, and its coordinates are the logical viewport's own.
	Window m.Maybe[RectView] `json:"window,omitzero"`
	Aspect string            `json:"aspect,omitempty"`
	// Target is screen or texture: a layer that named no target draws to the
	// screen, and a layer drawing into a texture nothing composites afterwards
	// is one of the ways a frame ends up empty.
	Target        string        `json:"target"`
	TargetTexture gfx.TextureID `json:"targetTexture,omitempty"`
	TargetWidth   int           `json:"targetWidth,omitempty"`
	TargetHeight  int           `json:"targetHeight,omitempty"`
	TargetMip     int           `json:"targetMip,omitempty"`
	TargetLayer   int           `json:"targetLayer,omitempty"`
	// Clear is the colour the layer clears its target to - r, g, b, a - and is
	// present only when the layer clears at all.
	Clear []float32 `json:"clear,omitempty"`
	// Ops is how many operations the layer recorded, before any kind filter, so
	// "on which layer" has an answer even where the filter emitted none of
	// them.
	Ops int `json:"ops"`
}

// OpView is one recorded operation: which call produced it, where it sits, and
// what it was given. The fields a kind gives no meaning to are absent rather
// than zero, because one flat union emitting all of them would put a dozen
// empty values beside every answer.
type OpView struct {
	// Index is the op's position in canvas flush order: its address, stable
	// under filtering, and what a vertex drill-down passes back.
	Index int    `json:"index"`
	Kind  string `json:"kind"`
	Layer int    `json:"layer"`
	// Clip is the recording-time clip rectangle in layer world space, present
	// only where one was in effect. An op clipped to nothing draws nothing,
	// which is an answer in itself.
	Clip m.Maybe[RectView] `json:"clip,omitzero"`
	// Path and Transform describe a sprite, and Texture the gfx texture it
	// samples where it named one instead of a resource path.
	Path      string               `json:"path,omitempty"`
	Texture   *gfx.TextureView     `json:"texture,omitempty"`
	Transform *SpriteTransformView `json:"transform,omitempty"`
	// FontPath, Text and Draw describe a text op.
	FontPath string        `json:"fontPath,omitempty"`
	Text     string        `json:"text,omitempty"`
	Draw     *TextDrawView `json:"draw,omitempty"`
	// VertexCount and Bounds summarise a triangle list, and VertexBytes says
	// how much geometry it recorded whatever layout it used - a custom layout
	// reports bytes and nothing else, because canvas cannot read its positions.
	// Vertices is the full list, and is present only for an op the request
	// named: a triangle-heavy frame is tens of thousands of them and nobody
	// debugs by reading coordinates.
	VertexCount int               `json:"vertexCount,omitempty"`
	VertexBytes int               `json:"vertexBytes,omitempty"`
	Bounds      m.Maybe[RectView] `json:"bounds,omitzero"`
	Vertices    []VertexView      `json:"vertices,omitempty"`
	// Material is the material the op named, and its absence means the op named
	// none: such a draw resolves to the layer's material set and then to the
	// built-in for its family, both at flush.
	Material *gfx.MaterialView `json:"material,omitempty"`
	// Params are the parameters recorded with the op, each carrying the live
	// arm of its union and nothing else.
	Params []gfx.ParameterView `json:"params,omitempty"`
}

// SpriteTransformView is one sprite's placement, with its enum named and its
// vectors rendered as the component arrays the view vocabulary uses for
// numbers. The zero fields are absent: a sprite sets two or three of these and
// means nothing by the rest.
type SpriteTransformView struct {
	// Position is x, y; Size and Origin likewise. Size is what the sprite draws
	// at, and its absence means Scale against the source's own pixels.
	Position []float32 `json:"position"`
	Size     []float32 `json:"size,omitempty"`
	Scale    float32   `json:"scale,omitempty"`
	Rotation float32   `json:"rotation,omitempty"`
	Origin   []float32 `json:"origin,omitempty"`
	// Frame is the sub-rectangle of the source that is sampled, and NineSlice
	// the insets a nine-slice is cut at, both in source pixels.
	Frame             *SpriteFrameView `json:"frame,omitempty"`
	NineSlice         *SpriteFrameView `json:"nineSlice,omitempty"`
	NineSliceScale    float32          `json:"nineSliceScale,omitempty"`
	NineSliceNoCenter bool             `json:"nineSliceNoCenter,omitempty"`
	FlipX             bool             `json:"flipX,omitempty"`
	FlipY             bool             `json:"flipY,omitempty"`
	TileX             bool             `json:"tileX,omitempty"`
	TileY             bool             `json:"tileY,omitempty"`
	// Filter is the sampler filtering, named rather than numbered.
	Filter string `json:"filter"`
}

// SpriteFrameView is a rectangle of source pixels given as insets from each
// edge, which is what SpriteFrame is.
type SpriteFrameView struct {
	Left   int `json:"left,omitempty"`
	Top    int `json:"top,omitempty"`
	Right  int `json:"right,omitempty"`
	Bottom int `json:"bottom,omitempty"`
}

// TextDrawView is one text op's layout and colour.
//
// It carries neither the draw's material nor its parameters, and that is not
// an omission: the op reports both already, its Material through the same
// MaterialView every other op uses and its Params as the recorded list, which
// for a text op is the draw's own.
type TextDrawView struct {
	Position []float32 `json:"position"`
	Size     float32   `json:"size"`
	// Color is r, g, b, a, and becomes the instance tint every glyph is drawn
	// with. A fully transparent one is the answer to text that is laid out and
	// invisible.
	Color        []float32 `json:"color"`
	Align        string    `json:"align"`
	WordWrapping bool      `json:"wordWrapping,omitempty"`
	WrapWidth    float32   `json:"wrapWidth,omitempty"`
}

// VertexView is one built-in vertex: position and uv in their own spaces, and
// the colour the shader multiplies by.
type VertexView struct {
	Position []float32 `json:"position"`
	Color    []float32 `json:"color"`
	UV       []float32 `json:"uv"`
}

// RectView is a rectangle in layer world space, named so an agent reading a
// clip and a bounding box reads them the same way.
type RectView struct {
	X      float32 `json:"x"`
	Y      float32 `json:"y"`
	Width  float32 `json:"width"`
	Height float32 `json:"height"`
}
