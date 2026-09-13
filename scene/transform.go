package scene

import "github.com/dvoyni/cog/m"

// Transform places one recorded thing in the world. Its zero value is the
// identity, so a caller who cares about none of it writes none of it.
//
// Scale is per axis. Only an all-zero Scale reads as the identity, which is
// what keeps the zero Transform the identity; a partly zero Scale is taken
// literally, so m.Vec3{X: 2} collapses the draw onto the X axis rather than
// silently becoming (2,1,1), and a flattened scale - m.Vec3{X: 1, Y: 1} - is
// expressible. WithScale is the uniform spelling.
//
// A non-uniform scale costs its draw the inverse-transpose normal path in the
// shader, and only its draw: the packer flags the instances whose basis does
// not scale uniformly, and every other instance keeps the plain one.
//
// There is no matrix override. A Transform is plain values, which is what lets
// an ECS Component hold one; anything a matrix said that position, rotation and
// per-axis scale cannot - a shear - is not something scene draws.
type Transform struct {
	Position m.Vec3
	Rotation m.Quat
	Scale    m.Vec3 // all zero means (1,1,1); otherwise literal
}

// At is the transform of a thing standing at a point, unrotated and unscaled.
func At(x, y, z float32) Transform {
	return Transform{Position: m.Vec3{X: x, Y: y, Z: z}}
}

// WithScale scales the transform uniformly by s. Zero is the identity, as an
// all-zero Scale is.
func (t Transform) WithScale(s float32) Transform {
	t.Scale = m.Vec3{X: s, Y: s, Z: s}
	return t
}

func (t Transform) WithRotation(q m.Quat) Transform {
	t.Rotation = q
	return t
}

// LookAt returns the transform of a camera standing at eye and facing target.
// It is a position and a rotation rather than a view matrix, because a camera
// that is not a Transform is the one thing in the API that will not compose
// with a follow rig — and scene would decompose the matrix for culling anyway.
func LookAt(eye, target, up m.Vec3) Transform {
	world, ok := m.LookAt4(eye, target, up).InverseAffine()
	if !ok {
		return Transform{Position: eye}
	}
	return Transform{Position: eye, Rotation: m.QuatFromMat4(world)}
}

// Mat4 resolves the transform to a model matrix.
func (t Transform) Mat4() m.Mat4 {
	return m.TRS4(t.Position, t.rotation(), t.scale())
}

// scale reads an all-zero Scale as the identity, and any other as written.
func (t Transform) scale() m.Vec3 {
	if t.Scale == (m.Vec3{}) {
		return m.Vec3{X: 1, Y: 1, Z: 1}
	}
	return t.Scale
}

// rotation reads Rotation as the identity when it was never written. The zero
// Quat is (0,0,0,0), which is not a rotation at all: taken at face value it
// collapses the basis to nothing and the frame renders empty.
func (t Transform) rotation() m.Quat {
	if t.Rotation == (m.Quat{}) {
		return m.Quat{W: 1}
	}
	return t.Rotation
}

// cameraView inverts a camera's transform into a view matrix, ignoring Scale.
// Scaling a view matrix scales the whole world instead, and the field cannot be
// avoided at the call site because its zero already means one.
func cameraView(t Transform) (m.Mat4, bool) {
	return cameraBasis(t).InverseAffine()
}

// cameraBasis is the world matrix a camera is read through: its own, with the
// scale dropped, because a scaled camera scales the world instead.
//
// It exists so cameraView and viewDirection cannot disagree about which matrix
// the camera is. They resolve the same rotation from it, one inverted and one
// not, and a scale applied to one but not the other would tilt every
// view-dependent shading term against the geometry it shades.
func cameraBasis(t Transform) m.Mat4 {
	t.Scale = m.Vec3{}
	return t.Mat4()
}
