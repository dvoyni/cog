package metronome

import (
	"fmt"
	"reflect"
	"time"

	"github.com/dvoyni/cog/kernel"
)

// Settings is a track's pacing, given to AddTrack.
type Settings struct {
	// Step is the time each beat of a fixed-step track stands for: tick n is
	// due at the epoch + n × Step.
	Step time.Duration
}

// FixedStep answers the settings of a fixed-step track beating hz times a
// second; hz zero is 60. A negative hz panics.
func FixedStep(hz int) Settings {
	if hz < 0 {
		panic(fmt.Sprintf("metronome: a fixed step of %d hz", hz))
	}
	if hz == 0 {
		hz = 60
	}
	return Settings{Step: time.Second / time.Duration(hz)}
}

// AddTrack declares a track publishing E, paced by settings. It is called in
// a plugin's Init, listed after metronome. E embeds Beat, which metronome
// stamps on every beat; the rest of E is the payload, zero as published.
//
// A track's event type is its identity. It is a composition error, and
// panics, for metronome not to be initialized yet, for a track of E to be
// declared twice, or for settings to have no step.
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
	// event is made once and reused: stamping a local through P would move it
	// to the heap on every beat.
	event := new(E)
	plugin.tracks = append(plugin.tracks, &track{
		eventType:    eventType,
		settings:     settings,
		nextDeadline: settings.Step,
		signal:       kernel.NewSignal(),
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
// running, inFlight and nextDeadline are guarded by the plugin's mutex.
type track struct {
	eventType reflect.Type
	settings  Settings
	// publish publishes one beat of the track's event, stamped with beat, and
	// answers the publication's out dependency.
	publish func(k *kernel.Kernel, beat Beat) kernel.JobHandle
	timer   Timer
	// signal is armed on the out dependency of the beat in flight.
	signal       *kernel.Signal
	tick         uint64
	nextDeadline time.Duration
	running      bool
	inFlight     bool
}

// run is the track's goroutine: it waits for each deadline, publishes the
// beat due, and waits for that beat to finish, until the plugin stops.
func (t *track) run(p *Plugin, k *kernel.Kernel) {
	defer p.trackGroup.Done()
	defer t.end(p)
	for {
		select {
		case <-t.timer.C():
		case <-p.stop:
			return
		}
		p.mutex.Lock()
		t.tick++
		t.nextDeadline = time.Duration(t.tick+1) * t.settings.Step
		t.inFlight = true
		p.mutex.Unlock()
		out := t.publish(k, Beat{
			Tick:          t.tick,
			Delta:         t.settings.Step,
			TimeFromStart: time.Duration(t.tick) * t.settings.Step,
		})
		k.Notify(t.signal, out)
		select {
		case <-t.signal.C():
		case <-p.stop:
			return
		}
		p.mutex.Lock()
		t.inFlight = false
		p.settleChanged.Broadcast()
		p.mutex.Unlock()
		t.timer.Reset(t.nextDeadline)
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
