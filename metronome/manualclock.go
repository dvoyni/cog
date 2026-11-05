package metronome

import (
	"context"
	"sync"
	"time"
)

// ManualClock is a Clock driven by hand, for tests and deterministic runs. It
// starts at 0 and moves only when told: Advance walks time deadline by
// deadline, and Jump moves it at once. A variable track beats at most once per
// Advance or Jump, at its target time.
type ManualClock struct {
	mutex  sync.Mutex
	now    time.Duration
	timers []*manualTimer
	// wait is metronome's wait callback, nil while no metronome reads the
	// clock.
	wait func(ctx context.Context) error
}

// manualTimer is a ManualClock's Timer.
type manualTimer struct {
	clock   *ManualClock
	channel chan struct{}
	// deadline is when the timer fires, while armed.
	deadline time.Duration
	armed    bool
	// variable marks a variable track's timer, which fires only at the end of
	// an Advance or a Jump, never on Reset nor at a deadline Advance walks.
	variable bool
	// fired is set by a fire and cleared by the next Reset or Stop: the
	// variable track owes the beat fired until it rearms.
	fired bool
}

// NewManualClock makes a ManualClock reading 0.
func NewManualClock() *ManualClock { return &ManualClock{} }

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
// fixed-step track: at each deadline it fires the timers due, and waits until
// every track reading the clock has settled, its beats published and
// finished, before moving on. Work takes no manual time, so no
// track falls behind and no step is dropped. At the target it fires the
// variable tracks due, once each, and waits for them to settle too.
//
// A negative d panics.
func (c *ManualClock) Advance(d time.Duration) {
	if d < 0 {
		panic("metronome: advancing a ManualClock by a negative duration")
	}
	c.mutex.Lock()
	target := c.now + d
	c.mutex.Unlock()
	for {
		_ = c.Wait(context.Background())
		c.mutex.Lock()
		next, ok := c.nextDeadline(target)
		if !ok {
			c.now = target
			c.fireVariable()
			c.mutex.Unlock()
			_ = c.Wait(context.Background())
			return
		}
		c.now = next
		c.fireDue()
		c.mutex.Unlock()
	}
}

// Wait returns once every track reading the clock has settled, none with a
// beat in flight or owing one, or with ctx's error if ctx ends first. With no
// metronome reading the clock, it returns at once.
func (c *ManualClock) Wait(ctx context.Context) error {
	c.mutex.Lock()
	wait := c.wait
	c.mutex.Unlock()
	if wait == nil {
		return nil
	}
	return wait(ctx)
}

// SetWaitCallback keeps wait, for Advance and Wait to wait on.
func (c *ManualClock) SetWaitCallback(wait func(ctx context.Context) error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.wait = wait
}

// Jump moves the clock forward by d at once, as a stall would, and fires every
// timer due by then. It does not wait for the beats it lets out: a fixed-step
// track behind owes the steps passed, up to its debt cap, and catches up
// back-to-back, and a variable track due beats once. Wait waits for that.
//
// A negative d panics.
func (c *ManualClock) Jump(d time.Duration) {
	if d < 0 {
		panic("metronome: jumping a ManualClock by a negative duration")
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.now += d
	c.fireDue()
	c.fireVariable()
}

// Reset arms the timer to fire at at. A fixed-step track's timer fires at once
// if the clock has reached at; a variable track's waits for the end of the
// next Advance or Jump.
func (t *manualTimer) Reset(at time.Duration) {
	t.clock.mutex.Lock()
	defer t.clock.mutex.Unlock()
	t.deadline = at
	t.armed = true
	t.fired = false
	if !t.variable && at <= t.clock.now {
		t.fire()
	}
}

// rearm reads the clock and arms a variable timer interval after it, in one
// step: a move before it is in the time read, and a move after it finds the
// timer armed, so no Advance or Jump falls between a beat and its rearm.
func (t *manualTimer) rearm(interval time.Duration) time.Duration {
	t.clock.mutex.Lock()
	defer t.clock.mutex.Unlock()
	t.deadline = t.clock.now + interval
	t.armed = true
	t.fired = false
	return t.clock.now
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
}

// newVariableTimer makes the Timer of a variable track, which fires only at
// the end of an Advance or a Jump.
func (c *ManualClock) newVariableTimer() Timer {
	timer := c.NewTimer().(*manualTimer)
	c.mutex.Lock()
	defer c.mutex.Unlock()
	timer.variable = true
	return timer
}

// nextDeadline answers the earliest deadline of an armed fixed-step timer no
// later than target. The caller holds the mutex.
func (c *ManualClock) nextDeadline(target time.Duration) (time.Duration, bool) {
	next := target
	found := false
	for _, timer := range c.timers {
		if !timer.variable && timer.armed && timer.deadline <= next {
			next = timer.deadline
			found = true
		}
	}
	return next, found
}

// fireDue fires every armed fixed-step timer whose deadline the clock has
// reached. The caller holds the mutex.
func (c *ManualClock) fireDue() {
	for _, timer := range c.timers {
		if !timer.variable && timer.armed && timer.deadline <= c.now {
			timer.fire()
		}
	}
}

// fireVariable fires every armed variable timer whose deadline the clock has
// reached. The caller holds the mutex.
func (c *ManualClock) fireVariable() {
	for _, timer := range c.timers {
		if timer.variable && timer.armed && timer.deadline <= c.now {
			timer.fire()
		}
	}
}

// pending answers whether the timer has fired and not been reset or stopped
// since.
func (t *manualTimer) pending() bool {
	t.clock.mutex.Lock()
	defer t.clock.mutex.Unlock()
	return t.fired
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
