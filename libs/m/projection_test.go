package m

import (
	"errors"
	"math"
	"testing"
)

func closeVec3(a, b Vec3) bool {
	return math.Abs(float64(a.X-b.X)) < tolerance && math.Abs(float64(a.Y-b.Y)) < tolerance && math.Abs(float64(a.Z-b.Z)) < tolerance
}

func TestProjectionPicksTheKindsMatrix(t *testing.T) {
	perspective, err := Projection(true, 1, 99, 99, 1, 100, 2)
	if err != nil || perspective != Perspective4(1, 2, 1, 100) {
		t.Fatalf("perspective = %v, %v; want Perspective4 ignoring height and shear", perspective, err)
	}
	parallel, err := Projection(false, 99, 10, 0.5, 1, 100, 2)
	if err != nil || parallel != Oblique4(-10, 10, -5, 5, 1, 100, 0.5) {
		t.Fatalf("parallel = %v, %v; want Oblique4 ignoring fovY", parallel, err)
	}
}

func TestProjectionReportsDegenerateParameters(t *testing.T) {
	for _, test := range []struct {
		name                           string
		perspective                    bool
		fovY, height, near, far, shear float32
	}{
		{"near past far", true, 1, 0, 10, 1, 0},
		{"zero fovY", true, 0, 0, 1, 10, 0},
		{"zero height", false, 0, 0, 1, 10, 1},
	} {
		_, err := Projection(test.perspective, test.fovY, test.height, test.shear, test.near, test.far, 1)
		var degenerate ErrProjectionDegenerate
		if !errors.As(err, &degenerate) || degenerate.Reason == "" {
			t.Fatalf("%s: err = %v, want an ErrProjectionDegenerate with a reason", test.name, err)
		}
	}
}

func TestViewDirectionIsZeroForPerspectiveAndTheRayOtherwise(t *testing.T) {
	camera := At(3, 4, 5)
	if got := ViewDirection(camera, true, 1); got != (Vec4{}) {
		t.Fatalf("perspective = %v, want zero", got)
	}
	if got := ViewDirection(camera, false, 0); !closeVec3(got.Vec3(), Vec3{Z: 1}) || got.W != 1 {
		t.Fatalf("orthographic = %v, want +Z with w 1", got)
	}
	want := Vec3{Y: -1, Z: 1}.Normalize()
	if got := ViewDirection(camera.WithScale(7), false, 1); !closeVec3(got.Vec3(), want) {
		t.Fatalf("oblique = %v, want %v whatever the scale", got, want)
	}
}

func TestScreenCoordinatesRoundTripWithYDown(t *testing.T) {
	projection, _ := Projection(true, 1, 0, 0, 1, 100, 2)
	view, ok := CameraView(LookAt(Vec3{Z: 10}, Vec3{}, Vec3{Y: 1}))
	if !ok {
		t.Fatal("CameraView refused a plain camera")
	}
	viewProjection, viewport := projection.Mul(view), Vec2{X: 200, Y: 100}

	centre, ok := WorldToScreen(viewProjection, viewport, Vec3{})
	if !ok || !closeVec3(Vec3{X: centre.X, Y: centre.Y}, Vec3{X: 100, Y: 50}) {
		t.Fatalf("origin = %v, %v; want the viewport centre", centre, ok)
	}
	above, _ := WorldToScreen(viewProjection, viewport, Vec3{Y: 1})
	if above.Y >= centre.Y {
		t.Fatalf("world up went to screen y %v, want above %v", above.Y, centre.Y)
	}
	world := Vec3{X: 1, Y: -2, Z: 3}
	screen, _ := WorldToScreen(viewProjection, viewport, world)
	if back, ok := ScreenToWorld(viewProjection, viewport, screen); !ok || !closeVec3(back, world) {
		t.Fatalf("round trip = %v, %v; want %v", back, ok, world)
	}
	ray, ok := ScreenToRay(viewProjection, viewport, Vec2{X: centre.X, Y: centre.Y})
	if !ok || !closeVec3(ray.Dir, Vec3{Z: -1}) {
		t.Fatalf("centre ray = %v, %v; want straight down -Z", ray, ok)
	}
	if _, ok := WorldToScreen(viewProjection, viewport, Vec3{Z: 20}); ok {
		t.Fatal("a point behind the eye got a coordinate")
	}
}

func TestScreenCoordinatesRefuseADegenerateViewport(t *testing.T) {
	for _, viewport := range []Vec2{{}, {X: 100}, {X: -1, Y: 100}} {
		if _, ok := WorldToScreen(NewMat4(), viewport, Vec3{}); ok {
			t.Fatalf("WorldToScreen accepted viewport %v", viewport)
		}
		if _, ok := ScreenToWorld(NewMat4(), viewport, Vec3{}); ok {
			t.Fatalf("ScreenToWorld accepted viewport %v", viewport)
		}
		if _, ok := ScreenToRay(NewMat4(), viewport, Vec2{}); ok {
			t.Fatalf("ScreenToRay accepted viewport %v", viewport)
		}
	}
}
