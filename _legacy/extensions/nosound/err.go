package nosound

import "github.com/dvoyni/cog/extensions/nosound/internal"

// ErrInvalidConfig reports a plugin configuration value that is not a Config.
type ErrInvalidConfig = internal.ErrInvalidConfig

// ErrNotOggVorbis reports bytes whose Ogg Vorbis headers would not parse: a bad
// path's contents, a file that is not Ogg Vorbis, a corrupt stream. It is
// terminal, like every Clip failure, and the Voices recorded against that Clip
// end with sound.ReasonFailed.
type ErrNotOggVorbis = internal.ErrNotOggVorbis

// ErrNoStreamLength reports an Ogg Vorbis stream whose length reads zero: a
// truncated file, or one whose last page carries no granule position.
//
// It is an error rather than a Clip of no duration on purpose. A zero duration
// makes the playhead and the duration fiction and silently removes
// ReasonFinished from every test that names that Clip, which is the one thing
// nosound exists to keep working.
type ErrNoStreamLength = internal.ErrNoStreamLength

// ErrLoopRegionIgnored reports a Clip whose LOOPSTART, LOOPLENGTH or LOOPEND
// comments do not describe a span inside it, so its Loop Region was dropped
// whole and a looping Voice on it repeats the whole Clip.
//
// It is not terminal: the Clip prepares, and this is the only notice that it
// plays without the loop point its author wrote. The region is dropped rather
// than clamped because a clamped loop sounds like a working loop with the wrong
// loop point, which is the single hardest audio bug to attribute.
//
// It is reported once per Clip: a prepare runs once per entry in sound's table,
// and the notice it queues is drained once. nosound says it at all so that a
// game's test suite learns a music file's tags are broken without a device in
// the room, which is the whole of what this Adapter is for.
type ErrLoopRegionIgnored = internal.ErrLoopRegionIgnored

// ErrNoStreamFormat reports an Ogg Vorbis stream whose identification header
// names no sample rate or no channels, which no duration can be computed from.
type ErrNoStreamFormat = internal.ErrNoStreamFormat
