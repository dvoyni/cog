package sbench

import (
	"testing"
	"unsafe"
)

// The iteration contract cog#238 fixed: All() yields (Entity, *T), filled from
// a precomputed field table, unrolled by field count. cog#239's body calls
// nested iteration "the sharp edge" — cog#234 measured nested *boxed*
// iteration at 2050 allocs / 57 KB per frame, because the inner closure is
// rebuilt once per outer entity. This file checks whether our shape survives
// being placed in an inner position, using the one-load probe from Q1.

type MoveQ struct {
	Body     *Body
	Collider Collider
}

type fieldT struct {
	sparse []uint64
	dense  unsafe.Pointer
	stride uintptr
	size   uintptr
	off    uintptr
}

type queryH struct {
	owners []Entity
	f0, f1 fieldT // f0 pointer field (write), f1 value field (read)
}

func copyN(dst, src unsafe.Pointer, n uintptr) {
	switch n {
	case 8:
		*(*uint64)(dst) = *(*uint64)(src)
	case 16:
		*(*[2]uint64)(dst) = *(*[2]uint64)(src)
	default:
		copy(unsafe.Slice((*byte)(dst), n), unsafe.Slice((*byte)(src), n))
	}
}

func (q *queryH) All() func(func(Entity, *MoveQ) bool) {
	return func(yield func(Entity, *MoveQ) bool) {
		var buf MoveQ
		p := unsafe.Pointer(&buf)
		f0, f1 := q.f0, q.f1
		d0 := unsafe.Add(p, f0.off)
		d1 := unsafe.Add(p, f1.off)
		for _, e := range q.owners {
			v0 := f0.sparse[e.idx()]
			if uint32(v0>>32) != e.gen() {
				continue
			}
			v1 := f1.sparse[e.idx()]
			if uint32(v1>>32) != e.gen() {
				continue
			}
			*(*unsafe.Pointer)(d0) = unsafe.Add(f0.dense, uintptr(uint32(v0))*f0.stride)
			copyN(d1, unsafe.Add(f1.dense, uintptr(uint32(v1))*f1.stride), f1.size)
			if !yield(e, &buf) {
				return
			}
		}
	}
}

func buildQuery(n int, space uint32) *queryH {
	bs, cs := newB[Body](space), newB[Collider](space)
	for _, e := range makeIDs(n, space, false) {
		bs.add(e, Body{X: 1})
		cs.add(e, Collider{R: 3})
	}
	return &queryH{
		owners: bs.owners,
		f0: fieldT{
			sparse: bs.sparse,
			dense:  unsafe.Pointer(&bs.dense[0]),
			stride: unsafe.Sizeof(Body{}),
			size:   unsafe.Sizeof(Body{}),
			off:    unsafe.Offsetof(MoveQ{}.Body),
		},
		f1: fieldT{
			sparse: cs.sparse,
			dense:  unsafe.Pointer(&cs.dense[0]),
			stride: unsafe.Sizeof(Collider{}),
			size:   unsafe.Sizeof(Collider{}),
			off:    unsafe.Offsetof(MoveQ{}.Collider),
		},
	}
}

const (
	outerN = 1024
	innerN = 64
)

// Flat: the ordinary case, one query iterated once.
func BenchmarkNest_Flat(b *testing.B) {
	q := buildQuery(outerN, outerN)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var s float64
		for _, it := range q.All() {
			s += it.Body.X + it.Collider.R
		}
		sink = s
	}
}

// Nested: All() is called once per outer entity, which is the shape that cost
// 2050 allocs when the iterator was obtained through a boxed resource handle.
func BenchmarkNest_Nested(b *testing.B) {
	outer := buildQuery(outerN, outerN)
	inner := buildQuery(innerN, innerN)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var s float64
		for _, o := range outer.All() {
			for _, i := range inner.All() {
				s += o.Body.X + i.Collider.R
			}
		}
		sink = s
	}
}

// Hoisted: the same nested work with the inner iterator obtained once. If this
// matches Nested, rebuilding the inner closure is free and the spec needs to
// say nothing; if it does not, the spec owes advice.
func BenchmarkNest_Hoisted(b *testing.B) {
	outer := buildQuery(outerN, outerN)
	inner := buildQuery(innerN, innerN)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var s float64
		seq := inner.All()
		for _, o := range outer.All() {
			for _, i := range seq {
				s += o.Body.X + i.Collider.R
			}
		}
		sink = s
	}
}

// Boxed: the failure mode #234 measured, reproduced here for contrast — the
// iterator reaches the range statement through an interface, so the range
// statement no longer sees one unambiguous func literal.
type seqSource interface {
	Seq() func(func(Entity, *MoveQ) bool)
}

func (q *queryH) Seq() func(func(Entity, *MoveQ) bool) { return q.All() }

func BenchmarkNest_NestedBoxed(b *testing.B) {
	outer := buildQuery(outerN, outerN)
	var inner seqSource = buildQuery(innerN, innerN)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var s float64
		for _, o := range outer.All() {
			for _, i := range inner.Seq() {
				s += o.Body.X + i.Collider.R
			}
		}
		sink = s
	}
}
