---
name: "Kernel Usage"
description: "Use when creating or changing Go code that registers plugins, commands, subscriptions, or resources with the kernel, or that runs inside a kernel handler. Covers handler structure, resource handles, scope discipline, and plugin file layout."
applyTo: "**/*.go"
---

# Kernel Usage

`kernel/README.md` documents the API. These are the rules for using it correctly.
Follow them in new and changed code without expanding a focused task into
unrelated cleanup.

## Handler Structure

A command or subscription is a factory returning `(Lock, Execute)` or
`(Lock, Observe)`. The factory runs **once, at registration**. Its returned
closures are cached and reused for every invocation.

```go
type LoadCmd kernel.Command[LoadRequest, LoadResponse]

func load() (kernel.Lock, kernel.Execute[LoadRequest, LoadResponse]) {
	var store kernel.Read[DataStore]
	var cache kernel.Write[*Cache]
	return func(access kernel.ResourceAccess) {
			store = access.GetRead[DataStore]()
			cache = access.GetWrite[*Cache]()
		}, func(k kernel.Kernel, request LoadRequest) (LoadResponse, error) {
			return LoadResponse{Data: store.Get().Load(request.Name)}, nil
		}
}
```

Declare identity types from the generic aliases, never by spelling the signature:

```go
type LoadCmd kernel.Command[LoadRequest, LoadResponse]
type UpdateEventHandler kernel.Subscription[app.UpdateEvent]
```

### The Factory Closure Is Shared

Because the factory runs once, **every variable it declares is shared by all
invocations**. Only resource handles belong there: they are immutable after
binding, so concurrent dispatches read them safely.

Mutable state in the factory closure is a data race. Handlers with disjoint or
read-only lock sets run concurrently. The one exception is a dispatcher bound by
`Uses`: like a resource handle, it is immutable after binding.

```go
// WRONG: counter is shared across concurrent invocations.
func handler() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	counter := 0
	return nil, func(k kernel.Kernel, e app.UpdateEvent) error {
		counter++
		return nil
	}
}
```

Per-invocation state belongs in the `Execute`/`Observe` body. State that must
persist across invocations belongs in a resource.

### Keep `Lock` Straight-Line

`Lock` binds handles and nothing else. No branching, no loops, no work. Every
`GetRead`/`GetWrite` must run unconditionally, because the lock set it produces
is fixed at registration and reused forever. Conditional logic belongs in the
body, where early returns are fine.

## Resource Handles

Requesting a handle is what declares the lock, so declaration and use cannot
drift. `GetRead[T]` permits concurrent readers; `GetWrite[T]` is exclusive and
also authorizes reads.

Never declare a lock on behalf of a command you dispatch. Declare the command
instead, with `Uses` (see below); composition folds its locks in for you.

### Never Store Resources Or Handle Values Locally

A handle is bound once and lives for the engine lifetime, but the value it
exposes is valid **only while the owning handler runs under its locks**. There is
no runtime guard on `Get`/`Set` — violating this is a silent data race, not an
error.

Do not:

- store a value obtained from `Get()` in a plugin struct, a package variable, or
  any object that outlives the handler;
- read or write a handle from a goroutine the handler starts;
- return a resource, an `fs.FS`, an open file, or any live handle from a command
  response;
- keep using a value across a call that may release the lock.

```go
// WRONG: the plugin outlives the handler; the value is unsynchronized after it returns.
func (p *Plugin) subscribe() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var state kernel.Write[*State]
	return func(access kernel.ResourceAccess) {
			state = access.GetWrite[*State]()
		}, func(k kernel.Kernel, _ app.UpdateEvent) error {
			p.state = state.Get()   // escapes the lock scope
			return nil
		}
}
```

Instead: copy immutable data out, do the whole operation inside the handler, or
use a lock-scoped callback command:

```go
useFileSystem(k, UseFileSystemRequest{Use: func(filesystem fs.FS) error {
	// Open, use, and close files before this callback returns.
	return nil
}})
```

Handing a value to a helper **called synchronously within the handler** is fine;
that is ordinary parameter passing. The rule is about outliving the scope.

If a plain object must reach the kernel during one handler pass, give it a field
and clear it on the way out:

```go
state.kernel = k
defer func() { state.kernel = kernel.Kernel{} }()
```

## The Kernel Value

`kernel.Kernel` is a per-dispatch value. Pass it by value; never take its
address, store it, or capture it in anything that outlives the handler. The zero
value panics, which is what makes a stale field fail loudly.

Where a callback from a foreign library needs it, capture it in a closure created
inside the lifecycle method that received it, rather than storing it on the
plugin:

```go
func (p *Plugin) Run(k kernel.Executioner) error {
	p.gpu.OnUpdate(func(dt float64) { p.onUpdate(k, dt) })
	return p.gpu.Run()
}
```

## Commands And Dispatch

A handler dispatches a command only through a dispatcher it declared in its
`Lock`:

```go
var setDesiredViewport func(kernel.Kernel, app.SetDesiredViewportRequest) (app.SetDesiredViewportResponse, error)
return func(access kernel.ResourceAccess) {
		setDesiredViewport = access.Uses[app.SetDesiredViewportCmd]()
	}, func(k kernel.Kernel, _ app.WindowSizeChangeEvent) error {
		_, err := setDesiredViewport(k, app.SetDesiredViewportRequest{Width: 100, Height: 100})
		return err
	}
```

Composition folds that command's lock closure into the handler's own set, so the
handler never names the callee's resources and the dispatch reuses locks it
already holds. Where the kernel reaches the dispatch through a handler-scoped
field, store the bound dispatcher the same way and call it with the per-dispatch
`Kernel`.

Use `Kernel.ExecuteCommandAsync` for fire-and-forget work whose response nobody
reads, and whose locks you do not want to widen the handler with. It runs as an
independent task later, so its request must not carry anything derived from a
locked resource, and its error goes to the central error handler.

`Executioner.ExecuteCommand` is the undeclared synchronous dispatch. Only
lifecycle methods and host callbacks get an `Executioner`; a handler receives a
plain `Kernel` and cannot obtain one.

Return system failures through `error`. Put expected outcomes in the response.
Do not report an error and also return it; it is reported once at an event,
lifecycle, or host boundary.

## Events

Prefer a command when you need a result or synchronous ordering. Prefer an event
when zero or more independent plugins may react.

Publishing is fire-and-forget and outlives the invocation that published it, so
publishing from inside a command handler is safe. Waiting on that publication
from inside the same handler is not, if any subscriber needs a lock the handler
holds.

Use `First`, `Last`, `Before`, and `After` only for real completion
dependencies, never to express a preference. Ready subscribers run concurrently.

## Package File Layout

Place a declaration by **what it is**, not by the feature it belongs to. Use these
filenames consistently:

- `contract.go`: package documentation and shared public value types.
- `commands.go`: command, request, and response declarations only.
- `commandsimpl.go`: command registration and handlers when they are substantial.
- `events.go`: event declarations.
- `resources.go`: documented public aliases to private resource types.
- `resourcesimpl.go`: private resource types, methods, and helpers.
- `config.go`: `Config`, defaults, and immutable `With*` setters.
- `plugin.go`: `Name`, `Plugin`, `New`, `Register`, and compact wiring.
- `err.go`: exported error types and their `Error` methods.

Never add a feature-named catch-all such as `viewport.go` holding a resource, its
events, and its commands together. Split it: the resource goes to `resources.go`,
the events to `events.go`, the commands to `commands.go`, and any shared enum or
value type to `contract.go`.

This layout applies to **contract-only packages** too, such as `app`, even though
they declare no `Plugin`. A contract-only package has no private implementation to
hide, so `resources.go` declares the public resource type directly instead of
aliasing one; say so in the doc comment, since the owning plugin lives elsewhere.

Small plugins may keep handlers in `plugin.go`; split files by ownership, not by
an arbitrary size threshold. A package whose private resources are substantial may
give each its own implementation file named for the resource, as `gfx` does with
`opqueue.go` and `resourcequeue.go`, instead of one `resourcesimpl.go`. Public
resource names should normally alias private implementations, as in
`type FileSystem = fileSystem`, so other plugins can declare locks without reaching into
the implementation.

Declare in `Dependencies` every plugin whose contracts you use. This is enforced:
composition fails with `ErrUndeclaredDependency` if a handler locks a resource
owned by a plugin that is not the owner itself or a transitive declared
dependency. Registration order no longer affects resource binding, but
dependencies still order `Register` and `Start`.

Test plugins are held to the same rule, so a fixture that locks a real plugin's
resource must declare that plugin.

## Validation

After changing kernel-facing code, build the narrowest package that includes the
change, then `go build ./...`. Registration errors surface at startup, not at
compile time, so run the affected target or its tests before considering the
change done.
