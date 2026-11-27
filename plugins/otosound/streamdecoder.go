//go:build !js

package otosound

import (
	"bytes"

	"github.com/jfreymuth/oggvorbis"
)

// streamDecoder is one decoder over a streamed clip's bytes, which a voice's
// read-ahead reads ahead of the mixer. It is an interface so that the
// streamed tier is testable without an Ogg: a test's decoder can count its
// reads, generate frames it can name one by one, or panic if it is ever
// called on the device thread, which is how nothing decoding there is proved
// rather than asserted.
type streamDecoder interface {
	// read decodes into samples, interleaved, and answers how many it
	// wrote. It answers io.EOF at the stream's end, as an io.Reader does.
	read(samples []float32) (int, error)
	// seek moves to a frame of the stream, in its source's frames. The
	// streamed tier reads bytes in memory, which seek, so it is sample-exact.
	seek(frame int64) error
}

// oggDecoder is the streamed tier's decoder: oggvorbis over a bytes.Reader
// on the clip's bytes, which the decoder reads where they are, copying
// nothing.
type oggDecoder struct {
	reader *oggvorbis.Reader
}

// openOggDecoder opens a decoder over encoded, a streamed clip's bytes.
func openOggDecoder(encoded []byte) (streamDecoder, error) {
	reader, err := oggvorbis.NewReader(bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	return oggDecoder{reader: reader}, nil
}

func (d oggDecoder) read(samples []float32) (int, error) { return d.reader.Read(samples) }

// seek seeks the page before frame and skips the rest of the way, so it
// lands on frame exactly.
func (d oggDecoder) seek(frame int64) error { return d.reader.SetPosition(frame) }
