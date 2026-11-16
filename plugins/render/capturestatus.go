package render

import "github.com/dvoyni/cog/plugins/gfx"

// CaptureStatus is how a capture ended, carried by CapturedEvent, each
// status gfx's of the same name.
type CaptureStatus uint8

const (
	// CaptureStatusCaptured is a window's picture, for Frame.ReadCapture.
	CaptureStatusCaptured = CaptureStatus(gfx.CaptureStatusCaptured)
	// CaptureStatusNotAcquired is no picture: the window acquired no
	// framebuffer in the frame the capture was taken from, being hidden,
	// outdated or timed out.
	CaptureStatusNotAcquired = CaptureStatus(gfx.CaptureStatusNotAcquired)
	// CaptureStatusFailed is no picture: the copy or its map failed, or the
	// device was lost while the capture was pending; the error was logged.
	CaptureStatusFailed = CaptureStatus(gfx.CaptureStatusFailed)
)
