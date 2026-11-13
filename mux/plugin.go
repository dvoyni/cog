package mux

import (
	"reflect"

	"github.com/dvoyni/cog/kernel"
)

// PluginName is mux's plugin name.
const PluginName kernel.PluginName = "mux"

// Plugin is mux. It is built empty, as every plugin is: models are registered
// by the plugins that use them, listed after it.
type Plugin struct {
	// registry is the registry Init installed, kept for Start to seal.
	registry *registry
}

// NewPlugin returns mux.
func NewPlugin() *Plugin { return &Plugin{} }

// Name returns PluginName.
func (*Plugin) Name() kernel.PluginName { return PluginName }

// Dependencies answers none: mux reads no other plugin's resource.
func (*Plugin) Dependencies() []kernel.PluginName { return nil }

// Init installs the registry, which every registration finds.
func (p *Plugin) Init(configurator *kernel.Configurator, config any) error {
	p.registry = &registry{models: map[reflect.Type]*modelRegistration{}}
	configurator.InitResource(p.registry)
	return nil
}

// Start seals the registry: every plugin has initialized, so a registration
// from here on panics.
func (p *Plugin) Start(kernel *kernel.Kernel) error {
	p.registry.sealed = true
	return nil
}

// Stop does nothing: mux subscribes to nothing and publishes nothing.
func (*Plugin) Stop(kernel *kernel.Kernel) error { return nil }

// Destroy does nothing.
func (*Plugin) Destroy() error { return nil }
