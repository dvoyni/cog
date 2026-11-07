package metronome_test

import (
	"slices"
	"testing"
	"time"

	"github.com/dvoyni/cog/metronome"
)

func TestAStopwatchNeverStartedReadsZero(t *testing.T) {
	for _, clock := range []metronome.Clock{metronome.NewWallClock(), metronome.NewManualClock()} {
		if elapsed := clock.NewStopwatch("idle").Elapsed(); elapsed != 0 {
			t.Errorf("a stopwatch of %T never started reads %v, want zero", clock, elapsed)
		}
	}
}

func TestAWallClocksStopwatchReadsTheTimeSinceItsStart(t *testing.T) {
	stopwatch := metronome.NewWallClock().NewStopwatch("wall")
	time.Sleep(10 * time.Millisecond)
	started := time.Now()
	stopwatch.Start()
	time.Sleep(10 * time.Millisecond)

	if elapsed := stopwatch.Elapsed(); elapsed < 10*time.Millisecond || elapsed > time.Since(started) {
		t.Errorf("the stopwatch reads %v, %v after its Start, want the time since", elapsed, time.Since(started))
	}
}

func TestAManualClocksStopwatchReadsTheManualTimeSinceItsStart(t *testing.T) {
	clock := metronome.NewManualClock()
	clock.Jump(4 * time.Millisecond)
	stopwatch := clock.NewStopwatch("manual")
	stopwatch.Start()
	clock.Jump(3 * time.Millisecond)

	if elapsed := stopwatch.Elapsed(); elapsed != 3*time.Millisecond {
		t.Errorf("the stopwatch reads %v, want 3ms", elapsed)
	}
	stopwatch.Start()
	if elapsed := stopwatch.Elapsed(); elapsed != 0 {
		t.Errorf("the stopwatch restarted reads %v, want zero", elapsed)
	}
}

func TestLengthenNextLengthensTheNextMeasurementsOfStopwatchesOfThatName(t *testing.T) {
	clock := metronome.NewManualClock()
	acquire := clock.NewStopwatch("acquire")
	other := clock.NewStopwatch("other")
	clock.LengthenNext("acquire", 3*time.Millisecond)
	clock.LengthenNext("acquire", 5*time.Millisecond)

	var measured []time.Duration
	for range 3 {
		acquire.Start()
		other.Start()
		clock.Jump(time.Millisecond)
		measured = append(measured, acquire.Elapsed())
		if elapsed := other.Elapsed(); elapsed != time.Millisecond {
			t.Errorf("a stopwatch of another name reads %v, want 1ms", elapsed)
		}
	}

	if want := []time.Duration{4 * time.Millisecond, 6 * time.Millisecond, time.Millisecond}; !slices.Equal(measured, want) {
		t.Errorf("the measurements read %v, want %v", measured, want)
	}
}
