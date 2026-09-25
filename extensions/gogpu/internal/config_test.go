package internal

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/bundles/input/inputplugin"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app/appplugin"
	"github.com/dvoyni/cog/slots/gfx/gfxplugin"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// The zero Config takes the documented defaults, including the switches that
// default to on.
func TestDefaultConfig(t *testing.T) {
	d := withDefaults(Config{})
	if d.Title != "cog" {
		t.Errorf("Title = %q, want %q", d.Title, "cog")
	}
	if d.Width != 1280 || d.Height != 720 {
		t.Errorf("size = %dx%d, want 1280x720", d.Width, d.Height)
	}
	if d.NoResize || d.NoVSync {
		t.Errorf("NoResize/NoVSync = %v/%v, want false/false", d.NoResize, d.NoVSync)
	}
}

// The With* setters override only their field and return a modified copy without
// mutating the receiver.
func TestConfigSettersChain(t *testing.T) {
	base := withDefaults(Config{})
	got := base.
		WithTitle("Feuds").
		WithSize(800, 600).
		WithVSync(false).
		WithFullscreen(true)

	if got.Title != "Feuds" {
		t.Errorf("Title = %q, want %q", got.Title, "Feuds")
	}
	if got.Width != 800 || got.Height != 600 {
		t.Errorf("size = %dx%d, want 800x600", got.Width, got.Height)
	}
	if !got.NoVSync {
		t.Error("NoVSync = false, want true")
	}
	if !got.Fullscreen {
		t.Error("Fullscreen = false, want true")
	}
	// Untouched fields keep their defaults.
	if got.AppName != base.AppName || got.NoResize {
		t.Errorf("untouched fields changed: AppName=%q NoResize=%v", got.AppName, got.NoResize)
	}
	// The receiver is unchanged (value semantics).
	if base.Title != "cog" || base.NoVSync {
		t.Errorf("base mutated: Title=%q NoVSync=%v", base.Title, base.NoVSync)
	}
}

// Init reports a config value that is not a cgogpu.Config through the kernel.
func TestPluginInitRejectsWrongConfigType(t *testing.T) {
	var err error
	kernel.New(map[kernel.PluginName]any{Name: 123}).
		Handler(func(got error) error {
			err = got
			return got
		}).
		WithPlugins(storageplugin.New(), permanentAdapter{}, appplugin.New(), gfxplugin.New(), inputplugin.New(), New()).
		Run()
	var invalid ErrInvalidConfig
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}
