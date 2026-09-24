package internal

import (
	"math"

	"github.com/dvoyni/cog/libs/m"
)

// The narrowphase: which two Shapes touch, where, and along which normal. One
// dispatch over the two families and six arms under it, four of which run GJK
// and EPA from gjk.go and two of which are closed forms cp solves directly.
// The manifold they fill is touching.go.

// edgePoint is one end of a support edge. cp keeps a hash there, mixed from the
// Shape's pointer and the vertex index, and carries the comment that matching
// on it could trigger false positives; the port keeps the vertex index itself,
// because a Contact's A and B already fix the two Shapes and the id only has to
// tell one pair's at most two points apart.
type edgePoint struct {
	p     m.Vec2d
	index uint32
}

// edge is cp's Edge: the face of one Shape that faces the collision, its two
// ends in world space, its rounding radius and its outward normal.
type edge struct {
	a, b   edgePoint
	radius float64
	n      m.Vec2d
}

// penetrateWorld is Penetration over world caches already built: collideWorld
// with the two surface points dropped and no direction invented on coincident
// centres.
func penetrateWorld(
	a Shape, transformA Transform, worldA []m.Vec2d,
	b Shape, transformB Transform, worldB []m.Vec2d,
) (m.Vec2d, float64, bool) {
	touch, ok := collideWorld(a, transformA, worldA, b, transformB, worldB, m.Vec2d{}, 0)
	if !ok {
		return m.Vec2d{}, 0, false
	}
	return touch.normal, touch.points[0].depth, true
}

// collideWorld is cp's Collide over world caches already built. The pair
// dispatch is a switch on the family, in place of cp's [9]CollisionFunc table
// keyed by an Order() type switch over an interface; the switch is what sorts
// the two Shapes into cp's kind order — circle < segment < poly — so the caller
// may pass them either way round and always reads a normal from its own first
// Shape towards its second.
//
// coincident is the direction to take when the two Shapes are placed so that no
// separating direction is preferred over any other, where cp invents a fixed
// (1, 0) that never breaks symmetry. The zero vector asks for no direction at
// all, which is what the pure Penetration reports.
//
// It reaches every arm. The closed circle-against-circle form reads it when the
// two centres coincide; the four GJK arms hand it to gjk, which reads it when
// the two bounding-box centres coincide and its cold-start axis is therefore the
// zero vector. Both are the same placement seen through two different pieces of
// arithmetic, and one seed answers both — the specification's symmetry-breaking
// is a single mechanism living in Detect, not one per collision arm.
//
// cached is the previous tick's simplex for this pair, cp's collisionId, which
// the four GJK arms warm start from and the two closed forms ignore. It is
// always read in the kind order the switch sorts the pair into, so a caller
// whose own order changed between ticks still hands GJK a usable guess.
func collideWorld(
	a Shape, transformA Transform, worldA []m.Vec2d,
	b Shape, transformB Transform, worldB []m.Vec2d,
	coincident m.Vec2d, cached uint32,
) (touching, bool) {
	if len(worldA) == 0 || len(worldB) == 0 {
		return touching{}, false
	}

	// The nine-arm switch on the family, in place of cp's [9]CollisionFunc table
	// keyed by a.Order()+b.Order()*3 and the Order() type switch over an
	// interface that fills it. Three of the arms turn the answer round, which is
	// what sorts the pair into cp's kind order — circle < segment < poly — so
	// the caller may pass its two Shapes either way and always reads a normal
	// from its own first Shape towards its second.
	switch {
	case a.Kind == ShapeCircle && b.Kind == ShapeCircle:
		return collideCircles(worldA[0], a.Radius, worldB[0], b.Radius, coincident)

	case a.Kind == ShapeCircle && b.Kind == ShapeSegment:
		return collideCircleSegment(worldA[0], a.Radius, b, transformB, worldB)

	case a.Kind == ShapeSegment && b.Kind == ShapeCircle:
		return flipped(collideCircleSegment(worldB[0], b.Radius, a, transformA, worldA))

	case a.Kind == ShapeCircle && isPolygon(b.Kind):
		return collideCirclePoly(a, worldA, b, worldB, coincident, cached)

	case isPolygon(a.Kind) && b.Kind == ShapeCircle:
		return flipped(collideCirclePoly(b, worldB, a, worldA, coincident, cached))

	case a.Kind == ShapeSegment && b.Kind == ShapeSegment:
		return collideSegments(a, transformA, worldA, b, transformB, worldB, coincident, cached)

	case a.Kind == ShapeSegment && isPolygon(b.Kind):
		return collideSegmentPoly(a, transformA, worldA, b, worldB, coincident, cached)

	case isPolygon(a.Kind) && b.Kind == ShapeSegment:
		return flipped(collideSegmentPoly(b, transformB, worldB, a, worldA, coincident, cached))
	}

	return collidePolys(a, worldA, b, worldB, coincident, cached)
}

// flipped turns one arm's answer round: the normal flips and the two surface
// points swap, leaving the caller's own first Shape the one the normal points
// away from. The simplex id is left alone, being in the kind order the next
// tick's GJK will read it in.
//
// This is where cp's swapped flag went. cp carries it on the arbiter and pays
// for it in TotalImpulse's sign; the port flips once, here, and reads one way
// for ever after.
func flipped(touch touching, ok bool) (touching, bool) {
	if !ok {
		return touching{}, false
	}
	touch.normal = touch.normal.Negate()
	for i := range touch.count {
		touch.points[i].p1, touch.points[i].p2 = touch.points[i].p2, touch.points[i].p1
	}
	return touch, true
}

// collideCircles is cp's CircleToCircle.
func collideCircles(
	centreA m.Vec2d, radiusA float64, centreB m.Vec2d, radiusB float64, coincident m.Vec2d,
) (touching, bool) {
	minimum := radiusA + radiusB
	delta := centreB.Sub(centreA)
	squared := delta.LengthSquared()

	if squared >= minimum*minimum {
		return touching{}, false
	}

	distance := math.Sqrt(squared)
	// cp answers a fixed (1, 0) when the two centres coincide, which never
	// breaks the symmetry it is there to break. The direction is the caller's:
	// Detect hands in a seeded one and the pure Penetration hands in none, which
	// leaves the normal zero and both points at their own centre.
	normal := coincident
	if distance != 0 {
		normal = delta.MulS(1 / distance)
	}

	var touch touching
	touch.normal = normal
	touch.count = 1
	touch.points[0] = touchingPoint{
		p1:    centreA.Add(normal.MulS(radiusA)),
		p2:    centreB.Add(normal.MulS(-radiusB)),
		depth: minimum - distance,
		id:    pointID(0, 0),
	}
	return touch, true
}

// collideCircleSegment is cp's CircleToSegment, the circle first as cp's kind
// order requires.
func collideCircleSegment(
	centre m.Vec2d, radius float64,
	segment Shape, transform Transform, world []m.Vec2d,
) (touching, bool) {
	segA, segB, segNormal := world[0], world[1], world[2]

	segDelta := segB.Sub(segA)
	// The fourth site of defect 6, which the porting index's three do not name:
	// cp divides by the segment's own squared length unguarded, so a segment
	// whose two endpoints are the same point — which NewSegmentShape builds
	// without complaint — is clamp01(0/0), a NaN closestT, and a Contact whose
	// Normal, Points and Depth are every one of them NaN. It is not the
	// coincident-centre case: any circle at all against a zero-length segment
	// gets it. The guard is C's, the one the port already restores at the three
	// sites the index does name, and it is the same constant.
	//
	// Guarded, a zero-length segment is the circle it geometrically is: the
	// quotient is 0, closest is segA, and the pair is a circle-against-circle
	// test at the segment's own rounding radius. No other answer changes, so the
	// cp corpus reads exactly as before.
	//
	// The spelling is C's own `if (d > 0)` shape, which the port already uses on
	// the circle point query's divide, rather than the max(x, CPFLOAT_MIN) it
	// uses on the two denominators in probePoly and ClosestT. It is the same
	// guard, and the reason for the difference is measured rather than assumed.
	//
	// math.Max carries Go's NaN and signed-zero semantics and compiles to a call:
	// spelled that way this arm ran about 13% slower than the unguarded one —
	// 59.9 ns against 52.8 ns a pair — with BenchmarkCollideCircles beside it
	// flat at 26 ns, which is what says the difference is the guard and not the
	// run order. Spelled as the branch below it is free: 46.09 ns against the
	// unguarded 46.51 ns over two further interleaved rounds. All of it is
	// BenchmarkCollideCircleSegment, A and B built as separate binaries and run
	// alternately, on an AMD Ryzen 9 7950X3D under go1.27.1 windows/amd64 at
	// GOMAXPROCS=32 — a whole-frame benchmark here swings about ±10% by run
	// order, and these absolute numbers moved between rounds while the ratio
	// within a round did not.
	var closestT float64
	if lengthSquared := segDelta.LengthSquared(); lengthSquared > 0 {
		closestT = clamp01(segDelta.Dot(centre.Sub(segA)) / lengthSquared)
	}
	closest := segA.Add(segDelta.MulS(closestT))

	minimum := radius + segment.Radius
	delta := closest.Sub(centre)
	squared := delta.LengthSquared()
	if squared >= minimum*minimum {
		return touching{}, false
	}

	distance := math.Sqrt(squared)
	// cp's fallback here is geometry rather than a coin flip — the segment's own
	// normal — and ports as written.
	normal := segNormal
	if distance != 0 {
		normal = delta.MulS(1 / distance)
	}

	// cp's end-cap rejection, which keeps a circle rolling along a chain of
	// segments from catching at a joint. It reads the neighbours' tangents,
	// rotated on use as cp does, and they are zero until the segment-chain
	// constructor sets them — in cp nothing ever sets them, which is defect 4.
	rotation := m.Vec2d{X: transform.A, Y: transform.B}
	if (closestT != 0 || normal.Dot(segment.Verts[2].Rotate(rotation)) >= 0) &&
		(closestT != 1 || normal.Dot(segment.Verts[3].Rotate(rotation)) >= 0) {
		var touch touching
		touch.normal = normal
		touch.count = 1
		touch.points[0] = touchingPoint{
			p1:    centre.Add(normal.MulS(radius)),
			p2:    closest.Add(normal.MulS(-segment.Radius)),
			depth: minimum - distance,
			id:    pointID(0, 0),
		}
		return touch, true
	}
	return touching{}, false
}

// collideCirclePoly is cp's CircleToPoly, the circle first as cp's kind order
// requires.
//
// The radius sign is defect 1: cp offsets the Polygon's surface point by
// +poly.r where C has −poly->r, which settles a circle about 2·r too deeply
// into a rounded Polygon. C's sign is the one here. It is harmless at Polygon
// radius 0, which is why nobody has met it.
func collideCirclePoly(
	circle Shape, worldCircle []m.Vec2d,
	poly Shape, worldPoly []m.Vec2d,
	coincident m.Vec2d, cached uint32,
) (touching, bool) {
	ctx := support{worldA: worldCircle, worldB: worldPoly, kindA: ShapeCircle, kindB: poly.Kind}
	points := gjk(ctx,
		boxForWorld(circle, worldCircle).Centre(),
		boxForWorld(poly, worldPoly).Centre(), coincident, cached)

	if points.d > circle.Radius+poly.Radius {
		return touching{}, false
	}

	var touch touching
	touch.normal = points.n
	touch.gjkId = points.id
	p1 := points.a.Add(points.n.MulS(circle.Radius))
	p2 := points.b.Add(points.n.MulS(-poly.Radius))
	touch.push(p1, p2, -p2.Sub(p1).Dot(points.n), pointID(0, 0))
	return touch, true
}

// collideSegments is cp's SegmentToSegment. Its end-cap rejection reads the
// neighbours' tangents, which are dead code in cp because nothing there ever
// writes them — defect 4 — and are live here, written by the segment
// constructor that takes the previous and next point.
func collideSegments(
	a Shape, transformA Transform, worldA []m.Vec2d,
	b Shape, transformB Transform, worldB []m.Vec2d,
	coincident m.Vec2d, cached uint32,
) (touching, bool) {
	ctx := support{worldA: worldA, worldB: worldB, kindA: ShapeSegment, kindB: ShapeSegment}
	points := gjk(ctx,
		boxForWorld(a, worldA).Centre(),
		boxForWorld(b, worldB).Centre(), coincident, cached)

	if points.d > a.Radius+b.Radius {
		return touching{}, false
	}

	n := points.n
	rotA, rotB := rotationOf(transformA), rotationOf(transformB)
	if (points.a != worldA[0] || n.Dot(a.Verts[2].Rotate(rotA)) <= 0) &&
		(points.a != worldA[1] || n.Dot(a.Verts[3].Rotate(rotA)) <= 0) &&
		(points.b != worldB[0] || n.Dot(b.Verts[2].Rotate(rotB)) >= 0) &&
		(points.b != worldB[1] || n.Dot(b.Verts[3].Rotate(rotB)) >= 0) {
		touch := touching{gjkId: points.id}
		if contactPoints(
			supportEdgeForSegment(a.Radius, worldA, n),
			supportEdgeForSegment(b.Radius, worldB, n.Negate()),
			points, &touch,
		) {
			return touch, true
		}
	}
	return touching{}, false
}

// collideSegmentPoly is cp's SegmentToPoly, the segment first as cp's kind
// order requires.
func collideSegmentPoly(
	segment Shape, transform Transform, worldSegment []m.Vec2d,
	poly Shape, worldPoly []m.Vec2d,
	coincident m.Vec2d, cached uint32,
) (touching, bool) {
	ctx := support{
		worldA: worldSegment, worldB: worldPoly,
		kindA: ShapeSegment, kindB: poly.Kind,
	}
	points := gjk(ctx,
		boxForWorld(segment, worldSegment).Centre(),
		boxForWorld(poly, worldPoly).Centre(), coincident, cached)

	n := points.n
	rotation := rotationOf(transform)
	if points.d-segment.Radius-poly.Radius <= 0 &&
		(points.a != worldSegment[0] || n.Dot(segment.Verts[2].Rotate(rotation)) <= 0) &&
		(points.a != worldSegment[1] || n.Dot(segment.Verts[3].Rotate(rotation)) <= 0) {
		touch := touching{gjkId: points.id}
		if contactPoints(
			supportEdgeForSegment(segment.Radius, worldSegment, n),
			supportEdgeForPoly(poly.Radius, worldPoly, n.Negate()),
			points, &touch,
		) {
			return touch, true
		}
	}
	return touching{}, false
}

// collidePolys is cp's PolyToPoly, and is what makes boxes stack.
func collidePolys(
	a Shape, worldA []m.Vec2d,
	b Shape, worldB []m.Vec2d,
	coincident m.Vec2d, cached uint32,
) (touching, bool) {
	ctx := support{worldA: worldA, worldB: worldB, kindA: a.Kind, kindB: b.Kind}
	points := gjk(ctx,
		boxForWorld(a, worldA).Centre(),
		boxForWorld(b, worldB).Centre(), coincident, cached)

	if points.d-a.Radius-b.Radius > 0 {
		return touching{}, false
	}

	touch := touching{gjkId: points.id}
	if contactPoints(
		supportEdgeForPoly(a.Radius, worldA, points.n),
		supportEdgeForPoly(b.Radius, worldB, points.n.Negate()),
		points, &touch,
	) {
		return touch, true
	}
	return touching{}, false
}

// supportEdgeForSegment is cp's SupportEdgeForSegment, over the world cache.
func supportEdgeForSegment(radius float64, world []m.Vec2d, n m.Vec2d) edge {
	segA, segB, segN := world[0], world[1], world[2]
	if segN.Dot(n) > 0 {
		return edge{
			a:      edgePoint{p: segA, index: 0},
			b:      edgePoint{p: segB, index: 1},
			radius: radius,
			n:      segN,
		}
	}
	return edge{
		a:      edgePoint{p: segB, index: 1},
		b:      edgePoint{p: segA, index: 0},
		radius: radius,
		n:      segN.Negate(),
	}
}

// supportEdgeForPoly is cp's SupportEdgeForPoly, over the world cache: whichever
// of the two faces meeting the support vertex faces the collision more squarely.
func supportEdgeForPoly(radius float64, world []m.Vec2d, n m.Vec2d) edge {
	verts, normals := polyWorld(world)
	count := len(verts)

	i1 := polySupportIndex(verts, n)
	i0 := (i1 - 1 + count) % count
	i2 := (i1 + 1) % count

	if n.Dot(normals[i1]) > n.Dot(normals[i2]) {
		return edge{
			a:      edgePoint{p: verts[i0], index: uint32(i0)},
			b:      edgePoint{p: verts[i1], index: uint32(i1)},
			radius: radius,
			n:      normals[i1],
		}
	}
	return edge{
		a:      edgePoint{p: verts[i1], index: uint32(i1)},
		b:      edgePoint{p: verts[i2], index: uint32(i2)},
		radius: radius,
		n:      normals[i2],
	}
}

// contactPoints is cp's ContactPoints: the at most two points where two support
// edges' surfaces meet, each end of each edge projected onto the other and
// clamped to it.
//
// cp's two 1e-15 denominators are C's CPFLOAT_MIN, weakened in translation;
// the port restores C's value, which is defect 6.
func contactPoints(e1, e2 edge, points closestPoints, touch *touching) bool {
	if points.d > e1.radius+e2.radius {
		return false
	}

	n := points.n
	touch.normal = n

	dE1A := e1.a.p.Cross(n)
	dE1B := e1.b.p.Cross(n)
	dE2A := e2.a.p.Cross(n)
	dE2B := e2.b.p.Cross(n)

	e1Denom := 1 / (dE1B - dE1A + smallestNormal)
	e2Denom := 1 / (dE2B - dE2A + smallestNormal)

	{
		p1 := n.MulS(e1.radius).Add(e1.a.p.Lerp(e1.b.p, clamp01((dE2B-dE1A)*e1Denom)))
		p2 := n.MulS(-e2.radius).Add(e2.a.p.Lerp(e2.b.p, clamp01((dE1A-dE2A)*e2Denom)))
		if depth := -p2.Sub(p1).Dot(n); depth >= 0 {
			touch.push(p1, p2, depth, pointID(e1.a.index, e2.b.index))
		}
	}
	{
		p1 := n.MulS(e1.radius).Add(e1.a.p.Lerp(e1.b.p, clamp01((dE2A-dE1A)*e1Denom)))
		p2 := n.MulS(-e2.radius).Add(e2.a.p.Lerp(e2.b.p, clamp01((dE1B-dE2A)*e2Denom)))
		if depth := -p2.Sub(p1).Dot(n); depth >= 0 {
			touch.push(p1, p2, depth, pointID(e1.b.index, e2.a.index))
		}
	}

	return touch.count > 0
}

// rotationOf is a rigid transform's rotation as the vector Rotate takes, which
// is what the segment neighbours' local tangents are turned by on use. cp's
// CacheData never transforms them, and neither does this.
func rotationOf(transform Transform) m.Vec2d {
	return m.Vec2d{X: transform.A, Y: transform.B}
}
