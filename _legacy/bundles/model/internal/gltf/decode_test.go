package gltf

import (
	"encoding/json"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/qmuntal/gltf"
)

func TestNodeMatrixPrefersAnExplicitMatrix(t *testing.T) {
	node := &gltf.Node{
		Matrix:      [16]float64{2, 0, 0, 0, 0, 2, 0, 0, 0, 0, 2, 0, 5, 6, 7, 1},
		Translation: [3]float64{99, 99, 99},
	}
	if got := nodeMatrix(node).Translation(); got != (m.Vec3{X: 5, Y: 6, Z: 7}) {
		t.Errorf("translation = %v, want the matrix's own", got)
	}
}

func TestNodeMatrixFallsBackToTRS(t *testing.T) {
	node := &gltf.Node{
		Translation: [3]float64{1, 2, 3},
		Rotation:    [4]float64{0, 0, 0, 1},
		Scale:       [3]float64{2, 2, 2},
	}
	matrix := nodeMatrix(node)
	if got := matrix.Translation(); got != (m.Vec3{X: 1, Y: 2, Z: 3}) {
		t.Errorf("translation = %v", got)
	}
	if matrix[0] != 2 {
		t.Errorf("scale = %v, want 2", matrix[0])
	}
}

// A payload that does not parse leaves the strength at the extension's own
// default, which renders the material as though the extension were absent.
func TestEmissiveStrengthFallsBackOnRubbish(t *testing.T) {
	if got := emissiveStrength(json.RawMessage(`{"emissiveStrength":"loud"}`), 1); got != 1 {
		t.Errorf("strength = %v, want the default", got)
	}
	if got := emissiveStrength(json.RawMessage(`{"emissiveStrength":-3}`), 1); got != 1 {
		t.Errorf("a negative strength = %v, want the default", got)
	}
	if got := emissiveStrength(json.RawMessage(`{"emissiveStrength":2}`), 1); got != 2 {
		t.Errorf("strength = %v, want 2", got)
	}
	if got := emissiveStrength("not a payload at all", 1); got != 1 {
		t.Errorf("a payload of the wrong type = %v, want the default", got)
	}
}
