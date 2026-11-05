package platform_test

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
)

// fake is the implementing plugin: it installs itself as Windows and as the
// InputSource, an in-memory writable filesystem, an in-memory value store and
// its read mounts. It opens each window at once, reporting it open from the
// next Pump, and records every call platform makes.
type fake struct {
	installWindows, installInputSource bool
	installWritable                    bool
	calls                              []string
	writable                           *memoryFileSystem
	mounts                             []platform.ReadMountSocket
	// windowChanges are what the next Pump reports, inputChanges what the next
	// AppendChanges does.
	windowChanges, inputChanges []platform.InputChange
	nextNative                  uintptr
}

func newFake() *fake {
	return &fake{
		installWindows:     true,
		installInputSource: true,
		installWritable:    true,
		writable:           &memoryFileSystem{MapFS: fstest.MapFS{}},
	}
}

func (*fake) Name() kernel.PluginName { return "fake" }

func (f *fake) Init(c *kernel.Configurator, config any) error {
	if f.installWindows {
		c.InitResource[platform.Windows](f)
	}
	if f.installInputSource {
		c.InitResource[platform.InputSource](f)
	}
	if f.installWritable {
		c.BindSocket[platform.WriteFSSocket](f.writable)
	}
	for _, mount := range f.mounts {
		c.BindSocket(mount)
	}
	return nil
}

func (*fake) Start(*kernel.Kernel) error { return nil }

func (*fake) Stop(*kernel.Kernel) error { return nil }

func (*fake) Destroy() error { return nil }

func (f *fake) Open(window platform.Window, entry platform.WindowEntry) error {
	f.calls = append(f.calls, "open "+entry.Title)
	f.nextNative++
	f.windowChanges = append(f.windowChanges, platform.InputChange{
		Kind:            platform.InputChangeWindowOpened,
		Window:          window,
		Size:            entry.Size,
		FramebufferSize: platform.Size{Width: entry.Size.Width * 2, Height: entry.Size.Height * 2},
		Scale:           2,
		Native:          platform.NativeHandle{Window: f.nextNative},
	})
	return nil
}

func (f *fake) Change(window platform.Window, previous, next platform.WindowEntry) error {
	f.calls = append(f.calls, fmt.Sprintf("change %v to %v", previous, next))
	return nil
}

func (f *fake) Close(window platform.Window) error {
	f.calls = append(f.calls, "close")
	return nil
}

func (f *fake) Pump(changes []platform.InputChange) []platform.InputChange {
	changes = append(changes, f.windowChanges...)
	f.windowChanges = f.windowChanges[:0]
	return changes
}

func (f *fake) AppendChanges(changes []platform.InputChange) []platform.InputChange {
	changes = append(changes, f.inputChanges...)
	f.inputChanges = f.inputChanges[:0]
	return changes
}

// pumpEvent asks the test plugin to schedule ProcessEvents.
type pumpEvent struct{}

// editEvent asks it to run the harness's edit under the manager's write lock.
type editEvent struct{}

// readEvent asks it to run the harness's read under the files' and values'
// read locks.
type readEvent struct{}

// writeEvent asks it to run the harness's write under their write locks.
type writeEvent struct{}

// harness is a kernel of the fake, platform and a test plugin, which
// schedules ProcessEvents and request edits on demand and records every
// window event platform publishes.
type harness struct {
	t          *testing.T
	supervisor *kernel.Supervisor
	kernel     *kernel.Kernel
	fake       *fake
	manager    *platform.WindowManager
	edit       func(manager *platform.WindowManager)
	read       func(filesystem platform.FS, values *platform.Values)
	write      func(filesystem platform.WriteFS, values *platform.Values)
	mutex      sync.Mutex
	published  []any
	errors     []error
}

func (h *harness) Name() kernel.PluginName { return "test" }

func (h *harness) Init(c *kernel.Configurator, config any) error {
	tasks, _ := c.Resource[platform.Tasks]()
	h.manager, _ = c.Resource[*platform.WindowManager]()
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event pumpEvent) (pumpEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(tasks.ProcessEvents, kernel.NoArg{}).Handle())
	})
	edit := c.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, manager kernel.Write[*platform.WindowManager]) kernel.NoResult {
		h.edit(manager.Get())
		return kernel.NoResult{}
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event editEvent) (editEvent, kernel.JobHandle) {
		return event, ex.Schedule(edit, kernel.NoArg{}, in).Handle()
	})
	read := c.RegisterTask2(func(k *kernel.Kernel, _ kernel.NoArg, filesystem kernel.Read[platform.FS], values kernel.Read[*platform.Values]) kernel.NoResult {
		h.read(filesystem.Get(), values.Get())
		return kernel.NoResult{}
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event readEvent) (readEvent, kernel.JobHandle) {
		return event, ex.Schedule(read, kernel.NoArg{}, in).Handle()
	})
	write := c.RegisterTask2(func(k *kernel.Kernel, _ kernel.NoArg, filesystem kernel.Write[platform.FS], values kernel.Write[*platform.Values]) kernel.NoResult {
		h.write(platform.ElevateFileAccess(filesystem), values.Get())
		return kernel.NoResult{}
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event writeEvent) (writeEvent, kernel.JobHandle) {
		return event, ex.Schedule(write, kernel.NoArg{}, in).Handle()
	})
	subscribeRecord[platform.WindowOpenedEvent](c, h)
	subscribeRecord[platform.WindowClosedEvent](c, h)
	subscribeRecord[platform.CloseRequestedEvent](c, h)
	return nil
}

func (*harness) Start(*kernel.Kernel) error { return nil }

func (*harness) Stop(*kernel.Kernel) error { return nil }

func (*harness) Destroy() error { return nil }

func (h *harness) LogErr(source kernel.KernelID, err error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.errors = append(h.errors, err)
}

func (*harness) LogWarn(source kernel.KernelID, message string, args ...any) {}

func (*harness) LogDebug(source kernel.KernelID, message string, args ...any) {}

// subscribeRecord records every E into h.published.
func subscribeRecord[E any](c *kernel.Configurator, h *harness) {
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event E) (E, kernel.JobHandle) {
		h.mutex.Lock()
		defer h.mutex.Unlock()
		h.published = append(h.published, event)
		return event, in
	})
}

// spawn spawns the fake, platform and the harness, the fake listed first,
// as an implementation is. The supervisor quits when the test ends.
func spawn(t *testing.T, f *fake) *harness {
	t.Helper()
	return spawnConfigured(t, f, platform.Config{})
}

// spawnConfigured spawns as spawn does, platform configured with config.
func spawnConfigured(t *testing.T, f *fake, config platform.Config) *harness {
	t.Helper()
	h := &harness{t: t, fake: f}
	h.supervisor = kernel.NewSupervisor(h)
	t.Cleanup(h.supervisor.Quit)
	id, err := h.supervisor.Spawn(map[kernel.PluginName]any{platform.PluginName: config}, 2, f, platform.NewPlugin(), h)
	if err != nil {
		t.Fatal(err)
	}
	h.kernel = h.supervisor.GetKernel(id)
	return h
}

// pump runs ProcessEvents once, ticking the main thread, as main does, until
// it finishes.
func (h *harness) pump() {
	h.t.Helper()
	_, out := h.kernel.Publish(pumpEvent{})
	h.wait(out)
}

// change runs edit on the manager under its write lock, and waits for it.
func (h *harness) change(edit func(manager *platform.WindowManager)) {
	h.t.Helper()
	h.edit = edit
	_, out := h.kernel.Publish(editEvent{})
	h.wait(out)
}

// reading runs read under the read locks, and waits for it.
func (h *harness) reading(read func(filesystem platform.FS, values *platform.Values)) {
	h.t.Helper()
	h.read = read
	_, out := h.kernel.Publish(readEvent{})
	h.wait(out)
}

// writing runs write under the write locks, and waits for it.
func (h *harness) writing(write func(filesystem platform.WriteFS, values *platform.Values)) {
	h.t.Helper()
	h.write = write
	_, out := h.kernel.Publish(writeEvent{})
	h.wait(out)
}

// open adds entry to the request and answers its Window.
func (h *harness) open(entry platform.WindowEntry) platform.Window {
	h.t.Helper()
	var window platform.Window
	h.change(func(manager *platform.WindowManager) { window = manager.Open(entry) })
	return window
}

// entry answers window's request entry.
func (h *harness) entry(window platform.Window) (platform.WindowEntry, bool) {
	h.t.Helper()
	var entry platform.WindowEntry
	var ok bool
	h.change(func(manager *platform.WindowManager) { entry, ok = manager.Entry(window) })
	return entry, ok
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

// takePublished answers the events published since the last call.
func (h *harness) takePublished() []any {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	published := h.published
	h.published = nil
	return published
}

func TestWindowOpensOnTheNextPumpAndClosesWhenRemoved(t *testing.T) {
	h := spawn(t, newFake())
	window := h.open(platform.WindowEntry{Title: "main", Size: platform.Size{Width: 640, Height: 480}})
	if window == (platform.Window{}) {
		t.Fatal("Open answered the zero Window")
	}
	if h.manager.IsOpen(window) {
		t.Fatal("the window is open before any pump")
	}

	h.pump()
	if !h.manager.IsOpen(window) {
		t.Fatal("the window is not open after the pump")
	}
	if got, want := h.manager.Size(window), (platform.Size{Width: 640, Height: 480}); got != want {
		t.Errorf("size is %v, want %v", got, want)
	}
	if got, want := h.manager.FramebufferSize(window), (platform.Size{Width: 1280, Height: 960}); got != want {
		t.Errorf("framebuffer size is %v, want %v", got, want)
	}
	if got := h.manager.Scale(window); got != 2 {
		t.Errorf("scale is %v, want 2", got)
	}
	if got := h.manager.Native(window); got == (platform.NativeHandle{}) {
		t.Error("the native handle is zero")
	}
	if got, want := h.takePublished(), []any{platform.WindowOpenedEvent{Window: window}}; !slices.Equal(got, want) {
		t.Fatalf("the pump published %v, want %v", got, want)
	}

	h.change(func(manager *platform.WindowManager) { manager.Close(window) })
	h.pump()
	if h.manager.IsOpen(window) {
		t.Fatal("the window is open after its entry was removed")
	}
	if got, want := h.takePublished(), []any{platform.WindowClosedEvent{Window: window}}; !slices.Equal(got, want) {
		t.Fatalf("the pump published %v, want %v", got, want)
	}
	if got, want := h.fake.calls, []string{"open main", "close"}; !slices.Equal(got, want) {
		t.Fatalf("the fake was called %v, want %v", got, want)
	}
}

func TestPumpAppliesOnlyWhatChanged(t *testing.T) {
	h := spawn(t, newFake())
	window := h.open(platform.WindowEntry{Title: "first"})
	h.change(func(manager *platform.WindowManager) {
		manager.Change(window, platform.WindowEntry{Title: "second"})
	})
	h.pump()
	h.pump()
	for _, title := range []string{"third", "fourth"} {
		h.change(func(manager *platform.WindowManager) { manager.SetTitle(window, title) })
	}
	h.pump()
	h.pump()
	want := []string{"open second", fmt.Sprintf("change %v to %v", platform.WindowEntry{Title: "second"}, platform.WindowEntry{Title: "fourth"})}
	if got := h.fake.calls; !slices.Equal(got, want) {
		t.Fatalf("the fake was called %v, want %v", got, want)
	}
	if got, want := h.takePublished(), []any{platform.WindowOpenedEvent{Window: window}}; !slices.Equal(got, want) {
		t.Fatalf("the pumps published %v, want %v", got, want)
	}
}

func TestSettersChangeOneFieldEach(t *testing.T) {
	h := spawn(t, newFake())
	opened := platform.WindowEntry{Title: "main", Size: platform.Size{Width: 640, Height: 480}}
	window := h.open(opened)
	h.pump()

	h.change(func(manager *platform.WindowManager) {
		manager.SetTitle(window, "renamed")
		manager.SetSize(window, platform.Size{Width: 800, Height: 600})
		manager.SetFullscreen(window, true)
		manager.SetHidden(window, true)
	})
	h.pump()
	changed := platform.WindowEntry{Title: "renamed", Size: platform.Size{Width: 800, Height: 600}, Fullscreen: true, Hidden: true}
	if got, want := h.fake.calls, []string{"open main", fmt.Sprintf("change %v to %v", opened, changed)}; !slices.Equal(got, want) {
		t.Fatalf("the fake was called %v, want %v", got, want)
	}

	h.change(func(manager *platform.WindowManager) { manager.Close(window) })
	var answers []bool
	h.change(func(manager *platform.WindowManager) {
		answers = append(answers,
			manager.SetTitle(window, "closed"),
			manager.SetSize(window, platform.Size{}),
			manager.SetFullscreen(window, false),
			manager.SetHidden(window, false),
		)
	})
	if slices.Contains(answers, true) {
		t.Fatalf("setters on a closed window answered %v, want all false", answers)
	}
}

func TestHandleKeptPastCloseNamesNothing(t *testing.T) {
	h := spawn(t, newFake())
	closed := h.open(platform.WindowEntry{Title: "closed"})
	h.pump()
	h.change(func(manager *platform.WindowManager) { manager.Close(closed) })
	reopened := h.open(platform.WindowEntry{Title: "reopened"})
	h.change(func(manager *platform.WindowManager) {
		manager.Change(closed, platform.WindowEntry{Title: "through the old handle"})
	})
	h.pump()

	if closed == reopened {
		t.Fatal("the window opened after a close got the closed one's handle")
	}
	if h.manager.IsOpen(closed) {
		t.Error("the closed window's handle names an open window")
	}
	if !h.manager.IsOpen(reopened) {
		t.Error("the reopened window is not open")
	}
	if _, ok := h.entry(closed); ok {
		t.Error("the closed window's handle names a request entry")
	}
	if entry, _ := h.entry(reopened); entry.Title != "reopened" {
		t.Errorf("a change through the closed handle reached the reopened window: its title is %q", entry.Title)
	}
	want := []any{
		platform.WindowOpenedEvent{Window: closed},
		platform.WindowClosedEvent{Window: closed},
		platform.WindowOpenedEvent{Window: reopened},
	}
	if got := h.takePublished(); !slices.Equal(got, want) {
		t.Fatalf("the pumps published %v, want %v", got, want)
	}
}

func TestCloseRequestedClosesNothing(t *testing.T) {
	h := spawn(t, newFake())
	window := h.open(platform.WindowEntry{Title: "main"})
	h.pump()
	h.takePublished()

	h.fake.windowChanges = append(h.fake.windowChanges, platform.InputChange{Kind: platform.InputChangeCloseRequested, Window: window})
	h.pump()
	if got, want := h.takePublished(), []any{platform.CloseRequestedEvent{Window: window}}; !slices.Equal(got, want) {
		t.Fatalf("the pump published %v, want %v", got, want)
	}
	if !h.manager.IsOpen(window) {
		t.Fatal("a close request closed the window")
	}
	if _, ok := h.entry(window); !ok {
		t.Fatal("a close request removed the window's entry")
	}
	if got, want := h.fake.calls, []string{"open main"}; !slices.Equal(got, want) {
		t.Fatalf("the fake was called %v, want %v", got, want)
	}
}

func TestWindowClosedByItselfIsClosed(t *testing.T) {
	h := spawn(t, newFake())
	window := h.open(platform.WindowEntry{Title: "main"})
	h.pump()
	h.takePublished()

	h.fake.windowChanges = append(h.fake.windowChanges, platform.InputChange{Kind: platform.InputChangeWindowClosed, Window: window})
	h.pump()
	if got, want := h.takePublished(), []any{platform.WindowClosedEvent{Window: window}}; !slices.Equal(got, want) {
		t.Fatalf("the pump published %v, want %v", got, want)
	}
	if h.manager.IsOpen(window) {
		t.Fatal("the window is open after it closed by itself")
	}
	if _, ok := h.entry(window); ok {
		t.Fatal("the window's entry outlived its close")
	}
	h.pump()
	if got, want := h.fake.calls, []string{"open main"}; !slices.Equal(got, want) {
		t.Fatalf("the fake was called %v, want %v", got, want)
	}
}

func TestInputNamingAnUnknownWindowPanics(t *testing.T) {
	h := spawn(t, newFake())
	window := h.open(platform.WindowEntry{Title: "main"})
	h.pump()
	h.takePublished()

	h.fake.inputChanges = append(h.fake.inputChanges,
		platform.InputChange{Kind: platform.InputChangeCloseRequested, Window: window},
		platform.InputChange{Kind: platform.InputChangeCloseRequested},
	)
	h.pump()
	h.mutex.Lock()
	errors := slices.Clone(h.errors)
	h.mutex.Unlock()
	if len(errors) != 1 || !strings.Contains(errors[0].Error(), "platform: an input change names") {
		t.Fatalf("the pump logged %v, want its panic over the unknown window", errors)
	}
	if got := h.takePublished(); len(got) != 0 {
		t.Fatalf("the pump published %v from a batch naming an unknown window", got)
	}
}

func TestInitPanicsUnlessEachSeamHasOneInstaller(t *testing.T) {
	for _, test := range []struct {
		name    string
		plugins func() []kernel.Plugin
		want    string
	}{
		{"no Windows", func() []kernel.Plugin {
			return []kernel.Plugin{&fake{installInputSource: true}, platform.NewPlugin()}
		}, "installs platform.Windows"},
		{"no InputSource", func() []kernel.Plugin {
			return []kernel.Plugin{&fake{installWindows: true}, platform.NewPlugin()}
		}, "installs platform.InputSource"},
		{"implementation after platform", func() []kernel.Plugin {
			return []kernel.Plugin{platform.NewPlugin(), newFake()}
		}, "installs platform.Windows"},
		{"two Windows", func() []kernel.Plugin {
			return []kernel.Plugin{newFake(), &secondInstaller{func(c *kernel.Configurator) {
				c.InitResource[platform.Windows](newFake())
			}}, platform.NewPlugin()}
		}, "initialized twice"},
		{"no writable filesystem", func() []kernel.Plugin {
			f := newFake()
			f.installWritable = false
			return []kernel.Plugin{f, platform.NewPlugin()}
		}, `platform.WriteFSSocket, declared by plugin "platform", has no binding`},
		{"two writable filesystems", func() []kernel.Plugin {
			return []kernel.Plugin{newFake(), &secondInstaller{func(c *kernel.Configurator) {
				c.BindSocket[platform.WriteFSSocket](&memoryFileSystem{MapFS: fstest.MapFS{}})
			}}, platform.NewPlugin()}
		}, "has 2 bindings"},
		{"a read mount without a FileSystem", func() []kernel.Plugin {
			return []kernel.Plugin{newFake(), platform.NewPlugin(), &secondInstaller{func(c *kernel.Configurator) {
				c.BindSocket(platform.ReadMountSocket{Priority: 3})
			}}}
		}, "priority 3 has no FS"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := kernel.NewSupervisor(nil)
			t.Cleanup(s.Quit)
			defer func() {
				recovered := recover()
				if recovered == nil || !strings.Contains(fmt.Sprint(recovered), test.want) {
					t.Fatalf("Spawn panicked with %v, want a panic saying %q", recovered, test.want)
				}
			}()
			s.Spawn(nil, 1, test.plugins()...)
		})
	}
}

// secondInstaller is a second plugin, which installs in its Init.
type secondInstaller struct{ install func(c *kernel.Configurator) }

func (*secondInstaller) Name() kernel.PluginName { return "second" }

func (p *secondInstaller) Init(c *kernel.Configurator, config any) error {
	p.install(c)
	return nil
}

func (*secondInstaller) Start(*kernel.Kernel) error { return nil }

func (*secondInstaller) Stop(*kernel.Kernel) error { return nil }

func (*secondInstaller) Destroy() error { return nil }
