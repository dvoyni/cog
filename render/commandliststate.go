package render

import (
	"fmt"

	"github.com/dvoyni/cog/gfx"
)

// commandListState is what a command list keeps while it records, pooled
// from frame to frame so recording allocates nothing once warm: the arena it
// takes chunks from, of chunkSize bytes, a multiple of 4; the arena chunks
// it took this frame, blocks going into the current one until it
// lacks room; the temporary meshes it acquired this frame; the pass and
// draw parameters set in its open pass, by their group, or the parameters
// set in its open compute pass, of any group; and the draft its
// draws build bind groups in, and hand the dynamic offsets of each they
// set; and the regions its copies hand gfx.
type commandListState struct {
	arena               *temporaryArena
	chunkSize           uint64
	takenChunks         []*arenaChunk
	currentChunk        *arenaChunk
	temporaryMeshes     []Mesh
	parameterSettings   [reservedGroup][]parameterSetting
	computeSettings     []parameterSetting
	bindGroupDraft      bindGroupDraft
	dynamicOffsets      [maxGroupDynamicOffsetCount]uint32
	textureCopies       [1]gfx.TextureCopy
	bufferTextureCopies [1]gfx.BufferTextureCopy
}

// allocate answers a range of a chunk s holds holding data, at a multiple of
// temporaryAlignment, taking from s's arena a chunk of s's chunk size when
// the current one lacks room, or one of data's size rounded up to a multiple
// of it when data is larger, so a large block whose size varies from frame
// to frame reuses its chunk; the zero BufferRange for no data. table's mutex
// is held, as a chunk made is a buffer of table's. The error is gfx's, when
// a chunk cannot be made.
func (s *commandListState) allocate(data []byte, table *assetTable, device gfx.Device) (BufferRange, error) {
	table.requireLocked("allocate")
	if bufferRange, ok := s.placeInCurrentChunk(data); ok {
		return bufferRange, nil
	}
	chunkSize := s.chunkSize
	if chunkSize%4 != 0 {
		panic(fmt.Sprintf("render: allocating from chunks of %d bytes, no multiple of 4", chunkSize))
	}
	size := uint64(len(data))
	paddedSize := (size + 3) &^ 3
	chunk, err := s.arena.acquireChunk((paddedSize+chunkSize-1)/chunkSize*chunkSize, table, device)
	if err != nil {
		return BufferRange{}, err
	}
	s.takenChunks = append(s.takenChunks, chunk)
	if paddedSize <= chunkSize {
		s.currentChunk = chunk
	}
	copy(chunk.bytes, data)
	chunk.end = size
	return BufferRange{Buffer: chunk.buffer, Size: size}, nil
}

// placeInCurrentChunk answers a range of s's current chunk holding data, at
// a multiple of temporaryAlignment, and whether the chunk has room for it;
// the zero BufferRange, and true, for no data. It takes no lock, as only
// s's list touches s.
func (s *commandListState) placeInCurrentChunk(data []byte) (BufferRange, bool) {
	size := uint64(len(data))
	if size == 0 {
		return BufferRange{}, true
	}
	chunk := s.currentChunk
	if chunk == nil {
		return BufferRange{}, false
	}
	offset := (chunk.end + temporaryAlignment - 1) &^ (temporaryAlignment - 1)
	if offset+(size+3)&^3 > uint64(len(chunk.bytes)) {
		return BufferRange{}, false
	}
	copy(chunk.bytes[offset:], data)
	chunk.end = offset + size
	return BufferRange{Buffer: chunk.buffer, Offset: offset, Size: size}, true
}

// writeChunks hands gfx the bytes of each chunk s took, in one write each,
// up to the end of its last block, so gfx's next submit carries them.
func (s *commandListState) writeChunks(table *assetTable, device gfx.Device) {
	for _, chunk := range s.takenChunks {
		writeBytes(table, device, chunk.gfxBuffer, 0, chunk.bytes[:(chunk.end+3)&^3])
	}
}

// clearParameterSettings forgets every pass, draw and compute parameter
// set, for the next pass, keeping their arrays.
func (s *commandListState) clearParameterSettings() {
	for group := range s.parameterSettings {
		s.parameterSettings[group] = s.parameterSettings[group][:0]
	}
	s.computeSettings = s.computeSettings[:0]
}

// reset empties s for the list that takes it next, keeping its arrays.
func (s *commandListState) reset() {
	clear(s.takenChunks)
	s.takenChunks = s.takenChunks[:0]
	s.currentChunk = nil
	s.temporaryMeshes = s.temporaryMeshes[:0]
	s.clearParameterSettings()
}
