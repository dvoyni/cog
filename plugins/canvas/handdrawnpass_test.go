package canvas_test

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// handDrawnCamera mirrors canvas.wgsl's CanvasCamera, a camera pass's
// parameters, 80 bytes.
type handDrawnCamera struct {
	worldToClip, clipToWorld     m.Mat2x3
	pixelSize, snap              float32
	viewportOrigin, viewportSize [2]float32
	encode                       uint32
	_                            uint32
}

// handDrawnPass is one camera's pass of the hand-drawn frame: its order,
// its viewport, whether it clears by its load, and its parameters.
type handDrawnPass struct {
	order    render.Order
	viewport render.Viewport
	clear    bool
	camera   handDrawnCamera
}

// makeHandDrawnPasses answers the passes of a window of size through
// cameraCount cameras, as makeGateCameras places them, each showing the
// world about its origin at a unit a pixel: the whole window, or count-1
// strips of order 0 under the whole window at order 1, at twice the zoom.
func makeHandDrawnPasses(size platform.Size, cameraCount int) []handDrawnPass {
	makePass := func(order render.Order, x, width, zoom float32, clear bool) handDrawnPass {
		height := float32(size.Height)
		worldToClip := m.Scaling2x3(2*zoom/width, -2*zoom/height)
		clipToWorld, _ := worldToClip.Inverse()
		return handDrawnPass{
			order:    order,
			viewport: render.Viewport{X: x, Width: width, Height: height, MaxDepth: 1},
			clear:    clear,
			camera:   handDrawnCamera{worldToClip: worldToClip, clipToWorld: clipToWorld, pixelSize: 1 / zoom, viewportOrigin: [2]float32{x, 0}, viewportSize: [2]float32{width, height}},
		}
	}
	if cameraCount == 1 {
		return []handDrawnPass{makePass(0, 0, float32(size.Width), 1, true)}
	}
	var passes []handDrawnPass
	stripCount := cameraCount - 1
	stripWidth := float32(size.Width) / float32(stripCount)
	for index := range stripCount {
		passes = append(passes, makePass(0, float32(index)*stripWidth, stripWidth, 1, false))
	}
	return append(passes, makePass(1, 0, float32(size.Width), 2, false))
}
