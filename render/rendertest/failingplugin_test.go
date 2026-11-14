package rendertest_test

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/render"
)

// failingPlugin is the test's plugin: on the first FrameEvent it logs each of
// errorsToLog from a recording task.
type failingPlugin struct {
	errorsToLog []error
}

func (*failingPlugin) Name() kernel.PluginName { return "failing plugin" }

func (*failingPlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{render.PluginName}
}

func (p *failingPlugin) Init(configurator *kernel.Configurator, _ any) error {
	log := configurator.RegisterTask0(func(k *kernel.Kernel, event render.FrameEvent) kernel.NoResult {
		for _, err := range p.errorsToLog {
			k.Logger().LogErr(err)
		}
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		if event.Tick != 1 {
			return event, in
		}
		return event, ex.Join(in, ex.Schedule(log, event, event.Ready).Handle())
	})
	return nil
}

func (*failingPlugin) Start(*kernel.Kernel) error { return nil }

func (*failingPlugin) Stop(*kernel.Kernel) error { return nil }

func (*failingPlugin) Destroy() error { return nil }
