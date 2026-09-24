package internal

import (
	"errors"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// passAspect resolves the aspect a pass's projection is built from. It is
// per-pass, never per-camera: one camera's passes legitimately target a
// 1024x1024 shadow map and the screen in the same frame, so there is no single
// camera aspect to resolve once.
//
// A screen-targeted pass renders at framebuffer resolution, but its aspect
// comes from the window size on the update thread. All three candidate sources
// are provably equal in aspect, and this is the one scene can read without
// reaching onto the render thread. The cost is that the aspect is up to one
// frame stale during a window resize, which can mis-cull only something already
// touching the frustum edge.
func passAspect(id CameraID, pass Pass, view *gfx.Viewport) (float32, error) {
	if pass.Target.IsNone() {
		if _, clears := pass.ClearColor.Get(); clears {
			return 0, ErrColourlessPassClearsColour{Camera: id, Tag: PassTagOf(pass)}
		}
		// A depth-only pass has no colour attachment to take a size from, and
		// falling through to the screen would build its frustum from the
		// window's aspect and silently drop casters.
		width, height, ok := pass.Depth.Size()
		if !ok {
			return 0, ErrColourlessPassWithoutDepth{Camera: id, Tag: PassTagOf(pass)}
		}
		return aspectOf(id, pass, float32(width), float32(height))
	}
	if width, height, ok := pass.Target.Size(); ok {
		return aspectOf(id, pass, float32(width), float32(height))
	}
	return aspectOf(id, pass, view.WindowWidth, view.WindowHeight)
}

func aspectOf(id CameraID, pass Pass, width, height float32) (float32, error) {
	if width <= 0 || height <= 0 {
		return 0, ErrPassTargetUnsized{Camera: id, Tag: PassTagOf(pass)}
	}
	return width / height, nil
}

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
