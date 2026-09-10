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

func TestObliqueWithoutShearIsOrthographic(t *testing.T) {
	// The two kinds meet at Shear 0, so an app animating a shear up from rest
	// is never handed a reported error on its first frame.
	oblique, err := projection(1, CameraDescr{Projection: Oblique, Height: 10, Near: 1, Far: 100}, 2)
	if err != nil {
		t.Fatalf("oblique projection: %v", err)
	}
	orthographic, err := projection(1, CameraDescr{Projection: Orthographic, Height: 10, Near: 1, Far: 100}, 2)
	if err != nil {
		t.Fatalf("orthographic projection: %v", err)
	}
	if oblique != orthographic {
		t.Errorf("Oblique with no shear = %v, want the orthographic %v", oblique, orthographic)
	}
}

func TestObliqueLeavesTheGroundUnforeshortened(t *testing.T) {
	// The whole point of the kind: revealing a vertical face costs no
	// ground-plane scale. Height still means world units across the target,
	// measured in the camera's own plane.
	descr := CameraDescr{Projection: Oblique, Height: 10, Shear: 0.5, Near: -50, Far: 50}
	projection, err := projection(1, descr, 2)
	if err != nil {
		t.Fatalf("projection: %v", err)
	}
	top, _ := m.Project(projection, m.Vec3{Y: 5})
	if !near(top.Y, 1) {
		t.Errorf("5 units up is at NDC y %v, want 1 for a 10-unit height", top.Y)
	}
	side, _ := m.Project(projection, m.Vec3{X: 10})
	if !near(side.X, 1) {
		t.Errorf("10 units across is at NDC x %v, want 1 at aspect 2", side.X)
	}
}

func TestObliqueShearsDepthIntoScreenUpAndPivotsAtTheCamera(t *testing.T) {
	// Shear is a gain on view-space depth, and it pivots about the camera's
	// own plane - the placement rule a caller has to know, because getting it
	// wrong renders an empty screen with nothing reported.
	const shear = 0.5
	descr := CameraDescr{Projection: Oblique, Height: 10, Shear: shear, Near: -50, Far: 50}
	projection, err := projection(1, descr, 2)
	if err != nil {
		t.Fatalf("projection: %v", err)
	}
	origin, _ := m.Project(projection, m.Vec3{})
	if !near(origin.X, 0) || !near(origin.Y, 0) {
		t.Errorf("the camera plane moved to %v, want the origin", origin)
	}
	// One world unit towards the viewer rides up the screen by the gain, in
	// the same units Height is measured in: 0.5 of 10 units is 0.1 of NDC.
	raised, _ := m.Project(projection, m.Vec3{Z: 1})
	if !near(raised.Y-origin.Y, shear*2/10) {
		t.Errorf("a unit of depth displaced NDC y by %v, want %v", raised.Y-origin.Y, shear*2/10)
	}
	if !near(raised.X, origin.X) {
		t.Errorf("the shear moved NDC x to %v, want no horizontal component", raised.X)
	}
}

func TestObliqueRequiresAHeightAndNothingElse(t *testing.T) {
	// Oblique inherits Orthographic's rules exactly. A zero shear is the
	// continuum's endpoint, a negative one is a mirror, and a large one is
	// merely a useless elevation - none of them is degenerate.
	if _, err := projection(1, CameraDescr{Projection: Oblique, Shear: 1, Near: 1, Far: 10}, 1); err == nil {
		t.Error("an oblique camera with no height resolved a projection anyway")
	}
	for _, shear := range []float32{0, -0.5, 1000} {
		descr := CameraDescr{Projection: Oblique, Height: 10, Shear: shear, Near: 1, Far: 10}
		if _, err := projection(1, descr, 1); err != nil {
			t.Errorf("shear %v: %v", shear, err)
		}
	}
}

func TestShearIsReadByObliqueAlone(t *testing.T) {
	// Shear rides beside FovY and Height, and like them it is a field one kind
	// reads and the others do not.
	for _, kind := range []ProjectionKind{Perspective, Orthographic} {
		descr := CameraDescr{Projection: kind, FovY: 1, Height: 10, Near: 1, Far: 100}
		plain, err := projection(1, descr, 2)
		if err != nil {
			t.Fatalf("projection: %v", err)
		}
		descr.Shear = 0.75
		sheared, err := projection(1, descr, 2)
		if err != nil {
			t.Fatalf("projection: %v", err)
		}
		if plain != sheared {
			t.Errorf("kind %v read Shear", kind)
		}
	}
}

func TestPerspectiveDefersItsViewDirectionToTheFragment(t *testing.T) {
	// A perspective camera has a real eye, so the view vector is radial from
	// it and cannot be a constant. The zero selector is what says so.
	direction := viewDirection(CameraDescr{FovY: 1, Near: 1, Far: 100})
	if direction.W != 0 {
		t.Errorf("perspective view direction selector = %v, want 0", direction.W)
	}
}

func TestOrthographicViewDirectionIsTheCameraAxis(t *testing.T) {
	// An orthographic camera has no eye point: its view direction is constant
	// across the frame, and it is the camera's own +Z, towards the viewer.
	direction := viewDirection(CameraDescr{Projection: Orthographic, Height: 10, Near: 1, Far: 100})
	if direction.W != 1 {
		t.Errorf("orthographic view direction selector = %v, want 1", direction.W)
	}
	if got := direction.Vec3(); !near(got.X, 0) || !near(got.Y, 0) || !near(got.Z, 1) {
		t.Errorf("view direction = %v, want the camera's +Z", got)
	}
}

func TestObliqueViewDirectionIsTheProjectionRayNotTheCameraAxis(t *testing.T) {
	// The bug the kind would otherwise introduce: under an oblique projection
	// the camera looks one way and the viewer sees another, so a view vector
	// differenced against the camera transform lights vertical faces as if
	// edge-on and floors as if head-on - the exact inverse of what is drawn.
	//
	// A camera rotated -90 degrees about X looks straight down, so its local
	// +Z is world +Y and its screen-up is world -Z. At shear 1 the implied
	// elevation is atan(1/1), so the viewer sits 45 degrees up on the +Z side.
	descr := CameraDescr{
		Transform:  Transform{Rotation: m.QuatAxisAngle(m.Vec3{X: 1}, -math.Pi/2)},
		Projection: Oblique, Height: 10, Shear: 1, Near: -50, Far: 50,
	}
	direction := viewDirection(descr)
	if direction.W != 1 {
		t.Errorf("oblique view direction selector = %v, want 1", direction.W)
	}
	const diagonal = math.Sqrt2 / 2
	got := direction.Vec3()
	if !near(got.X, 0) || !near(got.Y, diagonal) || !near(got.Z, diagonal) {
		t.Errorf("view direction = %v, want (0, %v, %v)", got, diagonal, diagonal)
	}
	// It is emphatically not the camera axis, which is what the old code read.
	axis := viewDirection(CameraDescr{Transform: descr.Transform, Projection: Orthographic, Height: 10, Near: 1, Far: 100})
	if near(got.Z, axis.Vec3().Z) {
		t.Error("the oblique view direction is the camera's own axis")
	}
}

func TestViewDirectionIsUnitLengthUnderAScaledCamera(t *testing.T) {
	// The view matrix ignores a TRS camera's scale, so the view direction must
	// too, and a Matrix override that carries one must still come back unit.
	scaled := CameraDescr{
		Transform:  Transform{Rotation: m.QuatAxisAngle(m.Vec3{Y: 1}, 0.4), Scale: 8},
		Projection: Oblique, Height: 10, Shear: 0.5, Near: -50, Far: 50,
	}
	if got := scaled.Transform.Mat4(); got[0] == 0 {
		t.Fatal("the test camera did not build a matrix")
	}
	if got := viewDirection(scaled).Vec3().Length(); !near(got, 1) {
		t.Errorf("view direction length under a scaled camera = %v, want 1", got)
	}
	override := m.Translation4(3, 4, 5).Mul(m.Scaling4(2, 2, 2))
	overridden := scaled
	overridden.Transform = Transform{Matrix: &override}
	if got := viewDirection(overridden).Vec3().Length(); !near(got, 1) {
		t.Errorf("view direction length under a matrix override = %v, want 1", got)
	}
}

// The arithmetic here is sceneViewDirection's, verbatim - frame.wgsl mixes the
// per-fragment difference against the packed constant by w - so this is the
// seam where the packer's encoding and the shader's reading of it are held to
// the same answer. Nothing else checks it: no test runs a fragment.
func shaderViewDirection(block sceneFrameBlock, position m.Vec3) m.Vec3 {
	toEye := block.CameraPosition.Vec3().Sub(position)
	constant := block.ViewDirection.Vec3()
	selector := block.ViewDirection.W
	return toEye.Add(constant.Sub(toEye).MulS(selector)).Normalize()
}

func TestTheViewDirectionSelectorPicksRadialOrConstant(t *testing.T) {
	eye := m.Vec3{X: 2, Y: 6, Z: 4}
	transform := Transform{Position: eye, Rotation: m.QuatAxisAngle(m.Vec3{X: 1}, -math.Pi/2)}
	surfaces := []m.Vec3{{X: -8, Z: -8}, {X: 9, Y: 1, Z: 7}, {X: 0, Y: 3, Z: -2}}

	// A perspective camera has a real eye, so every surface sees a different
	// view vector, and the selector must leave the difference alone.
	perspective := sceneFrameBlock{
		CameraPosition: cameraPosition(transform),
		ViewDirection:  viewDirection(CameraDescr{Transform: transform, FovY: 1, Near: 1, Far: 100}),
	}
	for _, surface := range surfaces {
		want := eye.Sub(surface).Normalize()
		if got := shaderViewDirection(perspective, surface); !near(got.X, want.X) || !near(got.Y, want.Y) || !near(got.Z, want.Z) {
			t.Errorf("perspective view at %v = %v, want the radial %v", surface, got, want)
		}
	}

	// An oblique camera's rays are parallel, so every surface must see the one
	// constant - and emphatically not something that varies with the eye it
	// does not have.
	descr := CameraDescr{Transform: transform, Projection: Oblique, Height: 10, Shear: 1, Near: -50, Far: 50}
	oblique := sceneFrameBlock{CameraPosition: cameraPosition(transform), ViewDirection: viewDirection(descr)}
	want := viewDirection(descr).Vec3()
	for _, surface := range surfaces {
		if got := shaderViewDirection(oblique, surface); !near(got.X, want.X) || !near(got.Y, want.Y) || !near(got.Z, want.Z) {
			t.Errorf("oblique view at %v = %v, want the constant %v", surface, got, want)
		}
	}
}
