package types

// MeshDescr is CPU-side geometry for one draw: a single interleaved vertex
// array (and an optional index array at one of the two index widths) as buffer
// descriptors, a primitive topology, and the vertex layout.
// MeshDescrWithVertices and MeshDescrWithIndices build one with the stride
// and counts derived; a descriptor written by hand must fill them itself.
type MeshDescr struct {
	Vertices BufferDescr
	Indices  BufferDescr
	// IndexWidth is how wide one of the indices is.
	IndexWidth IndexWidth
	Topology   PrimitiveTopology
	Layout     []VertexAttribute
	// Stride is the interleaved vertex stride the layout implies: the largest
	// attribute end offset.
	Stride int
	// VertexCount is how many vertices the vertex buffer holds, derived from
	// its size and the stride the layout implies. It is zero for a mesh whose
	// layout declares no attributes, since nothing then says how wide a
	// vertex is.
	VertexCount int
	// IndexCount is how many indices the index buffer holds at IndexWidth.
	// The mesh draws through its index buffer exactly when it is above zero.
	IndexCount int
}

// MeshDescrWithVertices builds non-indexed geometry from an interleaved vertex
// buffer, a topology, and the vertex layout.
func MeshDescrWithVertices(vertices BufferDescr, topology PrimitiveTopology, layout ...VertexAttribute) MeshDescr {
	return MeshDescrWithIndices(vertices, BufferDescr{}, IndexUint32, topology, layout...)
}

// MeshDescrWithIndices builds indexed geometry from vertex and index buffers,
// the width one index of that buffer is written at, a topology, and the vertex
// layout. A zero index buffer (as passed by MeshDescrWithVertices) yields a
// non-indexed mesh.
//
// The width describes the bytes rather than constraining them: gfx has no way
// to know how a caller wrote its indices, so declaring uint16 over uint32 bytes
// reads pairs of indices as one. What it can check - that the buffer's length
// divides by the width - it checks where the draw is translated, since this is
// a pure value constructor with no error return.
func MeshDescrWithIndices(
	vertices, indices BufferDescr, width IndexWidth,
	topology PrimitiveTopology, layout ...VertexAttribute,
) MeshDescr {
	mesh := MeshDescr{
		Vertices:   vertices,
		Indices:    indices,
		IndexWidth: width,
		Topology:   topology,
		Layout:     layout,
	}
	for i := range layout {
		mesh.Stride = max(mesh.Stride, layout[i].Offset+layout[i].Type.Size())
	}
	if mesh.Stride > 0 {
		mesh.VertexCount = vertices.Size / mesh.Stride
	}
	mesh.IndexCount = indices.Size / width.Bytes()
	return mesh
}
