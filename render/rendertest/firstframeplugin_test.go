package rendertest_test

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/render"
)

// firstFramePlugin is the test's plugin: on the first FrameEvent it calls
// onFirstFrame with the kernel and gfx's Device, from a task of its own, to
// log errors or reach the harness's device as gfx does.
type firstFramePlugin struct {
	onFirstFrame func(k *kernel.Kernel, device gfx.Device)
}

func (*firstFramePlugin) Name() kernel.PluginName { return "first frame plugin" }

func (*firstFramePlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{render.PluginName}
}

func (p *firstFramePlugin) Init(configurator *kernel.Configurator, _ any) error {
	callOnFirstFrame := configurator.RegisterTask1(func(k *kernel.Kernel, event render.FrameEvent, device kernel.Read[gfx.Device]) kernel.NoResult {
		p.onFirstFrame(k, device.Get())
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		if event.Tick != 1 {
			return event, in
		}
		return event, ex.Join(in, ex.Schedule(callOnFirstFrame, event, event.Ready).Handle())
	})
	return nil
}

func (*firstFramePlugin) Start(*kernel.Kernel) error { return nil }

func (*firstFramePlugin) Stop(*kernel.Kernel) error { return nil }

func (*firstFramePlugin) Destroy() error { return nil }
