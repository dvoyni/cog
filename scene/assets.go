package scene

import (
	"embed"
	"strconv"

	"github.com/dvoyni/cog/gfx"
)

const (
	shaderMountID = "builtin:scene"
	// sceneShaderPath is the bundled scene shader's root source: the two entry
	// points, composed by #include from the nine sources beside it. One vertex
	// stage and one fragment stage, because the backend hardcodes vs_main and
	// fs_main; the variants differ only in which declarations survive.
	sceneShaderPath = "builtin/scene/scene.wgsl"
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
