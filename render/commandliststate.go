package render

import (
	"fmt"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/platform"
)

// commandListState is what a command list keeps while it records, pooled
// from frame to frame so recording allocates nothing once warm: the arena it
// takes chunks from, of chunkSize bytes, a multiple of 4; the arena chunks
// it took this frame, blocks going into the current one until it
// lacks room; the temporary meshes it acquired this frame; the pass and
// draw parameters set in its open pass, by their group, or the parameters
// set in its open compute pass, of any group; and the draft its
// draws build bind groups in, and hand the dynamic offsets of each they
// set; the regions its copies hand gfx, and the colour attachments its
// passes do; and, for the clear-after-draw
// check, its list's order and the passes it began, in order, with the
// targets they write, passes[i]'s the passTargets from its
// targetStart to its targetEnd.
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
	colorAttachments    [maxColorTargetCount]gfx.RenderPassColorAttachment
	order               Order
	passes              []notedPass
	passTargets         []notedTarget
	// inspectedList is the list's inspection, nil when its frame is not
	// inspected.
	inspectedList *InspectedCommandList
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
	chunk, err := s.arena.acquireChunk((paddedSize+chunkSize-1)/chunkSize*chunkSize, s.order, len(s.takenChunks), table, device)
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
		writeBytes(table, device, Buffer{}, chunk.gfxBuffer, 0, chunk.bytes[:(chunk.end+3)&^3])
	}
}

// notePass notes a pass of descriptor begun, labelled label, descriptor's
// label kept, with its colour targets, their resolve targets and its
// depth-stencil target unless it is read-only, and answers the index of its
// note in s's passes.
func (s *commandListState) notePass(label string, descriptor PassDescriptor) int {
	targetStart := len(s.passTargets)
	for _, target := range descriptor.ColorTargets {
		s.passTargets = append(s.passTargets, notedTarget{key: passTargetKey{window: target.Window, texture: target.Texture, mipLevel: target.MipLevel, layer: target.Layer}, cleared: target.Clear})
		if resolve := target.Resolve; resolve != (ResolveTarget{}) {
			s.passTargets = append(s.passTargets, notedTarget{key: passTargetKey{window: resolve.Window, texture: resolve.Texture, mipLevel: resolve.MipLevel, layer: resolve.Layer}})
		}
	}
	if target := descriptor.DepthStencil; target.Texture != (Texture{}) && !target.ReadOnly {
		s.passTargets = append(s.passTargets, notedTarget{key: passTargetKey{texture: target.Texture, mipLevel: target.MipLevel, layer: target.Layer}, cleared: target.Clear})
	}
	s.passes = append(s.passes, notedPass{label: label, targetStart: targetStart, targetEnd: len(s.passTargets)})
	return len(s.passes) - 1
}

// nextInspectedPosition answers the position in s's inspected list of the
// pass, compute pass, copy or readback recorded next: how many it holds.
func (s *commandListState) nextInspectedPosition() int {
	list := s.inspectedList
	return len(list.Passes) + len(list.ComputePasses) + len(list.Copies) + len(list.Readbacks)
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
	s.passes = s.passes[:0]
	s.passTargets = s.passTargets[:0]
	s.inspectedList = nil
}

// notedPass is a pass a command list began, for the clear-after-draw
// check: its label, where its targets are among its list's, and whether a
// draw of it reached its encoder.
type notedPass struct {
	label                  string
	targetStart, targetEnd int
	drew                   bool
}

// notedTarget is a target a noted pass writes, and
// whether the pass clears it; a resolve target is never cleared.
type notedTarget struct {
	key     passTargetKey
	cleared bool
}

// passTargetKey names what a pass draws into: a window, or a texture at one
// mip level and one layer.
type passTargetKey struct {
	window          platform.Window
	texture         Texture
	mipLevel, layer uint32
}
