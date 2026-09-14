package internal

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// fakeMainLoop is the MainLoop these tests compose app with: it keeps the Loop app
// attaches, the way a platform MainLoop does, and counts the quits it is asked
// for. A test stands where the platform loop stands and calls that Loop
// on the goroutine it chooses.
type fakeMainLoop struct {
	loop  atomic.Pointer[app.Loop]
	quits atomic.Int32
}

func (d *fakeMainLoop) Attach(loop app.Loop) { d.loop.Store(&loop) }
func (d *fakeMainLoop) Quit()                { d.quits.Add(1) }

// attached is the Loop app handed over, or nil when it handed none.
func (d *fakeMainLoop) attached() app.Loop {
	if loop := d.loop.Load(); loop != nil {
		return *loop
	}
	return nil
}

// mainLoopAdapter provides a fakeMainLoop as app's MainLoop, the way a platform
// plugin provides its own: app is a Slot, and a composition without one fails.
type mainLoopAdapter struct{ mainLoop *fakeMainLoop }

// testAppMainLoop is the Adapter this fixture fills app's MainLoop Port as.
type testAppMainLoop kernel.Adapter[app.MainLoopPort]

func (mainLoopAdapter) Name() kernel.PluginName           { return "apptestmainloop" }
func (mainLoopAdapter) Dependencies() []kernel.PluginName { return nil }

func (a mainLoopAdapter) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[testAppMainLoop](app.MainLoop(a.mainLoop))
	return nil
}

// The subscriptions through which the observer sees every event app publishes.
type (
	observeUpdate     kernel.Subscription[app.UpdateEvent]
	observeRender     kernel.Subscription[app.RenderEvent]
	observeInit       kernel.Subscription[app.InitEvent]
	observeQuit       kernel.Subscription[app.QuitEvent]
	observeWindowSize kernel.Subscription[app.WindowSizeChangeEvent]
)

// observer records, in order, every app event it is delivered, so a test reads
// what subscribers saw rather than what app meant to publish.
type observer struct {
	mu      sync.Mutex
	updates []app.UpdateEvent
	renders []app.RenderEvent
	sizes   []app.WindowSizeChangeEvent
	// lifecycle is the order InitEvent and QuitEvent arrived in.
	lifecycle []string
}

func (*observer) Name() kernel.PluginName           { return "apptestobserver" }
func (*observer) Dependencies() []kernel.PluginName { return []kernel.PluginName{app.Name} }

func (o *observer) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[observeUpdate](func() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
		return nil, func(_ kernel.Kernel, event app.UpdateEvent) error {
			o.mu.Lock()
			defer o.mu.Unlock()
			o.updates = append(o.updates, event)
			return nil
		}
	})
	registrar.Subscribe[observeRender](func() (kernel.Lock, kernel.Observe[app.RenderEvent]) {
		return nil, func(_ kernel.Kernel, event app.RenderEvent) error {
			o.mu.Lock()
			defer o.mu.Unlock()
			o.renders = append(o.renders, event)
			return nil
		}
	})
	registrar.Subscribe[observeInit](func() (kernel.Lock, kernel.Observe[app.InitEvent]) {
		return nil, func(kernel.Kernel, app.InitEvent) error {
			o.mu.Lock()
			defer o.mu.Unlock()
			o.lifecycle = append(o.lifecycle, "init")
			return nil
		}
	})
	registrar.Subscribe[observeQuit](func() (kernel.Lock, kernel.Observe[app.QuitEvent]) {
		return nil, func(kernel.Kernel, app.QuitEvent) error {
			o.mu.Lock()
			defer o.mu.Unlock()
			o.lifecycle = append(o.lifecycle, "quit")
			return nil
		}
	})
	registrar.Subscribe[observeWindowSize](func() (kernel.Lock, kernel.Observe[app.WindowSizeChangeEvent]) {
		return nil, func(_ kernel.Kernel, event app.WindowSizeChangeEvent) error {
			o.mu.Lock()
			defer o.mu.Unlock()
			o.sizes = append(o.sizes, event)
			return nil
		}
	})
	return nil
}

// tickHarness is app composed with a fakeMainLoop and an observer, running in a
// real engine.
type tickHarness struct {
	t        *testing.T
	plugin   *plugin
	mainLoop *fakeMainLoop
	observer *observer
	k        kernel.Executioner
}

func newTickHarness(t *testing.T, config app.Config) *tickHarness {
	t.Helper()
	harness := &tickHarness{t: t, plugin: New().(*plugin), mainLoop: &fakeMainLoop{}, observer: &observer{}}
	ctx, cancel := context.WithCancel(context.Background())
	engine := kernel.New(map[kernel.PluginName]any{app.Name: config}).
		Handler(func(err error) bool { t.Errorf("unexpected kernel error: %v", err); return true }).
		WithPlugins(harness.plugin, mainLoopAdapter{harness.mainLoop}, harness.observer)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		engine.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Error("engine did not shut down")
		}
	})
	<-engine.Ready()
	harness.k = engine.Executioner()
	if harness.mainLoop.attached() == nil {
		t.Fatal("app started without attaching its Loop to the MainLoop")
	}
	return harness
}

// loop is the Loop app attached to the MainLoop.
func (h *tickHarness) loop() app.Loop { return h.mainLoop.attached() }

// frame runs one frame of dt real seconds on this goroutine, the way a
// platform loop does.
func (h *tickHarness) frame(dt float64) { h.loop().Frame(h.k, dt) }

// runFrames drives frames from a goroutine of its own until the returned stop
// is called, which is what the platform loop does and what the join window
// was always racing against: a test that only steps the clock by hand can
// never see an arm lose that race.
func (h *tickHarness) runFrames() (stop func()) {
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-done:
				return
			default:
			}
			h.frame(0.010)
			time.Sleep(time.Millisecond)
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}

func (h *tickHarness) recorded() []app.UpdateEvent {
	h.observer.mu.Lock()
	defer h.observer.mu.Unlock()
	return append([]app.UpdateEvent(nil), h.observer.updates...)
}

func (h *tickHarness) rendered() []app.RenderEvent {
	h.observer.mu.Lock()
	defer h.observer.mu.Unlock()
	return append([]app.RenderEvent(nil), h.observer.renders...)
}

func (h *tickHarness) control(request app.TimeRequest) app.TimeResponse {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	response, err := h.k.WithContext(ctx).ExecuteCommand[app.TimeCmd](request)
	if err != nil {
		h.t.Fatalf("%v: %v", request.Action, err)
	}
	return response
}

// ticks is the tick source behind the Loop, read only to synchronize a test
// with a request that is still in flight.
func (h *tickHarness) ticks() *tickSource { return &h.plugin.loop.ticks }

// waitPending blocks until the tick source is holding steps nobody has
// published yet, so a test can land a second request while one is in flight.
func (h *tickHarness) waitPending(steps int64) {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for h.ticks().pending.Load() != steps {
		if time.Now().After(deadline) {
			h.t.Fatalf("pending steps = %d, want %d", h.ticks().pending.Load(), steps)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitSharing blocks until callers are sharing one pending step, so a test can
// assert the pairing without racing the frame that ends it.
func (h *tickHarness) waitSharing(callers int64) {
	h.t.Helper()
	waitSharing(h.t, h.ticks(), callers, 5*time.Second)
}

func waitSharing(t *testing.T, ticks *tickSource, callers int64, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		ticks.mu.Lock()
		batch := ticks.batch
		ticks.mu.Unlock()
		if batch != nil && batch.sharing.Load() == callers {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no pending step is shared by %d callers", callers)
		}
		time.Sleep(time.Millisecond)
	}
}

func tickTestConfig() app.Config {
	return app.Config{}.
		WithStep(10 * time.Millisecond).
		WithMaxFrame(time.Second).
		WithMaxPending(4)
}
