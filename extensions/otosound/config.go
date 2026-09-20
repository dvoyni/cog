package otosound

import "time"

// Config configures the desktop Adapter. It is supplied under Name, and its
// zero value is the default.
//
// Channel count is not a knob. The output is stereo because panning is: a mono
// output makes constant-power panning meaningless, and a surround output is a
// different Mixer rather than a different config.
//
// Both fields belong to the oto context, and the context is per process. The
// first composition in a process owns it, so a second Engine's values here are
// ignored and reported once as ErrDeviceConfigIgnored; sound.Device then
// reports the values actually in force rather than the ones this Config asked
// for.
type Config struct {
	// BufferSize is how much audio the device buffers, and so how long a block
	// the Mixer produces and how long the declick ramp is.
	//   0 means 10ms, the default, which the spike ran for 12s with no underrun
	BufferSize time.Duration
	// SampleRate is the rate the Mixer produces and the device consumes. A Clip
	// is converted to it once, in Prepare, so the device thread never resamples
	// a base rate.
	//   0 means 48000
	SampleRate int
}

// WithBufferSize sets how much audio the device buffers.
func (c Config) WithBufferSize(size time.Duration) Config {
	c.BufferSize = size
	return c
}

// WithSampleRate sets the rate the Mixer produces and the device consumes.
func (c Config) WithSampleRate(rate int) Config {
	c.SampleRate = rate
	return c
}
