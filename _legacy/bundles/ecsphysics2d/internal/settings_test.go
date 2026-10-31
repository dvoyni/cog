package internal

import (
	"errors"
	"testing"
)

// The settings are fixed at registration and every zero field takes its
// documented default. The numbers are the spec's, and each is quoted with what
// it is in cp: iterations and the bias are cp's own, the Slop is the one real
// conversion out of cp's pixel-sized 0.1, persistence is cp's 3 ticks in
// seconds, and the two cell sizes have no counterpart in cp at all.
func TestAnUnconfiguredPluginTakesTheDocumentedDefaults(t *testing.T) {
	h := newHarness(t)

	want := settings{
		iterations:     10,
		slop:           0.005,
		bias:           6.32,
		persistence:    0.05,
		staticCellSize: 2,
		bodyCellSize:   2,
	}
	if got := h.plugin.settings; got != want {
		t.Fatalf("the resolved settings are %+v, want %+v", got, want)
	}
}

// A named field replaces its default and every other field keeps one, which is
// what "a zero field takes its default" means.
func TestANamedSettingReplacesOnlyItsOwnDefault(t *testing.T) {
	h := newHarnessWith(t, Config{Slop: 0.02, Iterations: 4, Seed: 7}, 64)

	got := h.plugin.settings
	if got.slop != 0.02 || got.iterations != 4 || got.seed != 7 {
		t.Errorf("the named settings resolved to %+v", got)
	}
	if got.bias != defaultBias || got.persistence != defaultPersistence ||
		got.staticCellSize != defaultStaticCellSize || got.bodyCellSize != defaultBodyCellSize {
		t.Errorf("an unnamed setting lost its default: %+v", got)
	}
}

// A seed of zero is an ordinary seed and not a request for an arbitrary one:
// nothing here reaches for a clock, because a simulation that differs run to run
// for a reason nobody asked for is worse than one that does not.
func TestASeedOfZeroIsTakenAsGiven(t *testing.T) {
	if got, err := resolveConfig(Config{Seed: 0}); err != nil || got.seed != 0 {
		t.Fatalf("resolveConfig with a zero seed = %+v, %v", got, err)
	}
}

// A configuration value of another type is refused rather than ignored: a
// caller that wrote one meant to configure this plugin.
func TestAConfigOfTheWrongTypeIsRefused(t *testing.T) {
	_, err := resolveConfig("not a Config")
	var refusal ErrInvalidConfig
	if !errors.As(err, &refusal) {
		t.Fatalf("resolveConfig with a string returned %v, want an ErrInvalidConfig", err)
	}
}
