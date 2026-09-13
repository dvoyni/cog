package gfx

import "github.com/dvoyni/cog/extensions/gfx/internal"

// ResourceQueue is registered as its own kernel resource. Subscriptions that
// create, update, or release persistent GPU resources must declare
// Writes[*gfx.ResourceQueue] and access it through their ResourceAccess. Unlike
// OpQueue, it is not triple-buffered or latest-wins: operations remain queued
// until the render thread executes them.
type ResourceQueue = internal.ResourceQueue
