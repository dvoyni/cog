package render

import (
	"fmt"

	"github.com/dvoyni/cog/gfx"
)

// commandListState is what a command list keeps while it records, pooled
// from frame to frame so recording allocates nothing once warm: the arena
// chunks it took this frame, blocks going into the current one until it
// lacks room; the pass and draw parameters set in its open pass, by their
// group; and the draft its draws build bind groups in, and hand the dynamic
// offsets of each they set.
type commandListState struct {
	takenChunks       []*arenaChunk
	currentChunk      *arenaChunk
	parameterSettings [reservedGroup][]parameterSetting
	bindGroupDraft    bindGroupDraft
	dynamicOffsets    [maxGroupDynamicOffsetCount]uint32
}

// allocate answers a range of a chunk s holds holding data, at a multiple of
// temporaryAlignment, taking from arena a chunk of chunkSize bytes, a
// multiple of 4, when the current one lacks room, or one of data's size
// rounded up to a multiple of chunkSize when data is larger, so a large
// block whose size varies from frame to frame reuses its chunk; the zero
// BufferRange for no data. The error is gfx's, when a chunk cannot be made.
func (s *commandListState) allocate(data []byte, chunkSize uint64, arena *temporaryArena, table *assetTable, device gfx.Device) (BufferRange, error) {
	if chunkSize%4 != 0 {
		panic(fmt.Sprintf("render: allocating from chunks of %d bytes, no multiple of 4", chunkSize))
	}
	size := uint64(len(data))
	if size == 0 {
		return BufferRange{}, nil
	}
	paddedSize := (size + 3) &^ 3
	chunk := s.currentChunk
	var offset uint64
	if chunk != nil {
		offset = (chunk.end + temporaryAlignment - 1) &^ (temporaryAlignment - 1)
	}
	if chunk == nil || offset+paddedSize > uint64(len(chunk.bytes)) {
		newChunk, err := arena.acquireChunk((paddedSize+chunkSize-1)/chunkSize*chunkSize, table, device)
		if err != nil {
			return BufferRange{}, err
		}
		s.takenChunks = append(s.takenChunks, newChunk)
		if paddedSize <= chunkSize {
			s.currentChunk = newChunk
		}
		chunk = newChunk
		offset = 0
	}
	copy(chunk.bytes[offset:], data)
	chunk.end = offset + size
	return BufferRange{Buffer: chunk.buffer, Offset: offset, Size: size}, nil
}

// writeChunks hands gfx the bytes of each chunk s took, in one write each,
// up to the end of its last block, so gfx's next submit carries them.
func (s *commandListState) writeChunks(table *assetTable, device gfx.Device) {
	for _, chunk := range s.takenChunks {
		writeBytes(table, device, chunk.gfxBuffer, 0, chunk.bytes[:(chunk.end+3)&^3])
	}
}

// clearParameterSettings forgets every pass and draw parameter set, for the next
// pass, keeping their arrays.
func (s *commandListState) clearParameterSettings() {
	for group := range s.parameterSettings {
		s.parameterSettings[group] = s.parameterSettings[group][:0]
	}
}

// reset empties s for the list that takes it next, keeping its arrays.
func (s *commandListState) reset() {
	clear(s.takenChunks)
	s.takenChunks = s.takenChunks[:0]
	s.currentChunk = nil
	s.clearParameterSettings()
}
