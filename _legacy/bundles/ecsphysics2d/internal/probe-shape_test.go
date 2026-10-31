package internal

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// roundedTolerance is how near a Probed Shape's T lands on a rounded feature,
// which the advance approaches from before the touch rather than landing on.
const roundedTolerance = 1e-8

func nearWithin(got, want, within float64) bool { return math.Abs(got-want) <= within }

func TestProbingABoxFaceOnIntoABoxMeetsItWhereTheFacesMeet(t *testing.T) {
	mover := NewBoxShape(1, 1, 0)
	target := NewBoxShape(2, 2, 0)

	hit, ok := ProbeShapeWith(mover, origin, m.Vec2d{X: 10}, 0, nil, target, m.Vec2d{X: 5}, 0, nil)
	if !ok {
		t.Fatal("a box Probed straight into a box met nothing")
	}
	if !nearF(hit.T, 0.35) {
		t.Errorf("T = %v, want 0.35, the leading face at x = 4", hit.T)
	}
	if !nearF(hit.Point.X, 4) || hit.Point.Y < -0.5-tolerance || hit.Point.Y > 0.5+tolerance {
		t.Errorf("Point = %v, want on the target's face x = 4 where the two faces meet", hit.Point)
	}
	if want := (m.Vec2d{X: -1}); !vecNear(hit.Normal, want) {
		t.Errorf("Normal = %v, want %v, facing the mover", hit.Normal, want)
	}
	if hit.Entity != 0 {
		t.Errorf("Entity = %v, want zero from a pair primitive", hit.Entity)
	}
}

func TestAProbedShapeMeetsACircleASegmentAndABox(t *testing.T) {
	movers := []struct {
		name  string
		shape Shape
		// lead is how far ahead of its Position the mover's leading face is.
		lead float64
	}{
		// Each is moved from the origin to x = 10 along the x axis, and each
		// leads with a flat face or the segment's whole length.
		{"a box", NewBoxShape(1, 1, 0), 0.5},
		{"a segment", NewSegmentShape(m.Vec2d{Y: -0.5}, m.Vec2d{Y: 0.5}, 0), 0},
		{"a rounded Polygon", NewBoxShape(1, 1, 0.25), 0.75},
	}
	targets := []struct {
		name  string
		shape Shape
		// face is the x the target's surface faces the mover at.
		face float64
	}{
		{"a circle", unitCircle, 4},
		{"a segment", upright, 5},
		{"a box", NewBoxShape(2, 2, 0), 4},
	}

	for _, mover := range movers {
		for _, target := range targets {
			t.Run(mover.name+" against "+target.name, func(t *testing.T) {
				hit, ok := ProbeShapeWith(mover.shape, origin, m.Vec2d{X: 10}, 0, nil,
					target.shape, m.Vec2d{X: 5}, 0, nil)
				if !ok {
					t.Fatal("met nothing")
				}
				if want := (target.face - mover.lead) / 10; !nearF(hit.T, want) {
					t.Errorf("T = %v, want %v", hit.T, want)
				}
				if !nearF(hit.Point.X, target.face) || math.Abs(hit.Point.Y) > 0.5+tolerance {
					t.Errorf("Point = %v, want on the target's surface at x = %v, within the mover's face",
						hit.Point, target.face)
				}
				if want := (m.Vec2d{X: -1}); !vecNear(hit.Normal, want) {
					t.Errorf("Normal = %v, want %v, facing the mover", hit.Normal, want)
				}
			})
		}
	}
}

func TestAProbedCornerMeetsAFace(t *testing.T) {
	// A box turned an eighth leads with its corner, √2/2 ahead of its Position,
	// and holds that angle the whole way.
	corner := math.Sqrt2 / 2
	diamond := NewBoxShape(1, 1, 0)

	hit, ok := ProbeShapeWith(diamond, origin, m.Vec2d{X: 10}, math.Pi/4, nil,
		NewBoxShape(2, 2, 0), m.Vec2d{X: 5}, 0, nil)
	if !ok {
		t.Fatal("a corner Probed into a face met nothing")
	}
	if want := (4 - corner) / 10; !nearF(hit.T, want) {
		t.Errorf("T = %v, want %v", hit.T, want)
	}
	if want := (m.Vec2d{X: 4}); !vecNear(hit.Point, want) {
		t.Errorf("Point = %v, want %v, where the corner lands on the face", hit.Point, want)
	}
	if want := (m.Vec2d{X: -1}); !vecNear(hit.Normal, want) {
		t.Errorf("Normal = %v, want the face's own %v", hit.Normal, want)
	}

	// The same the other way round: a face Probed into a target's corner, the
	// target turned an eighth, meets it at the corner.
	hit, ok = ProbeShapeWith(NewBoxShape(1, 1, 0), origin, m.Vec2d{X: 10}, 0, nil,
		NewBoxShape(2, 2, 0), m.Vec2d{X: 5}, math.Pi/4, nil)
	if !ok {
		t.Fatal("a face Probed into a corner met nothing")
	}
	if want := (4.5 - math.Sqrt2) / 10; !nearF(hit.T, want) {
		t.Errorf("T = %v, want %v", hit.T, want)
	}
	if want := (m.Vec2d{X: 5 - math.Sqrt2}); !vecNear(hit.Point, want) {
		t.Errorf("Point = %v, want the target's corner %v", hit.Point, want)
	}
}

func TestAProbedRoundedCornerMeetsACircleAlongTheLineBetweenThem(t *testing.T) {
	// The rounded box's core corner at (0.5, 0.5) passes the circle at
	// (5, 1.5): they touch when the corner is 1.25 from the centre, which is
	// the corner at x = 4.25 and the Position at 3.75.
	mover := NewBoxShape(1, 1, 0.25)
	hit, ok := ProbeShapeWith(mover, origin, m.Vec2d{X: 10}, 0, nil, unitCircle, m.Vec2d{X: 5, Y: 1.5}, 0, nil)
	if !ok {
		t.Fatal("a rounded corner Probed past a circle met nothing")
	}
	if !nearWithin(hit.T, 0.375, roundedTolerance) {
		t.Errorf("T = %v, want 0.375", hit.T)
	}
	if hit.T > 0.375+tolerance {
		t.Errorf("T = %v is past the touch at 0.375: the advance stops before it, never through it", hit.T)
	}
	if want := (m.Vec2d{X: 4.4, Y: 0.7}); !nearWithin(hit.Point.X, want.X, roundedTolerance) ||
		!nearWithin(hit.Point.Y, want.Y, roundedTolerance) {
		t.Errorf("Point = %v, want %v on the circle", hit.Point, want)
	}
	if want := (m.Vec2d{X: -0.6, Y: -0.8}); !nearWithin(hit.Normal.X, want.X, roundedTolerance) ||
		!nearWithin(hit.Normal.Y, want.Y, roundedTolerance) {
		t.Errorf("Normal = %v, want %v, from the circle towards the corner", hit.Normal, want)
	}
}

func TestAProbedShapeThatStartsOverlappingReportsAHitAtTheStart(t *testing.T) {
	box := NewBoxShape(2, 2, 0)
	to := m.Vec2d{X: 10}

	// The cores overlap: the mover is 0.7 into the box's face and 1.5 into its
	// height, so the way out is back through the face.
	hit, ok := ProbeShapeWith(NewBoxShape(1, 1, 0), m.Vec2d{X: 4.2}, to, 0, nil, box, m.Vec2d{X: 5}, 0, nil)
	if !ok || hit.T != 0 {
		t.Fatalf("a box starting inside a box gave %+v, %v, want a Hit at T = 0", hit, ok)
	}
	if !nearF(hit.Point.X, 4) {
		t.Errorf("Point = %v, want on the face at x = 4", hit.Point)
	}
	if want := (m.Vec2d{X: -1}); !vecNear(hit.Normal, want) {
		t.Errorf("Normal = %v, want %v, out of the overlap", hit.Normal, want)
	}

	// Only the rounding overlaps: the cores are a metre apart and the two radii
	// sum to 1.25.
	hit, ok = ProbeShapeWith(NewBoxShape(1, 1, 0.25), m.Vec2d{X: 3.5}, to, 0, nil,
		unitCircle, m.Vec2d{X: 5}, 0, nil)
	if !ok || hit.T != 0 {
		t.Fatalf("a rounded box starting inside a circle gave %+v, %v, want a Hit at T = 0", hit, ok)
	}
	if want := (m.Vec2d{X: 4}); !vecNear(hit.Point, want) || !vecNear(hit.Normal, m.Vec2d{X: -1}) {
		t.Errorf("the Hit is %+v, want Point %v facing (-1, 0)", hit, want)
	}

	// Two Shapes on the one centre still get a unit normal.
	hit, ok = ProbeShapeWith(NewBoxShape(1, 1, 0), m.Vec2d{X: 5}, to, 0, nil, box, m.Vec2d{X: 5}, 0, nil)
	if !ok || hit.T != 0 {
		t.Fatalf("a box starting on a box's centre gave %+v, %v, want a Hit at T = 0", hit, ok)
	}
	if !nearF(hit.Normal.Length(), 1) {
		t.Errorf("Normal = %v, want a unit vector", hit.Normal)
	}
}

func TestAProbedShapeTouchingWhereItStartsIsNotInsideIt(t *testing.T) {
	box := NewBoxShape(2, 2, 0)
	mover := NewBoxShape(1, 1, 0)
	start := m.Vec2d{X: 3.5}

	// Touching and moving in, it meets the face at once.
	if hit, ok := ProbeShapeWith(mover, start, m.Vec2d{X: 10}, 0, nil, box, m.Vec2d{X: 5}, 0, nil); !ok || hit.T != 0 {
		t.Errorf("a box touching a face and moving in gave %+v, %v, want a Hit at T = 0", hit, ok)
	}
	// Touching and moving away, or sliding along, it meets nothing.
	if hit, ok := ProbeShapeWith(mover, start, m.Vec2d{X: -10}, 0, nil, box, m.Vec2d{X: 5}, 0, nil); ok {
		t.Errorf("a box touching a face and moving away met it: %+v", hit)
	}
	if hit, ok := ProbeShapeWith(mover, start, m.Vec2d{X: 3.5, Y: 10}, 0, nil, box, m.Vec2d{X: 5}, 0, nil); ok {
		t.Errorf("a box sliding along a face met it: %+v", hit)
	}
}

func TestAProbedShapeMissesWhatItPassesOrStopsShortOf(t *testing.T) {
	box := NewBoxShape(2, 2, 0)
	mover := NewBoxShape(1, 1, 0)
	above := m.Vec2d{Y: 1.6}
	beyond := m.Vec2d{X: 10, Y: 1.6}

	if hit, ok := ProbeShapeWith(mover, above, beyond, 0, nil, box, m.Vec2d{X: 5}, 0, nil); ok {
		t.Errorf("a box passing 0.1 above a box met it: %+v", hit)
	}
	if hit, ok := ProbeShapeWith(mover, origin, m.Vec2d{X: 3.4}, 0, nil, box, m.Vec2d{X: 5}, 0, nil); ok {
		t.Errorf("a box stopping 0.1 short of a box met it: %+v", hit)
	}
	// Turned an eighth, the same box reaches 0.11 below the top face, and it
	// no longer passes.
	if _, ok := ProbeShapeWith(mover, above, beyond, math.Pi/4, nil, box, m.Vec2d{X: 5}, 0, nil); !ok {
		t.Error("a box turned an eighth, its corner below the top face, still passed")
	}
}

func TestAZeroLengthProbeOfAShapeBehavesAsAnOverlap(t *testing.T) {
	box := NewBoxShape(2, 2, 0)
	mover := NewBoxShape(1, 1, 0)

	inside := m.Vec2d{X: 4.2}
	if hit, ok := ProbeShapeWith(mover, inside, inside, 0, nil, box, m.Vec2d{X: 5}, 0, nil); !ok || hit.T != 0 {
		t.Errorf("a zero-length Probe overlapping a box gave %+v, %v, want a Hit at T = 0", hit, ok)
	}
	if hit, ok := ProbeShapeWith(mover, origin, origin, 0, nil, box, m.Vec2d{X: 5}, 0, nil); ok {
		t.Errorf("a zero-length Probe well clear of a box met it: %+v", hit)
	}
}

func TestProbingACircleShapeIsProbingThatCircle(t *testing.T) {
	// The circle's offset is turned by the angle, so its centre runs a
	// quarter-turned (0.2, 0.1) ahead of the Position.
	circle := NewCircleShape(0.5, m.Vec2d{X: 0.2, Y: 0.1})
	offset := m.Vec2d{X: -0.1, Y: 0.2}
	from, to := m.Vec2d{X: -1, Y: 0.3}, m.Vec2d{X: 9, Y: -0.4}

	for name, target := range map[string]Shape{
		"a circle": unitCircle, "a segment": upright, "a box": NewBoxShape(2, 2, 0.1),
	} {
		want, wantOK := ProbeShape(from.Add(offset), to.Add(offset), 0.5, target, m.Vec2d{X: 5}, 0.3, nil)
		got, ok := ProbeShapeWith(circle, from, to, math.Pi/2, nil, target, m.Vec2d{X: 5}, 0.3, nil)
		if !wantOK || ok != wantOK || !nearF(got.T, want.T) || !vecNear(got.Point, want.Point) ||
			!vecNear(got.Normal, want.Normal) {
			t.Errorf("against %s ProbeShapeWith gave %+v, %v and ProbeShape %+v, %v", name, got, ok, want, wantOK)
		}
	}
}
