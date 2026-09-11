package sbench

import "testing"

// Nest_Nested costs far more per inner entity than Nest_Flat does per entity,
// at zero allocations. These isolate why: whether it is nesting as such, or
// simply that All() called in a loop stops being inlinable.

// InnerOnly: the inner query iterated 1024 times, with no outer query at all.
// If this matches Nest_Nested, nesting is irrelevant and the cost is All() in
// a loop.
func BenchmarkNest_InnerOnly(b *testing.B) {
	inner := buildQuery(innerN, innerN)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var s float64
		for range outerN {
			for _, i := range inner.All() {
				s += i.Collider.R
			}
		}
		sink = s
	}
}

// HandNested: the same nested traversal written as plain slice loops, as the
// floor any iteration contract is measured against.
func BenchmarkNest_HandNested(b *testing.B) {
	ob, oc := newB[Body](outerN), newB[Collider](outerN)
	for _, e := range makeIDs(outerN, outerN, false) {
		ob.add(e, Body{X: 1})
		oc.add(e, Collider{R: 3})
	}
	ib, ic := newB[Body](innerN), newB[Collider](innerN)
	for _, e := range makeIDs(innerN, innerN, false) {
		ib.add(e, Body{X: 1})
		ic.add(e, Collider{R: 3})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var s float64
		for oi, oe := range ob.owners {
			ocol, ok := oc.get(oe)
			if !ok {
				continue
			}
			_ = ocol
			for ii, ie := range ib.owners {
				icol, ok := ic.get(ie)
				if !ok {
					continue
				}
				_ = ii
				s += ob.dense[oi].X + icol.R
			}
		}
		sink = s
	}
}

// InnerFlatOnce: the inner query iterated once, to price a single inner pass
// in the inlinable position.
func BenchmarkNest_InnerFlatOnce(b *testing.B) {
	inner := buildQuery(innerN, innerN)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var s float64
		for _, i := range inner.All() {
			s += i.Collider.R
		}
		sink = s
	}
}
