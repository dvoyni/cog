package types

// PipelineLimits is the subset of the WebGPU limits gfx checks shaders against.
type PipelineLimits struct {
	MaxBindGroups                   int
	MaxStorageBuffersPerShaderStage int
	MaxStorageBufferBindingSize     int
	MaxUniformBuffersPerShaderStage int
	MaxUniformBufferBindingSize     int
	MaxBufferSize                   int
}
