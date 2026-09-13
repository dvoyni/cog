package sceneimpl

import (
	"testing"

	"github.com/dvoyni/cog/bundles/scene/internal"
)

// sceneimpl exports Config and no DefaultConfig, so a zero field is how a caller
// asks for the default.
func TestAZeroConfigFieldTakesItsDefault(t *testing.T) {
	defaults := internal.DefaultConfig()
	for name, value := range map[string]any{"no configuration": nil, "a zero Config": Config{}} {
		if got, err := resolveConfig(value); err != nil || got != defaults {
			t.Errorf("%s resolved to %+v (%v), want the defaults %+v", name, got, err, defaults)
		}
	}
	if got, err := resolveConfig(Config{PoseSampleRate: 30}); err != nil || got.PoseSampleRate != 30 {
		t.Errorf("a named rate resolved to %+v (%v), want 30", got, err)
	}
}

func TestAnInvalidConfigIsRefused(t *testing.T) {
	for name, value := range map[string]any{
		"wrong type":    "scene",
		"negative rate": Config{PoseSampleRate: -1},
	} {
		if _, err := resolveConfig(value); err == nil {
			t.Errorf("%s: resolveConfig accepted %#v", name, value)
		}
	}
}
