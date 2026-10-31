package kernel

// runTask executes one synchronous unit of work on the calling goroutine. It is
// the funnel every blocking dispatch passes, which is a command reached through
// dispatch.
//
// A terminated engine refuses here, before the task runs, so no plugin code is
// entered on an engine whose composition never bound its handles.
//
// Before Run there is no coordinator to grant locks, so the task runs directly.
// That path is for a healthy engine that has not started yet - registration is
// single-threaded - which is why the refusal keys on termination and not on the
// absent scheduler.
func (e *Engine) runTask(t task, invocation any) error {
	// An empty lock set can never conflict with anything, so the scheduler has
	// no decision to take: a declared dispatch was granted its callee's locks at
	// composition, and a command declaring none asks for none. Going to the
	// scheduler anyway costs a lock to be told what finalisation already
	// settled. This changes no lock decision; it declines to pay for one already
	// taken.
	locks := t.locks()
	if locks.empty() {
		if e.scheduler.stopped() {
			return ErrSchedulerStopped{}
		}
		return t.run(invocation)
	}
	return e.scheduler.execute(t, invocation, locks)
}

// runPublication submits every subscriber of one publication as a job and
// returns at once. The batch goes in dependency order under one scheduler
// lock, so the subscribers' Before and After edges and the order they take each
// resource in are both settled before any of them runs. publication owns the
// jobs and completes when the last one is done.
func (e *Engine) runPublication(plan *publicationPlan, invocation any, publication *Publication) {
	publication.remaining.Store(int32(len(plan.jobs)))
	if err := e.scheduler.submitBatch(plan.jobs, invocation, publication); err != nil {
		// Nothing was submitted, so nothing will hand an outcome back: the
		// engine is ending, and there is nobody to tell.
		publication.complete()
	}
}
