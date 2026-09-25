package internal

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/libs/m"
)

// Every moving Sensor is swept, and a fast Body reports the Sensors it crosses
// (continuous-collision.md § Every moving Sensor is swept, § Detect › A fast
// Body reports the Sensors it crosses, › Which walk writes a pair). Each case
// is thrown from eight starting points, the scene shifted by an eighth of a
// tick's travel each time, so where a tick ends against the target falls
// everywhere a discrete test could miss it.

// boardFace is where the thin board every case crosses begins: 0.1 m thick,
// standing at targetX.
const boardFace = targetX - 0.05

// board is the thin target: a box 0.1 m thick along the path and 4 m across
// it, as a Static, a Sensor or not.
func board(sensor bool) ecsphysics2d.Shape {
	shape := ecsphysics2d.NewBoxShape(0.1, 4, 0)
	shape.Sensor = sensor
	return shape
}

// crossing is what one throw across the board showed: the current entries for
// the pair, tick by tick, and where the mover stood on each side of the tick they
// first met in.
type crossing struct {
	ticks         [][]ecsphysics2d.Contact
	first         int
	before, after m.Vec2d
	speedAfter    m.Vec2d
}

// crossBoard throws the mover along +X at the target from two metres short,
// less an eighth of a tick's travel for each phase, and keeps every tick's
// entries for the pair.
func crossBoard(t *testing.T, h *harness, mover ecs.Entity, target ecs.Entity, ticks int) crossing {
	t.Helper()
	seen := crossing{first: -1}
	for tick := range ticks {
		before := h.read(t, mover).Place.Current
		h.frame(t)
		// An Ended entry is the tick before's, reported once more as it ends.
		var entries []ecsphysics2d.Contact
		for _, entry := range meetingOf(h.contacts(t), mover, target) {
			if entry.Phase != ecsphysics2d.PhaseEnded {
				entries = append(entries, entry)
			}
		}
		seen.ticks = append(seen.ticks, entries)
		if len(entries) > 0 && seen.first < 0 {
			seen.first = tick
			read := h.read(t, mover)
			seen.before, seen.after, seen.speedAfter = before, read.Place.Current, read.Velocity.Linear
		}
	}
	return seen
}

// check asserts the crossing the specification asks of a Sensor: reported, in
// the tick the pair first met, once, by an entry whose A is the Sensor, with
// T < 1 and Depth 0 at the point the leading face met the board's; any later
// tick the mover began inside the board reports T = 0.
func (seen crossing) check(t *testing.T, phase int, sensor ecs.Entity, lead float64) {
	t.Helper()
	if seen.first < 0 {
		t.Fatalf("phase %d: the pair was never reported", phase)
	}
	for tick, entries := range seen.ticks {
		if len(entries) > 1 {
			t.Errorf("phase %d, tick %d: the pair is %d entries, want one", phase, tick, len(entries))
		}
	}
	entry := seen.ticks[seen.first][0]
	if entry.A != sensor || !entry.Sensor {
		t.Errorf("phase %d: the entry's A is %v and Sensor %v, want the Sensor %v and true",
			phase, entry.A, entry.Sensor, sensor)
	}
	if !(entry.T > 0 && entry.T < 1) || entry.Points[0].Depth != 0 {
		t.Errorf("phase %d: the entry has T %.9f and Depth %.9f, want T inside the tick and Depth 0",
			phase, entry.T, entry.Points[0].Depth)
	}
	if met := seen.before.X + (seen.after.X-seen.before.X)*entry.T + lead; math.Abs(met-boardFace) > 1e-6 {
		t.Errorf("phase %d: at T the leading face stood at x = %.9f, want the board's face at %.9f",
			phase, met, boardFace)
	}
	for tick := seen.first + 1; tick < len(seen.ticks); tick++ {
		for _, later := range seen.ticks[tick] {
			if later.T != 0 {
				t.Errorf("phase %d, tick %d: a later entry has T %.9f, want 0 for a mover that began inside",
					phase, tick, later.T)
			}
		}
	}
}

func TestAFastBoxSensorAndAFastSegmentSensorReportAThinTargetOnce(t *testing.T) {
	box := ecsphysics2d.NewBoxShape(0.4, 0.4, 0)
	box.Sensor = true
	segment := ecsphysics2d.NewSegmentShape(m.Vec2d{Y: -0.2}, m.Vec2d{Y: 0.2}, 0)
	segment.Sensor = true

	for _, sensor := range []struct {
		name  string
		shape ecsphysics2d.Shape
		lead  float64
	}{{"box", box, 0.2}, {"segment", segment, 0}} {
		t.Run(sensor.name, func(t *testing.T) {
			for phase := range phases {
				h := newHarness(t)
				target := h.spawn(t, spawnRequest{
					Kind:  kindShapedStatic,
					Place: ecsphysics2d.Position{Current: m.Vec2d{X: targetX}},
					Shape: board(false),
				})
				start := targetX - 2 - projectileSpeed*tick*float64(phase)/phases
				thrown := h.spawn(t, spawnRequest{
					Kind:     kindShapedBody,
					Place:    ecsphysics2d.Position{Current: m.Vec2d{X: start}},
					Velocity: ecsphysics2d.Velocity{Linear: m.Vec2d{X: projectileSpeed}},
					Body:     dynamic(t, 1, 1, 0, 0),
					Shape:    sensor.shape,
				})
				crossBoard(t, h, thrown, target, 6).check(t, phase, thrown, sensor.lead)
			}
		})
	}
}

// A resting Sensor that is a Body is in the Body index, which a fast Body's path
// test queries only when its Shape StopsAtBodies, so the thrown Body carries
// the flag for that one.
func TestAFastSolidBodyReportsASensorThatDidNotMoveAndIsNotStopped(t *testing.T) {
	resting := []struct {
		name   string
		bodies bool
		spawn  func(t testing.TB, h *harness) ecs.Entity
	}{{"a Static Sensor", false, func(t testing.TB, h *harness) ecs.Entity {
		return h.spawn(t, spawnRequest{
			Kind:  kindShapedStatic,
			Place: ecsphysics2d.Position{Current: m.Vec2d{X: targetX}},
			Shape: board(true),
		})
	}}, {"a Sensor Body at rest", true, func(t testing.TB, h *harness) ecs.Entity {
		return h.spawn(t, spawnRequest{
			Kind:  kindShapedBody,
			Place: ecsphysics2d.Position{Current: m.Vec2d{X: targetX}},
			Body:  dynamic(t, 1, 1, 0, 0),
			Shape: board(true),
		})
	}}}

	for _, target := range resting {
		for _, mover := range tunnelMovers()[:2] {
			t.Run(target.name+"/"+mover.name, func(t *testing.T) {
				for phase := range phases {
					h := newHarness(t)
					sensor := target.spawn(t, h)
					body, shape, reach := solidFor(t, mover.shape)
					shape.StopsAtBodies = target.bodies
					start := targetX - 2 - projectileSpeed*tick*float64(phase)/phases
					thrown := h.spawn(t, spawnRequest{
						Kind:     kindShapedBody,
						Place:    ecsphysics2d.Position{Current: m.Vec2d{X: start}},
						Velocity: ecsphysics2d.Velocity{Linear: m.Vec2d{X: projectileSpeed}},
						Body:     body,
						Shape:    shape,
					})
					seen := crossBoard(t, h, thrown, sensor, 6)
					seen.check(t, phase, sensor, reach)

					// Not stopped: the tick it crossed the Sensor in ran its whole
					// travel, and nothing touched its speed.
					if got, want := seen.after.X-seen.before.X, projectileSpeed*tick; math.Abs(got-want) > 1e-9 {
						t.Errorf("phase %d: the Body travelled %.9f in the tick it crossed the Sensor, want %.9f",
							phase, got, want)
					}
					if got := seen.speedAfter; got != (m.Vec2d{X: projectileSpeed}) {
						t.Errorf("phase %d: the Body's velocity became %v", phase, got)
					}
				}
			})
		}
	}
}

func TestTwoMovingSensorsCrossingAreOneEntryWithOneT(t *testing.T) {
	box := ecsphysics2d.NewBoxShape(0.4, 0.4, 0)
	box.Sensor = true

	cases := []struct {
		name         string
		shape        ecsphysics2d.Shape
		fromA, fromB m.Vec2d
		velA, velB   m.Vec2d
		t            float64
	}{{
		// Head-on, A going x 0 → 10 and B 10 → 0: they close by 20 m in the
		// tick from 9.6 m apart. Each reaches where the other ended long
		// before it could have met it there.
		name:  "two boxes head-on",
		shape: box,
		fromA: m.Vec2d{}, fromB: m.Vec2d{X: 10},
		velA: m.Vec2d{X: meetSpeed}, velB: m.Vec2d{X: -meetSpeed},
		t: 9.6 / 20,
	}, {
		// The path box's own case: A going along X and B along Y, both to
		// the origin, each ending where the other's path never reaches.
		name:  "two balls crossing at right angles",
		shape: sensorCircle(1),
		fromA: m.Vec2d{X: -10}, fromB: m.Vec2d{Y: -10},
		velA: m.Vec2d{X: 2 * meetSpeed}, velB: m.Vec2d{Y: 2 * meetSpeed},
		t: (10 - math.Sqrt2) / 20,
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for phase := range phases {
				h := newHarness(t)
				shift := m.Vec2d{X: 10 * float64(phase) / phases, Y: 3.7 * float64(phase) / phases}
				a := h.spawn(t, spawnRequest{
					Kind:     kindShapedBody,
					Place:    ecsphysics2d.Position{Current: c.fromA.Add(shift)},
					Velocity: ecsphysics2d.Velocity{Linear: c.velA},
					Body:     dynamic(t, 1, 1, 0, 0),
					Shape:    c.shape,
				})
				b := h.spawn(t, spawnRequest{
					Kind:     kindShapedBody,
					Place:    ecsphysics2d.Position{Current: c.fromB.Add(shift)},
					Velocity: ecsphysics2d.Velocity{Linear: c.velB},
					Body:     dynamic(t, 1, 1, 0, 0),
					Shape:    c.shape,
				})
				h.frame(t)

				entries := meetingOf(h.contacts(t), a, b)
				if len(entries) != 1 {
					t.Fatalf("phase %d: the pair is %d entries, want one", phase, len(entries))
				}
				entry := entries[0]
				if entry.A != min(a, b) || !entry.Sensor || entry.Points[0].Depth != 0 {
					t.Errorf("phase %d: the entry has A %v, Sensor %v and Depth %.9f, want the lower Entity %v, true and 0",
						phase, entry.A, entry.Sensor, entry.Points[0].Depth, min(a, b))
				}
				if math.Abs(entry.T-c.t) > 1e-6 {
					t.Errorf("phase %d: they met at T %.9f, want %.9f", phase, entry.T, c.t)
				}
			}
		})
	}
}
