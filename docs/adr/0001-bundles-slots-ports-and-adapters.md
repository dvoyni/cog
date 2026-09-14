---
status: superseded by ADR-0002
---

# Bundles, Slots, Ports and Adapters

> **Superseded** by [ADR 0002](0002-slots-extensions-and-bundles-as-declaration-roots.md). It changes what Slot, Extension and Bundle mean, retires Open slot and the Port's vocabulary package, and replaces the contract root, `…impl` and `internal/` with a declaration root, `internal/types`, `internal/` and a constructor package. This record stays as it was decided. Plugins on the tier test's migration list still follow it until they move.

Cog's top-level packages had mixed contract and implementation in one package, so a plugin that wanted another's contract imported its implementation as well. Plugins also named each other: through `Name` constants, through ordering identities declared next to handlers, and through `Executioner.Plugins[T]`. We sort every plugin into exactly one kind, each with its own directory and its own import rules, and enforce those rules with a test instead of prose:

- A **Bundle** is a Slot and its one Extension, shipped together and self-contained (`bundles/`: input, anim, canvas, scene, ui, ecs, ecsscene). Its root package is the slot. `Ximpl` holds `New` and `Config`, and `internal/` is the code the two share.
- An **Open slot** is a contract with no Extension of its own (`slots/app`). It declares no Resources.
- A **Port** is a plugin that ships its own contract and implementation but works only once an Adapter is bound to it: gfx and storage require exactly one, mcp collects any number. It uses the same root, `Ximpl` and `internal/` shape as a Bundle.
- An **Adapter** is contributed by a plugin, for example wgpu's GPU backend, `diskfs`, `jsfs`, or each plugin's mcp capabilities.
- A **Library** is code that defines no plugin (`libs/m`). It may import other Libraries and the kernel.

Every plugin that isn't a Bundle lives in `extensions/`. A directory there with an `…impl` child is a Port, and its root is contract anyone may import. Any other directory there (wgpu, diskfs, jsfs) is imported only by composition roots and tests. The same holds for every `…impl`.

The kernel gains `RequireAdapter[T]` and `CollectAdapters[T]` on the registrar, keyed by the interface type and bound at finalization. A missing or duplicate Adapter fails composition. `Executioner.Plugins[T]`, `SetBackendCmd` and `SetPermanentFSCmd` are deleted. So an Adapter must exist by `Register`, and a Port whose Adapter becomes usable later (wgpu's device arrives asynchronously) says so through readiness in its interface.

Ordering identities that another package orders against live in the contract root next to `Name`, named verb-plus-event: `gfx.PresentOnUpdate`, `gfx.RenderOnRender`, `canvas.FlushOnUpdate`, `scene.FlushOnUpdate`, `input.AdvanceOnUpdate`, `ui.ProcessOnUpdate`, `ecsscene.RecordOnUpdate`, `anim.AdvanceOnUpdate`. Identities nothing outside orders against become unexported.

This applies to the cog repo. Games and examples are composition roots and stay free-form.

## Considered Options

- **A separate slot and plugin directory for every concept.** Rejected. Six of nine concepts had exactly one implementation, so the seam would be hypothetical, and every change would touch two trees. Go's `internal/` only scopes friend code within one subtree.
- **gfx, input and storage as Open slots, because their implementation varies by platform.** Rejected. What varies beneath gfx and storage is an Adapter, not the whole Extension, and input's platform sources only *call* `ApplyCmd`. As Open slots they would need Resource ownership keyed by a slot-declared name, and every Resource type would become an interface.
- **Resources as interfaces, so a slot carries no implementation.** Rejected for Bundles and Ports. `canvas.OpQueue` and `scene.OpQueue` are called per sprite and per instance. Concrete types, with the consume side in `internal/`, cost nothing.
- **Ports as plugin interfaces (`PortPlugin{Port() PortId; SetAdapter(any)}`).** Rejected in favour of typed registrar declarations. Declarations already go through the registrar, and `any` would move a compile-time check to runtime.
- **"Module" for slot+extension.** Rejected. It collides with shader module and Go module. Taking "Bundle" renamed the ECS term: a Spawn now takes a Component set, spelled as a struct type with values.

## Consequences

- Every import path in cog, cog-examples and feuds-26 changes. Package names do not.
- `app.Viewport`, `SetViewportCmd` and `SetDesiredViewportCmd` move into gfx.
- A Bundle's contract root may act on a `*kernel.Registrar` it is handed (ecs registers Components and Systems that way). It never implements `kernel.Plugin`.
- A Bundle may contribute Adapters to a Port that collects them, which is how every mcp Provider stays inside its own package.

## Deferred (v2)

- Split gfx's backend contract (`Backend`, `GpuQueue`, the sinks and their ~50 types) into its own package. The type graph has no edge back to the recorder side, so the split has no cycle. It needs `GpuQueue`'s append methods exported.
