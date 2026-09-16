package types

import (
	"math"
	"sync"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// testEntity is a handle shaped as the ECS makes them, index plus a generation
// of 1, which is all an index ever needs of one: it compares them and hands
// them back.
func testEntity(i int) ecs.Entity { return ecs.Entity(uint64(i+1) | 1<<32) }

// the two group masks the filtering tests use.
const (
	walls       uint32 = 1 << 0
	projectiles uint32 = 1 << 1
)

func TestAnIndexHoldsTheLastShapeInsertedForAnEntity(t *testing.T) {
	idx := NewStaticIndex(2)
	wall := testEntity(0)

	idx.Insert(wall, unitCircle, m.Vec2d{X: 5}, 0, nil)
	if got := idx.Len(); got != 1 {
		t.Fatalf("Len = %d after one Insert, want 1", got)
	}
	if _, ok := idx.Probe(origin, m.Vec2d{X: 10}, 0, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity); !ok {
		t.Fatal("the inserted circle was not Probed")
	}

	// Static geometry is never changed in place; to move one, the app replaces
	// it, which is an Insert over the same Entity.
	idx.Insert(wall, unitCircle, m.Vec2d{X: 5, Y: 40}, 0, nil)
	if got := idx.Len(); got != 1 {
		t.Fatalf("Len = %d after a replacement, want 1", got)
	}
	if _, ok := idx.Probe(origin, m.Vec2d{X: 10}, 0, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity); ok {
		t.Error("a replaced Entity was still Probed where it used to be")
	}
	if _, ok := idx.Probe(m.Vec2d{Y: 40}, m.Vec2d{X: 10, Y: 40}, 0, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity); !ok {
		t.Error("a replaced Entity was not Probed where it moved to")
	}

	idx.Remove(wall)
	if got := idx.Len(); got != 0 {
		t.Fatalf("Len = %d after Remove, want 0", got)
	}
	idx.Remove(wall)
	idx.Remove(testEntity(99))
	if got := idx.Len(); got != 0 {
		t.Fatalf("Len = %d after removing what was not there, want 0", got)
	}
}

func TestProbeReportsTheNearestHitAndItsBoolIsLineOfSight(t *testing.T) {
	idx := NewStaticIndex(2)
	for i, x := range []float64{3, 5, 7} {
		idx.Insert(testEntity(i), NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{X: x}, 0, nil)
	}

	hit, ok := idx.Probe(origin, m.Vec2d{X: 10}, 0, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if !ok {
		t.Fatal("a Probe across three circles met nothing")
	}
	if hit.Entity != testEntity(0) || !nearF(hit.T, 0.25) {
		t.Errorf("the nearest Hit is %+v, want the circle at x = 3 at T = 0.25", hit)
	}

	// From the far end the nearest is the far one, so the Probe is not merely
	// finding the first Entity inserted.
	back, ok := idx.Probe(m.Vec2d{X: 10}, origin, 0, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if !ok || back.Entity != testEntity(2) {
		t.Errorf("the Hit probing back is %+v, want the circle at x = 7", back)
	}

	// Line of sight is the bool: a Probe along a clear line meets nothing.
	if _, ok := idx.Probe(m.Vec2d{Y: 20}, m.Vec2d{X: 10, Y: 20}, 0, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity); ok {
		t.Error("a Probe along a clear line reported a Hit")
	}
}

func TestProbeAllOrdersItsHitsByTAndNamesEachEntityOnce(t *testing.T) {
	idx := NewStaticIndex(2)
	// Inserted out of order, so the ordering cannot come from the insertion.
	for i, x := range []float64{7, 3, 5} {
		idx.Insert(testEntity(i), NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{X: x}, 0, nil)
	}
	// A wall long enough to be listed in a dozen cells at once, crossing the
	// Probe: it must still be named once.
	idx.Insert(testEntity(3), NewSegmentShape(m.Vec2d{Y: -12}, m.Vec2d{Y: 12}, 0), m.Vec2d{X: 9}, 0, nil)

	var dst []Hit
	dst = idx.ProbeAll(dst[:0], origin, m.Vec2d{X: 10}, 0, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)

	if len(dst) != 4 {
		t.Fatalf("ProbeAll found %d Hits, want 4: %+v", len(dst), dst)
	}
	want := []ecs.Entity{testEntity(1), testEntity(2), testEntity(0), testEntity(3)}
	for i := range want {
		if dst[i].Entity != want[i] {
			t.Fatalf("Hit %d is %v, want %v — the order is by T: %+v", i, dst[i].Entity, want[i], dst)
		}
		if i > 0 && dst[i-1].T > dst[i].T {
			t.Fatalf("Hit %d is at T %v, behind %v: %+v", i, dst[i].T, dst[i-1].T, dst)
		}
	}

	// The idiom is dst = ProbeAll(dst[:0], …), and a second call over the same
	// buffer starts clean.
	dst = idx.ProbeAll(dst[:0], origin, m.Vec2d{X: 4}, 0, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if len(dst) != 1 || dst[0].Entity != testEntity(1) {
		t.Errorf("a shorter ProbeAll over the same buffer gave %+v, want the circle at x = 3 alone", dst)
	}
}

func TestTheBroadphaseInflatesByTheQueryRadiusBeforeDescending(t *testing.T) {
	// cp's defect 5, which is in C too: the un-inflated ray reaches the index,
	// so a Probe with a radius misses what lies beside the cells the ray itself
	// crosses. Here the circle is a whole cell row above the ray.
	idx := NewStaticIndex(2)
	target := testEntity(0)
	at := m.Vec2d{X: 5, Y: 2.5}
	shape := NewCircleShape(0.1, m.Vec2d{})
	idx.Insert(target, shape, at, 0, nil)

	from, to, radius := origin, m.Vec2d{X: 10}, 2.6

	direct, okDirect := ProbeShape(from, to, radius, shape, at, 0, nil)
	if !okDirect {
		t.Fatal("the pair primitive itself missed the circle, so the index cannot be blamed")
	}

	hit, ok := idx.Probe(from, to, radius, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if !ok {
		t.Fatal("the index missed a Hit the pair primitive finds: the broadphase did not inflate")
	}
	if hit.Entity != target || !nearF(hit.T, direct.T) {
		t.Errorf("the index reports %+v, want the pair primitive's T %v on %v", hit, direct.T, target)
	}

	all := idx.ProbeAll(nil, from, to, radius, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if len(all) != 1 || all[0].Entity != target {
		t.Errorf("ProbeAll found %+v, want the one circle", all)
	}
}

func TestOverlapReportsWhatAShapeTouchesAndNothingItMerelyPassesNear(t *testing.T) {
	idx := NewBodyIndex(2)
	idx.Insert(testEntity(0), NewCircleShape(1, m.Vec2d{}), m.Vec2d{X: 5}, 0, nil)
	idx.Insert(testEntity(1), NewCircleShape(1, m.Vec2d{}), m.Vec2d{X: 9}, 0, nil)
	idx.Insert(testEntity(2), NewSegmentShape(m.Vec2d{Y: -4}, m.Vec2d{Y: 4}, 0), m.Vec2d{X: 6.5}, 0, nil)

	var dst []ecs.Entity
	dst = idx.Overlap(dst[:0], NewCircleShape(1, m.Vec2d{}), m.Vec2d{X: 5.7}, 0, nil,
		CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if len(dst) != 2 {
		t.Fatalf("Overlap found %v, want the circle at x = 5 and the wall at x = 6.5", dst)
	}
	found := map[ecs.Entity]bool{dst[0]: true, dst[1]: true}
	if !found[testEntity(0)] || !found[testEntity(2)] {
		t.Errorf("Overlap found %v, want %v and %v", dst, testEntity(0), testEntity(2))
	}

	// A point query is a circle of radius 0.
	dst = idx.Overlap(dst[:0], NewCircleShape(0, m.Vec2d{}), m.Vec2d{X: 5.5}, 0, nil,
		CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if len(dst) != 1 || dst[0] != testEntity(0) {
		t.Errorf("a point query found %v, want only the circle it is inside", dst)
	}

	dst = idx.Overlap(dst[:0], NewCircleShape(0.1, m.Vec2d{}), m.Vec2d{X: 20}, 0, nil,
		CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if len(dst) != 0 {
		t.Errorf("an Overlap well away from everything found %v", dst)
	}
}

func TestAShapeSpanningManyCellsIsNamedOnceByOverlap(t *testing.T) {
	idx := NewStaticIndex(2)
	// Twenty-four metres of wall is a dozen cells at a two-metre cell size.
	idx.Insert(testEntity(0), NewSegmentShape(m.Vec2d{Y: -12}, m.Vec2d{Y: 12}, 0.2), origin, 0, nil)

	// An Overlap wide enough to scan every cell the wall is listed in.
	dst := idx.Overlap(nil, NewCircleShape(20, m.Vec2d{}), origin, 0, nil,
		CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if len(dst) != 1 {
		t.Fatalf("Overlap named the wall %d times, want once: %v", len(dst), dst)
	}
}

func TestAQueryFiltersOnBothSidesAndSkipsTheExcludedEntity(t *testing.T) {
	idx := NewStaticIndex(2)

	wall := NewCircleShape(0.5, m.Vec2d{})
	wall.CollisionBits, wall.CollidesWith = walls, projectiles
	idx.Insert(testEntity(0), wall, m.Vec2d{X: 3}, 0, nil)

	deaf := NewCircleShape(0.5, m.Vec2d{})
	deaf.CollisionBits, deaf.CollidesWith = walls, CollisionBitsNone
	idx.Insert(testEntity(1), deaf, m.Vec2d{X: 5}, 0, nil)

	hit, ok := idx.Probe(origin, m.Vec2d{X: 10}, 0, projectiles, walls, ecs.NoEntity)
	if !ok || hit.Entity != testEntity(0) {
		t.Fatalf("a Probe in the right groups gave %+v, %v, want the wall at x = 3", hit, ok)
	}

	// The second wall is in a group the Probe looks for, but it looks for
	// nothing itself, so the pair never agrees.
	all := idx.ProbeAll(nil, origin, m.Vec2d{X: 10}, 0, projectiles, walls, ecs.NoEntity)
	if len(all) != 1 || all[0].Entity != testEntity(0) {
		t.Errorf("ProbeAll found %+v, want only the wall that looks back", all)
	}

	// A Prober in no group at all is invisible to everything.
	if _, ok := idx.Probe(origin, m.Vec2d{X: 10}, 0, CollisionBitsNone, walls, ecs.NoEntity); ok {
		t.Error("a Prober in no group still Hit something")
	}

	// exclude is one identity: a Body Probing from inside its own Shape would
	// Hit itself first every time.
	if _, ok := idx.Probe(origin, m.Vec2d{X: 10}, 0, projectiles, walls, testEntity(0)); ok {
		t.Error("the excluded Entity was Hit")
	}
	excluded := idx.Overlap(nil, NewCircleShape(1, m.Vec2d{}), m.Vec2d{X: 3}, 0, nil,
		projectiles, walls, testEntity(0))
	if len(excluded) != 0 {
		t.Errorf("Overlap found the excluded Entity: %v", excluded)
	}
}

func TestTheIndexAndThePairPrimitiveAgreeOverAScatterOfShapes(t *testing.T) {
	// The index is a broadphase and nothing more: whatever it hands back, the
	// primitive over the same values must agree with, and whatever it leaves
	// out, the primitive must reject.
	idx := NewStaticIndex(2)
	shapes := make([]Shape, 0, 64)
	places := make([]m.Vec2d, 0, 64)

	for i := range 64 {
		x := float64(i%8)*3.1 - 12
		y := float64(i/8)*3.7 - 12
		shape := NewCircleShape(0.4+float64(i%3)*0.3, m.Vec2d{})
		if i%4 == 0 {
			shape = NewSegmentShape(m.Vec2d{X: -1.3}, m.Vec2d{X: 1.1, Y: 0.7}, 0.15)
		}
		shapes = append(shapes, shape)
		places = append(places, m.Vec2d{X: x, Y: y})
		idx.Insert(testEntity(i), shape, places[i], 0, nil)
	}

	for _, radius := range []float64{0, 0.35, 1.2} {
		from := m.Vec2d{X: -14, Y: -9.3}
		to := m.Vec2d{X: 14, Y: 7.9}

		all := idx.ProbeAll(nil, from, to, radius, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
		reported := map[ecs.Entity]Hit{}
		for _, hit := range all {
			reported[hit.Entity] = hit
		}

		for i := range shapes {
			want, wantOK := ProbeShape(from, to, radius, shapes[i], places[i], 0, nil)
			got, gotOK := reported[testEntity(i)]
			if wantOK != gotOK {
				t.Fatalf("at radius %v the index says %v of %v and the primitive says %v",
					radius, gotOK, testEntity(i), wantOK)
			}
			if wantOK && !nearF(got.T, want.T) {
				t.Fatalf("at radius %v %v is at T %v in the index and %v in the primitive",
					radius, testEntity(i), got.T, want.T)
			}
		}
	}
}

func TestAnIndexListsNothingItCannotPlaceOrCannotTestYet(t *testing.T) {
	idx := NewStaticIndex(2)
	idx.Insert(testEntity(0), NewCircleShape(1, m.Vec2d{}), m.Vec2d{X: 5}, 0, nil)

	// A Shape placed at an infinity or a NaN would otherwise cover every cell
	// between the two ends of the grid.
	idx.Insert(testEntity(1), NewCircleShape(1, m.Vec2d{}), m.Vec2d{X: math.Inf(1)}, 0, nil)
	idx.Insert(testEntity(2), NewCircleShape(1, m.Vec2d{}), m.Vec2d{Y: math.NaN()}, 0, nil)

	// A kind with no world cache yet would otherwise sit at the origin and be
	// tested by every query that passes it.
	quad := Shape{Kind: ShapeQuad, CollisionBits: CollisionBitsAll, CollidesWith: CollisionBitsAll}
	idx.Insert(testEntity(3), quad, m.Vec2d{}, 0, nil)

	all := idx.ProbeAll(nil, m.Vec2d{X: -20, Y: -20}, m.Vec2d{X: 20, Y: 20}, 0,
		CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	for _, hit := range all {
		if hit.Entity != testEntity(0) {
			t.Errorf("a Probe met %v, which the index cannot place or cannot test", hit.Entity)
		}
	}

	// They are still held, so Remove and a later replacement both work.
	if got := idx.Len(); got != 4 {
		t.Errorf("Len = %d, want 4: an unplaceable Shape is held, only not listed", got)
	}
	idx.Insert(testEntity(1), NewCircleShape(1, m.Vec2d{}), m.Vec2d{X: 5, Y: 9}, 0, nil)
	if _, ok := idx.Probe(m.Vec2d{Y: 9}, m.Vec2d{X: 10, Y: 9}, 0,
		CollisionBitsAll, CollisionBitsAll, ecs.NoEntity); !ok {
		t.Error("an Entity replaced back onto the plane was not found")
	}
}

func TestClearKeepsTheBuffersSoARebuildAllocatesNothing(t *testing.T) {
	idx := NewBodyIndex(2)
	fill := func() {
		idx.Clear()
		for i := range 256 {
			idx.Insert(testEntity(i), NewCircleShape(0.4, m.Vec2d{}),
				m.Vec2d{X: float64(i%16) * 1.7, Y: float64(i/16) * 1.7}, 0, nil)
		}
	}

	// A handful of ticks to let every buffer reach the size it settles at.
	for range 8 {
		fill()
	}
	if got := idx.Len(); got != 256 {
		t.Fatalf("Len = %d after a rebuild, want 256", got)
	}

	if allocations := testing.AllocsPerRun(20, fill); allocations != 0 {
		t.Errorf("rebuilding the Body index allocated %v times a tick, want 0", allocations)
	}
}

func TestTheQueriesAllocateNothing(t *testing.T) {
	idx := NewStaticIndex(2)
	for i := range 64 {
		idx.Insert(testEntity(i), NewCircleShape(0.4, m.Vec2d{}),
			m.Vec2d{X: float64(i%8) * 1.9, Y: float64(i/8) * 1.9}, 0, nil)
	}

	from, to := m.Vec2d{X: -2, Y: -2}, m.Vec2d{X: 16, Y: 16}
	hits := make([]Hit, 0, 64)
	entities := make([]ecs.Entity, 0, 64)
	query := NewCircleShape(3, m.Vec2d{})

	// Warm the buffers, which is what "adequately sized" means: the only
	// allocation a multi-result query has is growing one the caller sized too
	// small, and that settles to none.
	hits = idx.ProbeAll(hits[:0], from, to, 0.3, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	entities = idx.Overlap(entities[:0], query, m.Vec2d{X: 7, Y: 7}, 0, nil,
		CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if len(hits) == 0 || len(entities) == 0 {
		t.Fatalf("the warm-up found %d Hits and %d Entities, want both non-empty", len(hits), len(entities))
	}

	for name, call := range map[string]func(){
		"Probe": func() {
			hitSink, boolSink = idx.Probe(from, to, 0, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
		},
		"Probe with a radius": func() {
			hitSink, boolSink = idx.Probe(from, to, 0.3, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
		},
		"ProbeAll": func() {
			hits = idx.ProbeAll(hits[:0], from, to, 0.3, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
		},
		"Overlap": func() {
			entities = idx.Overlap(entities[:0], query, m.Vec2d{X: 7, Y: 7}, 0, nil,
				CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
		},
	} {
		if allocations := testing.AllocsPerRun(100, call); allocations != 0 {
			t.Errorf("%s allocated %v times a call, want 0", name, allocations)
		}
	}
}

// TestConcurrentReadersGenuinelyShareAnIndex is what makes the two indices
// Resources a System takes for read: a query holds no per-query state, so any
// number of them run together and none of them changes the answer another gets.
//
// There is no race detector in this environment, so this runs under -count=10
// rather than under -race, and it compares every concurrent answer against the
// one a single reader gets.
func TestConcurrentReadersGenuinelyShareAnIndex(t *testing.T) {
	idx := NewStaticIndex(2)
	for i := range 256 {
		idx.Insert(testEntity(i), NewCircleShape(0.4, m.Vec2d{}),
			m.Vec2d{X: float64(i%16) * 1.7, Y: float64(i/16) * 1.7}, 0, nil)
	}

	from, to := m.Vec2d{X: -3, Y: -3}, m.Vec2d{X: 30, Y: 30}
	query := NewCircleShape(2.5, m.Vec2d{})
	place := m.Vec2d{X: 11, Y: 11}

	wantHit, wantOK := idx.Probe(from, to, 0.3, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	wantAll := idx.ProbeAll(nil, from, to, 0.3, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	wantOverlap := idx.Overlap(nil, query, place, 0, nil, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if !wantOK || len(wantAll) == 0 || len(wantOverlap) == 0 {
		t.Fatalf("the single-reader answers are %v, %d Hits and %d Entities, want all three non-empty",
			wantOK, len(wantAll), len(wantOverlap))
	}

	const readers = 8
	var group sync.WaitGroup
	failures := make([]string, readers)

	for reader := range readers {
		group.Add(1)
		go func() {
			defer group.Done()
			hits := make([]Hit, 0, 64)
			entities := make([]ecs.Entity, 0, 64)

			for range 500 {
				hit, ok := idx.Probe(from, to, 0.3, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
				if ok != wantOK || hit != wantHit {
					failures[reader] = "Probe disagreed with the single reader"
					return
				}
				hits = idx.ProbeAll(hits[:0], from, to, 0.3, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
				if len(hits) != len(wantAll) {
					failures[reader] = "ProbeAll returned a different number of Hits"
					return
				}
				for i := range hits {
					if hits[i] != wantAll[i] {
						failures[reader] = "ProbeAll returned a different Hit"
						return
					}
				}
				entities = idx.Overlap(entities[:0], query, place, 0, nil,
					CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
				if len(entities) != len(wantOverlap) {
					failures[reader] = "Overlap returned a different number of Entities"
					return
				}
			}
		}()
	}
	group.Wait()

	for reader, failure := range failures {
		if failure != "" {
			t.Errorf("reader %d: %s", reader, failure)
		}
	}
}
