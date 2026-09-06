package scene

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/m"
)

// target is the size the helpers are asked about. Deliberately not square, so a
// transposed viewport or an aspect applied to the wrong axis shows up.
var target = m.Vec2{X: 800, Y: 600}

func frontCamera() CameraDescr {
	return CameraDescr{
		Transform: LookAt(m.Vec3{Z: 10}, m.Vec3{}, m.Vec3{Y: 1}),
		FovY:      1.0472,
		Near:      0.1, Far: 100,
	}
}

func orthoCamera() CameraDescr {
	return CameraDescr{
		Transform:  LookAt(m.Vec3{Z: 10}, m.Vec3{}, m.Vec3{Y: 1}),
		Projection: Orthographic,
		Height:     8,
		Near:       0.1, Far: 100,
	}
}

func TestScreenToWorldRoundTripsWorldToScreen(t *testing.T) {
	// The one assertion that catches a sign error: the flip, the aspect and the
	// 0..1 depth convention all have to agree in both directions for a point
	// off the axes to come back.
	for name, camera := range map[string]CameraDescr{"perspective": simpleCamera(), "orthographic": orthoCamera()} {
		t.Run(name, func(t *testing.T) {
			world := m.Vec3{X: 0.7, Y: -1.3, Z: 0.4}
			screen, ok := WorldToScreen(camera, target, world)
			if !ok {
				t.Fatalf("WorldToScreen(%v) refused a point in front of the camera", world)
			}
			back, ok := ScreenToWorld(camera, target, screen)
			if !ok {
				t.Fatal("ScreenToWorld refused its own output")
			}
			if !near(back.X, world.X) || !near(back.Y, world.Y) || !near(back.Z, world.Z) {
				t.Errorf("round trip = %v, want %v (screen was %v)", back, world, screen)
			}
		})
	}
}

func TestScreenIsYDownFromTheTopLeft(t *testing.T) {
	// Screen is logical viewport coordinates, origin top-left, which is what ui
	// and pointer handling already use: the point the camera looks at lands in
	// the middle, and a point higher in the world lands at a smaller y.
	camera := frontCamera()
	centre, ok := WorldToScreen(camera, target, m.Vec3{})
	if !ok || !near(centre.X, target.X/2) || !near(centre.Y, target.Y/2) {
		t.Fatalf("the look-at point maps to %v (ok=%v), want the centre of the target", centre, ok)
	}
	above, _ := WorldToScreen(camera, target, m.Vec3{Y: 1})
	if above.Y >= centre.Y {
		t.Errorf("a point above the centre maps to y=%v, want less than %v - the Y flip is missing", above.Y, centre.Y)
	}
	right, _ := WorldToScreen(camera, target, m.Vec3{X: 1})
	if right.X <= centre.X {
		t.Errorf("a point to the right maps to x=%v, want more than %v", right.X, centre.X)
	}
}

func TestWorldToScreenDepthIsZeroAtNearAndOneAtFar(t *testing.T) {
	camera := frontCamera()
	camera.Near, camera.Far = 1, 11
	atNear, _ := WorldToScreen(camera, target, m.Vec3{Z: 9})
	if !near(atNear.Z, 0) {
		t.Errorf("depth at the near plane = %v, want 0", atNear.Z)
	}
	atFar, _ := WorldToScreen(camera, target, m.Vec3{Z: -1})
	if !near(atFar.Z, 1) {
		t.Errorf("depth at the far plane = %v, want 1", atFar.Z)
	}
}

func TestWorldToScreenRefusesAPointBehindTheEye(t *testing.T) {
	// The classic bug: a negative w divides into a plausible, mirrored,
	// confidently wrong point. It gets no coordinate at all.
	camera := frontCamera()
	for _, world := range []m.Vec3{{X: 1, Z: 20}, {Z: 10}, {X: -1, Y: 2, Z: 11}} {
		if screen, ok := WorldToScreen(camera, target, world); ok {
			t.Errorf("WorldToScreen(%v) = %v, true; want refused - it is at or behind the eye", world, screen)
		} else if screen != (m.Vec3{}) {
			t.Errorf("a refused WorldToScreen returned %v, want the zero value", screen)
		}
	}
}

func TestOffScreenInFrontAndOutsideTheDepthRangeStayTrue(t *testing.T) {
	// Off-screen indicator arrows are the second most common use of this
	// helper, so an extrapolated coordinate past the target edge is real.
	camera := frontCamera()
	camera.Near, camera.Far = 1, 20
	off, ok := WorldToScreen(camera, target, m.Vec3{X: 40, Y: 30})
	if !ok {
		t.Fatal("a point off to the side but in front was refused")
	}
	if off.X <= target.X || off.Y >= 0 {
		t.Errorf("off-screen point = %v, want extrapolated past the right edge and above the top", off)
	}
	beyond, ok := WorldToScreen(camera, target, m.Vec3{Z: -100})
	if !ok {
		t.Fatal("a point past the far plane was refused")
	}
	if beyond.Z <= 1 {
		t.Errorf("depth past the far plane = %v, want more than 1", beyond.Z)
	}
}

func TestOrthographicNeverFailsTheWTest(t *testing.T) {
	// An orthographic clip w is 1 everywhere, so a point behind the camera has
	// a coordinate - out of the 0..1 depth range, and correct there.
	behind, ok := WorldToScreen(orthoCamera(), target, m.Vec3{X: 1, Z: 20})
	if !ok {
		t.Fatal("an orthographic camera refused a point behind it; only the degenerate case fails")
	}
	if behind.Z >= 0 {
		t.Errorf("depth behind an orthographic eye = %v, want negative", behind.Z)
	}
}

func TestTheHelpersDegenerateSilently(t *testing.T) {
	// A zero-area viewport, a zero or inverted Near/Far, a zero FovY or Height:
	// the zero value with ok = false, and the identity from ViewProjection,
	// exactly as canvas.LayerTransform does for a zero-area window. The "zero
	// Near/Far is a reported error" diagnostic belongs at flush.
	good := simpleCamera()
	cases := map[string]struct {
		camera   CameraDescr
		viewport m.Vec2
	}{
		"zero viewport":      {good, m.Vec2{}},
		"zero-height target": {good, m.Vec2{X: 800}},
		"negative viewport":  {good, m.Vec2{X: -800, Y: 600}},
		"no depth range":     {CameraDescr{Transform: good.Transform, FovY: 1}, target},
		"far before near":    {CameraDescr{Transform: good.Transform, FovY: 1, Near: 10, Far: 1}, target},
		"zero fov":           {CameraDescr{Transform: good.Transform, Near: 0.1, Far: 100}, target},
		"zero ortho height":  {CameraDescr{Transform: good.Transform, Projection: Orthographic, Near: 0.1, Far: 100}, target},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if got := ViewProjection(test.camera, test.viewport); got != m.NewMat4() {
				t.Errorf("ViewProjection = %v, want the identity", got)
			}
			if got, ok := WorldToScreen(test.camera, test.viewport, m.Vec3{}); ok || got != (m.Vec3{}) {
				t.Errorf("WorldToScreen = %v, %v; want the zero value and false", got, ok)
			}
			if got, ok := ScreenToWorld(test.camera, test.viewport, m.Vec3{}); ok || got != (m.Vec3{}) {
				t.Errorf("ScreenToWorld = %v, %v; want the zero value and false", got, ok)
			}
			if got, ok := ScreenToRay(test.camera, test.viewport, m.Vec2{}); ok || got != (m.Ray{}) {
				t.Errorf("ScreenToRay = %v, %v; want the zero value and false", got, ok)
			}
		})
	}
}

func TestScreenToRayShootsThroughThePixel(t *testing.T) {
	camera := frontCamera()
	camera.Near, camera.Far = 1, 100
	centre, ok := ScreenToRay(camera, target, m.Vec2{X: target.X / 2, Y: target.Y / 2})
	if !ok {
		t.Fatal("ScreenToRay refused a valid camera")
	}
	if length := centre.Dir.Length(); !near(length, 1) {
		t.Errorf("Dir length = %v, want unit - every intersect's t is a world distance only if it is", length)
	}
	if !near(centre.Dir.X, 0) || !near(centre.Dir.Y, 0) || !near(centre.Dir.Z, -1) {
		t.Errorf("the centre ray points %v, want straight down -Z", centre.Dir)
	}
	// The origin is the pixel on the near plane, so t is a distance from there.
	if !near(centre.Origin.Z, 9) {
		t.Errorf("the ray starts at z=%v, want the near plane at z=9", centre.Origin.Z)
	}
	if distance, hit := centre.IntersectSphere(m.Sphere{Radius: 1}); !hit || !near(distance, 8) {
		t.Errorf("the centre ray hit the unit sphere at t=%v (%v), want t=8", distance, hit)
	}
	// A ray through a pixel above the centre goes up in world space, which is
	// the same Y flip WorldToScreen applies, read backwards.
	up, _ := ScreenToRay(camera, target, m.Vec2{X: target.X / 2, Y: target.Y / 4})
	if up.Dir.Y <= 0 {
		t.Errorf("the ray through a pixel above the centre points %v, want +Y", up.Dir)
	}
}

func TestScreenToRayAgreesWithWorldToScreen(t *testing.T) {
	// Picking is the reason this helper exists: the pixel a point projects to
	// must produce a ray that passes back through the point.
	for name, camera := range map[string]CameraDescr{"perspective": simpleCamera(), "orthographic": orthoCamera()} {
		t.Run(name, func(t *testing.T) {
			world := m.Vec3{X: 1.5, Y: -0.5, Z: 2}
			screen, ok := WorldToScreen(camera, target, world)
			if !ok {
				t.Fatal("WorldToScreen refused a point in front of the camera")
			}
			ray, ok := ScreenToRay(camera, target, m.Vec2{X: screen.X, Y: screen.Y})
			if !ok {
				t.Fatal("ScreenToRay refused the same camera")
			}
			if _, hit := ray.IntersectSphere(m.Sphere{Center: world, Radius: 0.01}); !hit {
				t.Error("the ray through the projected pixel missed the point it came from")
			}
		})
	}
}

func TestTheHelpersArePerTargetNotPerCamera(t *testing.T) {
	// One camera legitimately renders a 1024x1024 shadow map and the window, so
	// the size is the caller's to name: the same world point lands elsewhere,
	// and only the horizontal framing changes with the aspect.
	camera := frontCamera()
	wide, _ := WorldToScreen(camera, m.Vec2{X: 800, Y: 600}, m.Vec3{X: 1, Y: 1})
	square, _ := WorldToScreen(camera, m.Vec2{X: 600, Y: 600}, m.Vec3{X: 1, Y: 1})
	if near(wide.X/800, square.X/600) {
		t.Error("the horizontal fraction of the target did not change with the aspect")
	}
	if !near(wide.Y/600, square.Y/600) {
		t.Errorf("the vertical fraction changed with the aspect: %v vs %v - FovY is the literal vertical field of view", wide.Y/600, square.Y/600)
	}
}

func TestViewProjectionIgnoresCameraScale(t *testing.T) {
	// Scaling a view matrix scales the whole world instead, and the field
	// cannot be avoided at the call site because its zero already means one.
	camera := simpleCamera()
	scaled := camera
	scaled.Transform = camera.Transform.WithScale(7)
	plain, scaledMatrix := ViewProjection(camera, target), ViewProjection(scaled, target)
	for i := range plain {
		if math.Abs(float64(plain[i]-scaledMatrix[i])) > 1e-5 {
			t.Fatalf("a scaled camera projects differently:\n%v\n%v", plain, scaledMatrix)
		}
	}
}

func TestViewProjectionIsWhatWorldToScreenProjectsThrough(t *testing.T) {
	// Published as an output precisely so a caller with many points drops to
	// m.Project in a loop over one matrix.
	camera, world := simpleCamera(), m.Vec3{X: 0.4, Y: 1.1, Z: -0.2}
	ndc, ok := m.Project(ViewProjection(camera, target), world)
	if !ok {
		t.Fatal("m.Project refused a point in front of the camera")
	}
	screen, _ := WorldToScreen(camera, target, world)
	want := m.Vec3{X: (ndc.X + 1) * 0.5 * target.X, Y: (1 - ndc.Y) * 0.5 * target.Y, Z: ndc.Z}
	if !near(screen.X, want.X) || !near(screen.Y, want.Y) || !near(screen.Z, want.Z) {
		t.Errorf("WorldToScreen = %v, want %v from the published ViewProjection", screen, want)
	}
}
