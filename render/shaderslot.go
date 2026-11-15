package render

import (
	"fmt"
	"strings"

	"github.com/dvoyni/cog/gfx"
)

// shaderSlot is the slot of one shader: the gfx module behind its handle,
// the pipeline layout of its bindings, which it holds in the layout
// interner, and the entry points the module declares, each none for a
// shader that failed or was refused.
type shaderSlot struct {
	module      gfx.ShaderModule
	layout      *internedPipelineLayout
	entryPoints []gfx.ShaderEntryPoint
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
