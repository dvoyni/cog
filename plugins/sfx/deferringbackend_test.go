package sfx

import "github.com/dvoyni/cog/libs/m"

// deferringBackend answers every prepare done=false, keeping its token until
// finish completes it, and counts the IDs it mints.
type deferringBackend struct {
	deferred     []any
	completed    []Prepared
	installCount uint32
}

func (*deferringBackend) Voices(int) {}

func (*deferringBackend) Emit(*FlushChanges) {}

func (*deferringBackend) Device() Device { return Device{Ready: true} }

func (b *deferringBackend) Prepare(token any, _ []byte) (PreparedClip, bool, error) {
	b.deferred = append(b.deferred, token)
	return nil, false, nil
}

func (b *deferringBackend) TakePrepared() []Prepared {
	completed := b.completed
	b.completed = nil
	return completed
}

func (b *deferringBackend) Install(PreparedClip) (ClipID, error) {
	b.installCount++
	return ClipID(b.installCount), nil
}

// finish completes every prepare deferred so far, for the next TakePrepared.
func (b *deferringBackend) finish() {
	for _, token := range b.deferred {
		b.completed = append(b.completed, Prepared{Token: token, Clip: oneSecondClip{}})
	}
	b.deferred = nil
}

// oneSecondClip is the clip a deferringBackend prepares, one second long.
type oneSecondClip struct{}

func (oneSecondClip) Duration() float32 { return 1 }

func (oneSecondClip) Channels() int { return 1 }

func (oneSecondClip) SampleRate() int { return 48000 }

func (oneSecondClip) LoopRegion() m.Maybe[LoopRegion] { return m.Maybe[LoopRegion]{} }

func (oneSecondClip) LoopRegionErr() error { return nil }
