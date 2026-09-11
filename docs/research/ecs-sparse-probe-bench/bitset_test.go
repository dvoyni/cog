package sbench

import (
	"math/bits"
	"testing"
)

// A per-query entity index would iterate only matching entities, at the cost
// of maintaining that index on every structural change. This measures the
// cheaper relative that needs no maintenance and no per-query state beyond a
// reusable scratch buffer: give each Store a presence bitset, one bit per
// entity index, and intersect the bitsets at query time.
//
// Structural change touches one bit. Query time is peak/64 words of AND, then
// the matching entities only. specs does this with hibitset; nobody surveyed
// does it inside a sparse-set layout.

type bitset []uint64

func newBits(space uint32) bitset { return make(bitset, (space+63)/64) }

func (b bitset) set(i uint32)   { b[i>>6] |= 1 << (i & 63) }
func (b bitset) clear(i uint32) { b[i>>6] &^= 1 << (i & 63) }

// and2 writes b0 & b1 into dst.
func and2(dst, b0, b1 bitset) {
	for i := range dst {
		dst[i] = b0[i] & b1[i]
	}
}

func and4(dst, b0, b1, b2, b3 bitset) {
	for i := range dst {
		dst[i] = b0[i] & b1[i] & b2[i] & b3[i]
	}
}

// storeBits is storeB plus a presence bitset.
type storeBits[T any] struct {
	*storeB[T]
	bits bitset
}

func newBits2[T any](space uint32) *storeBits[T] {
	return &storeBits[T]{storeB: newB[T](space), bits: newBits(space)}
}

func (s *storeBits[T]) add(e Entity, v T) {
	s.storeB.add(e, v)
	s.bits.set(e.idx())
}

// --- arity 4, everything matches -------------------------------------------

func setupBits4(n int, space uint32) (*storeBits[Body], *storeBits[Collider], *storeBits[Health], *storeBits[Sprite], bitset) {
	bs, cs := newBits2[Body](space), newBits2[Collider](space)
	hs, ss := newBits2[Health](space), newBits2[Sprite](space)
	for _, e := range makeIDs(n, space, false) {
		bs.add(e, Body{X: 1})
		cs.add(e, Collider{R: 3})
		hs.add(e, Health{HP: 5})
		ss.add(e, Sprite{ID: 7})
	}
	return bs, cs, hs, ss, newBits(space)
}

func BenchmarkBits_Arity4_AllMatch(b *testing.B) {
	bs, cs, hs, ss, scratch := setupBits4(n2, n2)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var s float64
		and4(scratch, bs.bits, cs.bits, hs.bits, ss.bits)
		for w, word := range scratch {
			for word != 0 {
				bit := word & (-word)
				idx := uint32(w)<<6 | uint32(bits.TrailingZeros64(word))
				word ^= bit
				v0 := bs.sparse[idx]
				gen := uint32(v0 >> 32)
				e := mkEntity(idx, gen)
				s += bs.dense[uint32(v0)].X
				c, _ := cs.get(e)
				h, _ := hs.get(e)
				sp, _ := ss.get(e)
				s += c.R + h.HP + float64(sp.ID)
			}
		}
		sink = s
	}
}

// --- the bad case: 5000 / 5000 / 100 overlap -------------------------------

func setupBitsBad() (*storeBits[Body], *storeBits[Collider], bitset) {
	bs, cs := newBits2[Body](bigSpc), newBits2[Collider](bigSpc)
	for i := range bigN {
		bs.add(mkEntity(uint32(i), 1), Body{X: 1})
	}
	for i := range bigN {
		cs.add(mkEntity(uint32(bigN-overlap+i), 1), Collider{R: 3})
	}
	return bs, cs, newBits(bigSpc)
}

func BenchmarkBits_BadCase(b *testing.B) {
	bs, cs, scratch := setupBitsBad()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var s float64
		and2(scratch, bs.bits, cs.bits)
		for w, word := range scratch {
			for word != 0 {
				bit := word & (-word)
				idx := uint32(w)<<6 | uint32(bits.TrailingZeros64(word))
				word ^= bit
				v0 := bs.sparse[idx]
				e := mkEntity(idx, uint32(v0>>32))
				c, _ := cs.get(e)
				s += bs.dense[uint32(v0)].X + c.R
			}
		}
		sink = s
	}
}

// Same bad case, probing — the figure from Bad_DriveLarge, repeated here on
// the storeBits type so the comparison is like for like.
func BenchmarkBits_BadCaseProbe(b *testing.B) {
	bs, cs, _ := setupBitsBad()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var s float64
		for i, e := range bs.owners {
			c, ok := cs.get(e)
			if !ok {
				continue
			}
			s += bs.dense[i].X + c.R
		}
		sink = s
	}
}
