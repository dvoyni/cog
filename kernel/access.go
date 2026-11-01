package kernel

import (
	"fmt"
	"reflect"
)

// Access is handed to a task's declare function while RegisterTask runs.
// Requesting a handle from it is what declares the matching lock, so binding
// and declaring cannot drift apart. It is invalid once the declare function
// returns; the handles it gave out stay valid for the kernel's life.
type Access struct {
	kernel *Kernel
	// reads and writes are the lock masks being built, as on Task.
	reads, writes []uint64
}

// Read declares a read lock on resource T and returns its handle. T must be
// initialized already, by this plugin or one given before it; it is a
// composition error otherwise, and panics.
func (a *Access) Read[T any]() Read[T] {
	r := a.resource[T]()
	word, bit := maskPosition(r.index)
	a.growMasks(word + 1)
	if a.writes[word]&bit == 0 {
		a.reads[word] |= bit
	}
	return Read[T]{value: r.value.(*T)}
}

// Write declares a write lock on resource T and returns its handle. A write
// lock also authorizes reads, so it supersedes a read lock on the same T. T
// must be initialized already; it is a composition error otherwise, and
// panics.
func (a *Access) Write[T any]() Write[T] {
	r := a.resource[T]()
	word, bit := maskPosition(r.index)
	a.growMasks(word + 1)
	a.writes[word] |= bit
	a.reads[word] &^= bit
	return Write[T]{value: r.value.(*T)}
}

// Use declares that this task calls task directly and returns the call. Every
// lock task holds is folded into this task's own, so when the call runs
// inside this task's job those locks are already held: it acquires nothing,
// allocates nothing, and answers task's result directly.
//
// The call runs on whatever goroutine runs this task's job, pinned or not:
// pinning governs where scheduled jobs run, not direct calls. task must be
// this kernel's; it is a composition error otherwise, and panics.
func (a *Access) Use[A, B any](task *Task[A, B]) func(kernel *Kernel, arg A) B {
	if a.kernel == nil {
		panic("kernel: an Access used after its declare function returned")
	}
	if task == nil || task.kernel != a.kernel {
		panic("kernel: using a task that is not this kernel's")
	}
	a.growMasks(len(task.writes))
	for i := range task.writes {
		a.writes[i] |= task.writes[i]
		a.reads[i] |= task.reads[i]
		a.reads[i] &^= a.writes[i]
	}
	return task.run
}

// resource returns resource T, panicking if it is not initialized.
func (a *Access) resource[T any]() *resource {
	if a.kernel == nil {
		panic("kernel: an Access used after its declare function returned")
	}
	t := reflect.TypeFor[T]()
	r, ok := a.kernel.resources[t]
	if !ok {
		panic(fmt.Sprintf("kernel: a task locks resource %v, which is not initialized", t))
	}
	return r
}

// growMasks lengthens the masks to at least words words, the new ones
// locking nothing.
func (a *Access) growMasks(words int) {
	for len(a.writes) < words {
		a.reads = append(a.reads, 0)
		a.writes = append(a.writes, 0)
	}
}

// maskPosition answers where resource index sits in a lock mask: the word
// holding it, and its bit within that word.
func maskPosition(index int) (word int, bit uint64) {
	return index / 64, uint64(1) << (index % 64)
}

// Read is a handle to a resource locked for read, returned by Access.Read.
type Read[T any] struct {
	value *T
}

// Get reads the resource. It is valid only while the owning job runs.
func (r Read[T]) Get() T { return *r.value }

// Write is a handle to a resource locked for write, which also authorizes
// reads, returned by Access.Write.
type Write[T any] struct {
	value *T
}

// Get reads the resource. It is valid only while the owning job runs.
func (w Write[T]) Get() T { return *w.value }

// Set replaces the resource value.
func (w Write[T]) Set(value T) { *w.value = value }
