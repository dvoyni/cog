package scene

import (
	"github.com/dvoyni/cog/bundles/scene/internal"
	"github.com/dvoyni/cog/libs/m"
)

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
type Transform = internal.Transform

// At is the transform of a thing standing at a point, unrotated and unscaled.
func At(x, y, z float32) Transform { return internal.At(x, y, z) }

// LookAt returns the transform of a camera standing at eye and facing target.
// It is a position and a rotation rather than a view matrix, because a camera
// that is not a Transform is the one thing in the API that will not compose
// with a follow rig — and scene would decompose the matrix for culling anyway.
func LookAt(eye, target, up m.Vec3) Transform { return internal.LookAt(eye, target, up) }
