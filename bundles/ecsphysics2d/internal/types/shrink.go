package types

import (
	"unsafe"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
)

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
	// KeepIndices keeps both grids: their entries, the Entity to slot table and
	// the cell lists behind the buckets.
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

// shrink cuts the Contact list's two areas and reports the bytes each let go.
//
// The cached run goes first, so the entries it was holding are counted against
// the area that was holding them rather than against the buffers.
func (c *Contacts) shrink(request ShrinkRequest) ShrinkResponse {
	var released ShrinkResponse
	if !request.KeepCached {
		before := c.bytes()
		c.dropCached()
		released.Cached = before - c.bytes()
	}
	if !request.KeepContacts {
		before := c.bytes()
		c.clipBuffers()
		released.Contacts = before - c.bytes()
	}
	return released
}

// dropCached forgets every cached entry and cuts the buffer to the entries the
// app can still see.
//
// A cached entry is past the public view and carries nothing but Impulses for a
// pair that is not touching, so dropping one changes no answer a System can
// read; what it costs is the warm start that pair would have had if it came
// back inside the persistence window. That is the trade the command is for, and
// it is the app's to make.
//
// This tick's pair table is rebuilt over what survives, because detection looks
// the previous tick's pairs up through it and a stale slot would point past the
// end of the buffer.
func (c *Contacts) dropCached() {
	if len(c.entries) == c.visible {
		return
	}
	c.entries = clip(c.entries[:c.visible])
	c.aux = clip(c.aux[:c.visible])
	c.lookup.clear()
	for i := range c.entries {
		c.lookup.insert(c.entries[i].A, c.entries[i].B, int32(i))
	}
}

// clipBuffers cuts both entry buffers and both pair tables to what they hold.
//
// Only one of the two buffers holds anything a later tick reads. The other is
// the tick before's, which endTick has already walked and which the next
// beginTick truncates before detection writes a thing into it, so it is
// released whole rather than clipped; the same goes for the pair table beside
// it. The frame after a shrink regrows them, and that frame allocates, which is
// what the command's documentation says it does.
func (c *Contacts) clipBuffers() {
	c.entries = clip(c.entries)
	c.aux = clip(c.aux)
	c.previous, c.prevAux = nil, nil
	c.prevLookup = pairTable{}
	c.lookup.shrink()
}

// bytes is what the Contact list's buffers and tables hold, by capacity.
func (c *Contacts) bytes() uintptr {
	return uintptr(cap(c.entries)+cap(c.previous))*unsafe.Sizeof(Contact{}) +
		uintptr(cap(c.aux)+cap(c.prevAux))*unsafe.Sizeof(contactAux{}) +
		c.lookup.bytes() + c.prevLookup.bytes()
}

// shrink rebuilds the table at the smallest size that holds what it has at
// under half load, which is the load factor insert keeps it at. A table already
// at that size is left alone, so a second shrink allocates nothing.
func (t *pairTable) shrink() {
	size := pairTableMin
	for size < t.used*2 {
		size *= 2
	}
	if t.used == 0 {
		t.keys, t.used = nil, 0
		return
	}
	if size == len(t.keys) {
		return
	}
	old := t.keys
	t.keys = make([]pairKey, size)
	t.used = 0
	for i := range old {
		if old[i].a != ecs.NoEntity {
			t.insert(old[i].a, old[i].b, old[i].slot)
		}
	}
}

// bytes is what the table holds, by capacity.
func (t *pairTable) bytes() uintptr {
	return uintptr(cap(t.keys)) * unsafe.Sizeof(pairKey{})
}

// shrink cuts one grid's two areas and reports the bytes each let go.
//
// The slab goes first: packing it is what makes the entries' runs exact, and
// the grid pass that follows moves those entries about.
func (idx *index) shrink(request ShrinkRequest) ShrinkResponse {
	var released ShrinkResponse
	if !request.KeepWorldCache {
		before := idx.slabBytes()
		idx.packSlab()
		released.WorldCache = before - idx.slabBytes()
	}
	if !request.KeepIndices {
		before := idx.gridBytes()
		idx.packGrid()
		released.Indices = before - idx.gridBytes()
	}
	return released
}

// packSlab puts every live entry's world cache back to back and cuts the slab
// to what that comes to.
//
// A slab grows two ways and only one of them is slack. Clear resets it, so the
// Body index's is packed already and all this cuts is the capacity a spike left
// behind. The static index is never Cleared, and there an Insert that needs a
// longer run than the slot it recycled abandons the old one where it lies —
// which is free while the index is refilled every tick and is a leak for the
// life of the world where it is not. Packing is what collects those.
//
// A dead slot's run is forgotten rather than moved: it names a Shape no query
// can reach, and leaving its offset behind would have the next Insert to
// recycle that slot write over a live entry's cache. Setting its capacity to
// zero is what makes that Insert take a fresh run at the end.
func (idx *index) packSlab() {
	live := idx.liveWorldLen()
	if live == len(idx.slab) && len(idx.slab) == cap(idx.slab) {
		// Already exact, which is what a Body index is at the end of every
		// rebuild that did not grow it. A second shrink allocates nothing.
		return
	}
	if live == 0 {
		idx.slab = nil
		for slot := range idx.entries {
			idx.entries[slot].world, idx.entries[slot].worldLen = 0, 0
			idx.entries[slot].worldCap = 0
		}
		return
	}

	packed := make([]m.Vec2d, 0, live)
	for slot := range idx.entries {
		e := &idx.entries[slot]
		if !e.live {
			e.world, e.worldLen, e.worldCap = 0, 0, 0
			continue
		}
		at := int32(len(packed))
		packed = append(packed, idx.slab[e.world:e.world+e.worldLen]...)
		e.world, e.worldCap = at, e.worldLen
	}
	idx.slab = packed
}

// liveWorldLen is how long a packed slab is: the world caches of the live
// entries and nothing else.
func (idx *index) liveWorldLen() int {
	total := 0
	for slot := range idx.entries {
		if idx.entries[slot].live {
			total += int(idx.entries[slot].worldLen)
		}
	}
	return total
}

// packGrid drops the dead entry slots, rebuilds the Entity to slot table over
// what survives, and lists it all again into a bucket table sized for what it
// now holds.
//
// The cell lists cannot be clipped where they lie, because a listing names an
// entry by slot and compacting the entries renumbers them. Relisting is what
// growBuckets already does when the table doubles, and this is that pass with
// the table allowed to shrink.
//
// The Entity to slot table is rebuilt into a new map rather than cleared,
// because a Go map keeps the buckets a spike gave it and clear is not a
// release. It is rebuilt every time this runs, so a second shrink allocates one
// map where it allocates nothing else; there is no way to ask a map what it
// costs, so there is nothing cheaper to test first.
func (idx *index) packGrid() {
	kept := 0
	for slot := range idx.entries {
		if !idx.entries[slot].live {
			continue
		}
		idx.entries[kept] = idx.entries[slot]
		kept++
	}
	idx.entries = clip(idx.entries[:kept])
	idx.freeEntries = nil

	idx.slots = make(map[ecs.Entity]int32, kept)
	wanted := 0
	for slot := range idx.entries {
		e := &idx.entries[slot]
		idx.slots[e.entity] = int32(slot)
		if e.right >= e.left && e.top >= e.bottom {
			wanted += (int(e.right-e.left) + 1) * (int(e.top-e.bottom) + 1)
		}
	}

	// The table starts again at the documented floor and doubles up to what the
	// listings need, so a table a spike doubled four times comes back down. The
	// factor of two is list's own invariant: the listings never fill more than
	// half of it.
	size := defaultBuckets
	for size < wanted*2 {
		size *= 2
	}
	if size != len(idx.buckets) {
		idx.buckets = make([]int32, size)
	}
	idx.clearBuckets()
	idx.links = idx.links[:0]
	idx.freeLinks = nil
	idx.listings = 0
	for slot := range idx.entries {
		idx.pushCells(int32(slot))
	}
	idx.links = clip(idx.links)
}

// slabBytes is what the world cache holds, by capacity.
func (idx *index) slabBytes() uintptr {
	return uintptr(cap(idx.slab)) * unsafe.Sizeof(m.Vec2d{})
}

// gridBytes is what the entries, the cell lists and the bucket table hold, by
// capacity.
//
// The Entity to slot map is not in it, and cannot be: Go publishes a map's
// length and never what its buckets cost, so a map rebuilt at the right size
// releases memory this report has no way to see. What the response says is
// therefore a floor on what the area gave back, and the map is the part that is
// missing from it.
func (idx *index) gridBytes() uintptr {
	return uintptr(cap(idx.entries))*unsafe.Sizeof(entry{}) +
		uintptr(cap(idx.freeEntries)+cap(idx.freeLinks)+cap(idx.buckets))*unsafe.Sizeof(int32(0)) +
		uintptr(cap(idx.links))*unsafe.Sizeof(link{})
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
