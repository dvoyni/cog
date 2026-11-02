package kernel

import (
	"sync"
	"testing"

	"github.com/dvoyni/cog/kernel/container"
)

// fedArgs is a task taking an int32 that keeps every arg fed to it, by arg
// index, and runs nothing.
type fedArgs struct {
	args [8]int32
}

func (*fedArgs) runJob(kernel *Kernel, slot uint32) {}

func (f *fedArgs) receiveArg(index uint32, arg int32) { f.args[index] = arg }

// readyCounts counts how many times the job graph handed on each slot, from
// its kernel's ready queue.
type readyCounts struct {
	kernel *Kernel
	counts [4096]int32
	buffer []uint32
}

// of answers how many times slot has been handed on so far.
func (r *readyCounts) of(slot uint32) int32 {
	r.collect()
	return r.counts[slot]
}

// collect counts every slot waiting in the ready queue, and empties it.
func (r *readyCounts) collect() {
	r.buffer = r.kernel.runner.ready.Drain(r.buffer[:0])
	for _, slot := range r.buffer {
		r.counts[slot]++
	}
}

// newTestGraph answers the job graph of a partial kernel whose one task,
// index 0, locks nothing and is fed, and the count of what it hands on.
func newTestGraph(fed *fedArgs) (*jobGraph, *readyCounts) {
	k := newPartialKernel([]runnable{fed}, []*taskLocks{{}}, 0)
	return &k.graph, &readyCounts{kernel: k}
}

func signalled(sig *Signal) bool {
	select {
	case <-sig.C():
		return true
	default:
		return false
	}
}

func TestJobWaitsForItsBatch(t *testing.T) {
	g, ready := newTestGraph(&fedArgs{})
	job := g.acquireJob(0, 0, JobHandle{}, nil)
	if job.slot == container.None || ready.of(job.slot) != 0 {
		t.Fatalf("a job was handed on before its batch was submitted")
	}
	g.submit([]uint32{job.slot})
	if ready.of(job.slot) != 1 {
		t.Fatalf("submitting the batch handed the job on %d times, want once", ready.of(job.slot))
	}
}

func TestJobWaitsForEveryDependencyAndIsFed(t *testing.T) {
	fed := &fedArgs{}
	g, ready := newTestGraph(fed)
	from := g.acquireJob(0, 0, JobHandle{}, nil)
	dep := g.acquireJob(0, 0, JobHandle{}, nil)
	successor := g.acquireJob(0, 3, from, []JobHandle{dep})
	g.submit([]uint32{from.slot, dep.slot, successor.slot})
	if ready.of(successor.slot) != 0 {
		t.Fatal("the successor was handed on before its dependencies finished")
	}
	g.finishJob(from.slot, int32(7))
	if ready.of(successor.slot) != 0 {
		t.Fatal("the successor was handed on before its last dependency finished")
	}
	if fed.args[3] != 7 {
		t.Fatalf("the successor was fed %d, want 7", fed.args[3])
	}
	g.finishJob(dep.slot, int32(9))
	if ready.of(successor.slot) != 1 {
		t.Fatalf("the successor was handed on %d times, want once", ready.of(successor.slot))
	}
	if fed.args[3] != 7 {
		t.Fatalf("a plain dependency fed the successor %d", fed.args[3])
	}
}

func TestFinishedDependencyIsNotWaitedFor(t *testing.T) {
	g, ready := newTestGraph(&fedArgs{})
	finished := g.acquireJob(0, 0, JobHandle{}, nil)
	g.submit([]uint32{finished.slot})
	g.finishJob(finished.slot, int32(0))
	// The successor may take finished's slot, so it counts from here.
	successor := g.acquireJob(0, 0, finished, []JobHandle{finished, {}})
	before := ready.of(successor.slot)
	g.submit([]uint32{successor.slot})
	if ready.of(successor.slot) != before+1 {
		t.Fatal("a successor of finished and zero handles waited")
	}
}

func TestStaleHandleDoesNotNameTheSlotsNextJob(t *testing.T) {
	g, ready := newTestGraph(&fedArgs{})
	stale := g.acquireJob(0, 0, JobHandle{}, nil)
	g.submit([]uint32{stale.slot})
	g.finishJob(stale.slot, int32(0))
	current := g.acquireJob(0, 0, JobHandle{}, nil)
	if current.slot != stale.slot || current == stale {
		t.Fatalf("slot %d reused with handle %v, stale %v", stale.slot, current, stale)
	}
	ofStale := g.acquireJob(0, 0, JobHandle{}, []JobHandle{stale})
	ofCurrent := g.acquireJob(0, 0, JobHandle{}, []JobHandle{current})
	g.submit([]uint32{ofStale.slot, ofCurrent.slot})
	if ready.of(ofStale.slot) != 1 || ready.of(ofCurrent.slot) != 0 {
		t.Fatal("a stale handle was taken for the slot's next job")
	}
}

func TestSignalIsSentOnFinish(t *testing.T) {
	g, _ := newTestGraph(&fedArgs{})
	job := g.acquireJob(0, 0, JobHandle{}, nil)
	sig := NewSignal()
	if !g.armSignal(job, sig) {
		t.Fatal("armSignal refused a job not yet finished")
	}
	if signalled(sig) {
		t.Fatal("the signal was sent before the job finished")
	}
	g.finishJob(job.slot, int32(0))
	if !signalled(sig) {
		t.Fatal("finishing the job did not send the signal")
	}
	if g.armSignal(job, sig) || g.armSignal(JobHandle{}, sig) {
		t.Fatal("armSignal armed a finished job")
	}
}

// Every successor and Signal added while the predecessor finishes is either
// linked and reached by the finish, or refused and handed on at once: none is
// lost, none handed on twice.
func TestArmingRacesFinish(t *testing.T) {
	for range 200 {
		g, ready := newTestGraph(&fedArgs{})
		// Never submitted, so a racer reusing its slot after the finish is the
		// only job ever handed on from it.
		predecessor := g.acquireJob(0, 0, JobHandle{}, nil)
		const racers = 32
		successors := make([]uint32, racers)
		signals := make([]*Signal, racers)
		armed := make([]bool, racers)
		for i := range racers {
			signals[i] = NewSignal()
		}
		var wait sync.WaitGroup
		for i := range racers {
			wait.Go(func() {
				successor := g.acquireJob(0, 0, JobHandle{}, []JobHandle{predecessor})
				successors[i] = successor.slot
				g.submit([]uint32{successor.slot})
				armed[i] = g.armSignal(predecessor, signals[i])
			})
		}
		g.finishJob(predecessor.slot, int32(0))
		wait.Wait()
		for i := range racers {
			if got := ready.of(successors[i]); got != 1 {
				t.Fatalf("successor %d was handed on %d times, want once", i, got)
			}
			if armed[i] && !signalled(signals[i]) {
				t.Fatalf("signal %d was armed but not sent", i)
			}
		}
	}
}

func TestJobGraphAllocatesNothingOnceWarm(t *testing.T) {
	g, ready := newTestGraph(&fedArgs{})
	sig := NewSignal()
	batch := make([]uint32, 0, 2)
	cycle := func() {
		predecessor := g.acquireJob(0, 0, JobHandle{}, nil)
		successor := g.acquireJob(0, 0, predecessor, nil)
		g.submit(append(batch[:0], predecessor.slot, successor.slot))
		g.armSignal(predecessor, sig)
		g.finishJob(predecessor.slot, int32(1))
		<-sig.C()
		g.finishJob(successor.slot, int32(0))
		ready.collect()
	}
	cycle()
	if got := testing.AllocsPerRun(1000, cycle); got != 0 {
		t.Fatalf("a job graph cycle allocated %v times", got)
	}
}

// A stale handle racing the slot's finish and reuse never makes a job wait
// for the job that holds the slot now.
func TestStaleHandleRacingReuse(t *testing.T) {
	for range 200 {
		g, ready := newTestGraph(&fedArgs{})
		// Never submitted, so a racer reusing its slot is the only job ever
		// handed on from it.
		stale := g.acquireJob(0, 0, JobHandle{}, nil)
		const racers = 16
		successors := make([]uint32, racers)
		var wait sync.WaitGroup
		for i := range racers {
			wait.Go(func() {
				successor := g.acquireJob(0, 0, JobHandle{}, []JobHandle{stale})
				successors[i] = successor.slot
				g.submit([]uint32{successor.slot})
			})
		}
		g.finishJob(stale.slot, int32(0))
		reused := g.acquireJob(0, 0, JobHandle{}, nil)
		wait.Wait()
		for i := range racers {
			if got := ready.of(successors[i]); got != 1 {
				t.Fatalf("successor %d was handed on %d times, want once", i, got)
			}
		}
		if reused.slot == stale.slot {
			g.submit([]uint32{reused.slot})
			g.finishJob(reused.slot, int32(0))
			for i := range racers {
				if got := ready.of(successors[i]); got != 1 {
					t.Fatalf("finishing the slot's next job handed successor %d on again", i)
				}
			}
		}
	}
}
