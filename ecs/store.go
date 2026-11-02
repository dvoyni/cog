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
// part holds each row's owner and value. Removing swaps the last row into the
// gap.
//
// It knows its component only by size, so the same code serves every
// component type; the typed handles convert a row's address. Values live in
// chunks of rowsPerChunk rows that never move, so a row's address is stable
// until it is swap-removed.
type store struct {
	component componentInfo
	// sparse maps an entity index to its dense row plus 1; 0 is no row.
	sparse pages[uint32]
	owners container.Array[Entity]
	chunks []unsafe.Pointer
	// newChunk allocates one chunk of values. It is typed, made where the
	// component's type is known.
	newChunk func() unsafe.Pointer
	// lockRead and lockWrite declare a lock on this store's kernel resource,
	// for code that knows the component only by its reflect.Type.
	lockRead, lockWrite func(access *kernel.Access)
}

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
func (s Store[W, T]) Get(e Entity) *T {
	row, ok := s.store.row(e)
	if !ok {
		return nil
	}
	return (*T)(s.store.address(row))
}

// Write returns e's T, or nil if it has none, for writing in place. The
// pointer is valid as Get's is.
func (s Store[W, T]) Write(e Entity) *T {
	return s.Get(e)
}

// Add gives e value as its T, replacing the one it has, and reports whether
// e is alive.
func (s Store[W, T]) Add(e Entity, value T) bool {
	if !s.world.alive(e) {
		return false
	}
	row, ok := s.store.row(e)
	if !ok {
		row = s.store.add(e)
		maskSet(&s.world.entities.at(e.index()).mask, s.store.component.ID)
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
	maskUnset(&s.world.entities.at(e.index()).mask, s.store.component.ID)
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

// len answers how many rows the store holds.
func (s *store) len() int {
	return s.owners.Len()
}

// row answers e's row, or false if e has no value here. It is one load and
// one compare of the whole entity, generation included.
func (s *store) row(e Entity) (int, bool) {
	slot := s.sparse.at(e.index())
	if slot == nil || *slot == 0 {
		return 0, false
	}
	row := int(*slot - 1)
	return row, *s.owners.At(row) == e
}

// address returns the address of row's value.
func (s *store) address(row int) unsafe.Pointer {
	if s.component.Size == 0 {
		return unsafe.Pointer(&zeroBase)
	}
	return unsafe.Add(s.chunks[row>>rowShift], uintptr(row&rowMask)*s.component.Size)
}

// add gives e a row, whose value is left as it was, and returns it. e must
// have none.
func (s *store) add(e Entity) int {
	row := s.owners.Append(e)
	if s.component.Size != 0 && row>>rowShift == len(s.chunks) {
		s.chunks = append(s.chunks, s.newChunk())
	}
	*s.sparse.make(e.index()) = uint32(row) + 1
	return row
}

// remove drops e's row, which must exist, moving the last row into its place.
func (s *store) remove(e Entity, row int) {
	last := s.owners.SwapRemove(row)
	if row != last {
		moved := *s.owners.At(row)
		*s.sparse.at(moved.index()) = uint32(row) + 1
		if size := s.component.Size; size != 0 {
			copy(unsafe.Slice((*byte)(s.address(row)), size), unsafe.Slice((*byte)(s.address(last)), size))
		}
	}
	*s.sparse.at(e.index()) = 0
}
