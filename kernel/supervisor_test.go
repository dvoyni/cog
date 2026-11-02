package kernel

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestSpawnAssignsFreshIDs(t *testing.T) {
	s := NewSupervisor(nil)
	first, err := s.Spawn(nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Spawn(nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first == 0 || second <= first {
		t.Fatalf("Spawn answered %d then %d, want increasing non-zero IDs", first, second)
	}
	if k := s.GetKernel(first); k == nil || k.ID() != first {
		t.Fatalf("GetKernel(%d) answered %v", first, k)
	}
}

func TestDestroyRemovesKernel(t *testing.T) {
	s := NewSupervisor(nil)
	id, _ := s.Spawn(nil, 2)
	s.Destroy(id)
	if s.GetKernel(id) != nil {
		t.Fatal("GetKernel answered a kernel being destroyed")
	}
	s.Destroy(id) // not live: does nothing
	select {
	case <-s.Done():
		t.Fatal("destroying the last kernel closed Done")
	default:
	}
}

func TestQuitClosesDoneOnceKernelsAreGone(t *testing.T) {
	s := NewSupervisor(nil)
	for range 3 {
		if _, err := s.Spawn(nil, 2); err != nil {
			t.Fatal(err)
		}
	}
	s.Quit()
	s.Quit()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done never closed")
	}
	if len(s.kernels) != 0 {
		t.Fatal("kernels left after Done")
	}
	if _, err := s.Spawn(nil, 2); !errors.Is(err, ErrQuitting) {
		t.Fatalf("Spawn after Quit answered %v, want ErrQuitting", err)
	}
	if s.WaitMainThread() {
		t.Fatal("WaitMainThread answered true after Done")
	}
}

func TestQuitWithNoKernelsClosesDone(t *testing.T) {
	s := NewSupervisor(nil)
	s.Quit()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done never closed")
	}
}

func TestBroadcastFollowsSpawnAndDestroy(t *testing.T) {
	s := NewSupervisor(nil)
	first, _ := s.Spawn(nil, 2)
	second, _ := s.Spawn(nil, 2)
	third, _ := s.Spawn(nil, 2)
	before := s.spawnOrder
	s.Destroy(second)
	ids := []KernelID{}
	for _, k := range s.spawnOrder {
		ids = append(ids, k.ID())
	}
	if !slices.Equal(ids, []KernelID{first, third}) {
		t.Fatalf("spawn order after destroy is %v, want [%d %d]", ids, first, third)
	}
	if len(before) != 3 || before[1].ID() != second {
		t.Fatal("destroy changed a spawn order a Broadcast may still hold")
	}
	if got := testing.AllocsPerRun(100, func() { s.Broadcast(struct{ Value int32 }{1}) }); got != 0 {
		t.Fatalf("Broadcast allocated %v times", got)
	}
}

func TestPinnedJobWakesMainThread(t *testing.T) {
	s := NewSupervisor(nil)
	id, _ := s.Spawn(nil, 2)
	woke := make(chan bool)
	go func() { woke <- s.WaitMainThread() }()
	s.pushPinned(s.GetKernel(id), JobHandle{})
	select {
	case ok := <-woke:
		if !ok {
			t.Fatal("WaitMainThread answered false before Done")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WaitMainThread never woke")
	}
}

func TestWaitMainThreadWakesOnQuit(t *testing.T) {
	s := NewSupervisor(nil)
	woke := make(chan bool)
	go func() { woke <- s.WaitMainThread() }()
	s.Quit()
	select {
	case ok := <-woke:
		if ok {
			t.Fatal("WaitMainThread answered true on Quit")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WaitMainThread never woke")
	}
}

func TestConcurrentSpawnDestroyQuit(t *testing.T) {
	s := NewSupervisor(nil)
	var wait sync.WaitGroup
	for range 8 {
		wait.Go(func() {
			for range 50 {
				id, err := s.Spawn(nil, 2)
				if err != nil {
					return // quitting
				}
				s.Broadcast(struct{ Value int32 }{1})
				s.Destroy(id)
			}
		})
	}
	time.Sleep(time.Millisecond)
	s.Quit()
	wait.Wait()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done never closed")
	}
}

func TestSpawnWithNoWorkersPanics(t *testing.T) {
	expectPanic(t, "no workers", func() { NewSupervisor(nil).Spawn(nil, 0) })
}
