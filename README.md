# Cog Engine

Cog is a small typed plugin engine for Go. Plugins communicate through events,
commands, and locked resources; the kernel owns registration, scheduling, and
error handling.

## Why Cog

- **Safe parallelism by construction.** Ready event subscribers and independent
    tasks run concurrently whenever their resource access does not conflict.
    Handlers declare access by binding typed read and write handles; the
    scheduler acquires the resulting lock set atomically and prevents races over
    engine-managed state.
- **No lock bookkeeping to keep in sync.** The same binding that gives a handler
    access to a resource declares its lock. When one command uses another, Cog
    computes the transitive resource set at composition time, so callers stay
    decoupled from the callee's implementation details.
- **A small, highly decoupled microkernel.** Plugins depend on typed commands,
    events, and resources rather than concrete plugin implementations. Features
    can be added, removed, or replaced at the composition root without a central
    application object accumulating subsystem knowledge.
- **Declarative scenes without asset plumbing.** Build each frame from UI element
    values and canvas draw declarations that reference images and fonts by path.
    Canvas loads and caches assets lazily, packs sprites and glyphs into atlases,
    and manages their GPU resources; explicit unloading remains available when
    an application needs tighter residency control.
- **Invalid architectures fail before startup.** Cog validates plugin
    dependencies, unique ownership, required resources, command usage, and event
    ordering while composing the engine instead of discovering structural
    mistakes during play.
- **Deterministic lifecycle and event ordering.** Explicit dependencies govern
    startup and shutdown, while event subscribers can declare ordering only
    where it matters and remain parallel everywhere else.
- **Typed contracts without generated glue.** Exact Go types identify commands,
    events, and resources, preserving compile-time request and response types
    across plugin boundaries.
- **Low-allocation frame loops.** Handler factories run once during registration,
    synchronous command dispatch avoids per-call allocations, warmed UI
    processing is allocation-free, and canvas queues retain their backing
    storage between frames.
- **One operational boundary.** Context propagation, asynchronous errors,
    cancellation, and orderly shutdown converge in the kernel, and the finalized
    plugin and contract graph is available for runtime introspection.

## Packages

- [`kernel`](kernel/README.md): plugin lifecycle, typed registry, scheduler,
    resources, and errors.
- [`app`](slots/app/README.md): driver-neutral update, render, time-control, and quit
    contracts. No implementation.
- [`input`](bundles/input/README.md): input state, discrete events, the driver-facing
    apply command, and scripted input.
- [`anim`](bundles/anim/README.md): timelines of eased value tracks and one-tick cues,
    advanced every fixed step.
- [`ecs`](bundles/ecs/README.md): entities, components, the sparse-set stores they live
    in, and systems as plain funcs whose parameter types are their lock set.
- [`storage`](slots/storage/README.md): layered read filesystems and one permanent
    writable filesystem, which a platform Adapter provides.
- [`diskfs`](extensions/diskfs) and [`jsfs`](extensions/jsfs): storage's permanent
    filesystem Adapters, a directory under the user's data directory on desktop
    and localStorage in a browser.
- [`m`](libs/m): immutable vectors, rectangles, colors, matrices, quaternions,
    scalar helpers, and splines. Angles use radians.
- [`gfx`](extensions/gfx/README.md): driver-neutral rendering queues, resources, viewport,
    backend contract, frame capture, and per-tick snapshots.
- [`canvas`](bundles/canvas/README.md): layered 2D sprites, text, primitives, and custom
    triangles over gfx, with a snapshot of what a tick recorded.
- [`scene`](bundles/scene/README.md): declarative 3D cameras, glTF models, buffer-built
    meshes, punctual lights, and debug shapes over gfx.
- [`ecsscene`](bundles/ecsscene/README.md): the ecs↔scene binding — components holding
    scene's own types, and the one system that records them into scene.
- [`ui`](bundles/ui/README.md): immediate-mode layout, interaction, canvas-backed visual
    processing, and a snapshot of what layout resolved.
- [`wgpu`](extensions/wgpu/README.md): window, input, timing with pause, step and a hold
    that makes several observations describe one tick, and WebGPU system
    driver.
- [`mcp`](extensions/mcp/README.md): the agent-facing extension point — typed capabilities
    a plugin offers, as a Port that collects every plugin's Provider.
- [`mcpimpl`](extensions/mcp/mcpimpl/README.md): the broker that collects capabilities from
    every provider and serves them to an agent over MCP.

## Plugin Kinds

Every plugin is one kind, and its directory says which:

- **Slots** (`slots/`) cannot work until an **Adapter** fills a **Port** they
    require, and composition fails without one: app, gfx, storage.
- **Extensions** (`extensions/`) provide Adapters for Slots and declare no API:
    wgpu, diskfs, jsfs.
- **Bundles** (`bundles/`) are every other plugin. They require no Port, and
    may collect Adapters or contribute them.
- **Libraries** (`libs/`) define no plugin and import only other Libraries and
    the kernel, and `kernel` imports nothing else in cog.

Every plugin `X` has one shape:

- **The root, `X/`, holds declarations only**: commands, events, resources,
    Ports, Adapters, types, config, errors and `Name`, each in its own fixed
    file. An Extension's root holds only `Name`, config, its Adapters and
    errors.
- **Its functions are forwarders.** They live in `utils.go`, and each one is a
    single call into `X/internal/types` passing its parameters through. A
    Slot's forwarders name no other plugin's types.
- **`X/internal/types`** holds the concrete types the root aliases for
    performance, and **`X/internal/`** holds the implementation.
- **The constructor package, `X/Xplugin`,** exports only `New()`.

| package | may import |
| --- | --- |
| root | libs, kernel, other plugins' roots, its own `internal/types` |
| `internal/types` | libs, kernel, other plugins' roots |
| `internal/` | libs, kernel, any root, its own `internal/` and `internal/types` |
| constructor | kernel, its own `internal/` |

Nothing in cog imports a constructor package or another plugin's internals,
except tests. Games and examples are composition roots and import freely.

The plugins are moving to this shape one at a time. Until each moves, it keeps
the contract root, `…impl` and `internal/` shape of
[ADR 0001](docs/adr/0001-bundles-slots-ports-and-adapters.md), and the paths
and constructors below are its current ones. The kinds, the file allowlists,
where new code goes, the full import table and the plugins not yet moved are in
[`.github/instructions/architecture.instructions.md`](.github/instructions/architecture.instructions.md),
and `go test ./kernel/archtest` enforces them.

## Plugin Layout

Plugin file layout, handler structure, and resource-scope rules are enforced
conventions; see [`.github/instructions/kernel.instructions.md`](.github/instructions/kernel.instructions.md).

Each package's `README.md` is its API. Design records live under
`<package>/docs/specs/`: the package's general spec is `<package>.md` (for
example [`bundles/scene/docs/specs/scene.md`](bundles/scene/docs/specs/scene.md)) and a spec
covering one focused mechanism takes that mechanism's name.

## Lifecycle

```go
config := map[kernel.PluginName]any{
    storage.Name: storage.Config{}.
        WithReadFS("res", storage.DefaultReadPriority, os.DirFS("res")),
    wgpu.Name: wgpu.DefaultConfig().WithTitle("My App"),
}

plugins := []kernel.Plugin{
    storageplugin.New(),
    diskfs.New(diskfs.Config{AppId: "my-app"}), // provides storage's PermanentFS Adapter
    inputimpl.New(),
    gfximpl.New(),
    wgpu.New(), // provides gfx's Backend Adapter
    ...
}

kernel.New(config).
    WithPlugins(plugins...).
    Run(ctx)
```

`kernel.New` returns an `*Engine`: the composition root that owns the plugin set,
registry, scheduler, and lifetime. `WithPlugins` validates and topologically
orders dependencies, calls every `Register`, then finalizes ownership and
subscription DAGs. `Run` calls optional `PluginStarter` implementations in
dependency order, calls the optional `PluginHost` on the calling thread, and
invokes optional `PluginStopper` implementations in reverse order. Unrelated
plugins retain caller order.

At runtime plugins receive a `kernel.Kernel`: a small per-dispatch value carrying
the engine, the invocation context, and the locks its caller holds.

## Communication At A Glance

Plugins talk through three mechanisms, all identified by exact Go type:

- **Commands** are synchronous and return a result. Identity is a distinct
  defined factory type: `type LoadCmd kernel.Command[LoadRequest, LoadResponse]`.
- **Events** are asynchronous and need no registration. Zero or more
  subscriptions may react: `type updateHandler kernel.Subscription[app.UpdateEvent]`.
- **Resources** are shared state whose access the scheduler serializes. A handler
  binds a `Read[T]` or `Write[T]` handle once, and binding is what declares the
  lock.

A handler is a factory returning a `Lock` that binds handles and a body that runs
per invocation. The factory runs once, at registration.

See [`kernel/README.md`](kernel/README.md) for the full API and
[`.github/instructions/kernel.instructions.md`](.github/instructions/kernel.instructions.md)
for the rules that keep usage correct — particularly that values read from a
handle are valid only while the handler holds its lock.

## Errors and Shutdown

All command and subscription handler errors flow through the engine's serialized
`ErrorHandler`. Returning `true` terminates the engine; returning `false` allows
recovery where possible. The default handler logs and terminates. Context
cancellation and the host returning both shut down the runtime.
