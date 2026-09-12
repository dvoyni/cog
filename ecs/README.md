# ecs

`github.com/dvoyni/cog/ecs` lets an app describe things in the world as
**Entities** carrying **Components**, and describe behaviour as **Systems** —
plain Go funcs whose parameter types say what they touch.

**A Component moves, a Query can be narrowed, Entities can be created and
retired, one Entity can reach another, a Component can name an engine-side
thing, and another plugin attaches with no mechanism at all: the storage, the
registration, the Query, its filters, structural change, the accessors, the name
hash and its table, the resource and event handles, and both handler builders
are here.**
[`docs/specs/ecs.md`](docs/specs/ecs.md) is the specification the whole plugin is
judged against.

## Files

`contract.go` holds the package documentation and `Entity`; `component.go` the
`PointerFree` rule and `RegisterComponent`; `entities.go` the `Entities`
authority; `store.go` the `Store`, the type-erased `storeCore` a despawn reaches
every Store through, and the erased header a Query fills from; `query.go` the
`Query`, its driver and its fillers; `filter.go` the `Without` and `With` field
types; `spawn.go` the `Spawn` and `WriteableEntities` handles; `accessor.go` the
`Get`, `Set` and `Remove` accessors; `hash.go` the `HashOf` name hash and the
`Names` table a consumer resolves one through; `resource.go` the `Read` and `Write`
handles that name another plugin's resource; `in.go` the `In` cell and the
`Feed` that fills it; `response.go` the `Resp` cell a command answers through;
`system.go` the `ToHandler` and `ToExecute` builders and the parameter
classification both share; `plugin.go` the plugin that publishes the authority.

## Dependencies

- Go packages: the standard library and `kernel`
- Plugin dependencies: none
- Configuration: none

## Composing

```go
world := ecs.NewEntities(maxIDs)
kernel.New(config).WithPlugins(ecs.Plugin(world), physics.Plugin(world), game.Plugin(world))
```

The world handle is **a plain Go value threaded through plugin constructors**,
and it has to be: both component registration and the handler builder need it at
registration, where no handler is running and no resource value may be read. So
the binding shape is fixed before `WithPlugins` is called and is visible in the
composition root. `ecs.Plugin` publishes the authority as the `*Entities`
resource every System holds for read and every structural change will hold for
write; it registers nothing else, because Components are registered by the
plugins that define them and Systems are ordinary subscriptions.

## Entity

```go
type Entity uint64
const NoEntity Entity = 0
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
entities := ecs.NewEntities(maxIDs)   // at the composition root
entities.Alive(e)                     // does this handle name an entity that exists?
```

`Entities` is the id authority: it allocates indices, tracks their generations,
answers `Alive`, and holds a reference to every `Store`. There is exactly one
per Engine, and that is what makes an Engine the boundary of one simulation — a
second simulation is a second Engine. **No identifier in this package contains
the word `World`**, and a test enforces it.

`NewEntities(ids)` reserves room for `ids` indices. The number is the peak
concurrent entity count the app expects, **not a cap**: exceeding it costs a
growth, not an error. It is built at the composition root because both component
registration and the handler builder need the value at registration, where no
resource may be read.

Indices are **recycled through a free list**, which is what bounds every Store's
flat sparse index by *peak concurrent* entities rather than by entities ever
created. Reclamation is **eager and is therefore nothing at all**: a despawn
empties every Store immediately and the index returns to the free list at once.
There is no compaction, no shrink and no sweep anywhere in the package.

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
Store is asked, through an interface carrying exactly one method. That is also
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
doubles repeatedly. **Nothing shrinks** — the arrays are re-sliced, never handed
back, so a respawn reuses the row.

Two consequences of swap-remove are contract:

- **No dense row index may be held across a mutation**, and a `Ref` is valid
  only until the Store next changes structurally.
- **Iteration order is unspecified.** Direction is not: a forward walk that
  removes as it goes silently skips — measured here at 667 of 1000 — because the
  tail row drops into the hole and the next step goes straight past it. A
  reverse walk visits everyone, which is what the Query's `All()` rests on.

A **Tag** — a Component with no fields — is an ordinary Store whose rows carry
nothing, so its dense array costs no memory however many entities it holds.

## Components: the pointer-free rule

```go
func PointerFree(t reflect.Type) error
```

**A Component type contains no pointers, transitively.** That replaces "a plain
copyable struct" and is better because it is mechanically checkable: this walk,
run once when the type is registered. It forbids pointers, slices, maps,
channels, funcs, interfaces and **strings**, and permits numerics, bools,
fixed-size arrays, `Entity`, and structs of those.

The reason is copying before it is the collector: a Component is copied into and
out of a Store by value and must stay meaningful after the thing it was copied
from is gone. Being pointer-free also makes it trivially serialisable, and puts
the dense array in a span the mark phase never walks.

The error names the offending field **by path**, because the field that fails is
usually several structs down and naming only the Component is useless:

```
ecs.PathedDrawable.Deep.Inner.Path is a string, which is not pointer-free
```

An engine-side thing is named by a hash of its name rather than by the name
itself. Variable-length data has three answers: a child entity with an owning
reference, a fixed-capacity array where the bound is small and real, or a hash.
`sync` types are not special-cased — a mutex is pointer-free by this walk and is
still wrong in a Component, for the same reason `go vet` already says so.

## Naming an engine-side thing

```go
type HashKey interface{ ~uint64 }

func HashOf[K HashKey](text string) K
const NoHash = 0
```

**A Component names an engine-side thing — a model, a clip, a node, a pass tag —
by the 64-bit hash of its name.** The name is a string and a Component holds no
pointers; a hash is a plain number, so it may.

```go
type ClipHash uint64                             // the caller's own named type
var walkClip = ecs.HashOf[ClipHash]("Walk")      // package level, at init

func animate(q *ecs.Query[AnimQ]) {              // two Components, nothing else
    for _, it := range q.All() {
        if it.Motion.Speed > 0.1 {
            it.Anim.Clip = walkClip
        }
    }
}
```

`HashKey` constrains `K` to `~uint64`, so the hash **is** the caller's type:
nothing to unwrap, directly comparable, printable, usable as a map key, and
pointer-free because the constraint admits nothing else. `ClipHash` and
`ModelHash` stay distinct without a second type parameter, so a clip name cannot
be assigned where a model name belongs; a codebase that does not want the
distinction declares one such type and is done. `~uint64` rather than `~int`
because `int` is 32 bits on some platforms, and a hash that differs by where the
game was built has given up its one advantage over an assigned index.

**The property that decides this against interning is that producing a hash
needs nothing.** Hashing is pure, so a System changes what an Entity names
holding only the lock it already has on the Component. Interning is what
*assigns* the id, so the table would join the lock set of every System that ever
changed a clip — and the common case is mutation, a clip going from `"Idle"` to
`"Walk"` while the game runs, which a registration-time conversion never reaches.
An assigned id also cannot be a package-level `var`: there is no table at package
initialisation, and two Engines mean two tables.
`TestANameIsTheSameEverywhereAndAnIndexIsNot` is that stated as a test — two
tables see the same two paths in opposite orders and one string comes out as `0`
and as `1`, against one hash.

The hash is **FNV-1a 64**, written out in five lines rather than taken from
`hash/fnv`, which is an interface over a `[]byte` `Write` and so allocates on a
string and cannot inline. `TestTheHashIsFNV1a64` audits those five lines against
the standard library, so the wire format is pinned by an implementation that is
not this one: a hash that survives a save file is worth nothing if a later edit
quietly renumbers every name already written down.

`NoHash` is `0` and is untyped, so it compares against any `HashKey` without a
conversion and an unset Component field already means "no name". Nothing a game
writes down lands on it: the offset basis is the hash of the empty string, which
is not zero, and reaching zero needs the state after the second-to-last byte to
be a value below 256 — and `Names.Register` rejects the residue outright, so no
**resolvable** name is `NoHash` at all.

### The name table belongs to the consumer

```go
type Names[K HashKey, V any] struct{ … }

func (n *Names[K, V]) Register(text string, value V) error
func (n *Names[K, V]) Lookup(key K) (V, bool)
func (n *Names[K, V]) TextOf(key K) (string, bool)
```

`Names` is the reverse half, and **where it lives is the whole point**: it
belongs to the plugin that resolves names into things — a model table, a clip
table — under that plugin's own lock, read once per draw where a lock is held
anyway. **One** System declares it, rather than every System that ever assigns a
name. There is deliberately no process-wide interner.

It therefore has **no lock of its own and wants none**. A `Names` is an ordinary
value inside the resource that holds it, so its lock set is that resource's,
declared with `ecs.Read` or `ecs.Write` and taken by the kernel at registration
like any other. A mutex inside it would be the global lock this design exists to
avoid, paid on every draw. Its zero value is ready to use, and the owning plugin
fills it during Registration or Startup, before any System reads it.

`Register` is also where a **collision** is caught. Sixty-four bits makes one
vanishingly unlikely — about 3×10⁻¹² at ten thousand distinct names — but
vanishingly unlikely is not impossible, and an undetected one silently draws the
wrong model. Every resolvable name passes through `Register`, so one compare
turns the class into a startup error naming both strings. Re-registering a text
already registered replaces its value and is not an error: the table is keyed by
name, and the later registration is the one the manifest meant.

`TextOf` recovers the string, because a hash is opaque in a debugger and has to
stay legible in tooling. That is why a row keeps its text beside its value, and
it is the one structural choice here with a price: **1.2 ns a lookup**, measured
below against the same probe into a map holding the value alone.

**Nothing cleans the table because nothing accumulates in it.** It holds what the
consumer registered — its asset manifest, fixed at startup. Asking about a name
nobody declared answers that there is no such thing and leaves it the size it
was: `TestHashingAccumulatesNothing` hashes **1 000 000** procedural names
against eight declared models and the table still holds eight entries.

> **v1 hashes strings and nothing else** — a model, a clip, a node, a pass tag.
> Per-entity variable-length data is a different problem with a different answer
> ([#264](https://github.com/dvoyni/cog/issues/264)), and content-addressing is
> the wrong tool for it: such a table must hold the buffer itself, so it grows
> with every distinct value the game ever produces, and the only way to empty it
> is to count how many Entities still refer to each entry. Nothing can empty the
> table. What makes a manifest safe to never clean is that it is a manifest.

A plugin a System will record into must offer a **pointer-free handle** for
everything a Component needs to name; where it does not, the app makes its own
table. A dense index is not wrong — a consumer may index internally all it likes,
and it is the cheapest thing measured below. It is simply not the *Component's*
business, because it is not the same number in the next process.

## Component registration

```go
func RegisterComponent[C any](registrar *kernel.Registrar, en *ecs.Entities, ids uint32) *ecs.Store[C]
```

A Component type is registered **explicitly, once, by exactly one plugin**.
`RegisterComponent` checks the pointer-free rule, creates the Store, enrols it
with the authority, hands it to the kernel as an ordinary resource of type
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
an ordinary field — a Tag is just another Store, it costs no memory however many
entities it holds, and it is a very good driver because the scan picks it.

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

**A filter can never be the driver, and a Query must name at least one
present-typed Component or Tag.** `Without[T]`'s `owners` lists exactly the
Entities to *exclude* and nothing enumerates the complement, so a Query of
filters alone has nothing to walk and fails at registration:

```
plugin "systems" panicked in Register: ecs: Query game.FiltersOnlyQuery names
no present-typed Component, so nothing can drive it: a filter names the
Entities to exclude and nothing enumerates the rest, so a Query needs at least
one Component or Tag it matches on presence
```

`With[T]` does not drive either, although its `owners` would serve. **If you
want a Tag to drive, name it as an ordinary field** — a Tag yields nothing into
the struct anyway, so `Solid Solid` costs exactly what `_ ecs.With[Solid]` costs
and can be chosen as the driver. `With[T]` is for presence you want matched but
not copied, where `T` is not a Tag.


## System

```go
type MoveSystem kernel.Subscription[app.UpdateEvent]

registrar.Subscribe[MoveSystem](ecs.ToHandler[app.UpdateEvent](world, move)).After[GravitySystem]()
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
| `*ecs.Spawn[B]` | `write{*Entities}` + `write{*Store[F]}` per Bundle field | creating Entities |
| `*ecs.WriteableEntities` | `write{*Entities}` | despawning |
| `*ecs.Get[T]` | `read{*Store[T]}` + `read{*Entities}` | reading one Component of an Entity it did not iterate to |
| `*ecs.Set[T]`, `*ecs.Remove[T]` | `write{*Store[T]}` + `read{*Entities}` | writing, inserting or taking away the same |
| `*ecs.Read[T]`, `*ecs.Write[T]` | the kernel's own read/write on `T` | any other plugin's resource |
| `*ecs.In[T]` | nothing | a value projected out of the event or request |
| `*ecs.Resp[Res]` | nothing | **a command only** — the answer it writes |
| `kernel.Kernel` | nothing | the kernel value, for publishing an event |
| the event or request value | nothing | legal, and not the default shape |

**Anything else is a composition-time failure naming the System's type.** This
is a mistake every new user makes once, so the diagnostic matters more than the
mechanism. The failure is a registration-time panic, which the plugin boundary
reports as `ErrPluginPanic` naming the plugin — the same route a Query naming a
Component no plugin registered takes:

```
plugin "systems" panicked in Register: ecs: Query game.GuardedQ names
unregistered Component game.Guarded
```

That names the **Component** and the **Query**, rather than the store type the
user never wrote. Which of the three answers the specs record for this
diagnostic is right is still open
([#279](https://github.com/dvoyni/cog/issues/279)); the panic is the one taken
here, because the alternative — a sentence in the composition error list — needs
a way for a plugin-side builder to reach `kernel`'s private error list, and
nothing else in the ECS needs that.

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

### The event does not belong in the signature

```go
func advance(q *ecs.Query[AdvanceQ], dt *ecs.In[float64]) {
    step := dt.Get()                              // once, outside the loop
    for _, it := range q.All() { … }
}

ecs.ToHandler[app.UpdateEvent](world, advance, ecs.Feed(func(e app.UpdateEvent) float64 { return e.Dt }))
ecs.ToHandler[FixedTick](world, advance,       ecs.Feed(func(e FixedTick) float64 { return e.Step }))
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

registrar.HandleCommand[CountCmd](ecs.ToExecute[CountRequest, CountResponse](world, count))
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
type Projectile struct {          // a Bundle: the Components one act of creation makes
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
against a change to a Component's value. **Nothing here is a Command**, and that
is the part most likely to be built wrong from habit: `Spawn[B].New` and
`WriteableEntities.Despawn` are direct calls on handles the System already
holds, not messages, not a queue and not a deferred buffer. There is no
exclusion mechanism to build either, because the lock set below already excludes
everyone.

**Two handles, not one.** Folding `Despawn` onto `Spawn[B]` would force a Bundle
type on Systems that never spawn, so a System that only retires Entities names
only `*ecs.WriteableEntities`.

A **Bundle** is a struct type whose field types are the Components, the way a
Query is — and **a Bundle field simply *is* a Component field**. There is no
conversion mechanism and none is needed: hashing is pure, so the hash that names
an engine-side thing can be computed into a package-level `var` and a
declarative spawn naming a model by name needs nothing from the ECS.

A Bundle is **not a Component set**: it describes one act of creation, and the
Entity may gain and lose Components afterwards without the Bundle meaning
anything. A Tag is an ordinary Bundle field.

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
- A `Spawn[B]` additionally declares `write{*Store[F]}` **per Bundle field**,
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
| `ecs.Set[T]` | `.Of`, `.Ref(Entity) (*T, bool)`, `.UpdateFor(Entity, T)` | `write{*Store[T]}`, `read{*Entities}` |
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
| `Spawn[B].New`, `WriteableEntities.Despawn` | `write{*Entities}` | **every System in the frame** |
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

## Binding: how another plugin attaches

```go
func recordDraws(
    q      *ecs.Query[DrawQ],                  // the Components
    models *ecs.Read[*scene.Names],            // a resource, read
    out    *ecs.Write[*scene.OpQueue],         // a resource, written
) {
    table, queue := models.Get(), out.Get()
    for _, it := range q.All() {
        path, _ := table.Lookup(it.D.Model)
        queue.Model(it.D.Layers, path, scene.ModelDraw{ /* … */ })
    }
}
```

**There is no binding mechanism, and that is the decision.** A plugin that is
not the ECS — physics, audio, scene — attaches to the world by being an ordinary
plugin: it registers Components if it has any, subscribes Systems like anything
else, and reaches its own frame-local resource from inside them. `ecs.Read[T]`
and `ecs.Write[T]` are the only addition, and they wrap the
`kernel.Read`/`kernel.Write` a handler's `Lock` already declares — reached
through the signature instead of through a `Lock` func, so the resource joins the
System's lock set beside the Query's Stores, at registration, **as visible in the
signature as a Component is**. No binding type, no adapter, no registration call
of the ECS's own.

**The binding is necessarily a third plugin.** `ecs` imports only `kernel`, and
a plugin like `scene` imports nothing of `ecs`, so neither can know about the
other. That is what "no binding mechanism" means in practice — and a project not
using the ECS simply does not register that plugin and schedules no Systems.
cog ships the ecs↔scene one as [`ecsscene`](../ecsscene/README.md), which is the
whole shape in one System and carries the prohibitions a second binding has to
keep true.

**Neither handle is a place to keep anything.** `Get` goes to the cell the lock
covers on every call, so the value is refreshed per tick and valid only for the
body of the System, under the kernel's standing rule that a value read from a
handle lives only as long as the handler holds its lock. `Set` is for the few
resources reassigned wholesale rather than mutated in place.

**The wide lock lands in the bound plugin, not in the ECS.** A `*scene.OpQueue`
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
9 7950X3D, go1.27.1 windows/amd64, medians of five:

| whole frame | ns/op | allocs/op | B/op |
| --- | --- | --- | --- |
| nothing subscribed | 81 | **2** | 160 |
| hand-written subscription, 1 000 | 5 185 | **6** | 322 |
| **a System with a two-Component Query, 1 000** | **7 949** | **6** | 322 |
| …the same Query with a `Without`, a third tagged, 1 000 | 8 201 | **6** | 322 |
| hand-written subscription, 10 000 | 20 142 | **6** | 322 |
| **a System with a two-Component Query, 10 000** | **38 783** | **6** | 321 |
| …the same Query with a `Without`, a third tagged, 10 000 | 36 506 | **6** | 322 |

**The engine charges 2 per publication plus 4 per subscriber, and the Query
lands exactly on that line** — identical at 1 000 and at 10 000 Entities, which
is the real test, because an allocation in the iteration would scale with the
Entity count. Over a 10 000-frame steady state, which an average over `b.N`
could hide amortised growth behind, it is **6.003 objects a frame at 1k and
6.003 at 10k**, against the hand-written **6.015** — and **a filter changes
none of it**: 6.003 at 1k and 6.001 at 10k with a `Without` in the Query.
`-gcflags=-m` reports no `moved to heap` anywhere on the iteration path, so the
zero is explained by the compiler rather than merely observed.

From the 1k to 10k slope, so the per-frame floor drops out: **3.43 ns an Entity
against the hand-written loop's 1.66**. The filtered Query's slope is **3.14**,
lower than the unfiltered one's rather than higher, because a third of the
population fails the `Without` probe and never reaches the fill or the loop
body: **a filter is cheaper than the work it removes**, which is the whole
reason to reach for one.

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

### What structural change costs

The spawn itself, on the handles a System holds, in the steady state a game runs
in — the free list has ids and the Stores have rows, so nothing here is
measuring growth. Six Component types are enrolled with the authority, and the
despawn line is linear in that number, at about 3 ns a Store:

| | ns/op | allocs/op | B/op |
| --- | --- | --- | --- |
| the two Stores written directly, by hand | 8.53 | **0** | 0 |
| **`Spawn[B].New`, a two-field Bundle** | **14.50 — 1.70×** | **0** | 0 |
| `Spawn[B].New`, a four-field Bundle | 27.59 | **0** | 0 |
| …the same two-field spawn, bundle staged through the **parameter's address** | 21.21 | **1** | **16** |
| `WriteableEntities.Despawn`, asking all six Stores | 18.50 | **0** | 0 |

**`Spawn` stages its bundle through a field of the `Spawn`**, and that is the
fourth thing on the list above rather than a detail: the obvious spelling —
taking `&bundle` of the parameter and handing it to the cached per-field
closures — hands the address of a parameter to an opaque func value, so the
bundle escapes. One allocation the width of the Bundle, **per spawn**, and 46%
slower with it. A test holds both spellings side by side so the trap stays
closed.

The barrier, on a real frame: three Systems over 2 000 Entities, the two workers
writing different Components and the third iterating **its own** Component in
every arm, differing **only in what it declares**.

| the third System declares | ns/frame | allocs/op |
| --- | --- | --- |
| *(absent — two workers only)* | 13 695 | 10 |
| `read{*Entities}`, an ordinary Query | **14 907** | 11 |
| `write{*Entities}` — a `Spawn` parameter it never uses | **23 086** | 11 |
| the same, spawning and despawning one Entity a tick | **23 335** | 11 |

**The barrier costs ~8.2 µs a frame** — one scheduling round, which is what a
writer draining every reader and then releasing them costs. At 30 Hz that is
0.025% of a frame, and **it costs no allocation**.

**The spawning itself is free, and the entire cost is the declaration.**
Declaring a `Spawn` and never using it costs 23 086 ns; actually spawning and
despawning every tick costs 23 335, a difference of 249 ns which is the spawn and
the despawn themselves. That is the number behind [the usage
rule](#the-barrier-and-the-usage-rule-that-follows): the cost is paid at
registration, for the System's whole run, so the remedy is a smaller System and
never a cheaper spawn.

A frame containing a structural change stays on the engine's line, over a
1 000-frame steady state with one subscriber: **6.03 objects a frame spawning
and despawning one Entity a tick, and 6.01 spawning and despawning ten
thousand**. Ten thousand structural changes a tick add nothing, because the free
list recycles the ids and the Store reuses the dense row, so growth stops at the
high-water mark. `-gcflags=-m` reports no `moved to heap` on the spawn path
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

### What reaching another Entity costs

This is the one figure in this package the spec did not have. `Get`, `Set` and
`Remove` were specified from measurements taken on a *model* of the Store and
were never composed as kernel-bound handles, so the spec recorded their in-situ
allocation behaviour as a **Gap** inferred from the other handles. It is now
measured, and the inference was right.

Whole frame, the homing shape — a two-Component Query over the near Entity and
one scattered probe per Entity through the Reference it carries, the target
chosen by a stride coprime with the population so the probed rows are hit in an
order unrelated to the walk:

| whole frame | ns/op | allocs/op |
| --- | --- | --- |
| hand-written walk and probe, 1 000 | 5 424 | **6** |
| **Query + `Get` through a Reference, 1 000** | **9 834** | **6** |
| hand-written walk and probe, 10 000 | 23 230 | **6** |
| **Query + `Get` through a Reference, 10 000** | **60 485** | **6** |
| Query + `UpdateFor` and `From` per Entity, 1 000 | 16 920 | **6** |
| Query + `UpdateFor` and `From` per Entity, 10 000 | 120 229 | **6** |

**Six allocations a frame, identical at 1 000 and 10 000 Entities** — the
engine's own 2-per-publication-plus-4-per-subscriber line, the same one `Query`
and `Spawn` sit on. The second pair is the sharper test: it adds and removes a
Component **per Entity per tick**, so ten thousand structural changes a frame,
and it charges nothing for them. Over a ten-thousand-frame steady state:
**6.004 objects a frame at 1k and 6.003 at 10k** following a Reference, **6.003
and 6.001** adding and removing a Component per Entity, against the hand-written
**6.010**. `testing.AllocsPerRun` over the calls themselves reports **0** for
`Of`, `Ref`, `UpdateFor` and `From`. **The Gap is closed and the spec's
inference held.**

Per call, on the handles outside a frame, against the same probe with the
resource cell dereferenced once outside the loop:

| | ns/op | allocs/op |
| --- | --- | --- |
| the Store probed directly, the cell hoisted | **0.69** | 0 |
| **`Get[T].Of`** | **2.65** | 0 |
| `Set[T].Ref` | 3.17 | 0 |
| `Set[T].UpdateFor`, replacing a value | 3.66 | 0 |
| `Set[T].UpdateFor` inserting and `Remove[T].From` taking away | 7.21 | 0 |

**And there is a finding in that first pair.** The probe is the cheap part and
the spec is right that following a Reference is the same one-load probe the
Driver already pays — but the *handle* is not free: `Read[*Store[T]].Get()` is a
type assertion out of the `any`-typed resource cell, it costs **~2.0 ns**, and
an accessor pays it **per call** where a Query pays it once per run inside
`bind`. That is 3.9× the bare probe and it is the whole of the difference. From
the 1k→10k slope it shows up as **5.63 ns an Entity for the accessor shape
against the hand-written 1.98**, where the plain Query is 3.32 against 1.56 —
so the accessor arm is 2.8× its baseline where the Query is 2.1×.

It costs no allocation and it is not on the Query's path, so nothing here fails
the bar this design is held to. The remedy, if the number ever matters, is to
resolve the Store **once per tick** rather than once per call, which the handler
builder is the only thing positioned to do — and which is a change to the
parameter seam rather than to an accessor, so it is not made here.

### What the binding and the projection cost

Same engine, same event, same medians. Two claims, and both are "nothing".

| whole frame | ns/op | allocs/op |
| --- | --- | --- |
| a System naming `app.UpdateEvent`, 1 000 | 8 536 | **6** |
| **a System with `In` + `Feed`, 1 000** | **8 629** | **6** |
| …the same, `Get()` left inside the loop, 1 000 | 8 374 | **6** |
| a recording System — Query + `Read` + `Write`, 1 000 | 8 513 | **6** |
| a System naming `app.UpdateEvent`, 10 000 | 40 242 | **6** |
| **a System with `In` + `Feed`, 10 000** | **39 650** | **6** |
| …the same, `Get()` left inside the loop, 10 000 | 39 320 | **6** |
| a recording System — Query + `Read` + `Write`, 10 000 | 38 011 | **6** |
| a System publishing one event, 1 000 | 12 606 | **12** |

**`In` plus `Feed` costs no allocation over naming the event, and neither does a
resource in the signature** — six objects a frame, the engine's own
2-per-publication-plus-4-per-subscriber line, identical at 1 000 and at 10 000
Entities. Over a ten-thousand-frame steady state: **6.004 at 1k and 6.001 at
10k** for `In` + `Feed` against **6.004 and 6.003** for naming the event, and
**6.002 at both** for the recording System. `-gcflags=-m` still reports no
`moved to heap` anywhere in the package, and `queryCursor.row` and
`queryCursor.fill` still inline.

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
steady state, a System that publishes one event costs **12.046 objects a frame
against a silent System's 6.005** — **6.04 for the publication**, which is one
more publication's worth of exactly the line every other row here sits on. A
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

### What naming an engine-side thing costs

Microbenchmarks, not a frame: producing a hash and resolving one are both too
small to read off a whole-frame number. Same machine, `-benchtime 1s`, medians
of five. A thousand names of about thirty characters each, which is what a
manifest of model paths looks like.

| producing a name | ns/op | allocs/op |
| --- | --- | --- |
| **`HashOf` of a clip name, four bytes** | **1.44** | **0** |
| `HashOf` of a 28-byte path, a constant | 13.83 | **0** |
| `HashOf` of a 28-byte path the compiler cannot fold | 13.47 | **0** |

**Zero, and it has to be zero**: a System that changes what an Entity names does
this per Entity per tick, and `-gcflags=-m` reports `HashOf` inlined at every
call site with nothing escaping. FNV-1a is a byte at a time on a serial
multiply, so the cost is the length of the name and nothing else — which is the
argument for hashing a clip name where a path is not needed, and for hashing at
`init` where the name is a constant.

| resolving a name, per lookup | ns |
| --- | --- |
| a dense index into a slice | **0.64** |
| the same map, value only, no text in the row | 5.45 |
| **`Names.Lookup`, the stored hash** | **6.66** |
| `Names.TextOf`, the same probe for tooling | 6.42 |
| `Names.Lookup` of a name nobody registered | 5.24 |
| a `map[string]V` keyed by the path | 7.79 |
| hashing the string on every lookup | 25.19 |

Three things those numbers say. **A stored hash beats the path it came from** by
about 1.1 ns, and beats re-hashing it by 3.8×, which is the whole reason the
Component carries the hash rather than the name or the name's source. **A dense
index is 10× cheaper still** and is exactly what a consumer should use
internally once it has resolved — the hash is the *Component's* business, not
the table's. And **a miss is cheaper than a hit**, which is what makes the
two-million-probe sweep in `TestHashingAccumulatesNothing` finish in 40 ms and
leave the table at eight entries.

The **1.2 ns** between a bare map and `Names.Lookup` is the text the row carries
so `TextOf` can make a hash legible again. That is the one structural choice
here and it is deliberate: 1.2 ns on a per-draw probe buys a hash that is
readable in a debugger and a collision that is a named startup error rather than
a wrong model. Narrowing it further — a row holding an index into a text slice
rather than the string — is available and has not been spent.

| building the table | ns/op | allocs/op | B/op |
| --- | --- | --- | --- |
| `Register`, a manifest of 1 000 names | 89 434 — **89 ns a name** | 22 | 226 056 |

A startup cost, reported rather than defended. The 22 allocations are the map's
growth steps, not a per-name charge.
