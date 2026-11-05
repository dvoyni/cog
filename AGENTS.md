# cog

Before any work, read and follow:

- submodules/rules/HOW-TO-USE-AND-EXTEND-RULES.md
- submodules/rules/GENERAL-RULES.md
- submodules/rules/GO-RULES.md

Each module has its own `AGENTS.md` for rules that hold only there.

## Plugins

- A plugin's constructor takes no parameters: `NewPlugin()`. What it can be configured with is a plain `Config` struct of values in its package, given as its part of the kernel's config; `Init` asserts a non-nil part to `Config`, and nil means the defaults. No functional options. (kernel/docs/design.md, *Plugins*)
- A plugin is never a resource: no one may reach its lifecycle methods. What it offers others is an interface it implements, bound with `configurator.BindSocket[I](p)` to the socket the plugin that needs it declares; such small interfaces live in the declaring plugin package's `sockets.go`, beside nothing else; its resources are in `resources.go`. (kernel/docs/design.md, *Plugins*)
- The tasks a plugin offers others to schedule are the fields of one `Tasks` resource in its package, the first type in its `resources.go`: `platform.Tasks{ProcessEvents}`, read with `configurator.Resource[platform.Tasks]()`.

## Upstream forks

- A change in a forked upstream repository (gogpu, naga, wgpu and the rest under `submodules/`) leaves its `CHANGELOG.md` alone: the maintainers write it when they merge the PR. What a changelog entry would say, such as a behaviour change, goes in the PR description.

## Potential rules

- A task reaches a resource through `kernel.Read[T]` and `kernel.Write[T]`; one thing is done one way. A custom `kernel.Parameter` type is for the rare case it improves much, as ecs's queries do. (Taught when platform's `Files`/`WritableFiles` and `Values`/`WritableValues` parameters were replaced by plain resources.)
- A resource whose writes must not happen under a read lock is an interface of the reads alone (`platform.FS`, an alias of the standard `fs.FS`, for files), its value a private type that cannot be asserted into writing; a function taking `kernel.Write[T]` answers the writing interface, which is no resource: `platform.ElevateFileAccess(kernel.Write[platform.FS]) platform.WriteFS`. The function sits in `resources.go` between the pair it joins, after the reading interface and before the writing one. Nothing that writes it is a resource either. (Taught when `*platform.Files` let a job holding its read lock write.)
