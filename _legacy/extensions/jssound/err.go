package jssound

import "github.com/dvoyni/cog/extensions/jssound/internal"

// ErrInvalidConfig reports a plugin configuration value that is not a Config.
type ErrInvalidConfig = internal.ErrInvalidConfig

// ErrInvalidLatencyHint reports a negative latency hint. Zero is interactive,
// the default; a negative latency is not a latency.
type ErrInvalidLatencyHint = internal.ErrInvalidLatencyHint

// ErrInvalidDecodedClipLimit reports a residency limit that is neither a size
// nor one of the two sentinels. Zero is the default, -1 never streams and -2
// always streams; anything below -2 names no tier at all.
type ErrInvalidDecodedClipLimit = internal.ErrInvalidDecodedClipLimit

// ErrDeviceUnavailable reports a page with no Web Audio at all: no AudioContext
// constructor, or one that refused to construct. It is reported once, through
// kernel.ReportErrorOnce, and after that jssound behaves exactly as nosound
// does - it accepts everything, plays nothing, and the game runs.
//
// A context that exists and has not been resumed is not this. A browser suspends
// an AudioContext until the player has made a gesture, which is the ordinary
// first state of every web game and is reported to nobody: Ready is false, the
// game draws its click-to-enable prompt, and the gesture resumes it.
type ErrDeviceUnavailable = internal.ErrDeviceUnavailable

// ErrNotOggVorbis reports bytes whose Ogg Vorbis headers would not parse: a bad
// path's contents, a file that is not Ogg Vorbis, a corrupt stream. It is
// terminal, like every Clip failure, and the Voices recorded against that Clip
// end with sound.ReasonFailed.
//
// The headers are parsed in Go whichever decoder produces the samples, because
// the duration, the channels, the source rate and the Loop Region are facts
// about the file that decodeAudioData does not surface.
type ErrNotOggVorbis = internal.ErrNotOggVorbis

// ErrDecodeRefused reports a Clip whose Ogg Vorbis headers parsed and which the
// browser then declined to decode.
//
// It is its own error rather than an ErrNotOggVorbis because it says something
// different and a reader will want to tell them apart: the file is Ogg Vorbis,
// and this browser would not turn it into samples. It does not fall back to the
// wasm decoder, because the fallback's trigger is the probe at init and a Clip
// that fails after a probe that passed is a broken Clip rather than a browser
// without a codec.
type ErrDecodeRefused = internal.ErrDecodeRefused

// ErrNoStreamLength reports an Ogg Vorbis stream that holds no frames at all,
// counted rather than believed.
//
// A last page with no granule position is not this, and used to be: such a file
// has no computable decoded size, so it now streams - which is what the spec
// says an unmeasurable Clip does and what otosound has always done - and the
// stream's frames are counted on the goroutine that was going to decode them
// anyway. A file that truly decodes to nothing still fails here.
//
// It is an error rather than a Clip of no duration on purpose. A zero duration
// makes the playhead and the duration fiction and silently removes
// ReasonFinished from every Voice that names that Clip.
type ErrNoStreamLength = internal.ErrNoStreamLength

// ErrNoStreamFormat reports an Ogg Vorbis stream whose identification header
// names no sample rate, no channels, or more channels than the 2x2 gain matrix
// can address.
type ErrNoStreamFormat = internal.ErrNoStreamFormat

// ErrLoopRegionIgnored reports a Clip whose LOOPSTART, LOOPLENGTH or LOOPEND
// comments do not describe a span inside it, so its Loop Region was dropped
// whole and a looping Voice on it repeats the whole Clip.
//
// It is not terminal: the Clip plays, and this is the only notice that it plays
// without the loop point its author wrote. The region is dropped rather than
// clamped because a clamped loop sounds like a working loop with the wrong loop
// point, which is the single hardest audio bug to attribute, and because
// clamping would make a tag's meaning depend on the Clip it sits in.
//
// It is reported once per Clip: a prepare runs once per entry in sound's table,
// and the notice it queues is drained once.
type ErrLoopRegionIgnored = internal.ErrLoopRegionIgnored
