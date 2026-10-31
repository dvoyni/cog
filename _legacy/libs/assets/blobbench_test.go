package assets_test

import (
	"testing"

	"github.com/dvoyni/cog/libs/assets"
)

// benchSink keeps the constructed Blobs from being optimised away, so the
// allocation count below is the constructor's and not the optimiser's.
var benchSink assets.Blob

// Blob is a pointer and a length rather than a box because the descriptors
// holding one - a buffer's bytes, a texture's pixels, a parameter's raw layout -
// are built per frame, where construction is allocation-free today. A box makes
// every one of those a heap allocation. This is the assertion that keeps it so.
func BenchmarkNewBlobAllocatesNothing(b *testing.B) {
	source := []byte("a run of bytes that is built once and kept")
	b.ReportAllocs()
	for b.Loop() {
		benchSink = assets.NewBlob(source)
	}
	if allocs := testing.AllocsPerRun(100, func() { benchSink = assets.NewBlob(source) }); allocs != 0 {
		b.Fatalf("NewBlob allocated %v times per call, want 0", allocs)
	}
}

// The string constructor is the one inline text is meant to go through, so it
// pays nothing either: unsafe.StringData does not copy, where []byte(s) would
// allocate a fresh array and a fresh identity per conversion.
func BenchmarkNewBlobFromStringAllocatesNothing(b *testing.B) {
	const source = "// a shader written at the call site"
	b.ReportAllocs()
	for b.Loop() {
		benchSink = assets.NewBlobFromString(source)
	}
	if allocs := testing.AllocsPerRun(100, func() { benchSink = assets.NewBlobFromString(source) }); allocs != 0 {
		b.Fatalf("NewBlobFromString allocated %v times per call, want 0", allocs)
	}
}
