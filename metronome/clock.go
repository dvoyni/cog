package metronome

import (
	"context"
	"time"
)

// Clock is where metronome reads time from and how its tracks wait. metronome
// reads time through its Clock alone, so a test or an agent can replace time
// entirely. It is a resource too, Config.Clock or the WallClock metronome
// made, for a plugin listed after it to read time by in its Init:
//
//	clock, _ := configurator.Resource[metronome.Clock]()
//
// The epoch belongs to the clock: it is where Now reads zero, and every track
// measures its deadlines from it.
type Clock interface {
	// Now answers the time since the epoch.
	Now() time.Duration
	// NewTimer makes a Timer. metronome makes one per track, at Start.
	NewTimer() Timer
	// NewStopwatch makes a Stopwatch measuring by this clock's time. name
	// tells stopwatches apart, as ManualClock.LengthenNext does.
	NewStopwatch(name string) Stopwatch
	// SetWaitCallback hands the clock wait, which returns once every track
	// reading the clock has settled, none with a beat in flight or owing one,
	// or with ctx's error if ctx ends first. metronome sets it at Start and
	// sets nil at Stop. A clock that moves time when told, as a test's or an
	// agent's does, waits on it for the beats it let out to finish; a clock
	// whose time runs on by itself ignores it. A clock serves one metronome.
	SetWaitCallback(wait func(ctx context.Context) error)
}

// Stopwatch measures the time since its Start, by the clock that made it,
// for a plugin that reads time without keeping the clock. A plugin makes its
// stopwatches from the Clock resource in its Init. Elapsed is safe from
// several goroutines, and so is Start, though a Start while another
// measurement is read restarts it.
type Stopwatch interface {
	// Start starts a measurement, or starts it again.
	Start()
	// Elapsed answers the time since the last Start; zero before the first.
	Elapsed() time.Duration
}

// Timer is how a track waits for its next deadline. The clock decides when a
// deadline counts as reached: it may fire on time, late, on an outside event,
// or never.
type Timer interface {
	// Reset arms the timer to fire once Now has reached at.
	Reset(at time.Duration)
	// C is the channel a fire is delivered on, one value per fire.
	C() <-chan struct{}
	// Stop disarms the timer. A track stops its timer as it ends.
	Stop()
}
