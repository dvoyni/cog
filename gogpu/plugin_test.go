package gogpu_test

import (
	"os"
	"runtime"
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

// harness spawns a kernel of metronome, platform, gfx, gogpu and itself from
// the main thread, reads its Device, and quits it, failing the test on every
// error logged.
type harness struct {
	t          *testing.T
	supervisor *kernel.Supervisor
	kernel     *kernel.Kernel
	read       func(device gfx.Device)
	mutex      sync.Mutex
	logged     []error
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
		id, err = h.supervisor.Spawn(configs, 2, metronome.NewPlugin(), platform.NewPlugin(), gfx.NewPlugin(), gogpu.NewPlugin(), h)
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
	finished := false
	onMainThread(func() {
		_, out := h.kernel.Publish(readEvent{})
		signal := kernel.NewSignal()
		h.kernel.Notify(signal, out)
		finished = h.tickUntil(signal.C())
	})
	if !finished {
		h.t.Fatal("reading the device did not finish in 5s")
	}
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

func (*harness) Dependencies() []kernel.PluginName { return []kernel.PluginName{gfx.PluginName} }

func (h *harness) Init(c *kernel.Configurator, _ any) error {
	read := c.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, device kernel.Read[gfx.Device]) kernel.NoResult {
		h.read(device.Get())
		return kernel.NoResult{}
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event readEvent) (readEvent, kernel.JobHandle) {
		return event, ex.Schedule(read, kernel.NoArg{}, in).Handle()
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
