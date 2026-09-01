package gfx

// ShaderDescr describes a shader by inline source text (ShaderWithText) or a
// resource path (ShaderWithResource), resolved to bytes by the renderer.
type ShaderDescr struct {
	source     shaderSource
	textOrPath string
}

// shaderSource selects how a ShaderDescr's textOrPath is interpreted.
type shaderSource int

const (
	ShaderSourceText shaderSource = iota
	ShaderSourceResource
)

// ShaderWithText describes a shader from inline source bytes (e.g. WGSL).
func ShaderWithText(text string) ShaderDescr {
	return ShaderDescr{source: ShaderSourceText, textOrPath: text}
}

// ShaderWithResource describes a shader loaded from storage.ReadFS.
func ShaderWithResource(path string) ShaderDescr {
	return ShaderDescr{source: ShaderSourceResource, textOrPath: path}
}
