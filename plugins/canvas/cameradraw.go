package canvas

import (
	"fmt"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// multisampledCameraSampleCount is the samples of a multisampled camera,
// the one count above 1 a Camera's SampleCount takes.
const multisampledCameraSampleCount = 4

// cameraDraw is one camera of a frame as it draws: the camera as set; its
// target as it draws into it, its texture or else its window, the main
// window when it named neither; the colour target of its entries' pass,
// that target, cleared by the pass's load when it clears a viewport
// covering the target, or a multisampled camera's texture of
// multisampledDescriptor, cleared whole by the load, discarded and
// resolved, the two textures named once acquired; colorTargetOrigin, where
// that colour target's top left lies in the target, the viewport's for a
// multisampled camera; its view into the target and its entries' pass parameters; the
// entries it draws, those of the frame's sorted entries its layers hold
// that it does not cull, in sort order, part of the renderer's; its
// batches, part of the renderer's; and, for a quad drawn over its viewport,
// a clear or what a multisampled camera resolved, its instance in its
// command list's instance data.
type cameraDraw struct {
	camera                 Camera
	target                 drawnTarget
	colorTarget            render.ColorTarget
	multisampledDescriptor render.TextureDescriptor
	colorTargetOrigin      m.Vec2i
	view                   cameraView
	block                  cameraBlock
	entries                []sortedEntry
	batches                []batch
	viewportInstance       uint32
}

// makeCameraDraw answers how camera draws, its entries and batches still
// to find, and whether it draws at all: its target, the texture it names or
// else its window or mainWindow, measured from render's descriptor of the
// texture or from manager's sizes of the window, its output encoded
// wherever the target's format encodes nothing, as a window's BGRA8Unorm
// framebuffer does not; and its view into the target. A camera into a
// window not open or not sized, or with no view, as of an empty viewport,
// draws nothing; one naming both a window and a texture, or a texture
// render does not hold, is reported once too, as is a
// pixel-perfect camera's FitStretch, drawn as FitInscribe. A multisampled
// camera's entries draw into a texture of its viewport's size in its
// target's format, a window's taken as BGRA8Unorm, the viewport at the
// texture's origin.
func makeCameraDraw(inventory render.Inventory, manager *platform.WindowManager, mainWindow platform.Window, reporter *reporter, camera *Camera) (cameraDraw, bool) {
	drawnCamera := cameraDraw{camera: *camera}
	format := render.TextureFormatBGRA8Unorm
	switch {
	case camera.Texture != (render.Texture{}) && camera.Window != (platform.Window{}):
		refusal := Report{Kind: ReportKindCameraTargetRefused, Err: fmt.Errorf("canvas: a camera names both window %v and texture %v as its target", camera.Window, camera.Texture)}
		reporter.reportOnce(refusal.Error(), refusal)
		return cameraDraw{}, false
	case camera.Texture != (render.Texture{}):
		descriptor, held := inventory.TextureDescriptor(camera.Texture)
		if !held {
			refusal := Report{Kind: ReportKindCameraTargetRefused, Err: fmt.Errorf("canvas: a camera's target, texture %v, is one render does not hold", camera.Texture)}
			reporter.reportOnce(refusal.Error(), refusal)
			return cameraDraw{}, false
		}
		drawnCamera.target = drawnTarget{texture: camera.Texture, width: int(descriptor.Width), height: int(descriptor.Height), pixelsPerPoint: 1}
		format = descriptor.Format
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
	if camera.PixelPerfect && camera.Fit == FitStretch {
		reporter.reportOnce(pixelPerfectStretchReport.Error(), pixelPerfectStretchReport)
	}
	drawnCamera.view = view
	encode := format != render.TextureFormatRGBA8UnormSrgb && format != render.TextureFormatBGRA8UnormSrgb
	multisampled := camera.SampleCount == multisampledCameraSampleCount
	if multisampled {
		drawnCamera.colorTargetOrigin = view.viewport.Min()
	}
	drawnCamera.block = makeCameraBlock(&view, encode, drawnCamera.colorTargetOrigin)
	if !multisampled {
		drawnCamera.colorTarget = drawnCamera.makeTargetColorTarget()
		return drawnCamera, true
	}
	drawnCamera.colorTarget = render.ColorTarget{Clear: true, Discard: true}
	if camera.Clear {
		drawnCamera.colorTarget.ClearColor = makeLoadedClearColor(camera.ClearColor, encode)
	}
	drawnCamera.multisampledDescriptor = render.TextureDescriptor{
		Label:       "canvas multisampled camera",
		Width:       uint32(view.viewport.Width),
		Height:      uint32(view.viewport.Height),
		SampleCount: multisampledCameraSampleCount,
		Format:      format,
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

// makeTargetColorTarget answers the colour target of the camera's target as
// a pass draws into it: cleared by the pass's load when the camera clears a
// viewport covering the target, whose colour does not matter to a
// multisampled camera's composite, which then replaces all of it, and kept
// otherwise.
func (d *cameraDraw) makeTargetColorTarget() render.ColorTarget {
	colorTarget := render.ColorTarget{Window: d.target.window, Texture: d.target.texture}
	if d.camera.Clear && d.view.viewport == (m.Recti{Width: d.target.width, Height: d.target.height}) {
		colorTarget.Clear = true
		colorTarget.ClearColor = makeLoadedClearColor(d.camera.ClearColor, d.block.encode != 0)
	}
	return colorTarget
}
