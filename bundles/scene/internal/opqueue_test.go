package internal

import (
	"testing"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// A mesh draw carries no colour of its own, so it appends no paint: it draws
// the bundled PBR's white paint, or whatever its params say by name. A debug
// shape appends paint of its own colour, as model spells it.
func TestOnlyADebugShapeAppendsPaint(t *testing.T) {
	if paint := (DrawRecord{Mesh: model.NewMeshRef(model.MeshDurable, 1, 1)}).AppendPaint(nil); len(paint) != 0 {
		t.Errorf("a mesh draw appended %d paint params, want none", len(paint))
	}
	color := m.NewColorSrgb(1, 0, 0, 1)
	got := DrawRecord{Shape: ShapeBox, Color: color}.AppendPaint(nil)
	want := model.PaintParams(nil, color, false)
	if gfx.FingerprintParams(got) != gfx.FingerprintParams(want) {
		t.Errorf("a debug box appended %v, want model's paint %v", gfx.ParameterViewsOf(got), gfx.ParameterViewsOf(want))
	}
}
