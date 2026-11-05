package gfx

import "io/fs"

// ShaderDefine is one #define a shader is preprocessed with.
type ShaderDefine struct {
	Name, Value string
}

// PreprocessShader answers the WGSL at path in files after v1's directives:
// #include, #define, #const and #if. A task calls it under its own
// kernel.Read[platform.FS] and hands the result to
// Device.CreateShaderModule.
func PreprocessShader(files fs.FS, path string, defines []ShaderDefine) (string, error) {
	panic("gfx listing: no bodies")
}
