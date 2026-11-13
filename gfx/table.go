package gfx

import (
	"fmt"
	"iter"
	"sync"
)

// table is gfx's table of one handle type H: the slots its handles index,
// each with the generation of the handle it holds or last held, the
// descriptor D it was created with and the handles it was made from. It is
// safe from many goroutines, since jobs create and release under the
// Device's read lock.
//
// A handle's life is acquire, then retire once it is to be released, which
// makes it stale at once, then release, which hands its index out again; a
// backend releases its object between the two, so no create lands at an index
// whose object is still being released. While an object made from the handle
// is held, release keeps its slot instead, its descriptor and index
// unchanged, so what that object was made from can still be read, as a
// recovery does; the release of the last such object hands its index out.
type table[H ~struct{ handle }, D any] struct {
	mutex sync.RWMutex
	slots []slot[D]
	// freeIndices are the released slots, handed out before new ones.
	freeIndices []uint32
}

// acquire answers a handle to a free slot, or a new one when none is free,
// holding descriptor, made from dependencies, whose dependents the caller
// added.
func (t *table[H, D]) acquire(descriptor D, dependencies ...dependency) H {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	var index uint32
	if count := len(t.freeIndices); count > 0 {
		index = t.freeIndices[count-1]
		t.freeIndices = t.freeIndices[:count-1]
	} else {
		index = uint32(len(t.slots))
		t.slots = append(t.slots, slot[D]{})
	}
	entry := &t.slots[index]
	entry.generation = nextGeneration(entry.generation)
	entry.descriptor = descriptor
	entry.dependencies = dependencies
	entry.state = slotStateHeld
	return H{handle{index: index, generation: entry.generation}}
}

// retire makes h stale; ErrReleased when it already is.
func (t *table[H, D]) retire(h H) error {
	key := struct{ handle }(h).handle
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if !t.holds(key) {
		return ErrReleased
	}
	t.slots[key.index].state = slotStateRetired
	return nil
}

// release keeps the slot of h, retired, while an object made from h is held,
// and otherwise hands its index out again, dropping its descriptor and
// removing it as a dependent of what it was made from. It panics when h is
// not retired.
func (t *table[H, D]) release(h H) {
	index := struct{ handle }(h).index
	t.mutex.Lock()
	if t.slots[index].state != slotStateRetired {
		t.mutex.Unlock()
		panic(fmt.Sprintf("gfx: releasing %T at %d, which is not retired", h, index))
	}
	t.slots[index].state = slotStateKept
	dependencies := t.freeIfUnneeded(index)
	t.mutex.Unlock()
	removeDependents(dependencies)
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

// handles answers every handle whose slot is in state, in index order, with
// what it was acquired with, as they were when it was called.
func (t *table[H, D]) handles(state slotState) iter.Seq2[H, D] {
	t.mutex.RLock()
	var handles []H
	var descriptors []D
	for index, entry := range t.slots {
		if entry.state == state {
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

// addDependent counts one more object made from key; ErrReleased when key is
// stale.
func (t *table[H, D]) addDependent(key handle) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if !t.holds(key) {
		return ErrReleased
	}
	t.slots[key.index].dependentCount++
	return nil
}

// removeDependent counts one object made from key fewer; a kept slot losing
// the last has its index handed out again, as release does.
func (t *table[H, D]) removeDependent(key handle) {
	t.mutex.Lock()
	t.slots[key.index].dependentCount--
	dependencies := t.freeIfUnneeded(key.index)
	t.mutex.Unlock()
	removeDependents(dependencies)
}

// appendDependency answers dependencies with h appended, for an object made
// from h; ErrReleased when h is stale, the zero handle included.
func (t *table[H, D]) appendDependency(dependencies []dependency, h H) ([]dependency, error) {
	if err := t.check(h); err != nil {
		return dependencies, err
	}
	return append(dependencies, dependency{t, struct{ handle }(h).handle}), nil
}

// holds answers whether key names a held slot under its current generation;
// the caller holds the mutex.
func (t *table[H, D]) holds(key handle) bool {
	return key.index < uint32(len(t.slots)) && t.slots[key.index].state == slotStateHeld && t.slots[key.index].generation == key.generation
}

// freeIfUnneeded empties the slot at index when it is kept and no object made
// from it is held, keeping its generation, and hands index out again,
// answering what it was made from, whose dependents the caller removes once
// it has unlocked the mutex it holds; the caller holds the mutex.
func (t *table[H, D]) freeIfUnneeded(index uint32) []dependency {
	entry := &t.slots[index]
	if entry.state != slotStateKept || entry.dependentCount > 0 {
		return nil
	}
	dependencies := entry.dependencies
	*entry = slot[D]{generation: entry.generation}
	t.freeIndices = append(t.freeIndices, index)
	return dependencies
}

// slot is one entry of a table: the generation of the handle it holds or last
// held, that handle's descriptor and what it was made from, how many held or
// kept objects are made from it, and its state.
type slot[D any] struct {
	generation     uint32
	descriptor     D
	dependencies   []dependency
	dependentCount uint32
	state          slotState
}

// slotState is where a table's slot is in its handle's life.
type slotState uint8

const (
	// slotStateFree is a slot an acquire may hand out.
	slotStateFree slotState = iota
	// slotStateHeld is a slot whose handle is live, from its acquire until its
	// retire.
	slotStateHeld
	// slotStateRetired is a slot whose handle is stale and whose object the
	// backend may still be releasing, from its retire until its release.
	slotStateRetired
	// slotStateKept is a slot whose handle was released while objects made
	// from it are held, kept with its descriptor until the last of them is
	// released.
	slotStateKept
)

// dependency is a handle an object is made from, with the table holding it.
type dependency struct {
	table dependencyTable
	key   handle
}

// dependencyTable is a table as the objects made from its handles reach it:
// it counts them, and keeps a released handle's slot while one is held.
type dependencyTable interface {
	addDependent(key handle) error
	removeDependent(key handle)
}

// addDependents counts one more object made from each of dependencies;
// ErrReleased, with none counted, when one is stale.
func addDependents(dependencies []dependency) error {
	for addedCount, dependency := range dependencies {
		if err := dependency.table.addDependent(dependency.key); err != nil {
			removeDependents(dependencies[:addedCount])
			return err
		}
	}
	return nil
}

// removeDependents counts one object made from each of dependencies fewer.
func removeDependents(dependencies []dependency) {
	for _, dependency := range dependencies {
		dependency.table.removeDependent(dependency.key)
	}
}

// nextGeneration answers the generation after generation, skipping 0, which
// only the zero handle holds.
func nextGeneration(generation uint32) uint32 {
	if generation++; generation == 0 {
		generation++
	}
	return generation
}
