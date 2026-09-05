package scene

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

func testViewport() *app.Viewport {
	return &app.Viewport{
		Width: 800, Height: 600,
		WindowWidth: 800, WindowHeight: 600,
		FramebufferWidth: 1600, FramebufferHeight: 1200,
	}
}

// sizedTexture stands in for a baked renderable texture: only its declared
// size matters to a projection.
func sizedTexture(width, height int) gfx.TextureDescr {
	return gfx.TextureWithBytes(width, height, gfx.FormatRGBA8Srgb, nil, false, false)
}

func TestAScreenPassTakesItsAspectFromTheWindow(t *testing.T) {
	// The window size, not the framebuffer: all three candidate sources are
	// provably equal in aspect, and the window is the one on the update thread.
	aspect, err := passAspect(1, Pass{}, testViewport())
	if err != nil {
		t.Fatalf("screen pass: %v", err)
	}
	if math.Abs(float64(aspect)-800.0/600.0) > 1e-6 {
		t.Errorf("aspect = %v, want 800/600", aspect)
	}
}

func TestATexturedPassTakesItsAspectFromItsTarget(t *testing.T) {
	// A camera's passes may target different sizes, so there is no one camera
	// aspect to cache.
	pass := Pass{Target: gfx.TextureTarget(sizedTexture(1024, 256), 0, 0)}
	aspect, err := passAspect(1, pass, testViewport())
	if err != nil {
		t.Fatalf("texture pass: %v", err)
	}
	if math.Abs(float64(aspect)-4) > 1e-6 {
		t.Errorf("aspect = %v, want 4", aspect)
	}
}

func TestAColourlessPassTakesItsAspectFromItsDepthTexture(t *testing.T) {
	// Falling through to the window here would build a shadow pass's frustum
	// from the window's aspect and silently drop casters.
	pass := Pass{
		Tag:    "shadow",
		Target: gfx.NoTarget(),
		Depth:  gfx.DepthTarget(sizedTexture(2048, 1024)),
	}
	aspect, err := passAspect(1, pass, testViewport())
	if err != nil {
		t.Fatalf("depth-only pass: %v", err)
	}
	if math.Abs(float64(aspect)-2) > 1e-6 {
		t.Errorf("aspect = %v, want 2", aspect)
	}
}

func TestAColourlessPassWithoutADepthTextureIsReported(t *testing.T) {
	pass := Pass{Tag: "shadow", Target: gfx.NoTarget()}
	if _, err := passAspect(1, pass, testViewport()); err == nil {
		t.Error("a pass with nothing attached resolved an aspect")
	}
}

func TestAColourlessPassThatClearsColourIsReported(t *testing.T) {
	black := m.Color{A: 1}
	pass := Pass{
		Tag: "shadow", Target: gfx.NoTarget(),
		Depth: gfx.DepthTarget(sizedTexture(1024, 1024)), ClearColor: &black,
	}
	if _, err := passAspect(1, pass, testViewport()); err == nil {
		t.Error("a colourless pass was allowed to clear a colour it has no target for")
	}
}

func TestFovYIsTheLiteralVerticalFieldOfView(t *testing.T) {
	// Horizontal derives from the aspect, so a wider target shows more
	// horizontally rather than cropping the top and bottom.
	const fovY = math.Pi / 2 // 90 degrees: the far plane is exactly 2*far tall
	descr := CameraDescr{FovY: fovY, Near: 1, Far: 10}
	projection, err := projection(1, descr, 2)
	if err != nil {
		t.Fatalf("projection: %v", err)
	}
	// A point on the top edge of the far plane lands at NDC y = 1.
	top, ok := m.Project(projection, m.Vec3{Y: 10, Z: -10})
	if !ok {
		t.Fatal("a point inside the frustum did not project")
	}
	if math.Abs(float64(top.Y)-1) > 1e-4 {
		t.Errorf("the top of the far plane is at NDC y %v, want 1", top.Y)
	}
	// At aspect 2 the horizontal half-extent is twice the vertical one.
	side, ok := m.Project(projection, m.Vec3{X: 20, Z: -10})
	if !ok {
		t.Fatal("a point inside the frustum did not project")
	}
	if math.Abs(float64(side.X)-1) > 1e-4 {
		t.Errorf("the side of the far plane is at NDC x %v, want 1 at aspect 2", side.X)
	}
}

func TestDepthIsConventional(t *testing.T) {
	// Near maps to 0 and far to 1, so the useful ClearDepth is 1.0.
	projection, err := projection(1, CameraDescr{FovY: 1, Near: 1, Far: 100}, 1)
	if err != nil {
		t.Fatalf("projection: %v", err)
	}
	near, _ := m.Project(projection, m.Vec3{Z: -1})
	far, _ := m.Project(projection, m.Vec3{Z: -100})
	if math.Abs(float64(near.Z)) > 1e-4 {
		t.Errorf("the near plane is at depth %v, want 0", near.Z)
	}
	if math.Abs(float64(far.Z)-1) > 1e-4 {
		t.Errorf("the far plane is at depth %v, want 1", far.Z)
	}
}

func TestOrthographicHeightIsWorldUnitsAcrossTheTarget(t *testing.T) {
	descr := CameraDescr{Projection: Orthographic, Height: 10, Near: 1, Far: 100}
	projection, err := projection(1, descr, 2)
	if err != nil {
		t.Fatalf("projection: %v", err)
	}
	top, _ := m.Project(projection, m.Vec3{Y: 5, Z: -50})
	if math.Abs(float64(top.Y)-1) > 1e-4 {
		t.Errorf("5 units up is at NDC y %v, want 1 for a 10-unit height", top.Y)
	}
	side, _ := m.Project(projection, m.Vec3{X: 10, Z: -50})
	if math.Abs(float64(side.X)-1) > 1e-4 {
		t.Errorf("10 units across is at NDC x %v, want 1 at aspect 2", side.X)
	}
}

func TestADegenerateProjectionIsReported(t *testing.T) {
	cases := []struct {
		name  string
		descr CameraDescr
	}{
		{"no field of view", CameraDescr{Near: 1, Far: 10}},
		{"no orthographic height", CameraDescr{Projection: Orthographic, Near: 1, Far: 10}},
		{"near past far", CameraDescr{FovY: 1, Near: 10, Far: 1}},
	}
	for _, test := range cases {
		if _, err := projection(1, test.descr, 1); err == nil {
			t.Errorf("%s: resolved a projection anyway", test.name)
		}
	}
}
