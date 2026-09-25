package internal

import "embed"

// shaderMountID is the storage mount scene's own shaders are served under.
// It keeps the name of the paths it serves, which are builtin/scene/.
const shaderMountID = "builtin:scene"

// debugShaderPath is the debug shapes' shader's storage path.
const debugShaderPath = "builtin/scene/debug.wgsl"

// shaderFS is what storage mounts at math.MaxInt priority: the debug shapes'
// shader. It includes model's published sources by their storage paths, which
// model's own mount serves, so nothing of model's is copied here.
//
//go:embed builtin/scene/*.wgsl
var shaderFS embed.FS
