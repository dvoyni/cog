//go:build !js

package internal

import (
	"github.com/dvoyni/cog/extensions/otosound"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/sound"
)

// reportOnUpdate is the subscription the Adapter's two reports go out through.
// A device-side object has no Kernel of its own, so what it cannot say it hands
// back to something that can - gfx's takeRefusal, one Slot over - and this is
// the handler that holds one.
type reportOnUpdate kernel.Subscription[app.UpdateEvent]

// deviceUnavailableKey names the condition "no audio device could be opened at
// all", which is true from the first failed open onwards and is worth saying
// once. It is its own type so that it shares a namespace with nothing.
type deviceUnavailableKey struct{}

// deviceConfigIgnoredKey names "this Engine is the second in the process and
// the context was already open".
type deviceConfigIgnoredKey struct{}

// plugin is otosound: one Adapter, one subscription that says the two things
// the device side cannot say for itself, and a Stop that leaves no goroutine
// behind.
type plugin struct {
	// hardware is how the Adapter reaches a device. It is a field rather than a
	// call so that the suite can compose otosound without a sound card: a
	// process has one oto context to spend, and a test that spent it would
	// spend it for every test after it.
	hardware audio
	backend  *backend
}

// New creates the otosound plugin. Its otosound.Config arrives through
// kernel.New's config map under otosound.Name.
func New() kernel.Plugin { return &plugin{hardware: &otoAudio{}} }

// Name reports the plugin name.
func (p *plugin) Name() kernel.PluginName { return otosound.Name }

// Dependencies reports the plugins otosound requires; it has none. The Port it
// fills binds at composition and adds no dependency in either direction, and
// subscribing to app.UpdateEvent needs none either: an event nobody publishes
// is simply never delivered.
func (p *plugin) Dependencies() []kernel.PluginName { return nil }

// Register provides the Backend. It never fails on a Device: registration
// succeeds whether or not one exists, and nothing here opens one - the device
// is taken at Voices(n), which is the first moment the Mixer it would pull from
// is complete.
func (p *plugin) Register(registrar *kernel.Registrar, config any) error {
	cfg := otosound.Config{}
	if config != nil {
		var ok bool
		cfg, ok = config.(otosound.Config)
		if !ok {
			return otosound.ErrInvalidConfig{Got: config}
		}
	}
	if cfg.SampleRate < 0 {
		return otosound.ErrInvalidSampleRate{SampleRate: cfg.SampleRate}
	}
	if cfg.BufferSize < 0 {
		return otosound.ErrInvalidBufferSize{BufferSize: cfg.BufferSize}
	}
	if cfg.DecodedClipLimit < alwaysStream {
		return otosound.ErrInvalidDecodedClipLimit{DecodedClipLimit: cfg.DecodedClipLimit}
	}
	p.backend = newBackend(cfg, p.hardware)
	registrar.ProvideAdapter[otosound.SoundBackend](sound.Backend(p.backend))
	registrar.Subscribe[reportOnUpdate](p.reportOnUpdate)
	return nil
}

// Stop closes the device down and waits for the goroutine that watches it, so
// an engine that has shut down leaves nothing running behind it.
func (p *plugin) Stop(kernel.Executioner) error {
	if p.backend != nil {
		p.backend.stop()
	}
	return nil
}

// reportOnUpdate says the things the device side has to say out loud, each
// once. A Device that could never be opened is reported once and the Adapter
// then behaves as nosound does; a second Engine whose Config could not be
// honoured is reported once and stays audible; a Clip whose Loop Region did not
// fit it is reported once and plays without one. A Device that was open and was
// then lost is reported never.
//
// It locks nothing of the engine's. Two of the three are atomics an off-tick
// goroutine wrote, and the third is a queue drained under the Adapter's own
// lock - the one that is nowhere the Mixer can reach.
func (p *plugin) reportOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	return func(kernel.ResourceAccess) {}, func(k kernel.Kernel, _ app.UpdateEvent) {
		if failure := p.backend.failure.Load(); failure != nil {
			k.ReportErrorOnce(deviceUnavailableKey{}, otosound.ErrDeviceUnavailable{Err: failure.err})
		}
		if ignored := p.backend.ignored.Load(); ignored != nil {
			k.ReportErrorOnce(deviceConfigIgnoredKey{}, *ignored)
		}
		// Reported rather than reported-once: the queue is the dedupe, because
		// a dropped region names a Clip and the Adapter has no name for one.
		for _, dropped := range p.backend.takeDroppedRegions() {
			k.ReportError(dropped)
		}
	}
}
