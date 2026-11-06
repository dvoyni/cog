package gfx_test

import (
	"runtime"
	"testing"
	"time"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
)

// harness is the test's plugin and the supervisor's logger: it binds a
// recording.Instance in its Init, unless told not to, and runs a function
// over the Device under its read or write lock on demand.
type harness struct {
	t          *testing.T
	supervisor *kernel.Supervisor
	kernel     *kernel.Kernel
	instance   *recording.Instance
	// unbound leaves InstanceSocket without a binding.
	unbound bool
	read    func(device gfx.Device)
	write   func(device kernel.Write[gfx.Device])
}

// readEvent runs the harness's read under the Device's read lock.
type readEvent struct{}

// writeEvent runs the harness's write under the Device's write lock.
type writeEvent struct{}

func (*harness) Name() kernel.PluginName { return "test" }

func (*harness) Dependencies() []kernel.PluginName { return []kernel.PluginName{gfx.PluginName} }

func (h *harness) Init(c *kernel.Configurator, config any) error {
	if !h.unbound {
		c.BindSocket[gfx.InstanceSocket](h.instance)
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

// newHarness answers a harness with a fresh recording instance and a
// supervisor that quits when the test ends.
func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, instance: recording.NewInstance()}
	h.supervisor = kernel.NewSupervisor(h)
	t.Cleanup(h.supervisor.Quit)
	return h
}

// spawn spawns platform, gfx with config, and the harness, and answers
// Spawn's error.
func (h *harness) spawn(config *gfx.Config) error {
	h.t.Helper()
	id, err := h.supervisor.Spawn(map[kernel.PluginName]any{gfx.PluginName: config}, 2,
		platform.NewPlugin(), gfx.NewPlugin(), h)
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

// quit quits the supervisor and waits until every kernel is destroyed.
func (h *harness) quit() {
	h.t.Helper()
	h.supervisor.Quit()
	select {
	case <-h.supervisor.Done():
	case <-time.After(5 * time.Second):
		h.t.Fatal("the supervisor did not quit in 5s")
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

// kinds answers the kinds of calls, in order.
func kinds(calls []recording.Call) []recording.CallKind {
	answer := make([]recording.CallKind, len(calls))
	for i, call := range calls {
		answer[i] = call.Kind
	}
	return answer
}
