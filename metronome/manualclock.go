package metronome

import (
	"sync"
	"time"
)

// ManualClock is a Clock driven by hand, for tests and deterministic runs. It
// starts at 0 and moves only when told.
type ManualClock struct {
	mutex sync.Mutex
	// timerChanged is broadcast when a timer is reset or stopped, which is how
	// Advance learns that a fired track has settled and waits again.
	timerChanged sync.Cond
	now          time.Duration
	timers       []*manualTimer
}

// manualTimer is a ManualClock's Timer.
type manualTimer struct {
	clock   *ManualClock
	channel chan struct{}
	// deadline is when the timer fires, while armed.
	deadline time.Duration
	armed    bool
	// fired is set from a fire until the timer is reset or stopped: the track
	// it belongs to is still at work on the beat the fire let out.
	fired bool
}

// NewManualClock makes a ManualClock reading 0.
func NewManualClock() *ManualClock {
	clock := &ManualClock{}
	clock.timerChanged.L = &clock.mutex
	return clock
}

// Now answers the time the clock has been moved to.
func (c *ManualClock) Now() time.Duration {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.now
}

// NewTimer makes a Timer of this clock.
func (c *ManualClock) NewTimer() Timer {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	timer := &manualTimer{clock: c, channel: make(chan struct{}, 1)}
	c.timers = append(c.timers, timer)
	return timer
}

// Advance moves the clock forward by d, deadline by deadline across every
// timer: at each deadline it fires the timers due, and waits until each of
// their tracks has published its beat, seen it settle, and reset or stopped
// its timer, before moving on. Work takes no manual time, so no track falls
// behind.
//
// A negative d panics.
func (c *ManualClock) Advance(d time.Duration) {
	if d < 0 {
		panic("metronome: advancing a ManualClock by a negative duration")
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	target := c.now + d
	for {
		for c.anyFired() {
			c.timerChanged.Wait()
		}
		next, ok := c.nextDeadline(target)
		if !ok {
			break
		}
		c.now = next
		for _, timer := range c.timers {
			if timer.armed && timer.deadline <= c.now {
				timer.fire()
			}
		}
	}
	c.now = target
}

// Reset arms the timer to fire at at, or fires it at once if the clock has
// reached at.
func (t *manualTimer) Reset(at time.Duration) {
	t.clock.mutex.Lock()
	defer t.clock.mutex.Unlock()
	t.fired = false
	t.deadline = at
	t.armed = true
	if at <= t.clock.now {
		t.fire()
	}
	t.clock.timerChanged.Broadcast()
}

// C is the channel a fire is delivered on.
func (t *manualTimer) C() <-chan struct{} { return t.channel }

// Stop disarms the timer and drops a fire not yet received.
func (t *manualTimer) Stop() {
	t.clock.mutex.Lock()
	defer t.clock.mutex.Unlock()
	t.armed = false
	t.fired = false
	select {
	case <-t.channel:
	default:
	}
	t.clock.timerChanged.Broadcast()
}

// anyFired answers whether a timer has fired and not been reset or stopped
// since. The caller holds the mutex.
func (c *ManualClock) anyFired() bool {
	for _, timer := range c.timers {
		if timer.fired {
			return true
		}
	}
	return false
}

// nextDeadline answers the earliest deadline of an armed timer no later than
// target. The caller holds the mutex.
func (c *ManualClock) nextDeadline(target time.Duration) (time.Duration, bool) {
	next := target
	found := false
	for _, timer := range c.timers {
		if timer.armed && timer.deadline <= next {
			next = timer.deadline
			found = true
		}
	}
	return next, found
}

// fire delivers one fire. The caller holds the clock's mutex.
func (t *manualTimer) fire() {
	t.armed = false
	t.fired = true
	select {
	case t.channel <- struct{}{}:
	default:
	}
}
