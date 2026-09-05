package scene

import (
	"testing"

	"github.com/dvoyni/cog/m"
)

func closeEnough(a, b m.Vec3) bool {
	const epsilon = 1e-4
	d := a.Sub(b)
	return d.X < epsilon && d.X > -epsilon && d.Y < epsilon && d.Y > -epsilon && d.Z < epsilon && d.Z > -epsilon
}

func TestTheZeroTransformIsTheIdentity(t *testing.T) {
	// The zero Quat is (0,0,0,0), not (0,0,0,1), so an unwritten Rotation would
	// collapse the basis to nothing if it were taken at face value.
	point := m.Vec3{X: 1, Y: 2, Z: 3}
	if got := (Transform{}).Mat4().TransformPoint(point); !closeEnough(got, point) {
		t.Errorf("identity moved the point to %v, want %v", got, point)
	}
}

func TestScaleIsScalarAndZeroMeansOne(t *testing.T) {
	if got := At(1, 0, 0).Mat4().TransformPoint(m.Vec3{X: 2}); !closeEnough(got, m.Vec3{X: 3}) {
		t.Errorf("unscaled translate gave %v, want (3,0,0)", got)
	}
	if got := At(1, 0, 0).WithScale(2).Mat4().TransformPoint(m.Vec3{X: 2}); !closeEnough(got, m.Vec3{X: 5}) {
		t.Errorf("scaled translate gave %v, want (5,0,0)", got)
	}
}

func TestAMatrixReplacesTheWholeTransform(t *testing.T) {
	// Non-uniform scale is inexpressible through the scalar field, so it comes
	// in whole and nothing else on the Transform is consulted.
	matrix := m.Scaling4(2, 3, 4)
	transform := At(100, 100, 100).WithScale(9)
	transform.Matrix = &matrix
	if got := transform.Mat4().TransformPoint(m.Vec3{X: 1, Y: 1, Z: 1}); !closeEnough(got, m.Vec3{X: 2, Y: 3, Z: 4}) {
		t.Errorf("matrix transform gave %v, want (2,3,4)", got)
	}
}

func TestLookAtIsAPositionedObject(t *testing.T) {
	// LookAt returns where the camera *is*, not a view matrix: it has to compose
	// with a follow rig and be decomposable for culling.
	eye := m.Vec3{X: 3, Y: 2, Z: 4}
	transform := LookAt(eye, m.Vec3{}, m.Vec3{Y: 1})
	if !closeEnough(transform.Position, eye) {
		t.Errorf("position = %v, want the eye %v", transform.Position, eye)
	}
	// Its -Z axis points at the target, the convention m.LookAt4 builds against.
	forward := transform.Mat4().TransformDirection(m.Vec3{Z: -1})
	if !closeEnough(forward, eye.Negate().Normalize()) {
		t.Errorf("forward = %v, want %v", forward, eye.Negate().Normalize())
	}
}

func TestTheCameraViewIgnoresScale(t *testing.T) {
	// Scaling a view matrix scales the whole world instead, so the field that
	// cannot be avoided at the call site is simply not read.
	plain, ok := cameraView(LookAt(m.Vec3{Z: 5}, m.Vec3{}, m.Vec3{Y: 1}))
	if !ok {
		t.Fatal("view of a look-at transform was not invertible")
	}
	scaled, ok := cameraView(LookAt(m.Vec3{Z: 5}, m.Vec3{}, m.Vec3{Y: 1}).WithScale(10))
	if !ok {
		t.Fatal("view of a scaled look-at transform was not invertible")
	}
	if plain != scaled {
		t.Errorf("scale changed the view matrix:\n%v\n%v", plain, scaled)
	}
}

func TestTheViewMatrixIsTheInverseOfWhereTheCameraIs(t *testing.T) {
	eye := m.Vec3{X: 3, Y: 2, Z: 4}
	view, ok := cameraView(LookAt(eye, m.Vec3{}, m.Vec3{Y: 1}))
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
