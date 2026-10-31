package internal

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// A fast solid Body is stopped where its path first meets something
// (continuous-collision.md § Detect: one path pass, § Solve: back to T). The
// nine reference cases and the speed sweep are in tunnelling_test.go; these
// are the rules around the stop: what it skips, what its other Contacts say,
// what a filter's drop does, and the limits the specification pins.

// floorAt spawns a long Static floor whose top face is at y = 0.
func floorAt(t testing.TB, h *harness) ecs.Entity {
	t.Helper()
	return h.spawn(t, spawnRequest{
		Kind:  kindShapedStatic,
		Place: Position{Current: m.Vec2d{Y: -0.05}},
		Shape: NewSegmentShape(m.Vec2d{X: -20}, m.Vec2d{X: 200}, 0.05),
	})
}

// rollingBall spawns a fast ball of radius 0.2 sitting on that floor, sunk a
// hair into it as a resting Body is, and thrown along it at projectile speed.
func rollingBall(t testing.TB, h *harness) ecs.Entity {
	t.Helper()
	body, shape, _ := solidFor(t, NewCircleShape(0.2, m.Vec2d{}))
	return h.spawn(t, spawnRequest{
		Kind:     kindShapedBody,
		Place:    Position{Current: m.Vec2d{Y: 0.2 - 0.005}},
		Velocity: Velocity{Linear: m.Vec2d{X: projectileSpeed}},
		Body:     body,
		Shape:    shape,
	})
}

// between is the tick's Contact for the unordered pair, if there is one.
func between(list []Contact, a, b ecs.Entity) (Contact, bool) {
	for _, entry := range list {
		if (entry.A == a && entry.B == b) || (entry.A == b && entry.B == a) {
			return entry, true
		}
	}
	return Contact{}, false
}

// A fast ball rolling along the floor touches it where every tick begins, and
// only a surface its path enters can stop it: so the floor never stops it, and
// it covers its whole travel every tick, under gravity, pressed onto the floor.
func TestAFastBallRollingAlongTheFloorIsNotStoppedByIt(t *testing.T) {
	h, _ := newGravityHarness(t, nil, m.Vec2d{Y: -9.8})
	floor := floorAt(t, h)
	ball := rollingBall(t, h)

	const ticks = 60
	touching := 0
	for i := range ticks {
		before := h.read(t, ball).Place.Current
		h.frame(t)
		after := h.read(t, ball).Place.Current
		entry, found := between(h.contacts(t), ball, floor)
		if found {
			touching++
			if entry.T < 1 {
				t.Fatalf("tick %d: the floor stopped the ball at T %.4f", i, entry.T)
			}
		}
		if moved := after.X - before.X; math.Abs(moved-projectileSpeed*tick) > 1e-9 {
			t.Fatalf("tick %d: the ball rolled %.6f m, want its whole travel %.6f m", i, moved, projectileSpeed*tick)
		}
	}
	if touching < ticks/2 {
		t.Errorf("the ball touched the floor on %d of %d ticks, so it was not rolling along it", touching, ticks)
	}
}

// A stopped Body's other pairs are tested where it stopped: the ball rolling
// along the floor is stopped by a wall, and its floor Contact that tick is the
// one at the stopping point, reporting the same T and a Depth of its own.
func TestAStoppedBodysOtherPairsAreTestedWhereItStopped(t *testing.T) {
	h, _ := newGravityHarness(t, nil, m.Vec2d{Y: -9.8})
	floor := floorAt(t, h)
	ball := rollingBall(t, h)
	wall := h.spawn(t, spawnRequest{
		Kind:  kindShapedStatic,
		Place: Position{Current: m.Vec2d{X: 3}},
		Shape: NewSegmentShape(m.Vec2d{Y: 0.5}, m.Vec2d{Y: 4}, 0.4),
	})

	for i := range 10 {
		h.frame(t)
		list := h.contacts(t)
		stop, stopped := between(list, ball, wall)
		if !stopped {
			continue
		}
		if !(stop.T < 1) || stop.Count != 1 || stop.Points[0].Depth != 0 {
			t.Fatalf("tick %d: the wall's Contact is not a stop: T %.4f, %d point(s), Depth %.4f",
				i, stop.T, stop.Count, stop.Points[0].Depth)
		}
		underfoot, found := between(list, ball, floor)
		if !found {
			t.Fatalf("tick %d: the stopped ball reports no Contact with the floor it stands on", i)
		}
		if underfoot.T != stop.T {
			t.Errorf("tick %d: the floor Contact reports T %.6f, want the stop's %.6f", i, underfoot.T, stop.T)
		}
		if !(underfoot.Points[0].Depth > 0) {
			t.Errorf("tick %d: the floor Contact's Depth is %.6f, want the ball's own overlap where it stopped",
				i, underfoot.Points[0].Depth)
		}
		// Where it stopped is where it meets the wall's rounded lower end: the
		// ball's centre is the sum of the two radii from the wall's segment.
		at := h.read(t, ball).Place.Current
		if gap := math.Hypot(at.X-3, math.Max(0.5-at.Y, 0)) - (0.4 + 0.2); math.Abs(gap) > 1e-6 {
			t.Errorf("tick %d: the ball stands at %v, %.9f m off the wall, want touching it", i, at, gap)
		}
		return
	}
	t.Fatal("the ball never met the wall")
}

// pinnedThrow throws a mover along +X at a target and reports where it ended.
func pinnedThrow(
	t *testing.T, h *harness, shape Shape, from, speed float64, ticks int,
) (ecs.Entity, float64) {
	t.Helper()
	body, shape, _ := solidFor(t, shape)
	mover := h.spawn(t, spawnRequest{
		Kind:     kindShapedBody,
		Place:    Position{Current: m.Vec2d{X: from}},
		Velocity: Velocity{Linear: m.Vec2d{X: speed}},
		Body:     body,
		Shape:    shape,
	})
	h.frames(t, ticks)
	return mover, h.read(t, mover).Place.Current.X
}

// Pinned limit: a zero-thickness target. A bare segment has nothing to stand
// in the way of a Body the discrete walk sees on its far side, so it is the
// target the gate is closest to letting through. Just under the gate nothing
// is marked, and a mover that travels less than its own extent a tick is
// always first seen overlapping the segment from the near side, so the
// discrete walk holds it. This is today's behaviour, held so that a change to
// the gate or to the walk that loses it is seen.
func TestPinnedAZeroThicknessTargetJustUnderTheGate(t *testing.T) {
	for _, mover := range tunnelMovers() {
		_, _, extent := solidFor(t, mover.shape)
		speed := 0.99 * extent / tick
		t.Run(mover.name, func(t *testing.T) {
			for phase := range phases {
				h := newHarness(t)
				h.spawn(t, spawnRequest{
					Kind:  kindShapedStatic,
					Place: Position{Current: m.Vec2d{X: targetX}},
					Shape: NewSegmentShape(m.Vec2d{Y: -2}, m.Vec2d{Y: 2}, 0),
				})
				start := targetX - 1 - speed*tick*float64(phase)/phases
				_, end := pinnedThrow(t, h, mover.shape, start, speed, int(math.Ceil(2/(speed*tick))))
				if end > targetX {
					t.Errorf("phase %d: from x = %.3f it ended at %.3f, past the bare segment at %.3f",
						phase, start, end, targetX)
				}
			}
		})
	}
}

// Pinned limit: the target behind a dropped stop. A one-way platform is a
// filter System dropping the stopping Contact, and a drop means no stop: the
// Body stays where the tick left it. The path test kept only the first Hit,
// so a second wall behind the platform, within the same tick's path, was
// never tested, and the Body passes it too. That is the named limit, held.
//
// An ignored stop is the same: the app refused it, and it stops nothing.
func TestPinnedADroppedStopHidesTheTargetBehindIt(t *testing.T) {
	for _, mark := range []struct {
		name  string
		apply func(*Contact)
	}{
		{"dropped", func(entry *Contact) { entry.Drop() }},
		{"ignored", func(entry *Contact) { entry.Ignore() }},
	} {
		t.Run(mark.name, func(t *testing.T) {
			h := newHarness(t)
			platform := h.spawn(t, spawnRequest{
				Kind:  kindShapedStatic,
				Place: Position{Current: m.Vec2d{X: 1}},
				Shape: NewSegmentShape(m.Vec2d{Y: -2}, m.Vec2d{Y: 2}, 0.05),
			})
			behind := h.spawn(t, spawnRequest{
				Kind:  kindShapedStatic,
				Place: Position{Current: m.Vec2d{X: 2}},
				Shape: NewSegmentShape(m.Vec2d{Y: -2}, m.Vec2d{Y: 2}, 0.05),
			})
			stops := 0
			h.game.filter = func(entry *Contact) {
				if entry.A == platform || entry.B == platform {
					if entry.T < 1 {
						stops++
					}
					mark.apply(entry)
				}
			}

			// Three metres a tick, from x = 0: the one tick's path crosses the
			// platform and the wall behind it and ends clear of both.
			speed := 3 / tick
			body, shape, _ := solidFor(t, NewCircleShape(0.2, m.Vec2d{}))
			ball := h.spawn(t, spawnRequest{
				Kind:     kindShapedBody,
				Velocity: Velocity{Linear: m.Vec2d{X: speed}},
				Body:     body,
				Shape:    shape,
			})
			h.frame(t)
			if stops != 1 {
				t.Fatalf("the platform wrote %d stopping Contacts, want 1", stops)
			}
			if _, found := between(h.contacts(t), ball, behind); found {
				t.Errorf("the wall behind the platform was tested, want it hidden by the first Hit")
			}
			if at := h.read(t, ball).Place.Current.X; math.Abs(at-3) > 1e-9 {
				t.Errorf("the ball stands at x = %.6f, want where the tick left it, x = 3", at)
			}
		})
	}
}

// TestTheStopsSitOnTheEnginesAllocationLine is the step's allocation claim over
// a scene where fast Bodies are stopped every tick: circles and boxes bouncing
// between two walls, each in a lane of its own. It measures the path pass for
// a solid Body, the stopping Contact, the copy at T and Solve's move back, none
// of which the other scenes on the line reach.
func TestTheStopsSitOnTheEnginesAllocationLine(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	const ticks = 4_000

	measure := func(n int) (float64, int) {
		h := newHarnessWith(t, nil, uint32(4*max(n, 1)))
		populateStops(t, h, n)
		h.frames(t, 100)
		mallocs := allocationsDuring(func() {
			for range ticks {
				h.frame(t)
			}
		})
		stopped := 0
		for range 30 {
			h.frame(t)
			for _, entry := range h.contacts(t) {
				if entry.T < 1 && !entry.Sensor {
					stopped++
				}
			}
		}
		return float64(mallocs) / ticks, stopped
	}

	empty, _ := measure(0)
	full, stopped := measure(64)
	t.Logf("objects a step: %.3f with no Bodies, %.3f at N=64 with %d stops over 30 ticks", empty, full, stopped)
	if stopped == 0 {
		t.Fatal("the measured scene stopped nothing, so it does not measure a stop")
	}
	if full-empty > 0.05 {
		t.Errorf("stopping fast Bodies costs %.3f objects a tick, want none", full-empty)
	}
}

// populateStops is n lanes, each two Static walls 4 m apart and one fast Body
// between them, alternately a circle and a box, bouncing from wall to wall at
// projectile speed with a Restitution of 1.
func populateStops(t testing.TB, h *harness, n int) {
	t.Helper()
	wall := NewSegmentShape(m.Vec2d{Y: -0.4}, m.Vec2d{Y: 0.4}, 0.1)
	wall.Restitution = 1
	for i := range n {
		y := 2 * float64(i)
		for _, x := range []float64{-2, 2} {
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
		body, shape, _ := solidFor(t, shape)
		h.spawn(t, spawnRequest{
			Kind:     kindShapedBody,
			Place:    Position{Current: m.Vec2d{Y: y}},
			Velocity: Velocity{Linear: m.Vec2d{X: projectileSpeed}},
			Body:     body,
			Shape:    shape,
		})
	}
}
