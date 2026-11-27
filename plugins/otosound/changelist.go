//go:build !js

package otosound

import (
	"time"

	"github.com/dvoyni/cog/plugins/sfx"
)

// operationsPerVoice is how many operations one flush records against one
// voice slot: a start, an update and a stop. It sizes a change list, which is
// therefore three times the slot count and never grows in the ordinary case.
const operationsPerVoice = 3

// operationKind is what one operation does.
type operationKind uint8

const (
	// operationKindStart begins a voice in a slot, at an offset, with its
	// parameters.
	operationKindStart operationKind = iota
	// operationKindUpdate restates a slot's parameters, which the mixer ramps
	// to.
	operationKindUpdate
	// operationKindStop ends a voice, which the mixer ramps to silence before
	// it frees the slot.
	operationKindStop
	// operationKindDestroy releases a clip. The mixer does nothing with it:
	// the stops ordered before it already dropped every reference the voice
	// table held, and the flush side lets the clip go once the applied counter
	// has passed the changes that carried it. The mixer never frees.
	operationKindDestroy
)

// operation is one operation of a flush's changes, in the order it was
// recorded. A start's ClipID is resolved to the clip it names on the flush
// side, while the clip table is still the flush's own, so the mixer holds the
// clip and never looks it up.
type operation struct {
	// clip is the clip a start plays, nil on every other operation.
	clip   sfx.PreparedClip
	offset time.Duration
	params sfx.VoiceParams
	clipID sfx.ClipID
	slot   sfx.VoiceSlot
	kind   operationKind
	loop   bool
}

// changeList is one flush's changes as the mixer applies them, whole, at a
// block start: an ordered list of operations rather than the seam's four
// slices, since order is the point of it. A stop of a slot in one flush and a
// start of the same slot in the next are two operations whose order decides
// whether the second voice sounds at all, and four slices applied kind by kind
// cannot say that once two flushes have been merged into the staging changes.
//
// Its capacity is made once and kept across every publish, so a warm handoff
// allocates nothing.
type changeList struct {
	operations []operation
}

// newChangeList returns an empty change list with room for every operation
// one flush can record against voiceCount slots.
func newChangeList(voiceCount int) *changeList {
	return &changeList{operations: make([]operation, 0, operationsPerVoice*voiceCount)}
}

// record appends recorded. When coalesce is set, which is the merge and only
// the merge, an update folds into the last start or update standing for its
// slot instead: the same coalescing sfx performs within a flush, widened over
// the flushes the merge covers. Nothing else coalesces, since a start and a
// stop are what sfx's contract promises never to lose.
func (c *changeList) record(recorded operation, coalesce bool) {
	if coalesce && recorded.kind == operationKindUpdate {
		for index := len(c.operations) - 1; index >= 0; index-- {
			standing := &c.operations[index]
			if standing.kind == operationKindDestroy || standing.slot != recorded.slot {
				continue
			}
			if standing.kind != operationKindStop {
				// A start carries its own parameters and an update restates
				// them, so folding into either says exactly what the pair said.
				standing.params = recorded.params
				return
			}
			// The slot is stopped: the update reaches a voice the mixer will
			// ignore, and appending it keeps the record honest.
			break
		}
	}
	c.operations = append(c.operations, recorded)
}
