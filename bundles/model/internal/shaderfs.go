package internal

import "embed"

// shaderMountID is the storage mount the bundled shaders are served under. It
// keeps the name of the paths it serves, which are builtin/scene/ because they
// are the bundled scene shader's and are published under that name.
const shaderMountID = "builtin:scene"

// shaderFS is what storage mounts at math.MaxInt priority: the root source and
// the twelve sources it includes. The paths inside it are the ones
// model.SceneShaderPath and the five published sources spell.
//
// The shader is model's because it reads model's records and params - the
// material's numbers, the mesh record, the poses and the deltas - so the two
// change together, and
// every renderer that draws a model draws with it rather than mounting its own.
//
//go:embed builtin/scene/*.wgsl
var shaderFS embed.FS
