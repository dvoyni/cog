package internal

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

// A fast Kinematic body carries what it hits (continuous-collision.md § Solve:
// back to T). A Kinematic body is never stopped and never pushed, so a Dynamic
// body it meets on its path is carried along with it from the moment they
// meet: Previous + T·d_self + (1 − T)·d_other, where the Kinematic side's own
// path is d_other.

// paddle spawns a Kinematic body of that Shape at a place, moving at a
// velocity. Its Shape StopsAtBodies, since what a paddle is thrown at here is
// Bodies, which a fast Body meets only with the flag set.
func paddle(t testing.TB, h *harness, shape ecsphysics2d.Shape, at, velocity m.Vec2d) ecs.Entity {
	t.Helper()
	shape.StopsAtBodies = true
	return h.spawn(t, spawnRequest{
		Kind:     kindShapedKinematic,
		Place:    ecsphysics2d.Position{Current: at},
		Velocity: ecsphysics2d.Velocity{Linear: velocity},
		Shape:    shape,
	})
}

// carryShapes are the paddle and ball Shapes the carry is thrown with: two
// circles of radius 1, whose Probe is exact, and two boxes 2 m square, Probed
// by the swept convex test.
var carryShapes = []struct {
	name      string
	shape     ecsphysics2d.Shape
	tolerance float64
}{
	{"balls", ecsphysics2d.NewCircleShape(1, m.Vec2d{}), 1e-9},
	{"boxes", ecsphysics2d.NewBoxShape(2, 2, 0), 1e-6},
}

// carryCase is a Dynamic ball D and a Kinematic paddle K, and where each must
// stand at the end of the one tick, relative to the scene's starting point.
type carryCase struct {
	fromD, velD m.Vec2d
	fromK, velK m.Vec2d
	t           float64
	atD, atK    m.Vec2d
}

// checkCarry runs one case from eight starting points, each shape, and both
// orders of spawning, since the lower Entity judges a meeting: the pair is one
// stopping Contact at T, K stands where the tick left it, and D stands where
// K carried it.
func checkCarry(t *testing.T, c carryCase) {
	t.Helper()
	for _, shape := range carryShapes {
		for _, paddleFirst := range []bool{false, true} {
			order := "ball first"
			if paddleFirst {
				order = "paddle first"
			}
			t.Run(shape.name+", "+order, func(t *testing.T) {
				for phase := range phases {
					h := newHarness(t)
					shift := m.Vec2d{X: 10 * float64(phase) / phases, Y: 3.7 * float64(phase) / phases}
					var d, k ecs.Entity
					if paddleFirst {
						k = paddle(t, h, shape.shape, c.fromK.Add(shift), c.velK)
						d = thrown(t, h, shape.shape, c.fromD.Add(shift), c.velD)
					} else {
						d = thrown(t, h, shape.shape, c.fromD.Add(shift), c.velD)
						k = paddle(t, h, shape.shape, c.fromK.Add(shift), c.velK)
					}
					h.frame(t)

					entries := meetingOf(h.contacts(t), d, k)
					if len(entries) != 1 {
						t.Fatalf("phase %d: the pair is %d entries, want one", phase, len(entries))
					}
					if entry := entries[0]; entry.Sensor || math.Abs(entry.T-c.t) > shape.tolerance {
						t.Errorf("phase %d: the pair is not a stop at T %.3f: Sensor %v, T %.9f",
							phase, c.t, entry.Sensor, entry.T)
					}
					for _, side := range []struct {
						name string
						e    ecs.Entity
						want m.Vec2d
					}{{"the ball", d, c.atD.Add(shift)}, {"the paddle", k, c.atK.Add(shift)}} {
						if at := h.read(t, side.e).Place.Current; at.Distance(side.want) > shape.tolerance {
							t.Errorf("phase %d: %s stands at %v, want %v", phase, side.name, at, side.want)
						}
					}
				}
			})
		}
	}
}

// The paddle case, fast ball: D goes x 0 → 10 and K 10 → 0, ten metres a
// tick each. They meet at T = 0.4, D at 4 and K at 6. K is never stopped, so
// it ends at 0, and D is carried by K's remaining movement: 4 + 0.6·(−10) = −2,
// touching K from the side it came from. Moved back to T alone, D would stand
// at 4, on K's far side.
func TestAFastKinematicPaddleCarriesAFastBall(t *testing.T) {
	checkCarry(t, carryCase{
		fromD: m.Vec2d{}, velD: m.Vec2d{X: meetSpeed},
		fromK: m.Vec2d{X: 10}, velK: m.Vec2d{X: -meetSpeed},
		t:   0.4,
		atD: m.Vec2d{X: -2}, atK: m.Vec2d{},
	})
}

// The paddle case, slow ball: D rests at x = 5, and K goes 10 → 0. D did not
// move, so it is not marked, and K's own path meets it at T = 0.3, where K
// stands at 7. D is carried from its end pose by (1 − T)·d_K = 0.7·(−10), to
// −2, touching K at 0: hit, not passed through.
func TestAFastKinematicPaddleCarriesABallAtRest(t *testing.T) {
	checkCarry(t, carryCase{
		fromD: m.Vec2d{X: 5},
		fromK: m.Vec2d{X: 10}, velK: m.Vec2d{X: -meetSpeed},
		t:   0.3,
		atD: m.Vec2d{X: -2}, atK: m.Vec2d{},
	})
}

// A Kinematic body against a Static one moves neither side: the Kinematic
// body is never stopped, and a Static one never moves. The stopping Contact is
// only reported.
func TestAFastKinematicBodyAgainstAStaticOneMovesNeither(t *testing.T) {
	for phase := range phases {
		h := newHarness(t)
		shift := m.Vec2d{X: 10 * float64(phase) / phases, Y: 3.7 * float64(phase) / phases}
		wall := h.spawn(t, spawnRequest{
			Kind:  kindShapedStatic,
			Place: ecsphysics2d.Position{Current: m.Vec2d{X: 5}.Add(shift)},
			Shape: wallShape(),
		})
		k := paddle(t, h, ecsphysics2d.NewCircleShape(1, m.Vec2d{}), shift, m.Vec2d{X: meetSpeed})
		h.frame(t)

		stop, found := between(h.contacts(t), k, wall)
		if !found || !(stop.T < 1) {
			t.Fatalf("phase %d: the wall reports no stopping Contact with the paddle", phase)
		}
		if at, want := h.read(t, k).Place.Current, (m.Vec2d{X: 10}).Add(shift); at.Distance(want) > 1e-9 {
			t.Errorf("phase %d: the paddle stands at %v, want where the tick left it, %v", phase, at, want)
		}
		if at, want := h.read(t, wall).Place.Current, (m.Vec2d{X: 5}).Add(shift); at != want {
			t.Errorf("phase %d: the wall stands at %v, want %v", phase, at, want)
		}
	}
}

// Pinned limit: a Kinematic target closing on the Body. Each gate sees its own
// Body's motion, not the pair's, so a ball and a Kinematic box each moving just
// under its own gate, closing on each other at nearly twice what either gate
// allows, are marked by neither: nothing stops, and the pair is caught only by
// the discrete walk. Closing by less than both extents together, the ball is
// always first seen overlapping the box from the near side, so the walk holds
// it there, and the box pushes it along. This is today's behaviour, held so
// that a change to the gate or to the walk that loses it is seen.
func TestPinnedAKinematicTargetClosingOnTheBody(t *testing.T) {
	ball := ecsphysics2d.NewCircleShape(0.2, m.Vec2d{})
	box := ecsphysics2d.NewBoxShape(0.4, 0.4, 0)
	speed := 0.99 * 0.2 / tick
	for phase := range phases {
		h := newHarness(t)
		start := -1 - speed*tick*float64(phase)/phases
		d := thrown(t, h, ball, m.Vec2d{X: start}, m.Vec2d{X: speed})
		k := paddle(t, h, box, m.Vec2d{X: 1}, m.Vec2d{X: -speed})
		for i := range 12 {
			h.frame(t)
			for _, entry := range meetingOf(h.contacts(t), d, k) {
				if entry.T < 1 {
					t.Fatalf("phase %d, tick %d: the pair was stopped at T %.6f, want it left to the discrete walk",
						phase, i, entry.T)
				}
			}
			if at, by := h.read(t, d).Place.Current.X, h.read(t, k).Place.Current.X; at >= by {
				t.Fatalf("phase %d, tick %d: the ball stands at x = %.3f, past the box at %.3f",
					phase, i, at, by)
			}
		}
	}
}

// A fast Kinematic body carries every Dynamic body on its path, not just the
// first (continuous-collision.md § Detect: one path pass, § Solve: back to
// T). Detect cannot tell a Kinematic mover from a Dynamic one, so it keeps
// every Hit of a fast solid Body's path, and Solve, which reads Dynamic,
// decides: a Dynamic mover stops at its first Hit and the rest stop nothing,
// and a Kinematic mover carries each Dynamic body it meets by (1 − T)·d_K.

// carriedBall is one ball a paddle's path meets, where it rests, the T the
// paddle meets it at, and where the carry leaves it.
type carriedBall struct {
	from, vel m.Vec2d
	t         float64
	at        m.Vec2d
}

// checkCarriedAll runs the scene from eight starting points, each Shape, and
// both orders of spawning: extra spawns whatever else the scene holds, the
// paddle goes x 10 → 0, and every ball is one stopping Contact with it at its
// own T and stands where the paddle carried it. check, when there is one,
// asserts whatever else the tick's list must hold.
func checkCarriedAll(
	t *testing.T, balls []carriedBall, extra func(*harness, m.Vec2d),
	check func(t *testing.T, phase int, list []ecsphysics2d.Contact, k ecs.Entity, tolerance float64),
) {
	t.Helper()
	for _, shape := range carryShapes {
		for _, paddleFirst := range []bool{false, true} {
			order := "balls first"
			if paddleFirst {
				order = "paddle first"
			}
			t.Run(shape.name+", "+order, func(t *testing.T) {
				for phase := range phases {
					h := newHarness(t)
					shift := m.Vec2d{X: 10 * float64(phase) / phases, Y: 3.7 * float64(phase) / phases}
					if extra != nil {
						extra(h, shift)
					}
					var k ecs.Entity
					spawnPaddle := func() {
						k = paddle(t, h, shape.shape, m.Vec2d{X: 10}.Add(shift), m.Vec2d{X: -meetSpeed})
					}
					if paddleFirst {
						spawnPaddle()
					}
					ds := make([]ecs.Entity, len(balls))
					for i, ball := range balls {
						ds[i] = thrown(t, h, shape.shape, ball.from.Add(shift), ball.vel)
					}
					if !paddleFirst {
						spawnPaddle()
					}
					h.frame(t)

					list := h.contacts(t)
					for i, ball := range balls {
						entries := meetingOf(list, ds[i], k)
						if len(entries) != 1 {
							t.Fatalf("phase %d: the ball from %v is %d entries with the paddle, want one",
								phase, ball.from, len(entries))
						}
						entry := entries[0]
						if entry.Sensor || entry.Count != 1 || entry.Points[0].Depth != 0 ||
							math.Abs(entry.T-ball.t) > shape.tolerance {
							t.Errorf("phase %d: the ball from %v is not a stop at T %.3f: "+
								"Sensor %v, %d point(s), Depth %.6f, T %.9f",
								phase, ball.from, ball.t, entry.Sensor, entry.Count, entry.Points[0].Depth, entry.T)
						}
						if at, want := h.read(t, ds[i]).Place.Current, ball.at.Add(shift); at.Distance(want) > shape.tolerance {
							t.Errorf("phase %d: the ball from %v stands at %v, want %v", phase, ball.from, at, want)
						}
					}
					if at, want := h.read(t, k).Place.Current, shift; at.Distance(want) > shape.tolerance {
						t.Errorf("phase %d: the paddle stands at %v, want %v", phase, at, want)
					}
					if check != nil {
						check(t, phase, list, k, shape.tolerance)
					}
				}
			})
		}
	}
}

// Balls of radius 1 rest at x = 7 and x = 3, and the Kinematic paddle goes
// 10 → 0. It meets the first at T = 0.1, standing at 9, and carries it by
// 0.9·(−10) to −2; it meets the second at T = 0.5, standing at 5, and carries
// it by 0.5·(−10) to −2 as well. Keeping only the first Hit, the paddle and
// the carried ball passed straight through the second.
func TestAFastKinematicPaddleCarriesEveryBallOnItsPath(t *testing.T) {
	checkCarriedAll(t, []carriedBall{
		{from: m.Vec2d{X: 7}, t: 0.1, at: m.Vec2d{X: -2}},
		{from: m.Vec2d{X: 3}, t: 0.5, at: m.Vec2d{X: -2}},
	}, nil, nil)
}

// A Static wall at x = 7 is the paddle's first Hit, which moves neither side,
// and the ball resting at x = 3 behind it is carried all the same: met at
// T = 0.5, carried to −2.
func TestAFastKinematicPaddleCarriesABallBehindAWall(t *testing.T) {
	checkCarriedAll(t, []carriedBall{
		{from: m.Vec2d{X: 3}, t: 0.5, at: m.Vec2d{X: -2}},
	}, func(h *harness, shift m.Vec2d) {
		h.spawn(t, spawnRequest{
			Kind:  kindShapedStatic,
			Place: ecsphysics2d.Position{Current: m.Vec2d{X: 7}.Add(shift)},
			Shape: wallShape(),
		})
	}, nil)
}

// A meeting that comes sooner than the paddle's first Hit on its own path
// does not hide that Hit. A fast ball goes x 4 → 14 and meets the paddle
// along their relative motion at T = 0.2, where it stands at 6, and is
// carried by 0.8·(−10) to −2. A ball resting at x = 1.5 is the paddle's own
// first Hit, at T = 0.65, and is carried by 0.35·(−10) to −2.
func TestAFastKinematicPaddleCarriesABallBehindOneItMet(t *testing.T) {
	checkCarriedAll(t, []carriedBall{
		{from: m.Vec2d{X: 4}, vel: m.Vec2d{X: meetSpeed}, t: 0.2, at: m.Vec2d{X: -2}},
		{from: m.Vec2d{X: 1.5}, t: 0.65, at: m.Vec2d{X: -2}},
	}, nil, nil)
}

// A Dynamic mover is unchanged: a fast Dynamic ball going 10 → 0 with balls
// resting at x = 7 and x = 3 stops at the first, at T = 0.1 and x = 9. The
// second is past where it stopped, so no Contact names it, neither in the
// list a filter System reads nor in the one a reacting System reads, and it
// has not moved.
func TestAFastDynamicBallStopsAtTheFirstOfTwoTargets(t *testing.T) {
	for _, shape := range carryShapes {
		t.Run(shape.name, func(t *testing.T) {
			for phase := range phases {
				h := newHarness(t)
				shift := m.Vec2d{X: 10 * float64(phase) / phases, Y: 3.7 * float64(phase) / phases}
				mover := thrown(t, h, shape.shape, m.Vec2d{X: 10}.Add(shift), m.Vec2d{X: -meetSpeed})
				first := thrown(t, h, shape.shape, m.Vec2d{X: 7}.Add(shift), m.Vec2d{})
				second := thrown(t, h, shape.shape, m.Vec2d{X: 3}.Add(shift), m.Vec2d{})
				filtered := false
				h.game.filter = func(entry *ecsphysics2d.Contact) {
					if entry.Other(second) == mover {
						filtered = true
					}
				}
				h.frame(t)
				if filtered {
					t.Errorf("phase %d: a filter System saw a Contact with the second target", phase)
				}

				list := h.contacts(t)
				if stop, found := between(list, mover, first); !found || math.Abs(stop.T-0.1) > shape.tolerance {
					t.Errorf("phase %d: the first target is no stop at T 0.1: found %v, T %.9f", phase, found, stop.T)
				}
				if entry, found := between(list, mover, second); found {
					t.Errorf("phase %d: the second target, past the stop, reports a Contact at T %.6f", phase, entry.T)
				}
				if at, want := h.read(t, mover).Place.Current, (m.Vec2d{X: 9}).Add(shift); at.Distance(want) > shape.tolerance {
					t.Errorf("phase %d: the mover stands at %v, want where it stopped, %v", phase, at, want)
				}
				if at, want := h.read(t, second).Place.Current, (m.Vec2d{X: 3}).Add(shift); at != want {
					t.Errorf("phase %d: the second target stands at %v, want %v", phase, at, want)
				}
			}
		})
	}
}

// Balls asleep on the paddle's path are carried too, and woken: the sleep
// System wakes a sleeper a Kinematic mover's path meets past its first Hit,
// as it wakes one the first Hit names, so each ball carried to −2 is solved
// with the paddle and goes on in front of it the next tick instead of being
// passed through. The two balls were carried to one place, so the next tick
// also pushes them apart, and one may lean a little into the paddle: in front
// of it is its centre short of the paddle's front face.
func TestAFastKinematicPaddleCarriesAndWakesSleepingBalls(t *testing.T) {
	h, _ := newSleepHarness(t, m.Vec2d{}, ecsphysics2d.Sleep{IdleSpeed: 0.1, Time: napTime})
	ball := ecsphysics2d.NewCircleShape(1, m.Vec2d{})
	balls := []ecs.Entity{
		thrown(t, h, ball, m.Vec2d{X: 7}, m.Vec2d{}),
		thrown(t, h, ball, m.Vec2d{X: 3}, m.Vec2d{}),
	}
	settle(t, h, balls, 200)
	k := paddle(t, h, ball, m.Vec2d{X: 10}, m.Vec2d{X: -meetSpeed})
	h.frame(t)
	for _, d := range balls {
		if at := h.read(t, d).Place.Current; at.Distance(m.Vec2d{X: -2}) > 1e-9 {
			t.Errorf("the ball %v stands at %v, want carried to x = -2", d, at)
		}
		if h.asleep(t, d) {
			t.Errorf("the ball %v the paddle carried still sleeps", d)
		}
	}
	h.frame(t)
	by := h.read(t, k).Place.Current.X
	for _, d := range balls {
		if at := h.read(t, d).Place.Current.X; at > by-1 {
			t.Errorf("a tick later the ball %v stands at x = %.6f, not in front of the paddle at %.6f", d, at, by)
		}
	}
}

// A carried pair past the first Hit that touched on the tick before is one
// entry that Continues, not an Ended one beside a new one: Detect held the
// Hit, so it did not find the pair again and closed it Ended, and the Contact
// Solve writes for the carry takes the Ended entry's place. The paddle
// carries both balls on the first tick; before the second, both are set
// down at rest on its path again, at x = −3 and −7, and the paddle, going
// 0 → −10, meets them at T = 0.1 and 0.5.
func TestACarriedPairThatTouchedTheTickBeforeContinues(t *testing.T) {
	h, _ := newSleepHarness(t, m.Vec2d{}, ecsphysics2d.Sleep{})
	ball := ecsphysics2d.NewCircleShape(1, m.Vec2d{})
	k := paddle(t, h, ball, m.Vec2d{X: 10}, m.Vec2d{X: -meetSpeed})
	balls := []ecs.Entity{
		thrown(t, h, ball, m.Vec2d{X: 7}, m.Vec2d{}),
		thrown(t, h, ball, m.Vec2d{X: 3}, m.Vec2d{}),
	}
	h.frame(t)
	for i, x := range []float64{-3, -7} {
		h.setVelocity(t, balls[i], ecsphysics2d.Velocity{})
		at := m.Vec2d{X: x}
		h.kernel.ExecuteCommand[placeCmd](placeRequest{Entity: balls[i], Place: ecsphysics2d.Position{Current: at, Previous: at}})
	}
	h.frame(t)

	list := h.contacts(t)
	for i, want := range []float64{0.1, 0.5} {
		entries := meetingOf(list, balls[i], k)
		if len(entries) != 1 {
			t.Fatalf("ball %d is %d entries with the paddle, want one", i, len(entries))
		}
		if entry := entries[0]; entry.Phase != ecsphysics2d.PhaseContinuing || math.Abs(entry.T-want) > 1e-9 {
			t.Errorf("ball %d's entry is phase %v at T %.9f, want Continuing at T %.3f", i, entry.Phase, entry.T, want)
		}
		if at := h.read(t, balls[i]).Place.Current; at.Distance(m.Vec2d{X: -12}) > 1e-9 {
			t.Errorf("ball %d stands at %v, want carried to x = -12", i, at)
		}
	}
}

// resetter is an app that sets Bodies back where they started before every
// tick, so a scene repeats the same tick for as long as it runs.
type resetter struct{ resets []reset }

type reset struct {
	e        ecs.Entity
	at       m.Vec2d
	velocity m.Vec2d
}

type resetOnUpdate kernel.Subscription[app.UpdateEvent]

func (*resetter) Name() kernel.PluginName { return "physicstestresetter" }

func (*resetter) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, ecsphysics2d.Name}
}

func (r *resetter) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[resetOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, func(
		places *ecs.Set[ecsphysics2d.Position], velocities *ecs.Set[ecsphysics2d.Velocity],
	) {
		for _, each := range r.resets {
			if place, ok := places.Ref(each.e); ok {
				*place = ecsphysics2d.Position{Current: each.at, Previous: each.at}
			}
			if velocity, ok := velocities.Ref(each.e); ok {
				*velocity = ecsphysics2d.Velocity{Linear: each.velocity}
			}
		}
	})).Before[ecsphysics2d.IntegrateOnUpdate]()
	return nil
}

// TestTheCarriesSitOnTheEnginesAllocationLine is the step's allocation claim
// over a scene where fast Kinematic paddles carry balls past their first Hit
// every tick: lanes of a paddle going 10 → 0 over balls resting at 7, 3 and
// −1 and a Sensor resting at 5, all set back before each tick. Each pair
// touched the tick before, so every carried Contact and every Sensor crossed
// past the first Hit also takes an Ended entry's place. It measures the held
// Hits and crossings, the sleep System's reading of them, and Solve's carry
// and writing, none of which the other scenes on the line reach.
func TestTheCarriesSitOnTheEnginesAllocationLine(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	const ticks = 4_000

	measure := func(n int) (float64, int, int) {
		r := &resetter{}
		h := newHarnessWithPlugins(t, nil, uint32(8*max(n, 1)), r)
		ball := ecsphysics2d.NewCircleShape(1, m.Vec2d{})
		for i := range n {
			y := 4 * float64(i)
			start := m.Vec2d{X: 10, Y: y}
			k := paddle(t, h, ball, start, m.Vec2d{X: -meetSpeed})
			r.resets = append(r.resets, reset{e: k, at: start, velocity: m.Vec2d{X: -meetSpeed}})
			for _, x := range []float64{7, 3, -1} {
				at := m.Vec2d{X: x, Y: y}
				r.resets = append(r.resets, reset{e: thrown(t, h, ball, at, m.Vec2d{}), at: at})
			}
			h.spawn(t, spawnRequest{
				Kind:  kindShapedStatic,
				Place: ecsphysics2d.Position{Current: m.Vec2d{X: 5, Y: y}},
				Shape: sensorCircle(0.5),
			})
		}
		h.frames(t, 100)
		mallocs := allocationsDuring(func() {
			for range ticks {
				h.frame(t)
			}
		})
		carried, crossed := 0, 0
		for _, entry := range h.contacts(t) {
			if entry.T < 1 && entry.Phase == ecsphysics2d.PhaseContinuing {
				if entry.Sensor {
					crossed++
				} else {
					carried++
				}
			}
		}
		return float64(mallocs) / ticks, carried, crossed
	}

	empty, _, _ := measure(0)
	full, carried, crossed := measure(64)
	t.Logf("objects a step: %.3f with no Bodies, %.3f at N=64 with %d carried Contacts and %d crossed Sensors a tick",
		empty, full, carried, crossed)
	if carried != 3*64 || crossed != 64 {
		t.Fatalf("the measured scene carried %d balls and crossed %d Sensors a tick, want %d and %d",
			carried, crossed, 3*64, 64)
	}
	if full-empty > 0.05 {
		t.Errorf("carrying past the first Hit costs %.3f objects a tick, want none", full-empty)
	}
}
