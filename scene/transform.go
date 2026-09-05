package scene

import "github.com/dvoyni/cog/m"

// Transform places one recorded thing in the world. Its zero value is the
// identity, so a caller who cares about none of it writes none of it.
//
// Scale is scalar, following canvas.SpriteTransform.Scale. An m.Vec3 scale
// reads as "twice as wide" for m.Vec3{X: 2} but has to silently become (2,1,1),
// a legitimately flattened scale is inexpressible either way, and it forces the
// inverse-transpose normal path onto every draw. Non-uniform scale goes through
// Matrix, which replaces the transform whole.
type Transform struct {
	Position m.Vec3
	Rotation m.Quat
	Scale    float32 // zero means 1
	Matrix   *m.Mat4 // non-nil replaces the whole transform
}

// At is the transform of a thing standing at a point, unrotated and unscaled.
func At(x, y, z float32) Transform {
	return Transform{Position: m.Vec3{X: x, Y: y, Z: z}}
}

func (t Transform) WithScale(s float32) Transform {
	t.Scale = s
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
	if t.Matrix != nil {
		return *t.Matrix
	}
	scale := t.Scale
	if scale == 0 {
		scale = 1
	}
	return m.TRS4(t.Position, t.rotation(), m.Vec3{X: scale, Y: scale, Z: scale})
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
	if t.Matrix == nil {
		t.Scale = 1
	}
	return t.Mat4().InverseAffine()
}
