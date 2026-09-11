package qbench

import (
	"reflect"
	"testing"
	"unsafe"
)

// shape G: struct query filled from a flat, precomputed field table.
// No closures, no per-field indirect call, no per-field type knowledge:
// offsets and sizes only, with a size-class dispatch for the copy.
type field struct {
	off    uintptr // field offset within the query struct
	stride uintptr // element size in the source dense array
	size   uintptr // bytes to copy (0 => pointer field)
	sparse []int32
	dense  unsafe.Pointer
	isPtr  bool
}

type QSG struct {
	owners []Entity
	fields []field
}

func fillG(p unsafe.Pointer, fs []field, e Entity) bool {
	for i := range fs {
		f := &fs[i]
		j := f.sparse[e.idx()]
		if j < 0 {
			return false
		}
		src := unsafe.Add(f.dense, uintptr(j)*f.stride)
		d := unsafe.Add(p, f.off)
		if f.isPtr {
			*(*unsafe.Pointer)(d) = src
			continue
		}
		switch f.size {
		case 4:
			*(*[4]byte)(d) = *(*[4]byte)(src)
		case 8:
			*(*[8]byte)(d) = *(*[8]byte)(src)
		case 16:
			*(*[16]byte)(d) = *(*[16]byte)(src)
		case 32:
			*(*[32]byte)(d) = *(*[32]byte)(src)
		default:
			memcopy(d, src, f.size)
		}
	}
	return true
}

func (q *QSG) All() func(func(Entity, *MoveQ) bool) {
	return func(yield func(Entity, *MoveQ) bool) {
		var buf MoveQ
		p := unsafe.Pointer(&buf)
		for _, e := range q.owners {
			if !fillG(p, q.fields, e) {
				continue
			}
			if !yield(e, &buf) {
				return
			}
		}
	}
}

type QSG4 struct {
	owners []Entity
	fields []field
}

func (q *QSG4) All() func(func(Entity, *MoveQ4) bool) {
	return func(yield func(Entity, *MoveQ4) bool) {
		var buf MoveQ4
		p := unsafe.Pointer(&buf)
		for _, e := range q.owners {
			if !fillG(p, q.fields, e) {
				continue
			}
			if !yield(e, &buf) {
				return
			}
		}
	}
}

func ptrField[C any](off uintptr, st *Store[C]) field {
	var z C
	return field{off: off, stride: unsafe.Sizeof(z), size: 0, sparse: st.sparse,
		dense: unsafe.Pointer(&st.dense[0]), isPtr: true}
}

func valField[C any](off uintptr, st *Store[C]) field {
	var z C
	return field{off: off, stride: unsafe.Sizeof(z), size: unsafe.Sizeof(z), sparse: st.sparse,
		dense: unsafe.Pointer(&st.dense[0]), isPtr: false}
}

func offOf[T any](name string) uintptr {
	f, _ := reflect.TypeFor[T]().FieldByName(name)
	return f.Offset
}

func BenchmarkG_StructTable2(b *testing.B) {
	b.ReportAllocs()
	q := &QSG{owners: bodies.owners, fields: []field{
		ptrField(offOf[MoveQ]("Body"), bodies),
		valField(offOf[MoveQ]("Collider"), colliders),
	}}
	var acc float32
	for range b.N {
		for _, it := range q.All() {
			it.Px += it.Vx * dt
			acc += it.R
		}
	}
	sink = acc
}

func BenchmarkG_StructTable4(b *testing.B) {
	b.ReportAllocs()
	q := &QSG4{owners: bodies.owners, fields: []field{
		ptrField(offOf[MoveQ4]("Body"), bodies),
		valField(offOf[MoveQ4]("Collider"), colliders),
		valField(offOf[MoveQ4]("Health"), healths),
		valField(offOf[MoveQ4]("Faction"), factions),
	}}
	var acc float32
	for range b.N {
		for _, it := range q.All() {
			it.Px += it.Vx * dt
			acc += it.R + it.Cur + float32(it.Id)
		}
	}
	sink = acc
}
