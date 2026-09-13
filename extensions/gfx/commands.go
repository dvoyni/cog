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

// SetBackendCmd installs the Backend the plugin renders through. A driver calls
// it once, when its GPU device is ready; the backend is installed into all three
// OpQueue instances, so recording can reserve resource IDs.
type SetBackendCmd kernel.Command[SetBackendRequest, SetBackendResponse]
type SetBackendRequest struct{ Backend Backend }
type SetBackendResponse struct{}

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
