package kernel

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countedRuns is a task that locks nothing, counts every run by slot, waits
// for gate if it is set, and completes its job as a real task does.
type countedRuns struct {
	runs [8192]atomic.Int32
	gate chan struct{}
}

func (c *countedRuns) runJob(kernel *Kernel, slot uint32) {
	c.runs[slot].Add(1)
	if c.gate != nil {
		<-c.gate
	}
	kernel.completeJob(slot, NoResult{})
}

// newTestRunner answers a partial kernel whose one task is runs, with its
// runner started on workerCount workers.
func newTestRunner(runs *countedRuns, workerCount int) *Kernel {
	k := newPartialKernel([]runnable{runs}, []*taskLocks{{}}, 0)
	k.runner.start(workerCount)
	return k
}

// makeJobs makes count jobs ready in k the way a publication does: counted
// outstanding, acquired in the graph, then submitted. It answers their slots.
func makeJobs(k *Kernel, count int) []uint32 {
	batch := make([]uint32, 0, count)
	for range count {
		batch = append(batch, k.graph.acquireJob(0, 0, JobHandle{}, nil).slot)
	}
	k.runner.add(count)
	k.graph.submit(batch)
	return batch
}

// drainAsync starts draining k and answers a channel closed once it returns.
func drainAsync(k *Kernel) chan struct{} {
	drained := make(chan struct{})
	go func() {
		k.runner.drain()
		close(drained)
	}()
	return drained
}

func returned(c chan struct{}, within time.Duration) bool {
	select {
	case <-c:
		return true
	case <-time.After(within):
		return false
	}
}

func TestDrainWaitsForAnOutstandingPublication(t *testing.T) {
	k := newTestRunner(&countedRuns{}, 1)
	// A job finishing before the drain leaves no early wake behind for it.
	makeJobs(k, 1)
	for deadline := time.Now().Add(time.Second); k.runner.outstanding.Load() != 0; {
		if time.Now().After(deadline) {
			t.Fatal("a ready job never ran")
		}
		time.Sleep(time.Millisecond)
	}
	if !k.runner.add(1) {
		t.Fatal("add refused a publication before draining")
	}
	drained := drainAsync(k)
	for !k.runner.closed.Load() {
		time.Sleep(time.Millisecond)
	}
	if k.runner.add(1) {
		t.Fatal("add let a publication through once draining began")
	}
	k.runner.settle() // the refused one settles too
	if returned(drained, 20*time.Millisecond) {
		t.Fatal("drain returned while a publication was outstanding")
	}
	k.runner.settle()
	if !returned(drained, time.Second) {
		t.Fatal("drain did not return once the publication settled")
	}
}

func TestDrainWaitsForQueuedJobs(t *testing.T) {
	runs := &countedRuns{gate: make(chan struct{})}
	k := newTestRunner(runs, 2)
	slots := makeJobs(k, 100)
	drained := drainAsync(k)
	if returned(drained, 20*time.Millisecond) {
		t.Fatal("drain returned while jobs were still running")
	}
	close(runs.gate)
	if !returned(drained, time.Second) {
		t.Fatal("drain did not return once the jobs finished")
	}
	for _, slot := range slots {
		if got := runs.runs[slot].Load(); got != 1 {
			t.Fatalf("job %d ran %d times, want once", slot, got)
		}
	}
}

// Jobs are made ready in bursts from many goroutines, with pauses long enough
// for the workers to park between them: every job runs exactly once, and none
// is left in the queue with every worker parked.
func TestEveryReadyJobRunsWhileWorkersPark(t *testing.T) {
	const makers, bursts, perBurst = 8, 40, 10
	runs := &countedRuns{}
	k := newTestRunner(runs, 4)
	var making sync.WaitGroup
	var made sync.Mutex
	var slots []uint32
	for range makers {
		making.Go(func() {
			for range bursts {
				burst := makeJobs(k, perBurst)
				made.Lock()
				slots = append(slots, burst...)
				made.Unlock()
				time.Sleep(100 * time.Microsecond)
			}
		})
	}
	making.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for k.runner.outstanding.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if left := k.runner.outstanding.Load(); left != 0 {
		t.Fatalf("%d jobs were left ready with the workers parked", left)
	}
	k.runner.drain()
	ran := map[uint32]int32{}
	for _, slot := range slots {
		ran[slot] = runs.runs[slot].Load()
	}
	// Slots are reused once their jobs finish, so a slot runs as many times
	// as jobs took it.
	took := map[uint32]int32{}
	for _, slot := range slots {
		took[slot]++
	}
	for slot, want := range took {
		if ran[slot] != want {
			t.Fatalf("slot %d ran %d times, want %d", slot, ran[slot], want)
		}
	}
}

// Publications run alongside a drain: none that add lets through is still
// running once drain has returned.
func TestNoPublicationRunsPastDrain(t *testing.T) {
	for range 200 {
		k := newTestRunner(&countedRuns{}, 1)
		var drainReturned, late atomic.Bool
		var publishing sync.WaitGroup
		for range 8 {
			publishing.Go(func() {
				for !drainReturned.Load() {
					open := k.runner.add(1)
					if open && drainReturned.Load() {
						late.Store(true)
					}
					k.runner.settle()
					if !open {
						return
					}
				}
			})
		}
		k.runner.drain()
		drainReturned.Store(true)
		publishing.Wait()
		if late.Load() {
			t.Fatal("a publication add let through was still running after drain returned")
		}
	}
}
