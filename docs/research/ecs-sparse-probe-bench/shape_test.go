package sbench

import (
	"math/rand"
	"testing"
)

// ---------------------------------------------------------------------------
// Paging residency: paging only saves memory if a store's owners are clustered
// in index space. Generation-based recycling scatters them, so this measures
// what a page-on-demand index actually costs in both regimes.
// ---------------------------------------------------------------------------

func TestPagingResidency(t *testing.T) {
	const space = 1 << 20 // a million-slot index space
	rng := rand.New(rand.NewSource(11))

	for _, owners := range []int{3, 100, 10000} {
		clustered := newD[Body](space)
		for i := range owners {
			clustered.add(mkEntity(uint32(i), 1), Body{})
		}
		scattered := newD[Body](space)
		for _, p := range rng.Perm(space)[:owners] {
			scattered.add(mkEntity(uint32(p), 1), Body{})
		}
		flatBytes := space * 4
		t.Logf("owners=%-6d clustered pages=%-5d (%d KB)  scattered pages=%-5d (%d KB)  flat=%d KB",
			owners,
			clustered.pagesResident(), clustered.pagesResident()*pageSize*4/1024,
			scattered.pagesResident(), scattered.pagesResident()*pageSize*4/1024,
			flatBytes/1024)
	}
}

// ---------------------------------------------------------------------------
// The known bad case cog#239 requires the spec to state: two large, mostly
// disjoint stores. The driver yields everything it has and discards almost all
// of it. Then the remedy: a Tag that narrows the driver to the intersection.
// ---------------------------------------------------------------------------

const (
	bigN    = 5000
	overlap = 100
	bigSpc  = 10000
)

func setupBad() (*storeC[Body], *storeC[Collider], *storeC[struct{}]) {
	bs := newC[Body](bigSpc)
	cs := newC[Collider](bigSpc)
	tag := newC[struct{}](bigSpc)
	for i := range bigN {
		bs.add(mkEntity(uint32(i), 1), Body{X: 1})
	}
	for i := range bigN {
		cs.add(mkEntity(uint32(bigN-overlap+i), 1), Collider{R: 3})
	}
	for i := range overlap {
		tag.add(mkEntity(uint32(bigN-overlap+i), 1), struct{}{})
	}
	return bs, cs, tag
}

func BenchmarkBad_DriveLarge(b *testing.B) {
	bs, cs, _ := setupBad()
	b.ResetTimer()
	for range b.N {
		var s float64
		for i, e := range bs.owners {
			col, ok := cs.get(e)
			if !ok {
				continue
			}
			s += bs.dense[i].X + col.R
		}
		sink = s
	}
}

func BenchmarkBad_DriveTag(b *testing.B) {
	bs, cs, tag := setupBad()
	b.ResetTimer()
	for range b.N {
		var s float64
		for _, e := range tag.owners {
			body, ok := bs.get(e)
			if !ok {
				continue
			}
			col, ok := cs.get(e)
			if !ok {
				continue
			}
			s += body.X + col.R
		}
		sink = s
	}
}

// ---------------------------------------------------------------------------
// Driver selection: comparing N store lengths, every time the query runs.
// The question is whether this needs caching. Caching it would be a global
// index, and any global index is a global lock.
// ---------------------------------------------------------------------------

func BenchmarkDriverPick(b *testing.B) {
	lens := make([]int, 8)
	for i := range lens {
		lens[i] = 1000 + i*13
	}
	lens[5] = 7
	b.ResetTimer()
	for range b.N {
		best, bestLen := 0, lens[0]
		for i := 1; i < len(lens); i++ {
			if lens[i] < bestLen {
				best, bestLen = i, lens[i]
			}
		}
		sink = float64(best)
	}
}

// ---------------------------------------------------------------------------
// Tags: 85 separate stores probed one at a time, against one bitfield
// component probed once and masked. Speed is only half the question; the other
// half is that a bitfield collapses 85 lock units into one.
// ---------------------------------------------------------------------------

type Flags struct{ Class, Flag, Status uint32 }

func BenchmarkTag_ThreeStores(b *testing.B) {
	bs := newC[Body](n2)
	t1, t2, t3 := newC[struct{}](n2), newC[struct{}](n2), newC[struct{}](n2)
	for _, e := range makeIDs(n2, n2, false) {
		bs.add(e, Body{X: 1})
		t1.add(e, struct{}{})
		t2.add(e, struct{}{})
		t3.add(e, struct{}{})
	}
	b.ResetTimer()
	for range b.N {
		var s float64
		for i, e := range bs.owners {
			if _, ok := t1.get(e); !ok {
				continue
			}
			if _, ok := t2.get(e); !ok {
				continue
			}
			if _, ok := t3.get(e); !ok {
				continue
			}
			s += bs.dense[i].X
		}
		sink = s
	}
}

func BenchmarkTag_OneBitfield(b *testing.B) {
	bs := newC[Body](n2)
	fs := newC[Flags](n2)
	for _, e := range makeIDs(n2, n2, false) {
		bs.add(e, Body{X: 1})
		fs.add(e, Flags{Class: 0b101, Flag: 0b11, Status: 0b1})
	}
	b.ResetTimer()
	for range b.N {
		var s float64
		for i, e := range bs.owners {
			f, ok := fs.get(e)
			if !ok {
				continue
			}
			if f.Class&0b001 == 0 || f.Flag&0b010 == 0 || f.Status&0b001 == 0 {
				continue
			}
			s += bs.dense[i].X
		}
		sink = s
	}
}

// ---------------------------------------------------------------------------
// Growth: amortised doubling still allocates on the frame it grows, which is
// where "zero allocation on the hot path" is either delivered or quietly
// broken. 30 Hz means the spike matters more than the average.
// ---------------------------------------------------------------------------

const growN = 10000

func BenchmarkGrow_FromEmpty(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		s := newC[Body](growN)
		for i := range growN {
			s.add(mkEntity(uint32(i), 1), Body{X: 1})
		}
		sink = s.dense[0].X
	}
}

func BenchmarkGrow_Preallocated(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		s := newC[Body](growN)
		s.dense = make([]Body, 0, growN)
		s.owners = make([]Entity, 0, growN)
		for i := range growN {
			s.add(mkEntity(uint32(i), 1), Body{X: 1})
		}
		sink = s.dense[0].X
	}
}

// ---------------------------------------------------------------------------
// Scale sweep: the map records that no published 1k/10k/100k iteration sweep
// exists, so the curve cog needs is measured here. One probe, flat one-load.
// ---------------------------------------------------------------------------

func benchScale(b *testing.B, n int, probe bool) {
	space := uint32(n)
	bs, cs := newC[Body](space), newC[Collider](space)
	for _, e := range makeIDs(n, space, false) {
		bs.add(e, Body{X: 1})
		cs.add(e, Collider{R: 3})
	}
	b.ResetTimer()
	for range b.N {
		var s float64
		for i, e := range bs.owners {
			if probe {
				col, ok := cs.get(e)
				if !ok {
					continue
				}
				s += bs.dense[i].X + col.R
			} else {
				s += bs.dense[i].X
			}
		}
		sink = s
	}
}

func BenchmarkScale1k_Probe(b *testing.B)   { benchScale(b, 1024, true) }
func BenchmarkScale1k_Base(b *testing.B)    { benchScale(b, 1024, false) }
func BenchmarkScale16k_Probe(b *testing.B)  { benchScale(b, 16384, true) }
func BenchmarkScale16k_Base(b *testing.B)   { benchScale(b, 16384, false) }
func BenchmarkScale131k_Probe(b *testing.B) { benchScale(b, 131072, true) }
func BenchmarkScale131k_Base(b *testing.B)  { benchScale(b, 131072, false) }
