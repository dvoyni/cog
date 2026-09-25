package otosound

import "github.com/dvoyni/cog/extensions/otosound/internal"

// ErrInvalidConfig reports a plugin configuration value that is not a Config.
type ErrInvalidConfig = internal.ErrInvalidConfig

// ErrInvalidSampleRate reports a negative sample rate. Zero is the default; a
// negative rate is not a rate.
type ErrInvalidSampleRate = internal.ErrInvalidSampleRate

// ErrInvalidBufferSize reports a negative buffer size. Zero is the default; a
// negative buffer is not a buffer.
type ErrInvalidBufferSize = internal.ErrInvalidBufferSize

// ErrInvalidDecodedClipLimit reports a residency limit that is neither a size
// nor one of the two sentinels. Zero is the default, -1 never streams and -2
// always streams; anything below -2 names no tier at all.
type ErrInvalidDecodedClipLimit = internal.ErrInvalidDecodedClipLimit

// ErrDeviceUnavailable reports a Device that could not be opened at all. It is
// reported once, through kernel.ReportErrorOnce, and after that otosound
// behaves exactly as nosound does: it accepts everything, plays nothing, and
// the game runs.
//
// A Device that was open and was then lost is a different thing and is reported
// never. Ready going false is the whole of that notification, because loss is
// ordinary and silent: a game reads Device rather than handling it.
type ErrDeviceUnavailable = internal.ErrDeviceUnavailable

// ErrDeviceConfigIgnored reports a second Engine in one process whose Config
// could not be honoured. The oto context is per process and the first
// composition owns it; a second Engine shares that context and takes its own
// player, so both are audible but only the first one's rate and buffer size are
// in force.
//
// It is reported rather than silently accepted because the alternative - the
// second Engine going silent - makes which composition ran first a race.
type ErrDeviceConfigIgnored = internal.ErrDeviceConfigIgnored

// ErrNotOggVorbis reports bytes whose Ogg Vorbis headers would not parse: a bad
// path's contents, a file that is not Ogg Vorbis, a corrupt stream. It is
// terminal, like every Clip failure, and the Voices recorded against that Clip
// end with sound.ReasonFailed.
type ErrNotOggVorbis = internal.ErrNotOggVorbis

// ErrNoStreamLength reports an Ogg Vorbis stream that decoded to no frames at
// all: a truncated file, or one whose audio packets carry nothing.
//
// It is an error rather than a Clip of no duration on purpose. A zero duration
// makes the playhead and the duration fiction and silently removes
// ReasonFinished from every Voice that names that Clip.
type ErrNoStreamLength = internal.ErrNoStreamLength

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

// ErrNoStreamFormat reports an Ogg Vorbis stream whose identification header
// names no sample rate, no channels, or more channels than the Mixer's 2x2 gain
// matrix can address.
type ErrNoStreamFormat = internal.ErrNoStreamFormat
