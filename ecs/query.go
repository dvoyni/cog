package ecs

import (
	"fmt"
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

// driverOf returns the smallest store a walk can be driven by: every other
// term is probed by entity.
func driverOf(terms []term) *store {
	var driver *store
	for _, t := range terms {
		if t.mode != termWithout && (driver == nil || t.store.len() < driver.len()) {
			driver = t.store
		}
	}
	return driver
}

// match reports whether e satisfies every term, filling addresses with the
// address of each term's row. A term that writes an observed store marks
// a matched row changed, since it is yielded for writing.
func match(terms []term, addresses []unsafe.Pointer, e Entity) bool {
	for i, t := range terms {
		row, ok := t.store.row(e)
		if ok == (t.mode == termWithout) {
			return false
		}
		addresses[i] = unsafe.Pointer(&zeroBase)
		if ok {
			addresses[i] = t.store.address(row)
		}
	}
	for _, t := range terms {
		if t.mode == termWrite && t.store.observed {
			row, _ := t.store.row(e)
			t.store.markChanged(row)
		}
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
