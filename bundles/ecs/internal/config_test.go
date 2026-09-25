package internal

import (
	"testing"
)

// ecs declares Config and no DefaultConfig, so a zero field is how a caller asks
// for the default.
func TestAZeroConfigFieldTakesItsDefault(t *testing.T) {
	for name, value := range map[string]any{"no configuration": nil, "a zero Config": Config{}} {
		if got, err := resolveConfig(value); err != nil || got.PrewarmEntities != defaultPrewarmEntities {
			t.Errorf("%s resolved to %+v (%v), want PrewarmEntities %d", name, got, err, defaultPrewarmEntities)
		}
	}
	if got, err := resolveConfig(Config{PrewarmEntities: 50_000}); err != nil || got.PrewarmEntities != 50_000 {
		t.Errorf("a named count resolved to %+v (%v), want 50000", got, err)
	}
}

func TestAConfigOfTheWrongTypeIsRefused(t *testing.T) {
	if _, err := resolveConfig("ecs"); err == nil {
		t.Errorf("resolveConfig accepted a string")
	}
}
