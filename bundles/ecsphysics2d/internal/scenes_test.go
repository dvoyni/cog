package internal

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"

	"github.com/dvoyni/cog/libs/m"
)

// Two things no single earlier ticket's tests cover, because neither ticket knew
// about the other's feature, plus the finiteness invariant asserted where it is
// actually stated — over the Components the plugin writes, in a real engine, over
// ticks, rather than over the pure functions underneath.

// TestASweptSensorFindsAPolygonOnItsWayThroughAndNotOnlyWhereItLanded is the
// cross case between two tickets.
//
// The swept Sensor was built when Polygons had no world cache and sat in no index
// cell, so its tests could only aim at circles and segments; the ticket that gave
// Polygons both came after it, and its own tests were about the discrete
// narrowphase. probeWorld does fall through to probePoly, so the arm has been
// there the whole time — but nothing asserted the two features together.
//
// The scene is the tunnelling one: a Sensor fast enough to clear a Polygon whole
// inside one tick, so a discrete test where the tick left it finds nothing at all.
func TestASweptSensorFindsAPolygonOnItsWayThroughAndNotOnlyWhereItLanded(t *testing.T) {
	// A five-vertex Polygon, which is the kind that carries its outline in a
	// Polygon Component beside the Shape rather than in the Shape's own four
	// slots — the kind that had no world cache at all when the swept Sensor was
	// built. Its left face is vertical and spans y = 0, so a Probe along y = 0
	// enters it squarely at x = 0.6 rather than at a vertex where two faces meet.
	pentagon, pentagonVerts, err := NewPolygonShape([]m.Vec2d{
		{X: -0.4, Y: -0.3}, {X: -0.4, Y: 0.3}, {Y: 0.5}, {X: 0.4}, {Y: -0.5},
	}, 0)
	if err != nil {
		t.Fatalf("hulling the pentagon: %v", err)
	}

	h := newHarness(t)
	wall := h.spawn(t, spawnRequest{
		Kind:    kindPolygonStatic,
		Place:   Position{Current: m.Vec2d{X: 1}},
		Shape:   pentagon,
		Polygon: pentagonVerts,
	})
	// A box beyond it, carrying its four vertices inline, so the tick crosses
	// both polygon kinds and the Sensor's ordering by T has two entries to order.
	box := h.spawn(t, spawnRequest{
		Kind:  kindShapedStatic,
		Place: Position{Current: m.Vec2d{X: 2}},
		Shape: NewBoxShape(0.4, 2, 0),
	})
	arrow := h.spawn(t, spawnRequest{
		Kind:     kindShapedBody,
		Place:    Position{Current: m.Vec2d{X: -2.6}},
		Velocity: Velocity{Linear: m.Vec2d{X: 150}},
		Body:     dynamic(t, 1, 1, 0, 0),
		Shape:    sensorCircle(0),
	})

	// One tick at 150 m/s is 2.5 m. The first ends at x = −0.1, short of the
	// pentagon's face at 0.6; the second runs to 2.4 and clears both Polygons
	// whole — the pentagon from 0.6 to 1.4 and the box from 1.8 to 2.2 — which is
	// the tick a discrete test would have found nothing at all on.
	h.frame(t)
	if list := h.contacts(t); len(list) != 0 {
		t.Fatalf("the first tick, which ends short of the pentagon, reported %d Contacts: %+v",
			len(list), list)
	}
	before := h.read(t, arrow).Place.Current
	h.frame(t)
	after := h.read(t, arrow).Place.Current
	if before.X >= 0.6 || after.X <= 2.2 {
		t.Fatalf("the tick ran from %v to %v; it is meant to clear both Polygons whole",
			before.X, after.X)
	}

	list := h.contacts(t)
	if len(list) != 2 {
		t.Fatalf("a Sensor that crossed two Polygons reported %d Contacts, want 2: %+v",
			len(list), list)
	}
	// A Sensor's entries sit together ordered by T, so the hexagon is first.
	for i, want := range []ecs.Entity{wall, box} {
		entry := list[i]
		if entry.A != arrow || entry.B != want {
			t.Fatalf("entry %d names %v and %v, want the Sensor and %v", i, entry.A, entry.B, want)
		}
		if !entry.Sensor || entry.Phase != PhaseBegan {
			t.Errorf("entry %d is Sensor %v and %v, want a Sensor's that Began",
				i, entry.Sensor, entry.Phase)
		}
		if !(entry.T > 0 && entry.T < 1) {
			t.Errorf("entry %d is at T %v, want the fraction of the tick the Polygon was met at",
				i, entry.T)
		}
		if entry.Points[0].Depth != 0 {
			t.Errorf("entry %d has Depth %v, want 0: a Probed Sensor met the Shape rather than "+
				"overlapping it", i, entry.Points[0].Depth)
		}
	}
	if list[0].T >= list[1].T {
		t.Errorf("the hexagon is at T %v and the box at T %v, want the nearer first",
			list[0].T, list[1].T)
	}

	// The pentagon's near face is its vertical left one at x = 0.6 and the box's
	// its left face at x = 1.8. Both are exact, and both are probePoly's own
	// answer — the arm that had nothing to answer for until Polygons gained a
	// world cache.
	wantAt(t, "the pentagon's Point", list[0].Points[0].Point, m.Vec2d{X: 0.6})
	wantAt(t, "the pentagon's Normal", list[0].Normal, m.Vec2d{X: -1})
	wantAt(t, "the box's Point", list[1].Points[0].Point, m.Vec2d{X: 1.8})
	wantAt(t, "the box's Normal", list[1].Normal, m.Vec2d{X: -1})

	// And the whole point of the swept arm: the same scene with the Sensor merely
	// where the tick ended finds neither Polygon.
	discrete := newHarness(t)
	discrete.spawn(t, spawnRequest{
		Kind:    kindPolygonStatic,
		Place:   Position{Current: m.Vec2d{X: 1}},
		Shape:   pentagon,
		Polygon: pentagonVerts,
	})
	discrete.spawn(t, spawnRequest{
		Kind:  kindShapedStatic,
		Place: Position{Current: m.Vec2d{X: 2}},
		Shape: NewBoxShape(0.4, 2, 0),
	})
	discrete.spawn(t, spawnRequest{
		Kind:  kindShapedBody,
		Place: Position{Current: after},
		Body:  dynamic(t, 1, 1, 0, 0),
		Shape: sensorCircle(0),
	})
	discrete.frame(t)
	if list := discrete.contacts(t); len(list) != 0 {
		t.Errorf("a Sensor placed where the tick ended found %d Contacts, want none: %+v",
			len(list), list)
	}
}

// TestNoDegenerateSceneWritesANaNOrAnInfinityIntoAComponent is the finiteness
// invariant where the specification actually states it:
//
//	No input produces a NaN or an infinity in a Component the plugin writes.
//
// The pure functions have their own sweep elsewhere in this package. This is
// the same claim one level up, over the seven degenerate inputs the
// specification names, run through a real engine for long enough that the
// solver's accumulators, the warm start and the Joint pass all have somewhere
// to put a NaN if one is made.
//
// It matters that it is a scene and not a pair test: a NaN entering a Velocity on
// tick one is still a NaN on tick two hundred, and nothing downstream ever
// recovers, so a single degenerate Shape in a corner of a level poisons every
// Body it ever touches. jakecoffman/cp fails this on at least four of the seven
// and Chipmunk asserts on two more.
func TestNoDegenerateSceneWritesANaNOrAnInfinityIntoAComponent(t *testing.T) {
	const ticks = 240

	for _, scene := range degenerateScenes() {
		t.Run(scene.name, func(t *testing.T) {
			h := newHarness(t)
			watched, joints := scene.build(t, h)
			if len(watched) == 0 {
				t.Fatal("the scene published no Body to read back, so it measures nothing")
			}
			h.game.push = scene.push

			contacts, jointImpulses := 0, 0
			for range ticks {
				h.frame(t)
				for _, e := range watched {
					read := h.read(t, e)
					finiteVec(t, "Position", read.Place.Current)
					finiteVec(t, "Velocity", read.Velocity.Linear)
					finiteF(t, "the angular Velocity", read.Velocity.Angular)
					finiteF(t, "Rotation", read.Place.Angle)
					if read.HasForce {
						finiteVec(t, "Force", read.Force.Force)
						finiteF(t, "Torque", read.Force.Torque)
					}
				}
				for _, entry := range h.contacts(t) {
					contacts++
					finiteVec(t, "a Contact's Normal", entry.Normal)
					finiteF(t, "a Contact's T", entry.T)
					for i := range entry.Count {
						finiteVec(t, "a Contact Point", entry.Points[i].Point)
						finiteF(t, "a Contact's Depth", entry.Points[i].Depth)
					}
					finiteVec(t, "a Contact's total Impulse", entry.TotalImpulse())
					finiteF(t, "a Contact's total kinetic energy", entry.TotalKE())
				}
				for _, e := range joints {
					impulse := h.joint(t, e).Joint.Impulse()
					finiteF(t, "a Joint's Impulse", impulse)
					if impulse != 0 {
						jointImpulses++
					}
				}
			}

			t.Logf("%d ticks, %d Contact readings, %d non-zero Joint Impulses", ticks, contacts, jointImpulses)
			// The emptiness guard, in the same spirit as the allocation line's: a
			// scene whose Shapes never met would pass every assertion above while
			// putting nothing at all through the narrowphase or the solver.
			if scene.wantsContacts && contacts == 0 {
				t.Error("the scene produced no Contacts at all, so nothing it asserts went " +
					"through Detect or Solve")
			}
			if scene.wantsJointWork && jointImpulses == 0 {
				t.Error("the scene's Joints delivered no Impulse at any tick, so the Joint pass " +
					"did no work to be finite about")
			}
		})
	}
}

// degenerateScene is one of the specification's named degenerate inputs, built
// as a scene: which Entities to read back, which Joints to read an Impulse off,
// and whether the scene is meant to produce Contacts and Joint work at all.
type degenerateScene struct {
	name           string
	push           m.Vec2d
	wantsContacts  bool
	wantsJointWork bool
	build          func(t testing.TB, h *harness) (watched []ecs.Entity, joints []ecs.Entity)
}

func degenerateScenes() []degenerateScene {
	return []degenerateScene{{
		name:          "two Bodies on exactly coincident centres",
		push:          m.Vec2d{Y: -9.8},
		wantsContacts: true,
		build: func(t testing.TB, h *harness) ([]ecs.Entity, []ecs.Entity) {
			// Exactly the same Position, so the seeded nudge is the only thing
			// that parts them and every number it derives is a division by a
			// distance of zero away from a NaN.
			at := Position{Current: m.Vec2d{X: 2, Y: 2}}
			a := h.spawn(t, spawnRequest{
				Kind: kindShapedBody, Place: at,
				Body: dynamic(t, 1, 1, 0, 0), Shape: circle(0.3),
			})
			b := h.spawn(t, spawnRequest{
				Kind: kindShapedBody, Place: at,
				Body: dynamic(t, 1, 1, 0, 0), Shape: circle(0.3),
			})
			return []ecs.Entity{a, b}, nil
		},
	}, {
		name:          "a Body resting on a zero-length segment",
		push:          m.Vec2d{Y: -9.8},
		wantsContacts: true,
		build: func(t testing.TB, h *harness) ([]ecs.Entity, []ecs.Entity) {
			// NewSegmentShape builds this without complaint, and cp's closestT is
			// then clamp01(0/0). Guarded, the segment is the circle it is.
			point := m.Vec2d{X: 1, Y: 1}
			h.spawn(t, spawnRequest{
				Kind:  kindShapedStatic,
				Place: Position{},
				Shape: NewSegmentShape(point, point, 0.2),
			})
			body := h.spawn(t, spawnRequest{
				Kind:  kindShapedBody,
				Place: Position{Current: m.Vec2d{X: 1, Y: 1.4}},
				Body:  dynamic(t, 1, 1, 0, 0), Shape: circle(0.3),
			})
			return []ecs.Entity{body}, nil
		},
	}, {
		name:          "a Sensor Probing from exactly a circle's centre",
		wantsContacts: true,
		build: func(t testing.TB, h *harness) ([]ecs.Entity, []ecs.Entity) {
			// The Probe's own start is the circle's centre exactly, where cp's
			// point query divides by a distance of zero.
			at := m.Vec2d{X: 4, Y: 0}
			h.spawn(t, spawnRequest{
				Kind:  kindShapedStatic,
				Place: Position{Current: at},
				Shape: circle(0.5),
			})
			sensor := h.spawn(t, spawnRequest{
				Kind:     kindShapedBody,
				Place:    Position{Current: at},
				Velocity: Velocity{Linear: m.Vec2d{X: 0.01}},
				Body:     dynamic(t, 1, 1, 0, 0), Shape: sensorCircle(0.2),
			})
			return []ecs.Entity{sensor}, nil
		},
	}, {
		name:          "two Bodies of infinite mass pressed together",
		wantsContacts: true,
		build: func(t testing.TB, h *harness) ([]ecs.Entity, []ecs.Entity) {
			// The zero Dynamic is infinite mass and infinite moment — both
			// inverses are zero — so the pair has no mass between it and every
			// k in the solver is zero. cp compares INFINITY exactly to classify
			// a Body and the port classifies by Component presence, so this pair
			// is Dynamic and reaches the solver rather than being sorted out of
			// it.
			a := h.spawn(t, spawnRequest{
				Kind:  kindShapedBody,
				Place: Position{Current: m.Vec2d{X: 6}},
				Shape: circle(0.4),
			})
			b := h.spawn(t, spawnRequest{
				Kind:  kindShapedBody,
				Place: Position{Current: m.Vec2d{X: 6.5}},
				Shape: circle(0.4),
			})
			return []ecs.Entity{a, b}, nil
		},
	}, {
		name:           "two Bodies that cannot turn joined by a rotary Spring",
		wantsJointWork: false,
		build: func(t testing.TB, h *harness) ([]ecs.Entity, []ecs.Entity) {
			// C asserts moment != 0 in the rotary spring and the Go port dropped
			// the assert in translation, which leaves all five angular Joints an
			// unguarded Inf * 0 written straight into angular velocity.
			a := h.spawn(t, spawnRequest{
				Kind:  kindDynamic,
				Place: Position{Current: m.Vec2d{X: 8}},
				Body:  dynamic(t, 1, math.Inf(1), 0, 0),
			})
			b := h.spawn(t, spawnRequest{
				Kind:  kindDynamic,
				Place: Position{Current: m.Vec2d{X: 8.5}},
				Body:  dynamic(t, 1, math.Inf(1), 0, 0),
			})
			joint := h.spawn(t, spawnRequest{
				Kind:  kindJoint,
				Joint: NewRotarySpringJoint(a, b, 0.5, 40, 0.3),
			})
			return []ecs.Entity{a, b}, []ecs.Entity{joint}
		},
	}, {
		name:          "two identical boxes exactly on top of each other",
		push:          m.Vec2d{Y: -9.8},
		wantsContacts: true,
		build: func(t testing.TB, h *harness) ([]ecs.Entity, []ecs.Entity) {
			// The degenerate GJK simplex: two Shapes whose world bounding box
			// centres coincide exactly give a zero cold-start axis, every support
			// query answers with the same vertex, and closestTo reads a normal
			// Normalize guarded to zero. The answer is useless, and the claim
			// asserted here is only that it is finite.
			at := Position{Current: m.Vec2d{X: 10, Y: 2}}
			box := NewBoxShape(0.6, 0.6, 0)
			a := h.spawn(t, spawnRequest{
				Kind: kindPolygonBody, Place: at,
				Body: dynamic(t, 1, 0.1, 0, 0), Shape: box,
			})
			b := h.spawn(t, spawnRequest{
				Kind: kindPolygonBody, Place: at,
				Body: dynamic(t, 1, 0.1, 0, 0), Shape: box,
			})
			// A floor, so the pair has somewhere to settle and the scene keeps
			// producing Contacts rather than falling for ever.
			h.spawn(t, spawnRequest{
				Kind:  kindShapedStatic,
				Place: Position{Current: m.Vec2d{X: 10}},
				Shape: NewBoxShapeFor(NewBB(-2, -0.5, 2, 0.5), 0),
			})
			return []ecs.Entity{a, b}, nil
		},
	}, {
		name:           "a Joint anchored at both centres of gravity",
		push:           m.Vec2d{Y: -9.8},
		wantsJointWork: true,
		build: func(t testing.TB, h *harness) ([]ecs.Entity, []ecs.Entity) {
			// Both anchors at the Body's own Position, which is the centre of
			// gravity, so every r cross n in the solver is the zero vector and
			// the angular part of k is exactly zero.
			a := h.spawn(t, spawnRequest{
				Kind:  kindDynamic,
				Place: Position{Current: m.Vec2d{X: 12}},
				Body:  dynamic(t, 1, 1, 0, 0),
			})
			b := h.spawn(t, spawnRequest{
				Kind:  kindDynamic,
				Place: Position{Current: m.Vec2d{X: 12, Y: -0.5}},
				Body:  dynamic(t, 4, 1, 0, 0),
			})
			pivot := h.spawn(t, spawnRequest{
				Kind:  kindJoint,
				Joint: NewPivotJoint(a, b, m.Vec2d{}, m.Vec2d{}),
			})
			pin := h.spawn(t, spawnRequest{
				Kind:  kindJoint,
				Joint: NewPinJoint(a, b, m.Vec2d{}, m.Vec2d{}, 0.5),
			})
			return []ecs.Entity{a, b}, []ecs.Entity{pivot, pin}
		},
	}}
}

func finiteF(t testing.TB, name string, v float64) {
	t.Helper()
	if math.IsNaN(v) || math.IsInf(v, 0) {
		t.Fatalf("%s is %v, and the finiteness invariant forbids it in a Component the plugin writes", name, v)
	}
}

func finiteVec(t testing.TB, name string, v m.Vec2d) {
	t.Helper()
	finiteF(t, name+".X", v.X)
	finiteF(t, name+".Y", v.Y)
}
