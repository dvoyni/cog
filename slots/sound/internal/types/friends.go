package types

import (
	"io/fs"

	"github.com/dvoyni/cog/kernel"
)

// The friend functions: what sound's internal/ reads and drives on a public
// type's unexported state. Only packages under slots/sound can import this
// package, so these are not public API - which is the whole point, because the
// root aliases Queue, Voices and Clips and a game holding a read lock on the
// view must find no mutator on it at all.

// QueueOperations reads Queue.ops for sound's internal/.
func QueueOperations(q *Queue) []Operation { return q.operations() }

// QueueReset calls Queue.reset for sound's internal/.
func QueueReset(q *Queue) { q.reset() }

// QueueRank restates Queue's free list, its steal order and the context an
// incoming play is ranked against, from the table as this flush leaves it, for
// sound's internal/.
//
// It is called after the batch has been handed over, so a slot cannot be
// re-minted before the stop that vacated it has been emitted, and it is what a
// recorder reads when the next tick asks who loses the cap.
func QueueRank(q *Queue, v *Voices, b *Buses, l *Listener) { q.slots.rebuild(v, b, l) }

// ClipsResolve reads and prepares a Clip the table has no entry for, and
// answers what sound knows about it either way, for sound's internal/.
func ClipsResolve(c *Clips, k kernel.Kernel, fsys fs.FS, backend Backend, ref ClipRef) clipFacts {
	return c.resolve(k, fsys, backend, ref)
}

// VoicesStart applies one recorded play for sound's internal/.
func VoicesStart(v *Voices, op Operation, clip clipFacts, endings *[]Ending) {
	v.start(op, clip, endings)
}

// VoicesStop applies one recorded stop for sound's internal/.
func VoicesStop(v *Voices, voice Voice, endings *[]Ending) { v.stop(voice, endings) }

// VoicesSet applies one recorded SetVoice for sound's internal/.
func VoicesSet(v *Voices, voice Voice, params Params) { v.set(voice, params) }

// VoicesStopBus applies one recorded StopBus for sound's internal/.
func VoicesStopBus(v *Voices, bus Bus, endings *[]Ending) { v.stopBus(bus, endings) }

// VoicesFoldBuses folds each Bus's volume into each Voice's gain, for sound's
// internal/.
func VoicesFoldBuses(v *Voices, buses *Buses) { v.foldBuses(buses) }

// VoicesSpatialize runs the W3C equations over every live Voice against the
// Listener, for sound's internal/.
func VoicesSpatialize(v *Voices, listener *Listener) { v.spatializeAll(listener) }

// ListenerApply installs the tick's coalesced Listener for sound's internal/.
// It is called where the ordered operations end, beside the Bus volumes, for
// the same reason: the tick's last word on the Listener is the only one every
// Positional Voice could be spatialized against.
func ListenerApply(l *Listener, q *Queue) { l.apply(q.listenerSet()) }

// BusesApply installs the tick's coalesced Bus volumes for sound's internal/.
// It is called where the ordered operations end, so that the tick's last word
// on a Bus is the one every Voice on it is folded with.
func BusesApply(b *Buses, q *Queue) { b.apply(q.busVolumeSets()) }

// VoicesSeek applies one recorded Seek for sound's internal/.
func VoicesSeek(v *Voices, voice Voice, offset float32, endings *[]Ending) {
	v.seek(voice, offset, endings)
}

// VoicesSetEnginePaused records an engine Pause over the whole table and
// reports whether it changed anything, for sound's internal/.
func VoicesSetEnginePaused(v *Voices, paused bool) bool { return v.setEnginePaused(paused) }

// VoicesResolve binds the Voices whose Clips became resident and ends the ones
// whose Clips failed, for sound's internal/.
func VoicesResolve(v *Voices, clips *Clips, endings *[]Ending) { v.resolve(clips.lookup, endings) }

// VoicesAdvance moves every playhead by one tick for sound's internal/.
func VoicesAdvance(v *Voices, dt float64, endings *[]Ending) { v.advance(dt, endings) }

// VoicesCollect fills one tick's batch from the table for sound's internal/.
// resync restates every live Voice as a start at its current playhead, which is
// what a Device that has just become ready is owed.
func VoicesCollect(v *Voices, batch *Batch, resync bool) { v.collect(batch, resync) }

// VoicesEndTick clears what only the finished flush meant, for sound's
// internal/.
func VoicesEndTick(v *Voices) { v.endTick() }

// BatchReset empties a Batch for the next tick, for sound's internal/.
func BatchReset(b *Batch) { b.reset() }

// ClipsDrain installs every prepare that finished, for sound's internal/.
func ClipsDrain(c *Clips, k kernel.Kernel, backend Backend) { c.drain(k, backend) }
