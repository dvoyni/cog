package types

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// TestClampLengthIsChipmunksVectorClamp pins the one vector helper the pivot
// and the groove need that the m package does not carry, including the
// infinite ceiling every Joint arrives with.
func TestClampLengthIsChipmunksVectorClamp(t *testing.T) {
	v := m.Vec2d{X: 3, Y: 4}
	if got := clampLength(v, math.Inf(1)); got != v {
		t.Errorf("an infinite ceiling changed %v into %v", v, got)
	}
	if got := clampLength(v, 10); got != v {
		t.Errorf("a ceiling above the length changed %v into %v", v, got)
	}
	got := clampLength(v, 2.5)
	if math.Abs(got.Length()-2.5) > 1e-12 {
		t.Errorf("clamping to 2.5 gave a vector of length %v", got.Length())
	}
	if math.Abs(got.X-1.5) > 1e-12 || math.Abs(got.Y-2) > 1e-12 {
		t.Errorf("clamping to 2.5 turned the vector: %v", got)
	}
}
