# ecs

`github.com/dvoyni/cog/bundles/ecs` lets an app describe things in the world as
**Entities** carrying **Components**, and describe behaviour as **Systems** —
plain Go funcs whose parameter types say what they touch.

**A Component moves, a Query can be narrowed, Entities can be created and
retired, one Entity can reach another, a Component can name an engine-side
thing, and another plugin attaches with no mechanism at all: the storage, the
registration, the Query, its filters, structural change, the accessors, the
`List` and its write check, the resource and event handles, and both handler
builders are here.**
[`specs/ecs.md`](specs/ecs.md) is the specification the whole plugin is
judged against.

ecs is a **Bundle**: it requires no Adapter, and contributes one to mcp's
collected Port. The
vocabulary is in [`CONTEXT.md`](../../../CONTEXT.md) and the decision in
[ADR 0002](../../../docs/adr/0002-slots-extensions-and-bundles-as-declaration-roots.md).

## Packages

ecs has the declaration-root shape of
[`architecture.instructions.md`](../../../.github/instructions/architecture.instructions.md).

- **`bundles/ecs`** is the root, and holds declarations only, which are the
  whole library a System author uses: `Entity`, `NoEntity`, the `Entities` and
  `Store` resources, `Query`, `With` and `Without`, `Spawn` and
  `WriteableEntities`, the `Get`, `Set` and `Remove` accessors, `Read`
  and `Write`, `In` and `Feeder`, `Resp`, `ShrinkCmd` with `ShrinkRequest` and
  `ShrinkResponse`, `Config` and `Name`, and, in `adapters.go`, the Adapter
  identity `McpProvider`, a `kernel.Adapter[mcp.ProviderPort]`. Its functions,
  `RegisterComponent`, `NewStore`, `Storable`, `PointerFree`, `ToHandler`,
  `ToExecute` and `Feed`, are forwarders in `utils.go`. It declares no plugin,
  and it is what every other package imports. The List a Component holds is
  `m.List`, in `libs/m`, so a plugin can make a type storable without importing
  this package.
- **`bundles/ecs/internal/types`** declares every one of those types and holds
  the machinery behind them: the authority's allocation, despawn, Store
  enrolment and Component registry, which also answers a Component by the name
  `kernel.TypeName` renders for it, the Store and its type-erased header,
  registration, the Query with its driver and fillers, the filters, structural
  change, the accessors, `List` and its write check, the resource and event
  handles, both handler builders with the parameter classification they
  share, and the factories of the three [read
  Commands](#reading-the-world-by-name) with their request and response types.
  The root aliases every type and forwards every function to it, except those
  read Commands' types, which nothing outside `bundles/ecs` names.
- **`bundles/ecs/internal`** is the plugin: its `New`, the resolution of
  `ecs.Config`, and a `Register` that publishes the authority, registers
  `ShrinkCmd` and the three unexported [read
  Commands](#reading-the-world-by-name), and registers the Store of
  `m.Transform`, where an Entity stands: the one Component the ECS registers
  itself. It also contributes the `mcp.Provider` that offers the read Commands
  to an Agent, as `ecs.McpProvider` ([Offered To An
  Agent](#offered-to-an-agent)).
- **`bundles/ecs/ecsplugin`** exports only `New() kernel.Plugin`. Only
  composition roots and tests import it.

The aliased types stay concrete types (`type Entities = types.Entities`,
`type Query[Q any] = types.Query[Q]`): no probe, fill or spawn goes through an
interface, and a despawn pays the one indirect call per Store it always paid.
Their exported methods (`Entities.Alive`, `Query.All`, `Store.Get`, …) are
public API through the alias. What the plugin needs beyond that is the five
functions `internal/types` exports in `friends.go`, which nothing outside
`bundles/ecs` can call. `internal/types` never imports the root. The types are
declared there, but the kernel's architecture output and every ecs diagnostic
still name them `ecs.X` — the resource is `*ecs.Entities`, a Store
`*ecs.Store[game.Health]` — because `kernel.TypeName` renders a type declared in
an `internal` package under its enclosing package.

## Dependencies

- Go packages: the standard library, `kernel`, `assets` for `assets.Blob`, and
  the `mcp` root for the Provider's Adapter identity and capability constructors
- Plugin dependencies: none
- Configuration: `ecs.Config`, whose `PrewarmEntities` is how many Entities
  the authority reserves room for up front — a hint, not a limit; a zero field
  takes its default, 1024

## Composing

```go
config[ecs.Name] = ecs.Config{PrewarmEntities: prewarmEntities}
kernel.New(config).WithPlugins(ecsplugin.New(), physics.New(), game.New())
```

**No plugin constructor takes the world.** `ecsplugin.New` creates the authority
from its config and publishes it as the `*Entities` resource every System holds
for read and every structural change holds for write. Besides that it registers
[`ShrinkCmd`](#giving-memory-back), the three [read
Commands](#reading-the-world-by-name), and the Store of `m.Transform`, the one
Component the ECS registers itself (see [Component
registration](#component-registration)). Every other Component is registered by
the plugin that defines it, and Systems are ordinary subscriptions.

Component registration and the handler builder still need the authority at
registration, before any handler runs. They read it with
`kernel.Registrar.Dependency`, which is why `ToHandler` and `ToExecute` take the
registrar: it is the one registration-time resource read the kernel permits,
the value of a resource owned by a declared dependency, which has therefore
already registered. So
**every plugin that registers a Component or a System declares `ecs.Name` in its
`Dependencies`**. It already had to, for the `read{*Entities}` every System
takes; a plugin that registers Components alone and forgets it fails
composition. `Dependency` returns `ErrUnavailableDependency` naming it, and ecs
re-panics it: a registration-time panic, which the plugin boundary reports as
`kernel.ErrPluginPanic` naming the plugin, with `ErrUnavailableDependency` as the
recovered value.

## Entity

```go
type Entity = types.Entity   // a uint64
const NoEntity = types.NoEntity
```

An `Entity` is an opaque handle to one thing. It is comparable, copyable and
usable as a map key, and its zero value means no Entity. It carries a
generation, so a handle to a despawned entity is detectably stale rather than
silently addressing whatever took its place — which is what makes it safe for a
game to hold entities across frames: a creature's target, a spell's owner, a
projectile's caster.

The index and generation live in one `uint64`, and **that split is not public
contract**: there is no exported `Index` or `Generation`. Both this and a struct
of two `uint32`s are 8 bytes and both are comparable, but the struct leaks its
layout into every call site and can never be re-cut. Generations start at 1, so
`Entity(0)` unambiguously means "no Entity" while index 0 stays an ordinary
usable slot.

`Entity` implements `fmt.Stringer`. `Entity(7v2)` is index 7 at generation 2;
`NoEntity` prints as itself.

## Entities

```go
entities := registrar.Dependency[*ecs.Entities]()   // at registration
entities.Alive(e)                                   // does this handle name an entity that exists?
```

`Entities` is the id authority: it allocates indices, tracks their generations,
answers `Alive`, and holds a reference to every `Store`. There is exactly one
per Engine, and that is what makes an Engine the boundary of one simulation — a
second simulation is a second Engine. **No identifier in this package contains
the word `World`**, and a test enforces it.

The ecs plugin, built by `ecsplugin.New`, creates it, reserving room for
`Config.PrewarmEntities` indices, and nothing else can: the constructor is in
`internal/types`, which nothing outside `bundles/ecs` can import, and that is
what keeps it one per Engine.
The number is the peak concurrent entity count the app expects, **not a cap**:
exceeding it costs a growth, not an error.

Indices are **recycled through a free list**, which is what bounds every Store's
flat sparse index by *peak concurrent* entities rather than by entities ever
created. Reclamation is **eager and is therefore nothing at all**: a despawn
empties every Store immediately and the index returns to the free list at once.
**Nothing reclaims on its own**: there is no compaction and no sweep, and the free
list keeps its high-water capacity until the app shrinks it through
[`ShrinkCmd`](#giving-memory-back).

**Allocating and despawning are not methods a reader can call.** `Entities` is
held for read by every handler that touches any Store, so the authority to
change which entities exist arrives only through its write-locked promotion —
[`Spawn` and `WriteableEntities`](#structural-change) — and is visible in a
System's signature and nowhere else.

**`Alive` is the only question a read may ask, and it is the rarer of the two
liveness questions.** There are exactly two, and the one almost always wanted is
the cheaper one:

| the question | who answers it | what it needs |
| --- | --- | --- |
| **"does `e` still have `Health`?"** | the [accessor's](#reaching-another-entity) own probe — `Get[Health].Of`, or `Has` on the Store | **`read{*Store[Health]}`, a lock the System already holds** |
| "does `e` exist at all?" | `Entities.Alive(e)` | `read{*Entities}`, which is wider |

"Is my target still alive" really means "does my target still have `Health`", and
the accessor answers it in the probe it was going to make anyway — the compare
that finds the row is the compare that rejects a stale handle, so the liveness
costs nothing extra. Reaching for `Alive` instead pushes the wider lock into
every System that holds an Entity across frames, which is every System that holds
a target, an owner or a caster.

A despawn is **total**. Nothing records which Stores hold an entity, so every
Store is asked, through the one method of the type-erased `storeCore`, which a
Store enrols with the authority bound to itself: one indirect call per Store per
despawn, never per entity. That is also
why no per-entity index of "which Stores hold me" may ever be added: the index
would live in `Entities`, so maintaining it would move every Component addition
from that Component's lock to the one lock every System holds. **Any global
index is a global lock.**

## Store

```go
store := ecs.NewStore[Position](entities, peak)

store.Set(e, Position{X: 1})   // give e this Component, or replace its value
store.Has(e)                   // one load, one compare
store.Get(e)                   // (Position, bool) — a copy
store.Ref(e)                   // (*Position, bool) — for a writer
store.Remove(e)                // swap-remove; reports whether e had one
store.Len()                    // the population
```

A `Store` is the holding of every value of one Component type, one per
registered type, and **the unit a lock is taken on**. It is three arrays: a flat
`sparse` index of 8-byte slots, an `owners` list from dense row to the full
entity id, and the packed `dense` data. `Len` is exactly the population, because
removal is swap-remove and the packed arrays therefore never contain holes.

**The probe is one load and one compare, and the compare that finds the row is
the compare that rejects a stale handle.** The sparse slot folds the generation
in, so liveness is not an extra cost and there is no separate liveness structure
to consult: a stale handle fails membership for the same reason an absent one
does. A probe reads two arrays, not three — `owners` is for the driver of a
Query, not for the probe.

A Store must be reached as `*Store[T]`. A value store makes a kernel write
handle's `Get` return a copy, so mutations through it are silently discarded.

`NewStore` **enrols the Store with the authority**, which is what lets a despawn
empty it; component registration is its sanctioned caller, and that is where the
pointer-free check and the kernel resource come from. Its `ids` argument is a
reserve hint: reserving buys an allocation-free fill, where growing from empty
doubles repeatedly. **Nothing shrinks on its own** — a removal re-slices the
arrays and never hands them back, so a respawn reuses the row, and the app gives
the capacity back through [`ShrinkCmd`](#giving-memory-back).

Two consequences of swap-remove are contract:

- **No dense row index may be held across a mutation**, and a `Ref` is valid
  only until the Store next changes structurally.
- **Iteration order is unspecified.** Direction is not: a forward walk that
  removes as it goes silently skips — measured here at 667 of 1000 — because the
  tail row drops into the hole and the next step goes straight past it. A
  reverse walk visits everyone, which is what the Query's `All()` rests on.

A **Tag** — a Component with no fields — is an ordinary Store whose rows carry
nothing, so its dense array costs no memory however many entities it holds.

### Giving memory back

```go
released, err := executioner.ExecuteCommand[ecs.ShrinkCmd](ecs.ShrinkRequest{})
// released.Stores, released.Entities, released.Scratch, released.Hooks: bytes
```

**Nothing in the ECS gives memory back on its own, and the app gives it back
with one Command.** A Store, the free list, a Query's walk and what Hooks hold
keep their high-water capacity, so steady state never allocates; after a spike,
such as a level load or a screen of effects, the app executes `ShrinkCmd`,
because the app knows when the spike has ended and a heuristic cannot.

- **The zero request shrinks every area to capacity equal to length**, and each
  `Keep` option opts one out: `KeepStores` (each Store's rows, cut to its
  population, and its sparse array, cut to its highest index held),
  `KeepEntities` (the free list, and the free indices at the top of the index
  space), `KeepScratch` (each Query's walk, and each writer's row copies for
  Changed) and `KeepHooks` (each Store's Hook log, cut to the records some reader
  has yet to take, and each reader's copy). Records not yet read survive, with
  their values.
- **The response is the bytes each area released.** An area kept reports 0 and
  its capacity is unchanged. A Query's walk aliases its driver Store's owners
  array, so `Scratch` and `Stores` can count the same array and are not summed.
  Go frees the old arrays at its next collection; `debug.FreeOSMemory` is the
  caller's to call.
- **The frames after it regrow what they need, and those frames allocate.**
  Shrink when a spike has ended, not every frame. After them the frame is back on
  its line, within 0.05 objects of an engine that never shrank.
- **A dropped index keeps its handles stale.** Dropping an index forgets its
  generation, so the authority keeps one generation floor: an index allocated
  again starts above every generation it was ever issued, and a handle from
  before the shrink never matches the Entity it is allocated to.
- **Its lock is `write{*Entities}` alone**, which excludes every ECS System while
  it runs, because every handler that touches a Store holds `read{*Entities}`. It
  costs nothing in a frame that doesn't execute it.

## Components: what a Component may hold

```go
func Storable(t reflect.Type) error    // the registration gate
func PointerFree(t reflect.Type) error // the fast-path property
```

**A Component type contains no mutable indirection, transitively** — every
pointer it holds, it holds to memory nothing can write. That is mechanically
checkable: one walk, run once when the type is registered. It permits numerics,
bools, fixed-size arrays, `Entity`, structs of those, **`string`**,
**`assets.Blob`** and **`m.List[T]`**; it refuses pointers, bare slices, maps,
channels, funcs and interfaces.

**The rule is about the lock unit, not the collector.** A read yields a copy,
and that is what makes `read{C}` sound for concurrent readers — but only where
the copy is not itself a write handle:

| a read yields… | can the reader write the Store through it? |
| --- | --- |
| a number, an `Entity`, a `[32]byte` | no |
| a **`string`** | **no** — the header is a copy and the bytes are immutable |
| an **`assets.Blob`** | **no, by contract** — its bytes are never written after construction |
| a `[]T` | **yes** — refused for exactly this |
| an **`m.List[T]`** | only through `Set`, which validation mode checks |

Copying and serialisation come second, and a string satisfies both: it stays
meaningful after the thing it was copied from is gone, and it encodes trivially.

**`assets.Blob` is admitted on trust.** It is a pointer and a length whose
contract is that the bytes a Component holds are never written after
construction, recognised by type identity — a named `[]byte` of your own is
still refused. Nothing checks the contract: `Data()` hands out a live slice, and
validation mode cannot see a write through it, because a Blob has no mutator to
watch. Both of its fields are unexported, so a holder can read the run and
cannot repoint it. It is admitted because the bytes engine types carry
— a texture's pixels, a buffer's contents, a parameter's raw layout — are static
in practice, and a `List[byte]` would copy them for a guarantee nothing uses.
That is what makes `gfx.ParameterDescr`, `TextureDescr` and `BufferDescr`
Components as they stand. `PointerFree` still refuses one.

The error names the offending field **by path**, because the field that fails is
usually several structs down and naming only the Component is useless:

```
ecs.PathedDrawable.Deep.Inner.Handle is a ptr, which is mutable indirection
```

`PointerFree` is still here and still means what it meant. It is no longer the
gate but the **fast path**: a pointer-free Component is copied by sized moves,
left where it lies by a swap-remove, kept in a span the mark phase never walks,
and iterated by an unrolled filler. A Component holding a string, a Blob or a List gives
all four up, for its own Store only — so prefer `[32]byte` wherever the bound is
real.

A Component names an engine-side thing however the plugin that resolves those
names asks it to. This package used to supply a name hash for that and no longer
does; see [Naming an engine-side thing:
removed](#naming-an-engine-side-thing-removed).

`sync` types are not special-cased — a mutex is refused by this walk, and would
be wrong in a Component anyway, for the same reason `go vet` already says so.

## Variable-length data: `m.List[T]`

The List is declared in `libs/m`, beside `m.Maybe`, and is spelled `m.List`; the
ECS keeps what it knows about one — the registration walk admitting it, the
generation a Changed Hook compares, and the `ecs_validate` check `Set` makes,
which the ECS installs into `m` when validation mode is built.

```go
// package m
func NewList[T any](values ...T) List[T]
func ListOf[T any](values []T) List[T]

func (l List[T]) Len() int
func (l List[T]) At(i int) T
func (l List[T]) All() iter.Seq2[int, T]
func (l *List[T]) Set(i int, value T)  // write lock only; checked under -tags ecs_validate
func (l List[T]) MarshalJSON() ([]byte, error)
```

**A `[]T` in a Component is refused; a `List[T]` is what it holds instead.** The
backing array is unexported, the constructors copy into a fresh one, and `Len`,
`At` and `All` yield copies. There is deliberately **no `Slice`** — handing back
the backing array would give away the writable alias the type exists to
withhold.

Its length is fixed at construction and there is no `Append`: growing means an
allocation, and a List whose length changes is a new List written into the
Component under a write lock. Where the length changes every frame, prefer a
fixed-capacity array with a live count, or a child Entity.

**A List may hold elements that hold Lists.** Validation walks each outer List's
elements and stamps every nested backing array exactly as it stamps the outer
one, so a nested `Set` through a read panics as a flat one does. The cost lands
only in a validating build: a nested row fills the bounded stamp table faster, so
its oldest-first eviction forgets sooner.

**A List encodes as the JSON array of its elements**, `[]` when empty and never
`null`, and a List of Lists nests. It has no unexported state `encoding/json`
would otherwise show as `{}`, which is what the [read
Commands](#reading-the-world-by-name) rely on. Nothing decodes one.

**There is no `Raw()`.** Copy out through `All()` into scratch you reuse: four
328 B elements cost about 60 ns and no allocation, because the iterator inlines.
A read-only view nothing can check waits for a benchmark that asks for it.

In preference order, variable-length data has four answers: **a child Entity**
with an owning reference; **a fixed-capacity array** where the bound is small
and real; **a `List`** where the bound is not real but the contents are set at
spawn; and a side store keyed by Entity, which is post-v1.

## Validation mode

```
go test -tags ecs_validate ./...
```

`List.Set` writes through to memory every reader of that Component shares, so it
is legal only for a caller holding the write lock. That is a rule rather than a
property, and the tag is what checks it — at the write, where the error is,
naming the Component and the access mode:

```
ecs: List.Set through a read of game.Inventory: a read yields a copy and a copy
of a List shares its backing array, so this writes the Store while every
concurrent reader holds read{game.Inventory}. Name the Component as
*game.Inventory to write it
```

It catches a write through a read field or a `Get`, a write through a value
retained past the `All()` that yielded it, and a write through the caller's own
copy after that value entered a Store — and each of those one List further in,
for a List whose elements hold Lists. It does not see a write through an
`assets.Blob`. Without the tag `validate` is a constant
`false`, so a release build contains no branch, no table and no load for any of
it.

**It is detection, not prevention.** Its coverage is what a run executes. Run
the tag in tests and in development; a string needs none of this, which is a
reason to reach for one first.

## Naming an engine-side thing: removed

**This package supplied `HashOf`, `HashKey`, `NoHash` and `Names[K, V]`, and no
longer does.** They existed for one reason — a Component could not hold a
`string`, so a name had to become a number before it could be stored. A
Component may hold a string now, so how a Component says which model, clip or
node it means is a matter between the plugin that writes the name and the plugin
that resolves it. The ECS has no type, no table and no opinion.

The cost that justified the machinery did not survive being measured against
what it actually replaced: **6.66 ns** for a stored hash against **7.79 ns** for
a `map[string]V` keyed by the path — 1.1 ns, about 5.5 µs a frame at 5 000
drawables. A dense index is **0.64 ns** and is still what a consumer should
resolve *to*, which is the advice that outlives the hash: name in the Component,
index inside whatever consumes it.

Two properties of a hash were real and a consumer that wants them should keep
them in its own package. It is the same in every process and every run, which an
assigned index is not, so it survives a save file or a wire; and producing one
needs nothing, so a System renames what an Entity points at holding only the
lock it already had. Neither needs the *ECS* to own it.

`ecsscene` was built on `ModelHash`, `ClipHash`, `ecs.Names` and `ecs.NoHash`,
and was rebuilt without them: its `Model` Component holds the glTF path as a
string.

## Component registration

```go
func RegisterComponent[C any](registrar *kernel.Registrar, ids uint32) *ecs.Store[C]
```

A Component type is registered **explicitly, once, by exactly one plugin**.
`RegisterComponent` checks the Component rule, creates the Store, enrols it
with the authority it reads through `registrar.Dependency`, hands it to the kernel as an ordinary resource of type
`*ecs.Store[C]` — **owned by the calling plugin** — and bakes the per-type
closures a Query is later planned against. There is no resource factory and
kernel needs no change.

> **Register a Component in the plugin that defines its Go type.** Shared
> vocabulary belongs to the lowest plugin that owns it — `physics` declares
> `Body`, not the game — which is the direction imports already run.

That rule is what keeps the coupling check working on Component data. A System
in another plugin that locks the Store must declare a dependency on its owner,
and the Go import graph already forces the same edge, since a System cannot name
`B.Health` in a Query struct without importing `B`. Ownership by `ecs` would not
make the check lenient, it would make it **vacuous**. Registering someone else's
type stays legal as the escape hatch, and the kernel's duplicate-registration
error names both plugins.

**`m.Transform` is the one exception, and it is vacuous on purpose.** Where an
Entity stands is read by every binding, so the type is declared in `libs/m` and
its Store is registered by the ecs plugin itself: one Store, so two Components
can never describe one position without the scheduler relating them. The cost:
every plugin with Systems already depends on `ecs`, so a System writing
`m.Transform` without declaring anything else is never caught at composition.
Order its writers deliberately. The exception is not licence to register your
own Components in `ecs`.

Half of "explicit" is forced rather than chosen: a `Lock` must bind every handle
it will use, and a declared resource with no initial value fails finalisation —
so **you cannot lock a Store that does not exist at registration**, and
discovery-on-first-use is off the table. Registration order already guarantees
the Store exists first, because plugins are ordered by declared dependency.

`ids` is the peak population the Store reserves for. It is a hint, not a cap.

## Query

```go
type MoveQuery struct {
    Body     *Body     // write — yields the stored value itself
    Velocity Velocity  // read  — yields a copy
}

func move(q *ecs.Query[MoveQuery]) {
    for e, it := range q.All() {
        it.Body.Pos = it.Body.Pos.Add(it.Velocity.V)
        _ = e
    }
}
```

A **Query** is a struct type whose field types are the Components a System
touches, and **a field's pointer-ness is its access mode**. Nothing else
declares the lock set: `MoveQuery` above is `write{Body}`, `read{Velocity}` and
`read{*Entities}`, derived from the Go types at registration. That is what makes
**under-declaration unrepresentable** — the only route to a Store is a Query, and
the field types *are* the declaration.

**A read yields a copy.** A read yielding a pointer would be a data race against
concurrent readers and Go has no pointer-to-const, so a fat read Component costs
a copy per Entity — which is evidence the Component is too fat, not a reason for
a hatch. Fields are **named, not embedded**: two Components with the same base
name from different packages cannot both be embedded, and two Components each
having a `Kind` field would make `it.Kind` an ambiguous selector at the use
site, far from the cause.

`All()` yields `(Entity, *Q)` — the Entity first and always, because the
driver's owners array is loaded anyway. **The pointer is the Query's own buffer
and is valid only for the current step**, which is the lifetime rule kernel's
handles already carry. A Query belongs to one subscription: it is planned once,
for that System, and refilled for every Entity.

**The walk is backwards, and that is a guarantee**, not an accident. It is the
whole reason "you may restructure the Entity you are currently visiting" is safe
under swap-remove: a forward walk skips, silently, and reaches Entities appended
during the loop. Changing whether some *other* Entity is in the driver Store is
undefined.

### The driver

A Query picks one Store as its **driver**, walks its owners and probes the rest,
so it costs what its driver is long rather than what it matches. **The driver is
chosen per run, by scanning Store lengths, and never cached.** Store lengths
change on every structural change, so a choice made at registration would walk
five thousand Entities to find three the moment a Component became rare — and a
cache would be a table every structural change has to write, which is the global
index this storage model exists to avoid. Smallest-Store is a heuristic, not an
optimum: the optimum driver is the smallest *intersection*, unknowable without
computing it.

#### The known bad case, and its remedy

Being a heuristic, it has a case where it loses badly, and the case is worth
knowing because an app can hit it without noticing: **two large, mostly disjoint
Stores**. 5 000 entities with `Body`, 5 000 with `Collider`, 100 with both. The
driver can only be one of the two five thousands, so it yields 5 000 candidates
and throws away 4 900 of them:

| driving off | ns/op |
| --- | --- |
| a 5 000-entity Store — 5 000 candidates, 100 survive | 3 354 |
| **a 100-entity Tag on the intersection** | **422 — 7.9x faster** |

0.60 ns per discarded candidate — the two arms differ by 4 900 candidates and
2 932 ns — so it is real, linear, and cheap enough to be a problem only at this
ratio. Both arms do identical work per matched entity, and the
Query differs only by the Tag field.

**The remedy is an app-maintained Tag.** The app puts a `Solid` Tag on exactly
the entities that have both, maintains it itself, and names it in the Query as
`_ ecs.With[Solid]` — a Tag is just another Store, it costs no memory however
many entities it holds, and it is a very good driver because the scan picks it.
A `With` is a driver candidate like any field the Query matches on presence;
see [Filters](#filters).

There is deliberately **no ECS mechanism** for this. That road is EnTT's groups,
shipyard's packs and bevy's sparse-set markers, and **exclusivity is what killed
all three**: a Store may belong to at most one group, so the second Query that
wants a different grouping cannot have one, and the cost lands on every
structural change rather than on the Query that asked. An app-maintained Tag has
none of those properties — any number of them may overlap, nothing else has to
know, and the app decides when it is worth maintaining.

### Filters

```go
type ActiveQuery struct {
    Body     *Body
    Velocity Velocity
    _        ecs.Without[Disabled]   // narrows; yields nothing
}
```

A **filter is a blank field**. It narrows which Entities the Query matches and
puts nothing in the struct: `ecs.Without[T]` matches the Entities that do not
have `T`, and `ecs.With[T]` those that do, without `T` landing in the buffer.

**A filter contributes a read of its Store**, and this is the part that is easy
to get wrong. Evaluating `Without[Disabled]` loads `Store[Disabled]`'s sparse
slot, and a System adding `Disabled` holds `write{Disabled}` and is mutating
that exact array. A filter reads less *data* than a Component field — nothing
lands in the struct — but it reads **the same Store**, and the lock set is about
Stores. So `ActiveQuery` above is `write{Body}`, `read{Velocity}`,
`read{Disabled}` and `read{*Entities}`, and a System carrying it can never run
beside one holding `write{Disabled}`. A test asserts that through the kernel's
own conflict report rather than through a list of types: drop the declaration
and the pair stops being reported, which is the race.

**The fill skips a filter, and skipping it is the point rather than an
optimisation.** Go assigns a blank field an offset like any other — a zero-size
field in the middle of a struct shares the offset of the field after it, and a
trailing one forces a byte of padding — so a fill that copied the filtered
Component's bytes there would write over the field beside it or past the end of
the buffer. The recognition happens at registration, as a planned row width of
zero; by the time a filler runs there is nothing left to recognise.

A `Without` matches through the same single compare a Component field does. The
probe looks for the Entity's generation for a Component field and for the
**absent** generation for a `Without`, which is one `or` against a mask chosen at
registration. That is exact because a Store's sparse slot for a *live* Entity
holds either that Entity's generation or the absent one, and every Entity a
probe sees comes out of a driver's `owners`. The alternative — comparing the
probe's answer against a per-field bool — costs three instructions instead of
one and measured **3% of a ten-thousand-Entity frame**, paid by every Query
whether it has a filter or not.

**A `With` can drive; a `Without` cannot.** The driver is the shortest Store
among every field the Query matches on *presence*: Component fields, Tag fields
and `With` filters. The reason is what each Store holds. A `With[T]`'s `owners`
is a superset of the match set, exactly as a Component field's is, so walking it
and probing the rest finds every match. A `Without[T]`'s `owners` lists exactly
the Entities to *exclude*, and nothing enumerates the complement, so walking it
would visit precisely the Entities the Query drops. The asymmetry is the rule;
it is not an accident of the implementation, so neither "a filter never drives"
nor "a `Without` should drive" is worth re-proposing. In the code the test is
the probe's own encoding of "matches on absence" — a field whose cursor looks
for the absent generation is skipped by the scan — checked once per field per
run and never per Entity.

So **`_ ecs.With[T]` is the one recommended spelling for presence a System
matches on but does not read**, whether `T` is a Tag or not. Naming a Tag as an
ordinary field (`Solid Solid`) still works and still drives, but it reads as
though the value were wanted; and for a non-Tag `T`, naming it costs a copy per
Entity that the `With` spelling does not, while still being considered as the
driver. Measured on the bad case above — 5 000 with `Body`, 5 000 with
`Collider`, 100 with both and `Solid` on those — interleaved over ten rounds from
two test binaries, medians:

| Query | ns/op | allocs |
| --- | --- | --- |
| `{Body *Body; Collider Collider}` — drives off 5 000 | 3 645 | 0 |
| `{Body *Body; Collider Collider; Solid Solid}` | 478 | 0 |
| **`{Body *Body; Collider Collider; _ ecs.With[Solid]}`** | **481** | **0** |

The two remedy spellings are within noise of each other (0.8%) and both 7.6×
faster than the bad case; neither existing arm moved against the parent build
beyond run-order noise (bad case 3 820 → 3 645, Tag remedy 483 → 478).

**A Query must name at least one field it matches on presence.** A Query of
filters alone is legal if one of them is a `With`, and it yields the Entity and
an empty struct — "visit every Entity that has X" needs no field the System
never reads. A Query of `Without`s alone, or of no fields at all, has nothing to
walk and fails at registration:

```
plugin "systems" panicked in Register: ecs: Query game.WithoutsOnlyQuery names
nothing it matches on presence, so nothing can drive it: a Without filter names
the Entities to exclude and nothing enumerates the rest, so a Query needs at
least one Component, Tag or With
```

Letting a `With` drive changes nothing else. A filter is still planned zero
wide, so a `With` driver's fill copies nothing, in the unrolled fillers, the
per-field loop and `All()`'s inline two-field walk alike. Every filter still
contributes a read of its Store whether or not it drives, and validation mode
still skips every filter when it stamps a row.


## System

```go
type MoveSystem kernel.Subscription[app.UpdateEvent]

registrar.Subscribe[MoveSystem](ecs.ToHandler[app.UpdateEvent](registrar, move)).After[GravitySystem]()
```

A **System** is a plain Go func, called once per tick, that iterates the
Entities its Queries match itself. `ToHandler[E]` turns one into the
`func() (kernel.Lock, kernel.Observe[E])` factory an ordinary cog subscription
already takes, so **the ECS contributes no registration API of its own**:
`Before`, `After`, `First`, `Last`, ownership, `Describe` and every `Err` kind
work unchanged, and one subscription per System is what lets the existing
scheduler run disjoint Systems concurrently with no new machinery.

The lock set is the union of what the parameters declare, computed once by
walking the func's parameter types. **The reflection runs exactly once and never
again.** Arity is arbitrary, and **a System returns nothing** — a hard rule
rather than a style preference, because `reflect.Value.Call` allocates for a
callee that returns a value, so the builder rejects one at registration.

**Every handler that touches any Store declares `read{*Entities}`,
unconditionally and first.** That is the invariant the despawn traversal rests
on: a despawn reaches every Store through Go pointers the kernel does not
police, and holding the authority for write is what excludes every System that
holds it for read.

### What a signature may contain

**This is contract, not convention.** A System takes any number of:

| parameter | declares | what it is |
| --- | --- | --- |
| `*ecs.Query[Q]` | `read{*Entities}` + per-field access | the Components it iterates |
| `*ecs.Spawn[S]` | `write{*Entities}` + `write{*Store[F]}` per Component set field | creating Entities |
| `*ecs.WriteableEntities` | `write{*Entities}` | despawning |
| `*ecs.Get[T]` | `read{*Store[T]}` + `read{*Entities}` | reading one Component of an Entity it did not iterate to |
| `*ecs.Set[T]`, `*ecs.Remove[T]` | `write{*Store[T]}` + `read{*Entities}` | writing, inserting or taking away the same |
| `*ecs.Read[T]`, `*ecs.Write[T]` | the kernel's own read/write on `T` | any other plugin's resource |
| `*ecs.In[T]` | nothing | a value projected out of the event or request |
| `*ecs.Resp[Res]` | nothing | **a command only** — the answer it writes |
| `kernel.Kernel` | nothing | the kernel value, for publishing an event |
| the event or request value | nothing | legal, and not the default shape |

**Anything else is a composition-time failure naming the System.** This is a
mistake every new user makes once, so the diagnostic matters more than the
mechanism. The failure is a registration-time panic with one sentence naming
the System and the offending parameter, which the plugin boundary reports as
`kernel.ErrPluginPanic` naming the plugin, with `Boundary` `"Register"`; and
composition fails. **That is the contract**, not a placeholder. The builder runs
before `Registrar.Subscribe` receives the Subscription's identity type, so the
sentence names the System by its **func type**, and `ErrPluginPanic` names the
plugin it is registered in. A parameter the ECS does not hand out, as a composed
engine prints it:

```
plugin "systems" panicked in Register: ecs: System func(*ecs.Store[game.Body])
takes *ecs.Store[game.Body], which is not something a System may take; …
```

A Query naming a Component no plugin registered takes the same route, and names
the **Component** and the **Query** rather than the store type the user never
wrote:

```
plugin "systems" panicked in Register: ecs: Query game.GuardedQ names
unregistered Component game.Guarded
```

**Composition stops at the first bad signature**, so three of them are fixed one
per run. That is the kernel's rule for every fault — registration stops at the
first one it finds, whatever route the fault arrives by — and not a cost of this
route.

**A reported composition error is withdrawn.**
[#238](https://github.com/dvoyni/cog/issues/238) asked for this failure to be a
composition error collected with `ErrMissingResource` and the others, "rather
than surfacing as `ErrPluginPanic` with a stack".
[#279](https://github.com/dvoyni/cog/issues/279) withdrew that requirement, for
three reasons, and a proposal to bring it back has to meet them:

1. **The default output is one sentence with no stack.** `ErrPluginPanic`
   carries the stack in its `Stack` field, but its `Error()` renders only the
   plugin, the boundary and the recovered value, and the engine's default error
   handler logs `%v`. The stack reaches only a custom handler that reads the
   field. The stack in the user's face that #238 objected to does not happen.
2. **An ecs registration builder returns a value, so a fault in one can only
   panic.** `ToHandler` and `ToExecute` return the factory a subscription or
   Command takes, and `RegisterComponent` returns the `*Store[C]`; none of them
   has an error to return. Every ecs registration-time refusal takes this route:
   the Storable refusal, an unregistered Component in a Query or a Component set,
   the Component-set shape refusals, and `Dependency`'s own
   `ErrUnavailableDependency`, which ecs re-panics. That is ecs's consistency with
   itself, and it diverges from the kernel's on purpose: the kernel returns the
   error from `Dependency` because `Dependency` owes its caller a value it can
   pair with one. A builder returning `(factory, error)` would break the
   one-expression `registrar.Subscribe[X](ecs.ToHandler(...))` that is the point
   of the design.
3. **A reported error would buy nothing and cost a great deal.** The kernel stops
   composition at its first fault whatever route it takes, so a
   `Registrar.ReportError(error)` could not collect several bad signatures in one
   run either: one fix per run is what every route gives. What it would cost is
   every refusal site keeping going after an error, including a parameter's own
   preparation, which receives a `kernel.ResourceAccess` rather than the
   `Registrar` and plans Query fields that later code assumes are well-formed.
   The name would also clash with `Kernel.ReportError`, which has runtime rather
   than composition semantics.

The classification is a **closed set**, and structurally so: every parameter
that reaches the world is a pointer to a type in this package implementing one
unexported method, so no package outside can add one and the builder never has
to ask what a type means. A new handle is a new type, not a new branch.

`ecs.Read[T]` and `ecs.Write[T]` will **not** name the ECS's own cells —
`*ecs.Entities` or a `*ecs.Store[T]` — and the refusal is soundness rather than
tidiness. `Store.Remove` is exported, so `ecs.Read[*ecs.Store[Body]]` would hand
a read-locked System a mutator: a data race the kernel cannot see, because the
lock set says read and the code writes. The accessors take the right lock,
declare `read{*Entities}` with it, and check the Component was registered at
all.

A signature may name one resource twice — two `ecs.Write[T]`, or an
`ecs.Read[T]` beside an `ecs.Write[T]` — and each parameter's `Get` returns what
it resolved at the start of the invocation. A `Set` on one is read back by that
parameter's own `Get`, and the other does not see it until the next invocation.

### A System is not re-entrant

Every System keeps its per-invocation state in one struct built at registration:
the `reflect.Value` arguments it calls through, the cell its event or request is
written into, for `ToExecute`, the single `Resp` its answer leaves by, and the
handles each parameter resolves once an invocation — the Store behind a `Get`,
`Set` or `Remove`, the authority and the Stores behind a `Spawn` or a
`WriteableEntities`, the value behind a `Read` or `Write`. That is what makes a
System cost nothing a tick, and it means two invocations of one System must
never overlap.

**The kernel guarantees that; the caller does not have to.** Every System
declares `ResourceAccess.Exclusive()` in its `Lock`, so a second invocation
queues behind the first rather than joining it. It excludes a System against
itself alone, and costs no parallelism against any other System.

This matters most where the lock set would not have saved it. A System that
writes any Store is already serialised against itself by that write, but a
**read-only** one — a query that answers a question and changes nothing —
declares no write at all, so nothing else would keep two invocations apart:

```go
func count(request CountRequest, q *ecs.Query[CountQ], answer *ecs.Resp[CountResponse]) {
    reply := CountResponse{}
    for range q.All() { reply.N++ }
    answer.Set(reply)
}
```

Exposed as an agent tool, that is exactly the shape two parallel calls reach at
once. Without the declaration each caller could receive the other's answer, and
nothing would fail to say so.

### The event does not belong in the signature

```go
func advance(q *ecs.Query[AdvanceQ], dt *ecs.In[float64]) {
    step := dt.Get()                              // once, outside the loop
    for _, it := range q.All() { … }
}

ecs.ToHandler[app.UpdateEvent](registrar, advance, ecs.Feed(func(e app.UpdateEvent) float64 { return e.Dt }))
ecs.ToHandler[FixedTick](registrar, advance,       ecs.Feed(func(e FixedTick) float64 { return e.Step }))
```

A System that names the event **can only ever be subscribed to that event**. The
same gameplay cannot then be driven by a fixed-step tick, by a rollback
re-simulation, or by a test harness publishing its own frames, without being
written twice. The event belongs to the **adapter**, which is already generic
over it. Naming it stays legal — a System genuinely about one event should name
it — but it is not the shape to reach for.

`In[T]` carries a per-tick value and declares no lock; `Feed` is the projection,
resolved at registration. The `Feeder` carries the `In[T]` **instance** rather
than a way to build one, because a generic cannot be instantiated from a
`reflect.Type`, and the projection reads the event through the **same stable
cell** a named event parameter would have used — so `In` costs no allocation at
all over naming the event.

Three ways to get it wrong, each its own sentence at composition:

- an `*ecs.In[T]` no `Feed` supplies — the parameter is right and the
  registration site is missing a line;
- a `Feed` the System does not take — a projection computed every tick and
  thrown away, almost always a type that does not match;
- **one `Feed` given to two Systems.** Call `ecs.Feed` at each registration
  site. Hoisting one into a variable would have two Systems share a cell with no
  lock between them, and publications that may run concurrently writing it.

> **`In.Get()` belongs outside the loop.** `In` is a pointer to a cell the
> adapter writes, so a `Get()` inside the loop is a load the compiler cannot
> hoist past the Component writes — it has no way to prove they do not alias,
> and the disassembly confirms the load is re-issued per Entity. **On this
> implementation that costs nothing measurable** — see the cost table — because
> `All()` is a `range`-over-func, so the loop body is a separate closure that
> reloads anything it reads from outside itself anyway. Hoist it regardless: the
> rule is free to follow, the reason it might matter has not gone away, and a
> Query shape whose body does less work than this one's would show it.

### A System as a command

```go
type CountCmd kernel.Command[CountRequest, CountResponse]

func count(request CountRequest, q *ecs.Query[CountQ], answer *ecs.Resp[CountResponse]) {
    reply := CountResponse{}
    for _, it := range q.All() {
        if it.Health.HP >= request.AtLeast {
            reply.N++
        }
    }
    answer.Set(reply)
}

registrar.HandleCommand[CountCmd](ecs.ToExecute[CountRequest, CountResponse](registrar, count))
```

`ToExecute` is `ToHandler`'s command twin: the same signature, the same
classification, the same lock set, registered with `HandleCommand` instead of
`Subscribe`. The request is to a command what the event is to a subscription —
it may be named, and `Feed` projects out of it, so **one System func is both a
command and a subscription** without being written twice.

**A System still returns nothing**, so it answers through `*ecs.Resp[Res]`
instead — **recognised by its type, not by its position**, so there is no "the
last parameter is the response" convention to remember or to get wrong.
`Set` is the whole of the API, the cell is allocated once at registration, and
the value is copied out under the lock and the cell cleared behind it, so an
invocation that writes nothing answers the zero value rather than the previous
invocation's. **Zero allocations either way** — see the cost table.

Naming it is **optional**: a command that is an order rather than a question
takes no `Resp` and answers the zero value. Three refusals guard the rest:

- a `ToHandler` System naming any `Resp` — an event has no response to write
  into, and the sentence points at `ToExecute`;
- `*ecs.Resp[X]` where the command's response is `Y` — the message names both,
  because the mistake is always a copied registration line;
- naming the response twice, the same "at most once" rule the event and the
  request carry.

`Resp[T]` holds a `T`, not a `*T`. Every wrapper here is `X[T]` over the domain
type; `Read[T]`/`Write[T]` take a pointer only because a **kernel resource** is
keyed by its exact Go type, which is the kernel's rule and not this package's. A
`Resp[*T]` would also make the System supply the storage — either an allocation
an invocation, or a pointer to something that need not outlive the lock.

The error is always nil. A System has no way to fail that is not a panic, and a
panic is already `ErrPluginPanic`; expected rejection belongs in the response,
which is where the kernel asks for it anyway.

## Structural change

```go
type Projectile struct {          // a Component set: the Components a new Entity starts with
    Body     Body
    Velocity Velocity
    Collider Collider
}

func fire(sp *ecs.Spawn[Projectile], we *ecs.WriteableEntities) {
    e := sp.New(Projectile{Body: Body{X: 1}, Velocity: Velocity{X: 10}})
    we.Despawn(e)
}
```

A **structural change** is a change to which Entities have which Components, as
against a change to a Component's value. **No structural change is a Command**
(the ECS's Commands are [`ShrinkCmd`](#giving-memory-back) and the three [read
Commands](#reading-the-world-by-name), and none changes membership), and that is the part most likely to be built wrong from habit: `Spawn[S].New` and
`WriteableEntities.Despawn` are direct calls on handles the System already
holds, not messages and not a queue. There is no
exclusion mechanism to build either, because the lock set below already excludes
everyone. The deferring pair `*ecs.DeferredSpawn[S]` and `*ecs.DeferredDespawn`
queue instead, and their change is made at the next
`WriteableEntities.Drain()`; what they buy is lock duration and they are
specified in [`specs/deferred.md`](specs/deferred.md).

**Two handles, not one.** Folding `Despawn` onto `Spawn[S]` would force a
Component set type on Systems that never spawn, so a System that only retires Entities names
only `*ecs.WriteableEntities`.

A Spawn names the **Component set** a new Entity starts with as a struct type
whose field types are the Components, the way a Query is, and its value carries
the Components themselves — **a field of it simply *is* a Component field**.
There is no conversion mechanism and none is needed: a Component may hold a
string, so a declarative spawn naming a model by its path needs nothing from the
ECS.

The struct type names that set for **one act of creation and nothing more**: the
Entity may gain and lose Components afterwards and from then on the struct type
means nothing. It is not a structure the engine keeps, and nothing groups
Entities by it. A Tag is an ordinary field of it.

`Despawn` is **total and eager**: every Store is emptied of the Entity at once
and the index returns to the free list immediately, so no Store ever holds a dead
Entity. It reports whether the handle was alive to begin with, so despawning
twice is false the second time rather than an error.

**You may restructure the Entity you are currently visiting.** Despawning it
skips nobody and spawning one Entity per visited Entity terminates, both because
`All()` walks its driver backwards. Changing whether some *other* Entity is in
the driver Store is undefined, and a spawn that **grows** a Store invalidates the
pointer fields of the Query iterating it — the standing rule that no dense row
may be held across a mutation.

### The barrier, and the usage rule that follows

`Spawn` and `WriteableEntities` declare **`write{*Entities}`**, which supersedes
the read every System takes. `Entities` holds a reference to every Store, so
that is **one entry in the lock set and not N**, and it is a **total barrier**:
it excludes every System in the frame. Two consequences worth stating outright.

- A spawn whose Components are chosen at runtime has **exactly** the lock set of
  one whose Components are spelled in Go. There is nothing to name statically
  that is not already named.
- A `Spawn[S]` additionally declares `write{*Store[F]}` **per Component set field**,
  which is **redundant for locking and kept anyway, as an *ownership*
  declaration**. It is what makes cog's composition check fire, so a plugin
  spawning a `Health` must depend on `Health`'s owner. Dropping it would let any
  plugin fabricate any other plugin's Components with no declared relationship.

> **Split a rarely-spawning System out.** The `Uses` fold is static, so a System
> holds `write{*Entities}` for its **entire run**, not for the instant it
> spawns. A System that queries 5 000 Entities and spawns one projectile on the
> last of them blocks the whole frame for the duration of the query. **The cost
> is never the spawn; it is everything around it.**

**No general command buffer**, and the reason is allocation rather than taste: a
type-erased one costs one allocation per queued command, which at 30 Hz is tens
of KB a second fed to the collector during frames. What a command buffer would
buy is **lock duration**, not safety and not allocation.

## Reaching another Entity

```go
type Homing struct{ Target ecs.Entity }   // a Reference: an Entity in a Component

func home(q *ecs.Query[HomingQ], bodies *ecs.Get[Body], burning *ecs.Remove[Burning]) {
    for e, it := range q.All() {
        target, ok := bodies.Of(it.Homing.Target)
        if !ok {
            continue                       // gone, or never had a Body
        }
        it.Velocity.V = target.Pos.Sub(it.Body.Pos)
        burning.From(e)
    }
}
```

A Query reaches only what it drives over. A missile's target, a spell's owner, a
projectile's caster is an `Entity` kept inside a Component — a **Reference** —
and it is followed with an **accessor**. An `Entity` is pointer-free, so a
Reference is an ordinary Component field and earns no vocabulary of its own; so
does a bounded run of them, `[4]Entity`, whose cardinality is fixed in the type
because the engine caps nothing.

| handle | methods | declares |
| --- | --- | --- |
| `ecs.Get[T]` | `.Of(Entity) (T, bool)` | `read{*Store[T]}`, `read{*Entities}` |
| `ecs.Set[T]` | `.Of`, `.Ref(Entity) (*T, bool)`, `.UpdateFor(Entity, T)`, `.MarkChanged(Entity)` | `write{*Store[T]}`, `read{*Entities}` |
| `ecs.Remove[T]` | `.From(Entity) bool` | `write{*Store[T]}`, `read{*Entities}` |

**`Get[T]` has no `Ref`, and that is what stops a read handle being a write in
disguise.** A read yields a copy for the reason every read in this package does:
a read yielding a pointer is a data race against concurrent readers, and Go has
no pointer-to-const. A test asserts that no method on `Get` returns a pointer at
all, so it is a property of the type rather than of this paragraph.

**Each accessor declares `read{*Entities}` itself**, as well as its Store. That
is redundant while every System is built by `ToHandler`, which declares it first
and unconditionally — and it is declared here anyway, because the despawn
traversal rests on *every* route to a Store declaring it, and an invariant that
holds by the accident that everything also carries a Query is not closed.

**`Get` and `Remove` declare the read and then drop the handle**, so neither can
reach the authority at all — which is what makes "no liveness check of its own"
structural rather than a promise. `Set` keeps it, for the one question an
[insertion](#adding-and-removing-a-component) has to ask, and that second job is
what makes the declaration load-bearing rather than ceremonial.

### Adding and removing a Component

**`Set[T].UpdateFor` inserts when absent, so it is how a Component is added**, and
`Remove[T].From` is how one is taken away. Both are immediate — there is no
command buffer, because a type-erased one costs an allocation per queued command.

Inserting needs no handle of its own because it needs no authority: nothing
anywhere records which Entities have which Components, so `write{*Store[T]}` is
the whole of what adding one takes. That is the practical difference from a
spawn, and it is large:

| | declares | excludes |
| --- | --- | --- |
| `Spawn[S].New`, `WriteableEntities.Despawn` | `write{*Entities}` | **every System in the frame** |
| `Set[T].UpdateFor`, `Remove[T].From` | `write{*Store[T]}` | only Systems that touch `T` |

**`UpdateFor` is safe on the Entity a Query is currently visiting**, and on any
Entity at all with respect to the Query's own driver: an insertion appends a row,
and the backwards walk never reaches one. Removing the driver's Component from
the Entity being visited is safe for the same reason a despawn is. Doing it to
some *other* Entity of that driver is undefined, exactly as before.

**An insertion through a Reference to an Entity that no longer exists does
nothing**, and that refusal is load-bearing rather than defensive. Reading
through a dangling Reference is safe because the despawn already emptied every
Store; *inserting* through one would put a row back that no later despawn can
reach, because the despawn that would have reached it has happened. `Len` would
stop being the population, the driver scan reads `Len`, and a one-Component Query
would yield an Entity that does not exist. A System cannot make that check
itself — "does this Entity still exist" is the authority's question and no System
is handed the authority — so the accessor makes it, which is affordable for
exactly the reason it declares `read{*Entities}` in the first place. It is a load
and a compare, on the insertion path only, and it measured **0.3 ns**. Nothing is
reported, because absence is already what every accessor answers a dangling
Reference with: `Of` and `Ref` miss and `From` reports false.

### Three guarantees, and two of them are free

- **A Reference to a despawned Entity resolves to nothing, with no liveness check
  of its own.** A despawn is eager and total, so every Store was already emptied;
  the probe that would have found the row finds absence instead. `Get` and
  `Remove` keep no way to reach the authority at all, so that is a property of
  the types and not a promise about their code.
- **A recycled index never aliases a stale Reference.** The generation is in the
  sparse slot, so the compare that finds the row is the compare that rejects the
  handle. There is no window and no second structure.
- **A write pointer *is* invalidated by a structural change to that Store**, and
  this one is not free — it is the sharp edge.

### A pointer is invalidated by a structural change, in two shapes

Both shapes are silent, so both have a test rather than a sentence:

- **Swap-remove relocates.** `Remove[T].From` moves the last row into the hole,
  so the Entity that owned the last row is now somewhere else and a `Ref` taken
  before the removal addresses a slot that is nobody's. The write lands, in
  memory no Entity reads.
- **A growth abandons the array, and it takes the whole Query run with it.** A
  Query captures the driver's `owners` and every Store's arrays **once per run**,
  so an `UpdateFor` that grows a Store past its reserve mid-iteration leaves the
  rest of that run — the Query's own `*T` fields included — writing into the
  array the growth left behind. Measured as a test with a control: with the
  reserve exhausted **none** of the writes after the insertion land; with room to
  spare **all** of them do.

So: use a pointer and drop it. Nothing may be held across an `UpdateFor`, a
`From`, a spawn or a despawn — which is the standing rule that no dense row index
may be held across a mutation, seen from the other end.

### A Reference points one way

**A Query selects on presence and on nothing else**, so nothing narrows by what a
Component *contains* and **nothing anywhere lists what points at a given
Entity**. The far Entity does not know it is referenced. Finding everything that
points at a target is a Query over the Component that holds the Reference and a
comparison the System makes itself, costing the length of that one Store — an
ordinary Query and user code, requiring nothing of the ECS.

A stale slot in a `[4]Entity` is the game's to compact, and detecting one is free
on the read that was already happening. Centralising either would need the
reverse index refused above: **any global index is a global lock.**

## Reading the world by name

A caller outside Go (an Agent, a debugging tool) knows a Component only as the
string `kernel.TypeName` renders for it, the one the architecture output and
every ecs diagnostic already show: `ecs.Health`, `m.Transform`. The ecs plugin
registers three unexported read-only Commands that take that string:

| Command | request | answers |
| --- | --- | --- |
| `censusCmd` | nothing | `entities`, `freeIndices`, `indexSpace`, and `components`: every registered name with its Store's `population`, sorted by name |
| `entityCmd` | `entity`: `"7v2"`, `"Entity(7v2)"` or the decimal handle | `entity`, and `components`: every Component it carries, sorted by name, each `{name, value, error}` |
| `queryCmd` | `components` (at least one name), `limit` (0 is 50, at most 500) | `total`, `truncated`, and `entities` in ascending index order, each with only the named Components |

Nothing outside ecs dispatches them, so the root declares none of them; the mcp
provider that offers them to an Agent is ecs's own ([Offered To An
Agent](#offered-to-an-agent)). A name is resolved over the
`classes` map registration already fills, and two types rendering one name are
refused together and accepted by their package-qualified form,
`PkgPath.Name`.

**Each holds `write{*ecs.Entities}` and nothing else, and that is their price.**
A call waits for every running ECS System to release the authority, and every
System queued behind it waits until it returns; the query's limit bounds that
stall. **No frame's lock set widens**, because they are Commands and not
Systems, and a frame nobody reads from pays nothing. `Describe().Contention`
lists them as writers of `*ecs.Entities` conflicting with every System.

Values are encoded to JSON while the lock is held and decoded back with
`UseNumber` before the handler returns, so an answer shares no memory with any
Store and an Entity Reference stays exact. An `m.List` arrives as an array, an
`assets.Blob` as `{"len":N}`, a Tag as `{}`, and a value that cannot be encoded
reports `error` on that Component alone. An unknown name, a malformed or dead
Entity and a limit out of range are answered as the response's `Refusal`, naming
what would have worked; a dead Entity's refusal names the Entity holding its
index now, where one does. A handle to a free index, including the one that
index will carry next, is refused as not alive. The design record is the spec's
[Reading the world by name](specs/ecs.md#reading-the-world-by-name).

## Offered To An Agent

The plugin contributes an `mcp.Provider` from `Register`, as `ecs.McpProvider`,
and offers the three read Commands as capabilities, rendered as three tools. The
Provider and the capability bodies live in `bundles/ecs/internal`. All three are
`mcp.ReadOnly()`, so a client may auto-approve them.

- **`ecs_world`**: every registered Component name with its population, live
  Entities, free indices and the index space. The one to call first.
- **`ecs_entity`**: one Entity, given as `7v2`, `Entity(7v2)` or its decimal
  handle, with every Component it carries and its value.
- **`ecs_query`**: the Entities carrying every named Component, with those
  values, up to a limit (50 by default, at most 500), with `total` and
  `truncated`.

Each is an `mcp.Func` that dispatches its Command and answers a refusal as
`mcp.Unavailable`, the ordinary tool error an Agent reads and acts on. The
Provider holds nothing, subscribes nothing and adds no resource, so a game
nobody is debugging pays nothing for it, and an app that composes no broker
binds it to nothing. Every call carries the price above, and the three Commands
appear in `mcpserver_architecture`'s contention report as writers of
`*ecs.Entities`; the prompt text tells an Agent those pairs are the tools'. The
description prose the Agent reads is reproduced in full in
[`specs/mcp.md`](specs/mcp.md), so it is reviewed as prompt text.

## Binding: how another plugin attaches

ecsscene's recording System:

```go
func record(
    k kernel.Kernel,
    models  *ecs.Query[modelQuery],            // the Components
    meshes  *ecs.Query[meshQuery],
    lights  *ecs.Query[lightQuery],
    cameras *ecs.Query[cameraQuery],
    animations *ecs.Get[ecsscene.Animation],   // optional Components, probed
    params     *ecs.Get[ecsscene.Params],
    materials  *ecs.Get[ecsscene.Material],
    keys     *ecs.Read[*keyScratch],           // the load System's keys
    lookup   *ecs.Read[*model.Lookup],         // model's residency, read only
    viewport *ecs.Read[*gfx.Viewport],
    work *ecs.Write[*scratch],                 // the binding's own resource
    out  *ecs.Write[*gfx.OpQueue],             // the resource it draws into
)
```

**There is no binding mechanism, and that is the decision.** A plugin that is
not the ECS — physics, audio, drawing — attaches to the world by being an ordinary
plugin: it registers Components if it has any, subscribes Systems like anything
else, and reaches its own frame-local resource from inside them. `ecs.Read[T]`
and `ecs.Write[T]` are the only addition, and they wrap the
`kernel.Read`/`kernel.Write` a handler's `Lock` already declares — reached
through the signature instead of through a `Lock` func, so the resource joins the
System's lock set beside the Query's Stores, at registration, **as visible in the
signature as a Component is**. No binding type, no adapter, no registration call
of the ECS's own.

**The binding is necessarily a third plugin.** `ecs` imports only `kernel`, `libs/m`, `libs/assets` and the `bundles/mcp` root, and
a plugin like `model` or `gfx` imports nothing of `ecs`, so neither can know about the
other. That is what "no binding mechanism" means in practice — and a project not
using the ECS simply does not register that plugin and schedules no Systems.
cog ships the drawing one as [`ecsscene`](../../ecsscene/docs/README.md), the ECS's
renderer over `model`, which records to `gfx` itself and carries the prohibitions
a second binding has to keep true.

**Where an Entity stands is `m.Transform`, and nothing else.** Two Components
describing one position are unrelated to the scheduler, so two Systems writing
them run concurrently and nothing reports that they disagree; copy one way, in
one System. See ecsscene's [What a binding may not
do](../../ecsscene/docs/README.md#what-a-binding-may-not-do).

**Neither handle is a place to keep anything.** `Get` returns the value resolved
from the cell the lock covers at the start of the invocation — never one taken at
registration — so the value is refreshed per tick and valid only for the body of
the System, under the kernel's standing rule that a value read from a handle
lives only as long as the handler holds its lock. `Set` is for the few resources
reassigned wholesale rather than mutated in place, and a `Get` on the same
parameter reads it back.

**The wide lock lands in the bound plugin, not in the ECS.** A `*gfx.OpQueue`
is one resource, so every recording System serialises against every other
recording System for write, whatever Components they read — a property of the
bound plugin's API, not of the ECS. One recording System per bound plugin is the
shape.

**Publishing an event out of a System** is the other direction, and it needs
only `kernel.Kernel` in the signature. It is the one thing on this page that
allocates, and the cost is the kernel's publication rather than the ECS's: see
below.

## What it costs

Measured on a real `kernel.Engine` driven by a real `app.UpdateEvent`, AMD Ryzen
9 7950X3D, go1.27.1 windows/amd64. The **ns/op** column is the median of ten
interleaved rounds, from the same binary and session as
[What a wider Query costs](#what-a-wider-query-costs) below (re-run by
[#546](https://github.com/dvoyni/cog/issues/546) after
[#280](https://github.com/dvoyni/cog/issues/280)). The allocs/op column is
read from the tree at da4a6ae
([#283](https://github.com/dvoyni/cog/issues/283)); the B/op column was
measured before 98f84c8 took the context out of the kernel, and was not
re-measured:

| whole frame | ns/op | allocs/op | B/op, before 98f84c8 |
| --- | --- | --- | --- |
| nothing subscribed | 72 | **2** | 160 |
| hand-written subscription, 1 000 | 6 516 | **4** | 322 |
| **a System with a two-Component Query, 1 000** | **10 061** | **4** | 322 |
| …the same Query with a `Without`, a third tagged, 1 000 | 10 331 | **4** | 322 |
| hand-written subscription, 10 000 | 22 620 | **4** | 322 |
| **a System with a two-Component Query, 10 000** | **33 623** | **4** | 321 |
| …the same Query with a `Without`, a third tagged, 10 000 | 33 788 | **4** | 322 |

**The Query costs exactly what the hand-written subscription costs** —
identical at 1 000 and at 10 000 Entities, which is the real test, because an
allocation in the iteration would scale with the Entity count. That comparison,
not a formula, is what shows the ECS adds nothing; an earlier revision explained
the count with a formula over publications and subscribers, which over-counted
and is withdrawn. Over a 10 000-frame steady state, which an
average over `b.N` could hide amortised growth behind, it is **4.002 objects a
frame at 1k and 4.002 at 10k**, against the hand-written **4.005** — and **a
filter changes none of it**: 4.001 at 1k and 4.001 at 10k with a `Without` in
the Query (`TestTheFrameSitsOnTheEnginesAllocationLine`, at da4a6ae; 6.003 /
6.003 against 6.015 before 98f84c8).
`-gcflags=-m` reports no `moved to heap` anywhere on the iteration path, so the
zero is explained by the compiler rather than merely observed.

From the 1k to 10k slope, so the per-frame floor drops out: **2.62 ns an Entity
against the hand-written loop's 1.79**. The two-Component Query is shape 2, so
it takes the walk `All()` carries inside its own literal and pays no per-Entity
call. The filtered Query's slope is **2.61**, level with the unfiltered one's:
its `Without` makes it three fields, shape 3, which since #546 has a walk of
its own in `All()` too, so its extra probe costs about what the third of the
population the filter turns away saves. Under #280, when shape 3 still
delegated through `iterate` and paid an indirect `yield` an Entity, it was 3.42
against 2.64; before #422, when both delegated, 3.43 against 3.14. A `Without`
that rejects nothing costs what a third Component field costs, see below.

Three things the implementation does that those numbers depend on. Each is
invisible in a microbenchmark and each cost an allocation a tick when it was not
done:

- **The fill buffer is a field of the `Query`**, not a local in `All()`. As a
  local its address reaches an opaque `yield` and it escapes.
- **The filler is reached by a switch on a shape chosen at registration**, not
  through a func field. An indirect call is opaque to escape analysis, so the
  range statement's `yield` closure and the loop state it captures escape — two
  allocations a tick, measured here.
- **The fill is unrolled by field count.** A per-field loop costs about 2.4x a
  hand-written walk and per-field binder closures about 5x, and those allocate.
  Queries wider than four Components fall back to the loop.

### What a wider Query costs

What each additional field in a Query costs an Entity, on the shipped fillers
([#280](https://github.com/dvoyni/cog/issues/280)). The benchmark is
`BenchmarkQueryWidth` in `internal/types/widthbench_test.go`, kept in the tree
so every number here can be re-run.

**The setup.** A dedicated plugin registers eight Components `w0`…`w7`, all the
same pointer-free 8-byte shape, two Tags every Entity holds, and two Tags no
Entity ever receives. Every Entity holds every width Component and both present
Tags, set in the same order, so every Store's dense row is the Entity's index
and every Store is the same length. That is the cache-friendliest layout there
is, deliberately; other layouts were not measured. Equal lengths make the
first-declared field the Driver, and `TestTheWidthSweepDrivesOffItsFirstField`
pins that, the shape, the walk length and the yield count for every row. A
width-*k* Query probes *k*−1 fields per Entity, because the Driver's field is
never probed. The loop body is `it.W0.X += 1` everywhere, so an extra field
costs its probe and its fill and never any loop-body work. The walk is timed
alone, after one frame has bound the Query, never the whole frame. Every row
reports 0 allocs/op.

**The rows.**

- `components-k`: a Query of `w0`…`w(k−1)`, `W0` the one write, ranged over
  `All()`. This is what a System author pays. **Widths 1, 2 and 3 take a walk
  `All()` carries inside its literal** (width 2 since #422, widths 1 and 3
  since #546); width 4 delegates through `iterate` and pays an indirect `yield`
  an Entity, and widths 5 and up run `iterateWide`.
- `delegated-k`, k = 1…4: the same Queries walked by calling `q.iterate`
  directly, so every width takes the same path with one indirect `yield` an
  Entity. **These are the rows the go/no-go rule reads**, so that "the third
  field" is not confused with "the third field left the inlined literal".
- `tag-3`, `tag-4`: `W0`, `W1` and one or two present Tags as ordinary fields.
- `without-3`, `without-4`: `W0`, `W1` and one or two `Without`s that reject
  nothing. The width-3 pair take the width-3 walk through `All()`; the
  width-4 pair delegate.
- `hand-k`: the same walk written by hand over the typed Stores — `owners`
  backwards, `probe` each other Store, copy every probed value to a local and
  fold it into `W0`. `hand-1` touches only the dense row and does not even load
  `owners`, so it is a floor rather than a like-for-like baseline, and the
  width-1 ratios read high.

**How it was run.** One binary, `go test -c -o width.test
./bundles/ecs/internal/types`, ten rounds, and in each round every case as its
own invocation (`width.test -test.run '^$' -test.bench
'^BenchmarkQueryWidth$/^components-3$/^n=10000$' -test.count 1`), the case
order rotated by five each round, with the seven `BenchmarkFrame*` arms of the
table above in the same rotation. The figures are medians. AMD Ryzen 9 7950X3D,
go1.27.1 windows/amd64. The tables are #546's re-run of #280's sweep on the
code #546 landed, by the same recipe. The machine was shared with other builds
during both sessions: individual runs swing up to 2× and #280's medians sat
20–30% above a quiet run's, but the per-case minima tell the same story, and
the verdict below holds on both.

**Footprint.** A Component Store holds 8 bytes of dense row and 8 of sparse slot
an Entity (the sweep registers every Store with `ids` equal to the population,
so a `Without` probe loads a real slot rather than taking the out-of-range
return), plus the Driver's 8-byte `owners`. A Tag Store's rows are zero-width,
so a probe touches only its sparse slots. A width-*k* walk therefore touches
about 16·*k* + 8 bytes an Entity. This machine has 1 MB of L2 per core and 96 or
32 MB of L3 per CCD.

At 10 000 Entities, ns an Entity:

| width | `All()` | delegated | hand-written | `All()` × hand | delegated × hand | touched |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | **1.14** (inlined literal) | 1.77 | 0.44 | 2.6 | 4.0 | 240 KB |
| 2 | **2.25** (inlined literal) | 3.19 | 1.22 | 1.9 | 2.6 | 400 KB |
| 3 | **3.38** (inlined literal) | 4.19 | 1.96 | 1.7 | 2.1 | 560 KB |
| 4 | 6.14 | 5.71 | 2.89 | 2.1 | 2.0 | 720 KB |
| 5 | 13.80 | | | | | 880 KB |
| 6 | 18.47 | | | | | 1 040 KB |
| 7 | 27.61 | | | | | 1 200 KB |
| 8 | 37.26 | | | | | 1 360 KB |
| 2 + one present Tag | 3.06 | | | | | 480 KB |
| 2 + two present Tags | 5.40 | | | | | 560 KB |
| 2 + one `Without`, rejecting nothing | 3.03 | | | | | 480 KB |
| 2 + two `Without`s, rejecting nothing | 5.65 | | | | | 560 KB |

At 1 000 Entities:

| width | `All()` | delegated | hand-written | `All()` × hand | delegated × hand | touched |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | **1.19** (inlined literal) | 1.72 | 0.45 | 2.7 | 3.9 | 24 KB |
| 2 | **2.28** (inlined literal) | 3.16 | 1.24 | 1.8 | 2.5 | 40 KB |
| 3 | **3.40** (inlined literal) | 4.10 | 1.93 | 1.8 | 2.1 | 56 KB |
| 4 | 6.09 | 5.84 | 2.86 | 2.1 | 2.0 | 72 KB |
| 5 | 14.29 | | | | | 88 KB |
| 6 | 18.41 | | | | | 104 KB |
| 7 | 26.54 | | | | | 120 KB |
| 8 | 35.97 | | | | | 136 KB |
| 2 + one present Tag | 3.17 | | | | | 48 KB |
| 2 + two present Tags | 5.51 | | | | | 56 KB |
| 2 + one `Without`, rejecting nothing | 3.08 | | | | | 48 KB |
| 2 + two `Without`s, rejecting nothing | 5.50 | | | | | 56 KB |

**The third field is not a step.** On the delegated rows at 10 000, each field
adds **Δ2 = 1.41, Δ3 = 1.00, Δ4 = 1.53 ns** an Entity (1.44, 0.94 and 1.74 at
1 000; 1.36, 0.96 and 1.60 on the minima); #280's own session gave 1.64, 0.90
and 1.86. The rule written before #280 ran — the gap reproduces if Δ3 ≥ 3 ns
**and** Δ3 ≥ 2 × max(Δ2, Δ4) — fails on both counts in either session: the
third field is the *cheapest* of the three. The prototype's 10.8 ns for a
three-Component Query
([ecs.md](specs/ecs.md#time-and-one-number-worth-another-look)) does **not
reproduce** on the shipped Query. The hand-written walk climbs about 0.8–0.9 ns
a field at 10 000, so inside the unrolled range a Query's field costs about what
the data costs, plus, on the delegated path, the Query's fixed charge of a
filler call and one indirect `yield` an Entity.

**Through `All()`** the steps are Δ2 = 1.12, Δ3 = 1.13, Δ4 = 2.76 at 10 000
(1.09, 1.12, 2.70 at 1 000). Widths 1, 2 and 3 take a walk written out inside
`All()`'s literal, so they rise by about a field each and sit 0.6–0.9 ns an
Entity under their delegated rows; width 4 still delegates, and its 2.76 ns
step is the path change back to an indirect `yield` plus a field, not a dearer
probe. Under #280, when only width 2 had a literal, the steps were −0.09, 2.65
and 2.11. Why width 4 has no literal of its own is recorded in
[ecs.md](specs/ecs.md) beside #422's record.

**A present Tag costs no more than a Component field**, and **a `Without` that
rejects nothing costs a full probe**: both compare one sparse slot, and the
Tag's zero-width fill is free next to it. Through the width-3 walk, either as the
third field adds 0.81 and 0.77 ns an Entity over `components-2`, against 1.13 for
a third Component.

**The step at width 5 is the filler, not the cache.** Widths 1–4 run unrolled
walks; width 5 and up run `iterateWide`, the per-field loop, and pay
**about 8 ns** an Entity at the switch over the delegated width 4 and 4.5–9 ns a
field after it. It is the same
at 1 000, where every width fits in L2, as at 10 000, where widths 6–8 do not —
36.0 and 37.3 ns at width 8 — so nothing in this sweep is a cache effect.
Explaining or shrinking the per-field loop is
[#257](https://github.com/dvoyni/cog/issues/257)'s ground.

### What structural change costs

The spawn itself, on the handles a System holds, in the steady state a game runs
in — the free list has ids and the Stores have rows, so nothing here is
measuring growth. Six Component types are enrolled with the authority, and the
despawn line is linear in that number, at about 3 ns a Store. Measured as an interleaved A/B on a Ryzen 9 7950X3D with go1.27.1: `go test -c`
binaries from before and after [#282](https://github.com/dvoyni/cog/issues/282)
resolved each System's handles once a tick, ten rounds alternating which ran
first, medians. The
handle column is the per-call type assertion that resolve took away:

| | handle per call, ns/op | resolved once a tick, ns/op | allocs/op | B/op |
| --- | --- | --- | --- | --- |
| the two Stores written directly, by hand | 8.91 | 9.23 | **0** | 0 |
| **`Spawn[S].New`, a two-field Component set** | 16.67 | **14.62 — 1.58×** | **0** | 0 |
| `Spawn[S].New`, a four-field Component set | 29.68 | 28.27 | **0** | 0 |
| …the same two-field spawn, its value staged through the **parameter's address** | 23.36 | 22.35 | **1** | **16** |
| `WriteableEntities.Despawn`, asking all six Stores | 22.16 | 20.41 | **0** | 0 |

**`Spawn` stages the Component set's value through a field of the `Spawn`**,
and that is the fourth thing on the list above rather than a detail: the obvious
spelling — taking `&components` of the parameter and handing it to the cached
per-field closures — hands the address of a parameter to an opaque func value,
so the value escapes. One allocation the width of the struct, **per spawn**, and 53%
slower with it. A test holds both spellings side by side so the trap stays
closed.

The barrier, on a real frame: three Systems over 2 000 Entities, the two workers
writing different Components and the third iterating **its own** Component in
every arm, differing **only in what it declares**.

Time from [#272](https://github.com/dvoyni/cog/issues/272) (Ryzen 9 7950X3D,
go1.27.1, 2026-09-12, medians of five); allocations read from the tree at
da4a6ae (`BenchmarkBarrier*`, `-benchmem`; 10 / 11 / 11 / 11 before 98f84c8):

| the third System declares | ns/frame | allocs/op |
| --- | --- | --- |
| *(absent — two workers only)* | 13 695 | 8 |
| `read{*Entities}`, an ordinary Query | **14 907** | 9 |
| `write{*Entities}` — a `Spawn` parameter it never uses | **23 086** | 9 |
| the same, spawning and despawning one Entity a tick | **23 335** | 9 |

**The barrier costs ~8.2 µs a frame** — one scheduling round, which is what a
writer draining every reader and then releasing them costs. At 30 Hz that is
0.025% of a frame, and **it costs no allocation**. The figure is this
composition's: with the third System's Component overlapping the workers',
#272 saw the barrier collapse to about 0.4 µs, and no benchmark in the tree
reproduces that composition.

**The spawning itself is free, and the entire cost is the declaration.**
Declaring a `Spawn` and never using it costs 23 086 ns; actually spawning and
despawning every tick costs 23 335, a difference of 249 ns which is the spawn and
the despawn themselves. That is the number behind [the usage
rule](#the-barrier-and-the-usage-rule-that-follows): the cost is paid at
registration, for the System's whole run, so the remedy is a smaller System and
never a cheaper spawn.

A frame containing a structural change stays on the engine's line, over a
1 000-frame steady state with one subscriber: **4.028 objects a frame spawning
and despawning one Entity a tick, and 4.020 spawning and despawning ten
thousand** (`TestStructuralChangeStaysOnTheEnginesAllocationLine`, at da4a6ae;
6.03 and 6.01 before 98f84c8). Ten thousand structural changes a tick add
nothing, because the free list recycles the ids and the Store reuses the dense
row, so growth stops at the high-water mark. `-gcflags=-m` reports no `moved to heap` on the spawn path
either.

The exclusion is measured with a control, because an observed occupancy of 1
proves nothing unless a 2 was observable on the same harness. Two Systems
writing **different** Components:

| | greatest occupancy |
| --- | --- |
| two Queries, disjoint writes | **2** |
| the same pair, the first one spawning | **1** |

One more, which costs no allocation and is where filters were nearly paid for
twice: **a `Without` matches through the probe's existing compare**, by looking
for the absent generation instead of the Entity's own. Giving each field a bool
to compare the probe's answer against instead cost **3% of a ten-thousand-Entity
frame on every Query**, filters or not, because the probe is one compare and a
second test beside it is a third of its cost. The `or` that replaced it is not
free either — it is about 0.08 ns a probe, which is 14% of the Driver's
bad-case walk, where nearly every probe is a rejection and nothing else happens.
Making it free would mean a second set of unrolled fillers for Queries that
carry a filter, which is where monomorphised fillers would put it.

**Deferring the change, and when it pays.** The two deferring handles trade one
thing for another. A `*DeferredSpawn[S]` or a `*DeferredDespawn` declares
`read{*Entities}` where the immediate pair declares the write, so nothing in the
frame is excluded for that System's run; what it pays instead, per change, is
one atomic add on the reservation cursor, one append to a typed buffer, and a
second traversal of that buffer at the drain. The crossover is where the second
outgrows the first, and it is a measurement rather than an estimate.

Whole frame, the composition the barrier arms above use: two workers writing
different Components, the System under test iterating **its own** Component in
both arms and retiring what it created last tick, over 2 000 Entities. **The
general drainer is in both arms** — `ecs.DrainOnUpdate` is subscribed `Last`
unconditionally, so the drain node is in every composition whether anything
defers or not, and leaving it out of the immediate arm would charge the
deferring one for a barrier both pay. The created Entities carry a Component set
nothing else in the frame touches, so every walk is the same length at 1 changes
a tick and at 5 000, and only the change work varies. The immediate arm is
`*Spawn[S]` with `*WriteableEntities`, the deferring one `*DeferredSpawn[S]`
with `*DeferredDespawn`; a change is one spawn and one despawn.

Ryzen 9 7950X3D, go1.27.1, 2026-09-23, `BenchmarkCrossover*` at
`-benchtime=2000x`. Each arm ran in a process of its own and the two swapped
places between rounds, because `-count` reruns a benchmark in place rather than
interleaving and run order swings a whole-frame figure by ±10%; 24 rounds at 1
to 500 changes, 64 at 2 000 to 4 000, 13 at 5 000, minima.

| changes a tick | immediate, ns/frame | deferring, ns/frame | deferring saves |
| --- | --- | --- | --- |
| 1 | 22 572 | **18 104** | 4 468 — **19.8%** |
| 10 | 23 260 | **18 154** | 5 106 — **22.0%** |
| 100 | 27 778 | **21 225** | 6 553 — **23.6%** |
| 500 | 40 314 | **35 300** | 5 014 — **12.4%** |
| 2 000 | 86 013 | **83 870** | 2 143 — 2.5% |
| 3 000 | 117 395 | **115 119** | 2 276 — 1.9% |
| 4 000 | **148 393** | 149 173 | −780 — −0.5% |
| 5 000 | **179 158** | 185 207 | −6 049 — −3.4% |

**The crossover is about 3 700 changes a tick**, where the 3 000 arm's 1.9% lead
runs out and the 4 000 arm is level to half a percent. It is not 89, which is
what [#240](https://github.com/dvoyni/cog/issues/240)'s synthetic workload
estimated and what `deferred.md` carried until this was measured; the estimate
was low by about forty times. **Everywhere a game actually lives the deferring
handle is faster** — a fifth of a frame faster at 1 to 100 changes a tick, an
eighth at 500, which is the storm case — and nothing in the sweep the design was
built for is anywhere near the crossing. This composition prices that storm
case, 500 changes a tick, at 40.3 µs for the whole immediate frame.

Why it wins by so much for so long: the barrier the immediate arm declares is a
scheduling round the deferring arm never pays, and it is a fixed 4.5 µs whatever
the change count, while the per-change premium the deferring arm pays is 2.5 ns.
It takes thousands of changes for a 2.5 ns tax to catch a 4.5 µs one.

**The per-change premium, off the frame** — the same changes made through each
pair of handles with no scheduler between them, so what the barrier costs and
what a change costs are two numbers rather than one. `BenchmarkImmediateCycle*`
against `BenchmarkDrainCycle*`, minima of 11 interleaved rounds at
`-benchtime=200000x`: a cycle queues *n* Spawns, queues *n* Despawns of the last
cycle's Entities, and drains.

| changes a cycle | immediate, ns | queued and drained, ns | premium a change |
| --- | --- | --- | --- |
| 1 | 28.9 | 38.9 | +10.0 |
| 10 | 302.7 | 331.5 | +2.9 |
| 100 | 3 158 | 3 348 | **+1.9** |
| 500 | 15 610 | 16 840 | **+2.5** |

**A steady-state drain allocates nothing, and the benchmarks show it rather than
asserting it**: every row above is 0 B/op and 0 allocs/op under `-benchmem`,
because a drain cuts each buffer to zero length and keeps its capacity. The
premium at one change a cycle is ten nanoseconds rather than two and a half
because the drain's fixed cost — 7.2 ns for a world with two enrolled handles
and nothing queued, `BenchmarkDrainIdle` — is spread over one change instead of
five hundred. That fixed cost is the absent empty-drain skip, priced: a length
check per enrolled buffer, every drain, whether anything queued or not.

**What the reservation atomic costs.** The cursor is the one word the design
contends on, and it is the only atomic in it. `BenchmarkReserve` is the
uncontended call; `BenchmarkReserveParallel` is `RunParallel` at a sweep of
`GOMAXPROCS`, which is more pressure than a frame can put on it — a frame has as
many contenders as it has deferring Systems, not as many as the machine has
cores. Minima of seven rounds at `-benchtime=3000000x`:

| contenders on the cursor | ns a reservation |
| --- | --- |
| a serial loop | **1.51** |
| `RunParallel`, 1 | 3.10 |
| 2 | 3.68 |
| 4 | **6.04** |
| 8 | 9.96 |
| 16 | 17.92 |
| 32 | 20.83 |

At frame scale it is smaller than that, because a deferring System does more per
change than reserve. Four deferring Systems, each queuing into its own buffer and
declaring reads alone so the scheduler runs them together, against one System
making the same 400 changes by itself — each read against an idle twin with the
same signatures and the same number of nodes, since the engine charges a frame
for every node whatever it does (`BenchmarkContended*`, minima of 11 interleaved
rounds):

| | ns/frame | over its idle twin | a change |
| --- | --- | --- | --- |
| 1 System, idle | 10 210 | | |
| 1 System × 100 changes | 13 740 | 3 530 | 35.3 |
| 1 System × 400 changes | 23 568 | 13 358 | **33.4** |
| 2 Systems, idle | 14 904 | | |
| 2 Systems × 100 changes | 21 916 | 7 012 | 35.1 |
| 4 Systems, idle | 18 294 | | |
| 4 Systems × 400 changes total | 33 811 | 15 517 | **38.8** |

**Four parallel deferring Systems pay 5.4 ns a change for the cursor** — 2 159 ns
over the 400 changes, 16% of the change work and 6% of the frame — and none of
them is serialised and no lock set is widened to get it. That is the whole of
what the single word costs.

A deferring frame stays on the engine's line: **4.002 objects a frame at 1 000
Entities and 4.002 at 10 000**, against 4.001 for the same cycle made
immediately, over a 10 000-frame steady state with the deferring handles in the
signature (`TestADeferredSpawnSitsOnTheEnginesAllocationLine` and
`TestADeferredDespawnSitsOnTheEnginesAllocationLine`; the engine's line is 6).
Identical counts an order of magnitude apart are the claim, because a buffer that
grew or a drain that rebuilt one would allocate whatever the population.
`-gcflags=-m` adds no `moved to heap` for either handle, in either build mode
and with both in a System signature: the package's only two are still the
registration-time `entities` cells in `friends.go` and `shrink.go`.

### What reaching another Entity costs

This is the one figure in this package the spec did not have. `Get`, `Set` and
`Remove` were specified from measurements taken on a *model* of the Store and
were never composed as kernel-bound handles, so the spec recorded their in-situ
allocation behaviour as a **Gap** inferred from the other handles. It is now
measured, and the inference was right.

Whole frame, the homing shape — a two-Component Query over the near Entity and
one scattered probe per Entity through the Reference it carries, the target
chosen by a stride coprime with the population so the probed rows are hit in an
order unrelated to the walk. Measured as an interleaved A/B on a Ryzen 9 7950X3D with go1.27.1: `go test -c`
binaries from before and after [#282](https://github.com/dvoyni/cog/issues/282)
resolved each System's handles once a tick, ten rounds alternating which ran
first, medians.

| whole frame | handle per call, ns/op | resolved once a tick, ns/op | allocs/op |
| --- | --- | --- | --- |
| hand-written walk and probe, 1 000 | 6 473 | 6 247 | **4** |
| **Query + `Get` through a Reference, 1 000** | 11 198 | **9 399** | **4** |
| hand-written walk and probe, 10 000 | 25 150 | 25 240 | **4** |
| **Query + `Get` through a Reference, 10 000** | 57 825 | **49 549** | **4** |
| Query + `UpdateFor` and `From` per Entity, 1 000 | 17 354 | 14 684 | **4** |
| Query + `UpdateFor` and `From` per Entity, 10 000 | 128 171 | 99 534 | **4** |
| *control:* a plain Query, 1 000 | 8 793 | 8 236 | **4** |
| *control:* a plain Query, 10 000 | 31 462 | 31 324 | **4** |

**Four allocations a frame, identical at 1 000 and 10 000 Entities** — the
engine's own line, the same one `Query` and `Spawn` sit on, before the resolve
and after it. The second pair is the sharper test: it adds and removes a
Component **per Entity per tick**, so ten thousand structural changes a frame,
and it charges nothing for them. Over a ten-thousand-frame steady state:
**4.001 objects a frame at 1k and 4.002 at 10k** following a Reference, **4.001
and 4.002** adding and removing a Component per Entity, against the hand-written
**4.005**. `testing.AllocsPerRun` over the calls themselves reports **0** for
`Of`, `Ref`, `UpdateFor` and `From`. **The Gap is closed and the spec's
inference held.**

Per call, on the handles outside a frame, against the same probe with the
resource cell dereferenced once outside the loop, same A/B:

| | handle per call, ns/op | resolved once a tick, ns/op | allocs/op |
| --- | --- | --- | --- |
| the Store probed directly, the cell hoisted | 0.75 | **0.75** | 0 |
| **`Get[T].Of`** | 2.83 | **2.01** | 0 |
| `Set[T].Ref` | 3.65 | 2.70 | 0 |
| `Set[T].UpdateFor`, replacing a value | 4.33 | 3.53 | 0 |
| `Set[T].UpdateFor` inserting and `Remove[T].From` taking away | 8.84 | 6.56 | 0 |

**The finding in that first pair, and what was done about it.** The probe is the
cheap part and the spec is right that following a Reference is the same one-load
probe the Driver already pays — but the *handle* was not free:
`Read[*Store[T]].Get()` is a type assertion out of the `any`-typed resource
cell, and an accessor paid it **per call** where a Query pays it once per run
inside `bind`. From the 1k→10k slope it showed up as **5.18 ns an Entity for the
accessor shape against the hand-written 2.08**, 2.5× its baseline, where the
plain Query is 2.52.

So every parameter that reached a handle per call now resolves it **once per
invocation**: an unexported `resolver` capability, collected in the System's
`Lock` beside the Hook gates and called by the handler builder immediately
before the System's func, reads each handle into a plain field the per-call
methods use. `Get`, `Set`, `Remove`, `Spawn`, `WriteableEntities`, `Read` and
`Write` implement it; `Query` and `Hooks` already bound once a run and do not.
The accessor slope is now **4.46 ns an Entity against the hand-written 2.11**,
2.1× — the Query's own ratio — and the churning shape's fell from 12.31 to 9.43.
`Get.Of` is 0.8 ns off its per-call cost, and what is left over the bare probe
is the load of the cached field and the call, not the assertion. A plain
Query System names no resolver and pays a length check: its slope is 2.52
before and 2.57 after, and its 10k frame 31 462 against 31 324, both within
noise. Nothing is read at registration and nothing is cleared after the run;
the values cached are covered by [the same `Exclusive`
declaration](#a-system-is-not-re-entrant) as the System's arguments.

### What the binding and the projection cost

Same engine, same event, same medians. Two claims, and both are "nothing".

The allocs/op column is read from the tree at da4a6ae
([#283](https://github.com/dvoyni/cog/issues/283)); every row read 6, and the
publishing row 12, before 98f84c8.

| whole frame | ns/op | allocs/op |
| --- | --- | --- |
| a System naming `app.UpdateEvent`, 1 000 | 8 536 | **4** |
| **a System with `In` + `Feed`, 1 000** | **8 629** | **4** |
| …the same, `Get()` left inside the loop, 1 000 | 8 374 | **4** |
| a recording System — Query + `Read` + `Write`, 1 000 | 8 513 | **4** |
| a System naming `app.UpdateEvent`, 10 000 | 40 242 | **4** |
| **a System with `In` + `Feed`, 10 000** | **39 650** | **4** |
| …the same, `Get()` left inside the loop, 10 000 | 39 320 | **4** |
| a recording System — Query + `Read` + `Write`, 10 000 | 38 011 | **4** |
| a System publishing one event, 1 000 | 12 606 | **8** |

**`In` plus `Feed` costs no allocation over naming the event, and neither does a
resource in the signature** — four objects a frame, what a hand-written
subscription costs, identical at 1 000 and at 10 000 Entities. Over a
ten-thousand-frame steady state (`TestTheBoundFrameSitsOnTheEnginesAllocationLine`,
at da4a6ae): **4.002 at 1k and 4.002 at 10k** for `In` + `Feed` against **4.003
and 4.002** for naming the event, and **4.002 and 4.001** for the recording
System. `-gcflags=-m` still reports no
`moved to heap` on any path a frame or a Command runs, and `queryCursor.row` and
`queryCursor.fill` still inline. The package's one `moved to heap` is at
registration: `entities`, the cell the `ShrinkCmd` factory's two closures share
(`shrink.go`, and `friends.go` where the factory inlines), one allocation when
the plugin registers the Command, present since 144aa8a. See [What a Hook
costs](#what-a-hook-costs).

**Time is the same too**, and the three timed arms are inside this machine's
whole-frame noise of each other. That noise is several hundred nanoseconds on a
frame, so the hoist question is answered on the walk alone instead — the same
10 000-Entity two-Component walk, with the engine's publication out of the
picture:

| the walk over 10 000 Entities | ns/op | ns an Entity |
| --- | --- | --- |
| the step in a local the compiler keeps in a register | 33 794 | 3.38 |
| `In.Get()` hoisted out of the loop | 34 356 | 3.44 |
| `In.Get()` left inside the loop | 34 008 | 3.40 |

**The hoist rule's mechanism is real and its cost here is not.** The
disassembly shows the unhoisted body re-issuing the load per Entity, exactly as
the rule says — but the hoisted body issues one too, from the closure context,
because `All()` is a `range`-over-func and the loop body is a separate function.
Hoisting moves the load; it does not remove it. The spec records **~3% hoisted
and ~10% unhoisted** from the prototype; on this implementation, at this Query
shape, the spread is **under 2% and not consistently ordered**. The rule stays
in the docs — it is free to follow and the aliasing fact behind it has not
changed — but the 10% is not reproduced here and should not be quoted.

**Publishing from a System is the one thing that allocates**, and it is the
kernel's charge rather than the ECS's: a publication is a goroutine, a
completion handle and an event context. Measured over a two-thousand-frame
steady state, a System that publishes one event costs **8.051 objects a frame
against a silent System's 4.007** — **4.044 for the publication**
(`TestWhatPublishingFromASystemCosts`, at da4a6ae; 12.046 against 6.005 before
98f84c8). A
System that publishes per Entity would pay it per Entity; publish once a frame,
or not at all.

### What answering a command costs

Nothing measurable, and nothing at all in allocation. One whole invocation over
1 000 Entities — `ExecuteCommand`, the lock acquisition, the System, the answer
on its way back — with the two arms doing **identical** per-Entity work and
differing only in where the result goes:

| one command invocation, 1 000 Entities | ns/op | allocs/op | B/op |
| --- | --- | --- | --- |
| the System writes no answer, caller gets the zero response | 6 470 | **0** | 0 |
| **the System answers through `*ecs.Resp[Res]`** | **6 522** | **0** | 0 |

**52 ns on 6.5 µs, 0.8%, inside the noise — and zero objects either way**, which
is the claim that matters: a response leaving through a cell is exactly where an
allocation could appear, and none does. `testing.AllocsPerRun` over 1 000
invocations reports **0.000** for both arms.

The cell is allocated **once, at registration**, like every other parameter
object here, and `take` clears it on the way out so an invocation that answers
nothing cannot inherit the answer before it. Nothing is boxed: the `reflect.Value`
holding the cell pointer is built once and reused, the same way the event cell,
the kernel cell and every handle are.

### What a Hook costs

A System reading `*ecs.Hooks[T, K]` is specified in
[`specs/hooks.md`](specs/hooks.md), and every arm its *What it costs*
lists is measured here on the build. That section keeps the prototype's figures
as the record the build was held to; these supersede them. AMD Ryzen 9 7950X3D,
go1.27.1 windows/amd64, `NumCPU=32`, the build at 6ab00a5 with the benchmark arms
in `hooksbench_test.go` and `hooksmarkbench_test.go`. **The reference commit is
[50043cd](https://github.com/dvoyni/cog/commit/50043cd)**, the parent of the
first Hooks commit.

**[*Nothing watching*](#nothing-watching) below is re-measured** on the build
[#403](https://github.com/dvoyni/cog/issues/403) left, which is where the watch
check came within its budget. That session ran faster than the one the other
tables were taken in, so its absolute times are lower throughout; the A/B
differences are what compare. Every other table is as
[#395](https://github.com/dvoyni/cog/issues/395) measured it at 6ab00a5.

Figures are medians of five runs. Four things change that:

- **A budget row is an A/B:** test binaries of 50043cd and of the build,
  alternated with the order flipped each round, seven rounds.
- **A row priced against a control** is the median of each round's difference
  or ratio against the same world with an empty System in the reader's place,
  because the engine charges per subscription.
- **The per-act and reader-changes arms are medians of fifteen.** Their absolute
  times switch between two levels from one process to the next, by about 4 ns on
  an add plus remove pair. The difference against the control holds within a
  round.
- **Single values move by ±10%** with run order on this machine.

**Timings are published and never tested.** Tests assert only what can be
counted exactly: allocations, lock sets, occupancy, record counts and sizes.

#### Nothing watching

| against 50043cd | 50043cd | build | change | budget |
| --- | --- | --- | --- | --- |
| `UpdateFor` add plus `Remove.From` | 7.70 ns | 8.31 ns | +0.62 ns | ≤ +1.0 ns |
| `Spawn[S].New`, two fields | 15.39 ns | 15.49 ns | +0.10 ns | ≤ +1.0 ns |
| `Despawn`, six enrolled Stores | 19.53 ns | 18.96 ns | −0.57 ns | ≤ +2.0 ns |
| a two-Component Query frame, 1 000 | 8 453 ns | 8 210 ns | −2.9% | ≤ +3% |
| a two-Component Query frame, 10 000 | 38 687 ns | 38 411 ns | −0.7% | ≤ +3% |
| **the watch check, per handle per run: `Set` or `Remove`** | 189.8 ns | 193.3 ns | **+0.28 ns** | ≤ 1 ns |
| **…a `*T` Query field** | 79.45 ns | 81.37 ns | **+0.85 ns** | ≤ 1 ns |
| **…a field of a Spawn's Component set** | 69.70 ns | 70.81 ns | **+0.44 ns** | ≤ 1 ns |
| …a System naming no writer handle | 63.39 ns | 63.08 ns | −0.31 ns | |

`Despawn`'s figure moves with code placement. [#389](https://github.com/dvoyni/cog/issues/389)
measured a 1.6 ns placement effect on it and the owner accepted it, and
[#391](https://github.com/dvoyni/cog/issues/391) measured +1.28 ns. Every figure
above is within budget.

**The watch check is within its budget.** `BenchmarkHookWatchCheck` calls one run
of a System by hand on Stores nothing watches. The body does nothing but bind
what it holds. Its rows are medians of the per-round difference over 25 pooled
rounds, because the difference between two times this close is smaller than the
spread of either.
- **The Set and Remove row** holds a `Set` and a `Remove` on each of six Stores:
  twelve handles.
- **The Query row** is two `*T` fields.
- **The Spawn row** is a Component set of two fields.

**It was 2–4 ns per handle per run when [#395](https://github.com/dvoyni/cog/issues/395)
first published it**, over the budget, and the owner accepted those figures and
sent the work to [#403](https://github.com/dvoyni/cog/issues/403). Two things
were paying it, and neither had to:

- **The check ran at every run start.** A Store's watched kinds are written in
  one place, `Hooks.prepare`, at registration, so what the check computes cannot
  change after the first run makes it. It is made once now, on the System's
  first run, and no later run pays for it.
- **`rowCopy.compare` was called for every Store at run end**, even when the run
  copied no row, and it is far past the inlining budget. The `taken` flag it
  opens with is tested at the call site now, so an unwatched writer's run end is
  a load and a branch per Store instead of a call.

Measured against the build this ticket started from, that is 14.8 ns off the
twelve-handle run, 3.1 ns off the two `*T` fields and 2.4 ns off the two Spawn
fields. It is paid per handle per run, never per row.

#### Recording

An `UpdateFor` that adds plus a `Remove.From`, on a Store read under each kind
set, is priced against the same pair with nothing watching. That pair costs
8.0 ns on its own world (`BenchmarkHookNothingWatching`). Readers run off the
clock every 1 024 pairs (`BenchmarkHookAddRemove`, `BenchmarkHookAddRemoveString`).

| kind set | 1 reader | 4 readers |
| --- | --- | --- |
| `HookSpawned` | +3.0 ns | +3.1 ns |
| `HookDespawned` | +0.0 ns | −0.0 ns |
| `HookSpawnedDespawned` | +3.2 ns | +3.1 ns |
| `HookAdded` | +4.4 ns | +4.4 ns |
| `HookRemoved` | +3.0 ns | +3.0 ns |
| `HookAddedRemoved` | +4.4 ns | +4.5 ns |
| `HookAddedChanged` | +4.5 ns | +4.7 ns |
| `HookAll` | +4.6 ns | +4.8 ns |
| `HookAddedRemoved`, with a `string` in `T` | +4.9 ns | |
| `HookAll`, with a `string` in `T` | +5.5 ns | |

- **Recording is per watched Store, not per reader.** Four readers cost what one does.
- **`HookSpawned` still records the removal.** A removal fixes an addition's value,
  so it is recorded wherever an addition is watched, including an addition only a
  Spawn makes.
- **`HookDespawned` records neither half.** It watches only Despawns.

A Spawn carrying `body` and `velocity`, plus its Despawn, costs 42.6 ns with
nothing watching (`BenchmarkHookSpawnDespawn`):

| watching | added to the pair |
| --- | --- |
| `body` under `HookSpawnedDespawned` | +6.9 ns |
| `body` under `HookAll` | +9.6 ns |
| `body` and `velocity` under `HookAll` | +14.4 ns |
| `collider`, which the Spawn does not carry, under `HookAll` | +1.2 ns |

#### Reading

A reader's run, per record it is given: take, filter, fold, fill, iterate and
run end. Records are made off the clock, 1 024 at a time (`BenchmarkHookReader`,
`BenchmarkHookReaderChanges`).

| records | 1 reader | 4 readers, together |
| --- | --- | --- |
| add plus remove pairs | 6.0 ns | 18.4 ns |
| additions, filled from the live Store | 8.8 ns | 24.9 ns |
| changes, one per Entity | 8.1 ns | 23.6 ns |
| changes, two per Entity, folded into one, per change in the log | 4.9 ns | 14.7 ns |

A reader's work is paid per record. Four readers are four copies, so they cost
about three times one.

#### Changed

On a Store a `HookAddedChanged` or `HookAll` reader watches, a writer copies the
rows it is handed and compares them at its run end
(`BenchmarkHookChangedWriter`). A writer walks 10 000 Entities and writes a share
of them. The figures are per row walked, the compare included:

| rows written | `*T` field, unwatched | `*T` field, whole-Store copy | `Ref` per row, unwatched | `Ref` per row, copy per row |
| --- | --- | --- | --- | --- |
| 8 B, 0% | 2.52 ns | 3.00 ns | 3.19 ns | 10.82 ns |
| 8 B, 1% | 2.63 ns | 4.58 ns | 3.00 ns | 10.95 ns |
| 8 B, 10% | 2.59 ns | 5.69 ns | 3.26 ns | 10.85 ns |
| 8 B, 100% | 3.43 ns | 7.10 ns | 4.07 ns | 12.24 ns |
| 64 B, 0% | 2.48 ns | 5.02 ns | 3.22 ns | 12.72 ns |
| 64 B, 1% | 2.61 ns | 6.99 ns | 3.12 ns | 12.35 ns |
| 64 B, 10% | 2.58 ns | 7.48 ns | 3.26 ns | 12.75 ns |
| 64 B, 100% | 3.53 ns | 8.72 ns | 4.53 ns | 13.90 ns |

| call | unwatched | watched for Changed |
| --- | --- | --- |
| `Set[T].Ref` on 1% of 10 000, writing each row (`BenchmarkHookChangedRef`) | 4.36 ns | 12.75 ns |
| `Set[T].MarkChanged` on 1% of 10 000, per mark, its record included (`BenchmarkHookMarkChanged`) | 1.01 ns | 4.55 ns |

Two rules decide what a byte compare records.

- **A Component some reader watches for Changed has no implicit padding.** Go
  leaves padding holding whatever was there, so equal fields can differ in bytes.
  - **Validating build:** registering such a reader walks the Component's layout,
    nested structs and arrays included. It panics naming the field the gap
    follows, or "at the end", the byte count, and the fix: an explicit
    `_ [N]byte` field there.
  - **Release build:** there is no check. Implicit padding can record a Changed
    no field made, and never misses a real one.
- **A `List` nested in another `List`'s element** is written through `At`'s copy,
  which the compare cannot see. `Set[T].MarkChanged(e)` records it, and so does
  writing the whole outer element back through the stored `List`.

#### Frames

The whole frame on a real engine, each against the same frame with an empty
System in every reader's place (`BenchmarkHookFrame`):

| frame | 1 000 | 10 000 |
| --- | --- | --- |
| `churn`: 100 adds or removes a tick, `Hooks[collider, HookAddedRemoved]` | 17 499 → 19 450 ns, 1.11× | 39 505 → 40 870 ns, 1.06× |
| `churn`, `Hooks[collider, HookAll]` | 17 499 → 19 726 ns, 1.13× | 39 505 → 41 151 ns, 1.04× |
| **`churn` + `move` + `Hooks[collider, HookAddedRemoved]`** | 20 974 → 24 665 ns, 1.18× | 68 585 → 73 719 ns, **1.06×**, budget ≤ 1.10× |
| 100 spawns plus despawns a tick, `Hooks[body, HookSpawnedDespawned]` | 18 244 → 20 520 ns, 1.14× | 18 383 → 20 562 ns, 1.12× |
| **every `body` changed every tick, `Hooks[body, HookAddedChanged]`** | 18 666 → 33 443 ns, 1.80× | 55 199 → 154 821 ns, **2.80×** |
| the same, four `Hooks[body, HookAll]` readers against four empty Systems | 22 566 → 70 523 ns, 2.92× | 61 532 → 352 389 ns, 5.72× |

**The worst case is published, not budgeted.** A Changed reader over every
Entity, each changing every tick, costs about 10 ns per changed Entity at 10 000,
2.8× the frame. **Such a reader should be a Query.** A reader's work per record,
the Changed grid, the worst case and `ShrinkCmd` carry no budget: they are the
reader's trade.

#### Memory

**Allocations.** Every arm stays on the engine's line. Over a 5 000-frame steady
state (`TestAHookReaderStaysOnTheAllocationLine`), in objects a frame, read from
the tree at da4a6ae ([#283](https://github.com/dvoyni/cog/issues/283)); each
row sat two higher before 98f84c8:

| frame | control, 1k / 10k | with the readers, 1k / 10k |
| --- | --- | --- |
| `churn` + `HookAddedRemoved` | 11.061 / 11.058 | 11.043 / 11.014 |
| `churn` + `HookAll` | 11.061 / 11.058 | 11.015 / 11.009 |
| `churn` + `move` + `HookAddedRemoved` | 12.003 / 12.027 | 12.016 / 12.009 |
| spawn and despawn + `HookSpawnedDespawned` | 8.002 / 8.002 | 8.005 / 8.006 |
| `move` + `HookAddedChanged` | 8.008 / 8.013 | 8.016 / 8.009 |
| `move` + 4 × `HookAll` | 12.029 / 12.032 | 12.033 / 12.033 |

`testing.AllocsPerRun` reports 0 for `UpdateFor`, `From`, `Ref`, `MarkChanged`,
a `*T` field, Spawn and Despawn on a watched Store after its first watched run.

**A writer's first watched run** allocates its row copies, and every later run
allocates nothing (`BenchmarkHookFirstWatchedRun`, 10 000 Entities of an 8 B
Component):

| route | first watched run | later runs |
| --- | --- | --- |
| a `*T` field, the whole-Store copy | 163 840 B, 2 objects | 0 |
| `Ref` on 1% of the rows | 44 912 B, 17 objects | 0 |
| `MarkChanged` on 1% of the rows | 43 008 B, 9 objects | 0 |

The whole-Store copy is an `Entity` and a row per Entity. `Ref` and `MarkChanged`
also stamp 4 B per index in the Store's sparse array. Only `ShrinkCmd` gives
them back.

**A stalled reader's log grows by 24 B per record, plus a copy of `T` per
removal.** Capacity grows by `append`'s rule, and a test pins both. **A
removal's retained value** stays alive until the run end of the last reader
whose copy included it, and is freed then. A test checks this with a `string`
and `runtime.AddCleanup`, in both build modes.

**`ShrinkCmd`'s zero request after a spike** on a watched Store: n Entities gain
`collider`, every row is changed, all but one in a thousand are despawned, and
two `HookAll` readers read it all (`BenchmarkHookShrink`).

| spike | time | `Hooks` | `Stores` | `Entities` | `Scratch` |
| --- | --- | --- | --- | --- | --- |
| 1 000 | 13.5 µs | 257 780 B | 25 812 B | 10 720 B | 43 008 B |
| 10 000 | 25.8 µs | 2 588 638 B | 172 457 B | 24 158 B | 417 792 B |

- **The time is noisy.** It includes the Command's hand-off, and the five runs
  at 10 000 ranged from 7.3 to 55.6 µs.
- **`Hooks` includes each reader's fold marks**, with its copy.
- **`Scratch` includes a Query's walk**, which aliases a Store's owners array, so
  `Scratch` and `Stores` are not summed.

**The pace counter** is Validation mode's check that a reader keeps up. It costs
2.6 ns per counted run per log a System can append to
(`BenchmarkHookPaceCounter`). A release build compiles it out through
`const validate`.

#### Escape analysis

`-gcflags=-m`, in both build modes, shows no `moved to heap` on any of these:
- the log append in `UpdateFor`, `Remove.From`, Spawn and Despawn;
- a reader's fold and fill;
- the whole-Store and per-row copies, and `MarkChanged`;
- the compare at a writer's run end;
- `ShrinkCmd`'s execution.

**The package's one `moved to heap` is registration-time**: `entities`, the cell
the `ShrinkCmd` factory's two closures share (`shrink.go`, and `friends.go`
where the factory inlines). It is one allocation when the plugin registers the
Command, and it has been there since 144aa8a. The three [read
Commands](#reading-the-world-by-name)' factories each allocate one
`readCommand` at registration instead, as an explicit value rather than a
captured local, and their execution allocates the answer it builds; neither is
on a frame's path. Executing the Command allocates
only what the shrink itself makes: `shrinkIndices` builds a bitmap of the free
indices on every shrink that has some, and its size is known only at run time.

The [mcp Provider](#offered-to-an-agent) adds no `moved to heap` in ecs's own
source. Its bodies instantiate `kernel.ExecuteCommand` and `mcp.Func` in
`bundles/ecs/internal`, so `-gcflags=-m` on that package also prints the two
those generics carry wherever they are instantiated, as it does for input's
Provider: `stopped` at `kernel/command.go:93`, on the path of a dispatch the
stopped scheduler refused, and `settings` at
`bundles/mcp/internal/types/capability.go:70`, when a capability is
constructed. Both are on an Agent's call, and neither is on a frame's path.

### What naming an engine-side thing cost

Kept because it is the evidence for removing the name hash rather than for
having it. The benchmarks are gone with `hash.go`; these are their last numbers,
same machine, `-benchtime 1s`, medians of five, over a thousand names of about
thirty characters each.

| resolving a name, per lookup | ns |
| --- | --- |
| a dense index into a slice | **0.64** |
| the same map, value only, no text in the row | 5.45 |
| `Names.Lookup`, the stored hash | 6.66 |
| a `map[string]V` keyed by the path | **7.79** |
| hashing the string on every lookup | 25.19 |

**1.1 ns** between the hash and the name it was standing in for. That is what
`HashOf`, `HashKey`, `NoHash`, `Names[K, V]`, a glossary entry apiece for *Hash*
and *Name table*, and about 190 lines of package were buying once a Component
could hold the string itself — roughly 5.5 µs a frame at 5 000 drawables.

Two numbers from the same suite are worth carrying into whatever replaces it. A
dense index is **10× cheaper than either**, and is what a consumer should resolve
*to*. And producing a hash was free — 1.44 ns for a four-byte clip name, zero
allocations, inlined at every call site — so a consumer that wants a hash's
process-stable property pays nothing at the producing end to keep it.
