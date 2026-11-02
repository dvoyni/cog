package kernel

import (
	"sync"

	"github.com/dvoyni/cog/kernel/internal/concurrent"
)

// lockRequest is a job asking the lock combiner for its task's locks.
type lockRequest struct {
	slot, task uint32
}

// lockCombiner grants jobs the locks of their tasks. A job whose dependencies
// have all finished arrives, and is handed to the kernel's runner once its
// locks are held,
// or at once if it locks nothing; a job that finishes releases them. Jobs
// whose locks conflict are granted one at a time, oldest first, and a waiting
// writer reserves what it writes, so no job that arrived after it locks that
// first.
//
// Whichever goroutine arrives or releases becomes the combiner and does the
// work, unless another already is: then it leaves its request queued for that
// one and returns at once. Nothing waits for the combiner.
type lockCombiner struct {
	// kernel owns the combiner: its lock table is what the combiner reads a
	// task's locks from, and its runner is handed every job whose locks are
	// held, by whichever goroutine is the combiner, or by the arriving one for
	// a job that locks nothing.
	kernel *Kernel

	// arrivals is every job that arrived and locks something, and releases
	// the task of every finished job whose locks are not unlocked yet, both
	// waiting for the combiner.
	arrivals concurrent.Queue[lockRequest]
	releases concurrent.Queue[uint32]

	// mutex is held by whichever goroutine is the combiner. It is only ever
	// tried, never waited for.
	mutex sync.Mutex
	// Everything below is touched only while mutex is held. locks is set
	// once registration ends, sized to the kernel's resources.
	locks lockState
	// waiting is every arrived job whose locks are not free yet, oldest
	// first.
	waiting []lockRequest
	// arrived and released are the buffers arrivals and releases drain into,
	// kept between passes.
	arrived  []lockRequest
	released []uint32
}

// arrive asks for the locks of task for the job in slot, whose dependencies
// have all finished. The job is handed to the runner once they are held, which
// may be before arrive returns, or later on another goroutine.
func (c *lockCombiner) arrive(slot, task uint32) {
	if c.kernel.taskLocks[task].locksNothing() {
		c.kernel.runner.makeReady(slot, task)
		return
	}
	c.arrivals.Push(lockRequest{slot: slot, task: task})
	c.combine()
}

// release gives back the locks of task, held by a job that has finished.
func (c *lockCombiner) release(task uint32) {
	if c.kernel.taskLocks[task].locksNothing() {
		return
	}
	c.releases.Push(task)
	c.combine()
}

// combine becomes the combiner, unless another goroutine is: then it returns
// at once, and that combiner does the work. The combiner unlocks the locks of
// every release, then grants waiting jobs their locks, oldest first, each
// whose locks are all free; a waiting writer reserves what it writes, so no
// job behind it locks that before it does.
//
// Before giving the combiner up it looks again, so a request queued while it
// was busy, by a goroutine that then failed to become combiner, is never left
// behind.
func (c *lockCombiner) combine() {
	for c.mutex.TryLock() {
		c.released = c.releases.Drain(c.released[:0])
		for _, task := range c.released {
			c.locks.unlock(c.kernel.taskLocks[task])
		}
		c.arrived = c.arrivals.Drain(c.arrived[:0])
		c.waiting = append(c.waiting, c.arrived...)

		c.locks.clearReservations()
		stillWaiting := c.waiting[:0]
		for _, request := range c.waiting {
			locks := c.kernel.taskLocks[request.task]
			if c.locks.canLock(locks) {
				c.locks.lock(locks)
				c.kernel.runner.makeReady(request.slot, request.task)
				continue
			}
			c.locks.reserve(locks)
			stillWaiting = append(stillWaiting, request)
		}
		c.waiting = stillWaiting
		c.mutex.Unlock()

		if c.releases.Len() == 0 && c.arrivals.Len() == 0 {
			return
		}
	}
}
