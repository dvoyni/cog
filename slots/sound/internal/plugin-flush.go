package internal

import (
	"io/fs"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/sound"
	"github.com/dvoyni/cog/slots/sound/internal/types"
	"github.com/dvoyni/cog/slots/storage"
)

// flushScratch is the flush's own working memory: the batch handed to the
// Adapter, and the endings collected on the way to it. Both keep their capacity
// between ticks, so a warm engine flushes a tick of sound without allocating.
//
// It is a resource rather than a field on the plugin because state that must
// persist across invocations belongs in one: the factory closure is shared, and
// a resource is what Describe, the contention report and an agent reading the
// architecture can see.
type flushScratch struct {
	batch   sound.Batch
	endings []types.Ending
}

// flushOnUpdate is the whole of sound's tick. It runs once per app.UpdateEvent,
// last, so every recorder has finished with the queue, and it does four things
// in this order, atomically:
//
//  1. drain TakePrepared into the clip table;
//  2. apply the tick's operations, in the order they were recorded;
//  3. compute - bind the Clips that became resident, advance every playhead;
//  4. Emit once.
//
// Draining first is what makes a release recorded against an in-flight prepare
// cost nothing: by the time the completion arrives its entry is gone, so no
// ClipID is ever minted only to be destroyed.
//
// It holds a write lock on all four resources at once, which is what "flushed
// once per tick, atomically" means in lock terms: no recorder can be mid-append
// while the queue is drained, and no reader can see half a tick's Voices.
func (p *plugin) flushOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var queue kernel.Write[*sound.Queue]
	var voices kernel.Write[*sound.Voices]
	var clips kernel.Write[*sound.Clips]
	var device kernel.Write[*sound.Device]
	var scratch kernel.Write[*flushScratch]
	var filesystem kernel.Read[storage.FileSystem]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*sound.Queue]()
			voices = access.GetWrite[*sound.Voices]()
			clips = access.GetWrite[*sound.Clips]()
			device = access.GetWrite[*sound.Device]()
			scratch = access.GetWrite[*flushScratch]()
			filesystem = access.GetRead[storage.FileSystem]()
		}, func(k kernel.Kernel, event app.UpdateEvent) {
			backend := p.backend.Get()
			work := scratch.Get()
			types.BatchReset(&work.batch)
			work.endings = work.endings[:0]

			recorded, live, table := queue.Get(), voices.Get(), clips.Get()
			types.ClipsDrain(table, k, backend)
			apply(k, backend, filesystem, recorded, live, table, &work.endings)
			types.VoicesResolve(live, table, &work.endings)
			types.VoicesAdvance(live, event.Dt, &work.endings)

			types.VoicesCollect(live, &work.batch)
			backend.Emit(&work.batch)

			// The slots come back only now, after the batch carrying their
			// stops has been handed over, which is what "a slot is stopped
			// before sound reuses it" costs: one pass, once a tick.
			for _, ending := range work.endings {
				types.QueueRelease(recorded, ending.Voice)
			}
			types.VoicesEndTick(live)

			// Polled once per flush, and it is a field read rather than a
			// query. The resource is written through its pointer because
			// replacing it would allocate a Device every tick.
			*device.Get() = backend.Device()

			for _, ending := range work.endings {
				k.PublishEvent(sound.VoiceEndedEvent{Voice: ending.Voice, Reason: ending.Reason})
			}
		}
}

// apply runs the tick's operations in the order they were recorded, which is
// what makes a Play followed by a SetVoice indistinguishable from a Play that
// carried the same Params.
//
// The filesystem is boxed on the first Play and not before. Handing
// storage.FileSystem out as an fs.FS costs 32 bytes, measured, and a tick that
// names no new Clip should not pay it.
func apply(
	k kernel.Kernel,
	backend sound.Backend,
	filesystem kernel.Read[storage.FileSystem],
	recorded *sound.Queue,
	live *sound.Voices,
	table *sound.Clips,
	endings *[]types.Ending,
) {
	var files fs.FS
	for _, op := range types.QueueOperations(recorded) {
		switch op.Kind {
		case types.OpPlay:
			if files == nil {
				files = filesystem.Get()
			}
			types.VoicesStart(live, op, types.ClipsResolve(table, k, files, backend, op.Clip), endings)
		case types.OpStop:
			types.VoicesStop(live, op.Voice, endings)
		case types.OpSetVoice:
			types.VoicesSet(live, op.Voice, op.Params)
		case types.OpSeek:
			types.VoicesSeek(live, op.Voice, op.Offset, endings)
		}
	}
	types.QueueReset(recorded)
}
