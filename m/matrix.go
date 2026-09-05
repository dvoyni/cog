package m

import "math"

// Matrices use column-major storage: element (row, column) is m[column*N+row].
type Mat2 [4]float32
type Mat3 [9]float32
type Mat4 [16]float32

// NewMat2 accepts zero values for identity, one value to fill every element,
// or four values in column-major order.
func NewMat2(values ...float32) Mat2 {
	switch len(values) {
	case 0:
		return Mat2{1, 0, 0, 1}
	case 1:
		return Mat2{values[0], values[0], values[0], values[0]}
	case 4:
		return Mat2(values)
	default:
		panic("m.NewMat2 expects zero, one, or four values")
	}
}

// NewMat3 accepts zero values for identity, one value to fill every element,
// or nine values in column-major order.
func NewMat3(values ...float32) Mat3 {
	switch len(values) {
	case 0:
		return Mat3{1, 0, 0, 0, 1, 0, 0, 0, 1}
	case 1:
		return Mat3{values[0], values[0], values[0], values[0], values[0], values[0], values[0], values[0], values[0]}
	case 9:
		return Mat3(values)
	default:
		panic("m.NewMat3 expects zero, one, or nine values")
	}
}

// NewMat4 accepts zero values for identity, one value to fill every element,
// or sixteen values in column-major order.
func NewMat4(values ...float32) Mat4 {
	switch len(values) {
	case 0:
		return Mat4{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
	case 1:
		value := values[0]
		return Mat4{value, value, value, value, value, value, value, value, value, value, value, value, value, value, value, value}
	case 16:
		return Mat4(values)
	default:
		panic("m.NewMat4 expects zero, one, or sixteen values")
	}
}

func (matrix Mat2) Add(other Mat2) (result Mat2) {
	for index := range result {
		result[index] = matrix[index] + other[index]
	}
	return
}
func (matrix Mat2) Sub(other Mat2) (result Mat2) {
	for index := range result {
		result[index] = matrix[index] - other[index]
	}
	return
}
func (matrix Mat2) MulS(value float32) (result Mat2) {
	for index := range result {
		result[index] = matrix[index] * value
	}
	return
}
func (matrix Mat2) Mul(other Mat2) (result Mat2) {
	for column := 0; column < 2; column++ {
		for row := 0; row < 2; row++ {
			for inner := 0; inner < 2; inner++ {
				result[column*2+row] += matrix[inner*2+row] * other[column*2+inner]
			}
		}
	}
	return
}
func (matrix Mat2) MulVec2(vector Vec2) Vec2 {
	return Vec2{matrix[0]*vector.X + matrix[2]*vector.Y, matrix[1]*vector.X + matrix[3]*vector.Y}
}
func (matrix Mat2) Transpose() Mat2      { return Mat2{matrix[0], matrix[2], matrix[1], matrix[3]} }
func (matrix Mat2) Determinant() float32 { return matrix[0]*matrix[3] - matrix[2]*matrix[1] }
func (matrix Mat2) Inverse() (Mat2, bool) {
	determinant := matrix.Determinant()
	if determinant == 0 {
		return Mat2{}, false
	}
	return Mat2{matrix[3], -matrix[1], -matrix[2], matrix[0]}.MulS(1 / determinant), true
}

func (matrix Mat3) Add(other Mat3) (result Mat3) {
	for index := range result {
		result[index] = matrix[index] + other[index]
	}
	return
}
func (matrix Mat3) Sub(other Mat3) (result Mat3) {
	for index := range result {
		result[index] = matrix[index] - other[index]
	}
	return
}
func (matrix Mat3) MulS(value float32) (result Mat3) {
	for index := range result {
		result[index] = matrix[index] * value
	}
	return
}
func (matrix Mat3) Mul(other Mat3) (result Mat3) {
	for column := 0; column < 3; column++ {
		for row := 0; row < 3; row++ {
			for inner := 0; inner < 3; inner++ {
				result[column*3+row] += matrix[inner*3+row] * other[column*3+inner]
			}
		}
	}
	return
}
func (matrix Mat3) MulVec3(vector Vec3) Vec3 {
	return Vec3{matrix[0]*vector.X + matrix[3]*vector.Y + matrix[6]*vector.Z, matrix[1]*vector.X + matrix[4]*vector.Y + matrix[7]*vector.Z, matrix[2]*vector.X + matrix[5]*vector.Y + matrix[8]*vector.Z}
}
func (matrix Mat3) Transpose() Mat3 {
	return Mat3{matrix[0], matrix[3], matrix[6], matrix[1], matrix[4], matrix[7], matrix[2], matrix[5], matrix[8]}
}
func (matrix Mat3) Determinant() float32 {
	return matrix[0]*(matrix[4]*matrix[8]-matrix[7]*matrix[5]) - matrix[3]*(matrix[1]*matrix[8]-matrix[7]*matrix[2]) + matrix[6]*(matrix[1]*matrix[5]-matrix[4]*matrix[2])
}
func (matrix Mat3) Inverse() (Mat3, bool) {
	inverse, ok := inverseSquare([]float32(matrix[:]), 3)
	if !ok {
		return Mat3{}, false
	}
	return Mat3(inverse), true
}

func (matrix Mat4) Add(other Mat4) (result Mat4) {
	for index := range result {
		result[index] = matrix[index] + other[index]
	}
	return
}
func (matrix Mat4) Sub(other Mat4) (result Mat4) {
	for index := range result {
		result[index] = matrix[index] - other[index]
	}
	return
}
func (matrix Mat4) MulS(value float32) (result Mat4) {
	for index := range result {
		result[index] = matrix[index] * value
	}
	return
}
func (matrix Mat4) Mul(other Mat4) (result Mat4) {
	for column := 0; column < 4; column++ {
		for row := 0; row < 4; row++ {
			for inner := 0; inner < 4; inner++ {
				result[column*4+row] += matrix[inner*4+row] * other[column*4+inner]
			}
		}
	}
	return
}
func (matrix Mat4) MulVec4(vector Vec4) Vec4 {
	return Vec4{matrix[0]*vector.X + matrix[4]*vector.Y + matrix[8]*vector.Z + matrix[12]*vector.W, matrix[1]*vector.X + matrix[5]*vector.Y + matrix[9]*vector.Z + matrix[13]*vector.W, matrix[2]*vector.X + matrix[6]*vector.Y + matrix[10]*vector.Z + matrix[14]*vector.W, matrix[3]*vector.X + matrix[7]*vector.Y + matrix[11]*vector.Z + matrix[15]*vector.W}
}
func (matrix Mat4) Transpose() Mat4 {
	return Mat4{matrix[0], matrix[4], matrix[8], matrix[12], matrix[1], matrix[5], matrix[9], matrix[13], matrix[2], matrix[6], matrix[10], matrix[14], matrix[3], matrix[7], matrix[11], matrix[15]}
}
func (matrix Mat4) Determinant() float32 {
	_, determinant := eliminateSquare([]float32(matrix[:]), 4)
	return determinant
}
func (matrix Mat4) Inverse() (Mat4, bool) {
	inverse, ok := inverseSquare([]float32(matrix[:]), 4)
	if !ok {
		return Mat4{}, false
	}
	return Mat4(inverse), true
}

// Mat3 returns the upper-left 3x3 block, the rotation and scale of an affine
// transform. The normal matrix is this block inverted and transposed.
func (matrix Mat4) Mat3() Mat3 {
	return Mat3{matrix[0], matrix[1], matrix[2], matrix[4], matrix[5], matrix[6], matrix[8], matrix[9], matrix[10]}
}

// Translation returns the fourth column, the translation of an affine transform.
func (matrix Mat4) Translation() Vec3 { return Vec3{matrix[12], matrix[13], matrix[14]} }

// TransformPoint transforms a point, applying translation and dropping the
// perspective divide. Use Project when the matrix is a projection.
func (matrix Mat4) TransformPoint(point Vec3) Vec3 {
	return Vec3{
		matrix[0]*point.X + matrix[4]*point.Y + matrix[8]*point.Z + matrix[12],
		matrix[1]*point.X + matrix[5]*point.Y + matrix[9]*point.Z + matrix[13],
		matrix[2]*point.X + matrix[6]*point.Y + matrix[10]*point.Z + matrix[14],
	}
}

// TransformDirection transforms a direction, ignoring translation. The result
// is not renormalized, so a scaling matrix changes its length.
func (matrix Mat4) TransformDirection(direction Vec3) Vec3 {
	return Vec3{
		matrix[0]*direction.X + matrix[4]*direction.Y + matrix[8]*direction.Z,
		matrix[1]*direction.X + matrix[5]*direction.Y + matrix[9]*direction.Z,
		matrix[2]*direction.X + matrix[6]*direction.Y + matrix[10]*direction.Z,
	}
}

// InverseAffine inverts a matrix whose last row is (0, 0, 0, 1) by cofactor
// expansion of the upper-left block, allocating nothing. The general Inverse
// allocates five slices per call, which is too much per instance per frame.
// The last row is assumed rather than checked; a projection needs Inverse.
func (matrix Mat4) InverseAffine() (Mat4, bool) {
	cofactor0 := matrix[5]*matrix[10] - matrix[9]*matrix[6]
	cofactor1 := matrix[9]*matrix[2] - matrix[1]*matrix[10]
	cofactor2 := matrix[1]*matrix[6] - matrix[5]*matrix[2]
	determinant := matrix[0]*cofactor0 + matrix[4]*cofactor1 + matrix[8]*cofactor2
	if determinant == 0 {
		return Mat4{}, false
	}
	scale := 1 / determinant
	inverse := Mat4{
		cofactor0 * scale, cofactor1 * scale, cofactor2 * scale, 0,
		(matrix[8]*matrix[6] - matrix[4]*matrix[10]) * scale, (matrix[0]*matrix[10] - matrix[8]*matrix[2]) * scale, (matrix[4]*matrix[2] - matrix[0]*matrix[6]) * scale, 0,
		(matrix[4]*matrix[9] - matrix[8]*matrix[5]) * scale, (matrix[8]*matrix[1] - matrix[0]*matrix[9]) * scale, (matrix[0]*matrix[5] - matrix[4]*matrix[1]) * scale, 0,
		0, 0, 0, 1,
	}
	inverse[12] = -(inverse[0]*matrix[12] + inverse[4]*matrix[13] + inverse[8]*matrix[14])
	inverse[13] = -(inverse[1]*matrix[12] + inverse[5]*matrix[13] + inverse[9]*matrix[14])
	inverse[14] = -(inverse[2]*matrix[12] + inverse[6]*matrix[13] + inverse[10]*matrix[14])
	return inverse, true
}

// Decompose splits an affine matrix back into the arguments TRS4 composes,
// reporting false when an axis has collapsed to zero length. A negative
// determinant is reported as a negative X scale, since a mirror cannot be a
// rotation. Unlike QuatFromMat4 it divides scale out first, so the rotation is
// correct for scaled matrices.
func (matrix Mat4) Decompose() (translation Vec3, rotation Quat, scale Vec3, ok bool) {
	basis := matrix.Mat3()
	scale = Vec3{
		Vec3{basis[0], basis[1], basis[2]}.Length(),
		Vec3{basis[3], basis[4], basis[5]}.Length(),
		Vec3{basis[6], basis[7], basis[8]}.Length(),
	}
	if basis.Determinant() < 0 {
		scale.X = -scale.X
	}
	if scale.X == 0 || scale.Y == 0 || scale.Z == 0 {
		return Vec3{}, NewQuat(), Vec3{}, false
	}
	for column, axis := range [3]float32{scale.X, scale.Y, scale.Z} {
		inverse := 1 / axis
		basis[column*3], basis[column*3+1], basis[column*3+2] = basis[column*3]*inverse, basis[column*3+1]*inverse, basis[column*3+2]*inverse
	}
	return matrix.Translation(), QuatFromMat3(basis), scale, true
}

func Scaling2(x, y float32) Mat2 { return Mat2{x, 0, 0, y} }
func Rotation2(angle float32) Mat2 {
	sine, cosine := float32(math.Sin(float64(angle))), float32(math.Cos(float64(angle)))
	return Mat2{cosine, sine, -sine, cosine}
}
func Scaling3(x, y, z float32) Mat3 { return Mat3{x, 0, 0, 0, y, 0, 0, 0, z} }
func RotationX3(angle float32) Mat3 {
	sine, cosine := float32(math.Sin(float64(angle))), float32(math.Cos(float64(angle)))
	return Mat3{1, 0, 0, 0, cosine, sine, 0, -sine, cosine}
}
func RotationY3(angle float32) Mat3 {
	sine, cosine := float32(math.Sin(float64(angle))), float32(math.Cos(float64(angle)))
	return Mat3{cosine, 0, -sine, 0, 1, 0, sine, 0, cosine}
}
func RotationZ3(angle float32) Mat3 {
	sine, cosine := float32(math.Sin(float64(angle))), float32(math.Cos(float64(angle)))
	return Mat3{cosine, sine, 0, -sine, cosine, 0, 0, 0, 1}
}
func Translation4(x, y, z float32) Mat4 {
	result := NewMat4()
	result[12], result[13], result[14] = x, y, z
	return result
}
func Scaling4(x, y, z float32) Mat4 { return Mat4{x, 0, 0, 0, 0, y, 0, 0, 0, 0, z, 0, 0, 0, 0, 1} }
func RotationX4(angle float32) Mat4 {
	rotation := RotationX3(angle)
	return Mat4{rotation[0], rotation[1], rotation[2], 0, rotation[3], rotation[4], rotation[5], 0, rotation[6], rotation[7], rotation[8], 0, 0, 0, 0, 1}
}
func RotationY4(angle float32) Mat4 {
	rotation := RotationY3(angle)
	return Mat4{rotation[0], rotation[1], rotation[2], 0, rotation[3], rotation[4], rotation[5], 0, rotation[6], rotation[7], rotation[8], 0, 0, 0, 0, 1}
}
func RotationZ4(angle float32) Mat4 {
	rotation := RotationZ3(angle)
	return Mat4{rotation[0], rotation[1], rotation[2], 0, rotation[3], rotation[4], rotation[5], 0, rotation[6], rotation[7], rotation[8], 0, 0, 0, 0, 1}
}

// TRS4 composes translation, rotation and scale as T*R*S, the order glTF nodes
// use, without the two intermediate matrices the equivalent Mul chain builds.
func TRS4(translation Vec3, rotation Quat, scale Vec3) Mat4 {
	basis := rotation.Mat3()
	return Mat4{
		basis[0] * scale.X, basis[1] * scale.X, basis[2] * scale.X, 0,
		basis[3] * scale.Y, basis[4] * scale.Y, basis[5] * scale.Y, 0,
		basis[6] * scale.Z, basis[7] * scale.Z, basis[8] * scale.Z, 0,
		translation.X, translation.Y, translation.Z, 1,
	}
}

func Perspective4(fieldOfViewY, aspect, near, far float32) Mat4 {
	focalLength := float32(1 / math.Tan(float64(fieldOfViewY)/2))
	depth := 1 / (near - far)
	return Mat4{focalLength / aspect, 0, 0, 0, 0, focalLength, 0, 0, 0, 0, far * depth, -1, 0, 0, far * near * depth, 0}
}

// Orthographic4 builds a right-handed orthographic projection looking down -Z,
// writing clip depth in 0..1 like Perspective4 and so like WebGPU. glTF gives
// half-extents, so left and right are -xmag and +xmag.
func Orthographic4(left, right, bottom, top, near, far float32) Mat4 {
	width, height, depth := 1/(right-left), 1/(top-bottom), 1/(near-far)
	return Mat4{2 * width, 0, 0, 0, 0, 2 * height, 0, 0, 0, 0, depth, 0, -(right + left) * width, -(top + bottom) * height, near * depth, 1}
}

// lookAtFallbackUps are tried in order when the caller's up is parallel to
// forward, so a camera looking straight down gets a usable basis instead of a
// degenerate one.
var lookAtFallbackUps = [...]Vec3{{Y: 1}, {Z: 1}}

func LookAt4(eye, center, up Vec3) Mat4 {
	const parallelEpsilonSquared = 1e-6
	forward := center.Sub(eye).Normalize()
	side := forward.Cross(up.Normalize())
	for index := 0; side.LengthSquared() < parallelEpsilonSquared && index < len(lookAtFallbackUps); index++ {
		side = forward.Cross(lookAtFallbackUps[index])
	}
	side = side.Normalize()
	vertical := side.Cross(forward)
	return Mat4{side.X, vertical.X, -forward.X, 0, side.Y, vertical.Y, -forward.Y, 0, side.Z, vertical.Z, -forward.Z, 0, -side.Dot(eye), -vertical.Dot(eye), forward.Dot(eye), 1}
}

// projectEpsilon is the smallest clip-space w Project accepts. Dividing by a
// zero or negative w yields a plausible, mirrored, confidently wrong point, so
// a point at or behind the eye gets no coordinate at all.
const projectEpsilon = 1e-6

// Project maps a world point through a view-projection into normalized device
// coordinates, reporting false when the point is at or behind the eye.
func Project(viewProjection Mat4, world Vec3) (ndc Vec3, ok bool) {
	clip := viewProjection.MulVec4(world.Vec4(1))
	if clip.W <= projectEpsilon {
		return Vec3{}, false
	}
	inverse := 1 / clip.W
	return Vec3{clip.X * inverse, clip.Y * inverse, clip.Z * inverse}, true
}

// Unproject maps normalized device coordinates back to a world point through an
// already-inverted view-projection, returning the zero vector for a degenerate
// one. A perspective view-projection is not affine, so that inverse comes from
// Mat4.Inverse rather than InverseAffine.
func Unproject(inverseViewProjection Mat4, ndc Vec3) Vec3 {
	world := inverseViewProjection.MulVec4(ndc.Vec4(1))
	if world.W == 0 {
		return Vec3{}
	}
	inverse := 1 / world.W
	return Vec3{world.X * inverse, world.Y * inverse, world.Z * inverse}
}

func inverseSquare(matrix []float32, size int) ([]float32, bool) {
	rows := make([][]float32, size)
	for row := 0; row < size; row++ {
		rows[row] = make([]float32, size*2)
		for column := 0; column < size; column++ {
			rows[row][column] = matrix[column*size+row]
		}
		rows[row][size+row] = 1
	}
	for pivot := 0; pivot < size; pivot++ {
		best := pivot
		for row := pivot + 1; row < size; row++ {
			if abs32(rows[row][pivot]) > abs32(rows[best][pivot]) {
				best = row
			}
		}
		if rows[best][pivot] == 0 {
			return nil, false
		}
		rows[pivot], rows[best] = rows[best], rows[pivot]
		divisor := rows[pivot][pivot]
		for column := 0; column < size*2; column++ {
			rows[pivot][column] /= divisor
		}
		for row := 0; row < size; row++ {
			if row == pivot {
				continue
			}
			factor := rows[row][pivot]
			for column := 0; column < size*2; column++ {
				rows[row][column] -= factor * rows[pivot][column]
			}
		}
	}
	result := make([]float32, size*size)
	for row := 0; row < size; row++ {
		for column := 0; column < size; column++ {
			result[column*size+row] = rows[row][size+column]
		}
	}
	return result, true
}

func eliminateSquare(matrix []float32, size int) ([]float32, float32) {
	rows := make([][]float32, size)
	for row := 0; row < size; row++ {
		rows[row] = make([]float32, size)
		for column := 0; column < size; column++ {
			rows[row][column] = matrix[column*size+row]
		}
	}
	determinant := float32(1)
	for pivot := 0; pivot < size; pivot++ {
		best := pivot
		for row := pivot + 1; row < size; row++ {
			if abs32(rows[row][pivot]) > abs32(rows[best][pivot]) {
				best = row
			}
		}
		if rows[best][pivot] == 0 {
			return matrix, 0
		}
		if best != pivot {
			rows[pivot], rows[best] = rows[best], rows[pivot]
			determinant = -determinant
		}
		determinant *= rows[pivot][pivot]
		for row := pivot + 1; row < size; row++ {
			factor := rows[row][pivot] / rows[pivot][pivot]
			for column := pivot + 1; column < size; column++ {
				rows[row][column] -= factor * rows[pivot][column]
			}
		}
	}
	return matrix, determinant
}

func abs32(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}
