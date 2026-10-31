package kernel

import "sync/atomic"

// Publication is the completion handle of one event publication. It says when
// every runnable subscriber has finished, and nothing else: a subscriber that
// failed reported the failure itself.
//
// It is also the owner of the publication's jobs: each one hands its outcome
// back through jobDone, and the last one completes the handle.
type Publication struct {
	engine    *Engine
	remaining atomic.Int32
	done      chan struct{}
}

func newPublication(engine *Engine) *Publication {
	return &Publication{engine: engine, done: make(chan struct{})}
}

// Wait blocks until all runnable subscribers finish. It answers nothing: a
// subscriber that failed reported it, and the publisher has no part in that.
func (p *Publication) Wait() { <-p.done }

// complete is called once, by whoever finishes the publication's last job.
func (p *Publication) complete() { close(p.done) }

// jobDone is one subscriber's outcome. A skipped subscriber has nothing to say:
// the ancestor whose panic skipped it already said it.
func (p *Publication) jobDone(err error) {
	if err != nil && err != errSkipped {
		p.engine.reportError(shutdownAside(err))
	}
	if p.remaining.Add(-1) == 0 {
		p.complete()
	}
}
