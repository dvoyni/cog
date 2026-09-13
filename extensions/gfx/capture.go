package gfx

import (
	"image"

	"github.com/dvoyni/cog/kernel"
)

// GpuCapture is one completed readback: either the mapped bytes or the reason
// there are none. Pixels carries the GPU's own row padding, which BytesPerRow
// describes and Image removes; a backend never sees an image.Image.
//
// One struct carries success and failure so that a caller cannot handle one and
// forget the other, which is how a capture that never arrives becomes a hang
// somewhere far away.
type GpuCapture struct {
	Pixels        []byte
	Width, Height int
	Format        TextureFormat
	BytesPerRow   int
	Err           error
}

// Image un-strides the captured bytes into an image. It reports nil for a
// failed capture and for any format that is not 8-bit RGBA, which is every
// format a backend is allowed to hand back.
//
// image.NRGBA rather than image.RGBA: FormatRGBA8 is straight-alpha, and
// image.RGBA is premultiplied, so the obvious type would misread every
// translucent pixel. No colour conversion happens here in either case - an
// sRGB frame buffer's bytes are already what a PNG wants.
func (c GpuCapture) Image() image.Image {
	if c.Err != nil || !captureFormatSupported(c.Format) {
		return nil
	}
	if c.Width <= 0 || c.Height <= 0 {
		return nil
	}
	row := c.Width * 4
	if c.BytesPerRow < row || len(c.Pixels) < (c.Height-1)*c.BytesPerRow+row {
		return nil
	}
	picture := image.NewNRGBA(image.Rect(0, 0, c.Width, c.Height))
	for y := range c.Height {
		src := y * c.BytesPerRow
		dst := y * picture.Stride
		copy(picture.Pix[dst:dst+row], c.Pixels[src:src+row])
	}
	return picture
}

// captureFormatSupported reports whether a format is the 8-bit RGBA a capture
// can be an image of. Depth is not: it is a float field needing a range to be
// legible, which is a visualization question rather than a readback one.
func captureFormatSupported(format TextureFormat) bool {
	switch format.Resolve() {
	case FormatRGBA8, FormatRGBA8Srgb:
		return true
	default:
		return false
	}
}

// ArmCaptureCmd arms a readback of one colour target and hands back the wait.
// It is ordinary gfx API: anything holding a kernel handle may arm a capture,
// and the agent-facing capability is one caller among them.
//
// The response's channel is the only delivery path, and refusals travel it too,
// because GpuCapture carries Err. The alternative - a channel passed in with
// the request - leaves gfx unable to refuse a second arm synchronously.
type ArmCaptureCmd kernel.Command[ArmCaptureRequest, ArmCaptureResponse]

// ArmCaptureRequest names the target and how many stills to take of it.
type ArmCaptureRequest struct {
	// Target is what to read back: the frame buffer, or any colour texture the
	// frame rendered into.
	Target GpuCaptureDesc
	// Amount is how many stills to write; zero means one, and the maximum is
	// sixty.
	Amount int
	// Interval is how many ticks apart the stills are; zero means one.
	// Amount x Interval may not exceed six hundred ticks.
	Interval int
	// Paused says the caller knows the engine's tick source is stopped, so no
	// tick can begin after this request and the last completed tick already is
	// the present. gfx does not read the tick source itself: pausing belongs to
	// the host that owns the loop, and gfx must not require a host to exist.
	//
	// A paused capture is served from the next render and costs no tick, which
	// is what makes two captures taken under one pause byte-identical. A burst
	// is refused while paused, because there would be nothing new to photograph.
	Paused bool
}

// ArmCaptureResponse hands back the wait and the window size.
type ArmCaptureResponse struct {
	// Done receives one GpuCapture per still, in order, and is buffered to
	// Amount so the render thread never blocks on a caller that walked away.
	Done <-chan GpuCapture
	// Viewport is the window as of the arm, which a capability body cannot read
	// for itself. The pixel dimensions always come from the capture, so a
	// window resized inside the capture's two-frame window reports a stale
	// window size but never mis-describes the image.
	Viewport Viewport
}
