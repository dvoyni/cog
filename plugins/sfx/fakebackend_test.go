package sfx_test

import (
	"sync"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/sfx"
)

// fakeBackend is a backend sfx's tests bind through sfxtest's UseBackend, for
// what the headless backend never does: a prepare answering done=false and
// finishing at a later flush, or a backend refusing a clip or preparing
// nothing. It records nothing of what it is handed; a test wraps it with
// seamPlugin for that. Its fields are set before Spawn, and its counts are
// guarded, since the test reads what the flush's job wrote.
type fakeBackend struct {
	// prepareErr, when set, is what every Prepare answers instead.
	prepareErr error
	// deferring has every Prepare answer done=false, as a backend preparing
	// in a goroutine of its own does, until finishPrepares.
	deferring bool
	// preparingNothing has every Prepare answer done=true with neither a
	// prepared clip nor an error.
	preparingNothing bool
	// installingZero has every Install answer the zero ID.
	installingZero bool
	// mutex guards deferred, the tokens of the prepares answered done=false
	// and not yet finished; completed, what the next TakePrepared answers;
	// prepareCount, how many prepares were asked for; and installCount, how
	// many IDs were minted.
	mutex        sync.Mutex
	deferred     []any
	completed    []sfx.Prepared
	prepareCount int
	installCount uint32
}

func (*fakeBackend) Voices(int) {}

func (*fakeBackend) Emit(*sfx.FlushChanges) {}

func (*fakeBackend) Device() sfx.Device {
	return sfx.Device{Ready: true, Name: "fake", SampleRate: 48000, Channels: 2}
}

func (b *fakeBackend) Prepare(token any, _ []byte) (sfx.PreparedClip, bool, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.prepareCount++
	switch {
	case b.prepareErr != nil:
		return nil, false, b.prepareErr
	case b.deferring:
		b.deferred = append(b.deferred, token)
		return nil, false, nil
	case b.preparingNothing:
		return nil, true, nil
	}
	return fakeClip{}, true, nil
}

func (b *fakeBackend) TakePrepared() []sfx.Prepared {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	completed := b.completed
	b.completed = nil
	return completed
}

func (b *fakeBackend) Install(sfx.PreparedClip) (sfx.ClipID, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if b.installingZero {
		return 0, nil
	}
	b.installCount++
	return sfx.ClipID(b.installCount), nil
}

// finishPrepares finishes every prepare answered done=false so far, failed
// with err when it is not nil, for the next TakePrepared to answer: how a
// test says the goroutine a backend spawned has finished, without one.
func (b *fakeBackend) finishPrepares(err error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	for _, token := range b.deferred {
		if err != nil {
			b.completed = append(b.completed, sfx.Prepared{Token: token, Err: err})
			continue
		}
		b.completed = append(b.completed, sfx.Prepared{Token: token, Clip: fakeClip{}})
	}
	b.deferred = nil
}

// finishFirstPrepare finishes only the earliest prepare answered done=false
// and not yet finished, for the next TakePrepared to answer, leaving the rest
// in flight.
func (b *fakeBackend) finishFirstPrepare() {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if len(b.deferred) == 0 {
		return
	}
	b.completed = append(b.completed, sfx.Prepared{Token: b.deferred[0], Clip: fakeClip{}})
	b.deferred = b.deferred[1:]
}

// counts answers how many prepares were asked for and how many IDs were
// minted.
func (b *fakeBackend) counts() (prepareCount int, installCount uint32) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.prepareCount, b.installCount
}

// fakeClipSeconds is the length of the clip a fakeBackend prepares, which no
// test plays to its end.
const fakeClipSeconds = 4

// fakeClip is the clip a fakeBackend prepares: mono at 48 kHz,
// fakeClipSeconds long, carrying the facts and no samples, which is what sfx
// reads of every backend's.
type fakeClip struct{}

func (fakeClip) Duration() float32 { return fakeClipSeconds }

func (fakeClip) Channels() int { return 1 }

func (fakeClip) SampleRate() int { return 48000 }

func (fakeClip) LoopRegion() m.Maybe[sfx.LoopRegion] { return m.Maybe[sfx.LoopRegion]{} }

func (fakeClip) LoopRegionErr() error { return nil }
