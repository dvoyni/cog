package types

import "github.com/dvoyni/cog/libs/m"

// CameraView is m.CameraView, kept under scene's name for the flush and its
// tests.
func CameraView(t m.Transform) (m.Mat4, bool) {
	return m.CameraView(t)
}
