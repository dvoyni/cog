package canvas

import (
	"fmt"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// cameraDraw is one camera of a frame as it draws: the camera as set; its
// pass's colour target, its texture or else its window, the main window when
// it named neither, cleared by the pass's load when it clears a viewport
// covering the target; its viewport in the target's pixels and its pass
// parameters; the runs of the frame's sorted entries its layers hold; its
// batches, part of the renderer's; and, for a clear drawn over its
// viewport, its instance in its command list's instance data.
type cameraDraw struct {
	camera        Camera
	colorTarget   render.ColorTarget
	viewport      m.Recti
	block         cameraBlock
	layerRuns     [4][]sortedEntry
	batches       []batch
	clearInstance uint32
}

// makeCameraDraw answers how camera draws, its layer runs and batches still
// to find, and whether it draws at all: its target, the texture it names or
// else its window or mainWindow, sized from render's descriptor of the
// texture or from manager's sizes of the window, a texture's pixel one world
// unit and a window's point one, encoded wherever the target's format
// encodes nothing, as a window's BGRA8Unorm framebuffer does not; and its
// viewport placed in the target. A camera into a window not open or not
// sized, or of an empty viewport, draws nothing; one naming both a window
// and a texture, or a texture render does not hold, is reported once to
// logger too.
func makeCameraDraw(inventory render.Inventory, manager *platform.WindowManager, mainWindow platform.Window, logger *kernel.Logger, camera *Camera) (cameraDraw, bool) {
	drawnCamera := cameraDraw{camera: *camera}
	var width, height int
	var pixelSize float32
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
		drawnCamera.colorTarget.Texture = camera.Texture
		width = int(descriptor.Width)
		height = int(descriptor.Height)
		pixelSize = 1
		encode = descriptor.Format != render.TextureFormatRGBA8UnormSrgb && descriptor.Format != render.TextureFormatBGRA8UnormSrgb
	default:
		window := camera.Window
		if window == (platform.Window{}) {
			window = mainWindow
		}
		if window == (platform.Window{}) {
			return cameraDraw{}, false
		}
		size := manager.Size(window)
		framebufferSize := manager.FramebufferSize(window)
		if size.Width <= 0 || size.Height <= 0 || framebufferSize.Width <= 0 || framebufferSize.Height <= 0 {
			return cameraDraw{}, false
		}
		drawnCamera.colorTarget.Window = window
		width = framebufferSize.Width
		height = framebufferSize.Height
		pixelSize = float32(size.Width) / float32(framebufferSize.Width)
	}
	drawnCamera.viewport = camera.placeViewport(width, height)
	if drawnCamera.viewport.Width == 0 || drawnCamera.viewport.Height == 0 {
		return cameraDraw{}, false
	}
	drawnCamera.block = makeCameraBlock(drawnCamera.viewport, pixelSize, encode)
	if camera.Clear && drawnCamera.viewport == (m.Recti{Width: width, Height: height}) {
		drawnCamera.colorTarget.Clear = true
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
