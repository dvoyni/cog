package render

import "github.com/dvoyni/cog/gfx"

// PresentMode is how a window's surface hands its framebuffers to the
// display, each mode gfx's of the same name.
type PresentMode uint32

const (
	// PresentModeUndefined is the zero mode, which a SurfaceRequest reads as
	// PresentModeFifo.
	PresentModeUndefined = PresentMode(gfx.PresentModeUndefined)
	// PresentModeFifo waits for the display, which paces the frames: vsync.
	// Every backend has it.
	PresentModeFifo = PresentMode(gfx.PresentModeFifo)
	// PresentModeFifoRelaxed is Fifo that tears when a frame is late; on
	// Metal it turns vsync off.
	PresentModeFifoRelaxed = PresentMode(gfx.PresentModeFifoRelaxed)
	// PresentModeImmediate presents at once and may tear.
	PresentModeImmediate = PresentMode(gfx.PresentModeImmediate)
	// PresentModeMailbox replaces the queued framebuffer and does not tear;
	// on Metal it is Immediate.
	PresentModeMailbox = PresentMode(gfx.PresentModeMailbox)
)
