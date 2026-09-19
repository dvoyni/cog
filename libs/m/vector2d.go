package m

import "math"

// smallestNormalFloat64 is C's DBL_MIN, which Chipmunk2D spells CPFLOAT_MIN and
// adds to a denominator so a degenerate input yields a number instead of a NaN.
// Go has no name for it: math.SmallestNonzeroFloat64 is the smallest
// subnormal, three hundred orders of magnitude below C's smallest normal.
//
// jakecoffman/cp weakened the same guard to 1e-15, which is large enough to
// perturb a genuinely small vector rather than merely to stop a divide by zero.
// The value here is the C one.
const smallestNormalFloat64 = 2.2250738585072014e-308

// Vec2d is a vector on the plane in float64, beside Vec2 and Vec2i: the d
// follows the i. It exists because 2D rigid-body physics computes in float64
// and exposes its vectors to gameplay code, which should not have to import a
// physics package to do vector maths.
//
// Its shared methods carry Vec2's names. The rest come from Chipmunk by way of
// jakecoffman/cp v2.4.0 (MIT, Copyright (c) 2017 Jake Coffman), checked against
// Chipmunk2D f2f3d66 (MIT, Copyright (c) 2007-2015 Scott Lembcke and Howling
// Moon Software), and keep their algorithms.
type Vec2d struct{ X, Y float64 }

func NewVec2d(values ...float64) Vec2d {
	switch len(values) {
	case 0:
		return Vec2d{}
	case 1:
		return Vec2d{values[0], values[0]}
	case 2:
		return Vec2d{values[0], values[1]}
	default:
		panic("m.NewVec2d expects zero, one, or two values")
	}
}

// ForAngle is the unit vector at the given angle in radians, turning from +X
// towards +Y. It is the rotation Rotate and Unrotate take.
func ForAngle(angle float64) Vec2d { return Vec2d{math.Cos(angle), math.Sin(angle)} }

func (v Vec2d) Add(other Vec2d) Vec2d { return Vec2d{v.X + other.X, v.Y + other.Y} }
func (v Vec2d) Sub(other Vec2d) Vec2d { return Vec2d{v.X - other.X, v.Y - other.Y} }
func (v Vec2d) MulS(values ...float64) Vec2d {
	x, y := scalar2d("Vec2d.MulS", values)
	return Vec2d{v.X * x, v.Y * y}
}
func (v Vec2d) Negate() Vec2d                { return Vec2d{-v.X, -v.Y} }
func (v Vec2d) Dot(other Vec2d) float64      { return v.X*other.X + v.Y*other.Y }
func (v Vec2d) LengthSquared() float64       { return v.Dot(v) }
func (v Vec2d) Length() float64              { return math.Sqrt(v.Dot(v)) }
func (v Vec2d) Distance(other Vec2d) float64 { return v.Sub(other).Length() }

// Cross is the magnitude of the z component the 3D cross product would have.
func (v Vec2d) Cross(other Vec2d) float64 { return v.X*other.Y - v.Y*other.X }

// Perp turns the vector a quarter turn towards +Y.
func (v Vec2d) Perp() Vec2d { return Vec2d{-v.Y, v.X} }

// ReversePerp turns the vector a quarter turn towards -Y.
func (v Vec2d) ReversePerp() Vec2d { return Vec2d{v.Y, -v.X} }

// Project is the vector's projection onto other.
func (v Vec2d) Project(other Vec2d) Vec2d { return other.MulS(v.Dot(other) / other.Dot(other)) }

// Rotate rotates the vector by a rotation given as a vector, which is the
// complex product of the two. ForAngle builds the rotation, and Unrotate is the
// inverse; a rotation carried as a vector is how the ported code composes one
// without a sine and a cosine per use.
func (v Vec2d) Rotate(rotation Vec2d) Vec2d {
	return Vec2d{v.X*rotation.X - v.Y*rotation.Y, v.X*rotation.Y + v.Y*rotation.X}
}

// Unrotate undoes Rotate by the same rotation.
func (v Vec2d) Unrotate(rotation Vec2d) Vec2d {
	return Vec2d{v.X*rotation.X + v.Y*rotation.Y, v.Y*rotation.X - v.X*rotation.Y}
}

// Normalize is the unit vector in the same direction, and the zero vector
// normalizes to itself.
//
// The divisor carries Chipmunk's guard, which is what makes a zero vector
// return a number rather than a NaN. Vec2.Normalize spells the same intent as
// a branch; this keeps the ported arithmetic branch-free, as cp does.
func (v Vec2d) Normalize() Vec2d { return v.MulS(1.0 / (v.Length() + smallestNormalFloat64)) }

// Lerp interpolates towards other. At amount 1 it is exactly other, which the
// Add-of-a-scaled-difference spelling is not.
func (v Vec2d) Lerp(other Vec2d, amount float64) Vec2d {
	return v.MulS(1.0 - amount).Add(other.MulS(amount))
}

// ClosestT is where along the segment from the vector to other the segment
// comes closest to the origin, as a number in [-1, 1] that LerpT reads.
//
// The denominator carries Chipmunk's CPFLOAT_MIN, which jakecoffman/cp drops:
// without it a degenerate simplex, whose two points coincide, divides zero by
// zero and returns a NaN that reaches the contact through the closest-point
// search. With it the answer is a number.
func (v Vec2d) ClosestT(other Vec2d) float64 {
	delta := other.Sub(v)
	dot := delta.Dot(v.Add(other))
	return -clampd(dot/(delta.LengthSquared()+smallestNormalFloat64), -1.0, 1.0)
}

// Vec2 narrows to float32, which is what a render copy wants.
func (v Vec2d) Vec2() Vec2 { return Vec2{float32(v.X), float32(v.Y)} }

func scalar2d(name string, values []float64) (float64, float64) {
	if len(values) == 1 {
		return values[0], values[0]
	}
	if len(values) == 2 {
		return values[0], values[1]
	}
	panic("m." + name + " expects one or two values")
}

func clampd(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
