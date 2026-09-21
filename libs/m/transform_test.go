package m

import (
	"math"
	"testing"
)

func TestTheZeroTransformResolvesToTheIdentityMatrix(t *testing.T) {
	if got := (Transform{}).Mat4(); got != NewMat4() {
		t.Fatalf("zero Transform resolved to %v, want the identity", got)
	}
}

func TestAnUnrotatedTransformFacesNegativeZWithPositiveYUp(t *testing.T) {
	tr := At(3, 4, 5)
	if got := tr.Forward(); !vec3Near(got, Vec3{Z: -1}) {
		t.Errorf("Forward() = %v, want (0,0,-1)", got)
	}
	if got := tr.Right(); !vec3Near(got, Vec3{X: 1}) {
		t.Errorf("Right() = %v, want (1,0,0)", got)
	}
	if got := tr.Up(); !vec3Near(got, Vec3{Y: 1}) {
		t.Errorf("Up() = %v, want (0,1,0)", got)
	}
}

func TestTurningAQuarterCircleAboutUpSwingsForwardOntoTheLeftAxis(t *testing.T) {
	tr := Transform{}.WithRotation(QuatAxisAngle(Vec3{Y: 1}, float32(math.Pi/2)))
	if got := tr.Forward(); !vec3Near(got, Vec3{X: -1}) {
		t.Errorf("Forward() = %v, want (-1,0,0)", got)
	}
	if got := tr.Right(); !vec3Near(got, Vec3{Z: -1}) {
		t.Errorf("Right() = %v, want (0,0,-1)", got)
	}
	if got := tr.Up(); !vec3Near(got, Vec3{Y: 1}) {
		t.Errorf("Up() = %v, want (0,1,0)", got)
	}
}

func TestTheBasisAxesStayUnitVectorsUnderAScale(t *testing.T) {
	tr := At(0, 0, 0).WithScale(7)
	for name, got := range map[string]Vec3{"Forward": tr.Forward(), "Right": tr.Right(), "Up": tr.Up()} {
		if !near(got.Length(), 1) {
			t.Errorf("%s() = %v, whose length is %v, want 1", name, got, got.Length())
		}
	}
}

func TestLookAtFacesItsTarget(t *testing.T) {
	eye, target := Vec3{X: 1, Y: 2, Z: 3}, Vec3{X: 4, Y: 2, Z: -5}
	tr := LookAt(eye, target, Vec3{Y: 1})
	if tr.Position != eye {
		t.Errorf("Position = %v, want %v", tr.Position, eye)
	}
	if got, want := tr.Forward(), target.Sub(eye).Normalize(); !vec3Near(got, want) {
		t.Errorf("Forward() = %v, want %v", got, want)
	}
}

func TestAnAllZeroScaleReadsAsOneAndAPartlyZeroOneIsTakenLiterally(t *testing.T) {
	if got := (Transform{}).Mat4().TransformDirection(Vec3{X: 1}); !vec3Near(got, Vec3{X: 1}) {
		t.Errorf("an all-zero Scale scaled X by %v, want (1,0,0)", got)
	}
	flat := Transform{Scale: Vec3{X: 2}}
	if got := flat.Mat4().TransformDirection(Vec3{Y: 1}); !vec3Near(got, Vec3{}) {
		t.Errorf("a partly zero Scale scaled Y by %v, want it collapsed to zero", got)
	}
}

func TestScaledMat4AppliesItsExtraOnTopOfTheTransformsOwnScale(t *testing.T) {
	tr := At(0, 0, 0).WithScale(3)
	got := tr.ScaledMat4(Vec3{X: 2, Y: 2, Z: 2}).TransformDirection(Vec3{X: 1})
	if !vec3Near(got, Vec3{X: 6}) {
		t.Errorf("ScaledMat4 scaled X by %v, want (6,0,0)", got)
	}
}

func TestAPerAxisScaleScalesEachAxisAndLeavesTheTranslationAlone(t *testing.T) {
	tr := Transform{Position: Vec3{X: 1}, Scale: Vec3{X: 2, Y: 3, Z: 4}}
	if got := tr.Mat4().TransformPoint(Vec3{X: 1, Y: 1, Z: 1}); !vec3Near(got, Vec3{X: 3, Y: 3, Z: 4}) {
		t.Errorf("per-axis scale gave %v, want (3,3,4)", got)
	}
	if got := At(1, 0, 0).WithScale(2).Mat4().TransformPoint(Vec3{X: 2}); !vec3Near(got, Vec3{X: 5}) {
		t.Errorf("uniformly scaled translate gave %v, want (5,0,0)", got)
	}
}
