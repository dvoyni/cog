package types

import "math"

// near reports whether two floats agree to the tolerance the geometry tests
// assert at.
func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }

func abs32(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}
