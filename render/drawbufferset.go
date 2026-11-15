package render

import "github.com/dvoyni/cog/gfx"

// drawBufferSet are the gfx buffers a draw binds: those behind its mesh's
// vertex buffers and then its instance buffers, the first vertexBufferCount
// of vertexBuffers, by slot, each at its offset, and the one behind its
// mesh's index buffer.
type drawBufferSet struct {
	vertexBuffers     [maxVertexBufferCount]gfx.Buffer
	vertexOffsets     [maxVertexBufferCount]uint64
	vertexBufferCount int
	indexBuffer       gfx.Buffer
}

// addVertexBuffer adds buffer, bound at offset, at the slot after s's last
// vertex buffer.
func (s *drawBufferSet) addVertexBuffer(buffer gfx.Buffer, offset uint64) {
	s.vertexBuffers[s.vertexBufferCount] = buffer
	s.vertexOffsets[s.vertexBufferCount] = offset
	s.vertexBufferCount++
}
