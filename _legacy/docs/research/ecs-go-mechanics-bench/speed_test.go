package bench

import (
	"iter"
	"testing"

	"github.com/dvoyni/cog/docs/research/ecs-go-mechanics-bench/store"
)

// testing.B.Loop wraps variables assigned inside the loop in runtime.KeepAlive
// (see `go doc testing.B.Loop`), which pins the accumulator to memory and hides
// the real speed difference between an inlined yield and an indirect one. These
// benchmarks use the classic b.N form so the optimizer is unconstrained; Sink
// keeps the result live.

func BenchmarkSpeedSliceLoop(b *testing.B) {
	_ = store.New(N)
	var sum float32
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := range N {
			sum += float32(j)
		}
	}
	Sink = sum
}

func BenchmarkSpeedInlined(b *testing.B) {
	s := store.New(N)
	var sum float32
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, body := range s.All() {
			sum += body.X
		}
	}
	Sink = sum
}

func BenchmarkSpeedSeqLocal(b *testing.B) {
	s := store.New(N)
	seq := s.All()
	var sum float32
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, body := range seq {
			sum += body.X
		}
	}
	Sink = sum
}

func BenchmarkSpeedNoinline(b *testing.B) {
	s := store.New(N)
	var sum float32
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, body := range s.AllFat() {
			sum += body.X
		}
	}
	Sink = sum
}

func BenchmarkSpeedViaHandle(b *testing.B) {
	s := store.New(N)
	r, _ := newCell(s.All())
	var sum float32
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, body := range r.Get() {
			sum += body.X
		}
	}
	Sink = sum
}

func BenchmarkSpeedViaStoreHandle(b *testing.B) {
	s := store.New(N)
	r, _ := newCell(s)
	var sum float32
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, body := range r.Get().All() {
			sum += body.X
		}
	}
	Sink = sum
}

// Separate the two costs of the boxed shape: allocation vs. the indirect yield.
// Here the seq is opaque (a parameter) but the per-range allocation is paid once
// outside the timed loop is impossible — so instead compare an opaque seq whose
// loop body captures nothing, isolating the call overhead.
//
//go:noinline
func drainOpaque(seq iter.Seq2[store.Entity, store.Body]) {
	for _, b := range seq {
		Sink += b.X
	}
}

func BenchmarkSpeedOpaqueNoCapture(b *testing.B) {
	s := store.New(N)
	seq := s.All()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		drainOpaque(seq)
	}
}

// ---- parallel, classic form ----

func BenchmarkSpeedInlinedPar(b *testing.B) {
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

func BenchmarkSpeedViaHandlePar(b *testing.B) {
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

func BenchmarkSpeedViaHandleParBallast(b *testing.B) {
	allocBallast(1024)
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

func BenchmarkSpeedInlinedParBallast(b *testing.B) {
	allocBallast(1024)
	defer func() { ballast = nil }()
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
