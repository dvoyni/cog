package metronome_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/metronome"
)

// testPlugin runs init as its Init.
type testPlugin struct {
	init func(c *kernel.Configurator)
}

func (testPlugin) Name() kernel.PluginName { return "test" }

func (testPlugin) Dependencies() []kernel.PluginName { return nil }

func (p testPlugin) Init(c *kernel.Configurator, config any) error {
	p.init(c)
	return nil
}

func (testPlugin) Start(k *kernel.Kernel) error { return nil }

func (testPlugin) Stop(k *kernel.Kernel) error { return nil }

func (testPlugin) Destroy() error { return nil }

// recorder keeps the beats a track's jobs saw, in the order they ran.
type recorder struct {
	mutex sync.Mutex
	beats []metronome.Beat
}

func (r *recorder) add(beat metronome.Beat) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.beats = append(r.beats, beat)
}

func (r *recorder) all() []metronome.Beat {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return slices.Clone(r.beats)
}

// record subscribes a handler to E that schedules a job recording the beat
// header of each E, read by header, into r, and hands that job on as its out
// dependency.
func record[E any](c *kernel.Configurator, r *recorder, header func(E) metronome.Beat) {
	task := c.RegisterTask(func(access *kernel.Access) kernel.TaskBody[E, kernel.NoResult] {
		return func(k *kernel.Kernel, event E) kernel.NoResult {
			r.add(header(event))
			return kernel.NoResult{}
		}
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event E) (E, kernel.JobHandle) {
		return event, ex.Schedule(task, event, in).Handle()
	})
}

// updateBeat reads an UpdateEvent's beat header.
func updateBeat(event metronome.UpdateEvent) metronome.Beat { return event.Beat }

// spawn spawns a kernel of metronome on clock pacing tracks, and a test
// plugin whose Init runs init, and answers the kernel. The supervisor quits
// when the test ends.
func spawn(t *testing.T, clock metronome.Clock, tracks []metronome.Track, init func(c *kernel.Configurator)) *kernel.Kernel {
	t.Helper()
	return spawnPlugin(t, metronome.NewPlugin(), metronomeConfig(clock, tracks...), init)
}

// metronomeConfig answers a kernel config giving metronome clock and tracks.
func metronomeConfig(clock metronome.Clock, tracks ...metronome.Track) map[kernel.PluginName]any {
	return map[kernel.PluginName]any{metronome.PluginName: &metronome.Config{Clock: clock, Tracks: tracks}}
}

// spawnPlugin spawns a kernel of plugin and a test plugin whose Init runs
// init, with config, and answers the kernel. The supervisor quits when the
// test ends.
func spawnPlugin(t *testing.T, plugin *metronome.Plugin, config map[kernel.PluginName]any, init func(c *kernel.Configurator)) *kernel.Kernel {
	t.Helper()
	s := kernel.NewSupervisor(nil)
	t.Cleanup(s.Quit)
	id, err := s.Spawn(config, 4, plugin, testPlugin{init: init})
	if err != nil {
		t.Fatal(err)
	}
	return s.GetKernel(id)
}

// fixedBeats answers the beats ticks first to last of a fixed-step track of
// step, as metronome stamps them.
func fixedBeats(step time.Duration, first, last uint64) []metronome.Beat {
	var beats []metronome.Beat
	for tick := first; tick <= last; tick++ {
		beats = append(beats, metronome.Beat{Tick: tick, Delta: step, TimeFromStart: time.Duration(tick) * step})
	}
	return beats
}

func TestAdvancePublishesEveryBeatDue(t *testing.T) {
	clock := metronome.NewManualClock()
	r := &recorder{}
	spawn(t, clock, []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(10))}, func(c *kernel.Configurator) {
		record(c, r, updateBeat)
	})

	clock.Advance(350 * time.Millisecond)
	if got, want := r.all(), fixedBeats(100*time.Millisecond, 1, 3); !slices.Equal(got, want) {
		t.Fatalf("after 350ms the jobs saw %v, want %v", got, want)
	}
	clock.Advance(50 * time.Millisecond)
	if got, want := r.all(), fixedBeats(100*time.Millisecond, 1, 4); !slices.Equal(got, want) {
		t.Fatalf("after 400ms the jobs saw %v, want %v", got, want)
	}
}

func TestFixedStepZeroIsSixtyHertz(t *testing.T) {
	clock := metronome.NewManualClock()
	r := &recorder{}
	spawn(t, clock, []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(0))}, func(c *kernel.Configurator) {
		record(c, r, updateBeat)
	})

	clock.Advance(time.Second)
	if got, want := r.all(), fixedBeats(16666666*time.Nanosecond, 1, 60); !slices.Equal(got, want) {
		t.Fatalf("a second of FixedStep(0) saw %v, want %v", got, want)
	}
}

// slowEvent is the event of a second track.
type slowEvent struct {
	metronome.Beat
	Payload int32
}

func slowBeat(event slowEvent) metronome.Beat { return event.Beat }

func TestAdvanceWalksDeadlineByDeadlineAcrossTracks(t *testing.T) {
	clock := metronome.NewManualClock()
	r := &recorder{}
	spawn(t, clock, []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(10)), metronome.NewTrack[slowEvent](metronome.FixedStep(4))}, func(c *kernel.Configurator) {
		record(c, r, updateBeat)
		record(c, r, slowBeat)
	})

	clock.Advance(time.Second)
	beats := r.all()
	var update, slow []metronome.Beat
	for i, beat := range beats {
		if i > 0 && beat.TimeFromStart < beats[i-1].TimeFromStart {
			t.Fatalf("beat %v ran after the later %v", beat, beats[i-1])
		}
		if beat.Delta == 100*time.Millisecond {
			update = append(update, beat)
		} else {
			slow = append(slow, beat)
		}
	}
	if want := fixedBeats(100*time.Millisecond, 1, 10); !slices.Equal(update, want) {
		t.Fatalf("the update track saw %v, want %v", update, want)
	}
	if want := fixedBeats(250*time.Millisecond, 1, 4); !slices.Equal(slow, want) {
		t.Fatalf("the slow track saw %v, want %v", slow, want)
	}
}

func TestHandPublishedEventHasZeroBeat(t *testing.T) {
	r := &recorder{}
	k := spawn(t, metronome.NewManualClock(), []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(10))}, func(c *kernel.Configurator) {
		record(c, r, updateBeat)
	})

	_, out := k.Publish(metronome.UpdateEvent{})
	signal := kernel.NewSignal()
	k.Notify(signal, out)
	<-signal.C()
	if got, want := r.all(), []metronome.Beat{{}}; !slices.Equal(got, want) {
		t.Fatalf("a hand-published event carried %v, want %v", got, want)
	}
}

func TestTwoTracksOfOneEventTypePanic(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a second track of UpdateEvent did not panic")
		}
	}()
	spawn(t, metronome.NewManualClock(), []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(10)), metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(20))}, func(c *kernel.Configurator) {})
}

func TestConfigOfAnotherTypePanics(t *testing.T) {
	for _, config := range []any{metronome.NewManualClock(), metronome.Config{}} {
		t.Run(fmt.Sprintf("%T", config), func(t *testing.T) {
			defer func() {
				if message := fmt.Sprint(recover()); !strings.Contains(message, "want a *metronome.Config") {
					t.Fatalf("a config of another type than *Config panicked with %q", message)
				}
			}()
			spawnPlugin(t, metronome.NewPlugin(), map[kernel.PluginName]any{metronome.PluginName: config}, func(c *kernel.Configurator) {})
		})
	}
}

// testClock is a Clock the test drives: Now reads what setNow set, and a timer
// fires when the test calls fire, or at once on every Reset if autoFire.
type testClock struct {
	autoFire bool
	mutex    sync.Mutex
	now      time.Duration
	timers   []*testTimer
	wait     func(ctx context.Context) error
}

type testTimer struct {
	clock   *testClock
	channel chan struct{}
	// resetAt is the time the timer was last reset to.
	resetAt time.Duration
}

func (c *testClock) Now() time.Duration {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.now
}

func (c *testClock) NewTimer() metronome.Timer {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	timer := &testTimer{clock: c, channel: make(chan struct{}, 1)}
	c.timers = append(c.timers, timer)
	return timer
}

func (c *testClock) SetWaitCallback(wait func(ctx context.Context) error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.wait = wait
}

// Wait waits on metronome's wait callback, as a clock moving time when told
// does.
func (c *testClock) Wait(ctx context.Context) error {
	c.mutex.Lock()
	wait := c.wait
	c.mutex.Unlock()
	return wait(ctx)
}

func (c *testClock) setNow(now time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.now = now
}

// fire fires the timer of the track declared index-th.
func (c *testClock) fire(index int) {
	c.mutex.Lock()
	timer := c.timers[index]
	c.mutex.Unlock()
	timer.fire()
}

// lastReset answers the time the timer of the track declared index-th was
// last reset to.
func (c *testClock) lastReset(index int) time.Duration {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.timers[index].resetAt
}

func (t *testTimer) Reset(at time.Duration) {
	t.clock.mutex.Lock()
	t.resetAt = at
	t.clock.mutex.Unlock()
	if t.clock.autoFire {
		t.fire()
	}
}

func (t *testTimer) C() <-chan struct{} { return t.channel }

func (t *testTimer) Stop() {}

func (t *testTimer) fire() {
	select {
	case t.channel <- struct{}{}:
	default:
	}
}

// gatedRecord subscribes a handler to E that schedules a job, recording the
// beat into r once gate closes, and hands that job on as its out dependency.
func gatedRecord[E any](c *kernel.Configurator, r *recorder, header func(E) metronome.Beat, gate <-chan struct{}) {
	task := c.RegisterTask(func(access *kernel.Access) kernel.TaskBody[E, kernel.NoResult] {
		return func(k *kernel.Kernel, event E) kernel.NoResult {
			<-gate
			r.add(header(event))
			return kernel.NoResult{}
		}
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event E) (E, kernel.JobHandle) {
		return event, ex.Schedule(task, event, in).Handle()
	})
}

func TestWaitReturnsOnceEveryTrackHasSettled(t *testing.T) {
	clock := &testClock{}
	r := &recorder{}
	gate := make(chan struct{})
	spawn(t, clock, []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(10))}, func(c *kernel.Configurator) {
		gatedRecord(c, r, updateBeat, gate)
	})
	if err := clock.Wait(t.Context()); err != nil {
		t.Fatalf("Wait before any deadline answered %v", err)
	}

	clock.setNow(100 * time.Millisecond)
	clock.fire(0)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := clock.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait with a beat held in flight answered %v, want the context's deadline", err)
	}

	close(gate)
	if err := clock.Wait(t.Context()); err != nil {
		t.Fatalf("Wait answered %v", err)
	}
	if got, want := r.all(), fixedBeats(100*time.Millisecond, 1, 1); !slices.Equal(got, want) {
		t.Fatalf("once Wait returned the jobs had seen %v, want %v", got, want)
	}
}

func TestTracksRunIndependently(t *testing.T) {
	clock := &testClock{}
	r := &recorder{}
	gate := make(chan struct{})
	defer close(gate)
	handled := make(chan struct{}, 1)
	spawn(t, clock, []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(10)), metronome.NewTrack[slowEvent](metronome.FixedStep(4))}, func(c *kernel.Configurator) {
		c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event metronome.UpdateEvent) (metronome.UpdateEvent, kernel.JobHandle) {
			handled <- struct{}{}
			<-gate // a handler that stalls its track's goroutine
			return event, in
		})
		record(c, r, slowBeat)
	})

	clock.fire(0)
	<-handled
	clock.fire(1)
	deadline := time.After(5 * time.Second)
	for len(r.all()) == 0 {
		select {
		case <-deadline:
			t.Fatal("a stalled update track held back the slow track")
		case <-time.After(time.Millisecond):
		}
	}
	if got, want := r.all(), fixedBeats(250*time.Millisecond, 1, 1); !slices.Equal(got, want) {
		t.Fatalf("the slow track saw %v, want %v", got, want)
	}
}

// stopObserver flags, in its Stop, that every plugin listed after it has
// stopped, then holds the kernel's stop open a while so a beat published
// late has time to show.
type stopObserver struct {
	stopped *atomic.Bool
}

func (stopObserver) Name() kernel.PluginName { return "observer" }

func (stopObserver) Dependencies() []kernel.PluginName { return nil }

func (stopObserver) Init(c *kernel.Configurator, config any) error { return nil }

func (stopObserver) Start(k *kernel.Kernel) error { return nil }

func (o stopObserver) Stop(k *kernel.Kernel) error {
	o.stopped.Store(true)
	time.Sleep(20 * time.Millisecond)
	return nil
}

func (stopObserver) Destroy() error { return nil }

func TestNothingPublishesAfterStop(t *testing.T) {
	stopped := &atomic.Bool{}
	beats := &atomic.Int64{}
	late := &atomic.Int64{}
	s := kernel.NewSupervisor(nil)
	plugin := metronome.NewPlugin()
	_, err := s.Spawn(metronomeConfig(&testClock{autoFire: true}, metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(10)), metronome.NewTrack[slowEvent](metronome.FixedStep(4))), 4, stopObserver{stopped: stopped}, plugin, testPlugin{init: func(c *kernel.Configurator) {
		count := func() {
			beats.Add(1)
			if stopped.Load() {
				late.Add(1)
			}
		}
		c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event metronome.UpdateEvent) (metronome.UpdateEvent, kernel.JobHandle) {
			count()
			return event, in
		})
		c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event slowEvent) (slowEvent, kernel.JobHandle) {
			count()
			return event, in
		})
	}})
	if err != nil {
		t.Fatal(err)
	}
	for beats.Load() < 100 {
		time.Sleep(time.Millisecond)
	}

	s.Quit()
	<-s.Done()
	if n := late.Load(); n != 0 {
		t.Fatalf("%d beats published after metronome's Stop returned", n)
	}
}

// beatAt answers a fixed-step beat of step: tick, its lost steps, and the
// steps from the epoch to its deadline.
func beatAt(step time.Duration, tick uint64, lostSteps, deadlineSteps int64) metronome.Beat {
	return metronome.Beat{
		Tick:          tick,
		Delta:         step,
		Lost:          time.Duration(lostSteps) * step,
		TimeFromStart: time.Duration(deadlineSteps) * step,
	}
}

// checkInvariant fails t unless the beats are ticks 1 onward, each with
// TimeFromStart the sum of Delta + Lost up to it.
func checkInvariant(t *testing.T, beats []metronome.Beat) {
	t.Helper()
	var sum time.Duration
	for i, beat := range beats {
		sum += beat.Delta + beat.Lost
		if beat.Tick != uint64(i+1) {
			t.Fatalf("beat %d is %v, want tick %d", i, beat, i+1)
		}
		if beat.TimeFromStart != sum {
			t.Fatalf("beat %v has TimeFromStart %v, want the sum of Delta + Lost %v", beat, beat.TimeFromStart, sum)
		}
	}
}

// heldRecord subscribes a handler to E that records each beat into published
// as it is published, and hands on as its out dependency a job that waits for
// gate to close, holding the beat in flight until then.
func heldRecord[E any](c *kernel.Configurator, published *recorder, header func(E) metronome.Beat, gate <-chan struct{}) {
	task := c.RegisterTask(func(access *kernel.Access) kernel.TaskBody[E, kernel.NoResult] {
		return func(k *kernel.Kernel, event E) kernel.NoResult {
			<-gate
			return kernel.NoResult{}
		}
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event E) (E, kernel.JobHandle) {
		published.add(header(event))
		return event, ex.Schedule(task, event, in).Handle()
	})
}

// waiter is a clock that waits for the tracks reading it to settle.
type waiter interface {
	Wait(ctx context.Context) error
}

// waitHeld fails t unless Wait on clock is still waiting after a while: a
// track has a beat in flight or owes debt.
func waitHeld(t *testing.T, clock waiter) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := clock.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait on a track held back answered %v, want the context's deadline", err)
	}
}

// waitPublished waits until r holds count beats, and fails t if it does not
// in time.
func waitPublished(t *testing.T, r *recorder, count int) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for len(r.all()) < count {
		select {
		case <-deadline:
			t.Fatalf("the track published %v, want %d beats", r.all(), count)
		case <-time.After(time.Millisecond):
		}
	}
}

func TestJumpPublishesDebtWithinTheCap(t *testing.T) {
	const step = 100 * time.Millisecond
	clock := metronome.NewManualClock()
	r := &recorder{}
	spawn(t, clock, []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(10))}, func(c *kernel.Configurator) {
		record(c, r, updateBeat)
	})

	clock.Jump(10 * step)
	if err := clock.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []metronome.Beat{
		beatAt(step, 1, 6, 7),
		beatAt(step, 2, 0, 8),
		beatAt(step, 3, 0, 9),
		beatAt(step, 4, 0, 10),
	}
	if got := r.all(); !slices.Equal(got, want) {
		t.Fatalf("a jump of 10 steps saw %v, want the 4 steps the debt cap keeps, after 6 lost: %v", got, want)
	}
}

func TestMaxDebtCapsDebt(t *testing.T) {
	const step = 100 * time.Millisecond
	clock := metronome.NewManualClock()
	r := &recorder{}
	spawn(t, clock, []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.Settings{Step: step, MaxInFlight: 1, MaxDebt: 2})}, func(c *kernel.Configurator) {
		record(c, r, updateBeat)
	})

	clock.Jump(5 * step)
	if err := clock.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []metronome.Beat{beatAt(step, 1, 3, 4), beatAt(step, 2, 0, 5)}
	if got := r.all(); !slices.Equal(got, want) {
		t.Fatalf("a jump of 5 steps with MaxDebt 2 saw %v, want %v", got, want)
	}
}

func TestTimeFromStartIsDeltaPlusLostAcrossDrops(t *testing.T) {
	const step = 100 * time.Millisecond
	clock := metronome.NewManualClock()
	r := &recorder{}
	spawn(t, clock, []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(10))}, func(c *kernel.Configurator) {
		record(c, r, updateBeat)
	})

	clock.Advance(3 * step)
	clock.Jump(9 * step)
	if err := clock.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	clock.Advance(2*step + step/2)
	clock.Jump(20 * step)
	if err := clock.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	beats := r.all()
	checkInvariant(t, beats)
	// 3 on pace, 4 of 9 owed, 2 on pace, 4 of 20 owed: 13 beats, the last on
	// the deadline of step 34, with 21 steps lost before it.
	if got, want := beats[len(beats)-1], beatAt(step, 13, 0, 34); got != want {
		t.Fatalf("the last beat is %v, want %v", got, want)
	}
}

func TestMaxInFlightHoldsBackATrack(t *testing.T) {
	const step = 100 * time.Millisecond
	clock := metronome.NewManualClock()
	published := &recorder{}
	gate := make(chan struct{})
	spawn(t, clock, []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.Settings{Step: step, MaxInFlight: 2})}, func(c *kernel.Configurator) {
		heldRecord(c, published, updateBeat, gate)
	})

	clock.Jump(3 * step)
	waitHeld(t, clock)
	if got, want := published.all(), fixedBeats(step, 1, 2); !slices.Equal(got, want) {
		t.Fatalf("with two beats held in flight the track published %v, want %v", got, want)
	}

	close(gate)
	if err := clock.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got, want := published.all(), fixedBeats(step, 1, 3); !slices.Equal(got, want) {
		t.Fatalf("once released the track published %v, want %v", got, want)
	}
}

func TestMaxInFlightZeroIsNoLimit(t *testing.T) {
	const step = 100 * time.Millisecond
	clock := metronome.NewManualClock()
	published := &recorder{}
	gate := make(chan struct{})
	spawn(t, clock, []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.Settings{Step: step})}, func(c *kernel.Configurator) {
		heldRecord(c, published, updateBeat, gate)
	})

	clock.Jump(4 * step)
	waitPublished(t, published, 4)
	waitHeld(t, clock)
	close(gate)
	if err := clock.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got, want := published.all(), fixedBeats(step, 1, 4); !slices.Equal(got, want) {
		t.Fatalf("with no in-flight limit the track published %v, want %v", got, want)
	}
}

func TestHeldOutDependencyStallsTheTrackUntilReleased(t *testing.T) {
	const step = 100 * time.Millisecond
	clock := metronome.NewManualClock()
	published := &recorder{}
	gate := make(chan struct{})
	spawn(t, clock, []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(10))}, func(c *kernel.Configurator) {
		heldRecord(c, published, updateBeat, gate)
	})

	clock.Jump(step)
	waitPublished(t, published, 1)
	clock.Jump(10 * step)
	waitHeld(t, clock)
	if got, want := published.all(), fixedBeats(step, 1, 1); !slices.Equal(got, want) {
		t.Fatalf("a stalled track published %v, want %v", got, want)
	}

	close(gate)
	if err := clock.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []metronome.Beat{
		beatAt(step, 1, 0, 1),
		beatAt(step, 2, 6, 8),
		beatAt(step, 3, 0, 9),
		beatAt(step, 4, 0, 10),
		beatAt(step, 5, 0, 11),
	}
	if got := published.all(); !slices.Equal(got, want) {
		t.Fatalf("once released the track published %v, want the catch-up within the cap %v", got, want)
	}
}

func TestAdvanceSettlesEachBeatWithSeveralInFlight(t *testing.T) {
	const step = 100 * time.Millisecond
	clock := metronome.NewManualClock()
	finished := &atomic.Uint64{}
	early := &atomic.Int64{}
	spawn(t, clock, []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.Settings{Step: step, MaxInFlight: 2})}, func(c *kernel.Configurator) {
		task := c.RegisterTask(func(access *kernel.Access) kernel.TaskBody[metronome.UpdateEvent, kernel.NoResult] {
			return func(k *kernel.Kernel, event metronome.UpdateEvent) kernel.NoResult {
				time.Sleep(time.Millisecond)
				finished.Store(event.Tick)
				return kernel.NoResult{}
			}
		})
		c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event metronome.UpdateEvent) (metronome.UpdateEvent, kernel.JobHandle) {
			if finished.Load() != event.Tick-1 {
				early.Add(1)
			}
			return event, ex.Schedule(task, event, in).Handle()
		})
	})

	clock.Advance(20 * step)
	if n := early.Load(); n != 0 {
		t.Fatalf("%d beats published before the one due before them settled", n)
	}
	if got := finished.Load(); got != 20 {
		t.Fatalf("once Advance returned tick %d had finished, want 20", got)
	}
}

func TestBeatPublishAndWaitAllocateNothing(t *testing.T) {
	clock := metronome.NewManualClock()
	spawn(t, clock, []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(10))}, func(c *kernel.Configurator) {
		task := c.RegisterTask(func(access *kernel.Access) kernel.TaskBody[metronome.UpdateEvent, kernel.NoResult] {
			return func(k *kernel.Kernel, event metronome.UpdateEvent) kernel.NoResult { return kernel.NoResult{} }
		})
		c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event metronome.UpdateEvent) (metronome.UpdateEvent, kernel.JobHandle) {
			return event, ex.Schedule(task, event, in).Handle()
		})
	})
	beat := func() { clock.Advance(100 * time.Millisecond) }
	beat()
	if got := testing.AllocsPerRun(1000, beat); got != 0 {
		t.Fatalf("a beat's publish and wait allocated %v times", got)
	}
}

// drawEvent is the event of a variable track.
type drawEvent struct {
	metronome.Beat
}

func drawBeat(event drawEvent) metronome.Beat { return event.Beat }

// variableBeat answers a variable-track beat: tick, its Delta and Lost, and
// its publish time from the epoch.
func variableBeat(tick uint64, delta, lost, timeFromStart time.Duration) metronome.Beat {
	return metronome.Beat{Tick: tick, Delta: delta, Lost: lost, TimeFromStart: timeFromStart}
}

func TestVariableTrackBeatsOncePerCallAtTheTarget(t *testing.T) {
	const ms = time.Millisecond
	clock := metronome.NewManualClock()
	r := &recorder{}
	spawn(t, clock, []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(10)), metronome.NewTrack[drawEvent](metronome.Variable())}, func(c *kernel.Configurator) {
		record(c, r, drawBeat)
	})
	if err := clock.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := r.all(); len(got) != 0 {
		t.Fatalf("before any call the variable track published %v", got)
	}

	clock.Advance(350 * ms)
	if got, want := r.all(), []metronome.Beat{variableBeat(1, 350*ms, 0, 350*ms)}; !slices.Equal(got, want) {
		t.Fatalf("an Advance across 3 update deadlines saw %v, want one beat at the target %v", got, want)
	}
	clock.Jump(50 * ms)
	if err := clock.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []metronome.Beat{variableBeat(1, 350*ms, 0, 350*ms), variableBeat(2, 50*ms, 0, 400*ms)}
	if got := r.all(); !slices.Equal(got, want) {
		t.Fatalf("after a Jump the variable track saw %v, want %v", got, want)
	}
}

func TestMinIntervalSpacesVariableBeats(t *testing.T) {
	const ms = time.Millisecond
	clock := metronome.NewManualClock()
	r := &recorder{}
	settings := metronome.Variable()
	settings.MinInterval = 100 * ms
	spawn(t, clock, []metronome.Track{metronome.NewTrack[drawEvent](settings)}, func(c *kernel.Configurator) {
		record(c, r, drawBeat)
	})

	for range 4 {
		clock.Advance(60 * ms)
	}
	want := []metronome.Beat{variableBeat(1, 120*ms, 0, 120*ms), variableBeat(2, 120*ms, 0, 240*ms)}
	if got := r.all(); !slices.Equal(got, want) {
		t.Fatalf("4 Advances of 60ms with a MinInterval of 100ms saw %v, want %v", got, want)
	}
}

func TestMaxDeltaClampsAStall(t *testing.T) {
	const ms = time.Millisecond
	clock := metronome.NewManualClock()
	r := &recorder{}
	settings := metronome.Variable()
	settings.MaxDelta = 50 * ms
	spawn(t, clock, []metronome.Track{metronome.NewTrack[drawEvent](settings)}, func(c *kernel.Configurator) {
		record(c, r, drawBeat)
	})

	clock.Advance(20 * ms)
	clock.Jump(500 * ms)
	if err := clock.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	clock.Advance(30 * ms)
	want := []metronome.Beat{
		variableBeat(1, 20*ms, 0, 20*ms),
		variableBeat(2, 50*ms, 450*ms, 520*ms),
		variableBeat(3, 30*ms, 0, 550*ms),
	}
	beats := r.all()
	if !slices.Equal(beats, want) {
		t.Fatalf("a stall of 500ms with a MaxDelta of 50ms saw %v, want %v", beats, want)
	}
	checkInvariant(t, beats)
}

func TestVariableTrackHeldInFlightBeatsOnceReleased(t *testing.T) {
	const ms = time.Millisecond
	clock := metronome.NewManualClock()
	published := &recorder{}
	gate := make(chan struct{})
	spawn(t, clock, []metronome.Track{metronome.NewTrack[drawEvent](metronome.Variable())}, func(c *kernel.Configurator) {
		heldRecord(c, published, drawBeat, gate)
	})

	clock.Jump(10 * ms)
	waitPublished(t, published, 1)
	clock.Jump(10 * ms)
	clock.Jump(10 * ms)
	waitHeld(t, clock)
	if got, want := published.all(), []metronome.Beat{variableBeat(1, 10*ms, 0, 10*ms)}; !slices.Equal(got, want) {
		t.Fatalf("with its beat held a bare Variable track published %v, want only %v", got, want)
	}
	close(gate)
	if err := clock.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []metronome.Beat{variableBeat(1, 10*ms, 0, 10*ms), variableBeat(2, 20*ms, 0, 30*ms)}
	if got := published.all(); !slices.Equal(got, want) {
		t.Fatalf("a held variable track published %v, want one beat once released %v", got, want)
	}
}

func TestVariableTrackWaitsOnItsTimerResetToNow(t *testing.T) {
	const ms = time.Millisecond
	clock := &testClock{}
	r := &recorder{}
	spawn(t, clock, []metronome.Track{metronome.NewTrack[drawEvent](metronome.Variable())}, func(c *kernel.Configurator) {
		record(c, r, drawBeat)
	})

	if got := clock.lastReset(0); got != 0 {
		t.Fatalf("at Start the variable track's timer was reset to %v, want now, 0", got)
	}
	clock.setNow(10 * ms)
	clock.fire(0)
	if err := clock.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	clock.setNow(25 * ms)
	time.Sleep(20 * ms)
	if got, want := r.all(), []metronome.Beat{variableBeat(1, 10*ms, 0, 10*ms)}; !slices.Equal(got, want) {
		t.Fatalf("before its timer fired again the variable track published %v, want %v", got, want)
	}
	if got := clock.lastReset(0); got != 10*ms {
		t.Fatalf("after a beat at 10ms the timer was reset to %v, want now, 10ms", got)
	}

	clock.fire(0)
	waitPublished(t, r, 2)
	if got, want := r.all()[1], variableBeat(2, 15*ms, 0, 25*ms); got != want {
		t.Fatalf("once its timer fired the variable track published %v, want %v", got, want)
	}
}

// interpolation is what a beat says of its reference track.
type interpolation struct {
	tick  uint64
	alpha float32
}

// interpolations answers what each of beats says of its reference track.
func interpolations(beats []metronome.Beat) []interpolation {
	var all []interpolation
	for _, beat := range beats {
		all = append(all, interpolation{tick: beat.InterpolatedTick, alpha: beat.Alpha})
	}
	return all
}

// interpolatingDraw answers the settings of a variable track interpolating
// against the UpdateEvent track.
func interpolatingDraw() metronome.Settings {
	settings := metronome.Variable()
	settings.InterpolateAgainst = reflect.TypeFor[metronome.UpdateEvent]()
	return settings
}

func TestUninterpolatedTracksHaveZeroAlphaAndInterpolatedTick(t *testing.T) {
	const ms = time.Millisecond
	clock := metronome.NewManualClock()
	r := &recorder{}
	spawn(t, clock, []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(10)), metronome.NewTrack[drawEvent](metronome.Variable())}, func(c *kernel.Configurator) {
		record(c, r, updateBeat)
		record(c, r, drawBeat)
	})

	clock.Advance(150 * ms)
	clock.Advance(100 * ms)
	beats := r.all()
	if len(beats) != 4 {
		t.Fatalf("the tracks published %v, want 2 update and 2 draw beats", beats)
	}
	for _, beat := range beats {
		if beat.Alpha != 0 || beat.InterpolatedTick != 0 {
			t.Fatalf("a beat of a track not interpolating is %v, want a zero Alpha and InterpolatedTick", beat)
		}
	}
}

func TestBeatInterpolatesAgainstTheReferenceTrack(t *testing.T) {
	const ms = time.Millisecond
	clock := metronome.NewManualClock()
	r := &recorder{}
	spawn(t, clock, []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(10)), metronome.NewTrack[drawEvent](interpolatingDraw())}, func(c *kernel.Configurator) {
		record(c, r, drawBeat)
	})

	clock.Advance(50 * ms)
	clock.Advance(75 * ms)
	clock.Advance(50 * ms)
	clock.Advance(75 * ms)
	// Draw beats at 50, 125, 175 and 250ms, against update ticks due every
	// 100ms: none yet, then tick 1 twice, then tick 2.
	want := []interpolation{{0, 0.5}, {1, 0.25}, {1, 0.75}, {2, 0.5}}
	if got := interpolations(r.all()); !slices.Equal(got, want) {
		t.Fatalf("the draw track interpolated %v, want %v", got, want)
	}
}

func TestAlphaWhileTheReferenceTrackIsInDebt(t *testing.T) {
	const step = 100 * time.Millisecond
	clock := metronome.NewManualClock()
	r := &recorder{}
	gate := make(chan struct{})
	spawn(t, clock, []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(10)), metronome.NewTrack[drawEvent](interpolatingDraw())}, func(c *kernel.Configurator) {
		c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event metronome.UpdateEvent) (metronome.UpdateEvent, kernel.JobHandle) {
			if event.Tick >= 2 {
				<-gate // tick 2 does not finish publishing until released
			}
			return event, in
		})
		record(c, r, drawBeat)
	})

	clock.Advance(step + step/4)
	clock.Jump(3 * step)
	waitPublished(t, r, 2)
	close(gate)
	if err := clock.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	clock.Advance(step / 4)
	// At 425ms the update track owes ticks 2 to 4 and is still publishing
	// tick 2, so the draw beat shows tick 1 at the largest Alpha below 1. Once
	// it has caught up, the beat at 450ms is halfway past tick 4.
	want := []interpolation{{1, 0.25}, {1, math.Nextafter32(1, 0)}, {4, 0.5}}
	if got := interpolations(r.all()); !slices.Equal(got, want) {
		t.Fatalf("the draw track interpolated %v, want %v", got, want)
	}
}

func TestInterpolationReadsAReferenceTrackBeatingAlongside(t *testing.T) {
	r := &recorder{}
	// Every timer fires at once on every Reset, so both tracks beat as fast as
	// they can, the update track's ticks published while draw beats read them.
	spawn(t, &testClock{autoFire: true}, []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(10)), metronome.NewTrack[drawEvent](interpolatingDraw())}, func(c *kernel.Configurator) {
		record(c, r, drawBeat)
	})

	waitPublished(t, r, 200)
	beats := r.all()
	for i, beat := range beats {
		if i > 0 && beat.InterpolatedTick < beats[i-1].InterpolatedTick {
			t.Fatalf("draw beat %v shows an earlier tick than the beat before it, %v", beat, beats[i-1])
		}
		if beat.Alpha < 0 || beat.Alpha >= 1 {
			t.Fatalf("draw beat %v has an Alpha outside [0,1)", beat)
		}
	}
	if last := beats[len(beats)-1]; last.InterpolatedTick == 0 {
		t.Fatalf("after %d draw beats the last, %v, shows no update tick", len(beats), last)
	}
}

func TestInterpolatingAgainstNoFixedStepTrackPanics(t *testing.T) {
	for name, reference := range map[string]reflect.Type{
		"undeclared": reflect.TypeFor[slowEvent](),
		"variable":   reflect.TypeFor[drawEvent](),
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("interpolating against the %s track %v did not panic", name, reference)
				}
			}()
			settings := metronome.Variable()
			settings.InterpolateAgainst = reference
			spawn(t, metronome.NewManualClock(), []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(10)), metronome.NewTrack[drawEvent](settings)}, func(c *kernel.Configurator) {})
		})
	}
}
