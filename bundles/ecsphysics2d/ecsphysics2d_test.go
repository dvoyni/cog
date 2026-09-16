package ecsphysics2d

import (
	"errors"
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// The root is aliases and forwarders, so what these check is the wiring: that
// each forwarder passes its arguments through in the order the caller wrote
// them, which the compiler cannot see when the types all match.

func TestTheTransformForwardersPassTheirArgumentsThrough(t *testing.T) {
	if got, want := NewTransform(1, 2, 3, 4, 5, 6), (Transform{A: 1, C: 2, TX: 3, B: 4, D: 5, TY: 6}); got != want {
		t.Fatalf("NewTransform = %v, want %v", got, want)
	}
	if got := NewTransformIdentity().Point(m.Vec2d{X: 3, Y: 4}); got != (m.Vec2d{X: 3, Y: 4}) {
		t.Fatalf("the identity moved a point: %v", got)
	}
	if got, want := NewTransformTranslate(m.Vec2d{X: 1, Y: -1}).Point(m.Vec2d{}), (m.Vec2d{X: 1, Y: -1}); got != want {
		t.Fatalf("NewTransformTranslate = %v, want %v", got, want)
	}
	if got, want := NewTransformScale(2, 3).Point(m.Vec2d{X: 1, Y: 1}), (m.Vec2d{X: 2, Y: 3}); got != want {
		t.Fatalf("NewTransformScale = %v, want %v", got, want)
	}

	if got, want := NewTransformRotate(math.Pi/2).Point(m.Vec2d{X: 1}), (m.Vec2d{Y: 1}); !vecNear(got, want) {
		t.Fatalf("NewTransformRotate = %v, want %v", got, want)
	}

	rigid := NewTransformRigid(m.Vec2d{X: 3, Y: 4}, math.Pi/2)
	if got, want := rigid.Point(m.Vec2d{X: 1}), (m.Vec2d{X: 3, Y: 5}); !vecNear(got, want) {
		t.Fatalf("NewTransformRigid = %v, want %v", got, want)
	}
	point := m.Vec2d{X: 7, Y: -2}
	if got := NewTransformRigidInverse(rigid).Point(rigid.Point(point)); !vecNear(got, point) {
		t.Fatalf("NewTransformRigidInverse of NewTransformRigid = %v, want %v", got, point)
	}
}

func TestTheBBForwardersPassTheirArgumentsThrough(t *testing.T) {
	if got, want := NewBB(-1, -2, 3, 4), (BB{L: -1, B: -2, R: 3, T: 4}); got != want {
		t.Fatalf("NewBB = %v, want %v", got, want)
	}
	if got, want := NewBBForExtents(m.Vec2d{X: 1, Y: 1}, 2, 3), NewBB(-1, -2, 3, 4); got != want {
		t.Fatalf("NewBBForExtents = %v, want %v", got, want)
	}
	if got, want := NewBBForCircle(m.Vec2d{X: 1, Y: 1}, 2), NewBB(-1, -1, 3, 3); got != want {
		t.Fatalf("NewBBForCircle = %v, want %v", got, want)
	}
}

func TestTheMassForwardersPassTheirArgumentsThrough(t *testing.T) {
	square := []m.Vec2d{{X: -1, Y: -1}, {X: 1, Y: -1}, {X: 1, Y: 1}, {X: -1, Y: 1}}
	a, b := m.Vec2d{X: -1}, m.Vec2d{X: 1}

	if got, want := MomentForCircle(2, 0, 3, m.Vec2d{X: 4}), 41.0; !nearD(got, want) {
		t.Fatalf("MomentForCircle = %v, want %v", got, want)
	}
	if got, want := MomentForSegment(3, a, b, 0), 1.0; !nearD(got, want) {
		t.Fatalf("MomentForSegment = %v, want %v", got, want)
	}
	if got, want := MomentForBox(3, 2, 2), 2.0; !nearD(got, want) {
		t.Fatalf("MomentForBox = %v, want %v", got, want)
	}
	if got, want := MomentForPoly(3, square, m.Vec2d{}, 0), 2.0; !nearD(got, want) {
		t.Fatalf("MomentForPoly = %v, want %v", got, want)
	}
	if got, want := AreaForCircle(1, 2), 3*math.Pi; !nearD(got, want) {
		t.Fatalf("AreaForCircle = %v, want %v", got, want)
	}
	if got, want := AreaForSegment(a, b, 0.5), 0.25*math.Pi+2.0; !nearD(got, want) {
		t.Fatalf("AreaForSegment = %v, want %v", got, want)
	}
	if got, want := AreaForPoly(square, 0), 4.0; !nearD(got, want) {
		t.Fatalf("AreaForPoly = %v, want %v", got, want)
	}

	centroid, ok := CentroidForPoly(square)
	if !ok || !vecNear(centroid, m.Vec2d{}) {
		t.Fatalf("CentroidForPoly = %v, %v, want the origin", centroid, ok)
	}
	if _, ok := CentroidForPoly(nil); ok {
		t.Fatal("CentroidForPoly accepted an empty outline")
	}
}

func TestTheDynamicForwarderPassesItsArgumentsThrough(t *testing.T) {
	body, err := NewDynamic(2, 8, 15, 0.3)
	if err != nil {
		t.Fatalf("NewDynamic(2, 8, 15, 0.3): %v", err)
	}
	if got, want := body.Mass(), 2.0; got != want {
		t.Errorf("Mass = %v, want %v", got, want)
	}
	if got, want := body.Moment(), 8.0; got != want {
		t.Errorf("Moment = %v, want %v", got, want)
	}
	if got, want := body.Damping(), 15.0; got != want {
		t.Errorf("Damping = %v, want %v", got, want)
	}
	if got, want := body.AngularDamping(), 0.3; got != want {
		t.Errorf("AngularDamping = %v, want %v", got, want)
	}

	var refusal ErrBadMass
	if _, err := NewDynamic(0, 8, 0, 0); !errors.As(err, &refusal) {
		t.Errorf("NewDynamic with a mass of 0 returned %v, want an ErrBadMass", err)
	}
}

func nearD(got, want float64) bool   { return math.Abs(got-want) <= 1e-12 }
func vecNear(got, want m.Vec2d) bool { return nearD(got.X, want.X) && nearD(got.Y, want.Y) }
