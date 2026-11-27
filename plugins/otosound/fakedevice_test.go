//go:build !js

package otosound

import (
	"io"
	"sync"
	"time"
)

// fakeDevice is a sound card that opens nothing, which a test sets on the
// plugin it builds in place of oto: a test needing a sound card could not run
// in CI, and a process has one oto context to spend, so a test that spent it
// would spend it for every test after it in the binary.
type fakeDevice struct {
	mutex sync.Mutex
	// settled, when set, is what open answers in force whatever it is asked
	// for, as the process's context answers a kernel after the one that made
	// it; unset, open answers what it was asked for.
	settled deviceSettings
	// contextReady, when set, is what play waits on before it starts the
	// player, as oto's play waits for the context to be ready; unset, play
	// starts it at once.
	contextReady chan struct{}
	// source is what the player pulls, from play until close.
	source io.Reader
	// openCount, playCount and closeCount count each call that found
	// something to do: close only while a player plays.
	openCount, playCount, closeCount int
}

func (f *fakeDevice) open(sampleRate int, bufferSize time.Duration) (deviceSettings, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.openCount++
	if f.settled != (deviceSettings{}) {
		return f.settled, nil
	}
	return deviceSettings{sampleRate: sampleRate, bufferSize: bufferSize}, nil
}

func (f *fakeDevice) play(source io.Reader) error {
	if f.contextReady != nil {
		<-f.contextReady
	}
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.playCount++
	f.source = source
	return nil
}

// close models oto's PauseAndStopReading: once it returns, nothing reads the
// source.
func (f *fakeDevice) close() {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if f.source == nil {
		return
	}
	f.source = nil
	f.closeCount++
}

// counts answers how many times the device was opened, played and closed.
func (f *fakeDevice) counts() (opens, plays, closes int) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.openCount, f.playCount, f.closeCount
}

// playing answers the source the player pulls, nil when none plays.
func (f *fakeDevice) playing() io.Reader {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.source
}
