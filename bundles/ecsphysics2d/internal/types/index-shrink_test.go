package types

import (
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// TestTheZeroRequestGivesEveryAreaOfAnIndexBack is the grid half of the command:
// after a spike and a cut, both the grid and the world cache report bytes let
// go, and the index still answers exactly what it answered before.
func TestTheZeroRequestGivesEveryAreaOfAnIndexBack(t *testing.T) {
	idx := spikedIndex(t, 4096, 256)
	before := overlapped(idx)
	if len(before) == 0 {
		t.Fatal("the spiked index answers no Overlap at all, so the shrink is compared against nothing")
	}

	released := idx.shrink(ShrinkRequest{})
	t.Logf("a 4096 Entity spike cut to 256 released %d bytes of grid and %d of world cache",
		released.Indices, released.WorldCache)
	if released.Indices == 0 {
		t.Error("the zero request released no grid bytes after a spike")
	}
	if released.WorldCache == 0 {
		t.Error("the zero request released no world-cache bytes after a spike")
	}

	after := overlapped(idx)
	if len(after) != len(before) {
		t.Fatalf("the shrunk index answers %d Entities, want the %d it answered before", len(after), len(before))
	}
	held := make(map[ecs.Entity]bool, len(before))
	for _, e := range before {
		held[e] = true
	}
	for _, e := range after {
		if !held[e] {
			t.Fatalf("the shrunk index answers %v, which it did not answer before", e)
		}
	}
}

// TestAShrunkIndexTakesInsertsAndRemovalsAgain is the hazard the packing has to
// clear: compacting renumbers the slots every cell list names, and packing the
// slab leaves a recycled slot pointing at a run that has moved. An Insert after
// a shrink must land where the next query looks for it.
func TestAShrunkIndexTakesInsertsAndRemovalsAgain(t *testing.T) {
	idx := spikedIndex(t, 1024, 64)
	idx.shrink(ShrinkRequest{})

	// A recycled slot's run is the one packing forgets, so an Insert wanting a
	// longer cache than the slot it recycles must take a fresh run rather than
	// write over a live entry's.
	for i := range 64 {
		idx.Insert(testEntity(2000+i), NewSegmentShape(m.Vec2d{X: -0.5}, m.Vec2d{X: 0.5}, 0.1),
			m.Vec2d{X: float64(i) * 1.9, Y: 40}, 0, nil)
	}
	if idx.Len() != 128 {
		t.Fatalf("the index holds %d Entities after 64 more, want 128", idx.Len())
	}
	found := idx.Overlap(nil, NewCircleShape(0.2, m.Vec2d{}), m.Vec2d{X: 1.9 * 7, Y: 40}, 0, nil,
		CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if len(found) != 1 || found[0] != testEntity(2007) {
		t.Fatalf("an Overlap over an Entity inserted after the shrink answers %v, want just %v",
			found, testEntity(2007))
	}
	for i := range 64 {
		idx.Remove(testEntity(2000 + i))
	}
	if idx.Len() != 64 {
		t.Fatalf("the index holds %d Entities after the 64 were removed again, want 64", idx.Len())
	}
	if got := overlapped(idx); len(got) != 64 {
		t.Fatalf("a wide Overlap answers %d Entities, want the 64 that survived", len(got))
	}
}

// TestEachKeepOptionOptsOneAreaOutOfAnIndexShrink pins the opt-outs one at a
// time: a kept area reports 0 and is still holding what it held.
func TestEachKeepOptionOptsOneAreaOutOfAnIndexShrink(t *testing.T) {
	t.Run("KeepIndices", func(t *testing.T) {
		idx := spikedIndex(t, 4096, 256)
		buckets, entries := cap(idx.buckets), cap(idx.entries)
		released := idx.shrink(ShrinkRequest{KeepIndices: true})
		if released.Indices != 0 {
			t.Errorf("a kept grid released %d bytes", released.Indices)
		}
		if released.WorldCache == 0 {
			t.Error("the world cache was not kept and released nothing")
		}
		if cap(idx.buckets) != buckets || cap(idx.entries) != entries {
			t.Errorf("a kept grid holds %d buckets and %d entries, want the %d and %d it held",
				cap(idx.buckets), cap(idx.entries), buckets, entries)
		}
	})
	t.Run("KeepWorldCache", func(t *testing.T) {
		idx := spikedIndex(t, 4096, 256)
		slab := cap(idx.slab)
		released := idx.shrink(ShrinkRequest{KeepWorldCache: true})
		if released.WorldCache != 0 {
			t.Errorf("a kept world cache released %d bytes", released.WorldCache)
		}
		if released.Indices == 0 {
			t.Error("the grid was not kept and released nothing")
		}
		if cap(idx.slab) != slab {
			t.Errorf("a kept world cache holds %d vectors, want the %d it held", cap(idx.slab), slab)
		}
	})
}

// TestASecondIndexShrinkReleasesNothing is the idempotence the ECS's own shrink
// has: the first request takes the slack, and the second finds none.
func TestASecondIndexShrinkReleasesNothing(t *testing.T) {
	idx := spikedIndex(t, 4096, 256)
	idx.shrink(ShrinkRequest{})
	again := idx.shrink(ShrinkRequest{})
	if again.Indices != 0 || again.WorldCache != 0 {
		t.Errorf("a second shrink released %d grid bytes and %d world-cache bytes, want none",
			again.Indices, again.WorldCache)
	}
}

// TestTheBodyIndexShrinkIsExactAndASecondOneAllocatesNothing is what the Body
// index's slot rule buys the shrink: it keeps no Go map, so everything its grid
// holds is a slice whose capacity can be asked. The figure it reports is the
// whole of what it let go, and a second shrink has nothing to rebuild.
func TestTheBodyIndexShrinkIsExactAndASecondOneAllocatesNothing(t *testing.T) {
	idx := NewBodyIndex(2)
	fill := func(count int) {
		idx.Clear()
		for i := range count {
			idx.Insert(testEntity(i), NewCircleShape(0.4, m.Vec2d{}),
				m.Vec2d{X: float64(i%64) * 1.9, Y: float64(i/64) * 1.9}, 0, nil)
		}
	}
	fill(4096)
	fill(256)

	before := idx.gridBytes()
	released := idx.shrink(ShrinkRequest{})
	if released.Indices == 0 || released.Indices != before-idx.gridBytes() {
		t.Errorf("the shrink reported %d grid bytes, want the %d the grid went down by",
			released.Indices, before-idx.gridBytes())
	}
	if idx.Len() != 256 || len(overlappedBodies(idx)) != 256 {
		t.Fatalf("the shrunk Body index holds %d and answers %d, want 256 and 256",
			idx.Len(), len(overlappedBodies(idx)))
	}

	if allocations := testing.AllocsPerRun(10, func() { idx.shrink(ShrinkRequest{}) }); allocations != 0 {
		t.Errorf("a second shrink of the Body index allocated %v times, want 0", allocations)
	}
}

// overlappedBodies is overlapped for a Body index.
func overlappedBodies(idx *BodyIndex) []ecs.Entity {
	return idx.Overlap(nil, NewCircleShape(200, m.Vec2d{}), m.Vec2d{X: 60, Y: 30}, 0, nil,
		CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
}

// spikedIndex is a static index a spike filled and a cut emptied down to keep
// Entities: the shape of an index after a level load, with slack in every
// buffer, dead slots in the entry run, and abandoned world-cache runs the
// static index never Clears away.
func spikedIndex(t testing.TB, spike, keep int) *StaticIndex {
	t.Helper()
	idx := NewStaticIndex(2)
	for i := range spike {
		idx.Insert(testEntity(i), NewCircleShape(0.4, m.Vec2d{}),
			m.Vec2d{X: float64(i%64) * 1.9, Y: float64(i/64) * 1.9}, 0, nil)
	}
	// A quarter of the survivors replaced by a Shape with a longer world cache,
	// so the slab carries abandoned runs as well as slack: a segment caches two
	// vectors where a circle caches one.
	for i := 0; i < keep; i += 4 {
		idx.Insert(testEntity(i), NewSegmentShape(m.Vec2d{Y: -0.4}, m.Vec2d{Y: 0.4}, 0.1),
			m.Vec2d{X: float64(i%64) * 1.9, Y: float64(i/64) * 1.9}, 0, nil)
	}
	for i := keep; i < spike; i++ {
		idx.Remove(testEntity(i))
	}
	if idx.Len() != keep {
		t.Fatalf("the spiked index holds %d Entities, want %d", idx.Len(), keep)
	}
	return idx
}

// overlapped is every Entity a wide circle finds, which is how a test compares
// what an index answers before a shrink with what it answers after one.
func overlapped(idx *StaticIndex) []ecs.Entity {
	return idx.Overlap(nil, NewCircleShape(200, m.Vec2d{}), m.Vec2d{X: 60, Y: 30}, 0, nil,
		CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
}
