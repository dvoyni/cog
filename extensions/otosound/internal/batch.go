//go:build !js

package internal

import (
	"time"

	"github.com/dvoyni/cog/slots/sound"
)

// opsPerVoice is how many operations one tick can record against one slot: a
// start, an update and a stop. It sizes a ring entry, which is therefore
// 3 x MaxVoices operations and never grows in the ordinary case.
const opsPerVoice = 3

// opKind is what one operation does.
type opKind uint8

const (
	// opStart begins a voice in a slot, at an offset, with its parameters.
	opStart opKind = iota
	// opUpdate restates a slot's parameters, which the Mixer ramps towards.
	opUpdate
	// opStop ends a voice, which the Mixer ramps to silence before freeing.
	opStop
	// opDestroy releases a Clip. The Mixer does nothing with it: the stops
	// ordered before it already dropped every reference the voice table held,
	// and the memory comes back on the tick side once the applied counter has
	// passed the batch that carried it. The Mixer never frees.
	opDestroy
)

// op is one operation, in the order it was recorded.
//
// A ClipID is resolved to the clip it names on the tick side, at the moment the
// operation is staged, so the Mixer holds a pointer and never a lookup.
type op struct {
	clip *clipData
	// ring is the read-ahead a start on a streamed Clip was given, and is nil
	// on every other operation and on every start on a resident one. The tick
	// made it, and what travels is the ring alone: the decoder and the
	// goroutine behind it stay on the side of the seam that may have them.
	ring   *pcmRing
	offset time.Duration
	params sound.VoiceParams
	id     sound.ClipID
	slot   sound.VoiceSlot
	kind   opKind
	loop   bool
}

// batch is one handoff: a tick's operations in the order they were recorded,
// applied whole at a block start. Its capacity is made once and never given
// back, so a warm engine hands a tick of sound over without allocating.
//
// It is an ordered list rather than sound.Batch's four slices because order is
// the whole point of it. A stop of a slot in one tick and a start of that same
// slot in the next are two operations whose order decides whether the second
// Voice sounds at all, and four slices applied kind by kind cannot express that
// once two ticks have been merged into one batch.
type batch struct {
	ops []op
}

func newBatch(maxVoices int) *batch {
	return &batch{ops: make([]op, 0, opsPerVoice*maxVoices)}
}

func (b *batch) reset() { b.ops = b.ops[:0] }

// record appends o. When coalesce is set - the merge path, and only there - an
// update folds into the last operation already standing for that slot instead
// of appending, which is the same coalescing sound performs within a tick,
// widened over the ticks the merge covers. Nothing else coalesces: a start and
// a stop are the operations the contract promises never to lose.
func (b *batch) record(o op, coalesce bool) {
	if coalesce && o.kind == opUpdate {
		for i := len(b.ops) - 1; i >= 0; i-- {
			previous := &b.ops[i]
			if previous.kind == opDestroy || previous.slot != o.slot {
				continue
			}
			switch previous.kind {
			case opUpdate, opStart:
				// A start carries its own parameters and an update restates
				// them, so folding into either says exactly what the pair said.
				previous.params = o.params
				return
			default:
				// The slot is stopped; the update reaches a voice the Mixer
				// will ignore, and appending keeps the record honest.
			}
			break
		}
	}
	b.ops = append(b.ops, o)
}
