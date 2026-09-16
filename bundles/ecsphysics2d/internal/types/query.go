package types

import (
	"math"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// Hit is what a Probe reports: which Entity was met, how far along the Probe,
// and where and which way the surface faces there.
//
// T is a fraction of from towards to, in [0, 1], and carries no unit, so
// snap-back is from.Lerp(to, T). Point is on the hit Shape's surface. Normal is
// a unit vector facing the Prober, which is what a reflection negates against
// and what a snap-back moves along.
//
// A Probe that starts overlapping reports T = 0 with the normal from the
// nearest surface point, which is why spawn validation is a Probe of zero
// length. Entity is zero on a Hit a pair primitive reports, there being no
// index behind it to name one.
type Hit struct {
	Entity ecs.Entity
	T      float64
	Point  m.Vec2d
	Normal m.Vec2d
}

// worldScratchVerts is how many polygon vertices a query caches on its own
// stack, and worldScratchLen the vectors that takes: a vertex and a face normal
// each. A circle needs one vector and a segment three, so the polygon bound is
// what sizes the scratch.
//
// There is no vertex cap on a Polygon — cp has none either — so this is a
// threshold and not a limit: past it a query allocates one run, which is the
// only allocation on the query surface and is off the step's hot path, where
// the index's own slab holds the cache and is grown once. It is C's
// CP_POLY_SHAPE_INLINE_ALLOC played at the query rather than at the Shape.
const (
	worldScratchVerts = 32
	worldScratchLen   = 2 * worldScratchVerts
)

// worldLenFor is how many slab vectors a Shape's world cache occupies. verts is
// the Polygon Component's vertices, which only ShapePoly reads: every other
// kind carries its own count in its kind.
func worldLenFor(shape Shape, verts []m.Vec2d) int {
	switch shape.Kind {
	case ShapeCircle:
		return 1
	case ShapeSegment:
		return 3
	}
	return 2 * polyCount(shape, verts)
}

// cacheWorld places a Shape in the world and writes its world-space geometry
// into dst, returning how much of dst it used and the Shape's bounding box.
//
// This is cp's CacheData for each kind, with cp's per-class caches — a circle's
// tc, a segment's ta, tb and tn — written into a caller-owned run instead of
// into the shape object, because a Shape is a Component and a world cache is
// derived data an index entry holds.
//
// verts is the Polygon Component's vertices and is nil for every kind but Poly,
// which a Shape cannot carry inline.
func cacheWorld(shape Shape, at m.Vec2d, angle float64, verts []m.Vec2d, dst []m.Vec2d) (int, BB) {
	return cacheWorldAt(shape, NewTransformRigid(at, angle), verts, dst)
}

// cacheWorldAt is cacheWorld for a transform already built, which is what an
// index entry keeps beside its bounding box.
func cacheWorldAt(shape Shape, transform Transform, verts []m.Vec2d, dst []m.Vec2d) (int, BB) {
	switch shape.Kind {
	case ShapeCircle:
		dst[0] = transform.Point(shape.verts[0])
		return 1, boxForWorld(shape, dst[:1])

	case ShapeSegment:
		a := transform.Point(shape.verts[0])
		b := transform.Point(shape.verts[1])
		dst[0] = a
		dst[1] = b
		// cp keeps the local normal on the segment and transforms it here. The
		// port derives it, because a Shape stores no local normal: one
		// Normalize an edge, and a static Entity pays it once at insert.
		//
		// The sign is cp's constructor's, ReversePerp. cp's SetEndpoints uses
		// Perp, 180 degrees away, and so does C; the port has one sign, which
		// is a departure from Chipmunk itself rather than a defect fixed.
		dst[2] = b.Sub(a).Normalize().ReversePerp()
		return 3, boxForWorld(shape, dst[:3])
	}

	count := polyCount(shape, verts)
	if count == 0 || 2*count > len(dst) {
		return 0, BB{}
	}
	worldVerts, worldNormals := polyWorld(dst[:2*count])
	for i := range count {
		worldVerts[i] = transform.Point(polyVert(&shape, verts, i))
	}
	// Local normals are not stored, so cp's transform.Vect of a stored plane
	// normal becomes one Normalize an edge here — and a static Polygon pays it
	// once, at insert. The two agree: the transform is rigid, so rotating a
	// unit normal and normalizing the rotated edge are the same vector.
	//
	// Normal i belongs to the edge from vertex i−1 to vertex i, which is cp's
	// SetVerts plane order, and the winding the hulling constructor enforces is
	// what makes it face outward.
	for i := range count {
		worldNormals[i] = worldVerts[i].Sub(worldVerts[(i-1+count)%count]).ReversePerp().Normalize()
	}
	return 2 * count, boxForWorld(shape, dst[:2*count])
}

// boxForWorld is the box a placed Shape occupies, from its world cache: cp's
// CacheData box for each kind, grown by the rounding radius.
//
// It is a function of its own because GJK's cold start guesses its axis from
// the two boxes' centres, and a pair primitive holds no box of its own.
func boxForWorld(shape Shape, world []m.Vec2d) BB {
	switch shape.Kind {
	case ShapeCircle:
		return NewBBForCircle(world[0], shape.Radius)
	case ShapeSegment:
		world = world[:2]
	default:
		world, _ = polyWorld(world)
	}

	left, bottom := infinity, infinity
	right, top := -infinity, -infinity
	for _, v := range world {
		left, right = math.Min(left, v.X), math.Max(right, v.X)
		bottom, top = math.Min(bottom, v.Y), math.Max(top, v.Y)
	}
	radius := shape.Radius
	return NewBB(left-radius, bottom-radius, right+radius, top+radius)
}

// ProbeShape moves a circle of that radius from one point to another and
// reports the first Hit on one Shape placed at a position and an angle. It is
// the pair primitive behind an index's Probe, over values and touching no
// engine state, and its Hit names no Entity.
//
// verts is the Polygon Component's vertices and is nil for every kind but Poly.
func ProbeShape(
	from, to m.Vec2d, radius float64,
	shape Shape, at m.Vec2d, angle float64, verts []m.Vec2d,
) (Hit, bool) {
	var scratch [worldScratchLen]m.Vec2d
	world := worldRunFor(scratch[:], shape, verts)
	transform := NewTransformRigid(at, angle)
	used, _ := cacheWorldAt(shape, transform, verts, world)
	return probeWorld(from, to, radius, shape, world[:used])
}

// worldRunFor is the run one Shape's world cache is built in: the caller's own
// stack scratch wherever it fits, which is every kind but a Polygon of more
// than worldScratchVerts vertices, and a fresh run past that.
//
// That fresh run is the one allocation on the query surface. It is not on the
// step's hot path: detection reads the index's slab, which is grown once and
// kept across Clear.
func worldRunFor(scratch []m.Vec2d, shape Shape, verts []m.Vec2d) []m.Vec2d {
	if needed := worldLenFor(shape, verts); needed > len(scratch) {
		return make([]m.Vec2d, needed)
	}
	return scratch
}

// ClosestPoint is the point on a Shape's surface nearest a point, the Shape
// placed at a position and an angle. A point inside the Shape still gets the
// nearest surface point.
//
// verts is the Polygon Component's vertices and is nil for every kind but Poly.
func ClosestPoint(p m.Vec2d, shape Shape, at m.Vec2d, angle float64, verts []m.Vec2d) m.Vec2d {
	var scratch [worldScratchLen]m.Vec2d
	world := worldRunFor(scratch[:], shape, verts)
	transform := NewTransformRigid(at, angle)
	used, _ := cacheWorldAt(shape, transform, verts, world)
	point, _, _ := pointQueryWorld(p, shape, world[:used])
	return point
}

// Penetration is how deeply two placed Shapes overlap and which way apart, or
// false when they do not. There is no Overlaps boolean beside it: this ok is
// it.
//
// The normal points from a towards b, which is cp's own sense, and the depth is
// how far one would move along it to part them. On coincident centres the
// normal is zero and ok is true: choosing a direction is the caller's, and a
// pure function takes no randomness — where cp invents a fixed (1, 0) that
// never breaks symmetry.
//
// vertsA and vertsB are the Polygon Components' vertices and are nil for every
// kind but Poly.
func Penetration(
	a Shape, atA m.Vec2d, angleA float64, vertsA []m.Vec2d,
	b Shape, atB m.Vec2d, angleB float64, vertsB []m.Vec2d,
) (m.Vec2d, float64, bool) {
	var scratchA, scratchB [worldScratchLen]m.Vec2d
	worldA := worldRunFor(scratchA[:], a, vertsA)
	worldB := worldRunFor(scratchB[:], b, vertsB)
	transformA := NewTransformRigid(atA, angleA)
	transformB := NewTransformRigid(atB, angleB)
	usedA, _ := cacheWorldAt(a, transformA, vertsA, worldA)
	usedB, _ := cacheWorldAt(b, transformB, vertsB, worldB)
	return penetrateWorld(a, transformA, worldA[:usedA], b, transformB, worldB[:usedB])
}

// probeWorld is ProbeShape over a world cache already built.
func probeWorld(from, to m.Vec2d, radius float64, shape Shape, world []m.Vec2d) (Hit, bool) {
	if len(world) == 0 {
		return Hit{}, false
	}

	// A Probe that starts overlapping reports a Hit at T = 0, with the normal
	// from the nearest surface point. cp's segment queries report nothing here,
	// the root they solve for being negative; ignoring what a Probe starts
	// inside would let a projectile leave a wall it spawned in and would pass
	// spawn validation silently.
	//
	// The test is strict, as cp's collisions are, so a Probe exactly touching a
	// surface is not already inside it and a point never Hits a point.
	point, distance, gradient := pointQueryWorld(from, shape, world)
	if distance < radius {
		return Hit{T: 0, Point: point, Normal: gradient}, true
	}

	switch shape.Kind {
	case ShapeCircle:
		return probeCircle(world[0], shape.Radius, from, to, radius)
	case ShapeSegment:
		return probeSegment(shape, world, from, to, radius)
	}
	return probePoly(shape, world, from, to, radius)
}

// probeCircle is cp's CircleSegmentQuery: the swept circle against a circle,
// solved as a quadratic in the Probe's fraction.
//
// A zero-length Probe leaves qa at 0 and t a NaN, which both range tests reject
// — so a zero-length Probe is legal and answers through probeWorld's
// starts-inside branch alone, as cp's own arithmetic already allows.
func probeCircle(centre m.Vec2d, radius float64, a, b m.Vec2d, probeRadius float64) (Hit, bool) {
	da := a.Sub(centre)
	db := b.Sub(centre)
	rsum := radius + probeRadius

	qa := da.Dot(da) - 2*da.Dot(db) + db.Dot(db)
	qb := da.Dot(db) - da.Dot(da)
	det := qb*qb - qa*(da.Dot(da)-rsum*rsum)

	if det >= 0 {
		t := (-qb - math.Sqrt(det)) / qa
		if 0 <= t && t <= 1 {
			normal := da.Lerp(db, t).Normalize()
			return Hit{
				T:      t,
				Point:  a.Lerp(b, t).Sub(normal.MulS(probeRadius)),
				Normal: normal,
			}, true
		}
	}
	return Hit{}, false
}

// probeSegment is cp's Segment.SegmentQuery: the face plane offset outward by
// the summed radius, the Hit confined to the face by a cross-product span test,
// and a circle swept against each end cap when the span test fails and the
// summed radius is not 0. Exact, and nothing allocates.
func probeSegment(shape Shape, world []m.Vec2d, a, b m.Vec2d, probeRadius float64) (Hit, bool) {
	segA, segB, normal := world[0], world[1], world[2]
	d := segA.Sub(a).Dot(normal)
	r := shape.Radius + probeRadius

	flipped := normal
	if d > 0 {
		flipped = normal.Negate()
	}
	offset := flipped.MulS(r).Sub(a)

	// The endpoints relative to a, moved out by the thickness of the segment.
	grownA := segA.Add(offset)
	grownB := segB.Add(offset)
	delta := b.Sub(a)

	if delta.Cross(grownA)*delta.Cross(grownB) <= 0 {
		offsetD := d
		if d > 0 {
			offsetD -= r
		} else {
			offsetD += r
		}
		ad := -offsetD
		bd := delta.Dot(normal) - offsetD

		if ad*bd < 0 {
			t := ad / (ad - bd)
			return Hit{
				T:      t,
				Point:  a.Lerp(b, t).Sub(flipped.MulS(probeRadius)),
				Normal: flipped,
			}, true
		}
		return Hit{}, false
	}

	if r != 0 {
		first, okFirst := probeCircle(segA, shape.Radius, a, b, probeRadius)
		second, okSecond := probeCircle(segB, shape.Radius, a, b, probeRadius)
		switch {
		case okFirst && okSecond:
			if first.T <= second.T {
				return first, true
			}
			return second, true
		case okFirst:
			return first, true
		case okSecond:
			return second, true
		}
	}
	return Hit{}, false
}

// probePoly is cp's PolyShape.SegmentQuery: each face plane offset outward by
// the summed radius, the Hit confined to that face by a cross-product span
// test, and a circle swept against each vertex when the summed radius is not 0.
// Exact, for a rotated Polygon too, and nothing allocates.
//
// The one departure is the divide, which is defect 6: cp divides by an − bn
// unguarded, so a Probe parallel to a face divides by zero and hands back a
// NaN. C guards it with max(an − bn, CPFLOAT_MIN), and the guarded quotient
// falls outside [0, 1] and is rejected exactly where cp's negative one was.
func probePoly(shape Shape, world []m.Vec2d, a, b m.Vec2d, probeRadius float64) (Hit, bool) {
	verts, normals := polyWorld(world)
	count := len(verts)
	rsum := shape.Radius + probeRadius

	// cp's info starts at Alpha 1 with no Shape, which is what the bevel pass
	// below compares against and what "no Hit" is.
	best := Hit{T: 1}
	found := false

	for i := range count {
		n := normals[i]
		an := a.Dot(n)
		d := an - verts[i].Dot(n) - rsum
		if d < 0 {
			continue
		}

		bn := b.Dot(n)
		t := d / math.Max(an-bn, smallestNormal)
		if t < 0 || 1 < t {
			continue
		}

		point := a.Lerp(b, t)
		dt := n.Cross(point)
		if n.Cross(verts[(i-1+count)%count]) <= dt && dt <= n.Cross(verts[i]) {
			best = Hit{T: t, Point: point.Sub(n.MulS(probeRadius)), Normal: n}
			found = true
		}
	}

	if rsum > 0 {
		for i := range count {
			if hit, ok := probeCircle(verts[i], shape.Radius, a, b, probeRadius); ok && hit.T < best.T {
				best, found = hit, true
			}
		}
	}

	if !found {
		return Hit{}, false
	}
	return best, true
}

// pointQueryWorld is cp's PointQuery for each kind: the nearest point on the
// Shape's surface, the signed distance to it — negative inside — and the
// gradient of that distance, which is the outward unit normal there.
func pointQueryWorld(p m.Vec2d, shape Shape, world []m.Vec2d) (m.Vec2d, float64, m.Vec2d) {
	switch shape.Kind {
	case ShapeCircle:
		centre, radius := world[0], shape.Radius
		delta := p.Sub(centre)
		d := delta.Length()

		// C guards this divide and jakecoffman/cp does not, which is defect 6:
		// a Probe on a circle's centre divides by zero and hands back a NaN
		// point. The gradient's own MAGIC_EPSILON branch is cp's, three lines
		// below the divide it does not cover.
		point := centre.Add(m.Vec2d{Y: 1}.MulS(radius))
		if d > 0 {
			point = centre.Add(delta.MulS(radius / d))
		}
		gradient := m.Vec2d{Y: 1}
		if d > magicEpsilon {
			gradient = delta.MulS(1 / d)
		}
		return point, d - radius, gradient

	case ShapeSegment:
		segA, segB, normal := world[0], world[1], world[2]
		closest := closestPointOnSegment(p, segA, segB)

		delta := p.Sub(closest)
		d := delta.Length()
		radius := shape.Radius

		point := closest
		if d != 0 {
			point = closest.Add(delta.MulS(radius / d))
		}
		gradient := normal
		if d > magicEpsilon {
			gradient = delta.MulS(1 / d)
		}
		return point, d - radius, gradient
	}

	if !isPolygon(shape.Kind) {
		return p, infinity, m.Vec2d{Y: 1}
	}

	// cp's PolyShape.PointQuery: the nearest point on any edge, and whether the
	// point is outside any face plane, which is what the sign of the distance
	// is.
	verts, normals := polyWorld(world)
	count := len(verts)

	minimum := infinity
	var closest, closestNormal m.Vec2d
	outside := false

	previous := verts[count-1]
	for i := range count {
		current := verts[i]
		if !outside {
			outside = normals[i].Dot(p.Sub(current)) > 0
		}

		point := closestPointOnSegment(p, previous, current)
		if distance := p.Distance(point); distance < minimum {
			minimum = distance
			closest = point
			closestNormal = normals[i]
		}
		previous = current
	}

	d := minimum
	if !outside {
		d = -minimum
	}

	// The divide C guards and jakecoffman/cp does not, which is the Polygon's
	// half of defect 6: a point exactly on an edge leaves d at zero, and the
	// unguarded quotient puts a NaN into the surface point of every rounded
	// Polygon. The gradient's own MAGIC_EPSILON branch, below, is cp's.
	gradient := closestNormal
	if d != 0 {
		gradient = p.Sub(closest).MulS(1 / d)
	}
	point := closest.Add(gradient.MulS(shape.Radius))
	if minimum <= magicEpsilon {
		gradient = closestNormal
	}
	return point, d - shape.Radius, gradient
}

// touching is what a closed form finds: cp's CollisionInfo as a value, with the
// two surface points cp's PushContact writes beside the normal and the overlap
// Penetration already reported.
//
// normal points from the first Shape towards the second, which is cp's own
// sense. p1 is on the first Shape's surface and p2 on the second's, both in
// world space, exactly as cp pushes them; depth is how far along the normal one
// would move to part them, and is −(p2 − p1)·normal by construction. id is the
// point's identity across ticks, two vertex indices packed into a uint32 —
// exact, where cp mixes shape pointers into a hash and admits false positives.
//
// The count is 1 for both closed forms. The array is two wide because the
// Polygon manifolds that arrive with GJK push two, and the Contact entry the
// caller fills is already laid out for them.
type touching struct {
	normal m.Vec2d
	points [2]touchingPoint
	count  int
	// gjkId is cp's collisionId: the simplex GJK converged on, which the next
	// tick's GJK starts from. It is zero for the two closed forms, which have
	// no simplex, and is always in the kind order the dispatch sorted the pair
	// into, so a caller passing its two Shapes the other way round hands back
	// an id the next tick can still use.
	gjkId uint32
}

type touchingPoint struct {
	p1, p2 m.Vec2d
	depth  float64
	id     uint32
}

// pointID packs two vertex indices into one uint32. The closed forms have no
// vertices to name and pass 0, which is the id cp's own hash carries for them.
func pointID(first, second uint32) uint32 { return first<<16 | second }

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
	if (closestT != 0 || normal.Dot(segment.verts[2].Rotate(rotation)) >= 0) &&
		(closestT != 1 || normal.Dot(segment.verts[3].Rotate(rotation)) >= 0) {
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

// closestPointOnSegment is cp's Vector.ClosestPointOnSegment, with defect 6's
// guard on the same divide: a zero-length segment — or a Polygon edge whose two
// vertices coincide — is 0/0 in cp and a NaN surface point out of every point
// query that reaches it. Guarded, the answer is the one endpoint, which is the
// whole of what such a segment is.
func closestPointOnSegment(p, a, b m.Vec2d) m.Vec2d {
	delta := a.Sub(b)
	var t float64
	if lengthSquared := delta.LengthSquared(); lengthSquared > 0 {
		t = clamp01(delta.Dot(p.Sub(b)) / lengthSquared)
	}
	return b.Add(delta.MulS(t))
}

// clamp01 is cp's Clamp01.
func clamp01(f float64) float64 { return math.Max(0, math.Min(f, 1)) }
