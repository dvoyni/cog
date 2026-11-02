package ecs

import (
	"fmt"
	"reflect"

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
	index      uint32
	generation uint32
}

// Refs writes world W's refs holding T. It holds the write on them and a
// read on the world.
type Refs[W WorldID, T any] struct {
	refs *refs[W, T]
}

// RefsLookup reads world W's refs holding T. It holds a read on them and on
// the world, so several lookups run in parallel.
type RefsLookup[W WorldID, T any] struct {
	refs *refs[W, T]
}

// refs is the kernel resource of world W's refs holding T: their values in
// slots that never move, and the slots freed for reuse.
type refs[W WorldID, T any] struct {
	// slots holds a value per ref, and may hold pointers: the GC scans refs,
	// never stores.
	slots container.Array[refSlot[T]]
	// freeIndices holds the indices of freed slots, reused from the back.
	freeIndices container.Array[uint32]
}

// refSlot is one ref's value and generation.
type refSlot[T any] struct {
	value T
	// generation is the generation of the ref in this slot; a freed slot's
	// is the one its next ref gets. It starts at 1, so the zero Ref matches
	// no slot.
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
	registeredWorld[W](configurator)
	if _, ok := configurator.Resource[*refs[W, T]](); ok {
		panic(fmt.Sprintf("ecs: refs of %v are registered in world %v twice", reflect.TypeFor[T](), reflect.TypeFor[W]()))
	}
	configurator.InitResource(&refs[W, T]{})
}

// Declare declares the write on W's refs of T and a read on world W.
func (Refs[W, T]) Declare(access *kernel.Access) Refs[W, T] {
	access.Read[*world[W]]()
	return Refs[W, T]{refs: access.Write[*refs[W, T]]().Get()}
}

// New stores value in a new ref and returns its handle.
func (r Refs[W, T]) New(value T) Ref[T] {
	if n := r.refs.freeIndices.Len(); n > 0 {
		index := *r.refs.freeIndices.At(n - 1)
		r.refs.freeIndices.Resize(n - 1)
		slot := r.refs.slots.At(int(index))
		slot.value = value
		return Ref[T]{index: index, generation: slot.generation}
	}
	index := r.refs.slots.Append(refSlot[T]{value: value, generation: 1})
	return Ref[T]{index: uint32(index), generation: 1}
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
// releases them first; a ref never released lives as long as its world.
func (r Refs[W, T]) Release(h Ref[T]) bool {
	slot := r.refs.slot(h)
	if slot == nil {
		return false
	}
	var zero T
	slot.value = zero
	slot.generation++
	if slot.generation == 0 {
		slot.generation = 1
	}
	r.refs.freeIndices.Append(h.index)
	return true
}

// Declare declares a read on W's refs of T and on world W.
func (RefsLookup[W, T]) Declare(access *kernel.Access) RefsLookup[W, T] {
	access.Read[*world[W]]()
	return RefsLookup[W, T]{refs: access.Read[*refs[W, T]]().Get()}
}

// Get returns the value of ref h, or nil if h is stale or zero, to read only:
// other lookups may read it at the same time.
func (l RefsLookup[W, T]) Get(h Ref[T]) *T {
	return l.refs.get(h)
}

// get returns the value of ref h, or nil if h is stale or zero.
func (r *refs[W, T]) get(h Ref[T]) *T {
	slot := r.slot(h)
	if slot == nil {
		return nil
	}
	return &slot.value
}

// slot returns ref h's slot, or nil if h is stale or zero.
func (r *refs[W, T]) slot(h Ref[T]) *refSlot[T] {
	if int(h.index) >= r.slots.Len() {
		return nil
	}
	slot := r.slots.At(int(h.index))
	if slot.generation != h.generation {
		return nil
	}
	return slot
}
