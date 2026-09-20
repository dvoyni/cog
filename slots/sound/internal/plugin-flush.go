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
//  3. compute - bind the Clips that became resident, advance every playhead,
//     fold each Bus's volume into each Voice's gain, and run the W3C equations
//     over every Voice against the one Listener;
//  4. Emit once.
//
// Draining first is what makes a release recorded against an in-flight prepare
// cost nothing: by the time the completion arrives its entry is gone, so no
// ClipID is ever minted only to be destroyed.
//
// It holds a write lock on all six resources at once, which is what "flushed
// once per tick, atomically" means in lock terms: no recorder can be mid-append
// while the queue is drained, and no reader can see half a tick's Voices.
func (p *plugin) flushOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var queue kernel.Write[*sound.Queue]
	var voices kernel.Write[*sound.Voices]
	var clips kernel.Write[*sound.Clips]
	var buses kernel.Write[*sound.Buses]
	var listener kernel.Write[*sound.Listener]
	var device kernel.Write[*sound.Device]
	var scratch kernel.Write[*flushScratch]
	var filesystem kernel.Read[storage.FileSystem]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*sound.Queue]()
			voices = access.GetWrite[*sound.Voices]()
			clips = access.GetWrite[*sound.Clips]()
			buses = access.GetWrite[*sound.Buses]()
			listener = access.GetWrite[*sound.Listener]()
			device = access.GetWrite[*sound.Device]()
			scratch = access.GetWrite[*flushScratch]()
			filesystem = access.GetRead[storage.FileSystem]()
		}, func(k kernel.Kernel, event app.UpdateEvent) {
			backend := p.backend.Get()
			work := scratch.Get()
			types.BatchReset(&work.batch)
			work.endings = work.endings[:0]

			recorded, live, table, groups := queue.Get(), voices.Get(), clips.Get(), buses.Get()
			heardFrom := listener.Get()
			types.ClipsDrain(table, k, backend)
			apply(k, backend, filesystem, recorded, live, table, groups, heardFrom, &work.endings)
			types.VoicesResolve(live, table, &work.endings)
			types.VoicesAdvance(live, event.Dt, &work.endings)
			types.VoicesFoldBuses(live, groups)
			types.VoicesSpatialize(live, heardFrom)

			// Polled once per flush, and it is a field read rather than a
			// query. The resource is written through its pointer because
			// replacing it would allocate a Device every tick.
			//
			// It is read before the batch is collected so that a Device which
			// became ready during this tick is resynced in the tick that
			// noticed it rather than the one after. Ready going false is not
			// acted on at all: playback keeps being simulated, and loss is
			// reported to nobody.
			was := device.Get().Ready
			*device.Get() = backend.Device()
			arrived := device.Get().Ready && !was

			types.VoicesCollect(live, &work.batch, arrived)
			backend.Emit(&work.batch)

			types.VoicesEndTick(live)

			// The slots come back only now, after the batch carrying their
			// stops has been handed over, which is what "a slot is stopped
			// before sound reuses it" costs: one pass, once a tick. The same
			// pass ranks the Voices that stayed, because who loses the cap is
			// decided when a play is recorded and a recorder holds nothing but
			// the Queue - the table, the Buses and the Listener are all read
			// here, at the one moment every one of them is settled.
			types.QueueRank(recorded, live, groups, heardFrom)

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
	groups *sound.Buses,
	heardFrom *sound.Listener,
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
		case types.OpStopBus:
			types.VoicesStopBus(live, op.Bus, endings)
		case types.OpSeek:
			types.VoicesSeek(live, op.Voice, op.Offset, endings)
		}
	}

	// The tick's Bus volumes and its Listener land where the ordered operations
	// end, because neither is one of them: both are coalesced, and the tick's
	// last word on a Bus is the only one every Voice on it could be folded
	// with, exactly as its last word on the Listener is the only one every
	// Positional Voice could be spatialized against.
	types.BusesApply(groups, recorded)
	types.ListenerApply(heardFrom, recorded)
	types.QueueReset(recorded)
}
