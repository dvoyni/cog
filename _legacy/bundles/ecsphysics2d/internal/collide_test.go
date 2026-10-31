package internal

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// Every number here came out of the same throwaway harness the circle oracle
// did: github.com/jakecoffman/cp/v2 v2.4.0, one pair in a Space at the port's
// settings, reading its own arbiter's ContactPointSet in PreSolve — which is
// the manifold as detection left it, before any impulse. The pairs are handed
// to the port in cp's own kind order, which is the order its arbiter reports,
// so nothing here depends on the nine-arm switch's sorting; the flip has a test
// of its own below.
//
// Tolerance is the specification's 1e-9 and there is no mismatch budget: every
// case agrees with cp exactly or is named as a departure.

func TestSegmentAgainstSegmentIsCpsGJKPointForPoint(t *testing.T) {
	// Two crossing capsules, deeply overlapped, so the answer comes out of EPA
	// rather than out of GJK's own closest-point pass.
	across := NewSegmentShape(m.Vec2d{Y: -1}, m.Vec2d{Y: 1}, 0.15)
	along := NewSegmentShape(m.Vec2d{X: -1}, m.Vec2d{X: 1}, 0.1)
	touch, ok := collide(t, across, m.Vec2d{X: 0.3, Y: 0.2}, along, m.Vec2d{})
	if !ok {
		t.Fatal("two crossing capsules do not touch")
	}
	if touch.count != 1 {
		t.Fatalf("two crossing capsules have %d points, want 1", touch.count)
	}
	wantNormal(t, touch.normal, -0.99999999999999956, 0)
	wantPoint(t, "p1", touch.points[0].p1, 0.15000000000000005, -8.8817841970012523e-16)
	wantPoint(t, "p2", touch.points[0].p2, 1.0999999999999999, 0)
	wantNear(t, "depth", touch.points[0].depth, 0.9499999999999994)
}

func TestTwoParallelSegmentsMakeCpsTwoPointManifold(t *testing.T) {
	// The case the second Contact point exists for: two capsules lying side by
	// side, whose support edges overlap along their whole length.
	upper := NewSegmentShape(m.Vec2d{X: -0.5}, m.Vec2d{X: 0.5}, 0.1)
	lower := NewSegmentShape(m.Vec2d{X: -1}, m.Vec2d{X: 1}, 0.1)
	touch, ok := collide(t, upper, m.Vec2d{Y: 0.15}, lower, m.Vec2d{})
	if !ok {
		t.Fatal("two parallel capsules 0.15 m apart with a summed radius of 0.2 m do not touch")
	}
	if touch.count != 2 {
		t.Fatalf("two parallel capsules have %d points, want cp's 2", touch.count)
	}
	wantNormal(t, touch.normal, 0, -0.99999999999999978)
	wantPoint(t, "p1[0]", touch.points[0].p1, -0.5, 0.050000000000000017)
	wantPoint(t, "p2[0]", touch.points[0].p2, -0.49999999999999933, 0.099999999999999978)
	wantNear(t, "depth[0]", touch.points[0].depth, 0.049999999999999947)
	wantPoint(t, "p1[1]", touch.points[1].p1, 0.5, 0.050000000000000017)
	wantPoint(t, "p2[1]", touch.points[1].p2, 0.50000000000000022, 0.099999999999999978)
	wantNear(t, "depth[1]", touch.points[1].depth, 0.049999999999999947)
}

func TestCircleAgainstAPolygonIsCpsGJKPointForPoint(t *testing.T) {
	box := NewBoxShape(1, 1, 0)
	circle := NewCircleShape(0.5, m.Vec2d{})

	// Against a face, where the separating axis is the face normal.
	touch, ok := collide(t, circle, m.Vec2d{X: 0.2, Y: 0.8}, box, m.Vec2d{})
	if !ok {
		t.Fatal("a circle 0.3 m into a box's top face does not touch it")
	}
	if touch.count != 1 {
		t.Fatalf("a circle against a box has %d points, want 1", touch.count)
	}
	wantNormal(t, touch.normal, 0, -0.99999999999999889)
	wantPoint(t, "p1", touch.points[0].p1, 0.20000000000000001, 0.3000000000000006)
	wantPoint(t, "p2", touch.points[0].p2, 0.19999999999999996, 0.5)
	wantNear(t, "depth", touch.points[0].depth, 0.19999999999999918)

	// Against a corner, which is the vertex-against-vertex branch of
	// ClosestPoints: the axis is not an axis of the Minkowski difference at all.
	touch, ok = collide(t, circle, m.Vec2d{X: 0.8, Y: 0.75}, box, m.Vec2d{})
	if !ok {
		t.Fatal("a circle overlapping a box's corner does not touch it")
	}
	wantNormal(t, touch.normal, -0.76822127959737385, -0.64018439966447815)
	wantPoint(t, "corner p1", touch.points[0].p1, 0.41588936020131312, 0.42990780016776092)
	wantPoint(t, "corner p2", touch.points[0].p2, 0.5, 0.5)
	wantNear(t, "corner depth", touch.points[0].depth, 0.10948751620466565)
}

func TestACircleAgainstARoundedPolygonTakesTheCsRadiusSign(t *testing.T) {
	// Defect 1, and the one number in this file that is deliberately not cp's.
	// cp offsets the Polygon's surface point by +poly.r where C has −poly->r, so
	// its p2 lands 2·r on the wrong side of the face: it reports the same pair
	// 0.4 m less deep and, here, not touching at all. Everything else — the
	// normal and p1 — is cp's own, which is what pins the departure to the one
	// sign.
	box := NewBoxShape(1, 1, 0.2)
	circle := NewCircleShape(0.5, m.Vec2d{})
	touch, ok := collide(t, circle, m.Vec2d{X: 0.2, Y: 0.9}, box, m.Vec2d{})
	if !ok {
		t.Fatal("a circle overlapping a rounded box does not touch it")
	}

	wantNormal(t, touch.normal, 0, -0.99999999999999889)
	wantPoint(t, "p1", touch.points[0].p1, 0.20000000000000001, 0.40000000000000069)
	// cp says (0.2, 0.3) and a depth of −0.1; C's sign puts the surface point
	// 2·0.2 m the other way and the pair 0.3 m deep, which is what it is.
	wantPoint(t, "p2", touch.points[0].p2, 0.19999999999999996, 0.70000000000000018)
	wantNear(t, "depth", touch.points[0].depth, 0.30000000000000032)
}

func TestSegmentAgainstAPolygonIsCpsGJKPointForPoint(t *testing.T) {
	wall := NewSegmentShape(m.Vec2d{X: -1.5}, m.Vec2d{X: 1.5}, 0.1)
	box := NewBoxShape(1, 1, 0)
	touch, ok := collide(t, wall, m.Vec2d{Y: 0.55}, box, m.Vec2d{})
	if !ok {
		t.Fatal("a capsule resting 0.05 m into a box's top face does not touch it")
	}
	if touch.count != 2 {
		t.Fatalf("a capsule across a box's face has %d points, want cp's 2", touch.count)
	}
	wantNormal(t, touch.normal, 0, -0.99999999999999978)
	wantPoint(t, "p1[0]", touch.points[0].p1, -0.49999999999999967, 0.45000000000000007)
	wantPoint(t, "p2[0]", touch.points[0].p2, -0.5, 0.5)
	wantNear(t, "depth[0]", touch.points[0].depth, 0.04999999999999992)
	wantPoint(t, "p1[1]", touch.points[1].p1, 0.50000000000000089, 0.45000000000000007)
	wantPoint(t, "p2[1]", touch.points[1].p2, 0.5, 0.5)
	wantNear(t, "depth[1]", touch.points[1].depth, 0.04999999999999992)
}

func TestPolygonAgainstPolygonIsCpsGJKPointForPoint(t *testing.T) {
	box := NewBoxShape(1, 1, 0)

	// Two boxes stacked, which is the case the whole ticket is judged on.
	touch, ok := collide(t, box, m.Vec2d{}, box, m.Vec2d{X: 0.1, Y: 0.9})
	if !ok {
		t.Fatal("two boxes overlapping by 0.1 m do not touch")
	}
	if touch.count != 2 {
		t.Fatalf("two stacked boxes have %d points, want cp's 2", touch.count)
	}
	wantNormal(t, touch.normal, 0, 0.99999999999999956)
	wantPoint(t, "p1[0]", touch.points[0].p1, 0.5, 0.5)
	wantPoint(t, "p2[0]", touch.points[0].p2, 0.49999999999999911, 0.40000000000000002)
	wantNear(t, "depth[0]", touch.points[0].depth, 0.099999999999999936)
	wantPoint(t, "p1[1]", touch.points[1].p1, -0.40000000000000113, 0.5)
	wantPoint(t, "p2[1]", touch.points[1].p2, -0.40000000000000002, 0.40000000000000002)
	wantNear(t, "depth[1]", touch.points[1].depth, 0.099999999999999936)

	// A box on a turned box, where the two support edges are not parallel and
	// the two points have different depths.
	touch, ok = collideAt(t, box, m.Vec2d{}, 0.3, box, m.Vec2d{X: 0.1, Y: 0.8}, 0)
	if !ok {
		t.Fatal("a box resting on a turned box does not touch it")
	}
	if touch.count != 2 {
		t.Fatalf("a box on a turned box has %d points, want cp's 2", touch.count)
	}
	wantNormal(t, touch.normal, 0, 0.99999999999999889)
	wantPoint(t, "turned p1[0]", touch.points[0].p1, 0.32990814123213319, 0.62542834789347279)
	wantPoint(t, "turned p2[0]", touch.points[0].p2, 0.32990814123213252, 0.30000000000000004)
	wantNear(t, "turned depth[0]", touch.points[0].depth, 0.32542834789347236)
	wantPoint(t, "turned p1[1]", touch.points[1].p1, -0.40000000000000091, 0.39964130092519318)
	wantPoint(t, "turned p2[1]", touch.points[1].p2, -0.40000000000000002, 0.30000000000000004)
	wantNear(t, "turned depth[1]", touch.points[1].depth, 0.099641300925193022)
}

func TestATriangleAgainstABoxIsCpsGJKPointForPoint(t *testing.T) {
	// The third polygon kind: ShapeTri, whose three vertices ride in the
	// Shape's own slots beside a box's four.
	triangle, polygon, err := NewPolygonShape(
		[]m.Vec2d{{X: -0.6, Y: -0.4}, {X: 0.8, Y: -0.4}, {X: 0.1, Y: 0.7}}, 0)
	if err != nil {
		t.Fatalf("hulling a triangle: %v", err)
	}
	if triangle.Kind != ShapeTri || polygon.Verts.Len() != 0 {
		t.Fatalf("the triangle is kind %v with %d Polygon vertices", triangle.Kind, polygon.Verts.Len())
	}

	touch, ok := collide(t, NewBoxShape(1, 1, 0), m.Vec2d{}, triangle, m.Vec2d{X: 0.15, Y: 0.85})
	if !ok {
		t.Fatal("a triangle resting 0.05 m into a box does not touch it")
	}
	if touch.count != 2 {
		t.Fatalf("a triangle on a box has %d points, want cp's 2", touch.count)
	}
	wantNormal(t, touch.normal, 0, 0.99999999999999967)
	wantPoint(t, "p1[0]", touch.points[0].p1, 0.5, 0.5)
	wantPoint(t, "p2[0]", touch.points[0].p2, 0.49999999999999944, 0.44999999999999996)
	wantNear(t, "depth[0]", touch.points[0].depth, 0.050000000000000031)
	wantPoint(t, "p1[1]", touch.points[1].p1, -0.45000000000000084, 0.5)
	wantPoint(t, "p2[1]", touch.points[1].p2, -0.44999999999999996, 0.44999999999999996)
	wantNear(t, "depth[1]", touch.points[1].depth, 0.050000000000000031)
}

func TestTheNineArmSwitchReadsTheSameEitherWayRound(t *testing.T) {
	// cp sorts its pair by Order() and carries a swapped flag on the arbiter for
	// the rest of the tick. The port sorts in the switch, flips once, and the
	// caller always reads the normal from its own first Shape towards its
	// second — so every pair has to answer the same both ways.
	box := NewBoxShape(1, 1, 0)
	triangle, _, err := NewPolygonShape(
		[]m.Vec2d{{X: -0.6, Y: -0.4}, {X: 0.8, Y: -0.4}, {X: 0.1, Y: 0.7}}, 0)
	if err != nil {
		t.Fatalf("hulling a triangle: %v", err)
	}

	for name, pair := range map[string]struct {
		a, b   Shape
		atA    m.Vec2d
		atB    m.Vec2d
		points int
	}{
		"circle against a polygon": {
			a: NewCircleShape(0.5, m.Vec2d{}), b: box,
			atA: m.Vec2d{X: 0.2, Y: 0.8}, points: 1,
		},
		"segment against a polygon": {
			a: NewSegmentShape(m.Vec2d{X: -1.5}, m.Vec2d{X: 1.5}, 0.1), b: box,
			atA: m.Vec2d{Y: 0.55}, points: 2,
		},
		"circle against a segment": {
			a:   NewCircleShape(0.5, m.Vec2d{}),
			b:   NewSegmentShape(m.Vec2d{X: -2}, m.Vec2d{X: 2}, 0.1),
			atA: m.Vec2d{X: 0.25, Y: 0.4}, points: 1,
		},
		"triangle against a polygon": {
			a: triangle, b: box,
			atA: m.Vec2d{X: 0.15, Y: 0.85}, points: 2,
		},
	} {
		forward, okForward := collide(t, pair.a, pair.atA, pair.b, pair.atB)
		backward, okBackward := collide(t, pair.b, pair.atB, pair.a, pair.atA)
		if !okForward || !okBackward {
			t.Fatalf("%s touches %v one way and %v the other", name, okForward, okBackward)
		}
		if forward.count != pair.points || backward.count != pair.points {
			t.Errorf("%s has %d points one way and %d the other, want %d",
				name, forward.count, backward.count, pair.points)
		}
		if !vecNear(forward.normal, backward.normal.Negate()) {
			t.Errorf("%s gives %v one way and %v the other, want the negation",
				name, forward.normal, backward.normal)
		}
		// The two points of a manifold come out in the order the two support
		// edges were handed to cp's ContactPoints, which the swap reverses, so
		// the pair of points is compared as a set rather than in order.
		for i := range forward.count {
			matched := false
			for j := range backward.count {
				if vecNear(forward.points[i].p1, backward.points[j].p2) &&
					vecNear(forward.points[i].p2, backward.points[j].p1) &&
					solverNear(forward.points[i].depth, backward.points[j].depth) {
					matched = true
				}
			}
			if !matched {
				t.Errorf("%s point %d, %+v, has no counterpart the other way round",
					name, i, forward.points[i])
			}
		}
	}
}

func TestAManifoldNeverCarriesMoreThanTwoPointsHoweverManyVertices(t *testing.T) {
	// cp's MAX_CONTACTS_PER_ARBITER, and the reason a Contact's Points is [2]
	// whatever the vertex count is.
	var many []m.Vec2d
	for i := range 24 {
		many = append(many, m.ForAngle(-2*math.Pi*float64(i)/24))
	}
	shape, polygon, err := NewPolygonShape(many, 0)
	if err != nil {
		t.Fatalf("hulling a 24-gon: %v", err)
	}
	verts := PolygonVerts(nil, shape, polygon)

	for solverStep := range 16 {
		at := m.Vec2d{X: 1.5 - 0.1*float64(solverStep), Y: 0.3 * float64(solverStep%3)}
		touch, ok := collideWithVerts(t, shape, verts, m.Vec2d{}, 0, shape, verts, at, 0.17)
		if ok && touch.count > 2 {
			t.Fatalf("two 24-gons at %v made %d points, want at most 2", at, touch.count)
		}
	}
}

func TestTheNarrowphaseAllocatesNothingOnEveryPairKind(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	// EPA's hull is two fixed stack buffers ping-ponged, where cp allocates a
	// fresh slice on every one of up to thirty iterations. That is the
	// narrowphase's whole share of cp's 656 objects a step.
	box := NewBoxShape(1, 1, 0)
	circle := NewCircleShape(0.5, m.Vec2d{})
	capsule := NewSegmentShape(m.Vec2d{X: -1}, m.Vec2d{X: 1}, 0.1)
	across := NewSegmentShape(m.Vec2d{Y: -1}, m.Vec2d{Y: 1}, 0.15)

	var pentagonVerts []m.Vec2d
	for i := range 5 {
		pentagonVerts = append(pentagonVerts, m.ForAngle(-2*math.Pi*float64(i)/5))
	}
	pentagon, polygon, err := NewPolygonShape(pentagonVerts, 0)
	if err != nil {
		t.Fatalf("hulling a pentagon: %v", err)
	}
	verts := PolygonVerts(nil, pentagon, polygon)

	for name, call := range map[string]func(){
		"circle against a polygon": func() {
			touchSink, boolSink = collide(t, circle, m.Vec2d{X: 0.2, Y: 0.8}, box, m.Vec2d{})
		},
		"segment against a segment": func() {
			touchSink, boolSink = collide(t, across, m.Vec2d{X: 0.3, Y: 0.2}, capsule, m.Vec2d{})
		},
		"segment against a polygon": func() {
			touchSink, boolSink = collide(t, capsule, m.Vec2d{Y: 0.45}, box, m.Vec2d{})
		},
		"polygon against a polygon": func() {
			touchSink, boolSink = collide(t, box, m.Vec2d{}, box, m.Vec2d{X: 0.1, Y: 0.9})
		},
		"a Polygon Component against a box": func() {
			touchSink, boolSink = collideWithVerts(t, pentagon, verts, m.Vec2d{X: 0.1, Y: 0.8}, 0,
				box, nil, m.Vec2d{}, 0)
		},
		"Penetration over the Polygon kinds": func() {
			vecSink, floatSink, boolSink = Penetration(
				pentagon, m.Vec2d{X: 0.1, Y: 0.8}, 0, verts, box, m.Vec2d{}, 0, nil)
		},
		"a Probe against a polygon": func() {
			hitSink, boolSink = ProbeShape(m.Vec2d{X: -2}, m.Vec2d{X: 2}, 0.2, box, m.Vec2d{}, 0.4, nil)
		},
	} {
		if got := testing.AllocsPerRun(100, call); got != 0 {
			t.Errorf("%s allocated %v times a call, want 0", name, got)
		}
	}
}

// collideAt is collide with an angle for each Shape, which is what a turned
// Polygon needs.
func collideAt(
	t testing.TB, a Shape, atA m.Vec2d, angleA float64, b Shape, atB m.Vec2d, angleB float64,
) (touching, bool) {
	t.Helper()
	return collideWithVerts(t, a, nil, atA, angleA, b, nil, atB, angleB)
}

// collideWithVerts is collide for the kinds that carry their vertices in a
// Polygon Component rather than inline.
func collideWithVerts(
	t testing.TB,
	a Shape, vertsA []m.Vec2d, atA m.Vec2d, angleA float64,
	b Shape, vertsB []m.Vec2d, atB m.Vec2d, angleB float64,
) (touching, bool) {
	t.Helper()
	var scratchA, scratchB [worldScratchLen]m.Vec2d
	worldA := worldRunFor(scratchA[:], a, vertsA)
	worldB := worldRunFor(scratchB[:], b, vertsB)
	transformA := NewTransformRigid(atA, angleA)
	transformB := NewTransformRigid(atB, angleB)
	usedA, _ := cacheWorldAt(a, transformA, vertsA, worldA)
	usedB, _ := cacheWorldAt(b, transformB, vertsB, worldB)
	return collideWorld(a, transformA, worldA[:usedA], b, transformB, worldB[:usedB], m.Vec2d{}, 0)
}

var touchSink touching
