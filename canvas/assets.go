package canvas

import "embed"

const (
	shaderMountID         = "builtin:canvas"
	spriteShaderPath      = "builtin/canvas/sprite.wgsl"
	spriteBatchShaderPath = "builtin/canvas/spritebatch.wgsl"
	trianglesShaderPath   = "builtin/canvas/triangles.wgsl"
)

//go:embed builtin/canvas/*.wgsl
var shaderFS embed.FS
