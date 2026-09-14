package types

import "github.com/dvoyni/cog/libs/m"

// The coordinate helpers behind scene.ViewProjection, scene.WorldToScreen,
// scene.ScreenToWorld and scene.ScreenToRay; the root documents them and
// forwards here.

// ViewProjection is scene.ViewProjection.
func ViewProjection(camera CameraDescr, viewport m.Vec2) m.Mat4 {
	matrix, ok := viewProjection(camera, viewport)
	if !ok {
		return m.NewMat4()
	}
	return matrix
}

// WorldToScreen is scene.WorldToScreen.
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

// ScreenToWorld is scene.ScreenToWorld.
func ScreenToWorld(camera CameraDescr, viewport m.Vec2, screen m.Vec3) (m.Vec3, bool) {
	inverse, ok := inverseViewProjection(camera, viewport)
	if !ok {
		return m.Vec3{}, false
	}
	return m.Unproject(inverse, screenToNDC(viewport, screen)), true
}

// ScreenToRay is scene.ScreenToRay.
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
	projection, err := Projection(0, camera, viewport.X/viewport.Y)
	if err != nil {
		return m.Mat4{}, false
	}
	view, ok := CameraView(camera.Transform)
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
