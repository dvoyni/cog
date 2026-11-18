// Package mx stands in for what the sprites ticket adds to libs/m: Mat2x3 and
// its constructors. They would be m's: m.Mat2x3, m.Translation2x3.
package mx

import (
	"math"

	"github.com/dvoyni/cog/libs/m"
)

// Mat2x3 is a 2D affine transform: 2 rows by 3 columns, stored column-major,
// so its 24 bytes are WGSL's mat3x2<f32>, which names the columns first. Its
// last column is the translation. canvas reads a zero Mat2x3 as identity.
type Mat2x3 [6]float32

// Identity2x3 answers the transform that changes nothing.
func Identity2x3() Mat2x3 { return Mat2x3{1, 0, 0, 1, 0, 0} }

// Translation2x3 moves by x and y.
func Translation2x3(x, y float32) Mat2x3 { return Mat2x3{1, 0, 0, 1, x, y} }

// Rotation2x3 turns by angle radians, clockwise on screen since y points down.
func Rotation2x3(angle float32) Mat2x3 {
	sine, cosine := math.Sincos(float64(angle))
	return Mat2x3{float32(cosine), float32(sine), float32(-sine), float32(cosine), 0, 0}
}

// Scaling2x3 scales by x and y; a negative factor mirrors.
func Scaling2x3(x, y float32) Mat2x3 { return Mat2x3{x, 0, 0, y, 0, 0} }

// TRS2x3 places, turns and scales in one call: Translation2x3(translation) ·
// Rotation2x3(rotation) · Scaling2x3(scale), the scale applying first.
func TRS2x3(translation m.Vec2, rotation float32, scale m.Vec2) Mat2x3 {
	return Translation2x3(translation.X, translation.Y).Mul(Rotation2x3(rotation)).Mul(Scaling2x3(scale.X, scale.Y))
}

// Mul answers matrix · other, so other applies first: a parent's transform
// times its child's.
func (matrix Mat2x3) Mul(other Mat2x3) Mat2x3 {
	return Mat2x3{
		matrix[0]*other[0] + matrix[2]*other[1],
		matrix[1]*other[0] + matrix[3]*other[1],
		matrix[0]*other[2] + matrix[2]*other[3],
		matrix[1]*other[2] + matrix[3]*other[3],
		matrix[0]*other[4] + matrix[2]*other[5] + matrix[4],
		matrix[1]*other[4] + matrix[3]*other[5] + matrix[5],
	}
}

// TransformPoint answers point moved by matrix.
func (matrix Mat2x3) TransformPoint(point m.Vec2) m.Vec2 {
	return m.Vec2{
		X: matrix[0]*point.X + matrix[2]*point.Y + matrix[4],
		Y: matrix[1]*point.X + matrix[3]*point.Y + matrix[5],
	}
}
