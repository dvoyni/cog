package gfx

import "github.com/dvoyni/cog/m"

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
)

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
