package canvas

import "github.com/dvoyni/cog/plugins/render"

// materialSlot is one 2D material made: the render material and the shader
// canvas made it of, its CullMargin, and whether it was released, its
// render material still to give back.
type materialSlot struct {
	generation uint32
	material   render.Material
	shader     render.Shader
	cullMargin float32
	released   bool
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
