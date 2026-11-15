package render

import "github.com/dvoyni/cog/gfx"

// maxGroupDynamicOffsetCount is the most dynamic offsets one group of
// render's layouts binds at: more than the 8 uniform blocks and 4 storage
// buffers WebGPU's default limits allow a pipeline.
const maxGroupDynamicOffsetCount = 16

// bindGroupSet is what a pass's draws, or a compute pass's dispatches, bind
// for each group of their shader's layout, kept between them: the bind
// group with its dynamic offsets; the name of a storage binding of the group
// left unbound that a function the draw or dispatch runs reads, for which it
// is skipped, empty for none; the shader and functions the group's
// parameters set on the pass were resolved for, none since they last
// changed; and the binding last set on the pass's encoder.
type bindGroupSet struct {
	groupBindings [reservedGroup]groupBinding
	unboundReads  [reservedGroup]string
	resolvedFor   [reservedGroup]shaderSelection
	boundGroups   [reservedGroup]groupBinding
}

// findUnboundRead answers the name of the first storage binding of s's
// first groupCount groups left unbound that the draw or dispatch reads;
// empty for none.
func (s *bindGroupSet) findUnboundRead(groupCount int) string {
	for _, name := range s.unboundReads[:groupCount] {
		if name != "" {
			return name
		}
	}
	return ""
}

// setChangedGroups sets on encoder each of s's first groupCount groups
// whose binding is not the one last set, handing gfx its dynamic offsets in
// dynamicOffsets, its list's.
func (s *bindGroupSet) setChangedGroups(encoder bindGroupEncoder, groupCount int, dynamicOffsets *[maxGroupDynamicOffsetCount]uint32) {
	for group := range groupCount {
		binding := &s.groupBindings[group]
		if *binding != s.boundGroups[group] {
			offsets := dynamicOffsets[:binding.dynamicOffsetCount]
			copy(offsets, binding.dynamicOffsets[:])
			encoder.SetBindGroup(uint32(group), binding.bindGroup, offsets)
			s.boundGroups[group] = *binding
		}
	}
}

// bindGroupEncoder is the encoder of a pass or a compute pass, which bind
// groups are set on.
type bindGroupEncoder interface {
	SetBindGroup(index uint32, group gfx.BindGroup, dynamicOffsets []uint32)
}

// groupBinding is a bind group as a pass sets it: the group, and its first
// dynamicOffsetCount dynamicOffsets, zero where nothing fills a block.
type groupBinding struct {
	bindGroup          gfx.BindGroup
	dynamicOffsets     [maxGroupDynamicOffsetCount]uint32
	dynamicOffsetCount int
}
