package rendertest_test

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/render"
)

// temporaryCount is how many temporary textures temporaryPlugin acquires
// each frame.
const temporaryCount = 32

// temporaryPlugin is the test's plugin: on each FrameEvent it acquires
// temporaryCount temporary textures from a recording task, of a size the
// frame before did not acquire, so with one idle frame allowed each frame's
// end frees the previous frame's. Listed after render, it handles each
// gfx.RenderEvent after render does.
type temporaryPlugin struct {
	// onRenderEnd runs in a job on each gfx.RenderEvent, after the out of
	// render's handler.
	onRenderEnd func()
}

func (*temporaryPlugin) Name() kernel.PluginName { return "temporary plugin" }

func (*temporaryPlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{render.PluginName}
}

func (p *temporaryPlugin) Init(configurator *kernel.Configurator, _ any) error {
	acquire := configurator.RegisterTask1(func(k *kernel.Kernel, event render.FrameEvent, frame render.Frame) kernel.NoResult {
		for range temporaryCount {
			frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "bloom", Width: 32 + uint32(event.Tick%2), Height: 32, Format: render.TextureFormatRGBA8Unorm})
		}
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(acquire, event, event.Ready).Handle())
	})
	renderEnd := configurator.RegisterTask0(func(k *kernel.Kernel, _ kernel.NoArg) kernel.NoResult {
		p.onRenderEnd()
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.RenderEvent) (gfx.RenderEvent, kernel.JobHandle) {
		return event, ex.Schedule(renderEnd, kernel.NoArg{}, in).Handle()
	})
	return nil
}

func (*temporaryPlugin) Start(*kernel.Kernel) error { return nil }

func (*temporaryPlugin) Stop(*kernel.Kernel) error { return nil }

func (*temporaryPlugin) Destroy() error { return nil }
