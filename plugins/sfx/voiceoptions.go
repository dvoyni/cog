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
