package m

import (
	"math"
	"testing"
)

const tolerance2d = 1e-12

func TestVec2dConstructorTakesZeroOneOrTwoValues(t *testing.T) {
	if got, want := NewVec2d(), (Vec2d{}); got != want {
		t.Fatalf("NewVec2d() = %v, want %v", got, want)
	}
	if got, want := NewVec2d(2), (Vec2d{2, 2}); got != want {
		t.Fatalf("NewVec2d(2) = %v, want %v", got, want)
	}
	if got, want := NewVec2d(1, 2), (Vec2d{1, 2}); got != want {
		t.Fatalf("NewVec2d(1, 2) = %v, want %v", got, want)
	}
	assertPanics(t, func() { NewVec2d(1, 2, 3) })
	assertPanics(t, func() { Vec2d{}.MulS() })
}

func TestVec2dCarriesVec2sMethodNames(t *testing.T) {
	v := Vec2d{3, 4}
	other := Vec2d{1, 2}

	if got, want := v.Add(other), (Vec2d{4, 6}); got != want {
		t.Fatalf("Add = %v, want %v", got, want)
	}
	if got, want := v.Sub(other), (Vec2d{2, 2}); got != want {
		t.Fatalf("Sub = %v, want %v", got, want)
	}
	if got, want := v.MulS(2), (Vec2d{6, 8}); got != want {
		t.Fatalf("MulS scalar = %v, want %v", got, want)
	}
	if got, want := v.MulS(2, 3), (Vec2d{6, 12}); got != want {
		t.Fatalf("MulS per axis = %v, want %v", got, want)
	}
	if got, want := v.Negate(), (Vec2d{-3, -4}); got != want {
		t.Fatalf("Negate = %v, want %v", got, want)
	}
	if got, want := v.Dot(other), 11.0; got != want {
		t.Fatalf("Dot = %v, want %v", got, want)
	}
	if got, want := v.Cross(other), 2.0; got != want {
		t.Fatalf("Cross = %v, want %v", got, want)
	}
	if got, want := v.LengthSquared(), 25.0; got != want {
		t.Fatalf("LengthSquared = %v, want %v", got, want)
	}
	if got, want := v.Length(), 5.0; got != want {
		t.Fatalf("Length = %v, want %v", got, want)
	}
	if got, want := v.Distance(Vec2d{3, 1}), 3.0; got != want {
		t.Fatalf("Distance = %v, want %v", got, want)
	}
	if v != (Vec2d{3, 4}) {
		t.Fatalf("a method mutated its receiver: %v", v)
	}
}

func TestVec2dLerpReachesItsEndpointsExactly(t *testing.T) {
	from := Vec2d{1, 2}
	to := Vec2d{0.1, 0.3}

	if got := from.Lerp(to, 0); got != from {
		t.Fatalf("Lerp at 0 = %v, want %v", got, from)
	}
	if got := from.Lerp(to, 1); got != to {
		t.Fatalf("Lerp at 1 = %v, want %v exactly", got, to)
	}
	if got, want := from.Lerp(to, 0.5), (Vec2d{0.55, 1.15}); !vec2dNear(got, want) {
		t.Fatalf("Lerp at 0.5 = %v, want %v", got, want)
	}
}

func TestVec2dAddsCpsMethodsWithNoVec2Counterpart(t *testing.T) {
	if got, want := (Vec2d{1, 0}).Perp(), (Vec2d{0, 1}); got != want {
		t.Fatalf("Perp = %v, want %v", got, want)
	}
	if got, want := (Vec2d{1, 0}).ReversePerp(), (Vec2d{0, -1}); got != want {
		t.Fatalf("ReversePerp = %v, want %v", got, want)
	}
	if got, want := (Vec2d{1, 1}).Project(Vec2d{2, 0}), (Vec2d{1, 0}); !vec2dNear(got, want) {
		t.Fatalf("Project = %v, want %v", got, want)
	}
	if got, want := ForAngle(0), (Vec2d{1, 0}); !vec2dNear(got, want) {
		t.Fatalf("ForAngle(0) = %v, want %v", got, want)
	}
	if got, want := ForAngle(math.Pi/2), (Vec2d{0, 1}); !vec2dNear(got, want) {
		t.Fatalf("ForAngle(pi/2) = %v, want %v", got, want)
	}
}

// ForAngle takes its cosine and sine from one math.Sincos, which is cheaper
// than a math.Cos and a math.Sin and, in pure Go on every target cog ships,
// the same range reduction and kernels. This pins the result to the separate
// calls bit for bit, so a toolchain whose Sincos drifts from them fails here
// rather than moving every rotation in the physics by an ulp.
func TestVec2dForAngleIsBitEqualToASeparateCosAndSin(t *testing.T) {
	angles := []float64{
		0, math.Copysign(0, -1),
		math.Pi / 2, -math.Pi / 2, math.Pi, -math.Pi, 2 * math.Pi, -2 * math.Pi,
		math.Pi / 4, 3 * math.Pi / 4, 0.7, -0.9, 1, -1,
		1e-6, -1e-6, 1e-300, -1e-300, math.SmallestNonzeroFloat64,
		1e4, -1e4, 1e9, -1e9, 1<<29 - 0.5, 1e15, -1e15, 1e300, -1e300, math.MaxFloat64, -math.MaxFloat64,
		math.NaN(), math.Inf(1), math.Inf(-1),
	}
	// A spread over the ranges the probe on issue 569 drew from, stepped by an
	// irrational fraction so the angles do not land on a lattice.
	for _, magnitude := range []float64{2 * math.Pi, 1e4, 1e-6, 1e9} {
		for i := range 1000 {
			angles = append(angles, magnitude*(2*math.Mod(float64(i)*math.Phi, 1)-1))
		}
	}

	same := func(got, want float64) bool {
		if math.IsNaN(want) {
			return math.IsNaN(got)
		}
		return math.Float64bits(got) == math.Float64bits(want)
	}
	for _, angle := range angles {
		got := ForAngle(angle)
		if wantX, wantY := math.Cos(angle), math.Sin(angle); !same(got.X, wantX) || !same(got.Y, wantY) {
			t.Fatalf("ForAngle(%v) = %v, want {%v %v} bit for bit", angle, got, wantX, wantY)
		}
	}
}

func TestVec2dRotatesByAVectorAndUnrotatesBack(t *testing.T) {
	quarterTurn := ForAngle(math.Pi / 2)

	rotated := (Vec2d{1, 0}).Rotate(quarterTurn)
	if want := (Vec2d{0, 1}); !vec2dNear(rotated, want) {
		t.Fatalf("Rotate a quarter turn = %v, want %v", rotated, want)
	}
	if got, want := rotated.Unrotate(quarterTurn), (Vec2d{1, 0}); !vec2dNear(got, want) {
		t.Fatalf("Unrotate = %v, want %v", got, want)
	}

	original := Vec2d{3, -7}
	rotation := ForAngle(0.9)
	if got := original.Rotate(rotation).Unrotate(rotation); !vec2dNear(got, original) {
		t.Fatalf("rotate then unrotate = %v, want %v", got, original)
	}
}

func TestVec2dNormalizeKeepsChipmunksGuardRatherThanThePortsWeakenedOne(t *testing.T) {
	if got, want := (Vec2d{3, 4}).Normalize(), (Vec2d{0.6, 0.8}); !vec2dNear(got, want) {
		t.Fatalf("Normalize = %v, want %v", got, want)
	}

	zero := (Vec2d{}).Normalize()
	if zero != (Vec2d{}) {
		t.Fatalf("Normalize of the zero vector = %v, want the zero vector", zero)
	}
	if math.IsNaN(zero.X) || math.IsNaN(zero.Y) {
		t.Fatalf("Normalize of the zero vector produced a NaN: %v", zero)
	}

	// jakecoffman/cp weakened Chipmunk's CPFLOAT_MIN to 1e-15, which swamps a
	// genuinely small vector: this one would normalize to about (0.09, 0).
	tiny := (Vec2d{1e-16, 0}).Normalize()
	if want := (Vec2d{1, 0}); !vec2dNear(tiny, want) {
		t.Fatalf("Normalize of a tiny vector = %v, want %v", tiny, want)
	}
}

func TestVec2dClosestTReturnsANumberForADegenerateSimplex(t *testing.T) {
	// The defect: cp divides by the squared length of b − a with no guard, so a
	// simplex whose two points coincide returns a NaN.
	point := Vec2d{2, 3}
	if got := point.ClosestT(point); math.IsNaN(got) {
		t.Fatalf("ClosestT of a degenerate simplex = %v, want a number", got)
	}
	if got := (Vec2d{}).ClosestT(Vec2d{}); math.IsNaN(got) {
		t.Fatalf("ClosestT at the origin = %v, want a number", got)
	}

	if got, want := (Vec2d{-1, 1}).ClosestT(Vec2d{1, 1}), 0.0; !nearD(got, want) {
		t.Fatalf("ClosestT with the midpoint closest = %v, want %v", got, want)
	}
	if got, want := (Vec2d{1, 1}).ClosestT(Vec2d{3, 1}), -1.0; !nearD(got, want) {
		t.Fatalf("ClosestT with the first point closest = %v, want %v", got, want)
	}
	if got, want := (Vec2d{-3, 1}).ClosestT(Vec2d{-1, 1}), 1.0; !nearD(got, want) {
		t.Fatalf("ClosestT with the second point closest = %v, want %v", got, want)
	}
}

func TestVec2dConvertsToAndFromVec2(t *testing.T) {
	if got, want := (Vec2d{1.5, -2.5}).Vec2(), (Vec2{1.5, -2.5}); got != want {
		t.Fatalf("Vec2d.Vec2 = %v, want %v", got, want)
	}
	if got, want := (Vec2{1.5, -2.5}).Vec2d(), (Vec2d{1.5, -2.5}); got != want {
		t.Fatalf("Vec2.Vec2d = %v, want %v", got, want)
	}
	if got, want := (Vec2{3, 4}).Vec2d().Vec2(), (Vec2{3, 4}); got != want {
		t.Fatalf("round trip through Vec2d = %v, want %v", got, want)
	}
}

func TestVec2dAllocatesNothing(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	v := Vec2d{3, 4}
	other := Vec2d{1, 2}

	for name, call := range map[string]func(){
		"NewVec2d":      func() { vec2dSink = NewVec2d(1, 2) },
		"ForAngle":      func() { vec2dSink = ForAngle(0.7) },
		"Add":           func() { vec2dSink = v.Add(other) },
		"Sub":           func() { vec2dSink = v.Sub(other) },
		"MulS":          func() { vec2dSink = v.MulS(2) },
		"Negate":        func() { vec2dSink = v.Negate() },
		"Perp":          func() { vec2dSink = v.Perp() },
		"ReversePerp":   func() { vec2dSink = v.ReversePerp() },
		"Project":       func() { vec2dSink = v.Project(other) },
		"Rotate":        func() { vec2dSink = v.Rotate(other) },
		"Unrotate":      func() { vec2dSink = v.Unrotate(other) },
		"Normalize":     func() { vec2dSink = v.Normalize() },
		"Lerp":          func() { vec2dSink = v.Lerp(other, 0.25) },
		"Vec2":          func() { vec2Sink = v.Vec2() },
		"Vec2d":         func() { vec2dSink = vec2Sink.Vec2d() },
		"Dot":           func() { floatSink = v.Dot(other) },
		"Cross":         func() { floatSink = v.Cross(other) },
		"LengthSquared": func() { floatSink = v.LengthSquared() },
		"Length":        func() { floatSink = v.Length() },
		"Distance":      func() { floatSink = v.Distance(other) },
		"ClosestT":      func() { floatSink = v.ClosestT(other) },
	} {
		if allocations := testing.AllocsPerRun(100, call); allocations != 0 {
			t.Fatalf("%s allocated %v times a call, want 0", name, allocations)
		}
	}
}

var (
	vec2dSink Vec2d
	vec2Sink  Vec2
	floatSink float64
)

func nearD(got, want float64) bool   { return math.Abs(got-want) <= tolerance2d }
func vec2dNear(got, want Vec2d) bool { return nearD(got.X, want.X) && nearD(got.Y, want.Y) }
