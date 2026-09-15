# baseline: third-party sweep costs on the cog#345 workload

**PROTOTYPE, throwaway, never merges (cog#345).** This package gives reference
numbers for the question "what a sweep costs per call on each index". It runs
the exact geometry and query sets from `../queryindex` (`StaticLayouts`,
`BodyLayouts`, `Mixes`, `QuerySet`) inside two MIT-licensed Go physics
libraries. cog's own grid and BVH numbers can then sit beside them like for
like. This is not an adoption study. No library code is copied into cog.

- `github.com/jakecoffman/cp/v2` v2.4.0 (Chipmunk2D port, float64)
- `github.com/neguse/gox2d` v0.0.0-20260315140214-8616afaae132 (Box2D v3 port, float32)

The package is a separate module (`cogproto/baseline`, with
`replace github.com/dvoyni/cog => ../../../..`), so cog's `go.mod` never sees
either dependency.

## Files

- `baseline.go` holds the adapters. `CP` is a `cp.Space` and `Gox2d` is a
  `box2d.DynamicTree` with per-kind shape arrays. Both offer
  `Sweep(q) (Hit, bool)` and `SweepAll(dst, q) []Hit`.
- `baseline_test.go` holds `BenchmarkCP`, `BenchmarkGox2d` and `TestAgreement`.

## How to run

```sh
cd bundles/ecsphysics2d/proto/baseline
go test -tags box2d_release -run TestAgreement -v .
go test -tags box2d_release -run '^$' -bench . -benchtime=300ms -count=3 .
# or build once and interleave with another binary:
go test -c -tags box2d_release -o baseline.test.exe
./baseline.test.exe -test.run '^$' -test.bench 'Gox2d/scatter' -test.benchtime=300ms
```

Always pass `-tags box2d_release`. Without it, gox2d's `b2Assert` checks stay on.

Benchmark names are `Benchmark{CP,Gox2d}/<layout>/<mix>/<op>`. Static layouts
are `scatter`, `rooms16`, `rooms16cells` and `halls64`, with mixes `proj`,
`lospair`, `sight` and `losmap` over side 512, and ops `Sweep` and `SweepAll`.
The body layout is `arena1024`, with mixes `proj` and `body` over the layout's
side 128, and op `Sweep` only. Each benchmark cycles through the 4096 queries
with `qs[i&(len(qs)-1)]`, and all building happens before `b.ResetTimer()`.

## API mapping

| cog#345 term | cp | gox2d |
|---|---|---|
| static segment | `NewSegment(space.StaticBody, At-Half, At+Half, 0)` | `Segment{At-Half, At+Half}`, proxy over its exact AABB |
| circle body | `NewCircle(StaticBody, r, At)` | `Circle{At, r}` |
| box body | `NewBox2(StaticBody, BB{bounds}, 0)` | `MakeOffsetBox(hx, hy, At, RotIdentity)` |
| index | the space's static `BBTree`, built by incremental insertion in item order | one `DynamicTree` with exact AABBs, then `DynamicTreeRebuild(full=true)` (binned SAH) |
| `Sweep` (first hit, radius r) | `Space.SegmentQueryFirst(from, to, r, SHAPE_FILTER_ALL)` | r == 0: `DynamicTreeRayCast` + `RayCastSegment/Circle/Polygon`. r > 0: `DynamicTreeShapeCast` with a 1-point proxy of radius r + `ShapeCastSegment/Circle/Polygon`. The callback returns the hit fraction to clip the walk, as `b2World_CastRayClosest` does. |
| `SweepAll` (every hit) | `Space.SegmentQuery` with a bound method appending to a reused slice | the same tree walks with a callback that appends and returns -1, so the walk is never clipped |

gox2d SweepAll was natural: the tree callback's return value controls clipping.
It is benchmarked. Every static mix has radius 0, so SweepAll only ever runs
the ray-cast path. Neither library sorts SweepAll hits. They come back in tree
order, while `queryindex.Linear` sorts by T. Hits carry only the slot and T.
Both libraries also compute a point and a normal, and neither is compared here.
No exclude filter is used: cog passes entity 0.

## Agreement with `queryindex.Linear`

`TestAgreement` sweeps all 4096 queries of each set through both libraries and
through `Linear.Sweep`. It compares whether each reports a hit, and T within
1e-3 when both hit. It logs and does not fail.

| layout | mix | CP hit-bool | CP T ok | Gox2d hit-bool | Gox2d T ok | Linear hits |
|---|---|---:|---:|---:|---:|---:|
| scatter | proj / lospair / sight / losmap | 100% | 100% | 100% | 100% | 49 / 203 / 1171 / 3892 |
| rooms16 | proj / lospair / sight / losmap | 100% | 100% | 100% | 100% | 214 / 704 / 3337 / 4088 |
| rooms16cells | proj / lospair / sight / losmap | 100% | 100% | 100% | 100% | 214 / 704 / 3337 / 4088 |
| halls64 | proj / lospair / sight / losmap | 100% | 100% | 100% | 100% | 58 / 182 / 1298 / 4027 |
| arena1024 | proj (r = 0) | 100% | 100% | 100% | 100% (worst dT 1e-4) | 973 |
| arena1024 | body (r 0.49–0.74) | **83.08%** | 99.34% of 1058 | 99.93% | **87.53%** of 1748 (worst dT 0.109) | 1751 |

- **Radius 0 matches exactly.** On every static set and on `arena1024/proj`,
  both libraries report the same hits and the same T as Linear.
- **cp misses 693 of Linear's 1751 body-mix hits and never adds a hit.** In 506
  of those misses, the sweep starts already overlapping a body. cp's tree walk
  tests the bare from→to segment against node bounds, and those bounds are not
  grown by the query radius. The walk therefore never reaches a shape that only
  the swept circle would touch, including one the circle starts inside.
  `Shape.SegmentQuery` is an exact swept circle for each shape it is handed, but
  `Space.SegmentQueryFirst` with r > 0 is not an exact swept circle over the
  space. Its T is wrong in 0.7% of shared hits, where a culled nearer shape lets
  a farther one win.
- **gox2d finds the same hits but stops early.** The shape cast uses
  conservative advancement toward `totalRadius - linearSlop` (5 mm), so a hit
  lands about 0.005 / length earlier in T. Body sweeps are 0.1–1.0 m long, which
  moves T by up to 0.05. The worst case is 0.109, which is GJK plus the slop on
  short sweeps. It missed 3 hits, 1 of them a start overlap.
- Both libraries report a sweep that starts inside a shape as a hit at T = 0,
  which matches Linear. gox2d's doc comments say "initial overlap is treated as
  a miss", but the ported code returns `Hit` with `Fraction` 0.

## Medians (3 runs, `-benchtime=300ms`)

AMD Ryzen 9 7950X3D, Windows 11, go1.27.1, `-tags box2d_release`. These come
from a single sequential run and are not interleaved with cog's binary. The
binary is kept for interleaving. Allocs are as reported, with B/op in brackets.

| layout / mix / op | CP ns/op | CP allocs | Gox2d ns/op | Gox2d allocs |
|---|---:|---:|---:|---:|
| scatter/proj/Sweep | 766 | 2 (129 B) | 343 | 1 (24 B) |
| scatter/proj/SweepAll | 759 | 1 (81 B) | 338 | 1 (24 B) |
| scatter/lospair/Sweep | 828 | 2 (135 B) | 358 | 1 (24 B) |
| scatter/lospair/SweepAll | 958 | 1 (87 B) | 335 | 1 (24 B) |
| scatter/sight/Sweep | 1351 | 2 (169 B) | 451 | 1 (24 B) |
| scatter/sight/SweepAll | 1425 | 2 (131 B) | 478 | 1 (24 B) |
| scatter/losmap/Sweep | 2408 | 4 (269 B) | 776 | 1 (24 B) |
| scatter/losmap/SweepAll | 9268 | 17 (853 B) | 2583 | 1 (24 B) |
| rooms16/proj/Sweep | 4058 † | 2 (135 B) | 352 | 1 (24 B) |
| rooms16/proj/SweepAll | 4054 † | 1 (87 B) | 341 | 1 (24 B) |
| rooms16/lospair/Sweep | 4412 † | 2 (153 B) | 368 | 1 (24 B) |
| rooms16/lospair/SweepAll | 4176 † | 1 (105 B) | 386 | 1 (24 B) |
| rooms16/sight/Sweep | 5892 † | 5 (275 B) | 515 | 1 (24 B) |
| rooms16/sight/SweepAll | 6388 † | 4 (261 B) | 582 | 1 (24 B) |
| rooms16/losmap/Sweep | 15771 † | 19 (966 B) | 566 | 1 (24 B) |
| rooms16/losmap/SweepAll | 43915 † | 57 (2769 B) | 3562 | 1 (24 B) |
| rooms16cells/proj/Sweep | 4939 † | 2 (135 B) | 395 | 1 (24 B) |
| rooms16cells/proj/SweepAll | 5056 † | 1 (87 B) | 383 | 1 (24 B) |
| rooms16cells/lospair/Sweep | 5444 † | 2 (153 B) | 422 | 1 (24 B) |
| rooms16cells/lospair/SweepAll | 6182 † | 1 (105 B) | 428 | 1 (24 B) |
| rooms16cells/sight/Sweep | 11983 † | 5 (275 B) | 584 | 1 (24 B) |
| rooms16cells/sight/SweepAll | 13907 † | 4 (260 B) | 666 | 1 (24 B) |
| rooms16cells/losmap/Sweep | 55439 † | 20 (998 B) | 604 | 1 (24 B) |
| rooms16cells/losmap/SweepAll | 153852 † | 57 (2768 B) | 4535 | 1 (24 B) |
| halls64/proj/Sweep | 1205 | 2 (130 B) | 250 | 1 (24 B) |
| halls64/proj/SweepAll | 1090 | 1 (82 B) | 253 | 1 (24 B) |
| halls64/lospair/Sweep | 1204 | 2 (134 B) | 270 | 1 (24 B) |
| halls64/lospair/SweepAll | 1166 | 1 (86 B) | 269 | 1 (24 B) |
| halls64/sight/Sweep | 1352 | 2 (175 B) | 317 | 1 (24 B) |
| halls64/sight/SweepAll | 1443 | 2 (129 B) | 317 | 1 (24 B) |
| halls64/losmap/Sweep | 2937 | 9 (471 B) | 404 | 1 (24 B) |
| halls64/losmap/SweepAll | 4843 | 16 (812 B) | 974 | 1 (24 B) |
| arena1024/proj/Sweep | 1056 | 2 (168 B) | 320 | 1 (24 B) |
| arena1024/body/Sweep | 1047 ‡ | 2 (165 B) | 400 | 1 (96 B) |

For comparison, the earlier survey measured cp scatter/proj at 848 ns and cp
losmap at 2540 ns. It measured gox2d losmap at 712 ns, and a gox2d shape cast
at 306 ns. The scatter rows above are in line with those numbers.

† **cp's tree for the rooms layouts is an insertion-order artefact.** cp's
`BBTree.SubtreeInsert` picks a child by bounding-box area. Axis-aligned wall
segments have zero area, and so does every merge of colinear walls. Inserted in
generation order, which walks along each wall line, the tree degenerates. A
one-off probe (count=1, not kept) built the same items shuffled first and got
these results:

| cp, shuffled insertion | proj/Sweep | losmap/Sweep |
|---|---:|---:|
| scatter | 797 | 2186 |
| rooms16 | 979 | 1383 |
| rooms16cells | 972 | 1752 |

Read the † rows as "cp as naively fed", not as cp's best case. gox2d does not
have this problem, because it does a full SAH rebuild.

‡ cp's body-mix query does less work than the others: its broadphase is not
grown by the radius. See the agreement section.

## Caveats when comparing with a native closed-form grid or BVH

1. **gox2d's tree is a best case.** Its AABBs are exact and it gets one full
   rebuild with no later updates. A real `b2World` fattens non-static proxies by
   `aabbMargin` (0.1 m), and its tree degrades between rebuilds. cp's static
   tree, by contrast, is insertion-order sensitive (†). Neither number includes
   build or update cost. The body layout sits in each library's static
   structure, so no per-step reindex cost is measured.
2. **Allocations come from the API shape, not from the geometry.** gox2d
   allocates once per call, because the tree walk hands `&subInput` to a func
   value, so the input struct escapes (24 B for a ray, 96 B for a shape cast).
   cp allocates its query context and info, plus a little more per narrowphase
   candidate through interface calls. That is why its allocs rise to 57 on long
   rooms sweeps. `SegmentQuery` also takes and releases the space lock.
3. **Narrowphase shape.** gox2d r > 0 runs iterative GJK conservative
   advancement with a 5 mm slop, so it is neither closed form nor exact in T.
   Ray casts (r = 0) are closed form in both libraries. For boxes, cp uses a
   generic polygon and Box2D uses `RayCastPolygon` or GJK, never an
   axis-aligned slab test.
4. **cp r > 0 is not an exact swept circle at the space level**, because of
   broadphase culling (see Agreement). Treat `CP/arena1024/body` as a cheaper,
   less complete query than cog's.
5. **Precision.** cp is float64 throughout, with float32→float64 conversions
   paid in the adapter. gox2d is float32, like cog.
6. **Overheads the adapters add.** An interface call into the adapter, and in
   gox2d a switch on a kind byte plus a bound-method callback per candidate.
   Neither returns a point or normal to the caller, although both compute them.
   cog's Hit carries both.
7. **SweepAll differences.** SweepAll results are unsorted, while cog's SweepAll
   sorts by T. Every static mix is radius 0, so gox2d SweepAll never exercises
   the shape-cast path.
