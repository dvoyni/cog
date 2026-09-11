package qbench

import (
	"testing"
	"unsafe"
)

// shape H: the same reflection-derived field table as G, but with the
// per-field loop unrolled for a known field count. Registration picks one of a
// few specialised fillers; no codegen, no per-field indirect call.
type QSH struct {
	owners []Entity
	f0, f1 field
}

func (q *QSH) All() func(func(Entity, *MoveQ) bool) {
	return func(yield func(Entity, *MoveQ) bool) {
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
			if !yield(e, &buf) {
				return
			}
		}
	}
}

func copyN(d, s unsafe.Pointer, n uintptr) {
	switch n {
	case 4:
		*(*[4]byte)(d) = *(*[4]byte)(s)
	case 8:
		*(*[8]byte)(d) = *(*[8]byte)(s)
	case 16:
		*(*[16]byte)(d) = *(*[16]byte)(s)
	case 32:
		*(*[32]byte)(d) = *(*[32]byte)(s)
	default:
		memcopy(d, s, n)
	}
}

func BenchmarkH_StructUnrolled2(b *testing.B) {
	b.ReportAllocs()
	q := &QSH{
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

type QSH4 struct {
	owners         []Entity
	f0, f1, f2, f3 field
}

func (q *QSH4) All() func(func(Entity, *MoveQ4) bool) {
	return func(yield func(Entity, *MoveQ4) bool) {
		var buf MoveQ4
		p := unsafe.Pointer(&buf)
		f0, f1, f2, f3 := q.f0, q.f1, q.f2, q.f3
		d0 := unsafe.Add(p, f0.off)
		d1 := unsafe.Add(p, f1.off)
		d2 := unsafe.Add(p, f2.off)
		d3 := unsafe.Add(p, f3.off)
		for _, e := range q.owners {
			i := e.idx()
			j0, j1, j2, j3 := f0.sparse[i], f1.sparse[i], f2.sparse[i], f3.sparse[i]
			if j0 < 0 || j1 < 0 || j2 < 0 || j3 < 0 {
				continue
			}
			*(*unsafe.Pointer)(d0) = unsafe.Add(f0.dense, uintptr(j0)*f0.stride)
			copyN(d1, unsafe.Add(f1.dense, uintptr(j1)*f1.stride), f1.size)
			copyN(d2, unsafe.Add(f2.dense, uintptr(j2)*f2.stride), f2.size)
			copyN(d3, unsafe.Add(f3.dense, uintptr(j3)*f3.stride), f3.size)
			if !yield(e, &buf) {
				return
			}
		}
	}
}

func BenchmarkH_StructUnrolled4(b *testing.B) {
	b.ReportAllocs()
	q := &QSH4{
		owners: bodies.owners,
		f0:     ptrField(offOf[MoveQ4]("Body"), bodies),
		f1:     valField(offOf[MoveQ4]("Collider"), colliders),
		f2:     valField(offOf[MoveQ4]("Health"), healths),
		f3:     valField(offOf[MoveQ4]("Faction"), factions),
	}
	var acc float32
	for range b.N {
		for _, it := range q.All() {
			it.Px += it.Vx * dt
			acc += it.R + it.Cur + float32(it.Id)
		}
	}
	sink = acc
}
