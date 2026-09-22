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

- [`kernel`](kernel/docs/README.md): plugin lifecycle, typed registry, scheduler,
    resources, and errors.
- [`app`](slots/app/docs/README.md): the application loop — lifecycle, fixed-step update
    and render events, quit, and time control with pause, step and a hold that
    makes several observations describe one tick — over a platform MainLoop.
- [`input`](bundles/input/docs/README.md): input state, discrete events, the driver-facing
    apply command, and scripted input.
- [`anim`](bundles/anim/docs/README.md): timelines of eased value tracks and one-tick cues,
    advanced every fixed step.
- [`ecs`](bundles/ecs/docs/README.md): entities, components, the sparse-set stores they live
    in, and systems as plain funcs whose parameter types are their lock set.
- [`storage`](slots/storage/docs/README.md): layered read filesystems and one permanent
    writable filesystem, which a platform Adapter provides.
- [`diskstorage`](extensions/diskstorage) and
    [`jsstorage`](extensions/jsstorage): storage's permanent filesystem
    Adapters, a directory under the user's data directory on desktop and
    localStorage in a browser.
- [`m`](libs/m): immutable vectors, rectangles, colors, matrices, quaternions,
    scalar helpers, splines, and piecewise-linear ramps. Angles use radians.
- [`assets`](libs/assets/docs/README.md): one cache for loaded assets, keyed by a
    comparable descriptor — a static run of bytes, a descriptor, a stateless
    loader, and the four verbs a plugin caches an asset family with.
- [`config`](libs/config): plugin configuration named from outside the
    binary — `--cog.<plugin>.<Field>=<value>`, `COG_<PLUGIN>_<FIELD>`, and a
    `COG_ENV` string in the browser's localStorage — applied by one call at the
    composition root.
- [`gfx`](slots/gfx/docs/README.md): driver-neutral rendering queues, resources, viewport,
    backend contract, frame capture, and per-tick snapshots.
- [`canvas`](bundles/canvas/docs/README.md): layered 2D sprites, text, primitives, and custom
    triangles over gfx, with a snapshot of what a tick recorded.
- [`scene`](bundles/scene/docs/README.md): declarative 3D cameras, glTF models, buffer-built
    meshes, punctual lights, and debug shapes over gfx.
- [`ecsscene`](bundles/ecsscene/docs/README.md): the ECS's renderer over model — components
    holding model's refs, batched and recorded into gfx; an app composes it instead of scene.
- [`ui`](bundles/ui/docs/README.md): immediate-mode layout, interaction, canvas-backed visual
    processing, and a snapshot of what layout resolved.
- [`gogpu`](extensions/gogpu/docs/README.md): window, input, frame timing and WebGPU system
    driver, and app's platform MainLoop.
- [`mcp`](bundles/mcp/docs/README.md): the agent-facing extension point — typed capabilities
    a plugin offers, collected through a Port from every plugin's Provider by a
    broker that serves them to an agent over MCP.

## Plugin Kinds

Every plugin is one kind, and its directory says which:

- **Slots** (`slots/`) cannot work until an **Adapter** fills a **Port** they
    require, and composition fails without one: app, gfx, storage.
- **Extensions** (`extensions/`) provide Adapters for Slots and declare no API:
    gogpu, diskstorage, jsstorage.
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
- **`X/internal/types`** holds the concrete types the root aliases, for
    performance or because code there names them, and **`X/internal/`** holds
    the implementation.
- **The constructor package, `X/Xplugin`,** exports only `New()`.

| package | may import |
| --- | --- |
| root | libs, kernel, other plugins' roots, its own `internal/types` |
| `internal/types` | libs, kernel, other plugins' roots |
| `internal/` | libs, kernel, any root, its own `internal/` and `internal/types` |
| constructor | kernel, its own `internal/` |

Nothing in cog imports a constructor package or another plugin's internals,
except tests. Games and examples are composition roots and import freely.

This shape supersedes the one
[ADR 0001](docs/adr/0001-bundles-slots-ports-and-adapters.md) decided. The kinds, the
file allowlists, where new code goes and the full import table are in
[`.github/instructions/architecture.instructions.md`](.github/instructions/architecture.instructions.md),
and `go test ./kernel/archtest` enforces them.

## Plugin Layout

Plugin file layout, handler structure, and resource-scope rules are enforced
conventions; see [`.github/instructions/kernel.instructions.md`](.github/instructions/kernel.instructions.md).

Each package's documents live under `<package>/docs/`. `<package>/docs/README.md` is
its API, and design records live under `<package>/docs/specs/`: the package's general spec is `<package>.md` (for
example [`bundles/scene/docs/specs/scene.md`](bundles/scene/docs/specs/scene.md)) and a spec
covering one focused mechanism takes that mechanism's name.

## Lifecycle

```go
config := map[kernel.PluginName]any{
    diskstorage.Name: diskstorage.Config{AppId: "my-app"},
    gogpu.Name: gogpu.Config{}.WithTitle("My App"),
}

plugins := []kernel.Plugin{
    storageplugin.New(),
    diskstorageplugin.New(), // provides storage's PermanentFS Adapter
    inputplugin.New(),
    appplugin.New(),
    gfxplugin.New(),
    gogpuplugin.New(), // provides app's MainLoop and gfx's Backend Adapters
    mygame.New(os.DirFS("res")), // provides storage's read mount for res/
    ...
}

kernel.New(config).
    WithPlugins(plugins...).
    Run()
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

See [`kernel/docs/README.md`](kernel/docs/README.md) for the full API and
[`.github/instructions/kernel.instructions.md`](.github/instructions/kernel.instructions.md)
for the rules that keep usage correct — particularly that values read from a
handle are valid only while the handler holds its lock.

## Errors and Shutdown

All command and subscription handler errors flow through the engine's serialized
`ErrorHandler`. Returning `true` terminates the engine; returning `false` allows
recovery where possible. The default handler logs and terminates. Context
cancellation and the host returning both shut down the runtime.

## Checks

cog has no CI. A change is checked by running `scripts/check.sh` from anywhere
inside the repo. It stops at the first failing step, printing each step's name
before it runs:

1. `go build ./...`
2. `go vet ./...`
3. `gofmt -l` over every tracked `.go` file, failing on and naming any file it
   lists
4. `GOOS=js GOARCH=wasm go build ./...` and `GOOS=js GOARCH=wasm go vet ./...`
5. `go test ./...`
6. `go test -tags ecs_validate`, Validation mode, over every package but
   `bundles/ecsphysics2d/...`, which is excluded until
   [#548](https://github.com/dvoyni/cog/issues/548) is fixed
7. `go test -race` over every package but `docs/research/ecs-go-mechanics-bench`,
   a research harness that asserts nothing and whose allocation sweep would
   multiply under the detector, and `kernel/archtest`, which checks package
   structure, runs no concurrent code and is already slow; both still run in
   step 5

Steps 1–6 need only `sh`, `git` and Go, and run anywhere, Git Bash on Windows
included. Step 7 needs cgo and a C toolchain, so it runs under WSL or on Linux
(`apt install build-essential` on Debian or Ubuntu). On a machine that cannot
build the race detector the step fails with an explanation; it never skips.

- `scripts/check.sh --no-race` runs steps 1–6, for a machine without a C
  toolchain.
- `scripts/check.sh --race` runs step 7 only.

Allocation-count assertions run only without `-race`: the detector changes
allocation behaviour, so under it they skip. `extensions/jsstorage` and
`extensions/jssound` are built and vetted for js/wasm but their tests are not
run, since they need a js/wasm runtime the script does not drive.
