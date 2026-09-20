package types

import "github.com/dvoyni/cog/libs/m"

// Transform places one recorded thing in the world. It is m.Transform, which
// lives in m because scene is not its only consumer: sound is heard from one
// too, and neither package may depend on the other.
//
// A non-uniform scale costs its draw the inverse-transpose normal path in the
// shader, and only its draw: the packer flags the instances whose basis does
// not scale uniformly, and every other instance keeps the plain one.
type Transform = m.Transform

// At is the transform of a thing standing at a point, unrotated and unscaled.
func At(x, y, z float32) Transform { return m.At(x, y, z) }

// LookAt returns the transform of a camera standing at eye and facing target.
// It is a position and a rotation rather than a view matrix, because a camera
// that is not a Transform is the one thing in the API that will not compose
// with a follow rig - and scene would decompose the matrix for culling anyway.
func LookAt(eye, target, up m.Vec3) Transform { return m.LookAt(eye, target, up) }

// CameraView inverts a camera's transform into a view matrix, ignoring Scale.
// Scaling a view matrix scales the whole world instead, and the field cannot be
// avoided at the call site because its zero already means one.
func CameraView(t Transform) (m.Mat4, bool) {
	return cameraBasis(t).InverseAffine()
}

// cameraBasis is the world matrix a camera is read through: its own, with the
// scale dropped, because a scaled camera scales the world instead.
//
// It exists so CameraView and ViewDirection cannot disagree about which matrix
// the camera is. They resolve the same rotation from it, one inverted and one
// not, and a scale applied to one but not the other would tilt every
// view-dependent shading term against the geometry it shades.
func cameraBasis(t Transform) m.Mat4 {
	t.Scale = m.Vec3{}
	return t.Mat4()
}
