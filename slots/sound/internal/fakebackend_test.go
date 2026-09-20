package internal

import (
	"sync"

	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/sound"
)

// fakeClip is a prepared Clip that carries the facts and no samples, which is
// every Adapter's obligation reduced to the part sound actually reads.
type fakeClip struct {
	duration float32
	channels int
	rate     int
	// region is the Clip's Loop Region, absent unless a test gives it one. An
	// Adapter parses it out of the file's Vorbis comments; what sound sees is
	// two numbers in seconds, which is exactly what this hands it.
	region m.Maybe[sound.LoopRegion]
}

func (c fakeClip) Duration() float32                     { return c.duration }
func (c fakeClip) Channels() int                         { return c.channels }
func (c fakeClip) SampleRate() int                       { return c.rate }
func (c fakeClip) LoopRegion() m.Maybe[sound.LoopRegion] { return c.region }

// fakeBackend is the Adapter these tests compose: it records what sound handed
// it and answers what sound polls, so a test asserts about the Slot's own
// tick-computed state and about the one batch a tick produces.
//
// It is mutex-guarded because the test goroutine reads what the flush handler
// wrote, which is a boundary a real Adapter does not have - sound calls a
// Backend only from its flush.
type fakeBackend struct {
	mu sync.Mutex

	// slots is what Voices(n) was told, and countingVoices how often.
	slots          int
	countingVoices int

	device sound.Device

	// clip is what a successful Prepare reports.
	clip fakeClip
	// prepareErr, when set, is what every Prepare answers with instead.
	prepareErr error
	// deferring makes every Prepare answer done=false, the way an Adapter that
	// spawns a goroutine does.
	deferring bool
	// deferred holds the prepares answered with done=false, until a test
	// finishes them.
	deferred []any
	// completed is what the next TakePrepared drains.
	completed []sound.Prepared
	// installs counts the ids handed out; zero is none, so it starts at one.
	installs uint32
	// prepares counts the Prepares asked for, which is what "asking never
	// starts a load" is asserted against.
	prepares int

	// batches is every batch Emit was handed, copied out of the memory sound
	// owns and lends for the call.
	batches []sound.Batch
}

func newFakeBackend(clip fakeClip) *fakeBackend {
	return &fakeBackend{
		clip:   clip,
		device: sound.Device{Ready: true, Name: "fake", SampleRate: 48000, Channels: 2},
	}
}

func (b *fakeBackend) Voices(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.slots = n
	b.countingVoices++
}

func (b *fakeBackend) Emit(batch *sound.Batch) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.batches = append(b.batches, sound.Batch{
		Starts:   append([]sound.VoiceStart(nil), batch.Starts...),
		Updates:  append([]sound.VoiceUpdate(nil), batch.Updates...),
		Stops:    append([]sound.VoiceSlot(nil), batch.Stops...),
		Destroys: append([]sound.ClipID(nil), batch.Destroys...),
	})
}

func (b *fakeBackend) Device() sound.Device {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.device
}

// setReady is a Device arriving or going away between two ticks, which is the
// only way any of the three states reaches sound: nothing is pushed, and the
// flush reads whatever this says next time it asks.
func (b *fakeBackend) setReady(ready bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.device.Ready = ready
}

func (b *fakeBackend) Prepare(token any, _ assets.Blob) (sound.PreparedClip, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prepares++
	if b.prepareErr != nil {
		return nil, false, b.prepareErr
	}
	if b.deferring {
		b.deferred = append(b.deferred, token)
		return nil, false, nil
	}
	return b.clip, true, nil
}

func (b *fakeBackend) TakePrepared() []sound.Prepared {
	b.mu.Lock()
	defer b.mu.Unlock()
	taken := b.completed
	b.completed = nil
	return taken
}

func (b *fakeBackend) Install(sound.PreparedClip) (sound.ClipID, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.installs++
	return sound.ClipID(b.installs), nil
}

// finishPrepares moves every deferred prepare into what the next TakePrepared
// drains, which is how a test says "the goroutine an Adapter spawned has
// finished" without one.
func (b *fakeBackend) finishPrepares() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, token := range b.deferred {
		b.completed = append(b.completed, sound.Prepared{Token: token, Clip: b.clip})
	}
	b.deferred = nil
}

// emitted is the batches so far, copied so a test reads them without the lock.
func (b *fakeBackend) emitted() []sound.Batch {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]sound.Batch(nil), b.batches...)
}

// counts is how many Prepares were asked for and how many ids were handed out.
func (b *fakeBackend) counts() (prepares int, installs uint32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.prepares, b.installs
}

func (b *fakeBackend) slotCount() (slots, calls int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.slots, b.countingVoices
}
