package canvas

import (
	"fmt"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// cameraDraw is one camera of a frame as it draws: the camera as set; its
// target as it draws into it, its texture or else its window, the main
// window when it named neither; its pass's colour target, that target,
// cleared by the pass's load when it clears a viewport covering the target;
// its view into the target and its pass parameters; the runs of the frame's
// sorted entries its layers hold; its batches, part of the renderer's; and,
// for a clear drawn over its viewport, its instance in its command list's
// instance data.
type cameraDraw struct {
	camera        Camera
	target        drawnTarget
	colorTarget   render.ColorTarget
	view          cameraView
	block         cameraBlock
	layerRuns     [4][]sortedEntry
	batches       []batch
	clearInstance uint32
}

// makeCameraDraw answers how camera draws, its layer runs and batches still
// to find, and whether it draws at all: its target, the texture it names or
// else its window or mainWindow, measured from render's descriptor of the
// texture or from manager's sizes of the window, its output encoded
// wherever the target's format encodes nothing, as a window's BGRA8Unorm
// framebuffer does not; and its view into the target. A camera into a
// window not open or not sized, or with no view, as of an empty viewport,
// draws nothing; one naming both a window and a texture, or a texture
// render does not hold, is reported once to logger too.
func makeCameraDraw(inventory render.Inventory, manager *platform.WindowManager, mainWindow platform.Window, logger *kernel.Logger, camera *Camera) (cameraDraw, bool) {
	drawnCamera := cameraDraw{camera: *camera}
	encode := true
	switch {
	case camera.Texture != (render.Texture{}) && camera.Window != (platform.Window{}):
		refusal := Report{Kind: ReportKindCameraTargetRefused, Err: fmt.Errorf("canvas: a camera names both window %v and texture %v as its target", camera.Window, camera.Texture)}
		logger.LogErrOnce(refusal.Error(), refusal)
		return cameraDraw{}, false
	case camera.Texture != (render.Texture{}):
		descriptor, held := inventory.TextureDescriptor(camera.Texture)
		if !held {
			refusal := Report{Kind: ReportKindCameraTargetRefused, Err: fmt.Errorf("canvas: a camera's target, texture %v, is one render does not hold", camera.Texture)}
			logger.LogErrOnce(refusal.Error(), refusal)
			return cameraDraw{}, false
		}
		drawnCamera.target = drawnTarget{texture: camera.Texture, width: int(descriptor.Width), height: int(descriptor.Height), pixelsPerPoint: 1}
		encode = descriptor.Format != render.TextureFormatRGBA8UnormSrgb && descriptor.Format != render.TextureFormatBGRA8UnormSrgb
	default:
		window := camera.Window
		if window == (platform.Window{}) {
			window = mainWindow
		}
		if window == (platform.Window{}) {
			return cameraDraw{}, false
		}
		framebufferSize := manager.FramebufferSize(window)
		scale := manager.Scale(window)
		if framebufferSize.Width <= 0 || framebufferSize.Height <= 0 || !(scale > 0) {
			return cameraDraw{}, false
		}
		drawnCamera.target = drawnTarget{window: window, width: framebufferSize.Width, height: framebufferSize.Height, pixelsPerPoint: scale}
	}
	view, ok := makeCameraView(camera, drawnCamera.target.width, drawnCamera.target.height, drawnCamera.target.pixelsPerPoint)
	if !ok {
		return cameraDraw{}, false
	}
	drawnCamera.view = view
	drawnCamera.block = makeCameraBlock(&view, encode)
	clearsByLoad := camera.Clear && view.viewport == (m.Recti{Width: drawnCamera.target.width, Height: drawnCamera.target.height})
	drawnCamera.colorTarget = render.ColorTarget{Window: drawnCamera.target.window, Texture: drawnCamera.target.texture, Clear: clearsByLoad}
	if clearsByLoad {
		drawnCamera.colorTarget.ClearColor = makeLoadedClearColor(camera.ClearColor, encode)
	}
	return drawnCamera, true
}

// makeLoadedClearColor answers color, linear, as a pass's load clears its
// target to it: premultiplied, as canvas blends, and its colour encoded as
// sRGB when encode is set, for a target whose format encodes nothing, as
// canvas_output encodes what the camera draws, so a clear by the load and
// one drawn over a viewport show alike.
func makeLoadedClearColor(color m.Color, encode bool) render.Color {
	if encode {
		red, green, blue, alpha := color.Srgb()
		color = m.Color{R: red, G: green, B: blue, A: alpha}
	}
	return render.Color{R: float64(color.R * color.A), G: float64(color.G * color.A), B: float64(color.B * color.A), A: float64(color.A)}
}
