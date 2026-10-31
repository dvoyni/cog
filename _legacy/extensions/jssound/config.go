package jssound

import "github.com/dvoyni/cog/extensions/jssound/internal"

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
type Config = internal.Config
