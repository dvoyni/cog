package gfx

import (
	"iter"
	"sync"
)

// table is gfx's table of one handle type H: the slots its handles index,
// each with the generation of the handle it holds and the descriptor D it was
// created with. It is safe from many goroutines, since jobs create and release
// under the Device's read lock.
//
// A handle's life is acquire, then retire once it is to be released, which
// makes it stale at once, then release, which hands its index out again; a
// backend releases its object between the two, so no create lands at an index
// whose object is still being released.
type table[H ~struct{ handle }, D any] struct {
	mutex sync.RWMutex
	slots []slot[D]
	// freeIndices are the released slots, handed out before new ones.
	freeIndices []uint32
}

// acquire answers a handle to a free slot, or a new one when none is free,
// holding descriptor.
func (t *table[H, D]) acquire(descriptor D) H {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	var index uint32
	if count := len(t.freeIndices); count > 0 {
		index = t.freeIndices[count-1]
		t.freeIndices = t.freeIndices[:count-1]
	} else {
		index = uint32(len(t.slots))
		t.slots = append(t.slots, slot[D]{generation: nextGeneration(0)})
	}
	t.slots[index].descriptor = descriptor
	t.slots[index].held = true
	return H{handle{index: index, generation: t.slots[index].generation}}
}

// retire makes h stale, dropping its descriptor; ErrReleased when it already
// is.
func (t *table[H, D]) retire(h H) error {
	key := struct{ handle }(h).handle
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if !t.holds(key) {
		return ErrReleased
	}
	entry := &t.slots[key.index]
	entry.generation = nextGeneration(entry.generation)
	var zero D
	entry.descriptor = zero
	entry.held = false
	return nil
}

// release hands the index of h, retired, out again.
func (t *table[H, D]) release(h H) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.freeIndices = append(t.freeIndices, struct{ handle }(h).index)
}

// descriptor answers what h was acquired with; ErrReleased when it is stale.
func (t *table[H, D]) descriptor(h H) (D, error) {
	key := struct{ handle }(h).handle
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	if !t.holds(key) {
		var zero D
		return zero, ErrReleased
	}
	return t.slots[key.index].descriptor, nil
}

// check answers ErrReleased when h is stale, the zero handle included.
func (t *table[H, D]) check(h H) error {
	key := struct{ handle }(h).handle
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	if !t.holds(key) {
		return ErrReleased
	}
	return nil
}

// heldHandles answers every handle t holds, in index order, with what it
// was acquired with, as they were when it was called.
func (t *table[H, D]) heldHandles() iter.Seq2[H, D] {
	t.mutex.RLock()
	var handles []H
	var descriptors []D
	for index, entry := range t.slots {
		if entry.held {
			handles = append(handles, H{handle{index: uint32(index), generation: entry.generation}})
			descriptors = append(descriptors, entry.descriptor)
		}
	}
	t.mutex.RUnlock()
	return func(yield func(H, D) bool) {
		for index, h := range handles {
			if !yield(h, descriptors[index]) {
				return
			}
		}
	}
}

// holds answers whether key names a slot under its current generation; the
// caller holds the mutex.
func (t *table[H, D]) holds(key handle) bool {
	return key.index < uint32(len(t.slots)) && t.slots[key.index].generation == key.generation
}

// slot is one entry of a table: the generation of the handle it holds or
// last held, that handle's descriptor, and whether it is held, from its
// acquire until its retire.
type slot[D any] struct {
	generation uint32
	descriptor D
	held       bool
}

// nextGeneration answers the generation after generation, skipping 0, which
// only the zero handle holds.
func nextGeneration(generation uint32) uint32 {
	if generation++; generation == 0 {
		generation++
	}
	return generation
}
