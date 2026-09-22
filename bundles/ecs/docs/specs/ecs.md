# cog ecs — specification

`github.com/dvoyni/cog/bundles/ecs` is a plugin that lets an app describe things in the
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

**This specification is implemented.** `github.com/dvoyni/cog/bundles/ecs` is the
package it describes, and where the two disagree the code is the defect unless
this document says otherwise. [Required work](#required-work) is the checklist
it was built from and now records what is still open. The zero-allocation
prototype that produced the numbers below lives on the branch
`proto/ecs-zero-alloc`, which is still on `origin` (2e21edd, 2026-09-22) and
was never merged; the benchmarks that replaced it are in the package, and the
corrected figures below cite them. The sections Hooks changed are built as well: [Required
work](#required-work) records them under *Since Hooks*, and the package's README
carries their measured cost under [*What a Hook
costs*](../README.md#what-a-hook-costs).

**Two things here are younger than the rest, and each is marked where it
appears.** The first: the Component rule was relaxed after the package shipped.
A Component holds no *mutable* indirection rather than no pointers at all, which
admits `string` and [`m.List[T]`](#the-list). The sections that changed say what
they used to say and why the old reason did not survive, because the old rule
was argued for in this document at some length and a reader who remembers that
argument is owed the correction rather than a silent overwrite.

The second: **Hooks**, specified in [`hooks.md`](hooks.md) and assembled from
[ecs: Hooks, and how a System learns what happened to an
Entity](https://github.com/dvoyni/cog/issues/377). This document states in full
what Hooks change for code that names none: a `List`'s `Set`, what Validation
mode checks, the signature contract, and [giving memory
back](#giving-memory-back). A cost a reader puts on other Systems gets one line
and a link. A reader's own contract is only pointed to. Three claims this
document argued for are now false, and the sections that made them say what
they used to say: that nothing ever shrinks, that structural hooks were ruled
out, and that change detection would arrive as Query fields.

---

## Contents

- [Vocabulary](#vocabulary) · [What the numbers are, and what they are not](#what-the-numbers-are-and-what-they-are-not)
- [Entity](#entity) · [Component](#component) · [The List](#the-list) · [Validation mode](#validation-mode) · [Naming an engine-side thing](#naming-an-engine-side-thing)
- [Registration and ownership](#registration-and-ownership)
- [The Store](#the-store) · [Giving memory back](#giving-memory-back) · [The Query](#the-query) · [The Driver](#the-driver)
- [The System](#the-system) · [The lock set](#the-lock-set)
- [Structural change](#structural-change) · [Reaching another Entity](#reaching-another-entity) · [Reading the world by name](#reading-the-world-by-name)
- [Binding: how another plugin attaches](#binding-how-another-plugin-attaches)
- [More than one world](#more-than-one-world)
- [The zero-allocation claim](#the-zero-allocation-claim)
- [What is not foreclosed](#what-is-not-foreclosed)
- [Shapes that were rejected](#shapes-that-were-rejected)
- [Required work](#required-work) · [Out of scope](#out-of-scope)
- Hooks, in their own document: [`hooks.md`](hooks.md)

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
  its Go type, containing no *mutable* indirection transitively.
- **List** — a fixed-length run of values a Component may hold. It is what a
  slice is not allowed to be: its backing array is unexported and its one
  mutator is checked.
- **Tag** — a Component with no fields, whose presence is the whole of what it
  says.
- **Component set** — the exact set of Component types one Entity has. It
  describes an Entity; nothing groups Entities by it and no structure holds it.
  A Spawn names the one a new Entity starts with as a struct type. It is not a
  Bundle, which is a Slot shipped with its Extension — the plugin kind ecs
  itself is ([#340](https://github.com/dvoyni/cog/issues/340)).
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
- **Spawn** — creating an Entity with a complete Component set and its values in
  one structural change, that set named as a struct type. **Despawn** is its
  inverse and is total.
- **Reference** — an Entity kept inside a Component. It points one way.
- **Accessor** — a System's means of reaching one Component of an Entity it did
  not iterate to.
- **Hook** — one record of one act on one Component of an Entity, read by a
  System later rather than run at the act. See [`hooks.md`](hooks.md).
- **Hash**, **Name table** — retired. Naming an engine-side thing is the
  business of the two plugins that share the name, not of the ECS.
- **Validation mode** — a build tag that checks that nobody writes a List except
  through the Component holding it under a write lock, and that a System reading
  Hooks keeps pace with the Systems writing its Component.

---

## What the numbers are, and what they are not

Every measurement in this document was taken on **AMD Ryzen 9 7950X3D, Go
1.27.1, windows/amd64, `NumCPU=32`**, medians of five runs unless stated. The
design was measured on four research suites and the implementation on its own
benchmarks, and they are not equally strong evidence. Only one research suite is
on `main`; the other three live only on the branches named, all still on
`origin` at 2026-09-22:

| suite | where it is | what it is |
| --- | --- | --- |
| `docs/research/ecs-go-mechanics-bench/` | **on `main`** | Go language mechanics in isolation |
| `docs/research/ecs-query-shape-bench/` | branch `worktree-ecs-vocabulary` only | query-fill shapes against a hand-written loop |
| `docs/research/ecs-sparse-probe-bench/` | branch `worktree-ecs-vocabulary` only | a model of the Store: probe, driver, spawn, dispatch, scheduler |
| `docs/research/ecs-zero-alloc-proto/` | branch `proto/ecs-zero-alloc` only | the decided design on a real `kernel.Engine`, as a prototype |
| `bundles/ecs/internal/types/*bench_test.go` — accessor, binding, driver, frame, hooks, hooksmark, listheader, spawn, split, width | **on `main`** | **the implementation on a real `kernel.Engine`** |
| `bundles/ecsscene/internal/recordbench_test.go` | **on `main`** | the ecsscene binding's frame, drawn into a backend through gfx since [#537](https://github.com/dvoyni/cog/issues/537) |

The last three rows run on a real engine, driven by a real `app.UpdateEvent`,
and that distinction matters more than once below: three allocation constraints
appeared **only** in situ and are invisible in every microbenchmark
([The zero-allocation proof](https://github.com/dvoyni/cog/issues/243)). The
implementation's benchmarks are the strongest evidence in this document, and
every figure corrected against the implementation cites the ticket that
measured it with them. The allocation tests beside them in the same package
(`TestTheFrameSitsOnTheEnginesAllocationLine` and its siblings) log every
steady-state count this document quotes, and a count read from them names the
commit it was read at.

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
comparison is `==`. It implements `fmt.Stringer`. The top generation bit is the
**free bit**, so a live generation carries thirty-one: ~2×10⁹ reuses of one slot
before a stale handle could alias — at 30 Hz, never.

A game holds Entities across frames constantly: a creature's target, a spell's
owner, a projectile's caster. **That is safe**, and it is safe by construction
rather than by discipline — see [Reaching another
Entity](#reaching-another-entity).

Indices are **recycled** through a free list. That is what bounds the flat
sparse index by *peak concurrent* Entities rather than by Entities ever created,
and it is what makes [the Store's flat index](#the-store) affordable.

**The free bit is what keeps `Alive` exact in one compare.** A despawn steps the
index's generation and sets the free bit on it; allocating the index clears the
bit and hands out the generation that was already stored. A free index therefore
holds a word no issued handle ever equals, and `Alive` stays one compare against
one word — no free list, no bitmap, no second liveness structure consulted.

Without the bit, the handle a free index *will* carry answered `Alive` true
before anything held it. Nothing in Go can make that handle, but [reading the
world by name](#reading-the-world-by-name) parses one from a string, and the
[Reserved Entity](deferred.md#alive-is-false-for-a-reservation-until-the-drain)
a deferred Spawn hands back is exactly it and must be dead until its drain
([#559](https://github.com/dvoyni/cog/issues/559)).

The bit costs half the generation space and nothing else. `absentGeneration`,
the all-ones generation a Store writes into an empty sparse slot, falls in the
free half, so a live generation can no longer reach it at all and the generation
step no longer names it.

---

## Component

**A Component type contains no mutable indirection, transitively.** Every
pointer it holds, it holds to memory nothing can write. That is
**mechanically checkable**: a `reflect.Type` walk at registration, where cost is
irrelevant. It permits numerics, bools, fixed-size arrays, `Entity`, structs of
those, **`string`**, **`assets.Blob`** and **`m.List[T]`**. It refuses pointers,
bare slices, maps, channels, funcs, interfaces and `sync` types.

The check reports the offending field **by path**, because the field that fails
is usually several structs down and naming only the Component is useless:

```
plugin "pointy" panicked in Register: ecs: proto.PathedDrawable.Handle is a
ptr, which is mutable indirection
```

**This replaced "contains no pointers, transitively", and the history is worth
keeping because the reason the old rule carried was not the reason it gave.**
The old rule was stated with three justifications — copying, serialisation and
the collector — and a `string` survives all three. A string copy stays
meaningful after the thing it was copied from is gone; a string is trivially
serialisable; and the collector was always the thin one, measured below. What
forbade strings was never those. It was the lock unit, and the old rule was an
over-approximation of it that happened to be easy to state.

**The lock unit is what the rule is actually protecting.** A read yields a copy,
and that is the whole of why `read{C}` is sound for concurrent readers — but
only where the copy is not itself a write handle. This is where the kinds part
company, and the split is sharp rather than a matter of degree:

| a read yields… | can the reader write the Store through it? |
| --- | --- |
| a `float32`, an `Entity`, a `[32]byte` | no — it is a copy of the bytes |
| a **`string`** | **no** — the header is a copy and the bytes are immutable |
| an **`assets.Blob`** | **no, by contract** — the bytes are never written after construction, and nothing checks that |
| a `[]T` | **yes** — the header is a copy and the array is shared |
| an **`m.List[T]`** | only through `Set`, which validation mode checks |

A System holding nothing but `read{C}` writing the Store through a shared
backing array is a data race **no lock anywhere names**, and it would make the
claim this document opens with — that under-declaration is unrepresentable —
false. So `string` is admitted outright, for a property rather than as an
exception, static bytes are admitted as an [`assets.Blob`](#assetsblob-static-bytes-on-trust)
on a contract rather than a property, and a variable-length run is admitted
only as a [List](#the-list).

**Copying.** A Component is copied into and out of a Store by value and must
stay meaningful after the thing it was copied from is gone. Every refused kind
names memory the Store does not own and cannot keep alive.

**Serialisation.** A Component is trivially serialisable, which is the cheap
constraint that keeps replication from being foreclosed, and admitting `string`
does not spend it: a string is one of the easiest things there is to encode.
What it costs [data-driven
spawning](https://github.com/dvoyni/cog/issues/266) is narrower and is recorded
there rather than waved at — **a row stops being a fixed width**, so an encoder
that would have been `memcpy(row, size)` needs a per-type walk for the
Components that hold one.

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
magnitude below that, is **not a reason to forbid strings**, and this document
previously drew the opposite conclusion from the same number. Scaled to nox's
low thousands it is on the order of ten microseconds per collection. **Nor is
the lookup the reason it once was**: a name as a map key costs **7.79 ns**
against **6.66 ns** for the hash this package used to supply, and a dense index
**0.64 ns** against either. See [Naming an engine-side thing is not the ECS's
business](#naming-an-engine-side-thing-is-not-the-ecss-business), which is a
section about what this package no longer does.

**Two costs are real and land per-Store rather than globally**, which is what
makes admitting them affordable at all. A Component holding a string or a List
leaves the noscan span, so its Store is scanned; every Component that was legal
before this rule changed keeps the span, the memcpy fill and the iteration speed
it was measured with, bit for bit. And width: 48 B → 64 B for one string field
costs **12% more per iteration** (55.3 µs against 49.4 µs over 100k rows), which
is a reason to prefer a `[32]byte` where the bound is real, not a reason to
refuse.

### The rule costs three mechanisms, and they are not optional

Admitting a pointer into a row changes three places, each of which was written
against the old rule and says so in a comment. They are correctness, not tuning.

1. **The fill takes a typed copy.** `fill`'s sized moves write a row's bytes
   through an `unsafe.Pointer` and emit no write barrier. For a pointer-free row
   that is sound and is the measured path; for a row holding a string it is a
   pointer store the collector never sees. A non-trivial Component plans a typed
   closure instead — `*(*C)(dst) = *(*C)(src)`, baked where `C` was still a
   type, so the compiler emits whatever barriers the row needs.
2. **A vacated row is zeroed.** `remove` deliberately left the value in the
   vacated slot, because "a Component holds no pointer for it to keep alive".
   With one, the slot keeps a despawned Entity's data reachable until something
   else happens to take the row. Both arche (#147) and ark (#324) shipped this
   bug and fixed it the same way.
3. **A Query naming one takes the per-field loop.** This is the only one that is
   a trade rather than a repair, and it was measured: `fill` inlines at **cost
   67 against a budget of 80**, and an indirect call costs **68**, so a `fill`
   that could take a closure does not inline — which this document already
   prices at about **8% of a frame**, paid by every Query in the engine. Routing
   instead leaves every existing Query's machine code exactly as measured and
   puts the cost on the Queries that use the new types, at about **2.4×** a
   hand-written walk instead of **1.4×**. Interleaved A/B over pre-built
   binaries confirms the trivial path is unmoved: `DriverBadCase` −0.8%,
   `DriverTagRemedy` −1.7%, both inside the noise. Closing the gap for
   non-trivial Queries needs monomorphised fillers per Component type
   ([#257](https://github.com/dvoyni/cog/issues/257)), which is an optimisation
   and not a correctness question.

### The List

> **Amended by [#524](https://github.com/dvoyni/cog/issues/524).** The List is
> `m.List`, declared in `libs/m` beside `m.Maybe`, with `m.NewList` and
> `m.ListOf`; the root no longer declares a List or its constructors, and no
> alias is kept. What is ECS knowledge stays in `bundles/ecs/internal/types`: the
> registration walk recognises a List by the type of its first field, m's
> unexported marker; the generation `Set` bumps is still the header word a
> Changed Hook's byte compare sees; and under `-tags ecs_validate` `m` calls a
> check the ECS installs, so the release build still carries none of it.

**A `[]T` in a Component is refused and always will be.** A List is what a
Component holds instead: a fixed-length run of `T` whose backing array is
unexported, whose constructors copy into a fresh one, and whose only element
write is `Set`.

```go
type Inventory struct {
    Slots m.List[ItemID]
}

func use(q *ecs.Query[InvQ]) {
    for _, it := range q.All() {
        it.Inv.Slots.Set(0, empty)   // legal: Inv is a *Inventory field
    }
}
```

`Len`, `At` and `All` yield copies. **There is no `Slice`**, and its absence is
the type: handing back the backing array would give away exactly the writable
alias the type withholds.

**Its length is fixed at construction, and there is no `Append`.** Growing means
a new backing array, which is an allocation, and an allocation on the hot path
is what requirement 1 exists to refuse. A List whose length changes is a new
List written into the Component under an ordinary write lock. Where the length
changes every frame the answer is unchanged — a fixed-capacity array with a live
count, a child Entity, or a side store keyed by Entity.

**`Set` has a pointer receiver, `func (l *List[T]) Set(i int, v T)`, and adds one
to a generation kept in the List's header.** So an in-place change to the
elements shows in the bytes of the Component holding the List. That is what
lets a Changed Hook detect a change by comparing bytes
([What counts as a value change, for a Changed
Hook](https://github.com/dvoyni/cog/issues/268)). The call sites above are
unchanged, because a List reached through a `*T` field or `Ref` is addressable.

**The header grows from 24 to 32 bytes, and every List user pays it**, watched
or not. Measured on the build ([#388](https://github.com/dvoyni/cog/issues/388))
by `listheaderbench_test.go`, as medians of seven interleaved A/B rounds against
the 24-byte header; first measured on [`proto/ecs-hooks`](https://github.com/dvoyni/cog/tree/proto/ecs-hooks)
([`5f41e42`](https://github.com/dvoyni/cog/commit/5f41e42)) at 73, 28, 7.7 and 15.9:

| over a Component holding one `List` | 24 B header | 32 B header |
| --- | --- | --- |
| Query read, 10k Entities | 73.1 µs | 72.9 µs |
| Query write, 10k Entities | 28.3 µs | 28.6 µs |
| Store remove plus add, as the row crosses 32 → 40 B | 7.8 ns | **16.2 ns** |

**Where `Set` is legal:**

| `Set` called on | legal |
| --- | --- |
| a fresh List, not yet in any Store, before `UpdateFor` or a Spawn | yes |
| the stored List, reached through a `*T` Query field or `Set[T].Ref` | yes |
| a `Set[T].Of` copy, even if `UpdateFor` follows | **no** — use `Ref` |
| a Query read copy, a `Get[T].Of` copy, or a Hook's `Value` | **no** |

**A List that is `Set` belongs to exactly one Component.** If two Components
hold the same backing array, a `Set` under `write{A}` changes memory that
`B`'s readers read under `read{B}`, which is a race no lock names. A List held by
several Components is never `Set`.

**`List[T]` where `T` itself contains a List is admitted**, and validation
reaches it. This was refused at first, because validation stamped List backing
arrays at fixed offsets within a row and an array a List's *elements* name sits
at no fixed offset — allowing it would have shipped a check with a silent hole
in it. The hole is closed instead: registration records, for each List, its
element stride and the Lists within one element, and every stamp walks each
outer List's elements and stamps every nested backing array with the same mode
and owner as the List holding it. A `Set` on a nested List reached through a read
handle therefore panics exactly as a flat one does. Release builds are unchanged:
the walk lives in the validating file and nowhere else.

The trade is stated because it is a real one. **A nested row stamps n+1 arrays
where a flat one stamps one**, so it fills the bounded stamp table n+1 times as
fast, and the table's oldest-first eviction forgets sooner — a write through a
nested List stamped long ago is likelier to go undiagnosed than the same write
through a flat one.

**There is no `List.Raw()`.** A read-only view of the backing array would be a
`[]T` nothing can check, and the only argument for one is copy-out cost, which
was measured rather than assumed: copying four 328 B elements out through `All()`
into reused scratch costs **60 ns and 0 allocations** (reproduced at 51 ns), because
the iterator inlines. A view is added if a benchmark asks for it, not before.

**Deep-copying on the way into the Store was considered and does not work.** It
fixes ownership and not access: after a copy-in, the header a *read* yields
still points at Store-owned memory, so the read-lock holder can still write
through it. The hazard is on the read-out side, and closing it there means a
deep copy per entity per frame — an allocation on the hot path. Copying at the
boundary cannot manufacture immutability.

### `assets.Blob`: static bytes, on trust

**`assets.Blob` is admitted by type identity, and it is the one kind admitted on
a contract rather than a property.** A Blob is a pointer and a length whose
documented contract is that the bytes a Component holds are never written after
construction. A Blob that honours it is exactly as safe to hand a reader as a
string is. Nothing enforces it: `Data()` hands out a live slice and a write
through it is an ordinary slice write with no method in front of it, so
**validation mode cannot detect one** — the stamp table watches `List.Set`, and
there is no `Blob.Set` to watch. What the shape does buy is that both of its
fields are unexported: a holder can read the run and cannot repoint it.

It is admitted anyway because the bytes engine types carry — a texture's pixels,
a buffer's contents, a material parameter's raw layout — are static in practice,
and the alternatives each cost something nothing downstream uses. A `List[byte]`
would copy megabytes on construction and could not adopt an arena. A `string`
would copy on the way in and again on the way out to every API that takes bytes,
where a `Blob` converts to and from `[]byte` for free — still free, but no
longer implicit: `assets.NewBlob` wraps a `[]byte` without copying it and
`Data()` unwraps one, and every holder names the conversion where it builds one.
So `gfx`'s descriptors hold Blobs and are Components as they stand. Recognition
is by identity, so a caller's own named `[]byte` type is still a slice and is
still refused; `ecs` imports `assets` for the identity, which the import table
allows and which makes no cycle.

**`PointerFree` still refuses a Blob**, so a Store holding one is scanned, takes
the typed copy, and zeroes a vacated row — the same three mechanisms a string or
a List costs.

### Validation mode

**`List.Set` is checked under `-tags ecs_validate` and nowhere else, along with
the Hook checks [`hooks.md`](hooks.md) lists.** The build tag selects between a file where `const validate = true` and one where it is
`false`, so a release build contains no branch, no table and no load for any of
it. This is ark's own pattern (`//go:build ark_debug`) and Unity DOTS's
(`ENABLE_UNITY_COLLECTIONS_CHECKS`, on in the editor, compiled out of player
builds).

The check stamps each List backing array with the last handle it was reached
through, and `Set` consults the stamp. Four cases, all tested flat, and the
read, write and stored cases tested again one List further in:

| what the code does | validating build |
| --- | --- |
| `Set` through a `*C` Query field | allowed |
| `Set` through a `C` read field, or a `Get[C]` | **panics**, naming the Component and the mode |
| `Set` through a value retained past the `All()` that yielded it | **panics**, naming the run |
| `Set` through the caller's own copy, after the value entered a Store | **panics** — `ListOf` copies, but `Set` shares |
| `Set` through a `Set[C].Of` copy | **panics**, naming `Ref` |
| `Set` on an array a second Component holds while its first owner still holds it | **panics**, naming both |
| `Set` through a Hooks value, at any time | **panics**, naming `Hooks[T, K]` — see [`hooks.md`](hooks.md#a-value-that-holds-a-list) |
| any of the above, on a List inside another List's elements | the same as the flat case — the stamp walks nested Lists |
| registering a `Hooks[C, HookAddedChanged]` or `Hooks[C, HookAll]` reader of a Component with implicit padding, between fields, at the tail, or inside a nested struct or array | **panics** at registration, naming the Component, the field the gap follows or "at the end", the byte count, and the `_ [N]byte` field that fixes it — see [`hooks.md`](hooks.md#changed-is-a-difference-in-bytes) |

**`Set[C].Of` no longer stamps a write, and that is a change in behaviour.** It
used to stamp `modeWrite`, so a `Set` through its copy was allowed. The copy
shares the stored array, but `Of` hands out a value rather than the row, so the
write never reaches the Component's bytes and a Changed Hook cannot see it. `Ref`
is the route to a List a System means to write.

**The second owner.** When a Changed compare at a writer's run end finds a row
holding a List, it registers that List's array to that Component and Entity. An
array arriving in a second Component while its first owner still holds it is
marked shared, and `Set` on a shared array panics naming both. Registering at
the compare covers an array that arrives through `*p =`, which never passes
`UpdateFor` or a Spawn. **The first owner stops holding the array at the act that
removes its row**, `Remove[T].From` or a Despawn, and a Hook log that retains the
removed value is never an owner
([Validation mode and a retained value holding a
List](https://github.com/dvoyni/cog/issues/386)).

**This is detection and not prevention, and that is a weaker guarantee than
anything else in this document.** `string` is sound by construction; a List is
sound if a run exercises the bug. It is worth saying why it is still worth
having: an illegal write is a lock-model violation whether or not two goroutines
interleave, so a single-threaded run catches it on the first frame it happens —
which the race detector, [which cannot build in this
environment](#what-the-numbers-are-and-what-they-are-not), would not.

What it does not catch is stated in `validate_on.go` rather than left to be
discovered: a write through `unsafe`; a write by a callee the value was passed
to, which is reported against whoever called `Set`; a List whose array was
evicted from the bounded table, which a nested List fills faster; a write
through an `assets.Blob`, which has no method to check; and anything a run never
executes. Two more since Hooks:

- **A second owner on a Store no Changed Hook reads.** The shared-array mark is
  registered at the Changed compare, which runs only on a Store a Changed reader
  watches. A List copied with `Get`, added to a second Entity with `UpdateFor`
  and `Set` there is a race on any Store, and it is caught only on a watched one.
  Widening the mark to every `Ref` and `*T` bind on every Store in validating
  builds was considered and not taken: it is new design, and the hole is stated
  instead.
- **A kept List read outside a run holding `read{T}`.** A List kept from a Query
  read field or from a Hook still shares its array with the Store or the log.
  Reading it in a later run that holds `read{T}` is sound; reading it anywhere
  else is a race no lock names, and no stamp sees a read.

Its cost is a
map write per List field per stamped row per run under one mutex, so a
validating build serialises where a release build runs concurrently. That cost
is confined to a build nobody ships, which is the whole reason it is a tag.

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
   explicit type arguments. Cost is linear in width across the unrolled fillers,
   widths 1 to 4, at about 1–2 ns a field an Entity; it steps up at width 5,
   where the per-field loop takes over, by about 7.5 ns an Entity. Through
   `All()` widths 1 to 3 take a walk written out inside its literal and run
   0.6–0.9 ns an Entity under the delegated path width 4 still takes. See
   [Time, and one number worth another look](#time-and-one-number-worth-another-look).
2. **Sparse-index memory per Component type.** 8 bytes × peak concurrent index
   space, per type — 32 KB per type at nox's low thousands, **2.8 MB across 85
   types**.
3. **Flip cost.** Adding or removing a Tag is a structural change, not a field
   write.

### Variable-length data has four answers

In preference order. The first two are unchanged since
[#237](https://github.com/dvoyni/cog/issues/237) and sharpened by
[#246](https://github.com/dvoyni/cog/issues/246); the List is third because it
is the one that allocates.

1. **A child Entity** with an owning Reference, for structured data (an
   inventory, a spell list). This is how nox already works: a carried sword and
   a dropped sword are the same object.
2. **A fixed-capacity array in the Component**, where the bound is small and
   real. Now measured rather than assumed: a `[32]byte` inline name costs the
   collector **−0.015 ms** against an empty heap at 100k and **+0.051 ms** at
   1M, against **+0.805 ms** for the same field as a `string`. The costs are
   truncation and width, not the collector.
3. **An `m.List[T]` in the Component**, where the bound is not real but the
   contents are set at spawn and rarely rewritten. It costs one allocation per
   construction, takes its Store out of the noscan span, and routes every Query
   naming it to the per-field loop. See [The List](#the-list).
4. **A side resource keyed by Entity** — and this is **deferred out of v1**, to
   [per-entity data a Component cannot
   hold](https://github.com/dvoyni/cog/issues/264). It remains the right answer
   for per-entity data that churns every frame, which a List is the wrong shape
   for.

What is **not** on the list is a content-addressed table, and the reason is the
cleanup question: such a table must hold the buffer itself, so it grows with
every distinct value the game ever produces, and the only way to empty it is to
count how many Entities still refer to each entry — refcounting on a Component
write, which stops the Store being a `memcpy` and stops Despawn being a
type-erased removal. Keyed by Entity there is exactly one owner and nothing to
count. **Sharing is what costs; ownership is free.**

---

## Naming an engine-side thing is not the ECS's business

**The ECS supplies no naming scheme, and this section records the one it used to
supply.** A Component may hold a `string`, so how a Component says which model,
clip or node it means is a matter between the plugin that writes the name and
the plugin that resolves it. The ECS has no opinion, no type and no table.

**What was here.** `HashOf[K HashKey](string) K`, a 64-bit FNV-1a of a name
typed as the caller's own `~uint64`; `NoHash`; and `Names[K, V]`, the reverse
table a resolving plugin kept. It existed for one reason — **a Component could
not hold a string**, so a name had to become a number before it could be stored.
That reason is gone, and the machinery went with it.

**The cost argument that survived the first rule change did not survive
measurement.** This document justified the hash against **46.9 ns** for "a path,
as `scene` resolves one", which is `scene` re-resolving through its own
machinery and not the price of a name as a map key. Measured against the thing
it actually replaced, on the implementation rather than the prototype:

| per lookup, over 1 000 lookups | ns |
| --- | --- |
| a dense index into a slice | **0.64** |
| **`Names.Lookup`, the stored hash** | **6.66** |
| a `map[string]V` keyed by the path | **7.79** |
| hashing the string on every lookup | 25.19 |

**1.1 ns**, which at 5 000 drawables is about **5.5 µs a frame**. A whole
vocabulary — `Hash`, `Name table`, two exported types, a constant and a generic
— for 1.1 ns, in a package whose stated job is entities, components and systems.

**Two of its properties were real and a consumer that wants them should keep
them, in its own package.** A hash is the same in every process and every run,
which an assigned index is not, so it is the form that survives a save file or a
wire; and producing one needs nothing, so a System renames what an Entity points
at holding only the lock it already had. Neither property requires the *ECS* to
own it. `TestANameIsTheSameEverywhereAndAnIndexIsNot`, which interned two paths
in opposite orders into two tables and got different ids, was the evidence, and
it is worth rebuilding wherever the scheme lands.

**A dense index is still 10× cheaper than either and is still the right thing to
resolve *to*.** That was true when the hash was here and it is the advice that
outlives it: name in the Component, index inside whatever consumes it.

### What a bound plugin owes, and the scene vocabulary

A plugin a System records into must offer Components that are storable.
Machine-checked against the real `scene` package by running the walk over its
recording vocabulary:

| type | verdict |
| --- | --- |
| **`m.Transform`** | **legal** — plain values, and the one placement type since [#468](https://github.com/dvoyni/cog/issues/468) |
| `scene.ModelDraw`, `scene.MeshDraw`, `scene.CameraDescr` | rejected — `Transforms`, `Plays`, `Params`, `Passes` are bare slices |
| `scene.Material`, `gfx.MaterialDescr` | rejected — a slice, and a descriptor holding one |
| `model.ClipPlay` | **legal** — `.Clip` is a `string` |
| `model.ModelRef` | **legal** — `.Path` is a `string` |
| `scene.Pass`, `gfx.ParameterDescr` | **legal** — clears are `m.Maybe`, bytes are `assets.Blob` |
| **`model.MeshRef`** | **legal Component** — already a dense id and a generation |
| **`scene.LayerMask`, `scene.CameraID`** | **legal Component** |

The rejections that remain are slices, which are mutable indirection and the
line the rule draws: a binding spells those descriptors out as Component fields
holding `m.List`s and rebuilds them per draw. The rows that moved were refused
for holding a name, a matrix pointer or byte slices, and scene changed each of
those. **`ecsscene` was built on the removed
vocabulary** — `ModelHash`, `ClipHash`, `ecs.Names`, `ecs.NoHash` — and was
rebuilt as a thin binding whose Components held scene's own types, the path
included. Since [#538](https://github.com/dvoyni/cog/issues/538) its Components
hold `model`'s — `model.ModelRef`, `model.MeshRef`, `model.ClipPlay`,
`model.LightDescr` — beside its own copy of scene's camera, layer and pass
types, and the verdicts above hold of those the same way.

---

## Registration and ownership

A Component type is registered **explicitly, once, by exactly one plugin**:

```go
func RegisterComponent[C any](r *kernel.Registrar, ids uint32) *Store[C]
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

**`m.Transform` is the one exception, and it is vacuous on purpose.** Where an
Entity stands is read by every binding, so the type is declared in `libs/m` and
its Store is registered by the ecs plugin itself, not by a plugin that defines
it: one Store, so two Components can never describe one position without the
scheduler relating them. The cost is the vacuity above, confined to that one
Store: every plugin with Systems already depends on `ecs`, so a System writing
`m.Transform` without declaring anything else is never caught at composition.
Order its writers deliberately. It is not licence to register a binding's own
Components in `ecs`; every other Component follows the rule.

One wart, recorded before it is discovered in a log: an instantiated generic
renders its type argument with the full import path, so the error a developer
reads is `*ecs.Store[github.com/dvoyni/nox/game.Health]`. Verbose, unambiguous,
and not worth a kernel change to prettify.

### The world arrives through a declared dependency

`*Entities` is a kernel resource — it has to be, since it is what the locks are
taken on — but that is only the run-time half. Both `RegisterComponent[C]` and
`ToHandler` need it at **registration**, before any handler runs.

The first answer was to thread it through every plugin constructor from a value
built at the composition root (`world := ecs.NewEntities(maxIDs)`, then
`ecs.Plugin(world), game.Plugin(world)`), because registration could read no
resource value. That coupled every ECS-using plugin's constructor to the world
and made the app build a piece of the engine before the engine.

**Settled since:** the kernel gained `Registrar.Dependency[T]`, which returns
the value of a resource owned by a declared dependency, and the handler builders
take the registrar so they can call it. That is sound because dependencies
register first and nothing runs concurrently with registration, and it
returns `ErrUnavailableDependency` for a resource with no value yet or an
undeclared owner. The ecs builders re-panic that error: a registration-time
panic, which the plugin boundary reports as `kernel.ErrPluginPanic` naming the
plugin, with `ErrUnavailableDependency` as the recovered value, and composition
fails. The ecs plugin now creates the authority from its config itself, its
constructor is unreachable from outside the plugin, and composition takes no
world at all:

```go
config[ecs.Name] = ecs.Config{PrewarmEntities: prewarmEntities}
kernel.New(config).WithPlugins(ecsplugin.New(), physics.New(), game.New())
```

> **Amended by [#340](https://github.com/dvoyni/cog/issues/340).** ecs is a
> Bundle: the contract root `bundles/ecs` holds everything a System author uses
> and declares no plugin, `bundles/ecs/ecsimpl` holds `New` and `Config`, and
> `bundles/ecs/internal` declares `Entity` and `Entities`, which the root aliases.
> `ecs.Plugin()`, `ecs.Config` and `ecs.DefaultConfig().WithPrewarmEntities(n)`
> became `ecsimpl.New()` and `ecsimpl.Config{PrewarmEntities: n}`, whose zero
> field takes the default of 1024. The constructor of the authority lives in
> `internal`, which only the root and `ecsimpl` can import, and that is what keeps
> it one per Engine. The kernel's architecture output named the resource
> `*internal.Entities`, the package it is declared in, until
> [#349](https://github.com/dvoyni/cog/issues/349) rendered it `*ecs.Entities`.

> **Amended by [#358](https://github.com/dvoyni/cog/issues/358).** ecs moved to
> the declaration-root shape of
> [ADR 0002](../../../../docs/adr/0002-slots-extensions-and-bundles-as-declaration-roots.md).
> The root, `bundles/ecs`, holds declarations only: every type above is declared
> in `bundles/ecs/internal/types` and aliased in the root, `Config` is declared
> in the root, and `RegisterComponent`, `NewStore`, `Storable`, `PointerFree`,
> `ToHandler`, `ToExecute`, `Feed`, `NewList` and `ListOf` are forwarders in its
> `utils.go`, so every name here is still spelled `ecs.X`. The plugin moved from
> `ecsimpl` to `bundles/ecs/internal`; it is constructed with `ecsplugin.New()`
> and configured with `ecs.Config{PrewarmEntities: n}`. The authority's
> constructor is in `internal/types`, which nothing outside `bundles/ecs` can
> import. The files this document cites, such as `validate_on.go`, are in
> `internal/types`, and every ecs diagnostic names a type through
> `kernel.TypeName`, so a Store still reads `*ecs.Store[…]`.

The cost is one requirement a plugin already met: a plugin registering a
Component or a System declares `ecs`.
[#245](https://github.com/dvoyni/cog/issues/245) retired `World` from the API
and `CONTEXT.md` lists it under `_Avoid_`, so no exported identifier in `ecs`
contains the word `World`.
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
three-allocation column outright. Compaction stays out. Capacity goes back only
when the app asks, through [`ecs.ShrinkCmd`](#giving-memory-back).

---

## Giving memory back

*Since Hooks.* **Nothing in the ECS gives memory back on its own,
and the app gives it back with one Command**
([A Hooks reader that falls behind: what bounds its
log](https://github.com/dvoyni/cog/issues/381)). A Store keeps its high-water
capacity, and so do the free list, a System's scratch and a Hook log, so steady
state never allocates. After a spike, such as a level load or a screen of
effects, that capacity stays until the app releases it:

```go
type ShrinkCmd kernel.Command[ShrinkRequest, ShrinkResponse]

type ShrinkRequest struct {
    KeepHooks    bool // every Store's Hook log, and each reader's copy
    KeepStores   bool // each Store's rows cut to its length, its sparse array to its highest index held
    KeepEntities bool // the free list; unused indices at the top dropped behind a generation floor
    KeepScratch  bool // per-System buffers: a Query's walk, a writer's row copies for Changed
}

type ShrinkResponse struct{ Hooks, Stores, Entities, Scratch uintptr } // bytes released
```

- **The zero request shrinks everything**, and each `Keep` option opts one area
  out. The app executes it like any kernel Command.
- **Shrinking means capacity equals length exactly**, with no slack left.
- **The response reports bytes released per area.** Go frees the old arrays at
  its next collection. Handing memory back to the OS stays the runtime's job, and
  `debug.FreeOSMemory` is the caller's to call.
- **The frames after it regrow capacity, and those frames allocate.** Shrink when
  a spike has ended, not every frame.

**Its lock is `write{*Entities}` alone, and that is enough.** Every handler that
touches any Store declares `read{*Entities}`, and a Despawn already empties every
Store under that one lock (see [the load-bearing
invariant](#the-load-bearing-invariant-the-kernel-cannot-enforce)). So the
Command excludes every ECS System while it runs, needs no Store lock and no
registration order, and costs nothing in a frame that doesn't execute it. **The
ecs plugin registers it itself.**

**The generation floor.** `KeepEntities: false` drops free indices at the top of
the index space. Dropping an index forgets its generation, so the authority keeps
one generation floor, a `uint32`. An index allocated again after being dropped
starts above any generation ever issued, so a stale handle can never match it.
Staleness stays decided, never estimated.

**Why there is no heuristic.** General containers never shrink on their own: Go
slices and maps, Rust `Vec`, Bevy's message buffers, and flecs outside its
manual `ecs_shrink`. Allocators shrink against a decaying peak. For a spike
every *k* frames, "shrink under a quarter of capacity" shrinks between every pair
of spikes and grows back. The engine cannot know when a load has ended, and the
app does.

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

**The walk lives inside the closure `All()` returns, not in a method it calls.**
A filler is far past what the inliner takes from a method — `iterate1` is 186
cost units and `iterate2` is 331, against a budget of 80, with `fill` alone at 67
and appearing twice in each — so a `yield` reached through one is an indirect
call an Entity, and the range statement's own state machine survives into the
emitted closure beside it. Together that is about **1.1 ns an Entity**, paid by
every System in the engine whether or not it knows it is on a hot path, and it
is not inherent to range-over-func: it is the budget. A func literal created and
called exactly once gets **800** instead of 80, so with the two-field body
written out inside the literal the whole chain collapses into the call site —
the literal, the walk, and then the range statement's yield closure inlined into
the walk in turn, leaving no per-Entity call at all. Measured **0.888 ns an
Entity** on `BenchmarkFrameQuery10k`, which halves what a Query costs over a
hand-written loop, **1.860 → 0.952**. The signature does not change and no call
site moves.

Recorded so the arrangement is not tidied back into a call:

- **Two filler bodies in one literal.** 791 cost units for some shapes and 817
  for others, and past 800 the literal does not merely lose the win: it compiles
  as a standalone body that also loses the `row` and `fill` inlining `iterate2`
  keeps, **+4.8 ns an Entity** — worse than never having tried. That is why
  widths 1 and 3 landed as literals of their own (below), never as second arms
  of this one.
- **The filler held as a `func` field** rather than reached through the shape
  switch. A regression: an indirect call is opaque to escape analysis, so the
  yield closure escapes and the frame pays **two allocations a tick**. This is
  the same finding `Query.shape`'s own comment already carries.
- **`All()` returning a method value.** Measures identical to today at zero
  allocations, so it buys nothing — and it becomes the escaping shape the moment
  `All` gains a second producer.
- **Shrinking a filler under 80.** Not reachable: `iterate1` is 186 and `fill`
  alone is 67.

The 800 is a compiler-internal constant rather than a language guarantee, and
**the allocation-line tests do not see a bust**, because the regression
allocates nothing — they pass unchanged at 200 B/op and 4 allocs/op on a busted
build. `queryinline_test.go` is the net instead: it reads the `-gcflags=-m=2`
verdicts and fails both when the literal stops inlining and when a range body
stops collapsing into the walk.

**Widths 1 and 3 have walks of their own as well**
([#546](https://github.com/dvoyni/cog/issues/546)). Each is a literal of its
own, called once from the one `All()` returns, so each is priced against its
own 800 — width 1 at 196 units, width 3 at 483 — and the returned literal pays
only a call for each: **576** in all, against 413 with width 2 alone. `All()`
itself stays at 17 (22 in the dictionary wrapper) and inlines at every range
site, so the range statement still calls a literal it can see and every walk
comes along with it. Width 2's walk stays in the returned literal's own body,
and where it sits is load-bearing: with three walks the range body is called
from three places, and the inliner gives the 800 only to the call it resolves
first. That is width 2's because it is one literal shallower than the others;
the others get 160, twice a method's budget. So a range body past 160 still
collapses at width 2, and at widths 1 and 3 the walk inlines but the body is an
indirect call an Entity, as it was through the filler.

Judged per width against a rule fixed before the run, on `BenchmarkQueryWidth`
at 10 000 Entities: the saving *S* (old `components-k` less new) had to beat an
A/A band with 8 of 10 rounds agreeing in sign and reach 0.3 ns an Entity, and be
no less than *Y*, the indirect-`yield` cost at that width inside the new binary
(`delegated-k − components-k`), less the band; width 2, `BenchmarkFrameQuery*`
and the hand-written control had to stay put. Ten rounds, three binaries (old,
a copy of old for the band, new) interleaved with the order rotated, medians,
ns an Entity, AMD Ryzen 9 7950X3D, go1.27.1, on a machine shared with other
builds. All three candidates were measured together first, so `All()`'s cost
and width 2's numbers were taken with the whole set present:

| width | *S* | band | sign | *Y* | *S* on minima | verdict |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | **1.19** | 0.071 | 10/10 | 0.53 | 1.17 | lands |
| 3 | **0.91** | 0.018 | 9/10 | 0.85 | 0.67 | lands |
| 4 | 0.72 | 0.011 | **7/10** | −0.10 | 0.69 | not landed |

Width 2 did not move: `components-2` +0.027 against a 0.029 band, and neither
`BenchmarkFrameQuery` size nor `BenchmarkFrameHandWritten10k` agreed in sign in
more than 5 rounds of 10. The reduced set, widths 1 and 3, was then measured
again in a session of its own before landing: *S* = **1.21** and **1.08**, both
10/10, width 2 and the control unchanged, and `BenchmarkFrameFiltered10k` — a
shape-3 Query — **0.61 ns an Entity cheaper** (0.51 at 1 000). Across a package
boundary, in ecsphysics2d's test binary, a width-1 walk saved 0.46, a width-3
one 0.80 and `BenchmarkIntegrateOverBodyComponents` 0.94 ns an Entity.

The production range sites that now take a literal: width 3, `integrate`
(`positionQuery`), whose body collapses; width 1, the Joint walks —
`index`'s `jointIndexQuery`, whose body collapses, and `gatherJoints`,
`findWakes` and `build` over `JointQuery` and `IslandJointQuery`, whose bodies
are past 160 and stay an indirect call. `emitterQuery` is width 1 but reads a
`ClipRef`, which holds a string, so it is shape 5 and keeps the per-field loop.
The price is code size: every range site carries every walk, whichever it runs,
and `integrate` grew from 736 to 1 984 bytes of text.

Recorded so nobody re-attempts these without new information:

- **Width 4 in a literal of its own.** It fits — 624 alone, and the returned
  literal 656 with all three — and saved 0.72 ns an Entity on the median
  against a band of 0.011, 0.69 on the minima. But only 7 of 10 rounds agreed in
  sign: in two of them every arm ran 40–80% slow and the new binary drew the
  worse moment, and one was slower without a visible cause. That fails the
  rule's first condition, so it was not landed. *Y* could not be read either:
  `delegated-4`, unchanged code, ran 0.54 faster in the new binary than in the
  old, a layout effect, which put *Y* below zero. What a re-attempt needs is a
  quieter machine, not a different literal. It would carry `Solve`'s velocity
  walk, the Body index rebuild and the awake-island walk.
- **Width 2 in a literal of its own beside the others.** Every walk fits —
  width 2 at 343, the returned literal at 372 — and `All()` still inlines, but
  width 2's call to the range body is then resolved in the same batch as the
  others', is no longer the only one, and gets 160. A width-2 body past 160 stops
  collapsing: ecsscene's `record-range3`, at 177, is one.
  `TestAWidthTwoBodyPastTheCallBudgetStillCollapses` pins it.
- **`All()` returning a different literal by shape.** `All()` still inlines, at
  39 units, but a func value with two producers is one the range site cannot
  resolve, so neither literal inlines there and the range body and its state
  escape: two allocations a run, width 2 included — the escaping shape the
  method-value note above predicted.

**Across a package boundary `row` and `fill` do not inline into any walk,** the
width-2 one included, and did not before #546 either: in ecsphysics2d's
`integrate` every walk calls them an Entity. Inside this package they inline
everywhere, which is what `queryinline_test.go` and the width sweep see. The
walks still save what the cross-package rows above show, but a System in
another package does not get a call-free loop. Why the compiler does not carry
those two bodies across is not established.

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

`ecs.With[T]` has shipped as a second filter type, and it may drive a Query
where a `Without` may not ([The Driver](#the-driver),
[#291](https://github.com/dvoyni/cog/issues/291)). `ecs.Or[…]` can still arrive
later as a further field type without the derivation changing shape — the
query-vocabulary growth axis requirement 3 named. An earlier revision listed
`With[T]` beside `Or[…]` as future; it arrived without changing the derivation,
which is the claim. The rejected alternative was a second type parameter
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
the pointee is independently reachable whether the barrier fires or not.

**The value fields are a separate argument, and it is the one the Component rule
carries.** A *pointer-free* row's bytes need no barrier at all, which is what
licenses the sized moves. A row holding a string or a List does, so it is not
filled by them: it takes the typed copy, and the Query naming it takes the
per-field loop, for the inlining reason measured in [the rule costs three
mechanisms](#the-rule-costs-three-mechanisms-and-they-are-not-optional). The
invariant to carry forward is therefore sharper than "Components are
pointer-free": **the sized moves may only ever be planned for a row the
collector never has to scan.**

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

**A Query must name at least one field it matches on presence — a Component, a
Tag or a `With`** — checked at registration, where a Query of `Without`s alone,
or of no fields, fails naming the Query
([#291](https://github.com/dvoyni/cog/issues/291)). The Driver is the shortest
Store among those fields, so a `With` is a Driver candidate like a Component
field: its `owners` is a superset of the match set, exactly as a Component
field's is. **A `Without` can never drive**: `Without[T]`'s `owners` lists
exactly the Entities to *exclude*, and nothing enumerates the complement. An
earlier revision said no filter could drive, and gave as a second reason that a
`Without`-only Query would have to drive off `Entities` and so put
`read{*Entities}` into its lock set for that reason rather than by design. That
half is withdrawn: every System declares `read{*Entities}` unconditionally
([The lock set](#the-lock-set), rule 1), so it could never have widened a lock
set. The complement argument alone decides it.

### The known bad case, stated with its remedy

Two large, mostly disjoint Stores — 5 000 with `Body`, 5 000 with `Collider`,
100 with both — on the implementation
([#271](https://github.com/dvoyni/cog/issues/271); AMD Ryzen 9 7950X3D,
go1.27.1, windows/amd64, 2026-09-12, `-count=5` medians):

| | ns/op | allocs |
| --- | --- | --- |
| drive off a 5 000-entity Store | 3 354 — yields 5 000 candidates, 100 survive | 0 |
| drive off a 100-entity Tag on the intersection | **422 — 7.9× faster** | 0 |

An earlier revision quoted 2 596 against 137 ns, 19×, from the Store model in
`ecs-sparse-probe-bench`; the implementation measured 7.9×, and 19× is
withdrawn. **The smaller ratio is not a smaller hazard.** The discard side
matches the model: **0.60 ns per discarded candidate** against its 0.52 — real,
linear, cheap. The whole difference is in the remedy arm, which on the
implementation is a three-field Query doing real work per match — the Tag
drives, two probes, two fills and the body's read-modify-write — at about
**4.2 ns per matched Entity**, where the model's 137 ns implied about 1.4. It is
per-match cost and not fixed cost: an overlap sweep of 10 / 100 / 1 000 Entities
gives 65 / 435 / 4 113 ns, about 4.1 ns an Entity over ~26 ns fixed (#271).

Spelled with `_ ecs.With[Solid]` instead of a `Solid Solid` field, the remedy
costs the same, and it is the spelling the README recommends for presence a
System matches on but does not read
([#291](https://github.com/dvoyni/cog/issues/291); same machine, 2026-09-22,
interleaved over ten rounds from two test binaries, medians, 0 allocs):

| Query | ns/op |
| --- | --- |
| `{Body *Body; Collider Collider}` — drives off 5 000 | 3 645 |
| `{Body *Body; Collider Collider; Solid Solid}` | 477.65 |
| **`{Body *Body; Collider Collider; _ ecs.With[Solid]}`** | **481.35** |

Both remedy spellings are 7.6× faster than the bad case in that session, within
0.8% of each other.

**The remedy is an app-maintained Tag**, which is just another Store and a very
good Driver, named in the Query as `_ ecs.With[Solid]`. There is deliberately
**no ECS mechanism** for it: that road is EnTT groups, shipyard packs
and bevy's sparse-set markers, and exclusivity is what kills all three.

### Presence bitsets, measured and declined

**Rejected shape: narrowing the walk by intersecting per-Store presence
bitsets** (#258). A bitset is not a global index: it would live in `Store[T]`,
be written under `write{*Store[T]}` or `write{*Entities}`, and be read under the
`read{*Store[T]}` every Query naming `T` already holds. So it costs no System
parallelism. It failed on speed. **The app-maintained remedy stays the only
answer to the bad case.**

The prototype ANDs the bitsets of the fields matched on presence, AND NOT for a
`Without`, into a scratch snapshot. It walks the set bits and finds each row
through the sparse slots, and it has `All()`'s call shape at each width. Each
point has 5 000 `Body` and 5 000 `Collider` over exactly 10 000 live Entities.
The extra Components at widths 3 and 4 sit on every `Body`, so `Body` drives the
probing arm. Medians of ten interleaved rounds, in ns/op, with 0 allocations in
every arm and an A/A noise band of 3% at every point:

| overlap | width | probing | bitset | R1 | R2 | remedy |
| --- | --- | --- | --- | --- | --- | --- |
| 100% | 2 | **11 925** | 14 055 | 14 267 | 11 832 | |
| 100% | 3 | **23 008** | 24 857 | 24 968 | 23 332 | |
| 100% | 4 | 29 580 | 29 533 | 30 486 | 29 849 | |
| 100% | 3, `Without` | 15 060 | **11 976** | 11 665 | 11 450 | |
| 50% | 2 | 7 429 | 7 048 | 7 084 | 7 119 | |
| 50% | 3 | 14 023 | 13 534 | 12 866 | 12 910 | |
| 50% | 4 | 23 778 | **15 525** | 15 362 | 15 654 | |
| 50% | 3, `Without` | 9 905 | **6 094** | 6 146 | 6 135 | |
| 10% | 2 | 4 168 | **1 691** | 1 725 | 1 722 | |
| 10% | 3 | 6 255 | **3 010** | 3 025 | 3 083 | |
| 10% | 4 | 7 230 | **3 522** | 3 542 | 3 604 | |
| 10% | 3, `Without` | 5 802 | **1 510** | 1 490 | 1 532 | |
| 2% | 2 | 3 791 | **449** | 455 | 476 | 509 |
| 2% | 3 | 4 742 | **750** | 777 | 782 | |
| 2% | 4 | 4 861 | **901** | 926 | 1 000 | |
| 2% | 3, `Without` | 4 632 | **418** | 425 | 444 | |
| 1% | 2 | 3 566 | 307 | 311 | 333 | **246** |
| 1% | 3 | 4 295 | **440** | 459 | 488 | |
| 1% | 4 | 4 083 | **573** | 582 | 603 | |
| 1% | 3, `Without` | 4 070 | **311** | 323 | 338 | |

The `Without` at 100% excludes half the intersection, so it matches 50%.

**This is a selectivity trade.** An always-on bitset is 18% slower at full
overlap and width 2, and 8× faster at 2%. It beats probing from 50% overlap down
at every width. At 2% it even beats the remedy Tag (449 against 509), and the
remedy wins back only at 1%.

**The two Driver rules** each run once per run. Both take the bitset below a
match fraction of 0.75.
- **R1** predicts matches from Store lengths under independence,
  `len(gens) × Π(len/len(gens))`. It chose the bitset at every point. At width 2
  it predicts 2 500 at every overlap, because disjointness is correlation and
  lengths cannot see it.
- **R2** probes the Driver's last 16 rows without yielding them. It chose probing
  at 100% for widths 2 to 4, and the bitset everywhere else, which is the faster
  path at every point.

**Go required every condition for one rule, and neither rule met them all:**

| condition | R1 | R2 |
| --- | --- | --- |
| 1. at 100%, widths 2–4, not slower than probing by more than the band with 8/10 rounds agreeing | **fails**: +19.6% at width 2 (10/10), +8.5% at width 3 (8/10) | holds: −0.8%, +1.4%, +0.9% |
| 2. at 2%, width 2, at least 3× faster than probing | holds: 8.3× | holds: 8.0× |
| 3. within 10% of the faster arm at every point | **fails**: +19.6% at 100% width 2 | **fails on medians**: +11.1% at 2% width 4 and +10.8% at 1% width 3; the minima hold, worst +8.5% |
| 4. zero allocations | holds | holds |
| 5. `BenchmarkPresenceBitWrite` at most 1 ns | **fails**: 1.200 median, 1.161 minimum | **fails**: the same |

- **Condition 5** is the one that fails for both rules on both medians and
  minima. One set plus one clear on the bitset costs 1.2 ns, of which ~0.24 ns
  is the benchmark loop's own overhead.
- **R2's miss on condition 3** is the rule's own cost at the smallest walks:
  16 sampled rows and a second `bind`. A production rule would reuse `bind`.
- R2 samples the tail of the Driver, so it is only as good as the tail is
  typical. The sweep shuffles the four kinds of Entity for that reason. With
  `disjointStores`' layout, the whole intersection is allocated last, so R2
  would read no rejects at any overlap.

The machine was shared with concurrent builders (Ryzen 9 7950X3D, go1.27.1
windows/amd64). The prototype is in 4e4468f
(`bundles/ecs/internal/types/selectivitybench_test.go`), removed after it.
Check it out there to re-run the sweep.

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
    ecs.ToHandler[app.UpdateEvent](registrar, move,
        ecs.Feed(func(e app.UpdateEvent) float64 { return e.Dt })),
).After[GravitySystem]()
```

`ecs.ToHandler[E](registrar, system, feeds...)` returns a
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

### A System is not re-entrant

A System's arguments, its event or request cell, `ToExecute`'s single `Resp`,
and the handles each parameter resolves once an invocation into its own fields
are allocated once, at registration, which is what makes a System cost nothing a
tick. Two invocations of one System therefore must never overlap.

The kernel enforces it rather than the caller promising it: every System's
`Lock` declares `ResourceAccess.Exclusive()`, which excludes a System against
itself alone and leaves it concurrent with every other System. A System that
writes a Store would be serialised against itself by that write in any case; the
declaration is what covers a **read-only** System, whose lock set holds no write
for the scheduler to serialise on.

### What a signature may contain

This is contract, not convention. A System takes any number of:

| parameter | declares | what it is |
| --- | --- | --- |
| `*ecs.Query[Q]` | `read{*Entities}` + per-field access | the Components it iterates |
| `*ecs.Spawn[S]` | `write{*Entities}` + `write{*Store[F]}` per Component set field | creating Entities |
| `*ecs.WriteableEntities` | `write{*Entities}` | despawning |
| `*ecs.Get[T]` | `read{*Store[T]}` | reading one Component of an Entity it did not iterate to |
| `*ecs.Set[T]` | `write{*Store[T]}` | writing, or inserting, the same |
| `*ecs.Hooks[T, K]` | `read{*Entities}` + `read{*Store[T]}` | what happened to `T` since the System's last run — [`hooks.md`](hooks.md), *since Hooks* |
| `*ecs.Read[T]`, `*ecs.Write[T]` | the kernel's own read/write on `T` | any other plugin's resource |
| `*ecs.In[T]` | nothing | a value projected out of the event |
| `*ecs.Resp[Res]` | nothing | a command only: the answer it writes |
| `kernel.Kernel` | nothing | the kernel value |
| the event or request value | nothing | legal, and not the default — see below |

**Anything else is a composition-time failure naming the System's type.** This
is a mistake every new user makes once, so the diagnostic matters more than the
mechanism. The failure is a registration-time panic, which the plugin boundary
reports as `kernel.ErrPluginPanic` naming the plugin; the ecs README's
§What a signature may contain records why a reported composition error was
withdrawn.

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

ecs.ToHandler[app.UpdateEvent](registrar, advance, ecs.Feed(func(e app.UpdateEvent) float64 { return e.Dt }))
ecs.ToHandler[FixedTick](registrar, advance,       ecs.Feed(func(e FixedTick) float64 { return e.Step }))
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
frame steady state is **4.002 objects a frame at 1k and 4.002 at 10k** for `In`
+ `Feed`, against **4.003 and 4.002** for naming the event — the engine's own
line, identical either way (`TestTheBoundFrameSitsOnTheEnginesAllocationLine`,
read at da4a6ae; 6.004 / 6.001 and 6.004 / 6.003 before 98f84c8).

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
   unconditionally and first. Not only Queries: Accessors, `Add`, `Remove` and
   Hooks too, so the invariant is closed by construction rather than by the
   accident that everything happens to use a Query.
2. **A Query field's pointer-ness is its access mode**, and a filter field
   contributes a read of its Store.
3. **`Spawn` and `WriteableEntities` take `write{*Entities}`**, which supersedes
   the read and is therefore a **total barrier**: it excludes every System in
   the frame.
4. **`ecs.Read[T]`/`ecs.Write[T]` join the same set**, so a bound plugin's
   resource is as visible in the signature as a Component is.

**A Hooks reader adds its own two reads and never changes another handler's lock
set.** A reader existing makes no writer declare more. `TestAHookNeverWidensALockSet`
and an occupancy test hold that
([The benchmarks hooks.md publishes, and the thresholds they are held
to](https://github.com/dvoyni/cog/issues/385)).

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

The static `Spawn[S]`'s per-Component `declareWrite` is therefore
**redundant for locking**, and it is **kept as an ownership declaration**. That
is what makes cog's composition check fire, so a plugin spawning a `Health` must
declare a dependency on `Health`'s owner. Dropping it would let any plugin
fabricate any other plugin's Components with no declared relationship — a bigger
hole than the redundancy is a cost.

### What the barrier costs, and where the cost actually is

Three Systems over 2 000 Entities: the two workers write different Components,
and the third iterates **its own** Component, disjoint from the workers', in
every arm, differing **only in what it declares**. These are the
`BenchmarkBarrier*` benchmarks in `internal/types/spawnbench_test.go`
(`barrierEntities = 2_000`). Time from
[#272](https://github.com/dvoyni/cog/issues/272) (AMD Ryzen 9 7950X3D, go1.27.1,
windows/amd64, 2026-09-12, medians of five); allocations read from the tree at
da4a6ae (`-benchmem`):

| third System declares | ns/frame | allocs |
| --- | --- | --- |
| *(absent — two workers only)* | 13 695 | 8 |
| `read{*Entities}`, a Query | **14 907** | 9 |
| `write{*Entities}` — a `Spawn` parameter it never uses | **23 086** | 9 |
| the same, spawning and despawning one Entity a tick | **23 335** | 9 |

**The barrier costs ~8.2 µs a frame** — one scheduling round, a writer draining
every reader and then releasing them. At 30 Hz that is **0.025% of a frame**,
and it costs no allocation: the declared arm allocates what the reading arm
does. An earlier revision quoted 18 633 / 30 230 / 36 016 / 36 226 ns, ~6 µs and
0.018%, from the prototype, with 10 / 11 / 11 / 11 allocations; the
implementation measured ~8.2 µs, and every whole-frame count has been lower
since 98f84c8 took the context out of the kernel. That revision also called ~6 µs
"exactly what the ~2.2 µs task floor predicts"; that claim is withdrawn, since
the figure moves with the composition. **The figure depends on the
composition**: #272 found that when the third System's Component overlaps the
workers', the barrier collapses to about **0.4 µs**. No benchmark in the tree
reproduces the overlapping composition; the disjoint one above is the case
where `*Entities` is the only difference between the arms.

**The spawning itself is free.** Declaring a `Spawn` and never using it costs
23 086 ns; actually spawning and despawning every tick costs 23 335 ns, 249 ns
more, which is the spawn and the despawn themselves. **The entire cost is the
declaration, and it is paid at registration.**

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
[`kernel/docs/specs/ecs-support.md`](../../../../kernel/docs/specs/ecs-support.md).

---

## Structural change

A **structural change** is a change to which Entities have which Components, as
against a change to a Component's value: `Spawn`, `Despawn`, adding a Component,
removing one.

**There is no exclusion mechanism to build, and no structural change is a
Command.** That is the whole of [Structural change, and what excludes it from
iteration](https://github.com/dvoyni/cog/issues/240), and it retired all four of
that ticket's candidate answers at once. The ECS's Commands are
[`ShrinkCmd`](#giving-memory-back) and the three [read
Commands](#reading-the-world-by-name), and none is a structural change. A `Uses` dispatch is not a scheduled
unit — it passes `noLocks, noLocks` and runs on the calling goroutine — and the
fold is **static**, run once at finalisation, transitively, with cycle
detection. So **a handler's lock set is complete before the frame starts**: a
System that can spawn holds the spawn's locks for its entire run, and the
scheduler has already excluded everyone.

### The shape

| handle | method | declares |
| --- | --- | --- |
| `ecs.Spawn[S]` | `.New(S) Entity` | `write{*Entities}`, plus `write{*Store[F]}` per Component set field |
| `ecs.WriteableEntities` | `.Despawn(Entity) bool` | `write{*Entities}` |
| `ecs.Get[T]` | `.Of(Entity) (T, bool)` | `read{*Store[T]}` |
| `ecs.Set[T]` | `.Of(Entity) (T, bool)`, `.Ref(Entity) (*T, bool)`, `.UpdateFor(Entity, T)`, `.MarkChanged(Entity)` | `write{*Store[T]}` |
| `ecs.Remove[T]` | `.From(Entity) bool` | `write{*Store[T]}` |

Spawn and Despawn are **two handles rather than one**, because folding `Despawn`
onto `Spawn[S]` would force a Component set type on Systems that never spawn.

`Get[T]` has **no `Ref`**, and that is what stops a read handle being a write in
disguise. `Set[T].UpdateFor` **inserts when absent** — legal because it already
holds `write{*Store[T]}`, and safe during iteration because the reverse walk
never reaches an appended entry. So `UpdateFor` is how a Component is added;
`Remove[T]` is how one is taken away.

**On a Store a Changed Hook watches, a `*T` Query field, `Ref` and `UpdateFor`
copy the rows they hand out**, for the compare at the writer's run end. That is a
cost the reader puts on every writer of `T`, and it is priced in
[`hooks.md`](hooks.md#what-it-costs), and measured in the README's [*What a Hook
costs*](../README.md#what-a-hook-costs). *Since Hooks.*

**`Set[T].MarkChanged(e)` forces a Changed record for `e`** at the writer's run end, for a
write the compare cannot see, such as a `Set` on a List nested in another List's
element. It needs nothing beyond the `write{*Store[T]}` `Set[T]` holds, and does
nothing where no reader watches `T` for Changed
([`hooks.md` § Changed is a difference in bytes](hooks.md#changed-is-a-difference-in-bytes)).

~~**Gap.** The prototype builds `Query`, `Spawn`, `WriteableEntities`, `Read`,
`Write` and `In`, and it was the accessors' *cost* that was measured — on the
Store model in `ecs-sparse-probe-bench`, not on a real engine.
`Get`/`Set`/`Remove` as kernel-bound handles were never composed, so their
allocation behaviour in situ is inferred from the others rather than observed.~~
**Closed** by `TestTheAccessorsStayOnTheEnginesAllocationLine`
([#273](https://github.com/dvoyni/cog/issues/273)): the three are composed as
kernel-bound handles and sit on the engine's own line, flat in Entity count —
at da4a6ae, 4.001 / 4.002 objects a frame at 1k / 10k following a Reference and
4.002 / 4.002 adding and removing a Component per Entity, against a
hand-written subscription's 4.006. The inference held.

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

So: swap-remove on `Remove[T]`, swap-remove on Despawn, **nothing reclaims on its
own**, and the index returns to the free list immediately — safe not merely
because generations are exact but because nothing stale is left to trip over.
This said "no compaction, no shrink, no sweep" until Hooks. Memory now goes back
when the app executes [`ShrinkCmd`](#giving-memory-back), and never on the
engine's initiative. What eager reclamation was chosen for is unchanged.

**The vacated row is zeroed if, and only if, the Component is not pointer-free.**
A pointer-free row is left where it lies, because nothing it holds keeps
anything alive and clearing it would be work for no one — which is what the
numbers above were taken against and they are unchanged. A row holding a string
or a List is cleared by a typed assignment, because otherwise the slot keeps a
despawned Entity's data reachable until something else happens to take the row.
`TestARemovedRowDoesNotKeepItsValueAlive` pins it with a finaliser, which is the
only way to ask about reachability rather than a proxy for it. **That holds for
the Store, not for a Hook log:** on a Store a removal reader watches, the log
keeps the removed value alive until the last reader passes it
([`hooks.md`](hooks.md#the-log-and-the-locks-it-is-appended-under)).

A Despawn reaches every Store through an interface carrying **exactly one
method**, `remove(Entity)`. That costs 9%: 85 Stores are 218 ns through the
interface against 199 ns direct, zero allocations. Driver selection reads
`len(owners)` through the typed path inside a Query, never through the registry.
Since [#340](https://github.com/dvoyni/cog/issues/340) the authority is declared
in `internal`, which cannot name that interface, so a Store enrols its one
method bound to itself and a Despawn calls that: still one indirect call per
Store, never per Entity. A Store a Hook watches also enrols a capture, which a
Despawn calls first so the removal is recorded with its last value; a world with
no watched Store takes the path above unchanged. The Despawn figures here and
the Spawn figures in the next section predate the nothing-watching cost Hooks
add to both, which
[`hooks.md`](hooks.md#budgets-for-the-costs-nobody-asked-for) publishes with its
budgets.

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
its own `Lock`, with no dispatch at all. Component set fields are reflected **once at
registration** into one cached closure per field — the mirror of the Query fill:

| | ns/spawn | vs hand-written | allocs |
| --- | --- | --- | --- |
| `Spawn[struct{Body; Collider}]` | **15.7** | 1.47× | 0 |
| `Spawn[…4 fields]` | **26.0** | 2.11× | 0 |

The same volley costs **416 ns — 43× less**. **The trap, which an implementation
will hit:** the obvious spelling allocates. Passing the Component set by value
and taking `&v` hands the address of a parameter to an opaque func value, so the
value escapes — **48 B and one allocation per spawn**, 28.4 against 15.7 ns.
Copying into a buffer bound at registration removes it, sound because the
handler holds `write{*Entities}`.

**The struct type names a Component set for one act of creation and nothing
more.** The Entity may gain and lose Components afterwards, and from then on the
struct type means nothing. A field of it simply *is* a Component field — there is
no conversion step, because everything a Component may hold can be written where
the struct is declared, so a declarative spawn naming a model needs nothing from
the ECS:

```go
sp.New(DeclSet{
    P: Placement{Scale: 1},
    D: Drawable{Model: "models/crate.glb", Layers: scene.LayersAll},
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
no ordering, which ran first is unspecified. What a System reads through Hooks,
its own acts included, is [`hooks.md`](hooks.md#when-a-system-sees-a-record)'s
to specify.

### Hooks: what happened, read by a System

This section said structural hooks were **"ruled out of scope, not deferred"**,
so that a future need would arrive with a use case, and the use cases have
arrived. Callbacks stay rejected for the reason it gave: `OnAdd[T]` /
`OnRemove[T]` would fire inside a structural change, holding exactly the changing
Store's lock and nothing else, which is why a Hook is instead a record that an
ordinary System reads later under its own declared locks. They are specified in
[`hooks.md`](hooks.md).

### The load-bearing invariant the kernel cannot enforce

A Despawn reaches every Store through Go pointers held by `Entities`, which the
kernel does not police. **That traversal is sound only because every handler
touching a Store declares `read{*Entities}`.** It holds by construction — the
only route to a Store is a generated handle, and the generated `Lock` always
emits that read — but it is written into the spec as an invariant, not left
implicit, because an implementation that added a second route would break it
silently. [`ShrinkCmd`](#giving-memory-back) is the second thing that relies on
it: it rewrites every Store's arrays holding `write{*Entities}` alone. The
three read Commands are a further route beside it: they [read every Store by
Component name](#reading-the-world-by-name) through closures registration baked
into `Entities.classes`, not through a generated handle. They are sound for the
same reason `ShrinkCmd` is: their only callers hold `write{*Entities}`, which
supersedes the `read{*Entities}` the invariant requires, so no handler touching
a Store runs while they do.

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

That table is the Store model's (`ecs-sparse-probe-bench`), not the
implementation's. **Random access is the same probe the Driver already pays**,
and that half held on the implementation: the bare probe costs **0.75 ns** with
the resource cell hoisted. An earlier revision went on to call it **"not a
second-class path"**, which was true of the probe and not of the handle as
first built: `Get` paid a type assertion out of the resource cell on every call,
where a Query pays it once a run — `Get.Of` **2.65 ns** a call, and a homing
frame's slope **5.63 against a hand-written 1.98 ns an Entity**
([#273](https://github.com/dvoyni/cog/issues/273), same machine, 2026-09-12).
That is what the handle cost before #282. Since
[#282](https://github.com/dvoyni/cog/issues/282) every accessor resolves its
handles once an invocation, and what is left is stated rather than argued (AMD
Ryzen 9 7950X3D, go1.27.1, windows/amd64, 2026-09-22, two test binaries
interleaved over ten rounds, medians): `Get.Of` is **2.01 ns** a call against
the hoisted probe's 0.75, the difference being the load of the cached field and
the call, not the assertion; and the homing frame's slope is **4.46 against the
hand-written 2.11 ns an Entity, 2.1×**. For comparison, a plain two-Component
Query runs at 2.64 against its hand-written loop's 1.79, 1.5×
([#280](https://github.com/dvoyni/cog/issues/280), which measured it with
`All()`'s inlined two-field walk). **Following a Reference now costs about
1.3 ns a call over the bare probe, and nothing in allocation.** The README's
[What reaching another Entity costs](../README.md#what-reaching-another-entity-costs)
carries both builds side by side.

Three guarantees, written as tests, two of them free consequences of
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
(`bundles/scene/model.go:47`, "row 0 of every model is the authored hierarchy resolved
once"; `gltfload.go:421` is the only glTF node walk and it runs at load), so the
relation that usually forces an ECS to grow relations is answered on the far
side of the binding. `Parent`, `Child` and `Hierarchy` are a game's words for
its own Components; the engine has no opinion about them.

---

## Reading the world by name

Every way into the ECS above names its Components as Go types, and the lock set
is derived from those types at registration. Something outside Go cannot name a
Component that way: an Agent, a debugging tool or a data file has only a string
such as `"ecsscene.Model"`. So the ecs plugin registers three read-only
Commands that take the string instead
([#371](https://github.com/dvoyni/cog/issues/371)):

| Command | answers |
| --- | --- |
| census | every registered Component name with its Store's population, the live Entity count, the free-list depth and the index space |
| entity | one Entity, given as `"7v2"`, `"Entity(7v2)"` or its decimal handle, with every Component it carries and its value |
| query | the Entities carrying every named Component, in ascending index order, with only those Components' values; `total`, `truncated`, a default limit of 50 and a maximum of 500 |

They are unexported (`censusCmd`, `entityCmd`, `queryCmd` in
`bundles/ecs/internal`; the census was the ticket's "world" read, renamed
because no identifier in ecs may name a World), with their request and response types in
`internal/types`, because nothing outside ecs dispatches them: the mcp provider
that offers them to an Agent is ecs's own
([#289](https://github.com/dvoyni/cog/issues/289)). The root gains nothing for
them.

What an Agent sees of them, the tools `ecs_world`, `ecs_entity` and `ecs_query`
and the prompt text each carries, is specified in [mcp.md](mcp.md).

**The name is `kernel.TypeName`, and `classes` is the mapping.** Registration
already keeps a `componentClass` per Component type in `Entities.classes`, and
each class already carried its name as `owner`. So there is no second registry:
type to name is `class.owner`, and name to type is one unexported scan,
`classNamed`. `RegisterComponent[C]` bakes four more closures into the class
while `C` is still a compile-time type (the Store's population, its membership
probe, its owners and a JSON encoder of one Entity's value), which is the reason
`declareSet` is baked there. Nothing is added to `Store`, `Query` or any fill
path, and `Entities` gains no field: its size class is measured on a despawn.
Two types that render one name are refused together, listing both as
`PkgPath.Name`, and that package-qualified form is accepted.

**The lock is one entry, and it is already solved.** Each Command declares
`write{*ecs.Entities}` and nothing else. [That is one entry, not
N](#writeentities-is-one-entry-not-n): every handler touching a Store holds
`read{*Entities}`, so the one write excludes every ECS System without naming a
Store, which is why a Command registered before any Component exists can reach
all of them. `ShrinkCmd` holds exactly the same lock for the same reason.

**The price.** A call waits for every running ECS System to release
`*ecs.Entities`, and under Conflict-aware FIFO every ECS System queued behind it
waits until it returns. The stall is linear in the walk (for a query, the
smallest named Store's population) plus up to the limit's encodings, which is
what the limit bounds. A Command dispatched through the Executioner runs on the
calling goroutine once its locks are granted, so the stall is charged to
whichever frame is in flight, not to a frame of its own. Nothing here measures
it; the write barrier alone measured
[~8.2 µs a frame](#what-the-barrier-costs-and-where-the-cost-actually-is)
([#272](https://github.com/dvoyni/cog/issues/272)).

**No frame pays for it, and no lock set widens.** They are Commands, not
Systems: a frame nobody reads from runs exactly the handlers and lock sets it
ran before, and the ecs plugin still subscribes nothing. What does change is
visible where it should be: `Describe().Contention` lists the three Commands as
writers of `*ecs.Entities` conflicting with every System.

**Values are encoded under the lock and leave it detached.** A copied `m.List`
shares its backing array, and a System holding a write lock may call `List.Set`
the moment the reader lets go, so encoding after the handler returned would be
a data race. Each value is encoded to JSON inside the handler and decoded back,
with `UseNumber`, into plain maps, slices, strings, `json.Number` and bools that
share no memory with any Store; `UseNumber` keeps a `uint64` Entity Reference
exact. `m.List` encodes as the array of its elements and `assets.Blob` as
`{"len":N}`, never its bytes, which could be texture-sized and would be encoded
under the barrier. A value that cannot be encoded (a NaN) is reported on that
Component alone.

**Liveness on this path is `Alive`, over a generation an `Entity` may carry.**
[The free bit](#entity) answers the handle a freed index will carry next, so
this path no longer scans the free list for it. What it drops first is the one
handle only a parser can name: the word a free index stores, that next
generation with the free bit set, which `Alive` matches because it *is* that
index's word. No `Entity` carries it and no Go caller can make one. A dead
Entity is refused naming the Entity that holds its index now, where one does; a
free index names no holder, which is the same bit again. `Alive` itself is
unchanged by this path.

**Refusals are answers.** An unknown name (refused listing every registered
name), an ambiguous one, a malformed or dead Entity and a limit out of range
are expected outcomes, so they travel in the response's `Refusal`, with every
other field zero, rather than as a handler error. A handler body has no error to
return, and `Executioner.ExecuteCommand` answers with the response alone.

**Why Commands and not Systems.** A System may not name `*Entities`
(`guardHandle` refuses it), `WriteableEntities` exposes only `Despawn` and no
Store enumeration, and a `Query[Q]` cannot be built from strings. A Command
holding the authority for write is the one shape that reaches every Store by
name without widening anything a frame runs.

---

## Binding: how another plugin attaches

**There is no binding mechanism, and that is the decision.** A plugin that is
not the ECS attaches to the world by being an ordinary plugin: it registers
Components if it has any, subscribes Systems like anything else, and reaches its
own frame-local resource from inside them. The ECS gains exactly one thing — a
System may take anything a cog handler may take — and **no new vocabulary at
all** ([The binding shape](https://github.com/dvoyni/cog/issues/246)).

This was settled against the real `scene` package rather than against physics
and audio, which did not exist then and therefore could not answer it. Scene is
also the case a game reaches for first.

> **Amended by [#538](https://github.com/dvoyni/cog/issues/538).** `ecsscene`
> no longer binds into scene. Since the model split
> ([model.md](../../../model/docs/specs/model.md)), it is the ECS's renderer over
> `model`: it records to `gfx` itself and imports nothing of scene, and an app
> composes it *instead of* scene, never beside it. The decision above did not
> move — ecsscene is still an ordinary plugin reaching resources through its
> Systems' signatures, with nothing of the ECS's own. What changed is which
> resources, and three subsections below say what they used to say. ecsscene's
> own design record is [`ecsscene.md`](../../../ecsscene/docs/specs/ecsscene.md).

ecsscene's recording System, as it is now:

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

It used to take a write on scene's `OpQueue` and make one `queue.Model` or
`queue.Mesh` call per Entity. The shape of the signature is the same; only the
resources are different.

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

`ecs` imports only `kernel`, `libs/m`, `libs/assets` (for `assets.Blob` in
the storable walk) and the `bundles/mcp` root (for the Provider's Adapter
identity, [#289](https://github.com/dvoyni/cog/issues/289)); `model` and `gfx`
import nothing of `ecs`.
**Neither side can know about the other**, so a binding is necessarily a third
plugin that imports both. That is what "there is no binding mechanism" means in
practice, and it has a consequence worth stating: **a project not using the ECS
schedules no ECS Systems** — it simply does not register that plugin.

When this was written the other side was `scene`, which imported nothing of
`ecs` either; the argument is the same with `model` and `gfx` in its place.

**cog ships the bindings for its own in-house plugins** — `ecsscene` for drawing
over `model`, `ecsphysics2d` and `ecsaudio`. They remain separate plugins,
because the import graph allows nothing else, and a game that wants its own
instead simply does not register cog's.

### Data flows one way, and for a renderer that is not a choice

`gfx.OpQueue` is frame-local and re-recorded from nothing every frame. `model`
retains residency — loaded models, meshes, textures — and **no per-entity state
whatsoever**. There is no renderer-side object for a drawable Entity to be a copy
of, so "two copies and a sync cost every frame" does not arise, and neither does
the coupling worry on the other side: **the Components are the source of truth
because there is no other candidate.** This was said of scene's `OpQueue` first,
and it held unchanged when ecsscene moved from scene's queue to gfx's.

ecsscene does keep one thing between frames: its load System's **key
scratch**, the Batch key of each Entity, written when a `Model`, `Mesh`,
`Material` or `Params` changes. It is derived data, rebuilt from the Components
on change and dropped when the Entity is, never a second source of truth, and it
is a resource named in both Systems' signatures rather than anything the ECS
holds.

This does **not** generalise to physics, which does retain bodies. The direction
question is the bound plugin's to answer and stays
[#182](https://github.com/dvoyni/cog/issues/182)'s. What is settled is that the
*mechanism* is the same either way.

### The wide lock lands in the bound plugin, not in the ECS

Not where it was expected. The queue a binding records into is **one
resource**, so every recording System serialises against every other recording
System for write, whatever Components they read. **The ECS's per-Store
granularity buys nothing on the recording side** — that is a property of the
bound plugin's API. It was scene's `OpQueue`; it is `*gfx.OpQueue` now, which
only Last-phase flushes and gfx's own handlers otherwise write, so no ordinary
System lost parallelism in the move.

ecsscene holds **one more lock than it did**: its load System takes
`Write[*model.Lookup]`, because loading is exclusive, and it is the only
ecsscene System that does. The recording System reads the Lookup. A game System
that *writes* the Lookup now serialises against recording, where before scene's
Last-phase flush held that lock. Readers run side by side, so nothing that only
reads model residency is serialised.

Which is fine, because splitting the recording would lose anyway. Whole-frame,
publish → locks → Systems → wait, measured on `ecsscene` when it still proxied
into scene, with `storage`, `gfx`, `scene`, `ecs` and a game plugin composed
beside it — five subscribers to the tick — and 5 000 `Model` Entities per row,
with no frame drawn; the binary it replaced was built first and run alternately,
ten rounds, medians:

| whole frame | ns/op | allocs/op |
| --- | --- | --- |
| nothing to record | 19 931 | **14** |
| 5 000 | 413 928 | **14** |
| 5 000, each blending two clips | 456 584 | **14** |
| 5 000, each with one-colour `Params` | 570 824 | **14** |
| 5 000, each with a two-tag `Material` | 1 116 624 | **14** |

**Zero allocations from the binding**: over a 3 000-frame steady state the five
rows are 14.08, 14.02, 14.03, 14.03 and 14.05 objects a frame, the engine's own
charge, and a profile with every allocation sampled finds none in `ecsscene`,
`ecs`, `scene` or `gfx`. Time is linear at ~79 ns an Entity; the material row is
mostly scene copying the material into its arenas once per draw
([#314](https://github.com/dvoyni/cog/issues/314)). Against that, a second System
costs about **5.9 µs** of scheduling floor, so a second recording System would
have to save visiting ~75 Entities to pay for itself — and it still could not
run concurrently, because both hold the queue for write. Worse, a recorder
blocked behind another costs the kernel's scheduler an allocation per blocked
dispatch, which shows up as a fraction of an object a frame growing with frame
length. **One recording System per bound plugin is the shape.**

Those rows predate the redesign and measured recording only. The benches that
replaced them draw the frame into a backend as well, and are in
[`ecsscene.md` §What it costs](../../../ecsscene/docs/specs/ecsscene.md#what-it-costs):
5 000 Entities went from 14.2 ms through scene to 1.1 ms recorded to gfx as one
Batch, with allocations flat at 18–20 a frame whatever the population.

### Ordering needs nothing new

gfx subscribes its present, `gfx.PresentOnUpdate`, `Last`, so a recording
System that does not ask to be last already runs before it. ecsscene orders its
load System `Before[ecsscene.RecordOnUpdate]()` with the ordering the kernel
already has, and a game System that moves Transforms orders itself
`Before[ecsscene.RecordOnUpdate]()`. No new ordering vocabulary. When ecsscene
bound into scene the same held of scene's flush, `scene.FlushOnUpdate`,
subscribed `.Last().Before[gfx.PresentOnUpdate]()`, demonstrated in the
prototype with a `.Last()` stand-in for the flush.

### What a binding plugin may not do

Stated as prohibitions, because a backend adopted from outside cog will not have
been written with them in mind.

- **A Component holds no mutable indirection, transitively** — enforced at
  registration, where the type is named and nothing has been stored yet.
- **Never hand the bound plugin a view of a Store's memory.** gfx copies a
  draw's descriptors at the call, but bytes it was handed without a copy are
  read at its present, which is a *different* handler running after the
  recording System's locks are gone — as scene's flush was when ecsscene bound
  into scene. Copy a `List` out through `All()` into scratch; there is no
  `Raw()` to get this wrong with.
- **Draw data a bound plugin takes as a slice is rebuilt, not stored.** Play
  lists, params, material tags and passes are copied out of the Components into
  scratch each frame, and reset per frame rather than per draw, so nothing is
  overwritten before the frame is emitted. Scratch comes from backings allocated once, never from a stack
  array a recording call would let escape.
- **Anything a System keeps between calls is a resource.** Scratch captured in a
  closure has no lock anywhere naming it; as a resource in the signature it is in
  the lock set, and the kernel rather than a comment keeps two holders apart.
  ecsscene's key scratch and recording scratch are both resources.
- **Do not cache an Entity without checking liveness, and do not restructure the
  world from inside another Entity's iteration.**

### One measured thing that was scene's problem, not the ECS's

`scene` re-resolves a model path per recorded draw per frame at **46.9 ns**
against **0.54 ns** for a dense index — ~235 µs a frame at 5 000 drawables,
three quarters of it re-normalising a path that was validated at load. The ECS
binding was correct and allocation-free without any change there, so it was
recorded as [scene: name a model by an interned handle, not by its path every
frame](https://github.com/dvoyni/cog/issues/263) and is out of scope here.

ecsscene no longer pays it: its load System resolves a `ModelRef` to a
`model.ModelHandle`, a plain slot index, once, when the `Model` changes, and the
recording System reads through the handle. scene still resolves by path, and
#263 is still scene's.

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
System, wait. The counts are the implementation's, read from the tree at
da4a6ae: the allocation tests' steady-state logs, rounded, and a `-benchmem` run
for the row only a benchmark measures. The B/op column is the prototype's,
measured before 98f84c8, and was not re-measured:

| shape | allocs/op | B/op, prototype | ECS share |
| --- | --- | --- | --- |
| nothing subscribed (`BenchmarkFrameNoSystem`) | 2 | 160 | — |
| **1 hand-written subscription, no ECS** | **4** | **320** | baseline |
| 1 System, reflected, 1k **and** 10k | **4** | 320 | **0** |
| 1 System, baked, 1k and 10k | *not in the tree*; 6 on the prototype | 320 | 0 |
| 2 Components and a `Without` filter | **4** | 320 | **0** |
| 2 Systems, disjoint writes | *not reproduced in the tree*; 10 on the prototype | 608 | 0 |
| move + spawn + despawn every tick | *not reproduced in the tree*; 10 on the prototype | 611 | 0 |

An earlier revision explained the column with a formula over publications and
subscribers. It is withdrawn: it predicted
26 objects for the six-subscriber ecsscene frame
[#276](https://github.com/dvoyni/cog/issues/276) measured at a flat 15, and
since 98f84c8 it does not fit one subscriber either, which costs 4. No formula
replaces it.
**What shows the ECS contributes nothing is a comparison, not a formula: the
per-frame count is flat, independent of Entity count, and identical to a
hand-written subscription in the same composition.** Per named composition, at
da4a6ae:

- **2** with nothing subscribed (`BenchmarkFrameNoSystem`);
- **4** for one subscriber, hand-written or a System, at 1k and 10k, with or
  without a `Without` filter (`TestTheFrameSitsOnTheEnginesAllocationLine`:
  4.005 hand-written, 4.002 / 4.002, filtered 4.001 / 4.001);
- **4** following a Reference, and adding and removing a Component per Entity
  (`TestTheAccessorsStayOnTheEnginesAllocationLine`: 4.006 hand-written, 4.001 /
  4.002 and 4.002 / 4.002);
- **4** naming the event, with `In` + `Feed`, or with a resource in the
  signature (`TestTheBoundFrameSitsOnTheEnginesAllocationLine`: 4.001–4.003);
- **4** for one subscriber spawning and despawning every tick
  (`TestStructuralChangeStaysOnTheEnginesAllocationLine`: 4.028, and 4.020 at
  ten thousand a tick);
- **8** for a System publishing one event (`TestWhatPublishingFromASystemCosts`:
  8.051 against a silent System's 4.007, 4.044 for the publication);
- **8 / 9 / 9 / 9** for the four barrier arms, two workers with disjoint writes
  and a third System absent, reading, declaring a `Spawn`, and spawning
  (`BenchmarkBarrier*`, [What the barrier
  costs](#what-the-barrier-costs-and-where-the-cost-actually-is)).

The prototype's two-System rows have no exact counterpart in the tree. The
nearest in-tree composition is `BenchmarkBarrierAbsent` — two workers with
disjoint writes over 2 000 Entities, at 8. The README's
[What a Hook costs](../README.md#what-a-hook-costs) carries the Hook-reader
frames.

**The ecsscene frame is not one of these compositions.** Since #537 its benches
draw the frame into a backend through gfx, replay included, and cost 18–20
allocs/op, flat from 0 to 5 000 Entities. A drawn frame is not comparable to a
hand-written subscription line, so its figures are ecsscene's to carry:
[`ecsscene` README §What it costs](../../../ecsscene/docs/README.md#what-it-costs)
and [`ecsscene.md` §What it costs](../../../ecsscene/docs/specs/ecsscene.md#what-it-costs).

Identical at 1k and 10k, which is the real test: an allocation in the iteration
would scale with entity count, and none does. **Steady state over 10 000
frames**, because an average over `b.N` can hide amortised growth: **4.002
objects a frame at 1 000 Entities and 4.002 at 10 000**, against the
hand-written 4.005, at da4a6ae. On the prototype, before 98f84c8, it was 6.005
and 6.003, and 10.048 with structural change in its two-System frame, which no
test in the tree reproduces. With structural change on one subscriber the tree
gives 4.028 at one spawn and despawn a tick and 4.020 at ten thousand. Ten
thousand spawns and ten thousand despawns add nothing — the free list recycles
ids and the Store reuses the dense row, so growth stops at the high-water mark.

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
   System will be written — it does not. Nothing changes semantically within
   one invocation, since the same buffer was already reused for every Entity.
   Across invocations it holds because two invocations of one System never
   overlap: every System's `Lock` declares `Exclusive`, so the buffer is never
   shared by two runs at once ([A System is not
   re-entrant](#a-system-is-not-re-entrant)).
2. **`Spawn` stages its Component set through a field**, same hazard: taking the
   address of the parameter and handing it to cached closures
   heap-allocates it per spawn.
3. **A System returns nothing**, because `reflect.Value.Call` allocates for a
   callee that does. The builder rejects one at registration.

### Time, and one number worth another look

The prototype measured a three-Component Query at 10.8 ns an Entity against 3.21
for two, and recorded a **Gap**: a third probe seemed to cost about 7 ns an
Entity more, whether a third Component or a filter. **It does not reproduce on
the shipped Query**
([#280](https://github.com/dvoyni/cog/issues/280)). `BenchmarkQueryWidth`
times the walk alone at every width, through `All()` and through `q.iterate`
directly, beside a hand-written walk over the same Stores; per Entity at 10 000
Entities, medians of ten interleaved rounds, AMD Ryzen 9 7950X3D, go1.27.1:

| width | `All()` | delegated (`iterate`) | hand-written | delegated × hand-written |
| --- | --- | --- | --- | --- |
| 1 | 2.61 | 1.86 | 0.54 | 3.5 |
| 2 | **2.52** | 3.50 | 1.43 | 2.5 |
| 3 | 5.17 | 4.41 | 2.37 | 1.9 |
| 4 | 7.28 | 6.26 | 3.40 | 1.8 |

On the delegated rows, where every width takes the same path, each field adds
**1.64, 0.90 and 1.86 ns** an Entity for the second, third and fourth: the third
is the cheapest, not a step. The sweep's rule, set before it ran — the gap
reproduces if the third field's step is at least 3 ns and at least twice either
neighbour's — fails on both counts, at 1 000 Entities as at 10 000, and for a
present Tag or a `Without` that rejects nothing in the third slot too (0.82 and
0.84 ns). The 10.8 ns figure was the prototype's. Through `All()` in that
session, width 2 was cheaper than width 1 and width 3 looked like a 2.65 ns step,
because only width 2 took the walk `All()` carries inside its literal and the
others delegated and paid an indirect `yield` an Entity; that is a path, not a
probe. Since [#546](https://github.com/dvoyni/cog/issues/546) widths 1 and 3
have walks of their own, and its re-run of the sweep reads 1.14, 2.25, 3.38 and
6.14 through `All()` against 1.77, 3.19, 4.19 and 5.71 delegated, with the
delegated steps at 1.41, 1.00 and 1.53 — the same verdict. Width 5 and up
run the per-field loop and step up by about 7.5 ns an Entity, the same whether
the Stores fit in L2 or not. The README's
[What a wider Query costs](../README.md#what-a-wider-query-costs) has both
Entity counts, the Tag and `Without` rows, widths 5 to 8 and the Store
footprints.

### Concurrency

Two Systems with disjoint write sets, on the prototype before 98f84c8; no test
or benchmark in the tree reproduces this composition, so the allocation column
is the prototype's (the nearest in-tree frame, `BenchmarkBarrierAbsent`, gives 8
at da4a6ae):

| | GOMAXPROCS=1 | GOMAXPROCS=32 | speedup | allocs, prototype |
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

The design does not depend on the resolution: both builders measured 6 allocs a
frame on the prototype before 98f84c8, the same line as each other, so **the reflective builder is correct and the baked one is an
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

**Change detection — specified, and not in the shape this section promised.** It
said `Added[T]` and `Changed[T]` would arrive as further Query field types over
per-Store change versions, with the lock derivation unchanged, and that only the
storage and its per-frame cost were open. Neither half survived. Versions count
write access as a change, where a Changed Hook counts a difference in bytes
([What counts as a value change, for a Changed
Hook](https://github.com/dvoyni/cog/issues/268)). And a reader shaped like a
Query must widen the locks of every handle acting on its other Stores, which
costs System parallelism: two Systems that ran in parallel in 44.0 µs took
71.5 µs ([Where a Hook log lives, and what recording and resetting it
cost](https://github.com/dvoyni/cog/issues/380)). A System learns what happened
to one Component through `*ecs.Hooks[T, K]` instead, and `Chunk` stays unspent.
→ [`hooks.md`](hooks.md).

**Data-driven spawning and serialisation — additive.** No Component, Query,
System signature or lock changes when it arrives, and the expensive part is
already paid: a Component holds nothing that is not trivially encodable. One
thing the relaxed rule adds to that ticket, recorded here so it is not
discovered there: **a row holding a string or a List is not a fixed width**, so
a row encoder needs a per-type walk for those Components rather than one
`memcpy`. Three
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
and not allocation, both of which are already had. A drained change reaches a
Hook at the drain, as the same records an immediate change makes
([Deferred structural change and Hooks: when a drained change is
recorded](https://github.com/dvoyni/cog/issues/383)). →
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

   **Correction, measured.** This entry said deferred structural change was a
   hard prerequisite. It is not, and the mistake was to treat the affordance and
   the split as competing for the same loop. A loop that may be split is a loop
   that **cannot restructure at all**: the splittability rule below disqualifies
   `Set[T]`, `Spawn[S]`, `WriteableEntities` and `Remove[T]` outright, and what
   is left holds `read{*Entities}`, which every route to a Store declares — so no
   other System can move a row under it either. The 500/1000 measurement stands
   and is what rules out splitting a loop that restructures; it says nothing
   about one that cannot. [#260](https://github.com/dvoyni/cog/issues/260) is
   needed only for a split loop that also wants structural change, which the rule
   already refuses. Measured end to end on a real engine in
   [#278](https://github.com/dvoyni/cog/issues/278), which returned a go: at 5 000
   Entities and ~104 ns an Entity, 4.74x against serial, allocating what the
   serial frame allocates.

   Adding it later therefore needs, in order: a **splittability rule
   over the System signature**, which cog is unusually well placed to derive at
   registration because the signature *already is* the out-of-band declaration
   every other engine makes the author write by hand (`Query` including pointer
   fields is safe, since each Entity is visited by exactly one shard; `Set[T]`,
   `Spawn[S]`, `WriteableEntities`, `Remove[T]` and `kernel.Write[T]` are
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
no `Elem()` call anywhere a resource is keyed — not in `resource.go`,
`resourceaccess.go`, `registry.go` or `scheduler.go`; the only `Elem()` in the
package renders type names. So
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

**`RegisterConversion`, a Component-set-field conversion.** Built and then
removed: it existed because a dense index could not be written where the Entity
was declared. Everything a Component may now hold can be, so the Component set's
field simply
*is* the Component field and there is nothing to convert.

**A bare `[]T` as a Component field.** Refused for the lock unit and not for the
collector, which is the distinction the whole relaxation turns on: a copy of a
slice header shares its backing array, so a read yields a write handle and
`read{C}` stops meaning anything. Every Go ECS surveyed permits it — arche, ark,
donburi, unitoftime/ecs and go-gameengine-ecs all store the header and copy it —
and **not one of them has a lock-deriving scheduler**, so none has the guarantee
to lose. Ark, which permits slices, tells its users in the same breath not to
use them: *"For fast memory access, the use of slices in components should be
avoided. Use fixed-size arrays where possible."* The engines that allow the
shape *safely* are not in Go: Bevy can own a `Vec<T>` in a component because
`Query<&T>` hands out `&T` and `&` is transitively immutable. Go has no
pointer-to-const, which this document already says about reads, and `string` is
the one Go type that has the property built in.

**Deep-copying a Component's variable-length data on the way into the Store.**
It fixes ownership and leaves access untouched: the header a read yields still
points at Store-owned memory, so a read-lock holder still writes the Store
through it. Closing that means copying on the way *out*, which is an allocation
per entity per frame. It would also have cost an allocation per `Set`, a
recursive walk on every write, and the `memcpy` Store — the same three the
content-addressed table was refused for.

**A pointer hash.** A pointer is not a string with an awkward shape. Hashing one
answers no question — the object cannot be recovered from the hash, and an
address is not an identity anything else can agree on. The reference cases
decompose and each already has an answer: another Entity → `Entity`; an engine
object → not in the Component at all, but named in the System's signature; an
asset → its name, in whatever form the plugin that resolves names asks for.

**`Archetype` as a word.** Retired outright rather than annotated. An archetype
is the *exact* Component set of a group of Entities, which this design neither
matches on nor represents. `View` was rejected too: it is EnTT's word and in Go
connotes a slice-like value, which a Query is not.

---

## Required work

The checklist the implementation was built from, in dependency order. It is kept
because it is the record of what was promised; everything in the three code
sections below exists in the package, the documentation items are done, and what
remains open is called out at the end of the verification list. The *Since
Hooks* block after it is built too.

**`ecs` package — the core**

- `Entity`, `NoEntity`, `fmt.Stringer`, private index/generation accessors.
- `Entities`: id allocation with a free list, generation tracking with the free bit, `Alive` in one compare against it,
  the reference to every Store, eager total `Despawn`, created by the plugin from `Config.PrewarmEntities`.
  Declared in `internal` and aliased in the root since #340.
- `Store[T]`: the three arrays, the one-load probe, swap-remove, `append`
  doubling, a reserve hint, and the type-erased `storeCore` carrying exactly one
  method.
- `Storable(reflect.Type) error` and `PointerFree(reflect.Type) error`, each
  reporting the offending field by path: the first is the registration gate, the
  second the fast-path property. **`PointerFree` alone was the original item**;
  the split arrived with the relaxed rule.
- `List[T]`, its registration walk, and validation mode behind `-tags
  ecs_validate`. **Not in the original checklist**, and added with the rule.
- `RegisterComponent[C]`: `InitResource[*Store[C]]` on the caller's `Registrar`,
  enrolment with `Entities`, the baked per-type closures including the typed
  copy a non-trivial Component takes, and the legality check.
- `Plugin() kernel.Plugin`, which creates the authority from `Config`. Since
  #340 it is `ecsimpl.New()` and `ecsimpl.Config`, and since #358
  `ecsplugin.New()` and `ecs.Config`.

**`ecs` package — Query and System**

- `Query[Q]`: registration-time planning of the field table, per-run Driver
  selection by scanning Store lengths, `All()` walking the Driver backwards over
  a half-open index range, the fill buffer **as a field of the Query**, and the
  unrolled fillers chosen by field count.
- `Without[T]`, and `With[T]`, which shipped and may drive
  ([#291](https://github.com/dvoyni/cog/issues/291)); the fill skips blank
  fields.
- The registration-time error for a Query naming nothing it matches on
  presence — `Without`s alone, or no fields — since a Query needs at least one
  Component, Tag or `With` to drive it.
- `ToHandler[E]` and `ToExecute[Req, Res]`; the parameter classification table
  above is the contract; a System returning anything is rejected.
- `In[T]` and `Feed`.
- `Read[T]` and `Write[T]`.
- `Spawn[S]` staging its Component set **through a field**; `WriteableEntities`.
- `Get[T]`, `Set[T]` (`Of`, `Ref`, `UpdateFor`), `Remove[T]` — the three the
  prototype did not build.
- ~~`HashKey`, `HashOf[K]`, `NoHash`, `Names[K, V]` with `Register`, `Lookup`
  and `TextOf`, and the collision check.~~ **Built, then removed.** It existed
  because a Component could not hold a string; once one could, the whole
  vocabulary bought 1.1 ns a lookup.

**`kernel`**

- The conflict report on `Describe`, per
  [`kernel/docs/specs/ecs-support.md`](../../../../kernel/docs/specs/ecs-support.md).
  Nothing else. Every other kernel question this map opened closed with "no
  change".

**Documentation**

- `bundles/ecs/docs/README.md`, which is the package's API per the repo's own layout rule.
- `README.md`'s package list gains `ecs` when the package exists.
- `.github/instructions/kernel.instructions.md`'s "Keep `Lock` Straight-Line"
  becomes "Keep `Lock` Deterministic, Total and Final" — full replacement text
  in the kernel delta.

**Verification, and the bar is not `allocs/op` alone**

- The whole-frame allocation benchmark on a real engine at 1k and 10k, with and
  without a structural change per tick, and a 10 000-frame steady state. The bar
  is the engine's own line — what a hand-written subscription in the same
  composition costs — and *identical* at both entity counts. It read **6
  allocations a frame with one subscriber, 10 with two** when it was written;
  since 98f84c8 one subscriber costs 4, and the counts per composition are in
  [The zero-allocation claim](#the-zero-allocation-claim).
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
- **`-race` in CI — still open, and the one item on this list that is.** Every
  measurement behind this document was taken in an environment where the race
  detector cannot build, and `-count=10` was substituted. That substitution has
  survived into the implementation, which is exactly what this line said must
  not happen, so it is recorded as outstanding rather than quietly dropped. It
  matters more since the Component rule was relaxed: a `List` written through a
  read is a data race, and validation mode catches the ones a run executes while
  the detector would catch the ones that interleave.

**Since Hooks — built**

The items that change what this document specifies for code naming no Hooks.
Everything else Hooks need is in [`hooks.md` § Required work](hooks.md#required-work)
and in the implementation tickets under
[the map](https://github.com/dvoyni/cog/issues/377).

- `List.Set` with a pointer receiver and a generation in a 32-byte header
  ([#268](https://github.com/dvoyni/cog/issues/268)). **Built**
  ([#388](https://github.com/dvoyni/cog/issues/388)).
- `Set[C].Of` no longer stamping a write, so `Set` through its copy panics naming
  `Ref` ([#387](https://github.com/dvoyni/cog/issues/387)). **Built**
  ([#388](https://github.com/dvoyni/cog/issues/388)).
- The shared-array mark registered at the Changed compare, released at the
  removing act ([#268](https://github.com/dvoyni/cog/issues/268),
  [#386](https://github.com/dvoyni/cog/issues/386)). **Built**
  ([#393](https://github.com/dvoyni/cog/issues/393)).
- ~~`ShrinkCmd` and the generation floor
  ([#381](https://github.com/dvoyni/cog/issues/381)).~~ **Built**: Stores,
  Entities with the generation floor, and a Query's walk
  ([#389](https://github.com/dvoyni/cog/issues/389)); the Hook logs, readers'
  copies and writers' row copies for Changed
  ([#394](https://github.com/dvoyni/cog/issues/394)).
- ~~The `*ecs.Hooks[T, K]` row in the signature contract
  ([#380](https://github.com/dvoyni/cog/issues/380)).~~ **Built**
  ([#390](https://github.com/dvoyni/cog/issues/390)).

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
  nothing is foreclosed; the Component rule is the whole of what v1 pays, and a
  row that is not a fixed width is the one thing it adds to the bill.
- **The physics contract itself** — [Physics plugin: contract, swept queries,
  and an adopted backend](https://github.com/dvoyni/cog/issues/182), its own
  map. This spec fixes only the binding shape physics attaches through.
- **Chunk-parallel execution**, entirely, with its non-foreclosure obligation
  withdrawn. [#242](https://github.com/dvoyni/cog/issues/242) holds the full
  record.
- **Data-driven spawning and world serialisation** —
  [#266](https://github.com/dvoyni/cog/issues/266). Includes **a
  runtime-addressable template**: a template is already just a value of a
  Component set's struct type reused at every spawn site, and needs nothing from the ECS. `Template`
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
