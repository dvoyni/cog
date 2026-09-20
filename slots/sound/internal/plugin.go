package internal

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/sound"
	"github.com/dvoyni/cog/slots/sound/internal/types"
	"github.com/dvoyni/cog/slots/storage"
)

// plugin is sound: the queue, the clip table, the live Voice view, the Bus
// volumes, the Device, and the flush that runs once a tick.
type plugin struct {
	// backend is the bound Backend Adapter, valid from Start onwards.
	backend kernel.RequiredAdapter[sound.Backend]
	// maxVoices is the resolved cap, kept so Start can tell the Adapter how
	// many slots exist before any Emit.
	maxVoices int
}

// New creates the sound plugin. Its sound.Config arrives through kernel.New's
// config map under sound.Name.
func New() kernel.Plugin { return &plugin{} }

// Name reports the plugin name.
func (p *plugin) Name() kernel.PluginName { return sound.Name }

// Dependencies reports the plugins sound requires: storage, whose FileSystem
// the flush reads a Clip's bytes through. app is not among them - subscribing
// to app.UpdateEvent needs no dependency, because an event nobody publishes is
// simply never delivered.
func (p *plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{storage.Name}
}

// Register requires the Backend Adapter, resolves the configuration, registers
// the five resources and the per-tick batch, and subscribes the flush.
func (p *plugin) Register(registrar *kernel.Registrar, config any) error {
	p.backend = registrar.RequireAdapter[sound.BackendPort]()

	var cfg sound.Config
	if config != nil {
		var ok bool
		cfg, ok = config.(sound.Config)
		if !ok {
			return sound.ErrInvalidConfig{Got: config}
		}
	}
	if cfg.MaxVoices < 0 {
		return sound.ErrInvalidMaxVoices{MaxVoices: cfg.MaxVoices}
	}
	p.maxVoices = cfg.MaxVoices
	if p.maxVoices == 0 {
		p.maxVoices = sound.DefaultMaxVoices
	}

	registrar.InitResource(types.NewQueue(p.maxVoices))
	registrar.InitResource(types.NewVoices(p.maxVoices))
	registrar.InitResource(types.NewClips())
	registrar.InitResource(types.NewBuses())
	registrar.InitResource(&sound.Device{})
	registrar.InitResource(&flushScratch{})
	registrar.Subscribe[sound.FlushOnUpdate](p.flushOnUpdate).Last()
	return nil
}

// Start tells the Adapter how many slots exist. It runs once, after
// registration and before any tick, which is what the Backend contract asks
// for: Voices(n) before any Emit.
func (p *plugin) Start(kernel.Executioner) error {
	p.backend.Get().Voices(p.maxVoices)
	return nil
}
