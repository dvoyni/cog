// Package gogpu is the plugin that gives a Cog kernel a real GPU: it
// implements gfx's backend over our gogpu fork, driving the fork's HostLoop
// from the kernel's main thread.
package gogpu

import (
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"slices"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gogpu/shader"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
	host "github.com/gogpu/gogpu"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// PluginName is the gogpu plugin's name.
const PluginName kernel.PluginName = "gogpu"

// Plugin is the gogpu plugin: it binds itself to gfx.InstanceSocket and
// platform.WindowsSocket, starts the fork's HostLoop at gfx's adapter request
// and stops it at gfx's release of the instance, which it pins to the main
// thread. platform calls its window methods on the main thread alone.
type Plugin struct {
	// config is a copy of the Config Init was given.
	config Config
	// loop is the fork's HostLoop, started by the adapter request; nil before
	// it and after Release.
	loop *host.HostLoop
	// windows maps each window open to gogpu's.
	windows map[platform.Window]*host.Window
	// changes is what the next Pump reports, in the order it happened.
	changes []platform.InputChange
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

// Init binds the plugin to gfx.InstanceSocket and platform.WindowsSocket,
// pins gfx's Tasks.ReleaseDevice to the main thread, where the HostLoop
// stops, and opens Config.MainWindow in platform's WindowManager, which the
// first ProcessEvents run applies. A config of another type than *Config is a
// composition error, and panics.
func (p *Plugin) Init(configurator *kernel.Configurator, config any) error {
	p.config = *kernel.PluginConfigOrDefault[Config](config)
	configurator.BindSocket[gfx.InstanceSocket](p)
	configurator.BindSocket[platform.WindowsSocket](p)
	tasks, _ := configurator.Resource[gfx.Tasks]()
	configurator.PinToMainThread(tasks.ReleaseDevice)
	if p.config.MainWindow != (platform.WindowEntry{}) {
		manager, _ := configurator.Resource[*platform.WindowManager]()
		manager.Open(p.config.MainWindow)
	}
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
// second request answers an error. gogpu keeps running with no window open,
// and reports to the plugin a window it closes by itself.
func (p *Plugin) RequestAdapter(options gfx.RequestAdapterOptions) (gfx.BackendAdapter, error) {
	if p.loop != nil {
		return nil, errors.New("gogpu: a device replacement is not built yet")
	}
	graphicsAPI := host.GraphicsAPIVulkan
	switch p.config.Backend {
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
		return nil, fmt.Errorf("gogpu: backend %d is GL or the browser's WebGPU, which the plugin never chooses", p.config.Backend)
	}
	app := host.NewApp(host.Config{
		GraphicsAPI:          graphicsAPI,
		PowerPreference:      gputypes.PowerPreference(options.PowerPreference),
		ForceFallbackAdapter: options.ForceFallbackAdapter,
	}).SetQuitOnLastWindowClosed(false).OnAnyWindowClosed(p.reportClosed)
	loop := host.NewHostLoop(app)
	if err := loop.Start(); err != nil {
		return nil, err
	}
	p.loop = loop
	p.windows = map[platform.Window]*host.Window{}
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

// Open opens window with its surface through the HostLoop, at entry's title,
// size and fullscreen, gogpu's default size for a zero width or height, and
// shows it unless entry hides it. It reports the window open from the next
// Pump, at the scale its framebuffer's width gives. The user's close of the
// window is reported, and keeps it open. It answers an error before gfx's
// adapter request, which starts the HostLoop.
func (p *Plugin) Open(window platform.Window, entry platform.WindowEntry) error {
	if p.loop == nil {
		return errors.New("gogpu: a window opens only once gfx has requested the adapter")
	}
	config := host.DefaultConfig()
	config.Title = entry.Title
	if entry.Size.Width > 0 && entry.Size.Height > 0 {
		config.Width = entry.Size.Width
		config.Height = entry.Size.Height
	}
	config.Fullscreen = entry.Fullscreen
	opened, err := p.loop.NewWindow(config)
	if err != nil {
		return err
	}
	opened.SetOnClose(func() bool { return p.requestClose(window) })
	if !entry.Hidden {
		opened.Show()
	}
	p.windows[window] = opened
	width, height := opened.Size()
	framebufferWidth, framebufferHeight := opened.PhysicalSize()
	scale := float32(1)
	if width > 0 && framebufferWidth > 0 {
		scale = float32(framebufferWidth) / float32(width)
	}
	p.changes = append(p.changes, platform.InputChange{
		Kind:            platform.InputChangeWindowOpened,
		Window:          window,
		Size:            platform.Size{Width: width, Height: height},
		FramebufferSize: platform.Size{Width: framebufferWidth, Height: framebufferHeight},
		Scale:           scale,
	})
	return nil
}

// Change answers an error naming it: window changes are not built yet.
func (*Plugin) Change(platform.Window, platform.WindowEntry, platform.WindowEntry) error {
	return unbuilt("Change")
}

// SetCursor answers an error naming it: the cursor is not built yet.
func (*Plugin) SetCursor(platform.Window, platform.CursorRequest) error {
	return unbuilt("SetCursor")
}

// EffectiveCursorMode answers platform.CursorModeNormal: the cursor is not
// built yet.
func (*Plugin) EffectiveCursorMode(platform.Window) platform.CursorMode {
	return platform.CursorModeNormal
}

// SetCursorPosition answers an error naming it: the cursor is not built yet.
func (*Plugin) SetCursorPosition(platform.Window, platform.Point) error {
	return unbuilt("SetCursorPosition")
}

// Close destroys window's gogpu window at once, with its surface, and drops
// what the next Pump would have reported of it. It reports nothing, as
// platform.WindowsSocket says; a window not open is left alone.
func (p *Plugin) Close(window platform.Window) error {
	closed, ok := p.windows[window]
	if !ok {
		return nil
	}
	delete(p.windows, window)
	p.changes = slices.DeleteFunc(p.changes, func(change platform.InputChange) bool { return change.Window == window })
	closed.Destroy()
	return nil
}

// Pump has the HostLoop pump the operating system's events, once it is
// started, then appends to changes what happened to the windows since the
// last Pump, and answers it.
func (p *Plugin) Pump(changes []platform.InputChange) []platform.InputChange {
	if p.loop != nil {
		p.loop.PumpEvents()
	}
	changes = append(changes, p.changes...)
	p.changes = p.changes[:0]
	return changes
}

// Release stops the HostLoop, which closes every window still open and
// releases gogpu's device, adapter and platform, from gfx's
// Tasks.ReleaseDevice, pinned to the main thread, or from gfx's Destroy after
// a Start that failed, which runs on Spawn's thread.
func (p *Plugin) Release() {
	if p.loop != nil {
		p.loop.Stop()
		p.loop = nil
	}
	p.windows = nil
	p.changes = nil
}

// requestClose reports the user's close of window from the next Pump, and
// answers false, which keeps it open: closing it is the application's.
func (p *Plugin) requestClose(window platform.Window) bool {
	p.changes = append(p.changes, platform.InputChange{Kind: platform.InputChangeCloseRequested, Window: window})
	return false
}

// reportClosed reports from the next Pump the window gogpu closed by itself,
// which closed names. A window Close destroyed is forgotten first, and so
// not reported.
func (p *Plugin) reportClosed(closed host.WindowID) {
	for window, opened := range p.windows {
		if opened.ID() == closed {
			delete(p.windows, window)
			p.changes = append(p.changes, platform.InputChange{Kind: platform.InputChangeWindowClosed, Window: window})
			return
		}
	}
}
