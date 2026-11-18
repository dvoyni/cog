package canvas

import (
	"math"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// PluginName is canvas's plugin name.
const PluginName kernel.PluginName = "canvas"

// Config is canvas's part of the kernel's config, under PluginName. It has no
// field yet.
type Config struct{}

// Plugin is canvas: it installs Handlers and its private state, binds its
// built-in assets, publishes FrameEvent inside each render.FrameEvent, and
// draws the frame's entries once they are recorded.
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

// Init installs the draw state, with the kernel's logger for reports, the
// renderer and the window list; binds canvas's built-in assets as a read
// mount under the prefix cog.canvas, at the lowest priority, so any other
// mount holding one of their names overrides it; and subscribes to
// render.FrameEvent, platform.WindowOpenedEvent and
// platform.WindowClosedEvent, the Subs kept in the Handlers resource. A
// config of another type than *Config is a composition error, and panics.
func (*Plugin) Init(configurator *kernel.Configurator, config any) error {
	kernel.PluginConfigOrDefault[Config](config)
	configurator.InitResource(&drawState{logger: configurator.Logger()})
	configurator.InitResource(&renderer{})
	configurator.InitResource(&windowList{})
	configurator.BindSocket(platform.ReadMountSocket{FS: makeAssetFS(), Priority: math.MinInt, Prefix: assetPrefix})
	openFrameTask := configurator.RegisterTask1(openFrame)
	mergeLanesTask := configurator.RegisterTask1(mergeLanes)
	drawTask := configurator.RegisterTask6(draw)
	addWindowTask := configurator.RegisterTask1(addWindow)
	removeWindowTask := configurator.RegisterTask1(removeWindow)
	configurator.InitResource(Handlers{
		RecordFrame: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
			return recordFrame(ex, in, event, openFrameTask, mergeLanesTask, drawTask)
		}),
		TrackOpenedWindow: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event platform.WindowOpenedEvent) (platform.WindowOpenedEvent, kernel.JobHandle) {
			return trackOpenedWindow(ex, in, event, addWindowTask)
		}),
		ForgetClosedWindow: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event platform.WindowClosedEvent) (platform.WindowClosedEvent, kernel.JobHandle) {
			return forgetClosedWindow(ex, in, event, removeWindowTask)
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
