package sfx

import "time"

// VoiceStart begins one voice in one slot. A seek is a VoiceStart with an
// Offset. sfx hands a backend an Offset that is never negative, is short of
// the clip's duration, and with Loop is short of the loop end, the duration of
// a clip that loops whole, so a backend states no rule of its own for one
// outside them. A backend still bounds what it computes from an Offset by the
// frames it holds, which rounding, or a stream shorter than its headers say,
// can leave fewer.
type VoiceStart struct {
	Slot   VoiceSlot
	Clip   ClipID
	Offset time.Duration // where to begin; 0 is the head
	Loop   bool
	Params VoiceParams
}
