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
	// missing, if set, is what absent reports: a machine with no output device
	// at all, whose open and play both succeed anyway. That combination is not
	// a contrivance - it is precisely oto's Windows driver with no endpoints,
	// which substitutes a null context that takes players, drains the Mixer and
	// never errors - and it is the only way to get at it from a test, because
	// the thing being modelled is unexported one module over.
	missing error
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

	opens, plays, closes, probes int
	source                       io.Reader
}

func (f *fakeAudio) absent() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.probes++
	return f.missing
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

// close models oto's PauseAndStopReading rather than its Pause: it returns only
// once nothing is reading the Mixer any more. Nothing in the fixture reads it
// at all, so what the fixture owes is the count and the honest name.
func (f *fakeAudio) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.source = nil
	f.closes++
}

func (f *fakeAudio) openCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.opens
}

func (f *fakeAudio) playCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.plays
}

func (f *fakeAudio) closeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closes
}

func (f *fakeAudio) probeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.probes
}

// installDevice is a sound card appearing on a machine that had none: the
// endpoint the probe asks about is now there, and the open that was being
// withheld can go ahead.
func (f *fakeAudio) installDevice() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.missing = nil
}

// unplug is the headphones coming out: the poll starts erroring, and the
// reattach that follows cannot take a player either, which is what a device
// that is genuinely gone looks like from here.
func (f *fakeAudio) unplug(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lost, f.playErr = err, err
}

// plugBackIn is the device returning.
func (f *fakeAudio) plugBackIn() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lost, f.playErr = nil, nil
}
