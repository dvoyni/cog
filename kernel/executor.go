package kernel

import (
	"errors"
	"reflect"
)

// Executor collects the work batch of one publication. Kernel.Publish takes
// one the kernel kept from an earlier publication, hands it to every handler
// in turn, submits the whole batch once the last handler returns, and keeps
// it for the next. Nothing scheduled on it starts before that, so every job
// of the batch is still pending while handlers schedule, depend on and feed
// one another.
//
// An Executor is valid only during the publication it was handed for.
// Keeping one past it is a bug.
type Executor struct {
	kernel *Kernel
	// batch is the slot of every job scheduled so far, each still waiting on
	// its hold. Its capacity is kept between publications.
	batch []uint32
}

// errForeignTask is what scheduling a task that is not the kernel's logs.
var errForeignTask = errors.New("kernel: scheduling a task that is not this kernel's")

// Schedule adds a job of task with arg to the batch. It starts once every job
// in deps has finished and task's locks are free. deps is copied, never kept.
// Scheduling a nil task, or one registered in another kernel, is logged, and
// the Job returned is the zero Job.
func (ex *Executor) Schedule[A, B any](task *Task[A, B], arg A, deps ...JobHandle) Job[B] {
	if task == nil || task.kernel != ex.kernel {
		ex.kernel.logger.LogErr(errForeignTask)
		return Job[B]{}
	}
	index := task.args.Acquire()
	*task.args.At(index) = arg
	return Job[B]{handle: ex.add(task.index, index, deps)}
}

// ScheduleAfter is Schedule with from's result as the arg: the job starts
// after from and every job in deps, and every job scheduled after from gets
// its own copy of that result. from must belong to this batch, since a result
// does not outlive the batch it was produced in; from any other job, the
// zero arg is what the job runs with.
func (ex *Executor) ScheduleAfter[A, B any](task *Task[A, B], from Job[A], deps ...JobHandle) Job[B] {
	if task == nil || task.kernel != ex.kernel {
		ex.kernel.logger.LogErr(errForeignTask)
		return Job[B]{}
	}
	index := task.args.Acquire()
	var zero A
	*task.args.At(index) = zero
	handle := ex.add(task.index, index, deps)
	ex.kernel.jobs.addSuccessor(from.handle, handle.slot, true)
	return Job[B]{handle: handle}
}

// Join adds a job that runs nothing and finishes once every job in deps has.
// It is how a handler hands several jobs on as one out dependency.
func (ex *Executor) Join(deps ...JobHandle) JobHandle {
	return ex.add(0, 0, deps)
}

// Publish delivers event to E's handlers as Kernel.Publish does, but into this
// batch: the nested publication's jobs are submitted with the rest, so they
// can depend on and feed jobs already scheduled here. in is the first nested
// handler's in. It answers what the last handler returned, or event and in
// when nothing subscribes to E.
func (ex *Executor) Publish[E any](in JobHandle, event E) (E, JobHandle) {
	d, ok := ex.kernel.dispatchers[reflect.TypeFor[E]()]
	if !ok {
		return event, in
	}
	return d.(*dispatcher[E]).publish(ex.kernel, ex, in, event)
}

// add puts a new job of task, with its arg at arg, into the batch, waiting on
// every job in deps as well as its hold, and answers its handle.
func (ex *Executor) add(task, arg uint32, deps []JobHandle) JobHandle {
	slot, handle := ex.kernel.jobs.acquireJob(task, arg)
	for _, dep := range deps {
		ex.kernel.jobs.addSuccessor(dep, slot, false)
	}
	ex.batch = append(ex.batch, slot)
	return handle
}

// submit releases the hold of every job in the batch, handing on each that
// waits for nothing else, and gives the Executor back to the kernel.
func (ex *Executor) submit() {
	// Counted while the publication still is, so drain never misses them.
	ex.kernel.outstanding.Add(int64(len(ex.batch)))
	for _, slot := range ex.batch {
		if ex.kernel.jobs.jobs.At(slot).waiting.Add(-1) == 0 {
			ex.kernel.arrive(slot)
		}
	}
	ex.batch = ex.batch[:0]
	ex.kernel.executors.Push(ex)
}
