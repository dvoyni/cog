# cog

Before any work, read and follow submodules/rules/AGENTS.md.

Each module has its own `AGENTS.md` for rules that hold only there.

## Plugins

- A plugin's constructor takes no parameters: `NewPlugin()`. What it can be configured with is a plain `Config` struct of values in its package, given as a pointer, its part of the kernel's config, as `kernel/AGENTS.md` says. No functional options. (kernel/docs/design.md, *Plugins*)
- A plugin is never a resource: no one may reach its lifecycle methods. What it offers others is an interface it implements, bound with `configurator.BindSocket[I](p)` to the socket the plugin that needs it declares; such small interfaces live in the declaring plugin package's `sockets.go`, beside nothing else; its resources are in `resources.go`. (kernel/docs/design.md, *Plugins*)
- The tasks a plugin offers others to schedule are the fields of one `Tasks` resource in its package, the first type in its `resources.go`: `platform.Tasks{ProcessEvents}`, read with `configurator.Resource[platform.Tasks]()`.
- A resource is never kept outside the function that reached it: not in a plugin's field, another resource or a closure. A plugin that needs what a resource offers between calls makes its own values from it in `Init`, as gfx makes its stopwatches from `metronome.Clock`, and keeps those, never the resource.

## Upstream forks

- A change in a forked upstream repository (gogpu, naga, wgpu and the rest under `submodules/`) that changes its public API adds an entry to its `CHANGELOG.md`, under `[Unreleased]`, in the form the file already uses. A bug fix or an improvement that leaves the public API as it is adds none; its PR describes it.
- A fork change built on its own branch elsewhere, such as a scratchpad worktree, is also brought onto the local `submodules/<fork>` checkout, uncommitted, before it is handed over: the owner reads its diffs there before /complete. /complete stashes that copy before merging the branch into the fork's main.
- Under an umbrella, the next child taken is the first open, unblocked ticket of cog's own (gfx, platform, a plugin) before any upstream fork's: a fork ticket is taken only when none of ours is unblocked, so a fork change that unblocks one of ours is followed by ours.

## APIs

- A rule of an API that its user could trip over is documented on the calls it governs and in the design, not only decided: a render material changed by one task while another records draws of it may not show in those draws, written on the material's setters and in render's design.

## Potential rules

- [1] A filesystem, or any other layer a plugin's socket takes, is an adapter, not a plugin: a constructor in a subpackage of the declaring plugin that the application's own plugin calls and binds, so an application mixes them freely. `platform/filesystems.NewDiskWriteFS(directory)` is bound beside an `embed.FS` read mount by the game's plugin, not a `disk` plugin binding every layer from its `Config`. (Taught when the disk filesystems were first a `disk` plugin.)
- [1] A concept with its own term, setters and seam call is not a field of another value: it sits beside it, set by its own methods, and is read back only where something needs it. The cursor request is kept beside the window entry, set with `SetCursorShape`, `SetCursorHidden` and `SetCursorMode`, not `WindowEntry.Cursor` read back through `Entry`. (Taught when the cursor request was first a field of `WindowEntry`.)
- [1] A task's body is a named function over its handles, registered as `RegisterTask2(processEvents)`; state it keeps between runs is a resource, private when no one else may lock it, not a variable its closure captures. (Taught when platform's pump and values flush were closures over a `pump` value.)
- [2] A task reaches a resource through `kernel.Read[T]` and `kernel.Write[T]`; one thing is done one way. A custom `kernel.Parameter` type is for the rare case it improves much, as ecs's queries do. (Taught when platform's `Files`/`WritableFiles` and `Values`/`WritableValues` parameters were replaced by plain resources.)
- [1] A resource whose writes must not happen under a read lock is an interface of the reads alone (`platform.FS`, an alias of the standard `fs.FS`, for files), its value a private type that cannot be asserted into writing; a function taking `kernel.Write[T]` answers the writing interface, which is no resource: `platform.ElevateFileAccess(kernel.Write[platform.FS]) platform.WriteFS`. The function sits in `resources.go` between the pair it joins, after the reading interface and before the writing one. Nothing that writes it is a resource either. (Taught when `*platform.Files` let a job holding its read lock write.)
- [1] Which thread an implementation's calls must run on is the implementation's own, checked by platform where it differs: the seam's plugin pins nothing for it. gfx's surface, acquire and present jobs are not pinned for wgpu; gogpu's plugin, listed after gfx, pins the ones its OS requires through `gfx.Tasks`. (Taught when gfx pinned every surface, acquire and present task to the main thread for macOS's sake.)
- [1] Before a design accepts that something cannot work, it looks at how v1 in `_legacy` achieved the same thing: v1 drew the frame being captured offscreen and then onto the window, so its screenshots worked on DX12, where copying the back buffer does not. (Taught when gfx's capture was about to be left unavailable on DX12.)
- [2] A fork change does what cog needs, and a gap it uncovers that cog does not hit is not offered as more work: Windows keeping a destroyed first window as its primary is the maintainer's, not a follow-up of ours. (Taught when `Window.Destroy` closed with an offer to hand Windows' primary role on.)
- [1] A plugin's own type implements `kernel.Plugin` alone, and holds the state its sockets share. Each socket it binds is implemented by a private type of its own, in its own file, which keeps its methods and a back reference to the plugin, bound in `Init`, so no one holding the plugin reaches a socket's methods: gogpu binds `instance{plugin: p}` (`instance.go`), `windowSystem{plugin: p}` (`windowsystem.go`) and `inputSource{plugin: p}` (`inputsource.go`), not `BindSocket[I](p)`. It narrows "What it offers others is an interface it implements". (Taught when gogpu's `Plugin` carried every method of gfx's and platform's sockets, then when its sockets split the plugin's state among themselves.)
- [2] A count or size a plugin's behaviour is tuned by is a field of its `Config`, its zero value meaning a stated default, not a constant left for a later measurement to set: render's pools free what goes unused for `Config.TemporaryIdleFrames` frames, zero meaning 10. (Taught when render's pool drop delay was first left to the per-frame bar.)
