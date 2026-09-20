//go:build !js

package internal

import (
	"io"
	"sync"
	"time"
)

// fakeAudio is a device that opens nothing. The suite composes it instead of
// oto because a test needing a sound card could not run in CI, and because a
// process has exactly one oto context to spend - a test that spent it would
// spend it for every test after it in the same binary.
//
// Everything the Adapter owes is above this line: the ring, the voice table,
// the declick, the residency and the applied counter are all asserted by
// rendering blocks out of the Mixer directly, which is what a device would have
// been handed anyway.
type fakeAudio struct {
	mu sync.Mutex
	// openErr, if set, is what every open fails with, which is the Device that
	// could never be opened.
	openErr error
	// playErr, if set, is what every play fails with.
	playErr error
	// lost, once set, is what err reports, which is the Device that was open
	// and went away.
	lost error
	// rate and buffer are what open reports in force, defaulting to what it was
	// asked for.
	rate    int
	buffer  time.Duration
	ignored bool

	opens, plays int
	source       io.Reader
}

func (f *fakeAudio) open(rate int, buffer time.Duration) (facts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opens++
	if f.openErr != nil {
		return facts{}, f.openErr
	}
	settled := facts{sampleRate: rate, bufferSize: buffer, ignored: f.ignored}
	if f.rate != 0 {
		settled.sampleRate = f.rate
	}
	if f.buffer != 0 {
		settled.bufferSize = f.buffer
	}
	return settled, nil
}

func (f *fakeAudio) play(source io.Reader) (time.Duration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.plays++
	if f.playErr != nil {
		return 0, f.playErr
	}
	f.source = source
	return f.buffer, nil
}

func (f *fakeAudio) err() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lost
}

func (f *fakeAudio) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.source = nil
}

func (f *fakeAudio) openCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.opens
}
