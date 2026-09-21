package m

// Transform places one thing in the world. Its zero value is the identity, so
// a caller who cares about none of it writes none of it.
//
// Scale is per axis. Only an all-zero Scale reads as the identity, which is
// what keeps the zero Transform the identity; a partly zero Scale is taken
// literally, so Vec3{X: 2} collapses the thing onto the X axis rather than
// silently becoming (2,1,1), and a flattened scale - Vec3{X: 1, Y: 1} - is
// expressible. WithScale is the uniform spelling.
//
// There is no matrix override. A Transform is plain values, which is what lets
// an ECS Component hold one; anything a matrix said that position, rotation and
// per-axis scale cannot - a shear - is not something a Transform describes.
//
// It lives here rather than in scene because more than one consumer places
// things: scene draws at a Transform and sound is heard from one, and neither
// may depend on the other. For the same reason it is, unwrapped, the ECS
// Component that says where an Entity stands: the ecs plugin registers its one
// Store, and every binding reads that Store rather than a copy of its own.
type Transform struct {
	Position Vec3
	Rotation Quat
	Scale    Vec3 // all zero means (1,1,1); otherwise literal
}

// At is the transform of a thing standing at a point, unrotated and unscaled.
func At(x, y, z float32) Transform {
	return Transform{Position: Vec3{X: x, Y: y, Z: z}}
}

// WithScale scales the transform uniformly by s. Zero is the identity, as an
// all-zero Scale is.
func (t Transform) WithScale(s float32) Transform {
	t.Scale = Vec3{X: s, Y: s, Z: s}
	return t
}

func (t Transform) WithRotation(q Quat) Transform {
	t.Rotation = q
	return t
}

// LookAt returns the transform of a thing standing at eye and facing target.
// It is a position and a rotation rather than a view matrix, because a camera
// that is not a Transform is the one thing in the API that will not compose
// with a follow rig - and scene would decompose the matrix for culling anyway.
func LookAt(eye, target, up Vec3) Transform {
	world, ok := LookAt4(eye, target, up).InverseAffine()
	if !ok {
		return Transform{Position: eye}
	}
	return Transform{Position: eye, Rotation: QuatFromMat4(world)}
}

// Mat4 resolves the transform to a model matrix.
func (t Transform) Mat4() Mat4 {
	return TRS4(t.Position, t.rotation(), t.scale())
}

// ScaledMat4 resolves the transform to a model matrix with extra applied on top
// of its own scale, per axis. It exists so a caller that stretches a transform
// does not need the resolved scale, which is what keeps scale unexported.
func (t Transform) ScaledMat4(extra Vec3) Mat4 {
	return TRS4(t.Position, t.rotation(), extra.Mul(t.scale()))
}

// Forward, Right and Up are the axes the transform faces along, normalized.
// They are the world's axes: right-handed, facing -Z with +Y up.
func (t Transform) Forward() Vec3 { return t.Mat4().Forward() }
func (t Transform) Right() Vec3   { return t.Mat4().Right() }
func (t Transform) Up() Vec3      { return t.Mat4().Up() }

// scale reads an all-zero Scale as the identity, and any other as written.
func (t Transform) scale() Vec3 {
	if t.Scale == (Vec3{}) {
		return Vec3{X: 1, Y: 1, Z: 1}
	}
	return t.Scale
}

// rotation reads Rotation as the identity when it was never written. The zero
// Quat is (0,0,0,0), which is not a rotation at all: taken at face value it
// collapses the basis to nothing and the frame renders empty.
func (t Transform) rotation() Quat {
	if t.Rotation == (Quat{}) {
		return Quat{W: 1}
	}
	return t.Rotation
}
