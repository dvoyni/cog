package internal

import (
	"io/fs"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"

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
	batch   Batch
	endings []Ending
}

// flushOnUpdate is the whole of sound's tick. It runs once per app.UpdateEvent,
// last, so every recorder has finished with the queue, and it does four things
// in this order, atomically:
//
//  1. drain TakePrepared into the clip table;
//  2. apply the tick's operations, in the order they were recorded - a Play or
//     a Preload naming an unknown Clip reads its bytes and prepares it here,
//     and a Release stops the Voices on that Clip and queues its destroy;
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
// while the queue is drained, and no reader can see half a tick's Voices. Its
// own scratch and the record the last flush leaves behind are locked beside
// them; neither is a resource any System declares, so neither widens what a
// game contends on.
func (p *plugin) flushOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var queue kernel.Write[*Queue]
	var voices kernel.Write[*Voices]
	var clips kernel.Write[*Clips]
	var buses kernel.Write[*Buses]
	var listener kernel.Write[*Listener]
	var device kernel.Write[*Device]
	var scratch kernel.Write[*flushScratch]
	var last kernel.Write[*lastFlush]
	var filesystem kernel.Read[storage.FileSystem]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*Queue]()
			voices = access.GetWrite[*Voices]()
			clips = access.GetWrite[*Clips]()
			buses = access.GetWrite[*Buses]()
			listener = access.GetWrite[*Listener]()
			device = access.GetWrite[*Device]()
			scratch = access.GetWrite[*flushScratch]()
			last = access.GetWrite[*lastFlush]()
			filesystem = access.GetRead[storage.FileSystem]()
		}, func(k kernel.Kernel, event app.UpdateEvent) {
			backend := p.backend.Get()
			work := scratch.Get()
			BatchReset(&work.batch)
			work.endings = work.endings[:0]

			recorded, live, table, groups := queue.Get(), voices.Get(), clips.Get(), buses.Get()
			heardFrom := listener.Get()
			ClipsDrain(table, k, backend)
			apply(k, backend, filesystem, recorded, live, table, groups, heardFrom, &work.endings)
			VoicesResolve(live, table, &work.endings)
			VoicesAdvance(live, event.Dt, &work.endings)
			VoicesFoldBuses(live, groups)
			VoicesSpatialize(live, heardFrom)

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

			VoicesCollect(live, &work.batch, arrived)
			// After the Voices, so every stop this tick's releases caused is
			// already in the batch, ahead of the destroys that follow them.
			ClipsCollect(table, &work.batch)
			backend.Emit(&work.batch)

			VoicesEndTick(live)

			// The slots come back only now, after the batch carrying their
			// stops has been handed over, which is what "a slot is stopped
			// before sound reuses it" costs: one pass, once a tick. The same
			// pass ranks the Voices that stayed, because who loses the cap is
			// decided when a play is recorded and a recorder holds nothing but
			// the Queue - the table, the Buses and the Listener are all read
			// here, at the one moment every one of them is settled.
			QueueRank(recorded, live, groups, heardFrom)

			// The ring is written beside the event and in the same pass, so
			// the two never disagree about what ended: the event is what a
			// game subscribes to, and the ring is the same fact kept for the
			// one reader that cannot subscribe to anything. It records the
			// tick this flush ran on, which is the only moment it is known
			// here.
			record := last.Get()
			record.tick = event.Tick
			for _, ending := range work.endings {
				record.record(ending, event.Tick)
				k.PublishEvent(VoiceEndedEvent{Voice: ending.Voice, Reason: ending.Reason})
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
	backend Backend,
	filesystem kernel.Read[storage.FileSystem],
	recorded *Queue,
	live *Voices,
	table *Clips,
	groups *Buses,
	heardFrom *Listener,
	endings *[]Ending,
) {
	var files fs.FS
	for _, op := range QueueOperations(recorded) {
		switch op.Kind {
		case OpPlay:
			if files == nil {
				files = filesystem.Get()
			}
			VoicesStart(live, op, ClipsResolve(table, k, files, backend, op.Clip), endings)
		case OpStop:
			VoicesStop(live, op.Voice, endings)
		case OpSetVoice:
			VoicesSet(live, op.Voice, op.Params)
		case OpStopBus:
			VoicesStopBus(live, op.Bus, endings)
		case OpSeek:
			VoicesSeek(live, op.Voice, op.Offset, endings)
		case OpPreload:
			if files == nil {
				files = filesystem.Get()
			}
			ClipsPreload(table, k, files, backend, op.Clip)
		case OpRelease:
			// The Voices go first, and that order is the whole of "a release
			// is atomic within its tick": their stops are collected into the
			// same batch as the destroy this queues, ahead of it, so the Mixer
			// never applies a destroy for a Clip it is still mixing - and a
			// streamed Voice's read-ahead is halted by its own stop.
			VoicesStopClip(live, op.Clip, endings)
			ClipsRelease(table, k, op.Clip)
		case OpReleaseAll:
			VoicesStopAll(live, endings)
			ClipsReleaseAll(table, k)
		}
	}

	// The tick's Bus volumes and its Listener land where the ordered operations
	// end, because neither is one of them: both are coalesced, and the tick's
	// last word on a Bus is the only one every Voice on it could be folded
	// with, exactly as its last word on the Listener is the only one every
	// Positional Voice could be spatialized against.
	BusesApply(groups, recorded)
	ListenerApply(heardFrom, recorded)
	QueueReset(recorded)
}
