package render

import "github.com/dvoyni/cog/plugins/gfx"

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
// changed; for each dynamic offset of a resolved group, the index among
// the group's settings of the uniform block it binds, -1 for none; the
// binding last set on the pass's encoder; and, while its list is
// inspected, the parameters each group binds as the inspection writes them
// down.
type bindGroupSet struct {
	groupBindings       [reservedGroup]groupBinding
	unboundReads        [reservedGroup]string
	resolvedFor         [reservedGroup]shaderSelection
	offsetSettings      [reservedGroup][maxGroupDynamicOffsetCount]int
	boundGroups         [reservedGroup]groupBinding
	inspectedParameters [reservedGroup][]InspectedParameter
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

// putMaterialBinding puts the bind group material, found by its pass,
// binds for its own parameters in s, as @group(1), where its shader has it.
func (s *bindGroupSet) putMaterialBinding(material *drawnMaterial) {
	if len(material.shader.layout.groups) > materialGroup {
		s.groupBindings[materialGroup] = material.binding
		s.unboundReads[materialGroup] = material.unboundRead
		s.inspectedParameters[materialGroup] = material.inspectedParameters
	}
}

// moveDynamicOffset moves the dynamic offset of group, resolved, that binds
// the uniform block of the setting at index among the group's settings to
// that of setting, which replaces previous there; false when the group must
// be resolved again: it is not resolved, no dynamic offset of it binds that
// setting, or setting differs from previous by more than its offset in the
// same arena chunk.
func (s *bindGroupSet) moveDynamicOffset(group, index int, previous, setting *parameterSetting) bool {
	if s.resolvedFor[group] == (shaderSelection{}) || previous.kind != parameterKindUniform || setting.kind != parameterKindUniform ||
		previous.uniformType != setting.uniformType || previous.bufferRange.Buffer != setting.bufferRange.Buffer || previous.bufferRange.Size != setting.bufferRange.Size {
		return false
	}
	binding := &s.groupBindings[group]
	for offsetIndex, settingIndex := range s.offsetSettings[group][:binding.dynamicOffsetCount] {
		if settingIndex == index {
			binding.dynamicOffsets[offsetIndex] = uint32(setting.bufferRange.Offset)
			return true
		}
	}
	return false
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
