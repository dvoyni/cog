package ecs

import (
	"reflect"
	"runtime"
	"testing"
	"unsafe"
)

type position struct{ X, Y float32 }

// disabled is a Tag: a Component with no fields, whose presence is the whole of
// what it says.
type disabled struct{}

func TestAStoreHoldsOneValuePerEntity(t *testing.T) {
	entities := NewEntities(8)
	store := NewStore[position](entities, 8)
	a, b := entities.alloc(), entities.alloc()

	store.Set(a, position{X: 1})
	store.Set(b, position{X: 2})
	if store.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", store.Len())
	}
	if value, ok := store.Get(a); !ok || value.X != 1 {
		t.Fatalf("Get(a) = %v, %v; want {1 0}, true", value, ok)
	}

	// An entity holds at most one Component of a given type: a sparse set has
	// exactly one slot per entity, so a second Set replaces the value rather
	// than adding a row.
	store.Set(a, position{X: 9})
	if store.Len() != 2 {
		t.Fatalf("Len() = %d after overwriting one entity's value, want 2", store.Len())
	}
	if value, _ := store.Get(a); value.X != 9 {
		t.Fatalf("Get(a).X = %v after overwrite, want 9", value.X)
	}
}

func TestAWriteThroughRefLands(t *testing.T) {
	entities := NewEntities(8)
	store := NewStore[position](entities, 8)
	e := entities.alloc()
	store.Set(e, position{X: 1})

	ref, ok := store.Ref(e)
	if !ok {
		t.Fatalf("Ref(e) = _, false for an entity the store holds")
	}
	ref.X = 42
	if value, _ := store.Get(e); value.X != 42 {
		t.Fatalf("Get(e).X = %v after a write through Ref, want 42", value.X)
	}
}

// The probe is one load of the sparse slot and one compare of the generation
// half, and the compare that finds the row is the compare that rejects a stale
// handle. This store is enrolled with an Entities it never asks anything: there
// is no liveness structure to consult, because the generation is in the slot.
func TestAStaleHandleFailsMembershipWithNothingElseConsulted(t *testing.T) {
	entities := NewEntities(8)
	store := NewStore[position](entities, 8)
	e := entities.alloc()
	store.Set(e, position{X: 1})

	if !store.Has(e) {
		t.Fatalf("Has(e) = false for the entity just set")
	}
	store.Remove(e)
	if store.Has(e) {
		t.Fatalf("Has(e) = true after Remove")
	}

	// The same index at the next generation is a different entity, and the
	// store holds nothing for it — the handle that came before does not alias
	// it in either direction.
	recycled := newEntity(e.idx(), e.gen()+1)
	if store.Has(recycled) {
		t.Fatalf("Has(recycled) = true before anything was set for it")
	}
	store.Set(recycled, position{X: 2})
	if store.Has(e) {
		t.Fatalf("the stale handle %v found the recycled entity's row", e)
	}
	if value, ok := store.Get(recycled); !ok || value.X != 2 {
		t.Fatalf("Get(recycled) = %v, %v; want {2 0}, true", value, ok)
	}
}

// The probe answers about any Entity value, including ones no authority issued.
// A Query probes candidates it did not produce, so a panic here would be a
// panic in the middle of a frame.
func TestTheProbeIsTotal(t *testing.T) {
	entities := NewEntities(8)
	store := NewStore[position](entities, 4)
	cases := []struct {
		name string
		e    Entity
	}{
		{"NoEntity", NoEntity},
		{"an index beyond the sparse index", newEntity(1_000_000, 1)},
		{"an index inside it that was never set", newEntity(2, 1)},
	}
	for _, test := range cases {
		if store.Has(test.e) {
			t.Fatalf("Has(%s) = true, want false", test.name)
		}
		if _, ok := store.Get(test.e); ok {
			t.Fatalf("Get(%s) = _, true; want false", test.name)
		}
		if _, ok := store.Ref(test.e); ok {
			t.Fatalf("Ref(%s) = _, true; want false", test.name)
		}
		if store.Remove(test.e) {
			t.Fatalf("Remove(%s) = true, want false", test.name)
		}
	}
}

// Removal moves the last row into the hole, so the packed arrays never contain
// holes and len(owners) is exactly the population — which is the number driver
// selection reads.
func TestRemoveIsSwapRemoveAndThePopulationStaysExact(t *testing.T) {
	entities := NewEntities(8)
	store := NewStore[position](entities, 8)
	live := make([]Entity, 4)
	for i := range live {
		live[i] = entities.alloc()
		store.Set(live[i], position{X: float32(i)})
	}

	if !store.Remove(live[1]) {
		t.Fatalf("Remove(live[1]) = false")
	}
	if store.Len() != 3 {
		t.Fatalf("Len() = %d after one removal of four, want 3", store.Len())
	}
	// The row that moved is the last one, and it kept its value and its owner.
	if value, ok := store.Get(live[3]); !ok || value.X != 3 {
		t.Fatalf("the relocated entity reads back as %v, %v; want {3 0}, true", value, ok)
	}
	for i, e := range store.owners {
		if value := store.dense[i]; !store.Has(e) || store.sparse[e.idx()] != uint64(e.gen())<<32|uint64(i) {
			t.Fatalf("owners[%d] = %v is not indexed back to row %d (value %v)", i, e, i, value)
		}
	}
	if store.Remove(live[1]) {
		t.Fatalf("Remove(live[1]) = true the second time, want false")
	}
}

// Nothing shrinks: no compaction, no sweep, and no array handed back. A
// respawn reuses the row rather than buying it again.
func TestNothingShrinks(t *testing.T) {
	entities := NewEntities(64)
	store := NewStore[position](entities, 0)
	live := make([]Entity, 64)
	for i := range live {
		live[i] = entities.alloc()
		store.Set(live[i], position{X: float32(i)})
	}
	grown := cap(store.dense)

	for _, e := range live {
		store.Remove(e)
	}
	if store.Len() != 0 {
		t.Fatalf("Len() = %d after removing everything, want 0", store.Len())
	}
	if cap(store.dense) != grown {
		t.Fatalf("dense capacity went from %d to %d on removal: something shrank",
			grown, cap(store.dense))
	}
}

// The reserve hint is what an app that knows its peak buys the preallocated
// column with: three allocations for the three arrays and nothing during the
// fill, against append doubling's repeated regrowth.
func TestTheReserveHintBuysAnAllocationFreeFill(t *testing.T) {
	const n = 10000
	entities := NewEntities(n)
	ids := make([]Entity, n)
	for i := range ids {
		ids[i] = entities.alloc()
	}

	reservedStore := NewStore[position](entities, n)
	grownStore := NewStore[position](entities, 0)
	fill := func(store *Store[position]) func() {
		return func() {
			for _, e := range ids {
				store.Set(e, position{})
			}
		}
	}
	reserved := allocationsDuring(fill(reservedStore))
	grown := allocationsDuring(fill(grownStore))

	if reserved != 0 {
		t.Fatalf("filling a reserved store cost %d allocations, want none", reserved)
	}
	if grown == 0 {
		t.Fatalf("filling an unreserved store cost no allocation: the two arms are not measuring growth")
	}
	t.Logf("filling %d rows: reserved %d allocations, growing from empty %d", n, reserved, grown)
}

func allocationsDuring(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.Mallocs - before.Mallocs
}

// The type-erased half exists for exactly one caller — a despawn, which must
// empty every Store while naming no Component type. One method is the whole
// interface, so nothing else can ever be reached through it; driver selection
// reads len(owners) through the typed path.
func TestStoreCoreCarriesExactlyOneMethod(t *testing.T) {
	var _ storeCore = (*Store[position])(nil)
	core := reflect.TypeFor[storeCore]()
	if n := core.NumMethod(); n != 1 {
		t.Fatalf("storeCore carries %d methods, want exactly 1", n)
	}
	if name := core.Method(0).Name; name != "remove" {
		t.Fatalf("storeCore's one method is %q, want %q", name, "remove")
	}
}

// A Tag's Store is its sparse index and its owners: there is nothing to store,
// so the dense array costs no memory however many entities carry the Tag.
func TestATagStoreCarriesMembershipAndNoData(t *testing.T) {
	entities := NewEntities(64)
	store := NewStore[disabled](entities, 64)
	e := entities.alloc()
	store.Set(e, disabled{})

	if !store.Has(e) {
		t.Fatalf("Has(e) = false for a tagged entity")
	}
	if store.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", store.Len())
	}
	if bytes := uintptr(cap(store.dense)) * unsafe.Sizeof(disabled{}); bytes != 0 {
		t.Fatalf("a tag store's dense array holds %d bytes, want 0", bytes)
	}
}

// Swap-remove is what makes a forward walk skip, and this is the measurement
// the reverse walk of the next ticket rests on: the tail row drops into the
// hole at i and i++ steps straight over it.
func TestAForwardWalkWithSwapRemoveSkips(t *testing.T) {
	store, _ := filledStore(t, 1000)
	seen := map[Entity]bool{}
	for i := 0; i < len(store.owners); i++ {
		e := store.owners[i]
		seen[e] = true
		if int(store.dense[i].X)%2 == 0 {
			store.Remove(e)
		}
	}
	if len(seen) != 667 {
		t.Fatalf("a forward walk removing every even entity visited %d of 1000, want 667", len(seen))
	}
}

func TestAReverseWalkWithSwapRemoveVisitsEveryone(t *testing.T) {
	store, _ := filledStore(t, 1000)
	seen := map[Entity]bool{}
	for i := len(store.owners) - 1; i >= 0; i-- {
		e := store.owners[i]
		seen[e] = true
		if int(store.dense[i].X)%2 == 0 {
			store.Remove(e)
		}
	}
	if len(seen) != 1000 {
		t.Fatalf("a reverse walk visited %d of 1000, want all of them", len(seen))
	}
	if store.Len() != 500 {
		t.Fatalf("Len() = %d after removing every even entity, want 500", store.Len())
	}
}

func filledStore(t *testing.T, n int) (*Store[position], *Entities) {
	t.Helper()
	entities := NewEntities(uint32(n))
	store := NewStore[position](entities, uint32(n))
	for i := range n {
		store.Set(entities.alloc(), position{X: float32(i)})
	}
	return store, entities
}
