package ecsphysics2d

import (
	"github.com/dvoyni/cog/bundles/ecs"
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

// NewPinJoint holds A's anchor and B's anchor at a fixed distance in metres.
// Both anchors are local to their Body's centre of gravity, and PinDistance is
// the pure helper that works the distance out from where the two Bodies stand.
func NewPinJoint(a, b ecs.Entity, anchorA, anchorB m.Vec2d, distance float64) Joint {
	return types.NewPinJoint(a, b, anchorA, anchorB, distance)
}

// PinDistance is the distance between two anchors as they stand in the world,
// which is what cp's pin constructor reads off the two Bodies.
func PinDistance(
	posA m.Vec2d, angleA float64, anchorA m.Vec2d,
	posB m.Vec2d, angleB float64, anchorB m.Vec2d,
) float64 {
	return types.PinDistance(posA, angleA, anchorA, posB, angleB, anchorB)
}

// NewSlideJoint holds A's anchor and B's anchor within a range of distances in
// metres, and holds nothing at all inside that range.
func NewSlideJoint(a, b ecs.Entity, anchorA, anchorB m.Vec2d, minimum, maximum float64) Joint {
	return types.NewSlideJoint(a, b, anchorA, anchorB, minimum, maximum)
}

// NewPivotJoint holds A's anchor and B's anchor at the same point: the hinge a
// jointed figure of limbs turns about. PivotAnchors splits one world pivot into
// the two local anchors.
func NewPivotJoint(a, b ecs.Entity, anchorA, anchorB m.Vec2d) Joint {
	return types.NewPivotJoint(a, b, anchorA, anchorB)
}

// PivotAnchors splits one world pivot into the two anchors, each local to its
// own Body's centre of gravity.
func PivotAnchors(
	posA m.Vec2d, angleA float64,
	posB m.Vec2d, angleB float64,
	worldPivot m.Vec2d,
) (anchorA, anchorB m.Vec2d) {
	return types.PivotAnchors(posA, angleA, posB, angleB, worldPivot)
}

// NewGrooveJoint holds A's anchor on the line segment from start to end carried
// by B. All three points are local to their own Body. cp carries the groove on
// its first Body and the anchor on its second; this port's A is cp's b, so the
// two are the other way round — the one place that substitution is visible.
func NewGrooveJoint(a, b ecs.Entity, anchorA, start, end m.Vec2d) Joint {
	return types.NewGrooveJoint(a, b, anchorA, start, end)
}

// NewSpringJoint pushes A's anchor and B's anchor towards a rest length,
// harder the further they are from it, settling by its Absorption. stiffness is
// in N/m and absorption in N·s/m. It only pushes, so anything else acting on
// the two Bodies can win against it.
func NewSpringJoint(
	a, b ecs.Entity, anchorA, anchorB m.Vec2d,
	restLength, stiffness, absorption float64,
) Joint {
	return types.NewSpringJoint(a, b, anchorA, anchorB, restLength, stiffness, absorption)
}

// NewRotarySpringJoint pushes the two Bodies towards a rest Angle in radians,
// settling by its Absorption. stiffness is in N·m/rad.
func NewRotarySpringJoint(a, b ecs.Entity, restAngle, stiffness, absorption float64) Joint {
	return types.NewRotarySpringJoint(a, b, restAngle, stiffness, absorption)
}

// NewRotaryLimitJoint holds the Angle of A relative to B within a range of
// radians, and holds nothing inside it.
func NewRotaryLimitJoint(a, b ecs.Entity, minimum, maximum float64) Joint {
	return types.NewRotaryLimitJoint(a, b, minimum, maximum)
}

// NewRatchetJoint lets the Angle of A relative to B turn one way and clicks it
// over in steps of ratchet radians, offset by phase. angle is the Angle it
// starts at, which is A's Angle less B's; the app passes it because a cog
// constructor reads no Store.
func NewRatchetJoint(a, b ecs.Entity, angle, phase, ratchet float64) Joint {
	return types.NewRatchetJoint(a, b, angle, phase, ratchet)
}

// NewGearJoint holds the two Bodies turning in step: A's Angle times the ratio
// less B's Angle, held at the phase.
func NewGearJoint(a, b ecs.Entity, phase, ratio float64) Joint {
	return types.NewGearJoint(a, b, phase, ratio)
}

// NewMotorJoint turns the two Bodies against each other, driving A's Angular
// velocity less B's towards the negation of rate, which is cp's sign.
func NewMotorJoint(a, b ecs.Entity, rate float64) Joint {
	return types.NewMotorJoint(a, b, rate)
}

// NewJointedPairs is an empty set of the pairs a Joint holds apart. The plugin
// publishes one; an app never needs to build one.
func NewJointedPairs() *JointedPairs { return types.NewJointedPairs() }

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

// NewDynamicForShape is the Dynamic body a Shape of that density makes, in
// kg/m², with the two Damping rates in 1/s — and the Shape moved so that its
// centroid is its local origin, which is what Position is. It is how a new
// Dynamic body gets its mass and Moment of inertia; NewDynamic is for the Body
// whose numbers the app already has.
//
// The mass is density times the area, radius included, and the Moment is about
// the centroid, as Chipmunk's AccumulateMassFromShapes computes them for one
// Shape. The Shape and the Polygon go in and come back as NewPolygonShape
// returns them: a circle's offset becomes zero, a segment's endpoints and a
// polygon's vertices are shifted by the centroid, a segment's neighbour
// tangents stay as they are, and the material, the collision fields and Sensor
// are carried over. An inline kind returns the zero Polygon. The caller's
// Polygon is never written: a Poly comes back with a new vertex List, which
// allocates, this being a constructor and not the hot path.
//
// Recentring moves the geometry in the Body's frame, so to leave it where it
// was the app places the Body at the old origin plus the centroid. For a Body
// spawned at origin with an Angle of 0, that is the one line
//
//	place := Position{Current: origin.Add(centroid), Previous: origin.Add(centroid)}
//
// where centroid is the circle's offset, the midpoint of the segment's two
// endpoints, or CentroidForPoly of the outline the polygon was built from. A
// Body spawned turned adds centroid.Rotate(m.ForAngle(angle)) instead. A Body
// left at the old origin turns about it, and nothing reports that.
//
// A Shape with no area — a circle or a segment of radius 0 — is refused with
// ErrNoArea, a density that is not positive and finite with ErrBadDensity, and
// a mass or a Damping rate NewDynamic refuses with its own error. Every refusal
// returns the zero Dynamic and the Shape and the Polygon exactly as given.
func NewDynamicForShape(shape Shape, polygon Polygon, density, damping, angularDamping float64) (Dynamic, Shape, Polygon, error) {
	return types.NewDynamicForShape(shape, polygon, density, damping, angularDamping)
}
