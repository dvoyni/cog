package internal

import (
	"encoding/binary"
	"math"

	"github.com/dvoyni/cog/libs/m"
)

// ParameterDescr is one declarative shader parameter: a texture, buffer, color,
// scalar, vector, matrix, or sampler. Build it with the *Param constructors and
// pass it to Material or OpQueue.Draw.
type ParameterDescr struct {
	name    string
	kind    ParamKind
	texture TextureDescr
	buffer  BufferDescr
	// bufferOffset and bufferSize bind a range of buffer. A zero size means the
	// whole buffer from the offset.
	bufferOffset int
	bufferSize   int
	color        m.Color
	num          float32
	vec          m.Vec4
	mat          m.Mat4
	sampler      SamplerDesc
	// raw is the byte copy a RawParameter carries, already laid out the way the
	// shader reads it. It is validated against WGSL's alignment rules once per
	// type at construction, so nothing downstream re-derives a layout from it.
	// It is static, for the reason BufferDescr.bytes gives.
	raw m.Blob
}

// ParamKind tags the variant stored in a ParameterDescr.
type ParamKind uint8

const (
	paramNone ParamKind = iota
	ParamTexture
	ParamColor
	ParamFloat
	ParamVec4
	ParamMat4
	ParamSampler
	ParamBuffer
	ParamRaw
)

// valueKind reports whether the parameter carries bytes a shader reads out of
// its uniform block, as opposed to a binding it attaches to a bind group.
func (k ParamKind) ValueKind() bool {
	switch k {
	case ParamColor, ParamFloat, ParamVec4, ParamMat4, ParamRaw:
		return true
	}
	return false
}

// String names the kind the way an error message wants it: what a caller wrote,
// not what the enum is called.
func (k ParamKind) String() string {
	switch k {
	case ParamTexture:
		return "texture"
	case ParamColor:
		return "color"
	case ParamFloat:
		return "float"
	case ParamVec4:
		return "vec4"
	case ParamMat4:
		return "mat4"
	case ParamSampler:
		return "sampler"
	case ParamBuffer:
		return "buffer"
	case ParamRaw:
		return "raw"
	}
	return "none"
}

// FloatParam creates a scalar parameter.
func FloatParam(name string, v float32) ParameterDescr {
	return ParameterDescr{name: name, kind: ParamFloat, num: v}
}

// VecParam creates a vec4 parameter.
func VecParam(name string, v m.Vec4) ParameterDescr {
	return ParameterDescr{name: name, kind: ParamVec4, vec: v}
}

// MatParam creates a 4x4 matrix parameter.
func MatParam(name string, m m.Mat4) ParameterDescr {
	return ParameterDescr{name: name, kind: ParamMat4, mat: m}
}

// ColorParam creates a color parameter.
func ColorParam(name string, c m.Color) ParameterDescr {
	return ParameterDescr{name: name, kind: ParamColor, color: c}
}

// TextureParam creates a texture parameter from a texture descriptor.
func TextureParam(name string, tex TextureDescr) ParameterDescr {
	return ParameterDescr{name: name, kind: ParamTexture, texture: tex}
}

// SamplerParam creates a sampler parameter. The zero SamplerDesc clamps and
// filters linearly.
func SamplerParam(name string, desc SamplerDesc) ParameterDescr {
	return ParameterDescr{name: name, kind: ParamSampler, sampler: desc}
}

// BufferParam creates a buffer parameter from a buffer descriptor, binding the
// whole buffer.
func BufferParam(name string, buf BufferDescr) ParameterDescr {
	return ParameterDescr{name: name, kind: ParamBuffer, buffer: buf}
}

// BufferRangeParam binds one slice of a buffer, which is how a draw addresses
// its own record in a shared arena: the binding is the addressing, so no index
// has to be agreed on across the record/translate thread boundary. offset must
// be a multiple of StorageAlignment.
func BufferRangeParam(name string, buf BufferDescr, offset, size int) ParameterDescr {
	return ParameterDescr{name: name, kind: ParamBuffer, buffer: buf, bufferOffset: offset, bufferSize: size}
}

// Name reports the parameter's declared shader name.
func (p ParameterDescr) Name() string { return p.name }

// ColorValue returns the parameter's color and true when it is a color parameter.
func (p ParameterDescr) ColorValue() (m.Color, bool) { return p.color, p.kind == ParamColor }

// FloatValue returns the parameter's scalar and true when it is a float parameter.
func (p ParameterDescr) FloatValue() (float32, bool) { return p.num, p.kind == ParamFloat }

// TextureValue returns the parameter's texture and true when it is a texture parameter.
func (p ParameterDescr) TextureValue() (TextureDescr, bool) { return p.texture, p.kind == ParamTexture }

// SamplerValue returns the parameter's sampler and true when it is a sampler parameter.
func (p ParameterDescr) SamplerValue() (SamplerDesc, bool) { return p.sampler, p.kind == ParamSampler }

// VecValue returns the parameter's vec4 and true when it is a vec4 parameter.
func (p ParameterDescr) VecValue() (m.Vec4, bool) { return p.vec, p.kind == ParamVec4 }

// MatValue returns the parameter's mat4 and true when it is a mat4 parameter.
func (p ParameterDescr) MatValue() (m.Mat4, bool) { return p.mat, p.kind == ParamMat4 }

// BufferValue returns the parameter's buffer and true when it is a buffer
// parameter. The range it binds is BufferRange, not part of the descriptor:
// two draws addressing their own slices of one arena share the buffer and
// differ only in the range.
func (p ParameterDescr) BufferValue() (BufferDescr, bool) {
	return p.buffer, p.kind == ParamBuffer
}

// BufferRange returns the slice of the buffer this parameter binds, and true
// when it is a buffer parameter. A zero size means the whole buffer from the
// offset.
func (p ParameterDescr) BufferRange() (offset, size int, ok bool) {
	return p.bufferOffset, p.bufferSize, p.kind == ParamBuffer
}

// RawLen returns the byte length of a raw parameter's data and true when it is
// one. The bytes themselves stay inside the descriptor: they are already laid
// out the way one shader reads them, so nothing outside can do anything with
// them but forward them, and a reader that only wants to know how much is
// travelling gets the number without the copy.
func (p ParameterDescr) RawLen() (int, bool) { return len(p.raw), p.kind == ParamRaw }

// HasValue reports whether the parameter carries bytes a shader reads out of its
// uniform block, as opposed to a binding it attaches to a bind group. It is the
// question a recorder asks to decide whether a parameter can vary per item at
// all: a value can, and a bind group cannot, because there is one per draw.
func (p ParameterDescr) HasValue() bool { return p.kind.ValueKind() }

// ValueSize reports how many bytes AppendValue would append, and zero for a
// binding. A recorder collecting one name's values across many items needs it to
// tell that the items agree on the element size: one name at two kinds would
// otherwise pack an array the shader strides through wrongly, which is a wrong
// picture with nothing reported.
func (p ParameterDescr) ValueSize() int {
	switch p.kind {
	case ParamColor, ParamVec4:
		return 16
	case ParamMat4:
		return 64
	case ParamFloat:
		return 4
	case ParamRaw:
		return len(p.raw)
	}
	return 0
}

// AppendValue appends the parameter's value to dst in the byte layout a shader
// reads it at - four bytes for a float, sixteen for a color or vec4, sixty-four
// for a mat4, and a raw parameter's own bytes - and reports whether it had a
// value at all.
//
// A texture, sampler or buffer parameter is a binding rather than a value, so it
// appends nothing and reports false. This is what lets a recorder pack the same
// parameter into an array without a type switch of its own, and the growth of
// dst is the value's size.
func (p ParameterDescr) AppendValue(dst []byte) ([]byte, bool) {
	var buf [64]byte
	switch p.kind {
	case ParamColor:
		WriteColor(buf[:16], p.color)
		return append(dst, buf[:16]...), true
	case ParamVec4:
		WriteVec4(buf[:16], p.vec)
		return append(dst, buf[:16]...), true
	case ParamMat4:
		WriteMat4(buf[:64], p.mat)
		return append(dst, buf[:64]...), true
	case ParamFloat:
		binary.LittleEndian.PutUint32(buf[:4], math.Float32bits(p.num))
		return append(dst, buf[:4]...), true
	case ParamRaw:
		return append(dst, p.raw...), true
	}
	return dst, false
}

// WriteMat4, WriteColor and WriteVec4 pack a value parameter at its reflected
// offset, in the little-endian layout WGSL reads.
func WriteMat4(dst []byte, m m.Mat4) {
	for i := 0; i < 16; i++ {
		binary.LittleEndian.PutUint32(dst[i*4:], math.Float32bits(m[i]))
	}
}

func WriteColor(dst []byte, c m.Color) {
	binary.LittleEndian.PutUint32(dst[0:], math.Float32bits(c.R))
	binary.LittleEndian.PutUint32(dst[4:], math.Float32bits(c.G))
	binary.LittleEndian.PutUint32(dst[8:], math.Float32bits(c.B))
	binary.LittleEndian.PutUint32(dst[12:], math.Float32bits(c.A))
}

func WriteVec4(dst []byte, v m.Vec4) {
	binary.LittleEndian.PutUint32(dst[0:], math.Float32bits(v.X))
	binary.LittleEndian.PutUint32(dst[4:], math.Float32bits(v.Y))
	binary.LittleEndian.PutUint32(dst[8:], math.Float32bits(v.Z))
	binary.LittleEndian.PutUint32(dst[12:], math.Float32bits(v.W))
}
