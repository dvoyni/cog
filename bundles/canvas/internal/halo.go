package internal

import (
	"github.com/dvoyni/cog/slots/gfx"
)

// The shape of the band, defined once and used both by
// canvas.DefaultHaloProfile and by the material's own parameters, so the value a
// caller edits and the value a hand-assembled set renders at can never drift
// apart.
//
// They came off painted art rather than being guessed: 84% of the halo pixels in
// feuds' militiaman.png are exactly #ae9f8d, with alpha holding near 0.82 for
// the first fifth of the band and then falling almost exactly linearly. A plain
// pow(1-t, k) from the ink outward does not match painted art; the plateau is
// what makes it.
//
// Reach is defaulted despite a world-unit distance having no universal right
// value, because reach 0 renders nothing at all, and a visibly wrong halo
// explains itself where an invisible one does not.
const (
	DefaultHaloReach    = 6
	DefaultHaloPlateau  = 0.18
	DefaultHaloExponent = 1
)

// HaloSlot is the binding the halo material declares its profile at: a uniform
// of its own, struct HaloProfile in halo.wgsl, which the Go HaloProfile mirrors
// field for field, so a profile is set whole as one raw parameter. It is not a
// reserved name and not published: a caller names it by passing a
// canvas.HaloProfile, never by spelling it.
const HaloSlot = "halo"

// haloSpriteMaterial is the fourth sprite material canvas ships: it paints a
// soft outward band and no mark at all, so a caller records the same marks twice
// - once on a halo layer under this material, once on the ink layer above it
// under the built-in. Because the material never composites a mark, no band can
// reach anybody's ink, and overlap is ordinary painter's order.
//
// It covers the whole sprite family in one material - Sprite, Text glyphs,
// inline icons, FillRect, Line, StrokeRect - because all of them are sprite
// instances in one instanced atlas draw.
//
// Its own parameter is the default profile, because the material's parameters
// are its set's own values and a scope's are the frame's version over them: the
// material's are defaults that a scope overrides by name, which is what makes a
// hand-assembled MaterialSet{Sprite: ...} with no parameters render at reach 6
// rather than render nothing.
//
// It is baked here and MUST NOT be mutated. Material.Fingerprint hashes it, and
// the fingerprint is the batch key and the set's - so a mutated singleton would
// silently re-key every batch that named it.
var haloSpriteMaterial = MaterialWithState(
	gfx.ShaderWithResource(HaloShaderPath),
	gfx.StateOverlay2D(),
	gfx.ShaderParameterRaw(HaloSlot, DefaultHaloProfile()),
)

// HaloProfile is the shape of the band, independent of its colour; see
// canvas.HaloProfile.
//
// It is also the bytes of halo.wgsl's struct HaloProfile - three f32, twelve
// bytes, no padding - so its fields are in the shader's order and may not be
// reordered or added to without changing the shader with them.
type HaloProfile struct {
	// Reach is how far the band extends beyond the mark, in layer-local world
	// units. It is what the vertex stage grows the sprite's quad by on every
	// side.
	Reach float32
	// Plateau is the fraction of the band that holds at full strength before the
	// falloff begins, between 0 and 1.
	Plateau float32
	// Exponent shapes that falloff: 1 is the linear ramp measured off the art,
	// higher is a faster fade.
	Exponent float32
}

// DefaultHaloProfile returns the profile measured off painted art; see
// canvas.DefaultHaloProfile.
func DefaultHaloProfile() HaloProfile {
	return HaloProfile{Reach: DefaultHaloReach, Plateau: DefaultHaloPlateau, Exponent: DefaultHaloExponent}
}

// HaloMaterialSet returns the material set that haloes a layer; see
// canvas.HaloMaterialSet.
func HaloMaterialSet(profile HaloProfile) MaterialSet {
	return MaterialSet{
		Sprite: &haloSpriteMaterial,
		Params: []gfx.ShaderParameterDescr{gfx.ShaderParameterRaw(HaloSlot, profile)},
	}
}
