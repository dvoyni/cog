package canvas

import (
	"fmt"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/platform"
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

// makeDefaultCameraBlock answers the pass parameters of the default camera
// in a window of size, in points, whose framebuffer is framebufferSize, in
// pixels, neither of them zero: the world's origin at the window's centre,
// one world unit to a point and y down, the whole framebuffer its viewport,
// and its output encoded as sRGB, since a window's framebuffer is
// BGRA8Unorm, which encodes nothing.
func makeDefaultCameraBlock(size, framebufferSize platform.Size) cameraBlock {
	if size.Width <= 0 || size.Height <= 0 || framebufferSize.Width <= 0 || framebufferSize.Height <= 0 {
		panic(fmt.Sprintf("canvas: the default camera's window is %v points with a framebuffer of %v pixels, which must not be zero", size, framebufferSize))
	}
	worldToClip := m.Scaling2x3(2/float32(size.Width), -2/float32(size.Height))
	clipToWorld, _ := worldToClip.Inverse()
	return cameraBlock{
		worldToClip:  worldToClip,
		clipToWorld:  clipToWorld,
		pixelSize:    float32(size.Width) / float32(framebufferSize.Width),
		viewportSize: [2]float32{float32(framebufferSize.Width), float32(framebufferSize.Height)},
		encode:       1,
	}
}
