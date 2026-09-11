package sbench

import (
	"math/rand"
	"testing"
)

// A Query reaches only the entities it drives over. Following an Entity stored
// in a Component — a projectile's target, a light's owner, a child's parent —
// needs random access: Get[T] for a copy under read{*Store[T]}, Set[T] for a
// pointer under write{*Store[T]}. Both are statically nameable, so they are
// ordinary handles in the System's signature like Query and Spawn.
//
// Three things to establish: what following a reference costs against reading
// the same Component as a Query field, that a reference to a despawned entity
// is safe without any liveness check of its own, and that a recycled index does
// not resolve a stale reference to the wrong entity.

// Homing is the nox shape: a missile that steers toward a target it holds a
// handle to.
type Homing struct {
	Target Entity
	Turn   float64
}

func setupRefs(n int, scatter bool) (*storeB[Homing], *storeB[Body], []Entity) {
	space := uint32(n * 2)
	hs, bs := newB[Homing](space), newB[Body](space)
	ids := make([]Entity, n)
	for i := range ids {
		ids[i] = mkEntity(uint32(i), 1)
	}
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	if scatter {
		rng := rand.New(rand.NewSource(19))
		rng.Shuffle(n, func(i, j int) { order[i], order[j] = order[j], order[i] })
	}
	// Every entity has a Body; the missiles target a scattered other entity.
	for _, i := range order {
		bs.add(ids[i], Body{X: float64(i)})
	}
	rng := rand.New(rand.NewSource(23))
	for i := range ids {
		hs.add(ids[i], Homing{Target: ids[rng.Intn(n)], Turn: 0.1})
	}
	return hs, bs, ids
}

// The baseline cog#239 already measured: Body reached as a Query field, one
// probe keyed on the entity the driver is on.
func BenchmarkRef_AsQueryField(b *testing.B) {
	hs, bs, _ := setupRefs(structN, true)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var t float64
		for i := len(hs.dense) - 1; i >= 0; i-- {
			if v, ok := bs.get(hs.owners[i]); ok {
				t += v.X * hs.dense[i].Turn
			}
		}
		sink = t
	}
}

// The same Component reached through the reference instead. The probe is
// identical; what differs is that the key comes from the Component just read,
// so the load depends on it and the access pattern is unrelated to the driver.
func BenchmarkRef_ThroughReference(b *testing.B) {
	hs, bs, _ := setupRefs(structN, true)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var t float64
		for i := len(hs.dense) - 1; i >= 0; i-- {
			h := &hs.dense[i]
			if v, ok := bs.get(h.Target); ok {
				t += v.X * h.Turn
			}
		}
		sink = t
	}
}

// Both, which is the realistic homing system: read my own Body and my target's.
func BenchmarkRef_OwnAndTarget(b *testing.B) {
	hs, bs, _ := setupRefs(structN, true)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var t float64
		for i := len(hs.dense) - 1; i >= 0; i-- {
			h := &hs.dense[i]
			mine, ok := bs.get(hs.owners[i])
			if !ok {
				continue
			}
			if theirs, ok := bs.get(h.Target); ok {
				t += (theirs.X - mine.X) * h.Turn
			}
		}
		sink = t
	}
}

// A reference to a despawned entity needs no liveness check of its own: eager
// despawn removed the entity from every Store, so the probe simply misses.
func TestReferenceToDespawnedEntityMisses(t *testing.T) {
	r, ids := setupRegistry(8, 100, t)
	victim := ids[7]
	// Confirm it is somewhere first, so the test is not vacuous.
	found := false
	for _, st := range r.stores {
		if s, ok := st.(*storeB[Body]); ok && s.has(victim) {
			found = true
		}
	}
	if !found {
		t.Skip("fixture did not place the victim in any store")
	}
	r.despawnScan(victim)
	for i, st := range r.stores {
		if s, ok := st.(*storeB[Body]); ok {
			if _, ok := s.get(victim); ok {
				t.Fatalf("store %d still resolves a reference to a despawned entity", i)
			}
		}
	}
}

// And a recycled index must not resolve a stale reference to the entity that
// took its place — the generation in the slot is what prevents it.
func TestStaleReferenceDoesNotAliasRecycledIndex(t *testing.T) {
	bs := newB[Body](16)
	old := mkEntity(4, 1)
	bs.add(old, Body{X: 111})

	// Despawn: remove from the Store, then the index is recycled with a new
	// generation and the new entity gets its own Body.
	bs.remove(old)
	fresh := mkEntity(4, 2)
	bs.add(fresh, Body{X: 222})

	if v, ok := bs.get(old); ok {
		t.Fatalf("stale reference resolved to %v", v.X)
	}
	v, ok := bs.get(fresh)
	if !ok || v.X != 222 {
		t.Fatalf("live reference resolved to %v, %v", v, ok)
	}
}

// A pointer handed out by Set[T] is invalidated by any structural change to
// that Store, exactly as cog#239 said of dense indices. Demonstrated rather
// than asserted, so the spec's rule has evidence behind it.
func TestSetPointerIsInvalidatedByStructuralChange(t *testing.T) {
	bs := newB[Body](16)
	a, b2 := mkEntity(1, 1), mkEntity(2, 1)
	bs.add(a, Body{X: 1})
	bs.add(b2, Body{X: 2})

	p, _ := bs.get(b2) // pointer into dense[1]
	bs.remove(a)       // swap-remove moves b2's data into dense[0]

	p.X = 99
	live, _ := bs.get(b2)
	if live.X == 99 {
		t.Fatal("expected the retained pointer to address a stale slot after swap-remove")
	}
	if live.X != 2 {
		t.Fatalf("entity's own value is %v, want 2", live.X)
	}
}
