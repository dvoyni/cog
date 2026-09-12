# ecs

`github.com/dvoyni/cog/ecs` lets an app describe things in the world as
**Entities** carrying **Components**, and describe behaviour as **Systems** —
plain Go funcs whose parameter types say what they touch.

**A Component moves: the storage, the registration, the Query and the handler
builder are here.** What is not here yet is filters (`Without`, `With`),
spawning and despawning, the accessors that reach an Entity a System did not
iterate to, and the resource and event handles a binding uses;
[`docs/specs/ecs.md`](docs/specs/ecs.md) is the specification the whole plugin
is judged against, and this README covers only what is built.

## Files

`contract.go` holds the package documentation and `Entity`; `component.go` the
`PointerFree` rule and `RegisterComponent`; `entities.go` the `Entities`
authority; `store.go` the `Store`, the type-erased `storeCore` a despawn reaches
every Store through, and the erased header a Query fills from; `query.go` the
`Query`, its driver and its fillers; `system.go` the `ToHandler` builder;
`plugin.go` the plugin that publishes the authority.

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
change which entities exist arrives only through its write-locked promotion
(`WriteableEntities`, in a later ticket) and is visible in a System's signature
and nowhere else. `Alive` is the only question a read may ask, and it is rarely
the one wanted: "is my target still alive" almost always means "does my target
still have `Health`", which a Store probe answers under a lock the System
already holds.

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

What a signature may contain today is a `*ecs.Query[Q]` and, at most once, the
event value itself. Naming the event is legal but is not the ordinary shape: a
System that names one can only ever be subscribed to that one, where the same
gameplay should be drivable by a fixed-step tick, a rollback re-simulation or a
test harness publishing its own frames. A parameter the builder does not
recognise is a registration-time panic, which the plugin boundary reports as
`ErrPluginPanic` naming the plugin — and so is a Query naming a Component no
plugin registered:

```
plugin "systems" panicked in Register: ecs: Query game.GuardedQ names
unregistered Component game.Guarded
```

That names the **Component** and the **Query**, rather than the store type the
user never wrote.

## What it costs

Measured on a real `kernel.Engine` driven by a real `app.UpdateEvent`, AMD Ryzen
9 7950X3D, go1.27.1 windows/amd64, medians of three:

| whole frame | ns/op | allocs/op | B/op |
| --- | --- | --- | --- |
| nothing subscribed | 82 | **2** | 160 |
| hand-written subscription, 1 000 | 5 250 | **6** | 322 |
| **a System with a two-Component Query, 1 000** | **8 080** | **6** | 322 |
| hand-written subscription, 10 000 | 19 760 | **6** | 321 |
| **a System with a two-Component Query, 10 000** | **36 610** | **6** | 322 |

**The engine charges 2 per publication plus 4 per subscriber, and the Query
lands exactly on that line** — identical at 1 000 and at 10 000 Entities, which
is the real test, because an allocation in the iteration would scale with the
Entity count. Over a 10 000-frame steady state, which an average over `b.N`
could hide amortised growth behind, it is **6.002 objects a frame at 1k and
6.003 at 10k**, against the hand-written **6.008**. `-gcflags=-m` reports no
`moved to heap` anywhere on the iteration path, so the zero is explained by the
compiler rather than merely observed.

From the 1k to 10k slope, so the per-frame floor drops out: **3.17 ns an Entity
against the hand-written loop's 1.61**.

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
