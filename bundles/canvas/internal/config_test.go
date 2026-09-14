package internal

import (
	"testing"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/canvas/internal/types"
)

// canvas.Config has no DefaultConfig: its zero value is the default, so a zero
// field is how a caller asks for the default, and naming one field must not make
// the others invalid.
func TestAZeroConfigFieldTakesItsDefault(t *testing.T) {
	got, err := resolveConfig(canvas.Config{AtlasSize: 16})
	if err != nil {
		t.Fatalf("resolveConfig: %v", err)
	}
	defaults := types.WithDefaults(canvas.Config{})
	want := canvas.Config{AtlasSize: 16, LayersPerArray: defaults.LayersPerArray, MaxAtlasBytes: defaults.MaxAtlasBytes}
	if got != want {
		t.Fatalf("resolved %+v, want %+v", got, want)
	}
	if got, err := resolveConfig(nil); err != nil || got != defaults {
		t.Fatalf("no configuration resolved to %+v (%v), want the defaults %+v", got, err, defaults)
	}
}

func TestAnInvalidConfigIsRefused(t *testing.T) {
	for name, value := range map[string]any{
		"wrong type":        "canvas",
		"one layer":         canvas.Config{LayersPerArray: 1},
		"negative size":     canvas.Config{AtlasSize: -1},
		"array over budget": canvas.Config{AtlasSize: 64, MaxAtlasBytes: 64 * 64 * 4},
	} {
		if _, err := resolveConfig(value); err == nil {
			t.Errorf("%s: resolveConfig accepted %#v", name, value)
		}
	}
}
