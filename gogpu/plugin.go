// Package gogpu is the plugin that gives a Cog kernel a real GPU: it
// implements gfx's backend over our gogpu fork, driving the fork's HostLoop
// from the kernel's main thread.
package gogpu

import (
	"errors"
	"fmt"
	"io/fs"
	"runtime"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gogpu/shader"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
	fork "github.com/gogpu/gogpu"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// PluginName is the gogpu plugin's name.
const PluginName kernel.PluginName = "gogpu"

// Plugin is the gogpu plugin: it binds itself to gfx.InstanceSocket, starts
// the fork's HostLoop at gfx's adapter request and stops it at gfx's release
// of the instance, which it pins to the main thread.
type Plugin struct {
	// config is a copy of the Config Init was given.
	config Config
	// loop is the fork's HostLoop, started by the adapter request; nil before
	// it and after Release.
	loop *fork.HostLoop
}

// Config is the gogpu plugin's part of the kernel's config, under
// PluginName.
type Config struct {
	// Backend is the graphics API the adapter is requested on. Zero is
	// gogpu's choice on this system, GL excluded: Metal on macOS, Vulkan
	// elsewhere. GL and the browser's WebGPU are never chosen: asking for
	// either fails the adapter's request.
	Backend gfx.Backend
}

// NewPlugin returns the gogpu plugin.
func NewPlugin() *Plugin { return &Plugin{} }

// Name returns PluginName.
func (*Plugin) Name() kernel.PluginName { return PluginName }

// Dependencies names gfx, whose Tasks Init pins.
func (*Plugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{gfx.PluginName} }

// Init binds the plugin to gfx.InstanceSocket, and pins gfx's
// Tasks.ReleaseDevice to the main thread, where the HostLoop stops. A config
// of another type than *Config is a composition error, and panics.
func (p *Plugin) Init(configurator *kernel.Configurator, config any) error {
	p.config = *kernel.PluginConfigOrDefault[Config](config)
	configurator.BindSocket[gfx.InstanceSocket](p)
	tasks, _ := configurator.Resource[gfx.Tasks]()
	configurator.PinToMainThread(tasks.ReleaseDevice)
	return nil
}

// Start does nothing: the HostLoop starts at gfx's adapter request.
func (*Plugin) Start(*kernel.Kernel) error { return nil }

// Stop does nothing: the HostLoop stops at gfx's release of the instance.
func (*Plugin) Stop(*kernel.Kernel) error { return nil }

// Destroy does nothing.
func (*Plugin) Destroy() error { return nil }

// RequestAdapter starts the HostLoop, with no device and no window, on the
// thread gfx's Start runs on, which is main, and answers gogpu's adapter on
// Config's backend with options' power preference, or wgpu's software adapter
// when options force the fallback. A device replacement is not built yet: a
// second request answers an error.
func (p *Plugin) RequestAdapter(options gfx.RequestAdapterOptions) (gfx.BackendAdapter, error) {
	if p.loop != nil {
		return nil, errors.New("gogpu: a device replacement is not built yet")
	}
	graphicsAPI := fork.GraphicsAPIVulkan
	switch p.config.Backend {
	case gfx.BackendEmpty:
		if runtime.GOOS == "darwin" {
			graphicsAPI = fork.GraphicsAPIMetal
		}
	case gfx.BackendVulkan:
	case gfx.BackendMetal:
		graphicsAPI = fork.GraphicsAPIMetal
	case gfx.BackendDX12:
		graphicsAPI = fork.GraphicsAPIDX12
	default:
		return nil, fmt.Errorf("gogpu: backend %d is GL or the browser's WebGPU, which the plugin never chooses", p.config.Backend)
	}
	app := fork.NewApp(fork.Config{
		GraphicsAPI:          graphicsAPI,
		PowerPreference:      gputypes.PowerPreference(options.PowerPreference),
		ForceFallbackAdapter: options.ForceFallbackAdapter,
	})
	loop := fork.NewHostLoop(app)
	if err := loop.Start(); err != nil {
		return nil, err
	}
	p.loop = loop
	return &adapter{loop: loop, app: app, adapter: wgpu.AdapterFromHandle(app.GPUContextProvider().Adapter())}, nil
}

// CreateSurface answers an error naming it: surfaces are not built yet.
func (*Plugin) CreateSurface(platform.Window, platform.NativeHandle) error {
	return unbuilt("CreateSurface")
}

// ReleaseSurface does nothing: surfaces are not built yet.
func (*Plugin) ReleaseSurface(platform.Window) {}

// PreprocessShader answers gogpu/shader's Preprocess of the shader at path in
// files.
func (*Plugin) PreprocessShader(files fs.FS, path string, defines []gfx.ShaderDefine) (string, error) {
	return shader.Preprocess(files, path, defines)
}

// Release stops the HostLoop, which releases gogpu's device, adapter and
// platform, from gfx's Tasks.ReleaseDevice, pinned to the main thread, or
// from gfx's Destroy after a Start that failed, which runs on Spawn's
// thread.
func (p *Plugin) Release() {
	if p.loop != nil {
		p.loop.Stop()
		p.loop = nil
	}
}
