package internal

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/libs/m"
)

// A fast Kinematic body reports every resting Sensor on its path
// (continuous-collision.md § A fast Body reports the Sensors it crosses, § The
// Hits past the stop). Detect cannot tell a Kinematic mover from a Dynamic
// one, so it holds the Sensors a fast solid Body crosses past its stop, and
// Solve, which reads Dynamic, writes a Kinematic mover's and drops a Dynamic
// mover's, which never got there.

// sensorX is where the resting Sensor behind the paddle's first Hit stands:
// the thin board, its face towards the paddle at 3.05. A paddle 1 m from its
// centre to its leading face, going 10 → 0, meets it at T = 0.595.
const (
	sensorX = 3.0
	sensorT = (10 - 1 - (sensorX + 0.05)) / 10
)

// reportsSensor is the check a paddle's tick makes of the Sensor behind its
// first Hit: one entry, the Sensor as A, at the paddle's T with Depth 0.
func reportsSensor(sensor *ecs.Entity) func(*testing.T, int, []ecsphysics2d.Contact, ecs.Entity, float64) {
	return func(t *testing.T, phase int, list []ecsphysics2d.Contact, k ecs.Entity, tolerance float64) {
		t.Helper()
		entries := meetingOf(list, *sensor, k)
		if len(entries) != 1 {
			t.Fatalf("phase %d: the Sensor is %d entries with the paddle, want one", phase, len(entries))
		}
		entry := entries[0]
		if entry.A != *sensor || !entry.Sensor || entry.Count != 1 || entry.Points[0].Depth != 0 ||
			math.Abs(entry.T-sensorT) > tolerance {
			t.Errorf("phase %d: the Sensor's entry has A %v, Sensor %v, %d point(s), Depth %.6f, T %.9f; "+
				"want the Sensor %v, true, one point, Depth 0 and T %.3f",
				phase, entry.A, entry.Sensor, entry.Count, entry.Points[0].Depth, entry.T, *sensor, sensorT)
		}
	}
}

// The ticket's case: the paddle meets a ball resting at x = 7 at T = 0.1 and
// carries it to −2, and then sweeps across a resting Sensor at x = 3. Cut at
// the paddle's first Hit, as a Dynamic mover's crossings are, the Sensor was
// never reported.
func TestAFastKinematicPaddleReportsTheSensorBehindTheBallItCarries(t *testing.T) {
	var sensor ecs.Entity
	checkCarriedAll(t, []carriedBall{
		{from: m.Vec2d{X: 7}, t: 0.1, at: m.Vec2d{X: -2}},
	}, func(h *harness, shift m.Vec2d) {
		sensor = h.spawn(t, spawnRequest{
			Kind:  kindShapedStatic,
			Place: ecsphysics2d.Position{Current: m.Vec2d{X: sensorX}.Add(shift)},
			Shape: board(true),
		})
	}, reportsSensor(&sensor))
}

// A Static wall at x = 7 is the paddle's first Hit, and a Sensor Body resting
// at x = 3 behind it is reported all the same.
func TestAFastKinematicPaddleReportsTheSensorBehindAWall(t *testing.T) {
	var sensor ecs.Entity
	checkCarriedAll(t, nil, func(h *harness, shift m.Vec2d) {
		h.spawn(t, spawnRequest{
			Kind:  kindShapedStatic,
			Place: ecsphysics2d.Position{Current: m.Vec2d{X: 7}.Add(shift)},
			Shape: wallShape(),
		})
		sensor = h.spawn(t, spawnRequest{
			Kind:  kindShapedBody,
			Place: ecsphysics2d.Position{Current: m.Vec2d{X: sensorX}.Add(shift)},
			Body:  dynamic(t, 1, 1, 0, 0),
			Shape: board(true),
		})
	}, reportsSensor(&sensor))
}

// A Dynamic mover is unchanged: a fast ball going 10 → 0 is stopped by the
// wall at x = 7 and never reaches the Sensor at x = 3, so no entry names the
// pair, neither in the list a filter System reads nor in the one a reacting
// System reads.
func TestAFastDynamicBallStoppedByAWallReportsNoSensorBehindIt(t *testing.T) {
	for _, shape := range carryShapes {
		t.Run(shape.name, func(t *testing.T) {
			for phase := range phases {
				h := newHarness(t)
				shift := m.Vec2d{X: 10 * float64(phase) / phases, Y: 3.7 * float64(phase) / phases}
				wall := h.spawn(t, spawnRequest{
					Kind:  kindShapedStatic,
					Place: ecsphysics2d.Position{Current: m.Vec2d{X: 7}.Add(shift)},
					Shape: wallShape(),
				})
				sensor := h.spawn(t, spawnRequest{
					Kind:  kindShapedStatic,
					Place: ecsphysics2d.Position{Current: m.Vec2d{X: sensorX}.Add(shift)},
					Shape: board(true),
				})
				ball := thrown(t, h, shape.shape, m.Vec2d{X: 10}.Add(shift), m.Vec2d{X: -meetSpeed})
				filtered := false
				h.game.filter = func(entry *ecsphysics2d.Contact) {
					if entry.Other(sensor) == ball {
						filtered = true
					}
				}
				h.frame(t)
				if filtered {
					t.Errorf("phase %d: a filter System saw the Sensor behind the wall", phase)
				}
				list := h.contacts(t)
				if stop, found := between(list, ball, wall); !found || !(stop.T < 1) {
					t.Errorf("phase %d: the wall is no stop: found %v, T %.9f", phase, found, stop.T)
				}
				if entry, found := between(list, ball, sensor); found {
					t.Errorf("phase %d: the Sensor behind the wall reports an entry at T %.6f", phase, entry.T)
				}
			}
		})
	}
}

// A Sensor's entries, where it is A, sit together, ordered by T, the one
// Solve writes among them. A tall Sensor stands at x = 3; the paddle crosses
// it past its first Hit, the ball at 7, at T = 0.595. Two fast Dynamic balls
// in lanes of their own cross it too, stopped by nothing, so Detect writes
// theirs: one from 8, at T = 0.395, and one from 12, going 10.5 m in the
// tick, at T = 0.757. The paddle's entry goes between the two.
func TestAKinematicMoversSensorEntrySitsWithTheSensorsOthersInOrderOfT(t *testing.T) {
	tall := ecsphysics2d.NewBoxShape(0.1, 20, 0)
	tall.Sensor = true
	for _, shape := range carryShapes {
		t.Run(shape.name, func(t *testing.T) {
			for phase := range phases {
				h := newHarness(t)
				shift := m.Vec2d{X: 10 * float64(phase) / phases, Y: 3.7 * float64(phase) / phases}
				sensor := h.spawn(t, spawnRequest{
					Kind:  kindShapedStatic,
					Place: ecsphysics2d.Position{Current: m.Vec2d{X: sensorX}.Add(shift)},
					Shape: tall,
				})
				early := thrown(t, h, shape.shape, m.Vec2d{X: 8, Y: 4}.Add(shift), m.Vec2d{X: -meetSpeed})
				late := thrown(t, h, shape.shape, m.Vec2d{X: 12, Y: -4}.Add(shift), m.Vec2d{X: -1.05 * meetSpeed})
				thrown(t, h, shape.shape, m.Vec2d{X: 7}.Add(shift), m.Vec2d{})
				k := paddle(t, h, shape.shape, m.Vec2d{X: 10}.Add(shift), m.Vec2d{X: -meetSpeed})
				h.frame(t)

				want := []struct {
					b ecs.Entity
					t float64
				}{{early, (8 - 1 - 3.05) / 10}, {k, sensorT}, {late, (12 - 1 - 3.05) / 10.5}}
				var run []ecsphysics2d.Contact
				start := -1
				for i, entry := range h.contacts(t) {
					if entry.A != sensor {
						continue
					}
					if start >= 0 && i != start+len(run) {
						t.Fatalf("phase %d: the Sensor's entries do not sit together: %d after %d of them from %d",
							phase, i, len(run), start)
					}
					if start < 0 {
						start = i
					}
					run = append(run, entry)
				}
				if len(run) != len(want) {
					t.Fatalf("phase %d: the Sensor is A on %d entries, want %d", phase, len(run), len(want))
				}
				for i, w := range want {
					if entry := run[i]; entry.B != w.b || math.Abs(entry.T-w.t) > shape.tolerance {
						t.Errorf("phase %d: the Sensor's entry %d names %v at T %.9f, want %v at T %.3f",
							phase, i, entry.B, entry.T, w.b, w.t)
					}
				}
			}
		})
	}
}
