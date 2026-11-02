package kernel

// PluginName names a plugin within a kernel, and keys its part of the
// kernel's config.
type PluginName string

// Plugin is a statically linked unit of functionality installed into a kernel.
// What it offers other plugins is a resource: a struct, or an interface
// another plugin fills.
//
// Init and Destroy are a pair, as Start and Stop are: whatever Init acquires,
// such as an implementation it installs with InitResource, Destroy releases.
type Plugin interface {
	// Name returns the plugin's name, unique among the plugins of one kernel.
	Name() PluginName

	// Init declares the plugin's resources, tasks and handlers. It runs once,
	// in the order the plugins were given to Spawn, with the plugin's part of
	// the config Spawn was given: the value under its Name, or nil if there is
	// none. A plugin that needs another's resource is given after it. An Init
	// that fails destroys the plugins already initialized, in reverse, and the
	// spawn fails.
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
