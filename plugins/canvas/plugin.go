package canvas

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// PluginName is canvas's plugin name.
const PluginName kernel.PluginName = "canvas"

// Config is canvas's part of the kernel's config, under PluginName. It has no
// field yet.
type Config struct{}

// Plugin is canvas: it installs Handlers and its private draw state, and
// publishes FrameEvent inside each render.FrameEvent.
type Plugin struct{}

// NewPlugin returns canvas.
func NewPlugin() *Plugin { return &Plugin{} }

// Name returns PluginName.
func (*Plugin) Name() kernel.PluginName { return PluginName }

// Dependencies names render, which canvas draws through, and platform; canvas
// is listed after both, and a plugin that records into canvas after canvas.
func (*Plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{render.PluginName, platform.PluginName}
}

// Init installs the draw state, with the kernel's logger for reports, and
// subscribes to render.FrameEvent, the Sub kept in the Handlers resource. A
// config of another type than *Config is a composition error, and panics.
func (*Plugin) Init(configurator *kernel.Configurator, config any) error {
	kernel.PluginConfigOrDefault[Config](config)
	configurator.InitResource(&drawState{logger: configurator.Logger()})
	openFrameTask := configurator.RegisterTask1(openFrame)
	mergeLanesTask := configurator.RegisterTask1(mergeLanes)
	drawTask := configurator.RegisterTask2(draw)
	configurator.InitResource(Handlers{
		RecordFrame: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
			return recordFrame(ex, in, event, openFrameTask, mergeLanesTask, drawTask)
		}),
	})
	return nil
}

// Start does nothing.
func (*Plugin) Start(*kernel.Kernel) error { return nil }

// Stop does nothing.
func (*Plugin) Stop(*kernel.Kernel) error { return nil }

// Destroy does nothing.
func (*Plugin) Destroy() error { return nil }
