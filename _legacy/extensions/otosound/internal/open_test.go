//go:build !js

package internal

import (
	"os"
	"testing"

	"github.com/dvoyni/cog/libs/assets"
)

// BenchmarkAStreamedVoiceOpensItsDecoder is what every streamed Voice pays on
// its read-ahead goroutine before the first frame: opening a decoder over the
// Clip's bytes and seeking it to where the Voice starts. The first open in the
// process parses the Clip's setup header; every open after it, which is every
// Voice a game actually starts, finds that setup already parsed and pays only
// for its own buffers, the length scan and the seek.
func BenchmarkAStreamedVoiceOpensItsDecoder(b *testing.B) {
	data, err := os.ReadFile("testdata/pianoroll.ogg")
	if err != nil {
		b.Fatalf("reading the fixture clip: %v", err)
	}
	encoded := assets.NewBlob(data)
	open := func() {
		src, err := openOgg(encoded)
		if err != nil {
			b.Fatal(err)
		}
		if err := src.seek(clipFrames / 2); err != nil {
			b.Fatal(err)
		}
	}
	open()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		open()
	}
}
