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
	// missing, when set, is what probe answers: a machine with no output
	// device at all, whose open and play succeed anyway. That is oto's
	// Windows driver with no endpoints, whose null context takes players and
	// never errors, and which is unexported, so this is the only way a test
	// reaches it.
	missing error
	// openErr, when set, is what every open fails with: a device that could
	// never be opened.
	openErr error
	// gone, when set, is what poll answers and every play fails with: a
	// device that was playing and went away.
	gone error
	// source is what the player pulls, from play until close.
	source io.Reader
	// probeCount, openCount, playCount and closeCount count each call that
	// found something to do: close only while a player plays.
	probeCount, openCount, playCount, closeCount int
}

func (f *fakeDevice) probe() error {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.probeCount++
	return f.missing
}

func (f *fakeDevice) open(sampleRate int, bufferSize time.Duration) (deviceSettings, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.openCount++
	if f.openErr != nil {
		return deviceSettings{}, f.openErr
	}
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
	if f.gone != nil {
		return f.gone
	}
	f.source = source
	return nil
}

func (f *fakeDevice) poll() error {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.gone
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

// probes answers how many times the machine was asked whether it has an
// output device.
func (f *fakeDevice) probes() int {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.probeCount
}

// playing answers the source the player pulls, nil when none plays.
func (f *fakeDevice) playing() io.Reader {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.source
}

// installDevice is a sound card appearing on a machine that had none: the
// probe finds it, and the open it withheld goes ahead.
func (f *fakeDevice) installDevice() {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.missing = nil
}

// unplug is the headphones coming out: the poll answers err, and the play
// after it fails with err too, which is what a device truly gone looks like
// from here.
func (f *fakeDevice) unplug(err error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.gone = err
}

// refuseOpens has every open after it fail with err, as a lost device the
// retry cannot take again.
func (f *fakeDevice) refuseOpens(err error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.openErr = err
}

// plugBackIn is the device returning.
func (f *fakeDevice) plugBackIn() {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.gone = nil
}
