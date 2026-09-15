package ecs

import (
	"github.com/dvoyni/cog/bundles/ecs/internal/types"
	"github.com/dvoyni/cog/kernel"
)

// ShrinkCmd gives memory back after a spike, and it is the only thing in the
// ECS that does. A Store keeps its high-water capacity, and so do the free list,
// a Query's walk and what Hooks hold, so steady state never allocates; after a
// level load or a screen of effects that capacity stays until the app executes
// this:
//
//	response, err := executioner.ExecuteCommand[ecs.ShrinkCmd](ecs.ShrinkRequest{})
//
// The zero request shrinks every area to capacity equal to length, with no slack
// left, and each Keep option opts one area out. The response reports the bytes
// each area released; Go frees the old arrays at its next collection, and
// debug.FreeOSMemory is the caller's to call. The frames after it regrow what
// they need, and those frames allocate, so shrink when a spike has ended rather
// than every frame.
//
// The ecs plugin registers it, holding write{*Entities} and nothing besides.
// Every handler that touches a Store holds read{*Entities}, so that one lock
// excludes every ECS System while it runs and costs nothing in a frame that
// does not execute it.
type ShrinkCmd kernel.Command[ShrinkRequest, ShrinkResponse]

// ShrinkRequest names the areas a ShrinkCmd leaves alone. The zero value
// shrinks everything:
//
//   - KeepHooks keeps every Store's Hook log and each reader's copy;
//   - KeepStores keeps each Store's rows and its sparse array, which otherwise
//     are cut to its population and to its highest index held;
//   - KeepEntities keeps the free list and the index space, whose unused indices
//     at the top are otherwise dropped behind a generation floor, so a handle to
//     a dropped index never matches the Entity that index is later allocated to;
//   - KeepScratch keeps per-System buffers: a Query's walk, and a writer's row
//     copies for Changed.
type ShrinkRequest = types.ShrinkRequest

// ShrinkResponse is the bytes a ShrinkCmd released, per area. An area kept
// reports 0.
type ShrinkResponse = types.ShrinkResponse
