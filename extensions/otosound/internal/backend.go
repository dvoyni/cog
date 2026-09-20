//go:build !js

package internal

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dvoyni/cog/extensions/otosound"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/slots/sound"
)

// errNotOurClip is Install handed a prepared value this Adapter did not make.
// It is unexported and untyped because nothing can reach it: sound installs
// only what this Adapter's own Prepare produced, so it is a guard against a
// future caller rather than a failure any composition can currently have.
var errNotOurClip = errors.New("otosound: install was handed a prepared Clip this Adapter did not make")

// pendingDestroy is a Clip sound has released, held until the Mixer has passed
// the batch that carried the destroy.
//
// This is the whole of "the Mixer never frees". A release is stated on the tick,
// and the Mixer may be mid-copy out of that Clip's samples, so the destroy
// travels as an operation inside the batch - ordered after the stops before it,
// which is what drops the voice table's references - and the last reference on
// this side goes only once the applied counter has passed that batch. The Mixer
// never frees and never reads freed memory; the tick never waits.
type pendingDestroy struct {
	id   sound.ClipID
	clip *clipData
	seq  uint64
}

// deviceFailure is a Device that could not be opened, carried from the
// goroutine that tried to the tick that holds a Kernel. A device-side object
// has no Kernel of its own, so it hands its error back to whoever does - gfx's
// takeRefusal, one Slot over.
type deviceFailure struct{ err error }

// backend is otosound's Adapter: the tick side of the seam. Every method of it
// is called from sound's flush, which holds a write lock on all four of sound's
// resources, so they are serialized against each other by construction and the
// clip table below needs no lock of its own.
//
// What is not serialized against them is the Mixer, and that boundary is the
// ring and nothing else.
type backend struct {
	// askedRate and askedBuffer are what this Engine's Config asked for. What
	// is in force may differ, because the oto context is per process and the
	// first composition owns it.
	askedRate   int
	askedBuffer time.Duration
	// limit is Config.DecodedClipLimit as it was given, sentinels and all. It
	// decides the tier a prepared Clip lands in and nothing else; the device
	// never sees it, and neither does sound.
	limit int

	// rate is the rate the Mixer produces and a Clip is converted to. It is
	// settled synchronously, when the context is taken, so that no Clip is ever
	// prepared against a rate that later turns out not to be the device's.
	rate int

	ring  *ring
	mixer *mixer

	// streams is the read-ahead of every streamed Voice, indexed by slot and
	// sized once at Voices(n). It is the tick's alone: the tick starts a
	// read-ahead when it stages a start on a streamed Clip and halts it when it
	// stages that slot's stop, which is how a read-ahead stops with its Voice
	// however the Voice ended - finished, stolen, released or despawned, all
	// four of which reach the Adapter as the same stop.
	//
	// The Mixer is handed the ring out of one of these and never the stream
	// itself, so nothing on the device thread can start, stop or wait on a
	// decoder.
	streams []*stream

	// clips is the table of installed Clips. It is the tick's alone: a ClipID
	// is resolved to a pointer when an operation is staged, so the device
	// thread never reads this map.
	clips  map[sound.ClipID]*clipData
	lastID sound.ClipID

	// destroyed, staged and pending are one release travelling: marked by the
	// Destroys entry that carried it, recorded into the staging batch by the
	// same Emit, and held against the applied counter until the Mixer has
	// passed the batch it went out in.
	destroyed []pendingDestroy
	staged    []pendingDestroy
	pending   []pendingDestroy

	// completedMu guards completed, which the prepare goroutines append to and
	// the flush drains. It is the only lock in the Adapter, and it is nowhere
	// the Mixer can reach.
	completedMu sync.Mutex
	completed   []sound.Prepared

	// device is what Device reports. The device side writes it, the tick side
	// reads it, and it changes under its reader with no notification - which is
	// correct, because loss is silent and a game reads Device rather than
	// handling it.
	device atomic.Pointer[sound.Device]

	// failure and ignored are the two things the Adapter has to say out loud,
	// left here for the tick handler that holds a Kernel to report once.
	failure atomic.Pointer[deviceFailure]
	ignored atomic.Pointer[otosound.ErrDeviceConfigIgnored]

	// everReady is whether a Device has ever been audible in this Engine. It is
	// what separates the one condition the Adapter reports - no Device could be
	// opened at all - from the one it never reports, which is a Device that was
	// open and went away.
	everReady atomic.Bool

	audio audio
	// cadence is how long the device goroutine waits between polling an open
	// Device and between retrying a closed one. It is retryEvery, and it is a
	// field only so the suite can run a loss and a recovery in milliseconds
	// instead of seconds; no Config reaches it.
	cadence time.Duration
	done    chan struct{}
	// wg tracks the device goroutine so Stop can wait for it, which is what
	// keeps a test from leaking one per engine.
	wg sync.WaitGroup
}

func newBackend(cfg otosound.Config, hardware audio) *backend {
	rate := cfg.SampleRate
	if rate == 0 {
		rate = defaultSampleRate
	}
	buffer := cfg.BufferSize
	if buffer == 0 {
		buffer = defaultBufferSize
	}
	b := &backend{
		askedRate:   rate,
		askedBuffer: buffer,
		limit:       cfg.DecodedClipLimit,
		rate:        rate,
		clips:       make(map[sound.ClipID]*clipData),
		audio:       hardware,
		cadence:     retryEvery,
		done:        make(chan struct{}),
	}
	b.device.Store(&sound.Device{Name: string(otosound.Name)})
	return b
}

// Voices sizes everything the Mixer and the handoff need, once, and then opens
// the Device. Sizing first is deliberate: the Mixer must exist complete before
// anything can pull a block out of it, and sound calls this before any Emit.
func (b *backend) Voices(n int) {
	if n <= 0 {
		n = 1
	}
	b.ring = newRing(n)
	b.mixer = newMixer(b.ring, n)
	b.streams = make([]*stream, n)
	b.open()
}

// Emit stages one tick's operations, in the order the seam states them, and
// publishes them as one batch. The batch it is handed is owned by sound and
// valid only for the duration of the call, so everything is copied out of it
// here, with each ClipID resolved to the Clip it names while the table is still
// the tick's own.
func (b *backend) Emit(batch *sound.Batch) {
	if b.ring == nil {
		return
	}
	// A staging batch that is already carrying operations means the ring was
	// full last tick, so this tick is being merged into it rather than
	// published beside it - and merging is the only place an update coalesces.
	coalesce := b.ring.merging()

	// The stops go first. sound stops a slot before it reuses one, and a steal
	// puts both in one batch: the victim's stop and the start that takes its
	// slot over. Recorded the other way round, the stop would arrive at the
	// Mixer after the start and fade out the Voice that had just replaced it.
	for _, slot := range batch.Stops {
		b.ring.record(op{kind: opStop, slot: slot}, coalesce)
		b.haltReadAhead(slot)
	}
	for i := range batch.Starts {
		start := &batch.Starts[i]
		clip := b.clips[start.Clip]
		b.ring.record(op{
			kind:   opStart,
			slot:   start.Slot,
			id:     start.Clip,
			clip:   clip,
			ring:   b.openReadAhead(start.Slot, clip, start.Offset, start.Loop),
			offset: start.Offset,
			loop:   start.Loop,
			params: start.Params,
		}, coalesce)
	}
	for i := range batch.Updates {
		update := &batch.Updates[i]
		b.ring.record(op{kind: opUpdate, slot: update.Slot, params: update.Params}, coalesce)
	}
	for _, id := range batch.Destroys {
		b.mark(id)
	}
	for _, release := range b.destroyed {
		b.ring.record(op{kind: opDestroy, id: release.id}, coalesce)
		b.staged = append(b.staged, release)
	}
	// Cleared rather than merely truncated: a Clip pointer left in the spare
	// capacity of a slice keeps the samples alive, which is the whole thing
	// these three lists exist to let go of.
	clear(b.destroyed)
	b.destroyed = b.destroyed[:0]

	if seq, published := b.ring.publish(); published {
		for i := range b.staged {
			b.staged[i].seq = seq
			b.pending = append(b.pending, b.staged[i])
			b.staged[i] = pendingDestroy{}
		}
		b.staged = b.staged[:0]
	}
	b.reclaim()
}

// openReadAhead gives a start on a streamed Clip the ring its Voice will read,
// and reports nil for a resident one - which is the whole of the tier below the
// seam: everything else about the two is identical, and nothing above here
// knows there was a choice.
//
// It runs on the tick, and what it does there is a goroutine and one ring's
// worth of allocation. The 460 us of decoder open and the seek that follows are
// the goroutine's, which is what keeps a recovery affordable: sixty-four
// streamed Voices restated as starts on the tick the Device came back is
// sixty-four goroutines opening decoders in parallel, with the Mixer playing
// silence for each slot until its ring primes, rather than 29 ms of decoder
// opens on a thread that has 10 ms to fill a buffer.
//
// The stream it replaces is halted first. A slot is restarted either because
// sound stole it - a stop then a start, in that order - or because the Device
// came back and every live Voice was restated; both leave a read-ahead filling
// a ring nothing will read again.
func (b *backend) openReadAhead(slot sound.VoiceSlot, clip *clipData, offset time.Duration, loop bool) *pcmRing {
	if int(slot) < 0 || int(slot) >= len(b.streams) {
		return nil
	}
	b.haltReadAhead(slot)
	if clip == nil || !clip.streams() {
		return nil
	}
	opened := newStream(clip, offset, loop)
	b.streams[slot] = opened
	return opened.ring
}

// haltReadAhead stops the read-ahead on a slot, if it has one. It does not wait
// for the goroutine and it does not free the ring: the Mixer may still be
// ramping that Voice to silence out of frames the ring already holds, and the
// ring goes when the voice table drops it, which is the GC's business and never
// the device thread's.
func (b *backend) haltReadAhead(slot sound.VoiceSlot) {
	if int(slot) < 0 || int(slot) >= len(b.streams) {
		return
	}
	if running := b.streams[slot]; running != nil {
		running.halt()
		b.streams[slot] = nil
	}
}

// Device reports the device as it is now. It is a field read - one atomic load
// and a copy - which is what lets sound poll it every flush.
func (b *backend) Device() sound.Device { return *b.device.Load() }

// Prepare decodes a Clip off both the tick and the device thread. It answers
// done=false and spawns a goroutine, whose completion lands in a slice
// TakePrepared drains on the next flush.
//
// What it hands back is a []float32 behind a pointer, or the encoded bytes and
// a way to open decoders against them, and nothing else - which is the Port's
// garbage-collectability rule: a Clip released while its prepare is in flight
// has its completion dropped with nothing called, so anything needing explicit
// release would never be freed.
//
// Which of the two it is depends on the Clip's decoded size against
// DecodedClipLimit, and is decided in prepare, where the headers that make it
// decidable are read. It is the Adapter's business alone: both tiers report the
// same four facts, so sound never learns which it got and neither can a game.
func (b *backend) Prepare(token any, encoded assets.Blob) (sound.PreparedClip, bool, error) {
	rate, limit := b.rate, b.limit
	go func() {
		clip, err := prepare(encoded, rate, limit)
		b.completedMu.Lock()
		b.completed = append(b.completed, sound.Prepared{Token: token, Clip: preparedOrNil(clip), Err: err})
		b.completedMu.Unlock()
	}()
	return nil, false, nil
}

// preparedOrNil keeps a failed decode from handing sound a typed nil, which is
// a non-nil PreparedClip interface holding nothing and would be installed
// rather than reported.
func preparedOrNil(clip *clipData) sound.PreparedClip {
	if clip == nil {
		return nil
	}
	return clip
}

// TakePrepared returns the prepares that finished since the last call. It is
// the only poll that crosses back from a goroutine, and it crosses under a lock
// held nowhere near the Mixer.
func (b *backend) TakePrepared() []sound.Prepared {
	b.completedMu.Lock()
	defer b.completedMu.Unlock()
	if len(b.completed) == 0 {
		return nil
	}
	taken := b.completed
	b.completed = nil
	return taken
}

// Install mints the id sound names the Clip by afterwards. It is Install rather
// than Prepare that mints, so the handle comes into existence on sound's tick,
// where a Destroy is guaranteed to pair with it.
func (b *backend) Install(prepared sound.PreparedClip) (sound.ClipID, error) {
	clip, ok := prepared.(*clipData)
	if !ok || clip == nil {
		return 0, errNotOurClip
	}
	b.lastID++
	b.clips[b.lastID] = clip
	return b.lastID, nil
}

// mark moves a Clip out of the table and onto the list of releases this Emit
// records, after that tick's stops - because a destroy ordered before the stop
// of a Voice reading the Clip would be a destroy the Mixer had not finished
// with. Marking one twice is a no-op, so a batch naming a Clip twice is one
// release and not two.
func (b *backend) mark(id sound.ClipID) {
	clip, ok := b.clips[id]
	if !ok {
		return
	}
	delete(b.clips, id)
	b.destroyed = append(b.destroyed, pendingDestroy{id: id, clip: clip})
}

// reclaim drops the last reference to every Clip whose destroy the Mixer has
// applied. A Clip whose batch has not been applied yet is simply freed a tick
// or two later, which is the price of the tick never waiting.
//
// Until a Mixer has pulled its first block there is no device thread to be
// mid-copy out of anything, so a release is free immediately. That is what
// keeps otosound behaving as nosound does under a Device that never opened,
// rather than retaining every Clip a game ever released.
//
// A Device that was pulling and then was lost goes back to exactly that state:
// the device goroutine detaches the ring once the player has stopped reading,
// and the applied counter - which has stopped moving and will not move again
// until a device returns - stops being what a release waits for. Without it,
// every release after a loss is held for as long as the loss lasts, which is
// unbounded in time.
func (b *backend) reclaim() {
	if len(b.pending) == 0 {
		return
	}
	if !b.ring.pulled.Load() {
		clear(b.pending)
		b.pending = b.pending[:0]
		return
	}
	applied := b.ring.applied()
	kept := b.pending[:0]
	for _, release := range b.pending {
		if release.seq < applied {
			continue
		}
		kept = append(kept, release)
	}
	clear(b.pending[len(kept):])
	b.pending = kept
}
