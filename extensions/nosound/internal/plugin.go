package internal

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/sound"
)

// defaultSampleRate is the rate nosound's Device reports when its Config names
// none. It is unexported rather than aliased by the root because an Extension's
// root offers only Name, Config, its Adapters and its errors.
const defaultSampleRate = 48000

// reportOnUpdate is the subscription the Adapter's one report goes out through.
// A Backend has no Kernel of its own - sound calls it, and hands it nothing -
// so what it cannot say it hands back to something that can.
type reportOnUpdate kernel.Subscription[app.UpdateEvent]

// plugin is nosound: one Adapter, and one subscription that says the single
// thing a silent Adapter still has to say out loud. It registers no resource
// and no command, because a sink that keeps nothing has no state for the engine
// to guard.
type plugin struct{ backend *backend }

// New creates the nosound plugin. Its nosound.Config arrives through
// kernel.New's config map under nosound.Name.
func New() kernel.Plugin { return &plugin{} }

// Name reports the plugin name.
func (p *plugin) Name() kernel.PluginName { return Name }

// Dependencies reports the plugins nosound requires; it has none. The Port it
// fills binds at composition and adds no dependency in either direction, and
// subscribing to app.UpdateEvent needs none either: an event nobody publishes
// is simply never delivered.
func (p *plugin) Dependencies() []kernel.PluginName { return nil }

// Register provides the silent Backend. It never fails on a Device: there is
// none to open, which is the limit case of the rule that registration succeeds
// whether or not a Device exists.
func (p *plugin) Register(registrar *kernel.Registrar, config any) error {
	cfg := Config{}
	if config != nil {
		var ok bool
		cfg, ok = config.(Config)
		if !ok {
			return ErrInvalidConfig{Got: config}
		}
	}
	sampleRate := cfg.SampleRate
	if sampleRate == 0 {
		sampleRate = defaultSampleRate
	}
	p.backend = newBackend(sampleRate)
	registrar.ProvideAdapter[SoundBackend](sound.Backend(p.backend))
	registrar.Subscribe[reportOnUpdate](p.reportOnUpdate)
	return nil
}

// reportOnUpdate says the one thing nosound's Adapter has to say out loud, once
// per Clip: a Loop Region a file declared and could not have was dropped whole,
// and that Clip loops whole instead.
//
// It is reported rather than reported-once because the queue it drains is the
// dedupe - a dropped region names a Clip, and an Adapter has no name for one.
//
// It locks nothing of the engine's: what it reads is a queue guarded by the
// Adapter's own lock.
func (p *plugin) reportOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	return func(kernel.ResourceAccess) {}, func(k kernel.Kernel, _ app.UpdateEvent) {
		for _, dropped := range p.backend.takeDroppedRegions() {
			k.ReportError(dropped)
		}
	}
}
