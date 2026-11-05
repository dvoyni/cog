package kernel

import (
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"
)

// hookPlugin is a Plugin running whichever of its hooks are set.
type hookPlugin struct {
	name    PluginName
	init    func(c *Configurator)
	receive func(config any)
	start   func(k *Kernel)
	stop    func(k *Kernel)
	destroy func()
}

func (p hookPlugin) Name() PluginName { return p.name }

func (p hookPlugin) Init(c *Configurator, config any) error {
	if p.receive != nil {
		p.receive(config)
	}
	if p.init != nil {
		p.init(c)
	}
	return nil
}

func (p hookPlugin) Start(kernel *Kernel) error {
	if p.start != nil {
		p.start(kernel)
	}
	return nil
}

func (p hookPlugin) Stop(kernel *Kernel) error {
	if p.stop != nil {
		p.stop(kernel)
	}
	return nil
}

func (p hookPlugin) Destroy() error {
	if p.destroy != nil {
		p.destroy()
	}
	return nil
}

// newPartialKernel answers a kernel holding only what its job graph and lock
// combiner need: the task table tasks, its lock table taskLocks, and a
// lockState over resources resources. Jobs they make ready wait in its ready
// queue, and nothing runs them.
func newPartialKernel(tasks []runnable, taskLocks []*taskLocks, resources int) *Kernel {
	k := &Kernel{tasks: tasks, taskLocks: taskLocks}
	k.graph.kernel = k
	k.combiner.kernel = k
	k.runner.kernel = k
	k.lifecycle.kernel = k
	k.combiner.locks = makeLockState(resources)
	return k
}

// spawnTestKernel spawns a kernel whose one plugin registers with register,
// and answers the kernel and the Configurator register was given. The
// kernel is destroyed as the test ends.
func spawnTestKernel(t testing.TB, register func(c *Configurator)) (*Kernel, *Configurator) {
	var configurator *Configurator
	s := NewSupervisor(nil)
	t.Cleanup(s.Quit)
	id, err := s.Spawn(nil, runtime.GOMAXPROCS(0), hookPlugin{init: func(c *Configurator) {
		configurator = c
		register(c)
	}})
	if err != nil {
		t.Fatal(err)
	}
	return s.GetKernel(id), configurator
}

// waitFor blocks until job has finished, failing t if it takes seconds.
func waitFor(t *testing.T, k *Kernel, job JobHandle) {
	t.Helper()
	sig := NewSignal()
	k.Notify(sig, job)
	select {
	case <-sig.C():
	case <-time.After(5 * time.Second):
		t.Fatal("the job never finished")
	}
}

// recording is the order jobs ran in, appended to from any worker.
type recording struct {
	mutex   sync.Mutex
	entries []int32
}

func (r *recording) record(entry int32) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.entries = append(r.entries, entry)
}

func (r *recording) is(want ...int32) bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return slices.Equal(r.entries, want)
}

func (r *recording) String() string {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return fmt.Sprint(r.entries)
}

// recordTask registers a task locking nothing that records its arg into r.
func recordTask(c *Configurator, r *recording) *Task[int32, NoResult] {
	return c.RegisterTask(func(access *Access) TaskBody[int32, NoResult] {
		return func(k *Kernel, arg int32) NoResult {
			r.record(arg)
			return NoResult{}
		}
	})
}

func TestPanickingJobIsLoggedAndStillFinishes(t *testing.T) {
	handler := &errorLogHandler{}
	s := NewSupervisor(handler)
	t.Cleanup(s.Quit)
	var order recording
	var out JobHandle
	id, err := s.Spawn(nil, runtime.GOMAXPROCS(0), hookPlugin{init: func(c *Configurator) {
		panicking := c.RegisterTask(func(access *Access) TaskBody[NoArg, int32] {
			return func(k *Kernel, arg NoArg) int32 { panic("body failed") }
		})
		record := recordTask(c, &order)
		c.Subscribe(func(k *Kernel, ex *Executor, in JobHandle, e tick) (tick, JobHandle) {
			out = ex.ScheduleAfter(record, ex.Schedule(panicking, NoArg{})).Handle()
			return e, out
		})
	}})
	if err != nil {
		t.Fatal(err)
	}
	k := s.GetKernel(id)
	k.Publish(tick{})
	waitFor(t, k, out)
	if !order.is(0) {
		t.Fatalf("recorded %v, want the zero result fed on", &order)
	}
	if logged := handler.logged(); len(logged) != 1 || !errors.Is(logged[0], errTaskPanicked) {
		t.Fatalf("logged %v, want one panic", logged)
	}
}

// spawnHooked spawns a kernel of plugin on s, failing t if it cannot.
func spawnHooked(t *testing.T, s *Supervisor, plugin hookPlugin) *Kernel {
	t.Helper()
	id, err := s.Spawn(nil, runtime.GOMAXPROCS(0), plugin)
	if err != nil {
		t.Fatal(err)
	}
	return s.GetKernel(id)
}

// awaitClosed blocks until done is closed, failing t if it takes seconds.
func awaitClosed(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s never happened", what)
	}
}

func TestDestroyWaitsForTheJobsStopSchedules(t *testing.T) {
	s := NewSupervisor(nil)
	t.Cleanup(s.Quit)
	var order recording
	var ranBeforeDestroy bool
	destroyed := make(chan struct{})
	k := spawnHooked(t, s, hookPlugin{
		init: func(c *Configurator) {
			// Pinned, so no worker can run it: only drain keeps Destroy
			// waiting until the main thread has.
			record := recordTask(c, &order)
			c.PinToMainThread(record)
			c.Subscribe(func(k *Kernel, ex *Executor, in JobHandle, e tick) (tick, JobHandle) {
				return e, ex.Schedule(record, e.Frame).Handle()
			})
		},
		stop: func(k *Kernel) { k.Publish(tick{Frame: 1}) },
		destroy: func() {
			ranBeforeDestroy = order.is(1)
			close(destroyed)
		},
	})
	s.Destroy(k.ID())
	select {
	case <-destroyed:
		t.Fatal("Destroy ran before the pinned job Stop scheduled")
	case <-time.After(50 * time.Millisecond):
	}
	s.TickMainThread()
	awaitClosed(t, destroyed, "the destroy")
	if !ranBeforeDestroy {
		t.Fatalf("ran %v before Destroy, want the job Stop scheduled", &order)
	}
}

func TestPublishAfterDestroyPublishesNothing(t *testing.T) {
	s := NewSupervisor(nil)
	t.Cleanup(s.Quit)
	var handled bool
	destroyed := make(chan struct{})
	k := spawnHooked(t, s, hookPlugin{
		init: func(c *Configurator) {
			c.Subscribe(func(k *Kernel, ex *Executor, in JobHandle, e tick) (tick, JobHandle) {
				handled = true
				return e, ex.Join()
			})
		},
		destroy: func() { close(destroyed) },
	})
	s.Destroy(k.ID())
	awaitClosed(t, destroyed, "the destroy")
	if event, out := k.Publish(tick{Frame: 4}); handled || event.Frame != 4 || out != (JobHandle{}) {
		t.Fatalf("Publish after destroy answered %v, %v, handled %v", event, out, handled)
	}
}

func TestPinnedJobRunsOnTickMainThread(t *testing.T) {
	var order recording
	var out JobHandle
	k, _ := spawnTestKernel(t, func(c *Configurator) {
		record := recordTask(c, &order)
		c.PinToMainThread(record)
		c.Subscribe(func(k *Kernel, ex *Executor, in JobHandle, e tick) (tick, JobHandle) {
			out = ex.Schedule(record, 7).Handle()
			return e, out
		})
	})
	k.Publish(tick{})
	if order.is(7) {
		t.Fatal("the pinned job ran before the main thread ticked")
	}
	k.supervisor.TickMainThread()
	if !order.is(7) {
		t.Fatalf("ran %v, want the pinned job on the tick", &order)
	}
	waitFor(t, k, out)
}
