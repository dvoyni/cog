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

// term is one term of a query: the store it walks or probes, and how.
type term struct {
	store *store
	mode  termMode
}

type termMode uint8

const (
	termRead termMode = iota
	termWrite
	termWith
	termWithout
)

// filter is a term that yields nothing: With or Without.
type filter interface {
	filter() (reflect.Type, termMode)
}

func (With[T]) filter() (reflect.Type, termMode) { return reflect.TypeFor[T](), termWith }

func (Without[T]) filter() (reflect.Type, termMode) { return reflect.TypeFor[T](), termWithout }

// declareTerm reads term type T's mode, declares its lock and returns it. It
// is a composition error, and panics, for its component not to be registered
// in w.
func declareTerm[W WorldID, T any](access *kernel.Access, w *world[W]) term {
	t := reflect.TypeFor[T]()
	component, mode := t, termRead
	if f, ok := any(*new(T)).(filter); ok {
		component, mode = f.filter()
	} else if t.Kind() == reflect.Pointer {
		component, mode = t.Elem(), termWrite
	}
	s, ok := w.storesByType[component]
	if !ok {
		panic(fmt.Sprintf("ecs: a query term %v of a component not registered in world %v", t, reflect.TypeFor[W]()))
	}
	if mode == termWrite {
		s.lockWrite(access)
	} else {
		s.lockRead(access)
	}
	return term{store: s, mode: mode}
}

// declareQuery checks that terms have one that can drive a walk, and returns
// the mask of the terms that write. It is a composition error, and panics,
// for every term to be Without.
func declareQuery(terms []term) uint8 {
	writes := uint8(0)
	drives := false
	for i, t := range terms {
		if t.mode == termWrite {
			writes |= 1 << i
		}
		drives = drives || t.mode != termWithout
	}
	if !drives {
		panic("ecs: a query of Without terms only")
	}
	return writes
}

// queryWalk is one walk of a query's terms. It is driven by the smallest
// store a walk can be driven by, whose rows it goes through chunk by chunk;
// every other term is probed by entity. It does not hold the terms: a walk
// that did would put its query on the heap, since match stores addresses
// read through them.
type queryWalk struct {
	// driver is the index of the term that drives the walk.
	driver int
	// marked has a bit per term whose rows are marked changed when matched:
	// the terms that write an observed store. Observation cannot start
	// during a walk, which holds the stores' locks.
	marked uint8
	// addresses holds the address of each term's row in the entity last
	// matched: a filter's is zeroBase.
	addresses [8]unsafe.Pointer
}

// makeQueryWalk picks the term that drives a walk of terms, and the terms
// it marks.
func makeQueryWalk(terms []term) queryWalk {
	walk := queryWalk{driver: -1}
	for i, t := range terms {
		if t.mode != termWithout && (walk.driver < 0 || t.store.len() < terms[walk.driver].store.len()) {
			walk.driver = i
		}
		if t.mode == termWrite && t.store.observed() {
			walk.marked |= 1 << i
		}
	}
	return walk
}

// match reports whether e, at driverRow of the driving store, satisfies
// every other one of terms, filling addresses with the address of each one's row;
// the driver's is the caller's to fill. Once e matches, the rows of the
// marked terms are marked changed, since they are yielded for writing.
func (w *queryWalk) match(terms []term, driverRow int, e Entity) bool {
	var rows [8]int // a query has at most 8 terms, one bit each in marked
	rows[w.driver] = driverRow
	for i, t := range terms {
		if i == w.driver {
			continue
		}
		row, ok := t.store.row(e)
		if ok == (t.mode == termWithout) {
			return false
		}
		w.addresses[i] = unsafe.Pointer(&zeroBase)
		if ok {
			rows[i] = row
			w.addresses[i] = t.store.address(row)
		}
	}
	for marked := w.marked; marked != 0; marked &= marked - 1 {
		i := bits.TrailingZeros8(marked)
		terms[i].store.markChanged(rows[i])
	}
	return true
}

// termValue converts a row's address to term type T: the address itself for
// a term that writes, whose T is a pointer, else a copy of the value.
func termValue[T any](address unsafe.Pointer, write bool) T {
	if write {
		return *(*T)(unsafe.Pointer(&address))
	}
	return *(*T)(address)
}
