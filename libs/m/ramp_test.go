package m

import (
	"encoding/json"
	"testing"
)

// stepRamp is the curve Ramp's own documentation reads: 1 up to 0.2, down to
// 0.2 at 0.5, a step up to 0.6, and back up to 1 at 0.8.
func stepRamp() Ramp {
	return Ramp{{X: 0.2, Y: 1}, {X: 0.5, Y: 0.2}, {X: 0.5, Y: 0.6}, {X: 0.8, Y: 1}}
}

func TestRampEvaluate(t *testing.T) {
	for _, c := range []struct {
		name string
		x    float32
		want float32
	}{
		{"before the first point holds its Y", 0, 1},
		{"on the first point", 0.2, 1},
		{"inside the falling segment", 0.3, Lerp(1, 0.2, 1.0/3)},
		{"just before the step follows the left side", 0.4999, Lerp(1, 0.2, 0.2999/0.3)},
		{"on the step is the midpoint of both sides", 0.5, 0.4},
		{"just past the step follows the right side", 0.5001, Lerp(0.6, 1, 0.0001/0.3)},
		{"inside the rising segment", 0.6, Lerp(0.6, 1, 1.0/3)},
		{"on the last point", 0.8, 1},
		{"after the last point holds its Y", 0.9, 1},
	} {
		if got := stepRamp().Evaluate(c.x); !near(got, c.want) {
			t.Errorf("%s: Evaluate(%v) = %v, want %v", c.name, c.x, got, c.want)
		}
	}
}

// A point on the curve reads back as itself, not as a lerp that lands a
// rounding error away from it.
func TestRampEvaluateHitsItsPointsExactly(t *testing.T) {
	ramp := Ramp{{X: 0, Y: 0.1}, {X: 0.3, Y: 0.7}, {X: 1, Y: 0.3}}
	for _, point := range ramp {
		if got := ramp.Evaluate(point.X); got != point.Y {
			t.Errorf("Evaluate(%v) = %v, want exactly %v", point.X, got, point.Y)
		}
	}
}

func TestRampEvaluateDegenerateRamps(t *testing.T) {
	if got := (Ramp{}).Evaluate(0.5); got != 0 {
		t.Errorf("empty ramp: Evaluate = %v, want 0", got)
	}
	single := Ramp{{X: 0.5, Y: 0.7}}
	for _, x := range []float32{0, 0.5, 1} {
		if got := single.Evaluate(x); got != 0.7 {
			t.Errorf("single point: Evaluate(%v) = %v, want 0.7", x, got)
		}
	}
	// A ramp that is nothing but a step: each side holds, and the step itself
	// is the midpoint.
	step := Ramp{{X: 0.5, Y: 0}, {X: 0.5, Y: 1}}
	for _, c := range []struct{ x, want float32 }{{0.4, 0}, {0.5, 0.5}, {0.6, 1}} {
		if got := step.Evaluate(c.x); got != c.want {
			t.Errorf("bare step: Evaluate(%v) = %v, want %v", c.x, got, c.want)
		}
	}
}

func TestRampJSONRoundTrip(t *testing.T) {
	const text = `[[0.2,1],[0.5,0.2],[0.5,0.6],[0.8,1]]`
	var ramp Ramp
	if err := json.Unmarshal([]byte(text), &ramp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := stepRamp()
	if len(ramp) != len(want) {
		t.Fatalf("read %v, want %v", ramp, want)
	}
	for index := range want {
		if ramp[index] != want[index] {
			t.Fatalf("read %v, want %v", ramp, want)
		}
	}
	out, err := json.Marshal(ramp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(out) != text {
		t.Fatalf("Marshal = %s, want %s", out, text)
	}
}

// A Ramp inside a struct goes through the same encoding, and an empty one is an
// empty array rather than null.
func TestRampJSONInAStruct(t *testing.T) {
	type holder struct {
		Curve Ramp `json:"curve"`
	}
	out, err := json.Marshal(holder{})
	if err != nil || string(out) != `{"curve":[]}` {
		t.Fatalf("Marshal(empty) = %s, %v; want {\"curve\":[]}", out, err)
	}
	var back holder
	if err := json.Unmarshal([]byte(`{"curve":[[0,1],[1,0]]}`), &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got := back.Curve.Evaluate(0.25); got != 0.75 {
		t.Fatalf("decoded curve at 0.25 = %v, want 0.75", got)
	}
	if err := json.Unmarshal([]byte(`{"curve":null}`), &back); err != nil || back.Curve != nil {
		t.Fatalf("Unmarshal(null) = %v, %v; want nil, nil", back.Curve, err)
	}
}

func TestRampJSONRejectsMalformedPoints(t *testing.T) {
	for _, text := range []string{`[[0.2]]`, `[[0.2,1,3]]`, `[[]]`, `[0.2,1]`, `{"x":1}`} {
		var ramp Ramp
		if err := json.Unmarshal([]byte(text), &ramp); err == nil {
			t.Errorf("Unmarshal(%s) = %v, want an error", text, ramp)
		}
	}
}
