package types

import (
	"errors"
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// The hulls and the closed-form query numbers here came out of the same
// throwaway harness the Contact oracle did: github.com/jakecoffman/cp/v2 v2.4.0,
// printing its own ConvexHull output, SegmentQueryInfo and PointQueryInfo. What
// is committed is what the harness emitted, at the same 1e-9 tolerance and with
// no mismatch budget.

// unitBoxVerts is cp's own NewBox order for a 1 m box, which is the winding
// AreaForPoly treats as positive.
var unitBoxVerts = []m.Vec2d{
	{X: 0.5, Y: -0.5}, {X: 0.5, Y: 0.5}, {X: -0.5, Y: 0.5}, {X: -0.5, Y: -0.5},
}

func TestTheHullingConstructorEnforcesChipmunksWinding(t *testing.T) {
	// A square given anticlockwise and out of order. cp's ConvexHull answers
	// with the leftmost-lowest vertex first and the winding AreaForPoly reads as
	// positive, which is why the constructor always hulls rather than checking.
	scrambled := []m.Vec2d{{X: -0.5, Y: 0.5}, {X: 0.5, Y: -0.5}, {X: -0.5, Y: -0.5}, {X: 0.5, Y: 0.5}}
	shape, polygon, err := NewPolygonShape(scrambled, 0)
	if err != nil {
		t.Fatalf("hulling a square: %v", err)
	}
	if shape.Kind != ShapeQuad {
		t.Fatalf("a four-vertex hull is kind %v, want ShapeQuad carrying them inline", shape.Kind)
	}
	if polygon.Verts.Len() != 0 {
		t.Errorf("an inline kind came back with %d Polygon vertices, want the zero Polygon",
			polygon.Verts.Len())
	}

	want := []m.Vec2d{{X: -0.5, Y: -0.5}, {X: 0.5, Y: -0.5}, {X: 0.5, Y: 0.5}, {X: -0.5, Y: 0.5}}
	for i, v := range want {
		if !vecNear(shape.verts[i], v) {
			t.Errorf("hull vertex %d is %v, want cp's %v", i, shape.verts[i], v)
		}
	}
	if area := AreaForPoly(shape.verts[:4], 0); area <= 0 {
		t.Errorf("the hulled square has signed area %v, want it positive: that is the winding", area)
	}
}

func TestAThreeVertexOutlineIsATriangleAndAFiveVertexOneCarriesAPolygon(t *testing.T) {
	triangle := []m.Vec2d{{X: -0.6, Y: -0.4}, {X: 0.8, Y: -0.4}, {X: 0.1, Y: 0.7}}
	shape, polygon, err := NewPolygonShape(triangle, 0)
	if err != nil {
		t.Fatalf("hulling a triangle: %v", err)
	}
	if shape.Kind != ShapeTri || polygon.Verts.Len() != 0 {
		t.Fatalf("a triangle is kind %v with %d Polygon vertices, want ShapeTri and none",
			shape.Kind, polygon.Verts.Len())
	}
	for i, v := range triangle {
		if !vecNear(shape.verts[i], v) {
			t.Errorf("triangle vertex %d is %v, want cp's %v", i, shape.verts[i], v)
		}
	}

	// A pentagon has nowhere inline to go, so it is the one kind that needs the
	// second Component. There is no cap past that: cp has none either.
	var pentagon []m.Vec2d
	for i := range 5 {
		angle := -2 * math.Pi * float64(i) / 5
		pentagon = append(pentagon, m.ForAngle(angle))
	}
	shape, polygon, err = NewPolygonShape(pentagon, 0.05)
	if err != nil {
		t.Fatalf("hulling a pentagon: %v", err)
	}
	if shape.Kind != ShapePoly {
		t.Fatalf("a five-vertex hull is kind %v, want ShapePoly", shape.Kind)
	}
	if got := polygon.Verts.Len(); got != 5 {
		t.Fatalf("the Polygon holds %d vertices, want 5", got)
	}
	if shape.Radius != 0.05 {
		t.Errorf("the rounding is %v, want 0.05: every kind carries the radius", shape.Radius)
	}

	// A thirty-two-gon is just as legal, which is the whole of "no vertex cap".
	var many []m.Vec2d
	for i := range 32 {
		angle := -2 * math.Pi * float64(i) / 32
		many = append(many, m.ForAngle(angle).MulS(2))
	}
	if _, big, err := NewPolygonShape(many, 0); err != nil || big.Verts.Len() != 32 {
		t.Errorf("a 32-gon came back with %d vertices and %v, want 32 and no error", big.Verts.Len(), err)
	}
}

func TestARefusedOutlineIsAPointAndSaysWhichOfTheThreeItWas(t *testing.T) {
	for name, outline := range map[string]struct {
		verts []m.Vec2d
		want  error
	}{
		"two vertices": {
			verts: []m.Vec2d{{}, {X: 1}},
			want:  ErrTooFewVertices,
		},
		"three coincident vertices": {
			verts: []m.Vec2d{{X: 1, Y: 1}, {X: 1, Y: 1}, {X: 1, Y: 1}},
			want:  ErrDegenerateOutline,
		},
		"three collinear vertices": {
			verts: []m.Vec2d{{}, {X: 1}, {X: 2}},
			want:  ErrDegenerateOutline,
		},
		"a concave outline": {
			// An arrowhead: the fourth vertex is inside the hull of the other
			// three, so the hull drops it.
			verts: []m.Vec2d{{X: -1, Y: 0}, {X: 0, Y: 0.2}, {X: 1, Y: 0}, {X: 0, Y: 1}},
			want:  ErrConcaveOutline,
		},
		"a vertex on an edge": {
			// Redundant rather than concave, and refused on the same terms: the
			// hull changed what the app wrote.
			verts: []m.Vec2d{{X: -1, Y: -1}, {X: 0, Y: -1}, {X: 1, Y: -1}, {X: 0, Y: 1}},
			want:  ErrConcaveOutline,
		},
	} {
		shape, polygon, err := NewPolygonShape(outline.verts, 0.3)
		if !errors.Is(err, outline.want) {
			t.Errorf("%s gave %v, want %v", name, err, outline.want)
		}
		if shape.Kind != ShapeCircle || shape.Radius != 0 || shape.Offset() != (m.Vec2d{}) {
			t.Errorf("%s came back as %+v, want a point: a circle of radius 0 at the origin",
				name, shape)
		}
		if polygon.Verts.Len() != 0 {
			t.Errorf("%s came back with %d Polygon vertices, want none", name, polygon.Verts.Len())
		}
	}
}

func TestARefusedOutlineAllocatesNothingOnTheFailurePath(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	// The errors are package-level sentinels rather than structs carrying the
	// outline, which is what makes this true.
	tooFew := []m.Vec2d{{}, {X: 1}}
	if got := testing.AllocsPerRun(100, func() {
		shapeSink, polygonSink, errSink = NewPolygonShape(tooFew, 0)
	}); got != 0 {
		t.Errorf("refusing a two-vertex outline allocated %v times, want 0", got)
	}
}

func TestTheBoxConstructorsKeepCpsFourDirectVertices(t *testing.T) {
	box := NewBoxShape(1, 1, 0)
	if box.Kind != ShapeQuad {
		t.Fatalf("a box is kind %v, want ShapeQuad", box.Kind)
	}
	for i, v := range unitBoxVerts {
		if !vecNear(box.verts[i], v) {
			t.Errorf("box vertex %d is %v, want cp's %v", i, box.verts[i], v)
		}
	}

	// NewBoxShapeFor is the off-centre one, which is what a wall drawn around
	// something other than its own middle is.
	offset := NewBoxShapeFor(NewBB(1, 2, 3, 5), 0.1)
	want := []m.Vec2d{{X: 3, Y: 2}, {X: 3, Y: 5}, {X: 1, Y: 5}, {X: 1, Y: 2}}
	for i, v := range want {
		if !vecNear(offset.verts[i], v) {
			t.Errorf("off-centre box vertex %d is %v, want %v", i, offset.verts[i], v)
		}
	}
	if offset.Radius != 0.1 {
		t.Errorf("the box's rounding is %v, want 0.1", offset.Radius)
	}
}

func TestPolygonVertsReadsTheKindsOwnStoreAndAllocatesNothingWarm(t *testing.T) {
	var pentagon []m.Vec2d
	for i := range 5 {
		pentagon = append(pentagon, m.ForAngle(-2*math.Pi*float64(i)/5))
	}
	shape, polygon, err := NewPolygonShape(pentagon, 0)
	if err != nil {
		t.Fatalf("hulling a pentagon: %v", err)
	}

	dst := PolygonVerts(nil, shape, polygon)
	if len(dst) != 5 {
		t.Fatalf("PolygonVerts gave %d vertices, want 5", len(dst))
	}
	for i := range dst {
		if !vecNear(dst[i], polygon.Verts.At(i)) {
			t.Errorf("vertex %d is %v, want the Component's %v", i, dst[i], polygon.Verts.At(i))
		}
	}

	// An inline kind reads its own slots, and the two kinds that are not
	// polygons have no vertices to give.
	box := NewBoxShape(1, 1, 0)
	if got := PolygonVerts(dst[:0], box, Polygon{}); len(got) != 4 || !vecNear(got[0], unitBoxVerts[0]) {
		t.Errorf("a box gave %v, want its own four slots", got)
	}
	if got := PolygonVerts(dst[:0], NewCircleShape(1, m.Vec2d{}), Polygon{}); len(got) != 0 {
		t.Errorf("a circle gave %d vertices, want none", len(got))
	}

	if got := testing.AllocsPerRun(100, func() {
		dst = PolygonVerts(dst[:0], shape, polygon)
	}); got != 0 && !raceEnabled {
		t.Errorf("refilling a warm run allocated %v times, want 0", got)
	}
}

func TestASegmentsNeighboursAreTheLocalTangentsChipmunkStores(t *testing.T) {
	// C's cpSegmentShapeSetNeighbors, which has no counterpart in cp at all —
	// defect 4, and the reason cp's own end-cap rejection is dead code.
	previous := m.Vec2d{X: -2, Y: 0.5}
	a := m.Vec2d{X: -1}
	b := m.Vec2d{X: 1}
	next := m.Vec2d{X: 2, Y: 0.5}

	shape := NewSegmentShapeWithNeighbours(previous, a, b, next, 0.1)
	if shape.Kind != ShapeSegment || shape.A() != a || shape.B() != b || shape.Radius != 0.1 {
		t.Fatalf("the neighboured constructor built %+v", shape)
	}
	if got, want := shape.verts[2], previous.Sub(a); got != want {
		t.Errorf("the first tangent is %v, want previous − a = %v", got, want)
	}
	if got, want := shape.verts[3], next.Sub(b); got != want {
		t.Errorf("the second tangent is %v, want next − b = %v", got, want)
	}

	// The plain constructor leaves them zero, where the rejection is the no-op
	// it is in cp.
	plain := NewSegmentShape(a, b, 0.1)
	if plain.verts[2] != (m.Vec2d{}) || plain.verts[3] != (m.Vec2d{}) {
		t.Error("the plain segment constructor wrote a tangent")
	}
}

func TestACircleCrossingAChainJointDoesNotCatch(t *testing.T) {
	// Two segments meeting end to end at the origin, each told which point lies
	// beyond the other's far end. A circle rolled along the run and across the
	// joint must meet nothing but the two face normals: an end-cap contact at
	// the joint would have a sideways component and would push the circle back
	// the way it came, which is the catch the tangents exist to remove.
	left := m.Vec2d{X: -2}
	middle := m.Vec2d{}
	right := m.Vec2d{X: 2}

	first := NewSegmentShapeWithNeighbours(left, left, middle, right, 0)
	second := NewSegmentShapeWithNeighbours(left, middle, right, right, 0)
	circle := NewCircleShape(0.25, m.Vec2d{})

	for step := range 41 {
		x := -0.2 + 0.01*float64(step)
		// A little inside the surface, so the pair always touches.
		at := m.Vec2d{X: x, Y: 0.2}
		for name, wall := range map[string]Shape{"the left segment": first, "the right segment": second} {
			touch, ok := collide(t, circle, at, wall, m.Vec2d{})
			if !ok {
				continue
			}
			if math.Abs(touch.normal.X) > 1e-12 {
				t.Fatalf("at x = %v %s met the circle sideways, normal %v", x, name, touch.normal)
			}
		}
	}

	// Without the neighbours the same sweep does produce that cap contact, so
	// the rejection above is doing something rather than never firing.
	bare := NewSegmentShape(middle, right, 0)
	caught := false
	for step := range 21 {
		at := m.Vec2d{X: -0.2 + 0.01*float64(step), Y: 0.2}
		if touch, ok := collide(t, circle, at, bare, m.Vec2d{}); ok && math.Abs(touch.normal.X) > 1e-12 {
			caught = true
		}
	}
	if !caught {
		t.Error("the neighbourless segment never caught, so the test proves nothing")
	}
}

func TestTheClosedFormQueriesOnAPolygonAreCpsPointForPoint(t *testing.T) {
	box := NewBoxShape(1, 1, 0)
	rounded := NewBoxShape(1, 1, 0.2)
	origin := m.Vec2d{}

	// cp's PolyShape.PointQuery, from inside, from outside past a corner, and
	// on a rounded box where the surface point is the radius out along the
	// gradient.
	point, distance, gradient := pointQueryFor(box, origin, 0, m.Vec2d{X: 0.2, Y: 0.1})
	wantPoint(t, "inside point", point, 0.5, 0.099999999999999978)
	wantNear(t, "inside distance", distance, -0.29999999999999999)
	wantNormal(t, gradient, 1, -9.2518585385429716e-17)

	point, distance, gradient = pointQueryFor(box, origin, 0, m.Vec2d{X: 1.3, Y: 0.9})
	wantPoint(t, "corner point", point, 0.5, 0.5)
	wantNear(t, "corner distance", distance, 0.89442719099991597)
	wantNormal(t, gradient, 0.89442719099991574, 0.44721359549995787)

	point, distance, _ = pointQueryFor(rounded, origin, 0, m.Vec2d{X: 1.3, Y: 0.1})
	wantPoint(t, "rounded point", point, 0.69999999999999996, 0.099999999999999978)
	wantNear(t, "rounded distance", distance, 0.60000000000000009)

	// cp's PolyShape.SegmentQuery: a thick Probe against a rotated box, a thin
	// one against a rounded box's bevelled corner, and one exactly along a face.
	hit, ok := ProbeShape(m.Vec2d{X: -2, Y: -0.3}, m.Vec2d{X: 3, Y: 1.1}, 0.25,
		NewBoxShape(1, 1, 0), m.Vec2d{X: 1, Y: 0.5}, 0.4, nil)
	if !ok {
		t.Fatal("a thick Probe missed a rotated box")
	}
	wantNear(t, "rotated box T", hit.T, 0.45135848488447555)
	wantPoint(t, "rotated box point", hit.Point, 0.48705767292309882, 0.42925646441542836)
	wantNormal(t, hit.Normal, -0.92106099400288399, -0.38941834230865008)

	hit, ok = ProbeShape(m.Vec2d{X: -1, Y: 0.62}, m.Vec2d{X: 4, Y: 0.62}, 0.05,
		NewBoxShape(1, 1, 0.1), m.Vec2d{X: 2}, 0, nil)
	if !ok {
		t.Fatal("a Probe missed a rounded box's bevelled corner")
	}
	wantNear(t, "bevel T", hit.T, 0.48200000000000059)
	wantPoint(t, "bevel point", hit.Point, 1.4400000000000022, 0.57999999999999974)
	wantNormal(t, hit.Normal, -0.59999999999998299, 0.80000000000000426)

	// The parallel ray is defect 6: cp divides by an − bn of exactly zero and
	// survives only because the NaN happens to fail the span test below it. The
	// guarded quotient is a number, and it answers the same.
	hit, ok = ProbeShape(m.Vec2d{X: -3, Y: 0.5}, m.Vec2d{X: 3, Y: 0.5}, 0, box, origin, 0, nil)
	if !ok {
		t.Fatal("a Probe along a face missed the box entirely")
	}
	wantNear(t, "parallel T", hit.T, 0.41666666666666663)
	wantPoint(t, "parallel point", hit.Point, -0.5, 0.5)
	wantNormal(t, hit.Normal, -0.99999999999999889, 0)
	if math.IsNaN(hit.T) {
		t.Error("the parallel Probe answered a NaN, which is the guard's whole purpose")
	}
}

// pointQueryFor is ClosestPoint with the distance and the gradient kept, which
// is the whole of cp's PointQueryInfo.
func pointQueryFor(shape Shape, at m.Vec2d, angle float64, p m.Vec2d) (m.Vec2d, float64, m.Vec2d) {
	var scratch [worldScratchLen]m.Vec2d
	transform := NewTransformRigid(at, angle)
	used, _ := cacheWorldAt(shape, transform, nil, scratch[:])
	return pointQueryWorld(p, shape, scratch[:used])
}

var (
	shapeSink   Shape
	polygonSink Polygon
	errSink     error
)
