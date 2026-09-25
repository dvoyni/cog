package internal

import (
	"fmt"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/libs/m"
)

// The price an engaged Body pays (continuous-collision.md § Acceptance › The
// cost bar): N fast Bodies over BenchmarkTheStep's world, each set back where
// it started before every tick, so every tick is the same tick. The price is
// the step's time less the same scene's with the mechanism taken out, divided
// by N, read on the minimums of an interleaved A/B over two built binaries.
//
// The same scene is what the mechanism is taken out of, not the plain world
// without the fast Bodies: they are integrated, indexed, walked and solved
// either way, and that is not what continuous collision costs.
//
// Each fast Body flies along a lane of its own halfway between two rows of the
// world, 32 to a lane over the n/32 rows its Dynamic half fills, so its path
// box lies over cells the world is listed in, and its path test walks them.
// With no Hit, nothing is in the lane but the Bodies ahead and behind, which
// move with it: two marked partners whose relative path meets nothing. With a
// Hit, a thin Static board stands across the lane a fifth of the way along,
// which the path meets at about T = 0.22 and the Body would otherwise pass
// clean through.

// engagedSpeed is the tunnelling scene's throw, 40.4 m/s: 0.673 m a tick,
// over three times the movers' extent of 0.2 m.
const engagedSpeed = 40.4

// engagedMovers are the Shapes the fast Bodies are made of: a circle, whose
// path test is the closed-form Probe, and a box, whose path test is the swept
// convex one. Both have a minimum extent of 0.2 m.
var engagedMovers = []struct {
	name  string
	shape ecsphysics2d.Shape
}{
	{"circle", ecsphysics2d.NewCircleShape(0.2, m.Vec2d{})},
	{"box", ecsphysics2d.NewBoxShape(0.4, 0.4, 0)},
}

// engagedSensors are the moving Sensors priced against the same bar: a box, and
// a segment standing across its path. Neither has a gate.
var engagedSensors = []struct {
	name  string
	shape ecsphysics2d.Shape
}{
	{"box", asSensor(ecsphysics2d.NewBoxShape(0.4, 0.4, 0))},
	{"segment", asSensor(ecsphysics2d.NewSegmentShape(m.Vec2d{Y: -0.2}, m.Vec2d{Y: 0.2}, 0))},
}

func asSensor(shape ecsphysics2d.Shape) ecsphysics2d.Shape {
	shape.Sensor = true
	return shape
}

// laneAt is where the i-th fast Body starts: 0.85 m apart along a lane, which
// is its travel and its width with room to spare, and each lane halfway up the
// gap between two rows of the world.
func laneAt(i int) m.Vec2d {
	return m.Vec2d{X: 0.85 * float64(i%32), Y: 1.7*float64(i/32) + 0.85}
}

// populateEngaged is BenchmarkTheStep's world at n, and n fast Bodies of that
// Shape over it, set back before every tick. A board across each lane when hit
// is set. It returns the fast Bodies.
func populateEngaged(t testing.TB, h *harness, r *resetter, n int, shape ecsphysics2d.Shape, hit bool) []ecs.Entity {
	t.Helper()
	populate(t, h, n)
	velocity := m.Vec2d{X: engagedSpeed}
	fast := make([]ecs.Entity, n)
	for i := range n {
		at := laneAt(i)
		fast[i] = h.spawn(t, spawnRequest{
			Kind:     kindShapedBody,
			Place:    ecsphysics2d.Position{Current: at},
			Velocity: ecsphysics2d.Velocity{Linear: velocity},
			Body:     dynamic(t, 1, 1, 0, 0),
			Shape:    shape,
		})
		r.resets = append(r.resets, reset{e: fast[i], at: at, velocity: velocity})
		if hit {
			h.spawn(t, spawnRequest{
				Kind:  kindShapedStatic,
				Place: ecsphysics2d.Position{Current: at.Add(m.Vec2d{X: 0.375})},
				Shape: ecsphysics2d.NewBoxShapeFor(ecsphysics2d.NewBB(-0.025, -0.25, 0.025, 0.25), 0),
			})
		}
	}
	return fast
}

// engagedCount is what the last tick did to the fast Bodies: how many moved at
// least 0.2 m, which is every mover's minimum extent or more, so the gate, and
// how many were stopped, a Contact naming one with T < 1 that is no Sensor's. Anything else naming one,
// which would be a Hit the scene does not mean to have, is counted apart.
func engagedCount(t testing.TB, h *harness, fast []ecs.Entity) (moved, stopped, stray int) {
	t.Helper()
	isFast := make(map[ecs.Entity]bool, len(fast))
	for _, e := range fast {
		isFast[e] = true
		read := h.read(t, e)
		travel := read.Place.Current.Sub(read.Place.Previous)
		if travel.Dot(travel) >= 0.2*0.2 {
			moved++
		}
	}
	for _, entry := range h.contacts(t) {
		if !isFast[entry.A] && !isFast[entry.B] {
			continue
		}
		if entry.T < 1 && !entry.Sensor {
			stopped++
		} else {
			stray++
		}
	}
	return moved, stopped, stray
}

// TestTheEngagedSceneEngagesEveryFastBody holds the benchmark scenes to what
// they claim, so the price is read off the scene it says it is: with no Hit,
// every fast Body passes the gate and nothing names one; with a Hit, every one
// is stopped once and nothing else names it; and a moving Sensor names nothing.
func TestTheEngagedSceneEngagesEveryFastBody(t *testing.T) {
	const n = 64
	for _, mover := range engagedMovers {
		for _, hit := range []bool{false, true} {
			for _, bodies := range []bool{false, true} {
				shape := mover.shape
				shape.StopsAtBodies = bodies
				t.Run(fmt.Sprintf("%s/hit=%v/bodies=%v", mover.name, hit, bodies), func(t *testing.T) {
					r := &resetter{}
					h := newHarnessWithPlugins(t, nil, uint32(4*n), r)
					fast := populateEngaged(t, h, r, n, shape, hit)
					h.game.push = m.Vec2d{X: 10}
					h.frames(t, 20)
					moved, stopped, stray := engagedCount(t, h, fast)
					if hit {
						if stopped != n || stray != 0 {
							t.Fatalf("%d of %d fast Bodies were stopped, and %d other entries name one", stopped, n, stray)
						}
						return
					}
					if moved != n || stopped != 0 || stray != 0 {
						t.Fatalf("%d of %d fast Bodies passed the gate, with %d stops and %d other entries naming one",
							moved, n, stopped, stray)
					}
				})
			}
		}
	}
	for _, sensor := range engagedSensors {
		t.Run("sensor/"+sensor.name, func(t *testing.T) {
			r := &resetter{}
			h := newHarnessWithPlugins(t, nil, uint32(4*n), r)
			fast := populateEngaged(t, h, r, n, sensor.shape, false)
			h.game.push = m.Vec2d{X: 10}
			h.frames(t, 20)
			if _, stopped, stray := engagedCount(t, h, fast); stopped != 0 || stray != 0 {
				t.Fatalf("the moving Sensors are named by %d stops and %d other entries, want none", stopped, stray)
			}
		})
	}
}

// BenchmarkTheEngagedBody is one whole tick over the engaged scenes: each mover
// with no Hit and with one, its path tested against the statics alone and,
// with StopsAtBodies, against the Body index too, at N = 256 and N = 1 024 fast
// Bodies. The second less the first is the Body-index share. What each tick
// engaged is reported beside the time, counted once outside the timed loop, so
// the binary with the mechanism taken out shows it did not engage.
func BenchmarkTheEngagedBody(b *testing.B) {
	for _, mover := range engagedMovers {
		for _, hit := range []bool{false, true} {
			scene := "nohit"
			if hit {
				scene = "hit"
			}
			for _, bodies := range []bool{false, true} {
				against, shape := "statics", mover.shape
				if bodies {
					against = "bodies"
					shape.StopsAtBodies = true
				}
				for _, n := range []int{256, 1024} {
					b.Run(fmt.Sprintf("%s/%s/%s/N=%d", mover.name, scene, against, n), func(b *testing.B) {
						benchmarkEngaged(b, n, shape, hit)
					})
				}
			}
		}
	}
}

// BenchmarkTheMovingSensor is the same over N moving box or segment Sensors,
// meeting nothing, which is recorded against the bar and against the discrete
// test the sweep replaced.
func BenchmarkTheMovingSensor(b *testing.B) {
	for _, sensor := range engagedSensors {
		for _, n := range []int{256, 1024} {
			b.Run(fmt.Sprintf("%s/N=%d", sensor.name, n), func(b *testing.B) {
				benchmarkEngaged(b, n, sensor.shape, false)
			})
		}
	}
}

func benchmarkEngaged(b *testing.B, n int, shape ecsphysics2d.Shape, hit bool) {
	r := &resetter{}
	h := newHarnessWithPlugins(b, nil, uint32(4*n), r)
	fast := populateEngaged(b, h, r, n, shape, hit)
	h.game.push = m.Vec2d{X: 10}
	h.frames(b, 100)
	moved, stopped, stray := engagedCount(b, h, fast)
	if moved != n && !hit {
		b.Fatalf("%d of %d fast Bodies passed the gate", moved, n)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		h.frame(b)
	}
	b.StopTimer()
	b.ReportMetric(float64(stopped), "stops")
	b.ReportMetric(float64(stray), "others")
}
