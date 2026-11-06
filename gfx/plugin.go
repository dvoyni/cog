package gfx

import (
	"fmt"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/metronome"
	"github.com/dvoyni/cog/platform"
)

// PluginName is gfx's plugin name.
const PluginName kernel.PluginName = "gfx"

// Plugin is gfx: it declares the InstanceSocket a backend binds, installs
// the Device and Handlers, and draws one frame per RenderEvent.
type Plugin struct {
	// config is a copy of the Config Init was given.
	config         Config
	instanceSocket kernel.ExclusiveSocket[InstanceSocket]
	// instance, adapter and device.backend are what Start requested, for
	// Destroy to release; nil where Start got no further.
	instance InstanceSocket
	adapter  BackendAdapter
	device   *device
}

// Config is gfx's part of the kernel's config, under PluginName: what the
// device is requested with in Start.
type Config struct {
	PowerPreference PowerPreference
	// ForceFallbackAdapter asks for the software adapter, for pixel tests
	// with no GPU.
	ForceFallbackAdapter bool
	RequiredFeatures     FeatureFlags
	// RequiredLimits are the limits to request; zero is DefaultLimits(),
	// passed explicitly, since wgpu's zero is the adapter's full limits.
	RequiredLimits Limits
	// Surface is the surface request every window starts with.
	Surface SurfaceRequest
	// Clock is what gfx reads DrawEvent's durations from: the clock given to
	// metronome, so a test's manual clock makes them deterministic. Nil is
	// metronome's WallClock.
	Clock metronome.Clock
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

// Init declares the required InstanceSocket and installs the Device, empty
// until Start. A config of another type than *Config is a composition error,
// and panics.
func (p *Plugin) Init(configurator *kernel.Configurator, config any) error {
	p.config = *kernel.PluginConfigOrDefault[Config](config)
	p.instanceSocket = configurator.DeclareExclusiveSocket[InstanceSocket](false)
	p.device = &device{}
	configurator.InitResource[Device](p.device)
	return nil
}

// Start requests the adapter and the device from the bound InstanceSocket,
// with Config's limits or, when they are zero, DefaultLimits(). Finding no
// adapter, or no device, is an error.
func (p *Plugin) Start(*kernel.Kernel) error {
	p.instance = p.instanceSocket.Get()
	adapter, err := p.instance.RequestAdapter(RequestAdapterOptions{
		PowerPreference:      p.config.PowerPreference,
		ForceFallbackAdapter: p.config.ForceFallbackAdapter,
	})
	if err != nil {
		return fmt.Errorf("gfx: requesting an adapter: %w", err)
	}
	p.adapter = adapter
	limits := p.config.RequiredLimits
	if limits == (Limits{}) {
		limits = DefaultLimits()
	}
	backend, err := adapter.RequestDevice(DeviceDescriptor{
		RequiredFeatures: p.config.RequiredFeatures,
		RequiredLimits:   limits,
	})
	if err != nil {
		return fmt.Errorf("gfx: requesting a device: %w", err)
	}
	p.device.backend = backend
	p.device.features = p.config.RequiredFeatures
	p.device.limits = limits
	p.device.adapterInfo = adapter.Info()
	return nil
}

// Stop does nothing: frames still in flight finish.
func (*Plugin) Stop(*kernel.Kernel) error { return nil }

// Destroy releases the device, the adapter and the instance Start
// requested, after the kernel's last job; releasing the device cleanly is
// what saves Vulkan's pipeline cache.
func (p *Plugin) Destroy() error {
	if p.device.backend != nil {
		p.device.backend.Release()
	}
	if p.adapter != nil {
		p.adapter.Release()
	}
	if p.instance != nil {
		p.instance.Release()
	}
	return nil
}
