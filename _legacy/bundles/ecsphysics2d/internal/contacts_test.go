package internal

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"

	"github.com/dvoyni/cog/libs/m"
)

// This file is the specification's layer B: an identical Body state, an
// identical Contact set, and a short horizon of solves compared against cp at
// 1e-9. Every cp number here came out of a throwaway harness importing
// github.com/jakecoffman/cp/v2 v2.4.0; main never gains the dependency.
//
// Two settings are made to agree rather than compared. cp's collisionSlop has a
// setter and is set to the port's 0.005 m — comparing defaults would measure a
// setting against a known pixel-sized number rather than an algorithm. cp's
// collisionBias has none and stays at pow(0.9, 60), so the port is run at the
// rate that reproduces it exactly: 1 − exp(−cpBias·h) is cp's own
// 1 − pow(0.9^60, h). Persistence needs nothing: the port's 0.05 s is cp's 3
// ticks at 60 Hz.
var cpBias = -60 * math.Log(0.9)

func layerB(t testing.TB) *harness {
	t.Helper()
	return newHarnessWith(t, Config{Bias: cpBias}, 64)
}

// A step of cp's trajectory: where cp's Body stood and how fast it was going
// when its Step returned.
type cpStep struct {
	x, y, angle     float64
	vx, vy, angular float64
}

// wantTrajectory compares the port's Body against cp's over the horizon.
//
// The velocities compare step for step. The positions are one tick apart in
// exactly one thing and nothing else: cp keeps the de-penetration bias on the
// Body and spends it at the top of the next step's position integration, where
// the port applies it as a position delta before Solve returns and discards it.
// Nothing moves the Body between those two moments, so cp's position after step
// k+1 is the port's after step k plus the step cp's own velocity then took —
// which is what is asserted here, and is why both engines detect at exactly the
// same positions.
func wantTrajectory(t *testing.T, h *harness, body ecs.Entity, steps []cpStep) {
	t.Helper()
	const tolerance = 1e-9
	for k := range steps {
		h.frame(t)
		got := h.read(t, body)
		want := steps[k]

		if math.Abs(got.Velocity.Linear.X-want.vx) > tolerance ||
			math.Abs(got.Velocity.Linear.Y-want.vy) > tolerance ||
			math.Abs(got.Velocity.Angular-want.angular) > tolerance {
			t.Errorf("step %d velocity is (%.17g, %.17g) w %.17g, want cp's (%.17g, %.17g) w %.17g",
				k, got.Velocity.Linear.X, got.Velocity.Linear.Y, got.Velocity.Angular,
				want.vx, want.vy, want.angular)
		}
		if math.Abs(got.Place.Angle-want.angle) > tolerance {
			t.Errorf("step %d angle is %.17g, want cp's %.17g", k, got.Place.Angle, want.angle)
		}
		if k+1 >= len(steps) {
			continue
		}
		wantX := steps[k+1].x - want.vx*tick
		wantY := steps[k+1].y - want.vy*tick
		if math.Abs(got.Place.Current.X-wantX) > tolerance ||
			math.Abs(got.Place.Current.Y-wantY) > tolerance {
			t.Errorf("step %d position is (%.17g, %.17g), want cp's (%.17g, %.17g)",
				k, got.Place.Current.X, got.Place.Current.Y, wantX, wantY)
		}
	}
}

func TestTwoPressedCirclesSolveExactlyAsChipmunkDoes(t *testing.T) {
	h := layerB(t)
	first := h.spawn(t, spawnRequest{
		Kind:     kindShapedBody,
		Place:    Position{Current: m.Vec2d{}},
		Velocity: Velocity{Linear: m.Vec2d{X: 1, Y: 0.3}, Angular: 0.2},
		Body:     dynamic(t, 2, 8, 0, 0),
		Shape:    circle(0.5),
	})
	second := h.spawn(t, spawnRequest{
		Kind:     kindShapedBody,
		Place:    Position{Current: m.Vec2d{X: 0.9}},
		Velocity: Velocity{Linear: m.Vec2d{X: -0.5, Y: 0.1}, Angular: -0.4},
		Body:     dynamic(t, 3, 9, 0, 0),
		Shape:    circle(0.5),
	})

	// The two circles are 0.9 m apart with a summed radius of 1 m, approaching.
	// cp's own trajectory over five steps.
	firstSteps := []cpStep{
		{0.016666666666666666, 0.0050000000000000001, 0.0033333333333333335, 0.10047019725790818, 0.30342678020092223, 0.20000000000000001},
		{0.011141603146862357, 0.010084539924519889, 0.0066666666666666671, 0.10047019725790818, 0.30342678020092223, 0.20000000000000001},
		{0.0063370376811616082, 0.015191379395785378, 0.01, 0.10047019725790818, 0.30342678020092223, 0.20000000000000001},
		{0.0021817803380863492, 0.020315499530450372, 0.013333333333333334, 0.10047019725790818, 0.30342678020092223, 0.20000000000000001},
		{-0.0013882667131076666, 0.025452708523559225, 0.016666666666666666, 0.10047019725790818, 0.30342678020092223, 0.20000000000000001},
	}
	secondSteps := []cpStep{
		{0.89166666666666672, 0.0016666666666666668, -0.0066666666666666671, 0.099686535161394474, 0.097715479866051835, -0.40000000000000002},
		{0.89812782012431402, 0.0032769733836534071, -0.013333333333333334, 0.099686535161394474, 0.097715479866051835, -0.40000000000000002},
		{0.90410864154589232, 0.0048724137361430804, -0.02, 0.099686535161394474, 0.097715479866051835, -0.40000000000000002},
		{0.90965659088572026, 0.0064563336463664169, -0.026666666666666668, 0.099686535161394474, 0.097715479866051835, -0.40000000000000002},
		{0.91481440003096071, 0.0080315276509605148, -0.033333333333333333, 0.099686535161394474, 0.097715479866051835, -0.40000000000000002},
	}

	// Both Bodies are walked over the same ticks, so the horizon is run once and
	// read twice.
	const tolerance = 1e-9
	for k := range firstSteps {
		h.frame(t)
		for _, pair := range []struct {
			name  string
			body  ecs.Entity
			steps []cpStep
		}{{"first", first, firstSteps}, {"second", second, secondSteps}} {
			got, want := h.read(t, pair.body), pair.steps[k]
			if math.Abs(got.Velocity.Linear.X-want.vx) > tolerance ||
				math.Abs(got.Velocity.Linear.Y-want.vy) > tolerance ||
				math.Abs(got.Velocity.Angular-want.angular) > tolerance {
				t.Errorf("step %d: the %s circle's velocity is (%.17g, %.17g) w %.17g, "+
					"want cp's (%.17g, %.17g) w %.17g", k, pair.name,
					got.Velocity.Linear.X, got.Velocity.Linear.Y, got.Velocity.Angular,
					want.vx, want.vy, want.angular)
			}
			if k+1 >= len(pair.steps) {
				continue
			}
			wantX := pair.steps[k+1].x - want.vx*tick
			wantY := pair.steps[k+1].y - want.vy*tick
			if math.Abs(got.Place.Current.X-wantX) > tolerance ||
				math.Abs(got.Place.Current.Y-wantY) > tolerance {
				t.Errorf("step %d: the %s circle is at (%.17g, %.17g), want cp's (%.17g, %.17g)",
					k, pair.name, got.Place.Current.X, got.Place.Current.Y, wantX, wantY)
			}
		}
	}
}

func TestACircleHeldAgainstAWallByAForceSolvesExactlyAsChipmunkDoes(t *testing.T) {
	// Four of the specification's seven scenes write their own gravity as m·g
	// into Force from an ordinary System, leaving Constants.Gravity at zero,
	// which is the same fall. This is that, and it is also the one place a
	// Force written this tick shows up as a velocity next tick.
	h := layerB(t)
	h.spawn(t, spawnRequest{
		Kind:  kindShapedStatic,
		Place: Position{Current: m.Vec2d{}},
		Shape: NewSegmentShape(m.Vec2d{X: -5}, m.Vec2d{X: 5}, 0),
	})
	body := h.spawn(t, spawnRequest{
		Kind:  kindShapedBody,
		Place: Position{Current: m.Vec2d{Y: 0.48}},
		Body:  dynamic(t, 2, 8, 0, 0),
		Shape: circle(0.5),
	})
	h.game.push = m.Vec2d{Y: -9.81 * 2}

	wantTrajectory(t, h, body, []cpStep{
		{0, 0.47999999999999998, 0, 0, 0, 0},
		{0, 0.48149999999999998, 0, 0, 0, 0},
		{0, 0.48285, 0, 0, 0, 0},
		{0, 0.48406500000000002, 0, 0, 0, 0},
		{0, 0.48515850000000005, 0, 0, 0, 0},
		{0, 0.48614265000000007, 0, 0, 0, 0},
		{0, 0.48702838500000006, 0, 0, 0, 0},
		{0, 0.48782554650000004, 0, 0, 0, 0},
	})
}

func TestABodyPressedIntoAWallRestsWithinTheSlopAndDoesNotBuzz(t *testing.T) {
	// The specification's restatement of nox's settle band, against the Slop
	// rather than against a measured 0.7 cm.
	h := newHarness(t)
	h.spawn(t, spawnRequest{
		Kind:  kindShapedStatic,
		Place: Position{Current: m.Vec2d{}},
		Shape: NewSegmentShape(m.Vec2d{X: -5}, m.Vec2d{X: 5}, 0),
	})
	body := h.spawn(t, spawnRequest{
		Kind:  kindShapedBody,
		Place: Position{Current: m.Vec2d{Y: 1.5}},
		Body:  dynamic(t, 2, 8, 0, 0),
		Shape: circle(0.5),
	})
	h.game.push = m.Vec2d{Y: -9.81 * 2}

	h.frames(t, 240)
	settled := h.read(t, body).Place.Current.Y
	if depth := 0.5 - settled; depth < 0 || depth > defaultSlop+1e-9 {
		t.Fatalf("the Body rests %v m into the wall, want between nothing and the Slop of %v",
			depth, defaultSlop)
	}

	// Buzzing is the thing eyes catch and an assertion has to name: the resting
	// height moving at all from one tick to the next once it has settled.
	for range 120 {
		h.frames(t, 1)
		now := h.read(t, body).Place.Current.Y
		if math.Abs(now-settled) > 1e-9 {
			t.Fatalf("the settled Body moved from %.17g to %.17g", settled, now)
		}
		settled = now
	}

	list := h.contacts(t)
	if len(list) != 1 || list[0].Phase != PhaseContinuing {
		t.Fatalf("a Body resting on a wall reports %d Contacts, want one Continuing", len(list))
	}
}

func TestAPileOfCirclesSettlesWithinTheSlopAndDoesNotBuzz(t *testing.T) {
	// The ticket's own acceptance: the step closes, and a pile dropped under
	// app-written gravity comes to rest. The columns stand 1 m apart and the
	// circles are 0.3 m, so each column is its own stack and the whole pile is
	// four of them — warm starting, the Slop and the Iterations together.
	h := newHarnessWith(t, nil, 64)
	h.spawn(t, spawnRequest{
		Kind:  kindShapedStatic,
		Place: Position{Current: m.Vec2d{}},
		Shape: NewSegmentShape(m.Vec2d{X: -5}, m.Vec2d{X: 5}, 0),
	})
	var pile []ecs.Entity
	for column := range 4 {
		for height := range 3 {
			pile = append(pile, h.spawn(t, spawnRequest{
				Kind: kindShapedBody,
				Place: Position{Current: m.Vec2d{
					X: -1.5 + float64(column),
					Y: 0.5 + 0.7*float64(height),
				}},
				Body:  dynamic(t, 1, 1, 0, 0),
				Shape: circle(0.3),
			}))
		}
	}
	h.game.push = m.Vec2d{Y: -9.81}

	h.frames(t, 900)
	settled := make([]m.Vec2d, len(pile))
	for i, e := range pile {
		settled[i] = h.read(t, e).Place.Current
	}

	for _, entry := range h.contacts(t) {
		if entry.Phase != PhaseContinuing {
			t.Errorf("a settled pile still has a %v Contact between %v and %v",
				entry.Phase, entry.A, entry.B)
		}
		for i := range int(entry.Count) {
			if depth := entry.Points[i].Depth; depth > defaultSlop+1e-6 {
				t.Errorf("%v rests %v m into %v, want no more than the Slop of %v",
					entry.A, depth, entry.B, defaultSlop)
			}
		}
	}

	for range 120 {
		h.frames(t, 1)
		for i, e := range pile {
			now := h.read(t, e).Place.Current
			if now.Sub(settled[i]).Length() > 1e-9 {
				t.Fatalf("the settled pile buzzes: %v moved from %v to %v", e, settled[i], now)
			}
			settled[i] = now
		}
	}
}

func TestSlidingAlongAWallIsTheVelocityLessItsNormalComponent(t *testing.T) {
	// At the shipped defaults — Friction 0, Restitution 0 — the normal impulse
	// cancels exactly the approaching normal velocity and the Coulomb clamp
	// leaves the tangent alone, which is v ← v − (v·n)·n. It is the solver's own
	// behaviour rather than a special rule, and it is the test that validates
	// the shipped defaults.
	h := newHarness(t)
	h.spawn(t, spawnRequest{
		Kind:  kindShapedStatic,
		Place: Position{Current: m.Vec2d{}},
		Shape: NewSegmentShape(m.Vec2d{X: -5}, m.Vec2d{X: 5}, 0),
	})
	body := h.spawn(t, spawnRequest{
		Kind:     kindShapedBody,
		Place:    Position{Current: m.Vec2d{Y: 0.5}},
		Velocity: Velocity{Linear: m.Vec2d{X: 3, Y: -2}},
		Body:     dynamic(t, 2, 8, 0, 0),
		Shape:    circle(0.5),
	})

	h.frames(t, 1)
	got := h.read(t, body).Velocity.Linear
	// The normal here is (0, 1), so v − (v·n)·n is (3, 0) exactly.
	if math.Abs(got.X-3) > 1e-9 || math.Abs(got.Y) > 1e-9 {
		t.Errorf("the slide left the velocity at %v, want (3, 0)", got)
	}
}

func TestASensorEntryIsReportedAndNeverSolved(t *testing.T) {
	h := newHarness(t)
	sensor := circle(0.5)
	sensor.Sensor = true
	h.spawn(t, spawnRequest{
		Kind:  kindShapedStatic,
		Place: Position{Current: m.Vec2d{}},
		Shape: sensor,
	})
	body := h.spawn(t, spawnRequest{
		Kind:  kindShapedBody,
		Place: Position{Current: m.Vec2d{X: 0.6}},
		Body:  dynamic(t, 2, 8, 0, 0),
		Shape: circle(0.5),
	})

	h.frames(t, 2)
	list := h.contacts(t)
	if len(list) != 1 || !list[0].Sensor {
		t.Fatalf("a Sensor overlapping a Body gave %d Contacts, want one marked a Sensor's", len(list))
	}
	if list[0].Points[0].NormalImpulse != 0 {
		t.Errorf("the Sensor entry was solved: it carries an Impulse of %v", list[0].Points[0].NormalImpulse)
	}
	if got := h.read(t, body).Velocity.Linear; got != (m.Vec2d{}) {
		t.Errorf("the Sensor pushed the Body to %v; a Sensor pushes nothing", got)
	}
}

func TestAPairWithNoMassBetweenItIsReportedAndNeverSolved(t *testing.T) {
	// cp excludes a pair whose two Bodies both have infinite mass by comparing
	// against INFINITY; the port classifies by Component presence, which is what
	// the exclusion becomes — and it is exactly the k_scalar = 0 guard, so there
	// is no residual NaN case for Contacts. The pair is still reported, because
	// the groups decide what collides and the Body kinds do not overrule them.
	//
	// The immovable party here is a Kinematic Body: a Velocity and no Dynamic,
	// carrying a Shape, so it is in the Body index rather than the static one
	// and Static against Static never arises.
	h := newHarness(t)
	still := h.spawn(t, spawnRequest{
		Kind:  kindShapedStatic,
		Place: Position{Current: m.Vec2d{}},
		Shape: circle(0.5),
	})
	// A shaped Body with no Dynamic at all, spawned through the Kinematic set
	// and then given a Shape, which is the only way the harness reaches one.
	kinematic := h.spawn(t, spawnRequest{
		Kind:  kindKinematic,
		Place: Position{Current: m.Vec2d{X: 0.6}},
	})
	h.setShape(t, kinematic, circle(0.5))

	h.frames(t, 2)
	list := h.contacts(t)
	if len(list) != 1 {
		t.Fatalf("a Kinematic Body overlapping a Static one gave %d Contacts, want 1", len(list))
	}
	if list[0].Other(kinematic) != still {
		t.Fatalf("the entry names %v and %v, want the Kinematic Body and the Static one",
			list[0].A, list[0].B)
	}
	if list[0].Points[0].NormalImpulse != 0 {
		t.Errorf("a pair with no mass between it was solved: it carries %v",
			list[0].Points[0].NormalImpulse)
	}
	if list[0].Phase != PhaseContinuing {
		t.Errorf("an excluded pair that keeps touching is %v, want Continuing", list[0].Phase)
	}
	if got := h.read(t, kinematic); got.Velocity.Linear != (m.Vec2d{}) ||
		got.Place.Current.X != 0.6 {
		t.Errorf("the immovable pair moved something: %+v", got)
	}

	// A Dynamic Body in the same overlap is solved, which is what says the
	// exclusion is the mass and not the overlap.
	body := h.spawn(t, spawnRequest{
		Kind:     kindShapedBody,
		Place:    Position{Current: m.Vec2d{X: -0.6}},
		Velocity: Velocity{Linear: m.Vec2d{X: 1}},
		Body:     dynamic(t, 2, 8, 0, 0),
		Shape:    circle(0.5),
	})
	h.frames(t, 2)
	if got := h.read(t, body).Velocity.Linear.X; got != 0 {
		t.Errorf("the Dynamic Body's approach was not cancelled: its velocity is %v", got)
	}
	if got := h.read(t, body).Place.Current.X; got >= -0.6 {
		t.Errorf("the Dynamic Body at %v was not pushed back out of the Static Shape", got)
	}
}

func TestADroppedContinuingEntryEndsAndItsImpulsesAreZeroed(t *testing.T) {
	h := newHarness(t)
	h.spawn(t, spawnRequest{
		Kind:  kindShapedStatic,
		Place: Position{Current: m.Vec2d{}},
		Shape: NewSegmentShape(m.Vec2d{X: -5}, m.Vec2d{X: 5}, 0),
	})
	body := h.spawn(t, spawnRequest{
		Kind:  kindShapedBody,
		Place: Position{Current: m.Vec2d{Y: 0.48}},
		Body:  dynamic(t, 2, 8, 0, 0),
		Shape: circle(0.5),
	})
	h.game.push = m.Vec2d{Y: -9.81 * 2}

	h.frames(t, 3)
	held := h.contacts(t)
	if len(held) != 1 || held[0].Phase != PhaseContinuing ||
		held[0].Points[0].NormalImpulse == 0 {
		t.Fatalf("the Contact holding the Body up is %+v", held)
	}

	h.game.filter = func(entry *Contact) { entry.Drop() }
	h.frames(t, 1)
	dropped := h.contacts(t)
	if len(dropped) != 1 {
		t.Fatalf("dropping the entry changed the list to %d entries; nothing is deleted or moved", len(dropped))
	}
	if dropped[0].Phase != PhaseEnded {
		t.Errorf("a dropped Continuing entry is %v, want Ended so that reacting Systems see the end",
			dropped[0].Phase)
	}
	if !dropped[0].Dropped() {
		t.Error("the entry does not report that it was dropped")
	}
	if dropped[0].Points[0].NormalImpulse != 0 {
		t.Errorf("a dropped tick carries an Impulse of %v; a tick with no solution has the solution 0",
			dropped[0].Points[0].NormalImpulse)
	}
	if got := h.read(t, body).Velocity.Linear.Y; !(got < 0) {
		t.Errorf("the dropped Contact still held the Body up: its velocity is %v", got)
	}

	// The next tick compares against what survived, and nothing did.
	h.game.filter = nil
	h.frames(t, 1)
	again := h.contacts(t)
	if len(again) != 1 || again[0].Phase != PhaseBegan {
		t.Fatalf("after the drop the pair came back as %+v, want one Began entry", again)
	}
	if again[0].Points[0].NormalImpulse == 0 {
		t.Error("the Contact that came back never solved")
	}
}

func TestAnIgnoredPairStaysIgnoredUntilItComesApart(t *testing.T) {
	// cp's arb.Ignore, which one-way platforms need: PreSolve ignores the pair
	// when the normal points the wrong way, and without it a Body halfway
	// through is shoved back out the moment its normal flips.
	h := newHarness(t)
	h.spawn(t, spawnRequest{
		Kind:  kindShapedStatic,
		Place: Position{Current: m.Vec2d{}},
		Shape: NewSegmentShape(m.Vec2d{X: -5}, m.Vec2d{X: 5}, 0),
	})
	body := h.spawn(t, spawnRequest{
		Kind:  kindShapedBody,
		Place: Position{Current: m.Vec2d{Y: 0.48}},
		Body:  dynamic(t, 2, 8, 0, 0),
		Shape: circle(0.5),
	})
	h.game.push = m.Vec2d{Y: -9.81 * 2}

	// The filter marks once and is then taken away; the mark is what crosses the
	// ticks, not the filter.
	h.game.filter = func(entry *Contact) { entry.Ignore() }
	h.frames(t, 1)
	h.game.filter = nil

	// The mark keeps arriving for as long as the two keep touching, which a
	// circle of radius 0.5 does for the half metre it takes to pass the wall.
	//
	// The pair Continues while it does. The two marks differ in exactly this: a
	// drop is for one tick and the filter re-decides, so a dropped pair begins
	// again, but an ignore runs until the pair comes apart and there is nothing
	// to re-decide — which is cp's IGNORE state persisting and cp not calling
	// Begin a second time.
	for range 20 {
		h.frames(t, 1)
		list := h.contacts(t)
		if len(list) != 1 || !list[0].Ignored() {
			t.Fatalf("the pair arrived as %+v, want one entry already marked ignored", list)
		}
		if list[0].Phase != PhaseContinuing {
			t.Fatalf("an ignored pair that never stopped touching is %v, want Continuing", list[0].Phase)
		}
		if list[0].Points[0].NormalImpulse != 0 {
			t.Errorf("an ignored pair was solved: it carries %v", list[0].Points[0].NormalImpulse)
		}
	}

	// Nothing held the Body up, so it fell through the wall, which is the whole
	// point of the mark. It is in free fall one tick behind the analytic form,
	// because the Force an app writes this tick moves the Body next tick.
	h.frames(t, 40)
	fallen := h.read(t, body).Place.Current.Y
	if fallen > -0.5 {
		t.Fatalf("the ignored Body is at %v; nothing should have held it", fallen)
	}

	// The ignore ends when the pair misses one tick, so the Body coming back up
	// is stopped by the wall like anything else.
	if list := h.contacts(t); len(list) != 0 {
		t.Fatalf("the pair that came apart still reports %+v", list)
	}
	h.game.push = m.Vec2d{}
	h.place(t, body, m.Vec2d{Y: 0.48})
	h.frames(t, 2)
	again := h.contacts(t)
	if len(again) != 1 || again[0].Ignored() {
		t.Fatalf("the pair came back as %+v, want one entry no longer ignored", again)
	}
}

func TestAKinematicBodyPushesADynamicOneAndIsNotPushedBack(t *testing.T) {
	// The gather's row for a Kinematic Body has no inverse mass and a real
	// velocity, which is the whole of what makes it a wall that moves. cp says
	// the same thing by giving it an infinite mass; the port says it by the Body
	// having no Dynamic.
	h := newHarness(t)
	pusher := h.spawn(t, spawnRequest{
		Kind:     kindKinematic,
		Place:    Position{Current: m.Vec2d{}},
		Velocity: Velocity{Linear: m.Vec2d{X: 2}},
	})
	h.setShape(t, pusher, circle(0.5))
	pushed := h.spawn(t, spawnRequest{
		Kind:  kindShapedBody,
		Place: Position{Current: m.Vec2d{X: 0.9}},
		Body:  dynamic(t, 2, 8, 0, 0),
		Shape: circle(0.5),
	})

	h.frames(t, 2)
	if got := h.read(t, pusher).Velocity.Linear; got != (m.Vec2d{X: 2}) {
		t.Errorf("the Kinematic Body was pushed to %v; nothing pushes one", got)
	}
	if got := h.read(t, pushed).Velocity.Linear.X; got < 1.9 || got > 2.1 {
		t.Errorf("the Dynamic Body came away at %v m/s, want about the pusher's 2", got)
	}
}

func TestTwoExactlyCoincidentBodiesLeaveEveryComponentFinite(t *testing.T) {
	// The port's one claim over both cp and Chipmunk: no input produces a NaN or
	// an infinity in a Component the plugin writes. Coincident centres are the
	// degenerate input this ticket adds — cp's own answer is a fixed (1, 0)
	// there — so the whole step is run over them and every number checked.
	h := newHarness(t)
	var pair []ecs.Entity
	for range 2 {
		pair = append(pair, h.spawn(t, spawnRequest{
			Kind:  kindShapedBody,
			Place: Position{Current: m.Vec2d{X: 2, Y: -3}},
			Body:  dynamic(t, 2, 8, 0, 0),
			Shape: circle(0.5),
		}))
	}

	h.frames(t, 300)
	for _, e := range pair {
		got := h.read(t, e)
		for name, value := range map[string]float64{
			"Current.X": got.Place.Current.X, "Current.Y": got.Place.Current.Y,
			"Previous.X": got.Place.Previous.X, "Previous.Y": got.Place.Previous.Y,
			"Angle": got.Place.Angle, "PreviousAngle": got.Place.PreviousAngle,
			"Linear.X": got.Velocity.Linear.X, "Linear.Y": got.Velocity.Linear.Y,
			"Angular": got.Velocity.Angular,
			"Force.X": got.Force.Force.X, "Force.Y": got.Force.Force.Y,
			"Torque": got.Force.Torque,
		} {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				t.Errorf("%v's %s is %v", e, name, value)
			}
		}
	}

	// The two were parted rather than left on top of each other, which is what
	// the seeded nudge is for and what cp's fixed direction never guarantees.
	first := h.read(t, pair[0]).Place.Current
	second := h.read(t, pair[1]).Place.Current
	if apart := first.Sub(second).Length(); apart < 1-defaultSlop-1e-6 {
		t.Errorf("the two coincident Bodies are %v m apart, want their summed radius less the Slop", apart)
	}
}

func TestAnEndedEntryMayNameADespawnedEntity(t *testing.T) {
	// An Ended entry carries no reason — separation, a despawned party, a
	// removed Shape and a filter's drop all read the same — so a System finds
	// out by failing to look the Entity up in a Store it already reads. This
	// departs from cp, whose Count() returns 0 once an arbiter is CACHED.
	h := newHarness(t)
	first := h.spawn(t, spawnRequest{
		Kind:  kindShapedBody,
		Place: Position{Current: m.Vec2d{}},
		Body:  dynamic(t, 2, 8, 0, 0),
		Shape: circle(0.5),
	})
	second := h.spawn(t, spawnRequest{
		Kind:  kindShapedBody,
		Place: Position{Current: m.Vec2d{X: 0.9}},
		Body:  dynamic(t, 2, 8, 0, 0),
		Shape: circle(0.5),
	})

	h.frames(t, 1)
	if got := h.contacts(t); len(got) != 1 || got[0].Phase != PhaseBegan {
		t.Fatalf("the two circles gave %+v, want one Began entry", got)
	}

	h.despawn(t, second)
	h.frames(t, 1)
	list := h.contacts(t)
	if len(list) != 1 || list[0].Phase != PhaseEnded {
		t.Fatalf("despawning one party gave %+v, want one Ended entry", list)
	}
	if list[0].Other(first) != second {
		t.Errorf("the Ended entry names %v as the other party, want the despawned %v",
			list[0].Other(first), second)
	}
	if list[0].Count == 0 {
		t.Error("the Ended entry lost its points; it keeps the geometry of the tick it last touched")
	}
}

// TestACircleSpawnedConcentricWithABoxIsPartedAlongTheTicksSeededDirection is the
// end of the route the seed takes, through a real engine rather than a call into
// the narrowphase: Detect draws one direction a tick, hands it to collideWorld as
// coincident, and collideWorld hands it to the GJK arm that needs it.
//
// Two Shapes spawned on the same point exactly is the placement gjk's cold-start
// axis is nothing at, and before the seed reached the arms an app got a Contact
// there with a zero Normal and a zero Depth — a pair reported as touching with
// nothing to separate it, which the solver spends nothing on and which therefore
// fails without a symptom. The circle sits inside the box by a whole metre of
// summed half-extent and radius, and the Depth is that, whichever way the tick
// parted them.
//
// The seed is the whole of what chooses between the four: a 1 m box concentric
// with a 0.5 m circle is symmetric about its centre, so up, down, left and right
// are equally shallow and none is the right answer. cp answers (0, 1) every time,
// which is the symmetry that never breaks and what the specification rejects. The
// seeds below part it more than one way, which is what asserts the difference.
func TestACircleSpawnedConcentricWithABoxIsPartedAlongTheTicksSeededDirection(t *testing.T) {
	// The specification's 1e-9, as everywhere else in this file.
	const tolerance = 1e-9

	parted := map[m.Vec2d]uint64{}
	for _, seed := range []uint64{1, 2, 3, 4} {
		h := newHarnessWith(t, Config{Seed: seed}, 64)
		h.spawn(t, spawnRequest{
			Kind:  kindShapedStatic,
			Place: Position{Current: m.Vec2d{}},
			Shape: NewBoxShape(1, 1, 0),
		})
		h.spawn(t, spawnRequest{
			Kind:  kindShapedBody,
			Place: Position{Current: m.Vec2d{}},
			Shape: circle(0.5),
		})

		h.frames(t, 2)
		list := h.contacts(t)
		if len(list) != 1 {
			t.Fatalf("a circle concentric with a box gave %d Contacts at seed %d, want one",
				len(list), seed)
		}
		got := list[0]
		if math.Abs(got.Normal.Length()-1) > tolerance {
			t.Fatalf("the Contact's Normal at seed %d is %v, whose length is %v and not one — "+
				"the pair is reported as touching with nothing to separate it",
				seed, got.Normal, got.Normal.Length())
		}
		if got.Count == 0 {
			t.Fatalf("the Contact at seed %d carries no points at all", seed)
		}
		deepest := got.Points[0].Depth
		for i := range int(got.Count) {
			if got.Points[i].Depth > deepest {
				deepest = got.Points[i].Depth
			}
		}
		if math.Abs(deepest-1) > tolerance {
			t.Errorf("the Contact's deepest point at seed %d is %v, want the whole metre the "+
				"two overlap by", seed, deepest)
		}
		parted[m.Vec2d{
			X: math.Round(got.Normal.X * 1e9),
			Y: math.Round(got.Normal.Y * 1e9),
		}] = seed
	}

	if len(parted) < 2 {
		t.Errorf("every seed parted the pair the same way, which is a symmetry that never "+
			"breaks — cp's own fixed (0, 1) here, and what the specification rejects; the "+
			"directions drawn were %v", parted)
	}
}
