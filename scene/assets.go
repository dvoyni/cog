package scene

import (
	"embed"
	"strconv"

	"github.com/dvoyni/cog/gfx"
)

const (
	shaderMountID = "builtin:scene"
	// sceneShaderPath is the bundled scene shader's root source: the two entry
	// points, composed by #include from the ten sources beside it. One vertex
	// stage and one fragment stage, because the backend hardcodes vs_main and
	// fs_main; the variants differ only in which declarations survive.
	sceneShaderPath = "builtin/scene/scene.wgsl"

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
	// It declares four functions and three constants, and no binding and no
	// struct, so including it is safe from an extending material and a
	// non-extending one alike. Its header says what it declares; do not declare
	// those names again.
	VertexDecodePath = "builtin/scene/vertexdecode.wgsl"
)

//go:embed builtin/scene/*.wgsl
var shaderFS embed.FS

// sceneShader describes one variant of the bundled shader.
//
// SCENE_MAX_LIGHTS is supplied from maxLights on every variant, which is what
// keeps the Go cap and the shader's array in step: the shader declares its own
// default and scene overrides it, so neither side can drift from the other.
func sceneShader(opts ...gfx.ShaderOption) gfx.ShaderDescr {
	return gfx.ShaderWithResource(sceneShaderPath,
		append([]gfx.ShaderOption{gfx.ShaderConst("SCENE_MAX_LIGHTS", strconv.Itoa(maxLights))}, opts...)...)
}
