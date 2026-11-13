package mux

import (
	"reflect"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
)

// modelProbe is what a test registers against a model: an update writing the
// model's turn and reading the timeline's writing side, a view reading the
// model through a path and the timeline's reading side, and what each last
// read.
type modelProbe struct {
	muxTasks Tasks[game, depth]
	update   *kernel.Task[int32, kernel.NoResult]
	view     *kernel.Task[kernel.NoArg, kernel.NoResult]
	// updateNow and viewNow are the timeline's Now as the update and the
	// view last read it, and viewTurn the turn the view last read.
	updateNow time.Duration
	viewNow   time.Duration
	viewTurn  int32
}

// registerModelProbe registers game and the tasks of a modelProbe.
func registerModelProbe(configurator *kernel.Configurator) *modelProbe {
	probe := &modelProbe{}
	RegisterModel[game, depth](configurator)
	probe.muxTasks, _ = configurator.Resource[Tasks[game, depth]]()
	turn := MakePath[game, int32](configurator, "Turn")
	probe.update = configurator.RegisterTask2(func(_ *kernel.Kernel, newTurn int32, model kernel.Write[*game], timeline kernel.Write[Timeline[game, depth]]) kernel.NoResult {
		model.Get().Turn = newTurn
		probe.updateNow = ElevateTimeline(timeline).Now()
		return kernel.NoResult{}
	})
	probe.view = configurator.RegisterTask2(func(_ *kernel.Kernel, _ kernel.NoArg, model kernel.Read[*game], timeline kernel.Read[Timeline[game, depth]]) kernel.NoResult {
		probe.viewTurn, _ = turn.Get(model.Get())
		probe.viewNow = timeline.Get().Now()
		return kernel.NoResult{}
	})
	return probe
}

func TestRegisterModelInstallsTheModelItsTimelineAndItsTasks(t *testing.T) {
	spawn(t, func(configurator *kernel.Configurator) {
		RegisterModel[game, depth](configurator)
		model, ok := configurator.Resource[*game]()
		if !ok || model == nil || !reflect.ValueOf(*model).IsZero() {
			t.Fatalf("the model resource is %v, %v, want a pointer to the zero game", model, ok)
		}
		if _, ok := configurator.Resource[Timeline[game, depth]](); !ok {
			t.Fatal("no Timeline resource")
		}
		if tasks, ok := configurator.Resource[Tasks[game, depth]](); !ok || tasks.Step == nil {
			t.Fatalf("the Tasks resource is %v, %v, want one holding Step", tasks, ok)
		}
	})
}

func TestStepAdvancesNowByItsDeltas(t *testing.T) {
	var probe *modelProbe
	harness := spawn(t, func(configurator *kernel.Configurator) { probe = registerModelProbe(configurator) })
	harness.run(func(executor *kernel.Executor) kernel.JobHandle {
		update := executor.Schedule(probe.update, 3)
		first := executor.Schedule(probe.muxTasks.Step, time.Second, update.Handle())
		second := executor.Schedule(probe.muxTasks.Step, 250*time.Millisecond, first.Handle())
		return executor.Schedule(probe.view, kernel.NoArg{}, second.Handle()).Handle()
	})
	if probe.updateNow != 0 {
		t.Fatalf("the update read Now %v before any Step, want 0", probe.updateNow)
	}
	if probe.viewNow != 1250*time.Millisecond || probe.viewTurn != 3 {
		t.Fatalf("the view read Now %v and turn %d, want 1.25s and 3", probe.viewNow, probe.viewTurn)
	}
	harness.run(func(executor *kernel.Executor) kernel.JobHandle {
		step := executor.Schedule(probe.muxTasks.Step, 500*time.Millisecond)
		return executor.Schedule(probe.update, 4, step.Handle()).Handle()
	})
	if probe.updateNow != 1750*time.Millisecond {
		t.Fatalf("the update read Now %v, want 1.75s", probe.updateNow)
	}
}

func TestKernelsShareNothing(t *testing.T) {
	var steppedProbe *modelProbe
	var idleProbe *modelProbe
	steppedHarness := spawn(t, func(configurator *kernel.Configurator) { steppedProbe = registerModelProbe(configurator) })
	idleHarness := spawn(t, func(configurator *kernel.Configurator) { idleProbe = registerModelProbe(configurator) })
	steppedHarness.run(func(executor *kernel.Executor) kernel.JobHandle {
		update := executor.Schedule(steppedProbe.update, 9)
		return executor.Schedule(steppedProbe.muxTasks.Step, time.Second, update.Handle()).Handle()
	})
	idleHarness.run(func(executor *kernel.Executor) kernel.JobHandle {
		return executor.Schedule(idleProbe.view, kernel.NoArg{}).Handle()
	})
	if idleProbe.viewNow != 0 || idleProbe.viewTurn != 0 {
		t.Fatalf("the other kernel's view read Now %v and turn %d, want 0 and 0", idleProbe.viewNow, idleProbe.viewTurn)
	}
}

func TestRegisterModelPanicsNamingWhatIsWrong(t *testing.T) {
	gamePlugin := testPlugin{init: func(configurator *kernel.Configurator) { RegisterModel[game, depth](configurator) }}
	expectPanic(t, "model mux.game is registered before the mux plugin", func() { spawnPlugins(t, gamePlugin) })
	expectPanic(t, "model mux.game is registered before the mux plugin", func() { spawnPlugins(t, gamePlugin, NewPlugin()) })
	expectPanic(t, "model mux.game is registered twice", func() {
		spawn(t, func(configurator *kernel.Configurator) {
			RegisterModel[game, depth](configurator)
			RegisterModel[game, [12]int64](configurator)
		})
	})
	expectPanic(t, "depth [4]int32, which is not an array of int64", func() {
		spawn(t, func(configurator *kernel.Configurator) { RegisterModel[game, [4]int32](configurator) })
	})
	expectPanic(t, "depth int64, which is not an array of int64", func() {
		spawn(t, func(configurator *kernel.Configurator) { RegisterModel[game, int64](configurator) })
	})
}

func TestRegisteringAfterInitPanics(t *testing.T) {
	var keptConfigurator *kernel.Configurator
	spawn(t, func(configurator *kernel.Configurator) {
		keptConfigurator = configurator
		RegisterModel[game, depth](configurator)
	})
	expectPanic(t, "model mux.menu is registered after Init", func() { RegisterModel[menu, depth](keptConfigurator) })
	expectPanic(t, `path "Turn" of model mux.game is made after Init`, func() { MakePath[game, int32](keptConfigurator, "Turn") })
}
