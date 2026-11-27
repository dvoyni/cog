package sfx

import "github.com/dvoyni/cog/libs/m"

// VoiceOptions is how a voice starts, to Play. An absent field is the
// default.
type VoiceOptions struct {
	// Bus is the bus the voice's volume is folded with; absent, or outside
	// Config.MaxBuses, it is Master.
	Bus m.Maybe[Bus]
	// Paused stops the voice's playhead without ending it; absent is
	// playing. It is the voice's own, and stays what the game said whatever
	// Queue.SetPaused does: a voice is silent while either is set.
	Paused m.Maybe[bool]
}
