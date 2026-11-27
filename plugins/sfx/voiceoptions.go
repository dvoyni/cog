package sfx

import "github.com/dvoyni/cog/libs/m"

// VoiceOptions is how a voice starts, to Play. An absent field is the
// default.
type VoiceOptions struct {
	// Bus is the bus the voice's volume is folded with; absent, or outside
	// Config.MaxBuses, it is Master.
	Bus m.Maybe[Bus]
}
