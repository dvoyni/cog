---
status: accepted
supersedes: ADR-0002 (the root's contents and the import rules only)
---

# Roots Are Alias Indexes

ADR 0002 made every plugin root hold declarations only, aliasing nothing but its own `internal/types`. That forced `internal/` to import its root, for `Name`, the ordering identities, the errors and the Components, so the root could never name anything declared in `internal/`: the two would have been an import cycle. What the root must share with `internal/types` code - a Component a solver works on, a request a forwarder builds - had to be declared in `internal/types`. Code followed its data there, so a plugin's logic drifted into `internal/types`, away from the Systems that drive it, and a Component's shape was decided by which package could see its fields.

We turn the edge round. **A root declares nothing: it is an index of aliases.** Every name it offers is declared in its own `internal/` or `internal/types` and re-exported:

- a type as a type alias of the same name: `type Mesh = internal.Mesh`;
- a constant or a variable as a re-export of the same name: `const Name = internal.Name`, `var ErrNoArea = types.ErrNoArea`;
- a function as a forwarder in `utils.go`, a single call into `internal/` or `internal/types`, as before;
- an inline anchor where an importer needs one, as before.

**`internal/` and `internal/types` never import their own root.** The root imports its own `internal/`; nothing else in cog does. `Name`, the ordering identities, commands, Adapters, Components and errors are declared in `internal/`, which registers under them directly, and the root aliases them. An alias is the same type, so the kernel's reflection, `Subscribe`, `Before` and `ProvideAdapter` see exactly what they did.

**`internal/types` becomes optional, and holds data only.** A plugin keeps one when it has plenty of plain data another package of its own needs apart from the logic - POD structs with exported fields, enums, errors - and nothing else goes there: no function, and no method but `Error` and `String`. The logic that works on a type lives in `internal/`, beside the Systems that drive it, and a type with methods is declared there too. ecsphysics2d's solver, which ADR 0002 had put in `internal/types`, moves into `internal/`, and ecsscene's five vocabulary types go with the rest of `internal/`; neither plugin keeps an `internal/types`.

The root keeps its file names, so a reader still opens `components.go` for what an Entity can carry and `commands.go` for what the plugin answers. `internal/` mirrors them for what it declares on the root's behalf: `id.go`, `commands.go`, `components.go`, `err.go`.

A Component's fields are all exported, so every Component serialises whole; a field an invariant covers says so in its doc comment and names what writes it.

## Considered Options

- **Keep ADR 0002, with Components in `internal/types`.** Rejected: it is the arrangement that pulled logic into `internal/types`. A Component's unexported fields were visible to the solver only because the solver sat in `internal/types` with it.
- **Keep `internal/types` as a second implementation package.** Rejected: once nothing forces code there, a second package holding logic only splits one plugin's code over two import paths, and exports between them what should stay unexported.
- **Components in `internal/`, aliased by a root that `internal/` still imports.** Impossible: an import cycle, which Go rejects.
- **Declare Components in the root, beside the identities.** Works only for Components no `internal/types` code names, and a root cannot hold methods, so a Component with accessors could not live there. It splits one kind of declaration over two places by an accident of which package reads it.

## Consequences

- Any package importing a root now compiles that plugin's implementation. It links nothing new - a composed plugin is linked anyway - but a package naming `gfx.X` builds `gfx/internal` too. Cross-plugin cycles stay impossible: the edges between plugins already follow the plugin dependency order, which is acyclic, and removing each plugin's `internal/`-to-root edge leaves none.
- An alias exposes every exported method of the type it names. `internal/` declares what the root re-exports with that in mind: a method is public API the moment the root aliases its type.
- Doc comments: the declaration in `internal/` keeps its documentation, and the root's alias repeats it, because the root is what a game reads.
- The move is staged. `kernel/archtest` holds `aliasIndexRoots`, the plugins moved so far, and checks them under these rules and every other plugin under ADR 0002's. ecsaudio, ecsscene and ecsphysics2d move first. Each further plugin's move adds its entry; when every plugin is listed, the ADR 0002 root rules and the list go.
