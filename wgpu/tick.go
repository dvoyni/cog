package wgpu

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/dvoyni/cog/app"
)

// tickSource decides when an update tick is published: the driver's frame
// clock while running, or an explicit step while paused. Rendering is not a
// tick source — a paused engine keeps drawing the last completed frame, so
// pause stops app.UpdateEvent publication and nothing else.
//
// Its state is atomics rather than a kernel resource because onUpdate is a
// driver callback holding an Executioner, not a handler holding a lock:
// reading a resource would cost a dispatch every frame merely to ask whether
// to tick. The thread boundary it crosses — a command handler on some caller's
// goroutine against onUpdate on the main thread — is the same one alpha and
// frameSeq already cross, and for the same reason.
//
// mu guards only the handover of one batch of steps to the frame that
// publishes it, which is the one part of this that two atomics cannot express
// without a window where a step is counted twice or released early. A running
// frame reads one atomic and takes no lock; a paused frame with nothing
// pending reads two.
type tickSource struct {
	paused   atomic.Bool
	pending  atomic.Int64
	advanced atomic.Int64

	mu    sync.Mutex
	batch *stepBatch
}

// stepBatch is one handover of requested steps to the frame that publishes
// them. Everything waiting on the same batch is describing the same ticks,
// which is what lets several observers pair a moment on one step.
type stepBatch struct {
	done chan struct{}
	// sharing counts the callers waiting on this batch: the step's audience.
	sharing atomic.Int64
	// published is how many ticks the batch produced, valid once done is
	// closed. A batch abandoned by a resume publishes none.
	published int
}

// take reports the steps this frame should publish and whether the tick source
// is paused. The returned batch, when non-nil, is what published must close
// once the ticks are out.
//
// It runs on the main thread every frame, which is why the common paths are
// atomic loads: this is the read the whole design exists to keep cheap.
func (t *tickSource) take() (steps int, paused bool, batch *stepBatch) {
	if !t.paused.Load() {
		return 0, false, nil
	}
	if t.pending.Load() == 0 {
		return 0, true, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	steps, batch = int(t.pending.Swap(0)), t.batch
	t.batch = nil
	return steps, true, batch
}

// published records the ticks the frame put out and releases whoever asked for
// them. It runs on the main thread, immediately after the last step.
func (t *tickSource) published(steps int, batch *stepBatch) {
	if steps > 0 {
		t.advanced.Add(int64(steps))
	}
	if batch != nil {
		batch.published = steps
		close(batch.done)
	}
}

// control applies one time-control request and reports the tick source as the
// call left it. A step does not return until its ticks have been published, or
// until ctx ends; steps already requested when it ends are still published,
// which is why the agent-facing surface caps how many may be asked for.
func (t *tickSource) control(ctx context.Context, request app.TimeRequest) (app.TimeResponse, error) {
	switch request.Action {
	case app.TimeStatus:
		return t.state(app.TimeResponse{}), nil
	case app.TimePause:
		return t.state(app.TimeResponse{Changed: t.pause()}), nil
	case app.TimeResume:
		return t.state(app.TimeResponse{Changed: t.resume()}), nil
	case app.TimeStep:
		return t.step(ctx, request)
	default:
		return app.TimeResponse{}, ErrUnknownTimeAction{Action: request.Action}
	}
}

// pause stops update ticks and reports whether that changed anything. The
// advanced count belongs to one pause, so entering one resets it.
func (t *tickSource) pause() bool {
	if t.paused.Swap(true) {
		return false
	}
	t.advanced.Store(0)
	return true
}

// resume hands the tick source back to the frame clock and reports whether
// that changed anything. Nothing was banked while paused — onUpdate has been
// discarding frame time all along — so there is nothing to unwind and the
// first frame after this one is worth exactly one frame.
//
// A step still pending would never be published once the frame clock is back
// in charge, so it is abandoned here and its caller released, rather than left
// to wait out its deadline.
func (t *tickSource) resume() bool {
	was := t.paused.Swap(false)
	if t.pending.Load() == 0 {
		return was
	}
	t.mu.Lock()
	t.pending.Store(0)
	batch := t.batch
	t.batch = nil
	t.mu.Unlock()
	if batch != nil {
		close(batch.done)
	}
	return was
}

// step raises the requested ticks and waits for them. Stepping implies
// pausing: stepping a running engine is meaningless, so the request pauses
// rather than being refused.
func (t *tickSource) step(ctx context.Context, request app.TimeRequest) (app.TimeResponse, error) {
	steps := request.Steps
	if steps < 1 {
		steps = 1
	}
	batch, joined, changed := t.request(steps, request.Join)
	answer := app.TimeResponse{Changed: changed, Joined: joined}
	select {
	case <-batch.done:
	case <-ctx.Done():
		return t.state(answer), ctx.Err()
	}
	answer.Stepped = batch.published
	return t.state(answer), nil
}

// request raises steps, or joins the step already pending when join is set,
// and returns the batch to wait on.
//
// Joining is what makes several observers describe one tick: read per-caller,
// three arms landing together would be three steps on three different ticks,
// which is the precise opposite of what arming them together is for. An
// explicit step never joins — dropping ticks somebody asked for would be a
// silent lie — so only a caller that sets Join shares one.
func (t *tickSource) request(steps int, join bool) (batch *stepBatch, joined, changed bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	changed = t.pause()
	if join && t.pending.Load() > 0 {
		joined = true
	} else {
		t.pending.Add(int64(steps))
	}
	if t.batch == nil {
		t.batch = &stepBatch{done: make(chan struct{})}
	}
	t.batch.sharing.Add(1)
	return t.batch, joined, changed
}

// state fills in what every answer carries, whatever the action was.
func (t *tickSource) state(response app.TimeResponse) app.TimeResponse {
	response.Paused = t.paused.Load()
	response.Advanced = int(t.advanced.Load())
	return response
}
