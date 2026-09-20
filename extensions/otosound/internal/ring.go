//go:build !js

package internal

import "sync/atomic"

// ringDepth is how many published batches the ring holds: about 130ms at 60Hz.
// Deep enough that an ordinary scheduling hiccup on either side is invisible,
// shallow enough that the whole ring is a fixed allocation made once at
// Voices(n). Eight is a constant until something asks otherwise.
const ringDepth = 8

// ring is the handoff: a fixed set of preallocated batches, published with one
// atomic store and drained whole at a block boundary. Wait-free on both sides,
// because the tick must never wait on a device thread and the Mixer must never
// wait on the tick.
//
// It is deliberately not gfx's latest-wins triple buffer. A frame is a complete
// description and may be replaced; a batch is a delta. The failing sequence is
// two ticks landing between two blocks: tick N starts a Voice, tick N+1 updates
// a different one, latest-wins publishes N+1 over N, and the Voice never sounds
// with nothing reporting it.
//
// The synchronisation is two monotonic sequence numbers and nothing else. write
// is stored only by the tick, after the batch at write%ringDepth is complete;
// read is stored only by the Mixer, after every batch below it has been applied
// whole. read is therefore also the applied counter the tick frees behind.
type ring struct {
	slots   [ringDepth]*batch
	staging *batch

	write atomic.Uint64
	read  atomic.Uint64

	// pulled records that a Mixer has run at least once. Until one has, no
	// device thread exists to be mid-copy out of anything, so the tick may free
	// a destroyed Clip immediately instead of holding it against a counter that
	// will never move. Under a Device that never opened, that is the whole
	// difference between otosound behaving as nosound does and otosound
	// retaining every Clip a game releases.
	pulled atomic.Bool
}

func newRing(maxVoices int) *ring {
	r := &ring{staging: newBatch(maxVoices)}
	for i := range r.slots {
		r.slots[i] = newBatch(maxVoices)
	}
	return r
}

// record stages one operation. It is the tick side, and it never blocks.
func (r *ring) record(o op, coalesce bool) { r.staging.record(o, coalesce) }

// publish hands the staging batch to the Mixer and reports the sequence it went
// out as. A full ring - a device thread that has not run for ringDepth ticks,
// which is a stalled or dying device whose Ready is very likely already false -
// publishes nothing and leaves the staging batch to keep accumulating.
//
// Rejected: dropping the oldest batch, which loses exactly the starts the
// contract promised to keep. Rejected: blocking until the Mixer drains, which
// makes a dead device stall the game. Rejected: a seqlock, because a retry in a
// device callback is a spin whose bound is the writer's schedule.
//
// The publish is a pointer swap rather than a copy: the slot being released
// becomes the next staging batch, keeping its capacity, so the handoff costs
// one store and allocates nothing.
func (r *ring) publish() (uint64, bool) {
	write, read := r.write.Load(), r.read.Load()
	if write-read >= ringDepth {
		return 0, false
	}
	slot := write % ringDepth
	r.slots[slot], r.staging = r.staging, r.slots[slot]
	r.staging.reset()
	r.write.Store(write + 1)
	return write, true
}

// merging reports whether the staging batch is already carrying operations a
// full ring would not take, which is the only condition under which an update
// coalesces.
func (r *ring) merging() bool { return len(r.staging.ops) > 0 }

// applied reports the sequence the Mixer has applied every batch below. The
// tick reads it to decide what it may free: a Clip destroyed in batch N is
// unreferenced by the voice table once N has been applied, because the stops
// ordered before the destroy were applied with it.
func (r *ring) applied() uint64 { return r.read.Load() }

// pending reports how many published batches the Mixer has not yet drained. It
// exists for tests and for a Device readout; nothing in the mix reads it.
func (r *ring) pending() int { return int(r.write.Load() - r.read.Load()) }
