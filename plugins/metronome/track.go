package metronome

import (
	"fmt"
	"reflect"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/dvoyni/cog/kernel"
)

// defaultMaxDebt is the debt cap of a track whose Settings leave MaxDebt zero.
const defaultMaxDebt = 4

// alphaInDebt is a beat's Alpha while its reference track is in debt: the
// largest float32 below 1.
const alphaInDebt float32 = 1 - 1.0/(1<<24)

// Settings is a track's pacing, given to NewTrack.
type Settings struct {
	// Step is the time each beat of a fixed-step track stands for: tick n is
	// due at the epoch + n × Step, plus the steps dropped before it. Zero makes
	// a variable track, each beat standing for the time since the previous one.
	Step time.Duration
	// MinInterval is the least time between two beats of a variable track: its
	// timer is reset to the previous beat plus MinInterval. Zero is none: the
	// timer is reset to now.
	MinInterval time.Duration
	// MaxDelta clamps the Delta of a variable track's beat, after a stall; the
	// time clamped away is that beat's Lost. Zero is no clamp.
	MaxDelta time.Duration
	// MaxInFlight is how many of the track's beats may be in flight at once:
	// a new beat starts only while fewer are. A beat is in flight until its
	// publication's out dependency finishes. Zero is no limit.
	MaxInFlight int
	// MaxDebt caps a fixed-step track's debt, the steps it owes but has not
	// published: a step owed past the cap is dropped, and its time is the next
	// beat's Lost. Zero is 4. It means nothing on a variable track.
	MaxDebt int
	// InterpolateAgainst names the reference track by its event type: a
	// fixed-step track each beat interpolates against, giving its Alpha and
	// InterpolatedTick. Nil is none, and leaves both zero. Naming no
	// fixed-step track is a composition error, and panics at Start.
	InterpolateAgainst reflect.Type
}

// Variable answers the settings of a variable track: one beat in flight, as
// FixedStep keeps, so a beat publishes only once the previous one has
// finished; no MinInterval and no MaxDelta clamp, each to be set as the track
// needs.
func Variable() Settings { return Settings{MaxInFlight: 1} }

// FixedStep answers the settings of a fixed-step track beating hz times a
// second; hz zero is 60. It keeps one beat in flight, so a step publishes only
// once the previous one has finished, and time piles up only in the debt. A
// negative hz panics.
func FixedStep(hz int) Settings {
	if hz < 0 {
		panic(fmt.Sprintf("metronome: a fixed step of %d hz", hz))
	}
	if hz == 0 {
		hz = 60
	}
	return Settings{Step: time.Second / time.Duration(hz), MaxInFlight: 1}
}

// Track is a track as declared: its event type, its pacing, and how to
// publish its event. NewTrack makes it, and Config.Tracks hands it to
// metronome, which builds the track's state anew in each kernel's Init.
type Track struct {
	eventType reflect.Type
	settings  Settings
	// newPublish makes the publish func of one kernel's track, with an event
	// of its own to stamp.
	newPublish func() func(k *kernel.Kernel, beat Beat) kernel.JobHandle
}

// NewTrack declares a track publishing E, paced by settings, for
// Config.Tracks. E embeds Beat, which metronome stamps on every beat; the rest
// of E is the payload, zero as published.
//
// A track's event type is its identity. It is a composition error, and
// panics, for any of its settings to be negative, and for a Config to hold two
// tracks of one event type.
func NewTrack[E any, P interface {
	*E
	beater
}](settings Settings) Track {
	eventType := reflect.TypeFor[E]()
	if settings.Step < 0 || settings.MinInterval < 0 || settings.MaxDelta < 0 || settings.MaxInFlight < 0 || settings.MaxDebt < 0 {
		panic(fmt.Sprintf("metronome: track of %v has a negative setting: %+v", eventType, settings))
	}
	if settings.MaxDebt == 0 {
		settings.MaxDebt = defaultMaxDebt
	}
	return Track{
		eventType: eventType,
		settings:  settings,
		newPublish: func() func(k *kernel.Kernel, beat Beat) kernel.JobHandle {
			// event is made once per kernel and reused: stamping a local
			// through P would move it to the heap on every beat.
			event := new(E)
			return func(k *kernel.Kernel, beat Beat) kernel.JobHandle {
				var zero E
				*event = zero
				P(event).stamp(beat)
				_, out := k.Publish(*event)
				return out
			}
		},
	}
}

// track is one declared track: its pacing, and the state its goroutine keeps.
// running, inFlightCount, nextDeadline and lastBeat are guarded by the
// plugin's mutex.
type track struct {
	eventType reflect.Type
	settings  Settings
	// publish publishes one beat of the track's event, stamped with beat, and
	// answers the publication's out dependency.
	publish func(k *kernel.Kernel, beat Beat) kernel.JobHandle
	timer   Timer
	// signals is a ring of reused signals, one armed on the out dependency of
	// each beat in flight, oldest first from oldestSignal. Without an
	// in-flight limit it grows to the most beats ever in flight at once.
	signals       []*kernel.Signal
	oldestSignal  int
	inFlightCount int
	tick          uint64
	// stepCount counts the steps behind a fixed-step track: published or
	// dropped.
	stepCount    uint64
	nextDeadline time.Duration
	// lastBeat is when a variable track published its last beat, from the
	// epoch; the epoch itself before the first.
	lastBeat time.Duration
	running  bool
	// reference is the track this one interpolates against, found at Start;
	// nil for none.
	reference *track
	// published is a fixed-step track's last published tick, for the tracks
	// interpolating against it.
	published publishedTick
}

// publishedTick is a fixed-step track's last published tick and that tick's
// deadline, shared with the tracks interpolating against it through atomics.
// Its track's goroutine alone stores it. version is odd while a store is
// under way, so a load retries rather than read a tick with another tick's
// deadline: a seqlock, taking no lock and allocating nothing.
type publishedTick struct {
	version  atomic.Uint64
	tick     atomic.Uint64
	deadline atomic.Int64
}

// run is the track's goroutine. It waits on its timer for the next deadline,
// on its oldest beat in flight, and on stop; once the deadline is reached and
// a slot is free, it publishes the beat due and rearms the timer, until the
// plugin stops. A track in debt rearms on a deadline already past, so its
// owed steps publish back-to-back.
func (t *track) run(p *Plugin, k *kernel.Kernel) {
	defer p.trackGroup.Done()
	defer t.end(p)
	deadlineReached := false
	for {
		var fired, finished <-chan struct{}
		if !deadlineReached {
			fired = t.timer.C()
		}
		if t.inFlightCount > 0 {
			finished = t.signals[t.oldestSignal].C()
		}
		select {
		case <-fired:
			deadlineReached = true
		case <-finished:
			p.mutex.Lock()
			t.oldestSignal = (t.oldestSignal + 1) % len(t.signals)
			t.inFlightCount--
			p.settleChanged.Broadcast()
			p.mutex.Unlock()
		case <-p.stop:
			return
		}
		if deadlineReached && (t.settings.MaxInFlight == 0 || t.inFlightCount < t.settings.MaxInFlight) {
			if !t.publishDue(p, k) {
				t.timer.Reset(t.nextDeadline)
			}
			deadlineReached = false
		}
	}
}

// end stops the track's timer and marks it no longer running.
func (t *track) end(p *Plugin) {
	t.timer.Stop()
	p.mutex.Lock()
	defer p.mutex.Unlock()
	t.running = false
	p.settleChanged.Broadcast()
}

// publishDue publishes the beat due and arms a signal on its out dependency.
// A fixed-step track first drops the steps owed past the debt cap; a variable
// track clamps its Delta to MaxDelta. A variable track on a ManualClock reads
// the time and rearms its timer in one step, still owing until the beat is
// in flight, and publishDue answers true; otherwise the caller rearms it.
func (t *track) publishDue(p *Plugin, k *kernel.Kernel) (rearmed bool) {
	p.mutex.Lock()
	// The reference track is read before the time, so its tick's deadline is
	// not after now on a clock that fires on time.
	var interpolatedTick uint64
	var interpolatedDeadline time.Duration
	if t.reference != nil {
		interpolatedTick, interpolatedDeadline = t.reference.published.load()
	}
	var now time.Duration
	if timer, ok := t.timer.(*manualTimer); ok && t.settings.Step == 0 {
		now = timer.rearm(t.settings.MinInterval)
		rearmed = true
	} else {
		now = p.clock.Now()
	}
	t.tick++
	beat := Beat{Tick: t.tick}
	if step := t.settings.Step; step > 0 {
		if owed := int64(now/step) - int64(t.stepCount); owed > int64(t.settings.MaxDebt) {
			dropped := owed - int64(t.settings.MaxDebt)
			t.stepCount += uint64(dropped)
			beat.Lost = time.Duration(dropped) * step
		}
		t.stepCount++
		t.nextDeadline = time.Duration(t.stepCount+1) * step
		beat.Delta = step
		beat.TimeFromStart = time.Duration(t.stepCount) * step
	} else {
		beat.Delta = now - t.lastBeat
		if maxDelta := t.settings.MaxDelta; maxDelta > 0 && beat.Delta > maxDelta {
			beat.Lost = beat.Delta - maxDelta
			beat.Delta = maxDelta
		}
		beat.TimeFromStart = now
		t.lastBeat = now
		t.nextDeadline = now + t.settings.MinInterval
	}
	if t.reference != nil {
		// Past the tick's step the reference track owes its next one: in debt,
		// the beat shows the newest state. A clock that fired the tick early
		// leaves now before its deadline.
		beat.InterpolatedTick = interpolatedTick
		beat.Alpha = float32(max(float64(now-interpolatedDeadline), 0) / float64(t.reference.settings.Step))
		if beat.Alpha >= 1 {
			beat.Alpha = alphaInDebt
		}
	}
	if t.inFlightCount == len(t.signals) {
		t.growSignals()
	}
	signal := t.signals[(t.oldestSignal+t.inFlightCount)%len(t.signals)]
	t.inFlightCount++
	p.mutex.Unlock()
	out := t.publish(k, beat)
	// Stored once the publication has scheduled its writes, so a beat that
	// reads this tick has its tasks ordered after them.
	if t.settings.Step > 0 {
		t.published.store(beat.Tick, beat.TimeFromStart)
	}
	k.Notify(signal, out)
	return rearmed
}

// growSignals doubles the ring of signals of a track with no in-flight limit,
// keeping the beats in flight oldest first. The caller holds the plugin's
// mutex.
func (t *track) growSignals() {
	signals := make([]*kernel.Signal, 0, 2*len(t.signals))
	signals = append(signals, t.signals[t.oldestSignal:]...)
	signals = append(signals, t.signals[:t.oldestSignal]...)
	for range len(t.signals) {
		signals = append(signals, kernel.NewSignal())
	}
	t.signals = signals
	t.oldestSignal = 0
}

// owes answers whether the track has reached a beat it has not published yet.
// A fixed-step track owes once now has reached its next deadline. A variable
// track on a ManualClock owes once its timer has fired, until it rearms it; on
// any other clock, once now has reached its next deadline and moved on since
// its last beat. The caller holds the plugin's mutex.
func (t *track) owes(now time.Duration) bool {
	if t.settings.Step > 0 {
		return now >= t.nextDeadline
	}
	if timer, ok := t.timer.(*manualTimer); ok {
		return timer.pending()
	}
	return now >= t.nextDeadline && now > t.lastBeat
}

// store shares tick, just published, and its deadline. Only the track's
// goroutine calls it.
func (p *publishedTick) store(tick uint64, deadline time.Duration) {
	p.version.Add(1)
	p.tick.Store(tick)
	p.deadline.Store(int64(deadline))
	p.version.Add(1)
}

// load answers the last tick stored and its deadline, as one pair; zero and
// the epoch before the first.
func (p *publishedTick) load() (uint64, time.Duration) {
	for {
		version := p.version.Load()
		tick := p.tick.Load()
		deadline := p.deadline.Load()
		if version%2 == 0 && p.version.Load() == version {
			return tick, time.Duration(deadline)
		}
		runtime.Gosched()
	}
}
