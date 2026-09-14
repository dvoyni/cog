package canvas

import (
	"github.com/dvoyni/cog/bundles/canvas/internal/types"
	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/dvoyni/cog/libs/m"
)

// Layer orders canvas drawing, and is a gfx pass order: canvas declares one
// pass per non-empty layer at that order, so anything else recording into the
// same frame - a scene camera, say - interleaves by taking an order between two
// layer values, with no canvas API for it at all.
type Layer = types.Layer

// Vertex is the built-in position/color/UV vertex, and implements VertexLayout.
type Vertex = types.Vertex

// VertexLayout is implemented by plain-data vertex types accepted by
// OpQueue.DrawTriangles. The returned attributes map struct byte offsets to
// shader locations in order.
type VertexLayout = types.VertexLayout

// AspectMode is how SetLayerTransform fits a layer's world window into its
// surface.
type AspectMode = types.AspectMode

const (
	AspectInscribe = types.AspectInscribe
	AspectOverlap  = types.AspectOverlap
	AspectStretch  = types.AspectStretch
)

// SpriteFrame is a rectangle of source pixels given as insets from each edge.
type SpriteFrame = types.SpriteFrame

// SpriteTransform places one sprite: its position, size or scale, rotation,
// origin, source frame, nine-slice, flips, tiling and sampler filter.
type SpriteTransform = types.SpriteTransform

// TextAlign is how a text draw's lines align to its position.
type TextAlign = types.TextAlign

const (
	AlignLeft   = types.AlignLeft
	AlignCenter = types.AlignCenter
	AlignRight  = types.AlignRight
)

// TextDraw describes one Text draw: its layout, colour and shading.
type TextDraw = types.TextDraw

// ShapeDraw describes a FillRect, StrokeRect or Line: what colour it is, how
// thick, and what shades it. A zero Color is opaque white.
type ShapeDraw = types.ShapeDraw

// OpKind identifies which recording call produced an Op.
type OpKind = types.OpKind

const (
	OpSprite    = types.OpSprite
	OpText      = types.OpText
	OpTriangles = types.OpTriangles
)

// Op is a read-only view of one recorded operation. OpQueue.Ops returns them in
// flush order so a recorder can assert what it produced — layer, order,
// transform, clip and parameters — without running the GPU pipeline. Op.Param
// and Op.ColorParam look a recorded parameter up by name.
type Op = types.Op

// LookupAccess is a handler-scoped facade over a Lookup. It carries the kernel
// (for error reporting) and the read filesystem needed to lazily load sprites
// and fonts, without ever retaining them past the handler's lock scope. Acquire
// a *Lookup write dependency plus storage.FileSystem in a handler, build a
// LookupAccess with NewLookupAccess, and pass it to consumers for the duration
// of that handler.
type LookupAccess = types.LookupAccess

// FontMetrics reports a font's vertical metrics at a given size, in logical
// pixels, for baseline placement and inline-icon alignment.
type FontMetrics = types.FontMetrics

// MaterialSet is the shading a scope - the queue, a layer, a ui.Frame or a ui
// element subtree - supplies to the draws beneath it that name none of their
// own: one material per family (Sprite, Triangles, Texture), plus one parameter
// list shared by all three.
//
// A scope names a set rather than a material because a layer is never one
// family: every interesting layer carries sprites and triangles, and the two can
// never be one shader. A nil slot keeps its built-in, so an entirely zero set is
// the built-ins. One parameter list serves all three slots because gfx drops a
// name the bound shader never declared. docs/specs/materials.md carries the
// whole reasoning.
type MaterialSet = types.MaterialSet

// HaloProfile is the shape of the band, independent of its colour. Distances are
// in layer-local world units, so a band scales with the layer's transform for
// free - board zoom rides that transform, downstream of the quad expansion.
//
// Reach is how far the band extends beyond the mark: what the vertex stage grows
// the sprite's quad by on every side. Plateau is the fraction of the band that
// holds at full strength before the falloff begins, between 0 and 1. Exponent
// shapes that falloff: 1 is the linear ramp measured off the art, higher is a
// faster fade.
//
// It is a complete profile rather than a struct emitting a parameter per
// non-zero field: Plateau 0 is a legitimate value - no plateau, pure falloff -
// that a sentinel would read as 0.18. Taking the whole thing removes the trap
// instead of documenting it, and a fluent DefaultHaloProfile().WithReach(12) can
// be added later without breaking anything.
//
// Colour is not here. It rides tint - ShapeDraw.Color, TextDraw.Color and a
// sprite's tint parameter all land in the instance record's frozen Tint field -
// so it is per sprite and costs nothing, and tint.a is the band's peak alpha,
// which means fading a cluster fades its halo with it.
type HaloProfile = types.HaloProfile

// The four reserved canvas parameter names: the names canvas consumes itself and
// never forwards to a material.
//
// TextureSlot and SamplerSlot are the texture and sampler a draw samples; bind
// them through a material or through DrawTriangles params, and the built-in
// triangle shader samples them with raw uv. TintSlot and KeyColorSlot are fields
// of the sprite instance record, which canvas reads out of a draw's parameters
// by name and packs in, so a custom sprite shader reads them from the shared
// VertexOut rather than from a uniform and may not reclaim either name.
//
// All four are constants rather than string literals scattered through the
// flush, because a reserved name spelled in four places is reserved only by
// coincidence.
const (
	TextureSlot  = types.TextureSlot
	SamplerSlot  = types.SamplerSlot
	TintSlot     = types.TintSlot
	KeyColorSlot = types.KeyColorSlot
)

const (
	// The seven published sources: the WGSL an app includes when it writes a
	// canvas material, so it declares six lines and three includes instead of
	// copying seventy lines of contract it would then have to keep in sync by
	// hand. Each is included by absolute storage name, which is what these
	// constants spell - a relative include has no directory to resolve against
	// from a ShaderWithText root, and an inline Go string is the shape a custom
	// canvas material actually has.
	//
	// Every one of them states in its header exactly what it declares, because
	// the rule an app must obey is "do not declare anything a source you included
	// declares" and a duplicated binding costs the whole frame with nothing
	// reported anywhere.
	//
	// Each resolves through the full mount overlay, so an app that mounts its own
	// builtin/canvas/keycolor.wgsl at higher priority replaces that one source
	// inside canvas's own module and keeps the rest. That is cog's customization
	// mechanism working as designed, recorded here as available rather than left
	// to be discovered.

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

	// DefaultFontPath is the font Canvas draws with when Text is given no font
	// path. It ships inside the binary and is mounted alongside the built-in
	// shaders, so putting a number on screen costs no asset, no mount and no
	// configuration. It is exported so a caller can name it deliberately and mix
	// it with their own fonts, rather than reaching it only by omission.
	//
	// The file is google/fonts@ofl/jetbrainsmono/JetBrainsMono[wght].ttf, renamed
	// only because '[' is a glob metacharacter in a go:embed pattern. That build
	// covers Latin, Latin-Ext, Greek, Cyrillic, Cyrillic-Ext, arrows and
	// box-drawing in 1179 glyphs, and is smaller compressed than the upstream
	// statics. It is a variable font; x/image/font/sfnt rasterizes its default
	// instance, which is Regular.
	//
	// Its programming ligatures never fire here: opentype lays glyphs out rune by
	// rune with no GSUB shaping, so debug output is never silently rewritten.
	DefaultFontPath = types.DefaultFontPath
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
	Misc       m.Vec4 // atlasLayer, unused, unused, unused
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
	FromLayer     *int     `json:"fromLayer,omitempty"`
	ToLayer       *int     `json:"toLayer,omitempty"`
	Kinds         []string `json:"kinds,omitempty"`
	OmittedOps    int      `json:"omittedOps,omitempty"`
	OmittedLayers int      `json:"omittedLayers,omitempty"`
}

// LayerView is one canvas layer: the world window its ops are written in, what
// it draws into, and whether it clears.
type LayerView struct {
	// Layer is the layer's own number, which is also its gfx pass order.
	Layer int `json:"layer"`
	// Window is the world rectangle SetLayerTransform mapped onto the layer's
	// surface, and Aspect how it was fitted. A layer that set neither has
	// neither, and its coordinates are the logical viewport's own.
	Window *RectView `json:"window,omitempty"`
	Aspect string    `json:"aspect,omitempty"`
	// Target is screen or texture: a layer that named no target draws to the
	// screen, and a layer drawing into a texture nothing composites afterwards
	// is one of the ways a frame ends up empty.
	Target        string        `json:"target"`
	TargetTexture gpu.TextureID `json:"targetTexture,omitempty"`
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
	Clip *RectView `json:"clip,omitempty"`
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
	VertexCount int          `json:"vertexCount,omitempty"`
	VertexBytes int          `json:"vertexBytes,omitempty"`
	Bounds      *RectView    `json:"bounds,omitempty"`
	Vertices    []VertexView `json:"vertices,omitempty"`
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
