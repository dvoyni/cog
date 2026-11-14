package render_test

import (
	"sync/atomic"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/render"
)

// lockProbe is the test's plugin, listed after render. On its first
// FrameEvent it schedules two tasks after Ready, joined into the out
// dependency: one holding render's Frame, and the other that registerOther
// registers, such as registerWriter, or with none a second Frame task. Each task waits a while for
// the other to be running, and a Frame task keeps whether it ever found it so.
type lockProbe struct {
	registerOther func(p *lockProbe, configurator *kernel.Configurator) *kernel.Task[kernel.NoArg, kernel.NoResult]
	// running counts the probe's tasks running, and ranTogether is set by a
	// Frame task that found both running.
	running     atomic.Int32
	ranTogether atomic.Bool
	scheduled   atomic.Bool
}

func (*lockProbe) Name() kernel.PluginName { return "lock probe" }

func (*lockProbe) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{render.PluginName}
}

func (p *lockProbe) Init(configurator *kernel.Configurator, _ any) error {
	frameTask := configurator.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, _ render.Frame) kernel.NoResult {
		p.awaitTheOther()
		return kernel.NoResult{}
	})
	otherTask := frameTask
	if p.registerOther != nil {
		otherTask = p.registerOther(p, configurator)
	}
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		if p.scheduled.Swap(true) {
			return event, in
		}
		frameJob := ex.Schedule(frameTask, kernel.NoArg{}, event.Ready).Handle()
		otherJob := ex.Schedule(otherTask, kernel.NoArg{}, event.Ready).Handle()
		return event, ex.Join(in, ex.Join(frameJob, otherJob))
	})
	return nil
}

func (*lockProbe) Start(*kernel.Kernel) error { return nil }

func (*lockProbe) Stop(*kernel.Kernel) error { return nil }

func (*lockProbe) Destroy() error { return nil }

// registerWriter registers a task holding resource T for write, which waits
// for p's other task as a Frame task does.
func (p *lockProbe) registerWriter[T any](configurator *kernel.Configurator) *kernel.Task[kernel.NoArg, kernel.NoResult] {
	return configurator.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, _ kernel.Write[T]) kernel.NoResult {
		p.awaitTheOther()
		return kernel.NoResult{}
	})
}

// awaitTheOther counts its task running for up to 100ms, and sets ranTogether
// when the other task is running too in that time.
func (p *lockProbe) awaitTheOther() {
	p.running.Add(1)
	defer p.running.Add(-1)
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if p.running.Load() == 2 {
			p.ranTogether.Store(true)
			return
		}
		time.Sleep(time.Millisecond)
	}
}
