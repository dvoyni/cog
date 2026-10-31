package bench

import (
	"iter"
	"testing"

	"github.com/dvoyni/cog/docs/research/ecs-go-mechanics-bench/store"
)

const N = 1024

// Sink defeats dead-code elimination without allocating.
var Sink float32

// ---- local (same-package) store, so the inliner sees everything ----

type Entity = store.Entity
type Body = store.Body

type local struct {
	ids  []Entity
	data []Body
}

func newLocal(n int) *local {
	s := store.New(n)
	l := &local{ids: make([]Entity, n), data: make([]Body, n)}
	for id, b := range s.All() {
		l.ids[int(id)] = id
		l.data[int(id)] = b
	}
	return l
}

func (l *local) All() iter.Seq2[Entity, Body] {
	return func(yield func(Entity, Body) bool) {
		for i, id := range l.ids {
			if !yield(id, l.data[i]) {
				return
			}
		}
	}
}

// ---- baselines ----

func BenchmarkSliceLoop(b *testing.B) {
	l := newLocal(N)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var sum float32
		for i := range l.data {
			sum += l.data[i].X
		}
		Sink = sum
	}
}

// ---- 1. iteration shapes ----

func BenchmarkIterSamePackage(b *testing.B) {
	l := newLocal(N)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var sum float32
		for _, body := range l.All() {
			sum += body.X
		}
		Sink = sum
	}
}

func BenchmarkIterCrossPackage(b *testing.B) {
	s := store.New(N)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var sum float32
		for _, body := range s.All() {
			sum += body.X
		}
		Sink = sum
	}
}

// AllFat is over the inline budget, so the range statement cannot see the func
// literal and must call an opaque func value.
func BenchmarkIterCrossPackageNotInlinable(b *testing.B) {
	s := store.New(N)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var sum float32
		for _, body := range s.AllFat() {
			sum += body.X
		}
		Sink = sum
	}
}

// Method on a stored value (non-pointer receiver).
func BenchmarkIterValueReceiver(b *testing.B) {
	s := store.NewVal(N)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var sum float32
		for _, body := range s.All() {
			sum += body.X
		}
		Sink = sum
	}
}

// The seq handed across as a plain (non-boxed) func value in a variable.
func BenchmarkIterSeqInVariable(b *testing.B) {
	s := store.New(N)
	seq := s.All()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var sum float32
		for _, body := range seq {
			sum += body.X
		}
		Sink = sum
	}
}

// THE REAL SHAPE: the seq lives inside an `any` and comes back out through a
// type assertion, exactly as kernel.Read[iter.Seq2[...]] would deliver it.
func BenchmarkIterViaKernelReadHandle(b *testing.B) {
	s := store.New(N)
	r, _ := newCell(s.All())
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var sum float32
		for _, body := range r.Get() {
			sum += body.X
		}
		Sink = sum
	}
}

// The alternative shape: the HANDLE holds the store pointer, and the seq is
// produced inside the loop's own compilation scope.
func BenchmarkIterViaKernelReadStore(b *testing.B) {
	s := store.New(N)
	r, _ := newCell(s)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var sum float32
		for _, body := range r.Get().All() {
			sum += body.X
		}
		Sink = sum
	}
}

// Bare `any` round trip, no generics, to separate the generic method from the
// type assertion.
func BenchmarkIterViaBareAny(b *testing.B) {
	s := store.New(N)
	var boxed any = s.All()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var sum float32
		seq := boxed.(iter.Seq2[Entity, Body])
		for _, body := range seq {
			sum += body.X
		}
		Sink = sum
	}
}

// ---- 2. break and capture ----

func BenchmarkIterBreak(b *testing.B) {
	s := store.New(N)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var sum float32
		for id, body := range s.All() {
			sum += body.X
			if id == N/2 {
				break
			}
		}
		Sink = sum
	}
}

func BenchmarkIterBreakViaHandle(b *testing.B) {
	s := store.New(N)
	r, _ := newCell(s.All())
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var sum float32
		for id, body := range r.Get() {
			sum += body.X
			if id == N/2 {
				break
			}
		}
		Sink = sum
	}
}

// Body captures a variable declared OUTSIDE the benchmark loop iteration.
func BenchmarkIterCaptureOuter(b *testing.B) {
	s := store.New(N)
	var outer float32
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, body := range s.All() {
			outer += body.X
		}
	}
	Sink = outer
}

// Body captures a heap-resident accumulator (a slice it appends to).
func BenchmarkIterCaptureSlice(b *testing.B) {
	s := store.New(N)
	acc := make([]float32, 0, N)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		acc = acc[:0]
		for _, body := range s.All() {
			acc = append(acc, body.X)
		}
		Sink = acc[0]
	}
}

// A nested range-over-func: inner loop inside an outer one, which is what a
// query over two stores looks like.
func BenchmarkIterNested(b *testing.B) {
	s := store.New(32)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var sum float32
		for _, a := range s.All() {
			for _, c := range s.All() {
				sum += a.X * c.Y
			}
		}
		Sink = sum
	}
}

// ---- 3. escaping variant under a non-trivial live heap ----

var ballast [][]byte

func allocBallast(mb int) {
	ballast = make([][]byte, mb)
	for i := range ballast {
		ballast[i] = make([]byte, 1<<20)
		ballast[i][0] = byte(i)
	}
}

func BenchmarkIterViaHandleWithHeap(b *testing.B) {
	allocBallast(512)
	defer func() { ballast = nil }()
	s := store.New(N)
	r, _ := newCell(s.All())
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var sum float32
		for _, body := range r.Get() {
			sum += body.X
		}
		Sink = sum
	}
}

func BenchmarkIterInlinedWithHeap(b *testing.B) {
	allocBallast(512)
	defer func() { ballast = nil }()
	s := store.New(N)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var sum float32
		for _, body := range s.All() {
			sum += body.X
		}
		Sink = sum
	}
}

// ---- 4. parallel: does the escaping shape cost more at GOMAXPROCS>1? ----

func BenchmarkIterViaHandleParallel(b *testing.B) {
	s := store.New(N)
	r, _ := newCell(s.All())
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var sum float32
		for pb.Next() {
			for _, body := range r.Get() {
				sum += body.X
			}
		}
		Sink = sum
	})
}

func BenchmarkIterInlinedParallel(b *testing.B) {
	s := store.New(N)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var sum float32
		for pb.Next() {
			for _, body := range s.All() {
				sum += body.X
			}
		}
		Sink = sum
	})
}

func BenchmarkIterViaHandleParallelHeap(b *testing.B) {
	allocBallast(512)
	defer func() { ballast = nil }()
	s := store.New(N)
	r, _ := newCell(s.All())
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var sum float32
		for pb.Next() {
			for _, body := range r.Get() {
				sum += body.X
			}
		}
		Sink = sum
	})
}
