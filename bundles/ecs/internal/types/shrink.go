package types

import (
	"unsafe"

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
		}, func(_ kernel.Kernel, request ShrinkRequest) (ShrinkResponse, error) {
			return entities.Get().shrink(request), nil
		}
}

func (en *Entities) shrink(request ShrinkRequest) ShrinkResponse {
	var released ShrinkResponse
	if !request.KeepHooks {
		for _, shrink := range en.shrinkables().hooks {
			released.Hooks += shrink()
		}
	}
	if !request.KeepStores {
		for _, shrink := range en.shrinkables().stores {
			released.Stores += shrink()
		}
	}
	if !request.KeepEntities {
		released.Entities = en.shrinkIndices()
	}
	if !request.KeepScratch {
		for _, release := range en.shrinkables().scratch {
			released.Scratch += release()
		}
	}
	return released
}

// shrinkIndices drops the free indices at the top of the index space and cuts
// the generations and the free list to capacity equal to length, reporting the
// bytes let go.
func (en *Entities) shrinkIndices() uintptr {
	before := en.bytes()
	top := len(en.gens)
	if len(en.free) > 0 {
		// A bitmap of the free indices, so finding the unused run at the top
		// costs one pass over the free list and none over a sorted copy of it.
		free := make([]uint64, (top+63)/64)
		for _, index := range en.free {
			free[index/64] |= 1 << (index % 64)
		}
		for top > 0 && free[(top-1)/64]&(1<<((top-1)%64)) != 0 {
			top--
		}
	}
	// Dropping an index forgets its generation, so the floor takes the highest
	// generation dropped before the index space is cut.
	for _, generation := range en.gens[top:] {
		en.floor = max(en.floor, generation)
	}
	kept := en.free[:0]
	for _, index := range en.free {
		if int(index) < top {
			kept = append(kept, index)
		}
	}
	en.free = clip(kept)
	en.gens = clip(en.gens[:top])
	return before - en.bytes()
}

// bytes is what the generations and the free list hold, by capacity.
func (en *Entities) bytes() uintptr {
	return uintptr(cap(en.gens)+cap(en.free)) * unsafe.Sizeof(uint32(0))
}
