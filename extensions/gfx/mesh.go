package gfx

import "strconv"

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
	vertexTypeCount
)

const (
	maxVertexAttributes = 16
	maxVertexStride     = 2048
	vertexTypeBits      = 5
)

type vertexLayoutKey [maxVertexAttributes]uint16

// VertexScalar is the scalar type an attribute presents to the shader once the
// hardware has decoded it, which is not the same thing as the type its bytes
// are stored in: every normalized format arrives as float however many bits it
// occupies, and only the integer formats arrive as integers.
type VertexScalar uint8

const (
	// VertexScalarNone is the zero value: a type that decodes to nothing a
	// shader can read. No legal vertex format has it.
	VertexScalarNone VertexScalar = iota
	VertexScalarFloat
	VertexScalarUint
	VertexScalarSint
)

// wgsl renders the type a shader would declare for this kind at this many
// components, which is the spelling an author has to change to fix a mismatch.
func (s VertexScalar) wgsl(count int) string {
	scalar := "?"
	switch s {
	case VertexScalarFloat:
		scalar = "f32"
	case VertexScalarUint:
		scalar = "u32"
	case VertexScalarSint:
		scalar = "i32"
	}
	if count <= 1 {
		return scalar
	}
	return "vec" + strconv.Itoa(count) + "<" + scalar + ">"
}

// decode reports the scalar kind and component count this format presents to
// the shader that reads it.
func (t VertexType) decode() (VertexScalar, int) {
	switch t {
	case Float32:
		return VertexScalarFloat, 1
	case Float32x2, Float16x2, Unorm8x2, Snorm8x2, Unorm16x2, Snorm16x2:
		return VertexScalarFloat, 2
	case Float32x3:
		return VertexScalarFloat, 3
	case Float32x4, Float16x4, Unorm8x4, Snorm8x4, Unorm16x4, Snorm16x4, Unorm1010102:
		return VertexScalarFloat, 4
	case Uint8x2, Uint16x2:
		return VertexScalarUint, 2
	case Uint32:
		return VertexScalarUint, 1
	case Uint32x2:
		return VertexScalarUint, 2
	case Uint32x3:
		return VertexScalarUint, 3
	case Uint8x4, Uint16x4, Uint32x4:
		return VertexScalarUint, 4
	case Sint8x2, Sint16x2:
		return VertexScalarSint, 2
	case Sint32:
		return VertexScalarSint, 1
	case Sint32x2:
		return VertexScalarSint, 2
	case Sint32x3:
		return VertexScalarSint, 3
	case Sint8x4, Sint16x4, Sint32x4:
		return VertexScalarSint, 4
	}
	return VertexScalarNone, 0
}

// size reports the byte size of a vertex attribute element type.
func (t VertexType) size() int {
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
type IndexWidth uint8

const (
	IndexUint32 IndexWidth = iota
	IndexUint16
)

// Bytes reports how many bytes one index of this width occupies.
func (w IndexWidth) Bytes() int {
	if w == IndexUint16 {
		return 2
	}
	return 4
}

func (w IndexWidth) String() string {
	if w == IndexUint16 {
		return "uint16"
	}
	return "uint32"
}

// VertexAttr describes one attribute of the single interleaved vertex array: its
// byte offset and element type. Attributes bind to shader @location values in the
// order given. Build it with Attr.
type VertexAttr struct {
	offset int
	typ    VertexType
}

// Attr describes a vertex attribute at byte offset with element type typ.
func Attr(offset int, typ VertexType) VertexAttr { return VertexAttr{offset: offset, typ: typ} }

func vertexLayoutKeyOf(layout []VertexAttr) (vertexLayoutKey, bool) {
	var key vertexLayoutKey
	if len(layout) > len(key) {
		return key, false
	}
	for i := range layout {
		attr := layout[i]
		size := attr.typ.size()
		if attr.offset < 0 || attr.offset+size > maxVertexStride ||
			attr.typ <= UnknownVertexType || attr.typ >= vertexTypeCount {
			return vertexLayoutKey{}, false
		}
		key[i] = uint16(attr.offset)<<vertexTypeBits | uint16(attr.typ)
	}
	return key, true
}

// MeshDescr is CPU-side geometry for one draw: a single interleaved vertex array
// (and an optional index array at one of the two index widths) as buffer
// descriptors, a primitive topology, and the vertex layout. Build it with Mesh
// or MeshIndexed; its fields are unexported and read by the translator.
type MeshDescr struct {
	vertices    BufferDescr
	indices     BufferDescr
	indexWidth  IndexWidth
	indexed     bool
	topology    PrimitiveTopology
	layout      []VertexAttr
	vertexCount int
	indexCount  int
}

// Mesh builds non-indexed geometry from an interleaved vertex buffer, a topology,
// and the vertex layout.
func Mesh(vertices BufferDescr, topology PrimitiveTopology, layout ...VertexAttr) MeshDescr {
	return MeshIndexed(vertices, BufferDescr{}, IndexUint32, topology, layout...)
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
	mesh := MeshDescr{
		vertices:   vertices,
		indices:    indices,
		indexWidth: width,
		indexed:    indices.hasData(),
		topology:   topology,
		layout:     layout,
	}
	if stride := mesh.stride(); stride > 0 {
		mesh.vertexCount = vertices.size / stride
	}
	mesh.indexCount = indices.size / width.Bytes()
	return mesh
}

// VertexCount reports how many vertices the mesh's vertex buffer holds,
// derived from its size and the stride the layout implies. It is zero for a
// mesh whose layout declares no attributes, since nothing then says how wide
// a vertex is.
func (m MeshDescr) VertexCount() int { return m.vertexCount }

// IndexCount reports how many indices the mesh's index buffer holds at the
// width it was declared at, and zero for a non-indexed mesh.
func (m MeshDescr) IndexCount() int { return m.indexCount }

// IndexWidth reports how wide one of those indices is.
func (m MeshDescr) IndexWidth() IndexWidth { return m.indexWidth }

// Indexed reports whether the mesh draws through an index buffer.
func (m MeshDescr) Indexed() bool { return m.indexed }

// Topology reports how the mesh's vertices assemble into primitives.
func (m MeshDescr) Topology() PrimitiveTopology { return m.topology }

// stride reports the interleaved vertex stride derived from the layout (the
// largest attribute end offset).
func (m *MeshDescr) stride() int {
	s := 0
	for i := range m.layout {
		if e := m.layout[i].offset + m.layout[i].typ.size(); e > s {
			s = e
		}
	}
	return s
}
