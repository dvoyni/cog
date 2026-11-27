//go:build !js

package otosound

import (
	"io"
	"sync"
	"sync/atomic"
	"testing"
)

// generatedSample is one sample of a generated clip, which says exactly which
// frame and channel it came from, so a rendered block is compared frame by
// frame rather than by energy.
func generatedSample(frame int64, channel int) float32 {
	return float32((frame+int64(channel)*7)%1000) / 2000
}

// generatedDecoder is one decoder over frames a test computed.
type generatedDecoder struct {
	frames   int64
	channels int
	position int64
	// guarded makes a read on the device thread a panic.
	guarded bool
	reads   atomic.Int64
	seeks   atomic.Int64
	// wraps counts the seeks that came after the stream ran out, which a
	// read-ahead looping the whole clip makes.
	wraps atomic.Int64
}

func (d *generatedDecoder) read(samples []float32) (int, error) {
	if d.guarded && isOnDeviceGoroutine() {
		panic("a decoder was called on the thread that fills the device buffer")
	}
	d.reads.Add(1)
	if d.position >= d.frames {
		return 0, io.EOF
	}
	frameCount := min(int64(len(samples)/d.channels), d.frames-d.position)
	for frame := range frameCount {
		for channel := range d.channels {
			samples[int(frame)*d.channels+channel] = generatedSample(d.position+frame, channel)
		}
	}
	d.position += frameCount
	return int(frameCount) * d.channels, nil
}

func (d *generatedDecoder) seek(frame int64) error {
	d.seeks.Add(1)
	if d.position >= d.frames {
		d.wraps.Add(1)
	}
	d.position = frame
	return nil
}

// generatedStream is a streamed clip of frames a test computed: it opens a
// generated decoder per voice, and keeps each, so a test can ask what its
// voice's read-ahead did.
type generatedStream struct {
	frames   int64
	channels int
	guarded  bool
	// openErr, when set, is what every open fails with.
	openErr        error
	mutex          sync.Mutex
	openedDecoders []*generatedDecoder
}

func (g *generatedStream) open([]byte) (streamDecoder, error) {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	if g.openErr != nil {
		return nil, g.openErr
	}
	openedDecoder := &generatedDecoder{frames: g.frames, channels: max(g.channels, 1), guarded: g.guarded}
	g.openedDecoders = append(g.openedDecoders, openedDecoder)
	return openedDecoder, nil
}

// openCount answers how many decoders the stream opened.
func (g *generatedStream) openCount() int {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	return len(g.openedDecoders)
}

// decoder answers the index'th decoder the stream opened.
func (g *generatedStream) decoder(t *testing.T, index int) *generatedDecoder {
	t.Helper()
	g.mutex.Lock()
	defer g.mutex.Unlock()
	if index >= len(g.openedDecoders) {
		t.Fatalf("the read-aheads opened %d decoders, want more than %d", len(g.openedDecoders), index)
	}
	return g.openedDecoders[index]
}

// clip answers the streamed clip over the generated frames, at testRate, the rate
// the test mixer produces, so a rendered frame is the frame the decoder made
// with nothing filtered in between.
func (g *generatedStream) clip() *preparedClip {
	return &preparedClip{frames: g.frames, channels: max(g.channels, 1), sampleRate: testRate, openDecoder: g.open}
}
