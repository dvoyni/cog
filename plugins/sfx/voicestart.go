package sfx

import "time"

// VoiceStart begins one voice in one slot. A seek is a VoiceStart with an
// Offset.
type VoiceStart struct {
	Slot   VoiceSlot
	Clip   ClipID
	Offset time.Duration // where to begin; 0 is the head
	Loop   bool
	Params VoiceParams
}
