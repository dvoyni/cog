package internal

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// PresentCmd finalizes the writable OpQueue: it swaps it into the internal ready
// slot (dropping any still-unconsumed queue, latest-wins) and installs a reset
// queue for further recording. The plugin also runs this last on app.UpdateEvent.
type PresentCmd kernel.Command[PresentRequest, PresentResponse]

type PresentRequest struct{}

type PresentResponse struct{}

// AcquireCmd advances the internal read queue to the latest completed queue if
// one is pending, otherwise leaves it unchanged.
type AcquireCmd kernel.Command[AcquireRequest, AcquireResponse]

type AcquireRequest struct{}

type AcquireResponse struct{ Advanced bool }

// ReleaseCachedResourceCmd queues release of translator-owned texture and
// shader caches matching Path. Cleanup runs on the render thread before the
// latest frame; a later use of the path loads it again.
//
// A shader matches on membership rather than on its name: every module whose
// flatten read Path goes, whether Path rooted it or was included into it. So a
// shader built from inline text goes too, if it included the path - but one
// built from inline text with no #include has no source to match and no path
// this command can name. FreeCachedResourcesCmd is its only release, and no
// name is invented to give it a second one: a path for the one thing defined by
// not having one would be a sentinel inside a namespace of real paths.
//
// It is also the only retry there is. A read that failed is cached as failed and
// reported once, so a path whose file was missing stays missing as far as gfx is
// concerned until this command drops the entry - which also forgets the report,
// so the next attempt can speak again.
type ReleaseCachedResourceCmd kernel.Command[ReleaseCachedResourceRequest, ReleaseCachedResourceResponse]

type ReleaseCachedResourceRequest struct{ Path string }

type ReleaseCachedResourceResponse struct{}

// FreeCachedResourcesCmd queues release of every translator-owned texture,
// shader, pipeline, sampler, layout, and parameter plan. Explicit resources
// returned by ResourceQueue.Bake* remain caller-owned and are not released.
type FreeCachedResourcesCmd kernel.Command[FreeCachedResourcesRequest, FreeCachedResourcesResponse]

type FreeCachedResourcesRequest struct{}

type FreeCachedResourcesResponse struct{}

// SetViewportCmd updates the Viewport resource with the current render target
// size. A driver calls it when the size changes.
type SetViewportCmd kernel.Command[SetViewportRequest, SetViewportResponse]

// SetViewportRequest is the request for SetViewportCmd: the window size in
// device-independent pixels plus the physical framebuffer size.
type SetViewportRequest struct {
	Width, Height                       float32
	FramebufferWidth, FramebufferHeight float32
}

// SetViewportResponse reports the resolved logical and window dimensions.
type SetViewportResponse struct{ Viewport types.Viewport }

// SetDesiredViewportCmd selects the logical world-size policy. A game normally
// calls it once during initialization; the handler resolves it against each
// physical window size supplied through SetViewportCmd.
type SetDesiredViewportCmd kernel.Command[SetDesiredViewportRequest, SetDesiredViewportResponse]

// SetDesiredViewportRequest selects the logical viewport policy. Size is used by
// ViewportFixedWidth and ViewportFixedHeight. Width and Height define the desired
// rectangle for ViewportFit and ViewportCover. Invalid values fall back to
// ViewportWindow.
type SetDesiredViewportRequest struct {
	Mode          types.ViewportMode
	Width, Height float32
	Size          float32
}

// SetDesiredViewportResponse reports the viewport resolved against the most
// recently supplied window size.
type SetDesiredViewportResponse struct{ Viewport types.Viewport }

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
	Done <-chan Capture
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
