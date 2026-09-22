package m

import (
	"encoding/json"
	"iter"
	"unsafe"
)

// listMarker is how a reflecting walk, such as the ECS's registration walk,
// recognises a List without knowing its element type. A generic cannot be
// instantiated from a reflect.Type, so there is no List[T] to compare against;
// there is only this, which every List has as its first field and nothing
// outside this package can embed, because the type is unexported. The walk
// reaches it as the type of field 0 of any List it instantiates itself.
//
// It is zero-size, so it costs the List no width: a zero-size field at the
// front of a struct leaves the field after it at offset 0, and the List's slice
// header sits at the List's own offset.
type listMarker struct{}

// List is variable-length data an ECS Component may hold: a fixed-length run of
// T that yields copies and can only be written through a method. It is the
// storable spelling of a []T, and it lives here, beside [Maybe], so that a
// plugin can make a type storable without importing the ECS.
//
// It exists because a bare []T cannot be a Component and never will be. A
// Component is read by copying it, and a copy of a slice header shares the
// backing array with the original — so a System holding read{C} could write
// into the Store through it, concurrently with every other reader, and no lock
// anywhere would name that write. The lock unit is the Component type, and the
// whole of what makes that sound is that a read yields a value nothing can
// write through. A string has that property built in. A slice does not.
//
// A List has it by construction instead: the backing array is unexported, the
// constructors copy into a fresh one, and the only route to an element is At,
// which returns a copy, or Set, which is the thing the ECS's validation mode
// checks. See [List.Set] for what that check is and what it does not cover.
//
// Its length is fixed at construction. There is no Append, and that is a
// decision rather than an omission: growing means a new backing array, which is
// an allocation, and an allocation on the hot path is what the ECS exists to
// refuse. A List whose length changes is a new List written into the
// Component, which is an ordinary Component write under an ordinary write lock.
// Where the length changes every frame, the answer is the one it has always
// been — a fixed-capacity array with a live count, a child Entity, or a side
// store keyed by Entity.
//
// A List is not free. Its backing array is a heap allocation the collector
// scans, so a Component holding one leaves the noscan span a pointer-free
// Component lives in; prefer [N]T wherever the bound is small and real.
//
// Its header is 32 bytes rather than a slice's 24, and every List pays the
// difference whether anything watches it or not: the extra word is the
// generation Set adds one to. See [List.Set].
type List[T any] struct {
	_    listMarker
	data []T
	// gen is how an in-place change shows in the bytes of the Component
	// holding the List. The elements live in the backing array, outside the
	// row, so without it a Set would leave the row as it was and a byte
	// compare, such as the one a Changed Hook makes, could not see the write.
	gen uint64
}

// NewList copies its arguments into a List. It is the spelling for a literal:
// m.NewList(a, b, c).
func NewList[T any](values ...T) List[T] {
	return ListOf(values)
}

// ListOf copies a slice into a List. It copies rather than adopting, and that
// is the whole point of the type: an adopted slice would still be reachable and
// writable through the caller's own header, which is the aliasing the List
// exists to close. A nil or empty slice yields the zero List, which has length
// zero and no backing array at all.
func ListOf[T any](values []T) List[T] {
	if len(values) == 0 {
		return List[T]{}
	}
	data := make([]T, len(values))
	copy(data, values)
	return List[T]{data: data}
}

// Len is the number of elements. The zero List has length zero and is a
// perfectly ordinary empty List; nothing needs to check for it.
func (l List[T]) Len() int { return len(l.data) }

// At returns a copy of element i, for the same reason a Query's read field
// yields a copy of the Component: a reference would be a write handle wearing a
// read's clothes.
func (l List[T]) At(i int) T { return l.data[i] }

// All iterates the elements by index and value, both copies.
func (l List[T]) All() iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		for i, value := range l.data {
			if !yield(i, value) {
				return
			}
		}
	}
}

// MarshalJSON writes the elements as a JSON array, [] when the List is empty
// and never null, so a Component holding a List reads as its data rather than
// as the {} its unexported fields would otherwise give. A List of Lists nests
// through this same method. There is no UnmarshalJSON: nothing decodes a List.
func (l List[T]) MarshalJSON() ([]byte, error) {
	if len(l.data) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal(l.data)
}

// Set writes element i and adds one to the List's generation, so the change
// shows in the bytes of the Component holding the List. That is why the
// receiver is a pointer: a Set through a copy would bump the copy's generation
// and leave the stored row's bytes untouched. A List reached through a *C Query
// field or Set[C].Ref is addressable, so those call sites read as they always
// did.
//
// It is legal on a fresh List not yet in any Store, and on the stored List
// reached through a *C field of a Query or Set[C].Ref. Calling it through a
// value a read yielded, a Get[C].Of copy or a Set[C].Of copy is the error the
// ECS's validation mode exists to find: a Set[C].Of copy shares the stored
// array but not the row, so its write never reaches the Component's bytes.
//
// Under -tags ecs_validate the call hands the List's backing array to the check
// the ECS installs, which panics naming the Component and the access mode. The
// check is the ECS's, and so is everything it knows; this package only calls
// it. Without the tag the call is behind a compile-time constant false, so
// nothing of it survives into the binary: no branch, no table, no load.
//
// The check is detection and not prevention, which is a weaker guarantee than
// the rest of the ECS offers and is stated rather than implied. It finds the
// writes a run actually executes. It does not make the illegal write impossible
// the way the pointer-free rule made a dangling Component impossible, and a
// build without the tag has no check at all.
func (l *List[T]) Set(i int, value T) {
	if listSetChecked {
		checkListSet(unsafe.Pointer(unsafe.SliceData(l.data)))
	}
	l.data[i] = value
	l.gen++
}

// Slice is deliberately absent. Handing back the backing array would give away
// exactly the writable alias the type exists to withhold, and no amount of
// documentation makes a []T stop being a write handle. Copy out with All, or
// hold the data in a [N]T if what you want is a slice.
