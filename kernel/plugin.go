package kernel

// Plugin is a statically linked unit of functionality installed into a kernel.
// What it offers other plugins is a resource: a struct, or an interface
// another plugin fills.
type Plugin interface {
	// Register declares the plugin's resources, tasks and handlers. It runs
	// once, in the order the plugins were given to Spawn, with the config Spawn
	// was given. A plugin that needs another's resource is given after it.
	Register(configurator *Configurator, config any) error

	// Start runs once every plugin of the kernel has registered, in plugin
	// order. A Start that fails stops the plugins already started, in reverse,
	// and the spawn fails.
	Start(kernel *Kernel) error

	// Stop runs once as the kernel is destroyed, in reverse plugin order. Jobs
	// its publications schedule still run before the kernel is removed.
	Stop(kernel *Kernel) error
}
