package sceneimpl

import (
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/bundles/scene/internal"
	"github.com/dvoyni/cog/extensions/gfx"
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
func passAspect(id scene.CameraID, pass scene.Pass, view *gfx.Viewport) (float32, error) {
	if pass.Target.IsNone() {
		if _, clears := pass.ClearColor.Get(); clears {
			return 0, scene.ErrColourlessPassClearsColour{Camera: id, Tag: internal.PassTagOf(pass)}
		}
		// A depth-only pass has no colour attachment to take a size from, and
		// falling through to the screen would build its frustum from the
		// window's aspect and silently drop casters.
		width, height, ok := pass.Depth.Size()
		if !ok {
			return 0, scene.ErrColourlessPassWithoutDepth{Camera: id, Tag: internal.PassTagOf(pass)}
		}
		return aspectOf(id, pass, float32(width), float32(height))
	}
	if width, height, ok := pass.Target.Size(); ok {
		return aspectOf(id, pass, float32(width), float32(height))
	}
	return aspectOf(id, pass, view.WindowWidth, view.WindowHeight)
}

func aspectOf(id scene.CameraID, pass scene.Pass, width, height float32) (float32, error) {
	if width <= 0 || height <= 0 {
		return 0, scene.ErrPassTargetUnsized{Camera: id, Tag: internal.PassTagOf(pass)}
	}
	return width / height, nil
}
