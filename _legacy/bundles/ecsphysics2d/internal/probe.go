package internal

import (
	"math"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// The point and segment queries: what a swept circle meets along a path, the
// nearest point on a Shape's surface, and how deeply two placed Shapes
// overlap. They answer about one Shape at a time and report a Hit; finding the
// pairs worth asking about is index.go, and the manifold two touching Shapes
// make is collide.go.

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
