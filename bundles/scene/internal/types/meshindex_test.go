package types

import (
	"encoding/binary"
	"testing"

	"github.com/dvoyni/cog/extensions/gfx/gpu"
)

// The whole of scene's index-width rule, at the boundary that matters. The
// threshold is 65535 rather than 65536 because 0xFFFF is WebGPU's
// primitive-restart value for a uint16 strip: at 65535 vertices the largest
// legal index is 65534, so the restart value never appears in the buffer.
func TestIndexWidthFollowsTheVertexCount(t *testing.T) {
	for _, c := range []struct {
		vertices int
		want     gpu.IndexWidth
	}{
		{1, gpu.IndexUint16},
		{3, gpu.IndexUint16},
		{65535, gpu.IndexUint16},
		{65536, gpu.IndexUint32},
		{200000, gpu.IndexUint32},
	} {
		if got := indexWidthFor(c.vertices); got != c.want {
			t.Errorf("indexWidthFor(%d) = %v, want %v", c.vertices, got, c.want)
		}
	}
}

// The narrowed bytes are the same indices, not a reinterpret of half of them.
func TestNarrowedIndexBytesAreTheSameIndices(t *testing.T) {
	bytes := indexBytes([]uint32{0, 1, 65534}, gpu.IndexUint16)
	if len(bytes) != 6 {
		t.Fatalf("three uint16 indices are %d bytes, want 6", len(bytes))
	}
	for i, want := range []uint16{0, 1, 65534} {
		if got := binary.NativeEndian.Uint16(bytes[i*2:]); got != want {
			t.Errorf("index %d = %d, want %d", i, got, want)
		}
	}
	wide := indexBytes([]uint32{0, 1, 2}, gpu.IndexUint32)
	if len(wide) != 12 {
		t.Fatalf("three uint32 indices are %d bytes, want 12", len(wide))
	}
}
