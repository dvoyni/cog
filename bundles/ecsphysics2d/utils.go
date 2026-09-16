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
