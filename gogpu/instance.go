package gogpu

import (
	"errors"
	"fmt"
	"io/fs"
	"runtime"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gogpu/shader"
	"github.com/dvoyni/cog/platform"
	host "github.com/gogpu/gogpu"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// instance is the plugin's gfx.InstanceSocket: it starts the fork's HostLoop
// at gfx's adapter request, and stops it at gfx's release of the instance.
type instance struct {
	// plugin holds the HostLoop and the windows the instance shares with the
	// window system.
	plugin *Plugin
	// backend is Config.Backend.
	backend gfx.Backend
}

// RequestAdapter starts the HostLoop, with no device and no window, on the
// thread gfx's Start runs on, which is main, and answers gogpu's adapter on
// Config's backend with options' power preference, or wgpu's software adapter
// when options force the fallback. A device replacement is not built yet: a
// second request answers an error. gogpu keeps running with no window open,
// and reports to the window system a window it closes by itself, and to the
// input source each change of the keyboard layout.
func (i instance) RequestAdapter(options gfx.RequestAdapterOptions) (gfx.BackendAdapter, error) {
	if i.plugin.loop != nil {
		return nil, errors.New("gogpu: a device replacement is not built yet")
	}
	graphicsAPI := host.GraphicsAPIVulkan
	switch i.backend {
	case gfx.BackendEmpty:
		if runtime.GOOS == "darwin" {
			graphicsAPI = host.GraphicsAPIMetal
		}
	case gfx.BackendVulkan:
	case gfx.BackendMetal:
		graphicsAPI = host.GraphicsAPIMetal
	case gfx.BackendDX12:
		graphicsAPI = host.GraphicsAPIDX12
	default:
		return nil, fmt.Errorf("gogpu: backend %d is GL or the browser's WebGPU, which the plugin never chooses", i.backend)
	}
	app := host.NewApp(host.Config{
		GraphicsAPI:          graphicsAPI,
		PowerPreference:      gputypes.PowerPreference(options.PowerPreference),
		ForceFallbackAdapter: options.ForceFallbackAdapter,
	}).SetQuitOnLastWindowClosed(false).
		OnAnyWindowClosed(windowSystem{plugin: i.plugin}.reportClosed).
		OnKeyboardLayoutChanged(inputSource{plugin: i.plugin}.reportLayoutChanged)
	loop := host.NewHostLoop(app)
	if err := loop.Start(); err != nil {
		return nil, err
	}
	i.plugin.loop = loop
	i.plugin.app = app
	i.plugin.windows = map[platform.Window]*host.Window{}
	return &adapter{loop: loop, app: app, windows: i.plugin.windows, adapter: wgpu.AdapterFromHandle(app.GPUContextProvider().Adapter())}, nil
}

// CreateSurface does nothing: gogpu made the window's surface when it opened
// it.
func (instance) CreateSurface(platform.Window, platform.NativeHandle) error { return nil }

// ReleaseSurface does nothing: the window's surface goes with it.
func (instance) ReleaseSurface(platform.Window) {}

// PreprocessShader answers gogpu/shader's Preprocess of the shader at path in
// files.
func (instance) PreprocessShader(files fs.FS, path string, defines []gfx.ShaderDefine) (string, error) {
	return shader.Preprocess(files, path, defines)
}

// Release stops the HostLoop, which closes every window still open and
// releases gogpu's device, adapter and platform, and forgets the App, the
// windows and what they had left to report, from gfx's Tasks.ReleaseDevice,
// pinned to the main thread, or from gfx's Destroy after a Start that failed,
// which runs on Spawn's thread.
func (i instance) Release() {
	if i.plugin.loop != nil {
		i.plugin.loop.Stop()
		i.plugin.loop = nil
		i.plugin.app = nil
	}
	i.plugin.windows = nil
	i.plugin.changes = nil
	i.plugin.inputChanges = nil
}
