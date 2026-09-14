package app

// InitEvent is published once, immediately before the MainLoop enters its main
// loop.
type InitEvent struct{}

// UpdateEvent is the fixed-timestep simulation tick event. app publishes it
// once per fixed step, in order, on the MainLoop's main thread, catching up when
// frames run long.
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
	// rather than merely claimed to. app numbers every tick it publishes,
	// stepped or not, and never resets the count; zero means the event was not
	// published by app at all, as when a test publishes one by hand.
	Tick int64
}

// RenderEvent is the per-frame render event. app publishes it once per drawn
// frame, on the MainLoop's render thread.
type RenderEvent struct {
	// Alpha is the interpolation factor in [0,1) between the previous and current
	// update steps, used to smooth rendering between fixed ticks.
	Alpha float64
}

// QuitEvent is published once, after the MainLoop's platform loop returns.
type QuitEvent struct{}

// WindowSizeChangeEvent reports a change to the window size in device-independent
// pixels. app publishes it when the MainLoop reports one, before the MainLoop
// resolves the logical viewport.
type WindowSizeChangeEvent struct{ Width, Height float32 }
