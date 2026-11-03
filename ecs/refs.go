package ecs

import (
	"fmt"
	"iter"
	"math"
	"reflect"
	"slices"
	"sync/atomic"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/kernel/container"
)

// Ref is a handle to a value of type T kept outside the stores, in its
// world's refs of T. It is pointer-free, an index and a generation, so any T
// fits in a component through it: text, a list, a mesh. The zero Ref refers
// to nothing.
//
// A handle is stable: setting its value keeps it, so any number of
// components may hold one ref, and none of them changes when its value does.
// DeltaRefs sees the change. Once the ref is released, every copy of the
// handle reads nothing.
type Ref[T any] struct {
	index      uint32
	generation uint32
}

// RefsOptions configures refs as RegisterRefs or RegisterKeyedRefs creates
// them.
type RefsOptions struct {
	// FirstIndex and LastIndex bound the allocation range, inclusive: the
	// indices the refs hand out to new refs. Indices outside it are only
	// ever placed with NewAt or PutAt. A LastIndex of 0 means the top of the
	// 32-bit range.
	FirstIndex, LastIndex uint32
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

// DeltaRefs walks world W's refs of T with a delta this observation window,
// plain or keyed. It holds a read on them and on the world, and observes
// them from the world's first observation reset.
type DeltaRefs[W WorldID, T any] struct {
	refs *refsResource[W, T]
}

// RefDelta is what happened to one ref in an observation window: added,
// changed or released, and its value now, or as last held for a released
// one, to read only.
type RefDelta[T any] struct {
	Delta Delta
	Value *T
}

// refsResource is the kernel resource of world W's refs holding T: their
// values in slots that never move, and the refs to hand out next.
//
// Refs are handed out from the back of the free list, the last freed first,
// then from fresh indices of the allocation range. A reservation is one
// atomic count over that order, so command producers reserve in parallel
// under a read on the refs; every write settles the reservations taken so
// far first where it needs to.
//
// Observed refs keep a state per slot, and keep a released ref's slot, with
// its value, off the free list until the window ends, so no index is reused
// in the window its ref was released in.
type refsResource[W WorldID, T any] struct {
	// slots holds a value per ref, and may hold pointers: the GC scans refs,
	// never stores.
	slots indexTable[refSlot[T]]
	// firstIndex and lastIndex bound the allocation range, inclusive.
	firstIndex, lastIndex uint32
	// freeRefs holds the refs to hand out next, from the back: each is a
	// freed slot in the allocation range at its next generation.
	freeRefs container.Array[Ref[T]]
	// reservationCount counts the refs handed out since reservations were
	// last settled: the free refs from the back first, then fresh indices
	// from nextFreshIndex.
	reservationCount atomic.Int64
	// nextFreshIndex is the first index in the allocation range never handed
	// out. It is one past lastIndex once the range is used up.
	nextFreshIndex int64
	// liveRefs counts the live refs, which bounds the listing.
	liveRefs    int
	observation observationState
	// changedRefs lists, once each, the refs whose slots left refUnchanged
	// this window, so DeltaRefs and the reset visit only them. Past an
	// eighth of the live refs the listing stops and they visit every slot.
	changedRefs           []Ref[T]
	changedRefsOverflowed bool
	// keys is the map[K]Ref[T] of keyed refs, nil for plain ones.
	keys any
}

// refSlot is one ref's value, generation and state.
type refSlot[T any] struct {
	value T
	// generation is the generation of the ref last made in this slot, 0 if
	// none ever was. Generations start at 1.
	generation uint32
	live       bool
	state      refState
}

// refState is what happened to an observed ref in this observation window.
type refState uint8

const (
	refUnchanged refState = iota
	refChanged
	refAdded
	refReleased
	// refDiscarded is a ref added and released within the window, never
	// reported.
	refDiscarded
)

// windowedRefs is a world's refs of some type, as the observation reset
// reaches them.
type windowedRefs interface {
	startObservation()
	resetObservation()
}

// RegisterRefs registers world W's refs holding T, a kernel resource of
// their own, so systems on refs of different types run in parallel. It is
// called in a plugin's Init, after W is registered.
//
// It is a composition error, and panics, for W not to be registered, for
// refs of T to be registered in W twice, plain or keyed, or for the
// allocation range to be empty. Declaring refs that were never registered
// panics too.
func RegisterRefs[W WorldID, T any](configurator *kernel.Configurator, options RefsOptions) {
	registerRefs[W, T](configurator, options)
}

// Declare declares the write on W's refs of T and a read on world W. It is a
// composition error, and panics, for the refs to be keyed: KeyedRefs writes
// them.
func (Refs[W, T]) Declare(access *kernel.Access) Refs[W, T] {
	access.Read[*worldResource[W]]()
	refs := access.Write[*refsResource[W, T]]().Get()
	if refs.keys != nil {
		panic(fmt.Sprintf("ecs: Refs of keyed refs of %v in world %v: KeyedRefs writes them", reflect.TypeFor[T](), reflect.TypeFor[W]()))
	}
	return Refs[W, T]{refs: refs}
}

// New stores value in a new ref and returns its handle. It answers the zero
// Ref once the allocation range is used up.
func (r Refs[W, T]) New(value T) Ref[T] {
	return r.refs.new(value)
}

// NewAt stores value in a ref at h's index and generation, for mirrors and
// for loading a save. h's index must lie outside the allocation range and
// hold no live ref, nor one released this window.
func (r Refs[W, T]) NewAt(h Ref[T], value T) error {
	return r.refs.place(h, value)
}

// Get returns the value of ref h, or nil if h is released or zero. The
// pointer stays valid until the ref is released. Writing through it is not a
// change: Set is.
func (r Refs[W, T]) Get(h Ref[T]) *T {
	return r.refs.get(h)
}

// Alive reports whether ref h is alive.
func (r Refs[W, T]) Alive(h Ref[T]) bool {
	return r.refs.slot(h) != nil
}

// Set stores value in ref h, and reports whether h is alive. The handle
// stays, so the components holding it do not change; DeltaRefs reports the
// ref changed.
func (r Refs[W, T]) Set(h Ref[T], value T) bool {
	slot := r.refs.slot(h)
	if slot == nil {
		return false
	}
	slot.value = value
	r.refs.markChanged(h, slot)
	return true
}

// Release frees ref h, and reports whether it was alive. Every handle to it
// reads nothing at once. The ECS does not count the components holding a
// ref: whoever releases it knows none needs it, and a holder left reads
// nothing.
func (r Refs[W, T]) Release(h Ref[T]) bool {
	r.refs.settleReservations()
	return r.refs.release(h)
}

// Declare declares a read on W's refs of T and on world W.
func (RefsLookup[W, T]) Declare(access *kernel.Access) RefsLookup[W, T] {
	access.Read[*worldResource[W]]()
	return RefsLookup[W, T]{refs: access.Read[*refsResource[W, T]]().Get()}
}

// Get returns the value of ref h, or nil if h is released or zero, to read
// only: other lookups may read it at the same time.
func (l RefsLookup[W, T]) Get(h Ref[T]) *T {
	return l.refs.get(h)
}

// Alive reports whether ref h is alive.
func (l RefsLookup[W, T]) Alive(h Ref[T]) bool {
	return l.refs.slot(h) != nil
}

// Declare declares a read on W's refs of T and on world W, and observes
// them.
func (DeltaRefs[W, T]) Declare(access *kernel.Access) DeltaRefs[W, T] {
	access.Read[*worldResource[W]]()
	refs := access.Read[*refsResource[W, T]]().Get()
	if refs.observation == unobserved {
		refs.observation = observerWaiting
	}
	return DeltaRefs[W, T]{refs: refs}
}

// All walks the refs with a delta this window, each once: added, changed
// with Set or Update, or released. A ref added and released within the
// window is not yielded. Before the world's first observation reset, it
// yields nothing.
func (d DeltaRefs[W, T]) All() iter.Seq2[Ref[T], RefDelta[T]] {
	return func(yield func(Ref[T], RefDelta[T]) bool) {
		refs := d.refs
		if refs.observation != observed {
			return
		}
		for i := range refs.walkedSlots() {
			index := uint32(i)
			if !refs.changedRefsOverflowed {
				index = refs.changedRefs[i].index
			}
			slot := refs.slots.at(index)
			if slot == nil {
				continue
			}
			var delta Delta
			switch slot.state {
			case refAdded:
				delta = DeltaAdded
			case refChanged:
				delta = DeltaChanged
			case refReleased:
				delta = DeltaReleased
			default:
				continue
			}
			if !yield(Ref[T]{index: index, generation: slot.generation}, RefDelta[T]{Delta: delta, Value: &slot.value}) {
				return
			}
		}
	}
}

// registerRefs registers world W's refs holding T, as RegisterRefs and
// RegisterKeyedRefs do, and returns them.
func registerRefs[W WorldID, T any](configurator *kernel.Configurator, options RefsOptions) *refsResource[W, T] {
	w := registeredWorld[W](configurator)
	if _, ok := configurator.Resource[*refsResource[W, T]](); ok {
		panic(fmt.Sprintf("ecs: refs of %v are registered in world %v twice", reflect.TypeFor[T](), reflect.TypeFor[W]()))
	}
	last := options.LastIndex
	if last == 0 {
		last = math.MaxUint32
	}
	if last < options.FirstIndex {
		panic(fmt.Sprintf("ecs: refs of %v in world %v have an empty allocation range", reflect.TypeFor[T](), reflect.TypeFor[W]()))
	}
	refs := &refsResource[W, T]{firstIndex: options.FirstIndex, lastIndex: last, nextFreshIndex: int64(options.FirstIndex)}
	w.shrinkers = append(w.shrinkers, refs)
	w.windowedRefs = append(w.windowedRefs, refs)
	configurator.InitResource(refs)
	return refs
}

// get returns the value of ref h, or nil if h is released or zero. The value
// is a slot's first field, so the slot's address is the value's, nil
// included: converting it keeps the read path within the inlining budget.
func (r *refsResource[W, T]) get(h Ref[T]) *T {
	return (*T)(unsafe.Pointer(r.slot(h)))
}

// slot returns live ref h's slot, or nil if h is released or zero.
func (r *refsResource[W, T]) slot(h Ref[T]) *refSlot[T] {
	slot := r.slots.at(h.index)
	if slot == nil || !slot.live || slot.generation != h.generation {
		return nil
	}
	return slot
}

// new stores value in a new ref and returns its handle, or the zero Ref once
// the allocation range is used up.
func (r *refsResource[W, T]) new(value T) Ref[T] {
	h := r.reserve()
	r.settleReservations()
	if h != (Ref[T]{}) {
		r.store(h, value)
	}
	return h
}

// place stores value in a ref at h, outside the allocation range.
func (r *refsResource[W, T]) place(h Ref[T], value T) error {
	if h.generation == 0 {
		return fmt.Errorf("ecs: placing ref %d:%d, whose generation is 0", h.index, h.generation)
	}
	if h.index >= r.firstIndex && h.index <= r.lastIndex {
		return fmt.Errorf("ecs: placing ref %d:%d inside the allocation range", h.index, h.generation)
	}
	if slot := r.slots.at(h.index); slot != nil && (slot.live || slot.state != refUnchanged) {
		return fmt.Errorf("ecs: placing ref %d:%d where one is alive or was released this window", h.index, h.generation)
	}
	r.store(h, value)
	return nil
}

// reserve takes the handle of the next ref to hand out, which reads nothing
// until a value is stored in it, or the zero Ref once the allocation range
// is used up. Concurrent reserves take distinct handles; nothing else may
// change the refs meanwhile.
func (r *refsResource[W, T]) reserve() Ref[T] {
	reservation := r.reservationCount.Add(1) - 1
	freeCount := int64(r.freeRefs.Len())
	if reservation < freeCount {
		return *r.freeRefs.At(int(freeCount - 1 - reservation))
	}
	index := r.nextFreshIndex + reservation - freeCount
	if index > int64(r.lastIndex) {
		return Ref[T]{}
	}
	return Ref[T]{index: uint32(index), generation: 1}
}

// settleReservations takes the refs reserved so far off the free list, and
// moves nextFreshIndex past the fresh ones, each reading nothing until it is
// stored.
func (r *refsResource[W, T]) settleReservations() {
	reserved := int(r.reservationCount.Load())
	if reserved == 0 {
		return
	}
	r.reservationCount.Store(0)
	free := r.freeRefs.Len()
	taken := min(reserved, free)
	r.freeRefs.Resize(free - taken)
	r.nextFreshIndex = min(r.nextFreshIndex+int64(reserved-taken), int64(r.lastIndex)+1)
}

// store stores value in settled reservation or placed ref h, making it
// alive.
func (r *refsResource[W, T]) store(h Ref[T], value T) {
	slot := r.slots.atOrCreate(h.index)
	slot.value = value
	slot.generation = h.generation
	slot.live = true
	r.liveRefs++
	if r.observation == observed {
		slot.state = refAdded
		r.listChanged(h)
	}
}

// markChanged marks live ref h, whose slot is slot, changed, unless it was
// added this window. It does nothing in refs nobody observes.
func (r *refsResource[W, T]) markChanged(h Ref[T], slot *refSlot[T]) {
	if r.observation != observed {
		return
	}
	if slot.state == refUnchanged {
		slot.state = refChanged
		r.listChanged(h)
	}
}

// listChanged lists h, whose slot has just left refUnchanged, or stops the
// listing once it holds an eighth of the live refs.
func (r *refsResource[W, T]) listChanged(h Ref[T]) {
	if len(r.changedRefs) < r.liveRefs>>changedEntitiesShift {
		r.changedRefs = append(r.changedRefs, h)
	} else {
		r.changedRefsOverflowed = true
	}
}

// walkedSlots answers how many slots DeltaRefs and the reset visit: those
// of changedRefs, or every index the slots have pages for, once the listing
// stopped.
func (r *refsResource[W, T]) walkedSlots() int {
	if r.changedRefsOverflowed {
		return len(r.slots.pages) << indexPageShift
	}
	return len(r.changedRefs)
}

// release frees ref h, and reports whether it was alive. Observed refs keep
// its slot and value until the window ends. Reservations are settled.
func (r *refsResource[W, T]) release(h Ref[T]) bool {
	slot := r.slot(h)
	if slot == nil {
		return false
	}
	slot.live = false
	r.liveRefs--
	if r.observation != observed {
		r.free(h.index, slot)
		return true
	}
	switch slot.state {
	case refAdded:
		slot.state = refDiscarded
	case refUnchanged:
		slot.state = refReleased
		r.listChanged(h)
	default:
		slot.state = refReleased
	}
	return true
}

// free clears released slot at index, and puts it on the back of the free
// list at its next generation if it lies in the allocation range.
func (r *refsResource[W, T]) free(index uint32, slot *refSlot[T]) {
	var zero T
	slot.value = zero
	if index >= r.firstIndex && index <= r.lastIndex {
		r.freeRefs.Append(Ref[T]{index: index, generation: nextGeneration(slot.generation)})
	}
}

// startObservation starts observing if a DeltaRefs reads the refs, every
// live ref counting as added. It is the world's first observation reset.
func (r *refsResource[W, T]) startObservation() {
	if r.observation != observerWaiting {
		return
	}
	r.observation = observed
	// Every ref is added, past what the listing holds.
	r.changedRefsOverflowed = true
	for _, page := range r.slots.pages {
		if page == nil {
			continue
		}
		for i := range page {
			if page[i].live {
				page[i].state = refAdded
			}
		}
	}
}

// resetObservation ends the observation window: the states of the refs
// with a delta clear, and the window's released slots are freed. It runs
// under the barrier, where no reservation is being taken.
func (r *refsResource[W, T]) resetObservation() {
	if r.observation != observed {
		return
	}
	r.settleReservations()
	for i := range r.walkedSlots() {
		index := uint32(i)
		if !r.changedRefsOverflowed {
			index = r.changedRefs[i].index
		}
		slot := r.slots.at(index)
		if slot == nil || slot.state == refUnchanged {
			continue
		}
		if slot.state == refReleased || slot.state == refDiscarded {
			r.free(index, slot)
		}
		slot.state = refUnchanged
	}
	r.changedRefs = r.changedRefs[:0]
	r.changedRefsOverflowed = false
}

// shrink releases the free refs' chunks past their end, the listing's
// capacity and the slot pages holding nothing. Slots never move: a freed one
// keeps its place on the free list, at its next generation. It runs under
// the barrier, where reservations taken but not settled lie within the free
// refs or past nextFreshIndex.
func (r *refsResource[W, T]) shrink() {
	r.slots.shrink()
	r.freeRefs.Shrink()
	r.changedRefs = slices.Clip(r.changedRefs)
}
