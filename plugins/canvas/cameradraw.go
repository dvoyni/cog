package canvas

import (
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// cameraDraw is one camera of a frame as it draws: the camera as set; its
// plan, how it draws into its target; and this frame's work beside it, the
// entries it draws, those of the frame's sorted entries its layers hold
// that it does not cull, in sort order, part of the renderer's; its
// batches, part of the renderer's; and, for a quad its plan draws over its
// viewport, its instance in its command list's instance data.
type cameraDraw struct {
	camera           Camera
	plan             cameraPlan
	entries          []sortedEntry
	batches          []batch
	viewportInstance uint32
}

// makeCameraDraw answers how camera draws, its entries and batches still
// to find, and whether it draws at all: its target measured, then its plan
// decided, a pixel-perfect camera's FitStretch, drawn as FitInscribe,
// reported once. A camera whose target is not measured, or with no view, as
// of an empty viewport, draws nothing.
func makeCameraDraw(inventory render.Inventory, manager *platform.WindowManager, mainWindow platform.Window, reporter *reporter, camera *Camera) (cameraDraw, bool) {
	target, ok := makeDrawnTarget(inventory, manager, mainWindow, reporter, camera)
	if !ok {
		return cameraDraw{}, false
	}
	plan, ok := makeCameraPlan(camera, target)
	if !ok {
		return cameraDraw{}, false
	}
	if camera.PixelPerfect && camera.Fit == FitStretch {
		reporter.reportOnce(pixelPerfectStretchReport.Error(), pixelPerfectStretchReport)
	}
	return cameraDraw{camera: *camera, plan: plan}, true
}
