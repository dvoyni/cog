// Package kernel stands in for cog's kernel: only Read and Write, so that the
// game's tasks keep the signatures they would have. Nothing schedules them;
// main calls them in the order the kernel would run them.
package kernel

// Read is a task's read handle on a resource of type T.
type Read[T any] struct{ value *T }

// Get answers the resource.
func (r Read[T]) Get() T { return *r.value }

// Write is a task's write handle on a resource of type T.
type Write[T any] struct{ value *T }

// Get answers the resource.
func (w Write[T]) Get() T { return *w.value }

// Set replaces the resource.
func (w Write[T]) Set(value T) { *w.value = value }

// Share answers both handles on value. PROTOTYPE only: the kernel hands them
// to tasks.
func Share[T any](value *T) (Read[T], Write[T]) { return Read[T]{value}, Write[T]{value} }
