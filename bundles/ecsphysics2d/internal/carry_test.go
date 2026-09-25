package internal

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/libs/m"
)

// A fast Kinematic body carries what it hits (continuous-collision.md § Solve:
// back to T). A Kinematic body is never stopped and never pushed, so a Dynamic
// body it meets on its path is carried along with it from the moment they
// meet: Previous + T·d_self + (1 − T)·d_other, where the Kinematic side's own
// path is d_other.

// paddle spawns a Kinematic body of that Shape at a place, moving at a
// velocity.
func paddle(t testing.TB, h *harness, shape ecsphysics2d.Shape, at, velocity m.Vec2d) ecs.Entity {
	t.Helper()
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
