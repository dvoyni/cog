package types

import (
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// The swept Sensor's own layer A. Every number here is a closed form worked out
// by hand from the geometry rather than read off cp, cp having no swept Sensor
// to be the oracle for: what it asserts is cp's own segment query — the one the
// Probe is built from — reached through cp's own grid walk. The tolerance is
// the specification's 1e-9 m, and nothing here is loosened.

func TestAMovingCircleSensorIsProbedAlongItsPathAndNotOnlyWhereItLanded(t *testing.T) {
	// A point Sensor crossing a small circle and ending far past it: a discrete
	// test where the tick left it finds nothing at all.
	target := ecs.Entity(100)
	sensor := ecs.Entity(1)

	statics := NewStaticIndex(0)
	statics.Insert(target, NewCircleShape(0.05, m.Vec2d{}), m.Vec2d{X: 2}, 0, nil)

	bodies := NewBodyIndex(0)
	bodies.InsertMoving(sensor, sensorCircle(0), m.Vec2d{X: 4}, m.Vec2d{}, 0, 0, nil)

	contacts := NewContacts(7)
	Collide(contacts, bodies, statics, noJoints, 3, testSlop)

	if contacts.Len() != 1 {
		t.Fatalf("a Sensor swept through a circle made %d Contacts, want 1", contacts.Len())
	}
	entry := contacts.All()[0]
	if entry.A != sensor || entry.B != target {
		t.Errorf("the entry names %v and %v, want the Sensor and what it met", entry.A, entry.B)
	}
	if !entry.Sensor {
		t.Error("the entry does not say it is a Sensor's")
	}
	// The Probe reaches the circle's near surface at x = 2 − 0.05 over a path
	// four metres long.
	wantNear(t, "T", entry.T, 1.95/4)
	wantPoint(t, "Point", entry.Points[0].Point, 1.95, 0)
	wantNormal(t, entry.Normal, -1, 0)
	wantNear(t, "Depth", entry.Points[0].Depth, 0)

	// The same tick with the Sensor merely placed where it ended — which is
	// what a discrete test is — finds nothing, so the Probe is the whole of
	// what stopped it tunnelling.
	discrete := NewBodyIndex(0)
	discrete.Insert(sensor, sensorCircle(0), m.Vec2d{X: 4}, 0, nil)
	landed := NewContacts(7)
	Collide(landed, discrete, statics, noJoints, 3, testSlop)
	if landed.Len() != 0 {
		t.Errorf("the Sensor tested only where it landed found %d Contacts, want none", landed.Len())
	}
}

func TestASensorsEntriesSitTogetherOrderedByTAheadOfTheSolidPairs(t *testing.T) {
	sensor := ecs.Entity(1)
	atOne, atTwo, atThree := ecs.Entity(101), ecs.Entity(102), ecs.Entity(103)

	statics := NewStaticIndex(0)
	// Inserted back to front, so that the order the entries come out in is the
	// Probe's and not the index's.
	statics.Insert(atThree, NewCircleShape(0.05, m.Vec2d{}), m.Vec2d{X: 3}, 0, nil)
	statics.Insert(atOne, NewCircleShape(0.05, m.Vec2d{}), m.Vec2d{X: 1}, 0, nil)
	statics.Insert(atTwo, NewCircleShape(0.05, m.Vec2d{}), m.Vec2d{X: 2}, 0, nil)

	bodies := NewBodyIndex(0)
	bodies.InsertMoving(sensor, sensorCircle(0), m.Vec2d{X: 4}, m.Vec2d{}, 0, 0, nil)
	// Two ordinary Bodies overlapping each other well off the Sensor's path, so
	// that the list holds a solid pair for the Sensor's run to sit ahead of.
	bodies.Insert(ecs.Entity(2), NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{Y: 10}, 0, nil)
	bodies.Insert(ecs.Entity(3), NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{X: 0.5, Y: 10}, 0, nil)

	contacts := NewContacts(7)
	Collide(contacts, bodies, statics, noJoints, 3, testSlop)

	list := contacts.All()
	if len(list) != 4 {
		t.Fatalf("the tick made %d Contacts, want three Hits and one solid pair", len(list))
	}
	for i, want := range []ecs.Entity{atOne, atTwo, atThree} {
		if list[i].A != sensor || list[i].B != want {
			t.Errorf("entry %d names %v and %v, want the Sensor and %v", i, list[i].A, list[i].B, want)
		}
		wantNear(t, "T", list[i].T, (float64(i+1)-0.05)/4)
	}
	if list[3].Sensor {
		t.Error("the solid pair is inside the Sensor's run rather than after it")
	}
}

func TestAProbedSensorThatStartedInsideSomethingReportsTheOverlapAtTheStart(t *testing.T) {
	sensor, wall := ecs.Entity(1), ecs.Entity(100)

	statics := NewStaticIndex(0)
	statics.Insert(wall, NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{X: 0.4}, 0, nil)

	bodies := NewBodyIndex(0)
	bodies.InsertMoving(sensor, sensorCircle(0.5), m.Vec2d{X: 2}, m.Vec2d{}, 0, 0, nil)

	contacts := NewContacts(7)
	Collide(contacts, bodies, statics, noJoints, 3, testSlop)

	if contacts.Len() != 1 {
		t.Fatalf("a Sensor that began inside a circle made %d Contacts, want 1", contacts.Len())
	}
	entry := contacts.All()[0]
	if entry.T != 0 {
		t.Errorf("T is %v, want 0: the Probe started inside", entry.T)
	}
	// Centres 0.4 apart with a summed radius of 1, so the overlap at the start
	// is 0.6, and the surface point is 0.5 back along −X from the wall's centre.
	wantNear(t, "Depth", entry.Points[0].Depth, 0.6)
	wantPoint(t, "Point", entry.Points[0].Point, -0.1, 0)
	wantNormal(t, entry.Normal, -1, 0)
}

func TestEverythingNotProbedReportsTAtOneWithTheOverlapWhereTheTickEnded(t *testing.T) {
	// A segment Sensor is tested discretely however fast it moved, which is the
	// accepted hole: only the circle kind is swept.
	shape := NewSegmentShape(m.Vec2d{X: -1}, m.Vec2d{X: 1}, 0.1)
	shape.Sensor = true

	bodies := NewBodyIndex(0)
	bodies.InsertMoving(ecs.Entity(1), shape, m.Vec2d{}, m.Vec2d{X: -8}, 0, 0, nil)
	bodies.Insert(ecs.Entity(2), NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{Y: 0.5}, 0, nil)

	contacts := NewContacts(7)
	Collide(contacts, bodies, NewStaticIndex(0), noJoints, 3, testSlop)

	if contacts.Len() != 1 {
		t.Fatalf("a segment Sensor overlapping a circle made %d Contacts, want 1", contacts.Len())
	}
	entry := contacts.All()[0]
	if entry.A != ecs.Entity(1) || !entry.Sensor {
		t.Errorf("the entry names %v and reports Sensor %v, want the Sensor and true", entry.A, entry.Sensor)
	}
	if entry.T != 1 {
		t.Errorf("T is %v; everything found where the tick ended reports 1", entry.T)
	}
	wantNear(t, "Depth", entry.Points[0].Depth, 0.1)
}

func TestASensorThatDidNotMoveIsTestedWhereItStands(t *testing.T) {
	// This is the teleport rule from the other side: an app that teleports a
	// Sensor sets Previous = Current, and a Sensor whose path is a point is not
	// a moving Sensor at all.
	bodies := NewBodyIndex(0)
	bodies.InsertMoving(ecs.Entity(1), sensorCircle(0.5), m.Vec2d{X: 5}, m.Vec2d{X: 5}, 0, 0, nil)
	bodies.Insert(ecs.Entity(2), NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{X: 5.6}, 0, nil)

	contacts := NewContacts(7)
	Collide(contacts, bodies, NewStaticIndex(0), noJoints, 3, testSlop)

	if contacts.Len() != 1 {
		t.Fatalf("a teleported Sensor made %d Contacts, want 1", contacts.Len())
	}
	entry := contacts.All()[0]
	if entry.T != 1 {
		t.Errorf("T is %v, want 1: a Sensor that did not move is not Probed", entry.T)
	}
	wantNear(t, "Depth", entry.Points[0].Depth, 0.4)
}

func TestAStaticSensorIsNeverProbedAndQueriesStillFindIt(t *testing.T) {
	shape := sensorCircle(0.5)
	statics := NewStaticIndex(0)
	statics.Insert(ecs.Entity(100), shape, m.Vec2d{}, 0, nil)

	// Queries do not skip Sensors, which departs from cp: its point and segment
	// queries skip them unconditionally, and Overlap has to be able to find one.
	all := CollisionBitsAll
	if _, ok := statics.Probe(m.Vec2d{X: -5}, m.Vec2d{X: 5}, 0, all, all, ecs.NoEntity); !ok {
		t.Error("a Probe walked straight through a Sensor")
	}
	if found := statics.Overlap(nil, NewCircleShape(0.1, m.Vec2d{}), m.Vec2d{}, 0, nil,
		all, all, ecs.NoEntity); len(found) != 1 {
		t.Errorf("Overlap found %d Shapes, want the Sensor", len(found))
	}

	bodies := NewBodyIndex(0)
	bodies.InsertMoving(ecs.Entity(1), NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{X: 0.6}, m.Vec2d{X: -8}, 0, 0, nil)

	contacts := NewContacts(7)
	Collide(contacts, bodies, statics, noJoints, 3, testSlop)
	if contacts.Len() != 1 {
		t.Fatalf("a Static Sensor made %d Contacts, want 1", contacts.Len())
	}
	if got := contacts.All()[0].T; got != 1 {
		t.Errorf("T is %v, want 1: a Static Sensor is never Probed", got)
	}
}

func TestTwoMovingSensorsThatFindEachOtherKeepTheSmallerT(t *testing.T) {
	first, second := ecs.Entity(1), ecs.Entity(2)

	// Each Sensor Probes the other at the position the tick left it in, so both
	// find the pair and only the smaller T is written. The two cases differ in
	// which of them is swept first, so the answer cannot be the walk's order.
	cases := []struct {
		name                         string
		fromFirst, toFirst           m.Vec2d
		fromSecond, toSecond         m.Vec2d
		wantA, wantB                 ecs.Entity
		wantT, wantPointX, wantNorml float64
	}{{
		// The first reaches the second's end position, grown by both radii, at
		// x = 0.8 over a two-metre path; the second would have reached the
		// first's at x = 2.2 over four metres, which is later.
		name:       "the Sensor swept first has the smaller T",
		fromFirst:  m.Vec2d{},
		toFirst:    m.Vec2d{X: 2},
		fromSecond: m.Vec2d{X: 5},
		toSecond:   m.Vec2d{X: 1},
		wantA:      first,
		wantB:      second,
		wantT:      0.4,
		wantPointX: 0.9,
		wantNorml:  -1,
	}, {
		// The other way round: the second reaches the first's end position at
		// x = 4.2 over a 3.4 m path, well before the first reaches x = 0.8 over
		// four metres.
		name:       "the Sensor swept second has the smaller T",
		fromFirst:  m.Vec2d{},
		toFirst:    m.Vec2d{X: 4},
		fromSecond: m.Vec2d{X: 4.4},
		toSecond:   m.Vec2d{X: 1},
		wantA:      second,
		wantB:      first,
		wantT:      0.2 / 3.4,
		wantPointX: 4.1,
		wantNorml:  1,
	}}

	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			bodies := NewBodyIndex(0)
			bodies.InsertMoving(first, sensorCircle(0.1), each.toFirst, each.fromFirst, 0, 0, nil)
			bodies.InsertMoving(second, sensorCircle(0.1), each.toSecond, each.fromSecond, 0, 0, nil)

			contacts := NewContacts(7)
			Collide(contacts, bodies, NewStaticIndex(0), noJoints, 3, testSlop)

			if contacts.Len() != 1 {
				t.Fatalf("two Sensors that found each other made %d Contacts, want 1", contacts.Len())
			}
			entry := contacts.All()[0]
			if entry.A != each.wantA || entry.B != each.wantB {
				t.Errorf("the entry names %v and %v, want %v and %v",
					entry.A, entry.B, each.wantA, each.wantB)
			}
			wantNear(t, "T", entry.T, each.wantT)
			wantPoint(t, "Point", entry.Points[0].Point, each.wantPointX, 0)
			wantNormal(t, entry.Normal, each.wantNorml, 0)
		})
	}
}

func TestASweptSensorIsFilteredByTheGroupsBeforeAnyShapeTest(t *testing.T) {
	const (
		walls       uint32 = 1 << 0
		projectiles uint32 = 1 << 1
		ghosts      uint32 = 1 << 2
	)
	sensor, wall := ecs.Entity(1), ecs.Entity(100)

	sweep := func(bits, collidesWith uint32) int {
		shape := sensorCircle(0)
		shape.CollisionBits, shape.CollidesWith = bits, collidesWith

		stone := NewCircleShape(0.05, m.Vec2d{})
		stone.CollisionBits, stone.CollidesWith = walls, projectiles

		statics := NewStaticIndex(0)
		statics.Insert(wall, stone, m.Vec2d{X: 2}, 0, nil)
		bodies := NewBodyIndex(0)
		bodies.InsertMoving(sensor, shape, m.Vec2d{X: 4}, m.Vec2d{}, 0, 0, nil)

		contacts := NewContacts(7)
		Collide(contacts, bodies, statics, noJoints, 3, testSlop)
		return contacts.Len()
	}

	if got := sweep(projectiles, walls); got != 1 {
		t.Errorf("two sides that each look for the other made %d Contacts, want 1", got)
	}
	if got := sweep(projectiles, ghosts); got != 0 {
		t.Errorf("a Sensor looking only for ghosts still met the wall %d times", got)
	}
	if got := sweep(ghosts, walls); got != 0 {
		t.Errorf("a Sensor in a group the wall does not look for was still met %d times", got)
	}
	if got := sweep(CollisionBitsNone, CollisionBitsAll); got != 0 {
		t.Errorf("a Sensor in no group at all was seen %d times", got)
	}
	if got := sweep(CollisionBitsAll, CollisionBitsNone); got != 0 {
		t.Errorf("a Sensor looking for nothing still found %d things", got)
	}
}

func TestASolidPairIsFilteredByTheGroupsBeforeAnyShapeTest(t *testing.T) {
	const (
		crates uint32 = 1 << 0
		crowd  uint32 = 1 << 1
	)
	crate := NewCircleShape(0.5, m.Vec2d{})
	crate.CollisionBits, crate.CollidesWith = crates, crates
	person := NewCircleShape(0.5, m.Vec2d{})
	person.CollisionBits, person.CollidesWith = crowd, crowd

	bodies := NewBodyIndex(0)
	// Deeply overlapping, so only the groups can be what keeps them apart. The
	// Body index is kept current by a Clear and a refill, as Index keeps it.
	rebuild := func() {
		bodies.Clear()
		bodies.Insert(ecs.Entity(1), crate, m.Vec2d{}, 0, nil)
		bodies.Insert(ecs.Entity(2), person, m.Vec2d{X: 0.1}, 0, nil)
	}
	rebuild()

	contacts := NewContacts(7)
	Collide(contacts, bodies, NewStaticIndex(0), noJoints, 3, testSlop)
	if contacts.Len() != 0 {
		t.Fatalf("two Shapes in groups that do not look for each other made %d Contacts", contacts.Len())
	}

	// The same two Shapes, one of them widened to look for the other, touch.
	person.CollidesWith = crowd | crates
	rebuild()
	Collide(contacts, bodies, NewStaticIndex(0), noJoints, 3, testSlop)
	if contacts.Len() != 0 {
		t.Fatalf("one side alone looking for the other made %d Contacts, want none", contacts.Len())
	}
	crate.CollidesWith = crates | crowd
	rebuild()
	Collide(contacts, bodies, NewStaticIndex(0), noJoints, 3, testSlop)
	if contacts.Len() != 1 {
		t.Fatalf("two sides that each look for the other made %d Contacts, want 1", contacts.Len())
	}
}

func TestASweptSensorPairBeginsThenContinuesThenEndsExactlyOnce(t *testing.T) {
	sensor, target := ecs.Entity(1), ecs.Entity(100)
	statics := NewStaticIndex(0)
	statics.Insert(target, NewCircleShape(0.05, m.Vec2d{}), m.Vec2d{X: 2}, 0, nil)

	bodies := NewBodyIndex(0)
	sweep := func(from, to m.Vec2d) {
		bodies.Clear()
		bodies.InsertMoving(sensor, sensorCircle(0), to, from, 0, 0, nil)
	}

	contacts := NewContacts(7)
	sweep(m.Vec2d{}, m.Vec2d{X: 4})
	Collide(contacts, bodies, statics, noJoints, 3, testSlop)
	wantPhases(t, contacts, PhaseBegan)

	sweep(m.Vec2d{}, m.Vec2d{X: 4})
	Collide(contacts, bodies, statics, noJoints, 3, testSlop)
	wantPhases(t, contacts, PhaseContinuing)

	// Swept somewhere else entirely, so the pair comes apart.
	sweep(m.Vec2d{Y: 20}, m.Vec2d{X: 4, Y: 20})
	Collide(contacts, bodies, statics, noJoints, 3, testSlop)
	wantPhases(t, contacts, PhaseEnded)

	Collide(contacts, bodies, statics, noJoints, 3, testSlop)
	wantPhases(t, contacts)
}

func TestTheSweptSensorPathAllocatesNothing(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	statics := NewStaticIndex(0)
	for i := range 32 {
		statics.Insert(ecs.Entity(1000+i), NewCircleShape(0.2, m.Vec2d{}),
			m.Vec2d{X: float64(i) * 0.5}, 0, nil)
	}

	bodies := NewBodyIndex(0)
	fill := func() {
		bodies.Clear()
		for i := range 16 {
			from := m.Vec2d{Y: float64(i) * 0.01}
			to := m.Vec2d{X: 16, Y: float64(i) * 0.01}
			bodies.InsertMoving(ecs.Entity(1+i), sensorCircle(0.05), to, from, 0, 0, nil)
		}
	}

	contacts := NewContacts(7)
	// Warm every buffer the first ticks grow — the entry buffers, the two maps,
	// and the Probe scratch the sweep refills once a Sensor.
	for range 8 {
		fill()
		Collide(contacts, bodies, statics, noJoints, 3, testSlop)
	}
	if contacts.Len() == 0 {
		t.Fatal("the scene the measurement runs over found no Contacts at all")
	}
	if !contacts.All()[0].Sensor {
		t.Fatal("the scene the measurement runs over found no Sensor Hits at all")
	}

	if got := testing.AllocsPerRun(200, func() {
		fill()
		Collide(contacts, bodies, statics, noJoints, 3, testSlop)
	}); got != 0 {
		t.Errorf("sweeping the Sensors allocates %v objects a tick, want none", got)
	}
}

// sensorCircle is a circle Shape marked a Sensor, which is the whole of what a
// projectile is: the package has no projectile concept of its own.
func sensorCircle(radius float64) Shape {
	shape := NewCircleShape(radius, m.Vec2d{})
	shape.Sensor = true
	return shape
}
