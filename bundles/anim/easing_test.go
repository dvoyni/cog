package anim

import "testing"

func near(a, b float32) bool {
	const epsilon = 1e-5
	return a-b < epsilon && b-a < epsilon
}

func TestEasings(t *testing.T) {
	for _, easing := range []Easing{Linear, EaseCubicIn, EaseCubicOut, EaseCubicInOut, Hold(0.5, nil), Reverse(EaseCubicOut)} {
		if !near(easing(0), 0) || !near(easing(1), 1) {
			t.Fatalf("easing endpoints = %v %v, want 0 1", easing(0), easing(1))
		}
	}
	hold := Hold(0.5, Linear)
	if hold(0.25) != 0 || !near(hold(0.75), 0.5) {
		t.Fatalf("Hold(0.5) = %v %v, want 0 0.5", hold(0.25), hold(0.75))
	}
	if !near(Reverse(EaseCubicOut)(0.5), EaseCubicIn(0.5)) {
		t.Fatal("Reverse(EaseCubicOut) should match EaseCubicIn")
	}
}
