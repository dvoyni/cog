package types

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

const tolerance = 1e-12

func TestTransformTakesItsSixInReadingOrder(t *testing.T) {
	got := NewTransform(
		1, 2, 3,
		4, 5, 6,
	)
	want := Transform{A: 1, C: 2, TX: 3, B: 4, D: 5, TY: 6}
	if got != want {
		t.Fatalf("NewTransform = %v, want %v", got, want)
	}
	if identity := NewTransformIdentity(); identity.Point(m.Vec2d{X: 3, Y: 4}) != (m.Vec2d{X: 3, Y: 4}) {
		t.Fatalf("the identity moved a point: %v", identity)
	}
}

func TestTransformPointCarriesTheTranslationAndVecDoesNot(t *testing.T) {
	transform := NewTransformRigid(m.Vec2d{X: 3, Y: 4}, math.Pi/2)

	if got, want := transform.Point(m.Vec2d{X: 1}), (m.Vec2d{X: 3, Y: 5}); !vecNear(got, want) {
		t.Fatalf("Point = %v, want %v", got, want)
	}
	if got, want := transform.Vec(m.Vec2d{X: 1}), (m.Vec2d{Y: 1}); !vecNear(got, want) {
		t.Fatalf("Vec = %v, want %v", got, want)
	}
}

func TestTransformTranslateScaleAndRotateDoOneThingEach(t *testing.T) {
	point := m.Vec2d{X: 2, Y: 1}

	if got, want := NewTransformTranslate(m.Vec2d{X: 1, Y: -1}).Point(point), (m.Vec2d{X: 3, Y: 0}); !vecNear(got, want) {
		t.Fatalf("translate = %v, want %v", got, want)
	}
	if got, want := NewTransformScale(2, 3).Point(point), (m.Vec2d{X: 4, Y: 3}); !vecNear(got, want) {
		t.Fatalf("scale = %v, want %v", got, want)
	}
	if got, want := NewTransformRotate(math.Pi).Point(point), (m.Vec2d{X: -2, Y: -1}); !vecNear(got, want) {
		t.Fatalf("rotate = %v, want %v", got, want)
	}
}

func TestTransformInverseAndRigidInverseUndoTheTransform(t *testing.T) {
	point := m.Vec2d{X: 7, Y: -2}

	rigid := NewTransformRigid(m.Vec2d{X: 3, Y: 4}, 0.9)
	if got := NewTransformRigidInverse(rigid).Point(rigid.Point(point)); !vecNear(got, point) {
		t.Fatalf("rigid inverse of rigid = %v, want %v", got, point)
	}
	if got := rigid.Inverse().Point(rigid.Point(point)); !vecNear(got, point) {
		t.Fatalf("inverse of rigid = %v, want %v", got, point)
	}

	scaled := NewTransformScale(2, 4).Mul(NewTransformRotate(0.3))
	if got := scaled.Inverse().Point(scaled.Point(point)); !vecNear(got, point) {
		t.Fatalf("inverse of a scaled rotation = %v, want %v", got, point)
	}
}

func TestTransformMulAppliesItsArgumentFirst(t *testing.T) {
	point := m.Vec2d{X: 1, Y: 2}
	outer := NewTransformRigid(m.Vec2d{X: 5, Y: 0}, 0.4)
	inner := NewTransformScale(3, 2)

	if got, want := outer.Mul(inner).Point(point), outer.Point(inner.Point(point)); !vecNear(got, want) {
		t.Fatalf("Mul = %v, want %v", got, want)
	}
}

func TestTransformBBIsTheBoxAroundTheTurnedBox(t *testing.T) {
	box := NewBB(-1, -1, 1, 1)

	if got := NewTransformIdentity().BB(box); got != box {
		t.Fatalf("the identity changed a box: %v, want %v", got, box)
	}

	turned := NewTransformRotate(math.Pi / 4).BB(box)
	half := math.Sqrt2
	want := NewBB(-half, -half, half, half)
	if !nearF(turned.L, want.L) || !nearF(turned.B, want.B) || !nearF(turned.R, want.R) || !nearF(turned.T, want.T) {
		t.Fatalf("a box turned an eighth of a turn = %v, want %v", turned, want)
	}

	moved := NewTransformTranslate(m.Vec2d{X: 10}).BB(box)
	if got, want := moved, NewBB(9, -1, 11, 1); got != want {
		t.Fatalf("a moved box = %v, want %v", got, want)
	}
}

func TestTransformAllocatesNothing(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	transform := NewTransformRigid(m.Vec2d{X: 1, Y: 2}, 0.5)
	other := NewTransformScale(2, 3)
	point := m.Vec2d{X: 3, Y: 4}
	box := NewBB(-1, -1, 1, 1)

	for name, call := range map[string]func(){
		"NewTransform":             func() { transformSink = NewTransform(1, 0, 0, 0, 1, 0) },
		"NewTransformIdentity":     func() { transformSink = NewTransformIdentity() },
		"NewTransformTranslate":    func() { transformSink = NewTransformTranslate(point) },
		"NewTransformScale":        func() { transformSink = NewTransformScale(2, 3) },
		"NewTransformRotate":       func() { transformSink = NewTransformRotate(0.5) },
		"NewTransformRigid":        func() { transformSink = NewTransformRigid(point, 0.5) },
		"NewTransformRigidInverse": func() { transformSink = NewTransformRigidInverse(transform) },
		"Inverse":                  func() { transformSink = transform.Inverse() },
		"Mul":                      func() { transformSink = transform.Mul(other) },
		"Point":                    func() { vecSink = transform.Point(point) },
		"Vec":                      func() { vecSink = transform.Vec(point) },
		"BB":                       func() { bbSink = transform.BB(box) },
	} {
		if allocations := testing.AllocsPerRun(100, call); allocations != 0 {
			t.Fatalf("%s allocated %v times a call, want 0", name, allocations)
		}
	}
}

var (
	transformSink Transform
	bbSink        BB
	vecSink       m.Vec2d
	floatSink     float64
	boolSink      bool
)

func nearF(got, want float64) bool   { return math.Abs(got-want) <= tolerance }
func vecNear(got, want m.Vec2d) bool { return nearF(got.X, want.X) && nearF(got.Y, want.Y) }
