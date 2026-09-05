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
