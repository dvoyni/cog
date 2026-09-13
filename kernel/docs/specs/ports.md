# Ports and Adapters — specification

A **Port** is a plugin that works only once it is given an **Adapter** for an
interface it declares. An Adapter is a plain value implementing that interface,
contributed by some plugin during registration and bound to the Port by the
engine during composition. The vocabulary is in [`CONTEXT.md`](../../../CONTEXT.md);
the decision to have Ports at all is
[ADR 0001](../../../docs/adr/0001-bundles-slots-ports-and-adapters.md).

This is how the platform varies beneath a piece of engine functionality without
that functionality being replaced: gfx requires exactly one GPU backend, storage
requires exactly one filesystem, and the mcp broker collects every Provider.

## Declarations

Three registration-time declarations on `Registrar`, each keyed by the Go type of
an interface:

```go
func (r *Registrar) RequireAdapter[T any]() RequiredAdapter[T]
func (r *Registrar) CollectAdapters[T any]() CollectedAdapters[T]
func (r *Registrar) ProvideAdapter[T any](adapter T)
```

```go
backend := registrar.RequireAdapter[gpu.Backend]()     // in gfx
providers := registrar.CollectAdapters[mcp.Provider]() // in the mcp broker
registrar.ProvideAdapter[gpu.Backend](device)          // in wgpu
```

- **`RequireAdapter[T]`** declares that the calling plugin needs exactly one
  Adapter for `T`. The handle's `Get()` returns it.
- **`CollectAdapters[T]`** declares that the calling plugin takes any number of
  Adapters for `T`, zero included. The handle's `Get()` returns every one as a
  `ContributedAdapter[T]`: the Adapter and the `PluginName` of the plugin that
  contributed it, in plugin order.
- **`ProvideAdapter[T]`** contributes `adapter` for `T`. Because `T` is spelled
  explicitly and `adapter` has type `T`, the compiler checks that the value
  implements the interface.

The plugin that requires or collects `T` is the **Port** for `T`; a plugin that
provides for `T` is a **contributor**.

```go
type RequiredAdapter[T any] struct{ /* … */ }
func (h RequiredAdapter[T]) Get() T

type CollectedAdapters[T any] struct{ /* … */ }
func (h CollectedAdapters[T]) Get() []ContributedAdapter[T]

type ContributedAdapter[T any] struct {
    Plugin  PluginName
    Adapter T
}
```

## Rules

1. **`T` is an interface type.** Any other type argument, to any of the three
   declarations, panics at registration. The plugin boundary reports that as
   `ErrPluginPanic` naming the plugin. This also catches
   `ProvideAdapter(device)` written without its type argument, which infers the
   concrete type.
2. **Binding happens at finalization**: after every plugin's `Register`, before
   any `Start`. Adapters bind regardless of the order plugins register in, so a
   Port may register before or after its contributors.
3. **Binding adds no plugin-dependency edge**, in either direction. A Port does
   not depend on its contributors, nor they on it; `Dependencies`, plugin order
   and the coupling check are exactly what they would be without the
   declarations.
4. **A handle is valid from `Start` onwards.** `Get` panics before
   finalization — from inside `Register`, say — and on a zero handle.
5. **An Adapter is a plain value, not a Resource.** Reading it takes no lock and
   it appears in no lock set. Whether it may be called concurrently, and from
   where, is the Port interface's business. `Get` itself only reads what
   composition wrote, so any goroutine may call it once `Run` begins.
6. **A required interface with no Adapter** fails composition with
   `ErrMissingAdapter`.
7. **A required interface with two or more Adapters** fails composition with
   `ErrDuplicateAdapter`, naming every contributor.
8. **A nil Adapter is refused when it is provided.** `ProvideAdapter[T]` given
   an untyped nil records `ErrNilAdapter`, naming the providing plugin and `T`,
   and contributes nothing, so no Port ever binds a nil. The rule is the same
   whether `T` is required, collected or declared by no plugin at all. If the nil
   was a required interface's only contribution, that Port also reports
   `ErrMissingAdapter`. A **typed nil** — a nil pointer, map, func, chan or slice
   stored in the interface — is a valid interface value and is not nil: it binds
   like any other Adapter.
9. **A plugin declares each interface once.** Requiring and collecting the same
   `T` from one plugin, or declaring the same `T` twice, fails composition with
   `ErrDuplicateRegistration` of kind `adapter declaration`.
10. **An Adapter nobody requires or collects is not an error.** A Bundle may
    contribute an mcp Provider to an engine composed without mcp.
11. **Collected Adapters come in plugin order**: the order plugins register in,
    which is dependency order. Several Adapters from one plugin keep the order
    that plugin provided them in. `Get` returns a fresh slice each call.
12. **Nothing else is restricted.** Two plugins may each declare the same `T`,
    and each binding is checked on its own. A Port may contribute an Adapter to
    an interface it collects itself, as the mcp broker does for its own
    capabilities.

## Errors

```go
type ErrMissingAdapter struct {
    Port      PluginName
    Interface reflect.Type
}

type ErrDuplicateAdapter struct {
    Port         PluginName
    Interface    reflect.Type
    Contributors []PluginName
}

type ErrNilAdapter struct {
    Plugin    PluginName // the plugin that provided the nil
    Interface reflect.Type
}
```

All join the other finalization errors, so one composition reports every
unbound Port and every nil Adapter at once.

## Description

`ArchitectureDescription.Ports` lists every declaration, sorted by interface and
then by Port:

```go
type PortDescription struct {
    Interface    reflect.Type
    Port         PluginName
    Collects     bool         // false: requires exactly one
    Contributors []PluginName // in plugin order
}
```

`Dump` renders it as a `ports:` section after `resources:`:

```
ports:
  gpu.Backend (gfx) requires [wgpu]
  mcp.Provider (mcpserver) collects [input mcpserver scene]
```

An Adapter nobody consumes is not listed, because it binds to nothing.
