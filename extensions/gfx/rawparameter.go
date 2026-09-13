package gfx

import "github.com/dvoyni/cog/extensions/gfx/internal"

// RawParameter creates a parameter carrying an arbitrary plain-data struct by
// copying its bytes, so a shader value that is a record rather than a scalar or
// a vector can be named like any other parameter.
//
// T's memory layout is validated against WGSL's alignment rules once per type,
// and a mismatch panics. That check is the whole point of the constructor. Go
// aligns a struct to the widest alignment of its fields, which for float32 data
// is 4; WGSL aligns vec2 to 8 and vec3, vec4 and matrices to 16. So
//
//	type bad struct { A float32; B m.Vec3 }
//
// is 16 bytes in Go and 32 in WGSL: it passes any size-based check, and every
// element after the first reads the wrong memory with no error anywhere. The
// panic names the field, both offsets, and the padding that would fix it.
//
// Only plain-data members are accepted - float32, int32, uint32, m.Vec2, m.Vec3,
// m.Vec4, m.Color, m.Quat, m.Mat4, arrays of those, and structs of those. Any
// other member type panics, which is what keeps a pointer out of a byte copy.
func RawParameter[T any](name string, value T) ParameterDescr {
	return internal.RawParameter(name, value)
}
