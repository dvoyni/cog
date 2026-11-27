package sfx

import "time"

// ClipInfo is what a game can ask of a clip, with ClipTable.Info: its facts,
// every one zero until the clip is ready. It carries no ID: there is no handle
// to a clip, since a game has nothing to load or release.
type ClipInfo struct {
	// Duration is the clip's length.
	Duration time.Duration
	// Channels is 1 or 2: a stereo clip is never downmixed.
	Channels int
	// SampleRate is the clip's own rate, never the device's.
	SampleRate int
}
