package concurrent

import (
	"slices"
	"sync"
	"testing"
)

type slot struct{ value uint64 }

func TestPoolHandsOutEachSlotOnce(t *testing.T) {
	p := &Pool[slot]{}
	seen := map[uint32]bool{}
	for range 3*chunkSize + 7 {
		i := p.Acquire()
		if i == None || seen[i] {
			t.Fatalf("Acquire answered %d twice or None", i)
		}
		seen[i] = true
	}
}

func TestPoolReusesPutSlots(t *testing.T) {
	p := NewPool[slot](1)
	i := p.Acquire()
	p.At(i).value = 42
	p.Release(i)
	if j := p.Acquire(); j != i || p.At(j).value != 42 {
		t.Fatalf("Acquire answered %d holding %d, want %d holding 42", j, p.At(j).value, i)
	}
}

func TestNewPoolPreallocates(t *testing.T) {
	p := NewPool[slot](1000)
	// AllocsPerRun runs once to warm up, then once measured: 1000 Acquires in all.
	got := testing.AllocsPerRun(1, func() {
		for range 500 {
			p.Acquire()
		}
	})
	if got != 0 {
		t.Fatalf("Acquires from a pool of 1000 allocated %v times", got)
	}
}

func TestPoolAllocatesNothingOnceWarm(t *testing.T) {
	p := NewPool[slot](1)
	if got := testing.AllocsPerRun(1000, func() { p.Release(p.Acquire()) }); got != 0 {
		t.Fatalf("Acquire and Release allocated %v times", got)
	}
}

func TestPoolAcquireManyReleaseMany(t *testing.T) {
	p := &Pool[slot]{}
	held := p.AcquireMany(nil, chunkSize+10) // spans a growth
	seen := map[uint32]bool{}
	for _, i := range held {
		if i == None || seen[i] {
			t.Fatalf("AcquireMany answered %d twice or None", i)
		}
		seen[i] = true
	}
	p.ReleaseMany(held)
	again := p.AcquireMany(nil, len(held))
	for _, i := range again {
		if !seen[i] {
			t.Fatalf("AcquireMany after ReleaseMany answered %d, never handed out before", i)
		}
	}
	if got := testing.AllocsPerRun(100, func() {
		p.ReleaseMany(again)
		again = p.AcquireMany(again[:0], len(held))
	}); got != 0 {
		t.Fatalf("AcquireMany and ReleaseMany allocated %v times", got)
	}
}

func TestPoolConcurrentAcquireRelease(t *testing.T) {
	p := &Pool[slot]{}
	var holders sync.Map
	var wait sync.WaitGroup
	for g := range 8 {
		wait.Go(func() {
			held := []uint32{}
			for r := range 20000 {
				if r%3 == 2 && len(held) > 0 {
					i := held[len(held)-1]
					held = held[:len(held)-1]
					holders.Delete(i)
					p.Release(i)
					continue
				}
				i := p.Acquire()
				if other, taken := holders.LoadOrStore(i, g); taken {
					t.Errorf("slot %d held by %d and %d at once", i, other, g)
				}
				held = append(held, i)
			}
		})
	}
	wait.Wait()
}

func TestQueueIsFIFOAcrossGrowth(t *testing.T) {
	q := &Queue[uint32]{}
	next := uint32(1)
	want := uint32(1)
	// Interleave so the ring wraps before it grows.
	for round := range 50 {
		for range round + 3 {
			q.Push(next)
			next++
		}
		for range round + 1 {
			if got, _ := q.Pop(); got != want {
				t.Fatalf("Pop answered %d, want %d", got, want)
			}
			want++
		}
	}
	for _, got := range q.Drain(nil) {
		if got != want {
			t.Fatalf("Drain answered %d, want %d", got, want)
		}
		want++
	}
	if _, ok := q.Pop(); want != next || ok {
		t.Fatalf("queue lost or kept indexes: drained up to %d, pushed up to %d", want, next)
	}
}

func TestQueueClearsTakenSlots(t *testing.T) {
	q := &Queue[*int]{}
	for range 10 {
		q.Push(new(int))
	}
	for range 10 {
		q.Pop()
	}
	// Pushed past the end of the ring, so the Drain below wraps.
	for range 12 {
		q.Push(new(int))
	}
	q.Pop()
	q.Drain(nil)
	for i, item := range q.ring {
		if item != nil {
			t.Fatalf("ring slot %d still holds a taken item", i)
		}
	}
}

func TestQueueCloseRefusesPushes(t *testing.T) {
	q := &Queue[uint32]{}
	q.Push(1)
	q.Push(2)
	if got := q.Close(nil); !slices.Equal(got, []uint32{1, 2}) {
		t.Fatalf("Close answered %v, want [1 2]", got)
	}
	if !q.Closed() || q.Push(3) {
		t.Fatal("push to a closed queue succeeded")
	}
	if len(q.Close(nil)) != 0 {
		t.Fatal("closing a closed queue answered indexes")
	}
	q.Reset()
	if q.Closed() || !q.Push(3) {
		t.Fatal("a reset queue refused a push")
	}
	if got, ok := q.Pop(); !ok || got != 3 {
		t.Fatalf("a reset queue answered %d, %v, want 3, true", got, ok)
	}
}

func TestQueueConcurrentConservesIndexes(t *testing.T) {
	const pushers, each = 8, 5000
	q := &Queue[uint32]{}
	var wait sync.WaitGroup
	for g := range pushers {
		wait.Go(func() {
			for k := range each {
				q.Push(uint32(g*each + k + 1))
			}
		})
	}
	seen := make([]bool, pushers*each+1)
	count := 0
	take := func(i uint32) {
		if seen[i] {
			t.Fatalf("index %d taken twice", i)
		}
		seen[i] = true
		count++
	}
	done := make(chan struct{})
	go func() { wait.Wait(); close(done) }()
	var into []uint32
	for running := true; running; {
		select {
		case <-done:
			running = false
		default:
		}
		if i, ok := q.Pop(); ok {
			take(i)
		}
		into = q.Drain(into[:0])
		for _, i := range into {
			take(i)
		}
	}
	for _, i := range q.Drain(into[:0]) {
		take(i)
	}
	if count != pushers*each {
		t.Fatalf("took %d indexes, pushed %d", count, pushers*each)
	}
}

func TestQueueRacingCloseLosesNothing(t *testing.T) {
	for range 200 {
		q := &Queue[uint32]{}
		var accepted [64]bool
		var wait sync.WaitGroup
		for k := range 64 {
			wait.Go(func() { accepted[k] = q.Push(uint32(k + 1)) })
		}
		taken := len(q.Close(nil))
		wait.Wait()
		want := 0
		for _, ok := range accepted {
			if ok {
				want++
			}
		}
		if taken != want {
			t.Fatalf("Close took %d indexes, %d pushes succeeded", taken, want)
		}
	}
}

func TestQueueAllocatesNothingOnceGrown(t *testing.T) {
	q := &Queue[uint32]{}
	into := make([]uint32, 0, 64)
	for range 64 {
		q.Push(1)
	}
	q.Drain(into)
	got := testing.AllocsPerRun(1000, func() {
		for range 64 {
			q.Push(1)
		}
		q.Pop()
		into = q.Drain(into[:0])
	})
	if got != 0 {
		t.Fatalf("Push, Pop and Drain allocated %v times", got)
	}
}

func BenchmarkPoolAcquireRelease(b *testing.B) {
	p := NewPool[slot](1)
	for b.Loop() {
		p.Release(p.Acquire())
	}
}

func BenchmarkPoolAcquireReleaseParallel(b *testing.B) {
	p := NewPool[slot](1024)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p.Release(p.Acquire())
		}
	})
}

func BenchmarkQueuePushPop(b *testing.B) {
	q := &Queue[uint32]{}
	for b.Loop() {
		q.Push(1)
		q.Pop()
	}
}

func BenchmarkQueuePushPopParallel(b *testing.B) {
	q := &Queue[uint32]{}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			q.Push(1)
			q.Pop()
		}
	})
}

func BenchmarkQueuePushParallelDrain(b *testing.B) {
	q := &Queue[uint32]{}
	b.RunParallel(func(pb *testing.PB) {
		var into []uint32
		pushes := 0
		for pb.Next() {
			if !q.Push(1) {
				b.Fatal("push failed")
			}
			if pushes++; pushes%64 == 0 { // stands in for the consumer
				into = q.Drain(into[:0])
			}
		}
	})
}

// A publication's shape: take a batch of slots, use them, give them back, one
// lock per slot.
func BenchmarkPoolBatchParallel(b *testing.B) {
	p := NewPool[slot](4096)
	b.RunParallel(func(pb *testing.PB) {
		var held [16]uint32
		for pb.Next() {
			for k := range held {
				held[k] = p.Acquire()
				p.At(held[k]).value++
			}
			for _, i := range held {
				p.Release(i)
			}
		}
	})
}

// The same batch, one lock to take it and one to give it back.
func BenchmarkPoolManyParallel(b *testing.B) {
	p := NewPool[slot](4096)
	b.RunParallel(func(pb *testing.PB) {
		held := make([]uint32, 0, 16)
		for pb.Next() {
			held = p.AcquireMany(held[:0], 16)
			for _, i := range held {
				p.At(i).value++
			}
			p.ReleaseMany(held)
		}
	})
}
