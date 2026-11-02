package kernel

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dvoyni/cog/kernel/internal/concurrent"
)

func newTestKernel() *Kernel { return newKernel(NewSupervisor(nil), 1, nil, 1, nil) }

// successorsOf walks a successor list finishJob answered, oldest first.
func successorsOf(s *jobStorage, head uint32) []successorEdge {
	var edges []successorEdge
	for head != concurrent.None {
		edge := *s.successorEdges.At(head)
		edges = append([]successorEdge{edge}, edges...)
		head = edge.next
	}
	return edges
}

func signalled(sig *Signal) bool {
	select {
	case <-sig.C():
		return true
	default:
		return false
	}
}

func TestNewJobWaitsOnItsHold(t *testing.T) {
	s := &jobStorage{}
	slot, handle := s.acquireJob(0, concurrent.None)
	if slot == concurrent.None || handle.slot != slot {
		t.Fatalf("acquireJob answered slot %d, handle %v", slot, handle)
	}
	if got := s.jobs.At(slot).waiting.Load(); got != 1 {
		t.Fatalf("a new job waits on %d, want 1", got)
	}
}

func TestAddSuccessorRaisesWaiting(t *testing.T) {
	s := &jobStorage{}
	_, first := s.acquireJob(0, concurrent.None)
	_, second := s.acquireJob(0, concurrent.None)
	successor, _ := s.acquireJob(0, concurrent.None)
	if !s.addSuccessor(first, successor, true) || !s.addSuccessor(second, successor, false) {
		t.Fatal("addSuccessor refused a running predecessor")
	}
	if got := s.jobs.At(successor).waiting.Load(); got != 3 {
		t.Fatalf("successor waits on %d, want its hold and two predecessors", got)
	}
	edges := successorsOf(s, s.finishJob(first.slot))
	if len(edges) != 1 || edges[0].successor != successor || !edges[0].feedsResult {
		t.Fatalf("first's successors are %v", edges)
	}
}

func TestFinishedPredecessorIsNotWaitedFor(t *testing.T) {
	s := &jobStorage{}
	slot, finished := s.acquireJob(0, concurrent.None)
	s.releaseSuccessors(s.finishJob(slot))
	successor, _ := s.acquireJob(0, concurrent.None)
	if s.addSuccessor(finished, successor, false) {
		t.Fatal("addSuccessor linked a finished job")
	}
	s.releaseJob(slot)
	if s.addSuccessor(finished, successor, false) {
		t.Fatal("addSuccessor linked a released job")
	}
	if s.addSuccessor(JobHandle{}, successor, false) {
		t.Fatal("addSuccessor linked the zero JobHandle")
	}
	if got := s.jobs.At(successor).waiting.Load(); got != 1 {
		t.Fatalf("successor waits on %d, want only its hold", got)
	}
}

func TestStaleHandleDoesNotNameTheSlotsNextJob(t *testing.T) {
	s := &jobStorage{}
	slot, stale := s.acquireJob(0, concurrent.None)
	s.releaseSuccessors(s.finishJob(slot))
	s.releaseJob(slot)
	reused, current := s.acquireJob(0, concurrent.None)
	if reused != slot || current == stale {
		t.Fatalf("slot %d reused as %d with handle %v, stale %v", slot, reused, current, stale)
	}
	successor, _ := s.acquireJob(0, concurrent.None)
	if s.addSuccessor(stale, successor, false) || !s.addSuccessor(current, successor, false) {
		t.Fatal("a stale handle was taken for the slot's next job")
	}
}

func TestNotifySignalsOnFinish(t *testing.T) {
	k := newTestKernel()
	slot, handle := k.jobs.acquireJob(0, concurrent.None)
	sig := NewSignal()
	k.Notify(sig, handle)
	if signalled(sig) {
		t.Fatal("Notify signalled before the job finished")
	}
	k.jobs.finishJob(slot)
	if !signalled(sig) {
		t.Fatal("finishing the job did not signal")
	}
}

func TestNotifyFinishedJobSignalsAtOnce(t *testing.T) {
	k := newTestKernel()
	sig := NewSignal()
	k.Notify(sig, JobHandle{})
	if !signalled(sig) {
		t.Fatal("Notify on the zero JobHandle did not signal at once")
	}
	slot, handle := k.jobs.acquireJob(0, concurrent.None)
	k.jobs.finishJob(slot)
	k.Notify(sig, handle)
	if !signalled(sig) {
		t.Fatal("Notify on a finished job did not signal at once")
	}
	k.jobs.releaseJob(slot)
	k.Notify(sig, handle)
	if !signalled(sig) {
		t.Fatal("Notify on a released job did not signal at once")
	}
}

// Every successor and Signal added while the predecessor finishes is either
// linked and reached by the finish, or refused: none is lost, none counted
// twice.
func TestArmingRacesFinish(t *testing.T) {
	for range 200 {
		k := newTestKernel()
		predecessor, handle := k.jobs.acquireJob(0, concurrent.None)
		const racers = 32
		successors := make([]uint32, racers)
		signals := make([]*Signal, racers)
		for i := range racers {
			successors[i], _ = k.jobs.acquireJob(0, concurrent.None)
			signals[i] = NewSignal()
		}
		var linked atomic.Int32
		var wait sync.WaitGroup
		for i := range racers {
			wait.Go(func() {
				if k.jobs.addSuccessor(handle, successors[i], false) {
					linked.Add(1)
				}
				k.Notify(signals[i], handle)
			})
		}
		head := k.jobs.finishJob(predecessor)
		wait.Wait()
		edges := successorsOf(&k.jobs, head)
		if len(edges) != int(linked.Load()) {
			t.Fatalf("finish reached %d successors, %d were linked", len(edges), linked.Load())
		}
		for i, sig := range signals {
			if !signalled(sig) {
				t.Fatalf("signal %d was neither signalled by the finish nor at once", i)
			}
		}
	}
}

func TestJobStorageAllocatesNothingOnceWarm(t *testing.T) {
	k := newTestKernel()
	sig := NewSignal()
	cycle := func() {
		predecessor, handle := k.jobs.acquireJob(0, concurrent.None)
		successor, _ := k.jobs.acquireJob(0, concurrent.None)
		k.jobs.addSuccessor(handle, successor, true)
		k.Notify(sig, handle)
		k.jobs.releaseSuccessors(k.jobs.finishJob(predecessor))
		<-sig.C()
		k.jobs.releaseSuccessors(k.jobs.finishJob(successor))
		k.jobs.releaseJob(predecessor)
		k.jobs.releaseJob(successor)
	}
	cycle()
	if got := testing.AllocsPerRun(1000, cycle); got != 0 {
		t.Fatalf("a job's storage cycle allocated %v times", got)
	}
}

// A stale handle racing the slot's release and reuse never links to the job that
// holds the slot now.
func TestStaleHandleRacingReuse(t *testing.T) {
	for range 200 {
		s := &jobStorage{}
		slot, stale := s.acquireJob(0, concurrent.None)
		const racers = 16
		successors := make([]uint32, racers)
		for i := range racers {
			successors[i], _ = s.acquireJob(0, concurrent.None)
		}
		var wait sync.WaitGroup
		for i := range racers {
			wait.Go(func() { s.addSuccessor(stale, successors[i], false) })
		}
		s.releaseSuccessors(s.finishJob(slot))
		s.releaseJob(slot)
		reused, _ := s.acquireJob(0, concurrent.None)
		wait.Wait()
		if reused != slot {
			t.Fatalf("slot %d was not reused, got %d", slot, reused)
		}
		if edges := successorsOf(s, s.finishJob(reused)); len(edges) != 0 {
			t.Fatalf("stale handles linked %d successors to the slot's next job", len(edges))
		}
	}
}
