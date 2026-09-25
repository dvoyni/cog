package internal

import (
	"fmt"
	"math"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/bundles/ecsphysics2d/internal/types"
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
// The tunnelling cases state the guarantee continuous-collision.md specifies:
// a solid Dynamic body does not end on the far side of any Body it was thrown
// at, Static, Kinematic or Dynamic, and is stopped on the near side, touching
// it. The path pass is what holds it (types/contacts-paths.go).

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
	shape ecsphysics2d.Shape
}

// tunnelTarget is one of the three things they are thrown at.
type tunnelTarget struct {
	name string
	// face is how far the target's near face stands short of its Position
	// along X: half its thickness.
	face float64
	// bodies is whether the target is a Body, in the Body index, which a
	// fast Body's path test queries only when its Shape StopsAtBodies.
	bodies bool
	spawn  func(t testing.TB, h *harness) ecs.Entity
}

// movers are the three solid Shapes, each thin enough along X that the sum of
// its thickness and a target's is under a tick's 0.673 m for at least one
// target: a circle and a square 0.4 m across, and a 4 m plank thrown flat,
// face first, 0.2 m thick along its path.
func tunnelMovers() []tunnelMover {
	return []tunnelMover{
		{"a solid circle 0.4 m across", ecsphysics2d.NewCircleShape(0.2, m.Vec2d{})},
		{"a solid box 0.4 m square", ecsphysics2d.NewBoxShape(0.4, 0.4, 0)},
		{"a 4 m × 0.2 m plank", ecsphysics2d.NewBoxShape(0.2, 4, 0)},
	}
}

// wallShape is the swept Sensor test's wall: a segment 4 m tall of radius 0.2,
// 0.4 m thick, standing at the target's place.
func wallShape() ecsphysics2d.Shape {
	return ecsphysics2d.NewSegmentShape(m.Vec2d{Y: -2}, m.Vec2d{Y: 2}, 0.2)
}

func tunnelTargets() []tunnelTarget {
	return []tunnelTarget{
		{"a 0.4 m Static wall", 0.2, false, func(t testing.TB, h *harness) ecs.Entity {
			return h.spawn(t, spawnRequest{
				Kind:  kindShapedStatic,
				Place: ecsphysics2d.Position{Current: m.Vec2d{X: targetX}},
				Shape: wallShape(),
			})
		}},
		{"a 0.4 m Kinematic body", 0.2, true, func(t testing.TB, h *harness) ecs.Entity {
			return h.spawn(t, spawnRequest{
				Kind:  kindShapedKinematic,
				Place: ecsphysics2d.Position{Current: m.Vec2d{X: targetX}},
				Shape: wallShape(),
			})
		}},
		// A board 0.1 m thick and 4 m tall, at rest, of the same density as the
		// movers: light enough to be knocked aside, which is the case a fast
		// Body's test of the whole Body index is for.
		{"a thin Dynamic body", 0.05, true, func(t testing.TB, h *harness) ecs.Entity {
			body, shape, _ := solidFor(t, ecsphysics2d.NewBoxShape(0.1, 4, 0))
			return h.spawn(t, spawnRequest{
				Kind:  kindShapedBody,
				Place: ecsphysics2d.Position{Current: m.Vec2d{X: targetX}},
				Body:  body,
				Shape: shape,
			})
		}},
	}
}

// solidFor is the Dynamic an app builds for a solid Shape of unit density,
// recentred, and the Shape's minimum extent.
func solidFor(t testing.TB, shape ecsphysics2d.Shape) (ecsphysics2d.Dynamic, ecsphysics2d.Shape, float64) {
	t.Helper()
	body, shape, _, _, err := ecsphysics2d.NewDynamicForShape(shape, ecsphysics2d.Polygon{}, 1, 0, 0)
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
func minimumExtent(shape ecsphysics2d.Shape, verts []m.Vec2d) float64 {
	switch shape.Kind {
	case ecsphysics2d.ShapeCircle, ecsphysics2d.ShapeSegment:
		return shape.Radius
	}
	if shape.Kind != ecsphysics2d.ShapePoly {
		verts = ecsphysics2d.PolygonVerts(nil, shape, ecsphysics2d.Polygon{})
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

// TestEveryPolygonConstructorWritesTheFaceDistance is where the gate's measure
// is kept: a Polygon kind carries the distance from its Position to its nearest
// face's line in the Shape's padding, written by the constructor that built it,
// and it rides through the step in the Shape Component the app spawned. Each
// constructor's Shape is spawned as a Body, recentred as an app spawns one, and
// read back after a tick; what Index would read as its minimum extent has to be
// the reference scene's own reading of the definition, and the Shape has to
// still be 104 bytes.
func TestEveryPolygonConstructorWritesTheFaceDistance(t *testing.T) {
	if got, want := unsafe.Sizeof(ecsphysics2d.Shape{}), uintptr(104); got != want {
		t.Fatalf("Shape is %d bytes, want %d", got, want)
	}

	polygon := func(verts []m.Vec2d, radius float64) (ecsphysics2d.Shape, ecsphysics2d.Polygon) {
		shape, outline, err := ecsphysics2d.NewPolygonShape(verts, radius)
		if err != nil {
			t.Fatalf("NewPolygonShape: %v", err)
		}
		return shape, outline
	}
	type built struct {
		name    string
		shape   ecsphysics2d.Shape
		polygon ecsphysics2d.Polygon
	}
	var cases []built
	add := func(name string, shape ecsphysics2d.Shape, outline ecsphysics2d.Polygon) {
		cases = append(cases, built{name, shape, outline})
	}
	add("a box", ecsphysics2d.NewBoxShape(0.4, 0.4, 0), ecsphysics2d.Polygon{})
	add("the plank", ecsphysics2d.NewBoxShape(0.2, 4, 0), ecsphysics2d.Polygon{})
	add("a rounded box", ecsphysics2d.NewBoxShape(1, 3, 0.25), ecsphysics2d.Polygon{})
	add("an off-centre box", ecsphysics2d.NewBoxShapeFor(ecsphysics2d.NewBB(0.5, -1, 2.5, 0.2), 0), ecsphysics2d.Polygon{})
	shape, outline := polygon([]m.Vec2d{{X: 0, Y: 0}, {X: 3, Y: 0}, {X: 0, Y: 1}}, 0)
	add("a triangle", shape, outline)
	shape, outline = polygon([]m.Vec2d{{X: -1, Y: -0.5}, {X: 2, Y: -0.3}, {X: 1.5, Y: 1}, {X: -0.8, Y: 0.7}}, 0.1)
	add("a quad", shape, outline)
	hexagon := make([]m.Vec2d, 6)
	for i := range hexagon {
		angle := float64(i) * math.Pi / 3
		hexagon[i] = m.Vec2d{X: 1.5 * math.Cos(angle), Y: 0.75 * math.Sin(angle)}
	}
	shape, outline = polygon(hexagon, 0.05)
	add("a Polygon of six", shape, outline)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// As built, before anything recentres it: the constructor's own
			// write, about the Position the vertices were given around.
			if got, want := types.MinimumExtent(c.shape), minimumExtent(c.shape, verticesOf(c.shape, c.polygon)); !closeToFloat32(got, want) {
				t.Errorf("as built, minimum extent %v, want %v", got, want)
			}

			body, shape, outline, _, err := ecsphysics2d.NewDynamicForShape(c.shape, c.polygon, 1, 0, 0)
			if err != nil {
				t.Fatalf("NewDynamicForShape: %v", err)
			}
			h := newHarness(t)
			e := h.spawn(t, spawnRequest{
				Kind:    kindPolygonBody,
				Place:   ecsphysics2d.Position{Current: m.Vec2d{X: 3, Y: -2}},
				Body:    body,
				Shape:   shape,
				Polygon: outline,
			})
			h.frames(t, 2)
			stepped := h.read(t, e)
			want := minimumExtent(stepped.Shape, verticesOf(stepped.Shape, stepped.Polygon))
			if got := types.MinimumExtent(stepped.Shape); !closeToFloat32(got, want) {
				t.Errorf("after the step, minimum extent %v, want %v", got, want)
			}
		})
	}
}

// verticesOf is the local vertex run minimumExtent takes for a ShapePoly: the
// Polygon Component's. Every other kind reads its own slots.
func verticesOf(shape ecsphysics2d.Shape, polygon ecsphysics2d.Polygon) []m.Vec2d {
	if shape.Kind != ecsphysics2d.ShapePoly {
		return nil
	}
	return ecsphysics2d.PolygonVerts(nil, shape, polygon)
}

// closeToFloat32 is equality to within the float32 the face distance is kept
// in, and never above the true value: the kept distance is rounded towards
// zero, so the gate engages no later than 1×.
func closeToFloat32(got, want float64) bool {
	return got <= want && want-got <= 1e-6*max(1, want)
}

// TestASolidBodyDoesNotTunnel is the nine cases: each mover thrown at each
// target at projectile speed from eight phases. A throw tunnels when the mover
// ends on the far side of the target's centre after half a second; it says
// whether it did so clean (no Contact with the target on any tick) or pushed
// through (seen overlapping, and resolved out the far side).
//
// The near-side check is the other half: on the tick the mover first meets
// the target, it was stopped where it met it, so it stands on the near side
// touching it, and the Contact that says so is a stopping one, T < 1 with one
// point at Depth 0. A Body stopped at T whose Contact was then lost would end
// short of the target, which the far-side test alone never sees.
func TestASolidBodyDoesNotTunnel(t *testing.T) {
	for _, target := range tunnelTargets() {
		for _, mover := range tunnelMovers() {
			t.Run(fmt.Sprintf("%s at %s", mover.name, target.name), func(t *testing.T) {
				clean, pushed := 0, 0
				var tunnelled []string
				for phase := range phases {
					outcome, through, touched, met := throwAt(t, mover.shape, target, target.bodies, projectileSpeed, phase, 30)
					if !met.seen {
						t.Errorf("phase %d never met the target:\n%s", phase, outcome)
					} else if !met.stopping {
						t.Errorf("phase %d met the target with no stopping Contact: %s\n%s",
							phase, met.contact, outcome)
					} else if gap := met.gap; gap < -nearSide || gap > nearSide {
						t.Errorf("phase %d stopped %.9f m short of the target's near face, want touching:\n%s",
							phase, gap, outcome)
					}
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

// TestAFastBodyWithoutStopsAtBodiesPassesThroughBodies pins the fall-back's
// other side (continuous-collision.md § The fall-back for a solid Body): a
// mover whose Shape does not StopsAtBodies has its path tested against the
// statics alone, so the Kinematic and the thin Dynamic cases of the nine
// tunnel as they did before continuous collision, in some phase of every one,
// while the Static cases stay green without the flag. Setting the flag, as
// TestASolidBodyDoesNotTunnel does for those six, is what an app throwing a
// Body at Bodies does.
func TestAFastBodyWithoutStopsAtBodiesPassesThroughBodies(t *testing.T) {
	for _, target := range tunnelTargets() {
		if !target.bodies {
			continue
		}
		for _, mover := range tunnelMovers() {
			t.Run(fmt.Sprintf("%s at %s", mover.name, target.name), func(t *testing.T) {
				tunnelled := 0
				for phase := range phases {
					_, through, _, met := throwAt(t, mover.shape, target, false, projectileSpeed, phase, 30)
					if met.stopping {
						t.Errorf("phase %d was stopped at the target with no StopsAtBodies: %s", phase, met.contact)
					}
					if through {
						tunnelled++
					}
				}
				if tunnelled == 0 {
					t.Errorf("tunnelled in none of %d phases with no StopsAtBodies, want some", phases)
				}
				t.Logf("tunnelled in %d of %d phases", tunnelled, phases)
			})
		}
	}
}

// nearSide is how near the target's face a stopped mover has to stand to count
// as touching it: the swept convex test stops within a nanometre of the
// surface, and the circle's Probe is exact.
const nearSide = 1e-6

// meeting is what the tick the mover first met the target showed: whether it
// ever did, whether the Contact was a stopping one, and how far the mover's
// leading face then stood short of the target's near face.
type meeting struct {
	seen, stopping bool
	gap            float64
	contact        string
}

// throwAt runs one phase of one throw in an engine of its own, at speed for the
// given number of ticks, from two metres short of the target and a phase's
// share of one tick's travel further back, the mover's Shape carrying
// StopsAtBodies as asked. It says where the mover ended against the target,
// whether that is the far side, whether the pair ever reported a Contact, and
// what the first tick they met showed.
func throwAt(
	t *testing.T, shape ecsphysics2d.Shape, target tunnelTarget, stopsAtBodies bool, speed float64, phase, ticks int,
) (string, bool, bool, meeting) {
	t.Helper()
	h := newHarness(t)
	wall := target.spawn(t, h)
	body, shape, reach := solidFor(t, shape)
	shape.StopsAtBodies = stopsAtBodies
	step := speed * tick
	start := targetX - 2 - step*float64(phase)/phases
	mover := h.spawn(t, spawnRequest{
		Kind:     kindShapedBody,
		Place:    ecsphysics2d.Position{Current: m.Vec2d{X: start}},
		Velocity: ecsphysics2d.Velocity{Linear: m.Vec2d{X: speed}},
		Body:     body,
		Shape:    shape,
	})

	touched := false
	var met meeting
	for range ticks {
		h.frame(t)
		for _, entry := range h.contacts(t) {
			if (entry.A == mover && entry.B == wall) || (entry.A == wall && entry.B == mover) {
				if !touched {
					at, there := h.read(t, mover).Place.Current, h.read(t, wall).Place.Current
					met = meeting{
						seen: true,
						stopping: entry.T < 1 && !entry.Sensor && entry.Count == 1 &&
							entry.Points[0].Depth == 0,
						gap: (there.X - target.face) - (at.X + reach),
						contact: fmt.Sprintf("T %.4f, Sensor %v, %d point(s), Depth %.4f",
							entry.T, entry.Sensor, entry.Count, entry.Points[0].Depth),
					}
				}
				touched = true
			}
		}
	}
	at, there := h.read(t, mover).Place.Current, h.read(t, wall).Place.Current
	through := at.X > there.X
	return fmt.Sprintf("  phase %d: from x = %.3f to %.3f, target at %.3f, touched %v",
		phase, start, at.X, there.X, touched), through, touched, met
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
		shape ecsphysics2d.Shape
		speed float64
	}
	rows := []row{}
	for _, mover := range tunnelMovers() {
		rows = append(rows, row{mover.name + ", thrown", mover.shape, projectileSpeed})
	}
	rows = append(rows,
		row{"the thin Dynamic body, at rest", ecsphysics2d.NewBoxShape(0.1, 4, 0), 0},
		row{"the 0.4 m wall as a Kinematic body, at rest", wallShape(), 0},
		row{"the whole-step bench's circle, at rest", measured(0.4), 0},
		row{"a point Sensor (the swept Sensor's projectile)", sensorCircle(0), projectileSpeed},
		row{"a point, at rest", ecsphysics2d.NewCircleShape(0, m.Vec2d{}), 0},
		row{"a bare segment, at rest", ecsphysics2d.NewSegmentShape(m.Vec2d{X: -1}, m.Vec2d{X: 1}, 0), 0},
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
	Place ecsphysics2d.Position
	Shape ecsphysics2d.Shape
	_     ecs.Without[ecsphysics2d.Static]
}

type gateCounterOnUpdate kernel.Subscription[app.UpdateEvent]

func (*gateCounter) Name() kernel.PluginName { return "physicstestgatecounter" }
func (*gateCounter) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, ecsphysics2d.Name}
}

func (g *gateCounter) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[gateCounterOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, func(
		q *ecs.Query[gateCounterQuery],
		polygons *ecs.Get[ecsphysics2d.Polygon],
	) {
		g.ticks.Add(1)
		for entity, it := range q.All() {
			g.bodies.Add(1)
			var verts []m.Vec2d
			if it.Shape.Kind == ecsphysics2d.ShapePoly {
				polygon, _ := polygons.Of(entity)
				g.polygons = ecsphysics2d.PolygonVerts(g.polygons[:0], it.Shape, polygon)
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
	})).After[ecsphysics2d.IntegrateOnUpdate]().Before[ecsphysics2d.IndexOnUpdate]()
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

// sweepFactors are the speed sweep's travels in one tick, as multiples of the
// mover's minimum extent: one below the gate, where the discrete walk has to
// hold, and the rest at or past it, where the path test has to.
var sweepFactors = []float64{0.5, 1, 1.25, 1.5, 2, 2.5, 3, 4, 5}

// TestNothingTunnelsAcrossTheSpeedSweep pins the ≥ 1× gate from both sides:
// every mover thrown at every target, from eight phases, at each travel of the
// sweep, and none may end on the far side. Below 1× nothing is marked and the
// discrete walk alone keeps it on the near side; from 1× the path test does.
func TestNothingTunnelsAcrossTheSpeedSweep(t *testing.T) {
	for _, target := range tunnelTargets() {
		for _, mover := range tunnelMovers() {
			_, _, extent := solidFor(t, mover.shape)
			for _, factor := range sweepFactors {
				speed := factor * extent / tick
				// Long enough to cover the two metres and some, at the slowest.
				ticks := int(math.Ceil(3/(speed*tick))) + 5
				t.Run(fmt.Sprintf("%s at %s, %gx", mover.name, target.name, factor), func(t *testing.T) {
					var tunnelled []string
					for phase := range phases {
						outcome, through, _, _ := throwAt(t, mover.shape, target, target.bodies, speed, phase, ticks)
						if through {
							tunnelled = append(tunnelled, outcome)
						}
					}
					if len(tunnelled) > 0 {
						t.Errorf("tunnelled in %d of %d phases at %.2f m a tick:\n%s",
							len(tunnelled), phases, speed*tick, joinLines(tunnelled))
					}
				})
			}
		}
	}
}
