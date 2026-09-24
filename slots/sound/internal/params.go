package internal

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
	// Priority is a hard band above audibility in stealing, 0 being the
	// default and higher losing later. It is a band and not a weight: a
	// lower-priority Voice always loses to a higher-priority one whatever the
	// gains say, so music at priority 1 is not stolen by a crowd of footsteps
	// at 0, however loud the crowd.
	//
	// It never crosses the seam. An Adapter knows nothing of priorities, and a
	// stolen Voice reaches it as an ordinary stop.
	Priority m.Maybe[int]
	// Position is where the sound is, in the game's own units. Positional is
	// one-way: the first position a Voice receives, at Play or by SetVoice,
	// makes it positional for the rest of its life. A Voice that should stop
	// being positional is a new Play, which is why there is no clear verb and
	// why no operation is ever silently ignored.
	//
	// A Voice with no position at all is non-positional and heard centred: no
	// falloff, no cone and no panning. A UI click is one.
	Position m.Maybe[m.Vec3]
	// Orientation is which way the sound points, on the same right-handed,
	// forward -Z axes the Listener uses. It is read by the Cone and by nothing
	// else, and a Positional Voice without one is equally loud in every
	// direction whatever its Cone says.
	Orientation m.Maybe[m.Quat]
	// Falloff is how this Voice gets quieter with distance, replaced whole. A
	// Falloff given to a Voice with no position is kept and takes effect when
	// it gets one.
	Falloff m.Maybe[Falloff]
	// Cone is how this Voice gets quieter off its own axis, replaced whole.
	Cone m.Maybe[Cone]
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
	if value, ok := next.Priority.Get(); ok {
		p.Priority = m.Some(value)
	}
	if value, ok := next.Position.Get(); ok {
		p.Position = m.Some(value)
	}
	if value, ok := next.Orientation.Get(); ok {
		p.Orientation = m.Some(value)
	}
	if value, ok := next.Falloff.Get(); ok {
		p.Falloff = m.Some(value)
	}
	if value, ok := next.Cone.Get(); ok {
		p.Cone = m.Some(value)
	}
	return p
}

// spatialGain is the falloff and the cone these Params would be heard through
// from a Listener at listenerAt, distance apart. It is 1 on a Voice with no
// position, which is a non-positional Voice stated as a number rather than as a
// special case.
//
// It takes the distance rather than taking it again because the one caller that
// has it has already paid for the square root, and because the two callers must
// agree to the bit: a slot's cached spatial gain and the same arithmetic run
// over a play that has no slot yet are one formula or stealing ranks on two.
func (p Params) spatialGain(listenerAt m.Vec3, distance float32) float32 {
	position, ok := p.Position.Get()
	if !ok {
		return 1
	}
	gain := distanceGain(p.Falloff.Or(Falloff{}).resolve(), float64(distance))

	// A cone needs a facing. W3C's (1,0,0) orientation default is not
	// inherited, so a Positional Voice that was never turned is equally loud in
	// every direction whatever its Cone says - which is the only way a Voice
	// can never be accidentally directional along an axis nobody chose.
	if orientation, ok := p.Orientation.Get(); ok {
		forward := orientation.Rotate(m.Vec3{Z: -1})
		gain *= coneGain(p.Cone.Or(Cone{}).resolve(), position, forward, listenerAt)
	}
	return float32(gain)
}

// audibility is what a Voice started with these Params would be heard at right
// now: volume x bus x falloff x cone, the same product voiceSlot.audibility
// reports, for a play that has no slot to have cached its factors in.
//
// It is not a second scalar. It is the one scalar computed a moment earlier,
// which is what lets stealing rank an incoming play against the table it is
// arriving at without the play having to exist first.
func (p Params) audibility(busGain float32, listenerAt m.Vec3) float32 {
	spatial := float32(1)
	if position, ok := p.Position.Get(); ok {
		spatial = p.spatialGain(listenerAt, position.Sub(listenerAt).Length())
	}
	return p.Volume.Or(1) * busGain * spatial
}
