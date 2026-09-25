package internal

import (
	"fmt"
	"math"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// Two moving Bodies meet (continuous-collision.md § Index: one bit and a path
// box, § Detect › Which target is tested where, › Which walk writes a pair).
// A pair where both parties are marked is tested along their relative motion,
// from both start poses, with one shared T, and written once; every marked
// entry is listed in the grid by its path box, so two fast Bodies crossing at
// an angle find each other at all.

// meetSpeed is ten metres a tick, the specification's own numbers.
const meetSpeed = 10 / tick

// thrown spawns a solid Dynamic body of that Shape at a place, moving at a
// velocity. Its Shape StopsAtBodies, since what these scenes throw a Body at,
// or leave in a fast one's way, is Bodies, which a fast Body meets only with
// the flag set.
func thrown(t testing.TB, h *harness, shape Shape, at, velocity m.Vec2d) ecs.Entity {
	t.Helper()
	body, shape, _ := solidFor(t, shape)
	shape.StopsAtBodies = true
	return h.spawn(t, spawnRequest{
		Kind:     kindShapedBody,
		Place:    Position{Current: at},
		Velocity: Velocity{Linear: velocity},
		Body:     body,
		Shape:    shape,
	})
}

// meetingOf is the one tick's entries for the unordered pair, however many
// there are.
func meetingOf(list []Contact, a, b ecs.Entity) []Contact {
	var found []Contact
	for _, entry := range list {
		if (entry.A == a && entry.B == b) || (entry.A == b && entry.B == a) {
			found = append(found, entry)
		}
	}
	return found
}

// meetCase is one pair of fast Bodies and where they must stand when they
// meet, relative to the scene's starting point.
type meetCase struct {
	name         string
	shape        Shape
	fromA, fromB m.Vec2d
	velA, velB   m.Vec2d
	t            float64
	atA, atB     m.Vec2d
	tolerance    float64
	// alone, when 1 or 2, is the one party, A or B, whose Shape
	// StopsAtBodies; otherwise both do.
	alone int
}

// checkMeet runs one case from eight starting points, the whole scene shifted
// by an eighth of a tick's travel each time, so the grid cells fall
// differently: the pair is one stopping Contact with the shared T, and both
// Bodies stand where they met.
func checkMeet(t *testing.T, c meetCase) {
	t.Helper()
	for phase := range phases {
		h := newHarness(t)
		shift := m.Vec2d{X: 10 * float64(phase) / phases, Y: 3.7 * float64(phase) / phases}
		a := thrown(t, h, c.shape, c.fromA.Add(shift), c.velA)
		b := thrown(t, h, c.shape, c.fromB.Add(shift), c.velB)
		for party, e := range [2]ecs.Entity{a, b} {
			if c.alone != 0 && c.alone != party+1 {
				shape := h.read(t, e).Shape
				shape.StopsAtBodies = false
				h.setShape(t, e, shape)
			}
		}
		h.frame(t)

		entries := meetingOf(h.contacts(t), a, b)
		if len(entries) != 1 {
			t.Fatalf("phase %d: the pair is %d entries, want one", phase, len(entries))
		}
		entry := entries[0]
		if entry.Sensor || entry.Count != 1 || entry.Points[0].Depth != 0 {
			t.Errorf("phase %d: the pair is not a stop: Sensor %v, %d point(s), Depth %.6f",
				phase, entry.Sensor, entry.Count, entry.Points[0].Depth)
		}
		if math.Abs(entry.T-c.t) > c.tolerance {
			t.Errorf("phase %d: they met at T %.9f, want %.9f", phase, entry.T, c.t)
		}
		for _, side := range []struct {
			name string
			e    ecs.Entity
			want m.Vec2d
		}{{"A", a, c.atA.Add(shift)}, {"B", b, c.atB.Add(shift)}} {
			if at := h.read(t, side.e).Place.Current; at.Distance(side.want) > c.tolerance {
				t.Errorf("phase %d: %s stands at %v, want %v", phase, side.name, at, side.want)
			}
		}
	}
}

// Two balls of radius 1 thrown at each other, A going x 0 → 10 and B 10 → 0.
// At their end poses each already touches the other where it started, so a
// test against end poses skips both and they swap places; along their
// relative motion they meet at T = 0.4, A at 4 and B at 6.
func TestTwoFastBallsMeetHeadOn(t *testing.T) {
	checkMeet(t, meetCase{
		name:  "balls",
		shape: NewCircleShape(1, m.Vec2d{}),
		fromA: m.Vec2d{}, fromB: m.Vec2d{X: 10},
		velA: m.Vec2d{X: meetSpeed}, velB: m.Vec2d{X: -meetSpeed},
		t:   0.4,
		atA: m.Vec2d{X: 4}, atB: m.Vec2d{X: 6},
		// The circle's Probe is exact.
		tolerance: 1e-9,
	})
}

// The same meeting where only one of the two Shapes StopsAtBodies, either one:
// a pair meets when either party asks for Bodies. A is the lower Entity, whose
// walk judges a pair when both ask; when only B does, B's walk judges it.
func TestTwoFastBallsMeetWhenOnlyOneStopsAtBodies(t *testing.T) {
	for _, alone := range []int{1, 2} {
		t.Run(fmt.Sprintf("only %c", 'A'+alone-1), func(t *testing.T) {
			checkMeet(t, meetCase{
				name:  "balls",
				shape: NewCircleShape(1, m.Vec2d{}),
				fromA: m.Vec2d{}, fromB: m.Vec2d{X: 10},
				velA: m.Vec2d{X: meetSpeed}, velB: m.Vec2d{X: -meetSpeed},
				t:   0.4,
				atA: m.Vec2d{X: 4}, atB: m.Vec2d{X: 6},
				tolerance: 1e-9,
				alone:     alone,
			})
		})
	}
}

// The same meeting for two boxes 2 m square, whose relative motion is
// Probed by the swept convex test rather than the circle's closed form.
func TestTwoFastBoxesMeetHeadOn(t *testing.T) {
	checkMeet(t, meetCase{
		name:  "boxes",
		shape: NewBoxShape(2, 2, 0),
		fromA: m.Vec2d{}, fromB: m.Vec2d{X: 10},
		velA: m.Vec2d{X: meetSpeed}, velB: m.Vec2d{X: -meetSpeed},
		t:   0.4,
		atA: m.Vec2d{X: 4}, atB: m.Vec2d{X: 6},
		// The swept convex test stops within a nanometre of the surface,
		// which is a nanometre over the closing speed in T.
		tolerance: 1e-6,
	})
}

// Two balls of radius 1 crossing at right angles, A going (−10, 0) → (10, 0)
// and B (0, −10) → (0, 10). Listed by their end boxes, neither walk finds the
// other: A's path reaches y ∈ [−1, 1] and B ends at y = 10. Listed by their
// path boxes they meet, on the way to the origin, where they first touch:
// both √2 short of it, at T = (10 − √2)/20.
func TestTwoFastBallsCrossingAtRightAnglesMeet(t *testing.T) {
	first := (10 - math.Sqrt2) / 20
	checkMeet(t, meetCase{
		name:  "balls",
		shape: NewCircleShape(1, m.Vec2d{}),
		fromA: m.Vec2d{X: -10}, fromB: m.Vec2d{Y: -10},
		velA: m.Vec2d{X: 2 * meetSpeed}, velB: m.Vec2d{Y: 2 * meetSpeed},
		t:   first,
		atA: m.Vec2d{X: -math.Sqrt2}, atB: m.Vec2d{Y: -math.Sqrt2},
		tolerance: 1e-9,
	})
}

// A fast ball catching a slower one that is itself past the gate. Taken at the
// slower one's end pose, A meets it at T = 0.4, where it is not; a target that
// is marked stopping nothing, the two pass through each other. Along their
// relative motion A, 0 → 10, meets B, 3 → 6, at T = 1/7.
func TestAFastBallCatchesASlowerMovingOne(t *testing.T) {
	checkMeet(t, meetCase{
		name:  "balls",
		shape: NewCircleShape(1, m.Vec2d{}),
		fromA: m.Vec2d{}, fromB: m.Vec2d{X: 3},
		velA: m.Vec2d{X: meetSpeed}, velB: m.Vec2d{X: 0.3 * meetSpeed},
		t:   1.0 / 7,
		atA: m.Vec2d{X: 10.0 / 7}, atB: m.Vec2d{X: 3 + 3.0/7},
		tolerance: 1e-9,
	})
}

// The seam rule reads the relative motion for a pair tested along it. A ball
// of radius 1 going ten metres a tick overtakes one going nine and a half,
// 2.3 m ahead of it: they meet at T = 0.6, but close on each other by half a
// metre in the tick, less than the extent of either. By its own path the
// faster one would close by ten and stop; along the pair's relative motion
// neither can get past the other within the tick, so the pair is left to the
// discrete walk, which finds it at T = 1 where the tick left both.
func TestTwoFastBodiesClosingByLessThanTheirExtentAreLeftToTheDiscreteWalk(t *testing.T) {
	for phase := range phases {
		h := newHarness(t)
		shift := m.Vec2d{X: 10 * float64(phase) / phases}
		ball := NewCircleShape(1, m.Vec2d{})
		a := thrown(t, h, ball, shift, m.Vec2d{X: meetSpeed})
		b := thrown(t, h, ball, m.Vec2d{X: 2.3}.Add(shift), m.Vec2d{X: 0.95 * meetSpeed})
		h.frame(t)

		entries := meetingOf(h.contacts(t), a, b)
		if len(entries) != 1 {
			t.Fatalf("phase %d: the pair is %d entries, want one", phase, len(entries))
		}
		if entries[0].T != 1 {
			t.Errorf("phase %d: the pair was stopped at T %.6f, want it left to the discrete walk", phase, entries[0].T)
		}
	}
}

// A stopped Body's pairs with the awake Bodies are tested where it stopped,
// wherever that is in the grid. A ball thrown three metres a tick is stopped by
// a wall a metre on, and a Kinematic block it grazes stands under the stopping
// point, in a grid cell its end pose is nowhere near. Listed by its end box
// alone, neither walk would reach the other; listed by its path box, the pair
// is tested where the ball stopped and reports the stop's T.
func TestAStoppedBodysPairWithAnAwakeBodyIsTestedWhereItStopped(t *testing.T) {
	const graze = 0.003
	for phase := range phases {
		h := newHarness(t)
		shift := m.Vec2d{Y: 0.25 * float64(phase)}
		wall := h.spawn(t, spawnRequest{
			Kind:  kindShapedStatic,
			Place: Position{Current: m.Vec2d{X: 1}.Add(shift)},
			Shape: NewSegmentShape(m.Vec2d{Y: -2}, m.Vec2d{Y: 2}, 0.05),
		})
		block := h.spawn(t, spawnRequest{
			Kind:  kindShapedKinematic,
			Place: Position{Current: m.Vec2d{X: 0.7, Y: -0.3 + graze}.Add(shift)},
			Shape: NewBoxShape(0.2, 0.2, 0),
		})
		ball := thrown(t, h, NewCircleShape(0.2, m.Vec2d{}), shift, m.Vec2d{X: 3 / tick})
		h.frame(t)

		list := h.contacts(t)
		stop, stopped := between(list, ball, wall)
		if !stopped || !(stop.T < 1) {
			t.Fatalf("phase %d: the wall did not stop the ball", phase)
		}
		under, found := between(list, ball, block)
		if !found {
			t.Fatalf("phase %d: the stopped ball reports no Contact with the block it stands on", phase)
		}
		if under.T != stop.T {
			t.Errorf("phase %d: the block's Contact reports T %.6f, want the stop's %.6f", phase, under.T, stop.T)
		}
		if depth := under.Points[0].Depth; math.Abs(depth-graze) > 1e-6 {
			t.Errorf("phase %d: the block's Contact has Depth %.6f, want the graze %.6f", phase, depth, graze)
		}
	}
}

// TestTheMeetingsSitOnTheEnginesAllocationLine is the step's allocation claim
// over a scene where fast Bodies meet each other every few ticks: in each lane
// two of them, circles or boxes, thrown at each other between two walls and
// bouncing with a Restitution of 1. It measures the path box, the partners,
// the relative path test and the one Contact two parties share, none of which
// the lone stops reach.
func TestTheMeetingsSitOnTheEnginesAllocationLine(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	const ticks = 4_000

	measure := func(n int) (float64, int) {
		h := newHarnessWith(t, nil, uint32(5*max(n, 1)))
		movers := populateMeetings(t, h, n)
		h.frames(t, 100)
		mallocs := allocationsDuring(func() {
			for range ticks {
				h.frame(t)
			}
		})
		met := 0
		for range 30 {
			h.frame(t)
			for _, entry := range h.contacts(t) {
				if entry.T < 1 && !entry.Sensor && entry.Phase != PhaseEnded && movers[entry.A] && movers[entry.B] {
					met++
				}
			}
		}
		return float64(mallocs) / ticks, met
	}

	empty, _ := measure(0)
	full, met := measure(64)
	t.Logf("objects a step: %.3f with no Bodies, %.3f at N=64 with %d meetings over 30 ticks", empty, full, met)
	if met == 0 {
		t.Fatal("the measured scene met nothing, so it does not measure a meeting")
	}
	if full-empty > 0.05 {
		t.Errorf("fast Bodies meeting costs %.3f objects a tick, want none", full-empty)
	}
}

// populateMeetings is n lanes, each two Static walls 6 m apart and two fast
// Bodies between them thrown at each other at projectile speed, alternately
// circles and boxes, with a Restitution of 1. A box stopped on one corner
// spins, so each lane is closed above by a Static divider, and nothing leaves
// the lane it was thrown in.
func populateMeetings(t testing.TB, h *harness, n int) map[ecs.Entity]bool {
	t.Helper()
	movers := map[ecs.Entity]bool{}
	wall := NewSegmentShape(m.Vec2d{Y: -0.9}, m.Vec2d{Y: 0.9}, 0.1)
	wall.Restitution = 1
	divider := NewSegmentShape(m.Vec2d{X: -3}, m.Vec2d{X: 3}, 0.1)
	for i := range n {
		y := 2 * float64(i)
		h.spawn(t, spawnRequest{
			Kind:  kindShapedStatic,
			Place: Position{Current: m.Vec2d{Y: y + 1}},
			Shape: divider,
		})
		for _, x := range []float64{-3, 3} {
			h.spawn(t, spawnRequest{
				Kind:  kindShapedStatic,
				Place: Position{Current: m.Vec2d{X: x, Y: y}},
				Shape: wall,
			})
		}
		shape := NewCircleShape(0.2, m.Vec2d{})
		if i%2 == 1 {
			shape = NewBoxShape(0.4, 0.4, 0)
		}
		shape.Restitution = 1
		for _, side := range []float64{-1, 1} {
			movers[thrown(t, h, shape, m.Vec2d{X: side, Y: y}, m.Vec2d{X: -side * projectileSpeed})] = true
		}
	}
	return movers
}
