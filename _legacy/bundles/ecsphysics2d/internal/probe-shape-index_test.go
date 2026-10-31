package internal

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// crate is the Shape the world-query tests move: a unit box, the question of
// whether it fits through a gap being the one a game asks.
var crate = NewBoxShape(1, 1, 0)

func TestProbeWithReportsTheNearestHitOfAMovingShape(t *testing.T) {
	idx := NewStaticIndex(2)
	// A post 0.8 above the path, which a point Probe along the path passes and
	// the crate, reaching 0.5 either side of it, does not.
	idx.Insert(testEntity(0), NewCircleShape(0.4, m.Vec2d{}), m.Vec2d{X: 7, Y: 0.8}, 0, nil)
	idx.Insert(testEntity(1), NewBoxShape(1, 4, 0), m.Vec2d{X: 12}, 0, nil)
	// A post 1.2 above the path, which the crate clears.
	idx.Insert(testEntity(2), NewCircleShape(0.1, m.Vec2d{}), m.Vec2d{X: 4, Y: 1.2}, 0, nil)

	from, to := origin, m.Vec2d{X: 20}
	if _, ok := idx.Probe(from, to, 0, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity); !ok {
		t.Fatal("a point Probe met nothing, so the wall is not on the path")
	}

	hit, ok := idx.ProbeWith(crate, from, to, 0, nil, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if !ok {
		t.Fatal("the crate met nothing")
	}
	want, _ := ProbeShapeWith(crate, from, to, 0, nil, NewCircleShape(0.4, m.Vec2d{}), m.Vec2d{X: 7, Y: 0.8}, 0, nil)
	if hit.Entity != testEntity(0) || !nearF(hit.T, want.T) {
		t.Errorf("the nearest Hit is %+v, want the post at x = 7 at T %v", hit, want.T)
	}

	all := idx.ProbeAllWith(nil, crate, from, to, 0, nil, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if len(all) != 2 || all[0].Entity != testEntity(0) || all[1].Entity != testEntity(1) {
		t.Fatalf("ProbeAllWith found %+v, want the post and then the wall", all)
	}
	// The wall's face is at x = 11.5 and the crate leads by 0.5.
	if !nearF(all[1].T, 0.55) || !vecNear(all[1].Normal, m.Vec2d{X: -1}) {
		t.Errorf("the wall's Hit is %+v, want T 0.55 facing (-1, 0)", all[1])
	}

	// The idiom is dst = ProbeAllWith(dst[:0], …), and a shorter one over the
	// same buffer starts clean.
	all = idx.ProbeAllWith(all[:0], crate, from, m.Vec2d{X: 8}, 0, nil,
		CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if len(all) != 1 || all[0].Entity != testEntity(0) {
		t.Errorf("a shorter ProbeAllWith over the same buffer gave %+v, want the post alone", all)
	}

	// Line of sight is the bool here too.
	if _, ok := idx.ProbeWith(crate, m.Vec2d{Y: 10}, m.Vec2d{X: 20, Y: 10}, 0, nil,
		CollisionBitsAll, CollisionBitsAll, ecs.NoEntity); ok {
		t.Error("the crate moved along a clear line met something")
	}
}

func TestProbeWithFiltersOnBothSidesAndSkipsTheExcludedEntity(t *testing.T) {
	idx := NewStaticIndex(2)
	wall := NewBoxShape(1, 4, 0)
	wall.CollisionBits, wall.CollidesWith = walls, projectiles
	idx.Insert(testEntity(0), wall, m.Vec2d{X: 3}, 0, nil)

	deaf := NewBoxShape(1, 4, 0)
	deaf.CollisionBits, deaf.CollidesWith = walls, CollisionBitsNone
	idx.Insert(testEntity(1), deaf, m.Vec2d{X: 6}, 0, nil)

	from, to := origin, m.Vec2d{X: 10}
	all := idx.ProbeAllWith(nil, crate, from, to, 0, nil, projectiles, walls, ecs.NoEntity)
	if len(all) != 1 || all[0].Entity != testEntity(0) {
		t.Errorf("ProbeAllWith found %+v, want only the wall that looks back", all)
	}
	if _, ok := idx.ProbeWith(crate, from, to, 0, nil, CollisionBitsNone, walls, ecs.NoEntity); ok {
		t.Error("a Prober in no group still Hit something")
	}
	if _, ok := idx.ProbeWith(crate, from, to, 0, nil, projectiles, walls, testEntity(0)); ok {
		t.Error("the excluded Entity was Hit")
	}
}

func TestTheBodyIndexProbesAMovingShapeOverItsSleepersToo(t *testing.T) {
	idx := NewBodyIndex(2)
	idx.Insert(testEntity(0), NewBoxShape(1, 4, 0), m.Vec2d{X: 8}, 0, nil)
	InsertSleeper(idx, testEntity(1), NewBoxShape(1, 4, 0), m.Vec2d{X: 4}, 0, nil)
	idx.Insert(testEntity(2), NewBoxShape(1, 4, 0), m.Vec2d{X: 12}, 0, nil)

	from, to := origin, m.Vec2d{X: 20}
	hit, ok := idx.ProbeWith(crate, from, to, 0, nil, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if !ok || hit.Entity != testEntity(1) {
		t.Errorf("the nearest Hit is %+v, %v, want the sleeper at x = 4", hit, ok)
	}

	all := idx.ProbeAllWith(nil, crate, from, to, 0, nil, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	want := []ecs.Entity{testEntity(1), testEntity(0), testEntity(2)}
	if len(all) != len(want) {
		t.Fatalf("ProbeAllWith found %+v, want the three in order", all)
	}
	for i := range want {
		if all[i].Entity != want[i] {
			t.Fatalf("Hit %d is %v, want %v: both grids answer as one ordered run: %+v", i, all[i].Entity, want[i], all)
		}
	}
}

func TestProbeWithACircleShapeAgreesWithProbe(t *testing.T) {
	statics := NewStaticIndex(2)
	bodies := NewBodyIndex(2)
	for i := range 24 {
		shape := NewCircleShape(0.3+float64(i%3)*0.2, m.Vec2d{})
		if i%3 == 0 {
			shape = NewBoxShape(0.8, 1.6, 0.05)
		}
		at := m.Vec2d{X: float64(i%6)*2.3 - 6, Y: float64(i/6)*2.1 - 4}
		statics.Insert(testEntity(i), shape, at, 0.2*float64(i), nil)
		if i%2 == 0 {
			bodies.Insert(testEntity(i), shape, at, 0.2*float64(i), nil)
		} else {
			InsertSleeper(bodies, testEntity(i), shape, at, 0.2*float64(i), nil)
		}
	}

	// The circle's centre is its offset turned by the angle, ahead of the
	// Position the Probe is given.
	circle := NewCircleShape(0.35, m.Vec2d{X: 0.4})
	angle := math.Pi / 3
	centre := NewTransformRigid(origin, angle).Point(circle.Offset())
	from, to := m.Vec2d{X: -9, Y: -6}, m.Vec2d{X: 9, Y: 5}

	type index interface {
		Probe(from, to m.Vec2d, radius float64, bits, collidesWith uint32, exclude ecs.Entity) (Hit, bool)
		ProbeAll(dst []Hit, from, to m.Vec2d, radius float64, bits, collidesWith uint32, exclude ecs.Entity) []Hit
		ProbeWith(shape Shape, from, to m.Vec2d, angle float64, verts []m.Vec2d,
			bits, collidesWith uint32, exclude ecs.Entity) (Hit, bool)
		ProbeAllWith(dst []Hit, shape Shape, from, to m.Vec2d, angle float64, verts []m.Vec2d,
			bits, collidesWith uint32, exclude ecs.Entity) []Hit
	}
	for name, idx := range map[string]index{"the static index": statics, "the Body index": bodies} {
		want, wantOK := idx.Probe(from.Add(centre), to.Add(centre), 0.35, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
		got, ok := idx.ProbeWith(circle, from, to, angle, nil, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
		if !wantOK || ok != wantOK || got.Entity != want.Entity || !nearF(got.T, want.T) ||
			!vecNear(got.Point, want.Point) || !vecNear(got.Normal, want.Normal) {
			t.Errorf("on %s ProbeWith gave %+v, %v and Probe %+v, %v", name, got, ok, want, wantOK)
		}

		wantAll := idx.ProbeAll(nil, from.Add(centre), to.Add(centre), 0.35, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
		gotAll := idx.ProbeAllWith(nil, circle, from, to, angle, nil, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
		if len(gotAll) != len(wantAll) || len(wantAll) < 3 {
			t.Fatalf("on %s ProbeAllWith found %d Hits and ProbeAll %d, want the same, and several", name,
				len(gotAll), len(wantAll))
		}
		for i := range wantAll {
			if gotAll[i].Entity != wantAll[i].Entity || !nearF(gotAll[i].T, wantAll[i].T) {
				t.Errorf("on %s Hit %d is %+v, and ProbeAll's %+v", name, i, gotAll[i], wantAll[i])
			}
		}
	}
}

func TestTheIndexAndProbeShapeWithAgreeOverAScatterOfShapes(t *testing.T) {
	// The index is a broadphase and nothing more, for a moving Shape as for a
	// moving circle: whatever it hands back the primitive agrees with, and
	// whatever it leaves out the primitive rejects. The plank is wider than a
	// cell, so the band beside the path is what finds what its ends meet.
	idx := NewStaticIndex(2)
	shapes := make([]Shape, 0, 64)
	places := make([]m.Vec2d, 0, 64)
	angles := make([]float64, 0, 64)

	for i := range 64 {
		shape := NewCircleShape(0.3+float64(i%3)*0.25, m.Vec2d{})
		switch i % 4 {
		case 0:
			shape = NewSegmentShape(m.Vec2d{X: -1.3}, m.Vec2d{X: 1.1, Y: 0.7}, 0.1)
		case 1:
			shape = NewBoxShape(0.9, 0.5, 0.05)
		}
		shapes = append(shapes, shape)
		places = append(places, m.Vec2d{X: float64(i%8)*3.1 - 12, Y: float64(i/8)*3.7 - 12})
		angles = append(angles, 0.37*float64(i))
		idx.Insert(testEntity(i), shape, places[i], angles[i], nil)
	}

	movers := map[string]Shape{
		"a crate":           crate,
		"a rounded box":     NewBoxShape(0.6, 0.9, 0.2),
		"a plank":           NewBoxShape(4, 0.2, 0),
		"a segment":         NewSegmentShape(m.Vec2d{X: -0.7, Y: 0.2}, m.Vec2d{X: 0.6, Y: -0.3}, 0),
		"a rounded segment": NewSegmentShape(m.Vec2d{X: -0.4}, m.Vec2d{X: 0.4}, 0.15),
	}
	from, to := m.Vec2d{X: -14, Y: -9.3}, m.Vec2d{X: 14, Y: 7.9}

	for name, mover := range movers {
		for _, angle := range []float64{0, 0.6, math.Pi / 2} {
			all := idx.ProbeAllWith(nil, mover, from, to, angle, nil, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
			reported := map[ecs.Entity]Hit{}
			for i, hit := range all {
				reported[hit.Entity] = hit
				if i > 0 && all[i-1].T > hit.T {
					t.Fatalf("%s at %v: Hit %d at T %v comes after one at %v", name, angle, i, hit.T, all[i-1].T)
				}
			}
			if len(all) < 3 {
				t.Fatalf("%s at %v: ProbeAllWith found %d Hits across the scatter, want several", name, angle, len(all))
			}
			if len(reported) != len(all) {
				t.Fatalf("%s at %v: ProbeAllWith named an Entity twice: %+v", name, angle, all)
			}

			first := Hit{T: math.Inf(1)}
			for i := range shapes {
				want, wantOK := ProbeShapeWith(mover, from, to, angle, nil, shapes[i], places[i], angles[i], nil)
				got, gotOK := reported[testEntity(i)]
				if wantOK != gotOK {
					t.Fatalf("%s at %v: the index says %v of %v and the primitive says %v",
						name, angle, gotOK, testEntity(i), wantOK)
				}
				if wantOK && !nearF(got.T, want.T) {
					t.Fatalf("%s at %v: %v is at T %v in the index and %v in the primitive",
						name, angle, testEntity(i), got.T, want.T)
				}
				if wantOK && want.T < first.T {
					first = want
				}
			}

			hit, ok := idx.ProbeWith(mover, from, to, angle, nil, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
			if ok != !math.IsInf(first.T, 1) || (ok && !nearF(hit.T, first.T)) {
				t.Fatalf("%s at %v: ProbeWith gave %+v, %v, want the nearest the primitive finds, T %v",
					name, angle, hit, ok, first.T)
			}
		}
	}
}

func TestTheShapeProbesAllocateNothing(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	statics := NewStaticIndex(2)
	bodies := NewBodyIndex(2)
	for i := range 64 {
		shape := NewCircleShape(0.4, m.Vec2d{})
		if i%2 == 0 {
			shape = NewBoxShape(0.7, 0.7, 0)
		}
		at := m.Vec2d{X: float64(i%8) * 1.9, Y: float64(i/8) * 1.9}
		statics.Insert(testEntity(i), shape, at, 0, nil)
		bodies.Insert(testEntity(i), shape, at, 0, nil)
	}
	InsertSleeper(bodies, testEntity(64), crate, m.Vec2d{X: 20, Y: 20}, 0, nil)

	from, to := m.Vec2d{X: -2, Y: -2}, m.Vec2d{X: 16, Y: 16}
	hits := make([]Hit, 0, 64)
	hits = statics.ProbeAllWith(hits[:0], crate, from, to, 0.3, nil, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if len(hits) == 0 {
		t.Fatal("the warm-up found no Hits")
	}

	for name, call := range map[string]func(){
		"ProbeShapeWith a box against a box": func() {
			hitSink, boolSink = ProbeShapeWith(crate, origin, m.Vec2d{X: 10}, 0, nil, NewBoxShape(2, 2, 0), m.Vec2d{X: 5}, 0, nil)
		},
		"ProbeShapeWith a box against a segment": func() {
			hitSink, boolSink = ProbeShapeWith(crate, origin, m.Vec2d{X: 10}, 0.3, nil, upright, m.Vec2d{X: 5}, 0, nil)
		},
		"ProbeWith on the static index": func() {
			hitSink, boolSink = statics.ProbeWith(crate, from, to, 0.3, nil, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
		},
		"ProbeAllWith on the static index": func() {
			hits = statics.ProbeAllWith(hits[:0], crate, from, to, 0.3, nil, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
		},
		"ProbeWith on the Body index": func() {
			hitSink, boolSink = bodies.ProbeWith(crate, from, to, 0.3, nil, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
		},
		"ProbeAllWith on the Body index": func() {
			hits = bodies.ProbeAllWith(hits[:0], crate, from, to, 0.3, nil, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
		},
	} {
		if allocations := testing.AllocsPerRun(100, call); allocations != 0 {
			t.Errorf("%s allocated %v times a call, want 0", name, allocations)
		}
	}
}
