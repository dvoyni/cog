package gpu

import (
	"fmt"
	"image"
)

// Capture is one completed readback: either the mapped bytes or the reason
// there are none. Pixels carries the GPU's own row padding, which BytesPerRow
// describes and Image removes; a backend never sees an image.Image.
//
// One struct carries success and failure so that a caller cannot handle one and
// forget the other, which is how a capture that never arrives becomes a hang
// somewhere far away.
type Capture struct {
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
func (c Capture) Image() image.Image {
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

// ErrCaptureBusy reports a capture arm made while one is already live. It is
// refused rather than queued or coalesced: queueing turns a boolean into a
// queue for a fifty-millisecond window, and coalescing two waiters onto one
// frame founders on the fact that each names a different file.
//
// Both refusal sites raise it. gfx refuses a second arm synchronously, and the
// backend refuses a second in-flight map through Capture.Err, because capture
// is a public gfx feature and a game's own code may arm one.
type ErrCaptureBusy struct{}

func (ErrCaptureBusy) Error() string {
	return "gfx: a capture is already in flight"
}

// ErrCaptureAbandoned reports a capture the engine stopped before its readback
// resolved. It travels the channel a result would have used, so that a waiter
// learns the answer rather than sitting until its own deadline.
type ErrCaptureAbandoned struct{}

func (ErrCaptureAbandoned) Error() string {
	return "gfx: the engine stopped before the capture was read back"
}

// ErrCaptureUnsupported reports a target a capture cannot be an image of:
// depth, or any format that is not 8-bit RGBA. Depth readback is a real want
// and it is a visualization question rather than a readback one - a depth
// capture is a float field needing a range to be legible.
type ErrCaptureUnsupported struct{ Format TextureFormat }

func (e ErrCaptureUnsupported) Error() string {
	return fmt.Sprintf("gfx: %s cannot be captured; a capture is 8-bit RGBA", e.Format.Name())
}

// ErrCaptureNoTarget reports a capture of something the frame never rendered
// into: a screen capture in a frame that drew nothing to the screen, or a
// texture that has no GPU object yet. It is distinct from an unsupported
// format because the answers differ - one says wait for a frame that draws,
// the other says this can never be an image.
type ErrCaptureNoTarget struct{}

func (ErrCaptureNoTarget) Error() string {
	return "gfx: the capture's target was not rendered into this frame"
}
