package otosound

import (
	"fmt"
	"time"
)

// ErrInvalidConfig reports a plugin configuration value that is not a Config.
type ErrInvalidConfig struct{ Got any }

func (e ErrInvalidConfig) Error() string {
	return fmt.Sprintf("otosound: invalid config: want %T, got %T", Config{}, e.Got)
}

// ErrInvalidSampleRate reports a negative sample rate. Zero is the default; a
// negative rate is not a rate.
type ErrInvalidSampleRate struct{ SampleRate int }

func (e ErrInvalidSampleRate) Error() string {
	return fmt.Sprintf("otosound: invalid SampleRate %d", e.SampleRate)
}

// ErrInvalidBufferSize reports a negative buffer size. Zero is the default; a
// negative buffer is not a buffer.
type ErrInvalidBufferSize struct{ BufferSize time.Duration }

func (e ErrInvalidBufferSize) Error() string {
	return fmt.Sprintf("otosound: invalid BufferSize %v", e.BufferSize)
}

// ErrInvalidDecodedClipLimit reports a residency limit that is neither a size
// nor one of the two sentinels. Zero is the default, -1 never streams and -2
// always streams; anything below -2 names no tier at all.
type ErrInvalidDecodedClipLimit struct{ DecodedClipLimit int }

func (e ErrInvalidDecodedClipLimit) Error() string {
	return fmt.Sprintf("otosound: invalid DecodedClipLimit %d, want a size in bytes, 0, -1 or -2",
		e.DecodedClipLimit)
}

// ErrDeviceUnavailable reports a Device that could not be opened at all. It is
// reported once, through kernel.ReportErrorOnce, and after that otosound
// behaves exactly as nosound does: it accepts everything, plays nothing, and
// the game runs.
//
// A Device that was open and was then lost is a different thing and is reported
// never. Ready going false is the whole of that notification, because loss is
// ordinary and silent: a game reads Device rather than handling it.
type ErrDeviceUnavailable struct{ Err error }

func (e ErrDeviceUnavailable) Error() string {
	return fmt.Sprintf("otosound: no audio device could be opened: %v", e.Err)
}

func (e ErrDeviceUnavailable) Unwrap() error { return e.Err }

// ErrDeviceConfigIgnored reports a second Engine in one process whose Config
// could not be honoured. The oto context is per process and the first
// composition owns it; a second Engine shares that context and takes its own
// player, so both are audible but only the first one's rate and buffer size are
// in force.
//
// It is reported rather than silently accepted because the alternative - the
// second Engine going silent - makes which composition ran first a race.
type ErrDeviceConfigIgnored struct {
	AskedSampleRate, SampleRate int
	AskedBufferSize, BufferSize time.Duration
}

func (e ErrDeviceConfigIgnored) Error() string {
	return fmt.Sprintf(
		"otosound: the audio context is already open at %d Hz and %v, so this Engine's %d Hz and %v are ignored",
		e.SampleRate, e.BufferSize, e.AskedSampleRate, e.AskedBufferSize)
}

// ErrNotOggVorbis reports bytes whose Ogg Vorbis headers would not parse: a bad
// path's contents, a file that is not Ogg Vorbis, a corrupt stream. It is
// terminal, like every Clip failure, and the Voices recorded against that Clip
// end with sound.ReasonFailed.
type ErrNotOggVorbis struct{ Err error }

func (e ErrNotOggVorbis) Error() string {
	return fmt.Sprintf("otosound: not readable as ogg vorbis: %v", e.Err)
}

func (e ErrNotOggVorbis) Unwrap() error { return e.Err }

// ErrNoStreamLength reports an Ogg Vorbis stream that decoded to no frames at
// all: a truncated file, or one whose audio packets carry nothing.
//
// It is an error rather than a Clip of no duration on purpose. A zero duration
// makes the playhead and the duration fiction and silently removes
// ReasonFinished from every Voice that names that Clip.
type ErrNoStreamLength struct{}

func (ErrNoStreamLength) Error() string {
	return "otosound: the stream decoded to no frames, so a voice on it could never end"
}

// ErrNoStreamFormat reports an Ogg Vorbis stream whose identification header
// names no sample rate, no channels, or more channels than the Mixer's 2x2 gain
// matrix can address.
type ErrNoStreamFormat struct {
	SampleRate int
	Channels   int
}

func (e ErrNoStreamFormat) Error() string {
	return fmt.Sprintf("otosound: the stream reports %d Hz and %d channels", e.SampleRate, e.Channels)
}
