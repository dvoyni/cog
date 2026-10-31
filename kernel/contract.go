package kernel

// KernelID identifies a Kernel. Spawn assigns it from a counter the
// Supervisor increments atomically, so it is unique for the process's life
// and never reused: an ID kept past its kernel's destroy names no kernel,
// rather than whichever kernel was spawned next. The zero KernelID names no
// kernel.
type KernelID uint64

// SubscriptionFunc is a subscription's body: the event handler Subscribe
// adds. Handlers run synchronously and one by one, on the goroutine that
// published, and hold no locks: a handler that needs resources schedules a
// task on ex.
//
// in is the previous handler's out dependency, or the zero JobHandle for the
// first handler. A handler returns the event and its own out dependency: a
// job it scheduled, ex.Join of several, or in unchanged. That is how handlers
// later in the order wait for work earlier ones started.
//
// A handler may change the event by returning a changed copy, which is what
// the next handler receives; one that leaves it alone returns event as given.
// The event travels by value, never by pointer, because a pointer handed to a
// handler escapes to the heap. It is plain old data, which Subscribe checks.
type SubscriptionFunc[E any] func(kernel *Kernel, ex *Executor, in JobHandle, event E) (E, JobHandle)

// Sub is one subscription of a handler to E, returned by Subscribe. It is
// what OrderSubscriptions orders, and its type keeps that to two
// subscriptions of the same event. The zero Sub is not a subscription.
type Sub[E any] struct {
	index int32
}

// TaskFunc is a task's body. R is a struct whose every field is a Read or
// Write handle, and declaring those fields is what declares the task's locks:
//
//	type damageRes struct {
//		Health kernel.Write[Health]
//		Pos    kernel.Read[Position]
//	}
//	type DamageArg struct{ Target EntityID; Amount int32 }
//
//	func applyDamage(k *kernel.Kernel, res damageRes, arg DamageArg) Dead { ... }
//
// A is the task's arg and B its result. Both must be plain old data: no
// pointers, slices, maps, strings, interfaces, channels or funcs, at any
// depth, so the GC never scans job storage. Text travels as a hash or an ID,
// with the text itself in a resource. A task that takes nothing uses NoArg,
// one that answers nothing returns NoResult, and one that locks nothing uses
// NoResources.
//
// The body runs once per Job, on a worker, with R's locks held. It should be a
// top-level func or a closure that captures nothing: state travels in the arg,
// and a capturing closure allocates.
//
// A job always completes. A body that panics is recovered, its panic and stack
// go to the kernel's LogErr, and the job finishes with the zero B, so every job
// that depends on it, or takes its result, still runs.
//
// Jobs whose locks conflict run one at a time, in no guaranteed order.
type TaskFunc[R, A, B any] func(kernel *Kernel, res R, arg A) B

// Task is a registered TaskFunc taking A and answering B, belonging to the
// kernel it was registered in. It is only ever handled by pointer:
// RegisterTask allocates it once, and it carries the task's bound handles,
// compiled lock set and caller, so scheduling reads them straight off it.
type Task[A, B any] struct{}

// JobHandle names one scheduled job without its result type. It is what
// dependencies are declared with. Only an Executor makes one, directly or
// through Job.Handle. The zero JobHandle names a job that has already
// finished.
//
// slot is the job slot the run occupies, and gen that slot's generation when
// the run took it. A slot's generation advances every time it is reused, so a
// handle kept past its run reads as finished rather than as whichever run
// holds the slot now. gen starts at 1, which keeps the zero JobHandle from
// naming slot 0.
//
// A JobHandle means something only in the kernel that made it.
type JobHandle struct {
	slot uint32
	gen  uint32
}

// Job is one scheduled run of a task answering T. It is what
// Executor.ScheduleAfter takes, so a job can only be fed by one whose result
// has its arg type.
type Job[T any] struct {
	handle JobHandle
}

// Handle returns j without its result type, for declaring a dependency.
func (j Job[T]) Handle() JobHandle { return j.handle }

// NoArg is the arg of a task that takes none.
type NoArg = struct{}

// NoResult is the result of a task that answers nothing.
type NoResult = struct{}
