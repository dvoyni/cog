package kernel

type publicationResult struct {
	node int
	err  error
}

// runTask executes one scheduled unit of work. It is the one funnel every
// dispatch passes: commands through dispatch, events through runPublication.
//
// A terminated engine refuses here, before the task runs, so no plugin code is
// entered on an engine whose composition never bound its handles.
//
// Before Run there is no coordinator to grant locks, so the task runs directly.
// That path is for a healthy engine that has not started yet - registration is
// single-threaded - which is why the refusal keys on termination and not on the
// absent scheduler.
func (e *Engine) runTask(t task, invocation any) error {
	// An empty lock set can never conflict with anything, so the coordinator has
	// no decision to take: a declared dispatch was granted its callee's locks at
	// composition, and a command declaring none asks for none. Going to the
	// coordinator anyway costs a channel round-trip to be told what finalisation
	// already settled - 1113 ns against 0.49 ns for the call itself, which is why
	// the ECS wrote Uses off. This changes no lock decision; it declines to pay
	// for one already taken.
	read, write := t.locks()
	if len(read) == 0 && len(write) == 0 {
		if e.scheduler.stopped() {
			return ErrSchedulerStopped{}
		}
		return t.run(invocation)
	}
	return e.scheduler.execute(t, invocation, read, write)
}

func (e *Engine) runPublication(plan *publicationPlan, invocation any, publication *Publication) {
	// One subscriber is the common case, and it has no ordering to resolve: run it
	// on this goroutine and skip the scratch slices, result channel, and fan-out.
	if len(plan.nodes) == 1 {
		if err := e.runTask(plan.nodes[0].task, invocation); err != nil {
			e.reportError(shutdownAside(err))
		}
		publication.complete()
		return
	}

	remaining := make([]int, len(plan.nodes))
	blocked := make([]bool, len(plan.nodes))
	results := make(chan publicationResult, len(plan.nodes))
	completed := 0

	launch := func(node int) {
		go func() {
			results <- publicationResult{node: node, err: e.runTask(plan.nodes[node].task, invocation)}
		}()
	}
	for i, node := range plan.nodes {
		remaining[i] = node.dependsOn
		if node.dependsOn == 0 {
			launch(i)
		}
	}

	var finish func(int, bool)
	finish = func(node int, failed bool) {
		completed++
		for _, dependent := range plan.nodes[node].dependents {
			remaining[dependent]--
			blocked[dependent] = blocked[dependent] || failed
			if remaining[dependent] != 0 {
				continue
			}
			if blocked[dependent] {
				finish(dependent, true)
			} else {
				launch(dependent)
			}
		}
	}

	for completed < len(plan.nodes) {
		result := <-results
		if result.err != nil {
			e.reportError(shutdownAside(result.err))
		}
		finish(result.node, result.err != nil)
	}
	publication.complete()
}
