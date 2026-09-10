package m

import (
	"math"
	"testing"
)

func TestOrthographic4MatchesPerspectiveClipDepth(t *testing.T) {
	projection := Orthographic4(-2, 6, -3, 5, 1, 11)

	nearCenter, ok := Project(projection, Vec3{X: 2, Y: 1, Z: -1})
	if !ok {
		t.Fatal("near center should project")
	}
	if !vec3Near(nearCenter, Vec3{}) {
		t.Fatalf("near center = %v, want the origin of clip space", nearCenter)
	}

	farCorner, ok := Project(projection, Vec3{X: 6, Y: 5, Z: -11})
	if !ok {
		t.Fatal("far corner should project")
	}
	if !vec3Near(farCorner, Vec3{X: 1, Y: 1, Z: 1}) {
		t.Fatalf("far corner = %v, want (1, 1, 1)", farCorner)
	}

	nearCorner, ok := Project(projection, Vec3{X: -2, Y: -3, Z: -1})
	if !ok {
		t.Fatal("near corner should project")
	}
	if !vec3Near(nearCorner, Vec3{X: -1, Y: -1, Z: 0}) {
		t.Fatalf("near corner = %v, want (-1, -1, 0)", nearCorner)
	}
}

func TestOrthographic4KeepsWConstant(t *testing.T) {
	projection := Orthographic4(-1, 1, -1, 1, 0.5, 20)
	if got := projection.MulVec4(Vec4{X: 3, Y: -4, Z: -7, W: 1}).W; got != 1 {
		t.Fatalf("orthographic w = %v, want 1 for every point", got)
	}
}

func TestTRS4ComposesTranslationRotationScale(t *testing.T) {
	translation := Vec3{2, 3, 4}
	rotation := QuatAxisAngle(Vec3{Y: 1}, 0.7)
	scale := Vec3{2, 4, 8}

	want := Translation4(translation.X, translation.Y, translation.Z).
		Mul(rotation.Mat4()).
		Mul(Scaling4(scale.X, scale.Y, scale.Z))
	assertMat4Near(t, TRS4(translation, rotation, scale), want)
}

func TestDecomposeRoundTripsNonUniformScale(t *testing.T) {
	translation := Vec3{-5, 0.5, 12}
	rotation := QuatAxisAngle(Vec3{X: 1, Y: 2, Z: -3}.Normalize(), 1.1)
	scale := Vec3{3, 0.25, 7}

	matrix := TRS4(translation, rotation, scale)
	gotTranslation, gotRotation, gotScale, ok := matrix.Decompose()
	if !ok {
		t.Fatal("a scaled TRS matrix should decompose")
	}
	if !vec3Near(gotTranslation, translation) {
		t.Fatalf("translation = %v, want %v", gotTranslation, translation)
	}
	if !vec3Near(gotScale, scale) {
		t.Fatalf("scale = %v, want %v", gotScale, scale)
	}
	if math.Abs(float64(gotRotation.Dot(rotation))) < 1-tolerance {
		t.Fatalf("rotation = %v, want equivalent to %v", gotRotation, rotation)
	}
	assertMat4Near(t, TRS4(gotTranslation, gotRotation, gotScale), matrix)
}

func TestDecomposeRecoversMirroredScale(t *testing.T) {
	rotation := QuatAxisAngle(Vec3{Z: 1}, 0.4)
	matrix := TRS4(Vec3{1, 2, 3}, rotation, Vec3{-2, 3, 4})

	translation, gotRotation, scale, ok := matrix.Decompose()
	if !ok {
		t.Fatal("a mirrored matrix should decompose")
	}
	negatives := 0
	for _, axis := range []float32{scale.X, scale.Y, scale.Z} {
		if axis < 0 {
			negatives++
		}
	}
	if negatives != 1 {
		t.Fatalf("scale = %v, want exactly one negative axis", scale)
	}
	if got := gotRotation.Mat3().Determinant(); !near(got, 1) {
		t.Fatalf("rotation determinant = %v, want a proper rotation", got)
	}
	assertMat4Near(t, TRS4(translation, gotRotation, scale), matrix)
}

func TestDecomposeRejectsCollapsedAxis(t *testing.T) {
	if _, _, _, ok := TRS4(Vec3{1, 2, 3}, NewQuat(), Vec3{2, 0, 4}).Decompose(); ok {
		t.Fatal("a matrix with a zero-length column should not decompose")
	}
}

func TestTransformDirectionIgnoresTranslation(t *testing.T) {
	matrix := Translation4(10, 20, 30).Mul(RotationZ4(math.Pi / 2))

	if got := matrix.TransformPoint(Vec3{X: 1}); !vec3Near(got, Vec3{10, 21, 30}) {
		t.Fatalf("TransformPoint = %v, want (10, 21, 30)", got)
	}
	if got := matrix.TransformDirection(Vec3{X: 1}); !vec3Near(got, Vec3{Y: 1}) {
		t.Fatalf("TransformDirection = %v, want (0, 1, 0)", got)
	}
}

func TestMat4Mat3AndTranslationExtractBlocks(t *testing.T) {
	rotation := RotationY4(0.9)
	matrix := Translation4(7, 8, 9).Mul(rotation)

	if got, want := matrix.Translation(), (Vec3{7, 8, 9}); got != want {
		t.Fatalf("Translation = %v, want %v", got, want)
	}
	upper := matrix.Mat3()
	for column := 0; column < 3; column++ {
		for row := 0; row < 3; row++ {
			if got, want := upper[column*3+row], rotation[column*4+row]; got != want {
				t.Fatalf("Mat3[%d][%d] = %v, want %v", column, row, got, want)
			}
		}
	}
}

func TestInverseAffineInvertsAndAllocatesNothing(t *testing.T) {
	matrix := Translation4(2, 3, 4).Mul(RotationY4(0.3)).Mul(Scaling4(2, 4, 8))
	inverse, ok := matrix.InverseAffine()
	if !ok {
		t.Fatal("an affine matrix should invert")
	}
	assertMat4Near(t, matrix.Mul(inverse), NewMat4())
	assertMat4Near(t, inverse.Mul(matrix), NewMat4())

	general, _ := matrix.Inverse()
	assertMat4Near(t, inverse, general)

	if _, ok := Scaling4(1, 0, 1).InverseAffine(); ok {
		t.Fatal("a singular affine matrix should not invert")
	}

	if allocations := testing.AllocsPerRun(100, func() {
		inverseAffineSink, _ = matrix.InverseAffine()
	}); allocations != 0 {
		t.Fatalf("InverseAffine allocated %v times per run, want 0", allocations)
	}
}

var inverseAffineSink Mat4

func TestProjectRejectsPointsAtOrBehindTheEye(t *testing.T) {
	viewProjection := Perspective4(math.Pi/3, 1.5, 0.1, 100).Mul(LookAt4(Vec3{Z: 5}, Vec3{}, Vec3{Y: 1}))

	if _, ok := Project(viewProjection, Vec3{Z: 20}); ok {
		t.Fatal("a point behind the eye should not project")
	}
	if _, ok := Project(viewProjection, Vec3{Z: 5}); ok {
		t.Fatal("a point at the eye should not project")
	}
	if _, ok := Project(viewProjection, Vec3{}); !ok {
		t.Fatal("a point in front of the eye should project")
	}
}

func TestProjectUnprojectRoundTrip(t *testing.T) {
	viewProjection := Perspective4(math.Pi/3, 1.5, 0.1, 100).Mul(LookAt4(Vec3{X: 2, Y: 3, Z: 5}, Vec3{}, Vec3{Y: 1}))
	inverse, ok := viewProjection.Inverse()
	if !ok {
		t.Fatal("a view-projection should invert")
	}

	world := Vec3{X: -1, Y: 0.5, Z: 2}
	ndc, ok := Project(viewProjection, world)
	if !ok {
		t.Fatal("the sample point should project")
	}
	if !vec3Near(Unproject(inverse, ndc), world) {
		t.Fatalf("Unproject = %v, want %v", Unproject(inverse, ndc), world)
	}
}

func TestLookAt4FallsBackWhenUpIsParallelToForward(t *testing.T) {
	view := LookAt4(Vec3{Y: 10}, Vec3{}, Vec3{Y: 1})

	if got := view.TransformPoint(Vec3{}); !vec3Near(got, Vec3{Z: -10}) {
		t.Fatalf("target in view space = %v, want (0, 0, -10)", got)
	}
	side := Vec3{view[0], view[4], view[8]}
	vertical := Vec3{view[1], view[5], view[9]}
	forward := Vec3{-view[2], -view[6], -view[10]}
	if !near(side.Length(), 1) || !near(vertical.Length(), 1) || !near(forward.Length(), 1) {
		t.Fatalf("degenerate basis: side %v, vertical %v, forward %v", side, vertical, forward)
	}
	if !near(side.Dot(vertical), 0) || !near(side.Dot(forward), 0) || !near(vertical.Dot(forward), 0) {
		t.Fatalf("non-orthogonal basis: side %v, vertical %v, forward %v", side, vertical, forward)
	}
}

func TestOblique4WithoutShearIsOrthographic(t *testing.T) {
	// Shear 0 is the continuum's endpoint, not a special case: an app
	// animating a shear up from rest must pass through it unchanged.
	assertMat4Near(t, Oblique4(-2, 6, -3, 5, 1, 11, 0), Orthographic4(-2, 6, -3, 5, 1, 11))
}

func TestOblique4LeavesTheCameraPlaneUnsheared(t *testing.T) {
	// The shear pivots about z = 0, the camera's own plane, so a point there
	// projects exactly where the orthographic twin puts it. That is the whole
	// placement rule: put the camera in the plane you want held fixed.
	const shear = 0.5
	projection := Oblique4(-4, 4, -2, 2, -10, 10, shear)

	onCameraPlane, ok := Project(projection, Vec3{X: 2, Y: 1})
	if !ok {
		t.Fatal("a point on the camera plane should project")
	}
	unsheared, _ := Project(Orthographic4(-4, 4, -2, 2, -10, 10), Vec3{X: 2, Y: 1})
	if !vec3Near(onCameraPlane, unsheared) {
		t.Fatalf("the camera plane moved to %v, want the unsheared %v", onCameraPlane, unsheared)
	}
}

func TestOblique4ShearsDepthIntoScreenUpByTheGain(t *testing.T) {
	// One unit of view-space depth towards the viewer displaces screen-up by
	// exactly the gain, in the same world units the height is measured in: at
	// height 4, a unit of depth is 2/4 of NDC, and the gain multiplies it.
	const shear = 0.5
	projection := Oblique4(-8, 8, -2, 2, -10, 10, shear)

	base, _ := Project(projection, Vec3{})
	raised, _ := Project(projection, Vec3{Z: 1})
	if got, want := raised.Y-base.Y, float32(shear*2/4); !near(got, want) {
		t.Fatalf("a unit of depth displaced NDC y by %v, want %v", got, want)
	}
	// The ground is unforeshortened: nothing at a constant depth changes scale.
	sideBase, _ := Project(projection, Vec3{X: 8})
	if !near(sideBase.X-base.X, 1) {
		t.Fatalf("the horizontal scale changed under shear: %v, want 1", sideBase.X-base.X)
	}
	sideRaised, _ := Project(projection, Vec3{X: 8, Z: 1})
	if !near(sideRaised.Y-raised.Y, 0) || !near(sideRaised.X-raised.X, 1) {
		t.Fatal("the shear is not constant across the plane it shears")
	}
}

func TestOblique4KeepsDepthMonotonicAlongItsProjectionRay(t *testing.T) {
	// The ticket's load-bearing claim: the ordinary depth buffer still sorts an
	// oblique projection, so there is no painter's algorithm and no per-draw
	// sort. Along the projection ray the two points share a screen position,
	// and the nearer one must come back with the smaller clip depth.
	const shear = 1.5
	projection := Oblique4(-8, 8, -8, 8, -20, 20, shear)
	// The ray that leaves both screen coordinates unchanged: dx = 0 and
	// dy + k*dz = 0, so (0, -k, 1) up to scale.
	ray := Vec3{Y: -shear, Z: 1}

	nearer := Vec3{X: 1, Y: 2, Z: 3}
	farther := nearer.Sub(ray.MulS(5))

	nearNDC, ok := Project(projection, nearer)
	if !ok {
		t.Fatal("the nearer point should project")
	}
	farNDC, ok := Project(projection, farther)
	if !ok {
		t.Fatal("the farther point should project")
	}
	if !near(nearNDC.X, farNDC.X) || !near(nearNDC.Y, farNDC.Y) {
		t.Fatalf("the two points are not on one projection ray: %v and %v", nearNDC, farNDC)
	}
	if !(nearNDC.Z < farNDC.Z) {
		t.Fatalf("depth %v is not in front of %v along the projection ray", nearNDC.Z, farNDC.Z)
	}
}
