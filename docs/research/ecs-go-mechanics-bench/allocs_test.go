package bench

import (
	"iter"
	"runtime"
	"testing"

	"github.com/dvoyni/cog/docs/research/ecs-go-mechanics-bench/store"
)

// measure reports exact allocation count and bytes for one call of f, using
// runtime.MemStats deltas over many runs. testing.AllocsPerRun gives the count;
// MemStats gives the bytes.
func measure(runs int, f func()) (allocs float64, bytes float64) {
	allocs = testing.AllocsPerRun(runs, f)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for range runs {
		f()
	}
	runtime.ReadMemStats(&after)
	return allocs, float64(after.TotalAlloc-before.TotalAlloc) / float64(runs)
}

func TestExactAllocations(t *testing.T) {
	s := store.New(N)
	rSeq, _ := newCell(s.All())
	rStore, _ := newCell(s)
	var boxed any = s.All()
	seqVar := s.All()

	cases := []struct {
		name string
		f    func()
	}{
		{"slice loop", func() {
			var sum float32
			for i := range s.Len() {
				sum += float32(i)
			}
			Sink = sum
		}},
		{"range s.All() (inlinable)", func() {
			var sum float32
			for _, b := range s.All() {
				sum += b.X
			}
			Sink = sum
		}},
		{"range s.AllFat() (//go:noinline)", func() {
			var sum float32
			for _, b := range s.AllFat() {
				sum += b.X
			}
			Sink = sum
		}},
		{"range seqVar (func value in a local)", func() {
			var sum float32
			for _, b := range seqVar {
				sum += b.X
			}
			Sink = sum
		}},
		{"range boxed.(iter.Seq2) (bare any)", func() {
			var sum float32
			seq := boxed.(iter.Seq2[Entity, Body])
			for _, b := range seq {
				sum += b.X
			}
			Sink = sum
		}},
		{"range Read[iter.Seq2].Get()", func() {
			var sum float32
			for _, b := range rSeq.Get() {
				sum += b.X
			}
			Sink = sum
		}},
		{"range Read[*Store].Get().All()", func() {
			var sum float32
			for _, b := range rStore.Get().All() {
				sum += b.X
			}
			Sink = sum
		}},
		{"range Read[iter.Seq2].Get() with break", func() {
			var sum float32
			for id, b := range rSeq.Get() {
				sum += b.X
				if id == 10 {
					break
				}
			}
			Sink = sum
		}},
		{"nested range s.All() x2", func() {
			var sum float32
			for _, a := range s.All() {
				for _, c := range s.All() {
					sum += a.X * c.Y
				}
			}
			Sink = sum
		}},
		{"nested range Read[iter.Seq2].Get() x2", func() {
			var sum float32
			for _, a := range rSeq.Get() {
				for _, c := range rSeq.Get() {
					sum += a.X * c.Y
				}
			}
			Sink = sum
		}},
		{"nested: boxed outer, inlinable inner", func() {
			var sum float32
			for _, a := range rSeq.Get() {
				for _, c := range s.All() {
					sum += a.X * c.Y
				}
			}
			Sink = sum
		}},
		// Composition probes: how much of the 2 allocs is the closure and how
		// much is the captured accumulator.
		{"boxed, body captures NOTHING (writes pkg var)", func() {
			for _, b := range rSeq.Get() {
				Sink += b.X
			}
		}},
		{"boxed, body captures 1 local", func() {
			var a float32
			for _, b := range rSeq.Get() {
				a += b.X
			}
			Sink = a
		}},
		{"boxed, body captures 3 locals", func() {
			var a, c, d float32
			for _, b := range rSeq.Get() {
				a += b.X
				c += b.Y
				d += b.VX
			}
			Sink = a + c + d
		}},
	}

	for _, c := range cases {
		allocs, bytes := measure(2000, c.f)
		t.Logf("%-42s %5.1f allocs/op  %7.1f B/op", c.name, allocs, bytes)
	}
}
