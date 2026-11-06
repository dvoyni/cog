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
	// adapter, device.instance and device.backend are what Start requested,
	// for Destroy to release; nil where Start got no further.
	adapter BackendAdapter
	device  *device
	// acquireTask and presentTask are the frame chain's pinned ends, and
	// flushTask what runs between the frame's recording and its present,
	// which Handlers.Draw schedules.
	acquireTask *kernel.Task[Frame, kernel.NoResult]
	flushTask   *kernel.Task[Frame, kernel.NoResult]
	presentTask *kernel.Task[Frame, kernel.NoResult]
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

// Init declares the required InstanceSocket, installs the Device, empty
// until Start, and subscribes to RenderEvent and platform's window events,
// the Subs kept in the Handlers resource. A config of another type than *Config is a
// composition error, and panics.
func (p *Plugin) Init(configurator *kernel.Configurator, config any) error {
	p.config = *kernel.PluginConfigOrDefault[Config](config)
	p.instanceSocket = configurator.DeclareExclusiveSocket[InstanceSocket](false)
	p.device = &device{}
	configurator.InitResource[Device](p.device)
	create := configurator.RegisterTask2(createSurface)
	configurator.PinToMainThread(create)
	release := configurator.RegisterTask1(releaseSurface)
	configurator.PinToMainThread(release)
	p.acquireTask = configurator.RegisterTask1(acquireFramebuffers)
	configurator.PinToMainThread(p.acquireTask)
	p.flushTask = configurator.RegisterTask1(flushFrame)
	p.presentTask = configurator.RegisterTask1(presentFramebuffers)
	configurator.PinToMainThread(p.presentTask)
	configurator.InitResource(Handlers{
		Draw: configurator.Subscribe(p.draw),
		CreateSurface: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event platform.WindowOpenedEvent) (platform.WindowOpenedEvent, kernel.JobHandle) {
			return event, ex.Schedule(create, event.Window, in).Handle()
		}),
		ReleaseSurface: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event platform.WindowClosedEvent) (platform.WindowClosedEvent, kernel.JobHandle) {
			lastPresent := p.device.frames.markClosing(event.Window)
			return event, ex.Schedule(release, event.Window, in, lastPresent).Handle()
		}),
	})
	return nil
}

// Start requests the adapter and the device from the bound InstanceSocket,
// with Config's limits or, when they are zero, DefaultLimits(). Finding no
// adapter, or no device, is an error.
func (p *Plugin) Start(k *kernel.Kernel) error {
	p.device.logger = k.Logger()
	p.device.instance = p.instanceSocket.Get()
	adapter, err := p.device.instance.RequestAdapter(RequestAdapterOptions{
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

// Destroy releases the surfaces of the windows still open, then the device,
// the adapter and the instance Start requested, after the kernel's last job;
// releasing the device cleanly is what saves Vulkan's pipeline cache.
func (p *Plugin) Destroy() error {
	for _, window := range p.device.surfaces {
		p.device.instance.ReleaseSurface(window)
	}
	if p.device.backend != nil {
		p.device.backend.Release()
	}
	if p.adapter != nil {
		p.adapter.Release()
	}
	if p.device.instance != nil {
		p.device.instance.Release()
	}
	return nil
}
