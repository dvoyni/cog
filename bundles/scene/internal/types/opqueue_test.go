package types

import (
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// A mesh draw carries no colour of its own, so what it takes from the bundled
// PBR is glTF's white base colour - and metallic zero, because glTF's fully
// metallic default has no diffuse at all and scene has no environment to
// reflect, so a mesh with nothing said about it would render black.
func TestAMeshDrawTakesWhiteNonMetallicPaintFromTheBundledPbr(t *testing.T) {
	record := DrawRecord{Mesh: MeshRef{source: MeshDurable, id: 1, generation: 1}}.PbrRecord()
	if record.BaseColorFactor != (m.Vec4{X: 1, Y: 1, Z: 1, W: 1}) {
		t.Errorf("baseColorFactor is %v, want white", record.BaseColorFactor)
	}
	if record.MetallicFactor != 0 {
		t.Errorf("metallicFactor is %v, want zero", record.MetallicFactor)
	}
	if record.EmissiveFactor != (m.Vec4{}) {
		t.Errorf("emissiveFactor is %v, want nothing: a mesh draw is not self-lit", record.EmissiveFactor)
	}
	// A debug shape still paints with its own colour through the same record.
	shape := DrawRecord{Shape: ShapeBox, Color: m.NewColorSrgb(1, 0, 0, 1)}.PbrRecord()
	if shape.BaseColorFactor.X == 1 && shape.BaseColorFactor.Y == 1 {
		t.Errorf("a debug box painted %v, want its own colour", shape.BaseColorFactor)
	}
}

// A mesh draw's Params are for what a custom material declares and scene knows
// nothing about, so they do not reach the bundled PBR record. OverrideParams is
// the model draw's field and the record is the model's own; a mesh that wants a
// colour names a Material.
func TestAMeshDrawsParamsDoNotReachTheBundledRecord(t *testing.T) {
	ref := MeshRef{source: MeshDurable, id: 1, generation: 1}
	record := DrawRecord{
		Mesh:   ref,
		Params: []gfx.ParameterDescr{gfx.ColorParam("baseColorFactor", m.Color{R: 1})},
	}.PbrRecord()
	if record.BaseColorFactor != (m.Vec4{X: 1, Y: 1, Z: 1, W: 1}) {
		t.Errorf("baseColorFactor = %v, want a mesh draw's params left out of the record",
			record.BaseColorFactor)
	}
}
