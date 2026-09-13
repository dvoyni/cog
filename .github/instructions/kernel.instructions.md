---
name: "Kernel Usage"
description: "Use when creating or changing Go code that registers plugins, commands, subscriptions, resources or Adapters with the kernel, or that runs inside a kernel handler. Covers handler structure, resource handles, scope discipline, ordering, Adapters, dependencies, and which file in a contract root or …impl a declaration goes in."
applyTo: "**/*.go"
---

# Kernel Usage

`kernel/README.md` documents the API. These are the rules for using it correctly.
Follow them in new and changed code without expanding a focused task into
unrelated cleanup. Which package a declaration belongs in — contract root,
`…impl` or `internal/` — is [`architecture.instructions.md`](architecture.instructions.md);
this file assumes it.

## Handler Structure

A command or subscription is a factory returning `(Lock, Execute)` or
`(Lock, Observe)`. The factory runs **once, at registration**. Its returned
closures are cached and reused for every invocation.

```go
type LoadCmd kernel.Command[LoadRequest, LoadResponse]

func loadCmdImpl() (kernel.Lock, kernel.Execute[LoadRequest, LoadResponse]) {
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
type StepOnUpdate kernel.Subscription[app.UpdateEvent]
```

### Naming A Command

A command is three declarations plus one handler, and the names are mechanical:

```go
type LoadCmd kernel.Command[LoadRequest, LoadResponse]
type LoadRequest struct{ Name string }
type LoadResponse struct{ Data []byte }

func loadCmdImpl() (kernel.Lock, kernel.Execute[LoadRequest, LoadResponse])
```

- The **command type** carries the name and ends in `Cmd`. It is the contract
  other plugins dispatch, so export it unless nothing outside the package does.
- **Request and response** repeat the command's name without `Cmd` and spell
  `Request` and `Response` in full — never `Req`/`Resp`. Declare both even when
  empty, rather than putting a bare `struct{}` in the type parameters: a named
  empty response stays nameable at the call site and can grow a field later
  without touching every handler.
- The **handler is always private** and is the command type's name plus `Impl`.
  The suffix is what lets a package-private command such as `initializeCmd` have
  a handler that does not collide with it.

Register handlers by name; never inline the factory into `HandleCommand`:

```go
registrar.HandleCommand[LoadCmd](loadCmdImpl)
```

The command type is declared in the contract root and its handler in the
`…impl`, which registers it from its own `Register`, directly or through an
unexported `registerCommands(registrar)` as `storageimpl` does. The handler stays
private to the `…impl`: callers dispatch the command type and never name the
handler.

A test fixture that registers a different body per test case is the one place an
inline factory is right — there is no single implementation to name. A fixture
command with one body follows the rule like any other.

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

### Keep `Lock` Deterministic and Total

`Lock` binds handles and nothing else, and it runs **once**, during
registration: `Subscribe` and `HandleCommand` call it immediately and keep the
lock set it produces for the engine lifetime. Three guarantees are what the rule
is about.

- **Deterministic.** The set is fixed by the end of registration and never
  changes. It may depend on types and on registration-time configuration; it
  must not depend on anything observable only while the engine runs — no clock,
  no resource value, no entity count, no global mutable state.
- **Total.** Every handle the handler body can reach is bound here. There is no
  runtime guard on `Get`/`Set`, so a lock the body takes that `Lock` did not
  declare is a silent data race, not an error.
- **Final.** No lock is acquired after registration. A handler never widens its
  own set; it declares what it dispatches with `Uses` and composition folds
  those locks in.

**Hand-written `Lock` bodies stay straight-line** — no branching, no loops, no
work — because that is the cheapest way to satisfy all three and the only one a
reviewer can check by eye. Conditional logic belongs in the body, where early
returns are fine.

**A generated `Lock` may loop**, and the `ecs` handler builder does: it walks a
System's parameter list and declares one lock per Component named there. That
satisfies all three — deterministic, because the parameter list is fixed by the
System's Go signature; total, because the body can reach nothing its parameters
do not name; final, because the walk happens inside the single registration-time
call. What violates the rule is a loop whose **extent is not fixed by types**:
over a slice a later run could size differently, over the entities alive at
registration, or over anything read from the world.

A generated `Lock` must be able to point at the type-level function that
produces its set. If you cannot name that function, the loop is not generated,
it is conditional. `ecs.ToHandler` is the only sanctioned user of this
exemption; see [`kernel/docs/specs/ecs-support.md`](../../kernel/docs/specs/ecs-support.md).

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

- store a value obtained from `Get()` in a plugin struct, a package variable, an
  Adapter, or any object that outlives the handler;
- read or write a handle from a goroutine the handler starts;
- return a resource, an `fs.FS`, an open file, or any live handle from a command
  response;
- keep using a value across a call that may release the lock.

```go
// WRONG: the plugin outlives the handler; the value is unsynchronized after it returns.
func (p *plugin) subscribe() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
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
use a lock-scoped callback command, whose handler calls `Use` under its own
locks:

```go
useCache(k, UseCacheRequest{Use: func(cache *Cache) error {
	// Read and write the cache before this callback returns.
	return nil
}})
```

Handing a value to a helper **called synchronously within the handler** is fine;
that is ordinary parameter passing. The rule is about outliving the scope.

`Registrar.Dependency[T]` is the one registration-time read. Use it only for a
value you register against — the `ecs` authority is the case it exists for — and
keep the result only when it is a pointer its owner never replaces. A `Lock` has
no such read: anything it or the handler body reaches goes through a handle it
binds. A builder that needs a dependency's value to plan a `Lock` takes the
`Registrar`, as `ecs.ToHandler` does.

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
func (p *plugin) Run(k kernel.Executioner) error {
	p.gpu.OnUpdate(func(dt float64) { p.onUpdate(k, dt) })
	return p.gpu.Run()
}
```

## Commands And Dispatch

A handler dispatches a command only through a dispatcher it declared in its
`Lock`:

```go
var setDesiredViewport func(kernel.Kernel, gfx.SetDesiredViewportRequest) (gfx.SetDesiredViewportResponse, error)
return func(access kernel.ResourceAccess) {
		setDesiredViewport = access.Uses[gfx.SetDesiredViewportCmd]()
	}, func(k kernel.Kernel, _ app.WindowSizeChangeEvent) error {
		_, err := setDesiredViewport(k, gfx.SetDesiredViewportRequest{Width: 100, Height: 100})
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

### Ordering Identities

Name and place a subscription identity as `architecture.instructions.md`
§ Ordering Identities says: verb plus event, exported from the contract root when
another package orders against it. The `…impl` subscribes the root's identity,
and everyone else orders against it through contract alone:

```go
// bundles/canvas: the contract root
type FlushOnUpdate kernel.Subscription[app.UpdateEvent]

// bundles/canvas/canvasimpl
registrar.Subscribe[canvas.FlushOnUpdate](p.flush).Last().Before[gfx.PresentOnUpdate]()

// any other plugin, ordering against it through contract alone
registrar.Subscribe[recordOnUpdate](record).Before[canvas.FlushOnUpdate]()
```

An identity nothing outside orders against stays unexported in the `…impl`
(`canvasimpl`'s `drawsOnUpdate`).

## Adapters

An Adapter is a plain value implementing an interface a Port declares in its
contract root (`gfx.Backend`, `storage.PermanentFS`, `mcp.Provider`), contributed
by a plugin and bound to the Port during composition.
[`kernel/docs/specs/ports.md`](../../kernel/docs/specs/ports.md) has the full
rules.

**Requiring or collecting** makes a plugin a Port, so it happens only in an
`extensions/P/Pimpl`. Declare in `Register` and keep the handle on the plugin —
a handle, unlike a resource value, is safe to keep — then read it from `Start`
onwards; `Get` panics before composition binds it:

```go
p.backend = registrar.RequireAdapter[gfx.Backend]()       // exactly one
p.providers = registrar.CollectAdapters[mcp.Provider]()   // any number, zero included
```

**Providing** is open to any plugin, a Bundle included. Always spell the
interface as the type argument; inferred from the value, it is the concrete type,
which panics:

```go
registrar.ProvideAdapter[storage.PermanentFS](permanent)
registrar.ProvideAdapter[mcp.Provider](provider{})
```

- **The value exists by `Register`.** Something that becomes usable later says
  so through the interface: wgpu provides one stable `gfx.Backend` at `Register`
  and reports `Ready()` once its device arrives.
- **An Adapter takes no lock.** Reading it is not a resource access, so which
  goroutines may call it is the interface's contract, and it holds no resource
  value (see above).
- **Binding adds no dependency** in either direction. A contributor that also
  uses the Port's commands or resources declares that dependency itself.
- **Composition checks the count.** A required interface with no Adapter fails
  with `ErrMissingAdapter` and with several `ErrDuplicateAdapter`. Providing an
  untyped nil fails with `ErrNilAdapter`, so a Port needs no nil check; a typed
  nil pointer is a legal Adapter. A contribution nobody consumes is fine, which
  is why every plugin with capabilities provides its `mcp.Provider`
  unconditionally.
- **A test composing a Port composes an Adapter too**: a small fixture plugin
  whose `Register` provides it (the `backendAdapter` in the canvas, scene,
  ecsscene and ui tests).

A plugin's mcp capabilities are an unexported `provider{}` in its own package's
`mcpprovider.go` (the `…impl`, for a Bundle or Port), contributed from its
`Register`.

## Dependencies

Declare in `Dependencies` every plugin whose contracts you use, named by its
contract root's `Name`:

```go
func (p *plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{gfx.Name, storage.Name}
}
```

This is enforced: composition fails with `ErrUndeclaredDependency` if a handler
locks a resource owned by a plugin that is not the owner itself or a transitive
declared dependency. Registration order does not affect resource binding, but
dependencies still order `Register` and `Start`. Test plugins are held to the
same rule, so a fixture that locks a real plugin's resource must declare that
plugin.

## Package File Layout

Place a declaration by **what it is**, not by the feature it belongs to, in the
package its kind puts it in. Use these filenames consistently.

In a **contract root** (`bundles/X`, `extensions/P`, `slots/*`):

- `contract.go`: package documentation and shared public value types.
- `identities.go`: `Name` and the exported ordering identities, or
  `contract.go` beside the package documentation (canvas, scene and ui keep
  them there).
- `commands.go`: command, request, and response declarations only.
- `events.go`: event declarations.
- `resources.go`: the documented Resource types: an alias of the `internal/`
  declaration (`type State = internal.State`) when the `…impl` reads its
  unexported state, as every Resource queue's consume side does.
- `err.go`: exported error types and their `Error` methods.

In the **`…impl`**:

- `doc.go`: package documentation: what it holds, that only composition roots
  and tests import it, and which Adapters it requires, collects or provides.
- `plugin.go`: the unexported `plugin`, `New`, `Name`, `Dependencies`,
  `Register`, and compact subscription wiring.
- `commandsimpl.go`: command registration and every command handler.
- `config.go`: `Config` and resolving it, a zero field taking its default.
- `mcpprovider.go`: the unexported `provider` and its capabilities.

In **`internal/`**: `doc.go`, `friends.go` for the plain functions giving the
root and the `…impl` what exported methods do not, and a file per declared type
or family, named for it (`state.go`, `opqueue.go`). A root file wrapping those
declarations takes the same name (`scene/camera.go` over `internal/camera.go`).
A Resource only the `…impl` touches stays unexported in the `…impl`, as
`uiimpl`'s `processor` does.

Never add a feature-named catch-all such as `viewport.go` holding a resource, its
events, and its commands together. Split it: the resource goes to `resources.go`,
the events to `events.go`, the commands to `commands.go`, and any shared enum or
value type to `contract.go`.

Command handlers always live in `commandsimpl.go`, however small the package, so
`commands.go` stays a readable list of the contract and never mixes declaration
with implementation. Subscription handlers may stay in `plugin.go` while the
wiring is compact; split those by ownership, not by an arbitrary size threshold.

A package takes only the files it needs. An Open slot such as `app` declares
commands and events other plugins implement and has `commands.go` and
`events.go`, but no `resources.go`, since it declares no Resources. An Extension
such as `wgpu`, which implements `app.QuitCmd` but declares no command of its
own, has `commandsimpl.go` alone.

## Validation

After changing kernel-facing code, build the narrowest package that includes the
change, then `go build ./...`. Registration errors surface at startup, not at
compile time, so run the affected target or its tests before considering the
change done.
