---
name: "Kernel Usage"
description: "Use when creating or changing Go code that registers plugins, commands, subscriptions, resources, Ports or Adapters with the kernel, or that runs inside a kernel handler. Covers handler structure, resource handles, scope discipline, ordering, Ports and Adapters, dependencies, and which file in a plugin's root a declaration goes in."
applyTo: "**/*.go"
---

# Kernel Usage

`kernel/docs/README.md` documents the API. These are the rules for using it correctly.
Follow them in new and changed code without expanding a focused task into
unrelated cleanup. Which package a declaration belongs in — the root,
`internal/types`, `internal/` or the constructor package — and what a root may
hold is [`architecture.instructions.md`](architecture.instructions.md); this
file assumes it.

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

The command type is declared in the root's `commands.go` and its handler in
`internal/`, which registers it from the plugin's `Register`, directly or
through an unexported `registerCommands(registrar)`. The handler stays private
to `internal/`: callers dispatch the command type and never name the handler.

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
§ Ordering Identities says: verb plus event, exported from the root's `id.go`
when another package orders against it. `internal/` subscribes the root's
identity, and everyone else orders against it through the root alone:

```go
// bundles/canvas/id.go
type FlushOnUpdate kernel.Subscription[app.UpdateEvent]

// bundles/canvas/internal
registrar.Subscribe[canvas.FlushOnUpdate](p.flush).Last().Before[gfx.PresentOnUpdate]()

// any other plugin, ordering against it through the root alone
registrar.Subscribe[recordOnUpdate](record).Before[canvas.FlushOnUpdate]()
```

An identity nothing outside orders against stays unexported in `internal/`
(canvas's `drawsOnUpdate`).

## Ports and Adapters

A Port and an Adapter are declared identity types, like commands.
[`kernel/docs/specs/ports.md`](../../kernel/docs/specs/ports.md) has the full
rules.

**A Port** is a defined type built from `kernel.RequiredPort[I]` (exactly one
Adapter) or `kernel.CollectedPort[I]` (any number, zero included), where `I` is
the interface its Adapters implement. The plugin declaring it puts it, beside
that interface, in its root's `ports.go`. A required Port is what makes a plugin
a Slot, so only a Slot declares one:

```go
type BackendPort kernel.RequiredPort[Backend]         // gfx/ports.go
type MainLoopPort kernel.RequiredPort[MainLoop]       // app/ports.go
type PermanentFSPort kernel.RequiredPort[PermanentFS] // storage/ports.go
type ProviderPort kernel.CollectedPort[Provider]      // mcp/ports.go
```

**An Adapter** is a defined type built from `kernel.Adapter[P]`, where `P` is the
Port it fills, named for the Port's plugin and interface. The providing plugin
puts it in its root's `adapters.go`, and the tier test fails when a plugin
provides an Adapter `adapters.go` does not declare or declares one it never
provides:

```go
type AppMainLoop kernel.Adapter[app.MainLoopPort]        // gogpu/adapters.go
type GfxBackend kernel.Adapter[gfx.BackendPort]          // gogpu/adapters.go
type StoragePermanentFS kernel.Adapter[storage.PermanentFSPort] // diskstorage, jsstorage
type McpProvider kernel.Adapter[mcp.ProviderPort]        // every plugin with capabilities
```

**Requiring or collecting** a Port happens only in `internal/` of the plugin
that declares it. Declare in `Register` and keep the handle on the plugin — a
handle, unlike a resource value, is safe to keep — then read it from `Start`
onwards; `Get` panics before composition binds it:

```go
p.backend = registrar.RequireAdapter[gfx.BackendPort]()      // RequiredAdapter[gfx.Backend]
p.providers = registrar.CollectAdapters[mcp.ProviderPort]()  // CollectedAdapters[mcp.Provider]
```

**Providing** is open to any plugin, a Bundle included, and always names the
plugin's own Adapter type. The value has the Port's interface type, so the
compiler checks it; Go infers type arguments from the value before the
constraints, so convert a concrete value to the interface:

```go
registrar.ProvideAdapter[diskstorage.StoragePermanentFS](permanent) // already a storage.PermanentFS
registrar.ProvideAdapter[canvas.McpProvider](mcp.Provider(provider{}))
```

- **The value exists by `Register`.** Something that becomes usable later says
  so through the interface: gogpu provides one stable backend at `Register` and
  reports `Ready()` once its device arrives.
- **An Adapter takes no lock.** Reading it is not a resource access, so which
  goroutines may call it is the interface's contract, and it holds no resource
  value (see above).
- **Binding adds no dependency** in either direction. A contributor that also
  uses the declaring plugin's commands or resources declares that dependency
  itself.
- **Composition checks the count.** A required Port with no Adapter fails with
  `ErrMissingAdapter` and with several `ErrDuplicateAdapter`. Providing an
  untyped nil fails with `ErrNilAdapter`, so a plugin requiring a Port needs no
  nil check; a typed nil pointer is a legal Adapter. A contribution nobody
  consumes is fine, which is why every plugin with capabilities provides its
  `McpProvider` unconditionally.
- **A test composing a plugin that requires a Port composes an Adapter too**: a
  small fixture plugin whose `Register` provides it under a test-local Adapter
  type (the `backendAdapter` providing `testGfxBackend` in the canvas, scene,
  ecsscene and ui tests). A `_test.go` file is outside the `adapters.go` check.

A plugin's mcp capabilities are an unexported `provider{}` in its `internal/`,
contributed from its `Register`.

## Dependencies

Declare in `Dependencies` every plugin whose commands you dispatch or whose
resources you use, named by its root's `Name`:

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

A dependency on the plugin that handles a command is what guarantees the command
a handler, so a caller never has to read a missing one as an answer. A plugin
dispatching `app.TimeCmd` or `app.QuitCmd` declares `app.Name` (gfx, canvas and
ui do, for their snapshot capabilities). An event has no owner to depend on:
subscribing to `app.UpdateEvent` needs no dependency on app (input, anim and
scene subscribe without one), because an event nobody publishes is
simply never delivered, and subscribers are ordered with `First`, `Last`,
`Before` and `After`, not by dependencies.

## Package File Layout

Place a declaration by **what it is**, not by the feature it belongs to. Which
files a root may hold, by kind, is `architecture.instructions.md` § What A Root
Holds; this is what goes in each.

In a **root**:

- `doc.go`: package documentation: what the plugin offers, and which Ports it
  requires or collects and which Adapters it provides.
- `id.go`: `Name` and the exported ordering identities.
- `commands.go`: command, request, and response declarations only.
- `events.go`: event declarations.
- `resources.go`: the documented Resource types, or an alias of the
  `internal/types` declaration (`type State = types.State`) when the
  implementation reads its unexported state, as every Resource queue's consume
  side does.
- `ports.go`: the Port types the plugin declares, and the interface each
  carries.
- `adapters.go`: the Adapter types the plugin provides.
- `types.go`: the value types, enums and interfaces the declarations above
  name, and aliases of `internal/types` value types.
- `config.go`: `Config`, read from the config map under the root's `Name`, and
  its builder methods. Its zero value is the default.
- `err.go`: exported error types and their `Error` methods.
- `utils.go`: the forwarders into `internal/types`.

A root's only other code is an inline anchor beside the aliases it anchors,
`types.go` in gfx; architecture.instructions.md has the rule.

In **`internal/types`**: a file per declared type or family, named for it
(`state.go`, `opqueue.go`), and the plain functions giving `internal/` what
exported methods do not.

In **`internal/`** no layout is enforced. By convention: `plugin.go` for the unexported `plugin`, `New`, `Name`, `Dependencies`,
`Register` and compact subscription wiring; `commandsimpl.go` for command
registration and every command handler; `mcpprovider.go` for the unexported
`provider`, its capabilities and their unexported request and response types. A
Resource only the implementation touches stays unexported there.

The **constructor package** is a single file holding `New`.

Never add a feature-named catch-all such as `viewport.go` holding a resource, its
events, and its commands together. Split it: the resource goes to `resources.go`,
the events to `events.go`, the commands to `commands.go`, and any shared enum or
value type to `types.go`.

Command handlers never sit beside their commands: `commands.go` stays a readable
list of what the plugin offers. A root takes only the files it needs: an
Extension has no `commands.go` even when it handles another plugin's commands.

## Validation

After changing kernel-facing code, build the narrowest package that includes the
change, then `go build ./...`. Registration errors surface at startup, not at
compile time, so run the affected target or its tests before considering the
change done.
