# cog model — specification

`github.com/dvoyni/cog/bundles/model` is the plugin that owns everything a model
file can contain: glTF decode, geometry generation, vertex and morph packing,
GPU upload, the model and texture caches, and the PBR shader with the records it
reads. `bundles/scene`, cog's one renderer, is built on it. This document
specifies `model`, what a renderer keeps, and the order the split landed in.

**A note on names.** The split was designed and landed with two renderers on
`model`: a declarative one called `scene`, whose frame-local `OpQueue` recorded
`CameraDescr`s, `ModelDraw`s, `MeshDraw`s and `Op*` debug shapes and whose
flush culled, sorted and packed them, and the ECS binding beside it. Ticket
[#573](https://github.com/dvoyni/cog/issues/573) removed the declarative one and
gave the ECS binding its name. This document is the record of the split, so it
still speaks of both: **the recording scene** is the removed renderer, and
`scene` alone is the one that ships. File paths, line numbers and branch names
are as they were when each part was written.

**It is a split, not an improvement.** `model` is carved out of
`bundles/scene/internal/types` (14.7k lines, the recording scene's) and the
recording scene's per-frame code, and `scene` repeats the path the recording
scene took. Anything that would make either renderer do more than it did then
is a separate issue. Two were filed:
[per-instance properties](https://github.com/dvoyni/cog/issues/520), which is
open, and [the recording scene merging separate calls](https://github.com/dvoyni/cog/issues/49),
which landed before it was removed.

The design is bound by four requirements, in this order:

1. **The import table holds.** `.github/instructions/architecture.instructions.md`
   is the hard constraint on every placement. A root holds declarations, aliases
   and pure forwarders. `libs/*` imports only `libs` and `kernel`. No plugin
   reaches into another's `internal/`.
2. **One upload per model.** One cache, one mesh table, and every renderer reads
   them.
3. **`scene` imported nothing of the recording scene.** What both renderers
   needed was `model`'s (assets and the shader's layout) or `libs/m`'s (maths).
   There is no third, shared renderer bundle.
4. **Never cost System parallelism.** scene's readers of model residency run
   side by side. Only loading and unloading are exclusive.

One standing rule comes with it, and no section below reopens it. (A second,
that an app ran one renderer or the other and never both, lapsed with the
recording scene in #573.)

- **Unloading is cleanup between scenes.** An app unloads a model only once
  nothing draws it. Drawing an unloaded model is undefined behaviour. There are
  no generations and no invalidation.

This document is the specification the implementation is judged against. It is
assembled from the eleven resolved tickets of [model: one plugin beneath two
renderers, and what a renderer still owns](https://github.com/dvoyni/cog/issues/450),
and every section cites the tickets it came from. Where a claim rests on
something unverified it would be marked **Gap**; none is left. Where putting
decisions side by side settled something that no ticket did, it is marked
**Settled here**.

**Implemented: all five stages of [Required work](#required-work) have landed.**
`bundles/model` is a Bundle with a plugin, `modelplugin.New`, that registers
the `*model.Lookup` resource. It holds the glTF decoder, the unit geometry,
`Vertex` with the storage layout it reports, the conversion to GPU layouts, the
model and texture caches, the mesh table, the Lookup with its two facades, the
model material, the bundled shader, and the records it reads with their
packers, and the recording scene imported all of it from model's root.
`scene` records to `gfx` itself over the same `model`; it landed on main in one
merge ([#538](https://github.com/dvoyni/cog/issues/538)). **Its part of this
design moved to its own spec**,
[`scene.md`](../../../scene/docs/specs/scene.md): its Components and
vocabulary, its copy of the frame code, its two Systems, Batches and what it is
tested against. [The ECS scene after the split](#the-ecs-scene-after-the-split)
keeps the summary. The sweep, stage 5 ([#539](https://github.com/dvoyni/cog/issues/539)),
rewrote every caller to name `model.X`, deleted the recording scene's temporary
aliases, and settled what stays on `model`'s root ([model's root after the
sweep](#models-root-after-the-sweep)). [`mesh.md`](mesh.md) moved here with the
cache, and the recording scene's spec was cut down to the renderer: its
sections on what is now `model`'s moved to the end of this document, under
[The model contract, moved from scene.md](#the-model-contract-moved-from-scenemd).

---

## Contents

- [Vocabulary](#vocabulary)
- [What was measured](#what-was-measured)
- [What `model` declares, and what a renderer keeps](#what-model-declares-and-what-a-renderer-keeps)
- [The decoder seam](#the-decoder-seam)
- [The model material](#the-model-material)
- [The shader's records and their packers](#the-shaders-records-and-their-packers)
- [Residency: one Lookup, two facades](#residency-one-lookup-two-facades)
- [Storability, and `m.List`](#storability-and-mlist)
- [The recording scene after the split](#the-recording-scene-after-the-split)
- [model's root after the sweep](#models-root-after-the-sweep)
- [The ECS scene after the split](#the-ecs-scene-after-the-split)
- [The fountains](#the-fountains)
- [What is not foreclosed](#what-is-not-foreclosed)
- [Shapes that were rejected](#shapes-that-were-rejected)
- [Required work](#required-work)
- [Out of scope](#out-of-scope)
- [The model contract, moved from scene.md](#the-model-contract-moved-from-scenemd):
  [glTF models](#gltf-models) · [Buffer-built meshes](#buffer-built-meshes) ·
  [Animation](#animation) · [Lights](#lights) ·
  [Bundled PBR material](#bundled-pbr-material) · [Lookup facade](#lookup-facade) ·
  [Shader-side contract](#shader-side-contract)

---

## Vocabulary

The glossary is [`CONTEXT.md`](../../../../CONTEXT.md). The terms this document leans on:

- **Model material.** What one material in a model file becomes once loaded: a
  forward graphics material for each shader variant, the params and state it
  is made of, and a content key fixed at load. It names no Pass tag. It is not a **Scene
  material**, which is a renderer's and carries tags.
- **Batch.** One instanced draw. It was the recording scene's word and is
  scene's. "Group" is the word to avoid.
- **Read facade / load facade.** The two ways to reach the one `*model.Lookup`:
  under `kernel.Read`, which never loads, and under `kernel.Write`, which loads,
  preloads and unloads.
- **`ModelHandle`.** A plain slot index into `model`'s dense model table. A
  `ModelRef` resolves to it once. It has no generation.
- **Carve.** Moving code out of the recording scene into `model` without
  changing what it does.
- **Sweep.** The one landing that rewrites callers from `scene.X` to `model.X`
  and deletes the recording scene's temporary aliases.
- **Clip machine.** `model.ClipMachine`: a clip state machine the caller keeps
  and steps, which hands back a frame's `ClipPlay`s and the events of the step.
  It is one value, definition and runtime state together. "Animation graph" and
  "animator" are the words to avoid: there is no graph, and nothing animates on
  its own.
- **Clip state.** One state of a clip machine: a named clip, whether it loops,
  and a `Rate` on the caller's dt. Not a **Clip**, which is a sound.
- **Trigger.** The string `Fire` takes a transition on. `OnFinish` is the other
  way a transition is taken, and is not a trigger.
- **Freeze.** What a `Fire` during a crossfade does to the mix: its plays keep
  their relative weights and fade out as one group, while their clocks keep
  running.

---

## What was measured

From [where the ECS frame's time goes](https://github.com/dvoyni/cog/issues/493)
([results](https://github.com/dvoyni/cog/blob/research/ecsscene-baseline/bundles/ecsscene/docs/research/ecsscene-baseline.md)),
for 5 000 crates in view on a Ryzen 9 7950X3D at `6a5254d`:

| where | share of an 8.3 ms frame |
| --- | ---: |
| the ECS walk, the copy out, and `OpQueue.Model` | 4.6% |
| the recording scene's queue flush bookkeeping | 2.6% |
| `expandModels` | 14.2% |
| `prepareDraws`, of which re-keying the file material each draw | 24.5% (19.6%) |
| cull and layer test | 0.24% |
| sort | 0.12% |
| `emit`, per batch into gfx | 42.4% |

- **Sort, cull and layering are not the cost.** Together they are under 0.4%.
- **Proxying into the recording scene is about 7%.**
- **The call shape is the cost.** One call per Entity makes 5 000 Batches. The
  same crates as one call are 2.7 ms against 8.2 ms.
- **Re-keying a model's own material each draw** is 20% of the per-Entity frame
  and 55% of the instanced one. The key belongs to the loaded material, which is
  `model`'s.

These figures are why the ECS binding's redesign is about Batches and not about a faster
sort, and why the model material carries its key.

From [the decoder seam](https://github.com/dvoyni/cog/issues/496)
([results](https://github.com/dvoyni/cog/blob/research/decoder-seam/bundles/scene/docs/research/decoder-seam-cost.md)):

- Parsing Fox (skinned, 1 728 vertices) takes 2.5 ms, 69% of it the animation bake.
- CompareBaseColor (27 648 vertices) takes 1.7 ms, 76% of it reading vertices.
- Handing vertices over as the glTF library's typed slices is **14% faster** than
  today's read, which makes a closure call per element.

---

## What `model` declares, and what a renderer keeps

From [the inventory](https://github.com/dvoyni/cog/issues/492)
([table](https://github.com/dvoyni/cog/blob/research/scene-inventory/bundles/scene/docs/research/scene-inventory.md)),
[what model declares, and what a renderer keeps](https://github.com/dvoyni/cog/issues/494),
and [where the per-frame drawing code goes](https://github.com/dvoyni/cog/issues/521).

**The line: `model` owns everything a model file can contain, and a renderer owns
drawing.** Drawing means the queue, passes, layers, culling, sorting, and the
renderer's own way of saying "draw this".

Every name the inventory classified as `model/decode`, `model/pack`,
`model/cache`, `model/geom` or `model` goes to `bundles/model`. Every `scene` row
stayed in the recording scene's `bundles/scene`. Of 405 declarations, 269 land on the model side and 66
on the renderer's. The rows that needed a decision:

| name(s) | lands in | reading |
| --- | --- | --- |
| the model material (today `modelMaterial`, `loadedMaterial`) | `model` | see [The model material](#the-model-material) |
| `ShaderVariant`, `VariantStatic`, `variantSkin`, `variantMorph`, `variantSkinMorph`, `VariantCount`, `VariantFor` | `model` | chosen from skin and morph, which are model facts |
| the bundled PBR shader (`SceneShaderPath`, `SceneShader`) | `model` | it reads the params model's materials carry by name, so the shader and the materials move together |
| `Material`, `MaterialTag`, `MaterialKey`, `MaterialKeyOf`, `PassTag`, `TagForward` | each renderer, its own | a renderer wraps the model material's forward descr under its own tag |
| `CameraDescr`, `CameraID`, `ProjectionKind` and its three values, `Pass`, `DefaultPass`, `DepthClearFar` | each renderer, its own | `model` declares **no** camera, since none is decoded. A decoded glTF camera would arrive as a `ModelCamera` data record |
| `Projection`, `ViewDirection`, `WorldToScreen`, `ScreenToWorld`, `ScreenToRay` and helpers | `libs/m`, over plain parameters | each renderer's camera calls them |
| `LightKind`, `LightPoint`, `LightSpot`, `LightDescr`, `ModelLight` | `model` | the loader already fills them |
| `LightRecord` | each renderer | the recorded light carries a layer mask |
| `MaxLights` | `model` | the length of the shader's `Lights` array. [#521](https://github.com/dvoyni/cog/issues/521) replaced #494's row here |
| `Vertex`, `VertexLayout`, `MeshRef`, `MeshRecord`, `BakeFunc`, `Span`, `MeshBaker`, `pendingMesh`, `rebakeIndices`, durable minting and baking | `model` | `model` owns all mesh residency: file primitives, unit shapes and app-built meshes alike. There is one mesh table |
| `MeshSource`, `MeshNone`, `MeshDurable` | `model`, reduced | `model` knows only durable meshes |
| `MeshTemporary`, `TemporaryMeshID` | the recording scene | the declarative queue's per-frame mesh |
| unit geometry and unit meshes | `model` | |
| `unitShape`, `ShapeNone`, `ShapeBox`, `shapeSphere`, `shapePlane`, `shapeCount` | the recording scene | the shape enum is how its queue records |
| `overrideRecord` | `model`, exported | as a slice merge and a single-parameter merge |
| `SkinBuffers` | `model` | |
| `AnimBinding` | each renderer | per-Batch assembly is frame bookkeeping |
| `maxClipPlays` | `model`, exported as `MaxClipPlays` | the ECS binding's `MaxPlays` goes |
| `Config`, `WithDefaults` | `model` | the pose sample rate. A renderer that needs configuration declares its own |
| `Lookup` and its access types | `model` | everything it holds is `model`'s. See [Residency](#residency-one-lookup-two-facades) |
| `OpQueue`, `ModelDraw`, `MeshDraw`, `DrawRecord`, `ModelDrawRecord`, `LayerMask`, `RecordOnUpdate`, the Op inspection surface | the recording scene | all removed with it in #573 |

**Placement is `m.Transform`, named directly; no renderer re-exports it**
([#468](https://github.com/dvoyni/cog/issues/468)). `m.LookAt` builds a camera's
one, which is why no renderer declares a `LookAt`.

**`model` is one plugin, and the decoder is a package inside it**, at
`bundles/model/internal/types/gltf/`. Geometry generation (`UnitBoxGeometry`,
`UnitPlaneGeometry`, `UnitSphereGeometry`, `appendQuad`, 187 lines) lives in
`model` too, in `internal/types/geometry.go`. Both
landed in [#529](https://github.com/dvoyni/cog/issues/529); see [The decoder
seam](#the-decoder-seam).

**`model` is a plugin that composition roots register, before any renderer.** It
registers the `*model.Lookup` resource, and nothing else does, and it takes
`model.Config` under `model.Name`. scene declares a dependency on it, as the
recording scene did, and so does any plugin that locks the Lookup itself, because the kernel
requires a dependency on a resource's owner. **Settled here:** the Lookup is a
kernel resource and it is `model`'s, so the plugin that registers it is
`model`'s. The `PoseSampleRate` key moved from the recording scene's
`scene.Name` to `model.Name` with the Lookup
([#530](https://github.com/dvoyni/cog/issues/530)). scene takes no
configuration, and a setting left under `scene.Name` is ignored silently, but no
composition root in cog-examples or feuds-26 set the recording scene's
`scene.Config` (counted 2026-09-21). Every composition root that registers scene
registers `modelplugin.New()` before it.

---

## The decoder seam

From [the decoder seam](https://github.com/dvoyni/cog/issues/496). Built by
[#529](https://github.com/dvoyni/cog/issues/529).

**The seam is thin.** The decoder, `bundles/model/internal/types/gltf`, does no
GPU-layout work. `gltf.Decode` parses one file and returns a `gltf.Model`, which
`model/internal/types` names `DecodedModel`. It holds:

- vertex attribute arrays and index arrays (`Geometries`);
- unbaked animation curves and skins (`Clips`, `Skins`, `Joints`, and the node
  forest the pose walk needs, `Nodes` and `Roots`);
- morph target floats (each geometry's `Targets`, with the flattened weight
  slots' defaults and names in `MorphDefaults` and `MorphNames`);
- material parameters as plain values (`Materials`), glTF's own numbers under
  glTF's own names, with KHR_texture_transform and the emissive strength read;
- image references (`Images`);
- lights (`Lights`);
- the flattened scene walk (`Primitives`, `Scenes`, `DefaultScene`,
  `NeverCull`).

The glTF document is dropped before `Decode` returns. `DecodeModel` and
`DecodeDocument` in `model/internal/types` forward to it. They and the
`Decoded…` names were on `model`'s root until the sweep took them off, since
nothing outside `model` decodes.

**The walk stays in the decoder.** Flattening every scene, interning geometries
and material windings, claiming joints and morph weight slots, and recording the
re-root data are facts about the file, so the decoder computes them. Placement
answers that decide a storage layout arrive as plain booleans: a geometry's
`Skinned` and `SkinnedLayout`, and a primitive's `Skinned`, `Plain` and `Joint`.

**The GPU layout is applied after the decoder returns.** Filling the conversion
vertices, generating flat normals and tangents, remapping JOINTS_0 into the
model's numbering, `packMorphBlock`, `bakeClip` and the material numbers' fill each
run as their own pass over the decoded data. None of them is in the decoder
package. They run in `bundles/model/internal/types` (`gltfload.go`,
`gltfmesh.go`, `gltfmorph.go`, `gltfanim.go`), beside the model cache that
installs their result and the record types they fill: `scenePose`, `skinJointRecord`, the morph block layout and the pack helpers are
`model`'s. They ran in the recording scene until the cache moved with them in
[#530](https://github.com/dvoyni/cog/issues/530). The joint cap, 256 joints
because a storage vertex names a joint in one byte, is
checked there too: it is the storage layout's limit, not the file's.

**Vertex data crosses as structure of arrays.** Positions, normals, UVs,
tangents, colours, joints and weights cross as the glTF library's own typed
slices (`[][3]float32`, `[][4]uint8`, `[][4]uint16` and so on). A float accessor
is handed over as the slice modeler decoded, untouched; only a quantised or
normalised one is widened into a slice of the same shape. The conversion copies
them into its vertices in one plain loop per attribute, with no call per element,
which is the arm [the seam research](https://github.com/dvoyni/cog/blob/research/decoder-seam/bundles/scene/docs/research/decoder-seam-cost.md)
measured 14% faster than the element-by-element read it replaced. `Vertex` is
`model`'s and never crosses the seam.

**GPU record layouts never live in the decoder package.** It names no record,
no storage offset and no shader slot. Its material slots are glTF's five, in its
own order, and the conversion maps them onto the record's.

**The decoder names images, and `model` keys them.** The decoder resolves each
image reference and deduplicates by image and colour space. It hands over one
`gltf.Image` per distinct pair: an external image's resolved storage path, or an
embedded image's model path, image index and bytes, with the sRGB flag in both
cases. Material slots point at these by index. The conversion builds
`assets.Descr[textureDescrParams]` from them, and the rules for keying an image
stay beside the texture loader. **The decoder does not import `libs/assets`.**

**Imports and errors.** `model/internal/types` imports `internal/types/gltf`,
never the other way round. The decoder imports only `libs/m`, `qmuntal/gltf` and
`slots/gfx` (for sampler and topology enums). It declares its own report types:
`ErrModelTextureUnavailable`, `ErrModelPrimitiveSkipped` (an unsupported topology
or a primitive with no POSITION), `ErrModelBoundsMissing`,
`ErrModelNodeDuplicated` and `ErrModelSkinUnbound`. An unsupported required
extension fails the decode with a plain error, which the model cache wraps in
`ErrModelUnavailable` as before. `model` re-exports the five under their current
public names, so no error is wrapped twice and no settled name moves. The
recording scene aliased them until the sweep; callers now name `model.ErrModel…`.

**The unit geometry and `Vertex` are `model`'s.** `UnitBoxGeometry`,
`UnitPlaneGeometry` and `UnitSphereGeometry` (and the unexported `appendQuad`)
live in `bundles/model/internal/types/geometry.go`, and `model`'s own
`EnsureUnit` is their only caller. They build `[]Vertex`, and a
`Vertex` reports its storage layout through its `VertexLayout` method, so
`Vertex`, the `VertexLayout` interface and the storage layout's offsets, strides
and two attribute tables moved with them, ahead of the rest of mesh residency.
Since [#530](https://github.com/dvoyni/cog/issues/530) `model`'s packers are the
only ones writing that layout, and the root keeps only its two strides.

---

## The model material

From [what model declares](https://github.com/dvoyni/cog/issues/494) and [how
the ECS binding groups Entities into instanced draws](https://github.com/dvoyni/cog/issues/509).

**The cache no longer holds a renderer type.** Until the cache moved, the model
material was `[VariantCount]Material`, the recording scene's pass-tagged `Material`: the one
place the cache reached into renderer vocabulary, and the reason a file material
is re-keyed on every draw record, every frame. [#530](https://github.com/dvoyni/cog/issues/530)
brought the data shape forward so that `model` never named a `PassTag`: the
cache holds `modelMaterial{Forward [VariantCount]gfx.MaterialDescr; Record
ScenePbrRecord}`, and the bundled PBR comes back from `Lookup.EnsureBundled` as
`[VariantCount]gfx.MaterialDescr`. The recording scene wrapped each forward
descr it drew as `MaterialTag{TagForward, descr}` in an arena its flush kept
across frames, so a steady frame wrapped without allocating. [#532](https://github.com/dvoyni/cog/issues/532)
added the key: the load takes each forward descr's gfx fingerprint once, as
`modelMaterial.Key`, and the recording scene's `ForwardMaterialKey` turned it into the key
`MaterialKeyOf` would have given the wrapped material, so a draw of a file's
own material is keyed without fingerprinting anything. The same step moved the
bundled shader's WGSL sources and their storage mount into `model`, under the
storage paths they already had.

**The model material holds its ingredients and, for each shader variant:**

- a ready forward `gfx.MaterialDescr`;
- a content key computed once at load.

The ingredients are `MaterialIngredients{Params, State}`: the ten texture and
sampler params with defaults in empty slots, then the material's numbers as one
param per member of the shader's `scenePbrMaterial` uniform block, and the
pipeline state. Each forward descr is the bundled variant over them.
They came with [#568](https://github.com/dvoyni/cog/issues/568), so a renderer
can resolve a caller's shader over what the file says instead of in place of
it; scene does, and the recording scene drew the forward descrs until #573. A baked mesh's
ingredients are `BundledIngredients`, which `Lookup.EnsureBundledIngredients`
returns around the same two default textures `EnsureBundled` binds.

It names no `PassTag`. A renderer wraps the forward descr in its own material
under its own tag. A file material's renderer key derives from the model key, so
nothing fingerprints the material per draw. When shadows land, `model` adds a
shadow descr for each variant beside the forward one.

**The default scene shader is the Lookup's.** `LookupAccess.SetDefaultSceneShader`
takes a `SceneShaderDescr{Source, Params}`, the zero one being the bundled PBR,
and `LookupReadAccess.DefaultSceneShader` reads it back. It is a call rather
than `Config` because its params are typically baked textures, which exist only
once the backend does. model bakes nothing from it: a renderer that honours it
resolves its materials every frame, so it can change at any time. scene
honours it; the recording scene never did. `VariantShader` puts any shader under a draw's
`SCENE_SKIN` and `SCENE_MORPH`, through `gfx.ShaderDescr.With`, so the variant
stays the renderer's whichever shader is in effect.

---

## The shader's records and their packers

From [where the per-frame drawing code goes](https://github.com/dvoyni/cog/issues/521).
Landed in [#533](https://github.com/dvoyni/cog/issues/533): the records and
packers are in `internal/types/records.go`, `light.go` and `lightselection.go`,
aliased in the root's `types.go` and forwarded from its `utils.go`.

**`model` exports every record the shader reads, with its packer.** While two
renderers stood, both wrote the same bytes through the same code, so a shader
change could not compile against one renderer and draw garbage in the other.

- **The records:** `FrameBlock`, `Instance` with its flags (`SceneNonUniform`,
  `SceneNoSkin`, `ScenePlainJoint`) and `SceneNoAnim`, `Light`, and
  `SceneAnimHeader`, `ScenePlayRecord` and `SceneMorphWeight`, beside
  `SceneMesh` and `IdentityMesh`. The material's numbers were a record here too,
  `ScenePbrRecord`, until [#568](https://github.com/dvoyni/cog/issues/568) made them params of the material. They are aliased in
  `model`'s root, and their packers are forwarders into `model/internal/types`.
  The flags keep the WGSL constants' names; the three records the renderer
  used to declare take the names this section gave them.
- **The binding names are constants**, `BindingScene` followed by the WGSL
  name: `BindingSceneFrame` (`sceneFrame`), `BindingSceneInstances`,
  `BindingSceneAnim`, `BindingSceneMeshes`, `BindingScenePoses`, `BindingSceneSkinJoints` and `BindingSceneMorphDeltas`.
- **Each record has a `Size` constant**, `<Record>Size`, replacing the
  `unsafe.Sizeof` block in the recording scene's `draw.go`, because binding ranges need them.
  `PoseSize`, `SkinJointSize`, `AnimHeaderVec4s` and `PlayRecordVec4s` became
  constants too.

**Fixed-size records are returned by value.** `PackInstance(world, anim
InstanceAnim, mesh)` returns an `Instance`. `InstanceAnim` is what the record
says about animation: the block offset, `Skinned`, and the plain-bound `Joint`
and `Plain`. scene's `AnimBinding` embeds it beside the group 2 buffers and
`MorphAt`, which stay the renderer's. `PackLight` returns a `Light` and an
error, `ErrSpotDirectionMissing` or `ErrSpotConeInverted`, both `model`'s now.
Each renderer appends them with its own arena, which stays out of `model`.

**The variable-length animation block is written by `model`:**
`model.AppendAnim(dst []byte, plays, morph AnimMorph) ([]byte, uint32)`.
`AnimMorph` is the morph half: the primitive's `MorphBinding` and the sparse
targets `SelectMorphTargets` kept. The header, the order of the parts and the
vec4 padding are part of the layout. The renderer passes its arena's slice in
and keeps the result. A draw with nothing to animate appends nothing and gets
`SceneNoAnim`.

**The frame block keeps today's two steps.**

1. The renderer fills View, Projection, ViewProjection, CameraPosition and
   ViewDirection.
2. `model.PackFrameLighting(block, lighting model.FrameLighting, &selection)`
   writes the sun, the ambient, the lights and `LightCount`.

`model.FrameLighting` holds the camera's six lighting fields under the camera's names:
`SunDirection`, `SunColor`, `SunIntensity`, `AmbientSky`, `AmbientGround` and
`AmbientIntensity`. Each renderer copies them from its camera at the call site,
so neither camera changes shape. The defaults (normalising the sun, an intensity
of zero meaning 1) are resolved once, in `model`.

**Light selection is split.** `model` exports the part that fills the shader's
array: the contribution score, `ContributionAt`, and `LightSelection`, the array
capped at `MaxLights`, with `Reset`, `Offer`, `Count` and `Lights`. Each renderer
keeps `prepareLights`, the layer test and the frustum test, and offers `model`
only the lights that pass.

**Paint is `model`'s too.** A draw with no material of its own draws
`BundledIngredients`, white paint: glTF's defaults with metallic 0. A debug
shape lays `PaintParams(dst, color, selfLit)` over it on the draw. The
recording scene's debug shapes and bare meshes used to set a record's fields
themselves; a second renderer drawing a mesh with no material needed the same
numbers, and then neither renderer named one.

**Settled, verified while handing over:** the shader reads `animOffset` from each
instance's own record (`instance.wgsl`, read by `skin.wgsl` and `morph.wgsl`),
never as a value fixed for the draw. That is the condition scene's
[Batches](../../../scene/docs/specs/scene.md#batches) rest on for skinned
and morphed Entities.

---

## Residency: one Lookup, two facades

From [the cache read path](https://github.com/dvoyni/cog/issues/497).

**`model` owns the caches.** `modeltable.go`, `texturetable.go` and
`modelunload.go` moved into it in [#530](https://github.com/dvoyni/cog/issues/530).
There is one `*model.Lookup`, holding the model cache, the texture cache, the
mesh table every `MeshRef` indexes, the staging arena, the unit meshes, and the
deferred bake and release queues. Only the recording scene's per-frame
temporary mesh stayed behind: `MeshSource` is reduced to `MeshNone` and
`MeshDurable`, the recording scene declared `MeshTemporary` past them and
`TemporaryMeshID` beside it, and minted its
temporaries through `model.MintMesh` into a `model.LayoutCache` and an arena of
its own, building their refs with `model.NewMeshRef`. All of that went with it
in #573.

**A renderer holding the Lookup for writing calls its own methods**, which
replaced the recording scene's friend accessors: `ModelView` resolves one
draw's selectors, loading the model if needed; `Mesh` resolves a durable ref;
`EnsureUnit` bakes a `model.UnitMesh`, which the recording scene mapped its
shape enum onto; `EnsureBundled` returns
the bundled PBR's forward descrs; and `DrainMeshes` applies the staged bakes and
releases. The two facades and `ModelHandle` below landed in
[#531](https://github.com/dvoyni/cog/issues/531): the read facade is
`LookupReadAccess`, and the load facade is `LookupAccess`, `LookupDeviceAccess`
and the Lookup's own methods, `Resolve` among them.

**The read set is immutable from install to unload:**

- the primitive span of the selected scene or node, and for each primitive its
  `MeshRef`, `Local`, material index, bounds sphere, `Skinned`, `Joint` and
  `Plain`, morph `SlotBase` and `Targets`;
- the model materials;
- `NeverCull` and the re-root data;
- the clips, `JointCount`, the pose, skin-joint and morph buffers, and `Weights`;
- for each `MeshRef`, the mesh record: vertex and index buffers, `IndexWidth`,
  `Topology`, `Layout`, `Standard`, `Indexed`, `Bounds` and `UV`.

The renderer writes each frame's blended poses and morph weights into buffers
that `model` owns. Textures are never read per draw, because they are bound into
the material at install. **A reader therefore needs excluding only from loading
and unloading, never from another reader.**

**The two facades:**

- **The read facade**, under `kernel.Read[*model.Lookup]`, answers only for
  models already resident. It never loads, and a miss means "not resident".
- **The load facade**, under `kernel.Write[*model.Lookup]`, is today's `Get`,
  `Preload` and `Unload`.

**`ModelHandle` is a plain slot index.** `model` keeps a dense model table. A
`ModelRef` resolves once through the load facade into a `ModelHandle`, and from
then on a read is one index into the table, with no path clean and no string
hash. There is no generation, because a stale handle can only come from drawing
an unloaded model.

**Settled here: whoever holds the load facade also drives the bake and release
queues.** The recording scene's flush held `Write` on the Lookup and ran the
staged bakes and deferred releases against gfx's resource queue. After the split
each renderer did exactly that: the recording scene in its flush, and scene in
its load System, which is where it happens now. This followed from the facades
and from the two renderers never running together.

---

## Storability, and `m.List`

From [model's types as Components, or a binding layer over them](https://github.com/dvoyni/cog/issues/495).

**A type `model` exports for a System to hold must be storable:** `ModelRef`,
`MeshRef`, `ClipPlay`, `LightDescr`, `ModelLight`. All of them already are.
Cache answers, such as `ModelView` and the model material, are not Component
candidates. The spec states the rule so that a bare slice added to one of those
types is a violation and not a quiet regression.

**`ecs.List` moves to `libs/m` as `m.List`.** `List` is the storable form of
variable-length data and belongs beside `m.Maybe`. Once it is `m.List`, a plugin
can make a type storable without importing `bundles/ecs`. `model` does not
import the ECS, and nothing needs to guard that.

- `bundles/ecs/internal/types/list.go` imports only `iter`, `reflect` and
  `unsafe`, so it is a legal Library.
- **The move keeps** the generation header a Changed Hook compares, the
  registration walk admitting a List, and the `ecs_validate` checks on `Set`.
  Those are ECS knowledge, so they reach the List from the ECS side and do not
  live in `m`.
- **No alias is kept.** Every caller is in cog, so all of them are rewritten in
  the same landing. This follows [#468](https://github.com/dvoyni/cog/issues/468),
  which removed the recording scene's alias of `m.Transform`.

---

## The recording scene after the split

**The recording scene kept its name, its `OpQueue` API and its behaviour**
through the split, until #573 removed it. It lost the code that moved to
`model` and imported `model` instead. It kept:

- the queue: `OpQueue`, `ModelDraw`, `MeshDraw` and the recorded records;
- its `Material`, `MaterialTag`, `PassTag` and material table, with a file
  material's key derived from the model material's;
- its camera, pass and layer vocabulary, over `libs/m`'s projection maths;
- `LightRecord`, `prepareLights`, the layer and frustum tests;
- culling, sorting, the arena and emission;
- the per-frame temporary mesh and the shape enum;
- the Op inspection surface (`Ops`, `Passes`, `PassView`, `BatchView`).

**What the recording scene paid: nothing new.** Its flush already held write
access, and it kept loading on first draw through the load facade.

**During the carve, the recording scene's root re-exported `model`'s types as
temporary aliases**, such as `type ModelRef = model.ModelRef`, so cog-examples
and the ECS binding compiled unchanged. The Destination's "keeping its OpQueue
API" is about the ops, not about these re-exports. **The sweep deleted them**
([#539](https://github.com/dvoyni/cog/issues/539)): the recording scene's root
declared only the renderer, and every caller in cog and cog-examples names `model.ModelRef`,
`model.MeshRef`, `model.ClipPlay`, `model.LightDescr`, `*model.Lookup`,
`model.NewLookupAccess`, `model.Vertex`, `model.Config`, the `ErrModel…`,
`ErrMesh…` and spot-light reports and the rest. The aliases the recording
scene's own `internal/types` kept for the same purpose went too.

---

## model's root after the sweep

Settled by [#539](https://github.com/dvoyni/cog/issues/539). The carve exported
about a hundred names from `model`'s root so that the recording scene could
name them, and left which stay public to the sweep. **A name stays when:**

- either renderer's shipping code named it, or an app does (cog-examples);
- it is a report an app type-switches on, which is every `Err…` type;
- a name that stays names it in a signature or a field, so nothing public is
  unnameable: `MeshSource` (`NewMeshRef`), `BakeTextureFunc` (`EnsureBundled`),
  `Span` (`PackVertices`), `MorphBinding` (`AnimMorph`), `BakedClip`
  (`ResidentAnimation`), `PbrSlot` (`PbrSlots`), `AlphaMode` (`PbrState`),
  `LightKind` (`LightDescr`);
- it is one of the shader's records, sizes, flags or binding names, which
  [The shader's records](#the-shaders-records-and-their-packers) settled as a
  whole surface, whether or not a renderer reads each one today;
- it is the plugin's own: `Name`, `Config`, `Lookup`, `StorageReadMount`.

**Kept for the recording scene's tests, which stayed where they were.** These
were named only by tests, and the tests stayed in the recording scene because
they asserted what its flush bound and uploaded, which only its harness could
drive. The tests went with it in #573; the names stay on the root:

- the bundled material's construction, `PbrDefaults`, `PbrSlots`, `PbrSampler`,
  `NormalSlot`, `PbrState` with the `Alpha…` modes, `BundledPbr`, `SceneShader`
  and `SceneShaderPath`, from which the recording scene's material and flush
  tests built the material they expected to see bound (`model`'s own shader
  tests use the last three as well);
- `PackVertices`, which its vertex tests compared an upload against;
- the morph block's word counts (`MorphRangeWords`, `MorphTargetHeaderWords`,
  `MorphWordSize`), which its morph test read a delta buffer back by;
- `StorageStride`, `StorageSkinnedStride` and `StandardVertexAttrs`, which its
  end-to-end stride test asserted a resident primitive against.

**Taken off the root, 48 names**, because nothing outside `model` names them
and nothing kept names them in a signature. Each is still declared in
`model/internal/types`, where `model` uses it:

- the decoder's surface, `DecodeModel`, `DecodeDocument`, the twenty
  `Decoded…` types and the thirteen `Decoded…` constants. A second consumer of
  decoded data would be the second format that is out of scope;
- the unit geometry, `UnitBoxGeometry`, `UnitPlaneGeometry` and
  `UnitSphereGeometry`, which only `Lookup.EnsureUnit` calls;
- the default material numbers, which only the conversion and paint use;
- `SkinnedVertexLayout` and the eight per-attribute storage offsets
  (`StoragePosition` to `StorageWeights`). The recording scene named the offsets through
  aliases that nothing read, and a custom material reads the stored vertex
  through `VertexDecodePath`, not through offsets.

---

## The ECS scene after the split

From [#495](https://github.com/dvoyni/cog/issues/495), [#497](https://github.com/dvoyni/cog/issues/497),
[#501](https://github.com/dvoyni/cog/issues/501), [#509](https://github.com/dvoyni/cog/issues/509)
and [#521](https://github.com/dvoyni/cog/issues/521). Landed in
[#535](https://github.com/dvoyni/cog/issues/535), [#536](https://github.com/dvoyni/cog/issues/536)
and [#537](https://github.com/dvoyni/cog/issues/537), and on main in one merge
with [#538](https://github.com/dvoyni/cog/issues/538).

**scene repeated the recording scene's path, recording to `gfx` itself, and
imported nothing of it.** It was not redesigned to do anything the recording
scene did not, except draw Batches. Its design record is now its own:
[`scene.md`](../../../scene/docs/specs/scene.md). In summary:

- **Its Components wrap `model`'s values**, `model.ModelRef`, `model.MeshRef`,
  `model.ClipPlay` and `model.LightDescr`, and it does not register `model`'s
  types.
- **It declares its own copy of the recording scene's camera, layer and pass
  vocabulary**, with its names, shapes and zero values, and its own copy of the frame
  code that does not depend on the shader. What does depend on the shader is
  `model`'s.
- **Its load System** is the only scene System holding
  `kernel.Write[*model.Lookup]`. It keys each changed Entity into a Batch, and
  drives `model`'s bake and release queues.
- **Its recording System** holds the Lookup only for reading, through the read
  facade, buckets Entities into [Batches](../../../scene/docs/specs/scene.md#batches)
  by those keys, and draws one instanced draw per opaque Batch into
  `*gfx.OpQueue`, through `model`'s packers and binding names.
- **Its oracle is a recording `gfx.Backend`**, and it publishes no inspection
  API ([What it is tested against](../../../scene/docs/specs/scene.md#what-it-is-tested-against)).

---

## The fountains

From [which examples the redesigned ECS binding ships with](https://github.com/dvoyni/cog/issues/510).
These live in cog-examples, and describe them as they stood before #573 removed
the recording scene.

- **`cmd/ecs/fountain` is rewritten in place**, with the same frame and the same
  Components doing the same jobs, on `model`'s refs and scene's own types.
  It composes scene and not the recording scene, and cog-examples' headless
  harness starts it with `headless.NewECS`, which composes ecs and scene in the
  recording scene's place.
  Its `reference.png` was recaptured once, on the GPU, after the redesign
  ([#538](https://github.com/dvoyni/cog/issues/538)).
- **`cmd/scene/fountain` is new: the same frame through the recording scene's
  `OpQueue`**, with the same seed and `referenceStep`. Its motes are a plain
  slice, one recording call each with its own tint.
- **`internal/fountain` holds what they share:** the spray simulation, the clock,
  the xorshift stream, the layout constants, the HUD text, `spray_test.go`, and
  the expected figures for `referenceStep`. Each `cmd` keeps only its recording.

**Both HUDs read `ArmFrameCmd`.** Each arms one unfiltered snapshot per step and
sums the passes labelled with its camera's passes (`ground` and `forward`), which
leaves out canvas's HUD pass. Passes are passes, batches are draws, and drawn is
instances. The line reads `passes N  drawn N  batches N`. **The culled figure is
dropped from both**: fountain culls nothing, and exposing it from the ECS
binding would be new API.

**What each fountain's tests assert:**

- `TestTheHUDsArithmetic`: the census equals the tally, and there are two passes.
- `TestTheReferenceStepShowsEveryComponent`, through `headless.Backend` and the
  `ArmFrameCmd` snapshot: two passes labelled `ground` and `forward`, each with
  its clear; the fox and the nozzle drawn in `forward`; the basin drawn in both.
- `TestTheHUDReadsAsInReferencePNG` stays whole, batches figure included.
- **Against `internal/fountain`'s expected figures:** the passes, their whole
  labels and the instances in each pass must be equal. Draws may differ, and
  the ECS fountain's may be lower than the recording fountain's, never higher:
  two motes spawned on one step fade to the same tint, and scene batches them.
  The recording scene merged them too after
  [#49](https://github.com/dvoyni/cog/issues/49), so the two drew the same.

**Both comparisons assert, since [#538](https://github.com/dvoyni/cog/issues/538).**
`internal/fountain` holds `ReferencePasses`, each pass's whole label
(`scene.camera-100.ground`, `scene.camera-100.forward`) and its instances, and
`ReferenceSceneDraws`, the recording fountain's draws in each pass. The
recording fountain must equal both. The ecs fountain must equal `ReferencePasses` and stay
at or under `ReferenceSceneDraws` pass by pass. At step 600 the forward pass is
117 instances in both, drawn in 117 draws by the recording scene and 116 by
scene. The labels can be compared whole because scene keeps the recording
scene's spelling.

`headless.Engine.Passes` and `Ops` stayed for the recording scene's examples,
and fail a test on an engine from `NewECS`. The fountains do not call them.
`Lookup` and `LookupDevice` answer through `model`'s facades under either
renderer.

---

## What is not foreclosed

- **A second model format.** The thin seam means any decoder hands over the same
  plain data, so a collected Port for decoders can be declared when a second
  format arrives.
- **A decoded glTF camera**, as a `ModelCamera` data record beside `ModelLight`.
- **Per-instance properties** ([#520](https://github.com/dvoyni/cog/issues/520)),
  which would make a tinted crowd one draw.
- **Shadow descrs** in the model material, one for each variant.
- **Merging the test backends** (scene's, gfx's `fakeBackend`,
  cog-examples' `headless.Backend`).

---

## Shapes that were rejected

Each was ruled out by the ticket named, and most with the sequence that breaks it.

**Where the decoder lives** (charting):

- **`libs/gltf`.** The packing layer builds gfx resources (`gfx.BufferDescr` ×16,
  `gfx.VertexAttr` ×10, `gfx.MeshDescr` and more), and a Library may not import
  `slots/gfx`.
- **`bundles/scene/internal/types/gltf/`.** Right shape, wrong place: two plugins
  cannot share an `internal/` package, so the ECS binding could never import it.
- **`libs/geometry`.** The generators are gfx-free, but 187 lines do not justify
  a Library's `doc.go`, `id.go`, `internal/` and constructor package.

**The vocabulary** ([#494](https://github.com/dvoyni/cog/issues/494)):

- **A shared renderer bundle** for what both renderers needed. It would have
  brought back the coupling the split removed.
- **`model` declaring a camera.** No camera is decoded.

**The decoder seam** ([#496](https://github.com/dvoyni/cog/issues/496)):

- **Shared records**, the decoder importing `model`'s record types. It makes the
  decoder depend on the GPU layout, which gains nothing when the layout is
  applied after the decoder finishes.
- **Records inside the decoder package.** `model`'s runtime would reach into a
  package named `gltf` for its PBR record.

**Storability** ([#495](https://github.com/dvoyni/cog/issues/495)):

- **`model` importing `bundles/ecs`** to name `ecs.List`. An asset plugin would
  depend on an ECS that a non-ECS game never composes.
- **scene registering `model`'s types directly.** It claims the one Store each
  Go type can have, so a second ECS plugin gets `ErrDuplicateRegistration`. It
  is recorded in [`scene.md`](../../../scene/docs/specs/scene.md#shapes-that-were-rejected)
  as well.

**Residency** ([#497](https://github.com/dvoyni/cog/issues/497)):

- **A Component caching the resolved `ModelView`.** The app unloads the model,
  the next flush releases its buffers, and the Component still holds slices into
  the freed entry, so the next draw binds released buffers.
- **A read lock on today's loading `Lookup`.** Readers A and B both miss
  `fox.glb` and both load it, so it uploads twice. Making both writers
  serialises scene's Systems, which is a blocker.
- **A per-frame snapshot.** It is still taken under one of those two locks, and
  it copies every resident model every frame.
- **The app preloads everything, and a miss draws nothing.** An Entity naming a
  model that was never preloaded draws nothing, and nothing reports why.

**The shader's records** ([#521](https://github.com/dvoyni/cog/issues/521)):

- **The ECS binding copies the records.** A shader change that one copy misses
  still compiles, and that renderer draws garbage.
- **All of light selection in `model`.** `model` would take a cull mask, and
  layers are the renderer's.
- **The arena and sort keys in `libs/m`.** They are frame-building tools, not
  maths.
- **`model` exporting the arena, culling and emission.** That makes `model` a
  partial renderer.
- **Plain parameters to `PackFrameLighting`**, about ten of them.
- **The renderer fills the whole frame block** with only `radiance` exported. The
  defaults would be written twice and could come to disagree.
- **Each renderer writes its own animation append loop.** That copies part of the
  layout.
- **Reshaping the copied vocabulary**, such as nesting sun and ambient or
  renaming a type. That is improving, not splitting.

**Batches and the test oracle** ([#509](https://github.com/dvoyni/cog/issues/509),
[#501](https://github.com/dvoyni/cog/issues/501)) moved with scene's design
to [`scene.md` §Shapes that were
rejected](../../../scene/docs/specs/scene.md#shapes-that-were-rejected).

**The examples** ([#510](https://github.com/dvoyni/cog/issues/510)):

- **A 5 000-crate example.** The benches already measure it.
- **ECS versions of the recording scene's other examples.**
- **Two full copies of the fountain.** A difference in simulation would pass for
  a difference between renderers.
- **A stats resource published by the ECS binding.** It is the inspection surface #501
  rejected.

**The order** ([#522](https://github.com/dvoyni/cog/issues/522)):

- **`model` starts as a new bundle beside the recording scene**, with both renderers moving onto
  it later. For a while there would be two caches and two copies of 14.7k lines.
- **The ECS binding's redesign in several landings.** Two recording paths side
  by side would need a switch.
- **The ECS binding's tests move between the carve and the redesign.** That
  leaves the carve unguarded for it.
- **No aliases, with each carve step rewriting its callers.** Every step then
  mixes moving code with renaming.
- **Permanent aliases.** They give every model type two names.

---

## Required work

From [the migration order](https://github.com/dvoyni/cog/issues/522). Five stages,
in this order. **Every step ends green** in cog, cog-examples and feuds-26:

- `go build ./...`, `go vet ./...` and `go test ./...` pass;
- the js-tagged tests pass under node;
- the benches compile.

cog-examples and feuds-26 point at `../cog` through a `replace` directive, so a
step that breaks cog-examples lands together with its cog-examples fix. No step
has to measure performance, because a carve changes where code lives and not
what it does.

1. **`ecs.List` becomes `m.List`**, with every caller rewritten and no alias.
2. **The ECS binding's tests move onto the recording backend, and the
   fountains land.** All of it runs on the recording scene as it then was, so a
   carve that breaks its drawing shows up at the frame level.
   - The binding's `testBackend` and `RenderEvent` in the harness. The nine
     op-field tests and three `PassView` tests are rewritten as backend
     assertions, and the recording scene's `Op` reads are deleted.
   - The five benches gain their drawing bottom half.
   - `internal/fountain`, `cmd/scene/fountain`, and `cmd/ecs/fountain` rewritten
     onto it, with the HUD on `ArmFrameCmd`. The comparison of the two against
     the expected figures does not assert yet.
3. **The carve: the recording scene moves onto `model`.** Code moves out of it
   into `bundles/model`, and it imports it. Each step repoints its root aliases
   at `model`'s root. The ECS binding keeps proxying into its `OpQueue`
   throughout.
   `friends.go`'s test accessors move with the code they reach. In this order:
   1. the decoder and geometry. **This spec's first flip lands here**. Landed in
      [#529](https://github.com/dvoyni/cog/issues/529), with `Vertex` and its
      storage layout moved early and the conversion to GPU layouts left in
      the recording scene until step 2;
   2. the caches and `Lookup` and the model plugin, which registers the
      resource and takes `Config`. `mesh.md` moves here. Landed in
      [#530](https://github.com/dvoyni/cog/issues/530), bringing the conversion
      to GPU layouts, the per-frame animation resolution and the model
      material's forward-descr shape with it. The two facades and `ModelHandle`
      landed in [#531](https://github.com/dvoyni/cog/issues/531);
   3. the model material and the shader. Landed in
      [#532](https://github.com/dvoyni/cog/issues/532): the load-time key,
      `MaxClipPlays`, the override merges, and the WGSL with its mount;
   4. the shader's records and packers, the light-array filler, `FrameLighting`,
      `AppendAnim`, and the size and binding-name constants. Landed in
      [#533](https://github.com/dvoyni/cog/issues/533), with `PaintPbrRecord`.
      Projection maths went to `libs/m` in
      [#534](https://github.com/dvoyni/cog/issues/534).
4. **The ECS binding's redesign, in one landing.** It moves from proxying to
   recording to gfx, with its own copies of the arena, culling, sorting and
   vocabulary, the load System, and Batches. It stops importing the recording
   scene. The fountain comparison starts asserting. Its section of this spec
   moves into its own
   docs, and `ecs.md` §Binding is brought up to date. Built on the integration
   branch `model/ecsscene-redesign` as [#535](https://github.com/dvoyni/cog/issues/535)
   (the Components and the vocabulary), [#536](https://github.com/dvoyni/cog/issues/536)
   (the load System) and [#537](https://github.com/dvoyni/cog/issues/537) (the
   recording System), and landed on main in one merge with
   [#538](https://github.com/dvoyni/cog/issues/538), which moved the fountain
   onto the ECS binding alone and made its comparison assert. Its design is
   now [`scene.md`](../../../scene/docs/specs/scene.md).
5. **The sweep.** Every caller in cog and cog-examples names `model.X`, the
   recording scene's temporary aliases are deleted, and its spec is cut down to
   the renderer.
   Landed in [#539](https://github.com/dvoyni/cog/issues/539), which also took
   48 names nobody outside `model` used off `model`'s root ([model's root after
   the sweep](#models-root-after-the-sweep)), and moved that spec's sections on
   what is `model`'s to the end of this document.

---

## Out of scope

- **A second asset format** (FBX, OBJ, USD), and a collected Port for decoders
  that would only serve one.
- **An offline asset pipeline**, or any consumer outside the cog module.
- **Per-instance properties** ([#520](https://github.com/dvoyni/cog/issues/520)).
- **The recording scene merging separate calls** ([#49](https://github.com/dvoyni/cog/issues/49)),
  as part of the split. It landed afterwards as its own change.
- **Detecting a stale `ModelHandle`.** It is undefined behaviour by the
  standing rule.
- **Merging the test backends.**

---

## The model contract, moved from scene.md

> **Moved from the recording scene's `scene.md` by
> [#539](https://github.com/dvoyni/cog/issues/539).** The sections from here
> to the end were the recording scene's specification of what is now
> `model`'s: the glTF loader and residency, mesh baking, animation, the lights'
> packing and cap, the bundled PBR material, the Lookup facade and the
> shader-side contract. They keep their text, their ticket citations and their
> amendments, so "scene" in them often names the code that is now `model`'s,
> or the recording scene that [#573](https://github.com/dvoyni/cog/issues/573)
> removed - its `ModelDraw`, `MeshDraw`, `TemporaryMesh`, cameras and flush -
> and file paths and line numbers are as they were when each was written.
> Names were rewritten: a `scene.X` that is `model`'s now reads `model.X`. What
> was the recording scene's in each area - the draw calls, the light culling and
> layer test, and the flush that drove the Lookup - went with it; the renderer
> that ships, and its own design record, is
> [`scene.md`](../../../scene/docs/specs/scene.md).

---

## glTF models

([glTF model draw semantics and loading](https://github.com/dvoyni/cog/issues/14),
[glTF 2.0 feature inventory and loader choice](https://github.com/dvoyni/cog/issues/5))

`ModelDraw` was the recording scene's call, removed with it in #573. A model
is drawn today by scene's `Model` Component; see
[scene.md](../../../scene/docs/specs/scene.md).

### Loader

Parse with [`github.com/qmuntal/gltf`](https://github.com/qmuntal/gltf)
(v0.29.0, BSD-2-Clause) as a **parse layer only**: zero runtime dependencies, a
`Decoder` taking an `fs.FS` that maps straight onto `storage.FileSystem`, and a
clean `GOOS=js GOARCH=wasm` build. Scene converts the `gltf.Document` into its
own mesh, material and baked-pose types **in one pass at load and drops it**; the
document never appears in scene's API. Decode allocates about 2× file size once.

> **Amended by [#529](https://github.com/dvoyni/cog/issues/529).** The decode is
> `bundles/model`'s. Its decoder, `bundles/model/internal/types/gltf`, parses the
> file and hands over plain data: attribute arrays as the glTF library's own typed
> slices, index lists, unbaked curves and skins, morph target floats, material
> values, image references, lights and the flattened scene walk. scene converts
> that into its vertices, baked poses, morph blocks and PBR records, still in one
> load and still dropping the document, through `model.DecodeModel`. The unit
> geometry (`model.UnitBoxGeometry` and its two siblings) and `Vertex` with the
> storage layout it reports are `model`'s too, and scene names them through
> aliases. See [model.md](#the-decoder-seam).
>
> **Amended by [#539](https://github.com/dvoyni/cog/issues/539).** The
> conversion moved into `model` with the cache in #530, the aliases are gone,
> and the decode and unit-geometry forwarders left `model`'s root: nothing
> outside `model` calls them.

**Supported:** GLB and `.gltf` with external buffers and images, interleaved and
sparse accessors, 8/16/32-bit indices, per-primitive materials, generated flat
normals and tangents when missing, node tree with default scene, skins with
`inverseBindMatrices`, morph POSITION/NORMAL/TANGENT deltas with default weights,
animations on TRS and `weights` channels with STEP/LINEAR/CUBICSPLINE (all
resolved at load, since clips are baked), the full metallic-roughness material
set with two UV sets, alpha modes, double-sided, and samplers.

**Extensions in v1:** `KHR_texture_transform`, `KHR_materials_emissive_strength`,
`KHR_mesh_quantization` (dequantised at load, including morph deltas),
`KHR_lights_punctual` parsed and exposed **as data** for the app to declare —
nothing in scene converts a glTF light automatically, and plain directional
lights beyond the camera's sun are dropped. Rejected in `extensionsRequired`,
failing the model wholesale: Draco, meshopt, basisu, webp. Other `extensionsUsed`
are ignored.

**WebGPU gaps the loader papers over:** no `uint8` indices (widen to 16),
no 3-component 8/16-bit vertex formats (dequantise), no fan or loop topology and
no strips (converted to lists at load, so a model mesh is always a list and
batching, index buffers and the skinning path never branch on strip or fan
assembly), and no mipmap generation API (CPU box filter).

**Two topologies survive the conversion, not one.** A triangle strip or fan
becomes a triangle list; a line strip or loop becomes a **line list**, because
that is what gfx carries and there is nothing to turn a line into that is still
a line. Normals and tangents are triangle properties, so a line primitive gets
whatever the file supplied and is shaded by its base colour and emissive alone.
The invariant the batching and skinning paths actually rely on is that a model
mesh never assembles as a strip or a fan, which holds.

**A `POINTS` primitive is skipped and reported, and the rest of the model
loads.** gfx has three topologies — triangle list, triangle strip, line list —
and no point list, so there is nothing to convert a point cloud into. Adding a
fourth is engine work behind [#29](https://github.com/dvoyni/cog/issues/29), not
a loader decision, and the alternative here is losing a mesh that is mostly
triangles to one debug primitive. `MeshPrimitiveModes` is the only asset in the
repository this reaches, and it is where the report is asserted.

### Addressing

`path` names the file and is the **only cache key**. `Scene` names an entry in
the file's `scenes` array (empty is the default scene). `Node` names a node
within that scene (empty is the whole scene) — a **plain name, first depth-first
match**, not a slash path; a duplicate name reports once and keeps the first.

Both selectors are **names, and glTF names are optional**. A file whose scenes
carry no name has no addressable scene but its declared default, which is what
an empty `Scene` selects; the same is true of a node. This is not hypothetical:
`MultipleScenes` is the only file in the whole Khronos repository with more than
one `scenes` entry, and **both of its scenes are unnamed, as are both of its
nodes** — so the one asset that exists to exercise the `Scene` selector can only
be drawn through its default. The alternative, reading the selector as a decimal
index when no name matches, was rejected: it makes the selector a parsed string,
which is the same objection that keeps selectors out of the path.

A name is resolved **pre-order**, so a node sharing a name with one of its own
descendants resolves to the node — which is what "first depth-first match" says,
and what recording a subtree on the way back out would get backwards.

**Nodes only.** glTF `mesh` names are optional, non-unique and carry no place in
space, so a mesh is not addressable by name. Selectors are not encoded in the
path (`"props.glb#crate"`), because that makes the cache key a parsed string.

**A `Node` draw re-roots.** The node's authored world transform inside the file
is discarded and the draw's `Transform` replaces it, descendants keeping their
relative transforms. So `props.glb` + `Node: "crate"` behaves as an independent
asset however the artist laid the file out. `Node: ""` keeps the scene's root
transforms, because a scene *is* authored as one unit.

**A draw with an unmatched `Node` skips**, and never falls back to the whole
scene. One typo'd node name rendering an entire building at the origin is the
worse failure ([Model lookup facade](https://github.com/dvoyni/cog/issues/21)).

**A node whose authored world transform collapses an axis skips too**, under its
own report. Re-rooting *is* that transform's inverse, so a node scaled to zero
on some axis has nothing to draw the subtree through. A whole-scene draw of the
same file is unaffected and still draws it flat where the file put it, which is
why this is the draw's report rather than the load's.

**The report keys carry the selector, not just the path.** An unmatched node
reports under `"model:" + path + "#" + node` and an unmatched scene under
`"model:" + path + "#scene:" + scene`, so two typo'd names in one file are two
reports, a bad scene and a bad node are two more, and a bad draw repeated every
frame is still one. They are separate from the load's own `"model:" + path` key
and are not cleared by a successful load: the file is fine, the selector is not.

### Flattening

Load walks every scene in the file depth-first into a flat, **subtree-contiguous**
list of `{primitive, localMatrix, material, joint}`, each `localMatrix`
accumulated relative to the scene root. Depth-first order is exactly what makes
a subtree a slice rather than a filter, so a `Node` draw takes that node's
contiguous slice.

Three consequences:

- **A skinned node's own transform is ignored** per the glTF spec, so skinned
  primitives get an identity `localMatrix`.
- **A baked `localMatrix` with negative determinant reverses winding**, so load
  creates a `FrontFace: FrontCW` material variant for those primitives. Pipeline
  state is per material and the draw gets no say.
- **A node animated by TRS channels becomes a degenerate single-joint skin** —
  see [Animation](#animation).

**Every scene in the file is flattened, not only the default one.** `path` is a
model's only cache key, so a draw naming a scene has no second load to trigger
and every scene a selector can reach has to be there already. Geometry is
interned per glTF primitive across the whole file, so two scenes sharing a mesh
share the upload and the mesh id; what a second scene costs is its own placement
records.

The cycle guard is therefore **per scene, not per file**: a node two scenes both
root belongs to both, and a file-wide guard would leave the second empty.

**The re-root inverse is resolved per instance per frame, not precomputed at
load.** If an ancestor of the named node is animated, the node's true world
transform is time-varying and a load-time inverse is the wrong matrix — the crate
would inherit its ancestor's motion, contradicting the point of re-rooting. At
load scene records each named node's chain of *animated* ancestors, usually
empty; at instance-pack time, if the chain is non-empty, it walks the chain
against the same baked pose rows, inverts, and folds the result into the
instance world matrix. The packer already knows the clip and time, the chain is
a handful of joints, and the empty case costs nothing
([Baked pose buffer and skinning contract](https://github.com/dvoyni/cog/issues/15)).

**The chain is of *ancestors*, and a node animated in its own right keeps that
animation.** Re-rooting replaces where a node sits, not what it does: a wheel
node with a spin channel drawn by name spins about the draw's transform rather
than being frozen. So the node itself is never in its own chain, and a `weights`
channel is in nobody's — morph weights reshape a mesh and leave the node where
it was.

**Until a clip can play, the load-time inverse is the whole answer.** The chain
is recorded now; the pack-time walk lands with the baked pose rows, because the
baked pose of a model with no clip playing *is* the rest pose, and the inverse of
the rest pose is what a load computes. Nothing is silently approximate in the
meantime: there is as yet no clip to make the two differ.

### Materials and overrides

Each glTF material converts at load into its params - its texture bindings and
its numbers - owned by the model entry and shared by every draw of it. The two
override knobs do not overlap:

| knob | behaviour | case |
| --- | --- | --- |
| `Material != nil` | **replaces wholesale**; the file's params are not bound and do not survive | dissolve, silhouette, depth-only |
| `OverrideParams` | **merges by name** over each primitive's own params, as the draw's params, keeping everything it does not name | team colour, hit flash, fade |

A nil `Material` with no overrides binds the file's material directly, with no
copy. `OverrideParams` **broadcasts** to every material the draw binds — all six
of a multi-material model's — which is what the common per-draw override wants.
It is matched against the *resolved tag entry*, and a name that entry's shader
does not declare is **ignored rather than reported**: that is what keeps the
broadcast safe across tags.

**An override has one destination, and scene resolves none of it.** Every
member of the material - the five textures, the five samplers and every number
of the `scenePbrMaterial` uniform block, `baseColorFactor`, `metallicFactor`,
the five transforms and rotations among them - is a param gfx matches by name
against the reflected layout of the entry's shader. An override rides on the
draw's own parameter list, gfx resolves a draw parameter over a material one of
the same name, and it drops what the shader does not declare. That *is* the
matching rule, enforced by the same reflection every other binding goes
through. Until [#568](https://github.com/dvoyni/cog/issues/568) the numbers were a storage record scene packed
and merged overrides into itself; see
[The material's numbers are a uniform block](#the-materials-numbers-are-a-uniform-block).

So nothing is copied per draw: the `Material` is the entry's own, overrides or
not, and the overrides are the draw's params.

**Every member is a param, `uvSets` included.** `uvSets` is a packed five-bit
selector carried as a raw `u32` - which TEXCOORD set a slot samples is the
file's statement about its own mesh, so overriding it is a caller's business
and rarely a good one. A test reads the WGSL struct and fails on a member the
material carries no param for, because gfx packs one nothing supplies as zero.

**A parameter whose kind does not fit the member it names is refused by gfx**,
which reports the mismatch and drops the draw, on the footing it takes for any
shader's params. A vec4 member takes either `ColorParam` or `VecParam`.

**The two knobs compose rather than conflict.** The overrides reach a
replacement material by name, as they reach the file's. The file's numbers are
gone with its bindings, and a number neither names is packed as zero, so a
replacement that reads the bundled block supplies what it reads.

**`MeshDraw.Params` reach the material by name too.** A mesh draw is white
paint, and a param naming one of its numbers changes it, as a model's
overrides do.

**This was the recording scene's contract. scene overlays instead**
([#568](https://github.com/dvoyni/cog/issues/568)): its `Material` is laid over
each primitive's ingredients — the tag's shader or the default scene shader
under the primitive's variant, the file's params overlaid by name with the
default shader's and the tag's, the file's record with the same params merged
over it, and the file's state unless the tag names one — and a `Mesh` takes the
same path over `BundledIngredients`, its `Params` merged into the record too.
Wholesale replacement is then a shader declaring none of the file's bindings.
See [scene.md §Materials overlay the file](../../../scene/docs/specs/scene.md#materials-overlay-the-file).

### Loading

**Loading is synchronous, and a model that could not be loaded is skipped,
never substituted.** A draw of a path the cache does not hold reads, parses,
decodes and uploads it inside the flush that recorded the draw, so the model is
drawn in that same frame. A path that could not be loaded draws nothing — no
placeholder.

**The hitch is a property of this design, not an accident.** The JSON parse, the
image decodes, tangent generation and the vertex pack all run inside scene's
flush, holding `Write[*gfx.ResourceQueue]` and `Write[*gfx.OpQueue]`, so a large
file costs the frame that first named it several hundred milliseconds and
canvas's flush waits behind it. **`Preload(path)` is the lever**: it is the same
load, fired without a draw, so an app moves the cost into a loading screen it
controls. A game that skips `Preload` takes the hitch on first draw, which is
what gfx and canvas already do.

This replaces a two-command asynchronous load and the four-valued state machine
that described it. What was bought back is everything that existed only to
describe a load in flight: the `Missing → Loading → Resident → Failed` states,
the per-entry generation counter, the ghost rule that needed it, and the release
queue. There is no load in flight, so none of them has anything left to say.

**The load runs where the caller stands.** Scene's flush holds
`Read[storage.FileSystem]`, converted to an `fs.FS` once per frame rather than
once per model draw — handing a `storage.FileSystem` out as an interface
allocates — and a frame with no model draws pays nothing for it. It is the one
lock this design added, and it serialises against nothing a frame does:
`storage.FileSystem` is write-locked only by storage's own three mount commands.

**Residency is atomic and per path.** A model is drawable only when geometry,
baked poses, materials **and every one of its textures** are uploaded,
all of which happen before the call that asked for it returns — there is no
half-drawn model. **A failure never retries**: a typo'd path must not re-read the
file every frame forever, so whatever the load produced is cached, a failure
included, and it clears only on unload.

**Failure has two halves and they report differently.** The read is the asset
library's, so a file that cannot be opened is the library's failure to report —
once, keyed by the descriptor, wrapping the underlying error so
`errors.Is(err, fs.ErrNotExist)` still answers — and what the cache keeps is the
loader's `nil` default. Everything the loader itself finds wrong is scene's:
a file that does not parse, declares no scenes or requires an extension scene has
no decoder for is cached as an `ErrModelUnavailable` and reported under
`"model:" + path`. `State(path)` returns whichever of the two applies, and `nil`
when the model loaded.

**Partial failure binds a placeholder.** A model that parses but is missing a
texture still loads, and reports once. **What it binds depends on what the slot
asked for**: a *picture* slot — base colour, emissive — binds **magenta**,
because a missing base colour rendering white looks deliberate; a *data* slot —
normal, metallic-roughness, occlusion — keeps its own 1×1 default, because
magenta as a normal map is a surface lit from nowhere and as
metallic-roughness it is `metallic=1, roughness=0`, a mirror. A **rejected
required extension** is different — there is no geometry to fall back to, so the
model fails wholesale.

**Textures** live in a scene-owned cache, never canvas's atlas (wrap modes, mips
and per-texture samplers rule the atlas out). It is an `assets.Cache` of its own,
with its own params type: two caches sharing one params type would share one
report-once namespace. **No refcount**: nothing unloads automatically, so there
is nothing for a count to drive.

**A texture is named `{path, image index, colour space}`.** An external image is
named by its **resolved storage path** and shared across every model that binds
it; a GLB-embedded one has no path of its own, so it is named by its model's
path and its index — and its bytes ride in the descriptor's payload, which the
cache takes in place of a read. That is the whole reason the asset library lets a
`Blob` sit beside a `Name`: the alternative is reading the whole container once
per embedded image and re-parsing it to reach image N.

**The cache is consulted before the read**, which is what makes two models
sharing an external image one read and one decode. It used to be consulted after
the decode, at install time, so the second model decoded a picture it then threw
away.

**The colour space is part of the key**, because it is the *slot's* property and
not the image's: base colour and emissive are gamma-encoded pictures, and
metallic-roughness, normal and occlusion are data. One image bound to both kinds
of slot is therefore two GPU textures, and it has to be — sampling a normal map
through an sRGB view is a wrong picture with nothing in the frame to explain it.
**The material slot itself is not in the key**: one ORM image — occlusion,
roughness and metalness packed into one picture, which is glTF's standard
packing — stays one GPU texture, and picking the per-slot fallback stays the
material binding's business.

**Unload frees at the call, and the entry leaves the cache there.** A free
followed by a get is a **reload, not an error**, which is what a same-tick
unload becomes: the frame's own draws load the path again at the flush. What
still lands at the frame boundary is only the GPU buffers, through the same
pending-release list `ReleaseMesh` uses, so nothing the frame already recorded
draws from a dead buffer. `UnloadModel` frees geometry, baked poses and material
records **only — it does not cascade to textures**, because with no refcount it
cannot know whether another loaded model shares them by path. Unloading an
absent path is a no-op.

**Bounds** come from the POSITION accessor `min`/`max`, which glTF requires,
computed per primitive in the flattened local space and expanded by the summed
maximum morph position delta. A primitive whose accessor carries no `min`/`max`
makes the **whole model never-cull, reported once**.

---

## Buffer-built meshes

([Buffer-built models: static and dynamic](https://github.com/dvoyni/cog/issues/22),
[Buffer update strategy: static, per-frame, and dynamic](https://github.com/dvoyni/cog/issues/3))

The recording half, `TemporaryMesh` and `MeshDraw`, was the recording scene's,
and went with it in #573; a baked mesh is drawn today by scene's `Mesh`
Component.

### Vertices

The generic mechanism is canvas's verbatim: a plain-data struct implementing
`VertexLayout() []gfx.VertexAttr`, memcpy'd through `unsafe.Slice` into an
arena, its layout id cached by `reflect.Type`. Go 1.27 permits type parameters
on **methods**, so both mint functions carry `[TVertex]` directly.

Typed over raw `[]byte` + `[]gfx.VertexAttr` because the type system ties data
to layout, and because it lets scene recognise the standard layout **by type**
rather than by comparing attribute slices — which the custom-material check and
the bounds computation both need.

`model.Vertex` carries the **six** attributes of the standard layout, 72 B
authored and 32 B stored. It has no `Joints` and no `Weights`: no public path
ever wrote them — a skin binding is set only from a loaded model's animation —
so a buffer-built mesh never skins and the eight stored bytes would be dead in
every mesh an app can build. The **skinned layout**, the same six plus `JOINTS_0`
and `WEIGHTS_0` at 40 B, belongs to the glTF loader and is unreachable from the
public API. See [`mesh.md`](mesh.md) for both layouts row by row.

**A custom vertex layout requires a custom material.** The bundled PBR knows the
two named layouts and nothing else, and every variant of it reads a prefix of
one of them. The reverse is fine. A violation is
**reported once and the draw skipped** — the layout is recognised by Go type at
mint time, and the material is only known at draw time, so the check runs as the
frame prepares its draws and the report is keyed by the ref's id: once per ref,
however many draws named it. The stale-ref report is keyed the same way.

**A mesh draw taking the bundled PBR is white paint, `metallicFactor` 0.**
`MeshDraw` carries no colour: colour is a material's property, and a mesh that
wants one names a `Material`. glTF's own default is fully metallic, and a metal
has no diffuse at all — with no image-based lighting in v1 there is nothing to
reflect, so a mesh with nothing said about it would render **black**, which is the
one thing a draw with no material of its own must not be. This is the same
"paint, not metal" ruling the debug vocabulary already takes, minus the colour.

### Topology and indices

One `topology` argument, zero value `TriangleList`, every gfx topology passed
straight through. Indices are authored as `[]uint32` only, following the finding
that WebGPU has no `uint8` indices. What scene *stores* is narrower: a durable
mesh of 65535 vertices or fewer is uploaded as `uint16`, derived from the vertex
count rather than chosen, and a temporary mesh keeps `uint32` because narrowing
it would cost an allocating pass every frame rather than once. See **Index
width** in [`mesh.md`](mesh.md). The bundled PBR is documented as meaningful for
triangles only; a custom shader doing point sprites or a wireframe overlay is
legitimate and costs scene nothing to allow.

### Baking is deferred, and holds no gfx lock

`BakeMesh` mints the scene id and returns the ref **immediately**, queueing the
upload onto the `Lookup` for scene's own flush to drain — the flush already
write-locks `*gfx.ResourceQueue`, so a mesh baked and drawn in the same update
handler uploads in that same frame.

This keeps `LookupAccess` **GPU-free**, canvas's deliberate design. The house
style of write-locking `*gfx.ResourceQueue` in the app handler and threading it
down was rejected because `BakeBuffer` dereferences its backend with **no nil
guard**, so every caller would have to gate on `Ready()` and a mesh baked at
startup before the backend is installed would either panic or silently not exist.
Deferring gates it in one place.

`BakeMesh` **copies** the caller's bytes into a scene staging arena at call time
and hands `BakeBuffer` `copyData: false` at flush — one copy total. Consequently
the ref is a **scene** id and the `gfx.BufferDescr` lives inside the `Lookup`.

`UpdateMesh` is a **wholesale re-bake at any size**, deferred the same way,
recomputing the baked sphere in the pass that copies. There is **no capacity
concept in the API**: `ReBakeBuffer` already re-bakes at any length while
preserving the id, so growth is free at the gfx level. Sizing to a maximum vertex
count is a *performance* note about `CreateBuffer` plus bind-group invalidation,
not a constraint the API encodes. `UpdateMesh` rejects, reported once, a change
of vertex **layout** or **topology** — the pipeline key and the `meshID` both
assume they are fixed for the ref's life — and calling it on a temporary ref.

`ReleaseMesh` is explicit, frame-boundary and generation-counted. Drawing a
**released or stale** ref skips the draw and **reports once**, keyed by the ref's
id: a mesh that quietly stops appearing is the same failure class the load rules
guard against, and the generation counter is what makes a recycled id detectable
rather than drawing whatever now occupies that slot.

Invalid input — zero vertices, an index out of range, or an index count that is
not a multiple of 3 under `TriangleList` — is **reported once and yields a zero
`MeshRef`**, which then skips at draw time under the stale-ref rule. This departs
from `canvas.DrawTriangles`, which silently returns on bad input, because that is
a per-frame recording call where a report would spam every frame whereas a bake
happens once.

### Buffer lifetimes

There is no static/dynamic split at the API level: in WebGPU memory type is
chosen only by `MAP_*` flags, and every GPU-readable buffer is updated by a copy.
`queue.writeBuffer` is the recommended default on both paths — gogpu's native
path runs a 256 KiB-chunk staging belt flushed ahead of the user command buffer
in the same submit, and the browser path is `CopyBytesToJS` + `writeBuffer`. The
split that matters is **lifetime**, which gfx already encodes:

| lifetime | surface |
| --- | --- |
| durable geometry, baked poses, morph deltas | `ResourceQueue.BakeBuffer`, `ReleaseBuffer` on unload |
| per-frame instances, animation, lights, per-pass camera blocks | `BufferWithBytes` in the `OpQueue` temporary arena |
| caller-owned dynamic meshes | `BakeBuffer` + `ReBakeBuffer` (or the arena if regenerated every frame anyway) |

gfx needs nothing new for v1 and must not grow a mapped staging ring.
`gfx.BufferDesc.Dynamic` is dead code — never read, and a pure function of
`Kind` — and is deleted.

---

## Animation

Animation is **stateless**: each draw passes clip plays `{clip, time, weight}`,
and gameplay or the `anim` plugin owns time. Everything is baked at load;
runtime skinning and morphing are entirely vertex-shader work. **There are no
compute shaders in this scope.**

### Baked poses

([Baked pose buffer and skinning contract](https://github.com/dvoyni/cog/issues/15),
[GPU skinning and morph techniques survey](https://github.com/dvoyni/cog/issues/6))

**A pose record is 48 B — `[rot.xyzw][trans.xyz, _][scale.xyz, _]`** — three
aligned `vec4` loads, f32, holding **`globalJoint` alone**. Scale is a `vec3`.

Two rejected alternatives, both worth recording because each looks cheaper:

- **Premultiplying `globalJoint * inverseBind`** (the original research
  recommendation) is overturned. Inverse bind matrices routinely carry
  non-uniform scale from the bind pose, and premultiplying injects it into a
  record that is then decomposed to TRS, which cannot represent shear at all;
  degenerate single-joint skins have no inverse bind to premultiply; and the
  unpremultiplied buffer literally contains bone world transforms, which is what
  the fogged bone-socket work needs. `inverseBind` is instead a small per-skin
  array — one entry per joint, not per joint-frame — applied *after* the
  cross-play blend. Cost is one extra 48 B fetch and one 4×3 concat per
  *influence*, from an array small enough to stay L1-resident.
- **A 32 B record** with scalar scale packed into translation's `w` would cut
  pose memory and per-vertex loads by a third — the single largest cost in this
  design. It is rejected because **squash-and-stretch is animated non-uniform
  scale**, a mainstream idiom, and unlike a draw's `Transform` a pose has no
  call site where a per-axis scale could be written, so a stretched bone would
  be silently averaged away with nothing to correct.

**Normals and tangents use a precomputed normal matrix, never a shader inverse.**
Because the inverse bind can be non-orthonormal, so can the composed skinning
matrix. Load computes `transpose(inverse(inverseBind))` per joint. The shader
transforms the normal by the blended TRS rotation (orthonormal, so direct), then
by that matrix, then renormalises; the tangent takes the same path plus
Gram-Schmidt against the skinned normal, and its `w` handedness flips from the
inverse bind's determinant sign, also precomputed.

**The inverse bind and the normal matrix share one buffer.** They are both per
skin, both indexed by the same joint index, and both fetched on every influence,
so they interleave into a single `sceneSkinJoints` record: 64 B for the inverse
bind plus 48 B for the normal matrix, **112 B per joint**, exactly 7 × 16 with no
tail padding. One address computation instead of two, and both halves land
adjacent for all four influences.

Declared as **explicit `vec4` columns, not `mat4x3` and `mat3x3`**. WGSL pads
every matrix column to 16 bytes, so the record already carries 28 bytes that
matrix syntax cannot address — and the precomputed handedness sign needs
somewhere to live. Explicit columns make that padding addressable and give it a
free home.

Two buffers was the original shape; they were merged to recover a storage-buffer
slot, which cost nothing because nothing about the *semantics* moved — poses stay
unpremultiplied, the inverse bind still applies after the blend, the normal
matrix is still precomputed rather than inverted in the shader
([scene: the storage-buffer budget is eight of eight, not six](https://github.com/dvoyni/cog/issues/58)).

**Sample rate is 60 Hz, global**, from `Config.PoseSampleRate`; linear
interpolation, clip duration rounded up to whole frames. No per-clip override:
glTF has no field to express one. 60 rather than the researched 30 because
`STEP` channels and `CUBICSPLINE` curves are exactly what 30 Hz degrades visibly,
and glTF has both. **Per-vertex cost is unaffected by the rate** — always two
frames per play — so only storage doubles, and at demo scale that is noise
(a 24-joint three-clip rig is ~350 KiB).

**One joint index space per model.** Every skin's joints and every degenerate
node joint share a single numbering, remapped from each primitive's `JOINTS_0`
at load. Rows lay out per model path as `[rest frame][clip 0][clip 1]…`, so

```
row = clipBase + frame * jointCount + joint
```

is one MAD in the shader and the instance record needs only `clipBase`. The waste
is `(joints this clip does not animate) × frames`; the pathological file — many
independently-animated props sharing one file *and* per-prop clips — is precisely
what `Node` re-rooting exists to split apart.

**Row 0 of every model is an implicit rest frame**, the authored node hierarchy
resolved once. Without it a draw with `Plays: nil` has nothing to place its
geometry, because skinned nodes get an identity `localMatrix` and a degenerate
node's transform lives in the pose buffer — the model would collapse to the
origin. One extra frame per model (~1 KiB) makes `Preload` plus draw-with-no-plays
legal, and defines the zero-total-weight case.

**Any node whose world transform a clip can move becomes a degenerate
single-joint skin** with a weight-1.0 binding; nodes no clip reaches bake flat
into `localMatrix`. Rigid node animation — wheels, propellers, doors — is
ordinary glTF, so the alternative was a second animation mechanism with the
per-frame CPU hierarchy walk this design exists to eliminate. **The rule keys on
TRS channels only**: a node whose clip touches only its `weights` channel
creates **no joint** and keeps its authored `localMatrix`, so a morph-only model
loads with zero joints and an empty pose buffer
([Morph target contract](https://github.com/dvoyni/cog/issues/16)).

**"A clip can move it" is inherited, not local.** A static prop bolted to a
spinning turret moves with the turret, so the test is the node's own TRS
channels *or* a non-empty chain of animated ancestors. Keying on the node's own
channels alone would leave every mesh hanging under an animated bone frozen in
its rest place while its parent turned — which is the same failure the rule
exists to prevent, one level down.

**Only nodes that bind something claim a joint.** A node a clip steers but which
carries no mesh needs no joint of its own: its motion already reaches its
descendants through the hierarchy walk that bakes them, and a joint for it would
cost a 48 B row per frame for a binding nobody makes. The joints a model carries
are therefore every skin's joints, every mesh node needing a degenerate binding,
and the deepest animated ancestor of every *named* node — that last so a `Node`
draw can re-root against the frame, and reusing an existing joint on the same
node where there is one, because pose records are unpremultiplied and so hold
the same world transform whatever inverse bind they are paired with. Claiming a
fresh joint there instead very nearly doubles a rig: on Fox, whose every bone is
both named and animated, 24 joints become 40.

### Clip plays

```go
type ClipPlay struct {
    Clip   string
    Time   float32
    Weight float32
    Loop   bool
}
```

**Clips are addressed by name**, first match; an unknown name is reported once
and the play dropped.

**`Loop` is on the play, not the caller's time.** Gameplay owns time and `Time`
arrives already advanced, but whether a time past the end wraps or holds can only
be answered by whoever knows the clip loops, and that is not derivable from a raw
time value. `Loop` false clamps to `[0, duration]`; true takes `Time` modulo
duration, which makes negative time legal and a reversed animation free.

**There is no seam case in the frame pair.** The grid rounds *up* to whole
frames, so a clip's last row sits at or just past its own end and the wrapped
time always lands on a pair inside the clip's own rows. A pair straddling the
clip's boundary — its last frame blended back into its first — never has to be
built, and the clamped case simply takes the last row twice.

**Caps: 4 influences, 4 plays, 256 joints a model.** Influences are `JOINTS_0`
only. A 5th play is **dropped by lowest weight and reported once per model**.

The joint ceiling is the storage vertex's, not the pose buffer's: poses live in
a storage buffer indexed by row against a 128 MiB binding and would take any
count, but a vertex names its joint in **one byte**
([mesh.md](mesh.md#per-attribute)). It binds what a vertex can name — the joints
a model's *skins* claim, which are numbered first and contiguously — so the
plain joints a node binding claims, which ride the instance record in a full
`u32`, are outside it. **A model whose skins claim more fails wholesale at load,
naming the model**, because an index that did not fit would truncate to a
different bone with nothing reported. The cap is per model: every model has its
own joint numbering, so a level full of rigged characters does not share it.

**Play weights are normalised on the CPU** at pack time. The blend is a weighted
*mean* of TRS, not an additive layer: weights summing to 0.5 do not half-apply
the animation, they shrink every bone's translation toward the origin and mangle
the character. There is no legitimate non-unit sum, so "as given" would preserve
only the ability to express a bug. A total of ~0 falls back to the rest frame.

Per play the CPU folds `weight * (1 - frac)` and `weight * frac` into one scalar
each and emits `{baseRow0, baseRow1, w0, w1}` (16 B), so the shader does no
clip-length, wrap or normalisation arithmetic. It accumulates weighted TRS per
influence with quaternions sign-fixed against the running accumulator,
normalises, builds a 4×3, applies the inverse bind, and runs standard linear
blend skinning over 4 influences. Quaternion hemisphere continuity is fixed
within a clip **at bake**, so the frame lerp needs no runtime check.

**Unrepresentable data is best-effort plus one report.** Compose along the
hierarchy, decompose, recompose, and if the residual exceeds an epsilon report
once keyed `"model:"+path` and bake the decomposition anyway: a slightly wrong
elbow beats a missing character, and shear is invisible on virtually every real
rig. A single-keyframe clip and a zero-duration clip each bake to one frame;
neither is an error.

**Faithfulness is decided by recomposing, not by classifying the matrix.** A
collapsed axis in particular is *not* unrepresentable — a TRS record holds a zero
scale exactly — and treating it as such would pop an object animated down to
nothing back to full size. Scale keyframes reaching zero are ordinary: three of
the Khronos `InterpolationTest`'s nine cubes do it. The rotation such a matrix
cannot carry is rebuilt from the axes that survived, which is arbitrary and
unobservable, because a zero-scaled axis has no direction to get wrong.

**Vertex weights are normalised at load *and* renormalised in the shader**, and
the two are not redundant. glTF only says a producer *should* make `WEIGHTS_0`
sum to one and real files drift; unnormalised linear blend skinning then scales
the mesh as well as posing it. The load pass is what puts a weight inside
`[0, 1]` so that it has a `Unorm8` code to land on at all, and the shader's
divide by the accumulated total is what covers the sum that rounding four
weights into four bytes then misses — 11.7% of `Fox`'s vertices, always by
exactly one code ([mesh.md](mesh.md#per-attribute)). Neither half is permitted
to assume the other made the sum one, which is also what makes a malformed file
skinned correctly rather than silently shrunk.

**Skinning is model-only.** `MeshDraw` has no `Plays` field and no joint concept.

### Clip state machines

([Animation state machines](https://github.com/dvoyni/cog/issues/26))

```go
machine, err := model.NewClipMachine(clips, []model.ClipState{
    {Name: "Walk", Clip: "Walk", Loop: true},
    {Name: "Run", Clip: "Run", Loop: true, Rate: 1.5},
}, []model.ClipTransition{
    {From: "Walk", To: "Run", On: "run", Crossfade: 0.4, Ease: model.EaseCubicInOut},
    {From: "Run", To: "Walk", On: "walk", Crossfade: 0.6},
})

machine.Fire("run")                        // gameplay decides
events = machine.Step(dt, events[:0])      // gameplay owns time
machine.PlaysInto(&animation.Plays)        // scene's Animation Component
```

**A clip machine is a plain value the caller keeps.** A game holds one in a
field, a map or a Component. It is not a resource, not a System, and nothing
about it lives in `anim`. Animation stays stateless as far as the renderer is
concerned: the machine is gameplay's own
bookkeeping, and what reaches a draw is the same `[]ClipPlay` a hand-written
crossfade would build. Before it, every consumer did build that by hand, the
animated demo's fox and both fountains' foxes each scheduling weights of its
own.

**One type is both the definition and the runtime state, and it is storable.**
`ClipMachine` passes `ecs.Storable`, so it can be a Component without a second
"instance" type beside it. That decides its shape: its tables are `m.List`s,
the storable spelling of a slice, and its easing is the `EaseKind` enum
(`EaseLinear`, the zero value, `EaseCubicIn`, `EaseCubicOut`, `EaseCubicInOut`)
rather than a func. The curves are `anim`'s functions of those names,
re-implemented, because `model` does not import `anim`. Copying a machine to
many Entities shares the Lists' backing arrays read-only, so a copy costs
headers rather than tables, and each copy then steps on its own.

**Names resolve once, at construction.** `NewClipMachine` takes the
`[]ClipInfo` that `LookupDeviceAccess.Clips` returns, resolves every state's
clip to an index and a duration and every transition's states to indices, and
starts in `states[0]` at time 0. `Step` never touches the lookup, which is what
lets a System step machines without holding one. A name that does not resolve
is an error there, once, rather than a dropped play every frame:

| Error | When |
| --- | --- |
| `ErrClipMachineEmpty` | no states, so nothing to start in |
| `ErrClipStateClipMissing` | a state names a clip the model does not declare |
| `ErrClipTransitionStateMissing` | a transition's `From` or `To` names no state |
| `ErrClipTransitionTriggerInvalid` | a transition sets both `On` and `OnFinish`, or neither |

States and clips are addressed by name, first match, as clips always are.

**`Rate` scales the dt the caller passes**, per state, and zero reads as 1. A
game whose gaits must match their strides to ground speed passes a distance
rather than a time and sets each state's `Rate` to one over its gait's pace,
which is what the fountains do: their machines run on world units of ground
covered, and each clip advances exactly as far as its stride covers. The
crossfade runs on the same unscaled dt, so there it is measured in ground
covered too.

**`Loop` false clamps and holds the last frame**, as `ClipPlay.Loop` does, and
is what lets a state finish. `OnFinish` takes a transition on the Step a
non-looping `From` state's time first reaches its clip's duration. The new state
starts from time 0; the overshoot is not carried.

**Events.** `Step(dt, events)` appends `ClipEvent{Kind, State}`s, with
`StateName(i)` naming a state index. The order is fixed:

1. what every `Fire` since the last Step started, each as `ClipExited` for the
   state it left then `ClipEntered` for the one it started;
2. `ClipFinished`, once per entry into a non-looping state, on the Step its time
   first reaches its duration;
3. the `ClipExited` and `ClipEntered` of the `OnFinish` transition that same
   Step takes, if the state has one.

`Fire` returns only whether it found a transition, so what it starts is held
for the next Step, up to four Fires' worth; a caller firing more than that
between two Steps loses the oldest. The caller routes events itself, into
`anim` cues or its own code, because `model` does not import `anim`.

**The interrupt rule: a `Fire` during a crossfade freezes the mix.** The
incoming play and every outgoing play keep their relative weights and become
one outgoing group, which fades out as a whole over the new transition's
`Crossfade` and `Ease` while the new state fades in from time 0. The frozen
plays' clocks keep advancing, each at its own state's `Rate`, so the pose keeps
moving while it fades. Live plays never exceed `MaxClipPlays`: when the group
would push them past it, the lightest outgoing play is dropped and the rest keep
their proportions.

The failing sequence the rule exists for is a walk-to-run fade interrupted by a
jump halfway. Restarting the jump's fade from whichever of Walk or Run is
"current" snaps the other half of the pose away on the frame the jump is
fired; fading each outgoing play on a curve of its own grows the play count by
one per interrupt and has no answer at the cap. Freezing the mix keeps the pose
the player is looking at on the frame of the Fire, and costs one group.

**Weights need not sum to one**, because they are normalised at pack time
([Clip plays](#clip-plays)). The machine's do, which only makes the HUD's
numbers readable.

**What it does not preclude.** Blend layers by bone mask are a second
machine's plays on a subset of joints, which needs a mask on the play, not a
change here.

### Morph targets

([Morph target contract](https://github.com/dvoyni/cog/issues/16))

**Morphing is linear in the weights**, so blending N plays' weight vectors on the
CPU and applying the deltas once is *exactly* equal to morphing per play and
blending the results. Unlike the pose case there is no approximation to trade
away, so the entire morph blend is CPU-side and **the shader never sees a play**.

glTF `weights` channels bake onto the same 60 Hz grid as a plain CPU-side
`[]float32` per clip (`frames × targetCount`) that **never reaches the GPU**. At
pack time the CPU does the two-frame lerp per play and the weighted mean across
plays using the already-normalised play weights, and emits one final weight
vector.

**`MorphWeights` is positional** — the one index-addressed thing in the plugin.
Name addressing would put ~52 map hits per face per frame on the recording path
to re-derive a mapping the caller computed at startup; naming lives on the lookup
facade as `MorphTargets(path)` instead, so the per-frame path is a memcpy.
A non-nil `MorphWeights` **overrides the animated result wholesale**; nil falls
back to animated weights, then `node.weights`, then `mesh.weights`, then zero. A
**short slice leaves the remaining targets at 0**; a long slice ignores the tail
and reports once per model. Neither is an error.

**A slot belongs to a node, not a mesh.** glTF requires every primitive of a mesh
to carry the same targets in the same order, so a 3-primitive 8-target mesh
contributes **8** slots, not 24 — and `node.weights` overrides `mesh.weights`, so
two nodes referencing the same mesh have independent weights. The flattened list
is one entry per target of every morphed node in depth-first node order, so a
duplicated head appears as two runs of the same names. The *deltas* stay shared
per mesh, byte-identical between those two nodes: `morphBase` points at the
mesh's block while the weights come from the node's slots.

**One `sceneMorphDeltas` buffer per model.** One buffer per *primitive* is
overturned: it would mean a bind group per primitive, collapsing group 2's whole
reason for existing. Every morphed primitive's targets concatenate into the one
buffer and are reached by a base offset.

**The delta layout is specified in [mesh.md](mesh.md#morph-delta-storage)**, and
only there: the record's per-slot widths, the per-primitive ranges, the
per-target span and the address the shader builds from them. It used to be
described here as well, in a passage this document was the authority for — and
two specs describing one layout is how they drift, so what was here is a pointer
now. What stays in this document is the plumbing that layout change did not
touch: one buffer per model, the per-node weight slots, the CPU-side blend, and
the caps below.

Addressing needs **no base-vertex correction**: `gfx.MeshDescr` owns its own
buffers and always binds them at offset 0, so `@builtin(vertex_index)` is 0-based
within a primitive and indexes `sceneMorphDeltas` directly.

**Caps: 64 active targets**, culled first by `|w| < 1e-5` — absolute value,
because glTF does not clamp weights to `[0,1]` and a negative weight is
meaningful — then, if still over, dropped by lowest absolute weight and reported
once per model. Stored targets are unlimited. With sparse packing the cap no
longer constrains memory or layout at all and is purely a guard against runaway
per-vertex ALU.

**The morph half adds deltas and normalises nothing.** The naive reading of glTF
renormalises the normal after morphing *and* after skinning; the first is dead
work, because skinning is linear. Tangent deltas add to `xyz` and leave `w`
untouched.

**Bounds** expand at load by the maximum position-delta magnitude summed over the
targets: conservative (it assumes every target at weight 1 at once), computed in
one pass over delta data already being read, and it over-draws rather than
under-draws.

**Instancing morphed geometry saves draw calls, not vertex work.** 100 morphed
heads is one draw and 100× the morph ALU.

Buffer-built meshes have **no morph targets** in v1.

---

## Lights

([Lighting model and limits](https://github.com/dvoyni/cog/issues/17))

Naive forward: one light list per pass, **every shaded fragment loops all of
it**. The honest consequence, stated rather than hidden: adding a light costs
every shaded pixel in the pass, which is what makes the cap load-bearing rather
than decorative.

The sun and hemispheric ambient are per-camera fields
(see scene's `Camera` Component); the array holds point and spot
lights only, which is what makes the record branchless and 48 bytes with **no
`kind` field**.

```go
type LightDescr struct {
    Position   m.Vec3
    Direction  m.Vec3  // direction of travel; zero vector for a point light
    Color      m.Color // linear
    Intensity  float32 // zero means 1
    Range      float32 // zero means infinite
    InnerCone  float32 // radians; zero is a real value (falloff from the axis)
    OuterCone  float32 // radians; zero means pi/4, glTF's default
    Kind       LightKind
}
```

`PointLight` and `SpotLight` set `Kind` themselves over the one struct, so call
sites stay explicit, a hand-written point light leaves the cone fields zero, and
the per-camera light buffer is homogeneous without scene converting between two
structs.

```wgsl
struct SceneLight {
    position:   vec3<f32>,  // world
    invRange4:  f32,        // 1/range^4; 0 is infinite
    direction:  vec3<f32>,  // direction of travel; zero vector for point
    spotScale:  f32,        // 0 for point
    color:      vec3<f32>,  // linear, Intensity premultiplied
    spotOffset: f32,        // 1 for point
}

let toLight  = light.position - P;
let d2       = dot(toLight, toLight);
let L        = toLight * inverseSqrt(max(d2, 1e-12));
let window   = saturate(1.0 - d2 * d2 * light.invRange4);
let cone     = saturate(dot(-L, light.direction) * light.spotScale + light.spotOffset);
let radiance = light.color * window * cone / max(d2, 1e-6);
```

**Attenuation keeps glTF's falloff geometry but not its photometry.**
`Intensity` is **unitless**, documented as "radiance at one world unit".
Photometric units were rejected on a hard constraint, not taste: targets are
`RGBA8Srgb`, tonemapping is out of scope, so shading lands directly in 0..1 with
**no exposure control anywhere** — a 60 W bulb is ~64 cd and every frame would be
pure white. When HDR lands this becomes photometric by redefining the unit and
changing nothing else. `max(d2, 1e-6)` is a robustness guard for a light sitting
on a surface, not a falloff parameter.

**`Range` zero means infinite**, glTF's own default. Storing `invRange4 =
1/range^4` rather than `range` makes `saturate(1 - d^4 * invRange4)` evaluate to
exactly 1 when it is 0 — no branch, no `select`, no special case. Bug visibility
runs the right way too: a forgotten `Range` yields a light that reaches too far,
which you see immediately, rather than a silently skipped light. The residual
cost is that **an infinite-range light is unculled by construction and always
survives to the cap.**

**Spot cone** is `KHR_lights_punctual`'s smoothing, linear in cosine, with the
CPU precomputing:

```
spotScale  = 1 / max(cos(InnerCone) - cos(OuterCone), 1e-4)
spotOffset = -cos(OuterCone) * spotScale
```

so the shader is one `dot`, one MAD, one `saturate`. `InnerCone >= OuterCone` is
reported and the light skipped. **Trap worth a line:** a point light's
`direction` must be packed as an **actual zero vector**, not left uninitialised —
the `spotScale = 0` trick relies on `x * 0 == 0`, which is false for `NaN`.

**The sun stays out of the array.** Packing it as a directional entry would cost
an explicit `kind` field (with infinite range taken, no free discriminator is
left) and waste position, range and cone on that entry. The tiebreaker is that
the unification is unachievable anyway: **hemispheric ambient is normal-dependent,
not a direction**, so it can never join the loop.

**Cap: 16, silently, by contribution.** Past 16, scene keeps the 16 with the
highest contribution at the camera position — each light's own falloff evaluated
at the eye, times its colour's luminance — and drops the rest **with no error
reported**. The silence departs from the "reported once" rule used for plays and
morph targets, and the difference is principled: those caps are static and
asset-shaped, so "once at load, per model" is a well-defined moment and the
excess is an authoring mistake. A 17th light is **dynamic and camera-shaped** —
it appears when you turn around — so there is no natural "once", a per-frame
report is pure noise, and the degradation is continuous by construction, since
the lights dropped are exactly the ones contributing least. Record order was
rejected as the drop policy because it silently punishes recording order, which
a caller has no reason to believe is significant.

**Among equal contributions the offer order decides**, and for a scene watched
from outside its lights that is most of them. The score is the light's own
falloff window evaluated at the eye, so every light whose `Range` does not reach
the camera scores exactly zero, as does every spot the camera is not standing in
the beam of; the insertion replaces the weakest kept entry only if the newcomer
*beats* it, so a tie leaves the earlier light in place and the first 16 recorded
are the 16 kept. This is not a weakening of "the lights dropped are the ones
contributing least" — a light contributing nothing at the eye is contributing
least — but it does mean a caller who records more than 16 chooses which survive
by the order they record them in, and that a light mattering greatly to geometry
the camera is looking *at* can score nothing because it is far from where the
camera is looking *from*. The `pbr` demo is built on this: it records the lights
it means to keep first, and its five surplus lamps stand deep enough behind the
still life to score zero until the camera orbits in among them.

16 is a **fixed constant, not a `Config` knob**: a knob needs documented
interaction rules, and the answer to "I need 40 lights" is clustered lighting,
not a number that makes the naive loop slower. Being fixed is also what lets the
member be declared `lights: array<SceneLight, 16>` — 768 bytes inside
`sceneFrame` — rather than a runtime-sized array, which is a strictly smaller ask
on gfx's reflection work. `lightCount` still bounds the loop. The over-16
selection is a fixed `[16]` insertion by contribution — no sort, no allocation.

`Intensity` is premultiplied into `color` at pack time for the punctual array,
the sun and both ambient colours: it removes a per-fragment multiply and costs
nothing. All colours are **linear**, so an sRGB-picked literal must go through
`m.NewColorSrgb`.

**Nothing is reserved for shadows** — no sun shadow matrix, no comparison sampler
slot. An unused matrix and an unbound sampler are dead weight, and reflection
would type the sampler wrongly anyway, so the reservation would not even be
usable as reserved.

Which lights a pass offers - culling against the pass's frustum and the
layer test - is scene's, and is described in its
[README](../../../scene/docs/README.md).

---

## Bundled PBR material

([Bundled glTF PBR material contract](https://github.com/dvoyni/cog/issues/18))

The bundled PBR is a `gfx.MaterialDescr` like any other, wrapped in a `Material`
with **one `forward` entry and nothing else**. `MeshDraw.Material` /
`ModelDraw.Material` nil selects it.

### Parameters are glTF's names, verbatim

`baseColorFactor`, `baseColorTexture`, `metallicFactor`, `roughnessFactor`,
`metallicRoughnessTexture`, `normalTexture`, `normalScale`, `occlusionTexture`,
`occlusionStrength`, `emissiveFactor`, `emissiveTexture`, `alphaCutoff`.

These are **user-facing**: each is a param of the material, overridden by a
caller's param of the same name, so `gfx.ColorParam("baseColorFactor", c)` is
what a caller writes to tint a model.
Verbatim naming means the loader maps 1:1 with no translation table to drift, and
**the glTF specification becomes the parameter documentation** — including the
exact semantics of `occlusionStrength` and `normalScale`, which are easy to get
subtly wrong from memory. The uniform block itself is `scenePbrMaterial`,
reserved-prefixed, because no caller ever addresses the whole block.

**Five samplers**, one per slot (`baseColorSampler`, `metallicRoughnessSampler`,
`normalSampler`, `occlusionSampler`, `emissiveSampler`). glTF references a
sampler per texture and two slots of one material can legitimately differ — a
tiling ground beside a clamped decal — so a single shared sampler would silently
mis-sample a legal file. `SamplerDesc` stays comparable precisely so
`extensions/gfx/translate.go` dedupes identical descriptors to one object, making the GPU
cost near zero. Groups 0 and 2 use no samplers at all.

### Absent slots bind 1×1 defaults, and there are only two

WGSL requires every declared binding bound and gfx has no preprocessing, so
omitting a texture is not available without shader variants. Scene owns the
defaults and always binds all five; the factor parameters multiply through
unchanged.

Only **two** textures are needed: **a single white texel serves baseColor,
metallic-roughness, occlusion and emissive**, because 1.0 is a fixed point of the
sRGB transfer curve, so the sRGB-format slots and the linear-format slots both
read 1.0 from it. The second is the flat normal `(0.5, 0.5, 1)`. 1×1 rather than
larger because uploads go through `queue.WriteTexture`, which carries no 256-byte
row alignment, and for a constant texel every mip level is identical.

**A slot the file *named* and could not fill is a different case** and does not
come through here. An empty slot is the file saying nothing, and nothing is the
right picture; a named picture that did not arrive is a fault, and the colour
slots say so in magenta. The two meet in one place — the material binding takes
the texture cache's entry when it has one and falls back to these defaults when
the entry is the zero descriptor, which is exactly the placeholder a data slot
gets.

### Per-slot texture metadata is flattened, not arrayed

Each slot carries a UV-set selector and a `KHR_texture_transform`, declared as
**flat named members**:

```wgsl
baseColorTransform: vec4<f32>,   // offset.xy, scale.xy
baseColorRotation:  f32,
// ... x5 slots, plus a packed 5-bit uvSets selector
```

not `transforms: array<TexTransform, 5>`. gfx packs the record from name-matched
parameters and array members are not name-addressable, so flattening keeps every
member reachable through `OverrideParams` — and that buys a real capability
rather than symmetry: **animating `baseColorTransform` per frame *is* UV
scrolling** (water, lava, conveyor belts, scan lines), which the array form
forecloses permanently. It costs nothing to carry, since the record is a
256-byte-aligned bound range and the actual fields come to roughly 64 bytes, and
it widens no reflection ask.

**UV sets are capped at two** (TEXCOORD_0/1, glTF core's minimum), selected per
slot by one `select`. A slot naming `texCoord >= 2` is reported once and falls
back to set 0 — ignoring `texCoord: 1` would be a silent wrong-output failure on
a core feature. The transform is applied unconditionally, about 30 ALU across
five slots, less than one iteration of the 16-light loop. A single shared
transform per material was rejected as the classic trap: right for the common
atlas case, silently wrong the moment two slots differ.

### The BRDF is the Khronos reference, exactly

GGX/Trowbridge-Reitz distribution, Smith height-correlated visibility, Schlick
Fresnel, Lambert diffuse, `F0 = 0.04` for dielectrics lerped to `baseColor` by
`metallic`, `diffuseColor = baseColor * (1 - metallic)`.

Every demo model is a Khronos sample authored and screenshot-verified against
this exact BRDF, which makes it the only choice where "the model looks wrong" is
a **bug** rather than an open question about which approximation was picked.

**Ambient reaches metals through an analytic specular term.** The trap:
`diffuseColor = baseColor * (1 - metallic)`, so a pure metal has zero diffuse and
would render **black** everywhere the sun and punctual lights do not reach. So
ambient splits two ways, both scaled by occlusion:

- diffuse: `sceneAmbient(N) * occlusion * diffuseColor`
- specular: `sceneAmbient(reflect(-V, N)) * occlusion * EnvBRDFApprox(F0, roughness, NdotV)`

about six ALU and one extra `mix`. Lerping toward `baseColor` by metallic keeps
metals visible but makes them behave like diffuse paint — roughness stops
affecting them, which is precisely the property a metallic-roughness workflow
exists to express. **Honest limitation:** this approximates an environment that
does not exist, so a mirror-smooth metal reflects a smooth gradient rather than
the scene. Image-based lighting substitutes into exactly these two terms.

`KHR_materials_emissive_strength` **folds into `emissiveFactor` at load** and
clamps above 1 until HDR lands. Keeping it separate would only buy animatability,
and `emissiveFactor` is itself overridable.

### Pipeline state mapping

([Pipeline state growth for 3D](https://github.com/dvoyni/cog/issues/10))

| glTF | state |
| --- | --- |
| `doubleSided: true` | `Cull: CullNone` |
| `doubleSided: false` | `Cull: CullBack` |
| `alphaMode: OPAQUE` | `gfx.StateOpaque3D()` |
| `alphaMode: MASK` | `gfx.StateOpaque3D()` plus a shader `discard` against `alphaCutoff` |
| `alphaMode: BLEND` | `gfx.StateTransparent3D()` |
| node transform determinant < 0 | the same material with `FrontFace: FrontCW` |

`MASK` is **fixed-function-identical to `OPAQUE`** — it writes depth and batches
with the opaque geometry — and the cutoff is entirely a fragment-shader concern.
It cannot be alpha-to-coverage, which needs MSAA.

**The double-sided normal flip is unconditional**: `N = select(-N, N,
frontFacing)`. glTF requires the shading normal be flipped on back faces of a
double-sided material; making it conditional would cost a record flag and a
branch to save nothing, since for single-sided materials the select is a proven
no-op.

### Two named layouts, 32 and 40 bytes

The **standard layout** is six attributes at `@location(0..5)`, 32 bytes:
`POSITION` Float32x3 · `NORMAL` Unorm16x2 (oct32) · `TANGENT` Uint32 (oct 15/15
plus handedness) · `TEXCOORD_0` Unorm16x2 · `TEXCOORD_1` Unorm16x2 · `COLOR_0`
Unorm8x4. The **skinned layout** is the same six plus `JOINTS_0` Uint8x4 and
`WEIGHTS_0` Unorm8x4 at `@location(6..7)`, 40 bytes, and only the glTF loader
produces one. Eight of gfx's 16 attribute slots at most, well inside its 2048
stride cap. [`mesh.md`](mesh.md) is the authority on both, attribute by
attribute.

**Presence is trimmed exactly once, along a seam WGSL already had.**
`SceneVertexIn` declares locations 6 and 7 only under `SCENE_SKIN`, and a
converted geometry takes the skinned layout iff some placement draws it under
that define — one union per geometry, no new define, no new variant. Nothing
else is optional: the PBR needs tangents and both UV sets, and **`COLOR_0` is
included on failure mode**, not on evidence — it is glTF core, costs 4 bytes as
`Unorm8x4`, and omitting it renders a vertex-coloured model **silently white**
rather than erroring.

**Variants were rejected for a mechanical reason.** `gfx.ShaderDescr` is
source-or-path and the backend hardcodes `vs_main`/`fs_main`, so one module is
exactly one vertex plus one fragment stage — a variant is a whole separate module
carrying **its own copy of the entire BRDF**, the failure where a shading fix
lands in some copies and not others.

**Missing attributes are generated and repacked in place at load**, not bound
from a second buffer. gfx binds exactly one vertex buffer, and multi-buffer was
rejected on the finding that **total memory is identical either way** — repacking
gives one buffer at full stride, multi-buffer the original plus exactly the
remainder — so the only saving is a copy the one-pass conversion and
dequantisation largely already spend.

**The `arrayStride: 0` broadcast trick is unusable on cog's backends.** It is
spec-legal and gogpu's browser path forwards it untouched. Upstream's pure-Go
native path validated it not at all, and its backends diverged silently: the
software rasteriser dropped the whole draw, GLES read 0 as "tightly packed", Metal
set `stepRate: 1`. A trick that works in a browser and silently corrupts the
desktop HAL is exactly the divergence the map forbids
([gogpu/wgpu: arrayStride 0 is unvalidated and backends diverge](https://github.com/dvoyni/cog/issues/47)).
cog now pins the fork `dvoyni/wgpu` by `replace`, and its pipeline validation
rejects stride 0, a stride that is not a multiple of 4, a stride over
`maxVertexBufferArrayStride` and an attribute that runs past its stride, on every
native backend, before any HAL sees them. Stride 0 is rejected, not emulated, so
the trick stays unusable; the same patch is bound for upstream.

**Packing waited on a measured trigger and then got one.** v1 stored all eight
attributes wide, at 84 bytes; the narrowing above — oct normals and tangents,
per-mesh UV ranges, byte joints and weights — is a load-time encoding behind
unchanged attribute names, charted in [`mesh.md`](mesh.md) attribute by
attribute.

### Tangents

Tangents are **UV-gradient generated per triangle** when the primitive's material
has a normal map and `TANGENT` is absent, and an arbitrary orthonormal basis
otherwise. This is explicitly **not MikkTSpace**: a normal map baked against
MikkTSpace can show seams. No asset in the demo set exercises the generation path
— WaterBottle ships real tangents — which weakens the trigger rather than
strengthening it.

---

## Lookup facade

([Model lookup facade](https://github.com/dvoyni/cog/issues/21))

```go
la := model.NewLookupAccess(kernel, lookup)                          // bakes, unloads a model, reads the totals
dev := model.NewLookupDeviceAccess(kernel, lookup, fsys, resources)  // everything that loads, and the texture unloads

type ModelRef struct{ Path, Scene, Node string } // mirrors ModelDraw's selectors

type ClipInfo struct {
    Name     string
    Duration float32 // seconds
}
```

Bind `access.GetWrite[*model.Lookup]()` in the handler's `Lock` for the first,
and `storage.FileSystem` read plus `*gfx.ResourceQueue` write beside it for the
second, then use:

| Method | Facade | Result | Notes |
| --- | --- | --- | --- |
| `State(path) error` | device | nil, or why not | the only call that says *why* a model is not there; it loads like every other query, so by the time it answers the model is loaded or it failed |
| `Preload(path)` | device | — | the same load, fired without a draw; no return |
| `ModelLights(path, dst) ([]ModelLight, bool)` | device | the file's lamps | data; nothing converts one automatically |
| `Nodes(ref, dst) ([]string, bool)` | device | node names | a non-empty `Node` lists that subtree, the node itself first; the order is the flatten's depth-first order, never sorted |
| `Bounds(ref) (m.Vec4, bool)` | device | xyz centre, w radius | local space post-re-rooting; **rest pose for anything drawn through the pose buffer**, which is a skin *and* an animated node's own mesh |
| `AABB(ref) (min, max m.Vec3, ok bool)` | device | axis-aligned box | same space and pose rules |
| `Joints(path, dst) ([]string, bool)` | device | joint names | names only; count is `len` |
| `Clips(path, dst) ([]ClipInfo, bool)` | device | clip names and durations | |
| `MorphTargets(path, dst) ([]string, bool)` | device | target names | one flattened list per path, depth-first node order |
| `PoseBytes(path) (int, bool)` | device | GPU pose memory | |
| `MorphBytes(path) (int, bool)` | device | GPU delta memory | |
| `TotalPoseBytes() int` | plain | — | a running counter, O(1); no bool, because a sum over what is loaded is always real |
| `TotalMorphBytes() int` | plain | — | the same counter rule |
| `BakeMesh` / `UpdateMesh` / `ReleaseMesh` | plain | see [Buffer-built meshes](#buffer-built-meshes) | |
| `UnloadModel(path)` | plain | — | frees at the call; its buffers go at the frame boundary |
| `UnloadTexture(path)` / `UnloadAll()` | device | — | free a GPU texture at the call, which is what puts them on the device half |

### Every query returns `(value, ok)`, and every query loads

This is the facade's real contract. A query on a path the cache does not hold
runs the **same load a draw runs**, so a path is loaded exactly one way and
`Preload` is a lever for *where the cost lands*, **not a step you can forget**.
Inert queries would have a silent and *permanent* failure mode: a caller who
forgot `Preload` would poll an empty list forever with nothing to observe.

`ok` means **"this value is real"**, and nothing finer. It is false for an
invalid path, a missing path just queued, a loading path, a failed path, and a
resident path whose `Scene`/`Node` matched nothing. Inferring the same from a
zero return does not work: `PoseBytes` returning 0 is indistinguishable between a
still-loading model and a resident model with no skeleton, which is the whole
point of a memory report. When `ok` is false the `dst`-append accessors return
`dst` **untouched**, not a zeroed slice.

`ok` says only *this value is real*, so it cannot say why it is not — which is
what keeps **`State(path)` load-bearing**: failure is terminal, and a loading
screen watching only `ok` can never print a reason. There is no `Pending()`
aggregate and nothing to poll: a load has finished by the time the call that
asked for it returns.

**`State` returns an `error`, not a state word.** `nil` is loaded, and anything
else is the failure the load itself produced — the library's read failure, or
scene's own `ErrModelUnavailable`. A two-valued enum would have been a bool
wearing a costume, and a bool would have thrown away the one thing a HUD wants
to show.

**`State` is a query, so it loads too.** That is the rule applied without an
exception, and it is what makes a loading screen that calls only `State` work.

### Selectors, and what is not here

`Clips`, `MorphTargets`, `PoseBytes`, `MorphBytes`, `State`, `Preload` and the
unloads are **per path**: `path` is the whole cache key, a model has one joint
index space, and `MorphTargets` is one flattened list per path that `Node`
re-rooting does not renumber. Only `Nodes`, `Bounds` and `AABB` are
scene/node-scoped, and they take **`ModelRef{Path, Scene, Node}`** mirroring
`ModelDraw`'s own fields — three bare strings were rejected on a transposition
bug that compiles (`Bounds(p, "crate", "")` and `Bounds(p, "", "crate")` are both
valid and mean different things).

**Both answer about the rest pose, and "skinned" is the wrong word for which
geometry that covers.** A glTF skin is the obvious case, but a node with an
animation channel of its own *carrying a mesh* takes a degenerate single-joint
binding for exactly the same reason, and its placement leaves the instance
record for the pose buffer too. The flattened `local` matrix of both is the
**identity**, so a bound placed through it is a bound at the origin for geometry
the frame draws elsewhere — and under a `Node` selector it is worse than that,
because the re-root then applies the inverse of a transform that was never
applied. The load therefore keeps a second matrix per primitive, its **rest
placement**: `local` for everything the instance record places, and the node's
authored world transform for everything a pose row places. Row 0 of the pose
buffer is the authored hierarchy resolved once, so the two agree.

This is not a refinement, it is a correctness rule, and it was wrong until
`loading` looked: `CesiumMilkTruck` animates its two wheel nodes, so
`AABB(Node: "Wheels")` answered `inverse(world) · meshBox` — a box the wheel
never occupies — and the whole scene's box counted both wheels at the origin.
The demo's assertion is that the two wheel pairs, being one mesh under one local
rotation beneath two differently offset parents, re-root to the **same** box.

**Neither query reports where an animated model is *this* frame.** Replaying the
blend on the CPU is the per-frame hierarchy walk the design exists to remove,
which is also why such a draw is never culled. A caller who needs the live bound
computes it.

**Both bound geometries are published** because scene already has both: the tight
sphere is baked per node anyway, and the AABB is the load-time by-product it is
computed from. Publishing only the AABB would be a regression, since a sphere
derived from one is the circumsphere, up to √3 loose. `Bounds` uses the same
`m.Vec4` convention as `MeshDraw.Bounds`, so the name means one thing across the
plugin.

Over a **multi-primitive** subtree the √3 claim is narrower than it sounds, and
the implementation records the real rule. `Bounds` is the union (`m.Sphere.Union`)
of the primitives' own spheres, each transformed by the re-rooted placement;
`AABB` is the union of those primitives' boxes, each *refit* around its
transformed corners. Transforming a sphere is exact and refitting a box is not,
so the sphere wins wherever the placements rotate — which is the case the √3
remark is about. Where nothing rotates, the AABB's own circumsphere can be the
tighter of the two. **Neither dominates in general**, and both are published
precisely so a caller picks the one their test wants rather than deriving one
from the other.

A primitive whose POSITION accessor declared no min/max has no bound to
contribute, and makes both queries `ok = false` for any ref that selects it.
Returning a bound over the rest of the model would be a real-looking number the
unbounded piece sticks out of, with nothing in the answer to show the hole. A
ref that resolves to a real node carrying no geometry at all is also `false`,
but reports nothing — the absence of a report is what tells it apart from a typo.

**Mesh names are cut** — a draw addresses nodes only, so a mesh name is a string
a caller cannot act on. **No texture queries** beyond `UnloadTexture`. `Joints`
is names only, **no hierarchy** — parents and rest transforms are what bone
sockets need and they are purely additive when that lands. `Clips` returns name
and duration together because a caller needs the duration to know when a one-shot
play has ended and to normalise `Time`.

Every list accessor appends into a caller `dst`. Returning the internal slice
read-only would be free but dangles past the handler's lock scope, which is the
one thing `LookupAccess` forbids. These are cold paths — naming lives here
precisely so a caller resolves names to indices **once at startup**.

### Failure edges

- **An invalid path never enters the cache at all.** The facade validates it
  where the caller is standing (canvas's `validateResourcePath` rules), reports
  it once and returns — no entry, no tombstone — so a typo is permanently a typo
  and `UnloadModel` on the string the caller passed is what clears the report.
- **An unmatched `Scene`/`Node` on a resident model** returns `ok = false` and
  reports once, keyed `"model:" + path + "#" + node`. An unload clears every key
  under that path's prefix, not just `"model:" + path`, so a file that failed on
  a typo and was unloaded reports again if it fails again.
- **`Nodes` answers for a degenerate node**, one whose authored world transform
  collapsed an axis. Its names are a real answer; only *re-rooting* it is
  impossible, so it is `Bounds` and `AABB` that reject it — through the same
  `view()` a draw goes through — while `Nodes` resolves the scene and the
  subtree without one.
- **`UnloadTexture` frees every texture the path baked**, because colour space
  and embedded-image index are part of a texture's cache key while `path` is the
  whole of what a caller can name. For a `.glb` that means every image the
  container carries. That is a predicate over entries rather than a key, so it is
  one `FreeWhere` and each freed entry's report key goes with it. Nothing checks
  whether a resident model still binds them: this is the lever for a texture
  whose models are already gone, and the no-cascade wart is what makes the two
  directions asymmetric.
- **`UnloadAll` is models and textures, and nothing else.** Buffer-built meshes
  are the caller's own handles — a lookup-wide sweep has no way to tell them
  their `MeshRef`s went stale — and scene's own unit meshes, default textures
  and null skin would be re-baked on the very next frame.
- **An unloaded entry is deleted, not reset.** There is no load in flight for a
  tombstone to defeat, so the slot simply goes and the next draw or query loads
  the path afresh. (`MeshRef.generation` is a different mechanism, guarding a
  caller's own handles, and is untouched.)
- **Unload is the only retry lever.** A failure clears only on unload, so
  `UnloadModel(p)` on a failed path lets the next draw or query load it again,
  including recovery from an invalid path once the string is fixed. There is no
  `Retry`/`Reload`: it is `UnloadModel` + `Preload`, and because freeing is
  immediate the two may be called in that order in one handler.

### The facade splits, so no System pays for loading

Every verb that can load needs an `fs.FS` and the resource queue at the call,
because the load runs there. Putting all of them on one facade would have made
`NewLookupAccess` four dependencies — and one of its consumers is an **ECS
System whose entire use of it is two `BakeMesh` calls**. That System would have
had to declare `*ecs.Write[*gfx.ResourceQueue]` to bake a cube, serialising it
against canvas's flush, scene's flush and gfx. **Widening an ECS System's lock
set is rejected outright in this repo, not traded off.**

So `NewLookupAccess(k, lookup)` keeps its two dependencies and carries the mesh
verbs, `UnloadModel` and the two totals, and
`NewLookupDeviceAccess(k, lookup, fsys, resources)` carries everything that
loads plus the two unload verbs that free a GPU texture. The cost becomes
visible where it belongs — in each consumer's own `Lock` closure.

**`LookupDeviceAccess` is a convention, not a local choice.** Canvas splits the
*opposite* halves under the same name: there the loading half is cheap and the
unloading half is the device's. Both are named for what they carry, because the
constraint is the device either way.

---

## Shader-side contract

([Bind-group frequency convention](https://github.com/dvoyni/cog/issues/9))

The frequency convention is a **WGSL group-numbering contract**, not new gfx
machinery. gfx binds whatever reflection reports and never renumbers, so scene
expresses its three frequencies purely in shader source. **This binds scene
shaders only**; canvas keeps its own numbering untouched.

| group | frequency | contents |
| --- | --- | --- |
| 0 | per pass | `sceneFrame`, `sceneInstances`, `sceneAnim` — bound once per pass |
| 1 | per material | the material's numbers as the uniform block, plus its textures and samplers |
| 2 | per model | baked poses, inverse binds, normal matrices, morph deltas |
| 3 | — | a custom shader's own bindings ([#568](https://github.com/dvoyni/cog/issues/568)) |

Ascending frequency, lowest group changing least. Web's floor is 4 bind groups,
so three fit with one spare for shadows or post-processing to claim without
renumbering.

### The material's numbers are a uniform block

```wgsl
@group(1) @binding(0) var<uniform> scenePbrMaterial: ScenePbrMaterial;
```

Every other numeric input is a **storage buffer** from the per-frame arena, the
renderer's own. The material's numbers are the one exception, and the reason is
who owns them: they are the shader's, not the renderer's. gfx packs a uniform
block per draw from the draw's params by reflected member name, with the draw's
params over its material's, so a renderer hands a material's params over and
never learns its layout. A shader of the caller's own gets its same-named
members filled the same way.

**It was a storage record the renderer packed, and that was the coupling.**
gfx cannot fill a storage struct by name, so each renderer carried the bundled
PBR's record, merged every override into it by hand and bound it for every draw
whatever shader was in effect. A caller's shader could not receive numbers of
its own through that path, and one declaring `scenePbrMaterial` differently read
PBR bytes as its own. [#568](https://github.com/dvoyni/cog/issues/568) moved the numbers into the block and deleted
the record, its packer, its merge and its binding from both renderers.

The storage form was chosen when gfx's uniform path gave every draw a buffer
object of its own. It no longer does: every draw that declares a block gets a
256-strided slot in one arena buffer (`extensions/gogpu/internal/gfxuniformarena.go`),
the same stride the storage record padded to, per draw rather than per batch -
and a batch is one draw. `uniformMax` (`slots/gfx/internal/translator.go:29`)
caps a block at 256 bytes, and a shader declaring more is refused when it is
reflected, as `gfx.ErrUniformBlockTooLarge` ([#101](https://github.com/dvoyni/cog/issues/101));
the block is 160, and a test pins it under the cap. gfx allows a shader one
uniform block, and the block is **composed from three sources** so a shader over
the bundled stages can add per-draw numbers of its own to it:
`materialprologue.wgsl` opens `struct ScenePbrMaterial {`, `materialfields.wgsl`
lists the PBR members, and `materialepilogue.wgsl` closes it and binds it.
`material.wgsl` includes the three. An app shader includes the prologue, a
fields source of its own that includes the published fields and lists its
members after them, and the epilogue, *before* it includes the stages; includes
are once per path, so the material's own three are then skipped and the block is
declared once, with every member. The other order does not compile: the
extension's members land outside any struct. An extension of an extension
includes that one's fields source in its own, so they stack. They share the 96
bytes the PBR members leave, and each names its members with a prefix of its
own. Larger or non-numeric data still rides in textures, or in the one storage
buffer the bundled shader leaves. All three sources are published, as
`model.MaterialProloguePath`, `model.MaterialFieldsPath` and
`model.MaterialEpiloguePath`.

**Still no index.** A `u32` material index in the instance record **does not
work**: gfx packs at translate time on the render thread, because offsets come
from `Backend.ShaderLayout`, while a renderer writes instance records at record
time on the update thread — it would have to write an index for a block gfx has
not laid out yet. Two writers, one field, opposite sides of the thread
boundary. **The binding is the addressing**, and group 1 rebinds per material
exactly as intended.

**Scene bindings do not bypass name matching.** Scene injects
`BufferParam("sceneFrame", …)` and friends as ordinary per-draw parameters and
the existing matcher binds them exactly like a material texture. The plan cache
is keyed by parameter *shape*, so injecting the same names on every draw keeps
shapes identical and hits the cache every time. A separate "system bindings"
channel would be a second path to keep in sync for no capability.

The `scene`-prefixed name space is **reserved** for engine-supplied bindings,
mirroring canvas's `canvasTexture`/`canvasSampler`. A material parameter named
`scene*` is an app bug; gfx does not police it, the material simply loses.

### Declared bindings

| binding | group | contents |
| --- | --- | --- |
| `sceneFrame` | 0 | view, projection, viewProj, camera position, view direction, sun direction and colour, ambient sky/ground, `lightCount`, `lights: array<SceneLight, 16>` |
| `sceneInstances` | 0 | `array<SceneInstance>`, bound by range per pass |
| `sceneAnim` | 0 | `array<vec4<f32>>` arena, indexed by `sceneInstance.animOffset` |
| `scenePoses` | 2 | baked 48 B pose records |
| `sceneSkinJoints` | 2 | per-skin, per-joint 112 B record: inverse bind and normal matrix interleaved |
| `sceneMorphDeltas` | 2 | `array<u32>`, one block per morphed primitive: per-slot ranges, a base/first/count per target, then the records ([mesh.md](mesh.md#morph-delta-storage)) |

Plus the material's own group 1 bindings, which its params fill: the
`scenePbrMaterial` uniform block and the five textures and five samplers
(see [Bundled PBR material](#bundled-pbr-material)).

**The storage-buffer budget is seven of eight.**
Every reflected binding is emitted with visibility `Vertex|Fragment`
unconditionally, because reflection walks module globals without consulting entry
points, so a buffer only the vertex stage reads still consumes a fragment-stage
slot — which means the count above is seven in **each** stage, against the
browser core-adapter floor of 8.

It was eight, then seven, and it is eight again. Interleaving the two per-skin
arrays into one `sceneSkinJoints` buffer recovered a slot at no gfx cost, because
they share a joint index and are fetched together per influence
([scene: the storage-buffer budget is eight of eight, not six](https://github.com/dvoyni/cog/issues/58),
correcting the "six with two spare" figure the closed tickets record). That
recovered slot was the one permitted growth, and
[scene: UV0 and UV1 narrow against a per-mesh range](https://github.com/dvoyni/cog/issues/219)
spent it on `sceneMeshes`, the per-mesh UV range every narrowed UV decodes
against. The fully animated variant then sat exactly on the browser core floor,
until the material's numbers left storage for the uniform block
([#568](https://github.com/dvoyni/cog/issues/568)) and gave one back.

Three rules follow, and they are contract rather than guidance:

- **No scene shader may declare a ninth storage buffer.** The spare the uniform
  block freed is the caller's, not the bundled shader's: any further per-draw
  datum of cog's must go into a buffer that already exists.
- **A caller-supplied material may declare one of its own.** It may freely use
  the bindings scene binds on every draw — those are scene's and already counted
  — which is what the `procedural` demo does.
- **The debug check compares against `gfx.DefaultLimits()`, never against the
  device's reported limits**, and covers every shader gfx reflects, not just
  scene's own. A desktop adapter reports hardware limits (200 storage buffers is
  ordinary), so checking the real device passes a build that cannot run in a
  browser. The device's limits belong in the *message*, not the comparison: name
  the declared count, the device's limit and the web floor, so the diagnostic
  says which platform breaks.

Exceeding this in a browser is no longer found first as a lost frame. The check
is `checkWebLimits` (`slots/gfx/internal/limits.go:26`), and it runs when a
shader is created, on every platform. A shader past the floor is reported once
as `gfx.ErrShaderExceedsWebLimits`, naming the limit, the declared count, the
web floor and the device's limit. It is a portability report, not a refusal: the
shader is kept and renders on a device whose own limits allow it, so a desktop
run names the shader that a browser cannot run.

Moving `sceneFrame` to a uniform block is the named next lever when another
storage buffer is needed, and is filed rather than built
([scene: move sceneFrame to a uniform block](https://github.com/dvoyni/cog/issues/100),
parked, and blocking [sun shadow maps](https://github.com/dvoyni/cog/issues/52)).
gfx's uniform path is per draw only, one 256-strided arena slot each; reflection
holds a single uniform block; and nothing produces a `gfx.BufferUniform`, so no
range binding reaches a uniform-typed binding. What that ticket leaves open is
the gfx surface for a per-pass uniform binding. The silent truncation it once
listed is gone: a block over 256 bytes is refused
([#101](https://github.com/dvoyni/cog/issues/101)).

### The `sceneAnim` block

Per-instance animation parameters are **indirect**. `sceneInstances` is bound
once per pass and shared by every draw in it, so a fixed record would have to be
sized for the worst case — 64 B of skinning plus 256 B of morph weights, a ~320 B
tax on every debug line against ~48 B of content. Instead `sceneInstance` stays
~64 B and `animOffset` indexes a second per-frame buffer that only animating
draws write to.

```
vec4 0: { playCount: u32, targetCount: u32, morphBase: u32, morphStride: u32 }
vec4 1: { _, _, _, _ }                               // 4 words reserved
then  :  playCount   x { baseRow0: u32, baseRow1: u32, w0: f32, w1: f32 }  // 16 B
then  :  targetCount x { targetIndex: u32, weight: f32 }                   // 8 B,
                                                                           // padded to a vec4 boundary
```

**`vec4 1` is wholly reserved.** It carried `morphTargetStride` — the
`vertexCount * morphStride` a dense delta address multiplied by — and a morph
target now stores records only for the span of vertices it moves, so each one
carries its own base in its block's header and no per-primitive stride exists to
fold ([mesh.md](mesh.md#morph-delta-storage)).

Morph weights are a **count-prefixed sparse list**, not a dense 64-float block:
the CPU knows which entries are non-zero before it writes anything, so a 52-shape
face with 5 active shapes costs 40 B instead of 256 B and the shader loops 5
times over real work instead of 64 times with a `continue`. **The zero-skip
branch disappears entirely**, because zeros never reach the GPU.

**Four animation states, two independent counts.** A draw is skinned only,
morphed only, both, or neither; `playCount` and `targetCount` are each
independently zero-checkable and `animOffset == SCENE_NO_ANIM` covers neither. No
flags bitfield — two counts the shader reads anyway already carry the
information.

The three morph words are per-*primitive* constants duplicated per instance, 12 B
of the 32 B header. Putting them in the material's uniform block would remove
the duplication exactly, and was rejected: it would put scene geometry constants
into a block gfx packs on the render thread while scene records on the update
thread — the two-writers-across-a-boundary problem the binding-as-addressing
design exists to avoid.

### WGSL functions

These are the signatures the bundled shaders implement. **They are not published
in v1** — see the next section — but they are fixed here so that publishing them
later changes nothing.

```wgsl
struct SceneVertex { position: vec3f, normal: vec3f, tangent: vec4f }

// The 99% call: morph, then skin, per glTF order.
// Handles animOffset == SCENE_NO_ANIM and the SCENE_NOSKIN flag.
fn sceneDeformVertex(inst: u32, vertexIndex: u32,
                     joints: vec4u, weights: vec4f,
                     v: SceneVertex) -> SceneVertex

// The escape hatch: fully blended joint transform, inverse bind applied.
fn sceneJointMatrix(inst: u32, joint: u32) -> mat4x3f
fn sceneSkinMatrix(inst: u32, joints: vec4u, weights: vec4f) -> mat4x3f

struct SceneSurface {
    position:  vec3<f32>,   // world
    normal:    vec3<f32>,   // world, normalised
    baseColor: vec3<f32>,   // linear
    metallic:  f32,
    roughness: f32,
    occlusion: f32,
}

// Sun + every punctual light + hemispheric ambient scaled by s.occlusion.
fn sceneShadeSurface(s: SceneSurface) -> vec3<f32>

struct SceneLightSample { direction: vec3<f32>, radiance: vec3<f32> } // surface -> light

fn sceneLightCount() -> u32
fn sceneLightSample(i: u32, position: vec3<f32>) -> SceneLightSample
fn sceneSun() -> SceneLightSample                 // radiance zero when SunDirection is zero
fn sceneAmbient(normal: vec3<f32>) -> vec3<f32>   // mix(ground, sky, normal.y*0.5+0.5)
fn sceneCameraPosition() -> vec3<f32>              // the transform's eye; a real viewer only under Perspective
fn sceneViewDirection(position: vec3<f32>) -> vec3<f32> // unit, surface -> viewer, under every projection

struct ScenePbrSurface {
    surface:  SceneSurface,
    emissive: vec3<f32>,
    alpha:    f32,
}

fn scenePbrSurface(uv0: vec2<f32>, uv1: vec2<f32>, normal: vec3<f32>,
                   tangent: vec4<f32>, color: vec4<f32>,
                   worldPos: vec3<f32>, frontFacing: bool) -> ScenePbrSurface
```

Two levels for lighting because the common custom material wants a shaded
surface, while the main reason to write one — a toon ramp — needs per-light
`NdotL` *before* shading; without the low level such a shader must reimplement
attenuation and the cone, reintroducing exactly the inconsistency the contract
exists to prevent. `SceneSurface` carries **no view vector** (that is
`sceneViewDirection(s.position)`, one less field to get wrong and one the frame
answers correctly under every projection) and **no emissive** (emissive is the material's own output, not lighting, and debug lines
are self-lit through `emissiveFactor`, so a lighting function owning it would
read as a contradiction). A shader writes `sceneShadeSurface(s) + emissive`.

**The view direction is the frame's, not the camera position's.**
`sceneCameraPosition()` is the eye read straight out of the camera transform,
and it is a real viewer only under `Perspective`. An orthographic camera has no
eye point — its view direction is constant across the frame rather than radial
from the transform's translation — and an oblique one looks one way while its
viewer sees another, so every view-dependent term (specular, fresnel, rim,
anything consuming `nDotV`) differenced against the eye lights vertical faces as
if edge-on and floors as if head-on: the exact inverse of what is drawn. For
`Orthographic` the error was already there and merely small, because ortho
cameras tend to sit far away with narrow framing; `Oblique` turns it from a
subtle bias into a visible artefact, and the one fix serves both.

So `SceneFrame` carries `viewDirection: vec4<f32>` — **xyz the constant world
direction from a surface towards the viewer, w a mix selector**: 1 when that
constant is the answer, 0 when the shader must difference against
`cameraPosition` per fragment. Only `Perspective` takes the 0, and
`sceneViewDirection` is one `mix` rather than a branch or a discriminator
member. The constant is the direction that leaves both screen coordinates
unchanged — `(0, -Shear, 1)` in view space, which at `Shear: 0` is the camera's
own +Z and so serves `Orthographic` by the same line. It is resolved through
`cameraBasis`, the same unscaled world matrix `cameraView` inverts, so the two
cannot disagree about which matrix the camera is.

`sceneCameraPosition()` stays exported for the genuine distance work — a fog
term, a detail fade — that means the transform.

`scenePbrSurface` returns everything one set of texture fetches produces in one
call, rather than separate emissive/alpha helpers that invite the same texture to
be fetched two or three times — the compiler *may* common those up, but "may" is
not a contract, and this is the hot path.

The bundled fragment shader then reads as what it is:

```wgsl
let r = scenePbrSurface(uv0, uv1, normal, tangent, color, worldPos, frontFacing);
// alphaMode MASK: if (r.alpha < alphaCutoff) { discard; }
return vec4(sceneShadeSurface(r.surface) + r.emissive, r.alpha);
```

### Custom shaders

([scene: custom shader contract and prelude](https://github.com/dvoyni/cog/issues/48),
over [gfx: implement the WGSL shader preprocessor](https://github.com/dvoyni/cog/issues/144))

A caller may supply its own WGSL, as the `Shader` of a `scene.MaterialTag`
in a `scene.Material`, beside the tag's `State` and `Params`. gfx
preprocesses it, so it `//#include`s what model publishes by absolute storage
name and lights with the engine's own functions instead of re-typing them.
Include-once is by resolved path and the flattened module is line-preserving,
as [preprocessor.md](../../../../slots/gfx/docs/specs/preprocessor.md)
specifies, so a WGSL error still lands on a real line of the source that has it.

**Five of the bundled shader's sources are published.** Each is a constant in
model, and each constant's doc and the source's own `DECLARES:` header list
every name it declares, which an includer must not declare again. A test holds
the five lists and the sources together.

| constant | source | declares | bindings |
| --- | --- | --- | --- |
| `model.VertexDecodePath` | `builtin/model/vertexdecode.wgsl` | `sceneOctDecode`, `sceneDecodeNormal`, `sceneDecodeTangent`, `sceneDecodeUV`; three `SCENE_` constants | none |
| `model.FramePath` | `builtin/model/frame.wgsl` | `SceneFrame`, `SceneLight`, `SceneLightSample`; `sceneCameraPosition`, `sceneViewDirection`, `sceneAmbient`, `sceneSun`, `sceneLightCount`, `sceneLightSample`; `//#const SCENE_MAX_LIGHTS` | `sceneFrame`, storage, `@group(0) @binding(0)` |
| `model.PbrPath` | `builtin/model/pbr.wgsl` | `SceneSurface`, `ScenePbrSurface`; `SCENE_PI`, `SCENE_DIELECTRIC_F0`; the BRDF terms, `sceneEnvBRDFApprox`, `scenePunctualContribution`, `sceneShadeSurface` | none of its own; it includes `frame.wgsl` |
| `model.VertexStagePath` | `builtin/model/vertexstage.wgsl` | `vs_main`; through its includes the vertex structs and everything the stage reads, each named `scene`, `Scene` or `SCENE_` | groups 0 and, under the variant's defines, 2, exactly as the bundled shader |
| `model.FragmentStagePath` | `builtin/model/fragmentstage.wgsl` | `scenePbrFragment`; through its includes `SceneVertexOut`, `PbrPath` and `FramePath` | `scenePbrMaterial` and the five textures and samplers, group 1 |

**The two stages make an app shader the bundled PBR plus a step**
([#568](https://github.com/dvoyni/cog/issues/568)). `scene.wgsl` is now
`VertexStagePath`, `FragmentStagePath` and a one-line `fs_main` calling
`scenePbrFragment`; an app shader is the same two includes, its own bindings in
group 3 and its own `fs_main`, so a shading fix reaches it with nothing copied.
Group 3 is the one bind group the scene layout leaves free. A test flattens and
lowers such a shader under all four variants and checks it declares the bundled
module's bindings and its own, nothing else.

A material fills a `SceneSurface` however it likes, from its own vertices, its
own textures or a procedure, and writes `sceneShadeSurface(s) + emissive`. That
is the sun, every punctual light in the pass and the hemispheric ambient,
exactly as the bundled material is lit, so a custom surface beside a bundled
one agrees with it about where the light is.

The other eight sources stay private: `instance`, `material`, `skin`, `morph`,
`anim`, `deform`, `vertex` and the root `scene.wgsl`, though the two stages
bring what they declare in whole. What they declare changes
with the bundled shader and the records model packs, and nothing outside model
may name it. A material that needs the instance record declares its own copy of
`sceneInstances`, as the `procedural` demo does.

**The prelude is split, not monolithic, because a declared binding must be
bound.** Including `PbrPath` declares exactly one binding, `sceneFrame`, and
scene binds it on every draw, so the prelude costs a material
nothing it could fail to fill. A monolithic prelude would declare the instance,
material and animation buffers too, and oblige every consumer to fill group 2
for a debug line with no model. Getting it wrong is no longer invisible: since
[gfx: an unsupplied storage buffer binding fails silently](https://github.com/dvoyni/cog/issues/133),
a declared storage binding nothing fills drops that draw and reports
`gfx.ErrStorageBufferUnsupplied`, naming the shader, the parameter and its group
and binding, once per shader and parameter. The draw is lost; the frame is not.

**`SCENE_MAX_LIGHTS` needs no supply from an includer.** `frame.wgsl` declares
it as `//#const SCENE_MAX_LIGHTS=16`, which is `model.MaxLights`, and the
renderer packs exactly that many light records whatever material a draw uses.
The bundled shader supplies `MaxLights` over the default through
`gfx.ShaderConst`; a caller material supplies nothing and gets the same number.
Supplying any other value reads a light array the frame does not hold. A test
builds an includer with no supply and pins the reflected array at `MaxLights`.

**The storage-buffer rules bind caller materials too.** The fully animated
variant holds seven of the eight storage buffers the browser floor allows, so:

- a caller material may declare **one storage buffer of its own**;
- it may declare any of the ones the renderer already binds on every draw:
  `sceneFrame` (through `FramePath`, or `PbrPath`), `sceneInstances`,
  `sceneAnim` and `sceneMeshes`;
- through `FragmentStagePath` it gets `scenePbrMaterial`, the uniform block the
  draw's material params fill: for a mesh draw, white paint with its own params
  laid over it by name. It is the one uniform block gfx allows a shader, and
  the app adds its own members to it by composing it first from the three
  published `Material…Path` sources.

A test builds a material that includes `PbrPath` under scene and
reflects the module gfx handed the backend: one binding, `sceneFrame`, storage at
0/0, with a `MaxLights` light array. The `procedural` demo is the proving
consumer: it shades through `sceneShadeSurface` and declares only
`sceneInstances` beside what the prelude brings.

**Two findings that bind the bundled shaders themselves:**

- **Every declared binding must be bound.** Reflection is naga, which
  deliberately does not compact unused globals, so every declared
  `@group/@binding` lands in the explicit `BindGroupLayout`. A storage binding
  nothing fills drops the draw with `gfx.ErrStorageBufferUnsupplied`, and a bind
  group the device refuses is reported as `gogpu.ErrBindGroupRefused`. This is
  why the null skin and the 1×1 default textures exist, and why each variant
  declares only what it reads.
- **Every reflected binding is emitted `Vertex|Fragment`**, so a vertex-only
  buffer consumes a fragment-stage slot too; see the budget gap above.

### Binding cost

`resetAcc()` runs on every `SetPipeline` and after every `Draw`, so gfx re-emits
every binding per draw. That is left exactly as it is: the bind-group cache
already returns the *same object* for an unchanged group, so the only real cost
is the redundant `SetBindGroup` call, killed in the backend by comparing against
the last group bound — a pure optimisation with **zero API change**. Two
correctness rules on that filter: reset it on **shader change**, since bind-group
layout compatibility across shaders cannot be inferred from object identity, and
reset it at every **`BeginPass`**, since bind-group state does not survive a
render pass boundary. Draws are sorted by material, so runs are long and the
filter earns its keep; group 0, invariant for a whole pass, is bound once.
