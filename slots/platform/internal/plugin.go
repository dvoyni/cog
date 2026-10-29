// Package internal is the platform plugin: New, the handlers behind platform's
// commands and the subscription that pumps and uploads. Composition roots and
// tests reach New through platformplugin; everything else reaches platform
// through its root.
//
// The plugin requires exactly one Backend Adapter. A composition with none
// fails with kernel.ErrMissingAdapter.
package internal

import (
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// pumpOnUpdate is the subscription that pumps the Backend once a frame and
// uploads when something is pending and the interval has passed.
type pumpOnUpdate kernel.Subscription[app.UpdateEvent]

// plugin registers platform's state, its commands and its pump.
type plugin struct {
	// backend is the bound Backend Adapter, valid from Start onwards. Every
	// call into it is made under the write lock on state.
	backend kernel.RequiredAdapter[Backend]
	// storeInterval is the resolved Config.StoreInterval, fixed at Register.
	storeInterval time.Duration
}

// New creates the platform plugin. Its platform.Config arrives through
// kernel.New's config map under platform.Name.
func New() kernel.Plugin { return &plugin{} }

// Name reports the plugin name.
func (p *plugin) Name() kernel.PluginName { return Name }

// Dependencies reports the plugins platform requires; it has none. The Adapter
// it requires binds at composition and adds no dependency, and subscribing to
// app.UpdateEvent needs none either.
func (p *plugin) Dependencies() []kernel.PluginName { return nil }

// Register requires the Backend Adapter and registers platform's state,
// commands and pump.
func (p *plugin) Register(registrar *kernel.Registrar, config any) error {
	var cfg Config
	if config != nil {
		var ok bool
		cfg, ok = config.(Config)
		if !ok {
			return ErrInvalidConfig{Got: config}
		}
	}
	p.storeInterval = cfg.StoreInterval
	if p.storeInterval <= 0 {
		p.storeInterval = DefaultStoreInterval
	}
	p.backend = registrar.RequireAdapter[BackendPort]()
	registrar.InitResource(&state{})
	p.registerCommands(registrar)
	registrar.Subscribe[pumpOnUpdate](p.pumpOnUpdate)
	return nil
}

// Stop uploads what is pending and closes the Backend. Plugins stop in reverse
// dependency order, so every game plugin that depends on platform has sent its
// last unlock by now.
func (p *plugin) Stop(k kernel.Executioner) error {
	k.ExecuteCommand[closeCmd](closeRequest{})
	return nil
}

// pumpOnUpdate runs on the last tick of a frame only: the Backend is pumped
// once a frame, not once per catch-up step.
func (p *plugin) pumpOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var current kernel.Write[*state]
	return func(access kernel.ResourceAccess) {
			current = access.GetWrite[*state]()
		}, func(k kernel.Kernel, event app.UpdateEvent) {
			if !event.Last {
				return
			}
			s := current.Get()
			if s.closed {
				return
			}
			backend := p.backend.Get()
			if err := backend.Update(); err != nil {
				k.ReportError(err)
			}
			if s.dirty && time.Since(s.lastStore) >= p.storeInterval {
				s.store(k, backend)
			}
		}
}
