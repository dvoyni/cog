# kernel

`github.com/cog-engine/kernel` is Cog's typed plugin microkernel. Plugins
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
passed by value, never as a pointer. It carries the engine and the invocation
context. It is scoped to one dispatch, so retaining it past the handler that
received it is a bug. The zero `Kernel` panics on every method.

```go
engine := kernel.New(config).
    Handler(handleError).
    WithPlugins(plugins...)
engine.Run(ctx)
```

`New` creates an unstarted engine. `WithPlugins` validates the complete
dependency graph, orders plugins with stable caller-order ties, calls
`Register`, and finalizes ownership and subscription DAGs. `Run` starts the
scheduler, calls each `PluginStarter` in dependency order, runs zero or one
`PluginHost`, and calls each active `PluginStopper` in reverse order. Plugins
implement only the lifecycle capabilities they need. A headless engine blocks
until cancellation.

`PluginName` identifies plugins and keys their values in the configuration map.

```go
type Plugin interface {
    Name() PluginName
    Dependencies() []PluginName
    Register(*Registrar, any) error
}

type PluginStarter interface { Plugin; Start(Executioner) error }
type PluginStopper interface { Plugin; Stop(Executioner) error }
type PluginHost    interface { Plugin; Run(Executioner) error }
```

Lifecycle methods receive an `Executioner` rather than a `context.Context`; use
`Kernel.Context()` where a context is needed. `Stop` receives one carrying the
shutdown context, which outlives engine cancellation.

## Handlers: Lock and Execute

A command or subscription is a **factory** returning two closures:

```go
type Lock                     func(ResourceAccess)
type Execute[TRequest, TResponse any] func(Kernel, TRequest) (TResponse, error)
type Observe[TEvent any]              func(Kernel, TEvent) error

type Command[TRequest, TResponse any] = func() (Lock, Execute[TRequest, TResponse])
type Subscription[TEvent any]         = func() (Lock, Observe[TEvent])
```

The factory runs **once, at registration**. Its `Lock` binds resource handles;
its `Execute` or `Observe` is cached and reused for every later invocation. A
nil `Lock` declares no resources.

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
        }, func(k kernel.Kernel, request LoadRequest) (LoadResponse, error) {
            _ = config.Get()
            cache.Get().Store(request.Name)
            return LoadResponse{}, nil
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
var load func(kernel.Kernel, LoadRequest) (LoadResponse, error)
return func(access kernel.ResourceAccess) {
        load = access.Uses[LoadCmd]()
    }, func(k kernel.Kernel, event app.UpdateEvent) error {
        _, err := load(k, LoadRequest{Name: "level"})
        return err
    }
```

Composition folds `LoadCmd`'s lock set — transitively, through whatever it uses
in turn — into the declaring handler's own set, so the caller never names the
callee's resources. The dispatch then reuses the locks the handler already
holds, which keeps lock acquisition atomic and one-shot. A `Uses` cycle, or
`Uses` of an unregistered command, fails composition.

`Kernel.ExecuteCommandAsync` needs no declaration. It runs the command as an
independent top-level task that acquires its own locks, returns nothing, and
sends any error to the centralized error handler. Because the task runs after
the caller's locks are gone, its request must not carry anything derived from a
locked resource.

`Executioner.ExecuteCommand` is the undeclared synchronous dispatch. Only the
engine mints an `Executioner`, for plugin lifecycle methods and host callbacks:
they run outside any handler, so every command they dispatch acquires its own
set from the scheduler. A handler receives a plain `Kernel` and therefore cannot
dispatch except through `Uses` or `ExecuteCommandAsync`.

Use `Kernel.WithContext(ctx)` to add a caller deadline or cancellation scope.

## Events

Events need no declaration or registration. A subscription's identity is a
distinct defined type built from `Subscription`:

```go
type updateHandler kernel.Subscription[app.UpdateEvent]

func update() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
    var state kernel.Write[*State]
    return func(access kernel.ResourceAccess) {
            state = access.GetWrite[*State]()
        }, func(k kernel.Kernel, event app.UpdateEvent) error {
            state.Get().Step(event.Dt)
            return nil
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
predecessor completion; descendants of failed subscribers are skipped. First and
Last are concurrent phase groups and may have dependencies within their own
group. Discarding the handle is asynchronous; `Publication.Wait` is a barrier.

Publishing is fire-and-forget: the publication is bounded by the publisher's
lifetime scope, not by the invocation that published it, so an event published
from inside a command outlives that command's return.

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

Most resources are pointers mutated in place; `Set` is for the few reassigned
wholesale. A handler never declares a lock on behalf of a command it dispatches;
`ResourceAccess.Uses` does that for it.

Handles are bound once and live for the engine lifetime, but the **value** they
expose is only valid while the owning handler runs under its locks. Do not read
or write a handle from a goroutine that outlives the handler, and do not retain
values derived from one. This is a contract, not a checked invariant.

## Errors

`ErrorHandler func(error) bool` receives serialized errors. Returning true
cancels the engine; returning false permits recovery where possible.
`Engine.Handler` sets it, `Kernel.ReportError` invokes it directly, and a nil
handler restores the terminating default.

Exported error types:

- `ErrSchedulerStopped`: work was submitted after cancellation.
- `ErrConflictingPluginName`: two registered plugins use the same name.
- `ErrMissingPluginDependency`: a plugin's declared dependency is absent.
- `ErrPluginDependencyCycle`: plugin dependencies cannot be ordered.
- `ErrMultipleHosts`: more than one plugin implements `PluginHost`.
- `ErrDuplicateRegistration`: a contract or resource has multiple owners.
- `ErrMissingResource`: a declared resource has no initial value.
- `ErrUsingUnknownCommand`: a handler declares `Uses` of a command no plugin
  registered.
- `ErrUsingCommandCycle`: `Uses` declarations form a cycle, so no lock closure
  exists.
- `ErrPluginPanic`: a plugin boundary panicked; includes owner and stack.
- `ErrSubscriptionCycle`: event ordering contains a cycle; its fields expose
  the event and subscription types.
- `ErrExecutingUnknownCommand[TCommand]`: no handler is registered for the
  requested command type.

Each exported error type implements `Error() string`.

## Introspection

`Engine.Describe` returns a detached `ArchitectureDescription` of the finalized
architecture: plugins and their dependencies, resources and commands with their
owners, and every subscription with its event, phase, and ordering dependencies.
`Dump` renders it as a readable table.

## Public API Index

- Composition: `New`, `Engine`, `Engine.Handler`, `Engine.WithPlugins`,
  `Engine.Run`, `Engine.Ready`, `Engine.Executioner`, `Engine.Describe`, `Dump`,
  `ArchitectureDescription`.
- Runtime: `Kernel`, `Kernel.Context`, `Kernel.WithContext`,
  `Kernel.ExecuteCommandAsync`, `Kernel.PublishEvent`, `Kernel.ReportError`,
  `Executioner`, `Executioner.ExecuteCommand`, `Publication`, `Publication.Wait`.
- Registration: `Registrar`, `Registrar.InitResource`,
  `Registrar.HandleCommand`, `Registrar.Subscribe`, `Ordering[TEvent]`.
- Handlers: `Lock`, `Execute`, `Observe`, `Command`, `Subscription`,
  `CommandConstraint`, `SubscriptionConstraint`.
- Resources: `ResourceAccess`, `ResourceAccess.GetRead`,
  `ResourceAccess.GetWrite`, `ResourceAccess.Uses`, `Read[T]`, `Write[T]`.
- Contracts: `PluginName`, `Plugin`, `PluginStarter`, `PluginStopper`,
  `PluginHost`, `ErrorHandler`.
