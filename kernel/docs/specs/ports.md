# Ports and Adapters — specification

A **Port** is a declared identity type naming an interface some plugin needs
filled. An **Adapter** is a declared identity type naming one way of filling a
Port, and the plain value a plugin provides under it during registration, bound
by the engine during composition. The vocabulary is in
[`CONTEXT.md`](../../../CONTEXT.md); the decision to have Ports at all is
[ADR 0001](../../../docs/adr/0001-bundles-slots-ports-and-adapters.md).

This is how the platform varies beneath a piece of engine functionality without
that functionality being replaced: gfx requires exactly one GPU backend, storage
requires exactly one permanent filesystem, and the mcp broker collects every
Provider.

## Declaring Ports and Adapters

Ports and Adapters are declared like commands: a defined type built from a
kernel shape, and that type is the identity.

```go
type RequiredPort[I any]  = func(requiredPort) I   // exactly one Adapter
type CollectedPort[I any] = func(collectedPort) I  // any number, zero included
type Adapter[P any]       = func(adapterOf) P      // one Adapter for the Port P
```

The shapes are never called. They carry the interface and the kind, so the
compiler can read both back from the declared type. A plugin declares the Ports
it offers, and the plugin filling a Port declares its Adapter:

```go
// gfx
type BackendPort kernel.RequiredPort[gpu.Backend]

// mcp
type ProviderPort kernel.CollectedPort[Provider]

// wgpu
type GfxBackend kernel.Adapter[gfx.BackendPort]
type McpProvider kernel.Adapter[mcp.ProviderPort]
```

## Registrar calls

Three registration-time calls on `Registrar`:

```go
func (r *Registrar) RequireAdapter[P RequiredPortConstraint[I], I any]() RequiredAdapter[I]
func (r *Registrar) CollectAdapters[P CollectedPortConstraint[I], I any]() CollectedAdapters[I]
func (r *Registrar) ProvideAdapter[A AdapterConstraint[P], P portConstraint[K, I], K portKind, I any](adapter I)
```

Only the first type argument is written; the rest are inferred from it:

```go
p.backend = registrar.RequireAdapter[gfx.BackendPort]()          // in gfx
p.providers = registrar.CollectAdapters[mcp.ProviderPort]()      // in the mcp broker
registrar.ProvideAdapter[GfxBackend](gpu.Backend(p.gfxBackend))  // in wgpu
```

- **`RequireAdapter[P]`** declares that the calling plugin needs exactly one
  Adapter for the required Port `P`. The handle's `Get()` returns it, typed as
  `P`'s interface. A collected Port does not compile here.
- **`CollectAdapters[P]`** declares that the calling plugin takes any number of
  Adapters for the collected Port `P`, zero included. The handle's `Get()`
  returns every one as a `ContributedAdapter[I]`: the Adapter and the
  `PluginName` of the plugin that contributed it, in plugin order. A required
  Port does not compile here.
- **`ProvideAdapter[A]`** contributes `adapter` as the Adapter `A`, to the Port
  `A` is built on. `adapter` has that Port's interface type, so the compiler
  checks the value implements it. A Port type, or any type not built from
  `Adapter`, does not compile here.

**A concrete value is converted to the interface.** Go infers a call's type
parameters from its arguments before it reads their constraints, so an argument
of a concrete type would be taken as the interface and then contradict the
Port. Pass a value already typed as the interface, or convert it:
`ProvideAdapter[McpProvider](mcp.Provider(provider{}))`. A value that does not
implement the interface fails that conversion at compile time.

The plugin that requires or collects `P` **declares** it; a plugin that provides
an Adapter for `P` is a **contributor**.

```go
type RequiredAdapter[I any] struct{ /* … */ }
func (h RequiredAdapter[I]) Get() I

type CollectedAdapters[I any] struct{ /* … */ }
func (h CollectedAdapters[I]) Get() []ContributedAdapter[I]

type ContributedAdapter[I any] struct {
    Plugin  PluginName
    Adapter I
}
```

## Rules

1. **A Port is built on an interface type.** A Port built on any other type
   panics at registration, from any of the three calls. The plugin boundary
   reports that as `ErrPluginPanic` naming the plugin.
2. **A binding is keyed by the Port type**, never by the interface. Two Ports on
   the same interface are distinct, and an Adapter binds only to the Port its
   type names.
3. **Binding happens at finalization**: after every plugin's `Register`, before
   any `Start`. Adapters bind regardless of the order plugins register in, so a
   declaring plugin may register before or after its contributors.
4. **Binding adds no plugin-dependency edge**, in either direction. A declaring
   plugin does not depend on its contributors, nor they on it; `Dependencies`,
   plugin order and the coupling check are exactly what they would be without
   the declarations.
5. **A handle is valid from `Start` onwards.** `Get` panics before
   finalization — from inside `Register`, say — and on a zero handle.
6. **An Adapter is a plain value, not a Resource.** Reading it takes no lock and
   it appears in no lock set. Whether it may be called concurrently, and from
   where, is the Port interface's business. `Get` itself only reads what
   composition wrote, so any goroutine may call it once `Run` begins.
7. **A required Port with no Adapter** fails composition with
   `ErrMissingAdapter`.
8. **A required Port with two or more Adapters** fails composition with
   `ErrDuplicateAdapter`, naming every Adapter type and its contributor.
9. **A nil Adapter is refused when it is provided.** `ProvideAdapter[A]` given
   an untyped nil records `ErrNilAdapter`, naming the providing plugin and `A`,
   and contributes nothing, so no plugin ever binds a nil. The rule is the same
   whether the Port is required, collected or declared by no plugin at all. If
   the nil was a required Port's only contribution, that declaration also
   reports `ErrMissingAdapter`. A **typed nil** — a nil pointer, map, func, chan
   or slice stored in the interface — is a valid interface value and is not nil:
   it binds like any other Adapter.
10. **A plugin declares each Port once.** Declaring the same Port twice fails
    composition with `ErrDuplicateRegistration` of kind `port declaration`.
    Requiring a collected Port, or collecting a required one, does not compile.
11. **An Adapter nobody requires or collects is not an error.** A Bundle may
    contribute an mcp Provider to an engine composed without mcp.
12. **Collected Adapters come in plugin order**: the order plugins register in,
    which is dependency order. Several Adapters from one plugin keep the order
    that plugin provided them in. `Get` returns a fresh slice each call.
13. **Nothing else is restricted.** Two plugins may each declare the same Port,
    and each binding is checked on its own. A plugin may contribute an Adapter to
    a Port it collects itself, as the mcp broker does for its own capabilities.

## Errors

```go
type ErrMissingAdapter struct {
    Plugin PluginName   // the plugin requiring the Port
    Port   reflect.Type
}

type ErrDuplicateAdapter struct {
    Plugin   PluginName   // the plugin requiring the Port
    Port     reflect.Type
    Adapters []AdapterDescription // in plugin order
}

type ErrNilAdapter struct {
    Plugin  PluginName   // the plugin that provided the nil
    Adapter reflect.Type
}
```

All join the other finalization errors, so one composition reports every
unbound Port and every nil Adapter at once. Messages name types through
`TypeName`:

```
plugin "gfx" requires an adapter for gfx.BackendPort, but no plugin provides one
plugin "gfx" requires exactly one adapter for gfx.BackendPort, but several are provided: [wgpu.GfxBackend (wgpu), headless.gfxBackendAdapter (headlessbackend)]
plugin "diskfs" provides a nil diskfs.StoragePermanentFS
```

## Description

`ArchitectureDescription.Ports` lists every declaration, sorted by Port type
and then by declaring plugin:

```go
type PortDescription struct {
    Type      reflect.Type // the Port type
    Interface reflect.Type // the interface it is built on
    Owner     PluginName   // the plugin that declared it
    Collects  bool         // false: requires exactly one
    Adapters  []AdapterDescription // in plugin order
}

type AdapterDescription struct {
    Type   reflect.Type // the Adapter type
    Plugin PluginName   // the plugin that provided it
}
```

`Dump` renders it as a `ports:` section after `resources:`:

```
ports:
  gfx.BackendPort (gfx) requires [wgpu.GfxBackend (wgpu)]
  mcp.ProviderPort (mcpserver) collects [input.McpProvider (input), mcp.McpProvider (mcpserver)]
```

An Adapter nobody consumes is not listed, because it binds to nothing.
