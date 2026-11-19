package canvas

import (
	"fmt"
	"math"

	"github.com/dvoyni/cog/libs/m"
)

// cameraBlock mirrors canvas.wgsl's CanvasCamera, the pass parameters of a
// camera's pass: 80 bytes, WGSL's size of the struct, which aligns to 8.
type cameraBlock struct {
	worldToClip, clipToWorld     m.Mat2x3
	pixelSize, snap              float32
	viewportOrigin, viewportSize [2]float32
	encode                       uint32
	_                            uint32
}

// makeCameraBlock answers the pass parameters of a camera drawing through
// view, its viewport not empty, its output encoded as sRGB when encode is
// set, for a target whose format encodes nothing: world_to_clip maps the
// world through the view onto the viewport's clip space, and pixel_size is
// a pixel in world units along the view's axis of the smaller scale, so a
// hairline never thins below a pixel; snap is the view's grid.
func makeCameraBlock(view *cameraView, encode bool) cameraBlock {
	viewport := view.viewport
	if viewport.Width <= 0 || viewport.Height <= 0 {
		panic(fmt.Sprintf("canvas: a camera's viewport is %+v pixels, which may not be empty", viewport))
	}
	width := float32(viewport.Width)
	height := float32(viewport.Height)
	// pixelToClip maps the viewport's pixels onto clip space, y up.
	pixelToClip := m.Translation2x3(-1-2*float32(viewport.X)/width, 1+2*float32(viewport.Y)/height).Mul(m.Scaling2x3(2/width, -2/height))
	worldToClip := pixelToClip.Mul(view.worldToPixel)
	clipToWorld, _ := worldToClip.Inverse()
	block := cameraBlock{
		worldToClip:    worldToClip,
		clipToWorld:    clipToWorld,
		pixelSize:      float32(1 / min(math.Abs(float64(view.scale.X)), math.Abs(float64(view.scale.Y)))),
		snap:           view.snap,
		viewportOrigin: [2]float32{float32(viewport.X), float32(viewport.Y)},
		viewportSize:   [2]float32{width, height},
	}
	if encode {
		block.encode = 1
	}
	return block
}
