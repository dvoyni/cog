package internal

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// compose builds an engine over plugins without running it and reports every
// error composition raised.
func compose(config map[kernel.PluginName]any, plugins ...kernel.Plugin) error {
	var failure error
	kernel.New(config).
		Handler(func(err error) bool { failure = errors.Join(failure, err); return false }).
		WithPlugins(plugins...)
	return failure
}

// app is a Slot: an engine that composes it without a MainLoop does not compose.
func TestApp_RequiresAMainLoop(t *testing.T) {
	err := compose(nil, New())

	var missing kernel.ErrMissingAdapter
	if !errors.As(err, &missing) {
		t.Fatalf("composing app with no MainLoop reported %v, want ErrMissingAdapter", err)
	}
	if got := kernel.TypeName(missing.Port); got != "app.MainLoopPort" {
		t.Errorf("the Port missing its Adapter is %s, want app.MainLoopPort", got)
	}
}

// The plugin registers under the root's name, so a caller declaring app.Name
// as its dependency is declaring this plugin.
func TestApp_RegistersUnderTheRootsName(t *testing.T) {
	if name := New().Name(); name != app.Name {
		t.Errorf("app registers as %q, want %q", name, app.Name)
	}
}

// Start hands the MainLoop its Loop before the engine is ready, which is before
// any Host's Run: a MainLoop never enters its loop without one.
func TestApp_StartAttachesTheLoopBeforeTheEngineIsReady(t *testing.T) {
	mainLoop := &fakeMainLoop{}
	ctx, cancel := context.WithCancel(context.Background())
	engine := kernel.New(nil).
		Handler(func(err error) bool { t.Errorf("unexpected kernel error: %v", err); return true }).
		WithPlugins(New(), mainLoopAdapter{mainLoop})
	stopped := make(chan struct{})
	go func() { defer close(stopped); engine.Run(ctx) }()
	<-engine.Ready()
	attached := mainLoop.attached()
	cancel()
	<-stopped

	if attached == nil {
		t.Fatal("the engine was ready before app attached its Loop")
	}
}

// QuitCmd asks the MainLoop to stop its loop; nothing else in app can.
func TestApp_QuitCmdQuitsTheMainLoop(t *testing.T) {
	harness := newTickHarness(t, tickTestConfig())

	if _, err := harness.k.ExecuteCommand[app.QuitCmd](app.QuitRequest{}); err != nil {
		t.Fatalf("quit: %v", err)
	}
	if quits := harness.mainLoop.quits.Load(); quits != 1 {
		t.Errorf("the MainLoop was asked to quit %d times, want 1", quits)
	}
}

// Init and Quit bracket the platform loop with InitEvent and QuitEvent, and
// each has been delivered by the time the call returns.
func TestLoop_InitAndQuitPublishTheLifecycleEvents(t *testing.T) {
	harness := newTickHarness(t, tickTestConfig())
	lifecycle := func() []string {
		harness.observer.mu.Lock()
		defer harness.observer.mu.Unlock()
		return slices.Clone(harness.observer.lifecycle)
	}

	if err := harness.loop().Init(harness.k); err != nil {
		t.Fatalf("init: %v", err)
	}
	afterInit := lifecycle()
	harness.loop().Quit(harness.k)
	afterQuit := lifecycle()

	if !slices.Equal(afterInit, []string{"init"}) {
		t.Errorf("after Init subscribers saw %v, want [init]", afterInit)
	}
	if !slices.Equal(afterQuit, []string{"init", "quit"}) {
		t.Errorf("after Quit subscribers saw %v, want [init quit]", afterQuit)
	}
}

// A window size the MainLoop reports reaches subscribers before the call
// returns, so the MainLoop can resolve its viewport after them.
func TestLoop_WindowSizePublishesTheSize(t *testing.T) {
	harness := newTickHarness(t, tickTestConfig())

	harness.loop().WindowSize(harness.k, 800, 600)

	harness.observer.mu.Lock()
	defer harness.observer.mu.Unlock()
	want := []app.WindowSizeChangeEvent{{Width: 800, Height: 600}}
	if !slices.Equal(harness.observer.sizes, want) {
		t.Errorf("subscribers saw %v, want %v", harness.observer.sizes, want)
	}
}

// A frame publishes one tick per whole fixed step of real time, only the last
// of them marked Last, and the next render carries the fraction of a step left
// over.
func TestLoop_FramePublishesWholeStepsAndRenderCarriesTheRemainder(t *testing.T) {
	harness := newTickHarness(t, tickTestConfig().WithMaxPending(8))

	harness.frame(0.035) // three and a half steps of 10ms
	harness.loop().Render(harness.k)

	updates := harness.recorded()
	if len(updates) != 3 {
		t.Fatalf("a frame of 3.5 steps published %d ticks, want 3", len(updates))
	}
	for i, update := range updates {
		if update.Dt != 0.010 {
			t.Errorf("tick %d Dt = %v, want the fixed step 0.01", i, update.Dt)
		}
		if update.Last != (i == 2) {
			t.Errorf("tick %d Last = %v, want only the frame's final tick marked", i, update.Last)
		}
	}
	if renders := harness.rendered(); len(renders) != 1 || !almostEqual(renders[0].Alpha, 0.5) {
		t.Fatalf("renders = %+v, want one with Alpha 0.5", renders)
	}
}

// A long stall is clamped to MaxFrame, so catch-up work stays bounded.
func TestLoop_FrameClampsToMaxFrame(t *testing.T) {
	harness := newTickHarness(t, tickTestConfig().WithMaxFrame(20*time.Millisecond).WithMaxPending(8))

	harness.frame(1.0) // a huge stall, clamped to 20ms: two steps
	harness.loop().Render(harness.k)

	if got := len(harness.recorded()); got != 2 {
		t.Fatalf("a clamped stall published %d ticks, want 2", got)
	}
	if renders := harness.rendered(); !almostEqual(renders[0].Alpha, 0) {
		t.Errorf("alpha = %v, want 0", renders[0].Alpha)
	}
}

// When catch-up exceeds MaxPending the extra steps are dropped, but the time
// they stood for is still spent: the loop stays near real time rather than
// spiralling.
func TestLoop_FrameDropsStepsBeyondMaxPending(t *testing.T) {
	harness := newTickHarness(t, tickTestConfig().WithMaxPending(2))

	harness.frame(0.055) // five and a half steps, only two published
	harness.loop().Render(harness.k)
	harness.frame(0.001)

	if got := len(harness.recorded()); got != 2 {
		t.Fatalf("5.5 steps with MaxPending 2 published %d ticks, want 2 and no backlog", got)
	}
	if renders := harness.rendered(); !almostEqual(renders[0].Alpha, 0.5) {
		t.Errorf("alpha = %v, want 0.5", renders[0].Alpha)
	}
}

// Sub-step frame times accumulate across frames until a whole step is reached.
func TestLoop_FrameAccumulatesAcrossFrames(t *testing.T) {
	harness := newTickHarness(t, tickTestConfig().WithMaxPending(8))

	harness.frame(0.006)
	if got := len(harness.recorded()); got != 0 {
		t.Fatalf("0.6 of a step published %d ticks, want 0", got)
	}
	harness.frame(0.006)
	harness.loop().Render(harness.k)
	if got := len(harness.recorded()); got != 1 {
		t.Fatalf("1.2 steps published %d ticks, want 1", got)
	}
	if renders := harness.rendered(); !almostEqual(renders[0].Alpha, 0.2) {
		t.Errorf("alpha = %v, want 0.2", renders[0].Alpha)
	}
}

// The zero Config takes the documented defaults.
func TestConfig_TheZeroConfigTakesTheDefaults(t *testing.T) {
	got := withDefaults(app.Config{})
	want := app.Config{Step: time.Second / 60, MaxFrame: 250 * time.Millisecond, MaxPending: 4}
	if got != want {
		t.Errorf("withDefaults(Config{}) = %+v, want %+v", got, want)
	}
}

// The configuration is read under app.Name, and a field left zero takes its
// default: here MaxFrame's 250ms and MaxPending's 4 bound a one-second stall.
func TestConfig_AZeroFieldTakesItsDefault(t *testing.T) {
	harness := newTickHarness(t, app.Config{Step: 10 * time.Millisecond})

	harness.frame(1.0)

	updates := harness.recorded()
	if len(updates) != 4 {
		t.Fatalf("a one-second stall published %d ticks, want the default MaxPending of 4", len(updates))
	}
	if updates[0].Dt != 0.010 {
		t.Errorf("Dt = %v, want the configured 10ms step", updates[0].Dt)
	}
}

// The With* builders override only their field and return a modified copy.
func TestConfig_BuildersSetOnlyTheirField(t *testing.T) {
	base := app.Config{}
	got := base.WithStep(10 * time.Millisecond).WithMaxFrame(time.Second).WithMaxPending(2)

	want := app.Config{Step: 10 * time.Millisecond, MaxFrame: time.Second, MaxPending: 2}
	if got != want {
		t.Errorf("builders produced %+v, want %+v", got, want)
	}
	if base != (app.Config{}) {
		t.Errorf("the receiver changed to %+v", base)
	}
}

// A configuration value of the wrong type fails registration by name.
func TestConfig_AWrongTypeFailsRegistration(t *testing.T) {
	err := compose(map[kernel.PluginName]any{app.Name: 123}, New(), mainLoopAdapter{&fakeMainLoop{}})

	var invalid app.ErrInvalidConfig
	if !errors.As(err, &invalid) {
		t.Fatalf("a config of type int reported %v, want ErrInvalidConfig", err)
	}
	if invalid.Got != 123 {
		t.Errorf("ErrInvalidConfig.Got = %v, want the value handed over", invalid.Got)
	}
}
