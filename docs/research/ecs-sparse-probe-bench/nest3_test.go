package sbench

import "testing"

// Nest_InnerOnly showed that calling All() 1024 times in an ordinary loop is
// free — it matches the flat cost. So the penalty in Nest_Nested comes from
// the inner loop sitting inside the *outer* yield closure. This separates the
// two candidate causes: range-over-func inside range-over-func, against any
// loop at all inside a yield closure.

// Outer All(), inner plain slice loop.
func BenchmarkNest_OuterSeqInnerSlice(b *testing.B) {
	outer := buildQuery(outerN, outerN)
	ib, ic := newB[Body](innerN), newB[Collider](innerN)
	for _, e := range makeIDs(innerN, innerN, false) {
		ib.add(e, Body{X: 1})
		ic.add(e, Collider{R: 3})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var s float64
		for _, o := range outer.All() {
			for _, ie := range ib.owners {
				icol, ok := ic.get(ie)
				if !ok {
					continue
				}
				s += o.Body.X + icol.R
			}
		}
		sink = s
	}
}

// Outer plain slice loop, inner All().
func BenchmarkNest_OuterSliceInnerSeq(b *testing.B) {
	inner := buildQuery(innerN, innerN)
	ob, oc := newB[Body](outerN), newB[Collider](outerN)
	for _, e := range makeIDs(outerN, outerN, false) {
		ob.add(e, Body{X: 1})
		oc.add(e, Collider{R: 3})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var s float64
		for oi, oe := range ob.owners {
			if _, ok := oc.get(oe); !ok {
				continue
			}
			for _, i := range inner.All() {
				s += ob.dense[oi].X + i.Collider.R
			}
		}
		sink = s
	}
}
