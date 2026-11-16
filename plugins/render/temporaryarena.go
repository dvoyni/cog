package render

import (
	"fmt"
	"slices"
	"sync"

	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/render/internal/handle"
)

// temporaryAlignment is the alignment of every block of the temporary arena:
// 256 bytes, the largest offset alignment WebGPU lets a device ask of a
// uniform block or a storage buffer, so a block binds at a dynamic offset on
// any device.
const temporaryAlignment = 256

// temporaryChunkUsage is the usage of every arena chunk: what a temporary
// buffer may be given to, a mesh's vertices and indices, a uniform block, a
// storage buffer and a copy's source.
const temporaryChunkUsage = BufferUsageFlagVertex | BufferUsageFlagIndex | BufferUsageFlagUniform | BufferUsageFlagStorage | BufferUsageFlagCopySrc

// temporaryArena is the pool of chunks that temporary buffers, the uniform
// blocks of pass and draw parameters, and those of material versions, are
// blocks of. Each chunk is held by one command list from its first block
// until the frame ends, and freed once it has gone unused for as many frames
// as render is configured to wait.
type temporaryArena struct {
	// mutex guards pooledChunks, every chunk the arena has made and not
	// freed. It is taken under the asset table's, so freedBuffers, endFrame's
	// own, holds the buffers it frees until it has let go of mutex.
	mutex        sync.Mutex
	pooledChunks []*arenaChunk
	freedBuffers []Buffer
}

// arenaChunk is one chunk of the arena: whether a list holds it this frame;
// its buffer in the asset table and the gfx buffer behind it; its bytes as
// its list fills them, which go to gfx at the list's end; where its last
// block ends; and the order of the list that last took it, with how many
// chunks that list had taken before it.
type arenaChunk struct {
	poolHold
	buffer      Buffer
	gfxBuffer   gfx.Buffer
	bytes       []byte
	end         uint64
	order       Order
	chunkNumber int
}

// acquireChunk hands out a chunk of size bytes, a multiple of 4, that no
// list holds this frame, for the list at order that has taken chunkNumber
// chunks before it this frame, making one in table when the pool has none.
// It hands out first the chunk that list took at that number the frame
// before, so a list recording the same blocks frame after frame places
// them in the same chunks, and the bind groups made over those chunks are
// bound again rather than made anew after going unused. table's mutex is
// held. The error is gfx's, when it cannot be made.
func (a *temporaryArena) acquireChunk(size uint64, order Order, chunkNumber int, table *assetTable, device gfx.Device) (*arenaChunk, error) {
	if size%4 != 0 {
		panic(fmt.Sprintf("render: acquiring a chunk of %d bytes, no multiple of 4", size))
	}
	table.requireLocked("acquireChunk")
	a.mutex.Lock()
	defer a.mutex.Unlock()
	var free *arenaChunk
	for _, chunk := range a.pooledChunks {
		if chunk.held || uint64(len(chunk.bytes)) != size {
			continue
		}
		if chunk.order == order && chunk.chunkNumber == chunkNumber {
			free = chunk
			break
		}
		if free == nil {
			free = chunk
		}
	}
	if free == nil {
		buffer, gfxBuffer, err := table.insertBuffer(device, handle.TemporaryChunkLabel, size, temporaryChunkUsage)
		if err != nil {
			return nil, err
		}
		free = &arenaChunk{buffer: buffer, gfxBuffer: gfxBuffer, bytes: make([]byte, size)}
		a.pooledChunks = append(a.pooledChunks, free)
	}
	free.held = true
	free.order = order
	free.chunkNumber = chunkNumber
	return free, nil
}

// findBytes answers a copy of bufferRange's bytes, of a chunk of a's, as its
// list placed them; false for a range of no chunk's.
func (a *temporaryArena) findBytes(bufferRange BufferRange) ([]byte, bool) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	for _, chunk := range a.pooledChunks {
		if chunk.buffer == bufferRange.Buffer {
			return slices.Clone(chunk.bytes[bufferRange.Offset : bufferRange.Offset+bufferRange.Size]), true
		}
	}
	return nil, false
}

// endFrame takes back every chunk held this frame, and removes from table
// those that have now gone unused for idleFrames frames. A chunk taken again
// is filled from its start.
func (a *temporaryArena) endFrame(idleFrames int, table *assetTable, device gfx.Device) {
	a.mutex.Lock()
	keptChunks := a.pooledChunks[:0]
	for _, chunk := range a.pooledChunks {
		if chunk.endFrame(idleFrames) {
			a.freedBuffers = append(a.freedBuffers, chunk.buffer)
			continue
		}
		keptChunks = append(keptChunks, chunk)
	}
	clear(a.pooledChunks[len(keptChunks):])
	a.pooledChunks = keptChunks
	a.mutex.Unlock()
	for _, buffer := range a.freedBuffers {
		table.removeBuffer(device, buffer)
	}
	a.freedBuffers = a.freedBuffers[:0]
}
