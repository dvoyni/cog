package kernel

import (
	"errors"
	"runtime"
	"sync"
)

// errSkipped is the outcome handed to the owner of a job that never ran because
// a job it was ordered after failed.
var errSkipped = errors.New("kernel: skipped because a predecessor failed")

// task represents a unit of work that can be scheduled and executed by the scheduler.
// It defines the locks required for the task and the logic to run the task.
// The locks method returns the resources that the task needs to read and write.
// If one of the tasks already holds a lock on a resource that another task needs,
// the scheduler will wait until the lock is released before executing the task.
// Lock for read allows multiple tasks to read the same resource concurrently,
// but only one task can write to a resource at a time.
//
// run receives the invocation the task was scheduled with: its own
// commandContext for a command, the event's eventContext for a subscription.
type task interface {
	locks() *lockSet
	run(invocation any) error
}

// jobOwner is told how a pooled job ended: the error its run returned, errSkipped
// if it never ran, or ErrSchedulerStopped if the scheduler stopped first. It is
// called exactly once per job, after the job's locks are released.
type jobOwner interface {
	jobDone(err error)
}

// workItem is what a job runs. A job with no owner is run by the goroutine that
// submitted it, which is parked on the job's wake channel until it is granted;
// a job with an owner is run by a pool worker.
type workItem struct {
	task       task
	invocation any
	owner      jobOwner
}

// batchJob is one job of a batch submitted under a single lock. after names the
// earlier jobs of the same batch this one is ordered after; such an edge also
// carries failure, so a job after one that failed is skipped.
type batchJob struct {
	task  task
	locks *lockSet
	after []int32
}

// jobID names one job for its whole life and never again: the low half is its
// slot, the high half the slot's generation when the job took it. A job is live
// while its slot still carries its id, so an id held past the job's end - by a
// track, by a successor list - reads as finished rather than as whoever took
// the slot next.
type jobID uint64

func (id jobID) slot() int32 { return int32(uint32(id)) }

type jobState uint8

const (
	jobBlocked jobState = iota
	jobQueued
	jobRunning
)

// finished is a job finish is releasing: the one it was called for, and every
// job that ended skipped because of it.
type finished struct {
	id     jobID
	failed bool
}

type successor struct {
	id jobID
	// ordered marks an edge a batch declared, as opposed to one a shared
	// resource implied. Only an ordered edge skips its successor on failure.
	ordered bool
}

type jobSlot struct {
	lockTrace
	id         jobID
	generation uint32
	state      jobState
	skip       bool
	blockers   int32
	successors []successor
	work       workItem
	// wake is how a job run by its submitter is told it was granted. It is made
	// once per slot and reused by every such job that takes the slot.
	wake chan struct{}
}

// track is one resource's tail: the last job to write it, and every job that
// has read it since. A new reader waits for the writer; a new writer waits for
// the readers, or for the writer if nobody has read since. That is the whole
// lock protocol, and it grants each resource in submission order.
type track struct {
	writer  jobID
	readers []jobID
}

// scheduler grants resource locks. There is no coordinator goroutine: every
// decision is taken by the goroutine submitting or finishing a job, under mu,
// and costs work proportional to that job's own lock set and edges.
//
// Jobs are of two kinds. A blocking dispatch submits a job and runs it itself
// once granted. A publication or an asynchronous command submits jobs that a
// pool worker runs. The pool grows when a job is ready and no worker is idle,
// so a ready job never waits for a worker that is busy in plugin code, and a
// worker that finishes a job runs the next one it made ready itself rather than
// handing it over.
//
// It is live from creation until shutdown, after which nothing is granted and
// every job not already running is ended with ErrSchedulerStopped.
type scheduler struct {
	mu     sync.Mutex
	tracks []track
	slots  []jobSlot
	free   []int32
	// batch maps a batch's positions to the ids its jobs were given.
	batch []jobID

	queue      []jobID
	queueHead  int
	idle       int
	keepIdle   int
	workerWake *sync.Cond

	stopping bool
	done     chan struct{}
	stopOnce sync.Once
}

func newScheduler() *scheduler {
	s := &scheduler{
		done:     make(chan struct{}),
		keepIdle: runtime.GOMAXPROCS(0),
	}
	s.workerWake = sync.NewCond(&s.mu)
	return s
}

// execute runs t on the calling goroutine once its locks are granted, then
// releases them. It blocks until the task completes.
func (s *scheduler) execute(t task, invocation any, locks *lockSet) error {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return ErrSchedulerStopped{}
	}
	id, granted := s.submitLocked(locks, workItem{task: t, invocation: invocation}, nil)
	var wake chan struct{}
	if !granted {
		wake = s.slots[id.slot()].wake
	}
	s.mu.Unlock()

	if wake != nil {
		select {
		case <-wake:
		case <-s.done:
			return ErrSchedulerStopped{}
		}
	}
	defer s.finish(id, false, false)
	return t.run(invocation)
}

// submit hands one job to the pool. It returns at once; the owner hears how the
// job ended.
func (s *scheduler) submit(locks *lockSet, work workItem) error {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return ErrSchedulerStopped{}
	}
	queuedBefore := s.queued()
	s.submitLocked(locks, work, nil)
	signals, spawns := s.claimWorkersLocked(s.queued() - queuedBefore)
	s.mu.Unlock()
	s.startWorkers(signals, spawns)
	return nil
}

// submitBatch hands every job of jobs to the pool under one lock, so no other
// submission can interleave with it. Jobs must be in dependency order: each
// after index names an earlier position.
func (s *scheduler) submitBatch(jobs []batchJob, invocation any, owner jobOwner) error {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return ErrSchedulerStopped{}
	}
	queuedBefore := s.queued()
	s.batch = s.batch[:0]
	for _, job := range jobs {
		id, _ := s.submitLocked(job.locks, workItem{task: job.task, invocation: invocation, owner: owner}, job.after)
		s.batch = append(s.batch, id)
	}
	signals, spawns := s.claimWorkersLocked(s.queued() - queuedBefore)
	s.mu.Unlock()
	s.startWorkers(signals, spawns)
	return nil
}

// submitLocked gives a job a slot, puts it on the tail of every track it locks
// and behind every batch position it is ordered after, and reports whether it
// was granted on the spot. A granted pooled job is queued; a granted job run by
// its submitter is marked running and the submitter goes ahead.
func (s *scheduler) submitLocked(locks *lockSet, work workItem, after []int32) (jobID, bool) {
	if int(locks.span) > len(s.tracks) {
		s.tracks = append(s.tracks, make([]track, int(locks.span)-len(s.tracks))...)
	}
	id := s.allocateLocked()
	slot := &s.slots[id.slot()]
	slot.work = work
	traceSubmitted(slot)

	for _, index := range locks.read {
		t := &s.tracks[index]
		s.waitOnLocked(slot, t.writer, false)
		t.readers = append(s.pruneLocked(t.readers), id)
	}
	for _, index := range locks.write {
		t := &s.tracks[index]
		t.readers = s.pruneLocked(t.readers)
		if len(t.readers) == 0 {
			s.waitOnLocked(slot, t.writer, false)
		}
		for _, reader := range t.readers {
			s.waitOnLocked(slot, reader, false)
		}
		t.writer, t.readers = id, t.readers[:0]
	}
	for _, position := range after {
		s.waitOnLocked(slot, s.batch[position], true)
	}

	if slot.blockers > 0 {
		if work.owner == nil && slot.wake == nil {
			slot.wake = make(chan struct{}, 1)
		}
		return id, false
	}
	traceGranted(slot, 0)
	if work.owner == nil {
		slot.state = jobRunning
	} else {
		s.enqueueLocked(id)
	}
	return id, true
}

func (s *scheduler) allocateLocked() jobID {
	var index int32
	if n := len(s.free); n > 0 {
		index = s.free[n-1]
		s.free = s.free[:n-1]
	} else {
		index = int32(len(s.slots))
		s.slots = append(s.slots, jobSlot{})
	}
	slot := &s.slots[index]
	slot.generation++
	slot.id = jobID(uint64(slot.generation)<<32 | uint64(uint32(index)))
	slot.state = jobBlocked
	slot.skip = false
	slot.blockers = 0
	return slot.id
}

// live reports whether id still names a job that has not finished.
func (s *scheduler) live(id jobID) bool {
	return id != 0 && s.slots[id.slot()].id == id
}

// waitOnLocked makes slot wait for predecessor, unless predecessor has already
// finished. The same predecessor reached through two tracks is two edges, and
// finishing it releases both, so nothing needs to deduplicate them.
func (s *scheduler) waitOnLocked(slot *jobSlot, predecessor jobID, ordered bool) {
	if !s.live(predecessor) {
		return
	}
	before := &s.slots[predecessor.slot()]
	before.successors = append(before.successors, successor{id: slot.id, ordered: ordered})
	slot.blockers++
}

// pruneLocked drops the readers that have finished, so a resource that is only
// ever read keeps a list as long as its live readers and no longer.
func (s *scheduler) pruneLocked(readers []jobID) []jobID {
	kept := readers[:0]
	for _, reader := range readers {
		if s.live(reader) {
			kept = append(kept, reader)
		}
	}
	return kept
}

func (s *scheduler) enqueueLocked(id jobID) {
	s.slots[id.slot()].state = jobQueued
	s.queue = append(s.queue, id)
}

func (s *scheduler) queued() int { return len(s.queue) - s.queueHead }

// finish releases a finished job's locks, grants every job that was waiting on
// it alone, and frees its slot. A worker passes continuing, and gets back the
// first pooled job it made ready to run itself, without the queue.
func (s *scheduler) finish(id jobID, failed, continuing bool) (jobID, workItem) {
	var wakes [4]chan struct{}
	ready := wakes[:0]
	var stack [4]finished
	pending := append(stack[:0], finished{id, failed})
	var skipped []jobOwner
	var next jobID

	s.mu.Lock()
	slot := &s.slots[id.slot()]
	traceFinishing(slot)
	queuedBefore := s.queued()
	for len(pending) > 0 {
		done := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		slot := &s.slots[done.id.slot()]
		for _, after := range slot.successors {
			if !s.live(after.id) {
				continue
			}
			waiting := &s.slots[after.id.slot()]
			if after.ordered && done.failed {
				waiting.skip = true
			}
			waiting.blockers--
			if waiting.blockers > 0 {
				continue
			}
			traceGranted(waiting, done.id)
			switch {
			case waiting.skip:
				// A skipped job ends where it stands, and is itself a failure to
				// whatever was ordered after it.
				skipped = append(skipped, waiting.work.owner)
				pending = append(pending, finished{after.id, true})
			case waiting.work.owner == nil:
				waiting.state = jobRunning
				ready = append(ready, waiting.wake)
			case continuing && next == 0:
				waiting.state = jobRunning
				next = after.id
			default:
				s.enqueueLocked(after.id)
			}
		}
		s.releaseLocked(slot)
	}
	var work workItem
	if next != 0 {
		work = s.slots[next.slot()].work
	}
	signals, spawns := s.claimWorkersLocked(s.queued() - queuedBefore)
	s.mu.Unlock()

	for _, wake := range ready {
		wake <- struct{}{}
	}
	s.startWorkers(signals, spawns)
	for _, owner := range skipped {
		owner.jobDone(errSkipped)
	}
	return next, work
}

func (s *scheduler) releaseLocked(slot *jobSlot) {
	index := slot.id.slot()
	slot.id = 0
	slot.successors = slot.successors[:0]
	slot.work = workItem{}
	s.free = append(s.free, index)
}

// claimWorkersLocked decides who runs the added newly queued jobs: an idle worker
// is woken for each while there are any, and a new one is started for the rest.
func (s *scheduler) claimWorkersLocked(added int) (signals, spawns int) {
	for range added {
		if s.idle > 0 {
			s.idle--
			signals++
		} else {
			spawns++
		}
	}
	return signals, spawns
}

func (s *scheduler) startWorkers(signals, spawns int) {
	for range signals {
		s.workerWake.Signal()
	}
	for range spawns {
		go s.worker()
	}
}

// worker runs queued jobs until there are none and enough other workers are
// already idle, or the scheduler stops.
func (s *scheduler) worker() {
	for {
		id, work, ok := s.take()
		if !ok {
			return
		}
		for id != 0 {
			err := work.task.run(work.invocation)
			owner := work.owner
			id, work = s.finish(id, err != nil, true)
			owner.jobDone(err)
		}
	}
}

// take pops the next queued job, parking while the queue is empty.
func (s *scheduler) take() (jobID, workItem, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.queued() == 0 {
		if s.stopping || s.idle >= s.keepIdle {
			return 0, workItem{}, false
		}
		s.idle++
		s.workerWake.Wait()
	}
	id := s.queue[s.queueHead]
	s.queueHead++
	if s.queueHead == len(s.queue) {
		s.queue, s.queueHead = s.queue[:0], 0
	}
	slot := &s.slots[id.slot()]
	slot.state = jobRunning
	return id, slot.work, true
}

// shutdown stops granting, ends every job that is not already running with
// ErrSchedulerStopped, and wakes every parked worker so it can leave. A job that
// is running finishes normally. It is idempotent.
func (s *scheduler) shutdown() {
	s.stopOnce.Do(func() {
		var stopped []jobOwner
		s.mu.Lock()
		s.stopping = true
		close(s.done)
		for i := range s.slots {
			slot := &s.slots[i]
			if slot.id == 0 || slot.state == jobRunning {
				continue
			}
			if slot.work.owner != nil {
				stopped = append(stopped, slot.work.owner)
			}
			s.releaseLocked(slot)
		}
		s.queue, s.queueHead = nil, 0
		s.idle = 0
		s.mu.Unlock()
		s.workerWake.Broadcast()
		for _, owner := range stopped {
			owner.jobDone(ErrSchedulerStopped{})
		}
	})
}

// stopped reports whether the scheduler has shut down, for a caller deciding
// whether it may run work without asking for locks.
func (s *scheduler) stopped() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}
