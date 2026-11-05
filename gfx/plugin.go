package gfx

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
)

// PluginName is gfx's plugin name.
const PluginName kernel.PluginName = "gfx"

// Plugin is gfx: it declares the InstanceSocket a backend binds, installs
// the Device and Handlers, and draws one frame per RenderEvent.
type Plugin struct{}

// Config is gfx's part of the kernel's config, under PluginName: what the
// device is requested with in Start.
type Config struct {
	PowerPreference  PowerPreference
	RequiredFeatures Features
	// RequiredLimits are the limits to request; zero is DefaultLimits(),
	// passed explicitly, since wgpu's zero is the adapter's full limits.
	RequiredLimits Limits
}

// NewPlugin returns gfx.
func NewPlugin() *Plugin { return &Plugin{} }

// Name returns PluginName.
func (*Plugin) Name() kernel.PluginName { return PluginName }

// Dependencies names platform, which gfx reads windows from and so is listed
// after.
func (*Plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{platform.PluginName}
}

// Init declares the InstanceSocket, installs the Device and Handlers, and
// subscribes to RenderEvent and platform's window events.
func (*Plugin) Init(configurator *kernel.Configurator, config any) error {
	panic("gfx listing: no bodies")
}

// Start requests the adapter and the device from the bound InstanceSocket;
// finding no adapter is an error.
func (*Plugin) Start(*kernel.Kernel) error { panic("gfx listing: no bodies") }

// Stop does nothing: frames still in flight finish.
func (*Plugin) Stop(*kernel.Kernel) error { panic("gfx listing: no bodies") }

// Destroy releases the surfaces, the device and the adapter, after the
// kernel's last job.
func (*Plugin) Destroy() error { panic("gfx listing: no bodies") }
