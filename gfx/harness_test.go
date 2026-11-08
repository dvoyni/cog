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
	// recordFrame, when set, runs after each DrawEvent's Ready under the
	// Device's read lock in recordingTaskCount tasks at once, each given its
	// own index. Both are set before spawning, with workerCount.
	recordFrame        func(event gfx.DrawEvent, task int, device gfx.Device)
	recordingTaskCount int
	// workerCount is how many workers the kernel is spawned with, 2 unless a
	// test sets it before spawning.
	workerCount int
	// readCapture, when set, runs under the Device's read lock in a task
	// joined into each CapturedEvent's out dependency. It is set before
	// spawning.
	readCapture func(event gfx.CapturedEvent, device gfx.Device)
	// unbound leaves InstanceSocket without a binding.
	unbound bool
	// wrapDevice, when set, wraps each device the recording adapter answers,
	// for a test to change how calls behave around it. It is set before
	// spawning.
	wrapDevice func(device gfx.BackendDevice) gfx.BackendDevice
	// pinTasks pins every task in gfx's Tasks to the main thread in Init,
	// as a backend whose platform needs it does.
	pinTasks bool
	read     func(device gfx.Device)
	write    func(device kernel.Write[gfx.Device])
	edit     func(manager *platform.WindowManager)
	// handlers is gfx's Handlers as Init found it, when handlersFound.
	handlers      gfx.Handlers
	handlersFound bool
	// tasks is gfx's Tasks as Init found it, when tasksFound.
	tasks      gfx.Tasks
	tasksFound bool
	// mutex guards logged, the errors kernels logged and the test has not
	// taken, any left when the test ends failing it, drawn, every DrawEvent
	// published, mapped, every BufferMappedEvent published, captured, every
	// CapturedEvent published, lost, for every DeviceLostEvent published,
	// how many errors were logged and not taken then, and recoveredCount, how
	// many DeviceRecoveredEvents were published, and renderedCount, how many
	// RenderEvents gfx's Draw has handled.
	mutex          sync.Mutex
	logged         []error
	drawn          []gfx.DrawEvent
	mapped         []gfx.BufferMappedEvent
	captured       []gfx.CapturedEvent
	lost           []int
	recoveredCount int
	renderedCount  int
}

// readEvent runs the harness's read under the Device's read lock.
type readEvent struct{}

// writeEvent runs the harness's write under the Device's write lock.
type writeEvent struct{}

// editEvent runs the harness's edit under the WindowManager's write lock.
type editEvent struct{}

// pumpEvent schedules platform's ProcessEvents.
type pumpEvent struct{}

// recordingTask is what one of a DrawEvent's recording tasks is given: the
// event, and the task's index among them.
type recordingTask struct {
	event gfx.DrawEvent
	index int
}

// wrappingInstance is the recording instance with each device it answers
// wrapped by wrapDevice, through the adapter it answers.
type wrappingInstance struct {
	gfx.InstanceSocket
	wrapDevice func(device gfx.BackendDevice) gfx.BackendDevice
}

func (instance wrappingInstance) RequestAdapter(options gfx.RequestAdapterOptions) (gfx.BackendAdapter, error) {
	adapter, err := instance.InstanceSocket.RequestAdapter(options)
	if err != nil {
		return nil, err
	}
	return wrappingAdapter{BackendAdapter: adapter, wrapDevice: instance.wrapDevice}, nil
}

// wrappingAdapter is a recording adapter with each device it answers wrapped
// by wrapDevice.
type wrappingAdapter struct {
	gfx.BackendAdapter
	wrapDevice func(device gfx.BackendDevice) gfx.BackendDevice
}

func (adapter wrappingAdapter) RequestDevice(descriptor gfx.DeviceDescriptor) (gfx.BackendDevice, error) {
	device, err := adapter.BackendAdapter.RequestDevice(descriptor)
	if err != nil {
		return nil, err
	}
	return adapter.wrapDevice(device), nil
}

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
	switch {
	case h.unbound:
	case h.wrapDevice != nil:
		c.BindSocket[gfx.InstanceSocket](wrappingInstance{InstanceSocket: h.instance, wrapDevice: h.wrapDevice})
	default:
		c.BindSocket[gfx.InstanceSocket](h.instance)
	}
	c.BindSocket[platform.WindowsSocket](h.windows)
	h.handlers, h.handlersFound = c.Resource[gfx.Handlers]()
	h.tasks, h.tasksFound = c.Resource[gfx.Tasks]()
	if h.pinTasks {
		c.PinToMainThread(h.tasks.CreateSurface)
		c.PinToMainThread(h.tasks.ReleaseSurface)
		c.PinToMainThread(h.tasks.ConfigureSurfaces)
		c.PinToMainThread(h.tasks.AcquireFramebuffers)
		c.PinToMainThread(h.tasks.PresentFramebuffers)
		c.PinToMainThread(h.tasks.ReleaseOpenSurfaces)
		c.PinToMainThread(h.tasks.RecoverDevice)
	}
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
	recordFrame := c.RegisterTask1(func(k *kernel.Kernel, task recordingTask, device kernel.Read[gfx.Device]) kernel.NoResult {
		h.recordFrame(task.event, task.index, device.Get())
		return kernel.NoResult{}
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.DrawEvent) (gfx.DrawEvent, kernel.JobHandle) {
		h.mutex.Lock()
		h.drawn = append(h.drawn, event)
		h.mutex.Unlock()
		handles := []kernel.JobHandle{in, ex.Schedule(hold, event, event.Ready).Handle(), ex.Schedule(readFrame, event, event.Ready).Handle()}
		for index := range h.recordingTaskCount {
			handles = append(handles, ex.Schedule(recordFrame, recordingTask{event: event, index: index}, event.Ready).Handle())
		}
		return event, ex.Join(handles...)
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.BufferMappedEvent) (gfx.BufferMappedEvent, kernel.JobHandle) {
		h.mutex.Lock()
		h.mapped = append(h.mapped, event)
		h.mutex.Unlock()
		return event, in
	})
	readCapture := c.RegisterTask1(func(k *kernel.Kernel, event gfx.CapturedEvent, device kernel.Read[gfx.Device]) kernel.NoResult {
		if h.readCapture != nil {
			h.readCapture(event, device.Get())
		}
		return kernel.NoResult{}
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.CapturedEvent) (gfx.CapturedEvent, kernel.JobHandle) {
		h.mutex.Lock()
		h.captured = append(h.captured, event)
		h.mutex.Unlock()
		return event, ex.Join(in, ex.Schedule(readCapture, event).Handle())
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.DeviceLostEvent) (gfx.DeviceLostEvent, kernel.JobHandle) {
		h.mutex.Lock()
		h.lost = append(h.lost, len(h.logged))
		h.mutex.Unlock()
		return event, in
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.DeviceRecoveredEvent) (gfx.DeviceRecoveredEvent, kernel.JobHandle) {
		h.mutex.Lock()
		h.recoveredCount++
		h.mutex.Unlock()
		return event, in
	})
	// The harness is listed after gfx, so this runs after Draw.
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.RenderEvent) (gfx.RenderEvent, kernel.JobHandle) {
		h.mutex.Lock()
		h.renderedCount++
		h.mutex.Unlock()
		return event, in
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
		t:           t,
		instance:    recording.NewInstance(),
		windows:     &numberedWindows{Windows: headless.NewWindows()},
		clock:       metronome.NewManualClock(),
		render:      metronome.Variable(),
		workerCount: 2,
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
	id, err := h.supervisor.Spawn(configs, h.workerCount, platform.NewPlugin(), metronome.NewPlugin(), gfx.NewPlugin(), h)
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

// readDeviceWithoutTicking runs read over the Device under its read lock,
// and waits for it without ticking the main thread, so no pinned job runs
// meanwhile.
func (h *harness) readDeviceWithoutTicking(read func(device gfx.Device)) {
	h.t.Helper()
	h.read = read
	_, out := h.kernel.Publish(readEvent{})
	signal := kernel.NewSignal()
	h.kernel.Notify(signal, out)
	select {
	case <-signal.C():
	case <-time.After(5 * time.Second):
		h.t.Fatal("a job did not finish in 5s")
	}
}

// createBuffers creates count buffers of 4 bytes, for frame encoders to clear,
// and answers them.
func (h *harness) createBuffers(count int) []gfx.Buffer {
	h.t.Helper()
	buffers := make([]gfx.Buffer, count)
	h.readDevice(func(device gfx.Device) {
		for index := range buffers {
			buffer, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 4, Usage: gfx.BufferUsageFlagCopyDst})
			if err != nil {
				h.t.Errorf("CreateBuffer answered %v", err)
			}
			buffers[index] = buffer
		}
	})
	return buffers
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

// openWindow opens a window of 64 by 48 and pumps, and answers it.
func (h *harness) openWindow() platform.Window {
	h.t.Helper()
	return h.openWindowOfSize(platform.Size{Width: 64, Height: 48})
}

// openWindowOfSize opens a window of size and pumps, and answers it.
func (h *harness) openWindowOfSize(size platform.Size) platform.Window {
	h.t.Helper()
	var window platform.Window
	h.editWindows(func(manager *platform.WindowManager) {
		window = manager.Open(platform.WindowEntry{Size: size})
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

// waitForRenders waits, without ticking the main thread, until gfx's Draw
// has handled count RenderEvents.
func (h *harness) waitForRenders(count int) {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.mutex.Lock()
		rendered := h.renderedCount
		h.mutex.Unlock()
		if rendered >= count {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("%d RenderEvents handled in 5s, want %d", rendered, count)
		}
		runtime.Gosched()
	}
}

// waitForSubmissions waits, without ticking the main thread, until the
// recording holds count submissions.
func (h *harness) waitForSubmissions(count int) {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(h.instance.Recording().Submissions) < count {
		if time.Now().After(deadline) {
			h.t.Fatalf("%d submissions in 5s, want %d", len(h.instance.Recording().Submissions), count)
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

// mappedEvents answers every BufferMappedEvent published so far.
func (h *harness) mappedEvents() []gfx.BufferMappedEvent {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return slices.Clone(h.mapped)
}

// capturedEvents answers every CapturedEvent published so far.
func (h *harness) capturedEvents() []gfx.CapturedEvent {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return slices.Clone(h.captured)
}

// lostEvents answers, for every DeviceLostEvent published so far, how many
// errors were logged and not taken when it was.
func (h *harness) lostEvents() []int {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return slices.Clone(h.lost)
}

// recoveredEvents answers how many DeviceRecoveredEvents were published so
// far.
func (h *harness) recoveredEvents() int {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return h.recoveredCount
}

// kinds answers the kinds of calls, in order.
func kinds(calls []recording.Call) []recording.CallKind {
	answer := make([]recording.CallKind, len(calls))
	for i, call := range calls {
		answer[i] = call.Kind
	}
	return answer
}
