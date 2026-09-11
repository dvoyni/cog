package wgpu

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/input"
	"github.com/dvoyni/cog/kernel"
)

// tickTestUpdateHandler records every published tick.
type tickTestUpdateHandler kernel.Subscription[app.UpdateEvent]

// tickTestPlugin registers the driver's time-control handler against a real
// engine, plus the two seams a paused frame must still reach: the input apply
// command and the update event. The driver's own Register needs a window; the
// tick source does not, so the handler is registered here instead.
type tickTestPlugin struct{ harness *tickHarness }

func (p *tickTestPlugin) Name() kernel.PluginName           { return "ticktest" }
func (p *tickTestPlugin) Dependencies() []kernel.PluginName { return nil }

func (p *tickTestPlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[app.TimeCmd](p.harness.plugin.timeCmdImpl)
	registrar.HandleCommand[input.ApplyCmd](p.harness.applyCmdImpl)
	registrar.Subscribe[tickTestUpdateHandler](p.harness.observeUpdate)
	return nil
}

// tickHarness drives a bare driver's onUpdate against a running engine.
type tickHarness struct {
	t      *testing.T
	plugin *Plugin
	k      kernel.Executioner

	mu      sync.Mutex
	updates []app.UpdateEvent
	applied []input.Change
}

func newTickHarness(t *testing.T, config Config) *tickHarness {
	t.Helper()
	harness := &tickHarness{t: t, plugin: &Plugin{config: config}}
	ctx, cancel := context.WithCancel(context.Background())
	engine := kernel.New(nil).
		Handler(func(err error) bool { t.Errorf("unexpected kernel error: %v", err); return true }).
		WithPlugins(&tickTestPlugin{harness: harness})
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
	return harness
}

func (h *tickHarness) observeUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	return nil, func(_ kernel.Kernel, event app.UpdateEvent) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.updates = append(h.updates, event)
		return nil
	}
}

func (h *tickHarness) applyCmdImpl() (kernel.Lock, kernel.Execute[input.ApplyRequest, input.ApplyResponse]) {
	return nil, func(_ kernel.Kernel, request input.ApplyRequest) (input.ApplyResponse, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.applied = append(h.applied, request.Changes...)
		return input.ApplyResponse{}, nil
	}
}

// frame runs one driver frame: it advances the frame clock by dt the way
// onDraw does on the render thread, then runs onUpdate on this goroutine the
// way gogpu's main loop does.
func (h *tickHarness) frame(dt float64) {
	h.plugin.frameDtBits.Store(math.Float64bits(dt))
	h.plugin.frameSeq.Add(1)
	h.plugin.onUpdate(h.k, 0)
}

func (h *tickHarness) recorded() []app.UpdateEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]app.UpdateEvent(nil), h.updates...)
}

func (h *tickHarness) appliedChanges() []input.Change {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]input.Change(nil), h.applied...)
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

// waitPending blocks until the tick source is holding steps nobody has
// published yet, so a test can land a second request while one is in flight.
func (h *tickHarness) waitPending(steps int64) {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for h.plugin.ticks.pending.Load() != steps {
		if time.Now().After(deadline) {
			h.t.Fatalf("pending steps = %d, want %d", h.plugin.ticks.pending.Load(), steps)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitSharing blocks until callers are sharing one pending step, so a test can
// assert the pairing without racing the frame that ends it.
func (h *tickHarness) waitSharing(callers int64) {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.plugin.ticks.mu.Lock()
		batch := h.plugin.ticks.batch
		h.plugin.ticks.mu.Unlock()
		if batch != nil && batch.sharing.Load() == callers {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("no pending step is shared by %d callers", callers)
		}
		time.Sleep(time.Millisecond)
	}
}

func tickTestConfig() Config {
	return DefaultConfig().
		WithStep(10 * time.Millisecond).
		WithMaxFrame(time.Second).
		WithMaxPending(4)
}

// Pausing stops update ticks and nothing else: the frame still flushes input
// into the seam that reaches the input plugin.
func TestTickSource_PauseStopsTicksAndNothingElse(t *testing.T) {
	harness := newTickHarness(t, tickTestConfig())

	harness.frame(0.010)
	if got := len(harness.recorded()); got != 1 {
		t.Fatalf("running frame published %d ticks, want 1", got)
	}

	if response := harness.control(app.TimeRequest{Action: app.TimePause}); !response.Paused {
		t.Fatal("pause did not report a paused tick source")
	}

	harness.plugin.pending = []input.Change{input.KeyChange(input.KeyA, 0, true)}
	for range 10 {
		harness.frame(0.010)
	}

	if got := len(harness.recorded()); got != 1 {
		t.Errorf("paused frames published %d ticks, want the 1 from before the pause", got)
	}
	if got := len(harness.appliedChanges()); got != 1 {
		t.Errorf("input applied %d changes while paused, want 1", got)
	}
}

// Resume banks nothing: a long pause is followed by exactly one tick, not a
// catch-up burst.
func TestTickSource_ResumeBanksNothing(t *testing.T) {
	harness := newTickHarness(t, tickTestConfig())
	harness.control(app.TimeRequest{Action: app.TimePause})

	for range 100 {
		harness.frame(0.100) // ten steps' worth of frame time each
	}
	if got := len(harness.recorded()); got != 0 {
		t.Fatalf("paused frames published %d ticks, want 0", got)
	}

	response := harness.control(app.TimeRequest{Action: app.TimeResume})
	if response.Paused || !response.Changed {
		t.Fatalf("resume reported %+v, want a change to running", response)
	}

	harness.frame(0.010)
	if got := len(harness.recorded()); got != 1 {
		t.Fatalf("the frame after a long pause published %d ticks, want 1", got)
	}
}

// A step publishes exactly one update, marked as the last of its frame, so
// once-per-frame subscribers do their work and the step produces a complete
// frame.
func TestTickSource_StepPublishesOneLastUpdate(t *testing.T) {
	harness := newTickHarness(t, tickTestConfig())

	done := make(chan app.TimeResponse, 1)
	go func() {
		done <- harness.control(app.TimeRequest{Action: app.TimeStep})
	}()
	harness.waitPending(1)

	harness.frame(0.010)
	response := <-done

	updates := harness.recorded()
	if len(updates) != 1 {
		t.Fatalf("step published %d ticks, want 1", len(updates))
	}
	if !updates[0].Last {
		t.Error("a step's tick is not marked as the last of its frame")
	}
	if updates[0].Dt != harness.plugin.config.Step.Seconds() {
		t.Errorf("step tick Dt = %v, want the fixed step", updates[0].Dt)
	}
	if !response.Paused || !response.Changed {
		t.Errorf("step reported %+v, want a step that implied pause", response)
	}
	if response.Stepped != 1 || response.Advanced != 1 {
		t.Errorf("step reported %+v, want one tick stepped and advanced", response)
	}
}

// A multi-step request publishes all of the steps rather than dropping any,
// even past the catch-up cap, and each is the last of its frame so the last of
// them is what rendering shows.
func TestTickSource_MultiStepPublishesEveryStep(t *testing.T) {
	harness := newTickHarness(t, tickTestConfig().WithMaxPending(2))

	done := make(chan app.TimeResponse, 1)
	go func() {
		done <- harness.control(app.TimeRequest{Action: app.TimeStep, Steps: 5})
	}()
	harness.waitPending(5)

	harness.frame(0.010)
	response := <-done

	updates := harness.recorded()
	if len(updates) != 5 {
		t.Fatalf("step 5 published %d ticks, want 5 despite MaxPending 2", len(updates))
	}
	for i, update := range updates {
		if !update.Last {
			t.Errorf("tick %d of a step is not marked as the last of its frame", i)
		}
	}
	if response.Stepped != 5 || response.Advanced != 5 {
		t.Errorf("step 5 reported %+v, want five ticks stepped", response)
	}
}

// Stepping implies pause, and asking to pause an already-paused engine reads
// back as an ordinary answer rather than an error.
func TestTickSource_PausingAnAlreadyPausedEngineIsAnOrdinaryAnswer(t *testing.T) {
	harness := newTickHarness(t, tickTestConfig())

	first := harness.control(app.TimeRequest{Action: app.TimePause})
	if !first.Paused || !first.Changed {
		t.Fatalf("first pause reported %+v, want a change to paused", first)
	}
	second := harness.control(app.TimeRequest{Action: app.TimePause})
	if !second.Paused || second.Changed {
		t.Fatalf("second pause reported %+v, want paused with nothing changed", second)
	}
	status := harness.control(app.TimeRequest{Action: app.TimeStatus})
	if !status.Paused || status.Changed {
		t.Fatalf("status reported %+v, want paused with nothing changed", status)
	}
}

// An arm landing while a step is pending joins that step rather than raising
// another: that is what lets two capabilities describe one tick.
func TestTickSource_ArmJoinsAPendingStep(t *testing.T) {
	var ticks tickSource

	first, joined, changed := ticks.request(1, false)
	if joined || !changed {
		t.Fatalf("the first step joined=%v changed=%v, want a fresh step that paused", joined, changed)
	}
	second, joined, _ := ticks.request(1, true)
	if !joined {
		t.Fatal("an arm landing while a step is pending raised another step")
	}
	if second != first {
		t.Fatal("an arm landing while a step is pending waits on a different step")
	}
	if pending := ticks.pending.Load(); pending != 1 {
		t.Fatalf("pending steps = %d, want the one step both callers share", pending)
	}
}

// The same rule through the whole engine: two arms and the step they join
// publish one tick between them, and all three callers read back that tick.
func TestTickSource_ArmsSharingAStepDescribeOneTick(t *testing.T) {
	harness := newTickHarness(t, tickTestConfig())
	harness.control(app.TimeRequest{Action: app.TimePause})

	responses := make(chan app.TimeResponse, 3)
	go func() { responses <- harness.control(app.TimeRequest{Action: app.TimeStep}) }()
	harness.waitPending(1)
	for range 2 {
		go func() {
			responses <- harness.control(app.TimeRequest{Action: app.TimeStep, Join: true})
		}()
	}
	harness.waitSharing(3)

	harness.frame(0.010)

	joined := 0
	for range 3 {
		response := <-responses
		if response.Stepped != 1 {
			t.Errorf("a caller read back %d ticks, want the one step they share", response.Stepped)
		}
		if response.Advanced != 1 {
			t.Errorf("a caller read back %d advanced ticks, want 1", response.Advanced)
		}
		if response.Joined {
			joined++
		}
	}
	if joined != 2 {
		t.Errorf("%d callers joined the pending step, want 2", joined)
	}
	if got := len(harness.recorded()); got != 1 {
		t.Errorf("three callers produced %d ticks, want 1", got)
	}
}

// The tick source is read on the frame path with atomics alone: no kernel, no
// dispatch, nothing to lock.
func TestTickSource_ReadsWithoutADispatch(t *testing.T) {
	var ticks tickSource

	if steps, paused, batch := ticks.take(); steps != 0 || paused || batch != nil {
		t.Fatalf("a running tick source read back %d/%v/%v, want no steps and not paused", steps, paused, batch)
	}
	ticks.pause()
	if steps, paused, batch := ticks.take(); steps != 0 || !paused || batch != nil {
		t.Fatalf("a paused tick source read back %d/%v/%v, want no steps and paused", steps, paused, batch)
	}
}

// Resuming with a step still pending releases whoever is waiting for it rather
// than leaving them to time out against a clock that is running again.
func TestTickSource_ResumeReleasesAPendingStep(t *testing.T) {
	harness := newTickHarness(t, tickTestConfig())

	done := make(chan app.TimeResponse, 1)
	go func() { done <- harness.control(app.TimeRequest{Action: app.TimeStep, Steps: 3}) }()
	harness.waitPending(3)

	harness.control(app.TimeRequest{Action: app.TimeResume})
	response := <-done
	if response.Stepped != 0 {
		t.Errorf("a step abandoned by a resume reported %d ticks, want 0", response.Stepped)
	}
	if pending := harness.plugin.ticks.pending.Load(); pending != 0 {
		t.Errorf("%d steps survived the resume, want none", pending)
	}
	if got := len(harness.recorded()); got != 0 {
		t.Errorf("a step abandoned by a resume published %d ticks, want 0", got)
	}
}
