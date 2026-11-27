//go:build !js

package otosound

import (
	"errors"
	"fmt"
)

// errNotOurClip is Install handed a prepared clip otosound did not make. sfx
// installs only what otosound's own Prepare produced, so it guards against a
// future caller rather than a failure any composition can have.
var errNotOurClip = errors.New("otosound: Install was handed a prepared clip otosound did not make")

// errStreamingNotBuilt is a prepare's failure for a clip that streams: one
// whose decoded size is over Config.DecodedClipLimit, which under its -2
// sentinel is every clip. Streaming is designed and not yet built, so such a
// clip fails for good, as every clip failure does, and sfx reports it under
// the clip's path, rather than waiting on a prepare that never finishes with
// nothing said.
var errStreamingNotBuilt = errors.New("otosound: the clip's decoded size is over Config.DecodedClipLimit, so it streams, and streaming is not built yet")

// ErrNotOggVorbis reports bytes whose Ogg Vorbis headers would not parse, or
// that failed to decode: a file that is not Ogg Vorbis, or a corrupt stream.
// It is terminal, like every clip failure: the voices recorded against the
// clip end with sfx.ReasonFailed.
type ErrNotOggVorbis struct{ Err error }

func (e ErrNotOggVorbis) Error() string {
	return fmt.Sprintf("otosound: not readable as ogg vorbis: %v", e.Err)
}

func (e ErrNotOggVorbis) Unwrap() error { return e.Err }

// ErrNoStreamFormat reports an Ogg Vorbis stream whose identification header
// names no sample rate, no channels, or more channels than the mixer's 2×2
// gain matrix can address: refused rather than truncated.
type ErrNoStreamFormat struct {
	SampleRate int
	Channels   int
}

func (e ErrNoStreamFormat) Error() string {
	return fmt.Sprintf("otosound: the stream reports %d Hz and %d channels", e.SampleRate, e.Channels)
}

// ErrNoStreamLength reports an Ogg Vorbis stream with no frames to play: one
// that decoded to none, or, until streaming counts a stream's frames, one
// whose length reads zero, a truncated file or one whose last page carries
// no granule position. It is an error rather than a clip of no duration on
// purpose: a zero duration makes the playhead and the duration fiction, and
// silently removes sfx.ReasonFinished from every voice that names the clip.
type ErrNoStreamLength struct{}

func (ErrNoStreamLength) Error() string {
	return "otosound: the stream reports no frames, so a voice on it could never end"
}
