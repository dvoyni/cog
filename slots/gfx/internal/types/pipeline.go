package types

// PrimitiveTopology selects how vertices assemble into primitives.
type PrimitiveTopology uint8

const (
	TopologyTriangleList PrimitiveTopology = iota
	TopologyTriangleStrip
	TopologyLineList
)

// Limits is the subset of the WebGPU limits gfx checks shaders against.
type Limits struct {
	MaxBindGroups                   int
	MaxStorageBuffersPerShaderStage int
	MaxStorageBufferBindingSize     int
	MaxUniformBufferBindingSize     int
	MaxBufferSize                   int
}
