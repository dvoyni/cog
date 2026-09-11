package sbench

import (
	"math/rand"
	"testing"
)

type Health struct{ HP, Max float64 }

type Sprite struct{ ID, Layer uint32 }

var sink float64

// makeIDs builds n entity ids drawn from an index space of idSpace slots.
// scatter models generation-based index recycling, which leaves a live
// entity's index anywhere in the space rather than in a contiguous prefix.
func makeIDs(n int, idSpace uint32, scatter bool) []Entity {
	ids := make([]Entity, 0, n)
	if !scatter {
		for i := range n {
			ids = append(ids, mkEntity(uint32(i), 1))
		}
		return ids
	}
	rng := rand.New(rand.NewSource(7))
	perm := rng.Perm(int(idSpace))[:n]
	for _, p := range perm {
		ids = append(ids, mkEntity(uint32(p), 1))
	}
	return ids
}

const n2 = 1024

// --- baseline: the driver's dense array, no probe at all -------------------

func BenchmarkP0_Baseline(b *testing.B) {
	bs := newA[Body](n2)
	for _, e := range makeIDs(n2, n2, false) {
		bs.add(e, Body{X: 1, VX: 2})
	}
	b.ResetTimer()
	for range b.N {
		var s float64
		for i := range bs.dense {
			s += bs.dense[i].X
		}
		sink = s
	}
}

// --- arity 2: driver + one probe -------------------------------------------

func setupA2(ids []Entity, space uint32) (*storeA[Body], *storeA[Collider]) {
	bs, cs := newA[Body](space), newA[Collider](space)
	for _, e := range ids {
		bs.add(e, Body{X: 1, VX: 2})
		cs.add(e, Collider{R: 3})
	}
	return bs, cs
}

func benchA2(b *testing.B, scatter bool, space uint32) {
	bs, cs := setupA2(makeIDs(n2, space, scatter), space)
	b.ResetTimer()
	for range b.N {
		var s float64
		for i, e := range bs.owners {
			body := &bs.dense[i]
			col, ok := cs.get(e)
			if !ok {
				continue
			}
			s += body.X + col.R
		}
		sink = s
	}
}

func BenchmarkP2_A_Flat2Load(b *testing.B)        { benchA2(b, false, n2) }
func BenchmarkP2_A_Flat2LoadScatter(b *testing.B) { benchA2(b, true, 1<<16) }

func benchB2(b *testing.B, scatter bool, space uint32) {
	bs, cs := newB[Body](space), newB[Collider](space)
	for _, e := range makeIDs(n2, space, scatter) {
		bs.add(e, Body{X: 1, VX: 2})
		cs.add(e, Collider{R: 3})
	}
	b.ResetTimer()
	for range b.N {
		var s float64
		for i, e := range bs.owners {
			body := &bs.dense[i]
			col, ok := cs.get(e)
			if !ok {
				continue
			}
			s += body.X + col.R
		}
		sink = s
	}
}

func BenchmarkP2_B_Flat1Load64(b *testing.B)        { benchB2(b, false, n2) }
func BenchmarkP2_B_Flat1Load64Scatter(b *testing.B) { benchB2(b, true, 1<<16) }

func benchC2(b *testing.B, scatter bool, space uint32) {
	bs, cs := newC[Body](space), newC[Collider](space)
	for _, e := range makeIDs(n2, space, scatter) {
		bs.add(e, Body{X: 1, VX: 2})
		cs.add(e, Collider{R: 3})
	}
	b.ResetTimer()
	for range b.N {
		var s float64
		for i, e := range bs.owners {
			body := &bs.dense[i]
			col, ok := cs.get(e)
			if !ok {
				continue
			}
			s += body.X + col.R
		}
		sink = s
	}
}

func BenchmarkP2_C_Flat1Load32(b *testing.B)        { benchC2(b, false, n2) }
func BenchmarkP2_C_Flat1Load32Scatter(b *testing.B) { benchC2(b, true, 1<<16) }

func benchD2(b *testing.B, scatter bool, space uint32) {
	bs, cs := newD[Body](space), newD[Collider](space)
	for _, e := range makeIDs(n2, space, scatter) {
		bs.add(e, Body{X: 1, VX: 2})
		cs.add(e, Collider{R: 3})
	}
	b.ResetTimer()
	for range b.N {
		var s float64
		for i, e := range bs.owners {
			body := &bs.dense[i]
			col, ok := cs.get(e)
			if !ok {
				continue
			}
			s += body.X + col.R
		}
		sink = s
	}
}

func BenchmarkP2_D_Paged1Load32(b *testing.B)        { benchD2(b, false, n2) }
func BenchmarkP2_D_Paged1Load32Scatter(b *testing.B) { benchD2(b, true, 1<<16) }

// --- arity 4: driver + three probes ----------------------------------------

func benchA4(b *testing.B) {
	space := uint32(n2)
	bs, cs := newA[Body](space), newA[Collider](space)
	hs, ss := newA[Health](space), newA[Sprite](space)
	for _, e := range makeIDs(n2, space, false) {
		bs.add(e, Body{X: 1})
		cs.add(e, Collider{R: 3})
		hs.add(e, Health{HP: 5})
		ss.add(e, Sprite{ID: 7})
	}
	b.ResetTimer()
	for range b.N {
		var s float64
		for i, e := range bs.owners {
			body := &bs.dense[i]
			col, ok := cs.get(e)
			if !ok {
				continue
			}
			h, ok := hs.get(e)
			if !ok {
				continue
			}
			sp, ok := ss.get(e)
			if !ok {
				continue
			}
			s += body.X + col.R + h.HP + float64(sp.ID)
		}
		sink = s
	}
}

func BenchmarkP4_A_Flat2Load(b *testing.B) { benchA4(b) }

func benchC4(b *testing.B) {
	space := uint32(n2)
	bs, cs := newC[Body](space), newC[Collider](space)
	hs, ss := newC[Health](space), newC[Sprite](space)
	for _, e := range makeIDs(n2, space, false) {
		bs.add(e, Body{X: 1})
		cs.add(e, Collider{R: 3})
		hs.add(e, Health{HP: 5})
		ss.add(e, Sprite{ID: 7})
	}
	b.ResetTimer()
	for range b.N {
		var s float64
		for i, e := range bs.owners {
			body := &bs.dense[i]
			col, ok := cs.get(e)
			if !ok {
				continue
			}
			h, ok := hs.get(e)
			if !ok {
				continue
			}
			sp, ok := ss.get(e)
			if !ok {
				continue
			}
			s += body.X + col.R + h.HP + float64(sp.ID)
		}
		sink = s
	}
}

func BenchmarkP4_C_Flat1Load32(b *testing.B) { benchC4(b) }

func benchD4(b *testing.B) {
	space := uint32(n2)
	bs, cs := newD[Body](space), newD[Collider](space)
	hs, ss := newD[Health](space), newD[Sprite](space)
	for _, e := range makeIDs(n2, space, false) {
		bs.add(e, Body{X: 1})
		cs.add(e, Collider{R: 3})
		hs.add(e, Health{HP: 5})
		ss.add(e, Sprite{ID: 7})
	}
	b.ResetTimer()
	for range b.N {
		var s float64
		for i, e := range bs.owners {
			body := &bs.dense[i]
			col, ok := cs.get(e)
			if !ok {
				continue
			}
			h, ok := hs.get(e)
			if !ok {
				continue
			}
			sp, ok := ss.get(e)
			if !ok {
				continue
			}
			s += body.X + col.R + h.HP + float64(sp.ID)
		}
		sink = s
	}
}

func BenchmarkP4_D_Paged1Load32(b *testing.B) { benchD4(b) }
