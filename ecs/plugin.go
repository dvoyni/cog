package ecs

import (
	"reflect"

	"github.com/dvoyni/cog/kernel"
)

// PluginName is the ECS plugin's name.
const PluginName kernel.PluginName = "ecs"

// Plugin is the ECS. It is built empty, as every plugin is: worlds are
// registered by the plugins that use them, given after it.
type Plugin struct{}

// registry is the ECS's kernel resource listing the worlds registered.
type registry struct {
	worlds []reflect.Type
}

// NewPlugin returns the ECS plugin.
func NewPlugin() *Plugin { return &Plugin{} }

// Name returns PluginName.
func (*Plugin) Name() kernel.PluginName { return PluginName }

// Init installs the registry of worlds.
func (*Plugin) Init(configurator *kernel.Configurator, config any) error {
	configurator.InitResource(&registry{})
	return nil
}

// Start does nothing: the ECS subscribes to nothing and publishes nothing.
func (*Plugin) Start(kernel *kernel.Kernel) error { return nil }

// Stop does nothing.
func (*Plugin) Stop(kernel *kernel.Kernel) error { return nil }

// Destroy does nothing.
func (*Plugin) Destroy() error { return nil }
