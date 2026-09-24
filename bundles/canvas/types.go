package canvas

import (
	"github.com/dvoyni/cog/bundles/canvas/internal"
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

// OpKind identifies which recording call produced an Op.
type OpKind = internal.OpKind

const (
	OpSprite    = internal.OpSprite
	OpText      = internal.OpText
	OpTriangles = internal.OpTriangles
)

// Op is a read-only view of one recorded operation. OpQueue.Ops returns them in
// flush order so a recorder can assert what it produced — layer, order,
// transform, clip and parameters — without running the GPU pipeline. Op.Param
// and Op.ColorParam look a recorded parameter up by name.
type Op = internal.Op

// LookupAccess is a handler-scoped facade over a Lookup. It carries the kernel
// (for error reporting) and the read filesystem needed to lazily read sprite
// headers and font files, without ever retaining them past the handler's lock
// scope. Acquire a *Lookup write dependency plus storage.FileSystem in a
// handler, build a LookupAccess with NewLookupAccess, and pass it to consumers
// for the duration of that handler.
//
// Every verb it carries measures, and none of them touches the device, so a
// handler that lays a page out declares no *gfx.ResourceQueue and serialises
// against nothing that draws.
type LookupAccess = internal.LookupAccess

// LookupDeviceAccess is the half of the facade that needs the device: unloading
// an asset hands its texture, its atlas slot or its array straight back to gfx,
// so it takes a *gfx.ResourceQueue where LookupAccess takes a filesystem.
// Acquire a *Lookup and a *gfx.ResourceQueue write dependency in a handler,
// build one with NewLookupDeviceAccess, and never store the result.
//
// scene declares the same split under the same name, because what separates the
// two facades in both plugins is the device rather than what the verbs do.
type LookupDeviceAccess = internal.LookupDeviceAccess

// FontMetrics reports a font's vertical metrics at a given size, in logical
// pixels, for baseline placement and inline-icon alignment.
type FontMetrics = internal.FontMetrics

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
type MaterialSet = internal.MaterialSet

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
type HaloProfile = internal.HaloProfile

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
	TextureSlot  = internal.TextureSlot
	SamplerSlot  = internal.SamplerSlot
	TintSlot     = internal.TintSlot
	KeyColorSlot = internal.KeyColorSlot
)

const (
	// UniformsPath declares struct CanvasUniforms and the group 0 binding.
	// Include it only if you are NOT extending the block: an extending material
	// hand-writes those six lines, which is exactly why the block is its own
	// source.
	UniformsPath = internal.UniformsPath
	// ClipPath declares canvasClipped, and nothing else. Calling it is offered
	// rather than required, and a hand-written fs_main that omits it draws
	// outside the clip rectangle with no error anywhere.
	ClipPath = internal.ClipPath
	// SpriteBindingsPath declares the sprite family's group 1 sampler and array
	// texture, its group 2 instance buffer, and the SpriteInstance, Instances and
	// VertexOut structs.
	SpriteBindingsPath = internal.SpriteBindingsPath
	// SpriteVertexPath includes SpriteBindingsPath and declares vs_main. Include
	// it to replace only fs_main.
	SpriteVertexPath = internal.SpriteVertexPath
	// TrianglesBindingsPath declares the triangles family's group 1 sampler and
	// texture and its VertexOut.
	TrianglesBindingsPath = internal.TrianglesBindingsPath
	// TrianglesVertexPath includes TrianglesBindingsPath and declares vs_main.
	TrianglesVertexPath = internal.TrianglesVertexPath
	// KeyColorPath declares keyColorRamp, the sRGB transfer functions it is
	// written in, and the three key* constants, so a custom material wears the
	// exact ramp the built-ins do rather than a re-typed approximation.
	KeyColorPath = internal.KeyColorPath
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
	DefaultFontPath = internal.DefaultFontPath
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
type SpriteInstance = internal.SpriteInstance

// DrawsSnapshot is one produced snapshot, or the reason there is none. One
// struct carries both so that a caller cannot handle one and forget the other.
type DrawsSnapshot = internal.DrawsSnapshot

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
type DrawsView = internal.DrawsView

// LayerView is one canvas layer: the world window its ops are written in, what
// it draws into, and whether it clears.
type LayerView = internal.LayerView

// OpView is one recorded operation: which call produced it, where it sits, and
// what it was given. The fields a kind gives no meaning to are absent rather
// than zero, because one flat union emitting all of them would put a dozen
// empty values beside every answer.
type OpView = internal.OpView

// SpriteTransformView is one sprite's placement, with its enum named and its
// vectors rendered as the component arrays the view vocabulary uses for
// numbers. The zero fields are absent: a sprite sets two or three of these and
// means nothing by the rest.
type SpriteTransformView = internal.SpriteTransformView

// SpriteFrameView is a rectangle of source pixels given as insets from each
// edge, which is what SpriteFrame is.
type SpriteFrameView = internal.SpriteFrameView

// TextDrawView is one text op's layout and colour.
//
// It carries neither the draw's material nor its parameters, and that is not
// an omission: the op reports both already, its Material through the same
// MaterialView every other op uses and its Params as the recorded list, which
// for a text op is the draw's own.
type TextDrawView = internal.TextDrawView

// VertexView is one built-in vertex: position and uv in their own spaces, and
// the colour the shader multiplies by.
type VertexView = internal.VertexView

// RectView is a rectangle in layer world space, named so an agent reading a
// clip and a bounding box reads them the same way.
type RectView = internal.RectView
