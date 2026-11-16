package render

import (
	"slices"

	"github.com/dvoyni/cog/plugins/gfx"
)

// shaderSelection is a shader and the functions of it a draw or a dispatch
// runs, which what its parameters bind depends on: a draw's vertex and
// fragment entry points, a dispatch's compute one, an empty name none.
type shaderSelection struct {
	shader      Shader
	entryPoints [2]string
}

// usesBinding answers whether binding is used by a function s runs.
func (s shaderSelection) usesBinding(binding gfx.ShaderBinding) bool {
	return slices.ContainsFunc(binding.EntryPoints, func(name string) bool {
		return name != "" && slices.Contains(s.entryPoints[:], name)
	})
}
