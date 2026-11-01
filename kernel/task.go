package kernel

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
	// reads and writes are the lock masks: bit i of word i/64 is resource i.
	// They are only as long as the task's highest resource needs, and a word
	// past their end locks nothing. A resource both read and written is only
	// in writes. They include the locks of every task this one uses.
	reads, writes      []uint64
	pinnedToMainThread bool
	run                TaskBody[A, B]
}

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
