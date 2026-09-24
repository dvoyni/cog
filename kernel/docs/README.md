# kernel

`github.com/dvoyni/cog/kernel` is Cog's typed plugin microkernel. Plugins
register resources, synchronous commands, and ordered event subscriptions. The
kernel orders plugin lifecycles, validates ownership, schedules resource locks,
centralizes boundary errors, and runs an optional host.

## Dependencies

The package uses only the Go standard library and is not itself a plugin.

## Engine and Kernel

The package has two faces, split by phase.

`Engine` is the composition root. It exists once, is built before startup, and
owns the plugin set, registry, scheduler, and lifetime.

`Kernel` is the runtime handle. It is a small value created per dispatch and
passed by value, never as a pointer. It carries the engine it belongs to and
nothing else. It is scoped to one dispatch, so retaining it past the handler
that received it is a bug. There is no `context.Context` anywhere in the kernel:
cog is a standalone application, not a service, and a deadline that matters
belongs to the plugin that has one.

```go
engine := kernel.New(config).
    Handler(handleError).
    WithPlugins(plugins...)
cause := engine.Run()
```

`New` creates an unstarted engine. `WithPlugins` validates the complete
dependency graph, orders plugins with stable caller-order ties, calls
`Register`, and finalizes ownership and subscription DAGs. `Run` calls each
`PluginStarter` in dependency order, runs zero or one `PluginHost`, and calls
each active `PluginStopper` in reverse order. Plugins implement only the
lifecycle capabilities they need. It blocks, and answers how the run ended: nil
on an ordinary quit, the initialization failure of a composition that never
started, or the first error the handler terminated on.

`Engine.Quit` asks a running engine to stop. A headless engine returns once
every started plugin has stopped; one with a Host cannot, because the Host owns
the blocking loop, so `PluginHost.Quit` asks it to leave. app implements that by
asking its MainLoop to quit.

`Ready` is closed once the startup attempt is over, whether it succeeded or not:
a composition that failed closes it too, so nobody waiting on it blocks forever.
It is not a success signal, and there is no second place to ask — what went
wrong is what `Run` returns.

**Nothing is refused for being late.** A dispatch that started finishes, a
dispatch arriving during shutdown runs until the scheduler stops, and the engine
keeps no liveness flag for anyone to consult. The scheduler is live from `New`,
so a dispatch before `Run` runs too.

`PluginName` identifies plugins and keys their values in the configuration map.

```go
type Plugin interface {
    Name() PluginName
    Dependencies() []PluginName
    Register(*Registrar, any) error
}

type PluginStarter interface { Plugin; Start(Executioner) error }
type PluginStopper interface { Plugin; Stop(Executioner) error }
type PluginHost    interface { Plugin; Run(Executioner) error; Quit() }
```

Lifecycle methods receive an `Executioner`. `Stop`'s still dispatches: the
scheduler stops after every `Stop` has run, so shutdown work is talking to a
live engine. `Executioner.Quitting` is a channel closed when the engine is asked
to stop, for a plugin that owns something outside the engine — a listening
socket, a worker it started in `Start` — and has to stop accepting before the
engine tears down.

A plugin reaches another plugin only through a typed command, a published
event, a locked resource, or an Adapter bound to it at composition. The engine
hands out no plugin value: there is no lookup by plugin type, and a plugin that
wants the contributors to a Port it declares collects them with
`CollectAdapters`.

## Handlers: Lock and Execute

A command or subscription is a **factory** returning two closures:

```go
type Lock                             func(ResourceAccess)
type Execute[TRequest, TResponse any] func(Kernel, TRequest) TResponse
type Observe[TEvent any]              func(Kernel, TEvent)

type Command[TRequest, TResponse any] = func() (Lock, Execute[TRequest, TResponse])
type Subscription[TEvent any]         = func() (Lock, Observe[TEvent])
```

The factory runs **once, at registration**. Its `Lock` binds resource handles;
its `Execute` or `Observe` is cached and reused for every later invocation. A
nil `Lock` declares no resources.

A body returns a response, or nothing. There is no error channel: a failure the
caller should act on is part of the response, and a failure nobody can act on
goes to `ReportError`. That is what stops a body saying the same thing twice.

Requesting a handle is what declares the lock, so a handler cannot declare a
resource it does not use, or use one it did not declare. There is no separate
`Reads[T]`/`Writes[T]` declaration to keep in sync. `ResourceAccess.Uses[TCommand]`
works the same way for the commands a handler dispatches.

Because resource cells are created once and never replaced, handles bound during
registration stay valid for the engine lifetime — regardless of the order in
which plugins register. A plugin may bind another plugin's resource before that
plugin has initialized it.

## Commands

A command's identity is a distinct defined type built from `Command`. The type is
the declaration and carries the name; its handler is private and takes the same
name with an `Impl` suffix:

```go
type LoadCmd kernel.Command[LoadRequest, LoadResponse]

func loadCmdImpl() (kernel.Lock, kernel.Execute[LoadRequest, LoadResponse]) {
    var config kernel.Read[Config]
    var cache  kernel.Write[*Cache]
    return func(access kernel.ResourceAccess) {
            config = access.GetRead[Config]()
            cache = access.GetWrite[*Cache]()
        }, func(k kernel.Kernel, request LoadRequest) LoadResponse {
            _ = config.Get()
            cache.Get().Store(request.Name)
            return LoadResponse{}
        }
}

registrar.HandleCommand[LoadCmd](loadCmdImpl)
```

`Registrar.HandleCommand` registers one owned handler per command type;
duplicates fail composition. It returns nothing — a command has no configurable
surface.

A command is declared as the triple `LoadCmd` / `LoadRequest` / `LoadResponse`,
with both payload types named even when empty. Declarations belong in the
package's `commands.go` and handlers in `commandsimpl.go`; see
`.github/instructions/kernel.instructions.md` for the full convention.

### Dispatching

A handler declares the commands it dispatches in its `Lock`, exactly as it
declares resources:

```go
var load func(kernel.Kernel, LoadRequest) LoadResponse
return func(access kernel.ResourceAccess) {
        load = access.Uses[LoadCmd]()
    }, func(k kernel.Kernel, event app.UpdateEvent) {
        load(k, LoadRequest{Name: "level"})
    }
```

Composition folds `LoadCmd`'s lock set — transitively, through whatever it uses
in turn — into the declaring handler's own set, so the caller never names the
callee's resources. The dispatch then reuses the locks the handler already
holds, which keeps lock acquisition atomic and one-shot. A `Uses` cycle, or
`Uses` of an unregistered command, fails composition.

Because the fold is static, a declared dispatch asks the scheduler for nothing,
and the kernel runs it directly rather than paying a coordinator round-trip for
a decision finalisation already took. It costs what the dispatch it wraps costs.

`Kernel.ExecuteCommandAsync` needs no declaration. It runs the command as an
independent top-level task that acquires its own locks and returns nothing. Because the task runs after
the caller's locks are gone, its request must not carry anything derived from a
locked resource.

`Executioner.ExecuteCommand` is the undeclared synchronous dispatch. Only the
engine mints an `Executioner`, for plugin lifecycle methods and host callbacks:
they run outside any handler, so every command they dispatch acquires its own
set from the scheduler. A handler receives a plain `Kernel` and therefore cannot
dispatch except through `Uses` or `ExecuteCommandAsync`. That is what makes the
deadlock unrepresentable: a handler holding locks has no way to ask for more.

`ExecuteCommand` answers with the response alone. A dispatch the kernel could
not perform — an unregistered command, a body that panicked — is reported, and
the caller receives the zero response.

## Events

Events need no declaration or registration. A subscription's identity is a
distinct defined type built from `Subscription`:

```go
type updateHandler kernel.Subscription[app.UpdateEvent]

func update() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
    var state kernel.Write[*State]
    return func(access kernel.ResourceAccess) {
            state = access.GetWrite[*State]()
        }, func(k kernel.Kernel, event app.UpdateEvent) {
            state.Get().Step(event.Dt)
        }
}

registrar.Subscribe[updateHandler](update).First()
```

`Registrar.Subscribe` returns `*Ordering[TEvent]`, whose fluent API is:

- `Before[TSubscription]()`, `After[TSubscription]()`: add completion-order
  edges for the same event type.
- `First()`, `Last()`: order before or after every ordinary subscriber.

`Kernel.PublishEvent` returns a `Publication`. Ready subscribers run
concurrently subject to resource locks. `Before`/`After` dependencies wait for
predecessor completion. First and Last are concurrent phase groups and may have
dependencies within their own group. Discarding the handle is asynchronous;
`Publication.Wait` is a barrier and answers nothing.

A panic is the only subscriber failure a publication can see, and descendants of
a subscriber that panicked are skipped. One that merely reports has still
completed, and its dependents run: reporting says what went wrong and says
nothing about whether the work downstream can go ahead. Where the answer is no,
it belongs in state the dependent reads.

Publishing is fire-and-forget: the publication outlives the invocation that
published it, so an event published from inside a command outlives that
command's return.

## Resources

`Registrar.InitResource[T]` creates the resource identified by exact Go type and
supplies its initial value. Duplicate owners and declared resources without
initial values fail finalization.

Inside a `Lock`, `ResourceAccess.GetRead[T]()` and `GetWrite[T]()` declare the
lock and return a handle. A write lock also authorizes reads and supersedes a
read lock on the same type.

```go
value := handle.Get()      // Read[T] or Write[T]
handle.Set(replacement)    // Write[T] only
```

`Get` gives read only access, on a `Write` handle too: a resource is replaced
with `Set`, never by assigning through what `Get` returns. A handler never
declares a lock on behalf of a command it dispatches;
`ResourceAccess.Uses` does that for it.

`ResourceAccess.Exclusive()` declares that the handler never runs concurrently
with **itself**: a second invocation queues behind the first rather than joining
it. It excludes that handler and nothing else, because the key is the handler's
own identity type and no two handlers share one, so it costs no parallelism
against anybody else. A handler that write-locks whatever it mutates already has
this for free — two invocations conflict on that write — so `Exclusive` is for
state no lock reaches, which means mutable state in the factory closure. That is
forbidden by default; read "The Factory Closure Is Shared" before reaching for
it. The key names no resource, so it never appears among `Writes`.

`Registrar.Dependency[T]()` returns a resource's value **during registration**,
from `Register`. It is for a value a plugin registers against rather than runs
with, such as the ECS authority a Component's Store enrols in. `T` must be owned
by the reader or a plugin in its transitive dependency closure, which has
therefore registered first; otherwise it returns `ErrUnavailableDependency`.
It declares no lock and is not available to a `Lock`, whose only way to reach a
resource is a handle it binds. It returns the value as registration left it, so
keep what it returns only if it is a pointer its owner never replaces with
`Set`.

Handles are bound once and live for the engine lifetime, but the **value** they
expose is only valid while the owning handler runs under its locks. Do not read
or write a handle from a goroutine that outlives the handler, and do not retain
values derived from one. This is a contract, not a checked invariant.

## Ports and Adapters

A **Port** is a declared identity type naming what a plugin needs filled: an
interface its Adapters implement, or a value type its Adapters are;
an **Adapter** is a declared identity type naming one way of filling it, under
which a plugin provides a plain value that the engine binds during composition.
The full rules are in [`specs/ports.md`](specs/ports.md).

Both are declared like commands, as defined types built from a kernel shape:

```go
type MainLoopPort kernel.RequiredPort[MainLoop]    // app: exactly one Adapter
type BackendPort kernel.RequiredPort[Backend]      // gfx: exactly one Adapter
type ProviderPort kernel.CollectedPort[Provider]   // mcp: any number, zero included
type ReadMountPort kernel.CollectedPort[ReadMount] // storage: plain data, any number
type AppMainLoop kernel.Adapter[app.MainLoopPort]  // gogpu: fills app.MainLoopPort
type GfxBackend kernel.Adapter[gfx.BackendPort]    // gogpu: fills gfx.BackendPort
```

Three declarations on `Registrar` take those types:

```go
backend := registrar.RequireAdapter[gfx.BackendPort]()           // exactly one
providers := registrar.CollectAdapters[mcp.ProviderPort]()       // any number, zero included
registrar.ProvideAdapter[GfxBackend](gfx.Backend(device))        // contribute one
```

`RequireAdapter[P]` returns a `RequiredAdapter[I]` whose `Get()` yields the one
bound Adapter, typed as the type `I` the Port is built on. `CollectAdapters[P]` returns a
`CollectedAdapters[I]` whose `Get()` yields a fresh `[]ContributedAdapter[I]`:
each Adapter with the `PluginName` of the plugin that provided it, in plugin
order. `ProvideAdapter[A]` takes a value of the type the Port is built on, so
the compiler checks it implements the interface or is the value type. Go infers
type parameters from arguments before constraints, so a concrete value for an
interface Port is converted to the interface first. Requiring a collected Port, collecting a required one, or providing
anything but an Adapter type does not compile.

- A Port may be built on any type: an interface when its Adapters are
  behaviour, a value type such as a struct when they are plain data.
- Bindings are keyed by the Port type: two Ports on one type are distinct.
- Binding happens at finalization: after every `Register`, before any `Start`.
  It adds no plugin-dependency edge in either direction and does not depend on
  registration order.
- `Get` panics before finalization and is valid from `Start` onwards. An Adapter
  is not a Resource: reading it takes no lock, and its thread rules are the Port
  type's business.
- A required Port with no Adapter fails composition with `ErrMissingAdapter`;
  with several, `ErrDuplicateAdapter`.
- A nil Adapter is refused when it is provided: `ProvideAdapter[A]` given an
  untyped nil fails composition with `ErrNilAdapter` and contributes nothing,
  whether its Port is required, collected or declared by no plugin. No plugin
  ever receives a nil. A typed nil, such as a nil pointer, is a valid interface
  value and is not nil.
- A plugin declares each Port once. Declaring it twice fails with
  `ErrDuplicateRegistration` of kind `port declaration`.
- An Adapter nobody requires or collects is not an error.

## Errors

`ErrorHandler func(error) error` receives serialized errors and decides what
they mean. Returning nil keeps the engine running; returning an error terminates
it, and that error is what `Run` answers with. `Engine.Handler` sets it, a nil
handler restores the default, and `Kernel.ReportError` is what reaches it.

**The handler is the only place termination is decided.** The engine has no
opinion of its own about any error, including a plugin panic: that opinion lives
in the default handler, which logs everything and terminates on `ErrPluginPanic`
alone. A game that wants a different rule installs its own handler.

`ReportError` and `ReportErrorOnce` answer nothing. What happens next is the
handler's decision, and the reporter has finished with the failure either way.
`ReportErrorOnce` fires a burst the first time it is called under a key and
drops it thereafter; `ForgetReportedError` and `ForgetReportedErrors` let a key
speak again once the condition it named could have changed.

Code without a `Kernel` — a render-thread object, an Adapter's backend — hands
its error back to code that has one. The engine recovers only the goroutines it
started; a plugin that spawns its own owns their failures.

Exported error types:

- `ErrSchedulerStopped`: work was submitted after the coordinator stopped. It is
  not reported: a dispatch arriving after shutdown is the engine ending, not a
  failure in the thing that dispatched.
- `ErrConflictingPluginName`: two registered plugins use the same name.
- `ErrMissingPluginDependency`: a plugin's declared dependency is absent.
- `ErrPluginDependencyCycle`: plugin dependencies cannot be ordered.
- `ErrMultipleHosts`: more than one plugin is a `kernel.Host`, that is,
  implements `PluginHost`.
- `ErrDuplicateRegistration`: a contract or resource has multiple owners.
- `ErrMissingResource`: a declared resource has no initial value.
- `ErrUsingUnknownCommand`: a handler declares `Uses` of a command no plugin
  registered.
- `ErrUsingCommandCycle`: `Uses` declarations form a cycle, so no lock closure
  exists.
- `ErrUndeclaredDependency`: a handler locks a resource owned by a plugin its
  own plugin does not declare a dependency on.
- `ErrMissingAdapter`: a plugin requires a Port no plugin provides an Adapter
  for; it names the plugin and the Port type.
- `ErrDuplicateAdapter`: a plugin requires a Port several Adapters are provided
  for; it names every Adapter type and its contributor.
- `ErrNilAdapter`: a plugin provides a nil Adapter; it names the plugin and the
  Adapter type.
- `ErrUnavailableDependency`: `Dependency` was asked for a resource with no
  initial value or an undeclared owner. It is returned to the caller, whose
  `Register` propagates it.
- `ErrPluginPanic`: a plugin boundary panicked; includes owner and stack.
- `ErrSubscriptionCycle`: event ordering contains a cycle; its fields expose
  the event and subscription types.
- `ErrExecutingUnknownCommand[TCommand]`: no handler is registered for the
  requested command type.

Each exported error type implements `Error() string`.

## Introspection

`Engine.Describe` returns a detached `ArchitectureDescription` of the finalized
architecture: plugins and their dependencies, resources and commands with their
owners, every required or collected Port with the plugin declaring it and the
Adapters bound to it, every subscription with its event, phase, and ordering dependencies, and
the conflict report described below. `Dump` renders it as a readable table.

`ArchitectureDescription.Ports` holds one `PortDescription` per `RequireAdapter`
or `CollectAdapters` declaration: the Port `Type`, the `Interface` it is built
on, its declaring `Owner`, whether it `Collects`, and its `Adapters` in plugin
order, each an `AdapterDescription` of the Adapter `Type` and the `Plugin` that
provided it. `Dump` prints them in a `ports:` section, as
`app.MainLoopPort (app) requires [gogpu.AppMainLoop (gogpu)]`. An Adapter nobody
consumes binds to nothing and is not listed.

`CommandDescription` and `SubscriptionDescription` also carry `Reads`, `Writes`
and `Uses`. `Reads` and `Writes` are the **resolved, transitive** lock sets —
what the handler ends up holding once every command it declares in `Uses` has
been folded in — and `Uses` holds the direct edges that explain them. This is
the one fact in a description that no source file states, because a handler
deliberately never names the resources behind a command it dispatches.

Both also carry `SelfExclusive`, reporting that the handler declared
`Exclusive`. It sits apart from `Writes` because it names no resource, and the
conflict report below pairs distinct handlers only, so this flag is the one
place a handler's exclusion against itself shows.

### The conflict report

`ArchitectureDescription.Contention` is the pairwise reading of those same lock
sets: which handlers can never overlap, and what that costs. It is computed once,
by `Describe`, from registry state that is immutable after finalization — no new
reflection, no resource walk, and nothing on the dispatch path.

It has three views, each **ranked rather than enumerated**:

- `Contention.Resources` — the resources handler pairs serialise on, most
  contended first. This is the view that stays useful however wide the widest
  lock is: the resource every handler touches comes out on top, correctly, and
  the narrower ones are named underneath it. A resource nothing contends on is
  absent.
- `Contention.Phases` — per event and phase, how many member pairs serialise out
  of the pairs the phase has, whether that leaves it `SingleThreaded`, and which
  members hold `WidestLocks`: the ones conflicting with every other member, which
  are what makes the phase run single file. It accounts for locks only;
  `Before`/`After` edges are reported by `SubscriptionDescription.DependsOn`.
- `Contention.Handlers` — every handler pair that can never overlap and the
  resources that is true of, the pair sharing the most first. This set is
  quadratic, so `Dump` prints the worst of it and counts the rest; the
  description carries all of it. A pair's shared keys may include an `Exclusive`
  key absorbed through `Uses`: a handler that uses an `Exclusive` command takes
  on its self-exclusion and can never overlap it, and the pair names that key by
  the command's identity type. It names no resource, so it appears in
  `Contention.Handlers` and never in `Contention.Resources`.

Commands and subscriptions are treated alike in `Resources` and `Handlers`: any
two handlers may be in flight at once, so any two may serialise. Only `Phases` is
subscriptions alone, because a command is in no phase. A `HandlerRef` names a
handler in any of them: its `Kind`, its identity `Type`, its `Owner`, and for a
subscription the `Event`.

**It reports; it never errors and never panics.** A conflict is not a defect —
two handlers writing one resource is how shared state works, and only the reader
knows whether the serialisation is worth paying for. There are no suppression
knobs and no thresholds.

`Executioner.Describe` returns the same value from inside the running engine.
An `Executioner` exists only once `Run` begins, so the description it returns
is always the final one, and it reads registry state that is immutable after
finalization: no handle, no lock, no tick and no scheduler.

```go
func (e Executioner) Describe() ArchitectureDescription
```

It is on `Executioner` rather than `Kernel`, so it is phase-gated for free: only
the engine mints an `Executioner`, and only once `Run` begins.

### How types are named

Every place the kernel prints a type — `Dump`, the conflict report, each `Err…`
message, plugin boundaries — renders it with `TypeName`, and so do the tools
built on `Describe` (`mcpserver_architecture`, `ui_layout`):

```go
func TypeName(t reflect.Type) string
```

It is `reflect.Type.String()` with one rule added. A named type declared in a
package whose import path has an `internal` segment renders under its
enclosing package, the segment before the last `internal`:

| type | `String()` | `TypeName` |
| --- | --- | --- |
| `*OpQueue` declared in `bundles/canvas/internal` | `*internal.OpQueue` | `*canvas.OpQueue` |
| `installModelCmd` declared in `bundles/scene/internal` | `internal.installModelCmd` | `scene.installModelCmd` |
| `RenderOnRender` declared in `slots/gfx` | `gfx.RenderOnRender` | `gfx.RenderOnRender` |

A plugin that declares a type in `internal/` or `internal/types` aliases it in
its root, so the rendered name is the alias a caller writes and greps for.
Go's reflection cannot see aliases, which is why the rule is needed at all:
`String()` uses the package name, and that is `internal` for every such package.

- The rule applies inside pointers, slices, arrays, maps, channels, functions
  and generic type arguments: `[]*ui.Frame`, `m.Maybe[*canvas.Font]`. `reflect` spells a type argument by its full import
  path, and `TypeName` shortens that to the package part as well.
- It applies to any module's `internal` packages, a game's included.
- Predeclared types, unnamed structs and interfaces, and types named outside
  an `internal` package render as `reflect` renders them.
- A type the plugin declares in `internal/` that its root does not alias
  renders under the plugin too: scene's unexported `installModelCmd` reads
  `scene.installModelCmd` in `Dump`.

`ArchitectureDescription` keeps `reflect.Type` fields; only the string form is
`TypeName`'s.

## Public API Index

- Composition: `New`, `Engine`, `Engine.Handler`, `Engine.WithPlugins`,
  `Engine.Run`, `Engine.Quit`, `Engine.Ready`, `Engine.Executioner`,
  `Engine.Describe`, `Dump`,
  `ArchitectureDescription`.
- Introspection: `PluginDescription`, `ResourceDescription`,
  `CommandDescription`, `SubscriptionDescription`, `ContentionDescription`,
  `ResourceContention`, `PhaseContention`, `HandlerConflict`, `HandlerRef`,
  `PortDescription`, `AdapterDescription`, `TypeName`.
- Runtime: `Kernel`, `Kernel.ExecuteCommandAsync`, `Kernel.PublishEvent`,
  `Kernel.ReportError`, `Kernel.ReportErrorOnce`, `Kernel.ForgetReportedError`,
  `Kernel.ForgetReportedErrors`, `Executioner`, `Executioner.ExecuteCommand`,
  `Executioner.Quitting`, `Executioner.Describe`,
  `Publication`, `Publication.Wait`.
- Registration: `Registrar`, `Registrar.InitResource`,
  `Registrar.Dependency`, `Registrar.HandleCommand`, `Registrar.Subscribe`,
  `Ordering[TEvent]`.
- Ports and Adapters: `RequiredPort`, `CollectedPort`, `Adapter`,
  `RequiredPortConstraint`, `CollectedPortConstraint`, `AdapterConstraint`,
  `Registrar.RequireAdapter`, `Registrar.CollectAdapters`,
  `Registrar.ProvideAdapter`, `RequiredAdapter[I]`, `RequiredAdapter.Get`,
  `CollectedAdapters[I]`, `CollectedAdapters.Get`, `ContributedAdapter[I]`.
- Handlers: `Lock`, `Execute`, `Observe`, `Command`, `Subscription`,
  `CommandConstraint`, `SubscriptionConstraint`.
- Resources: `ResourceAccess`, `ResourceAccess.GetRead`,
  `ResourceAccess.GetWrite`, `ResourceAccess.Uses`,
  `ResourceAccess.Exclusive`, `Read[T]`, `Write[T]`.
- Contracts: `PluginName`, `Plugin`, `PluginStarter`, `PluginStopper`,
  `PluginHost`, `ErrorHandler`.
