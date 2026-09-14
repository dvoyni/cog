package internal

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dvoyni/cog/bundles/input/inputplugin"
	cwgpu "github.com/dvoyni/cog/extensions/wgpu"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx/gfxplugin"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// The zero Config takes the documented defaults, including the switches that
// default to on.
func TestDefaultConfig(t *testing.T) {
	d := withDefaults(cwgpu.Config{})
	if d.Step != time.Second/60 {
		t.Errorf("Step = %v, want 1/60s", d.Step)
	}
	if d.MaxPending != 4 {
		t.Errorf("MaxPending = %d, want 4", d.MaxPending)
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
	base := withDefaults(cwgpu.Config{})
	got := base.
		WithTitle("Feuds").
		WithSize(800, 600).
		WithVSync(false).
		WithStep(10 * time.Millisecond)

	if got.Title != "Feuds" {
		t.Errorf("Title = %q, want %q", got.Title, "Feuds")
	}
	if got.Width != 800 || got.Height != 600 {
		t.Errorf("size = %dx%d, want 800x600", got.Width, got.Height)
	}
	if !got.NoVSync {
		t.Error("NoVSync = false, want true")
	}
	if got.Step != 10*time.Millisecond {
		t.Errorf("Step = %v, want 10ms", got.Step)
	}
	// Untouched fields keep their defaults.
	if got.MaxPending != base.MaxPending || got.NoResize {
		t.Errorf("untouched fields changed: MaxPending=%d NoResize=%v", got.MaxPending, got.NoResize)
	}
	// The receiver is unchanged (value semantics).
	if base.Title != "cog" || base.NoVSync {
		t.Errorf("base mutated: Title=%q NoVSync=%v", base.Title, base.NoVSync)
	}
}

// Init reports a config value that is not a wgpu.Config through the kernel.
func TestPluginInitRejectsWrongConfigType(t *testing.T) {
	var err error
	kernel.New(map[kernel.PluginName]any{cwgpu.Name: 123}).
		Handler(func(got error) bool {
			err = got
			return true
		}).
		WithPlugins(storageplugin.New(), permanentAdapter{}, gfxplugin.New(), inputplugin.New(), New()).
		Run(context.Background())
	var invalid cwgpu.ErrInvalidConfig
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}
