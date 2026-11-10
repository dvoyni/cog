package gogpu_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
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
	"github.com/gogpu/gpucontext"
	"github.com/gogpu/wgpu"
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

// readInputEvent runs the harness's readInput under the InputState's read
// lock.
type readInputEvent struct{}

// harness spawns a kernel of metronome, platform, gfx, gogpu and itself from
// the main thread, reads its Device and InputState, edits its WindowManager
// and pumps, draws, records the window, pointer and key events platform
// publishes, gfx's BufferMappedEvent and each capture's picture, and quits
// it, failing the test on every error logged.
type harness struct {
	t          *testing.T
	supervisor *kernel.Supervisor
	kernel     *kernel.Kernel
	plugin     *gogpu.Plugin
	read       func(device gfx.Device)
	readInput  func(input *platform.InputState)
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

// readInputState runs read over the InputState under its read lock, ticking
// the main thread until it has run.
func (h *harness) readInputState(read func(input *platform.InputState)) {
	h.t.Helper()
	h.readInput = read
	publishAndWait(h, readInputEvent{})
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
	h.drawUntilFramed(window)
	return window
}

// drawUntilFramed draws beats until window has a framebuffer, and fails the
// test when 10 beats have none: a window shown gets its first one from a
// later beat.
func (h *harness) drawUntilFramed(window platform.Window) {
	h.t.Helper()
	framed := false
	draw := func(event gfx.DrawEvent, device gfx.Device) {
		framed = device.Framebuffer(event.Frame, window) != gfx.TextureView{}
	}
	for range 10 {
		h.draw(draw)
		if framed {
			return
		}
	}
	h.t.Fatalf("%v had no framebuffer in 10 beats", window)
}

// readBack draws beats until buffer's map is published resolved, failing the
// test when none is in 5s or it failed, and answers a copy of its first size
// bytes, unmapping it. Beats with no window shown take no time, so they are
// not counted.
func (h *harness) readBack(buffer gfx.Buffer, size uint64) []byte {
	h.t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		h.draw(nil)
		for _, event := range h.takePublished() {
			if mapped, ok := event.(gfx.BufferMappedEvent); !ok || mapped.Buffer != buffer {
				continue
			} else if mapped.Status != gfx.MapStatusMapped {
				h.t.Fatalf("%v's map was published %v", buffer, mapped.Status)
			}
			var read []byte
			h.readDevice(func(device gfx.Device) {
				mappedRange, err := device.MappedRange(buffer, 0, size)
				if err != nil {
					h.t.Errorf("MappedRange answered %v", err)
					return
				}
				read = bytes.Clone(mappedRange)
				if err := device.UnmapBuffer(buffer); err != nil {
					h.t.Errorf("UnmapBuffer answered %v", err)
				}
			})
			return read
		}
	}
	h.t.Fatalf("no beat in 5s published %v's map", buffer)
	return nil
}

// capture requests window's capture and draws beats clearing it to color
// until its picture is read, failing the test when none is in 5s, or the
// capture holds none.
func (h *harness) capture(window platform.Window, color gfx.Color) *image.NRGBA {
	h.t.Helper()
	h.readDevice(func(device gfx.Device) {
		if err := device.RequestCapture(window); err != nil {
			h.t.Errorf("RequestCapture answered %v", err)
		}
	})
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		h.draw(func(event gfx.DrawEvent, device gfx.Device) { clearFramebuffer(h.t, event, device, window, color) })
		for _, event := range h.takePublished() {
			if captured, ok := event.(capturedPicture); ok {
				if captured.err != nil {
					h.t.Fatalf("reading %v's capture answered %v", window, captured.err)
				}
				return captured.picture
			}
		}
	}
	h.t.Fatalf("no beat in 5s published %v's capture", window)
	return nil
}

// takePublished answers the events recorded since its last call.
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

// subscribeRecord records every E published.
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
	readInput := c.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, input kernel.Read[*platform.InputState]) kernel.NoResult {
		h.readInput(input.Get())
		return kernel.NoResult{}
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event readInputEvent) (readInputEvent, kernel.JobHandle) {
		return event, ex.Schedule(readInput, kernel.NoArg{}, in).Handle()
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
	subscribeRecord[platform.PointerEnterEvent](c, h)
	subscribeRecord[platform.PointerMoveEvent](c, h)
	subscribeRecord[platform.PointerLeaveEvent](c, h)
	subscribeRecord[platform.KeyEvent](c, h)
	subscribeRecord[gfx.BufferMappedEvent](c, h)
	readCapture := c.RegisterTask1(func(k *kernel.Kernel, event gfx.CapturedEvent, device kernel.Read[gfx.Device]) kernel.NoResult {
		picture, err := gfx.ReadCapture(device.Get(), event)
		h.mutex.Lock()
		h.published = append(h.published, capturedPicture{picture: picture, err: err})
		h.mutex.Unlock()
		return kernel.NoResult{}
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.CapturedEvent) (gfx.CapturedEvent, kernel.JobHandle) {
		return event, ex.Schedule(readCapture, event, in).Handle()
	})
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

// capturedPicture is what ReadCapture answered for a CapturedEvent, read in
// a task its publication joins, before gfx releases its buffer.
type capturedPicture struct {
	picture *image.NRGBA
	err     error
}

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

func TestThePointerIsFollowedAcrossTwoWindows(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	var first, second platform.Window
	h.pump(func(manager *platform.WindowManager) {
		first = manager.Open(testWindow)
		second = manager.Open(testWindow)
	})
	h.takePublished()

	onMainThread(func() {
		gogpu.ReportPointer(h.plugin, first, gpucontext.PointerEvent{Type: gpucontext.PointerEnter, X: 10, Y: 20})
		gogpu.ReportPointer(h.plugin, first, gpucontext.PointerEvent{Type: gpucontext.PointerMove, X: 30, Y: 40})
		gogpu.ReportPointer(h.plugin, first, gpucontext.PointerEvent{Type: gpucontext.PointerLeave, X: 320, Y: 40})
		gogpu.ReportPointer(h.plugin, second, gpucontext.PointerEvent{Type: gpucontext.PointerEnter, X: 5, Y: 6})
		gogpu.ReportPointer(h.plugin, second, gpucontext.PointerEvent{Type: gpucontext.PointerMove, X: 7, Y: 8})
	})
	h.pump(nil)

	want := []any{
		platform.PointerEnterEvent{Window: first},
		platform.PointerMoveEvent{Window: first, Position: platform.Point{X: 30, Y: 40}, Delta: platform.Point{X: 20, Y: 20}},
		platform.PointerLeaveEvent{Window: first},
		platform.PointerEnterEvent{Window: second},
		platform.PointerMoveEvent{Window: second, Position: platform.Point{X: 7, Y: 8}, Delta: platform.Point{X: 2, Y: 2}},
	}
	if published := h.takePublished(); !slices.Equal(published, want) {
		t.Errorf("the pointer's crossing published %v, want %v", published, want)
	}
	h.readInputState(func(input *platform.InputState) {
		if input.IsPointerInside(first) || input.PointerPosition(first) != (platform.Point{X: 30, Y: 40}) {
			t.Errorf("the first window's pointer is inside %v at %v, want outside at its last move", input.IsPointerInside(first), input.PointerPosition(first))
		}
		if !input.IsPointerInside(second) || input.PointerPosition(second) != (platform.Point{X: 7, Y: 8}) {
			t.Errorf("the second window's pointer is inside %v at %v, want inside at its last move", input.IsPointerInside(second), input.PointerPosition(second))
		}
	})
}

func TestAButtonIsPressedAndReleasedWhereThePointerIs(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	var window platform.Window
	h.pump(func(manager *platform.WindowManager) { window = manager.Open(testWindow) })
	h.takePublished()

	onMainThread(func() {
		gogpu.ReportPointer(h.plugin, window, gpucontext.PointerEvent{Type: gpucontext.PointerDown, X: 10, Y: 20, Button: gpucontext.ButtonRight, Modifiers: gpucontext.ModShift})
		gogpu.ReportPointer(h.plugin, window, gpucontext.PointerEvent{Type: gpucontext.PointerUp, X: 30, Y: 40, Button: gpucontext.ButtonRight})
	})
	h.pump(nil)

	want := []any{
		platform.KeyEvent{Window: window, Key: platform.KeyMouseRight, Pressed: true, Modifiers: platform.ModifierShift, Position: platform.Point{X: 10, Y: 20}},
		platform.KeyEvent{Window: window, Key: platform.KeyMouseRight, Position: platform.Point{X: 30, Y: 40}},
	}
	if published := h.takePublished(); !slices.Equal(published, want) {
		t.Errorf("the click published %v, want %v", published, want)
	}
	h.pump(nil)
	if published := h.takePublished(); len(published) != 0 {
		t.Errorf("a later pump published %v, want nothing", published)
	}
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

func TestShowingAWindowReachesTheOneItNames(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	var first, second platform.Window
	h.pump(func(manager *platform.WindowManager) {
		first = manager.Open(testWindow)
		second = manager.Open(testWindow)
	})

	h.pump(func(manager *platform.WindowManager) { manager.SetHidden(second, false) })
	h.drawUntilFramed(second)
	h.pump(func(manager *platform.WindowManager) { manager.SetHidden(first, false) })
	h.drawUntilFramed(first)
}

func TestResizingOneOfTwoWindowsChangesItsSizesAloneAndItsSurfaceAtItsNextFrame(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	resized := h.openShownWindow()
	other := h.openShownWindow()
	var framebufferBefore, otherFramebuffer platform.Size
	want := platform.Size{Width: 400, Height: 300}
	h.pump(func(manager *platform.WindowManager) {
		framebufferBefore = manager.FramebufferSize(resized)
		otherFramebuffer = manager.FramebufferSize(other)
		manager.SetSize(resized, want)
	})

	reported := false
	for deadline := time.Now().Add(5 * time.Second); !reported && time.Now().Before(deadline); {
		h.pump(func(manager *platform.WindowManager) { reported = manager.Size(resized) == want })
	}
	if !reported {
		t.Fatalf("no pump in 5s reported %v resized to %v", resized, want)
	}
	var framebuffer platform.Size
	h.pump(func(manager *platform.WindowManager) {
		framebuffer = manager.FramebufferSize(resized)
		if framebuffer == framebufferBefore {
			t.Errorf("the resized window's framebuffer stayed %v", framebuffer)
		}
		if manager.Size(other) != testWindow.Size || manager.FramebufferSize(other) != otherFramebuffer {
			t.Errorf("the other window is %v with a framebuffer of %v, want %v and %v as before", manager.Size(other), manager.FramebufferSize(other), testWindow.Size, otherFramebuffer)
		}
	})
	if size := h.capture(resized, gfx.Color{G: 1, A: 1}).Bounds().Size(); size != image.Pt(framebuffer.Width, framebuffer.Height) {
		t.Errorf("the capture after the resize is %v, want the resized framebuffer, %v", size, framebuffer)
	}
}

func TestFocusMovesBetweenTwoWindows(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	var first, second platform.Window
	h.pump(func(manager *platform.WindowManager) {
		first = manager.Open(testWindow)
		second = manager.Open(testWindow)
	})

	checkFocusedAlone := func(focused, unfocused platform.Window) {
		t.Helper()
		h.pump(nil)
		h.pump(func(manager *platform.WindowManager) {
			if !manager.IsFocused(focused) || manager.IsFocused(unfocused) {
				t.Errorf("%v is focused %v and %v %v, want %v alone", focused, manager.IsFocused(focused), unfocused, manager.IsFocused(unfocused), focused)
			}
		})
	}

	onMainThread(func() { gogpu.ReportFocus(h.plugin, first, true) })
	checkFocusedAlone(first, second)
	onMainThread(func() {
		gogpu.ReportFocus(h.plugin, first, false)
		gogpu.ReportFocus(h.plugin, second, true)
	})
	checkFocusedAlone(second, first)
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

func TestEachCaptureHoldsTheColourItsFrameClearedTo(t *testing.T) {
	for _, adapter := range []struct {
		name   string
		config *gfx.Config
	}{{"default", nil}, {"software", &gfx.Config{ForceFallbackAdapter: true}}} {
		t.Run(adapter.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustSpawn(adapter.config, nil)
			window := h.openShownWindow()
			var framebuffer platform.Size
			h.pump(func(manager *platform.WindowManager) { framebuffer = manager.FramebufferSize(window) })

			for _, want := range []color.NRGBA{{R: 0xFF, A: 0xFF}, {B: 0xFF, A: 0xFF}} {
				picture := h.capture(window, gfx.Color{R: float64(want.R) / 0xFF, B: float64(want.B) / 0xFF, A: 1})
				if size := picture.Bounds().Size(); size != image.Pt(framebuffer.Width, framebuffer.Height) {
					t.Errorf("the capture is %v, want the window's framebuffer, %v", size, framebuffer)
				}
				for y := range picture.Bounds().Dy() {
					for x := range picture.Bounds().Dx() {
						if got := picture.NRGBAAt(x, y); got != want {
							t.Fatalf("the capture's pixel at %d,%d is %v, want %v", x, y, got, want)
						}
					}
				}
			}
			for range 3 {
				h.draw(func(event gfx.DrawEvent, device gfx.Device) {
					clearFramebuffer(t, event, device, window, gfx.Color{G: 1, A: 1})
				})
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

func TestBytesWrittenToABufferAreReadBackThroughItsMap(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)

	written := []byte("sixteen bytes in")
	var buffer gfx.Buffer
	h.readDevice(func(device gfx.Device) { buffer = mapWrittenBuffer(t, device, written) })
	if read := h.readBack(buffer, uint64(len(written))); !bytes.Equal(read, written) {
		t.Errorf("the map read %q, want %q", read, written)
	}
}

func TestAReleasedBuffersIndexHoldsTheNextBuffer(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)

	written := []byte("the second write")
	var released, buffer gfx.Buffer
	h.readDevice(func(device gfx.Device) {
		released = mapWrittenBuffer(t, device, []byte("the first write!"))
		device.ReleaseBuffer(released)
		buffer = mapWrittenBuffer(t, device, written)
	})
	if buffer.Index() != released.Index() {
		t.Fatalf("the second buffer is at %d, want the released one's %d", buffer.Index(), released.Index())
	}
	if read := h.readBack(buffer, uint64(len(written))); !bytes.Equal(read, written) {
		t.Errorf("the map read %q, want %q", read, written)
	}
}

func TestATextureWrittenAndClearedThroughItsViewSubmits(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)

	h.readDevice(func(device gfx.Device) {
		texture := createTexture(t, device, 4)
		if err := writeWholeTexture(device, texture, 4); err != nil {
			t.Errorf("WriteTexture answered %v", err)
		}
		view, err := device.CreateTextureView(texture, gfx.TextureViewDescriptor{})
		if err != nil {
			t.Errorf("CreateTextureView answered %v", err)
			return
		}
		encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
		if err != nil {
			t.Errorf("CreateCommandEncoder answered %v", err)
			return
		}
		encoder.BeginRenderPass(gfx.RenderPassDescriptor{ColorAttachments: []gfx.RenderPassColorAttachment{{
			View: view, LoadOp: gfx.LoadOpClear, StoreOp: gfx.StoreOpStore,
		}}}).End()
		buffer, err := encoder.Finish()
		if err != nil {
			t.Errorf("Finish answered %v", err)
			return
		}
		if err := device.Submit(buffer); err != nil {
			t.Errorf("Submit answered %v", err)
		}
		device.ReleaseTextureView(view)
		device.ReleaseTexture(texture)
	})
}

func TestAReleasedTexturesIndexHoldsTheNextTexture(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)

	h.readDevice(func(device gfx.Device) {
		released := createTexture(t, device, 4)
		device.ReleaseTexture(released)
		texture := createTexture(t, device, 8)
		if texture.Index() != released.Index() {
			t.Errorf("the second texture is at %d, want the released one's %d", texture.Index(), released.Index())
			return
		}
		if err := writeWholeTexture(device, texture, 8); err != nil {
			t.Errorf("writing the whole second texture answered %v", err)
		}
	})
}

func TestASamplerIsCreatedAsDescribed(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)

	h.readDevice(func(device gfx.Device) {
		linear := gfx.SamplerDescriptor{
			MagFilter: gfx.FilterModeLinear, MinFilter: gfx.FilterModeLinear, MipmapFilter: gfx.FilterModeLinear,
			Anisotropy: 16,
		}
		sampler, err := device.CreateSampler(linear)
		if err != nil {
			t.Errorf("CreateSampler answered %v for an anisotropic linear sampler", err)
		} else {
			device.ReleaseSampler(sampler)
		}
		nearest := linear
		nearest.MagFilter = gfx.FilterModeNearest
		if _, err := device.CreateSampler(nearest); err == nil {
			t.Error("CreateSampler answered no error for an anisotropic sampler magnifying the nearest texel, which WebGPU forbids")
		}
	})
}

func TestAShaderModuleIsCreatedFromValidWGSL(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)

	h.readDevice(func(device gfx.Device) {
		module, err := device.CreateShaderModule(gfx.ShaderModuleDescriptor{WGSL: sampledWGSL})
		if err != nil {
			t.Errorf("CreateShaderModule answered %v for valid WGSL", err)
			return
		}
		device.ReleaseShaderModule(module)
	})
}

func TestInvalidWGSLAnswersAnErrorNamingTheProblem(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)

	h.readDevice(func(device gfx.Device) {
		_, err := device.CreateShaderModule(gfx.ShaderModuleDescriptor{WGSL: "fn value() -> f32 { return undeclared; }"})
		if err == nil || !strings.Contains(err.Error(), "undeclared") {
			t.Errorf("CreateShaderModule answered %v for WGSL returning an undeclared identifier, not an error naming it", err)
		}
	})
}

func TestABindGroupIsCreatedOverItsLayoutAndThePipelineLayoutHoldingIt(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)

	h.readDevice(func(device gfx.Device) {
		layout, err := device.CreateBindGroupLayout(sampledLayout)
		if err != nil {
			t.Errorf("CreateBindGroupLayout answered %v", err)
			return
		}
		defer device.ReleaseBindGroupLayout(layout)
		pipelineLayout, err := device.CreatePipelineLayout(gfx.PipelineLayoutDescriptor{BindGroupLayouts: []gfx.BindGroupLayout{layout}})
		if err != nil {
			t.Errorf("CreatePipelineLayout answered %v", err)
		} else {
			device.ReleasePipelineLayout(pipelineLayout)
		}
		entries, release := createSampledResources(t, device)
		defer release()
		group, err := device.CreateBindGroup(gfx.BindGroupDescriptor{Layout: layout, Entries: entries})
		if err != nil {
			t.Errorf("CreateBindGroup answered %v for the resources its layout describes", err)
		} else {
			device.ReleaseBindGroup(group)
		}
	})
}

func TestABufferBoundWithoutTheUsageItsLayoutNeedsAnswersAnError(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)

	h.readDevice(func(device gfx.Device) {
		layout, err := device.CreateBindGroupLayout(sampledLayout)
		if err != nil {
			t.Errorf("CreateBindGroupLayout answered %v", err)
			return
		}
		defer device.ReleaseBindGroupLayout(layout)
		entries, release := createSampledResources(t, device)
		defer release()
		storage, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 16, Usage: gfx.BufferUsageFlagStorage})
		if err != nil {
			t.Errorf("CreateBuffer answered %v", err)
			return
		}
		defer device.ReleaseBuffer(storage)
		entries[0].Buffer = storage
		if _, err := device.CreateBindGroup(gfx.BindGroupDescriptor{Layout: layout, Entries: entries}); err == nil {
			t.Error("CreateBindGroup answered no error for a storage buffer bound where its layout holds a uniform buffer")
		}
	})
}

func TestATriangleIsDrawnIntoAShownWindow(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	window := h.openShownWindow()
	var pipeline gfx.RenderPipeline
	h.readDevice(func(device gfx.Device) {
		pipeline = createRenderPipeline(t, device, triangleWGSL, []gfx.BindGroupLayout{}, nil, gfx.TextureFormatBGRA8Unorm)
	})

	for beat := range 30 {
		h.draw(func(event gfx.DrawEvent, device gfx.Device) {
			view := device.Framebuffer(event.Frame, window)
			if view == (gfx.TextureView{}) {
				t.Errorf("beat %d had no framebuffer to draw to", beat)
				return
			}
			encoder, err := device.Encoder(event.Frame, 0)
			if err != nil {
				t.Errorf("Encoder answered %v", err)
				return
			}
			pass := encoder.BeginRenderPass(gfx.RenderPassDescriptor{ColorAttachments: []gfx.RenderPassColorAttachment{{
				View: view, LoadOp: gfx.LoadOpClear, StoreOp: gfx.StoreOpStore, ClearValue: gfx.Color{A: 1},
			}}})
			pass.SetPipeline(pipeline)
			pass.Draw(gfx.DrawArgs{VertexCount: 3, InstanceCount: 1})
			pass.End()
		})
	}
}

func TestEachDrawSamplesATextureAsWritten(t *testing.T) {
	draws := map[string]func(pass gfx.RenderPassEncoder, indirect gfx.Buffer){
		"Draw": func(pass gfx.RenderPassEncoder, _ gfx.Buffer) {
			pass.Draw(gfx.DrawArgs{VertexCount: 3, InstanceCount: 1})
		},
		"DrawIndexed": func(pass gfx.RenderPassEncoder, _ gfx.Buffer) {
			pass.DrawIndexed(gfx.DrawIndexedArgs{IndexCount: 3, InstanceCount: 1})
		},
		"DrawIndirect":             func(pass gfx.RenderPassEncoder, indirect gfx.Buffer) { pass.DrawIndirect(indirect, 0) },
		"DrawIndexedIndirect":      func(pass gfx.RenderPassEncoder, indirect gfx.Buffer) { pass.DrawIndexedIndirect(indirect, 16) },
		"MultiDrawIndirect":        func(pass gfx.RenderPassEncoder, indirect gfx.Buffer) { pass.MultiDrawIndirect(indirect, 0, 1) },
		"MultiDrawIndexedIndirect": func(pass gfx.RenderPassEncoder, indirect gfx.Buffer) { pass.MultiDrawIndexedIndirect(indirect, 16, 1) },
	}
	for name, draw := range draws {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.mustSpawn(nil, nil)

			var readback gfx.Buffer
			h.readDevice(func(device gfx.Device) { readback = drawSampledTexture(t, device, false, draw) })
			checkSampledTexture(h, readback)
		})
	}
}

func TestEachDispatchWritesABufferReadBackThroughACopy(t *testing.T) {
	dispatches := map[string]func(pass gfx.ComputePassEncoder, indirect gfx.Buffer){
		"Dispatch":         func(pass gfx.ComputePassEncoder, _ gfx.Buffer) { pass.Dispatch(2, 1, 1) },
		"DispatchIndirect": func(pass gfx.ComputePassEncoder, indirect gfx.Buffer) { pass.DispatchIndirect(indirect, 0) },
	}
	for name, dispatch := range dispatches {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.mustSpawn(nil, nil)

			var readback gfx.Buffer
			h.readDevice(func(device gfx.Device) { readback = dispatchSquares(t, device, false, dispatch) })
			checkSquares(h, readback)
		})
	}
}

func TestAPipelineOfAZeroLayoutRunsOverTheLayoutsReflected(t *testing.T) {
	t.Run("render", func(t *testing.T) {
		h := newHarness(t)
		h.mustSpawn(nil, nil)

		var readback gfx.Buffer
		h.readDevice(func(device gfx.Device) {
			readback = drawSampledTexture(t, device, true, func(pass gfx.RenderPassEncoder, _ gfx.Buffer) {
				pass.Draw(gfx.DrawArgs{VertexCount: 3, InstanceCount: 1})
			})
		})
		checkSampledTexture(h, readback)
	})
	t.Run("compute", func(t *testing.T) {
		h := newHarness(t)
		h.mustSpawn(nil, nil)

		var readback gfx.Buffer
		h.readDevice(func(device gfx.Device) {
			readback = dispatchSquares(t, device, true, func(pass gfx.ComputePassEncoder, _ gfx.Buffer) { pass.Dispatch(2, 1, 1) })
		})
		checkSquares(h, readback)
	})
}

func TestAnF32TextureIsReflectedFilterableOnlyWhenSampled(t *testing.T) {
	for entryPoint, sampled := range map[string]bool{"load": false, "sample": true} {
		t.Run(entryPoint, func(t *testing.T) {
			h := newHarness(t)
			h.mustSpawn(nil, nil)

			h.readDevice(func(device gfx.Device) {
				module, err := device.CreateShaderModule(gfx.ShaderModuleDescriptor{WGSL: pickingWGSL})
				if err != nil {
					t.Errorf("CreateShaderModule answered %v", err)
				}
				pipeline, err := device.CreateComputePipeline(gfx.ComputePipelineDescriptor{Module: module, EntryPoint: entryPoint})
				if err != nil {
					t.Errorf("CreateComputePipeline answered %v", err)
				}
				texture, err := device.CreateTexture(gfx.TextureDescriptor{
					Size:   gfx.Extent3D{Width: 1, Height: 1, DepthOrArrayLayers: 1},
					Format: gfx.TextureFormatR32Float,
					Usage:  gfx.TextureUsageFlagTextureBinding,
				})
				if err != nil {
					t.Errorf("CreateTexture answered %v", err)
				}
				view, err := device.CreateTextureView(texture, gfx.TextureViewDescriptor{})
				if err != nil {
					t.Errorf("CreateTextureView answered %v", err)
				}
				picked, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 4, Usage: gfx.BufferUsageFlagStorage})
				if err != nil {
					t.Errorf("CreateBuffer answered %v", err)
				}
				entries := []gfx.BindGroupEntry{{Binding: 0, TextureView: view}, {Binding: 1, Buffer: picked}}
				if sampled {
					sampler, err := device.CreateSampler(gfx.SamplerDescriptor{})
					if err != nil {
						t.Errorf("CreateSampler answered %v", err)
					}
					entries = append(entries, gfx.BindGroupEntry{Binding: 2, Sampler: sampler})
				}
				_, err = device.CreateBindGroup(gfx.BindGroupDescriptor{Layout: device.ComputePipelineBindGroupLayout(pipeline, 0), Entries: entries})
				switch {
				case sampled && (err == nil || !strings.Contains(err.Error(), "Float32Filterable")):
					t.Errorf("CreateBindGroup answered %v for an R32Float view, which a filterable layout refuses without Float32Filterable", err)
				case !sampled && err != nil:
					t.Errorf("CreateBindGroup answered %v for an R32Float view, which an unfilterable layout takes", err)
				}
			})
		})
	}
}

func TestAPassCommandOnAHandleHoldingNothingFailsFinishNamingIt(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)

	h.readDevice(func(device gfx.Device) {
		encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
		if err != nil {
			t.Errorf("CreateCommandEncoder answered %v", err)
			return
		}
		pass := encoder.BeginComputePass(gfx.ComputePassDescriptor{})
		pass.SetPipeline(gfx.ComputePipeline{})
		pass.Dispatch(1, 1, 1)
		pass.End()
		buffer, err := encoder.Finish()
		if err == nil {
			buffer.Release()
		}
		if err == nil || !strings.Contains(err.Error(), "holds no compute pipeline") {
			t.Errorf("Finish answered %v for a dispatch on the zero pipeline, not an error naming it", err)
		}
	})
}

func TestADeviceLossIsGfxsAndAnotherErrorIsAnsweredAsItIs(t *testing.T) {
	loss := fmt.Errorf("wgpu: submit failed: %w", wgpu.ErrDeviceLost)
	if err := gogpu.MarkDeviceLoss(loss); !errors.Is(err, gfx.ErrDeviceLost) || !errors.Is(err, loss) {
		t.Errorf("%v is answered as %v, not as gfx's ErrDeviceLost wrapping it", loss, err)
	}
	for _, other := range []error{nil, wgpu.ErrSurfaceLost, errors.New("gogpu: not a loss")} {
		if err := gogpu.MarkDeviceLoss(other); err != other {
			t.Errorf("%v is answered as %v, not as it is", other, err)
		}
	}
}

// triangleWGSL draws an orange triangle over a black window, from its vertex
// indices alone.
const triangleWGSL = `
@vertex
fn corner(@builtin(vertex_index) index: u32) -> @builtin(position) vec4f {
	let corners = array(vec2f(0, 0.8), vec2f(-0.8, -0.8), vec2f(0.8, -0.8));
	return vec4f(corners[index], 0, 1);
}

@fragment
fn paint() -> @location(0) vec4f {
	return vec4f(1, 0.5, 0, 1);
}
`

// coveringWGSL covers a 4-texel square target with the texel of binding 1
// that binding 2 samples at each pixel's center, from a vertex buffer of
// corners, as sampledLayout lays them out.
const coveringWGSL = `
@group(0) @binding(1) var picture: texture_2d<f32>;
@group(0) @binding(2) var picker: sampler;

@vertex
fn corner(@location(0) position: vec2f) -> @builtin(position) vec4f {
	return vec4f(position, 0, 1);
}

@fragment
fn paint(@builtin(position) position: vec4f) -> @location(0) vec4f {
	return textureSample(picture, picker, position.xy / 4);
}
`

// pickingWGSL picks a texel of binding 0 into binding 1: load loads it,
// and sample samples it with binding 2 through a function.
const pickingWGSL = `
@group(0) @binding(0) var picture: texture_2d<f32>;
@group(0) @binding(1) var<storage, read_write> picked: f32;
@group(0) @binding(2) var picker: sampler;

fn pick(image: texture_2d<f32>, picking: sampler) -> f32 {
	return textureSampleLevel(image, picking, vec2f(0), 0).r;
}

@compute @workgroup_size(1)
fn load() {
	picked = textureLoad(picture, vec2u(0), 0).r;
}

@compute @workgroup_size(1)
fn sample() {
	picked = pick(picture, picker);
}
`

// squaresWGSL writes the square of each invocation's index into binding 0.
const squaresWGSL = `
@group(0) @binding(0) var<storage, read_write> squares: array<u32>;

@compute @workgroup_size(4)
fn square(@builtin(global_invocation_id) id: vec3u) {
	squares[id.x] = id.x * id.x;
}
`

// createRenderPipeline creates a pipeline of wgsl's corner and paint, over a
// layout of groups, or the zero layout, reflected, when groups is nil,
// drawing triangles from buffers to a target of format.
func createRenderPipeline(t *testing.T, device gfx.Device, wgsl string, groups []gfx.BindGroupLayout, buffers []gfx.VertexBufferLayout, format gfx.TextureFormat) gfx.RenderPipeline {
	module, err := device.CreateShaderModule(gfx.ShaderModuleDescriptor{WGSL: wgsl})
	if err != nil {
		t.Errorf("CreateShaderModule answered %v", err)
	}
	defer device.ReleaseShaderModule(module)
	var layout gfx.PipelineLayout
	if groups != nil {
		if layout, err = device.CreatePipelineLayout(gfx.PipelineLayoutDescriptor{BindGroupLayouts: groups}); err != nil {
			t.Errorf("CreatePipelineLayout answered %v", err)
		}
		defer device.ReleasePipelineLayout(layout)
	}
	pipeline, err := device.CreateRenderPipeline(gfx.RenderPipelineDescriptor{
		Layout: layout,
		Vertex: gfx.VertexState{Module: module, EntryPoint: "corner", Buffers: buffers},
		Fragment: gfx.FragmentState{Module: module, EntryPoint: "paint", Targets: []gfx.ColorTargetState{{
			Format: format, WriteMask: gfx.ColorWriteFlagAll,
		}}},
	})
	if err != nil {
		t.Errorf("CreateRenderPipeline answered %v", err)
	}
	return pipeline
}

// sampledSide is the side, in texels, of the texture drawSampledTexture
// samples and of the target it draws.
const sampledSide = 4

// sampledTexels are the texels drawSampledTexture writes, each different.
var sampledTexels = func() []byte {
	texels := make([]byte, 4*sampledSide*sampledSide)
	for texel := range sampledSide * sampledSide {
		copy(texels[4*texel:], []byte{byte(16 * texel), byte(255 - 16*texel), byte(texel), 255})
	}
	return texels
}()

// drawSampledTexture writes sampledTexels into a texture, covers a target of
// the same size with it through coveringWGSL with draw, given an indirect
// buffer of a draw's arguments at 0 and an indexed draw's at 16, submits,
// copies the target into a buffer of 256-byte rows, and maps that for
// reading. The pipeline's layout is reflected when reflected is true,
// sampledLayout's otherwise.
func drawSampledTexture(t *testing.T, device gfx.Device, reflected bool, draw func(pass gfx.RenderPassEncoder, indirect gfx.Buffer)) gfx.Buffer {
	picture := createTexture(t, device, sampledSide)
	layout := gfx.ImageDataLayout{BytesPerRow: 4 * sampledSide, RowsPerImage: sampledSide}
	size := gfx.Extent3D{Width: sampledSide, Height: sampledSide, DepthOrArrayLayers: 1}
	if err := device.WriteTexture(gfx.ImageCopyTexture{Texture: picture}, sampledTexels, layout, size); err != nil {
		t.Errorf("WriteTexture answered %v", err)
	}
	pictureView, err := device.CreateTextureView(picture, gfx.TextureViewDescriptor{})
	if err != nil {
		t.Errorf("CreateTextureView answered %v", err)
	}
	sampler, err := device.CreateSampler(gfx.SamplerDescriptor{})
	if err != nil {
		t.Errorf("CreateSampler answered %v", err)
	}
	var groups []gfx.BindGroupLayout
	if !reflected {
		groupLayout, err := device.CreateBindGroupLayout(gfx.BindGroupLayoutDescriptor{Entries: sampledLayout.Entries[1:]})
		if err != nil {
			t.Errorf("CreateBindGroupLayout answered %v", err)
		}
		groups = []gfx.BindGroupLayout{groupLayout}
	}
	corner := gfx.VertexBufferLayout{ArrayStride: 8, Attributes: []gfx.VertexAttribute{{Format: gfx.VertexFormatFloat32x2}}}
	pipeline := createRenderPipeline(t, device, coveringWGSL, groups, []gfx.VertexBufferLayout{corner}, gfx.TextureFormatRGBA8Unorm)
	if reflected {
		groups = []gfx.BindGroupLayout{device.RenderPipelineBindGroupLayout(pipeline, 0)}
	}
	group, err := device.CreateBindGroup(gfx.BindGroupDescriptor{Layout: groups[0], Entries: []gfx.BindGroupEntry{
		{Binding: 1, TextureView: pictureView}, {Binding: 2, Sampler: sampler},
	}})
	if err != nil {
		t.Errorf("CreateBindGroup answered %v", err)
	}
	corners := createWrittenBuffer(t, device, gfx.BufferUsageFlagVertex, []float32{-1, -1, 3, -1, -1, 3})
	indices := createWrittenBuffer(t, device, gfx.BufferUsageFlagIndex, []uint32{0, 1, 2})
	indirect := createWrittenBuffer(t, device, gfx.BufferUsageFlagIndirect, []uint32{3, 1, 0, 0, 3, 1, 0, 0, 0})
	target := createTexture(t, device, sampledSide)
	targetView, err := device.CreateTextureView(target, gfx.TextureViewDescriptor{})
	if err != nil {
		t.Errorf("CreateTextureView answered %v", err)
	}
	readback, err := device.CreateBuffer(gfx.BufferDescriptor{Size: uint64(256 * sampledSide), Usage: gfx.BufferUsageFlagMapRead | gfx.BufferUsageFlagCopyDst})
	if err != nil {
		t.Errorf("CreateBuffer answered %v", err)
	}

	encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
	if err != nil {
		t.Errorf("CreateCommandEncoder answered %v", err)
		return readback
	}
	pass := encoder.BeginRenderPass(gfx.RenderPassDescriptor{ColorAttachments: []gfx.RenderPassColorAttachment{{
		View: targetView, LoadOp: gfx.LoadOpClear, StoreOp: gfx.StoreOpStore,
	}}})
	pass.SetPipeline(pipeline)
	pass.SetBindGroup(0, group, nil)
	pass.SetVertexBuffer(0, corners, 0)
	pass.SetIndexBuffer(indices, gfx.IndexFormatUint32, 0)
	draw(pass, indirect)
	pass.End()
	encoder.CopyTextureToBuffer(target, readback, []gfx.BufferTextureCopy{{
		BufferLayout: gfx.ImageDataLayout{BytesPerRow: 256, RowsPerImage: sampledSide},
		TextureBase:  gfx.ImageCopyTexture{Texture: target},
		Size:         size,
	}})
	submitAndMap(t, device, encoder, readback, uint64(256*sampledSide))
	return readback
}

// dispatchSquares dispatches squaresWGSL with dispatch, given an indirect
// buffer of 2, 1 and 1 workgroups, into a storage buffer of 8 squares,
// submits, copies it into a buffer, and maps that for reading. The
// pipeline's layout is reflected when reflected is true, explicit otherwise.
func dispatchSquares(t *testing.T, device gfx.Device, reflected bool, dispatch func(pass gfx.ComputePassEncoder, indirect gfx.Buffer)) gfx.Buffer {
	module, err := device.CreateShaderModule(gfx.ShaderModuleDescriptor{WGSL: squaresWGSL})
	if err != nil {
		t.Errorf("CreateShaderModule answered %v", err)
	}
	var layout gfx.PipelineLayout
	if !reflected {
		groupLayout, err := device.CreateBindGroupLayout(gfx.BindGroupLayoutDescriptor{Entries: []gfx.BindGroupLayoutEntry{{
			Binding: 0, Visibility: gfx.ShaderStageFlagCompute, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeStorage},
		}}})
		if err != nil {
			t.Errorf("CreateBindGroupLayout answered %v", err)
		}
		if layout, err = device.CreatePipelineLayout(gfx.PipelineLayoutDescriptor{BindGroupLayouts: []gfx.BindGroupLayout{groupLayout}}); err != nil {
			t.Errorf("CreatePipelineLayout answered %v", err)
		}
	}
	pipeline, err := device.CreateComputePipeline(gfx.ComputePipelineDescriptor{Layout: layout, Module: module, EntryPoint: "square"})
	if err != nil {
		t.Errorf("CreateComputePipeline answered %v", err)
	}
	squares, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 32, Usage: gfx.BufferUsageFlagStorage | gfx.BufferUsageFlagCopySrc})
	if err != nil {
		t.Errorf("CreateBuffer answered %v", err)
	}
	group, err := device.CreateBindGroup(gfx.BindGroupDescriptor{Layout: device.ComputePipelineBindGroupLayout(pipeline, 0), Entries: []gfx.BindGroupEntry{{Binding: 0, Buffer: squares}}})
	if err != nil {
		t.Errorf("CreateBindGroup answered %v", err)
	}
	indirect := createWrittenBuffer(t, device, gfx.BufferUsageFlagIndirect, []uint32{2, 1, 1})
	readback, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 32, Usage: gfx.BufferUsageFlagMapRead | gfx.BufferUsageFlagCopyDst})
	if err != nil {
		t.Errorf("CreateBuffer answered %v", err)
	}

	encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
	if err != nil {
		t.Errorf("CreateCommandEncoder answered %v", err)
		return readback
	}
	pass := encoder.BeginComputePass(gfx.ComputePassDescriptor{})
	pass.SetPipeline(pipeline)
	pass.SetBindGroup(0, group, nil)
	dispatch(pass, indirect)
	pass.End()
	encoder.CopyBufferToBuffer(squares, 0, readback, 0, 32)
	submitAndMap(t, device, encoder, readback, 32)
	return readback
}

// checkSampledTexture reads back readback, drawSampledTexture's, and checks
// that it holds sampledTexels row by row.
func checkSampledTexture(h *harness, readback gfx.Buffer) {
	h.t.Helper()
	read := h.readBack(readback, 256*sampledSide)
	for row := range sampledSide {
		drawn := read[256*row : 256*row+4*sampledSide]
		written := sampledTexels[4*sampledSide*row : 4*sampledSide*(row+1)]
		if !bytes.Equal(drawn, written) {
			h.t.Errorf("row %d was drawn % x, want the texels written, % x", row, drawn, written)
		}
	}
}

// checkSquares reads back readback, dispatchSquares', and checks that it
// holds the squares of 0 to 7.
func checkSquares(h *harness, readback gfx.Buffer) {
	h.t.Helper()
	read := h.readBack(readback, 32)
	want := make([]byte, 0, 32)
	for index := range uint32(8) {
		want = binary.LittleEndian.AppendUint32(want, index*index)
	}
	if !bytes.Equal(read, want) {
		h.t.Errorf("the dispatch wrote % x, want the squares of 0 to 7, % x", read, want)
	}
}

// createWrittenBuffer creates a buffer of usage, and CopyDst, holding
// values, a slice of fixed-size numbers, little-endian. The write lands at
// the next submit.
func createWrittenBuffer(t *testing.T, device gfx.Device, usage gfx.BufferUsageFlags, values any) gfx.Buffer {
	written, err := binary.Append(nil, binary.LittleEndian, values)
	if err != nil {
		t.Errorf("encoding %v answered %v", values, err)
	}
	buffer, err := device.CreateBuffer(gfx.BufferDescriptor{Size: uint64(len(written)), Usage: usage | gfx.BufferUsageFlagCopyDst})
	if err != nil {
		t.Errorf("CreateBuffer answered %v", err)
	}
	if err := device.WriteBuffer(buffer, 0, written); err != nil {
		t.Errorf("WriteBuffer answered %v", err)
	}
	return buffer
}

// submitAndMap finishes encoder, submits it and maps size bytes of readback
// for reading.
func submitAndMap(t *testing.T, device gfx.Device, encoder gfx.StandaloneCommandEncoder, readback gfx.Buffer, size uint64) {
	buffer, err := encoder.Finish()
	if err != nil {
		t.Errorf("Finish answered %v", err)
		return
	}
	if err := device.Submit(buffer); err != nil {
		t.Errorf("Submit answered %v", err)
	}
	if err := device.MapBuffer(readback, gfx.MapModeRead, 0, size); err != nil {
		t.Errorf("MapBuffer answered %v", err)
	}
}

// sampledWGSL samples binding 1 with binding 2, tinted by binding 0, as
// sampledLayout lays them out.
const sampledWGSL = `
@group(0) @binding(0) var<uniform> tint: vec4f;
@group(0) @binding(1) var picture: texture_2d<f32>;
@group(0) @binding(2) var picker: sampler;

@fragment
fn shade(@location(0) coordinates: vec2f) -> @location(0) vec4f {
	return tint * textureSample(picture, picker, coordinates);
}
`

// sampledLayout lays out sampledWGSL's group: a uniform buffer, a float
// texture and a filtering sampler.
var sampledLayout = gfx.BindGroupLayoutDescriptor{Entries: []gfx.BindGroupLayoutEntry{
	{Binding: 0, Visibility: gfx.ShaderStageFlagFragment, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform}},
	{Binding: 1, Visibility: gfx.ShaderStageFlagFragment, Texture: gfx.TextureBindingLayout{
		SampleType: gfx.TextureSampleTypeFloat, ViewDimension: gfx.TextureViewDimension2D,
	}},
	{Binding: 2, Visibility: gfx.ShaderStageFlagFragment, Sampler: gfx.SamplerBindingLayout{Type: gfx.SamplerBindingTypeFiltering}},
}}

// createSampledResources creates the buffer, texture view and sampler
// sampledLayout lays out, and answers their bind group entries and the
// function releasing them.
func createSampledResources(t *testing.T, device gfx.Device) ([]gfx.BindGroupEntry, func()) {
	buffer, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 16, Usage: gfx.BufferUsageFlagUniform})
	if err != nil {
		t.Errorf("CreateBuffer answered %v", err)
	}
	texture := createTexture(t, device, 4)
	view, err := device.CreateTextureView(texture, gfx.TextureViewDescriptor{})
	if err != nil {
		t.Errorf("CreateTextureView answered %v", err)
	}
	sampler, err := device.CreateSampler(gfx.SamplerDescriptor{})
	if err != nil {
		t.Errorf("CreateSampler answered %v", err)
	}
	entries := []gfx.BindGroupEntry{{Binding: 0, Buffer: buffer}, {Binding: 1, TextureView: view}, {Binding: 2, Sampler: sampler}}
	return entries, func() {
		device.ReleaseSampler(sampler)
		device.ReleaseTextureView(view)
		device.ReleaseTexture(texture)
		device.ReleaseBuffer(buffer)
	}
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

// mapWrittenBuffer creates a MapRead buffer, writes written into it, lands the
// write with an empty submit, as gfx's writes land at the next submit, and
// maps it for reading.
func mapWrittenBuffer(t *testing.T, device gfx.Device, written []byte) gfx.Buffer {
	size := uint64(len(written))
	buffer, err := device.CreateBuffer(gfx.BufferDescriptor{Size: size, Usage: gfx.BufferUsageFlagMapRead | gfx.BufferUsageFlagCopyDst})
	if err != nil {
		t.Errorf("CreateBuffer answered %v", err)
		return buffer
	}
	if err := device.WriteBuffer(buffer, 0, written); err != nil {
		t.Errorf("WriteBuffer answered %v", err)
	}
	if err := device.Submit(); err != nil {
		t.Errorf("Submit answered %v", err)
	}
	if err := device.MapBuffer(buffer, gfx.MapModeRead, 0, size); err != nil {
		t.Errorf("MapBuffer answered %v", err)
	}
	return buffer
}

// createTexture creates an RGBA8Unorm texture side texels square, which can
// be written, sampled, drawn to and copied from.
func createTexture(t *testing.T, device gfx.Device, side uint32) gfx.Texture {
	texture, err := device.CreateTexture(gfx.TextureDescriptor{
		Size:   gfx.Extent3D{Width: side, Height: side, DepthOrArrayLayers: 1},
		Format: gfx.TextureFormatRGBA8Unorm,
		Usage: gfx.TextureUsageFlagCopyDst | gfx.TextureUsageFlagCopySrc | gfx.TextureUsageFlagTextureBinding |
			gfx.TextureUsageFlagRenderAttachment,
	})
	if err != nil {
		t.Errorf("CreateTexture answered %v", err)
	}
	return texture
}

// writeWholeTexture writes every texel of an RGBA8 texture side texels
// square, and answers WriteTexture's error.
func writeWholeTexture(device gfx.Device, texture gfx.Texture, side uint32) error {
	texels := bytes.Repeat([]byte{0xff, 0, 0xff, 0xff}, int(side*side))
	layout := gfx.ImageDataLayout{BytesPerRow: 4 * side, RowsPerImage: side}
	return device.WriteTexture(gfx.ImageCopyTexture{Texture: texture}, texels, layout, gfx.Extent3D{Width: side, Height: side, DepthOrArrayLayers: 1})
}
