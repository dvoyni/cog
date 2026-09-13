package m

import "math"

// Quat stores the vector part in XYZ and the scalar part in W.
type Quat struct{ X, Y, Z, W float32 }

// NewQuat returns identity with no values, fills all components with one value,
// or accepts X, Y, Z, W in that order.
func NewQuat(values ...float32) Quat {
	switch len(values) {
	case 0:
		return Quat{W: 1}
	case 1:
		return Quat{values[0], values[0], values[0], values[0]}
	case 4:
		return Quat{values[0], values[1], values[2], values[3]}
	default:
		panic("m.NewQuat expects zero, one, or four values")
	}
}

func (quaternion Quat) Add(other Quat) Quat {
	return Quat{quaternion.X + other.X, quaternion.Y + other.Y, quaternion.Z + other.Z, quaternion.W + other.W}
}
func (quaternion Quat) Sub(other Quat) Quat {
	return Quat{quaternion.X - other.X, quaternion.Y - other.Y, quaternion.Z - other.Z, quaternion.W - other.W}
}
func (quaternion Quat) AddS(value float32) Quat {
	return Quat{quaternion.X + value, quaternion.Y + value, quaternion.Z + value, quaternion.W + value}
}
func (quaternion Quat) SubS(value float32) Quat {
	return Quat{quaternion.X - value, quaternion.Y - value, quaternion.Z - value, quaternion.W - value}
}
func (quaternion Quat) MulS(value float32) Quat {
	return Quat{quaternion.X * value, quaternion.Y * value, quaternion.Z * value, quaternion.W * value}
}
func (quaternion Quat) DivS(value float32) Quat {
	return Quat{quaternion.X / value, quaternion.Y / value, quaternion.Z / value, quaternion.W / value}
}
func (quaternion Quat) Negate() Quat {
	return Quat{-quaternion.X, -quaternion.Y, -quaternion.Z, -quaternion.W}
}
func (quaternion Quat) Dot(other Quat) float32 {
	return quaternion.X*other.X + quaternion.Y*other.Y + quaternion.Z*other.Z + quaternion.W*other.W
}
func (quaternion Quat) LengthSquared() float32 { return quaternion.Dot(quaternion) }
func (quaternion Quat) Length() float32        { return sqrt(quaternion.LengthSquared()) }
func (quaternion Quat) Normalize() Quat {
	if length := quaternion.Length(); length != 0 {
		return quaternion.DivS(length)
	}
	return quaternion
}
func (quaternion Quat) Conjugate() Quat {
	return Quat{-quaternion.X, -quaternion.Y, -quaternion.Z, quaternion.W}
}

// Mul returns the Hamilton product quaternion*other, applying other first.
func (quaternion Quat) Mul(other Quat) Quat {
	return Quat{
		X: quaternion.W*other.X + quaternion.X*other.W + quaternion.Y*other.Z - quaternion.Z*other.Y,
		Y: quaternion.W*other.Y - quaternion.X*other.Z + quaternion.Y*other.W + quaternion.Z*other.X,
		Z: quaternion.W*other.Z + quaternion.X*other.Y - quaternion.Y*other.X + quaternion.Z*other.W,
		W: quaternion.W*other.W - quaternion.X*other.X - quaternion.Y*other.Y - quaternion.Z*other.Z,
	}
}

func (quaternion Quat) Inverse() (Quat, bool) {
	lengthSquared := quaternion.LengthSquared()
	if lengthSquared == 0 {
		return Quat{}, false
	}
	return quaternion.Conjugate().DivS(lengthSquared), true
}

func (quaternion Quat) Rotate(vector Vec3) Vec3 {
	inverse, ok := quaternion.Inverse()
	if !ok {
		return vector
	}
	rotated := quaternion.Mul(Quat{X: vector.X, Y: vector.Y, Z: vector.Z}).Mul(inverse)
	return Vec3{rotated.X, rotated.Y, rotated.Z}
}

func (quaternion Quat) NLerp(other Quat, amount float32) Quat {
	if quaternion.Dot(other) < 0 {
		other = other.Negate()
	}
	return quaternion.Add(other.Sub(quaternion).MulS(amount)).Normalize()
}

func (quaternion Quat) SLerp(other Quat, amount float32) Quat {
	cosine := quaternion.Dot(other)
	if cosine < 0 {
		other = other.Negate()
		cosine = -cosine
	}
	cosine = Clamp(cosine, -1, 1)
	if cosine > 0.9995 {
		return quaternion.NLerp(other, amount)
	}
	angle := float32(math.Acos(float64(cosine)))
	sine := float32(math.Sin(float64(angle)))
	fromWeight := float32(math.Sin(float64((1-amount)*angle))) / sine
	toWeight := float32(math.Sin(float64(amount*angle))) / sine
	return quaternion.MulS(fromWeight).Add(other.MulS(toWeight)).Normalize()
}

func QuatAxisAngle(axis Vec3, angle float32) Quat {
	axis = axis.Normalize()
	if axis.LengthSquared() == 0 {
		return NewQuat()
	}
	half := angle / 2
	sine := float32(math.Sin(float64(half)))
	return Quat{axis.X * sine, axis.Y * sine, axis.Z * sine, float32(math.Cos(float64(half)))}
}

func QuatRotationX(angle float32) Quat { return QuatAxisAngle(Vec3{X: 1}, angle) }
func QuatRotationY(angle float32) Quat { return QuatAxisAngle(Vec3{Y: 1}, angle) }
func QuatRotationZ(angle float32) Quat { return QuatAxisAngle(Vec3{Z: 1}, angle) }

// QuatEulerXYZ applies X, then Y, then Z rotations; all components are radians.
func QuatEulerXYZ(angles Vec3) Quat {
	return QuatRotationZ(angles.Z).Mul(QuatRotationY(angles.Y)).Mul(QuatRotationX(angles.X)).Normalize()
}

func (quaternion Quat) Mat3() Mat3 {
	quaternion = quaternion.Normalize()
	xx, yy, zz := quaternion.X*quaternion.X, quaternion.Y*quaternion.Y, quaternion.Z*quaternion.Z
	xy, xz, yz := quaternion.X*quaternion.Y, quaternion.X*quaternion.Z, quaternion.Y*quaternion.Z
	wx, wy, wz := quaternion.W*quaternion.X, quaternion.W*quaternion.Y, quaternion.W*quaternion.Z
	return Mat3{1 - 2*(yy+zz), 2 * (xy + wz), 2 * (xz - wy), 2 * (xy - wz), 1 - 2*(xx+zz), 2 * (yz + wx), 2 * (xz + wy), 2 * (yz - wx), 1 - 2*(xx+yy)}
}

func (quaternion Quat) Mat4() Mat4 {
	matrix := quaternion.Mat3()
	return Mat4{matrix[0], matrix[1], matrix[2], 0, matrix[3], matrix[4], matrix[5], 0, matrix[6], matrix[7], matrix[8], 0, 0, 0, 0, 1}
}

func QuatFromMat4(matrix Mat4) Quat {
	return QuatFromMat3(Mat3{matrix[0], matrix[1], matrix[2], matrix[4], matrix[5], matrix[6], matrix[8], matrix[9], matrix[10]})
}

func QuatFromMat3(matrix Mat3) Quat {
	trace := matrix[0] + matrix[4] + matrix[8]
	var result Quat
	if trace > 0 {
		scale := sqrt(trace+1) * 2
		result = Quat{(matrix[5] - matrix[7]) / scale, (matrix[6] - matrix[2]) / scale, (matrix[1] - matrix[3]) / scale, scale / 4}
	} else if matrix[0] > matrix[4] && matrix[0] > matrix[8] {
		scale := sqrt(1+matrix[0]-matrix[4]-matrix[8]) * 2
		result = Quat{scale / 4, (matrix[3] + matrix[1]) / scale, (matrix[6] + matrix[2]) / scale, (matrix[5] - matrix[7]) / scale}
	} else if matrix[4] > matrix[8] {
		scale := sqrt(1+matrix[4]-matrix[0]-matrix[8]) * 2
		result = Quat{(matrix[3] + matrix[1]) / scale, scale / 4, (matrix[7] + matrix[5]) / scale, (matrix[6] - matrix[2]) / scale}
	} else {
		scale := sqrt(1+matrix[8]-matrix[0]-matrix[4]) * 2
		result = Quat{(matrix[6] + matrix[2]) / scale, (matrix[7] + matrix[5]) / scale, scale / 4, (matrix[1] - matrix[3]) / scale}
	}
	return result.Normalize()
}
