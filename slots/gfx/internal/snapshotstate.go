package internal

import (
	"cmp"
	"slices"
	"sync"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// frameOnUpdate is the subscription type of the plugin's
// start-of-tick frame-snapshot handler, admitFrame, on app.UpdateEvent. It is ordered First
// for the same reason captureOnUpdate is: a snapshot describes a
// tick that *began* after the request, and an arm landing inside a tick
// already running has to wait for the next one. Nothing later in the
// publication can tell those two apart.
//
// The snapshot itself is not taken here. It is taken at the other end of the
// tick, inside the present handler, because that is the only point at which
// the frame's queue is complete and still alive - see presentOnUpdate.
type frameOnUpdate kernel.Subscription[app.UpdateEvent]

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
		return nil, types.ErrFrameBusy{}
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
func (s *snapshotState) record(queue *OpQueue, resources *ResourceQueue, tick int64) {
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
	case request.done <- FrameSnapshot{
		Frame: frameViewOf(queue, resources, request.pass), Tick: tick,
	}:
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
	case request.done <- FrameSnapshot{Err: types.ErrFrameAbandoned{}}:
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
	view := FrameView{Filter: filter, PassCount: len(OpQueuePasses(queue))}

	draws := make([]int, len(OpQueuePasses(queue)))
	instances := make([]int, len(OpQueuePasses(queue)))
	for i := range OpQueueOps(queue) {
		op := &OpQueueOps(queue)[i]
		if op.Kind != OpDraw {
			continue
		}
		view.DrawCount++
		view.InstanceCount += op.Instances
		pass := int(op.Pass)
		if pass < 0 || pass >= len(OpQueuePasses(queue)) {
			view.StrayDraws++
			continue
		}
		draws[pass]++
		instances[pass] += op.Instances
	}

	order := make([]int, len(OpQueuePasses(queue)))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return cmp.Compare(OpQueuePasses(queue)[a].Desc.Order, OpQueuePasses(queue)[b].Desc.Order)
	})
	for run, index := range order {
		desc := OpQueuePasses(queue)[index].Desc
		if filter != "" && desc.Label != filter {
			view.OmittedPasses++
			continue
		}
		view.Passes = append(view.Passes, passViewOf(index, run, desc, draws[index], instances[index]))
	}

	view.ResourceOps = appendResourceOpViews(view.ResourceOps, "durable", ResourceQueueOps(resources))
	view.ResourceOps = appendResourceOpViews(view.ResourceOps, "frame", OpQueueOps(queue))
	return view
}

// passViewOf renders one declared pass, at its declaration index and its
// position in run order.
func passViewOf(index, run int, desc PassDescr, draws, instances int) PassView {
	view := PassView{
		Index: index, Run: run, Label: desc.Label, Order: int(desc.Order),
		Target:     targetKindName(TargetKindOf(&desc.Target)),
		Depth:      depthKindName(DepthKindOf(&desc.Depth)),
		Load:       desc.Load.String(),
		Store:      desc.Store.String(),
		DepthLoad:  desc.DepthLoad.String(),
		DepthClear: desc.DepthClear,
		DepthStore: desc.DepthStore.String(),
		Draws:      draws, Instances: instances,
		Runs: PassHasEffect(&desc, draws),
	}
	if TargetKindOf(&desc.Target) == TargetTexture {
		view.TargetTexture = TargetTextureOf(&desc.Target)
		view.TargetWidth, view.TargetHeight, _ = desc.Target.Size()
		view.TargetMip, view.TargetLayer = TargetMip(&desc.Target), TargetLayer(&desc.Target)
	}
	if desc.Depth.IsTexture() {
		view.DepthTexture = DepthTexture(&desc.Depth)
	}
	if desc.Load == types.LoadClear {
		view.Clear = []float32{desc.Clear.R, desc.Clear.G, desc.Clear.B, desc.Clear.A}
	}
	return view
}

// appendResourceOpViews renders one queue's resource operations, skipping its
// draws. The index carried is the position in that queue, which is what an op
// is addressed by.
func appendResourceOpViews(dst []ResourceOpView, queue string, ops []Op) []ResourceOpView {
	for i := range ops {
		if ops[i].Kind == OpDraw {
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
func resourceOpViewOf(queue string, index int, o *Op) ResourceOpView {
	view := ResourceOpView{Queue: queue, Index: index, Kind: opKindName(o.Kind)}
	switch o.Kind {
	case OpBakeBuffer:
		view.Buffer, view.BufferKind = o.BufferID, o.BufferKind.String()
		view.Size, view.Bytes = o.BufferSize, len(o.Bytes)
	case OpReleaseBuffer:
		view.Buffer = o.BufferID
	case OpBakeTexture:
		view.Texture, view.Width, view.Height = o.TextureID, o.TexW, o.TexH
		view.Format, view.Mipmaps, view.Bytes = o.Format.String(), o.Mipmaps, len(o.Bytes)
	case OpReleaseTexture:
		view.Texture = o.TextureID
	case OpAllocateTexture:
		view.Texture, view.Width, view.Height = o.TextureID, o.TexW, o.TexH
		view.Layers, view.Format, view.Renderable = o.TexLayers, o.Format.String(), o.Renderable
	case OpUpdateTexture:
		region := o.Region
		view.Texture, view.Layer, view.Region, view.Bytes = o.TextureID, o.TexLayer, &region, len(o.Bytes)
	case OpReleaseCachedResource:
		view.Path = o.Path
	}
	return view
}

func opKindName(kind OpKind) string {
	switch kind {
	case OpDraw:
		return "draw"
	case OpBakeBuffer:
		return "bakeBuffer"
	case OpReleaseBuffer:
		return "releaseBuffer"
	case OpBakeTexture:
		return "bakeTexture"
	case OpReleaseTexture:
		return "releaseTexture"
	case OpReleaseCachedResource:
		return "releaseCachedResource"
	case OpFreeCachedResources:
		return "freeCachedResources"
	case OpAllocateTexture:
		return "allocateTexture"
	case OpUpdateTexture:
		return "updateTexture"
	}
	return UnknownName(int(kind))
}

func targetKindName(kind TargetKind) string {
	switch kind {
	case TargetScreen:
		return "screen"
	case TargetNone:
		return "none"
	case TargetTexture:
		return "texture"
	}
	return UnknownName(int(kind))
}

func depthKindName(kind DepthKind) string {
	switch kind {
	case DepthKindAuto:
		return "auto"
	case DepthKindNone:
		return "none"
	case DepthKindTexture:
		return "texture"
	}
	return UnknownName(int(kind))
}
