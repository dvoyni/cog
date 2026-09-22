package types

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// clockwiseSquare is the two-by-two square about the origin wound the way
// Chipmunk winds a polygon Shape, which is the winding AreaForPoly calls
// positive.
var clockwiseSquare = []m.Vec2d{{X: -1, Y: -1}, {X: 1, Y: -1}, {X: 1, Y: 1}, {X: -1, Y: 1}}

func TestMomentForCircleIsHalfMassRadiusSquaredPlusTheOffset(t *testing.T) {
	if got, want := MomentForCircle(2, 0, 3, m.Vec2d{}), 9.0; !nearF(got, want) {
		t.Fatalf("a solid disc = %v, want %v", got, want)
	}
	if got, want := MomentForCircle(2, 1, 3, m.Vec2d{}), 10.0; !nearF(got, want) {
		t.Fatalf("a hollow disc = %v, want %v", got, want)
	}
	// The parallel axis theorem: a mass of 2 moved 4 away adds 2 * 16.
	if got, want := MomentForCircle(2, 0, 3, m.Vec2d{X: 4}), 9.0+32.0; !nearF(got, want) {
		t.Fatalf("an offset disc = %v, want %v", got, want)
	}
}

func TestMomentForSegmentIsARodAboutItsMiddle(t *testing.T) {
	// A thin rod of length 2 about its centre is mass * length squared / 12.
	if got, want := MomentForSegment(3, m.Vec2d{X: -1}, m.Vec2d{X: 1}, 0), 1.0; !nearF(got, want) {
		t.Fatalf("a centred rod = %v, want %v", got, want)
	}
	// Moved so its middle is 2 away, it gains mass times 4.
	if got, want := MomentForSegment(3, m.Vec2d{X: 1}, m.Vec2d{X: 3}, 0), 1.0+12.0; !nearF(got, want) {
		t.Fatalf("an offset rod = %v, want %v", got, want)
	}
	if MomentForSegment(3, m.Vec2d{X: -1}, m.Vec2d{X: 1}, 0.5) <= 1.0 {
		t.Fatal("a fattened rod should have a larger moment than a thin one")
	}
}

func TestMomentForBoxAndMomentForPolyAgreeOnASquare(t *testing.T) {
	if got, want := MomentForBox(3, 2, 2), 2.0; !nearF(got, want) {
		t.Fatalf("MomentForBox = %v, want %v", got, want)
	}
	if got, want := MomentForPoly(3, clockwiseSquare, m.Vec2d{}, 0), MomentForBox(3, 2, 2); !nearF(got, want) {
		t.Fatalf("MomentForPoly on a square = %v, want MomentForBox's %v", got, want)
	}
}

func TestMomentForPolyOnTwoVerticesIsASegment(t *testing.T) {
	a, b := m.Vec2d{X: -1}, m.Vec2d{X: 1}
	if got, want := MomentForPoly(3, []m.Vec2d{a, b}, m.Vec2d{}, 0), MomentForSegment(3, a, b, 0); got != want {
		t.Fatalf("MomentForPoly on two vertices = %v, want MomentForSegment's %v", got, want)
	}
}

func TestAreaForCircleAndSegmentAreTheDiscAndTheCapsule(t *testing.T) {
	if got, want := AreaForCircle(0, 2), 4*math.Pi; !nearF(got, want) {
		t.Fatalf("a solid disc = %v, want %v", got, want)
	}
	if got, want := AreaForCircle(1, 2), 3*math.Pi; !nearF(got, want) {
		t.Fatalf("a hollow disc = %v, want %v", got, want)
	}
	// A capsule is a rectangle of length by twice the radius, plus a disc.
	if got, want := AreaForSegment(m.Vec2d{X: -1}, m.Vec2d{X: 1}, 0.5), 0.25*math.Pi+2.0; !nearF(got, want) {
		t.Fatalf("a capsule = %v, want %v", got, want)
	}
	if got, want := AreaForSegment(m.Vec2d{X: -1}, m.Vec2d{X: 1}, 0), 0.0; got != want {
		t.Fatalf("a segment of no radius = %v, want %v", got, want)
	}
}

func TestAreaForPolyCallsChipmunksWindingPositive(t *testing.T) {
	if got, want := AreaForPoly(clockwiseSquare, 0), 4.0; !nearF(got, want) {
		t.Fatalf("Chipmunk's winding = %v, want %v", got, want)
	}

	reversed := make([]m.Vec2d, len(clockwiseSquare))
	for i, vert := range clockwiseSquare {
		reversed[len(clockwiseSquare)-1-i] = vert
	}
	if got, want := AreaForPoly(reversed, 0), -4.0; !nearF(got, want) {
		t.Fatalf("the other winding = %v, want %v, which is what the Shape constructor rejects", got, want)
	}

	// A radius fattens the outline by a rim of perimeter by radius plus a disc.
	if got, want := AreaForPoly(clockwiseSquare, 0.5), 4.0+0.25*math.Pi+4.0; !nearF(got, want) {
		t.Fatalf("a rounded square = %v, want %v", got, want)
	}
}

func TestCentroidForPolyFindsTheCentreOfAnOutline(t *testing.T) {
	centroid, ok := CentroidForPoly(clockwiseSquare)
	if !ok {
		t.Fatal("a square is not a degenerate outline")
	}
	if want := (m.Vec2d{}); !vecNear(centroid, want) {
		t.Fatalf("the centroid of a centred square = %v, want %v", centroid, want)
	}

	moved := make([]m.Vec2d, len(clockwiseSquare))
	for i, vert := range clockwiseSquare {
		moved[i] = vert.Add(m.Vec2d{X: 5, Y: -2})
	}
	centroid, ok = CentroidForPoly(moved)
	if !ok {
		t.Fatal("a moved square is not a degenerate outline")
	}
	if want := (m.Vec2d{X: 5, Y: -2}); !vecNear(centroid, want) {
		t.Fatalf("the centroid of a moved square = %v, want %v", centroid, want)
	}
}

func TestCentroidForPolyTellsTheCallerAboutADegenerateOutline(t *testing.T) {
	// cp divides by twice the signed area with no guard, so each of these hands
	// back a NaN that would reach a Body's Position.
	for name, verts := range map[string][]m.Vec2d{
		"no vertices":         {},
		"coincident vertices": {{X: 1, Y: 1}, {X: 1, Y: 1}, {X: 1, Y: 1}},
		"a collinear outline": {{}, {X: 1}, {X: 2}},
		"a doubled edge":      {{}, {X: 1}, {}, {X: 1}},
	} {
		centroid, ok := CentroidForPoly(verts)
		if ok {
			t.Fatalf("%s was accepted, giving %v", name, centroid)
		}
		if math.IsNaN(centroid.X) || math.IsNaN(centroid.Y) {
			t.Fatalf("%s produced a NaN: %v", name, centroid)
		}
		if centroid != (m.Vec2d{}) {
			t.Fatalf("%s returned %v, want the zero vector", name, centroid)
		}
	}
}

func TestTheMassHelpersAllocateNothing(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	a, b := m.Vec2d{X: -1}, m.Vec2d{X: 1}
	offset := m.Vec2d{X: 2, Y: 3}

	for name, call := range map[string]func(){
		"MomentForCircle":  func() { floatSink = MomentForCircle(2, 0, 3, offset) },
		"MomentForSegment": func() { floatSink = MomentForSegment(2, a, b, 0.5) },
		"MomentForBox":     func() { floatSink = MomentForBox(2, 3, 4) },
		"MomentForPoly":    func() { floatSink = MomentForPoly(2, clockwiseSquare, offset, 0) },
		"AreaForCircle":    func() { floatSink = AreaForCircle(1, 2) },
		"AreaForSegment":   func() { floatSink = AreaForSegment(a, b, 0.5) },
		"AreaForPoly":      func() { floatSink = AreaForPoly(clockwiseSquare, 0.5) },
		"CentroidForPoly":  func() { vecSink, boolSink = CentroidForPoly(clockwiseSquare) },
	} {
		if allocations := testing.AllocsPerRun(100, call); allocations != 0 {
			t.Fatalf("%s allocated %v times a call, want 0", name, allocations)
		}
	}
}
