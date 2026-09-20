package jssound

import "time"

// Config configures the browser Adapter. It is supplied under Name, and its zero
// value is the default.
//
// There is no sample rate. The browser owns it: an AudioContext picks the output
// device's rate, decodeAudioData resamples into it, and an Adapter that asked
// for one would be asking for something no browser promises to give.
//
// There is no channel count either, for the reason every Adapter here has none:
// the output is stereo because panning is. A mono output makes constant-power
// panning meaningless and a surround one is a different Mixer.
//
// And there is no knob for the Ogg decoder. Whether the browser can decode Ogg
// Vorbis is detected once at init by decoding an embedded micro-clip, never
// sniffed from a user agent and never configured, because the condition is an
// operating system's codec support and no string a page can read reports it.
//
// There is no DecodedClipLimit yet. It is the tier knob, and this Extension has
// one tier: every Clip is decoded whole into an AudioBuffer. A field that named
// a limit nothing could exceed would be a knob that does nothing, so it arrives
// with the streamed tier that gives it a meaning.
type Config struct {
	// LatencyHint is what the AudioContext is asked to trade latency against
	// power, and reaches it as the latencyHint option in seconds.
	//   0 means interactive, the browser's own lowest-latency mode
	//
	// It is a hint in the browser's sense as well as ours: what is actually in
	// force is sound.Device.Latency, which is read back off the context rather
	// than repeated from here.
	LatencyHint time.Duration
}

// WithLatencyHint sets the latency the AudioContext is asked to trade for power.
func (c Config) WithLatencyHint(hint time.Duration) Config {
	c.LatencyHint = hint
	return c
}
