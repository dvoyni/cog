# What Go's 2D physics libraries supply, and what would reopen adopting one

Research for [dvoyni/cog#284](https://github.com/dvoyni/cog/issues/284), child of the physics
map [#182](https://github.com/dvoyni/cog/issues/182). Section references of the form §6 are to
that map's third comment, *"Requirements from the driving game"*.

The map's **Settled while charting** entry chooses *write our own* on five reasons and holds the
decision pending this ticket, because reasons 1 and 3 were the map's claims rather than verified
facts. This note verifies them against the repositories, and sweeps for candidates the seed issue
missed.

**The decision stands. Reason 1, the load-bearing one, does not.**

- **`jakecoffman/cp` does supply a genuine swept circle against segment geometry** — exact to
  closed form, with the fraction, the point, a unit normal and the shape hit. That is §6's Q1
  and Q2. It also has a soundness defect above the shape level that the map could not have
  guessed, and it allocates on every query.
- **The survey missed an entire lineage.** Four independent Go ports of **Box2D v3** landed in the
  last twelve months, and the best of them supplies `ShapeCastSegment` as a **free function over
  `float32` value types with zero allocations**, plus a broadphase that inflates its node AABBs by
  the query radius — the exact primitive the map says Go does not have. It is six months old and
  has one star.
- **Reasons 2, 3, 4 and 5 are confirmed**, reason 3 emphatically.

So the conclusion survives, the argument for it changes, and **the exit condition changes most of
all — because a version of it has already fired once, in March 2026, and nobody noticed.**
See [What this overturns](#what-this-overturns) and [The exit condition](#the-exit-condition).

No code was copied from any library. Everything below is a note with a file, a line and a commit.

## What was read

| Thing | Commit read | Date | Module / version | Licence |
| --- | --- | --- | --- | --- |
| `github.com/jakecoffman/cp/v2` | `64b09f584d36466266282dcfe80c5b9651592b44` | 2025-12-26 | `v2.4.0` — HEAD **is** the tag | MIT |
| `github.com/ByteArena/box2d` | `acbde413692f086bf32b364d1bcb8a546283eafd` | 2020-07-13 | `v1.0.2` = `05f3eabc3755dd654f11a368cec066e0a3a0c3a8`, **2018-08-24**, 18 commits *behind* HEAD | Zlib |
| `github.com/solarlune/resolv` | `8b4e8c15ba3b6428f976ddb2d56bbe04b719a8fe` | 2024-12-19 | `v0.8.1` — HEAD **is** the tag | MIT |
| `github.com/neguse/gox2d` | `8616afaae132b1d777a11ca406099b85bde248cc` | 2026-03-15 | no tags; pseudo-version `v0.0.0-20260315140214-8616afaae132` | MIT |
| `slembcke/Chipmunk2D` (upstream of `cp`) | `f2f3d66220b8bb1568b8402c6d194d73d4e477d1` | 2026-05-05 | Chipmunk 7.0.3 | MIT |
| `erincatto/box2d` at the ByteArena fork point | `f655c603ba9d83f07fc566d38d2654ba35739102` | **2017-06-24** | `b2_version = {2, 3, 2}` | MIT |
| `erincatto/box2d` current | tag `v3.1.1` | 2025-06-04 | — | MIT |

Measurements were taken in throwaway modules outside cog's tree. **Nothing was added to cog's
`go.mod`.**

```
go version go1.27.1 windows/amd64
AMD Ryzen 9 7950X3D 16-Core Processor, GOMAXPROCS=32
```

All four libraries **build and run against go1.27.1** with no changes, `ByteArena/box2d` included
despite having no `go.mod` at all (the proxy synthesises `v1.0.2` from the tag).

Two provenance corrections to the map's prose:

- The map dates the `ByteArena/box2d` upstream snapshot to 2017. The README (`README.md:7` at
  `acbde41`) names commit `f655c603` and calls it *"the latest Box2D commit as of 2017-09-20"* —
  but `f655c603` is dated **2017-06-24** and carries `b2Version b2_version = {2, 3, 2}`
  (`Box2D/Box2D/Common/b2Settings.cpp` at that ref). The snapshot is Box2D **2.3.2**.
- What `go get` resolves is **`v1.0.2`, tagged 2018-08-24**, eighteen commits behind the 2020
  master. "Abandoned since 2020" understates it: the *published* artefact is from 2018.

---

## What this overturns

Four things the map states are not what the repositories say.

1. **`cp` has "no continuous collision of any kind"** — false as a statement about the query
   surface. `cp` has no CCD *in its solver* (verified below: a body tunnels), but
   `Space.SegmentQueryFirst(start, end, radius, filter)` is a genuine swept circle against
   circles, capsules and polygons. The map's own workaround — *"raycast yourself"* — is spelled
   `radius > 0` in this library. The raycast **is** the sweep.
2. **"No candidate supplies the load-bearing query"** — false twice over. `cp` supplies Q1, Q2,
   Q3, Q4, Q7 and Q8 directly. `neguse/gox2d` supplies the sweep in exactly the packaging the map
   wants: value types, no world, `float32`, zero allocations.
3. **`ByteArena/box2d` is "the only candidate with real CCD"** — true of the *solver*, and
   `b2TimeOfImpact` did survive the port, but incomplete: the 2017 snapshot predates
   `b2ShapeCast`, which Box2D gained at **v2.4.0** (`include/box2d/b2_distance.h:100–120` at tag
   `v2.4.0`). The port has conservative-advancement TOI and **no shape cast**, and its TOI is
   tolerance-based, not exact — measured 5 mm early, below.
4. **The survey named three candidates and the field is at least twelve.** Four Go ports of Box2D
   v3, a maintained fork of `cp`, a Box2D 2.4.1 continuation of the ByteArena port that *does*
   have `B2ShapeCast`, and several sweep-only packages. See [the sweep](#5-what-the-survey-missed).

What survives untouched: reason 2 (the pointer graph), reason 3 (the vocabulary), reason 4 (the
response model), reason 5 (§9) — and a set of costs the map did not know about, which is what now
carries the decision.

---

## 1. `jakecoffman/cp` — a Chipmunk 7 port, actively maintained

Eight commits between 2025-06-19 and 2025-12-26; `v2.4.0` is HEAD; thirty commits since 2022-01-01.
This is a live project, not a corpse.

### 1.1 The radius parameter is a genuine swept circle

`Shape.SegmentQuery(a, b Vector, radius float64, info *SegmentQueryInfo) bool` (`shape.go:209`)
dispatches to a per-class implementation, and every one of the three adds the query radius to the
shape's own radius — the Minkowski identity that makes a thickened ray and a swept circle the same
test:

- `circle.go:72` `CircleSegmentQuery`: `rsum := r1 + r2`, then a quadratic in `t`. The returned
  `info.Point` is pulled back by `n.Mult(r2)`, i.e. reported on the *shape's* surface, not the
  swept circle's.
- `segment.go:110` `Segment.SegmentQuery`: `r := seg.r + r2`, offsets the segment by the combined
  thickness along the flipped normal, solves for the crossing — and, when the ray misses the flat
  face, falls through to `CircleSegmentQuery` against **both endpoints** (`segment.go:145–153`).
  That is a full capsule sweep with rounded caps, not a face-only test.
- `poly.go:114` `PolyShape.SegmentQuery`: `rsum := r + r2` against each face plane, then a second
  loop over the *"beveled vertexes"* (`poly.go:150–158`), again `CircleSegmentQuery` per vertex.

`SegmentQueryInfo` carries `Shape`, `Point`, `Normal`, `Alpha` — the shape hit, the contact point,
a unit normal and the fraction along the ray. Exactly §6 Q1's four outputs, *"which wall segment"*
included.

Measured, against a `Segment` from (2, −1) to (2, 1) with zero thickness, ray from (0, 1.4) to
(4, 1.4) — which passes 0.4 above the top endpoint:

```
r=0.0 hit=false alpha=1.0000
r=0.5 hit=true  alpha=0.4250 point=(2.000000, 1.000000) normal=(-0.600000, 0.800000) |n|=1.000000
closed-form alpha for the endpoint cap = 0.4250
```

A circle of radius 0.5 whose centre travels along `y = 1.4` first touches (2, 1) when its centre
is at `x = 2 − sqrt(0.25 − 0.16) = 1.7`, i.e. `alpha = 1.7/4 = 0.425`. The library agrees to four
decimals and the normal is unit-length. **It is the real thing, endpoint caps included.**

### 1.2 …but the `Space`-level broadphase silently drops grazing swept hits

`Space.SegmentQuery` (`space.go:1032`) and `Space.SegmentQueryFirst` (`space.go:1042`) both pass
the **raw, un-inflated** `start, end` to the spatial index:

```
space.staticShapes.class.SegmentQuery(&context, start, end, 1, segmentQuery, data)
```

The radius lives only in the `SegmentQueryContext` that the *narrowphase* callback reads
(`space.go:1015`). The BBTree traversal tests child node AABBs against the bare ray
(`bbtree.go:424–425`, `subtree.a.bb.SegmentQuery(a, b)`, itself the slab test at `bb.go:94`). A
shape the swept circle would hit, but whose AABB the centre ray misses, is culled before the
narrowphase ever sees it.

This is not a porting defect. Upstream Chipmunk does the same: `src/cpSpaceQuery.c:124–137` at
`f2f3d66` passes `start, end` to `cpSpatialIndexSegmentQuery` and keeps the radius in the context.
The maintained fork `setanarut/cm` inherits it verbatim (`space.go:948–963`).

Measured, on a field of 200 vertical wall segments so the tree has real internal nodes (a
single-shape tree cannot show this, because `SubtreeSegmentQuery` returns at a leaf *without*
testing that leaf's own BB, `bbtree.go:421–423`):

```
shape-level  hit=true  alpha=0.4850
space-level  hit=false alpha=1.0000
space-level SegmentQuery callbacks = 0
```

Control, same field, same tree, `radius = 0`: the space-level query finds the wall. Both queries
see the same 200 indexed shapes.

**Consequence for §6.** Q1 for a *projectile* is unaffected — projectiles are points, radius 0,
and the query is exact there. Q1 for a *character* sweep, Q2's swept circle vs bodies and Q9's
swept hop are affected: `cp` reports clean misses on grazing contacts, at a rate set by how much
thinner the AABB is than the sweep. Fixing it means inflating the query AABB before traversal,
which is unreachable from outside the package — `Space.staticShapes` is unexported and
`BBTree.SegmentQuery` takes no radius (`bbtree.go:447`, `spatialindex.go:20`).

### 1.3 The solver does tunnel, and there is no CCD anywhere in the package

`grep -rin "continuous|time of impact|timeofimpact|bullet|tunnel"` over all `*.go` and `*.md` at
`64b09f5` returns **nothing**. No bullet flag, no TOI, no sub-stepping in `Space.Step`.

Measured: a radius-0.05 circle body at 115 m/s (the game's crossbow bolt, §6) stepped at
`dt = 1/120 s` against a 0.4 m wall (`Segment` with radius 0.2) at x = 5:

```
step 4 x=4.501
step 5 x=5.460      <- through the wall, no contact
step 6 x=6.418
```

The same wall and motion, asked as a query instead:

```
swept point query: hit=true alpha=0.3132 point=(4.800000, 0.000000) normal=(-1.000000, 0.000000) attributable=true
```

`alpha` and `point` are exact (the wall's near surface is at 4.8), the normal is the axis-aligned
±X §1 promises, and `info.Shape` is pointer-equal to the wall that was added — §7's *"query
results must be attributable back to the wall cell"* holds by identity, for free.

**So the map is right about the mechanism and wrong about the conclusion.** `cp` cannot be handed
a fast body; it can be *asked* where a fast thing would hit. §6 wants the second, and §13 records
that the original game does exactly that — projectiles are swept queries, not bodies.

### 1.4 §6 coverage

| # | Query | `cp` | How |
| --- | --- | --- | --- |
| Q1 | swept point/circle vs walls → t, point, normal, which segment | **yes** (radius 0 exact; radius > 0 unsound at Space level, §1.2) | `Space.SegmentQueryFirst`, `space.go:1042` |
| Q2 | swept vs bodies, first or all hits | **yes**, same caveat | `Space.SegmentQuery` walks dynamic *and* static indices, `space.go:1035–1036` |
| Q3 | segment trace vs walls, boolean LOS | **yes** | same, radius 0, filtered to the wall category |
| Q4 | trace vs walls + shadow casters | **yes** | `ShapeFilter{Categories, Mask}`, `space.go:1014` |
| Q5 | radius overlap → every dynamic body in range | **adapter** | `Space.BBQuery` (`space.go:980`) is AABB only; a true circle overlap needs `Space.ShapeQuery` (`space.go:1084`) with a temporary `Shape`, which needs a `Body` |
| Q6 | nearest body matching a predicate within a radius | **partial** | `Space.PointQueryNearest` (`space.go:940`) — the predicate can only be a `ShapeFilter` bitmask, not a caller closure |
| Q7 | point query → body under a point | **yes** | `PointQueryNearest` with `maxDistance = 0` |
| Q8 | closest point on wall geometry to a circle | **yes** | `Segment.PointQuery` (`segment.go:86`) returns `Point`, `Distance`, `Gradient` — the clamped closest point §5 needs |
| Q9 | overlap at a destination, or a swept hop | **adapter** | `Space.ShapeQuery`, needs a `Shape` on a `Body` |

Six of nine directly; three through adapters that all bottom out in the same problem: **`cp` has
no way to ask a geometric question about a shape you have not first installed in a world.**

### 1.5 What every query costs, and what it allocates

Over a randomised 512 m map of 4096 2 m wall runs — the workload reused verbatim for `gox2d` in
§5.1, same RNG seeds, same query sets:

| Benchmark | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| `Shape.SegmentQuery`, one capsule, radius 0.5, no broadphase | 75.1 | 96 | **2** |
| `Space.SegmentQueryFirst`, 0.958 m point sweep | 848 | 133 | **2** |
| `Space.SegmentQueryFirst`, full-map LOS trace | 2540 | 286 | **5** |

(The LOS trace averages five allocations rather than two because it reaches shapes, and each shape
actually tested allocates two more inside `Shape.SegmentQuery`.)

The allocations are structural. `go build -gcflags='github.com/jakecoffman/cp/v2=-m'` names them:

```
space.go:1043:2:  moved to heap: info
space.go:1044:13: &SegmentQueryContext{...} escapes to heap
shape.go:210:2:   moved to heap: blank
shape.go:217:6:   moved to heap: nearest
```

Two causes, both architectural:

- **`any`-typed callback plumbing.** `SpatialIndexSegmentQuery` is
  `func(obj1 any, obj2 *Shape, data any) float64` (`spatialindex.go:5`), so the context and the
  result struct are passed as interfaces and escape. Every space query heap-allocates.
- **The `ShapeClass` interface.** `Shape.SegmentQuery` passes `&blank` and `&nearest` into
  `shape.Class.PointQuery` / `shape.Class.SegmentQuery` (`shape.go:216–224`); the call is not
  devirtualised, so escape analysis must assume retention. Even the world-free shape-level query
  allocates twice.

§6 calls Q3 *"the highest-volume query in the game — once per candidate contact pair per tick"*.
Contact-pair traces are short, so the 848 ns figure is the right one: a thousand contact pairs a
tick at 60 Hz is 60 000 queries a second — **51 ms of query time and 120 000 allocations per
second of play**, before any of the game's own work.

### 1.6 The pointer graph, measured

Reason 2 holds, and now with numbers. `Shape` (`shape.go:24–41`) is **168 bytes with 5 of 13
fields pointer-bearing**: `Class ShapeClass`, `space *Space`, `body *Body`,
`massInfo *ShapeMassInfo`, `UserData any`. `Body` is **304 bytes with 9 of 25**: `UserData`, two
func fields, `space`, and four intrusive list pointers. `Circle` and `Segment` each embed
`*Shape`.

Every field that would need to be set is unexported, so a `cp.Shape` cannot be constructed by
literal from outside the package; the only doors are `NewShape`, `NewCircle`, `NewSegment` and
`NewPolyShape`, all of which take a `*Body`, and several accessors dereference it unconditionally
(`shape.go:65` `SetSensor` calls `s.body.Activate()`).

This is the map's *"a Component holds no pointer, transitively, enforced at registration"* meeting
a type that is 40% pointers by field count. Confirmed, and not a near miss.

### 1.7 What "adopt it and use only its broadphase" actually costs

`cp` is **7 588 non-test lines**. The extractable index is `bbtree.go` (534) + `spatialindex.go`
(66) + `bb.go` (173) + `hashset.go` (229) ≈ **1 002 lines**, so ~87% would be carried and not used.
That is the cheap part of the answer. The expensive part is that the index is **typed on
`*Shape`**:

```
type SpatialIndexBB func(obj *Shape) BB              // spatialindex.go:3
type SpatialIndexQuery func(obj1 any, obj2 *Shape, collisionId uint32, data any) uint32
Insert(obj *Shape, hashId HashValue)                 // spatialindex.go:16
```

No generic parameter, no `any` on the stored object. To put a cog entity in `cp`'s BBTree you must
wrap it in a heap-allocated 168-byte `cp.Shape` carrying an interface `Class`, a `*Body` (nil-able
but load-bearing on several paths), a `*ShapeMassInfo`, and a boxed `UserData` as the only route
back to the `ecs.Entity`. `NewBBTree(cp.ShapeGetBB, nil)` (`bbtree.go:75`) and `Shape.SetBB`
(`shape.go:129`) are exported, so it is *technically* reachable — but what you have adopted is a
pointer per entity that cannot live in a Component, to avoid writing an AABB tree that is
534 lines in this very repository.

Unused if only the broadphase is taken: `collision.go` (518 lines — `GJK` at :360, `GJKRecurse` at
:380, `EPA` at :417, `EPARecurse` at :426, support functions, support edges, `ContactPoints`
manifold generation at :312), `arbiter.go` (531, sequential impulses with warm starting), the
eleven constraint files (`constraint.go` + ten joints, **917 lines**), and most of `space.go`
(1 131).

---

## 2. `ByteArena/box2d` — Box2D 2.3.2, published 2018

### 2.1 The TOI path survived the port intact

`CollisionB2TimeOfImpact.go` at `acbde41` is a faithful transcription: `B2TOIInput` (:8) with
`ProxyA/ProxyB/SweepA/SweepB/TMax`, `B2TOIOutput` (:36), `B2TimeOfImpact` (:284), the separation
function (:56 onwards) and the conservative-advancement loop. The bullet path in the world is
there too: `B2World.SolveTOI` (`DynamicsB2World.go:608`), the bullet gate (`:672–673`,
`collideA := bA.IsBullet() || typeA != B2_dynamicBody`), the minimum-TOI search (:628–728), the
advance-to-TOI (:733) and the dispatch from `Step` (:929–933). **Confirmed present and
structurally complete.**

It is usable as a **standalone swept query with no world**: two `B2DistanceProxy`s, two `B2Sweep`s,
one call. Measured, radius-0.5 circle swept from x = 0 to x = 10 against an edge at x = 5:

```
TOI state=4 (E_touching) t=0.450500   exact geometric answer 0.450000
```

And the game's own case, a radius-0 point swept 0.958 m across an edge:

```
point TOI state=4 t=0.516701          exact geometric answer 0.521920
```

**TOI is tolerance-based, not exact.** It targets a separation of `B2_linearSlop = 0.005`
(`CommonB2Settings.go:45`), so the reported impact is systematically ~5 mm off. §5's coincidence
nudge is **6 mm**; §11 asserts a wall-penetration band in *"low single-digit centimetres"*. A 5 mm
hard-coded tolerance sits inside the game's own smallest modelled quantity, and it is a package
constant, not a parameter.

Cost: **571 ns/op, 0 B/op, 0 allocs/op** — faster and cleaner than `cp`'s space query, because it
is a pure function over value types.

### 2.2 Two facts that disqualify it independently of its age

**`B2TimeOfImpact` mutates unsynchronised package-level globals on every call.**
`CollisionB2TimeOfImpact.go:53–55` declares `B2_toiTime, B2_toiMaxTime float64`,
`B2_toiCalls, B2_toiIters, B2_toiMaxIters int`, `B2_toiRootIters, B2_toiMaxRootIters int`, and the
function writes them: `B2_toiCalls++` (:288), `B2_toiRootIters++` (:424),
`B2_toiMaxIters = MaxInt(...)` (:472), `B2_toiTime += time` (:476). Verified at runtime:
`B2_toiCalls == 1` after one call from a test. cog schedules Systems concurrently; two Systems
calling this concurrently is a data race inside the library, unfixable from outside it.

**`B2_maxTranslation = 2.0` is a hard clamp on velocity** (`CommonB2Settings.go:78`), applied in
`DynamicsB2Island.go:308` and `:452`: any body whose `dt · v` exceeds 2.0 m has its velocity
silently scaled down. It is a compile-time constant. §10 requires that the tick rate stay a
parameter and that coefficients not silently depend on it; this is the opposite.

### 2.3 What it is missing, and what it is not

- **No shape cast.** `grep -rn "ShapeCast" *.go` returns nothing. Box2D gained `b2ShapeCast`
  (`include/box2d/b2_distance.h:100–120`) at **v2.4.0**, after the fork point.
- **`RayCast` has no radius.** `B2RayCastInput` is `{P1, P2, MaxFraction}`
  (`CollisionB2Collision.go:149`). Unlike Chipmunk there is no thick-ray shortcut, so swept
  circles must go through the full conservative-advancement TOI machinery.
- **`float64` throughout.** `grep -c float32` over the library source: zero hits. `m.Vec2` is
  `float32`.
- **17 513 non-test lines**, of which 5 413 are joints (`DynamicsB2Joint*.go` + `Rope`), 3 338 the
  contact solver and islands, and 2 129 hull narrowphase and distance — all of §9's "never asks
  for" list.
- **Dead.** `pushed_at` 2020-09-05, not archived, 11 open issues, 312 stars. No fork has become a
  maintained successor, though see `Alexander-r/box2d` in [§5](#5-what-the-survey-missed).

---

## 3. `SolarLune/resolv` — detection, and the library says so itself

Confirmed on both counts, and the library documents its own limit.

**No dynamics.** `grep -rn "Velocity|velocity|Mass|mass|force|Force"` over all non-example `.go`
files at `8b4e8c1` returns exactly one hit — the word "velocity" inside a doc comment describing
what a `Vector` might represent (`vector.go:22`). There is no mass, no force, no integrator, no
solver.

**No shape sweep.** The whole query surface is:

- `ShapeBase.IsIntersecting` / `Shape.Intersection` — static overlap returning an
  `IntersectionSet` with an MTV (`shape.go:245`, `circle.go:69`, `convexPolygon.go:288`).
- `ShapeBase.IntersectionTest(IntersectionTestSettings)` (`shape.go:273`) — the settings struct
  (`shape.go:250–258`) has **no motion vector**: a set of shapes and a callback. Purely static.
- `LineTest(LineTestSettings)` (`utils.go:274`) — an infinitely thin ray.
- `ConvexPolygon.ShapeLineTest(ShapeLineTestSettings)` (`convexPolygon.go:322`) — a *fan of rays
  cast from the polygon's vertices* along `settings.Vector` (`convexPolygon.go:304–315`). This is
  the closest thing to a sweep in the package and it is an approximation of one: it misses
  anything that fits between the vertex rays, and **it does not exist on `Circle`** — only
  `ConvexPolygon` has the method.

The doc comment on `NewSpace` (`space.go:13–15`) states the consequence:

> *"You want to move Objects at a maximum speed of one cell size per collision check to avoid
> missing any possible collisions."*

That is the tunnelling problem stated as a usage rule. §6 Q1 exists precisely because the game
cannot obey it.

Two further disqualifications found while reading:

- **Package-level mutable scratch state** reused across every call: `possibleIntersections`
  (`shape.go:265`), `lineTestResults` and `lineTestVertices` (`convexPolygon.go:317–318`),
  `intersectionSets` and `cellSelectionForEachIDSet` (`utils.go:255, 272`), plus `globalShapeID`
  (`shape.go:64`) and the `tagDirectory` map (`tags.go:66`). Concurrent queries from two Systems
  corrupt each other.
- **The space is an integer-sized grid** —
  `NewSpace(spaceWidth, spaceHeight, cellWidth, cellHeight int)` (`space.go:16`), cell sizes in
  `int` "pixels". §1's world is 512 m of `float32` metres on a 2 m grid; `resolv` is built for a
  pixel game.

---

## 4. The vocabulary claim (reason 3) — confirmed, and it is the strongest of the five

The claim: circle, AABB, point and segment with no rotation makes every pair test closed-form, so
GJK, EPA, SAT over hulls, manifold generation and warm starting are machinery this effort would
not use. Tested against what each library actually spends its narrowphase on.

**`cp`.** `collision.go` is 518 lines and it is *all* the machinery in question: `SupportPoint`
and `Support` on the Minkowski difference (:20–84), `GJK` (:360) and `GJKRecurse` (:380), `EPA`
(:417) and `EPARecurse` (:426), `SupportEdgeForSegment` / `SupportEdgeForPoly` (:265, :284),
`ContactPoints` manifold generation (:312). The dispatch table (`Collide`, :499) routes
`CircleToCircle` (:86), `CircleToSegment` (:109) and `SegmentToSegment` (:141) to closed-form
paths, and `CircleToPoly` (:164), `SegmentToPoly` (:177) and `PolyToPoly` (:196) into GJK/EPA.
**Three of six pair tests are closed form; the other three are exactly the poly cases §2 rules
out.** The library's own comment at `collision.go:417` says the expensive path is EPA and advises
adding radii to avoid it.

**`box2d`.** `CollisionB2Distance.go` (708 lines) is GJK plus the distance proxy;
`CollisionB2CollideEdge.go` (609), `CollisionB2CollidePolygon.go` and `CollisionB2ShapePolygon.go`
(479) are SAT-with-clipping manifold generation over hulls — 2 129 lines across the three.
`DynamicsB2ContactSolver.go` (907) is sequential impulses with warm starting and block solving.

**`gox2d`** (Box2D v3): `distance.go` is GJK and conservative advancement; `manifold.go`,
`hull.go` and the polygon narrowphase are the same hull machinery in the v3 idiom. Its
`ShapeCastSegment` is *not* closed form either — it is `MakeProxy` plus GJK, which is why its
answers carry a tolerance (§5.2).

**`resolv`.** `SATAxes` (`convexPolygon.go:235`), `Project` (:218), `calculateMTV` (:418),
`convexConvexTest` (`shape.go:438`) — SAT and MTV over arbitrary convex polygons, which is the
entire reason the package has a `ConvexPolygon` type.

**And the response model (reason 4), for completeness.** `cp`'s `Arbiter` computes
`arb.e = a.e * b.e` and `arb.u = a.u * b.u` (`arbiter.go:230–231`) — a restitution product and a
friction product — and clamps the tangent impulse with `jtMax := friction * con.jnAcc`
(`arbiter.go:154`): Coulomb friction, sequential impulses, warm-started. §5 prescribes a
per-sub-step-*rebuilt* acceleration accumulator of penetration springs at stiffness 25 and 10, one
tangential term scaled 0.7 by the lesser mass, LOS-gated, with **no restitution anywhere**. There
is no configuration of `cp`'s solver that becomes that. Adopting it means switching it off.

Reason 3 stands. The libraries spend their narrowphase precisely on the shapes §2 does not have.

---

## 5. What the survey missed

The seed issue named three candidates. A sweep of `pkg.go.dev`, `gh search repos` and
`gh api search/code` found that **Go ports of Box2D v3 exist — four of them, all from the last
twelve months** — and several other packages with real sweeps. `pkg.go.dev` is not a reliable
index for this question: its `box2d` search returns 25 results and includes **none** of `gox2d`,
`dbox2d` or `world-engine/pkg/box2d`.

| Repo | HEAD | Date | Licence | Pure Go | Swept circle vs segment | Dynamics |
| --- | --- | --- | --- | --- | --- | --- |
| **`neguse/gox2d`** | `8616afa` | 2026-03-15 | MIT | yes, hand-written | **yes** — `ShapeCastSegment`, `box2d/geometry.go:851` | full Box2D v3.1.1 |
| `Argus-Labs/world-engine` `pkg/box2d` | `e7c97c7` | 2026-09-10 | **repo root LGPL-3.0**; package LICENSE claims MIT+Zlib; **no `go.mod` in the package** | yes | **yes** — `ShapeCastSegment`, `pkg/box2d/geometry.go:967` | full v3.2.0 |
| `dhannyell/dbox2d` | `0a60d1b` | 2026-09-12 | MIT | yes | **yes** — `geometry.go` | v3.1.1, float32 **or** Q32.32 fixed point |
| `oliverbestmann/box2d-go` | `25cf4f4` | 2025-09-16 | MIT | no cgo, but **ccgo-transpiled** C — a 5 MB generated file, hand-rolled `libc`/TLS shims | yes | v3.1.1 verbatim |
| `Alexander-r/box2d` | `09ae655` | 2023-04-15 | Zlib | yes | **yes** — `B2ShapeCast`, `CollisionB2Distance.go:763`. This is the ByteArena port **advanced to Box2D 2.4.1**, which is where upstream added the shape cast | yes |
| `setanarut/cm` | `f1b7634` | 2026-07-26 | MIT | yes | **yes, as a thick raycast** — `(*Segment).SegmentQuery(a, b, r2, info)`, `segment.go:65`. A maintained fork of `cp`; **inherits the §1.2 broadphase defect verbatim** (`space.go:948–963`) | full Chipmunk |
| `setanarut/b2` | `9e712fc` | 2026-07-18 | Zlib-derived | yes | yes — Box2D 2.4.1, de-`b2`-prefixed | yes |
| `setanarut/coll` | `33316e1` | 2026-07-04 | MIT | yes | **no** — has `CircleCircleSweep2`, `BoxCircleSweep2`, `BoxSegmentSweep1`, but no circle-vs-segment sweep | none, by design |
| `matjam/bunyip` `phys/` | `01e7c3c` | 2026-09-06 | MIT | yes | **partial** — `ShapeCast2` (`phys/query2.go:94`) sweeps circles/boxes/polys, but there is **no `Segment` shape type** | yes |
| `git.sr.ht/~adnano/go-collide` | — | — | MIT | yes | **partial** — `TimeOfImpact` over `Circle`/`Polygon`, no segment, no shape cast | none |
| `tanema/ump` | `d5f261a` | 2017-10-18 | MIT | yes | **AABB only** — a bump.lua port; ray-vs-Minkowski-difference swept AABB, no circles, no segments | none |
| `zergon321/cirno` | `f13a757` | 2021-08-28 | MIT | yes | **no** — `Circlecast`/`Boxcast` take no direction or distance; they are overlap queries with misleading names | none |
| `vova616/chipmunk` | `c3710bb` | 2018-09-14 | MIT | yes | **no** — `SegmentQuery` exists only as an unimplemented interface method (`spatialIndex.go:76`) | partial Chipmunk |
| `oakmound/oak` `collision` | `ae32785` | 2026-07-28 | Apache-2.0 | yes | **no** — R-tree plus a thin raycaster | none |
| `EngoEngine/engo` `common` | `4d9de92` | 2024-07-12 | MIT | yes | **no** — static SAT | none |
| `TheBitDrifter/bappa` `spatial` | `5a25bdc` | 2025-11-17 | MIT | yes | **no, not analytic** — `continuousDetector.Check(…, steps int)` is fixed-step interpolation; its own comment warns it is *"computationally expensive… use sparingly"* | yes |

**No Go port of `erincatto/solver2d` exists.** `gh search repos "solver2d"` returns seven repos,
none in Go and none related; the six `search/code` hits for `solver2d language:go` are unrelated
packages that happen to share the name. Nothing on `pkg.go.dev`.

cgo bindings found and rejected as not pure Go, but named so they are not rediscovered:
`Advik-B/box2d-go`, `MortezaAsghariVostakolai/box2d` and `box2go`, `Mishka-Squat/box2d-go`,
`ianremmler/chipmunk`, `trichner/cp`, `koteyur/physac-go`, `gen2brain/raylib-go/physics`.

### 5.1 `neguse/gox2d` — the one that matters

MIT (`LICENSE`: *"Copyright (c) 2026 neguse / Portions copyright (c) 2022 Erin Catto (Box2D)"*),
module `github.com/neguse/gox2d`, `go 1.26.1`, **zero dependencies**. 60 non-test files,
29 574 lines, a hand-written port of Box2D v3.1.1 with its own test suite, determinism tests and
benchmarks. No tags, no releases, **one star**, one author, and the HEAD commit carries a
`Co-Authored-By: Claude` trailer.

It is the only thing found in Go that has the shape §12 asks for. Everything below was read or
measured at `8616afa`.

**Value types, `float32`, no pointer graph.**

| Type | Definition | Size |
| --- | --- | ---: |
| `Vec2` | `math.go:108`, `{X, Y float32}` | 8 B |
| `Segment` | `collision.go:168`, two `Vec2` | 16 B |
| `Capsule` | `collision.go:138`, two `Vec2` + radius | 20 B |
| `Circle` | `collision.go` | 12 B |
| `ShapeProxy` | `collision.go:72`, `Points [MaxPolygonVertices]Vec2` — a **fixed array**, not a slice | 80 B |
| `ShapeCastInput` | `collision.go:84` | 96 B |
| `CastOutput` | `collision.go:99`, `{Normal, Point Vec2; Fraction float32; Iterations int; Hit bool}` | 40 B |

Not one pointer among them. `m.Vec2` is `float32`; so is this.

**The casts are free functions over those values, with no world:**
`ShapeCastCircle` (`geometry.go:821`), `ShapeCastCapsule` (:836), `ShapeCastSegment` (:851),
`ShapeCastPolygon` (:866); plus `RayCastCircle` (:500) and `RayCastSegment` (:678).

**The broadphase inflates by the query radius — the defect `cp` has, fixed.**
`DynamicTreeShapeCast` (`dynamic_tree.go:1132`) builds the origin AABB, grows it by the proxy
radius (`:1147`), takes `extension := AABBExtents(originAABB)` (`:1151`) and adds that extension
to every node's half-extents in the separating-axis test (`:1198`,
`h := Add(AABBExtents(node.AABB), extension)`).

**The tree stores a caller-owned id, not a library body.**
`DynamicTreeCreateProxy(tree *DynamicTree, aabb AABB, categoryBits uint64, userData uint64) int`
(`dynamic_tree.go:655`) — `userData` is a plain `uint64`. That is where an `ecs.Entity` or a wall
cell index goes. There is no `*Body` anywhere in the index.

**Measured** (throwaway module, `go1.27.1`, same machine, same 4096-wall 512 m field as the `cp`
numbers; `-tags box2d_release` to disable the default-on assertions, `assert.go` /
`assert_release.go`):

| Benchmark | ns/op | B/op | allocs/op | `cp`, same workload |
| --- | ---: | ---: | ---: | --- |
| `ShapeCastSegment`, one segment, no broadphase | **137.8** | **0** | **0** | 75.1 ns, 96 B, 2 allocs (`Shape.SegmentQuery`) |
| `DynamicTreeShapeCast` + narrowphase, 0.958 m point sweep | **306** | 96 | 1 | 848 ns, 133 B, 2 allocs |
| `DynamicTreeRayCast` + `RayCastSegment`, full-map LOS trace | **712** | 24 | 1 | 2540 ns, 286 B, 5 allocs |

**2.8× faster on the sweep and 3.6× on the LOS trace, at a fifth of the allocated bytes.** Tree
height 15 over 4096 proxies, 960 000 bytes of nodes. The bare narrowphase is *slower* than `cp`'s
(138 ns against 75) because GJK conservative advancement is iterative where `cp`'s capsule test is
closed form — which is the same fact that produces the 5 mm tolerance below, and the same fact
that argues for writing the closed-form version.

The one allocation per tree query is the library's, not the caller's, and hoisting the input out
of the loop does not remove it: `dynamic_tree.go:1170` copies `subInput := *input` and passes
`&subInput` to an opaque callback func value, so escape analysis moves it to the heap —
`moved to heap: subInput` at `:1170` for the shape cast and `:1056` for the ray cast. Same class
of defect as `cp`'s, one allocation instead of two.

**Where it is not exact.** The casts are GJK conservative advancement, so the fraction carries
Box2D's linear slop and the normal carries GJK's tolerance:

| Case | reported | exact | error |
| --- | ---: | ---: | ---: |
| point vs segment, 0.958 m sweep | `0.516701` | `0.521920` | 5.0 mm early |
| point vs 0.4 m wall capsule | `0.318372` | `0.313152` | 5.0 mm early |
| circle r = 0.5 grazing a segment endpoint | `0.427066`, normal `(-0.5893, 0.8079)` | `0.425000`, normal `(-0.6, 0.8)` | 8 mm, normal off by 0.011 |

The contact *point* comes back exact (`{5, 0}`, `{4.8, 0}`) and the wall normal comes back exactly
`(-1, 0)` for the axis-aligned face cases. But §5's coincidence nudge is **6 mm** and §8 requires
reflection to preserve *"speed and angle exactly"* — a normal that is `(-0.5893, 0.8079)` where
the geometry says `(-0.6, 0.8)` does not flip exactly one component. The closed-form tests §2's
vocabulary permits have no such error.

---

## 6. Where the five reasons stand

| # | The map's reason | Verdict |
| --- | --- | --- |
| 1 | No candidate supplies the load-bearing query | **Overturned twice.** `cp` supplies Q1/Q2 as an exact swept circle and six of §6's nine queries directly. `gox2d` supplies the sweep as a zero-allocation `float32` free function with a radius-correct broadphase. |
| 2 | A Component holds no pointer, transitively | **Confirmed for `cp`** — `Shape` 168 B, 5 of 13 fields pointer-bearing; `Body` 304 B, 9 of 25; unexported fields make literal construction impossible. **Not true of `gox2d`**, whose shapes and casts are pure values. |
| 3 | The shape vocabulary deletes the machinery a library is worth | **Confirmed, strongest of the five.** 518 lines of GJK/EPA in `cp`, 2 129 of hull narrowphase in `box2d`, SAT+MTV as `resolv`'s whole reason for existing — and `gox2d`'s own sweep is GJK, which is *why* it carries a 5 mm tolerance the closed-form version would not. |
| 4 | The response model is wrong in both live candidates | **Confirmed.** `arbiter.go:154, 230–231` — restitution products, Coulomb clamp, warm-started sequential impulses. |
| 5 | §9 deletes gravity, joints, rotation, hulls | **Confirmed.** 917 lines of joints in `cp`, 5 413 in `ByteArena/box2d`, 9 862 lines of joints and solver in `gox2d` — against 5 521 lines of the world-free geometry, distance, tree and math that a query layer would actually use. |

**The decision to write our own survives, on reasons 2–5 rather than on reason 1.** Restate
reason 1 honestly: *the load-bearing query exists in Go, in two lineages, and it is the smallest
part of either library.* The swept-circle-vs-capsule test is **~45 lines of closed-form algebra**
(`cp`'s `segment.go:110–155` plus `circle.go:72–91` are the reference for the shape of it), and
writing it closed-form is what removes the 5 mm tolerance `gox2d` inherits from GJK.

Three costs the map did not know about, which are what now carry it:

- **Every `cp` query allocates** — 2 allocs/op at both levels, caused by `any`-typed callback
  plumbing and the `ShapeClass` interface, neither fixable from outside the package. §6's
  highest-volume query runs once per candidate contact pair per tick.
- **`cp`'s swept query is unsound above the shape level for radius > 0** (§1.2). Projectile sweeps
  are safe; character and body sweeps are not. The maintained fork inherits it.
- **`cp` and `ByteArena/box2d` are `float64`**; `m.Vec2` is `float32`. `gox2d` is not — and that,
  plus its value types, is why it is the one candidate that would fit at all.

---

## The exit condition

The map's candidate is *"a requirement arrives for rotation, joints, or arbitrary convex shapes."*
Right instinct, wrong grain: it is a judgement call about a hypothetical requirement, and
two-thirds of it is cheaper than it sounds. **Replace it with three observations, each of which
someone can notice without deciding anything — and note that the third has already fired once.**

**A. A support function appears in `physics2d`.** The moment any pair test cannot be written in
closed form, the next thing written is a support function — and once you have `Support(n) Vector`
you are three hundred lines from GJK and five hundred from EPA, which is `cp`'s `collision.go`
re-derived worse. **The observation:** a function in `physics2d` whose signature is
`(direction) → point on the shape`, or a shape type in the vocabulary that is not one of
{circle, AABB, point, segment}. This is the map's "arbitrary convex shapes" trigger made
countable, and it fires *before* the hulls arrive rather than after.

**B. Angular state enters a Component.** Not joints on their own — a distance or pin constraint
between two point masses with no rotation is closed-form and cheap. What is expensive is inertia:
the moment a Component carries an angle or an angular velocity, contact points stop being
interchangeable with body centres, manifolds acquire lever arms, and warm starting starts paying
for itself. **The observation:** a field of type angle or angular velocity in any `physics2d` or
`ecsphysics2d` Component. §1's *"no angular dynamics anywhere"* is the thing being watched, and
this is where it would first show.

**C. A pure-Go shape cast lands as a free function over value types — and it already has.** This
was going to be the forward-looking trigger. It is instead a fact:
`neguse/gox2d`'s `ShapeCastSegment` (`box2d/geometry.go:851`, commit `8616afa`, 2026-03-15) is
MIT, dependency-free, `float32`, zero-allocation, and its `DynamicTree` indexes a caller-owned
`uint64` with a radius-correct traversal. Adoption still loses **today**, for two reasons that are
themselves observations rather than judgements:

- **Maturity.** No tags, no releases, one star, one author, zero known dependents, six months old.
  cog would be its first external user, on an engine's critical path.
- **Exactness.** Its casts are GJK conservative advancement, measured **5 mm** conservative on the
  fraction and 0.011 off on a grazing normal. §5's coincidence nudge is 6 mm and §8 wants
  reflection exact. The closed-form version §2's vocabulary permits has neither error.

**So state the trigger as what would remove those two, and check it rather than re-deriving it:**
a pure-Go, permissively licensed shape-cast package that (i) has tagged releases and dependents
beyond its author, and (ii) reports an *exact* fraction and normal for circle-vs-segment rather
than a slop-tolerant one. Whoever looks next should start from
[§5](#5-what-the-survey-missed)'s table and check `neguse/gox2d`, `Argus-Labs/world-engine`
`pkg/box2d` (blocked today by an **LGPL-3.0 module root** with no `go.mod` of its own in the
package) and `dhannyell/dbox2d` — the same three, one year on. If that trigger fires, what reopens
is the **query layer only**, which is a swap; reasons 2–5 keep the plugin, the response System and
the Components ours regardless.

**What does not reopen it, and should not be mistaken for a trigger:** `cp` becoming
better-maintained (it already is — `v2.4.0`, 2025-12-26), `cp` gaining solver CCD, or
`ByteArena/box2d` being revived. None of those touches reasons 2–5, and none makes a 168-byte
pointer-bearing `Shape` legal in a Component.

**Two things to take as notes rather than dependencies.**

- `cp`'s radius-thickened segment query is the right *shape of API* for Q1 and Q2: one call, one
  radius parameter, radius 0 collapsing to the projectile case with no special path. §6 treats the
  point sweep and the circle sweep as two rows of a table; they are one function with `r = 0`.
- `gox2d`'s `DynamicTreeShapeCast` is the right *broadphase contract*: inflate the traversal AABB
  by the proxy radius before descending (`dynamic_tree.go:1147, 1151, 1198`), and store a
  caller-owned id in the leaf rather than a library object (`:655`). Doing the first is what `cp`
  gets wrong; doing the second is what makes the index legal next to an ECS.
