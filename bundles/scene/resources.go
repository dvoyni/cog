package scene

import "github.com/dvoyni/cog/bundles/scene/internal"

// OpQueue is the frame-local writable resource gameplay records cameras, models,
// meshes, lights and debug shapes into. The scene plugin consumes and
// republishes it at the end of each update tick, in FlushOnUpdate.
//
// It double-buffers: the flush publishes the frame it just consumed and starts
// recording into the buffer the previous publication vacated, so Ops and Passes
// describe the frame that was actually flushed and stay valid until the next
// one. Nothing is copied to achieve that - the two halves swap.
//
// It is a concrete type, so recording a draw is a direct method call with
// nothing between the caller and the queue; consuming it is the plugin's alone.
type OpQueue = internal.OpQueue
