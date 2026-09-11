package bench

import (
	"iter"
	"testing"

	"github.com/dvoyni/cog/docs/research/ecs-go-mechanics-bench/store"
)

type Huge struct{ F [1024]float32 } // 4 KiB

var (
	BodySink  store.Body
	BigSink   Big
	HugeSink  Huge
	StoreSink *store.Store
	SeqSink   iter.Seq2[Entity, Body]
)

// ---- Read[T].Get(): the type assertion cost by T's shape ----

func BenchmarkGetPointerStore(b *testing.B) {
	r, _ := newCell(store.New(N))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		StoreSink = r.Get()
	}
}

func BenchmarkGetSeq(b *testing.B) {
	s := store.New(N)
	r, _ := newCell(s.All())
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		SeqSink = r.Get()
	}
}

func BenchmarkGetSmallStruct16B(b *testing.B) {
	r, _ := newCell(store.Body{X: 1})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		BodySink = r.Get()
	}
}

func BenchmarkGetStruct512B(b *testing.B) {
	r, _ := newCell(Big{})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		BigSink = r.Get()
	}
}

func BenchmarkGetStruct4KiB(b *testing.B) {
	r, _ := newCell(Huge{})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		HugeSink = r.Get()
	}
}

// Reading a single field out of a boxed large struct still copies the whole
// struct first, because the assertion materialises a T.
func BenchmarkGetStruct4KiBOneField(b *testing.B) {
	r, _ := newCell(Huge{})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		Sink = r.Get().F[0]
	}
}

// The same 4 KiB payload behind a pointer.
func BenchmarkGetPointerTo4KiB(b *testing.B) {
	r, _ := newCell(&Huge{})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		Sink = r.Get().F[0]
	}
}

// ---- Write[T].Set(v): the boxing cost by T's shape ----

func BenchmarkSetPointerStore(b *testing.B) {
	_, w := newCell(store.New(N))
	s := store.New(N)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		w.Set(s)
	}
}

func BenchmarkSetSmallStruct16B(b *testing.B) {
	_, w := newCell(store.Body{})
	v := store.Body{X: 3}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		w.Set(v)
	}
}

func BenchmarkSetStruct4KiB(b *testing.B) {
	_, w := newCell(Huge{})
	var v Huge
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		w.Set(v)
	}
}

// ---- correctness, not speed: a non-pointer store loses mutations ----

type counterStore struct{ n int }

func (c *counterStore) Inc() { c.n++ }

func TestNonPointerStoreLosesMutation(t *testing.T) {
	// Pointer store: mutation through the handle sticks.
	rp, _ := newCell(&counterStore{})
	rp.Get().Inc()
	rp.Get().Inc()
	if got := rp.Get().n; got != 2 {
		t.Fatalf("pointer store: want 2, got %d", got)
	}

	// Value store: Get returns a copy, so the mutation is written to a
	// temporary and discarded.
	rv, _ := newCell(counterStore{})
	c1 := rv.Get()
	c1.Inc()
	c2 := rv.Get()
	if c2.n != 0 {
		t.Fatalf("value store unexpectedly retained mutation: %d", c2.n)
	}
	t.Logf("value store discarded the mutation as expected: %d", c2.n)
}
