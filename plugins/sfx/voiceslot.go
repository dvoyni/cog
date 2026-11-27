package sfx

// VoiceSlot identifies a voice to a backend. It is always in [0, MaxVoices),
// minted and reused by sfx, which is what lets a backend's voice table be a
// fixed array indexed directly by it.
//
// A Voice's index and generation are not public contract, so they do not
// cross into a backend at all. sfx stops a slot before it reuses one, so a
// backend never sees an ambiguous slot and needs no generation of its own.
type VoiceSlot int
