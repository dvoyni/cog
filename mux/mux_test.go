package mux

import (
	"fmt"
	"math"
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

// distance is a named number type of a float kind.
type distance float64

// level is a named number type of an integer kind.
type level uint8

// heading is a named number type that turns the shortest way round, by the
// interpolator turnShortestWay, which a test registers.
type heading float32

// turnShortestWay is heading's interpolator: it turns the shortest way
// round, so 350° eases to 10° through 360°, not back through 180°.
func turnShortestWay(a, b heading, t float32, ease func(a, b, t float32) float32) heading {
	turn := heading(math.Mod(float64(b-a)+540, 360)) - 180
	return heading(ease(float32(a), float32(a+turn), t))
}

// gauge holds a property of every float and integer kind, and of named
// number types.
type gauge struct {
	Int      int
	Int8     int8
	Int16    int16
	Int32    int32
	Int64    int64
	Uint     uint
	Uint8    uint8
	Uint16   uint16
	Uint32   uint32
	Uint64   uint64
	Uintptr  uintptr
	Float32  float32
	Float64  float64
	Distance distance
	Level    level
	Heading  heading
}

// squad is a fixed band of units under a banner.
type squad struct {
	Members [3]unit
	Banner  float32
}

type game struct {
	Turn    int32
	Hero    unit
	Camera  vec2
	Leader  *unit
	Escort  escort
	Plan    any
	Gauge   gauge
	Squads  [2]squad
	Terrain [2][3]float32
	// Guards holds pointers, which no path may cross.
	Guards [2]*unit
}

// newTestGame returns a game whose fields hold values no zero game does.
func newTestGame() *game {
	return &game{
		Turn:   7,
		Hero:   unit{vitality: vitality{Health: 30}, Position: vec2{X: 1, Y: 2}, Name: "ada"},
		Camera: vec2{X: 5, Y: 6},
		Squads: [2]squad{{Banner: 0.25}, {
			Members: [3]unit{2: {vitality: vitality{Health: 40}, Position: vec2{X: 3, Y: 4}}},
			Banner:  0.5,
		}},
		Terrain: [2][3]float32{{1, 2, 3}, {4, 5, 6}},
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
		got := fmt.Sprint(recover())
		if !strings.Contains(got, want) {
			t.Fatalf("panicked with %q, want one mentioning %q", got, want)
		}
	}()
	function()
}
