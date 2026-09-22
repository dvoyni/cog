# cog model — specification

`github.com/dvoyni/cog/bundles/model` is the plugin that owns everything a model
file can contain: glTF decode, geometry generation, vertex and morph packing,
GPU upload, the model and texture caches, and the PBR shader with the records it
reads. Two renderers are built on it. `bundles/scene` stays the declarative
renderer and keeps its `OpQueue` API. `bundles/ecsscene` stops proxying into
`scene.OpQueue` and records to `gfx` itself. This document specifies `model`,
what each renderer keeps, and the order the split lands in.

**It is a split, not an improvement.** `model` is carved out of
`bundles/scene/internal/types` (14.7k lines) and scene's per-frame code, and
ecsscene repeats the path scene already takes. Anything that would make either
renderer do more than it does today is a separate issue. Two of them are open:
[per-instance properties](https://github.com/dvoyni/cog/issues/520) and [scene
merging separate calls](https://github.com/dvoyni/cog/issues/49).

The design is bound by four requirements, in this order:

1. **The import table holds.** `.github/instructions/architecture.instructions.md`
   is the hard constraint on every placement. A root holds declarations, aliases
   and pure forwarders. `libs/*` imports only `libs` and `kernel`. No plugin
   reaches into another's `internal/`.
2. **One upload per model.** One cache, one mesh table, and every renderer reads
   them.
3. **ecsscene imports nothing of scene.** What both renderers need is `model`'s
   (assets and the shader's layout) or `libs/m`'s (maths). There is no third,
   shared renderer bundle.
4. **Never cost System parallelism.** ecsscene's readers of model residency run
   side by side. Only loading and unloading are exclusive.

Two standing rules come with it, and no section below reopens them:

- **An app runs `scene` or `ecsscene`, never both.** Running both is undefined
  behaviour. Nothing is designed to make it work, and nothing guards against it.
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

**Stages 1 to 4 of [Required work](#required-work) are implemented.**
`bundles/model` is a Bundle with a plugin, `modelplugin.New`, that registers
the `*model.Lookup` resource. It holds the glTF decoder, the unit geometry,
`Vertex` with the storage layout it reports, the conversion to GPU layouts, the
model and texture caches, the mesh table, the Lookup with its two facades, the
model material, the bundled shader, and the records it reads with their
packers, and scene imports all of it from model's root. ecsscene records to
`gfx` itself over the same `model`, and imports nothing of scene; it landed on
main in one merge ([#538](https://github.com/dvoyni/cog/issues/538)). **Its
part of this design moved to its own spec**,
[`ecsscene.md`](../../../ecsscene/docs/specs/ecsscene.md): its Components and
vocabulary, its copy of the frame code, its two Systems, Batches and what it is
tested against. [ecsscene after the split](#ecsscene-after-the-split) keeps the
summary. What is left is the sweep, stage 5, which deletes scene's temporary
aliases. [`mesh.md`](mesh.md) moved here with the cache, and `scene.md` is cut
down to the renderer in the sweep.

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
- [scene after the split](#scene-after-the-split)
- [ecsscene after the split](#ecsscene-after-the-split)
- [The fountains](#the-fountains)
- [What is not foreclosed](#what-is-not-foreclosed)
- [Shapes that were rejected](#shapes-that-were-rejected)
- [Required work](#required-work)
- [Out of scope](#out-of-scope)

---

## Vocabulary

The glossary is [`CONTEXT.md`](../../../../CONTEXT.md). The terms this document leans on:

- **Model material.** What one material in a model file becomes once loaded: a
  forward graphics material for each shader variant, the PBR record, and a
  content key fixed at load. It names no Pass tag. It is not a **Scene
  material**, which is a renderer's and carries tags.
- **Batch.** One instanced draw with one properties record. It is scene's word
  and ecsscene's. "Group" is the word to avoid.
- **Read facade / load facade.** The two ways to reach the one `*model.Lookup`:
  under `kernel.Read`, which never loads, and under `kernel.Write`, which loads,
  preloads and unloads.
- **`ModelHandle`.** A plain slot index into `model`'s dense model table. A
  `ModelRef` resolves to it once. It has no generation.
- **Carve.** Moving code out of scene into `model` without changing what it does.
- **Sweep.** The one landing that rewrites callers from `scene.X` to `model.X`
  and deletes scene's temporary aliases.

---

## What was measured

From [where ecsscene's frame time goes today](https://github.com/dvoyni/cog/issues/493)
([results](https://github.com/dvoyni/cog/blob/research/ecsscene-baseline/bundles/ecsscene/docs/research/ecsscene-baseline.md)),
for 5 000 crates in view on a Ryzen 9 7950X3D at `6a5254d`:

| where | share of an 8.3 ms frame |
| --- | ---: |
| the ECS walk, the copy out, and `OpQueue.Model` | 4.6% |
| scene's queue flush bookkeeping | 2.6% |
| `expandModels` | 14.2% |
| `prepareDraws`, of which re-keying the file material each draw | 24.5% (19.6%) |
| cull and layer test | 0.24% |
| sort | 0.12% |
| `emit`, per batch into gfx | 42.4% |

- **Sort, cull and layering are not the cost.** Together they are under 0.4%.
- **Proxying into scene is about 7%.**
- **The call shape is the cost.** One call per Entity makes 5 000 Batches. The
  same crates as one call are 2.7 ms against 8.2 ms.
- **Re-keying a model's own material each draw** is 20% of the per-Entity frame
  and 55% of the instanced one. The key belongs to the loaded material, which is
  `model`'s.

These figures are why ecsscene's redesign is about Batches and not about a faster
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
and [where scene's per-frame drawing code goes](https://github.com/dvoyni/cog/issues/521).

**The line: `model` owns everything a model file can contain, and a renderer owns
drawing.** Drawing means the queue, passes, layers, culling, sorting, and the
renderer's own way of saying "draw this".

Every name the inventory classified as `model/decode`, `model/pack`,
`model/cache`, `model/geom` or `model` goes to `bundles/model`. Every `scene` row
stays in `bundles/scene`. Of 405 declarations, 269 land on the model side and 66
on the renderer's. The rows that needed a decision:

| name(s) | lands in | reading |
| --- | --- | --- |
| the model material (today `modelMaterial`, `loadedMaterial`) | `model` | see [The model material](#the-model-material) |
| `ShaderVariant`, `VariantStatic`, `variantSkin`, `variantMorph`, `variantSkinMorph`, `VariantCount`, `VariantFor` | `model` | chosen from skin and morph, which are model facts |
| the bundled PBR shader (`SceneShaderPath`, `SceneShader`) | `model` | it reads `ScenePbrRecord`'s layout, so the shader and the record move together |
| `Material`, `MaterialTag`, `MaterialKey`, `MaterialKeyOf`, `PassTag`, `TagForward` | each renderer, its own | a renderer wraps the model material's forward descr under its own tag |
| `CameraDescr`, `CameraID`, `ProjectionKind` and its three values, `Pass`, `DefaultPass`, `DepthClearFar`, `LookAt` | each renderer, its own | `model` declares **no** camera, since none is decoded. A decoded glTF camera would arrive as a `ModelCamera` data record |
| `Projection`, `ViewDirection`, `WorldToScreen`, `ScreenToWorld`, `ScreenToRay` and helpers | `libs/m`, over plain parameters | each renderer's camera calls them |
| `LightKind`, `LightPoint`, `LightSpot`, `LightDescr`, `ModelLight` | `model` | the loader already fills them |
| `LightRecord` | each renderer | the recorded light carries a layer mask |
| `MaxLights` | `model` | the length of the shader's `Lights` array. [#521](https://github.com/dvoyni/cog/issues/521) replaced #494's row here |
| `Vertex`, `VertexLayout`, `MeshRef`, `MeshRecord`, `BakeFunc`, `Span`, `MeshBaker`, `pendingMesh`, `rebakeIndices`, durable minting and baking | `model` | `model` owns all mesh residency: file primitives, unit shapes and app-built meshes alike. There is one mesh table |
| `MeshSource`, `MeshNone`, `MeshDurable` | `model`, reduced | `model` knows only durable meshes |
| `MeshTemporary`, `TemporaryMeshID` | `scene` | the declarative queue's per-frame mesh |
| unit geometry and unit meshes | `model` | |
| `unitShape`, `ShapeNone`, `ShapeBox`, `shapeSphere`, `shapePlane`, `shapeCount` | `scene` | the shape enum is how scene's queue records |
| `overrideRecord` | `model`, exported | as a slice merge and a single-parameter merge |
| `SkinBuffers` | `model` | |
| `AnimBinding` | each renderer | per-Batch assembly is frame bookkeeping |
| `maxClipPlays` | `model`, exported as `MaxClipPlays` | ecsscene's `MaxPlays` goes |
| `Config`, `WithDefaults` | `model` | the pose sample rate. A renderer that needs configuration declares its own |
| `Lookup` and its access types | `model` | everything it holds is `model`'s. See [Residency](#residency-one-lookup-two-facades) |
| `OpQueue`, `ModelDraw`, `MeshDraw`, `DrawRecord`, `ModelDrawRecord`, `LayerMask`, `RecordOnUpdate`, the Op inspection surface | `scene` | |

**`Transform` stays `m.Transform`.** It already is one: scene's is an alias, for
the reason `transform.go` gives (sound is placed by a transform too). Each
renderer keeps its re-export.

**`model` is one plugin, and the decoder is a package inside it**, at
`bundles/model/internal/types/gltf/`. Geometry generation (`UnitBoxGeometry`,
`UnitPlaneGeometry`, `UnitSphereGeometry`, `appendQuad`, 187 lines) lives in
`model` too, in `internal/types/geometry.go`, forwarded from the root. Both
landed in [#529](https://github.com/dvoyni/cog/issues/529); see [The decoder
seam](#the-decoder-seam).

**`model` is a plugin that composition roots register, before any renderer.** It
registers the `*model.Lookup` resource, and nothing else does, and it takes
`model.Config` under `model.Name`. scene and ecsscene each declare a dependency
on it, and so does any plugin that locks the Lookup itself, because the kernel
requires a dependency on a resource's owner. **Settled here:** the Lookup is a
kernel resource and it is `model`'s, so the plugin that registers it is
`model`'s. The `PoseSampleRate` key moved from `scene.Name` to `model.Name` with
the Lookup ([#530](https://github.com/dvoyni/cog/issues/530)). scene takes no
configuration now, and a setting left under `scene.Name` is ignored silently,
but no composition root in cog-examples or feuds-26 set `scene.Config` (counted
2026-09-21). Every composition root that registers scene registers
`modelplugin.New()` before it.

---

## The decoder seam

From [the decoder seam](https://github.com/dvoyni/cog/issues/496). Built by
[#529](https://github.com/dvoyni/cog/issues/529).

**The seam is thin.** The decoder, `bundles/model/internal/types/gltf`, does no
GPU-layout work. `gltf.Decode` parses one file and returns a `gltf.Model`, which
`model`'s root names `DecodedModel`. It holds:

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

The glTF document is dropped before `Decode` returns. `model.DecodeModel` and
`model.DecodeDocument` forward to it.

**The walk stays in the decoder.** Flattening every scene, interning geometries
and material windings, claiming joints and morph weight slots, and recording the
re-root data are facts about the file, so the decoder computes them. Placement
answers that decide a storage layout arrive as plain booleans: a geometry's
`Skinned` and `SkinnedLayout`, and a primitive's `Skinned`, `Plain` and `Joint`.

**The GPU layout is applied after the decoder returns.** Filling the conversion
vertices, generating flat normals and tangents, remapping JOINTS_0 into the
model's numbering, `packMorphBlock`, `bakeClip` and the `ScenePbrRecord` fill each
run as their own pass over the decoded data. None of them is in the decoder
package. They run in `bundles/model/internal/types` (`gltfload.go`,
`gltfmesh.go`, `gltfmorph.go`, `gltfanim.go`), beside the model cache that
installs their result and the record types they fill: `ScenePbrRecord`,
`scenePose`, `skinJointRecord`, the morph block layout and the pack helpers are
`model`'s. They ran in scene until the cache moved with them in
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
public names, and scene's root and `internal/types` alias `model`'s, so no error
is wrapped twice and no settled name moves.

**The unit geometry and `Vertex` are `model`'s.** `model.UnitBoxGeometry`,
`UnitPlaneGeometry` and `UnitSphereGeometry` (and the unexported `appendQuad`)
live in `bundles/model/internal/types/geometry.go`. They build `[]Vertex`, and a
`Vertex` reports its storage layout through its `VertexLayout` method, so
`Vertex`, the `VertexLayout` interface and the storage layout's offsets, strides
and two attribute tables moved with them, ahead of the rest of mesh residency.
scene's packers still write that layout, and name it through aliases in
`vertexlayout.go`.

---

## The model material

From [what model declares](https://github.com/dvoyni/cog/issues/494) and [how
ecsscene groups Entities into instanced draws](https://github.com/dvoyni/cog/issues/509).

**The cache no longer holds a renderer type.** Until the cache moved, the model
material was `[VariantCount]Material`, scene's pass-tagged `Material`: the one
place the cache reached into renderer vocabulary, and the reason a file material
is re-keyed on every draw record, every frame. [#530](https://github.com/dvoyni/cog/issues/530)
brought the data shape forward so that `model` never named a `PassTag`: the
cache holds `modelMaterial{Forward [VariantCount]gfx.MaterialDescr; Record
ScenePbrRecord}`, and the bundled PBR comes back from `Lookup.EnsureBundled` as
`[VariantCount]gfx.MaterialDescr`. scene wraps each forward descr it draws as
`MaterialTag{TagForward, descr}` in an arena its flush keeps across frames, so a
steady frame wraps without allocating. [#532](https://github.com/dvoyni/cog/issues/532)
added the key: the load takes each forward descr's gfx fingerprint once, as
`modelMaterial.Key`, and scene's `ForwardMaterialKey` turns it into the key
`MaterialKeyOf` would have given the wrapped material, so a draw of a file's
own material is keyed without fingerprinting anything. The same step moved the
bundled shader's WGSL sources and their storage mount into `model`, under the
storage paths they already had.

**The model material holds, for each shader variant:**

- a ready forward `gfx.MaterialDescr`;
- its `ScenePbrRecord`;
- a content key computed once at load.

It names no `PassTag`. A renderer wraps the forward descr in its own material
under its own tag. A file material's renderer key derives from the model key, so
nothing fingerprints the material per draw. When shadows land, `model` adds a
shadow descr for each variant beside the forward one.

---

## The shader's records and their packers

From [where scene's per-frame drawing code goes](https://github.com/dvoyni/cog/issues/521).
Landed in [#533](https://github.com/dvoyni/cog/issues/533): the records and
packers are in `internal/types/records.go`, `light.go` and `lightselection.go`,
aliased in the root's `types.go` and forwarded from its `utils.go`.

**`model` exports every record the shader reads, with its packer.** Both
renderers write the same bytes through the same code, so a shader change cannot
compile against one renderer and draw garbage in the other.

- **The records:** `FrameBlock`, `Instance` with its flags (`SceneNonUniform`,
  `SceneNoSkin`, `ScenePlainJoint`) and `SceneNoAnim`, `Light`, and
  `SceneAnimHeader`, `ScenePlayRecord` and `SceneMorphWeight`, beside
  `ScenePbrRecord`, `SceneMesh` and `IdentityMesh`. They are aliased in
  `model`'s root, and their packers are forwarders into `model/internal/types`.
  The flags keep the WGSL constants' names; the three records the renderer
  used to declare take the names this section gave them.
- **The binding names are constants**, `BindingScene` followed by the WGSL
  name: `BindingSceneFrame` (`sceneFrame`), `BindingSceneInstances`,
  `BindingSceneAnim`, `BindingSceneMeshes`, `BindingScenePbrMaterial`,
  `BindingScenePoses`, `BindingSceneSkinJoints` and `BindingSceneMorphDeltas`.
- **Each record has a `Size` constant**, `<Record>Size`, replacing the
  `unsafe.Sizeof` block in scene's `draw.go`, because binding ranges need them.
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

`model.FrameLighting` holds the camera's six lighting fields under scene's names:
`SunDirection`, `SunColor`, `SunIntensity`, `AmbientSky`, `AmbientGround` and
`AmbientIntensity`. Each renderer copies them from its camera at the call site,
so neither camera changes shape. The defaults (normalising the sun, an intensity
of zero meaning 1) are resolved once, in `model`.

**Light selection is split.** `model` exports the part that fills the shader's
array: the contribution score, `ContributionAt`, and `LightSelection`, the array
capped at `MaxLights`, with `Reset`, `Offer`, `Count` and `Lights`. Each renderer
keeps `prepareLights`, the layer test and the frustum test, and offers `model`
only the lights that pass.

**Paint is `model`'s too.** A draw with no material of its own binds
`PaintPbrRecord(color, selfLit)`: glTF's defaults with metallic 0. scene's debug
shapes and bare meshes used to set the record's fields themselves; a second
renderer drawing a mesh with no material needs the same record.

**Settled, verified while handing over:** the shader reads `animOffset` from each
instance's own record (`instance.wgsl`, read by `skin.wgsl` and `morph.wgsl`),
never as a value fixed for the draw. That is the condition ecsscene's
[Batches](../../../ecsscene/docs/specs/ecsscene.md#batches) rest on for skinned
and morphed Entities.

---

## Residency: one Lookup, two facades

From [the cache read path](https://github.com/dvoyni/cog/issues/497).

**`model` owns the caches.** `modeltable.go`, `texturetable.go` and
`modelunload.go` moved into it in [#530](https://github.com/dvoyni/cog/issues/530).
There is one `*model.Lookup`, holding the model cache, the texture cache, the
mesh table every `MeshRef` indexes, the staging arena, the unit meshes, and the
deferred bake and release queues. Only scene's per-frame temporary mesh stays
behind: `MeshSource` is reduced to `MeshNone` and `MeshDurable`, scene declares
`MeshTemporary` past them and `TemporaryMeshID` beside it, and mints its
temporaries through `model.MintMesh` into a `model.LayoutCache` and an arena of
its own, building their refs with `model.NewMeshRef`.

**A renderer holding the Lookup for writing calls its own methods**, which
replaced scene's friend accessors: `ModelView` resolves one draw's selectors,
loading the model if needed; `Mesh` resolves a durable ref; `EnsureUnit` bakes a
`model.UnitMesh`, which scene maps its shape enum onto; `EnsureBundled` returns
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
queues.** Today scene's flush holds `Write` on the Lookup and runs the staged
bakes and deferred releases against gfx's resource queue. After the split each
renderer does exactly that, at the point scene does it today: scene in its flush,
and ecsscene in its load System. This follows from the facades and from the two
renderers never running together.

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
  which removed scene's alias of `m.Transform`.

---

## scene after the split

**scene keeps its name, its `OpQueue` API and its behaviour.** It loses the code
that moves to `model` and imports `model` instead. It keeps:

- the queue: `OpQueue`, `ModelDraw`, `MeshDraw` and the recorded records;
- its `Material`, `MaterialTag`, `PassTag` and material table, with a file
  material's key derived from the model material's;
- its camera, pass and layer vocabulary, over `libs/m`'s projection maths;
- `LightRecord`, `prepareLights`, the layer and frustum tests;
- culling, sorting, the arena and emission;
- the per-frame temporary mesh and the shape enum;
- the Op inspection surface (`Ops`, `Passes`, `PassView`, `BatchView`).

**What scene pays: nothing new.** Its flush already holds write access, and it
keeps loading on first draw through the load facade. Caching a handle for each
`ModelRef` to skip the path clean is an optional gain, not a requirement.

**During the carve, scene's root re-exports `model`'s types as temporary
aliases**, such as `type ModelRef = model.ModelRef`, so cog-examples and ecsscene
compile unchanged. The Destination's "keeping its OpQueue API" is about the ops,
not about these re-exports. The sweep deletes them.

---

## ecsscene after the split

From [#495](https://github.com/dvoyni/cog/issues/495), [#497](https://github.com/dvoyni/cog/issues/497),
[#501](https://github.com/dvoyni/cog/issues/501), [#509](https://github.com/dvoyni/cog/issues/509)
and [#521](https://github.com/dvoyni/cog/issues/521). Landed in
[#535](https://github.com/dvoyni/cog/issues/535), [#536](https://github.com/dvoyni/cog/issues/536)
and [#537](https://github.com/dvoyni/cog/issues/537), and on main in one merge
with [#538](https://github.com/dvoyni/cog/issues/538).

**ecsscene repeats scene's path, recording to `gfx` itself, and imports nothing
of scene.** It is not redesigned to do anything scene does not, except draw
Batches. Its design record is now its own:
[`ecsscene.md`](../../../ecsscene/docs/specs/ecsscene.md). In summary:

- **Its Components wrap `model`'s values**, `model.ModelRef`, `model.MeshRef`,
  `model.ClipPlay` and `model.LightDescr`, and it does not register `model`'s
  types.
- **It declares its own copy of scene's camera, layer and pass vocabulary**,
  with scene's names, shapes and zero values, and its own copy of the frame
  code that does not depend on the shader. What does depend on the shader is
  `model`'s.
- **Its load System** is the only ecsscene System holding
  `kernel.Write[*model.Lookup]`. It keys each changed Entity into a Batch, and
  drives `model`'s bake and release queues.
- **Its recording System** holds the Lookup only for reading, through the read
  facade, buckets Entities into [Batches](../../../ecsscene/docs/specs/ecsscene.md#batches)
  by those keys, and draws one instanced draw per opaque Batch into
  `*gfx.OpQueue`, through `model`'s packers and binding names.
- **Its oracle is a recording `gfx.Backend`**, and it publishes no inspection
  API ([What it is tested against](../../../ecsscene/docs/specs/ecsscene.md#what-it-is-tested-against)).

---

## The fountains

From [which examples the redesigned ecsscene ships with](https://github.com/dvoyni/cog/issues/510).
These live in cog-examples.

- **`cmd/ecs/fountain` is rewritten in place**, with the same frame and the same
  Components doing the same jobs, on `model`'s refs and ecsscene's own types.
  It composes ecsscene and not scene, and cog-examples' headless harness starts
  it with `headless.NewECS`, which composes ecs and ecsscene in scene's place.
  Its `reference.png` was recaptured once, on the GPU, after the redesign
  ([#538](https://github.com/dvoyni/cog/issues/538)).
- **`cmd/scene/fountain` is new: the same frame through `scene.OpQueue`**, with
  the same seed and `referenceStep`. Its motes are a plain slice, one scene call
  each with its own tint.
- **`internal/fountain` holds what they share:** the spray simulation, the clock,
  the xorshift stream, the layout constants, the HUD text, `spray_test.go`, and
  the expected figures for `referenceStep`. Each `cmd` keeps only its recording.

**Both HUDs read `ArmFrameCmd`.** Each arms one unfiltered snapshot per step and
sums the passes labelled with its camera's passes (`ground` and `forward`), which
leaves out canvas's HUD pass. Passes are passes, batches are draws, and drawn is
instances. The line reads `passes N  drawn N  batches N`. **The culled figure is
dropped from both**: fountain culls nothing, and exposing it from ecsscene would
be new API.

**What each fountain's tests assert:**

- `TestTheHUDsArithmetic`: the census equals the tally, and there are two passes.
- `TestTheReferenceStepShowsEveryComponent`, through `headless.Backend` and the
  `ArmFrameCmd` snapshot: two passes labelled `ground` and `forward`, each with
  its clear; the fox and the nozzle drawn in `forward`; the basin drawn in both.
- `TestTheHUDReadsAsInReferencePNG` stays whole, batches figure included.
- **Against `internal/fountain`'s expected figures:** the passes, their whole
  labels and the instances in each pass must be equal. Draws may differ, and
  ecsscene's may be lower than scene's, never higher: two motes spawned on one
  step fade to the same tint, and ecsscene batches them while scene never
  merges calls.

**Both comparisons assert, since [#538](https://github.com/dvoyni/cog/issues/538).**
`internal/fountain` holds `ReferencePasses`, each pass's whole label
(`scene.camera-100.ground`, `scene.camera-100.forward`) and its instances, and
`ReferenceSceneDraws`, the scene fountain's draws in each pass. The scene
fountain must equal both. The ecs fountain must equal `ReferencePasses` and stay
at or under `ReferenceSceneDraws` pass by pass. At step 600 the forward pass is
117 instances in both, drawn in 117 draws by scene and 116 by ecsscene. The
labels can be compared whole because ecsscene keeps scene's spelling.

`headless.Engine.Passes` and `Ops` stay for scene's examples, and fail a test
on an engine from `NewECS`. The fountains do not call them. `Lookup` and
`LookupDevice` answer through `model`'s facades under either renderer.

---

## What is not foreclosed

- **A second model format.** The thin seam means any decoder hands over the same
  plain data, so a collected Port for decoders can be declared when a second
  format arrives.
- **A decoded glTF camera**, as a `ModelCamera` data record beside `ModelLight`.
- **Per-instance properties** ([#520](https://github.com/dvoyni/cog/issues/520)),
  which would make a tinted crowd one draw in both renderers.
- **scene merging separate calls** ([#49](https://github.com/dvoyni/cog/issues/49)).
  The model material's load-time key already removes the per-draw fingerprint
  that merge would otherwise pay.
- **scene caching `ModelHandle`s** to skip the path clean.
- **Shadow descrs** in the model material, one for each variant.
- **Merging the four test backends** (scene's, ecsscene's, gfx's `fakeBackend`,
  cog-examples' `headless.Backend`).

---

## Shapes that were rejected

Each was ruled out by the ticket named, and most with the sequence that breaks it.

**Where the decoder lives** (charting):

- **`libs/gltf`.** The packing layer builds gfx resources (`gfx.BufferDescr` ×16,
  `gfx.VertexAttr` ×10, `gfx.MeshDescr` and more), and a Library may not import
  `slots/gfx`.
- **`bundles/scene/internal/types/gltf/`.** Right shape, wrong place: two plugins
  cannot share an `internal/` package, so ecsscene could never import it.
- **`libs/geometry`.** The generators are gfx-free, but 187 lines do not justify
  a Library's `doc.go`, `id.go`, `internal/` and constructor package.

**The vocabulary** ([#494](https://github.com/dvoyni/cog/issues/494)):

- **A shared renderer bundle** for what both renderers need. It would bring back
  the coupling the split removes.
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
- **ecsscene registering `model`'s types directly.** It claims the one Store each
  Go type can have, so a second ECS plugin gets `ErrDuplicateRegistration`. It
  is recorded in [`ecsscene.md`](../../../ecsscene/docs/specs/ecsscene.md#shapes-that-were-rejected)
  as well.

**Residency** ([#497](https://github.com/dvoyni/cog/issues/497)):

- **A Component caching the resolved `ModelView`.** The app unloads the model,
  the next flush releases its buffers, and the Component still holds slices into
  the freed entry, so the next draw binds released buffers.
- **A read lock on today's loading `Lookup`.** Readers A and B both miss
  `fox.glb` and both load it, so it uploads twice. Making both writers
  serialises ecsscene's Systems, which is a blocker.
- **A per-frame snapshot.** It is still taken under one of those two locks, and
  it copies every resident model every frame.
- **The app preloads everything, and a miss draws nothing.** An Entity naming a
  model that was never preloaded draws nothing, and nothing reports why.

**The shader's records** ([#521](https://github.com/dvoyni/cog/issues/521)):

- **ecsscene copies the records.** A shader change that one copy misses still
  compiles, and that renderer draws garbage.
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
- **Reshaping ecsscene's copied vocabulary**, such as nesting sun and ambient or
  renaming a type. That is improving, not splitting.

**Batches and the test oracle** ([#509](https://github.com/dvoyni/cog/issues/509),
[#501](https://github.com/dvoyni/cog/issues/501)) moved with ecsscene's design
to [`ecsscene.md` §Shapes that were
rejected](../../../ecsscene/docs/specs/ecsscene.md#shapes-that-were-rejected).

**The examples** ([#510](https://github.com/dvoyni/cog/issues/510)):

- **A 5 000-crate example.** The benches already measure it.
- **ecsscene versions of scene's other examples.**
- **Two full copies of the fountain.** A difference in simulation would pass for
  a difference between renderers.
- **A stats resource published by ecsscene.** It is the inspection surface #501
  rejected.

**The order** ([#522](https://github.com/dvoyni/cog/issues/522)):

- **`model` starts as a new bundle beside scene**, with both renderers moving onto
  it later. For a while there would be two caches and two copies of 14.7k lines.
- **The ecsscene redesign in several landings.** Two recording paths side by side
  would need a switch.
- **ecsscene's tests move between the carve and the redesign.** That leaves the
  carve unguarded for ecsscene.
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
2. **ecsscene's tests move onto the recording backend, and the fountains land.**
   All of it runs on today's ecsscene, so a carve that breaks ecsscene's drawing
   shows up at the frame level.
   - ecsscene's `testBackend` and `RenderEvent` in the harness. The nine op-field
     tests and three `PassView` tests are rewritten as backend assertions, and
     the `scene.Op` reads are deleted.
   - The five benches gain their drawing bottom half.
   - `internal/fountain`, `cmd/scene/fountain`, and `cmd/ecs/fountain` rewritten
     onto it, with the HUD on `ArmFrameCmd`. The comparison of the two against
     the expected figures does not assert yet.
3. **The carve: scene moves onto `model`.** Code moves out of scene into
   `bundles/model`, and scene imports it. Each step repoints scene's root aliases
   at `model`'s root. ecsscene keeps proxying into `scene.OpQueue` throughout.
   `friends.go`'s test accessors move with the code they reach. In this order:
   1. the decoder and geometry. **This spec's first flip lands here**. Landed in
      [#529](https://github.com/dvoyni/cog/issues/529), with `Vertex` and its
      storage layout moved early and the conversion to GPU layouts left in
      scene until step 2;
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
4. **The ecsscene redesign, in one landing.** It moves from proxying to recording
   to gfx, with its own copies of the arena, culling, sorting and vocabulary, the
   load System, and Batches. ecsscene stops importing scene. The fountain
   comparison starts asserting. ecsscene's section of this spec moves into its own
   docs, and `ecs.md` §Binding is brought up to date. Built on the integration
   branch `model/ecsscene-redesign` as [#535](https://github.com/dvoyni/cog/issues/535)
   (the Components and the vocabulary), [#536](https://github.com/dvoyni/cog/issues/536)
   (the load System) and [#537](https://github.com/dvoyni/cog/issues/537) (the
   recording System), and landed on main in one merge with
   [#538](https://github.com/dvoyni/cog/issues/538), which moved the fountain
   onto ecsscene alone and made its comparison assert. ecsscene's design is
   now [`ecsscene.md`](../../../ecsscene/docs/specs/ecsscene.md).
5. **The sweep.** Every caller in cog and cog-examples names `model.X`, scene's
   temporary aliases are deleted, and `scene.md` is cut down to the renderer.

---

## Out of scope

- **A second asset format** (FBX, OBJ, USD), and a collected Port for decoders
  that would only serve one.
- **An offline asset pipeline**, or any consumer outside the cog module.
- **Per-instance properties** ([#520](https://github.com/dvoyni/cog/issues/520)).
- **scene merging separate calls** ([#49](https://github.com/dvoyni/cog/issues/49)).
- **Running scene and ecsscene together**, and detecting a stale `ModelHandle`.
  Both are undefined behaviour by the standing rules.
- **Merging the four test backends.**
