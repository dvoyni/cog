package types

import (
	"github.com/dvoyni/cog/kernel"
)

// ShrinkRequest names the areas a shrink leaves alone. The zero request shrinks
// every area, and each Keep option opts one out.
type ShrinkRequest struct {
	// KeepHooks keeps every Store's Hook log and each reader's copy.
	KeepHooks bool
	// KeepStores keeps each Store's rows and sparse array.
	KeepStores bool
	// KeepEntities keeps the free list and the index space.
	KeepEntities bool
	// KeepScratch keeps per-System buffers: a Query's walk, and a writer's row
	// copies for Changed.
	KeepScratch bool
}

// ShrinkResponse is the bytes each area released.
type ShrinkResponse struct{ Hooks, Stores, Entities, Scratch uintptr }

// shrinkCommand is the ShrinkCmd factory the ecs plugin registers.
func shrinkCommand() (kernel.Lock, kernel.Execute[ShrinkRequest, ShrinkResponse]) {
	var entities kernel.Write[*Entities]
	return func(access kernel.ResourceAccess) {
			entities = access.GetWrite[*Entities]()
		}, func(_ kernel.Kernel, request ShrinkRequest) ShrinkResponse {
			return entities.Get().shrink(request)
		}
}
