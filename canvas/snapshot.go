package canvas

import (
	"slices"
	"strconv"
	"sync"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/m"
)

// DrawsArmUpdateEventHandler is the subscription type of the plugin's
// start-of-tick draw-snapshot handler on app.UpdateEvent. It is ordered First -
// ahead of every other subscriber, not merely in the first phase - because
// that is what makes "a tick that began after the request" decidable: an arm
// landing inside a tick that is already recording has to wait for the next
// one, and nothing later in the publication can tell the two apart. Its whole
// body is a mutex-guarded no-op when no snapshot is waiting.
type DrawsArmUpdateEventHandler kernel.Subscription[app.UpdateEvent]

// DrawsUpdateEventHandler is the subscription type of the plugin's
// end-of-tick draw-snapshot handler on app.UpdateEvent. It reads the queue
// from the Last phase, ordered before canvas's own flush, which is the only
// moment at which the frame is both complete and still alive: ui records in
// the earlier phase, so what it drew is there too, and flushFrame's deferred
// reset has not run yet.
type DrawsUpdateEventHandler kernel.Subscription[app.UpdateEvent]

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

// keeps reports whether one op survives the kind filter.
func (r ArmDrawsRequest) keeps(kind OpKind) bool {
	return len(r.Kinds) == 0 || slices.Contains(r.Kinds, kind)
}

// keepsLayer reports whether one layer survives the layer range.
func (r ArmDrawsRequest) keepsLayer(layerID Layer) bool {
	if r.FromLayer != nil && int(layerID) < *r.FromLayer {
		return false
	}
	return r.ToLayer == nil || int(layerID) <= *r.ToLayer
}

// ArmDrawsResponse hands back the wait and the viewport.
type ArmDrawsResponse struct {
	// Done receives exactly one DrawsSnapshot and is buffered, so the game's
	// own goroutine never blocks on a caller that walked away.
	Done <-chan DrawsSnapshot
	// Viewport is the window as of the arm, which a capability body cannot read
	// for itself. A resize between the arm and the tick it binds to is a stated
	// non-guarantee, exactly as it is for a capture.
	Viewport app.Viewport
}

// DrawsSnapshot is one produced snapshot, or the reason there is none. One
// struct carries both so that a caller cannot handle one and forget the other.
type DrawsSnapshot struct {
	Draws DrawsView
	Err   error
}

// DrawsView is one tick's recorded canvas operations, rendered while they are
// still alive. It is not a copy of the queue: no queue outlives the tick that
// filled it, and between ticks the queue is empty rather than stale, so the
// view is produced inside the tick and shaped by the request that asked for
// it. Nothing in it aliases the queue's storage - opQueue.Ops documents that
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

// snapshotRequest is one live draw snapshot: the filter it was armed with, and
// where its result goes.
type snapshotRequest struct {
	filter ArmDrawsRequest
	done   chan DrawsSnapshot
}

// snapshotState is canvas's one draw-snapshot slot. A request moves through
// two stages, and each holds at most one:
//
//	pending - waiting for a tick to begin after the request
//	armed   - a tick has begun; the snapshot is taken when it completes
//
// The pending stage is what makes the guarantee true: a snapshot shows the
// game as of a tick that *began* after the request, so an arm landing inside a
// tick already recording waits for the next one rather than describing a
// half-recorded frame.
//
// It is plugin-owned state with its own lock rather than a kernel resource,
// for the reason gfx's slots are: shutdown has to complete a waiting request,
// Stop runs after the scheduler has stopped, and a stopped scheduler grants no
// locks.
type snapshotState struct {
	mu             sync.Mutex
	request        *snapshotRequest
	pending, armed bool
}

// arm installs a request, or reports that one is already live. A second draw
// snapshot is refused; a capture and the other packages' snapshots are
// separate slots and may be in flight alongside it, which is what makes arming
// them together describe one tick.
func (s *snapshotState) arm(request ArmDrawsRequest) (*snapshotRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request != nil {
		return nil, ErrDrawsBusy{}
	}
	live := &snapshotRequest{filter: request, done: make(chan DrawsSnapshot, 1)}
	s.request, s.pending, s.armed = live, true, false
	return live, nil
}

// beginTick admits a waiting request to the tick that has just begun. It runs
// ahead of every other subscriber to app.UpdateEvent, before anything records,
// so the tick it admits a request to is one that began after the arm.
func (s *snapshotState) beginTick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request == nil || !s.pending {
		return
	}
	s.pending, s.armed = false, true
}

// record produces the snapshot of the tick that has just been recorded and
// hands it to whoever armed it. It runs after ui and the app have recorded and
// before the flush resets the queue, which is the only moment at which the
// frame is both complete and still alive.
//
// The build happens under the slot's own lock. It is bounded by the filter and
// does no I/O; the marshalling and the disk write happen on the caller's
// goroutine, never here.
func (s *snapshotState) record(queue *OpQueue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request == nil || !s.armed {
		return
	}
	request := s.request
	s.clear()
	// The channel is buffered to one and holds this slot's only send, so a full
	// one means the waiter has gone and the value is simply collected.
	select {
	case request.done <- DrawsSnapshot{Draws: drawsViewOf(queue, request.filter)}:
	default:
	}
}

// abandon completes a live request as a failure on the channel a result would
// have used. Shutdown is the case it exists for: a request armed in a tick the
// engine never finishes would otherwise leave its waiter learning nothing
// until its client's idle abort.
func (s *snapshotState) abandon() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request == nil {
		return
	}
	request := s.request
	s.clear()
	select {
	case request.done <- DrawsSnapshot{Err: ErrDrawsAbandoned{}}:
	default:
	}
}

// clear drops the request and both stage tokens with it.
func (s *snapshotState) clear() {
	s.request, s.pending, s.armed = nil, false, false
}

// drawsViewOf renders one tick's queue. It walks the queue itself rather than
// calling opQueue.Ops, for two reasons that pull the same way: Ops materialises
// every operation in the frame whatever the filter keeps, where the filter is
// precisely what is meant to bound the in-tick work; and the slices Ops hands
// back alias the queue's storage until the next reset, so a snapshot built on
// them would have to prove nothing escaped. Here each op is inspected only if
// it is kept, and rendered into owned values before the next one is looked at.
func drawsViewOf(queue *OpQueue, request ArmDrawsRequest) DrawsView {
	view := DrawsView{FromLayer: request.FromLayer, ToLayer: request.ToLayer}
	for _, kind := range request.Kinds {
		view.Kinds = append(view.Kinds, opKindName(kind))
	}

	layers := make([]Layer, 0, len(queue.ops))
	for layerID, value := range queue.ops {
		if speaks(value) {
			layers = append(layers, layerID)
		}
	}
	slices.Sort(layers)

	// The index counts every op the frame recorded, in flush order, whether or
	// not the filter emits it. That is what makes it an address rather than a
	// position in this particular reply.
	index := 0
	for _, layerID := range layers {
		value := queue.ops[layerID]
		view.LayerCount++
		keep := request.keepsLayer(layerID)
		if keep {
			view.Layers = append(view.Layers, layerViewOf(layerID, value))
		} else {
			view.OmittedLayers++
		}
		for i := range value.ops {
			at := index
			index++
			view.OpCount++
			if !keep || !request.keeps(opKindOf(value.ops[i].kind)) {
				view.OmittedOps++
				continue
			}
			op := queue.inspectOp(layerID, &value.ops[i])
			view.Ops = append(view.Ops, opViewOf(at, op, slices.Contains(request.Vertices, at)))
		}
	}
	return view
}

// speaks reports whether a layer says anything about this frame. The queue's
// map keeps its keys across a reset, so a layer that drew once and never again
// is still in it with everything zeroed, and reporting those would fill every
// response with layers the frame never touched.
func speaks(value layer) bool {
	return len(value.ops) > 0 || value.hasColor ||
		value.window != (m.Rect{}) || value.target != (gfx.TargetDescr{})
}

// layerViewOf renders one layer's coordinate frame and attachment.
func layerViewOf(layerID Layer, value layer) LayerView {
	view := LayerView{Layer: int(layerID), Target: "screen", Ops: len(value.ops)}
	if value.window != (m.Rect{}) {
		window := rectViewOf(value.window)
		view.Window, view.Aspect = &window, aspectModeName(value.aspect)
	}
	if texture, mip, arrayLayer, ok := value.target.Texture(); ok {
		view.Target, view.TargetTexture = "texture", texture
		view.TargetMip, view.TargetLayer = mip, arrayLayer
		view.TargetWidth, view.TargetHeight, _ = value.target.Size()
	} else if value.target.IsNone() {
		view.Target = "none"
	}
	if value.hasColor {
		view.Clear = colorComponents(value.clearColor)
	}
	return view
}

// opViewOf renders one operation at its record index, expanding its vertices
// only where the request named it.
func opViewOf(index int, op Op, expand bool) OpView {
	view := OpView{
		Index: index, Kind: opKindName(op.Kind), Layer: int(op.Layer),
		Params: gfx.ParameterViewsOf(op.Params),
	}
	if op.HasClip {
		clip := rectViewOf(op.Clip)
		view.Clip = &clip
	}
	if op.HasMaterial {
		material := gfx.MaterialViewOf(op.Material)
		view.Material = &material
	}
	if op.HasTexture {
		texture := gfx.TextureViewOf(op.Texture)
		view.Texture = &texture
	}
	switch op.Kind {
	case OpSprite:
		view.Path = op.Path
		transform := spriteTransformViewOf(op.Transform)
		view.Transform = &transform
	case OpText:
		view.FontPath, view.Text = op.FontPath, op.Text
		draw := textDrawViewOf(op.Draw)
		view.Draw = &draw
	case OpTriangles:
		view.VertexCount, view.VertexBytes = len(op.Vertices), op.VertexBytes
		if bounds, ok := vertexBounds(op.Vertices); ok {
			view.Bounds = &bounds
		}
		if expand {
			view.Vertices = vertexViewsOf(op.Vertices)
		}
	}
	return view
}

// spriteTransformViewOf renders one sprite placement.
func spriteTransformViewOf(transform SpriteTransform) SpriteTransformView {
	view := SpriteTransformView{
		Position: vec2Components(transform.Position),
		Scale:    transform.Scale, Rotation: transform.Rotation,
		NineSliceScale: transform.NineSliceScale, NineSliceNoCenter: transform.NineSliceNoCenter,
		FlipX: transform.FlipX, FlipY: transform.FlipY,
		TileX: transform.TileX, TileY: transform.TileY,
		Filter: gfx.FilterModeName(transform.Filter),
	}
	if transform.Size != (m.Vec2{}) {
		view.Size = vec2Components(transform.Size)
	}
	if transform.Origin != (m.Vec2{}) {
		view.Origin = vec2Components(transform.Origin)
	}
	if transform.Frame != (SpriteFrame{}) {
		frame := spriteFrameViewOf(transform.Frame)
		view.Frame = &frame
	}
	if transform.NineSlice != (SpriteFrame{}) {
		nineSlice := spriteFrameViewOf(transform.NineSlice)
		view.NineSlice = &nineSlice
	}
	return view
}

func spriteFrameViewOf(frame SpriteFrame) SpriteFrameView {
	return SpriteFrameView{
		Left: frame.Left, Top: frame.Top, Right: frame.Right, Bottom: frame.Bottom,
	}
}

// textDrawViewOf renders one text op's layout.
func textDrawViewOf(draw TextDraw) TextDrawView {
	return TextDrawView{
		Position: vec2Components(draw.Position), Size: draw.Size,
		Color: colorComponents(draw.Color), Align: textAlignName(draw.Align),
		WordWrapping: draw.WordWrapping, WrapWidth: draw.WrapWidth,
	}
}

// vertexViewsOf renders a triangle list in full. It is reached only for an op
// the request named by index, which is what keeps a frame of tens of thousands
// of vertices from arriving whole.
func vertexViewsOf(vertices []Vertex) []VertexView {
	if len(vertices) == 0 {
		return nil
	}
	views := make([]VertexView, len(vertices))
	for i, vertex := range vertices {
		views[i] = VertexView{
			Position: vec2Components(vertex.Position),
			Color:    colorComponents(vertex.Color),
			UV:       vec2Components(vertex.UV),
		}
	}
	return views
}

// vertexBounds is the axis-aligned box a triangle list's positions fall in,
// which is the part of a vertex list that answers "is it off screen" without
// being the list.
func vertexBounds(vertices []Vertex) (RectView, bool) {
	if len(vertices) == 0 {
		return RectView{}, false
	}
	left, top := vertices[0].Position.X, vertices[0].Position.Y
	right, bottom := left, top
	for _, vertex := range vertices[1:] {
		left, right = min(left, vertex.Position.X), max(right, vertex.Position.X)
		top, bottom = min(top, vertex.Position.Y), max(bottom, vertex.Position.Y)
	}
	return RectView{X: left, Y: top, Width: right - left, Height: bottom - top}, true
}

func rectViewOf(rect m.Rect) RectView {
	return RectView{X: rect.X, Y: rect.Y, Width: rect.Width, Height: rect.Height}
}

func vec2Components(value m.Vec2) []float32 { return []float32{value.X, value.Y} }

func colorComponents(value m.Color) []float32 {
	return []float32{value.R, value.G, value.B, value.A}
}

// The name tables. They are unexported for the reason gfx's are: naming an
// enum for a debug document is not the same promise as giving every canvas
// enum a String. An unknown value names its ordinal rather than falling back
// to a legal-looking name, so a member added without touching this file is
// visible instead of mislabelled.

func opKindName(kind OpKind) string {
	switch kind {
	case OpSprite:
		return "sprite"
	case OpText:
		return "text"
	case OpTriangles:
		return "triangles"
	}
	return unknownName(int(kind))
}

// opKindFor is the reverse of opKindName, for a request naming its filter in
// the same words the response answers in.
func opKindFor(name string) (OpKind, bool) {
	switch name {
	case "sprite":
		return OpSprite, true
	case "text":
		return OpText, true
	case "triangles":
		return OpTriangles, true
	}
	return 0, false
}

func aspectModeName(aspect AspectMode) string {
	switch aspect {
	case AspectInscribe:
		return "inscribe"
	case AspectOverlap:
		return "overlap"
	case AspectStretch:
		return "stretch"
	}
	return unknownName(int(aspect))
}

func textAlignName(align TextAlign) string {
	switch align {
	case AlignLeft:
		return "left"
	case AlignCenter:
		return "center"
	case AlignRight:
		return "right"
	}
	return unknownName(int(align))
}

// unknownName spells a value no table names, so an enum that grew reads as
// unknown rather than as whichever name happened to be last.
func unknownName(value int) string {
	return "unknown(" + strconv.Itoa(value) + ")"
}
