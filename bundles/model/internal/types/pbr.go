package types

import (
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
// It carries no numeric parameters. The factors, the texture transforms and the
// alpha cutoff all live in the scenePbrMaterial record, which is a range of a
// per-frame arena the renderer packs itself and binds per batch — the binding
// is the addressing, so no index has to agree across the update/render thread
// boundary. What is left here is what gfx binds by name: the five textures and
// the five samplers.
//
// The scene-prefixed parameter name space is reserved for engine-supplied
// bindings, mirroring canvas's canvasTexture and canvasSampler. There is no
// separate system-bindings channel to keep in sync: sceneFrame, sceneInstances
// and scenePbrMaterial are injected as ordinary per-draw parameters and the
// existing matcher binds them exactly like a material texture. A caller
// material that names a scene* parameter is an app bug; gfx does not police it,
// and the material simply loses.
func BundledPbr(defaults PbrDefaults) [VariantCount]gfx.MaterialDescr {
	params := make([]gfx.ParameterDescr, 0, 2*len(PbrSlots))
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
	var bundled [VariantCount]gfx.MaterialDescr
	for variant := range bundled {
		// One params slice serves all four: only the shader differs, and gfx
		// copies parameters into its own arena as it records.
		bundled[variant] = gfx.MaterialWithState(
			ShaderVariant(variant).shader(), PbrState(AlphaOpaque, false), params...)
	}
	return bundled
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
