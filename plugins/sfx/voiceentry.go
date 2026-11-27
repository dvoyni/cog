package sfx

import "time"

// VoiceEntry is one live voice as LiveVoices shows it.
type VoiceEntry struct {
	// Voice is the handle the Play handed back.
	Voice Voice
	// Path is the path of the clip the voice plays.
	Path string
	// Bus is the bus the voice plays on, Master when it named none or one
	// outside Config.MaxBuses.
	Bus Bus
	// Playhead is where the voice is in its clip. It is flush-accurate, not
	// sample-accurate: sfx computes it from the clip's duration, the same way
	// it computes endings, and the device is somewhere inside its current
	// block whenever anyone reads it.
	Playhead time.Duration
	// Duration is the clip's length, zero while the voice waits on its clip.
	Duration time.Duration
	// Audibility is the voice's final gain before panning, its bus's volume
	// folded in: the scalar a test asserts a voice is heard at.
	Audibility float32
	// Paused is whether the voice's playhead is stopped, by its own
	// VoiceOptions.Paused or by Queue.SetPaused's pause of every voice, so a
	// reader asking whether the playhead moves is answered about the playhead
	// and not about who stopped it.
	Paused bool
}
