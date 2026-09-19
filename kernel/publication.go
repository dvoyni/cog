package kernel

// Publication is the completion handle of one event publication. It says when
// every runnable subscriber has finished, and nothing else: a subscriber that
// failed reported the failure itself.
type Publication struct {
	done chan struct{}
}

func newPublication() *Publication {
	return &Publication{done: make(chan struct{})}
}

// complete is called once, by the goroutine running the publication.
func (p *Publication) complete() { close(p.done) }

// Wait blocks until all runnable subscribers finish. It answers nothing: a
// subscriber that failed reported it, and the publisher has no part in that.
func (p *Publication) Wait() { <-p.done }

type publicationResult struct {
	node int
	err  error
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
