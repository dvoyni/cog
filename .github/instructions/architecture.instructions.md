---
name: "Plugin Kinds"
description: "Use when creating, moving or changing Go code in cog: which kind a plugin is, which directory and package a declaration belongs in, what a root may declare, what each package may import, and where ordering identities are declared."
applyTo: "**/*.go"
---

# Plugin Kinds

Every plugin in cog is exactly one kind. Its kind fixes its directory, and every
plugin has the same package shape. [`docs/adr/0002-slots-extensions-and-bundles-as-declaration-roots.md`](../../docs/adr/0002-slots-extensions-and-bundles-as-declaration-roots.md)
records the kinds, and [`docs/adr/0003-roots-are-alias-indexes.md`](../../docs/adr/0003-roots-are-alias-indexes.md)
the root's shape; [`CONTEXT.md`](../../CONTEXT.md) defines each term. The kinds, the
shape and the import rules are enforced by `kernel/archtest`, not by review.

Everything here is the rule for every plugin, new ones included.

## The Kinds

| Kind | Where | What it is |
| --- | --- | --- |
| the kernel package | `kernel` | The Engine, Registrar and scheduler every Plugin is built on. |
| **Library** | `libs/<name>` | Code that is not a Plugin and defines none (`libs/m`). |
| **Slot** | `slots/<name>` | A Plugin whose root declares at least one required Port (app, gfx, storage). Composition fails until an Adapter fills it. |
| **Extension** | `extensions/<name>` | A Plugin that provides Adapters, at least one of them for a Slot's required Port, and declares no API (gogpu, diskstorage, jsstorage). It may also contribute to a collected Port. |
| **Bundle** | `bundles/<name>` | Every other Plugin (input, anim, canvas, scene, ui, ecs, mcp). Its root declares no required Port, and it may collect Adapters or contribute them. |

A plugin that would need both a Slot's Adapters and an API of its own is two
plugins: an Extension and a Bundle.

## The Package Shape

A plugin `X` is four places, and nothing else under `X` may hold Go code.

**Every plugin has an alias-index root (ADR 0003) but mcp**, which still has
ADR 0002's declaration root, described after it, until its capability builders
find a home that keeps the MCP SDK out of the plugins that declare tools.
`kernel/archtest`'s `aliasIndexRoots` lists the moved plugins. Every new
plugin takes the alias-index shape.

**The alias-index shape (ADR 0003):**

- **The root, `X/`, declares nothing: it is an index of aliases** of what its
  own `internal/` and `internal/types` declare, each under the target's own
  name — `type Mesh = internal.Mesh`, `const Name = internal.Name`,
  `var ErrNoArea = types.ErrNoArea` — plus the forwarders in `utils.go` and
  inline anchors. It keeps the file names below, so a reader opens
  `components.go` for what an Entity carries and `commands.go` for what the
  plugin answers. Each alias repeats its declaration's doc comment, because the
  root is what a game reads.
- **`X/internal/`** declares everything the root offers — `Name`, the ordering
  identities, commands, events, Adapters, Components, errors, `Config` — in
  files named as the root's are (`id.go`, `components.go`, `err.go`), beside
  the implementation. It registers under those names directly. **It never
  imports its own root**; nothing does but the root's importers.
- **`X/internal/types/`** is optional, and **holds data only**: POD structs
  with exported fields, enums, consts and errors — no function, and no method
  but `Error` and `String`. Keep one when a plugin has plenty of such data
  another package of its own needs apart from the logic; otherwise there is
  none, as in every moved plugin today. Logic, and any type with methods, is
  declared in `internal/`. It never imports its own root either.
- **`X/internal/<part>/`** sub-packages hold the separable parts of a crowded
  `internal/` — a part few other files reach into, such as gfx's shader
  vocabulary and preprocessor in `internal/shader`. They are implementation,
  so methods and logic are welcome; `internal/` imports them, they never import
  `internal/` or the root, and the root aliases and forwards into them exactly
  as into `internal/`, keeping each target's name.
- **A test that composes plugins depending on this one** cannot be a test of
  `internal/` itself: their roots import this root, which imports `internal/`.
  It is an external test, `package internal_test`, beside the others, reaching
  what it needs through an `export_test.go` (app's `pairing_test.go`).
- An alias exposes every exported method of the type it names, so a method on
  a type the root aliases is public API.

**The declaration shape (ADR 0002), mcp's alone:**

- **The root, `X/`**, holds declarations only: what the plugin offers others.
- **`X/internal/types/`** holds the concrete types the root aliases, their
  methods, and the plain functions the implementation needs to read their
  unexported state. A declaration goes there for one of two reasons only:
  - **performance**: a concrete type where an interface in the root would cost
    it, as the recording queues and hot state do;
  - **`internal/types` code names it**: `internal/types` never imports its own
    root, so a type or command its code needs, a forwarder's body included, is
    declared there and aliased in the root. input declares `SynthesizeCmd` and
    `Action` there because `types.Play`, which `input.Play` forwards to,
    dispatches the command.

  Anything else such code refers to comes from another plugin's root.
- **`X/internal/`** holds the implementation: the unexported plugin, its `New`,
  handlers, subscriptions, Adapter values and mcp provider. No file layout is
  enforced inside it.
- **The constructor package, `X/Xplugin/`** (`appplugin`, `canvasplugin`), is
  one file whose only export is `func New() kernel.Plugin`, returning
  `internal.New()`.

A plugin uses another plugin through its root only.

### What A Root Holds

Its non-test Go files come from its kind's allowlist. Non-Go files and `docs/`
stay where they are.

| Kind | Allowed files |
| --- | --- |
| Slot, Bundle | `doc.go`, `id.go`, `commands.go`, `events.go`, `resources.go`, `ports.go`, `adapters.go`, `components.go`, `types.go`, `config.go`, `err.go`, `utils.go` |
| Extension | `doc.go`, `id.go`, `config.go`, `adapters.go`, `err.go` |

- **Data-driven declarations.** Types with exported fields, and no getters or
  setters.
- **Aliases** go in the file matching what the aliased type is: a resource alias
  in `resources.go`, a Component in `components.go`, a value type in `types.go`
  (`type OpQueue = types.OpQueue`).
- **`Config`** is plain data whose zero value is the default. It may have
  builder methods (`WithValuesPath`), the only logic a root holds outside
  `utils.go` besides an inline anchor. There is no `DefaultConfig`.
- **Functions** appear only in `utils.go`, and each one is a pure forwarder: a
  single call into the plugin's own `internal/types`, returned when the
  forwarder has results, with its parameters passed through in order. A
  generic forwarder passes its type parameters through the same way:

  ```go
  func GetValue[T any](key string, defaultValue T, outValue *T) AccessValuesRequest {
  	return types.GetValue[T](key, defaultValue, outValue)
  }
  ```

- **An inline anchor** is the one code exception a root may hold outside
  `Config`'s builders and `utils.go`: an unexported function nothing calls,
  returning nothing, whose parameters are typed with the root's own aliases of
  `internal/types` types and whose every statement calls an argument-free
  method on one of them, discarding the results. It exists for the compiler.
  Go inlines a method of a package the caller does not import only when a
  package it does import references that method, and nothing outside a plugin
  imports its `internal/types`, so an accessor called per instance through a
  root alias stops inlining in importers unless the root references it. Anchor
  exactly the accessors a hot importer calls, say why in the function's
  comment, and confirm with `-gcflags=-m` before and after:

  ```go
  // inlineAnchor is never called; see architecture.instructions.md.
  func inlineAnchor(parameter ParameterDescr, format TextureFormat) {
  	_ = parameter.Name()
  	_, _ = parameter.ColorValue()
  	_ = format.Resolve()
  }
  ```

- **A Slot's forwarders** name only the Slot's own types, predeclared types,
  the standard library, Libraries and the kernel in their parameters and
  results, never another plugin's types. A Slot's API stays interface-like, so
  what fills it can change without its users changing.
- **No Plugin.** A root declares no type implementing `kernel.Plugin`, meaning
  no type with `Name`, `Dependencies` and `Register` methods.
- **No dispatch helpers.** A caller dispatches a command itself and handles its
  answer.
- **An Extension's root** declares only `Name`, `Config`, its Adapter types and
  `Err…` errors: no commands, events, resources, ports, types or forwarders.
  Its `Config` arrives, as every plugin's does, through `kernel.New`'s config
  map under `Name`, since its constructor takes nothing. An Extension may be
  the engine's `kernel.PluginHost`, as gogpu is: the host is the plugin value
  its constructor returns, found by the kernel, so nothing in the root names
  it. The platform main loop it runs is still an Adapter like any other:
  gogpu provides it as `AppMainLoop`, for app's `MainLoopPort`. An Extension
  built for one platform only (diskstorage is `!js`, jsstorage is `js`) tags
  its `internal/` implementation and its constructor package, and leaves its
  root untagged so the declarations build everywhere.
- **An Extension's name** takes the Slot it fills as its suffix when it fills
  Adapters for exactly one Slot: `diskstorage` and `jsstorage` both fill
  storage. An Extension that fills more than one Slot has no naming rule:
  gogpu, named for the library it wraps, fills both app and gfx. The tier test
  does not check names.

### Ports And Adapters

A Port is a type in the declaring plugin's `ports.go`; an Adapter is a type in
the providing plugin's `adapters.go`. [`kernel.instructions.md`](kernel.instructions.md)
§ Ports and Adapters has the spelling.

- A Slot's root declares at least one required Port. A Bundle's or an
  Extension's declares none, though a Bundle may declare a collected Port.
- Every `ProvideAdapter[A]` in a plugin names an `A` declared in that plugin's
  `adapters.go`, and every type declared there is provided. A call in a file
  only another platform builds counts.

## Placing New Code

Take the first answer that fits:

1. It defines no Plugin → a **Library** in `libs/`.
2. It cannot work until another plugin supplies an implementation → a **Slot**
   in `slots/`, whose `ports.go` declares that required Port.
3. It supplies implementations of Slots' Ports and offers nothing else → an
   **Extension** in `extensions/`.
4. Otherwise it is a **Bundle** in `bundles/`.

Within the plugin, a declaration another plugin uses goes in the root, in the
file its allowlist names for what it is. A type the root must alias, for
performance or because `internal/types` code names it, goes in
`internal/types`. Everything else goes in `internal/`.

## Import Rules

| package | may import (plus std and third-party) |
| --- | --- |
| `kernel` | nothing else in cog |
| `libs/*` | `libs`, `kernel` |
| root `X` | `libs`, `kernel`, other plugins' roots, its own `internal/types`; an alias-index root also its own `internal/` and the packages under it |
| `X/internal/types/…` | `libs`, `kernel`, other plugins' roots |
| `X/internal/…` | `libs`, `kernel`, any root — **but its own, in an alias-index plugin** — its own `internal/…` and `internal/types` |
| constructor `X/Xplugin` | `kernel`, its own `internal/` |

- Nothing in cog imports a constructor package or another plugin's `internal/`,
  except from `_test.go` files. A test composing an engine imports constructor
  packages; every other row holds for tests too.

## Ordering Identities

An ordering identity is the subscription identity type a subscriber is ordered
against with `Before`/`After`. Name it verb plus event, for what the handler does
on which event:

```go
type PresentOnUpdate kernel.Subscription[app.UpdateEvent]
```

Declare an identity another package orders against in the root's `id.go`, next
to `Name`, so ordering against it imports a root and nothing else
(`gfx.PresentOnUpdate`, `canvas.FlushOnUpdate`). An identity nothing outside its
plugin orders against is unexported, and stays in `internal/`.

## Composition Roots Are Exempt

Games and examples (cog-examples, feuds-26, nox) are composition roots. They pick
the Plugins an engine is built from, so they import whatever they compose,
constructor packages included. These rules apply to the cog repo only. Inside
cog, `kernel/archtest/**` and `docs/research/**` are outside the tiers.

## The Tier Test

`go test ./kernel/archtest` checks:

- every cog-internal import edge in every Go file, whatever its build tags;
- every root's files against its kind's allowlist;
- every root's functions against the forwarder and inline-anchor rules;
- every root for a Plugin type;
- every Slot for a required Port, and every Bundle and Extension for none;
- every Extension's declarations;
- every constructor package's exports;
- every plugin's `ProvideAdapter` calls against its `adapters.go`.

A failure names the file, the declaration or edge, and the rule it breaks, and
every violation fails the test: fix the code to fit the rules. A change to the
rules themselves changes this file and `kernel/archtest` together.
