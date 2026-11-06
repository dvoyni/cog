package gfx_test

import (
	"context"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/metronome"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/platform/headless"
)

// harness is the test's plugin and the supervisor's logger: it binds a
// recording.Instance in its Init, unless told not to, and numberedWindows,
// and runs a function over the Device under its read or write lock, or over
// the WindowManager under its write lock, and ProcessEvents, on demand. It
// spawns metronome with a render track on a ManualClock, and handles every
// DrawEvent, keeping each.
type harness struct {
	t          *testing.T
	supervisor *kernel.Supervisor
	kernel     *kernel.Kernel
	instance   *recording.Instance
	windows    *numberedWindows
	clock      *metronome.ManualClock
	// render is the render track's pacing, metronome.Variable() unless a test
	// sets it before spawning.
	render metronome.Settings
	// holdFrame, when set, runs after each DrawEvent's Ready holding no lock;
	// readFrame, when set, runs after Ready under the Device's read lock.
	// Each is set before spawning.
	holdFrame func(event gfx.DrawEvent)
	readFrame func(event gfx.DrawEvent, device gfx.Device)
	// unbound leaves InstanceSocket without a binding.
	unbound bool
	read    func(device gfx.Device)
	write   func(device kernel.Write[gfx.Device])
	edit    func(manager *platform.WindowManager)
	// handlers is gfx's Handlers as Init found it, when handlersFound.
	handlers      gfx.Handlers
	handlersFound bool
	// mutex guards logged, the errors kernels logged and the test has not
	// taken, any left when the test ends failing it, and drawn, every
	// DrawEvent published.
	mutex  sync.Mutex
	logged []error
	drawn  []gfx.DrawEvent
}

// readEvent runs the harness's read under the Device's read lock.
type readEvent struct{}

// writeEvent runs the harness's write under the Device's write lock.
type writeEvent struct{}

// editEvent runs the harness's edit under the WindowManager's write lock.
type editEvent struct{}

// pumpEvent schedules platform's ProcessEvents.
type pumpEvent struct{}

// numberedWindows are headless windows whose native handles are not zero:
// each opened window's is its number, counted from 1, so a test tells them
// apart.
type numberedWindows struct {
	*headless.Windows
	// openedCount is how many windows Pump has reported opened; only the
	// main thread reads and writes it.
	openedCount uintptr
}

// Pump answers the headless windows' changes, each opened window's with its
// native handle numbered.
func (w *numberedWindows) Pump(changes []platform.InputChange) []platform.InputChange {
	start := len(changes)
	changes = w.Windows.Pump(changes)
	for index := start; index < len(changes); index++ {
		if changes[index].Kind == platform.InputChangeWindowOpened {
			w.openedCount++
			changes[index].Native = platform.NativeHandle{Display: 1, Window: w.openedCount}
		}
	}
	return changes
}

func (*harness) Name() kernel.PluginName { return "test" }

func (*harness) Dependencies() []kernel.PluginName { return []kernel.PluginName{gfx.PluginName} }

func (h *harness) Init(c *kernel.Configurator, config any) error {
	if !h.unbound {
		c.BindSocket[gfx.InstanceSocket](h.instance)
	}
	c.BindSocket[platform.WindowsSocket](h.windows)
	h.handlers, h.handlersFound = c.Resource[gfx.Handlers]()
	read := c.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, device kernel.Read[gfx.Device]) kernel.NoResult {
		h.read(device.Get())
		return kernel.NoResult{}
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event readEvent) (readEvent, kernel.JobHandle) {
		return event, ex.Schedule(read, kernel.NoArg{}, in).Handle()
	})
	write := c.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, device kernel.Write[gfx.Device]) kernel.NoResult {
		h.write(device)
		return kernel.NoResult{}
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event writeEvent) (writeEvent, kernel.JobHandle) {
		return event, ex.Schedule(write, kernel.NoArg{}, in).Handle()
	})
	edit := c.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, manager kernel.Write[*platform.WindowManager]) kernel.NoResult {
		h.edit(manager.Get())
		return kernel.NoResult{}
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event editEvent) (editEvent, kernel.JobHandle) {
		return event, ex.Schedule(edit, kernel.NoArg{}, in).Handle()
	})
	hold := c.RegisterTask0(func(k *kernel.Kernel, event gfx.DrawEvent) kernel.NoResult {
		if h.holdFrame != nil {
			h.holdFrame(event)
		}
		return kernel.NoResult{}
	})
	readFrame := c.RegisterTask1(func(k *kernel.Kernel, event gfx.DrawEvent, device kernel.Read[gfx.Device]) kernel.NoResult {
		if h.readFrame != nil {
			h.readFrame(event, device.Get())
		}
		return kernel.NoResult{}
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.DrawEvent) (gfx.DrawEvent, kernel.JobHandle) {
		h.mutex.Lock()
		h.drawn = append(h.drawn, event)
		h.mutex.Unlock()
		held := ex.Schedule(hold, event, event.Ready).Handle()
		read := ex.Schedule(readFrame, event, event.Ready).Handle()
		return event, ex.Join(in, held, read)
	})
	tasks, _ := c.Resource[platform.Tasks]()
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event pumpEvent) (pumpEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(tasks.ProcessEvents, kernel.NoArg{}).Handle())
	})
	return nil
}

func (*harness) Start(*kernel.Kernel) error { return nil }

func (*harness) Stop(*kernel.Kernel) error { return nil }

func (*harness) Destroy() error { return nil }

func (h *harness) LogErr(source kernel.KernelID, err error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.logged = append(h.logged, err)
}

func (*harness) LogWarn(source kernel.KernelID, message string, args ...any) {}

func (*harness) LogDebug(source kernel.KernelID, message string, args ...any) {}

// newHarness answers a harness with a fresh recording instance and a
// supervisor that quits when the test ends, failing the test on every error
// logged and not taken.
func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		t:        t,
		instance: recording.NewInstance(),
		windows:  &numberedWindows{Windows: headless.NewWindows()},
		clock:    metronome.NewManualClock(),
		render:   metronome.Variable(),
	}
	h.supervisor = kernel.NewSupervisor(h)
	t.Cleanup(func() {
		for _, err := range h.takeLogged() {
			t.Errorf("logged %v", err)
		}
	})
	t.Cleanup(h.supervisor.Quit)
	return h
}

// spawn spawns platform, metronome with the render track, gfx with config,
// and the harness, and answers Spawn's error.
func (h *harness) spawn(config *gfx.Config) error {
	h.t.Helper()
	configs := map[kernel.PluginName]any{
		gfx.PluginName: config,
		metronome.PluginName: &metronome.Config{
			Clock:  h.clock,
			Tracks: []metronome.Track{metronome.NewTrack[gfx.RenderEvent](h.render)},
		},
	}
	id, err := h.supervisor.Spawn(configs, 2, platform.NewPlugin(), metronome.NewPlugin(), gfx.NewPlugin(), h)
	if err == nil {
		h.kernel = h.supervisor.GetKernel(id)
	}
	return err
}

// mustSpawn spawns as spawn does, and fails the test on an error.
func (h *harness) mustSpawn(config *gfx.Config) {
	h.t.Helper()
	if err := h.spawn(config); err != nil {
		h.t.Fatal(err)
	}
}

// readDevice runs read over the Device under its read lock, and waits for it.
func (h *harness) readDevice(read func(device gfx.Device)) {
	h.t.Helper()
	h.read = read
	_, out := h.kernel.Publish(readEvent{})
	h.wait(out)
}

// writeDevice runs write over the Device's write handle, and waits for it.
func (h *harness) writeDevice(write func(device kernel.Write[gfx.Device])) {
	h.t.Helper()
	h.write = write
	_, out := h.kernel.Publish(writeEvent{})
	h.wait(out)
}

// editWindows runs edit over the WindowManager under its write lock, and
// waits for it.
func (h *harness) editWindows(edit func(manager *platform.WindowManager)) {
	h.t.Helper()
	h.edit = edit
	_, out := h.kernel.Publish(editEvent{})
	h.wait(out)
}

// pump runs ProcessEvents, and waits for it.
func (h *harness) pump() {
	h.t.Helper()
	_, out := h.kernel.Publish(pumpEvent{})
	h.wait(out)
}

// openWindow opens a window and pumps, and answers it.
func (h *harness) openWindow() platform.Window {
	h.t.Helper()
	var window platform.Window
	h.editWindows(func(manager *platform.WindowManager) {
		window = manager.Open(platform.WindowEntry{Size: platform.Size{Width: 64, Height: 48}})
	})
	h.pump()
	return window
}

// closeWindow closes window and pumps.
func (h *harness) closeWindow(window platform.Window) {
	h.t.Helper()
	h.editWindows(func(manager *platform.WindowManager) { manager.Close(window) })
	h.pump()
}

// waitForCalls ticks the main thread until the recording holds count calls
// of kind, and answers them.
func (h *harness) waitForCalls(kind recording.CallKind, count int) []recording.Call {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.supervisor.TickMainThread()
		var calls []recording.Call
		for _, call := range h.instance.Recording().Calls {
			if call.Kind == kind {
				calls = append(calls, call)
			}
		}
		if len(calls) >= count {
			return calls
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("%d calls of kind %v in 5s, want %d", len(calls), kind, count)
		}
		runtime.Gosched()
	}
}

// advance advances the clock by d, ticking the main thread for the pinned
// jobs of the beats it lets out, until every beat has finished.
func (h *harness) advance(d time.Duration) {
	h.t.Helper()
	h.untilDone(func() { h.clock.Advance(d) })
}

// settle ticks the main thread until every beat in flight has finished.
func (h *harness) settle() {
	h.t.Helper()
	h.untilDone(func() { _ = h.clock.Wait(context.Background()) })
}

// waitForDraws waits, without ticking the main thread, until count
// DrawEvents have been published, and answers them.
func (h *harness) waitForDraws(count int) []gfx.DrawEvent {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if drawn := h.drawEvents(); len(drawn) >= count {
			return drawn
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("%d DrawEvents in 5s, want %d", len(h.drawEvents()), count)
		}
		runtime.Gosched()
	}
}

// quit quits the supervisor and ticks the main thread, for the pinned jobs
// still to run, until every kernel is destroyed.
func (h *harness) quit() {
	h.t.Helper()
	h.supervisor.Quit()
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.supervisor.TickMainThread()
		select {
		case <-h.supervisor.Done():
			return
		default:
		}
		if time.Now().After(deadline) {
			h.t.Fatal("the supervisor did not quit in 5s")
		}
		runtime.Gosched()
	}
}

// untilDone runs work on a goroutine of its own and ticks the main thread
// until it returns.
func (h *harness) untilDone(work func()) {
	h.t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		work()
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.supervisor.TickMainThread()
		select {
		case <-done:
			return
		default:
		}
		if time.Now().After(deadline) {
			h.t.Fatal("the beats did not settle in 5s")
		}
		runtime.Gosched()
	}
}

// wait ticks the main thread until out has finished.
func (h *harness) wait(out kernel.JobHandle) {
	h.t.Helper()
	signal := kernel.NewSignal()
	h.kernel.Notify(signal, out)
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.supervisor.TickMainThread()
		select {
		case <-signal.C():
			return
		default:
		}
		if time.Now().After(deadline) {
			h.t.Fatal("a job did not finish in 5s")
		}
		runtime.Gosched()
	}
}

// takeLogged answers the errors logged since it was last called.
func (h *harness) takeLogged() []error {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	logged := h.logged
	h.logged = nil
	return logged
}

// drawEvents answers every DrawEvent published so far.
func (h *harness) drawEvents() []gfx.DrawEvent {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return slices.Clone(h.drawn)
}

// kinds answers the kinds of calls, in order.
func kinds(calls []recording.Call) []recording.CallKind {
	answer := make([]recording.CallKind, len(calls))
	for i, call := range calls {
		answer[i] = call.Kind
	}
	return answer
}
