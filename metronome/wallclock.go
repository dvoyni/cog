package metronome

import (
	"math"
	"sync/atomic"
	"time"
)

// WallClock is the default Clock: monotonic time, with the epoch at the Start
// of the plugin it is given to.
type WallClock struct {
	// epoch is set by start, before any track reads the clock.
	epoch time.Time
}

// wallTimer is a WallClock's Timer, backed by one time.Timer made at NewTimer
// and reused on every Reset.
type wallTimer struct {
	clock   *WallClock
	channel chan struct{}
	timer   *time.Timer
	// deadline is when the timer fires, or disarmed. A fire takes it with a
	// compare-and-swap, so a time.Timer callback left over from an earlier
	// arm never fires a later one early.
	deadline atomic.Int64
}

// disarmed is a wallTimer's deadline while it is not armed.
const disarmed = math.MaxInt64

// NewWallClock makes a WallClock. Its epoch is set at the Start of the plugin
// it is given to, so it reads zero there, however early it was made.
func NewWallClock() *WallClock { return &WallClock{} }

// Now answers the monotonic time since the epoch.
func (c *WallClock) Now() time.Duration { return time.Since(c.epoch) }

// NewTimer makes a Timer of this clock.
func (c *WallClock) NewTimer() Timer {
	timer := &wallTimer{clock: c, channel: make(chan struct{}, 1)}
	timer.deadline.Store(disarmed)
	timer.timer = time.AfterFunc(time.Hour, timer.elapse)
	timer.timer.Stop()
	return timer
}

// Reset arms the timer to fire at at, or fires it at once if the clock has
// reached at. A fire of an earlier arm not yet received is dropped.
func (t *wallTimer) Reset(at time.Duration) {
	t.drain()
	t.deadline.Store(int64(at))
	if wait := at - t.clock.Now(); wait > 0 {
		t.timer.Reset(wait)
		return
	}
	t.fire(int64(at))
}

// C is the channel a fire is delivered on.
func (t *wallTimer) C() <-chan struct{} { return t.channel }

// Stop disarms the timer and drops a fire not yet received.
func (t *wallTimer) Stop() {
	t.deadline.Store(disarmed)
	t.timer.Stop()
	t.drain()
}

// start sets the epoch to now.
func (c *WallClock) start() { c.epoch = time.Now() }

// elapse is the time.Timer's callback. It fires the timer once its deadline is
// reached, and waits again for a deadline not yet reached, as after a callback
// left over from an earlier arm.
func (t *wallTimer) elapse() {
	deadline := t.deadline.Load()
	if deadline == disarmed {
		return
	}
	if wait := time.Duration(deadline) - t.clock.Now(); wait > 0 {
		t.timer.Reset(wait)
		return
	}
	t.fire(deadline)
}

// fire delivers one fire for deadline, unless the timer has been disarmed or
// armed again since.
func (t *wallTimer) fire(deadline int64) {
	if !t.deadline.CompareAndSwap(deadline, disarmed) {
		return
	}
	select {
	case t.channel <- struct{}{}:
	default:
	}
}

// drain drops a fire not yet received.
func (t *wallTimer) drain() {
	select {
	case <-t.channel:
	default:
	}
}
