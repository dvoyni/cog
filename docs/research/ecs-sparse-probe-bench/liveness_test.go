package sbench

import "testing"

// cog#240 settled that liveness is decided against Entities, not against the
// Stores: a Despawn takes write{*Entities} and bumps a generation, every Query
// carries read{*Entities} and checks each candidate before yielding it.
//
// That makes despawn O(1) and removes every drainer from the design, but it
// puts an extra random load on the hot path of every Query, and it leaves dead
// components resident in their Stores. This measures both.

// entities is the generation table Despawn writes and every Query reads.
type entities struct {
	gen  []uint32
	free []uint32
	next int // next never-used index, when the free list is empty
}

func newEntities(space uint32) *entities {
	return &entities{gen: make([]uint32, space)}
}

func (n *entities) alive(e Entity) bool { return n.gen[e.idx()] == e.gen() }

// despawn is the whole of a despawn: bump the generation, return the index to
// the free list. It touches no Store.
func (n *entities) despawn(e Entity) {
	if !n.alive(e) {
		return
	}
	n.gen[e.idx()]++
	n.free = append(n.free, e.idx())
}

func setupLive(n int, deadPct int) (*entities, *storeB[Body], *storeB[Collider]) {
	space := uint32(n * 2)
	ns := newEntities(space)
	bs, cs := newB[Body](space), newB[Collider](space)
	for i := range n {
		e := mkEntity(uint32(i), 1)
		ns.gen[e.idx()] = 1
		bs.add(e, Body{X: float64(i)})
		cs.add(e, Collider{R: 1})
	}
	// Kill a share of them, leaving their components resident — which is
	// exactly what lazy reclamation does.
	if deadPct > 0 {
		for i := range n {
			if i%(100/deadPct) == 0 {
				ns.despawn(mkEntity(uint32(i), 1))
			}
		}
	}
	return ns, bs, cs
}

// Baseline: the cog#239 contract, one probe, no liveness check. This is what
// the design costs if Despawn eagerly cleans every Store instead.
func BenchmarkLive_NoCheck(b *testing.B) {
	_, bs, cs := setupLive(structN, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var t float64
		for i := len(bs.dense) - 1; i >= 0; i-- {
			e := bs.owners[i]
			if v, ok := cs.get(e); ok {
				t += bs.dense[i].X + v.R
			}
		}
		sink = t
	}
}

// The chosen shape, with nothing dead: every candidate pays the liveness load
// and every candidate passes.
func BenchmarkLive_Check_0PctDead(b *testing.B) { benchLive(b, 0) }

// With a fifth of the driver dead — a Store that has not been touched since a
// wave of despawns.
func BenchmarkLive_Check_20PctDead(b *testing.B) { benchLive(b, 20) }

func benchLive(b *testing.B, deadPct int) {
	ns, bs, cs := setupLive(structN, deadPct)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var t float64
		for i := len(bs.dense) - 1; i >= 0; i-- {
			e := bs.owners[i]
			if !ns.alive(e) {
				continue
			}
			if v, ok := cs.get(e); ok {
				t += bs.dense[i].X + v.R
			}
		}
		sink = t
	}
}

// --- reclamation ----------------------------------------------------------

// addReusing is add() with the one extra branch lazy reclamation needs: if the
// slot already holds an entry for a previous generation of this index, that
// entry is an orphan no handle can reach, so overwrite it in place instead of
// appending. Reclamation then happens exactly when the pressure does — when an
// index is recycled — and the dense array stays bounded by peak live entities.
func (s *storeB[T]) addReusing(e Entity, v T) {
	old := s.sparse[e.idx()]
	if old != absentGen64 && uint32(old>>32) != e.gen() {
		d := uint32(old)
		s.dense[d] = v
		s.owners[d] = e
		s.sparse[e.idx()] = uint64(e.gen())<<32 | uint64(d)
		return
	}
	s.add(e, v)
}

// Recycle one index forever. Without slot reuse the dense array grows without
// bound; with it, the Store stays exactly one entry long.
func TestLazyReclamationBoundsTheStore(t *testing.T) {
	bs := newB[Body](16)
	for gen := uint32(1); gen <= 1000; gen++ {
		bs.addReusing(mkEntity(3, gen), Body{X: float64(gen)})
	}
	if len(bs.dense) != 1 {
		t.Fatalf("dense grew to %d over 1000 recycles, want 1", len(bs.dense))
	}
	if bs.owners[0] != mkEntity(3, 1000) {
		t.Fatalf("owner is %v, want the newest generation", bs.owners[0])
	}
	if _, ok := bs.get(mkEntity(3, 999)); ok {
		t.Fatal("a stale handle still resolves")
	}
	v, ok := bs.get(mkEntity(3, 1000))
	if !ok || v.X != 1000 {
		t.Fatalf("live handle resolved to %v, %v", v, ok)
	}
}

// Without the reuse branch, the same loop leaks one dense entry per recycle —
// stated as a test so the cost of omitting it is on the record.
func TestPlainAddLeaksOnRecycle(t *testing.T) {
	bs := newB[Body](16)
	for gen := uint32(1); gen <= 1000; gen++ {
		bs.add(mkEntity(3, gen), Body{X: float64(gen)})
	}
	if len(bs.dense) != 1000 {
		t.Fatalf("dense is %d, expected 1000 orphaned entries", len(bs.dense))
	}
}

// What the extra branch costs on the spawn path.
func BenchmarkAdd_Plain(b *testing.B) {
	const space = 1 << 16
	bs := newB[Body](space)
	for i := range space {
		bs.add(mkEntity(uint32(i), 1), Body{})
	}
	for i := range space {
		bs.remove(mkEntity(uint32(i), 1))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		e := mkEntity(uint32(i%space), 1)
		bs.add(e, Body{X: 1})
		bs.remove(e)
	}
}

func BenchmarkAdd_Reusing(b *testing.B) {
	const space = 1 << 16
	bs := newB[Body](space)
	for i := range space {
		bs.add(mkEntity(uint32(i), 1), Body{})
	}
	for i := range space {
		bs.remove(mkEntity(uint32(i), 1))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		e := mkEntity(uint32(i%space), 1)
		bs.addReusing(e, Body{X: 1})
		bs.remove(e)
	}
}

// Despawn itself, now that it touches no Store at all. Compare against
// BenchmarkDespawn_ScanAll_85, which is what eager cleaning costs.
func BenchmarkDespawn_GenerationBump(b *testing.B) {
	ns := newEntities(uint32(structN))
	ids := make([]Entity, structN)
	for i := range ids {
		ids[i] = mkEntity(uint32(i), 1)
		ns.gen[i] = 1
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		k := i % structN
		ns.gen[k] = uint32(i/structN) + 1
		ns.free = ns.free[:0]
		sink = float64(ns.gen[k])
	}
}
