package wgpu

import (
	"context"
	"errors"
	"testing"
	"time"

	cgfx "github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/input"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/storage"
)

// DefaultConfig populates the documented defaults, including the positive
// booleans.
func TestDefaultConfig(t *testing.T) {
	d := DefaultConfig()
	if d.Step != time.Second/60 {
		t.Errorf("Step = %v, want 1/60s", d.Step)
	}
	if d.MaxPending != 4 {
		t.Errorf("MaxPending = %d, want 4", d.MaxPending)
	}
	if d.Width != 1280 || d.Height != 720 {
		t.Errorf("size = %dx%d, want 1280x720", d.Width, d.Height)
	}
	if !d.Resizable || !d.VSync {
		t.Errorf("Resizable/VSync = %v/%v, want true/true", d.Resizable, d.VSync)
	}
}

// The With* setters override only their field and return a modified copy without
// mutating the receiver.
func TestConfigSettersChain(t *testing.T) {
	base := DefaultConfig()
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
	if got.VSync {
		t.Error("VSync = true, want false")
	}
	if got.Step != 10*time.Millisecond {
		t.Errorf("Step = %v, want 10ms", got.Step)
	}
	// Untouched fields keep their defaults.
	if got.MaxPending != base.MaxPending || !got.Resizable {
		t.Errorf("untouched fields changed: MaxPending=%d Resizable=%v", got.MaxPending, got.Resizable)
	}
	// The receiver is unchanged (value semantics).
	if base.Title != "cog" || !base.VSync {
		t.Errorf("base mutated: Title=%q VSync=%v", base.Title, base.VSync)
	}
}

// Init reports a config value that is not a wgpu.Config through the kernel.
func TestPluginInitRejectsWrongConfigType(t *testing.T) {
	var err error
	kernel.New(map[kernel.PluginName]any{Name: 123}).
		Handler(func(got error) bool {
			err = got
			return true
		}).
		WithPlugins(storage.New(), cgfx.New(), input.New(), New()).
		Run(context.Background())
	var invalid ErrInvalidConfig
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}
