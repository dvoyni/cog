package internal

import (
	"math"

	"github.com/dvoyni/cog/libs/m"
)

// The world cache: a placed Shape's vertices and normals in world coordinates,
// written once per index entry per tick and read by every query and every
// collision arm after it. Everything downstream takes the run rather than the
// Shape and the transform, which is why probe.go and collide.go name neither.

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
		dst[0] = transform.Point(shape.Verts[0])
		return 1, boxForWorld(shape, dst[:1])

	case ShapeSegment:
		a := transform.Point(shape.Verts[0])
		b := transform.Point(shape.Verts[1])
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
