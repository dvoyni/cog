package gfx

import (
	"encoding/binary"
	"math"

	"github.com/dvoyni/cog/m"
)

// ParameterDescr is one declarative shader parameter: a texture, buffer, color,
// scalar, vector, matrix, or sampler. Build it with the *Param constructors and
// pass it to Material or OpQueue.Draw.
type ParameterDescr struct {
	name    string
	kind    paramKind
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
	raw []byte
}

// paramKind tags the variant stored in a ParameterDescr.
type paramKind uint8

const (
	paramNone paramKind = iota
	paramTexture
	paramColor
	paramFloat
	paramVec4
	paramMat4
	paramSampler
	paramBuffer
	paramRaw
)

// valueKind reports whether the parameter carries bytes a shader reads out of
// its uniform block, as opposed to a binding it attaches to a bind group.
func (k paramKind) valueKind() bool {
	switch k {
	case paramColor, paramFloat, paramVec4, paramMat4, paramRaw:
		return true
	}
	return false
}

// String names the kind the way an error message wants it: what a caller wrote,
// not what the enum is called.
func (k paramKind) String() string {
	switch k {
	case paramTexture:
		return "texture"
	case paramColor:
		return "color"
	case paramFloat:
		return "float"
	case paramVec4:
		return "vec4"
	case paramMat4:
		return "mat4"
	case paramSampler:
		return "sampler"
	case paramBuffer:
		return "buffer"
	case paramRaw:
		return "raw"
	}
	return "none"
}

// FloatParam creates a scalar parameter.
func FloatParam(name string, v float32) ParameterDescr {
	return ParameterDescr{name: name, kind: paramFloat, num: v}
}

// VecParam creates a vec4 parameter.
func VecParam(name string, v m.Vec4) ParameterDescr {
	return ParameterDescr{name: name, kind: paramVec4, vec: v}
}

// MatParam creates a 4x4 matrix parameter.
func MatParam(name string, m m.Mat4) ParameterDescr {
	return ParameterDescr{name: name, kind: paramMat4, mat: m}
}

// ColorParam creates a color parameter.
func ColorParam(name string, c m.Color) ParameterDescr {
	return ParameterDescr{name: name, kind: paramColor, color: c}
}

// TextureParam creates a texture parameter from a texture descriptor.
func TextureParam(name string, tex TextureDescr) ParameterDescr {
	return ParameterDescr{name: name, kind: paramTexture, texture: tex}
}

// SamplerParam creates a sampler parameter. The zero SamplerDesc clamps and
// filters linearly.
func SamplerParam(name string, desc SamplerDesc) ParameterDescr {
	return ParameterDescr{name: name, kind: paramSampler, sampler: desc}
}

// BufferParam creates a buffer parameter from a buffer descriptor, binding the
// whole buffer.
func BufferParam(name string, buf BufferDescr) ParameterDescr {
	return ParameterDescr{name: name, kind: paramBuffer, buffer: buf}
}

// BufferRangeParam binds one slice of a buffer, which is how a draw addresses
// its own record in a shared arena: the binding is the addressing, so no index
// has to be agreed on across the record/translate thread boundary. offset must
// be a multiple of StorageAlignment.
func BufferRangeParam(name string, buf BufferDescr, offset, size int) ParameterDescr {
	return ParameterDescr{name: name, kind: paramBuffer, buffer: buf, bufferOffset: offset, bufferSize: size}
}

// Name reports the parameter's declared shader name.
func (p ParameterDescr) Name() string { return p.name }

// ColorValue returns the parameter's color and true when it is a color parameter.
func (p ParameterDescr) ColorValue() (m.Color, bool) { return p.color, p.kind == paramColor }

// FloatValue returns the parameter's scalar and true when it is a float parameter.
func (p ParameterDescr) FloatValue() (float32, bool) { return p.num, p.kind == paramFloat }

// TextureValue returns the parameter's texture and true when it is a texture parameter.
func (p ParameterDescr) TextureValue() (TextureDescr, bool) { return p.texture, p.kind == paramTexture }

// SamplerValue returns the parameter's sampler and true when it is a sampler parameter.
func (p ParameterDescr) SamplerValue() (SamplerDesc, bool) { return p.sampler, p.kind == paramSampler }

// VecValue returns the parameter's vec4 and true when it is a vec4 parameter.
func (p ParameterDescr) VecValue() (m.Vec4, bool) { return p.vec, p.kind == paramVec4 }

// HasValue reports whether the parameter carries bytes a shader reads out of its
// uniform block, as opposed to a binding it attaches to a bind group. It is the
// question a recorder asks to decide whether a parameter can vary per item at
// all: a value can, and a bind group cannot, because there is one per draw.
func (p ParameterDescr) HasValue() bool { return p.kind.valueKind() }

// ValueSize reports how many bytes AppendValue would append, and zero for a
// binding. A recorder collecting one name's values across many items needs it to
// tell that the items agree on the element size: one name at two kinds would
// otherwise pack an array the shader strides through wrongly, which is a wrong
// picture with nothing reported.
func (p ParameterDescr) ValueSize() int {
	switch p.kind {
	case paramColor, paramVec4:
		return 16
	case paramMat4:
		return 64
	case paramFloat:
		return 4
	case paramRaw:
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
	case paramColor:
		writeColor(buf[:16], p.color)
		return append(dst, buf[:16]...), true
	case paramVec4:
		writeVec4(buf[:16], p.vec)
		return append(dst, buf[:16]...), true
	case paramMat4:
		writeMat4(buf[:64], p.mat)
		return append(dst, buf[:64]...), true
	case paramFloat:
		binary.LittleEndian.PutUint32(buf[:4], math.Float32bits(p.num))
		return append(dst, buf[:4]...), true
	case paramRaw:
		return append(dst, p.raw...), true
	}
	return dst, false
}
