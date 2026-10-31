package internal

import "fmt"

// ErrInvalidConfig reports a plugin configuration value that is not a Config.
type ErrInvalidConfig struct{ Got any }

func (e ErrInvalidConfig) Error() string {
	return fmt.Sprintf("nosound: invalid config: want %T, got %T", Config{}, e.Got)
}

// ErrNotOggVorbis reports bytes whose Ogg Vorbis headers would not parse: a bad
// path's contents, a file that is not Ogg Vorbis, a corrupt stream. It is
// terminal, like every Clip failure, and the Voices recorded against that Clip
// end with sound.ReasonFailed.
type ErrNotOggVorbis struct{ Err error }

func (e ErrNotOggVorbis) Error() string {
	return fmt.Sprintf("nosound: not readable as ogg vorbis: %v", e.Err)
}

func (e ErrNotOggVorbis) Unwrap() error { return e.Err }

// ErrNoStreamLength reports an Ogg Vorbis stream whose length reads zero: a
// truncated file, or one whose last page carries no granule position.
//
// It is an error rather than a Clip of no duration on purpose. A zero duration
// makes the playhead and the duration fiction and silently removes
// ReasonFinished from every test that names that Clip, which is the one thing
// nosound exists to keep working.
type ErrNoStreamLength struct{}

func (ErrNoStreamLength) Error() string {
	return "nosound: the stream reports no length, so a voice on it could never end"
}

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
type ErrLoopRegionIgnored struct {
	// Start and End are the span the tags asked for, in sample frames. They are
	// both zero when Err says no value was read at all.
	Start, End int64
	// Frames is the Clip's true length in source frames, which is what End is
	// too large against.
	Frames int64
	// Err is the parse failure, when a tag carried something that is not a
	// count of frames, and nil when the values parsed and the span did not fit.
	Err error
}

func (e ErrLoopRegionIgnored) Error() string {
	if e.Err != nil {
		return fmt.Sprintf(
			"nosound: a Clip's loop tags could not be read, so it loops whole: %v", e.Err)
	}
	return fmt.Sprintf(
		"nosound: a Clip's loop region [%d, %d) frames is not inside its %d frames, so it loops whole",
		e.Start, e.End, e.Frames)
}

func (e ErrLoopRegionIgnored) Unwrap() error { return e.Err }

// ErrNoStreamFormat reports an Ogg Vorbis stream whose identification header
// names no sample rate or no channels, which no duration can be computed from.
type ErrNoStreamFormat struct {
	SampleRate int
	Channels   int
}

func (e ErrNoStreamFormat) Error() string {
	return fmt.Sprintf("nosound: the stream reports %d Hz and %d channels", e.SampleRate, e.Channels)
}
