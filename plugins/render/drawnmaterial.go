package render

import "github.com/dvoyni/cog/plugins/gfx"

// drawnMaterialCount is how many materials a pass keeps what its draws
// found of: enough for draws switching between a few.
const drawnMaterialCount = 4

// drawnMaterial is what a pass's draws found of one material, kept for its
// later draws of it while no material changes: the material, and the
// table's count of material changes it was found at; its label; the slot of
// its shader; the key of its pipelines, but for a draw's layout; and the
// functions it runs. Once a draw found them, as bindingFound says, it holds
// the material's bind group of @group(1), the name of a storage binding of
// that group left unbound that a function it runs reads, and, while its
// list is inspected, the group's parameters as the inspection writes them
// down. pipeline is the pipeline its last draw drew with, for layout.
type drawnMaterial struct {
	material            Material
	changeCount         uint64
	label               string
	shader              shaderSlot
	key                 pipelineKey
	selection           shaderSelection
	bindingFound        bool
	binding             groupBinding
	unboundRead         string
	inspectedParameters []InspectedParameter
	pipeline            gfx.RenderPipeline
	layout              drawLayout
}

// makeDrawnMaterial answers what a pass of formats found of material, its
// slot slot and its shader's shader, at the table's changeCount.
func makeDrawnMaterial(material Material, slot *materialSlot, shader shaderSlot, formats passFormat, changeCount uint64) drawnMaterial {
	return drawnMaterial{
		material:    material,
		changeCount: changeCount,
		label:       slot.label,
		shader:      shader,
		key: pipelineKey{
			shader:             slot.shader,
			vertexEntryPoint:   slot.vertexEntryPoint,
			fragmentEntryPoint: slot.fragmentEntryPoint,
			state:              slot.state.makeKeyState(formats),
			formats:            formats,
		},
		selection: slot.makeShaderSelection(),
	}
}
