package uiimpl

import (
	"sync"

	"github.com/dvoyni/cog/bundles/ui"
	"github.com/dvoyni/cog/bundles/ui/internal"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// armLayoutOnUpdate is the subscription type of the plugin's
// start-of-tick layout-snapshot handler on app.UpdateEvent. It is ordered
// First - ahead of every other subscriber, not merely in the first phase -
// because that is what makes "a tick that began after the request" decidable:
// an arm landing inside a tick whose frame is already being declared has to
// wait for the next one, and nothing later in the publication can tell the two
// apart. Its whole body is a mutex-guarded no-op when no snapshot is waiting.
type armLayoutOnUpdate kernel.Subscription[app.UpdateEvent]

// layoutOnUpdate is the subscription type of the plugin's end-of-tick
// layout-snapshot handler on app.UpdateEvent. It is ordered
// After[ui.ProcessOnUpdate], which is the only window in which the tree can be
// read at all.
//
// The geometry alone would survive the tick: the Processor's nodes keep their rects,
// clips, layers and active flags untouched until the next flatten. But
// layoutNode.element points into the app's borrowed child storage, which the
// frame releases at the end of processUpdate, so id, visual and userData are
// unreadable afterwards even though the numbers beside them are not. A
// post-tick read compiles, runs, and returns plausible nonsense for half the
// fields; this is why the read is here, and why what it reads is rendered by
// internal.LayoutViewOf rather than exported.
type layoutOnUpdate kernel.Subscription[app.UpdateEvent]

// snapshotRequest is one live layout snapshot: the filter it was armed with,
// and where its result goes.
type snapshotRequest struct {
	filter ui.ArmLayoutRequest
	done   chan ui.LayoutSnapshot
}

// snapshotState is ui's one layout-snapshot slot. A request moves through two
// stages, and each holds at most one:
//
//	pending - waiting for a tick to begin after the request
//	armed   - a tick has begun; the snapshot is taken when it completes
//
// The pending stage is what makes the guarantee true: a snapshot shows the
// game as of a tick that *began* after the request, so an arm landing inside a
// tick whose frame is already declared waits for the next one rather than
// describing a tree the agent's own last action never reached.
//
// It is plugin-owned state with its own lock rather than a kernel resource,
// for the reason gfx's and canvas's slots are: shutdown has to complete a
// waiting request, Stop runs after the scheduler has stopped, and a stopped
// scheduler grants no locks.
type snapshotState struct {
	mu             sync.Mutex
	request        *snapshotRequest
	pending, armed bool
}

// arm installs a request, or reports that one is already live. A second layout
// snapshot is refused; a capture and the other packages' snapshots are
// separate slots and may be in flight alongside it, which is what makes arming
// them together describe one tick.
func (s *snapshotState) arm(request ui.ArmLayoutRequest) (*snapshotRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request != nil {
		return nil, ui.ErrLayoutBusy{}
	}
	live := &snapshotRequest{filter: request, done: make(chan ui.LayoutSnapshot, 1)}
	s.request, s.pending, s.armed = live, true, false
	return live, nil
}

// beginTick admits a waiting request to the tick that has just begun. It runs
// ahead of every other subscriber to app.UpdateEvent, before any producer
// declares a frame, so the tick it admits a request to is one that began after
// the arm.
func (s *snapshotState) beginTick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request == nil || !s.pending {
		return
	}
	s.pending, s.armed = false, true
}

// record renders the tree of the tick that has just been processed and hands
// it to whoever armed it. It runs after processUpdate, while the app's
// borrowed child storage is still valid and before the next tick's flatten
// reuses the node array.
//
// The build happens under the slot's own lock. It is bounded by the filter and
// does no I/O; the marshalling of the document and the disk write happen on
// the caller's goroutine, never here. The one encoding that cannot wait is
// userData, which stops being readable the moment this returns.
func (s *snapshotState) record(resource *processor, tick int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request == nil || !s.armed {
		return
	}
	request := s.request
	s.clear()
	view, err := internal.LayoutViewOf(&resource.Processor, request.filter.Subtree, request.filter.MaxDepth)
	// The channel is buffered to one and holds this slot's only send, so a full
	// one means the waiter has gone and the value is simply collected.
	select {
	case request.done <- ui.LayoutSnapshot{Layout: view, Tick: tick, Err: err}:
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
	case request.done <- ui.LayoutSnapshot{Err: ui.ErrLayoutAbandoned{}}:
	default:
	}
}

// clear drops the request and both stage tokens with it.
func (s *snapshotState) clear() {
	s.request, s.pending, s.armed = nil, false, false
}
