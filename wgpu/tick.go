package wgpu

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dvoyni/cog/app"
)

// defaultHoldDuration and maxHoldDuration bound how long a hold may keep the
// step window open. The default is one second, which is long enough for
// several requests arriving over separate connections and short enough that
// an agent which forgets to release costs itself a second rather than a
// session; the cap is the ten seconds every other span in this family is
// capped at.
//
// A hold is the only wall-clock deadline in the tick source, and it is not an
// engine clock: it measures how long an absent agent may keep the engine from
// stepping, never how far the simulation has moved. There is still no time to
// scale and no Dt to distort.
const (
	defaultHoldDuration = time.Second
	maxHoldDuration     = 10 * time.Second
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
// mu guards the handover of one batch of steps to the frame that publishes
// it, and the hold that can postpone the handover. The handover is the one
// part of this that two atomics cannot express without a window where a step
// is counted twice or released early. A running frame reads one atomic and
// takes no lock; a paused frame with nothing pending reads two.
type tickSource struct {
	paused   atomic.Bool
	pending  atomic.Int64
	advanced atomic.Int64
	// tick numbers the ticks this source has published, from one, and never
	// resets. It is what lets everything recorded inside one tick say which
	// tick it was, and it costs one atomic add per published tick.
	tick atomic.Int64

	mu    sync.Mutex
	batch *stepBatch
	// holdUntil is when the hold on the step window runs out; the zero time
	// means no hold stands. holdExpired remembers that the last hold ended on
	// its deadline rather than being released, so that an agent coming back
	// to a window it thought it still had is told rather than left to infer
	// it from a split.
	holdUntil   time.Time
	holdExpired bool
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
//
// A hold makes it decline the batch it finds. That is the whole of the
// deterministic half of pairing: without it the join window is only as wide
// as the gap before the next rendered frame, and whether several arms share
// one tick depends on whether they all fit inside it. With it the window
// belongs to whoever took the hold.
func (t *tickSource) take() (steps int, paused bool, batch *stepBatch) {
	if !t.paused.Load() {
		return 0, false, nil
	}
	if t.pending.Load() == 0 {
		return 0, true, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	// Every frame is an observation, so a hold that has run out expires here
	// rather than needing a timer of its own, and the batch it was keeping
	// open goes out on this same frame.
	if t.holding(time.Now()) {
		return 0, true, nil
	}
	steps, batch = int(t.pending.Swap(0)), t.batch
	t.batch = nil
	return steps, true, batch
}

// next numbers the tick about to be published. It runs once per tick on the
// main thread and is one atomic add, which is what "cheap on the frame path"
// has to mean for something every tick pays.
func (t *tickSource) next() int64 {
	return t.tick.Add(1)
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
	case app.TimeHold:
		changed, err := t.hold(time.Now(), request.Hold)
		if err != nil {
			return app.TimeResponse{}, err
		}
		return t.state(app.TimeResponse{Changed: changed}), nil
	case app.TimeRelease:
		return t.state(app.TimeResponse{Changed: t.release(time.Now())}), nil
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
// to wait out its deadline. A hold goes the same way and for the same reason:
// there is no step window left to keep open, and resume is the one call that
// must always get an engine moving again whatever state it was left in.
func (t *tickSource) resume() bool {
	was := t.paused.Swap(false)
	t.mu.Lock()
	t.holdUntil = time.Time{}
	t.pending.Store(0)
	batch := t.batch
	t.batch = nil
	t.mu.Unlock()
	if batch != nil {
		close(batch.done)
	}
	return was
}

// hold keeps the step window open, so that arms landing over several frames
// still share one step instead of racing the frame clock for a place in the
// batch. It implies pause for the reason a step does: holding a running
// engine is meaningless, so the request pauses rather than being refused.
//
// It carries a deadline because the alternative is an engine an absent agent
// has left unable to step. Asking for longer than the cap is refused rather
// than quietly shortened: a caller told it holds the window for a minute, and
// silently given ten seconds, learns about the difference as a split.
func (t *tickSource) hold(now time.Time, span time.Duration) (changed bool, err error) {
	if span <= 0 {
		span = defaultHoldDuration
	}
	if span > maxHoldDuration {
		return false, ErrHoldTooLong{For: span, Max: maxHoldDuration}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pause()
	if t.holding(now) {
		return false, nil
	}
	t.holdUntil, t.holdExpired = now.Add(span), false
	return true, nil
}

// release ends a hold early and reports whether one was standing. The step it
// was keeping open publishes on the next frame, exactly as it would have
// without the hold; nothing waits here for that, because whoever asked for
// the step is already waiting on the batch.
func (t *tickSource) release(now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.holding(now) {
		return false
	}
	t.holdUntil = time.Time{}
	return true
}

// holding reports whether a hold still stands, expiring one that has run out.
// It is the only place a hold ends by itself, and it is called from every
// point the tick source is observed - each frame and each control request -
// so an expiry needs no timer and no goroutine of its own.
//
// Callers hold mu.
func (t *tickSource) holding(now time.Time) bool {
	if t.holdUntil.IsZero() {
		return false
	}
	if now.Before(t.holdUntil) {
		return true
	}
	t.holdUntil, t.holdExpired = time.Time{}, true
	return false
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
//
// On its own this is opportunistic: there is a pending step to join only
// until the next rendered frame takes the batch, and three requests arriving
// over three connections do not reliably fit in one frame's gap. A hold is
// what makes it deterministic, by stopping the frame from taking the batch at
// all — see take.
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
	response.Tick = t.tick.Load()
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.holding(now) {
		response.Held, response.HoldFor = true, t.holdUntil.Sub(now)
	}
	response.HoldExpired = t.holdExpired
	return response
}
