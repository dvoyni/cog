package ecs

import (
	"fmt"
	"reflect"
	"sync/atomic"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/kernel/container"
)

// Ref is a handle, held in a component, to a value of type T kept outside
// the stores, in its world's refs of T. It is pointer-free, an index and a
// generation, so any T fits in a component through it: text, a list, a map.
// The zero Ref refers to nothing.
//
// A ref belongs to one component field and is never shared: a copy of a
// handle reads nothing once the ref is set or released through another.
type Ref[T any] struct {
	// index is the slot's index plus one, so the zero Ref lies past every
	// slot.
	index      uint32
	generation uint32
}

// Refs writes world W's refs holding T. It holds the write on them and a
// read on the world.
type Refs[W WorldID, T any] struct {
	refs *refsResource[W, T]
}

// RefsLookup reads world W's refs holding T. It holds a read on them and on
// the world, so several lookups run in parallel.
type RefsLookup[W WorldID, T any] struct {
	refs *refsResource[W, T]
}

// refsResource is the kernel resource of world W's refs holding T: their
// values in slots that never move, and the refs to hand out next.
//
// Refs are handed out from the back of the free list, the last freed first,
// then from fresh slots past the last. A reservation is one atomic count
// over that order, so command producers reserve in parallel under a read on
// the refs; every write settles the reservations taken so far first where
// it needs to.
type refsResource[W WorldID, T any] struct {
	// slots holds a value per ref, and may hold pointers: the GC scans refs,
	// never stores.
	slots container.Array[refSlot[T]]
	// freeRefs holds the refs to hand out next, from the back: each is a
	// freed slot at its next generation.
	freeRefs container.Array[Ref[T]]
	// reservationCount counts the refs handed out since reservations were
	// last settled: the free refs from the back first, then fresh slots.
	reservationCount atomic.Int64
}

// refSlot is one ref's value and generation.
type refSlot[T any] struct {
	value T
	// generation is the generation of the ref alive in this slot, or 0 if
	// none is: a freed slot, or one reserved and not yet stored. Generations
	// start at 1, so no handle matches a slot with none.
	generation uint32
}

// RegisterRefs registers world W's refs holding T, a kernel resource of
// their own, so systems on refs of different types run in parallel. It is
// called in a plugin's Init, after W is registered.
//
// It is a composition error, and panics, for W not to be registered or for
// refs of T to be registered in W twice. Declaring refs that were never
// registered panics too.
func RegisterRefs[W WorldID, T any](configurator *kernel.Configurator) {
	w := registeredWorld[W](configurator)
	if _, ok := configurator.Resource[*refsResource[W, T]](); ok {
		panic(fmt.Sprintf("ecs: refs of %v are registered in world %v twice", reflect.TypeFor[T](), reflect.TypeFor[W]()))
	}
	refs := &refsResource[W, T]{}
	w.shrinkers = append(w.shrinkers, refs)
	configurator.InitResource(refs)
}

// Declare declares the write on W's refs of T and a read on world W.
func (Refs[W, T]) Declare(access *kernel.Access) Refs[W, T] {
	access.Read[*worldResource[W]]()
	return Refs[W, T]{refs: access.Write[*refsResource[W, T]]().Get()}
}

// New stores value in a new ref and returns its handle.
func (r Refs[W, T]) New(value T) Ref[T] {
	h := r.refs.reserve()
	r.refs.settleReservations()
	r.refs.store(h, value)
	return h
}

// Get returns the value of ref h, or nil if h is stale or zero. The pointer
// stays valid until the ref is released. Writing through it is not a change:
// Set is.
func (r Refs[W, T]) Get(h Ref[T]) *T {
	return r.refs.get(h)
}

// Set stores value in the ref field holds, and reports whether that ref is
// alive. It gives the ref a new generation and writes the new handle into
// field, so the component holding field changes: observers see it when the
// component was reached through a pointer that marks, a pointer query term
// or Store.Write. Copies of the old handle go stale.
func (r Refs[W, T]) Set(field *Ref[T], value T) bool {
	slot := r.refs.slot(*field)
	if slot == nil {
		return false
	}
	slot.value = value
	slot.generation++
	if slot.generation == 0 {
		slot.generation = 1
	}
	field.generation = slot.generation
	return true
}

// Release frees ref h, and reports whether it was alive. Every handle to it
// goes stale at once, a shadow's included. The ECS cannot find the refs a
// component holds, so whoever removes the component or despawns its entity
// releases them first, or records a release with CommandRefs beside a
// buffered Remove or Despawn; a ref never released lives as long as its
// world.
func (r Refs[W, T]) Release(h Ref[T]) bool {
	r.refs.settleReservations()
	return r.refs.release(h)
}

// Declare declares a read on W's refs of T and on world W.
func (RefsLookup[W, T]) Declare(access *kernel.Access) RefsLookup[W, T] {
	access.Read[*worldResource[W]]()
	return RefsLookup[W, T]{refs: access.Read[*refsResource[W, T]]().Get()}
}

// Get returns the value of ref h, or nil if h is stale or zero, to read only:
// other lookups may read it at the same time.
func (l RefsLookup[W, T]) Get(h Ref[T]) *T {
	return l.refs.get(h)
}

// get returns the value of ref h, or nil if h is stale or zero. The value is
// a slot's first field, so the slot's address is the value's, nil included:
// converting it keeps the read path within the inlining budget.
func (r *refsResource[W, T]) get(h Ref[T]) *T {
	return (*T)(unsafe.Pointer(r.slot(h)))
}

// slot returns ref h's slot, or nil if h is stale or zero.
func (r *refsResource[W, T]) slot(h Ref[T]) *refSlot[T] {
	index := int(h.index - 1)
	if index >= r.slots.Len() {
		return nil
	}
	slot := r.slots.At(index)
	if slot.generation != h.generation {
		return nil
	}
	return slot
}

// reserve takes the handle of the next ref to hand out, which reads nothing
// until a value is stored in it. Concurrent reserves take distinct handles;
// nothing else may change the refs meanwhile.
func (r *refsResource[W, T]) reserve() Ref[T] {
	reservation := r.reservationCount.Add(1) - 1
	freeCount := int64(r.freeRefs.Len())
	if reservation < freeCount {
		return *r.freeRefs.At(int(freeCount - 1 - reservation))
	}
	return Ref[T]{index: uint32(int64(r.slots.Len())+reservation-freeCount) + 1, generation: 1}
}

// settleReservations takes the refs reserved so far off the free list, and
// adds the fresh slots reserved, each reading nothing until it is stored.
func (r *refsResource[W, T]) settleReservations() {
	reserved := int(r.reservationCount.Load())
	if reserved == 0 {
		return
	}
	r.reservationCount.Store(0)
	free := r.freeRefs.Len()
	taken := min(reserved, free)
	r.freeRefs.Resize(free - taken)
	r.slots.Resize(r.slots.Len() + reserved - taken)
}

// store stores value in settled reservation h, making it alive.
func (r *refsResource[W, T]) store(h Ref[T], value T) {
	slot := r.slots.At(int(h.index - 1))
	slot.value = value
	slot.generation = h.generation
}

// release frees ref h, and reports whether it was alive. Reservations are
// settled.
func (r *refsResource[W, T]) release(h Ref[T]) bool {
	slot := r.slot(h)
	if slot == nil {
		return false
	}
	var zero T
	slot.value = zero
	next := Ref[T]{index: h.index, generation: slot.generation + 1}
	if next.generation == 0 {
		next.generation = 1
	}
	slot.generation = 0
	r.freeRefs.Append(next)
	return true
}

// shrink releases the chunks past the slots and the free refs. Slots never
// shrink: a freed one keeps its place on the free list, at its next
// generation. It runs under the barrier, where reservations taken but not
// settled lie within the free refs or past the slots.
func (r *refsResource[W, T]) shrink() {
	r.slots.Shrink()
	r.freeRefs.Shrink()
}
