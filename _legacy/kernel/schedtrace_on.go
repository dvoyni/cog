//go:build kernel_schedtrace

package kernel

import (
	"sync/atomic"
	_ "unsafe"
)

// lockTrace is empty on the slot: a slot outlives the job that took it, so the
// timeline is written to the job's task instead, when the task asks for one.
type lockTrace struct{}

// jobTrace is one job's timeline through the scheduler, in TSC ticks. Every
// stamp is taken under the scheduler's lock, so the gaps between them and the
// task's own stamps are exactly the handoffs the lock protocol costs.
//
// cause is the job whose finish granted this one, and zero when it was granted
// on submission: only a finish can unblock a job, so this names the holder it
// was last waiting on.
type jobTrace struct {
	id          jobID
	submittedAt int64
	grantedAt   int64
	releasedAt  int64
	cause       jobID
}

// tracedTask is a task that wants its timeline recorded.
type tracedTask interface{ jobTrace() *jobTrace }

// schedulerTrace accumulates the time spent holding the scheduler's lock.
var schedulerTrace struct {
	submits, finishes atomic.Int64
}

// cputicks reads the TSC. time.Now and runtime.nanotime both tick every 0.5ms
// on Windows, which is coarser than the whole lock protocol.
//
//go:linkname cputicks runtime.cputicks
func cputicks() int64

func traceOf(slot *jobSlot) *jobTrace {
	if traced, ok := slot.work.task.(tracedTask); ok {
		return traced.jobTrace()
	}
	return nil
}

func traceSubmitted(slot *jobSlot) {
	schedulerTrace.submits.Add(1)
	if trace := traceOf(slot); trace != nil {
		trace.id = slot.id
		trace.submittedAt = cputicks()
	}
}

func traceGranted(slot *jobSlot, cause jobID) {
	if trace := traceOf(slot); trace != nil {
		trace.grantedAt = cputicks()
		trace.cause = cause
	}
}

func traceFinishing(slot *jobSlot) {
	schedulerTrace.finishes.Add(1)
	if trace := traceOf(slot); trace != nil {
		trace.releasedAt = cputicks()
	}
}
