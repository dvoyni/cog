package ecs

import (
	"strings"
	"testing"

	"github.com/dvoyni/cog/kernel"
)

type game [2]uint64

type position struct{ X, Y float32 }

type velocity struct{ X, Y float32 }

type frozen struct{}

// testPlugin runs init as its Init.
type testPlugin struct {
	init func(c *kernel.Configurator)
}

func (testPlugin) Name() kernel.PluginName { return "test" }

func (testPlugin) Dependencies() []kernel.PluginName { return nil }

func (p testPlugin) Init(c *kernel.Configurator, config any) error {
	p.init(c)
	return nil
}

func (testPlugin) Start(k *kernel.Kernel) error { return nil }

func (testPlugin) Stop(k *kernel.Kernel) error { return nil }

func (testPlugin) Destroy() error { return nil }

// runEvent is what harness.run publishes: its one handler schedules
// whatever the test asks.
type runEvent struct{}

// harness is a kernel of the ECS plugin and a test plugin.
type harness struct {
	kernel   *kernel.Kernel
	schedule func(ex *kernel.Executor) kernel.JobHandle
}

// spawn spawns a kernel of the ECS plugin and a test plugin whose Init runs
// init after registering the world game.
func spawn(t testing.TB, init func(c *kernel.Configurator)) *harness {
	return spawnWorkers(t, 4, init)
}

// spawnWorkers is spawn with workers workers.
func spawnWorkers(t testing.TB, workers int, init func(c *kernel.Configurator)) *harness {
	return spawnLogged(t, workers, nil, init)
}

// spawnLogged is spawnWorkers whose supervisor logs to logHandler, or to the
// standard logger if it is nil.
func spawnLogged(t testing.TB, workers int, logHandler kernel.LogHandler, init func(c *kernel.Configurator)) *harness {
	h := &harness{}
	s := kernel.NewSupervisor(logHandler)
	t.Cleanup(s.Quit)
	id, err := s.Spawn(nil, workers, NewPlugin(), testPlugin{init: func(c *kernel.Configurator) {
		RegisterWorld[game](c, WorldOptions{LastIndex: 1<<20 - 1, Entities: 64})
		c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event runEvent) (runEvent, kernel.JobHandle) {
			return event, h.schedule(ex)
		})
		init(c)
	}})
	if err != nil {
		t.Fatal(err)
	}
	h.kernel = s.GetKernel(id)
	return h
}

// run schedules with schedule and waits for the job it returns.
func (h *harness) run(schedule func(ex *kernel.Executor) kernel.JobHandle) {
	h.schedule = schedule
	_, job := h.kernel.Publish(runEvent{})
	signal := kernel.NewSignal()
	h.kernel.Notify(signal, job)
	<-signal.C()
}

// declare returns parameter P bound in a task registered for it, for tests
// that use a handle outside any job.
func declare[P kernel.Parameter[P]](c *kernel.Configurator) P {
	var p P
	c.RegisterTask(func(access *kernel.Access) kernel.TaskBody[kernel.NoArg, kernel.NoResult] {
		p = p.Declare(access)
		return func(k *kernel.Kernel, arg kernel.NoArg) kernel.NoResult { return kernel.NoResult{} }
	})
	return p
}

func expectPanic(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		got, _ := recover().(string)
		if !strings.Contains(got, want) {
			t.Fatalf("panicked with %q, want one mentioning %q", got, want)
		}
	}()
	fn()
}
