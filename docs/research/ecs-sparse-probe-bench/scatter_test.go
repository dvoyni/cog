package sbench

import (
	"math/rand"
	"testing"
)

// The sweep in shape_test.go is a best case: both stores were filled in the
// same order, so the probed store's dense index equals the driver's and every
// probe lands sequentially. Real stores are filled at different times. Here
// the probed store is filled in a shuffled order, so sparse[] is still walked
// in order but dense[] is hit at random — the access pattern cart calls "not
// cache friendly".

func benchScaleShuffled(b *testing.B, n int) {
	space := uint32(n)
	bs, cs := newC[Body](space), newC[Collider](space)
	ids := makeIDs(n, space, false)
	for _, e := range ids {
		bs.add(e, Body{X: 1})
	}
	rng := rand.New(rand.NewSource(23))
	for _, k := range rng.Perm(n) {
		cs.add(ids[k], Collider{R: 3})
	}
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

func BenchmarkShuf1k_Probe(b *testing.B)   { benchScaleShuffled(b, 1024) }
func BenchmarkShuf16k_Probe(b *testing.B)  { benchScaleShuffled(b, 16384) }
func BenchmarkShuf131k_Probe(b *testing.B) { benchScaleShuffled(b, 131072) }
