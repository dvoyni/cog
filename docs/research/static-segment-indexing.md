# How engines index static grid-aligned wall segments, and what a door costs each one

Research for [dvoyni/cog#285](https://github.com/dvoyni/cog/issues/285), child of the
physics map [#182](https://github.com/dvoyni/cog/issues/182).

**Two numbering schemes are in play, so they are kept apart.** A reference written
`requirements §6` is to the map's *Requirements from the driving game* comment; `Q1`…`Q9` are
its query table. A reference written `§3.2` with no qualifier is a section of this document.

The question: **how do working engines index static, grid-aligned segment geometry so that a
swept query is cheap, a mutation is cheap, and a hit is attributable back to the authored
cell?**

Every claim below is cited to engine source, an original paper, or a first-party document.
Where a source publishes a number it is quoted with its hardware and scene. **Where nobody
publishes a number, this says so** rather than estimating; those gaps are collected in
[What nobody publishes](#9-what-nobody-publishes-and-what-would-have-to-be-measured).

---

## 0. The findings, first

1. **Nobody ships a general acceleration structure for this shape of problem, because the
   grid *is* the structure.** Doom, Wolfenstein 3D and the driving game's own original all
   index static wall geometry by direct cell address, and all three answer a door by reading
   the door's live state at query time rather than by touching the index at all. Mutation
   cost is not "cheap"; it is **zero**.
2. **The one-per-cell constraint deletes the mechanism the canonical paper was written to
   provide.** Amanatides & Woo's `rayID` mailboxing exists to stop a ray re-testing an object
   that spans several voxels; their own measurements put it at 6.6 → 3.8 intersections per
   ray on their largest scene. A segment that lives in exactly one cell can never be
   re-tested, so that machinery — and the merged-run identity problem it stands in for — is
   not paid for.
3. **A swept circle does not need a widened traversal, because every circle in this game is
   smaller than a cell.** The largest is the boulder at r = 1.97 m against a 2 m cell, so the
   set of cells within r of any point is provably at most 3×3, and the set within r of a
   swept segment is the 1-cell dilation of its DDA path. Q8 is a nine-load gather with no
   traversal at all. The margin is 1.5%, and that is a trigger to watch.
4. **Ghost vertices are one of four shipped answers to the internal-endpoint problem, and
   for colinear axis-aligned neighbours Box2D's version provably never generates a vertex
   contact at all.** Chipmunk's neighbour tangents, by contrast, evaluate to exactly zero for
   a colinear pair and reject nothing. The original game's answer is neither: a per-cell flag
   derived from grid adjacency, which is the same one bit of information without storing a
   vertex.
5. **Attribution to the cell is a solved, boring problem in every engine that needs it** —
   Godot maps body RID to cell coordinates, opennox writes the grid point into an out
   parameter, Box2D returns a `b2ShapeId`, Doom's intercept carries the linedef. The
   interesting half is the *runs*, and the driving game's own vision spec already answers it:
   with uniform cells, a distance along a run converts to a cell index by division.
6. **The alternatives lose on mutation, not on query.** A BVH answers a trace well and
   attributes hits correctly; what it cannot do is edit one leaf of a baked static structure.
   Box2D has no API to change one segment of a chain — only destroy and recreate the whole
   chain. Recast's per-tile BV-tree has no insert, remove or refit API at all, and an obstacle
   is a throttled per-frame tile re-bake.
7. **The one piece of primary literature that compares them says grids win exactly here.**
   Havran's thesis benchmarks twelve ray-shooting algorithms over thirty scenes and concludes
   that hierarchies pay off *"except for densely occupied scenes"*, naming the mechanism: a
   hierarchy's descent is wasted when the ray hits something close to its origin. Q3's dominant
   caller traces between two nearly touching bodies — 1 to 3 cells. That is the case Havran
   names.

---

## 1. The constraints, and the arithmetic they force

Restated from requirements §6 and §7 only where a number below depends on it.

| Constraint | Value |
| --- | --- |
| Grid | 256 × 256 cells, 2 m step, **512 m square** |
| Cells | **65 536** |
| Segments | **at most one per cell**, axis-aligned |
| Wall thickness | 0.4 m |
| Circle radii | 0.49–0.74 m creatures, 0.62 m player, **1.97 m boulder** |
| Projectiles | **radius 0**, up to 1.92 m per tick (crossbow bolt, 115 m/s) |
| Mutation | doors open/close, breakables break |

Four consequences follow by arithmetic alone, and they are the reason most published prior
art does not apply.

**Storage is small enough to stop being an argument.** An occupancy bit per cell is 65 536
bits = **8 KiB**, which fits in a typical 32 KiB L1 data cache. One byte per cell — enough
for a shape/direction index — is **64 KiB**. Two bytes is 128 KiB. For comparison, the
original game's per-cell wall record is **36 bytes** at its 32-bit layout (field offsets are
in the struct's own comments,
[`src/server/wall.go:707`](https://github.com/noxworld-dev/opennox/blob/dev/src/server/wall.go)),
which would be 2.25 MiB dense — and it is precisely *because* the record is fat that the
original stores it sparsely (§3.2). A design that keeps the hot field (is there a wall, and
which shape) separate from the cold fields (flags, health, door object) gets an L1-resident
first test.

**Every circle in the game is smaller than one cell.** Let cell size *s* = 2 m and *r* ≤ 1.97
m, so *r* < *s*. For any two points *p*, *q* with |*p* − *q*| < *s*, ⌊*p*/*s*⌋ and ⌊*q*/*s*⌋
differ by at most 1 per axis. Therefore:

- the cells within *r* of a **point** are contained in a **3 × 3 block** — a fixed nine-load
  gather, no traversal;
- the cells within *r* of a **segment** are contained in the **1-cell dilation** of that
  segment's supercover — a 3-wide corridor around the DDA path.

This is what makes Q8 (closest point on wall geometry to a circle, per body per sub-step)
structurally trivial, and it is what makes the swept-circle problem of §4 collapse. **The
margin is 1.97 against 2.00 — 1.5%.** A body larger than one cell makes the gather 5 × 5 and
the dilation two cells. That is the trigger to watch.

**Projectiles need no widening at all.** They are points (requirements §6), so Q1 for a projectile is a
plain ray DDA. The widening question only arises for the swept *circle* case.

**The highest-volume trace is also the shortest.** Q3's largest caller is the object-vs-object
contact gate, which runs "once per candidate contact pair per tick" — and a *candidate contact
pair* is by construction two bodies within r₁ + r₂ ≤ ~4 m of each other, i.e. **1 to 3 cells
apart**. The volume is in the call count, not the trace length. Amanatides & Woo's own cost
split makes that decisive: their initialisation is **33 floating-point operations** against
roughly 3 per cell (§2.1). For a two-cell trace the setup is ten times the traversal, so the
thing to optimise for Q3 is *per-call* cost, not per-cell cost — which is the opposite of what
the ray-tracing literature optimises for.

---

## 2. Grid traversal: what the canonical paper says, and what actually ships

### 2.1 Amanatides & Woo, and what it does and does not claim

John Amanatides and Andrew Woo, *A Fast Voxel Traversal Algorithm for Ray Tracing*,
Eurographics '87, Dept. of Computer Science, University of Toronto. Read from the author's
own university copy at
[`eecs.yorku.ca/~amana/research/grid.pdf`](https://www.eecs.yorku.ca/~amana/research/grid.pdf).

The abstract states the cost directly:

> A fast and simple voxel traversal algorithm through a 3D space partition is introduced.
> Going from one voxel to its neighbour requires only two floating point comparisons and one
> floating point addition. Also, multiple ray intersections with objects that are in more than
> one voxel are eliminated.

The 2D loop, verbatim from the paper:

```
loop {
   if(tMaxX < tMaxY) {
       tMaxX= tMaxX + tDeltaX;
       X= X + stepX;
   } else {
       tMaxY= tMaxY + tDeltaY;
       Y= Y + stepY;
   }
   NextVoxel(X,Y);
}
```

and the paper's own accounting for the 3D version:

> The loop above requires two floating point comparisons, one floating point addition, two
> integer comparisons and one integer addition per iteration. The initialization phase requires
> 33 floating point operations (including comparisons) if the origin of the ray is inside the
> grid and up to 40 floating point operations otherwise.

The 2D specialisation is a strict subset: **one floating-point comparison, one floating-point
addition, one integer addition**, plus the bounds check.

The paper also gives a trick worth having for Q1: once a candidate intersection is found, the
test for "is it in the current cell" reduces to one comparison — the intersection's *t* against
the current cell's `tMax`. The paper keeps two traversal functions, one with the extra
comparison and one without, and only switches to the checked one after a surface is hit.

**What the paper measures.** Four scenes at 512 × 512, one sample per pixel, one shadow-casting
light, on a Sun 3/75 running Unix:

| Figure | Time (min) | Grid subdivision | Objects | Objects/voxel | Intersections/ray | With `rayID` |
| --- | --- | --- | --- | --- | --- | --- |
| 4 | 49.5 | 20 | 4 | 0.3 | 4.6 | 1.7 |
| 5 | 44.8 | 20 | 31 | 0.9 | 5.3 | 2.0 |
| 6 | 21.7 | 30 | 62 | 0.2 | 1.9 | 1.4 |
| 7 | 32.6 | 40 | 3540 | 1.1 | 6.6 | 3.8 |

**What the paper does not measure: any comparison against an octree or a BVH.** Octrees appear
twice — as related work by Glassner in the introduction, and in Conclusions as *"we are looking
to see how easily the algorithm can be modified to traverse octree space subdivision schemes"*.
There is no grid-versus-hierarchy speedup figure in this paper. Anyone who quotes one is
quoting something else.

**`rayID` is the part that does not apply here.** It is a mailbox: each ray carries a unique
id, each object stores the last id that tested it, and a repeat test is skipped. Its whole
purpose is objects that span several voxels — and the paper is explicit that its second-order
benefit is being allowed to be sloppy about which voxels an object occupies:

> The fact that multiple intersections are eliminated means that we do not have to spend as
> much time in determining the minimal number of voxels that an object occupies.

**With one segment per cell there is nothing to mailbox.** The 1.7×–2.7× reduction in the table
is a cost this design never incurs. This is the single clearest way the constraint pays.

The paper also names the limit of pushing subdivision up — *"as we increase grid subdivision,
voxel initialization time, traversal time as well as memory usage also become significant thus
ultimately limiting the subdivision rate"* — which is the one tuning question a uniform grid
normally forces and which here is **already answered by authoring**: the cell size is 2 m
because the level is authored on 2 m cells.

### 2.2 What ships is usually *not* Amanatides & Woo

Three shipped boolean line-of-sight traces over a tile grid, none of them A&W.

**Wolfenstein 3D — `CheckLine`, two independent integer axis walks.**
[`WL_STATE.C:1037`](https://github.com/id-Software/wolf3d/blob/master/WOLFSRC/WL_STATE.C).
The comment says what it is: *"Returns true if a straight line between the player and ob is
unobstructed."* It runs two loops — one stepping *x* by whole tiles and interpolating *y*, then
one stepping *y* and interpolating *x* — in 8.8 fixed point. The inner loop is:

```c
do
{
    y = yfrac>>8;
    yfrac += ystep;

    value = (unsigned)tilemap[x][y];
    x += xstep;

    if (!value)
        continue;

    if (value<128 || value>256)
        return false;
    ...
} while (x != xt2);
```

Per cell: one shift, one add, one array index, one branch, one integer add. **No comparison
between two `tMax` values, because a boolean query does not need the cells in order.** The two
divisions (`((long)delta<<8)/deltafrac`) are in setup, one per axis. This is cheaper per cell
than A&W precisely by dropping the ordering — which is exactly the trade Q3 wants and Q1 does
not.

`tilemap` is `byte[64][64]` — **4 KiB, comfortably L1-resident.**

**Doom — `P_PathTraverse` over the BLOCKMAP, with mailboxing.**
[`p_maputl.c`](https://github.com/id-Software/DOOM/blob/master/linuxdoom-1.10/p_maputl.c).
The stepping is A&W-shaped but in fixed point against block indices rather than against a
running `tMax`:

```c
if ( (yintercept >> FRACBITS) == mapy)
{
    yintercept += ystep;
    mapx += mapxstep;
}
else if ( (xintercept >> FRACBITS) == mapx)
{
    xintercept += xstep;
    mapy += mapystep;
}
```
bounded by `for (count = 0 ; count < 64 ; count++)` with the comment *"Count is present to
prevent a round off error from skipping the break."*

Doom needs A&W's mailbox because **a Doom linedef is stored in every block it touches**, and it
implements it as `validcount`, a global counter bumped once per query and stamped into each
linedef:

```c
// The validcount flags are used to avoid checking lines
// that are marked in multiple mapblocks,
// so increment validcount before the first call
// to P_BlockLinesIterator, ...
    if (ld->validcount == validcount)
        continue; 	// line has already been checked
    ld->validcount = validcount;
```

This is the mechanism a **merged colinear run** would need if runs were the collision unit, and
it is the mechanism per-cell segments make unnecessary. Doom also caps collected hits at
`#define MAXINTERCEPTS 128` into a static array, sorted by `frac` afterwards by
`P_TraverseIntercepts`.

The BLOCKMAP itself is 128-map-unit blocks; `P_LoadBlockMap` reads a four-word header (origin
x, origin y, width, height) then a `width * height` array of `short` offsets into lists of line
indices terminated by `-1`. The declaration carries its own limit as a comment: `short*
blockmap;	// int for larger maps` — a 16-bit offset table, so the lump is bounded to 64 K
words.

**Note which structure serves which query in Doom, because it is not the obvious one.**
`P_CheckSight` ([`p_sight.c`](https://github.com/id-Software/DOOM/blob/master/linuxdoom-1.10/p_sight.c))
does *not* use the BLOCKMAP. It uses the REJECT table as a trivial reject and then walks the
**BSP** via `P_CrossBSPNode`. The BLOCKMAP serves movement (`P_CheckPosition`) and hitscan
(`P_PathTraverse`). Doom runs three indexes over the same geometry for three different query
shapes — which is a precedent for requirements §7's "physics indexing it is fine, physics owning it is not".

**The driving game's original — a per-column slab walk, and it returns the cell.**
`MapTraceRay`/`MapTraceRayAt` in
[`opennox src/server/wall.go`](https://github.com/noxworld-dev/opennox/blob/dev/src/server/wall.go).
This is neither A&W nor Bresenham. It special-cases the two axis-aligned cases into a single
loop, and for the general case walks the *major* axis one column at a time, computing the span
of minor-axis cells the ray covers in that column and iterating them:

```go
yi1 := int(math.Trunc(float64(y1/common.GridStep)) - stepY)
yi2 := int(math.Trunc(float64(y2/common.GridStep)) + stepY)
```

Two things are worth taking from it. First, **it already over-visits by one cell at each end of
each column span** (the `- stepY` / `+ stepY`), so the original's own trace is a conservatively
widened traversal, not a minimal one. Second, the signature is
`MapTraceRayAt(p1, p2, outPos *types.Pointf, outGrid *image.Point, flags MapTraceFlags) bool`
— **the cell is an out parameter and attribution is built in.** It also carries a float epsilon
in its stepping (`x = xnext + 0.1*mulX`), which is the kind of thing a clean-room
reimplementation should not inherit.

**libtcod and NetHack use Bresenham, not a DDA.** libtcod's `FOV_BASIC` is documented in
`fov_circular_raycasting.c` as *"Cast a Bresenham ray marking tiles along the line as lit"* and
calls `TCOD_line_init_mt`/`TCOD_line_step_mt`, an integer Bresenham with a single error
accumulator; its shadowcasting family (`FOV_SHADOW`, `FOV_PERMISSIVE_*`, `FOV_RESTRICTIVE`,
`FOV_SYMMETRIC_SHADOWCAST`) is angular-interval sweeping over rows, not per-ray traversal at
all. NetHack's `src/vision.c` documents `clear_path()` as *"the generalized integer Bresenham's
algorithm (fast line drawing) for all quadrants,"* citing Rogers, *Procedural Elements for
Computer Graphics*, McGraw-Hill 1985. **Neither publishes any complexity or benchmark figure**
for any of its FOV algorithms.

> **Bresenham is not a supercover.** It visits one cell per major-axis step and can cut a
> corner diagonally between two occupied cells. For a *vision* query that is a stylistic choice
> a roguelike can make; for requirements §6's Q3 gating physical contact through a 0.4 m wall it is a
> correctness question, and the answer has to be a supercover-style traversal (A&W, or Doom's,
> or the original's) rather than Bresenham.

---

## 3. The alternatives, and exactly where each one loses

### 3.1 Dynamic AABB tree (BVH) — wins the query, loses the edit

Box2D is the reference implementation and it is BVH-only. **Official Box2D has never shipped a
uniform grid or spatial hash broadphase**: v2.4.1's `b2BroadPhase` holds a single
`b2DynamicTree m_tree`, and v3.1.1's holds `b2DynamicTree trees[b2_bodyTypeCount]` — three
trees split by body type, still all AABB trees
([`src/broad_phase.h`](https://github.com/erincatto/box2d/blob/v3.1.1/src/broad_phase.h)).

v3 does the right thing for static geometry and is worth noting as the shape of a good BVH
answer:

- static shapes get their own tree and a smaller fattening margin —
  `float margin = proxyType == b2_staticBody ? speculativeDistance : aabbMargin;`
  (`src/shape.c`, `b2UpdateShapeAABBs`), against `#define B2_AABB_MARGIN ( 0.05f *
  b2_lengthUnitsPerMeter )`;
- the per-step rebuild explicitly skips the static tree —
  `b2BroadPhase_RebuildTrees` only rebuilds `b2_dynamicBody` and `b2_kinematicBody`;
- `b2BroadPhase_EnlargeProxy` asserts it is never called on the static tree;
- the only full static rebuild, `b2World_RebuildStaticTree`, is documented `/// This is for
  internal testing`.

So *creating or destroying* one static shape is a leaf insert/remove, not a rebuild. That much
is fine. **What is not fine is that there is no way to edit a chain.** `b2ChainDef`'s own doc
comment says chains *"have no mass and should be used on static bodies"*, the only chain
setters are `b2Chain_SetFriction`, `b2Chain_SetRestitution` and `b2Chain_SetMaterial`, and
`b2DestroyChain` walks every constituent segment:

```c
int count = chain->count;
for ( int i = 0; i < count; ++i )
{
    int shapeId = chain->shapeIndices[i];
    b2Shape* shape = b2ShapeArray_Get( &world->shapes, shapeId );
    bool wakeBodies = true;
    b2DestroyShapeInternal( world, shape, body, wakeBodies );
}
```

**A door in one cell of a chain is a destroy-and-recreate of the whole chain** — O(N)
broad-phase removes and inserts plus a contact-destroy pass — where the grid pays nothing. This
is the decisive asymmetry, and it is code-verified rather than documented: **Box2D publishes no
number for the cost of mutating a chain, of a single tree insert or remove, or of a static-tree
rebuild.** Erin Catto's own GDC 2019 slides, *Dynamic Bounding Volume Hierarchies*
([box2d.org/files/ErinCatto_DynamicBVH_Full.pdf](https://box2d.org/files/ErinCatto_DynamicBVH_Full.pdf)),
decline even to cover removal: *"Leaf removal is straight forward and is not covered."*

Rapier reaches the same place from the other direction. Parry's current `Bvh` refit documents
its cost model in the doc comment
([`src/partitioning/bvh/bvh_refit.rs`](https://github.com/dimforge/parry/blob/master/src/partitioning/bvh/bvh_refit.rs)):

> - **Time**: O(n) where n is the number of nodes
> - **Space**: O(n) temporary storage in workspace
> - Much faster than rebuilding the tree from scratch

O(n) in the node count — for 65 536 static leaves, a refit is a full walk. Rapier's current
`BroadPhaseBvh` holds one `tree: Bvh` for static and dynamic colliders alike
([`src/geometry/broad_phase_bvh/mod.rs`](https://github.com/dimforge/rapier/blob/master/src/geometry/broad_phase_bvh/mod.rs)).
**No first-party source publishes a refit figure in absolute time, or any broadphase figure at
65 536-static-collider scale.**

**The BVH does not lose on attribution.** Box2D's `b2CastResultFcn` receives the `b2ShapeId`
per hit, and Chipmunk's `cpSegmentQueryInfo` carries `shape`, `point`, `normal` and `alpha`.
Whatever else is true, "which thing did I hit" is not the discriminator.

### 3.2 Uniform spatial hash — the right idea, indexed the wrong way

Chipmunk ships one and documents when to use it
([`doc-src/chipmunk-docs.textile`](https://github.com/slembcke/Chipmunk2D/blob/master/doc-src/chipmunk-docs.textile)):

> Chipmunk officially supports two spatial indexes. The default is an axis-aligned bounding box
> tree ... The other available spatial index type available is a spatial hash, which can be
> much faster when you have a very large number (1000s) of objects that are all the same size.
> For smaller numbers of objects, or objects that vary a lot in size, the spatial hash is
> usually much slower. It also requires tuning (usually through experimentation) ...

> Setting `dim` to the average collision shape size is likely to give the best performance.
> ... Setting `count` to ~10x the number of objects in the space is probably a good starting
> point.

Read against this problem, the doc is describing the *favourable* case exactly — thousands of
objects, all the same size — which is what 65 536 identical 2 m cells are. But the hash is not
the default (`cpSpaceInit` builds `cpBBTreeNew` for both `staticShapes` and `dynamicShapes`),
and its whole tuning burden — pick `dim`, pick `count`, hope the hash spreads — exists only
because it must handle *arbitrary* positions. **A dense authored grid needs no hash: the cell
index is the address.** The hash's virtue here is also its own obsolescence.

The same story shows up as a version delta in Godot. Godot 3.5 shipped
`servers/physics_2d/broad_phase_2d_hash_grid.cpp` alongside `broad_phase_2d_bvh.cpp`; **Godot
4.4's `modules/godot_physics_2d/` contains only `godot_broad_phase_2d_bvh.cpp`** — the hash
grid was dropped. Note carefully what that is evidence *for*: Godot's broadphase indexes
dynamic AABBs of arbitrary size and position, which is the case the Chipmunk doc says the hash
loses. It is not evidence against a grid for authored, fixed-size, static cells.

Interestingly, the driving game's original does use a hash, and its shape says why. Walls are
kept in `byPos []*Wall` chained by `NextByPos16`, addressed by

```go
func wallArrayInd(pos image.Point) (uint16, bool) {
	if pos.X < 0 || pos.X >= WallGridSize || pos.Y < 0 || pos.Y >= WallGridSize {
		return 0, false
	}
	return (uint16(pos.Y) + (uint16(pos.X) << 8)) & 0x1FFF, true
}
```

— `(y + (x<<8)) & 0x1FFF`, i.e. **8 192 buckets for 65 536 cells**, with the 8 x-values
differing by 32 colliding into one chain. It is a hash only because the record is 36 bytes and
a dense array of them would be 2.25 MiB. With the record split, the dense array is 64 KiB and
the chain walk disappears.

### 3.3 Quadtree — pays for adaptivity this geometry does not have

No engine surveyed here uses a quadtree for static 2D collision geometry: Box2D, Chipmunk,
Rapier and Godot 4 all use AABB trees; Doom, Wolf3D and the original use grids; Doom
additionally uses a BSP. A quadtree's benefit is adaptive subdivision where occupancy is
uneven, and it is paid for with a root-to-leaf descent per query. **The occupancy here is
uniform by construction** — one authored cell, one segment — so there is nothing for the
adaptivity to find, and the descent is a pure loss against an index computation.

The closest published support is Havran's thesis (§8.3), which tested four octree variants
against a uniform grid over 30 scenes and gives the mechanism in one sentence: *"In this kind of
scene the down traversal phase for hierarchical spatial data structures is expensive, since the
ray intersects the object very close to the origin of a ray."* That is 3D and it is octrees, not
2D quadtrees; **no primary source publishes a quadtree-versus-grid comparison for uniformly
dense 2D static geometry.**

### 3.4 A bitset per cell — a prefilter, not an index

Occupancy as one bit per cell is 8 KiB for the whole map, which is the cheapest possible
"is there anything here" test and is L1-resident. What it cannot do is answer Q1 or Q8:
**a bit gives you neither *t*, nor a unit normal, nor a span, nor an identity.** Recast's
rasterisation stage is the closest thing in a shipping engine to "geometry as occupancy"
(`rcRasterizeTriangle` filling `rcHeightfield` spans), and it is a *build* stage whose output
is then turned into polygons before anything queries it.

The useful reading is therefore not "bitset instead of" but "bitset in front of": an 8 KiB
occupancy plane as the DDA's per-cell test, with the 64 KiB shape array and the cold flag array
touched only on a hit. That is an arithmetic argument about cache residency, not a measured
one — see §9.

### 3.5 Navmesh tiles — the clearest example of what mutation costs a baked structure

Recast/Detour is not a candidate index here, but it is the best-documented instance of the
failure mode. Its per-tile BV-tree is built at bake time and has **no insert, remove or refit
API at all**; runtime change is whole-tile replacement, and an obstacle is a throttled per-frame
re-bake queue. `dtNavMeshQuery::raycast` returns `t`, `hitNormal` and a `dtPolyRef` path, so
again attribution is not the problem — **granularity of edit is.** The code and the constants
are in [§8.1](#81-recastdetour-attribution-is-fine-granularity-of-edit-is-the-whole-problem).

---

## 4. Sweeping a circle through a grid, not just a ray

Four shipped strategies, in increasing order of what they cost.

### 4.1 Minkowski-expand the static geometry at bake time — Quake's three hulls

Quake does not sweep shapes. It **pre-expands the world three times**, once per collider size,
and then every trace is a point trace. `qbsp`'s `ExpandBrush`
([`Quake-Tools qutils/QBSP/BRUSH.C`](https://github.com/id-Software/Quake-Tools/blob/master/qutils/QBSP/BRUSH.C)):

```c
vec3_t	hull_size[3][2] = {
{ {0, 0, 0}, {0, 0, 0} },
{ {-16,-16,-32}, {16,16,24} },
{ {-32,-32,-64}, {32,32,24} }
};
...
// expand all of the planes
for (i=0 ; i<numbrushfaces ; i++)
{
    p = &faces[i].plane;
    VectorCopy (vec3_origin, corner);
    for (x=0 ; x<3 ; x++)
    {
        if (p->normal[x] > 0)
            corner[x] = hull_size[hullnum][1][x];
        else if (p->normal[x] < 0)
            corner[x] = hull_size[hullnum][0][x];
    }
    p->dist += DotProduct (corner, p->normal);
}
// add any axis planes not contained in the brush to bevel off corners
...
// add all of the edge bevels
```

and `SV_HullForEntity` picks one by size at runtime
([`WinQuake/world.c`](https://github.com/id-Software/Quake/blob/master/WinQuake/world.c)):

```c
VectorSubtract (maxs, mins, size);
if (size[0] < 3)
    hull = &model->hulls[0];
else if (size[0] <= 32)
    hull = &model->hulls[1];
else
    hull = &model->hulls[2];
```

**Two things transfer and one does not.**

The plane push `p->dist += DotProduct(corner, p->normal)` is, for an **axis-aligned** plane,
just `dist += halfwidth`. The two loops that follow — axis bevels and edge bevels — exist
entirely to round off the corners and edges that a non-axis-aligned brush acquires when
Minkowski-summed with a box. **For purely axis-aligned geometry those loops add nothing the
brush does not already have.** So Minkowski expansion of axis-aligned static geometry against
an *axis-aligned box* collider is exact and free: widen the slab by r, extend the span by r,
done.

What does not transfer is that our collider is a **circle**, and the Minkowski sum of a disc
and a segment is a **stadium** — a slab with two round caps — not a longer segment. The exact
test is "distance from the segment to the swept centre-line ≤ r", which is a segment–segment
distance, still closed form and still cheap for axis-aligned inputs, but it is not a point
test. *Unless* the endpoints are suppressed (§5 below), in which case the caps are never the closest
feature and the test really does collapse to a 1D slab interval.

The other cost Quake pays is the one this design must not: **N copies of the static index, one
per collider size, and therefore size quantisation.** Quake gets three player sizes in the whole
game. With radii spanning 0.49–1.97 m that is not available here.

### 4.2 Minkowski-expand the *query* — Box2D's swept fat box

Box2D v3's `b2DynamicTree_ShapeCast` builds its corridor by inflating the proxy's AABB by the
radius and then unioning it with its translated copy
([`src/dynamic_tree.c`](https://github.com/erincatto/box2d/blob/v3.1.1/src/dynamic_tree.c)):

```c
b2Vec2 radius = { input->proxy.radius, input->proxy.radius };
originAABB.lowerBound = b2Sub( originAABB.lowerBound, radius );
originAABB.upperBound = b2Add( originAABB.upperBound, radius );
...
b2Vec2 t = b2MulSV( maxFraction, input->translation );
b2AABB totalAABB = {
	b2Min( originAABB.lowerBound, b2Add( originAABB.lowerBound, t ) ),
	b2Max( originAABB.upperBound, b2Add( originAABB.upperBound, t ) ),
};
```

then culls tree nodes with `b2AABB_Overlaps(node->aabb, totalAABB)` before doing a per-leaf
`b2ShapeCast`. The grid analogue is the 1-cell dilation derived in §1 — same idea, and for
r < s the dilation is provably exactly one cell.

Bevy states the *narrow-phase* half of the same reduction in three lines, and it is the cheapest
statement of it anywhere in this survey — `circle_collision_at` sweeps a circle by adding its
radius to the target and casting a ray, `aabb_collision_at` sweeps a box by Minkowski-differencing
the extents. Code and citation in [§8.2](#82-bevy-has-no-broadphase-and-its-swept-tests-are-minkowski-reductions).

### 4.3 Corridor widening by a constant — Doom's `MAXRADIUS`

Doom's is the bluntest and, for a fixed body-size range, the cheapest.
[`p_local.h`](https://github.com/id-Software/DOOM/blob/master/linuxdoom-1.10/p_local.h):

```c
// MAXRADIUS is for precalculated sector block boxes
// the spider demon is larger,
// but we do not have any moving sectors nearby
#define MAXRADIUS		32*FRACUNIT
```

and `P_CheckPosition` ([`p_map.c`](https://github.com/id-Software/DOOM/blob/master/linuxdoom-1.10/p_map.c)):

```c
// The bounding box is extended by MAXRADIUS
// because mobj_ts are grouped into mapblocks
// based on their origin point, and can overlap
// into adjacent blocks by up to MAXRADIUS units.
xl = (tmbbox[BOXLEFT] - bmaporgx - MAXRADIUS)>>MAPBLOCKSHIFT;
xh = (tmbbox[BOXRIGHT] - bmaporgx + MAXRADIUS)>>MAPBLOCKSHIFT;
```

Note the asymmetry in the same function: **the thing pass is widened, the line pass is not** —
```c
// check lines
xl = (tmbbox[BOXLEFT] - bmaporgx)>>MAPBLOCKSHIFT;
```
because lines are registered in *every* block their segment touches at build time, whereas
things are hashed only by their origin point. That is exactly the trade this design faces in
miniature: **register geometry in every cell it touches and the query needs no widening but
needs mailboxing; register it in one cell and the query must widen but never mailboxes.** Requirements §7
forces the second — the cell is the unit of identity — and §1 shows the widening is one cell.

And Doom never sweeps at all: `P_TryMove` tests a static AABB at the destination and only then
commits, with the O(1) relink of §6.2. There is no continuous circle sweep anywhere in
linuxdoom-1.10.

### 4.4 Conservative advancement — the general answer, and what it costs

Box2D's `b2ShapeCast` is the reference. v2.4.1 (`src/collision/b2_distance.cpp`):

```cpp
// GJK-raycast
// Algorithm by Gino van den Bergen.
// "Smooth Mesh Contacts with GJK" in Game Physics Pearls. 2010
...
const float tolerance = 0.5f * b2_linearSlop;
const int32 k_maxIters = 20;
```

v3.1.1 (`src/distance.c`) is the same algorithm with the comment `// Shape cast using
conservative advancement`, `tolerance = 0.25f * linearSlop`, `int maxIterations = 20`, and a
loop whose body is a full `b2ShapeDistance` (a GJK) per iteration. Parry does the same thing:
`cast_shapes_support_map_support_map` delegates to `gjk::directional_distance`, documented as
*"This function internally uses GJK raycasting on the Minkowski difference"*, with
`eps_tol() = DEFAULT_EPSILON * 10.0` and **no hard iteration cap found in the loop**.

**Twenty GJK evaluations per cast is the price of not knowing your geometry.** Erin Catto's
GDC 2013 *Continuous Collision* slides
([box2d.org/files/ErinCatto_ContinuousCollision_GDC2013.pdf](https://box2d.org/files/ErinCatto_ContinuousCollision_GDC2013.pdf))
characterise naive conservative advancement as sometimes needing "hundreds of iterations", which
is why the shipped variant is the capped GJK-raycast that reports a miss rather than iterating
freely. For an axis-aligned segment against a circle none of this is needed: the sweep is a
closed-form quadratic-free interval test, and the iteration count is one.

Chipmunk, for completeness, **has no shape cast at all**: the public headers contain point,
segment and bounding-box queries and nothing else — no `ShapeCast`, no sweep, no TOI. That is
first-party structural confirmation of the map's existing claim. (The commonly cited forum
thread recommending "raycast it yourself" cannot be quoted: `chipmunk-physics.net` now serves a
DreamHost "Site Not Found" placeholder as of 2026-09-12.)

---

## 5. The internal endpoint, and whether ghost vertices even apply

A circle sliding along a run of per-cell segments meets a shared endpoint at every cell
boundary. State the artefact precisely, because the precise statement is what makes the
axis-aligned case easy: **the problem is not the shared vertex itself but the *neighbouring*
cell's endpoint showing up as a second contact carrying a spurious diagonal normal.** A body
whose centre projects inside cell A's span gets a correct face contact from A; cell B's closest
point is the shared vertex, at a slightly greater distance, and if that distance is under the
radius B contributes a contact whose normal points diagonally out of the vertex. Under the
requirements' §5 accumulator that is a sideways shove at every cell boundary.

Four shipped mechanisms, one counter-example, and they do not agree.

### 5.1 Box2D — ghost vertices, and they resolve the joint to exactly one owner

v3's `b2ChainSegment` carries its neighbours' vertices directly
([`include/box2d/collision.h`](https://github.com/erincatto/box2d/blob/v3.1.1/include/box2d/collision.h)):

```c
typedef struct b2ChainSegment
{
	/// The tail ghost vertex
	b2Vec2 ghost1;
	/// The line segment
	b2Segment segment;
	/// The head ghost vertex
	b2Vec2 ghost2;
	/// The owning chain shape index (internal usage only)
	int chainId;
} b2ChainSegment;
```

and `b2CollideChainSegmentAndCircle` in `src/manifold.c` uses them as a Voronoi-region test:

```c
if ( v <= 0.0f )
{
	// Behind point1?
	// Is pB in the Voronoi region of the previous edge?
	b2Vec2 prevEdge = b2Sub( p1, segmentA->ghost1 );
	float uPrev = b2Dot( prevEdge, b2Sub( pB, p1 ) );
	if ( uPrev <= 0.0f ) { return manifold; }
	pA = p1;
}
else if ( u <= 0.0f )
{
	// Ahead of point2?
	b2Vec2 nextEdge = b2Sub( segmentA->ghost2, p2 );
	float vNext = b2Dot( nextEdge, b2Sub( pB, p2 ) );
	// Is pB in the Voronoi region of the next edge?
	if ( vNext > 0.0f ) { return manifold; }
	pA = p2;
}
```

**Work the algebra for a colinear neighbour and the endpoint region vanishes entirely.** Let
the run lie along +x with `ghost1 = p1 − e`, so `prevEdge` is parallel to `e`. The branch is
entered when `v = dot(e, pB − p1) ≤ 0`; but then `uPrev = dot(prevEdge, pB − p1)` has the same
sign as `v`, so `uPrev ≤ 0` and the function returns with no manifold. **For perfectly colinear
neighbours, a chain segment never generates a vertex contact — every point is in exactly one
segment's face region.** The spurious diagonal normal of the opening paragraph cannot be
produced.

The tie-break at the exact joint is asymmetric on purpose: at `v = 0` this segment rejects
(`uPrev ≤ 0`), while the previous segment reaches its own `u ≤ 0` branch with `vNext = 0`,
which fails the strict `vNext > 0` and so *accepts*. **Exactly one owner, no duplicate, no
hole.**

The convexity half of the machinery is the part that goes inert. `b2ClassifyNormal`'s Gauss-map
test is guarded by `convex1`/`convex2`, computed as
`b2Cross( edge0, edge1 ) >= convexTol` with `const float convexTol = 0.01f;` — and a colinear
pair gives a cross product of exactly 0, which fails it, so the classification is `b2_normalSnap`
(use the segment's own normal) rather than `b2_normalSkip`. **On a dead-straight axis-aligned
run the Gauss map never rejects anything.** So the answer to the ticket's question — do ghost
vertices apply when every normal is axis-aligned — is: *the vertex-region half is not only
applicable, it is the whole mechanism; the convexity half is dead code for this geometry.*

Two costs come attached. `b2ChainDef`'s doc comment warns that **"an open chain shape has NO
COLLISION on the first and final edge"**, and chains are one-sided by construction — Erin
Catto's own post explains why
([box2d.org/posts/2020/06/ghost-collisions/](https://box2d.org/posts/2020/06/ghost-collisions/)):

> Chain shapes are meant to be used for building large game worlds, so the chain loops can
> contain hundreds of edges. It is too expensive to perform the point-in-polygon test.
> Therefore, it seems that smooth collision can only be efficiently and robustly achieved with
> one-sided edge chains.

That reasoning does not bind a grid. **The information a ghost vertex carries is one bit per
side — "is there a colinear neighbour here" — and on a dense grid that bit is a neighbouring
array read, not stored geometry.** Which is exactly what the original game does (§5.3).

### 5.2 Chipmunk — neighbour tangents, which evaluate to zero and reject nothing

`cpSegmentShape` carries `a_tangent`/`b_tangent`, set by
`cpSegmentShapeSetNeighbors(shape, prev, next)` as `cpvsub(prev, seg->a)` and
`cpvsub(next, seg->b)`. The doc states the intent:

> When you have a number of segment shapes that are all joined together, things can still
> collide with the "cracks" between the segments. By setting the neighbor segment endpoints you
> can tell Chipmunk to avoid colliding with the inner parts of the crack.

The test, in `src/cpCollision.c`'s `CircleToSegment`:

```c
if(
    (closest_t != 0.0f || cpvdot(n, cpvrotate(segment->a_tangent, rot)) >= 0.0) &&
    (closest_t != 1.0f || cpvdot(n, cpvrotate(segment->b_tangent, rot)) >= 0.0)
){
```

**For a colinear neighbour the tangent is parallel to the segment and therefore perpendicular
to the contact normal at the joint, so `cpvdot(n, tangent)` is exactly 0.0, which satisfies both
`>= 0.0` and `<= 0.0`.** The guard passes and the contact stands. Chipmunk's mechanism
suppresses contacts only where the neighbour bends *back* relative to the normal — genuinely
reflex joints — and does nothing on a straight run.

This is not a bug: for Chipmunk's sequential-impulse solver a doubled contact at a shared
vertex resolves to the same place. **It matters here because the requirements' §5 response is not sequential
impulses but a summed penetration-spring accumulator**, where two contacts at one boundary are
two springs. Whether that is visible at stiffness 25 is a measurement, not a claim (§9).

### 5.3 The original game — one bit derived from grid adjacency

The driving game's own answer is neither ghost vertices nor tangents. From
`../nox/docs/specs/sim-constants.md` §4, whose provenance line credits `opennox/opennox`
`src/server/wall.go` (grade A for the grid, grade B/C for the resolution rules):

> A per-cell shape index (derived from the wall's direction and its neighbours) selects which
> of the two segments exist, and clips each one to a span along its diagonal of `0…23`,
> `0…11.5`, or `11.5…23`. The half-length spans are how T-junctions and corners are built.
> Segment *endpoints* only act as collision features when the diagonally adjacent cell is
> empty — otherwise the segment is treated as continuing into its neighbour and no corner
> contact is generated. **This is why sliding along a straight run of wall is smooth and does
> not catch on cell boundaries.**

The mechanism is visible in the shipped code as a 22-entry span table indexed by the cell's
direction byte, two entries per cell (one per diagonal), each entry a presence flag and a span:

```go
var noxMapTable313272 = []struct {
	Field0 byte
	Field4 float32
	Field8 float32
}{
	{Field0: 0, Field4: 0, Field8: 0},
	{Field0: 1, Field4: 0, Field8: 23},
	...
	{Field0: 1, Field4: 11.5, Field8: 23},
}
```

used in `mapTraceRayImpl` as `if t := noxMapTable313272[2*int(tflag)+0]; t.Field0 != 0 && ...`.
**The neighbour information is baked into the cell's direction byte at load, and the "does this
endpoint exist" question is answered by a table lookup on one byte** — no stored vertex, no
chain, and it survives a door or a breakable changing state because the direction byte is not
what changes (§6 below).

Note the original's spans are quantised to three values because its segments are cell
diagonals; with axis-aligned segments and one per cell the equivalent is simpler still — a
presence flag per side, or equivalently "extend the span to the cell boundary iff the
neighbour is a colinear wall".

### 5.4 What Parry has, and the surprise about which dimension gets it

Parry's equivalent is pseudo-normal cones, and **the 2D and 3D paths are different types**.
`TriMesh::triangle_normal_constraints` and `TriMeshFlags::FIX_INTERNAL_EDGES` are gated
`#[cfg(feature = "dim3")]`. The 2D construct is `Polyline` with `PolylineFlags::ORIENTED`,
whose `segment_normal_constraints` is gated `#[cfg(feature = "dim2")]`; the doc on
`SegmentPseudoNormals` reads:

> The pseudo-normals of a polyline segment, approximating the outward normal cones of its
> features. ... `face` is the segment's outward normal and `edges` are the outward
> pseudo-normals at its two endpoints. An oriented polyline uses them to clamp contact normals
> to one side, so it acts as a one-sided surface.

The vertex pseudo-normal is *"the normalized sum of its incident segments' outward normals ...
their exact bisector — no angle weighting"*. For two colinear neighbours the bisector is the
face normal itself, so the cone is degenerate and clamping a contact normal to it yields the
face normal — the same outcome as Box2D reaching for the face, arrived at differently. There is
also an unreleased `CompoundFlags::FIX_INTERNAL_EDGES` on `master`, `dim2`-gated as landed.

### 5.5 Godot — one body per cell, and no mechanism at all

Worth recording as the counter-example, because Godot has literally this problem.
`TileMapLayer::_physics_update_cell`
([`scene/2d/tile_map_layer.cpp`](https://github.com/godotengine/godot/blob/4.4/scene/2d/tile_map_layer.cpp))
creates **one `PhysicsServer2D` body per cell per physics layer**, positioned at the cell, with
the tile's collision polygons added as convex shapes:

```cpp
if (!body.is_valid()) {
    body = ps->body_create();
}
bodies_coords[body] = r_cell_data.coords;
ps->body_set_mode(body, use_kinematic_bodies ? PhysicsServer2D::BODY_MODE_KINEMATIC : PhysicsServer2D::BODY_MODE_STATIC);
```

No merging, no chain, no neighbour information — and correspondingly no ghost-vertex,
pseudo-normal or tangent mechanism anywhere in the 2D tilemap path. Godot's attribution is the
`bodies_coords` map plus the public `get_coords_for_body_rid()`, and its mutation granularity
is genuinely per-cell (only cells on the `dirty.cell_list` are re-run). It is the honest
demonstration that per-cell bodies and cell attribution are workable; it is also the
demonstration that "per-cell bodies" and "smooth sliding" are separate problems that have to be
solved separately.

---

## 6. Mutation: what a door costs each structure

### 6.1 In a grid it costs nothing, and three shipped engines agree on why

The shared pattern is worth naming because all three arrived at it independently: **the cell
stores an identity or a flag; the mutable state lives beside the index; the query reads it
live.** Nothing is invalidated and nothing is rebuilt.

*Wolfenstein 3D.* A door tile's byte carries the door's *index*, and its slide position is a
separate array read at trace time (`WL_STATE.C`, `CheckLine`):

```c
value &= ~0x80;
intercept = yfrac-ystep/2;

if (intercept>doorposition[value])
    return false;
```

The `tilemap` byte is never rewritten as the door slides.

*Doom.* The REJECT table is `ceil(numsectors² / 8)` bytes, cached straight from the WAD by
`P_SetupLevel` and **never written again at runtime** — it is a build-time trivial-reject that
answers "can these two sectors *ever* see each other". Door state is consulted live, inside
`P_CrossSubsector`, from the sectors' current heights:

```c
// quick test for totally closed doors
if (openbottom >= opentop)
    return false;		// stop
```

So Doom's expensive precomputed visibility structure is deliberately *coarse enough to be
door-independent*, and the door is handled by the fine test that runs anyway.

*The driving game's original.* The cell's `Wall` record carries `Flags4` and the three lookup
functions differ only in which flags they filter:

```go
func (s *serverWalls) GetWallAtGrid(pos image.Point) *Wall {
	...
	if pos == it.GridPos() && !it.Flags4.HasAny(wall.FlagDoor|wall.FlagBroken) {
```
against `GetWallAtGrid2` (excludes only `FlagBroken`) and `GetWallAtGridRaw` (excludes nothing).
**Breaking a wall is one flag-bit write**; the index is untouched, and *which* readers stop
seeing it is chosen per query by picking a lookup function. A door is even lighter: the cell
holds a pointer to the door object and `Sub_57B500` reads the door's live angle, returning "not
blocking" unless it is at its closed angle, and returning **0 or 1 — which of the two segments
exists — from the door's own axis**:

```go
ang := *(*uint32)(unsafe.Add(ud, 12))
if ang != *(*uint32)(unsafe.Add(ud, 4)) {
    return -1
}
dp := DoorSize(byte(ang))
if dp.X > 0 && dp.Y > 0 { return 1 } else if ...
```

That is requirements §7's "a door reflects about its own axis" implemented as a per-query branch inside the
cell test, not as a separate body. And the whole `MapTraceRay` family takes a `MapTraceFlags`
bitmask that decides whether windows, secrets and doors block — **one index, five query
policies**, which is directly the shape requirements §7 wants for physics, AI sight and the vision sweep
reading the same data with different rules.

### 6.2 The O(1) claim, in code

Doom's blockmap is the canonical statement that a *uniform grid* mutation is O(1) regardless of
occupancy, because the per-object back-pointers make removal a splice rather than a scan
(`p_maputl.c`):

```c
if (thing->bnext)
    thing->bnext->bprev = thing->bprev;
if (thing->bprev)
    thing->bprev->bnext = thing->bnext;
else
{
    ...
    blocklinks[blocky*bmapwidth+blockx] = thing->bnext;
}
```
with re-insertion a head-push into the destination block's list. **Six pointer writes, no
scan.** For a one-segment-per-cell array it is lighter still: a store.

### 6.3 What it costs the alternatives — confirmed, and it is as decisive as it looks

| Structure | Cost of one door toggling |
| --- | --- |
| Dense per-cell array | one store; the query reads live state (Wolf3D, opennox) |
| Uniform hash | remove + insert into one bucket; O(chain) |
| Box2D `b2ChainShape` | **no per-segment edit API exists**; `b2DestroyChain` + `b2CreateChain` = O(N) shape destroy/create with broad-phase removes and inserts |
| Box2D loose static shapes | one `RemoveLeaf` + one `InsertLeaf` on the static tree — fine, but 65 536 leaves to build |
| Parry/Rapier `Bvh` | `insert_or_update_partially` then `refit`, documented **O(n) in node count** |
| Recast/Detour tile | rebuild the tile: `dtTileCache::addObstacle` + amortised `update()`, or `removeTile`/`addTile` |
| Doom REJECT-style precomputed visibility | not updated at all; the structure is deliberately coarse enough not to need it |
| Godot `TileMapLayer` | genuinely per-cell — only cells on `dirty.cell_list` are re-run — but each is a `body_create`/`body_clear_shapes`/`body_add_shape` round trip through `PhysicsServer2D` |

**None of these publishes a number.** Box2D publishes none for chain mutation or tree
insert/remove; Parry publishes an O(n) doc comment and no timing; Chipmunk's only cost text is
the qualitative doc line on `cpSpaceReindexStatic` — *"Generally updating only the shapes that
changed is faster."*

---

## 7. Attribution: the cell, and the merged run

### 7.1 Naming the cell is not the hard part

Every engine that needs it does it, and cheaply.

| Engine | Mechanism |
| --- | --- |
| opennox | `MapTraceRayAt(..., outGrid *image.Point, ...)` writes the grid cell on a hit |
| Godot | `bodies_coords[body] = r_cell_data.coords`, exposed as `get_coords_for_body_rid()` |
| Box2D v3 | `b2CastResultFcn( b2ShapeId shapeId, b2Vec2 point, b2Vec2 normal, float fraction, void* )` |
| Chipmunk | `cpSegmentQueryInfo { const cpShape *shape; cpVect point; cpVect normal; cpFloat alpha; }` |
| Detour | `dtNavMeshQuery::raycast` fills a `dtPolyRef` path alongside `t` and `hitNormal` |

With a dense grid the answer is free: **the cell index *is* the address the query already
computed**, so Q1's "which cell" costs one integer that the DDA is holding anyway. Note that
Box2D's own `b2ShapeCast` output struct — `{ normal, point, fraction, iterations, hit }` —
carries *no* identity; attribution is added one layer up by the world query. A design that puts
the cell in the return value of the primitive is strictly ahead of that.

### 7.2 The runs, and why uniform cells make this arithmetic

Requirements §7 requires merged colinear runs for rendering and vision while the cell stays the unit of
identity, and forbids physics privately owning the data. The driving game's vision spec has
already worked this out and it should not be re-derived here.
`../nox/docs/specs/vision-and-occlusion.md` §10, *"A wall is swept as a run and culled as a
segment"*:

> - **Sweep the run.** One occluder, as now. Cost is unchanged.
> - **Attribute and cull the segment.** When an interval survives, its two boundary rays
>   already land on the run's own face ... Those two points are a span *along* the wall, so the
>   segments it covers are an index range: **O(1) per surviving interval, no extra ray work.**

and, on why the cell is the identity:

> **What the segment is** is the authored 2 m cell, which is the unit the level is authored in
> ... per-cell properties, and a run that spans two of them was never a coherent object.

The same spec supplies the scale: **86 authored cells became 12 runs in one greybox room**, ~7×,
and it records the cost of *not* merging for vision as roughly 20× against its measured curve.

**Because cells are uniform, converting a point on a run to a cell is a division.** For a run
starting at cell *c* along +x, the cell containing a hit at distance *d* along the run is
`c + ⌊d / 2 m⌋`. No table, no search, no per-run index. This is the property that lets one
representation serve rendering and vision (runs) and physics (cells) without keeping two
structures in step — and it is a property of the *uniform grid specifically*. It would not hold
for arbitrary segment soup, which is the general case the literature addresses.

Doom is the cautionary version of the other choice: its linedefs are registered into every
block they touch, which requires `validcount` mailboxing on every query to avoid double-testing
one linedef. **One-segment-per-cell buys the absence of that machinery.**

---

## 8. The navmesh, the Rust/Bevy stack, and what the literature actually measured

### 8.1 Recast/Detour: attribution is fine, granularity of edit is the whole problem

`dtNavMeshQuery::raycast`
([`Detour/Source/DetourNavMeshQuery.cpp`](https://github.com/recastnavigation/recastnavigation/blob/main/Detour/Source/DetourNavMeshQuery.cpp))
is a polygon walk, and it demonstrates the attribution pattern cleanly: each polygon crossed is
appended to a path, and the normal is computed from the crossed edge at the end.

```cpp
// Store visited polygons.
if (n < hit->maxPath) hit->path[n++] = curRef;
...
if (!nextRef)
{
    // No neighbour, we hit a wall. Calculate hit normal.
    const int a = segMax;
    const int b = segMax+1 < nv ? segMax+1 : 0;
    ...
    hit->hitNormal[0] = dz;  hit->hitNormal[1] = 0;  hit->hitNormal[2] = -dx;
    dtVnormalize(hit->hitNormal);
```

The per-polygon primitive is `dtIntersectSegmentPoly2D` in `DetourCommon.cpp`, a Cyrus–Beck
slab clip that also returns `segMin`/`segMax` — the *edge indices* — which is how the walk knows
which neighbour link to follow. **Identity survives because the polygon boundaries were
preserved at bake time**, which is the same reason a per-cell segment index can name a cell.

Mutation is where it separates. Detour's per-tile `dtBVNode` tree is built once by
`createBVTree` in `DetourNavMeshBuilder.cpp` — a `qsort` plus recursive median split into a flat
escape-index array — and **there is no insert, remove or refit API anywhere in the codebase.**
`queryPolygonsInTile` walks it as a read-only blob. Runtime change is whole-tile replacement:
`dtNavMesh::addTile`/`removeTile`, or `dtTileCache`'s obstacle queue.

`DetourTileCache` is the honest statement of what that costs. It is a throttled budget system —
`MAX_REQUESTS = 64`, `MAX_UPDATE = 64`, `DT_MAX_TOUCHED_TILES = 8` — whose `update()` doc comment
says so:

```
/// Updates the tile cache by rebuilding tiles touched by unfinished obstacle requests.
///  @param[out] upToDate  Whether the tile cache is fully up to date with obstacle requests and tile rebuilds.
```

and whose body processes **exactly one tile per call**, with `buildNavMeshTile` running a full
local re-bake — decompress the layer, `dtMarkCylinderArea`/`dtMarkBoxArea`, then
`dtBuildTileCacheRegions` → `dtBuildTileCacheContours` → `dtBuildTileCachePolyMesh` →
`dtCreateNavMeshData` → `removeTile` + `addTile`. It even sets `params.buildBvTree = false`,
because the BV-tree is a bake-time luxury the dynamic path skips.

**A door in Recast is a tile re-bake, amortised across frames.** Recast publishes no number for
it: no README, wiki page or `Docs/*.md` in the repository states a rebuild time.

Recast also supplies the cleanest evidence for §3.4. `rcHeightfield`'s `addSpan`
([`Recast/Source/RecastRasterization.cpp`](https://github.com/recastnavigation/recastnavigation/blob/main/Recast/Source/RecastRasterization.cpp))
documents itself as *"Adds a span to the heightfield. If the new span overlaps existing spans, it
will merge the new span with the existing ones"* and then frees the merged-away span. **Occupancy
rasterisation is lossy of source-primitive identity by construction** — which is exactly why a
bitset cannot serve Q1 or Q8.

### 8.2 Bevy has no broadphase, and its swept tests are Minkowski reductions

Bevy core ships no physics. What it ships is primitive-vs-primitive casts, now in
`bevy_shape::bounding` (moved out of `bevy_math`;
[`crates/bevy_shape/src/bounding/raycast2d.rs`](https://github.com/bevyengine/bevy/blob/main/crates/bevy_shape/src/bounding/raycast2d.rs)),
and the swept versions are exactly the reduction §4.1 and §4.2 describe, in three lines each:

```rust
pub fn aabb_collision_at(&self, mut aabb: Aabb2d) -> Option<f32> {
    aabb.min -= self.aabb.max;
    aabb.max -= self.aabb.min;
    self.ray.aabb_intersection_at(&aabb)
}

pub fn circle_collision_at(&self, mut circle: BoundingCircle) -> Option<f32> {
    circle.center -= self.circle.center;
    circle.circle.radius += self.circle.radius();
    self.ray.circle_intersection_at(&circle)
}
```

**Sweeping a circle is a ray against a radius-summed circle; sweeping a box is a ray against a
Minkowski-differenced box.** No iteration, no GJK. The same reduction applied to a circle and an
axis-aligned segment gives a ray against a stadium — and, with the endpoints suppressed (§5), a
ray against a widened slab.

`avian2d`'s spatial queries run on `ColliderTrees`, whose module doc is worth quoting because it
is the best-documented *good* BVH answer for static geometry
([`src/collider_tree/mod.rs`](https://github.com/avianphysics/avian/blob/main/src/collider_tree/mod.rs)):

> Colliders of dynamic, kinematic, and static bodies are all stored in a separate ColliderTree
> ... Trees for dynamic and kinematic bodies are rebuilt every physics step, while the static
> tree is incrementally updated when static colliders are added, removed, or modified.

(It uses its own `obvhs` BVH, not parry's `Qbvh`.) **No Bevy-adjacent crate ships a uniform grid
or spatial hash for static geometry**; the community `bevy_spatial` crate offers KD-tree and grid
options but is not first-party.

### 8.3 The literature says grids win on densely, regularly occupied scenes — and says it plainly

This is the closest thing to a direct answer in the primary literature, and it is worth having
because Amanatides & Woo deliberately do not provide one.

Vlastimil Havran, *Heuristic Ray Shooting Algorithms*, PhD thesis, Czech Technical University,
Prague ([author's own copy](https://dcgi.fel.cvut.cz/home/havran/DISSVH/dissvh.pdf)). Twelve ray
shooting algorithms — BSP, kd-tree, uniform grid, BVH, adaptive grid, recursive grid, HUG and
four octree variants — over 30 SPD scenes on a Pentium II 350 MHz. The headline is *against*
grids:

> We can see that the winner in the tests on SPD scenes is the RSA based on the **kd-tree**,
> while the RSA based on the **BVH has the worst average running time**, being in some tests
> even more than two orders of magnitude slower than the former.

but the exception is stated explicitly, and it is this problem:

> UG: Classical RSA. The employed smart algorithm for heterogeneous grid resolution setting
> results in **the best performance of UG for several scenes. These scenes are densely occupied
> with mostly regular structure** ("jacksX", "latticeX", and "mountX"). In this kind of scene the
> down traversal phase for hierarchical spatial data structures is expensive, since the ray
> intersects the object very close to the origin of a ray. For sparsely occupied scenes the UG
> has rather poor performance as it lacks a sense of hierarchy.

> After testing 12 RSAs over 30 SPD scenes of different complexities and 4 testing procedures, we
> can conclude that using RSAs based on hierarchical spatial data structures, particularly on the
> kd-tree, definitely pays off — **except for densely occupied scenes.**

His own scene-classification heuristic routes low-sparseness scenes to the uniform grid and
mid-range ones to the kd-tree. **A 256 × 256 grid with a segment in a large fraction of its cells
is the low-sparseness case by construction**, and Havran's second sentence names the mechanism
precisely: a hierarchy's descent cost is wasted when the ray hits something almost immediately —
which is the *contact-gate trace of §1*, 1 to 3 cells long.

Wald, Boulos and Shirley, *Ray Tracing Deformable Scenes using Dynamic Bounding Volume
Hierarchies*, ACM TOG 26(1), 2007
([author's copy](https://www.sci.utah.edu/~wald/Publications/2007/BVH/download/togbvh.pdf))
supplies the refit-versus-rebuild figures, on one 2.6 GHz Opteron core:

| Scene | Triangles | Full BVH build | Triangle update | AABB refit |
| --- | --- | --- | --- | --- |
| runner | 78 k | 0.13 s | 0.96 ms | 0.47 ms |
| fairy | 180 k | 1.26 s | 6.88 ms | 4.76 ms |
| toys | 11 k | — | 15.89 ms | 12.91 ms |

> (1) BVH refits are small, and do not greatly influence run time.
> ...
> (2) The intentionally bad BART model breaks the refitting approach, resulting in a significant
> performance deterioration during deformation.

**Read the caveat carefully before importing the conclusion.** These are *deforming* meshes with
constant topology — vertices move, the tree's shape stays — which is the case refit is designed
for. Toggling a door is a **topology change**: a leaf appears or disappears. That is the case
where a refit does not apply and an insert/remove or rebuild does. Their grid comparison (Table
VIII) has the grid rebuilt *in full every frame*, which is the worst possible grid and still only
loses "for all scenes but BART". Neither of these axes is the one this design sits on.

**No paper found publishes a number for the exact configuration here** — a dense 2D
axis-aligned-segment grid with discrete per-cell mutation. Havran's dense/regular result and
Wald's refit table are the closest analogues, and both are 3D and both differ in the mutation
model.

---

## 9. What nobody publishes, and what would have to be measured

Listed as findings, because "this needs measuring" is a legitimate outcome and the map has a
prototype exception reserved.

1. **No primary source publishes a per-cell traversal cost in time.** Amanatides & Woo publish
   an operation count (2 FP comparisons, 1 FP add, 2 int comparisons, 1 int add per 3D cell; 33
   FP ops of setup) and whole-image render times on a Sun 3/75. Nobody publishes nanoseconds per
   cell on a modern machine, for any grid, in any of the sources read here. **A Go DDA over a
   64 KiB byte array is a measurement this effort would have to take itself.**
2. **Nobody publishes the cache effect of the split layout.** The claim in §3.4 — that an 8 KiB
   occupancy bitset in front of a 64 KiB shape array beats a single fatter array — is
   arithmetic about L1 residency, not a measurement. It is a small, well-shaped benchmark.
3. **No engine publishes a cost for mutating a static index.** Box2D publishes nothing for chain
   destroy/create, tree insert or tree remove — its own GDC 2019 slides say *"Leaf removal is
   straight forward and is not covered."* Parry publishes O(n) and no timing. Chipmunk publishes
   only "prefer updating the shapes that changed". Recast publishes no tile-rebuild time in any
   README, wiki page or `Docs/*.md`. So the grid-versus-BVH mutation gap is established
   **structurally** (there is no per-segment chain edit API, and no navmesh BV-tree refit API at
   all) rather than numerically.
4. **No first-party benchmark exists at this scale or shape.** Box2D v3's `benchmark/` CSVs
   cover `joint_grid`, `large_pyramid`, `many_pyramids`, `rain`, `smash`, `spinner`, `tumbler` —
   all dynamics stress tests, none a static chain grid, none isolating `b2ShapeCast` or a tree
   insert. Rapier and Chipmunk publish nothing at 65 536-static-collider scale in 2D.
5. **Whether the doubled contact of §5.2 is visible at stiffness 25 is unknown.** It is a
   consequence derived from Chipmunk's code and the requirements' §5 accumulator, not something anyone has
   measured for this response model.
6. **Q3's real cost is a call-count problem, and the call count is nox's.** §1 establishes the
   traces are 1–3 cells. What is not established is how many there are per tick. That is the
   number the map's reserved prototype should produce, and it is a property of the game, not of
   the index.

7. **The two papers that do publish grid-versus-hierarchy numbers are both 3D and neither
   matches the mutation model.** Havran's dense-scene result (§8.3) is the right *density*
   regime and the wrong dimension and geometry; Wald's refit table is the right *incremental
   update* question but for deforming meshes of constant topology, where a door — a leaf
   appearing and disappearing — is not the case refit addresses.

Three numbers that *are* published and are worth carrying, all first-party or author-hosted,
each with the caveat that none measures static 2D segment indexing:

- **Scott Lembcke (Chipmunk's author), *AABB Tree Shootout*, 2023-01-17**
  ([slembcke.net/blog/TreePerf](https://www.slembcke.net/blog/TreePerf/)), Ryzen 7 3700X on Pop
  OS 22.04, ~40 k dynamic circles, ~15 k collisions/frame: *"The same simulation that took 45 ms
  to run with b2DynamicTree takes 12 ms to run using cpBBTree."* And the failure mode:
  *"increasing the speed by 10x makes the cache worthless. The bounding boxes are expanded so
  much that it just gets clogged up with false positives ... and jumps to a whopping 284 ms."*
  He also warns the average is useless — *"Chipmunk in particular has very inconsistent frame
  timing ... all of the graphs above use a 95 percentile measurement."* **A tree's cost is
  velocity-dependent; a grid's is not.**
- **Dimforge, *Announcing the Rapier physics engine*, 2020-08-25**
  ([dimforge.com/blog/2020/08/25/…](https://dimforge.com/blog/2020/08/25/announcing-the-rapier-physics-engine/)),
  Ryzen 9 3900X 3.8 GHz / Core i7 7920HQ 3.1 GHz: *"Rapier is generally slightly faster than
  Box2d in these benchmarks, especially when joints are involved."* Relevant only as a bound on
  what adopting a Rust-class engine could buy, which §12 of the requirements has already
  argued is not the axis that matters.
- **Wald, Boulos & Shirley, ACM TOG 26(1) 2007**, one 2.6 GHz Opteron core: a BVH **refit** costs
  0.47 ms at 78 k triangles and 4.76 ms at 180 k, against a **full build** of 0.13 s and 1.26 s
  — roughly 250× — with the authors' own conclusion that *"BVH refits are small, and do not
  greatly influence run time."* The table is in §8.3 with the reason it does not transfer.

---

## 10. Bearing on questions the map has not asked

Three things surfaced that this ticket did not ask for. The first two sharpen
[#288](https://github.com/dvoyni/cog/issues/288) in ways its seven questions do not currently
reach; the third is not on any ticket at all.

**(a) One index, several query policies, is a shipped pattern — and it is a read-only pattern.**
opennox's `MapTraceFlags` selects per call whether windows, secrets, doors and out-of-range
cells block; Doom reaches the same end with three different structures. The version worth
copying is opennox's: **the index is immutable during a frame and the *policy* is a parameter of
the query, not a property of the index.** That is directly relevant to #288's read/write split —
it means AI sight, the contact gate, the vision sweep and projectile validation can all be
`Read` on the same Resource with no duplication and no per-reader index — but #288 asks where
the geometry lives and who writes it, not whether the *filter* belongs to the caller. It should.

**(b) The mutable state does not have to live in the index, and if it does not, the write lock
may not be needed at all.** Every shipped engine here keeps the door's live state *outside* the
structure the query traverses — Wolf3D's `doorposition[]`, Doom's sector heights, opennox's door
object behind a pointer in the cell. If cog follows that, the wall index becomes **immutable for
the whole level** and the only thing that changes per tick is a small side table. #288 asks "can
the index be a read Resource written by exactly one System at one defined point"; the shipped
answer is stronger — *there may be nothing to write*, and the per-cell flags (door, secret,
breakable, broken, window) are a separate, small, hot Resource with a different owner and a
different lifetime from the geometry. That reframes #288's question 3 ("does the flag store and
the geometry index have the same owner?") from a preference into a load-bearing choice, and
suggests the answer is *no*.

**(c) The 1.97 m boulder against the 2 m cell is a 1.5% margin that a whole class of
simplifications rests on.** §1's 3×3 bound, the fixed nine-load Q8 gather, and the one-cell
swept corridor all hold *only* because every circle is smaller than a cell. Nothing in the map
or the requirements records this as a constraint on future content — it is currently an accident
of the recovered numbers. If it is to be relied on, it belongs in the spec as an invariant with
an assertion, alongside the compound-shape watch the map already keeps. That is a
domain-modelling question the map has not asked.
