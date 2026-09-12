package ecs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// The id authority. What it owes the rest of the design is that a handle is
// exact — a despawned one is detectably stale rather than silently addressing
// whatever took its place — and that an index returns to use immediately, which
// is what bounds every Store's flat sparse index by peak concurrent entities
// rather than by entities ever created.

func TestEntitiesHandsOutDistinctLiveHandles(t *testing.T) {
	entities := NewEntities(8)
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
	entities := NewEntities(8)
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
	entities := NewEntities(1)
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
	entities := NewEntities(8)
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
	entities := NewEntities(n)
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

// A despawn is total: it empties every Store, naming no Component type at all.
// Nothing records which Stores hold an entity, so every Store is asked — which
// is exactly why holding Entities for write is the one lock that covers a
// structural change, and why no per-entity index of "which Stores hold me" may
// ever be added.
func TestDespawnEmptiesEveryStore(t *testing.T) {
	entities := NewEntities(8)
	positions := NewStore[position](entities, 8)
	tags := NewStore[disabled](entities, 8)
	doomed, bystander := entities.alloc(), entities.alloc()
	positions.Set(doomed, position{X: 1})
	tags.Set(doomed, disabled{})
	positions.Set(bystander, position{X: 2})

	if !entities.despawn(doomed) {
		t.Fatalf("despawn(%v) = false for a live entity", doomed)
	}
	if positions.Has(doomed) {
		t.Fatalf("the despawned entity still has a position")
	}
	if tags.Has(doomed) {
		t.Fatalf("the despawned entity still carries its tag")
	}
	if positions.Len() != 1 || !positions.Has(bystander) {
		t.Fatalf("the bystander lost its position: Len() = %d", positions.Len())
	}
	if value, ok := positions.Get(bystander); !ok || value.X != 2 {
		t.Fatalf("the bystander's value reads back as %v, %v; want {2 0}, true", value, ok)
	}
}

// Reclamation is eager and is therefore nothing at all: the row goes when the
// entity goes. This is the orphan-slot case, and it is measured rather than
// argued, because without eager removal each recycle of one index leaks one
// dense entry — a thousand of them here.
func TestRecyclingAnIndexLeaksNoDenseEntry(t *testing.T) {
	const cycles = 1000
	entities := NewEntities(1)
	store := NewStore[position](entities, 1)

	peak := 0
	for i := range cycles {
		e := entities.alloc()
		store.Set(e, position{X: float32(i)})
		if store.Len() > peak {
			peak = store.Len()
		}
		if !entities.despawn(e) {
			t.Fatalf("despawn(%v) = false on cycle %d", e, i)
		}
	}

	if store.Len() != 0 {
		t.Fatalf("Len() = %d after %d recycles of one index, want 0", store.Len(), cycles)
	}
	if peak != 1 {
		t.Fatalf("the store held %d rows at its peak, want 1: one entity was ever alive", peak)
	}
	if cap(store.dense) != 1 {
		t.Fatalf("the dense array grew to %d rows over %d recycles, want 1", cap(store.dense), cycles)
	}
	if len(store.sparse) != 1 {
		t.Fatalf("the sparse index grew to %d slots, want 1: the free list bounds it", len(store.sparse))
	}
}

// Entities is the id authority, and the word World is retired from this API: a
// second simulation is a second Engine, never a second world, and CONTEXT.md
// lists World under Avoid. The rule is mechanical, so the check is too.
func TestNoIdentifierInThePackageIsAWorld(t *testing.T) {
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("listing the package's sources: %v", err)
	}
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), source, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", source, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && strings.Contains(strings.ToLower(identifier.Name), "world") {
				t.Errorf("%s names %q: the id authority is Entities and nothing in ecs is a World",
					source, identifier.Name)
			}
			return true
		})
	}
}
