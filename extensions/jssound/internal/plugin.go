//go:build js

package internal

import (
	"github.com/dvoyni/cog/extensions/jssound"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/sound"
)

// reportOnUpdate is the subscription the Adapter's reports go out through. A
// Backend has no Kernel of its own - sound calls it, and hands it nothing - so
// what it cannot say it hands back to something that can.
type reportOnUpdate kernel.Subscription[app.UpdateEvent]

// deviceUnavailableKey names the condition "this page has no Web Audio at all",
// which is true from registration onwards and is worth saying once. It is its
// own type so that it shares a namespace with nothing.
type deviceUnavailableKey struct{}

// plugin is jssound: one Adapter, one subscription that says the two things the
// browser side cannot say for itself, and a Stop that leaves no listener on the
// page.
type plugin struct{ backend *backend }

// New creates the jssound plugin. Its jssound.Config arrives through kernel.New's
// config map under jssound.Name.
func New() kernel.Plugin { return &plugin{} }

// Name reports the plugin name.
func (p *plugin) Name() kernel.PluginName { return jssound.Name }

// Dependencies reports the plugins jssound requires; it has none. The Port it
// fills binds at composition and adds no dependency in either direction, and
// subscribing to app.UpdateEvent needs none either: an event nobody publishes is
// simply never delivered.
func (p *plugin) Dependencies() []kernel.PluginName { return nil }

// Register provides the Backend. It never fails on a Device: a page with no Web
// Audio registers successfully and is reported once afterwards, and a context
// the browser has not let resume yet is not a failure at all - it is the
// ordinary first state of every web game.
func (p *plugin) Register(registrar *kernel.Registrar, config any) error {
	cfg := jssound.Config{}
	if config != nil {
		var ok bool
		cfg, ok = config.(jssound.Config)
		if !ok {
			return jssound.ErrInvalidConfig{Got: config}
		}
	}
	if cfg.LatencyHint < 0 {
		return jssound.ErrInvalidLatencyHint{LatencyHint: cfg.LatencyHint}
	}
	if cfg.DecodedClipLimit < alwaysStream {
		return jssound.ErrInvalidDecodedClipLimit{DecodedClipLimit: cfg.DecodedClipLimit}
	}
	p.backend = newBackend(cfg)
	registrar.ProvideAdapter[jssound.SoundBackend](sound.Backend(p.backend))
	registrar.Subscribe[reportOnUpdate](p.reportOnUpdate)
	return nil
}

// Stop closes the context and takes the gesture listener off the page, so an
// Engine that has shut down leaves nothing behind in a document that may outlive
// it - a single-page app swapping one game for another is the case.
func (p *plugin) Stop(kernel.Executioner) error {
	if p.backend != nil {
		p.backend.stop()
	}
	return nil
}

// reportOnUpdate says the things the browser side has to say out loud, each
// once. A page with no Web Audio is reported once and the Adapter then behaves
// as nosound does; a Clip whose Loop Region did not fit it is reported once and
// plays without one.
//
// A context that has not resumed is reported never, which is the same rule as
// otosound's lost Device one platform over: Ready being false is the whole of
// that notification, and on web not-ready is the normal case for as long as the
// player has not clicked.
//
// It locks nothing of the engine's. What it reads is a queue guarded by the
// Adapter's own lock, and a field written before any of this could run.
func (p *plugin) reportOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	return func(kernel.ResourceAccess) {}, func(k kernel.Kernel, _ app.UpdateEvent) {
		if p.backend.failure != nil {
			k.ReportErrorOnce(deviceUnavailableKey{}, jssound.ErrDeviceUnavailable{Err: p.backend.failure})
		}
		// Reported rather than reported-once: the queue is the dedupe, because
		// a dropped region names a Clip and the Adapter has no name for one.
		for _, dropped := range p.backend.takeDroppedRegions() {
			k.ReportError(dropped)
		}
	}
}
