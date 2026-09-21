package types

import "github.com/dvoyni/cog/libs/m"

// The coordinate helpers behind scene.ViewProjection, scene.WorldToScreen,
// scene.ScreenToWorld and scene.ScreenToRay; the root documents them and
// forwards here. Each resolves the camera to a view-projection and hands the
// arithmetic to libs/m, which ecsscene's camera calls too.

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
	return m.WorldToScreen(matrix, viewport, world)
}

// ScreenToWorld is scene.ScreenToWorld.
func ScreenToWorld(camera CameraDescr, viewport m.Vec2, screen m.Vec3) (m.Vec3, bool) {
	matrix, ok := viewProjection(camera, viewport)
	if !ok {
		return m.Vec3{}, false
	}
	return m.ScreenToWorld(matrix, viewport, screen)
}

// ScreenToRay is scene.ScreenToRay.
func ScreenToRay(camera CameraDescr, viewport m.Vec2, screen m.Vec2) (m.Ray, bool) {
	matrix, ok := viewProjection(camera, viewport)
	if !ok {
		return m.Ray{}, false
	}
	return m.ScreenToRay(matrix, viewport, screen)
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
	view, ok := m.CameraView(camera.Transform)
	if !ok {
		return m.Mat4{}, false
	}
	return projection.Mul(view), true
}
