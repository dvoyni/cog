package headless_test

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/platform/headless"
)

// harness is the test's plugin and the supervisor's logger: it binds a
// headless.Windows in its Init, schedules ProcessEvents and edits of the
// window manager on demand, and records the window events platform
// publishes.
type harness struct {
	t          *testing.T
	supervisor *kernel.Supervisor
	kernel     *kernel.Kernel
	windows    *headless.Windows
	edit       func(manager *platform.WindowManager)
	mutex      sync.Mutex
	published  []any
}

// pumpEvent schedules ProcessEvents.
type pumpEvent struct{}

// editEvent schedules the harness's edit under the manager's write lock.
type editEvent struct{}

func (*harness) Name() kernel.PluginName { return "test" }

func (*harness) Dependencies() []kernel.PluginName { return []kernel.PluginName{platform.PluginName} }

func (h *harness) Init(c *kernel.Configurator, config any) error {
	h.windows = headless.NewWindows()
	c.BindSocket[platform.WindowsSocket](h.windows)
	tasks, _ := c.Resource[platform.Tasks]()
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
	subscribeRecord[platform.WindowOpenedEvent](c, h)
	subscribeRecord[platform.WindowClosedEvent](c, h)
	subscribeRecord[platform.ResizeEvent](c, h)
	return nil
}

func (*harness) Start(*kernel.Kernel) error { return nil }

func (*harness) Stop(*kernel.Kernel) error { return nil }

func (*harness) Destroy() error { return nil }

func (h *harness) LogErr(source kernel.KernelID, err error) {
	h.t.Errorf("kernel %d logged %v", source, err)
}

func (*harness) LogWarn(source kernel.KernelID, message string, args ...any) {}

func (*harness) LogDebug(source kernel.KernelID, message string, args ...any) {}

// subscribeRecord records every E platform publishes.
func subscribeRecord[E any](c *kernel.Configurator, h *harness) {
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event E) (E, kernel.JobHandle) {
		h.mutex.Lock()
		h.published = append(h.published, event)
		h.mutex.Unlock()
		return event, in
	})
}

// spawn spawns platform and the harness. The supervisor quits when the test
// ends.
func spawn(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t}
	h.supervisor = kernel.NewSupervisor(h)
	t.Cleanup(h.supervisor.Quit)
	id, err := h.supervisor.Spawn(nil, 2, platform.NewPlugin(), h)
	if err != nil {
		t.Fatal(err)
	}
	h.kernel = h.supervisor.GetKernel(id)
	return h
}

// pump runs ProcessEvents once, and waits for it.
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

// wait ticks the main thread, which ProcessEvents is pinned to, until out has
// finished.
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
