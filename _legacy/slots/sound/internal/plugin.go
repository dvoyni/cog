package internal

import (
	"github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/kernel"

	"github.com/dvoyni/cog/slots/storage"
)

// plugin is sound: the queue, the clip table, the live Voice view, the Bus
// volumes, the Device, and the flush that runs once a tick.
type plugin struct {
	// backend is the bound Backend Adapter, valid from Start onwards.
	backend kernel.RequiredAdapter[Backend]
	// maxVoices is the resolved cap, kept so Start can tell the Adapter how
	// many slots exist before any Emit.
	maxVoices int
}

// New creates the sound plugin. Its sound.Config arrives through kernel.New's
// config map under sound.Name.
func New() kernel.Plugin { return &plugin{} }

// Name reports the plugin name.
func (p *plugin) Name() kernel.PluginName { return Name }

// Dependencies reports the plugins sound requires: storage, whose FileSystem
// the flush reads a Clip's bytes through. app is not among them - subscribing
// to app.UpdateEvent needs no dependency, because an event nobody publishes is
// simply never delivered.
func (p *plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{storage.Name}
}

// Register requires the Backend Adapter, resolves the configuration, registers
// the six resources, the per-tick batch and what the last flush left for the
// agent-facing listing, subscribes the flush and the response to an engine
// Pause, and contributes sound's mcp Provider.
func (p *plugin) Register(registrar *kernel.Registrar, config any) error {
	p.backend = registrar.RequireAdapter[BackendPort]()

	var cfg Config
	if config != nil {
		var ok bool
		cfg, ok = config.(Config)
		if !ok {
			return ErrInvalidConfig{Got: config}
		}
	}
	if cfg.MaxVoices < 0 {
		return ErrInvalidMaxVoices{MaxVoices: cfg.MaxVoices}
	}
	p.maxVoices = cfg.MaxVoices
	if p.maxVoices == 0 {
		p.maxVoices = DefaultMaxVoices
	}

	registrar.InitResource(NewQueue(p.maxVoices))
	registrar.InitResource(NewVoices(p.maxVoices))
	registrar.InitResource(NewClips())
	registrar.InitResource(NewBuses())
	registrar.InitResource(NewListener())
	registrar.InitResource(&Device{})
	registrar.InitResource(&flushScratch{})
	registrar.InitResource(&lastFlush{})
	registrar.HandleCommand[voicesCmd](voicesCmdImpl)
	registrar.Subscribe[FlushOnUpdate](p.flushOnUpdate).Last()
	registrar.Subscribe[SuspendOnPauseChange](p.suspendOnPauseChange)
	registrar.ProvideAdapter[McpProvider](mcp.Provider(provider{}))
	return nil
}

// Start tells the Adapter how many slots exist. It runs once, after
// registration and before any tick, which is what the Backend contract asks
// for: Voices(n) before any Emit.
func (p *plugin) Start(kernel.Executioner) error {
	p.backend.Get().Voices(p.maxVoices)
	return nil
}
