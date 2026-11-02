package kernel

import (
	"testing"
)

type benchRequest struct{ A, B, C int64 }
type benchResponse struct{ Sum int64 }
type benchEvent struct{ Tick int64 }
type benchCounter struct{ Value int64 }

// benchCommand registers a task holding a write lock on benchCounter, the
// shape v1's BenchmarkExecuteCommand dispatched.
func benchCommand(c *Configurator) *Task[benchRequest, benchResponse] {
	return c.RegisterTask(func(access *Access) TaskBody[benchRequest, benchResponse] {
		counter := access.Write[benchCounter]()
		return func(k *Kernel, request benchRequest) benchResponse {
			counter.Set(benchCounter{Value: counter.Get().Value + 1})
			return benchResponse{Sum: request.A + request.B + request.C}
		}
	})
}

// runPublications publishes a benchEvent per iteration and waits until its out
// dependency has finished, the way code outside the kernel waits on one.
func runPublications(b *testing.B, k *Kernel) {
	sig := NewSignal()
	b.ReportAllocs()
	for i := int64(0); b.Loop(); i++ {
		_, out := k.Publish(benchEvent{Tick: i})
		k.Notify(sig, out)
		<-sig.C()
	}
}

// BenchmarkScheduleWriterJob is v1's BenchmarkExecuteCommand and
// BenchmarkPublishEventSingleSubscriber in v2's terms: one publication whose
// handler schedules one job holding a write lock, waited on from outside.
func BenchmarkScheduleWriterJob(b *testing.B) {
	k, _ := spawnTestKernel(b, func(c *Configurator) {
		c.InitResource(benchCounter{})
		command := benchCommand(c)
		c.Subscribe(func(k *Kernel, ex *Executor, in JobHandle, e benchEvent) (benchEvent, JobHandle) {
			return e, ex.Schedule(command, benchRequest{A: e.Tick, B: 1, C: 2}).Handle()
		})
	})
	runPublications(b, k)
}

// BenchmarkScheduleWriterJobOneWorker is BenchmarkScheduleWriterJob on a
// kernel with one worker, which runs its jobs one at a time.
func BenchmarkScheduleWriterJobOneWorker(b *testing.B) {
	s := NewSupervisor(nil)
	b.Cleanup(s.Quit)
	id, err := s.Spawn(nil, 1, hookPlugin{init: func(c *Configurator) {
		c.InitResource(benchCounter{})
		command := benchCommand(c)
		c.Subscribe(func(k *Kernel, ex *Executor, in JobHandle, e benchEvent) (benchEvent, JobHandle) {
			return e, ex.Schedule(command, benchRequest{A: e.Tick, B: 1, C: 2}).Handle()
		})
	}})
	if err != nil {
		b.Fatal(err)
	}
	runPublications(b, s.GetKernel(id))
}

// BenchmarkUseInlineCall is v1's BenchmarkUsesNestedDispatch: the job's task
// calls the writer task through Use, inside its own job.
func BenchmarkUseInlineCall(b *testing.B) {
	k, _ := spawnTestKernel(b, func(c *Configurator) {
		c.InitResource(benchCounter{})
		command := benchCommand(c)
		outer := c.RegisterTask(func(access *Access) TaskBody[benchRequest, benchResponse] {
			inner := access.Use(command)
			return func(k *Kernel, request benchRequest) benchResponse { return inner(k, request) }
		})
		c.Subscribe(func(k *Kernel, ex *Executor, in JobHandle, e benchEvent) (benchEvent, JobHandle) {
			return e, ex.Schedule(outer, benchRequest{A: e.Tick, B: 1, C: 2}).Handle()
		})
	})
	runPublications(b, k)
}

// BenchmarkPublishThreeOrderedHandlers is v1's
// BenchmarkPublishEventThreeSubscribers: three ordered handlers that lock
// nothing. v2 handlers run on the publisher, so there is nothing to wait for.
func BenchmarkPublishThreeOrderedHandlers(b *testing.B) {
	k, _ := spawnTestKernel(b, func(c *Configurator) {
		handler := func(k *Kernel, ex *Executor, in JobHandle, e benchEvent) (benchEvent, JobHandle) { return e, in }
		first := c.Subscribe(handler)
		second := c.Subscribe(handler)
		third := c.Subscribe(handler)
		c.OrderSubscriptions(first, second)
		c.OrderSubscriptions(second, third)
	})
	b.ReportAllocs()
	for i := int64(0); b.Loop(); i++ {
		k.Publish(benchEvent{Tick: i})
	}
}

// BenchmarkFanOut has no v1 counterpart: one publication schedules 64 jobs
// locking nothing and joins them, so they spread over every worker. Divide by
// 64 for the cost of one job.
func BenchmarkFanOut(b *testing.B) {
	const jobs = 64
	k, _ := spawnTestKernel(b, func(c *Configurator) {
		work := c.RegisterTask(func(access *Access) TaskBody[benchRequest, benchResponse] {
			return func(k *Kernel, request benchRequest) benchResponse {
				return benchResponse{Sum: request.A + request.B + request.C}
			}
		})
		c.Subscribe(func(k *Kernel, ex *Executor, in JobHandle, e benchEvent) (benchEvent, JobHandle) {
			var handles [jobs]JobHandle
			for i := range handles {
				handles[i] = ex.Schedule(work, benchRequest{A: e.Tick, B: int64(i)}).Handle()
			}
			return e, ex.Join(handles[:]...)
		})
	})
	runPublications(b, k)
}

// BenchmarkFanOutContended is BenchmarkFanOut with every job writing one
// resource, so the lock combiner runs them one at a time.
func BenchmarkFanOutContended(b *testing.B) {
	const jobs = 64
	k, _ := spawnTestKernel(b, func(c *Configurator) {
		c.InitResource(benchCounter{})
		command := benchCommand(c)
		c.Subscribe(func(k *Kernel, ex *Executor, in JobHandle, e benchEvent) (benchEvent, JobHandle) {
			var handles [jobs]JobHandle
			for i := range handles {
				handles[i] = ex.Schedule(command, benchRequest{A: e.Tick, B: int64(i)}).Handle()
			}
			return e, ex.Join(handles[:]...)
		})
	})
	runPublications(b, k)
}
