package gfx

import (
	"fmt"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/metronome"
	"github.com/dvoyni/cog/platform"
)

// Plugin is gfx: it declares the InstanceSocket a backend binds, installs
// the Device and Handlers, and draws one frame per RenderEvent.
type Plugin struct {
	// config is a copy of the Config Init was given.
	config         Config
	instanceSocket kernel.ExclusiveSocket[InstanceSocket]
	// device holds the instance, the adapter and the backend device Start
	// requested, for Destroy to release; nil where Start got no further.
	device *device
	// tasks are the surface tasks and the recovery the Tasks resource holds,
	// flushTask what runs between a frame's recording and its present, and
	// releaseCaptureTask what releases a capture's buffer after its
	// CapturedEvent; the handlers schedule them.
	tasks              Tasks
	flushTask          *kernel.Task[Frame, kernel.NoResult]
	releaseCaptureTask *kernel.Task[Buffer, kernel.NoResult]
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
}

// NewPlugin returns gfx.
func NewPlugin() *Plugin { return &Plugin{} }

// Name returns PluginName.
func (*Plugin) Name() kernel.PluginName { return PluginName }

// Dependencies names platform, which gfx reads windows from, and metronome,
// whose Clock DrawEvent's durations are timed by; gfx is listed after both.
func (*Plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{platform.PluginName, metronome.PluginName}
}

// Init declares the required InstanceSocket, installs the Device, empty
// until Start, with its stopwatches made from metronome's Clock, installs
// Tasks, and subscribes to RenderEvent and platform's
// window events, the Subs kept in the Handlers resource, and to its own
// private delivery of captures. A config of another
// type than *Config is a composition error, and panics.
func (p *Plugin) Init(configurator *kernel.Configurator, config any) error {
	p.config = *kernel.PluginConfigOrDefault[Config](config)
	p.instanceSocket = configurator.DeclareExclusiveSocket[InstanceSocket](false)
	clock, _ := configurator.Resource[metronome.Clock]()
	p.device = &device{
		surfaceRequest:      p.config.Surface,
		configurations:      map[platform.Window]SurfaceConfiguration{},
		acquireStopwatch:    clock.NewStopwatch(AcquireStopwatchName),
		submissionStopwatch: clock.NewStopwatch(SubmissionStopwatchName),
	}
	p.device.submissionStopwatch.Start()
	configurator.InitResource[Device](p.device)
	p.tasks = Tasks{
		CreateSurface:       configurator.RegisterTask2(createSurface),
		ReleaseSurface:      configurator.RegisterTask1(releaseSurface),
		ConfigureSurfaces:   configurator.RegisterTask2(configureSurfaces),
		AcquireFramebuffers: configurator.RegisterTask2(acquireFramebuffers),
		PresentFramebuffers: configurator.RegisterTask1(presentFramebuffers),
		ReleaseOpenSurfaces: configurator.RegisterTask1(releaseOpenSurfaces),
		ReleaseDevice:       configurator.RegisterTask1(releaseDevice),
		RecoverDevice:       configurator.RegisterTask1(recoverDevice),
	}
	configurator.InitResource(p.tasks)
	p.flushTask = configurator.RegisterTask1(flushFrame)
	p.releaseCaptureTask = configurator.RegisterTask1(releaseCapture)
	configurator.Subscribe(p.publishCapture)
	configurator.Subscribe(p.scheduleRelease)
	configurator.InitResource(Handlers{
		Draw: configurator.Subscribe(p.draw),
		CreateSurface: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event platform.WindowOpenedEvent) (platform.WindowOpenedEvent, kernel.JobHandle) {
			return event, ex.Schedule(p.tasks.CreateSurface, event.Window, in).Handle()
		}),
		ResizeSurface: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event platform.ResizeEvent) (platform.ResizeEvent, kernel.JobHandle) {
			p.device.frames.markUnconfigured(event.Window)
			return event, in
		}),
		ReleaseSurface: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event platform.WindowClosedEvent) (platform.WindowClosedEvent, kernel.JobHandle) {
			lastPresent := p.device.frames.markClosing(event.Window)
			return event, ex.Schedule(p.tasks.ReleaseSurface, event.Window, in, lastPresent).Handle()
		}),
	})
	return nil
}

// Start requests the adapter and the device from the bound InstanceSocket,
// with Config's limits or, when they are zero, DefaultLimits(), and keeps
// both requests for a recovery to make again. Finding no adapter, or no
// device, is an error.
func (p *Plugin) Start(k *kernel.Kernel) error {
	p.device.kernel = k
	p.device.instance = p.instanceSocket.Get()
	p.device.adapterOptions = RequestAdapterOptions{
		PowerPreference:      p.config.PowerPreference,
		ForceFallbackAdapter: p.config.ForceFallbackAdapter,
	}
	adapter, err := p.device.instance.RequestAdapter(p.device.adapterOptions)
	if err != nil {
		return fmt.Errorf("gfx: requesting an adapter: %w", err)
	}
	p.device.adapter = adapter
	limits := p.config.RequiredLimits
	if limits == (Limits{}) {
		limits = DefaultLimits()
	}
	p.device.deviceDescriptor = DeviceDescriptor{
		RequiredFeatures: p.config.RequiredFeatures,
		RequiredLimits:   limits,
	}
	backend, err := adapter.RequestDevice(p.device.deviceDescriptor)
	if err != nil {
		return fmt.Errorf("gfx: requesting a device: %w", err)
	}
	p.device.backend = backend
	p.device.features = p.config.RequiredFeatures
	p.device.limits = limits
	p.device.adapterInfo = adapter.Info()
	return nil
}

// Stop schedules Tasks.ReleaseOpenSurfaces after the last present, then
// Tasks.ReleaseDevice, each under the write lock, so the surfaces of the
// windows still open and then the device are released on the thread a
// backend pins them to, before any Destroy runs. Frames still in flight
// finish first, and a beat after the release draws nothing.
func (*Plugin) Stop(k *kernel.Kernel) error {
	k.Publish(stopEvent{})
	return nil
}

// Destroy, after the kernel's last job, releases what Tasks.ReleaseDevice
// did not, as it does, answering the unmaps' errors: the adapter and the
// instance a Start that failed at the device requested, since no Stop runs
// for it. Releasing the device cleanly is what saves Vulkan's pipeline
// cache.
func (p *Plugin) Destroy() error {
	return p.device.release()
}
