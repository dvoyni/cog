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
// path, and shared by every voice playing it after. A voice whose clip is not
// ready yet waits on it, addressable and stoppable, and starts from offset
// once it is, with no catch-up. A clip that cannot be read or prepared is
// reported once under its path and ends its voices with ReasonFailed, a
// flush or more after the play, never in the flush that applies it. A play
// that finds every slot taken still gets a real handle, and its voice ends
// with ReasonStolen in the flush that applies it, never having been live.
func (q queue) Play(path string, offset time.Duration, options VoiceOptions) Voice {
	voice := q.mintVoice()
	bus := options.Bus.Or(Master).resolve(len(q.state.busVolumeSets))
	q.state.operations = append(q.state.operations, operation{kind: operationPlay, voice: voice, path: path, offset: offset, options: options, bus: bus})
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

// Preload records a read and a prepare of the clip at path without playing
// it, so a loading screen pays the read rather than the first shot fired.
// The read happens inside the flush that applies it, as a Play's does.
//
// It promises residency, not a free Play: a short clip's next Play is free,
// while a long one streams, and its first Play still opens a decoder of its
// own and is silent until it primes, which a game cannot tell from the
// other. Preloading a clip already loaded, loading or playing changes
// nothing: a Play and a Preload of one path name one clip and cost one
// prepare. ClipTable.Info answers once the clip is ready.
func (q queue) Preload(path string) {
	q.state.operations = append(q.state.operations, operation{kind: operationPreload, path: path})
}

// Release records the release of the clip at path, for a game that wants its
// memory back. At the flush that applies it, every voice playing the clip
// ends with ReasonReleased, a voice still waiting on it included, and the
// backend is told to destroy the clip after their stops, in that flush's
// changes: after that flush no voice plays the clip, on any backend. It also
// forgets the clip's failure, which only a release clears: a path that
// failed and is named again is read again, and reported again if it fails
// again.
//
// It is ordered: a Play of path recorded before it is cut by it, and one
// recorded after it reads the clip again. Releasing a path nothing has named
// does nothing. Releasing is optional, so a game that wants a sound to
// finish simply does not release its clip.
func (q queue) Release(path string) {
	q.state.operations = append(q.state.operations, operation{kind: operationRelease, path: path})
}

// ReleaseAll records the release of every clip, as Release records each: at
// the flush that applies it every voice ends with ReasonReleased, which is
// what a teardown means. A game that wants one track to bridge a transition
// releases clip by clip and keeps that one, or starts the bridge after
// ReleaseAll; naming a clip again reads it again.
func (q queue) ReleaseAll() {
	q.state.operations = append(q.state.operations, operation{kind: operationReleaseAll})
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
	// operationPreload reads and prepares a clip nothing plays yet.
	operationPreload
	// operationRelease ends every voice on a clip and destroys it.
	operationRelease
	// operationReleaseAll releases every clip.
	operationReleaseAll
)

// operation is one recorded operation. An operation is a delta and may not be
// dropped, so the queue keeps every one, in order, until the flush applies
// it.
type operation struct {
	kind  operationKind
	voice Voice
	// path is the clip a play, a preload or a release names, and offset and
	// options are where in it a play's voice begins and how it starts.
	path    string
	offset  time.Duration
	options VoiceOptions
	// bus is the resolved bus a play's voice plays on, or a StopBus stops.
	bus Bus
}
