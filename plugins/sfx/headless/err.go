package headless

import "fmt"

// ErrNotOggVorbis reports bytes whose Ogg Vorbis headers would not parse: a
// file that is not Ogg Vorbis, or a corrupt stream. It is terminal, like every
// clip failure: the voices recorded against the clip end with
// sfx.ReasonFailed.
type ErrNotOggVorbis struct{ Err error }

func (e ErrNotOggVorbis) Error() string {
	return fmt.Sprintf("headless: not readable as ogg vorbis: %v", e.Err)
}

func (e ErrNotOggVorbis) Unwrap() error { return e.Err }

// ErrNoStreamFormat reports an Ogg Vorbis stream whose identification header
// names no sample rate or no channels, which no duration can be computed
// from.
type ErrNoStreamFormat struct {
	SampleRate int
	Channels   int
}

func (e ErrNoStreamFormat) Error() string {
	return fmt.Sprintf("headless: the stream reports %d Hz and %d channels", e.SampleRate, e.Channels)
}

// ErrNoStreamLength reports an Ogg Vorbis stream whose length reads zero: a
// truncated file, or one whose last page carries no granule position. It is
// an error rather than a clip of no duration on purpose: a zero duration
// makes the playhead and the duration fiction, and silently removes
// sfx.ReasonFinished from every test that names the clip.
type ErrNoStreamLength struct{}

func (ErrNoStreamLength) Error() string {
	return "headless: the stream reports no length, so a voice on it could never end"
}
