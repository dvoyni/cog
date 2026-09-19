package types

import (
	"unsafe"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// The index's half of ShrinkCmd: the world-cache slab and the grid, each
// packed down to what is live and then cut to what is used. The request, and
// the response both areas report into, are in shrink.go.

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
