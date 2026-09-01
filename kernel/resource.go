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

// Get reads the resource. It is valid only while the owning handler runs.
func (r Read[T]) Get() T { return r.cell.get[T]() }

// Write is a handle to a resource locked for write, bound during registration.
type Write[T any] struct{ cell *resource }

// Get reads the resource. It is valid only while the owning handler runs.
func (w Write[T]) Get() T { return w.cell.get[T]() }

// Set replaces the resource value. Most resources are pointers mutated in
// place; Set is for the few that are reassigned wholesale.
func (w Write[T]) Set(value T) {
	w.cell.value = value
	w.cell.initialized = true
}

// ResourceAccess is the binder handed to a Lock. Requesting a handle both
// declares the lock and binds the handle, so the two cannot drift apart. The
// returned handles outlive the Lock call; the ResourceAccess itself does not.
type ResourceAccess struct {
	resources map[reflect.Type]*resource
	read      map[reflect.Type]struct{}
	write     map[reflect.Type]struct{}
	uses      map[reflect.Type]*usage
}

func newResourceAccess(resources map[reflect.Type]*resource) *ResourceAccess {
	return &ResourceAccess{
		resources: resources,
		read:      map[reflect.Type]struct{}{},
		write:     map[reflect.Type]struct{}{},
		uses:      map[reflect.Type]*usage{},
	}
}

// GetRead declares a read lock on T and returns its handle.
func (r ResourceAccess) GetRead[T any]() Read[T] {
	id := reflect.TypeFor[T]()
	cell := r.cell(id)
	if _, writable := r.write[id]; !writable {
		r.read[id] = struct{}{}
	}
	return Read[T]{cell: cell}
}

// GetWrite declares a write lock on T and returns its handle. A write lock also
// authorizes reads, so it supersedes any read lock on the same type.
func (r ResourceAccess) GetWrite[T any]() Write[T] {
	id := reflect.TypeFor[T]()
	cell := r.cell(id)
	delete(r.read, id)
	r.write[id] = struct{}{}
	return Write[T]{cell: cell}
}

// cell returns T's cell, creating it if this is the first mention. Registration
// is single-threaded, and the map slot is never replaced afterward.
func (r ResourceAccess) cell(id reflect.Type) *resource {
	cell := r.resources[id]
	if cell == nil {
		cell = &resource{typ: id}
		r.resources[id] = cell
	}
	return cell
}

// absorb folds other's lock set into this one, keeping a write's precedence
// over a read on the same type.
func (r *ResourceAccess) absorb(other *ResourceAccess) {
	for resourceType := range other.read {
		if _, writable := r.write[resourceType]; !writable {
			r.read[resourceType] = struct{}{}
		}
	}
	for resourceType := range other.write {
		delete(r.read, resourceType)
		r.write[resourceType] = struct{}{}
	}
}

// noLocks is the shared empty lock set used by declared nested dispatches, which
// reuse their caller's locks rather than acquiring their own. The scheduler only
// ranges over lock sets, so one immutable instance is safe to share.
var noLocks = map[reflect.Type]struct{}{}
