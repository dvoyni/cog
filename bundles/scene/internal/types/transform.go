package types

import "github.com/dvoyni/cog/libs/m"

// CameraView inverts a camera's transform into a view matrix, ignoring Scale.
// Scaling a view matrix scales the whole world instead, and the field cannot be
// avoided at the call site because its zero already means one.
func CameraView(t m.Transform) (m.Mat4, bool) {
	return cameraBasis(t).InverseAffine()
}

// cameraBasis is the world matrix a camera is read through: its own, with the
// scale dropped, because a scaled camera scales the world instead.
//
// It exists so CameraView and ViewDirection cannot disagree about which matrix
// the camera is. They resolve the same rotation from it, one inverted and one
// not, and a scale applied to one but not the other would tilt every
// view-dependent shading term against the geometry it shades.
func cameraBasis(t m.Transform) m.Mat4 {
	t.Scale = m.Vec3{}
	return t.Mat4()
}
