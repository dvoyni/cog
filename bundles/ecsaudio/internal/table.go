package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/slots/sound"
)

// entry is one Entity's correspondence to the Voice the binding started for it.
//
// It holds a handle and never asks whether that handle is live. An entry
// outlives its Voice: a one-shot that finished and a Voice stolen under sound's
// cap both leave a dead handle sitting here, and that is what stops the next
// tick re-playing it. Every operation is a no-op on a Voice that is gone, so
// nothing here needs a guard.
type entry struct {
	// voice is what the Play handed back, live or not.
	voice sound.Voice
	// clip is what that Play named, kept because a ClipRef holding a Blob is
	// not comparable and a change of Clip is a Stop and a fresh Play.
	clip sound.ClipRef
	// positional records whether the Play carried a Position, which is the
	// difference between a Voice a Transform may move and one it may not.
	// sound's rule is one-way in the other direction - the first position a
	// Voice receives, at Play or by SetVoice, makes it positional for life - so
	// without this flag a Transform added to a playing non-positional Voice
	// would quietly spatialize it mid-note.
	positional bool
	// seen is the reconcile that last matched this entry. An entry the current
	// reconcile did not see belongs to an Entity that despawned or lost its
	// Emitter, which is the same row of the table either way.
	seen uint64
}

// table is the binding's Entity-to-Voice correspondence, and the reason the
// recording System takes no structural lock. It is deliberately not a Component:
// a Component would cost a structural change every time a sound started, for
// bookkeeping nobody outside the binding reads.
//
// It is a resource the plugin owns rather than something the System's closure
// captured, for the reason ecsscene's scratch is: anything a System keeps
// between calls belongs in its lock set, so the kernel, not a comment, is what
// keeps two holders apart.
type table struct {
	entries map[ecs.Entity]entry
	// reconcile counts the reconciles, so that "seen this tick" is a comparison
	// rather than a second pass clearing flags.
	reconcile uint64
}

// newTable builds the empty correspondence.
func newTable() *table { return &table{entries: map[ecs.Entity]entry{}} }

// begin opens a reconcile. Everything the walk that follows does not touch has
// departed.
func (t *table) begin() { t.reconcile++ }

// of reports the entry an Entity holds, and whether it holds one. Holding one
// is what says do not play; whether its Voice is still alive is never asked.
func (t *table) of(e ecs.Entity) (entry, bool) {
	held, ok := t.entries[e]
	return held, ok
}

// put records the Voice a Play just started for an Entity, replacing whatever
// that Entity held, and marks it seen by this reconcile.
func (t *table) put(e ecs.Entity, voice sound.Voice, clip sound.ClipRef, positional bool) {
	t.entries[e] = entry{voice: voice, clip: clip, positional: positional, seen: t.reconcile}
}

// keep marks an entry seen by this reconcile without changing what it holds.
func (t *table) keep(e ecs.Entity, held entry) {
	held.seen = t.reconcile
	t.entries[e] = held
}

// sweep stops the Voice of every entry this reconcile did not see and drops it.
// A despawned Entity stops matching the query, so it arrives here as the same
// row as an Entity whose Emitter was removed - no ecs Hook, no Reference to
// resolve, and no reach-back from the Voice.
//
// The scan is over the table rather than over the world, so it costs what the
// binding is holding rather than what the world holds.
func (t *table) sweep(queue *sound.Queue) {
	for e, held := range t.entries {
		if held.seen == t.reconcile {
			continue
		}
		queue.Stop(held.voice)
		delete(t.entries, e)
	}
}
