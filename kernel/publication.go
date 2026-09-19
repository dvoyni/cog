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
