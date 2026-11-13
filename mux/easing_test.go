package mux

import (
	"math"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
)

// pennerReference is each of mux's easings at a quarter, a half and three
// quarters of a span from 0 to 1, as easings.net's own formula for the curve
// gives it, computed apart from mux.
var pennerReference = [...][3]float32{
	EasingLinear:       {0.25, 0.5, 0.75},
	EasingQuadIn:       {0.0625, 0.25, 0.5625},
	EasingQuadOut:      {0.4375, 0.75, 0.9375},
	EasingQuadInOut:    {0.125, 0.5, 0.875},
	EasingCubicIn:      {0.015625, 0.125, 0.421875},
	EasingCubicOut:     {0.578125, 0.875, 0.984375},
	EasingCubicInOut:   {0.0625, 0.5, 0.9375},
	EasingQuartIn:      {0.00390625, 0.0625, 0.3164062},
	EasingQuartOut:     {0.6835938, 0.9375, 0.9960938},
	EasingQuartInOut:   {0.03125, 0.5, 0.96875},
	EasingQuintIn:      {0.0009765625, 0.03125, 0.2373047},
	EasingQuintOut:     {0.7626953, 0.96875, 0.9990234},
	EasingQuintInOut:   {0.015625, 0.5, 0.984375},
	EasingSineIn:       {0.07612047, 0.2928932, 0.6173166},
	EasingSineOut:      {0.3826834, 0.7071068, 0.9238795},
	EasingSineInOut:    {0.1464466, 0.5, 0.8535534},
	EasingExpoIn:       {0.005524272, 0.03125, 0.1767767},
	EasingExpoOut:      {0.8232233, 0.96875, 0.9944757},
	EasingExpoInOut:    {0.015625, 0.5, 0.984375},
	EasingCircIn:       {0.03175416, 0.1339746, 0.3385622},
	EasingCircOut:      {0.6614378, 0.8660254, 0.9682458},
	EasingCircInOut:    {0.0669873, 0.5, 0.9330127},
	EasingBackIn:       {-0.06413656, -0.0876975, 0.1825903},
	EasingBackOut:      {0.8174097, 1.087697, 1.064137},
	EasingBackInOut:    {-0.09968184, 0.5, 1.099682},
	EasingElasticIn:    {-0.005524272, -0.015625, 0.08838835},
	EasingElasticOut:   {0.9116117, 1.015625, 1.005524},
	EasingElasticInOut: {0.01196944, 0.5, 0.9880306},
	EasingBounceIn:     {0.02734375, 0.234375, 0.5273438},
	EasingBounceOut:    {0.4726562, 0.765625, 0.9726562},
	EasingBounceInOut:  {0.1171875, 0.5, 0.8828125},
}

func TestMuxsEasingsFollowPennersCurves(t *testing.T) {
	probe := spawnTimelineProbe(t, nil)
	timeline := ElevateTimeline(probe.timeline)
	// Each tween leaves at the Step that reads the next one's first quarter,
	// or its half for the last.
	for easing, quarters := range pennerReference {
		timeline.Tween(probe.cameraY, timeline.Now(), 0, 1, time.Second, Easing(easing))
		for quarter, want := range quarters {
			if got := probe.stepAndRead(t, 250*time.Millisecond); math.Abs(float64(got-want)) > 1e-6 {
				t.Errorf("easing %d is at %v after %d quarters of its span, want %v", easing, got, quarter+1, want)
			}
		}
	}
	timeline.Tween(probe.cameraY, timeline.Now(), 2, 6, time.Second, EasingQuadIn)
	if got := probe.stepAndRead(t, 500*time.Millisecond); got != 3 {
		t.Errorf("QuadIn from 2 to 6 is at %v halfway, want 3", got)
	}
}

func TestAGameRegistersEasingsUnderItsOwnNumbersInAnyOrder(t *testing.T) {
	const (
		jumpAtStart Easing = 258
		jumpHalfway Easing = 256
	)
	probe := spawnTimelineProbe(t, func(configurator *kernel.Configurator) {
		RegisterEasing(configurator, jumpAtStart, func(a, b, t float32) float32 { return b })
		RegisterEasing(configurator, jumpHalfway, func(a, b, t float32) float32 {
			if t < 0.5 {
				return a
			}
			return b
		})
	})
	timeline := ElevateTimeline(probe.timeline)
	timeline.Tween(probe.cameraY, timeline.Now(), 2, 6, time.Second, jumpHalfway)
	// The model's own value shows once the tween ends, before the next one.
	probe.expectSteps(t, []timedValue{{250 * time.Millisecond, 2}, {500 * time.Millisecond, 6}, {500 * time.Millisecond, 0}})
	timeline.Tween(probe.cameraY, timeline.Now(), 2, 6, time.Second, jumpAtStart)
	probe.expectSteps(t, []timedValue{{time.Millisecond, 6}})
	expectPanic(t, `path "Camera.Y" is tweened by easing 257, which is not registered`, func() {
		timeline.Tween(probe.cameraY, timeline.Now(), 2, 6, time.Second, 257)
	})
}

func TestRegisterEasingPanicsNamingWhatIsWrong(t *testing.T) {
	jump := func(a, b, t float32) float32 { return b }
	expectPanic(t, "easing 256 is registered before the mux plugin", func() {
		spawnPlugins(t, testPlugin{init: func(configurator *kernel.Configurator) { RegisterEasing(configurator, 256, jump) }})
	})
	expectPanic(t, "easing 0 is registered, but numbers below 256 are mux's", func() {
		spawn(t, func(configurator *kernel.Configurator) { RegisterEasing(configurator, EasingLinear, jump) })
	})
	expectPanic(t, "easing 255 is registered, but numbers below 256 are mux's", func() {
		spawn(t, func(configurator *kernel.Configurator) { RegisterEasing(configurator, 255, jump) })
	})
	expectPanic(t, "easing 256 is registered twice", func() {
		spawn(t, func(configurator *kernel.Configurator) {
			RegisterEasing(configurator, 256, jump)
			RegisterEasing(configurator, 256, jump)
		})
	})
	var keptConfigurator *kernel.Configurator
	spawn(t, func(configurator *kernel.Configurator) { keptConfigurator = configurator })
	expectPanic(t, "easing 256 is registered after Init", func() { RegisterEasing(keptConfigurator, 256, jump) })
}
