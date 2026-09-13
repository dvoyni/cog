package canvas

import (
	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/kernel"
)

// ArmDrawsCmd arms one draw snapshot and hands back the wait. It is ordinary
// canvas API: anything holding a kernel handle may ask what a tick recorded,
// and the agent-facing capability is one caller among them.
//
// The response's channel is the only delivery path, and refusals travel it
// too, because DrawsSnapshot carries Err - the same shape gfx's arms use, for
// the same reason: a channel passed in with the request would leave canvas
// unable to refuse a second arm synchronously.
type ArmDrawsCmd kernel.Command[ArmDrawsRequest, ArmDrawsResponse]

// ArmDrawsRequest carries the filter, because the filter is what bounds the
// work done inside the tick. Nothing about the output is decided before the
// request is known: the serialization runs in the tick, and by then it already
// knows what was asked for.
type ArmDrawsRequest struct {
	// FromLayer and ToLayer bound the layers kept, inclusive, and a nil one is
	// unbounded on that side. Layers they drop are counted rather than silently
	// missing, and every op keeps its own record index, so an index read off a
	// filtered snapshot still addresses the same op in an unfiltered one.
	FromLayer, ToLayer *int
	// Kinds keeps only the recording calls named. An empty list keeps them all.
	Kinds []OpKind
	// Vertices are the record indices of the triangle ops whose vertices are
	// returned in full. Every other triangle op reports a count and a bounding
	// box, which is the only shape a frame of tens of thousands of vertices can
	// come back in.
	Vertices []int
}

// ArmDrawsResponse hands back the wait and the viewport.
type ArmDrawsResponse struct {
	// Done receives exactly one DrawsSnapshot and is buffered, so the game's
	// own goroutine never blocks on a caller that walked away.
	Done <-chan DrawsSnapshot
	// Viewport is the window as of the arm, which a capability body cannot read
	// for itself. A resize between the arm and the tick it binds to is a stated
	// non-guarantee, exactly as it is for a capture.
	Viewport gfx.Viewport
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
