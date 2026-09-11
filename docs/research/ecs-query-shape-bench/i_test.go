package qbench

import (
	"testing"
	"unsafe"
)

// ---- I: struct yielded BY VALUE, built with typed (monomorphised) writes.
// Nothing takes the address of a buffer, so the compiler may keep the whole
// query struct in registers. Theoretical floor for the struct shape.

type QSI struct {
	bs *Store[Body]
	cs *Store[Collider]
}

func (q *QSI) All() func(func(Entity, MoveQ) bool) {
	return func(yield func(Entity, MoveQ) bool) {
		for i := range q.bs.owners {
			e := q.bs.owners[i]
			j := q.cs.sparse[e.idx()]
			if j < 0 {
				continue
			}
			if !yield(e, MoveQ{Body: &q.bs.dense[i], Collider: q.cs.dense[j]}) {
				return
			}
		}
	}
}

func BenchmarkI_StructByValueTyped2(b *testing.B) {
	b.ReportAllocs()
	q := &QSI{bodies, colliders}
	var acc float32
	for range b.N {
		for _, it := range q.All() {
			it.Px += it.Vx * dt
			acc += it.R
		}
	}
	sink = acc
}

// ---- J: struct yielded BY VALUE, but filled through unsafe offsets first.
// Taking unsafe.Pointer(&buf) forces buf into memory, then the yield copies it
// back out. Tests whether by-value yielding helps a reflective fill.

type QSJ struct {
	owners []Entity
	f0, f1 field
}

func (q *QSJ) All() func(func(Entity, MoveQ) bool) {
	return func(yield func(Entity, MoveQ) bool) {
		var buf MoveQ
		p := unsafe.Pointer(&buf)
		f0, f1 := q.f0, q.f1
		d0, d1 := unsafe.Add(p, f0.off), unsafe.Add(p, f1.off)
		for _, e := range q.owners {
			j0 := f0.sparse[e.idx()]
			if j0 < 0 {
				continue
			}
			j1 := f1.sparse[e.idx()]
			if j1 < 0 {
				continue
			}
			*(*unsafe.Pointer)(d0) = unsafe.Add(f0.dense, uintptr(j0)*f0.stride)
			copyN(d1, unsafe.Add(f1.dense, uintptr(j1)*f1.stride), f1.size)
			if !yield(e, buf) {
				return
			}
		}
	}
}

func BenchmarkJ_StructByValueUnsafe2(b *testing.B) {
	b.ReportAllocs()
	q := &QSJ{
		owners: bodies.owners,
		f0:     ptrField(offOf[MoveQ]("Body"), bodies),
		f1:     valField(offOf[MoveQ]("Collider"), colliders),
	}
	var acc float32
	for range b.N {
		for _, it := range q.All() {
			it.Px += it.Vx * dt
			acc += it.R
		}
	}
	sink = acc
}

// ---- L: all-pointer query struct, unsafe fill, yielded by pointer.
// No component value is copied at all — isolates the 16-byte copy from the
// cost of the buffer round-trip itself.

type MoveQP struct {
	Body     *Body
	Collider *Collider
}

type QSL struct {
	owners []Entity
	f0, f1 field
}

func (q *QSL) All() func(func(Entity, *MoveQP) bool) {
	return func(yield func(Entity, *MoveQP) bool) {
		var buf MoveQP
		p := unsafe.Pointer(&buf)
		f0, f1 := q.f0, q.f1
		d0, d1 := unsafe.Add(p, f0.off), unsafe.Add(p, f1.off)
		for _, e := range q.owners {
			j0 := f0.sparse[e.idx()]
			if j0 < 0 {
				continue
			}
			j1 := f1.sparse[e.idx()]
			if j1 < 0 {
				continue
			}
			*(*unsafe.Pointer)(d0) = unsafe.Add(f0.dense, uintptr(j0)*f0.stride)
			*(*unsafe.Pointer)(d1) = unsafe.Add(f1.dense, uintptr(j1)*f1.stride)
			if !yield(e, &buf) {
				return
			}
		}
	}
}

func BenchmarkL_StructAllPtrUnsafe2(b *testing.B) {
	b.ReportAllocs()
	q := &QSL{
		owners: bodies.owners,
		f0:     ptrField(offOf[MoveQP]("Body"), bodies),
		f1:     ptrField(offOf[MoveQP]("Collider"), colliders),
	}
	var acc float32
	for range b.N {
		for _, it := range q.All() {
			it.Body.Px += it.Body.Vx * dt
			acc += it.Collider.R
		}
	}
	sink = acc
}
