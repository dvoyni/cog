package types

import "github.com/dvoyni/cog/kernel"

// ShrinkRequest names the areas a shrink leaves alone. The zero request shrinks
// every area, and each Keep option opts one out.
//
// Memory release is a command the app calls and never a heuristic the plugin
// runs: a buffer that shrinks itself when it looks idle gives the frame after a
// quiet tick an allocation nobody asked for, and a buffer that never gives
// memory back holds a level's peak for the whole run. Both were rejected, which
// is why this is here and why nothing else in the package calls it.
type ShrinkRequest struct {
	// KeepContacts keeps the Contact list's two entry buffers, their private
	// runs and the two pair tables.
	KeepContacts bool
	// KeepCached keeps the cached-entry run: the pairs that have already
	// reported Ended and are carried unreported as Impulse carriers until the
	// persistence window closes. Shrinking it drops them, so a pair that
	// flickers apart across the shrink starts again at Began with no warm start.
	KeepCached bool
	// KeepIndices keeps both grids: their entries, the static index's Entity
	// to slot table and the cell lists behind the buckets.
	KeepIndices bool
	// KeepWorldCache keeps the world-cache slab behind both grids — the
	// world-space geometry the closed forms read.
	KeepWorldCache bool
}

// ShrinkResponse is the bytes each area released, summed over both indices
// where an area names two.
type ShrinkResponse struct{ Contacts, Cached, Indices, WorldCache uintptr }

// add sums another area report into this one, which is how the two indices
// report as one area each.
func (r *ShrinkResponse) add(other ShrinkResponse) {
	r.Contacts += other.Contacts
	r.Cached += other.Cached
	r.Indices += other.Indices
	r.WorldCache += other.WorldCache
}

// ShrinkCommand is the ShrinkCmd factory the physics plugin registers.
//
// Its lock is write on the three Resources whose buffers it cuts and nothing
// besides — no Component Store and not the id authority — so it excludes Index,
// Detect and Solve, which is exactly what it must, and costs a tick that does
// not execute it nothing at all.
func ShrinkCommand() (kernel.Lock, kernel.Execute[ShrinkRequest, ShrinkResponse]) {
	var contacts kernel.Write[*Contacts]
	var statics kernel.Write[*StaticIndex]
	var bodies kernel.Write[*BodyIndex]
	return func(access kernel.ResourceAccess) {
			contacts = access.GetWrite[*Contacts]()
			statics = access.GetWrite[*StaticIndex]()
			bodies = access.GetWrite[*BodyIndex]()
		}, func(_ kernel.Kernel, request ShrinkRequest) ShrinkResponse {
			var released ShrinkResponse
			released.add(contacts.Get().shrink(request))
			released.add(statics.Get().shrink(request))
			released.add(bodies.Get().shrink(request))
			return released
		}
}

// clip returns s at capacity equal to its length, copying into a new array when
// there is slack and returning s itself when there is none. An empty s becomes
// nil rather than a zero-capacity slice of the old array, which would keep that
// array reachable. It is the ECS's own, spelt again here because internal
// packages do not import one another.
func clip[T any](s []T) []T {
	if len(s) == cap(s) {
		return s
	}
	if len(s) == 0 {
		return nil
	}
	clipped := make([]T, len(s))
	copy(clipped, s)
	return clipped
}
