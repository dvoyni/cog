package kernel

import (
	"errors"
	"runtime"
	"sync"
	"testing"
)

type tick struct{ Frame int32 }

type total struct{ Value int32 }

// adder registers a task adding its arg to the total resource and answering
// the new total.
func adder(c *Configurator) *Task[int32, int32] {
	return c.RegisterTask(func(access *Access) TaskBody[int32, int32] {
		sum := access.Write[total]()
		return func(k *Kernel, arg int32) int32 {
			value := sum.Get()
			value.Value += arg
			sum.Set(value)
			return value.Value
		}
	})
}

func TestPublishWithoutSubscribers(t *testing.T) {
	k, _ := spawnTestKernel(t, func(c *Configurator) {})
	event, out := k.Publish(tick{Frame: 3})
	if event.Frame != 3 || out != (JobHandle{}) {
		t.Fatalf("Publish answered %v, %v", event, out)
	}
}

func TestPublishSubmitsOnlyAfterTheLastHandler(t *testing.T) {
	var heldDuringHandlers bool
	k, c := spawnTestKernel(t, func(c *Configurator) {
		c.InitResource(total{})
		add := adder(c)
		c.Subscribe(func(k *Kernel, ex *Executor, in JobHandle, e tick) (tick, JobHandle) {
			return e, ex.Schedule(add, 1).Handle()
		})
		c.Subscribe(func(k *Kernel, ex *Executor, in JobHandle, e tick) (tick, JobHandle) {
			// in is the first handler's job: its hold is still on.
			heldDuringHandlers = k.jobs.jobs.At(in.slot).waiting.Load() > 0
			e.Frame++
			return e, ex.Join(in, ex.Schedule(add, 2).Handle())
		})
	})
	event, out := k.Publish(tick{Frame: 1})
	if !heldDuringHandlers {
		t.Fatal("a job was submitted before the last handler returned")
	}
	if event.Frame != 2 {
		t.Fatalf("Publish answered frame %d, want the last handler's 2", event.Frame)
	}
	waitFor(t, k, out)
	if got, _ := c.Resource[total](); got.Value != 3 {
		t.Fatalf("total is %d, want 3", got.Value)
	}
}

func TestDependencyOrdersJobs(t *testing.T) {
	var order recording
	var out JobHandle
	k, _ := spawnTestKernel(t, func(c *Configurator) {
		record := recordTask(c, &order)
		c.Subscribe(func(k *Kernel, ex *Executor, in JobHandle, e tick) (tick, JobHandle) {
			first := ex.Schedule(record, 1).Handle()
			out = ex.Schedule(record, 2, first).Handle()
			return e, out
		})
	})
	k.Publish(tick{})
	waitFor(t, k, out)
	if !order.is(1, 2) {
		t.Fatalf("ran %v, want 1 then 2", &order)
	}
}

func TestScheduleAfterFeedsResult(t *testing.T) {
	k, c := spawnTestKernel(t, func(c *Configurator) {
		c.InitResource(total{Value: 5})
		add := adder(c)
		double := c.RegisterTask(func(access *Access) TaskBody[int32, int32] {
			sum := access.Write[total]()
			return func(k *Kernel, arg int32) int32 {
				sum.Set(total{Value: arg * 2})
				return arg * 2
			}
		})
		c.Subscribe(func(k *Kernel, ex *Executor, in JobHandle, e tick) (tick, JobHandle) {
			added := ex.Schedule(add, 1) // 5 + 1 = 6
			return e, ex.ScheduleAfter(double, added).Handle()
		})
	})
	_, out := k.Publish(tick{})
	waitFor(t, k, out)
	if got, _ := c.Resource[total](); got.Value != 12 {
		t.Fatalf("total is %d, want double of 6", got.Value)
	}
}

func TestExecutorPublishNestsIntoTheBatch(t *testing.T) {
	type nested struct{ Entry int32 }
	var order recording
	var inner JobHandle
	k, _ := spawnTestKernel(t, func(c *Configurator) {
		record := recordTask(c, &order)
		c.Subscribe(func(k *Kernel, ex *Executor, in JobHandle, e nested) (nested, JobHandle) {
			inner = ex.Schedule(record, e.Entry, in).Handle()
			return e, inner
		})
		c.Subscribe(func(k *Kernel, ex *Executor, in JobHandle, e tick) (tick, JobHandle) {
			outer := ex.Schedule(record, 1).Handle()
			_, out := ex.Publish(outer, nested{Entry: 2})
			return e, out
		})
	})
	_, out := k.Publish(tick{})
	if out != inner {
		t.Fatal("Publish did not answer the nested handler's out")
	}
	waitFor(t, k, out)
	if !order.is(1, 2) {
		t.Fatalf("ran %v, want the outer job then the nested one", &order)
	}
}

func TestExecutorIsReused(t *testing.T) {
	k, _ := spawnTestKernel(t, func(c *Configurator) {})
	first := k.acquireExecutor()
	first.submit()
	if second := k.acquireExecutor(); second != first {
		t.Fatal("a submitted Executor was not reused")
	}
}

// errorLogHandler keeps every error, logged from any goroutine.
type errorLogHandler struct {
	StdLogger
	mutex  sync.Mutex
	errors []error
}

func (h *errorLogHandler) LogErr(source KernelID, err error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.errors = append(h.errors, err)
}

func (h *errorLogHandler) logged() []error {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return h.errors
}

func TestScheduleForeignTaskIsLogged(t *testing.T) {
	handler := &errorLogHandler{}
	s := NewSupervisor(handler)
	t.Cleanup(s.Quit)
	foreign := newTestConfigurator().RegisterTask(nothing)
	id, err := s.Spawn(nil, runtime.GOMAXPROCS(0), hookPlugin{init: func(c *Configurator) {
		c.Subscribe(func(k *Kernel, ex *Executor, in JobHandle, e tick) (tick, JobHandle) {
			if job := ex.Schedule(foreign, NoArg{}); job != (Job[NoResult]{}) {
				t.Error("scheduling a foreign task answered a Job")
			}
			return e, in
		})
	}})
	if err != nil {
		t.Fatal(err)
	}
	s.GetKernel(id).Publish(tick{})
	if logged := handler.logged(); len(logged) != 1 || !errors.Is(logged[0], errForeignTask) {
		t.Fatalf("logged %v, want one foreign task error", logged)
	}
}

func TestPublishAndRunAllocatesNothingOnceWarm(t *testing.T) {
	k, _ := spawnTestKernel(t, func(c *Configurator) {
		c.InitResource(total{})
		add := adder(c)
		c.Subscribe(func(k *Kernel, ex *Executor, in JobHandle, e tick) (tick, JobHandle) {
			added := ex.Schedule(add, 1)
			after := ex.ScheduleAfter(add, added)
			return e, ex.Join(added.Handle(), after.Handle())
		})
	})
	sig := NewSignal()
	cycle := func() {
		_, out := k.Publish(tick{})
		k.Notify(sig, out)
		<-sig.C()
	}
	cycle()
	if got := testing.AllocsPerRun(1000, cycle); got != 0 {
		t.Fatalf("a publication and its jobs allocated %v times", got)
	}
}
