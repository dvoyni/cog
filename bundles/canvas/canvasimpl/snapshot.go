package canvasimpl

import (
	"slices"
	"strconv"
	"sync"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/canvas/internal"
	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

// armDrawsOnUpdate is the subscription type of the plugin's
// start-of-tick draw-snapshot handler on app.UpdateEvent. It is ordered First -
// ahead of every other subscriber, not merely in the first phase - because
// that is what makes "a tick that began after the request" decidable: an arm
// landing inside a tick that is already recording has to wait for the next
// one, and nothing later in the publication can tell the two apart. Its whole
// body is a mutex-guarded no-op when no snapshot is waiting.
type armDrawsOnUpdate kernel.Subscription[app.UpdateEvent]

// drawsOnUpdate is the subscription type of the plugin's
// end-of-tick draw-snapshot handler on app.UpdateEvent. It reads the queue
// from the Last phase, ordered before canvas.FlushOnUpdate, which is the only
// moment at which the frame is both complete and still alive: ui records in
// the earlier phase, so what it drew is there too, and flushFrame's deferred
// reset has not run yet.
type drawsOnUpdate kernel.Subscription[app.UpdateEvent]

// keeps reports whether one op survives the kind filter.
func keeps(r canvas.ArmDrawsRequest, kind canvas.OpKind) bool {
	return len(r.Kinds) == 0 || slices.Contains(r.Kinds, kind)
}

// keepsLayer reports whether one layer survives the layer range.
func keepsLayer(r canvas.ArmDrawsRequest, layerID canvas.Layer) bool {
	if r.FromLayer != nil && int(layerID) < *r.FromLayer {
		return false
	}
	return r.ToLayer == nil || int(layerID) <= *r.ToLayer
}

// snapshotRequest is one live draw snapshot: the filter it was armed with, and
// where its result goes.
type snapshotRequest struct {
	filter canvas.ArmDrawsRequest
	done   chan canvas.DrawsSnapshot
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
func (s *snapshotState) arm(request canvas.ArmDrawsRequest) (*snapshotRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request != nil {
		return nil, canvas.ErrDrawsBusy{}
	}
	live := &snapshotRequest{filter: request, done: make(chan canvas.DrawsSnapshot, 1)}
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
func (s *snapshotState) record(queue *canvas.OpQueue, tick int64) {
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
	case request.done <- canvas.DrawsSnapshot{Draws: drawsViewOf(queue, request.filter), Tick: tick}:
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
	case request.done <- canvas.DrawsSnapshot{Err: canvas.ErrDrawsAbandoned{}}:
	default:
	}
}

// clear drops the request and both stage tokens with it.
func (s *snapshotState) clear() {
	s.request, s.pending, s.armed = nil, false, false
}

// drawsViewOf renders one tick's queue. It walks the queue itself rather than
// calling OpQueue.Ops, for two reasons that pull the same way: Ops materialises
// every operation in the frame whatever the filter keeps, where the filter is
// precisely what is meant to bound the in-tick work; and the slices Ops hands
// back alias the queue's storage until the next reset, so a snapshot built on
// them would have to prove nothing escaped. Here each op is inspected only if
// it is kept, and rendered into owned values before the next one is looked at.
func drawsViewOf(queue *canvas.OpQueue, request canvas.ArmDrawsRequest) canvas.DrawsView {
	view := canvas.DrawsView{FromLayer: request.FromLayer, ToLayer: request.ToLayer}
	for _, kind := range request.Kinds {
		view.Kinds = append(view.Kinds, opKindName(kind))
	}

	ops := internal.OpQueueLayers(queue)
	layers := make([]canvas.Layer, 0, len(ops))
	for layerID, value := range ops {
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
		value := ops[layerID]
		view.LayerCount++
		keep := keepsLayer(request, layerID)
		if keep {
			view.Layers = append(view.Layers, layerViewOf(layerID, value))
		} else {
			view.OmittedLayers++
		}
		for i := range value.Ops {
			at := index
			index++
			view.OpCount++
			if !keep || !keeps(request, internal.OpKindOf(value.Ops[i].Kind)) {
				view.OmittedOps++
				continue
			}
			op := internal.OpQueueInspect(queue, layerID, &value.Ops[i])
			view.Ops = append(view.Ops, opViewOf(at, op, slices.Contains(request.Vertices, at)))
		}
	}
	return view
}

// speaks reports whether a layer says anything about this frame. The queue's
// map keeps its keys across a reset, so a layer that drew once and never again
// is still in it with everything zeroed, and reporting those would fill every
// response with layers the frame never touched.
func speaks(value internal.LayerOps) bool {
	return len(value.Ops) > 0 || value.HasColor ||
		value.Window != (m.Rect{}) || value.Target != (gfx.TargetDescr{})
}

// layerViewOf renders one layer's coordinate frame and attachment.
func layerViewOf(layerID canvas.Layer, value internal.LayerOps) canvas.LayerView {
	view := canvas.LayerView{Layer: int(layerID), Target: "screen", Ops: len(value.Ops)}
	if value.Window != (m.Rect{}) {
		window := rectViewOf(value.Window)
		view.Window, view.Aspect = &window, aspectModeName(value.Aspect)
	}
	if texture, mip, arrayLayer, ok := value.Target.Texture(); ok {
		view.Target, view.TargetTexture = "texture", texture
		view.TargetMip, view.TargetLayer = mip, arrayLayer
		view.TargetWidth, view.TargetHeight, _ = value.Target.Size()
	} else if value.Target.IsNone() {
		view.Target = "none"
	}
	if value.HasColor {
		view.Clear = colorComponents(value.ClearColor)
	}
	return view
}

// opViewOf renders one operation at its record index, expanding its vertices
// only where the request named it.
func opViewOf(index int, op canvas.Op, expand bool) canvas.OpView {
	view := canvas.OpView{
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
	case canvas.OpSprite:
		view.Path = op.Path
		transform := spriteTransformViewOf(op.Transform)
		view.Transform = &transform
	case canvas.OpText:
		view.FontPath, view.Text = op.FontPath, op.Text
		draw := textDrawViewOf(op.Draw)
		view.Draw = &draw
	case canvas.OpTriangles:
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
func spriteTransformViewOf(transform canvas.SpriteTransform) canvas.SpriteTransformView {
	view := canvas.SpriteTransformView{
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
	if transform.Frame != (canvas.SpriteFrame{}) {
		frame := spriteFrameViewOf(transform.Frame)
		view.Frame = &frame
	}
	if transform.NineSlice != (canvas.SpriteFrame{}) {
		nineSlice := spriteFrameViewOf(transform.NineSlice)
		view.NineSlice = &nineSlice
	}
	return view
}

func spriteFrameViewOf(frame canvas.SpriteFrame) canvas.SpriteFrameView {
	return canvas.SpriteFrameView{
		Left: frame.Left, Top: frame.Top, Right: frame.Right, Bottom: frame.Bottom,
	}
}

// textDrawViewOf renders one text op's layout.
func textDrawViewOf(draw canvas.TextDraw) canvas.TextDrawView {
	return canvas.TextDrawView{
		Position: vec2Components(draw.Position), Size: draw.Size,
		Color: colorComponents(draw.Color), Align: textAlignName(draw.Align),
		WordWrapping: draw.WordWrapping, WrapWidth: draw.WrapWidth,
	}
}

// vertexViewsOf renders a triangle list in full. It is reached only for an op
// the request named by index, which is what keeps a frame of tens of thousands
// of vertices from arriving whole.
func vertexViewsOf(vertices []canvas.Vertex) []canvas.VertexView {
	if len(vertices) == 0 {
		return nil
	}
	views := make([]canvas.VertexView, len(vertices))
	for i, vertex := range vertices {
		views[i] = canvas.VertexView{
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
func vertexBounds(vertices []canvas.Vertex) (canvas.RectView, bool) {
	if len(vertices) == 0 {
		return canvas.RectView{}, false
	}
	left, top := vertices[0].Position.X, vertices[0].Position.Y
	right, bottom := left, top
	for _, vertex := range vertices[1:] {
		left, right = min(left, vertex.Position.X), max(right, vertex.Position.X)
		top, bottom = min(top, vertex.Position.Y), max(bottom, vertex.Position.Y)
	}
	return canvas.RectView{X: left, Y: top, Width: right - left, Height: bottom - top}, true
}

func rectViewOf(rect m.Rect) canvas.RectView {
	return canvas.RectView{X: rect.X, Y: rect.Y, Width: rect.Width, Height: rect.Height}
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

func opKindName(kind canvas.OpKind) string {
	switch kind {
	case canvas.OpSprite:
		return "sprite"
	case canvas.OpText:
		return "text"
	case canvas.OpTriangles:
		return "triangles"
	}
	return unknownName(int(kind))
}

// opKindFor is the reverse of opKindName, for a request naming its filter in
// the same words the response answers in.
func opKindFor(name string) (canvas.OpKind, bool) {
	switch name {
	case "sprite":
		return canvas.OpSprite, true
	case "text":
		return canvas.OpText, true
	case "triangles":
		return canvas.OpTriangles, true
	}
	return 0, false
}

func aspectModeName(aspect canvas.AspectMode) string {
	switch aspect {
	case canvas.AspectInscribe:
		return "inscribe"
	case canvas.AspectOverlap:
		return "overlap"
	case canvas.AspectStretch:
		return "stretch"
	}
	return unknownName(int(aspect))
}

func textAlignName(align canvas.TextAlign) string {
	switch align {
	case canvas.AlignLeft:
		return "left"
	case canvas.AlignCenter:
		return "center"
	case canvas.AlignRight:
		return "right"
	}
	return unknownName(int(align))
}

// unknownName spells a value no table names, so an enum that grew reads as
// unknown rather than as whichever name happened to be last.
func unknownName(value int) string {
	return "unknown(" + strconv.Itoa(value) + ")"
}
