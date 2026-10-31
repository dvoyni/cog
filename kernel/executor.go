package kernel

// Executor collects the work batch of one publication. Kernel.Publish takes
// one from the kernel's pool, hands it to every handler in turn, submits the
// whole batch to the scheduler once the last handler returns, and puts it
// back. Nothing scheduled on it starts before that, so every job of the batch
// is still pending while handlers schedule, depend on and feed one another.
//
// An Executor is valid only during the publication it was handed for.
// Keeping one past it is a bug.
type Executor struct{}

// Schedule adds a job of task with arg to the batch. It starts once every job
// in deps has finished and task's locks are free. deps is copied, never kept.
// Scheduling a nil task, or one registered in another kernel, is logged, and
// the Job returned is the zero Job.
func (ex *Executor) Schedule[A, B any](task *Task[A, B], arg A, deps ...JobHandle) Job[B] {
	return Job[B]{}
}

// ScheduleAfter is Schedule with from's result as the arg: the job starts
// after from and every job in deps, and every job scheduled after from gets
// its own copy of that result. from must belong to this batch, since a result
// does not outlive the batch it was produced in.
func (ex *Executor) ScheduleAfter[A, B any](task *Task[A, B], from Job[A], deps ...JobHandle) Job[B] {
	return Job[B]{}
}

// Join adds a job that runs nothing and finishes once every job in deps has.
// It is how a handler hands several jobs on as one out dependency.
func (ex *Executor) Join(deps ...JobHandle) JobHandle { return JobHandle{} }

// Publish delivers event to E's handlers as Kernel.Publish does, but into this
// batch: the nested publication's jobs are submitted with the rest, so they
// can depend on and feed jobs already scheduled here. in is the first nested
// handler's in. It answers what the last handler returned.
func (ex *Executor) Publish[E any](in JobHandle, event E) (E, JobHandle) {
	return event, in
}
