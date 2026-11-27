package sfx

import (
	"cmp"
	"math"
	"slices"
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
		generations:       make([]uint32, maxVoices),
		freeSlots:         freeSlots,
		stealOrder:        make([]stealCandidate, 0, maxVoices),
		busVolumeSets:     make([]m.Maybe[float32], maxBuses),
		rankingBusVolumes: make([]float32, maxBuses),
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
// flush or more after the play, never in the flush that applies it.
//
// At a full table the play steals: the voice with the lowest Priority, and
// within it the lowest audibility, ties going to the oldest, ends with
// ReasonStolen in the flush that applies the play, which takes its slot, and
// is never resumed. The play is as stealable as any voice and ranks as the
// newest: one strictly below every live voice is the one that loses, still
// getting a real handle, its voice ending with ReasonStolen in the flush that
// applies it, never having been live. The victim is chosen here, as the play
// is recorded, against the table as the last flush left it: a play steals
// only a voice live at the end of the last flush, never one a play before it
// in the same flush started, and every candidate, the play included, is
// ranked against the bus volumes and the listener of the last flush. The
// play's own rank is read from options here, so a later operation on its
// voice before the flush does not change what it stole.
func (q queue) Play(path string, offset time.Duration, options VoiceOptions) Voice {
	bus := options.Bus.Or(Master).resolve(len(q.state.busVolumeSets))
	voice := q.mintVoice(options, bus)
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
// VoiceParams.Paused update of every live voice whose pause it changes.
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

// SetListener records where the game is heard from: each field options
// states moves the listener, and an absent one leaves it as it is. It is
// coalesced field by field, the last value recorded before a flush winning,
// so a job that moves the listener and one that turns it do not overwrite
// each other. Every positional voice the listener moved relative to is
// restated to the backend at that flush, with no operation naming it.
func (q queue) SetListener(options ListenerOptions) {
	q.state.listener = q.state.listener.merge(options)
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

// mintVoice answers the handle of a play with options on bus: a free slot's
// next generation; at a full table, the next generation of the slot of the
// voice the play outranks, which goes on holding the victim's whole handle,
// so every operation recorded against the victim before the flush still finds
// it, applied in order before the start that takes the slot over; or, when
// the play outranks nothing, a handle of the index past the last slot, which
// addresses nothing.
func (q queue) mintVoice(options VoiceOptions, bus Bus) Voice {
	state := q.state
	if count := len(state.freeSlots); count > 0 {
		index := state.freeSlots[count-1]
		state.freeSlots = state.freeSlots[:count-1]
		state.generations[index]++
		return Voice(uint64(state.generations[index])<<32 | uint64(index))
	}
	if count := len(state.stealOrder); count > 0 {
		audibility, _ := options.computeAudibility(state.rankingListenerAt, state.rankingBusVolumes[bus])
		incoming := voiceRank{priority: options.Priority.Or(0), audibility: audibility, startNumber: math.MaxUint64}
		if victim := state.stealOrder[count-1]; victim.rank.compare(incoming) < 0 {
			state.stealOrder = state.stealOrder[:count-1]
			state.generations[victim.index]++
			return Voice(uint64(state.generations[victim.index])<<32 | uint64(victim.index))
		}
	}
	state.unslottedCount++
	return Voice(uint64(state.unslottedCount)<<32 | uint64(len(state.generations)))
}

// rankSlots restates what the next flush's plays are minted against, once
// the flush's changes are handed over, so a slot is stopped before sfx reuses
// it: the free slots, the last first, so popping from the end hands out slot
// 0 first; every live voice in the steal order, the one stolen next last;
// and the ranking context, the bus volumes and the listener's position a play
// arriving is ranked against. It is copied here because a recorder holds the
// queue's lock and nothing else. One pass restates both lists, so a slot is
// free or stealable, never both and never neither.
func (q queue) rankSlots(voices liveVoices, buses busVolumes, heardFrom listener) {
	state := q.state
	state.freeSlots = state.freeSlots[:0]
	state.stealOrder = state.stealOrder[:0]
	for index := len(voices.table.slots) - 1; index >= 0; index-- {
		switch slot := &voices.table.slots[index]; {
		case slot.state == slotEmpty:
			state.freeSlots = append(state.freeSlots, uint32(index))
		case slot.state.live():
			state.stealOrder = append(state.stealOrder, stealCandidate{
				index: uint32(index),
				rank:  voiceRank{priority: slot.options.Priority.Or(0), audibility: slot.audibility, startNumber: slot.startNumber},
			})
		}
	}
	slices.SortFunc(state.stealOrder, func(a, b stealCandidate) int { return b.rank.compare(a.rank) })
	copy(state.rankingBusVolumes, buses.volumes)
	state.rankingListenerAt = heardFrom.position
}

// queueState is the queue's own state.
type queueState struct {
	// operations are the operations recorded since the last flush, in order.
	operations []operation
	// paused is the pause SetPaused recorded last since the last flush,
	// absent when it recorded none.
	paused m.Maybe[bool]
	// listener is every SetListener recorded since the last flush, merged.
	listener ListenerOptions
	// generations is each slot's current generation, starting at 0 so the
	// first handle a slot hands out is generation 1.
	generations []uint32
	// freeSlots are the slots no voice holds, popped from the end so slot 0
	// goes first.
	freeSlots []uint32
	// stealOrder is the slots of the voices live at the end of the last
	// flush, the one stolen next last, so it is popped from the end as a
	// free slot is, each at most once before the next flush.
	stealOrder []stealCandidate
	// rankingBusVolumes and rankingListenerAt are the ranking context, each
	// bus's volume and the listener's position as of the end of the last
	// flush, which a play arriving is ranked against.
	rankingBusVolumes []float32
	rankingListenerAt m.Vec3
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

// voiceRank is the key stealing orders voices by: the lowest priority loses,
// then the lowest audibility, ties broken by age, oldest first. Priority is
// a band and not a weight, which is why it is compared first rather than
// folded into audibility: a weight would let a loud enough crowd of
// footsteps outvote the music.
type voiceRank struct {
	priority   int
	audibility float32
	// startNumber is the voice's place in the order the table started
	// voices in, and a play arriving ranks as the newest of all.
	startNumber uint64
}

// compare answers below zero when r is stolen before other, above zero when
// other is, and zero only for one voice: startNumber is unique, which makes
// the tie contract rather than whatever order the table was walked in.
func (r voiceRank) compare(other voiceRank) int {
	return cmp.Or(
		cmp.Compare(r.priority, other.priority),
		cmp.Compare(r.audibility, other.audibility),
		cmp.Compare(r.startNumber, other.startNumber),
	)
}

// stealCandidate is one live voice's slot as the steal order holds it.
type stealCandidate struct {
	index uint32
	rank  voiceRank
}
