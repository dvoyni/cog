package ecs

import (
	"iter"
	"reflect"
	"unsafe"
)

// listMarker is how the registration walk recognises a List without knowing its
// element type. A generic cannot be instantiated from a reflect.Type, so there
// is no List[T] to compare against; there is only this, which every List has as
// its first field and nothing outside this package can embed, because the type
// is unexported.
//
// It is zero-size, so it costs the List no width: a zero-size field at the
// front of a struct leaves the field after it at offset 0.
type listMarker struct{}

var listMarkerType = reflect.TypeFor[listMarker]()

// List is variable-length data a Component may hold: a fixed-length run of T
// that yields copies and can only be written through a method.
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
// which returns a copy, or Set, which is the thing validation mode checks. See
// [Set] for what that check is and what it does not cover.
//
// Its length is fixed at construction. There is no Append, and that is the same
// decision as the rest of this design rather than an omission: growing means a
// new backing array, which is an allocation, and an allocation on the hot path
// is what requirement 1 exists to refuse. A List whose length changes is a new
// List written into the Component, which is an ordinary Component write under
// an ordinary write lock. Where the length changes every frame, the answer is
// the one it has always been — a fixed-capacity array with a live count, a
// child Entity, or a side store keyed by Entity.
//
// A List is not free. Its backing array is a heap allocation the collector
// scans, so a Component holding one leaves the noscan span a pointer-free
// Component lives in; prefer [N]T wherever the bound is small and real.
type List[T any] struct {
	_    listMarker
	data []T
}

// NewList copies its arguments into a List. It is the spelling for a literal:
// ecs.NewList(a, b, c).
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

// Set writes element i. It is legal only for a caller holding the write lock on
// the Component the List came out of — a *C field of a Query, or a Set[C]
// accessor — and calling it through a value a read yielded is the error
// validation mode exists to find.
//
// In validation mode the call checks the List's backing array against what the
// run that produced it was allowed to do, and panics naming the Component and
// the access mode. Outside validation mode the check is a compile-time constant
// false, so nothing of it survives into the binary: no branch, no table, no
// load. See the ecs_validate build tag.
//
// The check is detection and not prevention, which is a weaker guarantee than
// the rest of this package offers and is stated rather than implied. It finds
// the writes a run actually executes. It does not make the illegal write
// impossible the way the pointer-free rule made a dangling Component
// impossible, and a build without the tag has no check at all.
func (l List[T]) Set(i int, value T) {
	if validate {
		checkListWritable(unsafe.Pointer(unsafe.SliceData(l.data)))
	}
	l.data[i] = value
}

// Slice is deliberately absent. Handing back the backing array would give away
// exactly the writable alias the type exists to withhold, and no amount of
// documentation makes a []T stop being a write handle. Copy out with All, or
// hold the data in a [N]T if what you want is a slice.

// isList reports whether t is some List[T]. It recognises the marker rather
// than the name, so a user type called List is not mistaken for one and a
// rename of this one cannot silently break the walk.
func isList(t reflect.Type) bool {
	return t.Kind() == reflect.Struct &&
		t.NumField() == 2 &&
		t.Field(0).Type == listMarkerType
}

// listElem is the element type of a List, which the legality walk needs in
// order to recurse into it.
func listElem(t reflect.Type) reflect.Type { return t.Field(1).Type.Elem() }

// listSite is one List within a type, as validation mode needs to find it: the
// offset its slice header sits at, and - for a List whose element type holds
// Lists of its own - the element stride and the sites within one element.
//
// nested is what lets a List of Lists be checked. A Component row names its own
// Lists' backing arrays at fixed offsets, but the arrays an element names are
// one indirection further out, at offsets that exist only once the outer List
// has elements; so a site carries the element layout and the stamp walks the
// outer List's elements at stamp time.
type listSite struct {
	offset uintptr
	stride uintptr
	nested []listSite
}

// listSites is every List within t, found once at registration. It is what
// validation mode stamps from, and it is computed whether or not validation is
// built, because registration-time cost is irrelevant and a class that carries
// the answer is simpler than one that carries a build tag.
//
// A List's header sits at the List's own offset: the marker in front of it is
// zero-size, so the slice field starts where the List does.
func listSites(t reflect.Type) []listSite {
	return appendListSites(nil, t, 0)
}

func appendListSites(into []listSite, t reflect.Type, base uintptr) []listSite {
	if isList(t) {
		elem := listElem(t)
		return append(into, listSite{offset: base, stride: elem.Size(), nested: listSites(elem)})
	}
	switch t.Kind() {
	case reflect.Struct:
		for i := range t.NumField() {
			field := t.Field(i)
			into = appendListSites(into, field.Type, base+field.Offset)
		}
	case reflect.Array:
		stride := t.Elem().Size()
		for i := range t.Len() {
			into = appendListSites(into, t.Elem(), base+uintptr(i)*stride)
		}
	}
	return into
}
