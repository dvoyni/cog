package gfx

// OpQueue is the writable kernel resource gameplay records high-level frame
// commands into. Subscriptions must declare Writes[*gfx.OpQueue] and access it
// through their ResourceAccess.
type OpQueue = opQueue

// ResourceQueue is registered as its own kernel resource. Subscriptions that
// create, update, or release persistent GPU resources must declare
// Writes[*gfx.ResourceQueue] and access it through their ResourceAccess.
type ResourceQueue = resourceQueue
