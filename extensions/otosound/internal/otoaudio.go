//go:build !js

package internal

import (
	"errors"
	"io"
	"sync"
	"time"

	"github.com/dvoyni/cog/extensions/otosound"
	"github.com/ebitengine/oto/v3"
)

const (
	// defaultSampleRate is the rate the Mixer produces when Config names none.
	defaultSampleRate = 48000
	// defaultBufferSize is how much audio the device buffers when Config names
	// none. The spike ran 10ms for 12s with zero underruns - 1200 reads,
	// exactly 12.000s produced - and it is also the declick ramp's length,
	// because the ramp is one block.
	defaultBufferSize = 10 * time.Millisecond
)

// errNoContext is a play attempted before any context exists, which only
// happens if the open that should have created one was lost to a shutdown.
var errNoContext = errors.New("otosound: the audio context was not created")

// facts are what taking the device settled. They may differ from what a Config
// asked for, because the context is per process and the first composition in it
// owns the rate and the buffer size.
type facts struct {
	sampleRate int
	bufferSize time.Duration
	ignored    bool
}

// audio is how the Adapter reaches a sound device, named as an interface so
// that everything above it is testable without one. A suite that needed a sound
// card could not run in CI, and a process has exactly one oto context to spend,
// so a test that opened the real one would spend it for every test after it.
type audio interface {
	// open takes or creates the process-wide context and reports what is
	// actually in force. It does not wait for the device to become usable, so
	// it is cheap enough to call from startup.
	open(rate int, buffer time.Duration) (facts, error)
	// play attaches source to a player of that context and starts it. It may
	// wait for the context to become ready, so it runs on its own goroutine.
	play(source io.Reader) (latency time.Duration, err error)
	// err reports a device that has gone away since play succeeded, which is
	// polled rather than pushed because oto has nothing to push.
	err() error
	// close releases this Engine's player. The context is the process's and
	// outlives every Engine in it.
	close()
}

// The process-wide context, and what it was opened at. It is a package-level
// singleton because oto's is: creating a second one is an error, so the first
// composition's Config is the one in force for every Engine in the process.
var (
	contextMu     sync.Mutex
	sharedContext *oto.Context
	sharedReady   chan struct{}
	sharedRate    int
	sharedBuffer  time.Duration
)

// otoAudio is the real device: one oto context per process, one player per
// Engine. Both Engines are audible; what the second one loses is its Config.
type otoAudio struct {
	player *oto.Player
}

func (a *otoAudio) open(rate int, buffer time.Duration) (facts, error) {
	contextMu.Lock()
	defer contextMu.Unlock()
	if sharedContext != nil {
		return facts{
			sampleRate: sharedRate,
			bufferSize: sharedBuffer,
			ignored:    sharedRate != rate || sharedBuffer != buffer,
		}, nil
	}
	created, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:      rate,
		ChannelCount:    outChannels,
		Format:          oto.FormatFloat32LE,
		BufferSize:      buffer,
		ApplicationName: string(otosound.Name),
	})
	if err != nil {
		return facts{}, err
	}
	sharedContext, sharedReady, sharedRate, sharedBuffer = created, ready, rate, buffer
	return facts{sampleRate: rate, bufferSize: buffer}, nil
}

func (a *otoAudio) play(source io.Reader) (time.Duration, error) {
	contextMu.Lock()
	context, ready, rate, buffer := sharedContext, sharedReady, sharedRate, sharedBuffer
	contextMu.Unlock()
	if context == nil {
		return 0, errNoContext
	}
	<-ready
	if err := context.Err(); err != nil {
		return 0, err
	}
	player := context.NewPlayer(source)
	// oto takes the context's buffer as a duration and the player's as bytes,
	// and both have to be said: the context sizes the device's buffer and the
	// player sizes the queue the Mixer is pulled into.
	player.SetBufferSize(int(buffer.Seconds()*float64(rate)) * bytesPerFrame)
	player.Play()
	a.player = player
	return buffer, nil
}

func (a *otoAudio) err() error {
	contextMu.Lock()
	context := sharedContext
	contextMu.Unlock()
	if context != nil {
		if err := context.Err(); err != nil {
			return err
		}
	}
	if a.player == nil {
		return nil
	}
	return a.player.Err()
}

func (a *otoAudio) close() {
	if a.player == nil {
		return
	}
	a.player.Pause()
	a.player = nil
}
