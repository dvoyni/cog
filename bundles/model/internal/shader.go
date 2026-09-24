package internal

import (
	"strconv"

	"github.com/dvoyni/cog/slots/gfx"
)

const (
	// SceneShaderPath is the bundled scene shader's root source: the two entry
	// points, composed by #include from the ten sources beside it. One vertex
	// stage and one fragment stage, because the backend hardcodes vs_main and
	// fs_main; the variants differ only in which declarations survive.
	SceneShaderPath = "builtin/model/scene.wgsl"

	// VertexDecodePath is the one published source of the ten: the decode for
	// the four attributes a storage vertex holds encoded, the normal as oct32
	// in a two-component 16-bit unorm, the tangent as one word of oct 15/15
	// plus handedness, and each UV set as a two-component 16-bit unorm against
	// its mesh's range.
	//
	// A custom material drawing a standard-layout mesh includes it by this
	// absolute storage name and calls sceneDecodeNormal and sceneDecodeTangent,
	// rather than re-typing the arithmetic or - what the float layout allowed -
	// declaring @location(1) as a vec3<f32> and shading from whatever the fetch
	// unit made of two unorm components. That declaration is now refused at
	// pipeline time, which is the point: the storage layout is contract, so the
	// decode for it is published rather than private.
	//
	// The UV half is the one nothing refuses: @location(3) and @location(4) are
	// vec2<f32> whether they store as floats or as unorms, so a material that
	// samples a texture and skips sceneDecodeUV samples the wrong place rather
	// than failing to build. The scale and the bias come from the mesh record,
	// which instance.wgsl declares at @group(0) @binding(3) and sceneMeshOf
	// reaches - sceneDecodeUV itself takes them as arguments, which is what
	// keeps this file free of bindings.
	//
	// It declares four functions, sceneOctDecode, sceneDecodeNormal,
	// sceneDecodeTangent and sceneDecodeUV, and three constants,
	// SCENE_OCT_TANGENT_MAX, SCENE_TANGENT_Y_SHIFT and SCENE_TANGENT_HANDEDNESS.
	// It declares no binding and no struct, so including it is safe from an
	// extending material and a non-extending one alike. Its header says the
	// same; do not declare those names again.
	VertexDecodePath = "builtin/model/vertexdecode.wgsl"

	// FramePath is the per-pass view of the world: the camera, the sun, the
	// hemispheric ambient and the punctual lights, and the accessors that read
	// them. A custom material includes it by this absolute storage name to
	// light with the frame the renderer packs rather than a hand-copied
	// prefix of it, which drifts the first time a field is added in front of
	// the ones it reads.
	//
	// It declares:
	//   - the structs SceneFrame, SceneLight and SceneLightSample;
	//   - one binding, sceneFrame, a read-only storage buffer at @group(0)
	//     @binding(0), which scene binds on every draw, so
	//     declaring it costs a material nothing and leaves nothing unfilled;
	//   - the functions sceneCameraPosition, sceneViewDirection, sceneAmbient,
	//     sceneSun, sceneLightCount and sceneLightSample;
	//   - the //#const SCENE_MAX_LIGHTS, whose default of 16 is MaxLights. An
	//     includer supplies nothing: the default is the size the renderer
	//     packs, and a gfx.ShaderConst naming any other number would read a
	//     light array the frame does not hold.
	//
	// sceneFrame is the one storage binding the prelude costs, and it is one
	// the renderer already counts. The bundled shader's fully animated
	// variant holds seven of the eight storage buffers the browser floor
	// allows, so a caller material over the bundled stages has one of its own
	// to spend. Do not declare those names again.
	FramePath = "builtin/model/frame.wgsl"

	// PbrPath is the bundled material's BRDF and lighting loop: everything
	// between a shaded surface and the radiance leaving it. A custom material
	// fills a SceneSurface however it likes - from its own vertices, its own
	// textures, a procedure - and hands it to sceneShadeSurface, which lights
	// it with the sun, every punctual light in the pass and the hemispheric
	// ambient exactly as the bundled material is lit. Emissive is the
	// material's own and is added to the result.
	//
	// It declares:
	//   - the structs SceneSurface and ScenePbrSurface;
	//   - the constants SCENE_PI and SCENE_DIELECTRIC_F0;
	//   - the BRDF terms sceneD_GGX, sceneV_SmithGGXCorrelated and
	//     sceneF_Schlick, sceneEnvBRDFApprox, scenePunctualContribution, and
	//     sceneShadeSurface;
	//   - no binding of its own.
	//
	// It includes FramePath itself, so including it brings in everything
	// FramePath declares, sceneFrame among it, once: include-once is by
	// resolved path, so a material that includes both gets one copy. Do not
	// declare those names again.
	PbrPath = "builtin/model/pbr.wgsl"

	// VertexStagePath is the bundled vertex stage whole: vs_main, which
	// decodes, deforms and places a vertex exactly as the bundled shader does
	// and hands the fragment stage a SceneVertexOut. A custom scene shader
	// includes it and FragmentStagePath and writes only its own fs_main,
	// which is how it becomes the bundled PBR plus one step rather than a
	// copy of it that drifts.
	//
	// It declares:
	//   - the entry point vs_main, so an includer declares no vertex stage;
	//   - through the private sources it includes, every name the stage
	//     reads: the structs SceneVertexIn, SceneVertexOut and SceneVertex,
	//     the group 0 bindings sceneInstances, sceneAnim and sceneMeshes
	//     beside FramePath's sceneFrame, and under SCENE_SKIN and SCENE_MORPH
	//     the group 2 ones. Each of those begins scene, Scene or SCENE_, and
	//     none is contract beyond that prefix.
	//
	// The variant is the renderer's: it supplies SCENE_SKIN and SCENE_MORPH
	// for the draw's geometry whatever shader is in effect, so an includer
	// declares neither. Do not declare those names again.
	VertexStagePath = "builtin/model/vertexstage.wgsl"

	// FragmentStagePath is the bundled fragment stage as a function a custom
	// fs_main calls: scenePbrFragment(in, frontFacing) is the surface the
	// file's material describes, its MASK discard and its shading, as linear
	// radiance in rgb and coverage in a.
	//
	// It declares:
	//   - the function scenePbrFragment, which discards and so is callable
	//     only from a fragment stage;
	//   - through what it includes, SceneVertexOut, PbrPath and FramePath
	//     whole, and the material's bindings: scenePbrMaterial, the uniform
	//     block at @group(1) @binding(0), and the five textures and five
	//     samplers PbrSlots names at @group(1) @binding(1) to (10), every one
	//     filled on every draw from the file or the bundled defaults.
	//
	// scenePbrMaterial is the one uniform block gfx allows a shader. An
	// includer with per-draw numbers of its own adds them to it, by composing
	// the block itself from MaterialProloguePath, a fields source of its own
	// over MaterialFieldsPath, and MaterialEpiloguePath, before it includes
	// this. Group 3 is left for the includer's own bindings, which ride as
	// params on the Material or on the default scene shader. Do not declare
	// those names again.
	FragmentStagePath = "builtin/model/fragmentstage.wgsl"

	// MaterialProloguePath opens the material's uniform block: it declares
	// the struct ScenePbrMaterial, and nothing after its opening brace.
	//
	// The block is three sources because gfx fills one uniform block per
	// shader, by member name per draw, so a custom shader's own per-draw
	// numbers belong in this block, and a struct cannot be reopened. The
	// bundled material includes the three in order. An extending shader
	// includes this, then a fields source of its own that includes
	// MaterialFieldsPath and lists its members after it, then
	// MaterialEpiloguePath - all before FragmentStagePath or anything else
	// that reaches the material. Includes are once per resolved path, so the
	// material's own three are then skipped and the block holds every member,
	// once. Included the other way round it does not compile: the extension's
	// members fall outside any struct. A shader extending an extension
	// includes that extension's fields source in its own, so extensions stack.
	// Do not declare those names again.
	MaterialProloguePath = "builtin/model/materialprologue.wgsl"

	// MaterialFieldsPath is the inside of the material's uniform block: the
	// members baseColorFactor, emissiveFactor, baseColorTransform,
	// metallicRoughnessTransform, normalTransform, occlusionTransform,
	// emissiveTransform, baseColorRotation, metallicRoughnessRotation,
	// normalRotation, occlusionRotation, emissiveRotation, metallicFactor,
	// roughnessFactor, normalScale, occlusionStrength, alphaCutoff and uvSets,
	// 160 bytes of the 256 gfx allows a block. It is not WGSL on its own; an
	// extension's fields source includes it first and names its own members
	// with a prefix of its own after it. Do not declare those names again.
	MaterialFieldsPath = "builtin/model/materialfields.wgsl"

	// MaterialEpiloguePath closes the material's uniform block and declares
	// its binding, scenePbrMaterial, the uniform block at @group(1)
	// @binding(0). Do not declare those names again.
	MaterialEpiloguePath = "builtin/model/materialepilogue.wgsl"
)

// SceneShader describes one variant of the bundled shader.
//
// SCENE_MAX_LIGHTS is supplied from MaxLights on every variant, which is what
// keeps the Go cap and the shader's array in step: the shader declares its own
// default and scene overrides it, so neither side can drift from the other.
func SceneShader(opts ...gfx.ShaderOption) gfx.ShaderDescr {
	return gfx.ShaderWithResource(SceneShaderPath,
		append([]gfx.ShaderOption{gfx.ShaderConst("SCENE_MAX_LIGHTS", strconv.Itoa(MaxLights))}, opts...)...)
}
