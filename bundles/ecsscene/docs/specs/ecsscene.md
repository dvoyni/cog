# cog ecsscene — specification

`github.com/dvoyni/cog/bundles/ecsscene` is the ECS's renderer. It draws
Entities over `model`, beside `scene` rather than on top of it: **it records to
`gfx` itself and imports nothing of scene.** This document is its design record:
what it is made of, why each part is shaped the way it is, what it is tested
against, and the shapes that were ruled out. The API a game writes against is
[`../README.md`](../README.md). The general shape every binding of the ECS
takes is [`ecs.md` §Binding](../../../ecs/docs/specs/ecs.md#binding-how-another-plugin-attaches).

**It repeats scene's path, and is not an improvement on it.** ecsscene draws
what scene would draw for the same frame: the same passes, the same labels, the
same instances. It does not do anything scene does not do. The one thing it
does differently is the call shape: it groups Entities into **Batches**, where
scene draws every call on its own. That difference is what the redesign was for.

Two standing rules come from [the model spec](../../../model/docs/specs/model.md),
and nothing below reopens them:

- **An app runs `scene` or `ecsscene`, never both.** Running both is undefined
  behaviour. Nothing is designed to make it work, and nothing guards against it.
- **Unloading is cleanup between scenes.** An app unloads a model only once no
  Entity draws it. Drawing an unloaded model is undefined behaviour. There are
  no generations and no invalidation.

This section of the design used to live in `model.md`, as the plan for stage 4
of the split. It moved here when the redesign landed on main
([#538](https://github.com/dvoyni/cog/issues/538)), in one merge carrying
[#535](https://github.com/dvoyni/cog/issues/535) (the Components and the
vocabulary), [#536](https://github.com/dvoyni/cog/issues/536) (the load System)
and [#537](https://github.com/dvoyni/cog/issues/537) (the recording System).
Before it, ecsscene was a binding into `scene.OpQueue`: one `OpQueue.Model` or
`OpQueue.Mesh` call per Entity, recorded by scene's flush.

---

## Contents

- [Why it stopped proxying into scene](#why-it-stopped-proxying-into-scene)
- [Its Components](#its-components)
- [Its own copy of the vocabulary](#its-own-copy-of-the-vocabulary)
- [Its own copy of the frame code](#its-own-copy-of-the-frame-code)
- [The load System](#the-load-system)
- [The recording System](#the-recording-system)
- [Batches](#batches)
- [What it is tested against](#what-it-is-tested-against)
- [What it costs](#what-it-costs)
- [Shapes that were rejected](#shapes-that-were-rejected)
- [Out of scope](#out-of-scope)

---

## Why it stopped proxying into scene

From [where ecsscene's frame time goes](https://github.com/dvoyni/cog/issues/493),
measured for 5 000 crates in view at `6a5254d` (the table is in
[model.md §What was measured](../../../model/docs/specs/model.md#what-was-measured)).

- **The call shape was the cost.** One scene call per Entity made 5 000 Batches.
  The same crates as one call were 2.7 ms against 8.2 ms.
- **Proxying into scene itself was about 7%.** Sort, cull and layering were
  under 0.4% together.
- **Re-keying a model's own material every draw was 20%** of the per-Entity
  frame. The key now belongs to the loaded model material, taken once at load.

So ecsscene draws Batches, keyed on change, and reaches `gfx` without scene's
queue in between. It could not batch through scene, because scene never merges
separate calls ([#49](https://github.com/dvoyni/cog/issues/49)), and making it
merge them is out of scope.

---

## Its Components

From [#495](https://github.com/dvoyni/cog/issues/495). Landed in
[#535](https://github.com/dvoyni/cog/issues/535).

**ecsscene wraps `model`'s values in Components of its own**, passed through
unconverted. It does not register `model`'s types: registering `model.MeshRef`
would claim the one Store that Go type can have, and a second ECS plugin naming
it would get `ErrDuplicateRegistration`.

| Component | shape |
| --- | --- |
| `Model` | `{Ref model.ModelRef; Layers LayerMask}` |
| `Mesh` | `{Ref model.MeshRef; Bounds m.Vec4; Layers LayerMask; NeverCull bool}` |
| `Animation` | `{Plays [model.MaxClipPlays]model.ClipPlay}`. ecsscene's own `MaxPlays` is gone |
| `Params` | `{Values m.List[gfx.ParameterDescr]}` |
| `Material` | `{Tags m.List[MaterialTag]}`, where `MaterialTag.Tag` is ecsscene's own `PassTag` |
| `Light` | `{Descr model.LightDescr; Layers LayerMask}`. The recording System writes `Descr.Position` and `Descr.Direction` from the Entity's Transform, and the Component documents both as ignored |
| `Camera` | flat, with scene's field names. `Passes` is an `m.List[Pass]` |

**Where an Entity stands is not one of them.** It is an `m.Transform`, whose one
Store the ecs plugin registers, so a game's Systems and other bindings read the
same placement ecsscene draws from.

**`Model` and `Mesh` spell out their tail padding, and `model.MeshRef` its
padding after the source.** The ECS compares a Changed record by its bytes, and
Hooks with Changed refuse implicit padding in a validating build
([#536](https://github.com/dvoyni/cog/issues/536)). It is a layout change with
no API change.

---

## Its own copy of the vocabulary

From [#494](https://github.com/dvoyni/cog/issues/494). Landed in
[#535](https://github.com/dvoyni/cog/issues/535).

**ecsscene declares its own copies of scene's camera, layer and pass types**,
each keeping scene's name, shape and zero-value meaning:

- `LayerMask`, with `LayersAll` and `Layer`;
- `PassTag`, with `TagForward`;
- `Pass`, with its six fields;
- `CameraID`, over `gfx.Order`;
- `ProjectionKind`, with `Perspective`, `Orthographic` and `Oblique`.

`LightKind` is `model`'s. Projection maths is `libs/m`'s. **There is no third,
shared renderer bundle**: what both renderers need is `model`'s or `libs/m`'s,
and a shared bundle would bring back the coupling the split removed.

**It reports scene's errors under its own names**, declared again in its root's
`err.go`: the camera, pass, material-tag and custom-layout errors. A game reading
an ecsscene error never imports scene to name it.

---

## Its own copy of the frame code

From [#521](https://github.com/dvoyni/cog/issues/521). Landed in
[#537](https://github.com/dvoyni/cog/issues/537).

**ecsscene copies the frame code that does not depend on the shader** into its
own `internal/`, adapted to Entities: the arena and `recordBytes`, culling and
draw preparation, the sort keys and `sortEntries`, the material table, light
selection and pass resolution, and the emission loop. The arena and the sort are
scene's verbatim; the rest walk Entities where scene walks recorded calls.

**What depends on the shader is `model`'s, and never copied**: the records the
bundled shader reads, their packers (`PackInstance`, `PackLight`,
`PackFrameLighting`, `AppendAnim`), their size constants and their binding
names. A shader change reaches both renderers at once, because there is only
one copy of what it reads.

The two copies of the frame code are expected to differ in their details, since
ecsscene draws Batches and scene does not.

---

## The load System

From [#497](https://github.com/dvoyni/cog/issues/497) and
[#509](https://github.com/dvoyni/cog/issues/509). Landed in
[#536](https://github.com/dvoyni/cog/issues/536), behind `ecsscene.LoadOnUpdate`,
and ordered `Before[RecordOnUpdate]`.

**It runs on what changed**: `Hooks[T, HookAll]` of `Model`, `Mesh`, `Material`
and `Params`, taking each touched Entity once however many Hooks name it. It
holds `kernel.Write[*model.Lookup]`, the filesystem and the resource queue, so
it is the one ecsscene System that loads. For each Entity it:

- resolves the `ModelRef` to a `ModelHandle` through the load facade, loading
  the model if needed, and applies the ref's selectors through the read facade's
  view, reporting a selector error once;
- computes the Batch keys: (`ModelHandle`, primitive, material key, `Params`
  hash) for each primitive of a `Model`, and (`MeshRef`, material key, `Params`
  hash) for a `Mesh`;
- stores them in its key scratch, keyed by Entity, and **never in a Component**,
  since writing the Component would make this System its writer.

**A primitive is named by the `MeshRef` it draws**, not by its index. An index
in a selector's view is not its index in the model, and `model` does not export
the view's start.

**The material key** is the model material's load-time key for its variant, or,
with a `Material` override, a content key over each tag and its gfx fingerprint.
Zero is the bundled PBR a `Mesh` with no `Material` draws with. **The `Params`
hash** is `gfx.FingerprintParams`, and absent or empty `Params` hash to zero.

It also:

- **drives `model`'s bake and release queues** every frame. Nothing else drains
  them in an ecsscene app, since scene's flush is not composed;
- **ensures the bundled PBR** once the backend is up, and passes its materials
  and the backend's readiness to the recording System through the key scratch,
  because the recording System holds the Lookup only for reading;
- **walks no Entity in a steady frame.** No Hook names anything, so a steady
  frame hashes no material and no parameter.

**A model that fails to load stays unkeyed** until its Component changes again,
because the Lookup caches every load failure but a missing backend. Entities
touched before the backend is up wait for the first frame that has one. **An
Entity that loses both `Model` and `Mesh`, or is despawned, leaves the scratch.**

**Apps may still preload through the load facade** to choose when an upload
happens.

---

## The recording System

From [#509](https://github.com/dvoyni/cog/issues/509) and
[#521](https://github.com/dvoyni/cog/issues/521). Landed in
[#537](https://github.com/dvoyni/cog/issues/537), behind
`ecsscene.RecordOnUpdate`.

**Its lock set is its signature**: every Store it queries, the load System's key
scratch, `kernel.Read[*model.Lookup]` and `*gfx.Viewport` read; its own scratch
and `*gfx.OpQueue` written. It reaches the Lookup only through
`model.NewLookupReadAccess`, so it never loads. Each tick it:

1. buckets every placed `Model` primitive and `Mesh` into the Batch its key
   names. A Batch resolves its mesh, material, properties record and `Params`
   once, from the first Entity bucketed into it, and each instance keeps its own
   world matrix, bounds, layers and animation offset;
2. packs every light through `model.PackLight`, and every camera's passes;
3. for each camera and pass, culls every instance by the camera's layer mask and
   the pass's frustum, then filters each Batch's survivors by whether its
   material serves the pass's tag;
4. sorts: opaque by material and Batch, blended back to front;
5. packs one properties record and one instanced draw per opaque Batch, and one
   draw per blended instance, through `model`'s packers, and emits the arenas
   and the passes into `*gfx.OpQueue` under `model`'s binding names.

A frame before the backend is up, or with no window, is skipped whole, as
scene's flush skips it.

**Passes keep scene's labels, `scene.camera<ID>.<tag>`.** The same frame through
either renderer reaches gfx with the same labels, which is what lets the two
fountains be compared pass by pass, and a HUD reading `ArmFrameCmd` read both.
A pass no material serves is still emitted, with its clear.

**Batches also split by shader variant.** It changes nothing for a file's own
material, whose key is per variant already. It stops an override `Material`
over a skinned and an unskinned use of one mesh from sharing group-2 bindings
that only one of them declares.

**An Entity whose model has no skin or morph packs no animation block.** scene
appended one nobody referenced; the instance output is the same.

**The lock it writes costs no ordinary System its parallelism.** It writes
`*gfx.OpQueue`, which only Last-phase flushes and gfx's own handlers write.
The one new contention is a game System that *writes* the Lookup: it now
serialises against recording, where before scene's Last-phase flush held that
lock. **One recording System per bound plugin is the shape**
([`ecs.md` §Binding](../../../ecs/docs/specs/ecs.md#the-wide-lock-lands-in-the-bound-plugin-not-in-the-ecs)):
`*gfx.OpQueue` is one resource, so two recording Systems would serialise
anyway.

**It declares no ordering beyond the load System's `Before`.** gfx subscribes
`gfx.PresentOnUpdate` `Last`, so what the recording System draws in a tick is in
what that tick presents. A game System that moves Transforms orders itself
`Before[ecsscene.RecordOnUpdate]()`; one that spawns drawables or writes their
Components orders itself `Before[ecsscene.LoadOnUpdate]()`.

---

## Batches

From [how ecsscene groups Entities into instanced draws](https://github.com/dvoyni/cog/issues/509).

**ecsscene builds its Batches every frame, in its recording System, from keys
written on change.** Sorting measured under 0.4% of a frame, so rebuilding them
is cheap, and a steady frame never hashes a material.

**Not in the key:** `LayerMask`, culling and the camera. They filter instances
inside a Batch for each pass, the way scene packs only a Batch's survivors. One
Batch can draw a different subset in each pass.

**What splits a Batch** is anything that changes the key: a different mesh or
primitive, a different `Material` (so a different pipeline), or different
`Params` values. **Equal `Params` batch.** 5 000 crates all tinted red are one
Batch, and 5 000 crates tinted differently are 5 000 Batches, as in scene.
Per-instance properties, which would make a tinted crowd one draw, are
[#520](https://github.com/dvoyni/cog/issues/520).

**Skinned and morphed Entities that share a mesh and material share a Batch.**
Each instance points at its own animation block through the instance record's
`AnimOffset`, which the shader reads per instance (verified; see [model.md §The
shader's records](../../../model/docs/specs/model.md#the-shaders-records-and-their-packers)).
The fallback #509 kept in reserve, a hash of `Plays` joining the key, is not
needed.

**Blended materials stay one draw per instance**, sorted back to front across
Batches, as in scene. An alpha-tested cutout is `BlendOpaque`, so it batches.

**Batching is always on, with no Component to opt out.** It costs a game nothing
it would opt into, and the only thing it changes that a game could observe is
the order of opaque draws, which is already unspecified. It is not a heuristic,
because equal keys batch deterministically.

---

## What it is tested against

From [what ecsscene asserts against once there is no Op to read back](https://github.com/dvoyni/cog/issues/501).

**The oracle is a recording `gfx.Backend`.** The harness publishes `RenderEvent`
as well as `UpdateEvent`, and tests assert on what gfx received:

- the vertex and index buffers bound;
- draw and instance counts;
- pass descriptors and clears;
- the baked bytes of the instance, camera and light buffers, decoded against
  `model`'s record layouts;
- the `SetParams` bytes.

Tests in the same package may also read ecsscene's scratch as a white-box extra,
never as the only assertion on a behaviour.

**ecsscene keeps its own `testBackend`, in `_test.go`**, modelled on scene's. It
also records `SetParams` bytes, which scene's drops, and supplies a
`ShaderLayout` for each variant so gfx keeps the parameters.

**ecsscene publishes no inspection API.** Counts a game or a HUD needs come from
gfx's `ArmFrameCmd`.

**Tests assert properties, not Batch shape.** A test checks which mesh drew how
many instances with which records. Only the Batch tests (`batch_test.go`) assert
exact Batch counts: 5 000 identical crates are one draw of 5 000, 5 000 distinct
tints are 5 000 draws, blended panes around smoke are four draws back to front,
and animated Entities share a Batch with an animation block each.

**Layer and pass routing have their own tests** (`routing_test.go`): layers
across three cameras, passes by material tag, and frustum culling inside a
Batch. scene does that routing too and has never tested it; that gap is
scene's.

**The five benches** (`BenchmarkFrameEmpty`, `Frame5000`, `FrameAnimated5000`,
`FrameParams5000`, `FrameMaterial5000`) draw: a camera, a model made resident
from an in-memory file system, `UpdateEvent` plus `RenderEvent`, and a backend
that reports `Ready()` and whose sinks replay and discard. They assert nothing.

**`TestRecordingAllocatesNothingPerEntity` now holds only trivially.** Its
harness uses a backend that never becomes Ready, and the recording System skips
such frames whole. The drawn benches carry the allocation claim instead. Giving
the test a drawing harness is a follow-up.

**The fountains compare the two renderers frame to frame.** `cmd/ecs/fountain`
and `cmd/scene/fountain` in cog-examples draw the same frame at the same step,
sharing their simulation in `internal/fountain`. At the reference step both are
held to the same passes, labels and instances in each pass, and the ecs
fountain's draws in each pass are held to no more than the scene fountain's.
At step 600 the scene fountain's forward pass is 117 draws of 117 instances and
the ecs fountain's is 116, because two motes thrown on one step fade to the same
tint and share a Batch. See [model.md §The
fountains](../../../model/docs/specs/model.md#the-fountains).

---

## What it costs

Interleaved runs of two built binaries, before (the proxy into `scene.OpQueue`,
`de8e5f9`) and after (`191003f`), 8 rounds, arm order rotated, each arm its own
process at 1 s. Ryzen 9 7950X3D, go1.27.1 windows/amd64. Means in ns/op, with
allocs/op:

| arm | before | after | ratio |
| --- | ---: | ---: | ---: |
| `FrameEmpty` | 31 687 (19) | 28 103 (18) | 0.89× |
| `Frame5000` | 14 162 122 (5020) | 1 105 657 (19) | 12.8× faster |
| `FrameAnimated5000` | 17 135 613 (10020) | 1 482 363 (20) | 11.6× faster |
| `FrameParams5000` | 15 283 788 (5020) | 1 126 376 (19) | 13.6× faster |
| `FrameMaterial5000` | 9 761 146 (20) | 1 122 856 (18) | 8.7× faster |

Every arm is one Batch, so a frame is one instanced draw where the proxy made
5 000. What is left is about 220 ns an Entity: the walk, the key lookup, the
cull and packing its instance record. **Allocations no longer scale with the
population.**

---

## Shapes that were rejected

Each was ruled out by the ticket named, most with the sequence that breaks it.

**Storability** ([#495](https://github.com/dvoyni/cog/issues/495)):

- **ecsscene registering `model`'s types directly.** It claims the one Store each
  Go type can have, so a second ECS plugin gets `ErrDuplicateRegistration`.

**Batches** ([#509](https://github.com/dvoyni/cog/issues/509)):

- **scene merges calls.** ecsscene no longer passes through `scene.OpQueue`, so
  the merge is not on its path, and 5 000 crates would stay 5 000 draws.
- **The ECS keeps Batches standing.** A model loaded at runtime needs a runtime
  key to group storage by. A Tag is fixed at compile time, Relations do not
  exist, and grouping storage by value is rejected in `ecs.md`.
- **Any Entity with `Params` draws alone.** `FrameParams5000` would make 5 000
  Batches where one does.
- **Batching blended entries.** Glass panes A (near) and B (far) share a mesh and
  material, with smoke C between them. Merging A and B draws both before C or
  both after it, so one composites in the wrong order.

**The test oracle** ([#501](https://github.com/dvoyni/cog/issues/501)):

- **An inspection surface of ecsscene's own.** The System fills scratch correctly
  but forgets to bind the instance buffer. The surface reports the right Batches,
  every test passes, and the frame draws everything at the origin.
- **Golden frames.** Every fake returns false from `TakeCapture` and CI has no
  GPU, so the test is skipped and a regression merges.
- **`ArmFrameCmd` as the oracle.** Binding the wrong model's mesh with the right
  instance count passes.

The rejected shapes of residency ([#497](https://github.com/dvoyni/cog/issues/497)),
of the split of the frame code ([#521](https://github.com/dvoyni/cog/issues/521)),
of the fountains ([#510](https://github.com/dvoyni/cog/issues/510)) and of the
landing order ([#522](https://github.com/dvoyni/cog/issues/522)), which shape
ecsscene as much as scene, stay with the rest of `model`'s record in
[model.md §Shapes that were rejected](../../../model/docs/specs/model.md#shapes-that-were-rejected).

---

## Out of scope

- **Per-instance properties** ([#520](https://github.com/dvoyni/cog/issues/520)),
  which would make a tinted crowd one draw in both renderers.
- **scene merging separate calls** ([#49](https://github.com/dvoyni/cog/issues/49)).
- **Running scene and ecsscene together**, and detecting a stale `ModelHandle`.
  Both are undefined behaviour by the standing rules.
- **Merging the four test backends** (scene's, ecsscene's, gfx's `fakeBackend`,
  cog-examples' `headless.Backend`).
