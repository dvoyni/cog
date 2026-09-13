package scene

import (
	"math"
	"slices"
	"testing"
)

// The opaque key is material first and mesh second, so a pass walks every draw
// of one pipeline before switching, and the same mesh runs together inside it.
func TestOpaqueKeyIsMaterialThenMesh(t *testing.T) {
	if key := opaqueKey(3, 7); key != 3<<32|7 {
		t.Fatalf("opaqueKey(3, 7) = %#x, want material in the high word and mesh in the low", key)
	}
	if opaqueKey(1, math.MaxUint32) >= opaqueKey(2, 0) {
		t.Fatal("a lower material id with any mesh sorts after a higher material id")
	}
}

// The blend key sorts back to front: a farther draw takes the smaller key so
// an ascending sort emits it first.
func TestBlendKeyOrdersFartherFirst(t *testing.T) {
	depths := []float32{-1, 0, 0.5, 1, 2.5, 100, math.MaxFloat32}
	for i := 1; i < len(depths); i++ {
		near, far := blendKey(depths[i-1]), blendKey(depths[i])
		if far >= near {
			t.Fatalf("depth %v keys %#x and depth %v keys %#x; the farther must be smaller",
				depths[i], far, depths[i-1], near)
		}
	}
}

func TestSortEntriesOrdersByKeyThenRecordingOrdinal(t *testing.T) {
	entries := []sortEntry{
		{key: 2, draw: 5},
		{key: 1, draw: 9},
		{key: 2, draw: 1},
		{key: 1, draw: 3},
		{key: 0, draw: 7},
	}
	sortEntries(entries)
	want := []sortEntry{{0, 7}, {1, 3}, {1, 9}, {2, 1}, {2, 5}}
	if !slices.Equal(entries, want) {
		t.Fatalf("sorted to %v, want %v", entries, want)
	}
}

// The sort runs on a reused slice of 12-byte entries and must allocate nothing,
// because it runs once per pass per frame.
func TestSortEntriesAllocatesNothing(t *testing.T) {
	entries := make([]sortEntry, 1000)
	fill := func() {
		for i := range entries {
			entries[i] = sortEntry{key: uint64((i * 7919) % 101), draw: uint32(i)}
		}
	}
	fill()
	if allocations := testing.AllocsPerRun(20, func() { fill(); sortEntries(entries) }); allocations != 0 {
		t.Fatalf("sorting allocated %v times per pass", allocations)
	}
}
