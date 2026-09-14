package internal

import (
	"sync"

	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// captureRequest is one live capture or burst: where its stills go, and how far
// through them the engine has got.
type captureRequest struct {
	target   gfx.CaptureDesc
	amount   int
	interval int
	// bounds counts the stills bound to a tick so far, and delivered the ones
	// whose pixels have been sent. Ordinals are contiguous from zero, so a
	// short burst is a prefix rather than a set with holes in it.
	bounds    int
	delivered int
	// ticks is how many more ticks must begin before the next still binds.
	ticks int
	done  chan gfx.Capture
}

// captureState is gfx's one capture slot. A still moves through it in four
// stages, and each stage holds at most one, which is what keeps a burst from
// outrunning the single readback the backend allows:
//
//	pending  - waiting for a tick to begin after the request
//	armed    - a tick has begun; it binds when that tick completes
//	bound    - it rides the next render, whichever queue that render draws
//	inflight - the copy is encoded; its readback resolves a frame later
//
// The pending stage is what makes the guarantee true: a capture shows the game
// as of a tick that *began* after the request, so an arm landing inside a tick
// already running waits for the next one rather than binding to it.
//
// It is plugin-owned state with its own lock rather than a kernel resource,
// which is the one thing here that is forced rather than chosen: shutdown has
// to complete a waiting capture, Stop runs after the scheduler has stopped,
// and a stopped scheduler grants no locks - so a resource would be unreachable
// from the very path abandonment exists for.
type captureState struct {
	mu                              sync.Mutex
	request                         *captureRequest
	pending, armed, bound, inflight bool
}

// arm installs a request, or reports that one is already live. Refusing here,
// synchronously, is the first of the two refusal sites: the backend refuses a
// second in-flight map as well, because capture is a public gfx feature and a
// game's own code may arm one.
func (s *captureState) arm(request gfx.ArmCaptureRequest) (*captureRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request != nil {
		return nil, gfx.ErrCaptureBusy{}
	}
	amount := max(request.Amount, 1)
	interval := max(request.Interval, 1)
	switch {
	case amount > types.MaxCaptureAmount:
		return nil, gfx.ErrCaptureAmount{Amount: amount, Max: types.MaxCaptureAmount}
	case amount*interval > types.MaxCaptureSpan:
		return nil, gfx.ErrCaptureSpan{Ticks: amount * interval, Max: types.MaxCaptureSpan}
	case amount > 1 && request.Paused:
		return nil, gfx.ErrCaptureBurstPaused{}
	}
	live := &captureRequest{
		target: request.Target, amount: amount, interval: interval,
		done: make(chan gfx.Capture, amount),
	}
	s.request = live
	// A paused engine will complete no further tick, so the last one already is
	// the present and the still binds straight to the next render. The
	// guarantee is satisfied vacuously rather than weakened.
	s.pending, s.armed, s.bound, s.inflight = !request.Paused, false, request.Paused, false
	if request.Paused {
		live.bounds = 1
	}
	return live, nil
}

// beginTick moves a waiting still into the tick that has just begun, and
// counts down the interval between the stills of a burst. It runs ahead of
// every other subscriber to app.UpdateEvent, before anything records, so the
// tick it admits a still to is one that began after the arm.
func (s *captureState) beginTick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request == nil || !s.pending || s.armed {
		return
	}
	if s.request.ticks > 0 {
		s.request.ticks--
		return
	}
	s.pending, s.armed = false, true
}

// endTick binds the still admitted by beginTick to the tick that has just
// completed. It runs last, beside the queue swap, so the still rides the ready
// slot: if a newer recorded queue displaces the pending one before a render,
// the capture goes with the newer queue and the guarantee only strengthens.
func (s *captureState) endTick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request == nil || !s.armed || s.bound {
		return
	}
	s.armed, s.bound = false, true
	s.request.bounds++
	if s.request.bounds < s.request.amount {
		s.pending, s.request.ticks = true, s.request.interval-1
	}
}

// target reports what the frame about to be rendered should read back.
func (s *captureState) target() (gfx.CaptureDesc, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request == nil || !s.bound {
		return gfx.CaptureDesc{}, false
	}
	return s.request.target, true
}

// encoded records that the render just executed carried the capture op, so its
// result is now the backend's to hand back.
func (s *captureState) encoded() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request == nil || !s.bound {
		return
	}
	s.bound, s.inflight = false, true
}

// deliver hands one completed readback to whoever armed it, and ends the
// request when it was the last. The send is non-blocking: the channel is
// buffered to the whole burst, so a full one means the waiter has gone, and a
// value nobody receives is simply collected.
//
// A failure ends the request wherever it lands. A burst truncates rather than
// failing, and the ordinals already delivered are the short success.
func (s *captureState) deliver(capture gfx.Capture) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request == nil || !s.inflight {
		return
	}
	s.inflight = false
	select {
	case s.request.done <- capture:
	default:
	}
	s.request.delivered++
	if capture.Err != nil || s.request.delivered >= s.request.amount {
		s.clear()
	}
}

// abandon completes a live request as a failure on the channel a result would
// have used. Shutdown is the case it exists for: a capture armed in frame N
// resolves in N+1, and if the window closes between them the waiter would
// otherwise learn nothing until its client's idle abort.
func (s *captureState) abandon() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request == nil {
		return
	}
	select {
	case s.request.done <- gfx.Capture{Err: gfx.ErrCaptureAbandoned{}}:
	default:
	}
	s.clear()
}

// clear drops the request and every stage token with it.
func (s *captureState) clear() {
	s.request = nil
	s.pending, s.armed, s.bound, s.inflight = false, false, false, false
}
