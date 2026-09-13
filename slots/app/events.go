package app

// InitEvent is published once immediately before the system driver enters its
// main loop.
type InitEvent struct{}

// UpdateEvent is the fixed-timestep simulation tick event.
// A driver should publish it once per fixed step, in order, catching up
// when frames run long.
type UpdateEvent struct {
	// Dt is the fixed timestep in seconds.
	Dt float64
	// Last reports whether this is the final catch-up step published for the
	// current frame, so subscribers can do once-per-frame work (e.g. recording
	// draws) on the latest simulation state instead of every step.
	Last bool
	// Tick numbers this tick within the engine's run, counting from one. It
	// is what names the moment something recorded inside a tick describes, so
	// that two things recorded in one tick can be shown to describe one tick
	// rather than merely claimed to. A driver numbers every tick it
	// publishes, stepped or not, and never resets the count; zero means the
	// driver does not number ticks at all.
	Tick int64
}

// RenderEvent is the per-frame render event. A driver should publish it once per
// drawn frame, on its render thread.
type RenderEvent struct {
	// Alpha is the interpolation factor in [0,1) between the previous and current
	// update steps, used to smooth rendering between fixed ticks.
	Alpha float64
}

// QuitEvent is published once after the system driver's main loop returns.
type QuitEvent struct{}

// WindowSizeChangeEvent reports a change to the window size in device-independent
// pixels. Window drivers publish it before resolving the logical viewport.
type WindowSizeChangeEvent struct{ Width, Height float32 }
