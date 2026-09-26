package internal

import (
	"unsafe"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// PbrValues are the bundled material's numbers as the load works them out:
// glTF's factors, each slot's KHR_texture_transform and TEXCOORD set, and the
// MASK cutoff.
//
// It is also the Go mirror of the shader's ScenePbrMaterial, member for member
// and byte for byte: seven vec4s, then eleven 4-byte scalars, then the four
// bytes WGSL rounds the struct up to 160 with. A model's draw params set it as
// one whole binding, scenePbrMaterial, through gfx.RawParameterRef, whose check
// and the layout test hold the two together.
//
// For the forward material, appendParams turns them into named gfx params, one
// per member of the block, and gfx packs those by reflected name like any
// shader's params - so a renderer binds a material's numbers without knowing
// what they are, and a caller's same-named param overrides one exactly as it
// overrides a texture.
//
// A renderer drawing through draw params cannot lay a param over one member of
// a set's block, because gfx binds whole bindings only. Overlay is how it keeps
// doing so for this one block: it folds a param named for a member into a copy
// of the values, and the renderer sets the copy whole. It is the only
// member-level write left, and it lives with the struct whose members it names.
//
// The names are glTF's, verbatim, because they are user-facing: the loader maps
// 1:1 with no translation table to drift, and the glTF specification is their
// documentation - including the exact semantics of occlusionStrength and
// normalScale, which are easy to get subtly wrong from memory. The shader
// declares the per-slot metadata as flat named members - baseColorTransform,
// baseColorRotation and their four siblings - rather than an array, because an
// array member is not name-addressable and animating baseColorTransform per
// frame is UV scrolling.
type PbrValues struct {
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
	// _ is the struct's trailing padding: WGSL rounds ScenePbrMaterial up to
	// its 16-byte alignment, and the set's binding is that size.
	_ uint32
}

// pbrSlotCount is the number of texture slots the bundled PBR has.
const pbrSlotCount = len(PbrSlots)

// pbrValueCount is how many params appendParams writes: one per member of
// ScenePbrMaterial.
const pbrValueCount = 2 + 2*pbrSlotCount + 6

// defaultPbrValues are glTF's own default material: white, fully metallic,
// fully rough, with every texture slot multiplying through unchanged.
func defaultPbrValues() PbrValues {
	values := PbrValues{
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
func paintPbrValues(color m.Color, selfLit bool) PbrValues {
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
func (v *PbrValues) appendParams(dst []gfx.ParameterDescr) []gfx.ParameterDescr {
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
func (v *PbrValues) selectUVSet(report func(error), slot, texCoord int) {
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

// pbrMember is one member of ScenePbrMaterial: where it sits in PbrValues and
// how many bytes it spans.
type pbrMember struct{ offset, size uintptr }

// pbrMembers is every member of ScenePbrMaterial by its WGSL name, which is
// the name appendParams writes it under.
var pbrMembers = func() map[string]pbrMember {
	var v PbrValues
	vec4, scalar := unsafe.Sizeof(m.Vec4{}), unsafe.Sizeof(float32(0))
	members := map[string]pbrMember{
		"baseColorFactor":   {unsafe.Offsetof(v.baseColorFactor), vec4},
		"emissiveFactor":    {unsafe.Offsetof(v.emissiveFactor), vec4},
		"metallicFactor":    {unsafe.Offsetof(v.metallicFactor), scalar},
		"roughnessFactor":   {unsafe.Offsetof(v.roughnessFactor), scalar},
		"normalScale":       {unsafe.Offsetof(v.normalScale), scalar},
		"occlusionStrength": {unsafe.Offsetof(v.occlusionStrength), scalar},
		"alphaCutoff":       {unsafe.Offsetof(v.alphaCutoff), scalar},
		"uvSets":            {unsafe.Offsetof(v.uvSets), scalar},
	}
	for slot := range PbrSlots {
		members[PbrSlots[slot].Transform] = pbrMember{unsafe.Offsetof(v.transforms) + uintptr(slot)*vec4, vec4}
		members[PbrSlots[slot].Rotation] = pbrMember{unsafe.Offsetof(v.rotations) + uintptr(slot)*scalar, scalar}
	}
	return members
}()

// IsPbrValue reports whether name is a member of ScenePbrMaterial - one of the
// names appendParams writes - rather than a binding of its own.
func IsPbrValue(name string) bool {
	_, ok := pbrMembers[name]
	return ok
}

// Overlay writes a param named for a member of ScenePbrMaterial into that
// member, as gfx once packed it: the param's bytes verbatim, whichever
// constructor built them. It reports whether the name is a member at all; a
// member param of the wrong size writes nothing, as it never fitted the slot.
// Any other param is left to bind by its own name.
func (v *PbrValues) Overlay(param gfx.ParameterDescr) bool {
	member, ok := pbrMembers[param.Name()]
	if !ok {
		return false
	}
	if uintptr(param.ValueSize()) != member.size {
		return true
	}
	// The struct is laid out as WGSL reads it, little-endian, which the layout
	// test holds; so the member's bytes are the window of the value it spans,
	// and the append lands inside it rather than growing anything.
	bytes := unsafe.Slice((*byte)(unsafe.Pointer(v)), unsafe.Sizeof(*v))
	param.AppendValue(bytes[member.offset : member.offset : member.offset+member.size])
	return true
}
