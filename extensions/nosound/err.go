package nosound

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

// ErrNoStreamFormat reports an Ogg Vorbis stream whose identification header
// names no sample rate or no channels, which no duration can be computed from.
type ErrNoStreamFormat struct {
	SampleRate int
	Channels   int
}

func (e ErrNoStreamFormat) Error() string {
	return fmt.Sprintf("nosound: the stream reports %d Hz and %d channels", e.SampleRate, e.Channels)
}
