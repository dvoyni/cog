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

// worldScratchLen is how many world-space vectors the widest Shape kind a query
// caches needs: a segment's two endpoints and its normal. A circle needs one.
// The Polygon kinds arrive with the Polygon pipeline and raise it.
const worldScratchLen = 4

// worldLenFor is how many slab vectors a Shape's world cache occupies.
func worldLenFor(shape Shape) int {
	switch shape.Kind {
	case ShapeCircle:
		return 1
	case ShapeSegment:
		return 3
	}
	return 0
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
		centre := transform.Point(shape.verts[0])
		dst[0] = centre
		return 1, NewBBForCircle(centre, shape.Radius)

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

		left, right := a.X, b.X
		if right < left {
			left, right = right, left
		}
		bottom, top := a.Y, b.Y
		if top < bottom {
			bottom, top = top, bottom
		}
		radius := shape.Radius
		return 3, NewBB(left-radius, bottom-radius, right+radius, top+radius)
	}

	return 0, BB{}
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
	var world [worldScratchLen]m.Vec2d
	transform := NewTransformRigid(at, angle)
	used, _ := cacheWorldAt(shape, transform, verts, world[:])
	return probeWorld(from, to, radius, shape, world[:used])
}

// ClosestPoint is the point on a Shape's surface nearest a point, the Shape
// placed at a position and an angle. A point inside the Shape still gets the
// nearest surface point.
//
// verts is the Polygon Component's vertices and is nil for every kind but Poly.
func ClosestPoint(p m.Vec2d, shape Shape, at m.Vec2d, angle float64, verts []m.Vec2d) m.Vec2d {
	var world [worldScratchLen]m.Vec2d
	transform := NewTransformRigid(at, angle)
	used, _ := cacheWorldAt(shape, transform, verts, world[:])
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
	var worldA, worldB [worldScratchLen]m.Vec2d
	transformA := NewTransformRigid(atA, angleA)
	transformB := NewTransformRigid(atB, angleB)
	usedA, _ := cacheWorldAt(a, transformA, vertsA, worldA[:])
	usedB, _ := cacheWorldAt(b, transformB, vertsB, worldB[:])
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
	return Hit{}, false
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

	return p, infinity, m.Vec2d{Y: 1}
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
	touch, ok := collideWorld(a, transformA, worldA, b, transformB, worldB, m.Vec2d{})
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
// coincident is the direction to take when two circles share a centre, where cp
// invents a fixed (1, 0) that never breaks symmetry. The zero vector asks for
// no direction at all, which is what the pure Penetration reports.
//
// Only the two closed forms are here. Everything with a Polygon on either side,
// and segment against segment, goes through GJK, which arrives with the Polygon
// pipeline; until then those pairs report no touch.
func collideWorld(
	a Shape, transformA Transform, worldA []m.Vec2d,
	b Shape, transformB Transform, worldB []m.Vec2d,
	coincident m.Vec2d,
) (touching, bool) {
	if len(worldA) == 0 || len(worldB) == 0 {
		return touching{}, false
	}

	switch {
	case a.Kind == ShapeCircle && b.Kind == ShapeCircle:
		return collideCircles(worldA[0], a.Radius, worldB[0], b.Radius, coincident)

	case a.Kind == ShapeCircle && b.Kind == ShapeSegment:
		return collideCircleSegment(worldA[0], a.Radius, b, transformB, worldB)

	case a.Kind == ShapeSegment && b.Kind == ShapeCircle:
		// The kind order the switch requires puts the circle first, so the one
		// answer is turned round here: the normal flips and the two surface
		// points swap, leaving the caller's own first Shape the one the normal
		// points away from.
		touch, ok := collideCircleSegment(worldB[0], b.Radius, a, transformA, worldA)
		if !ok {
			return touching{}, false
		}
		touch.normal = touch.normal.Negate()
		for i := range touch.count {
			touch.points[i].p1, touch.points[i].p2 = touch.points[i].p2, touch.points[i].p1
		}
		return touch, true
	}

	return touching{}, false
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
	closestT := clamp01(segDelta.Dot(centre.Sub(segA)) / segDelta.LengthSquared())
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

// closestPointOnSegment is cp's Vector.ClosestPointOnSegment.
func closestPointOnSegment(p, a, b m.Vec2d) m.Vec2d {
	delta := a.Sub(b)
	t := clamp01(delta.Dot(p.Sub(b)) / delta.LengthSquared())
	return b.Add(delta.MulS(t))
}

// clamp01 is cp's Clamp01.
func clamp01(f float64) float64 { return math.Max(0, math.Min(f, 1)) }
