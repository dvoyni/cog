package sfx

import "time"

// VoiceEntry is one live voice as LiveVoices shows it.
type VoiceEntry struct {
	// Voice is the handle the Play handed back.
	Voice Voice
	// Path is the path of the clip the voice plays.
	Path string
	// Playhead is where the voice is in its clip. It is flush-accurate, not
	// sample-accurate: sfx computes it from the clip's duration, the same way
	// it computes endings, and the device is somewhere inside its current
	// block whenever anyone reads it.
	Playhead time.Duration
	// Duration is the clip's length, zero while the voice waits on its clip.
	Duration time.Duration
}
