package metronome

import "time"

// Clock is where metronome reads time from and how its tracks wait. metronome
// reads time through its Clock alone, so a test or an agent can replace time
// entirely.
//
// The epoch belongs to the clock: it is where Now reads zero, and every track
// measures its deadlines from it.
type Clock interface {
	// Now answers the time since the epoch.
	Now() time.Duration
	// NewTimer makes a Timer. metronome makes one per track, at Start.
	NewTimer() Timer
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
