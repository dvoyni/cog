package sfx

// VoiceUpdate restates one slot's parameters. sfx emits target values once per
// flush, and the backend ramps to them across its own blocks.
type VoiceUpdate struct {
	Slot   VoiceSlot
	Params VoiceParams
}
