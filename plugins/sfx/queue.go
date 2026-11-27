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
// free, and maxBuses buses.
func makeQueue(maxVoices, maxBuses int) queue {
	freeSlots := make([]uint32, maxVoices)
	for i := range freeSlots {
		freeSlots[i] = uint32(maxVoices - 1 - i)
	}
	return queue{state: &queueState{
		generations:   make([]uint32, maxVoices),
		freeSlots:     freeSlots,
		busVolumeSets: make([]m.Maybe[float32], maxBuses),
	}}
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
	bus := options.Bus.Or(Master).resolve(len(q.state.busVolumeSets))
	q.state.operations = append(q.state.operations, operation{kind: operationPlay, voice: voice, path: path, offset: offset, bus: bus})
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

// SetBus records bus's linear volume, 1 being unity, which the flush folds
// into the gains of every voice on it. It is coalesced, the last value
// recorded before a flush winning, since every voice on the bus reads it at
// the one moment the flush computes: a slider dragged through a hundred
// values before one flush costs one. A bus outside Config.MaxBuses is
// Master.
func (q queue) SetBus(bus Bus, volume float32) {
	q.state.busVolumeSets[bus.resolve(len(q.state.busVolumeSets))] = m.Some(volume)
}

// StopBus records the end of every voice on bus, each ending with
// ReasonStopped, as a Stop naming it would; Master stops every voice. It is
// ordered rather than coalesced: a play recorded before it is stopped, and
// one recorded after it is not. A bus outside Config.MaxBuses is Master.
func (q queue) StopBus(bus Bus) {
	q.state.operations = append(q.state.operations, operation{kind: operationStopBus, bus: bus.resolve(len(q.state.busVolumeSets))})
}

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
	// generations is each slot's current generation, starting at 0 so the
	// first handle a slot hands out is generation 1.
	generations []uint32
	// freeSlots are the slots no voice holds, popped from the end so slot 0
	// goes first.
	freeSlots []uint32
	// unslottedCount counts the handles minted at a full table, each the
	// generation of the next, so no two compare equal.
	unslottedCount uint32
	// busVolumeSets is each bus's volume SetBus recorded since the last
	// flush, the last value winning, indexed by a resolved bus, and absent
	// for a bus no SetBus named.
	busVolumeSets []m.Maybe[float32]
}

// operationKind is what one recorded operation does.
type operationKind uint8

const (
	// operationPlay begins a voice.
	operationPlay operationKind = iota
	// operationStop ends one.
	operationStop
	// operationStopBus ends every voice on a bus.
	operationStopBus
)

// operation is one recorded operation. An operation is a delta and may not be
// dropped, so the queue keeps every one, in order, until the flush applies
// it.
type operation struct {
	kind  operationKind
	voice Voice
	// path and offset are a play's clip and where in it the voice begins.
	path   string
	offset time.Duration
	// bus is the resolved bus a play's voice plays on, or a StopBus stops.
	bus Bus
}
