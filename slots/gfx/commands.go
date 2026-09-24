package gfx

import "github.com/dvoyni/cog/slots/gfx/internal"

// PresentCmd finalizes the writable OpQueue: it swaps it into the internal ready
// slot (dropping any still-unconsumed queue, latest-wins) and installs a reset
// queue for further recording. The plugin also runs this last on app.UpdateEvent.
type PresentCmd = internal.PresentCmd

type PresentRequest = internal.PresentRequest

type PresentResponse = internal.PresentResponse

// AcquireCmd advances the internal read queue to the latest completed queue if
// one is pending, otherwise leaves it unchanged.
type AcquireCmd = internal.AcquireCmd

type AcquireRequest = internal.AcquireRequest

type AcquireResponse = internal.AcquireResponse

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
type ReleaseCachedResourceCmd = internal.ReleaseCachedResourceCmd

type ReleaseCachedResourceRequest = internal.ReleaseCachedResourceRequest

type ReleaseCachedResourceResponse = internal.ReleaseCachedResourceResponse

// FreeCachedResourcesCmd queues release of every translator-owned texture,
// shader, pipeline, sampler, layout, and parameter plan. Explicit resources
// returned by ResourceQueue.Bake* remain caller-owned and are not released.
type FreeCachedResourcesCmd = internal.FreeCachedResourcesCmd

type FreeCachedResourcesRequest = internal.FreeCachedResourcesRequest

type FreeCachedResourcesResponse = internal.FreeCachedResourcesResponse

// SetViewportCmd updates the Viewport resource with the current render target
// size. A driver calls it when the size changes.
type SetViewportCmd = internal.SetViewportCmd

// SetViewportRequest is the request for SetViewportCmd: the window size in
// device-independent pixels plus the physical framebuffer size.
type SetViewportRequest = internal.SetViewportRequest

// SetViewportResponse reports the resolved logical and window dimensions.
type SetViewportResponse = internal.SetViewportResponse

// SetDesiredViewportCmd selects the logical world-size policy. A game normally
// calls it once during initialization; the handler resolves it against each
// physical window size supplied through SetViewportCmd.
type SetDesiredViewportCmd = internal.SetDesiredViewportCmd

// SetDesiredViewportRequest selects the logical viewport policy. Size is used by
// ViewportFixedWidth and ViewportFixedHeight. Width and Height define the desired
// rectangle for ViewportFit and ViewportCover. Invalid values fall back to
// ViewportWindow.
type SetDesiredViewportRequest = internal.SetDesiredViewportRequest

// SetDesiredViewportResponse reports the viewport resolved against the most
// recently supplied window size.
type SetDesiredViewportResponse = internal.SetDesiredViewportResponse

// ArmCaptureCmd arms a readback of one colour target and hands back the wait.
// It is ordinary gfx API: anything holding a kernel handle may arm a capture,
// and the agent-facing capability is one caller among them.
//
// The response's channel is the only delivery path, and refusals travel it too,
// because Capture carries Err. The alternative - a channel passed in with
// the request - leaves gfx unable to refuse a second arm synchronously.
type ArmCaptureCmd = internal.ArmCaptureCmd

// ArmCaptureRequest names the target and how many stills to take of it.
type ArmCaptureRequest = internal.ArmCaptureRequest

// ArmCaptureResponse hands back the wait and the window size.
type ArmCaptureResponse = internal.ArmCaptureResponse

// ArmFrameCmd arms one frame snapshot and hands back the wait. It is ordinary
// gfx API: anything holding a kernel handle may ask what the renderer was told
// to do for a tick, and the agent-facing capability is one caller among them.
//
// The response's channel is the only delivery path, and refusals travel it too,
// because FrameSnapshot carries Err - the same shape ArmCaptureCmd uses, for
// the same reason: a channel passed in with the request would leave gfx unable
// to refuse a second arm synchronously.
type ArmFrameCmd = internal.ArmFrameCmd

// ArmFrameRequest carries the filter, because the filter is what bounds the
// work done inside the tick. Nothing about the output is decided before the
// request is known.
type ArmFrameRequest = internal.ArmFrameRequest

// ArmFrameResponse hands back the wait and the viewport.
type ArmFrameResponse = internal.ArmFrameResponse
