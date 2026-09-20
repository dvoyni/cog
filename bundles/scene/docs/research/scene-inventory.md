# The scene inventory: every declaration, and the side it lands on

Research output for [the inventory: every declaration in scene, and the side it lands
on](https://github.com/dvoyni/cog/issues/492), under the map [model: one plugin beneath
two renderers, and what a renderer still owns](https://github.com/dvoyni/cog/issues/450).

**There is nothing decided here.** A `side` column is a starting point for the
conversation on [what model declares, and what a renderer
keeps](https://github.com/dvoyni/cog/issues/494), and anything genuinely two-sided is
marked `unclear` with what makes it ambiguous, rather than guessed.

Measured at `2e481e1` on `main`, Go 1.25, 2026-09-20.

## Sides used in the tables

| side | meaning |
| --- | --- |
| `model/decode` | the glTF decoder package, `bundles/model/internal/types/gltf/` |
| `model/pack` | packing and the GPU record layouts the decoder writes into |
| `model/cache` | residency: load, upload, evict |
| `model/geom` | the unit geometry generators |
| `model` | a plain model declaration, none of the above |
| `scene` | the declarative renderer keeps it |
| `both` | both renderers need it, so it cannot sit in either |
| `m` | already in, or belongs in, `libs/m` |
| `follows` | a test-only accessor; it follows whatever it reaches into |
| `n/a` | a generic helper with no side (`abs`, `grow`, `sequence`) |
| `unclear` | genuinely two things, or one thing whose side is not decidable yet |

## How it was produced

Every table below is machine-generated and reproducible, not read off by eye:

1. `go/ast` over `bundles/scene`, `bundles/scene/internal` and
   `bundles/scene/internal/types`, excluding `_test.go`, giving every top-level
   declaration with its kind, receiver and line.
2. A cross-reference pass: for each declared name, a word-boundary count of its
   occurrences in every non-test file of `internal/types`, `internal`, the root, and
   `bundles/ecsscene`. That is what the `used by` columns hold; a declaration's own file
   is discounted by one for its declaration.
3. `ecs.Storable` run for real, from a throwaway test inside the module, over the whole
   root vocabulary plus every `ecsscene` Component.
4. Side and role assigned per file, with per-name overrides where a file is not uniform.

The counts: **405 top-level declarations** and **191 methods on 60 receivers** in
`internal/types`; **88 declarations** in the root, of which **49 are `type X = types.Y`
aliases**, matching the ticket; **1,914 lines** in `ecsscene`.

## What the sweep found

**The split is lopsided.** Of the 405 declarations in `internal/types`, **269 land on the
model side** (101 packing, 78 decode, 47 plain model, 36 cache, 7 geometry) and **66 on
the renderer**. 32 more are `friends.go` test accessors that follow whatever they reach
into, 21 are `unclear`, 7 are helpers with no side, 7 need both renderers, 3 are `libs/m`.
`bundles/scene` is a model library with a renderer on top, by count.

**`scene.Transform` is already `m.Transform`.** `transform.go:12` is a type alias, and its
comment says why: *"it is m.Transform, which lives in m because scene is not its only
consumer: sound is heard from one too, and neither package may depend on the other."* The
map's fog entry asking whether `Transform` belongs in `libs/m` is answered by the code —
it is there. What `scene` owns is a re-export, and `ecsscene` already redefines it
(`type Transform scene.Transform`) to keep the Store's Go type its own.

**`Material` and `MaterialTag` are renderer types, not model types.** This contradicts the
expectation on the decision ticket, which lists `Material` as "a model fact, but rejected
as a Component". The declaration says otherwise: `MaterialTag` binds a **`PassTag`** to a
`gfx.MaterialDescr`, and `Material` is a slice of those. A pass is the renderer's by the
settled line, so the *type* goes with it. The model's own material is not called
`Material` at all — it is `loadedMaterial` (`gltfload.go:201`), `modelMaterial`
(`modeltable.go:136`) and `ScenePbrRecord` (`pack.go:31`). The split does not cut
`Material` in half; it hands the name to the renderer and leaves the model's material
under three other names.

**The cache already calls the decoder.** `modeltable.go` imports `github.com/qmuntal/gltf`
directly and names `convertDocument`, `LoadedModel`, `loadedScene`, `loadedMaterial` and
`gltfGeometry`. `modeltable.go` and the six `gltf*.go` files are the only non-test files
in the package that import the glTF library. Putting the cache and the decoder in one
plugin is not a convenience — they are one call apart today.

**The decode/runtime cross-cut is 68 names, not 12 — and almost all of it stays inside
`model`.** Two of the twelve the ticket listed were attributed to a call site rather than a
declaration: `selectUVSet` is a method on `*ScenePbrRecord` at `pack.go:82`, and
`recordWords` is a method on `morphMask` at `morph.go:36`; `scenePose`, `sceneSkinJoint`
and `skinJointRecord` are in `anim.go`, not `gltfanim.go`. Fifty-eight of the 68 survive dropping the generic helpers. More usefully: of the 68, the
runtime files that read them are `anim`, `animpack`, `morph`, `morphdelta`, `vertexpack`,
`vertexskin`, `modeltable`, `texturetable`, `material`, `pack`, `modelerr` — **every one of
them model-side**. Only five escape to the renderer: `ScenePbrRecord` (with
`defaultPbrRecord`, `emissiveFactor` and `Member`), `Material`, `LightDescr`, `ModelLight`
and `Config`. The decoder seam ticket is therefore about one record, not about sixty
names.

**`ecsscene` depends on 24 scene names at runtime and 13 more only in its tests.** The
map's figure of 29 was an overcount from a regex that also matched `ecsscene.X`. The
split matters: the 13 test-only names are the **Op inspection surface** — `scene.Op`,
`scene.OpKind`, `scene.OpModel`, `scene.OpMesh`, `scene.OpCamera`, `scene.OpPointLight`,
`scene.OpSpotLight`, `scene.PassView`, `scene.Lookup`, `scene.NewLookupAccess`,
`scene.Layer`, `scene.Perspective`, `scene.Oblique`, `scene.Vertex`. **When `ecsscene`
stops proxying into `scene.OpQueue`, its entire test oracle goes with it.** Every
behavioural test in `bundles/ecsscene/internal` asserts by reading back the Ops scene
recorded. That is a cost the redesign has to pay and nothing on the map has named it yet.

**`Lookup` is the largest single unresolved object.** One resource holds the model cache,
the texture cache, the mesh table every `MeshRef` indexes, the staging arena, the unit
meshes and the deferred bake and release queues — reached through `LookupAccess` (7 methods) and `LookupDeviceAccess` (16 methods), with 19 of `friends.go`'s 32 test
accessors reaching into it. It is `unclear` in the tables below, and it is not one name:
it is 7 declarations, 39 methods and the 19 accessors, and splitting it is most of the
mechanical work in the whole map.

**`VertexLayout` is rejected as a Component for being an interface, not a slice.** Every
other rejection in the vocabulary is a bare slice, whose remedy the ECS spec already names
(`ecs.List`). An interface has no such remedy, so if a renderer wants a Component naming a
vertex layout it needs a concrete descriptor or an id instead. See Part E.

---

## Part A — `bundles/scene`, the root

88 declarations (9 methods on the root-local error types omitted). The root is 49 aliases,
10 locally-declared error and subscription types, 19 re-exported constants and 10
functions. A side here is inherited from what the alias resolves to.

| name | kind | root file | resolves to | side |
| --- | --- | --- | --- | --- |
| `Config` | alias | `config.go` | `types.Config` | model |
| `ErrCameraAlreadyRecorded` | type | `err.go` | declared here | scene |
| `ErrCameraClipPlanesMissing` | type | `err.go` | declared here | scene |
| `ErrPassTargetUnsized` | type | `err.go` | declared here | scene |
| `ErrColourlessPassWithoutDepth` | type | `err.go` | declared here | scene |
| `ErrColourlessPassClearsColour` | type | `err.go` | declared here | scene |
| `ErrCameraProjectionDegenerate` | alias | `err.go` | `types.ErrCameraProjectionDegenerate` | scene |
| `ErrMaterialTagAlreadyServed` | type | `err.go` | declared here | scene |
| `ErrTextureUVSetUnsupported` | alias | `err.go` | `types.ErrTextureUVSetUnsupported` | scene |
| `ErrSpotConeInverted` | type | `err.go` | declared here | scene |
| `ErrSpotDirectionMissing` | type | `err.go` | declared here | scene |
| `ErrMeshGeometryInvalid` | alias | `err.go` | `types.ErrMeshGeometryInvalid` | scene |
| `ErrMeshUnavailable` | alias | `err.go` | `types.ErrMeshUnavailable` | scene |
| `ErrMeshCustomLayoutNeedsMaterial` | type | `err.go` | declared here | both |
| `ErrMeshUpdateRejected` | alias | `err.go` | `types.ErrMeshUpdateRejected` | scene |
| `ErrModelUnavailable` | alias | `err.go` | `types.ErrModelUnavailable` | model |
| `ErrModelTextureUnavailable` | alias | `err.go` | `types.ErrModelTextureUnavailable` | model |
| `ErrModelPrimitiveSkipped` | alias | `err.go` | `types.ErrModelPrimitiveSkipped` | model |
| `ErrModelBoundsMissing` | alias | `err.go` | `types.ErrModelBoundsMissing` | model |
| `ErrModelPathInvalid` | alias | `err.go` | `types.ErrModelPathInvalid` | model |
| `ErrModelNodeDuplicated` | alias | `err.go` | `types.ErrModelNodeDuplicated` | model |
| `ErrModelSceneMissing` | alias | `err.go` | `types.ErrModelSceneMissing` | model |
| `ErrModelNodeMissing` | alias | `err.go` | `types.ErrModelNodeMissing` | model |
| `ErrModelNodeDegenerate` | alias | `err.go` | `types.ErrModelNodeDegenerate` | model |
| `ErrModelSkinUnbound` | alias | `err.go` | `types.ErrModelSkinUnbound` | model |
| `ErrModelPoseApproximated` | alias | `err.go` | `types.ErrModelPoseApproximated` | model |
| `ErrModelClipMissing` | alias | `err.go` | `types.ErrModelClipMissing` | model |
| `ErrModelPlaysOverLimit` | alias | `err.go` | `types.ErrModelPlaysOverLimit` | model |
| `ErrModelMorphWeightsOverLength` | alias | `err.go` | `types.ErrModelMorphWeightsOverLength` | model |
| `ErrModelMorphTargetsOverLimit` | alias | `err.go` | `types.ErrModelMorphTargetsOverLimit` | model |
| `Name` | const | `id.go` | declared here | scene |
| `FlushOnUpdate` | type | `id.go` | declared here | scene |
| `OpQueue` | alias | `resources.go` | `types.OpQueue` | scene |
| `Lookup` | alias | `resources.go` | `types.Lookup` | unclear |
| `Transform` | alias | `types.go` | `types.Transform` | m |
| `CameraID` | alias | `types.go` | `types.CameraID` | scene |
| `ProjectionKind` | alias | `types.go` | `types.ProjectionKind` | model |
| `Perspective` | const | `types.go` | declared here | model |
| `Orthographic` | const | `types.go` | declared here | model |
| `Oblique` | const | `types.go` | declared here | scene |
| `PassTag` | alias | `types.go` | `types.PassTag` | scene |
| `TagForward` | const | `types.go` | declared here | scene |
| `Pass` | alias | `types.go` | `types.Pass` | scene |
| `CameraDescr` | alias | `types.go` | `types.CameraDescr` | unclear |
| `LayerMask` | alias | `types.go` | `types.LayerMask` | scene |
| `LayersAll` | const | `types.go` | declared here | scene |
| `LightKind` | alias | `types.go` | `types.LightKind` | model |
| `LightPoint` | const | `types.go` | declared here | model |
| `LightSpot` | const | `types.go` | declared here | model |
| `LightDescr` | alias | `types.go` | `types.LightDescr` | model |
| `Material` | alias | `types.go` | `types.Material` | scene |
| `MaterialTag` | alias | `types.go` | `types.MaterialTag` | scene |
| `Vertex` | alias | `types.go` | `types.Vertex` | model |
| `VertexLayout` | alias | `types.go` | `types.VertexLayout` | both |
| `MeshRef` | alias | `types.go` | `types.MeshRef` | both |
| `MeshDraw` | alias | `types.go` | `types.MeshDraw` | scene |
| `VertexDecodePath` | const | `types.go` | declared here | model |
| `ModelDraw` | alias | `types.go` | `types.ModelDraw` | scene |
| `ModelLight` | alias | `types.go` | `types.ModelLight` | model |
| `ModelRef` | alias | `types.go` | `types.ModelRef` | model |
| `ClipPlay` | alias | `types.go` | `types.ClipPlay` | model |
| `ClipInfo` | alias | `types.go` | `types.ClipInfo` | model |
| `OpKind` | alias | `types.go` | `types.OpKind` | scene |
| `OpCamera` | const | `types.go` | declared here | scene |
| `OpBox` | const | `types.go` | declared here | scene |
| `OpSphere` | const | `types.go` | declared here | scene |
| `OpPlane` | const | `types.go` | declared here | scene |
| `OpLine3D` | const | `types.go` | declared here | scene |
| `OpWireBox` | const | `types.go` | declared here | scene |
| `OpMesh` | const | `types.go` | declared here | scene |
| `OpPointLight` | const | `types.go` | declared here | scene |
| `OpSpotLight` | const | `types.go` | declared here | scene |
| `OpModel` | const | `types.go` | declared here | scene |
| `Op` | alias | `types.go` | `types.Op` | scene |
| `PassView` | alias | `types.go` | `types.PassView` | scene |
| `BatchView` | alias | `types.go` | `types.BatchView` | scene |
| `LookupAccess` | alias | `types.go` | `types.LookupAccess` | unclear |
| `LookupDeviceAccess` | alias | `types.go` | `types.LookupDeviceAccess` | unclear |
| `ViewProjection` | func | `utils.go` | declared here | scene |
| `WorldToScreen` | func | `utils.go` | declared here | scene |
| `ScreenToWorld` | func | `utils.go` | declared here | scene |
| `ScreenToRay` | func | `utils.go` | declared here | scene |
| `Layer` | func | `utils.go` | declared here | scene |
| `At` | func | `utils.go` | declared here | m |
| `LookAt` | func | `utils.go` | declared here | m |
| `NewLookup` | func | `utils.go` | declared here | unclear |
| `NewLookupAccess` | func | `utils.go` | declared here | unclear |
| `NewLookupDeviceAccess` | func | `utils.go` | declared here | unclear |

---

## Part B — `bundles/scene/internal/types`, file by file

405 top-level declarations in 42 non-test files, in declaration order. Each file carries a
role and a default side; a row whose side differs from its file's default has a note
saying why.

The `used by` columns are occurrence counts, not call counts: `opqueue:3` means the name
appears three times in `opqueue.go`. A declaration's own file is discounted by one. `--`
means no non-test file outside the declaration's own names it, which for an unexported
helper is normal and for an exported declaration is worth a look (`AnimHeaderVec4s`,
`PlayRecordVec4s`, `morphWeightSize`, `variantSkinMorph` and `tangentReservedBit` are
named by nothing but tests).

#### `anim.go` — pose and skin GPU records, clip vocabulary · default **model/pack**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `ClipPlay` | type | 19 | anim:1 animpack:1 model:2 modelerr:1 opqueue:3 | -- · err:1 types:3 · types:1 systems:3 | model | names a clip by string; the play is the app's animation control |
| `ClipInfo` | type | 45 | anim:1 gltfanim:1 modeltable:3 | -- · types:3 · -- | model |  |
| `maxClipPlays` | const | 53 | anim:1 animpack:3 | -- | model/pack |  |
| `scenePose` | type | 72 | anim:5 animpack:1 gltfanim:2 | -- | model/pack |  |
| `sceneSkinJoint` | type | 95 | anim:4 gltfanim:2 | -- | model/pack |  |
| `ScenePlayRecord` | type | 119 | anim:2 animpack:5 morph:1 | animpack:1 plugin:1 · -- · -- | model/pack |  |
| `SceneAnimHeader` | type | 134 | anim:2 | animpack:1 · -- · -- | model/pack |  |
| `PoseSize` | var | 155 | modeltable:1 | -- | model/pack |  |
| `SkinJointSize` | var | 156 | modeltable:1 | -- | model/pack |  |
| `AnimHeaderVec4s` | var | 157 | -- | -- | model/pack |  |
| `PlayRecordVec4s` | var | 158 | -- | -- | model/pack |  |
| `skinJointRecord` | func | 170 | anim:1 gltfanim:1 | -- | model/pack |  |
| `poseFromMatrix` | func | 198 | anim:1 gltfanim:1 | -- | model/pack |  |
| `decomposeCollapsed` | func | 223 | anim:2 | -- | model/pack |  |
| `faithful` | func | 280 | anim:3 | -- | model/pack |  |
| `poseResidualTolerance` | const | 293 | anim:3 | -- | model/pack |  |
| `matrixFromPose` | func | 298 | anim:1 animpack:1 | -- | model/pack |  |

#### `animpack.go` — clip resolution and pose blending into records · default **model/pack**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `ResidentAnimation` | type | 17 | animpack:6 modelselect:1 modeltable:4 morph:1 | model:1 · -- · -- | model/pack |  |
| `ResolvePlays` | func | 93 | animpack:1 | model:1 · -- · -- | model/pack |  |
| `WeightFrames` | type | 155 | animpack:5 morph:1 | plugin:1 · -- · -- | model/pack |  |
| `resolvedPlay` | type | 158 | animpack:4 | -- | model/pack |  |
| `zeroWeightTolerance` | const | 211 | animpack:2 | -- | model/pack |  |
| `abs` | func | 213 | animpack:5 morph:5 morphdelta:3 | -- | n/a | numeric helper |
| `ReportOnce` | type | 223 | animpack:2 morph:2 | model:1 · -- · -- | model/pack |  |
| `clipReportKey` | func | 229 | animpack:1 | -- | model/pack |  |
| `playsReportKey` | func | 230 | animpack:1 | -- | model/pack |  |
| `BlendJoint` | func | 239 | animpack:1 | model:1 · -- · -- | model/pack |  |

#### `camera.go` — camera descriptor, pass declaration, projection kind · default **split**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `CameraID` | type | 20 | camera:1 err:1 friends:1 inspect:3 opqueue:4 projection:1 | plugin:4 projection:2 · err:7 types:3 · types:1 | scene | a dense id the queue registers a camera under; glTF has no such id |
| `ProjectionKind` | type | 29 | camera:3 | -- · types:3 · types:1 | model | glTF cameras declare perspective or orthographic; Oblique is scene's addition |
| `Perspective` | const | 32 | camera:3 projection:2 | pack:2 · types:3 · -- | model |  |
| `Orthographic` | const | 33 | camera:8 projection:3 | -- · types:5 · -- | model |  |
| `Oblique` | const | 38 | camera:7 projection:2 | -- · types:4 · -- | scene | not a glTF projection: scene added it |
| `PassTag` | type | 44 | camera:5 friends:2 inspect:1 material:2 | material:5 plugin:2 · err:4 types:4 · types:1 | scene | a pass is the renderer's, by the settled line |
| `TagForward` | const | 48 | camera:4 material:3 modeltable:1 | material:1 · types:3 · types:1 | scene |  |
| `Pass` | type | 72 | camera:5 doc:1 friends:2 material:1 opqueue:3 | draw:1 plugin:3 projection:2 · doc:1 types:3 · types:1 systems:2 | scene |  |
| `CameraDescr` | type | 84 | camera:2 coords:6 inspect:1 light:1 opqueue:2 projection:2 | pack:1 · doc:1 types:3 utils:4 · types:1 systems:1 | unclear | half model (projection, near/far), half renderer (Passes, CullMask, sun and ambient) |
| `DepthClearFar` | const | 132 | camera:2 | -- | scene |  |
| `DefaultPass` | func | 142 | camera:1 | plugin:1 · -- · -- | scene |  |

#### `config.go` — plugin configuration (bake rate) · default **split**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `Config` | type | 5 | config:4 doc:1 gltfload:1 light:1 lookup:3 opqueue:1 | config:5 doc:1 plugin:1 · config:4 doc:1 · plugin:1 | model | its one field is the animation bake rate, which is a load-time model fact |
| `WithDefaults` | func | 14 | config:1 lookup:1 | config:2 · -- · -- | model |  |

#### `coords.go` — view/projection and screen maths · default **scene**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `ViewProjection` | func | 10 | coords:3 | pack:1 plugin:1 · utils:4 · -- | scene |  |
| `WorldToScreen` | func | 19 | coords:3 | -- · utils:6 · -- | scene |  |
| `ScreenToWorld` | func | 36 | coords:4 | -- · utils:5 · -- | scene |  |
| `ScreenToRay` | func | 45 | coords:4 | -- · utils:3 · -- | scene |  |
| `screenToNDC` | func | 57 | coords:4 | -- | scene |  |
| `viewProjection` | func | 69 | coords:4 | cull:2 plugin:3 · -- · -- | scene |  |
| `inverseViewProjection` | func | 90 | coords:3 | -- | scene |  |

#### `err.go` — renderer-side errors · default **scene**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `ErrCameraProjectionDegenerate` | type | 8 | err:2 projection:3 | plugin:1 · err:3 · -- | scene |  |
| `ErrTextureUVSetUnsupported` | type | 21 | err:2 pack:1 | -- · err:3 · -- | scene |  |
| `ErrMeshGeometryInvalid` | type | 38 | err:2 mesh:3 | -- · err:3 · -- | scene |  |
| `ErrMeshUnavailable` | type | 49 | err:2 meshbake:2 | plugin:1 · err:3 · -- | scene |  |
| `ErrMeshUpdateRejected` | type | 57 | err:2 meshbake:2 | -- · err:3 · -- | scene |  |

#### `friends.go` — test-only accessors into unexported state · default **follows**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `LayerMaskDrawnBy` | func | 16 | friends:1 | cull:1 light:1 · -- · -- | follows |  |
| `LookupAccessLookup` | func | 19 | friends:1 | -- | follows |  |
| `LookupDefaults` | func | 22 | friends:1 | -- | follows |  |
| `LookupDrainMeshes` | func | 25 | friends:1 | plugin:1 · -- · -- | follows |  |
| `LookupEnsureBundled` | func | 28 | friends:1 | plugin:1 · -- · -- | follows |  |
| `LookupEnsureUnit` | func | 31 | friends:1 | plugin:1 · -- · -- | follows |  |
| `LookupMesh` | func | 36 | friends:1 | plugin:1 · -- · -- | follows |  |
| `LookupMeshes` | func | 39 | friends:1 | -- | follows |  |
| `LookupModel` | func | 44 | friends:1 | model:1 · -- · -- | follows |  |
| `LookupPendingMeshes` | func | 52 | friends:1 | -- | follows |  |
| `LookupStaging` | func | 55 | friends:1 | -- | follows |  |
| `MaterialTagOf` | func | 58 | friends:1 | material:3 · -- · -- | follows |  |
| `MeshRefGeneration` | func | 61 | friends:1 | plugin:1 · -- · -- | follows |  |
| `MeshRefIndex` | func | 64 | friends:1 | plugin:1 · -- · -- | follows |  |
| `MeshRefSource` | func | 67 | friends:1 | plugin:2 · -- · -- | follows |  |
| `OpQueueAppendDraw` | func | 70 | friends:1 | model:1 · -- · -- | follows |  |
| `OpQueueBeginFlush` | func | 73 | friends:1 | plugin:1 · -- · -- | follows |  |
| `OpQueueDraw` | func | 76 | friends:1 | -- | follows |  |
| `OpQueueDrawCount` | func | 79 | friends:1 | model:1 · -- · -- | follows |  |
| `OpQueueDuplicates` | func | 82 | friends:1 | plugin:1 · -- · -- | follows |  |
| `OpQueueEndFlush` | func | 85 | friends:1 | plugin:1 · -- · -- | follows |  |
| `OpQueueFlushDraws` | func | 88 | friends:1 | plugin:2 · -- · -- | follows |  |
| `OpQueueFlushLights` | func | 91 | friends:1 | plugin:1 · -- · -- | follows |  |
| `OpQueueFlushMeshes` | func | 94 | friends:1 | plugin:1 · -- · -- | follows |  |
| `OpQueueFlushModels` | func | 97 | friends:1 | model:1 · -- · -- | follows |  |
| `OpQueuePublishBatches` | func | 100 | friends:1 | plugin:1 · -- · -- | follows |  |
| `OpQueuePublishPass` | func | 103 | friends:1 | plugin:1 · -- · -- | follows |  |
| `OpQueuePublishedFrame` | func | 106 | friends:1 | plugin:1 · -- · -- | follows |  |
| `OpQueueRecordedDraws` | func | 109 | friends:1 | -- | follows |  |
| `OpQueueRecordedMeshes` | func | 112 | friends:1 | -- | follows |  |
| `PassTagOf` | func | 115 | friends:1 | plugin:3 projection:3 · -- · -- | follows |  |
| `SelectorReportKey` | func | 118 | friends:1 | model:1 · -- · -- | follows |  |

#### `gltfanim.go` — decode: animation, skins, poses · default **model/decode**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `bakedAnimation` | type | 20 | gltfanim:5 gltfload:1 gltfmorph:1 | -- | model/decode |  |
| `BakedClip` | type | 51 | animpack:3 gltfanim:6 | -- | model/decode |  |
| `restRow` | const | 90 | animpack:1 gltfanim:2 | -- | model/decode |  |
| `jointSpace` | type | 99 | gltfanim:8 gltfload:1 | -- | model/decode |  |
| `newJointSpace` | func | 116 | gltfanim:1 | -- | model/decode |  |
| `clipTrack` | type | 392 | gltfanim:5 gltfmorph:1 | -- | model/decode |  |
| `animatedWeights` | type | 407 | gltfanim:3 gltfmorph:6 | -- | model/decode |  |
| `animatedNode` | type | 413 | gltfanim:6 | -- | model/decode |  |
| `animCurve` | type | 643 | gltfanim:13 gltfload:2 gltfmorph:1 | -- | model/decode |  |
| `samplerKey` | type | 699 | gltfanim:2 gltfload:4 gltfmorph:1 | -- | model/decode |  |
| `addVec4` | func | 785 | animpack:3 gltfanim:4 | -- | model/decode |  |
| `scaleVec4` | func | 789 | animpack:3 gltfanim:7 | -- | model/decode |  |
| `negateVec4` | func | 793 | animpack:1 gltfanim:1 | -- | model/decode |  |
| `dotQuat` | func | 795 | animpack:1 gltfanim:1 | -- | model/decode |  |
| `lerpVec4` | func | 802 | gltfanim:2 | -- | model/decode |  |

#### `gltfattr.go` — decode: accessor reads · default **model/decode**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `attrValue` | type | 14 | gltfanim:3 gltfattr:10 gltfmesh:5 gltfmorph:3 | -- | model/decode |  |
| `readAttribute` | func | 26 | gltfanim:3 gltfattr:1 gltfmesh:5 gltfmorph:3 | -- | model/decode |  |
| `attrComponent` | type | 90 | gltfattr:6 | -- | model/decode |  |
| `componentScale` | func | 102 | gltfattr:2 | -- | model/decode |  |
| `component` | func | 122 | gltfanim:1 gltfattr:17 gltfmesh:1 mesh:1 morphdelta:2 shader:2 vertexoct:3 vertexpack:2 vertexuv:4 | -- · types:1 · -- | n/a | generic component helper |
| `fillScalar` | func | 130 | gltfattr:6 | -- | model/decode |  |
| `fillVec2` | func | 136 | gltfattr:6 | -- | model/decode |  |
| `fillVec3` | func | 145 | gltfattr:6 | -- | model/decode |  |
| `fillVec4` | func | 155 | gltfattr:6 | -- | model/decode |  |
| `accessorAt` | func | 171 | gltfanim:3 gltfattr:2 gltfmesh:1 gltfmorph:2 | -- | model/decode |  |
| `attributeAccessor` | func | 179 | gltfattr:1 gltfmesh:9 gltfmorph:1 | -- | model/decode |  |

#### `gltfload.go` — decode: document, scenes, nodes, materials · default **model/decode**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `extEmissiveStrength` | const | 20 | gltfload:2 | -- | model/decode |  |
| `extMeshQuantization` | const | 21 | gltfload:1 | -- | model/decode |  |
| `supportedRequired` | var | 33 | gltfload:2 | -- | model/decode |  |
| `LoadedModel` | type | 43 | gltfload:6 modeltable:4 | -- | model/decode |  |
| `loadedPrimitive` | type | 89 | gltfload:3 | -- | model/decode |  |
| `loadedScene` | type | 127 | gltfload:6 modelquery:1 modeltable:1 | -- | model/decode |  |
| `loadedNode` | type | 143 | gltfload:4 | -- | model/decode |  |
| `geometryKey` | type | 183 | gltfload:4 | -- | model/decode |  |
| `loadedMaterial` | type | 201 | gltfload:5 modeltable:1 | -- | model/decode |  |
| `materialVariant` | type | 215 | gltfload:4 | -- | model/decode |  |
| `defaultMaterial` | const | 222 | gltfload:2 | -- | model/decode |  |
| `modelConverter` | type | 229 | gltfanim:14 gltfload:12 gltfmorph:5 | -- | model/decode |  |
| `convertDocument` | func | 299 | gltfload:1 modeltable:1 | -- | model/decode |  |
| `checkRequiredExtensions` | func | 345 | gltfload:2 | -- | model/decode |  |
| `defaultSceneIndex` | func | 356 | gltfload:2 | -- | model/decode |  |
| `animatedNodes` | func | 370 | gltfload:2 | -- | model/decode |  |
| `nodeBinding` | type | 522 | gltfload:3 | -- | model/decode |  |
| `baseColorSlot` | const | 763 | gltfload:1 | -- | model/decode |  |
| `metallicRoughnessSlot` | const | 764 | gltfload:1 | -- | model/decode |  |
| `occlusionSlot` | const | 765 | gltfload:1 | -- | model/decode |  |
| `emissiveSlot` | const | 766 | gltfload:1 | -- | model/decode |  |
| `emissiveFactor` | func | 777 | gltfload:3 opqueue:1 override:1 shapes:1 | -- | model/decode |  |
| `emissiveStrength` | func | 796 | gltfload:3 | -- | model/decode |  |
| `alphaModeOf` | func | 819 | gltfload:2 | -- | model/decode |  |
| `nodeMatrix` | func | 832 | gltfanim:2 gltfload:2 | -- | model/decode |  |

#### `gltfmesh.go` — decode: primitives, topology, tangents · default **model/decode**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `gltfGeometry` | type | 22 | gltfanim:1 gltfload:1 gltfmesh:9 modeltable:1 | -- | model/decode |  |
| `errPointTopology` | var | 82 | gltfmesh:2 | -- | model/decode |  |
| `convertPrimitive` | func | 90 | gltfload:1 gltfmesh:1 | -- | model/decode |  |
| `readVertexAttributes` | func | 151 | gltfmesh:2 vertexpack:1 | -- | model/decode |  |
| `readIndices` | func | 242 | gltfmesh:2 | -- | model/decode |  |
| `convertTopology` | func | 264 | gltfmesh:2 | -- | model/decode |  |
| `sequence` | func | 289 | camera:1 gltfmesh:8 | -- · types:1 · -- | n/a | slice helper |
| `expandLineStrip` | func | 301 | gltfmesh:3 | -- | model/decode |  |
| `expandTriangleStrip` | func | 322 | gltfmesh:2 | -- | model/decode |  |
| `expandTriangleFan` | func | 338 | gltfmesh:2 | -- | model/decode |  |
| `unweld` | func | 355 | gltfmesh:2 gltfmorph:1 morphdelta:1 | -- | model/decode |  |
| `generateFlatNormals` | func | 383 | gltfmesh:2 | -- | model/decode |  |
| `generateTangents` | func | 407 | gltfmesh:2 | -- | model/decode |  |
| `triangles` | func | 445 | gltfmesh:3 modelerr:1 shapes:1 unitmesh:4 | -- · err:1 · -- | n/a | index helper |
| `orthogonal` | func | 456 | anim:1 gltfmesh:3 | -- | n/a | vector helper |
| `accessorBox` | func | 472 | gltfmesh:2 | -- | model/decode |  |

#### `gltfmorph.go` — decode: morph targets and curves · default **model/decode**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `gltfMorph` | type | 19 | gltfmesh:1 gltfmorph:9 | -- | model/decode |  |
| `readMorphTargets` | func | 58 | gltfmesh:1 gltfmorph:1 | -- | model/decode |  |
| `morphSlots` | var | 133 | gltfmorph:3 morph:2 morphdelta:7 | -- | model/decode |  |
| `maxLength` | func | 146 | gltfmorph:2 | -- | model/decode |  |
| `morphSlotRun` | type | 281 | gltfanim:1 gltfload:3 gltfmorph:4 | -- | model/decode |  |
| `morphTargetNames` | func | 292 | gltfmorph:2 | -- | model/decode |  |
| `morphCurve` | type | 331 | gltfanim:1 gltfload:2 gltfmorph:11 | -- | model/decode |  |
| `hermite` | func | 481 | gltfanim:3 gltfmorph:2 | -- | model/decode |  |

#### `gltftexture.go` — decode: texture requests and samplers · default **model/decode**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `missingTexture` | const | 25 | gltfload:3 gltftexture:7 | -- | model/decode |  |
| `textureRequests` | type | 38 | gltfload:1 gltftexture:7 | -- | model/decode |  |
| `newTextureRequests` | func | 49 | gltfload:1 | -- | model/decode |  |
| `readBufferViewBytes` | func | 163 | gltftexture:2 | -- | model/decode |  |
| `defaultModelSampler` | var | 176 | gltfload:1 gltftexture:3 | -- | model/decode |  |
| `modelSampler` | func | 186 | gltftexture:2 | -- | model/decode |  |
| `addressMode` | func | 211 | gltftexture:2 | -- | model/decode |  |

#### `inspect.go` — Op inspection surface · default **scene**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `OpKind` | type | 9 | inspect:3 | -- · types:3 · -- | scene |  |
| `OpCamera` | const | 12 | inspect:1 opqueue:1 | -- · types:2 · -- | scene |  |
| `OpBox` | const | 13 | inspect:1 shapes:1 | -- · types:2 · -- | scene |  |
| `OpSphere` | const | 14 | inspect:1 shapes:1 | -- · types:2 · -- | scene |  |
| `OpPlane` | const | 15 | inspect:1 shapes:1 | -- · types:2 · -- | scene |  |
| `OpLine3D` | const | 16 | inspect:2 shapes:1 | -- · types:2 · -- | scene |  |
| `OpWireBox` | const | 17 | inspect:2 shapes:1 | -- · types:2 · -- | scene |  |
| `OpMesh` | const | 18 | inspect:1 meshdraw:1 | -- · types:2 · -- | scene |  |
| `OpPointLight` | const | 19 | inspect:1 light:1 | -- · types:2 · -- | scene |  |
| `OpSpotLight` | const | 20 | inspect:1 light:1 | -- · types:2 · -- | scene |  |
| `OpModel` | const | 21 | inspect:1 model:1 | -- · types:2 · -- | scene |  |
| `Op` | type | 27 | inspect:3 light:3 meshdraw:2 model:1 opqueue:9 shapes:5 | -- · types:6 · -- | scene |  |
| `PassView` | type | 84 | friends:1 inspect:1 opqueue:6 | cull:1 plugin:1 · types:3 utils:1 · -- | scene |  |
| `BatchView` | type | 105 | friends:1 inspect:2 mesh:1 modeltable:1 opqueue:2 | draw:2 · types:3 · -- | scene |  |

#### `layers.go` — layer mask · default **scene**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `LayerMask` | type | 9 | camera:1 doc:1 friends:3 inspect:1 layers:9 light:3 meshdraw:1 model:2 opqueue:1 shapes:5 | cull:1 light:2 · types:3 utils:1 · types:4 | scene |  |
| `LayersAll` | const | 12 | camera:1 layers:3 | -- · types:4 · -- | scene |  |
| `layerCount` | const | 15 | layers:2 | -- | scene |  |
| `Layer` | func | 19 | layers:1 | -- · utils:3 · -- | scene |  |

#### `light.go` — light descriptor and recording · default **split**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `LightKind` | type | 30 | light:3 | -- · types:3 · types:1 | model | KHR_lights_punctual |
| `LightPoint` | const | 33 | light:1 | -- · types:2 · -- | model |  |
| `LightSpot` | const | 34 | gltfload:1 light:1 | light:1 · types:2 · systems:1 | model |  |
| `LightDescr` | type | 47 | gltfload:1 inspect:1 light:6 model:1 | light:2 · doc:1 types:4 · systems:1 | model | the loader already fills one; ModelLight carries it out of the file |
| `MaxLights` | const | 63 | light:1 shader:2 | light:4 pack:1 · -- · -- | scene | a per-pass cap, and the shader array size |
| `LightRecord` | type | 66 | friends:1 light:4 opqueue:2 | light:1 · -- · -- | scene | one light as the queue recorded it, with its LayerMask |

#### `lookup.go` — the one persistent resource · default **split**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `Lookup` | type | 19 | animpack:1 config:1 doc:2 friends:19 gltfload:1 lookup:18 mesh:6 meshbake:6 modeltable:14 modelunload:2 unitmesh:1 vertexpack:1 | model:1 plugin:10 · config:1 doc:2 resources:3 types:4 utils:6 · -- | unclear | one resource holding both caches and the mesh table, staging and unit meshes |
| `NewLookup` | func | 72 | lookup:2 | -- · utils:3 · -- | unclear |  |
| `NewSizedLookup` | func | 76 | lookup:2 | plugin:1 · -- · -- | unclear |  |
| `LookupAccess` | type | 98 | doc:2 friends:2 lookup:5 meshbake:4 meshdraw:1 modeltable:2 modelunload:1 | -- · doc:1 resources:1 types:3 utils:3 · -- | unclear | scoped access to the above; both halves reach through it |
| `NewLookupAccess` | func | 105 | lookup:2 | -- · types:1 utils:3 · -- | unclear |  |
| `LookupDeviceAccess` | type | 126 | doc:2 lookup:7 modelquery:5 modeltable:7 modelunload:2 | -- · doc:1 resources:1 types:3 utils:1 · -- | unclear |  |
| `NewLookupDeviceAccess` | func | 136 | lookup:1 | -- · types:1 utils:3 · -- | unclear |  |

#### `material.go` — material vocabulary, PBR defaults, shader variants · default **split**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `MaterialTag` | type | 19 | doc:1 friends:2 material:3 meshdraw:1 | -- · types:3 · doc:1 types:4 systems:1 | scene | binds a PassTag to a gfx.MaterialDescr; the tag is the renderer's |
| `Material` | type | 34 | friends:1 gltfload:2 lookup:2 material:9 mesh:1 meshbake:1 meshdraw:15 model:14 modeltable:4 opqueue:6 | material:5 model:4 plugin:2 · doc:1 err:2 types:6 · doc:1 types:6 plugin:1 systems:11 | scene | a slice of MaterialTag, so it inherits the renderer side |
| `MaterialKey` | type | 53 | material:3 meshdraw:5 model:5 opqueue:3 | material:3 model:3 plugin:1 · -- · -- | scene | the batching key |
| `materialSeed` | var | 55 | material:1 | -- | scene |  |
| `MaterialKeyOf` | func | 60 | material:1 meshdraw:1 | material:1 · -- · -- | scene |  |
| `grow` | func | 77 | anim:1 gltfanim:2 gltfload:1 material:1 morph:1 | material:4 model:3 plugin:2 · types:1 · -- | n/a | generic slice helper |
| `PbrDefaults` | type | 93 | friends:1 lookup:2 material:2 modeltable:3 | -- | model | the bundled PBR the decoder fills |
| `PbrSampler` | var | 101 | material:2 | -- | model |  |
| `PbrSlot` | type | 112 | material:2 | -- | model |  |
| `PbrSlots` | var | 123 | gltfload:1 material:3 modeltable:1 override:3 pack:2 | -- | model | the five glTF texture slots, by their glTF names |
| `NormalSlot` | const | 148 | gltfload:3 material:2 modeltable:1 | -- | model |  |
| `BundledPbr` | func | 167 | lookup:1 material:1 | -- | model |  |
| `ShaderVariant` | type | 197 | material:5 modeltable:1 | material:1 · -- · -- | unclear | chosen from model facts (skin, morph) but names a pipeline |
| `VariantStatic` | const | 200 | material:1 | -- | unclear |  |
| `variantSkin` | const | 201 | material:2 | -- | unclear |  |
| `variantMorph` | const | 202 | material:2 | -- | unclear |  |
| `variantSkinMorph` | const | 203 | -- | -- | unclear |  |
| `VariantCount` | const | 204 | lookup:2 material:2 modeltable:1 | material:1 · -- · -- | unclear |  |
| `VariantFor` | func | 208 | material:1 | model:1 plugin:1 · -- · -- | unclear |  |
| `AlphaMode` | type | 236 | gltfload:4 material:3 | -- | model | glTF alphaMode |
| `AlphaOpaque` | const | 239 | gltfload:2 material:1 | -- | model |  |
| `AlphaMask` | const | 240 | gltfload:3 | -- | model |  |
| `AlphaBlend` | const | 241 | gltfload:2 material:1 | -- | model |  |
| `PbrState` | func | 251 | gltfload:2 material:2 | -- | model | turns a glTF alpha mode into the gfx.MaterialState it implies |

#### `mesh.go` — authoring vertex, mesh refs, mesh minting · default **split**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `Vertex` | type | 54 | mesh:12 meshbake:1 meshdraw:2 modeltable:1 morphdelta:1 unitmesh:12 vertexpack:6 | -- · err:1 types:7 · -- | model | the authoring vertex the packer consumes; not glTF-shaped, see the notes |
| `MeshSource` | type | 70 | friends:1 mesh:3 | -- | unclear | durable vs temporary is a residency fact, and temporary is the queue's |
| `MeshNone` | const | 73 | unitmesh:1 | plugin:1 · -- · -- | unclear |  |
| `MeshDurable` | const | 74 | mesh:4 | -- | unclear |  |
| `MeshTemporary` | const | 75 | mesh:1 meshbake:1 meshdraw:1 | plugin:1 · -- · -- | unclear |  |
| `TemporaryMeshID` | const | 82 | mesh:3 | -- | scene | the queue's per-frame mesh slot |
| `VertexLayout` | type | 106 | mesh:8 meshbake:3 meshdraw:1 vertexpack:2 | -- · types:3 · -- | both | a gfx layout; rejected as a Component for being an interface |
| `MeshRef` | type | 113 | doc:1 err:2 friends:8 inspect:1 lookup:2 mesh:9 meshbake:8 meshdraw:4 modeltable:4 opqueue:2 unitmesh:1 | cull:1 plugin:4 · err:2 types:3 · doc:1 types:1 | both | a dense id and a generation; both renderers name meshes with it |
| `MeshRecord` | type | 134 | doc:1 friends:2 lookup:1 mesh:8 meshbake:3 meshdraw:2 modeltable:1 | cull:2 draw:1 plugin:4 · -- · -- | both | the uploaded mesh, read by the draw path of either renderer |
| `layoutCache` | type | 186 | lookup:1 mesh:3 meshdraw:1 | -- | model/cache |  |
| `meshInput` | type | 218 | mesh:5 meshbake:1 unitmesh:1 | -- | model/cache |  |
| `mintMesh` | func | 258 | mesh:1 meshbake:2 meshdraw:1 | -- | model/cache |  |
| `validateMesh` | func | 297 | mesh:3 | -- | model/cache |  |
| `uploadBytes` | func | 324 | mesh:3 | -- | model/cache |  |
| `BakeFunc` | type | 349 | friends:1 mesh:3 unitmesh:1 | plugin:1 · -- · -- | model/cache |  |
| `bakeTextureFunc` | type | 353 | friends:1 lookup:1 mesh:1 | -- | model/cache |  |
| `narrowIndexLimit` | const | 399 | mesh:2 | -- | model/cache |  |
| `indexWidthFor` | func | 408 | mesh:2 modeltable:1 unitmesh:1 | -- | model/cache |  |
| `indexBytes` | func | 423 | mesh:2 modeltable:1 unitmesh:1 vertexpack:1 | -- | model/cache |  |

#### `meshbake.go` — deferred bake and upload of app-built meshes · default **both**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `Span` | type | 10 | mesh:2 meshbake:4 meshdraw:2 vertexpack:6 | -- | both |  |
| `pendingMesh` | type | 23 | friends:1 lookup:1 meshbake:2 | -- | both |  |
| `MeshBaker` | type | 36 | friends:1 meshbake:3 | plugin:1 · -- · -- | both |  |
| `rebakeIndices` | func | 222 | meshbake:2 | -- | both |  |

#### `meshdraw.go` — MeshDraw and its recording · default **scene**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `MeshDraw` | type | 12 | inspect:1 meshdraw:7 model:2 modelquery:1 opqueue:2 | -- · doc:1 types:4 · systems:1 | scene |  |
| `temporaryMesh` | type | 152 | meshdraw:4 | -- | scene |  |
| `MeshRecording` | type | 175 | friends:2 meshdraw:3 opqueue:3 | -- | scene |  |

#### `model.go` — ModelDraw, ModelLight, draw record · default **split**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `ModelDraw` | type | 11 | inspect:1 meshdraw:1 model:3 modelquery:1 | -- · doc:1 types:5 · systems:1 | scene |  |
| `ModelLight` | type | 126 | gltfload:2 model:1 modeltable:3 | -- · types:3 · -- | model | a light carried out of the file |
| `ModelDrawRecord` | type | 142 | friends:1 model:4 opqueue:2 | model:2 · -- · -- | scene |  |

#### `modelerr.go` — model load and selection errors · default **model**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `ErrModelUnavailable` | type | 17 | modelerr:3 modelquery:1 modeltable:3 | -- · err:3 · -- | model |  |
| `ErrModelTextureUnavailable` | type | 41 | gltftexture:4 modelerr:3 modeltable:1 texturetable:1 | -- · err:3 · -- | model |  |
| `ErrModelPrimitiveSkipped` | type | 58 | gltfload:1 modelerr:3 | -- · err:3 · -- | model |  |
| `ErrModelBoundsMissing` | type | 75 | gltfload:1 modelerr:2 | -- · err:3 · -- | model |  |
| `ErrModelPathInvalid` | type | 88 | modelerr:2 modeltable:1 | -- · err:3 · -- | model |  |
| `ErrModelNodeDuplicated` | type | 98 | gltfload:1 modelerr:2 | -- · err:3 · -- | model |  |
| `ErrModelSceneMissing` | type | 115 | modelerr:2 modelquery:2 modelselect:2 | -- · err:3 · -- | model |  |
| `ErrModelNodeMissing` | type | 129 | modelerr:2 modelquery:1 modelselect:2 | -- · err:3 · -- | model |  |
| `ErrModelNodeDegenerate` | type | 145 | modelerr:2 modelselect:2 | -- · err:3 · -- | model |  |
| `ErrModelSkinUnbound` | type | 159 | gltfanim:1 modelerr:3 | -- · err:3 · -- | model |  |
| `ErrModelPoseApproximated` | type | 181 | gltfanim:1 modelerr:2 | -- · err:3 · -- | model |  |
| `ErrModelClipMissing` | type | 195 | animpack:1 modelerr:2 | -- · err:3 · -- | model |  |
| `ErrModelPlaysOverLimit` | type | 207 | animpack:1 modelerr:2 | -- · err:3 · -- | model |  |
| `ErrModelMorphWeightsOverLength` | type | 227 | modelerr:2 morph:1 | -- · err:3 · -- | model |  |
| `ErrModelMorphTargetsOverLimit` | type | 245 | modelerr:2 morph:1 | -- · err:3 · -- | model |  |

#### `modelquery.go` — ModelRef, path validation, bounds · default **model**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `ModelRef` | type | 20 | modelquery:6 | -- · types:3 · doc:1 types:1 | model |  |
| `modelBox` | type | 35 | modelquery:1 modeltable:3 | -- | model |  |
| `ModelKey` | func | 52 | modelquery:1 modeltable:1 modelunload:3 | -- | model |  |
| `validateResourcePath` | func | 67 | modelquery:2 | -- | model |  |

#### `modelselect.go` — ModelView, scene/node selection · default **model**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `ModelView` | type | 12 | doc:1 modelselect:6 | model:2 plugin:1 · -- · -- | model |  |
| `ModelSelectorError` | type | 49 | friends:2 modelquery:1 modelselect:2 | -- | model |  |

#### `modeltable.go` — model cache: load, parse, upload, residency · default **model/cache**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `ModelDescrParams` | type | 24 | lookup:1 modeltable:3 texturetable:1 | -- | model/cache |  |
| `modelDescr` | alias | 27 | modeltable:3 modelunload:1 | -- | model/cache |  |
| `modelUserData` | type | 41 | lookup:1 modeltable:5 modelunload:2 | -- | model/cache |  |
| `modelLoader` | type | 50 | lookup:1 modeltable:4 | -- | model/cache |  |
| `residentModel` | type | 59 | friends:1 lookup:2 modelquery:1 modelselect:1 modeltable:8 | -- | model/cache |  |
| `modelPrimitive` | type | 97 | modelquery:1 modelselect:1 modeltable:5 | -- | model/cache |  |
| `modelMaterial` | type | 136 | modelselect:1 modeltable:5 | -- | model/cache |  |
| `modelReportPrefix` | const | 147 | modeltable:1 modelunload:1 | -- | model/cache |  |
| `textureReportPrefix` | const | 148 | modeltable:1 modelunload:1 | -- | model/cache |  |
| `modelReportKey` | func | 151 | modeltable:4 modelunload:1 | -- | model/cache |  |
| `textureReportKey` | func | 152 | modeltable:2 modelunload:1 texturetable:1 | -- | model/cache |  |
| `errBackendNotReady` | var | 158 | modeltable:2 | -- | model/cache |  |
| `errModelNotRead` | var | 165 | modeltable:2 | -- | model/cache |  |
| `errLookupUnavailable` | var | 170 | modelquery:1 modeltable:1 | -- | model/cache |  |
| `parseModel` | func | 273 | modeltable:2 | -- | model/cache |  |
| `directoryFS` | func | 291 | modeltable:2 | -- | model/cache |  |
| `bindModelMaterial` | func | 403 | modeltable:2 texturetable:1 | -- | model/cache |  |
| `needsAnimatedReroot` | func | 586 | modeltable:2 | -- | model/cache |  |

#### `morph.go` — morph mask, binding, weight blending · default **model/pack**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `morphMask` | type | 15 | gltfmorph:3 morph:7 morphdelta:1 | -- | model/pack |  |
| `morphPosition` | const | 18 | gltfmorph:3 | -- | model/pack |  |
| `morphNormal` | const | 19 | gltfmorph:2 | -- | model/pack |  |
| `morphTangent` | const | 20 | gltfmorph:2 | -- | model/pack |  |
| `MorphBinding` | type | 74 | gltfload:1 gltfmorph:3 modeltable:1 morph:2 | animpack:1 · -- · -- | model/pack | written by the decoder, read by the draw path |
| `SceneMorphWeight` | type | 105 | morph:6 | animpack:1 plugin:1 · -- · -- | model/pack |  |
| `morphWeightSize` | var | 110 | -- | -- | model/pack |  |
| `maxMorphTargets` | const | 115 | morph:4 | -- | model/pack |  |
| `morphWeightTolerance` | const | 121 | morph:2 | -- | model/pack |  |
| `BlendMorphWeights` | func | 135 | morph:1 | model:1 · -- · -- | model/pack |  |
| `SelectMorphTargets` | func | 191 | morph:1 | model:1 · -- · -- | model/pack |  |
| `morphWeightsReportKey` | func | 221 | morph:1 | -- | model/pack |  |
| `morphTargetsReportKey` | func | 222 | morph:1 | -- | model/pack |  |

#### `morphdelta.go` — morph delta packing · default **model/pack**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `MorphRangeWords` | const | 50 | morphdelta:2 | -- | model/pack |  |
| `MorphTargetHeaderWords` | const | 54 | morphdelta:2 | -- | model/pack |  |
| `MorphWordSize` | const | 58 | modeltable:1 morphdelta:1 | -- | model/pack |  |
| `morphPositionCodeMax` | const | 65 | morphdelta:3 | -- | model/pack |  |
| `morphDirectionCodeMax` | const | 66 | morphdelta:3 | -- | model/pack |  |
| `morphRanges` | type | 77 | morphdelta:3 | -- | model/pack |  |
| `morphSpan` | type | 96 | morphdelta:5 | -- | model/pack |  |
| `morphLiveSpan` | func | 109 | morphdelta:2 | -- | model/pack |  |
| `packMorphBlock` | func | 141 | gltfmorph:2 morphdelta:1 | -- | model/pack |  |
| `packMorphPosition` | func | 189 | gltfmorph:1 morphdelta:1 | -- | model/pack |  |
| `packMorphDirection` | func | 202 | gltfmorph:2 morphdelta:1 | -- | model/pack |  |
| `morphSnormCode` | func | 220 | morphdelta:7 | -- | model/pack |  |

#### `opqueue.go` — the recording queue and its records · default **scene**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `CameraRecord` | type | 13 | friends:1 opqueue:6 | plugin:2 · -- · -- | scene |  |
| `OpQueue` | type | 24 | camera:1 doc:4 friends:30 light:3 meshdraw:2 model:2 opqueue:17 shapes:5 | doc:1 draw:1 model:1 plugin:14 · doc:2 id:1 resources:3 types:1 · systems:1 | scene |  |
| `DrawRecord` | type | 209 | doc:1 friends:4 meshdraw:1 model:1 opqueue:9 shapes:5 | cull:2 model:1 plugin:2 · -- · -- | scene |  |

#### `override.go` — parameter override over the PBR record · default **split**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `overrideRecord` | func | 24 | opqueue:1 override:1 | -- | unclear | a renderer act that writes a model record in place |

#### `pack.go` — PBR record, anim binding, skin buffers · default **split**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `SceneNoAnim` | const | 11 | pack:1 | animpack:2 model:1 pack:1 plugin:1 · -- · -- | model/pack |  |
| `ScenePbrRecord` | type | 31 | gltfload:1 modeltable:1 opqueue:3 override:2 pack:4 | draw:2 · -- · -- | model/pack | written by the decoder, read by the draw path and by override |
| `pbrSlotCount` | const | 60 | gltfload:2 modeltable:1 pack:3 | -- | model/pack |  |
| `defaultPbrRecord` | func | 64 | gltfload:1 opqueue:1 pack:1 | -- | model/pack |  |
| `pbrUVSetCount` | const | 93 | pack:2 | -- | model/pack |  |
| `AnimBinding` | type | 104 | doc:1 opqueue:1 pack:1 | cull:1 draw:1 model:2 pack:1 plugin:1 · -- · -- | unclear | assembled per batch at record time out of the model's buffers |
| `SkinBuffers` | type | 128 | animpack:2 pack:2 | draw:1 · -- · -- | model | the model's pose, joint and morph buffers |
| `recordSliceBytes` | func | 147 | modeltable:3 pack:1 | -- | model/pack |  |

#### `projection.go` — projection and view direction maths · default **scene**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `Projection` | func | 13 | camera:2 coords:1 projection:4 | pack:1 plugin:2 · -- · types:1 systems:2 | scene |  |
| `ViewDirection` | func | 51 | projection:1 transform:1 | pack:3 plugin:2 · -- · -- | scene |  |

#### `shader.go` — bundled shader paths · default **split**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `SceneShaderPath` | const | 14 | shader:2 | shaderfs:1 · -- · -- | scene | the bundled scene shader |
| `VertexDecodePath` | const | 42 | shader:1 vertexoct:1 | shaderfs:1 · err:1 types:2 · -- | model | the decode for the storage layout the packer writes |
| `SceneShader` | func | 50 | material:1 shader:1 | -- | scene |  |

#### `shapes.go` — Box/Sphere/Plane/Line3D/WireBox recording calls · default **scene**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `lineTransform` | func | 120 | shapes:2 | -- | scene |  |
| `abs32` | func | 142 | gltfmesh:4 shapes:1 vertexoct:5 | -- | n/a | numeric helper |

#### `texturetable.go` — texture cache · default **model/cache**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `textureDescrParams` | type | 31 | gltftexture:2 lookup:1 texturetable:4 | -- | model/cache |  |
| `textureDescr` | alias | 64 | gltfload:1 gltftexture:5 modelunload:1 texturetable:2 | -- | model/cache |  |
| `externalImage` | const | 68 | gltftexture:2 texturetable:3 | -- | model/cache |  |
| `textureUserData` | type | 77 | lookup:1 modeltable:1 modelunload:2 texturetable:4 | -- | model/cache |  |
| `textureLoader` | type | 85 | lookup:1 texturetable:4 | -- | model/cache |  |
| `placeholderTexture` | func | 155 | texturetable:3 | -- | model/cache |  |
| `magentaTexel` | var | 165 | texturetable:2 | -- | model/cache |  |
| `textureReportPath` | func | 170 | gltftexture:1 texturetable:2 | -- | model/cache |  |

#### `transform.go` — re-export of m.Transform and its helpers · default **m**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `Transform` | alias | 12 | anim:1 camera:2 coords:1 inspect:5 material:6 meshdraw:8 model:10 modelquery:2 modelselect:1 modeltable:1 opqueue:4 override:1 projection:1 shapes:13 transform:8 | cull:1 model:2 plugin:3 · doc:1 types:5 utils:4 · doc:2 types:11 plugin:1 systems:14 | m | already an alias to m.Transform, in m because sound needs one too |
| `At` | func | 15 | mesh:1 morphdelta:1 transform:2 vertexuv:1 | -- · utils:3 · -- | m |  |
| `LookAt` | func | 21 | transform:2 | -- · utils:3 · types:1 systems:1 | m |  |
| `CameraView` | func | 26 | coords:1 projection:1 transform:2 | plugin:1 · -- · -- | scene |  |
| `cameraBasis` | func | 37 | projection:1 transform:2 | -- | scene |  |

#### `unitmesh.go` — unit geometry generators and their residency · default **split**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `unitShape` | type | 14 | friends:1 opqueue:1 unitmesh:3 | -- | scene | the shape enum the queue records with |
| `ShapeNone` | const | 17 | opqueue:2 unitmesh:1 | plugin:1 · -- · -- | scene |  |
| `ShapeBox` | const | 18 | shapes:3 | -- | scene |  |
| `shapeSphere` | const | 19 | shapes:1 unitmesh:2 | -- | scene |  |
| `shapePlane` | const | 20 | shapes:1 unitmesh:1 | -- | scene |  |
| `shapeCount` | const | 21 | lookup:1 | -- | scene |  |
| `sphereSegments` | const | 29 | unitmesh:6 | -- | model/geom |  |
| `sphereRings` | const | 30 | unitmesh:7 | -- | model/geom |  |
| `quadFace` | type | 78 | unitmesh:4 | -- | model/geom |  |
| `appendQuad` | func | 85 | unitmesh:3 | -- | model/geom |  |
| `unitBoxGeometry` | func | 106 | unitmesh:2 | -- | model/geom |  |
| `unitPlaneGeometry` | func | 128 | unitmesh:2 | -- | model/geom |  |
| `unitSphereGeometry` | func | 147 | unitmesh:2 | -- | model/geom |  |

#### `vertexoct.go` — octahedral normal/tangent encoding · default **model/pack**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `octNormalMax` | const | 37 | vertexoct:3 | -- | model/pack |  |
| `octTangentMax` | const | 38 | vertexoct:3 | -- | model/pack |  |
| `octTangentYShift` | const | 56 | vertexoct:1 | -- | model/pack |  |
| `tangentHandednessBit` | const | 57 | vertexoct:1 | -- | model/pack |  |
| `tangentReservedBit` | const | 58 | -- | -- | model/pack |  |
| `octEncode` | func | 63 | vertexoct:3 | -- | model/pack |  |
| `signNotZero` | func | 87 | vertexoct:3 | -- | model/pack |  |
| `quantizeUnorm` | func | 97 | vertexoct:5 | -- | model/pack |  |
| `packNormal` | func | 110 | vertexoct:1 vertexpack:1 | -- | model/pack |  |
| `packTangent` | func | 123 | vertexoct:1 vertexpack:1 | -- | model/pack |  |

#### `vertexpack.go` — vertex packing into the storage layout · default **model/pack**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `storagePosition` | const | 26 | vertexpack:2 | -- | model/pack |  |
| `storageNormal` | const | 27 | vertexpack:3 | -- | model/pack |  |
| `storageTangent` | const | 28 | vertexpack:2 | -- | model/pack |  |
| `storageUV0` | const | 29 | vertexpack:2 | -- | model/pack |  |
| `storageUV1` | const | 30 | vertexpack:2 | -- | model/pack |  |
| `storageColor` | const | 31 | vertexpack:5 | -- | model/pack |  |
| `StorageStride` | const | 32 | vertexpack:3 | -- | model/pack |  |
| `storageJoints` | const | 34 | vertexpack:2 | -- | model/pack |  |
| `storageWeights` | const | 35 | vertexpack:2 | -- | model/pack |  |
| `StorageSkinnedStride` | const | 36 | vertexpack:2 | -- | model/pack |  |
| `unorm8CodeMax` | const | 42 | gltfmesh:1 vertexpack:4 vertexskin:2 | -- | model/pack |  |
| `skinnedVertexLayout` | var | 76 | vertexpack:3 | -- | model/pack |  |
| `standardVertexLayout` | var | 86 | mesh:1 vertexpack:1 | -- | model/pack |  |
| `StandardVertexAttrs` | const | 92 | vertexpack:2 | -- | model/pack |  |
| `skinnedVertex` | type | 108 | gltfmesh:7 modeltable:1 vertexpack:5 | -- | model/pack |  |
| `PackVertices` | func | 160 | mesh:1 unitmesh:1 vertexpack:1 | -- | model/pack |  |
| `boundVertices` | func | 179 | vertexpack:2 | -- | model/pack |  |
| `packOverAuthored` | func | 212 | modeltable:1 vertexpack:2 | -- | model/pack |  |
| `_` | var | 242 | animpack:3 gltfanim:9 gltfload:7 gltfmesh:5 gltfmorph:11 mesh:1 meshbake:2 meshdraw:2 modelquery:3 modeltable:6 modelunload:2 morph:1 morphdelta:1 shapes:6 texturetable:4 unitmesh:4 | draw:2 material:1 model:1 pack:1 plugin:9 projection:1 · -- · plugin:1 systems:6 | model/pack |  |
| `packInto` | func | 246 | vertexpack:2 | -- | model/pack |  |
| `packVertex` | func | 276 | vertexpack:5 | -- | model/pack |  |
| `packSkin` | func | 303 | vertexpack:2 | -- | model/pack |  |
| `packUnorm8` | func | 316 | vertexpack:5 vertexskin:2 | -- | model/pack |  |
| `putUV` | func | 328 | vertexpack:3 | -- | model/pack |  |
| `putFloat32` | func | 333 | vertexpack:3 | -- | model/pack |  |
| `putVec3` | func | 337 | vertexpack:1 | -- | model/pack |  |
| `appendArena` | func | 347 | mesh:2 unitmesh:1 vertexpack:1 | -- | model/pack |  |

#### `vertexskin.go` — joint and weight packing · default **model/pack**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `sceneMaxSkinJoints` | const | 43 | gltfanim:5 vertexskin:2 | -- | model/pack |  |
| `packJoint` | func | 53 | vertexpack:1 vertexskin:1 | -- | model/pack |  |
| `packWeight` | func | 72 | vertexpack:2 vertexskin:1 | -- | model/pack |  |

#### `vertexuv.go` — mesh record, UV range, UV quantisation · default **model/pack**

| name | kind | @ | used inside `internal/types` | used by `internal` · root · `ecsscene` | side | note |
| --- | --- | --- | --- | --- | --- | --- |
| `uvCodeMax` | const | 31 | vertexuv:4 | -- | model/pack |  |
| `SceneMesh` | type | 46 | mesh:2 meshdraw:1 vertexpack:6 vertexuv:8 | draw:3 · -- · -- | model/pack | the mesh record the draw path binds |
| `IdentityMesh` | var | 60 | vertexuv:2 | draw:1 · -- · -- | model/pack |  |
| `uvRange` | type | 85 | gltfmesh:1 vertexpack:1 vertexuv:4 | -- | model/pack |  |
| `meshRecordFor` | func | 113 | modeltable:1 vertexpack:1 vertexuv:1 | -- | model/pack |  |
| `quantizeUV` | func | 134 | vertexpack:2 vertexuv:1 | -- | model/pack |  |

### Methods, by receiver

191 methods on 60 receivers. A method follows its receiver, so these are listed rather
than classified. The receivers declared across several files are the ones to watch:
`OpQueue` spans five files, `Lookup` six, `LookupDeviceAccess` four.

| receiver | n | declared in | methods |
| --- | --- | --- | --- |
| `BakedClip` | 2 | gltfanim.go | row, weightRow |
| `CameraDescr` | 1 | camera.go | shear |
| `DrawRecord` | 3 | opqueue.go | World, PbrRecord, basePbrRecord |
| `ErrCameraProjectionDegenerate` | 1 | err.go | Error |
| `ErrMeshGeometryInvalid` | 1 | err.go | Error |
| `ErrMeshUnavailable` | 1 | err.go | Error |
| `ErrMeshUpdateRejected` | 1 | err.go | Error |
| `ErrModelBoundsMissing` | 1 | modelerr.go | Error |
| `ErrModelClipMissing` | 1 | modelerr.go | Error |
| `ErrModelMorphTargetsOverLimit` | 1 | modelerr.go | Error |
| `ErrModelMorphWeightsOverLength` | 1 | modelerr.go | Error |
| `ErrModelNodeDegenerate` | 2 | modelerr.go modelselect.go | Error, reportKey |
| `ErrModelNodeDuplicated` | 1 | modelerr.go | Error |
| `ErrModelNodeMissing` | 2 | modelerr.go modelselect.go | Error, reportKey |
| `ErrModelPathInvalid` | 1 | modelerr.go | Error |
| `ErrModelPlaysOverLimit` | 1 | modelerr.go | Error |
| `ErrModelPoseApproximated` | 1 | modelerr.go | Error |
| `ErrModelPrimitiveSkipped` | 2 | modelerr.go | Error, Unwrap |
| `ErrModelSceneMissing` | 2 | modelerr.go modelselect.go | Error, reportKey |
| `ErrModelSkinUnbound` | 2 | modelerr.go | Error, Unwrap |
| `ErrModelTextureUnavailable` | 2 | modelerr.go | Error, Unwrap |
| `ErrModelUnavailable` | 2 | modelerr.go | Error, Unwrap |
| `ErrTextureUVSetUnsupported` | 1 | err.go | Error |
| `LayerMask` | 2 | layers.go | drawnBy, orAll |
| `LookupAccess` | 7 | lookup.go meshbake.go modeltable.go modelunload.go | Valid, BakeMesh, UpdateMesh, ReleaseMesh, TotalPoseBytes, TotalMorphBytes, UnloadModel |
| `LookupDeviceAccess` | 16 | lookup.go modelquery.go modeltable.go modelunload.go | Valid, resolve, State, Nodes, Bounds, AABB, bounds, ModelLights, Preload, Joints, Clips, PoseBytes, MorphTargets, MorphBytes, UnloadTexture, UnloadAll |
| `Lookup` | 16 | lookup.go mesh.go meshbake.go modeltable.go modelunload.go unitmesh.go | ensureBundled, mesh, claimMesh, bakeMeshNow, releaseMesh, stage, drainMeshes, model, installModel, reportLoad, bakeModelGeometry, ensureDefaults, residentAnimation, clearTextureReports, clearModelReports, ensureUnit |
| `MaterialTag` | 1 | material.go | tag |
| `MeshDraw` | 1 | meshdraw.go | sphere |
| `MeshRecord` | 1 | mesh.go | Descr |
| `MeshRecording` | 2 | meshdraw.go | reset, copyMaterial |
| `MeshRef` | 1 | mesh.go | ID |
| `ModelDrawRecord` | 1 | model.go | Instances |
| `MorphBinding` | 1 | morph.go | Morphed |
| `OpQueue` | 28 | light.go meshdraw.go model.go opqueue.go shapes.go | PointLight, SpotLight, flushLights, Mesh, TemporaryMesh, Model, flushModels, Camera, OpCount, Reset, Ops, Passes, beginFlush, publishPass, recordedDuplicates, endFlush, draw, appendFlushDraw, drawCount, flushDraws, flushMeshes, publishBatches, resolveBatches, Box, Sphere, Plane, Line3D, WireBox |
| `Pass` | 1 | camera.go | tag |
| `ResidentAnimation` | 2 | animpack.go | Skin, clip |
| `SceneMesh` | 1 | vertexuv.go | packRecord |
| `ScenePbrRecord` | 2 | override.go pack.go | Member, selectUVSet |
| `ShaderVariant` | 1 | material.go | shader |
| `Span` | 1 | meshbake.go | Of |
| `Vertex` | 1 | mesh.go | VertexLayout |
| `animCurve` | 5 | gltfanim.go | end, sample, valueAt, hermite, tangent |
| `animatedNode` | 1 | gltfanim.go | local |
| `gltfGeometry` | 1 | gltfmesh.go | expandBoxByMorph |
| `gltfMorph` | 4 | gltfmorph.go | morphed, vertexCount, remap, binding |
| `jointSpace` | 5 | gltfanim.go | count, claimSkinned, claimPlain, pose, append |
| `layoutCache` | 1 | mesh.go | resolve |
| `modelConverter` | 29 | gltfanim.go gltfload.go gltfmorph.go | buildJointSpace, inverseBindMatrices, bindGeometryJoints, bakeAnimation, claimRerootJoints, clipTracks, restingNode, bakeRestFrame, bakeClip, storeRow, resolveWorlds, composeNode, buildNodeForest, animCurve, flattenScene, walkNode, claimNode, closeNode, flattenMesh, geometry, collectLight, material, convertMaterial, bindSlot, packMorphDeltas, claimMorphSlots, morphCurve, animatedWeights, bakeMorphWeights |
| `modelLoader` | 3 | modeltable.go | Load, Default, Free |
| `morphCurve` | 5 | gltfmorph.go | end, sample, copyKey, valueAt, valueAt3 |
| `morphMask` | 3 | morph.go | slots, recordWords, prefix |
| `morphRanges` | 1 | morphdelta.go | add |
| `residentModel` | 2 | modelquery.go modelselect.go | scene, View |
| `resolvedPlay` | 1 | animpack.go | resolve |
| `skinnedVertex` | 1 | vertexpack.go | VertexLayout |
| `temporaryMesh` | 1 | meshdraw.go | Record |
| `textureLoader` | 3 | texturetable.go | Load, Default, Free |
| `textureRequests` | 4 | gltftexture.go | texture, image, resolvePath, imageBytes |
| `uvRange` | 2 | vertexuv.go | add, scaleBias |

---

## Part C — the decode/runtime cross-cut

Every declaration named by **both** a `gltf*.go` file and a non-decode file. This is the
set the decoder seam cannot cut cleanly, and it is what [the decoder seam: does gltf write
packed records, or hand over data model packs?](https://github.com/dvoyni/cog/issues/496)
is really about.

58 names, after dropping eight generic helpers (`grow`, `abs`, `abs32`, `sequence`,
`component`, `triangles`, `orthogonal`, `maxLength`). Three methods belong here too and do
not appear in the table because they are methods, not declarations:

- `(*ScenePbrRecord).selectUVSet` — `pack.go:82`, called from `gltfload.go:754`
- `(*ScenePbrRecord).Member` — `override.go:54`, called from `overrideRecord` in the same file
- `(morphMask).recordWords` — `morph.go:36`, called from `gltfmorph.go:193` and
  `morphdelta.go:142`

The last column is the one that matters: **which of these the renderer touches at all.**
Everything with `--` there is a seam entirely inside `bundles/model`, and the decision
about it is `model`'s private business.

| name | kind | declared | read by (runtime files) | of those, renderer-side |
| --- | --- | --- | --- | --- |
| `ClipInfo` | type | `anim.go:45` | anim modeltable | -- |
| `scenePose` | type | `anim.go:72` | anim animpack | -- |
| `sceneSkinJoint` | type | `anim.go:95` | anim | -- |
| `skinJointRecord` | func | `anim.go:170` | anim | -- |
| `poseFromMatrix` | func | `anim.go:198` | anim | -- |
| `Config` | type | `config.go:5` | config doc light lookup opqueue | light opqueue |
| `BakedClip` | type | `gltfanim.go:51` | animpack | -- |
| `restRow` | const | `gltfanim.go:90` | animpack | -- |
| `addVec4` | func | `gltfanim.go:785` | animpack | -- |
| `scaleVec4` | func | `gltfanim.go:789` | animpack | -- |
| `negateVec4` | func | `gltfanim.go:793` | animpack | -- |
| `dotQuat` | func | `gltfanim.go:795` | animpack | -- |
| `LoadedModel` | type | `gltfload.go:43` | modeltable | -- |
| `loadedScene` | type | `gltfload.go:127` | modelquery modeltable | -- |
| `loadedMaterial` | type | `gltfload.go:201` | modeltable | -- |
| `convertDocument` | func | `gltfload.go:299` | modeltable | -- |
| `emissiveFactor` | func | `gltfload.go:777` | opqueue override shapes | opqueue override shapes |
| `gltfGeometry` | type | `gltfmesh.go:22` | modeltable | -- |
| `readVertexAttributes` | func | `gltfmesh.go:151` | vertexpack | -- |
| `unweld` | func | `gltfmesh.go:355` | morphdelta | -- |
| `morphSlots` | var | `gltfmorph.go:133` | morph morphdelta | -- |
| `LightSpot` | const | `light.go:34` | light | light |
| `LightDescr` | type | `light.go:47` | inspect light model | inspect light model |
| `Lookup` | type | `lookup.go:19` | animpack config doc friends lookup mesh meshbake modeltable modelunload unitmesh vertexpack | -- |
| `Material` | type | `material.go:34` | friends lookup material mesh meshbake meshdraw model modeltable opqueue | meshdraw model opqueue |
| `PbrSlots` | var | `material.go:123` | material modeltable override pack | override |
| `NormalSlot` | const | `material.go:148` | material modeltable | -- |
| `AlphaMode` | type | `material.go:236` | material | -- |
| `AlphaOpaque` | const | `material.go:239` | material | -- |
| `AlphaMask` | const | `material.go:240` | material | -- |
| `AlphaBlend` | const | `material.go:241` | material | -- |
| `PbrState` | func | `material.go:251` | material | -- |
| `ModelLight` | type | `model.go:126` | model modeltable | model |
| `ErrModelTextureUnavailable` | type | `modelerr.go:41` | modelerr modeltable texturetable | -- |
| `ErrModelPrimitiveSkipped` | type | `modelerr.go:58` | modelerr | -- |
| `ErrModelBoundsMissing` | type | `modelerr.go:75` | modelerr | -- |
| `ErrModelNodeDuplicated` | type | `modelerr.go:98` | modelerr | -- |
| `ErrModelSkinUnbound` | type | `modelerr.go:159` | modelerr | -- |
| `ErrModelPoseApproximated` | type | `modelerr.go:181` | modelerr | -- |
| `morphMask` | type | `morph.go:15` | morph morphdelta | -- |
| `morphPosition` | const | `morph.go:18` | morph | -- |
| `morphNormal` | const | `morph.go:19` | morph | -- |
| `morphTangent` | const | `morph.go:20` | morph | -- |
| `MorphBinding` | type | `morph.go:74` | modeltable morph | -- |
| `packMorphBlock` | func | `morphdelta.go:141` | morphdelta | -- |
| `packMorphPosition` | func | `morphdelta.go:189` | morphdelta | -- |
| `packMorphDirection` | func | `morphdelta.go:202` | morphdelta | -- |
| `ScenePbrRecord` | type | `pack.go:31` | modeltable opqueue override pack | opqueue override |
| `pbrSlotCount` | const | `pack.go:60` | modeltable pack | -- |
| `defaultPbrRecord` | func | `pack.go:64` | opqueue pack | opqueue |
| `textureDescrParams` | type | `texturetable.go:31` | lookup texturetable | -- |
| `textureDescr` | alias | `texturetable.go:64` | modelunload texturetable | -- |
| `externalImage` | const | `texturetable.go:68` | texturetable | -- |
| `textureReportPath` | func | `texturetable.go:170` | texturetable | -- |
| `unorm8CodeMax` | const | `vertexpack.go:42` | vertexpack vertexskin | -- |
| `skinnedVertex` | type | `vertexpack.go:108` | modeltable vertexpack | -- |
| `sceneMaxSkinJoints` | const | `vertexskin.go:43` | vertexskin | -- |
| `uvRange` | type | `vertexuv.go:85` | vertexpack vertexuv | -- |

Only five of the 58 reach a renderer file: `ScenePbrRecord` (with `defaultPbrRecord`,
`emissiveFactor`, `Member` and `PbrSlots`), `Material`, `LightDescr`, `ModelLight` and
`Config`. `Material` is renderer-side by the reading in Part A, so of the model's own
decoder output exactly **one record escapes into the renderer: `ScenePbrRecord`**, through
`opqueue.go` (`DrawRecord.PbrRecord`, `basePbrRecord`) and `override.go`.

---

## Part D — what `ecsscene` names today

### D1. The runtime dependency: 24 scene names in non-test code

| scene name | n | used for |
| --- | --- | --- |
| `scene.Transform` | 9 | `type Transform scene.Transform`, and `scene.Transform(it.Place)` into every draw |
| `scene.Material` | 5 | the `[]MaterialTag` the recording System rebuilds per draw into scratch |
| `scene.LayerMask` | 4 | a `Layers` field on `Model`, `Mesh` and `Light` |
| `scene.ClipPlay` | 4 | `Animation.Plays [MaxPlays]scene.ClipPlay`, and the scratch slice |
| `scene.Pass` | 3 | `Camera.Passes ecs.List[scene.Pass]`, rebuilt per frame |
| `scene.ModelRef` | 2 | `Model.Ref` |
| `scene.MeshRef` | 2 | `Mesh.Ref` |
| `scene.MaterialTag` | 2 | rebuilding a tag entry from `ecsscene.MaterialTag` |
| `scene.LookAt` | 2 | documentation of how a spot aims |
| `scene.CameraDescr` | 2 | the descriptor `queue.Camera` is called with |
| `scene.TagForward` | 1 | the zero `PassTag` default |
| `scene.ProjectionKind` | 1 | `Camera.Projection` |
| `scene.Passes` | 1 | doc comment |
| `scene.PassTag` | 1 | `MaterialTag.Tag` |
| `scene.OpQueue` | 1 | `ecs.Write[*scene.OpQueue]` — the System's output resource |
| `scene.Name` | 1 | the plugin dependency |
| `scene.ModelDraw` | 1 | built per model entity |
| `scene.MeshDraw` | 1 | built per mesh entity |
| `scene.LightSpot` | 1 | the spot branch |
| `scene.LightKind` | 1 | `Light.Kind` |
| `scene.LightDescr` | 1 | built per light entity |
| `scene.FlushOnUpdate` | 1 | ordering: its System runs before scene's flush |
| `scene.ClipPlays` | 1 | doc comment |
| `scene.CameraID` | 1 | `Camera.ID` |

### D2. The test oracle: 13 more names, tests only

`scene.Op`, `scene.OpKind`, `scene.OpCamera`, `scene.OpModel`, `scene.OpMesh`,
`scene.OpPointLight`, `scene.OpSpotLight`, `scene.PassView`, `scene.Lookup`,
`scene.NewLookupAccess`, `scene.Layer`, `scene.Perspective`, `scene.Oblique`,
`scene.Vertex`.

Every behavioural test in `bundles/ecsscene/internal` asserts by reading back the Ops that
`scene` recorded. **A renderer that records straight to `gfx` has no `Op` to read back**,
so the redesign replaces the test oracle as well as the recording path. Nothing on the map
names this yet.

### D3. `ecsscene`'s own declarations

Ten, all in `types.go`, and this is the binding layer the redesign means to remove:

| name | shape | why it is not scene's type |
| --- | --- | --- |
| `Transform` | `type Transform scene.Transform` | a defined type so the Store's Go type belongs to this package |
| `Model` | `{Ref scene.ModelRef; Layers scene.LayerMask}` | the storable half of `ModelDraw` |
| `Mesh` | `{Ref scene.MeshRef; Bounds m.Vec4; Layers; NeverCull}` | the storable half of `MeshDraw` |
| `Animation` | `{Plays [MaxPlays]scene.ClipPlay}` | a fixed array, because a slice is not storable |
| `Params` | `{Values ecs.List[gfx.ParameterDescr]}` | an `ecs.List`, because a slice is not storable |
| `Material` | `{Tags ecs.List[MaterialTag]}` | `scene.Material` **is** a slice |
| `Light` | eight scalar fields | `scene.LightDescr` is storable; this splits position out to the Transform |
| `Camera` | `scene.CameraDescr`'s fields with `Passes ecs.List[scene.Pass]` | `CameraDescr.Passes` is a slice |
| `MaxPlays` | `= 4` | scene's own cap, restated |
| `MaterialTag` | `{Tag; Shader; State; Params ecs.List[...]}` | `gfx.MaterialDescr` holds a slice |

Six of the ten exist **only** because a scene type holds a bare slice. That is the whole
brief for [model's types as Components, or a binding layer over
them](https://github.com/dvoyni/cog/issues/495).

---

## Part E — storability, machine-checked

`ecs.Storable` run today over the root vocabulary and every `ecsscene` Component. This
extends the table at `bundles/ecs/docs/specs/ecs.md:666` with eleven rows it does not
cover.

| type | verdict | first offending field |
| --- | --- | --- |
| `scene.Transform` (= `m.Transform`) | **legal** | |
| `scene.CameraID` | **legal** | |
| `scene.ProjectionKind` | **legal** *(new)* | |
| `scene.PassTag` | **legal** | |
| `scene.Pass` | **legal** | |
| `scene.CameraDescr` | rejected | `.Passes` is a slice |
| `scene.LayerMask` | **legal** | |
| `scene.LightKind` | **legal** *(new)* | |
| `scene.LightDescr` | **legal** *(new)* | |
| `scene.Material` | rejected | is itself a slice |
| `scene.MaterialTag` | rejected | `.Descr.params` is a slice |
| `scene.Vertex` | **legal** *(new)* | |
| `scene.VertexLayout` | rejected | **is an interface** |
| `scene.MeshRef` | **legal** | |
| `scene.MeshDraw` | rejected | `.Transforms` is a slice |
| `scene.ModelDraw` | rejected | `.Transforms` is a slice |
| `scene.ModelLight` | **legal** *(new)* | |
| `scene.ModelRef` | **legal** | |
| `scene.ClipPlay` | **legal** | |
| `scene.ClipInfo` | **legal** *(new)* | |
| `scene.OpKind` | **legal** *(new)* | |
| `scene.Op` | rejected *(new)* | `.Descr.Passes` is a slice |
| `scene.PassView` | rejected *(new)* | `.Batches` is a slice |
| `scene.BatchView` | **legal** *(new)* | |
| `scene.Config` | **legal** *(new)* | |
| `scene.LookupAccess` | rejected *(new)* | `kernel.engine` is a pointer |
| `scene.LookupDeviceAccess` | rejected *(new)* | `kernel.engine` is a pointer |
| every `ecsscene` Component (9) | **legal** | |

Two things worth carrying into the decision:

- **`VertexLayout` is rejected for being an interface**, which is the only rejection in the
  whole vocabulary whose remedy is not `ecs.List`. A Component that names a vertex layout
  needs a concrete descriptor or an interned id instead.
- **`Vertex` is storable**, so a Component could hold one. Whether that is ever wanted is a
  different question, but the rule does not forbid it.

---

## Part F — the `unclear` list, and what makes each ambiguous

21 declarations plus their methods. These are the rows [what model declares, and what a
renderer keeps](https://github.com/dvoyni/cog/issues/494) has to actually decide; the rest
of the inventory is largely mechanical.

| names | what makes it two-sided |
| --- | --- |
| `CameraDescr` (+`shear`) | its projection, near and far are model facts a glTF camera carries; its `Passes`, `CullMask`, sun and ambient are the renderer's frame description. One struct, two owners. |
| `Lookup`, `NewLookup`, `NewSizedLookup`, `LookupAccess`, `NewLookupAccess`, `LookupDeviceAccess`, `NewLookupDeviceAccess` (+39 methods, +19 `friends.go` accessors) | one resource holding the model cache, the texture cache, the mesh table, the staging arena, the unit meshes and the deferred bake/release queues. The first four are `model`'s; the mesh table is named by both renderers through `MeshRef`; staging and the unit meshes serve the declarative renderer's per-frame shapes. |
| `ShaderVariant`, `VariantStatic`, `variantSkin`, `variantMorph`, `variantSkinMorph`, `VariantCount`, `VariantFor` (+`shader`) | the variant is *selected* from model facts — does this primitive skin, does it morph — but it *names a pipeline*, which is the renderer's. `ecsscene` recording to `gfx` itself would have to select the same variant from the same facts. |
| `MeshSource`, `MeshNone`, `MeshDurable`, `MeshTemporary` | durable-vs-temporary is a residency fact (`model/cache`), but "temporary" exists only because the declarative queue mints a mesh for one frame. A renderer that does not do that does not need the distinction. |
| `overrideRecord` | a renderer act (merge this draw's params) that writes a model record (`ScenePbrRecord`) in place. Either the record moves with a merge function the renderer calls, or the merge stays with the renderer and the record's field layout becomes its dependency. |
| `AnimBinding` | assembled per batch at record time out of the model's `SkinBuffers`, a frame-local offset and an index into the frame's morph arena. Half model residency, half frame bookkeeping. |

Three more that are not marked `unclear` but should be read carefully before the decision,
because the reading here is contestable:

- **`Material`, `MaterialTag`, `MaterialKey`, `MaterialKeyOf`** — called renderer-side
  above, because the tag is a `PassTag`. If `#494` decides the other way, the whole
  `material.go` block moves and `ScenePbrRecord`'s escape into the renderer goes with it.
- **`Vertex`** — called `model`, because the packer consumes it. But it is scene's
  *authoring* struct, not glTF's; nothing in the decoder produces one. If authoring a mesh
  is a renderer service, `Vertex` is the renderer's and the packer takes an interface.
- **`Config`/`WithDefaults`** — called `model`, because its one field is the animation
  bake rate. If the renderer ever gains configuration of its own, this splits.

---

## Reproducing this

The extraction and cross-reference tools are throwaway; they live in the session
scratchpad, not in the repo. They are ~200 lines of `go/ast` plus one `awk` pass, and the
method is written out in *How it was produced* above. The `ecs.Storable` probe was a
temporary `_test.go` inside the module, run once and deleted.
