# cog scene — specification

`github.com/dvoyni/cog/bundles/scene` is cog's 3D renderer, the ECS's binding
of `model`'s drawing: **it records Entities to `gfx` itself.** This document is
its design record:
what it is made of, why each part is shaped the way it is, what it is tested
against, and the shapes that were ruled out. The API a game writes against is
[`../README.md`](../README.md). The general shape every binding of the ECS
takes is [`ecs.md` §Binding](../../../ecs/docs/specs/ecs.md#binding-how-another-plugin-attaches).

**A note on names.** Until [#573](https://github.com/dvoyni/cog/issues/573)
this plugin was the ECS binding beside a second renderer, and that second
renderer was the one called `scene`: a frame-local recording API
(`CameraDescr`, `OpQueue` with its `Op*` debug shapes, the `PassView` and
`BatchView` inspection views) whose flush culled, sorted and packed what a tick
recorded. #573 removed it and gave this plugin its name. Below, **the recording
renderer** is that removed one; every other `scene` is this plugin.

**It began as the recording renderer's path over Entities, not an improvement
on it.** It drew what the recording renderer drew for the same frame: the same
passes, the same labels, the same instances. It did differently two things. The
first is the call shape: it groups Entities into **Batches**, where the
recording renderer drew every call on its own. That difference is what the
redesign was for. The second came after it
([#568](https://github.com/dvoyni/cog/issues/568)): **a `Material` overlays what
the file provides** rather than replacing it, and a draw naming no shader takes
model's default scene shader; see
[Materials overlay the file](#materials-overlay-the-file).

One standing rule comes from [the model spec](../../../model/docs/specs/model.md),
and nothing below reopens it:

- **Unloading is cleanup between scenes.** An app unloads a model only once no
  Entity draws it. Drawing an unloaded model is undefined behaviour. There are
  no generations and no invalidation.

This section of the design used to live in `model.md`, as the plan for stage 4
of the split. It moved here when the redesign landed on main
([#538](https://github.com/dvoyni/cog/issues/538)), in one merge carrying
[#535](https://github.com/dvoyni/cog/issues/535) (the Components and the
vocabulary), [#536](https://github.com/dvoyni/cog/issues/536) (the load System)
and [#537](https://github.com/dvoyni/cog/issues/537) (the recording System).
Before it, this plugin was a binding into the recording renderer's `OpQueue`:
one `OpQueue.Model` or `OpQueue.Mesh` call per Entity, recorded by that
renderer's flush.

---

## Contents

- [Why it stopped proxying into the recording renderer](#why-it-stopped-proxying-into-the-recording-renderer)
- [Its Components](#its-components)
- [Its vocabulary](#its-vocabulary)
- [Its frame code](#its-frame-code)
- [The load System](#the-load-system)
- [The recording System](#the-recording-system)
- [Batches](#batches)
- [Materials overlay the file](#materials-overlay-the-file)
- [What it is tested against](#what-it-is-tested-against)
- [What it costs](#what-it-costs)
- [Shapes that were rejected](#shapes-that-were-rejected)
- [Out of scope](#out-of-scope)

---

## Why it stopped proxying into the recording renderer

From [where the ECS frame's time goes](https://github.com/dvoyni/cog/issues/493),
measured for 5 000 crates in view at `6a5254d` (the table is in
[model.md §What was measured](../../../model/docs/specs/model.md#what-was-measured)).

- **The call shape was the cost.** One recording-renderer call per Entity made
  5 000 Batches. The same crates as one call were 2.7 ms against 8.2 ms.
- **Proxying into the recording renderer itself was about 7%.** Sort, cull and
  layering were under 0.4% together.
- **Re-keying a model's own material every draw was 20%** of the per-Entity
  frame. The key now belongs to the loaded model material, taken once at load.

So scene draws Batches, keyed on change, and reaches `gfx` without the
recording renderer's queue in between. It could not batch through that
renderer, because it did not merge separate calls when this was decided, and
making it merge them was out of scope here. The recording renderer merged them
later ([#49](https://github.com/dvoyni/cog/issues/49)), on the same criteria
scene keys a Batch on: key, per-draw parameters and animation.

---

## Its Components

From [#495](https://github.com/dvoyni/cog/issues/495). Landed in
[#535](https://github.com/dvoyni/cog/issues/535).

**scene wraps `model`'s values in Components of its own**, passed through
unconverted. It does not register `model`'s types: registering `model.MeshRef`
would claim the one Store that Go type can have, and a second ECS plugin naming
it would get `ErrDuplicateRegistration`.

| Component | shape |
| --- | --- |
| `Model` | `{Ref model.ModelRef; Layers LayerMask}` |
| `Mesh` | `{Ref model.MeshRef; Bounds m.Vec4; Layers LayerMask; NeverCull bool}` |
| `Animation` | `{Plays [model.MaxClipPlays]model.ClipPlay}`. The binding's own earlier `MaxPlays` is gone |
| `Params` | `{Values m.List[gfx.ParameterDescr]}` |
| `Material` | `{Tags m.List[MaterialTag]}`, where `MaterialTag.Tag` is scene's `PassTag` |
| `Light` | `{Descr model.LightDescr; Layers LayerMask}`. The recording System writes `Descr.Position` and `Descr.Direction` from the Entity's Transform, and the Component documents both as ignored |
| `Camera` | flat, with the recording renderer's `CameraDescr` field names. `Passes` is an `m.List[Pass]` |

**Where an Entity stands is not one of them.** It is an `m.Transform`, whose one
Store the ecs plugin registers, so a game's Systems and other bindings read the
same placement scene draws from. Because that Store is shared and owned by
`ecs`, the coupling check will not catch an undeclared writer of it, so writers
of `m.Transform` are ordered deliberately.

**`Model` and `Mesh` spell out their tail padding, and `model.MeshRef` its
padding after the source.** The ECS compares a Changed record by its bytes, and
Hooks with Changed refuse implicit padding in a validating build
([#536](https://github.com/dvoyni/cog/issues/536)). It is a layout change with
no API change.

---

## Its vocabulary

From [#494](https://github.com/dvoyni/cog/issues/494). Landed in
[#535](https://github.com/dvoyni/cog/issues/535).

**scene declares its own camera, layer and pass types.** They began as copies
of the recording renderer's, each keeping that renderer's name, shape and
zero-value meaning, and since #573 they are the only ones:

- `LayerMask`, with `LayersAll` and `Layer`;
- `PassTag`, with `TagForward`;
- `Pass`, with its six fields;
- `CameraID`, over `gfx.Order`;
- `ProjectionKind`, with `Perspective`, `Orthographic` and `Oblique`.

`LightKind` is `model`'s. Projection maths is `libs/m`'s. **There was no third,
shared renderer bundle** while two renderers stood: what both needed was
`model`'s or `libs/m`'s, and a shared bundle would have brought back the
coupling the split removed.

**Its errors are declared in its own `internal/err.go`** and aliased in the
root's `err.go`: the camera, pass, material-tag and custom-layout errors. They
began as the recording renderer's errors under this plugin's names.

---

## Its frame code

From [#521](https://github.com/dvoyni/cog/issues/521). Landed in
[#537](https://github.com/dvoyni/cog/issues/537).

**scene copied the recording renderer's frame code that does not depend on the
shader** into its own `internal/`, adapted to Entities: the arena and
`recordBytes`, culling and draw preparation, the sort keys and `sortEntries`,
the material table, light selection and pass resolution, and the emission loop.
The arena and the sort were that renderer's verbatim; the rest walk Entities
where it walked recorded calls. Since #573 this is the only copy.

**What depends on the shader is `model`'s, and never copied**: the records the
bundled shader reads, their packers (`PackInstance`, `PackLight`,
`PackFrameLighting`, `AppendAnim`), their size constants and their binding
names. While two renderers stood, a shader change reached both at once, because
there was only one copy of what it reads.

---

## The load System

From [#497](https://github.com/dvoyni/cog/issues/497) and
[#509](https://github.com/dvoyni/cog/issues/509). Landed in
[#536](https://github.com/dvoyni/cog/issues/536), behind `scene.LoadOnUpdate`,
and ordered `Before[RecordOnUpdate]`.

**It runs on what changed**: `Hooks[T, HookAll]` of `Model`, `Mesh`, `Material`
and `Params`, taking each touched Entity once however many Hooks name it. It
holds `kernel.Write[*model.Lookup]`, the filesystem and the resource queue, so
it is the one scene System that loads. For each Entity it:

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

**The material key** is the model material's load-time key for its variant.
Under a `Material` it is that key and the `Material`'s content key (each tag and
its gfx fingerprint) hashed together, because the `Material` is laid over the
file material and one `Material` over two file materials resolves to two
materials. A `Mesh`'s "file" is one for every `Mesh`, so its key is the
`Material`'s alone. Zero is the bundled PBR a `Mesh` with no `Material` draws
with. **The `Params`
hash** is `gfx.FingerprintParams`, and absent or empty `Params` hash to zero.

It also:

- **drives `model`'s bake and release queues** every frame. Nothing else drains
  them;
- **ensures the bundled PBR's ingredients** once the backend is up, and passes
  them and the backend's readiness to the recording System through the key scratch,
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
`scene.RecordOnUpdate`.

**Its lock set is its signature**: every Store it queries, the load System's key
scratch, `kernel.Read[*model.Lookup]` and `*gfx.Viewport` read; its own scratch
and `*gfx.OpQueue` written. It reaches the Lookup only through
`model.NewLookupReadAccess`, so it never loads. Each tick it:

1. buckets every placed `Model` primitive and `Mesh` into the Batch its key
   names. A Batch resolves its mesh, material and `Params` once, from the
   first Entity bucketed into it, and each instance keeps its own
   world matrix, bounds, layers and animation offset;
2. packs every light through `model.PackLight`, and every camera's passes;
3. for each camera and pass, culls every instance by the camera's layer mask and
   the pass's frustum, then filters each Batch's survivors by whether its
   material serves the pass's tag;
4. sorts: opaque by material and Batch, blended back to front;
5. packs one instanced draw per opaque Batch, and one draw per blended
   instance, through `model`'s packers, and emits the arenas
   and the passes into `*gfx.OpQueue` under `model`'s binding names.

A frame before the backend is up, or with no window, is skipped whole, as the
recording renderer's flush skipped it.

**Passes are labelled `scene.camera<ID>.<tag>`**, the recording renderer's
labels. While both renderers stood, the same frame through either reached gfx
with the same labels, which is what let the two fountains be compared pass by
pass. A pass no material serves is still emitted, with its clear.

**Batches also split by shader variant.** It changes nothing for a file's own
material, whose key is per variant already. It stops a `Material` over a skinned
and an unskinned use of one mesh from sharing group-2 bindings that only one of
them declares.

**An Entity whose model has no skin or morph packs no animation block.** The
recording renderer appended one nobody referenced; the instance output is the
same.

**The lock it writes costs no ordinary System its parallelism.** It writes
`*gfx.OpQueue`, which only Last-phase flushes and gfx's own handlers write.
The one new contention is a game System that *writes* the Lookup: it now
serialises against recording, where before the recording renderer's Last-phase
flush held that lock. **One recording System per bound plugin is the shape**
([`ecs.md` §Binding](../../../ecs/docs/specs/ecs.md#the-wide-lock-lands-in-the-bound-plugin-not-in-the-ecs)):
`*gfx.OpQueue` is one resource, so two recording Systems would serialise
anyway.

**It declares no ordering beyond the load System's `Before`.** gfx subscribes
`gfx.PresentOnUpdate` `Last`, so what the recording System draws in a tick is in
what that tick presents. A game System that moves Transforms orders itself
`Before[scene.RecordOnUpdate]()`; one that spawns drawables or writes their
Components orders itself `Before[scene.LoadOnUpdate]()`.

---

## Batches

From [how the ECS binding groups Entities into instanced draws](https://github.com/dvoyni/cog/issues/509).

**scene builds its Batches every frame, in its recording System, from keys
written on change.** Sorting measured under 0.4% of a frame, so rebuilding them
is cheap, and a steady frame never hashes a material.

**Not in the key:** `LayerMask`, culling and the camera. They filter instances
inside a Batch for each pass, so only a Batch's survivors are packed. One
Batch can draw a different subset in each pass.

**What splits a Batch** is anything that changes the key: a different mesh or
primitive, a different `Material` (so a different pipeline), or different
`Params` values. **Equal `Params` batch.** 5 000 crates all tinted red are one
Batch, and 5 000 crates tinted differently are 5 000 Batches.
Per-instance properties, which would make a tinted crowd one draw, are
[#520](https://github.com/dvoyni/cog/issues/520).

**Skinned and morphed Entities that share a mesh and material share a Batch.**
Each instance points at its own animation block through the instance record's
`AnimOffset`, which the shader reads per instance (verified; see [model.md §The
shader's records](../../../model/docs/specs/model.md#the-shaders-records-and-their-packers)).
The fallback #509 kept in reserve, a hash of `Plays` joining the key, is not
needed.

**Blended materials stay one draw per instance**, sorted back to front across
Batches. An alpha-tested cutout is `BlendOpaque`, so it batches.

**Batching is always on, with no Component to opt out.** It costs a game nothing
it would opt into, and the only thing it changes that a game could observe is
the order of opaque draws, which is already unspecified. It is not a heuristic,
because equal keys batch deterministically.

---

## Materials overlay the file

From [#568](https://github.com/dvoyni/cog/issues/568), which the consuming
game's fade-to-black asked for: one step after shading, on every draw, glTF
models included.

**A `Material` used to mean *my shader and my bindings*.** On a `Model` that
threw away the file's record (reset to white paint), its textures, and every
material but one, since one `Material` served every primitive; and the variant
was the caller's, so a shader without `SCENE_SKIN` drew a skinned model in its
bind pose, and one with it over a static model lost the frame. **It now means
*my shader, over what the file says*.** For each primitive and each tag:

| Part | Resolved from |
| --- | --- |
| shader | the tag's, else the default scene shader; either way `model.VariantShader` adds the primitive's `SCENE_SKIN` / `SCENE_MORPH` |
| params | the primitive's `model.MaterialIngredients.Params` — textures, samplers and numbers — ⊕ the default scene shader's `Params` ⊕ the tag's, by name; the `Params` Component rides on the draw, which gfx lays over them |
| state | the tag's, else the file's |

**model keeps each material's ingredients** — params and state — beside the
four forward materials the bundled shader makes of them, which the recording
renderer drew until #573. A `Mesh`'s are `model.BundledIngredients`.

**The renderer knows no shader.** The bundled material's numbers were a
storage struct the renderer packed per Batch, because gfx cannot fill a storage
struct by name: every renderer carried the bundled PBR's record, merged
overrides into it by hand, and bound it for every draw whatever shader was in
effect. A shader of the caller's own could not have numbers of its own through
that path, and one declaring `scenePbrMaterial` differently read PBR bytes as
its own. So the numbers are the shader's one uniform block now, and each is a
param of the material: gfx packs whatever block the shader in effect declares,
by member name, with a draw's params over its material's. Both renderers then
bound only what describes the scene, and the overlay above is the whole of how
a number reaches a draw. It freed the eighth storage buffer, too.

**Every material a frame interns is recorded once into gfx's queue.** A
material now carries 27 params where it carried 10, and gfx copied and baked a
material's params, and hashed every name to find its parameter plan, on every
draw. A frame of 5 000 crates tinted 5 000 ways is 5 000 draws of one material,
and measured 55% slower than before the change. `gfx.OpQueue.FrameMaterial`
copies a material's params and takes the hash of their names once per frame;
every draw naming the recorded material copies nothing of it and hashes only
its own params. scene records each material when it interns it, and a
bundled variant the first time a bare `Mesh` draws it, because recording bakes
its ten textures and a frame with no bare `Mesh` would pay that for nothing.
The recording renderer did the same.

Measured against `main` before the change, interleaved, eight rounds a side:
the 5 000-tint frame (`BenchmarkFrameDistinct5000`) is 13.1 ms before and
8.5 ms after, 35% less; the one-Batch arms are within 3%, and the empty frame
pays about 1 µs for resolving the four bundled variants; the recording
renderer's `BenchmarkFrame` was 12-17% faster, since merging draws no longer
built and compared a record for each. Allocations are unchanged: 19 objects a
frame in every scene arm, and the recording renderer one fewer, 22, for the
materials arena it no longer uploaded.

**The default scene shader is model's**, on the Lookup
(`LookupAccess.SetDefaultSceneShader`), read by the recording System once a
frame through the read facade. **Resolution is per frame**, from the
ingredients, into the frame's arenas, interned once per material key a frame
sees, so the default can change at any time and nothing is rebuilt. The only
state outside a frame is a map from (shader, variant) to the shader under the
variant's defines, because a descriptor's supply is a string built at the call
and building one per Batch per frame would be the steady frame's only
allocation. The allocation test has an arm under a default shader to hold that.

**A missing binding is gfx's to report, and it already does.** gfx fills an
unfilled texture with white and an unfilled sampler with its default, and
drops a draw whose declared storage buffer nothing supplies with
`gfx.ErrStorageBufferUnsupplied`, once per shader and binding. The whole-frame
loss #568 describes predates that. With the variant cog's, the skin and morph
buffers can no longer be that binding.

**The bundled shader is published in two stages** so an app shader calls the
bundled fragment rather than copying it: `model.VertexStagePath` (`vs_main`)
and `model.FragmentStagePath` (`scenePbrFragment`). `scene.wgsl` is the two plus
a one-line `fs_main`. **Group 3 is the app's.** Groups 0 to 2 stay what cog
binds. The material's block is the one uniform block gfx allows, so an app's own
per-draw numbers are members it adds to that block, composing it from
`model.MaterialProloguePath`, its own fields over `model.MaterialFieldsPath`,
and `model.MaterialEpiloguePath` before it includes the stages. Larger data rides
in textures and samplers, or in the one storage buffer of the eight the bundled
shader leaves.

---

## What it is tested against

From [what the ECS binding asserts against once there is no Op to read back](https://github.com/dvoyni/cog/issues/501).

**The oracle is a recording `gfx.Backend`.** The harness publishes `RenderEvent`
as well as `UpdateEvent`, and tests assert on what gfx received:

- the vertex and index buffers bound;
- draw and instance counts;
- pass descriptors and clears;
- the baked bytes of the instance, camera and light buffers, decoded against
  `model`'s record layouts;
- the `SetParams` bytes.

Tests in the same package may also read scene's scratch as a white-box extra,
never as the only assertion on a behaviour.

**scene keeps its own `testBackend`, in `_test.go`**, modelled on the
recording renderer's. It also records `SetParams` bytes, which that one
dropped, and supplies a `ShaderLayout` for each variant so gfx keeps the
parameters.

**scene publishes no inspection API.** Counts a game or a HUD needs come from
gfx's `ArmFrameCmd`.

**Tests assert properties, not Batch shape.** A test checks which mesh drew how
many instances with which records. Only the Batch tests (`batch_test.go`) assert
exact Batch counts: 5 000 identical crates are one draw of 5 000, 5 000 distinct
tints are 5 000 draws, blended panes around smoke are four draws back to front,
and animated Entities share a Batch with an animation block each.

**Layer and pass routing have their own tests** (`routing_test.go`): layers
across three cameras, passes by material tag, and frustum culling inside a
Batch. The recording renderer did that routing too and never tested it.

**The five benches** (`BenchmarkFrameEmpty`, `Frame5000`, `FrameAnimated5000`,
`FrameParams5000`, `FrameMaterial5000`) draw: a camera, a model made resident
from an in-memory file system, `UpdateEvent` plus `RenderEvent`, and a backend
that reports `Ready()` and whose sinks replay and discard. They assert nothing.

**`TestRecordingAllocatesNothingPerEntity` now holds only trivially.** Its
harness uses a backend that never becomes Ready, and the recording System skips
such frames whole. The drawn benches carry the allocation claim instead. Giving
the test a drawing harness is a follow-up.

**The fountains compared the two renderers frame to frame** while both stood.
`cmd/ecs/fountain` and `cmd/scene/fountain` in cog-examples drew the same frame
at the same step, sharing their simulation in `internal/fountain`, the second
through the recording renderer. At the reference step both were held to the
same passes, labels and instances in each pass, and the ecs fountain's draws in
each pass to no more than the recording fountain's. At step 600 the recording
fountain's forward pass was 117 draws of 117 instances and the ecs fountain's
116, because two motes thrown on one step fade to the same tint and share a
Batch. See [model.md §The
fountains](../../../model/docs/specs/model.md#the-fountains).

---

## What it costs

Interleaved runs of two built binaries, before (the proxy into the recording
renderer's `OpQueue`, `de8e5f9`) and after (`191003f`), 8 rounds, arm order rotated, each arm its own
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

- **scene registering `model`'s types directly.** It claims the one Store each
  Go type can have, so a second ECS plugin gets `ErrDuplicateRegistration`.

**Batches** ([#509](https://github.com/dvoyni/cog/issues/509)):

- **The recording renderer merges calls.** scene no longer passed through that
  renderer's `OpQueue`, so the merge was not on its path, and 5 000 crates
  would have stayed 5 000 draws.
- **The ECS keeps Batches standing.** A model loaded at runtime needs a runtime
  key to group storage by. A Tag is fixed at compile time, Relations do not
  exist, and grouping storage by value is rejected in `ecs.md`.
- **Any Entity with `Params` draws alone.** `FrameParams5000` would make 5 000
  Batches where one does.
- **Batching blended entries.** Glass panes A (near) and B (far) share a mesh and
  material, with smoke C between them. Merging A and B draws both before C or
  both after it, so one composites in the wrong order.

**The test oracle** ([#501](https://github.com/dvoyni/cog/issues/501)):

- **An inspection surface of scene's own.** The System fills scratch correctly
  but forgets to bind the instance buffer. The surface reports the right Batches,
  every test passes, and the frame draws everything at the origin.
- **Golden frames.** Every fake returns false from `TakeCapture` and CI has no
  GPU, so the test is skipped and a regression merges.
- **`ArmFrameCmd` as the oracle.** Binding the wrong model's mesh with the right
  instance count passes.

**Materials** ([#568](https://github.com/dvoyni/cog/issues/568)):

- **A second mode beside replacement.** gfx binds by name and drops what no
  binding declares, so an outline shader declaring only its own bindings
  already replaces. A mode flag would be two spellings of one behaviour.
- **`m.Maybe` for a tag's `Shader` and `State`.** The zero `gfx.ShaderDescr`
  names no shader, so a zero `Shader` is unambiguous. A zero `State` is not:
  it is `StateOverlay2D`, and zero-as-unset means a tag cannot ask for it.
  That cost was taken over wrapping every tag's state, and it is recorded
  here because it is a real hole.
- **Baking the default scene shader into model's materials at load.** Its
  params are typically baked textures, which exist only once the backend
  does, so it is set after startup: the first model loaded before the call
  would keep the old shader until rebuilt, or the call would have to be
  refused. Resolving per frame from ingredients has neither case.
- **Checking the shader's reflected bindings in scene.** Reflection lives
  in gfx's translator, on the render side of the queue; the recording System
  never sees it, and gfx already refuses and reports the draw.
- **Keeping the numbers a storage record the renderer packs.** The renderer
  then knows the bundled shader: it merges a tag's params into a PBR struct by
  hand, one record per tag, and binds it under every shader, and a shader of
  the caller's own has no way to receive numbers through the same overlay.
  The storage form was chosen when gfx's uniform path gave every draw a buffer
  of its own; since it stages every draw's block in one arena at the same
  256-byte stride the record padded to, it costs nothing the record did not.
- **Applying the default shader's params only where the default shader is in
  effect.** A tag's own shader then has to carry the scene-wide texture again
  per Entity, which is what the default was for; a shader not declaring the
  names never reads them.

The rejected shapes of residency ([#497](https://github.com/dvoyni/cog/issues/497)),
of the split of the frame code ([#521](https://github.com/dvoyni/cog/issues/521)),
of the fountains ([#510](https://github.com/dvoyni/cog/issues/510)) and of the
landing order ([#522](https://github.com/dvoyni/cog/issues/522)), which shaped
scene as much as the recording renderer, stay with the rest of `model`'s record
in [model.md §Shapes that were rejected](../../../model/docs/specs/model.md#shapes-that-were-rejected).

---

## Out of scope

- **Per-instance properties** ([#520](https://github.com/dvoyni/cog/issues/520)),
  which would make a tinted crowd one draw.
- **Detecting a stale `ModelHandle`.** It is undefined behaviour by the
  standing rule.
- **Merging the three test backends** (scene's, gfx's `fakeBackend`,
  cog-examples' `headless.Backend`).
