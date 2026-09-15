# What a smart ECS port of Chipmunk starts from

Research for the physics map, [Physics plugin: a smart ECS port of Chipmunk](https://github.com/dvoyni/cog/issues/182), taken on 2026-09-15 when the map's owner redrew its destination from "nox's minimal physics" to "a fully functional 2D engine like cp, out of the box, on the ECS".

Two things were done:

1. **An inventory of `jakecoffman/cp`** (Go port of Chipmunk 7): every algorithm, the exact step order, the data layout, the constants, and where the Go port departs from C. Each ticket on the map ports from here and cites these lines.
2. **Measurements** of `cp`'s step and queries, and of wrapping a Box2D v3 world (`neguse/gox2d`) behind the ECS, on one shared scene, so the owner could choose between porting and wrapping on numbers.

Sources read: `github.com/jakecoffman/cp/v2` v2.4.0 at `64b09f584d36466266282dcfe80c5b9651592b44` (MIT, "Copyright (c) 2017 Jake Coffman"); `slembcke/Chipmunk2D` at `f2f3d66220b8bb1568b8402c6d194d73d4e477d1` (MIT, "Copyright (c) 2007-2015 Scott Lembcke and Howling Moon Software"); `github.com/neguse/gox2d` at `8616afaae132b1d777a11ca406099b85bde248cc` (MIT). Citations are Go `file:line` in `cp` unless marked **C**. Measurements were taken in throwaway modules outside cog's tree; nothing was added to cog's `go.mod`. No race detector builds in this environment.

```
go version go1.27.1 windows/amd64
AMD Ryzen 9 7950X3D 16-Core Processor, GOMAXPROCS=32
```

## Defects found in cp

These are what the port fixes rather than inherits.

1. **`CircleToPoly` sign bug.** The poly contact point is offset by `+poly.r` (`collision.go:173`); C uses `-poly.r` (**C** `cpCollision.c:677`). A circle against a rounded polygon settles about 2·r too deep. Harmless at poly radius 0.
2. **Every step allocates**: 164 allocs / 12.5 KB at N=256 and 656 allocs / 49.8 KB at N=1024. All of it is `Collide`'s `CollisionInfo` escaping (`collision.go:500`, once per narrowphase call) and `LookupHandler` returning `&CollisionHandler{}` (`space.go:841`, once per touching arbiter).
3. **The static BBTree is badly unbalanced.** 4 224 grid segments inserted in order give mean leaf depth 37.5 and max 75 (balanced ≈ 13). The Go port has no `Optimize`, no `ReindexStatic`, no rotations, and `BBTree.Reindex` panics "implement me" (`bbtree.go:318-320`).
4. **Segment neighbours are missing.** No `SetNeighbors` (**C** `cpShape.c:539`); `a_tangent`/`b_tangent` are always zero (`segment.go:167-168`), so end-cap rejection (`collision.go:134-135, 156-159, 190-191`) never fires and chains of segments catch at joints.
5. **Space-level sweeps with radius > 0 miss hits**, in C too: the broadphase receives the un-inflated ray (`space.go:1045-1046`; **C** `cpSpaceQuery.c:172-173`). Against brute force over 1 024 one-metre sweeps at r = 0.3: 187 of 918 hits missed and 87 returned a later hit (N=256); 201 of 910 missed and 100 later (N=1024). No misses at r = 0.
6. Smaller departures from C: `ClosestT` lacks `+CPFLOAT_MIN` (`vector.go:165`); poly `SegmentQuery` lacks `max(an-bn, CPFLOAT_MIN)` (`poly.go:129`); circle `PointQuery` divides by `d` unguarded (`circle.go:58`); `SpaceHash.Reindex` clears but never rehashes (`spacehash.go:103-108`); `CONTACTS_BUFFER_SIZE` is 1 024 (≈98 KB a buffer) against ≈340 in C; `INFINITY` is `math.MaxFloat64`, not +Inf (`everything.go:9`). No `Space.PointQuery` (all shapes within a distance), no Sweep1D, no threaded space. Segment normals use rperp in the constructor and perp in `SetEndpoints`, in C as well (`segment.go:164` vs `:65`).
7. **Default slop is 0.1** (`space.go:71`), sized for pixels. In metres it allows a third of a 0.3 m radius as overlap.

## 1. Files

7 400 non-test lines.

| Group | Files (lines) |
| --- | --- |
| Math | `vector.go` 185, `bb.go` 173, `transform.go` 123, `everything.go` 396 (constants; `Contact`, `CollisionInfo`, `ShapeFilter`; mass helpers; `k_scalar`/`k_tensor`/`bias_coef` at `:339-394`) |
| Shapes | `shape.go` 275 (`Shape`, `ShapeClass` interface, `Order()` type switch, `Shape.SegmentQuery`, `ShapesCollide`); `circle.go` 92; `segment.go` 181 (radius `r`); `poly.go` 352 (radius `r`; `NewBox`/`NewBox2` `:181-202`; `SetVerts` `:204`; QuickHull `ConvexHull` `:248-352`) |
| Mass, moment, area | `everything.go:204-302` (`MomentForBox/Box2/Circle/Segment/Poly`, `AreaForCircle/Segment/Poly`, `CentroidForPoly`); per-shape mass info `circle.go:20`, `segment.go:174`, `poly.go:238` |
| Collision | `collision.go` 518: support functions `:26-59`; `CircleToCircle` `:86`, `CircleToSegment` `:109`, `SegmentToSegment` `:141`, `CircleToPoly` `:164`, `SegmentToPoly` `:177`, `PolyToPoly` `:196`; Minkowski/`ClosestPoints` `:210-251`; `SupportEdgeFor*` `:265-309`; `ContactPoints` (manifold clip) `:312-351`; `HashPair` `:355`; `GJK` `:360`, `EPA` `:418`; dispatch table `:486`, `Collide` `:499` |
| Arbiter | `arbiter.go` 531, `hashset_arbiter.go` 60, `contactbuffer.go` 29 |
| Body | `body.go` 628 |
| Space | `space.go` 1 131 (step, add/remove, sleeping, queries) |
| Indices | `bbtree.go` 534, `spacehash.go` 399, `spatialindex.go` 66, `hashset.go` 229 |
| Sleeping | `space.go:150-239, 525-656`; `body.go:369-480` |
| Constraints | `constraint.go` 101; `pinjoint` 87, `slidejoint` 90, `pivotjoint` 83, `groovejoint` 98, `dampedspring` 92, `dampedrotaryspring` 69, `rotarylimitjoint` 81, `ratchetjoint` 89, `gearjoint` 70, `simplemotor` 57 |
| Handlers | `arbiter.go:360-404`, `space.go:840-868`, `everything.go:44-83` |
| Autogeometry | `march.go` 191, `polyline.go` 188 |
| Other | `draw.go` 186 (debug draw through a `Drawer` interface) |

## 2. `Space.Step`

`space.go:665-771`, in the same order as **C** `cpSpaceStep.c:336-445`.

1. `dt == 0` returns; `stamp++`; `prev_dt`, `curr_dt` (`:666-673`).
2. **Reset arbiters** (`:676-684`): `state = NORMAL`, unthread from bodies' arbiter lists, truncate `space.arbiters`.
3. Lock.
4. **Integrate positions**: `position_func` per dynamic and kinematic body (`:689-691`), writing `p`, `a`, the transform, and zeroing `v_bias`/`w_bias`.
5. `PushFreshContactBuffer` (`:694`, `:472-487`).
6. **Update shape caches**: `Shape.Update` → `Class.CacheData(body.transform)` writes circle `tc`, segment `ta/tb/tn`, poly world planes and `shape.bb` (`:695`).
7. **Pairs**: `dynamicShapes.ReindexQuery(SpaceCollideShapesFunc)` (`:696`; `bbtree.go:332-356`). A leaf is reinserted only when its bb escapes its fat bb. Per pair, `SpaceCollideShapesFunc` (`:408-470`) runs `QueryReject` (same body, filter, bb, constraint list), `Collide`, caches the arbiter keyed by the shape-pointer pair, and runs `arb.Update` (`arbiter.go:191-260`): `r1`/`r2` relative to `body.p`; `jnAcc`/`jtAcc` carried from old contacts with the same hash; `n`, `e`, `u`, `surface_vr`; handler lookup; Begin (first collision only) then PreSolve. The arbiter joins `space.arbiters` unless ignored, a sensor, or both masses infinite.
8. `Unlock(false)`.
9. `ProcessComponents` (`:701`, `:525-615`): pushes every arbiter onto both bodies' lists; sleep work only when the sleep threshold is not infinite.
10. Lock.
11. **Arbiter filter** (`:706`; `hashset_arbiter.go:6-34`): after one untouched tick an arbiter is CACHED and its Separate callback runs; after `collisionPersistence` untouched ticks it is removed.
12. **Prestep**: arbiters `PreStep(dt, slop, 1 - collisionBias^dt)` (`:711-715`), then constraints `PreSolve` and `Class.PreStep(dt)` (`:717-723`).
13. **Integrate velocities**: `damping = space.damping^dt`; `velocity_func` per dynamic body (`:726-730`).
14. **Warm start**: `dt_coef = dt/prev_dt` (0 on the first step); arbiters then constraints `ApplyCachedImpulse` (`:733-744`).
15. **Iterations**: each iteration applies every arbiter's impulse, then every constraint's (`:747-756`).
16. **Post-solve** callbacks: constraints, then arbiters (`:759-768`).
17. `Unlock(true)`: activates roused bodies and runs post-step callbacks (`:770`, `:777-810`).

**Order: positions first (from last step's velocity and bias velocity), then detection, then velocities.**

## 3. Body integration

- **Velocity** (`body.go:608-618`), kinematic bodies skipped: `v = v·damping + (gravity + f·m_inv)·dt`, `w = w·damping + t·i_inv·dt`, then `f` and `t` are zeroed. Damping is **global only**, raised to `dt` (`space.go:726`), applied to both `v` and `w`.
- **Position** (`body.go:621-628`): `p += (v + v_bias)·dt`, `a += (w + w_bias)·dt`, set the transform, zero the biases.
- **Overrides**: per-body function pointers (`body.go:555-562`).
- **Centre of gravity**: `p` is the world CoG; `transform = [R | p − R·cog]` (`body.go:359-367`); `Position()` returns the body origin (`:280`); `SetPosition` stores `p = R·cog + pos` (`:285-289`).
- **Mass from shapes**: `AccumulateMassFromShapes` (`:232-260`) mass-weights the shapes' cogs and sums `m·info.i` with a parallel-axis term. Shape mass defaults to 0, so `NewBody(m, i)` stands.
- **Kinds**: static when `sleepingIdleTime == INFINITY`, kinematic when `m == INFINITY` (`:221-229`); static and kinematic set `m = i = INFINITY`, `m_inv = i_inv = 0`, `v = w = 0` (`:167-175`). Static bodies are never integrated.

## 4. Arbiter and contact solver

- **`Contact`** (`everything.go:85-95`, 96 B): `r1, r2, nMass, tMass, bounce, jnAcc, jtAcc, jBias, bias, hash`.
- **`Arbiter`** (`arbiter.go:11-32`): `e, u, surface_vr`, user data, shape and body pointers, intrusive threads, `count`, `contacts` (a slice into the contact buffer), `n`, handlers, `swapped`, `stamp`, `state`.
- **States** (`everything.go:19-31`): FIRST_COLLISION, NORMAL, IGNORE, CACHED, INVALIDATED.
- **Persistence**: a hash set keyed by the unordered shape-pointer pair (`space.go:59-64, 430-436`); expiry after 3 untouched ticks (`hashset_arbiter.go:26`); contacts matched by `hash` (`arbiter.go:214-223`), which is 0 for single-contact pairs and the edge vertex ids for GJK pairs (`collision.go:338, 347`).
- **PreStep** (`arbiter.go:168-189`): `nMass = 1/k(n)`, `tMass = 1/k(perp n)` with `k = m_inv + i_inv·(r×n)²` summed over both bodies (`everything.go:339-346`); `dist = (r2 − r1 + pb − pa)·n`; `bias = −biasCoef·min(0, dist + slop)/dt` with `biasCoef = 1 − 0.9^(60·dt)` (0.1 at 60 Hz); `jBias = 0`; `bounce = vrn·e`, taken before velocity integration.
- **ApplyCachedImpulse** (`:113-123`): skipped on first collision; applies `R(n)·(jnAcc, jtAcc)·dt_coef`.
- **ApplyImpulse** (`:125-162`):
  - bias: `jBias = max(jBias + (bias − vbn)·nMass, 0)`, applied to `v_bias`/`w_bias` only (split impulse, not warm-started);
  - normal: `jnAcc = max(jnAcc − (bounce + vrn)·nMass, 0)`;
  - friction: `jtAcc` clamped to `±u·jnAcc`;
  - `vr` includes `surface_vr`, the tangential part of `b.surfaceV − a.surfaceV` (`:233-234`).
- **Mixing**: `e = ea·eb`, `u = ua·ub` (`:230-231`). At most 2 contacts per arbiter (`space.go:11`).

## 5. Collision per pair

Shapes are sorted circle < segment < poly; the normal points from `a` to `b`; `r1` lies on `a`'s surface and `r2` on `b`'s; `dist < 0` is overlap.

| Pair | Method | Contacts | Notes |
| --- | --- | --- | --- |
| circle–circle | closed form (`:86-103`) | 1 | coincident centres give `n = (1, 0)` |
| circle–segment | closed form (`:109-139`) | 1 | segment radius included; end-cap rejection dead (defect 4) |
| segment–segment | GJK, EPA only if cores intersect, `ContactPoints` clip (`:141-162`) | ≤ 2 | |
| circle–poly | GJK/EPA (`:164-175`) | 1 | sign bug (defect 1) |
| segment–poly | GJK/EPA + clip (`:177-194`) | ≤ 2 | |
| poly–poly | GJK/EPA + clip (`:196-207`) | ≤ 2 | |

- Rounded shapes: GJK runs on the cores; radii are subtracted from the distance and contact points pushed out along `n` (`:334-335, 343-344`).
- GJK warm start: a cached `collisionId` on the BBTree pair (`:363-366`); a pair re-found through the static tree starts at 0 (`bbtree.go:182`).
- Tolerances: GJK/EPA 30 iterations, warn at 20 (`:9-11`), warnings through `log.Println` (`:479`); `1e-15` in `Normalize` (`vector.go:89`), vertex–vertex normals (`:248`) and clip denominators (`:328-329`); exact float equality in `points.a.Equal(ta)` and `closestT != 0/1`; a clipped point is kept at `dist <= 0`.

## 6. Queries

- **Shape level**: `Shape.SegmentQuery` (`shape.go:209-228`) first point-queries the start; inside the radius it returns alpha 0, normal `normalize(a − nearest)`, and leaves `Point = b`.
  - circle: point query closed form (`circle.go:52-66`); segment query the `CircleSegmentQuery` quadratic (`:72-92`). **Exact.**
  - segment: point query closed form (`segment.go:86-108`); segment query offset faces plus two cap circles, caps only when the span test fails (`:110-158`). **Closed form.**
  - poly: point query O(count), exact to the rounded shape (`poly.go:66-112`); segment query offset face planes then vertex circles when `rsum > 0` (`:114-157`). **Closed form, so a swept circle against a rotated polygon stays exact.**
- **Space level**: `PointQueryNearest` (`space.go:940-962`), exact, skips sensors; `SegmentQuery` / `SegmentQueryFirst` (`:1032-1048`), static tree then dynamic with `t_exit = best alpha`, skips sensors, un-inflated traversal (defect 5); `BBQuery` (`:980-988`), AABB only; `ShapeQuery` (`:1084-1120`), exact, allocates `make([]Contact, 2)` (`shape.go:246`).

## 7. Spatial indices

- Two BBTrees, static and dynamic; the dynamic one carries a velocity function and the static tree (`space.go:77, 103-104`). `UseSpatialHash` swaps both for `SpaceHash` (`:870-883`).
- **BBTree**: `Node{obj *Shape, bb, parent, a, b, stamp, pairs *Pair}`, a leaf when `obj != nil` (`bbtree.go:7-33`); leaves found through `HashSet[*Shape, *Node]`; per-leaf doubly linked `Pair` lists caching a GJK id; free lists grown 1 024 at a time. Fat AABB: 10% of the extent each side plus `0.1·v` (`:454-471`). Insert by area cost, no rebalancing (`:242-266`). The dynamic tree reindexes every step and reinserts only escaping leaves (`:367-383`); the static tree changes only through `Space.ReindexShape` (`space.go:1123`). Pair generation: a moved leaf queries the static root then sibling subtrees; an unmoved leaf replays its cached pairs (`:146-172`).
- **SpaceHash**: bins of `*Handle{obj, retains, stamp}` lists; cleared and rehashed every `ReindexQuery` (`spacehash.go:167-203`); statics queried per dynamic object (`spatialindex.go:60-66`); handles from a `sync.Pool`.

## 8. Sleeping and islands

- `dynamicBodies []*Body`; `sleepingComponents []*Body` (component roots) linked through `body.sleepingRoot`/`sleepingNext` (`body.go:63-65, 466-480`). The contact graph is intrusive (`body.arbiterList` plus arbiter threads; `constraintList` plus `next_a`/`next_b`). Flood fill is a recursive DFS (`space.go:626-656`).
- `IdleSpeedThreshold` defaults to 0, meaning `|g|·dt`, so with no gravity only exactly zero kinetic energy is idle (`:530-553`). `SleepTimeThreshold` defaults to `MaxFloat64`, i.e. off (`:82, 526`).
- A component sleeps when every member's idle time reaches the threshold (`:617-624`); `Deactivate` moves its shapes into the static tree and copies its arbiters' contacts to the heap (`:207-239`).
- Wake: every body or shape setter calls `Activate`; `SetGravity` wakes all (`:119-126`); adding a non-static shape, adding or removing a constraint, removing a body or shape; touching a kinematic or sleeping body (`:563-568`); a constraint to a kinematic body (`:577-584`).

## 9. Constraints

- `Constrainer{PreStep(dt), ApplyCachedImpulse(dt_coef), ApplyImpulse(dt), GetImpulse()}` (`constraint.go:5-10`). `Constraint` holds both bodies, list links, `maxForce` (∞), `errorBias` (0.9^60), `maxBias` (∞), `collideBodies`, PreSolve/PostSolve and user data (`:15-46`). Every joint matches C line for line (C additionally asserts `moment != 0` in the rotary spring).

| Joint | Lines | Needs rotation? |
| --- | ---: | --- |
| pin | 87 | no, with anchors at the CoG |
| slide | 90 | no, with anchors at the CoG |
| pivot | 83 | no, with anchors at the CoG |
| damped spring | 92 | no, with anchors at the CoG |
| groove | 98 | yes: body a's transform sets the groove and `r1` is a lever (`groovejoint.go:293-312`) |
| damped rotary spring | 69 | purely angular |
| rotary limit | 81 | purely angular |
| ratchet | 89 | purely angular |
| gear | 70 | purely angular |
| simple motor | 57 | purely angular |

With anchors at the CoG, `r = 0` and the angular terms vanish, **but `i_inv` must be 0, not +Inf**: `Inf·0` is NaN in `k_scalar`, `k_tensor` and `apply_impulses` (`everything.go:341, 363-378`; `arbiter.go:329, 335`).

## 10. Shape offset and centre of gravity

- Circle `c`, segment `a/b/n` and poly vertices and normals are in body-local coordinates relative to the **body origin, not the CoG** (`circle.go:7, 30`; `segment.go:6, 14-16`; `poly.go:12-13, 38-53`), transformed to world every step.
- Shape cogs: circle = its offset, segment = midpoint, poly = centroid.
- **One shape with mass > 0**: body `m` = shape `m`, `i = m·info.i`, cog = shape cog, so the body turns about the shape's centre.
- **One shape with mass 0** (`NewBody(m, i)`): cog stays at the origin; an offset has to go into `MomentForCircle`'s offset term (`everything.go:222-224`).
- Contact `r1`/`r2` are relative to `body.p`, the CoG (`arbiter.go:207-208`).

## 11. Data layout, for an ECS port

- **Portable as free functions over values**: vector, BB and transform math; shape `CacheData`; point and segment queries; `CircleSegmentQuery`; mass helpers; GJK, EPA and `ContactPoints` given support data as values; arbiter PreStep and impulses and every joint, given bodies as indexable arrays; integration.
- **Pointer-walking**: `Shape{Class, *Space, *Body, *ShapeMassInfo}` with `Circle{*Shape}` back-embeds; `Arbiter` holds shape and body pointers and intrusive threads; `Body` holds shape, arbiter and constraint lists, sleeping links and `*Space`; BBTree and HashSet are linked nodes; arbiter keys are pointer addresses; collision functions read `body.Rotation()`, `shape.hashid` and `bb` through pointers.
- **Indirection on hot paths**: per pair, `SpatialIndexQuery` with `any` arguments and type assertions (`space.go:409-410`), the collision function table, two `Shape.Order()` type switches, `Class.(*Circle)` assertions, `SupportPointFunc` values inside recursive GJK; per arbiter per step, handler funcs; per shape, the `ShapeClass` interface; per constraint per iteration, the `Constrainer` interface; per body, position and velocity function pointers.

## 12. Constants, and float32

| Constant | Default | Where |
| --- | --- | --- |
| iterations | 10 | `space.go:68` |
| damping | 1 | `:70` |
| collision slop | 0.1 | `:71` |
| collision bias | 0.9^60 ≈ 0.001797 | `:72` |
| collision persistence | 3 | `:73` |
| sleep time threshold | `MaxFloat64` | `:82` |
| idle speed threshold | 0 | `:83` |
| constraint error bias | 0.9^60 | `constraint.go:39` |
| contacts per arbiter / buffer | 2 / 1 024 | `space.go:11-12` |
| `MAGIC_EPSILON` | 1e-5 | `everything.go:10` |
| `INFINITY` | `MaxFloat64` | `everything.go:9` |
| GJK / EPA iterations | 30 / 30, warn 20 | `collision.go:9-11` |
| tree margin | 0.1·extent and 0.1·v | `bbtree.go:457, 461` |
| fallback normals | (1, 0), (0, 1) | `collision.go:99`; `circle.go:64` |

**What breaks in float32** (cog's `m.Vec2` is float32): `INFINITY = MaxFloat64` does not fit and is compared exactly to classify bodies (`body.go:222-225`; `space.go:453, 526`); the `1e-15` epsilons do nothing except against exact 0; `MAGIC_EPSILON` 1e-5 is about the float32 spacing at 128 m (7.6e-6); exact equality in the end-cap test; `CheckPointGreater` is a product of differences, so it flips sign sooner near degeneracy; `CircleSegmentQuery`'s `qb² − qa·c` cancels; positions integrate absolutely (`p += v·dt`); `m = 0` gives `m_inv = +Inf`; slop, tree margins and thresholds are absolute lengths.

## 13. Measurements

### Scene

Shared by both libraries: a 128 m × 128 m area with grid lines every 4 m cut into **4 224 zero-radius 2 m static segments**; N dynamic circles r = 0.3 m, mass 10 kg; no gravity; linear drag 0.25/s; a random force of 0–100 N per body per tick; dt = 1/60; sleeping off; 120 warm-up ticks; `-count=5 -benchmem`, medians.

- cp: segments on `space.StaticBody`; `NewBody(10, MomentForCircle(10, 0, 0.3, 0))`; `SetDamping(exp(-0.25))`, i.e. ×0.995842 per step on both `v` and `w`; shape friction √0.6 (pair 0.6, Box2D's default); elasticity 0; iterations 10; slop, bias and persistence at defaults. Arbiters touching, averaged over 600 ticks: 44.5 at N=256 (16.4 against walls), 177.2 at N=1024 (66.0 against walls).
- gox2d: one static body holding every segment (4 224 static bodies measured 2–4% slower); 4 sub-steps; contact events enabled on circles; touching contacts 60.6 body–body + 41.3 body–wall at N=256, 245 + 171 at N=1024 (Box2D counts differently from cp's arbiters).

### Step

| | N=256 | N=1024 |
| --- | ---: | ---: |
| **cp** `Space.Step` + forces | **60.9 µs**, 164 allocs, 12.5 KB | **284 µs**, 656 allocs, 49.8 KB |
| **gox2d** `World_Step` single-threaded + forces + move events | 159 µs, 8 allocs, 17 KB | 651 µs, 8 allocs, 62.5 KB |
| gox2d `World_Step`, 4 workers | 171 µs | 443 µs |

gox2d's per-step allocations are mostly one porting bug: `World_Step` truncates `BodyMoveEvents` to length 0 (`world_step.go:567`) and `Solve` then checks `len < awakeBodyCount` (`solver.go:1045`) and reallocates; checking capacity, as C does, took N=1024 to 13.3 KB and 7 allocs with no time change. Its built-in scheduler spawns goroutines per task with no pool, and is slower than one thread at N=256 at every worker count.

### Queries, 1 m, origins clear of geometry

| | N=256 | N=1024 |
| --- | ---: | ---: |
| cp `SegmentQueryFirst` r = 0 | 2 894 ns, 4 allocs | 3 173 ns, 4 allocs |
| cp `SegmentQueryFirst` r = 0.3 | 2 870 ns, 4 allocs, hit rate 0.729 against brute force 0.896 | 3 125 ns, 4 allocs |
| gox2d `World_CastRayClosest` | 600 ns, 3 allocs | 761 ns, 3 allocs |
| gox2d `World_CastShape` circle r = 0.3 | 989 ns, 2 allocs | 1 169 ns, 2 allocs |
| cog's hashed grid, measured by [prototype #345](https://github.com/dvoyni/cog/issues/345) | short sweep 30–50 ns, 0 allocs | |

cp's four allocations are escaped `info`, `context`, `blank` and `nearest` (`space.go:1043-1044`; `shape.go:210, 217`).

### What wrapping gox2d behind the ECS would cost, at N=1024

| Part | Cost |
| --- | ---: |
| Sync in: `SetLinearVelocity` + `ApplyForceToCenter` | 10.6 ns/body in Box2D, ≈ 14 µs with the ECS walk (≈ 3.4 ns/Entity) |
| Sync out: move events (transforms only, every awake body) | ≈ 1 µs |
| Sync out: velocity getters | ≈ 8 µs |
| Contact begin/end events into a list | ≈ 0.05 µs |
| Create + destroy one body with one circle | 0.4–0.6 µs |
| Scheduling a handful of Systems | ≈ 23 µs |
| **Wrapper total** | **≈ 50–60 µs over the step** |

gox2d API facts that bear on it: `BodyID{Index1 int32; World0 uint16; Generation uint16}` (`id.go:31`) and `ShapeID` are 8 bytes with no pointers; user data is `any` everywhere, so storing an integer id boxes it; `World_Step(worldID, timeStep, subStepCount)` sets `world.Locked` and queries read that plain bool, so querying during a step races; queries write nothing in the world struct, so concurrent queries between steps look safe by reading (unverified without the race detector); ids index a process-global `var worlds [128]World`, and `CreateWorld` writes package globals unsynchronised; the package builds for `GOOS=js GOARCH=wasm` (2.9 MB, not run). A trivial cgo call to C Box2D costs 22.0 ns against 0.99 ns for a Go call, and does not build for the browser.

The wrapper was not chosen: it mirrors state the map says Components own, its queries are 20× the grid's and allocate, and gox2d is one author's unreleased port. The owner chose a port of cp onto the ECS.
