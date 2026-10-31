// Package internal is the noplatform plugin: New, and the empty Backend it
// provides for platform's BackendPort. Composition roots and tests reach New
// through noplatformplugin; everything else reaches noplatform through its root.
package internal

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/platform"
)

// plugin is noplatform: one Adapter and nothing else.
type plugin struct{}

// New creates the noplatform plugin.
func New() kernel.Plugin { return plugin{} }

// Name reports the plugin name.
func (plugin) Name() kernel.PluginName { return Name }

// Dependencies reports the plugins noplatform requires; it has none. The Port
// it fills binds at composition and adds no dependency.
func (plugin) Dependencies() []kernel.PluginName { return nil }

// Register provides the empty Backend. It takes no Config.
func (plugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[PlatformBackend](platform.Backend(backend{}))
	return nil
}
