//go:build !js

package otosound

import "sync/atomic"

// handoffRing carries each flush's changes to the mixer: a fixed set of change
// lists, made once at Voices, published with one atomic store and drained
// whole at a block start. It is wait-free on both sides, since the flush must
// never wait on the device thread and the mixer must never wait on the flush.
//
// It is not a latest-wins buffer, the way a renderer publishes frames. A frame
// is a complete description and may be replaced; a flush's changes are a
// delta. Two flushes landing between two blocks, the first starting a voice
// and the second updating another, would publish the second over the first,
// and the voice would never sound with nothing reporting it.
//
// The synchronization is two monotonic sequence numbers and nothing else.
// published is stored only by the flush side, once the change list at
// published % depth is complete; applied only by the mixer, once every change
// list below it has been applied whole. applied is therefore also the counter
// the flush side lets destroyed clips go behind.
type handoffRing struct {
	// slots are the published change lists, RingDepth of them.
	slots []*changeList
	// staging is the flush side's own change list, which the mixer never
	// reads: each Emit records into it, and a publish swaps it into a slot.
	staging *changeList

	published atomic.Uint64
	applied   atomic.Uint64

	// pulled is whether a player pulls the ring: set by the device goroutine
	// once this kernel's player plays, and cleared by it once a lost device's
	// player has stopped; nothing else writes it. While it is not set, no
	// device thread exists to be mid-copy out of anything, so Emit publishes
	// nothing and lets every destroyed clip go at once, and a ring nobody
	// drains holds nothing: sfx restates every live voice as a start when the
	// device turns ready, so nothing is lost.
	pulled atomic.Bool
}

// newHandoffRing returns an empty ring of depth slots, each change list sized
// for voiceCount slots, as is the staging one.
func newHandoffRing(depth, voiceCount int) *handoffRing {
	ring := &handoffRing{
		slots:   make([]*changeList, depth),
		staging: newChangeList(voiceCount),
	}
	for index := range ring.slots {
		ring.slots[index] = newChangeList(voiceCount)
	}
	return ring
}

// publish hands the staging changes to the mixer, unless they are empty. A
// full ring, a device thread that has not run for RingDepth flushes,
// publishes nothing: the staging changes stay, and the next Emit merges into
// them, so a stall costs latency and never tears or drops an operation.
//
// Rejected: dropping the oldest changes, which loses exactly the starts sfx's
// contract keeps; blocking until the mixer drains, which makes a dead device
// stall the game; a seqlock, since a retry in a device callback is a spin
// bounded by the writer's schedule.
//
// The publish is a pointer swap rather than a copy: the slot it takes over,
// which the mixer has passed, becomes the next staging changes, keeping its
// capacity, so a warm handoff allocates nothing. Those are cleared before
// they are truncated: a clip left in the spare capacity would keep its
// samples alive after the flush side has let it go.
func (r *handoffRing) publish() {
	sequence := r.published.Load()
	depth := uint64(len(r.slots))
	if len(r.staging.operations) == 0 || sequence-r.applied.Load() >= depth {
		return
	}
	slot := sequence % depth
	published := r.staging
	r.staging = r.slots[slot]
	r.slots[slot] = published
	clear(r.staging.operations)
	r.staging.operations = r.staging.operations[:0]
	r.published.Store(sequence + 1)
}

// drain is the mixer's half, run on the device thread at a block start: it
// calls apply on every operation of every change list published since the
// last drain, in order, then stores the applied counter once. Several
// flushes' changes landing in one block are applied in order, not coalesced:
// two flushes' operations taking effect at one instant is correct, since they
// were ordered and stay ordered. It touches the ring through its atomics
// alone, and allocates nothing.
func (r *handoffRing) drain(apply func(*operation)) {
	published := r.published.Load()
	applied := r.applied.Load()
	if applied == published {
		// Most blocks find nothing new, and skip the store on the line the
		// flush side reads.
		return
	}
	depth := uint64(len(r.slots))
	for ; applied < published; applied++ {
		changes := r.slots[applied%depth]
		for index := range changes.operations {
			apply(&changes.operations[index])
		}
	}
	r.applied.Store(applied)
}

// empty drops every published change list the mixer has not applied, clearing
// each so it keeps no clip alive, and stores the applied counter past them.
// It takes the mixer's place, and is called only on the device goroutine
// while no player reads the mixer.
func (r *handoffRing) empty() {
	published := r.published.Load()
	depth := uint64(len(r.slots))
	for applied := r.applied.Load(); applied < published; applied++ {
		changes := r.slots[applied%depth]
		clear(changes.operations)
		changes.operations = changes.operations[:0]
	}
	r.applied.Store(published)
}
