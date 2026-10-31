package internal

import (
	"slices"

	"github.com/dvoyni/cog/libs/m"
)

// OpKind names what one recorded operation does.
type OpKind uint8

const (
	// OpPlay begins a Voice on a Clip. The handle it hands back was minted when
	// the play was recorded.
	OpPlay OpKind = iota
	// OpStop ends a Voice.
	OpStop
	// OpSetVoice restates a Voice's Params.
	OpSetVoice
	// OpStopBus ends every Voice on a Bus. It is recorded in order rather than
	// coalesced away like a Bus volume, because unlike a volume it interacts
	// with the plays around it: a Play recorded before it is stopped and one
	// recorded after it is not.
	OpStopBus
	// OpSeek moves a Voice's playhead. It is its own operation and not a field
	// of Params, because "apply in order" and "last value wins" are the same
	// thing for a parameter and are not for a cursor move: a Seek recorded
	// before a Stop and a Seek recorded after one are different ticks.
	OpSeek
	// OpPreload reads and prepares a Clip nothing is playing yet.
	OpPreload
	// OpRelease drops a Clip, stopping the Voices on it. It is ordered rather
	// than coalesced for the reason OpStopBus is: it interacts with the plays
	// around it, and a Play recorded after a Release of the same Clip reloads
	// it while one recorded before it is cut.
	OpRelease
	// OpReleaseAll drops every Clip and cuts every Voice.
	OpReleaseAll
)

// Operation is one recorded operation, kept in the order it was recorded. An
// operation is a delta and may not be dropped, which is why the Queue is not
// gfx's latest-wins triple buffer: a frame is a complete description and may be
// replaced, this may not.
type Operation struct {
	Kind   OpKind
	Voice  Voice
	Bus    Bus
	Clip   ClipRef
	Offset float32
	Params Params
}

// rank is the key stealing orders Voices by: the lowest (priority, audibility)
// loses, ties broken by age, oldest first.
//
// Priority is a band and not a weight, which is the whole of why it is a
// separate field compared first rather than a factor folded into audibility: a
// lower-priority Voice always loses to a higher-priority one whatever the gains
// say, and a weight would let a loud enough crowd of footsteps outvote music.
type rank struct {
	priority   int
	audibility float32
	born       uint64
}

// below reports whether a is stolen before b. It is a total order, because born
// is unique, which is what makes the tie a contract rather than a symptom of
// whatever order the table happened to be walked in.
func (a rank) below(b rank) bool {
	switch {
	case a.priority != b.priority:
		return a.priority < b.priority
	case a.audibility != b.audibility:
		return a.audibility < b.audibility
	default:
		return a.born < b.born
	}
}

// candidate is one occupied slot as the steal order holds it.
type candidate struct {
	index uint32
	key   rank
}

// minter hands out Voice handles at the moment a play is recorded, so a handle
// is usable in the same tick that recorded it and there is no command to wait
// on. It owns the generations, and the live table owns nothing but the handle
// it was given, so the two can never disagree about whether a handle is stale.
//
// The table is fixed and generational, so no free list beyond the slots
// themselves and no map from handle to Voice is needed: the handle is the index.
//
// # Where stealing is resolved, and why it is here
//
// The spec asserts three things that cannot all hold at a full table: a handle
// is minted when the play is recorded, the handle is the index, and stealing
// resolves in the compute phase. The minter has no slot to hand out at record
// time, and compute has not run yet.
//
// The one that bends is the third. The other two are visible to a game - it
// holds the handle, and every verb resolves through it - while "stealing
// resolves in compute" is visible to nobody: what a game can observe is that
// the victim is in the view until the flush that steals it, and that is true
// either way. And the alternative bends what a game can see: deferring the mint
// would mean a play that might lose hands back a handle that addresses nothing
// until the next tick, so Play would have two return shapes and a Stop recorded
// beside it would silently miss.
//
// So the victim is chosen here, when the play is recorded, against the table as
// it stood at the end of the last flush - which is the same table every other
// question a recorder asks is answered from, because a Bus volume and the
// Listener are also read as of the last flush. Two consequences are stated
// rather than left implied:
//
//   - A play can only steal a Voice that was live at the end of the last flush.
//     A play cannot steal a Voice started earlier in its own tick, so a hundred
//     plays in one frame consume at most the slots that existed, and the rest
//     lose outright.
//   - The ranking context is one moment. Every candidate, the incoming play
//     included, is ranked against the same Bus volumes and the same Listener,
//     so no two of them are compared across a frame boundary.
type minter struct {
	// generations is each slot's current generation, one per slot, starting at
	// 0 so the first mint hands out generation 1.
	generations []uint32
	// free is the slots nothing holds, popped from the end so slot 0 goes first
	// and a test reads the handles it expects.
	free []uint32
	// order is the occupied slots, best first so the one stolen next is popped
	// from the end the way a free slot is. It is rebuilt once per flush and
	// consumed by the steals a tick records.
	order []candidate
	// busVolumes and listenerAt are the ranking context: what an incoming
	// play's audibility is computed against, as of the end of the last flush.
	// They are copied rather than reached for because a recorder holds the
	// Queue's write lock and nothing else - a Play that had to read the Buses
	// and the Listener would widen every recorder's lock set to do it.
	busVolumes [MaxBuses]float32
	listenerAt m.Vec3
	// unslotted counts the handles minted for plays that found no slot at all.
	// They carry index len(generations), which addresses nothing, and their
	// Voices end with ReasonStolen in the flush that recorded them - the
	// incoming play being the one that loses.
	unslotted uint32
}

func newMinter(maxVoices int) minter {
	free := make([]uint32, maxVoices)
	for i := range free {
		free[i] = uint32(maxVoices - 1 - i)
	}
	return minter{generations: make([]uint32, maxVoices), free: free}
}

// mint hands out the next handle: a free slot when there is one, otherwise the
// slot of the Voice this play outranks, otherwise a handle that addresses
// nothing because this play is the one that lost.
//
// Bumping the generation of a stolen slot does not disturb the Voice still
// sitting in it. The live table compares the whole handle, and the slot still
// holds the victim's, so every operation the rest of this tick records against
// the victim still finds it - and is applied in order, before the start that
// takes the slot over.
func (m *minter) mint(params Params) Voice {
	if n := len(m.free); n > 0 {
		index := m.free[n-1]
		m.free = m.free[:n-1]
		return m.handle(index)
	}
	if n := len(m.order); n > 0 && m.order[n-1].key.below(m.incoming(params)) {
		index := m.order[n-1].index
		m.order = m.order[:n-1]
		return m.handle(index)
	}
	m.unslotted++
	return newVoice(uint32(len(m.generations)), m.unslotted)
}

// handle mints the next generation of one slot.
func (m *minter) handle(index uint32) Voice {
	m.generations[index]++
	return newVoice(index, m.generations[index])
}

// incoming is the rank of a play that has no slot yet. Its age is the largest
// there is, because it is the youngest thing in the comparison: an incoming
// play loses only when it is strictly below every Voice on the table, never on
// a tie, which is the other half of "ties broken by age, oldest first".
func (m *minter) incoming(params Params) rank {
	busGain := m.busVolumes[params.Bus.Or(Master).resolve()]
	return rank{
		priority:   params.Priority.Or(0),
		audibility: params.audibility(busGain, m.listenerAt),
		born:       ^uint64(0),
	}
}

// rebuild restates the free list, the steal order and the ranking context from
// the table. sound calls it once per flush, after the batch carrying this
// tick's stops has been handed over, which is what "a slot is stopped before
// sound reuses it" costs.
//
// It rebuilds both lists in one pass rather than releasing a slot per ending,
// so the two can never disagree: a slot is free or it is stealable, never both
// and never neither. A steal that hands a victim's slot straight to the play
// that took it is exactly the case a per-ending release got wrong, because the
// ending names a Voice whose index is already somebody else's.
func (m *minter) rebuild(live *Voices, buses *Buses, listener *Listener) {
	m.free, m.order = m.free[:0], m.order[:0]
	for i := len(live.slots) - 1; i >= 0; i-- {
		if slot := &live.slots[i]; slot.state == slotLive {
			m.order = append(m.order, candidate{index: uint32(i), key: slot.rank()})
		} else {
			m.free = append(m.free, uint32(i))
		}
	}
	// Best first, so the next victim is the last entry. The order is total, so
	// which sort this is cannot change the answer.
	slices.SortFunc(m.order, func(a, b candidate) int {
		switch {
		case a.key.below(b.key):
			return 1
		case b.key.below(a.key):
			return -1
		default:
			return 0
		}
	})
	m.busVolumes, m.listenerAt = buses.volumes, listener.position
}

// Queue is where every operation is recorded, under a write lock, and it is
// flushed once per tick, atomically. It is a resource and not a channel: a
// recorder holds the lock, appends, and is done.
//
// A Voice's Params do not coalesce here. They are stated as "coalesced within
// a tick, last value wins", and applying them in the order they were recorded
// says exactly that - the last SetVoice a tick recorded for a Voice is the last
// one applied - while keeping Play and Stop in one list with them, which is
// what makes a Play followed by a SetVoice indistinguishable from a Play that
// carried the same Params.
//
// A Bus volume does coalesce, and the difference is real rather than a
// preference. A per-Voice param is a value nothing else in the list reads, so
// the two readings agree; a Bus volume is read by every Voice on that Bus at
// the end of the tick, so there is exactly one moment it can be read at and a
// position in the list would mean nothing. What does interact with the list is
// StopBus, which is why that one is an Operation and this one is a table.
type Queue struct {
	ops   []Operation
	slots minter
	// busVolumes is the tick's Bus volumes, last value winning, indexed by a
	// resolved Bus. The flush hands it to Buses and clears it.
	busVolumes [MaxBuses]m.Maybe[float32]
	// listener is the tick's Listener changes, merged field by field so that a
	// System that moves the Listener and one that turns it do not overwrite
	// each other. It coalesces for the same reason a Bus volume does: every
	// Positional Voice reads it at the one moment the tick ends, so a position
	// in the ordered list would mean nothing.
	listener ListenerParams
}

// NewQueue builds an empty queue over a table of maxVoices slots.
func NewQueue(maxVoices int) *Queue {
	return &Queue{slots: newMinter(maxVoices)}
}

// Play records a play and hands back its handle. The handle is minted here, so
// it is usable in the same tick that recorded it; the Voice itself exists from
// the flush that applies this operation, and is silent until its Clip is
// resident.
//
// offset is where to begin, in seconds. It returns no error: a play that finds
// no slot still gets a real handle, and its ending arrives in the same flush.
//
// At a full table the handle names the slot of the Voice this play outranks,
// and that Voice ends with ReasonStolen in the flush that applies this play.
// The Params are read here and not only in the flush because they carry the
// play's own rank - its Priority, its Volume, its Bus and where it is - and a
// play that is itself the quietest thing on the table is the one that loses.
//
// They stay one ordered list and are still applied in order: this reads them, it
// does not consume them. The one place a Play followed by a SetVoice is not
// indistinguishable from a Play that carried the same Params is here, at a full
// table, because the slot was handed out before the SetVoice was recorded. A
// game that wants a sound to survive the cap says so on the Play.
func (q *Queue) Play(clip ClipRef, offset float32, params Params) Voice {
	voice := q.slots.mint(params)
	q.ops = append(q.ops, Operation{Kind: OpPlay, Voice: voice, Clip: clip, Offset: offset, Params: params})
	return voice
}

// Stop records the end of a Voice. It is a no-op on a Voice that no longer
// exists, with no error and no response field to check: a Stop racing a Clip
// that finished a tick ago is the most common race in audio code.
func (q *Queue) Stop(voice Voice) {
	if voice == NoVoice {
		return
	}
	q.ops = append(q.ops, Operation{Kind: OpStop, Voice: voice})
}

// SetVoice records a change to a Voice's Params. An absent field is unchanged.
// It is a no-op on a Voice that no longer exists.
func (q *Queue) SetVoice(voice Voice, params Params) {
	if voice == NoVoice {
		return
	}
	q.ops = append(q.ops, Operation{Kind: OpSetVoice, Voice: voice, Params: params})
}

// SetBus records a Bus's volume, linear, 1 being unity. It is coalesced within
// the tick, last value winning, so a slider dragged through a hundred values in
// one tick costs one and the Voices on that Bus are re-emitted once.
//
// An out-of-range Bus is Master, the same rule a Play naming one follows.
func (q *Queue) SetBus(bus Bus, volume float32) {
	q.busVolumes[bus.resolve()] = m.Some(volume)
}

// StopBus records the end of every Voice on a Bus. Master stops everything,
// because every Bus is directly under it.
//
// The Voices it ends do so with ReasonStopped rather than a member of its own:
// StopBus is Stop over a set, splitting it later is additive, and a reader
// switching on ReasonStopped keeps working.
func (q *Queue) StopBus(bus Bus) {
	q.ops = append(q.ops, Operation{Kind: OpStopBus, Bus: bus.resolve()})
}

// Seek moves a Voice's playhead to offset, in seconds. A negative offset
// clamps to zero; an offset past the end ends a one-shot and wraps a looping
// Voice to its loop start.
//
// It is block-accurate and never sample-accurate. In our Mixer it is a cursor
// move, and in Web Audio it is a fresh start(when, offset) - promising
// sample-exactness here would foreclose that Adapter, and the playhead being
// tick-accurate is the same admission from the other side.
//
// It is a no-op on a Voice that no longer exists.
func (q *Queue) Seek(voice Voice, offset float32) {
	if voice == NoVoice {
		return
	}
	q.ops = append(q.ops, Operation{Kind: OpSeek, Voice: voice, Offset: offset})
}

// Preload records a Clip to be read and prepared without playing it, so that
// the frame which eats the read is a loading screen rather than the first shot
// fired. The read happens inside sound's flush, on the tick this was recorded.
//
// It promises that the Clip is resident and will not fail. It does not promise
// that the next Play is free, and it cannot: a Clip short enough to be decoded
// whole is free to play afterwards, while a longer one is streamed, so its
// first Play still opens a decoder of its own and is silent until that Voice's
// read-ahead primes. Which tier a Clip landed in is the Adapter's own business
// and a game cannot tell, so the guarantee that is always true is the weaker
// one - a guarantee the caller cannot verify and the engine cannot keep would
// be worse.
//
// It is idempotent in the sense that matters: preloading a Clip already loaded,
// already loading, or already playing changes nothing.
func (q *Queue) Preload(clip ClipRef) {
	q.ops = append(q.ops, Operation{Kind: OpPreload, Clip: clip})
}

// Release records the end of a Clip: its bytes are dropped and its samples are
// queued for destruction, and every Voice playing it is stopped, ending with
// ReasonReleased.
//
// Stopping is the side effect and not the verb. The deferral that would have
// waited for those Voices to end on their own is retired: under it a looping
// ambience's Voice never ends, so the release is never forwarded, the memory
// never comes back and nothing is reported. Releasing something still bound is
// the caller's mistake, which is the rule gfx, scene and canvas already live
// by, and a release that quietly does not release is worse than one that stops
// a sound.
//
// After the flush that applies it, no Voice is playing that Clip, on any
// Adapter. That is a guarantee and not a reported property: stopping a Voice
// needs no cooperation from a buffer's lifetime on either side of the seam.
//
// A streamed Clip is safe to release while it is playing, and it is safe for a
// reason worth stating rather than implying: the Voices go first, which halts
// their read-aheads, and the encoded bytes belong to the Library, so nothing
// the destroy frees is anything a decoder still holds.
//
// Releasing a Clip nothing has named does nothing. Naming it again afterwards
// reloads it, which is also the only thing that clears a Clip that failed.
func (q *Queue) Release(clip ClipRef) {
	q.ops = append(q.ops, Operation{Kind: OpRelease, Clip: clip})
}

// ReleaseAll records the end of every Clip, cutting every Voice, which is what
// a teardown call means. A game that wants one track to bridge a transition
// releases per Clip and keeps the bridging one, or lets this cut it and starts
// the bridge afterwards - asking again reloads.
//
// Every one of these three is optional. A game that never calls any of them
// never meets any of this, which is what makes the cheap, honest answer good
// enough.
func (q *Queue) ReleaseAll() {
	q.ops = append(q.ops, Operation{Kind: OpReleaseAll})
}

// SetListener records where the game is heard from. An absent field is
// unchanged, so a game that only walks never restates the rotation it chose
// once, and a 2D game sets its QuatRotationX(-Pi/2) at startup and never again.
//
// It is coalesced within the tick, field by field, last value winning: every
// Positional Voice reads the Listener at the one moment the tick ends, so there
// is exactly one Listener a tick can be heard from.
//
// sound never reads a camera. A game, or ecsaudio, copies a camera's Transform
// across.
func (q *Queue) SetListener(params ListenerParams) { q.listener = q.listener.merge(params) }

// operations is the tick's recorded operations, in order.
func (q *Queue) operations() []Operation { return q.ops }

// busVolumeSets is the tick's coalesced Bus volumes, by reference so the flush
// reads them without copying 32 entries.
func (q *Queue) busVolumeSets() *[MaxBuses]m.Maybe[float32] { return &q.busVolumes }

// listenerSet is the tick's coalesced Listener, by reference so the flush reads
// it without copying it.
func (q *Queue) listenerSet() *ListenerParams { return &q.listener }

// reset empties the recording for the next tick, keeping the capacity so a warm
// engine allocates nothing to record a frame of sound.
func (q *Queue) reset() {
	q.ops = q.ops[:0]
	q.busVolumes = [MaxBuses]m.Maybe[float32]{}
	q.listener = ListenerParams{}
}
