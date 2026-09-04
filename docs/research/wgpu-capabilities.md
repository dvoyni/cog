# wgpu backend capabilities for scene

Resolves [dvoyni/cog#4](https://github.com/dvoyni/cog/issues/4) (child of the scene map, #1).
Inventory of what the vendored `github.com/gogpu/wgpu` and browser WebGPU can do that
scene's gfx extensions will lean on, on both platform paths, with pointers to what cog's
`wgpu` package already uses versus what is merely available.

## Which stacks are actually in play

`go.mod` pins `github.com/gogpu/wgpu v0.31.6-0.20260827093430-9ecabb42040a` (plus
`gputypes v0.5.2`, `naga v0.18.0`, `gogpu v0.53.0`). That module ships three
implementations selected by build tag (`README.md` "Three implementations, one API"):

| Build | Files | Stack | cog uses it? |
|---|---|---|---|
| default (`!rust && !(js && wasm)`) | `*_native.go` -> `core/` -> `hal/{vulkan,dx12,gles}` (Windows, `hal/allbackends/register_windows.go`; Metal on darwin) | **Pure Go**, zero CGO. Not Dawn, not wgpu-native. | yes: `wgpu/platform_desktop.go` |
| `-tags rust` | `*_rust.go` -> `github.com/go-webgpu/webgpu` -> wgpu-native v29 | Rust FFI | **no**. `go-webgpu/webgpu` is only an indirect dep; no `rust` tag anywhere in cog. |
| `GOOS=js GOARCH=wasm` | `*_browser.go` -> `internal/browser/*` -> `syscall/js` | **Browser WebGPU directly** (`navigator.gpu`); does not go through go-webgpu | yes: `wgpu/platform_web.go` |

Consequences:
- Comments in `wgpu/gfxbackend.go` that say "native Dawn tolerates X" are naming the wrong thing; the desktop stack is gogpu's own Go core + HAL. Its validation is thinner than a browser's, so **desktop accepting something is not evidence the web path will**.
- The device is requested with `RequestDevice(nil)` on both paths (`gogpu@v0.53.0/renderer.go:330`). On native that means the **adapter's real hardware limits** (`adapter_native.go` RequestDevice, comment at lines 76-80; Vulkan reads `vkLimits.MaxPerStageDescriptorStorageBuffers`, `hal/vulkan/api.go:570`; DX12 hard-codes 64, `hal/dx12/adapter.go:292`). In the browser a nil descriptor is `requestDevice()` with no `requiredLimits`, so the device gets the **spec default limits** regardless of the adapter (`adapter_browser.go:47`). Desktop is therefore strictly more permissive than web; the spec defaults below are the contract scene must design to.
- The browser adapter is requested without `featureLevel` (`internal/browser/convert.go:29-47` only sets `powerPreference`/`forceFallbackAdapter`), so the browser gives a **core** adapter or `null`. Compatibility mode (`featureLevel: "compatibility"`, shipping in Chrome 146) is never entered by cog. See Risks.

## Inventory

Legend: **used** = cog's wgpu package already exercises it; **avail** = present in the vendored API and mapped on both backends, not yet used by cog; spec = [WebGPU spec](https://gpuweb.github.io/gpuweb/) (fetched 2026-09-04).

| Capability | Desktop (pure Go) | Web (browser) | Status in cog | Pointers |
|---|---|---|---|---|
| Render to texture (color attachment on a user texture) | Any `RenderAttachment` texture; `RenderPassColorAttachment{View, ResolveTarget, LoadOp, StoreOp, ClearValue}`, N attachments | Same; `colorAttachments` array + `resolveTarget` emitted | **avail**. cog only ever attaches the surface view; `resolveTarget()` returns nil for anything but `screenID` and `newTexture` never sets `TextureUsageRenderAttachment` | `wgpu/gfxbackend.go:633-638, 256-263, 668-675`; `wgpu@.../descriptor.go:419-433`; `internal/browser/convert_resources.go:760-792` |
| Texture views (format/dimension/aspect/mip and layer sub-ranges) | `TextureViewDescriptor{Format, Dimension, Aspect, BaseMipLevel, MipLevelCount, BaseArrayLayer, ArrayLayerCount}` | Same; `baseMipLevel`/`baseArrayLayer` set when non-zero. Descriptor must be non-nil on browser | **used** for full views only (2D and 2D-array, all mips). Per-layer/per-mip views **avail** | `wgpu/gfxbackend.go:267-275`; `wgpu/texutil.go:36-46` (nil-desc note); `descriptor.go:88-97`; `convert_resources.go:97-121` |
| Depth texture as pass attachment | `RenderPassDepthStencilAttachment{View, Depth*, DepthReadOnly, Stencil*, StencilReadOnly}` | Same. Browser binding **always emits `stencilLoadOp/StoreOp`** unless `StencilReadOnly`, so a stencil-less depth format needs `StencilReadOnly: true` (or keep a `*Stencil8` format) | **used**: one shared `Depth24PlusStencil8` buffer sized to the target, `DepthStoreOp: Store` | `wgpu/gfxbackend.go:20-24, 672-675, 873-903`; `convert_resources.go:795-816` |
| Depth texture as sampled texture (shadows) | `TextureUsageTextureBinding` on depth formats; `TextureSampleTypeDepth`; `SamplerBindingTypeComparison`; naga lowers `texture_depth_2d`, `sampler_comparison`, `textureSampleCompare(Level)` to SPIR-V/HLSL/MSL | spec: all depth formats support `TEXTURE_BINDING` and `RENDER_ATTACHMENT`, sample types `"depth"` and `"unfilterable-float"`; comparison samplers always allowed. Browser maps `"depth"`/`"comparison"` | **avail**. cog's reflection emits `SampleTypeFloat` for every image and `Filtering` for every sampler, so a depth binding would fail layout validation today | `wgpu/gfxbackend.go:431-445`; `wgpu/gfxreflect.go:52-63`; `naga@v0.18.0/ir/ir.go:368-405` (`SamplerType.Comparison`, `ImageClassDepth`); `naga/wgsl/internal/lower/lower.go:10342, 13452, 14037`; `gputypes/sampler.go:218`, `texture.go:869`; `convert_enums.go:435, 449` |
| Sampler comparison | `SamplerDescriptor.Compare`; Vulkan sets `CompareEnable`, DX12 `ComparisonFunc` | `compare` set when not Undefined | **avail** (cog never sets `Compare`) | `descriptor.go:114-125`; `hal/vulkan/device.go:858-891`; `hal/dx12/device.go:1688`; `convert_resources.go:173-174` |
| Several render passes per submit | Encoder state machine returns to Recording after `End()`, so `BeginRenderPass` repeats on one encoder; also multiple `Queue.Submit` per frame | Same (WebGPU) | **used**: exactly one pass, one encoder, one submit per frame (`gfx/plugin.go:136-137`). Multi-pass is **avail** | `wgpu/gfxbackend.go:641-694`; `wgpu@.../core/command_encoder.go:25-40` |
| Texture formats: `RGBA8UnormSrgb`, `BGRA8UnormSrgb` | Enumerated and mapped in every HAL (`R8g8b8a8Srgb`, `B8g8r8a8Srgb`) | spec: both renderable + blendable + `"float"`; `bgra8unorm-srgb` requires `core-features-and-limits` (absent in compat) | **avail**. `gfx.TextureFormat` has only `FormatRGBA8`; the backend hard-codes `RGBA8Unorm`. Surface is hard-coded linear `BGRA8Unorm` on both platforms and `SurfaceConfiguration` has no `ViewFormats`, so the swapchain **cannot be sRGB-viewed**: gamma encoding is the fragment shader's job | `gfx/contract.go:31-36`; `wgpu/gfxbackend.go:261, 273`; `gogpu@v0.53.0/renderer.go:309`; `descriptor.go:502-509`; `hal/vulkan/convert.go:112-118` |
| `RGBA16Float` | Mapped in Vulkan/DX12/Metal/GLES tables | spec: renderable, blendable, resolve, `"float"` filterable, no feature needed (multisampling needs core) | **avail** | `gputypes/texture.go:102`; `hal/vulkan/convert.go:133`; `hal/dx12/adapter.go:360`; `convert_enums.go:60` |
| `Depth32Float`, `Depth24Plus` | Mapped (`D32Sfloat`, `X8D24UnormPack32`) | spec: both renderable + sampleable (`"depth"`, `"unfilterable-float"`); `depth24plus` is **not** a texel-copy source, `depth32float` is | **avail**; cog uses `Depth24PlusStencil8` only | `hal/vulkan/convert.go:143-146`; `convert_enums.go:70-73` |
| Mipmap generation | No API in gogpu/wgpu or gogpu (`grep -rn "func.*[Mm]ipmap"` finds nothing) | No API in WebGPU | **used** CPU-side: `uploadMipChain` box-filters RGBA8 and `WriteTexture`s each level. GPU generation would be a chain of render passes over per-mip views (blit pipeline) | `wgpu/gfxbackend.go:295-339` |
| Storage buffers per stage | Hardware value (typically >= 8, DX12 reports 64); `core/validate.go:571` checks it | spec default `maxStorageBuffersPerShaderStage` **8**; `maxStorageBuffersInVertexStage` **8** core / **0** compat; `maxStorageBuffersInFragmentStage` 8 / 4 | **used**: reflection emits `BufferBindingTypeReadOnlyStorage`/`Storage` with `Vertex|Fragment` visibility; `BufferStorage` gets `Storage|Vertex|Index|CopyDst` usage | `wgpu/gfxbackend.go:433-438, 500-505`; `wgpu/gfxreflect.go:39-45`; `gputypes/limits.go:82` |
| Max storage binding size / buffer size | Hardware (`MaxStorageBufferRange`) | spec default `maxStorageBufferBindingSize` **128 MiB**, `maxBufferSize` **256 MiB**, `minStorageBufferOffsetAlignment` **256** | bind-cache key already assumes <= 256 MiB (`uint32` offset/size) | `wgpu/gfxbindcache.go:18-26`; `wgpu/gfxbackend.go:154-169` |
| Max bind groups | Hardware; `core/validate.go:282` checks | spec default **4** (`maxBindGroupsPlusVertexBuffers` 24, `maxBindingsPerBindGroup` 1000) | cog derives one BGL per reflected `@group`; nothing caps the group index at 4 | `wgpu/gfxbackend.go:449-461` |
| Uniform binding size | Hardware | spec default **64 KiB** core / 16 KiB compat | **used**: 256 B per-draw pooled uniform | `wgpu/gfxbackend.go:18, 845-857` |
| Instancing via `instance_index` | `Draw/DrawIndexed(count, instances, first, ...)`; naga `BuiltinInstanceIndex`; `VertexStepModeInstance` | Same; `"instance"` step mode emitted | **used**: `Draw(first, count, instances, indexed)` forwards `instances`; only step-mode `Vertex` layouts are built | `wgpu/gfxbackend.go:171-184, 559-563`; `naga/ir/ir.go:677`; `gputypes/vertex.go:181` |
| Cull mode / front face | `PrimitiveState{FrontFace, CullMode}` (zero values = CCW / None per spec) | `frontFace`/`cullMode` emitted | **avail**: cog hard-codes `CullModeNone`; `gfx.PipelineDesc` has no field | `wgpu/gfxbackend.go:572`; `gputypes/render.go:319-382`; `convert_resources.go:531-534` |
| Depth state details | `DepthCompare`, `DepthWriteEnabled`, `DepthBias*` (for shadow passes) | Same | **used**: `Less` + write when `DepthTest`, else `Always`; bias unused | `wgpu/gfxbackend.go:532-548`; `descriptor.go:275-286` |
| Dynamic offsets | `BufferBindingLayout.HasDynamicOffset`; `SetBindGroup(index, group, offsets)` | Same | **avail** (cog passes `nil` offsets and rebuilds bind groups per resource combination) | `wgpu/gfxbackend.go:839`; `gputypes/binding.go:30-37` |
| Indirect / multi-draw | `DrawIndirect`, `MultiDrawIndirect` | `drawIndirect`; multi-draw emulated | not needed by scene v1 | `renderpass_native.go:243-296` |
| Compute | full | full | out of scope (#1) | - |

Default-limit values above are the spec's `supported limits` table rows
(`maxBindGroups 4`, `maxStorageBuffersPerShaderStage 8`, `maxStorageBuffersInVertexStage 8|0`,
`maxStorageBufferBindingSize 134217728`, `maxBufferSize 268435456`, `maxColorAttachments 8|4`,
`maxTextureDimension2D 8192|4096`, `maxSampledTexturesPerShaderStage 16`, `maxSamplersPerShaderStage 16`,
`maxVertexBuffers 8`, `maxVertexBufferArrayStride 2048`, `maxInterStageShaderVariables 16|15`);
`gputypes.DefaultLimits()` (`limits.go:74-110`) mirrors the core column but predates the
per-stage vertex/fragment storage limits, which are not in `gputypes.Limits` at all.

## What scene's gfx extensions must add to cog (gap list)

1. `gfx.TextureDesc` needs a render-target flag and more `TextureFormat` values (sRGB, RGBA16F, depth); the backend must pass `TextureUsageRenderAttachment` and resolve non-screen `TextureViewID`s in `resolveTarget`.
2. Per-pass depth: a camera target texture needs its own depth attachment; today one screen-sized depth buffer is shared. For sampled shadow depth use `Depth32Float` (copyable, no stencil) and set `StencilReadOnly: true` in the browser path, or keep a `*Stencil8` format.
3. Reflection (`gfxreflect.go`) must map `ImageClassDepth` -> `TextureSampleTypeDepth` and `SamplerType.Comparison` -> `SamplerBindingTypeComparison`; today every texture is `Float` and every sampler `Filtering`, which a browser rejects for depth bindings.
4. `PipelineDesc` needs `CullMode`/`FrontFace` (and later `DepthBias` for shadow casters).
5. Bind-group layout visibility is `Vertex|Fragment` for everything, so every storage buffer counts against **both** per-stage limits; scene should keep total storage bindings per shader <= 8 or split visibility by stage.
6. Colour pipeline: swapchain is linear `BGRA8Unorm` with no sRGB view, so PBR shaders must apply the sRGB OETF themselves (canvas currently writes display-space values straight through).

## Risks

- **Vertex-stage storage buffers on web (skinning).** On a core adapter the guarantee is `maxStorageBuffersInVertexStage = 8` and `maxStorageBufferBindingSize = 128 MiB`, which comfortably fits per-instance data + baked pose buffers + morph deltas. cog never requests `featureLevel: "compatibility"`, so on devices that only offer compat (webgpufundamentals: "possibly 0 storage buffers in vertex shaders", ~45% of older Android hardware) `requestAdapter()` returns `null` and cog has **no WebGPU at all**, not a degraded scene. That matches the map's "no fallback paths", but the failure mode is total. If a compat path is ever wanted, vertex-stage skinning must move to uniform arrays (16 KiB there) or textures; storage-buffer skinning is not portable to compat.
- **Desktop does not validate against web limits.** Native devices get hardware limits (`adapter_native.go`), so a shader binding 9+ storage buffers or a bind group > 4 passes on desktop and fails in the browser. Recommend a debug check in `buildShaderLayouts` against `gputypes.DefaultLimits()` (plus the missing per-stage vertex/fragment counts).
- **`maxBindGroups = 4`.** Scene's natural split (frame / camera+pass / material / instance) already uses all four groups; a fifth (e.g. skinning) has to share group 3 with instancing. Bind-group cache keys must stay per-group.
- **Browser stencil ops on the depth attachment.** Any depth-only format (`Depth32Float`, `Depth24Plus`) used as a pass attachment on web must set `StencilReadOnly: true` or the binding emits `stencilLoadOp` and the browser rejects the pass (`convert_resources.go:807-810`). This is why cog currently uses `Depth24PlusStencil8`.
- **Instance-buffer binding size** is bounded by 128 MiB per binding, fine; but per-draw bind-group creation (`bindGroups.get`) per distinct offset will churn unless instance ranges use `firstInstance`/`instance_index` with one bind group per frame, or dynamic offsets (256 B aligned).
- **naga runtime arrays.** `arraylength_repro_test.go` exists in the vendored root for an `arrayLength()` correctness repro on Metal; skinning shaders should carry counts in a uniform rather than rely on `arrayLength()` of a storage array.
- **`BGRA8UnormSrgb` is core-only** and there is no swapchain `ViewFormats`; do not plan on an sRGB swapchain on either platform.

Sources: vendored module cache under `go env GOMODCACHE` at the pinned versions above;
WebGPU spec limits table and format capability tables (gpuweb.github.io/gpuweb, sections
`supported limits`, `plain-color-formats`, `depth-formats`, `GPURequestAdapterOptions.featureLevel`
"defaulting to \"core\""); gpuweb `proposals/compatibility-mode.md`;
webgpufundamentals "WebGPU Compatibility Mode".
