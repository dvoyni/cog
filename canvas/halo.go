package canvas

import "github.com/dvoyni/cog/gfx"

// The shape of the band, defined once and used both by DefaultHaloProfile and
// by the material's own parameters, so the value a caller edits and the value a
// hand-assembled set renders at can never drift apart.
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
	defaultHaloReach    = 6
	defaultHaloPlateau  = 0.18
	defaultHaloExponent = 1
)

// The three per-batch parameter names the halo material declares as members of
// its uniform block. They are not reserved names and not exported: a caller
// names them by passing a HaloProfile, never by spelling them.
const (
	haloReachSlot    = "haloReach"
	haloPlateauSlot  = "haloPlateau"
	haloExponentSlot = "haloExponent"
)

// HaloProfile is the shape of the band, independent of its colour. Distances are
// in layer-local world units, so a band scales with the layer's transform for
// free - board zoom rides that transform, downstream of the quad expansion.
//
// It is a complete profile rather than a struct emitting a parameter per
// non-zero field: Plateau 0 is a legitimate value - no plateau, pure falloff -
// that a sentinel would read as 0.18. Taking the whole thing removes the trap
// instead of documenting it, and a fluent DefaultHaloProfile().WithReach(12) can
// be added later without breaking anything.
//
// Colour is not here. It rides tint - ShapeDraw.Color, TextDraw.Color and a
// sprite's tint parameter all land in the instance record's frozen Tint field -
// so it is per sprite and costs nothing, and tint.a is the band's peak alpha,
// which means fading a cluster fades its halo with it.
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

// DefaultHaloProfile returns the profile measured off painted art, filled in for
// a caller to edit - canvas.DefaultConfig()'s own idiom.
func DefaultHaloProfile() HaloProfile {
	return HaloProfile{Reach: defaultHaloReach, Plateau: defaultHaloPlateau, Exponent: defaultHaloExponent}
}

// haloSpriteMaterial is the fourth published sprite material: it paints a soft
// outward band and no mark at all, so a caller records the same marks twice -
// once on a halo layer under this material, once on the ink layer above it under
// the built-in. Because the material never composites a mark, no band can reach
// anybody's ink, and overlap is ordinary painter's order.
//
// It covers the whole sprite family in one material - Sprite, Text glyphs,
// inline icons, FillRect, Line, StrokeRect - because all of them are sprite
// instances in one instanced atlas draw.
//
// Its own parameters are the profile's defaults, because parameterRefFor
// searches a draw's parameters first and the material's second: the material's
// are defaults that a scope overrides by name, which is what makes a
// hand-assembled MaterialSet{Sprite: ...} with no parameters render at reach 6
// rather than render nothing.
//
// They are baked here and MUST NOT be mutated. MaterialDescr.Fingerprint hashes
// them, and the fingerprint is the batch key - so a mutated singleton would
// silently re-key every batch that named it.
var haloSpriteMaterial = gfx.MaterialWithState(
	gfx.ShaderWithResource(haloShaderPath),
	gfx.StateOverlay2D,
	gfx.FloatParam(haloReachSlot, defaultHaloReach),
	gfx.FloatParam(haloPlateauSlot, defaultHaloPlateau),
	gfx.FloatParam(haloExponentSlot, defaultHaloExponent),
)

// HaloMaterialSet returns the material set that haloes a layer: name it over a
// layer with SetLayerMaterial, record the marks there, and record the same marks
// again on the layer above under no material at all.
//
//	write.SetLayerMaterial(layerHalo, canvas.HaloMaterialSet(canvas.DefaultHaloProfile()))
//	drawCluster(write, layerHalo, haloColor)
//	drawCluster(write, layerInk, inkColor)
//
// Two layers is the idiom rather than a requirement, but is what to write: a
// draw added to the ink layer later lands in the ink run rather than becoming a
// coloured ghost.
//
// The profile arrives as the set's per-batch parameters, which is the only
// frequency it has. It cannot be named at a draw - every valued parameter
// constructor sets HasValue, and a draw's valued parameter becomes a per-sprite
// storage array while a scope's is shared - so **two reaches are two scopes**.
// That is the idiom, not a limitation.
//
// **Dedicate the layer to the marks being haloed.** Triangles and Texture stay
// nil, so a DrawTriangles or DrawTexture recorded on the halo layer paints as
// itself rather than as a band; and every sprite-family draw on it, including
// ones another subsystem adds later, becomes a band in its own colour.
//
// There is deliberately no HaloMaterial() beside DefaultMaterial(), despite the
// symmetry: a draw naming its own material takes none of its scope's parameters,
// so naming the halo at a draw would render at the material's own defaults and
// silently ignore every profile on the layer above it. A scope is the only way
// in.
func HaloMaterialSet(profile HaloProfile) MaterialSet {
	return MaterialSet{
		Sprite: &haloSpriteMaterial,
		Params: []gfx.ParameterDescr{
			gfx.FloatParam(haloReachSlot, profile.Reach),
			gfx.FloatParam(haloPlateauSlot, profile.Plateau),
			gfx.FloatParam(haloExponentSlot, profile.Exponent),
		},
	}
}
