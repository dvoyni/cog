package sbench

import (
	"math/rand"
	"testing"
)

// If Write[*Entities] is the lock that covers every Store, then Entities holds
// a reference to every Store and a Despawn walks them all. Go generics cannot
// hold []*storeB[T] for heterogeneous T, so the registry is a slice of an
// interface, and every Store visited costs a dynamic dispatch on top of the
// random load into its index.
//
// This measures that shape against the direct one, and against the two
// alternatives that would avoid the scan.

// anyStore is the erased view Entities keeps. Despawn needs exactly one method;
// remove already no-ops on a generation mismatch, so no separate has() call is
// needed.
type anyStore interface {
	remove(e Entity)
	length() int
}

func (s *storeB[T]) length() int { return len(s.dense) }

type registry struct {
	gen    []uint32
	stores []anyStore
}

// despawnScan is the whole of an eager Despawn: bump the generation, then ask
// every Store to drop the entity. No Store is consulted about whether it holds
// the entity first — remove is already that test.
func (r *registry) despawnScan(e Entity) {
	r.gen[e.idx()]++
	for _, st := range r.stores {
		st.remove(e)
	}
}

func setupRegistry(nStores, nEntities int, tb testing.TB) (*registry, []Entity) {
	space := uint32(nEntities * 2)
	r := &registry{gen: make([]uint32, space)}
	typed := make([]*storeB[Body], nStores)
	for i := range typed {
		typed[i] = newB[Body](space)
		r.stores = append(r.stores, typed[i])
	}
	ids := make([]Entity, nEntities)
	rng := rand.New(rand.NewSource(11))
	for i := range ids {
		ids[i] = mkEntity(uint32(i), 1)
		r.gen[i] = 1
		// A real entity is in a handful of Stores, not all of them.
		for k := 0; k < 5 && k < nStores; k++ {
			st := typed[rng.Intn(nStores)]
			if !st.has(ids[i]) {
				st.add(ids[i], Body{X: float64(i)})
			}
		}
	}
	return r, ids
}

func benchDespawnRegistry(b *testing.B, nStores int) {
	r, ids := setupRegistry(nStores, structN, b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		e := ids[i%len(ids)]
		// Re-add so the Stores stay populated: measure the scan, not a
		// draining Store. Only the removal is timed work either way.
		r.despawnScan(e)
	}
}

func BenchmarkDespawnRegistry_8(b *testing.B)  { benchDespawnRegistry(b, 8) }
func BenchmarkDespawnRegistry_85(b *testing.B) { benchDespawnRegistry(b, 85) }

// The same scan without the interface, to price the dynamic dispatch alone.
func benchDespawnDirect(b *testing.B, nStores int) {
	space := uint32(structN * 2)
	typed := make([]*storeB[Body], nStores)
	for i := range typed {
		typed[i] = newB[Body](space)
	}
	ids := make([]Entity, structN)
	rng := rand.New(rand.NewSource(11))
	for i := range ids {
		ids[i] = mkEntity(uint32(i), 1)
		for k := 0; k < 5 && k < nStores; k++ {
			st := typed[rng.Intn(nStores)]
			if !st.has(ids[i]) {
				st.add(ids[i], Body{X: float64(i)})
			}
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		e := ids[i%len(ids)]
		for _, st := range typed {
			st.remove(e)
		}
	}
}

func BenchmarkDespawnDirect_8(b *testing.B)  { benchDespawnDirect(b, 8) }
func BenchmarkDespawnDirect_85(b *testing.B) { benchDespawnDirect(b, 85) }

// Eager despawn leaves no dead entity in any Store, so a Query pays no
// per-candidate liveness check and len(owners) stays the exact population that
// cog#239's driver selection depends on. This asserts both.
func TestEagerDespawnLeavesNoResidue(t *testing.T) {
	r, ids := setupRegistry(8, 100, t)
	before := 0
	for _, st := range r.stores {
		before += st.length()
	}
	for _, e := range ids[:50] {
		r.despawnScan(e)
	}
	after := 0
	for _, st := range r.stores {
		after += st.length()
	}
	if after >= before {
		t.Fatalf("despawn freed nothing: %d -> %d", before, after)
	}
	// No Store may still resolve a despawned entity.
	for _, e := range ids[:50] {
		for i, st := range r.stores {
			if s, ok := st.(*storeB[Body]); ok && s.has(e) {
				t.Fatalf("store %d still holds despawned entity %v", i, e)
			}
		}
	}
	// And every survivor is still reachable.
	live := 0
	for _, e := range ids[50:] {
		for _, st := range r.stores {
			if s, ok := st.(*storeB[Body]); ok && s.has(e) {
				live++
			}
		}
	}
	if live == 0 {
		t.Fatal("despawn removed survivors too")
	}
}
