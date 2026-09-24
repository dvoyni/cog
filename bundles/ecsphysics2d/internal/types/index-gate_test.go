package types

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// Continuous collision's gate, as Index sets it: the path bit on a solid Body
// whose movement in the tick reaches its minimum extent, and where its path
// starts. Nothing in Detect reads the bit for a solid Body yet, so the only
// place it shows is the entry itself; the behaviour it gates is tested through
// the step by the tickets that give it one.

func TestTheGateMarksASolidBodyThatMovesItsOwnMinimumExtent(t *testing.T) {
	box := NewBoxShape(0.4, 1, 0.05) // nearest face 0.2, so a minimum extent of 0.25
	point := NewCircleShape(0, m.Vec2d{})
	bareSegment := NewSegmentShape(m.Vec2d{X: -1}, m.Vec2d{X: 1}, 0)
	bareLiteral := Shape{Kind: ShapeQuad, CollisionBits: CollisionBitsAll, CollidesWith: CollisionBitsAll}
	bareLiteral.verts = box.verts

	cases := []struct {
		name  string
		shape Shape
		moved float64
		want  bool
	}{
		{"a circle at rest", NewCircleShape(0.2, m.Vec2d{}), 0, false},
		{"a circle moving just under its radius", NewCircleShape(0.2, m.Vec2d{}), 0.199, false},
		{"a circle moving its radius", NewCircleShape(0.2, m.Vec2d{}), 0.2, true},
		{"a capsule moving just under its rounding", NewSegmentShape(m.Vec2d{Y: -2}, m.Vec2d{Y: 2}, 0.2), 0.199, false},
		{"a capsule moving its rounding", NewSegmentShape(m.Vec2d{Y: -2}, m.Vec2d{Y: 2}, 0.2), 0.2, true},
		{"a box moving under its face and rounding", box, 0.249, false},
		{"a box moving its face and rounding", box, 0.25, true},
		{"the plank moving under its thin side", NewBoxShape(0.2, 4, 0), 0.099, false},
		{"the plank moving its thin side", NewBoxShape(0.2, 4, 0), 0.1, true},
		{"a point at rest", point, 0, false},
		{"a point moving at all", point, 1e-9, true},
		{"a bare segment at rest", bareSegment, 0, false},
		{"a bare segment moving at all", bareSegment, 1e-9, true},
		{"a bare Polygon literal at rest", bareLiteral, 0, false},
		{"a bare Polygon literal moving at all", bareLiteral, 1e-9, true},
		{"a Body placed at a NaN", NewCircleShape(0.2, m.Vec2d{}), math.NaN(), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			idx := NewBodyIndex(2)
			// Along X from the origin, so the movement is exactly the one named.
			previous, at := m.Vec2d{}, m.Vec2d{X: c.moved}
			idx.InsertMoving(testEntity(0), c.shape, at, previous, 0.3, 0.1, nil)
			if len(idx.entries) != 1 {
				t.Fatalf("the index holds %d entries, want 1", len(idx.entries))
			}
			if got := idx.entries[0].path; got != c.want {
				t.Fatalf("path bit %v, want %v", got, c.want)
			}
		})
	}
}

func TestTheGateRecordsWhereASolidBodysPathStarts(t *testing.T) {
	at, previous := m.Vec2d{X: 4, Y: -1}, m.Vec2d{X: 1, Y: -2}
	angle, previousAngle := 0.7, -0.4

	// A circle's path is its centre's chord, held at the end angle: its centre
	// now, translated by Previous − Current, whatever angle it started at.
	offset := m.Vec2d{X: 0.5, Y: 0.25}
	idx := NewBodyIndex(2)
	idx.InsertMoving(testEntity(0), NewCircleShape(0.2, offset), at, previous, angle, previousAngle, nil)
	centreNow := NewTransformRigid(at, angle).Point(offset)
	want := centreNow.Add(previous.Sub(at))
	if e := idx.entries[0]; !e.path || !nearVec(e.previousCentre, want) {
		t.Errorf("a circle's path starts at %v (marked %v), want %v", e.previousCentre, e.path, want)
	}

	// Any other Shape's path is its Position's chord, held at the end angle.
	idx.Clear()
	idx.InsertMoving(testEntity(0), NewBoxShape(0.4, 0.4, 0), at, previous, angle, previousAngle, nil)
	if e := idx.entries[0]; !e.path || e.previousCentre != previous {
		t.Errorf("a box's path starts at %v (marked %v), want %v", e.previousCentre, e.path, previous)
	}
}

func TestAMovingSensorIsStillMarkedWithoutAGate(t *testing.T) {
	sensor := NewCircleShape(0.5, m.Vec2d{})
	sensor.Sensor = true
	idx := NewBodyIndex(2)
	// Far under its radius, which would not pass the gate: a Sensor has none.
	idx.InsertMoving(testEntity(0), sensor, m.Vec2d{X: 0.01}, m.Vec2d{}, 0, 0, nil)
	if e := idx.entries[0]; !e.path || e.previousCentre != (m.Vec2d{}) {
		t.Errorf("a moving circle Sensor: marked %v from %v, want marked from the origin", e.path, e.previousCentre)
	}
	idx.Clear()
	idx.InsertMoving(testEntity(0), sensor, m.Vec2d{X: 1}, m.Vec2d{X: 1}, 0, 0, nil)
	if idx.entries[0].path {
		t.Error("a Sensor that did not move was marked")
	}
}

func nearVec(a, b m.Vec2d) bool { return a.Distance(b) <= 1e-12 }
