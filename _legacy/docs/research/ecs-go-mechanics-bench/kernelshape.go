package bench

import "reflect"

// This file is a byte-for-byte-shaped copy of the parts of kernel/resource.go
// that matter to the question: the cell's `value` is an `any`, and `get[T]`
// recovers T with a type assertion. Copied rather than imported so the
// benchmarks can build a cell without going through registration.

type resource struct {
	typ         reflect.Type
	value       any
	initialized bool
}

func (r *resource) get[T any]() T {
	if r == nil || !r.initialized {
		var zero T
		return zero
	}
	return r.value.(T)
}

// Read is a handle to a resource locked for read, bound during registration.
type Read[T any] struct{ cell *resource }

func (r Read[T]) Get() T { return r.cell.get[T]() }

type Write[T any] struct{ cell *resource }

func (w Write[T]) Get() T { return w.cell.get[T]() }

func (w Write[T]) Set(value T) {
	w.cell.value = value
	w.cell.initialized = true
}

func newCell[T any](v T) (Read[T], Write[T]) {
	c := &resource{typ: reflect.TypeFor[T]()}
	r, w := Read[T]{cell: c}, Write[T]{cell: c}
	w.Set(v)
	return r, w
}
