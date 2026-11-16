package rendertest

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/render"
)

// openWindowEvent opens a window of size, then pumps platform's events.
type openWindowEvent struct {
	size platform.Size
}

// harnessPlugin is the harness's own plugin, listed after render: it binds
// the harness's recording instance and its headless windows, unless gogpu's
// plugin binds its own on the software adapter, wraps gfx's instance, opens
// windows for it, and keeps the job making each opened window's surface.
type harnessPlugin struct {
	harness *Harness
}

// Name answers the harness plugin's name.
func (harnessPlugin) Name() kernel.PluginName { return "rendertest" }

// Dependencies names render.
func (harnessPlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{render.PluginName}
}

// Init binds gfx's InstanceSocket to the recording instance and platform's
// WindowsSocket to the headless windows, but on the software adapter, where
// gogpu's plugin binds its own; wraps gfx's InstanceSocket, whichever binds
// it, so its devices are a frame behind and answer reflection from binding
// declarations, or check them against the adapter's; and subscribes to
// openWindowEvent; to platform.WindowOpenedEvent, whose in, the plugin
// being listed after gfx, is the job making the window's surface; and to
// render.FrameEvent, to note each frame's beat for Frames.
func (p harnessPlugin) Init(configurator *kernel.Configurator, _ any) error {
	if !p.harness.softwareAdapter {
		configurator.BindSocket[gfx.InstanceSocket](p.harness.instance)
		configurator.BindSocket[platform.WindowsSocket](p.harness.windows)
	}
	configurator.WrapSocket[gfx.InstanceSocket](func(bound gfx.InstanceSocket) gfx.InstanceSocket {
		return harnessInstance{InstanceSocket: bound, harness: p.harness}
	})
	openWindow := configurator.RegisterTask1(func(k *kernel.Kernel, size platform.Size, manager kernel.Write[*platform.WindowManager]) kernel.NoResult {
		p.harness.opened = manager.Get().Open(platform.WindowEntry{Size: size})
		return kernel.NoResult{}
	})
	tasks, _ := configurator.Resource[platform.Tasks]()
	processEvents := tasks.ProcessEvents
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event openWindowEvent) (openWindowEvent, kernel.JobHandle) {
		openJob := ex.Schedule(openWindow, event.size, in).Handle()
		return event, ex.Schedule(processEvents, kernel.NoArg{}, openJob).Handle()
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event platform.WindowOpenedEvent) (platform.WindowOpenedEvent, kernel.JobHandle) {
		p.harness.surfaceMade = in
		return event, in
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		p.harness.noteBeat(event.Beat)
		return event, in
	})
	return nil
}

// Start does nothing.
func (harnessPlugin) Start(*kernel.Kernel) error { return nil }

// Stop does nothing.
func (harnessPlugin) Stop(*kernel.Kernel) error { return nil }

// Destroy does nothing.
func (harnessPlugin) Destroy() error { return nil }
