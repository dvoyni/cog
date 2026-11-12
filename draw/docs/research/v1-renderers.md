# v1's renderers: what they offered, and how a game reached each

Research for [#801](https://github.com/dvoyni/cog/issues/801), under the map
[draw](https://github.com/dvoyni/cog/issues/800). It is the inventory the
feature list starts from. It records facts and v1's own judgements, and
decides nothing for draw.

**Sources.** v1's docs, specs and code under `_legacy/`, and the maps
[#443](https://github.com/dvoyni/cog/issues/443),
[#374](https://github.com/dvoyni/cog/issues/374),
[#450](https://github.com/dvoyni/cog/issues/450) and
[#188](https://github.com/dvoyni/cog/issues/188) with their comments. A path
with no module prefix is relative to `_legacy/`; `L` is a line in the file
named. I read the READMEs in full and the specs by section, and skimmed the
code to confirm the API surfaces and constants the docs name. Where docs and
code disagree, the code wins; §6 lists each case and what I could not verify.

**What `_legacy` holds.** Three reshapes landed before v1 was frozen, and
older docs still speak of what they replaced:

- The recording renderer once called `scene` (a frame-local `OpQueue` of
  `CameraDescr`, `ModelDraw` and `MeshDraw`) was removed by
  [#573](https://github.com/dvoyni/cog/issues/573). `bundles/scene` is now the
  ECS binding, which took the name (`bundles/scene/docs/specs/scene.md`
  L11-17). No `scene.OpQueue` exists in the code. Its sorting, culling,
  layering and batching live on in the ECS scene's recording System.
- gfx's materials (`MaterialDescr`, `FrameMaterial`) were replaced by
  explicit shaders and durable draw params
  ([#595](https://github.com/dvoyni/cog/issues/595)-#604,
  `slots/gfx/docs/specs/drawparams.md`).
- `libs/assets` was built, and gfx, scene (since then model) and canvas
  moved onto it ([#443](https://github.com/dvoyni/cog/issues/443)).

The games that drew through v1 were feuds-26, nox and cog-examples
(drawparams.md L36-37).

---

## 1. How a game reached anything

| Route | What it looks like | Used for |
|---|---|---|
| Lock a resource in a handler | `access.GetWrite[*canvas.OpQueue]()` in a subscription's `Lock`; `*gfx.OpQueue`, `*gfx.ResourceQueue`, `*gfx.Viewport`, `*canvas.Lookup`, `*model.Lookup`, `*anim.Timelines` | recording draws, creating GPU resources |
| ECS System parameters | `*ecs.Write[*gfx.OpQueue]`, `*ecs.Write[*model.Lookup]`, `*ecs.Read[storage.FileSystem]` | scene's own Systems, and a game System that bakes a mesh |
| Spawn Components | `scene.Model{Ref: model.ModelRef{Path: "models/crate.glb"}}` beside `m.Transform` | everything 3D: models, meshes, lights, cameras, debug shapes |
| A facade over a resource | `canvas.NewLookupAccess(k, lookup, fsys)`, `canvas.NewLookupDeviceAccess(k, lookup, resources)`, `model.NewLookupAccess(k, lookup)`, `model.NewLookupDeviceAccess(k, lookup, fsys, resources, compile)` | measuring, loading, querying, unloading |
| Commands | `gfx.CompileShaderCmd`, `ArmCaptureCmd`, `ArmFrameCmd`, `SetDesiredViewportCmd`, `ReleaseCachedResourceCmd`, `FreeCachedResourcesCmd`, `canvas.SetFallbackLanguageCmd`, `ClearGlyphsCmd` | one-shot requests |
| Ordering identities | `Before[gfx.PresentOnUpdate]`, `Before[canvas.FlushOnUpdate]`, `Before[scene.LoadOnUpdate]`, `Before[scene.RecordOnUpdate]` | landing in this tick's frame |
| MCP tools | `gfx_capture`, `gfx_frame`, `canvas_draws` | an agent |

Sources: `bundles/canvas/docs/README.md` L153-156, L708-748, L774-781;
`bundles/scene/docs/README.md` L192-264, L318-324; `slots/gfx/docs/README.md`
L383-402, L414-417; `bundles/model/docs/specs/model.md` L2132-2140.

The README of gfx is `slots/gfx/docs/README.md`, canvas's
`bundles/canvas/docs/README.md`, scene's `bundles/scene/docs/README.md`; below
they are "gfx README", "canvas README" and "scene README", and `model.md`,
`mesh.md`, `scene.md`, `assets.md`, `drawparams.md` and `materials.md` are the
specs under the same bundles.

---

## 2. Feature inventory

### 2.1 Frame, passes and targets (gfx)

| Feature | Where it lived | How a game reached it | Source |
|---|---|---|---|
| Frame queues | Recorders fill a frame-local `*gfx.OpQueue` on update. gfx rotates queues through a latest-wins triple buffer and translates the latest one on the render thread into a backend `gfx.Queue`. | Write `*gfx.OpQueue` in a handler ordered before `gfx.PresentOnUpdate` | gfx README L3-6, L428-440 |
| Passes | `OpQueue.NewPass(PassDescr{Order, Target, Depth, Load, Clear, Store, DepthLoad, DepthClear, DepthStore, Label})` returns a `PassId` valid for the frame. Every draw names its pass, and there is no default pass. | Call `NewPass`; hand the `PassId` to other Systems | gfx README L170-183; `slots/gfx/internal/types/passdescr.go` L17-26 |
| Pass order and merging | Passes run by `Order`, ties broken by declaration. Adjacent passes on the same attachments merge into one GPU pass. A pass runs only if it draws or loads. | Implicit | gfx README L184-193 |
| Targets | `TargetDescrScreen()`, `TargetDescrTexture(tex, mip, layer)`, `TargetDescrNone()` (depth-only) | `PassDescr.Target` | gfx README L179-181 |
| Depth | `DepthDescrAuto()` (one texture shared by every pass at one size), `DepthDescrNone()`, `DepthDescrTarget(tex)` | `PassDescr.Depth` | gfx README L180-181, L198-200 |
| Frame buffer and present pass | "Screen" is a frame-sized `FormatRGBA8` buffer, put on the surface by an implicit full-screen present pass. It is allocated only when something renders to the screen. | Implicit | gfx README L234-254 |
| Logical viewport | `*gfx.Viewport`: logical, window (DIP) and framebuffer sizes. Modes `ViewportWindow`, `ViewportFixedWidth`, `ViewportFixedHeight`, `ViewportFit`, `ViewportCover`. | Read `*gfx.Viewport`; `SetDesiredViewportCmd` | gfx README L442-456 |
| Barriers | gfx computes the frame's write-then-read pairs and emits the transitions. | Implicit | canvas README L619-621; `extensions/gogpu/docs/README.md` L278-292 |
| Absent | No per-pass viewport or scissor, no multiple render targets, no MSAA. | — | gfx README L201-202 |

### 2.2 GPU resources (gfx)

| Feature | Where it lived | How a game reached it | Source |
|---|---|---|---|
| Durable buffers | `ResourceQueue.NewBuffer`, `UploadBuffer` (the whole buffer, any length, same id), `ReleaseBuffer` | Write `*gfx.ResourceQueue` | gfx README L204-218 |
| Durable textures | `NewTexture(w, h, layers, format, mipmaps)`, `UploadTexture(tex, layer, region, pixels, copyData)`, `ReleaseTexture`. A whole-layer upload rebuilds the mip chain with a box filter in the format's colour space. | Same | gfx README L220-224, L816-819 |
| Durable render targets | `NewRenderTarget(w, h, layers, format)`. It is a separate call because the attachment usage is not free. | Same | gfx README L223-228 |
| Per-frame temporaries | `OpQueue.NewTemporaryBuffer`, `NewTemporaryTexture`, `NewTemporaryTarget` (returns the target and the texture) | Write `*gfx.OpQueue` | gfx README L117; `slots/gfx/internal/opqueue.go` L269-363 |
| Texture by path | `gfx.TextureWithResource(path)`: PNG or JPEG, decoded on the render thread, always `FormatRGBA8Srgb`, no mips, cached by path. It draws white until it resolves. | Name it in a param | gfx README L463-471, L563-571 |
| Inline data | `TextureWithBytes` and `BufferWithBytes` carry their bytes in the descriptor as an `assets.Blob`, and gfx bakes them. | In a param or a `MeshDescr` | gfx README L460-468, L508-513 |
| Formats | `FormatRGBA8`, `FormatRGBA8Srgb`, `FormatDepth32F`. Views `TextureView2D` and `TextureView2DArray`. | — | `slots/gfx/internal/types/textureformat.go` L14-20; gfx README L810-814 |
| Samplers | `SamplerDesc` by value: per-axis address (clamp, repeat, mirror), mag, min and mip filters, anisotropy, comparison. The backend dedupes equal descriptors. | `ShaderParameterSampler(name, desc)` | gfx README L642-647 |
| Meshes | `MeshDescr{Vertices, Indices, IndexWidth, Topology, Layout, Stride, VertexCount, IndexCount}`, `IndexUint16` or `IndexUint32`, 32 vertex formats | `MeshDescrWithVertices`, `MeshDescrWithIndices` | gfx README L478-489, L649-654 |
| Topologies | `TriangleList`, `TriangleStrip`, `LineList`; no point list | — | gfx README L635-636 |

### 2.3 Shaders, draw params and pipeline state (gfx)

| Feature | Where it lived | How a game reached it | Source |
|---|---|---|---|
| WGSL preprocessor | `#include`, `#define`, `#const`, `#if`; include-once by resolved path. The supply (defines and consts) is part of a shader's identity. | `gfx.ShaderWithResource(path)` or `ShaderWithText`, plus a supply | gfx README L13-18, L472-477; `slots/gfx/docs/specs/preprocessor.md` |
| CPU compile and reflection | `CompileShaderCmd{FS, Descr}` flattens the shader, reflects it through the backend's `ReflectShader`, and answers a `ShaderProgram` or `Err`. The command's lock is empty. | Dispatch it, or take a `gfx.ShaderCompiler` func | gfx README L132-142 |
| Durable shaders | `ResourceQueue.NewShader()` reserves an id, then `UploadProgram`; `ReleaseShader` frees the module and its pipelines. Reloading means release and recreate. | Write `*gfx.ResourceQueue` | gfx README L138-142 |
| Draw params (a "set") | `NewDrawParams(k, shader, DrawState, params...)` resolves each param against the program by binding name, once. It returns a `DrawStateId`: a `uint32`, comparable, pointer-free, and the complete batch key. Also `UpdateDrawParams` and `ReleaseDrawParams`. | Same | gfx README L127-149, L490-491 |
| The frame's version of a set | `OpQueue.SetDrawParams(k, set, params...)`: values for the rest of this frame, copy-on-write per binding | Write `*gfx.OpQueue` | gfx README L155-160 |
| Param kinds | Whole bindings only: `Float`, `Vec4`, `Mat4`, `Color`, `Raw[T]` (WGSL layout checked once per type, panics on a mismatch), `RawRef[T]` (borrowed above 64 B), `Texture`, `Sampler`, `Buffer`, `BufferRange` | `gfx.ShaderParameter*` constructors | gfx README L150-154, L496-543 |
| Defaults for an unfilled binding | Sampler: clamp and linear. Texture: 1x1 white, with 2D and 2D-array views. Storage buffer: the draw is dropped and reported once. | Implicit | gfx README L162-168, L556-606 |
| Draw state | `DrawState{Blend, DepthCompare, DepthWrite, Cull, FrontFace}`, fixed on a set. Blends: alpha, opaque, additive, multiply. Presets `StateOpaque3D()`, `StateTransparent3D()`, `StateOverlay2D()` (the zero value). | Argument to `NewDrawParams` | gfx README L626-640 |
| Pipelines | Built and cached by the translator per shader, state, vertex layout and target format. Freed by `FreeCachedResourcesCmd` and rebuilt on demand. | Implicit | gfx README L396-402, L781-791 |
| Validation | Exact vertex interface (`(kind, count)` equal), stride a multiple of 4, every shader checked against WebGPU's default limits (never the device's), param kind and uniform size checked when a set is made, index buffer length against its width, texture view dimension. Each drops the draw and reports once. | Implicit; `gfx.CheckVertexInterface` is exported | gfx README L544-554, L615-621, L656-701, L871-898 |
| Instanced draw | `OpQueue.Draw(pass, mesh, set, instances, firstInstance)`. `firstInstance` lets a batch read its own slice of a shared instance arena. `ShaderParameterBufferRange` binds one record of an arena, with storage offsets 256-aligned. | Write `*gfx.OpQueue` | gfx README L117-123, L609-613 |

### 2.4 2D: canvas, with ui on it

| Feature | Where it lived | How a game reached it | Source |
|---|---|---|---|
| Layers | A `Layer` is a `gfx.Order`. Each layer that draws or clears gets one gfx pass, and contiguous layers on one target merge. | Every canvas call names a layer | canvas README L189-194 |
| Layer world space | `SetLayerTransform(layer, window m.Rect, AspectMode)` (`AspectInscribe`, `AspectOverlap`, `AspectStretch`), with `LayerTransform`, `WorldToScreen`, `ScreenToWorld` | Write `*canvas.OpQueue` | canvas README L162-163, L701-706 |
| Clear | `Clear(layer, color)`, per layer and positioned | Same | canvas README L158-161 |
| Sprites | `Sprite(layer, path, SpriteTransform, *Material, params...)`: position, size, scale, rotation, origin, a `Frame` sub-rectangle, flips, tiling, filter, nine-slice | Same | canvas README L170-205 |
| Texture-sourced quads | `SpriteTexture(layer, gfx.TextureDescr, ...)`, `DrawTexture[TVertex](layer, tex, verts, ...)` | Same | canvas README L173-176 |
| Shapes | `FillRect`, `StrokeRect`, `Line` with `ShapeDraw{Color, Thickness, Material, Params}`, drawn as sprite instances over a white atlas texel | Same | canvas README L177-178, L212-219 |
| Custom triangles | `DrawTriangles[TVertex VertexLayout]`: non-indexed, over any pointer-free vertex struct | Same | canvas README L185, L227-230 |
| Text | `Text(layer, fontPath, text, TextDraw)`: TTF and OTF through `x/image/font/opentype`, a glyph atlas, multiline, wrapping, alignment, inline icons `${path}`, language spans `$[ja]{...}`. A fallback chain by language (`Config.FallbackFonts`, `SetFallbackLanguageCmd`, `ClearGlyphsCmd`), and a built-in font (JetBrains Mono). | Same; measuring through `LookupAccess.FontMetrics`, `MeasureTextSize`, `MeasureWrappedTextSize` | canvas README L90-129, L179-184, L734-739 |
| Sprite atlas | A `texture_2d_array` that canvas packs: `Config{AtlasSize: 4096, LayersPerArray: 2, MaxAtlasBytes: 256 MiB}` by default. Oversized tiled sprites become standalone textures. | `canvas.Config` | canvas README L82-88, L141-146 |
| Clip | `SetClip(rect)` and `RemoveClip()`: a test in `fs_main`, with no scissor | Write `*canvas.OpQueue` | canvas README L166-167, L531-535 |
| Materials | `canvas.Material` (shader descriptor, `DrawState`, its own params), passed by address. Three families (sprite, triangles, texture), each with a built-in default. `MaterialSet` scopes a layer (`SetLayerMaterial`) or the queue (`SetMaterial`). A draw's material resolves draw, then layer, then built-in. Each is compiled and made into a set on its first batch. | `canvas.NewMaterial`, `MaterialWithState` | canvas README L232-309 |
| Param frequency | A material's params: one uniform per batch. A sprite draw's params: one storage array per name at group 2, indexed by instance. A triangles draw's params: per material. Per vertex: the caller's `TVertex`. | — | canvas README L386-422 |
| Published WGSL | Eight includable sources (`uniforms`, `clip`, `spritebindings`, `spritevertex`, `trianglesbindings`, `trianglesvertex`, `tint`, `haloband`), mounted at `math.MaxInt` priority. An app replaces one by mounting its own. | `//#include builtin/canvas/...` | canvas README L450-484, L580-583 |
| Tint | `tint` lands in the 96 B `SpriteInstance`, on a ramp: `lerp(0, tint/0.153, texel)` per channel. `DefaultTint()`, `TintFor(color)`. | `TextDraw.Color`, `ShapeDraw.Color`, the `tint` param | canvas README L433-448 |
| Halo | `HaloMaterialSet(HaloProfile{Reach, Plateau, Exponent})` over a dedicated layer. The caller records the marks twice, band layer then ink layer. | `SetLayerMaterial` | canvas README L311-384; [#188](https://github.com/dvoyni/cog/issues/188) |
| Render to texture | `SetLayerTarget(layer, gfx.TargetDescr)`, the target from `NewTemporaryTarget` or `NewRenderTarget`. The texture is drawn with `SpriteTexture` or `DrawTexture`, or sampled by a `scene.Material`. | gfx handles, passed through canvas untouched | canvas README L586-634 |
| ui | Immediate-mode layout that records its visuals into canvas's queue, and passes a `MaterialSet` down the element tree | ui's resources | `bundles/ui/docs/README.md` L1-6; canvas README L307-309 |

### 2.5 3D: model and scene

| Feature | Where it lived | How a game reached it | Source |
|---|---|---|---|
| glTF 2.0 | GLB and `.gltf` with external buffers and images, sparse accessors, 8, 16 and 32-bit indices, the node tree, skins, morph targets, animations (STEP, LINEAR and CUBICSPLINE, baked at load), the full metallic-roughness set with two UV sets. Extensions `KHR_texture_transform`, `KHR_materials_emissive_strength`, `KHR_mesh_quantization`, and `KHR_lights_punctual` as data only. Draco, meshopt, basisu and webp are rejected. Parsed with `qmuntal/gltf`. Strips and fans become lists at load; points are skipped and reported. | `scene.Model{Ref: model.ModelRef{Path, Scene, Node}, Layers}` | model.md L968-1029 |
| Addressing | The path is the only cache key. `Scene` and `Node` select by name, and a `Node` draw re-roots its subtree under the Entity's `m.Transform`. | `ModelRef` fields | model.md L1031-1077 |
| Buffer-built meshes | `LookupAccess.BakeMesh[TVertex]` returns a `MeshRef` at once and stages the upload. `UpdateMesh` re-bakes at any size, and `ReleaseMesh` frees at the frame boundary. A custom vertex layout needs a custom material. | `scene.Mesh{Ref, Bounds, Layers, NeverCull}` | model.md L1296-1395; scene README L125-130 |
| Unit geometry | Box, plane and sphere generators | model's internals; the debug shapes | model.md L232-237 |
| Vertex storage | A 32 B standard layout and a 40 B skinned one: oct-encoded normal, packed tangent, `Unorm16x2` UVs with a per-mesh range, `Unorm8x4` colour, `Uint8x4` joints. `uint16` indices up to 65,535 vertices, and sparse morph deltas. | Implicit | mesh.md L207-290, L772-883, L884-1066 |
| Bundled PBR | The Khronos reference BRDF, with glTF's parameter names verbatim (`baseColorFactor`, ...). Five textures and five samplers. 1x1 white and flat-normal defaults, and magenta for a colour slot the file named and could not fill. Alpha OPAQUE, MASK and BLEND; double-sided; a negative determinant gets `FrontCW`. An analytic ambient specular term, and no IBL. | The default for every `Model` and `Mesh` | model.md L1932-2068, L1240-1250 |
| Shader variants | Static, skin, morph and skin+morph, selected by `SCENE_SKIN` and `SCENE_MORPH`. The engine chooses the variant, whatever shader is in effect. | Implicit | model.md L218; scene README L399-401 |
| Material over the file | `scene.Material{Tags: m.List[MaterialTag{Tag, Shader, State, Params}]}`: per pass tag, the tag's shader (or the default scene shader) over each primitive's own textures, numbers and state | A Component | scene README L361-416 |
| Per-Entity params | `scene.Params{Values: m.List[gfx.ShaderParameterDescr]}`. A param named for a member of `scenePbrMaterial` is written into the block, the one member-level write. | A Component, and part of the Batch key | scene README L177, L370, L380-384 |
| Default scene shader | `LookupAccess.SetDefaultSceneShader(model.SceneShaderDescr{Source, Params})` replaces the bundled PBR for every draw that names no shader, and its params ride on every draw. | model's facade | scene README L410-416 |
| Custom shaders | `//#include builtin/model/vertexstage.wgsl` and `fragmentstage.wgsl`, the shader's own bindings in group 3, its own `fs_main` around `scenePbrFragment`. Five sources are published. | The shader of a `MaterialTag` | model.md L2650-2747 |
| Bind group convention | Group 0 per pass (frame, instances, animation), 1 per material, 2 per model (poses, morph deltas), 3 the app's own | A WGSL contract | model.md L2344-2362 |
| Lights | Per camera, a sun and a hemispheric ambient. Up to 16 point and spot lights (`model.MaxLights`), the rest dropped silently by their contribution at the eye. A naive forward loop: every fragment loops every light. | `scene.Light{Descr: model.LightDescr, Layers}`, and the camera's fields | model.md L1794-1930; `bundles/model/internal/light.go` L65 |
| Shadows | None, and nothing is reserved for them | — | model.md L1921-1925 |
| Cameras | `scene.Camera{ID, Projection, FovY, Height, Shear, Near, Far, CullMask, SunDirection, SunColor, SunIntensity, AmbientSky, AmbientGround, AmbientIntensity, Passes}`. Projections: `Perspective`, `Orthographic`, `Oblique`. | A Component beside `m.Transform` | scene README L148-180; `bundles/scene/internal/camera.go` |
| Camera passes | `Pass{Tag, Target, Depth, ClearColor, ClearDepth, Order}`, where `Order` is an offset from the camera's id. Each is labelled `scene.camera<ID>.<tag>`, and a material serves a pass by its tag. | `Camera.Passes` | `bundles/scene/internal/camera.go`; scene README L326-327 |
| Layers | A 32-bit `LayerMask` on models, meshes, lights and debug shapes, and a camera's `CullMask` | Component fields | `bundles/scene/internal/camera.go`; scene README L119-146 |
| Culling | Per camera and pass: the layer mask, then the frustum against each instance's world sphere. `NeverCull` opts out. | Implicit | scene README L296-305 |
| Sorting | Opaque draws by material and Batch. Blended draws back to front, one draw per instance. | Implicit | scene README L306-308, L351-352 |
| Batching | The key is (model handle, primitive, material key, `Params` hash), or (`MeshRef`, material key, `Params` hash), computed on change by the load System. One instanced draw per opaque Batch per pass, always on. | Implicit | scene README L232-241, L336-359 |
| Debug shapes | `DebugBox`, `DebugSphere`, `DebugPlane`, `DebugLine`, `DebugWireBox`: self-lit, drawn as ordinary meshes | Components | scene README L448-495 |
| Model queries | `State`, `Preload`, `Nodes`, `Bounds`, `AABB`, `ModelLights`, `Joints`, `Clips`, `MorphTargets`, `PoseBytes`, `MorphBytes`. Every query loads. | `model.NewLookupDeviceAccess` | model.md L2132-2200 |

### 2.6 Animation

| Feature | Where it lived | How a game reached it | Source |
|---|---|---|---|
| Skeletal | Poses baked at load on a 60 Hz grid as 48 B TRS records; skinning in the vertex shader. Caps: 4 influences, 4 plays, 256 joints. No compute shaders. | `scene.Animation{Plays [4]model.ClipPlay}` | model.md L1414-1560 |
| Clip plays | `ClipPlay{Clip, Time, Weight, Loop}`. Stateless: the game owns time. | The `Animation` Component | model.md L1532-1560 |
| Clip machine | `model.NewClipMachine(clips, states, transitions)`, then `Fire`, `Step(dt)`, `PlaysInto(&animation.Plays)`: crossfades with easing, in a plain value the game keeps | Game code | model.md L1614-1640 |
| Morph targets | Weights blended on the CPU from the plays, sparse deltas applied in the vertex shader | Implicit, from the file's `weights` channels | model.md L1719-1792 |
| anim timelines | `bundles/anim`: eased value tracks, one-tick cues and flipbooks in a `*anim.Timelines` resource that the plugin advances each tick. It produces values and draws nothing. | Lock `*anim.Timelines`, read a value, draw with it | `bundles/anim/docs/README.md` L1-169 |

### 2.7 Inspection and tooling

| Feature | Where it lived | How a game reached it | Source |
|---|---|---|---|
| Capture | `ArmCaptureCmd{Target, Amount, Interval, Paused}`, where the target is the screen or any renderable texture. A capture binds to a tick that began after the request, arrives a frame later, and bursts go up to 60 stills. | Dispatch it; `gfx.Capture.Image()` | gfx README L256-313 |
| Agent tools | `gfx_capture` writes a PNG of the screen. `gfx_frame` lists every pass, draw and instance counts per set, every resource op, and draw params views. `canvas_draws` lists one tick's canvas ops in flush order. | MCP | gfx README L315-360; canvas README L658-699 |
| Recording inspection | `canvas.OpQueue.Ops`, `LayerWindow`, `LayerTarget`, `LayerClear` | Tests | canvas README L636-656 |

---

## 3. Assets: named, loaded, cached, released, and referred to

### 3.1 The Library under every cache

`libs/assets` (`libs/assets/docs/README.md` L27-61, L241-321):

- **`Descr[P]{Name, Blob, Params}` is the key, never the handle.** The key
  is `{Name, Params}` when `Name` is set, and `{Blob, Params}` otherwise.
  `Params` is a typed, comparable value for bake parameters (font size,
  colour space, shader supply).
- **`Blob` is a pointer and a length.** Its identity is the run of bytes,
  not their contents, so a Blob used as a key must be built once and kept.
- **`Loader[P, U, T]` is stateless.** It has `Load(k, data, params, fsys,
  user)`, `Default(d, user)` and `Free(value, user)`. The kernel, the
  filesystem and the GPU queue arrive per call.
- **`Cache.Get` loads on a miss and caches the result, whatever it is.**
  `Free`, `FreeAll` and `FreeWhere` free at the call, with no queue.
- **Failure is terminal and reported once,** by the Library, keyed by the
  `Descr`. `Free` is the retry lever.
- **What it does not do:** it has no probe, no walk, no refcount, no budget
  and no file watching, and it does not know what a frame is.
- **It is plugin-internal.** A game cannot tell the plugins share it.

### 3.2 Per asset

| Asset | Named by | Loaded where and when | Cached in | On failure | Released by | A draw referred to it by |
|---|---|---|---|---|---|---|
| gfx path texture | `TextureWithResource(path)`, which becomes `Descr{Name: path}`. sRGB is fixed, so the path alone is the key. | On the render thread, while translating the first frame that names it | The translator's `assets.Cache` (`slots/gfx/internal/translator.go` L112) | A failed read is reported once. A file that opens but fails `image.Decode` is cached silently (#443). Unresolved, it draws white. | `ReleaseCachedResourceCmd{Path}`, `FreeCachedResourcesCmd` | A `ShaderParameterTexture` in a set or a version |
| gfx durable texture or buffer | An id inside `TextureDescr` or `BufferDescr`, from `NewTexture`, `NewBuffer` or `NewRenderTarget`. Ids come from monotonic counters and are never reused. | At `New*` and `Upload*`, replayed on the render thread | None: the caller owns it | — | `ReleaseTexture`, `ReleaseBuffer` | The descriptor carrying the id, in a param or a `MeshDescr` |
| gfx shader | `ShaderDescr`: a path or inline text, plus the supply. Its identity is (root, supply). | `CompileShaderCmd` on the caller's goroutine, then `UploadProgram` | Not by gfx since #604: the caller keeps the `ShaderID` | `CompileShaderResponse.Err`, returned and never reported | `ReleaseShader` | A set's shader |
| gfx set | `DrawStateId` | `NewDrawParams` | The caller | Bad params are reported once and ignored | `ReleaseDrawParams` | `Draw(pass, mesh, set, ...)` |
| canvas sprite | A normalized path string | At the flush for a draw (into the atlas), or at a measure (the header only) | Three caches: atlas entries, tiled standalone textures, sizes | Terminal, once per cache | `UnloadSprite`, `UnloadAll` | The path, per op |
| canvas font | A path, plus the pixel size for a face | At the flush or a measure | Font sources and faces | Terminal | `UnloadFont`, `UnloadAll`. Glyph pages go only on a framebuffer scale change. | The path, in `Text` |
| canvas material | A `*canvas.Material` value, keyed by content fingerprint and binding shape | Shader compiled and set made in the flush, on the first batch | canvas's set drawer | A shader that fails is reported once, cached as failed, and draws nothing | Nothing found: canvas calls no `ReleaseDrawParams` | The pointer argument, or a `MaterialSet` |
| model | `ModelRef{Path, Scene, Node}`. The path is the key, and resolves to a `ModelHandle`: a slot index with no generation. | Synchronously inside `Lookup.Resolve`, which scene's load System calls when a Component changes (`bundles/scene/internal/keyer.go` L101); also on `Preload` or any query | An `assets.Cache` of `*residentModel` (`bundles/model/internal/lookup.go` L50) | Reported once; `State(path)` returns why. It draws nothing, never a placeholder. | `UnloadModel` (does not cascade to textures), `UnloadAll` | The `scene.Model` Component, which the load System keys |
| model texture | `{path, image index, sRGB}`. An embedded image is named by its model's path and index, its bytes riding as the Blob. | During the model's load, checked before the read | A second `assets.Cache` (`bundles/model/internal/lookup.go` L51) | Magenta in a colour slot, the default in a data slot | `UnloadTexture(path)`, every variant through `FreeWhere` | Bound into the material's set at load |
| model shaders | The four variant programs | Compiled once per Lookup, on the first load that has a compiler | The Lookup | A variant that fails is reported once, and its sets stay zero | Never | Through a material's set |
| model material sets | One per material and variant | At load | The model's entry | — | At the frame boundary after `UnloadModel` | `ModelView.Materials[i].Sets[variant]` |
| scene sets | Material key, variant, and the binding shape of the `Params` | In the load System, the first time a drawable is keyed | The load System's scratch, held once per primitive or mesh drawing through it | `scene.ErrMaterialShaderUnavailable`, once | With the last hold; a new default scene shader releases them all | The Batch's draw |
| mesh | A `MeshRef` from `BakeMesh`, counted by generation | Staged, then drained by the load System | model's mesh table | Invalid input is reported once and yields a zero ref | `ReleaseMesh`, at the frame boundary | The `scene.Mesh` Component |

Sources: assets.md L632-1095; gfx README L132-149, L204-232, L389-402,
L463-471; canvas README L232-262, L708-770; model.md L478-541, L1193-1294,
L2132-2200; drawparams.md L513-575; #443's comments.

### 3.3 What held across them

- **Nothing refcounts but scene's sets.** The Library does not refcount
  (assets README L303). `UnloadModel` does not cascade to textures, because
  with no refcount it cannot know whether another model shares them by path
  (model.md L1283-1287). scene's sets are held once per primitive or mesh and
  released with the last hold (drawparams.md L560-561).
- **Unloading is cleanup between scenes.** Drawing an unloaded model is
  undefined, and there are no generations and no invalidation (model.md
  L46-48). A mesh ref is the exception: it is generation-counted, so a stale
  one skips and reports (model.md L1380-1384).
- **Loading is synchronous, and the hitch is stated.** A large glTF costs
  the frame that first names it several hundred milliseconds, and `Preload`
  moves the cost into a loading screen (model.md L1199-1210).
- **The asset filesystem is static, so nothing watches it.** A failed load
  stays failed until a `Free` (assets.md L1166-1185).
- **The loading lock is kept off Systems that do not load.** canvas and model
  each split a read-only facade from a device facade (canvas README
  L710-725; assets.md L987-1006).
- **Descriptors and Components were made storable.** `BufferDescr`,
  `TextureDescr` and `ShaderParameterDescr` may sit in an ECS Component,
  their bytes as `assets.Blob` (gfx README L508-513). `Mesh` and `Light` are
  pointer-free. `Model`, `Params` and `Material` hold a string, an `m.List`
  or a Blob, and give up the ECS's fast path for their own Store (scene
  README L189-190).

---

## 4. What went wrong or cost too much

| What | Where | Cost recorded | What v1 did | Source |
|---|---|---|---|---|
| A material per draw | gfx | The queue copied a draw's params every draw, and the translator hashed their names and looked each binding up by name in two lists. `FrameMaterial` and `FrameRecording` existed only to claw that back, and each bundle keyed batches on its own fingerprint. | Explicit shaders and durable sets (#595-#604) | drawparams.md L1-13, L626-640 |
| Re-keying a model's own material every draw | the recording scene | 20% of the per-Entity frame, 55% of the instanced one | The key is taken once, at load | model.md L141-176 |
| One call per Entity | scene's proxy into the recording renderer | 5,000 crates were 5,000 draws at 14.2 ms a frame; emission was 42.4% of it | Batches keyed on change: 1.1 ms, 12.8x faster, allocations no longer scale with the population | scene.md L66-86, L485-505 |
| (Not a cost) sort, cull, layering | — | Under 0.4% of the frame together | — | model.md L157 |
| Caches failing silently | gfx textures, canvas atlas | Three of four caches failed silently. gfx re-decoded a missing texture every frame, forever, and canvas cleared its failures every frame. | `libs/assets`: terminal failure, reported once | assets.md L17-19, L632-640, L933-940 |
| An asynchronous load state machine | scene's model table | About 300 lines (`Missing` → `Loading` → `Resident` → `Failed`, generations, a release queue) | Deleted; loading made synchronous | assets.md L788-800; #443 |
| A hot-reload loop that never existed | gfx docs | Four sentences describing a developer loop, backed by a test that called `ReleaseCachedResourceCmd` by hand | Deleted | assets.md L1195-1210 |
| Mesh storage | scene | A fixed 84 B stride and `uint32` indices: 3.77 MiB of geometry over the corpus | Narrowed to 1.325 MiB (35.1%). Fidelity was judged by eye only, since there was no pixel readback. | mesh.md L11-16, L94-101, L1278-1300 |
| Silent shader and binding mismatches | gfx | A narrower vertex format shaded wrongly with no error. An unsupplied storage binding drew the whole frame wrong. A param of the wrong kind bound the wrong descriptor. | The exact vertex check, `ErrStorageBufferUnsupplied`, `ErrParameterKindMismatch` | gfx README L544-554, L588-606, L656-667; materials.md L10-17 |
| A gap in the bind groups | gogpu on Vulkan | A group 3 above a variant with no group 2 failed every frame | The backend binds an empty group at each gap | drawparams.md L836-842 |
| The storage buffer budget | model's animated variant | 7 of the 8 storage buffers the WebGPU floor allows per stage, leaving one for an app shader | Shadows were blocked on freeing a slot (#52) | model.md L2718; #52 |
| No sRGB swapchain | gogpu | gogpu hardcodes `BGRA8Unorm`, and `bgra8unorm-srgb` is illegal on the web: a frame buffer of about 8 MiB at 1080p plus a full-screen pass | Accepted; the present pass is "where post-processing hangs later" | gfx README L242-247 |
| No barriers from gogpu | gogpu on Vulkan | A pass reading what an earlier pass rendered flickers in tiles | gfx and gogpu place the transitions themselves | gogpu README L278-292 |
| `CopySrc` on every renderable texture | gfx, gogpu | On some drivers it turns off lossless framebuffer compression, paid whether or not a capture happens | Accepted and stated | gfx README L309-313 |
| Re-rendered frames naming released ids | gfx | A released mesh drew with whatever was bound earlier in the pass | A liveness check; such draws drop silently | drawparams.md L105-111; gfx README L166-168 |
| Lights | model | Every light costs every shaded pixel. The cap is 16 with a silent drop, and ties go by record order. | "The answer to 'I need 40 lights' is clustered lighting" | model.md L1798-1800, L1880-1914 |
| Shadows never landed | scene | Depth-only passes did not execute on the native Vulkan HAL (gogpu/wgpu#353), and a depth pass's output could not be depended on in the same frame | #52 closed as not planned | #52, comments |
| Clip as a discard | canvas | A custom `fs_main` that skips `canvasClipped` draws outside the clip, with no error | A documented warning | canvas README L531-535 |
| A frozen instance record | canvas | The 96 B `SpriteInstance` is mirrored by hand, so a divergence is a silent misread, and there is no API to add a per-sprite member | Stated as a wall (#148) | canvas README L545-548; materials.md L991-1010 |
| A version carrying bindings forward | canvas, scene | A version keeps what the next `SetDrawParams` does not name, so one batch's texture leaked into the next, untextured one | One set per material and binding shape | drawparams.md L576-590 |
| A material keyed by its values | canvas | A material rebuilt each frame at a changing value is a new set every frame, never released | A rule: a changing value goes on the draw or the scope | canvas README L258-262 |
| The halo's two-layer idiom in a game | canvas, feuds | Three reaches needed three halo layers, and a haloed composite clashed with painter's-order depth sorting | It owns its whole stack of layers | #188, comments |
| No per-instance properties | scene | 5,000 crates tinted 5,000 ways are 5,000 draws | #520 closed as not planned | scene README L342-345 |
| A material hashed every draw | the recording scene's `OpQueue.Mesh` | `Fingerprint` was 48% of a shared-material frame | Not filed; the path was removed | #543, resolution |
| An atlas refusal cached terminally | canvas | Unloading per path leaves a sprite that would now fit out of the atlas for good | `UnloadAll` at a level boundary | canvas README L750-757; assets.md L1097-1138 |
| Glyph pages | canvas | Unreclaimable until the framebuffer scale changes; `UnloadAll` does not reclaim them | Open | canvas README L747-748; #443 |
| The Library's seams | `libs/assets` | A loader cannot name the file it loads, so canvas reports decode failures without the path. Two tiers sharing a params type share one report-once namespace. | Open, and fixed by giving each tier its own `P` | #443, comments |
| Temporary pools | gfx | They never release their buffers and textures | Out of scope | drawparams.md L888 |
| Shader compile cost | gfx | A naga parse on the caller's goroutine, never measured | A recorded Gap | drawparams.md L874-876 |

**v1's own judgements, as the docs state them:**

- "The call shape was the cost" (scene.md L70-72).
- "Material is retired from gfx." It stays a word the bundles use for their
  own concepts (drawparams.md L79-81).
- canvas kept its API "as a stopgap until it is rebuilt on scene". The next
  two refactors were to be scene's proper materials, then canvas drawing
  through scene (materials.md L76-79; drawparams.md L493-495, L882-883).
- Batching is always on, with no opt-out (scene README L353-355).
- A model that fails draws nothing, never a placeholder; a named colour
  texture that fails draws magenta (model.md L1195-1196, L1240-1250).
- Shaders are checked against WebGPU's default limits, never the device's,
  so a desktop build cannot pass with what a browser refuses (gfx README
  L615-621).
- Every hot path stays at zero allocations (drawparams.md L870).

---

## 5. What v2 took over, and what nothing in v2 covers yet

### 5.1 Taken over

| v1 | v2 | Difference | Source |
|---|---|---|---|
| WGSL preprocessor | `gogpu/shader`, reached through `Device.PreprocessShader(files, path, defines)`. `ShaderDefine{Name, Value}` fills both defines and `#const`s. | v2's root is a path in an `fs.FS`; v1 also took inline text (`ShaderWithText`) | `gfx/docs/design.md` L196-206; `gfx/descriptors.go` L73 |
| Reflection | The backend reflects a module once, at its creation; a pipeline with a zero layout gets its reflected layouts as explicit ones | v1 reflected to resolve params by name; v2 reflects for layouts only | design.md L207-217 |
| Vertex interface check | `ErrVertexMismatch` | v2 follows WebGPU: a component count differing from the input's is fine. v1 required `(kind, count)` to be equal. | design.md L218-224; gfx README L668-676 |
| Limits check | `ErrLimitExceeded`, against the device's limits | v1 always checked against WebGPU's defaults; v2's device is designed against those defaults, with features opt-in | design.md L224-230, L77-79; gfx README L615-621 |
| Capture | `Device.RequestCapture(window)`, `CapturedEvent`, `ReadCapture`. Where the back buffer cannot be copied (DX12), the frame is drawn offscreen, "as v1 did". | v2 captures a window's framebuffer only, with no bursts; textures are read back through `MapBuffer` | design.md L376-412 |
| Barriers | The backend records a barrier for a texture sampled after its use as an attachment | — | design.md L498-504 |

New in v2, with no v1 counterpart: recovery after a device loss with every
handle kept (I found no v1 handling), several windows, compute pipelines,
declared timestamps (unavailable until the fork exposes them), and one
recording adapter in place of v1's three test backends (design.md L430-463,
L414-428, L465-496).

Out of gfx's scope: v1's agent-facing capture tool, as "a plugin of its own on
`CapturedEvent`" (design.md L557-558).

v2 pieces next to draw's ground:

- **ecs's Ref:** a pointer-free, stable handle to a value kept outside the
  stores, which many components may hold (`ecs/CONTEXT.md` L50-52).
- **platform's FileSystem:** one overlay of the read mounts and the
  writable filesystem, as v1's `storage.FileSystem` was
  (`platform/CONTEXT.md` L80-82). platform also has window space and
  framebuffer space (L51-55). v1's logical viewport modes (Fit, Cover,
  fixed width or height) are not in v2.
- **mux:** specified, not built (the module holds only `CONTEXT.md` and
  `docs/specs/mux.md`). It is eased timed overrides of model state, the
  nearest thing to anim's tracks. I found no flipbook in it.

### 5.2 Nothing in v2 covers yet

gfx's design hands these to "scene" (design.md L1-12, L236-240, L329-332,
L553-556); the map makes them draw's.

| Area | v1 features with no v2 counterpart |
|---|---|
| Assets | A cache keyed by descriptor, terminal failure reported once, loading by path or blob. No image decoder (PNG, JPEG), mip generation, font loading or glTF decoder: a search of gfx, gogpu, platform, ecs and mux finds no `image.Decode`, `opentype` or `qmuntal`. |
| Shaders and materials | Program identity by (root, supply), and variants. Params resolved by binding name, durable sets, per-frame versions, defaults for an unfilled binding (white texture, default sampler, dropped storage). Pipeline sharing per descriptor, and sampler dedupe. |
| Frame structure | Passes ordered by `Order` and merged, a pass running only if it has an effect, depth pooled per size, a pool of transient targets, a uniform ring and instance arena, the frame buffer and present pass, logical viewport modes |
| Geometry | `MeshDescr` as a unit, unit shapes, vertex and index packing, morph deltas |
| Drawing at scale | Instancing and batching, culling, sorting, layers, cameras with passes by tag |
| Shading | Lights and their selection, the bundled PBR, shader variants, the material overlay, a default scene shader, the published shader stages |
| Animation | Skinning, morphing, clip plays, the clip machine |
| 2D | All of canvas: layers, sprites, the atlas, text and fonts, shapes, clip, materials and scopes, tint, the halo, layers rendered to a texture |
| Inspection | The frame snapshot (`gfx_frame`), `canvas_draws`, a recorder's own inspection |
| Hot reload | Deferred by gfx (design.md L562). v1 had none either. |

---

## 6. Where docs and code disagree, and what I could not verify

**Disagreements (the code wins):**

1. assets.md L42 says "Nothing in this document is built." It is built:
   `libs/assets/cache.go` exists, and canvas (`bundles/canvas/internal/lookup.go`
   L45-54), model (`bundles/model/internal/lookup.go` L50-51), gfx
   (`slots/gfx/internal/translator.go` L112) and sound use it. #443's
   resolution lists the commits.
2. assets.md describes two gfx caches, shaders and textures. gfx caches only
   path textures (`translator.go` L109-112): #604 deleted its shader cache
   (drawparams.md L804-813), and shaders are their callers'.
3. assets.md and model.md put the model cache in scene and loading in
   "scene's flush". The cache is model's (`bundles/model/internal/lookup.go`
   L50-51), and loading runs in scene's load System through `Lookup.Resolve`
   (`bundles/scene/internal/keyer.go` L101). The "flush" was the removed
   recording renderer's.
4. #188's comments say #193 deliberately left the halo's WGSL unpublished.
   The code publishes `HaloBandPath` (`bundles/canvas/internal/types.go`
   L233-237), and the canvas README documents including it (L370-384).
5. model.md L1964 and L1969 say gfx has no preprocessing, and cite
   `extensions/gfx/translate.go`. Both are stale: the preprocessor exists,
   and the path is gone.
6. The ticket names scene's `OpQueue`, which #573 removed. Only canvas and
   gfx declare an `OpQueue` in `_legacy`.

**Not verified:**

- Nothing was built, run or measured. Every figure here is the docs' own.
- I checked these claims against the code: gfx's `OpQueue` and
  `ResourceQueue` methods, the texture formats, `PassDescr`'s fields,
  canvas's `OpQueue` and Lookup methods, model's facade methods,
  `MaxLights = 16`, `MaxClipPlays = 4`, scene's camera, pass and layer
  vocabulary, the cache declarations, the image decoders model and canvas
  register, and the published halo band. Everything else rests on the docs.
- "No device-loss handling in v1" is a search result, not a proof: I
  searched `extensions/gogpu/internal` and `slots/gfx/internal` for loss and
  recovery and found nothing.
- "canvas never releases its shaders and sets" comes from the same kind of
  search: no `ReleaseDrawParams` or `ReleaseShader` in
  `bundles/canvas/internal`.
- I read ui only as far as it records into canvas, and sound not at all.
- Line numbers that the specs themselves cite for older code are as they
  were written.
