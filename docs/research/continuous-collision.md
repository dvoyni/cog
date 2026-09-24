# How 2D engines stop tunnelling without sub-steps, and what each costs per Body

Research for [dvoyni/cog#575](https://github.com/dvoyni/cog/issues/575), child of the map
[physics: continuous collision for the whole world](https://github.com/dvoyni/cog/issues/328).
It feeds the grilling ticket [#577](https://github.com/dvoyni/cog/issues/577), which owns the
decision. This note states facts and what they imply; it chooses nothing.

The map's **Settled while charting** section is taken as fixed: linear only, the lost time is
dropped, the gate is the one-step displacement against the Shape's minimum extent folded into
Integrate, no lock set widens, and the most performant approach wins.

Vocabulary is `CONTEXT.md`'s: Body, Shape, Probe, Hit, Contact, Static / Kinematic / Dynamic
body, Continuous collision. Engine identifiers (`isBullet`, `b2TimeOfImpact`, `ccd_enabled`) are
quoted as the engines spell them; "bullet" in prose means only Box2D's or Rapier's flag of that
name.

No code was copied from any engine. Everything below cites a file and a line at a named commit.

## The answer in six lines

1. **Every engine that drops the lost time does the same thing**: after the Body's position for
   the step is final and before the next detection, a Body that moved more than a fraction of
   its thinnest width has its path from the old pose to the new tested against Static geometry
   (and, if opted in, against moving Bodies at their *end* poses); the first time of impact wins
   and the Body is put back there. Box2D v3 does it, Rapier 0.35 adopted it wholesale in August
   2026, and `neguse/gox2d` already ports it to Go.
2. **The gate is the same in both**: `maxMotion > 0.5 × minExtent`, where `maxMotion` adds a
   rotation term `|ω|·maxExtent`. Box2D's `minExtent` for a Polygon is its inner radius plus
   rounding, a circle's is its radius, and a bare segment's is **0**.
3. **Neither engine covers Kinematic bodies by default.** A non-bullet Dynamic body is tested
   against Static geometry only. cog's settled guarantee (Static **and** Kinematic) is wider
   than either engine's default.
4. **Measured in Go (`gox2d`, float32)**: a Body that trips the gate costs **≈ +510 ns** a tick
   when it has something to stop at and **≈ +214 ns** when it does not; a Body below the gate
   costs **nothing measurable** (median +0.9 ns, inside run noise).
5. **Measured in C (Box2D v3 HEAD, one worker)**: turning the pass on costs **+6 %** of the
   whole step in the `rain` scene, **+14–17 %** in `smash` and `tumbler`, and nothing
   distinguishable in the pyramid and junkyard scenes. Nobody publishes a number of their own.
6. **Speculative contacts are not a substitute.** Box2D v3's and Rapier's reach 4 × Slop (2 cm)
   and exist to stop slow Bodies jittering; Rapier's per-Body "soft CCD" widens that reach per
   Body at the broadphase's expense and has no gate.

## What was read

| Source | Commit read | Date | Version | Licence |
| --- | --- | --- | --- | --- |
| `erincatto/box2d` HEAD | `8cb1768f8141a0e8da557f656876abc173f3d4b0` | 2026-09-23 | `3.2.0` in `include/box2d/base.h:133–139`, unreleased | MIT |
| `erincatto/box2d` tag `v3.1.1` | `8c661469c9507d3ad6fbd2fea3f1aa71669c2fe3` | 2025-06-04 | the last release | MIT |
| `erincatto/box2d` tag `v2.4.1` | `9ebbbcd960ad424e03e5de6e66a40764c16f51bc` | 2020-10-17 | for contrast | MIT |
| `dimforge/rapier` HEAD | `28d0ba929b460597f0959fe600c7afd65612f6f9` | 2026-09-19 | `0.35.3` (`Cargo.toml:48`), released 2026-08-28 | Apache-2.0 |
| `dimforge/parry` HEAD | `3383f51cbbe9af70565427e7a66c605e1557c1fc` | 2026-09-18 | Rapier pins `parry2d 0.31.1` | Apache-2.0 |
| `ByteArena/box2d` | `acbde413692f086bf32b364d1bcb8a546283eafd` | 2020-07-13 | Box2D 2.3.2 port | Zlib |
| `neguse/gox2d` | `8616afaae132b1d777a11ca406099b85bde248cc` | 2026-03-15 | Box2D v3.1.1 port | MIT |
| Box2D blog, [Starting Box2D 3.0](https://box2d.org/posts/2023/01/starting-box2d-3.0/) and [Releasing Box2D 3.0](https://box2d.org/posts/2024/08/releasing-box2d-3.0/) | — | 2023-01, 2024-08 | — | — |

Measurements: `AMD Ryzen 9 7950X3D 16-Core Processor`, Windows 11, `go1.27.1 windows/amd64`,
Box2D built with MinGW-w64 `gcc`, `-DCMAKE_BUILD_TYPE=Release`, SSE2 default. Every A/B was
interleaved, on and off alternating, three or five rounds, because whole-step benches swing by
run order. Nothing was added to cog's `go.mod`; the harnesses lived in a scratch module.

---

## 1. Box2D v3: time of impact after the solve, no sub-step

Read at HEAD `8cb1768`; where v3.1.1 differs it is said.

### Algorithm

Conservative advancement on a separating axis, with a mixed bisection / false-position root
finder: `b2TimeOfImpact`, `src/distance.c:1148`. It runs GJK distance between the two shapes,
builds a separation function on the resulting axis, and advances until the separation reaches a
**target of `max(B2_LINEAR_SLOP, totalRadius − B2_LINEAR_SLOP)`** with a tolerance of
`0.25 × B2_LINEAR_SLOP` (`distance.c`, the "Setup target distance and tolerance" block). It is
bounded at 20 outer iterations (`k_maxIterations`) and alternates bisection and false position
in the root loop ("Use a mix of false position and bisection"). The pose is interpolated
between the start and end transforms: centre linearly, rotation by `b2NLerp`.

So **the time of impact is not exact**: a Body is stopped one Slop (5 mm) *inside* the target,
by design, so the next step's detection sees a touching Contact. Measured in the Go port below:
a radius-0.1 circle aimed at a wall face at `x = −0.05` stops with its centre at `x = −0.145`,
i.e. 5 mm deep.

The docs say it plainly: *"The TOI function might miss collisions that are clear at the final
positions. Nevertheless, it is very fast and adequate for tunnel prevention."*
(`docs/collision.md:270–271`).

### Which pairs it covers

`b2SolveContinuous` (`src/solver.c:383`) queries, per Shape of the fast Body, with the box that
bounds the Shape at both ends of the step (`solver.c:441`, `b2AABB_Union( box1, box2 )`):

- the **static** tree, always (`solver.c:443`);
- the **kinematic** and **dynamic** trees **only if the Body is a bullet** (`solver.c:445–449`).

The callback (`b2ContinuousQueryCallback`, `solver.c:183`) then skips: the same Body, sensors
unless both sides want sensor events, filtered pairs (`b2ShouldShapesCollide`,
`b2ShouldBodiesCollide`, the custom filter), and **any other bullet** (`solver.c:228–232`). A
sensor Shape on the fast Body is never the moving Shape (`solver.c:435–438`).

A bullet's targets are at their **end** poses: a non-fast Body has `center0 = center` written in
finalize before bullets run (`solver.c:689–693`), so `b2MakeRelativeSweep` of a target yields
no motion. Relative motion is ignored. The body definition says so: *"the bullet does a
continuous check after all non-bullet bodies have moved. You could get unlucky and have the
bullet body end a time step very close to a non-bullet body and the non-bullet body then moves
over the bullet body."* (`include/box2d/types.h:267–281`).

### The gate

In `b2FinalizeBodiesTask` (`solver.c:579` onwards), per awake Body, per step:

```
maxVelocity      = |v| + |ω|·maxExtent                          (solver.c:627)
maxDeltaPosition = |Δposition| + |sin Δangle|·maxExtent          (solver.c:630)
maxMotion        = max(maxDeltaPosition, maxVelocity·dt)         (solver.c:672)
fast  ⇔  Dynamic ∧ enableContinuous ∧ maxMotion > safetyFactor·minExtent   (solver.c:673)
```

- **`safetyFactor` is per Body, default `0.5`** (`src/types.c:38`), documented as
  *"Recommended range [0.01, 0.5]. Default is 0.5 for high performance with low tunneling risk."*
  (`types.h:244–249`).
- **The comparison is strict.** A Body with `minExtent = 0` that does not move is not fast.
- **It is only evaluated for a Body that is not falling asleep** (`solver.c:666`): a Body under
  its sleep threshold skips the test.
- **v3.1.1 is simpler**: `maxVelocity * timeStep > 0.5f * sim->minExtent` (v3.1.1
  `solver.c:626`), velocity only, a constant 0.5, no per-Body factor. HEAD added the position
  term, so position correction that moved a Body counts too.

**The extent** (`b2ComputeShapeExtent`, `src/shape.c:923`), per Shape, taking the minimum over a
Body's Shapes (`body.c:559–560`):

| Shape | `minExtent` | `maxExtent` |
| --- | --- | --- |
| circle | radius | distance of centre from the centre of mass + radius |
| capsule (a rounded segment) | radius | farther end from the centre of mass + radius |
| polygon | **min over faces of the face's distance from the centroid**, + rounding radius | farthest vertex + rounding |
| segment, chain segment | **0** | farther end |

So Box2D's minimum extent is exactly the map's: a circle's radius, a segment's rounding radius, a
Polygon's inner radius plus rounding. It is computed when mass is updated, not per step.

### Where it sits in the step

`b2World_Step` (`physics_world.c:884`): update broadphase pairs (`:942`) → collide, i.e.
narrowphase at the *start* poses (`:979`) → solve (`:987`). Inside solve, after the constraint
stages, **finalize** writes each Body's final transform and runs the gate; a fast non-bullet
Body runs `b2SolveContinuous` **inline in the same parallel-for** (`solver.c:687`); fast bullets
are appended to an array and run afterwards in their own parallel-for (`solver.c:1884–1892`),
followed by a **serial** broadphase enlarge for bullet Shapes (`solver.c:1903–1940`).
`docs/simulation.md:2017–2049` gives the same order: *update transforms … performs continuous
collision between dynamic and static bodies* → hit events → refit BVH → *bullets*.

So it sits **after position is final and before the next step's detection**, and the non-bullet
pass needs no ordering between Bodies because its targets never move: *"This is deterministic
because the order of TOI sweeps doesn't matter."* (`solver.c:679`).

### Per-Body state it adds

On `b2BodySim` (`src/body.h:185–208`): `center0` and `rotation0` (the start pose of the step),
`minExtent`, `maxExtent`, and flag bits `b2_isFast`, `b2_isBullet`, `b2_enlargeBulletBounds`,
`b2_hadTimeOfImpact` (`body.h:26–42`). On `b2Body`: `safetyFactor` (`body.h:108`). Per Shape:
nothing new; the fat bounding box is reused. Per step: a stack array of awake-Body size for
bullet indices (`solver.c:1329`).

### What it does with the lost time

Drops it. The Body is placed at the interpolated pose at the impact fraction, and that pose
becomes the start of the next step (`solver.c:456–466`). Velocity is **not** touched, with one
exception new since v3.1.1: HEAD removes the gravity the Body did not get to fall through,
`dv = −(1 − fraction)·dt·gravityScale·gravity` (`solver.c:468–475`, *"Timeloss means there is a
lost gravity contribution. Other forces and torques are ignored for now."*). The next step's
narrowphase finds a touching Contact at the stopped pose and the solver takes the velocity from
there. The 3.0 release post names the cost of this design as *"minimal movement pauses (time
loss)"*.

Two more consequences a port would inherit:

- **A fast Body loses contact recycling** for that step (`physics_world.c:527–532`,
  `isFast == false && recycleDistance > 0.0f …`), so its Contacts are fully re-collided.
- **An initially overlapping pair is ignored**: at fraction 0 Box2D retries with a circle of
  radius `B2_CORE_FRACTION × minExtent = 0.25 × minExtent` about the Shape's centroid
  (`solver.c:348–359`), so a Body already touching a wall is not pinned there.

### Speculative contacts: the other half

Box2D v3 keeps a Contact point up to **`B2_SPECULATIVE_DISTANCE = 4 × B2_LINEAR_SLOP`**, 2 cm
(`include/box2d/constants.h:55`), before the Shapes touch (e.g. `manifold.c:54`, `:159`). In the
contact solver a point with positive separation `s` gets a velocity bias of `s / h`: the Bodies
may close exactly that gap this sub-step and no more (`contact_solver.c:327–332`, `:442–443`).
Static Shapes' bounding boxes are fattened by exactly this distance (`shape.c:92–99`).

It is what stops slow Bodies jittering, not a tunnelling defence: 2 cm is the whole reach. The
0.5 × `minExtent` gate covers what moves further. The 2023 plan post had proposed speculative
contacts as the whole answer (*"No sub-steps required!"*) and conceded *"Collisions can be missed
and there can be ghost collisions"*; what shipped is the hybrid: *"It uses a hybrid speculative
and time of impact approach"* ([Releasing Box2D 3.0](https://box2d.org/posts/2024/08/releasing-box2d-3.0/)).

### Cost

**Nothing is published for the pass itself.** The 3.0 post gives only *"v3 is more than twice as
fast as v2.4"*; the v3.1 notes say *"Faster continuous collision"* with no figure
(`docs/release_notes_v310.md:32`); the world profile times bullets (`profile.bullets`) but folds
the non-bullet pass into `transforms`.

**Measured here**, the in-tree benchmark (`benchmark/main.c`) with and without its own
`--no-continuous` switch, one worker, best of three runs per invocation, three interleaved rounds,
total milliseconds for the scene's fixed step count:

| scene (steps) | on, ms | off, ms | on − off |
| --- | --- | --- | --- |
| `rain` (1000) | 6289.6 / 6199.9 / 6193.1 | 5923.8 / 5834.7 / 5835.3 | **+6.1 %**, ≈ 0.36 ms a step |
| `smash` (300) | 937.8 / 962.0 / 948.1 | 818.5 / 829.0 / 832.9 | **+14.4 %**, ≈ 0.40 ms a step |
| `tumbler` (750) | 972.1 / 942.6 / 950.5 | 827.3 / 863.6 / 812.0 | **+15–17 %** |
| `washer` (500) | 3552 / 3654 / 5021 | 2622 / 2622 / 2983 | +35 % or more; noisy |
| `large_pyramid` (500) | 940.6 / 1006.8 / 888.1 | 905.8 / 897.9 / 923.5 | within noise |
| `many_pyramids` (200) | 1571.9 / 1642.0 / 1514.0 | 1649.1 / 1606.7 / 1580.9 | within noise |
| `junkyard` (800) | 2506.5 / 2407.8 / 2650.2 | 2548.4 / 2369.2 / 2408.6 | within noise |

Caveat: *off* is not the same simulation, because off lets things tunnel. Scenes where nothing
moves fast (stacks) pay nothing; scenes built of flung Bodies (`smash` has no Static geometry to
stop at at all, `benchmarks.c:465–505`) pay 14–17 %, which is therefore the gate tripping, the
tree query finding nothing, the bounding box recomputed, and contact recycling lost, rather than
time-of-impact work.

### What still tunnels

- **A non-bullet Dynamic body through a Kinematic or Dynamic body.** Only the static tree is
  queried (`solver.c:443–449`); `docs/loose_ends.md:105`: *"general dynamic versus dynamic
  continuous collision is not handled … This is done for performance reasons."*
- **A bullet through a bullet**, and a bullet through anything whose own motion carries it across
  the bullet's end pose (targets are at end poses).
- **Anything the conservative advancement misses** at a grazing angle (`collision.md:270–271`,
  the "Missed Collision" figure).
- **A fast Body clipping a chain segment by less than `0.25 × minExtent`**, which HEAD allows on
  purpose (*"Minimal clipping"*, `solver.c:258–282`), and anything behind a one-sided chain.
- **Joints** stretch: *"Continuous collision does not handle joints"* (`loose_ends.md:106`).
- **A sensor Shape** never runs the pass (`solver.c:435–438`); a fast solid Body *does* report the
  sensors it crossed, capped at `B2_MAX_CONTINUOUS_SENSOR_HITS = 8` (`solver.c:164`).
- **Rotation** is modelled (nlerp), but capped at `B2_MAX_ROTATION = 0.25π` a step, *"increasing
  this to 0.5f * B2_PI or greater will break continuous collision"* (`constants.h:48–51`).

---

## 2. Box2D v2.4: sub-stepping to the time of impact (contrast only)

Read at tag `v2.4.1`. Re-simulating the lost time is ruled out on the map; this is what it would
have cost.

- **Algorithm.** The same conservative advancement (`b2TimeOfImpact`), but driven by a global
  loop, `b2World::SolveTOI` (`src/dynamics/b2_world.cpp:585`): over **every Contact** in the
  world, find the minimum time of impact; advance the two Bodies there; build a mini island of
  those two Bodies and their Static, Kinematic or bullet neighbours; **re-solve the remaining
  `(1 − α)·dt`** with 20 position iterations (`b2_world.cpp:868`, `b2Island::SolveTOI`); commit
  the moved proxies; repeat.
- **Pairs.** A Contact is considered if either side is a bullet or not Dynamic
  (`collideA = bA->IsBullet() || typeA != b2_dynamicBody`, loop body line 76 of `SolveTOI`):
  Dynamic against Static and Kinematic always; Dynamic against Dynamic only with a bullet.
- **Gate. None by speed.** Every such Contact gets a time of impact computed every step, cached
  per Contact until a Body is displaced. The broadphase proxy covers the union of the start and
  end boxes (`b2Fixture::Synchronize`, `src/dynamics/b2_fixture.cpp:167–176`) so the pair exists.
- **Placement.** After the whole regular step (`b2_world.cpp:951–954`), serial.
- **Per-Body state.** `b2Sweep` (`localCenter, c0, c, a0, a, alpha0`, `include/box2d/b2_math.h:382–388`);
  per Contact `m_toi`, `m_toiCount`, a flag. Caps `b2_maxSubSteps = 8` per Contact and
  `b2_maxTOIContacts = 32` per island (`b2_common.h:77–83`).
- **Cost.** Serial by construction: *"This is inherently serial and precludes multithreading. It
  is also very expensive."* ([Starting Box2D 3.0](https://box2d.org/posts/2023/01/starting-box2d-3.0/)).
  No number published.
- **Lost time.** Re-simulated, which is the sub-step the map dropped.
- **Still tunnels.** Dynamic through Dynamic without a bullet; any pair past 8 sub-steps.

---

## 3. Rapier 0.35: Box2D v3's design, adopted in August 2026

Rapier **rewrote its CCD in `v0.35.0-beta.0` (2026-08-02)** to Box2D v3's model, and says so:
*"The CCD solver was rewritten around sweep-based time of impact: each fast body is clamped to
its earliest impact (pose only; velocities resolve through speculative contacts) … Fast dynamic
bodies now always run CCD against fixed colliders; `ccd_enabled` upgrades a body to a 'bullet'
that also sweeps kinematic and dynamic bodies."* (`CHANGELOG.md:145–152`). Parry's new module
opens *"NOTE: this is mostly ported from Box2D which had much better CCD quality than Rapier."*
(`parry src/query/sweep_toi/mod.rs:9`). The user guide (`website/docs/user_guides/templates/rigid_body_ccd.mdx`)
still describes the old opt-in behaviour and is stale against the code.

### Algorithm

`parry::query::sweep_time_of_impact`: conservative advancement with separation functions,
target `max(slop, totalRadius − slop)`, tolerance `0.25 × slop` (`parry src/query/sweep_toi/sweep_toi.rs:79–83`),
the same false-position / bisection mix (`:200–230`). Motion is endpoint-interpolated
(`Sweep::from_poses`). A non-proxy shape falls back to Rapier's older nonlinear shape cast
(`src/dynamics/ccd/sweeps.rs:480–519`). The initial-overlap fallback is Box2D's core circle,
`CORE_FRACTION × min_extent` (`sweeps.rs:458–471`).

### Pairs

Non-bullets test **fixed** colliders (and soft-body meshes) only (`sweeps.rs:22–48`,
`is_auto_target`); `ccd_enabled` Bodies test everything but other bullets
(`src/dynamics/rigid_body.rs:590–595`: *"A bullet never sweeps another bullet, so two bullets can
still tunnel through each other."*). Targets are stationary at their end-of-step pose
(`sweeps.rs:108–111`). Meshes, polylines, heightfields and voxels are never the moving shape
(`sweeps.rs:96–106`).

### The gate

`RigidBodyCcd::is_moving_fast_with_next_position` (`src/dynamics/rigid_body_components.rs:1155–1180`):
`max(|Δcom| + |sin Δθ|·max_extent, max_point_velocity·dt) > 0.5 × ccd_thickness`, with
`FAST_BODY_SAFETY_FACTOR = 0.5` a constant (`:1149`). It is Box2D HEAD's test term for term.

`ccd_thickness` is **not** Box2D's inner radius for polygons: parry gives a circle and a capsule
their radius, a cuboid its smallest half-extent, and a convex polygon **the smallest half-extent
of its local bounding box** (`parry src/shape/shape.rs:1260–1263`, marked *"TODO: we should use
the OBB instead"*); a segment and a triangle 0. A round shape adds its border radius
(`shape.rs:1607–1608`). The Body takes the minimum over its colliders, excluding shapes that are
never moved through the pass (`rigid_body_components.rs:1245–1252`).

The gate is **fused into the solver's parallel body write-back** (`src/dynamics/solver/staged_island_solver/worker.rs:995–1001`,
*"Fused post-solve CCD activation … replaces the pipeline's serial post-solve walk over every
active body"*), and the pass is skipped entirely when no Body tripped it
(`src/pipeline/physics_pipeline/substep.rs:524–546`). That is the same economy the map asks of
Integrate.

### Where it sits

`substep.rs:500–552`: solve velocities and integrate to `next_position` → gate → `solve_continuous`
clamps `next_position` → `advance_to_final_positions` → next detection. Pass 1 (non-bullets vs
fixed) runs in parallel over Bodies; pass 2 (bullets vs everything) runs after, reading pass 1's
clamped poses (`src/dynamics/ccd/ccd_solver.rs:190–270`). The fixed targets are a cached flat
list, invalidated on scene change (`ccd_solver.rs:28–35`).

### Per-Body state

`RigidBodyCcd` (`rigid_body_components.rs:1074–1094`): `ccd_thickness`, `ccd_active`,
`ccd_enabled`, `soft_ccd_prediction`, `allow_fast_rotation`. The start pose is the existing
`position` beside `next_position`.

### Lost time

Dropped, pose only, velocity kept: `apply_clamps`, *"Pose only — velocities are preserved."*
(`ccd_solver.rs:331–345`). Unless `IntegrationParameters::max_ccd_substeps > 1` (default `1`,
`src/dynamics/integration_parameters.rs:293`, `:419`), which splits the **whole world's** step at
the first impact found anywhere (`substep.rs:433–490`): a global sub-step, the thing the map
dropped.

### Soft CCD

Per Body, opt-in, `soft_ccd_prediction` distance `d` (added in v0.19.0, 2024-05-05,
`CHANGELOG.md:661–664`). No gate and no time of impact:

- the broadphase box is grown to cover the pose predicted from velocity and forces, capped at `d`
  (`src/geometry/collider.rs:613–640`);
- the narrowphase prediction distance for the pair becomes `max(prediction, dt·|v₁ − v₂|)`, each
  velocity clamped to `d/dt` (`src/geometry/narrow_phase/pair_update.rs:326–349`);
- the solver then treats the far-apart points as speculative Contacts.

It covers **every** pair the Body is in, Dynamic included, and relative motion properly. It costs
broadphase pairs and manifolds for every tick the Body carries it, fast or not: *"Large values
can impact performance badly by increasing the work needed from the broad-phase."*
(`rigid_body.rs:606–616`). What it misses is what speculative contacts miss: a contact point
chosen at the start pose that no longer describes the geometry by the end.

### Cost

**No number published.** Two qualitative statements in the source: growing the swept boxes into
the tree instead of querying with them was rejected because *"Swept AABBs in the tree leak into
the next step's broad phase (pair explosion, ~2x narrow-phase cost)"* (`ccd_solver.rs:180–182`),
and *"re-scanning every collider each step dominated CCD on large scenes"* (`ccd_solver.rs:28–30`).
Not measured here.

### Still tunnels

A non-bullet through Kinematic and Dynamic bodies; bullet through bullet; mesh-like moving
shapes; a Polygon whose bounding-box half-extent overstates its thinnest width (the gate trips
later than Box2D's).

### Before 0.35

For the record: v0.7 to v0.34 CCD was opt-in per Body, *"motion-clamping, i.e., each fast-moving
rigid-body with CCD enabled will be stopped at the time where their first contact happen"*
(`CHANGELOG.md:1162–1168`), against every collider with nonlinear shape casts. Rapier replaced it.

---

## 4. What Go already ports

### `ByteArena/box2d` — Box2D 2.3.2, sub-stepping

The v2 design of section 2, transcribed: `B2World.SolveTOI` (`DynamicsB2World.go:608`), the pair
rule (`:672–673`), the dispatch from `Step` (`:930–932`), `B2_maxSubSteps = 8`
(`CommonB2Settings.go:57`), swept broadphase boxes (`DynamicsB2Fixture.go:321–325`).
`B2TimeOfImpact` measured **571 ns/op, 0 allocs** as a standalone query
(`docs/research/go-2d-physics-libraries.md` § 2.1 on `research/go-2d-physics-libraries`), and it
**writes unsynchronised package globals on every call** (`CollisionB2TimeOfImpact.go:53–55`,
`:288`), a data race under cog's concurrent Systems (§ 2.2 of the same note). It re-simulates the
lost time, which is ruled out.

### `neguse/gox2d` — Box2D v3.1.1, the design in section 1

The libraries survey found `gox2d`'s `ShapeCastSegment`; it did not look at the solver. **The
continuous pass is ported too**: `solveContinuous` (`box2d/solver.go:306`), the v3.1.1 gate
`maxVelocity*timeStep > 0.5*sim.MinExtent` (`solver.go:544`), the bullet stage (`solver.go:980`),
the static-only query and the core-circle fallback (`solver.go:211–276`), the polygon inner-radius
extent (`box2d/shape.go:840–851`). `TimeOfImpact` (`box2d/distance.go:781`) keeps no package
state. It is float32.

**Measured**, a scratch module over `gox2d` with a local `replace`: 1 024 Static walls
(boxes 0.1 × 2 m, a 32 × 32 grid, 4 m apart), 1 024 Dynamic circles of radius 0.1 (so the gate
is 0.05 m), no gravity, no sleeping, 60 Hz, four solver sub-steps (Box2D's default). Before every
step each circle is put back at its start with its velocity reset, so every step is the same
step. Three scenes, each with the pass on and off, interleaved, five rounds, `-benchtime 2s`:

| scene | per step | on, ns per Body (median of 5) | off | on − off |
| --- | --- | --- | --- | --- |
| **hit**: 36 m/s, starts 0.3 m before a wall, would pass clean through | 0.6 m | 1 237 | 727 | **+510 ns** |
| **miss**: 36 m/s in open space, nothing within reach | 0.6 m | 894 | 680 | **+214 ns** |
| **slow**: 1 m/s, under the gate | 0.017 m | 114.5 | 113.6 | **+0.9 ns**, pairwise −0.6 to +7.6: noise |

A sanity test confirms the pass works: with it on, a hit circle stops at `x = −0.145` (5 mm into
the face at −0.05); off, it ends at +0.3, through the wall.

So in this Go port **a Body that trips the gate costs about 214 ns before it finds anything and
about 510 ns when it does**, against a whole step of 680–730 ns a Body in the same scene; and a
Body below the gate costs nothing a benchmark can see. The *hit* figure includes everything the
engine does differently after a stop (a bounding box at the clamped pose, a proxy moved, a
touching Contact next step), and the scene is the worst case: every Body trips and every Body
hits.

---

## 5. Side by side

| | Box2D v3 (HEAD) | Box2D v2.4 | Rapier 0.35 | Rapier soft CCD | `gox2d` (v3.1.1) | `ByteArena` (2.3.2) |
| --- | --- | --- | --- | --- | --- | --- |
| **Algorithm** | conservative advancement, then stop | conservative advancement, then re-solve | conservative advancement (ported from Box2D), then stop | speculative Contacts over a grown reach | as Box2D v3 | as Box2D v2 |
| **Dynamic vs Static** | always, when gated | always | always, when gated | opt-in, every tick | always, when gated | always |
| **Dynamic vs Kinematic** | bullet only | always | bullet only | opt-in | bullet only | always |
| **Dynamic vs Dynamic** | bullet only, targets at end pose, never bullet vs bullet | bullet only | same as Box2D v3 | opt-in, relative motion | same as Box2D v3 | bullet only |
| **Gate** | `max(Δpos + |sin Δθ|·maxExt, (|v|+|ω|·maxExt)·dt) > safety·minExtent`, safety 0.5 per Body | none (per Contact, every step) | same test, 0.5 constant | none | `(|v|+|ω|·maxExt)·dt > 0.5·minExtent` | none |
| **Extent** | Polygon inner radius + rounding; circle, capsule radius; segment 0 | — | cuboid min half-extent; polygon min AABB half-extent; circle, capsule radius; segment 0 | — | as v3 | — |
| **Where** | end of step, after positions are final, before next detection; inline in the parallel finalize | after the step, serial loop | after solve, before next detection; parallel per Body | broadphase and narrowphase | as v3 | after the step, serial |
| **Per-Body state** | start pose, min/max extent, 4 flag bits, safety factor | `b2Sweep` + per-Contact TOI | thickness, 2 bools, prediction | prediction distance | as v3.1.1 | `B2Sweep` + per-Contact TOI |
| **Cost evidence** | none published; measured +6 % (`rain`) to +14–17 % (`smash`, `tumbler`) of the step, 0 on stacks | "very expensive", serial; none published | none published | "can impact performance badly"; none published | measured +214 ns / +510 ns per gated Body, ~0 below the gate | 571 ns per TOI call (survey) |
| **Lost time** | dropped; HEAD removes the lost gravity from velocity | re-simulated | dropped, velocity kept (unless world sub-steps) | none lost | dropped | re-simulated |
| **Still tunnels** | non-bullet through Kinematic/Dynamic; bullet through bullet; grazing misses; chain clipping < 0.25·minExtent; joints | Dynamic through Dynamic w/o bullet; past 8 sub-steps | as Box2D v3; polygons gated late | stale start-pose contact points | as Box2D v3.1.1 | as v2.4; racy globals |

---

## 6. What this means for ecsphysics2d

Facts and their consequences under the settled constraints. The choice is
[#577](https://github.com/dvoyni/cog/issues/577)'s.

**The mechanism every engine converged on already fits the settled answers.** Dropping the lost
time, stopping the Body at the first impact, leaving velocity to the next tick's Contact, and
gating by a fraction of the minimum extent is Box2D v3's design and, since August, Rapier's. v2.4's
re-solve is the only algorithm here that needs a sub-step, and it is serial and ungated. Soft CCD
is the only one without a gate, so it pays every tick a Body carries it, fast or not: it cannot meet
the "one compare, no new dispatch when nothing is fast" bar.

**The gate's constant differs from both engines by a factor of two.** Box2D and Rapier trip at
`> 0.5 × minExtent`; the map's gate trips at `≥ minExtent`. For a circle against a zero-thickness
segment, displacement equal to the radius is exactly where the centre can cross the segment's line,
so push-out goes the wrong way; below it, ordinary Contact pushes the right way. Box2D's 0.5 is a
safety margin over that boundary, *"for high performance with low tunneling risk"*, and it also
covers rotation, which the map puts out of scope. A factor of 1 engages fewer Bodies. This is a
fact to price, not a reopening.

**A minimum extent of 0 needs a rule.** A bare segment Shape and a point (circle of radius 0) have
minimum extent 0, as in Box2D. With the map's `≥`, `|Current − Previous| ≥ 0` is true for a Body
that did not move at all, so such a Body would engage every tick. Box2D's strict `>` makes a still
zero-extent Body not fast and a moving one always fast.

**The settled guarantee is wider than either engine's default.** Both engines test a non-bullet
Dynamic body against Static geometry only. cog promises Static **and Kinematic**. Kinematic bodies
live in the **Body index**, not the Static index (`CONTEXT.md`, *Body index*), so every engaged Body
would query the Body index as well as the Static index. The *miss* measurement (+214 ns) is one
tree query that found nothing; a second index query is of that order again, once per engaged Body.
Dynamic against Dynamic is the opt-in Component, as it is `isBullet` / `ccd_enabled` in both
engines, and both treat the other Body as standing still at its end pose. That is exactly cog's
existing rule for two moving Sensors (*tested against each other's end positions*).

**Where the work can sit without widening a lock set.** From § The five Systems' table:

| needs | who already holds it |
| --- | --- |
| the displacement `Current − Previous` | Integrate writes `Position` |
| the Shape's minimum extent | Index and Detect read `Shape`; **Integrate does not** |
| the Static index and Body index, and the Shape at both poses | Detect reads `Shape`, `Position`, `StaticIndex`, `BodyIndex` |
| writing a Contact with `T < 1`, `Depth 0` | Detect writes `Contacts` |
| moving `Position.Current` back | Integrate and Solve write `Position`; Detect does not |

- The compare "folded into Integrate" needs the minimum extent where Integrate can read it.
  Integrate's locks are `Velocity`, `Sleeping` read and `Position` write. Reading `Shape` from
  Integrate **widens its lock set**, which the map forbids. The per-Shape constant would have to
  reach Integrate through a Component it already holds (a field on `Position` or `Velocity`, 8 B a
  Body, kept in step with the Shape) or the compare moves to a System that already reads `Shape`.
- Both engines put the pass **after the position is final and before the next detection**. In
  cog's order that is between Integrate and Detect. Detect already holds everything the test reads
  and already writes the Contact the map settled on (*`T` below 1 and `Depth` 0, as a Probed Sensor
  does*). It does not write `Position`; Solve does, and runs after it, so a Contact carrying `T`
  could be applied by Solve. That is a lock-set observation, not a design.
- Box2D runs the non-bullet pass **inline in the parallel finalize, per Body, with no ordering
  between Bodies**, because its targets do not move. The same property holds in cog for Static
  targets and for Kinematic targets at their end pose, which is what lets the work split per Body.

**The primitive cog already has.** cog's Probe is an exact swept circle measured at **103 ns** on
the Static index (§ The queries and the indices). For a circle Body it is the whole test, exact,
with no Slop-deep stop. Box2D's own fallback for anything else is a **circle of `0.25 × minExtent`
about the centroid** (`B2_CORE_FRACTION`); the inscribed circle, radius `minExtent`, is the largest
circle guaranteed inside a Polygon. A Polygon Body tested only by its inscribed circle is stopped
when its core would cross something, and can still clip a corner through something thinner than
its overhang; a swept Polygon test (the map's *Probing a box*) is what closes that gap. Box2D and
Rapier both use GJK conservative advancement, which stops one Slop deep, by design, for every
Shape.

**The price to beat.** From `gox2d`, the same design in Go: about **+214 ns per engaged Body per
tick** with nothing found and **+510 ns** with an impact, **~0 per Body below the gate**. From
Box2D in C: **0 %** of the step on stacked scenes and **6–17 %** on scenes made of flung Bodies,
most of which is the query and bookkeeping rather than the time of impact itself. cog's Probe (103
ns, float64, exact for circles) is below `gox2d`'s engaged-Body cost before it does any
bookkeeping. The reference scene the map calls for is what turns these into a frame cost.
