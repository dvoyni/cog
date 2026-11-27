package sfx

import (
	"time"

	"github.com/dvoyni/cog/kernel"
)

// Tasks is the resource holding every task sfx offers others to schedule,
// read in a later plugin's Init with configurator.Resource[sfx.Tasks]().
type Tasks struct {
	// Flush applies every operation recorded on the Queue since the last
	// flush, at once and in the order they were recorded, advances every
	// playhead by the delta it is given, hands the backend the flush's
	// changes in one Emit, and then publishes a VoiceEndedEvent for each
	// voice that ended, in order. A clip's bytes are read inside it,
	// synchronously, at the flush that first names the clip's path: it is the
	// one place sfx can stall.
	//
	// sfx schedules it nowhere. A game schedules it on each update beat,
	// after its recorders, passing the beat's Delta plus its Lost, so the
	// playheads stay in step with what the device has played even after a
	// stall, and joins it into the handler's out dependency:
	//
	//	recorded := ex.Schedule(recordSounds, kernel.NoArg{})
	//	flushed := ex.Schedule(tasks.Flush, beat.Delta+beat.Lost, recorded.Handle())
	//	return beat, ex.Join(in, flushed.Handle())
	//
	// An operation recorded by a job not ordered before it lands in the next
	// flush, never lost.
	Flush *kernel.Task[time.Duration, kernel.NoResult]
}

// Queue is the resource every operation is recorded on, under
// kernel.Write[sfx.Queue], by every recorder, and which each flush drains at
// once. It is not latest-wins: an operation is a delta and is never dropped.
// A recorder holds its write lock and nothing else.
type Queue = queue

// LiveVoices is the resource of the live voices, as of the last flush: read
// it under kernel.Read[sfx.LiveVoices]. It has no mutators, and the flush
// alone writes it. Every voice that ever existed is observable exactly once:
// it is in LiveVoices now, or its VoiceEndedEvent has fired.
type LiveVoices = liveVoices

// ClipTable is the resource of what sfx knows of each clip, by path, as of
// the last flush: read it under kernel.Read[sfx.ClipTable]. It has no
// mutators, and the flush alone writes it. A path is read once, at the flush
// that first names it, and the clip shared by every voice that plays it.
type ClipTable = clipTable
