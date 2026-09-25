package internal

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
)

// fakeMainLoop is the MainLoop these tests compose app with: it keeps the Loop app
// attaches, the way a platform MainLoop does, and counts the quits it is asked
// for. A test stands where the platform loop stands and calls that Loop
// on the goroutine it chooses.
type fakeMainLoop struct {
	loop  atomic.Pointer[Loop]
	quits atomic.Int32
}

func (d *fakeMainLoop) Attach(loop Loop) { d.loop.Store(&loop) }
func (d *fakeMainLoop) Quit()            { d.quits.Add(1) }

func (d *fakeMainLoop) ClipboardWrite(string) error { return nil }

// attached is the Loop app handed over, or nil when it handed none.
func (d *fakeMainLoop) attached() Loop {
	if loop := d.loop.Load(); loop != nil {
		return *loop
	}
	return nil
}

// mainLoopAdapter provides a fakeMainLoop as app's MainLoop, the way a platform
// plugin provides its own: app is a Slot, and a composition without one fails.
type mainLoopAdapter struct{ mainLoop *fakeMainLoop }

// testAppMainLoop is the Adapter this fixture fills app's MainLoop Port as.
type testAppMainLoop kernel.Adapter[MainLoopPort]

func (mainLoopAdapter) Name() kernel.PluginName           { return "apptestmainloop" }
func (mainLoopAdapter) Dependencies() []kernel.PluginName { return nil }

func (a mainLoopAdapter) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[testAppMainLoop](MainLoop(a.mainLoop))
	return nil
}

// The subscriptions through which the observer sees every event app publishes.
type (
	observeUpdate     kernel.Subscription[UpdateEvent]
	observeRender     kernel.Subscription[RenderEvent]
	observeInit       kernel.Subscription[InitEvent]
	observeQuit       kernel.Subscription[QuitEvent]
	observeWindowSize kernel.Subscription[WindowSizeChangeEvent]
	observePause      kernel.Subscription[PauseChangeEvent]
)

// observer records, in order, every app event it is delivered, so a test reads
// what subscribers saw rather than what app meant to publish.
type observer struct {
	mu      sync.Mutex
	updates []UpdateEvent
	renders []RenderEvent
	sizes   []WindowSizeChangeEvent
	pauses  []PauseChangeEvent
	// lifecycle is the order InitEvent and QuitEvent arrived in.
	lifecycle []string
}

func (*observer) Name() kernel.PluginName           { return "apptestobserver" }
func (*observer) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }

func (o *observer) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[observeUpdate](func() (kernel.Lock, kernel.Observe[UpdateEvent]) {
		return nil, func(_ kernel.Kernel, event UpdateEvent) {
			o.mu.Lock()
			defer o.mu.Unlock()
			o.updates = append(o.updates, event)
		}
	})
	registrar.Subscribe[observeRender](func() (kernel.Lock, kernel.Observe[RenderEvent]) {
		return nil, func(_ kernel.Kernel, event RenderEvent) {
			o.mu.Lock()
			defer o.mu.Unlock()
			o.renders = append(o.renders, event)
		}
	})
	registrar.Subscribe[observeInit](func() (kernel.Lock, kernel.Observe[InitEvent]) {
		return nil, func(kernel.Kernel, InitEvent) {
			o.mu.Lock()
			defer o.mu.Unlock()
			o.lifecycle = append(o.lifecycle, "init")
		}
	})
	registrar.Subscribe[observeQuit](func() (kernel.Lock, kernel.Observe[QuitEvent]) {
		return nil, func(kernel.Kernel, QuitEvent) {
			o.mu.Lock()
			defer o.mu.Unlock()
			o.lifecycle = append(o.lifecycle, "quit")
		}
	})
	registrar.Subscribe[observeWindowSize](func() (kernel.Lock, kernel.Observe[WindowSizeChangeEvent]) {
		return nil, func(_ kernel.Kernel, event WindowSizeChangeEvent) {
			o.mu.Lock()
			defer o.mu.Unlock()
			o.sizes = append(o.sizes, event)
		}
	})
	registrar.Subscribe[observePause](func() (kernel.Lock, kernel.Observe[PauseChangeEvent]) {
		return nil, func(_ kernel.Kernel, event PauseChangeEvent) {
			o.mu.Lock()
			defer o.mu.Unlock()
			o.pauses = append(o.pauses, event)
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

func newTickHarness(t *testing.T, config Config) *tickHarness {
	t.Helper()
	harness := &tickHarness{t: t, plugin: New().(*plugin), mainLoop: &fakeMainLoop{}, observer: &observer{}}
	engine := kernel.New(map[kernel.PluginName]any{Name: config}).
		Handler(func(err error) error { t.Errorf("unexpected kernel error: %v", err); return err }).
		WithPlugins(harness.plugin, mainLoopAdapter{harness.mainLoop}, harness.observer)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		engine.Run()
	}()
	t.Cleanup(func() {
		engine.Quit()
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
func (h *tickHarness) loop() Loop { return h.mainLoop.attached() }

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

func (h *tickHarness) recorded() []UpdateEvent {
	h.observer.mu.Lock()
	defer h.observer.mu.Unlock()
	return append([]UpdateEvent(nil), h.observer.updates...)
}

func (h *tickHarness) paused() []PauseChangeEvent {
	h.observer.mu.Lock()
	defer h.observer.mu.Unlock()
	return append([]PauseChangeEvent(nil), h.observer.pauses...)
}

func (h *tickHarness) rendered() []RenderEvent {
	h.observer.mu.Lock()
	defer h.observer.mu.Unlock()
	return append([]RenderEvent(nil), h.observer.renders...)
}

func (h *tickHarness) control(request TimeRequest) TimeResponse {
	h.t.Helper()
	response := h.k.ExecuteCommand[TimeCmd](request)
	if response.Err != nil {
		h.t.Fatalf("%v: %v", request.Action, response.Err)
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

func tickTestConfig() Config {
	return Config{}.
		WithStep(10 * time.Millisecond).
		WithMaxFrame(time.Second).
		WithMaxPending(4)
}
