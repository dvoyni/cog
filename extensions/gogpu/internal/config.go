package internal

import (
	"github.com/gogpu/gogpu"
	"github.com/gogpu/gputypes"
)

// withDefaults fills every zero field of config with the value the driver runs
// with when it is given none: a 1280x720 window titled "cog".
func withDefaults(config Config) Config {
	if config.Title == "" {
		config.Title = "cog"
	}
	if config.Width == 0 {
		config.Width = 1280
	}
	if config.Height == 0 {
		config.Height = 720
	}
	return config
}

// requiredFeatures are the optional GPU features the driver cannot render
// without, requested by name at device creation.
//
// IndirectFirstInstance is what lets a draw name a non-zero firstInstance.
// Scene gives every batch its own slice of the pass's instance buffer and
// offsets into it that way, so without the feature every scene draw is
// rejected, the encoder never finishes, and nothing reaches the screen. It is
// requested rather than hoped for because the alternative failure is a black
// window: a device that lacks it fails here, by name.
const requiredFeatures = gputypes.Features(gputypes.FeatureIndirectFirstInstance)

// gogpuConfig maps a cgogpu.Config onto the gogpu library's Config. Continuous
// render is forced on: this Extension is a game-loop driver, not an idle UI
// app.
func gogpuConfig(c Config) gogpu.Config {
	g := gogpu.DefaultConfig().
		WithTitle(c.Title).
		WithSize(c.Width, c.Height).
		WithContinuousRender(true).
		WithResizable(!c.NoResize).
		WithVSync(!c.NoVSync).
		WithRequiredFeatures(requiredFeatures)
	if c.Fullscreen {
		g = g.WithFullscreen()
	}
	if c.AppName != "" {
		g = g.WithAppName(c.AppName)
	}
	return g
}

// Config configures the gogpu driver's window. The fixed step it drives is
// app's Config, not this one. It is supplied under Name, and its zero value is
// the default: a zero field takes the default its comment names, so a caller
// sets only what it changes, directly or with the With* builders (each returns
// a modified copy):
//
//	cfg := gogpu.Config{}.WithTitle("Feuds").WithSize(1600, 900)
//
// Fields are exported so a Config can be serialized. The switches that default
// to on are spelled as their negation (NoResize, NoVSync), so that off is the
// zero value too.
type Config struct {
	// Title is the window title. Empty means "cog".
	Title string
	// Width and Height are the initial logical window size (DIP). Zero means
	// 1280x720; each is defaulted on its own.
	Width, Height int
	// NoResize fixes the window's size. Default: false, a resizable window.
	NoResize bool
	// NoVSync disables vertical sync. Default: false, vsync on.
	NoVSync bool
	// Fullscreen starts the window fullscreen. Default: false.
	Fullscreen bool
	// AppName is the application/menu name (macOS). Default: empty (the gogpu
	// library's default).
	AppName string
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
	c.NoResize = !resizable
	return c
}

// WithVSync sets whether vertical sync is enabled.
func (c Config) WithVSync(vsync bool) Config {
	c.NoVSync = !vsync
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
