package types

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
	// OpSeek moves a Voice's playhead. It is its own operation and not a field
	// of Params, because "apply in order" and "last value wins" are the same
	// thing for a parameter and are not for a cursor move: a Seek recorded
	// before a Stop and a Seek recorded after one are different ticks.
	OpSeek
)

// Operation is one recorded operation, kept in the order it was recorded. An
// operation is a delta and may not be dropped, which is why the Queue is not
// gfx's latest-wins triple buffer: a frame is a complete description and may be
// replaced, this may not.
type Operation struct {
	Kind   OpKind
	Voice  Voice
	Clip   ClipRef
	Offset float32
	Params Params
}

// minter hands out Voice handles at the moment a play is recorded, so a handle
// is usable in the same tick that recorded it and there is no command to wait
// on. It owns the generations, and the live table owns nothing but the handle
// it was given, so the two can never disagree about whether a handle is stale.
//
// The table is fixed and generational, so no free list beyond the slots
// themselves and no map from handle to Voice is needed: the handle is the index.
type minter struct {
	// generations is each slot's current generation, one per slot, starting at
	// 0 so the first mint hands out generation 1.
	generations []uint32
	// free is the slots nothing holds, popped from the end so slot 0 goes first
	// and a test reads the handles it expects.
	free []uint32
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

// mint hands out the next handle, taking a free slot when there is one.
func (m *minter) mint() Voice {
	if n := len(m.free); n > 0 {
		index := m.free[n-1]
		m.free = m.free[:n-1]
		m.generations[index]++
		return newVoice(index, m.generations[index])
	}
	m.unslotted++
	return newVoice(uint32(len(m.generations)), m.unslotted)
}

// release returns a slot to the free list. sound calls it when a Voice ends, in
// the flush that ended it, so the slot cannot be re-minted until the batch
// carrying its stop has been emitted.
func (m *minter) release(index uint32) {
	if int(index) < len(m.generations) {
		m.free = append(m.free, index)
	}
}

// Queue is where every operation is recorded, under a write lock, and it is
// flushed once per tick, atomically. It is a resource and not a channel: a
// recorder holds the lock, appends, and is done.
//
// Nothing here coalesces. Params are stated as "coalesced within a tick, last
// value wins", and applying them in the order they were recorded says exactly
// that - the last SetVoice a tick recorded for a Voice is the last one applied
// - while keeping Play and Stop in one list with them, which is what makes a
// Play followed by a SetVoice indistinguishable from a Play that carried the
// same Params.
type Queue struct {
	ops   []Operation
	slots minter
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
func (q *Queue) Play(clip ClipRef, offset float32, params Params) Voice {
	voice := q.slots.mint()
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

// operations is the tick's recorded operations, in order.
func (q *Queue) operations() []Operation { return q.ops }

// reset empties the recording for the next tick, keeping the capacity so a warm
// engine allocates nothing to record a frame of sound.
func (q *Queue) reset() { q.ops = q.ops[:0] }
