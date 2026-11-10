// Package gogpu is the plugin that gives a Cog kernel a real GPU: it
// implements gfx's backend over our gogpu fork, driving the fork's HostLoop
// from the kernel's main thread.
package gogpu

import (
	"runtime"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
	host "github.com/gogpu/gogpu"
)

// PluginName is the gogpu plugin's name.
const PluginName kernel.PluginName = "gogpu"

// Plugin is the gogpu plugin. Its Init binds gfx.InstanceSocket,
// platform.WindowsSocket and platform.InputSourceSocket to an instance, a
// window system and an input source of its own, each over the plugin, which
// holds what they share: the instance starts the fork's HostLoop at gfx's
// adapter request and stops it at gfx's release of the instance, which Init
// pins to the main thread.
type Plugin struct {
	// loop is the fork's HostLoop, which the instance starts at the adapter
	// request; nil before it and after the instance's Release.
	loop *host.HostLoop
	// windows maps each window the window system opened to gogpu's; the
	// instance makes it with the loop, and the device reads it.
	windows map[platform.Window]*host.Window
	// changes is what the window system's next Pump reports, in the order it
	// happened.
	changes []platform.InputChange
	// inputChanges is what the input source's next AppendChanges reports, in
	// the order it happened.
	inputChanges []platform.InputChange
}

// Config is the gogpu plugin's part of the kernel's config, under
// PluginName.
type Config struct {
	// Backend is the graphics API the adapter is requested on. Zero is
	// gogpu's choice on this system, GL excluded: Metal on macOS, Vulkan
	// elsewhere. GL and the browser's WebGPU are never chosen: asking for
	// either fails the adapter's request.
	Backend gfx.Backend
	// MainWindow is the window opened at start, through platform's
	// WindowManager like any other. The zero entry opens none.
	MainWindow platform.WindowEntry
}

// NewPlugin returns the gogpu plugin.
func NewPlugin() *Plugin { return &Plugin{} }

// Name returns PluginName.
func (*Plugin) Name() kernel.PluginName { return PluginName }

// Dependencies names gfx, whose Tasks Init pins, and platform, whose
// WindowManager it opens the main window in.
func (*Plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{gfx.PluginName, platform.PluginName}
}

// Init binds an instance to gfx.InstanceSocket, a window system to
// platform.WindowsSocket and its input source to
// platform.InputSourceSocket, and pins to the main thread gfx's tasks the
// HostLoop or the OS requires there: acquire and present, which begin and end
// frames through the HostLoop, the device's recovery and release, and on
// Windows the surfaces' configure, and on Vulkan their release. It then opens
// Config.MainWindow in platform's WindowManager, which the first
// ProcessEvents run applies. A config of another type than *Config is a
// composition error, and panics.
func (p *Plugin) Init(configurator *kernel.Configurator, config any) error {
	gogpuConfig := kernel.PluginConfigOrDefault[Config](config)
	configurator.BindSocket[gfx.InstanceSocket](instance{plugin: p, backend: gogpuConfig.Backend})
	configurator.BindSocket[platform.WindowsSocket](windowSystem{plugin: p})
	configurator.BindSocket[platform.InputSourceSocket](inputSource{plugin: p})
	tasks, _ := configurator.Resource[gfx.Tasks]()
	configurator.PinToMainThread(tasks.AcquireFramebuffers)
	configurator.PinToMainThread(tasks.PresentFramebuffers)
	configurator.PinToMainThread(tasks.RecoverDevice)
	configurator.PinToMainThread(tasks.ReleaseDevice)
	if runtime.GOOS == "windows" {
		configurator.PinToMainThread(tasks.ConfigureSurfaces)
		if gogpuConfig.Backend != gfx.BackendDX12 {
			configurator.PinToMainThread(tasks.ReleaseSurface)
			configurator.PinToMainThread(tasks.ReleaseOpenSurfaces)
		}
	}
	if gogpuConfig.MainWindow != (platform.WindowEntry{}) {
		manager, _ := configurator.Resource[*platform.WindowManager]()
		manager.Open(gogpuConfig.MainWindow)
	}
	return nil
}

// Start does nothing: the HostLoop starts at gfx's adapter request.
func (*Plugin) Start(*kernel.Kernel) error { return nil }

// Stop does nothing: the HostLoop stops at gfx's release of the instance.
func (*Plugin) Stop(*kernel.Kernel) error { return nil }

// Destroy does nothing.
func (*Plugin) Destroy() error { return nil }
