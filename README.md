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
- [`app`](app/README.md): driver-neutral update, render, and quit contracts. No
    implementation.
- [`input`](input/README.md): input state, discrete events, and the driver-facing
    apply command.
- [`anim`](anim/README.md): timelines of eased value tracks and one-tick cues,
    advanced every fixed step.
- [`storage`](storage/README.md): layered read filesystems and one permanent
    writable filesystem.
- [`m`](m): immutable vectors, rectangles, colors, matrices, quaternions,
    scalar helpers, and splines. Angles use radians.
- [`gfx`](gfx/README.md): driver-neutral rendering queues, resources, viewport,
    and backend contract.
- [`canvas`](canvas/README.md): layered 2D sprites, text, primitives, and custom
    triangles over gfx.
- [`ui`](ui/README.md): immediate-mode layout, interaction, and canvas-backed
    visual processing.
- [`wgpu`](wgpu/README.md): window, input, timing, and WebGPU system driver.

## Plugin Layout

Plugin file layout, handler structure, and resource-scope rules are enforced
conventions; see [`.github/instructions/kernel.instructions.md`](.github/instructions/kernel.instructions.md).

## Lifecycle

```go
config := map[kernel.PluginName]any{
    storage.Name: storage.DefaultConfig("my-app").WithReadDiskFS("res"),
    wgpu.Name:    wgpu.DefaultConfig().WithTitle("My App"),
}

plugins := []kernel.Plugin{
    storage.New(),
    input.New(),
    gfx.New(),
    wgpu.New(),
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
