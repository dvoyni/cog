package internal

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/dvoyni/cog/bundles/input"
	cgogpu "github.com/dvoyni/cog/extensions/gogpu"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
	cgfx "github.com/dvoyni/cog/slots/gfx"
	"github.com/gogpu/gogpu"
)

// plugin is the gogpu driver. Its configuration arrives through the configured
// map under cgogpu.Name.
type plugin struct {
	config cgogpu.Config
	gpu    *gogpu.App

	// loop is app's half of the application loop, attached by app from its
	// Start, before Run. The main and render threads only read it.
	loop app.Loop

	// Frame clock: onDraw (render thread, paced to real vsync/rAF, so accurate
	// across JS turns) measures the true per-frame dt; onUpdate (main thread, called
	// at an unreliable rate under gogpu's busy loop, whose time deltas are quantized
	// on wasm) consumes it via the frame sequence and hands it to the Loop's fixed
	// step. lastDraw is render-thread-only; frameDtBits/frameSeq are atomic;
	// lastFrameSeq is main-thread-only.
	// looping is true while the platform loop is running, so Quit asks the App
	// to leave a loop it is actually in. Quit arrives on another goroutine and
	// may arrive before Run entered the loop or after it left.
	looping atomic.Bool

	lastDraw     time.Time
	frameDtBits  atomic.Uint64
	frameSeq     atomic.Uint64
	lastFrameSeq uint64
	windowWidth  int
	windowHeight int

	// pending holds input changes accumulated from gogpu's EventSource callbacks
	// (main thread), flushed once per frame in onUpdate. Main-thread-only; no lock.
	pending []input.Change

	// gfxBackend is gfx's Backend adapter: provided at registration, and
	// attached to the device once the device exists.
	gfxBackend *gfxBackend
}

// backendAttachFailureKey names the one condition gogpu reports once per
// engine: the device never became attachable. It is its own type rather than a
// bare struct{} because the kernel keys its report-once table by the boxed
// key's dynamic type across every plugin, so two singletons sharing struct{}
// would be one singleton.
type backendAttachFailureKey struct{}

// Ensure plugin satisfies the host contract (owns the main thread).
var _ kernel.PluginHost = (*plugin)(nil)

// New creates the gogpu plugin. Its cgogpu.Config is supplied at Register
// through kernel.New's config map, so New takes no arguments.
func New() kernel.Plugin {
	return &plugin{}
}

// Name reports the plugin name.
func (p *plugin) Name() kernel.PluginName {
	return cgogpu.Name
}

// Dependencies reports the plugins this plugin requires: gfx (whose viewport it
// drives, and whose Backend adapter it provides) and input (to which it
// forwards OS input events). The app MainLoop it provides binds without a
// dependency: it dispatches none of app's commands, and app attaches its Loop
// from Start, which precedes Run whatever the plugin order.
func (p *plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{cgfx.Name, input.Name}
}

// Register provides gfx's Backend adapter and app's MainLoop, resolves the
// configuration (nil -> the zero cgogpu.Config, and every zero field takes its
// default), and builds the gogpu App. It does not block; the main loop starts
// in Run.
//
// The adapter is provided before the device exists, because a Port's adapter is
// bound at composition and the device is created asynchronously inside the
// render loop. It is not Ready until onDraw attaches the device.
func (p *plugin) Register(registrar *kernel.Registrar, config any) error {
	if p.gfxBackend == nil {
		p.gfxBackend = newGfxBackend()
	}
	registrar.ProvideAdapter[cgogpu.GfxBackend](cgfx.Backend(p.gfxBackend))
	registrar.ProvideAdapter[cgogpu.AppMainLoop](app.MainLoop(mainLoop{p}))
	var cfg cgogpu.Config
	if config != nil {
		c, ok := config.(cgogpu.Config)
		if !ok {
			return cgogpu.ErrInvalidConfig{Got: config}
		}
		cfg = c
	}
	cfg = withDefaults(cfg)
	p.config = cfg

	p.gpu = gogpu.NewApp(gogpuConfig(cfg))
	return nil
}

// Run owns the calling (main) thread: it wires the gogpu callbacks to k, has the
// Loop app attached publish app.InitEvent, then runs the App's blocking main
// loop, and has the Loop publish app.QuitEvent once it returns. Run returns
// when the window closes, or when Quit asks the App to leave the loop, after
// which the engine shuts down. The callbacks are wired here rather than in
// Register because that is where a Kernel first exists; the captured value is
// immutable, so the main and render threads share it safely.
func (p *plugin) Run(k kernel.Executioner) error {
	// Frame clock: hand gogpu's variable OnUpdate to the Loop's fixed step.
	p.gpu.OnUpdate(func(dt float64) { p.onUpdate(k, dt) })
	// Per-frame render barrier (app.RenderEvent on the render thread).
	p.gpu.OnDraw(func(dc *gogpu.Context) { p.onDraw(k, dc) })
	// Bridge gogpu input events into the input contract.
	p.wireInput(k)

	if err := p.loop.Init(k); err != nil {
		return err
	}
	defer p.loop.Quit(k)
	p.looping.Store(true)
	err := p.gpu.Run()
	p.looping.Store(false)
	return err
}

// Quit asks the gogpu App to leave its platform loop, which is what returns
// Run. The engine calls it to stop a running engine, from a goroutine that is
// not the one inside Run, so it checks that there is a loop to leave: a Quit
// arriving before Run entered the loop, or after it left, has nothing to do.
func (p *plugin) Quit() {
	if p.looping.Load() {
		p.gpu.Quit()
	}
}

// Stop abandons any readback the window closed on. A capture armed in the
// last frame has no further submit to resolve against, so its staging buffer
// and pending map are released here and the reason takes their place; gfx's
// own Stop is what delivers that reason to whoever armed it.
func (p *plugin) Stop(kernel.Executioner) error {
	if p.gfxBackend != nil {
		p.gfxBackend.captures.abandon()
	}
	return nil
}


// onUpdate runs on the gogpu (main) thread each frame. It flushes batched input,
// then hands the Loop the real time of any newly rendered frames (measured in
// onDraw), because gogpu's deltaTime is 0 on wasm and onUpdate's own time deltas
// are quantized within gogpu's busy-loop burst. The flush comes first so every
// tick the frame publishes sees that input, and it runs whether or not the tick
// source is paused, because pause stops the tick and not the frame.
func (p *plugin) onUpdate(k kernel.Executioner, _ float64) {
	p.flushInput(k)
	p.loop.Frame(k, p.consumeFrameTime())
}

// consumeFrameTime reports the real time of the frames rendered since the last
// call, and marks them consumed. It runs whether or not the tick source is
// paused: a paused engine keeps drawing, and the Loop discards the time a paused
// frame hands it, so no pause turns into a catch-up burst on resume.
func (p *plugin) consumeFrameTime() float64 {
	seq := p.frameSeq.Load()
	if seq <= p.lastFrameSeq {
		return 0
	}
	dt := math.Float64frombits(p.frameDtBits.Load()) * float64(seq-p.lastFrameSeq)
	p.lastFrameSeq = seq
	return dt
}

// onDraw runs on the gogpu render thread each frame. It measures the frame
// clock, reports a window size change to the Loop, resolves the viewport,
// attaches the backend to the surface, and then has the Loop publish
// app.RenderEvent as a barrier, in whose handler gfx replays the latest
// completed frame onto the surface view. gogpu presents the surface after
// onDraw returns.
func (p *plugin) onDraw(k kernel.Executioner, dc *gogpu.Context) {
	// Measure the true per-frame dt from the render cadence (paced across JS turns,
	// so accurate on wasm) and publish it for onUpdate to hand to the Loop.
	if now := time.Now(); !p.lastDraw.IsZero() {
		p.frameDtBits.Store(math.Float64bits(now.Sub(p.lastDraw).Seconds()))
		p.frameSeq.Add(1)
		p.lastDraw = now
	} else {
		p.lastDraw = now
	}

	windowW, windowH := dc.Size()
	if windowW != p.windowWidth || windowH != p.windowHeight {
		p.windowWidth, p.windowHeight = windowW, windowH
		p.loop.WindowSize(k, float32(windowW), float32(windowH))
	}

	// SurfaceView forces gogpu's lazy frame start and returns the frame's render
	// target; nil means the surface is not ready yet, so skip this frame.
	view := dc.SurfaceView()
	if view == nil {
		return
	}
	fbW, fbH := dc.FramebufferSize()
	k.ExecuteCommand[cgfx.SetViewportCmd](
		cgfx.SetViewportRequest{
			Width: float32(windowW), Height: float32(windowH),
			FramebufferWidth: float32(fbW), FramebufferHeight: float32(fbH),
		})

	// Make the surface current on the backend, then have the Loop publish
	// app.RenderEvent — the gfx plugin renders in its render-thread handler.
	if !p.gfxBackend.Ready() {
		if err := p.gfxBackend.attach(p.gpu.DeviceProvider(), dc.Backend()); err != nil {
			// The device is created asynchronously, so this is expected until it is
			// ready; report once so a permanent failure is still visible. The frame
			// is skipped: nothing is rendered before the backend is Ready.
			k.ReportErrorOnce(backendAttachFailureKey{}, err)
			return
		}
	}
	// A pass the backend declined to encode is reported here rather than on the
	// render thread, which has no kernel handle. It fires once per run.
	if err := p.gfxBackend.takeRefusal(); err != nil {
		k.ReportError(err)
	}
	p.gfxBackend.setScreen(view, fbW, fbH)
	p.loop.Render(k)
}
