package types

import (
	"math"

	"github.com/dvoyni/cog/libs/m"
)

// GJK and EPA, ported from cp's collision.go, with the three layers of
// indirection cp puts between them and a Shape removed.
//
// cp's SupportContext carries two func fields, rebuilt at four call sites and
// passed by value through GJK, GJKRecurse, EPA and EPARecurse, each support
// function type-asserting its shape back out of an interface. All three layers
// go: the port's support is a free function switching on the family over the
// world cache the index entry already holds, and the context is the two runs
// and the two kinds.

const (
	// maxGJKIterations and maxEPAIterations are cp's own caps on the two loops.
	maxGJKIterations = 30
	maxEPAIterations = 30

	// epaHullMax is how large the expanding hull can get, and it is a bound
	// rather than a cap: the hull starts at three points and grows by one per
	// iteration, and the insert is refused from iteration 30 onwards, so the
	// most it reaches is 3 + 29. cp allocates make([]MinkowskiPoint, count+1) on
	// every one of those iterations — the narrowphase's whole share of cp's 656
	// allocations a step — where C uses alloca. The port ping-pongs two stack
	// buffers of this size instead.
	epaHullMax = 32
)

// smallestNormal is C's CPFLOAT_MIN, added to a denominator so a degenerate
// input yields a number rather than a NaN.
//
// jakecoffman/cp weakened the same guard to 1e-15 at collision.go :248, :328
// and :329, which is large enough to perturb a genuinely small vector rather
// than merely to stop a divide by zero. This is C's value, and it is the
// constant m.Vec2d's own ported guards already carry.
const smallestNormal = 2.2250738585072014e-308

// supportPoint is cp's SupportPoint: the extreme point of one Shape along an
// axis, and which of its vertices that was, so the next tick's GJK can start
// from the same pair.
type supportPoint struct {
	p     m.Vec2d
	index uint32
}

// minkowskiPoint is cp's MinkowskiPoint: a point on the surface of the two
// Shapes' Minkowski difference, with the two original support points kept so
// that the closest points can be interpolated back out of it.
type minkowskiPoint struct {
	a, b m.Vec2d
	// ab is b − a, the point on the difference itself.
	ab m.Vec2d
	// id is the two support indices packed together, one byte each.
	id uint32
}

// closestPoints is cp's ClosestPoints: the nearest surface point on each Shape,
// the minimum separating axis between them, the signed distance along it, and
// the simplex the next tick warm starts from.
type closestPoints struct {
	a, b m.Vec2d
	n    m.Vec2d
	d    float64
	id   uint32
}

// support is cp's SupportContext without its two func fields: the two Shapes'
// world caches and the kinds that say how to read them. It is passed by value
// through the recursion as cp's is.
type support struct {
	worldA, worldB []m.Vec2d
	kindA, kindB   ShapeKind
}

// at is cp's SupportContext.Support: the extreme point of the Minkowski
// difference along an axis.
func (ctx support) at(n m.Vec2d) minkowskiPoint {
	a := supportPointFor(ctx.kindA, ctx.worldA, n.Negate())
	b := supportPointFor(ctx.kindB, ctx.worldB, n)
	return newMinkowskiPoint(a, b)
}

// newMinkowskiPoint is cp's NewMinkowskiPoint, packing the two support indices
// into one byte each.
func newMinkowskiPoint(a, b supportPoint) minkowskiPoint {
	return minkowskiPoint{
		a:  a.p,
		b:  b.p,
		ab: b.p.Sub(a.p),
		id: (a.index&0xFF)<<8 | (b.index & 0xFF),
	}
}

// closestTo is cp's MinkowskiPoint.ClosestPoints: the closest points on the two
// Shapes given the closest edge of their Minkowski difference to the origin.
//
// This is cp's line exactly, and EPA is what calls it: there the origin is
// inside the hull, and a d at or below zero is a real overlap. GJK calls
// closestOutside instead, which carries the guard below.
func (v0 minkowskiPoint) closestTo(v1 minkowskiPoint) closestPoints {
	return v0.closest(v1, false)
}

// closestOutside is closestTo for GJK, where the origin is known to be outside
// the Minkowski difference: a departure from cp, for a defect in cp.
//
// C's line 246 in cpCollision.c, identical at 7.0.2 and master, reads
//
//	if(d <= 0.0f || (-1.0f < t && t < 1.0f)){
//
// and d is the origin's distance to the edge's supporting line, not to the
// edge. When t is clamped the nearest point of the edge is its end, the origin
// is beyond the edge, and if the origin lies on that line d is 0 — or, at a
// general angle, a rounding below it — so two disjoint Shapes come back touching
// at their summed radii. The research that reproduced it in C is
// docs/research/chipmunk-c-gjk.md on the research/chipmunk-c-gjk branch
// (dvoyni/cog#506).
//
// So the test is on the clamp and not on d == 0: an exact zero is only the
// lattice's form of it, and the rounded one is what the engine reaches. A
// clamped t takes the vertex arm, where the distance is |p|. So does a simplex
// collapsed to one Minkowski point, which has no edge and so no line at all:
// its n is the zero vector and its d an exact 0, and gjkRecurse only returns
// one once the search along −p found nothing nearer, which makes |p| the
// distance. Either way p itself being the zero vector is two Shapes meeting at
// a vertex, which have no p/|p| and keep the edge's normal so that the contact
// still has a direction.
//
// EPA is left on cp's line on purpose. There the origin is inside the hull, and
// a hull edge ending at a point in the middle of a face can clamp t by a
// rounding of 1e-16 with the origin a real 0.01 m to 0.6 m inside: the guard
// would read that overlap as a separation of |p|, and a stack of boxes falls
// through itself (TestBoxesStackAndSettle).
func (v0 minkowskiPoint) closestOutside(v1 minkowskiPoint) closestPoints {
	return v0.closest(v1, true)
}

// closest is the body closestTo and closestOutside share; outside says whether
// the guard is on.
func (v0 minkowskiPoint) closest(v1 minkowskiPoint, outside bool) closestPoints {
	// Where along the edge the difference comes closest to the origin. The
	// ClosestT the port uses carries C's CPFLOAT_MIN, which jakecoffman/cp
	// drops, so a degenerate simplex answers with a number and not a NaN.
	t := v0.ab.ClosestT(v1.ab)
	p := lerpT(v0.ab, v1.ab, t)

	// The original support points interpolated at the same t, which is what
	// makes them the surface points in absolute coordinates.
	pa := lerpT(v0.a, v1.a, t)
	pb := lerpT(v0.b, v1.b, t)
	id := (v0.id&0xFFFF)<<16 | (v1.id & 0xFFFF)

	delta := v1.ab.Sub(v0.ab)
	n := delta.ReversePerp().Normalize()
	d := n.Dot(p)

	edge := d <= 0 || (-1 < t && t < 1)
	if outside {
		edge = (-1 < t && t < 1 && delta != (m.Vec2d{})) || p == (m.Vec2d{})
	}
	if edge {
		// Overlapping, or an ordinary vertex-against-edge collision.
		return closestPoints{a: pa, b: pb, n: n, d: d, id: id}
	}

	// Vertex against vertex, where the separating axis is not an axis of the
	// Minkowski difference at all.
	d2 := p.Length()
	n2 := p.MulS(1 / (d2 + smallestNormal))
	return closestPoints{a: pa, b: pb, n: n2, d: d2, id: id}
}

// gjk is cp's GJK: the closest points between two Shapes, warm started from the
// cached simplex when there is one and from the two bounding-box centres when
// there is not.
//
// centreA and centreB are the two Shapes' bounding-box centres. cp reads them
// off the box each Shape keeps; the port's boxes live in the index entry, and
// the pair primitives build theirs beside the world cache, so they are handed
// in rather than reached for.
//
// coincident is the caller's seeded direction, and it is read at exactly one
// placement: the two centres coinciding, where their difference is the zero
// vector and the cold-start axis with it. Every support query then answers with
// the same point, the simplex collapses to one Minkowski point, and closestTo
// takes its d <= 0 arm with a normal Normalize guarded to zero — so without a
// seed the pair comes back touching with no direction and no depth, however
// deeply the two really overlap.
//
// The seed is the caller's and not this function's, which is the whole of the
// design here: cp invents a fixed (1, 0), a symmetry that never breaks, and the
// specification rejects it. Detect draws a direction once a tick and hands it
// down; the pure Penetration hands in the zero vector, which asks for no
// direction at all and leaves the answer exactly as it was. It is the same
// parameter, by the same route, that collideCircles already took — there is one
// symmetry-breaking mechanism in the package, not two.
//
// The seed is drawn once a tick and not once here: asking costs a sine and a
// cosine, and a pair that has to ask is vanishingly rare, so every candidate pair
// would otherwise pay for a direction almost none of them read. What this
// function pays is one comparison on the cold-start path, and it is free —
// BenchmarkCollidePolys 367.5 ns a pair against 369.3 without the guard, and
// BenchmarkDetect 14.18 µs against 14.31 at N=256 and 58.4 µs against 58.6 at
// N=1024, each the mean of two interleaved rounds with the two variants built as
// separate binaries and run alternately, on an AMD Ryzen 9 7950X3D under
// go1.27.1 windows/amd64 at GOMAXPROCS=32. The guarded side is the faster of the
// two in every pairing, which is how a difference of nothing reads.
func gjk(ctx support, centreA, centreB, coincident m.Vec2d, cached uint32) closestPoints {
	var v0, v1 minkowskiPoint

	if cached != 0 {
		// The Minkowski points of the last tick, by the indices it cached.
		v0 = newMinkowskiPoint(
			shapePoint(ctx.kindA, ctx.worldA, (cached>>24)&0xFF),
			shapePoint(ctx.kindB, ctx.worldB, (cached>>16)&0xFF),
		)
		v1 = newMinkowskiPoint(
			shapePoint(ctx.kindA, ctx.worldA, (cached>>8)&0xFF),
			shapePoint(ctx.kindB, ctx.worldB, cached&0xFF),
		)
	} else {
		axis := centreA.Sub(centreB).Perp()
		if axis == (m.Vec2d{}) {
			// A caller that handed in no seed gets the zero axis it would have
			// had, and the degenerate answer that follows from it, unchanged.
			axis = coincident
		}
		v0 = ctx.at(axis)
		v1 = ctx.at(axis.Negate())
		if axis == (m.Vec2d{}) {
			// Still no axis: the answer is cp's, on cp's line. gjkRecurse would
			// now search along −p from this collapsed simplex and find a
			// direction the pure Penetration must not invent, so the
			// coincident-centres rule is answered here, exactly as the recursion
			// answered it before that search existed.
			return v0.closestTo(v1)
		}
	}

	return gjkRecurse(ctx, v0, v1, 1)
}

// gjkRecurse is cp's GJKRecurse, the GJK loop. The recursion is kept as
// written: 30 frames is nothing, and flattening it would be a departure with no
// reason behind it.
//
// Its two answers go through closestOutside rather than cp's closestTo: GJK
// only answers with the origin outside the difference, and that is where the
// supporting-line guard holds.
func gjkRecurse(ctx support, v0, v1 minkowskiPoint, iteration int) closestPoints {
	if iteration > maxGJKIterations {
		return v0.closestOutside(v1)
	}

	if pointGreater(v1.ab, v0.ab, m.Vec2d{}) {
		// The origin is behind the axis. Flip and try again.
		return gjkRecurse(ctx, v1, v0, iteration)
	}

	// A simplex collapsed to one Minkowski point searches along −p, as a
	// clamped one does. That is a departure from cp, for a defect in cp: two
	// Shapes on one line — capsules end to end, draw 4094 of the finiteness
	// fuzz — tie every support query along the cold-start axis, which is
	// perpendicular to that line, so both ends of the simplex are the same
	// point. The ClosestT the port uses, like C master's since e7ea51e, then
	// gives t = 0, the search direction is the perpendicular of the zero vector,
	// and GJK stops where it started with no normal and a d of 0: two disjoint
	// Shapes touching. C 7.0.2, whose ClosestT divides zero by zero, clamps the
	// NaN to t = 1 and searches along −p by accident; the port does it on
	// purpose. The zero axis of two coincident centres never gets here: gjk
	// answers it itself, because a direction found there is not the pure
	// Penetration's to report.
	t := v0.ab.ClosestT(v1.ab)
	var n m.Vec2d
	if -1 < t && t < 1 && v0.ab != v1.ab {
		n = v1.ab.Sub(v0.ab).Perp()
	} else {
		n = lerpT(v0.ab, v1.ab, t).Negate()
	}
	p := ctx.at(n)

	// The first test is a departure from cp, for a defect in cp. p is the
	// difference's extreme point along n, so p·n < 0 puts the whole difference
	// on the far side of a line through the origin, and the origin outside it
	// whatever the two orientation tests say. They can say otherwise only by
	// rounding: two capsules end to end on one line give a v0, p and v1 all on a
	// line through the origin and 0.55 m or more from it, a triangle with no
	// area, and C's cpCheckPointGreater pair (cpCollision.c line 372, 7.0.2 and
	// master alike) reads it as containing the origin. EPA then ends on a clamped
	// edge with d a rounding below 0 and reports the summed radii, 0.5 m. The
	// test needs no tolerance: it changes an answer only where the orientation
	// tests contradict the support point.
	if p.ab.Dot(n) >= 0 && pointGreater(p.ab, v0.ab, m.Vec2d{}) && pointGreater(v1.ab, p.ab, m.Vec2d{}) {
		// The origin is inside the simplex: the two overlap, and the answer is
		// on the surface of the Minkowski difference rather than outside it.
		return epa(ctx, v0, p, v1)
	}

	if checkAxis(v0.ab, v1.ab, p.ab, n) {
		return v0.closestOutside(v1)
	}

	if closestDist(v0.ab, p.ab) < closestDist(p.ab, v1.ab) {
		return gjkRecurse(ctx, v0, p, iteration+1)
	}
	return gjkRecurse(ctx, p, v1, iteration+1)
}

// epa is cp's EPA, called from GJK when the two Shapes overlap.
//
// cp's hull is a fresh slice per iteration; here it is two fixed stack buffers
// ping-ponged, which is the whole of this port's saving on cp's narrowphase
// allocations. The pair is passed as a pointer so that the recursion copies
// neither buffer; it is a stack local of this frame and nothing stores it.
func epa(ctx support, v0, v1, v2 minkowskiPoint) closestPoints {
	var hulls [2][epaHullMax]minkowskiPoint
	hulls[0][0], hulls[0][1], hulls[0][2] = v0, v1, v2
	return epaRecurse(ctx, &hulls, 0, 3, 1)
}

// epaRecurse is cp's EPARecurse: each recursion adds a point to the convex hull
// until the closest point on the surface is known.
//
// which names the buffer the hull is in and the rebuilt one goes into the
// other. cp's log.Println on a high iteration count is not ported — C's three
// GJK warnings were dropped by the Go port already, and this one is the last of
// them.
func epaRecurse(
	ctx support, hulls *[2][epaHullMax]minkowskiPoint, which, count, iteration int,
) closestPoints {
	hull := hulls[which][:count]

	// The closest segment hull[i] to hull[i+1] to the origin.
	mini := 0
	minDist := infinity
	i := count - 1
	for j := 0; j < count; j++ {
		if d := closestDist(hull[i].ab, hull[j].ab); d < minDist {
			minDist = d
			mini = i
		}
		i = j
	}

	v0 := hull[mini]
	v1 := hull[(mini+1)%count]

	p := ctx.at(v1.ab.Sub(v0.ab).Perp())

	duplicate := p.id == v0.id || p.id == v1.id
	if !duplicate && pointGreater(v0.ab, v1.ab, p.ab) && iteration < maxEPAIterations {
		// Rebuild the convex hull by inserting p, into the other buffer.
		next := &hulls[1-which]
		next[0] = p
		count2 := 1

		for k := range count {
			index := (mini + 1 + k) % count

			h0 := next[count2-1].ab
			h1 := hull[index].ab
			h2 := p.ab
			if k+1 < count {
				h2 = hull[(index+1)%count].ab
			}

			if pointGreater(h0, h2, h1) {
				next[count2] = hull[index]
				count2++
			}
		}

		return epaRecurse(ctx, hulls, 1-which, count2, iteration+1)
	}

	// No new point to insert, so this is the closest edge of the difference.
	return v0.closestTo(v1)
}

// supportPointFor is cp's three SupportPointFuncs — CircleSupportPoint,
// SegmentSupportPoint and PolySupportPoint — as one switch on the family over
// the world cache. This is where cp's two func fields and their type assertions
// went.
func supportPointFor(kind ShapeKind, world []m.Vec2d, n m.Vec2d) supportPoint {
	switch kind {
	case ShapeCircle:
		return supportPoint{p: world[0]}
	case ShapeSegment:
		if world[0].Dot(n) > world[1].Dot(n) {
			return supportPoint{p: world[0]}
		}
		return supportPoint{p: world[1], index: 1}
	}
	verts, _ := polyWorld(world)
	i := polySupportIndex(verts, n)
	return supportPoint{p: verts[i], index: uint32(i)}
}

// shapePoint is cp's Shape.Point: the support point a cached index names, for
// the warm start. cp's own clamp on a Polygon whose vertex count changed is
// kept, and it is what makes a cached id that belonged to the other party of a
// same-kind pair merely a poor guess rather than a panic — the port's index
// slots are rebuilt every tick, where cp's arbiter fixes which Shape is which.
func shapePoint(kind ShapeKind, world []m.Vec2d, i uint32) supportPoint {
	switch kind {
	case ShapeCircle:
		return supportPoint{p: world[0]}
	case ShapeSegment:
		if i == 0 {
			return supportPoint{p: world[0]}
		}
		return supportPoint{p: world[1], index: 1}
	}
	verts, _ := polyWorld(world)
	index := uint32(0)
	if i < uint32(len(verts)) {
		index = i
	}
	return supportPoint{p: verts[index], index: index}
}

// polySupportIndex is cp's PolySupportPointIndex: which vertex is farthest
// along an axis.
func polySupportIndex(verts []m.Vec2d, n m.Vec2d) int {
	maximum := -infinity
	index := 0
	for i := range verts {
		if d := verts[i].Dot(n); d > maximum {
			maximum = d
			index = i
		}
	}
	return index
}

// pointGreater is cp's Vector.PointGreater, lerpT its LerpT, closestDist its
// ClosestDist and checkAxis its CheckAxis. They live here rather than in libs/m
// because they are GJK's own predicates and mean nothing to gameplay code.
func pointGreater(a, b, c m.Vec2d) bool {
	return (b.Y-a.Y)*(a.X+b.X-2*c.X) > (b.X-a.X)*(a.Y+b.Y-2*c.Y)
}

func lerpT(a, b m.Vec2d, t float64) m.Vec2d {
	half := 0.5 * t
	return a.MulS(0.5 - half).Add(b.MulS(0.5 + half))
}

func closestDist(a, b m.Vec2d) float64 {
	return lerpT(a, b, a.ClosestT(b)).LengthSquared()
}

func checkAxis(v0, v1, p, n m.Vec2d) bool {
	return p.Dot(n) <= math.Max(v0.Dot(n), v1.Dot(n))
}
