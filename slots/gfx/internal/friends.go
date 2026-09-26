package internal

import (
	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// The friend functions: what gfx's internal/ reads from a public type's
// unexported state. Only packages under slots/gfx can import this package, so
// these are not public API.

// OpQueueBakeTextureIfNeeded calls OpQueue.bakeTextureIfNeeded for gfx's internal/.
func OpQueueBakeTextureIfNeeded(v *OpQueue, a0 descriptors.TextureDescr) descriptors.TextureDescr {
	return v.bakeTextureIfNeeded(a0)
}

// OpQueueOps reads OpQueue.ops for gfx's internal/.
func OpQueueOps(v *OpQueue) []Op { return v.ops }

// OpQueuePasses reads OpQueue.passes for gfx's internal/.
func OpQueuePasses(v *OpQueue) []passRecord { return v.passes }

// OpQueueTemporaryBuffer calls OpQueue.temporaryBuffer for gfx's internal/.
func OpQueueTemporaryBuffer(v *OpQueue, a0 types.BufferKind, a1 []byte, a2 bool) descriptors.BufferDescr {
	return v.temporaryBuffer(a0, a1, a2)
}

// OpQueueTemporaryBuffers reads OpQueue.temporaryBuffers for gfx's internal/.
func OpQueueTemporaryBuffers(v *OpQueue) []temporaryBuffer { return v.temporaryBuffers }

// ResourceQueueFreeCachedResources calls ResourceQueue.freeCachedResources for gfx's internal/.
func ResourceQueueFreeCachedResources(v *ResourceQueue) { v.freeCachedResources() }

// ResourceQueueOps reads ResourceQueue.ops for gfx's internal/.
func ResourceQueueOps(v *ResourceQueue) []Op { return v.ops }

// ResourceQueueReleaseCachedResource calls ResourceQueue.releaseCachedResource for gfx's internal/.
func ResourceQueueReleaseCachedResource(v *ResourceQueue, a0 string) { v.releaseCachedResource(a0) }

// ResourceQueueReset calls ResourceQueue.reset for gfx's internal/.
func ResourceQueueReset(v *ResourceQueue) { v.reset() }
