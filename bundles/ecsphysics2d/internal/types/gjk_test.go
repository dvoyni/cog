package types

import (
	"fmt"
	"math"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// The two things the simplex itself has to get right, whichever arm in
// collide_test.go reached it: a degenerate simplex answers with a number, and
// the cached id is a starting guess that never changes the answer.

func TestADegenerateSimplexAnswersWithANumber(t *testing.T) {
	// The guard ClosestT carries, which jakecoffman/cp drops: two Shapes sharing
	// a centre give GJK a simplex whose points coincide, and the unguarded
	// divide is zero over zero. The finiteness invariant is that no input puts a
	// NaN into anything the plugin writes.
	box := NewBoxShape(1, 1, 0)
	point := NewCircleShape(0, m.Vec2d{})
	zeroSegment := NewSegmentShape(m.Vec2d{}, m.Vec2d{}, 0)

	// The closed forms are #428's and are not retested here; these are the four
	// pairs that reach GJK.
	for name, pair := range map[string][2]Shape{
		"a box on a box":                 {box, box},
		"a point inside a box":           {point, box},
		"a zero-length segment in a box": {zeroSegment, box},
		"two zero-length segments":       {zeroSegment, zeroSegment},
	} {
		touch, ok := collide(t, pair[0], m.Vec2d{}, pair[1], m.Vec2d{})
		if !ok {
			continue
		}
		if math.IsNaN(touch.normal.X) || math.IsNaN(touch.normal.Y) {
			t.Errorf("%s gave the normal %v", name, touch.normal)
		}
		for i := range touch.count {
			p := touch.points[i]
			if math.IsNaN(p.depth) || math.IsInf(p.depth, 0) ||
				math.IsNaN(p.p1.X) || math.IsNaN(p.p1.Y) ||
				math.IsNaN(p.p2.X) || math.IsNaN(p.p2.Y) {
				t.Errorf("%s point %d is %+v", name, i, p)
			}
		}
	}
}

func TestTheCachedSimplexWarmStartsTheNextTickAndAnswersTheSame(t *testing.T) {
	// cp's collisionId. A cached id is a starting guess and nothing more, so the
	// answer must not depend on it — including when the id belonged to the pair
	// the other way round, which the port's per-tick index slots can do and
	// cp's fixed arbiter order cannot.
	box := NewBoxShape(1, 1, 0)
	var worldA, worldB [worldScratchLen]m.Vec2d
	transformA := NewTransformRigid(m.Vec2d{}, 0)
	transformB := NewTransformRigid(m.Vec2d{X: 0.1, Y: 0.9}, 0)
	usedA, _ := cacheWorldAt(box, transformA, nil, worldA[:])
	usedB, _ := cacheWorldAt(box, transformB, nil, worldB[:])

	cold, ok := collideWorld(box, transformA, worldA[:usedA], box, transformB, worldB[:usedB], m.Vec2d{}, 0)
	if !ok {
		t.Fatal("two stacked boxes do not touch")
	}
	if cold.gjkId == 0 {
		t.Fatal("a GJK pair came back with no simplex to warm start from")
	}

	// A simplex the pair itself produced, and the same one as the other party
	// would have packed it, which is what the port's per-tick index slots can
	// hand back when the two Bodies change places in the walk.
	for name, cached := range map[string]uint32{
		"its own simplex": cold.gjkId,
		// The same two Minkowski points as the pair the other way round would
		// have packed them: the two support indices swap inside each half.
		"a mirrored simplex": (cold.gjkId&0x00FF00FF)<<8 | (cold.gjkId&0xFF00FF00)>>8,
	} {
		warm, ok := collideWorld(
			box, transformA, worldA[:usedA], box, transformB, worldB[:usedB], m.Vec2d{}, cached)
		if !ok {
			t.Fatalf("warm started from %s the pair stopped touching", name)
		}
		if warm.count != cold.count || !vecNear(warm.normal, cold.normal) {
			t.Fatalf("warm started from %s the pair is %d points along %v, want %d along %v",
				name, warm.count, warm.normal, cold.count, cold.normal)
		}
		for i := range cold.count {
			if !near(warm.points[i].depth, cold.points[i].depth) {
				t.Errorf("warm started from %s point %d is %v deep, want %v",
					name, i, warm.points[i].depth, cold.points[i].depth)
			}
		}
	}

	// An id naming vertices the Shape does not have is cp's own clamp, which
	// puts every index at 0 and hands GJK a simplex whose two points coincide.
	// cp's answer there is a poor one and so is the port's; what is required of
	// it is that it is a number, which is what ClosestT's restored CPFLOAT_MIN
	// buys. No id the port itself packs can be out of range: every byte of one
	// is a vertex index of the Shape that produced it.
	wild, ok := collideWorld(
		box, transformA, worldA[:usedA], box, transformB, worldB[:usedB], m.Vec2d{}, 0xFFFFFFFF)
	if ok {
		if math.IsNaN(wild.normal.X) || math.IsNaN(wild.normal.Y) {
			t.Errorf("an out-of-range simplex gave the normal %v", wild.normal)
		}
		for i := range wild.count {
			if math.IsNaN(wild.points[i].depth) || math.IsInf(wild.points[i].depth, 0) {
				t.Errorf("an out-of-range simplex gave point %d the depth %v",
					i, wild.points[i].depth)
			}
		}
	}
}

// The supporting line of a Minkowski edge. When GJK stops on an edge whose
// nearest point to the origin is clamped to one of its ends, the origin is
// beyond the edge; if it also lies on the line through the edge, cp's closestTo
// measures d to that line, finds it 0 or a rounding below, and takes its
// overlapping arm. Two disjoint Shapes then come back touching, at a depth of
// their summed radii. The same family has two more forms on one line: a simplex
// collapsed to one Minkowski point, and a flat triangle the orientation tests
// read as holding the origin. Each test below is a sequence that failed.

// supportingLineGaps are the surface-to-surface gaps of the end-to-end sweep, in
// metres, and supportingLineAngles how many angles it turns the pair through.
var supportingLineGaps = []float64{0.05, 0.1, 0.125, 0.15, 0.2}

const supportingLineAngles = 3600

// detectTwoBodies is Detect over two Bodies and nothing else: a fresh pair of
// indices, one Collide, and the Contacts it wrote.
func detectTwoBodies(
	a Shape, atA m.Vec2d, angleA float64, vertsA []m.Vec2d,
	b Shape, atB m.Vec2d, angleB float64, vertsB []m.Vec2d,
) int {
	contacts := NewContacts(7)
	bodies, statics := NewBodyIndex(0), NewStaticIndex(0)
	bodies.Insert(ecs.Entity(1), a, atA, angleA, vertsA)
	bodies.Insert(ecs.Entity(2), b, atB, angleB, vertsB)
	Collide(contacts, bodies, statics, noJoints, 3)
	return contacts.Len()
}

// TestTwoCapsulesEndToEndOnOneLineAreNeverReportedTouching is the sweep that
// showed the engine reaches the defect. Two capsules 1 m long with radius 0.25
// lie end to end on one line, their surfaces a few centimetres apart, turned
// through 3,600 angles. Their Minkowski difference is a thin parallelogram on
// a line through the origin, and it reaches all three forms: GJK stops on it
// with t clamped, or collapses to one point on the cold-start axis, or hands a
// flat triangle to EPA.
//
// Before the guards, 1,410 of the 3,660 placements whose boxes intersect were
// reported touching, 548 of them deeper than 0.25 m, and Detect made 1,410
// Contacts of them; 6,692 of all 18,000 touched through Penetration. Of the
// 3,660, the clamp guard alone left 863, the collapsed-simplex search then 1 (9
// of the 18,000), and the EPA-entry test none.
func TestTwoCapsulesEndToEndOnOneLineAreNeverReportedTouching(t *testing.T) {
	capsule := NewSegmentShape(m.Vec2d{X: -0.5}, m.Vec2d{X: 0.5}, 0.25)
	shape := finitenessShape{name: "a capsule", shape: capsule}

	var placed, gated, touching, gatedTouching, falseDepth, detected int
	var first string
	for _, gap := range supportingLineGaps {
		for i := range supportingLineAngles {
			angle := float64(i) * (2 * math.Pi / supportingLineAngles)
			atB := m.ForAngle(angle).MulS(1.5 + gap)
			placed++

			_, boxA := finitenessWorld(shape, m.Vec2d{}, angle)
			_, boxB := finitenessWorld(shape, atB, angle)
			boxes := boxA.Intersects(boxB)
			if boxes {
				gated++
			}

			_, depth, ok := Penetration(capsule, m.Vec2d{}, angle, nil, capsule, atB, angle, nil)
			_, backDepth, backOK := Penetration(capsule, atB, angle, nil, capsule, m.Vec2d{}, angle, nil)
			if ok || backOK {
				touching++
				if boxes {
					gatedTouching++
					if math.Max(depth, backDepth) > 0.25 {
						falseDepth++
					}
				}
				if first == "" {
					first = fmt.Sprintf("gap %v at angle %v: touching %v at depth %v one way, "+
						"%v at depth %v the other", gap, angle, ok, depth, backOK, backDepth)
				}
			}

			if boxes {
				detected += detectTwoBodies(capsule, m.Vec2d{}, angle, nil, capsule, atB, angle, nil)
			}
		}
	}

	t.Logf("%d placements, %d reported touching through Penetration; %d with boxes the "+
		"broadphase admits, %d of those touching (%d deeper than 0.25 m) and %d Contacts "+
		"through Detect", placed, touching, gated, gatedTouching, falseDepth, detected)
	if gated == 0 {
		t.Fatal("no placement had intersecting boxes, so the sweep says nothing about " +
			"what reaches Detect")
	}
	if touching > 0 {
		t.Errorf("%d of %d disjoint end-to-end capsule pairs are reported touching; the first "+
			"is %s", touching, placed, first)
	}
	if detected > 0 {
		t.Errorf("Detect made %d Contacts between disjoint end-to-end capsules", detected)
	}
}

// TestTheResearchHeadlinePairIsNotTouchingEitherWayRound is draw 5491 of the
// finiteness fuzz, the pair the research traced inside Chipmunk's C: a capsule
// at (−1, 1.5) turned π/2, standing on x = −1 from y = 1 to 2, and a segment at
// (1.25, 1) lying on y = 1 from x = 0.75 to 1.75. The capsule's surface is 1.5 m
// from the segment, and the Minkowski difference's top edge lies on y = 0
// starting 1.75 to the right of the origin. C reports two contacts at 0.25 m.
func TestTheResearchHeadlinePairIsNotTouchingEitherWayRound(t *testing.T) {
	capsule := NewSegmentShape(m.Vec2d{X: -0.5}, m.Vec2d{X: 0.5}, 0.25)
	atCapsule := m.Vec2d{X: -1, Y: 1.5}
	atSegment := m.Vec2d{X: 1.25, Y: 1}

	for name, segment := range map[string]Shape{
		"a segment": NewSegmentShape(m.Vec2d{X: -0.5}, m.Vec2d{X: 0.5}, 0),
		// The catalogue's own B at that draw, whose end-cap rejection made the
		// one-way answer go the other way round from C's.
		"a chained segment": NewSegmentShapeWithNeighbours(
			m.Vec2d{X: -1}, m.Vec2d{X: -0.5}, m.Vec2d{X: 0.5}, m.Vec2d{X: 1}, 0),
	} {
		_, depth, ok := Penetration(capsule, atCapsule, math.Pi/2, nil, segment, atSegment, 0, nil)
		if ok {
			t.Errorf("the capsule against %s is reported touching at depth %v", name, depth)
		}
		_, depth, ok = Penetration(segment, atSegment, 0, nil, capsule, atCapsule, math.Pi/2, nil)
		if ok {
			t.Errorf("%s against the capsule is reported touching at depth %v", name, depth)
		}
	}
}

// TestACollapsedSimplexSearchesAlongItsOnePointAndPartsTwoCollinearShapes is
// draw 4094 of the finiteness fuzz, the collapsed form the research traced
// inside C master: a segment at (−0.25, −1.25) and a capsule of radius 0.25 at
// (2, −1.25), both lying on y = −1.25, 1.25 m apart core to core. The cold-start
// axis is perpendicular to that line, every support query ties, and the simplex
// is the one Minkowski point (2.25, 0). C master's GJK then searches along the
// perpendicular of the zero vector, stops, and reports two contacts with a zero
// normal; C 7.0.2 reaches the clamped edge instead and reports a false 0.25 m.
//
// The coincident-centres rule is the other half and must not move: a zero
// cold-start axis is a collapse too, and there the pure Penetration still
// withholds the direction and a circle concentric with a box still overlaps.
func TestACollapsedSimplexSearchesAlongItsOnePointAndPartsTwoCollinearShapes(t *testing.T) {
	segment := NewSegmentShape(m.Vec2d{X: -0.5}, m.Vec2d{X: 0.5}, 0)
	capsule := NewSegmentShape(m.Vec2d{X: -0.5}, m.Vec2d{X: 0.5}, 0.25)
	atSegment := m.Vec2d{X: -0.25, Y: -1.25}
	atCapsule := m.Vec2d{X: 2, Y: -1.25}

	if _, depth, ok := Penetration(segment, atSegment, 0, nil, capsule, atCapsule, 0, nil); ok {
		t.Errorf("the segment against the capsule is reported touching at depth %v", depth)
	}
	if _, depth, ok := Penetration(capsule, atCapsule, 0, nil, segment, atSegment, 0, nil); ok {
		t.Errorf("the capsule against the segment is reported touching at depth %v", depth)
	}

	// The coincident-centres rule, unchanged.
	circle := NewCircleShape(0.5, m.Vec2d{})
	box := NewBoxShape(1, 1, 0)
	normal, depth, ok := Penetration(circle, m.Vec2d{X: 3}, 0, nil, box, m.Vec2d{X: 3}, 0, nil)
	if !ok {
		t.Fatal("a circle concentric with a box does not overlap it")
	}
	if normal != (m.Vec2d{}) {
		t.Errorf("a circle concentric with a box parts along %v through the pure Penetration, "+
			"want no direction", normal)
	}
	if depth < 0 {
		t.Errorf("a circle concentric with a box overlaps it by %v", depth)
	}
}

// endToEndTriangle is a right triangle whose apex is at the Body's origin and
// whose top edge runs back from it along the local x axis, so a Shape laid on
// that axis ahead of the apex is end to end with it on one line.
func endToEndTriangle(t testing.TB) Shape {
	t.Helper()
	triangle, _, err := NewPolygonShape([]m.Vec2d{{}, {X: -1}, {X: -1, Y: -1}}, 0)
	if err != nil {
		t.Fatalf("hulling the triangle: %v", err)
	}
	return triangle
}

// TestAnExactVertexToVertexTangencyStillTouchesAlongAUnitNormal is the case the
// guard leaves alone. When the two Shapes meet at a vertex, the Minkowski
// difference's nearest point is the origin itself: p is the zero vector, there
// is no p/|p| to take, and closestTo keeps the edge's normal and its d of 0 so
// that the contact still has a direction.
func TestAnExactVertexToVertexTangencyStillTouchesAlongAUnitNormal(t *testing.T) {
	box := NewBoxShape(1, 1, 0)
	segment := NewSegmentShape(m.Vec2d{X: -0.5}, m.Vec2d{X: 0.5}, 0)

	for _, c := range []struct {
		name     string
		a, b     Shape
		atA, atB m.Vec2d
	}{
		{"two boxes corner to corner", box, box, m.Vec2d{}, m.Vec2d{X: 1, Y: 1}},
		// Clamped as well as at the origin: the Minkowski edge the apex makes
		// with the box's bottom face runs along the line through the origin and
		// ends on it.
		{"a triangle's apex on a box's corner, end to end on one line",
			endToEndTriangle(t), box, m.Vec2d{}, m.Vec2d{X: 0.5, Y: 0.5}},
		// The collapsed simplex at a touch. Both segments lie on y = 0, so the
		// cold-start axis ties every support query and the simplex is one
		// Minkowski point; before the search along −p it stopped there with a
		// zero normal, as C master does. Searching along −p reaches the edge
		// from (0, 0) to (1, 0), whose normal is a unit one, as C 7.0.2's is.
		{"two segments meeting end to end", segment, segment, m.Vec2d{}, m.Vec2d{X: 1}},
	} {
		for _, flip := range []bool{false, true} {
			a, atA, b, atB := c.a, c.atA, c.b, c.atB
			if flip {
				a, atA, b, atB = b, atB, a, atA
			}
			normal, depth, ok := Penetration(a, atA, 0, nil, b, atB, 0, nil)
			if !ok {
				t.Errorf("%s (flipped %v) is not reported touching", c.name, flip)
				continue
			}
			if depth != 0 {
				t.Errorf("%s (flipped %v) touches at depth %v, want 0", c.name, flip, depth)
			}
			if math.Abs(normal.Length()-1) > cpTolerance {
				t.Errorf("%s (flipped %v) touches along %v, which is not a unit normal",
					c.name, flip, normal)
			}
		}
	}
}

// TestAPolygonVertexOnTheLineOfAnotherPolygonsEdgeAndBeyondItDoesNotTouch shows
// the guard is not about segments. Two pairs of polygons, each turned through
// 3,600 angles with a gap between them, which is where rounding leaves d a hair
// below zero rather than exactly on it:
//
//   - a right triangle and a thin post 0.1 m wide end to end, the triangle's top
//     edge and the post's bottom edge on one line;
//   - two rounded boxes as two steps of a staircase, the lower one's top face on
//     the line of the upper one's bottom face.
//
// In both, a vertex of one Shape lies on the line of an edge of the other,
// beyond its end, and the Minkowski difference has an edge on a line through
// the origin. Before the guard, 415 of the triangle placements and 73 of the
// staircase ones were reported touching, and Detect made 12 Contacts of the
// staircase placements whose boxes intersect.
func TestAPolygonVertexOnTheLineOfAnotherPolygonsEdgeAndBeyondItDoesNotTouch(t *testing.T) {
	triangle := endToEndTriangle(t)
	step := NewBoxShape(1, 1, 0.05)

	var placed, touching, gated, detected int
	var first string
	report := func(what string, gap, angle, depth, backDepth float64, ok, backOK bool) {
		if !ok && !backOK {
			return
		}
		touching++
		if first == "" {
			first = fmt.Sprintf("%s, gap %v at angle %v: touching %v at depth %v one way, "+
				"%v at depth %v the other", what, gap, angle, ok, depth, backOK, backDepth)
		}
	}

	for _, gap := range supportingLineGaps {
		post := NewBoxShapeFor(NewBB(gap, 0, gap+0.1, 1), 0)
		for i := range supportingLineAngles {
			angle := float64(i) * (2 * math.Pi / supportingLineAngles)

			placed++
			_, depth, ok := Penetration(triangle, m.Vec2d{}, angle, nil, post, m.Vec2d{}, angle, nil)
			_, backDepth, backOK := Penetration(post, m.Vec2d{}, angle, nil, triangle, m.Vec2d{}, angle, nil)
			report("the triangle and the post", gap, angle, depth, backDepth, ok, backOK)

			placed++
			atB := m.Vec2d{X: 1.1 + gap, Y: 1}.Rotate(m.ForAngle(angle))
			_, depth, ok = Penetration(step, m.Vec2d{}, angle, nil, step, atB, angle, nil)
			_, backDepth, backOK = Penetration(step, atB, angle, nil, step, m.Vec2d{}, angle, nil)
			report("the two steps", gap, angle, depth, backDepth, ok, backOK)

			_, boxA := finitenessWorld(finitenessShape{shape: step}, m.Vec2d{}, angle)
			_, boxB := finitenessWorld(finitenessShape{shape: step}, atB, angle)
			if boxA.Intersects(boxB) {
				gated++
				detected += detectTwoBodies(step, m.Vec2d{}, angle, nil, step, atB, angle, nil)
			}
		}
	}

	t.Logf("%d placements, %d reported touching through Penetration; %d staircase placements "+
		"with boxes the broadphase admits, %d Contacts through Detect",
		placed, touching, gated, detected)
	if gated == 0 {
		t.Fatal("no staircase placement had intersecting boxes, so the sweep says nothing " +
			"about what reaches Detect")
	}
	if touching > 0 {
		t.Errorf("%d of %d disjoint polygon pairs are reported touching; the first is %s",
			touching, placed, first)
	}
	if detected > 0 {
		t.Errorf("Detect made %d Contacts between disjoint staircase steps", detected)
	}
}
