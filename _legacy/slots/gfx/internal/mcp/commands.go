package mcp

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx/internal"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// MaxCaptureAmount caps one request at sixty stills, and MaxCaptureSpan caps
// the ticks they span. The interval is what buys a long window, never the
// amount. The
// span figure is app.TimeRequest's step cap, deliberately the same number so
// there is one figure to remember.
const (
	MaxCaptureAmount = 60
	MaxCaptureSpan   = 600
)

// ArmCaptureCmd arms a readback of one colour target and hands back the wait.
// It is ordinary gfx API: anything holding a kernel handle may arm a capture,
// and the agent-facing capability is one caller among them.
//
// The response's channel is the only delivery path, and refusals travel it too,
// because Capture carries Err. The alternative - a channel passed in with
// the request - leaves gfx unable to refuse a second arm synchronously.
type ArmCaptureCmd kernel.Command[ArmCaptureRequest, ArmCaptureResponse]

// ArmCaptureRequest names the target and how many stills to take of it.
type ArmCaptureRequest struct {
	// Target is what to read back: the frame buffer, or any colour texture the
	// frame rendered into.
	Target types.CaptureDesc
	// Amount is how many stills to write; zero means one, and the maximum is
	// sixty.
	Amount int
	// Interval is how many ticks apart the stills are; zero means one.
	// Amount x Interval may not exceed six hundred ticks.
	Interval int
	// Paused says the caller knows the engine's tick source is stopped, so no
	// tick can begin after this request and the last completed tick already is
	// the present. The handler does not read the tick source itself: the
	// caller asks app with app.TimeCmd, as gfx_capture does, and says what it
	// was told.
	//
	// A paused capture is served from the next render and costs no tick, which
	// is what makes two captures taken under one pause byte-identical. A burst
	// is refused while paused, because there would be nothing new to photograph.
	Paused bool
}

// ArmCaptureResponse hands back the wait and the window size.
type ArmCaptureResponse struct {
	// Done receives one Capture per still, in order, and is buffered to
	// Amount so the render thread never blocks on a caller that walked away.
	Done <-chan internal.Capture
	// Viewport is the window as of the arm, which a capability body cannot read
	// for itself. The pixel dimensions always come from the capture, so a
	// window resized inside the capture's two-frame window reports a stale
	// window size but never mis-describes the image.
	Viewport types.Viewport
	// Err is the outcome an arm was refused with: a request the capture state
	// would not take, or one that arrived while another was already armed.
	Err error
}

// ArmFrameCmd arms one frame snapshot and hands back the wait. It is ordinary
// gfx API: anything holding a kernel handle may ask what the renderer was told
// to do for a tick, and the agent-facing capability is one caller among them.
//
// The response's channel is the only delivery path, and refusals travel it too,
// because FrameSnapshot carries Err - the same shape ArmCaptureCmd uses, for
// the same reason: a channel passed in with the request would leave gfx unable
// to refuse a second arm synchronously.
type ArmFrameCmd kernel.Command[ArmFrameRequest, ArmFrameResponse]

// ArmFrameRequest carries the filter, because the filter is what bounds the
// work done inside the tick. Nothing about the output is decided before the
// request is known.
type ArmFrameRequest struct {
	// Pass, when set, keeps only the passes whose Label is exactly it. Passes
	// it drops are counted in the result rather than silently missing, and
	// every pass keeps its own declaration index, so an index read off a
	// filtered snapshot still addresses the same pass in an unfiltered one.
	Pass string
}

// ArmFrameResponse hands back the wait and the viewport.
type ArmFrameResponse struct {
	// Done receives exactly one FrameSnapshot and is buffered, so the game's
	// own goroutine never blocks on a caller that walked away.
	Done <-chan FrameSnapshot
	// Viewport is the window as of the arm, which a capability body cannot
	// read for itself. A resize between the arm and the tick it binds to is a
	// stated non-guarantee, exactly as it is for a capture.
	Viewport types.Viewport
	// Err is the outcome an arm was refused with, exactly as it is for a
	// capture.
	Err error
}

// FrameSnapshot is one produced snapshot, or the reason there is none. One
// struct carries both so that a caller cannot handle one and forget the other.
type FrameSnapshot struct {
	Frame internal.FrameView
	// Tick is app.UpdateEvent.Tick of the tick the snapshot was taken in. It
	// travels with the snapshot rather than being asked for afterwards,
	// because only the tick itself knows which one it was.
	Tick int64
	Err  error
}
