package kernel

import "reflect"

// resource is a registered resource cell. Cells are created once during
// registration and never replaced, so handles bound to them stay valid for the
// engine lifetime regardless of plugin registration order. value is guarded at
// runtime by the scheduler's read/write locks on the resource's type.
type resource struct {
	typ         reflect.Type
	value       any
	initialized bool
	owner       PluginName
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

// Get reads the resource. It gives READ ONLY access and is valid only while
// the owning handler runs.
func (r Read[T]) Get() T { return r.cell.get[T]() }

// Write is a handle to a resource locked for write, bound during registration.
type Write[T any] struct{ cell *resource }

// Get reads the resource. It gives READ ONLY access, even on a Write handle:
// the resource is replaced with Set, never by assigning through what Get
// returns. It is valid only while the owning handler runs.
func (w Write[T]) Get() T { return w.cell.get[T]() }

// Set replaces the resource value. It is the only way a handler replaces a
// resource; Get gives read only access.
func (w Write[T]) Set(value T) {
	w.cell.value = value
	w.cell.initialized = true
}
