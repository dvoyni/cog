package gfx

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
// (and optional uint32 index array) as buffer descriptors, a primitive topology,
// and the vertex layout. Build it with Mesh or MeshIndexed; its fields are
// unexported and read by the translator.
type MeshDescr struct {
	vertices    BufferDescr
	indices     BufferDescr
	indexed     bool
	topology    PrimitiveTopology
	layout      []VertexAttr
	vertexCount int
	indexCount  int
}

// Mesh builds non-indexed geometry from an interleaved vertex buffer, a topology,
// and the vertex layout.
func Mesh(vertices BufferDescr, topology PrimitiveTopology, layout ...VertexAttr) MeshDescr {
	return MeshIndexed(vertices, BufferDescr{}, topology, layout...)
}

// MeshIndexed builds indexed geometry (uint32 indices) from vertex and index
// buffers, a topology, and the vertex layout. A zero index buffer (as passed by
// Mesh) yields a non-indexed mesh.
func MeshIndexed(vertices, indices BufferDescr, topology PrimitiveTopology, layout ...VertexAttr) MeshDescr {
	mesh := MeshDescr{
		vertices: vertices,
		indices:  indices,
		indexed:  indices.hasData(),
		topology: topology,
		layout:   layout,
	}
	if stride := mesh.stride(); stride > 0 {
		mesh.vertexCount = vertices.size / stride
	}
	mesh.indexCount = indices.size / 4
	return mesh
}

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
