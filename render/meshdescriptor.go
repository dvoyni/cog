package render

import (
	"cmp"
	"fmt"

	"github.com/dvoyni/cog/gfx"
)

// MeshDescriptor is what a mesh is made of: its vertex buffers, each a range
// of a buffer with its layout, read per vertex at slots from 0 in their
// order; an index buffer, a zero Buffer meaning none; and its topology. Its
// label names it in reports. A mesh owns none of the buffers it names.
type MeshDescriptor struct {
	Label         string
	VertexBuffers []VertexBuffer
	Indices       IndexBuffer
	Topology      PrimitiveTopology
}

// BufferRange is part of a buffer: Size bytes from Offset, a zero Size
// meaning the rest of the buffer.
type BufferRange struct {
	Buffer       Buffer
	Offset, Size uint64
}

// findBuffer answers the gfx buffer behind r's buffer in table and r's size,
// a zero Size the rest of the buffer; an error when the buffer failed, was
// freed or has no bytes, or r ends past it.
func (r BufferRange) findBuffer(table *assetTable) (gfx.Buffer, uint64, error) {
	buffer, bufferSize, ok := table.findBuffer(r.Buffer)
	if !ok {
		return gfx.Buffer{}, 0, fmt.Errorf("names buffer %v, which failed, was freed or has no bytes", r.Buffer)
	}
	size, err := r.findSize(bufferSize)
	return buffer, size, err
}

// findSize answers r's size in a buffer of bufferSize bytes, a zero Size the
// rest of the buffer; an error when r ends past it.
func (r BufferRange) findSize(bufferSize uint64) (uint64, error) {
	if r.Offset > bufferSize || r.Size > bufferSize-r.Offset {
		return 0, fmt.Errorf("is %d bytes at offset %d of buffer %v, of %d bytes, past its end", r.Size, r.Offset, r.Buffer, bufferSize)
	}
	return cmp.Or(r.Size, bufferSize-r.Offset), nil
}

// VertexLayout is how a vertex buffer's vertices are laid out: Stride bytes
// apart, a multiple of 4, each holding Attributes.
type VertexLayout struct {
	Stride     uint64
	Attributes []VertexAttribute
}

// VertexAttribute is one value each vertex holds: in Format, Offset bytes
// into the vertex, a multiple of 4, or of 2 for a format of two 8-bit
// components, fed to the vertex shader's input at Location.
type VertexAttribute struct {
	Format   VertexFormat
	Offset   uint64
	Location uint32
}

// IndexBuffer is a mesh's index buffer: a range of a buffer, its offset
// where the mesh's first index is, holding indices of Format.
type IndexBuffer struct {
	Range  BufferRange
	Format IndexFormat
}

// IndexFormat is the type of a mesh's indices, each gfx's of the same name.
type IndexFormat uint32

const (
	IndexFormatUint16 = IndexFormat(gfx.IndexFormatUint16)
	IndexFormatUint32 = IndexFormat(gfx.IndexFormatUint32)
)

// size answers how many bytes an index of format f takes; zero for no
// format.
func (f IndexFormat) size() uint64 {
	switch f {
	case IndexFormatUint16:
		return 2
	case IndexFormatUint32:
		return 4
	}
	return 0
}

// PrimitiveTopology is how a mesh's vertices are assembled into primitives,
// each gfx's of the same name. Its zero value is a triangle list.
type PrimitiveTopology uint32

const (
	PrimitiveTopologyTriangleList  = PrimitiveTopology(gfx.PrimitiveTopologyTriangleList)
	PrimitiveTopologyPointList     = PrimitiveTopology(gfx.PrimitiveTopologyPointList)
	PrimitiveTopologyLineList      = PrimitiveTopology(gfx.PrimitiveTopologyLineList)
	PrimitiveTopologyLineStrip     = PrimitiveTopology(gfx.PrimitiveTopologyLineStrip)
	PrimitiveTopologyTriangleStrip = PrimitiveTopology(gfx.PrimitiveTopologyTriangleStrip)
)
