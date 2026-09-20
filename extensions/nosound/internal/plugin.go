package internal

import (
	"github.com/dvoyni/cog/extensions/nosound"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/sound"
)

// defaultSampleRate is the rate nosound's Device reports when its Config names
// none. It lives here rather than in the root because an Extension's root
// declares only Name, Config, its Adapters and its errors.
const defaultSampleRate = 48000

// plugin is nosound: one Adapter and nothing else. It registers no resource, no
// command and no subscription, because a sink that keeps nothing has no state
// for the engine to guard.
type plugin struct{}

// New creates the nosound plugin. Its nosound.Config arrives through
// kernel.New's config map under nosound.Name.
func New() kernel.Plugin { return &plugin{} }

// Name reports the plugin name.
func (p *plugin) Name() kernel.PluginName { return nosound.Name }

// Dependencies reports the plugins nosound requires; it has none. The Port it
// fills binds at composition and adds no dependency in either direction.
func (p *plugin) Dependencies() []kernel.PluginName { return nil }

// Register provides the silent Backend. It never fails on a Device: there is
// none to open, which is the limit case of the rule that registration succeeds
// whether or not a Device exists.
func (p *plugin) Register(registrar *kernel.Registrar, config any) error {
	cfg := nosound.Config{}
	if config != nil {
		var ok bool
		cfg, ok = config.(nosound.Config)
		if !ok {
			return nosound.ErrInvalidConfig{Got: config}
		}
	}
	sampleRate := cfg.SampleRate
	if sampleRate == 0 {
		sampleRate = defaultSampleRate
	}
	registrar.ProvideAdapter[nosound.SoundBackend](sound.Backend(newBackend(sampleRate)))
	return nil
}
