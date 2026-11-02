package kernel

import (
	"errors"
	"fmt"
	"runtime/debug"

	"github.com/dvoyni/cog/kernel/internal/concurrent"
)

// TaskBody is a task's body: what its declare function returns once it has
// requested the handles the body uses. It closes over those handles, and is
// created once, at registration, so running it allocates nothing:
//
//	c.damage = configurator.RegisterTask(func(access *kernel.Access) kernel.TaskBody[DamageArg, Dead] {
//		health := access.Write[Health]()
//		pos := access.Read[Position]()
//		return func(k *kernel.Kernel, arg DamageArg) Dead { ... }
//	})
//
// A is the task's arg and B its result. Both must be pointer-free: no
// pointers, slices, maps, strings, interfaces, channels or funcs, at any
// depth, so the GC never scans job storage. Text travels as a hash or an ID,
// with the text itself in a resource. A task that takes nothing uses NoArg,
// one that answers nothing returns NoResult, and one that locks nothing
// requests no handles.
//
// The body runs once per Job, on a worker, with its locks held. State travels
// in the arg: the body keeps none of its own between runs.
//
// A job always completes. A body that panics is recovered, its panic and stack
// go to the kernel's LogErr, and the job finishes with the zero B, so every job
// that depends on it, or takes its result, still runs.
//
// Jobs whose locks conflict run one at a time, in no guaranteed order.
type TaskBody[A, B any] func(kernel *Kernel, arg A) B

// Task is a registered TaskBody taking A and answering B, belonging to the
// kernel it was registered in. It is only ever handled by pointer:
// RegisterTask allocates it once, and it carries the task's compiled lock set
// and body, so scheduling reads them straight off it.
type Task[A, B any] struct {
	kernel *Kernel
	// index is the task's place in its kernel's task table, which is what a
	// job names the task it runs by.
	index uint32
	// locks is the task's entry in its kernel's lock table, shared so that
	// the lock combiner reads it by index without knowing A or B.
	locks *taskLocks
	run   TaskBody[A, B]
	// args holds the arg of every job of the task not yet run. A is
	// pointer-free, so the GC never scans it.
	args concurrent.Pool[A]
}

// runnable is what the kernel's task table holds: every Task, whatever its
// types, and the Join task.
type runnable interface {
	// runJob runs the job in slot, then completes it.
	runJob(kernel *Kernel, slot uint32)
}

// argReceiver is every Task taking A, whatever its result: what a job
// answering A feeds its result through to the jobs scheduled after it.
type argReceiver[A any] interface {
	receiveArg(index uint32, arg A)
}

// errTaskPanicked is what a task body that panics logs.
var errTaskPanicked = errors.New("kernel: task panicked")

// runJob takes the job's arg out of the arg pool, runs the body on it, and
// completes the job with the result. A body that panics is logged, and its
// job completes with the zero B, so the jobs and Signals waiting on it still
// go on.
func (t *Task[A, B]) runJob(kernel *Kernel, slot uint32) {
	index := kernel.jobs.jobs.At(slot).arg
	arg := *t.args.At(index)
	t.args.Release(index)
	kernel.completeJob(slot, t.runRecovering(kernel, arg))
}

// receiveArg stores arg as the arg at index, for a job fed by another's
// result.
func (t *Task[A, B]) receiveArg(index uint32, arg A) { *t.args.At(index) = arg }

// joinTask is the task at index 0 of every kernel's task table: what a Join
// runs, which is nothing.
type joinTask struct{}

// runJob completes the job at once.
func (joinTask) runJob(kernel *Kernel, slot uint32) { kernel.completeJob(slot, NoResult{}) }

// NoArg is the arg of a task that takes none.
type NoArg = struct{}

// NoResult is the result of a task that answers nothing.
type NoResult = struct{}

// taskLocks is what the lock combiner needs of a task: its lock masks and
// where its jobs run.
type taskLocks struct {
	// reads and writes are the lock masks: bit i of word i/64 is resource i.
	// They are only as long as the task's highest resource needs, and a word
	// past their end locks nothing. A resource both read and written is only
	// in writes. They include the locks of every task this one uses.
	reads, writes      []uint64
	pinnedToMainThread bool
}

// locksNothing reports whether the task holds no lock, so its jobs need no
// lock combiner.
func (l *taskLocks) locksNothing() bool { return len(l.writes) == 0 }

// runRecovering runs the body on arg and answers its result, or the zero B if
// the body panics, logging the panic with the stack it panicked on.
func (t *Task[A, B]) runRecovering(kernel *Kernel, arg A) (result B) {
	defer func() {
		if recovered := recover(); recovered != nil {
			kernel.logger.LogErr(fmt.Errorf("%w: task %d: %v\n%s", errTaskPanicked, t.index, recovered, debug.Stack()))
		}
	}()
	return t.run(kernel, arg)
}
