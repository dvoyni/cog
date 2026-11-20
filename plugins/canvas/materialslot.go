package canvas

import (
	"reflect"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/render"
)

// materialSlot is one 2D material made: the render material and the shader
// canvas made it of, its CullMargin, the Go type of its argument, nil for
// none, the stride of its instance data, zero for none, and whether that
// type's layout mismatched its shader's, and whether it was released, its
// render material still to give back.
type materialSlot struct {
	generation             uint32
	material               render.Material
	shader                 render.Shader
	cullMargin             float32
	argumentType           reflect.Type
	stride                 uint32
	argumentLayoutMismatch bool
	released               bool
}

// takesArgument answers whether the material, material's slot, draws an
// entry giving it argument: none, which draws the zero value of the type it
// declares, or one of that type. An argument of another type, or one given
// a material that takes none, is reported once to logger for the material
// and the type.
func (s *materialSlot) takesArgument(logger *kernel.Logger, material Material, argument recordedArgument) bool {
	if argument.goType == nil || argument.goType == s.argumentType {
		return true
	}
	reportArgumentRefused(logger, material, argument.goType, s.argumentType)
	return false
}

// findSlot answers the slot of slots, a table's or a copy of them, that
// material names, and whether it names one: the slot at its index, of its
// generation.
func findSlot(slots []materialSlot, material Material) (*materialSlot, bool) {
	if int(material.index) >= len(slots) || slots[material.index].generation != material.generation {
		return nil, false
	}
	return &slots[material.index], true
}
