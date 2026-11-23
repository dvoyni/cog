package canvas

import (
	"cmp"
	"fmt"
	"reflect"
	"strings"

	"github.com/dvoyni/cog/plugins/render"
)

// materialSlot is one 2D material made: its descriptor's Label, or its
// Shader when it has none, which its inspected batches carry; the render
// materials and the
// shader canvas made it of, material of its own vertex stage and
// meshMaterial of canvas's mesh vertex stage, whether material's vertex
// stage is its shader's own, which a mesh of another layout than
// canvas.Vertex's draws through, its CullMargin, the Go type of its
// argument, nil for none, the stride of its instance data, zero for none,
// and whether that type's layout mismatched its shader's, and whether it was
// released, its render materials still to give back.
type materialSlot struct {
	generation             uint32
	label                  string
	material               render.Material
	meshMaterial           render.Material
	shader                 render.Shader
	ownVertexStage         bool
	cullMargin             float32
	argumentType           reflect.Type
	stride                 uint32
	argumentLayoutMismatch bool
	released               bool
}

// makeMaterialSlot makes the slot of a 2D material of descriptor through
// inventory, its render materials and its shader acquired, its argument's
// layout checked against its shader's, a mismatch reported once
// for the shader and the type. Its vertex stage is its shader's own when it
// names one, not canvas's, the include's and its materials' names beginning
// canvas_, whose quads and canvas.Vertex meshes read no vertex of another
// layout.
func makeMaterialSlot(inventory render.Inventory, reporter *reporter, descriptor MaterialDescriptor) materialSlot {
	material, meshMaterial, shader := descriptor.makeRenderMaterials(inventory)
	slot := materialSlot{label: cmp.Or(descriptor.Label, descriptor.Shader), material: material, meshMaterial: meshMaterial, shader: shader, ownVertexStage: descriptor.VertexEntryPoint != "" && !strings.HasPrefix(descriptor.VertexEntryPoint, canvasNamePrefix), cullMargin: descriptor.CullMargin, argumentType: descriptor.Argument.goType, stride: findInstanceStride(descriptor.Argument.goType)}
	if slot.argumentType == nil {
		return slot
	}
	if err := descriptor.Argument.checkLayout(inventory, shader); err != nil {
		slot.argumentLayoutMismatch = true
		mismatch := Report{Kind: ReportKindArgumentLayoutMismatch, Err: fmt.Errorf("canvas: the argument %v of a material of shader %q does not match the shader's struct Argument, so its entries are skipped: %w", slot.argumentType, descriptor.Shader, err)}
		reporter.reportOnce(argumentLayoutMismatchLogKey+descriptor.Shader+" "+slot.argumentType.String(), mismatch)
	}
	return slot
}

// takesArgument answers whether the material, material's slot, draws an
// entry giving it argument: none, which draws the zero value of the type it
// declares, or one of that type. An argument of another type, or one given
// a material that takes none, is reported once for the material
// and the type.
func (s *materialSlot) takesArgument(reporter *reporter, material Material, argument recordedArgument) bool {
	if argument.goType == nil || argument.goType == s.argumentType {
		return true
	}
	reportArgumentRefused(reporter, material, argument.goType, s.argumentType)
	return false
}

// drawsLayout answers whether the material, material's slot, draws a mesh
// of canvas.Vertex's layout, standard, or of another: always the first,
// and the second when its vertex stage is its shader's own. One whose
// vertex stage is canvas's, refused another layout, is reported once for
// the material.
func (s *materialSlot) drawsLayout(reporter *reporter, material Material, standard bool) bool {
	if standard || s.ownVertexStage {
		return true
	}
	missing := Report{Kind: ReportKindVertexStageMissing, Err: fmt.Errorf("canvas: material %v has no vertex stage of its own, so a mesh of another layout than canvas.Vertex's drawn with it is skipped", material)}
	reporter.reportOnce(vertexStageMissingLogKey+fmt.Sprint(material), missing)
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
