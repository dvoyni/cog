package sfx

import "github.com/dvoyni/cog/libs/m"

// VoiceOptions is how a voice starts, to Play. An absent field is the
// default. Paused is its one field yet: every voice plays its clip once, at
// the clip's own rate, at unity and centred.
type VoiceOptions struct {
	// Paused stops the voice's playhead without ending it; absent is
	// playing. It is the voice's own, and stays what the game said whatever
	// Queue.SetPaused does: a voice is silent while either is set.
	Paused m.Maybe[bool]
}
