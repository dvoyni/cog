package internal

import (
	"testing"
)

// canvas.Config has no DefaultConfig: its zero value is the default, so a zero
// field is how a caller asks for the default, and naming one field must not make
// the others invalid.
func TestAZeroConfigFieldTakesItsDefault(t *testing.T) {
	got, err := resolveConfig(Config{AtlasSize: 16})
	if err != nil {
		t.Fatalf("resolveConfig: %v", err)
	}
	defaults := WithDefaults(Config{})
	want := Config{AtlasSize: 16, LayersPerArray: defaults.LayersPerArray, MaxAtlasBytes: defaults.MaxAtlasBytes}
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
		"one layer":         Config{LayersPerArray: 1},
		"negative size":     Config{AtlasSize: -1},
		"array over budget": Config{AtlasSize: 64, MaxAtlasBytes: 64 * 64 * 4},
	} {
		if _, err := resolveConfig(value); err == nil {
			t.Errorf("%s: resolveConfig accepted %#v", name, value)
		}
	}
}
