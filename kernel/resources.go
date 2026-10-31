package kernel

// NoResources is the resources of a task that locks none.
type NoResources = struct{}

// Read is a handle to a resource locked for read. It is a field of a task's
// resource struct, bound when the task is registered.
type Read[T any] struct{}

// Get reads the resource. It is valid only while the owning job runs.
func (r Read[T]) Get() T {
	var zero T
	return zero
}

// Write is a handle to a resource locked for write, which also authorizes
// reads. It is a field of a task's resource struct, bound when the task is
// registered.
type Write[T any] struct{}

// Get reads the resource. It is valid only while the owning job runs.
func (w Write[T]) Get() T {
	var zero T
	return zero
}

// Set replaces the resource value.
func (w Write[T]) Set(value T) {}
