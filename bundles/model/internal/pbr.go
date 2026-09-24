package internal

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// PbrDefaults are the 1x1 textures every absent texture slot binds. WGSL
// requires every declared binding bound and gfx does no preprocessing, so
// omitting a texture is not available without shader variants: scene owns the
// defaults and always binds all five.
//
// There are only two, because a single white texel serves baseColor,
// metallic-roughness, occlusion and emissive: 1.0 is a fixed point of the sRGB
// transfer curve, so the sRGB-format and linear-format slots both read 1.0 from
// it. The factor parameters multiply through unchanged.
type PbrDefaults struct {
	White      gfx.TextureDescr
	FlatNormal gfx.TextureDescr
}

// PbrSampler is the sampler every default slot binds: glTF's own default wrap,
// which is repeat, filtered linearly. SamplerDesc stays comparable so
// gfx dedupes the five identical descriptors down to one GPU object.
var PbrSampler = gfx.SamplerDesc{AddressU: gfx.AddressRepeat, AddressV: gfx.AddressRepeat}

// PbrSlot is one texture slot of the bundled material: the glTF-verbatim
// parameter names of its texture and of the two record members that place it,
// plus the scene-owned name of its sampler.
//
// The transform and the rotation are named here rather than derived from a
// prefix because they are the shader's own member names, and OverrideParams
// matches against those: the record declares them flat rather than as an array
// precisely so that a caller can address one, and animating baseColorTransform
// per frame is UV scrolling.
type PbrSlot struct {
	Texture   string
	Sampler   string
	Transform string
	Rotation  string
}

// PbrSlots are the five slots, in record order. Five samplers rather than one
// shared: glTF references a sampler per texture and two slots of one material
// can legitimately differ — a tiling ground beside a clamped decal — so a
// shared sampler would silently mis-sample a legal file.
var PbrSlots = [...]PbrSlot{
	{
		Texture: "baseColorTexture", Sampler: "baseColorSampler",
		Transform: "baseColorTransform", Rotation: "baseColorRotation",
	},
	{
		Texture: "metallicRoughnessTexture", Sampler: "metallicRoughnessSampler",
		Transform: "metallicRoughnessTransform", Rotation: "metallicRoughnessRotation",
	},
	{
		Texture: "normalTexture", Sampler: "normalSampler",
		Transform: "normalTransform", Rotation: "normalRotation",
	},
	{
		Texture: "occlusionTexture", Sampler: "occlusionSampler",
		Transform: "occlusionTransform", Rotation: "occlusionRotation",
	},
	{
		Texture: "emissiveTexture", Sampler: "emissiveSampler",
		Transform: "emissiveTransform", Rotation: "emissiveRotation",
	},
}

// NormalSlot is the one slot whose default is the flat normal rather than the
// white texel.
const NormalSlot = 2

// BundledPbr builds the bundled PBR material's forward gfx material, once per
// shader variant. Every draw that names no material of its own uses it, under
// whatever pass tag its renderer wraps it in: model names no pass.
//
// Its params are BundledIngredients': the five textures and five samplers,
// and white paint's numbers as the members of the scenePbrMaterial uniform
// block. gfx binds and packs all of them by name, so a renderer drawing it
// never names one.
//
// The scene-prefixed parameter name space is reserved for engine-supplied
// bindings, mirroring canvas's canvasTexture and canvasSampler. There is no
// separate system-bindings channel to keep in sync: sceneFrame, sceneInstances
// and the rest are injected as ordinary per-draw parameters and the existing
// matcher binds them exactly like a material texture. A caller
// material that names a scene* parameter is an app bug; gfx does not police it,
// and the material simply loses.
func BundledPbr(defaults PbrDefaults) [VariantCount]gfx.MaterialDescr {
	ingredients := BundledIngredients(defaults)
	var bundled [VariantCount]gfx.MaterialDescr
	for variant := range bundled {
		// One params slice serves all four: only the shader differs, and gfx
		// copies parameters into its own arena as it records.
		bundled[variant] = gfx.MaterialWithState(
			ShaderVariant(variant).shader(), ingredients.State, ingredients.Params...)
	}
	return bundled
}

// MaterialIngredients are what a draw's material is resolved from, apart from
// its shader: the params gfx binds by name and the pipeline state. A glTF
// material keeps its own, and a baked mesh has BundledIngredients, which is
// the "file" a mesh draws from.
//
// A renderer resolves a caller's shader over them rather than over nothing, so
// a shader that replaces the bundled one keeps every texture, sampler, state
// and number the file gave its primitive. gfx matches params to bindings by
// name and drops a param no binding declares, so a shader that reads none of
// them costs nothing for their being there.
type MaterialIngredients struct {
	// Params are the five texture slots and their samplers, a default texel
	// in each slot the file left empty - all ten, always, because WGSL
	// requires every declared binding bound - and then one param per member
	// of the scenePbrMaterial uniform block, glTF's defaults included, because
	// gfx packs a member nothing supplies as zero.
	Params []gfx.ParameterDescr
	State  gfx.MaterialState
}

// BundledIngredients are a baked mesh's ingredients: the white texel in every
// picture slot and the flat normal in the normal one, each under glTF's
// default sampler, drawn opaque and single-sided with white paint.
func BundledIngredients(defaults PbrDefaults) MaterialIngredients {
	params := make([]gfx.ParameterDescr, 0, 2*len(PbrSlots)+pbrValueCount)
	for i, slot := range PbrSlots {
		texture := defaults.White
		if i == NormalSlot {
			texture = defaults.FlatNormal
		}
		params = append(params,
			gfx.TextureParam(slot.Texture, texture),
			gfx.SamplerParam(slot.Sampler, PbrSampler),
		)
	}
	paint := paintPbrValues(m.NewColorLinear(1, 1, 1, 1), false)
	return MaterialIngredients{
		Params: paint.appendParams(params),
		State:  PbrState(AlphaOpaque, false),
	}
}

// ShaderVariant is which of the bundled module's four variants a draw needs. It
// is derived from the draw rather than declared, because the thing that varies
// is what the mesh is: a debug line and a static prop read neither the poses nor
// the deltas, and declaring bindings they never read costs every such draw four
// of the eight storage buffers a browser core adapter guarantees.
type ShaderVariant int32

const (
	VariantStatic ShaderVariant = iota
	variantSkin
	variantMorph
	variantSkinMorph
	VariantCount = 4
)

// VariantFor picks the variant a draw needs from what it actually deforms.
func VariantFor(skinned, morphed bool) ShaderVariant {
	variant := VariantStatic
	if skinned {
		variant |= variantSkin
	}
	if morphed {
		variant |= variantMorph
	}
	return variant
}

// shader describes the bundled module under this variant's defines. Nothing in
// gfx needs changing to allow the cut: prepareParameterPlan walks the reflected
// layout and looks up a parameter by name for each resource, so a parameter that
// is supplied but no longer declared is never visited.
func (v ShaderVariant) shader() gfx.ShaderDescr {
	var opts []gfx.ShaderOption
	if v&variantSkin != 0 {
		opts = append(opts, gfx.ShaderDefine("SCENE_SKIN"))
	}
	if v&variantMorph != 0 {
		opts = append(opts, gfx.ShaderDefine("SCENE_MORPH"))
	}
	return SceneShader(opts...)
}

// VariantShader is shader under variant's defines: SCENE_SKIN and SCENE_MORPH
// for what the draw's geometry deforms, added to whatever supply the shader
// already carries. The zero shader is no shader, and reads as the bundled one.
//
// The variant is the geometry's and never the caller's, whichever shader is in
// effect: the group 2 buffers a draw binds are the ones its geometry needs, so
// a shader without SCENE_SKIN would draw a skinned model in its bind pose, and
// one with it over a static model would declare a binding nothing fills.
func VariantShader(shader gfx.ShaderDescr, variant ShaderVariant) gfx.ShaderDescr {
	if shader == (gfx.ShaderDescr{}) {
		return variant.shader()
	}
	switch variant {
	case variantSkin:
		return shader.With(gfx.ShaderDefine("SCENE_SKIN"))
	case variantMorph:
		return shader.With(gfx.ShaderDefine("SCENE_MORPH"))
	case variantSkinMorph:
		return shader.With(gfx.ShaderDefine("SCENE_SKIN"), gfx.ShaderDefine("SCENE_MORPH"))
	}
	return shader
}

// AlphaMode is glTF's alphaMode, which selects fixed-function state and, for
// alphaMask, a shader discard.
type AlphaMode uint8

const (
	AlphaOpaque AlphaMode = iota
	AlphaMask
	AlphaBlend
)

// PbrState maps glTF's alphaMode and doubleSided onto pipeline state.
//
// MASK is fixed-function-identical to OPAQUE — it writes depth and batches with
// the opaque geometry — because the cutoff is entirely a fragment-shader
// concern: the shader discards against alphaCutoff, which is zero for an opaque
// material and therefore a no-op there. It cannot be alpha-to-coverage, which
// needs MSAA.
func PbrState(alpha AlphaMode, doubleSided bool) gfx.MaterialState {
	state := gfx.StateOpaque3D()
	if alpha == AlphaBlend {
		state = gfx.StateTransparent3D()
	}
	if doubleSided {
		state.Cull = gfx.CullNone
	} else {
		state.Cull = gfx.CullBack
	}
	return state
}
