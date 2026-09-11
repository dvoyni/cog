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
2. **The 8-byte slot buys almost nothing over the 4-byte one** — 0.384 against 0.422 ns, ~9% — for
   double the index memory. C dominates B.
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

## Files

- `store.go` — the four store shapes.
- `probe_test.go` — baseline and the arity-2 / arity-4 probe comparison.
- `shape_test.go` — paging residency, the bad case, driver selection, tags, growth, scale sweep.
- `scatter_test.go` — the shuffled-order sweep.
