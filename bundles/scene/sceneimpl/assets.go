package sceneimpl

import "embed"

// shaderMountID is the storage mount the bundled shaders are served under.
const shaderMountID = "builtin:scene"

// shaderFS is what Start mounts at math.MaxInt priority: the root source and
// the ten sources it includes. The paths inside it are the ones
// internal.SceneShaderPath and scene.VertexDecodePath spell.
//
//go:embed builtin/scene/*.wgsl
var shaderFS embed.FS
