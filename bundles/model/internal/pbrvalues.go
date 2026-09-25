package internal

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// pbrValues are the bundled material's numbers as the load works them out:
// glTF's factors, each slot's KHR_texture_transform and TEXCOORD set, and the
// MASK cutoff. They never leave model as a struct. appendParams turns them into
// named gfx params, one per member of the shader's ScenePbrMaterial uniform
// block, and gfx packs those by reflected name like any shader's params - so a
// renderer binds a material's numbers without knowing what they are, and a
// caller's same-named param overrides one exactly as it overrides a texture.
//
// The names are glTF's, verbatim, because they are user-facing: the loader maps
// 1:1 with no translation table to drift, and the glTF specification is their
// documentation - including the exact semantics of occlusionStrength and
// normalScale, which are easy to get subtly wrong from memory. The shader
// declares the per-slot metadata as flat named members - baseColorTransform,
// baseColorRotation and their four siblings - rather than an array, because an
// array member is not name-addressable and animating baseColorTransform per
// frame is UV scrolling.
type pbrValues struct {
	baseColorFactor m.Vec4
	// emissiveFactor is linear radiance added after shading. Its w is spare;
	// KHR_materials_emissive_strength folds into the rgb at load.
	emissiveFactor m.Vec4
	// transforms is each slot's KHR_texture_transform as offset.xy, scale.xy,
	// and rotations its rotation in radians.
	transforms [pbrSlotCount]m.Vec4
	rotations  [pbrSlotCount]float32

	metallicFactor    float32
	roughnessFactor   float32
	normalScale       float32
	occlusionStrength float32
	// alphaCutoff is zero for an OPAQUE material, which makes the shader's
	// unconditional discard a no-op there: alpha is never below zero. A MASK
	// material sets its own, glTF's default being 0.5.
	alphaCutoff float32
	// uvSets is the packed per-slot UV selector, one bit per slot: 0 is
	// TEXCOORD_0 and 1 is TEXCOORD_1. Two sets is glTF core's minimum and the
	// cap scene keeps. It travels as a raw u32, the one member no numeric
	// parameter kind spells.
	uvSets uint32
}

// pbrSlotCount is the number of texture slots the bundled PBR has.
const pbrSlotCount = len(PbrSlots)

// pbrValueCount is how many params appendParams writes: one per member of
// ScenePbrMaterial.
const pbrValueCount = 2 + 2*pbrSlotCount + 6

// defaultPbrValues are glTF's own default material: white, fully metallic,
// fully rough, with every texture slot multiplying through unchanged.
func defaultPbrValues() pbrValues {
	values := pbrValues{
		baseColorFactor:   m.Vec4{X: 1, Y: 1, Z: 1, W: 1},
		metallicFactor:    1,
		roughnessFactor:   1,
		normalScale:       1,
		occlusionStrength: 1,
	}
	for slot := range values.transforms {
		values.transforms[slot] = m.Vec4{Z: 1, W: 1}
	}
	return values
}

// paintPbrValues are the numbers of a surface with no material of its own:
// paint, not metal. They take glTF's defaults except for metallic, because
// glTF defaults to a fully metallic surface and a metal has no diffuse at all -
// it would render as a dark mirror of an environment that does not exist, and
// the bundled shader has no image-based lighting to reflect.
//
// A self-lit surface is black paint that glows: the colour goes into
// emissiveFactor, which the shader adds after shading, and the base colour is
// black so the lights contribute nothing to it. Alpha stays on the base colour
// either way.
func paintPbrValues(color m.Color, selfLit bool) pbrValues {
	values := defaultPbrValues()
	values.metallicFactor = 0
	values.baseColorFactor, values.emissiveFactor = paintFactors(color, selfLit)
	return values
}

// paintFactors are the base colour and emissive factors of paint in one
// colour, lit or self-lit.
func paintFactors(color m.Color, selfLit bool) (base, emissive m.Vec4) {
	if selfLit {
		return m.Vec4{W: color.A}, m.Vec4{X: color.R, Y: color.G, Z: color.B}
	}
	return m.Vec4{X: color.R, Y: color.G, Z: color.B, W: color.A}, m.Vec4{}
}

// PaintParams appends the params that turn the bundled PBR's white paint -
// what BundledIngredients binds - into paint of one colour, lit or self-lit:
// its base colour and its emissive factor. A renderer lays them over the
// bundled material by name on the draw, and never names either itself.
func PaintParams(dst []gfx.ParameterDescr, color m.Color, selfLit bool) []gfx.ParameterDescr {
	base, emissive := paintFactors(color, selfLit)
	return append(dst,
		gfx.VecParam("baseColorFactor", base),
		gfx.VecParam("emissiveFactor", emissive),
	)
}

// appendParams appends one param per member of ScenePbrMaterial, by the
// member's name, in declaration order. Every member is written, defaults
// included, because gfx packs a member nothing supplies as zero - and a zero
// normalScale or texture scale is a broken surface, not a default one.
func (v *pbrValues) appendParams(dst []gfx.ParameterDescr) []gfx.ParameterDescr {
	dst = append(dst,
		gfx.VecParam("baseColorFactor", v.baseColorFactor),
		gfx.VecParam("emissiveFactor", v.emissiveFactor),
	)
	for slot := range PbrSlots {
		dst = append(dst, gfx.VecParam(PbrSlots[slot].Transform, v.transforms[slot]))
	}
	for slot := range PbrSlots {
		dst = append(dst, gfx.FloatParam(PbrSlots[slot].Rotation, v.rotations[slot]))
	}
	return append(dst,
		gfx.FloatParam("metallicFactor", v.metallicFactor),
		gfx.FloatParam("roughnessFactor", v.roughnessFactor),
		gfx.FloatParam("normalScale", v.normalScale),
		gfx.FloatParam("occlusionStrength", v.occlusionStrength),
		gfx.FloatParam("alphaCutoff", v.alphaCutoff),
		gfx.RawParameter("uvSets", v.uvSets),
	)
}

// selectUVSet points one slot at a TEXCOORD set. A slot naming a set past the
// cap falls back to set 0 and is reported: ignoring texCoord: 1 would be a
// silent wrong-output failure on a core glTF feature, so the fallback says so
// out loud.
func (v *pbrValues) selectUVSet(report func(error), slot, texCoord int) {
	if texCoord < 0 || texCoord >= pbrUVSetCount {
		report(ErrTextureUVSetUnsupported{Slot: PbrSlots[slot].Texture, TexCoord: texCoord})
		texCoord = 0
	}
	v.uvSets &^= 1 << uint(slot)
	v.uvSets |= uint32(texCoord) << uint(slot)
}

// pbrUVSetCount is the UV-set cap: TEXCOORD_0 and TEXCOORD_1, glTF core's
// minimum. The selector is one bit per slot because of it.
const pbrUVSetCount = 2
