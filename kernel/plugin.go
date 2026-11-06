package kernel

import (
	"fmt"
	"reflect"
)

// PluginName names a plugin within a kernel, and keys its part of the
// kernel's config.
type PluginName string

// Plugin is a statically linked unit of functionality installed into a kernel.
// What it offers other plugins' jobs is a resource; what it offers another
// plugin is an implementation bound to the socket that plugin declares. A
// plugin is never a resource itself.
//
// Init and Destroy are a pair, as Start and Stop are: whatever Init acquires,
// such as a file it opens, Destroy releases.
type Plugin interface {
	// Name returns the plugin's name, unique among the plugins of one kernel.
	Name() PluginName

	// Dependencies names the plugins whose resources this plugin's Init needs;
	// nil is none. Each must be given to Spawn before it: Spawn checks this
	// before any Init and never reorders the plugins, and a dependency given
	// after it or not at all, the plugin itself, or a name given twice is a
	// composition error, and panics naming both plugins. A socket the plugin
	// binds or declares is no dependency: bindings resolve after every Init.
	Dependencies() []PluginName

	// Init declares the plugin's resources, tasks and handlers. It runs once,
	// in the order the plugins were given to Spawn, with the plugin's part of
	// the config Spawn was given: the value under its Name, a pointer to the
	// plugin's Config, or nil if there is none, which PluginConfigOrDefault
	// unwraps. A plugin that needs another's resource is given after it, and
	// names it among its Dependencies. An Init that fails destroys the plugins
	// already initialized, in reverse, and the spawn fails.
	Init(configurator *Configurator, config any) error

	// Start runs once every plugin of the kernel has initialized, in plugin
	// order. A Start that fails stops the plugins already started, in reverse,
	// then destroys every plugin, and the spawn fails.
	Start(kernel *Kernel) error

	// Stop runs once as the kernel is destroyed, in reverse plugin order. Jobs
	// its publications schedule still run before the kernel is removed.
	Stop(kernel *Kernel) error

	// Destroy runs once, after Stop and once every job of the kernel has
	// finished, in reverse plugin order. It runs for every plugin whose Init
	// succeeded, started or not. A failure is logged through LogErr.
	Destroy() error
}

// PluginConfigOrDefault answers config, a plugin's part of the kernel's
// config as Init is given it, as the *C it must be: the pointer given, or,
// when there is none or it is a nil *C, defaultConfig, or a new zero C when
// no default is given. A plugin's Init so reads its config in one line and
// never sees nil. A config of any other type, a C itself included, or more
// than one default, is a composition error, and panics.
func PluginConfigOrDefault[C any](config any, defaultConfig ...*C) *C {
	if len(defaultConfig) > 1 {
		panic(fmt.Sprintf("kernel: PluginConfigOrDefault takes at most one default, given %d", len(defaultConfig)))
	}
	given, ok := config.(*C)
	if config != nil && !ok {
		panic(fmt.Sprintf("kernel: config is a %T, want a %v", config, reflect.TypeFor[*C]()))
	}
	if given != nil {
		return given
	}
	if len(defaultConfig) == 1 && defaultConfig[0] != nil {
		return defaultConfig[0]
	}
	return new(C)
}
