package ecs

import (
	"unsafe"

	"github.com/dvoyni/cog/kernel"
)

// Delta is what happened to one term of an entity in an observation window,
// since the last observation reset.
type Delta uint8

const (
	// DeltaNone is a term untouched, or written back to its value as of the
	// reset. The comparison is bytewise, so -0.0 and 0.0 differ.
	DeltaNone Delta = iota
	DeltaAdded
	DeltaChanged
	DeltaRemoved
)

// ObservationReset is the system that ends world W's observation window,
// registered with RegisterTask1(ecs.ObservationReset[Game]) and scheduled
// where the frame needs it: first, last, after the network send, after the
// renderer has read its deltas. It takes the barrier. Every observed
// store's shadows take their rows' values and its removed rows are dropped;
// the window's despawned indices, and reservations never spawned, go back
// to the free list.
//
// Its first run turns observation on, and every row present then counts as
// added in the first window. A world it never runs in observes nothing and
// keeps nothing for observers: a despawned index is reused at once.
func ObservationReset[W WorldID](k *kernel.Kernel, _ kernel.NoArg, world World[W]) kernel.NoResult {
	world.world.resetObservation()
	return kernel.NoResult{}
}

// declareDeltaQuery checks that terms can make a delta query, and registers
// an observer of each value term's store. It is a composition error, and
// panics, for a term to write or be optional, or for no term to be a value.
func declareDeltaQuery(terms []term) {
	values := false
	for _, t := range terms {
		switch t.mode {
		case termWrite:
			panic("ecs: a delta query term writes its store: delta query terms are values")
		case termOptionalRead, termOptionalWrite:
			panic("ecs: a delta query term is optional: delta query terms are values")
		case termRead:
			values = true
			t.store.registerObserver()
		}
	}
	if !values {
		panic("ecs: a delta query of With and Without terms only")
	}
}

// deltaWalk is one walk of a delta query's terms. Each value term drives in
// turn, through every row of its store, live and in the tail; the other
// terms are probed by entity. An entity is yielded by the first term with a
// delta, so each comes once.
type deltaWalk struct {
	// addresses and previous hold the address of each term's value and of
	// its value as of the reset, nil if it was added, in the entity last
	// matched: a filter's are tagRow.
	addresses, previous [8]unsafe.Pointer
	deltas              [8]Delta
}

// observed reports whether every value term's store observes. They start
// together at the world's first observation reset, before which a delta
// query yields nothing.
func (w *deltaWalk) observed(terms []term) bool {
	for _, t := range terms {
		if t.mode == termRead && !t.store.observed() {
			return false
		}
	}
	return true
}

// matches reports whether the owner of drivingRow, in the driving term's
// store, is yielded there: it has or had every value term this window, has
// or lacks each filter's component now, has a delta at the driving term and
// none at the value terms before it. It fills the walk's addresses,
// previous values and deltas.
func (w *deltaWalk) matches(terms []term, drivingTerm, drivingRow int) bool {
	drivingStore := terms[drivingTerm].store
	drivingDelta := drivingStore.delta(drivingRow)
	if drivingDelta == DeltaNone {
		return false
	}
	e := *drivingStore.owners.At(drivingRow)
	for i, t := range terms {
		if t.mode != termRead {
			if _, ok := t.store.row(e); ok == (t.mode == termWithout) {
				return false
			}
			w.addresses[i] = unsafe.Pointer(&tagRow)
			w.previous[i] = unsafe.Pointer(&tagRow)
			w.deltas[i] = DeltaNone
			continue
		}
		row := drivingRow
		delta := drivingDelta
		if i != drivingTerm {
			var ok bool
			if row, ok = t.store.reportedRow(e); !ok {
				return false
			}
			delta = t.store.delta(row)
			if i < drivingTerm && delta != DeltaNone {
				return false
			}
		}
		w.addresses[i] = t.store.address(row)
		w.previous[i] = nil
		if delta != DeltaAdded {
			w.previous[i] = t.store.shadow(row)
		}
		w.deltas[i] = delta
	}
	return true
}

// previousValue converts the address of a term's value as of the reset to
// term type T, the zero T if it is nil.
func previousValue[T any](address unsafe.Pointer) T {
	if address == nil {
		var zero T
		return zero
	}
	return *(*T)(address)
}
