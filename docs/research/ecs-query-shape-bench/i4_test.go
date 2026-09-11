package qbench

import (
	"testing"
	"unsafe"
)

// Does yielding the query struct by value still pay off at 4 components,
// where MoveQ4 is 40 bytes rather than 24?

type QSI4 struct {
	bs *Store[Body]
	cs *Store[Collider]
	hs *Store[Health]
	fs *Store[Faction]
}

func (q *QSI4) All() func(func(Entity, MoveQ4) bool) {
	return func(yield func(Entity, MoveQ4) bool) {
		for i := range q.bs.owners {
			e := q.bs.owners[i]
			j := q.cs.sparse[e.idx()]
			k := q.hs.sparse[e.idx()]
			l := q.fs.sparse[e.idx()]
			if j < 0 || k < 0 || l < 0 {
				continue
			}
			if !yield(e, MoveQ4{
				Body:     &q.bs.dense[i],
				Collider: q.cs.dense[j],
				Health:   q.hs.dense[k],
				Faction:  q.fs.dense[l],
			}) {
				return
			}
		}
	}
}

func BenchmarkI_StructByValueTyped4(b *testing.B) {
	b.ReportAllocs()
	q := &QSI4{bodies, colliders, healths, factions}
	var acc float32
	for range b.N {
		for _, it := range q.All() {
			it.Px += it.Vx * dt
			acc += it.R + it.Cur + float32(it.Id)
		}
	}
	sink = acc
}

type QSJ4 struct {
	owners         []Entity
	f0, f1, f2, f3 field
}

func (q *QSJ4) All() func(func(Entity, MoveQ4) bool) {
	return func(yield func(Entity, MoveQ4) bool) {
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
			if !yield(e, buf) {
				return
			}
		}
	}
}

func BenchmarkJ_StructByValueUnsafe4(b *testing.B) {
	b.ReportAllocs()
	q := &QSJ4{
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
