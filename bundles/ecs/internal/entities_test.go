package internal

import "testing"

// The id authority. What it owes the rest of the design is that a handle is
// exact — a despawned one is detectably stale rather than silently addressing
// whatever took its place — and that an index returns to use immediately, which
// is what bounds every Store's flat sparse index by peak concurrent entities
// rather than by entities ever created. What it owes the Stores enrolled with
// it is tested in the ecs root, where a Store exists.

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
