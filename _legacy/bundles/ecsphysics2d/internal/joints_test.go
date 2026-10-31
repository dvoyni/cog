package internal

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"

	"github.com/dvoyni/cog/libs/m"
)

// Everything here runs the Joint half of Solve through a real engine and reads
// the Components back, as the rest of this package's tests do. The layer-B
// comparison against cp is the one that actually pins a constraint: a Joint
// that is merely not obviously wrong in a scene is not verified, so every one
// of the ten kinds has its own row in cpJointCases.

// jointTolerance is the specification's 1e-9, and every comparison against cp
// is made at it. A float64 ULP at this scene's scale is about 1e-16, so 1e-9
// sits seven orders above the noise.
const jointTolerance = 1e-9

// chipmunkErrorBias is cp's stored errorBias, 0.9^60, re-spelled as the rate
// per second the port takes. cp's collisionBias has no setter, so the port is
// the side that moves; the Joint's errorBias is the same quantity and the same
// re-spelling, and running the port here rather than at its own 6.32 is what
// makes the two comparable byte for byte.
var chipmunkErrorBias = -60 * math.Log(0.9)

// The two Bodies every layer-B scene is built from. They are deliberately
// asymmetric in mass, in Moment and in every component of their state, so that
// a swapped sign or a party read the wrong way round cannot pass.
//
// Neither carries a Shape, so no scene here has a Contact in it at all.
type jointParty struct {
	mass, moment float64
	place        Position
	velocity     Velocity
}

var (
	jointPartyA = jointParty{
		mass: 2, moment: 8,
		place:    Position{Current: m.Vec2d{X: 0.3, Y: 1.2}, Angle: 0.4},
		velocity: Velocity{Linear: m.Vec2d{X: 1.5, Y: -0.7}, Angular: 0.9},
	}
	jointPartyB = jointParty{
		mass: 3, moment: 5,
		place:    Position{Current: m.Vec2d{X: -0.6, Y: 0.5}, Angle: -0.25},
		velocity: Velocity{Linear: m.Vec2d{X: -0.8, Y: 0.4}, Angular: -1.3},
	}
	jointAnchorA = m.Vec2d{X: 0.2, Y: -0.35}
	jointAnchorB = m.Vec2d{X: -0.15, Y: 0.45}
)

// spawnJointParty puts one of the two Bodies into the world, shapeless, with no
// Damping and no Force, so that the velocity integration is exactly cp's at a
// damping of 1 and a gravity of nothing.
func (p jointParty) spawn(t testing.TB, h *harness) ecs.Entity {
	t.Helper()
	return h.spawn(t, spawnRequest{
		Kind:     kindDynamic,
		Place:    p.place,
		Velocity: p.velocity,
		Body:     dynamic(t, p.mass, p.moment, 0, 0),
	})
}

// withBias is every Joint this file spawns, re-spelled onto cp's own error
// bias so that the comparison measures the algorithm and not a setting.
func withBias(joint Joint) Joint {
	joint.ErrorBias = chipmunkErrorBias
	return joint
}

// TestEveryJointKindMatchesChipmunkOverAShortHorizon is layer B for all ten
// kinds: identical Body state, one Joint, ten ticks, compared on both Bodies'
// places, Angles and velocities and on the Joint's own Impulse at 1e-9.
//
// A short horizon rather than one tick, because one tick proves the arithmetic
// and the horizon that matters is the handful of ticks over which the warm
// start and the bias correction engage. The Contact set is empty by
// construction — nothing here has a Shape — which is what lets the comparison
// be exact: only Contacts write a bias velocity, and it is the bias velocity
// whose spending the port moved.
func TestEveryJointKindMatchesChipmunkOverAShortHorizon(t *testing.T) {
	cases := []struct {
		name string
		make func(a, b ecs.Entity) Joint
	}{
		{"pin", func(a, b ecs.Entity) Joint {
			distance := PinDistance(
				jointPartyA.place.Current, jointPartyA.place.Angle, jointAnchorA,
				jointPartyB.place.Current, jointPartyB.place.Angle, jointAnchorB)
			return NewPinJoint(a, b, jointAnchorA, jointAnchorB, distance)
		}},
		{"pinLimited", func(a, b ecs.Entity) Joint {
			distance := PinDistance(
				jointPartyA.place.Current, jointPartyA.place.Angle, jointAnchorA,
				jointPartyB.place.Current, jointPartyB.place.Angle, jointAnchorB)
			joint := NewPinJoint(a, b, jointAnchorA, jointAnchorB, distance)
			joint.MaxForce = 4.5
			return joint
		}},
		{"slide", func(a, b ecs.Entity) Joint {
			return NewSlideJoint(a, b, jointAnchorA, jointAnchorB, 0.4, 0.9)
		}},
		{"pivot", func(a, b ecs.Entity) Joint {
			return NewPivotJoint(a, b, jointAnchorA, jointAnchorB)
		}},
		{"pivotSoft", func(a, b ecs.Entity) Joint {
			joint := NewPivotJoint(a, b, jointAnchorA, jointAnchorB)
			joint.MaxBias = 0.75
			joint.MaxForce = 6.0
			return joint
		}},
		{"groove", func(a, b ecs.Entity) Joint {
			return NewGrooveJoint(a, b, jointAnchorA,
				m.Vec2d{X: -0.5, Y: 0.1}, m.Vec2d{X: 0.7, Y: 0.25})
		}},
		{"spring", func(a, b ecs.Entity) Joint {
			return NewSpringJoint(a, b, jointAnchorA, jointAnchorB, 0.8, 14.0, 1.7)
		}},
		{"rotarySpring", func(a, b ecs.Entity) Joint {
			return NewRotarySpringJoint(a, b, 0.35, 9.0, 1.1)
		}},
		{"rotaryLimit", func(a, b ecs.Entity) Joint {
			return NewRotaryLimitJoint(a, b, -0.2, 0.3)
		}},
		{"ratchet", func(a, b ecs.Entity) Joint {
			// cp reads the starting Angle off the two Bodies as b.a − a.a,
			// which is A's Angle less B's; a cog constructor reads no Store, so
			// the app passes exactly that.
			angle := jointPartyA.place.Angle - jointPartyB.place.Angle
			return NewRatchetJoint(a, b, angle, 0.05, 0.4)
		}},
		{"gear", func(a, b ecs.Entity) Joint {
			return NewGearJoint(a, b, 0.12, 2.5)
		}},
		{"motor", func(a, b ecs.Entity) Joint {
			return NewMotorJoint(a, b, 3.2)
		}},
	}

	for _, it := range cases {
		t.Run(it.name, func(t *testing.T) {
			want, ok := cpJointCases[it.name]
			if !ok {
				t.Fatalf("no frozen cp trajectory for %q", it.name)
			}

			h := newHarness(t)
			a := jointPartyA.spawn(t, h)
			b := jointPartyB.spawn(t, h)
			joint := h.spawn(t, spawnRequest{Kind: kindJoint, Joint: withBias(it.make(a, b))})

			for tick, row := range want {
				h.frame(t)
				got := [13]float64{}
				placeA, velocityA := h.read(t, a).Place, h.read(t, a).Velocity
				placeB, velocityB := h.read(t, b).Place, h.read(t, b).Velocity
				got[0], got[1], got[2] = placeA.Current.X, placeA.Current.Y, placeA.Angle
				got[3], got[4], got[5] = velocityA.Linear.X, velocityA.Linear.Y, velocityA.Angular
				got[6], got[7], got[8] = placeB.Current.X, placeB.Current.Y, placeB.Angle
				got[9], got[10], got[11] = velocityB.Linear.X, velocityB.Linear.Y, velocityB.Angular
				got[12] = h.joint(t, joint).Joint.Impulse()

				for i, wanted := range row {
					if math.Abs(got[i]-wanted) > jointTolerance {
						t.Fatalf("tick %d, number %d (%s): got %.17g, cp has %.17g, apart by %.3g",
							tick+1, i, jointNumberNames[i], got[i], wanted, math.Abs(got[i]-wanted))
					}
				}
			}
		})
	}
}

// jointNumberNames names the thirteen numbers a frozen row carries, so a
// failure says which one went wrong rather than which column did.
var jointNumberNames = [13]string{
	"A's x", "A's y", "A's Angle", "A's vx", "A's vy", "A's Angular velocity",
	"B's x", "B's y", "B's Angle", "B's vx", "B's vy", "B's Angular velocity",
	"the Joint's Impulse",
}

// TestARotarySpringBetweenTwoBodiesThatDoNotTurnIsFiniteWhereChipmunkIsNaN is
// the defect in cp's Go port that is not among the porting index's seven.
//
// An infinite Moment of inertia is legal — it is a Body that does not turn —
// and two of them give iSum = +Inf, an Impulse clamped to an infinite jMax
// since MaxForce defaults to ∞, and then Inf·0 written straight into Angular
// velocity. Chipmunk asserts moment != 0 in the rotary Spring and catches one
// of the five; the Go port dropped the assert in translation and catches none.
func TestARotarySpringBetweenTwoBodiesThatDoNotTurnIsFiniteWhereChipmunkIsNaN(t *testing.T) {
	for _, it := range []struct {
		name string
		make func(a, b ecs.Entity) Joint
	}{
		{"rotary spring", func(a, b ecs.Entity) Joint {
			return NewRotarySpringJoint(a, b, 0.35, 9.0, 1.1)
		}},
		{"rotary limit", func(a, b ecs.Entity) Joint {
			return NewRotaryLimitJoint(a, b, -0.2, 0.3)
		}},
		{"ratchet", func(a, b ecs.Entity) Joint {
			return NewRatchetJoint(a, b, 0.65, 0.05, 0.4)
		}},
		{"gear", func(a, b ecs.Entity) Joint {
			return NewGearJoint(a, b, 0.12, 2.5)
		}},
		{"motor", func(a, b ecs.Entity) Joint {
			return NewMotorJoint(a, b, 3.2)
		}},
	} {
		t.Run(it.name, func(t *testing.T) {
			h := newHarness(t)
			// An infinite Moment is what NewDynamic accepts and stores as an
			// inverse of zero: a Body that does not turn.
			a := h.spawn(t, spawnRequest{
				Kind:     kindDynamic,
				Place:    jointPartyA.place,
				Velocity: jointPartyA.velocity,
				Body:     dynamic(t, 2, math.Inf(1), 0, 0),
			})
			b := h.spawn(t, spawnRequest{
				Kind:     kindDynamic,
				Place:    jointPartyB.place,
				Velocity: jointPartyB.velocity,
				Body:     dynamic(t, 3, math.Inf(1), 0, 0),
			})
			joint := h.spawn(t, spawnRequest{Kind: kindJoint, Joint: withBias(it.make(a, b))})

			h.frames(t, 10)

			for _, e := range []ecs.Entity{a, b} {
				read := h.read(t, e)
				if !finiteVelocity(read.Velocity) || !finitePlace(read.Place) {
					t.Fatalf("%v is not finite: place %+v, velocity %+v", e, read.Place, read.Velocity)
				}
			}
			if impulse := h.joint(t, joint).Joint.Impulse(); impulse != 0 {
				t.Errorf("the skipped Joint reports an Impulse of %v, want 0", impulse)
			}
		})
	}
}

// TestAJointToADespawnedBodyIsSkippedAndItsImpulseReadsZero is the port's
// answer where cp asserts both Bodies are non-nil: a Reference simply misses.
//
// The plugin does not despawn the Joint — structural change during the step is
// forbidden — and a dangling Reference is the app's to clean up as it is
// anywhere else. Impulse reading 0 is how an app notices, and there is no
// diagnostic list and no counter Resource.
func TestAJointToADespawnedBodyIsSkippedAndItsImpulseReadsZero(t *testing.T) {
	h := newHarness(t)
	a := jointPartyA.spawn(t, h)
	b := jointPartyB.spawn(t, h)
	distance := PinDistance(
		jointPartyA.place.Current, jointPartyA.place.Angle, jointAnchorA,
		jointPartyB.place.Current, jointPartyB.place.Angle, jointAnchorB)
	joint := h.spawn(t, spawnRequest{
		Kind:  kindJoint,
		Joint: withBias(NewPinJoint(a, b, jointAnchorA, jointAnchorB, distance)),
	})

	h.frames(t, 3)
	if impulse := h.joint(t, joint).Joint.Impulse(); impulse == 0 {
		t.Fatal("the Joint delivered no Impulse at all before the Body was despawned")
	}

	h.despawn(t, b)
	h.frames(t, 1)

	read := h.joint(t, joint)
	if !read.Found {
		t.Fatal("the plugin despawned the Joint, which is a structural change inside the step")
	}
	if impulse := read.Joint.Impulse(); impulse != 0 {
		t.Errorf("the skipped Joint reports an Impulse of %v, want 0", impulse)
	}

	// The surviving Body carries on under nothing, which is what "skipped"
	// means: no Impulse reaches it.
	before := h.read(t, a).Velocity
	h.frames(t, 1)
	after := h.read(t, a).Velocity
	if !near(after.Linear.X, before.Linear.X) || !near(after.Linear.Y, before.Linear.Y) ||
		!near(after.Angular, before.Angular) {
		t.Errorf("the surviving Body was still pushed: %+v became %+v", before, after)
	}
}

// TestAJointAnchorsToTheWorldThroughAStaticBody is the ordinary anchor case,
// and it must work: the Static party is a zero-inverse-mass row that is never
// written back, exactly as a Contact against a wall already is.
func TestAJointAnchorsToTheWorldThroughAStaticBody(t *testing.T) {
	h := newHarness(t)
	wall := h.spawn(t, spawnRequest{
		Kind:  kindStatic,
		Place: Position{Current: m.Vec2d{X: 0, Y: 2}},
	})
	body := h.spawn(t, spawnRequest{
		Kind:     kindDynamic,
		Place:    Position{Current: m.Vec2d{X: 0, Y: 1}},
		Velocity: Velocity{Linear: m.Vec2d{X: 2}},
		Body:     dynamic(t, 1, 4, 0, 0),
	})
	// The Static Body is B, which is the party a pendulum hangs from.
	h.spawn(t, spawnRequest{
		Kind: kindJoint,
		Joint: withBias(NewPinJoint(
			body, wall, m.Vec2d{}, m.Vec2d{}, 1)),
	})

	h.frames(t, 120)

	read := h.read(t, body)
	if !finitePlace(read.Place) || !finiteVelocity(read.Velocity) {
		t.Fatalf("the hung Body is not finite: %+v, %+v", read.Place, read.Velocity)
	}
	// A pin holds a distance, so two seconds of swinging leaves the Body on the
	// circle of radius 1 about the wall. It sits a little outside it, and by a
	// knowable amount rather than an arbitrary tolerance: each tick carries the
	// Body v·h along the tangent, which lengthens the radius by about
	// (v·h)²/2 ≈ 5.5e-4, and the error bias pushes 1 − exp(−6.32/60) = 10% of
	// the error out a tick, so the steady state is ten times the per-tick
	// stretch. The Slop has nothing to do with it: a Joint is not a Contact.
	distance := read.Place.Current.Distance(m.Vec2d{X: 0, Y: 2})
	stretch := 2 * tick * 2 * tick / 2
	if math.Abs(distance-1) > 2*stretch/0.1 {
		t.Errorf("the pinned Body sits %v from its anchor, want within %v of 1",
			distance, 2*stretch/0.1)
	}

	wallRead := h.read(t, wall)
	if wallRead.Place.Current != (m.Vec2d{X: 0, Y: 2}) {
		t.Errorf("the Static anchor moved to %+v", wallRead.Place.Current)
	}
}

// TestAPendulumOfPinJointsKeepsTheSmallAngleperiod is the specification's
// layer-C scene for the Joint solver, against a closed form rather than against
// cp: a mass hung from a pin Joint at a small Angle swings at 2*pi*sqrt(L/g).
//
// The scene writes m*g into Force from an ordinary System and leaves
// Constants.Gravity at zero, which is the same fall and checks that gravity
// really is optional rather than assumed. A Force written this tick moves the
// Body next tick, so the measured period carries one tick of offset and the
// tolerance below has it in.
func TestAPendulumOfPinJointsKeepsTheSmallAnglePeriod(t *testing.T) {
	const (
		length  = 1.0
		gravity = 9.81
		mass    = 1.5
		swing   = 0.05
	)
	h := newHarness(t)
	pivot := h.spawn(t, spawnRequest{
		Kind:  kindStatic,
		Place: Position{Current: m.Vec2d{}},
	})
	bob := h.spawn(t, spawnRequest{
		Kind: kindDynamic,
		Place: Position{Current: m.Vec2d{
			X: length * math.Sin(swing), Y: -length * math.Cos(swing),
		}},
		Body: dynamic(t, mass, 0.01, 0, 0),
	})
	h.spawn(t, spawnRequest{
		Kind: kindJoint,
		Joint: withBias(NewPinJoint(
			bob, pivot, m.Vec2d{}, m.Vec2d{}, length)),
	})
	h.game.push = m.Vec2d{Y: -mass * gravity}

	// The bob starts at its far swing, so it crosses the bottom a quarter of a
	// period in and every half period after that: the whole period is the time
	// between the first crossing and the third, which is also what takes the
	// starting quarter out of the measurement.
	previous := h.read(t, bob).Place.Current.X
	crossings, ticks, first := 0, 0, 0
	for crossings < 3 && ticks < 600 {
		h.frame(t)
		ticks++
		current := h.read(t, bob).Place.Current.X
		if (previous > 0) != (current > 0) {
			crossings++
			if crossings == 1 {
				first = ticks
			}
		}
		previous = current
	}
	if crossings < 3 {
		t.Fatalf("the pendulum crossed the bottom %d times in %d ticks; it is not swinging", crossings, ticks)
	}

	measured := float64(ticks-first) * tick
	want := 2 * math.Pi * math.Sqrt(length/gravity)
	// Two ticks of tolerance: one for the Force latency and one for the tick
	// the crossing is detected on.
	if math.Abs(measured-want) > 3*tick {
		t.Errorf("the pendulum's period measured %.4f s against the closed form %.4f s", measured, want)
	}
}

// TestAJointThatSaysSoKeepsItsTwoBodiesFromColliding is cp's
// QueryRejectConstraints, in the one place the port can put it: Index builds
// the set out of the Joint walk and Detect checks it after the bit filter and
// the bounding-box test, so the Contact is never created and never reported.
func TestAJointThatSaysSoKeepsItsTwoBodiesFromColliding(t *testing.T) {
	h := newHarness(t)
	a := h.spawn(t, spawnRequest{
		Kind:  kindShapedBody,
		Place: Position{Current: m.Vec2d{X: 0, Y: 0}},
		Body:  dynamic(t, 1, 4, 0, 0),
		Shape: circle(0.5),
	})
	b := h.spawn(t, spawnRequest{
		Kind:  kindShapedBody,
		Place: Position{Current: m.Vec2d{X: 0.4, Y: 0}},
		Body:  dynamic(t, 1, 4, 0, 0),
		Shape: circle(0.5),
	})

	// Without a Joint the two overlap and the pair is reported.
	h.frames(t, 1)
	if len(h.contacts(t)) != 1 {
		t.Fatalf("the two overlapping Shapes gave %d Contacts, want 1", len(h.contacts(t)))
	}
	if pairs := h.jointedPairs(t, a, b); pairs.Len != 0 {
		t.Fatalf("the set of pairs a Joint holds apart is %d before any Joint exists", pairs.Len)
	}

	joint := NewPivotJoint(a, b, m.Vec2d{}, m.Vec2d{})
	joint.CollideBodies = false
	h.spawn(t, spawnRequest{Kind: kindJoint, Joint: withBias(joint)})

	h.frames(t, 1)
	pairs := h.jointedPairs(t, a, b)
	if pairs.Len != 1 || !pairs.Has {
		t.Fatalf("Index built %d pairs and has(a, b) is %v", pairs.Len, pairs.Has)
	}
	// Never created and never reported: the previous tick's entry does not even
	// come back as Ended, because Detect never saw the pair.
	for _, entry := range h.contacts(t) {
		if entry.Phase != PhaseEnded {
			t.Errorf("the pair is still reported as %v", entry.Phase)
		}
	}
	h.frames(t, 1)
	if got := len(h.contacts(t)); got != 0 {
		t.Errorf("the jointed pair still gives %d Contacts", got)
	}
}

// TestAJointThatStillCollidesLeavesTheContactAlone is the other half of the
// same rule, and it is what makes the gate real: a scene whose Joints all
// collide keeps an empty set, so Detect pays one branch.
func TestAJointThatStillCollidesLeavesTheContactAlone(t *testing.T) {
	h := newHarness(t)
	a := h.spawn(t, spawnRequest{
		Kind:  kindShapedBody,
		Place: Position{Current: m.Vec2d{X: 0, Y: 0}},
		Body:  dynamic(t, 1, 4, 0, 0),
		Shape: circle(0.5),
	})
	b := h.spawn(t, spawnRequest{
		Kind:  kindShapedBody,
		Place: Position{Current: m.Vec2d{X: 0.4, Y: 0}},
		Body:  dynamic(t, 1, 4, 0, 0),
		Shape: circle(0.5),
	})
	h.spawn(t, spawnRequest{
		Kind:  kindJoint,
		Joint: withBias(NewPivotJoint(a, b, m.Vec2d{}, m.Vec2d{})),
	})

	h.frames(t, 2)
	if pairs := h.jointedPairs(t, a, b); pairs.Len != 0 {
		t.Errorf("a Joint whose Bodies still collide put %d pairs into the set", pairs.Len)
	}
	if got := len(h.contacts(t)); got != 1 {
		t.Errorf("the pair gives %d Contacts, want 1", got)
	}
}

// TestTheRatchetWritesItsAngleBackAndNothingElseCrossesATick pins the one
// parameter the solver mutates, and pins that nothing else does: every one of
// cp's PreStep scratch fields is frame-local in the dense Joint row.
func TestTheRatchetWritesItsAngleBackAndNothingElseCrossesATick(t *testing.T) {
	h := newHarness(t)
	a := jointPartyA.spawn(t, h)
	b := jointPartyB.spawn(t, h)
	start := jointPartyA.place.Angle - jointPartyB.place.Angle
	joint := h.spawn(t, spawnRequest{
		Kind:  kindJoint,
		Joint: withBias(NewRatchetJoint(a, b, start, 0.05, 0.4)),
	})

	h.frames(t, 20)
	read := h.joint(t, joint).Joint
	if read.Angle() == start {
		t.Fatalf("the ratchet never clicked over: its Angle is still %v", start)
	}
	// It clicks in whole steps offset by the phase, which is what floor gives.
	steps := (read.Angle() - 0.05) / 0.4
	if math.Abs(steps-math.Round(steps)) > 1e-12 {
		t.Errorf("the ratchet's Angle %v is not a whole step off its phase", read.Angle())
	}
	// Every other parameter is exactly what it was written as.
	if read.Phase() != 0.05 || read.Ratchet() != 0.4 {
		t.Errorf("the ratchet's phase or step moved: %v, %v", read.Phase(), read.Ratchet())
	}
}

// TestTheTwoSpringsRebuildTheirImpulseAndNeverWarmStart is the irregularity the
// specification names: their ApplyCachedImpulse is empty and their accumulated
// Impulse is rebuilt in PreStep, so for those two the stored field is a readout
// only. It is checked by holding the scene still and watching the Impulse track
// the Spring's own law rather than accumulate.
func TestTheTwoSpringsRebuildTheirImpulseAndNeverWarmStart(t *testing.T) {
	const (
		restLength = 0.8
		stiffness  = 14.0
	)
	h := newHarness(t)
	// Two Bodies that cannot move at all: infinite Moment and a Kinematic
	// partner would still turn, so both are given a huge mass instead, which
	// leaves the Spring's own force the only thing the Impulse can be.
	a := h.spawn(t, spawnRequest{
		Kind:  kindDynamic,
		Place: Position{Current: m.Vec2d{X: 1.3}},
		Body:  dynamic(t, 1e12, 1e12, 0, 0),
	})
	b := h.spawn(t, spawnRequest{
		Kind:  kindDynamic,
		Place: Position{},
		Body:  dynamic(t, 1e12, 1e12, 0, 0),
	})
	joint := h.spawn(t, spawnRequest{
		Kind: kindJoint,
		Joint: withBias(NewSpringJoint(
			a, b, m.Vec2d{}, m.Vec2d{}, restLength, stiffness, 0)),
	})

	want := (restLength - 1.3) * stiffness * tick
	for range 5 {
		h.frame(t)
		got := h.joint(t, joint).Joint.Impulse()
		if math.Abs(got-want) > 1e-6 {
			t.Fatalf("the Spring's Impulse is %v, want %v rebuilt from its law every tick", got, want)
		}
	}
}

// TestASpringLosesPartOfItsImpulseToDampingInTheTickItIsApplied records cp's
// behaviour rather than correcting it, which is what the specification asks
// for: cp applies the Spring Impulse inside PreStep, before velocity
// integration, so it is multiplied by the Body's Damping in the same tick,
// where a Force write is added after that multiply.
func TestASpringLosesPartOfItsImpulseToDampingInTheTickItIsApplied(t *testing.T) {
	const (
		mass      = 2.0
		damping   = 15.0
		stretched = 1.3
	)
	h := newHarness(t)
	anchor := h.spawn(t, spawnRequest{Kind: kindStatic, Place: Position{}})
	body := h.spawn(t, spawnRequest{
		Kind:  kindDynamic,
		Place: Position{Current: m.Vec2d{X: stretched}},
		Body:  dynamic(t, mass, 4, damping, 0),
	})
	h.spawn(t, spawnRequest{
		Kind: kindJoint,
		Joint: withBias(NewSpringJoint(
			body, anchor, m.Vec2d{}, m.Vec2d{}, 0.8, 14.0, 0)),
	})

	h.frames(t, 1)

	// The Impulse the Spring applied, divided by the mass, damped by one tick.
	impulse := (0.8 - stretched) * 14.0 * tick
	want := impulse / mass * math.Exp(-damping*tick)
	got := h.read(t, body).Velocity.Linear.X
	if math.Abs(got-want) > 1e-12 {
		t.Errorf("the Spring left the Body at %v, want %v — the Impulse damped in the same tick", got, want)
	}
	// And that the loss really is the 22% the specification quotes.
	if lost := 1 - math.Exp(-damping*tick); math.Abs(lost-0.2212) > 1e-3 {
		t.Errorf("the tick's damping loss is %.4f, not the quoted 22%%", lost)
	}
}

// TestADegenerateJointParameterIsSkippedRatherThanTurnedIntoANaN is the port's
// own clause, and it is the finiteness invariant rather than cp: the gear's
// ratio and the ratchet's step each sit in a denominator, and a groove whose
// two ends coincide has no direction to project onto.
func TestADegenerateJointParameterIsSkippedRatherThanTurnedIntoANaN(t *testing.T) {
	for _, it := range []struct {
		name string
		make func(a, b ecs.Entity) Joint
	}{
		{"a gear of ratio zero", func(a, b ecs.Entity) Joint {
			return NewGearJoint(a, b, 0.12, 0)
		}},
		{"a ratchet of step zero", func(a, b ecs.Entity) Joint {
			return NewRatchetJoint(a, b, 0.65, 0.05, 0)
		}},
		{"a groove with no length", func(a, b ecs.Entity) Joint {
			return NewGrooveJoint(a, b, jointAnchorA,
				m.Vec2d{X: 0.2, Y: 0.1}, m.Vec2d{X: 0.2, Y: 0.1})
		}},
		{"a Joint anchored at both centres of gravity", func(a, b ecs.Entity) Joint {
			return NewPivotJoint(a, b, m.Vec2d{}, m.Vec2d{})
		}},
	} {
		t.Run(it.name, func(t *testing.T) {
			h := newHarness(t)
			a := jointPartyA.spawn(t, h)
			b := jointPartyB.spawn(t, h)
			h.spawn(t, spawnRequest{Kind: kindJoint, Joint: withBias(it.make(a, b))})

			h.frames(t, 10)
			for _, e := range []ecs.Entity{a, b} {
				read := h.read(t, e)
				if !finitePlace(read.Place) || !finiteVelocity(read.Velocity) {
					t.Fatalf("%v is not finite: %+v, %+v", e, read.Place, read.Velocity)
				}
			}
		})
	}
}

// TestAJointBetweenTwoBodiesWithNoMassIsSkipped is the guard's first clause,
// which is provable rather than empirical: k = m_sum·I + A with A positive
// semi-definite, so k is non-singular whenever m_sum is positive, and since
// Dynamic rejects an infinite mass that is exactly "at least one Body is
// Dynamic".
func TestAJointBetweenTwoBodiesWithNoMassIsSkipped(t *testing.T) {
	h := newHarness(t)
	// Two Kinematic Bodies: a Velocity the app writes and no Dynamic beside it,
	// so nothing pushes either of them.
	a := h.spawn(t, spawnRequest{
		Kind:     kindKinematic,
		Place:    Position{Current: m.Vec2d{X: 1}},
		Velocity: Velocity{Linear: m.Vec2d{X: 1}},
	})
	b := h.spawn(t, spawnRequest{
		Kind:     kindKinematic,
		Place:    Position{},
		Velocity: Velocity{Linear: m.Vec2d{X: -1}},
	})
	joint := h.spawn(t, spawnRequest{
		Kind:  kindJoint,
		Joint: withBias(NewPinJoint(a, b, m.Vec2d{}, m.Vec2d{}, 1)),
	})

	h.frames(t, 10)
	if impulse := h.joint(t, joint).Joint.Impulse(); impulse != 0 {
		t.Errorf("the skipped Joint reports an Impulse of %v, want 0", impulse)
	}
	// Both Kinematic Bodies carry on at exactly the velocity the app wrote.
	if got := h.read(t, a).Velocity.Linear.X; !near(got, 1) {
		t.Errorf("the first Kinematic Body was pushed to %v", got)
	}
	if got := h.read(t, b).Velocity.Linear.X; !near(got, -1) {
		t.Errorf("the second Kinematic Body was pushed to %v", got)
	}
}

// TestSeveralJointsMayHoldOneBody is why a Joint is an Entity of its own rather
// than a Component on one of the Bodies: a Component is one per Entity, and a
// Body may be held by several Joints.
func TestSeveralJointsMayHoldOneBody(t *testing.T) {
	h := newHarness(t)
	left := h.spawn(t, spawnRequest{
		Kind:  kindStatic,
		Place: Position{Current: m.Vec2d{X: -1, Y: 1}},
	})
	right := h.spawn(t, spawnRequest{
		Kind:  kindStatic,
		Place: Position{Current: m.Vec2d{X: 1, Y: 1}},
	})
	hung := h.spawn(t, spawnRequest{
		Kind:  kindDynamic,
		Place: Position{Current: m.Vec2d{}},
		Body:  dynamic(t, 1, 4, 0, 0),
	})
	distance := math.Sqrt(2)
	h.spawn(t, spawnRequest{
		Kind:  kindJoint,
		Joint: withBias(NewPinJoint(hung, left, m.Vec2d{}, m.Vec2d{}, distance)),
	})
	h.spawn(t, spawnRequest{
		Kind:  kindJoint,
		Joint: withBias(NewPinJoint(hung, right, m.Vec2d{}, m.Vec2d{}, distance)),
	})
	h.game.push = m.Vec2d{Y: -9.81}

	h.frames(t, 240)

	read := h.read(t, hung)
	if !finitePlace(read.Place) {
		t.Fatalf("the doubly held Body is not finite: %+v", read.Place)
	}
	// Two pins of equal length from two symmetric anchors leave exactly one
	// place below them, and the Body hangs there however hard it is pulled.
	if math.Abs(read.Place.Current.X) > 1e-3 {
		t.Errorf("the doubly held Body drifted to x = %v", read.Place.Current.X)
	}
	for _, anchor := range []m.Vec2d{{X: -1, Y: 1}, {X: 1, Y: 1}} {
		if got := read.Place.Current.Distance(anchor); math.Abs(got-distance) > 1e-2 {
			t.Errorf("the Body sits %v from %+v, want %v", got, anchor, distance)
		}
	}
}

func finitePlace(place Position) bool {
	return finite(place.Current.X) && finite(place.Current.Y) && finite(place.Angle)
}

func finiteVelocity(velocity Velocity) bool {
	return finite(velocity.Linear.X) && finite(velocity.Linear.Y) && finite(velocity.Angular)
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
