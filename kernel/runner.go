package kernel

import (
	"sync"
	"sync/atomic"

	"github.com/dvoyni/cog/kernel/container"
)

// runner runs a kernel's jobs once their locks are held: pinned ones on
// the main thread, every other on the kernel's own workers. It also counts
// every job and publication still outstanding, so the kernel can drain: stop
// taking work, and wait out what it has.
type runner struct {
	// kernel owns the runner: its task table is what a job is run through, and
	// its supervisor runs its pinned jobs.
	kernel *Kernel

	// ready is every job whose locks are held, or that locks nothing,
	// waiting for a worker. Pinned jobs go to the Supervisor instead.
	ready container.Queue[uint32]

	// workers is every goroutine running jobs. A worker that finds ready
	// empty counts itself in idle and parks on wake, which has a slot per
	// worker: a job made ready while one is idle puts a token in it, so a
	// worker that parks after the job was pushed still wakes. stop is closed
	// to end them. wake, stop and drained are made by start.
	workers sync.WaitGroup
	idle    atomic.Int32
	wake    chan struct{}
	stop    chan struct{}

	// outstanding counts every job not yet finished and every publication
	// still running. closed is set as the kernel drains, and from then on add
	// answers false; the last outstanding job or publication then puts a
	// token in drained.
	outstanding atomic.Int64
	closed      atomic.Bool
	drained     chan struct{}
}

// start starts workerCount workers, which run ready jobs until drain ends
// them.
func (r *runner) start(workerCount int) {
	r.wake = make(chan struct{}, workerCount)
	r.stop = make(chan struct{})
	r.drained = make(chan struct{}, 1)
	for range workerCount {
		r.workers.Go(r.work)
	}
}

// add counts n more outstanding jobs or publications, and answers whether the
// kernel still takes work: false once drain has begun. Each of the n is
// settled once finished, whatever add answered. It counts before it checks,
// so drain never finds nothing outstanding while a publication add let
// through still runs.
func (r *runner) add(n int) bool {
	r.outstanding.Add(int64(n))
	return !r.closed.Load()
}

// settle marks one outstanding job or publication finished, waking drain at
// the last once the kernel is draining.
func (r *runner) settle() {
	if r.outstanding.Add(-1) == 0 && r.closed.Load() {
		select {
		case r.drained <- struct{}{}:
		default: // drain already has a token
		}
	}
}

// makeReady hands on the job in slot, a job of task whose locks are held: to
// the main thread if task is pinned there, otherwise to a worker.
func (r *runner) makeReady(slot, task uint32) {
	if r.kernel.taskLocks[task].pinnedToMainThread {
		r.kernel.supervisor.pushPinned(r.kernel, r.kernel.graph.handle(slot))
		return
	}
	r.ready.Push(slot)
	if r.idle.Load() > 0 {
		select {
		case r.wake <- struct{}{}:
		default: // every worker already has a token
		}
	}
}

// runPinned runs pinned job on the calling goroutine, the main thread, and
// finishes it as a worker would. Supervisor.TickMainThread calls it for each
// job the kernel handed over.
func (r *runner) runPinned(job JobHandle) {
	r.kernel.tasks[r.kernel.graph.task(job.slot)].runJob(r.kernel, job.slot)
}

// drain makes add answer false from now on, blocks until every job and
// publication still outstanding has finished, then ends the workers.
func (r *runner) drain() {
	r.closed.Store(true)
	if r.outstanding.Load() != 0 {
		<-r.drained
	}
	close(r.stop)
	r.workers.Wait()
}

// work is one worker: it runs ready jobs until stop is closed, parking on
// wake while there is none.
func (r *runner) work() {
	for {
		if slot, ok := r.ready.Pop(); ok {
			r.kernel.tasks[r.kernel.graph.task(slot)].runJob(r.kernel, slot)
			continue
		}
		// Counted idle before looking again, so a job pushed in between is
		// either seen here or wakes this worker.
		r.idle.Add(1)
		if r.ready.Len() == 0 {
			select {
			case <-r.wake:
			case <-r.stop:
				return
			}
		}
		r.idle.Add(-1)
	}
}
