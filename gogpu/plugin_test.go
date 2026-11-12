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
	host "github.com/gogpu/gogpu"
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
// then ProcessEvents, ticking the main thread until both have run. It skips
// the test where the system refused a window because it opens one, as
// Wayland does, dropping that error.
func (h *harness) pump(edit func(manager *platform.WindowManager)) {
	h.t.Helper()
	h.edit = edit
	publishAndWait(h, pumpEvent{})
	h.mutex.Lock()
	defer h.mutex.Unlock()
	loggedBefore := len(h.logged)
	h.logged = slices.DeleteFunc(h.logged, func(err error) bool {
		return strings.Contains(err.Error(), "one window is supported")
	})
	if len(h.logged) < loggedBefore {
		h.t.Skip("the system opens one window, and a second was asked for")
	}
}

// draw publishes a beat of the render track, ticking the main thread until
// its frame has presented, with readFrame run over the Device after the
// frame's acquire.
func (h *harness) draw(readFrame func(event gfx.DrawEvent, device gfx.Device)) {
	h.t.Helper()
	h.readFrame = readFrame
	publishAndWait(h, gfx.RenderEvent{})
}

// openShownWindow opens testWindow shown, and draws a frame of it.
func (h *harness) openShownWindow() platform.Window {
	h.t.Helper()
	shown := testWindow
	shown.Hidden = false
	var window platform.Window
	h.pump(func(manager *platform.WindowManager) { window = manager.Open(shown) })
	h.drawFrame(window, nil)
	return window
}

// drawFrame pumps and draws beats, as an application's beats do, until one
// has window's framebuffer, runs readFrame, unless nil, over that beat
// alone, and fails the test when none has in 2s. A window shown gets its
// first framebuffer from a later beat, since its surface is created by a job
// the pump does not wait for, and a Wayland window gets none until a pump
// has read the compositor's frame callback for its last present, which
// comes a beat or more after it.
func (h *harness) drawFrame(window platform.Window, readFrame func(event gfx.DrawEvent, device gfx.Device)) {
	h.t.Helper()
	framed := false
	draw := func(event gfx.DrawEvent, device gfx.Device) {
		framed = device.Framebuffer(event.Frame, window) != gfx.TextureView{}
		if framed && readFrame != nil {
			readFrame(event, device)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for !framed && time.Now().Before(deadline) {
		h.pump(nil)
		h.draw(draw)
	}
	if !framed {
		h.t.Fatalf("%v had no framebuffer in 2s", window)
	}
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
// capture holds none. A capture whose beat did not acquire the window, as
// Wayland's beats waiting on the compositor do not, is requested again.
func (h *harness) capture(window platform.Window, color gfx.Color) *image.NRGBA {
	h.t.Helper()
	request := func(device gfx.Device) {
		if err := device.RequestCapture(window); err != nil {
			h.t.Errorf("RequestCapture answered %v", err)
		}
	}
	h.readDevice(request)
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		h.pump(nil)
		h.draw(func(event gfx.DrawEvent, device gfx.Device) { clearFramebuffer(h.t, event, device, window, color) })
		for _, event := range h.takePublished() {
			captured, ok := event.(capturedPicture)
			if !ok {
				continue
			}
			if captured.status == gfx.CaptureStatusNotAcquired {
				h.readDevice(request)
				continue
			}
			if captured.err != nil {
				h.t.Fatalf("reading %v's capture answered %v", window, captured.err)
			}
			return captured.picture
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

// awaitCursorLock pumps until window's cursor mode in force is Locked, and
// fails the test when it is not in 2s. On Wayland, whose compositor puts a
// lock in force only while the pointer is over the window, where a test
// cannot move it, it skips the test when the pointer is not there.
func (h *harness) awaitCursorLock(window platform.Window) {
	h.t.Helper()
	if runtime.GOOS == "linux" && os.Getenv("WAYLAND_DISPLAY") != "" {
		pointerInside := false
		h.readInputState(func(input *platform.InputState) { pointerInside = input.IsPointerInside(window) })
		if !pointerInside {
			h.t.Skip("Wayland puts a lock in force only while the pointer is over the window, and it is not")
		}
	}
	if !h.pumpUntil(func(manager *platform.WindowManager) bool {
		return manager.EffectiveCursorMode(window) == platform.CursorModeLocked
	}) {
		h.t.Fatalf("%v's lock never came in force while it has the focus", window)
	}
}

// takeLogged answers the errors logged since its last call, which then fail
// the test no more.
func (h *harness) takeLogged() []error {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	logged := h.logged
	h.logged = nil
	return logged
}

// pumpUntil pumps until done, run over the WindowManager as each pump's
// edit, answers true, which it then answers, and answers false when it has
// not in 2s. Each check sees what the pumps before it reported: the system
// reports a focus change, and so the cursor mode in force, a pump or more
// after it.
func (h *harness) pumpUntil(done func(manager *platform.WindowManager) bool) bool {
	h.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	reached := false
	for !reached && time.Now().Before(deadline) {
		h.pump(func(manager *platform.WindowManager) { reached = done(manager) })
	}
	return reached
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
		h.published = append(h.published, capturedPicture{status: event.Status, picture: picture, err: err})
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

// capturedPicture is a CapturedEvent's status and what ReadCapture answered
// for it, read in a task its publication joins, before gfx releases its
// buffer.
type capturedPicture struct {
	status  gfx.CaptureStatus
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

func TestALockedCursorsMoveKeepsThePointerInPlaceAndGivesItsDelta(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	window := h.openShownWindow()
	h.pump(func(manager *platform.WindowManager) { manager.SetCursorMode(window, platform.CursorModeLocked) })
	if !h.pumpUntil(func(manager *platform.WindowManager) bool { return manager.IsFocused(window) }) {
		t.Skip("the shown window never took the focus")
	}
	h.awaitCursorLock(window)
	h.takePublished()

	// gogpu reports a locked move at the window's centre, the mouse's
	// motion in its delta.
	onMainThread(func() {
		gogpu.ReportPointer(h.plugin, window, gpucontext.PointerEvent{Type: gpucontext.PointerEnter, X: 50, Y: 60})
		gogpu.ReportPointer(h.plugin, window, gpucontext.PointerEvent{Type: gpucontext.PointerMove, X: 160, Y: 120, DeltaX: 3, DeltaY: -4})
	})
	h.pump(nil)

	want := []any{platform.PointerMoveEvent{Window: window, Position: platform.Point{X: 50, Y: 60}, Delta: platform.Point{X: 3, Y: -4}}}
	moves := slices.DeleteFunc(h.takePublished(), func(event any) bool {
		_, isMove := event.(platform.PointerMoveEvent)
		return !isMove
	})
	if !slices.Equal(moves, want) {
		t.Errorf("the locked move published %v, want %v", moves, want)
	}
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
		gogpu.ReportPointer(h.plugin, window, gpucontext.PointerEvent{Type: gpucontext.PointerDown, Button: gpucontext.ButtonEraser})
		gogpu.ReportPointer(h.plugin, window, gpucontext.PointerEvent{Type: gpucontext.PointerDown, Button: gpucontext.ButtonNone})
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

func TestEachKeyIsReportedAsCogsKeyOfTheSameName(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	var window platform.Window
	h.pump(func(manager *platform.WindowManager) { window = manager.Open(testWindow) })
	h.takePublished()

	var reported []gpucontext.Key
	var want []any
	onMainThread(func() {
		for key := range gpucontext.Key(512) {
			wantKey, _ := platform.ParseKey("Key" + key.String())
			gogpu.ReportKey(h.plugin, window, key, 0, true)
			gogpu.ReportKey(h.plugin, window, key, 0, false)
			reported = append(reported, key)
			want = append(want, platform.KeyEvent{Window: window, Key: wantKey, Pressed: true}, platform.KeyEvent{Window: window, Key: wantKey})
		}
	})
	h.pump(nil)

	published := h.takePublished()
	if len(published) != len(want) {
		t.Fatalf("%d keys pressed and released published %d events, want %d", len(reported), len(published), len(want))
	}
	for index := range want {
		if published[index] != want[index] {
			t.Errorf("gogpu's %v published %v, want %v", reported[index/2], published[index], want[index])
		}
	}
}

func TestAKeyPressedAgainWhileHeldIsARepeat(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	var window platform.Window
	h.pump(func(manager *platform.WindowManager) { window = manager.Open(testWindow) })
	h.takePublished()
	modifiers := gpucontext.ModShift | gpucontext.ModCapsLock
	pressed := platform.KeyEvent{Window: window, Key: platform.KeyW, Pressed: true, Modifiers: platform.ModifierShift | platform.ModifierCapsLock}
	repeated := pressed
	repeated.Repeat = true
	released := pressed
	released.Pressed = false

	onMainThread(func() {
		gogpu.ReportKey(h.plugin, window, gpucontext.KeyW, modifiers, true)
		gogpu.ReportKey(h.plugin, window, gpucontext.KeyW, modifiers, true)
	})
	h.pump(nil)

	want := []any{pressed, repeated}
	if published := h.takePublished(); !slices.Equal(published, want) {
		t.Errorf("a press and a repeat published %v, want %v", published, want)
	}
	h.readInputState(func(input *platform.InputState) {
		if !input.IsHeld(platform.KeyW) || !input.JustPressed(platform.KeyW) {
			t.Errorf("KeyW is held %v and just pressed %v, want both", input.IsHeld(platform.KeyW), input.JustPressed(platform.KeyW))
		}
	})

	onMainThread(func() {
		gogpu.ReportKey(h.plugin, window, gpucontext.KeyW, modifiers, true)
		gogpu.ReportKey(h.plugin, window, gpucontext.KeyW, modifiers, false)
		gogpu.ReportKey(h.plugin, window, gpucontext.KeyW, modifiers, true)
	})
	h.pump(nil)

	want = []any{repeated, released, pressed}
	if published := h.takePublished(); !slices.Equal(published, want) {
		t.Errorf("a repeat, a release and a press published %v, want %v", published, want)
	}
}

func TestAKeyHeldAtAFocusLossIsPressedAfreshAfterIt(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	var window platform.Window
	h.pump(func(manager *platform.WindowManager) { window = manager.Open(testWindow) })
	onMainThread(func() { gogpu.ReportKey(h.plugin, window, gpucontext.KeyW, 0, true) })
	h.pump(nil)
	onMainThread(func() { gogpu.ReportFocus(h.plugin, window, false) })
	h.pump(nil)
	h.takePublished()

	onMainThread(func() { gogpu.ReportKey(h.plugin, window, gpucontext.KeyW, 0, true) })
	h.pump(nil)

	want := []any{platform.KeyEvent{Window: window, Key: platform.KeyW, Pressed: true}}
	if published := h.takePublished(); !slices.Equal(published, want) {
		t.Errorf("the press after the focus loss published %v, want %v", published, want)
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

func TestAShownWindowGetsAFramebufferAgainAfterEachPresent(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	window := h.openShownWindow()

	for range 3 {
		h.drawFrame(window, nil)
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
	h.drawFrame(second, nil)
	h.pump(func(manager *platform.WindowManager) { manager.SetHidden(first, false) })
	h.drawFrame(first, nil)
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

func TestALockedCursorReadsNormalWhileItsWindowLacksTheFocusAndLockedAfter(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	locked := h.openShownWindow()
	h.pump(func(manager *platform.WindowManager) { manager.SetCursorMode(locked, platform.CursorModeLocked) })
	if !h.pumpUntil(func(manager *platform.WindowManager) bool { return manager.IsFocused(locked) }) {
		t.Skip("the shown window never took the focus")
	}

	checkCursorMode := func(want platform.CursorMode, while string) {
		t.Helper()
		var mode platform.CursorMode
		if !h.pumpUntil(func(manager *platform.WindowManager) bool {
			mode = manager.EffectiveCursorMode(locked)
			return mode == want
		}) {
			t.Errorf("the locked window's cursor mode in force is %v %s, want %v", mode, while, want)
		}
	}
	h.awaitCursorLock(locked)
	h.openShownWindow()
	checkCursorMode(platform.CursorModeNormal, "while another window has the focus")
	h.pump(func(manager *platform.WindowManager) { manager.SetHidden(locked, true) })
	h.pump(func(manager *platform.WindowManager) { manager.SetHidden(locked, false) })
	checkCursorMode(platform.CursorModeLocked, "once shown again, which gives it the focus")
}

func TestTheCursorsShapeAndHidingReachAWindowWithoutAnError(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	var window platform.Window
	h.pump(func(manager *platform.WindowManager) { window = manager.Open(testWindow) })

	h.pump(func(manager *platform.WindowManager) {
		manager.SetCursorShape(window, platform.CursorShapeText)
		manager.SetCursorHidden(window, true)
	})
	h.pump(func(manager *platform.WindowManager) { manager.SetCursorHidden(window, false) })
}

func TestTheCursorMovesWhereAskedOrTheSystemsRefusalIsLogged(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	window := h.openShownWindow()
	position := platform.Point{X: 10, Y: 20}
	// A cursor moved in from outside the window enters it, and macOS reports
	// the enter a pump or more later, a few points off. So the cursor is
	// moved in first, and the pointer waited for inside.
	inside := platform.Point{X: 100, Y: 100}
	h.pump(func(manager *platform.WindowManager) { manager.SetCursorPosition(window, inside) })
	entered := false
	for deadline := time.Now().Add(2 * time.Second); !entered && time.Now().Before(deadline); {
		h.pump(nil)
		h.readInputState(func(input *platform.InputState) { entered = input.IsPointerInside(window) })
	}
	h.takePublished()

	h.pump(func(manager *platform.WindowManager) { manager.SetCursorPosition(window, position) })

	logged := h.takeLogged()
	h.readInputState(func(input *platform.InputState) {
		placed := input.PointerPosition(window) == position
		if len(logged) == 0 && !placed {
			t.Errorf("the pointer is at %v after the cursor moved to %v, want there", input.PointerPosition(window), position)
		}
		if len(logged) > 0 && placed {
			t.Errorf("the pointer was placed at %v although moving the cursor logged %v", position, logged)
		}
	})
	for _, err := range logged {
		if !errors.Is(err, host.ErrCursorPositionUnsupported) {
			t.Errorf("moving the cursor logged %v, want the system's refusal, %v", err, host.ErrCursorPositionUnsupported)
		}
	}
}

func TestEachFrameClearsAShownWindowToThePassColor(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	window := h.openShownWindow()

	for range 30 {
		h.drawFrame(window, func(event gfx.DrawEvent, device gfx.Device) {
			clearFramebuffer(t, event, device, window, gfx.Color{R: 1, B: 1, A: 1})
		})
	}
}

func TestAFramesSubmitIsSeenFinishedByALaterPoll(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)
	window := h.openShownWindow()

	var latency time.Duration
	for frame := 0; frame < 10 && latency == 0; frame++ {
		h.drawFrame(window, func(event gfx.DrawEvent, device gfx.Device) {
			latency = event.GPULatency
			clearFramebuffer(t, event, device, window, gfx.Color{A: 1})
		})
	}
	if latency <= 0 {
		t.Error("no DrawEvent in 10 frames carried a GPU latency, want a frame's submit seen finished")
	}
}

func TestTheSoftwareAdapterClearsAWindow(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(&gfx.Config{ForceFallbackAdapter: true}, nil)
	window := h.openShownWindow()

	for range 3 {
		h.drawFrame(window, func(event gfx.DrawEvent, device gfx.Device) {
			clearFramebuffer(t, event, device, window, gfx.Color{G: 1, A: 1})
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
				h.drawFrame(window, func(event gfx.DrawEvent, device gfx.Device) {
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
		pipeline = createRenderPipeline(t, device, triangleWGSL, []gfx.BindGroupLayout{}, nil, gfx.TextureFormatBGRA8Unorm, nil)
	})

	for range 30 {
		h.drawFrame(window, func(event gfx.DrawEvent, device gfx.Device) {
			view := device.Framebuffer(event.Frame, window)
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
			checkTexture(h, readback, sampledTexels)
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

func TestEachCopyIntoATextureReadsBackTheTexelsCopied(t *testing.T) {
	copies := map[string]func(t *testing.T, device gfx.Device, encoder gfx.CommandEncoder, destination gfx.Texture){
		"CopyBufferToTexture": func(t *testing.T, device gfx.Device, encoder gfx.CommandEncoder, destination gfx.Texture) {
			rows := make([]byte, 256*sampledSide)
			for row := range sampledSide {
				copy(rows[256*row:], sampledTexels[4*sampledSide*row:4*sampledSide*(row+1)])
			}
			source := createWrittenBuffer(t, device, gfx.BufferUsageFlagCopySrc, rows)
			encoder.CopyBufferToTexture(source, destination, []gfx.BufferTextureCopy{{
				BufferLayout: gfx.ImageDataLayout{BytesPerRow: 256, RowsPerImage: sampledSide},
				TextureBase:  gfx.ImageCopyTexture{Texture: destination},
				Size:         sampledSize,
			}})
		},
		"CopyTextureToTexture": func(t *testing.T, device gfx.Device, encoder gfx.CommandEncoder, destination gfx.Texture) {
			source := createSampledTexture(t, device)
			encoder.CopyTextureToTexture(source, destination, []gfx.TextureCopy{{
				Source: gfx.ImageCopyTexture{Texture: source}, Destination: gfx.ImageCopyTexture{Texture: destination}, Size: sampledSize,
			}})
		},
	}
	for name, copyInto := range copies {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.mustSpawn(nil, nil)

			var readback gfx.Buffer
			h.readDevice(func(device gfx.Device) {
				destination := createTexture(t, device, sampledSide)
				encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
				if err != nil {
					t.Errorf("CreateCommandEncoder answered %v", err)
					return
				}
				copyInto(t, device, encoder, destination)
				readback = readTexture(t, device, encoder, destination)
			})
			checkTexture(h, readback, sampledTexels)
		})
	}
}

func TestAClearedRangeOfABufferReadsBackAsZeroes(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)

	var readback gfx.Buffer
	h.readDevice(func(device gfx.Device) {
		cleared := createWrittenBuffer(t, device, gfx.BufferUsageFlagCopySrc, []byte("sixteen bytes in"))
		encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
		if err != nil {
			t.Errorf("CreateCommandEncoder answered %v", err)
			return
		}
		encoder.ClearBuffer(cleared, 4, 8)
		readback = readBuffer(t, device, encoder, cleared, 16)
	})
	if read, want := h.readBack(readback, 16), []byte("sixt\x00\x00\x00\x00\x00\x00\x00\x00s in"); !bytes.Equal(read, want) {
		t.Errorf("the buffer read %q, want %q", read, want)
	}
}

func TestEachPassStateReachesTheDrawAfterIt(t *testing.T) {
	whiteTexel := []byte{255, 255, 255, 255}
	equalStencilFace := gfx.StencilFaceState{Compare: gfx.CompareFunctionEqual}
	states := map[string]struct {
		blend                       gfx.BlendState
		depthStencil                gfx.DepthStencilState
		set                         func(pass gfx.RenderPassEncoder)
		paintedTexel                []byte
		paintedWidth, paintedHeight int
	}{
		"SetViewport": {
			set: func(pass gfx.RenderPassEncoder) {
				pass.SetViewport(gfx.Viewport{Width: 2, Height: sampledSide, MaxDepth: 1})
			},
			paintedTexel: whiteTexel, paintedWidth: 2, paintedHeight: sampledSide,
		},
		"SetScissorRect": {
			set:          func(pass gfx.RenderPassEncoder) { pass.SetScissorRect(gfx.ScissorRect{Width: sampledSide, Height: 2}) },
			paintedTexel: whiteTexel, paintedWidth: sampledSide, paintedHeight: 2,
		},
		"SetBlendConstant": {
			blend: gfx.BlendState{
				Color: gfx.BlendComponent{SrcFactor: gfx.BlendFactorConstant, DstFactor: gfx.BlendFactorZero, Operation: gfx.BlendOperationAdd},
				Alpha: gfx.BlendComponent{SrcFactor: gfx.BlendFactorOne, DstFactor: gfx.BlendFactorZero, Operation: gfx.BlendOperationAdd},
			},
			set:          func(pass gfx.RenderPassEncoder) { pass.SetBlendConstant(gfx.Color{R: 0.2, G: 0.4, B: 0.6, A: 1}) },
			paintedTexel: []byte{51, 102, 153, 255}, paintedWidth: sampledSide, paintedHeight: sampledSide,
		},
		"SetStencilReference": {
			depthStencil: gfx.DepthStencilState{
				Format: gfx.TextureFormatDepth24PlusStencil8, DepthCompare: gfx.CompareFunctionAlways,
				StencilFront: equalStencilFace, StencilBack: equalStencilFace, StencilReadMask: 0xff,
			},
			set:          func(pass gfx.RenderPassEncoder) { pass.SetStencilReference(clearedStencil) },
			paintedTexel: whiteTexel, paintedWidth: sampledSide, paintedHeight: sampledSide,
		},
	}
	for name, state := range states {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.mustSpawn(nil, nil)

			var readback gfx.Buffer
			h.readDevice(func(device gfx.Device) { readback = paintTarget(t, device, state.blend, state.depthStencil, state.set) })
			texels := make([]byte, 4*sampledSide*sampledSide)
			for y := range state.paintedHeight {
				for x := range state.paintedWidth {
					copy(texels[4*(sampledSide*y+x):], state.paintedTexel)
				}
			}
			checkTexture(h, readback, texels)
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
		checkTexture(h, readback, sampledTexels)
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

func TestEachBindingIsReflectedAsTheEntryWebGPUsAutoLayoutHolds(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)

	h.readDevice(func(device gfx.Device) {
		module, err := device.CreateShaderModule(gfx.ShaderModuleDescriptor{WGSL: bindingsWGSL})
		if err != nil {
			t.Errorf("CreateShaderModule answered %v", err)
		}
		pipeline, err := device.CreateComputePipeline(gfx.ComputePipelineDescriptor{Module: module, EntryPoint: "mix"})
		if err != nil {
			t.Errorf("CreateComputePipeline answered %v", err)
		}
		createView := func(format gfx.TextureFormat, usage gfx.TextureUsageFlags) gfx.TextureView {
			texture, err := device.CreateTexture(gfx.TextureDescriptor{Size: gfx.Extent3D{Width: 1, Height: 1, DepthOrArrayLayers: 1}, Format: format, Usage: usage})
			if err != nil {
				t.Errorf("CreateTexture answered %v for %v", err, format)
			}
			view, err := device.CreateTextureView(texture, gfx.TextureViewDescriptor{})
			if err != nil {
				t.Errorf("CreateTextureView answered %v for %v", err, format)
			}
			return view
		}
		comparing, err := device.CreateSampler(gfx.SamplerDescriptor{Compare: gfx.CompareFunctionLess})
		if err != nil {
			t.Errorf("CreateSampler answered %v", err)
		}
		skipped := createBindGroup(t, device, nil, nil)
		used := createBindGroup(t, device, bindingsLayout, []gfx.BindGroupEntry{
			{Binding: 0, Buffer: createWrittenBuffer(t, device, gfx.BufferUsageFlagUniform, []float32{2, 0, 0, 0})},
			{Binding: 1, Buffer: createWrittenBuffer(t, device, gfx.BufferUsageFlagStorage, []float32{3})},
			{Binding: 2, Buffer: createWrittenBuffer(t, device, gfx.BufferUsageFlagStorage, []float32{0})},
			{Binding: 3, TextureView: createView(gfx.TextureFormatDepth32Float, gfx.TextureUsageFlagTextureBinding)},
			{Binding: 4, Sampler: comparing},
			{Binding: 5, TextureView: createView(gfx.TextureFormatR32Float, gfx.TextureUsageFlagStorageBinding)},
		})

		encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
		if err != nil {
			t.Errorf("CreateCommandEncoder answered %v", err)
			return
		}
		pass := encoder.BeginComputePass(gfx.ComputePassDescriptor{})
		pass.SetPipeline(pipeline)
		pass.SetBindGroup(0, skipped, nil)
		pass.SetBindGroup(1, used, nil)
		pass.Dispatch(1, 1, 1)
		pass.End()
		if _, err := encoder.Finish(); err != nil {
			t.Errorf("Finish answered %v for a dispatch whose groups are of an empty layout and of bindingsLayout", err)
		}
	})
}

func TestABindingBothStagesUseIsVisibleToBoth(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)

	h.readDevice(func(device gfx.Device) {
		pipeline := createRenderPipeline(t, device, sharedWGSL, nil, nil, gfx.TextureFormatRGBA8Unorm, nil)
		group := createBindGroup(t, device, []gfx.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: gfx.ShaderStageFlagVertex | gfx.ShaderStageFlagFragment,
			Buffer:     gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, MinBindingSize: 16},
		}}, []gfx.BindGroupEntry{{Binding: 0, Buffer: createWrittenBuffer(t, device, gfx.BufferUsageFlagUniform, []float32{1, 0.5, 0, 1})}})
		target, err := device.CreateTextureView(createTexture(t, device, 1), gfx.TextureViewDescriptor{})
		if err != nil {
			t.Errorf("CreateTextureView answered %v", err)
		}

		encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
		if err != nil {
			t.Errorf("CreateCommandEncoder answered %v", err)
			return
		}
		pass := encoder.BeginRenderPass(gfx.RenderPassDescriptor{ColorAttachments: []gfx.RenderPassColorAttachment{{
			View: target, LoadOp: gfx.LoadOpClear, StoreOp: gfx.StoreOpStore,
		}}})
		pass.SetPipeline(pipeline)
		pass.SetBindGroup(0, group, nil)
		pass.Draw(gfx.DrawArgs{VertexCount: 3, InstanceCount: 1})
		pass.End()
		if _, err := encoder.Finish(); err != nil {
			t.Errorf("Finish answered %v for a draw whose group is visible to the vertex and fragment stages", err)
		}
	})
}

func TestAnEmptyEntryPointIsTheModulesOneOfItsStage(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil, nil)

	h.readDevice(func(device gfx.Device) {
		triangle, err := device.CreateShaderModule(gfx.ShaderModuleDescriptor{WGSL: triangleWGSL})
		if err != nil {
			t.Errorf("CreateShaderModule answered %v", err)
		}
		_, err = device.CreateRenderPipeline(gfx.RenderPipelineDescriptor{
			Vertex: gfx.VertexState{Module: triangle},
			Fragment: gfx.FragmentState{Module: triangle, Targets: []gfx.ColorTargetState{{
				Format: gfx.TextureFormatRGBA8Unorm, WriteMask: gfx.ColorWriteFlagAll,
			}}},
		})
		if err != nil {
			t.Errorf("CreateRenderPipeline answered %v for empty entry points of a module of one vertex and one fragment entry point", err)
		}
		squares, err := device.CreateShaderModule(gfx.ShaderModuleDescriptor{WGSL: squaresWGSL})
		if err != nil {
			t.Errorf("CreateShaderModule answered %v", err)
		}
		if _, err := device.CreateComputePipeline(gfx.ComputePipelineDescriptor{Module: squares}); err != nil {
			t.Errorf("CreateComputePipeline answered %v for an empty entry point of a module of one compute entry point", err)
		}
	})
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

// paintingWGSL paints white over every pixel of its target.
const paintingWGSL = `
@vertex
fn corner(@builtin(vertex_index) index: u32) -> @builtin(position) vec4f {
	let corners = array(vec2f(-1, -1), vec2f(3, -1), vec2f(-1, 3));
	return vec4f(corners[index], 0, 1);
}

@fragment
fn paint() -> @location(0) vec4f {
	return vec4f(1);
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

// bindingsWGSL uses a binding of each kind WebGPU's "auto" layout tells
// apart, all in group 1, group 0 skipped.
const bindingsWGSL = `
@group(1) @binding(0) var<uniform> scale: vec4f;
@group(1) @binding(1) var<storage, read> source: array<f32>;
@group(1) @binding(2) var<storage, read_write> destination: array<f32>;
@group(1) @binding(3) var depth: texture_depth_2d;
@group(1) @binding(4) var comparing: sampler_comparison;
@group(1) @binding(5) var written: texture_storage_2d<r32float, write>;

@compute @workgroup_size(1)
fn mix() {
	let compared = textureSampleCompareLevel(depth, comparing, vec2f(0.5), 0.5);
	destination[0] = source[0] * scale.x + compared;
	textureStore(written, vec2u(0), vec4f(compared));
}
`

// bindingsLayout is the layout WebGPU's "auto" builds of bindingsWGSL's
// group 1, each binding at its type's minimum size.
var bindingsLayout = []gfx.BindGroupLayoutEntry{
	{Binding: 0, Visibility: gfx.ShaderStageFlagCompute, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, MinBindingSize: 16}},
	{Binding: 1, Visibility: gfx.ShaderStageFlagCompute, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeReadOnlyStorage, MinBindingSize: 4}},
	{Binding: 2, Visibility: gfx.ShaderStageFlagCompute, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeStorage, MinBindingSize: 4}},
	{Binding: 3, Visibility: gfx.ShaderStageFlagCompute, Texture: gfx.TextureBindingLayout{SampleType: gfx.TextureSampleTypeDepth, ViewDimension: gfx.TextureViewDimension2D}},
	{Binding: 4, Visibility: gfx.ShaderStageFlagCompute, Sampler: gfx.SamplerBindingLayout{Type: gfx.SamplerBindingTypeComparison}},
	{Binding: 5, Visibility: gfx.ShaderStageFlagCompute, StorageTexture: gfx.StorageTextureBindingLayout{
		Access: gfx.StorageTextureAccessWriteOnly, Format: gfx.TextureFormatR32Float, ViewDimension: gfx.TextureViewDimension2D,
	}},
}

// sharedWGSL reads binding 0 from both its stages.
const sharedWGSL = `
@group(0) @binding(0) var<uniform> tint: vec4f;

@vertex
fn corner(@builtin(vertex_index) index: u32) -> @builtin(position) vec4f {
	let corners = array(vec2f(-1, -1), vec2f(3, -1), vec2f(-1, 3));
	return vec4f(corners[index] * tint.a, 0, 1);
}

@fragment
fn paint() -> @location(0) vec4f {
	return tint;
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
// drawing triangles from buffers to a target of format, its descriptor
// adjusted by adjust unless nil.
func createRenderPipeline(t *testing.T, device gfx.Device, wgsl string, groups []gfx.BindGroupLayout, buffers []gfx.VertexBufferLayout, format gfx.TextureFormat, adjust func(descriptor *gfx.RenderPipelineDescriptor)) gfx.RenderPipeline {
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
	descriptor := gfx.RenderPipelineDescriptor{
		Layout: layout,
		Vertex: gfx.VertexState{Module: module, EntryPoint: "corner", Buffers: buffers},
		Fragment: gfx.FragmentState{Module: module, EntryPoint: "paint", Targets: []gfx.ColorTargetState{{
			Format: format, WriteMask: gfx.ColorWriteFlagAll,
		}}},
	}
	if adjust != nil {
		adjust(&descriptor)
	}
	pipeline, err := device.CreateRenderPipeline(descriptor)
	if err != nil {
		t.Errorf("CreateRenderPipeline answered %v", err)
	}
	return pipeline
}

// sampledSide is the side, in texels, of the texture drawSampledTexture
// samples and of the target it draws.
const sampledSide = 4

// sampledSize is the size of a texture sampledSide texels square.
var sampledSize = gfx.Extent3D{Width: sampledSide, Height: sampledSide, DepthOrArrayLayers: 1}

// sampledTexels are the texels createSampledTexture writes, each different.
var sampledTexels = func() []byte {
	texels := make([]byte, 4*sampledSide*sampledSide)
	for texel := range sampledSide * sampledSide {
		copy(texels[4*texel:], []byte{byte(16 * texel), byte(255 - 16*texel), byte(texel), 255})
	}
	return texels
}()

// createSampledTexture creates a texture sampledSide texels square holding
// sampledTexels, written at the next submit.
func createSampledTexture(t *testing.T, device gfx.Device) gfx.Texture {
	texture := createTexture(t, device, sampledSide)
	layout := gfx.ImageDataLayout{BytesPerRow: 4 * sampledSide, RowsPerImage: sampledSide}
	if err := device.WriteTexture(gfx.ImageCopyTexture{Texture: texture}, sampledTexels, layout, sampledSize); err != nil {
		t.Errorf("WriteTexture answered %v", err)
	}
	return texture
}

// drawSampledTexture writes sampledTexels into a texture, covers a target of
// the same size with it through coveringWGSL with draw, given an indirect
// buffer of a draw's arguments at 0 and an indexed draw's at 16, submits,
// copies the target into a buffer of 256-byte rows, and maps that for
// reading. The pipeline's layout is reflected when reflected is true,
// sampledLayout's otherwise.
func drawSampledTexture(t *testing.T, device gfx.Device, reflected bool, draw func(pass gfx.RenderPassEncoder, indirect gfx.Buffer)) gfx.Buffer {
	pictureView, err := device.CreateTextureView(createSampledTexture(t, device), gfx.TextureViewDescriptor{})
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
	pipeline := createRenderPipeline(t, device, coveringWGSL, groups, []gfx.VertexBufferLayout{corner}, gfx.TextureFormatRGBA8Unorm, nil)
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
	return drawIntoTarget(t, device, gfx.RenderPassDepthStencilAttachment{}, func(pass gfx.RenderPassEncoder) {
		pass.SetPipeline(pipeline)
		pass.SetBindGroup(0, group, nil)
		pass.SetVertexBuffer(0, corners, 0)
		pass.SetIndexBuffer(indices, gfx.IndexFormatUint32, 0)
		draw(pass, indirect)
	})
}

// clearedStencil is the stencil value paintTarget clears its depth-stencil
// attachment to.
const clearedStencil = 1

// paintTarget paints paintingWGSL over a target sampledSide texels square,
// cleared to transparent black, with set's pass state, through a pipeline of
// blend and depthStencil. A depth-stencil format draws with an attachment of
// it, its stencil cleared to clearedStencil. It submits, copies the target
// into a buffer of 256-byte rows, and maps that for reading.
func paintTarget(t *testing.T, device gfx.Device, blend gfx.BlendState, depthStencil gfx.DepthStencilState, set func(pass gfx.RenderPassEncoder)) gfx.Buffer {
	pipeline := createRenderPipeline(t, device, paintingWGSL, nil, nil, gfx.TextureFormatRGBA8Unorm, func(descriptor *gfx.RenderPipelineDescriptor) {
		descriptor.Fragment.Targets[0].Blend = blend
		descriptor.DepthStencil = depthStencil
	})
	var attachment gfx.RenderPassDepthStencilAttachment
	if depthStencil.Format != 0 {
		texture, err := device.CreateTexture(gfx.TextureDescriptor{Size: sampledSize, Format: depthStencil.Format, Usage: gfx.TextureUsageFlagRenderAttachment})
		if err != nil {
			t.Errorf("CreateTexture answered %v", err)
		}
		view, err := device.CreateTextureView(texture, gfx.TextureViewDescriptor{})
		if err != nil {
			t.Errorf("CreateTextureView answered %v", err)
		}
		attachment = gfx.RenderPassDepthStencilAttachment{
			View: view, DepthLoadOp: gfx.LoadOpClear, DepthStoreOp: gfx.StoreOpDiscard, DepthClearValue: 1,
			StencilLoadOp: gfx.LoadOpClear, StencilStoreOp: gfx.StoreOpDiscard, StencilClearValue: clearedStencil,
		}
	}
	return drawIntoTarget(t, device, attachment, func(pass gfx.RenderPassEncoder) {
		pass.SetPipeline(pipeline)
		set(pass)
		pass.Draw(gfx.DrawArgs{VertexCount: 3, InstanceCount: 1})
	})
}

// drawIntoTarget records draw into a render pass on a target sampledSide
// texels square, cleared to transparent black, and depthStencil, submits,
// copies the target into a buffer of 256-byte rows, and maps that for
// reading.
func drawIntoTarget(t *testing.T, device gfx.Device, depthStencil gfx.RenderPassDepthStencilAttachment, draw func(pass gfx.RenderPassEncoder)) gfx.Buffer {
	target := createTexture(t, device, sampledSide)
	targetView, err := device.CreateTextureView(target, gfx.TextureViewDescriptor{})
	if err != nil {
		t.Errorf("CreateTextureView answered %v", err)
	}
	encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
	if err != nil {
		t.Errorf("CreateCommandEncoder answered %v", err)
		return gfx.Buffer{}
	}
	pass := encoder.BeginRenderPass(gfx.RenderPassDescriptor{
		ColorAttachments:       []gfx.RenderPassColorAttachment{{View: targetView, LoadOp: gfx.LoadOpClear, StoreOp: gfx.StoreOpStore}},
		DepthStencilAttachment: depthStencil,
	})
	draw(pass)
	pass.End()
	return readTexture(t, device, encoder, target)
}

// readTexture copies texture, sampledSide texels square, after encoder's
// commands into a buffer of 256-byte rows, submits, and maps that for
// reading.
func readTexture(t *testing.T, device gfx.Device, encoder gfx.StandaloneCommandEncoder, texture gfx.Texture) gfx.Buffer {
	readback, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 256 * sampledSide, Usage: gfx.BufferUsageFlagMapRead | gfx.BufferUsageFlagCopyDst})
	if err != nil {
		t.Errorf("CreateBuffer answered %v", err)
	}
	encoder.CopyTextureToBuffer(texture, readback, []gfx.BufferTextureCopy{{
		BufferLayout: gfx.ImageDataLayout{BytesPerRow: 256, RowsPerImage: sampledSide},
		TextureBase:  gfx.ImageCopyTexture{Texture: texture},
		Size:         sampledSize,
	}})
	submitAndMap(t, device, encoder, readback, 256*sampledSide)
	return readback
}

// readBuffer copies size bytes of buffer after encoder's commands into a
// buffer, submits, and maps that for reading.
func readBuffer(t *testing.T, device gfx.Device, encoder gfx.StandaloneCommandEncoder, buffer gfx.Buffer, size uint64) gfx.Buffer {
	readback, err := device.CreateBuffer(gfx.BufferDescriptor{Size: size, Usage: gfx.BufferUsageFlagMapRead | gfx.BufferUsageFlagCopyDst})
	if err != nil {
		t.Errorf("CreateBuffer answered %v", err)
	}
	encoder.CopyBufferToBuffer(buffer, 0, readback, 0, size)
	submitAndMap(t, device, encoder, readback, size)
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

	encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
	if err != nil {
		t.Errorf("CreateCommandEncoder answered %v", err)
		return gfx.Buffer{}
	}
	pass := encoder.BeginComputePass(gfx.ComputePassDescriptor{})
	pass.SetPipeline(pipeline)
	pass.SetBindGroup(0, group, nil)
	dispatch(pass, indirect)
	pass.End()
	return readBuffer(t, device, encoder, squares, 32)
}

// checkTexture reads back readback, a texture sampledSide texels square
// copied into 256-byte rows, and checks that it holds texels row by row.
func checkTexture(h *harness, readback gfx.Buffer, texels []byte) {
	h.t.Helper()
	read := h.readBack(readback, 256*sampledSide)
	for row := range sampledSide {
		rowRead := read[256*row : 256*row+4*sampledSide]
		rowWanted := texels[4*sampledSide*row : 4*sampledSide*(row+1)]
		if !bytes.Equal(rowRead, rowWanted) {
			h.t.Errorf("row %d read % x, want % x", row, rowRead, rowWanted)
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
// clearing window's framebuffer to color, unless the frame has none.
func clearFramebuffer(t *testing.T, event gfx.DrawEvent, device gfx.Device, window platform.Window, color gfx.Color) {
	view := device.Framebuffer(event.Frame, window)
	if view == (gfx.TextureView{}) {
		return
	}
	encoder, err := device.Encoder(event.Frame, 0)
	if err != nil {
		t.Errorf("Encoder answered %v", err)
		return
	}
	encoder.BeginRenderPass(gfx.RenderPassDescriptor{ColorAttachments: []gfx.RenderPassColorAttachment{{
		View: view, LoadOp: gfx.LoadOpClear, StoreOp: gfx.StoreOpStore, ClearValue: color,
	}}}).End()
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

// createBindGroup creates a bind group of entries over a layout of
// layoutEntries.
func createBindGroup(t *testing.T, device gfx.Device, layoutEntries []gfx.BindGroupLayoutEntry, entries []gfx.BindGroupEntry) gfx.BindGroup {
	layout, err := device.CreateBindGroupLayout(gfx.BindGroupLayoutDescriptor{Entries: layoutEntries})
	if err != nil {
		t.Errorf("CreateBindGroupLayout answered %v", err)
	}
	group, err := device.CreateBindGroup(gfx.BindGroupDescriptor{Layout: layout, Entries: entries})
	if err != nil {
		t.Errorf("CreateBindGroup answered %v", err)
	}
	return group
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
