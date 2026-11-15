package render

import (
	"fmt"
	"sync"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/render/internal/handle"
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

// temporaryArena is the pool of chunks that temporary buffers, and the
// uniform blocks of pass and draw parameters, are blocks of. Each chunk is
// held by one command list from its first block until the frame ends, and
// freed once it has gone unused for as many frames as render is configured
// to wait.
type temporaryArena struct {
	// mutex guards pooledChunks, every chunk the arena has made and not freed.
	mutex        sync.Mutex
	pooledChunks []*arenaChunk
}

// arenaChunk is one chunk of the arena: whether a list holds it this frame;
// its buffer in the asset table and the gfx buffer behind it; its bytes as
// its list fills them, which go to gfx at the list's end; and where its last
// block ends.
type arenaChunk struct {
	poolHold
	buffer    Buffer
	gfxBuffer gfx.Buffer
	bytes     []byte
	end       uint64
}

// acquireChunk hands out a chunk of size bytes, a multiple of 4, that no
// list holds this frame, making one in table when the pool has none. The
// error is gfx's, when it cannot be made.
func (a *temporaryArena) acquireChunk(size uint64, table *assetTable, device gfx.Device) (*arenaChunk, error) {
	if size%4 != 0 {
		panic(fmt.Sprintf("render: acquiring a chunk of %d bytes, no multiple of 4", size))
	}
	a.mutex.Lock()
	defer a.mutex.Unlock()
	for _, chunk := range a.pooledChunks {
		if !chunk.held && uint64(len(chunk.bytes)) == size {
			chunk.held = true
			return chunk, nil
		}
	}
	buffer, gfxBuffer, err := table.addBuffer(device, handle.TemporaryChunkLabel, size, temporaryChunkUsage)
	if err != nil {
		return nil, err
	}
	chunk := &arenaChunk{poolHold: poolHold{held: true}, buffer: buffer, gfxBuffer: gfxBuffer, bytes: make([]byte, size)}
	a.pooledChunks = append(a.pooledChunks, chunk)
	return chunk, nil
}

// endFrame takes back every chunk held this frame, and removes from table
// those that have now gone unused for idleFrames frames. A chunk taken again
// is filled from its start.
func (a *temporaryArena) endFrame(idleFrames int, table *assetTable, device gfx.Device) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	keptChunks := a.pooledChunks[:0]
	for _, chunk := range a.pooledChunks {
		if chunk.endFrame(idleFrames) {
			table.removeBuffer(device, chunk.buffer)
			continue
		}
		keptChunks = append(keptChunks, chunk)
	}
	clear(a.pooledChunks[len(keptChunks):])
	a.pooledChunks = keptChunks
}
