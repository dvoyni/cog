package container

import (
	"sync"
	"sync/atomic"
)

// ObjectTable is a growing store of T slots at indexes its owner hands out,
// such as the objects a backend keeps at the indexes of another table's
// handles. Slots live in fixed-size chunks that never move, so a pointer At
// or Grow returns stays valid for the table's life.
//
// At takes no lock, and Grow takes the table's mutex only to add chunks, so
// both are safe from any goroutine. A slot's own reads and writes are its
// owner's to keep apart: an atomic T, or a lock of the owner's. The zero
// ObjectTable is empty and ready to use.
//
// T may hold pointers: the GC scans the chunks.
type ObjectTable[T any] struct {
	chunks atomic.Pointer[[]*[chunkSize]T]
	mutex  sync.Mutex
}

// At returns the slot at index, or nil while the table has not grown to hold
// it.
func (table *ObjectTable[T]) At(index uint32) *T {
	chunks := table.chunks.Load()
	if chunks == nil || index>>chunkShift >= uint32(len(*chunks)) {
		return nil
	}
	return &(*chunks)[index>>chunkShift][index&chunkMask]
}

// Grow adds chunks until the table holds the slot at index, and returns it.
// A slot already held is returned without the lock.
func (table *ObjectTable[T]) Grow(index uint32) *T {
	if slot := table.At(index); slot != nil {
		return slot
	}
	table.mutex.Lock()
	defer table.mutex.Unlock()
	var chunks []*[chunkSize]T
	if old := table.chunks.Load(); old != nil {
		chunks = *old
	}
	// A fresh slice, not an append in place: At may be reading the old one.
	grown := chunks[:len(chunks):len(chunks)]
	for index>>chunkShift >= uint32(len(grown)) {
		grown = append(grown, new([chunkSize]T))
	}
	table.chunks.Store(&grown)
	return &grown[index>>chunkShift][index&chunkMask]
}
