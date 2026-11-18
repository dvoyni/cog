package canvas

import (
	"fmt"

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

// makeCameraBlock answers the pass parameters of a camera drawing into
// viewport, in its target's pixels and not empty, each pixel pixelSize
// world units across, its output encoded as sRGB when encode is set, for a
// target whose format encodes nothing: the world's origin at the viewport's
// centre, y down.
func makeCameraBlock(viewport m.Recti, pixelSize float32, encode bool) cameraBlock {
	if viewport.Width <= 0 || viewport.Height <= 0 || !(pixelSize > 0) {
		panic(fmt.Sprintf("canvas: a camera's viewport is %+v pixels, each %v world units across, neither of which may be zero", viewport, pixelSize))
	}
	worldToClip := m.Scaling2x3(2/(float32(viewport.Width)*pixelSize), -2/(float32(viewport.Height)*pixelSize))
	clipToWorld, _ := worldToClip.Inverse()
	block := cameraBlock{
		worldToClip:    worldToClip,
		clipToWorld:    clipToWorld,
		pixelSize:      pixelSize,
		viewportOrigin: [2]float32{float32(viewport.X), float32(viewport.Y)},
		viewportSize:   [2]float32{float32(viewport.Width), float32(viewport.Height)},
	}
	if encode {
		block.encode = 1
	}
	return block
}
