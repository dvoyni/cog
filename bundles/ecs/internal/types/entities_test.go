package types

import "testing"

// The id authority. What it owes the rest of the design is that a handle is
// exact — a despawned one is detectably stale rather than silently addressing
// whatever took its place — and that an index returns to use immediately, which
// is what bounds every Store's flat sparse index by peak concurrent entities
// rather than by entities ever created. What it owes the Stores enrolled with
// it is tested in enrolment_test.go.

func TestEntitiesHandsOutDistinctLiveHandles(t *testing.T) {
	entities := newEntities(8)
	seen := map[Entity]bool{}
	for range 100 {
		e := entities.alloc()
		if e == NoEntity {
			t.Fatalf("alloc returned NoEntity: generations start at 1 so that cannot happen")
		}
		if seen[e] {
			t.Fatalf("alloc returned %v twice", e)
		}
		if !entities.Alive(e) {
			t.Fatalf("%v is not Alive immediately after alloc", e)
		}
		seen[e] = true
	}
}

func TestDespawnRetiresTheHandleAndRecyclesTheIndex(t *testing.T) {
	entities := newEntities(8)
	first := entities.alloc()

	if !entities.despawn(first) {
		t.Fatalf("despawn(%v) = false, want true for a live entity", first)
	}
	if entities.Alive(first) {
		t.Fatalf("%v is still Alive after despawn", first)
	}
	if entities.despawn(first) {
		t.Fatalf("despawn(%v) = true the second time, want false", first)
	}

	next := entities.alloc()
	if next.idx() != first.idx() {
		t.Fatalf("index %d was not recycled: the next alloc took %d", first.idx(), next.idx())
	}
	if next == first {
		t.Fatalf("the recycled handle equals the retired one (%v): the generation did not move", next)
	}
	if entities.Alive(first) {
		t.Fatalf("the retired handle %v came back Alive once its index was recycled", first)
	}
}

// Recycling is the whole reason a flat sparse index is affordable: an app that
// spawns and despawns for an hour uses as many indices as it ever had alive at
// once.
func TestTheIndexSpaceIsBoundedByPeakConcurrentEntities(t *testing.T) {
	entities := newEntities(1)
	for range 1000 {
		e := entities.alloc()
		if !entities.despawn(e) {
			t.Fatalf("despawn(%v) = false", e)
		}
	}
	if used := len(entities.gens); used != 1 {
		t.Fatalf("1000 spawn/despawn cycles used %d indices, want 1", used)
	}
}

// TestAFabricatedHandleAtAFreeIndexsNextGenerationIsNotAlive names the blind
// spot the free bit closes. A despawn steps the generation as it retires the
// index, so a free index already stores the generation it will carry when it is
// allocated again; without the bit, a handle fabricated from that index and
// that generation compared equal and answered Alive true. It is the handle a
// deferred Spawn's Reserved Entity will be — deferred.md § Alive is false for a
// reservation until the drain — and it must be dead until the drain hands it
// out.
func TestAFabricatedHandleAtAFreeIndexsNextGenerationIsNotAlive(t *testing.T) {
	entities := newEntities(8)
	first := entities.alloc()
	entities.despawn(first)

	next := newEntity(first.idx(), first.gen()+1)
	if entities.Alive(next) {
		t.Fatalf("%v, the handle free index %d will carry next, is Alive before anything allocates it", next, first.idx())
	}
	if entities.despawn(next) {
		t.Fatalf("despawn(%v) = true for a handle nothing holds yet", next)
	}

	// The same word, once the index goes live, is the handle that index holds:
	// the bit is what the free index carried, not a different generation.
	if reused := entities.alloc(); reused != next {
		t.Fatalf("the recycled index came back as %v, want %v: the free bit moved the generation", reused, next)
	}
	if !entities.Alive(next) {
		t.Fatalf("%v is not Alive although it is the handle the recycled index was allocated at", next)
	}
}

// The bit itself: set on the generation of a free index, clear on a live one.
// Alive is that word compared against the handle's, and nothing else, so this
// is the whole of what makes the case above work.
func TestAFreeIndexCarriesTheFreeBitAndGoingLiveClearsIt(t *testing.T) {
	entities := newEntities(8)
	e := entities.alloc()
	if generation := entities.gens[e.idx()]; generation&freeGeneration != 0 {
		t.Fatalf("index %d carries generation %#x while it is live, want the free bit clear", e.idx(), generation)
	}

	entities.despawn(e)
	if generation := entities.gens[e.idx()]; generation&freeGeneration == 0 {
		t.Fatalf("index %d carries generation %#x while it is free, want the free bit set", e.idx(), generation)
	}

	reused := entities.alloc()
	if generation := entities.gens[reused.idx()]; generation&freeGeneration != 0 {
		t.Fatalf("index %d carries generation %#x once it is live again, want the free bit clear", reused.idx(), generation)
	}
	if reused.gen()&freeGeneration != 0 {
		t.Fatalf("the handle %v carries the free bit: no issued handle ever may", reused)
	}
}

// A generation steps inside the live half only. One that reached the free half
// would make a live entity look free to Alive, and the all-ones generation a
// Store writes into an empty sparse slot lives in that half too, which is why
// nextGeneration no longer names it.
func TestAGenerationNeverStepsIntoTheFreeHalf(t *testing.T) {
	for _, g := range []uint32{1, 2, freeGeneration - 3, freeGeneration - 2, freeGeneration - 1} {
		next := nextGeneration(g)
		if next == 0 || next == absentGeneration || next&freeGeneration != 0 {
			t.Fatalf("nextGeneration(%#x) = %#x, want a generation a live entity may carry", g, next)
		}
	}
	if next := nextGeneration(freeGeneration - 1); next != 1 {
		t.Fatalf("nextGeneration at the top of the live half = %d, want it to wrap to 1", next)
	}
}

func TestAliveRejectsNoEntityAndHandlesItNeverIssued(t *testing.T) {
	entities := newEntities(8)
	entities.alloc()
	cases := []struct {
		name string
		e    Entity
	}{
		{"NoEntity", NoEntity},
		{"an index never allocated", newEntity(500, 1)},
		{"a generation never issued", newEntity(0, 9)},
		{"a live index at its generation with the free bit", newEntity(0, freeGeneration|1)},
	}
	for _, test := range cases {
		if entities.Alive(test.e) {
			t.Fatalf("Alive(%s) = true, want false", test.name)
		}
		if entities.despawn(test.e) {
			t.Fatalf("despawn(%s) = true, want false", test.name)
		}
	}
}

// Requirement 1 is zero heap allocation on the hot path, and a spawn-heavy
// frame is on it. Once the free list has indices, allocation is a pop and a
// despawn is a push.
func TestAllocationAndDespawnAreAllocationFreeInSteadyState(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	const n = 256
	entities := newEntities(n)
	live := make([]Entity, 0, n)
	for range n {
		live = append(live, entities.alloc())
	}
	for _, e := range live {
		entities.despawn(e)
	}
	live = live[:0]

	allocs := testing.AllocsPerRun(10, func() {
		for range n {
			live = append(live, entities.alloc())
		}
		for _, e := range live {
			entities.despawn(e)
		}
		live = live[:0]
	})
	if allocs != 0 {
		t.Fatalf("a spawn/despawn round of %d entities allocated %v times, want 0", n, allocs)
	}
}
