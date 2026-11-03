package ecs

import (
	"fmt"
	"math/bits"
	"reflect"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
)

//go:generate go run ./internal/generate

// With filters a query to the entities that have a T, yielding nothing. It
// reads T's store.
type With[T any] struct{}

// Without filters a query to the entities that have no T, yielding nothing.
// It reads T's store.
type Without[T any] struct{}

// Optional is a term for a component the entity may lack: Optional[T] reads
// T's store and Optional[*T] writes it. An entity matches with or without
// one; a query of Optional and Without terms only has nothing to walk.
type Optional[T any] struct {
	// address is the address of the entity's row, nil if it has none.
	address unsafe.Pointer
	write   bool
}

// Has reports whether the entity has the component, copying nothing.
func (o Optional[T]) Has() bool { return o.address != nil }

// Get returns the component and true if the entity has one: a copy for
// Optional[T], the row's address for Optional[*T], whose row is marked
// changed when matched. Else it returns the zero T and false.
func (o Optional[T]) Get() (T, bool) {
	if o.address == nil {
		var zero T
		return zero, false
	}
	return termValue[T](o.address, o.write, false), true
}

// term is one term of a query: the store it walks or probes, and how.
type term struct {
	store *storeResource
	mode  termMode
}

type termMode uint8

// The modes up to termWith can drive a walk: an entity matching them has a
// row in their store.
const (
	termRead termMode = iota
	termWrite
	termWith
	termWithout
	termOptionalRead
	termOptionalWrite
)

// filter is a term that yields nothing: With or Without.
type filter interface {
	filter() (reflect.Type, termMode)
}

func (With[T]) filter() (reflect.Type, termMode) { return reflect.TypeFor[T](), termWith }

func (Without[T]) filter() (reflect.Type, termMode) { return reflect.TypeFor[T](), termWithout }

// optional is an Optional term, which yields itself.
type optional interface {
	optional() reflect.Type
}

func (Optional[T]) optional() reflect.Type { return reflect.TypeFor[T]() }

// declareTerm reads term type T's mode, declares its lock and returns it. It
// is a composition error, and panics, for its component not to be registered
// in w.
func declareTerm[W WorldID, T any](access *kernel.Access, w *worldResource[W]) term {
	t := reflect.TypeFor[T]()
	component, mode := t, termRead
	if f, ok := any(*new(T)).(filter); ok {
		component, mode = f.filter()
	} else if o, ok := any(*new(T)).(optional); ok {
		component, mode = o.optional(), termOptionalRead
		if component.Kind() == reflect.Pointer {
			component, mode = component.Elem(), termOptionalWrite
		}
	} else if t.Kind() == reflect.Pointer {
		component, mode = t.Elem(), termWrite
	}
	s, ok := w.storesByType[component]
	if !ok {
		panic(fmt.Sprintf("ecs: a query term %v of a component not registered in world %v", t, reflect.TypeFor[W]()))
	}
	if mode == termWrite || mode == termOptionalWrite {
		s.lockWrite(access)
	} else {
		s.lockRead(access)
	}
	return term{store: s, mode: mode}
}

// declareQuery checks that terms have one that can drive a walk, and returns
// the masks of the terms that write and of those that are optional. It is a
// composition error, and panics, for every term to be Without or Optional.
func declareQuery(terms []term) (writes, optionals uint8) {
	drives := false
	for i, t := range terms {
		if t.mode == termWrite || t.mode == termOptionalWrite {
			writes |= 1 << i
		}
		if t.mode >= termOptionalRead {
			optionals |= 1 << i
		}
		drives = drives || t.mode <= termWith
	}
	if !drives {
		panic("ecs: a query of Without and Optional terms only")
	}
	return writes, optionals
}

// queryWalk is one walk of a query's terms. It is driven by the smallest
// store a walk can be driven by, whose rows it goes through chunk by chunk;
// every other term is probed by entity. It does not hold the terms: a walk
// that did would put its query on the heap, since matches stores addresses
// read through them.
type queryWalk struct {
	// drivingTerm is the index of the term that drives the walk.
	drivingTerm int
	// marked has a bit per term whose rows are marked changed when matched:
	// the terms that write an observed store. Observation cannot start
	// during a walk, which holds the stores' locks.
	marked uint8
	// addresses holds the address of each term's row in the entity last
	// matched: a filter's is tagRow, an optional term's nil if it has none.
	addresses [8]unsafe.Pointer
}

// makeQueryWalk picks the term that drives a walk of terms, and the terms
// it marks.
func makeQueryWalk(terms []term) queryWalk {
	walk := queryWalk{drivingTerm: -1}
	for i, t := range terms {
		if t.mode <= termWith && (walk.drivingTerm < 0 || t.store.len() < terms[walk.drivingTerm].store.len()) {
			walk.drivingTerm = i
		}
		if (t.mode == termWrite || t.mode == termOptionalWrite) && t.store.observed() {
			walk.marked |= 1 << i
		}
	}
	return walk
}

// matches reports whether e, at drivingRow of the driving store, satisfies
// every other one of terms, filling addresses with the address of each one's row;
// the driving term's is the caller's to fill. Once e matches, the rows of the
// marked terms it has are marked changed, since they are yielded for
// writing.
func (w *queryWalk) matches(terms []term, drivingRow int, e Entity) bool {
	var rows [8]int // a query has at most 8 terms, one bit each in marked
	rows[w.drivingTerm] = drivingRow
	for i, t := range terms {
		if i == w.drivingTerm {
			continue
		}
		row, ok := t.store.row(e)
		switch {
		case t.mode >= termOptionalRead:
			w.addresses[i] = nil
		case ok == (t.mode == termWithout):
			return false
		default:
			w.addresses[i] = unsafe.Pointer(&tagRow)
		}
		if ok {
			rows[i] = row
			w.addresses[i] = t.store.address(row)
		}
	}
	for marked := w.marked; marked != 0; marked &= marked - 1 {
		i := bits.TrailingZeros8(marked)
		if w.addresses[i] != nil {
			terms[i].store.markChanged(rows[i])
		}
	}
	return true
}

// termValue converts a row's address to term type T: an Optional holding it
// for an optional term, the address itself for a term that writes, whose T
// is a pointer, else a copy of the value.
func termValue[T any](address unsafe.Pointer, write, optional bool) T {
	if optional {
		// Optional's layout does not depend on its type argument.
		o := Optional[struct{}]{address: address, write: write}
		return *(*T)(unsafe.Pointer(&o))
	}
	if write {
		return *(*T)(unsafe.Pointer(&address))
	}
	return *(*T)(address)
}
