package canvas

import (
	"github.com/dvoyni/cog/bundles/canvas/internal"
	"github.com/dvoyni/cog/extensions/gfx"
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

// DefaultHaloProfile returns the profile measured off painted art - 84% of the
// halo pixels in feuds' militiaman.png sit on a plateau for the first fifth of
// the band and then fall almost exactly linearly - filled in for a caller to
// edit.
func DefaultHaloProfile() HaloProfile {
	return HaloProfile{
		Reach: internal.DefaultHaloReach, Plateau: internal.DefaultHaloPlateau,
		Exponent: internal.DefaultHaloExponent,
	}
}

// HaloMaterialSet returns the material set that haloes a layer: name it over a
// layer with SetLayerMaterial, record the marks there, and record the same marks
// again on the layer above under no material at all.
//
//	write.SetLayerMaterial(layerHalo, canvas.HaloMaterialSet(canvas.DefaultHaloProfile()))
//	drawCluster(write, layerHalo, haloColor)
//	drawCluster(write, layerInk, inkColor)
//
// The halo material paints a soft outward band and no mark at all, so no band
// can reach anybody's ink and overlap is ordinary painter's order. It covers the
// whole sprite family - Sprite, Text glyphs, inline icons, FillRect, Line,
// StrokeRect - because all of them are sprite instances in one instanced atlas
// draw.
//
// Two layers is the idiom rather than a requirement, but is what to write: a
// draw added to the ink layer later lands in the ink run rather than becoming a
// coloured ghost.
//
// The profile arrives as the set's per-batch parameters, which is the only
// frequency it has. It cannot be named at a draw - every valued parameter
// constructor sets HasValue, and a draw's valued parameter becomes a per-sprite
// storage array while a scope's is shared - so **two reaches are two scopes**.
// That is the idiom, not a limitation. The material carries the defaults as its
// own parameters, so a hand-assembled MaterialSet{Sprite: ...} renders at reach
// 6 rather than rendering nothing.
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
		Sprite: &internal.HaloSpriteMaterial,
		Params: []gfx.ParameterDescr{
			gfx.FloatParam(internal.HaloReachSlot, profile.Reach),
			gfx.FloatParam(internal.HaloPlateauSlot, profile.Plateau),
			gfx.FloatParam(internal.HaloExponentSlot, profile.Exponent),
		},
	}
}
