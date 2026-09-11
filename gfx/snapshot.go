package gfx

import (
	"cmp"
	"slices"
	"sync"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

// FrameUpdateEventHandler is the subscription type of the plugin's
// start-of-tick frame-snapshot handler on app.UpdateEvent. It is ordered First
// for the same reason CaptureUpdateEventHandler is: a snapshot describes a
// tick that *began* after the request, and an arm landing inside a tick
// already running has to wait for the next one. Nothing later in the
// publication can tell those two apart.
//
// The snapshot itself is not taken here. It is taken at the other end of the
// tick, inside the present handler, because that is the only point at which
// the frame's queue is complete and still alive - see presentOnUpdate.
type FrameUpdateEventHandler kernel.Subscription[app.UpdateEvent]

// ArmFrameCmd arms one frame snapshot and hands back the wait. It is ordinary
// gfx API: anything holding a kernel handle may ask what the renderer was told
// to do for a tick, and the agent-facing capability is one caller among them.
//
// The response's channel is the only delivery path, and refusals travel it too,
// because FrameSnapshot carries Err - the same shape ArmCaptureCmd uses, for
// the same reason: a channel passed in with the request would leave gfx unable
// to refuse a second arm synchronously.
type ArmFrameCmd kernel.Command[ArmFrameRequest, ArmFrameResponse]

// ArmFrameRequest carries the filter, because the filter is what bounds the
// work done inside the tick. Nothing about the output is decided before the
// request is known.
type ArmFrameRequest struct {
	// Pass, when set, keeps only the passes whose Label is exactly it. Passes
	// it drops are counted in the result rather than silently missing, and
	// every pass keeps its own declaration index, so an index read off a
	// filtered snapshot still addresses the same pass in an unfiltered one.
	Pass string
}

// ArmFrameResponse hands back the wait and the viewport.
type ArmFrameResponse struct {
	// Done receives exactly one FrameSnapshot and is buffered, so the game's
	// own goroutine never blocks on a caller that walked away.
	Done <-chan FrameSnapshot
	// Viewport is the window as of the arm, which a capability body cannot
	// read for itself. A resize between the arm and the tick it binds to is a
	// stated non-guarantee, exactly as it is for a capture.
	Viewport app.Viewport
}

// FrameSnapshot is one produced snapshot, or the reason there is none. One
// struct carries both so that a caller cannot handle one and forget the other.
type FrameSnapshot struct {
	Frame FrameView
	Err   error
}

// FrameView is one tick's renderer declarations, rendered while they are still
// alive. It is not a copy of the queue: no queue outlives the tick that filled
// it, and between ticks the queue is empty rather than stale, so the view is
// produced inside the tick and shaped by the request that asked for it.
//
// Every index in it is a source index - a position in the queue that recorded
// the thing, never a position in the emitted array. Filtering makes the
// emitted array a subset, and if indices were positions in that subset every
// cross-reference would point at the wrong thing. With source indices the
// index is the address, and an elided pass stays addressable for free.
type FrameView struct {
	// Passes are the frame's declared passes in run order - Order first,
	// declaration sequence breaking ties - which is the order the translator
	// runs them in.
	Passes []PassView `json:"passes,omitempty"`
	// ResourceOps are the frame's resource traffic: the durable queue's
	// pending operations first, then the frame queue's own, which is the order
	// the translator emits them in. They belong to no pass - every bake is
	// hoisted ahead of all of them - so they carry no pass index and a pass
	// filter never drops them: "was the texture ever baked" is half of what
	// this answers, and a filter that hid it would hide the answer.
	ResourceOps []ResourceOpView `json:"resourceOps,omitempty"`
	// PassCount, DrawCount and InstanceCount are the whole frame's, whatever
	// the filter kept, so a filtered response still says how much of the frame
	// it is describing.
	PassCount     int `json:"passCount"`
	DrawCount     int `json:"drawCount"`
	InstanceCount int `json:"instanceCount"`
	// StrayDraws are draws recorded outside every pass. They are dropped by
	// the renderer and reported as ErrDrawWithoutPass, and they are the answer
	// to "nothing is on screen" often enough to be named here too.
	StrayDraws int `json:"strayDraws,omitempty"`
	// Filter echoes the pass label the request asked for, and OmittedPasses is
	// how many passes it dropped. Whatever a snapshot omits it says it
	// omitted: a tool that silently truncates cannot be told from a game that
	// drew nothing.
	Filter        string `json:"filter,omitempty"`
	OmittedPasses int    `json:"omittedPasses,omitempty"`
}

// PassView is one declared render pass: where it draws, in what order, what
// happens to its attachments at either end, and how much work it carries.
type PassView struct {
	// Index is the pass's declaration index in the frame's queue: its address,
	// stable under filtering. Run is its position in the frame's whole run
	// order, which is what "a pass ordered wrong" is read off.
	Index int    `json:"index"`
	Run   int    `json:"run"`
	Label string `json:"label,omitempty"`
	Order int    `json:"order"`
	// Target is screen, texture or none. A pass drawing into something that is
	// not the screen, and nothing compositing it afterwards, is one of the
	// ways a frame ends up black.
	Target        string    `json:"target"`
	TargetTexture TextureID `json:"targetTexture,omitempty"`
	TargetWidth   int       `json:"targetWidth,omitempty"`
	TargetHeight  int       `json:"targetHeight,omitempty"`
	TargetMip     int       `json:"targetMip,omitempty"`
	TargetLayer   int       `json:"targetLayer,omitempty"`
	// Depth is auto, texture or none.
	Depth        string    `json:"depth"`
	DepthTexture TextureID `json:"depthTexture,omitempty"`
	Load         string    `json:"load"`
	// Clear is the colour the pass clears to - r, g, b, a - and is present
	// only when Load is clear.
	Clear      []float32 `json:"clear,omitempty"`
	Store      string    `json:"store"`
	DepthLoad  string    `json:"depthLoad"`
	DepthClear float32   `json:"depthClear,omitempty"`
	DepthStore string    `json:"depthStore"`
	// Draws and Instances are counted rather than listed, because a draw's
	// mesh and material are opaque handles with nothing to resolve them
	// against. The aggregate is the informative part, and it is exact.
	Draws     int `json:"draws"`
	Instances int `json:"instances"`
	// Runs reports whether the pass is observable at all. A pass with no draws
	// that loads nothing is skipped by the translator, which is the difference
	// between a pass that ran and drew nothing and a pass that never ran.
	Runs bool `json:"runs"`
}

// ResourceOpView is one resource operation: what it does, to which handle, and
// how big the thing is. Bulk bytes are reported as a count and left where they
// are.
type ResourceOpView struct {
	// Queue is durable or frame: the persistent ResourceQueue, whose
	// operations wait until the render thread consumes them, or the frame's
	// own queue, whose uploads live and die with the frame. Index is the
	// position within that queue, so the address is the pair.
	Queue string `json:"queue"`
	Index int    `json:"index"`
	Kind  string `json:"kind"`
	// Path is the resource path, for the operations that name one.
	Path string `json:"path,omitempty"`
	// Buffer, BufferKind and Size describe a buffer operation.
	Buffer     BufferID `json:"buffer,omitempty"`
	BufferKind string   `json:"bufferKind,omitempty"`
	Size       int      `json:"size,omitempty"`
	// Texture and the fields after it describe a texture operation.
	Texture    TextureID `json:"texture,omitempty"`
	Width      int       `json:"width,omitempty"`
	Height     int       `json:"height,omitempty"`
	Layers     int       `json:"layers,omitempty"`
	Layer      int       `json:"layer,omitempty"`
	Region     *Region   `json:"region,omitempty"`
	Format     string    `json:"format,omitempty"`
	Mipmaps    bool      `json:"mipmaps,omitempty"`
	Renderable bool      `json:"renderable,omitempty"`
	// Bytes is how much data the operation uploads. The data itself does not
	// travel: a baked texture in a response is a base64 megabyte nobody asked
	// for.
	Bytes int `json:"bytes,omitempty"`
}

// snapshotRequest is one live frame snapshot: the filter it was armed with,
// and where its result goes.
type snapshotRequest struct {
	pass string
	done chan FrameSnapshot
}

// snapshotState is gfx's one frame-snapshot slot. A request moves through two
// stages, and each holds at most one:
//
//	pending - waiting for a tick to begin after the request
//	armed   - a tick has begun; the snapshot is taken when it completes
//
// The pending stage is what makes the guarantee true: a snapshot shows the
// game as of a tick that *began* after the request, so an arm landing inside a
// tick already running waits for the next one rather than describing it half
// recorded.
//
// It is plugin-owned state with its own lock rather than a kernel resource,
// for the reason captureState gives: shutdown has to complete a waiting
// request, Stop runs after the scheduler has stopped, and a stopped scheduler
// grants no locks.
type snapshotState struct {
	mu             sync.Mutex
	request        *snapshotRequest
	pending, armed bool
}

// arm installs a request, or reports that one is already live. A second
// snapshot of this kind is refused; a capture and the other packages'
// snapshots are separate slots and may be in flight alongside it, which is
// what makes arming them together describe one tick.
func (s *snapshotState) arm(request ArmFrameRequest) (*snapshotRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request != nil {
		return nil, ErrFrameBusy{}
	}
	live := &snapshotRequest{pass: request.Pass, done: make(chan FrameSnapshot, 1)}
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

// record produces the snapshot of the tick that has just completed and hands
// it to whoever armed it. It runs beside the queue swap, after canvas has
// flushed into the queue and before present swaps it away, which is the only
// moment at which the frame is both complete and still alive.
//
// The build happens under the slot's own lock. It is bounded by the filter and
// does no I/O; the marshalling and the disk write happen on the caller's
// goroutine, never here.
func (s *snapshotState) record(queue *OpQueue, resources *ResourceQueue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request == nil || !s.armed {
		return
	}
	request := s.request
	s.clear()
	// The channel is buffered to one and holds this slot's only send, so a
	// full one means the waiter has gone and the value is simply collected.
	select {
	case request.done <- FrameSnapshot{Frame: frameViewOf(queue, resources, request.pass)}:
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
	case request.done <- FrameSnapshot{Err: ErrFrameAbandoned{}}:
	default:
	}
}

// clear drops the request and both stage tokens with it.
func (s *snapshotState) clear() {
	s.request, s.pending, s.armed = nil, false, false
}

// frameViewOf renders one tick's queues. The pass order it walks is the
// translator's own - Order first, declaration sequence breaking ties - so what
// an agent reads as run order is the order the GPU sees.
func frameViewOf(queue *OpQueue, resources *ResourceQueue, filter string) FrameView {
	view := FrameView{Filter: filter, PassCount: len(queue.passes)}

	draws := make([]int, len(queue.passes))
	instances := make([]int, len(queue.passes))
	for i := range queue.ops {
		op := &queue.ops[i]
		if op.kind != opDraw {
			continue
		}
		view.DrawCount++
		view.InstanceCount += op.instances
		pass := int(op.pass)
		if pass < 0 || pass >= len(queue.passes) {
			view.StrayDraws++
			continue
		}
		draws[pass]++
		instances[pass] += op.instances
	}

	order := make([]int, len(queue.passes))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return cmp.Compare(queue.passes[a].desc.Order, queue.passes[b].desc.Order)
	})
	for run, index := range order {
		desc := queue.passes[index].desc
		if filter != "" && desc.Label != filter {
			view.OmittedPasses++
			continue
		}
		view.Passes = append(view.Passes, passViewOf(index, run, desc, draws[index], instances[index]))
	}

	view.ResourceOps = appendResourceOpViews(view.ResourceOps, "durable", resources.ops)
	view.ResourceOps = appendResourceOpViews(view.ResourceOps, "frame", queue.ops)
	return view
}

// passViewOf renders one declared pass, at its declaration index and its
// position in run order.
func passViewOf(index, run int, desc PassDescr, draws, instances int) PassView {
	view := PassView{
		Index: index, Run: run, Label: desc.Label, Order: int(desc.Order),
		Target:     targetKindName(desc.Target.kind),
		Depth:      depthKindName(desc.Depth.kind),
		Load:       loadOpName(desc.Load),
		Store:      storeOpName(desc.Store),
		DepthLoad:  loadOpName(desc.DepthLoad),
		DepthClear: desc.DepthClear,
		DepthStore: storeOpName(desc.DepthStore),
		Draws:      draws, Instances: instances,
		Runs: desc.hasEffect(draws),
	}
	if desc.Target.kind == targetTexture {
		view.TargetTexture = desc.Target.texture
		view.TargetWidth, view.TargetHeight, _ = desc.Target.Size()
		view.TargetMip, view.TargetLayer = desc.Target.mip, desc.Target.layer
	}
	if desc.Depth.IsTexture() {
		view.DepthTexture = desc.Depth.texture
	}
	if desc.Load == LoadClear {
		view.Clear = []float32{desc.Clear.R, desc.Clear.G, desc.Clear.B, desc.Clear.A}
	}
	return view
}

// appendResourceOpViews renders one queue's resource operations, skipping its
// draws. The index carried is the position in that queue, which is what an op
// is addressed by.
func appendResourceOpViews(dst []ResourceOpView, queue string, ops []op) []ResourceOpView {
	for i := range ops {
		if ops[i].kind == opDraw {
			continue
		}
		dst = append(dst, resourceOpViewOf(queue, i, &ops[i]))
	}
	return dst
}

// resourceOpViewOf renders one resource operation, carrying only the fields
// its kind gives meaning to. The op struct is one flat union shared by every
// kind, so emitting all of it would put eight irrelevant zeroes beside each
// answer.
func resourceOpViewOf(queue string, index int, o *op) ResourceOpView {
	view := ResourceOpView{Queue: queue, Index: index, Kind: opKindName(o.kind)}
	switch o.kind {
	case opBakeBuffer:
		view.Buffer, view.BufferKind = o.bufferID, bufferKindName(o.bufferKind)
		view.Size, view.Bytes = o.bufferSize, len(o.bytes)
	case opReleaseBuffer:
		view.Buffer = o.bufferID
	case opBakeTexture:
		view.Texture, view.Width, view.Height = o.textureID, o.texW, o.texH
		view.Format, view.Mipmaps, view.Bytes = formatName(o.format), o.mipmaps, len(o.bytes)
	case opReleaseTexture:
		view.Texture = o.textureID
	case opAllocateTexture:
		view.Texture, view.Width, view.Height = o.textureID, o.texW, o.texH
		view.Layers, view.Format, view.Renderable = o.texLayers, formatName(o.format), o.renderable
	case opUpdateTexture:
		region := o.region
		view.Texture, view.Layer, view.Region, view.Bytes = o.textureID, o.texLayer, &region, len(o.bytes)
	case opReleaseCachedResource:
		view.Path = o.path
	}
	return view
}

func opKindName(kind opKind) string {
	switch kind {
	case opDraw:
		return "draw"
	case opBakeBuffer:
		return "bakeBuffer"
	case opReleaseBuffer:
		return "releaseBuffer"
	case opBakeTexture:
		return "bakeTexture"
	case opReleaseTexture:
		return "releaseTexture"
	case opReleaseCachedResource:
		return "releaseCachedResource"
	case opFreeCachedResources:
		return "freeCachedResources"
	case opAllocateTexture:
		return "allocateTexture"
	case opUpdateTexture:
		return "updateTexture"
	}
	return unknownName(int(kind))
}

func targetKindName(kind targetKind) string {
	switch kind {
	case targetScreen:
		return "screen"
	case targetNone:
		return "none"
	case targetTexture:
		return "texture"
	}
	return unknownName(int(kind))
}

func depthKindName(kind depthKind) string {
	switch kind {
	case depthKindAuto:
		return "auto"
	case depthKindNone:
		return "none"
	case depthKindTexture:
		return "texture"
	}
	return unknownName(int(kind))
}
