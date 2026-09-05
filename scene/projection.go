package scene

import (
	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/m"
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
func passAspect(id CameraID, pass Pass, view *app.Viewport) (float32, error) {
	if pass.Target.IsNone() {
		if pass.ClearColor != nil {
			return 0, ErrColourlessPassClearsColour{Camera: id, Tag: pass.tag()}
		}
		// A depth-only pass has no colour attachment to take a size from, and
		// falling through to the screen would build its frustum from the
		// window's aspect and silently drop casters.
		width, height, ok := pass.Depth.Size()
		if !ok {
			return 0, ErrColourlessPassWithoutDepth{Camera: id, Tag: pass.tag()}
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
		return 0, ErrPassTargetUnsized{Camera: id, Tag: pass.tag()}
	}
	return width / height, nil
}

// projection builds a camera's projection matrix at one pass's aspect.
//
// FovY is the literal vertical field of view and Height the orthographic twin;
// horizontal derives from the aspect, so a wider target shows more horizontally
// and a narrower one crops the sides. There is no FovAxis and no reference
// aspect, because a 3D camera has no reference framing it does not invent.
func projection(id CameraID, descr CameraDescr, aspect float32) (m.Mat4, error) {
	if descr.Near >= descr.Far {
		return m.Mat4{}, ErrCameraProjectionDegenerate{Camera: id, Reason: "Near is at or past Far"}
	}
	if descr.Projection == Orthographic {
		if descr.Height <= 0 {
			return m.Mat4{}, ErrCameraProjectionDegenerate{Camera: id, Reason: "Height is zero or negative"}
		}
		half := descr.Height / 2
		return m.Orthographic4(-half*aspect, half*aspect, -half, half, descr.Near, descr.Far), nil
	}
	if descr.FovY <= 0 {
		return m.Mat4{}, ErrCameraProjectionDegenerate{Camera: id, Reason: "FovY is zero or negative"}
	}
	return m.Perspective4(descr.FovY, aspect, descr.Near, descr.Far), nil
}
