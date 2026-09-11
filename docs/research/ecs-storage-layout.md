# Storage layout: archetype tables against sparse sets

Research note for [cog#235](https://github.com/dvoyni/cog/issues/235), under the map
[cog#180 — ecs: a plugin whose systems are plain funcs and whose locks come from components](https://github.com/dvoyni/cog/issues/180).
Read 2026-09-11.

**Findings, not the decision.** The decision is made in
[cog#239 — How components are stored, and what a System body sees while it iterates](https://github.com/dvoyni/cog/issues/239).
Two adjacent tickets own material deliberately left out of here:
[cog#234](https://github.com/dvoyni/cog/issues/234) owns Go's zero-allocation and reflection
mechanics, and [cog#242](https://github.com/dvoyni/cog/issues/242) owns chunk-parallel execution —
§4 below is the input to it, not an answer for it.

**Licence posture.** cog is MIT. Every project read here is permissively licensed. Nothing below is
copied code; designs are restated in prose and quotations are short and attributed.

## How to read this document

Every claim names its source and the **kind** of source it is, because the kinds are not equally
trustworthy:

| Kind | Meaning |
| --- | --- |
| **author** | The library author writing about their own design and its reasons. The most valuable kind, and what this ticket asked for. |
| **docs** | The project's official manual, book, rustdoc, godoc or source doc-comment. |
| **source** | Read directly out of the project's own repository. |
| **bench** | A published benchmark suite. Always name whose. |
| **measured** | Run on this machine on 2026-09-11 for this note. Hardware and method stated inline. |
| **paper** | Peer-reviewed. |

Where a number could not be recovered honestly there is a **Gap** entry saying so. §10 collects them.

---

## Summary

1. **The two families do not differ in "speed"; they differ in *which* operation is free.** Archetype
   tables make a matching query a pointer-free contiguous scan and make a component add a full row
   copy. Sparse sets make an add a push and make a multi-component query a probe per entity per
   extra component. Every number in this note is a restatement of that one sentence.
2. **The measured iteration gap is about 2–2.5×, not an order of magnitude**, and it grows with
   query arity. Rust's `ecs_bench_suite` at 10 000 entities, 2-component query: legion 1.03 ns per
   entity, hecs 1.34, bevy (table) 1.77, shipyard (sparse set) 2.49. Bevy's own maintainers put it
   the same way — "table iteration is 2-4x faster than sparse set iteration".
3. **The measured structural-change gap is 6× to 20×, and it is the bigger number.** Same suite,
   20 000 add/remove transitions: shipyard ≈ 6 ns per transition, hecs ≈ 40 ns, bevy ≈ 67 ns,
   legion ≈ 121 ns. In C++ the same shape holds at roughly 10× (EnTT against flecs).
4. **Both gaps are small in absolute terms and both invert under fragmentation.** On a
   single-component query spread over 26 archetypes, shipyard beats *every* archetype implementation
   by 4× to 19×. Archetype iteration is only fast when the matching entities are in few tables.
5. **Nobody who has built one of these recommends a layout without first asking about the
   workload.** skypjack: the archetype model "works like a charm when the sets of components
   assigned to the entities don't change much during the runtime". Mertens: pick per *component*,
   not per library. cart: tables by default, sparse sets when you know a component churns.
6. **The strongest structural idea found is not a layout at all — it is bevy's separation of
   *archetype* from *table*.** Many archetypes can share one table; they differ only in their
   sparse-set components. Adding a sparse component therefore changes an entity's archetype and
   moves no component data. See §3.
7. **Chunking is not load-bearing for iteration speed and is load-bearing for parallelism and change
   detection.** Unity DOTS chunks at 16 KiB and gets per-chunk change versions and per-chunk job
   scheduling. Legion copied that, then *removed* it. flecs, EnTT, bevy, hecs and every Go
   implementation do not chunk — they split contiguous columns into row ranges instead, which gets
   the parallelism without the memory granule. §4.
8. **Tag components are free in bytes and expensive in tables.** Every mature implementation stores
   a zero-sized component as no bytes at all. In an archetype layout a tag is still a full dimension
   of the table space and toggling one is a full row move. This is the cost that nox's 32 flag bits
   would pay. §5, §8.
9. **Query resolution converged on one answer across four languages: cache matching tables, and let
   the only invalidation event be "a new table was created".** Archetype sets are append-only in
   bevy, hecs, arche, Ark and flecs, which is exactly what makes a monotonic generation counter a
   sound cursor rather than a dirty flag. §6.
10. **nox's projectile churn is not the archetype-hostile workload the ticket assumed.** A spawn is
    entity *creation with a complete component set*, which appends to one table and moves nothing.
    Even 1000 simultaneous live projectiles is ~50 structural events per 30 Hz tick. The
    archetype-hostile workload in nox is the 32 runtime flag bits and the enchantment set on
    long-lived creatures. §8.

---

## 0. The families, and a correction to the ticket's roster

**Archetype tables.** Entities are grouped by their exact component set. Each group holds one
contiguous array ("column") per component type. A query selects whole groups by set inclusion and
scans them; there is no per-entity test. Adding or removing a component changes the entity's exact
set, so the entity's whole row is copied to another group.

**Sparse sets.** Each component type owns a dense array of values, a parallel dense array of entity
ids, and a sparse array indexed by entity id giving the row. A query walks the smallest dense array
and probes each other type's sparse array per candidate. Adding or removing is a push or a
swap-and-pop on one type, touching nothing else.

**A third family exists and the ticket did not name it: bitsets.** `specs` and `EntityX` keep one
storage per component plus a hierarchical bitset of presence; queries intersect bitsets. Classified
this way by SanderMertens' own `ecs-faq` (**author**, https://github.com/SanderMertens/ecs-faq),
which also names a fourth, *reactive* (Entitas), where mutation signals maintain query membership.
The FAQ's own one-line statements of the first two: archetypes "store entities in tables, where
components are columns and entities are rows … fast to query and iterate"; sparse sets "store each
component in its own sparse set which has the entity id as key … allow for fast add/remove
operations".

### Roster corrections

- **`mlange-42/arche` is in maintenance and has a successor.** Its README carries the banner "Arche
  has a successor: Ark. If you are new here, use Ark" (**source**,
  https://github.com/mlange-42/arche). **`mlange-42/ark`** is the same author's rewrite, is what his
  benchmark suite now measures, and is the Go design worth studying. Ark ships both `LICENSE-MIT`
  and `LICENSE-APACHE` (dual). Both are covered below.
- **`yohamta/donburi` is MIT, not Apache-2.0** (**source**, its `LICENSE` file). The ticket said
  Apache-2.0.
- **`SanderMertens/ecs_benchmark`**, flecs' own suite, 404s. Numbers attributed to flecs below come
  from the third-party `abeimler/ecs_benchmark`, which flecs' FAQ itself links.

---

## 1. Iteration cost

### 1.1 Rust — `ecs_bench_suite`, the only cross-library sweep with recoverable numbers

The suite publishes violin plots only, but its criterion output is committed to git, so the mean
point estimates are recoverable from `estimates.json` (**bench**,
https://github.com/rust-gamedev/ecs_bench_suite). Figures below are the `master` run:
`bevy_ecs 0.5`, `hecs 0.5`, `legion 0.3`, `shipyard 0.5`, `specs 0.16.1`.

`simple_iter` — 10 000 entities, **one** archetype, 4 components, query over 2:

| Implementation | Layout | ns total | ns/entity |
| --- | --- | ---: | ---: |
| legion | archetype (packed) | 10 304 | 1.03 |
| hecs | archetype | 13 440 | 1.34 |
| bevy | table | 17 654 | 1.77 |
| **shipyard** | **sparse set** | **24 915** | **2.49** |
| specs | bitset + `VecStorage` | 25 058 | 2.51 |

`fragmented_iter` — 26 archetypes × 20 entities, query over **one** component present in all of
them:

| Implementation | Layout | ns total | ns/entity |
| --- | --- | ---: | ---: |
| **shipyard** | **sparse set** | **75** | **0.14** |
| hecs | archetype | 326 | 0.63 |
| legion | archetype (packed) | 635 | 1.22 |
| bevy | table | 1 443 | 2.78 |

**The inversion is the finding.** The same sparse-set implementation is 2.4× *slower* on a dense
2-component query and 4×–19× *faster* on a fragmented 1-component query. Archetype iteration is not
unconditionally fast; it is fast when the matched entities live in few tables.

Bevy's own maintainer figure, independent of this suite: "Table iteration is 2-4x faster than sparse
set iteration at the moment" — alice-i-cecile, **docs/maintainer issue**,
https://github.com/bevyengine/bevy/issues/2144.

### 1.2 C++ — `abeimler/ecs_benchmark`

6 components, 7 small systems, Linux, 3.13 GHz, GCC 14.2.1 (**bench**,
https://github.com/abeimler/ecs_benchmark). "Update 7 systems", wall time:

| Entities | EnTT (views) | EnTT (group) | flecs |
| ---: | ---: | ---: | ---: |
| 1 | 123 ns | 89 ns | 1 741 ns |
| 256 | 7 µs | 4 µs | 4 µs |
| ~1 K | 31 µs | 18 µs | 10 µs |
| ~4 K | 138 µs | 89 µs | 30 µs |
| ~16 K | 569 µs | 403 µs | 120 µs |
| ~262 K | 12 ms | 8 ms | 3 ms |
| ~1 M | 40 ms | 35 ms | 19 ms |
| ~2 M | 85 ms | 87 ms | 42 ms |

flecs' fixed per-query overhead dominates below ~100 entities; the two are level at 256; from 1 K up
the archetype scan wins by 2×–4× on this mixed multi-system workload. EnTT *groups* recover 30–45 %
of that gap up to 262 K and stop helping at 2 M.

flecs' FAQ states the split directly and concedes half of it (**docs**,
https://www.flecs.dev/flecs/FAQ.html): "Multi-component queries are faster in Flecs", "Single
component queries are faster in EnTT". The same page warns: "When doing a benchmark comparison don't
rely on someone else's numbers, always test for your own use case!"

### 1.3 Go — Ark, arche, donburi, unitoftime

Published, 2-component query, ns **per entity**, from `mlange-42/go-ecs-benchmarks` (**bench**,
https://github.com/mlange-42/go-ecs-benchmarks; the README discloses that its maintainer is Ark's
author):

| N | Ark | Ark (tables) | donburi | ggecs | unitoftime |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 1 K | 1.82 | 0.90 | 19.92 | 5.06 | 3.27 |
| 16 K | 1.78 | 0.84 | 21.12 | 5.05 | 3.26 |
| 256 K | 1.77 | 0.83 | 22.89 | 5.09 | 3.20 |
| 1 M | 1.84 | 0.96 | 30.63 | 5.05 | 3.20 |

The suite's own caveat: donburi, ggecs and unitoftime use **callback-based** loops, so "iteration
speed may degrade if the callback contains complex logic and the Go compiler is unable to inline
it". That is a Go-specific tax on API *shape* rather than on layout, and it is worth carrying into
cog's iteration contract.

**measured** (2026-09-11, AMD Ryzen 9 7950X3D, Go 1.27.1, windows/amd64; `ark v0.8.3`,
`arche v0.15.3`, `donburi v1.15.8`, `unitoftime/ecs v0.0.3`; 3-component query over
`float64`-only structs, single archetype, `-benchmem`):

| Implementation | 1 K | 10 K | 100 K | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Ark, entity-wise `Next`/`Get` | 1.023 ns/e | 1.011 ns/e | 1.021 ns/e | **0** |
| Ark, `NextTable`/`GetColumns` | 0.411 ns/e | 0.368 ns/e | 0.351 ns/e | **0** |
| arche, generic `Query3` | — | 2.25 ns/e | — | **0** |
| donburi, `Query.Each` | — | 12.2 ns/e | — | **0** |
| unitoftime, `MapId` | — | 2.06 ns/e | — | **0** |

**Read these as overhead, not throughput.** They are flat from 1 K to 100 K because the working set
stays in this CPU's very large cache; they say nothing about the memory-bandwidth wall a real frame
hits. What they do establish is that **zero allocations per iteration is table stakes in Go** — all
four hit it — and that the column-slice path costs roughly a third of the entity-wise path.

### 1.4 Where the sparse-set probe starts to hurt, and why

Shipyard's guide states the algorithm plainly (**docs**,
https://github.com/leudz/shipyard/blob/master/guide/master/src/going-deeper/sparse-set.md): iterate
the *shortest* dense array, and for each candidate check `dense[sparse[id]] == id` in every other
sparse set. That is **one random-access probe per entity per additional component**. Cost therefore
grows linearly in query *arity* while archetype iteration stays at zero probes regardless of arity.

EnTT documents the same heuristic (**docs**, `docs/md/entity.md`): a multi-type view, on
construction, "look[s] at the number of elements available in each pool and use[s] the smallest set
in order to speed up iterations".

skypjack has worked hard to make the probe cheap and documents each step (**author**, the
*ECS back and forth* series): the sparse array is paged, costing "an indirection to reach the page"
(Part 9, https://skypjack.github.io/2020-08-02-ecs-baf-part-9/); a reserved sentinel removes the
round-trip through the dense array; and Part 13
(https://skypjack.github.io/2021-10-09-ecs-baf-part-13/) folds the entity version into the sparse
slot so a membership test is one bitwise compare with no tombstone branch. cart's statement of the
residual cost (**author**, bevy PR #1525, https://github.com/bevyengine/bevy/pull/1525): sparse-set
iteration "isn't cache friendly, and there is an extra layer of indirection because you must first
map the entity id to an index in the component array".

### 1.5 EnTT groups: a hand-placed archetype, and what it costs to have one

A *group* reorders the packed arrays of two or more pools so the matching entities sit tightly packed
at the front of every owned pool **in the same order** — skypjack's "perfect SoA" (**author**, Part
6, https://skypjack.github.io/2019-11-19-ecs-baf-part-6/): "We know for sure that the i-th element
of each array is assigned to the same entity, no matter what." The docs claim a full-owning group
iterates "as if users are accessing sequentially a bunch of packed arrays of components all sorted
identically, with no jumps nor branches", and "the more types a group owns, the faster it will be to
iterate" (**docs**, `entity.md`).

The price, all from the same two sources, is the reason a group is not simply "archetypes, opt-in":

- **Ownership is exclusive.** A component belongs to at most one non-nested group. `<A,B>` and
  `<A,C>` are *rejected*: coexisting groups must nest, one's component set fully containing the
  other's. **You cannot perfectly optimise two overlapping access patterns.**
- **Non-owning groups are discouraged by the docs themselves**: "users should avoid using non-owning
  groups, if possible" — they need custom data structures and cost memory.
- Owned pools cannot be sorted afterwards, and groups are incompatible with stable (in-place-delete)
  storage — they "even refuse to compile".
- **Structural change on an owned type gets more expensive** (see §2.4).

skypjack's own later verdict on the mechanism is unflattering (**author**, Part 10,
https://skypjack.github.io/2021-02-27-ecs-baf-part-10/): a group "creates an interdependence between
independent pools to induce a sort of table within them", and he criticises the API for hiding which
storage model a given `view<T,U,V>` will actually use.

---

## 2. Structural change cost

### 2.1 The measured gap

Rust, `ecs_bench_suite` `add_remove_component` — 10 000 entities gain then lose one component, so
20 000 transitions (**bench**, same run as §1.1):

| Implementation | Layout | total | ns/transition |
| --- | --- | ---: | ---: |
| specs | bitset | 89 799 ns | ≈ 4.5 |
| **shipyard** | **sparse set** | **120 029 ns** | **≈ 6** |
| hecs | archetype | 794 077 ns | ≈ 40 |
| bevy | table | 1 337 316 ns | ≈ 67 |
| legion | archetype (packed) | 2 425 808 ns | ≈ 121 |

**6.6× (hecs) to 20× (legion)** against the sparse-set case — and this is with a *single* extra
component. hecs documents that `insert`/`remove` cost is "proportional to the number of components
`entity` has" (**docs**, https://docs.rs/hecs). Bevy's 0.5 post deliberately uses a harsher shape:
adding and removing a 4×4 matrix from an entity that already carries five others, "to help
illustrate the cost of 'table storage' … which requires moving the 'other' components to a new
table" (**author**, https://bevy.org/news/bevy-0-5/).

C++, `abeimler` add/remove of one component: EnTT 25 µs vs flecs 236 µs at 1 K; 408 µs vs 3 890 µs at
16 K; 26 ms vs 250 ms at 1 M — **roughly 10×, flat across scale**.

Go, Ark's own published table (**bench**, https://mlange-42.github.io/ark/benchmarks/, generated by
CI on EPYC 7763; memory already allocated):

| Operation | Unbatched | Batched (1000) |
| --- | ---: | ---: |
| new entity, no components | 16.2 ns | 9.5 ns |
| new entity, 5 components | 39.0 ns | 9.8 ns |
| remove entity, 5 components | 61.9 ns | 7.0 ns |
| **add 1 component** | **50.5 ns** | **4.9 ns** |
| add 1 component to an entity with 5 | 117.1 ns | 8.6 ns |
| remove 1 of 5 components | 95.1 ns | 7.8 ns |
| exchange 1 of 5 | 119.8 ns | 8.2 ns |

**Batching is ~10× across the board**, matching the docs' claim that "Batching can speed up
operations by up to an order of magnitude" (**docs**, https://mlange-42.github.io/ark/performance/).
Note the row that matters for §8: adding one component to an entity that already has five costs
2.3× what adding it to an entity with none costs. **The archetype penalty is proportional to what
the entity already carries, not to what you are adding.**

**measured** (same machine and versions as §1.3): Ark steady-state add-then-remove of one component
on 10 000 entities, after archetype capacity is warm — **31.4 ns per operation, 0 B/op, 0 allocs/op**.
Ark create-then-destroy an entity with 2 components — **26.5 ns/op, 0 allocs/op**. donburi's
equivalent — **116.3 ns/op, 96 B/op, 5 allocs/op**.

### 2.2 The archetype graph: every serious archetype implementation has one

The naive cost of an archetype transition is not the row copy — it is *finding the destination*.
cart describes the pre-graph path in bevy (**author**, PR #1525): allocate a vector of the entity's
component ids, add or remove one, sort it for order-independence, hash it to look up the archetype,
"and that's all before we get to the *already* expensive full copy of all components to the new
table storage".

The graph makes archetypes nodes and add/remove-of-one-component the edges, caching each transition
after its first use. Attribution runs Mertens → gjroelofs → cart, stated by cart in that PR.

- **flecs** originated it in this lineage (**author**, "Building an ECS #3: Storage in Pictures",
  https://ajmmertens.medium.com/building-an-ecs-storage-in-pictures-642b8bfd6e04).
- **bevy** adds **bundle edges**: bundles get densely packed ids, so a multi-component add traverses
  *one* edge rather than N. cart: "an operation that used to be *heavy* (both for allocations and
  compute) is now two dirt-cheap array lookups and zero allocations."
- **arche and Ark** both maintain one, and publish the payoff as a number worth stealing (**docs**,
  https://mlange-42.github.io/ark/architecture/ and
  https://mlange-42.github.io/arche/background/architecture/): "Transitions are stored in the nodes
  with lookup approx. **10 times faster than Go's `map`**." Archetypes materialise only for nodes
  that actually receive entities; traversed-but-unpopulated nodes hold no storage. The docs state
  the graph "stabilizes quickly", after which a structural change is pure edge-following.

### 2.3 Deferral is universal, and its stated motive is usually *safety*, not speed

Every implementation surveyed offers a command buffer, and the primary documented reason is that you
cannot structurally mutate while iterating — the batching win is secondary.

- **flecs** — `ecs_defer_begin`/`ecs_defer_end`; inside systems "operations are automatically
  deferred and merged at the end of the frame" (**docs**, https://www.flecs.dev/flecs/Manual.html).
  Entity ids are handed out immediately even when creation is deferred; operations on entities
  deleted during the window are silently dropped.
- **bevy** — `Commands`: "Since each command requires exclusive access to the `World`, all queued
  commands are automatically applied in sequence when the `ApplyDeferred` system runs" (**docs**,
  `crates/bevy_ecs/src/system/commands/mod.rs`). Sync points are inserted by default between stages
  and between systems with an explicit dependency where the upstream defers. `ParallelCommands`
  exists for use inside `par_iter`.
- **hecs** — `CommandBuffer`, applied at `run_on(&mut world)`; pair with `World::reserve_entity` when
  the handle is needed immediately. hecs also has `World::exchange`, which fuses remove-then-insert
  and "skips the intermediate archetype" — **one row move instead of two**, a trick worth noting.
- **legion** — systems take `&mut CommandBuffer` and the schedule applies it.
- **Ark / arche** — batch APIs rather than a buffer, with the 10× shown above.

### 2.4 The EnTT caveat: a group inverts EnTT's own advantage

This is the single most important asterisk on "sparse sets make structural change cheap". From
EnTT's docs (**docs**, `entity.md`): "All groups affect to an extent the creation and destruction of
their components. This is due to the fact that they must *observe* changes in the pools of interest
and arrange data *correctly* when needed for the types they own." Every add or remove of an owned
component fires the group's callback and performs swaps in *every* owned pool to keep the group
boundary correct.

skypjack's own framing (**author**, EnTT v3.0.0 release notes and Part 6): groups "trade more
performance during iterations with the invocation of a callback or such during the creation and
destruction of components"; it is "still faster than many of the other solutions when it comes to
adding or removing components but it's something to take in consideration anyway". And a warning
about measuring it: "It's not something one can easily observe in a real world case."

**So the honest statement of the C++ trade is not "EnTT is cheap to mutate and flecs is cheap to
iterate". It is: EnTT is cheap to mutate *until* you buy back iteration speed with a group, at which
point you have a hand-placed archetype with archetype-like mutation costs and an exclusivity
constraint archetypes do not have.**

### 2.5 The stated rules of thumb

- **bevy**, canonical wording, repeated in the 0.5 blog and PR #1525 (**author**): "By default Query
  iteration is fast. **If developers know that they want to add/remove a component at high
  frequencies, they can set the storage to 'sparse set'**." Current rustdoc is the same rule tersely:
  Table is "optimized for query iteration", SparseSet "optimized for component insertion and
  removal".
- **flecs**, since v4.0/v4.1 (**docs**, https://www.flecs.dev/flecs/ComponentTraits.html): the
  `Sparse` trait stores a component "outside of tables, which means they do not have to be moved",
  explicitly trading "query speed for component add/remove speed". Mertens' summary (**author**,
  https://ajmmertens.medium.com/flecs-4-1-is-out-fab4f32e36f6): **pick per component, not per
  library.**
- **Mertens on what must never live in fragmenting storage** (**author**, "Why storing state
  machines in ECS is a bad idea",
  https://ajmmertens.medium.com/why-storing-state-machines-in-ecs-is-a-bad-idea-742de7a18e59):
  "frequently adding/removing tags associated with states gets very expensive as all components for
  an entity need to be copied for each state transition" — and he notes it is effectively **two
  copies per component** in flecs' storage.

### 2.6 Contested: bevy maintainers now doubt their own rule of thumb

Bevy Discussion #19164, "Consider Removing Sparse Set Component Storage" (**docs/maintainer
discussion**, https://github.com/bevyengine/bevy/discussions/19164), argues the rule is largely
wrong in practice: "the insert and remove cost is not zero! … if you don't need insert/remove speed,
`Table` is way better, and if you are inserting/removing enough that `Table` isn't worth the
iteration speed, even `SparseSet` is also too slow". The observed best answer in some real cases was
a *table* component holding `Option<MyActualComponent>`. The proposal is to drop sparse sets and add
a `MaybeTable` storage — an occupancy bitmask on the existing column — which would also take entity
metadata from O(entities × sparse-set component types) down to O(entities).

**Not adopted.** But it is the strongest public evidence available that **supporting two layouts
carries a large and ongoing complexity tax**, which is directly relevant to cog#239 and to the map's
"third-party storage strategies are out of scope" line.

---

## 3. Memory, and archetype fragmentation

### 3.1 bevy's archetype/table separation — the most transferable idea in this note

Easy to get backwards, so quoted exactly (**docs**, `crates/bevy_ecs/src/archetype.rs` module docs):

> "Archetypes are not to be confused with `Table`s. Each archetype stores its table components in
> one table, and **each archetype uniquely points to one table, but multiple archetypes may store
> their table components in the same table. These archetypes differ only by the `SparseSet`
> components.**"

cart (**author**, PR #1525): "Archetypes are now 'just metadata' … they no longer store components
directly." An archetype records its component ids and their storage types, its `TableId`, its entity
list with each entity's table row, and its graph edges. The relation is **one table → many
archetypes**.

Why this answers part of fragmentation: an entity with table components `[A,B,C]` plus sparse
`[D,E]` shares the **same table** as one with `[A,B,C]` plus sparse `[F]`. **Adding a sparse-set
component creates a new archetype, creates no new table, and moves no component data.** cart: "This
1→Many relationship is how we preserve fast 'cache friendly' iteration performance when possible."

Each marker moved to sparse storage removes one *dimension* from the table space. The `2^n` blow-up
still happens in archetype metadata; the tables — the things iteration walks — stay coarse.

Mertens' rebuttal is worth recording beside it (**author**, flecs 4.1 post): bevy's sparse components
*still* change an entity's archetype, so they do not avoid fragmentation in the metadata; flecs 4.1's
`DontFragment` trait is his answer, and he claims flecs is "the first open source ECS that supports
both".

Costs bevy pays, stated in its own docs: **archetypes and tables leak.** "Like tables, archetypes
can be created but are never cleaned up. **Empty archetypes are not removed, and persist until the
world is dropped**" (**docs**, `archetype.rs`). Bevy carries dedicated benchmarks for this pathology
(`empty_archetypes.rs`, `fragmentation.rs` — the latter builds ~4096 archetypes from 65 536 entities
with random marker combinations and then queries for something that matches almost none of them).

### 3.2 flecs on table explosion

Acknowledged in the official docs (**docs**, https://www.flecs.dev/flecs/Relationships.html):
"Applications that make extensive use of relationships might observe high levels of fragmentation,
as relationships can introduce many different combinations of components" — tempered by "the Flecs
storage is optimized for supporting large amounts (hundreds of thousands) of tables". The roadmap
line in the same file: "Improvements are planned to decrease fragmentation overhead introduced by
relationships."

Mitigations, in the order they arrived:

- **Union relationships** — many targets of one relationship in a single archetype via external
  switch lists, at query-time cost (**author**, Storage in Pictures).
- **Relationship flattening** (v3.2, experimental) — "stores entities with different parents in the
  same table, which can significantly reduce memory fragmentation". The worked example cut a test
  scene from **46 000 tables to 960** (**author**,
  https://ajmmertens.medium.com/flecs-3-2-is-out-8feb44d37e3). This is the most concrete
  fragmentation number found anywhere.
- **`DontFragment`** (v4.1) — sparse *and* non-fragmenting, motivated in the docs precisely for
  rare components, which "would otherwise result in many tables that only contain a small number of
  entities" (**docs**, `ComponentTraits.md`).
- **Empty-table cleanup** — cached queries keep empty archetypes separate from non-empty; apps can
  opt out of the event traffic and periodically call `ecs_delete_empty_tables` (**docs**, Queries
  manual).

skypjack's version of the same risk (**author**, Part 2,
https://skypjack.github.io/2019-03-07-ecs-baf-part-2/): "the number of archetypes could explode if
you have a high number of possible combinations of components assigned to different entities at
runtime."

### 3.3 Go: per-entity overhead, and a preallocation default that is a fragmentation multiplier

Ark (**source**, `ecs/entity.go`, `ecs/table.go`): `Entity` is `{id, gen uint32}` = 8 B in the
table's entity column; `entityIndex` is `{table tableID, row uint32}` = 8 B in a world-level slice.
**≈ 16 B per entity of pure bookkeeping**, plus the recycle list. `World.Stats()` confirms: an
archetype with two 8-byte components reports 24 B/entity.

Both worlds preallocate per-archetype capacity and grow by **doubling**. Defaults (**source**,
`ecs/config.go`): **arche 128**, **Ark 1024** (128 for relation archetypes).

Ark is honest about the consequence in its own docs (**docs**,
https://mlange-42.github.io/ark/stats/): a worked example of 100 entities with 2 components reports
"Memory: 4.7/56.0 kB" — 12× over-reservation, because every table is born at capacity 1024.

**measured**, pushing it deliberately: 512 entities spread over **257 archetypes** (8 optional
markers → 256 combinations), all carrying `Position`:

| World initial capacity | Memory used | Memory reserved | After `World.Shrink()` |
| ---: | ---: | ---: | ---: |
| 1024 (default) | 52 kB | **22 552 kB** | 22 552 kB (unchanged) |
| 8 | 52 kB | 186.6 kB | 186.6 kB (unchanged) |

**434× over-allocation at the default**, and `Shrink` recovered nothing — exactly as documented,
since capacity is "never below the initial capacities specified during world initialization"
(**source**, `ecs/world.go`). **The fragmentation cost of an archetype layout is dominated by the
per-table preallocation constant, not by the per-table metadata.** If cog preallocates per table,
the default must be small or adaptive.

Other Go implementations, for contrast (**source**):

- **arche has no `Shrink` at all** — memory only grows.
- **Ark's `World.Shrink(stopAfter ...time.Duration)`** shrinks table capacity to the next power of
  two above occupancy and frees empty relation tables, **with a time budget so it can be amortised
  across frames**, returning `true` if work remains. Its doc comment also says "This method should
  not be used regularly!" Directly relevant prior art.
- **Normal archetype tables are never recycled** in either arche or Ark: "Normal archetype tables
  without a relation are never removed, because they are not considered temporary" (**docs**, Ark
  Architecture). Relation tables are deactivated, keep their memory, and are reused for a different
  target.
- **`unitoftime/ecs` fragments *within* a table**: deletion pushes the row onto a `holes []int` list
  instead of swap-removing, and the iteration loop pays `if ids[idx] == InvalidEntity { continue }`
  per entity forever. Tables never compact.
- **ggecs** keeps a 256-entry `Storage` *interface* array per archetype (~4 KiB of pointers before
  any data) and grows storage in steps of `StorageBufferIncrementBy = 10000`.

### 3.4 legion's chunks, and what they wasted

legion 0.2.4 (**source**, `legion-core-0.2.4/src/storage.rs`): `MAX_CHUNK_SIZE = 16 * 1024 * 10` =
**163 840 bytes (160 KiB)**, `COMPONENT_STORAGE_ALIGNMENT = 64`, and chunk entity capacity =
`max(1, MAX_CHUNK_SIZE / max(largest_component_size, sizeof(Entity)))`.

So a legion chunk is **not** a fixed 16 KiB total like Unity's. It sizes the *widest column* to
≤ 160 KiB and allocates every other column at that same entity capacity, each 64-byte aligned.
Worked example: components of 4 B, 4 B and 64 B → capacity 2 560 entities → ≈ 200 KiB per chunk.
`ComponentStorage::allocate()` takes the whole layout in one call, so **a chunk holding one entity
holds the whole ~200 KiB**. That granule multiplied by every (archetype × tag-value) combination.

legion 0.3 replaced it with **packed archetypes** — per-archetype contiguous slices, incrementally
defragmented across archetypes. The cost model is stated directly (**docs**,
`legion-0.4.0/src/storage.rs`):

> "One of the disadvantages of archetypes is that there are discontinuities between component arrays
> of different archetypes. **In practise this causes approximately one additional L2/3 cache miss
> per unique entity layout that exists among the result set of a query.**"

`PackOptions` defaults (**source**): `stability_threshold: 120` frames,
`fragmentation_threshold: 1/64` (one saved cache miss per 64 entities),
`maximum_iteration_size: 4 * 1024 * 1024` entities moved per repack. `WorldOptions { groups }` lets
an app declare component groups that are "frequently queried for together", with implicit
sub-groups.

The payoff is the `fragmented_iter` row: **1 786 ns → 509 ns, a 3.5× improvement over legion 0.2.4
on the same machine**, with `simple_iter` unchanged (13 456 → 13 419 ns) (**author**, TomGillen's
legion 0.3 announcement, https://gist.github.com/TomGillen/8c9ed5dabf5d4f6b3c517d342d2c5a62, plus
the committed criterion estimates).

### 3.5 specs: per-component storage choice, years before bevy

The specs book's "Storages" chapter (**docs**,
https://github.com/amethyst/specs/blob/master/docs/tutorials/src/05_storages.md) made storage a
per-component decision and stated the density rule that governs it:

| Storage | Mechanism | For |
| --- | --- | --- |
| `VecStorage` | one sparse `Vec`, gaps uninitialised | very common components |
| `DenseVecStorage` | data `Vec` + entity→index redirection | "useful when your component is bigger than a `usize` because it consumes less RAM" |
| `HashMapStorage` | hash map | "components which are associated with very few entities" |
| `BTreeStorage` | B-tree | "the default … in case you're not sure" |
| `NullStorage` | ZST; state lives in the mask | flagging entities |

`VecStorage` wastes `sizeof(T)` per absent entity and is "a waste of memory to use … for rare
components"; `DenseVecStorage` pays one `usize` per entity instead. Presence lives in a hierarchical
bitset (4 layers, each upper bit summarising a range below, so empty ranges skip wholesale).

---

## 4. Chunking

**Input to [cog#242](https://github.com/dvoyni/cog/issues/242), which owns the feature.**

### 4.1 Unity DOTS — the ancestor, and the only one that gets the full payoff

**docs**, https://docs.unity3d.com/Packages/com.unity.entities@1.3/manual/concepts-archetypes.html:

> "Each chunk consists of **16 KiB** and the number of entities that they can store depends on the
> number and size of the components in the chunk's archetype."

Packing is tight and order-free: entity 0 at index 0, and on removal "the last entity of the chunk is
moved to fill the gap" — swap-remove. Worked arithmetic from the 0.17-era manual: 92 B of components
plus the 8 B entity id = 100 B/entity → 16 384/100 ≈ **163 entities per chunk**.
`MaximumChunkCapacityAttribute` can cap this per component type.

Two things chunks buy, both at chunk granularity and neither available without a fixed granule:

1. **Job scheduling.** An `IJobChunk`'s `Execute()` is invoked "once for each chunk that matches the
   entity query", and `ScheduleParallel()` runs those invocations concurrently (**docs**, Unity
   `chunk_iteration_job` manual). A chunk is a disjoint unit of work by construction — no range
   splitting, and the layout's alignment is the false-sharing guard.
2. **Change detection.** "For efficiency, the change version applies to whole chunks not individual
   entities. If another job which has the ability to write to that type of component accesses a
   chunk, then ECS increments the change version" — with the honest caveat that this happens "even
   if the job that declares write access to a component does not actually change the component
   value" (**docs**, Unity version-numbers manual). This is the mechanism `Changed[T]` in cog's
   "Not yet specified" list would most naturally use.

Shared components (`ISharedComponentData`) subdivide an archetype's chunks by shared value — Unity's
fragmentation lever, and the thing legion ported as `Tag`.

### 4.2 legion adopted it, then removed it

The legion book states the lineage outright (**docs**): "Legion's internal architecture is heavily
inspired by the new Unity ECS architecture, while the publicly facing API is strongly built upon
specs." Structure in 0.2.4 was `Archetype → Chunkset (one per unique tag-value combination) →
Chunk`.

What chunks bought, per the Amethyst RFC that argued for adopting legion (**docs/RFC discussion**,
https://github.com/amethyst/rfcs/issues/22): "Legion guarantees allocation of similar entities into
contiguous, aligned chunks with all their components in linear memory"; systems could be scheduled
"on a per-chunk basis"; and "Queries in legion store filter and change state, allowing for extremely
granular change detection on a **Archetype, Chunk and Entity** level".

**Both chunks and tags are gone in 0.3+.** `legion 0.4.0`'s source contains zero occurrences of
`tag`, and `chunk` survives only as `ChunkView`/`iter_chunks` — a *view* yielding one archetype's
component slices, introduced in the 0.3 announcement under the heading "Archetype Iteration"
(**source** + **author**). Change detection consequently coarsened from per-chunk to
**per-(archetype, component type)**: `PackedStorage` holds `versions: Vec<UnsafeCell<u64>>` described
as "Ordered archetype versions", surfaced as the `maybe_changed::<T>()` filter, which the 0.3 blog
calls "cheap **course-grained** change tracking".

**Gap.** No authored rationale for removing tags or chunks was found; the only primary trace is a
user on the merge issue saying "(I still think the loss of tags is unfortunate)"
(https://github.com/amethyst/legion/issues/169). The structural inference — a 160 KiB minimum
granule per (archetype × tag value) works directly against the packed-archetype defragmentation 0.3
was built around — is *inference*, not a cited claim.

### 4.3 Nobody else chunks — and they get parallelism anyway

- **flecs**: each table column is a single contiguous allocation, `realloc`'d on growth (**source**,
  `src/storage/table.c`). Confirmed by the public API: `ecs_table_size` reports "the number of
  elements allocated in the table per column". Component pointers are therefore invalidated by table
  growth, which is exactly why the `Sparse` trait exists as the pointer-stability escape hatch.
  Parallelism splits by **table slice**: with 4 threads and a 1000-entity table, thread 1 gets rows
  0–249 and so on, and "the same entity is always processed by the same thread, until the next sync
  point" (**docs**, https://www.flecs.dev/flecs/Systems.html).
- **EnTT**: the *component* storage is paged — `ENTT_PACKED_PAGE = 1024` (**source**,
  `src/entt/config/config.h`) — but the reason is pointer stability, not cache-sized work units.
  skypjack (**author**, Part 12, https://skypjack.github.io/2021-08-29-ecs-baf-part-12/): on growth
  "moving the components means to only move the pointers to the component pages", at the price of
  "a few extra jumps" per iteration, which he calls negligible. The sparse array is separately paged
  at `ENTT_SPARSE_PAGE = 4096`. EnTT ships no scheduler; parallelism is the user's.
- **bevy**: no blocks. `par_iter` synthesises the granularity (**docs**,
  `crates/bevy_ecs/src/batching.rs`): "A parallel query will **chunk up large tables and archetypes
  into chunks of at most a certain batch size** … By default, this batch size is automatically
  determined by **dividing the size of the largest matched archetype by the number of threads
  (rounded up)**", with the stated assumption "that each entity has roughly the same amount of work
  to be done, which may not hold true in every workload". `batches_per_thread` is the tuning knob;
  `BatchingStrategy::fixed(n)` pins it.
- **hecs**: "a dense, contiguous array for each type of component … effectively a columnar database"
  (**docs**, README). No chunks.
- **No Go implementation chunks entity storage.** Verified in source for Ark, arche, donburi and
  unitoftime: every one is a single contiguous growable array per (archetype, component). Two
  near-misses not to confuse with chunking: arche's `layoutChunkSize = 16` grows the per-archetype
  *column-descriptor* array; Ark's `pagedSlice[T]` with 32-element pages gives pointer stability to
  *archetype metadata*.

### 4.4 What the Go ecosystem does instead, and what that means for cog

- **Ark exposes the column directly.** `NextTable()` + `GetColumns()` hands back real Go slices for
  the whole table, documented as "about two times as fast" than entity-wise iteration (**docs**,
  https://mlange-42.github.io/ark/queries/); **measured** here at 2.8×–2.9× (1.02 → 0.36 ns/entity).
  That is the chunk-iteration ergonomic without chunks, **and it is the natural seam for
  parallelism: a table is a slice, so a range of it is a subslice.**
- **Ark's parallelism story is manual and explicit** (**docs**, Ark queries and design docs): "Ark in
  general is not thread-safe… This design choice avoids internal locking mechanisms, which would
  introduce overhead and complexity", but "Concurrent query execution is yet possible if the queries
  don't access the same entities concurrently". The two sanctioned patterns are parallel systems
  over disjoint archetypes, and partitioning *within* one archetype using entity relations so each
  relation sub-table is a parallel unit. The docs name the hazard: the user owns avoiding "data
  races and performance degradation due to false sharing".
- **`unitoftime/ecs` is the only Go one with a built-in parallel map.** `View{N}.MapIdParallel`
  (**source**, `view_gen.go`) spawns `runtime.NumCPU()` goroutines over an *unbuffered* channel and
  greedily slices each archetype's component slices into contiguous `[start:end]` subslices — chunk
  size decided per call rather than baked into layout. One channel send per work item is a
  synchronisation point, so it suits coarse items only.
- **donburi actively pessimises the single-threaded path for a concurrency model it does not have**:
  each `Archetype` embeds a `sync.Mutex`, and `Query.Iter` **unlocks and relocks around every single
  `yield`** to the caller's callback (**source**, `internal/storage/archetype.go`, `query.go`). Two
  mutex operations per entity per query. A cautionary example for cog's iteration contract.

### 4.5 The one line to carry into cog#242

**Chunking is not required to parallelise, and every implementation that dropped it still
parallelises — by splitting a contiguous column into row ranges.** What chunking uniquely buys is a
*stable, pre-existing* unit of work with a natural identity, which is what makes per-chunk change
versions possible. The choice that forecloses chunk-parallel execution is not "did you chunk" but
**"does the iteration contract expose a table's rows as an addressable range, or only one entity at
a time"**. Ark's `GetColumns` and flecs' table slices keep it open; a callback-per-entity API like
donburi's closes it.

There is also a frame-time argument nobody in the Go ecosystem makes. Growing a contiguous column
means "allocate 2× and copy everything"; growing a chunked one means "allocate one more fixed-size
block". Amortised throughput is similar, but the **spike profile** is not — and both published Go
benchmark suites carefully mark every structural-change row "memory already alloc.", i.e. **the
published numbers exclude growth entirely.** Genuinely open space.

---

## 5. Empty and tag components

**Every mature implementation stores a zero-sized component as zero bytes.** The interesting
differences are in what a tag costs *structurally*.

- **EnTT — the empty-type optimisation (ETO)** (**docs**, `entity.md`). For `std::is_empty_v<T>` the
  type "is not instantiated by default. Therefore, only the entities to which it is assigned are
  made available. There does not exist a way to *get* empty types from a storage or a registry."
  Consequences the docs state: "iterations are faster because only the entities to which the type is
  assigned are considered. Moreover, less memory is used, mainly because there does not exist any
  instance of the component." The storage degenerates to a bare sparse set of entity ids. Opt out
  with `ENTT_NO_ETO` or a non-zero `page_size` in `component_traits<T>`. **Cost of a tag in EnTT:
  one extra sparse set, zero effect on any other component's layout — unless the tag is owned by a
  group, in which case adding or removing it reshuffles every owned pool.**
- **flecs — tags are first-class and distinct from ZSTs** (**docs**, `Quickstart.md`): "a tag is a
  component that does not have any data … either empty types (in C++) or regular entities that do
  not have the `EcsComponent` component". In the table, a tag appears in the (sorted) component-id
  array but gets **no column**; a column map records which ids lack one, so a tag-only transition is
  constant-time (**author**, Storage in Pictures; corroborated by the Tables API note that "the
  column array does not include elements for tags"). **But a tag is still a table dimension.**
  Adding one is a full archetype move of every real component the entity has. That is the entire
  thesis of Mertens' state-machine post: with orthogonal state machines, "each combination of states
  can cause a combinatorial explosion of tables". `DontFragment` is the explicit remedy, with the
  caveat that such components "don't show up in types".
- **specs — `NullStorage`**, purpose-built (**docs**, the book): "the `NullStorage` does itself only
  contain a user-defined ZST … Because it's wrapped in a so-called `MaskedStorage`, insertions and
  deletions modify the mask, so it can be used for flagging entities." **One bit in the hibitset and
  nothing else** — structurally the cheapest marker representation of anything surveyed, and a
  direct consequence of specs not being archetypal at all.
- **bevy — no tag concept; a marker is a ZST `Component`**, and markers are the canonical sparse-set
  case in the community guidance. **But the canonical case is broken, officially and still open.**
  Issue #2144 (**docs/maintainer issue**): "if a query has *any* sparse set components, we must use
  sparse set iteration" — so `Query<&Dense, With<Sparse>>` drops off the fast dense path even though
  every byte it returns lives in a table. alice-i-cecile: "This is a common and natural pattern,
  especially if you're using sparse-set marker components, so fixing this problem would help ensure
  that sparse-set components actually accomplish their promised perf benefits in real
  applications." cart's reply explains why it is not trivially fixable: the *filter's* data is not
  in the table, so you would have to either put it there (pushing cost back into archetype moves) or
  look up the archetype per row. Attempted fix PR #4800 was unsound. **Treat "make your markers
  sparse-set" as a claim requiring measurement in your own query mix.**
- **legion** had real tags (tag *values*, partitioning an archetype into chunksets — Unity's
  `ISharedComponentData` ported) and **removed them in 0.3**. After removal a marker is an ordinary
  ZST component, so toggling one is a full archetype transition — and archetype transitions are
  legion's *worst* benchmark (§2.1, slowest of six). A real regression for high-frequency markers.
- **Go — arche and Ark handle `struct{}` at literally zero per-entity cost, and there is a
  Go-specific reason the technique works.** **measured**, 1000 entities, Ark `World.Stats()`:
  archetype `[Position, TagZ]` where `TagZ = struct{}` reports **24 B/entity**; archetype
  `[Position]` alone reports **24 B/entity**. Identical. Both codebases special-case `itemSize == 0`
  at every site that would do pointer arithmetic — arche's `archetype.Remove`/`Zero`/`ZeroRange`/
  `extend`, Ark's `column.Set`/`Zero`/`ZeroRange` (**source**). This matters because **in Go every
  zero-size allocation returns the same address, `runtime.zerobase`**, so the slice header is
  non-nil with no backing storage and `unsafe.Add(ptr, index*0)` is always the same address. The
  happy consequence: because the pointer is non-nil, the "does this archetype have component X?"
  check still works via the column pointer (arche's `HasComponent` tests
  `getLayout(id).pointer != nil`), so **tags need no separate representation at all**.
- **Go — donburi does the opposite, and it is the cautionary example.** `donburi.NewTag()` returns
  `*ComponentType[Tag]` where **`type Tag string`** (**source**, `tag.go`). A Go `string` header is
  **16 bytes and contains a pointer**. Every donburi "tag" is therefore a 16-byte, GC-scanned,
  separately heap-allocated object per entity.

---

## 6. Query resolution

**"At least these components" is universal, and its cost is per-*archetype*, not per-entity.** Every
archetype implementation matches an archetype iff its component set is a superset of the query's
required set — hecs' README: "a fast linear traversal can be made through each group having a
**superset** of those components". The failure mode is therefore not slow iteration but a re-test of
every archetype on every query run, growing without bound as archetypes proliferate.

Four languages converged on the same answer: **cache the matching set, and let the only invalidation
event be "a new table was created".**

### 6.1 bevy `QueryState` — a generation *cursor*, not a dirty flag

cart's list of what is cached (**author**, PR #1525): "(1) **Cache archetype (and table) matches** —
this resolves another issue with (naive) archetypal ECS: query performance getting worse as the
number of archetypes goes up (and fragmentation occurs). (2) **Cache Fetch and Filter state** — the
expensive parts … (such as hashing the `TypeId` to find the `ComponentId`) now only happen once when
the Query is first constructed. (3) **Incrementally build up state** — when new archetypes are
added, we only process the new archetypes."

Mechanism (**source/docs**, `crates/bevy_ecs/src/query/state.rs`): two `FixedBitSet`s
(`matched_tables`, `matched_archetypes`) plus `archetype_generation: ArchetypeGeneration`, a
monotonic counter on `World::archetypes()`. The update iterates **only**
`&archetypes[old_generation..]`. **Archetypes are append-only and never removed, which is exactly
what makes a generation *index* a sound cursor rather than a hash or a dirty set. Nothing
"invalidates" the cache; new archetypes only append and the cursor advances.**

A modern refinement worth stealing: when the query has required components, bevy skips the linear
scan of new archetypes entirely — it takes the world's `component_index`, picks the required
component with the **fewest** archetypes (`min_by_key(ExactSizeIterator::len)`), and visits only
those ids.

And the fork that §5 breaks: `QueryState` carries `is_dense: bool`, computed at construction, and
iteration walks a `union StorageId { table_id, archetype_id }` — "Query iteration is **exclusively
dense (over tables) or archetypal (over archetypes)** based on whether the query filters are dense or
not … This removes the need for discriminator to minimize memory usage and branching during
iteration" (**docs**, `state.rs`).

Measured payoff, stated qualitatively in the 0.5 blog (**author**): on a query matching 5 entities in
one archetype while 100 other archetypes do not match — "a reasonable test of 'real world' queries in
games, which generally have many different entity 'types', most of which *don't* match a given
query" — "Bevy 0.4 needs to check every archetype each time the iterator is run, whereas Bevy 0.5
amortizes that cost to zero". Separately, `Query::for_each` gave "~1.5-3x iteration speed
improvements for 'fragmented iteration', and minor ~1.2x improvements for unfragmented iteration".

### 6.2 flecs — cached by default for systems, uncached for ad-hoc

- The base mechanism is a **component index** mapping each component id to the archetypes containing
  it, so a query can be evaluated with no cache at all. Mertens (**author**): "The number of
  components is always way less than the number of archetypes, so this reduces the overhead by a
  lot!"
- A **cached** query stores the prematched archetype list: "a query can instead cache the list of
  matching archetypes. This is a cheap cache to maintain" because "games typically only use a finite
  set of component combinations", and "iterating a cached query just means iterating a list of
  prematched results, and this is really, really fast" (**docs**,
  https://www.flecs.dev/flecs/Queries.html). `ecs-faq` puts the asymptote on it: "as tables stabilize
  quickly, query evaluation overhead is reduced to zero on average."
- **Invalidation is archetype creation and deletion**, and it is not free: "Cached queries add
  overhead to archetype creation/deletion, as these changes have to get propagated to caches."
  Cached queries additionally partition matched archetypes into empty and non-empty, which is more
  event traffic; opt out and call `ecs_delete_empty_tables` periodically instead.
- Cache kinds: `Default` (cached iff the query is attached to an entity, so **systems are cached and
  ad-hoc queries are not**), `Auto`, `All`, `None`. Components, tags, pairs, `$this`-source terms,
  wildcards and traversal are cacheable; query *variables* are not.
- FAQ pitfall worth repeating in cog's spec: queries are "the fastest way to iterate over entities,
  but are expensive to create" — never build one inside a system body.

### 6.3 EnTT — views are free, groups are eager

A view holds references to the relevant storages and does no matching step at all: "Creating and
destroying views is not expensive at all since they do not have any type of initialization";
"Storing aside views is not required as they are extremely cheap to construct" (**docs**,
`entity.md`). **There is nothing to invalidate**, because resolution happens per iteration by
picking the smallest pool and probing the rest. `entt::exclude<T>` is the negation;
`view::use<T>()` overrides the smallest-pool heuristic when deterministic order is wanted.

A group is the eager alternative: matching is precomputed into the pools' physical layout and kept
correct by observers on every create/destroy of an owned type (§1.5, §2.4). Resolution at iteration
time is then zero — paid for continuously.

skypjack's stated direction of travel is a **multi-type storage**, which he describes as sitting
"halfway between groups (the limitations of which it overcomes) and archetypes (the issues of which
it solves hopefully)" (**author**, EnTT Discussion #855). **Gap:** how much of that shipped, and how
it resolves queries, was not verified.

### 6.4 hecs — caching arrived late and is deliberately understated

From the CHANGELOG (**docs**): 0.6.0 added an opt-in `PreparedQuery`; **0.11.0** made it implicit —
"Set-up work is now automatically cached for most uses of queries, dramatically improving the
performance of small queries in worlds with many archetypes". The current `PreparedQuery` rustdoc
now actively discourages its use: "Prepared queries are less convenient and usually do not measurably
impact performance."

The public invalidation token is `World::archetypes_generation()`, and hecs is honest that it is
*conservative*: it "may be, but is not necessarily, changed as a result of adding or removing any
entity or component" — weaker than bevy's generation-as-cursor. hecs 0.5.0's changelog records a bug
where it was *not* updated by a column batch spawn that introduced a new archetype, which is evidence
this is the real path.

### 6.5 Go

- **arche** evaluates `filter.Matches(&arch.Mask)` against **every archetype on every iteration** by
  default — one bitmask test each, but linear in archetype count. The doc comment on `Cache`
  (**source**, `ecs/cache.go`) gives the threshold, and it is the most directly useful number in this
  section: "**If the number of archetypes exceeds approx. 50-100, uncached filters experience a
  slowdown. The relative slowdown increases with lower numbers of entities queried (noticeable below
  a few thousand entities).**" `World.Cache().Register(filter)` returns a `CachedFilter` holding a
  materialised archetype slice; **invalidation is incremental and additive** — `Cache.addArchetype`
  runs on archetype creation and tests that one archetype against every registered filter.
  `removeArchetype` exists only for relation archetypes. Expressiveness: `All`, `Without`,
  `Exclusive` (exactly these and no more), plus `With`/`Optional` on the generic side and
  `And`/`Or`/`Not` combinators.
- **Ark** adds a **component → archetypes inverted index** (**docs**,
  https://mlange-42.github.io/ark/queries/): "Ark maintains a mapping from each component to the set
  of archetypes that include it. This is used to reduce the number of filter checks by
  **pre-selecting archetypes by the most 'rare' component of a query**." Confirmed in source as
  `storage.componentIndex [][]archetypeID` consumed via `q.rareComp`. **This bounds uncached query
  cost by the population of the rarest requested component rather than by total archetype count, and
  needs no invalidation beyond an append.** It is the same idea bevy reaches via
  `component_index.min_by_key(len)` and flecs via its component index — three independent arrivals
  at the same answer.
  Ark also **deliberately dropped `Optional`**, which arche has: "There is no `Optional` provided, as
  it would require an additional check in `Query2.Get` et al." A pointed datum — the author removed a
  feature because it cost one branch on the hot path.
  **measured**: 1024 matching entities scattered over 256 archetypes plus 256 non-matching — Ark
  uncached **4 373 ns**, cached **2 843 ns**, both 0 allocs/op. Registration buys ~35 % here.
- **donburi** has no bitmask at all: `Index.SearchFrom` walks slices of component types and makes an
  interface call per archetype doing a linear scan per predicate. It is saved only by a watermark
  cache — each `Query` holds `{archetypes, seen int}` and only archetypes with index ≥ `seen` are
  re-matched. Since donburi never removes archetypes, **the cache is append-only and never
  invalidated**. No `Without` and no `Optional` primitive; `Not(Contains(x))` is the idiom.
- **unitoftime** uses a single **global generation counter** bumped on every new archetype; each
  filter caches its archetype list plus the generation it was built at and rebuilds the whole list
  when they differ (**source**, `filter.go`). Coarser than arche/Ark — one new archetype invalidates
  *every* filter — but it reuses the slice, so it is still amortised zero-alloc.

### 6.6 shipyard — the non-archetypal contrast, and a cautionary tale about packing

There is no archetype matching. Query resolution is: pick the shortest dense array, probe the rest.
**There is nothing to cache and nothing to invalidate** — which is precisely why it wins
`fragmented_iter` by 4×–19× and loses `simple_iter` by 2.4×.

Shipyard's escape hatch for the iteration deficit was **packs**, and they were **removed in 0.5.0**:
"Packs are removed temporarily, this implementation had too many limitations. The next implementation
should not have the same problems but requires a fairly big amount of work" (**author**, release
notes, https://github.com/leudz/shipyard/releases/tag/v0.5.0) — with the stated reason that "once a
storage was tightly packed you couldn't tightly pack it again … this could cause issues when you
have a component used often (like `Position`) that you want to pack multiple times".

cart had rejected packing for a general-purpose engine four years earlier, for the same two reasons
(**author**, PR #1525): "(1) 'packs' conflict with each other. If bevy decides to internally pack the
`Transform` and `GlobalTransform` components, users are then blocked if they want to pack some custom
component with `Transform`. (2) users need to take manual action to optimize."

**The exclusivity constraint on EnTT groups, the conflict that killed shipyard's packs, and cart's
prediction are all the same fact.** Any mechanism that reorders one shared component's storage to
suit one access pattern is exclusive, and an engine has more than one access pattern per component.
This is the strongest evidence in the note against the "sparse sets plus opt-in grouping" shape.

---

## 7. Which layout each source recommends, and for which workload

| Source | Recommends | For which workload | Kind |
| --- | --- | --- | --- |
| **skypjack** (EnTT) | Neither, explicitly | Archetypes "work like a charm when the sets of components assigned to the entities don't change much during the runtime". Sparse sets where you want *control*: "not automatic — while archetypes are implicitly generated for users, sparse sets require explicitly deciding what to optimize". | author |
| **Mertens** (flecs) | Archetype **default**, per-component sparse escape | Multi-component queries, bulk creation, destruction → archetype. Components whose membership changes every frame (states, transient flags) → `Sparse`/`DontFragment`. "Pick per component, not per library." | author |
| **cart** (bevy) | Table **default**, sparse-set opt-in | "By default Query iteration is fast. If developers know that they want to add/remove a component at high frequencies, they can set the storage to 'sparse set'." | author |
| **bevy maintainers, 2025** | Possibly **drop** sparse sets | "if you don't need insert/remove speed, `Table` is way better, and if you are inserting/removing enough that `Table` isn't worth the iteration speed, even `SparseSet` is also too slow." Not adopted. | docs/discussion |
| **mlange-42** (Ark/arche) | Archetype, with a graph, batching, and a rare-component index | Go specifically; the whole design is built around making structural change cheap *within* an archetype layout rather than escaping it. | docs |
| **TomGillen** (legion) | Archetype + **incremental defragmentation**, not chunks | Removed chunks and tags in 0.3 in favour of packed archetypes. "approximately one additional L2/3 cache miss per unique entity layout that exists among the result set of a query." | author/docs |
| **specs book** | Per-component, by **density** | `VecStorage` for common, `DenseVecStorage` for large, `HashMapStorage` for rare, `NullStorage` for flags. | docs |
| **Cox et al., CGVC 2025** | Neither | "sparse-set ECSes enable cheaper entity modifications but scale poorly during iteration, while archetypes excel at large-scale iteration through cache efficiency but incur higher composition change costs." C++, Conway's Life, 100–50 000 entities. DOI 10.2312/cgvc.20251224. | paper |

Two meta-observations worth as much as the table:

- **Nobody claims a layout without naming a workload.** skypjack refuses outright — both models "play
  in the same league"; "There doesn't exist a one-fits-all solution"; "measure, measure, measure!"
  (**author**, Parts 2 and 4). His Part 1 goes further and says the primary benefit of ECS is code
  organisation, not performance.
- **`ecs_bench_suite` was archived in November 2022 with its maintainers' own epitaph**: "we
  collectively realized that speed is only one aspect of an ECS, and a rather small one at that once
  a baseline of performance has been established" (**docs**,
  https://github.com/rust-gamedev/wg/issues/130).

---

## 8. What nox's shape implies

Every fact in this section is cited to a nox spec under `nox/docs/specs/`.

### 8.1 The shape, in facts

- **Fixed 30 Hz simulation**: 33.33 ms per tick, containing input, five physics sub-steps and one
  draw (`sim-constants.md` §1). **2D on a plane** — no verticality in the simulation
  (`sim-constants.md`, Units).
- **One universal entity record.** Creature, prop, door, pickup, projectile and light are the same
  schema: class bitmask (32 bits), subclass bitmask, **flags bitmask (32 bits)**, material bitmask,
  plus health, speed, extent, mass, weight, lifetime, light and draw, and a hook list
  (`look-and-feel.md` §4.1).
- **The 32 classes are composable tags, not a hierarchy**, and an object "routinely carries several"
  (`look-and-feel.md` §4.2, stated explicitly).
- **Many of the 32 flags toggle during play**: `DEAD`, `AIRBORNE`, `FALLING`, `IN_HOLE`, `ACTIVE`,
  `ENABLED`, `PENDING`, `SELECTED`, `MARKED`, `EQUIPPED`, `DESTROYED`, `STILL`, `PARTITIONED`
  (spatial-index membership), `NO_UPDATE` (`look-and-feel.md` §4.5). Monsters carry a **second**
  21-entry status bitmask that also toggles — `ALERT`, `INJURED`, `RUNNING`, `MORPHED`, `ON_FIRE`,
  `FRUSTRATED` (`look-and-feel.md` §4.8).
- **Projectiles are short-lived**: a plain projectile lives **40 ticks (1.33 s)**; a homing magic
  missile 90; counterspell 150; death ball 120 (`sim-constants.md` §5). Nearly every projectile's
  collider is a *point* (`sim-constants.md` §3). A cast makes several: magic-missile count by power
  level is `[1,2,3,4,5]` solo, `[2,3,4,5,6]` arena (`sim-constants.md` §9).
- **Projectile population is capped by design.** There are no cooldowns; the limiters are mana, the
  one-instance-per-(spell, caster) rule, and **per-caster live-object caps** — a caster may have only
  so many missiles, traps or summons alive, and "the excess simply not spawned"
  (`spell-system.md`, Cooldowns).
- **Transient status effects are a first-class subsystem, and there are two of them.** A
  live-instance list for duration spells, visited every tick, and a *separate* enchantment list of
  timed modifiers on a unit. The spec's own conclusion: "Nox keeps them separate and so should we"
  (`spell-system.md` §10.3).
- **Level scale is small.** The slice's five-room level is 53 × 30 m with 100–250 occluders; the
  densest measured room had 49 (`vision-and-occlusion.md` §5). Walls are grid data, not entities
  (`sim-constants.md` §4).

### 8.2 The projectile-churn arithmetic, and why the ticket's premise dissolves

At equilibrium, spawn rate equals despawn rate equals `live / lifetime`. With 40-tick projectiles:

| Live projectiles | Spawns/tick | Structural events/tick | Per second at 30 Hz |
| ---: | ---: | ---: | ---: |
| 50 | 1.25 | 2.5 | 75 |
| 200 | 5 | 10 | 300 |
| 1000 | 25 | 50 | 1500 |

Against §2.1's worst archetype figure (legion, ≈ 121 ns per transition) even the implausible
1000-projectile row costs ~6 µs per tick out of 33 330 µs. **The rate is not the problem and never
was.**

**And it is the wrong *kind* of event.** A projectile spawn is entity creation with its **complete**
component set, and a despawn is destruction. In an archetype layout neither moves a row between
tables: creation appends to exactly one table, destruction is a swap-remove per column. The row-copy
cost that makes archetypes expensive is paid by **add/remove of a component on an entity that
already exists**. In nox that means:

- enchantments landing on and expiring from creatures (`Hasted`, `Slowed`, `OnFire`, `Poisoned`,
  `Invisible`, `Invulnerable`, `Shielded`);
- the runtime flag and status toggles above, *if* those become components;
- `DEAD` / `DESTROYED` transitions, equip/unequip, pickup/drop.

Creature counts are tens, not thousands, and enchantment application is a handful per second. **nox's
real archetype-move rate is one to two orders of magnitude below its projectile churn, and its
projectile churn is already negligible.** The ticket asked "how bad does that get"; the answer is
*not bad*, and the reason is about the **kind** of structural change rather than its rate.

One caveat that survives: §2.1's Ark table shows adding one component to an entity with five costs
2.3× what adding it to a bare entity costs. **The archetype penalty scales with what the entity
already carries.** A nox creature is the widest entity in the game — body, model, collider, health,
speed, AI state, inventory, equipment, light — so an enchantment landing on a creature is the most
expensive row move nox will ever perform, even though it is rare.

### 8.3 The combinatorial question is the live one

If each of the 32 class bits, 32 flag bits and 21 monster-status bits becomes a tag component, the
table count is bounded by the number of *combinations actually instantiated* — not `2^85` — but nox's
own text says classes are composable and routinely co-occur, and the flags change at runtime. Three
costs follow, each with a source in this note:

1. **Table count.** Every distinct live combination is its own table. A query matching 400 tables of
   3 entities is a pointer chase, not a scan — this is exactly §1.1's `fragmented_iter` row, where
   the sparse-set implementation beat every archetype one by 4×–19×. arche's own threshold
   (§6.5) puts the onset at **50–100 archetypes**, and says the relative slowdown is *worse* at low
   entity counts — which is nox's regime, not the opposite.
2. **Preallocation.** §3.3's measurement — 512 entities over 257 archetypes reserving 22.5 MB at
   Ark's default capacity, 434× the bytes actually used — is a direct model of the flags-as-tags
   case, and it says the fragmentation cost lands in the *preallocation constant*, not the metadata.
3. **Toggle cost.** `AIRBORNE` going on and off, `ALERT` flipping, `SELECTED` following the mouse —
   each is a full row move, and each is the event that advances every query cache's generation
   (§6.1). This is precisely Mertens' state-machine warning applied to nox.

**A third option the vocabulary makes obvious, and neither layout forces.** nox's flags are *already
bitmasks in the source material*. Keeping `Flags uint32` as one ordinary component keeps the table
count flat and makes a toggle a plain write with **no structural change at all** — at the cost that
a flag stops being a storage-level query filter, so `Without(Dead)` becomes a per-entity branch in
the system body rather than a table-level exclusion. Note that this is close to what bevy's own
maintainers reached for independently (§2.6: a *table* component holding `Option<T>` outperforming
both storages), and close to specs' density rule (§3.5: rare or churning presence does not belong in
a dense layout). **The research does not decide this. It records that nox's shape makes the choice
available, and that the choice is largely orthogonal to archetype-versus-sparse-set.**

### 8.4 The four nox-specific facts that should drive cog#239

1. **Entity counts are in the low thousands, not 100 K.** Every iteration number in §1 is an
   *overhead* measurement at that scale, and the archetype-vs-sparse-set iteration gap (2–2.5×)
   applied to a few thousand entities is microseconds per tick. **Iteration speed is not what
   decides this for nox.**
2. **Query fragmentation is the real iteration risk, and it is created by the design's own
   vocabulary choices, not by the layout.** 32 flag bits as tags puts nox straight into the regime
   where archetype iteration loses (§1.1, §6.5).
3. **The budget is a 30 Hz tick, so the frame-time *spike* profile matters more than throughput.**
   §4.5's unexamined point — contiguous columns grow by "allocate 2× and copy everything" — is the
   spike nox would actually feel, and it is excluded from every published benchmark.
4. **Two of nox's needs are chunk-shaped.** The duration-spell live-instance list and the enchantment
   list are both "visit everything of this kind every tick", and cog's "Not yet specified" list names
   `Changed[T]`. Unity's per-chunk change version (§4.1) is the mechanism that makes change detection
   cheap, and legion lost it when it lost chunks (§4.2). That is an input to cog#242, not an argument
   for chunking.

---

## 9. Cross-cutting observations that did not fit a numbered question

- **Three independent projects arrived at the same query optimisation**: pre-select archetypes by the
  *rarest* requested component. flecs' component index, bevy's `component_index.min_by_key(len)`,
  Ark's `componentIndex` + `rareComp` (§6.2, §6.1, §6.5). None cites the others. It bounds query cost
  by the rarest component's population rather than by total archetype count and needs no invalidation
  beyond an append.
- **Exclusivity kills every "reorder shared storage to suit one query" mechanism.** EnTT groups
  reject `<A,B>` beside `<A,C>`; shipyard removed packs for the same reason; cart predicted it in
  2021 (§6.6). Any cog design that tries to buy archetype-like iteration inside a sparse layout will
  meet this.
- **Append-only archetype sets are load-bearing, and they leak by design.** bevy, arche, Ark, hecs
  and flecs all rely on it for cheap cache maintenance, and bevy and arche both document that empty
  tables are never reclaimed. Ark's time-budgeted `World.Shrink` is the only amortised reclamation
  mechanism found in any of them (§3.3).
- **Callback-shaped iteration APIs cost real time in Go**, per the Go benchmark suite's own caveat
  (§1.3) and donburi's per-`yield` mutex traffic (§4.3). cog's iteration contract should be a range,
  not a callback — which is also what keeps chunk-parallel execution open (§4.5).

---

## 10. Gaps — what could not be established

1. **No published 1k/10k/100k iteration sweep exists in any primary source.** `ecs_bench_suite` fixes
   counts per benchmark (10 000 / 520 / 1 000); bevy's own benches fix 10 000 and 100 000 and publish
   no numbers; `abeimler` has no 10 K point (16 K is nearest). The Go sweep in §1.3 is **measured
   here**, not published. Any curve cog needs must be measured.
2. **`SanderMertens/ecs_benchmark`** — flecs' own suite — 404s. No flecs-authored entities/second or
   ns/entity figure was found; the README's "millions of entities every frame" is the only
   first-party claim.
3. **All three Bevy 0.5 benchmark charts are rasterised SVGs with no text elements.** The qualitative
   claims are quotable; the axis values are not recoverable.
4. **`ecs_bench_suite`'s two committed runs are not comparable to each other** (different machines
   and dates), and the older one's bevy is `bevy_ecs 0.1` — *pre*-ECS-V2, essentially a hecs fork —
   so it says nothing about bevy's hybrid design. Only the newer run does, and it is four to five
   years stale.
5. **legion's authored rationale for removing chunks and tags is not documented anywhere** primary.
   §4.2's structural explanation is inference.
6. **EnTT's "multi-type storage"** as the post-groups direction: skypjack's intent is quoted, the
   shipped design is not verified.
7. **No direct skypjack ↔ Mertens debate thread was located.** EnTT Discussion #855 is the closest —
   skypjack answering "should EnTT adopt archetypes?" — and Mertens does not appear in it.
8. **shipyard's treatment of zero-sized components** is inferred from the documented sparse-set
   structure; no explicit doc statement was found.
9. **The `measured` figures in §1.3, §2.1, §3.3, §5 and §6.5 came from a throwaway benchmark module**
   built for this note on 2026-09-11 (AMD Ryzen 9 7950X3D, Go 1.27.1, windows/amd64; `ark v0.8.3`,
   `arche v0.15.3`, `donburi v1.15.8`, `unitoftime/ecs v0.0.3`). It was not committed — the map is
   plan-only and cog#243 owns the one prototype this effort keeps. The method is stated inline at
   each table so it can be redone.
10. **`marioolofo/go-gameengine-ecs`'s README benchmark table is self-flagged stale** ("these
    benchmarks are from the old version of this package", 2023-02-23) and its figures are visibly
    broken. **Do not cite them**; use `go-ecs-benchmarks`' ggecs column instead. Its source also
    carries a malformed `// go:build -gcflags=-B` directive (space after `//`) that does nothing —
    a presumed attempt to disable bounds checks.
11. **No current, maintained Go port of leopotam's ecslite** appears in any benchmark suite the
    arche/Ark author endorses. The ticket's implicit invitation to compare against that lineage could
    not be honoured from a primary source.
