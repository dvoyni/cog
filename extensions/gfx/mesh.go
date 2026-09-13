package gfx

import (
	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/dvoyni/cog/extensions/gfx/internal"
)

// VertexAttr describes one attribute of the single interleaved vertex array: its
// byte offset and element type. Attributes bind to shader @location values in the
// order given. Build it with Attr.
type VertexAttr = internal.VertexAttr

// Attr describes a vertex attribute at byte offset with element type typ.
func Attr(offset int, typ gpu.VertexType) VertexAttr {
	return internal.Attr(offset, typ)
}

// MeshDescr is CPU-side geometry for one draw: a single interleaved vertex array
// (and an optional index array at one of the two index widths) as buffer
// descriptors, a primitive topology, and the vertex layout. Build it with Mesh
// or MeshIndexed; its fields are unexported and read by the translator.
type MeshDescr = internal.MeshDescr

// Mesh builds non-indexed geometry from an interleaved vertex buffer, a topology,
// and the vertex layout.
func Mesh(vertices BufferDescr, topology gpu.PrimitiveTopology, layout ...VertexAttr) MeshDescr {
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
	vertices, indices BufferDescr, width gpu.IndexWidth,
	topology gpu.PrimitiveTopology, layout ...VertexAttr,
) MeshDescr {
	return internal.MeshIndexed(vertices, indices, width, topology, layout...)
}
