package types

import (
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

func closeEnough(a, b m.Vec3) bool {
	const epsilon = 1e-4
	d := a.Sub(b)
	return d.X < epsilon && d.X > -epsilon && d.Y < epsilon && d.Y > -epsilon && d.Z < epsilon && d.Z > -epsilon
}

func TestTheCameraViewIgnoresScale(t *testing.T) {
	// Scaling a view matrix scales the whole world instead, so the field that
	// cannot be avoided at the call site is simply not read.
	plain, ok := CameraView(m.LookAt(m.Vec3{Z: 5}, m.Vec3{}, m.Vec3{Y: 1}))
	if !ok {
		t.Fatal("view of a look-at transform was not invertible")
	}
	scaled, ok := CameraView(m.LookAt(m.Vec3{Z: 5}, m.Vec3{}, m.Vec3{Y: 1}).WithScale(10))
	if !ok {
		t.Fatal("view of a scaled look-at transform was not invertible")
	}
	if plain != scaled {
		t.Errorf("scale changed the view matrix:\n%v\n%v", plain, scaled)
	}
}

func TestTheViewMatrixIsTheInverseOfWhereTheCameraIs(t *testing.T) {
	eye := m.Vec3{X: 3, Y: 2, Z: 4}
	view, ok := CameraView(m.LookAt(eye, m.Vec3{}, m.Vec3{Y: 1}))
	if !ok {
		t.Fatal("view was not invertible")
	}
	if got := view.TransformPoint(eye); !closeEnough(got, m.Vec3{}) {
		t.Errorf("the eye maps to %v in view space, want the origin", got)
	}
	// The target sits down -Z at its distance from the eye.
	if got := view.TransformPoint(m.Vec3{}); !closeEnough(got, m.Vec3{Z: -eye.Length()}) {
		t.Errorf("the target maps to %v, want (0,0,%v)", got, -eye.Length())
	}
}
