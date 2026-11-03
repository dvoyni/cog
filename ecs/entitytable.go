package ecs

import (
	"fmt"
	"iter"
	"sync/atomic"

	"github.com/dvoyni/cog/kernel/container"
)

// entityTable is a world's entities: each index's slot, and the rules by
// which indices are handed out, reserved, freed and reused.
//
// Entities are handed out from the back of the free list, the last freed
// first, then from fresh indices of the allocation range. A reservation is
// one atomic count over that order, so producers reserve in parallel under a
// read on the world; every other change runs under the barrier, which
// settles the reservations taken so far first where it needs to.
//
// A table that observes keeps a despawned index off the free list until the
// window ends, so no index is reused in the window it died in, and the end
// of the window takes back reservations never spawned. One that does not
// observe frees a despawned index at once, and loses the index of a
// reservation never spawned.
type entityTable[W WorldID] struct {
	// firstIndex and lastIndex bound the allocation range, inclusive.
	firstIndex, lastIndex uint32
	slots                 indexTable[entitySlot[W]]
	// freeEntities holds the entities to hand out next, from the back: each
	// is a freed index in the allocation range at its next generation.
	freeEntities container.Array[Entity]
	// reservationCount counts the entities handed out since reservations
	// were last settled: the free entities from the back first, then fresh
	// indices from nextFreshIndex.
	reservationCount atomic.Int64
	// nextFreshIndex is the first index in the allocation range never handed
	// out. It is one past lastIndex once the range is used up.
	nextFreshIndex int64
	// observing is set by the first end of a window.
	observing bool
	// despawnedIndices holds the indices despawned in this window, freed at
	// its end. It stays empty while the table does not observe.
	despawnedIndices container.Array[uint32]
	// despawnedOutsideRange has a bit per index outside the allocation range
	// despawned in this window, word index>>6, which place refuses until the
	// window ends: the stores' tails still hold the old entity's rows there.
	despawnedOutsideRange indexTable[uint64]
}

// entitySlot is the entity table's entry for one index.
type entitySlot[W WorldID] struct {
	// liveGeneration is the generation of the entity alive at this index, or
	// 0 if none is.
	liveGeneration uint32
	// lastGeneration is the last generation handed out or placed at this
	// index.
	lastGeneration uint32
	// componentMask holds the components the entity has, one bit per
	// ComponentID.
	componentMask W
}

// init sets the allocation range, first to last inclusive, and prepares
// slots for prepared entities from first.
func (t *entityTable[W]) init(first, last uint32, prepared int) {
	t.firstIndex = first
	t.lastIndex = last
	t.nextFreshIndex = int64(first)
	for i := range prepared {
		t.slots.atOrCreate(first + uint32(i))
	}
}

// alive reports whether e is alive. It is one load and one compare.
func (t *entityTable[W]) alive(e Entity) bool {
	slot := t.slots.at(e.index())
	return slot != nil && slot.liveGeneration == e.generation() && slot.liveGeneration != 0
}

// live yields every live entity and its slot, in index order. Despawning
// one moves no other; one spawned during the walk may or may not be yielded.
func (t *entityTable[W]) live() iter.Seq2[Entity, *entitySlot[W]] {
	return func(yield func(Entity, *entitySlot[W]) bool) {
		for p, page := range t.slots.pages {
			if page == nil {
				continue
			}
			for i := range page {
				slot := &page[i]
				if slot.liveGeneration != 0 && !yield(makeEntity(uint32(p<<indexPageShift|i), slot.liveGeneration), slot) {
					return
				}
			}
		}
	}
}

// mask answers the components e has, or false if e is not alive. It runs
// under the barrier, where no store's writer changes a mask.
func (t *entityTable[W]) mask(e Entity) (W, bool) {
	if !t.alive(e) {
		var none W
		return none, false
	}
	return t.slots.at(e.index()).componentMask, true
}

// setComponent records that alive e has component id. Writers of different
// stores may record on one entity at once.
func (t *entityTable[W]) setComponent(e Entity, id ComponentID) {
	maskSet(&t.slots.at(e.index()).componentMask, id)
}

// unsetComponent records that alive e no longer has component id.
func (t *entityTable[W]) unsetComponent(e Entity, id ComponentID) {
	maskUnset(&t.slots.at(e.index()).componentMask, id)
}

// reserve takes the next entity to hand out, which is not alive until it is
// spawned. It answers NoEntity once the allocation range is used up.
// Concurrent reserves take distinct entities; nothing else may change the
// table meanwhile.
func (t *entityTable[W]) reserve() Entity {
	reservation := t.reservationCount.Add(1) - 1
	freeCount := int64(t.freeEntities.Len())
	if reservation < freeCount {
		return *t.freeEntities.At(int(freeCount - 1 - reservation))
	}
	index := t.nextFreshIndex + reservation - freeCount
	if index > int64(t.lastIndex) {
		return NoEntity
	}
	return makeEntity(uint32(index), 1)
}

// spawn makes the next entity alive, with no components, and returns it. It
// answers NoEntity once the allocation range is used up.
func (t *entityTable[W]) spawn() Entity {
	if !t.observing {
		t.settleReservations(false)
	}
	e := t.reserve()
	if e != NoEntity {
		t.setAlive(e)
	}
	return e
}

// spawnReserved makes reserved entity e alive, and reports whether it was a
// reservation not yet spawned: its index holds no live entity and e is the
// generation the index hands out next.
func (t *entityTable[W]) spawnReserved(e Entity) bool {
	lastGeneration := uint32(0)
	if slot := t.slots.at(e.index()); slot != nil {
		if slot.liveGeneration != 0 {
			return false
		}
		lastGeneration = slot.lastGeneration
	}
	if e.generation() != nextGeneration(lastGeneration) {
		return false
	}
	t.setAlive(e)
	return true
}

// place makes e alive at its index and generation, for mirrors and saves.
// e's index must lie outside the allocation range and hold no live entity,
// nor one despawned in this window.
func (t *entityTable[W]) place(e Entity) error {
	if e.generation() == 0 {
		return fmt.Errorf("ecs: creating entity %#x, whose generation is 0", uint64(e))
	}
	if index := e.index(); index >= t.firstIndex && index <= t.lastIndex {
		return fmt.Errorf("ecs: creating entity %#x inside the allocation range", uint64(e))
	}
	if slot := t.slots.at(e.index()); slot != nil && slot.liveGeneration != 0 {
		return fmt.Errorf("ecs: creating entity %#x where one is alive", uint64(e))
	}
	if word := t.despawnedOutsideRange.at(e.index() >> 6); word != nil && *word&(1<<(e.index()&63)) != 0 {
		return fmt.Errorf("ecs: creating entity %#x where one was despawned this window", uint64(e))
	}
	t.setAlive(e)
	return nil
}

// despawn ends e and answers the components it had, for the world to
// remove, or false if e was not alive. Its index is freed at once, or at the
// end of the window if the table observes.
func (t *entityTable[W]) despawn(e Entity) (W, bool) {
	slot := t.slots.at(e.index())
	if slot == nil || slot.liveGeneration != e.generation() || slot.liveGeneration == 0 {
		var none W
		return none, false
	}
	mask := slot.componentMask
	slot.componentMask = *new(W)
	slot.liveGeneration = 0
	if t.observing {
		t.despawnedIndices.Append(e.index())
		if index := e.index(); index < t.firstIndex || index > t.lastIndex {
			*t.despawnedOutsideRange.atOrCreate(index >> 6) |= 1 << (index & 63)
		}
	} else {
		t.settleReservations(false)
		t.free(e.index())
	}
	return mask, true
}

// endWindow ends an observation window: reservations never spawned come back
// under a new generation, and the window's despawned indices are freed in
// despawn order. The first end turns observing on instead, and reports
// started.
func (t *entityTable[W]) endWindow() (started bool) {
	if !t.observing {
		t.settleReservations(false)
		t.observing = true
		return true
	}
	t.settleReservations(true)
	for i := range t.despawnedIndices.Len() {
		index := *t.despawnedIndices.At(i)
		if index < t.firstIndex || index > t.lastIndex {
			*t.despawnedOutsideRange.at(index >> 6) &^= 1 << (index & 63)
		}
		t.free(index)
	}
	t.despawnedIndices.Resize(0)
	return false
}

// setAlive makes e alive at its index and generation.
func (t *entityTable[W]) setAlive(e Entity) {
	slot := t.slots.atOrCreate(e.index())
	slot.lastGeneration = e.generation()
	slot.liveGeneration = e.generation()
}

// settleReservations folds the reservations taken since they were last
// settled into the free list and nextFreshIndex, so the free list ends at
// the next entity to hand out. It runs under the barrier, where no
// reservation is being taken.
//
// With reclaim, a reservation never spawned goes back on the free list with
// its generation burned, so the stale entity never comes alive. Only the end
// of a window reclaims: it is the one point where no reservation of the
// window still waits for its commit. Without reclaim, such an index is lost.
func (t *entityTable[W]) settleReservations(reclaim bool) {
	reservations := int(t.reservationCount.Load())
	if reservations == 0 {
		return
	}
	freeCount := t.freeEntities.Len()
	takenFree := min(reservations, freeCount)
	takenFresh := min(int64(reservations-takenFree), int64(t.lastIndex)+1-t.nextFreshIndex)
	kept := freeCount - takenFree
	if reclaim {
		// The taken entities are the last takenFree: each never spawned is
		// kept in place at its next generation.
		for i := freeCount - takenFree; i < freeCount; i++ {
			if reserved := *t.freeEntities.At(i); t.burn(reserved) {
				*t.freeEntities.At(kept) = makeEntity(reserved.index(), nextGeneration(reserved.generation()))
				kept++
			}
		}
	}
	t.freeEntities.Resize(kept)
	if reclaim {
		for i := range takenFresh {
			if reserved := makeEntity(uint32(t.nextFreshIndex+i), 1); t.burn(reserved) {
				t.freeEntities.Append(makeEntity(reserved.index(), nextGeneration(reserved.generation())))
			}
		}
	}
	t.nextFreshIndex += takenFresh
	t.reservationCount.Store(0)
}

// burn records reserved's generation as handed out at its index, so a commit
// of the stale entity is refused, and reports whether reserved was never
// spawned.
func (t *entityTable[W]) burn(reserved Entity) bool {
	slot := t.slots.atOrCreate(reserved.index())
	if slot.lastGeneration == reserved.generation() {
		return false
	}
	slot.lastGeneration = reserved.generation()
	return true
}

// free puts a despawned index on the back of the free list, at its next
// generation, if it lies in the allocation range. Reservations are settled.
func (t *entityTable[W]) free(index uint32) {
	if index >= t.firstIndex && index <= t.lastIndex {
		t.freeEntities.Append(makeEntity(index, nextGeneration(t.slots.at(index).lastGeneration)))
	}
}

// shrink releases the chunks past the free entities and the window's
// despawned indices, and the pages of despawnedOutsideRange left empty.
// Slots never shrink: each keeps its index's last
// generation, so a stale entity never comes alive.
func (t *entityTable[W]) shrink() {
	t.freeEntities.Shrink()
	t.despawnedIndices.Shrink()
	t.despawnedOutsideRange.shrink()
}
