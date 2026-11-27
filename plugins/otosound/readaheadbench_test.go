//go:build !js

package otosound

import "testing"

// BenchmarkAStreamedVoiceOpensItsDecoder is what every streamed voice pays on
// its read-ahead's goroutine before its first frame: a decoder opened over
// the clip's bytes and sought to where the voice starts. The first open in
// the process parses the clip's setup header; every open after it, which is
// every voice a game starts, finds that setup parsed and pays only for its
// own buffers and the seek.
func BenchmarkAStreamedVoiceOpensItsDecoder(b *testing.B) {
	encoded := readFixture(b)
	openAndSeek := func() {
		decoder, err := openOggDecoder(encoded)
		if err != nil {
			b.Fatal(err)
		}
		if err := decoder.seek(clipFrames / 2); err != nil {
			b.Fatal(err)
		}
	}
	openAndSeek()
	b.ReportAllocs()
	for b.Loop() {
		openAndSeek()
	}
}
