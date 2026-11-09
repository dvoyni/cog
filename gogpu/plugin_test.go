package gogpu_test

import (
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gogpu"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/metronome"
	"github.com/dvoyni/cog/platform"
)

// init keeps the main goroutine on the main thread, where TestMain runs what
// the tests send to mainThread: HostLoop starts and stops there.
func init() { runtime.LockOSThread() }

// mainThread takes the functions the tests run on the main thread.
var mainThread = make(chan func())

func TestMain(m *testing.M) {
	exitCode := make(chan int)
	go func() { exitCode <- m.Run() }()
	for {
		select {
		case run := <-mainThread:
			run()
		case code := <-exitCode:
			os.Exit(code)
		}
	}
}

// onMainThread runs run on the main thread, and waits for it.
func onMainThread(run func()) {
	finished := make(chan struct{})
	mainThread <- func() {
		defer close(finished)
		run()
	}
	<-finished
}

// readEvent runs the harness's read under the Device's read lock.
type readEvent struct{}

// pumpEvent runs the harness's edit under the WindowManager's write lock,
// then ProcessEvents.
type pumpEvent struct{}

// harness spawns a kernel of metronome, platform, gfx, gogpu and itself from
// the main thread, reads its Device, edits its WindowManager and pumps,
// draws, records the window events platform publishes, and quits it, failing
// the test on every error logged.
type harness struct {
	t          *testing.T
	supervisor *kernel.Supervisor
	kernel     *kernel.Kernel
	plugin     *gogpu.Plugin
	read       func(device gfx.Device)
	edit       func(manager *platform.WindowManager)
	readFrame  func(event gfx.DrawEvent, device gfx.Device)
	mutex      sync.Mutex
	logged     []error
	published  []any
	quitted    bool
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t}
	h.supervisor = kernel.NewSupervisor(h)
	t.Cleanup(func() {
		if !h.quitted && !h.quit() {
			t.Error("the kernel did not quit in 5s")
		}
		h.mutex.Lock()
		defer h.mutex.Unlock()
		for _, err := range h.logged {
			t.Errorf("logged %v", err)
		}
	})
	return h
}

// spawn spawns the kernel with gfx's and gogpu's configs, and answers Spawn's
// error. It skips the test on Linux with no display, and where gogpu finds
// no adapter.
func (h *harness) spawn(gfxConfig *gfx.Config, config *gogpu.Config) error {
	h.t.Helper()
	if runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		h.t.Skip("no display")
	}
	var err error
	onMainThread(func() {
		configs := map[kernel.PluginName]any{gfx.PluginName: gfxConfig, gogpu.PluginName: config}
		var id kernel.KernelID
		h.plugin = gogpu.NewPlugin()
		id, err = h.supervisor.Spawn(configs, 2, metronome.NewPlugin(), platform.NewPlugin(), gfx.NewPlugin(), h.plugin, h)
		h.kernel = h.supervisor.GetKernel(id)
	})
	if err != nil && strings.Contains(err.Error(), "failed to request adapter") {
		h.t.Skip(err)
	}
	return err
}

// mustSpawn spawns as spawn does, and fails the test on an error.
func (h *harness) mustSpawn(gfxConfig *gfx.Config, config *gogpu.Config) {
	h.t.Helper()
	if err := h.spawn(gfxConfig, config); err != nil {
		h.t.Fatal(err)
	}
}

// readDevice runs read over the Device under its read lock, ticking the main
// thread until it has run.
func (h *harness) readDevice(read func(device gfx.Device)) {
	h.t.Helper()
	h.read = read
	publishAndWait(h, readEvent{})
}

// pump runs edit, unless nil, over the WindowManager under its write lock,
// then ProcessEvents, ticking the main thread until both have run.
func (h *harness) pump(edit func(manager *platform.WindowManager)) {
	h.t.Helper()
	h.edit = edit
	publishAndWait(h, pumpEvent{})
}

// draw publishes a beat of the render track, ticking the main thread until
// its frame has presented, with readFrame run over the Device after the
// frame's acquire.
func (h *harness) draw(readFrame func(event gfx.DrawEvent, device gfx.Device)) {
	h.t.Helper()
	h.readFrame = readFrame
	publishAndWait(h, gfx.RenderEvent{})
}

// openShownWindow opens testWindow shown, and draws beats until it has a
// framebuffer: its surface is created by a job the pump does not wait for,
// so the first beat may come before it. It fails the test when 10 beats have
// none.
func (h *harness) openShownWindow() platform.Window {
	h.t.Helper()
	shown := testWindow
	shown.Hidden = false
	var window platform.Window
	h.pump(func(manager *platform.WindowManager) { window = manager.Open(shown) })
	for range 10 {
		drawn := false
		h.draw(func(event gfx.DrawEvent, device gfx.Device) {
			drawn = device.Framebuffer(event.Frame, window) != gfx.TextureView{}
		})
		if drawn {
			return window
		}
	}
	h.t.Fatal("the shown window had no framebuffer in 10 beats")
	return window
}

// takePublished answers the window events published since its last call.
func (h *harness) takePublished() []any {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	published := h.published
	h.published = nil
	return published
}

// publishAndWait publishes event on the main thread, and ticks it until the
// jobs event scheduled have run, failing the test when that takes over 5s.
func publishAndWait[E any](h *harness, event E) {
	h.t.Helper()
	finished := false
	onMainThread(func() {
		_, out := h.kernel.Publish(event)
		signal := kernel.NewSignal()
		h.kernel.Notify(signal, out)
		finished = h.tickUntil(signal.C())
	})
	if !finished {
		h.t.Fatalf("the jobs of %T did not finish in 5s", event)
	}
}

// subscribeRecord records every E platform publishes.
func subscribeRecord[E any](c *kernel.Configurator, h *harness) {
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event E) (E, kernel.JobHandle) {
		h.mutex.Lock()
		h.published = append(h.published, event)
		h.mutex.Unlock()
		return event, in
	})
}

// quit quits the supervisor, ticking the main thread until it is done, and
// answers whether that took 5s or less.
func (h *harness) quit() bool {
	h.quitted = true
	finished := false
	onMainThread(func() {
		h.supervisor.Quit()
		finished = h.tickUntil(h.supervisor.Done())
	})
	return finished
}

// tickUntil ticks the main thread until done is closed, and answers false
// when that takes over 5s.
func (h *harness) tickUntil(done <-chan struct{}) bool {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h.supervisor.TickMainThread()
		select {
		case <-done:
			return true
		case <-time.After(time.Millisecond):
		}
	}
	return false
}

func (*harness) Name() kernel.PluginName { return "harness" }

func (*harness) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{gfx.PluginName, platform.PluginName}
}

func (h *harness) Init(c *kernel.Configurator, _ any) error {
	read := c.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, device kernel.Read[gfx.Device]) kernel.NoResult {
		h.read(device.Get())
		return kernel.NoResult{}
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event readEvent) (readEvent, kernel.JobHandle) {
		return event, ex.Schedule(read, kernel.NoArg{}, in).Handle()
	})
	tasks, _ := c.Resource[platform.Tasks]()
	edit := c.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, manager kernel.Write[*platform.WindowManager]) kernel.NoResult {
		if h.edit != nil {
			h.edit(manager.Get())
		}
		return kernel.NoResult{}
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event pumpEvent) (pumpEvent, kernel.JobHandle) {
		edited := ex.Schedule(edit, kernel.NoArg{}, in)
		return event, ex.Schedule(tasks.ProcessEvents, kernel.NoArg{}, edited.Handle()).Handle()
	})
	readFrame := c.RegisterTask1(func(k *kernel.Kernel, event gfx.DrawEvent, device kernel.Read[gfx.Device]) kernel.NoResult {
		if h.readFrame != nil {
			h.readFrame(event, device.Get())
		}
		return kernel.NoResult{}
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.DrawEvent) (gfx.DrawEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(readFrame, event, event.Ready).Handle())
	})
	subscribeRecord[platform.WindowOpenedEvent](c, h)
	subscribeRecord[platform.WindowClosedEvent](c, h)
	subscribeRecord[platform.CloseRequestedEvent](c, h)
	return nil
}

func (*harness) Start(k *kernel.Kernel) error { return nil }

func (*harness) Stop(k *kernel.Kernel) error { return nil }

func (*harness) Destroy() error { return nil }

func (h *harness) LogErr(source kernel.KernelID, err error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.logged = append(h.logged, err)
}

func (*harness) LogWarn(source kernel.KernelID, message string, args ...any) {}

func (*harness) LogDebug(source kernel.KernelID, message string, args ...any) {}

// defaultBackend is gogpu's backend on this system, GL excluded.
func defaultBackend() gfx.Backend {
	if runtime.GOOS == "darwin" {
		return gfx.BackendMetal
	}
	return gfx.BackendVulkan
}

func TestTheDeviceIsGogpusOnItsAdapter(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)

	h.readDevice(func(device gfx.Device) {
		if info := device.AdapterInfo(); info.Name == "" || info.Backend != defaultBackend() {
			t.Errorf("AdapterInfo answered %+v, want a named adapter on %v", info, defaultBackend())
		}
		if features := device.Features(); features != 0 {
			t.Errorf("Features answered %#x, want none", features)
		}
		if limits := device.Limits(); limits != gfx.DefaultLimits() {
			t.Errorf("Limits answered %+v, want the defaults", limits)
		}
	})
}

func TestConfigBackendPicksTheBackend(t *testing.T) {
	backends := map[string][]gfx.Backend{
		"darwin":  {gfx.BackendMetal},
		"linux":   {gfx.BackendVulkan},
		"windows": {gfx.BackendVulkan, gfx.BackendDX12},
	}[runtime.GOOS]
	for _, backend := range backends {
		h := newHarness(t)
		h.mustSpawn(nil, &gogpu.Config{Backend: backend})

		h.readDevice(func(device gfx.Device) {
			if info := device.AdapterInfo(); info.Backend != backend {
				t.Errorf("AdapterInfo answered %+v, want %v", info, backend)
			}
		})
		if !h.quit() {
			t.Fatalf("the kernel on %v did not quit in 5s", backend)
		}
	}
}

func TestGLIsNeverChosen(t *testing.T) {
	h := newHarness(t)

	if err := h.spawn(nil, &gogpu.Config{Backend: gfx.BackendGL}); err == nil {
		t.Error("Spawn answered no error for GL")
	}
}

func TestTheKernelQuitsCleanly(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)

	if !h.quit() {
		t.Fatal("the kernel did not quit in 5s")
	}
	again := newHarness(t)
	again.mustSpawn(nil, nil)
}

func TestShadersArePreprocessedByGogpuShader(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	files := fstest.MapFS{
		"main.wgsl":   {Data: []byte("#include common.wgsl\nfn main() {}\n")},
		"common.wgsl": {Data: []byte("const one = 1;\n")},
	}

	h.readDevice(func(device gfx.Device) {
		source, err := device.PreprocessShader(files, "main.wgsl", nil)
		if err != nil || !strings.Contains(source, "const one = 1;") || !strings.Contains(source, "fn main() {}") {
			t.Errorf("PreprocessShader answered %q, %v, want common.wgsl spliced into main.wgsl", source, err)
		}
	})
}

func TestForceFallbackAdapterSpawnsOnTheSoftwareAdapter(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(&gfx.Config{ForceFallbackAdapter: true}, nil)

	h.readDevice(func(device gfx.Device) {
		if info := device.AdapterInfo(); info.DeviceType != gfx.DeviceTypeCPU {
			t.Errorf("AdapterInfo answered %+v, want wgpu's software adapter", info)
		}
	})
}

func TestALimitBeyondTheAdaptersFailsSpawnNamingIt(t *testing.T) {
	h := newHarness(t)
	limits := gfx.DefaultLimits()
	limits.MaxTextureDimension2D = 1 << 30

	err := h.spawn(&gfx.Config{RequiredLimits: limits}, nil)
	if err == nil || !strings.Contains(err.Error(), "MaxTextureDimension2D") {
		t.Errorf("Spawn answered %v, want an error naming MaxTextureDimension2D", err)
	}
}

// testWindow is a small hidden window, so a run steals no focus.
var testWindow = platform.WindowEntry{Title: "gogpu test", Size: platform.Size{Width: 320, Height: 240}, Hidden: true}

func TestWindowsOpenAndCloseThroughTheWindowManager(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	var window platform.Window

	h.pump(func(manager *platform.WindowManager) { window = manager.Open(testWindow) })
	if published := h.takePublished(); !slices.Equal(published, []any{platform.WindowOpenedEvent{Window: window}}) {
		t.Errorf("opening published %v, want the window opened", published)
	}
	h.pump(func(manager *platform.WindowManager) {
		if !manager.IsOpen(window) || manager.Size(window) != testWindow.Size {
			t.Errorf("the opened window is open %v at %v, want open at %v", manager.IsOpen(window), manager.Size(window), testWindow.Size)
		}
		manager.Close(window)
	})
	if published := h.takePublished(); !slices.Equal(published, []any{platform.WindowClosedEvent{Window: window}}) {
		t.Errorf("closing published %v, want the window closed", published)
	}
}

func TestTheUsersCloseIsReportedAndKeepsTheWindow(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	var window platform.Window
	h.pump(func(manager *platform.WindowManager) { window = manager.Open(testWindow) })
	h.takePublished()

	var kept bool
	onMainThread(func() { kept = !gogpu.RequestClose(h.plugin, window) })
	h.pump(nil)

	if !kept {
		t.Error("the close callback answered true, which closes the window")
	}
	if published := h.takePublished(); !slices.Equal(published, []any{platform.CloseRequestedEvent{Window: window}}) {
		t.Errorf("the user's close published %v, want the close requested", published)
	}
	h.pump(func(manager *platform.WindowManager) {
		if !manager.IsOpen(window) {
			t.Error("the window closed at the user's close")
		}
	})
}

func TestConfigMainWindowOpensAtStart(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, &gogpu.Config{MainWindow: testWindow})

	h.pump(nil)

	published := h.takePublished()
	if len(published) != 1 {
		t.Fatalf("the first pump published %v, want the main window opened", published)
	}
	if _, ok := published[0].(platform.WindowOpenedEvent); !ok {
		t.Errorf("the first pump published %v, want the main window opened", published)
	}
}

func TestEachBeatDrawsAShownWindowAndPresents(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	window := h.openShownWindow()

	for beat := range 3 {
		drawn := false
		h.draw(func(event gfx.DrawEvent, device gfx.Device) {
			drawn = device.Framebuffer(event.Frame, window) != gfx.TextureView{}
		})
		if !drawn {
			t.Errorf("beat %d had no framebuffer for the shown window", beat)
		}
	}
}

func TestAHiddenWindowIsSkipped(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	var window platform.Window
	h.pump(func(manager *platform.WindowManager) { window = manager.Open(testWindow) })

	h.draw(func(event gfx.DrawEvent, device gfx.Device) {
		if view := device.Framebuffer(event.Frame, window); view != (gfx.TextureView{}) {
			t.Errorf("the hidden window had framebuffer %v, want none", view)
		}
	})
}

func TestEachBeatClearsAShownWindowToThePassColor(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	window := h.openShownWindow()

	for beat := range 30 {
		h.draw(func(event gfx.DrawEvent, device gfx.Device) {
			if !clearFramebuffer(t, event, device, window, gfx.Color{R: 1, B: 1, A: 1}) {
				t.Errorf("beat %d had no framebuffer to clear", beat)
			}
		})
	}
}

func TestAFramesSubmitIsSeenFinishedByALaterPoll(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	window := h.openShownWindow()

	var latency time.Duration
	for beat := 0; beat < 10 && latency == 0; beat++ {
		h.draw(func(event gfx.DrawEvent, device gfx.Device) {
			latency = event.GPULatency
			clearFramebuffer(t, event, device, window, gfx.Color{A: 1})
		})
	}
	if latency <= 0 {
		t.Error("no DrawEvent in 10 beats carried a GPU latency, want a frame's submit seen finished")
	}
}

func TestTheSoftwareAdapterClearsAWindow(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(&gfx.Config{ForceFallbackAdapter: true}, nil)
	window := h.openShownWindow()

	for beat := range 3 {
		h.draw(func(event gfx.DrawEvent, device gfx.Device) {
			if !clearFramebuffer(t, event, device, window, gfx.Color{G: 1, A: 1}) {
				t.Errorf("beat %d had no framebuffer to clear", beat)
			}
		})
	}
}

func TestAStandaloneEncoderFinishesAndSubmits(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)

	h.readDevice(func(device gfx.Device) {
		encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{Label: "standalone"})
		if err != nil {
			t.Errorf("CreateCommandEncoder answered %v", err)
			return
		}
		buffer, err := encoder.Finish()
		if err != nil {
			t.Errorf("Finish answered %v", err)
			return
		}
		if err := device.Submit(buffer); err != nil {
			t.Errorf("Submit answered %v", err)
		}
	})
}

func TestARenderPassOnAViewHoldingNothingFailsFinish(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)

	h.readDevice(func(device gfx.Device) {
		encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
		if err != nil {
			t.Errorf("CreateCommandEncoder answered %v", err)
			return
		}
		encoder.BeginRenderPass(gfx.RenderPassDescriptor{ColorAttachments: []gfx.RenderPassColorAttachment{{
			LoadOp: gfx.LoadOpClear, StoreOp: gfx.StoreOpStore,
		}}}).End()
		if buffer, err := encoder.Finish(); err == nil {
			buffer.Release()
			t.Error("Finish answered no error for a pass on the zero view")
		}
	})
}

// clearFramebuffer records, in frame's encoder at order 0, a render pass
// clearing window's framebuffer to color, and answers whether the frame had
// one.
func clearFramebuffer(t *testing.T, event gfx.DrawEvent, device gfx.Device, window platform.Window, color gfx.Color) bool {
	view := device.Framebuffer(event.Frame, window)
	if view == (gfx.TextureView{}) {
		return false
	}
	encoder, err := device.Encoder(event.Frame, 0)
	if err != nil {
		t.Errorf("Encoder answered %v", err)
		return true
	}
	encoder.BeginRenderPass(gfx.RenderPassDescriptor{ColorAttachments: []gfx.RenderPassColorAttachment{{
		View: view, LoadOp: gfx.LoadOpClear, StoreOp: gfx.StoreOpStore, ClearValue: color,
	}}}).End()
	return true
}
