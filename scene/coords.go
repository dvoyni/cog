package scene

import "github.com/dvoyni/cog/m"

// The coordinate helpers are pure package-level functions, callable on any
// thread with no plugin instance. A lookup against last frame's resolved camera
// state would buy only staleness, a LookupAccess dependency in code that is
// otherwise arithmetic, and nothing at all for a camera not recorded this
// frame. PassView.Frustum stays an inspection and test surface, not a
// coordinate API.
//
// Screen is logical viewport coordinates, origin top-left, Y down, which is
// what ui and pointer handling already use. WebGPU NDC is Y-up and
// origin-centre, so these four functions flip Y:
//
//	x = (ndc.x + 1) * 0.5 * viewport.X
//	y = (1 - ndc.y) * 0.5 * viewport.Y
//
// This is the one place scene flips Y, and it stands beside the render-pipeline
// finding that no Y flip is needed for scene. Both are true - one of the
// pipeline, where geometry goes to clip space and stays there, one of these
// helpers, which cross into a Y-down 2D space the pipeline never touches - and
// neither sentence may be used to delete the other.
//
// Every function takes the target's size in pixels, never an aspect and never a
// camera-wide framing: a camera that renders both a 1024x1024 shadow map and
// the window has no single screen for a point to be on, so the caller names the
// size it means. A TemporaryTarget camera therefore returns texture pixels,
// which canvas maps to the screen with its own WorldToScreen.
//
// Degenerate input is silent - the zero value with ok = false, and the identity
// from ViewProjection, exactly as canvas.LayerTransform returns identity for a
// zero-area window. A pure function has no kernel handle; the "a zero Near/Far
// is a reported error" diagnostic happens at flush, which is where a caller who
// forgot Far will actually see it.

// ViewProjection is the matrix a camera projects through for a target of the
// given size in pixels, and the identity for a degenerate camera or viewport.
//
// It is published as an output so a caller with many points drops to m.Project
// in a loop over one matrix, and it is the single place the FovY-is-vertical
// rule, the Transform inversion, the ignored camera scale and the 0..1 depth
// convention are encoded.
func ViewProjection(camera CameraDescr, viewport m.Vec2) m.Mat4 {
	matrix, ok := viewProjection(camera, viewport)
	if !ok {
		return m.NewMat4()
	}
	return matrix
}

// WorldToScreen maps a world point to logical viewport coordinates for a target
// of the given size. X and Y are target pixels from the top-left, and Z is the
// WebGPU 0..1 NDC depth - exactly what ScreenToWorld takes back.
//
// ok is false when the point is at or behind the eye plane, or the camera or
// viewport is degenerate. Off-screen but in front stays true: the coordinate is
// extrapolated past the target edge and is correct there, which is what an
// off-screen indicator arrow needs. Depth outside Near/Far likewise stays true,
// and an orthographic camera never fails the eye-plane test because its clip w
// is 1 everywhere.
//
// Behind the camera never returns a coordinate. Dividing by a negative w yields
// a plausible, mirrored, confidently wrong point, and no NaN sentinel is
// offered either: a bool the compiler makes you look at beats a value that
// silently propagates.
func WorldToScreen(camera CameraDescr, viewport m.Vec2, world m.Vec3) (m.Vec3, bool) {
	matrix, ok := viewProjection(camera, viewport)
	if !ok {
		return m.Vec3{}, false
	}
	ndc, ok := m.Project(matrix, world)
	if !ok {
		return m.Vec3{}, false
	}
	return m.Vec3{
		X: (ndc.X + 1) * 0.5 * viewport.X,
		Y: (1 - ndc.Y) * 0.5 * viewport.Y,
		Z: ndc.Z,
	}, true
}

// ScreenToWorld maps a screen point back to the world. It takes WorldToScreen's
// output unchanged - X and Y in target pixels from the top-left, Z the 0..1 NDC
// depth - so ScreenToWorld(c, vp, WorldToScreen(c, vp, p)) round-trips.
//
// ok is false for the degenerate case only. A screen point outside the target
// or a depth outside 0..1 is extrapolated, not refused.
func ScreenToWorld(camera CameraDescr, viewport m.Vec2, screen m.Vec3) (m.Vec3, bool) {
	inverse, ok := inverseViewProjection(camera, viewport)
	if !ok {
		return m.Vec3{}, false
	}
	return m.Unproject(inverse, screenToNDC(viewport, screen)), true
}

// ScreenToRay is the ray through a screen pixel, starting on the near plane and
// pointing into the scene. Dir is unit length, so an intersect's t is a world
// distance from that near-plane origin.
//
// ok is false for the degenerate case only. Scene has no list to raycast
// against - draws are frame-local and consumed at flush - so picking is a loop
// over the caller's own entities, calling Bounds or AABB and keeping the
// smallest t.
func ScreenToRay(camera CameraDescr, viewport m.Vec2, screen m.Vec2) (m.Ray, bool) {
	inverse, ok := inverseViewProjection(camera, viewport)
	if !ok {
		return m.Ray{}, false
	}
	origin := m.Unproject(inverse, screenToNDC(viewport, m.Vec3{X: screen.X, Y: screen.Y}))
	into := m.Unproject(inverse, screenToNDC(viewport, m.Vec3{X: screen.X, Y: screen.Y, Z: 1}))
	return m.NewRay(origin, into.Sub(origin)), true
}

// screenToNDC undoes the Y flip and the pixel scale. Z passes through: clip
// depth is already the 0..1 convention on both sides.
func screenToNDC(viewport m.Vec2, screen m.Vec3) m.Vec3 {
	return m.Vec3{
		X: screen.X/viewport.X*2 - 1,
		Y: 1 - screen.Y/viewport.Y*2,
		Z: screen.Z,
	}
}

// viewProjection resolves a camera against a target size, reusing the same
// projection the plugin builds at flush so the helpers cannot drift from what
// is drawn. The camera id it passes is a placeholder: a pure function reports
// nothing, and every projection error collapses to ok = false here.
func viewProjection(camera CameraDescr, viewport m.Vec2) (m.Mat4, bool) {
	if viewport.X <= 0 || viewport.Y <= 0 {
		return m.Mat4{}, false
	}
	projection, err := projection(0, camera, viewport.X/viewport.Y)
	if err != nil {
		return m.Mat4{}, false
	}
	view, ok := cameraView(camera.Transform)
	if !ok {
		return m.Mat4{}, false
	}
	return projection.Mul(view), true
}

// inverseViewProjection inverts a resolved view-projection. A perspective
// view-projection is not affine, so this is the general m.Mat4.Inverse, which
// allocates five slices per call - ScreenToWorld and ScreenToRay therefore
// allocate once per call. Building the inverse in closed form from the camera
// parameters is a permitted private optimisation; nothing in the contract
// promises the allocation either way.
func inverseViewProjection(camera CameraDescr, viewport m.Vec2) (m.Mat4, bool) {
	matrix, ok := viewProjection(camera, viewport)
	if !ok {
		return m.Mat4{}, false
	}
	return matrix.Inverse()
}
