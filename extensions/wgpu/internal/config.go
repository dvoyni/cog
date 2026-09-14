package internal

import (
	"time"

	cwgpu "github.com/dvoyni/cog/extensions/wgpu"
	"github.com/gogpu/gogpu"
	"github.com/gogpu/gputypes"
)

// withDefaults fills every zero field of config with the value the driver runs
// with when it is given none: a 1/60s step, a 250ms frame clamp, four pending
// steps, and a 1280x720 window titled "cog".
func withDefaults(config cwgpu.Config) cwgpu.Config {
	if config.Step == 0 {
		config.Step = time.Second / 60
	}
	if config.MaxFrame == 0 {
		config.MaxFrame = 250 * time.Millisecond
	}
	if config.MaxPending == 0 {
		config.MaxPending = 4
	}
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

// gogpuConfig maps a Config onto a gogpu.Config. Continuous render
// is forced on: wgpu is a game-loop driver, not an idle UI app.
func gogpuConfig(c cwgpu.Config) gogpu.Config {
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
