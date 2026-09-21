package ecsphysics2d

import (
	"github.com/dvoyni/cog/bundles/ecsphysics2d/internal/types"
	"github.com/dvoyni/cog/kernel"
)

// ShrinkCmd gives physics' memory back after a spike, and it is the only thing
// in the package that does. The Contact buffers, the pair tables, the two grids,
// the world-cache slab and the solver's scratch all keep their high-water
// capacity, so a steady
// scene never allocates; after a level load, a boss's debris or a screen of
// projectiles that capacity stays until the app executes this:
//
//	response, err := executioner.ExecuteCommand[ecsphysics2d.ShrinkCmd](
//	    ecsphysics2d.ShrinkRequest{})
//
// The zero request shrinks every area, and each Keep option opts one out. The
// response reports the bytes each area released; Go frees the old arrays at its
// next collection, and debug.FreeOSMemory is the caller's to call. The ticks
// after it regrow what they need, and those ticks allocate, so shrink when a
// spike has ended rather than every tick.
//
// It is a command and not a heuristic, deliberately. A buffer that shrank
// itself whenever a tick looked quiet would put an allocation into the tick
// after every lull, on the hot path, with nothing in the app able to say when;
// and a buffer that never gave memory back would hold the busiest scene's peak
// for the life of the world. Both were rejected. The app knows when the spike
// is over and nothing else does.
//
// The physics plugin registers it, holding write on Contacts, StaticIndex and
// BodyIndex and nothing besides — no Component Store and not the id authority.
// That excludes Index, Detect and Solve while it runs, which is exactly what it
// must, and costs a tick that does not execute it nothing at all. It also
// declares itself exclusive, so two invocations never overlap whatever its
// locks become.
type ShrinkCmd kernel.Command[ShrinkRequest, ShrinkResponse]

// ShrinkRequest names the areas a ShrinkCmd leaves alone. The zero value
// shrinks everything:
//
//   - KeepContacts keeps the Contact list's two entry buffers, the private run
//     beside each and the two pair tables, which otherwise are cut to what they
//     hold;
//   - KeepCached keeps the cached-entry run — the pairs that have already
//     reported Ended and are carried unreported as Impulse carriers until the
//     persistence window closes. Shrinking it drops them, so a pair that
//     flickers apart across the shrink comes back at Began with no warm start;
//   - KeepIndices keeps both grids: their entries, the static index's Entity to
//     slot table and the cell lists behind the buckets, which otherwise are
//     compacted and the bucket table re-sized to what the listings now need;
//   - KeepWorldCache keeps the world-cache slab behind both grids, which
//     otherwise is packed — and packing it is the one area that collects
//     something a steady state leaks rather than merely slack, because a Static
//     replaced by a Shape needing a longer run abandons its old one where it
//     lies and only a Clear the static index never gets would reclaim it;
//   - KeepScratch keeps the solver's gather — the solved list, the Body slot
//     table and rows, the Joint rows — and the swept Sensor Probe buffer, which
//     otherwise are released whole. Nothing in them is read across a tick, so
//     shrinking them changes no answer; the slot table is sized to the largest
//     Body slot detection ever saw, so it is what a large spike leaves behind.
type ShrinkRequest = types.ShrinkRequest

// ShrinkResponse is the bytes a ShrinkCmd released, per area, summed over both
// indices where an area names two. An area kept reports 0.
//
// It is a floor rather than an exact figure in one place: the static index's
// Entity to slot table is a Go map, and a map publishes its length and never
// what its buckets cost, so what that map gives back is real and is not counted
// here. The Body index keeps no such table, so its share of Indices is exact.
type ShrinkResponse = types.ShrinkResponse
