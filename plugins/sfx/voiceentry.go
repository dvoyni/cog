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
	// Audibility is the scalar the voice is heard at before panning: its
	// falloff times its cone, 1 on a non-positional voice. It is never read
	// off the gain matrix, so a voice orbiting the listener at one distance
	// is as audible at every point of the orbit. The pan is sfx's arithmetic,
	// and the view does not carry it: a game checking that its sound is on
	// the player's right checks where it put the source.
	Audibility float32
	// Distance is how far the voice is from the listener, in the game's
	// units, zero on a non-positional voice.
	Distance float32
}
