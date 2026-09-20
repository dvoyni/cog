package types

import "github.com/dvoyni/cog/libs/m"

// Bus is a group of Voices the game declares, sharing one volume. Buses are
// flat: every one of them is directly under Master, because a tree is where a
// DSP graph starts.
type Bus int

// Master is the only Bus sound declares; a game declares the rest. It is zero,
// so an unset Bus lands on Master and a game that declares no Buses still has
// working audio.
const Master Bus = 0

// Params is "start like this" to Play and "become this" to SetVoice. An absent
// field is the default to Play and unchanged to SetVoice, which is m.Maybe's
// whole job here: one value with a set-mask, rather than a narrow setter per
// field, because the ECS face must reconcile a whole Component in one operation.
//
// It stays a legal Component value: comparable when its fields are, pointer-free
// and copyable into a Store.
//
// Clip and Offset are deliberately not fields. Changing a Voice's Clip is a new
// Play, a changed offset is a Seek, and a ClipRef is compared with Equal rather
// than ==, which it would otherwise drag into every Params.
type Params struct {
	// Bus is the group this Voice's volume is folded into. sound records and
	// reports it; a Bus volume to fold is issue 478.
	Bus m.Maybe[Bus]
	// Volume is linear, 1 being unity.
	Volume m.Maybe[float32]
	// Pitch is the playback rate, 1 being the Clip's own. It reaches the
	// Adapter as VoiceParams.Rate and it scales the playhead here, so a Voice
	// at twice its rate ends after half its duration rather than after all of
	// it with the Adapter having run out long before.
	//
	// A negative rate would run a Voice backwards off the front of its buffer
	// and is clamped to zero, which is a legitimate freeze.
	Pitch m.Maybe[float32]
	// Loop makes the Voice repeat the way its Clip says to: a Clip with no Loop
	// Region says the whole of itself, which is what every Clip says today. A
	// looping Voice never ends by itself.
	Loop m.Maybe[bool]
	// Paused stops the Voice advancing without ending it. It suspends rather
	// than silences: the playhead stops and resumes on the same sample.
	Paused m.Maybe[bool]
}

// merge returns p with every field next states replacing p's own, which is what
// "an absent field is unchanged" means when a SetVoice reaches a Voice that
// already carries Params.
func (p Params) merge(next Params) Params {
	if value, ok := next.Bus.Get(); ok {
		p.Bus = m.Some(value)
	}
	if value, ok := next.Volume.Get(); ok {
		p.Volume = m.Some(value)
	}
	if value, ok := next.Pitch.Get(); ok {
		p.Pitch = m.Some(value)
	}
	if value, ok := next.Loop.Get(); ok {
		p.Loop = m.Some(value)
	}
	if value, ok := next.Paused.Get(); ok {
		p.Paused = m.Some(value)
	}
	return p
}
