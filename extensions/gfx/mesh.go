package gfx

import "github.com/dvoyni/cog/extensions/gfx/internal"

// VertexType is the element type of one attribute in the interleaved vertex
// array: float, half-float, normalized, or integer scalar/vector types. Names
// mirror the WebGPU vertex formats.
type VertexType = internal.VertexType

const (
	UnknownVertexType = internal.UnknownVertexType
	Float32           = internal.Float32
	Float32x2         = internal.Float32x2
	Float32x3         = internal.Float32x3
	Float32x4         = internal.Float32x4
	Float16x2         = internal.Float16x2
	Float16x4         = internal.Float16x4
	Uint8x2           = internal.Uint8x2
	Uint8x4           = internal.Uint8x4
	Sint8x2           = internal.Sint8x2
	Sint8x4           = internal.Sint8x4
	Unorm8x2          = internal.Unorm8x2
	Unorm8x4          = internal.Unorm8x4
	Snorm8x2          = internal.Snorm8x2
	Snorm8x4          = internal.Snorm8x4
	Uint16x2          = internal.Uint16x2
	Uint16x4          = internal.Uint16x4
	Sint16x2          = internal.Sint16x2
	Sint16x4          = internal.Sint16x4
	Unorm16x2         = internal.Unorm16x2
	Unorm16x4         = internal.Unorm16x4
	Snorm16x2         = internal.Snorm16x2
	Snorm16x4         = internal.Snorm16x4
	Uint32            = internal.Uint32
	Uint32x2          = internal.Uint32x2
	Uint32x3          = internal.Uint32x3
	Uint32x4          = internal.Uint32x4
	Sint32            = internal.Sint32
	Sint32x2          = internal.Sint32x2
	Sint32x3          = internal.Sint32x3
	Sint32x4          = internal.Sint32x4
	Unorm1010102      = internal.Unorm1010102
)

// VertexScalar is the scalar type an attribute presents to the shader once the
// hardware has decoded it, which is not the same thing as the type its bytes
// are stored in: every normalized format arrives as float however many bits it
// occupies, and only the integer formats arrive as integers.
type VertexScalar = internal.VertexScalar

const (
	// VertexScalarNone is the zero value: a type that decodes to nothing a
	// shader can read. No legal vertex format has it.
	VertexScalarNone  = internal.VertexScalarNone
	VertexScalarFloat = internal.VertexScalarFloat
	VertexScalarUint  = internal.VertexScalarUint
	VertexScalarSint  = internal.VertexScalarSint
)

// IndexWidth is how wide one element of an index buffer is. There are exactly
// two, fixed by the platform rather than chosen: WebGPU has no uint8 index
// format, so geometry that was authored at a byte per index is stored at two.
//
// Nothing about it is a fidelity call - an index is exact or it is broken - so
// gfx neither derives nor validates the choice: it carries whatever width the
// caller declared its bytes to be in, and the only thing it can check is that
// the bytes divide by it.
//
// The zero value is IndexUint32, the width that is legal for any mesh, so a
// descriptor built without naming one is wide rather than wrong.
type IndexWidth = internal.IndexWidth

const (
	IndexUint32 = internal.IndexUint32
	IndexUint16 = internal.IndexUint16
)

// VertexAttr describes one attribute of the single interleaved vertex array: its
// byte offset and element type. Attributes bind to shader @location values in the
// order given. Build it with Attr.
type VertexAttr = internal.VertexAttr

// Attr describes a vertex attribute at byte offset with element type typ.
func Attr(offset int, typ VertexType) VertexAttr {
	return internal.Attr(offset, typ)
}

// MeshDescr is CPU-side geometry for one draw: a single interleaved vertex array
// (and an optional index array at one of the two index widths) as buffer
// descriptors, a primitive topology, and the vertex layout. Build it with Mesh
// or MeshIndexed; its fields are unexported and read by the translator.
type MeshDescr = internal.MeshDescr

// Mesh builds non-indexed geometry from an interleaved vertex buffer, a topology,
// and the vertex layout.
func Mesh(vertices BufferDescr, topology PrimitiveTopology, layout ...VertexAttr) MeshDescr {
	return internal.Mesh(vertices, topology, layout...)
}

// MeshIndexed builds indexed geometry from vertex and index buffers, the width
// one index of that buffer is written at, a topology, and the vertex layout. A
// zero index buffer (as passed by Mesh) yields a non-indexed mesh.
//
// The width describes the bytes rather than constraining them: gfx has no way
// to know how a caller wrote its indices, so declaring uint16 over uint32 bytes
// reads pairs of indices as one. What it can check - that the buffer's length
// divides by the width - it checks where the draw is translated, since this is
// a pure value constructor with no error return.
func MeshIndexed(
	vertices, indices BufferDescr, width IndexWidth,
	topology PrimitiveTopology, layout ...VertexAttr,
) MeshDescr {
	return internal.MeshIndexed(vertices, indices, width, topology, layout...)
}
