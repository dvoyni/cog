package metronome

import (
	"context"
	"fmt"
	"sync"

	"github.com/dvoyni/cog/kernel"
)

// PluginName is metronome's plugin name.
const PluginName kernel.PluginName = "metronome"

// Plugin is metronome: it runs one goroutine per track its Config declares,
// between Start and Stop, each publishing its track's beats. It is listed
// before the plugins handling those beats, so it stops last.
type Plugin struct {
	clock  Clock
	tracks []*track
	// mutex guards each track's settling state, which settleChanged is
	// broadcast on.
	mutex         sync.Mutex
	settleChanged sync.Cond
	stop          chan struct{}
	trackGroup    sync.WaitGroup
}

// Config is metronome's part of the kernel's config, under PluginName. A
// kernel spawned without one gets the zero Config.
type Config struct {
	// Clock is where metronome reads time and whose timers its tracks wait
	// on. Nil is a WallClock: monotonic time, with the epoch at Start.
	Clock Clock
	// Tracks are the tracks metronome paces, each made by NewTrack.
	Tracks []Track
}

// NewPlugin returns metronome. It is configured by its Config, given to Init.
func NewPlugin() *Plugin {
	plugin := &Plugin{}
	plugin.settleChanged.L = &plugin.mutex
	return plugin
}

// Name returns PluginName.
func (*Plugin) Name() kernel.PluginName { return PluginName }

// Dependencies answers none: metronome reads no other plugin's resource.
func (*Plugin) Dependencies() []kernel.PluginName { return nil }

// Init takes the clock and the tracks from config, a *Config or nil, installs
// the clock as the Clock resource, and builds each track's state. A config of
// another type, or two tracks of one event type, is a composition error, and
// panics.
func (p *Plugin) Init(configurator *kernel.Configurator, config any) error {
	metronomeConfig := kernel.PluginConfigOrDefault[Config](config)
	p.clock = metronomeConfig.Clock
	if p.clock == nil {
		p.clock = NewWallClock()
	}
	configurator.InitResource[Clock](p.clock)
	for _, declared := range metronomeConfig.Tracks {
		for _, t := range p.tracks {
			if t.eventType == declared.eventType {
				panic(fmt.Sprintf("metronome: two tracks of %v", declared.eventType))
			}
		}
		signals := make([]*kernel.Signal, max(declared.settings.MaxInFlight, 1))
		for i := range signals {
			signals[i] = kernel.NewSignal()
		}
		p.tracks = append(p.tracks, &track{
			eventType:    declared.eventType,
			settings:     declared.settings,
			nextDeadline: declared.settings.Step + declared.settings.MinInterval,
			signals:      signals,
			publish:      declared.newPublish(),
		})
	}
	return nil
}

// Start starts the default clock's epoch and one goroutine per track, each
// publishing its track's beats on it, and hands the clock its wait callback.
// On a ManualClock it gives each variable track a timer that fires only at
// the end of an Advance or a Jump.
//
// It first finds each track's reference track, every track being declared by
// then. A track interpolating against no fixed-step track is a composition
// error, and panics.
func (p *Plugin) Start(kernel *kernel.Kernel) error {
	for _, t := range p.tracks {
		if t.settings.InterpolateAgainst == nil {
			continue
		}
		for _, reference := range p.tracks {
			if reference.eventType == t.settings.InterpolateAgainst && reference.settings.Step > 0 {
				t.reference = reference
			}
		}
		if t.reference == nil {
			panic(fmt.Sprintf("metronome: track of %v interpolates against %v, not a fixed-step track", t.eventType, t.settings.InterpolateAgainst))
		}
	}
	if clock, ok := p.clock.(*WallClock); ok {
		clock.start()
	}
	p.stop = make(chan struct{})
	manual, isManual := p.clock.(*ManualClock)
	for _, t := range p.tracks {
		if isManual && t.settings.Step == 0 {
			t.timer = manual.newVariableTimer()
		} else {
			t.timer = p.clock.NewTimer()
		}
		t.timer.Reset(t.nextDeadline)
		p.mutex.Lock()
		t.running = true
		p.mutex.Unlock()
		p.trackGroup.Add(1)
		go t.run(p, kernel)
	}
	p.clock.SetWaitCallback(p.wait)
	return nil
}

// Stop takes the wait callback back from the clock, then signals every track
// goroutine and joins them, so no beat is published after it returns. It does
// not wait on beats in flight, which the kernel finishes.
func (p *Plugin) Stop(kernel *kernel.Kernel) error {
	p.clock.SetWaitCallback(nil)
	close(p.stop)
	p.trackGroup.Wait()
	return nil
}

// Destroy does nothing.
func (*Plugin) Destroy() error { return nil }

// wait is the clock's wait callback: it returns once every track has settled,
// none with a beat in flight nor owing one, or with ctx's error if ctx ends
// first. A track not running counts as settled.
func (p *Plugin) wait(ctx context.Context) error {
	// A context that is never done needs no waking, and Advance waits on one
	// every step: skipping AfterFunc keeps that wait from allocating.
	if ctx.Done() != nil {
		stopWaking := context.AfterFunc(ctx, func() {
			p.mutex.Lock()
			defer p.mutex.Unlock()
			p.settleChanged.Broadcast()
		})
		defer stopWaking()
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	for !p.settled() {
		if err := ctx.Err(); err != nil {
			return err
		}
		p.settleChanged.Wait()
	}
	return nil
}

// settled answers whether every track has settled. The caller holds the
// mutex.
func (p *Plugin) settled() bool {
	for _, t := range p.tracks {
		if t.running && (t.inFlightCount > 0 || t.owes(p.clock.Now())) {
			return false
		}
	}
	return true
}
