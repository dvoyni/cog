package gfx

import "github.com/dvoyni/cog/kernel"

// PluginName is gfx's plugin name.
const PluginName kernel.PluginName = "gfx"

const (
	// AcquireStopwatchName names the stopwatch timing each backend acquire,
	// for DrawEvent.AcquireWait: a test lengthens an acquire with
	// metronome.ManualClock.LengthenNext.
	AcquireStopwatchName = "gfx: acquire"
	// SubmissionStopwatchName names the stopwatch, started in Init, whose
	// readings stamp the frames' submits and the polls that see them
	// finished, for DrawEvent.GPULatency.
	SubmissionStopwatchName = "gfx: submissions"
)
