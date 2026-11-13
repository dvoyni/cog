package mux

import (
	"strings"
	"testing"

	"github.com/dvoyni/cog/kernel"
)

type depth [4]int64

type vec2 struct{ X, Y float32 }

type vitality struct{ Health int32 }

type unit struct {
	vitality
	Position vec2
	Name     string
}

// escort reaches its vitality through a pointer, which no path may cross.
type escort struct{ *vitality }

type game struct {
	Turn   int32
	Hero   unit
	Camera vec2
	Leader *unit
	Escort escort
	Plan   any
}

// newTestGame returns a game whose fields hold values no zero game does.
func newTestGame() *game {
	return &game{
		Turn:   7,
		Hero:   unit{vitality: vitality{Health: 30}, Position: vec2{X: 1, Y: 2}, Name: "ada"},
		Camera: vec2{X: 5, Y: 6},
	}
}

type menu struct{ Opacity float32 }

// testPlugin runs init as its Init.
type testPlugin struct {
	init func(configurator *kernel.Configurator)
}

func (testPlugin) Name() kernel.PluginName { return "test" }

func (testPlugin) Dependencies() []kernel.PluginName { return nil }

func (p testPlugin) Init(configurator *kernel.Configurator, config any) error {
	p.init(configurator)
	return nil
}

func (testPlugin) Start(kernel *kernel.Kernel) error { return nil }

func (testPlugin) Stop(kernel *kernel.Kernel) error { return nil }

func (testPlugin) Destroy() error { return nil }

// runEvent is what harness.run publishes: its one handler schedules
// whatever the test asks.
type runEvent struct{}

// harness is a kernel of mux's plugin and a test plugin.
type harness struct {
	kernel   *kernel.Kernel
	schedule func(executor *kernel.Executor) kernel.JobHandle
}

// spawn spawns a kernel of mux's plugin and a test plugin whose Init runs
// init.
func spawn(t testing.TB, init func(configurator *kernel.Configurator)) *harness {
	spawned := &harness{}
	spawned.kernel = spawnPlugins(t, NewPlugin(), testPlugin{init: func(configurator *kernel.Configurator) {
		configurator.Subscribe(func(_ *kernel.Kernel, executor *kernel.Executor, _ kernel.JobHandle, event runEvent) (runEvent, kernel.JobHandle) {
			return event, spawned.schedule(executor)
		})
		init(configurator)
	}})
	return spawned
}

// run schedules with schedule and waits for the job it returns.
func (h *harness) run(schedule func(executor *kernel.Executor) kernel.JobHandle) {
	h.schedule = schedule
	_, job := h.kernel.Publish(runEvent{})
	signal := kernel.NewSignal()
	h.kernel.Notify(signal, job)
	<-signal.C()
}

// spawnPlugins spawns a kernel of plugins and answers it.
func spawnPlugins(t testing.TB, plugins ...kernel.Plugin) *kernel.Kernel {
	supervisor := kernel.NewSupervisor(nil)
	t.Cleanup(supervisor.Quit)
	id, err := supervisor.Spawn(nil, 4, plugins...)
	if err != nil {
		t.Fatal(err)
	}
	return supervisor.GetKernel(id)
}

// declare declares P through a task registered for it, as a job's handles
// are declared, and answers it.
func declare[P kernel.Parameter[P]](configurator *kernel.Configurator) P {
	var parameter P
	configurator.RegisterTask(func(access *kernel.Access) kernel.TaskBody[kernel.NoArg, kernel.NoResult] {
		parameter = parameter.Declare(access)
		return func(_ *kernel.Kernel, _ kernel.NoArg) kernel.NoResult { return kernel.NoResult{} }
	})
	return parameter
}

func expectPanic(t *testing.T, want string, function func()) {
	t.Helper()
	defer func() {
		t.Helper()
		got, _ := recover().(string)
		if !strings.Contains(got, want) {
			t.Fatalf("panicked with %q, want one mentioning %q", got, want)
		}
	}()
	function()
}
