package wgpu

import (
	"time"

	"github.com/gogpu/gogpu"
)

// Config configures the wgpu driver. Build it from DefaultConfig and the With*
// setters (each returns a modified copy), mirroring gogpu's config style:
//
//	cfg := wgpu.DefaultConfig().WithTitle("Feuds").WithSize(1600, 900)
//
// It is delivered through kernel.New's config map, keyed by Name.
// Fields are exported so a Config can be serialized.
type Config struct {
	// Step is the fixed simulation interval (the update rate). Default: 1/60s.
	Step time.Duration
	// MaxFrame clamps the elapsed time absorbed in a single frame, bounding
	// catch-up work after a stall (anti spiral-of-death). Default: 250ms.
	MaxFrame time.Duration
	// MaxPending bounds how many catch-up steps may queue before extras are
	// dropped. Default: 4.
	MaxPending int

	// Title is the window title. Default: "cog".
	Title string
	// Width and Height are the initial logical window size (DIP). Default: 1280x720.
	Width, Height int
	// Resizable allows the window to be resized. Default: true.
	Resizable bool
	// VSync enables vertical sync. Default: true.
	VSync bool
	// Fullscreen starts the window fullscreen. Default: false.
	Fullscreen bool
	// AppName is the application/menu name (macOS). Default: empty (gogpu default).
	AppName string
}

// DefaultConfig returns the default configuration. Chain With* setters to override.
func DefaultConfig() Config {
	return Config{
		Step:       time.Second / 60,
		MaxFrame:   250 * time.Millisecond,
		MaxPending: 4,
		Title:      "cog",
		Width:      1280,
		Height:     720,
		Resizable:  true,
		VSync:      true,
	}
}

// WithStep sets the fixed simulation interval.
func (c Config) WithStep(step time.Duration) Config {
	c.Step = step
	return c
}

// WithMaxFrame sets the per-frame elapsed-time clamp.
func (c Config) WithMaxFrame(d time.Duration) Config {
	c.MaxFrame = d
	return c
}

// WithMaxPending sets the catch-up queue capacity.
func (c Config) WithMaxPending(n int) Config {
	c.MaxPending = n
	return c
}

// WithTitle sets the window title.
func (c Config) WithTitle(title string) Config {
	c.Title = title
	return c
}

// WithSize sets the initial logical window size (DIP).
func (c Config) WithSize(width, height int) Config {
	c.Width, c.Height = width, height
	return c
}

// WithResizable sets whether the window can be resized.
func (c Config) WithResizable(resizable bool) Config {
	c.Resizable = resizable
	return c
}

// WithVSync sets whether vertical sync is enabled.
func (c Config) WithVSync(vsync bool) Config {
	c.VSync = vsync
	return c
}

// WithFullscreen sets whether the window starts fullscreen.
func (c Config) WithFullscreen(fullscreen bool) Config {
	c.Fullscreen = fullscreen
	return c
}

// WithAppName sets the application/menu name (macOS).
func (c Config) WithAppName(name string) Config {
	c.AppName = name
	return c
}

// gogpuConfig maps a Config onto a gogpu.Config. Continuous render
// is forced on: wgpu is a game-loop driver, not an idle UI app.
func (c Config) gogpuConfig() gogpu.Config {
	g := gogpu.DefaultConfig().
		WithTitle(c.Title).
		WithSize(c.Width, c.Height).
		WithContinuousRender(true).
		WithResizable(c.Resizable).
		WithVSync(c.VSync)
	if c.Fullscreen {
		g = g.WithFullscreen()
	}
	if c.AppName != "" {
		g = g.WithAppName(c.AppName)
	}
	return g
}
