# glTF 2.0 feature inventory and loader choice

Resolves [#5](https://github.com/dvoyni/cog/issues/5) for the scene map
([#1](https://github.com/dvoyni/cog/issues/1)). Scope is fixed by the map: runtime
loading of `.gltf`/`.glb` through `storage`, WebGPU only, no compute, skeletal
poses baked per clip at load, morphing and skinning in the vertex shader, bundled
metallic-roughness PBR with constant ambient and no IBL.

**Gist:** parse with [`github.com/qmuntal/gltf`](https://github.com/qmuntal/gltf)
(zero runtime dependencies, `fs.FS` decoder, builds for `js/wasm`), hand-roll only
the conversion into scene's own mesh, material, and baked-pose types, and drop the
`gltf.Document` after conversion.

## 1. Core feature inventory

Spec: [glTF 2.0](https://registry.khronos.org/glTF/specs/2.0/glTF-2.0.html).
"Must" means a demo model or the map's settled decisions depend on it; "optional"
means cheap and common but no demo needs it; "reject" means fail the load and report
once through `kernel.ReportError` (the map's failure-reporting note).

### Containers, buffers, images

| Feature | Verdict | Notes |
|---|---|---|
| `.glb` (JSON chunk + one BIN chunk) | must | [GLB layout](https://registry.khronos.org/glTF/specs/2.0/glTF-2.0.html#glb-file-format-specification). Preferred distribution form for the demos; one `storage` read. |
| `.gltf` with external `.bin` and images | must | Relative URIs resolve against the model's directory in `storage.FileSystem` (`fs.Sub`). FlightHelmet and every Blender export ship this way. |
| `data:` base64 buffers and images | optional | Trivial with `encoding/base64`; qmuntal decodes buffers, images via `Image.MarshalData`. |
| Images in a `bufferView` (GLB-embedded) | must | How every `.glb` carries textures. Decode with `image.Decode`; `image/png` and `image/jpeg` are already linked by `canvas` and `gfx`. |
| Image MIME types beyond PNG/JPEG (WebP, KTX2) | reject | [Texture compression is deferred](https://github.com/dvoyni/cog/issues/1) (KTX2/Basis note). `EXT_texture_webp`/`KHR_texture_basisu` in `extensionsRequired` fail the load. |
| Absolute or `..` URIs | reject | `storage` requires `fs.ValidPath`; qmuntal's `validateBufferURI` already rejects non-local paths. |

### Accessors and buffer views

| Feature | Verdict | Notes |
|---|---|---|
| Tightly packed and interleaved (`byteStride` 4..252) accessors | must | [Data alignment](https://registry.khronos.org/glTF/specs/2.0/glTF-2.0.html#data-alignment). CesiumMan and Fox interleave POSITION/NORMAL/TEXCOORD in one view. Scene re-packs into its own vertex layout at load, so strides never reach the GPU. |
| Sparse accessors | must | [Sparse accessors](https://registry.khronos.org/glTF/specs/2.0/glTF-2.0.html#sparse-accessors). Exporters use them for morph targets; an accessor without `bufferView` is all zeros. `modeler.ReadAccessor` resolves both. |
| `min`/`max` on POSITION | must | Required by the spec; scene's per-camera CPU frustum cull needs a bounding sphere per mesh. Compute from data if absent rather than reject. |
| Normalized integer attributes (UV, colour, weights) | must | Spec allows `UNSIGNED_BYTE`/`UNSIGNED_SHORT` normalized for TEXCOORD, COLOR, WEIGHTS. Convert to float at load. |
| Index component types | must | [`UNSIGNED_BYTE`/`SHORT`/`INT`](https://registry.khronos.org/glTF/specs/2.0/glTF-2.0.html#_mesh_primitive_indices). WebGPU has only [`uint16`/`uint32`](https://www.w3.org/TR/webgpu/#enumdef-gpuindexformat): widen 8-bit indices to 16. |
| Non-indexed primitives | must | Draw non-indexed or synthesize indices; either is a few lines. |

### Meshes and primitives

| Feature | Verdict | Notes |
|---|---|---|
| Multiple primitives per mesh, each with its own material | must | FlightHelmet, MilkTruck, MorphStressTest. One scene draw per primitive; instancing key is (primitive, material, pass). |
| `TRIANGLES` | must | |
| `TRIANGLE_STRIP`, `TRIANGLE_FAN` | optional | Convert to lists at load; WebGPU has [no fan topology](https://www.w3.org/TR/webgpu/#enumdef-gpuprimitivetopology). |
| `POINTS`, `LINES`, `LINE_LOOP`, `LINE_STRIP` | reject | The bundled PBR shader is triangle-only; no demo needs them. Revisit if a consumer asks. |
| POSITION, NORMAL, TANGENT, TEXCOORD_0, TEXCOORD_1, COLOR_0, JOINTS_0, WEIGHTS_0 | must | The spec's minimum: [at least two UV sets, one colour, one joint/weight set](https://registry.khronos.org/glTF/specs/2.0/glTF-2.0.html#meshes-overview). Occlusion often uses `texCoord: 1`. |
| Missing NORMAL | must | Spec: [flat normals MUST be computed](https://registry.khronos.org/glTF/specs/2.0/glTF-2.0.html#meshes-overview); de-index and compute at load. |
| Missing TANGENT with a normal map | must | Spec says MikkTSpace SHOULD be used; a per-triangle UV-gradient tangent at load is adequate for the demos (DamagedHelmet ships tangents anyway). Record the shortcut in `scene/spec.md`. |
| JOINTS_1/WEIGHTS_1 (more than 4 influences) | reject (ignore) | Keep the first set, ignore extra sets. Renormalise weights. |

### Node hierarchy and scenes

| Feature | Verdict | Notes |
|---|---|---|
| Node tree, TRS or `matrix`, default `scene` | must | Flatten to world transforms per mesh instance at load. If `scene` is absent use index 0; if `scenes` is absent, the file has no renderable content and loads as an empty model. |
| Multiple scenes | optional | Load the default only; other scenes are ignored, not rejected. |
| Cameras | reject (ignore) | A scene camera is declared per frame by the app; glTF camera nodes carry no data scene consumes. |

### Skins

| Feature | Verdict | Notes |
|---|---|---|
| `joints`, `inverseBindMatrices` | must | [Skins](https://registry.khronos.org/glTF/specs/2.0/glTF-2.0.html#skins). The load-time bake resolves the joint hierarchy per clip frame into `joint_world * inverseBind` and uploads per-bone per-frame matrices. Missing `inverseBindMatrices` means identity. |
| `skeleton` | optional (ignore) | A hint for the root; the bake walks the node graph from the scene root regardless. |
| Skinned node transform | must | Spec: [only joint transforms apply; the skinned mesh node's own transform MUST be ignored](https://registry.khronos.org/glTF/specs/2.0/glTF-2.0.html#skinned-mesh-attributes). Easy to get wrong; note in spec. |
| Joint count | must (no limit) | Palettes live in a storage buffer, so no uniform-size cap. Fox has 24 joints, CesiumMan 19. |

### Morph targets

| Feature | Verdict | Notes |
|---|---|---|
| POSITION / NORMAL / TANGENT deltas | must | [Morph targets](https://registry.khronos.org/glTF/specs/2.0/glTF-2.0.html#morph-targets). AnimatedMorphCube has all three, MorphStressTest has 8 targets of POSITION+NORMAL. Upload deltas as a storage buffer indexed by target and vertex; blend in the vertex shader. |
| `mesh.weights` defaults and `node.weights` overrides | must | Default pose when no `weights` clip plays. |
| `weights` animation channel | must | Baked per frame alongside bone poses (one float per target per frame). |
| Morph targets with other attributes (TEXCOORD, COLOR) | reject (ignore) | Spec allows them as extras; no sample uses them. |

### Animations

| Feature | Verdict | Notes |
|---|---|---|
| Channels targeting `translation`, `rotation`, `scale`, `weights` | must | [Animations](https://registry.khronos.org/glTF/specs/2.0/glTF-2.0.html#animations). |
| `LINEAR` (slerp for rotations), `STEP`, `CUBICSPLINE` | must | [Interpolation](https://registry.khronos.org/glTF/specs/2.0/glTF-2.0.html#interpolation). Since clips are sampled into frames at load, all three modes are load-time CPU only; the shader sees only frames. CUBICSPLINE uses in-tangent/value/out-tangent triples. |
| Clamping outside the keyframe range | must | First and last keys hold. |
| Channels targeting nodes outside a skin (rigid animation) | must | MilkTruck wheels, BoxAnimated. Bake into a per-node track the same way; rigid-animated nodes are the degenerate one-bone case. |
| Animations targeting nodes not in the default scene | optional (ignore) | |

### Materials

| Feature | Verdict | Notes |
|---|---|---|
| `pbrMetallicRoughness` factors and textures | must | [Materials](https://registry.khronos.org/glTF/specs/2.0/glTF-2.0.html#materials). baseColor (sRGB), metallicRoughness (linear, G=roughness, B=metallic). |
| `normalTexture.scale`, `occlusionTexture.strength`, `emissiveTexture`, `emissiveFactor` | must | DamagedHelmet uses all five maps. |
| `texCoord` per texture slot | must | Spec minimum of two sets; drives a per-slot UV set index in the material uniform. |
| `alphaMode` OPAQUE/MASK/BLEND, `alphaCutoff` | must | Settled in the map. MASK is a shader discard; BLEND goes to a sorted transparent pass. AlphaBlendModeTest covers all three. |
| `doubleSided` | must | Settled. Pipeline cull mode `none` plus normal flip on back faces. |
| Samplers: wrap S/T, mag/min filters | must | Map 1:1 to WebGPU address and filter modes. Mipmapped min filters require generating mips: WebGPU has no `generateMipmap`, so scene needs a small render-pass blit chain (a gfx contract item for the texture ticket). |
| Missing material | must | Spec default: white, metallic 1, roughness 1. |
| Vertex colour multiply | must | COLOR_0 multiplies baseColor per spec. |

## 2. Extension assessment

| Extension | Verdict | Cost | Notes |
|---|---|---|---|
| [`KHR_texture_transform`](https://github.com/KhronosGroup/glTF/tree/main/extensions/2.0/Khronos/KHR_texture_transform) | support in v1 | low | `offset`, `rotation`, `scale`, optional `texCoord` override per texture slot; a 3x2 UV matrix per slot in the material uniform. Blender emits it whenever a Mapping node is used. qmuntal ships `ext/texturetransform`. Sample: TextureTransformTest (CC0). |
| [`KHR_materials_emissive_strength`](https://github.com/KhronosGroup/glTF/tree/main/extensions/2.0/Khronos/KHR_materials_emissive_strength) | support in v1 | trivial | One float multiplier on `emissiveFactor` (default 1). Blender emits it for emission strength above 1. Not in qmuntal's `ext/`; a 10-line `RegisterExtension` or reading the raw JSON from `Material.Extensions`. Sample: EmissiveStrengthTest (CC-BY-4.0). |
| [`KHR_lights_punctual`](https://github.com/KhronosGroup/glTF/tree/main/extensions/2.0/Khronos/KHR_lights_punctual) | parse in v1, expose as data | low | Point and spot map onto scene's per-frame point/spot lights (intensity in candela, `range`, `innerConeAngle`/`outerConeAngle`); directional lights are dropped per the map ("plain directional lights beyond the sun are dropped"). The loader returns the light list with node transforms; the app decides whether to declare them each frame, so scene stays a pure function of the frame. qmuntal ships `ext/lightspunctual`. Sample: LightsPunctualLamp (CC-BY-4.0). |
| [`KHR_mesh_quantization`](https://github.com/KhronosGroup/glTF/tree/main/extensions/2.0/Khronos/KHR_mesh_quantization) | support in v1 by dequantizing at load | low | Only relaxes accessor component types (int8/int16 positions, normals, UVs). Since scene re-packs vertices anyway, converting to float at load costs nothing extra and keeps one vertex layout; uploading quantized data directly would collide with WebGPU's lack of 3-component 8/16-bit [vertex formats](https://www.w3.org/TR/webgpu/#enumdef-gpuvertexformat). Needed because gltfpack/meshoptimizer output lists it in `extensionsRequired`. `modeler.ReadAccessor` reads any component type; the typed helpers (`ReadPosition`) accept float only, so use the generic path. Sample: AnimatedMorphCube/glTF-Quantized (CC0). |
| `KHR_materials_unlit` | optional | trivial | Skip lighting in the bundled shader. Not required by demos; cheap if a consumer wants stylised assets. |
| `KHR_draco_mesh_compression`, `EXT_meshopt_compression` | reject | high | Both need a decoder; no maintained pure-Go Draco. Fail when in `extensionsRequired`. |
| `KHR_texture_basisu`, `EXT_texture_webp` | reject | | Texture compression deferred by the map. |
| `KHR_materials_*` (clearcoat, transmission, sheen, volume, specular, ior, iridescence, anisotropy) | ignore | | Advanced BRDF lobes are outside the bundled model. Ignored when only in `extensionsUsed` (FlightHelmet lists transmission this way and renders fine without it). |
| `EXT_mesh_gpu_instancing` | ignore | | Scene auto-instances; not needed for demos. |

Rule: unknown extensions in `extensionsUsed` are ignored; unknown extensions in
`extensionsRequired` fail the load with the extension name in the report.

## 3. Loader: `github.com/qmuntal/gltf` versus hand-rolled

Facts checked on 2026-09-04 against
[qmuntal/gltf v0.29.0](https://github.com/qmuntal/gltf/releases/tag/v0.29.0)
(released 2026-08-19, BSD-2-Clause, 287 stars, `go 1.20` minimum):

- **Dependencies.** [`go.mod`](https://github.com/qmuntal/gltf/blob/master/go.mod)
  requires only `github.com/go-test/deep` (tests). `go list -deps` for `gltf`,
  `gltf/binary`, `gltf/modeler` shows stdlib only. Cog's `go.mod` grows by one
  line and no transitive modules.
- **Web build.** `GOOS=js GOARCH=wasm go build github.com/qmuntal/gltf/...`
  succeeds (verified). Pure Go, no cgo, no build tags. `os` is used only in the
  convenience `gltf.Open`; the [`Decoder`](https://github.com/qmuntal/gltf/blob/master/decoder.go)
  takes `io.Reader` plus an `fs.FS` for external resources, which is exactly the
  `storage.FileSystem` shape canvas already loads through.
- **API fit.** `gltf.NewDecoderFS(r, fs.Sub(filesystem, dir)).Decode(&doc)`
  detects GLB versus JSON by content, decodes the JSON with `encoding/json`,
  reads the BIN chunk, decodes `data:` URIs, and reads external `.bin` files
  through the `fs.FS`. Images are not loaded (URI/`bufferView` are exposed),
  which is what scene wants: decode through `image.Decode` like canvas.
  [`modeler.ReadAccessor`](https://github.com/qmuntal/gltf/blob/master/modeler/read.go)
  returns a typed slice for any accessor (interleaved, sparse, no-bufferView,
  any component type) and is bounds-checked against malformed documents;
  `modeler.ReadBufferView` is a zero-copy slice view. Optional fields are
  pointers; `Extensions` keeps unregistered extensions as raw JSON.
- **Allocation behaviour** (measured with a scratch program, `runtime.MemStats`):

  | Model | `Decoder.Decode` | `ReadAccessor` for every accessor | `ReadBufferView` |
  |---|---|---|---|
  | DamagedHelmet.glb, 3.60 MiB | 2022 allocs, 7.47 MiB | 25 allocs, 0.56 MiB | 0 allocs |
  | Fox.glb, 0.16 MiB | 1704 allocs, 0.45 MiB | 166 allocs, 0.12 MiB | 0 allocs |

  Decode costs about 2x file size because the BIN chunk is read with
  `io.ReadAll(LimitReader)` (a deliberate anti-DoS choice documented in the
  source: the untrusted `byteLength` is never used for a preallocation) plus
  the `encoding/json` reflection tree. All of it is load-time, once per model,
  and garbage as soon as scene has copied out its own buffers. Nothing runs per
  frame. For a web build the transient 2x peak on a 50 MiB model is the only
  concern, and scene's residency budget (deferred by the map) governs that
  regardless of parser.
- **Risk.** `v0.x` module: pin the version; the API has been stable in shape
  since 2019. Single maintainer, but active (hardening commits in 2026, fuzz and
  bench tests in tree) and forkable at roughly 5k lines.

A hand-rolled parser would re-implement: the JSON schema structs (still
`encoding/json`), GLB chunk parsing, base64 URIs, URI sanitising, accessor
unpacking with strides, sparse overlays, normalized conversion, and all the
bounds validation that keeps a corrupt file from panicking the engine. That is
roughly 800 to 1200 lines with no allocation win: the same JSON tree and the
same buffer copies happen either way, and the one measurable improvement
(preallocating the BIN chunk to `byteLength`) trades away a DoS guard for one
copy at load time.

**Recommendation.** Use `qmuntal/gltf` (`gltf` and `gltf/modeler`; skip
`gltf/binary` unless needed) as the parse layer only. Scene owns the conversion:
one pass from `gltf.Document` into scene's mesh, material, texture, and baked
pose types, then drop the document and its `Buffer.Data`. Read tightly packed
float attributes through `ReadBufferView` (zero copy into the repack) and
everything else through `ReadAccessor`. Register `KHR_materials_emissive_strength`
locally; use the shipped `ext/texturetransform` and `ext/lightspunctual`. The
`gltf.Document` never appears in scene's public API, so the dependency stays
swappable.

## 4. Sample models for the demos

All from [KhronosGroup/glTF-Sample-Assets](https://github.com/KhronosGroup/glTF-Sample-Assets)
(licence per model in its `metadata.json` and README "Legal" section). Verified
counts come from the `glTF/*.gltf` JSON.

| Demo | Model | Licence | Size | What it proves |
|---|---|---|---|---|
| PBR showcase | [DamagedHelmet](https://github.com/KhronosGroup/glTF-Sample-Assets/tree/main/Models/DamagedHelmet) | CC-BY-4.0 (ctxwing, 2018 conversion); earlier version CC-BY-NC-4.0 (theblueturtle_) | 3.6 MiB glb | All five PBR maps on one material, GLB-embedded JPEG/PNG images, one primitive. The NC lineage makes it fine for an open examples repo but worth the attribution line; [SciFiHelmet](https://github.com/KhronosGroup/glTF-Sample-Assets/tree/main/Models/SciFiHelmet) (CC0) and [BoomBox](https://github.com/KhronosGroup/glTF-Sample-Assets/tree/main/Models/BoomBox) (CC0, 10 MiB) are clean-licence alternates. |
| Skinned character | [Fox](https://github.com/KhronosGroup/glTF-Sample-Assets/tree/main/Models/Fox) | CC0 model (PixelMannen); CC-BY-4.0 rig and animation (tomkranis) and glTF conversion (AsoboStudio, scurest) | 159 KiB glb | 24-joint skin with `inverseBindMatrices` and `skeleton`, three LINEAR clips (Survey, Walk, Run) targeting translation and rotation, interleaved buffer views. Three clips make it the weight-blending demo. |
| Skinned character (alternate) | [CesiumMan](https://github.com/KhronosGroup/glTF-Sample-Assets/tree/main/Models/CesiumMan) | CC-BY-4.0 (Cesium); logo under a non-copyrightable legal mark | 427 KiB glb | 19 joints, one clip animating translation, rotation and scale (57 channels), interleaved views. Tests scale channels that Fox lacks. |
| Morph targets | [AnimatedMorphCube](https://github.com/KhronosGroup/glTF-Sample-Assets/tree/main/Models/AnimatedMorphCube) | CC0 (Microsoft) | 6 KiB glb | Two targets with POSITION, NORMAL and TANGENT deltas, a `weights` clip. The `glTF-Quantized` variant is the `KHR_mesh_quantization` test (int8/int16 normalized attributes, `extensionsRequired`). |
| Morph stress | [MorphStressTest](https://github.com/KhronosGroup/glTF-Sample-Assets/tree/main/Models/MorphStressTest) | CC-BY-4.0 (Ed Mackey, AGI) | 562 KiB glb | 8 targets on two primitives, three `weights` clips, double-sided materials, occlusion texture. Proves the per-target storage buffer scales. |
| Multi-material scene | [FlightHelmet](https://github.com/KhronosGroup/glTF-Sample-Assets/tree/main/Models/FlightHelmet) | CC0 (Gary Hsu conversion; Microsoft original) | 47 MiB, `.gltf` only | Six meshes with six materials, 15 external PNG textures and an external `.bin`: the external-resource path through `storage`. Lists `KHR_materials_transmission` in `extensionsUsed` only, so it proves ignore-unknown-used. Heavy for web; see the alternate. |
| Multi-material scene (light) | [CesiumMilkTruck](https://github.com/KhronosGroup/glTF-Sample-Assets/tree/main/Models/CesiumMilkTruck) | CC-BY-4.0 (Cesium); logo legal mark | 361 KiB glb | Two meshes, four primitives, four materials (two untextured), six-node hierarchy, a rigid rotation clip on the wheels. Multi-material plus non-skinned node animation in one small file. |
| Alpha modes | [AlphaBlendModeTest](https://github.com/KhronosGroup/glTF-Sample-Assets/tree/main/Models/AlphaBlendModeTest) | CC-BY-4.0 (Ed Mackey, AGI) | 2.9 MiB glb | OPAQUE, MASK with cutoff, BLEND side by side. |
| Interpolation | [InterpolationTest](https://github.com/KhronosGroup/glTF-Sample-Assets/tree/main/Models/InterpolationTest) | CC0 (Khronos) | 7 KiB glb | STEP, LINEAR, CUBICSPLINE samplers for the bake. |
| Sparse accessors | [SimpleSparseAccessor](https://github.com/KhronosGroup/glTF-Sample-Assets/tree/main/Models/SimpleSparseAccessor) | CC-BY-4.0 (Marco Hutter) | tiny `.gltf` | Sparse POSITION overlay. |
| Extension tests | [TextureTransformTest](https://github.com/KhronosGroup/glTF-Sample-Assets/tree/main/Models/TextureTransformTest) (CC0), [EmissiveStrengthTest](https://github.com/KhronosGroup/glTF-Sample-Assets/tree/main/Models/EmissiveStrengthTest) (CC-BY-4.0), [LightsPunctualLamp](https://github.com/KhronosGroup/glTF-Sample-Assets/tree/main/Models/LightsPunctualLamp) (CC-BY-4.0) | | One per v1 extension. |

Avoid: Sponza (Crytek licence agreement), BrainStem (Poser EULA). CC-BY models
need an attribution line in the `cog-examples` README next to the asset.

## 5. Items this hands to later tickets

- Mip generation as a gfx blit chain (texture ticket): needed for any glTF
  sampler with a mipmap min filter, which is the default in most exporters.
- Bounding sphere from accessor `min`/`max` for the frustum cull; skinned and
  morphed meshes need a padded bound (spec ticket).
- Skinned node transform is ignored per spec; rigid-animated nodes bake as
  one-bone tracks (load semantics ticket).
- Tangent generation shortcut and flat-normal generation (spec ticket).
- Failure classes: unknown `extensionsRequired`, unsupported image MIME,
  non-triangle primitive, invalid URI. All report once via `kernel.ReportError`
  and load as an empty model, mirroring canvas.
