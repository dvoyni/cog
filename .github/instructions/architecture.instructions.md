---
name: "Plugin Kinds"
description: "Use when creating, moving or changing Go code in cog: which plugin kind a package is, which directory it belongs in, what it may import, and where ordering identities are declared."
applyTo: "**/*.go"
---

# Plugin Kinds

Every package in cog is exactly one kind, and its kind fixes both its directory
and what it may import. [`docs/adr/0001-bundles-slots-ports-and-adapters.md`](../../docs/adr/0001-bundles-slots-ports-and-adapters.md)
records why; [`CONTEXT.md`](../../CONTEXT.md) defines each term. The import
rules are enforced by `archtest/tiers_test.go`, not by review.

## The Kinds

| Kind | Where | What it is |
| --- | --- | --- |
| the kernel package | `kernel` | The Engine, Registrar and scheduler every Plugin is built on. |
| **Library** | `libs/<name>` | Code that is not a Plugin and defines none (`libs/m`). |
| **Open slot** | `slots/<name>` | A Slot shipped without an Extension (`slots/app`). It declares no Resources. |
| **Bundle** | `bundles/X` | A Slot and its one Extension, shipped together and self-contained (input, anim, canvas, scene, ui, ecs, ecsscene). |
| **Port** | `extensions/P` | A Plugin shipping its own contract and implementation that works only once an Adapter is bound to it: gfx and storage require exactly one, mcp collects any number. |
| **Adapter** or Extension of an Open slot | `extensions/<name>` | A Plugin implementing a contract it does not own: wgpu (Extension of `app`, Adapter of gfx), diskfs and jsfs (Adapters of storage). |

A Bundle and a Port share one shape:

- **Contract root**, `bundles/X` or `extensions/P`: the Slot. Commands, events,
  Resource types, `Name`, and the ordering identities others order against. It
  may act on a `*kernel.Registrar` it is handed, and it declares no type that
  implements `kernel.Plugin`.
- **`Ximpl`** / **`Pimpl`**: the unexported Plugin, its handlers, and any
  Adapter it contributes. It exports `func New() kernel.Plugin`, plus `Config`
  when the Plugin has real configuration (keyed by the root's `Name`; a zero
  field takes its default), and nothing else. The Bundles are the worked
  examples; `gfximpl`, `storageimpl` and `mcpimpl` still export their Plugin
  type from before this settled.
- **`internal/…`**: code the root and the `…impl` share, such as the consume
  side of a Resource queue. A root with nothing to hide has none (ecsscene,
  mcp).

A contract type whose unexported state the `…impl` reads is declared in
`internal/` with its fields unexported, and the root re-exports it as an alias
plus a wrapper per constructor (`type MeshDescr = internal.MeshDescr`). Its
methods, the recording methods included, live in `internal/`. It stays a
concrete type, so nothing goes through an interface. What the root and the
`…impl` need beyond its exported methods, `internal/` exports as plain
functions (`internal.OpQueueOps(q)`); only they can import `internal/`, so none
of it is public API. `internal/` never imports its own root, so anything such a
type refers to, down to the enums in its fields, is declared there too. Types
nothing outside the root reads the insides of stay declared in the root.
`extensions/gfx` is the worked example.

## Placing New Code

Take the first answer that fits:

1. It defines no Plugin and no Slot → a **Library** in `libs/`.
2. It is a contract whichever Extension an engine is composed with fills → an
   **Open slot** in `slots/`.
3. It implements a contract some other package owns, and ships none of its own
   → a directory in `extensions/` with no `…impl` child.
4. It needs something supplied from outside, an Adapter, before it works → a
   **Port**: `extensions/P`, `extensions/P/Pimpl`, `extensions/P/internal/…`.
5. Otherwise it is a **Bundle**: `bundles/X`, `bundles/X/Ximpl`,
   `bundles/X/internal/…`.

New code goes in the root, the `…impl` or `internal/` by what it is: contract in
the root, the Plugin and its handlers in the `…impl`, anything both need in
`internal/`. A subpackage anywhere else under `bundles/X` or `extensions/P`
matches no tier and fails the test.

## The `extensions/` Rule

Every Plugin that is not a Bundle lives in `extensions/`. A directory there with
an `…impl` child is a Port, and its root is contract any package may import. Any
other directory there (wgpu, diskfs, jsfs) is imported only by composition roots
and tests, and so is every `…impl`, in `bundles/` and `extensions/` alike.

## Import Rules

| package | may import (plus std and third-party) |
| --- | --- |
| `kernel` | nothing else in cog |
| `libs/*` | `libs`, `kernel` |
| `slots/*` | `libs`, `kernel`, `slots/*`, contract roots |
| contract root: `bundles/X`, or `extensions/P` when `P` has a `Pimpl` child | `libs`, `kernel`, `slots/*`, other contract roots, its own `internal/…` |
| `bundles/X/internal/…`, `extensions/P/internal/…` | `libs`, `kernel`, `slots/*`, other contract roots |
| `bundles/X/Ximpl`, `extensions/P/Pimpl` | anything its contract root may, plus that root |
| other `extensions/*` (wgpu, diskfs, jsfs) | `libs`, `kernel`, `slots/*`, contract roots |

- Nothing in cog imports an `…impl` package or an `extensions/*` directory that
  is not a Port, except from `_test.go` files. A test composing an engine may
  import both; every other row holds for tests too.
- Contract roots and `slots/*` declare no type that implements `kernel.Plugin`,
  meaning no type with `Name`, `Dependencies` and `Register` methods.

A plugin reaches another through its contract root only: never through its
`…impl`, and never by naming its Plugin.

## Ordering Identities

An ordering identity is the subscription identity type a subscriber is ordered
against with `Before`/`After`. Name it verb plus event, for what the handler does
on which event:

```go
type PresentOnUpdate kernel.Subscription[app.UpdateEvent]
```

Declare an identity another package orders against in the contract root, next to
`Name`, so ordering against it imports contract and nothing else
(`gfx.PresentOnUpdate`, `canvas.FlushOnUpdate`). An identity nothing outside its
package orders against is unexported, and stays in the package that subscribes it.

## Composition Roots Are Exempt

Games and examples (cog-examples, feuds-26, nox) are composition roots. They pick
the Plugins and Adapters an engine is built from, so they import whatever they
compose, `…impl` and Adapters included. These rules apply to the cog repo only.
Inside cog, `archtest` and `docs/research/**` are outside the tiers.

## The Tier Test

`go test ./archtest` checks every cog-internal import edge in every Go file,
whatever its build tags, and every contract root and slot for a Plugin type. A
failure names the file, the edge and the rule it breaks, and every violation
fails the test: fix the code to fit the rules. A change to the rules themselves
changes this file and `archtest/tiers_test.go` together.
