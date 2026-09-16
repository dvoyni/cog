package types

import (
	"math"
	"testing"
	"time"
	"unsafe"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// Every number in this file that names cp came out of a throwaway harness that
// imports github.com/jakecoffman/cp/v2 v2.4.0 and prints what its own arbiter
// holds, run at the port's settings — ten iterations, a Slop of 0.005 m, no
// gravity and no damping. main never gains the dependency; what is committed is
// what the harness emitted, which is the specification's own arrangement.
//
// Tolerance is the specification's 1e-9 m on points and depth and 1e-9 on the
// normal's components. There is no mismatch budget and nothing here is
// loosened: every case agrees with cp exactly or is named as a departure.
const oracleTolerance = 1e-9

func TestTheContactEntryIsExactlyTheBytesTheSpecificationLaysOut(t *testing.T) {
	if got := unsafe.Sizeof(Contact{}); got != 320 {
		t.Errorf("a Contact is %d bytes, want 320", got)
	}
	if got := unsafe.Sizeof(ContactPoint{}); got != 120 {
		t.Errorf("a ContactPoint is %d bytes, want 120", got)
	}
	// Later tickets and the allocation claim both depend on the layout, not
	// only on the total, so the two runs of it are pinned as well.
	if got := unsafe.Sizeof([2]ContactPoint{}); got != 240 {
		t.Errorf("a Contact's two points are %d bytes, want 240", got)
	}
}

func TestCircleAgainstCircleIsCpsClosedFormPointForPoint(t *testing.T) {
	a := NewCircleShape(0.5, m.Vec2d{})
	b := NewCircleShape(0.7, m.Vec2d{})
	touch, ok := collide(t, a, m.Vec2d{}, b, m.Vec2d{X: 0.9, Y: 0.3})
	if !ok {
		t.Fatal("two circles 0.9487 m apart with a summed radius of 1.2 m do not touch")
	}

	// cp reports the pair from its own first Shape towards its second; the port
	// reports it from the first Shape the caller named, which is what makes the
	// nine-arm switch's kind ordering invisible.
	wantNormal(t, touch.normal, 0.94868329805051388, 0.31622776601683794)
	if touch.count != 1 {
		t.Fatalf("a circle against a circle has %d points, want 1", touch.count)
	}
	wantPoint(t, "p1", touch.points[0].p1, 0.47434164902525694, 0.15811388300841897)
	wantPoint(t, "p2", touch.points[0].p2, 0.23592169136464036, 0.078640563788213436)
	wantNear(t, "depth", touch.points[0].depth, 0.25131670194948624)
}

func TestCircleAgainstSegmentIsCpsClosedFormPointForPoint(t *testing.T) {
	circle := NewCircleShape(0.5, m.Vec2d{})
	wall := NewSegmentShape(m.Vec2d{X: -2}, m.Vec2d{X: 2}, 0.1)
	touch, ok := collide(t, circle, m.Vec2d{X: 0.25, Y: 0.4}, wall, m.Vec2d{})
	if !ok {
		t.Fatal("a circle 0.4 m above a 0.1 m wall with a 0.5 m radius does not touch it")
	}

	wantNormal(t, touch.normal, 0, -1)
	wantPoint(t, "p1", touch.points[0].p1, 0.25, -0.099999999999999978)
	wantPoint(t, "p2", touch.points[0].p2, 0.25, 0.10000000000000001)
	wantNear(t, "depth", touch.points[0].depth, 0.19999999999999998)
}

func TestACircleOffTheEndOfASegmentMeetsItsCap(t *testing.T) {
	// cp's end-cap rejection reads the neighbours' tangents, which nothing in cp
	// ever writes — defect 4 — so at the port's own zero tangents the rejection
	// is the no-op it is in cp and the cap is found.
	circle := NewCircleShape(0.5, m.Vec2d{})
	wall := NewSegmentShape(m.Vec2d{X: -2}, m.Vec2d{X: 2}, 0.1)
	touch, ok := collide(t, circle, m.Vec2d{X: 2.3, Y: 0.2}, wall, m.Vec2d{})
	if !ok {
		t.Fatal("a circle overlapping a segment's end cap does not touch it")
	}

	wantNormal(t, touch.normal, -0.83205029433784361, -0.55470019622522937)
	wantPoint(t, "p1", touch.points[0].p1, 1.883974852831078, -0.077350098112614674)
	wantPoint(t, "p2", touch.points[0].p2, 2.0832050294337843, 0.055470019622522938)
	wantNear(t, "depth", touch.points[0].depth, 0.23944487245360122)
}

func TestTheDepthAndTheTwoPointsSayTheSameThing(t *testing.T) {
	// PreStep reads Depth where cp recomputes (r2 − r1 + bodyDelta)·n. The two
	// are equal by construction, and this is the construction.
	a := NewCircleShape(0.4, m.Vec2d{})
	b := NewSegmentShape(m.Vec2d{X: -1, Y: -0.3}, m.Vec2d{X: 1, Y: 0.6}, 0.05)
	for _, at := range []m.Vec2d{
		{X: 0, Y: 0.2}, {X: -0.6, Y: -0.4}, {X: 0.9, Y: 0.8}, {X: 1.1, Y: 0.6},
	} {
		touch, ok := collide(t, a, at, b, m.Vec2d{})
		if !ok {
			continue
		}
		point := touch.points[0]
		derived := -point.p2.Sub(point.p1).Dot(touch.normal)
		if math.Abs(derived-point.depth) > oracleTolerance {
			t.Errorf("at %v the stored depth is %v and the points say %v", at, point.depth, derived)
		}
	}
}

func TestPenetrationStillReportsNoDirectionOnCoincidentCentres(t *testing.T) {
	a := NewCircleShape(0.5, m.Vec2d{})
	b := NewCircleShape(0.7, m.Vec2d{})
	normal, depth, ok := Penetration(a, m.Vec2d{X: 3, Y: 4}, 0, nil, b, m.Vec2d{X: 3, Y: 4}, 0, nil)
	if !ok {
		t.Fatal("two circles sharing a centre do not touch")
	}
	if normal != (m.Vec2d{}) {
		t.Errorf("the pure form invented the direction %v; choosing one is the caller's", normal)
	}
	if !near(depth, 1.2) {
		t.Errorf("the overlap of two coincident circles is %v, want their summed radius 1.2", depth)
	}
}

func TestTwoCoincidentCirclesArePartedAlongTheSeededDirection(t *testing.T) {
	// cp answers a fixed (1, 0) here, which never breaks the symmetry it is
	// there to break. This is the one place randomness enters the package.
	first, second := detectCoincident(t, 1), detectCoincident(t, 2)
	if first == second {
		t.Fatalf("two seeds parted the same pair the same way, along %v", first)
	}
	for _, normal := range []m.Vec2d{first, second, detectCoincident(t, 0)} {
		if !near(normal.Length(), 1) {
			t.Errorf("the nudge %v is not a unit vector", normal)
		}
	}
	// A seed of zero is an ordinary seed and not a request for an arbitrary
	// one, so the same seed always parts the pair the same way.
	if a, b := detectCoincident(t, 0), detectCoincident(t, 0); a != b {
		t.Errorf("the same seed parted the pair along %v and then %v", a, b)
	}
}

func TestAPairIsReportedOnceHoweverManyCellsItSpans(t *testing.T) {
	// A Shape listed in nine cells would be met nine times by a walk with no
	// dedupe. The port has no per-query stamp to spend, its indices being
	// Resources concurrent readers share, so the pair is tested at one cell of
	// the two Shapes' overlap instead.
	contacts := NewContacts(7)
	bodies, statics := NewBodyIndex(0.25), NewStaticIndex(0.25)
	bodies.Insert(ecs.Entity(1), NewCircleShape(1.5, m.Vec2d{}), m.Vec2d{}, 0, nil)
	bodies.Insert(ecs.Entity(2), NewCircleShape(1.5, m.Vec2d{}), m.Vec2d{X: 1}, 0, nil)
	statics.Insert(ecs.Entity(3), NewCircleShape(1.5, m.Vec2d{}), m.Vec2d{Y: 1}, 0, nil)

	Collide(contacts, bodies, statics, 3)
	if got := contacts.Len(); got != 3 {
		t.Fatalf("three mutually overlapping Shapes made %d Contacts, want 3", got)
	}
	seen := map[[2]ecs.Entity]int{}
	for _, entry := range contacts.All() {
		low, high := order(entry.A, entry.B)
		seen[[2]ecs.Entity{low, high}]++
	}
	for pair, count := range seen {
		if count != 1 {
			t.Errorf("the pair %v was reported %d times", pair, count)
		}
	}
}

func TestAIsTheNonStaticPartyAndTheNormalFacesIt(t *testing.T) {
	contacts := NewContacts(7)
	bodies, statics := NewBodyIndex(0), NewStaticIndex(0)
	body, wall := ecs.Entity(9), ecs.Entity(4)
	bodies.Insert(body, NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{Y: 0.4}, 0, nil)
	statics.Insert(wall, NewSegmentShape(m.Vec2d{X: -2}, m.Vec2d{X: 2}, 0), m.Vec2d{}, 0, nil)

	Collide(contacts, bodies, statics, 3)
	if contacts.Len() != 1 {
		t.Fatalf("a circle resting in a wall made %d Contacts, want 1", contacts.Len())
	}
	entry := contacts.All()[0]
	if entry.A != body || entry.B != wall {
		t.Fatalf("A is %v and B is %v; A is the party that is not Static", entry.A, entry.B)
	}
	// The normal is B's surface facing A, so it points from the wall up at the
	// circle, whatever the order the walk met the two in.
	wantNormal(t, entry.Normal, 0, 1)
	if got := entry.NormalFor(wall); !near(got.Y, -1) {
		t.Errorf("the wall's own view of the normal is %v, want it facing away from the circle", got)
	}
	// Point is on B's surface: the wall, under the circle's centre.
	wantPoint(t, "Point", entry.Points[0].Point, 0, 0)
	wantNear(t, "Depth", entry.Points[0].Depth, 0.1)
}

func TestAIsTheSensorBeforeItIsAnythingElse(t *testing.T) {
	contacts := NewContacts(7)
	bodies, statics := NewBodyIndex(0), NewStaticIndex(0)
	sensor := NewCircleShape(0.5, m.Vec2d{})
	sensor.Sensor = true
	// The Sensor is the higher Entity and the Static one, which is what makes
	// the rule's order visible: both of the later clauses would pick the other.
	statics.Insert(ecs.Entity(20), sensor, m.Vec2d{}, 0, nil)
	bodies.Insert(ecs.Entity(3), NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{X: 0.5}, 0, nil)

	Collide(contacts, bodies, statics, 3)
	if contacts.Len() != 1 {
		t.Fatalf("a Sensor overlapping a Body made %d Contacts, want 1", contacts.Len())
	}
	entry := contacts.All()[0]
	if entry.A != ecs.Entity(20) {
		t.Errorf("A is %v, want the Sensor", entry.A)
	}
	if !entry.Sensor {
		t.Error("the entry does not say it is a Sensor's")
	}
	if entry.T != 1 {
		t.Errorf("T is %v; everything found where the tick ended reports 1", entry.T)
	}
}

func TestAPairBeginsThenContinuesThenEndsExactlyOnce(t *testing.T) {
	contacts := NewContacts(7)
	bodies, statics := NewBodyIndex(0), NewStaticIndex(0)
	one, two := ecs.Entity(1), ecs.Entity(2)

	touching := func(apart float64) {
		bodies.Clear()
		bodies.Insert(one, NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{}, 0, nil)
		bodies.Insert(two, NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{X: apart}, 0, nil)
		Collide(contacts, bodies, statics, 3)
	}

	touching(0.9)
	wantPhases(t, contacts, PhaseBegan)
	touching(0.95)
	wantPhases(t, contacts, PhaseContinuing)
	touching(2)
	wantPhases(t, contacts, PhaseEnded)
	// The end is reported once; after it the pair is carried cached, invisible,
	// until the persistence window closes.
	touching(2)
	wantPhases(t, contacts)
	touching(2)
	wantPhases(t, contacts)
	touching(0.9)
	wantPhases(t, contacts, PhaseBegan)
}

func TestAnEndedEntryKeepsTheGeometryOfTheTickItLastTouched(t *testing.T) {
	// This departs from cp, whose Count() returns 0 once an arbiter is CACHED,
	// so its Separate callback sees no points at all.
	contacts := NewContacts(7)
	bodies, statics := NewBodyIndex(0), NewStaticIndex(0)
	place := func(apart float64) {
		bodies.Clear()
		bodies.Insert(ecs.Entity(1), NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{}, 0, nil)
		bodies.Insert(ecs.Entity(2), NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{X: apart}, 0, nil)
		Collide(contacts, bodies, statics, 3)
	}

	place(0.9)
	touched := contacts.All()[0]
	place(3)

	ended := contacts.All()[0]
	if ended.Phase != PhaseEnded {
		t.Fatalf("the entry is %v, want Ended", ended.Phase)
	}
	if ended.Count != touched.Count || ended.Points[0].Point != touched.Points[0].Point ||
		ended.Points[0].Depth != touched.Points[0].Depth || ended.Normal != touched.Normal {
		t.Errorf("the Ended entry %+v is not the geometry of the tick it last touched", ended)
	}
}

func TestAPairThatFlickersApartAndBackKeepsItsImpulses(t *testing.T) {
	// cp removes an arbiter only after collisionPersistence steps, so a pair
	// that separates and re-touches inside the window warm starts from what it
	// had — its CACHED to FIRST_COLLISION revival.
	contacts := NewContacts(7)
	bodies, statics := NewBodyIndex(0), NewStaticIndex(0)
	place := func(apart float64) {
		bodies.Clear()
		bodies.Insert(ecs.Entity(1), NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{}, 0, nil)
		bodies.Insert(ecs.Entity(2), NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{X: apart}, 0, nil)
		Collide(contacts, bodies, statics, 3)
	}

	place(0.9)
	// Stand in for a solve: the accumulated Impulse is what crosses the tick.
	contacts.entries[0].Points[0].NormalImpulse = 17.5
	contacts.entries[0].Points[0].TangentImpulse = -2.25

	place(3) // Ended, carried forward as an Impulse carrier.
	place(0.9)

	revived := contacts.All()[0]
	if revived.Phase != PhaseBegan {
		t.Errorf("a revival is %v, want Began — cp's CACHED becomes FIRST_COLLISION", revived.Phase)
	}
	if !near(revived.Points[0].NormalImpulse, 17.5) || !near(revived.Points[0].TangentImpulse, -2.25) {
		t.Errorf("the revival carries %v and %v, want 17.5 and -2.25",
			revived.Points[0].NormalImpulse, revived.Points[0].TangentImpulse)
	}
}

func TestAPairOutsideThePersistenceWindowStartsOver(t *testing.T) {
	contacts := NewContacts(7)
	bodies, statics := NewBodyIndex(0), NewStaticIndex(0)
	place := func(apart float64) {
		bodies.Clear()
		bodies.Insert(ecs.Entity(1), NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{}, 0, nil)
		bodies.Insert(ecs.Entity(2), NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{X: apart}, 0, nil)
		Collide(contacts, bodies, statics, 3)
	}

	place(0.9)
	contacts.entries[0].Points[0].NormalImpulse = 17.5
	for range 4 {
		place(3)
	}
	// The buffer holds nothing at all now, the cached run having expired.
	if got := len(contacts.entries); got != 0 {
		t.Fatalf("the buffer still carries %d entries past the window", got)
	}
	place(0.9)
	if got := contacts.All()[0].Points[0].NormalImpulse; got != 0 {
		t.Errorf("a pair outside the window warm started from %v, want nothing", got)
	}
}

func TestTheOnlyBitsThatCrossATickAreTheOnesTheSpecificationNames(t *testing.T) {
	contacts := NewContacts(7)
	bodies, statics := NewBodyIndex(0), NewStaticIndex(0)
	place := func() {
		bodies.Clear()
		bodies.Insert(ecs.Entity(1), NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{}, 0, nil)
		bodies.Insert(ecs.Entity(2), NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{X: 0.9}, 0, nil)
		Collide(contacts, bodies, statics, 3)
	}

	place()
	// Material is recomputed from the two Shapes every tick, as cp does, which
	// is what makes a filter's edit of it last exactly one tick.
	contacts.entries[0].Friction = 0.9
	contacts.entries[0].Restitution = 0.5
	contacts.entries[0].SurfaceVelocity = m.Vec2d{X: 4}
	contacts.entries[0].Points[0].nMass = 12

	place()
	entry := contacts.All()[0]
	if entry.Friction != 0 || entry.Restitution != 0 || entry.SurfaceVelocity != (m.Vec2d{}) {
		t.Errorf("a filter's material edit outlived its tick: %+v", entry)
	}
	if entry.Points[0].nMass != 0 {
		t.Errorf("PreStep's scratch crossed a tick: nMass is %v", entry.Points[0].nMass)
	}
}

func TestTotalImpulseAndTotalKEReadTheSolutionOffTheEntry(t *testing.T) {
	var entry Contact
	entry.Count = 1
	entry.Normal = m.Vec2d{X: 0, Y: 1}
	entry.Restitution = 0.5
	entry.Points[0].NormalImpulse = 3
	entry.Points[0].TangentImpulse = 4
	entry.Points[0].nMass = 2
	entry.Points[0].tMass = 8

	// The impulse is Σ rotate(Normal, (jn, jt)), and it is the one applied to A
	// — cp negates its own unless swapped, a wart of the per-pair handlers this
	// port does not have.
	impulse := entry.TotalImpulse()
	wantPoint(t, "TotalImpulse", impulse, -4, 3)

	// Chipmunk's cpArbiterTotalKE, which jakecoffman/cp has no counterpart for.
	eCoef := (1 - 0.5) / (1 + 0.5)
	wantNear(t, "TotalKE", entry.TotalKE(), eCoef*9/2+16/8.0)

	// A point PreStep never reached has no mass at all, and an Ended entry
	// keeping its points is what makes that reachable.
	var untouched Contact
	untouched.Count = 1
	untouched.Points[0].NormalImpulse = 3
	if got := untouched.TotalKE(); got != 0 {
		t.Errorf("an entry the solver never reached reports %v of energy, want none", got)
	}
}

func TestAnExcludedTickForgetsTheSolutionItNeverHad(t *testing.T) {
	// cp drops an excluded pair's contacts outright at space.go's else branch,
	// so its next Update finds none to copy an accumulated Impulse from. Every
	// one of the four exclusions is the same rule: a tick with no solution has
	// the solution zero, and the alternative applies a sixty-tick-old Impulse
	// when a filter changes its mind.
	for _, exclusion := range []struct {
		name string
		mark func(entry *Contact)
	}{
		{"a Sensor", func(entry *Contact) { entry.Sensor = true }},
		{"a dropped entry", func(entry *Contact) { entry.Drop() }},
		{"an ignored entry", func(entry *Contact) { entry.Ignore() }},
	} {
		contacts := NewContacts(7)
		bodies, statics := NewBodyIndex(0), NewStaticIndex(0)
		bodies.Clear()
		bodies.Insert(ecs.Entity(1), NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{}, 0, nil)
		bodies.Insert(ecs.Entity(2), NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{X: 0.9}, 0, nil)
		Collide(contacts, bodies, statics, 3)

		entry := &contacts.entries[0]
		entry.Points[0].NormalImpulse = 17.5
		entry.Points[0].TangentImpulse = -2.25
		exclusion.mark(entry)
		// The Bodies are both massless here, which is the fourth exclusion, so
		// the list is walked twice over: beginSolve takes out the three marks
		// and preStep takes out the mass.
		contacts.beginSolve()
		contacts.preStep(1.0/60, 0.005, 6.32)

		if got := contacts.entries[0].Points[0]; got.NormalImpulse != 0 || got.TangentImpulse != 0 {
			t.Errorf("%s kept %v and %v across a tick it was never solved on",
				exclusion.name, got.NormalImpulse, got.TangentImpulse)
		}
	}
}

func TestAnIgnoredPairContinuesWhileADroppedOneBeginsAgain(t *testing.T) {
	// The two marks differ in one thing and it is visible in the phase: a drop
	// is for one tick and the filter re-decides, so the pair begins again — the
	// one accepted difference from cp — while an ignore runs until the pair
	// comes apart, so there is nothing to re-decide and the pair Continues, as
	// cp's own IGNORE state does.
	for _, mark := range []struct {
		name  string
		apply func(entry *Contact)
		want  Phase
	}{
		{"an ignore", func(entry *Contact) { entry.Ignore() }, PhaseContinuing},
		{"a drop", func(entry *Contact) { entry.Drop() }, PhaseBegan},
	} {
		contacts := NewContacts(7)
		bodies, statics := NewBodyIndex(0), NewStaticIndex(0)
		place := func() {
			bodies.Clear()
			bodies.Insert(ecs.Entity(1), NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{}, 0, nil)
			bodies.Insert(ecs.Entity(2), NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{X: 0.9}, 0, nil)
			Collide(contacts, bodies, statics, 3)
		}

		place()
		mark.apply(&contacts.entries[0])
		place()
		if got := contacts.All()[0].Phase; got != mark.want {
			t.Errorf("after %s the pair is %v, want %v", mark.name, got, mark.want)
		}
	}
}

func TestASeedThatWouldStopTheNudgeIsMovedOffIt(t *testing.T) {
	// The mixing is a multiply by an odd constant, so exactly one seed lands on
	// the generator's fixed point. Landing there would hand back cp's own fixed
	// (1, 0) for ever, which is the thing the nudge replaces.
	for _, seed := range []uint64{0, 1, math.MaxUint64, math.MaxUint64 - 1} {
		contacts := NewContacts(seed)
		if contacts.nudge == 0 {
			t.Fatalf("the seed %d left the nudge stopped", seed)
		}
		first, second := contacts.nudgeNormal(), contacts.nudgeNormal()
		if first == second {
			t.Errorf("the seed %d draws %v every time", seed, first)
		}
	}
}

func TestABodyWithAnUnplaceableBoxIsSkippedRatherThanSearchedFor(t *testing.T) {
	// A Shape placed at a NaN or an infinity is listed in no cell at all, which
	// the index already refuses to do. The static half of the walk derives its
	// cell range from the box rather than from that refusal, so it has to make
	// the same one: a box running from one infinity to the other would clamp to
	// the two ends of an int32 grid and be searched for cell by cell.
	//
	// It is asserted as a deadline rather than as a value, because the failure
	// is a walk that does not end.
	contacts := NewContacts(7)
	bodies, statics := NewBodyIndex(0), NewStaticIndex(0)
	statics.Insert(ecs.Entity(1), NewCircleShape(0.4, m.Vec2d{}), m.Vec2d{}, 0, nil)
	bodies.Insert(ecs.Entity(2), NewCircleShape(0.4, m.Vec2d{}), m.Vec2d{X: 0.5}, 0, nil)
	bodies.Insert(ecs.Entity(3),
		NewSegmentShape(m.Vec2d{X: math.Inf(-1)}, m.Vec2d{X: math.Inf(1)}, 0), m.Vec2d{}, 0, nil)

	done := make(chan struct{})
	go func() {
		defer close(done)
		Collide(contacts, bodies, statics, 3)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("detection is still walking the grid looking for a Shape that is listed in no cell")
	}

	if contacts.Len() != 1 {
		t.Fatalf("the placeable pair gave %d Contacts, want 1", contacts.Len())
	}
	entry := contacts.All()[0]
	if entry.A != ecs.Entity(2) || entry.B != ecs.Entity(1) {
		t.Errorf("the entry names %v and %v, want the Body and the Static one", entry.A, entry.B)
	}
}

func TestDetectionAllocatesNothing(t *testing.T) {
	contacts := NewContacts(7)
	bodies, statics := NewBodyIndex(0), NewStaticIndex(0)
	for i := range 64 {
		statics.Insert(ecs.Entity(1000+i), NewCircleShape(0.4, m.Vec2d{}),
			m.Vec2d{X: float64(i%8) * 0.7, Y: float64(i/8) * 0.7}, 0, nil)
	}
	fill := func() {
		bodies.Clear()
		for i := range 64 {
			bodies.Insert(ecs.Entity(1+i), NewCircleShape(0.4, m.Vec2d{}),
				m.Vec2d{X: float64(i%8) * 0.7, Y: float64(i/8) * 0.7}, 0, nil)
		}
	}
	// Warm every buffer the first ticks grow — the two entry buffers, the two
	// maps and the index's own arena — so what is measured is the steady state.
	for range 8 {
		fill()
		Collide(contacts, bodies, statics, 3)
	}
	if contacts.Len() == 0 {
		t.Fatal("the scene the measurement runs over found no Contacts at all")
	}

	if got := testing.AllocsPerRun(200, func() {
		fill()
		Collide(contacts, bodies, statics, 3)
	}); got != 0 {
		t.Errorf("detection allocates %v objects a tick, want none", got)
	}
}

// collide places two Shapes and runs the narrowphase over them, which is what a
// pair test is with the index taken away.
func collide(t testing.TB, a Shape, atA m.Vec2d, b Shape, atB m.Vec2d) (touching, bool) {
	t.Helper()
	var worldA, worldB [worldScratchLen]m.Vec2d
	transformA := NewTransformRigid(atA, 0)
	transformB := NewTransformRigid(atB, 0)
	usedA, _ := cacheWorldAt(a, transformA, nil, worldA[:])
	usedB, _ := cacheWorldAt(b, transformB, nil, worldB[:])
	return collideWorld(a, transformA, worldA[:usedA], b, transformB, worldB[:usedB], m.Vec2d{})
}

// detectCoincident runs one tick of detection over two circles sharing a centre
// and reports the direction the seed parted them along.
func detectCoincident(t testing.TB, seed uint64) m.Vec2d {
	t.Helper()
	contacts := NewContacts(seed)
	bodies, statics := NewBodyIndex(0), NewStaticIndex(0)
	bodies.Insert(ecs.Entity(1), NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{X: 3, Y: 4}, 0, nil)
	bodies.Insert(ecs.Entity(2), NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{X: 3, Y: 4}, 0, nil)
	Collide(contacts, bodies, statics, 3)
	if contacts.Len() != 1 {
		t.Fatalf("two coincident circles made %d Contacts, want 1", contacts.Len())
	}
	return contacts.All()[0].Normal
}

func wantPhases(t testing.TB, contacts *Contacts, phases ...Phase) {
	t.Helper()
	list := contacts.All()
	if len(list) != len(phases) {
		t.Fatalf("the list holds %d entries, want %d", len(list), len(phases))
	}
	for i := range phases {
		if list[i].Phase != phases[i] {
			t.Errorf("entry %d is %v, want %v", i, list[i].Phase, phases[i])
		}
	}
}

func wantNormal(t testing.TB, got m.Vec2d, x, y float64) {
	t.Helper()
	if math.Abs(got.X-x) > oracleTolerance || math.Abs(got.Y-y) > oracleTolerance {
		t.Errorf("the normal is (%.17g, %.17g), want cp's (%.17g, %.17g)", got.X, got.Y, x, y)
	}
}

func wantPoint(t testing.TB, name string, got m.Vec2d, x, y float64) {
	t.Helper()
	if math.Abs(got.X-x) > oracleTolerance || math.Abs(got.Y-y) > oracleTolerance {
		t.Errorf("%s is (%.17g, %.17g), want (%.17g, %.17g)", name, got.X, got.Y, x, y)
	}
}

func wantNear(t testing.TB, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > oracleTolerance {
		t.Errorf("%s is %.17g, want %.17g", name, got, want)
	}
}

func near(got, want float64) bool { return math.Abs(got-want) <= oracleTolerance }
