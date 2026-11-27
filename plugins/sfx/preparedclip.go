package sfx

import "github.com/dvoyni/cog/libs/m"

// PreparedClip is a clip turned into something a voice can be started from.
// Which kind it is, shared samples or bytes and a way to read them, is the
// backend's business, and a game cannot tell.
//
// It stays opaque about samples and honest about facts: sfx computes every
// voice's ending from Duration and the voice's rate, so a backend never
// reports a voice, and a game under the headless backend ends its voices as
// under any other.
type PreparedClip interface {
	// Duration is the clip's length in seconds.
	Duration() float32
	// Channels is 1 or 2; a stereo clip is never downmixed.
	Channels() int
	// SampleRate is the clip's own, never the device's.
	SampleRate() int
	// LoopRegion is the span the clip's tags declare; absent, a looping voice
	// repeats the whole clip.
	LoopRegion() m.Maybe[LoopRegion]
	// LoopRegionErr says why a region the clip's tags declared was dropped
	// whole: an ErrLoopRegionIgnored with no Path, which sfx fills in when it
	// reports it. It is nil when the region is fine or absent.
	LoopRegionErr() error
}
