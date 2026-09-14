package gfx

import "github.com/dvoyni/cog/slots/gfx/internal/types"

// Viewport holds the logical world size, device-independent window size, and
// physical framebuffer size. A game chooses the logical sizing policy through
// SetDesiredViewportCmd; the driver supplies both output sizes through
// SetViewportCmd.
type Viewport = types.Viewport

// OpQueue is the writable kernel resource gameplay records high-level frame
// commands into. Subscriptions must declare Writes[*gfx.OpQueue] and access it
// through their ResourceAccess. All uploads it owns are temporary and may be
// dropped with the frame; persistent GPU resources are managed separately
// through ResourceQueue.
//
// It is a concrete type: its recording methods are called per draw, and they
// never go through an interface. What it recorded is read by gfx alone.
type OpQueue = types.OpQueue

// ResourceQueue is registered as its own kernel resource. Subscriptions that
// create, update, or release persistent GPU resources must declare
// Writes[*gfx.ResourceQueue] and access it through their ResourceAccess. Unlike
// OpQueue, it is not triple-buffered or latest-wins: operations remain queued
// until the render thread executes them.
type ResourceQueue = types.ResourceQueue
