package kernel

import (
	"context"
	"reflect"
	"sync"
)

// task represents a unit of work that can be scheduled and executed by the scheduler.
// It defines the locks required for the task and the logic to run the task.
// The locks method returns the resources that the task needs to read and write.
// If one of the tasks already holds a lock on a resource that another task needs,
// the scheduler will wait until the lock is released before executing the task.
// Lock for read allows multiple tasks to read the same resource concurrently,
// but only one task can write to a resource at a time.
type task interface {
	locks(ctx context.Context) (read, write map[reflect.Type]struct{})
	run(ctx context.Context) error
}

// lockRequest is a worker goroutine's request to the coordinator (run) for a set
// of resource locks. The coordinator signals granted once the locks are held. The
// same pointer is later sent on the release channel to free the locks (or to
// withdraw the request if it was never granted).
//
// granted is a buffered channel signalled by send rather than close, so a request
// can be recycled through lockRequests without reallocating it.
type lockRequest struct {
	read    map[reflect.Type]struct{}
	write   map[reflect.Type]struct{}
	granted chan struct{}
}

var lockRequests = sync.Pool{
	New: func() any { return &lockRequest{granted: make(chan struct{}, 1)} },
}

type scheduler struct {
	acquire chan *lockRequest
	release chan *lockRequest
	done    chan struct{}
}

func newScheduler() *scheduler {
	return &scheduler{
		acquire: make(chan *lockRequest),
		release: make(chan *lockRequest),
		done:    make(chan struct{}),
	}
}

// run is the coordinator. It owns all lock bookkeeping, so no mutexes are needed:
// every mutation of the lock tables happens in this single goroutine. It blocks
// on a select, yielding the OS thread so worker goroutines make progress even
// under GOMAXPROCS=1. It returns when ctx is canceled.
func (s *scheduler) run(ctx context.Context) error {
	defer close(s.done)

	readers := map[reflect.Type]int{}
	writers := map[reflect.Type]struct{}{}
	granted := map[*lockRequest]struct{}{}
	var pending []*lockRequest

	compatible := func(req *lockRequest) bool {
		for name := range req.read {
			if _, locked := writers[name]; locked {
				return false
			}
		}
		for name := range req.write {
			if _, locked := writers[name]; locked {
				return false
			}
			if readers[name] > 0 {
				return false
			}
		}
		return true
	}

	lock := func(req *lockRequest) {
		for name := range req.read {
			readers[name]++
		}
		for name := range req.write {
			writers[name] = struct{}{}
		}
	}

	unlock := func(req *lockRequest) {
		for name := range req.read {
			readers[name]--
			if readers[name] <= 0 {
				delete(readers, name)
			}
		}
		for name := range req.write {
			delete(writers, name)
		}
	}

	requestResources := func(req *lockRequest, resources map[reflect.Type]struct{}) {
		for resourceType := range req.read {
			resources[resourceType] = struct{}{}
		}
		for resourceType := range req.write {
			resources[resourceType] = struct{}{}
		}
	}
	overlaps := func(req *lockRequest, resources map[reflect.Type]struct{}) bool {
		for resourceType := range req.read {
			if _, blocked := resources[resourceType]; blocked {
				return true
			}
		}
		for resourceType := range req.write {
			if _, blocked := resources[resourceType]; blocked {
				return true
			}
		}
		return false
	}

	// dispatch is conflict-aware FIFO: later requests may pass an earlier blocked
	// request only when they touch none of the same resources.
	dispatch := func() {
		kept := pending[:0]
		blockedResources := map[reflect.Type]struct{}{}
		for _, req := range pending {
			if compatible(req) && !overlaps(req, blockedResources) {
				lock(req)
				granted[req] = struct{}{}
				req.granted <- struct{}{}
			} else {
				kept = append(kept, req)
				requestResources(req, blockedResources)
			}
		}
		pending = kept
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case req := <-s.acquire:
			pending = append(pending, req)
			dispatch()
		case req := <-s.release:
			if _, ok := granted[req]; ok {
				unlock(req)
				delete(granted, req)
			} else {
				// The request was canceled before it was granted: drop it.
				for i, p := range pending {
					if p == req {
						pending = append(pending[:i], pending[i+1:]...)
						break
					}
				}
			}
			// The worker released ownership by sending, so recycling is safe here and
			// only here.
			recycleLockRequest(req)
			dispatch()
		}
	}
}

// schedule fans a batch of tasks out to run asynchronously: it returns
// immediately and the tasks run in order on a background goroutine as their
// locks become available. Task errors are passed to onError; true stops the
// batch, while false continues it. Batches submitted from different goroutines
// run concurrently, subject to their locks.
func (s *scheduler) schedule[TTask task](tasks []TTask, ctx context.Context, onError func(error) bool) {
	go func() {
		for _, t := range tasks {
			if err := s.execute(t, ctx); err != nil {
				if onError == nil || onError(err) {
					return
				}
			}
		}
	}()
}

// execute acquires the task's resource locks from the coordinator, runs the task
// in the current goroutine, then releases the locks. It blocks until the task
// completes. Blocking on the grant channel yields to other goroutines, which is
// what keeps the single-threaded coordination model live.
func (s *scheduler) execute(t task, ctx context.Context) error {
	read, write := t.locks(ctx)
	req := lockRequests.Get().(*lockRequest)
	req.read, req.write = read, write

	select {
	case s.acquire <- req:
	case <-ctx.Done():
		recycleLockRequest(req)
		return ctx.Err()
	case <-s.done:
		recycleLockRequest(req)
		return ErrSchedulerStopped{}
	}

	// From here the coordinator owns req and is the only side that may recycle it.
	select {
	case <-req.granted:
	case <-ctx.Done():
		// Withdraw: the coordinator removes it if still pending, or releases the
		// locks if it was granted in the meantime.
		s.post(s.release, req)
		return ctx.Err()
	case <-s.done:
		return ErrSchedulerStopped{}
	}

	defer s.post(s.release, req)
	return t.run(ctx)
}

// recycleLockRequest returns a request to the pool. A withdrawn request may still
// carry an unread grant, so drain it before reuse or the next send would block
// the coordinator.
func recycleLockRequest(req *lockRequest) {
	select {
	case <-req.granted:
	default:
	}
	req.read, req.write = nil, nil
	lockRequests.Put(req)
}

// post sends a lock message to the coordinator, giving up if the scheduler has
// stopped so callers never block on a dead coordinator (which would leak the
// goroutine).
func (s *scheduler) post(ch chan<- *lockRequest, req *lockRequest) {
	select {
	case ch <- req:
	case <-s.done:
	}
}
