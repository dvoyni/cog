package qbench

import (
	"reflect"
	"testing"
	"unsafe"
)

const N = 1024
const dt = float32(0.016)

type Entity uint64

func (e Entity) idx() uint32 { return uint32(e) }

type Body struct{ Px, Py, Pz, Pw, Vx, Vy, Vz, Vw float32 } // 32B, written
type Collider struct {
	R, H        float32
	Mask, Layer uint32
} // 16B, read
type Health struct{ Cur, Max float32 }  // 8B, read
type Faction struct{ Id, Rank uint32 }  // 8B, read

type Store[T any] struct {
	dense  []T
	owners []Entity
	sparse []int32
}

func newStore[T any](n int) *Store[T] {
	s := &Store[T]{dense: make([]T, n), owners: make([]Entity, n), sparse: make([]int32, n)}
	for i := range n {
		s.owners[i] = Entity(i)
		s.sparse[i] = int32(i)
	}
	return s
}

var (
	bodies    = newStore[Body](N)
	colliders = newStore[Collider](N)
	healths   = newStore[Health](N)
	factions  = newStore[Faction](N)
)

var sink float32

// ---------------------------------------------------------------- baseline

func BenchmarkA_SliceLoop2(b *testing.B) {
	b.ReportAllocs()
	var acc float32
	for range b.N {
		for i := range bodies.owners {
			e := bodies.owners[i]
			j := colliders.sparse[e.idx()]
			if j < 0 {
				continue
			}
			p := &bodies.dense[i]
			c := colliders.dense[j]
			p.Px += p.Vx * dt
			acc += c.R
		}
	}
	sink = acc
}

func BenchmarkA_SliceLoop4(b *testing.B) {
	b.ReportAllocs()
	var acc float32
	for range b.N {
		for i := range bodies.owners {
			e := bodies.owners[i]
			j := colliders.sparse[e.idx()]
			k := healths.sparse[e.idx()]
			l := factions.sparse[e.idx()]
			if j < 0 || k < 0 || l < 0 {
				continue
			}
			p := &bodies.dense[i]
			p.Px += p.Vx * dt
			acc += colliders.dense[j].R + healths.dense[k].Cur + float32(factions.dense[l].Id)
		}
	}
	sink = acc
}

// ------------------------------------------------- shape B: positional At(i)

type Q2 struct {
	bs *Store[Body]
	cs *Store[Collider]
}

func (q *Q2) Len() int { return len(q.bs.owners) }

func (q *Q2) At(i int) (Entity, *Body, Collider, bool) {
	e := q.bs.owners[i]
	j := q.cs.sparse[e.idx()]
	if j < 0 {
		var z Collider
		return e, nil, z, false
	}
	return e, &q.bs.dense[i], q.cs.dense[j], true
}

func BenchmarkB_PositionalAt2(b *testing.B) {
	b.ReportAllocs()
	q := &Q2{bodies, colliders}
	var acc float32
	for range b.N {
		for i := range q.Len() {
			_, p, c, ok := q.At(i)
			if !ok {
				continue
			}
			p.Px += p.Vx * dt
			acc += c.R
		}
	}
	sink = acc
}

type Q4 struct {
	bs *Store[Body]
	cs *Store[Collider]
	hs *Store[Health]
	fs *Store[Faction]
}

func (q *Q4) Len() int { return len(q.bs.owners) }

func (q *Q4) At(i int) (Entity, *Body, Collider, Health, Faction, bool) {
	e := q.bs.owners[i]
	j := q.cs.sparse[e.idx()]
	k := q.hs.sparse[e.idx()]
	l := q.fs.sparse[e.idx()]
	if j < 0 || k < 0 || l < 0 {
		var c Collider
		var h Health
		var f Faction
		return e, nil, c, h, f, false
	}
	return e, &q.bs.dense[i], q.cs.dense[j], q.hs.dense[k], q.fs.dense[l], true
}

func BenchmarkB_PositionalAt4(b *testing.B) {
	b.ReportAllocs()
	q := &Q4{bodies, colliders, healths, factions}
	var acc float32
	for range b.N {
		for i := range q.Len() {
			_, p, c, h, f, ok := q.At(i)
			if !ok {
				continue
			}
			p.Px += p.Vx * dt
			acc += c.R + h.Cur + float32(f.Id)
		}
	}
	sink = acc
}

// --------------------------------------------- shape C: positional Each(fn)

func (q *Q2) Each(fn func(Entity, *Body, Collider)) {
	for i := range q.bs.owners {
		e := q.bs.owners[i]
		j := q.cs.sparse[e.idx()]
		if j < 0 {
			continue
		}
		fn(e, &q.bs.dense[i], q.cs.dense[j])
	}
}

func BenchmarkC_PositionalEach2(b *testing.B) {
	b.ReportAllocs()
	q := &Q2{bodies, colliders}
	var acc float32
	for range b.N {
		q.Each(func(_ Entity, p *Body, c Collider) {
			p.Px += p.Vx * dt
			acc += c.R
		})
	}
	sink = acc
}

// ------------------------- shape D: struct query, monomorphised typed fill

type MoveQ struct {
	*Body
	Collider
}

type MoveQ4 struct {
	*Body
	Collider
	Health
	Faction
}

type QS struct {
	bs *Store[Body]
	cs *Store[Collider]
}

func (q *QS) All() func(func(Entity, *MoveQ) bool) {
	return func(yield func(Entity, *MoveQ) bool) {
		var buf MoveQ
		for i := range q.bs.owners {
			e := q.bs.owners[i]
			j := q.cs.sparse[e.idx()]
			if j < 0 {
				continue
			}
			buf.Body = &q.bs.dense[i]
			buf.Collider = q.cs.dense[j]
			if !yield(e, &buf) {
				return
			}
		}
	}
}

func BenchmarkD_StructTyped2(b *testing.B) {
	b.ReportAllocs()
	q := &QS{bodies, colliders}
	var acc float32
	for range b.N {
		for _, it := range q.All() {
			it.Px += it.Vx * dt
			acc += it.R
		}
	}
	sink = acc
}

type QS4 struct {
	bs *Store[Body]
	cs *Store[Collider]
	hs *Store[Health]
	fs *Store[Faction]
}

func (q *QS4) All() func(func(Entity, *MoveQ4) bool) {
	return func(yield func(Entity, *MoveQ4) bool) {
		var buf MoveQ4
		for i := range q.bs.owners {
			e := q.bs.owners[i]
			j := q.cs.sparse[e.idx()]
			k := q.hs.sparse[e.idx()]
			l := q.fs.sparse[e.idx()]
			if j < 0 || k < 0 || l < 0 {
				continue
			}
			buf.Body = &q.bs.dense[i]
			buf.Collider = q.cs.dense[j]
			buf.Health = q.hs.dense[k]
			buf.Faction = q.fs.dense[l]
			if !yield(e, &buf) {
				return
			}
		}
	}
}

func BenchmarkD_StructTyped4(b *testing.B) {
	b.ReportAllocs()
	q := &QS4{bodies, colliders, healths, factions}
	var acc float32
	for range b.N {
		for _, it := range q.All() {
			it.Px += it.Vx * dt
			acc += it.R + it.Cur + float32(it.Id)
		}
	}
	sink = acc
}

// ------------------ shape E: struct query, offset + memmove fill (reflective)

type QSU struct {
	bs      *Store[Body]
	cs      *Store[Collider]
	offBody uintptr
	offCol  uintptr
	szCol   uintptr
}

func memcopy(dst, src unsafe.Pointer, n uintptr) {
	copy(unsafe.Slice((*byte)(dst), n), unsafe.Slice((*byte)(src), n))
}

func (q *QSU) All() func(func(Entity, *MoveQ) bool) {
	return func(yield func(Entity, *MoveQ) bool) {
		var buf MoveQ
		p := unsafe.Pointer(&buf)
		for i := range q.bs.owners {
			e := q.bs.owners[i]
			j := q.cs.sparse[e.idx()]
			if j < 0 {
				continue
			}
			*(**Body)(unsafe.Add(p, q.offBody)) = &q.bs.dense[i]
			memcopy(unsafe.Add(p, q.offCol), unsafe.Pointer(&q.cs.dense[j]), q.szCol)
			if !yield(e, &buf) {
				return
			}
		}
	}
}

func newQSU() *QSU {
	t := reflect.TypeFor[MoveQ]()
	f0, _ := t.FieldByName("Body")
	f1, _ := t.FieldByName("Collider")
	return &QSU{bodies, colliders, f0.Offset, f1.Offset, f1.Type.Size()}
}

func BenchmarkE_StructMemmove2(b *testing.B) {
	b.ReportAllocs()
	q := newQSU()
	var acc float32
	for range b.N {
		for _, it := range q.All() {
			it.Px += it.Vx * dt
			acc += it.R
		}
	}
	sink = acc
}

// ------------------- shape F: struct query, per-field binder closures (general)

type binder func(dst unsafe.Pointer, e Entity) bool

func ptrBinder[C any](off uintptr, st *Store[C]) binder {
	return func(dst unsafe.Pointer, e Entity) bool {
		j := st.sparse[e.idx()]
		if j < 0 {
			return false
		}
		*(**C)(unsafe.Add(dst, off)) = &st.dense[j]
		return true
	}
}

func valBinder[C any](off uintptr, st *Store[C]) binder {
	return func(dst unsafe.Pointer, e Entity) bool {
		j := st.sparse[e.idx()]
		if j < 0 {
			return false
		}
		*(*C)(unsafe.Add(dst, off)) = st.dense[j]
		return true
	}
}

type QSB struct {
	owners  []Entity
	binders []binder
}

func (q *QSB) All() func(func(Entity, *MoveQ) bool) {
	return func(yield func(Entity, *MoveQ) bool) {
		var buf MoveQ
		p := unsafe.Pointer(&buf)
	outer:
		for _, e := range q.owners {
			for _, bd := range q.binders {
				if !bd(p, e) {
					continue outer
				}
			}
			if !yield(e, &buf) {
				return
			}
		}
	}
}

func newQSB() *QSB {
	t := reflect.TypeFor[MoveQ]()
	f0, _ := t.FieldByName("Body")
	f1, _ := t.FieldByName("Collider")
	return &QSB{
		owners:  bodies.owners,
		binders: []binder{ptrBinder(f0.Offset, bodies), valBinder(f1.Offset, colliders)},
	}
}

func BenchmarkF_StructBinder2(b *testing.B) {
	b.ReportAllocs()
	q := newQSB()
	var acc float32
	for range b.N {
		for _, it := range q.All() {
			it.Px += it.Vx * dt
			acc += it.R
		}
	}
	sink = acc
}

type QSB4 struct {
	owners  []Entity
	binders []binder
}

func (q *QSB4) All() func(func(Entity, *MoveQ4) bool) {
	return func(yield func(Entity, *MoveQ4) bool) {
		var buf MoveQ4
		p := unsafe.Pointer(&buf)
	outer:
		for _, e := range q.owners {
			for _, bd := range q.binders {
				if !bd(p, e) {
					continue outer
				}
			}
			if !yield(e, &buf) {
				return
			}
		}
	}
}

func newQSB4() *QSB4 {
	t := reflect.TypeFor[MoveQ4]()
	f0, _ := t.FieldByName("Body")
	f1, _ := t.FieldByName("Collider")
	f2, _ := t.FieldByName("Health")
	f3, _ := t.FieldByName("Faction")
	return &QSB4{
		owners: bodies.owners,
		binders: []binder{
			ptrBinder(f0.Offset, bodies),
			valBinder(f1.Offset, colliders),
			valBinder(f2.Offset, healths),
			valBinder(f3.Offset, factions),
		},
	}
}

func BenchmarkF_StructBinder4(b *testing.B) {
	b.ReportAllocs()
	q := newQSB4()
	var acc float32
	for range b.N {
		for _, it := range q.All() {
			it.Px += it.Vx * dt
			acc += it.R + it.Cur + float32(it.Id)
		}
	}
	sink = acc
}
