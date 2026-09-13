package gfx

import "github.com/dvoyni/cog/extensions/gfx/internal"

// OpQueue is the writable kernel resource gameplay records high-level frame
// commands into. Subscriptions must declare Writes[*gfx.OpQueue] and access it
// through their ResourceAccess. All uploads it owns are temporary and may be
// dropped with the frame; persistent GPU resources are managed separately
// through ResourceQueue.
//
// It is a concrete type: its recording methods are called per draw, and they
// never go through an interface. What it recorded is read by gfx alone.
type OpQueue = internal.OpQueue
