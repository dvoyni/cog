package gfx

import "github.com/dvoyni/cog/kernel"

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
type SetViewportResponse struct{ Viewport Viewport }

// SetDesiredViewportCmd selects the logical world-size policy. A game normally
// calls it once during initialization; the handler resolves it against each
// physical window size supplied through SetViewportCmd.
type SetDesiredViewportCmd kernel.Command[SetDesiredViewportRequest, SetDesiredViewportResponse]

// SetDesiredViewportRequest selects the logical viewport policy. Size is used by
// ViewportFixedWidth and ViewportFixedHeight. Width and Height define the desired
// rectangle for ViewportFit and ViewportCover. Invalid values fall back to
// ViewportWindow.
type SetDesiredViewportRequest struct {
	Mode          ViewportMode
	Width, Height float32
	Size          float32
}

// SetDesiredViewportResponse reports the viewport resolved against the most
// recently supplied window size.
type SetDesiredViewportResponse struct{ Viewport Viewport }
