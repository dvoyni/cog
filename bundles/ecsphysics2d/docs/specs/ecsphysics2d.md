# cog ecsphysics2d — specification

`github.com/dvoyni/cog/bundles/ecsphysics2d` is a plugin that gives an app 2D
rigid-body physics on a plane: Bodies that move and turn, Shapes that are
circles, segments or convex Polygons, an impulse solver with Friction and
Restitution, ten kinds of Joint, a Contact list with begin/continue/end, and
spatial queries that allocate nothing. This document specifies the whole of it.

**It is a port of Chipmunk, not a design of our own.** cp's algorithms are
transliterated from `github.com/jakecoffman/cp/v2` **v2.4.0** and checked against
Chipmunk2D's C; what changes is the *data layout*, from cp's pointer graph to
cog's Components, Resources and Systems. Like cp, it computes in **float64**.

The design is bound by four requirements, in this order, and every decision
below was taken against them:

1. **Probes are load-bearing.** A Probe of radius 0 is the common case, not the
   degenerate one, because a discrete Shape test never registers a point at any
   tick rate. A design that cannot say *"probe from A to B with this radius and
   report the first Hit"* is unusable whatever else it offers.
2. **The simulation is 2D on a plane.** What structurally stops verticality
   creeping in is that there is no third component in the contract's vectors to
   put it in.
3. **cp's behaviour is the target, and a departure from cp is a finding** —
   permitted, but stated with its reason. Only four reasons are acceptable: the
   ECS layout in place of a pointer graph, zero allocations, per-second units,
   or a defect in cp.
4. **Zero heap allocation on the hot path**, measured rather than asserted.

Two properties fall out of taking those in that order. **A Component is the
source of truth and nothing is mirrored** — there is no world object holding
state that Components shadow, so gameplay's per-tick writes are plain Component
writes rather than calls through a body handle. And **a body's kind is said by
which Components it has**, not by a flag or a sentinel mass, which is what
replaces cp's exact comparison against `INFINITY`.

This document is the specification the implementation is judged against. It is
assembled from the eighteen resolved tickets of [Physics plugin: a smart ECS
port of Chipmunk](https://github.com/dvoyni/cog/issues/182); every section cites
the tickets it came from. Where a claim rests on something unverified it is
marked **Gap** and says what would settle it; where assembling these decisions
next to each other settled something no ticket did, it is marked **Settled
here**.

**Nothing of this is implemented.** cog has no physics today — not a stub,
nothing in the tree — so [Required work](#required-work) is the whole build, not
a list of what is left.

**The package names nothing of the driving game.** [nox](https://github.com/dvoyni/nox)
is the named reference workload and supplies requirements, not vocabulary: no
game terms, no recovered constants, no content assumptions. Where a measurement
below comes from nox's scene it is named as a workload, never as a rule.

### How cp is cited

**Every citation names its version.** The default is
`github.com/jakecoffman/cp/v2` **v2.4.0**; a citation marked **C** is
Chipmunk2D at `f2f3d66`. The prose names the *symbol*, and
[Ported symbols](#ported-symbols) holds the `file:line` for the pinned version.
That indirection is deliberate: re-anchoring onto a future cp is then one table
edit rather than two hundred inline ones — which is exactly what
[Verifying the port against cp](https://github.com/dvoyni/cog/issues/423) §10.1
found had to be done by hand when the closed tickets were discovered to cite
v1.2.1 while the porting index described v2.4.0. **Settled here.**

### Licence

Both sources are MIT and the package keeps both notices:

- `jakecoffman/cp` — "Copyright (c) 2017 Jake Coffman";
- Chipmunk2D — "Copyright (c) 2007-2015 Scott Lembcke and Howling Moon Software".

Box2D v3 and `neguse/gox2d` were read as references only and nothing is taken
from them.

---

## Contents

- [Vocabulary](#vocabulary) · [What the numbers are, and what they are not](#what-the-numbers-are-and-what-they-are-not)
- [The Components](#the-components) · [The Shape](#the-shape)
- [The five Systems](#the-five-systems) · [The contact list](#the-contact-list) · [Joints](#joints)
- [Sleeping and Islands](#sleeping-and-islands)
- [The queries and the indices](#the-queries-and-the-indices) · [Settings](#settings)
- [Fidelity to cp](#fidelity-to-cp) · [The zero-allocation claim](#the-zero-allocation-claim)
- [What is not foreclosed](#what-is-not-foreclosed) · [Shapes that were rejected](#shapes-that-were-rejected)
- [Ported symbols](#ported-symbols)
- [Required work](#required-work) · [Out of scope](#out-of-scope)

---

## Vocabulary

Every term here is in `CONTEXT.md` under **Physics**, which is the glossary of
record; this list is a reading aid, not a second definition.

- **Body** — an Entity physics moves or collides with. Exactly one of three
  kinds, said by its Components.
- **Static body** / **Kinematic body** / **Dynamic body** — never moves; moved by
  the app setting its velocity; moved by Forces and pushed by what it touches.
- **Angle**, **Angular velocity**, **Torque**, **Moment of inertia** — the
  rotational half of a Body's state. An infinite Moment of inertia is a Body
  that does not turn, not a kind of its own.
- **Centre of gravity** — the point a Body moves and turns about, which is the
  Body's position itself.
- **Shape** — the one convex region a Body occupies. A point is a circle of
  radius 0; a box is a Polygon of four vertices; any kind may be rounded.
- **Polygon** — the vertices of a Shape too large to carry them inline.
- **CollisionBits** / **CollidesWith** — the groups a Shape is in, and the groups
  it collides with. Both sides must agree.
- **Sensor** — a Shape that reports Contacts but is never pushed and pushes
  nothing.
- **Contact** — two Entities whose Shapes touch, found once a tick, marked begun,
  continuing or ended.
- **Contact point** — one of the at most two places a Contact touches.
- **Slop** — how far two Shapes may overlap and be left alone.
- **Impulse** — how much push was delivered in an instant.
- **Friction** / **Restitution** — a Contact's resistance to sliding, and how much
  approach speed is kept on bouncing. Each Shape carries its own; the pair's is
  the product.
- **Damping** — how fast a Dynamic body's velocity decays on its own, as a rate
  per second. A Body's, wherever it is.
- **Constants** — the physics values that hold for the whole world rather than
  for one Body, gravity among them. Physics starts them at its own defaults and
  reads them every tick; a game that wants others changes them itself. Not
  **Config**, which is fixed when physics starts.
- **Sleeping body** — a Dynamic body physics has stopped moving because it and
  everything in its Island stayed idle long enough, until something disturbs it.
  Off unless the game turns it on.
- **Island** — the Dynamic bodies joined by touching or by Joints, which fall
  asleep together and wake together. A Static or Kinematic body never belongs to
  one and never joins two.
- **Joint** — a rule holding two Bodies to each other. Its own Entity.
- **Spring** — a Joint that pushes towards a rest distance or Angle.
- **Absorption** — how strongly a Spring resists its ends moving, force per unit
  of that speed.
- **Probe** — moving a circle, possibly of radius 0, in a straight line and
  finding what it touches. **Hit** is what it reports.
- **Overlap** — asking which Entities a Shape at a position touches.
- **Static index** / **Body index** — the plugin's two indices. Which one a query
  asks is the caller's choice.

**cp's own words are not adopted.** *Arbiter* is a Contact, *constraint* is a
Joint, *space* is nothing at all — the plugin has no world object. *Manifold*,
*collider*, *fixture*, *rigid body*, *trigger*, *sweep*, *ray cast* and *shape
cast* are on the glossary's avoid lists. **Chunk**, **Relation**, **Prefab** and
**Template** are unspent by the ECS and this package does not spend them either.

---

## What the numbers are, and what they are not

Every measurement quoted below was taken on:

```
go version go1.27.1 windows/amd64
AMD Ryzen 9 7950X3D 16-Core Processor, GOMAXPROCS=32
```

Numbers fall into four classes and the difference matters:

- **cp's, measured** — `Space.Step` at 60.9 µs (N=256) and 284 µs (N=1024) on the
  reference scene, with 164 / 12.5 KB and 656 / 49.8 KB of allocation, and 44.5
  and 177.2 touching Contacts. From the porting index §13. These are the numbers
  the port is compared against, and **they are recorded, never asserted.**
- **cog's, measured** — the index costs, **re-taken in float64** on the hardware
  above: a short Probe at **103 ns**, `BodyIndex` upkeep at **70 µs** per 1 024
  Bodies — **67 µs** since [#441](https://github.com/dvoyni/cog/issues/441) took
  the Entity-to-slot map out of it, and **53 µs** at `Angle` 0 since
  [#570](https://github.com/dvoyni/cog/issues/570) took its rotation from one
  `math.Sincos` — one static Entity replaced at **97–102 ns**.
  The float32 prototype [issue 345](https://github.com/dvoyni/cog/issues/345) had
  the same three at 30–50 ns, 9 µs and 17–24 ns, and **those numbers are
  superseded, not adjusted**: its Components were half the size and nothing
  measured over them can be quoted for this package.
- **cog's, derived** — the ECS walk at **≈ 9 ns an Entity over these Components**,
  **re-taken in float64** against `ecs.md`'s ≈ 3.4 ns; scheduling at ≈ 5.7 µs a
  System, confirmed here at 5.3–6.3 µs for a frame with one System and no Bodies
  in it; `Uses` dispatch at 1113 ns against 0.49 ns for a direct call, from
  `ecs.md` and not re-derived.
- **Gaps** — stated as such where they appear.

**The float64 re-measurement has been taken**, and three of the four numbers came
back materially worse than the float32 figures the design was argued from.

| | float32, as argued from | float64, re-taken | |
| --- | --- | --- | --- |
| ECS Query walk over Body Components | ≈ 3.4 ns an Entity | **8.6–10.1 ns an Entity** | 2.6× |
| short grid Probe, radius 0 | 30–50 ns | **103 ns** | ≈ 2.6× |
| `BodyIndex` upkeep, 1 024 Bodies | 9 µs | **69–73 µs** | **7.7×** |
| two `math.Exp` a Body a tick | never measured | **≈ 27 ns a Body** | — |

The walk was re-taken as a **slope** between 1 000 and 10 000 Entities, so that
the frame's fixed cost drops out, and the float32 predecessor was re-taken the
same way in the same session, **interleaved round by round** rather than one
after the other: `ecs.md`'s own two-Component walk reproduced at 3.48 ns an
Entity on this machine, which is what makes the 2.6× a comparison and not two
measurements from two eras. **Body Components doubling is not the whole reason.**
A `Position` and a `Velocity` are 48 B and 24 B, so the walk streams 72 B a row
where `ecs.md`'s two test Components are 16 B; float64 doubles the struct, and
the rest of the factor is that this is a far wider row than the number was ever
taken over.

**The upkeep gap is the one that matters**, and it is a finding rather than a
correction: 70 µs against 9 µs is 7.7×, and it sits inside `BodyIndex`'s write
lock every tick. Broken into its parts, per 1 024 Bodies: **17 µs** of `math.Cos`
and `math.Sin`, one pair a Body, which this specification prescribes; **13 µs**
of Go map operations for the Entity-to-slot table, two a Body; **8 µs** of cell
listing; and the remainder in the world-cache write and the entry bookkeeping.
The map alone is larger than the whole figure the design was argued from. Nothing
is re-decided on the strength of it here: the number is recorded, the design it
was argued for is unchanged, and what to do about it is a later ticket's.

**[#441](https://github.com/dvoyni/cog/issues/441) took the map out, and it was
worth less in place than on its own.** The Body index is Cleared and refilled every
tick, so a Shape's slot is now the walk's own position and the index keeps no
Entity-to-slot table; the static index, kept current incrementally, keeps its map.
Measured interleaved A/B, both binaries built and alternated round by round, on the
hardware above: the rebuild of 1 024 Bodies went from a median **70.5 µs to
67.4 µs** (18 runs each), and the whole step from **369.8 µs to 362.2 µs** at
N = 1 024 and **101.8 µs to 100.8 µs** at N = 256 (12 runs each). A short Probe
and `ProbeAll` are unchanged. **The 13 µs the map cost measured on its own did not
all come back**: removing it saves 3–4 µs of the rebuild benchmark and about
8 µs of the step, which is a decomposition priced part by part not adding up in
place, and is recorded rather than explained. The rotations, 17 µs, were then the
largest single part.

**Reusing a Body's rotation when its `Angle` did not change was put and decided
*no*** on [#569](https://github.com/dvoyni/cog/issues/569): the rebuild stays
stateless. What that ticket found instead is that `m.ForAngle`'s `math.Cos` and
`math.Sin` give the same bits as one `math.Sincos` in about 60% of the time, and
[#570](https://github.com/dvoyni/cog/issues/570) made the change, pinned bit for
bit against the separate calls on amd64 and on wasm. Measured interleaved A/B,
both binaries built and alternated round by round with the order of the pair
swapped every round, 16 runs each, on the hardware above and with another
benchmark running on the machine at the same time: the rotations went from a
median **16.1 ns to 13.4 ns a Body**, which is **16.5 µs to 13.7 µs** per 1 024
Bodies, and are still the rebuild's largest part. The rebuild of 1 024 Bodies went
from a median **67.4 µs to 52.9 µs** (minimums 66.7 and 52.3), and the whole step
from **422.9 µs to 417.3 µs** at N = 1 024 and **127.9 µs to 126.2 µs** at
N = 256 (minimums 406.9 to 398.0 and 120.5 to 117.9), which is inside a
whole-frame benchmark's noise.

**The rebuild's 14.5 µs is not the rotation's saving; it is the saving at
`Angle` 0.** Every Body `BenchmarkBodyIndexRebuild` inserts stands at `Angle` 0,
and `math.Sincos` returns at once for a zero angle where `math.Cos` runs its
whole kernel, so at 0 the rotation all but vanishes. At angles that differ per
Body, which is `BenchmarkTheRebuildsRotations`, the saving is 2.7 ns a Body,
about 2.8 µs per 1 024. A rotating scene should read the latter.

**`BenchmarkTheStep`'s reference scene never rotates**, and its step figures here
and everywhere else in this specification are taken over Bodies that never turn.
Its circles carry a Friction of 0.7, but every pair is pressed straight along
the line between its centres — each Dynamic circle is pushed along +X into a
Static one 0.75 m further along +X — so the tangent stays at zero, no impulse has
a lever arm, and every Body keeps a bit-equal `Angle` of 0 on every tick. That was
checked over 3 000 ticks at N = 256 and N = 1 024. It is a known property of the
scene, recorded and not changed, because changing the scene would break the
comparison with every A/B already recorded against it; **its numbers are not
representative of a scene that rotates**, and no decision about rotation should
be read from them.

**The porting index's own measurements are not reproducible.** They were taken
*"in throwaway modules outside cog's tree"* with no branch, directory or
repository holding the harness. The `research/port-vs-cp` branch specified in
[Fidelity to cp](#fidelity-to-cp) exists to stop that recurring.

**One number on the record cannot be cited.** The porting index gives cp's Probe
hit rate as *"0.729 against brute force 0.896"* while defect 5 describes the same
experiment as *"187 of 918 hits missed"* — which is 0.714, not 0.729. The two are
never reconciled. The port **re-derives** it rather than quoting either. The
claim that survives is the qualitative one both figures support: **cp misses
roughly a fifth of the Hits a radius-0.3 Probe should find, and returns a farther
Hit for about a tenth more.**

---

## The Components

From [The Component vocabulary](https://github.com/dvoyni/cog/issues/287),
[Rotation](https://github.com/dvoyni/cog/issues/319),
[The contact solver](https://github.com/dvoyni/cog/issues/405) and
[Joints](https://github.com/dvoyni/cog/issues/318).

| Component | Fields | Written by | Read by |
| --- | --- | --- | --- |
| `Position` | `Current, Previous m.Vec2d`; `Angle, PreviousAngle float64` | Integrate, Solve (the bias correction), gameplay | Index, Detect, gameplay, the app's render copy |
| `Velocity` | `Linear m.Vec2d`; `Angular float64` | Integrate, Solve, gameplay | Integrate, Solve, gameplay |
| `Force` | `Force m.Vec2d`; `Torque float64` | gameplay adds; Solve clears | Solve |
| `Dynamic` | `InvMass, InvInertia, Damping, AngularDamping`, exported for serialisation and written only by the constructors and setters | constructors and setters | Solve |
| `Shape` | see [The Shape](#the-shape) | the app | Index, Detect, queries |
| `Polygon` | `Verts m.List[m.Vec2d]` | the app | Index |
| `Joint` | see [Joints](#joints) | the app; Solve writes the Impulse and ratchet's `Angle` | Index, Solve |
| `Static` | Tag | spawn | Index |
| `Sleeping` | Tag | the sleep System, and nothing else | Integrate, Index, Solve, the app's own Queries |

Beside these the plugin keeps one Component of its own, `Rest`, which no app can
name: what the sleep System remembers per Dynamic body, added the first tick
sleeping is on. [Sleeping and Islands](#sleeping-and-islands) says why it is a
Component.

**The split follows writers, because the lock unit is the Store.** Fields with
identical writer and reader sets travel together; `Force`, the busiest gameplay
write, is kept away from `Position`'s readers so that a System adding Force does
not block the render copy. One Component for everything was rejected for that
reason; one Component per field buys no concurrency past this split and widens
every Query — `ecs.md` records an extra probe at about 7 ns as an unexplained
gap. **Each angular field has exactly the writers of its linear partner**, which
is why they join the existing Components rather than forming their own; separate
ones would need two versions of every Query so a rotation-free Body could leave
them out. That costs 32 B per Body, including Bodies that never turn.

### Body kind is presence

- **Dynamic** — has `Dynamic`. Integrated from Force, pushed by what it touches.
- **Kinematic** — has `Velocity` and no `Dynamic`. The app sets its velocity; it
  pushes, nothing pushes it. The solver reads the missing `Dynamic` as infinite
  mass.
- **Static** — has the `Static` Tag. No `Velocity`.

This is what replaces cp's `m == INFINITY` test, which matters because cp's
`INFINITY` is `math.MaxFloat64` compared *exactly* — a defect the port does not
inherit. **A `Kinematic` Tag is rejected**: with the absence of `Dynamic` already
saying it, the Tag would encode one fact twice, which the glossary forbids. A
`Kind` field is rejected because Static and Kinematic Bodies would carry a mass
that means nothing.

### Constructors and invariants

Unexported fields exist **only where fields carry an invariant between them** —
`Dynamic` and `Shape`. Everything else keeps exported fields, so reflecting a
velocity stays a plain field write, and a replacement solver in another package
can write every field it needs to.

- `NewDynamic(mass, moment, damping, angularDamping)`, with `SetMass`,
  `SetMoment`, `SetDamping`, `SetAngularDamping`.
- **Mass or moment of 0, negative, or NaN is rejected**, so an infinite inverse
  is never stored. That is the `+Inf · 0 = NaN` cp's joints hit.
- **Infinite mass is rejected too.** A `Dynamic` carrying `invMass = 0` would be
  an infinite-mass Dynamic body, indistinguishable from Kinematic, and would slip
  past a presence-based classification into the solved list. A Body nothing
  pushes is said by having no `Dynamic`.
- **An infinite moment is legal and means a Body that does not turn**, stored as
  `invInertia = 0`. This is cp's own spelling of `1/∞`. There is no
  `FixedRotation` Tag; it would say twice what `invInertia = 0` says.
- `Dynamic{}` is harmless: infinite mass and infinite moment, documented as *not
  a Body you built*, not as a second spelling of Kinematic. Nothing checks for it.

Ported here as free functions: `MomentForCircle`, `MomentForSegment`,
`MomentForBox`, `MomentForPoly`, `AreaForCircle`, `AreaForSegment`,
`AreaForPoly`, `CentroidForPoly`.

#### Mass from shape and density

**`NewDynamicForShape(shape, polygon, density, damping, angularDamping)
(Dynamic, Shape, Polygon, m.Vec2d, error)`** is how a new Dynamic body gets its mass and
moment, and `NewDynamic` is for the Body whose numbers the app already has. It
is cp's `AccumulateMassFromShapes` in its one-shape case, one Shape per Body
being the rule:

- **Mass is `density · area`** and the moment is about the centroid, from the
  helpers cp's own shapes use — `AreaForCircle` and `MomentForCircle`;
  `AreaForSegment` and `MomentForBox` over the capsule's length and width, which
  is cp's `NewSegmentMassInfo` and `MomentForSegment`'s formula; `AreaForPoly`,
  `MomentForPoly` and `CentroidForPoly` for the three polygon kinds. Every
  area takes the Shape's radius; `MomentForPoly` does not, in cp or in C. The
  Body is built through `NewDynamic`, so its checks apply.
- **It recentres.** `Position` is the centre of gravity, so where cp stores a
  `cog` offset the constructor moves the Shape instead: a circle's offset
  becomes zero, a segment's endpoints and a polygon's vertices are shifted by
  the centroid, and a segment's neighbour tangents, being relative, stay. The
  material, the collision fields and `Sensor` are carried over.
- **It returns the centroid**, the vector the Shape was shifted by, in the
  Shape's original frame. The app places the Body at the old origin plus it to
  leave the geometry where it was:
  `Position{Current: origin.Add(centroid), Previous: origin.Add(centroid)}`, or
  `origin.Add(centroid.Rotate(m.ForAngle(angle)))` for a Body spawned turned.
  Handing it back is what makes that one line for every kind: a Poly's
  recentred Shape carries no vertex to compare against. Recentring is the part an app gets wrong, and gets wrong silently — an
  off-centre polygon spins about the wrong point and nothing fails — which is
  why a bare `MassForShape` returning numbers was not taken.
- **The pair goes in and comes back as `NewPolygonShape` returns it.** An
  inline kind returns the zero Polygon. **The caller's Polygon is never
  written**: a Poly gets a new vertex List, an allocation the constructor
  documents, being off the hot path. The inline kinds allocate nothing.
- **A Shape with no area** — a circle or a segment of radius 0 — is refused with
  `ErrNoArea`, and a density that is not positive and finite with
  `ErrBadDensity`. Like `NewDynamic`'s refusals, each returns the zero `Dynamic`,
  with the Shape and Polygon as given and the zero centroid.
- It adds **no bytes to `Shape`**. Its answers agree with cp's at 1e-9 on every
  kind, frozen in `cpmasscases_test.go`.

### Position is the centre of gravity

**The Body has no separate origin and no `cog` field.** Shape geometry is local
to `Position` and may be off-centre: a circle has a centre offset, a segment
local endpoints, a Polygon local vertices. This is cp with `cog` fixed at 0, so
`transform = [R | p]` and no origin-versus-centre-of-gravity distinction runs
through the solver, Contacts or Joints. Contact `r1`/`r2` and Joint anchors are
all relative to `Position`.

A Polygon whose vertices are not centred is legal and turns about `Position` —
correct physics for mass concentrated there, such as a hammer written that way on
purpose. A door drawn around its hinge is recentred by `NewDynamicForShape` at
spawn; if the art's pivot is not the centre of gravity, the app's render copy
adds a constant offset.

Rejected: deriving the centre of gravity from the Shape (the integrator would
have to read `Shape`, and an offset centre would be inexpressible), and carrying
a `cog` field as cp does (every prestep, Contact and Joint would carry
`Position + R·cog`, which is two coordinate systems through all the ported code).

### Rotation is an unwrapped angle

`Angle` is float64 radians, turning from +X towards +Y as in cp, and **never
wrapped**. The previous tick's is kept beside it for render interpolation, so
interpolation is a plain lerp with no seam at ±π. Cos and sin are derived once
per moved Body per tick, where the world-space Shape data is cached.

Storing (cos, sin) beside the angle is rejected: the two fields would have to
agree, which means unexported fields and a setter, so gameplay could no longer
set a heading with a plain field write. Precision is not a concern — ten turns a
second for ten hours is about 2.3e6 rad, where float64's spacing is 5e-10.

### The vector is `m.Vec2d`

A float64 vector in `libs/m` beside `Vec2` and `Vec2i`; the `d` follows the `i`.
Methods take `Vec2`'s names (`Add`, `Sub`, `MulS`, `Dot`, `Cross`, `Length`,
`Normalize`, `Rotate`, `Lerp`), and cp's methods with no match are added under
cog-style names (`Perp`, `ReversePerp`, `Project`, `Unrotate`, `ForAngle`).
Conversion is `Vec2d.Vec2()` and `Vec2.Vec2d()`; the render copy narrows with
`Vec2()`.

It goes in `libs/m` rather than in this package because every physics Component
and query exposes the type to gameplay code, and a physics-owned vector would
make gameplay import physics for vector maths. cp's `Transform` and `BB` are
physics-only and stay here.

### What the package does not have

- **No immobile concept.** The app picks a very large mass or a Kinematic body.
- **No shipped Bundles.** `Spawn` treats every Bundle field as a Component and
  Bundles do not embed, so a plugin-shipped Bundle could only spawn an Entity
  carrying nothing but this plugin's Components. The spec lists the Component set
  per kind; the constructors build the values.
- **No `m.Transform`.** Transform is 3D with rotation and scale; sharing it
  would put the third axis into the contract. The app writes `m.Transform` from
  `Position` in its own one-way System. Two Components describing one
  position are, to the scheduler, unrelated — the lock unit is the Component
  type, so two Systems writing "the" transform would run concurrently on separate
  copies and nothing would report the conflict.
- **No validity checks**, and the ECS's Validation mode is not borrowed for them.
  Combinations other than the three kinds are undefined. **One trap is worth
  stating**: a Dynamic body missing `Force` falls out of the integrator's Query
  and silently never moves. A Dynamic body with no `Shape` is legal and wanted —
  it moves and collides with nothing.

---

## The Shape

From [Convex polygons](https://github.com/dvoyni/cog/issues/323),
[Collision filtering](https://github.com/dvoyni/cog/issues/406) and
[The contact solver](https://github.com/dvoyni/cog/issues/405).

**One `Shape` Component of 104 bytes**, whose kind names its vertex count:

| field | type | bytes | what it holds |
| --- | --- | ---: | --- |
| `verts` | `[4]m.Vec2d` | 64 | the geometry, local to `Position`, read by the kind |
| `Radius` | `float64` | 8 | a circle's radius, or the rounding on a segment or Polygon |
| `Friction` | `float64` | 8 | default 0 |
| `Restitution` | `float64` | 8 | default 0 |
| `CollisionBits`, `CollidesWith` | `uint32` each | 8 | the groups it is in, and the groups it collides with |
| `Kind` | `uint8` | 1 | Circle, Segment, Tri, Quad, Poly |
| `Sensor` | `bool` | 1 | |
| padding | | 6 | |

**The kind carries the vertex count.** `Circle` uses `verts[0]` as its centre
offset; `Segment` uses `verts[0..1]` and, when it has neighbours, `verts[2..3]`
for the two tangents; `Tri` and `Quad` fill three and four. A count field
contradicting its kind cannot be spelled, and each branch of the pair dispatch
has a constant loop bound.

**Four slots cost nothing extra.** A segment with neighbours already needs four,
so a triangle and a box ride along free. Eight would be 152 B before Friction and
Restitution, adding 64 bytes to every circle and every wall segment in the world;
cp's `planes` layout, vertices and normals both, would be 280 B. Against the
reference scene that is 88 KB / 152 KB / 280 KB for 1 024 Bodies and 363 KB /
627 KB / 1 155 KB for 4 224 statics. For comparison, `cp.Shape` is 168 B with
five pointers and a circle is three allocations; **C**'s `cpPolyShape` is 600 B,
because it inlines twelve planes whether a box needs them or not.

**Local normals are not stored.** They are derived when the world cache is built,
one `Normalize` per edge — and a static Polygon is cached once, at insert, so the
sqrt falls only on moving Polygons. Storing them is a pure time-for-space trade
paid by every circle in the world.

### A Polygon over four vertices is a second Component

`Polygon{Verts m.List[m.Vec2d]}`, probed with `ecs.Get[Polygon]` by Index and
copied into the world cache once a tick. The hot path never touches it.
`m.List[T]` is the storable answer to a variable-length run in a Component: a
32-byte header, length fixed at construction, `At` yielding copies, deliberately
no `Slice`.

**So there is no vertex cap at all** — which matters because **cp has none
either**. **C**'s `CP_POLY_SHAPE_INLINE_ALLOC = 6` is an inline-storage
threshold, not a limit, and the Go port always heap-allocates. Box2D v3's
`MaxPolygonVertices = 8` is the only real cap anyone ships.

**The three pair primitives take one extra `verts []m.Vec2d`**, nil for every
kind but `Poly`, because a `Shape` of kind `Poly` does not carry its vertices and
a List cannot hand out a slice. The caller owns the scratch array, as
`ProbeAll`'s destination slice already requires.

**The cap of four is a constant, not a design.** [prototype: what Get[Polygon]
costs](https://github.com/dvoyni/cog/issues/409) is the measurement that would
raise it, and it records that raising it later is a source change for apps: a
5-gon spawned with a `Polygon` Component becomes an inline kind.

### Building a Shape, and what a bad one does

**One way in: a constructor that always hulls**, cp's `ConvexHull` (QuickHull)
ported, with the box constructor keeping cp's four direct vertices. **Winding is
cp's clockwise**, and the constructor enforces it: `AreaForPoly` treats clockwise
as positive, so counter-clockwise input would otherwise give negative mass.

**A failing constructor returns the Shape and an error, and the Shape is a
point** — a circle of radius 0 at the local origin, which collides with almost
nothing and cannot be mistaken for what was asked for. The errors are
package-level sentinels, so the failure path allocates nothing. Three failures:

1. fewer than three vertices;
2. a degenerate outline — zero area or coincident vertices, which makes cp's
   `CentroidForPoly` divide by zero;
3. **an outline the hull changed**, which is the concave case.

**A concave Polygon does not silently become its hull.** cp does none of this:
`cpPolyValidate` is gone from this version, the raw constructors accept anything,
and Go's asserts compile out by default, so in cp a concave outline collides as
its hull forever with nothing said.

### Segment neighbours come from the app

Defect 4 is fixed. **C**'s `cpSegmentShapeSetNeighbors` stores two local tangents
so a Body rolling along chained segments does not catch at the joints; the Go
port has no such method, leaves them zero, and its end-cap rejection is therefore
dead code.

**A second constructor takes the previous and next point**, and the tangents live
at `verts[2]` and `verts[3]`. They stay local and are rotated on use, as cp does —
`CacheData` never transforms them.

**There is no chain concept.** Segments remain separate Entities, and which
segment neighbours which is the app's knowledge, like door axes and merged runs.
Naming a chain would invent the concept [Static geometry as
Entities](https://github.com/dvoyni/cog/issues/288) refused to have.

### The world cache lives in the index entry

cp recomputes the transformed circle centre, segment endpoints and normal, and
the world half of a Polygon's planes every step through the body transform. **The
port puts that cache in the index entry**, beside the bounding box the grid
already stores: the transform per entry, and the world vertices and normals in a
plugin-owned slab. Being internal, it is free of the Component's fixed-array cap.

- **Statics are cached once**, when the `Hooks[Shape, HookAddedRemoved]` drain
  inserts them, and never again.
- **Bodies are cached in Index**, which already walks `Position` and `Shape`.

Not a Component: adding or removing one is a structural change — it takes
`write{*Store[T]}` and nothing wider (`ecs.md`'s *Structural change*, and
`Set.UpdateFor` in `set.go`), but it would be one every tick a Body came or went —
and it would double the memory to mirror something derived. *This paragraph once
said the structural change took the frame-wide `write{*Entities}` lock; only
Spawn and Despawn do, and the line is corrected by
[sleeping](https://github.com/dvoyni/cog/issues/316), whose Tag rests on it.* Not recomputed per pair, which is Box2D v3's way: a Shape
takes part in several pair tests a tick, and a wall segment in a crowd in many.

### Collision: a switch, values, and a stack

- **The pair dispatch is a nine-arm switch on the family** — circle, segment,
  polygon — which the five kinds map onto. cp's `[9]CollisionFunc` table keyed by
  `a.Order()+b.Order()*3` goes, and with it `Order()`'s type switch on an
  interface.
- **Support is a free function switching on family** over the world-cache entry.
  cp's `SupportContext` carries **two func fields**, rebuilt at four call sites
  and passed by value through `GJK`, `GJKRecurse`, `EPA` and `EPARecurse`, each
  support function type-asserting its shape back out. All three layers go.
- **EPA's hull becomes two fixed `[32]MinkowskiPoint` buffers ping-ponged on the
  stack.** cp's `EPARecurse` calls `make([]MinkowskiPoint, count+1)` **on every
  iteration**, up to 30, where the **C** uses `alloca`. 32 is cp's 30 iterations
  plus the initial three. This is the narrowphase's whole share of cp's 656
  allocations a step; the other source, `LookupHandler`, is removed by having no
  handler table at all.
- **The recursion is kept as written.** 30 frames is nothing, and flattening it
  would be a departure with no reason behind it.
- **At most two Contact points a pair**, cp's `MAX_CONTACTS_PER_ARBITER`,
  whatever the vertex count.

Ported line for line: `CircleToCircle`, `CircleToSegment`, `EPARecurse`,
`ContactPoints`, `SupportEdgeForPoly`, `SupportEdgeForSegment`, and the mass
helpers. Changed: the dispatch and support layers above; `CircleToPoly`'s sign;
the guards; cp's `log.Println` on high EPA iterations, which the port does not do
— the **C**'s three GJK warnings having been dropped by the Go port already; and
`ClosestPoints` and `GJKRecurse`, for the three departures below.

**GJK does not report disjoint Shapes on one line as touching.** From
[ecsphysics2d: GJK reports disjoint Shapes as touching when the origin lands on
a Minkowski edge's supporting line](https://github.com/dvoyni/cog/issues/439).
All three are departures for **a defect in cp**, and the **C** has each of them:
[research: does Chipmunk's C GJK report disjoint Shapes as
touching?](https://github.com/dvoyni/cog/issues/506) reproduced the first in C
7.0.2 and `master`, 436 of 3,848 box-disjoint fuzz pairs, the port's count
exactly, and the other two were traced in both builds for #439. They are reached
through Detect and not only through the bare `Penetration`: two capsules 1 m
long, radius 0.25, laid end to end on one line 0.05–0.2 m apart over 3,600
angles have intersecting boxes at 3,660 placements, and 1,410 of those were
Contacts, 548 of them deeper than 0.25 m. After the three, none is.

- **GJK's answers take the vertex arm when `t` is clamped or the simplex has
  collapsed.** cp's `ClosestPoints` takes its overlapping arm on
  `if(d <= 0.0f || (-1.0f < t && t < 1.0f))` (`cpCollision.c:246`, the same at
  7.0.2 and `master`), and `d` is the distance to the edge's *supporting line*,
  not to the edge. With `t` clamped the origin is beyond the edge's end, and on
  that line `d` is 0 — or at a general angle a rounding below it — so the pair is
  reported at its summed radii. The test is on the clamp and not on `d == 0`,
  because the rounded form is the one the engine reaches. A simplex collapsed to
  one Minkowski point has no edge, a zero normal and a `d` of 0, and takes the
  same arm. Either way `p` being the zero vector is a vertex contact, which keeps
  the edge's normal so it still has a direction.
- **EPA keeps cp's `ClosestPoints`.** There the origin is inside the hull, and a
  hull edge ending at a point in the middle of a face clamps `t` by a rounding of
  1e-16 with the origin a real 0.01–0.6 m inside. The guard would read that
  overlap as a separation of `|p|`, and a stack of boxes falls through itself.
- **A collapsed simplex searches along −p.** Two Shapes on one line tie every
  support query along the cold-start axis, which is perpendicular to that line,
  so both ends of the simplex are one point. `master`'s `ClosestT`, which the port
  carries, then gives `t = 0`, the search direction is the perpendicular of the
  zero vector, and GJK stops where it started with no normal. **C 7.0.2**'s
  `ClosestT` divides zero by zero and its clamp turns the NaN into `t = 1`, so it
  searches along −p by accident and lands on the clamped edge instead: the
  research's draw 4094 is two contacts with a zero normal in `master` and a false
  0.25 m in 7.0.2. The port searches along −p on purpose. **The coincident-centres
  rule is untouched:** a cold-start axis still zero after the seed is answered by
  `GJK` itself with cp's `ClosestPoints`, so the pure `Penetration` still reports
  no direction there.
- **EPA is entered only if the new support point reached the origin**:
  `p·n >= 0` is tested before cp's two `cpCheckPointGreater` calls
  (`cpCollision.c:372`). `p` is the difference's extreme point along `n`, so
  `p·n < 0` puts the origin outside whatever the orientation tests say, and they
  say otherwise only by rounding: capsules end to end give a `v0`, `p` and `v1`
  on one line through the origin, a triangle with no area that C and cp read as
  holding it, and EPA then reports the summed radii. It needs no tolerance and
  changes an answer only where the two tests contradict each other.

The zero-depth disagreement between argument orders, where end-cap rejection
rejects an exactly tangent pair one way round only, is not fixed here: the
finiteness fuzz logs 1 such pair before the three and 1 after, and still fails on
any disagreement with a real overlap behind it. `Penetration` needs no box check
from its caller.

---

## The five Systems

From [The System decomposition](https://github.com/dvoyni/cog/issues/398),
[The contact solver](https://github.com/dvoyni/cog/issues/405) and
[Joints](https://github.com/dvoyni/cog/issues/318).

**There are no sub-steps.** Physics updates once per `app.UpdateEvent` tick, in
five Systems that always run in series, and alongside the rest of the frame
wherever locks allow. The step is `UpdateEvent.Dt`, fed with `ecs.Feed`. Physics
runs on **every** published tick, catch-up ticks included; it never skips,
because a skipped step changes the simulation.

**`Integrate → Index → Detect → Sleep → Solve`, in cp's own order** — positions
first, then detection, then cp's `ProcessComponents`, then everything else inside
Solve.

| System | reads | writes |
| --- | --- | --- |
| **Integrate** | `Velocity`, `Sleeping` | `Position` |
| **Index** | `Shape`, `Position`, `Polygon`, `Static`, `Joint`, `Sleeping`, `Hooks[Shape, HookAddedRemoved]`, `Hooks[Sleeping, HookAddedRemoved]` | `StaticIndex`, `BodyIndex`, `JointedPairs` |
| **Detect** | `Shape`, `Position`, `StaticIndex`, `BodyIndex`, `JointedPairs` | `Contacts` |
| *(the app's filter Systems — cp's Begin and PreSolve)* | `Contacts` + the app's own | `Contacts` |
| **Sleep** | `Sleep`, `Constants`, `Dynamic`, `Velocity`, `Position`, `Joint` | `Force`, `Contacts`, `Sleeping`, `Rest`, the `WakeCmd` queue |
| **Solve** | `Dynamic`, `Constants`, `Sleeping` | `Velocity`, `Force`, `Position`, `Contacts`, `Joint` |
| *(the app's reaction Systems — cp's PostSolve)* | `Contacts`, `Joint` | the app's own |

Every System also takes `read{*Entities}`. **The only new locks are on Resources
physics owns**, and the chain serialises nothing that could have run in parallel.
Making Systems that run in parallel today take turns is rejected outright.

Sleeping added the Sleep System and one read to three of the others:
Integrate, Index and Solve each read the `Sleeping` Store, which only the sleep
System writes, and every other lock the sleep System takes is one Solve, the next
link of the chain, already holds as strongly. The argument is in
[Sleeping and Islands](#sleeping-and-islands), and
`TestSleepingCostsNoSystemParallelism` holds it against the engine's own
description.

Solve's read of `Constants` is the one lock in the table nothing of physics'
writes. A read is shared, so it serialises Solve against no System that ran
beside it before `Constants` existed; the only System that waits on it is an
app's own that took `write{*Constants}`, and that System runs in series with
Solve on every tick it is subscribed to, which is the price the app chose.

- **Integrate is one System, not two.** Both its Queries use `Velocity`, one
  writing and one reading, so a split serialises anyway and costs about 6 µs.
- **Index and Detect are two Systems, not one.** The index writes are held only
  for the rebuild — **about 70 µs for 1 024 Bodies, re-taken in float64** against
  the 9 µs the float32 prototype gave — and detection, the heavy part, runs under
  reads, so gameplay queries overlap it. One System doing both would hold the
  index write through detection and no query could overlap it. The re-taken
  number makes the split matter more rather than less, and it also says plainly
  what a gameplay query on `BodyIndex` waits behind at that population.
- **Scheduling costs about 5 × 5.7 µs ≈ 29 µs a tick.** For physics this
  supersedes the map's finding that one System per bound plugin is the shape the
  arithmetic supports: detection and response must be separate Systems. The
  fifth, Sleep, is paid whether or not sleeping is on, and is what sleeping costs
  a world that leaves it off.

### Integration, per second, for one step `h`

**Velocity**, Dynamic bodies only (cp skips Kinematic ones), inside Solve:

```
v ← v·exp(−damping·h)        + (gravity + Force·invMass)·h
w ← w·exp(−angularDamping·h) + Torque·invInertia·h
Force, Torque ← 0
```

`gravity` is `Constants.Gravity`, read once a tick and not once a Body.

**Position**, every Body with a `Velocity`, Kinematic ones included, in Integrate:

```
Previous ← Current;  PreviousAngle ← Angle
Current  ← Current + v·h
Angle    ← Angle   + w·h
```

`exp(−rate·h)` is cp's `damping^dt` with `damping = e^−rate`. It is exact and
stable at any `h`. A naive `v − rate·v·h` goes negative once `rate·h > 1`;
Box2D's `1/(1 + rate·h)` is cheaper but departs from cp.

**Measured**, on the hardware above: the velocity update costs **37 ns a Body a
tick** as it ships, **10 ns** with Box2D's form in place of the exponentials, and
**2.6 ns** with no damping factor at all. So exponential Damping's two
`math.Exp` calls are **≈ 27 ns a Body a tick** against the cheap form and ≈ 34 ns
against nothing — about **28 µs a tick over 1 024 Dynamic bodies**, which is the
same order as keeping `BodyIndex` current. The pair on its own,
in a loop with nothing to overlap them, is ≈ 15 ns; they cost more where they sit
than they do in isolation. **cp's form is kept anyway**: it is exact and stable
at any `h`, and the alternative is a departure from cp with no reason from the
four that are acceptable.

**Damping is per Body**, a rate in 1/s, for moving and for turning — a superset
of cp's single global damping, which is reproduced by giving every Body the same
rates. A spinning wheel and a sliding crate need different rates, and the second
rate costs 8 B.

**The gravity term is cp's own**, `BodyUpdateVelocity`'s
`v·damping + (g + f·m_inv)·dt`, with `g` read from `Constants`
(see [Constants](#constants)). It is added beside `Force·invMass` and scaled by
the step with it, so an app that keeps writing `m·g` into `Force` under a
gravity of zero gets the same fall, agreeing at 1e-9. It has no angular term,
and neither a Kinematic nor a Static body receives it: the Query names
`Dynamic`, which is cp's early return for a Kinematic body said by the ECS
layout. cp's `SetGravity` also wakes every sleeping Body, and so does a changed
`Constants.Gravity` here: the sleep System compares the gravity it last saw
([Sleeping and Islands](#sleeping-and-islands)). A sleeper is not
velocity-integrated, so it gathers no gravity while it sleeps.

**One property the tests must name the rate for.** cp damps exactly but applies
Force as a plain Euler step, so terminal speed is not `F/(mλ)`:

$$v^{*} = \frac{F}{m\lambda}\cdot\frac{\lambda h}{1-e^{-\lambda h}}$$

At λ = 15 /s that is +13.0% at 60 Hz and +27.1% at 30 Hz — a 12% gap between
rates. At λ = 0.3 /s it is +0.25%, and at zero damping it is exact, so this bites
only heavily damped Bodies. **cog's fixed step makes it a constant offset that
disappears into tuning.** The exact form
`v ← v·e^{−λh} + (F/m)(1−e^{−λh})/λ` was weighed and **not taken**: it costs a
divide and a `λ = 0` branch per Body per tick (≈ 4 µs at N=1024) and departs from
the one formula above, to buy nothing at a fixed step.

### Solve is indivisible

Velocity integration cannot be its own System. It sits between `PreStep` and the
warm start, and **both sides are forced**:

- `PreStep` computes `bounce` from the velocity *before* integration, which is
  what stops gravity-fed jitter from eating Restitution;
- `ApplyCachedImpulse` must follow damping, or the warm-start Impulse is damped
  away before it does anything.

So Solve runs, in this order:

1. **Build the dense solved-Contact list** — a `[]int32` of entries, rebuilt each
   tick, excluding Sensors, dropped, ignored, and both-infinite-mass pairs. cp
   builds this during collision because its PreSolve is a callback; the port
   builds it at the top of Solve because the filters are Systems that run after
   Detect.
2. **Walk the `Joint` Query once, resolve each pair of References, build the
   dense Joint list** with the exclusions in [Joints](#joints).
3. **Assign `BodyIndex` slots over bodies in Contacts ∪ bodies in Joints.**
4. **Gather** those Bodies into dense arrays.
5. **`PreStep`** Contacts, then Joints.
6. **Integrate velocities.**
7. **Warm start** Contacts, then Joints.
8. **`Iterations` passes**: every Contact, then every Joint — **two complete
   passes per iteration, not interleaved element by element**, as cp does.
9. **Scatter** velocities back, and apply the bias as a position delta.

Steps 2 and 3 amend [The contact solver](https://github.com/dvoyni/cog/issues/405),
whose gather set was *bodies in solved Contacts*: a jointed Body may touch nothing
at all.

**A `Force` written this tick moves the Body next tick.** Under cp's order
position integration runs first, so only `Force` pays this latency; a `Velocity`
written directly is still immediate. Box2D's order (velocities → solve →
positions) removes it, and is a different engine's step.

### Solver data layout: gather, through a slot table

The Contact entry's two Bodies are already `BodyIndex` slots at detection, so a
`[]int32` of slot → dense index, cleared each tick, replaces any hash. At
N=1024 with 177.2 touching Contacts and 10 iterations:

| | cost a tick |
| --- | --- |
| Store lookups per Contact per iteration — 3 540 random reaches | 12–35 µs, **4–12% of a 284 µs step** |
| Gather through an Entity→slot hash — ~500 gathers, ~600 hash operations | ~9 µs; the hash eats the win |
| **Gather through the slot table** — ~500 gathers and scatters at ≈ 9 ns, plus a 4 KB clear | **~5 µs** |

The ≈ 9 ns is the float64 re-measurement of the ≈ 3.4 ns this table was first
written with, and it narrows the win over the hash from about 4.5× to about 1.8×
without changing which row is cheapest. **The arithmetic is also generous to the
slot table and always was**: it prices a gather into a dense `[]int32` at what an
ECS Query walk over 72-byte Component rows costs, which is the one number this
package had for *"reaching a Body's data"* and is not the same operation. The
conflation is recorded rather than repaired; the row order it produces is not in
question, and re-deriving it belongs with whoever reopens the solver.

The hot loop then touches nothing but two dense `float64` arrays. It is also
where the bias velocities live for free, and it is the structure a parallel
solver would partition if that is ever reopened.

### The bias velocity never becomes state

cp keeps `v_bias`/`w_bias` on the body, accumulates them in the iterations, and
spends and zeroes them at the top of the *next* step's position integration.
**In the port they are two columns of the gather array, applied as a position
delta before Solve returns** — `Current += vBias·h`, `Angle += wBias·h` — and
then discarded. No Component, no Resource, no cross-tick state, no glossary term.

It saves 24 bytes a Body against the alternative, which was real: unexported
`biasLinear`/`biasAngular` fields on `Velocity`, cp's shape, taking the Component
gameplay writes every tick from 24 B to 48 B.

Two smaller things fall out. The correction lands *before* the app's reaction
Systems read `Position`, so they see the corrected one. And it lands before the
next tick's `Previous ← Current`, so an app that teleports a Body between Solve
and Integrate cannot pick up a stale de-penetration nudge on top of the teleport.

**A correctness argument for this was put and withdrawn**, and is recorded so it
is not re-derived: cp computes the bias with the current step's `dt` and spends it
with the next step's, so under a *variable* timestep the correction lands scaled
by `dt_next/dt_prev`. **cog's step is fixed** — `slots/app` publishes one event
`steps` times unchanged, so a catch-up tick is an extra tick of the same size and
overload drops whole steps rather than stretching them. The ratio is always
exactly 1 and the error never fires. The decision stands on size and on having no
cross-tick state, not on correctness.

### The `k = 0` guard already exists

`k_scalar = a.m_inv + a.i_inv·(r1×n)² + b.m_inv + b.i_inv·(r2×n)²`, so it is zero
only when **both** Bodies have zero inverse mass *and* zero inverse inertia —
which is exactly the both-infinite-mass exclusion already in the dense solved
list. **There is no residual NaN case for Contacts**, and the guard is a
precondition rather than an assertion, with a debug-build assertion beside `nMass`
for the port's own tests.

A premise worth correcting: *"a Body that does not turn against a static one,
normal through its centre"* still has `m_inv > 0`, so `k > 0`. And cp has no
assert on this path at all — its `"Unsolvable constraint"` assert is in
`k_tensor`, which only Joints use. The Joint side of the guard is in
[Joints](#joints), and it is **two clauses, not one.**

### The app orders everything else

The plugin chains its own five Systems and exports their identity types; it names
neither `input` nor `scene` and adds no ordering against them, sitting in the
ordinary group, already after input's `First`.

- Gameplay that adds `Force` or moves Bodies: `Before[Integrate]`.
- The app's filter Systems: `After[Detect]().Before[Solve]()`, taking
  `*ecs.Write[*Contacts]`. Two filters serialise with each other; accepted. A
  filter that wants cp's order — PreSolve before the Islands are built — adds
  `Before[Sleep]()`; unordered against it, it runs on either side.
- Reaction Systems: after the filters. They may run alongside Solve and each
  other, except those reading this tick's Impulses, which must be after Solve.
- The render copy from `Position` into the app's own Transform: after physics and
  `Before[scene.RecordOnUpdate]`.

Ordering needs **no new vocabulary**: `Registrar.Subscribe`'s
`Before`/`After`/`First`/`Last` carry over, and "physics after input, before
rendering" is written the way scene's flush already writes itself.

### What a query and detection see within a tick

- **`BodyIndex`** matches `Position.Current` from Index until the next Integrate.
  Writes and spawns after Index in tick *t* are missing until Index in tick *t+1*.
  A query placed before Integrate is exact.
- **Detection goes through the Bodies in `BodyIndex`**, not a fresh Query, so a
  Body spawned after Index joins detection next tick from both sides and is never
  half-seen.
- **`StaticIndex`**: a Static Entity added or removed before Index in tick *t* is
  seen by Detect in tick *t*; one changed after Index is seen from *t+1*.
- **A Sleeping body stays in `BodyIndex`**, in a grid of its own that Index
  moves it into the tick after its Island falls asleep and out of the tick after
  it wakes. A query asks both grids, so between the two it finds the Body in one
  or the other and never in neither.
- **Any future pause must keep Index running.** Index runs on every
  `UpdateEvent`, the event static writers use, so `hooks.md`'s *"a reader runs as
  often as its writers"* holds. A pause that stops Index silently stops draining
  the hooks.

---

## The contact list

From [Contact events](https://github.com/dvoyni/cog/issues/402),
[Contact manifolds](https://github.com/dvoyni/cog/issues/407) and
[Collision filtering](https://github.com/dvoyni/cog/issues/406).

**One list, one entry per pair, marked Began, Continuing or Ended**, written once
a tick, always on. Solid Contacts and Sensor Hits share one type; Solve skips
Sensor entries. `Contacts` is a Resource the plugin registers, rebuilt each tick
reusing its buffer — never Entities, because spawning takes the frame-wide
`write{*Entities}` lock. Its contents persist until the next Detect, so a System
before Integrate legally reads the previous tick's.

**The entry is cp's arbiter, as one struct of 320 bytes:**

```go
type Contact struct {
    A, B            ecs.Entity      //  16
    Normal          m.Vec2d         //  16   B's surface, facing A
    SurfaceVelocity m.Vec2d         //  16
    Points          [2]ContactPoint // 240
    T               float64         //   8
    Friction        float64         //   8
    Restitution     float64         //   8
    gjkId           uint32          //   4
    Count           uint8           //   1
    Phase           Phase           //   1   Began, Continuing, Ended
    Sensor          bool            //   1
    flags           uint8           //   1   dropped, ignored
}                                   // 320 B

type ContactPoint struct {
    Point          m.Vec2d  // 16   on B's surface
    Depth          float64  //  8   overlap here
    NormalImpulse  float64  //  8   cp's jnAcc — carried across ticks
    TangentImpulse float64  //  8   cp's jtAcc — carried across ticks
    r1, r2         m.Vec2d  // 32   written at detection, as cp's Update does
    nMass, tMass   float64  // 16 ┐
    bounce, bias   float64  // 16 ├ rewritten every tick by PreStep
    jBias          float64  //  8 ┘
    id             uint32   //  4   which point this is, across ticks
    _              [4]byte  //  4
}                           // 120 B
```

A split into an app-facing entry and a parallel solver-private array — drawn
along cp's own *"survives the tick / rewritten by PreStep"* line — was put and
rejected. The scratch lives in the entry, and everything that follows is simpler
for it.

**The one substitution the ECS forces.** cp derives the public Contact point as
`r1 + bodyA.p`, because the arbiter holds `*Body`. The port's entry holds
`ecs.Entity`, so a method on it cannot reach a position: **`Point` and `Depth`
are stored, and `r1`/`r2` sit beside them.** That is 64 B a pair of cp's own
redundancy, kept deliberately — re-deriving `r1`/`r2` would cost two vector
subtractions a point inside `ApplyImpulse`, ten iterations a tick.

One thing falls out free: **`PreStep` reads `Depth` instead of recomputing
`dist`.** Detection already found it at the same Body positions — the step
integrates positions before it detects — so the two are equal by construction.

**Per-point ids are exact, not hashed.** cp's `hash` is a `uintptr` mixed from
*shape pointers* and vertex indices, and its match loop carries the comment
*"This could trigger false positives"*. The port needs none of it: `A` and `B`
already fix the two Shapes, so the id only has to tell one pair's at most two
points apart. **Two vertex indices packed into a `uint32`** does that exactly.

**Public, scratch and carried**, as three overlapping sets:

| | fields |
| --- | --- |
| the app reads | `A`, `B`, `Normal`, `Points[i].Point`, `.Depth`, `.NormalImpulse`, `.TangentImpulse`, `T`, `Count`, `Phase`, `Sensor` |
| the app writes | `flags`, `Friction`, `Restitution`, `SurfaceVelocity` — for one tick |
| crosses a tick | `NormalImpulse`, `TangentImpulse`, `id`, `gjkId`, the ignored flag — **and nothing else** |

### Which party is A, and one normal

`A` is the Sensor; otherwise the party that is not Static; otherwise the lower
Entity. Pairs are matched across ticks as *unordered* pairs, so a change of `A`
never breaks the phase. A method gives one party's view, so a reacting System
does not flip signs by hand.

Detection picks the *test* order by kind — circle < segment < poly, which the
nine-arm switch requires — then assigns A and B by the rule above and **flips
once at the boundary**: if A is cp's `a`, `Normal = −n` and `(r1, r2)` stay;
otherwise `Normal = +n` and they swap. One flip a pair a tick, one convention
thereafter.

**cp's `swapped` flag is deleted.** It exists in cp only to serve per-pair
handlers, which the port does not have — its filters are Systems walking a list.
Deleting it also deletes the wart in `TotalImpulse`, which returns its sum
negated unless `swapped`.

### Finding a pair again

**Two buffers as today, plus a swapped pair of open-addressed maps** from the
unordered Entity pair to a slot in that buffer. Detection inserts as it appends
and looks up the previous map — no extra pass, no sort, about two probes an entry
a tick. The full pair is on the entry, so a hashed key is verified rather than
trusted.

Warm starting rides this lookup: phases already required exactly it, so it adds
no cost. Rejected: sorting both buffers and merge-walking (about 50 µs at 2 000
entries against a 284 µs step); and cp's own persistent open-addressed table at
stable slots, which would give persistence for free but turns the app's two walks
a tick into an indirect walk over a table with holes — the contiguous walk is what
those two passes are paying for.

### Phases, and cp's persistence

cp fires Separate after one untouched tick and removes the arbiter after
`collisionPersistence`, so a pair that flickers apart and back within the window
keeps its Impulses. The list reports Ended after one tick, and that does not
change. Both hold if **the buffer carries three runs and the app sees two**:

```
[ current entries | ended entries | cached entries ]
                                   ^ the public view stops here
```

A **cached** entry has already reported Ended and is carried forward unreported,
purely as a carrier of Impulses, until it expires. The same map matches it, so a
re-touch inside the window warm-starts from it — cp's CACHED → FIRST_COLLISION
revival — and the app never sees it. The count is small: only pairs that
separated in the last two ticks.

- **An Ended entry keeps last tick's geometry**, carries **no reason** —
  separation, a despawned party, a removed `Shape` and a filter's drop all read
  the same — and may name a despawned Entity. A System finds that out by failing
  to look it up in a Store it already reads. **This departs from cp**, whose
  `Count()` returns 0 once an arbiter is CACHED, so its Separate callback sees no
  points at all.
- **A filter-dropped tick zeroes the Impulses.** Warm starting carries *the
  previous tick's solution*, and a tick with no solution has the solution 0. The
  alternative applies a sixty-tick-old Impulse when a filter changes its mind.
- **Material never carries.** It is recomputed from the two Shapes every tick as
  cp does, so **a filter's edit lasts exactly one tick**.

### What the filters may do

A filter **marks**; nothing is deleted or moved. There are **two marks**:

- **dropped**, for this tick. A dropped Began entry disappears — nobody saw it
  begin. A dropped Continuing entry becomes **Ended**, still skipped by Solve, so
  reacting Systems see the end. An Ended entry cannot be dropped.
- **ignored**, until the pair comes apart — cp's `arb.Ignore`, which one-way
  platforms need: PreSolve ignores the pair when the normal points the wrong way,
  and without it a Body halfway through gets shoved back out the moment its
  normal flips. While the pair keeps touching its entry arrives already marked,
  and Solve skips it as it skips a Sensor. Ignoring a Began entry means the pair
  is never seen at all and no Ended entry follows. It ends when the pair misses
  one tick.

Because the next tick compares against **what survived**, a reacting System always
sees a Contact begin, continue and end in that order, however a filter changes its
mind between ticks. One accepted difference from cp: a dropped Began entry comes
back as Began the next tick, where cp does not call Begin again.

### The material lives on the entry

`Friction`, `Restitution` and `SurfaceVelocity` are entry fields, filled by Detect
from the pair and writable by a filter for one tick. This is where cp's PreSolve
edits land: a filter can make one pair slippery for one tick without writing a
`Shape`, which is permanent and shared.

cp's combination rules port as written — `e = ea·eb`, `u = ua·ub`, both plain
products, and `surface_vr` is **A's surface velocity less B's** with its normal
component removed. cp writes it `b.surfaceV − a.surfaceV`, which is the same thing:
cp's normal points from its first Body to its second and this port's faces A, so
the port's A plays cp's b. `TestAFilterWritesASurfaceVelocityAndTheContactCarriesTheBodyAlongIt`
pins it that way round: a belt moving one way carries what rests on it the same
way. **Friction and Restitution ship on `Shape`**, defaults 0 and 0 matching
cp, as plain fields with documented ranges rather than constructors — the range is
cp's and cp does not validate either.

**Addition 1 closes here.** A solver with Friction fixed at 0 slides everything
forever, which is not *"cp's feature set, out of the box"*, and cp's own
`jtMax := friction * con.jnAcc` is dead code at 0 — the solver cannot be tested
without it.

**Addition 2 closes as a filter recipe, with no `Shape` field.** Surface velocity
changes exactly one thing: the friction solver drives the relative tangential
velocity to `−surface_vr` instead of to zero, and the Contact behaves as though
the surface were sliding under whatever rests on it — a conveyor, a travelator, a
wheel modelled as a circle that grips without spinning. It is properly physical:
the drag stays bounded by the Coulomb clamp, so a belt with no Friction carries
nothing.

Detect fills the entry's `SurfaceVelocity` with zero, and the package doc's
*Conveyors* passage is the recipe. The app puts a Component of its own on the
belt's Entity holding the belt's surface velocity, and a filter System ordered
`After[DetectOnUpdate]().Before[SolveOnUpdate]()` walks `Contacts` and, for each
Contact with a belt on either side, writes `SurfaceVelocity`: A's surface velocity
less B's, a party with no belt counting as zero, with its normal component
removed. The normal component must go because Solve adds the entry to the pair's
relative velocity *before* splitting it into normal and tangent, so what is left
along the Normal would push the pair apart or pull it together. The entry's
Friction is the product of the two Shapes', so the belt's Shape needs a Friction,
and so does whatever rides on it, or the filter writes the entry's Friction itself.
The filter's locks are the `*ecs.Write[*Contacts]` every filter takes and a read
of the app's own Component, so no physics System's lock set changes.
`TestAFilterWritesASurfaceVelocityAndTheContactCarriesTheBodyAlongIt` is the
recipe's proof: its filter is the recipe, over a belt that also runs into the
wall, and the Body leaves at the belt's tangential speed and no normal speed.

Why not the field. cp keeps a `surfaceV` on every Shape and combines it per pair
in `Arbiter.Update`; the port would pay **16 bytes on every `Shape` and on every
index entry's copy of it** — about 2 µs on the Index walk and 2.7 µs on a resting
step per 5,248 Shapes, scaled from
[#409](https://github.com/dvoyni/cog/issues/409) — and every circle and wall would
pay it for a conveyor that is rare. As a recipe, a scene with belts pays for them
in its own System and a scene without pays nothing. `Shape` stays at 104 bytes.
Decided by [#507](https://github.com/dvoyni/cog/issues/507), built by
[#326](https://github.com/dvoyni/cog/issues/326).

### What a reacting System reads

Both of cp's PostSolve figures are methods on the entry, computed from data it
already holds:

- **`TotalImpulse`** = `Σ rotate(Normal, (jn, jt))` over the points.
- **`TotalKE`** = `Σ eCoef·jn²/nMass + jt²/tMass`, with `eCoef = (1 − e)/(1 + e)`.

`TotalKE` **does not exist in `jakecoffman/cp`** — it is `cpArbiterTotalKE` in
**C**, so shipping it is porting from the C, which the porting rule allows and
which is stated here rather than discovered later. **Addition 6 closes.**

**Before Solve the Impulses are last tick's; after Solve they are this tick's.**
That is what warm starting means, and it is useful rather than a hazard — a filter
can ask how hard the pair was hit last tick — but it is a contract, so it is
written down.

### Ordering, and memory

A Sensor's entries sit together, ordered by `T`; Ended entries come after every
current entry; nothing else is ordered. **A System walks the list; there is no
per-Entity index.** *"What is this Body touching now"* is a walk with the
one-party view. Adding `Of(e)` later is purely additive, for when an app measures
the walk as too slow.

The two buffers keep their largest size and answer **a plugin shrink command
modelled on `ecs.ShrinkCmd`**, with an opt-out per area. Cached entries hold
memory for the persistence window, are invisible to the app, and join the shrink
command's areas. The two index grids grow and shrink the same way. What the
command reports for the indices is **exact for the Body index**, which holds
nothing but slices, and **a floor for the static index**, whose Entity-to-slot
table is a Go map: Go publishes a map's length and never what its buckets cost.
**The solver's scratch shrinks too**, the fifth area: the gather Solve refills from nothing every
tick — the solved list, the slot table, the Body and Joint rows — and the swept
Sensor Probe buffer with the Body slot beside each Hit. None of it is read across a tick, so the command releases it
whole and changes no answer; the slot table is sized to the largest Body slot
detection ever saw, which after a 100 000 Body spike is 400 KB, and up to twice
that with the slack it grows by, that nothing else would ever give back. The
sleeping Islands' records, members and quiet Contacts are Contact-list buffers and
are packed with them; the Island build's scratch is solver scratch and is released
with it. The command is the one physics handler that is not a
System, so it declares itself exclusive on its own: its three write locks already
serialise it, and the declaration is against those locks ever narrowing.

### Which pairs are reported

**Every pair the groups let collide, whatever the Body kinds** — Kinematic against
Static, Kinematic against Kinematic, anything against a Sensor. Static against
Static is never tested. Unity's default of dropping Kinematic pairs is rejected:
the groups decide what collides, and kind does not overrule them. So touch damage
reads the same list response does.

### Sensors, and what keeps a point from tunnelling

The package **has no projectile concept**. A projectile is an ordinary Dynamic
body with a circle `Shape` marked a Sensor; drag, homing Force and inherited
velocity all come from the integrator.

**Every moving Sensor with a circle Shape is Probed once per tick, from
`Position.Previous` to `Current`**, with no opt-in flag. A swept circle's Hits are
a superset of a discrete Overlap's, since what it overlaps at the start is Hit at
`T = 0` and what it overlaps at the end is Hit before `T = 1`. Its Contacts are
its Entity plus a `Hit`, ordered by `T`, so the app takes the first and stops at a
wall.

- Box and segment Sensors are tested discretely; a fast one can miss.
- A Static Sensor is never Probed.
- An app that teleports a Sensor sets `Previous = Current`, as render
  interpolation already requires.
- **Snap-back is the app's write.** The plugin never moves a Sensor back, and
  never stops one. The app reads the Contacts and writes
  `Position.Current = Previous`, then explodes, reflects or expires it.
- A Sensor's path is **a chord, not the polyline**, so a sharply curving Sensor
  can clip a corner within one tick.
- **Two moving Sensors are tested against each other's end positions**, not their
  relative motion, so two Sensors with a radius crossing within one tick can miss
  each other. When both find each other, the Hit with the smaller `T` is kept.

**`T` and `Depth` mean one thing on every entry.** For a Probed Sensor, `T` is the
Probe's and `Depth` is 0, except for a Probe that started inside something, which
reports `T = 0` with the overlap at the start. Everything else is found where the
tick ended: `T = 1` and `Depth` is the overlap there.

---

## Joints

From [Joints](https://github.com/dvoyni/cog/issues/318).

**A Joint is its own Entity carrying one kind-discriminated `Joint` Component of
120 bytes** — the same shape `Shape` uses, which is a five-kind union rather than
five Component types. It is a separate Entity rather than a Component on one of
the Bodies because **a Body can carry several Joints** and a Component is one per
Entity. This is the first place the Reference vocabulary is load-bearing:
References arrive with Joints, between Bodies, never between a Body and its Shape.

| part | bytes |
| --- | ---: |
| `A`, `B` References | 16 |
| kind + `collideBodies` + padding | 8 |
| `MaxForce`, `ErrorBias`, `MaxBias` | 24 |
| parameter union — widest is the Spring: two anchors, rest length, stiffness, Absorption | 56 |
| accumulated Impulse, an `m.Vec2d` — pivot and groove need two components | 16 |
| | **120** |

Groove's normal is **not stored**: cp caches it in its constructor and never
changes it, so the port derives it at gather time for one normalize a tick, and
the union stays at 56 rather than 64.

**Ten Component types were put and rejected.** A simple motor would be 64 bytes
instead of 120, an app could query one kind directly, and a limit could never be
read off a pin. Against that: ten Stores, ten Queries inside Solve, ten lock
entries, and a Joint's kind becoming a structural change instead of a Component
write. `Shape` already made this trade once and the port stays consistent with it.
Type safety comes back the way `Shape`'s does — **per-kind constructors and
per-kind accessors**.

### All ten ship, and the two function pointers do not

All ten — pin, slide, pivot, groove, damped spring, damped rotary spring, rotary
limit, ratchet, gear, simple motor. They share one gather, one dense row and one
pass, and each is 60–100 lines of pure arithmetic; dropping any saves nothing
structural and costs the destination's *"cp's feature set, out of the box"*.

**But two do not port as written.** `DampedSpring.SpringForceFunc` and
`DampedRotarySpring.SpringTorqueFunc` are **function pointers in what would be
Component data**, and a Component holds no pointers, transitively, enforced at
registration. There is no form they survive in: not as an index into a registry,
which is a `Uses` dispatch at 1113 ns against 0.49 ns for a direct call; not as a
Reference.

So the port keeps **cp's linear law only**: `(RestLength − dist) · Stiffness` and
`(relativeAngle − RestAngle) · Stiffness`. An app wanting a nonlinear Spring
writes `Force` from its own System, which is nearly the same computation.
**They differ in one way, recorded rather than corrected**: cp applies the Spring
as an Impulse inside `PreStep`, *before* velocity integration, so it is multiplied
by the Body's damping in the same tick; a `Force` write is added *after* that
multiply. At 15 /s and 60 Hz the factor is `exp(−0.25) = 0.779`, so the Spring
loses **22% of its Impulse in the tick it is applied**. This is cp's behaviour and
the port inherits it.

### Where the solver state lives

**Only two things cross a tick:**

| | where |
| --- | --- |
| the accumulated Impulse, which warm start needs | **the Component**, one `m.Vec2d` |
| **ratchet's `Angle`**, which cp mutates inside `PreStep` as the ratchet clicks over | **the Component** — a public parameter, gameplay-visible by nature |
| everything else — `r1`, `r2`, `k`, `nMass`, `bias`, the groove normal, `clamp`, `targetVrn`, `vCoef` | **the frame-local dense Joint row**, thrown away when Solve returns |

This **overturns** the reading, carried into the Joints ticket from
[Contact manifolds](https://github.com/dvoyni/cog/issues/407), that a Joint
Component carrying its own `PreStep` scratch is the consistent shape. The two
cases do not share the one thing that matters: **a Contact entry is solver-owned
data inside a Resource nothing else writes; a Joint Component is app-owned data
the app writes and reads.** Putting roughly 120 bytes of solver litter into a
value gameplay edits, all of it recomputed from scratch every tick anyway, is the
wrong trade. It is the same place the bias velocities went, for the same reason.

`GetImpulse` then needs no new mechanism: **`Joint.Impulse()` is a method on the
Component**, exactly as `TotalImpulse` is a method on the Contact entry.

**Two kinds are irregular.** The damped spring and the damped rotary spring **do
not warm start** — their `ApplyCachedImpulse` is empty and their accumulated
Impulse is rebuilt in `PreStep` — so for those two the stored field is a readout
only.

### Cost, and a missing Body

The References are followed **once**, at gather; the twelve passes index the dense
array. At 500 Joints: one Query walk of about **4.5 µs** at the re-taken 9 ns an
Entity — 1.7 µs on the float32 figure this was first written with — a
dense row of about 128 bytes, and 64 KB streamed twelve times — against a 284 µs
step at N=1024.

Three cases, all decided **once at gather** and never inside an iteration:

- **A Reference misses** — the Body was despawned. The Joint is **skipped and its
  stored Impulse zeroed**. The plugin does not despawn the Joint: structural
  change during the step is forbidden, and a dangling Reference is the app's to
  clean up as it is anywhere else. `Impulse()` reading 0 is how an app notices.
  cp asserts both bodies are non-nil.
- **The Body is Static or Kinematic.** The ordinary *anchor to the world* case,
  and it must work. The gather gives it a zero-inverse-mass row and never writes
  velocity back — exactly what a Contact against a wall already does.
- **Both Bodies are non-Dynamic.** Skipped; the guard below.

**Richer reporting was rejected** — no diagnostic list, no counter Resource. A
missing Reference is a programming error with silent, well-defined behaviour.

### The `k = 0` guard is two rules, and the second is new

**For `k_scalar` and `k_tensor` the answer is the Contacts' answer, and provably
so.** Both build `k = m_sum·I + A`, where `A` is a sum of positive semi-definite
outer products, so `k` is non-singular whenever `m_sum > 0`. Since **`Dynamic`
rejects infinite mass**, `m_sum > 0` is exactly *at least one Body is Dynamic*.
That covers pin, slide, pivot, groove and the damped spring — including the damped
spring's own `"Unsolvable spring"` assert, which is a second assert the Joints
ticket did not know about until it read for it.

**It does not cover the five angular kinds, and nothing else does either.** Four
of them compute `iSum = 1/(a.i_inv + b.i_inv)`; the gear is ratio-weighted,
`1/(a.i_inv·ratio_inv + ratio·b.i_inv)`, **which changes the arithmetic and not the
hazard** — with both inverse moments zero the denominator is still zero whatever
the ratio. *The plain-sum form was the reading carried on the record; the gear's
variant is corrected here against v2.4.0.* An **infinite moment is legal** — it is
*a Body that does not turn*. Two such Bodies give `iSum = +Inf`, an Impulse clamped
to an infinite `jMax` since `MaxForce` defaults to ∞, and then **`Inf · 0 = NaN`**
written straight into angular velocity.

> **The guard, checked once at gather and never inside an iteration:** a Joint is
> solved only if **at least one Body is `Dynamic`**, and — for the **damped rotary
> spring, rotary limit, ratchet, gear and simple motor** — only if **at least one
> Body has `invInertia > 0`**.

**This is a defect in cp's Go port, and it is not among the porting index's
seven.** The index records that **C** *additionally asserts `moment != 0` in the
rotary spring*: so Chipmunk catches one of the five and `jakecoffman/cp` catches
none — the assert was dropped in translation. The port's guard covers all five,
silently, without asserting.

### `collideBodies` stays the plugin's

cp walks the Body's intrusive constraint list per candidate pair. The port has no
such list and will not grow one. **Index — which already drains the static hooks
and rebuilds `BodyIndex` — also walks the `Joint` Query and builds
`JointedPairs`**, a set of Entity pairs whose Joint says *do not collide*, using
the open-addressed pair map the Contact list already needs. **Detect checks it
after the bit filter and the bounding-box test**, so the Contact is **never
created and never reported**, which is what cp does.

Gated on `len(JointedPairs) == 0`, so a scene with no such Joint pays one branch.
In use: roughly 400 lookups on a busy N=1024 tick, about **4 µs**.

**Handing it to the app's relationship filter was put and rejected.** It is the
consistent reading of the rule that everything depending on a particular pair
stays with the app — but a ragdoll is unusable without this, so making every app
that spawns a pin Joint write a filter System *and its own pair-lookup structure*
is a real regression against *"out of the box"*. The flag is also not a game rule
about a particular pair; it is a property of the Joint, which is plugin data.

### Units, and the name clash

cp's constraint constants are already per second and already SI.

| cp | the port |
| --- | --- |
| `errorBias`, stored as 0.9⁶⁰ | **the same 6.32 /s** as the collision bias — byte for byte the same number, re-spelled |
| `maxForce`, ∞ | **newtons**, unchanged; used as `maxForce · dt`, an Impulse ceiling |
| `maxBias`, ∞ | **metres per second**, unchanged; radians per second for the five angular kinds |
| Spring stiffness | **N/m**; **N·m/rad** for the rotary Spring |
| Spring damping | **N·s/m**, renamed **Absorption** |

All three of `MaxForce`, `ErrorBias` and `MaxBias` stay **per Joint**, not plugin
configuration: `MaxForce` is how a breakable or limited Joint is expressed and
`MaxBias` is how cp makes a Joint soft. Their *defaults* come from the settings
struct.

**`Absorption`** is the glossary's answer to a three-way clash: **Damping** is a
Body's decay rate in 1/s, **Friction** is a Contact's ratio, and cp's Spring
`damping` is a force per unit velocity — a third thing. Rejected: `Damper`, one
letter from Damping when keeping those apart is the glossary's whole job; and
Box2D v3's hertz-and-damping-ratio reparameterisation, which is better to author
but is a departure from cp and needs mass to convert.

### Constructors take values

**cp's constructors read live body state; the port's cannot** — a cog constructor
is a free function over values with no Store access. Three of cp's read through
body pointers, and each gets a pure helper beside it:

| cp | reads | the port |
| --- | --- | --- |
| `NewPinJoint` | both world anchors, to set the distance | a pure helper returns the distance |
| `NewPivotJoint` | `WorldToLocal` on both, to split a world pivot | `PivotAnchors(posA, angleA, posB, angleB, worldPivot)` |
| `NewRatchetJoint` | `b.a - a.a`, the initial `Angle` | the app passes it |

The app has those arguments in hand at spawn time, and *geometry stays pure as
code: free functions over value types* is already the rule.

---

## Sleeping and Islands

From [physics: bodies that stop moving go to sleep](https://github.com/dvoyni/cog/issues/316),
decided on [physics: follow-up](https://github.com/dvoyni/cog/issues/315), and
[physics: gravity](https://github.com/dvoyni/cog/issues/320), which it waited on.

**Bodies that have been idle long enough stop being integrated, indexed, detected
and solved, and wake when something disturbs them.** It is cp's
`ProcessComponents`, **off by default** as cp's is: an app that never turns it on
gets exactly the step it had before, with no Island built.

**Why it earns its place.** The port runs cp's impulse solver, so under gravity
every resting Contact of a pile is detected and solved every tick. A sleeping
Island is out of the Integrate walk, out of the Body index rebuild, out of
detection against itself and the statics, and out of Solve; what is left of it a
tick is a walk over its Bodies comparing what they carry.

### Settings

```go
type Sleep struct {
    IdleSpeed float64 // m/s
    Time      float64 // s
}
```

- **A Resource the plugin registers at its zero value, which is off.** `Time` 0
  is off, the zero-value spelling of cp's `SleepTimeThreshold` of `INFINITY`,
  cp's default; an infinite `Time` is off as well. An app writes it through
  `ecs.Write[*Sleep]`, once from `app.InitEvent` as a rule. Turning it off again
  wakes every Island, rather than leaving them asleep with nothing left to wake
  them, which is what cp's own loop would do.
- **Idleness is cp's.** A Dynamic body is idle on a tick when `v·v·m + w²·i` —
  cp's `KineticEnergy`, which has no ½, with its guard against `0·∞` for a Body
  that does not turn — is at most `m·IdleSpeed²`, and then its idle time grows by
  `h`; otherwise it is 0. An Island falls asleep when **every** member's idle time
  has reached `Time` (cp's `ComponentActive`).
- **`IdleSpeed` 0 falls back to cp's estimate from gravity**, `|g|²·h²` with `g`
  read from `Constants` each tick. A world with zero gravity and `IdleSpeed` 0
  therefore never idles: a game that writes its gravity into `Force` names an
  `IdleSpeed` itself.

### Marking: a Tag, and what the plugin keeps beside it

**A sleeping Body carries the `Sleeping` Tag**, which the plugin adds and removes
and nothing else may. Integrate, the Body index rebuild and the velocity
integration skip it with `Without[Sleeping]`, and an app's Queries can do the
same. Each of the three names its walk a second time without the filter and
takes that one on a tick when no Body sleeps, for what the filter costs a walk
(*What it costs, and where it pays*); the two walks lock what the filtered one
alone does. A Tag was once suspect because adding one was thought to take the
frame-wide `write{*Entities}`; it takes `write{*Store[Sleeping]}` and nothing
else (`ecs.md`'s *Structural change*), and
only the sleep System writes that Store.

What sleeping has to remember per Body — cp's `sleepingIdleTime`, the `Force` it
compares against, and for a sleeper the `Position` and `Velocity` it left in it
and the Island it sleeps in — is a plugin-private Component, `Rest`, added the
first tick sleeping is on. It is a Component and not a table in a Resource
because the sleep System reaches it from the Entity a Contact or a Joint names,
which is one load of a sparse array where a table keyed by Entity is a hash, and
because a despawn empties it with every other Store. **Nothing is mirrored**:
what a sleeper's `Position` and `Velocity` were is what they are compared
against, and they stay the source of truth.

### The sleep System

**A fifth System, between Detect and Solve, where cp's `ProcessComponents`
runs.** It keeps each awake Dynamic body's idle time, wakes every Island
something disturbed, builds the tick's Islands, and puts to sleep those that
have been idle for `Time`.

**Islands are a union-find over the tick's awake Dynamic bodies**, joined along
the Contact list and the Joints — cp flood-fills each Body's intrusive lists,
which the port does not have. The buffers are kept across ticks, so the build
allocates nothing. As cp: a Static or Kinematic body never joins an Island nor
bridges two; a Contact or a Joint with a Kinematic body keeps its Island awake;
a Sensor Contact neither joins nor wakes, and neither does one a filter dropped
or ignored.

**Its locks are Solve's, and the Stores and Resources sleeping adds.** It reads
`Sleep`, `Constants`, `Dynamic`, `Velocity`, `Position` and `Joint`, and writes
`Force`, `Contacts`, the `Sleeping` and `Rest` Stores and the `WakeCmd`'s queue.
Every lock but the new ones is one Solve — the next link of the chain — already
holds as strongly, so anything that could run beside the chain before still can.
Integrate, Index and Solve each gain `read{*Store[Sleeping]}`, a Store only the
sleep System writes; Detect gains nothing. `TestSleepingCostsNoSystemParallelism`
reads the sets off the engine's description and holds all of it.

**It reads no index**, which is what keeps it inside Solve's locks. The two
things it needs from them Detect writes into the Contact list, holding both
indices for read and the list for write already: where the sleepers' slots start,
and which quiet Contacts name a Static that has left the static index.

**App filters and the sleep System both write `Contacts`.** cp runs PreSolve
before `ProcessComponents`, so a Contact it rejects neither joins nor wakes an
Island; a filter that wants that adds `Before[SleepOnUpdate]()`. One ordered only
`After[DetectOnUpdate]().Before[SolveOnUpdate]()` runs on either side of it, and
the difference is one tick: which of this tick's drops the Islands see, and
whether the filter sees the Contacts a waking Island hands back. **Left unordered
it also costs an allocation on the ticks the sleep System is the one kept
waiting**: the kernel's dispatch builds a map of the blocked request's resources,
which spills to the heap past eight, and the sleep System has twelve. That is the
kernel's dispatch line rather than the step's, and it moves with contention, not
with the Body count; the test harness orders its filter before the sleep System,
which is cp's order and takes the race away.

### Waking

The port has no setters to hook — cp's `SetPosition`, `SetVelocity`, `SetForce`
and a dozen more each call `Activate` — so waking compares values. **An Island
wakes, all of it, on the sleep System's next run, when:**

- **an awake Body touches a sleeper** (cp's arbiter loop), or is jointed to one.
  A sleeper is only ever detected against an awake Body, so any solid Contact
  naming one wakes it;
- **a sleeper's `Velocity` or `Position` differs from what the sleep System left
  in them**: a kick, an impulse, a teleport;
- **its `Force` differs from the one it last spent before falling asleep.** The
  sleep System clears a sleeper's `Force` every tick — Solve does not reach it —
  so gameplay that adds `m·g` every tick shows the same value every tick and
  disturbs nothing, where cp's every force write wakes (`body.go` :331, :342,
  :497). On an awake Body a `Force` differing from the previous tick's resets
  its idle time, so a Body leaning on a wall under changing input never stutters
  between asleep and awake;
- **`Constants.Gravity` changes**, which wakes every Island, as cp's `SetGravity`
  does: the sleep System keeps the gravity it last saw and compares;
- **a support is removed**: a member despawned wakes the rest of its Island, and
  a Static leaving the static index wakes every Island whose quiet Contacts name
  it (cp's `RemoveShape`, `ActivateStatic`);
- **a `WakeCmd` names it** — the way for everything else, a `Shape` or a
  `Dynamic` changed while it sleeps among them. It follows `ShrinkCmd`: a
  Command the plugin registers, self-exclusive, whose lock is a write on its own
  queue and nothing wider.

**A sleeper gathers no gravity.** It is not velocity-integrated while it sleeps,
as in cp, so the tick it wakes it receives exactly one `g·h`, not one per tick
slept. Its `Velocity` is left as it fell asleep, which cp does too; nothing zeroes
it.

### Contacts

**A sleeping Island's Contacts go quiet, as cp's do**: they leave the current run
on the tick it falls asleep, and are carried past the public view with no expiry,
neither Continuing nor Ended. They are carried in a run of the Island's own
rather than in the per-tick cached run, which would copy every quiet Contact
every tick for as long as the pile sleeps. **On waking they come back Continuing,
with their warm-start Impulses kept**, into the current run, and Solve warm-starts
them that tick — their parties have not moved, so the geometry they carry is still
true. Two do not come back that way, and come back **Ended** instead: one whose
party has gone, despawned or out of the static index, and one whose party the app
moved while it slept, whose geometry names a place it has left. cp re-detects its
woken arbiters before it solves them again, which is the same answer.

Which Contacts go quiet is exactly the ones nothing will test while the Island
sleeps: every solid Contact whose parties are all asleep or Static. A Sensor
Contact never goes quiet, and one between a sleeper and an awake Sensor is still
found and reported every tick; one between two sleepers, or a sleeper and a
Static, is not tested and ends, as cp's does.

### The Body index keeps sleepers in a grid of their own

**The Body index holds sleepers in a second grid, updated when an Island falls
asleep or wakes and not rebuilt every tick.** Index moves a Body in and out on
the next tick, off the `Sleeping` Tag's own Hook; the rebuild skips it with
`Without[Sleeping]`. Detect tests every awake Body against it and never tests it
against itself or the static index. **A query on the Body index asks both grids,
so its answers do not change**, and **the static index never holds a sleeper.**

cp moves a sleeping Body's Shapes into its static tree. The port's two indices
are public and the caller chooses which to ask, so doing the same would make an
explosion's `Overlap` on the Body index miss sleeping crates and a line-of-sight
Probe on the static index hit them.

The Body index has no Entity-to-slot table ([#441](https://github.com/dvoyni/cog/issues/441)),
so taking sleepers out is one pass over the sleepers' grid however many leave,
and the solver numbers the sleepers' slots after the awake grid's. A Body woken
this tick is still in the sleepers' grid, and the Contacts it hands back are
solved at a slot the sleep System numbers past every one Detect did.

### What it costs, and where it pays

Measured interleaved: every round ran each variant once, alternating, eight
rounds, the median quoted; `go1.27.1 windows/amd64`, AMD Ryzen 9 7950X3D,
GOMAXPROCS=32, with other builds sharing the machine, so the absolute numbers sit
above the ones quoted elsewhere here and only the comparisons are claimed. The
piles are `BenchmarkTheSettledPile`: stacks of four half-metre crates under the
plugin's gravity, each stack an Island.

| | sleeping off | sleeping on | |
| --- | ---: | ---: | --- |
| a settled pile, N = 256 | 450.8 µs | **72.7 µs** | 6.2× |
| a settled pile, N = 1 024 | 1 570.2 µs | **116.6 µs** | 13.5× |
| the reference scene, nothing ever sleeping, N = 256 | 126.9 µs | 127.8 µs | +0.9 µs, +0.7% |
| the reference scene, nothing ever sleeping, N = 1 024 | 420.1 µs | 432.1 µs | +12.0 µs, +2.9% |

The last two rows are `BenchmarkTheIslandBuild`: the reference scene of
`BenchmarkTheStep`, Bodies under a Force and Contacts that never settle, with
sleeping on at a `Time` nothing reaches, which is the Island build's whole cost
on a tick where nothing sleeps.

**The break-even is about thirteen Bodies**, for a settled pile. It is taken from
the two sizes rather than measured at it: off costs about 1.46 µs a crate over a
fixed 76 µs, on about 0.06 µs a crate over 58 µs, and the two lines cross near
N = 13. Past that, a settled pile is cheaper asleep; a scene where nothing
settles pays the Island build, a few per cent.

**Sleeping off costs the fifth System's dispatch, and after
[physics: sleeping off costs the step 7–11%](https://github.com/dvoyni/cog/issues/541)
nothing else that measures.** It was first recorded against `main` before
sleeping at 110.9 → 123.5 µs for `BenchmarkTheStep` at N = 256 and 382.6 →
410.3 µs at N = 1 024. Profiled before anything changed, against the physics from
before sleeping compiled over the same ECS, the cost sat in three places, and the
sleep System's own work was none of them: its off path returns before it builds
anything and does not show in a profile, and neither do Detect's two recorded
facts nor the `Rest` Store.

- **The `Without[Sleeping]` filter.** A filter is a field of the Query like any
  other, and `Query.All` picks its walk by field count: it folds the range body
  into the walk at two fields whatever the body's size, at three only while the
  body is small, and past three not at all. The filter took Integrate's walk from
  two fields to three and the Body index rebuild's and the velocity integration's
  from three to four, and every Entity paid an indirect call: at N = 1 024
  Integrate went from about 10 µs a tick to 18 µs and Index from 63 µs to
  75 µs. Each of the three now names its walk twice, with the filter and
  without, and takes the unfiltered one while `NobodySleeps` — a
  `With[Sleeping]` Query that stops at its first Entity — says the filter would
  drop nothing. The pair
  names no Store the filtered walk did not, so the lock sets are unchanged and
  `TestSleepingCostsNoSystemParallelism` passes as it was. The same answer lets
  the Joint gather skip its two `Sleeping` probes a Joint.
- **The swept Sensors' Probe of the Body index**, which went through the index's
  two-grid `probeAllSlots`. Asking the awake grid directly while nothing sleeps
  measured 3% of the reference step at N = 1 024; the profile puts less than that
  in the sweep itself, so the number is quoted and the cause is not.
- **The fifth System's dispatch**, about 6–8 µs a tick and flat in the Body
  count. Built with the sleep System unsubscribed, the step matched the physics
  from before sleeping within 0.5% at both sizes, and a sleep System locking one
  Resource cost what the real one does, so the lock set does not enter into it:
  it is the kernel's dispatch of one more link in the chain. It is not removable
  from physics without widening a lock set — the sleep System folded into Solve
  would give Solve both indices to read and `Sleeping`, `Rest` and the
  `WakeCmd`'s queue to write, and an app's Probe of the Body index could no
  longer run beside it — and registering the System only when sleeping is
  configured on would break turning sleeping on between ticks.

Measured after, interleaved: ten rounds, each running every binary once, the
order reversed every other round, the median quoted with the minimum beside it;
`go1.27.1 windows/amd64`, AMD Ryzen 9 7950X3D, GOMAXPROCS=32, with another build
benchmarking on the machine at the same time. *Before* is `main` before sleeping
(82b84dc). *Reference* is that physics compiled over today's ECS, which is what
the step would cost without sleeping now: it differs from *before* by the ECS
work merged since, which a world pays whether or not physics sleeps. *Was* is
`main` at 1f96299, and *now* is the step after this change.

| | before | reference | was | now | now against reference |
| --- | ---: | ---: | ---: | ---: | --- |
| `BenchmarkTheStep`, N = 256 | 103.6 µs | 109.1 µs | 122.0 µs | 116.7 µs (min 115.0) | +7.6 µs, +7.0% |
| `BenchmarkTheStep`, N = 1 024 | 361.9 µs | 369.0 µs | 404.6 µs | 376.7 µs (min 372.6) | +7.7 µs, +2.1% |
| `BenchmarkThePolygonStep`, N = 256 | 386.4 µs | 393.2 µs | 405.2 µs | 401.5 µs | +8.3 µs, +2.1% |
| `BenchmarkThePolygonStep`, N = 1 024 | 1 437.1 µs | 1 438.7 µs | 1 475.2 µs | 1 457.2 µs | +18.5 µs, +1.3% |
| `BenchmarkTheJointedStep`, N = 256 | 130.1 µs | 135.3 µs | 149.0 µs | 141.8 µs | +6.5 µs, +4.8% |
| `BenchmarkTheJointedStep`, N = 1 024 | 469.1 µs | 470.7 µs | 515.6 µs | 479.6 µs | +8.9 µs, +1.9% |

What is left is the fifth System's dispatch, and it is recorded as a finding
rather than argued away: about 7 µs a tick, 2% of the reference step at
N = 1 024 and 7% at N = 256, against an order of magnitude on a pile that
sleeps. The polygon scene's 18.5 µs at N = 1 024 is 1.3%, inside that
benchmark's noise as it was when first recorded. Taking the dispatch out is a
decision this finding does not make: it needs either a cheaper dispatch per link
from the kernel or a lock set wider than the rule allows.

Sleeping on costs what it did before this change, measured the same way against
the build that brought it (29ab6f0) and against `main`. The Island build on a
tick where nothing sleeps takes the unfiltered walks too, so it got cheaper:

| | 29ab6f0 | was | now |
| --- | ---: | ---: | ---: |
| a settled pile asleep, N = 256 | 62.0 µs | 66.9 µs | 67.0 µs |
| a settled pile asleep, N = 1 024 | 110.1 µs | 113.7 µs | 114.3 µs |
| the Island build, nothing sleeping, N = 256 | 117.6 µs | 125.6 µs | 120.2 µs |
| the Island build, nothing sleeping, N = 1 024 | 408.8 µs | 421.3 µs | 392.5 µs |

The settled pile sits where `main` had it. The 4–5 µs both sit above 29ab6f0 is
not physics, whose code between the two differs only by the Systems' names, but
the ECS work merged since; it was not profiled here.

---

## The queries and the indices

From [The query surface](https://github.com/dvoyni/cog/issues/306),
[prototype: what a sweep costs per call on each index](https://github.com/dvoyni/cog/issues/345),
[Static geometry as Entities](https://github.com/dvoyni/cog/issues/288) and
[Collision filtering](https://github.com/dvoyni/cog/issues/406).

**Two layers, both exported.** Exported because a replacement solver lives in
another package and is built from exactly these.

**Pair primitives** — free functions over values, touching no engine state. Each
takes an extra `verts []m.Vec2d`, nil for every kind but `Poly`:

| Primitive | Returns |
| --- | --- |
| `ProbeShape(from, to m.Vec2d, radius float64, shape Shape, at m.Vec2d, angle float64, verts []m.Vec2d)` | `(Hit, bool)`, `Hit.Entity` zero |
| `Penetration(a Shape, atA m.Vec2d, angleA float64, vertsA []m.Vec2d, b Shape, atB …)` | `(normal m.Vec2d, depth float64, ok bool)` |
| `ClosestPoint(p m.Vec2d, shape Shape, at m.Vec2d, angle float64, verts []m.Vec2d)` | `m.Vec2d` |

There is no `Overlaps` boolean: `Penetration`'s `ok` is it. The names differ from
the index methods on purpose, so a free function never reads as a world query.

**World queries** — methods on two index Resources, reached through
`ecs.Read[*StaticIndex]` / `ecs.Read[*BodyIndex]`. **Nine queries are three
methods**, each taking one `exclude ecs.Entity` plus the groups it is in and the
groups it looks for:

| Method | Covers |
| --- | --- |
| `Probe(from, to, radius, bits, collidesWith, exclude) (Hit, bool)` — the first Hit | first-hit; **line of sight is its bool**; a swept hop |
| `ProbeAll(dst []Hit, from, to, radius, bits, collidesWith, exclude) []Hit` — every Hit, **ordered by T**, appended | all hits; looking *past* a Hit |
| `Overlap(dst []ecs.Entity, shape Shape, at, angle, verts, bits, collidesWith, exclude) []ecs.Entity` — appended, **unordered** | what a Shape touches; a point query is a circle of radius 0 |

- **Nearest is not a function**: `Overlap`, then a loop in the app keeping its own
  predicate and the minimum `ClosestPoint` distance. A predicate parameter is a
  func value and allocates.
- **Looking past a Hit is `ProbeAll`**: Hits come back in order and the app stops
  where its own rule says. *"Such things are simply not colliders"* was rejected,
  because it fails the moment the thing that lets sight through must still stop
  movement.
- **Why `exclude`.** A Probe starting inside something reports a Hit, and a Body
  is in `BodyIndex`, so a Body Probing would Hit **itself** first every time. One
  excluded Entity is an identity, not a category. Excluding a second party stays
  the app's, through `ProbeAll` or its filter System.
- **No any-hit query.** An early-out saves about 4 ns of 45 on the one
  high-volume boolean. Measured, and it does not earn a place.
- **An iterator was rejected**: ordering by T needs scratch, and a Resource shared
  by concurrent readers cannot hold scratch. The caller's slice is the scratch.

### `Hit`, and what a Probe means

`Hit{Entity ecs.Entity; T float64; Point, Normal m.Vec2d}`. `Overlap` returns
Entities only, having no first contact to describe.

- **T is a fraction** of `from → to`, in [0, 1], carrying no unit. Snap-back is
  `from + (to − from)·T`.
- **`Point` is on the hit Shape's surface** — grown by the Probe's radius where
  the Shape is rounded. The Probing circle's centre at T is computable from T; the
  surface point is not.
- **`Normal` is a unit vector facing the Prober.** For a segment it is whichever
  side the Probe came from. It is what reflection negates against and what
  snap-back moves along. On a Polygon it is the face normal on a face and the
  outward normal at the corner on a rounded vertex.
- **A Probe that starts overlapping reports a Hit at `T = 0`**, with the normal
  from the nearest surface point. Ignoring the Shape it starts inside (Box2D's ray
  cast) was rejected: a projectile would leave a wall it spawned in, and spawn
  validation would pass silently.
- **A zero-length Probe** is legal and behaves as an Overlap that reports a `Hit`.
- **Growth by the radius is exact**: a circle Probed against a box meets a rounded
  rectangle, not a larger box with square corners. cp's closed forms give this for
  rotated Polygons too — the segment query offsets each face plane outward by the
  summed radius, confines the Hit to that face by a cross-product span test, then
  sweeps a circle against each vertex when the radius is non-zero. **Exact, and
  nothing allocates.**
- **A point never Hits a point.** A radius-0 Probe against a radius-0 circle, or
  along a segment, has zero area. Projectiles do not Hit each other unless the app
  gives one a radius. Nothing tries to fix this.
- **`Overlap` counts as touching exactly what Contact detection counts**, so a
  segment Probe never Overlaps a segment Shape. A crossing is a question for
  `Probe`.
- **`Penetration` on coincident centres returns a zero normal with `ok = true`.**
  Choosing a direction is the caller's; a pure function takes no randomness.

### No duration, anywhere

Nothing on the query surface takes a duration or a velocity. A Probe takes
positions and the caller computes `to = p + v·h`. The step size is Solve's, so a
tick-rate hazard cannot reach a query.

### Two indices, and the split is visible

**`StaticIndex`** holds Entities with the `Static` Tag; **`BodyIndex`** holds every
other Entity with a `Shape`, Kinematic and Dynamic alike. Shapeless Bodies are in
neither. **Which index a query asks is the caller's choice** — line of sight
against static geometry is a Probe on `StaticIndex`, "versus Bodies" is a Probe on
`BodyIndex`. The locks stay apart: rebuilding the Body index write-locks only
`BodyIndex`, so a line-of-sight query never waits for it.

Kernel Resources are keyed by type, so these are two named types even if they
share an implementation.

**Both are hashed uniform grids internally**, which is not part of the contract.
Measured, **re-taken in float64**: a short Probe costs **103 ns** at a 2 m cell
over 1 024 circles among sixteen forty-metre walls, where the float32 prototype
gave 30–50 ns. The structures it was chosen over — 190–300 ns for a BVH (4–7×,
and 2× in a dense melee), about 300 ns for `gox2d`'s SAH tree, and 745–790 ns
plus 2 allocations for cp's BBTree — **were measured on that prototype and none
of them has been re-taken**, so the ratios in this paragraph are the prototype's
and the only float64 number in it is the grid's own. cp's own static tree is also badly
unbalanced — 4 224 grid segments inserted in order give mean leaf depth 37.5 and
max 75, against about 13 balanced — with no `Optimize`, no rotations, and a
`Reindex` that panics `"implement me"`.

- **Cells are hashed, so there is no world extent.** The hash costs 6–10% on
  frequent short queries and 20–30% on long walks, matches the dense grid on
  incremental Body updates, and still allocates nothing. Memory follows the Shapes
  rather than the world. **The package imposes no world-size limit**: in float64 a
  1 µm step is reached only about 4 million km from the origin. Re-centring is the
  render's concern, not physics'.
- **Cell size is a plugin setting, per index, with a documented default of 2 m.**
  Dense crowds are cheapest at 1 m, long merged walls at 4–8 m; on the reference
  workload 2 m is best or within 25% of best for every frequent query. **The
  default is documented as tuned for a metre-scaled world**, so it does not read as
  a content assumption.
- **A BVH is not built now and can be added later**, since the structure is
  internal and no signature changes. The numbers record where it does better: a
  large-radius `Overlap` (a 36.9 m one costs 2.2 µs on a BVH against 9.4 µs on a
  2 m hash), long `ProbeAll` in a dense crowd, and Bodies spread very unevenly
  over a huge world. Against that it costs 128–750 µs to rebuild, and a refit-only
  tree queries 2–5.5× slower after 16 m of drift.
- **The Body index finds a slot by the walk's own position.** It is Cleared and
  refilled every tick, so the Nth Shape Index inserts is in slot N and nothing
  there looks an Entity up; an Entity inserted twice between two Clears is held
  twice, which the rebuild never does. The static index is kept current
  incrementally and has to find an existing entry by its Entity, so it keeps an
  Entity-to-slot table. **The two are deliberately not one mechanism.** The swept
  Sensor, which has a Hit's Entity and needs its entry, is handed the slot beside
  each Hit by its own Probe of the Body index.
- **A large Shape is listed in every cell its box covers**, but a cell holds a slot
  id, not a copy, and a rectangle scan tests each Shape once however many cells it
  spans. A 20 m platform rotated 45° covers about 196 cells at 2 m: 784 bytes of
  cell lists and one extra narrow test, paid once at insert for a static. **This is
  a spec note, not a rule.** The watch is many large Shapes overlapping one region,
  where every query there tests all of them; the answers, if it ever trips, are an
  oversize list scanned once per query or a second coarse level — neither measured,
  so neither built.
- **The broadphase inflates by the query radius before descending**, which is
  exactly cp's defect 5 fixed, and stores a caller-owned `ecs.Entity` in the leaf
  rather than a library object — which is what makes an index legal next to an ECS.

### Static geometry is ordinary Entities

**The plugin has no wall, grid or cell concept.** Static geometry is ordinary
Entities — a `Shape` plus the `Static` Tag — never changed in place; to move one,
the app replaces it. A query that Hits one returns its `ecs.Entity`, and that is
the whole of attribution.

**The plugin learns of changes through `Hooks[Shape, HookAddedRemoved]`**, drained
by Index. Rejected: diffing the index against a Query every tick (cost
proportional to the static count, every tick) and an explicit reindex call
(Chipmunk's `cpSpaceReindexStatic`, which goes stale silently). One static Entity
replaced costs 17–24 ns.

**Line of sight does not serialise the frame, and nothing had to be built for
that.** The kernel scheduler keeps a `readers` count beside `writers` and rejects
only a write request or a resource already held for write, so any number of `Read`
holders run together. Queries are reads. The earlier finding that *"one
frame-local Resource is one write lock"* overgeneralised from the ECS scene
binding's write on the `*scene.OpQueue` of the recording renderer #573 removed,
which serialises because it is declared `Write`.

### Filtering, two-sided

A pair collides when
`a.CollisionBits & b.CollidesWith != 0 && b.CollisionBits & a.CollidesWith != 0` —
cp's `ShapeFilter.Reject` without cp's `Group`, which is out of scope. **32
groups**; 64 would make the pair 16 bytes instead of 8. **C** uses `unsigned int`;
only the Go port widens it.

- **0 in either field means nothing collides**, as in cp: no groups to be seen in,
  or nothing to collide with. **Every constructor sets both fields to all bits**,
  as cp's shapes start at `SHAPE_FILTER_ALL`, so a Shape built the normal way
  collides with everything. Exported constants name all bits and no bits.
- **The hazard, accepted**: a `Shape` written as a bare literal collides with
  nothing, and setting `CollisionBits` while forgetting `CollidesWith` is silent.
- **The plugin has no collision configuration at all.** An app wanting its rules in
  one table writes one; deriving each kind's `CollidesWith` from a list of pairs is
  a few lines of ordinary constants. This is strictly more expressive than a
  matrix: every matrix can be written as masks, but not the reverse — two
  projectiles in one group with different rules need a whole new group under a
  matrix and are two different values here.
- **A body changes what it collides with by writing its own `Shape`**, which Index
  picks up next tick. A Static body still cannot: it is replaced.
- **Querying as a Shape collides is passing that Shape's two fields.** A Shape that
  collides with nothing is invisible to queries too.
- **Queries do not skip Sensors**, which departs from cp — its point and segment
  queries skip them unconditionally while its box and shape queries do not.
  `Overlap` has to find Sensors, a Probe that always skipped them would leave no
  way to Hit one, and the groups already express the skip when an app wants it.
- **A pair-dependent rule cannot be expressed in the bits.** Owner exclusion and a
  line-of-sight gate stay in the app's filter System, so **there are two filtering
  mechanisms by design**: the plugin filters by category, the app by relationship.

### cp's collision handlers, mapped

| cp | the port |
| --- | --- |
| **Begin** | a Began entry, read by the app's filter System |
| **PreSolve** returning false | the filter marks the entry dropped for this tick |
| **PreSolve** editing friction, restitution, surface velocity | an entry write, lasting one tick |
| **PostSolve** | a reaction System after Solve |
| **PostSolve**'s `TotalImpulse` / `TotalKE` | methods on the entry |
| **Separate**, including on removal | an Ended entry, carrying no reason |
| handlers keyed by collision type, and wildcards | the app's System reads its own Components and Tags on `A` and `B`. No lookup table — **which also removes cp's allocation per touching pair** |
| post-step callbacks | addition 8 |
| `QueryRejectConstraints` | `JointedPairs`, built in Index |

**There are no callbacks and no Hooks on Contacts anywhere.** A reacting System
reads the list after the filters.

---

## Settings

From [The contact solver](https://github.com/dvoyni/cog/issues/405),
[prototype #345](https://github.com/dvoyni/cog/issues/345) and
[Joints](https://github.com/dvoyni/cog/issues/318).

**One settings struct, fixed at registration.** Configuration, not constants — but
nothing changes them at runtime: cp exposes setters and never moves them mid-run,
and every one is a property of the solver or an index rather than of a scene.

| setting | cp's stored value | the port | what changed |
| --- | --- | --- | --- |
| `Iterations` | 10 | **10** | nothing — a count |
| `Slop` | 0.1 | **0.005 m** | a real conversion |
| `Bias` | `math.Pow(0.9, 60)` ≈ 0.001797 | **6.32 /s** | re-spelled, same number |
| `Persistence` | 3 ticks | **0.05 s** | units |
| `StaticCellSize` | — | **2 m** | cog's |
| `BodyCellSize` | — | **2 m** | cog's |
| coincidence seed | fixed `(1, 0)` | **a seeded source the caller supplies** | symmetry |
| Joint `MaxForce`, `ErrorBias`, `MaxBias` defaults | ∞, 0.9⁶⁰, ∞ | ∞, **6.32 /s**, ∞ | per-second units |

**The Slop is the only genuine conversion.** cp's 0.1 is pixel-sized — defect 7 —
which in metres allows a third of a 0.3 m radius as overlap. 0.005 m is Box2D's
linear slop in a metre world, 0.8% of a character on a 2 m grid, and nox's own
measured settle band of 0.7 cm is the sanity check on it.

**The Bias is already per second in cp.** cp computes `1 - pow(collisionBias, dt)`,
so the stored 0.9⁶⁰ is *the share of overlap left after one second*, yielding 0.1
per step at 60 Hz at any tick rate. Nothing needs converting, only re-expressing:
0.0018 is unreadable as a setting, and Damping already fixed the spelling for this
shape of quantity. `1 − exp(−6.32/60) = 0.1` reproduces cp exactly. **Two
spellings of one idea would be worse than an ugly number.**

**Persistence is a hysteresis window, not a frame count.** 3 ticks means 0.05 s at
60 Hz and 0.1 s at 30, and the second is not what cp intends. Stored in seconds,
converted to ticks internally.

**No per-body override** — cp has none, and none of the four solver settings is a
property of a Body.

**Not derived from the index cell size.** They correlate, since both track how big
things are, but the cell size tunes the broadphase against typical *Shape* size
while the Slop is a tolerance against *world scale*. Deriving one from the other
would mean retuning the broadphase silently changes how deeply Bodies rest in each
other. Both defaults are documented against the same assumption — a metre-scaled
world — and that is a cross-reference in the doc, not a coupling in the code.

**A `SolverSettings` Resource was rejected**, and so was a runtime settings
command: nothing needs to change them mid-run, and a Resource an app System
declared `write` on would conflict with Solve for the whole frame, on every tick,
including the ones it writes nothing.

**The coincidence nudge is the one place randomness enters the solver**, so it
draws from a seeded source the caller supplies — nearly free now, awkward later.
It lives in Detect. cp's other two fallbacks are geometry, not coin flips, and
port as written: circle-against-segment falls back to the segment's own normal,
and a circle point query to `(0, 1)`.

### Constants

From [physics: which small additions earn their place](https://github.com/dvoyni/cog/issues/507)
and [physics: gravity](https://github.com/dvoyni/cog/issues/320).

**`Constants` is a Resource of the values that are a property of the scene, not
of the solver**, and gravity is the one there is. It is the Resource the
`SolverSettings` rejection above did not rule out, because what that rejection
weighed is a value nothing changes mid-run; a scene's gravity is one an app may
want to change, and an app that never does pays nothing.

```go
type Constants struct {
    Gravity m.Vec2d // m/s², default 0
}
```

- **The plugin registers it**, next to `Contacts`, **at its own defaults**:
  gravity 0, which is a top-down plane.
- **Solve takes `read{*Constants}`** and reads `Gravity` once a tick. A change
  applies from the next Solve; nothing is cached across ticks.
- **An app that wants another value takes `write{*Constants}`** in a System of
  its own — once from `app.InitEvent` for a constant gravity, or every tick for
  one that changes. That System runs in series with Solve, and with anything
  else that reads `Constants`, on every tick it is subscribed to: **the price
  the app chose**, and the reason a value set once belongs in an init System.
- **It is not a `Config` field.** `Config` is fixed when physics starts and every
  one of its fields is a property of the solver or of an index.

cp's `Space.SetGravity` wakes every sleeping Body, and cp's idle-speed threshold
falls back to one derived from gravity; both arrived with sleeping. The sleep
System reads `Constants` as Solve does: a changed `Gravity` wakes every Island,
and an `IdleSpeed` of 0 falls back to `|g|·h` from it
([Sleeping and Islands](#sleeping-and-islands)).

---

## Fidelity to cp

From [Verifying the port against cp](https://github.com/dvoyni/cog/issues/423).
This section follows `scene.md`'s **Demos and acceptance** pattern: what each
layer pins, assertions for everything that is a number or an ordering, and one
falsifiable sentence for what only eyes can judge.

*"Behaves like cp"* is **three claims at three scales, and cp can only be the
oracle for two of them.**

| layer | what it is | oracle | asserted? |
| --- | --- | --- | --- |
| **A — pair tests and queries** | `Collide` and the three Probes: **pure functions of values**, no history, no accumulation | **cp**, exactly | yes, every case |
| **B — one step from a fixed state** | identical Body state, identical Contact set, one solve | **cp**, for a few ticks | yes, a short horizon |
| **C — a scene over time** | the whole simulation running | **closed forms**, never cp | yes, against the closed form |

**Nothing chaotic is ever asserted.** A statistical comparison of a settled scene
was put and **rejected**: there is no partial credit within a run — a single
flipped bit flips a `d2 >= 4r²` branch and the histories are unrelated within 30 s
— and FMA fusion is **build-configuration** dependent, so the port diverges from
its *own* recorded trajectory under a different `GOAMD64`. It would be a large
amount of machinery, a weak signal, and a test that flakes on a compiler flag.

**How far the port and cp stay together is a recorded measurement, never a test.**

### The corpus, and where it lives

The repo has **no CI at all, no `testdata/`, no golden or snapshot tests**. A
test-only dependency is nonetheless precedented — `golang.org/x/tools` is a plain
direct require imported only by `kernel/archtest`. So importing cp was available
and was **not taken**.

**The harness lives on a `research/port-vs-cp` branch, which does import cp.
`main` carries only what the harness emitted.**

- The emitted artifact is **`cpcases_test.go`**, an ordinary table-driven
  `[]struct{…}` of cp's frozen answers — **generated Go source, not a fixture
  file.** It needs no `testdata/`, it is compiler-checked, it reads as a diff, and
  it is exactly cog's existing test idiom.
- **`main`'s `go.mod` never gains `cp`.**
- The branch follows the `research/<name>` convention, with the note beside it
  recording the numbers.

Three reasons, in order of weight:

1. **A corpus regenerated each run is flaky by construction.** Some configurations
   are legitimately ambiguous — EPA on a square resting exactly flat on a square
   has no single right face — so a randomised differential test fails at random on
   a case where neither answer is wrong.
2. **An engine should not permanently depend on the library it replaces.** Every
   cog consumer's module graph would carry a physics engine for a one-time act of
   verification.
3. **It fixes the porting index's own sin**: its measurements cannot be reproduced
   today. A committed harness is re-runnable when cp is re-read.

**Tolerance: 1e-9 m on points and depth, 1e-9 on the normal's components.** A
float64 ULP at the 512 m worst-case bound is ~1e-13 m, GJK/EPA runs at most 30
iterations, and the only real wobble is FMA fusion at ~1 ULP per fused operation.
So 1e-9 sits about four orders above the noise and six below the 0.005 m Slop.

**The corpus runs cold.** GJK's warm start is a cached id on the pair, so both
sides are given id 0 and `Collide` is a genuine function of the two Shapes and
poses.

**Coverage**: the six pair kinds — circle–circle, circle–segment, segment–segment,
circle–poly, segment–poly, poly–poly — each with radius 0 and radius > 0, over
roughly a hundred poses apiece. About a thousand cases, and a generated file of
order 150 KB. **That size is the cost of the choice and is named here rather than
discovered later**; it is the scale of cog's larger specs.

**The ambiguity rule: classify, never average.** Every case where cp and the port
disagree beyond tolerance is sorted by hand into one of two piles:

- **a defect fix** — it joins the assertion list below and is asserted as a
  *difference*, with the **C** line that proves the port right;
- **genuinely bistable** — it leaves the fidelity corpus and is frozen as the
  **port's own** regression case, pinning the port's answer against future change
  and claiming nothing about cp.

**There is no mismatch budget and no tolerance loosened to absorb a failure.**
Every case that remains in the fidelity corpus agrees exactly; the ones that
cannot are named individually.

### Layer B

Identical Body state, an identical Contact set, one solve, compared on the
resulting velocities at the same 1e-9. Gauss-Seidel is order-dependent, so the
test **feeds Contacts in cp's order**; the port's own ordering is a departure that
changes the answer in the last bits and is not what this layer is measuring.

**A short horizon, not one tick.** One tick proves the arithmetic; the horizon
that matters is the handful of ticks over which warm starting, persistence and the
bias correction all engage. The assertion is over a **fixed, small number of
ticks** with the Contact set stable by construction — two circles pressed
together, a box on a plane — not over a scene that rearranges itself.

### Layer C: seven scenes

Each names the cp code path it exercises. **Only the first needs cp at all.**

| scene | exercises | oracle |
| --- | --- | --- |
| the reference scene, N=256 and N=1024 | the whole step | **cp**, for timing and allocations only |
| a stack of boxes settling | warm starting, two-point Contacts, Slop, `Iterations` | rest within the Slop, no buzz |
| a box on a ramp at the friction limit | the Coulomb clamp `jtMax = u·jnAcc` | slides iff `tan θ > u` |
| a restitution ladder, `e` = 0, 0.5, 1 | `bounce` taken before damping | bounce height ratio `e²` |
| a pendulum of pin Joints | the Joint solver, `errorBias` | small-angle period `2π√(L/g)` |
| a circle rolling along a segment chain | segment neighbours (defect 4) | no catch at the joints |
| 1 024 one-metre Probes at `r = 0.3` | broadphase inflation (defect 5) | brute force over all Shapes |

Scenes 2–6 are **stronger than a cp comparison**, because a closed form is *right*
where cp is merely the reference — and they cost no dependency at all.

**Four of these scenes write their own gravity as a Force.** The ramp, the ladder
and the pendulum leave `Constants.Gravity` at zero and add `m·g` into `Force`
from an ordinary System — the same fall as the gravity term, which is asserted at
1e-9 against cp beside them, **and a check that gravity really is optional
rather than assumed.**

**A `Force` written this tick moves the Body next tick**, so every scene that
writes gravity is off by one tick against the analytic form. The scenes settle or
average over enough ticks that the offset is below tolerance; where it is not, the
closed form is stated with the offset in it.

### The departures sort into three buckets, not two

**Invisible — nothing to compare, so nothing to exclude.** One kind-discriminated
`Joint` Component against cp's ten structs; dense gather arrays addressed by a
slot table; frame-local `PreStep` scratch; `GetImpulse()` becoming `Impulse()`;
the Spring's `Absorption`; groove's derived normal; `collideBodies` via
`JointedPairs`; the bias re-spelled as 6.32 /s; persistence in seconds;
constructors taking values; `Probe` for cp's segment query; the narrowphase's
switch in place of interface dispatch; per-point ids exact where cp hashes. **These
change no observable number.** They are layout, naming and units, and a comparison
has no quantity to hold out. **This is most of the list, and saying so is the
finding.**

**Configured away — cp has the setting.** Slop, iterations, bias, persistence,
Friction and Restitution. **cp always runs at the port's settings.** Comparing
defaults measures a setting, not an algorithm, and cp's defaults are pixel-sized
so the comparison would be against a known wrong number.

**Asserted — a deliberately different, better answer.**

| departure | assert | proof it is right |
| --- | --- | --- |
| `CircleToPoly`'s radius sign | the Contact point differs by ~2·r | **C** has `-poly->r` |
| Segment neighbours | a circle crossing a chain joint does not catch | **C** has `SetNeighbors` |
| `ClosestT` missing `+CPFLOAT_MIN` | a degenerate simplex returns a number | **C** guards it |
| Circle `PointQuery` dividing by `d` | a Probe on a circle's centre returns a point | **C** tests `d > 0` |
| Poly `SegmentQuery` missing `max(an−bn, CPFLOAT_MIN)` | a parallel ray returns a number | **C** guards it |
| Space-level Probes at `r > 0` | the port finds Hits cp misses | **defect 5 is in C too** |
| The five angular Joints | a finite result where cp's Go port gives NaN | **C** asserts one of the five |
| A Joint to a despawned Body | skipped, where cp asserts | a Reference simply misses |
| `Dynamic` rejects infinite mass | classification by Component presence | cp compares `INFINITY` exactly |
| An Ended entry keeps last tick's geometry | points are there where cp's `Count()` gives 0 | cp's CACHED state |
| The coincidence normal is seeded | symmetry breaks | cp's fixed `(1,0)` never breaks it |
| One segment normal sign | one sign, both ways of setting endpoints | **a departure from Chipmunk itself** — see below |

**The real exclusion list has two entries on it**: nox's `0.7` friction, which is
*velocity-proportional* tangential damping where cp's is Coulomb and so does not
convert at all; and cp's `swapped` flag, deleted along with the sign wart it
caused.

### The defects the port does not inherit

Seven from the porting index, plus two found while resolving the map:

1. **`CircleToPoly`'s radius sign** — `+poly.r` where **C** has `-poly->r`. A
   circle against a rounded Polygon settles about 2·r too deep. Harmless at
   Polygon radius 0.
2. **656 allocations a step** at N=1024, from two sources: `CollisionInfo`
   escaping once per narrowphase call, and a `&CollisionHandler{}` **lookup key**
   built once per touching pair inside `LookupHandler`. **Both are gone** — EPA's
   hull becomes stack buffers, and there is no handler table to key into. *The
   porting index describes the second as `LookupHandler` "returning
   `&CollisionHandler{}`"; it returns the found handler, and the allocation is the
   key. The allocation is real, the description was not.*
3. **A static BBTree that never rebalances**, with no `Optimize`, no rotations,
   and a `Reindex` that panics. Replaced by a hashed grid.
4. **Missing segment neighbours**, leaving the end-cap rejection dead code.
5. **Space-level Probes with radius > 0 miss Hits**, in **C** too: the broadphase
   receives the un-inflated ray.
6. **Missing `CPFLOAT_MIN` guards** — `ClosestT`, the circle point query's divide
   by `d`, the Polygon segment query's `an − bn` — which in float64 are the
   difference between a number and a NaN. The port restores **C**'s guards and
   uses `CPFLOAT_MIN` where the Go port weakened it to `1e-15`.
7. **`INFINITY = MaxFloat64` compared exactly to classify bodies.** The port
   classifies by Component presence.
8. **A pixel-sized 0.1 Slop.**
9. **`C`'s `moment != 0` assert in the rotary spring, dropped in translation**,
   which leaves cp's five angular Joints an unguarded `Inf·0`. *Found by
   [Joints](https://github.com/dvoyni/cog/issues/318); not among the index's
   seven.*

And two more for the record, neither of which the port can inherit because the
structures are gone: cp's contact-buffer constructor zeroes 96 KB twice where
**C** does one `cpcalloc`; and `SpaceHash.Reindex` clears but never rehashes.

**One item on the record was misattributed and is corrected here.** The segment
normal inconsistency — `rperp` in the constructor against `perp` in
`SetEndpoints`, two normals 180° apart — was recorded by
[Convex polygons](https://github.com/dvoyni/cog/issues/323) as new to the Go port,
with **C** said to be consistent. **Both halves are wrong**: it is in the index's
defect 6, which says plainly *"in C as well"*, and **C** is not consistent. The Go
port mirrors **C** exactly. **So the port having one sign is a departure from
Chipmunk itself, not a defect fix**, and it sits on the assertion list under that
reason.

### The finiteness invariant

The one thing the port claims over **both** cp and Chipmunk:

> **No input produces a NaN or an infinity in a Component the plugin writes.**

A fuzz over degenerate inputs — coincident centres, a zero-length segment, a Probe
starting exactly on a circle's centre, both Bodies of infinite mass, two
non-turning Bodies joined by a rotary Spring, a degenerate GJK simplex, a Joint
anchored at the centre of gravity — asserting that every output is finite. It is
stated in the spec as a **constraint**, not only as a test.

**It is the highest-value assertion in the plan**: cheap, hardware-independent,
and it is what would have caught **all three defects found while resolving
[Joints](https://github.com/dvoyni/cog/issues/318),
[Convex polygons](https://github.com/dvoyni/cog/issues/323) and
[Contact manifolds](https://github.com/dvoyni/cog/issues/407)** — none of which
were found by looking for them. `jakecoffman/cp` fails it on at least four of the
listed inputs and Chipmunk asserts on two more.

### The one demo

**One demo in `cog-examples`**, where cog's acceptance tests live and run GPU-free:
a stack, a ramp and a jointed figure in one scene. *"Does this look like a physics
engine"* is the one judgement no assertion makes.

### The app-facing acceptance tests

Four of nox's survive the redraw, two are retired. They are the **app's** tests,
not fidelity to cp, and they are listed here because they are the spec's evidence
that the port is usable:

| criterion | tests | verdict |
| --- | --- | --- |
| terminal speed and ramp — 95% of terminal within 190 ms | the integrator alone | **keep**, asserted in m/s and ms |
| slide along walls — `v ← v − (v·n)·n` | now the *solver* | **keep, repointed** |
| no projectile through a 0.4 m wall at 40.4 m/s | the Probed Sensor | **keep**, as Detect's |
| crate stops by damping — half-life ≈ 2.3 s | Damping alone | **keep, converted** |
| reflection exact | an app-side mirror | **retire, replace** |
| a character pressed into a wall settles into a 0.7 cm band | the recovered spring | **retire, restate** |

**Slide along walls stops being a special rule and becomes the solver's own
behaviour.** At `e = 0, u = 0` the normal Impulse cancels exactly the approaching
normal velocity and the Coulomb clamp leaves the tangent alone, which *is*
`v ← v − (v·n)·n`. It becomes the test that validates the shipped defaults.

**Reflection's replacement is better than what it replaces.** At `e = 1, u = 0`,
cp gives `v' = v − 2(v·n)·n`, an exact mirror preserving speed at *any* wall angle,
not only the four diagonals nox's cheap rule needed.

**The settle band is restated against the Slop**: *"a Body pressed into a wall
rests within the Slop of the surface and does not buzz."*

**The crate confirms Damping's units against a measured number.** nox's `k` is per
*tick*: ×60 gives 15 /s for the crate and 0.3 /s for the boulder, and
`ln2 / 0.3 = 2.31 s` reproduces nox's own quoted 2.3 s half-life.

---

## The zero-allocation claim

**Zero heap allocations on every query, every primitive, and the whole step.**

The rules, stated with their reasons:

1. **Zero heap allocations** on every query and primitive path, and inside the
   four Systems.
2. **Multi-result queries append** to a caller-supplied slice and return it
   (`dst = idx.ProbeAll(dst[:0], …)`). The only allocation is growing a buffer the
   caller sized too small, which settles to none.
3. **No interface and no func value** in any query or primitive signature. A
   capturing closure handed to a non-inlined method escapes; `ecs.md` measured
   per-field binder closures at one allocation where iterators and slice loops
   measured zero.
4. **An index holds no per-query state**, because its readers run concurrently.
5. **Never hand a bound plugin a pointer into a Component Store.** The measured
   hazard in `scene` is a `*m.Mat4` retained by value until a flush running in a
   *different* System, after the recording System's locks are gone.

**Enforcement.** Every query and primitive has a test asserting
`testing.AllocsPerRun(…) == 0` on a warmed, adequately sized buffer, plus a
benchmark for cost. **A benchmark alone reports and never fails, which is how cp's
two allocations per query survived.**

**The step gets `TestTheStepSitsOnTheEnginesAllocationLine`**, mirroring
`ecs`'s `TestTheFrameSitsOnTheEnginesAllocationLine`: 100 warm-up ticks, 10 000
counted through a `MemStats` delta, at **two** workload sizes (N=256 and N=1024),
a **slope check** plus one absolute ceiling, and `t.Logf` of all five numbers so a
passing run still prints the measurement. **Allocations are hardware-independent,
which is exactly what makes them assertable.** cp allocates 164 / 12.5 KB and
656 / 49.8 KB a step; the port allocates none.

**Wall-clock is asserted nowhere.** cp's 60.9 µs and 284 µs are recorded with the
hardware named, measured **interleaved A/B** against the port rather than in
sequence, because a whole-frame benchmark here swings about ±10% by run order.
**The target is at or under cp on the same scene and the same machine; missing it
is a finding to investigate, not a red test.**

Benchmark files follow the repo's naming — `<topic>bench_test.go`, no underscore
before *bench* — call `b.ReportAllocs()` in code rather than relying on
`-benchmem`, and use the classic `b.N` loop: **`testing.B.Loop` is deliberately
rejected in this repo** for pinning loop variables through `runtime.KeepAlive`.
There is no race detector in this environment; concurrency claims are tested with
`-count=10` and say so.

### The evidence

Written after the measuring rather than before it. On the hardware this
specification names:

| measured | objects a tick |
| --- | --- |
| the engine with no Bodies in it | 19.089 |
| N=256, over 104 Contacts and 40 Sensor Hits | 19.019 |
| N=1 024, over 608 Contacts and 352 Sensor Hits | 19.015 |
| the Polygon scene, no Bodies | 19.033 |
| the Polygon scene, N=256 over 256 Contacts | 19.013 |
| the jointed scene, nothing in it | 19.039 |
| the jointed scene, N=256 over 104 Contacts and 64 Joints | 19.017 |
| the jointed scene, N=1 024 over 608 Contacts and 256 Joints | 19.010 |
| the Polygon scene under `Constants` gravity, no Bodies | 18.026 |
| the Polygon scene under `Constants` gravity, N=256 over 256 Contacts | 18.048 |
| piles under `Constants` gravity with sleeping on, no Bodies | 19.045 |
| the same, N=256 all asleep | 19.030 |
| the same, N=1 024 all asleep | 19.017 |
| the same churning — a stack kicked awake every tick — no Bodies | 20.026 |
| the same churning, N=256, 4 000 kicks | 20.009 |
| the same churning, N=1 024, 4 000 kicks | 20.035 |

The six sleeping rows were taken with sleeping, whose fifth System is one
more subscription on every line — the kernel's own is about 18 objects a tick
since — and their scenes compose the app's `Constants` writer, and the churning
ones the kicking System as well; each sits flat on its own empty line, and a
sleeping-off run of the four scenes above sits flat on 18.0. The gravity rows
before them were taken later than the rest, when the kernel's own line had dropped to
about 17 objects a tick, and their scene composes one more subscription — the
app's System writing `Constants` every tick — so they sit a whole object above
their own era's line; what they claim is their own flat slope, and repeated runs
put it at −0.03 to +0.02.

**The slope is flat and slightly negative**, which is the claim: what a tick
allocates is the kernel's own dispatch over the subscriptions the engine
composes, and it does not move when the Body count goes up four-fold. cp
allocates 164 objects and 12.5 KB a step at N=256 and 656 and 49.8 KB at N=1 024
over the same scene. The Contact counts and the Sensor-Hit counts are printed
beside every figure and asserted above zero, because **two earlier rounds
measured an empty scene without noticing** and a third found the fixture spawning
shapeless Bodies, so the index rebuild walked nothing.

Every query and primitive benchmark reports **0 B/op and 0 allocs/op** beside its
cost, over a warmed, adequately sized buffer, and the `AllocsPerRun` tests are
what fail when one does not. **`ShrinkCmd` is the one thing in the package that
allocates on purpose**, and it is not on the hot path: the ticks after it regrow
what it cut, which is measured against a control that had the same spike and no
shrink.

---

## What is not foreclosed

- **Per-pair materials.** The product is only the default combination; the fields
  are on the *entry*, and the app's filter Systems run exactly where cp's PreSolve
  edits land. Rubber-on-ice for one pair is an entry write.
- **A parallel solver.** The dense gather arrays are what one would partition.
- **A BVH**, or any other index structure: it is internal and no signature changes.
- **A per-`Shape` surface velocity**, over the filter recipe that closes addition
  2: one `Shape` field and two lines in Detect, at 16 bytes on every `Shape` and
  every index entry — about 2 µs on the Index walk and 2.7 µs on a resting step
  per 5,248 Shapes, scaled from [#409](https://github.com/dvoyni/cog/issues/409).
  It stays possible later.
- **Raising the inline vertex cap**: a constant, with
  [#409](https://github.com/dvoyni/cog/issues/409) holding the measurement.
- **A per-Entity Contact index** (`Of(e)`): purely additive.
- **An any-hit query**: rejected on numbers, not on principle.
- **Contacts as Entities**, once structural change can be deferred.
- **Sub-steps within a tick.** The nested sub-step event was **verified working** by
  a throwaway test at about 52 µs a tick, and parked with its three rules.
- **Post-step callbacks** (addition 8), **debug drawing** (addition 9),
  **geometry from images** (addition 10).

---

## Shapes that were rejected

| rejected | why |
| --- | --- |
| Wrapping a Box2D v3 port behind the ECS | sync measured at ≈ 50–60 µs a tick over a 651 µs step; Components would mirror a world holding the truth; its queries cost 0.6–1.2 µs *with* allocations; it is one author's unreleased port. cgo to C Box2D costs 22 ns a call and does not build for the browser |
| A native port of Box2D v3 | the most complete reference, and about 29 600 lines of Go against cp's 7 400 for the same feature set |
| Using cp as it is | `cp.Shape` is 168 B with 5 of 13 fields pointer-bearing, and unexported fields forbid literal construction. A Component holds no pointer, transitively |
| One Component for all body state | a System adding Force would block the render copy reading position |
| One Component per field | buys no concurrency past the writers split, and widens every Query |
| A `Kinematic` Tag, a `Kind` field, a `FixedRotation` Tag | each encodes twice what presence or a stored inverse already says |
| Storing (cos, sin) beside the `Angle` | the two would have to agree, so gameplay could not set a heading with a field write |
| A `cog` field, or deriving the centre of gravity from the Shape | two coordinate systems through all the ported code; an offset centre inexpressible |
| Eight inline vertices | 152 B charged to every circle and wall segment in the world |
| Storing local normals | a time-for-space trade paid by every circle in the world |
| The world cache as a Component, or recomputed per pair | a structural change per tick; a Shape takes part in several pair tests a tick |
| Ten Joint Component types | ten Stores, ten Queries inside Solve, ten lock entries, and a kind change becomes structural |
| Joint scratch on the Joint Component | a Contact entry is solver-owned Resource data; a Joint Component is app-owned |
| Splitting the Contact entry into app-facing and solver-private halves | the owner chose one struct; it also makes `TotalKE` free rather than needing a back-pointer |
| Unexported bias fields on `Velocity` | cp's shape, taking a Component gameplay writes every tick from 24 B to 48 B |
| A `SolverSettings` Resource, or a runtime settings command | an app System declaring `write` would conflict with Solve for the whole frame, every tick |
| The exact-damping velocity integrator | a divide and a branch per Body per tick to buy nothing at a fixed step |
| Store lookups per Contact per iteration; gathering through an Entity→slot hash | 12–35 µs; ~9 µs, where the hash eats the win |
| Sorting to match pairs; cp's persistent arbiter table | ~50 µs at 2 000 entries; an indirect walk over a table with holes |
| A global collision matrix of rules | strictly less expressive than two per-Shape masks, and it would need its own runtime-change story |
| Handing `collideBodies` to the app's filter | a ragdoll would not work out of the box |
| Sub-steps, in either of two priced forms | the owner dropped them; cp's solver iterations take their place |
| Deleting a dropped Contact entry | a reacting System would see a Contact begin twice without ending, and every drop shifts the slice |
| Two lists, solid and Sensor | filters and *"what is X touching"* would walk both |
| One entry per party | twice the list, and a filter must drop both halves |
| A predicate or iterator in a query signature | a func value allocates; ordering needs scratch an index cannot hold |
| Ignoring the Shape a Probe starts inside | a projectile would leave a wall it spawned in, and spawn validation would pass silently |
| A statistical comparison of a settled scene against cp | no partial credit, and FMA fusion makes it flake on a compiler flag |
| Importing cp test-only into `main` | a corpus regenerated each run flakes on legitimately bistable cases |
| Folding the sleep System into Solve, to save its dispatch while sleeping is off | Solve would gain both indices to read and `Sleeping`, `Rest` and the `WakeCmd`'s queue to write, and an app's Probe of the Body index could no longer run beside it |
| Registering the sleep System only when `Sleep` is configured on | turning sleeping on between ticks would stop working |
| A population count on an ECS accessor, to ask whether anything sleeps | public ECS API for what a one-field `With[Sleeping]` Query answers under the read the filter takes already |

---

## Ported symbols

Anchored to **`github.com/jakecoffman/cp/v2` v2.4.0**. The prose above names
symbols; this table holds the lines. **Re-anchoring onto a future cp is one edit
here**, and the four ABSENT rows are the ones to re-check first, because each is a
claim this specification makes *about* cp.

**space.go** — `Space.Step` :665-771 · iterations default :68 · slop default :71 ·
bias default :72 · persistence default :73 · `MAX_CONTACTS_PER_ARBITER` :11 ·
`CONTACTS_BUFFER_SIZE` :12 · `SpaceCollideShapesFunc` :408 · `QueryReject` :498 ·
the arbiter append :454, guarded by :444-453 (Sensor at :450, both-infinite-mass at
:453) · `QueryRejectConstraints` :514 · `LookupHandler` :840, its key literal :841
· `SegmentQuery` :1032 · `SegmentQueryFirst` :1042 · `ShapeQuery` :1084 ·
`BBQuery` :980 · `PointQueryNearest` :940 · **the un-inflated broadphase ray
(defect 5)** :1036-1037 and :1045-1046 · PostSolve's `UserData` argument :767 ·
`SetGravity` :119-126, waking every sleeping component :123-125 · gravity read
once a step and handed to every Body :726-730 · the idle-speed fallback from
gravity :530-536 · `IdleSpeedThreshold` and `SleepTimeThreshold` :21-22, the
latter defaulting to `INFINITY` :82 · `Space.Activate` :150-205 and `Deactivate`
:207-239 · `ProcessComponents` :525-615, its kinetic-energy test :549, its
arbiter loop waking a sleeper or a Body touching a Kinematic one :558-573, its
constraint loop :577-584 · `ComponentActive` :617 · `FloodFillComponent`
:626-656.

**body.go** — `BodyUpdateVelocity` :608 · `BodyUpdatePosition` :621 · defaults
wired :85-86 · `SetTransform` :359, with `p − R·cog` at :363-366 · `Activate`
:370-411 · `ActivateStatic` :414 · `IsSleeping` :429 · `KineticEnergy` :443 · the
setters that wake, `SetPosition` :286, `SetVelocity` :300, `SetForce` :332,
`SetTorque` :343, `ApplyForceAtWorldPoint` :498 among them ·
`AccumulateMassFromShapes` :232-260, ported in its one-shape case as
`NewDynamicForShape`, which cp's `Shape.SetDensity` (`shape.go` :97) drives. Its
three per-kind mass infos are `CircleShapeMassInfo` (`circle.go` :20),
`NewSegmentMassInfo` (`segment.go` :174) and `PolyShapeMassInfo` (`poly.go`
:238).

**arbiter.go** — `PreStep` :168, its `dist` :182 · `ApplyCachedImpulse` :113 ·
`ApplyImpulse` :125 · `Update` :191 · `TotalImpulse` :409 · `e = ea·eb` :230 ·
`u = ua·ub` :231 · surface velocity :233-234 · `Count()` :424, its CACHED guard
:425-428.

**everything.go** — `k_scalar_body` :339 · `k_scalar` :344 · `k_tensor` :352, its
`assert(det != 0)` :383 · `bias_coef` :392 · `Contact` :85 · `CollisionInfo` :113 ·
`ShapeFilter` :169, `Reject` :186 · `INFINITY` :9 · `MAGIC_EPSILON` :10 ·
`SHAPE_FILTER_ALL` :39 · `MomentForBox` :205, `MomentForCircle` :222,
`MomentForSegment` :229, `MomentForPoly` :237 · `AreaForCircle` :261,
`AreaForSegment` :266, `AreaForPoly` :273 · `CentroidForPoly` :288.

**collision.go** — `SupportContext` :61, its two func fields :63 ·
`ClosestPoints` struct :73, method :224 · `CircleToCircle` :86, its `(1,0)`
fallback :99 · `CircleToSegment` :109 · `SegmentToSegment` :141 · `CircleToPoly`
:164, **its sign bug** :173 · `SegmentToPoly` :177 · `PolyToPoly` :196 ·
`SupportEdgeForSegment` :265 · `SupportEdgeForPoly` :284 · `ContactPoints` :312 ·
`HashPair` :355 · `GJK` :360 · `GJKRecurse` :380 · `EPA` :418 · `EPARecurse` :426,
**its per-iteration `make`** :453 · `log.Println` :479, guarded :478 ·
`BuiltinCollisionFuncs` :486, dispatched :516 · `Collide` :499 · iteration
constants :9-11 · the `1e-15` epsilons :248, :328, :329.

**circle.go** — `CacheData` :29 · `PointQuery` :52, **its unguarded divide** :58,
with the `MAGIC_EPSILON` test arriving three lines *after* it at :61 · `(0,1)`
fallback :64.

**segment.go** — `a_tangent`/`b_tangent` declared :10 · `CacheData` :13 ·
`SetEndpoints` :62, **its `Perp` normal** :65 · `NewSegment` :160, **its
`ReversePerp` normal** :164, tangents left zero :167-168 · the dead end-cap
rejection reads them at `collision.go` :134, :156, :190.

**poly.go** — `CacheData` :38 · `PointQuery` :66, an unguarded divide :101 ·
`SegmentQuery` :114, **its unguarded `an − bn`** :129 · `NewBox` :181 · `NewBox2`
:194 · `SetVerts` :204 · `ConvexHull` :250.

**vector.go** — `Normalize` :88, its `1e-15` :89 · `ClosestT` :163, **its missing
`+CPFLOAT_MIN`** :165.

**bbtree.go** — `Pair` :25, its `collisionId` :27, read and rewritten :165, reset
on insert :196 · `Reindex` :318, **its `panic("implement me")`** :319.

**contactbuffer.go** — `NewContactBuffer` :13, **zeroing twice** at :14 and :15.

**constraint.go** — `Constrainer` :5 · `Constraint` :15 · `errorBias` default :39.

**the ten joints** — 816 lines between them. `dampedspring.go` 92: `SpringForceFunc`
:12, `assert(k != 0)` :53, the impulse inside PreStep :61, empty
`ApplyCachedImpulse` :64-66, `DefaultSpringForce` :90 · `dampedrotaryspring.go` 69:
`SpringTorqueFunc` :9, `defaultSpringTorque` :15, `iSum` :34-35, empty
`ApplyCachedImpulse` :47-49 · `rotarylimitjoint.go` 81: `iSum` :34 ·
`ratchetjoint.go` 89: `iSum` :46, **`Angle` mutated in PreStep** :43 ·
`gearjoint.go` 70: `iSum` :32 · `simplemotor.go` 57: `iSum` :26 · `pinjoint.go` 87:
`NewPinJoint` :14, reading transforms :23, :28 · `slidejoint.go` 90 ·
`pivotjoint.go` 83: `NewPivotJoint` :13, `WorldToLocal` :18, :24 ·
`groovejoint.go` 98: the cached groove normal :21.

### Four symbols are ABSENT, and each is load-bearing

- **`Segment.SetNeighbors`** — nothing writes `a_tangent`/`b_tangent` after
  construction anywhere in the module. **Defect 4 confirmed at the source.**
- **`Arbiter.TotalKE`** — no match in the module, and no per-arbiter kinetic
  energy anywhere in it. cp computes kinetic energy in **three** places, all per
  Body: `(*Body).KineticEnergy` at `body.go` :443, its caller in cp's own sleeping
  logic at `space.go` :549, and the inline sum in `DebugInfo` at `everything.go`
  :329. **So shipping `TotalKE` is porting from the C**, as stated above. *An
  earlier revision said the `DebugInfo` sum was the only one; corrected by
  [sleeping](https://github.com/dvoyni/cog/issues/316), which ports the second.*
- **`(*Body).UpdatePosition`** — there is no such method; the default integrators
  are the package-level `BodyUpdateVelocity` and `BodyUpdatePosition`, wired as
  function pointers. The citations above are corrected to the package-level names.
  *A citation slip in the closed tickets, corrected here.*
- **`LookupHandler` returning `&CollisionHandler{}`** — **the porting index's
  wording for defect 2 is imprecise.** `LookupHandler` returns `handler` or
  `defaultHandler`; the allocation is a `&CollisionHandler{TypeA: a, TypeB: b}`
  **lookup key** built on the line below it. *The allocation is real and still
  happens once per touching pair — only the description was wrong.* **Settled
  here.**

---

## Required work

The checklist this specification is built from. Blocking order runs top to bottom
within a group; groups after the first assume the value types exist.

**Foundations**

- [ ] `m.Vec2d` in `libs/m`, with `Vec2`'s method names plus `Perp`,
      `ReversePerp`, `Project`, `Unrotate`, `ForAngle`, and `Vec2d.Vec2()` /
      `Vec2.Vec2d()`.
- [ ] `Transform` and `BB` in `ecsphysics2d`, ported.
- [ ] The mass helpers as free functions, and `NewDynamicForShape` over them.

**Shapes and geometry**

- [ ] The `Shape` value, its five kinds, and the per-kind constructors, including
      the hulling Polygon constructor with its three failure sentinels and the
      segment-neighbour constructor.
- [ ] `Polygon` as a second Component over `m.List[m.Vec2d]`.
- [ ] `ConvexHull`, ported.
- [ ] The pair tests: the nine-arm switch, the closed forms, GJK, EPA with its two
      stack buffers, and `ContactPoints`, **with the guards restored**.
- [ ] The closed-form queries on all three families.

**The indices**

- [ ] The hashed uniform grid, with per-index cell size and the world cache slab.
- [ ] `StaticIndex` and `BodyIndex` as Resources, and the `Hooks[Shape,
      HookAddedRemoved]` drain.
- [ ] `Probe`, `ProbeAll`, `Overlap`, and the three pair primitives.

**The Components and the step**

- [ ] `Position`, `Velocity`, `Force`, `Dynamic`, the `Static` Tag.
- [ ] The four Systems, their lock sets, and the plugin's own chaining.
- [ ] The Contact list Resource, the two buffers, the swapped pair maps, phases,
      the two filter marks, and the three-run persistence window.
- [ ] Solve: the nine-step order, the gather through the slot table, `PreStep`,
      warm start, the iterations, and the bias applied as a position delta.
- [ ] `TotalImpulse` and `TotalKE` as methods on the entry.
- [x] Sleeping: the `Sleep` Resource, the `Sleeping` Tag and the plugin's `Rest`,
      the sleep System and its Islands, the quiet Contacts, the sleepers' grid in
      the Body index, and `WakeCmd`
      ([#316](https://github.com/dvoyni/cog/issues/316)).

**Joints**

- [ ] The `Joint` Component, its kind union, per-kind constructors and accessors,
      and the three pure helpers.
- [ ] All ten kinds, ported, with the two spring force hooks dropped.
- [ ] `JointedPairs`, built in Index and checked in Detect.
- [ ] The two-clause gather guard.

**Verification**

- [ ] The `research/port-vs-cp` branch: the harness, the emitted
      `cpcases_test.go`, and the note.
- [ ] Layer B's fixed-state tests.
- [ ] The seven scenes.
- [ ] The finiteness fuzz.
- [ ] `TestTheStepSitsOnTheEnginesAllocationLine`, and `AllocsPerRun` on every
      query and primitive.
- [ ] The interleaved A/B benchmark against cp, and the recorded numbers.
- [ ] The `cog-examples` demo.

**Housekeeping**

- [ ] The plugin's shrink command, modelled on `ecs.ShrinkCmd`, with its areas.
- [ ] The settings struct and its documented defaults.
- [ ] Fill in [Ported symbols](#ported-symbols).
- [ ] **Re-measure in float64** what the prototype measured in float32: the Query
      walk, the grid Probes, and the two `math.Exp` calls a Body a tick.

---

## Out of scope

- **Several Shapes on one Body**, dropped for good. A Shape's *offset* is not
  dropped: geometry is local to `Position` and may be off-centre.
- **Non-convex Polygons and mesh colliders.** Nobody supports concavity directly —
  cp, Box2D and Bullet all decompose — and decomposition means compound Shapes,
  and with them the Body→Shape Reference, one index entry per piece, and one
  Contact entry per piece-pair. **A concave outline is a chain of segment
  Entities**, which the port supports and makes slide smoothly; what it cannot be
  is a Dynamic body's Shape. A concave *Dynamic* body arriving redraws the
  destination rather than adding a ticket.
- **cp's collision group filter**, which the app's relationship filter covers.
- **Sub-steps within a tick**; cp's solver iterations take their place.
- **Parallelism inside the solver.** cp has none; `gox2d`'s goroutine-per-task
  scheduler is slower than one thread at N=256 at every worker count and only wins
  at N=1024. A parallel impulse solver needs graph colouring or Box2D v3's solver
  sets, which is a different engine's design.
- **Continuous collision for the whole world**, and **Probing a box**. Only
  circles Probe. A solid, non-Sensor Body that moves farther than its own extent in
  one tick tunnels; a thrown boulder is discrete.
- **Verticality and any third axis.** The `2d` in the package name is the
  commitment. Gravity as a constant acceleration *in the plane* is not out of
  scope: it is [Constants](#constants).
- **Bit-identical replay.** Determinism is neither required nor pursued. Two cheap
  things are taken anyway: the coincidence nudge draws from a seeded source, and
  the price of full replay is recorded rather than paid — **the whole
  cross-architecture bill is FMA fusion**, bought by a `float64(x*y)+z` barrier
  inside each helper; one machine is free, one architecture is a build lock. A
  mid-game snapshot is not a valid replay start point unless it serialises the
  Store's dense layout, which is rollback's bill rather than replay's.
- **Replication, networking and rollback.** Nothing is foreclosed.
- **Anything the driving game owns**: its vision sweep, door axes, merged wall
  runs, per-cell flags, level authoring, and every recovered constant.
