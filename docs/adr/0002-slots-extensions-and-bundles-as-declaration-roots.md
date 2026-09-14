---
status: accepted
supersedes: ADR-0001
---

# Slots, Extensions and Bundles as Declaration Roots

ADR 0001 split every plugin into a contract root, an `…impl` and an `internal/` package, and told its kinds apart with words the code did not hold. Its roots still carried logic: recording helpers, dispatch helpers such as `app.Paused`, and constructors that did real work. A concept needed three packages. "Slot", "Extension" and "Open slot" had no kernel concept behind them, so `app` was a contract nothing guaranteed a handler for. A Port was an interface plus a `RequireAdapter` call inside `Register`, invisible in any API package. gfx's backend contract lived in a second package, `gpu`, only because its root carried implementation.

We give every plugin one shape and one of three kinds, and check both with the tier test:

- A **Slot** (`slots/`: app, gfx, storage) declares at least one required Port and cannot work until an Adapter fills it. Its forwarders name only its own types, predeclared types, the standard library, Libraries and the kernel.
- An **Extension** (`extensions/`: gogpu, diskstorage, jsstorage) provides Adapters for Slots and declares no API.
- A **Bundle** (`bundles/`) is every other plugin. It requires no Port, and may collect Adapters or contribute them.

Every plugin `X` is four places:

- **The root, `X/`, holds declarations only**, in fixed file names: commands, events, resources, ports, adapters, types, config, errors and `Name`. Its only logic is `Config`'s builder methods and the forwarders in `utils.go`, each a single call into `X/internal/types`.
- **`X/internal/types`** holds the concrete types the root aliases, and exists only where an interface would cost performance, such as the recording queues.
- **`X/internal/`** holds the implementation.
- **The constructor package `X/xplugin`** exports only `New()`, and only composition roots and tests import it.

Ports and Adapters are declared identity types, as commands are: a Port in its plugin's `ports.go`, an Adapter in the providing plugin's `adapters.go`. The tier test cross-checks every `ProvideAdapter` against `adapters.go`. gfx folds `gpu` back into its root. app becomes a Slot owning the application loop, with a MainLoop Port that gogpu fills.

This applies to the cog repo. Games and examples are composition roots and stay free-form.

## Considered Options

- **Keep ADR 0001's contract root, `…impl` and `internal/`.** Rejected. A reader could not open a root and see only what the plugin offers, and three packages per concept is too granular for most plugins.
- **Keep gfx's backend contract in `gpu`.** Rejected. The split existed only because the root carried implementation; once the root holds declarations, the contract belongs there, and each of its types keeps one name, `gfx.X`.
- **Keep the kinds as unenforced words.** Rejected. A kind nothing checks drifts from the directory it is in, and nothing made a Slot's Adapter present, which is how callers of `app` came to read a missing handler as "not paused".

## Consequences

- The move is staged. The tier test holds a migration list of plugins not yet moved, each still checked under ADR 0001's rules. Each plugin's move deletes its entry, and the final sweep deletes the empty list.
- `DefaultConfig` is removed everywhere: a `Config`'s zero value is its default.
- Helpers that dispatch commands leave the API, so every caller dispatches and handles the answer itself.
- The time tool moves from gogpu to app and is renamed `app_time`. No other tool name or schema changes.
- `FlattenShader` leaves gfx's API, since its signature names `storage.FileSystem`.
- A root that aliases types with hot accessors may hold one kind of code: an unexported, never-called inline anchor calling those accessors. Go inlines a method of a package its caller does not import only when a package the caller imports references it, and a root of aliases and forwarders references none; gfx's move lost per-sprite and per-draw inlining in canvas and scene until its root anchored them (#366).
