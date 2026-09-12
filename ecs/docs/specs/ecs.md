# cog ecs — specification

`github.com/dvoyni/cog/ecs` is a plugin that lets an app describe things in the
world as **Entities** carrying **Components**, and describe behaviour as
**Systems** — plain Go funcs whose parameter types say what they touch. This
document specifies the whole of it: what an Entity and a Component are, how a
System's signature becomes a cog lock set, how Components are stored and
iterated, what a structural change may do and what it excludes, and how a plugin
that is not the ECS attaches to the world.

The design is bound by four requirements, in this order, and every decision
below was taken against them:

1. **Zero heap allocation on the hot path**, measured rather than asserted.
2. **Concurrency comes from cog's existing scheduler.** The ECS contributes no
   scheduler, no ordering and no registration API of its own.
3. **Extensible** along named axes, and explicitly not along others.
4. **Reasonably simple to use.** A System is a plain Go func; reflection runs at
   registration only.

Two properties fall out of taking those in that order, and they are worth
stating before anything else because most of what follows is a consequence of
one or the other. **The lock unit is the Component type** — under sparse-set
storage a Component type is exactly one object, so a kernel resource cell maps
onto it one to one. And **under-declaration is unrepresentable**: the only route
to a Component's storage is a Query, and the Query's field types *are* the
declaration, so a System physically cannot touch a Component its signature does
not name. That is strictly stronger than what a hand-written kernel `Lock` gives
today.

This document is the specification the implementation is judged against. It is
assembled from the resolved tickets of
[Design an ecs plugin for cog](https://github.com/dvoyni/cog/issues/180); every
section cites the tickets it came from. Where a claim rests on something
unverified it is marked **Gap** and says what would settle it; where assembling
these decisions next to each other settled something no ticket did, it is marked
**Settled here**.

**Nothing in this specification is implemented.** The map that produced it is
plan-only apart from one deliberate exception — the zero-allocation prototype,
whose job was to produce a number. That prototype lives on the throwaway branch
`proto/ecs-zero-alloc` under `docs/research/ecs-zero-alloc-proto/`, is a nested
module so a root `go build ./...` skips it, and is deleted once this document
cites it. There is no `ecs` package in the tree.
[Required work](#required-work) is the checklist an implementation session works
from.

---

## Contents

- [Vocabulary](#vocabulary) · [What the numbers are, and what they are not](#what-the-numbers-are-and-what-they-are-not)
- [Entity](#entity) · [Component](#component) · [Naming an engine-side thing](#naming-an-engine-side-thing)
- [Registration and ownership](#registration-and-ownership)
- [The Store](#the-store) · [The Query](#the-query) · [The Driver](#the-driver)
- [The System](#the-system) · [The lock set](#the-lock-set)
- [Structural change](#structural-change) · [Reaching another Entity](#reaching-another-entity)
- [Binding: how another plugin attaches](#binding-how-another-plugin-attaches)
- [More than one world](#more-than-one-world)
- [The zero-allocation claim](#the-zero-allocation-claim)
- [What is not foreclosed](#what-is-not-foreclosed)
- [Shapes that were rejected](#shapes-that-were-rejected)
- [Required work](#required-work) · [Out of scope](#out-of-scope)

---

## Vocabulary

Every term here is in `CONTEXT.md` under **Entities and Components**, which is
the glossary of record; this list is a reading aid, not a second definition.
Four words are **deliberately unspent** and no implementation may claim them:
`Chunk`, `Relation`, `Prefab` and `Template`. `Archetype`, `Table` and `View`
are retired outright.

- **Entity** — an opaque handle to one thing. Comparable, copyable, map-keyable;
  the zero value means no Entity.
- **Component** — a plain value an Entity either has or has not, addressed by
  its Go type, containing no pointers transitively.
- **Tag** — a Component with no fields, whose presence is the whole of what it
  says.
- **Component set** — the exact set of Component types one Entity has. It
  describes an Entity; nothing groups Entities by it and no structure holds it.
- **Store** — the holding of every value of one Component type. One per
  registered type, and **the unit a lock is taken on**.
- **Entities** — the id authority, and the one thing that can reach every Store.
- **Query** — a struct type whose field types are the Components a System
  touches; a field's pointer-ness is its access mode.
- **Filter** — a Query field that narrows which Entities match and yields
  nothing, but still reads its Component's Store.
- **Driver** — the one Store a Query walks; every other Component it names is
  probed per candidate.
- **System** — a plain Go func, called once per tick, that iterates the Entities
  its Queries match itself.
- **Structural change** — a change to *which* Entities have *which* Components,
  as against a change to a Component's value.
- **Spawn** — creating an Entity with a complete Component set in one structural
  change, that set named as a **Bundle**. **Despawn** is its inverse and is
  total.
- **Reference** — an Entity kept inside a Component. It points one way.
- **Accessor** — a System's means of reaching one Component of an Entity it did
  not iterate to.
- **Hash** — the 64-bit hash of a name, and how a Component says which model,
  clip or node it means. **Name table** — where the plugin that resolves names
  keeps them.

---

## What the numbers are, and what they are not

Every measurement in this document was taken on **AMD Ryzen 9 7950X3D, Go
1.27.1, windows/amd64, `NumCPU=32`**, medians of five runs unless stated. There
are four benchmark suites and they are not equally strong evidence:

| suite | branch | what it is |
| --- | --- | --- |
| `docs/research/ecs-go-mechanics-bench/` | `research/ecs-go-mechanics` | Go language mechanics in isolation |
| `docs/research/ecs-query-shape-bench/` | `worktree-ecs-vocabulary` | query-fill shapes against a hand-written loop |
| `docs/research/ecs-sparse-probe-bench/` | `worktree-ecs-vocabulary` | a model of the Store: probe, driver, spawn, dispatch, scheduler |
| `docs/research/ecs-zero-alloc-proto/` | `proto/ecs-zero-alloc` | **the decided design on a real `kernel.Engine`** |

Only the last runs on a real engine, driven by a real `app.UpdateEvent`, and
that distinction matters more than once below: three allocation constraints
appeared **only** in situ and are invisible in every microbenchmark
([The zero-allocation proof](https://github.com/dvoyni/cog/issues/243)).

Four honest limits on all of it.

- **`allocs/op` alone is not the bar, and taking it as the bar would have passed
  the worst shape found.** A component iterator reached through a boxed handle
  scores 2 allocations and costs **2470 ns against 622 ns** inlined; only ~50 ns
  of that gap is the allocation, and the remaining 1800 ns is 1024 indirect
  yield calls **scaling with entity count**
  ([research: what Go permits](https://github.com/dvoyni/cog/issues/234)). Time,
  and how it scales, are part of every claim here.
- **nox is a prediction, not a workload.** It is the named reference consumer,
  it is still in design, and **its documents state no entity count anywhere**.
  Where a figure like "5000 entities" appears below it is an assumed order of
  magnitude, said so at the point of use.
- **The race detector cannot build in this environment.** Every suite was run
  stable at `-count=10` instead, and that is a weaker guarantee. An
  implementation must run the real package under `-race` in CI.
- **`testing.B.Loop` hides the finding.** It wraps loop-assigned variables in
  `runtime.KeepAlive`, which pinned an accumulator to memory and made two
  iteration shapes differing by 4× look identical at ~2450 ns. Timings use the
  classic `b.N` form; allocation counts are unaffected.

---

## Entity

An **Entity** is a defined type over `uint64`: **index in the low 32 bits,
generation in the high 32** — and the split is deliberately **not public
contract**. There is no exported `Index()` or `Generation()`, only unexported
accessors. That is the whole argument for `uint64` over
`struct{Index, Gen uint32}`: both are 8 bytes and both are comparable, copyable
and map-keyable, but the struct leaks its layout into every call site and can
never be re-cut ([What a Component is, what an Entity
is](https://github.com/dvoyni/cog/issues/237)).

Generations **start at 1**, so `Entity(0)` unambiguously means "no Entity" while
index 0 stays an ordinary usable slot. `NoEntity` is the named constant;
comparison is `==`. It implements `fmt.Stringer`. Thirty-two bits of generation
is ~4×10⁹ reuses of one slot before a stale handle could alias — at 30 Hz,
never.

A game holds Entities across frames constantly: a creature's target, a spell's
owner, a projectile's caster. **That is safe**, and it is safe by construction
rather than by discipline — see [Reaching another
Entity](#reaching-another-entity).

Indices are **recycled** through a free list. That is what bounds the flat
sparse index by *peak concurrent* Entities rather than by Entities ever created,
and it is what makes [the Store's flat index](#the-store) affordable.

---

## Component

**A Component type contains no pointers, transitively.** That one sentence
replaces "a plain copyable struct", and it is better because it is
**mechanically checkable**: a `reflect.Type` walk at registration, where cost is
irrelevant. It forbids pointers, slices, maps, channels, funcs, interfaces,
`sync` types — **and strings**. It permits numerics, bools, fixed-size arrays,
`Entity`, and structs of those.

The check reports the offending field **by path**, because the field that fails
is usually several structs down and naming only the Component is useless:

```
plugin "stringly" panicked in Register: ecs: proto.PathedDrawable.Path is a
string, which is not pointer-free
```

Three reasons for the rule, in the order they carry weight, and the third is
weaker than it sounds — which is stated because it cuts both ways.

**Copying.** A Component is copied into and out of a Store by value and must
stay meaningful after the thing it was copied from is gone. Every forbidden kind
names memory the Store does not own and cannot keep alive.

**Serialisation.** A pointer-free type is trivially serialisable, which is the
cheap constraint that keeps replication from being foreclosed. This is the
expensive half of that future already paid for, and it is why [data-driven
spawning](https://github.com/dvoyni/cog/issues/266) will be additive when it
arrives.

**The collector, and honestly it is the thin one.** Measured while resolving
[The binding shape](https://github.com/dvoyni/cog/issues/246): forced full
collections, one Store live, timed in batches because the Windows wall clock
quantises a single collection.

| Store, 100 000 entities | per collection | over an empty heap |
| --- | --- | --- |
| no store | 0.1243 ms | — |
| **pointer-free Component (48 B)** | **0.1237 ms** | **−0.001 ms** |
| one `string` field (64 B), interned | 0.3345 ms | +0.210 ms |
| one `string` field (64 B), all distinct | 0.3341 ms | +0.210 ms |

At 1 000 000 entities the pointer-free Store is still **−0.009 ms** against an
empty heap — 48 MB of dense rows the mark phase never reaches, because Go puts a
pointer-free allocation in a noscan span — while one `string` field costs
**+0.798 ms** interned and **+1.435 ms** distinct. A pointer-free Store placed
beside a scanned one costs **0.003 ms**.

Two findings inside that table are worth carrying, because both contradict the
obvious repair. **Interning does not help the mark phase** (0.3345 against
0.3341 ms at 100k): scanning is proportional to pointer *slots*, not to distinct
objects. And **a pointer is not only a GC cost** — it widens the Component from
48 B to 64 B, so fewer rows fit a cache line and iteration costs **12% more**
(55.3 µs against 49.4 µs over 100k rows).

But 0.21 ms per *collection* at 100 000 Entities, when nox is two orders of
magnitude below that, is a thin reason to forbid strings on its own. **The
lookup is what actually forces the handle**, not the collector: naming a model
by path costs **46.9 ns** against **0.54 ns** by dense index, and that is
per-draw, per-frame. See [Naming an engine-side
thing](#naming-an-engine-side-thing).

### An Entity holds at most one Component of a given type

Not a policy but arithmetic: a sparse set has exactly one slot per Entity. The
ECS answer to "two weapons" is two Entities. This is a **core invariant**, and
[Shapes that were rejected](#shapes-that-were-rejected) records what was refused
rather than break it.

### Tags, and the one rule about encoding

A **Tag** is a Component with no fields. Its purpose is **narrowing a Query** —
`Disabled`, `Hidden`, `Dead`. It is **not** a second way to store a boolean.

The unifying rule is **no redundant encoding**: say each fact exactly once.

| the fact carries… | encoding | cost |
| --- | --- | --- |
| data that only exists while it holds | the data Component, added and removed; **presence is the flag** | a structural change per transition, small iteration set |
| no data at all | a **Tag**, added and removed; the Tag *is* the state | a structural change per transition, small iteration set |
| a stable capability plus churning state | presence = capability, **field** = state (`Fireable{OnFire bool}`) | no structural change, iterates every capable Entity |

So `Burning{}` as a Tag is legitimate when it has no parameters. What is
forbidden is stating a fact twice: a bare `OnFire` Tag *beside* a `Burning{…}`
Component, or a boolean field duplicating a Tag that already exists.

**This rule is documentation, not enforcement.** The no-pointers rule is a type
property and is checked; "do not use a Tag as a boolean" is not, and the spec
says so rather than implying a guard.

### There is no cap on Component types, and three real limits

There is no cap, and its absence is a consequence of the storage decision rather
than a generosity. Archetype ECSes generally cap it because an archetype's
signature is a bitmask over component ids and that bitmask's width *is* the cap.
Sparse sets have no archetype signature at all: a Component type is one Store,
one kernel resource keyed by `reflect.Type`, and neither the registry nor the
scheduler's reader-count/writer-set table is fixed-width.

Three limits are real and are named rather than claiming "unlimited":

1. **How many Components one System names.** Arity inference scales to 8 with no
   explicit type arguments, and cost is not linear in width — see
   [The Query](#the-query).
2. **Sparse-index memory per Component type.** 8 bytes × peak concurrent index
   space, per type — 32 KB per type at nox's low thousands, **2.8 MB across 85
   types**.
3. **Flip cost.** Adding or removing a Tag is a structural change, not a field
   write.

### Variable-length data has three answers

In preference order, unchanged since
[#237](https://github.com/dvoyni/cog/issues/237) and sharpened by
[#246](https://github.com/dvoyni/cog/issues/246):

1. **A child Entity** with an owning Reference, for structured data (an
   inventory, a spell list). This is how nox already works: a carried sword and
   a dropped sword are the same object.
2. **A fixed-capacity array in the Component**, where the bound is small and
   real. Now measured rather than assumed: a `[32]byte` inline name costs the
   collector **−0.015 ms** against an empty heap at 100k and **+0.051 ms** at
   1M, against **+0.805 ms** for the same field as a `string`. The costs are
   truncation and width, not the collector.
3. **A side resource keyed by Entity** — and this is **deferred out of v1**, to
   [per-entity data a Component cannot
   hold](https://github.com/dvoyni/cog/issues/264). What v1 ships is
   [hashing](#naming-an-engine-side-thing), which covers every case v1 has.

What is **not** on the list is a content-addressed table, and the reason is the
cleanup question: such a table must hold the buffer itself, so it grows with
every distinct value the game ever produces, and the only way to empty it is to
count how many Entities still refer to each entry — refcounting on a Component
write, which stops the Store being a `memcpy` and stops Despawn being a
type-erased removal. Keyed by Entity there is exactly one owner and nothing to
count. **Sharing is what costs; ownership is free.**

---

## Naming an engine-side thing

**A Component names an engine-side thing by a hash of its name, never by the
name itself and never by an assigned index.** The name is a string and a
Component holds no pointers; a hash is a plain number.

```go
type ClipHash uint64                              // the caller's own named type
var walkClip = ecs.HashOf[ClipHash]("Walk")       // package level

func animate(q *ecs.Query[AnimQ]) {               // two Components, nothing else
    for _, it := range q.All() {
        if it.M.Speed > 0.1 {
            it.A.Clip = walkClip
        } else {
            it.A.Clip = idleClip
        }
    }
}
```

`ecs.HashOf[K HashKey](string) K` constrains `K` to `~uint64`, so the hash **is**
the caller's type — nothing to unwrap, no accessor, directly comparable,
printable, usable as a map key, and pointer-free because the constraint admits
nothing else. `ClipHash` and `ModelHash` stay distinct without a second type
parameter, so a clip name cannot be assigned where a model name belongs; a
codebase that does not want the distinction declares one such type and is done.
`~uint64` rather than `~int` because `int` is 32 bits on some platforms, and a
hash that depends on where the game was built has given up the one advantage it
had.

**The property that decides this is that producing a hash needs nothing.**
Hashing is pure, so a System changes what an Entity names holding only the lock
it already has on the Component. An assigned index cannot: interning is what
*assigns* the index, so the interning table would join the lock set of every
System that ever changed an animation clip. That is the case a registration-time
conversion cannot reach, because the common case is *mutation* — a clip changing
from `"Idle"` to `"Walk"` while the game runs — not spawning.

Measured, whole-frame, assigning a clip to every Entity:

| | ns/op | allocs/op | per entity |
| --- | --- | --- | --- |
| hashed name, 1 000 | 7 624 | **6** | — |
| synchronised interner, 1 000 | 18 827 | **9** | — |
| hashed name, 10 000 | 31 578 | **6** | **2.66 ns** |
| synchronised interner, 10 000 | 143 985 | **9** | **13.9 ns** |

The interner is 5× the per-entity cost **and it allocates**, failing requirement
1 outright — for a naming scheme rather than for anything the game asked for.

Per lookup, over 1 000 lookups:

| | ns |
| --- | --- |
| dense index | **0.55** |
| stored 64-bit hash → map | **4.6** |
| hashing the string every lookup | 19.0 |
| a path, as `scene` resolves one | 44.3–46.9 |

**And an assigned index can never be a package-level `var`.** There is no table
at package initialisation, and two Engines mean two tables:
`TestANameIsTheSameEverywhereAndAnIndexIsNot` interns the same two paths in
opposite orders into two tables and gets ids `0` and `1` for one string, against
one hash.

### The name table belongs to the consumer

`ecs.Names[K, V]` is the reverse half, and **where it lives is the whole point**:
it belongs to the plugin that resolves names into things — a model table, a clip
table — under that plugin's own lock, read once per draw where a lock is held
anyway. **One** System declares it, rather than every System that ever assigns a
name. There is deliberately no process-wide interner.

`Names.Register(text, value)` is also where a **collision** is caught.
Sixty-four bits makes one vanishingly unlikely — about 3×10⁻¹² at ten thousand
distinct names — but vanishingly unlikely is not impossible, and an undetected
one would silently draw the wrong model. Every resolvable name passes through
`Register`, so one compare turns the class into a startup error. `TextOf`
recovers the original string, so a hash stays legible in tooling.

**Nothing cleans the table because nothing accumulates in it.** It holds what
the consumer registered — its asset manifest, fixed at startup. Asking it about
a name nobody declared answers that there is no such thing and leaves it the
size it was: `TestHashingAccumulatesNothing` hashes **1 000 000** procedural
names against eight declared models and the table still holds eight entries.
That is a property of a manifest rather than of hashing in general, which is why
this is the **only** thing v1 hashes: v1 hashes a model, a clip, a node, a pass
tag — **strings, and nothing else**.

### The obligation this puts on a bound plugin

A plugin a System will record into must offer a **pointer-free handle** for
everything a Component needs to name. Where it does not, the app makes its own
table. Machine-checked against the real `scene` package by running the
pointer-free walk over its recording vocabulary:

| type | verdict |
| --- | --- |
| `scene.Transform` | rejected — `.Matrix` is a pointer |
| `scene.ModelDraw`, `scene.MeshDraw` | rejected — `.Transform.Matrix` is a pointer |
| `scene.ClipPlay` | rejected — `.Clip` is a `string` |
| `scene.Material` | rejected — is a slice |
| `scene.ModelRef` | rejected — `.Path` is a `string` |
| **`scene.MeshRef`** | **legal Component** — already a dense id and a generation |
| **`scene.LayerMask`, `scene.CameraID`** | **legal Component** |

Note the exception. The gap is not a principle; it is one type that has not been
given the treatment another one already has.

**One caveat the spec must carry:** a hash is stable across processes and runs,
which an interned ordering is not — so it is the form that survives being
written to a save file or sent over a wire. A dense index is not wrong, it is
simply not the *Component's* business; a consumer may index internally all it
likes.

---

## Registration and ownership

A Component type is registered **explicitly, once, by exactly one plugin**:

```go
func RegisterComponent[C any](r *kernel.Registrar, en *Entities, ids uint32) *Store[C]
```

It creates `C`'s Store, hands it to the kernel as an ordinary resource of type
`*ecs.Store[C]` via `Registrar.InitResource`, enrols it with `Entities`, and
bakes the per-type closures that registration-time reflection will later look up
by `reflect.Type`.

Half of "explicit" is forced rather than chosen: a `Lock` must bind every handle
it will use, and a declared resource with no initial value fails finalisation
with `ErrMissingResource` — so **you cannot lock a Store that does not exist at
registration time**, and discovery-on-first-use is off the table. The other half
was genuinely open, and derivation *from the registered Systems' signatures* was
rejected: it trades a composition-time error for a runtime one (a Component only
a spawner adds and no System reads would have no Store, discovered when the
spawn panics) and it makes ownership unattributable.

**No resource factory is needed and none should be added.** The map opened
expecting kernel to grow `ResourceFactory func(reflect.Type) any`; explicit
registration was not merely sufficient but is what actually ran, on an
**unmodified kernel** ([What kernel
gains](https://github.com/dvoyni/cog/issues/244)). Two facts make it safe rather
than merely workable. Registration order already guarantees it: `orderPlugins`
topologically sorts by declared dependency, and a System over `game.Health` is
in a plugin that imports `game.Health`, therefore depends on that plugin,
therefore registers after it — **the Store always exists before any Query naming
it**. And the failure mode is a composition failure the ECS catches before
kernel can, naming the Component and the Query rather than a store type the user
never wrote:

```
plugin "systems" panicked in Register: ecs: Query proto.GuardedQ names
unregistered Component proto.Guarded
```

### The plugin that registers a Component owns its Store

This is the one judgement here rather than a measurement, so both branches were
built and composed. Under caller-owns, `Describe` reports
`*ecs.Store[protoecs.Guarded] owned by "components"`, and a System in another
plugin that does not declare that dependency fails composition by name. Under
ownership by `ecs`, **the same System composes with no dependency at all.**

That is the whole trade. Ownership by `ecs` does not make the coupling check
lenient, it makes it **vacuous**: once every Store has the same owner and every
plugin with a System already depends on `ecs`, `ErrUndeclaredDependency` can
never fire on a Component again, and the check that exists to catch "plugin A
quietly reads plugin B's data" stops seeing the majority of an ECS app's data.
It would also collapse `ErrDuplicateRegistration`'s diagnostic, which could then
only ever say `ecs` twice.

The objection — a System in plugin A touching a Component declared by plugin B
must make A depend on B — is real, is the intended price, and is smaller than it
reads: **the Go import graph already forces the same edge**, since a System
cannot name `B.Health` in a Query struct without importing `B`. So the rule is:

> **Register a Component in the plugin that defines its Go type.** Shared
> vocabulary belongs to the lowest plugin that owns it — `physics` declares
> `Body`, not the game — which is the direction imports already run.

Registering someone else's type stays legal as the escape hatch for the inverted
case, and `ErrDuplicateRegistration` catches the collision naming both plugins.

One wart, recorded before it is discovered in a log: an instantiated generic
renders its type argument with the full import path, so the error a developer
reads is `*ecs.Store[github.com/dvoyni/nox/game.Health]`. Verbose, unambiguous,
and not worth a kernel change to prettify.

### The world handle is a plain Go value

`*Entities` is a kernel resource — it has to be, since it is what the locks are
taken on — but that is only the run-time half. Both `RegisterComponent[C]` and
`ToHandler` need it at **registration**, where no handler is running and no
resource value may be read. So composition necessarily looks like this, and the
binding shape is fixed before `WithPlugins` is called and visible in the app's
composition root:

```go
world := ecs.NewEntities(maxIDs)
kernel.New(cfg).WithPlugins(ecs.Plugin(world), physics.Plugin(world), game.Plugin(world))
```

**Settled here:** the prototype's constructor is called `NewWorld` in two
tickets' prose and `NewEntities` in the code that actually ran.
[#245](https://github.com/dvoyni/cog/issues/245) retired `World` from the API
and `CONTEXT.md` lists it under `_Avoid_`, so **`ecs.NewEntities` is the name**,
and no exported identifier in `ecs` contains the word `World`.

---

## The Store

A **Store** is the holding of every value of one Component type, one per
registered type, and **the unit a lock is taken on**. It is three arrays
([Sparse sets: paging, tags, driver selection and the iteration
contract](https://github.com/dvoyni/cog/issues/239)):

```
Store[T]:
    sparse []uint64   // entity index -> generation<<32 | dense index
    owners []Entity   // dense index  -> full entity id
    dense  []T        // packed component data
```

`len(owners)` **is** the population. There is no separate count, and swap-remove
is what keeps that exact, because the packed arrays never contain holes.

**A Store must be reached as `*Store[T]`, and that is a correctness rule before
it is a cost one.** A value store makes `kernel.Write[T].Get()` return a copy, so
mutations through it are **silently discarded** — demonstrated by
`TestNonPointerStoreLosesMutation`, not inferred. The cost argument is secondary
and also decisive: a value store copies the whole struct per access (126 ns for
4 KiB, and **62.9 ns even to read a single field**, 105× worse than through a
pointer, which is 0.60 ns).

### The probe is one load, and liveness is free

The probe is `sparse[e.idx()]`, then compare the generation half against
`e.gen()`. No trip through `owners`, no tombstone branch — the absent slot's
generation half is all-ones, which no live generation reaches. Four shapes, ns
**per probe**:

| | slot | arity 2 | arity 4 | bytes/slot |
| --- | --- | --- | --- | --- |
| two loads: `sparse[e]` then `owners[d] == e` | `int32` | 0.551 | 0.727 | 4 |
| **one load, generation in the slot** | **`uint64`** | **0.384** | — | **8** |
| one load, 8-bit generation packed | `uint32` | 0.422 | 0.540 | 4 |
| paged | `uint32` in 4096-entry pages | 0.813 | 0.902 | 4 |

**The one-load probe is 24–26% cheaper, and the compare that buys it is the
compare that rejects a stale handle** — liveness is not an extra cost, it *is*
the probe.

The 8-byte slot is chosen over the 4-byte one **not for the 9%**. A packed 8-bit
generation aliases after 255 recycles of an index, which would be sound only if
a slot were always cleared when its Component left the Entity — and that would
have forbidden [structural change](#structural-change) from choosing lazy
reclamation while that was still open. A 32-bit generation matches `Entity`'s
own field exactly, so **staleness is decided, never estimated**. The 4-byte slot
is the lever to pull if index memory ever bites.

Consequence: **`owners` is not touched by a probe at all**, only by the Driver. A
probed Store touches two arrays, not three. And `owners` stays separate from
`dense` rather than interleaved, because `dense` must remain a contiguous `[]T`.

### The sparse index is flat, not paged

Paging's claim is that a Store with few owners should not pay for the whole
index. That holds only if owners are **clustered** in index space, and
generation recycling scatters them. Index space of 2²⁰ slots, 4096-entry pages,
flat cost 4096 KB:

| owners | clustered | scattered |
| --- | --- | --- |
| 100 | 1 page, 16 KB | 88 pages, **1408 KB** |
| 10 000 | 3 pages, 48 KB | 256 pages, **4096 KB — the entire flat index** |

So in the general case paging costs what flat costs **plus 93% per probe**. And
the arithmetic that settles it: because indices are recycled, index space is
bounded by **peak concurrent Entities**, not by Entities ever created. At nox's
low thousands a flat index is 32 KB per Component type, **2.8 MB across 85
types**. Paging solves a problem recycling already solved, and `Page` stays an
unspent word.

### Tags are Stores, and 85 Tags beat one bitfield

nox's 32 class + 32 flag + 21 status bits, narrowing a query by three of them
over 1024 all-matching Entities: three Tag Stores **1.650 ns/entity**, one
`Flags{Class, Flag, Status uint32}` Component **1.415 ns/entity**.

**The bitfield is 14% faster, not 3×** — the probes hit and the branches predict
— and it is bought at a price no benchmark shows. The lock unit is the Component
type, so **one bitfield collapses 85 independent lock units into one**, and
every System touching any flag conflicts with every System touching any other. A
14% iteration win does not buy the destruction of the lock granularity this
design exists for.

**A Tag's Store has `sparse` and `owners` and no `dense` array.** There is
nothing to store and nothing lands in the Query struct.

Applying the no-redundant-encoding rule hard, most of nox's `class` bits
**vanish** into Component composition anyway: you never want "all `MONSTER`s",
you want everything with `Health` and `Brain` and `Faction`, and having those
Components *is* being a monster. Which of a game's bits land where is the game's
decision, not the engine's.

### Removal is swap-remove, so iteration order is unspecified

Removal moves the last row into the hole, which relocates a **different**
Entity's dense index. Two consequences are written into the contract: **no dense
index may be held across a mutation**, and if determinism is wanted later it
comes from sorting at a defined point, **never** from making the Store stable —
stable storage is what makes EnTT groups refuse to compile, and reordering
shared storage to suit one query is the road nobody has made work.

### Growth and sizing

Filling a Store with 10 000 Components:

| | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| from empty, `append` doubling | 230 000 | 1 711 980 | **39** |
| preallocated | 81 000 | 450 561 | **3** |

Doubling churns 3.8× the bytes and 2.8× the time. This is **not** the hot path —
iteration allocates zero and growth happens only under a structural write lock —
but at 30 Hz the spike is what is felt. So: `append` doubling for `dense` and for
the sparse index, **nothing shrinks automatically**, and a
reserve-at-registration hint lets an app that knows its peak buy the
three-allocation column outright. Compaction stays out.

---

## The Query

A **Query** is a struct type whose field types are the Components one System
touches, and **a field's pointer-ness is its access mode** ([How a System
declares what it touches](https://github.com/dvoyni/cog/issues/238)):

```go
type MoveQuery struct {
    Body     *Body     // write — yields the stored value itself
    Velocity Velocity  // read  — yields a copy
    _        ecs.Without[Disabled]
}
```

Lock set: `write{Body}`, `read{Velocity}`, `read{Disabled}`, plus
`read{*Entities}`. Nothing in the file declares it.

**A read must yield a copy.** A read yielding a pointer is a data race against
concurrent readers, and Go has no pointer-to-const. A fat read Component
therefore costs a memcpy per Entity — which is a feature, since it is evidence
the Component is too fat, and the three answers for variable-length data are
already named. There is no `Ref[T]` "read me a pointer, trust me" hatch.

Two spellings were rejected. **Mode on the whole Query**
(`kernel.Write[Query[T]]`) write-locks every Component in it, which is half the
false serialisation self-inflicted. **`kernel.Read`/`kernel.Write` inside the
Query** would make one name mean two things — those are handles with a `Get()`,
bound by the kernel; inside a Query they would be pure type-level markers the
kernel never sees. Marker types (`ecs.R[T]`/`ecs.W[T]`) lose more weakly: with
`ecs.R[Velocity]` you receive a `Velocity`, so the type lies about what it
yields, where `*Body`/`Velocity` are literally the types you get.

**Fields are named, not embedded.** Embedding would give promoted access, but
two Components with the same base name from different packages cannot both be
embedded, and two Components each having a `Kind` field make `it.Kind` an
ambiguous selector — a compile error at the *use* site, far from the cause. The
ECS reads field *types* and never names, so names are free to read well.
**Embedding is reserved for composing Queries**: one Query struct embedded in
another flattens its fields in.

### Why a struct, and not a numbered family

**Go caps range-over-func at two values.** A three-value yield does not compile —
`expected at most 2 expressions`, verified against the compiler. So a positional
`QueryN` can never use the idiomatic loop beyond one Component; it needs
`q.Each(func(e, b, c){…})` or an index loop with an `ok`. **The language decided
this before performance was consulted.**

Then measured anyway, ns per Entity over 1024 Entities:

| shape | 2 comp | ×hand | 4 comp | ×hand | allocs |
| --- | --- | --- | --- | --- | --- |
| hand-written slice loop | 1.45 | 1.00 | 2.56 | 1.00 | 0 |
| positional `At(i)` index loop | 1.46 | 1.01 | 3.87 | **1.51** | 0 |
| **`Query[T]`, unrolled reflect fill** | **1.98** | **1.37** | **3.83** | **1.50** | **0** |
| `Query[T]`, monomorphised typed fill | 2.00 | 1.38 | 3.50 | 1.37 | 0 |
| `Query[T]`, generic per-field loop | 3.90 | 2.69 | 6.00 | 2.34 | 0 |
| `Query[T]`, per-field binder closures | 7.14 | 4.92 | 12.78 | 4.99 | **1** |

Four things the implementation must take from that table.

1. **The reflective fill costs what a hand-monomorphised fill costs**, given
   precomputed offsets and a handful of fillers unrolled by field count, chosen
   once at registration: 2031 ns against 2047. **No codegen is needed.** The
   naive shapes are the traps — a per-field loop is 2.4×, per-field binder
   closures ~5× *and they allocate*. Those are implementation mistakes, named
   here as such.
2. **Positional degrades faster with arity and they cross at four Components**,
   because six return values exceed the register ABI and spill. Positional's
   advantage exists only in the 1–3 band and is gone where Queries get
   interesting.
3. **The overhead is neither the copy nor `unsafe`.** An all-pointer struct
   copying no Component bytes matches one copying 16 per Entity, and the
   monomorphised fill — typed writes, no `unsafe` anywhere — costs the same
   1.38×.
4. **The overhead is the memory round-trip forced by addressing the fill
   buffer.** The same struct, constructed with typed writes and yielded *by
   value* so its address is never taken, is **1490 ns against the baseline's
   1485 — parity to 0.3%**.

That last one generalises into a statement the implementation may rely on: **a
generic fill can never reach parity.** Writing fields generically requires an
address, an address requires memory, memory defeats register promotion. The
1.37× is the price of not knowing the struct's type at compile time, and only
generated code can pay it off — filed as [generate monomorphised query
fillers](https://github.com/dvoyni/cog/issues/257), a pure optimisation changing
no user code.

**`All()` yields `(Entity, *Q)`** — Entity first and always, because `owners` is
loaded anyway and the alternative is a second iteration shape. It yields `*Q`
rather than `Q`: by-value yielding is free when monomorphised, but with the
reflective fill it is no better at two Components and **2.5× worse at four**
(9.65 ns/entity against 3.83), because a 40-byte struct sourced from memory is
copied wholesale per Entity. **The pointer is valid only for the current
iteration step** — the same lifetime rule kernel's handles already carry.

Two attempts to speed the fill up both made it slower and are recorded so the
next person does not repeat them: spreading a field into scalar arguments to
keep it in registers **stopped the fill inlining and doubled the two-Component
frame** (37 µs → 73 µs at 10k), and unrolling a three-field path beside the
two-field one cost about 6% rather than saving any.

### Filters

**A filter is a blank field**: `_ ecs.Without[Disabled]`. It yields nothing into
the Query.

**But it contributes a read.** Evaluating `Without[T]` loads `Store[T]`'s sparse
slot for the Entity, and a concurrent System adding `Disabled` holds
`write{Disabled}` and is mutating that exact array. So **`Without[T]` contributes
`read{T}`, and so does any `With[T]`**. A filter reads less *data* than a
Component field — nothing lands in the struct — but it reads the same *Store*,
and the lock set is about Stores. This corrects
[#238](https://github.com/dvoyni/cog/issues/238), which had filters contributing
no access; it is a data race, not a refinement.

`ecs.With[T]` and `ecs.Or[…]` can arrive later as further field types without
the derivation changing shape — the query-vocabulary growth axis requirement 3
named. The rejected alternative was a second type parameter
(`ecs.Query[MoveQuery, ecs.Without[Disabled]]`), which separates access from
matching more visibly but reintroduces a numbered family the moment you want two
filters.

**Implementation note the spec must carry:** Go still assigns a blank field an
offset, and a trailing zero-size field forces a byte of padding, so the fill
must recognise and skip filter fields rather than try to populate them.

### The `unsafe` fill is sanctioned, with a named invariant

Filling the Query struct writes a `*Body` into it through `unsafe.Pointer`,
which emits **no GC write barrier**. A barrier-free pointer write is unsound in
general. It is sound here for a reason that must be stated rather than left
implicit: **a Query's fill buffer may only ever hold pointers into a live
Store**, and a Store is a kernel resource cell held for the engine lifetime, so
the pointee is independently reachable whether the barrier fires or not. The
pointer-free rule is what guarantees the value fields need no barrier at all.
The safe alternative is a typed setter closure per pointer field — the ~5× shape
that allocates.

### Nested iteration allocates nothing, and costs ~3× anyway

This was expected to be the sharp edge, because nested *boxed* iteration costs
2050 allocs / 57 KB per frame. Outer Query 1024 Entities, inner 64, so 65 536
inner steps:

| shape | ns/inner | allocs |
| --- | --- | --- |
| hand-written slice loops, both levels | **0.861** | 0 |
| outer `All()`, inner slice loop | **0.864** | 0 |
| control — `All()` in an ordinary loop, no outer query | 1.371 | 0 |
| outer slice loop, inner `All()` | 1.892 | 0 |
| outer `All()`, inner `All()` | **4.013** | 0 |
| …inner iterator hoisted out of the outer loop | 3.925 | 0 |
| …inner iterator reached through an interface | 3.863 | 0 |

**Nothing allocates**, the boxed failure mode does not reproduce, and hoisting
or boxing the inner iterator changes nothing — so rebuilding it per outer Entity
is free. The cost is **position**: an `All()` in the *outer* slot is at the
hand-written floor, while an `All()` in the *inner* slot loses inlining.

It is documented rather than forbidden, and the real hazard is stated with it:
**nesting is O(n × m) regardless**. A per-Entity neighbour scan belongs behind a
broadphase from the physics plugin, not a second full Query.

---

## The Driver

Nothing stores "the Entities that have both A and B" — refusing to materialise
that set is what buys the absence of a global index. So a Query picks one Store
as its **Driver**, walks its `owners`, and probes the rest:
`cost ≈ len(driver) × (1 + probes)`. `len(driver)` is the only term the ECS
controls, and it is not a tiebreak but the dominant factor.

**The Driver is chosen per run, by scanning Store lengths — never cached.** Store
lengths change on every structural change, so the choice cannot be made at
registration: a Query that picked `Position` when `Burning` was rare would walk
5000 Entities to find three, silently. Comparing 8 Store lengths costs **3.7 ns**
once per Query run — 2.2 µs *per second* at 20 Systems and 30 Hz. Caching it
would be a table every structural change must write, so every structural change
would conflict with every Query: **the exclusion this storage model exists to
avoid, bought for 3.7 ns.**

Smallest-Store is a **heuristic, not an optimum**. The optimum Driver is the
smallest *intersection*, unknowable without computing it; smallest-Store bounds
candidates by the rarest Component's population, which flecs, bevy and Ark each
arrived at independently.

**A Query must name at least one present-typed Component or Tag**, checked at
registration with a named error. A filter can never drive: `Without[T]`'s
`owners` lists exactly the Entities to *exclude*, and nothing enumerates the
complement. A `Without`-only Query would have to drive off `Entities`, which
would put `read{*Entities}` into a Query's lock set for that reason rather than
by design.

### The known bad case, stated with its remedy

Two large, mostly disjoint Stores — 5000 with `Body`, 5000 with `Collider`, 100
with both:

| | ns/op |
| --- | --- |
| drive off a 5000-entity Store | 2596 — yields 5000 candidates, 100 survive |
| drive off a 100-entity Tag on the intersection | **137 — 19× faster** |

0.52 ns per discarded candidate: real, linear, cheap. **The remedy is an
app-maintained Tag**, which is just another Store and a very good Driver. There
is deliberately **no ECS mechanism** for it: that road is EnTT groups, shipyard
packs and bevy's sparse-set markers, and exclusivity is what kills all three.

### The scale sweep nobody publishes

No primary source publishes a 1k/10k/100k iteration sweep, so cog measured one.
One probe, ns/entity, `shuffled` filling the probed Store in a different order
so `dense[]` is hit at random — the realistic case:

| entities | driver only | probe, ordered | probe, shuffled |
| --- | --- | --- | --- |
| 1 024 | 0.612 | 1.021 | 1.042 |
| 16 384 | 0.605 | 1.056 | 1.054 |
| 131 072 | 0.620 | 1.060 | **1.597** |

**The cache cliff is between 16K and 131K Entities**, where the two dense arrays
stop fitting in L2. Below it, dense order does not matter at all. nox lives two
orders of magnitude inside the flat region — the concrete form of the finding
that iteration speed is not what decides this design.

---

## The System

A **System** is a plain Go func, called **once per tick**, which iterates the
Entities its Queries match itself. There is no ECS registration API: a System
becomes an ordinary cog subscription.

```go
func move(q *ecs.Query[MoveQuery], dt *ecs.In[float64]) {
    d := dt.Get()                                  // once, outside the loop
    for e, it := range q.All() {
        it.Body.Pos = it.Body.Pos.Add(it.Velocity.V.Scale(float32(d)))
        _ = e
    }
}

type MoveSystem kernel.Subscription[app.UpdateEvent]

registrar.Subscribe[MoveSystem](
    ecs.ToHandler[app.UpdateEvent](world, move,
        ecs.Feed(func(e app.UpdateEvent) float64 { return e.Dt })),
).After[GravitySystem]()
```

`ecs.ToHandler[E](world, system, feeds...)` returns a
`func() (kernel.Lock, kernel.Observe[E])` — exactly `kernel.Subscription[E]`, the
factory shape `Subscribe` already takes. `ecs.ToExecute[Req, Res]` is its
command twin, returning `kernel.Execute[Req, Res]`, which is what makes a System
invocable as a command.

**A System still returns nothing, so a command answers through `*ecs.Resp[Res]`
in the signature** — recognised by its type and injected by the builder, so the
classification stays a contract rather than a positional convention. Naming it
is optional: a command that is an order rather than a question takes no `Resp`
and answers the zero value. The cell is allocated once at registration and
cleared on the way out, so an invocation that writes nothing cannot inherit the
one before it, and answering costs **0 allocations and 52 ns on a 6.5 µs
invocation**, which is inside the noise. It holds a `Res` rather than a `*Res`:
every wrapper here is `X[T]` over the domain type, and making the System supply
the storage would be either an allocation an invocation or a pointer that need
not outlive the lock.

**That is requirement 2 satisfied completely.** `Before`/`After`/`First`/`Last`,
ownership, `Describe` and every `Err*` kind work unchanged; the identity type is
the ordinary `kernel.Subscription[E]` defined type the author writes for any
subscription; and a System may subscribe to **any** event type, so render-time
and input-time Systems work identically. One subscription per System is also
what gives requirement 2 its payoff: each System's lock set is its own, so the
existing scheduler parallelises Systems with no new machinery.

Rejected: one subscription per plugin or per named stage, which collapses every
System in a stage into a single lock set and destroys exactly the parallelism
this design exists to get.

### Where the loop lives

The System is called once per tick and iterates itself. The alternative — the
ECS calling the func once per matching Entity — was rejected **on structure
before cost**: a per-Entity func cannot break early, cannot do once-per-frame
work, cannot look at two Entities at once, and cannot consult a second Query,
and nox needs all four (collision is pairwise, and `app.UpdateEvent.Last` exists
precisely so a subscriber can do once-per-frame work).

Cost agrees. Per-Entity dispatch is 2.40 ns per entity per system against a
0.59 ns loop step, where per-Query dispatch is 87–142 ns **once per System per
tick** — 0.09% of a 30 Hz frame at 200 Systems.

### What a signature may contain

This is contract, not convention. A System takes any number of:

| parameter | declares | what it is |
| --- | --- | --- |
| `*ecs.Query[Q]` | `read{*Entities}` + per-field access | the Components it iterates |
| `*ecs.Spawn[B]` | `write{*Entities}` + `write{*Store[F]}` per Bundle field | creating Entities |
| `*ecs.WriteableEntities` | `write{*Entities}` | despawning |
| `*ecs.Get[T]` | `read{*Store[T]}` | reading one Component of an Entity it did not iterate to |
| `*ecs.Set[T]` | `write{*Store[T]}` | writing, or inserting, the same |
| `*ecs.Read[T]`, `*ecs.Write[T]` | the kernel's own read/write on `T` | any other plugin's resource |
| `*ecs.In[T]` | nothing | a value projected out of the event |
| `*ecs.Resp[Res]` | nothing | a command only: the answer it writes |
| `kernel.Kernel` | nothing | the kernel value |
| the event or request value | nothing | legal, and not the default — see below |

**Anything else is a composition-time failure naming the System's type.** This
is a mistake every new user makes once, so the diagnostic matters more than the
mechanism.

**Arity is arbitrary**, via `reflect.Value.Call`, measured at **0 allocs/op from
arity 0 to 12** (58.5 → 197 ns). Since the call happens once per tick that time
is noise. Combined with the struct Query, **there are no numbered type families
anywhere in the design** — no `QueryN`, no `SystemN`.

**A System returns nothing.** `reflect.Value.Call` allocates for a callee that
returns a value (2 allocs / 28 B — the frame pool is unusable, plus
`make([]Value, nout)`), so the builder rejects one at registration. This is a
hard rule, not a style preference.

### The event does not belong in the signature

A System that names the event **can only ever be subscribed to that event**. The
same gameplay cannot then be driven by a fixed-step tick, by a rollback
re-simulation, or by a test harness publishing its own frames, without writing
the System twice. The event belongs to the **adapter**, which is already generic
over it.

```go
func advance(q *ecs.Query[AdvancedQ], dt *ecs.In[float64]) { … }

ecs.ToHandler[app.UpdateEvent](world, advance, ecs.Feed(func(e app.UpdateEvent) float64 { return e.Dt }))
ecs.ToHandler[FixedTick](world, advance,       ecs.Feed(func(e FixedTick) float64 { return e.Step }))
```

`In[T]` carries a per-tick value; `Feed` is the projection, resolved at
registration. `TestOneSystemRunsUnderTwoUnrelatedEvents` runs one func under
both events in one engine, unchanged. Naming the event stays **legal** — a
System genuinely about a specific event should name it — but it is not the
shape the spec shows.

Two implementation notes. The `Feeder` holds the `In[T]` **instance** rather
than a way to build one, because a generic cannot be instantiated from a
`reflect.Type`; and the projection closure reads the event through the same
stable cell the event parameter would have used, so nothing is boxed per tick.

**And `In.Get()` should be read once outside the loop — that is a usage rule,
not an implementation detail.** `In` is a pointer to a cell the adapter writes,
so a `Get()` inside the loop is a load the compiler cannot hoist past the
Component writes: it has no way to prove they do not alias. The disassembly of
the built package confirms it, the unhoisted body re-issuing the load on every
Entity.

**No allocation cost at all**, which the implementation holds: a ten-thousand
frame steady state is **6.004 objects a frame at 1k and 6.001 at 10k** for `In`
+ `Feed`, against **6.004 and 6.003** for naming the event — the engine's own
line, identical either way.

**The time cost, however, did not survive implementation, and the number this
spec first carried should not be quoted.** The prototype measured it whole-frame
at 50 438 / 52 111 / 55 620 ns and read ~3% hoisted and ~10% unhoisted out of
that; whole-frame variance on the implementation is several hundred nanoseconds,
which is the same size as the effect, so the question is settled on the walk
alone. The same 10 000-Entity two-Component walk, the engine's publication out
of the picture, medians of five:

| the walk over 10 000 Entities | ns/op | ns an Entity |
| --- | --- | --- |
| the step in a local the compiler keeps in a register | 33 794 | 3.38 |
| `In.Get()` hoisted out of the loop | 34 356 | 3.44 |
| `In.Get()` left inside the loop | 34 008 | 3.40 |

**Under 2%, and not consistently ordered** — the unhoisted arm lands *between*
the other two. The disassembly says why, and it is a property of the iteration
shape rather than of `In`: `All()` is a `range`-over-func, so **the loop body is
a separate function**, and anything it reads from outside itself it reads
through its closure context. The hoisted body issues its own load
(`MOVSS 0x10(DX), X0`) exactly as the unhoisted one does. **Hoisting moves the
load; it does not remove it.**

The rule stays, because it is free to follow, because the aliasing fact behind
it is real, and because a loop body doing less work than this one's would make
the extra dependent load visible. What is withdrawn is the ~10%.

### What a mis-declared System does

**Under-declaration is unrepresentable**, as stated at the top: the only route
to a Store is a Query or an Accessor, and both declare. Retaining a `*Body`
across ticks is still a violation — of the kernel's existing "never store handle
values locally" rule, not a new one.

**Over-declaration is therefore the only real defect, and it is a performance
bug**, not a correctness one: correct results, needless serialisation. Catching
it means watching every write, which puts a barrier on the hot path requirement
1 exists to keep clear. **There is no runtime guard.** `Describe`/`Dump` already
report resolved read/write sets, so a System's lock set is inspectable at
composition time. If more is wanted later, the cheap version is a build-tagged
flag — one bool per Store per tick, not per Entity — warning once if a
write-locked Store was never written.

---

## The lock set

A System's lock set is the union of what every parameter declares, computed once
at registration by walking the func's parameter types. **The reflection runs
exactly once and never again.**

Four rules make the whole of it.

1. **Every handler that touches any Store declares `read{*Entities}`**,
   unconditionally and first. Not only Queries: Accessors, `Add` and `Remove`
   too, so the invariant is closed by construction rather than by the accident
   that everything happens to use a Query.
2. **A Query field's pointer-ness is its access mode**, and a filter field
   contributes a read of its Store.
3. **`Spawn` and `WriteableEntities` take `write{*Entities}`**, which supersedes
   the read and is therefore a **total barrier**: it excludes every System in
   the frame.
4. **`ecs.Read[T]`/`ecs.Write[T]` join the same set**, so a bound plugin's
   resource is as visible in the signature as a Component is.

### `write{*Entities}` is one entry, not N

This is the shape that dissolved [How a data-driven spawn names its lock
set](https://github.com/dvoyni/cog/issues/256). `Entities` holds a reference to
every Store, so `Despawn` empties all of them naming no Component at all — and
because every System touching any Store already declares `read{*Entities}`,
holding it for write excludes all of them. So a spawn whose Components are
chosen at runtime has **exactly** the lock set of one whose Components are
spelled in Go: there is nothing to name statically that is not already named.

Measured rather than argued, with a control, because an observed occupancy of 1
proves nothing unless a 2 was observable. Two Systems writing **different**
Components:

| | greatest occupancy |
| --- | --- |
| two Queries, disjoint writes | **2** |
| the same pair, first one spawning | **1** |

The static `Spawn[Bundle]`'s per-Component `declareWrite` is therefore
**redundant for locking**, and it is **kept as an ownership declaration**. That
is what makes cog's composition check fire, so a plugin spawning a `Health` must
declare a dependency on `Health`'s owner. Dropping it would let any plugin
fabricate any other plugin's Components with no declared relationship — a bigger
hole than the redundancy is a cost.

### What the barrier costs, and where the cost actually is

Three Systems over 2 000 Entities, the third iterating the same Entities in
every mode and differing **only in what it declares**:

| third System declares | ns/frame | allocs |
| --- | --- | --- |
| *(absent — two workers only)* | 18 633 | 10 |
| `read{*Entities}`, a Query | **30 230** | 11 |
| `write{*Entities}` — a `Spawn` parameter it never uses | **36 016** | 11 |
| the same, spawning and despawning one Entity a tick | **36 226** | 11 |

**The barrier costs ~6 µs a frame** — one scheduling round, exactly what the
~2.2 µs task floor predicts for a writer draining readers and then releasing
them. At 30 Hz that is **0.018% of a frame**, and it costs no allocation.

**The spawning itself is free.** Declaring a `Spawn` and never using it costs
36 016 ns; actually spawning and despawning every tick costs 36 226 ns, inside
the noise. **The entire cost is the declaration, and it is paid at
registration.**

That is the line with teeth, and it becomes a usage rule:

> **Split a rarely-spawning System out.** Because the `Uses` fold is static, a
> System holds `write{*Entities}` for its **entire run**, not for the instant it
> spawns. A System that queries 5 000 Entities and spawns one projectile on the
> last of them blocks the whole frame for the duration of the query. The cost is
> never the spawn; it is everything around it.

That hazard is also the motivation for [defer structural change through a typed
command buffer](https://github.com/dvoyni/cog/issues/260): what a command buffer
would buy is **lock duration**, not safety and not allocation.

### Two Systems, one Component, disjoint Entities: accepted

Two Systems writing one Component over Entity sets that never meet **serialise**,
and that is the answer. Not a reluctant one: the prize for solving it was
measured and it is **under a tenth of a percent of a frame** at the reference
workload ([Two Systems, one component, disjoint
entities](https://github.com/dvoyni/cog/issues/241)).

To price the ceiling rather than argue about it, K Systems did identical work on
private data and differed only in the lock each declared, so admission was the
only variable:

| Systems | Entities | Work each | Disjoint | Shared | Prize |
| --- | --- | --- | --- | --- | --- |
| 2 | 1000 | ~0.4 µs | 6.96 µs | 6.77 µs | **0.97×** |
| 8 | 1000 | ~0.4 µs | 18.70 µs | 27.85 µs | 1.49× |
| 8 | 5000 | ~2.1 µs | 23.99 µs | 52.82 µs | 2.20× |
| 8 | 5000 ×20 | ~43 µs | 120.38 µs | 402.50 µs | 3.34× |

**At the canonical example — two Systems, a thousand Entities — serialising was
marginally *faster* than running in parallel.** The cause is a floor: **a
scheduled task costs ~2.2 µs**, so a System doing 0.4 µs of work is five times
cheaper to run than to schedule. **The prize is a function of per-System work,
not of Entity count or System count.**

The harness is validated rather than assumed: 8 Systems sleeping 20 ms finish in
**20.7 ms disjoint against 161.6 ms shared, 7.81×**, so the scheduler
parallelises essentially perfectly when the locks permit. The small numbers
above are a property of the workload.

**It does not bite for nox**, and the check was against nox's own system list
rather than an invented example: every multi-writer its docs describe is
*overlapping*, not disjoint, and the one place it faced the question head-on it
**unified the writer** — "the character is a physics body, not a transform that
a movement system writes positions to". This is a prediction about a game that
is not written yet, and it is recorded as one.

**The remedy is guidance, not mechanism.** When two Systems genuinely write one
Component over sets that never meet, they are usually two different quantities
wearing one name — `WorldPosition` and `ScreenPosition`, not `Position` twice.
nox's docs contain the same instinct independently: "mass and weight are
different fields: mass drives the physics push, weight drives inventory
encumbrance."

**Filter-encoded disjointness is closed**, and "bevy removed it" is the weaker
half of why — see [Shapes that were rejected](#shapes-that-were-rejected).

### Making it visible

What is built instead is **visibility, in `kernel`**: a registration-time
conflict report. `Describe` already emits fully-resolved `Reads`, `Writes` and
`Uses` per handler with the transitive `Uses` closure folded in; the only thing
missing is the pairwise conflict computation. It is a pure function of data that
is immutable once `finalize` returns, so it cannot touch the hot path.

**The ECS makes the naive report noisy, and predictably so**: every System reads
`*Entities` and every structural change writes it, so a raw pairwise dump says
*"every System conflicts with every spawning System"* — true, unavoidable, and
useless as a list. **Per-resource contention is the shape that survives contact
with an ECS**: it puts `*Entities` at the top, correctly, and names the
Component Stores underneath. **Rank, do not enumerate.** Reported, never an
error and never a panic — bevy's equivalent ships off by default with four
suppression knobs, which is the honest measure of how noisy this class of
diagnostic gets. The delta is specified in
[`kernel/docs/specs/ecs-support.md`](../../../kernel/docs/specs/ecs-support.md).

---

## Structural change

A **structural change** is a change to which Entities have which Components, as
against a change to a Component's value: `Spawn`, `Despawn`, adding a Component,
removing one.

**There is no exclusion mechanism to build, and nothing in the ECS is a
Command.** That is the whole of [Structural change, and what excludes it from
iteration](https://github.com/dvoyni/cog/issues/240), and it retired all four of
that ticket's candidate answers at once. A `Uses` dispatch is not a scheduled
unit — it passes `noLocks, noLocks` and runs on the calling goroutine — and the
fold is **static**, run once at finalisation, transitively, with cycle
detection. So **a handler's lock set is complete before the frame starts**: a
System that can spawn holds the spawn's locks for its entire run, and the
scheduler has already excluded everyone.

### The shape

| handle | method | declares |
| --- | --- | --- |
| `ecs.Spawn[B]` | `.New(B) Entity` | `write{*Entities}`, plus `write{*Store[F]}` per Bundle field |
| `ecs.WriteableEntities` | `.Despawn(Entity) bool` | `write{*Entities}` |
| `ecs.Get[T]` | `.Of(Entity) (T, bool)` | `read{*Store[T]}` |
| `ecs.Set[T]` | `.Of(Entity) (T, bool)`, `.Ref(Entity) (*T, bool)`, `.UpdateFor(Entity, T)` | `write{*Store[T]}` |
| `ecs.Remove[T]` | `.From(Entity) bool` | `write{*Store[T]}` |

Spawn and Despawn are **two handles rather than one**, because folding `Despawn`
onto `Spawn[B]` would force a Bundle type on Systems that never spawn.

`Get[T]` has **no `Ref`**, and that is what stops a read handle being a write in
disguise. `Set[T].UpdateFor` **inserts when absent** — legal because it already
holds `write{*Store[T]}`, and safe during iteration because the reverse walk
never reaches an appended entry. So `UpdateFor` is how a Component is added;
`Remove[T]` is how one is taken away.

**Gap.** The prototype builds `Query`, `Spawn`, `WriteableEntities`, `Read`,
`Write` and `In`, and it was the accessors' *cost* that was measured — on the
Store model in `ecs-sparse-probe-bench`, not on a real engine.
`Get`/`Set`/`Remove` as kernel-bound handles were never composed, so their
allocation behaviour in situ is inferred from the others rather than observed.
What would settle it: build the three, add them to the whole-frame allocation
benchmark, and confirm they land on the same 6-per-tick line.

### `All()` walks its Driver backwards, and that is a guarantee

Iteration order is unspecified, but **direction is not** — and direction turns
out to be the whole answer to self-invalidation.

| 5000 Entities | ns/op |
| --- | --- |
| forward, bare walk | 2926 |
| reverse, bare walk | 2909 |
| forward, one probe | 4979 |
| **reverse, one probe** | **4569 — 8% faster** |

Reverse costs nothing and with a probe is slightly faster, since random access
dominates. What it buys:

- forward + swap-remove visits **667 of 1000** — it silently skips;
- forward + a compensating `i--` is correct, and is the line everyone forgets;
- **reverse + swap-remove is correct with no compensation**;
- reverse never reaches an Entity appended during the loop, where forward does —
  a System spawning one Entity per visited Entity **does not terminate** (10 001
  visits from an initial 100).

So the contract is: **you may restructure the Entity you are currently
visiting.** Changing another Entity's membership of the Driver Store is
**undefined**, and that is measured rather than hedged — removing a
non-current Entity still skips one, 999 of 1000.

### Reclamation is eager, and is therefore nothing at all

| 50 despawns/tick, 5000 Entities, 20 Systems | per tick |
| --- | --- |
| **eager** — 50 × 218 ns | **10.9 µs** |
| lazy — 0.05 µs of despawn, plus a per-candidate liveness tax | 19.5 µs |

**1.8× cheaper at nox's scale**, crossover at **~89 despawns a tick**. Lazy was
measured too: the per-candidate check costs +20.8% (4691 → 5666 ns), and a
Driver a fifth dead costs 1.334 ns per surviving Entity.

Eager additionally keeps three things lazy would have spent: **no Store ever
holds a dead Entity**; **`len(owners)` stays the exact population** the Driver
choice reads; and **`add` needs no orphan-slot branch** — without one, 1000
recycles of an index leak 1000 dense entries, measured.

So: swap-remove on `Remove[T]`, swap-remove on Despawn, **no compaction, no
shrink, no sweep**, and the index returns to the free list immediately — safe
not merely because generations are exact but because nothing stale is left to
trip over.

A Despawn reaches every Store through an interface carrying **exactly one
method**, `remove(Entity)`. That costs 9%: 85 Stores are 218 ns through the
interface against 199 ns direct, zero allocations. Driver selection reads
`len(owners)` through the typed path inside a Query, never through the registry.

**The storm case is recorded, not designed for**: 500 despawns in a tick costs
109 µs, a third of a 33 ms budget. The answer if a game hits it is
[#260](https://github.com/dvoyni/cog/issues/260)'s deferred buffer, not a second
liveness model carried from the start.

### Spawn is a handle, not a Command, by a factor of 2270

| | ns/call | allocs |
| --- | --- | --- |
| `Uses` dispatch | **1113** | 0 |
| direct call on a handle the System already holds | **0.49** | 0 |

The zero allocations identify the cause: a subscription's Kernel sets
`bounded: true`, so the dispatcher **skips** the per-call context setup that
would itself cost 169 ns and 4 allocs. What remains is the **scheduler
round-trip to the coordinator goroutine**, paid even though a declared dispatch
requests no locks and therefore cannot block. At a 16-missile volley that is
17.8 µs per cast.

A handle declared in the System's signature declares the same lock set through
its own `Lock`, with no dispatch at all. Bundle fields are reflected **once at
registration** into one cached closure per field — the mirror of the Query fill:

| | ns/spawn | vs hand-written | allocs |
| --- | --- | --- | --- |
| `Spawn[struct{Body; Collider}]` | **15.7** | 1.47× | 0 |
| `Spawn[…4 fields]` | **26.0** | 2.11× | 0 |

The same volley costs **416 ns — 43× less**. **The trap, which an implementation
will hit:** the obvious spelling allocates. Passing the bundle by value and
taking `&v` hands the address of a parameter to an opaque func value, so the
bundle escapes — **48 B and one allocation per spawn**, 28.4 against 15.7 ns.
Copying into a buffer bound at registration removes it, sound because the
handler holds `write{*Entities}`.

**A Bundle is not a Component set.** It describes one act of creation; the
Entity may gain and lose Components afterwards without the Bundle meaning
anything. A Bundle field simply *is* a Component field — there is no conversion
step, because a hash is pure and can be written at a package-level `var`, so a
declarative spawn naming a model by name needs nothing from the ECS:

```go
var crateModel = ecs.HashOf[ModelHash]("models/crate.glb")

sp.New(DeclBundle{
    P: Placement{Scale: 1},
    D: Drawable{Model: crateModel, Layers: scene.LayersAll},
})
```

### There is no command buffer, and the reason is allocation

A type-erased command buffer costs **one allocation per queued command** — 50
allocs / 1600 B per tick, 4920 ns against 4147 for a typed queue. At 30 Hz that
is 48 KB/s fed to the collector during frames, which requirement 1 forbids. So
**no general command buffer is built and `Add`/`Remove` stay immediate.**

### What a System sees

Because every handler touching a Store holds `read{*Entities}` and Spawn and
Despawn hold it for write, **a restructuring System and any other System can
never run concurrently**, and torn state is unrepresentable.

**No new promise is made:** a System sees every structural change made by
Systems that ran before it and none from those that ran after; where two declare
no ordering, which ran first is unspecified.

### Structural hooks are out of scope

`OnAdd[T]` / `OnRemove[T]` would fire inside a structural change, under its
lock, so they cannot be cog events without re-entering the world mid-mutation —
and a callback there holds exactly the changing Store's lock and nothing else. A
game's lifecycle hooks (create, init, die, pickup, drop, …) are ordinary cog
events and the ECS owns none of them. **Ruled out of scope, not deferred**, so a
future need arrives with a use case rather than as a reserved hole.

### The load-bearing invariant the kernel cannot enforce

A Despawn reaches every Store through Go pointers held by `Entities`, which the
kernel does not police. **That traversal is sound only because every handler
touching a Store declares `read{*Entities}`.** It holds by construction — the
only route to a Store is a generated handle, and the generated `Lock` always
emits that read — but it is written into the spec as an invariant, not left
implicit, because an implementation that added a second route would break it
silently.

It is also why the obvious optimisation stays forbidden. A per-Entity bitmask of
"which Stores hold me" would let a Despawn skip the scan, but the mask lives in
`Entities`, so maintaining it would move **every add from `write{*Store[T]}` to
`write{*Entities}`** — and every Component addition would then exclude every
Query in the frame. A sharper form of the standing corollary: **any global index
is a global lock.**

---

## Reaching another Entity

A Query reaches only what it drives over. A missile's target is reached through
an `Entity` stored in a Component — a **Reference** — and followed with an
Accessor.

| 5000 Entities | ns/op |
| --- | --- |
| `Body` as a Query field | 4693 |
| a scattered target's `Body` **through the Reference** | **4360** |
| both — the real homing shape | 8213 |

Random access is the same probe the Driver already pays, **not a second-class
path**. Three guarantees, written as tests, two of them free consequences of
eager Despawn:

- **a Reference to a despawned Entity simply misses** — no liveness check of its
  own, because the Despawn already emptied every Store;
- **a recycled index never aliases a stale Reference** — the generation is in
  the slot;
- **a write pointer *is* invalidated by a structural change to that Store** —
  swap-remove relocates the data and a retained pointer addresses a stale slot.

### Liveness has two questions and the one you want is free

- *"Does `e` still have Component `T`?"* → the Accessor's own probe, needing
  **`read{*Store[T]}` — a lock the System already holds**.
- *"Does `e` exist at all?"* → `Entities.Alive(e)`, needing `read{*Entities}`.

**The first is almost always the right question.** "Is my target still alive"
really means "does my target still have `Health`". Stating that plainly matters,
because the alternative pushes a wider lock into every System that holds an
Entity across frames.

### A Reference points one way

**A Query selects on presence and on nothing else.** No Query narrows by what a
Component *contains*, so finding every Entity whose Reference points somewhere
in particular is a comparison the System makes itself, once per candidate. The
far Entity does not know it is referenced and **nothing anywhere lists what
points at a given Entity** ([Can a Query select by a
relation?](https://github.com/dvoyni/cog/issues/267)).

**A bounded run of References is the sanctioned many side, and it earns no new
word.** `[4]Entity` is pointer-free, so it is already a legal Component field;
what this spec adds is that it is legal *on purpose*. It carries one sharp edge,
stated rather than discovered: **the cardinality is fixed in the type, and the
engine caps nothing.** A game with a one-to-four creature cage writes `[4]Entity`
and a live count; a game with an inventory bounded by carry weight rather than
by slot count has to pick an N the engine will not pick for it.

**The documented escape for the reverse direction is to scan.** Query
`{P Parent}` and compare `.Entity == target` in the body, costing the length of
`Store[Parent]`. That is not a mechanism — it is an ordinary Query and user
code, requiring nothing of the ECS.

**A stale slot is the game's to compact.** A dangling Reference is safe to read,
but the slot still holds the handle, and for a container with a live count that
is a hole the count no longer describes. Detecting it is free on the read that
was already happening, so the game compacts lazily as it walks. Centralising it
would need the reverse index refused above: **the same argument kills both**,
which is why they are recorded together.

**Composing a transform down a tree needs nothing from the ECS.** It is a System
reading a `Parent` Reference through an Accessor — a plain probe — and the
ordering is the game's. `scene` bakes its own hierarchy at load
(`scene/model.go:47`, "row 0 of every model is the authored hierarchy resolved
once"; `gltfload.go:421` is the only glTF node walk and it runs at load), so the
relation that usually forces an ECS to grow relations is answered on the far
side of the binding. `Parent`, `Child` and `Hierarchy` are a game's words for
its own Components; the engine has no opinion about them.

---

## Binding: how another plugin attaches

**There is no binding mechanism, and that is the decision.** A plugin that is
not the ECS attaches to the world by being an ordinary plugin: it registers
Components if it has any, subscribes Systems like anything else, and reaches its
own frame-local resource from inside them. The ECS gains exactly one thing — a
System may take anything a cog handler may take — and **no new vocabulary at
all** ([The binding shape](https://github.com/dvoyni/cog/issues/246)).

This was settled against the real `scene` package rather than against physics
and audio, which do not exist and therefore cannot answer it. Scene is also the
case a game reaches for first.

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

`ecs.Read[T]` and `ecs.Write[T]` are the only addition. They wrap the
`kernel.Read[T]`/`kernel.Write[T]` a handler's `Lock` already declares, reached
through the signature instead of through a `Lock` func — so the resource joins
the System's lock set beside its Query's Stores, at registration, as visibly as
a Component is. **No binding type, no adapter, no registration call of the ECS's
own.**

Neither is a place to keep anything: the value is refreshed per tick and is
valid only for the body of the System, under the kernel's standing rule that a
value read from a handle lives only as long as the handler holds its lock.

### The binding is necessarily a third plugin

`ecs` imports only `kernel`; `scene` imports nothing of `ecs`. **Neither can
know about the other**, so a binding is necessarily a third plugin that imports
both. That is what "there is no binding mechanism" means in practice, and it has
a consequence worth stating: **a project not using the ECS schedules no ECS
Systems** — it simply does not register that plugin.

**cog ships the bindings for its own in-house plugins** — ecs↔scene, and physics
and audio when they exist. They remain separate plugins, because the import
graph allows nothing else, and a game that wants its own instead simply does not
register cog's.

### Data flows one way, and for scene that is not a choice

`scene.OpQueue` is frame-local and re-recorded from nothing every frame; scene
retains residency and **no per-entity state whatsoever**. There is no scene-side
object for a drawable Entity to be a copy of, so "two copies and a sync cost
every frame" does not arise, and neither does the coupling worry on the other
side: **the Components are the source of truth because there is no other
candidate.**

This does **not** generalise to physics, which does retain bodies. The direction
question is the bound plugin's to answer and stays
[#182](https://github.com/dvoyni/cog/issues/182)'s. What is settled is that the
*mechanism* is the same either way.

### The wide lock lands in the bound plugin, not in the ECS

Not where it was expected. `*scene.OpQueue` is **one resource**, so every
recording System serialises against every other recording System for write,
whatever Components they read. **The ECS's per-Store granularity buys nothing on
the recording side** — that is a property of the bound plugin's API. Scene
publishes one queue, so scene recording is one lock wide.

Which is fine, because splitting it would lose anyway. Whole-frame, publish →
locks → Systems → wait:

| entities | ns/op | allocs/op |
| --- | --- | --- |
| 0 (baseline: the same two Systems, nothing to record) | 10 700 | **10** |
| 100 | 17 202 | **10** |
| 1 000 | 72 553 | **10** |
| 5 000 | 313 249 | **10** |

**Zero allocations from the binding**: identical at 0 and at 5 000 Entities, and
sitting exactly on the 2-per-publication-plus-4-per-subscriber line. Time is
linear at ~60 ns a drawable. Against that, a second System costs about **5.9 µs**
of scheduling floor, so a second recording System would have to save visiting
~110 Entities to pay for itself — and it still could not run concurrently,
because both hold the queue for write. **One recording System per bound plugin
is the shape.**

### Ordering needs nothing new

`scene.Plugin` subscribes its flush `.Last().Before[gfx.UpdateEventHandler]()`,
so a recording System that does not ask to be last already runs before it. No
new ordering vocabulary, demonstrated in the prototype with a `.Last()` stand-in
for the flush.

### What a binding plugin may not do

Stated as prohibitions, because a backend adopted from outside cog will not have
been written with them in mind.

- **A Component holds no pointer, transitively** — enforced at registration,
  where the type is named and nothing has been stored yet.
- **Never hand the bound plugin a pointer into a Store.** `ModelDraw.Transform.Matrix`
  is a `*m.Mat4` **retained by value** in scene's record until the flush — and
  the flush is a *different* System, running after the recording System's locks
  are gone. A matrix pointing into a Component Store would be read unlocked. Use
  a TRS form, or point into scratch that outlives the frame.
- **Variable-length draw data is not a Component.** Play lists, morph weights
  and override params are built in System-owned scratch and rebuilt each frame;
  scene copies each into its own arena at record time and says so, so the caller
  may reuse the backing the moment the call returns. **Scratch captured in a
  closure is safe for one System only** — two Systems sharing it have no lock
  between them. If in doubt make it a resource, which puts it in the lock set.
- **Do not cache an Entity without checking liveness, and do not restructure the
  world from inside another Entity's iteration.**

### One measured thing that is scene's problem, not the ECS's

`scene` re-resolves a model path per recorded draw per frame at **46.9 ns**
against **0.54 ns** for a dense index — ~235 µs a frame at 5 000 drawables,
three quarters of it re-normalising a path that was validated at load. The ECS
binding is correct and allocation-free without any change there, so it is
recorded as [scene: name a model by an interned handle, not by its path every
frame](https://github.com/dvoyni/cog/issues/263) and is out of scope here.

---

## More than one world

**Yes, more than one may exist. A second world is a second `kernel.Engine`,
named by the Go variable that holds it. No phantom tag, and no change to the
ECS** ([May more than one World exist](https://github.com/dvoyni/cog/issues/245)).

The question had been posed one layer too low. A resource cell is keyed **per
engine registry**, not per process, so two engines holding the same Component Go
type hold two cells, two stores and two locks.
`TestTwoWorldsDoNotSerialiseAgainstEachOther` measures the feared cost directly:
both Systems hold the **write** lock on the same `*ecs.Store[Marked]` type at the
same instant, each blocked on a barrier until the other arrives. If the lock
were shared the second never enters and the test times out. It does not.

**The obstacle to two worlds in one engine is the id authority, not the Store.**
Composing two does not fail on a Component at all:

```
plugin "world-b" registered duplicate resource *ecs.Entities already owned by "world-a"
```

`*ecs.Entities` is one cell per engine and is the **first** thing that collides,
so a phantom tag on `Store` alone would buy nothing — it would have to fork
`Entities`, `Spawn`, `WriteableEntities` and every Query field. Renaming the
plugins does not help either, because the collision is on the cell rather than
the name.

**A second world costs 2 goroutines** — the engine's `Run` loop plus one
scheduler coordinator. There is no worker pool to duplicate, and kernel's only
process-global is a `sync.Pool` of lock requests, shared safely by construction.
It needs no second OS loop: `app.UpdateEvent` is a plain struct, so the engine
owning the frame drives the other through its `Executioner`. **One hazard,
recorded rather than hidden:** that nested wait blocks a *task* in the driving
engine while it holds its locks. It completes because the driven engine's
coordinator and lock tables are separate, but it deadlocks if the driven world
ever waits back into the driver — so synchronous flow is safe in one direction
only.

### The price of changing our mind, and the escape that already exists

To put two worlds inside **one** engine later, a tag must propagate through
`Entities`, `Store`, `Spawn`, `WriteableEntities`, every Query field and
therefore every System signature in every game. Go has no defaulted type
parameters, so there is no way to add it non-breakingly. That is the full price.

But the reversal is cheaper than that number suggests, because **type-level
separation is already available with no ECS change at all**: a user-space
generic wrapper — `Tagged[Marked, serverW]` and `Tagged[Marked, clientW]` —
registers as two distinct Component types and gets two distinct stores and two
distinct locks through the ordinary path. What it does *not* fork is the id
authority: one Entity id addresses both. Which fits the motivating case better
than a second world would, because for client prediction the predicted and
authoritative values belong to the **same** Entity.

So, by use:

- **server world beside a client world**, editor preview, headless test world →
  **a second Engine**;
- **predicted beside authoritative on the same Entity** → **tagged Component
  types in one world**, no new machinery;
- **a rollback snapshot** → **neither**; it is a copy of store memory, not a
  world at all.

---

## The zero-allocation claim

**Zero, and the design stands** — but it was reached through a failure, and the
way it failed is the useful part, because the fix is a spec constraint
([The zero-allocation proof](https://github.com/dvoyni/cog/issues/243)).

What was measured is not a microbenchmark: the decided design on a **real
`kernel.Engine`**, driven by a **real `app.UpdateEvent`** at 30 Hz, because the
`any`-boxed resource cell, the real lock acquisition and the reflective call are
exactly where an allocation hides and none of them exists standalone.

`allocs/op` per **whole frame** — publish, acquire every declared lock, run every
System, wait:

| shape | allocs/op | B/op | ECS share |
| --- | --- | --- | --- |
| nothing subscribed | 2 | 160 | — |
| **1 hand-written subscription, no ECS** | **6** | **320** | baseline |
| 1 System, reflected, 1k **and** 10k | **6** | 320 | **0** |
| 1 System, baked, 1k and 10k | **6** | 320 | **0** |
| 3 Components / `Without` filter | **6** | 320 | **0** |
| 2 Systems, disjoint writes | **10** | 608 | **0** |
| move + spawn + despawn every tick | **10** | 611 | **0** |

**The engine charges 2 per publication plus 4 per subscriber, and every ECS
shape lands exactly on that line — so the ECS contributes nothing**, including
`Spawn` and `Despawn` running every tick in the last row.

Identical at 1k and 10k, which is the real test: an allocation in the iteration
would scale with entity count, and none does. **Steady state over 10 000
frames**, because an average over `b.N` can hide amortised growth: **6.005
objects a frame at 1 000 Entities, 6.003 at 10 000**; with structural change
10.048, flat. Ten thousand spawns and ten thousand despawns add nothing — the
free list recycles ids and the Store reuses the dense row, so growth stops at
the high-water mark.

**Escape analysis reports no `moved to heap` anywhere on the iteration path**, so
the zero is *explained by the compiler* rather than merely observed.

### Three constraints bought it, and none is visible in a microbenchmark

All three appeared only in situ, and all three are binding on the
implementation.

1. **The Query's fill buffer is a field of the `Query`, not a local in
   `All()`.** As a local its address reaches an opaque `yield` and it escapes —
   **one allocation, 24 B, per Query per tick**, the width of a two-Component
   query struct, and the compiler names it (`moved to heap: buf`). In a
   microbenchmark the iterator inlines at the range site and the buffer stays on
   the stack; across a package boundary with a real callee — how every real
   System will be written — it does not. Nothing changes semantically, since the
   same buffer was already reused for every Entity.
2. **`Spawn` stages its bundle through a field**, same hazard: taking the
   address of the bundle parameter and handing it to cached closures
   heap-allocates it per spawn.
3. **A System returns nothing**, because `reflect.Value.Call` allocates for a
   callee that does. The builder rejects one at registration.

### Time, and one number worth another look

Per Entity, from the 1k→10k slope so the per-frame floor drops out:

| | ns/entity | × hand-written |
| --- | --- | --- |
| hand-written loop, 2 Components | 2.07 | 1.00 |
| **Query, 2 Components** | **3.21** | **1.55** |
| Query, 3 Components | 10.8 | 5.2 |

Consistent with the 1.37× measured against a looser baseline. But **a third
probe costs ~7 ns an Entity more, whether it is a third Component or a filter**,
and that is *not* the loop shape — both attempts to fix it as a loop-shape
problem made it worse and were reverted. **Gap:** the third-probe cost is
unexplained. It matters because a spec that names ~8 Components per System as a
limit is implying a curve that this says is not linear. What would settle it: a
width sweep from one to eight probes at fixed entity count, with cache-miss
counters, before any guidance recommends a Query width.

### Concurrency

Two Systems with disjoint write sets:

| | GOMAXPROCS=1 | GOMAXPROCS=32 | speedup | allocs |
| --- | --- | --- | --- | --- |
| 1 000 Entities | 12.3 µs | 13.3 µs | **0.92×** | 10 both |
| 10 000 Entities | 64.6 µs | 44.0 µs | **1.47×** | 10 both |

The scheduler does run them concurrently and concurrency allocates nothing. **At
1 000 Entities parallel is slower than serial** — the ~2.2 µs task floor
reproduced independently, from the other direction.

### Bake the call

| System that does nothing | per tick |
| --- | --- |
| baked builder | 3.04 µs |
| reflected builder | 5.15 µs |
| **difference** | **2.1 µs** |

Attributed rather than guessed: a hybrid builder keeping every other part of the
reflective path — same signature reflection, same `reflect.New` parameters, same
stable event and Kernel cells — and changing only `Call(args)` to a direct call
lands on the baked number.

**Gap, and it is recorded as one rather than papered over.** The same call costs
**79 ns in a tight loop** and **2100 ns inside a real publication**. Four
explanations were tested and **all four rejected**: caches evicted between calls
(90 ns), a fresh goroutine per call (164 ns), a fresh goroutine with a deep
stack (155 ns). The cause is unexplained; the consequence is not. 2.1 µs is the
whole ~2.2 µs scheduling floor, so **a reflected call roughly doubles the cost
of dispatching a cheap System.**

The design does not depend on the resolution: both builders measure 6 allocs a
frame, so **the reflective builder is correct and the baked one is an
optimisation**. Fixed-arity generic constructors (`ToHandler1`, `ToHandler2`, …)
are a **preference**, not a forced fallback — worth having because 2.1 µs a
System a tick is real, not worth blocking v1 on, and orthogonal to
[#257](https://github.com/dvoyni/cog/issues/257)'s generated fillers.

### Correctness was checked before the cost was believed

A zero-allocation loop computing the wrong answer proves nothing. Writes land
through a pointer field; the `Without` filter drops exactly the tagged tenth;
the population holds steady at n+1 across 500 ticks of spawn-and-despawn.

---

## What is not foreclosed

**A constraint nobody wrote down is not a constraint**, so each of these says
what would have to change, in what register.

**Change detection — additive, and the shape is already settled.** `Added[T]`
and `Changed[T]` arrive as further Query field types with the lock derivation
unchanged, which is exactly the query-vocabulary growth axis requirement 3
named — except that, unlike `Without`, they contribute a real **read**. What was
never decided is the **storage and its per-frame cost**, and v1 ships without
it: no consumer has asked, `Chunk` is already reserved for the one thing a real
block would buy (a run of rows sharing one change version), and the open half
touches the zero on the *write* path, so it must be measured on the same footing
rather than argued. → [ecs: where a change tick lives, and what it costs a
frame](https://github.com/dvoyni/cog/issues/268).

**Data-driven spawning and serialisation — additive.** No Component, Query,
System signature or lock changes when it arrives, and the expensive part is
already paid: Components are pointer-free *explicitly so they serialise*. Three
things are genuinely missing — naming a Component type from a string, decoding a
row into a Component value, and closing the hole that reaching a Store through
`Entities` declares nothing, so `ErrUndeclaredDependency` never fires on that
path. →
[#266](https://github.com/dvoyni/cog/issues/266).

**Per-entity data a Component cannot hold — additive.** A side store keyed by
Entity, emptied by the Despawn that already exists, touching no Component, Query
or System signature. →
[#264](https://github.com/dvoyni/cog/issues/264).

**Deferred structural change — additive, and it buys lock duration.** Not safety
and not allocation, both of which are already had. →
[#260](https://github.com/dvoyni/cog/issues/260).

**Relations — not additive, and the promise is weaker on purpose.** The only
route to answering "who points at me" cheaply changes what a `Store` is, so the
promise is that **nothing in v1 makes it harder**, not that it drops in. What it
would cost is named in [Shapes that were
rejected](#shapes-that-were-rejected). →
[#267](https://github.com/dvoyni/cog/issues/267).

**Chunk-parallel execution — dropped from v1 entirely, and the non-foreclosure
obligation withdrawn with it.** No spec carries a clause for it, and this is the
one entry here that is *not* a promise. The three findings are worth keeping
because they are what a future proposal has to beat
([#242](https://github.com/dvoyni/cog/issues/242)):

1. **The reference workload cannot use it.** nox's expensive Systems are
   expensive *because they leave the Entity* — collision line traces, projectile
   sweeps sampled at ~6-unit intervals, a behaviour tree of ~37 guarded actions,
   and the vision sweep, which at 17–35 µs is the costliest thing measured
   anywhere in its design and is **not a per-entity loop at all**. Animation
   sampling is the only entity-local heavy candidate in the whole design.
2. **A shard is a scheduled task, so it pays the same ~2.2 µs floor.** At ~5000
   Entities a full-Store walk is ~2.1 µs, so an 8-way split would put every
   shard an order of magnitude under the floor. Bevy reached the same place from
   the other side: adaptive batching shipped with a measured regression
   (216 µs → 316 µs), attributed to "generating too many small tasks".
3. **Preserving the option was not free.** Splitting the Driver's index range
   breaks the backwards-walk guarantee that "you may restructure the Entity you
   are on" is built on — measured, running shards *sequentially* so no race is
   involved: whole array 1000/1000 visited, split into 2, 4 or 8 ranges
   **500/1000**, silently. So the obligation would have cost either that
   affordance or [#260](https://github.com/dvoyni/cog/issues/260) pulled into
   v1.

   Adding it later therefore needs, in order: **deferred structural change
   first** (a hard prerequisite, not an optimisation); a **splittability rule
   over the System signature**, which cog is unusually well placed to derive at
   registration because the signature *already is* the out-of-band declaration
   every other engine makes the author write by hand (`Query` including pointer
   fields is safe, since each Entity is visited by exactly one shard; `Set[T]`,
   `Spawn[B]`, `WriteableEntities`, `Remove[T]` and `kernel.Write[T]` are
   disqualifying; `Get[T]` is safe only where `T` is not also written by the
   same System); **the unit being one `All()` loop rather than a System**, since
   a cog System may take several Queries; and an **explicit opt-in**, because
   safety is derivable from the signature and profitability is not.

---

## Shapes that were rejected

Recorded so they are not reinvented, each with the reason that actually killed
it rather than the first objection raised.

**Archetype tables.** Lost on three counts, in order. The lock unit is the
Component type, and under archetype tables `Position` is a column smeared across
every table containing it, co-owned by structures several other Components own —
so it does not map onto a resource cell. A structural change rewrites a row
across two tables, and its lock set is not knowable until runtime. And the
iteration advantage is **not the one the textbook advertises**: ~2–2.5× at 10k
on a two-Component query, which **inverts to 4×–19× against archetypes** on a
one-Component query spread over 26 archetypes. A game with 85 tag types
guarantees that regime; 512 Entities over 257 archetypes was measured reserving
**22.5 MB against 52 kB used, 434×**.

**`World` as a kernel resource — unsound, not merely unnecessary.** Resources are
keyed by `reflect.TypeFor[T]()` and **nothing normalises pointer-ness**: there is
no `Elem()` call in `resource.go`, `registrar.go` or `scheduler.go`. So
`Read[World]` and `Write[*World]` are two unrelated cells that **do not exclude
each other**, and the structural guarantee fails *silently*. The general rule
this generalises to, and which the implementation must hold: **one spelling per
resource, everywhere** — which is why `Entities` is `*Entities` in every
declaration in this document.

**A cached query index, an archetype graph, or a per-Entity "which Stores hold
me" table.** All the same shape and all refused by one corollary: **any global
index is a global lock.** Each is shared mutable state every structural change
must update, which recreates exactly the exclusion sparse sets were chosen to
avoid. Sparse sets' advantage is the *absence* of a global index; a design that
adds one back has spent it.

**flecs-style relation pairs** — a `Store` holding many rows per Entity keyed
`(relation, target)`. The only shape that makes the reverse direction cheap
without a separate index, and refused **for what it costs**: it breaks *an
Entity holds at most one Component of a given type*, gives `Store` a second row
shape and a second probe, and changes the Driver's length from an entity count
to a row count, which driver selection is built on. A core invariant traded for
a capability with no caller.

**Filter-encoded disjointness** — recognising `<&mut P, With<L>>` and
`<&mut P, Without<L>>` as compatible. Structurally impossible here, and bevy's
having deleted it is the weaker half. A filter **does not reduce to anything
key-shaped**: it stays a pairwise, non-transitive predicate, so with A =
`<&mut P, With<L>>`, B = `<&mut P, Without<L>>` and C = `<&mut P>`, A–B are
compatible while A–C and B–C are not. A reader-count/writer-set table keyed on
`P` must simultaneously **admit B and block C**, which it cannot represent.
Bevy avoids this by having no lock table at all — an n×n matrix over an
enumerable system set — and `scheduler.acquire` is an **open channel with no
such n**. The synthetic-key escape needs a non-generic lock API (impossible: a
generic cannot be instantiated from a `reflect.Type`), a **variable-length loop
in `Lock`**, and genuine partitions, which only `With`/`Without` of the *same*
tag provides. Bevy's own removal in 0.16 measured **−38% worst case against
+56% best**, its maintainer concluding *"our current parallelism is too
fine-grained for realistic projects."*

**Lock per (Component × store).** Dissolved rather than lost: it was written
while archetype tables were live, and under sparse sets there is exactly one
Store per Component type, so (Component × store) *is* (Component). Recorded so
no future reader thinks it was weighed and beaten on merit.

**Reordering shared storage to suit one query** — EnTT groups, shipyard packs.
Nobody has made it work: both reject or removed overlapping sets, and
exclusivity is what kills it. If a candidate design reaches for it, that is the
counter-evidence.

**One bitfield Component instead of 85 Tags.** 14% faster to iterate, and it
collapses 85 independent lock units into one.

**A paged sparse index.** 93% more per probe, and it saves nothing once
recycling scatters indices.

**A `ResourceFactory` on the kernel.** Explicit registration is not a near miss
but what actually ran, on an unmodified kernel — so the questions hanging off a
factory (what replaces `ErrMissingResource`, what happens when it declines, what
`Describe` says about a resource that appeared from nowhere) do not arise.

**A non-generic `ResourceAccess` entry point.** Not needed: the generic call is
baked into a closure at registration and looked up by `reflect.Type` afterwards,
so the impossible instantiation is never attempted. Worth recording that adding
one later would cost nothing in *safety* — `GetRead[T]` is
`reflect.TypeFor[T]()` followed by the same map lookup, and ownership is checked
at `finalize` against the resulting set, not at the call — only the ability to
grep for who locks what.

**A general command buffer for structural change.** Type-erased, it costs one
allocation per queued command. Deferred structural change returns post-v1 as
[#260](https://github.com/dvoyni/cog/issues/260), typed, and motivated by lock
duration.

**A process-wide interner for names.** 5× the per-entity cost, it allocates, it
cannot be read at package initialisation, it gives different ids in different
processes, and it has no answer to what empties it.

**`RegisterConversion`, a Bundle-field conversion.** Built and then removed: it
existed because a dense index could not be written where the Entity was
declared. A hash can, because hashing is pure, so the Bundle field simply *is*
the Component field and there is nothing to convert.

**A pointer hash.** A pointer is not a string with an awkward shape. Hashing one
answers no question — the object cannot be recovered from the hash, and an
address is not an identity anything else can agree on. The reference cases
decompose and each already has an answer: another Entity → `Entity`; an engine
object → not in the Component at all, but named in the System's signature; an
asset → a hash.

**`Archetype` as a word.** Retired outright rather than annotated. An archetype
is the *exact* Component set of a group of Entities, which this design neither
matches on nor represents. `View` was rejected too: it is EnTT's word and in Go
connotes a slice-like value, which a Query is not.

---

## Required work

A checklist for implementation sessions, in dependency order. Nothing here
exists.

**`ecs` package — the core**

- `Entity`, `NoEntity`, `fmt.Stringer`, private index/generation accessors.
- `Entities`: id allocation with a free list, generation tracking, `Alive`, the
  reference to every Store, eager total `Despawn`, `NewEntities(ids uint32)`.
- `Store[T]`: the three arrays, the one-load probe, swap-remove, `append`
  doubling, a reserve hint, and the type-erased `storeCore` carrying exactly one
  method.
- `PointerFree(reflect.Type) error`, reporting the offending field by path.
- `RegisterComponent[C]`: `InitResource[*Store[C]]` on the caller's `Registrar`,
  enrolment with `Entities`, the baked per-type closures, and the pointer-free
  check.
- `Plugin(world *Entities) kernel.Plugin`.

**`ecs` package — Query and System**

- `Query[Q]`: registration-time planning of the field table, per-run Driver
  selection by scanning Store lengths, `All()` walking the Driver backwards over
  a half-open index range, the fill buffer **as a field of the Query**, and the
  unrolled fillers chosen by field count.
- `Without[T]`, and `With[T]` if it is wanted in v1; the fill skips blank
  fields.
- The registration-time error for a Query naming no present-typed Component.
- `ToHandler[E]` and `ToExecute[Req, Res]`; the parameter classification table
  above is the contract; a System returning anything is rejected.
- `In[T]` and `Feed`.
- `Read[T]` and `Write[T]`.
- `Spawn[B]` staging its bundle **through a field**; `WriteableEntities`.
- `Get[T]`, `Set[T]` (`Of`, `Ref`, `UpdateFor`), `Remove[T]` — the three the
  prototype did not build.
- `HashKey`, `HashOf[K]`, `NoHash`, `Names[K, V]` with `Register`, `Lookup` and
  `TextOf`, and the collision check.

**`kernel`**

- The conflict report on `Describe`, per
  [`kernel/docs/specs/ecs-support.md`](../../../kernel/docs/specs/ecs-support.md).
  Nothing else. Every other kernel question this map opened closed with "no
  change".

**Documentation**

- `ecs/README.md`, which is the package's API per the repo's own layout rule.
- `README.md`'s package list gains `ecs` when the package exists.
- `.github/instructions/kernel.instructions.md`'s "Keep `Lock` Straight-Line"
  becomes "Keep `Lock` Deterministic, Total and Final" — full replacement text
  in the kernel delta.

**Verification, and the bar is not `allocs/op` alone**

- The whole-frame allocation benchmark on a real engine at 1k and 10k, with and
  without a structural change per tick, and a 10 000-frame steady state. The bar
  is **6 allocations a frame with one subscriber, 10 with two** — the engine's
  own line — and *identical* at both entity counts.
- `-gcflags=-m` on the iteration path showing no `moved to heap`, so the zero is
  explained rather than observed.
- Timings in the classic `b.N` form, **never `testing.B.Loop`**, beside a
  hand-written slice loop as the baseline.
- The correctness tests that must pass before any number is believed: a write
  through a pointer field lands; `Without` drops exactly the tagged set;
  population holds steady across hundreds of ticks of spawn-and-despawn; a
  forward walk is *shown* to skip so the reverse walk is a tested guarantee, not
  a comment; a Reference to a despawned Entity resolves to nothing; a recycled
  index does not alias a stale Reference.
- **`-race` in CI.** Every measurement behind this document was taken in an
  environment where the race detector cannot build, and `-count=10` was
  substituted. That substitution must not survive into the implementation.

---

## Out of scope

Recorded on [the map](https://github.com/dvoyni/cog/issues/180) and repeated
here so a reader of the spec alone does not re-propose them.

- **Refactoring `scene` and `anim` onto the ECS.** A large, risky refactor of
  code that works, against a design with no consumer yet.
- **Third-party storage strategies** — a plugin supplying sparse-set storage
  beside archetype tables. Lock granularity would stop being a static property
  of the Query type, which destroys the premise.
- **Replication and networking implementation.** Cheap constraints only, so
  nothing is foreclosed; the pointer-free rule is the whole of what v1 pays.
- **The physics contract itself** — [Physics plugin: contract, swept queries,
  and an adopted backend](https://github.com/dvoyni/cog/issues/182), its own
  map. This spec fixes only the binding shape physics attaches through.
- **Structural hooks** (`OnAdd[T]` / `OnRemove[T]`) — ruled out, not deferred.
- **Chunk-parallel execution**, entirely, with its non-foreclosure obligation
  withdrawn. [#242](https://github.com/dvoyni/cog/issues/242) holds the full
  record.
- **Change detection's storage and per-frame cost** —
  [#268](https://github.com/dvoyni/cog/issues/268). The shape is settled and
  stays settled.
- **Data-driven spawning and world serialisation** —
  [#266](https://github.com/dvoyni/cog/issues/266). Includes **a
  runtime-addressable template**: a template is already just a value of a Bundle
  type reused at every spawn site, and needs nothing from the ECS. `Template`
  and `Prefab` stay unspent.
- **Per-entity data a Component cannot hold** —
  [#264](https://github.com/dvoyni/cog/issues/264).
- **An engine-owned transform hierarchy.** Resolved out rather than deferred:
  the ECS's part is already built, and a consumer wanting the engine to own the
  ordering would be a new effort against a redrawn destination.
- **Interning a model name inside `scene`** —
  [#263](https://github.com/dvoyni/cog/issues/263). Scene's own hot path and
  scene's own decision; the binding is correct and allocation-free without it.
- **Generated monomorphised query fillers** —
  [#257](https://github.com/dvoyni/cog/issues/257). A pure optimisation with a
  measured number and nothing left to decide.
- **Narrowing query iteration with per-Store presence bitsets** —
  [#258](https://github.com/dvoyni/cog/issues/258). A selectivity trade: 9.2×
  better on the bad case, 45% worse when everything matches.
