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
	if descr.Projection == Orthographic || descr.Projection == Oblique {
		if descr.Height <= 0 {
			return m.Mat4{}, ErrCameraProjectionDegenerate{Camera: id, Reason: "Height is zero or negative"}
		}
		// The two kinds share every rule and meet at Shear 0, so there is no
		// second error to report here: nothing about a shear is degenerate - 0
		// is the continuum's endpoint, which an app animating a shear up from
		// rest must pass through, a negative one is a mirror, and a large one
		// is only a useless elevation.
		half := descr.Height / 2
		return m.Oblique4(-half*aspect, half*aspect, -half, half, descr.Near, descr.Far, descr.shear()), nil
	}
	if descr.FovY <= 0 {
		return m.Mat4{}, ErrCameraProjectionDegenerate{Camera: id, Reason: "FovY is zero or negative"}
	}
	return m.Perspective4(descr.FovY, aspect, descr.Near, descr.Far), nil
}

// viewDirection is the direction a camera's viewer looks from, packed as the
// shader reads it: xyz the constant world direction from a surface towards the
// viewer, and w a mix selector - 1 when that constant is the answer, 0 when the
// shader must difference against the camera position per fragment instead.
//
// Only Perspective has a real eye, and only there is the view vector radial.
// Orthographic and Oblique project along parallel rays, so their view direction
// is one vector for the whole frame. Getting this from the camera's translation
// is wrong for both: an orthographic camera has no eye point, and an oblique
// one looks one way while its viewer sees another, so every view-dependent term
// lights vertical faces as if edge-on and floors as if head-on.
//
// The ray is the direction that leaves both screen coordinates unchanged. The
// projection sends (x, y, z) to (x, y + shear*z), so a direction moves nothing
// when dx is 0 and dy + shear*dz is 0: (0, -shear, 1) in view space, which at
// shear 0 is the camera's own +Z and so serves Orthographic by the same line.
func viewDirection(descr CameraDescr) m.Vec4 {
	if descr.Projection == Perspective {
		return m.Vec4{}
	}
	// The basis rather than the transform's own matrix: cameraView drops a TRS
	// camera's scale, so reading the scaled matrix here would put the direction
	// and the view matrix into disagreement. TransformDirection is the right
	// operation either way - a projection ray is a direction, not a normal, so
	// no inverse-transpose question arises.
	ray := cameraBasis(descr.Transform).TransformDirection(m.Vec3{Y: -descr.shear(), Z: 1}).Normalize()
	return m.Vec4{X: ray.X, Y: ray.Y, Z: ray.Z, W: 1}
}
