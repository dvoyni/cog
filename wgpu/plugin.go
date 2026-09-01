// Package wgpu is cog's window + input + GPU driver plugin, built on gogpu. It is
// a kernel.PluginHost: it owns the OS main loop and drives the engine's
// fixed-timestep Update and per-frame Render events (declared in package app).
//
// gogpu's OnUpdate becomes ordered fixed-timestep app.UpdateEvent values through an
// accumulator, and OnDraw publishes app.RenderEvent{Alpha} as a render-thread
// barrier. The plugin also implements gfx.Backend, forwards OS input into the
// input contract, and reports window and framebuffer sizes to the viewport.
package wgpu

import (
	"context"
	"math"
	"sync/atomic"
	"time"

	"github.com/dvoyni/cog/app"
	cgfx "github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/input"
	"github.com/dvoyni/cog/kernel"
	"github.com/gogpu/gogpu"
)

// Name is the plugin name wgpu registers under; it is also its key in the config
// map passed to kernel.New.
const Name kernel.PluginName = "wgpu"

// Plugin is the wgpu driver. Hand it to kernel.New; its configuration arrives
// through the configured map under Name.
type Plugin struct {
	config Config
	gpu    *gogpu.App

	// accum is the unspent frame time (seconds); touched only on the gogpu OnUpdate
	// (main) thread. alpha is the render interpolation factor (atomic float64 bits).
	accum float64
	alpha atomic.Uint64

	// Frame clock: onDraw (render thread, paced to real vsync/rAF, so accurate
	// across JS turns) measures the true per-frame dt; onUpdate (main thread, called
	// at an unreliable rate under gogpu's busy loop, whose time deltas are quantized
	// on wasm) consumes it via the frame sequence to drive the fixed-step. lastDraw
	// is render-thread-only; frameDtBits/frameSeq are atomic; lastFrameSeq is
	// main-thread-only.
	lastDraw     time.Time
	frameDtBits  atomic.Uint64
	frameSeq     atomic.Uint64
	lastFrameSeq uint64
	windowWidth  int
	windowHeight int

	// pending holds input changes accumulated from gogpu's EventSource callbacks
	// (main thread), flushed once per frame in onUpdate. Main-thread-only; no lock.
	pending []input.Change

	gfxBackend             *gfxBackend
	reportedBackendFailure bool
}

// Ensure Plugin satisfies the host contract (owns the main thread).
var _ kernel.PluginHost = (*Plugin)(nil)

// New creates a wgpu plugin. Its Config is supplied at Init through kernel.New's
// config map, so New takes no arguments.
func New() *Plugin {
	return &Plugin{}
}

// Name reports the plugin name.
func (p *Plugin) Name() kernel.PluginName {
	return Name
}

// Dependencies reports the plugins wgpu requires: gfx (whose backend and
// viewport it drives) and input (to which it forwards OS input events).
func (p *Plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{cgfx.Name, input.Name}
}

// Register resolves the configuration (nil -> DefaultConfig, otherwise the provided
// wgpu.Config — build it from DefaultConfig via the With* setters), builds the
// gogpu App, and registers its commands. It does not block; the main loop starts
// in Run.
func (p *Plugin) Register(registrar *kernel.Registrar, config any) error {
	cfg := DefaultConfig()
	if config != nil {
		c, ok := config.(Config)
		if !ok {
			return ErrInvalidConfig{Got: config}
		}
		cfg = c
	}
	p.config = cfg

	p.gpu = gogpu.NewApp(cfg.gogpuConfig())
	// Bridge gogpu input events into the input contract.
	p.wireInput()

	registrar.HandleCommand[app.QuitCmd](p.quit)
	return nil
}

func (p *Plugin) quit() (kernel.Lock, kernel.Execute[app.QuitRequest, app.QuitResponse]) {
	return nil, func(kernel.Kernel, app.QuitRequest) (app.QuitResponse, error) {
		p.gpu.Quit()
		return app.QuitResponse{}, nil
	}
}

// Run owns the calling (main) thread: it wires the gogpu callbacks to k, starts a
// watcher that quits the gogpu App when the engine's context is canceled, then
// runs the App's blocking main loop. Run returns when the window closes (or the
// app quits), after which the engine shuts down. The callbacks are wired here
// rather than in Register because that is where a Kernel first exists; the
// captured value is immutable, so the main and render threads share it safely.
func (p *Plugin) Run(k kernel.Executioner) error {
	ctx := k.Context()
	// Fixed-timestep accumulator: turn gogpu's variable OnUpdate into fixed steps.
	p.gpu.OnUpdate(func(dt float64) { p.onUpdate(k, dt) })
	// Per-frame render barrier (publishes app.RenderEvent on the render thread).
	p.gpu.OnDraw(func(dc *gogpu.Context) { p.onDraw(k, dc) })

	if err := k.PublishEvent(app.InitEvent{}).Wait(); err != nil {
		return err
	}
	defer func() { _ = k.PublishEvent(app.QuitEvent{}).Wait() }()
	runDone := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		quitOnCancellation(ctx, runDone, p.gpu.Quit)
	}()
	err := p.gpu.Run()
	close(runDone)
	<-watcherDone
	return err
}

func quitOnCancellation(ctx context.Context, runDone <-chan struct{}, quit func()) {
	select {
	case <-runDone:
		return
	case <-ctx.Done():
		select {
		case <-runDone:
			return
		default:
			quit()
		}
	}
}

// onUpdate runs on the gogpu (main) thread each frame. It flushes batched input,
// then publishes one fixed app.UpdateEvent per whole Step accumulated. It advances
// the accumulator by the real time of any newly rendered frames (measured in
// onDraw), because gogpu's deltaTime is 0 on wasm and onUpdate's own time deltas
// are quantized within gogpu's busy-loop burst. Publishing on the main thread —
// not a separate goroutine — avoids starving the game under gogpu's busy loop.
func (p *Plugin) onUpdate(k kernel.Executioner, _ float64) {
	p.flushInput(k)
	var dt float64
	if seq := p.frameSeq.Load(); seq > p.lastFrameSeq {
		dt = math.Float64frombits(p.frameDtBits.Load()) * float64(seq-p.lastFrameSeq)
		p.lastFrameSeq = seq
	}
	n := p.accumulate(dt)
	e := app.UpdateEvent{Dt: p.config.Step.Seconds()}
	for ; n > 0; n-- {
		e.Last = n == 1
		_ = k.PublishEvent(e).Wait()
	}
}

// accumulate folds dt (clamped to MaxFrame) into the fixed-step accumulator and
// returns how many app.UpdateEvent values to publish this frame — capped at
// MaxPending, with excess whole steps dropped to stay near real-time rather than
// spiralling. The leftover fraction is stored as the render interpolation alpha.
func (p *Plugin) accumulate(dt float64) int {
	step := p.config.Step.Seconds()
	if step <= 0 {
		return 0
	}
	if maxFrame := p.config.MaxFrame.Seconds(); maxFrame > 0 && dt > maxFrame {
		dt = maxFrame
	}
	p.accum += dt
	maxSteps := p.config.MaxPending
	if maxSteps < 1 {
		maxSteps = 1
	}
	steps := 0
	for p.accum >= step {
		p.accum -= step
		if steps < maxSteps {
			steps++
		}
	}
	p.storeAlpha(p.accum / step)
	return steps
}

// onDraw runs on the gogpu render thread each frame. It drains pending texture
// uploads (GPU resource creation must stay on this thread), publishes app.RenderEvent as
// a barrier, then consumes the latest completed frame and replays its ops as
// native GPU calls onto the surface view. gogpu presents the surface after
// onDraw returns. Alpha is the interpolation factor from the last update.
func (p *Plugin) onDraw(k kernel.Executioner, dc *gogpu.Context) {
	// Measure the true per-frame dt from the render cadence (paced across JS turns,
	// so accurate on wasm) and publish it for onUpdate's fixed-step accumulator.
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
		_ = k.PublishEvent(app.WindowSizeChangeEvent{
			Width: float32(windowW), Height: float32(windowH),
		}).Wait()
	}

	// SurfaceView forces gogpu's lazy frame start and returns the frame's render
	// target; nil means the surface is not ready yet, so skip this frame.
	view := dc.SurfaceView()
	if view == nil {
		return
	}
	fbW, fbH := dc.FramebufferSize()
	k.ExecuteCommand[app.SetViewportCmd](
		app.SetViewportRequest{
			Width: float32(windowW), Height: float32(windowH),
			FramebufferWidth: float32(fbW), FramebufferHeight: float32(fbH),
		})

	// Make the surface current on the backend, then publish app.RenderEvent — the
	// gfx plugin renders in its render-thread handler.
	if p.gfxBackend == nil {
		backend, err := newGfxBackend(p.gpu.DeviceProvider())
		if err != nil {
			// The device is created asynchronously, so this is expected until it is
			// ready; report once so a permanent failure is still visible.
			if !p.reportedBackendFailure {
				p.reportedBackendFailure = true
				k.ReportError(err)
			}
			return
		}
		p.gfxBackend = backend
		k.ExecuteCommand[cgfx.SetBackendCmd](cgfx.SetBackendRequest{Backend: backend})
	}
	p.gfxBackend.setScreen(view, fbW, fbH)
	_ = k.PublishEvent(app.RenderEvent{Alpha: p.loadAlpha()}).Wait()

	// Return control to the browser event loop so it composites (presents) the
	// frame we just submitted, and so wall-clock advances for the next update.
	// No-op on desktop.
	yieldMainThread()
}

// storeAlpha and loadAlpha carry the render interpolation factor across the
// main->render thread boundary as an atomically-stored float64.
func (p *Plugin) storeAlpha(a float64) {
	p.alpha.Store(math.Float64bits(a))
}

func (p *Plugin) loadAlpha() float64 {
	return math.Float64frombits(p.alpha.Load())
}
