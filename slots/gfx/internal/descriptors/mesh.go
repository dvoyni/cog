package descriptors

import "github.com/dvoyni/cog/slots/gfx/internal/types"

// VertexTypeCount is one past the last vertex format, the bound a layout key
// is checked against.
const VertexTypeCount = Unorm1010102 + 1

const (
	MaxVertexAttributes = 16
	MaxVertexStride     = 2048
	vertexTypeBits      = 5
)

type VertexLayoutKey [MaxVertexAttributes]uint16

// VertexAttr describes one attribute of the single interleaved vertex array: its
// byte offset and element type. Attributes bind to shader @location values in the
// order given. Build it with Attr.
type VertexAttr struct {
	offset int
	typ    VertexType
}

// Attr describes a vertex attribute at byte offset with element type typ.
func Attr(offset int, typ VertexType) VertexAttr { return VertexAttr{offset: offset, typ: typ} }

func VertexLayoutKeyOf(layout []VertexAttr) (VertexLayoutKey, bool) {
	var key VertexLayoutKey
	if len(layout) > len(key) {
		return key, false
	}
	for i := range layout {
		attr := layout[i]
		size := attr.typ.Size()
		if attr.offset < 0 || attr.offset+size > MaxVertexStride ||
			attr.typ <= UnknownVertexType || attr.typ >= VertexTypeCount {
			return VertexLayoutKey{}, false
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
	topology    types.PrimitiveTopology
	layout      []VertexAttr
	vertexCount int
	indexCount  int
}

// Mesh builds non-indexed geometry from an interleaved vertex buffer, a topology,
// and the vertex layout.
func Mesh(vertices BufferDescr, topology types.PrimitiveTopology, layout ...VertexAttr) MeshDescr {
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
	topology types.PrimitiveTopology, layout ...VertexAttr,
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
func (m MeshDescr) Topology() types.PrimitiveTopology { return m.topology }

// stride reports the interleaved vertex stride derived from the layout (the
// largest attribute end offset).
func (m *MeshDescr) stride() int {
	s := 0
	for i := range m.layout {
		if e := m.layout[i].offset + m.layout[i].typ.Size(); e > s {
			s = e
		}
	}
	return s
}
