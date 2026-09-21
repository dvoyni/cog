package types

import (
	"errors"

	"github.com/dvoyni/cog/libs/m"
)

// Projection builds a camera's projection matrix at one pass's aspect, resolving
// the descr to m.Projection's plain parameters and re-reporting a degenerate
// one under the camera's id.
//
// FovY is the literal vertical field of view and Height the orthographic twin;
// horizontal derives from the aspect, so a wider target shows more horizontally
// and a narrower one crops the sides. There is no FovAxis and no reference
// aspect, because a 3D camera has no reference framing it does not invent.
func Projection(id CameraID, descr CameraDescr, aspect float32) (m.Mat4, error) {
	matrix, err := m.Projection(descr.Projection == Perspective, descr.FovY, descr.Height, descr.shear(), descr.Near, descr.Far, aspect)
	if degenerate := (m.ErrProjectionDegenerate{}); errors.As(err, &degenerate) {
		return m.Mat4{}, ErrCameraProjectionDegenerate{Camera: id, Reason: degenerate.Reason}
	}
	return matrix, err
}

// ViewDirection is m.ViewDirection for a camera: the constant direction towards
// its viewer with w = 1 under Orthographic and Oblique, and the zero vector
// under Perspective, whose view vector is radial and differenced per fragment.
func ViewDirection(descr CameraDescr) m.Vec4 {
	return m.ViewDirection(descr.Transform, descr.Projection == Perspective, descr.shear())
}
