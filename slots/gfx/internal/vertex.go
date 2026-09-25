package internal

import (
	"github.com/dvoyni/cog/slots/gfx/internal/shader"
)

// VertexType is the element type of one attribute in the interleaved vertex
// array: float, half-float, normalized, or integer scalar/vector types. Names
// mirror the WebGPU vertex formats.
type VertexType uint8

const (
	UnknownVertexType VertexType = iota
	Float32
	Float32x2
	Float32x3
	Float32x4
	Float16x2
	Float16x4
	Uint8x2
	Uint8x4
	Sint8x2
	Sint8x4
	Unorm8x2
	Unorm8x4
	Snorm8x2
	Snorm8x4
	Uint16x2
	Uint16x4
	Sint16x2
	Sint16x4
	Unorm16x2
	Unorm16x4
	Snorm16x2
	Snorm16x4
	Uint32
	Uint32x2
	Uint32x3
	Uint32x4
	Sint32
	Sint32x2
	Sint32x3
	Sint32x4
	Unorm1010102 // packed 10/10/10/2 normalized unsigned in one u32
)

// Decode reports the scalar kind and component count this format presents to
// the shader that reads it, and VertexScalarNone with zero for a type that is
// not a vertex format.
func (t VertexType) Decode() (shader.VertexScalar, int) {
	switch t {
	case Float32:
		return shader.VertexScalarFloat, 1
	case Float32x2, Float16x2, Unorm8x2, Snorm8x2, Unorm16x2, Snorm16x2:
		return shader.VertexScalarFloat, 2
	case Float32x3:
		return shader.VertexScalarFloat, 3
	case Float32x4, Float16x4, Unorm8x4, Snorm8x4, Unorm16x4, Snorm16x4, Unorm1010102:
		return shader.VertexScalarFloat, 4
	case Uint8x2, Uint16x2:
		return shader.VertexScalarUint, 2
	case Uint32:
		return shader.VertexScalarUint, 1
	case Uint32x2:
		return shader.VertexScalarUint, 2
	case Uint32x3:
		return shader.VertexScalarUint, 3
	case Uint8x4, Uint16x4, Uint32x4:
		return shader.VertexScalarUint, 4
	case Sint8x2, Sint16x2:
		return shader.VertexScalarSint, 2
	case Sint32:
		return shader.VertexScalarSint, 1
	case Sint32x2:
		return shader.VertexScalarSint, 2
	case Sint32x3:
		return shader.VertexScalarSint, 3
	case Sint8x4, Sint16x4, Sint32x4:
		return shader.VertexScalarSint, 4
	}
	return shader.VertexScalarNone, 0
}

// Size reports the byte size of one element of this type, and zero for a type
// that is not a vertex format.
func (t VertexType) Size() int {
	switch t {
	case Uint8x2, Sint8x2, Unorm8x2, Snorm8x2:
		return 2
	case Float32, Float16x2, Uint8x4, Sint8x4, Unorm8x4, Snorm8x4,
		Uint16x2, Sint16x2, Unorm16x2, Snorm16x2, Uint32, Sint32, Unorm1010102:
		return 4
	case Float32x2, Float16x4, Uint16x4, Sint16x4, Unorm16x4, Snorm16x4, Uint32x2, Sint32x2:
		return 8
	case Float32x3, Uint32x3, Sint32x3:
		return 12
	case Float32x4, Uint32x4, Sint32x4:
		return 16
	}
	return 0
}

// VertexAttribute is one attribute of the interleaved vertex buffer supplied to a
// pipeline: its byte offset, element type, and shader @location.
type VertexAttribute struct {
	Offset   int
	Type     VertexType
	Location int
}
