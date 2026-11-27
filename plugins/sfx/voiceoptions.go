package sfx

import "github.com/dvoyni/cog/libs/m"

// VoiceOptions is how a voice starts, to Play: an absent field is the
// default. It is comparable and pointer-free, so a component can be exactly
// this value.
type VoiceOptions struct {
	// Bus is the bus the voice's volume is folded with; absent, or outside
	// Config.MaxBuses, it is Master.
	Bus m.Maybe[Bus]
	// Paused stops the voice's playhead without ending it; absent is
	// playing. It is the voice's own, and stays what the game said whatever
	// Queue.SetPaused does: a voice is silent while either is set.
	Paused m.Maybe[bool]
	// Priority is the voice's band in stealing; absent is 0. At a full table
	// a voice in a lower band always loses to one in a higher band, whatever
	// their audibility says, so a voice one band up is never stolen by any
	// number of plays below it: music at 1 survives a crowd of footsteps at
	// 0. A play's own band is read when the play is recorded, since that is
	// where it is ranked.
	Priority m.Maybe[int]
	// Position is where the voice is, in the game's units. A voice with none
	// is non-positional, heard centred, with no falloff, cone or panning,
	// which is what a UI click is. The first position a voice receives makes
	// it positional for the rest of its life.
	Position m.Maybe[m.Vec3]
	// Orientation is which way a positional voice faces, on the listener's
	// axes: forward -Z, up +Y, right +X. Its Cone needs it: a voice with no
	// orientation is equally loud in every direction. A 2D emitter turned θ
	// in-plane is m.QuatRotationZ(θ).Mul(m.QuatRotationX(-math.Pi / 2)), the
	// 2D listener's own rotation turned.
	Orientation m.Maybe[m.Quat]
	// Falloff is how a positional voice grows quieter with distance, given
	// whole; absent, W3C's defaults. A voice with no position keeps it for
	// when it gets one.
	Falloff m.Maybe[Falloff]
	// Cone is the directions a positional voice with an orientation is loud
	// in, given whole; absent, W3C's defaults, which are no cone. A voice
	// with no position keeps it for when it gets one.
	Cone m.Maybe[Cone]
}

// computeAudibility answers how loud a voice with these options is heard by a
// listener at listenerAt, on a bus at busVolume, before panning: its falloff
// times, when it has a facing, its cone, 1 on a non-positional voice, times
// busVolume; and how far it is from the listener, zero on a non-positional
// voice. It is the one scalar stealing ranks on, computed for a voice in the
// table and for a play arriving at it alike, so the two compare to the bit.
func (o VoiceOptions) computeAudibility(listenerAt m.Vec3, busVolume float32) (audibility, distance float32) {
	position, positional := o.Position.Get()
	if !positional {
		return busVolume, 0
	}
	distance = position.Distance(listenerAt)
	gain := computeDistanceGain(o.Falloff.Or(Falloff{}).resolve(), float64(distance))
	if orientation, facing := o.Orientation.Get(); facing {
		gain *= computeConeGain(o.Cone.Or(Cone{}).resolve(), position, orientation.Rotate(m.Vec3{Z: -1}), listenerAt)
	}
	return float32(gain) * busVolume, distance
}
