package types

import (
	"strconv"

	"github.com/dvoyni/cog/slots/gfx"
)

const (
	// SceneShaderPath is the bundled scene shader's root source: the two entry
	// points, composed by #include from the ten sources beside it. One vertex
	// stage and one fragment stage, because the backend hardcodes vs_main and
	// fs_main; the variants differ only in which declarations survive.
	SceneShaderPath = "builtin/scene/scene.wgsl"

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
	VertexDecodePath = "builtin/scene/vertexdecode.wgsl"

	// FramePath is the per-pass view of the world: the camera, the sun, the
	// hemispheric ambient and the punctual lights, and the accessors that read
	// them. A custom material includes it by this absolute storage name to
	// light with the frame both renderers pack rather than a hand-copied
	// prefix of it, which drifts the first time a field is added in front of
	// the ones it reads.
	//
	// It declares:
	//   - the structs SceneFrame, SceneLight and SceneLightSample;
	//   - one binding, sceneFrame, a read-only storage buffer at @group(0)
	//     @binding(0), which scene and ecsscene both bind on every draw, so
	//     declaring it costs a material nothing and leaves nothing unfilled;
	//   - the functions sceneCameraPosition, sceneViewDirection, sceneAmbient,
	//     sceneSun, sceneLightCount and sceneLightSample;
	//   - the //#const SCENE_MAX_LIGHTS, whose default of 16 is MaxLights. An
	//     includer supplies nothing: the default is the size both renderers
	//     pack, and a gfx.ShaderConst naming any other number would read a
	//     light array the frame does not hold.
	//
	// sceneFrame is the one storage binding the prelude costs, and it is one
	// the renderer already counts: a caller material may declare no storage
	// buffer of its own, because the bundled shader's fully animated variant
	// holds all eight the browser floor allows. Do not declare those names
	// again.
	FramePath = "builtin/scene/frame.wgsl"

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
	PbrPath = "builtin/scene/pbr.wgsl"
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
