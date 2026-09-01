# Cog Engine

Cog is a small typed plugin engine for Go. Plugins communicate through events,
commands, and locked resources; the kernel owns registration, scheduling, and
error handling.

The module requires Go 1.27 because the kernel uses generic methods.

## Packages

- [`kernel`](kernel/README.md): plugin lifecycle, typed registry, scheduler,
    resources, and errors.
- [`app`](app/README.md): driver-neutral update, render, and quit contracts. No
    implementation.
- [`input`](input/README.md): input state, discrete events, and the driver-facing
    apply command.
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
