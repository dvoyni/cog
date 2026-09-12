package ecs

// Blobs is per-Entity variable-length bytes: the case a Component cannot hold
// and a hash cannot name.
//
// It is here as the worked example of a SideStore rather than as something the
// ECS ships. A plugin that needs one writes it; what the ECS supplies is the
// enrolment in Despawn, which is the only part that has to be engine business.
//
// The structure is a Store's, minus the generics: a flat sparse index into a
// dense array, swap-removed. The difference is that the dense rows are byte
// slices rather than plain values, so a removed row's buffer is kept and
// reissued rather than dropped -- a spawn-and-despawn churn reaches a
// high-water mark and then allocates nothing, which is the same discipline
// cog#243 measured for Stores.
type Blobs struct {
	sparse []uint64 // entity index -> gen<<32 | dense index
	owners []Entity
	data   [][]byte
	// free holds the buffers removed rows gave back, reissued newest first.
	free [][]byte
}

func NewBlobs(ids uint32) *Blobs {
	b := &Blobs{
		sparse: make([]uint64, ids),
		owners: make([]Entity, 0, ids),
		data:   make([][]byte, 0, ids),
	}
	for i := range b.sparse {
		b.sparse[i] = absentSlot
	}
	return b
}

func (b *Blobs) probe(e Entity) (uint32, bool) {
	v := b.sparse[e.idx()]
	return uint32(v), uint32(v>>32) == e.gen()
}

// Set gives e a copy of buf. The copy goes into a buffer this store already
// owns wherever one is available, so steady-state churn allocates nothing.
func (b *Blobs) Set(e Entity, buf []byte) {
	if j, ok := b.probe(e); ok {
		b.data[j] = append(b.data[j][:0], buf...)
		return
	}
	j := uint32(len(b.owners))
	var row []byte
	if n := len(b.free); n > 0 {
		row, b.free = b.free[n-1], b.free[:n-1]
	}
	row = append(row[:0], buf...)
	b.owners = append(b.owners, e)
	b.data = append(b.data, row)
	b.sparse[e.idx()] = uint64(e.gen())<<32 | uint64(j)
}

// Get returns e's bytes. They alias the store and are valid only while the
// caller holds its lock, exactly as a Component pointer is.
func (b *Blobs) Get(e Entity) ([]byte, bool) {
	j, ok := b.probe(e)
	if !ok {
		return nil, false
	}
	return b.data[j], true
}

// Remove is the SideStore half: swap-remove, and the vacated buffer goes on the
// free list with its capacity intact rather than to the collector.
func (b *Blobs) Remove(e Entity) bool {
	j, ok := b.probe(e)
	if !ok {
		return false
	}
	last := uint32(len(b.owners) - 1)
	b.free = append(b.free, b.data[j][:0])
	if j != last {
		b.data[j] = b.data[last]
		moved := b.owners[last]
		b.owners[j] = moved
		b.sparse[moved.idx()] = uint64(moved.gen())<<32 | uint64(j)
	}
	b.data = b.data[:last]
	b.owners = b.owners[:last]
	b.sparse[e.idx()] = absentSlot
	return true
}

func (b *Blobs) Len() int { return len(b.owners) }

// Pooled reports how many buffers are held for reuse. A churning world's count
// settles at its high-water mark, which is what says nothing is leaking and
// nothing is being re-allocated.
func (b *Blobs) Pooled() int { return len(b.free) }
