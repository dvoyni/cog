package m

import (
	"math"
	"testing"
)

const tolerance = 0.0001

func TestVectorConstructorsAndScalarOperations(t *testing.T) {
	if got, want := NewVec3(), (Vec3{}); got != want {
		t.Fatalf("NewVec3() = %v, want %v", got, want)
	}
	if got, want := NewVec3(2), (Vec3{2, 2, 2}); got != want {
		t.Fatalf("NewVec3(2) = %v, want %v", got, want)
	}
	if got, want := NewVec3(1, 2, 3), (Vec3{1, 2, 3}); got != want {
		t.Fatalf("NewVec3 exact = %v, want %v", got, want)
	}

	original := Vec3{1, 2, 3}
	if got, want := original.MulS(2, 3, 4), (Vec3{2, 6, 12}); got != want {
		t.Fatalf("MulS = %v, want %v", got, want)
	}
	if original != (Vec3{1, 2, 3}) {
		t.Fatalf("MulS mutated receiver: %v", original)
	}
	if got, want := (Vec3{1.6, -1.5, 1.2}).Round(), (Vec3{2, -2, 1}); got != want {
		t.Fatalf("Round = %v, want %v", got, want)
	}
	if got, want := (Vec3{1.9, -1.9, 0}).Int(), (Vec3i{1, -1, 0}); got != want {
		t.Fatalf("Int = %v, want truncation %v", got, want)
	}
}

func TestVectorInvalidArityPanics(t *testing.T) {
	assertPanics(t, func() { NewVec4(1, 2) })
	assertPanics(t, func() { Vec2{}.AddS() })
	assertPanics(t, func() { Vec2i{}.MulS(1, 2, 3) })
}

func TestClamp(t *testing.T) {
	if got := Clamp(-2, -1, 1); got != -1 {
		t.Fatalf("Clamp below range = %v, want -1", got)
	}
	if got := Clamp(2, -1, 1); got != 1 {
		t.Fatalf("Clamp above range = %v, want 1", got)
	}
	if got := Clamp01(0.25); got != 0.25 {
		t.Fatalf("Clamp01 inside range = %v, want 0.25", got)
	}
	if got := Clamp01(2); got != 1 {
		t.Fatalf("Clamp01 above range = %v, want 1", got)
	}
}

func TestRectQueriesNormalizeNegativeDimensions(t *testing.T) {
	rect := Rect{X: 10, Y: 20, Width: -8, Height: -12}
	if got, want := rect.Normalize(), (Rect{X: 2, Y: 8, Width: 8, Height: 12}); got != want {
		t.Fatalf("Normalize = %v, want %v", got, want)
	}
	if !rect.Contains(Vec2{5, 10}) {
		t.Fatal("normalized rect should contain point")
	}
	intersection, ok := rect.Intersection(Rect{X: 0, Y: 0, Width: 4, Height: 10})
	if !ok || intersection != (Rect{X: 2, Y: 8, Width: 2, Height: 2}) {
		t.Fatalf("Intersection = %v, %v", intersection, ok)
	}
}

func TestMatrixConstructorsAndInverse(t *testing.T) {
	if got, want := NewMat2(), (Mat2{1, 0, 0, 1}); got != want {
		t.Fatalf("NewMat2() = %v, want identity %v", got, want)
	}
	if got, want := NewMat2(0), (Mat2{}); got != want {
		t.Fatalf("NewMat2(0) = %v, want zero %v", got, want)
	}
	if got, want := NewMat2(1, 2, 3, 4), (Mat2{1, 2, 3, 4}); got != want {
		t.Fatalf("column-major constructor = %v, want %v", got, want)
	}

	matrix := Translation4(2, 3, 4).Mul(RotationY4(0.3)).Mul(Scaling4(2, 4, 8))
	inverse, ok := matrix.Inverse()
	if !ok {
		t.Fatal("matrix should be invertible")
	}
	assertMat4Near(t, matrix.Mul(inverse), NewMat4())
	if _, ok := NewMat4(0).Inverse(); ok {
		t.Fatal("zero matrix should be singular")
	}
}

func TestAnglesUseRadiansAndShortestPath(t *testing.T) {
	if got := (Vec2{X: 1}).Rotate(math.Pi / 2); !vec2Near(got, Vec2{Y: 1}) {
		t.Fatalf("quarter turn = %v, want (0, 1)", got)
	}
	from := float32(170 * DegToRad)
	to := float32(-170 * DegToRad)
	if got, want := LerpAngle(from, to, 0.5), float32(math.Pi); !near(got, want) {
		t.Fatalf("LerpAngle = %v, want %v", got, want)
	}
}

func TestSplines(t *testing.T) {
	start := Vec2{1, 2}
	end := Vec2{5, 6}
	if got := BezierQuadratic(start, Vec2{3, 8}, end, 0); got != start {
		t.Fatalf("Bezier start = %v", got)
	}
	if got := BezierQuadratic(start, Vec2{3, 8}, end, 1); got != end {
		t.Fatalf("Bezier end = %v", got)
	}
	if got := CatmullRom(Vec2{}, start, end, Vec2{8, 9}, 0); got != start {
		t.Fatalf("CatmullRom start = %v", got)
	}
	if got := CatmullRom(Vec2{}, start, end, Vec2{8, 9}, 1); got != end {
		t.Fatalf("CatmullRom end = %v", got)
	}
}

func TestColorInterpolationExtrapolates(t *testing.T) {
	color := Color{R: 0.25, A: 0.5}
	if got, want := color.Lerp(Color{R: 0.75, A: 1}, 2), (Color{R: 1.25, A: 1.5}); got != want {
		t.Fatalf("Color.Lerp = %v, want %v", got, want)
	}
	if got, want := color.Opacity(0.5).A, float32(0.25); got != want {
		t.Fatalf("Opacity alpha = %v, want %v", got, want)
	}
}

func TestColorHSLA(t *testing.T) {
	if got, want := NewColorHSLA(0, 1, 0.5, 0.75), (Color{R: 1, A: 0.75}); !colorNear(got, want) {
		t.Fatalf("red HSLA = %v, want %v", got, want)
	}
	if got, want := NewColorHSLA(2*math.Pi/3, 1, 0.5, 1), (Color{G: 1, A: 1}); !colorNear(got, want) {
		t.Fatalf("green HSLA = %v, want %v", got, want)
	}
	if got, want := NewColorHSLA(-2*math.Pi/3, 1, 0.5, 1), (Color{B: 1, A: 1}); !colorNear(got, want) {
		t.Fatalf("wrapped HSLA = %v, want %v", got, want)
	}

	wantHue, wantSaturation, wantLightness, wantAlpha := float32(1.7), float32(0.65), float32(0.35), float32(0.4)
	hue, saturation, lightness, alpha := NewColorHSLA(wantHue, wantSaturation, wantLightness, wantAlpha).Hsla()
	if !near(hue, wantHue) || !near(saturation, wantSaturation) || !near(lightness, wantLightness) || !near(alpha, wantAlpha) {
		t.Fatalf("HSLA round trip = (%v, %v, %v, %v), want (%v, %v, %v, %v)", hue, saturation, lightness, alpha, wantHue, wantSaturation, wantLightness, wantAlpha)
	}

	hue, saturation, lightness, alpha = NewColorSrgb(0.25, 0.25, 0.25, 0.5).Hsla()
	if hue != 0 || saturation != 0 || !near(lightness, 0.25) || alpha != 0.5 {
		t.Fatalf("achromatic HSLA = (%v, %v, %v, %v)", hue, saturation, lightness, alpha)
	}
}

func TestQuaternionRotationAndMatrixRoundTrip(t *testing.T) {
	if got, want := NewQuat(), (Quat{W: 1}); got != want {
		t.Fatalf("NewQuat() = %v, want identity %v", got, want)
	}
	rotation := QuatAxisAngle(Vec3{Z: 1}, math.Pi/2)
	if got := rotation.Rotate(Vec3{X: 1}); !vec3Near(got, Vec3{Y: 1}) {
		t.Fatalf("rotated vector = %v, want (0, 1, 0)", got)
	}
	roundTrip := QuatFromMat3(rotation.Mat3())
	if math.Abs(float64(rotation.Dot(roundTrip))) < 1-tolerance {
		t.Fatalf("quaternion round trip = %v, want rotation equivalent to %v", roundTrip, rotation)
	}
	if got := NewQuat().SLerp(rotation, 0.5).Rotate(Vec3{X: 1}); !vec3Near(got, Vec3{X: sqrt(0.5), Y: sqrt(0.5)}) {
		t.Fatalf("half slerp rotation = %v", got)
	}
}

func assertPanics(t *testing.T, function func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	function()
}

func assertMat4Near(t *testing.T, got, want Mat4) {
	t.Helper()
	for index := range got {
		if !near(got[index], want[index]) {
			t.Fatalf("matrix[%d] = %v, want %v; matrix = %v", index, got[index], want[index], got)
		}
	}
}

func near(got, want float32) bool  { return math.Abs(float64(got-want)) <= tolerance }
func vec2Near(got, want Vec2) bool { return near(got.X, want.X) && near(got.Y, want.Y) }
func vec3Near(got, want Vec3) bool {
	return near(got.X, want.X) && near(got.Y, want.Y) && near(got.Z, want.Z)
}
func colorNear(got, want Color) bool {
	return near(got.R, want.R) && near(got.G, want.G) && near(got.B, want.B) && near(got.A, want.A)
}
