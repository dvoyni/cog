package m

// Mat2x3 is a 2D affine transform, 2 rows by 3 columns: its first four
// elements are its linear part as a Mat2, and its last column the translation.
// Its 24 bytes are WGSL's mat3x2<f32> byte for byte, which names the columns
// first: three vec2<f32> columns, each 8 bytes apart.
//
// Its zero value sends every point to the origin; a caller may read it as
// identity, as canvas does for an entry's transform, and Translation2x3(0, 0)
// is the identity itself.
type Mat2x3 [6]float32

func makeMat2x3(linear Mat2, translation Vec2) Mat2x3 {
	return Mat2x3{linear[0], linear[1], linear[2], linear[3], translation.X, translation.Y}
}

// Translation2x3 moves by x and y.
func Translation2x3(x, y float32) Mat2x3 { return makeMat2x3(NewMat2(), Vec2{X: x, Y: y}) }

// Rotation2x3 turns by angle radians, from +X towards +Y: counterclockwise
// with Y up, clockwise on a screen whose Y points down.
func Rotation2x3(angle float32) Mat2x3 { return makeMat2x3(Rotation2(angle), Vec2{}) }

// Scaling2x3 scales by x and y about the origin; a negative factor mirrors.
func Scaling2x3(x, y float32) Mat2x3 { return makeMat2x3(Scaling2(x, y), Vec2{}) }

// TRS2x3 composes translation, rotation and scale as T*R*S, the scale applying
// first.
func TRS2x3(translation Vec2, rotation float32, scale Vec2) Mat2x3 {
	return makeMat2x3(Rotation2(rotation).Mul(Scaling2(scale.X, scale.Y)), translation)
}

// Mul answers matrix * other, so other applies first: a parent's transform
// times its child's.
func (matrix Mat2x3) Mul(other Mat2x3) Mat2x3 {
	return makeMat2x3(matrix.linear().Mul(other.linear()), matrix.TransformPoint(other.translation()))
}

// Inverse answers the transform that undoes matrix, reporting false when its
// linear part has collapsed, a zero scale on some axis.
func (matrix Mat2x3) Inverse() (Mat2x3, bool) {
	linear, ok := matrix.linear().Inverse()
	if !ok {
		return Mat2x3{}, false
	}
	return makeMat2x3(linear, linear.MulVec2(matrix.translation()).Negate()), true
}

// TransformPoint answers point moved by matrix, translation included.
func (matrix Mat2x3) TransformPoint(point Vec2) Vec2 {
	return matrix.linear().MulVec2(point).Add(matrix.translation())
}

// TransformRect answers the bounding box of rect's corners moved by matrix,
// rect itself under a transform that only moves and scales it.
func (matrix Mat2x3) TransformRect(rect Rect) Rect {
	rect = rect.Normalize()
	halfSize := rect.Size().MulS(0.5)
	centre := matrix.TransformPoint(rect.Center())
	// Each axis of the box spans the rectangle's half sizes along the
	// columns, whichever way they point.
	halfExtent := Vec2{
		X: abs32(matrix[0])*halfSize.X + abs32(matrix[2])*halfSize.Y,
		Y: abs32(matrix[1])*halfSize.X + abs32(matrix[3])*halfSize.Y,
	}
	return Rect{X: centre.X - halfExtent.X, Y: centre.Y - halfExtent.Y, Width: 2 * halfExtent.X, Height: 2 * halfExtent.Y}
}

// Mat3 answers the homogeneous form, matrix with a bottom row of (0, 0, 1),
// which transforms (x, y, 1) as TransformPoint transforms (x, y).
func (matrix Mat2x3) Mat3() Mat3 {
	return Mat3{matrix[0], matrix[1], 0, matrix[2], matrix[3], 0, matrix[4], matrix[5], 1}
}

func (matrix Mat2x3) linear() Mat2      { return Mat2(matrix[:4]) }
func (matrix Mat2x3) translation() Vec2 { return Vec2{X: matrix[4], Y: matrix[5]} }
