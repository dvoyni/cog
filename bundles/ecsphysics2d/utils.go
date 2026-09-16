package ecsphysics2d

import (
	"github.com/dvoyni/cog/bundles/ecsphysics2d/internal/types"
	"github.com/dvoyni/cog/libs/m"
)

// NewTransform takes the six components of the matrix in reading order, row by
// row: a, c, tx, then b, d, ty.
func NewTransform(a, c, tx, b, d, ty float64) Transform {
	return types.NewTransform(a, c, tx, b, d, ty)
}

// NewTransformIdentity is the transform that changes nothing.
func NewTransformIdentity() Transform { return types.NewTransformIdentity() }

// NewTransformTranslate moves by a vector.
func NewTransformTranslate(translate m.Vec2d) Transform {
	return types.NewTransformTranslate(translate)
}

// NewTransformScale scales each axis.
func NewTransformScale(scaleX, scaleY float64) Transform {
	return types.NewTransformScale(scaleX, scaleY)
}

// NewTransformRotate turns by an angle in radians.
func NewTransformRotate(radians float64) Transform { return types.NewTransformRotate(radians) }

// NewTransformRigid turns by an angle in radians and then moves, which is the
// only kind of transform a Body has.
func NewTransformRigid(translate m.Vec2d, radians float64) Transform {
	return types.NewTransformRigid(translate, radians)
}

// NewTransformRigidInverse undoes a rigid transform without a divide.
func NewTransformRigidInverse(t Transform) Transform { return types.NewTransformRigidInverse(t) }

// NewBB is the box with those four edges: left, bottom, right, top.
func NewBB(l, b, r, t float64) BB { return types.NewBB(l, b, r, t) }

// NewBBForExtents is the box centred on a point with those half sizes.
func NewBBForExtents(centre m.Vec2d, halfWidth, halfHeight float64) BB {
	return types.NewBBForExtents(centre, halfWidth, halfHeight)
}

// NewBBForCircle is the box around a circle at that position.
func NewBBForCircle(position m.Vec2d, radius float64) BB {
	return types.NewBBForCircle(position, radius)
}

// NewCircleShape is a circle of that radius about an offset from the Body's
// Position, which is its centre of gravity. A radius of 0 is a point. Both
// collision fields start at every group, as cp's shapes do.
func NewCircleShape(radius float64, offset m.Vec2d) Shape {
	return types.NewCircleShape(radius, offset)
}

// NewSegmentShape is the segment from a to b, both local to the Body's
// Position, fattened by that radius — which makes it a capsule. Both collision
// fields start at every group.
func NewSegmentShape(a, b m.Vec2d, radius float64) Shape {
	return types.NewSegmentShape(a, b, radius)
}

// NewSegmentShapeWithNeighbours is the segment from a to b with the points the
// app knows come before and after it along its run of geometry, so that a Body
// rolling over the joint does not catch on the end cap. The two neighbours are
// kept as local tangents and rotated on use.
//
// There is no chain concept: the segments stay separate Entities, and which
// segment neighbours which is the app's knowledge. Chipmunk's own
// cpSegmentShapeSetNeighbors has no counterpart in cp at all, which is why cp's
// end-cap rejection is dead code.
func NewSegmentShapeWithNeighbours(previous, a, b, next m.Vec2d, radius float64) Shape {
	return types.NewSegmentShapeWithNeighbours(previous, a, b, next, radius)
}

// NewPolygonShape is the convex polygon of those vertices, local to the Body's
// Position and rounded by that radius. It always hulls, which is what enforces
// Chipmunk's winding, and it is the one way to build a polygon Shape.
//
// Three or four vertices come back as ShapeTri or ShapeQuad carrying them
// inline, with the zero Polygon; more come back as ShapePoly with the vertices
// in the Polygon, which the app spawns on the same Entity. Spawning the zero
// Polygon beside an inline kind is harmless, so a caller may always spawn both.
//
// A refused outline comes back as a point — a circle of radius 0 — with the
// zero Polygon and one of ErrTooFewVertices, ErrDegenerateOutline or
// ErrConcaveOutline. A concave Polygon does not silently become its hull, which
// is what cp does with nothing said.
func NewPolygonShape(verts []m.Vec2d, radius float64) (Shape, Polygon, error) {
	return types.NewPolygonShape(verts, radius)
}

// NewBoxShape is a box of that width and height centred on the Body's Position,
// rounded by that radius. It keeps its four vertices directly rather than
// hulling them, a box being convex and wound by construction.
func NewBoxShape(width, height, radius float64) Shape {
	return types.NewBoxShape(width, height, radius)
}

// NewBoxShapeFor is the box with those four edges, local to the Body's
// Position, rounded by that radius — which is what a wall drawn around
// something other than its own middle is.
func NewBoxShapeFor(box BB, radius float64) Shape {
	return types.NewBoxShapeFor(box, radius)
}

// PolygonVerts appends a Shape's local vertices to dst and returns it, which is
// how a caller builds the run the queries take: the Polygon Component's for
// ShapePoly and the Shape's own slots for ShapeTri and ShapeQuad. The idiom is
// dst = PolygonVerts(dst[:0], shape, polygon), which settles to no allocation
// once the buffer is big enough.
func PolygonVerts(dst []m.Vec2d, shape Shape, polygon Polygon) []m.Vec2d {
	return types.PolygonVerts(dst, shape, polygon)
}

// NewStaticIndex is an empty static index with that cell size in metres. A cell
// size of 0 or less takes the documented default of 2 m, which is tuned for a
// metre-scaled world rather than assumed of one.
func NewStaticIndex(cellSize float64) *StaticIndex { return types.NewStaticIndex(cellSize) }

// NewBodyIndex is an empty Body index with that cell size in metres, with the
// same default as NewStaticIndex.
func NewBodyIndex(cellSize float64) *BodyIndex { return types.NewBodyIndex(cellSize) }

// ProbeShape moves a circle of that radius from one point to another against
// one Shape placed at a position and an angle, and reports the first Hit. It is
// the pair primitive behind an index's Probe, over values and touching no
// engine state, so its Hit names no Entity.
//
// verts is the Polygon Component's vertices and is nil for every kind but
// ShapePoly, which a Shape cannot carry inline.
func ProbeShape(
	from, to m.Vec2d, radius float64,
	shape Shape, at m.Vec2d, angle float64, verts []m.Vec2d,
) (Hit, bool) {
	return types.ProbeShape(from, to, radius, shape, at, angle, verts)
}

// Penetration is how deeply two placed Shapes overlap and which way apart, or
// false when they do not. There is no Overlaps boolean beside it: this ok is
// it. The normal points from a towards b, and on coincident centres it is zero
// with ok true, choosing a direction being the caller's.
func Penetration(
	a Shape, atA m.Vec2d, angleA float64, vertsA []m.Vec2d,
	b Shape, atB m.Vec2d, angleB float64, vertsB []m.Vec2d,
) (m.Vec2d, float64, bool) {
	return types.Penetration(a, atA, angleA, vertsA, b, atB, angleB, vertsB)
}

// ClosestPoint is the point on a Shape's surface nearest a point, the Shape
// placed at a position and an angle. Nearest is not a query of its own: it is
// an Overlap and a loop keeping the app's own predicate and this distance.
func ClosestPoint(p m.Vec2d, shape Shape, at m.Vec2d, angle float64, verts []m.Vec2d) m.Vec2d {
	return types.ClosestPoint(p, shape, at, angle, verts)
}

// MomentForCircle is the moment of inertia of a circle of that mass, with r1
// and r2 the inner and outer radii. A solid circle has an inner radius of 0,
// and offset moves it off the centre of gravity.
func MomentForCircle(mass, r1, r2 float64, offset m.Vec2d) float64 {
	return types.MomentForCircle(mass, r1, r2, offset)
}

// MomentForSegment is the moment of inertia of a segment of that mass from a to
// b with that radius.
func MomentForSegment(mass float64, a, b m.Vec2d, radius float64) float64 {
	return types.MomentForSegment(mass, a, b, radius)
}

// MomentForBox is the moment of inertia of a solid box of that mass.
func MomentForBox(mass, width, height float64) float64 {
	return types.MomentForBox(mass, width, height)
}

// MomentForPoly is the moment of inertia of a solid polygon of that mass,
// taking its centre of gravity to be its centroid, with offset added to each
// vertex. radius is accepted and ignored, as it is in Chipmunk.
func MomentForPoly(mass float64, verts []m.Vec2d, offset m.Vec2d, radius float64) float64 {
	return types.MomentForPoly(mass, verts, offset, radius)
}

// AreaForCircle is the area of a hollow circle, with r1 and r2 the inner and
// outer radii. A solid circle has an inner radius of 0.
func AreaForCircle(r1, r2 float64) float64 { return types.AreaForCircle(r1, r2) }

// AreaForSegment is the area of a segment from a to b fattened by that radius,
// which is a capsule.
func AreaForSegment(a, b m.Vec2d, radius float64) float64 {
	return types.AreaForSegment(a, b, radius)
}

// AreaForPoly is the signed area of a polygon fattened by that radius.
// Clockwise winding gives a positive area, which is Chipmunk's winding for
// polygon Shapes.
func AreaForPoly(verts []m.Vec2d, radius float64) float64 {
	return types.AreaForPoly(verts, radius)
}

// CentroidForPoly is the natural centroid of a polygon outline, and reports
// false for a degenerate one — zero area or coincident vertices — where the
// divide would otherwise hand back a NaN.
func CentroidForPoly(verts []m.Vec2d) (m.Vec2d, bool) { return types.CentroidForPoly(verts) }

// NewDynamic is the Dynamic body a mass, a Moment of inertia and the two
// Damping rates in 1/s describe. A rejected argument yields the zero Dynamic,
// which moves under nothing, and the error naming which argument it was.
func NewDynamic(mass, moment, damping, angularDamping float64) (Dynamic, error) {
	return types.NewDynamic(mass, moment, damping, angularDamping)
}
