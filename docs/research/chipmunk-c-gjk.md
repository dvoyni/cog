# Does Chipmunk's C GJK report disjoint Shapes as touching on a Minkowski edge's supporting line?

Research for [dvoyni/cog#506](https://github.com/dvoyni/cog/issues/506), child of the
physics follow-up map [#315](https://github.com/dvoyni/cog/issues/315), serving the
decision on [#439](https://github.com/dvoyni/cog/issues/439).

**Answer: yes.** Chipmunk2D's C does exactly what the port does. `ClosestPointsNew`
takes its "overlapping" branch on `d <= 0.0f`, and `d` is the origin's distance to the
*supporting line* of the closest Minkowski edge, not to the edge. When the origin sits on
that line but beyond the edge's end, `d` is exactly `0`, the vertex/vertex branch that
would have measured the real distance is skipped, and the pair is reported at distance
zero. The line is the same, character for character, in Chipmunk 7.0.2 (the release
`jakecoffman/cp` ports), in current `master`, in `jakecoffman/cp` v2.4.0 and in
`bundles/ecsphysics2d`. It is not a transliteration slip in cp, and C has no guard that
cp dropped.

**Reproduced in C.** The fuzz's own placements, run through Chipmunk's
`cpShapesCollide` built from source, give **436 of 3,848** box-disjoint pairs reported
as touching, the port's count exactly. The headline pair (a capsule and a segment whose
surfaces are 1.5 m apart) comes back with two contacts at a depth of 0.25 m, and a
one-line trace inside `ClosestPointsNew` shows `d = 0` on an edge the origin lies on the
extension of. See [Reproduction](#reproduction).

No upstream issue, commit or comment acknowledges the case. See
[Upstream record](#upstream-record).

## Sources

| Source | Revision |
|---|---|
| Chipmunk2D `master` | [`f2f3d66`](https://github.com/slembcke/Chipmunk2D/tree/f2f3d66220b8bb1568b8402c6d194d73d4e477d1) (2026-05-05) |
| Chipmunk2D `Chipmunk-7.0.2` | [`09c5469`](https://github.com/slembcke/Chipmunk2D/tree/09c546986f121a4873b6d1a4ce90572f4743d692) (2017-08-28) |
| Chipmunk2D `Chipmunk-7.0.3` | [`87340c2`](https://github.com/slembcke/Chipmunk2D/tree/87340c216bf97554dc552371bbdecf283f7c540e) (2019-06-07) |
| jakecoffman/cp `v2.4.0` | [`64b09f5`](https://github.com/jakecoffman/cp/tree/64b09f584d36466266282dcfe80c5b9651592b44) (2025-12-25) |
| ecsphysics2d | `bundles/ecsphysics2d/internal/types/gjk.go` on `main` at `6a5254d` |

All line numbers below are for those revisions. Both repositories were cloned and read
directly; nothing here comes from a secondary write-up.

## Which Chipmunk release jakecoffman/cp ports

cp states no Chipmunk version anywhere: its README says only "Go port of
Chipmunk2D", and no Go file carries a version constant. The version is fixed by the
code instead.

- **cp began on 2017-08-04** (its first commit, `dffb98c init`). Chipmunk 7.0.2 was
  tagged on 2017-08-28, and `cpCollision.c` had not changed on `master` since
  2015-10-29 (`a46ebb9`), so the C cp was reading is the 7.0.2 file.
- **cp has the October 2015 changes that 7.0.1 lacks.** cp's `PointGreater` and
  `CheckAxis` (`vector.go:155` and `:159`) are `cpCheckPointGreater` and `cpCheckAxis`
  from `cpRobust.c`, which arrived in `3b5d27d` ("Fixed a precision bug in the GJK
  algorithm", 2015-10-28) and `0651556` ("Simplify cpRobust.c", 2015-10-29). Both are
  ancestors of `Chipmunk-7.0.2` and not of `Chipmunk-7.0.1` (2015-07-02).
- **cp lacks the June 2019 change that `master` has.** cp's `ClosestT`
  (`vector.go:163-166`) divides by a bare `delta.LengthSq()`. `master` adds
  `+ CPFLOAT_MIN` to that divide in `e7ea51e` ("Fix possible divide by zero in
  collisions", 2019-06-10), after both 7.0.2 and 7.0.3.

For `src/cpCollision.c`, `git diff Chipmunk-7.0.2 Chipmunk-7.0.3` is empty, and
`git diff Chipmunk-7.0.2 master` is two lines: that `ClosestT` divide (line 203) and
the `CircleToPoly` contact sign from `bc09ed5` (line 677). `src/cpRobust.c` is
identical at all three. **Neither difference touches the lines below**, and every line
number quoted is the same at 7.0.2 and at `master`.

## The decisive C

`src/cpCollision.c`, `ClosestPointsNew`, lines 227-258, identical at
[7.0.2](https://github.com/slembcke/Chipmunk2D/blob/09c546986f121a4873b6d1a4ce90572f4743d692/src/cpCollision.c#L227-L258)
and [`master`](https://github.com/slembcke/Chipmunk2D/blob/f2f3d66220b8bb1568b8402c6d194d73d4e477d1/src/cpCollision.c#L227-L258):

```c
231	cpFloat t = ClosestT(v0.ab, v1.ab);
232	cpVect p = LerpT(v0.ab, v1.ab, t);
...
240	// First try calculating the MSA from the minkowski difference edge.
241	// This gives us a nice, accurate MSA when the surfaces are close together.
242	cpVect delta = cpvsub(v1.ab, v0.ab);
243	cpVect n = cpvnormalize(cpvrperp(delta));
244	cpFloat d = cpvdot(n, p);
245	
246	if(d <= 0.0f || (-1.0f < t && t < 1.0f)){
247		// If the shapes are overlapping, or we have a regular vertex/edge collision, we are done.
248		struct ClosestPoints points = {pa, pb, n, d, id};
249		return points;
250	} else {
251		// Vertex/vertex collisions need special treatment since the MSA won't be shared with an axis of the minkowski difference.
252		cpFloat d2 = cpvlength(p);
253		cpVect n2 = cpvmult(p, 1.0f/(d2 + CPFLOAT_MIN));
```

`n` is the unit normal of the *edge's line*, and `d = n·p` is the signed distance from
the origin to that line. When `t` is clamped to `-1` or `1` the closest point `p` is an
end vertex. If the origin is on the line through the edge, that vertex is on the line
too, so `d` is exactly `0` and `d <= 0.0f` is true. The comment on line 247 says what
the branch is for, "the shapes are overlapping", but the test cannot tell overlap
(`d < 0`) or real contact from the origin being collinear with an edge it lies beyond.
Line 252 (`d2 = cpvlength(p)`), which is the correct distance in that configuration,
is never reached.

The branch is reached from `GJKRecurse` (lines 349-393) when `cpCheckAxis` accepts the
current edge, line 377, `if(cpCheckAxis(v0.ab, v1.ab, p.ab, n)){`, then line 380,
`return ClosestPointsNew(v0, v1);`. It is also reached at the iteration cap on line 353.
When `t` is clamped, the search direction on line 361 is `cpvneg(LerpT(v0.ab, v1.ab, t))`,
the negated end vertex. The new support point is then no further along it than that
vertex, `cpCheckAxis` (`cpRobust.c:11-13`,
`return cpvdot(p, n) <= cpfmax(cpvdot(v0, n), cpvdot(v1, n));`) accepts, and GJK stops
on the collinear edge.

`SegmentToSegment` then compares that `d` with the summed radii at line 594,
`points.d <= (seg1->r + seg2->r) && (`, so `d = 0` passes for any pair with radius,
and it also passes at radius zero.

The condition has been in this form since at least `fcbfb19` ("Comments and a couple
small fixes", 2013-12-22). That commit changed `(0.0f < t && t < 1.0f)` to
`(-1.0f < t && t < 1.0f)` and left `d <= 0.0f` as it was.

## Line by line against cp v2.4.0 and the port

| Step | Chipmunk C (7.0.2 = master) | jakecoffman/cp v2.4.0 `collision.go` | ecsphysics2d `gjk.go` |
|---|---|---|---|
| closest t | `:231` `ClosestT(v0.ab, v1.ab)` | `:226` `v0.ab.ClosestT(v1.ab)` | `:105` `v0.ab.ClosestT(v1.ab)` |
| closest p | `:232` `LerpT(v0.ab, v1.ab, t)` | `:227` `v0.ab.LerpT(v1.ab, t)` | `:106` `lerpT(v0.ab, v1.ab, t)` |
| edge normal | `:243` `cpvnormalize(cpvrperp(delta))` | `:238` `delta.ReversePerp().Normalize()` | `:115` `delta.ReversePerp().Normalize()` |
| line distance | `:244` `cpvdot(n, p)` | `:239` `n.Dot(p)` | `:116` `n.Dot(p)` |
| **the branch** | `:246` `if(d <= 0.0f \|\| (-1.0f < t && t < 1.0f))` | `:241` `if d <= 0 \|\| (-1 < t && t < 1)` | `:118` `if d <= 0 \|\| (-1 < t && t < 1)` |
| vertex/vertex | `:252-253` `cpvlength(p)`, `1/(d2 + CPFLOAT_MIN)` | `:247-248` `p.Length()`, `1/(d2 + 1e-15)` | `:125-126` `p.Length()`, `1/(d2 + smallestNormal)` |
| flip test | `:356` `cpCheckPointGreater(v1.ab, v0.ab, cpvzero)` | `:385` `v1.ab.PointGreater(v0.ab, Vector{})` | `:200` `pointGreater(v1.ab, v0.ab, m.Vec2d{})` |
| search axis | `:361` `perp(v1-v0)` inside, `-LerpT(...)` clamped | `:391-395` same | `:207-211` same |
| EPA test | `:372` | `:400` | `:214` |
| stop test | `:377` `cpCheckAxis(v0.ab, v1.ab, p.ab, n)` | `:404` `v0.ab.CheckAxis(v1.ab, p.ab, n)` | `:220` `checkAxis(v0.ab, v1.ab, p.ab, n)` |
| cold-start axis | `:464` `cpvperp(cpvsub(cpBBCenter(bb1), cpBBCenter(bb2)))` | `:369` same | `:179` same, plus the seeded fallback for a zero axis |

The only differences on this path are the small denominators: cp's `1e-15` where C has
`CPFLOAT_MIN` (`cpVect.h:149` in `cpvnormalize`, `cpCollision.c:253`), and C `master`'s
`+ CPFLOAT_MIN` in `ClosestT`, which the port carries and cp does not. None of them
changes the result here: the edge in question has length 1, `n` is exactly axis-aligned,
and `d` is an exact `0` either way. The port's seeded fallback for a zero cold-start
axis is read only when the two box centres coincide, which a box-disjoint pair never has.

**So the difference #439's option 1 asks about is neither a guard nor a slip. The C has
no guard, and cp copied the C faithfully.** Adding a guard would depart from Chipmunk
as well as from cp.

## Reproduction

### Harness

Everything is throwaway and outside the repo.

1. **The draws.** A small Go program replays the fuzz's pair sweep exactly:
   splitmix64 from `0x9E3779B97F4A7C15`, the same `intn` sequence for kind, kind, angle,
   angle, lattice, the one-in-three coincidence draw and the second lattice, and the same
   14-Shape catalogue built through the public `ecsphysics2d` constructors. It calls the
   public `ecsphysics2d.Penetration` both ways round and prints one line per draw with
   the placements and the port's answer.
2. **The C.** A C driver builds the same 14 Shapes with Chipmunk's own constructors
   (`cpCircleShapeNew`, `cpSegmentShapeNew` + `cpSegmentShapeSetNeighbors`,
   `cpBoxShapeNew`, `cpBoxShapeNew2`, `cpPolyShapeNew`) on kinematic bodies placed with
   `cpBodySetPosition` and `cpBodySetAngle`. It takes each box from `cpShapeCacheBB`,
   and for every pair with `!cpBBIntersects` it calls the public `cpShapesCollide` both
   ways round. "Touching" means `count > 0`.
3. **Built from source**, linking every `src/*.c` except `cpHastySpace.c`, with
   `clang 20.1.3 (x86_64-pc-windows-msvc) -O2 -std=gnu99 -DNDEBUG`, once from
   `Chipmunk-7.0.2` and once from `master`. There was no `-ffast-math`, although
   Chipmunk's own CMake adds it for release builds (`CMakeLists.txt:59`). A third
   `master` build with `-ffast-math` is reported separately.

### Results over the whole sweep

| Build | box-disjoint pairs | touching (C, a against b) | port, same order | both | C only | port only |
|---|---|---|---|---|---|---|
| Chipmunk 7.0.2 | 3,848 | **436** | 436 | 435 | 1 | 1 |
| Chipmunk `master` | 3,848 | **436** | 436 | 435 | 1 | 1 |
| `master` with `-ffast-math` | 3,852 | 440 | 437 | 435 | 5 | 2 |

The 3,848 box-disjoint pairs match the fuzz's own logged count, so the C boxes and the
port's boxes agree pair for pair. The 436 matches the fuzz's
`436 of those reported touching anyway`, logged by
`TestTheFinitenessInvariantHoldsOverASweepOfDegenerateInputs` on this machine.

The two pairs where C and the port disagree, draws 5491 and 5931, are both one-way
answers: each reports touching in one argument order and not the other. At draw 5491,
C reports touching with a against b and the port with b against a. This is #439's
second consequence, the end-cap rejection at `d = 0` reading one way only. Why the two
disagree on the order was not investigated. It does not bear on the question: at both
draws, both implementations report a box-disjoint pair as touching in some order.

With `-ffast-math`, the four extra disjoint pairs are boxes computed differently. The
headline pairs below give identical answers in that build.

The circle-against-polygon depths differ between 7.0.2 and `master` at a few draws, for
example draw 104 at 0.25 against 0.75. That is the `bc09ed5` contact-sign fix in
`CircleToPoly`, which is downstream of GJK. The *touching* verdicts are identical
between the two builds.

### The headline pair

Draw **5491**:

- A is a capsule, the segment (−0.5, 0)–(0.5, 0) with radius 0.25, at (−1, 1.5)
  turned by π/2. It is vertical, x = −1, y from 1 to 2.
- B is a segment (−0.5, 0)–(0.5, 0) with radius 0, at (1.25, 1), angle 0. It is
  horizontal, y = 1, x from 0.75 to 1.75.

The closest core points are (−1, 1) and (0.75, 1), 1.75 m apart. The capsule's
surface is **1.5 m** from B. The boxes are
(−1.25, 0.75)–(−0.75, 2.25) and (0.75, 1)–(1.75, 1), which are disjoint.

The Minkowski difference B − A is the rectangle x ∈ [1.75, 2.75], y ∈ [−1, 0]. Its top
edge lies on y = 0, which passes through the origin, and the edge starts 1.75 to the
right of it.

To trace it, one statement was added after line 244 of `master`'s `cpCollision.c`, and
nothing else was changed:

```c
fprintf(stderr, "ClosestPointsNew: v0.ab=(%.17g,%.17g) v1.ab=(%.17g,%.17g) t=%.17g p=(%.17g,%.17g) n=(%.17g,%.17g) d=%.17g\n", ...);
```

Output, with B a plain segment (the catalogue's `a segment`, kind 5):

```
ClosestPointsNew: v0.ab=(1.75,0) v1.ab=(2.75,0) t=-1 p=(1.75,0) n=(0,-1) d=0
ClosestPointsNew: v0.ab=(-1.75,0) v1.ab=(-2.75,0) t=-1 p=(-1.75,0) n=(0,1) d=0
```

The first line is `cpShapesCollide(A, B)` and the second is `cpShapesCollide(B, A)`.
In both, GJK stops on the collinear edge with `t = -1` and `d = 0`, where `|p| = 1.75`.

| Implementation | A against B | B against A |
|---|---|---|
| Chipmunk 7.0.2, `cpShapesCollide` | 2 contacts, normal (0, −1), depth 0.25 | 0 contacts |
| Chipmunk `master`, `cpShapesCollide` | 2 contacts, normal (0, −1), depth 0.25 | 0 contacts |
| jakecoffman/cp v2.4.0, `cp.ShapesCollide` | 2 contacts, normal (0, −1), distance −0.2499999999999995 | 0 contacts |

GJK returns `d = 0` in both orders. In the reverse order, Chipmunk's `ContactPoints`
clipping emits no contact, which is the either-way-round asymmetry. The same happens
with the catalogue's actual B, the chained segment (kind 7, neighbours (−1, 0) and
(1, 0)). There, C gives A against B 2 contacts at depth 0.25 and B against A none, and
the port gives A against B nothing and B against A touching at depth 0.25.

jakecoffman/cp was run from the clone at `v2.4.0` through `replace`, with a plain
segment, because cp exports no neighbour setter (`segment.go:167-168` zeroes the
tangents).

Two more pairs show the same trace:

- Draw 3644, a chained segment turned by −π/2 at (−2, −1) against a segment at
  (−1.5, 0). The trace is `v0.ab=(0,0.5) v1.ab=(0,1.5) t=-1 n=(1,-0) d=0`, a vertical
  edge on x = 0 with the origin beyond its end.
- Draw 4094, two parallel horizontal segments on y = −1.25, 1.25 m apart core to core.
  Here the Minkowski difference has no area and GJK's two support points coincide
  (`v0.ab = v1.ab = (2.25, 0)`), so `n` is the guarded normalisation of the zero vector
  and `d = 0` again.

### What was not reproduced

- **Chipmunk was not run inside a `cpSpace`.** Only the public pair primitive
  `cpShapesCollide` was run, which, like the port's `Penetration`, has no broadphase in
  front of it. Inside a space, `QueryReject` in `cpSpaceStep.c:220-224` returns early on
  `!cpBBIntersects(a->bb, b->bb)`, the same gate #439 cites at `detect.go:137` and
  `index.go:261`. So **Chipmunk has the same layering as the port**: the behaviour is
  reachable only through the bare pair primitive.
- **jakecoffman/cp was run on the one headline pair only**, not on the whole sweep.
- **No platform other than Windows x64 with clang** was tried.

## Upstream record

Nothing in the upstream record acknowledges the case.

- **Chipmunk2D issues and PRs.** GitHub search on `slembcke/Chipmunk2D` for `GJK`,
  `collinear`, `touching`, `false collision`, `ClosestPoints`, `phantom`,
  `cpShapesCollide`, `segment` and `collision` turned up nothing about it. The nearest
  are these two:
  - [#96](https://github.com/slembcke/Chipmunk2D/issues/96), open since 2015-02-16:
    "There is a divide by zero error when two line segments collide and exactly share
    an endpoint." That is a different degenerate case, in the segment-segment path.
  - [#158](https://github.com/slembcke/Chipmunk2D/issues/158), closed: internal-edge
    snagging, answered with "Add a smoothing radius".
- **Chipmunk2D history.** `git log --all -i --grep=gjk --grep=closest --grep=minkowski
  --grep=touch` lists the 2012-2013 GJK development commits, `3b5d27d` (the
  `cpCheckAxis` precision fix) and `fcbfb19`. None of them concerns an origin
  collinear with an edge. `git log -S` on the branch line finds only `fcbfb19`, which
  kept `d <= 0.0f`.
- **Source comments.** The only comments near the line are the two quoted above:
  "If the shapes are overlapping, or we have a regular vertex/edge collision" and
  "Vertex/vertex collisions need special treatment". Neither mentions the collinear
  case.
- **jakecoffman/cp.** A search for `GJK` in its issues finds nothing. According to
  `git log -S"if d <= 0 ||" -- collision.go`, the line arrived in `c307882`
  ("collisions working some", 2017-08-15) and no later commit has changed it.

## What this settles for #439

Option 1's premise, "a defect in cp", holds, and **it is Chipmunk's defect, inherited
verbatim**. It is not a porting error. Whether to depart from it is still #439's
decision. Chipmunk shares the port's mitigation: its engine path is box-gated, and only
its public `cpShapesCollide` exposes the behaviour, which is the port's `Penetration`
exactly.
