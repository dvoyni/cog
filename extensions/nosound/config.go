package nosound

// Config configures the silent Adapter. It is supplied under Name, and its zero
// value is the default.
//
// There is no buffer size and no channel count. Nothing is buffered, and the
// output is stereo because panning is: a mono output would make constant-power
// panning meaningless, and a surround output is a different Mixer.
type Config struct {
	// SampleRate is what the Device reports, so a test can assert a rate
	// without a device. Zero means 48000.
	SampleRate int
}

// WithSampleRate sets the rate the Device reports.
func (c Config) WithSampleRate(rate int) Config {
	c.SampleRate = rate
	return c
}
