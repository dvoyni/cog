package mcp

import (
	"sync"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/gfx/internal"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
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
// the frame's queue is complete and still alive - see
// internal.FrameObserver.Presenting.
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
// It is observer-owned state with its own lock rather than a kernel resource,
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
func (s *snapshotState) record(queue *internal.OpQueue, resources *internal.ResourceQueue, tick int64) {
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
		Frame: internal.FrameViewOf(queue, resources, request.pass), Tick: tick,
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
