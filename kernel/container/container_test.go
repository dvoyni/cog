package container

import (
	"reflect"
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

func TestObjectTableGrowsToHoldAnIndex(t *testing.T) {
	table := &ObjectTable[slot]{}
	if held := table.At(0); held != nil {
		t.Fatalf("an empty table holds slot 0 at %p", held)
	}
	far := uint32(3*chunkSize + 7)
	held := table.Grow(far)
	held.value = 42
	if table.At(far) != held || table.At(0) == nil {
		t.Fatalf("after Grow(%d), At answered %p and slot 0 %p, want %p and a slot", far, table.At(far), table.At(0), held)
	}
	if past := table.At(4 * chunkSize); past != nil {
		t.Errorf("slot %d past the chunks grown is held at %p", 4*chunkSize, past)
	}
	table.Grow(10 * chunkSize)
	if table.At(far) != held || held.value != 42 {
		t.Errorf("growing further moved slot %d", far)
	}
}

func TestObjectTableSlotsYieldsEverySlotGrownInIndexOrder(t *testing.T) {
	table := &ObjectTable[slot]{}
	for range table.Slots() {
		t.Fatal("an empty table yields a slot")
	}
	table.Grow(chunkSize + 1).value = 7
	var yielded []*slot
	for held := range table.Slots() {
		yielded = append(yielded, held)
	}
	if len(yielded) != 2*chunkSize {
		t.Fatalf("Slots yielded %d slots, want the %d of the two chunks grown", len(yielded), 2*chunkSize)
	}
	for index, held := range yielded {
		if held != table.At(uint32(index)) {
			t.Fatalf("Slots' slot %d is %p, not At's %p", index, held, table.At(uint32(index)))
		}
	}
	for range table.Slots() {
		break
	}
}

func TestObjectTableConcurrentGrowLosesNoSlot(t *testing.T) {
	table := &ObjectTable[slot]{}
	const writerCount = 8
	const indexCount = writerCount * 4 * chunkSize
	var wait sync.WaitGroup
	for writer := range writerCount {
		wait.Go(func() {
			for index := writer; index < indexCount; index += writerCount {
				table.Grow(uint32(index)).value = uint64(index) + 1
			}
		})
	}
	wait.Wait()
	for index := range indexCount {
		if held := table.At(uint32(index)); held == nil || held.value != uint64(index)+1 {
			t.Fatalf("slot %d lost its value", index)
		}
	}
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

func TestArrayAppendResizeSwapRemove(t *testing.T) {
	a := &Array[uint64]{}
	for i := range 2*chunkSize + 3 {
		if got := a.Append(uint64(i)); got != i {
			t.Fatalf("Append answered position %d, want %d", got, i)
		}
	}
	first := a.At(0)
	a.Append(99) // pointers survive growth
	if first != a.At(0) || *first != 0 {
		t.Fatal("growing moved an item")
	}
	last := a.Len() - 1
	if moved := a.SwapRemove(1); moved != last || *a.At(1) != 99 || a.Len() != last {
		t.Fatalf("SwapRemove moved %d holding %d, want %d holding 99", moved, *a.At(1), last)
	}
	a.Resize(2)
	a.Resize(chunkSize + 1)
	if *a.At(2) != 0 || *a.At(chunkSize) != 0 {
		t.Fatal("Resize up kept items cut off before")
	}
}

func TestArrayChunksCoverEveryItem(t *testing.T) {
	a := &Array[int]{}
	for i := range 2*chunkSize + 5 {
		a.Append(i)
	}
	a.Resize(chunkSize + 7)
	want := 0
	for c := range a.Chunks() {
		for _, got := range a.Chunk(c) {
			if got != want {
				t.Fatalf("chunk walk answered %d, want %d", got, want)
			}
			want++
		}
	}
	if want != a.Len() {
		t.Fatalf("chunk walk visited %d items, the array holds %d", want, a.Len())
	}
}

func TestArrayShrinkReleasesChunksPastLen(t *testing.T) {
	a := &Array[int]{}
	for i := range 3*chunkSize + 5 {
		a.Append(i)
	}
	a.Resize(chunkSize + 1)
	kept := a.At(chunkSize)
	a.Shrink()
	if len(a.chunks) != 2 || a.Len() != chunkSize+1 || a.At(chunkSize) != kept || *kept != chunkSize {
		t.Fatalf("Shrink left %d chunks and %d items, want 2 and %d in place", len(a.chunks), a.Len(), chunkSize+1)
	}
	a.Resize(0)
	a.Shrink()
	if len(a.chunks) != 0 {
		t.Fatalf("Shrink of an empty array left %d chunks", len(a.chunks))
	}
	for i := range chunkSize + 1 {
		a.Append(i)
	}
	if a.Len() != chunkSize+1 || *a.At(chunkSize) != chunkSize {
		t.Fatal("an array grown again after Shrink lost items")
	}
}

func TestArrayAllocatesNothingOnceGrown(t *testing.T) {
	a := &Array[uint64]{}
	a.Resize(3 * chunkSize)
	got := testing.AllocsPerRun(100, func() {
		a.Resize(0)
		for i := range 3 * chunkSize {
			a.Append(uint64(i))
		}
		a.SwapRemove(0)
	})
	if got != 0 {
		t.Fatalf("Append, Resize and SwapRemove allocated %v times", got)
	}
}

func TestBitsSetUnsetNext(t *testing.T) {
	b := &Bits{}
	want := []int{0, 63, 64, 1000, 64*chunkSize + 1}
	for _, i := range want {
		b.Set(i)
	}
	b.Set(5)
	b.Unset(5)
	b.Unset(1 << 30) // beyond the set: nothing to do
	var got []int
	for i, ok := b.Next(0); ok; i, ok = b.Next(i + 1) {
		got = append(got, i)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("walk answered %v, want %v", got, want)
	}
	if b.Has(5) || !b.Has(1000) || b.Has(1<<30) {
		t.Fatal("Has disagrees with what was set")
	}
	b.Clear()
	if _, ok := b.Next(0); ok {
		t.Fatal("a cleared set still holds an index")
	}
	if allocs := testing.AllocsPerRun(100, func() { b.Set(1000); b.Clear() }); allocs != 0 {
		t.Fatalf("Set and Clear allocated %v times", allocs)
	}
}

func TestIsPointerFree(t *testing.T) {
	type plain struct {
		A [4]int32
		B struct{ C float64 }
	}
	type nested struct{ Inner [2]struct{ P *int } }
	if !IsPointerFree(reflect.TypeFor[plain]()) {
		t.Fatal("a struct of numbers and arrays is not pointer-free")
	}
	if IsPointerFree(reflect.TypeFor[nested]()) {
		t.Fatal("a pointer nested in an array of structs is pointer-free")
	}
}
