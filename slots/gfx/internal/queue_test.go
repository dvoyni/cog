package internal

import (
	"bytes"
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// uniformSink records what a replay hands a backend about uniforms, and drops
// the rest.
type uniformSink struct {
	benchmarkGpuSink
	arena  []byte
	blocks [][2]int
}

func (s *uniformSink) BakeUniforms(arena []byte)           { s.arena = arena }
func (s *uniformSink) BeginPass(types.PassDesc) RenderPass { return s }
func (s *uniformSink) SetUniformBlock(offset, size int) {
	s.blocks = append(s.blocks, [2]int{offset, size})
}

// A block is as large as its shader declares, and only its start is aligned:
// blocks of different sizes share one arena, each binding exactly its own
// bytes at an offset a uniform binding accepts.
func TestUniformBlocksOfAnySizeStartAligned(t *testing.T) {
	var queue Queue
	queue.BeginPass(types.PassDesc{Screen: true})
	for _, size := range []int{80, 300, 16} {
		block := queue.SetUniformBlock(size)
		if len(block) != size {
			t.Fatalf("block len = %d, want %d", len(block), size)
		}
		for i := range block {
			block[i] = byte(size)
		}
	}
	queue.EndPass()

	sink := &uniformSink{}
	queue.ReplayBakes(sink)
	queue.ReplayPasses(sink)

	want := [][2]int{{0, 80}, {256, 300}, {768, 16}}
	if len(sink.blocks) != len(want) {
		t.Fatalf("blocks = %v, want %v", sink.blocks, want)
	}
	for i := range want {
		if sink.blocks[i] != want[i] {
			t.Errorf("block %d = (offset, size) %v, want %v", i, sink.blocks[i], want[i])
		}
	}
	if len(sink.arena) != 1024 {
		t.Errorf("arena = %d bytes, want 1024: each block rounded up to the alignment", len(sink.arena))
	}
	for _, b := range want {
		offset, size := b[0], b[1]
		if !bytes.Equal(sink.arena[offset:offset+size], bytes.Repeat([]byte{byte(size)}, size)) {
			t.Errorf("block at %d does not hold what was packed into it", offset)
		}
	}
}

// Reset keeps the arena's memory, and a claim hands it back zeroed - the block
// and the padding behind it - so nothing the last frame packed reaches this
// frame's upload.
func TestAReusedUniformBlockComesBackZeroed(t *testing.T) {
	var queue Queue
	block := queue.SetUniformBlock(UniformAlignment)
	for i := range block {
		block[i] = 0xFF
	}
	queue.Reset()

	queue.SetUniformBlock(16)
	sink := &uniformSink{}
	queue.ReplayBakes(sink)
	if !bytes.Equal(sink.arena, make([]byte, UniformAlignment)) {
		t.Errorf("arena after reuse = %x..., want zeroes", sink.arena[:8])
	}
}
