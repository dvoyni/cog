package sfx

import (
	"time"

	"github.com/dvoyni/cog/libs/m"
)

// queue is what Queue names: the operations recorded since the last flush,
// in order, and the handles minted for them. It holds one private pointer,
// so Get copies a word.
type queue struct{ state *queueState }

// makeQueue answers an empty queue over a table of maxVoices slots, every one
// free.
func makeQueue(maxVoices int) queue {
	freeSlots := make([]uint32, maxVoices)
	for i := range freeSlots {
		freeSlots[i] = uint32(maxVoices - 1 - i)
	}
	return queue{state: &queueState{generations: make([]uint32, maxVoices), freeSlots: freeSlots}}
}

// Play records a play of the clip at path, from offset into it, and answers
// the voice's handle. The handle is minted here, so it addresses the voice in
// the flush that applies this play, before anyone could ask about it; the
// voice exists from that flush, silent until its clip is ready.
//
// The clip is read through platform.FS at the flush that first names its
// path, and shared by every voice playing it after. A play that finds every
// slot taken still gets a real handle, and its voice ends with ReasonStolen
// in the flush that applies it, never having been live.
func (q queue) Play(path string, offset time.Duration, options VoiceOptions) Voice {
	voice := q.mintVoice()
	q.state.operations = append(q.state.operations, operation{kind: operationPlay, voice: voice, path: path, offset: offset, options: options})
	return voice
}

// Stop records the end of voice, which ends with ReasonStopped at the flush
// that applies it. It is a no-op on a voice that no longer exists, and on
// NoVoice, with no error to check: a stop racing a clip that finished a flush
// ago is the most common race in audio code.
func (q queue) Stop(voice Voice) {
	if voice == NoVoice {
		return
	}
	q.state.operations = append(q.state.operations, operation{kind: operationStop, voice: voice})
}

// SetPaused records a pause of every voice at once, as when a menu is shown
// over the game, or its end, with false. A pause suspends every live voice,
// each resuming on the same sample it stopped on; a voice played during it
// starts suspended; Stop still applies. The device stays open and is fed
// silence. A voice paused by its own VoiceOptions.Paused stays paused when
// the pause ends: a voice is silent while either is set.
//
// It is coalesced, not ordered: set twice before one flush, the last value
// wins, applied where the flush's ordered operations end, so the flush that
// applies a pause advances no playhead. It reaches the backend as a
// VoiceParams.Paused update of every live voice.
func (q queue) SetPaused(paused bool) { q.state.paused = m.Some(paused) }

// mintVoice answers the next handle: a free slot's next generation, or, at a
// full table, a handle of the index past the last slot, which addresses
// nothing.
func (q queue) mintVoice() Voice {
	state := q.state
	if count := len(state.freeSlots); count > 0 {
		index := state.freeSlots[count-1]
		state.freeSlots = state.freeSlots[:count-1]
		state.generations[index]++
		return Voice(uint64(state.generations[index])<<32 | uint64(index))
	}
	state.unslottedCount++
	return Voice(uint64(state.unslottedCount)<<32 | uint64(len(state.generations)))
}

// queueState is the queue's own state.
type queueState struct {
	// operations are the operations recorded since the last flush, in order.
	operations []operation
	// paused is the pause SetPaused recorded last since the last flush,
	// absent when it recorded none.
	paused m.Maybe[bool]
	// generations is each slot's current generation, starting at 0 so the
	// first handle a slot hands out is generation 1.
	generations []uint32
	// freeSlots are the slots no voice holds, popped from the end so slot 0
	// goes first.
	freeSlots []uint32
	// unslottedCount counts the handles minted at a full table, each the
	// generation of the next, so no two compare equal.
	unslottedCount uint32
}

// operationKind is what one recorded operation does.
type operationKind uint8

const (
	// operationPlay begins a voice.
	operationPlay operationKind = iota
	// operationStop ends one.
	operationStop
)

// operation is one recorded operation. An operation is a delta and may not be
// dropped, so the queue keeps every one, in order, until the flush applies
// it.
type operation struct {
	kind  operationKind
	voice Voice
	// path, offset and options are a play's clip, where in it the voice
	// begins and how it starts.
	path    string
	offset  time.Duration
	options VoiceOptions
}
