// Package m provides immutable value-style mathematics for Cog. All angles use
// radians.
package m

import "math"

type Vec2 struct{ X, Y float32 }
type Vec3 struct{ X, Y, Z float32 }
type Vec4 struct{ X, Y, Z, W float32 }

type Vec2i struct{ X, Y int }
type Vec3i struct{ X, Y, Z int }
type Vec4i struct{ X, Y, Z, W int }

func NewVec2(values ...float32) Vec2 {
	switch len(values) {
	case 0:
		return Vec2{}
	case 1:
		return Vec2{values[0], values[0]}
	case 2:
		return Vec2{values[0], values[1]}
	default:
		panic("m.NewVec2 expects zero, one, or two values")
	}
}

func NewVec3(values ...float32) Vec3 {
	switch len(values) {
	case 0:
		return Vec3{}
	case 1:
		return Vec3{values[0], values[0], values[0]}
	case 3:
		return Vec3{values[0], values[1], values[2]}
	default:
		panic("m.NewVec3 expects zero, one, or three values")
	}
}

func NewVec4(values ...float32) Vec4 {
	switch len(values) {
	case 0:
		return Vec4{}
	case 1:
		return Vec4{values[0], values[0], values[0], values[0]}
	case 4:
		return Vec4{values[0], values[1], values[2], values[3]}
	default:
		panic("m.NewVec4 expects zero, one, or four values")
	}
}

func NewVec2i(values ...int) Vec2i {
	switch len(values) {
	case 0:
		return Vec2i{}
	case 1:
		return Vec2i{values[0], values[0]}
	case 2:
		return Vec2i{values[0], values[1]}
	default:
		panic("m.NewVec2i expects zero, one, or two values")
	}
}

func NewVec3i(values ...int) Vec3i {
	switch len(values) {
	case 0:
		return Vec3i{}
	case 1:
		return Vec3i{values[0], values[0], values[0]}
	case 3:
		return Vec3i{values[0], values[1], values[2]}
	default:
		panic("m.NewVec3i expects zero, one, or three values")
	}
}

func NewVec4i(values ...int) Vec4i {
	switch len(values) {
	case 0:
		return Vec4i{}
	case 1:
		return Vec4i{values[0], values[0], values[0], values[0]}
	case 4:
		return Vec4i{values[0], values[1], values[2], values[3]}
	default:
		panic("m.NewVec4i expects zero, one, or four values")
	}
}

func (v Vec2) Add(other Vec2) Vec2 { return Vec2{v.X + other.X, v.Y + other.Y} }
func (v Vec2) Sub(other Vec2) Vec2 { return Vec2{v.X - other.X, v.Y - other.Y} }
func (v Vec2) Mul(other Vec2) Vec2 { return Vec2{v.X * other.X, v.Y * other.Y} }
func (v Vec2) Div(other Vec2) Vec2 { return Vec2{v.X / other.X, v.Y / other.Y} }
func (v Vec2) AddS(values ...float32) Vec2 {
	x, y := scalar2f("Vec2.AddS", values)
	return Vec2{v.X + x, v.Y + y}
}
func (v Vec2) SubS(values ...float32) Vec2 {
	x, y := scalar2f("Vec2.SubS", values)
	return Vec2{v.X - x, v.Y - y}
}
func (v Vec2) MulS(values ...float32) Vec2 {
	x, y := scalar2f("Vec2.MulS", values)
	return Vec2{v.X * x, v.Y * y}
}
func (v Vec2) DivS(values ...float32) Vec2 {
	x, y := scalar2f("Vec2.DivS", values)
	return Vec2{v.X / x, v.Y / y}
}
func (v Vec2) Negate() Vec2                { return Vec2{-v.X, -v.Y} }
func (v Vec2) Dot(other Vec2) float32      { return v.X*other.X + v.Y*other.Y }
func (v Vec2) Cross(other Vec2) float32    { return v.X*other.Y - v.Y*other.X }
func (v Vec2) LengthSquared() float32      { return v.Dot(v) }
func (v Vec2) Length() float32             { return sqrt(v.LengthSquared()) }
func (v Vec2) Distance(other Vec2) float32 { return v.Sub(other).Length() }
func (v Vec2) Normalize() Vec2 {
	if length := v.Length(); length != 0 {
		return v.DivS(length)
	}
	return v
}
func (v Vec2) Lerp(other Vec2, amount float32) Vec2 { return v.Add(other.Sub(v).MulS(amount)) }
func (v Vec2) Round() Vec2                          { return Vec2{round(v.X), round(v.Y)} }
func (v Vec2) Floor() Vec2                          { return Vec2{floor(v.X), floor(v.Y)} }
func (v Vec2) Ceil() Vec2                           { return Vec2{ceil(v.X), ceil(v.Y)} }
func (v Vec2) Int() Vec2i                           { return Vec2i{int(v.X), int(v.Y)} }

func (v Vec3) Add(other Vec3) Vec3 { return Vec3{v.X + other.X, v.Y + other.Y, v.Z + other.Z} }
func (v Vec3) Sub(other Vec3) Vec3 { return Vec3{v.X - other.X, v.Y - other.Y, v.Z - other.Z} }
func (v Vec3) Mul(other Vec3) Vec3 { return Vec3{v.X * other.X, v.Y * other.Y, v.Z * other.Z} }
func (v Vec3) Div(other Vec3) Vec3 { return Vec3{v.X / other.X, v.Y / other.Y, v.Z / other.Z} }
func (v Vec3) AddS(values ...float32) Vec3 {
	x, y, z := scalar3f("Vec3.AddS", values)
	return Vec3{v.X + x, v.Y + y, v.Z + z}
}
func (v Vec3) SubS(values ...float32) Vec3 {
	x, y, z := scalar3f("Vec3.SubS", values)
	return Vec3{v.X - x, v.Y - y, v.Z - z}
}
func (v Vec3) MulS(values ...float32) Vec3 {
	x, y, z := scalar3f("Vec3.MulS", values)
	return Vec3{v.X * x, v.Y * y, v.Z * z}
}
func (v Vec3) DivS(values ...float32) Vec3 {
	x, y, z := scalar3f("Vec3.DivS", values)
	return Vec3{v.X / x, v.Y / y, v.Z / z}
}
func (v Vec3) Negate() Vec3           { return Vec3{-v.X, -v.Y, -v.Z} }
func (v Vec3) Dot(other Vec3) float32 { return v.X*other.X + v.Y*other.Y + v.Z*other.Z }
func (v Vec3) Cross(other Vec3) Vec3 {
	return Vec3{v.Y*other.Z - v.Z*other.Y, v.Z*other.X - v.X*other.Z, v.X*other.Y - v.Y*other.X}
}
func (v Vec3) LengthSquared() float32      { return v.Dot(v) }
func (v Vec3) Length() float32             { return sqrt(v.LengthSquared()) }
func (v Vec3) Distance(other Vec3) float32 { return v.Sub(other).Length() }
func (v Vec3) Normalize() Vec3 {
	if length := v.Length(); length != 0 {
		return v.DivS(length)
	}
	return v
}
func (v Vec3) Lerp(other Vec3, amount float32) Vec3 { return v.Add(other.Sub(v).MulS(amount)) }
func (v Vec3) Round() Vec3                          { return Vec3{round(v.X), round(v.Y), round(v.Z)} }
func (v Vec3) Floor() Vec3                          { return Vec3{floor(v.X), floor(v.Y), floor(v.Z)} }
func (v Vec3) Ceil() Vec3                           { return Vec3{ceil(v.X), ceil(v.Y), ceil(v.Z)} }
func (v Vec3) Int() Vec3i                           { return Vec3i{int(v.X), int(v.Y), int(v.Z)} }
func (v Vec3) Min(other Vec3) Vec3 {
	return Vec3{min(v.X, other.X), min(v.Y, other.Y), min(v.Z, other.Z)}
}
func (v Vec3) Max(other Vec3) Vec3 {
	return Vec3{max(v.X, other.X), max(v.Y, other.Y), max(v.Z, other.Z)}
}
func (v Vec3) Abs() Vec3           { return Vec3{abs32(v.X), abs32(v.Y), abs32(v.Z)} }
func (v Vec3) Vec4(w float32) Vec4 { return Vec4{v.X, v.Y, v.Z, w} }

func (v Vec4) Add(other Vec4) Vec4 {
	return Vec4{v.X + other.X, v.Y + other.Y, v.Z + other.Z, v.W + other.W}
}
func (v Vec4) Sub(other Vec4) Vec4 {
	return Vec4{v.X - other.X, v.Y - other.Y, v.Z - other.Z, v.W - other.W}
}
func (v Vec4) Mul(other Vec4) Vec4 {
	return Vec4{v.X * other.X, v.Y * other.Y, v.Z * other.Z, v.W * other.W}
}
func (v Vec4) Div(other Vec4) Vec4 {
	return Vec4{v.X / other.X, v.Y / other.Y, v.Z / other.Z, v.W / other.W}
}
func (v Vec4) AddS(values ...float32) Vec4 {
	x, y, z, w := scalar4f("Vec4.AddS", values)
	return Vec4{v.X + x, v.Y + y, v.Z + z, v.W + w}
}
func (v Vec4) SubS(values ...float32) Vec4 {
	x, y, z, w := scalar4f("Vec4.SubS", values)
	return Vec4{v.X - x, v.Y - y, v.Z - z, v.W - w}
}
func (v Vec4) MulS(values ...float32) Vec4 {
	x, y, z, w := scalar4f("Vec4.MulS", values)
	return Vec4{v.X * x, v.Y * y, v.Z * z, v.W * w}
}
func (v Vec4) DivS(values ...float32) Vec4 {
	x, y, z, w := scalar4f("Vec4.DivS", values)
	return Vec4{v.X / x, v.Y / y, v.Z / z, v.W / w}
}
func (v Vec4) Negate() Vec4                { return Vec4{-v.X, -v.Y, -v.Z, -v.W} }
func (v Vec4) Dot(other Vec4) float32      { return v.X*other.X + v.Y*other.Y + v.Z*other.Z + v.W*other.W }
func (v Vec4) LengthSquared() float32      { return v.Dot(v) }
func (v Vec4) Length() float32             { return sqrt(v.LengthSquared()) }
func (v Vec4) Distance(other Vec4) float32 { return v.Sub(other).Length() }
func (v Vec4) Normalize() Vec4 {
	if length := v.Length(); length != 0 {
		return v.DivS(length)
	}
	return v
}
func (v Vec4) Lerp(other Vec4, amount float32) Vec4 { return v.Add(other.Sub(v).MulS(amount)) }
func (v Vec4) Round() Vec4                          { return Vec4{round(v.X), round(v.Y), round(v.Z), round(v.W)} }
func (v Vec4) Floor() Vec4                          { return Vec4{floor(v.X), floor(v.Y), floor(v.Z), floor(v.W)} }
func (v Vec4) Ceil() Vec4                           { return Vec4{ceil(v.X), ceil(v.Y), ceil(v.Z), ceil(v.W)} }
func (v Vec4) Vec3() Vec3                           { return Vec3{v.X, v.Y, v.Z} }
func (v Vec4) Int() Vec4i                           { return Vec4i{int(v.X), int(v.Y), int(v.Z), int(v.W)} }

func (v Vec2i) Add(other Vec2i) Vec2i { return Vec2i{v.X + other.X, v.Y + other.Y} }
func (v Vec2i) Sub(other Vec2i) Vec2i { return Vec2i{v.X - other.X, v.Y - other.Y} }
func (v Vec2i) Mul(other Vec2i) Vec2i { return Vec2i{v.X * other.X, v.Y * other.Y} }
func (v Vec2i) Div(other Vec2i) Vec2i { return Vec2i{v.X / other.X, v.Y / other.Y} }
func (v Vec2i) AddS(values ...int) Vec2i {
	x, y := scalar2i("Vec2i.AddS", values)
	return Vec2i{v.X + x, v.Y + y}
}
func (v Vec2i) SubS(values ...int) Vec2i {
	x, y := scalar2i("Vec2i.SubS", values)
	return Vec2i{v.X - x, v.Y - y}
}
func (v Vec2i) MulS(values ...int) Vec2i {
	x, y := scalar2i("Vec2i.MulS", values)
	return Vec2i{v.X * x, v.Y * y}
}
func (v Vec2i) DivS(values ...int) Vec2i {
	x, y := scalar2i("Vec2i.DivS", values)
	return Vec2i{v.X / x, v.Y / y}
}
func (v Vec2i) Negate() Vec2i                { return Vec2i{-v.X, -v.Y} }
func (v Vec2i) Dot(other Vec2i) int          { return v.X*other.X + v.Y*other.Y }
func (v Vec2i) Cross(other Vec2i) int        { return v.X*other.Y - v.Y*other.X }
func (v Vec2i) LengthSquared() int           { return v.Dot(v) }
func (v Vec2i) Length() float32              { return sqrt(float32(v.LengthSquared())) }
func (v Vec2i) Distance(other Vec2i) float32 { return v.Sub(other).Length() }
func (v Vec2i) Float() Vec2                  { return Vec2{float32(v.X), float32(v.Y)} }

func (v Vec3i) Add(other Vec3i) Vec3i { return Vec3i{v.X + other.X, v.Y + other.Y, v.Z + other.Z} }
func (v Vec3i) Sub(other Vec3i) Vec3i { return Vec3i{v.X - other.X, v.Y - other.Y, v.Z - other.Z} }
func (v Vec3i) Mul(other Vec3i) Vec3i { return Vec3i{v.X * other.X, v.Y * other.Y, v.Z * other.Z} }
func (v Vec3i) Div(other Vec3i) Vec3i { return Vec3i{v.X / other.X, v.Y / other.Y, v.Z / other.Z} }
func (v Vec3i) AddS(values ...int) Vec3i {
	x, y, z := scalar3i("Vec3i.AddS", values)
	return Vec3i{v.X + x, v.Y + y, v.Z + z}
}
func (v Vec3i) SubS(values ...int) Vec3i {
	x, y, z := scalar3i("Vec3i.SubS", values)
	return Vec3i{v.X - x, v.Y - y, v.Z - z}
}
func (v Vec3i) MulS(values ...int) Vec3i {
	x, y, z := scalar3i("Vec3i.MulS", values)
	return Vec3i{v.X * x, v.Y * y, v.Z * z}
}
func (v Vec3i) DivS(values ...int) Vec3i {
	x, y, z := scalar3i("Vec3i.DivS", values)
	return Vec3i{v.X / x, v.Y / y, v.Z / z}
}
func (v Vec3i) Negate() Vec3i       { return Vec3i{-v.X, -v.Y, -v.Z} }
func (v Vec3i) Dot(other Vec3i) int { return v.X*other.X + v.Y*other.Y + v.Z*other.Z }
func (v Vec3i) Cross(other Vec3i) Vec3i {
	return Vec3i{v.Y*other.Z - v.Z*other.Y, v.Z*other.X - v.X*other.Z, v.X*other.Y - v.Y*other.X}
}
func (v Vec3i) LengthSquared() int           { return v.Dot(v) }
func (v Vec3i) Length() float32              { return sqrt(float32(v.LengthSquared())) }
func (v Vec3i) Distance(other Vec3i) float32 { return v.Sub(other).Length() }
func (v Vec3i) Float() Vec3                  { return Vec3{float32(v.X), float32(v.Y), float32(v.Z)} }

func (v Vec4i) Add(other Vec4i) Vec4i {
	return Vec4i{v.X + other.X, v.Y + other.Y, v.Z + other.Z, v.W + other.W}
}
func (v Vec4i) Sub(other Vec4i) Vec4i {
	return Vec4i{v.X - other.X, v.Y - other.Y, v.Z - other.Z, v.W - other.W}
}
func (v Vec4i) Mul(other Vec4i) Vec4i {
	return Vec4i{v.X * other.X, v.Y * other.Y, v.Z * other.Z, v.W * other.W}
}
func (v Vec4i) Div(other Vec4i) Vec4i {
	return Vec4i{v.X / other.X, v.Y / other.Y, v.Z / other.Z, v.W / other.W}
}
func (v Vec4i) AddS(values ...int) Vec4i {
	x, y, z, w := scalar4i("Vec4i.AddS", values)
	return Vec4i{v.X + x, v.Y + y, v.Z + z, v.W + w}
}
func (v Vec4i) SubS(values ...int) Vec4i {
	x, y, z, w := scalar4i("Vec4i.SubS", values)
	return Vec4i{v.X - x, v.Y - y, v.Z - z, v.W - w}
}
func (v Vec4i) MulS(values ...int) Vec4i {
	x, y, z, w := scalar4i("Vec4i.MulS", values)
	return Vec4i{v.X * x, v.Y * y, v.Z * z, v.W * w}
}
func (v Vec4i) DivS(values ...int) Vec4i {
	x, y, z, w := scalar4i("Vec4i.DivS", values)
	return Vec4i{v.X / x, v.Y / y, v.Z / z, v.W / w}
}
func (v Vec4i) Negate() Vec4i                { return Vec4i{-v.X, -v.Y, -v.Z, -v.W} }
func (v Vec4i) Dot(other Vec4i) int          { return v.X*other.X + v.Y*other.Y + v.Z*other.Z + v.W*other.W }
func (v Vec4i) LengthSquared() int           { return v.Dot(v) }
func (v Vec4i) Length() float32              { return sqrt(float32(v.LengthSquared())) }
func (v Vec4i) Distance(other Vec4i) float32 { return v.Sub(other).Length() }
func (v Vec4i) Float() Vec4                  { return Vec4{float32(v.X), float32(v.Y), float32(v.Z), float32(v.W)} }

func scalar2f(name string, values []float32) (float32, float32) {
	if len(values) == 1 {
		return values[0], values[0]
	}
	if len(values) == 2 {
		return values[0], values[1]
	}
	panic("m." + name + " expects one or two values")
}

func scalar3f(name string, values []float32) (float32, float32, float32) {
	if len(values) == 1 {
		return values[0], values[0], values[0]
	}
	if len(values) == 3 {
		return values[0], values[1], values[2]
	}
	panic("m." + name + " expects one or three values")
}

func scalar4f(name string, values []float32) (float32, float32, float32, float32) {
	if len(values) == 1 {
		return values[0], values[0], values[0], values[0]
	}
	if len(values) == 4 {
		return values[0], values[1], values[2], values[3]
	}
	panic("m." + name + " expects one or four values")
}

func scalar2i(name string, values []int) (int, int) {
	if len(values) == 1 {
		return values[0], values[0]
	}
	if len(values) == 2 {
		return values[0], values[1]
	}
	panic("m." + name + " expects one or two values")
}

func scalar3i(name string, values []int) (int, int, int) {
	if len(values) == 1 {
		return values[0], values[0], values[0]
	}
	if len(values) == 3 {
		return values[0], values[1], values[2]
	}
	panic("m." + name + " expects one or three values")
}

func scalar4i(name string, values []int) (int, int, int, int) {
	if len(values) == 1 {
		return values[0], values[0], values[0], values[0]
	}
	if len(values) == 4 {
		return values[0], values[1], values[2], values[3]
	}
	panic("m." + name + " expects one or four values")
}

func sqrt(value float32) float32  { return float32(math.Sqrt(float64(value))) }
func round(value float32) float32 { return float32(math.Round(float64(value))) }
func floor(value float32) float32 { return float32(math.Floor(float64(value))) }
func ceil(value float32) float32  { return float32(math.Ceil(float64(value))) }
