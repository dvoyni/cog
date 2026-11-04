package metronome

import (
	"fmt"
	"reflect"
	"time"

	"github.com/dvoyni/cog/kernel"
)

// defaultMaxDebt is the debt cap of a track whose Settings leave MaxDebt zero.
const defaultMaxDebt = 4

// Settings is a track's pacing, given to AddTrack.
type Settings struct {
	// Step is the time each beat of a fixed-step track stands for: tick n is
	// due at the epoch + n × Step, plus the steps dropped before it.
	Step time.Duration
	// MaxInFlight is how many of the track's beats may be in flight at once:
	// a new beat starts only while fewer are. A beat is in flight until its
	// publication's out dependency finishes. Zero is no limit.
	MaxInFlight int
	// MaxDebt caps a fixed-step track's debt, the steps it owes but has not
	// published: a step owed past the cap is dropped, and its time is the next
	// beat's Lost. Zero is 4.
	MaxDebt int
}

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

// AddTrack declares a track publishing E, paced by settings. It is called in
// a plugin's Init, listed after metronome. E embeds Beat, which metronome
// stamps on every beat; the rest of E is the payload, zero as published.
//
// A track's event type is its identity. It is a composition error, and
// panics, for metronome not to be initialized yet, for a track of E to be
// declared twice, for settings to have no step, or for MaxInFlight or MaxDebt
// to be negative.
func AddTrack[E any, P interface {
	*E
	beater
}](configurator *kernel.Configurator, settings Settings) {
	eventType := reflect.TypeFor[E]()
	plugin, ok := configurator.Resource[*Plugin]()
	if !ok {
		panic(fmt.Sprintf("metronome: track of %v is declared before the metronome plugin", eventType))
	}
	for _, t := range plugin.tracks {
		if t.eventType == eventType {
			panic(fmt.Sprintf("metronome: two tracks of %v", eventType))
		}
	}
	if settings.Step <= 0 {
		panic(fmt.Sprintf("metronome: track of %v has no step", eventType))
	}
	if settings.MaxInFlight < 0 || settings.MaxDebt < 0 {
		panic(fmt.Sprintf("metronome: track of %v has a negative MaxInFlight or MaxDebt", eventType))
	}
	if settings.MaxDebt == 0 {
		settings.MaxDebt = defaultMaxDebt
	}
	signals := make([]*kernel.Signal, max(settings.MaxInFlight, 1))
	for i := range signals {
		signals[i] = kernel.NewSignal()
	}
	// event is made once and reused: stamping a local through P would move it
	// to the heap on every beat.
	event := new(E)
	plugin.tracks = append(plugin.tracks, &track{
		eventType:    eventType,
		settings:     settings,
		nextDeadline: settings.Step,
		signals:      signals,
		publish: func(k *kernel.Kernel, beat Beat) kernel.JobHandle {
			var zero E
			*event = zero
			P(event).stamp(beat)
			_, out := k.Publish(*event)
			return out
		},
	})
}

// track is one declared track: its pacing, and the state its goroutine keeps.
// running, inFlightCount and nextDeadline are guarded by the plugin's mutex.
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
	// stepCount counts the steps behind the track: published or dropped.
	stepCount    uint64
	nextDeadline time.Duration
	running      bool
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
			t.publishDue(p, k)
			deadlineReached = false
			t.timer.Reset(t.nextDeadline)
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

// publishDue publishes the beat due, first dropping the steps owed past the
// debt cap, and arms a signal on its out dependency.
func (t *track) publishDue(p *Plugin, k *kernel.Kernel) {
	step := t.settings.Step
	now := p.clock.Now()
	p.mutex.Lock()
	var lost time.Duration
	if owed := int64(now/step) - int64(t.stepCount); owed > int64(t.settings.MaxDebt) {
		dropped := owed - int64(t.settings.MaxDebt)
		t.stepCount += uint64(dropped)
		lost = time.Duration(dropped) * step
	}
	t.stepCount++
	t.tick++
	t.nextDeadline = time.Duration(t.stepCount+1) * step
	if t.inFlightCount == len(t.signals) {
		t.growSignals()
	}
	signal := t.signals[(t.oldestSignal+t.inFlightCount)%len(t.signals)]
	t.inFlightCount++
	p.mutex.Unlock()
	out := t.publish(k, Beat{
		Tick:          t.tick,
		Delta:         step,
		Lost:          lost,
		TimeFromStart: time.Duration(t.stepCount) * step,
	})
	k.Notify(signal, out)
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
