package rendertest_test

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/render"
)

// firstFramePlugin is the test's plugin: on the first FrameEvent it calls
// onFirstFrame with the kernel and a parameter P, such as kernel.Read of
// gfx's Device or render.Surface, from a task of its own after Ready, joined
// into the out dependency, to log errors or reach what P locks.
type firstFramePlugin[P kernel.Parameter[P]] struct {
	onFirstFrame func(k *kernel.Kernel, parameter P)
}

func (*firstFramePlugin[P]) Name() kernel.PluginName { return "first frame plugin" }

func (*firstFramePlugin[P]) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{render.PluginName}
}

func (p *firstFramePlugin[P]) Init(configurator *kernel.Configurator, _ any) error {
	callOnFirstFrame := configurator.RegisterTask1(func(k *kernel.Kernel, event render.FrameEvent, parameter P) kernel.NoResult {
		p.onFirstFrame(k, parameter)
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

func (*firstFramePlugin[P]) Start(*kernel.Kernel) error { return nil }

func (*firstFramePlugin[P]) Stop(*kernel.Kernel) error { return nil }

func (*firstFramePlugin[P]) Destroy() error { return nil }
