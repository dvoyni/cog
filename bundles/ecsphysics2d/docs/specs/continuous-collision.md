# cog ecsphysics2d — continuous collision

A solid Body that moves far enough in one tick to get through something is
stopped where it first meets it, instead of ending up beyond it. This document
specifies how `ecsphysics2d` does that for every Body in the world, and what it
costs a world where nothing moves fast.

**It is an addition to the port, not a port.** cp has no continuous collision:
its answer to tunnelling is the app's, a segment query by hand or a Shape large
enough that one step cannot clear it. So this sits beside the swept Sensor as
the second place the package goes past cp, and cp's behaviour is untouched for
every Body the mechanism does not engage. The design is Box2D v3's and Rapier's
(since 0.35), fitted to cp's order of Systems; nothing is taken from either.

It is assembled from the six resolved tickets of
[physics: continuous collision for the whole world](https://github.com/dvoyni/cog/issues/328):

- [research: how 2D engines stop tunnelling without sub-steps, and what each costs per Body](https://github.com/dvoyni/cog/issues/575)
- [task: a reference tunnelling scene and its cost harness](https://github.com/dvoyni/cog/issues/576)
- [physics: the continuous-collision mechanism and where it sits in the five Systems](https://github.com/dvoyni/cog/issues/577)
- [physics: the Dynamic opt-in to continuous collision, its name and how two moving Bodies meet](https://github.com/dvoyni/cog/issues/578)
- [physics: whether the swept Sensor moves onto continuous collision's gate and path test](https://github.com/dvoyni/cog/issues/579)
- [physics: what the tunnelling scene must show, and at what cost, before continuous collision hands over](https://github.com/dvoyni/cog/issues/580)

Every section cites the tickets it came from. As in
[`ecsphysics2d.md`](ecsphysics2d.md), a claim resting on something unverified is
marked **Gap**, and something settled by putting the decisions side by side,
which no ticket did, is marked **Settled here**.

The whole of it is bound by one hard constraint: **no System's lock set widens,
and no System that runs in parallel today is made to take turns.** Every choice
below was checked against that, and where the obvious design broke it, the
design changed.

**Nothing of this is implemented.** The reference scene exists, and its nine
cases fail today by design. [Required work](#required-work) is the whole build,
and [Acceptance](#acceptance) is what it must show.

---

## Contents

- [Vocabulary](#vocabulary) · [What an app sees](#what-an-app-sees)
- [The gate](#the-gate) · [Where each part runs](#where-each-part-runs)
- [Index: one bit and a path box](#index-one-bit-and-a-path-box)
- [Detect: one path pass](#detect-one-path-pass) · [Solve: back to `T`](#solve-back-to-t)
- [Every moving Sensor is swept](#every-moving-sensor-is-swept)
- [Probing a Shape](#probing-a-shape)
- [Named limits](#named-limits)
- [What changes in `ecsphysics2d.md`](#what-changes-in-ecsphysics2dmd)
- [Shapes that were rejected](#shapes-that-were-rejected)
- [Acceptance](#acceptance) · [Required work](#required-work) · [Out of scope](#out-of-scope)

---

## Vocabulary

Every term is in `CONTEXT.md` under **Physics**, which is the glossary of record.

- **Continuous collision**: stopping a Body that moves far enough in one tick
  to get through something, so it meets what it would have hit instead of ending
  up beyond it. It engages only for a Body whose movement in the tick reaches its
  own thinnest width. A Kinematic body is never stopped: what it would have hit is
  carried along with it instead.
- **Probe**, widened: moving a Shape, **without turning it**, in a straight line
  and finding what it touches. The Shape is most often a circle, possibly of
  radius 0.
- **Sensor**, widened: a Sensor that moves, **whatever its Shape**, reports
  everything it touched on its way through the tick.
- **Contact**, widened: when one party is a Sensor, **or a Body was stopped
  short**, the Contact says how far through the tick the touch happened.

**Avoided:** *sweep*, *bullet*, *CCD*, and *time of impact* (which is one way of
doing it, not the behaviour). The package has no projectile concept, so *bullet*
names nothing here. In prose, **the path** is a Body's straight line from
`Previous` to `Current`, and **the path test** is the Probe of a Shape along it.

---

## What an app sees

From 577 and 578.

- **A solid Body does not tunnel.** A Dynamic body moving at least its own
  thinnest width in a tick is stopped where it first meets a Static, Kinematic
  or Dynamic Body on its path. It is not stopped short or pushed out the far
  side. There is **no opt-in** and no Component to add. An app that throws a
  boulder writes the same Components it writes today.
- **A surface a Body slides along does not stop it**, even where one tile of a
  floor meets the next. A Hit stops a Body only where its path enters the
  target; a seam is left to ordinary contact
  ([below](#a-seam-stops-nothing)).
- **The stopping Contact is an ordinary Contact.** It carries `T < 1` and
  `Depth 0`, which is the form a Probed Sensor's Contact already has. A Contact
  gains no field, and `T` keeps one meaning: how far through the tick the Body
  was when it was found.
- **The lost time is dropped.** A stopped Body sits at the Contact for the rest
  of the tick, and the solver takes it from there, in the same tick. Nothing is
  re-simulated, because re-simulating is a sub-step, and the owner dropped
  sub-steps.
- **A filter that drops the stopping Contact means no stop.** The Body stays at
  its end pose, as if nothing had been found. A one-way platform is a filter
  System, as it is today, and needs no new API.
- **A fast Kinematic body carries what it hits.** A Kinematic body is never
  stopped and never pushed. **Every** Dynamic body it meets on its path, not
  just the first, is carried along with it from the moment they meet, so a fast
  paddle hits each ball instead of passing through it. The Contact of a carry
  past the first is written by Solve, so a filter never sees it
  ([below](#the-hits-past-the-stop)).
- **Every moving Sensor is swept, whatever its Shape.** A box or segment Sensor's
  Contacts now take the circle Sensor's form (`T < 1`, `Depth 0`), and two moving
  Sensors crossing within a tick find each other.
- **`Probe` and `ProbeAll` gain a form that moves a Shape**, `ProbeWith` and
  `ProbeAllWith`, beside the circle forms, which stay as they are.
- **A world where nothing moves fast pays one compare per Body**, inside a walk
  Index already makes, and no new dispatch.

---

## The gate

From 575, 576 and 577.

**Continuous collision engages for a Body when `|Current − Previous| ≥ 1 ×` its
Shape's minimum extent.** The step is fixed, so displacement is the whole test,
and it is compared squared, without a square root.

**Minimum extent** is how thin the Shape is along its thinnest direction, from
the Body's Position:

| Shape | minimum extent |
| --- | --- |
| circle | its radius |
| segment | its rounding radius; a bare segment has 0 |
| triangle, box, quad, `Polygon` | the distance from the Position to the nearest face's line, plus the rounding radius |

**Not the bounding radius**, which lets a long plank through: a 4 m × 0.2 m plank
thrown flat has a bounding radius of 2 m and a minimum extent of 0.1 m.

**A Shape of minimum extent 0**, a point or a bare segment used as a solid Body,
**engages whenever it moves**, `|Current − Previous| > 0`. That is the swept
Sensor's own rule. Under `≥`, it would otherwise engage at rest (576's census).

**Where the extent is kept. Settled here.** The map settled that minimum extent
is a constant computed when the Shape is set. For a circle and a segment it is
`Radius`, which Index reads live. For a Polygon kind, the face distance changes
only when the vertices do, and the constructors are the only writers of
vertices (`Shape.Verts` already says not to write them directly). So:

- **the Shape stores the face distance as an unexported `float32` in its six
  padding bytes**, written by `NewPolygonShape`, `NewBoxShape` and the triangle
  and quad constructors, and read by Index with a branch on the kind it already
  switches on;
- **the Shape stays 104 B**, and every index entry keeps its size;
- **a Polygon Shape written as a bare literal has a face distance of 0**, so it
  engages on any motion. That is safe, since it is tested more often and never
  tunnels, and it is the same class of hazard as a bare literal's collision bits,
  which the package already accepts.

Computing the face distance in Index was rejected: it is one cross product and
one length for each face, four for a box, every Body, every tick, against a bar
of one compare.

**`≥ 1×`, not Box2D's and Rapier's `> ½×`.** The reference scene's speed sweep
(577) shows nothing tunnels at or below 1.25×. Tunnelling starts once the travel
exceeds the mover's extent plus the target's half-thickness: 1.5× against a
0.05 m board, 2.5× against a 0.4 m wall. `> ½×` would engage twice as many
Bodies and buy nothing, except against the two named limits below
(a zero-thickness target and a closing Kinematic target). The half-speed margin
does not close those either. Neither benchmark scene trips either threshold, the
largest ratio being 0.098×.

---

## Where each part runs

From 577 and 579.

**Five Systems, the same lock sets, no sixth System.**

| System | what it does for continuous collision | lock change |
| --- | --- | --- |
| **Integrate** | nothing | none |
| **Index** | the gate: sets the path bit on the entry, records where the path starts, and lists the entry by its path box | none; it already reads `Shape` and `Position` |
| **Detect** | one path pass, before the discrete walk, for Sensors and fast Bodies together; writes the stopping Contacts, holds every later Hit on a Body in `Contacts`' scratch, and tests a stopped Body's other pairs where it stopped | none; it already reads both indices and writes `Contacts` |
| *(the app's filter Systems)* | see the stopping Contact while the Body is still at its end pose; may drop it. They see no held Hit | none |
| **Sleep** | keeps awake a Body that a Kinematic mover's held Hit will carry, and wakes it if it sleeps | none; it already reads `Dynamic` and writes `Contacts` and `Sleeping` |
| **Solve** | moves each stopped Dynamic body back to its stopping point, carrying it by a Kinematic side's remaining movement; carries every Dynamic body a Kinematic mover's held Hits name and writes their Contacts; then solves as usual | none; it already reads `Dynamic` and writes `Position` and `Contacts` |

- **Integrate cannot hold the gate.** It never reads `Shape`, so it has no extent
  to compare against. Giving it one widens its lock set, or adds 8 B to `Position`
  or `Velocity`, which gameplay writes every tick. Both were rejected. This
  replaces the map's earlier "one compare, folded into Integrate".
- **Index already makes one branch per Body for the swept Sensor.** The gate is a
  compare beside that branch, in a walk Index already pays for.
- **A sixth System was rejected.** It costs about 5.7 µs of dispatch every tick,
  whether anything is fast or not, which breaks the "no new dispatch" bar.
- **What the app's Systems see.** A filter sees the stopping Contact with the Body
  still at its end pose. A reaction System sees the Body at its stopping point.

---

## Index: one bit and a path box

From 577, 578 and 579.

**One bit on the entry, meaning "Detect tests this entry's path".** It replaces
both the swept Sensor's `swept` bit and the `fast` bit the mechanism ticket
planned. Index sets it from the `Sensor` flag, which the entry already holds:

- **a Sensor**, on any motion (`from ≠ to`), as today, **whatever its Shape**;
- **a solid Body**, when its movement passes [the gate](#the-gate).

**Where the path starts.** The entry already has a `previousCentre` field that
only the swept Sensor writes. Index now writes the start of the path there for
every marked entry, which costs one store per marked entry and no memory:

- **a circle** moves along its centre's chord, from its centre at the previous
  pose to its centre now, as the swept Sensor does today;
- **any other Shape** moves along its Position's chord, **held at its end angle**,
  which is what the world cache already holds. Its start pose is the end pose
  translated by `Previous − Current`.

**Every marked entry is listed in the grid by its path box**, the union of its
start and end boxes, not by its end box alone. Without that, two fast Bodies
crossing at an angle are never tested (579's amendment to 578):

- ball A, radius 1, goes (−10, 0) → (10, 0);
- ball B, radius 1, goes (0, −10) → (0, 10);
- their centres would cross at the origin at `T = 0.5`; they first touch on
  the way there, each √2 short of it, at `T = (10 − √2)/20 ≈ 0.429`;
- A's path box reaches `y ∈ [−1, 1]`, and B ends at y = 10; B's reaches
  `x ∈ [−1, 1]`, and A ends at x = 10;
- listed by end box, neither walk finds the other, and they pass through.

Listing by path box costs more grid cells for the marked entries only. Overlap
and Probe queries meet more candidates around a marked entry, but narrowphase
still tests the end pose, so **no query's answer changes**. Index already reads
`Previous` and already writes the box, so no lock set widens.

**The fall-back's flag** (see [Acceptance](#acceptance)): if it is ever taken,
Index copies one more `bool` from the `Shape` onto the entry. Index already reads
`Shape`, so that is not a new read.

---

## Detect: one path pass

From 577, 578 and 579.

**One pass, before the discrete walk, over every marked entry.** It generalises
today's `sweepSensors`. For each marked entry, it runs one path test through both
indices, and **the `Sensor` flag picks the keep rule**:

- **a Sensor keeps every Hit, ordered by `T`**, excluding only itself, as today;
- **a solid Body keeps every Hit on its path** that is not a Sensor, is not
  already touched at `Previous`, and is one its path enters rather than a seam
  ([below](#a-seam-stops-nothing)). The first stops it. Every later one on a
  Body is held for Solve, which alone can tell whether the mover is stopped
  ([below](#the-hits-past-the-stop)). It also keeps every Hit on a Sensor that
  did not move, which stops nothing
  ([below](#a-fast-body-reports-the-sensors-it-crosses)).

Two passes were rejected: two loops over the entries, two copies of the path box
and relative-motion code, and the same pair possibly tested twice where a Sensor
meets a fast Body.

### What a solid Body's path test skips

- **Targets it already touches at `Previous`.** Those are left to ordinary
  detection. Only a surface the path *enters* can stop a Body, so a fast ball
  rolling along the floor is not stopped by the floor. This is Box2D's rule too.
- **Sensors, as a stop.** A Sensor never stops a Body. A resting one it crosses
  is still reported.
- **Pairs detection already rejects:** the collision bits, and pairs a Joint
  holds apart (`JointedPairs`).
- **Seams.** A target the path meets but does not enter, such as the next tile
  of a floor the Body slides along, is left to the discrete walk
  ([below](#a-seam-stops-nothing)). This is asked last, of a Hit that would
  otherwise stop the Body.

### A seam stops nothing

From [physics: a fast Body sliding over the seams of a tiled floor is not stopped by the next tile](https://github.com/dvoyni/cog/issues/588).

**A resting Body stands a Slop's depth into the surface it slides on, so its
path meets the next tile.** The "already touched at `Previous`" skip does not
cover that tile. Measured on the first build of the stop, with a floor of Static
tiles 0.2 m thick, laid edge to edge, and a mover resting 5 mm into it under
gravity:

- a ball of radius 0.2 at 1×, 2× and 5× its extent a tick stops at every first
  seam crossing, on box tiles and on a segment chain, 0.4 m and 1.6 m wide. The
  stopping normal is the next tile's leading corner, about (−0.22, 0.975). A snag
  costs up to 94% of that tick's travel and about 5% of the speed;
- a 0.4 m box does the same, and its stopping normals include (−1, 0), the next
  tile's vertical face;
- the discrete walk alone resolves every one of those meetings upwards, out of the
  tile, and never stops the Body.

**The rule.** A Hit that passes the other skips stops the Body only if **both**
hold. Otherwise it is a seam, and the pair is left to the discrete walk:

- **The Body closes on the target by at least its own minimum extent, along the
  Hit's normal.** This is the gate, taken along the normal the Body meets the
  target by: `−d · n ≥` minimum extent, where `d` is the path and `n` the Hit's
  normal, which faces the Body. A Body closing by less cannot get past the
  surface within the tick, so the discrete walk resolves it from the side it
  came from. That is the gate's own argument, and the speed sweep's evidence for
  it carries over. It is one product, so it is asked first.
- **The target reaches into the band the Body sweeps deeper than the Body
  already rests, plus the Slop.** The resting depth is the deepest overlap, at
  `Previous`, with any solid target the Body already touches (the path test's
  Hits at `T = 0`). To it is added how much further the path takes the Body into
  that surface by the end of the tick, along that surface's normal. The test is
  the Body shrunk by that depth and Probed against the one target, along its
  path lengthened by the same depth, so the band keeps its full length.
  - For a circle it is `probeWorld` with the radius less the depth.
  - For any other Shape it is the swept convex test's own Newton advance, run
    on the signed distance plus the depth. EPA gives that distance inside an
    overlap, and it stays convex along the path, so the advance still never
    steps past the depth.

A tile laid flush with the one underfoot reaches no deeper than that one does,
so it stops nothing. A wall, a board or a post across the band reaches far
deeper, and it stops the Body where its path first meets it.

**A pair tested along its relative motion keeps the rule.** Where both parties
are marked ([Which target is tested where](#which-target-is-tested-where)), the
path is the relative one, and the rule reads it in place of `d`: the gate along
the normal is the pair's closing, and the depth test Probes the shrunk Body along
the relative path, lengthened. A fast ball overtaking another that closes on it
by less than its extent in the tick is left to the discrete walk, though its own
path would close by far more. **Settled here:** the resting depth counts a marked
partner the Body already overlaps where both paths start, taken along the pair's
relative motion, as it counts any other target touched at `Previous`; a marked
target where the tick left it is not where the Body stood beside it, so it is
not counted there.

**Why each half is there, by the sequence that fails without it:**

- **Without the gate along the normal**, the depth stops a Body landing on the
  floor it slides over. A box that the discrete walk has bounced 9 mm clear of a
  segment chain comes down at a slant, at 5× its extent. It rests on nothing, so
  the depth it must beat is the Slop alone, and it lands deeper than that: the
  floor it lands on stops it, at `T = 0.66`, with the Slop or without it. The
  gate along the normal passes over the landing, which closes on the floor by
  its fall of a centimetre or two, not by its speed.
- **Without the depth**, the gate along the normal stops the ball at 5× on the
  next tile's corner. At normal (−0.22, 0.975) it closes by 0.22 m a tick against
  an extent of 0.2 m. It also stops the box at 1× on the next tile's face,
  (−1, 0).
- **Without the Slop**, the depth is exact, and a Body touching nothing is
  stopped by the next tile's face. A box bounced 1.8 mm clear of a floor of
  0.4 m box tiles, at 5× its extent, sinks 3.5 mm in the tick. Its corner meets
  the next tile's face 1.7 mm deep, at normal (−1, 0.005), and it was stopped at
  `T = 0.25`, at a tolerance of 1 µm and of 1 mm alike.
- **Without the drift into the resting surface**, a box tipped by the discrete
  walk rests 8.7 mm into one tile and settles 2.5 cm in the tick. Its corner
  digs into the next tile deeper than the Slop allows, even at 2 cm, and it is
  stopped at `T = 0.36`.
- **Without lengthening the path**, a Body that meets a target just before the
  tick ends is not stopped. The plank thrown at the thin Dynamic board, phase
  2, ends 1.7 mm into the board. The plank shrunk by the Slop never reaches it,
  so the Hit reads as a seam, and the pair is found at `T = 1` with no stop.
  That breaks the nine cases' near-side check.

**The off-centre post stops.** A post 5 cm wide whose top reaches halfway up the
mover's lower half is passed beside by the mover's centre. It is met at the
post's corner, at a normal the Body closes on by far more than its extent, and it
reaches far deeper than the Slop. So every mover stops on it, from every phase.
This was the lead candidate's weak case (see [Shapes that were rejected](#shapes-that-were-rejected)).

**What it costs, and where.**

- **It runs only on a Hit that would stop the Body**, after the other skips. It
  never runs on the nothing-fast path, and never for a Body the gate did not
  mark.
- **The resting depth is measured once per Body**, and only when some Hit
  reaches it. It costs one point query (a circle) or one GJK (anything else) per
  target touched at `Previous`.
- **Each Hit that reaches the depth test costs one more Probe**, against that
  one target.
- **No lock changes.** The Slop reaches Detect as a setting, beside the
  persistence it already takes, and nothing new is read.
- **Measured against the stop before it**, interleaved with an A/A copy of the
  baseline, on the minimums (AMD Ryzen 9 7950X3D, GOMAXPROCS 32):
  - `BenchmarkDetect` is flat: 56.5 µs against 56.6 and 56.3 at N = 1 024,
    13.60 µs against 13.54 and 13.52 at N = 256;
  - `BenchmarkThePolygonStep` is within 1%, and `BenchmarkTheStep` at N = 256
    within the A/A spread;
  - **No gap, once measured against the tree before the path pass.** Against
    the stop, `BenchmarkTheStep` at N = 1 024 read 2.6 to 3.0% slower, in a
    scene that engages no Body. A four-way interleave settles it: 12 rounds of
    the tree before the path pass (a39cc01), the stop (23b5341), an A/A copy of
    the stop, and this rule, read on the minimums. `BenchmarkTheStep` at
    N = 1 024 reads 382 656, 372 941, 375 162 and 383 831 ns: this rule is
    +0.3% on the tree before the path pass, with equal medians (385.6 and
    385.7 µs). `BenchmarkThePolygonStep` at N = 1 024 reads 1 437 326,
    1 436 465, 1 440 467 and 1 440 734 ns (+0.2%), and both at N = 256 sit
    within 0.3%. The stop's own 2.6% speedup was code placement, which this
    change gives back; continuous collision so far costs a scene with no fast
    Body nothing the benchmarks can see.

**Pinned: a graze.** A target that the Body closes on by less than its extent,
or that reaches no deeper than the Slop past the resting depth, is left to the
discrete walk, so a fast Body can pass a post that it only grazes. See
[Named limits](#named-limits).

### A fast Body reports the Sensors it crosses

**Settled at the handover, by the owner.** A Sensor that did not move this tick
is not marked, so its own walk never tests the path of a fast Body crossing it.
If the Body's path test only skipped Sensors, a fast ball could cross a resting
goal-line trigger unseen, which is the tunnelling this spec exists to stop.

- **The solid Body's path test writes every Sensor Hit along its path** as an
  entry that stops nothing: **the Sensor is A**, as the A rule already says, with
  the Hit's `T` and `Depth 0`, or `T = 0` and the overlap for one it starts
  inside.
- **The path test is a find-all Probe for an engaged solid Body**, not a
  first-Hit one: every Sensor Hit is kept, and so is the first solid Hit. The
  extra cost is paid only by engaged Bodies, and it counts against the engaged
  bar.
- **A moving Sensor meeting a fast Body** is still written by the Sensor's walk,
  along their relative motion. The Body's walk skips Sensors that are marked, so
  the pair is written once.
- **Rejected: naming it as a limit.** A trigger a fast ball can skip is the
  failure this map was drawn to close.

### The Hits past the stop

From [physics: a fast Kinematic body carries every Dynamic body on its path, not just the first](https://github.com/dvoyni/cog/issues/589).

**Keeping only the first Hit passes a Kinematic body through everything
behind it.** Detect cannot tell a Kinematic mover from a Dynamic one, since
reading `Dynamic` would widen its lock set, so both kept only their first Hit:

- balls of radius 1 rest at x = 7 and x = 3, and a Kinematic paddle of
  radius 1 goes 10 → 0;
- the ball at 7 is the first Hit, at `T = 0.1`, and is carried to −2;
- the ball at 3 is never tested, and the paddle and the carried ball pass
  straight through it;
- a Static wall as the first Hit does the same to every Dynamic body behind it.

**Detect holds every later Hit, and Solve decides by kind.** The path test is
already a find-all Probe, so the Hits are in hand:

- **Detect holds, for every engaged solid Body, each Hit past the first that
  would stop it**, the Contact it would be, made as the stopping Contact is,
  in `Contacts`' scratch and not in the list. The seam rule and the skips apply
  to each, as to the first. A meeting with a marked Body that comes sooner than
  the first Hit of the Body's own path holds that Hit too. Only Hits on the Body
  index are held: a Static past the stop is never carried.
- **The sleep System reads the held Hits** once it has built the tick's nodes,
  which are the awake Dynamic bodies. A held Hit whose mover is not one is a
  Kinematic mover's. Its target is kept awake, as one touching a Kinematic body
  is, or its Island is woken if it sleeps, as for a Contact naming it.
- **Solve drops a Dynamic mover's held Hits**, which lie past where it stopped.
  For a Kinematic mover, it carries each Dynamic target from its end pose by
  `(1 − T)·d_K`, and **writes the Hit as its Contact**, at the end of the
  current run: `T < 1`, one point, `Depth 0`. A Kinematic or Static target is
  neither carried nor written.
- **A carried pair that touched on an earlier tick** has an Ended or a cached
  entry, which Detect wrote because it did not find the pair again. That entry
  gives way to the carried Contact, which continues from it. The pair table is
  rebuilt over the result. All of this runs only on a tick with such a carry.
- **A pair the discrete walk already wrote**, the target touching the mover
  where it stopped, keeps that Contact, and is carried unless a filter dropped
  or ignored it.

**Settled here: held in scratch and written by Solve, not written and then
dropped.** A Hit past a Dynamic mover's stop is no touch. The filters run
between Detect and Solve, and neither can tell kinds, so a Contact Detect
writes for it would be seen by every filter:

- a fast Dynamic ball goes 10 → 0 over resting balls at 7 and 3;
- written and marked for Solve to drop, the Contact with the ball at 3 is in
  the list a filter reads, though the mover stopped at 9;
- a filter that scores on touch scores a ball that was never reached.

Solve cannot delete a Contact once a filter has seen it, since a reacting
System would then see a pair begin and vanish. So the held Hits stay out of the
list until Solve knows what they are. **The price is that a filter never sees a
carry past the first, and cannot drop it**; a reacting System sees it as an
ordinary Contact. The first Hit is still written by Detect, and a filter may
drop it. That is named [below](#named-limits).

**Where it costs.** Nothing on the nothing-fast path. An engaged solid Body runs
the skips and the seam rule on every Hit on the Body index, not only up to the
first. Solve and the sleep System each test for an empty run once a tick.

### Which target is tested where

- **A target that is not marked is taken at its end pose, whatever its kind.**
  - **Named limit, the ghost Hit:** a target that moved into the path during the
    tick counts as already there. The error is at most the target's own movement
    in the tick, which is below its own extent, or it would be marked.
- **A pair where both parties are marked meets along their relative motion,**
  `(Current_A − Previous_A) − (Current_B − Previous_B)`, starting from both start
  poses, with **one shared `T`**. That covers two fast Bodies, two moving
  Sensors, and a Sensor meeting a fast Body:
  - two balls of radius 1 meet head-on, A going 0 → 10, B going 10 → 0. At end
    poses each already touches the other at its own `Previous`, so the "already
    touching" rule skips both, the discrete walk sees them apart, and they swap
    places. Along their relative motion they meet at `T = 0.4`, A at 4 and B at 6;
  - two moving Sensors crossing within a tick are one entry with one `T`, so
    `sensorHit`'s re-Probe of the other Sensor and its tie-break go away.

**Why every fast Body tests the whole Body index (578).** An index entry holds a
Shape, its world cache and its transform. Index's Body walk reads `Shape` and
`Position` and never `Dynamic`, so Detect cannot tell a Kinematic target from a
Dynamic one. Fencing off Dynamic targets behind an opt-in, as both engines do,
needs that read, and it would widen Index's lock set. The engines' own reason
does not apply here: Box2D runs its continuous pass in parallel while Bodies are
finalised, so a bullet against Dynamic Bodies must wait for a later serial pass.
In cp's order, every end pose is in `BodyIndex` before Detect starts, so a
Dynamic target costs the same as a Kinematic one.

### Which walk writes a pair, and which party is A

- **A moving Sensor meeting a solid Body** is written by the Sensor's walk. The
  solid Body's walk skips marked Sensors.
- **A resting Sensor crossed by a fast Body** is written by the Body's walk.
- **Two marked parties of the same sort**, two Sensors or two solid Bodies, are
  written by the lower Entity's walk, which is the pair table's own tie-break.
- **A marked party meeting one that is not** is written by the marked party's
  walk.
- **Which party is A** follows [`ecsphysics2d.md`'s rule](ecsphysics2d.md#which-party-is-a-and-one-normal),
  unchanged: the Sensor, otherwise the party that is not Static, otherwise the
  lower Entity. Which walk writes a pair and which party is A are separate
  questions.

**Settled here: how two fast solid Bodies' walks find and settle their pair.**

- **Each finds the other in the grid**, in the cells of its own path box, which
  are where it is listed: two paths that meet share a cell. Nothing else scans
  for a partner, and a Body with no marked partner pays one scan of its own
  cells.
- **The lower Entity's walk judges the pair**: the Probe along the relative
  path, the skips, and the seam rule with that Body's extent and resting depth.
  Taking one side's extent is safe: getting through each other takes closing by
  both parties' thickness, and either extent is less than that.
- **A pair that would stop is kept whatever its judge's own first Hit**, since
  it may be the other party's earliest. Each Body's earliest stop is settled
  only once every walk has run, and a pair is written when it is the earliest
  for either party, once, the Contact shared by every party it stops.
- **The Hit is taken against the other party where the tick left it**, which is
  the relative path shifted by that party's whole path; its point is moved back
  along that party's path to where the two met, so `r1` and `r2` are taken at
  both stopping poses.

**Ordering.** The path pass runs first, so its entries sit at the front of the
list. The spec's ordering line becomes **"A Sensor's entries, where it is A, sit
together, ordered by `T`"**. Where two Sensors meet, the entry sits in one of the
two runs only, which is already what happens today when one side wins.

### The stopping Contact

- **One Contact per stopped pair, `T < 1`, `Sensor` false.**
- **One Contact point**, the Hit's, with **`Depth 0`**. **Settled here:** a
  face-to-face landing gets its second point next tick from ordinary detection.
  Computing a full manifold at `T` would be a second narrowphase per stop.
- **`r1` and `r2` are taken at the stopping poses,** which Detect knows, because
  Solve moves the Body there before `PreStep` reads them.
- **The discrete walk leaves the stopped pair alone**, as it already does for a
  swept Sensor's pairs.
- **Phase, warm starting and the ignore mark** come from the same previous-tick
  lookup the swept Sensor already uses (`carry`), so a stopped pair begins,
  continues and ends like any other.

### A stopped Body's other pairs are tested where it stopped

When a solid Body is stopped, **its other discrete pairs are tested at the
stopping point, not at its end pose.** Detect places a copy of the Body at `T`,
in scratch memory `Contacts` owns. `BodyIndex` is only read. The cost is paid
only on ticks where something stops, and the scratch joins the solver-scratch
area of the shrink command.

Those Contacts report **the same `T`** and **their own `Depth`**, so `T` reads
"where along its path the Body was when this was found", which is the swept
Sensor's meaning carried over.

---

## Solve: back to `T`

From 577 and 578.

**Before anything else, Solve moves each stopped Body to where its pair stood at
`T`.** Only Solve reads `Dynamic`, so only Solve decides which side moves:

- **a Dynamic side** goes to its stopping point, `Previous + T·d_self`;
- **a side that is not Dynamic keeps its end pose**;
- **a Dynamic side meeting a side that is not Dynamic** is also carried by
  whatever movement that side still has after `T`:
  `Previous + T·d_self + (1 − T)·d_other`.

Then it solves the stopping Contact with the ordinary impulse solver. `Depth 0`
gives no position bias, restitution follows cp's `bounce`, and velocity is left
alone until the impulse acts on it. The Angle is not moved back, because the path
test held the Shape at its end angle.

Examples:

- **Dynamic ball D, x 0 → 10, against Kinematic paddle K, x 10 → 0.** They meet
  at `T = 0.4`. D becomes `4 + 0.6·(−10) = −2`, touching K at 0 from the side it
  came from. Without the carry D would be left at 4, on K's far side, with the
  normal still facing the old way.
- **A slow Dynamic target hit by a fast Kinematic Body** is carried from its end
  pose by `(1 − T)·d_K`. The paddle hits the ball instead of passing through it.
- **A Kinematic Body against a Static one** moves neither side. The Contact is
  only reported.

**Settled here: what `d_other` is, and how Solve tells the cases apart.**

- **`d_other` is the other side's path as the path pass took it.** A marked
  side met along the pair's relative motion stood at `Previous + T·d_other` when
  they met, so a Dynamic body it stops is carried by the `(1 − T)·d_other` it
  had left. A target that is not marked was taken where the tick left it, so the
  Hit already has its whole movement in it: a Dynamic body stopped by a slow
  Kinematic target is moved back to `T` and not carried, which would open a gap
  of `(1 − T)·d_other` or push it into the target.
- **A Dynamic body a fast Kinematic body meets on the Kinematic body's own path
  is carried from its end pose.** It was not marked, so it has no stop of its
  own; were it marked, the two would have met along their relative motion. A
  Dynamic body two Kinematic bodies meet in the one tick is carried by each.
- **Solve reads the stopping Contact's other party and one flag on the stop**,
  set by Detect: whether the stop was a meeting. With `Dynamic`, which it
  already reads, that is enough to pick the case, and the other side's path is
  its `Position`, which it already writes. No lock set changes.
- **`r1` and `r2` need no change.** Detect takes them at both stopping poses,
  and a carry moves the Dynamic side and the point they touch at together.

**A Kinematic body carries every Dynamic body on its path** (from 589). Each
Hit of its own path past the first on a Dynamic body is carried as the first
is, from the body's end pose by `(1 − T)·d_K`, and Solve writes its Contact
([The Hits past the stop](#the-hits-past-the-stop)). A body two Kinematic
bodies meet in the one tick is carried by both, the two carries added.

**Each Body stops at its own earliest `T`**, which is "first Hit only" applied
one Body at a time. If B's first Hit is C at `T = 0.2` and the A–B pair meets at
`T = 0.4`, B stops at 0.2, and A stops at 0.4 against where B would have been.
Testing A again against B's actual stopping point was rejected: it is a second
round of path tests, it becomes a chain as soon as a third Body is involved, and
it still cannot know B's kind.

**A dropped or ignored stopping Contact stops neither side.** Solve moves only
for kept Contacts with `T < 1` that are not Sensors.

---

## Every moving Sensor is swept

From 579. This changes [`ecsphysics2d.md` § Sensors](ecsphysics2d.md#sensors-and-what-keeps-a-point-from-tunnelling).

**Box and segment Sensors are swept with the same path test as solid Bodies**:
the convex Shape moved in a straight line at its end angle. The hole the code
comment calls accepted, "a fast one can miss", closes. It was accepted only
because no swept test existed for those Shapes.

**What an app sees change:** a moving box or segment Sensor's entries take the
Probe's form: `T < 1` with `Depth 0`, or `T = 0` with the overlap for one that
starts inside something. Today they carry `T = 1` with the overlap's depth.
Circle Sensors already use this form, so every Sensor's entries now mean one
thing. The chord limit, which ignores turning, is the one solid Bodies have.

**No gate for a Sensor.** A Sensor is swept whenever it moves, as today:

- **Taken: no gate.** A solid Body can fall back to discrete detection because
  the solver picks it up from there next tick. A Sensor only reports, and falling
  back would lose two things:
  - a graze partway through the tick, where a small target is within reach of
    the path's middle but not of either end;
  - the promise that a Sensor's entries sit together, ordered by `T`. A slow
    Sensor's entries would carry `T = 1` among the discrete pairs, and "read the
    first entry, snap back to `Previous`" would mean two things.
- **Rejected: the gate for every Sensor.** It saves the Probe on slow Sensors,
  and nothing shows those are common: a pressure plate is Static and never
  Probed, and a projectile is fast.
- **Rejected: the gate for box and segment Sensors only.** It splits the rule by
  Shape.

**Of the three holes the code names as accepted,** "box and segment Sensors can
miss" and "two moving Sensors can miss each other" close. The chord limit stays,
and solid Bodies now share it.

---

## Probing a Shape

From 577, and the handover (the `…With` names are **Settled here**, approved by
the owner).

**The path test for a Shape that is not a circle** is new: a convex Shape moved
in a straight line **at a fixed angle**, finding the first touch, built on
`gjk.go`'s distance routines. A circle keeps `probeWorld`, which is exact against
every Shape kind. Turning is out of scope, so a thin Shape spinning fast is
still a limit.

**It is public**, because Probing a box is the same question a game asks when it
wants to know whether a crate fits through a gap. The circle forms stay as they
are: a circle is the common case, and a point is the case the package exists
for. The new forms sit beside them and mirror `Overlap`'s way of taking a Shape:

| World query, on `StaticIndex` and `BodyIndex` | Returns |
| --- | --- |
| `ProbeWith(shape Shape, from, to m.Vec2d, angle float64, verts []m.Vec2d, bits, collidesWith uint32, exclude ecs.Entity)` | `(Hit, bool)`, the first Hit |
| `ProbeAllWith(dst []Hit, shape Shape, from, to m.Vec2d, angle float64, verts []m.Vec2d, bits, collidesWith uint32, exclude ecs.Entity)` | `[]Hit`, every Hit ordered by `T`, appended |

| Pair primitive | Returns |
| --- | --- |
| `ProbeShapeWith(mover Shape, from, to m.Vec2d, angle float64, moverVerts []m.Vec2d, target Shape, at m.Vec2d, targetAngle float64, targetVerts []m.Vec2d)` | `(Hit, bool)`, `Hit.Entity` zero |

- **`from` and `to` are the mover's Position** at each end, and `angle` is held
  throughout.
- **`Hit` means what it means today**: `T` a fraction of `from → to`; `Point` on
  the target's surface; `Normal` a unit vector facing the mover; a Probe that
  starts overlapping reports `T = 0`; a path of zero length behaves as an Overlap
  that reports a `Hit`.
- **Nothing allocates**, as for every query and primitive (`AllocsPerRun`).
- **Widening `Probe` itself to take a Shape was rejected.** It changes every
  existing call site, and the circle case would pay for a parameter it never
  uses.

**The glossary's Probe** already reads "moving a Shape, without turning it", with
*shape cast* kept on the Avoid list.

---

## Named limits

From 577, 578, 579, 580 and 588. Each is a behaviour a game can see. The four
marked **pinned** have a test that holds today's behaviour, so any change to it
is deliberate. The others are written down only: each fixes itself within one tick,
or is out of scope.

| limit | what happens | |
| --- | --- | --- |
| **A zero-thickness target** | a bare segment: tunnelling starts just past 1×, where the gate has only just engaged a mover that can still pass it at an angle | **pinned** |
| **A Kinematic target closing on the Body** | each gate sees its own Body's motion, not the pair's, so a Body and a Kinematic target each under its own gate, closing on each other faster than the gates allow together, are caught only by the discrete walk | **pinned** |
| **The target behind a dropped stop** | a one-way platform: a Dynamic body passes anything behind a dropped target in the same tick. A Kinematic body still carries the Dynamic bodies behind it | **pinned** |
| **A carry past the first, to a filter** | the Contact of a Dynamic body a Kinematic body carries past its first Hit is written by Solve, so a filter never sees it and cannot drop it; a reacting System does | written down |
| **A graze** | a target the Body closes on, along the Hit's normal, by less than its extent, or that reaches into its band no deeper than the Slop past the depth it already rests at, is a seam to the path pass: a fast ball passes a post that reaches 3 mm into its path, and a Body sunk deep in something at `Previous` passes anything that reaches no deeper | **pinned** |
| **A seam met while landing** | a Body touching nothing at `Previous` has only the Slop to beat, so one coming down onto a tiled floor more than the Slop deeper in the tick it crosses a seam is stopped by the next tile's face, and loses the rest of that tick's travel | written down |
| **Rotational tunnelling** | the path is a chord at the end angle, so a thin Shape spinning fast can slip through | out of scope |
| **A dropped stop's other Contacts** | tested at the stopping point, they describe that pose for one tick | written down |
| **The ghost Hit** | a target that moved into the path during the tick counts as already there | written down |
| **Stale Contacts beside a Kinematic side** | Detect cannot tell kinds, so it tests a stopped Body's other pairs as if both sides move back to `T`; where one is Kinematic, the Dynamic side's other Contacts describe the wrong pose for one tick, and so do those of a Dynamic body it carries, found where the tick left it | written down |
| **The one-tick gap** | A stopped against where B would have been can stand short of B, and the gap closes next tick through ordinary contact | written down |
| **The chord** | a sharply curving Body or Sensor can clip a corner within one tick | written down (already in `ecsphysics2d.md`) |

---

## What changes in `ecsphysics2d.md`

Made by the ticket that lands the behaviour it describes, not before.

- **§ Sensors, and what keeps a point from tunnelling:** every moving Sensor is
  swept whatever its Shape; the "box and segment Sensors … can miss" and "two
  moving Sensors … can miss each other" bullets go; the section points here.
- **§ Ordering, and memory:** "A Sensor's entries, where it is A, sit together,
  ordered by `T`". The scratch for a stopped Body's copy at `T` joins the solver
  scratch area.
- **§ `Hit`, and what a Probe means / § The queries and the indices:** the
  `…With` rows, and "moving a Shape without turning it" for the Probe.
- **§ The five Systems:** a line under the lock table saying continuous collision
  changed none of it, pointing here.
- **§ The Shape:** the face distance in the padding bytes, and the bare-literal
  hazard.
- **§ Out of scope:** "Continuous collision for the whole world, and Probing a
  box" is removed and points here. If the kill criterion trips and the owner
  records "not adopted", it is replaced by that record instead.
- **§ Vocabulary:** Probe, Sensor and Contact as widened, and **Continuous
  collision** added.

---

## Shapes that were rejected

| rejected | why |
| --- | --- |
| Speculative Contacts, even behind the gate | they need next step's motion, and only `Velocity` holds it, which neither Index nor Detect reads; last step's displacement misses the first fast tick, since a Body just launched had `Previous = Current`; a Contact with a gap gives Contacts a second meaning and stops Bodies at corners they would have missed |
| Rapier's soft CCD | speculative Contacts over a per-Body reach, with no gate, paid every tick |
| Box2D v2.4's re-solve to the time of impact | serial, ungated, and a sub-step, which the owner dropped |
| The gate in Integrate | Integrate reads no `Shape`; reading it widens its lock set, and carrying the extent in `Position` or `Velocity` adds 8 B to a Component gameplay writes every tick |
| A sixth System | about 5.7 µs of dispatch every tick, fast or not |
| `> ½×` minimum extent | engages twice as many Bodies; the scene shows nothing tunnels at or below 1.25× |
| Computing the face distance in Index | one cross product and one length per face, per Body, per tick |
| An opt-in Component for Dynamic targets | Detect cannot see a Body's kind; Index reading `Dynamic` widens its lock set |
| The static index by default, the Body index behind an opt-in | puts Kinematic targets behind the opt-in, reversing a settled point; **kept as the fall-back** |
| Testing a Body again against another's actual stopping point | a second round of path tests, a chain with a third Body, and still blind to kind |
| Writing the Hits past a stop in Detect, for Solve to drop a Dynamic mover's | every filter sees a Contact past a Dynamic mover's stop, a touch that never happened, and Solve cannot take it back once seen |
| Detect or Index telling kinds, to keep a Kinematic mover's Hits only | reading `Dynamic`, even as a filter, widens the lock set |
| Naming the angled crossing as a limit | "two fast Bodies meet along their relative motion" would hold only head-on |
| Two path passes, one for Sensors and one for solid Bodies | two loops, two copies of the path-box and relative-motion code, the same pair tested twice |
| A gate for Sensors, or for box and segment Sensors only | loses the graze partway through a tick and the one meaning of a Sensor's entries; or splits the rule by Shape |
| Widening `Probe` to take a Shape | changes every call site; the circle case pays for a parameter it never uses |
| A full manifold at `T` for the stopping Contact | a second narrowphase per stop, for a point ordinary detection gives next tick |
| Stop only if the Body's centre path enters the target (Box2D v3's rule, generalised; the lead candidate) | the centre can stop short of a target the Body meets: the circle and the box thrown at the 0.4 m Static wall, phases 3 and 4, end with their centres short of the wall, so nothing stops them, and the discrete walk finds each pair 0.17 m and 0.08 m deep at `T = 1`, which fails the nine cases' near-side check (the plank too, phase 3). A post the centre passes beside is never stopped: the three movers pass it without meeting it in 3 of 8 phases (the plank in 5), and in the rest meet it only in the discrete walk, at `T = 1`. The ball rolling into a wall's rounded lower end meets it with its edge, not its centre, and is not stopped |
| The depth alone, a path reaching deeper than the resting depth | a Body touching nothing has nothing to rest on: a box the discrete walk bounced 1.8 mm clear of 0.4 m box tiles, at 5×, sinks 3.5 mm in the tick and meets the next tile's face 1.7 mm deep, and is stopped there at `T = 0.25`; a box landing at a slant on a segment chain at 5× is stopped by the floor it lands on, at `T = 0.66`, with the Slop added or not |
| The gate along the Hit's normal alone | the next tile's corner meets the ball at 5× at normal (−0.22, 0.975), which it closes on by 0.22 m against an extent of 0.2 m, and the next tile's face meets the box at 1× at (−1, 0): both stopped at every seam |
| The resting depth without the drift into the resting surface | a box tipped by the discrete walk rests 8.7 mm into a tile and settles 2.5 cm in the tick, and its corner digs into the next tile deeper than the Slop allows: stopped at `T = 0.36`, and still with a tolerance of 2 cm |
| The depth test along the path as it is, not lengthened | a Body that meets a target just before the tick ends reaches it by less than the Slop: the plank at the thin Dynamic board, phase 2, ends 1.7 mm into it, so the Hit reads as a seam and the pair is found at `T = 1` with no stop, which fails the near-side check |
| A tolerance of a fixed share of the extent (Box2D's quarter of the minimum extent, for chains) | the depth a Body rests at is the Slop, in metres, not a share of its size: a 1 cm marble has a tolerance of 2.5 mm, rests 5 mm into the floor, and would snag every seam. Not built; the sequence is enough |
| A test of the end pose, stopping where the discrete walk would push the Body forward | a ball at 5× crossing a whole 0.4 m tile can end with its centre just past the tile's far end, overlapping its far corner by the depth it rests at; the push out of that corner leans forward, so it would read as pushed through and stop at the seam. Not built; the sequence is enough |
| A prototype before handing over | a price needs most of the build, so it would be built twice, and the only cost every world pays is already priced at the noise floor |

---

## Acceptance

From 576 and 580. **This is what the build must show before continuous collision
counts as done**, and each implementation ticket cites the part it meets.

### Where it is tested

Three places, all of them existing ones (approved at the handover):

1. **The plugin's own step, through the kernel test harness** that
   `TestASolidBodyDoesNotTunnel` already uses: spawn Bodies, publish ticks, read
   `Position` and `Contacts`. Every behaviour test is here.
2. **The public `…With` queries**, tested directly as `ProbeShape` is today.
3. **Benchmarks**, read with the interleaved A/B driver `ab-step.sh`.

Nothing is tested through Index's or Detect's internals.

### The pass bar

- **All nine cases of `TestASolidBodyDoesNotTunnel` are green**: 0 of 8 starting
  points tunnel, neither clean nor pushed through. The movers are a circle and a
  box 0.4 m across and a 4 m × 0.2 m plank, thrown at 40.4 m/s at a 0.4 m Static
  wall, the same wall as a Kinematic body, and a thin Dynamic board 0.1 × 4 m.
  Its skip is removed by the ticket that turns it green.
- **8 starting points stay.** At 40.4 m/s they are 8.4 cm apart, finer than any
  Shape's extent.
- **A new check: the mover ends on the near side, touching the target.** This
  catches a Body stopped at `T` whose Contact is then dropped by mistake.

### New permanent cases

Each earlier decision's failing sequence becomes a case, thrown from 8 starting
points, so none of those decisions can be undone silently:

1. two fast balls head-on: A from x = 0 to 10, B from 10 to 0, meeting at
   `T = 0.4`;
2. two fast balls crossing at right angles, on their way to the origin, first
   touching at `T = (10 − √2)/20 ≈ 0.429` (the path box);
3. a fast Kinematic paddle against a Dynamic ball, fast and slow: the ball is
   carried along with the paddle, not passed through;
4. a fast box Sensor and a fast segment Sensor crossing a thin target: each
   reports it once, with `T < 1` and `Depth 0`;
5. two moving Sensors crossing: one entry, one `T`;
6. a fast solid Body crossing a box Sensor that did not move: the Sensor is
   reported once, with `T < 1` and `Depth 0`, and the Body is not stopped;
7. the seam scene (588): the ball and the box, on box tiles and on a segment
   chain, 0.4 m and 1.6 m wide, at 1×, 2× and 5× their extent, from 4 phases for
   60 ticks under gravity: no tile Contact has `T < 1`, and no tick falls short
   of its travel because of a stop;
8. the off-centre post (588): each mover thrown over a post 5 cm wide whose top
   reaches halfway up its lower half is stopped where it first meets it, from
   every phase.
9. a fast Kinematic paddle over balls resting at x = 7 and x = 3, and over a
   Static wall and then a ball (589): every ball is carried, circles and boxes,
   both spawn orders; a fast Dynamic ball over the same two balls stops at the
   first, and no Contact names the second.

### The speed sweep

**A table-driven test.** For every mover against every target, nothing tunnels
at travel of 0.5, 1, 1.25, 1.5, 2, 2.5, 3, 4 and 5 times the mover's minimum
extent. It pins the `≥ 1×` gate from both sides: below 1× the discrete walk must
hold, and at or above it the path test must.

### The pinned limits

A test each for [the four pinned limits](#named-limits): a zero-thickness target
just under the gate, a closing Kinematic target, the target hidden behind a
dropped stop, and a graze.

### The cost bar

All read on the minimums, with interleaved A/B over two built binaries.

- **A world where nothing moves fast:**
  - `BenchmarkTheStep` and `BenchmarkThePolygonStep`, at N = 256 and N = 1 024,
    show **no regression above the noise floor**, about 1% and 0.2% (576);
  - **a new Index microbenchmark**, the Body index rebuild through the moving
    insert with `Previous` poses, before and after, holds **≤ 2 ns per Body**.
    The whole-step benchmark can show the compare is invisible, but it cannot
    put a number on it. The microbenchmark covers the gate compare and the branch
    that picks the path box or the end box.
- **Each engaged Body:** **≤ 450 ns with no Hit and ≤ 1 µs with a Hit**, within
  2× of Box2D in Go (+214 ns and +510 ns, against Static geometry only; 575). It
  is measured on a new benchmark scene of N fast Bodies over the
  `BenchmarkTheStep` world, divided by N. The price includes the query of the
  whole Body index and the listing by path box.
- **A moving box or segment Sensor** is measured against the same bar, compared
  with today's discrete test for it.
- **Every new path allocates nothing**: the step stays on the engine's allocation
  line, and `AllocsPerRun` is 0 on the `…With` queries.

### The kill criterion

- **Only a failure of the nothing-fast bar can end this at "not adopted"**, and
  only a failure the gate ticket cannot fix inside Index: a regression above the
  noise floor, or more than 2 ns per Body.
- **When it trips, the build stops and asks the owner.** The owner decides
  whether "not adopted" is recorded in `ecsphysics2d.md` § Out of scope, with the
  numbers and the reason, next to the line that already rules continuous
  collision out. If it is, this spec is withdrawn and the map closes. **Nothing
  is recorded without the owner's word.**
- **A failing pass bar is a bug, not a verdict.** The nine cases fail today by
  design.
- **An engaged-Body price over the bar does not kill.** Only Bodies that would
  otherwise tunnel pay it, while every world pays the nothing-fast cost.

### The fall-back for a solid Body

- **What triggers it:** the part of the no-Hit price that comes from querying
  the Body index, measured as the price with the Body index minus the price with
  the static index alone. If that part takes the total over 450 ns, the fall-back
  is taken.
- **Its form: an exported `bool` on `Shape`, `StopsAtBodies`**, in the padding
  bytes beside `Sensor`. **Settled here**: the name reads as what it does for
  the Shape, and it is the name if the fall-back is taken. It adds no memory and
  no read, and widens no lock set, because Index already reads `Shape`. Index
  copies it onto the entry, and Detect adds the Body index only for an entry
  that carries it. The padding still holds the face distance: layout is `Kind`,
  `Sensor`, `StopsAtBodies`, one spare byte, then the `float32`.
- **What changes when it is taken:**
  - the pass bar becomes: Static cases green always; Kinematic and thin-Dynamic
    cases green with the flag set, plus a pinned test showing they tunnel
    without it;
  - the glossary's **Continuous collision** changes to match;
  - this spec's "no opt-in" lines change with it.
- **If the static-index price alone is over the bar, nothing changes.** It is
  recorded as the price, since only a fast Body pays it.

### A swept Sensor over the bar

**Recorded only, and the Sensor stays swept.** The reasons for having no Sensor
gate (the one meaning of a Sensor's entries, the graze partway through a tick)
hold whatever the price, and the main use of a moving non-circle Sensor is a fast
projectile, which a gate would sweep anyway.

---

## Required work

In blocking order. **The gate goes first, because it carries the kill check.**

**The gate and the nothing-fast bar**

- [ ] The minimum extent: the face distance in the Shape's padding, written by
      the Polygon constructors; circle and segment read `Radius`.
- [ ] Index sets the path bit for a solid Body past the gate, beside today's
      Sensor branch, and records the start of its path.
- [ ] The Index microbenchmark, and the A/B run of both whole-step benchmarks.
      **If the bar fails and Index cannot fix it, stop and ask the owner.**

**The path test**

- [ ] The swept convex test, a Shape moved at a fixed angle, on `gjk.go`.
- [ ] `ProbeWith`, `ProbeAllWith` and `ProbeShapeWith`, their tests and
      `AllocsPerRun`.

**Stopping a solid Body**

- [ ] Detect's one path pass: the solid keep rule, the skips, the stopping
      Contact, and the discrete walk leaving the stopped pair alone.
- [ ] Solve moving a stopped Dynamic body back to `T`; each Body at its own
      earliest `T`; a dropped stop stops nothing. The nine cases go green, with
      the near-side check.
- [ ] A stopped Body's other pairs tested at `T`, in `Contacts` scratch.

**Moving targets**

- [ ] Listing marked entries by their path box.
- [ ] Relative motion for a pair where both parties are marked, and which walk
      writes it.
- [ ] The Kinematic carry in Solve.
- [ ] The five new permanent cases, the speed sweep and the three pinned limits.

**Sensors**

- [ ] Box and segment Sensors on the path pass; `sensorHit`'s re-Probe and
      tie-break removed; a fast Body reporting the resting Sensors it crosses;
      the Sensor cases.

**The price**

- [ ] The engaged-Body benchmark scene, the no-Hit and Hit prices, the
      Body-index share, and the moving-Sensor price. Take the fall-back if it
      triggers.

**Housekeeping**

- [ ] The edits to `ecsphysics2d.md` listed [above](#what-changes-in-ecsphysics2dmd).
- [ ] The code comment in `contacts-sensors.go` naming three accepted holes
      shrinks to one.

---

## Out of scope

- **Rotational tunnelling.** The path is a chord at the end angle, as the swept
  Sensor's already is.
- **Re-simulating the lost time.** That is sub-steps, dropped by the owner.
- **A projectile concept.** A projectile is still an ordinary Body; nothing here
  names one.
- **An opt-in**, unless [the fall-back](#the-fall-back-for-a-solid-body) is
  taken.
