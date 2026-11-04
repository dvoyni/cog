package metronome

import (
	"context"
	"errors"
	"sync"

	"github.com/dvoyni/cog/kernel"
)

// PluginName is metronome's plugin name.
const PluginName kernel.PluginName = "metronome"

// Plugin is metronome: it runs one goroutine per declared track between Start
// and Stop, each publishing its track's beats. It is listed before the plugins
// that declare tracks, so it stops last.
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

// Option configures a Plugin as NewPlugin builds it.
type Option func(plugin *Plugin)

// NewPlugin returns metronome, configured by options.
func NewPlugin(options ...Option) *Plugin {
	plugin := &Plugin{}
	plugin.settleChanged.L = &plugin.mutex
	for _, option := range options {
		option(plugin)
	}
	return plugin
}

// WithClock makes metronome read time from clock and wait on its timers.
func WithClock(clock Clock) Option {
	return func(plugin *Plugin) { plugin.clock = clock }
}

// Name returns PluginName.
func (*Plugin) Name() kernel.PluginName { return PluginName }

// Init installs the plugin as a resource, for AddTrack to find. It fails if no
// clock was given.
func (p *Plugin) Init(configurator *kernel.Configurator, config any) error {
	if p.clock == nil {
		return errors.New("metronome: no clock given; pass one with WithClock")
	}
	configurator.InitResource(p)
	return nil
}

// Start starts one goroutine per track, each publishing its track's beats on
// it. On a ManualClock it links the plugin to the clock, so Advance waits for
// its tracks to settle, and gives each variable track a timer that fires only
// at the end of an Advance or a Jump.
func (p *Plugin) Start(kernel *kernel.Kernel) error {
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
	if isManual {
		manual.attach(p)
	}
	return nil
}

// Stop signals every track goroutine and joins them, so no beat is published
// after it returns, and unlinks the plugin from a ManualClock. It does not
// wait on beats in flight, which the kernel finishes.
func (p *Plugin) Stop(kernel *kernel.Kernel) error {
	if manual, ok := p.clock.(*ManualClock); ok {
		manual.detach(p)
	}
	close(p.stop)
	p.trackGroup.Wait()
	return nil
}

// Destroy does nothing.
func (*Plugin) Destroy() error { return nil }

// Wait returns once every track has settled: none has a beat in flight, and
// none owes a beat, having reached it without publishing it. It answers
// ctx.Err() if ctx ends first. A track not running counts as settled.
func (p *Plugin) Wait(ctx context.Context) error {
	stopWaking := context.AfterFunc(ctx, func() {
		p.mutex.Lock()
		defer p.mutex.Unlock()
		p.settleChanged.Broadcast()
	})
	defer stopWaking()
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
	now := p.clock.Now()
	for _, t := range p.tracks {
		if t.running && (t.inFlightCount > 0 || t.owes(now)) {
			return false
		}
	}
	return true
}
