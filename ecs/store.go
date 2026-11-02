package ecs

import (
	"reflect"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/kernel/container"
)

const (
	rowShift     = 8
	rowsPerChunk = 1 << rowShift
	rowMask      = rowsPerChunk - 1
)

// zeroBase is the address of every row of a tag, whose rows have no bytes.
var zeroBase byte

// store holds every value of one component type in one world, as a sparse
// set: a sparse table maps an entity's index to its dense row, and the dense
// part holds each row's owner and value. Removing swaps the last live row
// into the gap.
//
// It knows its component only by size, so the same code serves every
// component type; the typed handles convert a row's address. Values live in
// chunks of rowsPerChunk rows that never move, so a row's address is stable
// until it is swap-removed.
//
// An observed store also keeps, per row, a shadow, the value as of the last
// observation reset, and a state. A row removed in the observation window
// moves to the tail, after the live rows, keeping its value and shadow for
// observers until the reset drops it.
type store struct {
	component componentInfo
	// rowByIndex maps an entity index to its dense row plus 1; 0 is no row.
	// A row in the tail stays linked, so a re-add finds its shadow.
	rowByIndex  indexTable[uint32]
	owners      container.Array[Entity]
	valueChunks []unsafe.Pointer
	// liveRows counts the live rows, which come first. Only an observed
	// store has rows after them: its tail.
	liveRows int
	// observerRegistered is set once a system takes Changes of this store;
	// observed, once it observes, from the world's first observation reset.
	observerRegistered bool
	observed           bool
	shadowChunks       []unsafe.Pointer
	rowStates          container.Array[rowState]
	// newChunk allocates one chunk of values. It is typed, made where the
	// component's type is known.
	newChunk func() unsafe.Pointer
	// lockRead and lockWrite declare a lock on this store's kernel resource,
	// for code that knows the component only by its reflect.Type.
	lockRead, lockWrite func(access *kernel.Access)
}

// rowState is what happened to an observed row in this observation window.
type rowState uint8

const (
	rowUnchanged rowState = iota
	rowChanged
	rowAdded
	// rowRemoved and rowDiscarded rows are in the tail. A discarded one was
	// added and removed within the window, and is never reported.
	rowRemoved
	rowDiscarded
)

// storeOf is the kernel resource behind T's store in world W. Each W and T
// make a distinct type, which is what names the resource.
type storeOf[W WorldID, T any] struct {
	store
}

func newStoreOf[W WorldID, T any](id ComponentID) *storeOf[W, T] {
	s := &storeOf[W, T]{}
	s.component = componentInfo{
		ID:    id,
		Type:  reflect.TypeFor[T](),
		Size:  unsafe.Sizeof(*new(T)),
		Align: unsafe.Alignof(*new(T)),
	}
	s.newChunk = func() unsafe.Pointer { return unsafe.Pointer(new([rowsPerChunk]T)) }
	s.lockRead = func(access *kernel.Access) { access.Read[*storeOf[W, T]]() }
	s.lockWrite = func(access *kernel.Access) { access.Write[*storeOf[W, T]]() }
	return s
}

// Store writes T's store in world W by entity. It holds the write on the
// store and a read on the world.
type Store[W WorldID, T any] struct {
	world *world[W]
	store *store
}

// Lookup reads T's store in world W by entity. It holds a read on the store
// and on the world.
type Lookup[W WorldID, T any] struct {
	store *store
}

// Declare declares the write on T's store and a read on world W.
func (Store[W, T]) Declare(access *kernel.Access) Store[W, T] {
	w := access.Read[*world[W]]().Get()
	return Store[W, T]{world: w, store: &access.Write[*storeOf[W, T]]().Get().store}
}

// Has reports whether e has a T.
func (s Store[W, T]) Has(e Entity) bool {
	_, ok := s.store.row(e)
	return ok
}

// Get returns e's T, or nil if it has none, for reading in place. The pointer
// is valid for the system call, until the next add or remove on the store.
// Writing through it goes unseen by observers: use Write.
func (s Store[W, T]) Get(e Entity) *T {
	row, ok := s.store.row(e)
	if !ok {
		return nil
	}
	return (*T)(s.store.address(row))
}

// Write returns e's T, or nil if it has none, for writing in place, and
// marks it changed for observers. The pointer is valid as Get's is.
func (s Store[W, T]) Write(e Entity) *T {
	row, ok := s.store.row(e)
	if !ok {
		return nil
	}
	s.store.markChanged(row)
	return (*T)(s.store.address(row))
}

// Add gives e value as its T, replacing the one it has, and reports whether
// e is alive.
func (s Store[W, T]) Add(e Entity, value T) bool {
	if !s.world.alive(e) {
		return false
	}
	row, ok := s.store.row(e)
	if ok {
		s.store.markChanged(row)
	} else {
		row = s.store.add(e)
		maskSet(&s.world.entitySlots.at(e.index()).componentMask, s.store.component.ID)
	}
	*(*T)(s.store.address(row)) = value
	return true
}

// Remove removes e's T, and reports whether it had one.
func (s Store[W, T]) Remove(e Entity) bool {
	row, ok := s.store.row(e)
	if !ok {
		return false
	}
	s.store.remove(e, row)
	maskUnset(&s.world.entitySlots.at(e.index()).componentMask, s.store.component.ID)
	return true
}

// Declare declares a read on T's store and on world W.
func (Lookup[W, T]) Declare(access *kernel.Access) Lookup[W, T] {
	access.Read[*world[W]]()
	return Lookup[W, T]{store: &access.Read[*storeOf[W, T]]().Get().store}
}

// Has reports whether e has a T.
func (l Lookup[W, T]) Has(e Entity) bool {
	_, ok := l.store.row(e)
	return ok
}

// Get returns a copy of e's T, or false if it has none.
func (l Lookup[W, T]) Get(e Entity) (T, bool) {
	row, ok := l.store.row(e)
	if !ok {
		var zero T
		return zero, false
	}
	return *(*T)(l.store.address(row)), true
}

// len answers how many live rows the store holds.
func (s *store) len() int {
	return s.liveRows
}

// row answers e's live row, or false if e has no value here. It is one load
// and one compare of the whole entity, generation included.
func (s *store) row(e Entity) (int, bool) {
	slot := s.rowByIndex.at(e.index())
	if slot == nil || *slot == 0 {
		return 0, false
	}
	row := int(*slot - 1)
	return row, row < s.liveRows && *s.owners.At(row) == e
}

// address returns the address of row's value.
func (s *store) address(row int) unsafe.Pointer {
	if s.component.Size == 0 {
		return unsafe.Pointer(&zeroBase)
	}
	return unsafe.Add(s.valueChunks[row>>rowShift], uintptr(row&rowMask)*s.component.Size)
}

// shadow returns the address of row's shadow. The store is observed.
func (s *store) shadow(row int) unsafe.Pointer {
	if s.component.Size == 0 {
		return unsafe.Pointer(&zeroBase)
	}
	return unsafe.Add(s.shadowChunks[row>>rowShift], uintptr(row&rowMask)*s.component.Size)
}

// add gives e a live row, whose value is left as it was, and returns it. e
// must have none. A row e left in the tail this window is brought back.
func (s *store) add(e Entity) int {
	if s.observed {
		if row, ok := s.tailRow(e); ok {
			s.swap(row, s.liveRows)
			state := s.rowStates.At(s.liveRows)
			if *state == rowDiscarded {
				*state = rowAdded
			} else {
				*state = rowChanged
			}
			s.liveRows++
			return s.liveRows - 1
		}
	}
	row := s.owners.Append(e)
	if s.component.Size != 0 && row>>rowShift == len(s.valueChunks) {
		s.valueChunks = append(s.valueChunks, s.newChunk())
		if s.observed {
			s.shadowChunks = append(s.shadowChunks, s.newChunk())
		}
	}
	*s.rowByIndex.atOrCreate(e.index()) = uint32(row) + 1
	if s.observed {
		s.rowStates.Append(rowAdded)
		s.swap(row, s.liveRows)
	}
	s.liveRows++
	return s.liveRows - 1
}

// remove takes e's live row out, moving the last live row into its place.
// An observed store keeps it in the tail; any other drops it.
func (s *store) remove(e Entity, row int) {
	last := s.liveRows - 1
	s.liveRows = last
	if s.observed {
		s.swap(row, last)
		state := s.rowStates.At(last)
		if *state == rowAdded {
			*state = rowDiscarded
		} else {
			*state = rowRemoved
		}
		return
	}
	s.owners.SwapRemove(row)
	if row != last {
		moved := *s.owners.At(row)
		*s.rowByIndex.at(moved.index()) = uint32(row) + 1
		if size := s.component.Size; size != 0 {
			copy(unsafe.Slice((*byte)(s.address(row)), size), unsafe.Slice((*byte)(s.address(last)), size))
		}
	}
	*s.rowByIndex.at(e.index()) = 0
}

// markChanged marks live row changed, unless it was added this window. It
// does nothing in a store nobody observes.
func (s *store) markChanged(row int) {
	if !s.observed {
		return
	}
	if state := s.rowStates.At(row); *state == rowUnchanged {
		*state = rowChanged
	}
}

// startObservation starts observing, every row counting as added. The store
// has no tail yet.
func (s *store) startObservation() {
	s.observed = true
	for range s.valueChunks {
		s.shadowChunks = append(s.shadowChunks, s.newChunk())
	}
	for range s.owners.Len() {
		s.rowStates.Append(rowAdded)
	}
}

// resetObservation ends the observation window: each live row's shadow
// takes its value, and the tail is dropped.
func (s *store) resetObservation() {
	if !s.observed {
		return
	}
	size := s.component.Size
	for row := range s.liveRows {
		if state := s.rowStates.At(row); *state != rowUnchanged {
			copy(unsafe.Slice((*byte)(s.shadow(row)), size), unsafe.Slice((*byte)(s.address(row)), size))
			*state = rowUnchanged
		}
	}
	for row := s.liveRows; row < s.owners.Len(); row++ {
		if slot := s.rowByIndex.at(s.owners.At(row).index()); *slot == uint32(row)+1 {
			*slot = 0
		}
	}
	s.owners.Resize(s.liveRows)
	s.rowStates.Resize(s.liveRows)
}

// tailRow answers e's row in the tail, or false if it has none.
func (s *store) tailRow(e Entity) (int, bool) {
	slot := s.rowByIndex.at(e.index())
	if slot == nil || *slot == 0 {
		return 0, false
	}
	row := int(*slot - 1)
	return row, row >= s.liveRows && row < s.owners.Len() && *s.owners.At(row) == e
}

// swap exchanges rows a and b: owners, values, shadows and states, keeping
// rowByIndex linked to them. The store is observed.
func (s *store) swap(a, b int) {
	if a == b {
		return
	}
	ownerA := *s.owners.At(a)
	ownerB := *s.owners.At(b)
	// Both are read before either is written: two owners in one store may
	// share an index, one of them in the tail.
	slotA := s.rowByIndex.at(ownerA.index())
	slotB := s.rowByIndex.at(ownerB.index())
	linkedA := *slotA == uint32(a)+1
	linkedB := *slotB == uint32(b)+1
	if linkedA {
		*slotA = uint32(b) + 1
	}
	if linkedB {
		*slotB = uint32(a) + 1
	}
	*s.owners.At(a) = ownerB
	*s.owners.At(b) = ownerA
	stateA := *s.rowStates.At(a)
	*s.rowStates.At(a) = *s.rowStates.At(b)
	*s.rowStates.At(b) = stateA
	if size := s.component.Size; size != 0 {
		swapBytes(s.address(a), s.address(b), size)
		swapBytes(s.shadow(a), s.shadow(b), size)
	}
}

// swapBytes exchanges size bytes at a and b.
func swapBytes(a, b unsafe.Pointer, size uintptr) {
	x := unsafe.Slice((*byte)(a), size)
	y := unsafe.Slice((*byte)(b), size)
	for i := range x {
		x[i], y[i] = y[i], x[i]
	}
}
