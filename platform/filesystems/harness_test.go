package filesystems_test

import (
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
)

// harness is the application's plugin and the supervisor's logger: it binds
// the filesystems bind gives in its Init, and runs read under the read locks
// of the FS resource and the values, and write under their write locks, as
// jobs.
type harness struct {
	t          *testing.T
	supervisor *kernel.Supervisor
	kernel     *kernel.Kernel
	bind       func(c *kernel.Configurator)
	read       func(filesystem platform.FS, values *platform.Values)
	write      func(filesystem platform.WriteFS, values *platform.Values)
}

// readEvent schedules the harness's read.
type readEvent struct{}

// writeEvent schedules the harness's write.
type writeEvent struct{}

func (h *harness) Name() kernel.PluginName { return "test" }

func (*harness) Dependencies() []kernel.PluginName { return []kernel.PluginName{platform.PluginName} }

func (h *harness) Init(c *kernel.Configurator, config any) error {
	h.bind(c)
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

// spawn spawns platform and the harness, which binds what bind does. The
// supervisor quits when the test ends.
func spawn(t *testing.T, bind func(c *kernel.Configurator)) *harness {
	t.Helper()
	h := &harness{t: t}
	h.supervisor = kernel.NewSupervisor(h)
	t.Cleanup(h.supervisor.Quit)
	h.respawn(bind)
	return h
}

// respawn destroys h's kernel, if any, and spawns platform and the harness
// again under h's supervisor, the harness binding what bind does.
func (h *harness) respawn(bind func(c *kernel.Configurator)) {
	h.t.Helper()
	if h.kernel != nil {
		h.supervisor.Destroy(h.kernel.ID())
	}
	h.bind = bind
	id, err := h.supervisor.Spawn(nil, 2, platform.NewPlugin(), h)
	if err != nil {
		h.t.Fatal(err)
	}
	h.kernel = h.supervisor.GetKernel(id)
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

// wait waits until out has finished.
func (h *harness) wait(out kernel.JobHandle) {
	h.t.Helper()
	signal := kernel.NewSignal()
	h.kernel.Notify(signal, out)
	select {
	case <-signal.C():
	case <-time.After(5 * time.Second):
		h.t.Fatal("a job did not finish in 5s")
	}
}
