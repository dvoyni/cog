package kernel

import (
	"sync/atomic"

	"github.com/dvoyni/cog/kernel/internal/concurrent"
)

// JobHandle names one scheduled job without its result type. It is what
// dependencies are declared with. Only an Executor makes one, directly or
// through Job.Handle. The zero JobHandle names a job that has already
// finished.
//
// slot is the job's slot in the kernel's job graph, and generation that
// slot's generation when the job took it. A slot's generation advances every
// time the slot is released, so a handle kept past its job reads as finished
// rather than as whichever job holds the slot now. Slot 0 is never handed
// out, which keeps the zero JobHandle from naming a job.
//
// A JobHandle means something only in the kernel that made it.
type JobHandle struct {
	slot       uint32
	generation uint32
}

// Job is one scheduled run of a task answering T. It is what
// Executor.ScheduleAfter takes, so a job can only be fed by one whose result
// has its arg type.
type Job[T any] struct {
	handle JobHandle
}

// Handle returns j without its result type, for declaring a dependency.
func (j Job[T]) Handle() JobHandle { return j.handle }

// job is one slot of a kernel's job graph: what every job has whatever its
// task. It holds no pointers, so the GC never scans the storage, and no
// mutex: each field is one atomic word.
type job struct {
	// successors and signals are the job's two lists, each packed as one
	// list word: the slot's generation and the list's head. Packing them
	// together is what makes linking safe without a lock: a compare-and-swap
	// on the word succeeds only while the slot still holds the same job and
	// that job has not finished. See packList.
	//
	// successors heads the job's list in the kernel's successor edges, the
	// jobs waiting for it; signals heads its list in the signal edges, the
	// Signals Notify armed for it. Both are newest first.
	successors atomic.Uint64
	signals    atomic.Uint64
	// waiting counts what the job still waits for before it may take its
	// locks: each dependency not yet finished, and one hold its batch keeps
	// until it is submitted.
	waiting atomic.Int32
	// task is the index in the kernel's task table of what the job runs; 0
	// is a Join's, which runs nothing. arg is the index of the job's arg in
	// that task's arg pool. Both are set by acquireJob, before the job is linked
	// to anything, and never change while it lives.
	task, arg uint32
}

// closedList is the head of a list whose job has finished: nothing is linked
// to it after. No edge index reaches it.
const closedList = ^uint32(0)

// packList packs a slot's generation and a list's head into one list word.
func packList(generation, head uint32) uint64 {
	return uint64(generation)<<32 | uint64(head)
}

// unpackList answers the generation and head a list word holds.
func unpackList(word uint64) (generation, head uint32) {
	return uint32(word >> 32), uint32(word)
}

// successorEdge links a job to one that waits for it.
type successorEdge struct {
	// next is the following edge of the same list, or None.
	next uint32
	// successor is the waiting job's slot.
	successor uint32
	// feedsResult is set when the successor takes the job's result as its
	// arg, as Executor.ScheduleAfter declares.
	feedsResult bool
}

// signalEdge links a job to a Signal armed for it. It is apart from
// successorEdge because it holds a pointer: its pool is the only part of the job
// graph the GC scans.
type signalEdge struct {
	// next is the following edge of the same list, or None.
	next   uint32
	signal *Signal
}

// jobGraph is a kernel's job graph: every job not yet finished, and the
// dependencies and feedings between them. It alone counts what each job still
// waits for, so it alone decides when a job is ready, and hands it on through
// ready.
type jobGraph struct {
	jobs           concurrent.Pool[job]
	successorEdges concurrent.Pool[successorEdge]
	signalEdges    concurrent.Pool[signalEdge]
	// tasks is the kernel's task table, through which a finished job's result
	// is fed to the arg of each job scheduled after it. It is set once
	// registration ends, before the first job, and never changes after.
	tasks []runnable
	// ready is handed every job that waits for nothing any more: its batch is
	// submitted and its dependencies have all finished. It is set with tasks,
	// and may be called from any goroutine.
	ready func(slot uint32)
}

// acquireJob takes a free slot for a job of task, whose arg is at arg in the
// task's arg pool, and answers its handle. The job waits for every job in
// deps, and for from, whose result it takes as its arg; a zero from feeds
// nothing. It also waits on one hold, which submit releases. deps is never
// kept.
func (g *jobGraph) acquireJob(task, arg uint32, from JobHandle, deps []JobHandle) JobHandle {
	slot := g.jobs.Acquire()
	j := g.jobs.At(slot)
	j.task, j.arg = task, arg
	generation, _ := unpackList(j.successors.Load())
	j.successors.Store(packList(generation, concurrent.None))
	j.signals.Store(packList(generation, concurrent.None))
	j.waiting.Store(1)
	for _, dep := range deps {
		g.addSuccessor(dep, slot, false)
	}
	g.addSuccessor(from, slot, true)
	return JobHandle{slot: slot, generation: generation}
}

// submit releases the hold of every job in batch, handing each that waits for
// nothing else to ready.
func (g *jobGraph) submit(batch []uint32) {
	for _, slot := range batch {
		if g.jobs.At(slot).waiting.Add(-1) == 0 {
			g.ready(slot)
		}
	}
}

// finishJob finishes the job in slot, which answered result: it sends every
// Signal armed for it, feeds result to every job scheduled after it, hands
// each successor that waits for nothing else to ready, and releases the slot.
// Every handle to the job reads as finished from then on.
func (g *jobGraph) finishJob[B any](slot uint32, result B) {
	j := g.jobs.At(slot)
	// Only this job's finish changes the generation, so it is stable here.
	generation, _ := unpackList(j.successors.Load())
	_, successors := unpackList(j.successors.Swap(packList(generation, closedList)))
	_, signals := unpackList(j.signals.Swap(packList(generation, closedList)))

	for signals != concurrent.None {
		edge := g.signalEdges.At(signals)
		edge.signal.send()
		next := edge.next
		edge.signal = nil // let the Signal go
		g.signalEdges.Release(signals)
		signals = next
	}
	for successors != concurrent.None {
		edge := g.successorEdges.At(successors)
		successor := g.jobs.At(edge.successor)
		if edge.feedsResult {
			g.tasks[successor.task].(argReceiver[B]).receiveArg(successor.arg, result)
		}
		if successor.waiting.Add(-1) == 0 {
			g.ready(edge.successor)
		}
		next := edge.next
		g.successorEdges.Release(successors)
		successors = next
	}

	// Its lists stay closed, under a generation no handle names yet.
	j.successors.Store(packList(generation+1, closedList))
	j.signals.Store(packList(generation+1, closedList))
	g.jobs.Release(slot)
}

// armSignal links signal to the job handle names, and answers whether it did.
// A job that has already finished, the zero JobHandle included, is not armed
// and answers false, and the caller signals at once.
func (g *jobGraph) armSignal(handle JobHandle, signal *Signal) bool {
	if handle.slot == concurrent.None {
		return false
	}
	edge := g.signalEdges.Acquire()
	*g.signalEdges.At(edge) = signalEdge{signal: signal}
	next := &g.signalEdges.At(edge).next
	if link(&g.jobs.At(handle.slot).signals, handle.generation, edge, next) {
		return true
	}
	g.signalEdges.At(edge).signal = nil
	g.signalEdges.Release(edge)
	return false
}

// task answers the index in the kernel's task table of what the job in slot
// runs.
func (g *jobGraph) task(slot uint32) uint32 { return g.jobs.At(slot).task }

// arg answers the index of the job in slot's arg in its task's arg pool.
func (g *jobGraph) arg(slot uint32) uint32 { return g.jobs.At(slot).arg }

// handle answers the handle of the job in slot.
func (g *jobGraph) handle(slot uint32) JobHandle {
	generation, _ := unpackList(g.jobs.At(slot).successors.Load())
	return JobHandle{slot: slot, generation: generation}
}

// addSuccessor makes successor wait for predecessor, feeding it
// predecessor's result if feedsResult, and answers whether it does. A
// predecessor that has already finished, the zero JobHandle included, is not
// waited for, and answers false. The successor's waiting count is raised
// before the edge is linked, so a finish never brings it to zero early.
func (g *jobGraph) addSuccessor(predecessor JobHandle, successor uint32, feedsResult bool) bool {
	if predecessor.slot == concurrent.None {
		return false
	}
	waiting := &g.jobs.At(successor).waiting
	waiting.Add(1)
	edge := g.successorEdges.Acquire()
	*g.successorEdges.At(edge) = successorEdge{successor: successor, feedsResult: feedsResult}
	next := &g.successorEdges.At(edge).next
	if link(&g.jobs.At(predecessor.slot).successors, predecessor.generation, edge, next) {
		return true
	}
	g.successorEdges.Release(edge)
	waiting.Add(-1)
	return false
}

// link pushes edge onto the list word holds, setting next to the edge it
// goes before, and answers whether it did. It refuses once the slot no longer
// holds the job of generation or that job has finished.
func link(word *atomic.Uint64, generation, edge uint32, next *uint32) bool {
	for {
		old := word.Load()
		current, head := unpackList(old)
		if current != generation || head == closedList {
			return false
		}
		*next = head
		if word.CompareAndSwap(old, packList(generation, edge)) {
			return true
		}
	}
}
