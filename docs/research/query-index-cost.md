# What a sweep costs per call on each index, and which structure holds it down

Results note for [prototype: what a sweep costs per call on each index, and which structure holds it
down](https://github.com/dvoyni/cog/issues/345), under the physics map
[#182](https://github.com/dvoyni/cog/issues/182). Prototype code:
`bundles/ecsphysics2d/proto/` on branch `proto/query-index-cost`. Nothing merges.

**Measured** on a Ryzen 9 7950X3D, `go1.27.1 windows/amd64`, 2026-09-15. Each figure is the median of
**three interleaved rounds** (prototype binary, baseline binary, three times over) at
`-benchtime 200ms`. All figures are ns per call unless marked µs. Raw output, the medians and the
shape-test counts are in `bundles/ecsphysics2d/proto/results/`.

## The answer in six lines

1. **A uniform grid, for both indices.** It is 4–7× cheaper than a BVH on every short query at the
   reference workload (30–50 ns against 190–300 ns; 2× in a 0.25 / m² melee), 7–10× cheaper than `gox2d`'s tree, and 20–25×
   cheaper than `cp`. It allocates nothing.
2. **Cell size is 2–4 m on this workload,** and it sets the cost only on long walks and in dense crowds.
   Shapes much larger than a cell cost a short query nothing. On long walks they are re-tested once per
   cell, which is what pushes long merged walls towards 4–8 m.
3. **Keeping `BodyIndex` current every sub-step is affordable:** an incremental grid update is 9 µs for
   1 024 bodies and 46 µs for 4 096. A BVH rebuild is 130–750 µs, and a refit-only BVH is 4× slower to
   query after 16 m of drift.
4. **`SweepAll` is not materially dearer than `Sweep`.** It pays for the extra distance it walks, not for
   ordering by T. On short queries the two are within 5%.
5. **No any-hit query.** On `StaticIndex` line of sight an early-out saves about 4 ns of 45. It saves more
   only on dense bodies, and no §6 query asks that.
6. **The whole query and index bill for a busy tick is about 0.26 ms, 1.5% of a 16.7 ms frame.** On a
   BVH it is about 1 ms, and `cp`'s contact-pair line of sight alone is about 0.8 ms.

## 1. The workload

The ticket allows a prototype to name nox. Everything below is seeded and reproducible (`workload.go`).

**Static layouts**, all on a 512 m map:

| Layout | Segments | What it stresses |
| --- | ---: | --- |
| `scatter` | 4 096 | the survey's randomised case: 2 m segments on random cell edges ([#284](https://github.com/dvoyni/cog/issues/284), [#285](https://github.com/dvoyni/cog/issues/285)) |
| `rooms16` | 3 705 | a building: 16 m rooms with a 2 m doorway per wall span, runs merged (mean ~7 m) |
| `rooms16cells` | 14 784 | the same walls, one segment per 2 m cell (an app that does not merge) |
| `halls64` | 275 | 64 m halls with runs averaging ~31 m, **far larger than any cell** |

**Body layouts**, in nox's §2 shape mix: 70% creatures (r 0.49–0.74 m), 10% boulders (r 1.97 m),
10% crates (2.1 × 1.0 m box) and 10% blocks (2.6 m box). Overlaps are allowed.

| Layout | Bodies | Area | Density |
| --- | ---: | --- | --- |
| `crowd256` | 256 | 32 m square | 0.25 / m², a melee |
| `arena1024` | 1 024 | 128 m square | 0.06 / m², a busy fight |
| `map4096` | 4 096 | the whole map | 0.016 / m², a populated level |

Nox's sources give no body count. These three bracket it, and the verdict does not change across them.

**Query mixes**, 4 096 queries each, starting uniformly over the layout's area in random directions:

| Mix | Radius | Length | Stands for |
| --- | --- | --- | --- |
| `proj` | 0 | 0.4–1.0 m | Q1/Q2: a projectile's sub-step (§6: 0.44–0.96 m) |
| `body` | 0.49–0.74 m | 0.1–1.0 m | a body-radius sweep |
| `lospair` | 0 | 1–4 m | Q3: line of sight between a candidate contact pair |
| `sight` | 0 | 5–31.5 m | Q4: AI sight, gated at 31.5 m |
| `losmap` | 0 | endpoints anywhere | the survey's full-map trace, a worst case no §6 query makes |

Queries on `BodyIndex` each exclude one body, as a projectile that is a body would.

Hit rates, from `TestStats`: on `rooms16`, 5% of `proj` sweeps hit, 17% of `lospair` and 82% of `sight`.
On `arena1024` it is 24%, 37% and 81%. The layouts are not empty space.

## 2. What was built

- **Primitives** (`shape.go`): closed-form sweeps of a circle against a circle, box or segment. A point is
  radius 0. Growth is exact: a box is rounded, a segment becomes a capsule. `Hit` follows
  [#306](https://github.com/dvoyni/cog/issues/306), including T = 0 for a sweep that starts
  overlapping and a normal facing the sweeper.
- **`Linear`**: every shape, every query. It is the correctness oracle.
- **`Grid`**: a dense uniform grid with per-cell slot lists; a shape is listed in every cell its bounds
  cover.
  - **Short sweeps** scan the rectangle of cells their grown bounds cover. Each shape is tested once, in
    the first cell of the rectangle it appears in, so nothing needs per-query state.
  - **Long sweeps** walk the cells in order (Amanatides & Woo), with a band of ⌈r / cell⌉ cells either
    side. They stop once the best hit lies before the exit of the cell being left.
  - **Changes:** `Update` moves bodies and touches the cell lists only for a body whose cell range
    changed. `Replace` swaps one static slot.
- **`BVH`**: a median-split hierarchy with leaves of 2, 4 or 8 shapes, one flat node slice, and a
  nearer-child-first walk on a fixed stack. Node boxes are grown by the sweep radius. It is either
  rebuilt or refit.
- **Baselines** (`baseline/`, a separate module): `cp` v2.4.0 and `gox2d` on the same geometry and the
  same queries. `gox2d`'s tree is built with binned SAH and exact bounds, which is its best case.

**Correctness.** Every index agrees with `Linear` on `Sweep` (hit, T, entity), on the any-hit query, on
`SweepAll` (the same hits in T order) and on `Overlap`. That covers every layout and every mix, 1 024
queries each. The oracle test was checked by breaking the grid on purpose twice, truncating the walk and
stopping it early. Both were caught. The baselines agree with `Linear` exactly at radius 0. At radius > 0,
`cp` misses 17% of hits because its tree walk uses the ungrown segment, and `gox2d`'s iterative cast
stops about 5 mm short (`baseline/README.md`).

**The allocation rule holds.** `testing.AllocsPerRun == 0` on every query path of every index, and on
grid rebuild, grid update, static replace and BVH refit (`TestZeroAllocations`).

## 3. Per-call cost

### StaticIndex: `Sweep`, ns

| Index | scatter proj | scatter lospair | scatter sight | scatter losmap | rooms16 proj | rooms16 lospair | rooms16 sight | rooms16 losmap | rooms16cells proj | halls64 sight |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| linear | 76 997 | – | – | 101 049 | 32 668 | – | – | 34 428 | 134 411 | – |
| **grid 1 m** | 30 | 44 | 282 | 688 | 36 | 51 | 224 | 244 | **38** | 267 |
| **grid 2 m** | **30** | **38** | 192 | 470 | **35** | **45** | 160 | 188 | 41 | 164 |
| **grid 4 m** | 37 | 43 | **172** | **411** | 42 | 51 | **146** | **167** | 59 | 88 |
| grid 8 m | 62 | 65 | 248 | 464 | 59 | 70 | 184 | 182 | 111 | **53** |
| bvh 2 | 191 | 213 | 356 | 683 | 212 | 235 | 359 | 418 | 230 | 168 |
| bvh 4 | 222 | 256 | 435 | 812 | 261 | 280 | 442 | 511 | 239 | 201 |
| bvh 8 | 295 | 324 | 545 | 1 007 | 321 | 359 | 548 | 628 | 285 | 224 |
| `gox2d` tree | 300 | 294 | 411 | 698 | 307 | 330 | 435 | 462 | 312 | 261 |
| `cp` | 745 (2 allocs) | 788 (2) | 1 112 (2) | 1 996 (4) | 3 039 † | 3 204 † | 4 503 † | 11 675 † | 3 471 † | 1 056 |

† `cp`'s bounding-box tree chooses insertion points by area. Axis-aligned walls have zero area, so inserted
in generation order it degenerates on the rooms layouts. A shuffled probe measured about 980 ns for
`proj` and 1 380 ns for `losmap`. Read `cp` on `scatter`; the survey's 848 and 2 540 ns there came out
at 745 and 1 996 here.

### BodyIndex: `Sweep`, ns

| Index | crowd256 proj | crowd256 body | crowd256 lospair | arena1024 proj | arena1024 body | arena1024 sight | map4096 proj | map4096 body |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| linear | 2 430 | 2 656 | 2 483 | 10 336 | 10 512 | 10 371 | 39 055 | 39 773 |
| **grid 1 m** | **91** | **188** | **182** | **42** | **71** | 207 | 32 | 45 |
| **grid 2 m** | 111 | 194 | 184 | 47 | 81 | **203** | **30** | **38** |
| grid 4 m | 180 | 259 | 244 | 70 | 107 | 278 | 33 | 41 |
| grid 8 m | 357 | 458 | 422 | 134 | 172 | 452 | 52 | 64 |
| bvh 2 | 248 | 331 | 303 | 211 | 250 | 387 | 204 | 220 |
| bvh 4 | 258 | 338 | 300 | 227 | 270 | 386 | 225 | 278 |
| bvh 4, refit after 4 m drift | 456 | 573 | 538 | 295 | 339 | 502 | 258 | 279 |
| bvh 4, refit after 16 m drift | 1 422 | 1 659 | 1 622 | 928 | 1 010 | 1 451 | 479 | 497 |
| `gox2d` tree | – | – | – | 264 | 338 | – | – | – |
| `cp` | – | – | – | 819 | 799 ‡ | – | – | – |

‡ `cp`'s radius > 0 space query is incomplete (see §2), so it is cheaper than it would be if correct.

### The margin holds against noise

149 of 1 030 benchmarks varied by more than 25% between rounds, mostly the µs-scale `crowd256` update
benchmarks, where one round was disturbed. So the headline is checked **grid 2 m's slowest round
against bvh 2's fastest**, on the short mixes:

| Layout | proj | lospair | body |
| --- | ---: | ---: | ---: |
| scatter, rooms16, rooms16cells, halls64 | 4.6–5.5× | 4.1–5.2× | 3.8–4.9× |
| arena1024 | 3.7× | 2.5× | 2.9× |
| map4096 | 6.2× | 5.5× | 5.5× |
| crowd256 | 2.0× | 1.5× | 1.6× |

The BVH's own floor explains it. It spends 190–250 ns walking a dozen levels of slab tests before testing
2–4 shapes (`TestStats`), while the grid tests under one shape per short static query, in a rectangle
found by arithmetic. `gox2d`'s SAH-built tree lands at the same 260–340 ns, so a better builder does not
close the gap.

## 4. Shapes larger than a cell, and choosing cell size

- **Short queries do not care.** On `halls64`, whose walls average ~31 m (15 cells at 2 m), `proj` costs
  28–32 ns at every cell size from 1 to 8 m. The rectangle scan tests each shape once however many
  cells list it.
- **Long walks pay once per cell.** A walk running along a long wall re-tests it in every cell it shares.
  `halls64` `sight` costs 267 ns at 1 m and 53 ns at 8 m. On short walls the pull goes the other way:
  `rooms16cells` is cheapest at 1–2 m.
- **Dense crowds want small cells.** `crowd256` is cheapest at 1 m (91 against 111 ns at 2 m and 180 at
  4 m), because a big cell holds many bodies.
- **Overlap favours large cells at large radii.** A 36.9 m Push costs 6.3 µs at 2 m, 3.8 µs at 4 m and
  2.2 µs on bvh 4. It runs once per cast, so it does not move the budget.

On this workload **2 m is best or within 25% of best everywhere a §6 query runs often.** 4 m wins on the
long queries. Nothing about 2 m is a content assumption, and the cost is flat enough across 2–4 m that the
choice is a tuning knob rather than a cliff.

**Memory.** A dense grid covering the 512 m map plus a 32 m margin has 83 000 cells at 2 m, about 2.1 MB
of cell headers per index, and 8.3 MB at 1 m. A dense grid needs **the world's extent** up front.
Out-of-extent shapes are clamped into edge cells: still correct, but slow if there are many. A spatial hash
removes the extent but was not measured.

## 5. Keeping the indices current

| Per sub-step (µs) | crowd256 | arena1024 | map4096 |
| --- | ---: | ---: | ---: |
| grid 2 m `Build` (full rebuild) | 3.5 | 17.3 | 96.2 |
| **grid 2 m `Update`** (only bodies that changed cells, 0.11 m steps) | **2.5** | **9.4** | **46.4** |
| grid 4 m `Update` | 2.0 | 8.3 | 40.5 |
| bvh 4 `Build` | 11.2 | 128.1 | 747.0 |
| bvh 4 `Refit` | 1.0 | 4.3 | 17.2 |

- **Refit is cheap and then goes wrong.** After 4 m of drift a refit-only BVH queries 1.1–1.8× slower,
  and after 16 m 2–5.5× slower. A creature running at 13.7 m/s drifts 16 m in 1.2 s, so a refit BVH needs
  periodic rebuilds, and each rebuild costs the 128–747 µs above.
- **A grid has no drift to repair.** Updating every sub-step makes `BodyIndex` exact to the sub-step
  at 9 µs per sub-step for 1 024 bodies. That turns the map's open *"when is `BodyIndex` rebuilt, and how
  stale are a query's positions"* into a choice rather than a constraint.
- **Static changes.** Replacing one static Entity in a grid (a door opening) costs 13–22 ns. A BVH rebuilds
  the whole static set: 0.6 ms on `rooms16`, 2.5 ms on `rooms16cells`. Building the grid at level load
  costs 6–256 µs.

## 6. `Sweep` against `SweepAll`, and whether an any-hit query earns a place

| ns | rooms16 static lospair | rooms16 static sight | rooms16 static losmap | arena1024 body proj | arena1024 body sight | crowd256 body body |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| grid 2 m `Sweep` | 45 | 160 | 188 | 47 | 203 | 194 |
| grid 2 m any-hit | 41 | 135 | 161 | 39 | 141 | 48 |
| grid 2 m `SweepAll` | 44 | 257 | 2 718 | 45 | 427 | 193 |

- **On short queries `SweepAll` costs the same as `Sweep`,** within 5% everywhere. Ordering the one or two
  hits by T is free.
- **On long queries `SweepAll` pays for walking to the end:** 1.6× on 31 m static sight, 2.1× on body sight,
  and 14× on a full-map trace that §6 never makes. That is the distance past the first hit, not the sort.
  **Ordering by T does not make `SweepAll` materially dearer.**
- **An any-hit query saves about 4 ns of 45 on Q3,** the only high-volume boolean, which runs on
  `StaticIndex`: about 4 µs a tick at 1 000 pairs. It pays off 4× only in a dense crowd with a
  body-radius sweep (48 against 194 ns), because hits there are near-certain and `Sweep` must finish its
  rectangle. No §6 query asks that. Q4's body half is a `SweepAll` anyway.
  **The numbers do not ask for an any-hit query.**

## 7. Per-tick budget

A busy tick, grid 2 m throughout, with `Update` per sub-step. Scenario A is `rooms16` walls plus
`arena1024` bodies, Scenario B is `rooms16cells` plus `map4096`. The call rates are §6's, and the counts
are deliberately heavy.

| Work | Rate | A: µs / tick | B: µs / tick |
| --- | --- | ---: | ---: |
| Projectile sub-step: `Sweep` static + `Sweep` bodies | 256 projectiles × 2 sub-steps | 42 | 36 |
| Q3 contact-pair line of sight: `Sweep` static | 1 000 pairs | 45 | 56 |
| `BodyIndex` `Update` | 2 sub-steps | 19 | 93 |
| Q4 AI sight: `Sweep` static + `SweepAll` bodies | 256 creatures a tick (unstaggered) | 150 | 108 |
| **Total** | | **256 µs (1.5%)** | **293 µs (1.8%)** |
| Same work on bvh 4, rebuilt per sub-step | | ≈ 1 030 µs | ≈ 2 230 µs |

For scale, `cp`'s Q3 alone at 1 000 pairs is 788 µs and 2 000 allocations a tick (on `scatter`), which
is the survey's *"51 ms of query time per second of play"* again.

Per projectile: **82 ns a sub-step, 164 ns a tick.** Per line-of-sight check: **45 ns.** A frame's
16.7 ms holds about 100 000 projectiles' ticks or 370 000 contact-pair checks on one core. Queries are
reads and share their Resource (#288), so they need not all land on one core.

## 8. What this does not settle

These are decisions for the ticket's resolution, not facts the prototype can supply.

- **Who picks cell size.** It is an index parameter. It could be a plugin setting the app supplies, a
  default, or derived per index at build time from the shapes it holds. The map's guard forbids a content
  assumption, and a fixed 2 m default is close to one.
- **World extent.** A dense grid needs bounds; a spatial hash does not, and was not measured.
- **Update per sub-step or less often.** Per sub-step is affordable. Whether it is required is the System
  decomposition's call.
- **Not measured:** concurrent readers contending for memory bandwidth, clustered rather than uniform body
  placement beyond `crowd256`, a spatial hash, a CSR (compact array) cell layout instead of per-cell
  slices, and the cost of a hook drain feeding `Replace`.

## Caveats

- The harness calls every index through one interface, and cog#306 forbids one in the real surface. Every
  candidate pays the same few ns, which slightly flatters the BVH's ratio.
- Single-threaded, uniform random query origins, float32 throughout. Node boxes carry a 1 mm culling
  margin and the grid walk a 1 mm stop margin, so float32 rounding at a boundary cannot cull a real hit.
- The BVH is a straightforward median split, not SAH. `gox2d`'s SAH tree measures within 10–30% of it, so
  the builder is not what the verdict turns on.
- One machine, three interleaved rounds. The 25%-plus spreads are named above, and the headline ratios are
  quoted worst grid against best BVH.
