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
  stopped and never pushed. A Dynamic body it meets on its path is carried along
  with it from the moment they meet, so a fast paddle hits the ball instead of
  passing through it.
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
| **Detect** | one path pass, before the discrete walk, for Sensors and fast Bodies together; writes the stopping Contacts, and tests a stopped Body's other pairs where it stopped | none; it already reads both indices and writes `Contacts` |
| *(the app's filter Systems)* | see the stopping Contact while the Body is still at its end pose; may drop it | none |
| **Sleep** | nothing | none |
| **Solve** | moves each stopped Dynamic body back to its stopping point, carrying it by a Kinematic side's remaining movement, then solves as usual | none; it already reads `Dynamic` and writes `Position` |

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
- they meet at the origin at `T = 0.5`;
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
- **a solid Body keeps its first Hit** that is not a Sensor and is not already
  touched at `Previous`, which stops it, and every Hit on a Sensor that did not
  move, which does not ([below](#a-fast-body-reports-the-sensors-it-crosses)).

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

From 577, 578, 579 and 580. Each is a behaviour a game can see. The three marked
**pinned** have a test that holds today's behaviour, so any change to it is
deliberate. The others are written down only: each fixes itself within one tick,
or is out of scope.

| limit | what happens | |
| --- | --- | --- |
| **A zero-thickness target** | a bare segment: tunnelling starts just past 1×, where the gate has only just engaged a mover that can still pass it at an angle | **pinned** |
| **A Kinematic target closing on the Body** | each gate sees its own Body's motion, not the pair's, so a Body and a Kinematic target each under its own gate, closing on each other faster than the gates allow together, are caught only by the discrete walk | **pinned** |
| **The target behind a dropped stop** | a one-way platform: anything behind a dropped target in the same tick is not tested | **pinned** |
| **Rotational tunnelling** | the path is a chord at the end angle, so a thin Shape spinning fast can slip through | out of scope |
| **A dropped stop's other Contacts** | tested at the stopping point, they describe that pose for one tick | written down |
| **The ghost Hit** | a target that moved into the path during the tick counts as already there | written down |
| **Stale Contacts beside a Kinematic side** | Detect cannot tell kinds, so it tests a stopped Body's other pairs as if both sides move back to `T`; where one is Kinematic, the Dynamic side's other Contacts describe the wrong pose for one tick | written down |
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
| Naming the angled crossing as a limit | "two fast Bodies meet along their relative motion" would hold only head-on |
| Two path passes, one for Sensors and one for solid Bodies | two loops, two copies of the path-box and relative-motion code, the same pair tested twice |
| A gate for Sensors, or for box and segment Sensors only | loses the graze partway through a tick and the one meaning of a Sensor's entries; or splits the rule by Shape |
| Widening `Probe` to take a Shape | changes every call site; the circle case pays for a parameter it never uses |
| A full manifold at `T` for the stopping Contact | a second narrowphase per stop, for a point ordinary detection gives next tick |
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
2. two fast balls crossing at right angles, meeting at the origin at `T = 0.5`
   (the path box);
3. a fast Kinematic paddle against a Dynamic ball, fast and slow: the ball is
   carried along with the paddle, not passed through;
4. a fast box Sensor and a fast segment Sensor crossing a thin target: each
   reports it once, with `T < 1` and `Depth 0`;
5. two moving Sensors crossing: one entry, one `T`;
6. a fast solid Body crossing a box Sensor that did not move: the Sensor is
   reported once, with `T < 1` and `Depth 0`, and the Body is not stopped.

### The speed sweep

**A table-driven test.** For every mover against every target, nothing tunnels
at travel of 0.5, 1, 1.25, 1.5, 2, 2.5, 3, 4 and 5 times the mover's minimum
extent. It pins the `≥ 1×` gate from both sides: below 1× the discrete walk must
hold, and at or above it the path test must.

### The pinned limits

A test each for [the three pinned limits](#named-limits): a zero-thickness target
just under the gate, a closing Kinematic target, and the target hidden behind a
dropped stop.

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
