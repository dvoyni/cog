package internal

import (
	"fmt"
	"math"
	"sync/atomic"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/kernel"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

// The reference tunnelling scene: the one thing every price on the
// continuous-collision map is quoted against (issue #328, its task ticket
// #576). Nothing here is a mechanism. It is the scene the mechanism has to
// pass, the census of which Bodies its gate would engage, and the count the
// whole-step benchmark is read beside.
//
// The tunnelling cases state the guarantee the map settled and fail today:
// a solid Dynamic body does not end on the far side of a Static or Kinematic
// body it was thrown at, nor of a Dynamic one once it carries the opt-in
// Component. They are left failing on purpose, on this branch, so that the
// mechanism's work is what turns them green.

// projectileSpeed is the swept Sensor's own acceptance speed: 0.673 m a tick at
// 60 Hz, which clears a 0.4 m wall whole.
const projectileSpeed = 40.4

// phases is how many starting offsets, spread evenly over one tick's travel,
// each case is thrown from. Where a Body stands when the tick ends decides
// whether a discrete test ever sees it overlap the target, so one throw
// proves nothing either way; eight say how often it tunnels.
const phases = 8

// targetX is where every target stands; every mover is thrown at it along +X
// from at least two metres short.
const targetX = 1.0

// tunnelMover is one of the three solid Shapes thrown.
type tunnelMover struct {
	name  string
	shape Shape
}

// tunnelTarget is one of the three things they are thrown at.
type tunnelTarget struct {
	name  string
	spawn func(t testing.TB, h *harness) ecs.Entity
}

// movers are the three solid Shapes, each thin enough along X that the sum of
// its thickness and a target's is under a tick's 0.673 m for at least one
// target: a circle and a square 0.4 m across, and a 4 m plank thrown flat,
// face first, 0.2 m thick along its path.
func tunnelMovers() []tunnelMover {
	return []tunnelMover{
		{"a solid circle 0.4 m across", NewCircleShape(0.2, m.Vec2d{})},
		{"a solid box 0.4 m square", NewBoxShape(0.4, 0.4, 0)},
		{"a 4 m × 0.2 m plank", NewBoxShape(0.2, 4, 0)},
	}
}

// wallShape is the swept Sensor test's wall: a segment 4 m tall of radius 0.2,
// 0.4 m thick, standing at the target's place.
func wallShape() Shape {
	return NewSegmentShape(m.Vec2d{Y: -2}, m.Vec2d{Y: 2}, 0.2)
}

func tunnelTargets() []tunnelTarget {
	return []tunnelTarget{
		{"a 0.4 m Static wall", func(t testing.TB, h *harness) ecs.Entity {
			return h.spawn(t, spawnRequest{
				Kind:  kindShapedStatic,
				Place: Position{Current: m.Vec2d{X: targetX}},
				Shape: wallShape(),
			})
		}},
		{"a 0.4 m Kinematic body", func(t testing.TB, h *harness) ecs.Entity {
			return h.spawn(t, spawnRequest{
				Kind:  kindShapedKinematic,
				Place: Position{Current: m.Vec2d{X: targetX}},
				Shape: wallShape(),
			})
		}},
		// A board 0.1 m thick and 4 m tall, at rest, of the same density as the
		// movers: light enough to be knocked aside, which is the case the
		// opt-in Component is for.
		{"a thin Dynamic body", func(t testing.TB, h *harness) ecs.Entity {
			body, shape, _ := solidFor(t, NewBoxShape(0.1, 4, 0))
			return h.spawn(t, spawnRequest{
				Kind:  kindShapedBody,
				Place: Position{Current: m.Vec2d{X: targetX}},
				Body:  body,
				Shape: shape,
			})
		}},
	}
}

// solidFor is the Dynamic an app builds for a solid Shape of unit density,
// recentred, and the Shape's minimum extent.
func solidFor(t testing.TB, shape Shape) (Dynamic, Shape, float64) {
	t.Helper()
	body, shape, _, _, err := NewDynamicForShape(shape, Polygon{}, 1, 0, 0)
	if err != nil {
		t.Fatalf("NewDynamicForShape: %v", err)
	}
	return body, shape, minimumExtent(shape, nil)
}

// minimumExtent is the gate's measure as the map settled it: how thin the Shape
// is along its thinnest direction, taken from the Body's centre. A circle's
// radius; a segment's rounding radius, since a segment has no width of its own;
// a polygon's inner radius — the nearest face to the centre — plus its
// rounding. It is the test's own reading of that definition, not the
// mechanism's.
func minimumExtent(shape Shape, verts []m.Vec2d) float64 {
	switch shape.Kind {
	case ShapeCircle, ShapeSegment:
		return shape.Radius
	}
	if shape.Kind != ShapePoly {
		verts = PolygonVerts(nil, shape, Polygon{})
	}
	inner := math.Inf(1)
	for i, a := range verts {
		b := verts[(i+1)%len(verts)]
		edge := b.Sub(a)
		// The distance from the origin to the edge's line, along its normal.
		inner = min(inner, math.Abs(edge.Cross(a))/edge.Length())
	}
	return inner + shape.Radius
}

// TestASolidBodyDoesNotTunnel is the nine cases: each mover thrown at each
// target at projectile speed from eight phases. A throw tunnels when the mover
// ends on the far side of the target's centre after half a second; it says
// whether it did so clean (no Contact with the target on any tick) or pushed
// through (seen overlapping, and resolved out the far side).
func TestASolidBodyDoesNotTunnel(t *testing.T) {
	for _, target := range tunnelTargets() {
		for _, mover := range tunnelMovers() {
			t.Run(fmt.Sprintf("%s at %s", mover.name, target.name), func(t *testing.T) {
				clean, pushed := 0, 0
				var tunnelled []string
				for phase := range phases {
					outcome, through, touched := throw(t, mover.shape, target, phase)
					if through {
						if touched {
							pushed++
						} else {
							clean++
						}
						tunnelled = append(tunnelled, outcome)
					}
				}
				if clean+pushed > 0 {
					t.Errorf("tunnelled in %d of %d phases (%d clean, %d pushed through):\n%s",
						clean+pushed, phases, clean, pushed, joinLines(tunnelled))
				}
			})
		}
	}
}

// throw runs one phase of one case in an engine of its own, and says where the
// mover ended against the target, whether that is the far side, and whether
// the pair ever reported a Contact.
func throw(t *testing.T, shape Shape, target tunnelTarget, phase int) (string, bool, bool) {
	t.Helper()
	h := newHarness(t)
	wall := target.spawn(t, h)
	body, shape, _ := solidFor(t, shape)
	step := projectileSpeed * tick
	start := targetX - 2 - step*float64(phase)/phases
	mover := h.spawn(t, spawnRequest{
		Kind:     kindShapedBody,
		Place:    Position{Current: m.Vec2d{X: start}},
		Velocity: Velocity{Linear: m.Vec2d{X: projectileSpeed}},
		Body:     body,
		Shape:    shape,
	})

	touched := false
	for range 30 {
		h.frame(t)
		for _, entry := range h.contacts(t) {
			if (entry.A == mover && entry.B == wall) || (entry.A == wall && entry.B == mover) {
				touched = true
			}
		}
	}
	at, there := h.read(t, mover).Place.Current, h.read(t, wall).Place.Current
	through := at.X > there.X
	return fmt.Sprintf("  phase %d: from x = %.3f to %.3f, target at %.3f, touched %v",
		phase, start, at.X, there.X, touched), through, touched
}

func joinLines(lines []string) string {
	out := ""
	for i, line := range lines {
		if i > 0 {
			out += "\n"
		}
		out += line
	}
	return out
}

// TestTheGateCensus records, at the fixed step, which Shape the gate engages
// and from what speed: the minimum extent, the speed at which one tick's
// displacement reaches it (the map's settled gate, ≥ 1×), and the speed at
// which it passes half of it (Box2D v3's and Rapier's, > 0.5×). Nothing is
// asserted but the three numbers' own arithmetic; the log is the record.
func TestTheGateCensus(t *testing.T) {
	type row struct {
		name  string
		shape Shape
		speed float64
	}
	rows := []row{}
	for _, mover := range tunnelMovers() {
		rows = append(rows, row{mover.name + ", thrown", mover.shape, projectileSpeed})
	}
	rows = append(rows,
		row{"the thin Dynamic body, at rest", NewBoxShape(0.1, 4, 0), 0},
		row{"the 0.4 m wall as a Kinematic body, at rest", wallShape(), 0},
		row{"the whole-step bench's circle, at rest", measured(0.4), 0},
		row{"a point Sensor (the swept Sensor's projectile)", sensorCircle(0), projectileSpeed},
		row{"a point, at rest", NewCircleShape(0, m.Vec2d{}), 0},
		row{"a bare segment, at rest", NewSegmentShape(m.Vec2d{X: -1}, m.Vec2d{X: 1}, 0), 0},
	)
	t.Logf("%-50s %8s %10s %10s %8s %8s", "Shape", "extent", "≥1× from", ">½× from", "≥1×", ">½×")
	for _, r := range rows {
		extent := minimumExtent(r.shape, nil)
		moved := r.speed * tick
		t.Logf("%-50s %7.3fm %8.2fm/s %8.2fm/s %8v %8v", r.name, extent,
			extent/tick, 0.5*extent/tick, moved >= extent, moved > 0.5*extent)
	}
}

// gateCounter is a System of the test's own, between Integrate and Index,
// counting the Bodies whose tick displacement would engage the gate at each of
// the two factors. It reads what the mechanism's one compare would read, and
// holds no lock the step's Systems do not already give up between them.
type gateCounter struct {
	polygons  []m.Vec2d
	ticks     atomic.Int64
	bodies    atomic.Int64
	atOne     atomic.Int64
	overHalf  atomic.Int64
	fastest   atomic.Uint64 // math.Float64bits of the largest displacement ÷ extent seen
	pointsSet atomic.Int64
}

type gateCounterQuery struct {
	Place Position
	Shape Shape
	_     ecs.Without[Static]
}

type gateCounterOnUpdate kernel.Subscription[app.UpdateEvent]

func (*gateCounter) Name() kernel.PluginName { return "physicstestgatecounter" }
func (*gateCounter) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, Name}
}

func (g *gateCounter) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[gateCounterOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, func(
		q *ecs.Query[gateCounterQuery],
		polygons *ecs.Get[Polygon],
	) {
		g.ticks.Add(1)
		for entity, it := range q.All() {
			g.bodies.Add(1)
			var verts []m.Vec2d
			if it.Shape.Kind == ShapePoly {
				polygon, _ := polygons.Of(entity)
				g.polygons = PolygonVerts(g.polygons[:0], it.Shape, polygon)
				verts = g.polygons
			}
			extent := minimumExtent(it.Shape, verts)
			moved := it.Place.Current.Distance(it.Place.Previous)
			if extent == 0 {
				g.pointsSet.Add(1)
			}
			if moved >= extent {
				g.atOne.Add(1)
			}
			if moved > 0.5*extent {
				g.overHalf.Add(1)
			}
			if extent > 0 {
				ratio := moved / extent
				if ratio > math.Float64frombits(g.fastest.Load()) {
					g.fastest.Store(math.Float64bits(ratio))
				}
			}
		}
	})).After[IntegrateOnUpdate]().Before[IndexOnUpdate]()
	return nil
}

// TestTheBenchScenesTripNoGate runs both whole-step benchmark scenes as the
// benchmarks run them and counts, every tick after the warm-up, how many Bodies
// the gate would engage. The map's cost bar is that a world where nothing is
// fast pays one compare per Body and nothing more; this is the check that the
// benchmark scenes are such a world, so that pricing the compare against them
// prices the compare and nothing else.
func TestTheBenchScenesTripNoGate(t *testing.T) {
	scenes := []struct {
		name     string
		populate func(testing.TB, *harness, int)
		push     m.Vec2d
		ids      int
	}{
		{"BenchmarkTheStep", populate, m.Vec2d{X: 10}, 2},
		{"BenchmarkThePolygonStep", populatePolygons, m.Vec2d{Y: -9.8}, 4},
	}
	for _, scene := range scenes {
		for _, n := range []int{256, 1024} {
			t.Run(fmt.Sprintf("%s N=%d", scene.name, n), func(t *testing.T) {
				counter := &gateCounter{}
				h := newHarnessWithPlugins(t, nil, uint32(scene.ids*n), counter)
				scene.populate(t, h, n)
				h.game.push = scene.push
				h.frames(t, 100)
				warm := struct{ ticks, bodies, atOne, overHalf int64 }{
					counter.ticks.Load(), counter.bodies.Load(), counter.atOne.Load(), counter.overHalf.Load(),
				}
				h.frames(t, 500)
				ticks := counter.ticks.Load() - warm.ticks
				bodies := counter.bodies.Load() - warm.bodies
				atOne := counter.atOne.Load() - warm.atOne
				overHalf := counter.overHalf.Load() - warm.overHalf
				t.Logf("%d ticks, %.0f shaped non-Static Bodies a tick; engaged per tick: %.3f at ≥1×, %.3f at >½×; "+
					"%d Body-ticks had a zero extent; largest displacement ÷ extent %.4f",
					ticks, float64(bodies)/float64(ticks), float64(atOne)/float64(ticks),
					float64(overHalf)/float64(ticks), counter.pointsSet.Load(),
					math.Float64frombits(counter.fastest.Load()))
				if atOne != 0 || overHalf != 0 {
					t.Errorf("the bench scene engages the gate: %d at ≥1×, %d at >½× over %d ticks", atOne, overHalf, ticks)
				}
			})
		}
	}
}
