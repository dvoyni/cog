package kernel

import (
	"context"
	"errors"
	"sync"
)

// Publication is the completion result of one event publication. It retains the
// context it was published with, which already bounds subscriber work.
type Publication struct {
	ctx  context.Context
	done chan struct{}
	mu   sync.Mutex
	err  error
}

func newPublication(ctx context.Context) *Publication {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Publication{ctx: ctx, done: make(chan struct{})}
}

func (p *Publication) complete(err error) {
	p.mu.Lock()
	p.err = err
	p.mu.Unlock()
	close(p.done)
}

// Wait blocks until all runnable subscribers finish, or until the publishing
// context is canceled.
func (p *Publication) Wait() error {
	select {
	case <-p.done:
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.err
	case <-p.ctx.Done():
		return p.ctx.Err()
	}
}

type publicationResult struct {
	node int
	err  error
}

// invocationContext is the context a publication hands to each subscriber. The
// concrete type is the generic eventContext for the event being published.
type invocationContext interface {
	context.Context
}

func (e *Engine) runPublication(plan *publicationPlan, ctx invocationContext, publication *Publication) {
	// One subscriber is the common case, and it has no ordering to resolve: run it
	// on this goroutine and skip the scratch slices, result channel, and fan-out.
	if len(plan.nodes) == 1 {
		err := e.runTask(plan.nodes[0].task, ctx)
		if err != nil {
			e.reportError(err)
		}
		publication.complete(err)
		return
	}

	remaining := make([]int, len(plan.nodes))
	blocked := make([]bool, len(plan.nodes))
	results := make(chan publicationResult, len(plan.nodes))
	completed := 0
	var errs []error

	launch := func(node int) {
		go func() {
			results <- publicationResult{node: node, err: e.runTask(plan.nodes[node].task, ctx)}
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
			if blocked[dependent] || ctx.Err() != nil {
				finish(dependent, true)
			} else {
				launch(dependent)
			}
		}
	}

	for completed < len(plan.nodes) {
		result := <-results
		if result.err != nil {
			errs = append(errs, result.err)
			e.reportError(result.err)
		}
		finish(result.node, result.err != nil)
	}
	publication.complete(errors.Join(errs...))
}
