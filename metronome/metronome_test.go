package metronome_test

import (
	"context"
	"errors"
	"slices"
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

// spawn spawns a kernel of metronome on clock and a test plugin whose Init
// runs init, and answers the plugin and the kernel. The supervisor quits when
// the test ends.
func spawn(t *testing.T, clock metronome.Clock, init func(c *kernel.Configurator)) (*metronome.Plugin, *kernel.Kernel) {
	t.Helper()
	s := kernel.NewSupervisor(nil)
	t.Cleanup(s.Quit)
	plugin := metronome.NewPlugin(metronome.WithClock(clock))
	id, err := s.Spawn(nil, 4, plugin, testPlugin{init: init})
	if err != nil {
		t.Fatal(err)
	}
	return plugin, s.GetKernel(id)
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
	spawn(t, clock, func(c *kernel.Configurator) {
		metronome.AddTrack[metronome.UpdateEvent](c, metronome.FixedStep(10))
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
	spawn(t, clock, func(c *kernel.Configurator) {
		metronome.AddTrack[metronome.UpdateEvent](c, metronome.FixedStep(0))
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
	spawn(t, clock, func(c *kernel.Configurator) {
		metronome.AddTrack[metronome.UpdateEvent](c, metronome.FixedStep(10))
		metronome.AddTrack[slowEvent](c, metronome.FixedStep(4))
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
	_, k := spawn(t, metronome.NewManualClock(), func(c *kernel.Configurator) {
		metronome.AddTrack[metronome.UpdateEvent](c, metronome.FixedStep(10))
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
	spawn(t, metronome.NewManualClock(), func(c *kernel.Configurator) {
		metronome.AddTrack[metronome.UpdateEvent](c, metronome.FixedStep(10))
		metronome.AddTrack[metronome.UpdateEvent](c, metronome.FixedStep(20))
	})
}

// testClock is a Clock the test drives: Now reads what setNow set, and a timer
// fires when the test calls fire, or at once on every Reset if autoFire.
type testClock struct {
	autoFire bool
	mutex    sync.Mutex
	now      time.Duration
	timers   []*testTimer
}

type testTimer struct {
	clock   *testClock
	channel chan struct{}
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

func (t *testTimer) Reset(at time.Duration) {
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
	plugin, _ := spawn(t, clock, func(c *kernel.Configurator) {
		metronome.AddTrack[metronome.UpdateEvent](c, metronome.FixedStep(10))
		gatedRecord(c, r, updateBeat, gate)
	})
	if err := plugin.Wait(t.Context()); err != nil {
		t.Fatalf("Wait before any deadline answered %v", err)
	}

	clock.setNow(100 * time.Millisecond)
	clock.fire(0)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := plugin.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait with a beat held in flight answered %v, want the context's deadline", err)
	}

	close(gate)
	if err := plugin.Wait(t.Context()); err != nil {
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
	spawn(t, clock, func(c *kernel.Configurator) {
		metronome.AddTrack[metronome.UpdateEvent](c, metronome.FixedStep(10))
		metronome.AddTrack[slowEvent](c, metronome.FixedStep(4))
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
	plugin := metronome.NewPlugin(metronome.WithClock(&testClock{autoFire: true}))
	_, err := s.Spawn(nil, 4, stopObserver{stopped: stopped}, plugin, testPlugin{init: func(c *kernel.Configurator) {
		metronome.AddTrack[metronome.UpdateEvent](c, metronome.FixedStep(10))
		metronome.AddTrack[slowEvent](c, metronome.FixedStep(4))
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
