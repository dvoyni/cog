package internal

import (
	"errors"
	"strconv"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// depthClearFar is the depth a camera's default pass clears to.
const depthClearFar float32 = 1

// defaultPass is the one pass a Camera whose Passes are empty gets: forward,
// clearing depth, keeping colour.
func defaultPass() Pass {
	return Pass{Tag: TagForward, ClearDepth: m.Some(depthClearFar)}
}

// shear is a Camera's shear, which only an Oblique projection reads.
func shear(camera *Camera) float32 {
	if camera.Projection != Oblique {
		return 0
	}
	return camera.Shear
}

// projection builds a Camera's projection matrix at one pass's aspect through
// libs/m, re-reporting a degenerate one under the camera's id.
func projection(camera *Camera, aspect float32) (m.Mat4, error) {
	matrix, err := m.Projection(camera.Projection == Perspective,
		camera.FovY, camera.Height, shear(camera), camera.Near, camera.Far, aspect)
	if degenerate := (m.ErrProjectionDegenerate{}); errors.As(err, &degenerate) {
		return m.Mat4{}, ErrCameraProjectionDegenerate{Camera: camera.ID, Reason: degenerate.Reason}
	}
	return matrix, err
}

// ViewProjection is the matrix a Camera placed at at draws through, for a
// viewport of the given size in pixels: the one its screen-targeted passes
// upload, Oblique shear and Near/Far included. A caller picking or placing a
// marker passes it to m.WorldToScreen, m.ScreenToWorld or m.ScreenToRay.
//
// A camera the renderer skips is refused with the error it reports for it,
// and a viewport with a side of zero or less with ErrViewportUnsized, rather
// than a matrix a pick silently misses through.
func ViewProjection(camera Camera, at m.Transform, viewport m.Vec2) (m.Mat4, error) {
	view, err := cameraView(&camera, at)
	if err != nil {
		return m.Mat4{}, err
	}
	if viewport.X <= 0 || viewport.Y <= 0 {
		return m.Mat4{}, ErrViewportUnsized{Camera: camera.ID, Viewport: viewport}
	}
	projectionMatrix, err := projection(&camera, viewport.X/viewport.Y)
	if err != nil {
		return m.Mat4{}, err
	}
	return projectionMatrix.Mul(view), nil
}

// cameraView is the view matrix of a Camera placed at place, refusing a
// camera missing a clip plane: the projection it would get instead is
// degenerate, and every pass built from it would cull against a volume nobody
// asked for.
func cameraView(camera *Camera, place m.Transform) (m.Mat4, error) {
	if camera.Near == 0 || camera.Far == 0 {
		return m.Mat4{}, ErrCameraClipPlanesMissing{Camera: camera.ID, Near: camera.Near, Far: camera.Far}
	}
	view, ok := m.CameraView(place)
	if !ok {
		return m.Mat4{}, ErrCameraProjectionDegenerate{Camera: camera.ID, Reason: "the transform has no inverse"}
	}
	return view, nil
}

// viewDirection is m.ViewDirection for a Camera placed at place.
func viewDirection(camera *Camera, place m.Transform) m.Vec4 {
	return m.ViewDirection(place, camera.Projection == Perspective, shear(camera))
}

// cameraPosition reads the eye out of a camera's transform. Scale is ignored
// the way the view matrix ignores it.
func cameraPosition(transform m.Transform) m.Vec4 {
	eye := transform.Mat4().Translation()
	return m.Vec4{X: eye.X, Y: eye.Y, Z: eye.Z, W: 1}
}

// passAspect resolves the aspect a pass's projection is built from. It is
// per pass, never per camera: one camera's passes legitimately target a
// shadow map and the screen in the same frame. A screen-targeted pass takes
// the window's aspect, which the update thread can read.
func passAspect(id CameraID, pass *Pass, view *gfx.Viewport) (float32, error) {
	if pass.Target.IsNone() {
		if _, clears := pass.ClearColor.Get(); clears {
			return 0, ErrColourlessPassClearsColour{Camera: id, Tag: tagOf(pass.Tag)}
		}
		// A depth-only pass has no colour attachment to take a size from, and
		// falling through to the screen would build its frustum from the
		// window's aspect and silently drop casters.
		width, height, ok := pass.Depth.Size()
		if !ok {
			return 0, ErrColourlessPassWithoutDepth{Camera: id, Tag: tagOf(pass.Tag)}
		}
		return aspectOf(id, pass, float32(width), float32(height))
	}
	if width, height, ok := pass.Target.Size(); ok {
		return aspectOf(id, pass, float32(width), float32(height))
	}
	return aspectOf(id, pass, view.WindowWidth, view.WindowHeight)
}

func aspectOf(id CameraID, pass *Pass, width, height float32) (float32, error) {
	if width <= 0 || height <= 0 {
		return 0, ErrPassTargetUnsized{Camera: id, Tag: tagOf(pass.Tag)}
	}
	return width / height, nil
}

// passLabel keys the debug label of one camera's pass. Labels are cached
// because the camera set is stable frame to frame.
type passLabel struct {
	camera CameraID
	tag    PassTag
}

// passDescr translates one Camera pass into the gfx pass it emits.
//
// Store ops are inferred rather than exposed. Depth is kept iff the pass names
// an explicit depth texture, and discarded otherwise. Colour is always kept.
func passDescr(labels map[passLabel]string, id CameraID, pass *Pass, order gfx.Order) gfx.PassDescr {
	desc := gfx.PassDescr{
		Order:      order,
		Target:     pass.Target,
		Depth:      pass.Depth,
		DepthStore: gfx.StoreDiscard,
		Label:      label(labels, id, tagOf(pass.Tag)),
	}
	if pass.Depth.IsTexture() {
		desc.DepthStore = gfx.StoreKeep
	}
	if color, ok := pass.ClearColor.Get(); ok {
		desc.Load, desc.Clear = gfx.LoadClear, color
	}
	if depth, ok := pass.ClearDepth.Get(); ok {
		desc.DepthLoad, desc.DepthClear = gfx.LoadClear, depth
	}
	return desc
}

// label is a pass's debug label, "scene.camera<id>.<tag>": a pass is labelled
// by the camera and tag it draws, so a HUD or a comparison reading gfx's
// frame finds a camera's passes by name.
func label(labels map[passLabel]string, id CameraID, tag PassTag) string {
	key := passLabel{camera: id, tag: tag}
	text, ok := labels[key]
	if !ok {
		text = "scene.camera" + strconv.Itoa(int(id)) + "." + string(tag)
		labels[key] = text
	}
	return text
}
