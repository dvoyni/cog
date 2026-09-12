package proto

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"protoecs/ecs"
)

// cog#237 forbade pointers in a Component and gave two reasons, neither of them
// allocation: "A Component array with no pointers is never scanned by the GC,
// which is real at 100k entities on a design whose first requirement is not
// feeding the GC; and it is trivially serialisable for the replication
// constraint."
//
// The first half was asserted, never measured, and it is the expensive half --
// it is what forces an asset name out of a Component and into an interned
// handle. This file measures it: what a Store actually costs the collector,
// with and without a pointer in the Component, and whether a pointer-free Store
// costs anything at all.
//
// The Stores here are built with NewStore rather than RegisterComponent,
// because RegisterComponent now rejects a Component carrying a string and the
// measurement needs one.

// PodBody is a legal Component: six floats, no pointer, 48 bytes.
type PodBody struct{ Px, Py, Pz, Vx, Vy, Vz float64 }

// RefBody is the same thing with a name on it -- the Component an author would
// write if strings were allowed. 64 bytes, one pointer.
type RefBody struct {
	Px, Py, Pz, Vx, Vy, Vz float64
	Name                   string
}

// NamedBody keeps the text itself, in a fixed-size array. It is pointer-free,
// so it is a legal Component -- the option a hash cannot offer, because a hash
// cannot be shown to a player. 80 bytes.
type NamedBody struct {
	Px, Py, Pz, Vx, Vy, Vz float64
	Name                   [32]byte
}

// gcFloor is the per-collection cost of a forced full GC with live held live.
//
// The collections are timed in batches rather than one at a time: the wall
// clock on Windows quantises to about half a millisecond, which is larger than
// a whole collection of a small heap, so timing one of them reports either 0 or
// the granularity and nothing in between. A batch of batchSize amortises that
// away. The minimum across rounds rather than the mean, because a forced GC
// competes with whatever else the machine is doing, so the floor is the signal.
func gcFloor(live any, rounds int) time.Duration {
	const batchSize = 50
	runtime.GC()
	runtime.GC()
	var best time.Duration
	for i := range rounds {
		start := time.Now()
		for range batchSize {
			runtime.GC()
		}
		d := time.Since(start) / batchSize
		if i == 0 || d < best {
			best = d
		}
	}
	runtime.KeepAlive(live)
	return best
}

func fillPod(n int) *ecs.Store[PodBody] {
	en := ecs.NewEntities(uint32(n))
	s := ecs.NewStore[PodBody](uint32(n))
	for range n {
		s.Add(en.Alloc(), PodBody{Px: 1, Vy: 2})
	}
	return s
}

// fillRef builds the pointer-bearing Store. shared decides whether every row
// names the same string or its own: the distinction matters because scanning is
// proportional to pointer *slots*, not to distinct objects, so interning the
// strings should not help the mark phase at all.
func fillNamed(n int) *ecs.Store[NamedBody] {
	en := ecs.NewEntities(uint32(n))
	s := ecs.NewStore[NamedBody](uint32(n))
	var name [32]byte
	copy(name[:], "models/props/crate.glb")
	for range n {
		s.Add(en.Alloc(), NamedBody{Px: 1, Vy: 2, Name: name})
	}
	return s
}

func fillRef(n int, shared bool) *ecs.Store[RefBody] {
	en := ecs.NewEntities(uint32(n))
	s := ecs.NewStore[RefBody](uint32(n))
	const one = "models/props/crate.glb"
	for i := range n {
		name := one
		if !shared {
			name = fmt.Sprintf("models/props/crate_%07d.glb", i)
		}
		s.Add(en.Alloc(), RefBody{Px: 1, Vy: 2, Name: name})
	}
	return s
}

// What the collector pays for a Store, by Component shape. Everything here is
// one forced full collection with exactly one Store live.
func TestWhatTheGCActuallyScans(t *testing.T) {
	const rounds = 5
	for _, n := range []int{100_000, 1_000_000} {
		t.Run(fmt.Sprintf("entities=%d", n), func(t *testing.T) {
			empty := gcFloor(nil, rounds)
			pod := gcFloor(fillPod(n), rounds)
			shared := gcFloor(fillRef(n, true), rounds)
			distinct := gcFloor(fillRef(n, false), rounds)
			named := gcFloor(fillNamed(n), rounds)

			t.Logf("no store                     %9.4f ms", empty.Seconds()*1e3)
			t.Logf("pointer-free Component       %9.4f ms  (%+.3f ms)",
				pod.Seconds()*1e3, (pod-empty).Seconds()*1e3)
			t.Logf("string Component, interned   %9.4f ms  (%+.3f ms)",
				shared.Seconds()*1e3, (shared-empty).Seconds()*1e3)
			t.Logf("string Component, distinct   %9.4f ms  (%+.3f ms)",
				distinct.Seconds()*1e3, (distinct-empty).Seconds()*1e3)
			t.Logf("[32]byte name Component      %9.4f ms  (%+.3f ms)",
				named.Seconds()*1e3, (named-empty).Seconds()*1e3)

			// The claim under test: a pointer-free Store is not scanned, so it
			// costs the collector nothing however large it is. Allow generous
			// slack -- this is a wall-clock floor on a busy machine, and the
			// effect being checked is order-of-magnitude, not marginal.
			if pod > empty+3*time.Millisecond {
				t.Errorf("a pointer-free Store cost the collector %v over an empty heap", pod-empty)
			}
			// The text kept inline is still pointer-free, so it must cost the
			// collector nothing however wide it is.
			if named > empty+3*time.Millisecond {
				t.Errorf("a [32]byte name cost the collector %v over an empty heap", named-empty)
			}
			if shared <= pod {
				t.Errorf("a string Component cost no more than a pointer-free one (%v against %v)", shared, pod)
			}
		})
	}
}

// Whether a pointer-free Store stays free when something else on the heap is
// scanned. It should: the span is noscan either way, so the collector never
// reaches it.
func TestAPointerFreeStoreStaysFreeBesideAScannedOne(t *testing.T) {
	const n, rounds = 1_000_000, 5
	refOnly := gcFloor(fillRef(n, true), rounds)
	both := gcFloor([]any{fillRef(n, true), fillPod(n)}, rounds)
	t.Logf("string Store alone           %9.4f ms", refOnly.Seconds()*1e3)
	t.Logf("string Store + POD Store     %9.4f ms  (%+.3f ms)",
		both.Seconds()*1e3, (both-refOnly).Seconds()*1e3)
	if both > refOnly+5*time.Millisecond {
		t.Errorf("adding a pointer-free Store cost %v of mark time", both-refOnly)
	}
}

// The other half of the price, which is not the collector's: a pointer widens
// the Component, so fewer rows fit a cache line and the iteration itself slows
// down. This walks the dense array directly, so it measures the Component
// shape and nothing else.
func BenchmarkIterateByComponentShape(b *testing.B) {
	const n = 100_000
	b.Run("pointer-free/48B", func(b *testing.B) {
		s := fillPod(n)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			for j := range n {
				r := s.RowAt(j)
				r.Px += r.Vx
			}
		}
	})
	b.Run("string/64B", func(b *testing.B) {
		s := fillRef(n, true)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			for j := range n {
				r := s.RowAt(j)
				r.Px += r.Vx
			}
		}
	})
}
