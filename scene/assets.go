package scene

import "embed"

const (
	shaderMountID = "builtin:scene"
	// sceneShaderPath is the bundled scene shader: one module, one vertex stage
	// and one fragment stage, because gfx does no shader preprocessing and a
	// variant would be a second module carrying its own copy of everything.
	sceneShaderPath = "builtin/scene/scene.wgsl"
)

//go:embed builtin/scene/*.wgsl
var shaderFS embed.FS
