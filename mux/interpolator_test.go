package mux

import (
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
)

// makeTweenCheck makes the path text names, to a property of T, in Init,
// and answers a check that tweens it from from to to over a second, eased by
// easing, and expects it current at want once at has passed.
func makeTweenCheck[T comparable](configurator *kernel.Configurator, text string, from, to T, easing Easing, at time.Duration, want T) func(t *testing.T, probe *timelineProbe) {
	path := MakePath[game, T](configurator, text)
	return func(t *testing.T, probe *timelineProbe) {
		t.Helper()
		probe.expectTween(t, path, from, to, easing, at, want)
	}
}

func TestEveryNumberKindTweensByTheInterpolatorBuiltIn(t *testing.T) {
	const halfway = 500 * time.Millisecond
	var checks []func(t *testing.T, probe *timelineProbe)
	probe := spawnTimelineProbe(t, func(configurator *kernel.Configurator) {
		checks = []func(t *testing.T, probe *timelineProbe){
			makeTweenCheck[int](configurator, "Gauge.Int", -5, 5, EasingLinear, halfway, 0),
			makeTweenCheck[int16](configurator, "Gauge.Int16", -3, 4, EasingLinear, halfway, 1),
			makeTweenCheck[int32](configurator, "Gauge.Int32", 10, 20, EasingQuadIn, halfway, 13),
			makeTweenCheck[int64](configurator, "Gauge.Int64", 0, 1<<40, EasingLinear, halfway, 1<<39),
			makeTweenCheck[uint](configurator, "Gauge.Uint", 7, 3, EasingLinear, halfway, 5),
			makeTweenCheck[uint16](configurator, "Gauge.Uint16", 100, 200, EasingLinear, halfway, 150),
			makeTweenCheck[uint32](configurator, "Gauge.Uint32", 0, 4000000000, EasingLinear, halfway, 2000000000),
			makeTweenCheck[uint64](configurator, "Gauge.Uint64", 1<<50, 1<<50+10, EasingLinear, halfway, 1<<50+5),
			makeTweenCheck[uintptr](configurator, "Gauge.Uintptr", 0, 10, EasingLinear, halfway, 5),
			makeTweenCheck[float32](configurator, "Gauge.Float32", 1, 2, EasingLinear, halfway, 1.5),
			// A float64 keeps a precision a float32 does not have.
			makeTweenCheck[float64](configurator, "Gauge.Float64", 1e10, 1e10+2, EasingLinear, halfway, 1e10+1),
			makeTweenCheck[distance](configurator, "Gauge.Distance", 0, 3, EasingLinear, halfway, 1.5),
			makeTweenCheck[level](configurator, "Gauge.Level", 1, 4, EasingLinear, halfway, 3),
			// Back overshoots its ends, and an integer stops at its kind's.
			makeTweenCheck[uint8](configurator, "Gauge.Uint8", 10, 200, EasingBackIn, 250*time.Millisecond, 0),
			makeTweenCheck[int8](configurator, "Gauge.Int8", -10, 120, EasingBackOut, halfway, 127),
		}
	})
	for _, check := range checks {
		check(t, probe)
	}
}

func TestAGamesInterpolatorReplacesTheBuiltInOneForItsTypeAlone(t *testing.T) {
	var headingPath Path[game, heading]
	var float32Path Path[game, float32]
	probe := spawnTimelineProbe(t, func(configurator *kernel.Configurator) {
		headingPath = MakePath[game, heading](configurator, "Gauge.Heading")
		float32Path = MakePath[game, float32](configurator, "Gauge.Float32")
		// Registered after the path to its type is made: the order does not
		// matter.
		RegisterInterpolator(configurator, turnShortestWay)
	})
	probe.expectTween(t, headingPath, 350, 10, EasingLinear, 500*time.Millisecond, 360)
	probe.expectTween(t, float32Path, 350, 10, EasingLinear, 500*time.Millisecond, 180)
}

func TestTweeningATypeWithNoInterpolatorPanicsNamingIt(t *testing.T) {
	var positionPath Path[game, vec2]
	probe := spawnTimelineProbe(t, func(configurator *kernel.Configurator) {
		positionPath = MakePath[game, vec2](configurator, "Hero.Position")
	})
	timeline := ElevateTimeline(probe.timeline)
	expectPanic(t, `path "Hero.Position" is tweened, but no interpolator tweens its mux.vec2`, func() {
		timeline.Tween(positionPath, 0, vec2{}, vec2{X: 1}, time.Second, EasingLinear)
	})
}

func TestRegisterInterpolatorPanicsNamingWhatIsWrong(t *testing.T) {
	expectPanic(t, "interpolator for mux.heading is registered before the mux plugin", func() {
		spawnPlugins(t, testPlugin{init: func(configurator *kernel.Configurator) { RegisterInterpolator(configurator, turnShortestWay) }})
	})
	expectPanic(t, "interpolator for mux.heading is registered twice", func() {
		spawn(t, func(configurator *kernel.Configurator) {
			RegisterInterpolator(configurator, turnShortestWay)
			RegisterInterpolator(configurator, turnShortestWay)
		})
	})
	var keptConfigurator *kernel.Configurator
	spawn(t, func(configurator *kernel.Configurator) { keptConfigurator = configurator })
	expectPanic(t, "interpolator for mux.heading is registered after Init", func() { RegisterInterpolator(keptConfigurator, turnShortestWay) })
}
