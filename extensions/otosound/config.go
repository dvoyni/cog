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
	// DecodedClipLimit is the largest decoded size, in bytes, a Clip may have
	// and still be decoded once at load; a larger Clip streams, with a decoder
	// and a read-ahead ring per Voice.
	//   0  means 512 KiB, the default
	//  -1  means no limit: always decode and cache
	//  -2  means always stream: never decode at load
	//   >0 is the limit in bytes
	//
	// It is on decoded size rather than encoded size because decoded size is
	// what costs memory, and it is computable before decoding anything, as
	// frames x channels x 4 from the identification header and the end granule
	// position. At 512 KiB that is 2.97 s mono 44.1 kHz, 1.49 s stereo
	// 44.1 kHz, 1.37 s stereo 48 kHz.
	//
	// Neither tier alone is defensible, which is why this is a limit and not a
	// mode. A five-minute stereo track resident is 101 MiB of float32 against
	// 4.8 MB encoded, 21x, so music cannot be resident; and opening a decoder
	// is 460 us and 137 KB, so a footstep cannot pay for one to play a Clip
	// whose whole decoded form is smaller than the decoder streaming it.
	//
	// A Clip whose length reads 0 - a truncated file - streams whatever this
	// says, because there is no decoded size to compare it against.
	//
	// Which tier a Clip landed in is invisible: both report the same duration,
	// channels, rate and loop region, and a game cannot ask.
	DecodedClipLimit int
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

// WithDecodedClipLimit sets the largest decoded size a Clip may have and still
// be held resident. Zero is 512 KiB, -1 never streams and -2 always streams.
func (c Config) WithDecodedClipLimit(bytes int) Config {
	c.DecodedClipLimit = bytes
	return c
}
