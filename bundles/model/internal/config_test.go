package internal

import (
	"testing"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/model/internal/types"
)

// model.Config has no DefaultConfig: its zero value is the default, so a zero
// field is how a caller asks for the default.
func TestAZeroConfigFieldTakesItsDefault(t *testing.T) {
	defaults := types.WithDefaults(model.Config{})
	for name, value := range map[string]any{"no configuration": nil, "a zero Config": model.Config{}} {
		if got, err := resolveConfig(value); err != nil || got != defaults {
			t.Errorf("%s resolved to %+v (%v), want the defaults %+v", name, got, err, defaults)
		}
	}
	if got, err := resolveConfig(model.Config{PoseSampleRate: 30}); err != nil || got.PoseSampleRate != 30 {
		t.Errorf("a named rate resolved to %+v (%v), want 30", got, err)
	}
}

func TestAnInvalidConfigIsRefused(t *testing.T) {
	for name, value := range map[string]any{
		"wrong type":    "model",
		"negative rate": model.Config{PoseSampleRate: -1},
	} {
		if _, err := resolveConfig(value); err == nil {
			t.Errorf("%s: resolveConfig accepted %#v", name, value)
		}
	}
}
