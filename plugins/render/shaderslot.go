package render

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/dvoyni/cog/plugins/gfx"
)

// shaderSlot is the slot of one shader: the gfx module behind its handle,
// the pipeline layout of its bindings, which it holds in the layout
// interner, the entry points the module declares, and its bindings, sorted
// by group and number as its layout's entries are, with the index of each
// group's first, each none for a shader that failed or was refused; and,
// for a loaded variant, the defines it was compiled with, sorted by name,
// which a reload compiles it with again.
type shaderSlot struct {
	module      gfx.ShaderModule
	layout      *internedPipelineLayout
	entryPoints []gfx.ShaderEntryPoint
	bindings    []gfx.ShaderBinding
	groupStarts [reservedGroup + 1]int
	defines     []gfx.ShaderDefine
}

// makeShaderSlot answers the slot of module, its layout, its entry points
// and bindings, which it sorts by group and number.
func makeShaderSlot(module gfx.ShaderModule, layout *internedPipelineLayout, entryPoints []gfx.ShaderEntryPoint, bindings []gfx.ShaderBinding) shaderSlot {
	slices.SortFunc(bindings, func(a, b gfx.ShaderBinding) int {
		return cmp.Or(cmp.Compare(a.Group, b.Group), cmp.Compare(a.Binding, b.Binding))
	})
	slot := shaderSlot{module: module, layout: layout, entryPoints: entryPoints, bindings: bindings}
	for group := range slot.groupStarts {
		slot.groupStarts[group] = len(bindings)
		if index := slices.IndexFunc(bindings, func(binding gfx.ShaderBinding) bool { return binding.Group >= uint32(group) }); index >= 0 {
			slot.groupStarts[group] = index
		}
	}
	return slot
}

// findGroupBindings answers s's bindings of group, below the reserved one,
// sorted by number, each at the index of its entry in the group's layout.
func (s shaderSlot) findGroupBindings(group uint32) []gfx.ShaderBinding {
	if group >= reservedGroup {
		panic(fmt.Sprintf("render: finding a shader's bindings of @group(%d), which render reserves", group))
	}
	return s.bindings[s.groupStarts[group]:s.groupStarts[group+1]]
}

// findEntryPoint answers the name of s's function of stage named name, an
// empty name meaning its only one of stage, and an empty name when name is
// empty and s has none of stage. A name s has no function of stage of, or
// an empty name when s has several of stage, answers an error naming them.
func (s shaderSlot) findEntryPoint(stage gfx.ShaderStageFlags, name string) (string, error) {
	var names []string
	for _, entryPoint := range s.entryPoints {
		if entryPoint.Stage != stage {
			continue
		}
		if entryPoint.Name == name {
			return name, nil
		}
		names = append(names, entryPoint.Name)
	}
	switch {
	case name != "":
		return "", fmt.Errorf("names %q, which is none of the shader's functions of its stage, %q", name, names)
	case len(names) > 1:
		return "", fmt.Errorf("names none of the shader's functions of its stage, %s, of which it has several", strings.Join(names, ", "))
	case len(names) == 1:
		return names[0], nil
	}
	return "", nil
}

// releaseModule releases s's module and its holding of its layout in
// layouts; a slot with no module releases nothing.
func (s shaderSlot) releaseModule(device gfx.Device, layouts *layoutInterner) {
	if s.module != (gfx.ShaderModule{}) {
		device.ReleaseShaderModule(s.module)
		layouts.release(device, s.layout)
	}
}
