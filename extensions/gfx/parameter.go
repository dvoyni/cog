package gfx

import (
	"github.com/dvoyni/cog/extensions/gfx/internal"
	"github.com/dvoyni/cog/libs/m"
)

// ParameterDescr is one declarative shader parameter: a texture, buffer, color,
// scalar, vector, matrix, or sampler. Build it with the *Param constructors and
// pass it to Material or OpQueue.Draw.
type ParameterDescr = internal.ParameterDescr

// FloatParam creates a scalar parameter.
func FloatParam(name string, v float32) ParameterDescr {
	return internal.FloatParam(name, v)
}

// VecParam creates a vec4 parameter.
func VecParam(name string, v m.Vec4) ParameterDescr {
	return internal.VecParam(name, v)
}

// MatParam creates a 4x4 matrix parameter.
func MatParam(name string, m m.Mat4) ParameterDescr {
	return internal.MatParam(name, m)
}

// ColorParam creates a color parameter.
func ColorParam(name string, c m.Color) ParameterDescr {
	return internal.ColorParam(name, c)
}

// TextureParam creates a texture parameter from a texture descriptor.
func TextureParam(name string, tex TextureDescr) ParameterDescr {
	return internal.TextureParam(name, tex)
}

// SamplerParam creates a sampler parameter. The zero SamplerDesc clamps and
// filters linearly.
func SamplerParam(name string, desc SamplerDesc) ParameterDescr {
	return internal.SamplerParam(name, desc)
}

// BufferParam creates a buffer parameter from a buffer descriptor, binding the
// whole buffer.
func BufferParam(name string, buf BufferDescr) ParameterDescr {
	return internal.BufferParam(name, buf)
}

// BufferRangeParam binds one slice of a buffer, which is how a draw addresses
// its own record in a shared arena: the binding is the addressing, so no index
// has to be agreed on across the record/translate thread boundary. offset must
// be a multiple of StorageAlignment.
func BufferRangeParam(name string, buf BufferDescr, offset, size int) ParameterDescr {
	return internal.BufferRangeParam(name, buf, offset, size)
}
