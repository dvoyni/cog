# Kernel rules

## Plugins

- A plugin's part of the kernel's config is always a pointer to its `Config`: `metronome.PluginName: &metronome.Config{...}`. `Init` reads it with `kernel.PluginConfigOrDefault(config, &Config{...})`, or `kernel.PluginConfigOrDefault[Config](config)` for a zero default, and so never sees nil; any other type, a `Config` by value included, panics. A plugin never writes its config: a default it fills in goes in a variable of its own, as platform's `valuesFile`, not into the caller's `Config`.
- A plugin's README has a **Kernel surface** section listing what it shares through the kernel, each with what it is for: the resources it installs and those it reads from other plugins, the events it publishes and those it subscribes to, and the sockets it declares and those it binds. A kind it has none of says none, so the list is the whole surface: what to list a plugin before or after, and what it needs bound.
- A plugin that subscribes to events installs a `Handlers` resource holding the `kernel.Sub` of each subscription, one field per handler named for what it does, so a plugin listed after it orders its own handlers around them with `OrderSubscriptions`: `platform.Handlers{FlushValues}`. It is the type after `Tasks` in `resources.go`. A plugin that subscribes to nothing has none.

## Code

- A composition error (registering something invalid, a bad `Spawn` argument) panics. A runtime failure goes to `LogErr` and the kernel carries on.
- Task args, task results and events are pointer-free: no pointers, slices, maps, strings, interfaces, channels or funcs at any depth.
- A type a socket is declared of, interface or value, is named with the suffix `Socket`: `platform.WriteFSSocket`, `platform.ReadMountSocket`. A type merely used by one, such as `fs.FS` in a `ReadMountSocket`, is not.
- `configurator_gen.go` is written by `internal/generate`: change the generator and run `go generate`, never the output.

## Tests

- Tests reach a kernel the way plugins do: spawn it through the `Supervisor` (`spawnTestKernel`, `spawnHooked`) and register in a plugin's `Init`. Internals are read to assert, never called to stand in for a lifecycle step.
- An internal module with an interface of its own, such as `lockState` or `jobGraph`, is tested at that interface, without a kernel.

## Potential rules
