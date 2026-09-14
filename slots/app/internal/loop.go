package internal

import (
	"math"
	"sync/atomic"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// loop is app's half of the application loop, and the app.Loop a driver
// drives. It turns the driver's variable frames into ordered fixed-step
// app.UpdateEvents through an accumulator, and publishes every other app event
// when the driver reports the moment it names.
//
// Its threading is the driver's: Frame runs on the driver's main thread and
// Render on its render thread, possibly at the same time. accum is touched only
// by Frame; alpha crosses to Render as atomic float64 bits; the tick source's
// own state is atomics that a command handler on any goroutine writes.
type loop struct {
	config app.Config

	// accum is the unspent frame time in seconds. Main-thread-only.
	accum float64
	// alpha is the render interpolation factor, as atomic float64 bits.
	alpha atomic.Uint64

	// ticks is the tick source: what decides when an update tick is published.
	ticks tickSource
}

var _ app.Loop = (*loop)(nil)

func newLoop(config app.Config) *loop { return &loop{config: config} }

// Init publishes app.InitEvent and waits for its subscribers.
func (l *loop) Init(k kernel.Executioner) error {
	return k.PublishEvent(app.InitEvent{}).Wait()
}

// Quit publishes app.QuitEvent and waits for its subscribers.
func (l *loop) Quit(k kernel.Executioner) {
	_ = k.PublishEvent(app.QuitEvent{}).Wait()
}

// WindowSize publishes app.WindowSizeChangeEvent and waits for its subscribers.
func (l *loop) WindowSize(k kernel.Executioner, width, height float32) {
	_ = k.PublishEvent(app.WindowSizeChangeEvent{Width: width, Height: height}).Wait()
}

// Frame publishes one fixed app.UpdateEvent per whole Step accumulated, each
// waited for in turn. Publishing on the driver's main thread — not a separate
// goroutine — avoids starving the game under a busy platform loop.
//
// While paused the frame's time is discarded rather than accumulated, so
// nothing is banked and a resume costs no catch-up ticks, and the only ticks
// published are the steps somebody asked for. Everything else the driver does
// this frame runs exactly as it does while running, because pause stops the
// tick and not the frame.
func (l *loop) Frame(k kernel.Executioner, dt float64) {
	steps, paused, batch := l.ticks.take()
	if !paused {
		steps = l.accumulate(dt)
	}
	e := app.UpdateEvent{Dt: l.config.Step.Seconds()}
	for n := steps; n > 0; n-- {
		// Every step is the last of its frame, so once-per-frame subscribers
		// do their work and each step produces a complete frame; rendering
		// then shows the last of them.
		e.Last = paused || n == 1
		// Every tick carries its own number, so that whatever a subscriber
		// records inside one can name the tick it describes.
		e.Tick = l.ticks.next()
		_ = k.PublishEvent(e).Wait()
	}
	if paused {
		l.ticks.published(steps, batch)
	}
}

// Render publishes app.RenderEvent carrying the interpolation factor the last
// Frame left, and waits for its subscribers.
func (l *loop) Render(k kernel.Executioner) {
	_ = k.PublishEvent(app.RenderEvent{Alpha: l.loadAlpha()}).Wait()
}

// accumulate folds dt (clamped to MaxFrame) into the fixed-step accumulator and
// returns how many app.UpdateEvent values to publish this frame — capped at
// MaxPending, with excess whole steps dropped to stay near real-time rather than
// spiralling. The leftover fraction is stored as the render interpolation alpha.
func (l *loop) accumulate(dt float64) int {
	step := l.config.Step.Seconds()
	if step <= 0 {
		return 0
	}
	if maxFrame := l.config.MaxFrame.Seconds(); maxFrame > 0 && dt > maxFrame {
		dt = maxFrame
	}
	l.accum += dt
	maxSteps := l.config.MaxPending
	if maxSteps < 1 {
		maxSteps = 1
	}
	steps := 0
	for l.accum >= step {
		l.accum -= step
		if steps < maxSteps {
			steps++
		}
	}
	l.storeAlpha(l.accum / step)
	return steps
}

// storeAlpha and loadAlpha carry the render interpolation factor across the
// main->render thread boundary as an atomically-stored float64.
func (l *loop) storeAlpha(a float64) {
	l.alpha.Store(math.Float64bits(a))
}

func (l *loop) loadAlpha() float64 {
	return math.Float64frombits(l.alpha.Load())
}
