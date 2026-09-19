package kernel

import "reflect"

// noLocks is the shared empty lock set used by declared nested dispatches, which
// reuse their caller's locks rather than acquiring their own. The scheduler only
// ranges over lock sets, so one immutable instance is safe to share.
var noLocks = map[reflect.Type]struct{}{}

// usage is what a Uses declaration returns to its dispatcher closure. command is
// filled at composition, once every plugin has registered, so a Uses declaration
// does not depend on registration order.
type usage struct {
	command *command
}

// ResourceAccess is the binder handed to a Lock. Requesting a handle both
// declares the lock and binds the handle, so the two cannot drift apart. The
// returned handles outlive the Lock call; the ResourceAccess itself does not.
type ResourceAccess struct {
	resources map[reflect.Type]*resource
	// self is the handler's own identity type, which Exclusive locks it against.
	self  reflect.Type
	read  map[reflect.Type]struct{}
	write map[reflect.Type]struct{}
	uses  map[reflect.Type]*usage
}

func newResourceAccess(resources map[reflect.Type]*resource, self reflect.Type) *ResourceAccess {
	return &ResourceAccess{
		resources: resources,
		self:      self,
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

// Exclusive declares that this handler never runs concurrently with itself. A
// second invocation waits for the first to finish rather than joining it.
//
// It excludes this handler and nothing else: the key is the handler's own
// identity type, and no two handlers share one, so declaring it costs no
// parallelism against anybody. A handler that write-locks anything it mutates
// already has this for free, because two invocations conflict on that write.
// Exclusive is for the state a lock cannot reach — mutable state in the factory
// closure — which is otherwise forbidden outright. See "The Factory Closure Is
// Shared" in kernel.instructions.md before reaching for it: a resource is
// visible to Describe, to the contention report and to an agent reading the
// architecture, and closure state is visible to none of them.
//
// It deliberately does not go through cell: the key names no resource, and
// nothing may report it as one.
func (r ResourceAccess) Exclusive() { r.write[r.self] = struct{}{} }

// Uses declares that this handler dispatches TCommand and returns the dispatcher
// to call it with. Composition folds TCommand's lock closure into this handler's
// own set, so the caller never names the callee's resources, and the dispatch
// then reuses the locks the handler already holds.
func (r ResourceAccess) Uses[
	TCommand CommandConstraint[TRequest, TResponse], TRequest any, TResponse any,
]() func(Kernel, TRequest) TResponse {
	id := reflect.TypeFor[TCommand]()
	target := r.uses[id]
	if target == nil {
		target = &usage{}
		r.uses[id] = target
	}
	return func(k Kernel, request TRequest) TResponse {
		// noLocks on both sides: composition folded the callee's closure into this
		// handler's own set, so the caller already holds everything the callee
		// needs and the nested dispatch acquires nothing.
		return dispatch[TRequest, TResponse](k.engine, target.command, noLocks, noLocks, request)
	}
}

// isResource reports whether a lock-set key names a real resource. Only a key
// Exclusive added does not, including one absorbed from a command declared in
// Uses, and every report of what a handler holds skips those.
func (r ResourceAccess) isResource(id reflect.Type) bool { return r.resources[id] != nil }

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
