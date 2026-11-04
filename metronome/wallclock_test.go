package metronome_test

import (
	"slices"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/metronome"
)

// beatSignal subscribes a handler to UpdateEvent that sends each beat on a
// channel it answers, dropping a beat the test has not taken yet.
func beatSignal(c *kernel.Configurator) <-chan metronome.Beat {
	beats := make(chan metronome.Beat, 1)
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event metronome.UpdateEvent) (metronome.UpdateEvent, kernel.JobHandle) {
		select {
		case beats <- event.Beat:
		default:
		}
		return event, in
	})
	return beats
}

// awaitBeats waits for r to hold count beats, failing the test after within.
func awaitBeats(t *testing.T, r *recorder, count int, within time.Duration) {
	t.Helper()
	deadline := time.After(within)
	for len(r.all()) < count {
		select {
		case <-deadline:
			t.Fatalf("%d beats after %v, want %d", len(r.all()), within, count)
		case <-time.After(time.Millisecond):
		}
	}
}

func TestWallClockIsTheDefault(t *testing.T) {
	r := &recorder{}
	plugin := metronome.NewPlugin()
	time.Sleep(100 * time.Millisecond) // an epoch at NewPlugin would make every beat below due at once
	started := time.Now()
	spawnPlugin(t, plugin, func(c *kernel.Configurator) {
		metronome.AddTrack[metronome.UpdateEvent](c, metronome.FixedStep(100))
		record(c, r, updateBeat)
	})

	awaitBeats(t, r, 10, 5*time.Second)
	if elapsed := time.Since(started); elapsed < 100*time.Millisecond {
		t.Fatalf("ten beats of 10ms published %v after Spawn, want at least 100ms", elapsed)
	}
	if got, want := r.all()[:10], fixedBeats(10*time.Millisecond, 1, 10); !slices.Equal(got, want) {
		t.Fatalf("the jobs saw %v, want %v", got, want)
	}
}

func TestWallClockEpochIsAtStart(t *testing.T) {
	r := &recorder{}
	clock := metronome.NewWallClock()
	time.Sleep(100 * time.Millisecond) // an epoch at NewWallClock would make every beat below due at once
	started := time.Now()
	spawnPlugin(t, metronome.NewPlugin(metronome.WithClock(clock)), func(c *kernel.Configurator) {
		metronome.AddTrack[metronome.UpdateEvent](c, metronome.FixedStep(100))
		record(c, r, updateBeat)
	})

	awaitBeats(t, r, 10, 5*time.Second)
	if elapsed := time.Since(started); elapsed < 100*time.Millisecond {
		t.Fatalf("ten beats of 10ms published %v after Spawn, want at least 100ms", elapsed)
	}
	if now := clock.Now(); now < 100*time.Millisecond || now > time.Since(started) {
		t.Fatalf("the clock reads %v, %v after Spawn, want the time since Start", now, time.Since(started))
	}
}

func TestWallClockFiresAPastDeadlineAtOnce(t *testing.T) {
	r := &recorder{}
	stalled := make(chan time.Time, 1)
	spawnPlugin(t, metronome.NewPlugin(), func(c *kernel.Configurator) {
		metronome.AddTrack[metronome.UpdateEvent](c, metronome.FixedStep(10))
		c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event metronome.UpdateEvent) (metronome.UpdateEvent, kernel.JobHandle) {
			if event.Tick == 1 {
				time.Sleep(time.Second) // about ten steps fall due meanwhile
				stalled <- time.Now()
			}
			return event, in
		})
		record(c, r, updateBeat)
	})

	// The debt cap keeps 4 of the steps owed, published back-to-back.
	awaitBeats(t, r, 5, 5*time.Second)
	if elapsed := time.Since(<-stalled); elapsed > 300*time.Millisecond {
		t.Fatalf("ticks 2 to 5, all past due, took %v after the stall, want them at once", elapsed)
	}
	beats := r.all()[:5]
	var sum time.Duration
	for i, beat := range beats {
		sum += beat.Delta + beat.Lost
		if beat.Tick != uint64(i+1) || beat.TimeFromStart != sum {
			t.Fatalf("the jobs saw %v, want ticks 1 to 5 with TimeFromStart the sum of Delta + Lost", beats)
		}
	}
	if beats[1].Lost == 0 {
		t.Fatalf("tick 2 after a stall of about ten steps lost nothing: %v", beats)
	}
}

func TestWallClockBeatAllocatesNothing(t *testing.T) {
	var beats <-chan metronome.Beat
	spawnPlugin(t, metronome.NewPlugin(), func(c *kernel.Configurator) {
		metronome.AddTrack[metronome.UpdateEvent](c, metronome.FixedStep(1000))
		beats = beatSignal(c)
	})
	beat := func() { <-beats }
	beat()
	if got := testing.AllocsPerRun(100, beat); got != 0 {
		t.Fatalf("a beat on the wall clock allocated %v times", got)
	}
}
