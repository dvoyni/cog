# Render paths over gfx and the wgpu fork: what fits, and what each costs

This file answers
[draw research: which render paths fit wgpu, and what forward+, deferred and a visibility buffer each cost](https://github.com/dvoyni/cog/issues/802)
on the [draw map](https://github.com/dvoyni/cog/issues/800). It records facts
for the render-path session. It does not choose a path.

**Read on 2026-11-12, at these versions:**

- the wgpu fork `dvoyni/wgpu` at [`5a49474`][fork] (v0.34.13 plus cog's fixes),
  with gputypes v0.8.2 and naga v0.21.10 as its `go.mod` pins them. `go.work`
  builds this fork;
- gfx's declarations on cog at `f3d0dcc`;
- the WebGPU editor's draft, `gpuweb/gpuweb` at `10be35c`;
- Unity Graphics `master` at `0efc28e`, with the Unity 6.0 and 6.3 manuals;
  Godot `master` at `e8fe0a1`, with the 4.5 and stable docs; Bevy v0.20.1;
  Filament `main` at `32084eb`; three.js r187.

**How to read the marks:**

- **Fits** means at WebGPU's default limits, which gfx requests when
  `RequiredLimits` is zero ([gfx design][gfx-limits]).
- **(derived)** marks arithmetic or reasoning done in this file, not a claim
  a source makes.
- **Not verified** marks something that no source or code read here settled.

---

## 1. The stack under draw, at the fork's head

### 1.1 The fork is past v0.34.2

[#660] and [#661] measured v0.34.2. gfx's design still says that version is
pinned ([design.md L81][gfx-pin]). But `gogpu/go.mod` requires v0.34.13
([L14][gogpu-mod]), and `go.work` builds `./submodules/wgpu`. Since v0.34.2
the fork gained three things a render path can use:

| What | Since | Works on | In gfx today |
|---|---|---|---|
| Query sets, timestamp writes, resolve | API in v0.34.3; working since v0.34.7 ([CHANGELOG L228][cl-ts]) | Vulkan (when the queue reports at least 36 `timestampValidBits`) and DX12. Not Metal ([L255][cl-ts-metal]) | Declared. `CreateQuerySet` answers `ErrQuerySetsUnavailable` ([design][gfx-ts]) |
| Render bundles | API in v0.34.4 ([L454][cl-bundle-api]); working since v0.34.7. Core records a command list and replays it into the pass ([L291][cl-bundle]) | every backend | Left out, "absent upstream" ([design L174][gfx-leftout]) |
| `MultiDrawIndirectCount`, `MultiDrawIndexedIndirectCount` | v0.34.3 ([L491][cl-count]) | Vulkan 1.2+ only ([vulkan/api.go L564-L567][vk-feat]) | Left out ([design L174][gfx-leftout]) |

The map's "no indirect-count draws and no render bundles" is true of gfx. It
is no longer true of the fork.

### 1.2 Features each backend offers

The table shows what the fork's adapters advertise at `5a49474`, and whether
gfx's `FeatureFlags` can name the feature at all ([gfx/enums.go L82-L101][gfx-feat]).

| Feature | Vulkan | DX12 | Metal | Name in gfx |
|---|---|---|---|---|
| `indirect-first-instance` | if `drawIndirectFirstInstance` | no | no | yes |
| multi-draw indirect with more than one draw | if `multiDrawIndirect` | always | no | yes |
| multi-draw indirect with a count buffer | on Vulkan 1.2+ | no | no | no |
| `depth32float-stencil8` | always | from feature level 12_0 | no | yes |
| `depth-clip-control` | if `depthClamp` | no | always | yes |
| `rg11b10ufloat-renderable` | no | no | no | yes |
| `float32-filterable` | no | no | no | yes |
| `timestamp-query` | if the queue has the bits | always | no | own constant, never reported |
| `shader-f16` | if supported | only on the opt-in DXIL path with SM 6.2 | no | yes |
| subgroups | if supported | no | no | yes |
| `@builtin(primitive_index)` | if `geometryShader` | on the default FXC path | no | no (gputypes v0.8.0 lacks it) |
| 64-bit buffer atomics, min/max | if buffer and shared int64 atomics | no | no | no |
| barycentrics, `draw_index` | if supported | no | no | no |
| push constants / immediates | none | none | none | no |
| binding arrays | none: `BindGroupLayoutEntry` has no count ([binding.go L14][gt-bgle]) | none | none | no |

Sources for the table:

- Vulkan: [api.go L542-L588][vk-feat] and [feature_support.go L222-L320][vk-feat2].
- DX12: [adapter.go L257-L284][dx-feat] and [shader_capabilities.go L25-L38][dx-shcap].
- Metal: [api.go L180-L185][mtl-feat].

What the fork's core enforces:

- **`rg11b10ufloat` as a render attachment** needs its feature at texture
  and pipeline creation ([feature_format.go L56-L62][core-rg11]). No desktop
  backend offers it; only GLES does. So no desktop backend in the fork
  renders to RG11B10 today.
- **Filtering `r32float`, `rg32float` and `rgba32float`** needs
  `float32-filterable` ([format_filterable.go][core-f32]).
- **`MultiDrawIndirect` with a count above 1** needs its feature
  ([renderpass_native.go L280-L290][rp-mdi]). On Metal the encoder fails.
- **A non-zero `firstInstance` in indirect arguments** is passed through
  unchecked ([L236-L238][rp-fi]). The fork does not apply the spec's rule
  that such a draw is a no-op without the feature ([spec][spec-fi]).
  Whether DX12 and Metal honour it is not verified on hardware.

### 1.3 Limits: WebGPU's defaults and what each adapter reports

| Limit | WebGPU default ([spec][spec-limits]) | Vulkan in the fork | DX12 in the fork | Metal in the fork |
|---|---|---|---|---|
| `maxColorAttachments` | 8 | device value, capped at 8 | 8 | 8 |
| `maxColorAttachmentBytesPerSample` | 32 | 32 (the default, not raised) | 32 (the default) | 128 |
| `maxStorageBuffersPerShaderStage` | 8 (fragment: 8 in core, 4 in compatibility mode) | device value | 64 | 8 |
| `maxStorageTexturesPerShaderStage` | 4 | device value | 64 | 8 |
| `maxSampledTexturesPerShaderStage` | 16 | device value | 128 | 128 |
| `maxComputeWorkgroupStorageSize` | 16384 | device value | 32768 | 32768 |
| `maxComputeInvocationsPerWorkgroup` | 256 | device value | 1024 | 1024 |
| `maxTextureArrayLayers` | 256 | device value | 256 | 2048 |
| `maxUniformBufferBindingSize` | 65536 | device value | 65536 | buffer size |
| `maxImmediateSize` | 64 | no such limit in gputypes | — | — |

Sources: Vulkan [api.go L665-L707][vk-lim]; DX12 [adapter.go L316-L340][dx-lim];
Metal [api.go L116-L145][mtl-lim]; gputypes defaults [limits.go L99-L135][gt-lim];
gfx's mirror of the defaults [descriptors.go L371-L441][gfx-lim].

What the fork does not check, though WebGPU does:

- **Color attachment bytes per sample.** `maxColorAttachmentBytesPerSample`
  appears nowhere in the fork's core. gfx does not check it either, since it
  checks layouts and vertex buffers only ([design][gfx-checks]).
- **Whether a format may be multisampled.** The multisample checks are mip
  count, dimension, layers and storage usage ([validate.go L201-L235][core-ms]).
  Resolve targets are checked against the spec's resolve column
  ([validate_render_attachment.go L231-L243][core-resolve]).
- **Which formats allow `read-write` storage access.** Only the count of
  storage textures per stage is checked ([validate.go L873][core-st]).

### 1.4 What neither WebGPU nor the fork offers, and what WebGPU has that the fork lacks

| Capability | WebGPU spec | Fork | What it would serve |
|---|---|---|---|
| Subpasses, input attachments, framebuffer fetch, pixel local storage | absent (no occurrence in the spec source) | absent | keeping a G-buffer in tile memory |
| `TRANSIENT_ATTACHMENT` usage | present. It may only be combined with `RENDER_ATTACHMENT`, and must be cleared and discarded ([spec L4523][spec-transient]) | absent. gputypes has five usages ([texture.go L801][gt-usage]), and Metal's memoryless mode is declared but unused | MSAA and depth that never leave tile memory; not a G-buffer read by a later pass |
| Immediates (`setImmediates`, 64 bytes) | present | absent, and no push constants | per-draw indices without a buffer |
| Binding arrays / bindless | absent | absent | one draw or dispatch shading every material |
| Multi-draw indirect, indirect count | absent | present as native extensions (1.2) | GPU-driven submission |
| `texture-formats-tier1`/`tier2` (snorm and 16-bit unorm render targets, more `read-write` storage formats) | present | absent from gputypes ([validate_render_attachment.go L231-L235][core-resolve]) | packing G-buffers |
| `float32-blendable`, `dual-source-blending` | present | absent from gputypes | — |
| `atomic-vec2u-min-max` (64-bit atomic min/max in WGSL) | present ([spec][spec-features]) | absent. Vulkan offers int64 buffer atomics under another name | software rasterization into a visibility buffer |
| `primitive-index` | present | Vulkan and DX12 only, and not in gfx | writing a triangle ID |

MSAA sample counts are 1 and 4 only, in the spec and in the fork
([validate.go L90][core-ms-count]). The spec has no depth or stencil resolve.

---

## 2. Paths against the device

### 2.1 The matrix

| Path | Verdict at the defaults | Color attachments and bytes per sample | Storage and compute | MSAA | Depth and stencil | What it would use that the fork lacks |
|---|---|---|---|---|---|---|
| **Forward**, a bounded light list per draw (v1, URP Forward, Godot Mobile and Compatibility) | **Fits** | 1 HDR target (`rgba16float`: 8 B) | Lights in a uniform (64 KiB) or storage buffer. Per-draw data through dynamic offsets (8 dynamic uniform buffers per layout) or `instance_index` into a buffer | 4x, resolvable to `rgba16float` | any | push constants or immediates for the per-draw index |
| **Tiled forward+** (Harada 2012) | **Fits** | as forward | Compute culling per tile. A 16x16 tile at one invocation per pixel is 256 invocations, the default maximum, and its list fits in 16 KiB of workgroup memory (derived; Harada used 8x8 tiles). Fragment reads a few storage buffers of 8: Bevy's clustered forward needs 3 | 4x | needs a depth prepass for each tile's depth bounds | subgroups (Vulkan only) to cut culling cost |
| **Clustered forward+** (Olsson 2012) | **Fits**, in all three binning styles | as forward | Binning on the CPU into a uniform or storage buffer (URP, Filament), in compute (three.js, Olsson), or by rasterizing light proxies with fragment-stage `atomicOr` into a storage buffer, then a compute pass (Godot, Bevy). Bevy's fragment stage takes 3 storage buffers | 4x | dense grids need no prepass; Olsson's unique-cluster variant needs pre-z | — |
| **Classic deferred** (G-buffer, then light volumes or fullscreen) | **Fits, with a G-buffer of at most 32 B per sample** in WebGPU's accounting (section 2.2) | 4 to 5 targets. URP's base layout is exactly 32 B, with `rgba16float` in place of RG11B10 | lighting pass samples 4 to 5 G-buffer textures of 16 | possible only by sampling multisampled G-buffers per sample, at 4x the memory (derived). No engine named here ships it | stencil light volumes need `depth24plus-stencil8` (`depth32float-stencil8` is not on Metal); a read-only depth attachment while sampling depth is supported | subpasses or transient attachments, which keep the G-buffer on tile. Without them the G-buffer always goes through memory (section 4.3) |
| **Tiled or clustered deferred** (compute over the G-buffer; URP Deferred+) | **Fits** | as classic | a compute pass writes the result to a write-only `rgba16float` storage texture (core), 1 of 4 storage textures per stage | as classic | as classic | as classic |
| **Visibility buffer, hardware raster** (Burns and Hunt 2013, The Forge) | **Fits with workarounds** | `r32uint` (4 B), plus motion if wanted. `r32uint` is not multisampleable in WebGPU; `rg16uint` and `rgba8uint` are (section 2.2) | vertex pulling from storage buffers in the resolve; per-material work lists, which `DispatchIndirect` can consume (derived) | needs an `rg16uint` or `rgba8uint` ID target (derived) | depth only | `primitive_index` (not on Metal, not in gfx), so the ID comes from de-indexed or flat-interpolated vertex data (derived). Binding arrays: without them, a pass per material (section 3.4). Indirect count: Vulkan only. Multi-draw: not on Metal. Compute has no derivatives, so texture gradients are computed analytically ([Burns and Hunt][vb]) |
| **Visibility buffer, software raster** (Nanite-style; Bevy's meshlets) | **Does not fit** | — | 64-bit atomic min/max into the ID buffer. Bevy requires `TEXTURE_INT64_ATOMIC`, `TEXTURE_ATOMIC`, `SHADER_INT64`, `SUBGROUP` and `IMMEDIATES` ([meshlet/mod.rs L120-L126][bevy-meshlet]) | not supported by Bevy's meshlets | — | Texture atomics: none in the fork. Int64 buffer atomics: Vulkan only, and gfx cannot request them. WebGPU's `atomic-vec2u-min-max`: not in gputypes. Immediates: none |

Sources for the matrix rows:

- Limits: [the WebGPU limits table][spec-limits].
- Storage texture formats: [plain color formats][spec-formats].
- Bevy's 3 fragment storage buffers: [cluster/mod.rs L39][bevy-sb].
- Godot's raster binning: [cluster_builder_rd.cpp L474-L506][godot-cb] and
  [cluster_render.glsl L151-L152][godot-cr].
- Harada's tile size and depth prepass: [Forward+][harada].
- Olsson's pre-z requirement: [Clustered Shading][olsson].

### 2.2 G-buffer budgets in WebGPU's accounting

WebGPU counts color attachments by **render target pixel byte cost**, not by
memory footprint. The cost is summed in attachment order, each format first
rounded up to its component alignment
([algorithm][spec-bps], [table][spec-formats]):

| Format | Cost (B) | Alignment (B) | Memory (B per texel) | Multisample in core | Resolve |
|---|---|---|---|---|---|
| `rgba8unorm`, `rgba8unorm-srgb`, `bgra8unorm` | 8 | 1 | 4 | yes | yes |
| `rgba8uint` | 4 | 1 | 4 | yes | no |
| `rgb10a2unorm` | 8 | 4 | 4 | yes | yes |
| `rg11b10ufloat` | 8 | 4 | 4 | only with `rg11b10ufloat-renderable`, which the fork's desktop backends lack | only with that feature |
| `r16float`, `rg16float`, `rgba16float` | 2, 4, 8 | 2 | 2, 4, 8 | yes | yes |
| `rg16uint` | 4 | 2 | 4 | yes | no |
| `r32uint`, `r32float` | 4 | 4 | 4 | `r32uint`: no; `r32float`: yes | no |
| `rg32uint`, `rgba32uint`, `rgba32float` | 8, 16, 16 | 4 | 8, 16, 16 | no | no |

Packing into `uint` formats halves the cost of 8-bit channels: `rgba8uint`
costs 4 B, `rgba8unorm` costs 8 B (derived, from the table).

Layouts from the engines, summed under that rule (derived):

| Layout | Targets | Cost per sample | Memory per pixel (color only) | Under 32 B? |
|---|---|---|---|---|
| URP Deferred, base ([G-buffer layout][urp-gbuf]) | albedo and flags, specular and occlusion, normal and smoothness (four 8-bit channels each), emissive/GI/lighting `B10G11R11` | 8+8+8+8 = **32** | 16 | yes, exactly. In the fork the lighting target must be `rgba16float` (also 8 B), since RG11B10 is not renderable |
| URP, plus Rendering Layers, ShadowMask or Native RenderPass depth-as-color (`R32_SFloat`) | one more target for each | more than 32 with any one of them, since each adds at least 1 B | — | no |
| Bevy deferred prepass with normals and motion ([prepass/mod.rs L392-L416][bevy-prepass-targets], [deferred/mod.rs L17-L19][bevy-def]) | `rgb10a2unorm` normals, `rg16float` motion, `rgba32uint` packed material, `r8uint` lighting ID | 8 → 12 → 28 → **29** | 25 | yes |
| Arm's Vulkan subpass sample ([README][arm-subpass]) | `rgba8` sRGB lighting, `rgba8` albedo, `rgb10a2` normal | **24** | 12 | yes |
| Filament's estimate for its PBR model ([Filament][filament-doc]) | not given | — | 20 to 24 ("160 and 192 bits per pixel") | — |
| Visibility buffer ([Burns and Hunt][vb]; The Forge packs 1+8+23 bits into one 32-bit target, [Engel 2018][tvb-blog]) | `r32uint` | **4** | 4 | yes |

**Memory at 1920x1080 (derived):** URP's base G-buffer is 16 B per pixel,
about 33 MB at 1x and 133 MB at 4x MSAA. A 4 B visibility buffer is about
8.3 MB. Burns and Hunt report 64 MB for their visibility buffer at 1080p with
8x sampling, against 398 MB for a 24 B per sample G-buffer ([vb][vb]).

**Metal's own budget** for multiple color targets, per pixel per sample, is
32 B on the oldest Apple GPU families. Later families allow 64 B and 128 B
([Metal feature set tables][metal-fst]). Which family has which value did
not come out of the PDF's layout cleanly: not verified per family.

---

## 3. How each path handles lights, transparency, anti-aliasing, materials and post effects

### 3.1 Many lights

| Path | How lights scale | Caps in shipped engines |
|---|---|---|
| Forward | Every light in the draw's list runs for every shaded fragment of it, overdraw included | URP Forward: 9 per object (8 additional plus the main light) ([URP L245-L248][urp-limits], [comparison][urp-cmp]). Godot Mobile and Compatibility: 8 omni plus 8 spot per mesh ([renderers][godot-renderers]). v1: 16 per pass, every fragment runs all of them ([frame.wgsl][v1-frame]) |
| Tiled forward+ | A tile's list includes every light overlapping the tile's depth range. Depth discontinuities in a tile add "large amounts of extraneous work" ([Filament][filament-doc]) | — |
| Clustered forward+ | Each fragment loops over its cluster's list. Olsson: the crossover with tiled is "at most around 2k lights", and clustered "is very competitive even for cases with very few lights" ([Olsson][olsson]) | URP Forward+: 256 visible lights on desktop, 32 on mobile, GL and WebGPU. URP's source says "WebGPU's minimal limits are based on mobile rather than desktop" ([URP L253-L265][urp-limits], [ShaderConfig.cs][urp-shcfg]). Godot Forward+: 512 per cluster by default ([renderers][godot-renderers]). Filament: 255 lights in total, 16 depth slices ([EngineEnums.h L113][fil-enums], [Froxelizer.cpp L87][fil-frox]). three.js `ClusteredLighting`: 1024 lights and 64 per cluster by default ([ClusteredLighting.js][three-cl]). Bevy: storage buffers that it resizes when they overflow ([bevy_light cluster/mod.rs L55-L75][bevy-lcl]), 204 lights in the uniform fallback ([cluster/mod.rs L31][bevy-sb]) |
| Classic deferred | Each light shades only the pixels its volume covers, but reads the G-buffer again per light | URP Deferred: unlimited for opaque objects ([comparison][urp-cmp]); stencil light volumes, a sphere or hemisphere mesh per light ([DeferredLights.cs L593, L924-L928][urp-dl]) |
| Tiled or clustered deferred | The G-buffer is read once per pixel. Lights are looped per tile or cluster | URP Deferred+: a fullscreen draw per lighting model over clustered lists ([DeferredLights.cs L744][urp-dl]) |
| Visibility buffer | The resolve does the lighting. The Forge's "Forward++" applies a tiled light list ([The Forge v1.57][forge-157]) | — |

### 3.2 Transparency

| Path | Transparent surfaces |
|---|---|
| Forward, tiled and clustered forward+ | The same shaders and light lists. A tile's depth bounds come from opaque depth, so a tiled list can miss lights in front of the opaque surface; clustered grids cover every depth (derived) |
| Deferred, classic and tiled | A forward pass after lighting. URP Deferred: 9 lights per transparent object ([comparison][urp-cmp]); Deferred+ "falls back to Forward+ for transparency" (Unity staff, [forum][urp-forum]) |
| Visibility buffer | Not handled by the technique ([Burns and Hunt][vb]: "We address all of these problems except transparency"). The Forge sorts triangle IDs in per-pixel linked lists for order-independent transparency ([v1.57][forge-157]) |

### 3.3 Anti-aliasing

| Path | MSAA | Other AA |
|---|---|---|
| Forward and forward+ | Native, at 4x in WebGPU. URP Forward and Forward+: yes ([comparison][urp-cmp]). Godot: every renderer ([renderers][godot-renderers]). Bevy: `Msaa::Sample4` by default ([view/mod.rs L248-L253][bevy-msaa]). Filament: yes ([Filament][filament-doc]) | FXAA, SMAA, TAA as post passes, fed by depth and motion vectors |
| Deferred | URP Deferred: no ([comparison][urp-cmp]). Bevy deferred: "doesn't support MSAA" ([Bevy 0.12][bevy-012]). Godot rejected deferred because it "requires using hacks to be able to use MSAA" ([architecture][godot-arch]) | TAA, FXAA, SMAA |
| Visibility buffer | Burns and Hunt's gains grow with the sample count, up to 8 samples, most on bandwidth-limited integrated GPUs ([vb][vb]). In WebGPU an MSAA ID target must be `rg16uint` or `rgba8uint` (derived, section 2.2). Bevy's meshlets: not compatible with MSAA ([meshlet/mod.rs L97-L98][bevy-meshlet]) | TAA |

### 3.4 Material variety

| Path | How materials vary |
|---|---|
| Forward and forward+ | Any shader per draw. Olsson: forward "inherently support[s] MSAA, custom material shaders, and sidesteps the issue of G-Buffer storage" ([Olsson][olsson]). Harada: Forward+ "removes the major drawback of deferred techniques, which is a restriction of materials and lighting models" ([Harada][harada]) |
| Deferred | What the G-buffer encodes. URP stores material flags in bits and falls back to forward for shaders without a deferred pass ([G-buffer layout][urp-gbuf], [comparison][urp-cmp]). Bevy writes a lighting-pass ID (`r8uint`), copies it into a `depth16unorm` texture and depth-tests one pass per ID ([deferred/mod.rs L17-L19][bevy-def]) |
| Visibility buffer | The resolve evaluates materials. Burns and Hunt dispatch one kernel per material over the tiles that hold it ([vb][vb]). Bevy's meshlets draw "1 fullscreen triangle draw per material" against a material depth buffer ([material_shade_nodes.rs L98][bevy-mshade]). The Forge shades everything in one draw through "bindless texture arrays" ([Engel 2018][tvb-blog], [v1.57][forge-157]), which the fork cannot do |

### 3.5 What each path hands to post effects

| Path | Depth | Normals | Motion vectors |
|---|---|---|---|
| Forward and forward+ | from the main pass or a prepass | an extra target or a prepass. Godot writes depth, normal and roughness in a prepass, forced by SSAO, SSR, SSIL or screen-space contact shadows ([render_forward_clustered.cpp L2198-L2199][godot-prepass]). Bevy has an `rgb10a2unorm` normal prepass ([prepass/mod.rs L59][bevy-prepass]). three.js's SSR example adds `normal` and `metalrough` as extra targets of the scene pass ([webgpu_postprocessing_ssr.html L148-L151][three-ssr]) | a prepass target (Bevy: `rg16float`, [prepass/mod.rs L60][bevy-prepass]; Godot: motion vectors in its passes, [L1886-L1890][godot-mv]) |
| Deferred | in the G-buffer pass | in the G-buffer | an extra target (Bevy writes it beside its deferred target, [prepass/mod.rs L392-L416][bevy-prepass-targets]) |
| Visibility buffer | from the raster's depth attachment | reconstructed in the resolve. Bevy has a fullscreen pass that writes prepass textures from the visibility buffer ([material_shade_nodes.rs L113-L114][bevy-mshade]) | reconstructed likewise |

An MSAA depth buffer cannot be resolved in WebGPU. Godot resolves its MSAA
depth prepass with a pass of its own ([L2230-L2231][godot-prepass]).

---

## 4. Published costs

No source measures any of these paths on gogpu/wgpu. Since v0.34.7 the
fork's timestamps work on Vulkan and DX12 (section 1.1); gfx does not reach
them yet. The measurements below are old: 2012 and 2013 hardware, and one
2019 phone.

### 4.1 Discrete desktop GPUs

| Source | Hardware | Setup | Result |
|---|---|---|---|
| [Olsson, Billeter, Assarsson, HPG 2012][olsson] | NVIDIA GTX 480; resolution not stated | the view in its Figure 1, about 2,400 lights | clustered 17 ms (2.3 ms clustering, 1.5 ms light assignment, 5.6 ms shading) against tiled 26 ms (1.0 ms assignment, 17.7 ms shading) |
| same | same | Crytek Sponza, 10k random lights | deferred 97.1 ms in total; tiled forward 23.6 ms. The clustered variants' stage times are in a figure only |
| same | same | light assignment, clustered against tiled | 32 lights: 0.71 against 0.24 ms. 1,024: 0.73 against 0.51 ms. 32,768: 1.42 against 9.31 ms. 1,048,576: 5.73 against 341.56 ms. "At 1M lights ... over 35 fps" |
| same | same | Necropolis fly-through | ClusteredForward performs "similar to TiledDeferred" |
| [Harada, McKee, Yang, EG 2012][harada] | AMD Radeon HD 6970 | 3,072 lights, 1280x720, 8x8 tiles | Forward+ faster than compute-based deferred in total. Forward+ wins the prepass and light processing; deferred wins final shading. With the memory clock lowered, "the gap ... widened". Times are in a figure only |
| same, analysis | — | memory traffic | deferred minus Forward+ traffic is N·F·(15 − M·(1/T + 1 + L)), with N pixels, F the size of a float, M the average lights per tile, T the tile size and L a light's size. The paper concludes that "in theory" no deferred variant "can beat Forward+ in terms of memory traffic" |
| [Burns and Hunt, JCGT 2013][vb] | AMD Radeon HD 5750 (73.6 GB/s), GeForce GTX 560 Ti (128 GB/s) | 1080p, 1 to 8 samples, three scenes | "little to no net benefit" at 1 sample. Gains grow with samples. On the small-triangle scene, "the platforms with the smallest caches never benefit". Speedups are in a figure only |

### 4.2 Integrated GPUs

| Source | Hardware | Result |
|---|---|---|
| [Burns and Hunt][vb] | Intel HD 4000 (25.6 GB/s); Intel Iris Pro 5200 (25.6 GB/s plus 128 MB eDRAM) | The visibility buffer's gains are largest here: "particularly on the bandwidth-constrained Intel platforms", "and particularly so with the Intel Iris Pro" |
| [Harada][harada] | AMD A8-3510MX (Radeon HD 6620G) | the results "confirm the theoretical analysis of Forward+ on an integrated GPU"; times in a figure only |

### 4.3 Tile-based GPUs

| Source | Hardware | Result |
|---|---|---|
| [Arm, Vulkan-Samples "Subpasses"][arm-subpass] | Mali-G76 (Galaxy S10), 2220x1080, 16x16 tiles | Deferred as one render pass of two subpasses against two render passes: "approximately a 55% reduction in the number of tiles used (262.2k/s vs 614.7k/s), and therefore a 55% reduction in bandwidth". Without transient, lazily allocated attachments, fragment jobs double (56/s to 113/s). Mali merges subpasses only within 128 bits per pixel of tile color storage, 256 on Mali-G72 and later |
| [Godot internal architecture][godot-arch] | mobile GPUs in general | Forward+ "is substantially less efficient on mobile". The Mobile renderer uses `R10G10B10A2` UNORM instead of the `RGBA16F` that Forward+ uses, which "halves the bandwidth required" |
| [Unity URP comparison][urp-cmp] | mobile in general | Forward and Forward+: "Low performance impact"; Deferred: "High performance impact, because Unity adds extra render passes to render the G-buffer" |
| [Filament][filament-doc] | mobile-first rationale | a 160 to 192 bits per pixel G-buffer "would translate directly to rather high bandwidth requirements" |

What this means for the fork (derived): WebGPU has no subpasses, and the
fork has no transient attachments (section 1.4). So a deferred path over gfx
always takes the "two render passes" case above, the one that uses more
bandwidth. Only Mali was measured; Apple GPUs are tile-based too.

### 4.4 Claims without extractable numbers

- The Forge reports "considerable performance gains due to reduced memory
  bandwidth compared to traditional G-buffer based rendering" for its
  visibility buffer ([v1.57][forge-157]). Its tables are images in
  [Engel 2018][tvb-blog]: **not verified**.
- Bevy's meshlets: "Much greater base overhead" than its standard renderer
  for small amounts of geometry ([meshlet/mod.rs L89][bevy-meshlet]). No
  numbers given.

---

## 5. What each engine chose, and why

| Engine | Paths shipped | Default | How lights are binned | Why, in its own words |
|---|---|---|---|---|
| **Unity URP** (Unity 6.x) | **Four:** Forward, Forward+, Deferred, Deferred+ (6.1 and later) | Forward | Forward+: CPU Burst jobs (`LightMinMaxZJob`, `ZBinningJob`, `TilingJob`) build per-tile bitmasks and depth bins ([ForwardLights.cs L308-L435][urp-fl]). Deferred: stencil light volumes. Deferred+: clustered lights in a fullscreen draw ([DeferredLights.cs][urp-dl]) | Forward "if you don't have many lights ... or you're rendering on a mobile or low-end platform". Forward+ when "the Deferred rendering path renders too slowly". Deferred is out when you need Rendering Layers or accurate terrain blending. No MSAA in deferred ([comparison][urp-cmp], [6.3][urp-cmp63]). Deferred+ adds "GPU Resident Drawer and GPU occlusion support" ([6.1 what's new][urp-61]) |
| **Godot 4** | **Three renderers,** one per project: Forward+ (Vulkan, D3D12, Metal), Mobile (same APIs), Compatibility (OpenGL) | Forward+ on desktop, Mobile on mobile, Compatibility on the web ([renderers][godot-renderers]) | Forward+: light, probe and decal proxies (sphere, cone, box) are rasterized into 32 px tiles with a 32-bit depth mask per element, via fragment `atomicOr` ([cluster_builder_rd.h L193][godot-cbh], [cluster_builder_rd.cpp L474-L506][godot-cb], [cluster_render.glsl L120-L157][godot-cr]). Mobile: per-mesh lists, and subpasses within one render pass where its effects allow ([render_forward_mobile.cpp L288, L904-L915][godot-mobile]) | "While deferred rendering can be faster in some cases, it's also less flexible and requires using hacks to be able to use MSAA. Since games with a less realistic art style can benefit a lot from MSAA, we chose to go with forward rendering for Godot 4" ([architecture][godot-arch]) |
| **Bevy** 0.20 (on wgpu) | **Forward and deferred, chosen per material and mixable in one scene** (deferred since 0.12), plus an experimental visibility buffer for meshlets | Forward, MSAA 4x | Clustered: 4,096 clusters and 24 depth slices by default ([bevy_light cluster/mod.rs L297-L307][bevy-lcl]). On the GPU since 0.19 ([bevy#23036](https://github.com/bevyengine/bevy/pull/23036)): compute z-slicing, a rasterized count, a prefix sum, a rasterized fill. Its source notes "256 is the maximum workgroup size in `wgpu`" ([gpu.rs L1-L48][bevy-gpu]). CPU fallback without compute or storage buffers, and on Android ([cluster/mod.rs L79-L95][bevy-sb]) | "for most use cases, we recommend using forward by default, unless a feature explicitly needs deferred". Deferred "doesn't support MSAA" and uses more texture bandwidth ([Bevy 0.12][bevy-012]). Meshlets: "Vulkan and Metal backends" only, no MSAA ([meshlet/mod.rs L80-L103][bevy-meshlet]) |
| **Filament** | **One:** clustered forward | — | Froxels assigned on the CPU across its job system, 16 slices, light records in UBOs ([Froxelizer.cpp L76-L87, L720-L731][fil-frox]). Its design doc also describes a compute variant ([Filament][filament-doc]) | Goals: "Low bandwidth requirements; Multiple dynamic lights per pixel", plus "MSAA; Transparency; Multiple material models". Deferred is "unfortunately very expensive in terms of bandwidth" ([Filament][filament-doc]) |
| **three.js** `WebGPURenderer` r187 | **Forward,** with opt-in lighting systems: `DynamicLighting` (uniform arrays, so a change in light count recompiles nothing) and `ClusteredLighting` (Forward+, added April 2026). `TiledLighting` was added in October 2024 and removed in June 2026 | Forward, each light built into the material's shader | `ClusteredLighting`: a compute pass bins into 32 px tiles × 24 exponential slices; light data in a float `DataTexture` ([ClusteredLightsNode.js L20-L84, L357][three-cln], [ClusteredLighting.js][three-cl], [DynamicLighting.js][three-dl]) | "Suitable for 3D scenes with many point and spot lights and real depth complexity"; for `DynamicLighting`, "so light count changes do not recompile materials" ([sources][three-cl]) |
| **cog v1** (`_legacy`) | **One:** naive forward | — | none: up to 16 punctual lights in a storage buffer, plus a sun and hemispheric ambient; no shadows | "lightCount bounds a loop every shaded fragment runs whole, so adding a light costs every shaded pixel in the pass" ([frame.wgsl][v1-frame]) |

**Several paths in one engine:**

- URP ships four, selected per renderer asset.
- Godot ships three, but a project runs one.
- Bevy mixes forward and deferred per material in one frame.
- three.js swaps its lighting system per renderer.
- Filament ships one.

One more prior art, outside the named engines:

- DOOM (2016) prepared its clustered structure as jobs: its slides' pseudo-code
  runs "1 job per depth slice" ([Sousa, SIGGRAPH 2016][idtech6]). Bevy cites
  the talk for its z-slicing ([bevy_light cluster/mod.rs L30-L37][bevy-lcl]).

---

## 6. Not verified, and gaps in this file

- **No measurement on the stack.** Nothing here is measured on gogpu/wgpu,
  on any backend.
- **Hardware support the fork does not advertise.**
  - Whether DX12 and Metal honour a non-zero `firstInstance` in indirect
    arguments, which the fork passes through unchecked.
  - Whether DX12 and Metal render to RG11B10 if the fork offered the feature.
- **Behaviour the fork does not validate.** What the backends do with an
  `r32uint` MSAA texture, or a `read-write` storage texture outside `r32*`.
- **Metal's per-family budget.** Its per-pixel color budget for each Apple
  family (section 2.2).
- **Numbers held in figures.** The Olsson, Harada, Burns and Hunt figures
  and The Forge's tables hold numbers that the text extraction could not
  read.
- **Resolution of Olsson's measurements.** It is not stated in the text.
- **Intel's "Forward Clustered Shading" sample** (2014) answers 404 at intel.com.
- **Unity 6 manual reorganization.** The manual was being reorganized, and
  several deferred pages returned 404. Deferred+ internals come from source
  and a staff forum post, not from the manual.

---

## Sources

[#660]: https://github.com/dvoyni/cog/issues/660
[#661]: https://github.com/dvoyni/cog/issues/661
[fork]: https://github.com/dvoyni/wgpu/tree/5a494744afef6cb97f0306790962c61ef02fb3f7
[gfx-limits]: https://github.com/dvoyni/cog/blob/f3d0dcc5e1222a46f400e2e0a5ad708414b0eb77/gfx/docs/design.md#L134-L136
[gfx-pin]: https://github.com/dvoyni/cog/blob/f3d0dcc5e1222a46f400e2e0a5ad708414b0eb77/gfx/docs/design.md#L81-L82
[gfx-leftout]: https://github.com/dvoyni/cog/blob/f3d0dcc5e1222a46f400e2e0a5ad708414b0eb77/gfx/docs/design.md#L174-L177
[gfx-ts]: https://github.com/dvoyni/cog/blob/f3d0dcc5e1222a46f400e2e0a5ad708414b0eb77/gfx/docs/design.md#L418-L426
[gfx-checks]: https://github.com/dvoyni/cog/blob/f3d0dcc5e1222a46f400e2e0a5ad708414b0eb77/gfx/docs/design.md#L218-L230
[gfx-feat]: https://github.com/dvoyni/cog/blob/f3d0dcc5e1222a46f400e2e0a5ad708414b0eb77/gfx/enums.go#L82-L101
[gfx-lim]: https://github.com/dvoyni/cog/blob/f3d0dcc5e1222a46f400e2e0a5ad708414b0eb77/gfx/descriptors.go#L371-L441
[gogpu-mod]: https://github.com/dvoyni/cog/blob/f3d0dcc5e1222a46f400e2e0a5ad708414b0eb77/gogpu/go.mod#L14
[v1-frame]: https://github.com/dvoyni/cog/blob/f3d0dcc5e1222a46f400e2e0a5ad708414b0eb77/_legacy/bundles/model/internal/builtin/model/frame.wgsl#L19-L36
[cl-ts]: https://github.com/dvoyni/wgpu/blob/5a494744afef6cb97f0306790962c61ef02fb3f7/CHANGELOG.md#L228
[cl-ts-metal]: https://github.com/dvoyni/wgpu/blob/5a494744afef6cb97f0306790962c61ef02fb3f7/CHANGELOG.md#L255
[cl-bundle]: https://github.com/dvoyni/wgpu/blob/5a494744afef6cb97f0306790962c61ef02fb3f7/CHANGELOG.md#L291
[cl-bundle-api]: https://github.com/dvoyni/wgpu/blob/5a494744afef6cb97f0306790962c61ef02fb3f7/CHANGELOG.md#L454
[cl-count]: https://github.com/dvoyni/wgpu/blob/5a494744afef6cb97f0306790962c61ef02fb3f7/CHANGELOG.md#L491
[vk-feat]: https://github.com/dvoyni/wgpu/blob/5a494744afef6cb97f0306790962c61ef02fb3f7/hal/vulkan/api.go#L542-L588
[vk-feat2]: https://github.com/dvoyni/wgpu/blob/5a494744afef6cb97f0306790962c61ef02fb3f7/hal/vulkan/feature_support.go#L222-L320
[vk-lim]: https://github.com/dvoyni/wgpu/blob/5a494744afef6cb97f0306790962c61ef02fb3f7/hal/vulkan/api.go#L665-L707
[dx-feat]: https://github.com/dvoyni/wgpu/blob/5a494744afef6cb97f0306790962c61ef02fb3f7/hal/dx12/adapter.go#L257-L284
[dx-lim]: https://github.com/dvoyni/wgpu/blob/5a494744afef6cb97f0306790962c61ef02fb3f7/hal/dx12/adapter.go#L316-L340
[dx-shcap]: https://github.com/dvoyni/wgpu/blob/5a494744afef6cb97f0306790962c61ef02fb3f7/hal/dx12/shader_capabilities.go#L25-L38
[mtl-feat]: https://github.com/dvoyni/wgpu/blob/5a494744afef6cb97f0306790962c61ef02fb3f7/hal/metal/api.go#L174-L185
[mtl-lim]: https://github.com/dvoyni/wgpu/blob/5a494744afef6cb97f0306790962c61ef02fb3f7/hal/metal/api.go#L116-L145
[core-rg11]: https://github.com/dvoyni/wgpu/blob/5a494744afef6cb97f0306790962c61ef02fb3f7/core/feature_format.go#L56-L62
[core-f32]: https://github.com/dvoyni/wgpu/blob/5a494744afef6cb97f0306790962c61ef02fb3f7/core/format_filterable.go
[core-ms-count]: https://github.com/dvoyni/wgpu/blob/5a494744afef6cb97f0306790962c61ef02fb3f7/core/validate.go#L90-L97
[core-ms]: https://github.com/dvoyni/wgpu/blob/5a494744afef6cb97f0306790962c61ef02fb3f7/core/validate.go#L200-L235
[core-st]: https://github.com/dvoyni/wgpu/blob/5a494744afef6cb97f0306790962c61ef02fb3f7/core/validate.go#L873-L876
[core-resolve]: https://github.com/dvoyni/wgpu/blob/5a494744afef6cb97f0306790962c61ef02fb3f7/core/validate_render_attachment.go#L227-L243
[rp-fi]: https://github.com/dvoyni/wgpu/blob/5a494744afef6cb97f0306790962c61ef02fb3f7/renderpass_native.go#L236-L238
[rp-mdi]: https://github.com/dvoyni/wgpu/blob/5a494744afef6cb97f0306790962c61ef02fb3f7/renderpass_native.go#L280-L290
[gt-bgle]: https://github.com/gogpu/gputypes/blob/v0.8.2/binding.go#L14-L30
[gt-usage]: https://github.com/gogpu/gputypes/blob/v0.8.2/texture.go#L789-L801
[gt-lim]: https://github.com/gogpu/gputypes/blob/v0.8.2/limits.go#L99-L135
[spec-limits]: https://gpuweb.github.io/gpuweb/#limits
[spec-formats]: https://gpuweb.github.io/gpuweb/#plain-color-formats
[spec-bps]: https://gpuweb.github.io/gpuweb/#abstract-opdef-calculating-color-attachment-bytes-per-sample
[spec-fi]: https://gpuweb.github.io/gpuweb/#dom-gpurendercommandsmixin-drawindirect
[spec-features]: https://gpuweb.github.io/gpuweb/#feature-index
[spec-transient]: https://github.com/gpuweb/gpuweb/blob/10be35c70d7bfc37e0557f2d0e38ec42742aba63/spec/index.bs#L4523-L4526
[olsson]: https://www.cse.chalmers.se/~uffe/clustered_shading_preprint.pdf
[harada]: https://takahiroharada.files.wordpress.com/2015/04/forward_plus.pdf
[vb]: https://jcgt.org/published/0002/02/04/paper.pdf
[tvb-blog]: http://diaryofagraphicsprogrammer.blogspot.com/2018/03/triangle-visibility-buffer.html
[forge-157]: https://github.com/ConfettiFX/The-Forge/releases/tag/v1.57
[arm-subpass]: https://github.com/KhronosGroup/Vulkan-Samples/blob/b4fb432b80234e54cf7be7411f20b65494a5b0f5/samples/performance/subpasses/README.adoc
[metal-fst]: https://developer.apple.com/metal/Metal-Feature-Set-Tables.pdf
[idtech6]: http://advances.realtimerendering.com/s2016/Siggraph2016_idTech6.pdf
[urp-cmp]: https://docs.unity.com/en-us/engine/6000.0/manual/render-pipelines/universal-render-pipeline/introduction/urp-concepts/rendering-paths/comparison
[urp-cmp63]: https://docs.unity.com/en-us/engine/6000.3/manual/render-pipelines/universal-render-pipeline/introduction/urp-concepts/rendering-paths/comparison
[urp-gbuf]: https://docs.unity.com/en-us/engine/6000.0/manual/render-pipelines/universal-render-pipeline/introduction/urp-concepts/rendering-paths/deferred-rendering-path/g-buffer-layout
[urp-61]: https://docs.unity3d.com/6000.1/Documentation/Manual/urp/whats-new/urp-whats-new.html
[urp-forum]: https://discussions.unity.com/t/is-deferred-specific-to-urp-in-upcoming-versions/1596710
[urp-limits]: https://github.com/Unity-Technologies/Graphics/blob/0efc28ea742b1966b9dd9cc4489e58b986a13faf/Packages/com.unity.render-pipelines.universal/Runtime/UniversalRenderPipeline.cs#L245-L286
[urp-shcfg]: https://github.com/Unity-Technologies/Graphics/blob/0efc28ea742b1966b9dd9cc4489e58b986a13faf/Packages/com.unity.render-pipelines.universal-config/Runtime/ShaderConfig.cs#L16-L22
[urp-fl]: https://github.com/Unity-Technologies/Graphics/blob/0efc28ea742b1966b9dd9cc4489e58b986a13faf/Packages/com.unity.render-pipelines.universal/Runtime/ForwardLights.cs#L308-L440
[urp-dl]: https://github.com/Unity-Technologies/Graphics/blob/0efc28ea742b1966b9dd9cc4489e58b986a13faf/Packages/com.unity.render-pipelines.universal/Runtime/DeferredLights.cs#L593-L930
[godot-renderers]: https://docs.godotengine.org/en/stable/tutorials/rendering/renderers.html
[godot-arch]: https://docs.godotengine.org/en/4.5/engine_details/architecture/internal_rendering_architecture.html
[godot-cbh]: https://github.com/godotengine/godot/blob/e8fe0a1b5798b3b17f600840ac4303169528e174/servers/rendering/renderer_rd/cluster_builder_rd.h#L193
[godot-cb]: https://github.com/godotengine/godot/blob/e8fe0a1b5798b3b17f600840ac4303169528e174/servers/rendering/renderer_rd/cluster_builder_rd.cpp#L474-L506
[godot-cr]: https://github.com/godotengine/godot/blob/e8fe0a1b5798b3b17f600840ac4303169528e174/servers/rendering/renderer_rd/shaders/cluster_render.glsl#L120-L157
[godot-prepass]: https://github.com/godotengine/godot/blob/e8fe0a1b5798b3b17f600840ac4303169528e174/servers/rendering/renderer_rd/forward_clustered/render_forward_clustered.cpp#L2198-L2231
[godot-mv]: https://github.com/godotengine/godot/blob/e8fe0a1b5798b3b17f600840ac4303169528e174/servers/rendering/renderer_rd/forward_clustered/render_forward_clustered.cpp#L1886-L1890
[godot-mobile]: https://github.com/godotengine/godot/blob/e8fe0a1b5798b3b17f600840ac4303169528e174/servers/rendering/renderer_rd/forward_mobile/render_forward_mobile.cpp#L904-L940
[bevy-012]: https://bevy.org/news/bevy-0-12/
[bevy-gpu]: https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_pbr/src/cluster/gpu.rs#L1-L48
[bevy-sb]: https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_pbr/src/cluster/mod.rs#L31-L103
[bevy-lcl]: https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_light/src/cluster/mod.rs#L30-L307
[bevy-def]: https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_core_pipeline/src/deferred/mod.rs#L17-L19
[bevy-prepass]: https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_core_pipeline/src/prepass/mod.rs#L59-L60
[bevy-prepass-targets]: https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_core_pipeline/src/prepass/mod.rs#L392-L416
[bevy-meshlet]: https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_pbr/src/meshlet/mod.rs#L80-L126
[bevy-mshade]: https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_pbr/src/meshlet/material_shade_nodes.rs#L98-L114
[bevy-msaa]: https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_render/src/view/mod.rs#L248-L253
[filament-doc]: https://google.github.io/filament/Filament.md.html
[fil-enums]: https://github.com/google/filament/blob/32084eb5d9c9da3bf9688f2d4055f2de7e40b2f3/libs/filabridge/include/private/filament/EngineEnums.h#L113
[fil-frox]: https://github.com/google/filament/blob/32084eb5d9c9da3bf9688f2d4055f2de7e40b2f3/filament/src/Froxelizer.cpp#L76-L87
[three-cl]: https://github.com/mrdoob/three.js/blob/r187/examples/jsm/lighting/ClusteredLighting.js
[three-cln]: https://github.com/mrdoob/three.js/blob/r187/examples/jsm/tsl/lighting/ClusteredLightsNode.js
[three-dl]: https://github.com/mrdoob/three.js/blob/r187/examples/jsm/lighting/DynamicLighting.js
[three-ssr]: https://github.com/mrdoob/three.js/blob/r187/examples/webgpu_postprocessing_ssr.html#L148-L151

- WebGPU editor's draft: [limits][spec-limits], [plain color formats][spec-formats], [bytes per sample][spec-bps], [feature index][spec-features], [`TRANSIENT_ATTACHMENT`][spec-transient].
- The wgpu fork at [`5a49474`][fork]; gputypes [v0.8.2](https://github.com/gogpu/gputypes/tree/v0.8.2).
- cog: [#660], [#661], [gfx design][gfx-pin], [gfx enums][gfx-feat], [v1's frame.wgsl][v1-frame].
- Papers and talks: [Olsson et al. 2012][olsson], [Harada et al. 2012][harada], [Burns and Hunt 2013][vb], [Engel 2018][tvb-blog], [The Forge v1.57][forge-157], [Sousa 2016][idtech6], [Arm subpasses sample][arm-subpass], [Metal feature set tables][metal-fst].
- Engines: Unity [comparison 6.0][urp-cmp], [6.3][urp-cmp63], [G-buffer][urp-gbuf], [6.1 what's new][urp-61], [forum][urp-forum], [source][urp-fl]; Godot [renderers][godot-renderers], [architecture][godot-arch], [source][godot-cb]; Bevy [0.12 notes][bevy-012], [source][bevy-gpu]; Filament [doc][filament-doc], [source][fil-frox]; three.js [source][three-cln].
