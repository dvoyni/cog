package canvas

import (
	"fmt"

	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// drawnTarget is one target as a camera drew into it: its window or its
// texture, the other zero; its size in pixels; its pixels to a point, 1
// for a texture; and its format, a window's taken as BGRA8Unorm.
type drawnTarget struct {
	window         platform.Window
	texture        render.Texture
	width, height  int
	pixelsPerPoint float32
	format         render.TextureFormat
}

// makeDrawnTarget answers camera's target, the texture it names or else its
// window or mainWindow, measured from render's descriptor of the texture or
// from manager's sizes of the window, a window's format taken as
// BGRA8Unorm, and whether it is measured: a window not open or not sized is
// not, and neither is a target refused, a camera naming both a window and a
// texture, or a texture render does not hold, each reported once.
func makeDrawnTarget(inventory render.Inventory, manager *platform.WindowManager, mainWindow platform.Window, reporter *reporter, camera *Camera) (drawnTarget, bool) {
	switch {
	case camera.Texture != (render.Texture{}) && camera.Window != (platform.Window{}):
		refusal := Report{Kind: ReportKindCameraTargetRefused, Err: fmt.Errorf("canvas: a camera names both window %v and texture %v as its target", camera.Window, camera.Texture)}
		reporter.reportOnce(refusal.Error(), refusal)
		return drawnTarget{}, false
	case camera.Texture != (render.Texture{}):
		descriptor, held := inventory.TextureDescriptor(camera.Texture)
		if !held {
			refusal := Report{Kind: ReportKindCameraTargetRefused, Err: fmt.Errorf("canvas: a camera's target, texture %v, is one render does not hold", camera.Texture)}
			reporter.reportOnce(refusal.Error(), refusal)
			return drawnTarget{}, false
		}
		return drawnTarget{texture: camera.Texture, width: int(descriptor.Width), height: int(descriptor.Height), pixelsPerPoint: 1, format: descriptor.Format}, true
	}
	window := camera.Window
	if window == (platform.Window{}) {
		window = mainWindow
	}
	if window == (platform.Window{}) {
		return drawnTarget{}, false
	}
	framebufferSize := manager.FramebufferSize(window)
	scale := manager.Scale(window)
	if framebufferSize.Width <= 0 || framebufferSize.Height <= 0 || !(scale > 0) {
		return drawnTarget{}, false
	}
	return drawnTarget{window: window, width: framebufferSize.Width, height: framebufferSize.Height, pixelsPerPoint: scale, format: render.TextureFormatBGRA8Unorm}, true
}
