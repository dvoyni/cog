package internal

import (
	"math"

	"github.com/dvoyni/cog/libs/m"
)

// Transform is a 2D affine transform, the matrix
//
//	| A C TX |
//	| B D TY |
//
// Ported from cp's Transform. cp keeps the six fields unexported behind
// constructors that take them in reading order; they are exported here because
// cog's value types are data-driven and the six carry no invariant between
// them. The constructors keep cp's reading order all the same, so a ported call
// site reads as it does in cp.
type Transform struct{ A, B, C, D, TX, TY float64 }

// NewTransform takes the six in reading order, row by row.
//
// cp declares this twice, as NewTransform and NewTransformTranspose with
// identical bodies; the duplicate is dropped.
func NewTransform(a, c, tx, b, d, ty float64) Transform {
	return Transform{A: a, B: b, C: c, D: d, TX: tx, TY: ty}
}

// NewTransformIdentity is the transform that changes nothing.
func NewTransformIdentity() Transform {
	return NewTransform(
		1, 0, 0,
		0, 1, 0,
	)
}

// NewTransformTranslate moves by a vector.
func NewTransformTranslate(translate m.Vec2d) Transform {
	return NewTransform(
		1, 0, translate.X,
		0, 1, translate.Y,
	)
}

// NewTransformScale scales each axis.
func NewTransformScale(scaleX, scaleY float64) Transform {
	return NewTransform(
		scaleX, 0, 0,
		0, scaleY, 0,
	)
}

// NewTransformRotate turns by an angle in radians.
func NewTransformRotate(radians float64) Transform {
	rotation := m.ForAngle(radians)
	return NewTransform(
		rotation.X, -rotation.Y, 0,
		rotation.Y, rotation.X, 0,
	)
}

// NewTransformRigid turns by an angle and then moves, which is the only kind of
// transform a Body has: rotation about its Position, and no scale.
func NewTransformRigid(translate m.Vec2d, radians float64) Transform {
	rotation := m.ForAngle(radians)
	return NewTransform(
		rotation.X, -rotation.Y, translate.X,
		rotation.Y, rotation.X, translate.Y,
	)
}

// NewTransformRigidInverse undoes a rigid transform without the divide Inverse
// does, which is safe because a rigid transform's determinant is 1.
func NewTransformRigidInverse(t Transform) Transform {
	return NewTransform(
		t.D, -t.C, t.C*t.TY-t.TX*t.D,
		-t.B, t.A, t.TX*t.B-t.A*t.TY,
	)
}

// Inverse is the transform that undoes this one.
func (t Transform) Inverse() Transform {
	inverseDeterminant := 1.0 / (t.A*t.D - t.C*t.B)
	return NewTransform(
		t.D*inverseDeterminant, -t.C*inverseDeterminant, (t.C*t.TY-t.TX*t.D)*inverseDeterminant,
		-t.B*inverseDeterminant, t.A*inverseDeterminant, (t.TX*t.B-t.A*t.TY)*inverseDeterminant,
	)
}

// Mul composes two transforms, applying other first.
func (t Transform) Mul(other Transform) Transform {
	return NewTransform(
		t.A*other.A+t.C*other.B, t.A*other.C+t.C*other.D, t.A*other.TX+t.C*other.TY+t.TX,
		t.B*other.A+t.D*other.B, t.B*other.C+t.D*other.D, t.B*other.TX+t.D*other.TY+t.TY,
	)
}

// Point transforms a position, translation included.
func (t Transform) Point(p m.Vec2d) m.Vec2d {
	return m.Vec2d{X: t.A*p.X + t.C*p.Y + t.TX, Y: t.B*p.X + t.D*p.Y + t.TY}
}

// Vec transforms a direction, which is Point without the translation.
func (t Transform) Vec(v m.Vec2d) m.Vec2d {
	return m.Vec2d{X: t.A*v.X + t.C*v.Y, Y: t.B*v.X + t.D*v.Y}
}

// BB is the axis-aligned box around the transformed box, which is larger than
// the transformed box unless the transform is axis-aligned itself.
func (t Transform) BB(bb BB) BB {
	halfWidth := (bb.R - bb.L) * 0.5
	halfHeight := (bb.T - bb.B) * 0.5

	a := t.A * halfWidth
	b := t.C * halfHeight
	d := t.B * halfWidth
	e := t.D * halfHeight
	return NewBBForExtents(
		t.Point(bb.Centre()),
		math.Max(math.Abs(a+b), math.Abs(a-b)),
		math.Max(math.Abs(d+e), math.Abs(d-e)),
	)
}
