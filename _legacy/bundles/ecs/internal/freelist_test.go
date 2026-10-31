package internal

import (
	"reflect"
	"slices"
	"testing"
)

// The free list while reservations are outstanding: deferred.md § Immediate
// handles, and ShrinkCmd, while reservations are outstanding. What these ask is
// that the two handles are correct in the same frame — a game that migrates one
// System to DeferredSpawn while another still spawns immediately gets the same
// Entities either way, and never the same index twice — and that ShrinkCmd
// keeps its shape while that is going on.

// TestAnImmediateSpawnTakesItsIndexThroughTheSameCursor is the first of the
// three rules, asked of the authority directly. An immediate Spawn holds the
// write lock, so it needs no atomic, but it takes its index through the same
// cursor a reservation does — over the free list newest first, then past the
// edge of the index space — which is what makes it impossible for the two to
// name one index however they are interleaved.
//
// It goes live at once, and that is the difference from a reservation: past the
// edge it grows the index space where a reservation waits for the spawn pass to
// grow it. The edge itself is pinned, so that growth does not move the region
// the reservations after it are counting into — the hand-outs stay consecutive,
// and the index space a drain leaves has no hole in it.
func TestAnImmediateSpawnTakesItsIndexThroughTheSameCursor(t *testing.T) {
	en := newEntities(8)
	made := []Entity{en.alloc(), en.alloc(), en.alloc()}
	en.despawn(made[2])
	en.despawn(made[1])
	freed := slices.Clone(en.free)

	// Interleaved as two Systems in one frame interleave: over the free list
	// first, then past the edge, with the immediate handle taking both kinds.
	first := en.alloc()
	reserved := []Entity{en.reserve()}
	second := en.alloc()
	reserved = append(reserved, en.reserve())
	third := en.alloc()

	immediate := []Entity{first, second, third}
	// The first immediate Spawn pops the newest free index, which is the cut its
	// own hand-out would have made; the reservation after it takes the one left,
	// and from there every hand-out is past the edge, one index apiece.
	wantImmediate := []uint32{freed[1], 3, 5}
	for i, e := range immediate {
		if e.idx() != wantImmediate[i] {
			t.Fatalf("immediate Spawn %d took index %d, want %d: the cursor over the free list newest first, then past the edge",
				i, e.idx(), wantImmediate[i])
		}
		if !en.Alive(e) {
			t.Fatalf("the immediate %v is not alive at the call", e)
		}
	}
	// The first immediate Spawn ran with the cursor at zero, where a pop is the
	// hand-out and its cut in one step, so only the two after it are on it.
	if got := en.reserved.Load(); got != 4 {
		t.Fatalf("two immediate Spawns and two reservations left the cursor at %d, want 4", got)
	}
	wantReserved := []uint32{freed[0], 4}
	for i, e := range reserved {
		if e.idx() != wantReserved[i] {
			t.Fatalf("reservation %d took index %d, want %d", i, e.idx(), wantReserved[i])
		}
		if en.Alive(e) {
			t.Fatalf("the reserved %v is alive before the drain", e)
		}
	}
	if slices.Contains(wantImmediate, wantReserved[0]) || slices.Contains(wantImmediate, wantReserved[1]) {
		t.Fatalf("an immediate Spawn and a reservation both took an index: immediate %v, reserved %v",
			wantImmediate, wantReserved)
	}

	en.enrolDrainSpawn(func() {
		for _, e := range reserved {
			en.spawnReserved(e)
		}
	})
	en.drain()

	for _, e := range append(slices.Clone(immediate), reserved...) {
		if !en.Alive(e) {
			t.Fatalf("%v is not alive after the drain", e)
		}
	}
	if len(en.gens) != 6 || len(en.free) != 0 {
		t.Fatalf("five hand-outs left %d generations and the free list %v, want 6 and empty: the index space grew by one per hand-out past the edge and has no hole in it",
			len(en.gens), en.free)
	}

	// An immediate Spawn with the cursor at zero grows the index space without
	// going through the cursor at all, so the edge has to follow it: a
	// reservation made after one must start past what it took, not on it.
	grown := en.alloc()
	after := en.reserve()
	if grown.idx() != 6 || after.idx() != 7 {
		t.Fatalf("an immediate Spawn with the cursor at zero took index %d and the reservation after it %d, want 6 and 7",
			grown.idx(), after.idx())
	}
}

// TestAnImmediateAndADeferredNewInOnePublicationNeverShareAnIndex is the same
// rule through the handles, in the half-migrated System the whole ticket is
// about: one Spawn and one DeferredSpawn in one signature, called alternately,
// with a free list to recycle and then past the end of the index space.
func TestAnImmediateAndADeferredNewInOnePublicationNeverShareAnIndex(t *testing.T) {
	const made = 6
	w := newReservedWorld(t)
	// A spike and its end, so the run below starts with indices to recycle: the
	// case an empty free list cannot reach.
	recycled := []Entity{w.entities.alloc(), w.entities.alloc(), w.entities.alloc()}
	for _, e := range recycled {
		w.entities.despawn(e)
	}

	var immediate, deferred []Entity
	w.composed(t, func(sp *Spawn[reservedSet], queue *DeferredSpawn[reservedSet]) {
		for i := range made {
			if i%2 == 0 {
				immediate = append(immediate, sp.New(reservedSet{Body: body{X: float32(i)}}))
				continue
			}
			deferred = append(deferred, queue.New(reservedSet{Body: body{X: float32(i)}}))
		}
	})

	indices := map[uint32]bool{}
	for _, e := range append(slices.Clone(immediate), deferred...) {
		if indices[e.idx()] {
			t.Fatalf("index %d was handed out twice: immediate %v, deferred %v", e.idx(), immediate, deferred)
		}
		indices[e.idx()] = true
	}
	for _, e := range immediate {
		if !w.entities.Alive(e) {
			t.Fatalf("the immediate %v is not alive at the call", e)
		}
	}
	for _, e := range deferred {
		if w.entities.Alive(e) {
			t.Fatalf("the deferred %v is alive before the drain", e)
		}
	}

	w.drained(t)

	for _, e := range append(slices.Clone(immediate), deferred...) {
		if !w.entities.Alive(e) {
			t.Fatalf("%v is not alive after the drain", e)
		}
		if value, ok := w.components.bodies.Get(e); !ok {
			t.Fatalf("%v carries no body after the drain, read back as %+v", e, value)
		}
	}
	if len(w.entities.gens) != made || len(w.entities.free) != 0 {
		t.Fatalf("%d interleaved Spawns left %d generations and the free list %v, want %d and empty",
			made, len(w.entities.gens), w.entities.free, made)
	}
}

// TestAnIndexFreedImmediatelyWaitsOnTheReleasedListUntilTheNextDrain is the
// second rule, through the handles: an immediate Despawn made while a
// reservation is outstanding puts its index on the released list, so nothing
// hands it out again until the drain has moved it onto the free list. Appending
// to the free list there would put the index inside the part the cursor has
// already handed out, or renumber every reservation made after it.
//
// The index is back in use from the next drain, which is at most one frame
// later because the ECS drains every Update.
func TestAnIndexFreedImmediatelyWaitsOnTheReleasedListUntilTheNextDrain(t *testing.T) {
	w := newReservedWorld(t)
	victim := w.entities.alloc()

	var reserved Entity
	w.queued(t, func(spawn *DeferredSpawn[reservedSet]) { reserved = spawn.New(reservedSet{}) })
	w.probed(t, func(it probing) {
		if !it.we.Despawn(victim) {
			t.Fatalf("the immediate Despawn of %v missed", victim)
		}
	})

	if slices.Contains(w.entities.free, victim.idx()) {
		t.Fatalf("index %d went onto the free list %v while a reservation was outstanding",
			victim.idx(), w.entities.free)
	}
	if !slices.Contains(w.entities.released, victim.idx()) {
		t.Fatalf("index %d is not on the released list %v after an immediate Despawn while a reservation was outstanding",
			victim.idx(), w.entities.released)
	}
	var later Entity
	w.queued(t, func(spawn *DeferredSpawn[reservedSet]) { later = spawn.New(reservedSet{}) })
	if later.idx() == victim.idx() || later.idx() == reserved.idx() {
		t.Fatalf("a reservation made after the immediate Despawn took index %d, which %v and %v hold",
			later.idx(), victim, reserved)
	}

	w.drained(t)

	if len(w.entities.released) != 0 {
		t.Fatalf("the drain left %v on the released list, want it moved onto the free list", w.entities.released)
	}
	if !slices.Contains(w.entities.free, victim.idx()) {
		t.Fatalf("index %d is not on the free list %v after the drain", victim.idx(), w.entities.free)
	}
	var next Entity
	w.composed(t, func(sp *Spawn[reservedSet], _ *DeferredSpawn[reservedSet]) {
		next = sp.New(reservedSet{})
	})
	if next.idx() != victim.idx() {
		t.Fatalf("the Spawn after the drain took index %d, want the released %d recycled normally",
			next.idx(), victim.idx())
	}
}

// TestTheSettleCutsTheFreeListBeforeItMovesTheReleasedListOn fixes the order of
// the settle's three steps, which is the whole reason the despawn pass needs no
// special case: cut the free list back to the cursor, reset the cursor, then
// move the released list on. Moving first would put the released indices inside
// the cut and lose them, and the index left free below would be one the frame
// had already handed out.
func TestTheSettleCutsTheFreeListBeforeItMovesTheReleasedListOn(t *testing.T) {
	en := newEntities(8)
	made := []Entity{en.alloc(), en.alloc(), en.alloc()}
	en.despawn(made[0])

	reserved := en.reserve()
	if reserved.idx() != made[0].idx() {
		t.Fatalf("the reservation took index %d, want the one free index %d", reserved.idx(), made[0].idx())
	}
	en.despawn(made[1])
	if !slices.Equal(en.free, []uint32{made[0].idx()}) || !slices.Equal(en.released, []uint32{made[1].idx()}) {
		t.Fatalf("before the drain the free list is %v and the released list %v, want %v and %v",
			en.free, en.released, []uint32{made[0].idx()}, []uint32{made[1].idx()})
	}
	en.enrolDrainSpawn(func() { en.spawnReserved(reserved) })

	en.drain()

	if !slices.Equal(en.free, []uint32{made[1].idx()}) {
		t.Fatalf("the settle left the free list %v, want only the released index %d: the cut comes first",
			en.free, made[1].idx())
	}
	if len(en.released) != 0 || en.reserved.Load() != 0 {
		t.Fatalf("the settle left %v released and the cursor at %d, want nothing and 0",
			en.released, en.reserved.Load())
	}
	if next := en.alloc(); next.idx() != made[1].idx() {
		t.Fatalf("the Spawn after the drain took index %d, want the released %d", next.idx(), made[1].idx())
	}
}

// TestAnIndexFreedByTheDespawnPassIsAvailableToTheVeryNextReservation is what
// the settle sitting between the passes buys: by the time the despawn pass
// runs, the cursor is reset and the free list is ordinary again, so the pass is
// literally today's despawn — its index goes straight onto the free list, not
// onto the released one, and the next reservation takes it.
func TestAnIndexFreedByTheDespawnPassIsAvailableToTheVeryNextReservation(t *testing.T) {
	en := newEntities(8)
	made := []Entity{en.alloc(), en.alloc()}
	doomed := made[1]
	// A reservation outstanding across the drain, so the cursor the despawn pass
	// runs under is one the settle reset rather than one that was never moved.
	reserved := en.reserve()
	en.enrolDrainSpawn(func() { en.spawnReserved(reserved) })
	en.enrolDrainDespawn(func() {
		if !en.despawn(doomed) {
			t.Errorf("the despawn pass missed %v", doomed)
		}
	})

	en.drain()

	if len(en.released) != 0 {
		t.Fatalf("the despawn pass put %v on the released list, want the free list: the settle has already reset the cursor",
			en.released)
	}
	if !slices.Equal(en.free, []uint32{doomed.idx()}) {
		t.Fatalf("the despawn pass left the free list %v, want only index %d", en.free, doomed.idx())
	}
	if next := en.reserve(); next.idx() != doomed.idx() {
		t.Fatalf("the reservation after the drain took index %d, want the index the despawn pass freed, %d",
			next.idx(), doomed.idx())
	}
}

// TestAShrinkDropsNoIndicesWhileAReservationIsOutstanding is the third rule.
// ShrinkCmd keeps its shape and its response: its index step drops nothing and
// reports 0 bytes for Entities while any reservation is outstanding, because
// the free list is holding indices the cursor has handed out and cutting the
// index space would drop it out from under a Reserved Entity. Stores, scratch
// and Hook logs shrink as they always did.
//
// A reservation lasts only until the next drain, so the Command an app sends
// between frames sees the cursor at zero and is the shrink it always was.
func TestAShrinkDropsNoIndicesWhileAReservationIsOutstanding(t *testing.T) {
	en := newEntities(8)
	var stores, scratch, hooks int
	en.enrol(func(Entity) bool { return false }, func() uintptr { stores++; return 8 })
	en.enrolScratch(func() uintptr { scratch++; return 4 })
	en.enrolHooks(func() uintptr { hooks++; return 2 })
	made := []Entity{en.alloc(), en.alloc(), en.alloc()}
	en.despawn(made[2])
	en.despawn(made[1])
	reserved := en.reserve()
	gens, free := slices.Clone(en.gens), slices.Clone(en.free)
	capacities := [2]int{cap(en.gens), cap(en.free)}

	outstanding := en.shrink(ShrinkRequest{})

	if outstanding.Entities != 0 {
		t.Fatalf("a shrink with a reservation outstanding reported %d Entities bytes, want 0", outstanding.Entities)
	}
	if outstanding.Stores != 8 || outstanding.Scratch != 4 || outstanding.Hooks != 2 {
		t.Fatalf("a shrink with a reservation outstanding reported %+v, want the other three areas shrunk as ever",
			outstanding)
	}
	if stores != 1 || scratch != 1 || hooks != 1 {
		t.Fatalf("a shrink with a reservation outstanding called %d Store, %d scratch and %d Hook releases, want 1 each",
			stores, scratch, hooks)
	}
	if !slices.Equal(en.gens, gens) || !slices.Equal(en.free, free) ||
		cap(en.gens) != capacities[0] || cap(en.free) != capacities[1] {
		t.Fatalf("a shrink with a reservation outstanding left generations %v and free list %v, want %v and %v untouched",
			en.gens, en.free, gens, free)
	}

	en.enrolDrainSpawn(func() { en.spawnReserved(reserved) })
	en.drain()
	if !en.Alive(reserved) {
		t.Fatalf("the reserved %v did not come to life after a shrink it survived", reserved)
	}

	settled := en.shrink(ShrinkRequest{})

	if settled.Entities == 0 {
		t.Fatalf("a shrink with the cursor at zero reported no Entities bytes, want the free index at the top dropped")
	}
	if len(en.gens) != 2 {
		t.Fatalf("the settled shrink left an index space of %d, want 2: the one free index at the top dropped",
			len(en.gens))
	}
}

// TestShrinkResponseKeepsItsShape is the rest of "ShrinkCmd keeps its shape and
// its response": deferral adds no area, and the released list is Entities
// memory like the free list it is waiting to join.
func TestShrinkResponseKeepsItsShape(t *testing.T) {
	response := reflect.TypeFor[ShrinkResponse]()
	var got []string
	for i := range response.NumField() {
		got = append(got, response.Field(i).Name)
	}
	if want := []string{"Hooks", "Stores", "Entities", "Scratch"}; !slices.Equal(got, want) {
		t.Fatalf("ShrinkResponse carries %v, want %v", got, want)
	}
}

// TestTheReleasedListIsAllocationFreeInSteadyState keeps requirement 1 over the
// path this ticket adds. A frame that reserves, spawns immediately, despawns
// immediately and drains reaches a steady state where the released list and the
// free list both hold their capacity, so the settle's move is a copy into a
// buffer that is already there.
func TestTheReleasedListIsAllocationFreeInSteadyState(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	const n = 64
	en := newEntities(n)
	reserved := make([]Entity, 0, n)
	live := make([]Entity, 0, n)
	en.enrolDrainSpawn(func() {
		for _, e := range reserved {
			en.spawnReserved(e)
		}
	})
	round := func() {
		reserved = reserved[:0]
		for range n {
			reserved = append(reserved, en.reserve())
		}
		live = live[:0]
		for range n {
			live = append(live, en.alloc())
		}
		// Immediate Despawns while the reservations are outstanding: every one of
		// these lands on the released list and the settle moves it.
		for _, e := range live {
			en.despawn(e)
		}
		en.drain()
		for _, e := range reserved {
			en.despawn(e)
		}
	}
	for range 5 {
		round()
	}

	if allocs := testing.AllocsPerRun(10, round); allocs != 0 {
		t.Fatalf("a round of %d reservations, %d immediate Spawns and %d immediate Despawns allocated %v times, want 0",
			n, n, n, allocs)
	}
}
