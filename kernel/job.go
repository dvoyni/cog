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
// slot is the job's slot in the kernel's job storage, and generation that
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

// job is one slot of a kernel's job storage: what every job has whatever its
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
// successorEdge because it holds a pointer: its pool is the only part of job
// storage the GC scans.
type signalEdge struct {
	// next is the following edge of the same list, or None.
	next   uint32
	signal *Signal
}

// jobStorage is a kernel's jobs and the edges between them.
type jobStorage struct {
	jobs           concurrent.Pool[job]
	successorEdges concurrent.Pool[successorEdge]
	signalEdges    concurrent.Pool[signalEdge]
}

// acquireJob takes a free slot for a job of task, whose arg is at arg in the
// task's arg pool, and answers the slot and the job's handle. The job starts
// waiting on one hold, which its batch releases once submitted.
func (s *jobStorage) acquireJob(task, arg uint32) (uint32, JobHandle) {
	slot := s.jobs.Acquire()
	j := s.jobs.At(slot)
	j.task, j.arg = task, arg
	generation, _ := unpackList(j.successors.Load())
	j.successors.Store(packList(generation, concurrent.None))
	j.signals.Store(packList(generation, concurrent.None))
	j.waiting.Store(1)
	return slot, JobHandle{slot: slot, generation: generation}
}

// handle answers the handle of the job in slot.
func (s *jobStorage) handle(slot uint32) JobHandle {
	generation, _ := unpackList(s.jobs.At(slot).successors.Load())
	return JobHandle{slot: slot, generation: generation}
}

// addSuccessor makes successor wait for predecessor, and answers whether it
// does. A predecessor that has already finished, the zero JobHandle
// included, is not waited for, and answers false. The successor's waiting
// count is raised before the edge is linked, so a finish never brings it to
// zero early.
func (s *jobStorage) addSuccessor(predecessor JobHandle, successor uint32, feedsResult bool) bool {
	if predecessor.slot == concurrent.None {
		return false
	}
	waiting := &s.jobs.At(successor).waiting
	waiting.Add(1)
	edge := s.successorEdges.Acquire()
	*s.successorEdges.At(edge) = successorEdge{successor: successor, feedsResult: feedsResult}
	next := &s.successorEdges.At(edge).next
	if link(&s.jobs.At(predecessor.slot).successors, predecessor.generation, edge, next) {
		return true
	}
	s.successorEdges.Release(edge)
	waiting.Add(-1)
	return false
}

// armSignal links signal to the job handle names, and answers whether it did.
// A job that has already finished, the zero JobHandle included, is not armed
// and answers false, and the caller signals at once.
func (s *jobStorage) armSignal(handle JobHandle, signal *Signal) bool {
	if handle.slot == concurrent.None {
		return false
	}
	edge := s.signalEdges.Acquire()
	*s.signalEdges.At(edge) = signalEdge{signal: signal}
	next := &s.signalEdges.At(edge).next
	if link(&s.jobs.At(handle.slot).signals, handle.generation, edge, next) {
		return true
	}
	s.signalEdges.At(edge).signal = nil
	s.signalEdges.Release(edge)
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

// finishJob marks the job in slot finished, sends every Signal armed for it,
// and answers the head of its successor list, newest first, which the caller
// now owns: it walks the list, then returns the edges with releaseSuccessors.
// Nothing is linked to the job after.
func (s *jobStorage) finishJob(slot uint32) uint32 {
	j := s.jobs.At(slot)
	// Only this job's finish changes the generation, so it is stable here.
	generation, _ := unpackList(j.successors.Load())
	_, successors := unpackList(j.successors.Swap(packList(generation, closedList)))
	_, signals := unpackList(j.signals.Swap(packList(generation, closedList)))

	for signals != concurrent.None {
		edge := s.signalEdges.At(signals)
		edge.signal.send()
		next := edge.next
		edge.signal = nil // let the Signal go
		s.signalEdges.Release(signals)
		signals = next
	}
	return successors
}

// releaseSuccessors returns every edge of a successor list finishJob answered.
func (s *jobStorage) releaseSuccessors(head uint32) {
	for head != concurrent.None {
		next := s.successorEdges.At(head).next
		s.successorEdges.Release(head)
		head = next
	}
}

// releaseJob returns the slot of a finished job, advancing its generation so
// every handle to the job reads as finished from then on: its lists stay
// closed, under a generation no handle names yet.
func (s *jobStorage) releaseJob(slot uint32) {
	j := s.jobs.At(slot)
	generation, _ := unpackList(j.successors.Load())
	j.successors.Store(packList(generation+1, closedList))
	j.signals.Store(packList(generation+1, closedList))
	s.jobs.Release(slot)
}
