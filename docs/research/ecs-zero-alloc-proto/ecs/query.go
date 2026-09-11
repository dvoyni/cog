package ecs

import (
	"fmt"
	"iter"
	"reflect"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
)

// A Query field is one of three things, and which one is read off the field's
// type alone: a pointer field writes and yields the stored value, a value field
// reads and yields a copy, and a Without[T] field filters and yields nothing
// (cog#238). A read yielding a pointer would race, which is why the second kind
// copies.
const (
	kindPtr uint8 = iota
	kindVal
	kindFilter
)

type field struct {
	core   *storeCore // resolved per tick, valid only under the handler's locks
	off    uintptr    // byte offset of this field within Q
	size   uintptr
	stride uintptr
	kind   uint8
}

// queryCore is the non-generic half of a Query, so the per-tick work can be
// reached through an interface without naming Q.
type queryCore struct {
	f       []field
	resolve []func() *storeCore
	drv     int
	filter  bool
}

// refresh runs once per tick, inside the handler's locks. It resolves each
// Store through its kernel handle and then picks the Driver by scanning Store
// lengths -- per run, never cached, because the cache would be the global index
// this storage model exists to avoid (cog#239).
func (q *queryCore) refresh() {
	best, bestN := -1, 0
	for i := range q.f {
		c := q.resolve[i]()
		q.f[i].core = c
		if q.f[i].kind == kindFilter {
			continue // a filter can never drive (cog#239)
		}
		if n := len(c.owners); best < 0 || n < bestN {
			best, bestN = i, n
		}
	}
	q.drv = best
}

// Query is a System parameter. Q is a struct whose fields name the Components.
type Query[Q any] struct {
	queryCore

	// buf is the fill buffer every yielded *Q points at, and it is a field of
	// the Query rather than a local in All for one measured reason: All hands
	// &buf to an opaque yield, so as a local it escapes and costs one heap
	// allocation per Query per tick -- 24 B a tick for a two-Component Query,
	// and the only allocation the prototype charged above a hand-written
	// subscription. The Query is allocated once, at registration, so as a field
	// it costs nothing per frame.
	//
	// Nothing changes semantically: the same buffer is already reused for every
	// Entity, so a System that retained a yielded *Q across iterations was
	// reading the wrong Entity either way.
	buf Q
}

// plan runs once, at registration. It is the only reflection in the design and
// it never runs again: the lock set is a pure function of types known here,
// which is how the straight-line Lock rule is kept rather than broken (cog#238).
func (q *Query[Q]) plan(a kernel.ResourceAccess, en *Entities) {
	t := reflect.TypeFor[Q]()
	if t.Kind() != reflect.Struct {
		panic(fmt.Sprintf("ecs: Query type %s is not a struct", t))
	}
	present := 0
	for i := range t.NumField() {
		sf := t.Field(i)
		var comp reflect.Type
		var kind uint8
		switch {
		case sf.Type.Implements(reflect.TypeFor[filterField]()):
			comp = reflect.Zero(sf.Type).Interface().(filterField).filtered()
			kind = kindFilter
			q.filter = true
		case sf.Type.Kind() == reflect.Pointer:
			comp, kind = sf.Type.Elem(), kindPtr
			present++
		default:
			comp, kind = sf.Type, kindVal
			present++
		}
		ops, ok := en.ops[comp]
		if !ok {
			panic(fmt.Sprintf("ecs: Query %s names unregistered Component %s", t, comp))
		}
		// Pointer-ness is the access mode, so it is also what picks the lock.
		declare := ops.declareRead
		if kind == kindPtr {
			declare = ops.declareWrite
		}
		q.resolve = append(q.resolve, declare(a))
		q.f = append(q.f, field{off: sf.Offset, size: ops.size, stride: ops.size, kind: kind})
	}
	if present == 0 {
		panic(fmt.Sprintf("ecs: Query %s names no present-typed Component, so nothing can drive it", t))
	}
}

// All yields every Entity matching the Query, and a filled Q for each.
//
// It walks the Driver's dense array **backwards**, and that is a guarantee, not
// an implementation detail: swap-remove moves the last row into the hole, and a
// walk that started at the end has already been there, so a System may
// restructure the Entity it is on. Forward, the same walk silently skips a
// third of the Store (cog#240).
func (q *Query[Q]) All() iter.Seq2[Entity, *Q] {
	return func(yield func(Entity, *Q) bool) {
		buf := &q.buf
		p := unsafe.Pointer(buf)
		owners := q.f[q.drv].core.owners
		if len(q.f) == 2 && !q.filter {
			f0, f1 := q.f[0], q.f[1]
			d0, d1 := unsafe.Add(p, f0.off), unsafe.Add(p, f1.off)
			for i := len(owners) - 1; i >= 0; i-- {
				e := owners[i]
				j0, ok0 := f0.core.probe(e)
				if !ok0 {
					continue
				}
				j1, ok1 := f1.core.probe(e)
				if !ok1 {
					continue
				}
				fill(d0, f0, j0)
				fill(d1, f1, j1)
				if !yield(e, buf) {
					return
				}
			}
			return
		}
		q.general(p, buf, owners, yield)
	}
}

// general is every shape the unrolled path does not cover. It is a separate
// function so the two-Component loop above stays small enough to inline at the
// range site, which cog#234 found is the whole difference between 622ns and
// 2470ns.
func (q *Query[Q]) general(p unsafe.Pointer, buf *Q, owners []Entity, yield func(Entity, *Q) bool) {
outer:
	for i := len(owners) - 1; i >= 0; i-- {
		e := owners[i]
		for k := range q.f {
			f := q.f[k]
			j, ok := f.core.probe(e)
			if f.kind == kindFilter {
				if ok {
					continue outer
				}
				continue
			}
			if !ok {
				continue outer
			}
			fill(unsafe.Add(p, f.off), f, j)
		}
		if !yield(e, buf) {
			return
		}
	}
}

// fill writes one Component into the fill buffer: the stored address for a
// pointer field, a copy for a value field.
//
// Two attempts to speed this up both made it slower and were reverted. Spreading
// the field into six scalar arguments -- so the loop would hold them in
// registers rather than copy a 48-byte struct -- stopped the function inlining
// and doubled the two-Component frame, 37us to 73us at 10k. Unrolling a
// three-field path beside the two-field one cost about 6% rather than saving
// any. The small by-value helper is what the compiler handles best here.
func fill(dst unsafe.Pointer, f field, j uint32) {
	if f.kind == kindPtr {
		*(*unsafe.Pointer)(dst) = f.core.at(j)
		return
	}
	copyN(dst, f.core.at(j), f.size)
}

// queryLike is how the handler builder reaches a *Query[Q] it constructed from
// a reflect.Type. Instantiating a generic from a reflect.Type is impossible
// (cog#234), but allocating one and reaching its methods through an interface
// is not.
type queryLike interface {
	plan(a kernel.ResourceAccess, en *Entities)
	refresh()
}
