package render

import "github.com/dvoyni/cog/gfx"

// maxGroupDynamicOffsetCount is the most dynamic offsets one group of
// render's layouts binds at: more than the 8 uniform blocks and 4 storage
// buffers WebGPU's default limits allow a pipeline.
const maxGroupDynamicOffsetCount = 16

// drawBindGroupSet is what a pass's draws bind for each group of their
// shader's layout, kept between draws: the bind group with its dynamic
// offsets; the name of a storage binding of the group left unbound that a
// function the draw runs reads, for which it is skipped, empty for none; and
// the shader and functions the group's pass or draw parameters were
// resolved for, none since they last changed.
type drawBindGroupSet struct {
	groupBindings [reservedGroup]groupBinding
	unboundReads  [reservedGroup]string
	resolvedFor   [reservedGroup]shaderSelection
}

// shaderSelection is a shader and the functions of it a draw runs, which
// what the draw's pass and draw parameters bind depends on.
type shaderSelection struct {
	shader                               Shader
	vertexEntryPoint, fragmentEntryPoint string
}

// groupBinding is a bind group as a pass sets it: the group, and its first
// dynamicOffsetCount dynamicOffsets, zero where nothing fills a block.
type groupBinding struct {
	bindGroup          gfx.BindGroup
	dynamicOffsets     [maxGroupDynamicOffsetCount]uint32
	dynamicOffsetCount int
}
