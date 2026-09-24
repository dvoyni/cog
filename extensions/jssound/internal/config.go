package internal

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
// And there is no knob for either decoder. Whether the browser can decode Ogg
// Vorbis, and whether its WebCodecs AudioDecoder takes Vorbis and so can decode
// a streamed Clip off this thread, are both detected once at init - one by
// decoding an embedded micro-clip, the other by asking isConfigSupported with
// that clip's own setup headers. Neither is sniffed from a user agent and
// neither is configured, because the condition is an operating system's codec
// support and no string a page can read reports it.
type Config struct {
	// LatencyHint is what the AudioContext is asked to trade latency against
	// power, and reaches it as the latencyHint option in seconds.
	//   0 means interactive, the browser's own lowest-latency mode
	//
	// It is a hint in the browser's sense as well as ours: what is actually in
	// force is sound.Device.Latency, which is read back off the context rather
	// than repeated from here.
	LatencyHint time.Duration
	// DecodedClipLimit is the largest decoded size, in bytes, a Clip may have
	// and still be decoded whole into one AudioBuffer at load; a larger Clip
	// streams, decoded a chunk at a time and scheduled on the context clock.
	//   0  means 512 KiB, the default
	//  -1  means no limit: always decode and cache
	//  -2  means always stream: never decode at load
	//   >0 is the limit in bytes
	//
	// It is the same field, with the same sentinels and the same default, that
	// otosound.Config carries, and it is deliberately the same rather than a
	// web-flavoured variant: a game that tuned the limit for one platform is
	// saying something about its Clips rather than about its Adapter, and two
	// spellings of one decision would be two decisions the day one moved.
	//
	// It is on decoded size rather than encoded size because decoded size is
	// what costs memory, and it is computable before decoding anything, as
	// frames x channels x 4 from the identification header and the end granule
	// position. At 512 KiB that is 2.97 s mono 44.1 kHz, 1.49 s stereo
	// 44.1 kHz, 1.37 s stereo 48 kHz.
	//
	// The frames counted are the source's, not the context's. A browser
	// resamples into its own rate, so the AudioBuffer a resident Clip really
	// costs is that figure scaled by the context rate over the file's - but the
	// context rate is the output device's and moves when the player changes
	// headphones, and a limit whose meaning moved with it would put one Clip on
	// either side of the line on one machine. The file's own frames are the
	// stable measure, and they are what otosound counts too.
	//
	// Neither tier alone is defensible, which is why this is a limit and not a
	// mode. A five-minute stereo track resident is 101 MiB of float32 against
	// 4.8 MB encoded, 21x, and on iOS that is the whole page's budget; and a
	// streamed Voice costs a decoder, a resampler and a chunk queue, so a
	// footstep must not pay for one to play a Clip whose whole decoded form is
	// smaller than the machinery streaming it.
	//
	// A Clip whose length reads 0 - a truncated file - streams whatever this
	// says, because there is no decoded size to compare it against.
	//
	// Which tier a Clip landed in is invisible: both report the same duration,
	// channels, rate and loop region, and a game cannot ask.
	DecodedClipLimit int
}

// WithLatencyHint sets the latency the AudioContext is asked to trade for power.
func (c Config) WithLatencyHint(hint time.Duration) Config {
	c.LatencyHint = hint
	return c
}

// WithDecodedClipLimit sets the largest decoded size a Clip may have and still
// be held resident. Zero is 512 KiB, -1 never streams and -2 always streams.
func (c Config) WithDecodedClipLimit(bytes int) Config {
	c.DecodedClipLimit = bytes
	return c
}
