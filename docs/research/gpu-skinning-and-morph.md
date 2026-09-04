# GPU skinning and morph techniques for scene

Resolves [#6](https://github.com/dvoyni/cog/issues/6) for the scene plugin map
([#1](https://github.com/dvoyni/cog/issues/1)). Settled inputs: no compute
shaders; every clip is baked once on the CPU into per-bone per-frame poses in a
storage buffer; each draw passes N clip plays `{clip, time, weight}` and the
vertex shader blends them; WebGPU core on desktop and browser, no fallbacks.

## 1. Pose representation and blending

Three representations are in use for baked poses. The question is what happens
when the shader mixes them twice: between two adjacent frames of one clip, and
across N clips by play weight.

| Axis | 4x3 matrix | TRS (T3 R4 S3) | Dual quaternion |
|---|---|---|---|
| Floats per bone-frame | 12 | 10 (12 with vec3 padding) | 8 |
| Frame lerp at 30 Hz | Fine: adjacent-frame rotation deltas are a few degrees, so matrix lerp shrinkage is invisible (what Unity's sample and GPU Gems 3 do) | Exact (nlerp on R) | Exact |
| Cross-clip blend at 50/50 | Wrong for large rotation differences: linear matrix blend shrinks and shears (the LBS candy-wrapper artefact applied to whole poses) | Correct (nlerp rotation, lerp T and S) | Correct, and also fixes candy-wrapper at joints |
| Non-uniform scale / shear | Exact | Scale yes, shear no | No scale at all (Kavan 2008 bolts it on as a separate matrix phase) |
| Per-influence work after blending | 0 | quat normalize + quat-to-matrix (~50 ops) | DQ normalize + DQ-to-matrix (~40 ops) |
| Matches glTF semantics | Yes (glTF defines LBS) | Yes (LBS on composed matrices) | No (different deformation; bulging) |

Sources: Kavan et al., [Skinning with Dual Quaternions](https://users.cs.utah.edu/~ladislav/kavan07skinning/kavan07skinning.pdf)
(I3D 2007) and [Geometric Skinning with Approximate Dual Quaternion Blending](https://users.cs.utah.edu/~ladislav/kavan08geometric/kavan08geometric.pdf)
(TOG 2008), which cover antipodality (sign flip against a pivot before summing)
and the scale/shear limitation; [GPU Gems 3 ch. 2](https://developer.nvidia.com/gpugems/gpugems3/part-i-geometry/chapter-2-animated-crowd-rendering)
(matrices, 3 texels per bone, lerp between frames); Unity's
[Animation-Instancing](https://github.com/Unity-Technologies/Animation-Instancing)
(`AnimationInstancingBase.cginc`: RGBAHalf texture, 3 texels per bone, skins
with the floor and ceil frame matrices and lerps the two skinned positions).

Verdict: matrices are the cheapest per vertex but cannot blend clips
correctly, and clip blending by weight is the whole point of the play list.
Dual quaternions are the most compact but change the deformation model away
from glTF's LBS and drop scale. TRS is the only representation that blends
correctly across clips and still lands on plain LBS. Its cost is one
quaternion normalize and one TRS-to-matrix per influence.

Quaternion sign handling: at bake time keep every frame of a clip in the same
hemisphere as the previous frame (so the frame lerp needs no check). Across
clips, sign-fix each fetched rotation against the running accumulator (one dot
and one select per record); this is robust for arbitrary clip pairs and costs
about 3 ops per record.

What the baked TRS is: the skinning transform `globalJoint * inverseBind`,
decomposed per (skin, clip). Composing TRS along the hierarchy is exact unless
a parent has non-uniform scale under a child rotation (shear), which TRS cannot
carry. Bake by composing matrices, decompose, recompose, and report once
through `kernel.ReportError` if the residual exceeds an epsilon. Premultiplying
the inverse bind matrix at bake removes a 4x3 multiply and a 48-byte fetch per
influence at runtime; the cost is duplicating a clip if two skins share joints,
which is rare in glTF character assets.

## 2. Sample rate and memory per clip

Authoring norms are 24 or 30 fps (glTF exporters, Houdini's VAT tools default to
24, Unity's instancing sample bakes at 15 fps by default with a 1..120 slider).
Bytes per second of clip for a 64-joint skeleton, f32:

| Rate | 4x3 (48 B) | TRS padded (48 B) | TRS tight (40 B) | DQ (32 B) |
|---|---|---|---|---|
| 15 Hz | 45 KiB/s | 45 KiB/s | 37.5 KiB/s | 30 KiB/s |
| 30 Hz | 90 KiB/s | 90 KiB/s | 75 KiB/s | 60 KiB/s |
| 60 Hz | 180 KiB/s | 180 KiB/s | 150 KiB/s | 120 KiB/s |

A character with 60 s of total clip time is 5.3 MiB at 30 Hz padded TRS; the
pose buffer is shared by every instance of the model. 30 Hz with linear
interpolation reproduces 24/30 fps authored motion; 15 Hz visibly softens fast
attacks even with interpolation; 60 Hz is only justified for clips authored at
60. f16 storage halves the table (Unity uses RGBAHalf for crowds) at ~1e-3
quaternion precision, which shows as jitter at the end of long bone chains, so
keep it a later memory-budget option rather than the default.

## 3. Per-vertex fetch cost: 4 influences x 2 frames x N clips

Each vertex fetches `4 * 2 * N = 8N` pose records. A record is 3 vec4 loads at
48 B (padded TRS or 4x3) or 2 vec4 loads at 32 B (DQ). Rough ALU counts assume
the frame fraction and play weight are folded into one scalar per record on the
CPU, so each record costs one weighted accumulate.

| N | Records | vec4 loads (48 B rec) | Bytes read | TRS ops (approx.) | Matrix ops (approx.) |
|---|---|---|---|---|---|
| 1 | 8 | 24 | 384 B | ~350 | ~170 |
| 2 | 16 | 48 | 768 B | ~430 | ~270 |
| 4 | 32 | 96 | 1.5 KiB | ~590 | ~460 |
| 8 | 64 | 192 | 3 KiB | ~910 | ~840 |
| classic palette (CPU/compute resolved) | 4 | 12 | 192 B | ~70 | ~70 |

TRS ops = 4 * (10 * 2N + 50) + 72; matrix ops = 4 * (12 * 2N) + 72; both include
the final position and normal transform. The working set is tiny (64 joints x
2 frames x N x 48 B = 6N KiB) and stays in L1/L2, so the cost is instruction
count and load latency, not DRAM bandwidth. At 1M skinned vertices per frame
at 60 fps, N = 4 is ~35 GFLOP/s: under 1% of a discrete GPU, a few percent of an
integrated one. The redundancy is the real inefficiency: with 10k vertices and
64 joints every bone blend is recomputed ~156 times per instance. It starts to
hurt with crowds (hundreds of instances x 10k vertices) at N >= 2 on integrated
or mobile-class GPUs; that is the trigger for the deferred per-instance
pose-resolve pass in #1, and the skinned demo should measure vertices x N
rather than guess. Cap N at 4 plays per draw: the instance record stays fixed
size and the worst case stays inside the table above.

## 4. Texture versus storage buffer for pose data

| | Storage buffer | Float texture |
|---|---|---|
| Addressing | Direct struct/array index, any stride | Row/column arithmetic; 3 texel loads per record; width limits (8192 core, 4096 compat) force 2D tiling |
| Capacity per binding | `maxStorageBufferBindingSize` 128 MiB default | 8192 x 8192 x 16 B = 1 GiB in principle, but 2D tiling code |
| Formats needed from gfx | Already have `BufferStorage`, `BufferParam`, storage instance buffer precedent | Needs RGBA32Float or RGBA16Float; gfx has only `FormatRGBA8` today (ticket #11) |
| Free interpolation | None | Bilinear filter along the frame axis gives the frame lerp for free (rgba16float filterable by default; rgba32float needs the optional `float32-filterable` feature) |
| Vertex-stage availability | Core: 8 per stage. Compatibility mode: 0 | `textureLoad` everywhere including compat |

Precedent: three.js keeps bone matrices in a uniform buffer when they fit and
falls back to a bone texture only for large skeletons
([`Skinning.js`](https://github.com/mrdoob/three.js/blob/dev/src/nodes/accessors/Skinning.js));
Bevy uses `array<mat4x4>` storage buffers when available and a 256-matrix
uniform otherwise ([`skinning.wesl`](https://github.com/bevyengine/bevy/blob/main/crates/bevy_pbr/src/render/skinning.wesl)).
Textures were the answer when vertex shaders had no buffer access; on WebGPU
core they add addressing code and a format dependency for no gain.

## 5. Morph target storage and target-count ceiling

glTF requires deltas for POSITION, NORMAL and TANGENT applied before any
skinning or node transform, states the target count is unlimited, and asks
implementations to support at least eight morphed attributes, optionally using
only the eight highest-weighted (spec 3.7.2.2). Three layouts exist:

| Layout | Ceiling | Notes |
|---|---|---|
| Extra vertex attributes per target (classic WebGL) | 16 attribute slots minus ~6 base ones: 3 targets with P+N+T, 10 with P only | Static; every target costs vertex-fetch bandwidth even at weight 0 |
| Texture array / 3D texture, one layer per target | 256 layers (`maxTextureArrayLayers`); 2048 depth for 3D | Bevy's original design (PR #8158) chose R32Float 3D textures because WebGL2 has no vertex-stage storage buffers; three.js uses a `DataArrayTexture` for the same reason |
| Storage buffer, target-major `array<MorphVertex>` | Bounded by binding size and the weight array | Bevy's current default when storage buffers exist: `{position, normal, tangent}` as three padded vec3 (48 B), loop over targets, `continue` on weight 0 |

Per-vertex morph cost for T active targets is 3T vec4 loads and 9T MADs; T = 8
equals the N = 1 skinning row above. Target-major is the right order: adjacent
lanes read adjacent vertices of the same target, which coalesces. Expand glTF
sparse accessors at load; `@builtin(vertex_index)` indexes the delta stream
directly when each primitive owns its buffers. Renormalize normals and tangents
after morphing. Memory is 48 B x vertices x targets: a 10k-vertex face with 52
ARKit shapes is 25 MiB, which is why the padded layout should give way to
packed f16 (24 B) if the residency budget bites, not to textures.

Ceiling: unlimited targets in the buffer; weights per draw capped by the
instance record. 64 weights (256 B per instance) covers ARKit's 52 and Bevy's
uniform-path limit; targets past the cap are dropped by lowest weight, as the
glTF spec permits.

## 6. Constraints from WebGPU

The backend inventory (#4) is still open; these are the spec facts it should
confirm against the vendored `gogpu/wgpu` and the browser path.

- `maxStorageBuffersPerShaderStage` defaults to 8, and core mode gives the
  vertex stage all 8 via `maxStorageBuffersInVertexStage`. Scene's budget:
  instances 1, poses 1, morph deltas 1, optional second weight set 0 (packed in
  the instance record), leaving room for lights and custom shaders.
- `maxStorageBufferBindingSize` defaults to 128 MiB and `maxBufferSize` to
  256 MiB; `minStorageBufferOffsetAlignment` is 256 B, so per-clip sub-ranges
  should be addressed by index from one buffer, not by binding offsets.
- Compatibility mode (`featureLevel: "compatibility"`, Chrome's path for
  GL/D3D11-class devices) sets `maxStorageBuffersInVertexStage` and
  `maxStorageTexturesInVertexStage` to 0 and `maxTextureDimension2D` to 4096.
  Vertex-shader skinning from storage buffers therefore requires a core
  adapter; per the map's "no fallback paths", request core and fail
  initialization otherwise. Record this in #4.
- Uniform buffers are capped at 64 KiB per binding (1365 4x3 matrices): enough
  for one resolved palette, not for baked clips. Storage buffers are required.
- WGSL storage arrays of `f32` have stride 4, so a tight 40 B TRS record is
  legal via scalar indexing; `vec3` inside a struct pads to 16 B. Loop bounds
  over N plays and T targets are runtime values from the instance record; this
  is uniform control flow per draw and fine.
- No float texture formats exist in gfx today; the storage-buffer design does
  not need any.

## 7. Recommended defaults

- Pose representation: TRS, 3 x vec4 per bone-frame (rotation quaternion;
  translation + scale.x; scale.yz + 2 spare floats), f32, baked as
  `globalJoint * inverseBind` per (skin, clip) with within-clip hemisphere
  continuity. One storage buffer per model holding all clips; a small CPU-side
  table maps clip and frame to a bone-row base index.
- Sample rate: 30 Hz, linear interpolation between floor and ceil frames, last
  frame duplicated for clamped clips and wrapped for loops. Per-clip override
  allowed at load.
- Blend method: fold `weight * (1 - frac)` and `weight * frac` into one scalar
  per record on the CPU; the shader accumulates weighted TRS per influence with
  sign-fixed quaternions, normalizes, builds a 4x3 matrix, and applies standard
  LBS over 4 influences. Plays per draw capped at 4; weights are used as given
  (the caller normalizes if it wants a partition of unity).
- Instance record: per play `{baseRow0: u32, baseRow1: u32, w0: f32, w1: f32}`
  (16 B x 4 plays), so the shader does no clip-length arithmetic.
- Morph storage: one storage buffer per primitive, target-major, 48 B per
  vertex per target (position, normal, tangent deltas as padded vec3), sparse
  accessors expanded at load. Weights live in the instance record, 64 per draw,
  zero weights skipped in the loop; excess targets dropped by lowest weight.
- Order in the vertex shader: morph, then skin, then node/world transform, per
  glTF.
- Target-count ceiling: 64 active weights per draw; unlimited stored targets.
- Deferred, as already listed in #1: per-instance pose-resolve compute pass if
  the skinned demo shows N >= 2 hurting on integrated GPUs; f16 pose and delta
  packing when the residency budget needs it; second joint/weight set
  (`JOINTS_1`) only if a demo asset requires it.

## Sources

- WebGPU spec, [Limits](https://www.w3.org/TR/webgpu/#limits) and the
  [compatibility mode proposal](https://github.com/gpuweb/gpuweb/blob/main/proposals/compatibility-mode.md)
- [glTF 2.0 specification](https://registry.khronos.org/glTF/specs/2.0/glTF-2.0.html), morph targets 3.7.2.2 and skins 3.7.3
- Kavan et al. 2007, 2008 (linked above); [GDC talk](https://www.gdcvault.com/play/524/Skinning-with-Dual)
- GPU Gems 3 chapter 2, Animated Crowd Rendering (linked above)
- Unity Technologies, Animation-Instancing (`AnimationGenerator.cs`, `AnimationInstancingBase.cginc`)
- Bevy [morph.wesl](https://github.com/bevyengine/bevy/blob/main/crates/bevy_pbr/src/render/morph.wesl), [mesh_types.wesl](https://github.com/bevyengine/bevy/blob/main/crates/bevy_pbr/src/render/mesh_types.wesl), [PR #8158](https://github.com/bevyengine/bevy/pull/8158)
- three.js [Morph.js](https://github.com/mrdoob/three.js/blob/dev/src/nodes/accessors/Morph.js), [Skinning.js](https://github.com/mrdoob/three.js/blob/dev/src/nodes/accessors/Skinning.js)
- SideFX Labs [Vertex Animation Textures 3.0](https://www.sidefx.com/docs/houdini/nodes/out/labs--vertex_animation_textures-3.0.html)
