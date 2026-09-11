# What a sparse-set probe costs

Throwaway benchmark module for [cog#239](https://github.com/dvoyni/cog/issues/239), *Sparse sets:
paging, tags, driver selection and the iteration contract*. It is a nested module (`module sbench`)
so `go build ./...` at the repo root skips it, and it is kept for the same reason
[`ecs-query-shape-bench`](../ecs-query-shape-bench) is: the map's first requirement is zero heap
allocation **proven by measurement**, and `allocs/op` alone is explicitly not sufficient.

Storage is sparse sets — settled while charting, on the strength of
[cog#235](https://github.com/dvoyni/cog/issues/235). This module does not re-ask that. It asks what
the *probe* costs, which shape of sparse slot to use, whether to page the index, whether nox's 85
flag bits should be 85 stores or one bitfield, and where the cache cliff is.

**Machine**: AMD Ryzen 9 7950X3D, Go 1.27.1, windows/amd64. 1024 entities unless stated. Every
figure is the **median of 5 runs** at 20000 iterations. `ns/entity` is `ns/op ÷ 1024`.

## The four probe shapes

All four store the same three arrays — a `sparse` index keyed by entity index, a packed `dense`
data array, and `owners` carrying the full entity id. They differ only in what a membership test
has to load.

| | slot | probe | bytes/slot |
|---|---|---|---|
| **A** | `int32` dense index, `-1` absent | `sparse[e]`, then `owners[d] == e` | 4 |
| **B** | `uint64`: generation ‖ dense index | `sparse[e]`, compare generation | 8 |
| **C** | `uint32`: 8-bit generation ‖ 24-bit dense index | `sparse[e]`, compare generation | 4 |
| **D** | C's slot, in 4096-entry pages allocated on demand | page indirection, then C's probe | 4 |

**A** is the probe as the ticket states it: two random loads, the second only to reject a stale
handle. **B** and **C** are skypjack's
[part 13](https://skypjack.github.io/2021-10-09-ecs-baf-part-13/) — fold the generation into the
sparse slot, so membership is one load and one compare with no trip through `owners` and no
tombstone branch. **D** is EnTT's paged index.

The driver never probes itself: it walks its own `dense` array by index and takes the entity from
`owners[i]`. Only the other stores are probed.

### Results

| | ns/op | ns/entity | ns **per probe** | vs A |
|---|---|---|---|---|
| baseline, driver only, no probe | 591.7 | 0.578 | — | — |
| **A** flat, two loads | 1156 | 1.129 | **0.551** | 1.00× |
| **B** flat, one load, 8-byte slot | 985.7 | 0.963 | **0.384** | 0.70× |
| **C** flat, one load, 4-byte slot | 1024 | 1.000 | **0.422** | 0.77× |
| **D** paged, one load, 4-byte slot | 1424 | 1.391 | **0.813** | 1.48× |

At arity 4 (driver + three probes) the ordering holds and the gaps widen:

| | ns/op | ns/entity | ns per probe | vs A |
|---|---|---|---|---|
| **A** | 2826 | 2.760 | **0.727** | 1.00× |
| **C** | 2250 | 2.197 | **0.540** | 0.74× |
| **D** | 3363 | 3.284 | **0.902** | 1.24× |

Scattering the entity indices over a 64K space, to model generation-based recycling, moves nothing
much at this size: A 1.184, B 1.001, C 1.023, D 1.517 ns/entity.

**Conclusions.**

1. **Folding the generation into the sparse slot is worth 24–26%** of probe cost and costs nothing.
   C is the same 4 bytes per slot as A and is cheaper at every arity measured.
2. **The 8-byte slot is ~9% faster than the 4-byte one** — 0.384 against 0.422 ns — for double the
   index memory, so on speed and memory alone the choice is close to a wash. cog#239 chose **B**,
   and not for either: C's 8-bit generation aliases after 255 recycles of an index, which is sound
   only if a store slot is *always* cleared when its component leaves the entity. B's 32-bit
   generation matches `Entity`'s own field exactly, so staleness is decided rather than estimated,
   and [cog#240](https://github.com/dvoyni/cog/issues/240) stays free to choose lazy reclamation.
   C is the lever to pull if index memory ever bites, at the price of constraining that ticket.
3. **Paging costs 93% more per probe** than the flat index it replaces (0.813 against 0.422).
4. The generation compare that makes the one-load probe possible is the *same* compare that makes a
   stale handle fail membership. The liveness check is not an extra cost; it is the probe.

## Paging saves nothing once entity indices scatter

Paging's whole claim is that a store with few owners should not pay for the full index. Whether it
does depends entirely on whether those owners are **clustered** in index space — and
generation-based recycling scatters them, because a recycled index is wherever the freed entity
happened to live.

Index space of 2²⁰ slots, 4096-entry pages, flat cost 4096 KB:

| owners | clustered | scattered |
|---|---|---|
| 3 | 1 page, 16 KB | 3 pages, 48 KB |
| 100 | 1 page, 16 KB | 88 pages, **1408 KB** |
| 10 000 | 3 pages, 48 KB | 256 pages, **4096 KB — the entire flat index** |

So paging pays off only for a store whose owners are created together and never recycled into, and
it is exactly as expensive as a flat index in the general case, *plus* the 93% probe penalty.

The arithmetic that actually decides it: **because indices are recycled, the index space is bounded
by peak concurrent entities, not by total entities ever created.** At nox's scale — low thousands —
a flat 4-byte index is `4 × 4096 = 16 KB` per component type, or **1.4 MB across all 85 flag/class/
status types**. There is nothing here for paging to save.

## Tags: 85 stores, or one bitfield?

nox has 32 class + 32 flag + 21 status bits. As Tags that is 85 sparse sets, each probed
separately. As one `Flags{Class, Flag, Status uint32}` component it is one probe and three mask
tests. Query narrowed by three of them, 1024 entities, all matching:

| | ns/op | ns/entity |
|---|---|---|
| three Tag stores, three probes | 1690 | 1.650 |
| one bitfield component, one probe + masks | 1449 | 1.415 |

**The bitfield is 14% faster, not 3× faster** — the probes hit, and the branches predict. That is a
much weaker speed argument than the shape of the question suggests, and it is bought at a price the
benchmark cannot show: **the lock unit is the component type**, so one bitfield collapses 85
independent lock units into one. Any System writing any flag would then conflict with any System
reading any other flag.

## The known bad case, and its remedy

Two large, mostly disjoint stores: 5000 with `Body`, 5000 with `Collider`, 100 with both.

| | ns/op | note |
|---|---|---|
| drive off a 5000-entity store | 2596 | yields 5000 candidates, 100 survive |
| drive off a 100-entity Tag | 137.0 | **19× faster** |

The driver does 0.52 ns per discarded candidate, so the waste is real but linear and cheap. The
remedy is a Tag maintained on the intersection, and it is the same mechanism as any other query —
no new machinery.

## Driver selection needs no cache

Comparing the lengths of 8 stores to pick the smallest: **3.7 ns**, once per query run. At 20
Systems and 30 Hz that is 2.2 µs *per second*. Caching it would be a global index, and any global
index is a global lock.

## Growth

Filling a store with 10 000 components, dense arrays grown by `append` against preallocated:

| | ns/op | B/op | allocs/op |
|---|---|---|---|
| from empty | 230 000 | 1 711 980 | **39** |
| preallocated | 81 000 | 450 561 | **3** |

Doubling churns **3.8× the bytes** and costs **2.8× the time**. This is not the hot path —
iteration allocates zero, and growth happens only on structural change, which already holds a write
lock — but it is a frame spike, which at 30 Hz is what is felt.

## Scale sweep

The research note records that **no published 1k/10k/100k iteration sweep exists in any primary
source**. Here is one, shape C, one probe. `ordered` fills both stores in the same order, so the
probed store's dense index equals the driver's; `shuffled` fills the probed store in a different
order, so `sparse[]` is still walked in order but `dense[]` is hit at random — the access pattern
cart calls "not cache friendly", and the realistic one.

| entities | driver only | probe, ordered | probe, shuffled | shuffled penalty |
|---|---|---|---|---|
| 1 024 | 0.612 | 1.021 | 1.042 | +2% |
| 16 384 | 0.605 | 1.056 | 1.054 | −0% |
| 131 072 | 0.620 | 1.060 | **1.597** | **+51%** |

**The cliff is between 16K and 131K entities**, where the two dense arrays stop fitting in L2. Below
it, dense order does not matter at all. nox lives at low thousands, two orders of magnitude inside
the flat region — which is the concrete form of the research note's finding that iteration speed is
not what decides this design.

## Nested iteration

cog#239 calls this the sharp edge, because cog#234 measured nested *boxed* iteration at 2050 allocs
/ 57 KB per frame — the inner closure rebuilt once per outer entity. Outer query 1024 entities,
inner query 64, so 65 536 inner steps. `ns/inner` is `ns/op / 65536`.

| shape | ns/op | ns/inner | allocs/op |
|---|---|---|---|
| hand-written slice loops, both levels | 56 400 | **0.861** | 0 |
| outer `All()`, inner slice loop | 56 635 | **0.864** | 0 |
| control: `All()` called 1024× in an ordinary loop, no outer query | 89 875 | 1.371 | 0 |
| outer slice loop, inner `All()` | 124 001 | 1.892 | 0 |
| outer `All()`, inner `All()` | 262 962 | **4.013** | 0 |
| … with the inner iterator hoisted out of the outer loop | 257 240 | 3.925 | 0 |
| … with the inner iterator reached through an interface | 253 174 | 3.863 | 0 |

The control matters: `All()` called 1024 times in an *ordinary* loop costs 1.371 ns/inner, the same
as iterating it once. **Rebuilding the iterator is free.** Hoisting it changes nothing (3.925
against 4.013), and neither does boxing it through an interface (3.863) — so this is not cog#234's
failure mode, and **nothing here allocates**.

What costs is position:

1. **An `All()` in the *outer* position is free.** Outer `All()` with a plain inner loop is 0.864
   against the hand-written 0.861 — parity.
2. **An `All()` in the *inner* position loses inlining**, and it compounds: 1.38× the control when
   the outer is a plain loop, **2.93× when the outer is also `All()`**.

So the cost is the inner yield becoming an indirect call once its range statement sits inside
another yield closure. It is a time cost only; requirement 1 — zero heap allocation on the hot path
— holds in every variant measured.

## Narrowing the iteration list: bitset intersection

Every Query is known at registration, so one could maintain a per-query list of entities matching
the *whole* query and iterate only those. The cheaper relative needs no maintenance at all: give
each Store a presence bitset, one bit per entity index, and intersect the bitsets at query time into
a reusable scratch buffer. Structural change touches one bit; query time is peak/64 words of AND
plus the matches. specs does this with `hibitset`; nobody surveyed does it inside a sparse-set
layout.

| | ns/op | ns/entity | vs probing |
|---|---|---|---|
| arity 4, all 1024 entities match — probing (shape C) | 2250 | 2.197 | 1.00× |
| arity 4, all 1024 entities match — bitset AND | 3261 | 3.185 | **1.45× worse** |
| bad case 5000/5000/100 — probing | 2545 | — | 1.00× |
| bad case 5000/5000/100 — bitset AND | **277** | — | **9.2× better** |

Zero allocations in both. The stores here are shape B, whose probe is ~9% cheaper than the shape C
figure it is compared against, so the all-match gap is slightly wider than the table shows.

**It is a selectivity trade, and a sharp one.** When most candidates match, the AND is wasted work
and bit-walking is slower than walking `owners` — 45% worse. When few match, it skips the rejects
entirely and wins 9.2×. Both regimes are real: the first is the common query, the second is the
bad case above.

The cost is one bit per entity per component type — **43 KB across all 85 of nox's tag types** — and
one bit written per structural change. Crucially it adds **no new lock units**: the bitset lives in
`Store[T]`, written under `write{T}` and read under `read{T}`, both of which a query already holds.

**Why this beats the maintained per-query index it is derived from.** A per-query index can only
store entity ids, never dense indices, because swap-remove relocates dense indices on every removal.
So it still pays a sparse lookup per component per match — exactly what the bitset pays. The only
thing it saves over the bitset is the AND, which is the cheap part, and it buys that by writing
every affected query index on every structural change. **The expensive part of a match is the sparse
lookup, and neither form avoids it.**

# Structural change (cog#240)

Same machine and store shape. Iteration direction is free and decides self-invalidation:

| 5000 entities | ns/op |
|---|---|
| forward, bare walk | 2926 |
| reverse, bare walk | 2909 |
| forward, one probe | 4979 |
| **reverse, one probe** | **4569 — 8% faster** |

Forward + swap-remove visits 667 of 1000; forward + a compensating `i--` is correct; **reverse is
correct with no compensation**. Reverse never reaches an entity appended during the loop, forward
does — a system spawning one entity per visited entity does not terminate (10001 visits from 100).
Reverse only covers removing the *current* entity: removing another still skips one (999/1000).

Despawn must ask every Store, since nothing indexes which hold an entity:

| | ns/despawn |
|---|---|
| 8 Stores, through an interface | 27.5 |
| **85 Stores, through an interface** | **218** |
| 85 Stores, called directly | 199 |

Dynamic dispatch is 9%. Eager beats deciding liveness lazily at nox's scale — 50 despawns a tick
cost **10.9 µs eager** against **19.5 µs lazy**, because lazy taxes every query 0.195 ns per
candidate (4691 → 5666 ns over 5000 entities, +20.8%). Crossover near **89 despawns a tick**. Eager
also keeps `len(owners)` exact for driver selection and needs no reclamation at all; lazy needs an
orphan-slot branch in `add` or 1000 recycles of one index leak 1000 dense entries.

A `Uses` dispatch is not viable on a hot path:

| | ns/call | allocs |
|---|---|---|
| `Uses` dispatch | **1113** | 0 |
| direct call on a held handle | **0.49** | 0 |
| `Spawn[2 fields]` through cached closures | 15.7 | 0 |
| `Spawn[4 fields]` through cached closures | 26.0 | 0 |
| …the same, hand-written | 10.7 / 12.3 | 0 |

The zero allocations locate the 1113 ns: a subscription's Kernel is `bounded`, so the dispatcher
skips its per-call context setup (169 ns, 4 allocs when taken). What is left is the scheduler
round-trip, paid though the request is empty. Deferral into a **typed** queue costs +4.6% at zero
allocations; into a type-erased `[]any` buffer it costs +24% and **one allocation per command**
(50 allocs / 1600 B per tick). Passing a spawn bundle by value and taking its address allocates too
— 48 B per spawn — unless it is copied into a buffer bound at registration.

Following an `Entity` held in a Component costs nothing extra: 4360 ns through the reference against
4693 ns for the same Component as a query field, 8213 ns for both.

## Files

- `store.go` — the four store shapes.
- `probe_test.go` — baseline and the arity-2 / arity-4 probe comparison.
- `shape_test.go` — paging residency, the bad case, driver selection, tags, growth, scale sweep.
- `scatter_test.go` — the shuffled-order sweep.
- `nest_test.go`, `nest2_test.go`, `nest3_test.go` — nested iteration, and where its cost comes from.
- `bitset_test.go` — bitset intersection against probing.
- `structural_test.go` — iteration direction, deferral, the despawn scan, spawn cost.
- `liveness_test.go` — the lazy-reclamation alternative, priced and rejected.
- `registry_test.go` — eager despawn reaching every Store through `Entities`.
- `dispatch_test.go` — `Uses` dispatch against a direct call, on the real kernel.
- `spawn_test.go` — bundle scatter through cached closures, and the escaping-bundle trap.
- `refaccess_test.go` — following a reference, and the guarantees that makes safe.
