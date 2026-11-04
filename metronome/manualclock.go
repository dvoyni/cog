package metronome

import (
	"context"
	"slices"
	"sync"
	"time"
)

// ManualClock is a Clock driven by hand, for tests and deterministic runs. It
// starts at 0 and moves only when told: Advance walks time deadline by
// deadline, and Jump moves it at once.
type ManualClock struct {
	mutex  sync.Mutex
	now    time.Duration
	timers []*manualTimer
	// plugins are the started plugins reading this clock, whose tracks
	// Advance waits on to settle.
	plugins []*Plugin
}

// manualTimer is a ManualClock's Timer.
type manualTimer struct {
	clock   *ManualClock
	channel chan struct{}
	// deadline is when the timer fires, while armed.
	deadline time.Duration
	armed    bool
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
// timer: at each deadline it fires the timers due, and waits until every
// track of the plugins reading the clock has settled, its beats published and
// finished, before moving on. Work takes no manual time, so no track falls
// behind and no step is dropped.
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
		c.settle()
		c.mutex.Lock()
		next, ok := c.nextDeadline(target)
		if !ok {
			c.now = target
			c.mutex.Unlock()
			return
		}
		c.now = next
		c.fireDue()
		c.mutex.Unlock()
	}
}

// Jump moves the clock forward by d at once, as a stall would, and fires every
// timer due by then. It does not wait for the beats it lets out: a fixed-step
// track behind owes the steps passed, up to its debt cap, and catches up
// back-to-back. Plugin.Wait waits for that.
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
}

// Reset arms the timer to fire at at, or fires it at once if the clock has
// reached at.
func (t *manualTimer) Reset(at time.Duration) {
	t.clock.mutex.Lock()
	defer t.clock.mutex.Unlock()
	t.deadline = at
	t.armed = true
	if at <= t.clock.now {
		t.fire()
	}
}

// C is the channel a fire is delivered on.
func (t *manualTimer) C() <-chan struct{} { return t.channel }

// Stop disarms the timer and drops a fire not yet received.
func (t *manualTimer) Stop() {
	t.clock.mutex.Lock()
	defer t.clock.mutex.Unlock()
	t.armed = false
	select {
	case <-t.channel:
	default:
	}
}

// attach links a started plugin to the clock, for Advance to wait on its
// tracks.
func (c *ManualClock) attach(plugin *Plugin) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.plugins = append(c.plugins, plugin)
}

// detach unlinks a stopping plugin from the clock.
func (c *ManualClock) detach(plugin *Plugin) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.plugins = slices.DeleteFunc(c.plugins, func(attached *Plugin) bool { return attached == plugin })
}

// settle waits until every track of the plugins reading the clock has
// settled. The caller does not hold the mutex: Plugin.Wait reads Now.
func (c *ManualClock) settle() {
	for i := 0; ; i++ {
		c.mutex.Lock()
		if i >= len(c.plugins) {
			c.mutex.Unlock()
			return
		}
		plugin := c.plugins[i]
		c.mutex.Unlock()
		_ = plugin.Wait(context.Background())
	}
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

// fireDue fires every armed timer whose deadline the clock has reached. The
// caller holds the mutex.
func (c *ManualClock) fireDue() {
	for _, timer := range c.timers {
		if timer.armed && timer.deadline <= c.now {
			timer.fire()
		}
	}
}

// fire delivers one fire. The caller holds the clock's mutex.
func (t *manualTimer) fire() {
	t.armed = false
	select {
	case t.channel <- struct{}{}:
	default:
	}
}
