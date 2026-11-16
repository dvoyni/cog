# What gogpu/wgpu v0.34.2 offers for surfaces, readback, timestamps, pipeline caching and device loss

Research for [cog#661](https://github.com/dvoyni/cog/issues/661), on the map [cog#608](https://github.com/dvoyni/cog/issues/608). Gathered 2026-11-05.

The question: per backend (Vulkan, DX12, Metal), what does gogpu/wgpu v0.34.2 offer for surfaces (formats, color spaces, present and alpha modes, configure and resize, acquire results, which call blocks under FIFO), frames in flight, readback, timestamps, pipeline creation and caching, and device loss? The answers are facts about the pinned code. They are not a gfx design.

## Sources and versions

| Source | Version |
|---|---|
| `github.com/gogpu/wgpu` | v0.34.2 (Go module cache), the version `submodules/gogpu/go.mod` pins |
| `github.com/gogpu/gputypes` | v0.8.0, same pin |
| gogpu fork | `dvoyni/gogpu` [`d23f6fb`](https://github.com/dvoyni/gogpu/tree/d23f6fb3acebc48ea320db449eb4c8b2656455c4) (the `submodules/gogpu` pin) |
| Microsoft | [DXGI flip model](https://learn.microsoft.com/en-us/windows/win32/direct3ddxgi/dxgi-flip-model), [GetFrameLatencyWaitableObject](https://learn.microsoft.com/en-us/windows/win32/api/dxgi1_3/nf-dxgi1_3-idxgiswapchain2-getframelatencywaitableobject) |
| Apple | [CAMetalLayer.pixelFormat](https://developer.apple.com/documentation/quartzcore/cametallayer/pixelformat), [CAMetalLayer.framebufferOnly](https://developer.apple.com/documentation/quartzcore/cametallayer/framebufferonly) |
| wgpu (Rust), for comparison only | 30.0.1 on docs.rs: [SurfaceConfiguration](https://docs.rs/wgpu/latest/wgpu/type.SurfaceConfiguration.html), [PipelineCache](https://docs.rs/wgpu/latest/wgpu/struct.PipelineCache.html) |

Citation prefixes:

- `W:` is `https://github.com/gogpu/wgpu/blob/v0.34.2/`, the same files as `$(go env GOMODCACHE)/github.com/gogpu/wgpu@v0.34.2/`.
- `T:` is `https://github.com/gogpu/gputypes/blob/v0.8.0/`.
- `G:` is `https://github.com/dvoyni/gogpu/blob/d23f6fb3acebc48ea320db449eb4c8b2656455c4/`.

The public API is package `wgpu` (`*_native.go`, built without `-tags rust`). Below it are `core/` and the per-backend `hal/`.

## Answer in short

| Topic | Vulkan | DX12 | Metal |
|---|---|---|---|
| Surface formats reported | Driver list, filtered to formats wgpu maps; only the sRGB-nonlinear color space in practice | Fixed list: BGRA8, RGBA8, their sRGB variants, RGBA16Float | Fixed list: BGRA8, RGBA8, their sRGB variants, RGBA16Float |
| HDR / color space | No color-space field. RGBA16Float pairs with scRGB, but `VK_EXT_swapchain_colorspace` is never enabled | None (`SetColorSpace1` is never called) | None (no colorspace or EDR set on the layer) |
| Present modes reported | Driver list (Immediate, Mailbox, Fifo, FifoRelaxed) | Fifo, Mailbox, plus Immediate if tearing is allowed | Fifo, Immediate, Mailbox |
| Alpha modes | Driver flags, all four | Opaque, plus Premultiplied if DirectComposition is present | Opaque, Premultiplied reported; `AlphaMode` is ignored at configure |
| Acquire: outdated / lost | `ErrSurfaceOutdated` / `ErrSurfaceLost` / `ErrDeviceLost` | Never reported | Never reported |
| Acquire: timeout | 1 s, then `hal.ErrNotReady` (not re-exported by `wgpu`) | 1 s cap on the waitable, then a silent proceed | No timeout: `nextDrawable` blocks |
| What blocks under FIFO | Acquire (fence after `vkAcquireNextImageKHR`). The HAL does not wait at present | Acquire (frame-slot fence, then the latency waitable). The HAL does not wait at present | Acquire (`nextDrawable`) and `Queue.Submit` (2 in flight). Present does not block |
| Frames in flight | Swapchain images = `minImageCount+1` | 2 back buffers, `SetMaximumFrameLatency(2)`, 2 frame slots | 3 drawables, 2 submits |
| Readback | `Buffer.Map`/`MapAsync` + `Device.Poll`, all backends. Surface copy works if the usage asks for `CopySrc` | Surface back buffers are created as render targets only | Drawables are `framebufferOnly` unless the usage includes `StorageBinding` |
| Timestamps | **None in the public API.** HAL: compute-pass boundaries only | **None in the public API.** HAL: render- and compute-pass boundaries | None anywhere (`CreateQuerySet` returns `ErrTimestampsNotSupported`) |
| Async pipelines | None (synchronous `Create*Pipeline`) | None | None |
| Pipeline cache | Automatic `VkPipelineCache`, persisted to `os.UserCacheDir()/gogpu/vulkan/<adapter>/pipeline.cache` | Automatic PSO blob per pipeline in `os.UserCacheDir()/gogpu/dx12/<adapter>/`, plus an in-memory shader bytecode cache | None |
| Device loss | `hal.ErrDeviceLost` from acquire, present and fence waits. Submit returns an untyped error | Wrapped HRESULT errors, never `ErrDeviceLost` | Never detected |
| Loss callback / recreation | No lost callback or promise on any backend. Recreate by `Device.Release`, `RequestDevice`, then `Surface.Configure` | same | same |

Conclusions that matter beyond this ticket:

- **"A FIFO present blocks" is false on all three backends, as wgpu drives them.** The vsync wait is in `Surface.GetCurrentTexture`, and on Metal also in `Queue.Submit`. See [Which call blocks under FIFO](#which-call-blocks-under-fifo) and [For metronome's vsync preset](#for-metronomes-vsync-preset).
- **The public API has no query sets, timestamp writes or timestamp period at all.** The map's Notes say "timestamps at pass boundaries". That holds only for the HAL, and only partly. See [Timestamps](#timestamps).
- **`wgpu.SurfaceConfiguration` has six fields.** Frame latency, view formats and color space cannot be set, although `gputypes.SurfaceConfiguration` declares the first two.
- **No device-lost callback exists.** DX12 and Metal never return `ErrDeviceLost`.

## Surfaces

### API shape

- **Configuration** is `wgpu.SurfaceConfiguration{Width, Height, Format, Usage, PresentMode, AlphaMode}` (W:descriptor.go:498-505). It has no `ViewFormats`, no `DesiredMaximumFrameLatency` and no color space. `gputypes.SurfaceConfiguration` does declare `DesiredMaximumFrameLatency` and `ViewFormats` (T:surface.go:108-117), but `wgpu` uses its own struct and never reads them. The HAL config adds only `EnableDamagePresent`, a DX12 switch (W:hal/descriptor.go:120-150), which the public struct does not expose. For comparison, Rust wgpu 30's `SurfaceConfiguration` has `desired_maximum_frame_latency`, `view_formats` and `color_space`.
- **Capabilities** come from `Adapter.GetSurfaceCapabilities(surface)`, which returns `{Formats, PresentModes, AlphaModes}` (W:adapter_native.go:148-191). It reports no usages and no color spaces. On an adapter without a HAL it returns `PresentModes: [Fifo]` only (W:adapter_native.go:168-173).
- **Acquire** is `Surface.GetCurrentTexture() (*SurfaceTexture, suboptimal bool, err error)` (W:surface_native.go:327-375). The state machine allows one acquired texture at a time. A second acquire returns `core.ErrSurfaceAlreadyAcquired` (W:core/surface.go:142-147).
- **Present** is `Surface.Present` / `PresentWithDamage` (W:surface_native.go:392-413), or `DiscardTexture` to drop the frame. Presenting returns the surface to Configured even when the backend present fails (W:core/surface.go:198-211).
- **Public errors** are `ErrSurfaceLost`, `ErrSurfaceOutdated`, `ErrTimeout`, `ErrDeviceLost` and `ErrOutOfMemory`, all aliases of `hal` sentinels (W:error.go:14-18). `hal.ErrNotReady` ("skip this frame") is not re-exported (W:hal/error.go:41-45).
- **Reconfigure** is calling `Configure` again. It is rejected while a texture is acquired (W:surface_native.go:280-285, W:core/surface.go:69-71). Nothing reconfigures automatically on `ErrSurfaceOutdated`: the caller must. The only automatic path is an optional `SetPrepareFrame` hook, which reconfigures before acquire when it reports a size change (W:core/surface.go:358-384).
- **Zero size** fails `Configure` with `hal.ErrZeroArea` on all three backends (W:hal/vulkan/api.go:301-303, W:hal/dx12/instance.go:376-378, W:hal/metal/surface.go:37-39).
- **`Surface.ActualExtent()`** reports the swapchain size after Vulkan's clamping. DX12 and Metal return the configured size (W:surface_native.go:518-538).
- **Views of a surface texture:** on Vulkan, `CreateView` on a swapchain texture returns the swapchain's own pre-created view and ignores the descriptor's format (W:hal/vulkan/device.go:711-716). So an sRGB view of a linear swapchain is not available. Prior art agrees: the v0.31.6 inventory on `origin/research/wgpu-capabilities` found no `ViewFormats`. Nothing has changed since.

### Vulkan

- **Formats:** the driver's `vkGetPhysicalDeviceSurfaceFormatsKHR` list, kept only where wgpu has a reverse mapping: BGRA8, BGRA8Srgb, RGBA8 (Unorm/Srgb/Snorm/Uint/Sint), RGBA16Float, RGBA32Float, R8, R16Float (W:hal/vulkan/adapter.go:478-519, W:hal/vulkan/convert.go:666-687). `A2B10G10R10` is not in the reverse map, so 10-bit surfaces are dropped.
- **Color space:** it is chosen implicitly from the format. RGBA16Float wants `EXTENDED_SRGB_LINEAR` (scRGB). Everything else wants `SRGB_NONLINEAR` and falls back to the first pair with the same format (W:hal/vulkan/swapchain.go:188-224). Color spaces other than sRGB-nonlinear need the instance extension `VK_EXT_swapchain_colorspace`, which wgpu never enables. It enables only `VK_KHR_surface`, the platform WSI extensions and `VK_EXT_debug_utils` (W:hal/vulkan/api.go:66-90). So in practice no HDR surface is offered, and RGBA16Float is not advertised either: it is filtered out unless it is paired with scRGB (W:hal/vulkan/swapchain.go:188-194).
- **Present modes:** the driver list, mapped to Immediate, Mailbox, Fifo and FifoRelaxed (W:hal/vulkan/adapter.go:521-534). An unsupported requested mode fails `Configure`, with no fallback (W:hal/vulkan/swapchain.go:226-237).
- **Alpha modes:** the driver's `supportedCompositeAlpha`, mapped to Opaque, Premultiplied, Unpremultiplied and Inherit. `Auto` picks Opaque first (W:hal/vulkan/swapchain.go:239-280).
- **Usage:** `RenderAttachment` is always added. Every requested bit is checked against `supportedUsageFlags`, so `CopySrc` works where the driver allows it (W:hal/vulkan/swapchain.go:297-310).
- **Configure / resize:** an existing swapchain is waited on with `vkDeviceWaitIdle` and then passed as `oldSwapchain` (W:hal/vulkan/swapchain.go:555-587). A defined `currentExtent` is used verbatim. Otherwise the requested size is clamped (W:hal/vulkan/swapchain.go:378-406). Reconfiguring with a different device is refused while the old swapchain exists (W:hal/vulkan/swapchain.go:560-563).
- **Acquire results** (W:hal/vulkan/swapchain.go:1083-1111):
  - Success or `SUBOPTIMAL` returns the texture. Suboptimal is reported, except on Android (W:hal/vulkan/swapchain_platform.go:57-61).
  - `TIMEOUT` (after 1 s, W:hal/vulkan/swapchain.go:1043-1048) returns no texture. The surface turns that into `hal.ErrNotReady` (W:hal/vulkan/api.go:376-379).
  - `NOT_READY` and `ERROR_OUT_OF_DATE` return `ErrSurfaceOutdated`.
  - `ERROR_SURFACE_LOST` returns `ErrSurfaceLost`, and `ERROR_DEVICE_LOST` returns `ErrDeviceLost`.
  - Present maps the same results the same way (W:hal/vulkan/swapchain.go:1264-1285). Any failure marks the swapchain broken until reconfigure (W:hal/vulkan/swapchain.go:71-75).
- **Swapchain sync is per queue:** acquire records the swapchain as the queue's single `activeSwapchain` (W:hal/vulkan/api.go:384-389). The first `Submit` after acquire waits on the acquire semaphore, and every `Submit` with an active swapchain signals one more present semaphore (W:hal/vulkan/queue.go:286-309). Present waits on all of them. Offscreen submits are kept out with `Queue.SetSwapchainSuppressed` (W:queue_native.go:375-396). Acquiring a second surface replaces the first one's registration.

### DX12

- **Formats** are a fixed list, not queried: BGRA8Unorm, RGBA8Unorm, BGRA8UnormSrgb, RGBA8UnormSrgb, RGBA16Float (W:hal/dx12/adapter.go:391-399). The requested format becomes the DXGI swapchain format directly (W:hal/dx12/surface.go:38-43, 71-83). Microsoft's flip-model page lists only `R16G16B16A16_FLOAT`, `B8G8R8A8_UNORM` and `R8G8B8A8_UNORM` as formats to set, and wgpu always uses a flip model (`FLIP_DISCARD`, or `FLIP_SEQUENTIAL` for damage present, W:hal/dx12/surface.go:64-68). So the two sRGB entries are advertised, but they are not formats the flip model accepts. Rust wgpu works around this with a linear swapchain and an sRGB render-target view. This port has no such path.
- **Color space:** none. `IDXGISwapChain3::SetColorSpace1` is in the vtable (W:hal/dx12/dxgi/interfaces.go:535) and never called.
- **Present modes:** Fifo always, Immediate if `DXGI_FEATURE_PRESENT_ALLOW_TEARING` is available, Mailbox always (W:hal/dx12/adapter.go:405-420). At present (W:hal/dx12/queue.go:777-801):
  - Fifo and FifoRelaxed use sync interval 1.
  - Immediate uses interval 0 with `ALLOW_TEARING`.
  - Mailbox uses interval 0 without tearing.
- **Alpha modes:** Opaque, plus Premultiplied when `dcomp.dll` loads (W:hal/dx12/adapter.go:379-389). Premultiplied switches to a DirectComposition swapchain (W:hal/dx12/surface.go:90-122).
- **Usage** is ignored. Back buffers are created with `DXGI_USAGE_RENDER_TARGET_OUTPUT` and wrapped as `RenderAttachment` textures (W:hal/dx12/surface.go:77, 254-265).
- **Configure / resize:** the same device and the same alpha path go to `ResizeBuffers` after a full GPU wait (W:hal/dx12/instance.go:386-395, W:hal/dx12/surface.go:295-335). Otherwise the swapchain is unconfigured and recreated (W:hal/dx12/instance.go:397-401). `ResizeBuffers` keeps the original swapchain flags, so switching to Immediate on a resize does not add the tearing flag (W:hal/dx12/surface.go:315-321). The swapchain uses `DXGI_SCALING_STRETCH` (W:hal/dx12/surface.go:79), so a stale size stretches instead of erroring.
- **Acquire never returns outdated, lost or timeout.** It returns a frame-slot or waitable error, or an index-out-of-range error (W:hal/dx12/instance.go:442-491). `Suboptimal` is always false (W:hal/dx12/instance.go:483-484). Present returns `"dx12: Present failed: %w"` around the HRESULT (W:hal/dx12/queue.go:825-831), so a removed device or an occluded window reaches the caller as an untyped error. `dxgi.IsDeviceRemovedError` and `IsOccluded` exist (W:hal/dx12/dxgi/error.go:59-80) but are not used on these paths.
- **`DiscardTexture` is a no-op** (W:hal/dx12/instance.go:494-498).

### Metal

- **Formats** are a fixed list: BGRA8Unorm, BGRA8UnormSrgb, RGBA8Unorm, RGBA8UnormSrgb, RGBA16Float (W:hal/metal/adapter.go:115-122). The format is passed to `setPixelFormat:` (W:hal/metal/surface.go:77-80). Apple's `pixelFormat` page lists the accepted formats: `bgra8Unorm`, `bgra8Unorm_srgb`, `rgba16Float`, `rgb10a2Unorm`, `bgr10a2Unorm` and the `*_xr` formats. RGBA8 and RGBA8Srgb are advertised but not on that list, and the 10-bit formats are on it but not advertised.
- **Color space / EDR:** none. Nothing sets `colorspace` or `wantsExtendedDynamicRangeContent` on the layer.
- **Present modes:** Fifo, Immediate and Mailbox are reported (W:hal/metal/adapter.go:123-127). The only effect is `displaySyncEnabled = (mode == Fifo)` (W:hal/metal/surface.go:107-111). So Immediate and Mailbox behave the same, and FifoRelaxed, if requested, turns vsync off.
- **Alpha modes:** Opaque and Premultiplied are reported (W:hal/metal/adapter.go:128-131). `Configure` never reads `AlphaMode` (W:hal/metal/surface.go:34-157).
- **Usage:** `framebufferOnly` is true unless the usage includes `StorageBinding` (W:hal/metal/surface.go:88-90). Apple: with `framebufferOnly` true, "you may not sample, read from, or write to those textures".
- **Configure / resize:**
  - Same size, format and device only updates vsync (W:hal/metal/surface.go:46-54).
  - A size change waits for GPU idle, except in transaction-present mode (W:hal/metal/surface.go:56-67). It then sets `drawableSize`, `maximumDrawableCount = 3` and `allowsNextDrawableTimeout = false` (W:hal/metal/surface.go:82-105). Gravity is set to top-left, so a stale drawable is letterboxed, not stretched (W:hal/metal/surface.go:130-145).
  - `Surface.SetPresentsWithTransaction` toggles CA-transaction presents for live resize (W:surface_native.go:429-441, W:hal/metal/surface.go:166-182).
- **Acquire never returns outdated, lost or timeout.** A nil drawable is a plain error (W:hal/metal/surface.go:189-193), and `Suboptimal` is always false (W:hal/metal/surface.go:230). Present always returns nil (W:hal/metal/queue.go:413-459).

### Which call blocks under FIFO

- **Vulkan:** `GetCurrentTexture` blocks, three times over (W:hal/vulkan/swapchain.go:1064-1129):
  1. It waits on the device timeline for the submission that last consumed this acquire semaphore. There are as many semaphores as images (W:hal/vulkan/swapchain.go:661-662).
  2. It calls `vkAcquireNextImageKHR` with a 1 s timeout.
  3. It waits on the post-acquire fence, which signals when the presentation engine has released the image. This wait is the vsync wait.

  `Present` only calls `vkQueuePresentKHR` (W:hal/vulkan/swapchain.go:1256). Whether a driver blocks inside it is up to the driver. wgpu adds no wait there.
- **DX12:** `GetCurrentTexture` blocks. It first waits, with no timeout, for the GPU to finish the frame two presents back (W:hal/dx12/device.go:562-616). Then it waits up to 1 s on the frame-latency waitable object (W:hal/dx12/instance.go:453-462). A timeout there is not reported. Microsoft documents this handle as the one that "signals when the DXGI adapter has finished presenting a new frame", and says the app "should wait on this handle before starting any rendering operations". Present calls `Present(1, 0)` and advances the frame index, with no wait (W:hal/dx12/queue.go:829-841).
- **Metal:** `GetCurrentTexture` blocks in `nextDrawable` when all three drawables are held, with no timeout (W:hal/metal/surface.go:95-105, 189). `Queue.Submit` blocks when two submits are in flight (W:hal/metal/queue.go:53-58). Present commits a command buffer with `presentDrawable:` and returns (W:hal/metal/queue.go:432-442). Only transaction mode adds a `waitUntilScheduled` (W:hal/metal/queue.go:444-447).
- **Public `Queue.Submit`** is documented as non-blocking (W:queue_native.go:44-50). That holds on Vulkan and DX12. On Metal the HAL throttle above blocks it.

### What the gogpu fork does with it

- **Format:** it always uses `BGRA8Unorm` (G:renderer.go:374) and never consults `caps.Formats`.
- **Present mode:**
  - vsync on picks FifoRelaxed, then Fifo.
  - vsync off picks Immediate, then Mailbox, then Fifo.
  - It reads `GetSurfaceCapabilities` before each configure (G:renderer.go:446-498).
- **Alpha:** Opaque, or Premultiplied for transparent windows when reported (G:renderer.go:504-520).
- **Errors:** `ErrSurfaceOutdated` on acquire or present means reconfigure. `ErrSurfaceLost` marks the surface lost. Any other error, including Vulkan's `hal.ErrNotReady` after a 1 s timeout, is logged as an error and also reconfigures (G:renderer.go:685-703, 884-918).
- **Resize:** it reconfigures before each acquire when `PrepareFrame` reports a new physical size (G:renderer.go:640-648).

## Frames in flight

- **Vulkan:** the bound is the swapchain image count. It requests `minImageCount + 1`, capped by `maxImageCount` (W:hal/vulkan/swapchain.go:514-523). Typically that is 3, so two frames can queue behind the one on screen. Nothing else limits CPU run-ahead, and `Submit` never blocks (W:hal/vulkan/queue.go:235-376).
- **DX12:** 2 back buffers (W:hal/dx12/surface.go:19-20), `SetMaximumFrameLatency(2)` (W:hal/dx12/surface.go:22-23, 158-166), and 2 frame slots that each record a fence value on submit (W:hal/dx12/device.go:41, 618-640). Acquire waits for the slot's previous frame (W:hal/dx12/device.go:594-616). `Submit` itself does not wait.
- **Metal:** `maximumDrawableCount = 3` (W:hal/metal/surface.go:95), plus a 2-slot channel that `Submit` takes from and the command-buffer completion handler gives back (W:hal/metal/queue.go:17-22, 53-102, W:hal/metal/adapter.go:56). It counts submits, not frames, so a frame with two `Submit` calls uses both slots.
- **No public knob.** All three bounds are constants, and wgpu's configuration has no frame-latency field (see [Surfaces](#api-shape)).
- **Completion tracking:** `Queue.Submit` returns a submission index, and `Queue.Poll()` returns the last completed one, without blocking (W:queue_native.go:250-257). Underneath:
  - Vulkan reads a timeline semaphore, or polls a fence pool without timeline support (W:hal/vulkan/queue.go:378-402).
  - DX12 reads its fence (W:hal/dx12/device.go:651-655).
  - Metal reads an atomic set by completion handlers (W:hal/metal/queue.go:104-131).

  A public `Fence` with `Device.WaitForFence(f, value, timeout)` exists (W:device_native.go:841-918), but no call ties a fence to a `Submit`, and `Submit` takes none.

## Readback

- **Mapping API** (W:buffer.go:92-223):
  - `Buffer.Map(ctx, mode, offset, size)` blocks.
  - `Buffer.MapAsync(...)` returns a `*MapPending` with `Status()` and `Wait(ctx)`.
  - `Buffer.MappedRange(offset, size)` then returns `.Bytes()`, and `Unmap()` releases the mapping.

  The offset must be a multiple of 8 and the size a multiple of 4. Usage `MapRead` or `MapWrite` is required.
- **When a map resolves:** `MapAsync` gates on the queue's latest submission index at the time of the call, not on the buffer's last use (W:buffer.go:172-181). Resolution happens in two ways (W:device_native.go:1078-1112):
  - `Device.Poll(PollPoll)` drains completed submissions without blocking. `Queue.Submit` also calls it on its way out (W:queue_native.go:157-165).
  - `Device.Poll(PollWait)` waits for the whole device to go idle (`hal.Device.WaitIdle`) and resolves everything.

  No call waits for one submission index. A map pending when the device goes away resolves with `ErrMapDeviceLost` (W:map_types.go:128-129, W:core/device_poll.go:138-149).
- **Backends:**
  - Vulkan keeps buffers persistently mapped and invalidates non-coherent memory on map (W:hal/vulkan/device.go:439-488).
  - DX12 uses READBACK heaps and Metal uses Shared storage. Both are persistently mapped and coherent (W:hal/api.go:21-35, 131-134).
- **Texture to buffer:** `CommandEncoder.CopyTextureToBuffer(src, dst, regions)` (W:encoder_native.go:389-440). The public layer does not check the source's `CopySrc` usage or the 256-byte `BytesPerRow` alignment. Errors surface at `Finish`. gogpu pads rows to 256 itself (G:renderer.go:1836-1843).
- **Copying a surface texture out:** use `SurfaceTexture.AsTexture()` (W:surface_native.go:732-744) as the copy source between acquire and present. Per backend:
  - Vulkan honours `CopySrc` in the surface usage.
  - DX12 ignores the usage. Back buffers are render-target-output resources wrapped as `RenderAttachment` (W:hal/dx12/surface.go:77, 261).
  - Metal drawables stay `framebufferOnly` unless the usage includes `StorageBinding` (W:hal/metal/surface.go:88-90).

  The software backend alone offers `Surface.ReadPixels` (W:surface_native.go:493-516).
- **gogpu's pattern** is `RenderToImage`. It renders offscreen, copies to a `CopyDst|MapRead` buffer, then calls `Poll(PollWait)`, `MapAsync`, `Poll(PollWait)` and `MappedRange` (G:renderer.go:1845-1927). It never copies from the swapchain.

## Timestamps

- **Public API: none.** No `.go` file in package `wgpu` outside the browser and rust builds mentions `QuerySet` or `Timestamp`. There is no `CreateQuerySet`, no `TimestampWrites` on `RenderPassDescriptor` (W:descriptor.go:415-419) or the compute-pass descriptor, no `WriteTimestamp`, no `ResolveQuerySet` and no `Queue.GetTimestampPeriod`. Occlusion queries are missing for the same reason. `core` drops compute-pass `TimestampWrites` with the note "QuerySet HAL integration pending" (W:core/command.go:1726-1729). This was already true at v0.31.6.
- **Feature flags do not match support:**
  - Vulkan and DX12 never set `FeatureTimestampQuery` (W:hal/vulkan/api.go:509-550, W:hal/dx12/adapter.go:244-259).
  - Metal sets it on Metal3 GPUs (W:hal/metal/api.go:77-79), but its `CreateQuerySet` returns `hal.ErrTimestampsNotSupported` (W:hal/metal/device.go:1088-1092), and its `ResolveQuerySet` is a stub (W:hal/metal/encoder.go:341-345).
- **HAL only.** `wgpu.Device.HalDevice()` (W:wrap.go:98) reaches `hal.Device.CreateQuerySet` (W:hal/api.go:205-207). Pass timestamp writes and `ResolveQuerySet` live on HAL encoders, which the public encoders do not expose.
  - **Vulkan:** timestamp and occlusion pools exist (W:hal/vulkan/query.go:28-75). Only compute passes write timestamps, at begin and end (W:hal/vulkan/command.go:1065-1080, 1395-1405). Render passes ignore `TimestampWrites`. Pools are reset with host `vkResetQueryPool`, but the `hostQueryReset` feature is never enabled. Only `timelineSemaphore` is chained in (W:hal/vulkan/query.go:60-61, W:hal/vulkan/adapter.go:121-128). Resolve uses `vkCmdCopyQueryPoolResults` with `64_BIT|WAIT` and an 8-byte stride (W:hal/vulkan/command.go:776-820). The period is `VkPhysicalDeviceLimits.timestampPeriod`, with a fallback of 1.0 (W:hal/vulkan/device.go:272-275, W:hal/vulkan/queue.go:815-820).
  - **DX12:** timestamp and binary-occlusion heaps exist (W:hal/dx12/query.go:35-72). Render and compute passes write `EndQuery` at begin and at `End` (W:hal/dx12/command.go:370-388, 485-506, 535, 872). Resolve uses `ResolveQueryData` (W:hal/dx12/command.go:327-343). The period is `1e9 / GetTimestampFrequency()` ns (W:hal/dx12/queue.go:849-866).
  - **Metal:** nothing. The period is a hard-coded 1.0 (W:hal/metal/queue.go:461-465).
- **Mid-pass and encoder-level timestamps:** no HAL method writes a timestamp outside a pass's begin and end (W:hal/command.go:40-80). That matches the map's note "none mid-pass".

## Pipelines

- **No async creation.** `Device.CreateRenderPipeline` and `CreateComputePipeline` are synchronous (W:device_native.go:518, 637). No `*Async` variant exists in the native build. The public layer takes no device-wide lock around the HAL call (W:device_native.go:518-537), and the map's Notes say the device is safe from many goroutines. So "async" means calling it on another goroutine. HAL-internal serialization was not audited here.
- **No public pipeline cache.** There is no `PipelineCache` type or descriptor field. The HAL caches on its own:
  - **Vulkan:** one device-wide `VkPipelineCache` is created at device open (W:hal/vulkan/adapter.go:184) and loaded from `os.UserCacheDir()/gogpu/vulkan/<sha256(vendor, device, driver, pipelineCacheUUID)>/pipeline.cache`. A blob the driver rejects is deleted and the cache starts empty (W:hal/vulkan/pipeline_cache.go:19-82). Every graphics and compute pipeline uses it (W:hal/vulkan/pipeline.go:324, 436). It is saved once, atomically, at device destroy (W:hal/vulkan/pipeline_cache.go:84-130, W:hal/vulkan/device.go:1569). A crash loses the session's additions.
  - **DX12:**
    - On disk: each PSO's driver blob is cached under `os.UserCacheDir()/gogpu/dx12/<adapter key>/<key>.pso`. It is loaded into `CachedPSO` at creation, saved after a first creation, and deleted on `E_INVALIDARG` (W:hal/dx12/pso_cache.go:18-139, W:hal/dx12/device.go:302-305).
    - In memory: a separate LRU of compiled shader bytecode, keyed by the HLSL hash, entry point, stage and target, holds 200 entries and keeps 100 on eviction (W:hal/dx12/shader_cache.go:15-60).
  - **Metal:** none. No `MTLBinaryArchive` is used.
  - **Directory helpers:** all in W:internal/pipelinecache/disk.go:17-98. The location cannot be changed, and caching cannot be turned off.
- **New since v0.31.6:** `internal/pipelinecache`, `hal/vulkan/pipeline_cache.go` and `hal/dx12/pso_cache.go` are all absent from v0.31.6.
- **For comparison:** Rust wgpu 30's `PipelineCache` is explicit, Vulkan-only, and returned through `get_data` for the app to persist.

## Device loss

- **Reporting:** there is no lost callback, lost future, `Device.Lost()` or uncaptured-error handler. Error scopes exist (`PushErrorScope`/`PopErrorScope`, W:device_native.go:936-944). Loss shows up only as errors from individual calls:
  - **Vulkan:** acquire, present and fence waits return `hal.ErrDeviceLost` (W:hal/vulkan/swapchain.go:1104-1106, 1278-1280, W:hal/vulkan/device.go:1488-1489, 1523-1524, W:hal/vulkan/fence.go:144-145). `vkQueueSubmit` failures return `"vulkan: vkQueueSubmit failed: %d"`, not wrapped (W:hal/vulkan/queue.go:368-374), and the public `Submit` wraps that as `"wgpu: submit failed: %w"` (W:queue_native.go:117-125).
  - **DX12:** `GetDeviceRemovedReason` is checked after `ExecuteCommandLists`, after swapchain creation and in pipeline creation (W:hal/dx12/queue.go:199-204, W:hal/dx12/surface.go:190-193, W:hal/dx12/device.go:695-704). The result is a wrapped HRESULT error. DRED breadcrumbs are logged. `hal.ErrDeviceLost` never appears under `hal/dx12`.
  - **Metal:** never detected. Completion handlers record only the submission index, never `MTLCommandBufferStatusError` (W:hal/metal/queue.go:111-131). `ErrDeviceLost` never appears under `hal/metal`.
  - `core.ErrDeviceLost` is a separate sentinel from `hal.ErrDeviceLost`, and only ray tracing uses it (W:core/error.go:33-34, W:core/ray_tracing.go:22).
- **What survives:** `Device.Release` discards each surface's acquisition and waits idle. It then flushes deferred destroys, destroys the encoder pool and the HAL device, and finally retires every surface configured with that device to Unconfigured, so the surfaces can be configured again (W:device_native.go:989-1067, W:surface_native.go:558-569). The `Instance`, `Adapter` and `Surface` survive. Every buffer, texture, pipeline and other object created from the device does not: its HAL device lookup fails with `ErrReleased`.
- **Recreation:** nothing is automatic. The sequence is `Device.Release()`, then `Adapter.RequestDevice(...)`, then `Surface.Configure(newDevice, ...)`, then recreate every resource. On Vulkan the old device must be released first, because a configured swapchain refuses a different device (W:hal/vulkan/swapchain.go:560-563). Whether the old `Adapter` stays valid after a Windows TDR was not tested.
- **gogpu fork:** it declares `gogpu.ErrDeviceLost` (G:errors.go:26-29), but nothing returns or checks it.

## For metronome's vsync preset

`metronome/docs/design.md` (*In flight*) sets vsync as `MaxInFlight: 2` with no `MinInterval`, on the reasoning that "a FIFO present blocks, the out dependency waits on it, and the display paces the track".

- **The premise is refuted** for all three backends as wgpu v0.34.2 drives them. `Present` returns without waiting for the display. The vsync wait happens in the next frame's `GetCurrentTexture`, and on Metal also in `Queue.Submit` (see [Which call blocks under FIFO](#which-call-blocks-under-fifo)).
- **The outcome still mostly holds.** Beat N's out dependency finishes soon after its present. Beat N+1's `AcquireFramebuffer` job then blocks until an image is free, which keeps beat N+1 in flight. With `MaxInFlight: 2`, beat N+2 cannot start either, so the display still paces the track, one beat later than the design text says.
- **Consequences:**
  - The vsync wait occupies the worker (or pinned thread) that runs `AcquireFramebuffer`, for up to about one refresh interval per frame. On Vulkan it can last up to 1 s before returning `hal.ErrNotReady`. On Metal it has no limit.
  - On Metal, the throttle also falls inside `FlushEncoders`'s `Submit`.
  - The waits are the backends' own bounds: about 2 queued images on Vulkan, 2 frames on DX12, 3 drawables and 2 submits on Metal. `MaxInFlight` only adds a bound on top of them.

## Other findings outside the question

- **No indirect-count draws** anywhere in v0.34.2, public or HAL. `MultiDrawIndirect(buffer, offset, drawCount)` takes a CPU count (W:renderpass_native.go:250), and no `*IndirectCount` symbol exists. The map's Notes say "with count on Vulkan and DX12".
