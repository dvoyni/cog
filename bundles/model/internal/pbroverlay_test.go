package internal

import (
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// Overlay is the member-level write a renderer keeps for this one block: every
// param appendParams writes, laid over zero values, rebuilds the values it was
// written from, member for member - so the names and the offsets agree.
func TestOverlayingEveryMemberRebuildsTheValues(t *testing.T) {
	values := defaultPbrValues()
	values.baseColorFactor = m.Vec4{X: 0.25, Y: 0.5, Z: 0.75, W: 1}
	values.emissiveFactor = m.Vec4{X: 2}
	values.transforms[NormalSlot] = m.Vec4{X: 1, Y: 2, Z: 3, W: 4}
	values.rotations[len(values.rotations)-1] = 0.5
	values.alphaCutoff = 0.3
	values.uvSets = 0b10110

	var rebuilt PbrValues
	for _, param := range values.appendParams(nil) {
		if !rebuilt.Overlay(param) {
			t.Errorf("appendParams wrote %q, which Overlay does not know as a member", param.Name)
		}
		if !IsPbrValue(param.Name) {
			t.Errorf("IsPbrValue(%q) is false for a member appendParams writes", param.Name)
		}
	}
	if rebuilt != values {
		t.Errorf("the overlay rebuilt %+v, want %+v", rebuilt, values)
	}
}

// A colour lands as its four floats, a name that is no member is left for the
// caller to bind by its own name, and a member param of the wrong size writes
// nothing.
func TestOverlayWritesOneMemberAndLeavesTheRest(t *testing.T) {
	values := defaultPbrValues()
	want := values
	want.baseColorFactor = m.Vec4{X: 1, W: 0.5}
	if !values.Overlay(gfx.ShaderParameterColor("baseColorFactor", m.Color{R: 1, A: 0.5})) {
		t.Fatal("baseColorFactor is not a member")
	}
	if values.Overlay(gfx.ShaderParameterFloat("fade", 1)) || IsPbrValue("fade") {
		t.Error("fade was taken for a member")
	}
	if !values.Overlay(gfx.ShaderParameterFloat("emissiveFactor", 9)) {
		t.Error("emissiveFactor at the wrong size was not recognised as a member")
	}
	if values != want {
		t.Errorf("the overlay left %+v, want %+v", values, want)
	}
}
