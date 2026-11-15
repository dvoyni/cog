package render

import (
	"fmt"

	"github.com/dvoyni/cog/gfx"
)

// maxVertexBufferCount is how many vertex buffers a mesh may have by WebGPU's
// default limits.
const maxVertexBufferCount = 8

// meshSlot is the slot of one mesh: what it was made of, each vertex buffer
// kept as its range and stride, its layouts by their number in the vertex
// layout interner, and whether a draw of it was skipped and reported.
type meshSlot struct {
	label         string
	vertexBuffers []meshVertexBuffer
	indices       IndexBuffer
	topology      PrimitiveTopology
	vertexLayout  uint32
	reported      bool
}

// findElements answers how many vertices and indices the mesh of s draws,
// as its buffers in table now hold them: the fewest vertices the range of a
// vertex buffer strided holds, and the indices its index range holds, none
// with no index buffer; and fills buffers with the gfx buffers behind them.
// A buffer that failed, was freed or has no bytes, or whose range ends past
// it, answers an error naming it.
func (s meshSlot) findElements(table *assetTable, buffers *meshBinding) (vertexCount, indexCount uint32, err error) {
	vertexCount = ^uint32(0)
	for index, vertexBuffer := range s.vertexBuffers {
		var size uint64
		if buffers.vertexBuffers[index], size, err = vertexBuffer.bufferRange.findBuffer(table); err != nil {
			return 0, 0, fmt.Errorf("vertex buffer %d %w", index, err)
		}
		if vertexBuffer.stride > 0 {
			vertexCount = min(vertexCount, uint32(min(size/vertexBuffer.stride, uint64(^uint32(0)))))
		}
	}
	if vertexCount == ^uint32(0) {
		vertexCount = 0
	}
	if s.indices.Range.Buffer == (Buffer{}) {
		return vertexCount, 0, nil
	}
	var size uint64
	if buffers.indexBuffer, size, err = s.indices.Range.findBuffer(table); err != nil {
		return 0, 0, fmt.Errorf("its index buffer %w", err)
	}
	return vertexCount, uint32(min(size/s.indices.Format.size(), uint64(^uint32(0)))), nil
}

// meshVertexBuffer is one vertex buffer of a mesh: its range, and the stride
// of its vertices.
type meshVertexBuffer struct {
	bufferRange BufferRange
	stride      uint64
}

// meshBinding are the gfx buffers a draw of a mesh binds: those behind its
// vertex buffers, by slot, and the one behind its index buffer.
type meshBinding struct {
	vertexBuffers [maxVertexBufferCount]gfx.Buffer
	indexBuffer   gfx.Buffer
}
