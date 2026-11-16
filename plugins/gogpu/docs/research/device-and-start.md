# What gogpu's HostLoop needs to start, and whether its device takes gfx's features and limits and survives a loss

Research for [cog#713](https://github.com/dvoyni/cog/issues/713), on the map [cog#611](https://github.com/dvoyni/cog/issues/611). Gathered 2026-11-07.

[cog#712](https://github.com/dvoyni/cog/issues/712) decided that gfx stays as it is and the gogpu plugin passes gfx's calls through to gogpu's own windows, surfaces and device, by way of `HostLoop`. gfx's `RequestAdapter` and `RequestDevice` answer the device `HostLoop.Start` creates. This note asks what the fork allows for that:

1. **Start:** what `HostLoop.Start` needs, and whether it can start with no window or a hidden one.
2. **Device options:** whether gogpu's `Config` takes gfx's adapter options, required features and required limits, and what the adapter's `Info`, `Features` and `Limits` answer.
3. **Device loss:** how a loss is detected on Vulkan, DX12 and Metal, and whether the device can be replaced while windows and surfaces survive.
4. **Other objects:** whether gfx's buffers, textures, pipelines and the rest can be created on gogpu's device from worker goroutines while `HostLoop`'s frame calls run on main.

It reuses [Which of gfx's surface calls each OS requires on the main thread](https://github.com/dvoyni/cog/blob/research/main-thread-surface-calls/research/main-thread-surface-calls.md) ([cog#710](https://github.com/dvoyni/cog/issues/710)) for threads, and [The gogpu/wgpu v0.34.2 API that gfx mirrors](https://github.com/dvoyni/cog/blob/research/gogpu-wgpu-api/research/gogpu-wgpu-api.md) for goroutine safety. Neither is repeated here.

## Sources and versions

| Source | Version |
|---|---|
| gogpu fork | `dvoyni/gogpu` [`92d976d`](https://github.com/dvoyni/gogpu/tree/92d976d655fb366878fa120272e97093bf71cf1b) (the `submodules/gogpu` pin at cog `6581d1b`) |
| gogpu/wgpu | [`v0.34.2`](https://github.com/gogpu/wgpu/tree/v0.34.2), from the fork's `go.mod` |
| gogpu/gputypes | [`v0.8.0`](https://github.com/gogpu/gputypes/tree/v0.8.0), from the fork's `go.mod` |
| cog | [`6581d1b`](https://github.com/dvoyni/cog/tree/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92) |

Below, `G:` is `https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/`, `W:` is `https://github.com/gogpu/wgpu/blob/v0.34.2/`, `T:` is `https://github.com/gogpu/gputypes/blob/v0.8.0/` and `C:` is `https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/`.

## Answer in short

| Question | Answer | Fork change |
|---|---|---|
| 1. Start | **Supported with a fork change** | A window-less `HostLoop.Start` that brings up the platform and the device with no surface, and one method that opens every window, the first included, with its surface |
| 2. Device options | **Supported with a fork change** | `Config.RequiredLimits` passed to `RequestDevice`, `Config.ForceFallbackAdapter` passed to `RequestAdapter`, and features and limits checked against the adapter before the device opens |
| 3. Device loss | **Supported with a fork change** on Vulkan and DX12; **not supported** on Metal | `BeginFrame` and `EndFrame` answer the acquire and present errors; a device replacement that keeps windows and surfaces. Metal needs a wgpu change: it never detects a loss |
| 4. Other objects | **Supported** | None. Keep gogpu off the GL backend |

## 1. Start

### What `Start` does today

- `HostLoop.Start` runs `start` with the inline render loop and no modal callback ([G:hostloop.go#L51-L61](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/hostloop.go#L51-L61)). `start` does these steps in order ([G:hostloop.go#L185-L207](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/hostloop.go#L185-L207)):
  1. `initPlatform`: initializes the platform manager and **creates the primary window** ([G:app.go#L542-L598](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/app.go#L542-L598)).
  2. `initRenderer`, which calls `newRenderer(platWindow, …)` ([G:app.go#L697-L711](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/app.go#L697-L711)). That creates the instance, then **the window's surface**, then the adapter (with the surface as `CompatibleSurface`) and the device, then configures the surface ([G:renderer.go#L264-L300](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/renderer.go#L264-L300), [L381-L409](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/renderer.go#L381-L409)).
  3. `platWindow.Show()`, unconditionally.
  4. `registerPrimaryWindow`, which wraps that window as `App.PrimaryWindow()` with `visible: true` ([G:app.go#L989-L1005](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/app.go#L989-L1005)).
  5. `startRunLoop`.
- **So `Start` cannot start with no window.** It always creates the primary window together with its surface and the device, and the device is made from that window's surface.
- **It cannot start hidden.** `platform.Config` has no visibility field ([G:internal/platform/platform.go#L27-L43](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/internal/platform/platform.go#L27-L43)), and `start` calls `Show` itself. `Window.Hide` exists ([G:window_manager.go#L163-L168](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/window_manager.go#L163-L168)), but only after `Start` has shown the window, so the window flashes.
- **The primary window comes before the device, and every other window after it.** `App.NewWindow` refuses to run before `Start`: "NewWindow called before Run()" when `a.manager` or `a.renderer` is nil ([G:window_manager.go#L334-L337](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/window_manager.go#L334-L337)). After `Start` it works under `HostLoop`, because it goes through `a.renderLoop`, which is the inline loop.
- **Running is tied to the primary window.** `Running` is false once the primary window closes ([G:hostloop.go#L76-L81](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/hostloop.go#L76-L81)).
- **Threads:** every `HostLoop` call must run on the thread that called `Start`, which must be the main thread ([G:hostloop.go#L26-L29](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/hostloop.go#L26-L29)). The platform manager's `Init` "must be on main thread" ([G:app.go#L540-L546](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/app.go#L540-L546)).

### What the device needs from a window

- **On the backends gfx targets, nothing.** The surface goes before the adapter only for GLES: "GLES (EGL) requires a window handle for context creation, but creating the surface early is harmless for other backends" ([G:renderer.go#L281-L284](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/renderer.go#L281-L284)).
- **The fork already makes a device with no window.** `NewHeadlessRenderer` calls `initAdapterDevice(nil)` ([G:renderer.go#L311-L353](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/renderer.go#L311-L353)). wgpu takes the surface-less path when `CompatibleSurface` is nil ([W:instance_native.go#L71-L82](https://github.com/gogpu/wgpu/blob/v0.34.2/instance_native.go#L71-L82)).
- **One Vulkan difference to check.** With a surface, the Vulkan adapter is "surface-qualified": it picks a graphics queue family that `vkGetPhysicalDeviceSurfaceSupportKHR` says can present ([W:hal/vulkan/adapter.go#L30-L33](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/vulkan/adapter.go#L30-L33), [L379-L405](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/vulkan/adapter.go#L379-L405)). Without one, it takes the first graphics family and does not check presentation. Desktop drivers present from their graphics family, but a window-less start should confirm presentation when the first surface is configured.
- **Device creation has no main-thread rule in gogpu itself.** `Run` creates the renderer on its render thread, not on main: `initRenderer` goes through `renderLoop.RunOnRenderThreadVoid`, which is `thread.NewRenderLoop()` under `Run` ([G:app.go#L385-L394](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/app.go#L385-L394), [L697-L711](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/app.go#L697-L711)).

### How this meets cog

- gfx requests the adapter and the device in its `Start` ([C:gfx/plugin.go#L106-L141](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/gfx/plugin.go#L106-L141)), on the goroutine that called `Spawn` ([C:kernel/supervisor.go#L127-L153](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/kernel/supervisor.go#L127-L153)).
- platform opens windows later, through `WindowsSocket.Open`, from the pinned `ProcessEvents` ([C:platform/sockets.go#L3-L12](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/platform/sockets.go#L3-L12)).
- **Without a fork change** the gogpu plugin would have to call `Start` before gfx's `Start`, on main. The window would show before platform asks for any, and the plugin would have to hand that window to platform's first `Open` and apply the entry to it as a change. Closing it would end `Running`. That leans on accidents of the order things happen in, and is not recommended.

### Fork change

- **`HostLoop.Start` without a window.** It initializes the platform manager and creates the instance, adapter and device with no surface, as `NewHeadlessRenderer` does. Nothing is shown.
- **One method that opens a window with its surface,** for every window, the first included. That is `App.NewWindow`'s body without the parts written for `Run`:
  - its secondary-window default of vsync off ([G:window_manager.go#L376](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/window_manager.go#L376));
  - its unconditional `Show` ([L397](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/window_manager.go#L397));
  - its live-resize hook, which draws `App`'s own frame ([L417-L436](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/window_manager.go#L417-L436); the primary window's equivalent is [cog#714](https://github.com/dvoyni/cog/issues/714)).
- **`Renderer.primary` becomes optional.** Today `Resize`, `Destroy`, `SurfaceFormat` and the live-resize hooks assume it exists ([G:renderer.go#L541-L556](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/renderer.go#L541-L556), [L1973-L1990](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/renderer.go#L1973-L1990), [G:device_provider.go#L43-L46](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/device_provider.go#L43-L46)). `Running` would then no longer hang on a primary window.
- **The thread stays main,** since the platform manager's `Init` needs it. So the application calls `Spawn` on the main thread, as the kernel's own example does ([C:kernel/supervisor.go#L79-L92](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/kernel/supervisor.go#L79-L92)). The alternative is for the change to leave the device half callable from any thread, as `Run` already treats it.

## 2. Device options

### What gfx asks

gfx's `Config` has `PowerPreference`, `ForceFallbackAdapter`, `RequiredFeatures` and `RequiredLimits`. A zero `RequiredLimits` becomes `DefaultLimits()`, passed explicitly ([C:gfx/plugin.go#L33-L44](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/gfx/plugin.go#L33-L44), [L106-L141](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/gfx/plugin.go#L106-L141)). `RequestAdapterOptions` has no backend field ([C:gfx/descriptors.go#L543-L557](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/gfx/descriptors.go#L543-L557)).

### Option by option

| gfx asks | gogpu today | Verdict |
|---|---|---|
| `PowerPreference` | `Config.PowerPreference` ([G:config.go#L89-L93](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/config.go#L89-L93)) is passed to `RequestAdapterOptions` ([G:renderer.go#L382-L387](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/renderer.go#L382-L387)). The values are equal: None 0, LowPower 1, HighPerformance 2 ([T:adapter.go#L128-L139](https://github.com/gogpu/gputypes/blob/v0.8.0/adapter.go#L128-L139)) | supported, by cast |
| `ForceFallbackAdapter` | No `Config` field, and `initAdapterDevice` does not set it. wgpu has it ([W:types.go#L100-L109](https://github.com/gogpu/wgpu/blob/v0.34.2/types.go#L100-L109)) and honours it by returning the first CPU adapter ([W:core/instance.go#L325-L336](https://github.com/gogpu/wgpu/blob/v0.34.2/core/instance.go#L325-L336)). `Config.GraphicsAPI = GraphicsAPISoftware` gets the software adapter today by another road ([G:config.go#L63-L66](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/config.go#L63-L66), [G:gpu/backend/native/hal_vulkan.go#L16-L27](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/gpu/backend/native/hal_vulkan.go#L16-L27)) | fork change: `Config.ForceFallbackAdapter`, passed through |
| backend (not in gfx's `Config`) | `Config.GraphicsAPI`: Auto, Vulkan, DX12, Metal, GLES, Software. Auto includes GL on Linux and Windows ([G:gpu/backend/native/hal_windows.go#L21-L34](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/gpu/backend/native/hal_windows.go#L21-L34)) | supported; see question 4 for keeping GL out |
| `RequiredFeatures` | `Config.RequiredFeatures` is passed to `DeviceDescriptor.RequiredFeatures` ([G:config.go#L95-L109](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/config.go#L95-L109), [G:renderer.go#L400-L407](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/renderer.go#L400-L407)). The bits equal gfx's `FeatureFlags` ([T:features.go#L11-L49](https://github.com/gogpu/gputypes/blob/v0.8.0/features.go#L11-L49), [C:gfx/enums.go#L83-L100](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/gfx/enums.go#L83-L100), [C:gfx/queries.go#L11](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/gfx/queries.go#L11)) | supported, by cast, but not checked on Vulkan or Metal (next section) |
| `RequiredLimits` | No `Config` field, and `initAdapterDevice` passes none. wgpu turns a zero `RequiredLimits` into "the adapter's actual hardware limits" ([W:adapter_native.go#L80-L90](https://github.com/gogpu/wgpu/blob/v0.34.2/adapter_native.go#L80-L90)). So gogpu's device always gets the adapter's full limits, exactly what gfx's explicit `DefaultLimits()` exists to avoid | fork change: `Config.RequiredLimits`, passed through |

### What wgpu checks when the device opens

- **On the path gogpu uses, wgpu passes features and limits straight to the HAL** (`requestDeviceHAL`, [W:adapter_native.go#L70-L120](https://github.com/gogpu/wgpu/blob/v0.34.2/adapter_native.go#L70-L120)). The feature check in `core.CreateDevice` ([W:core/device.go#L33-L38](https://github.com/gogpu/wgpu/blob/v0.34.2/core/device.go#L33-L38)) is on the other, ID-based path.
- **DX12** refuses features the adapter lacks ([W:hal/dx12/adapter.go#L324-L330](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/dx12/adapter.go#L324-L330)).
- **Vulkan** ignores both arguments: `Open(_ gputypes.Features, _ gputypes.Limits)` ([W:hal/vulkan/adapter.go#L27-L29](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/vulkan/adapter.go#L27-L29)). It enables every core feature the physical device has (`PEnabledFeatures: &a.features`, [L118](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/vulkan/adapter.go#L118)).
- **Metal** takes them and does not check them ([W:hal/metal/adapter.go#L40-L70](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/metal/adapter.go#L40-L70)).
- **No backend checks limits against the adapter.**
- **The device keeps what was asked, and validates against it.** `core.NewDevice` stores the requested features and limits ([W:core/resource.go#L172-L196](https://github.com/gogpu/wgpu/blob/v0.34.2/core/resource.go#L172-L196)), and wgpu checks calls against them: buffer size ([L418-L425](https://github.com/gogpu/wgpu/blob/v0.34.2/core/resource.go#L418-L425)), texture dimensions, bind groups, color attachments and bindings per group ([W:core/validate.go#L116-L162, L282-L287, L485-L532](https://github.com/gogpu/wgpu/blob/v0.34.2/core/validate.go#L116-L162)).
- So passing `DefaultLimits()` gives gfx a device that refuses what WebGPU's defaults refuse, which is what gfx wants.
- **gogpu's own comment promises more than happens.** It says an adapter that cannot supply a feature "fails the request by name" ([G:config.go#L95-L99](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/config.go#L95-L99)). That holds only on DX12. The fork change should check required features against `adapter.Features()` and required limits against `adapter.Limits()` before `RequestDevice`, so that gfx's `Start` fails for a missing feature or limit on every backend.

### Converting gfx's limits

- gfx's `Limits` carries gputypes' WebGPU fields with the same names and types ([C:gfx/descriptors.go#L361-L396](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/gfx/descriptors.go#L361-L396), [T:limits.go#L7-L69](https://github.com/gogpu/gputypes/blob/v0.8.0/limits.go#L7-L69)).
- gputypes adds `MaxPushConstantSize`, `MaxNonSamplerBindings` and the ray-tracing limits, so the conversion is a field-by-field copy, not a cast.
- Take the extra fields from `gputypes.DefaultLimits()`: `MaxNonSamplerBindings` 1000000, push constants 0, ray tracing 0.

### What the adapter answers

- **How to reach the adapter.** `DeviceProvider` gives only `Device`, `Queue` and `SurfaceFormat` ([G:device_provider.go#L14-L23](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/device_provider.go#L14-L23)). `App.GPUContextProvider().Adapter()` returns the adapter as a handle ([G:gpucontext_adapter.go#L52-L58](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/gpucontext_adapter.go#L52-L58), [L229-L237](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/gpucontext_adapter.go#L229-L237)), which `wgpu.AdapterFromHandle` turns back into the `*wgpu.Adapter` ([W:gpucontext_helpers.go#L31](https://github.com/gogpu/wgpu/blob/v0.34.2/gpucontext_helpers.go#L31)). No fork change is needed to read it.
- **`Info()`** is `gputypes.AdapterInfo`: Name, Vendor, VendorID, DeviceID, DeviceType, Driver, DriverInfo and Backend ([T:adapter.go#L109-L126](https://github.com/gogpu/gputypes/blob/v0.8.0/adapter.go#L109-L126)), read at `RequestAdapter` ([W:instance_native.go#L93-L104](https://github.com/gogpu/wgpu/blob/v0.34.2/instance_native.go#L93-L104)). These are gfx's `AdapterInfo` fields one for one, and the `DeviceType` and `Backend` values are equal ([C:gfx/enums.go#L5-L27](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/gfx/enums.go#L5-L27)).
- **`Features()`** is every optional feature the adapter supports.
- **`Limits()`** is the hardware's limits. Vulkan reads them from the physical-device properties ([W:hal/vulkan/api.go#L628](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/vulkan/api.go#L628)), DX12 builds them from its feature level ([W:hal/dx12/adapter.go#L278](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/dx12/adapter.go#L278)), and Metal uses a fixed table ([W:hal/metal/api.go#L114-L150](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/metal/api.go#L114-L150)).
- **The device answers what was requested.** `Device.Features()` and `Device.Limits()` are the requested values ([W:device_native.go#L52-L59](https://github.com/gogpu/wgpu/blob/v0.34.2/device_native.go#L52-L59)). Today that is `Config.RequiredFeatures` and the adapter's full limits.

## 3. Device loss

### How wgpu reports a loss

- **Vulkan: typed, at most points.**
  - `VK_ERROR_DEVICE_LOST` becomes `hal.ErrDeviceLost`, which is `wgpu.ErrDeviceLost` ([W:error.go#L13-L19](https://github.com/gogpu/wgpu/blob/v0.34.2/error.go#L13-L19)).
  - Typed points: the acquire ([W:hal/vulkan/swapchain.go#L1104-L1106](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/vulkan/swapchain.go#L1104-L1106)), the present ([L1278-L1280](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/vulkan/swapchain.go#L1278-L1280)), fence waits and status ([W:hal/vulkan/device.go#L1488-L1489, L1523-L1524](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/vulkan/device.go#L1488-L1524), [W:hal/vulkan/fence.go#L144-L145](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/vulkan/fence.go#L144-L145)) and `mapVulkanResult` ([W:hal/vulkan/swapchain_platform.go#L72-L90](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/vulkan/swapchain_platform.go#L72-L90)).
  - **Not typed:** `vkQueueSubmit`'s failure, reported only as a number ([W:hal/vulkan/queue.go#L368-L373](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/vulkan/queue.go#L368-L373)). A loss at submit is seen at the next acquire, present or fence poll.
- **DX12: not typed as `ErrDeviceLost`, but mappable.**
  - Every check calls `GetDeviceRemovedReason` and wraps the `d3d12.HRESULTError` with `%w`. The checks are at submit ([W:hal/dx12/queue.go#L199-L204](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/dx12/queue.go#L199-L204)), at `checkHealth`, which resource and pipeline creation call ([W:hal/dx12/device.go#L695-L704](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/dx12/device.go#L695-L704)), and at `CreateTexture` ([L1244-L1249](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/dx12/device.go#L1244-L1249)). `Present` wraps its error the same way ([W:hal/dx12/queue.go#L825-L831](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/dx12/queue.go#L825-L831)).
  - A backend can map them with `errors.As` to `d3d12.HRESULTError` and compare against `DXGI_ERROR_DEVICE_REMOVED`, `_HUNG` and `_RESET` ([W:hal/dx12/d3d12/error.go#L10-L24, L58-L60](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/dx12/d3d12/error.go#L58-L60)). The package is public and builds only on Windows.
- **Metal: never.** Nothing in `hal/metal` reads a command buffer's status or error. `MTLCommandBufferStatusError` is declared and never used ([W:hal/metal/types.go#L422](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/metal/types.go#L422)). This is the gap gfx's design already lists ([C:gfx/docs/design.md#L488-L489](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/gfx/docs/design.md#L488-L489)). It is in wgpu, not in the fork.

### What gogpu does with those errors

- **It never returns its own `ErrDeviceLost`.** It is declared ([G:errors.go#L26-L29](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/errors.go#L26-L29)) and returned nowhere.
- **`BeginFrame` swallows acquire errors.** A device-lost acquire falls into the default case of `recoverFromAcquireError`, which logs it and reconfigures ([G:renderer.go#L685-L708](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/renderer.go#L685-L708)). `BeginFrame` answers only `ok=false` ([G:hostloop.go#L99-L139](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/hostloop.go#L99-L139)).
- **`EndFrame` swallows present errors.** A present error other than outdated or surface-lost is logged as "PRESENT ERROR" ([G:renderer.go#L884-L923](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/renderer.go#L884-L923)), and `EndFrame` answers nothing ([G:hostloop.go#L156-L167](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/hostloop.go#L156-L167)). A failed frame-encoder `Finish` is only logged too ([G:renderer.go#L1023-L1035](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/renderer.go#L1023-L1035)).
- So gfx's `AcquireFramebuffer` and `Present`, which go through `BeginFrame` and `EndFrame`, cannot answer `ErrDeviceLost` as gfx requires ([C:gfx/sockets.go#L40-L44](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/gfx/sockets.go#L40-L44)).
- gfx's other calls reach wgpu directly (question 4), so the backend maps their errors itself.

### Replacing the device

- **What gfx does.** It releases the backend device, asks the same adapter for a new one, falls back to a fresh adapter from the instance, recreates every object, and marks every surface for configuring ([C:gfx/device.go#L807-L840](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/gfx/device.go#L807-L840), [C:gfx/sockets.go#L130-L137](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/gfx/sockets.go#L130-L137)).
- **gogpu has no such path.**
  - `Renderer.device` and `adapter` are set once, in `initAdapterDevice` ([G:renderer.go#L381-L409](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/renderer.go#L381-L409)).
  - The only teardown, `Renderer.Destroy`, releases the primary surface together with the device and adapter ([G:renderer.go#L1973-L2056](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/renderer.go#L1973-L2056)).
  - The `SurfaceLost` state says "caller must recreate", and nothing does ([G:renderer.go#L33-L46](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/renderer.go#L33-L46)).
- **wgpu supports what a replacement needs.**
  - **Releasing a device keeps the surfaces.** `Device.Release` discards any acquired surface texture, waits for idle and ignores the wait's error, destroys the device, and then retires every surface configured on it ([W:device_native.go#L1007-L1069](https://github.com/gogpu/wgpu/blob/v0.34.2/device_native.go#L1007-L1069), [W:surface_native.go#L551-L568](https://github.com/gogpu/wgpu/blob/v0.34.2/surface_native.go#L551-L568)). The surfaces remain, unconfigured. That is what gfx's `BackendDevice.Release` requires: the surfaces unconfigured and the acquired framebuffer dropped.
  - **A surface takes a new device.** `Surface.Configure` accepts any device of the same instance, and recreates the HAL surface if the backend changed ([W:surface_native.go#L257-L311](https://github.com/gogpu/wgpu/blob/v0.34.2/surface_native.go#L257-L311)).
  - **A fresh adapter comes from the same instance.** The renderer keeps the instance until `ReleaseInstance`.
- **`HostLoop` already reconfigures a surface that cannot render,** even at an unchanged size ([G:hostloop.go#L117-L123](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/hostloop.go#L117-L123)). `RenderTarget.configure` reads the device at the call ([G:renderer.go#L446-L458](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/renderer.go#L446-L458)). So once the device is replaced and each surface is marked unconfigured, the next `BeginFrame` configures it on the new device.

### Fork change

- **Surface the errors.** `BeginFrame` and `EndFrame` answer the acquire and present errors, or the `Context` keeps them for the caller, instead of only logging them. Device-lost errors stay wrapped, so `errors.Is(err, wgpu.ErrDeviceLost)` holds on Vulkan and `errors.As` to `HRESULTError` on DX12.
- **Replace the device without touching windows.** Split `Renderer.Destroy`'s device half into a release and a request:
  - The release frees gogpu's device-level objects (the built-in pipelines, the bind-group cache, the blit pipeline), drains the deferred destroys and the submission tracker, and calls `Device.Release`. It can also release the adapter.
  - The request asks the same or a fresh adapter for a device with the same descriptor, then marks every `RenderTarget` unconfigured (`SurfaceReady`, size zero).
  - Windows and their `wgpu.Surface`s survive.
- **It must be callable from gfx's recovery job.** That job runs on a worker under the write lock on `gfx.Device` ([C:gfx/device.go#L892](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/gfx/device.go#L892)), and recovery is not among the `gfx.Tasks` a backend may pin ([C:gfx/resources.go#L8-L26](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/gfx/resources.go#L8-L26)).
  - The write lock keeps the pinned `AcquireFramebuffers` and `PresentFramebuffers` out, since both hold its read lock ([C:gfx/framepool.go#L383, L484](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/gfx/framepool.go#L383)). So no `BeginFrame` or `EndFrame` overlaps the replacement.
  - `PumpEvents` takes no gfx lock, and it reconfigures surfaces (see *Also found*). That reconfigure has to move out of the pump.
- **Metal stays unsupported.** The replacement itself works on any backend, but nothing tells the backend to start it. That needs a wgpu change: check each command buffer's status in its completion handler and report `ErrDeviceLost`.

## 4. Other objects

- **wgpu documents its device as concurrent-safe.** "Instance, Adapter, and Device are safe for concurrent use. Encoders … are NOT thread-safe" ([W:doc.go#L31-L34](https://github.com/gogpu/wgpu/blob/v0.34.2/doc.go#L31-L34), [W:device_native.go#L17-L21](https://github.com/gogpu/wgpu/blob/v0.34.2/device_native.go#L17-L21)). `Queue` serializes `Submit`, `WriteBuffer` and `WriteTexture` under one mutex ([W:queue_native.go#L14-L22](https://github.com/gogpu/wgpu/blob/v0.34.2/queue_native.go#L14-L22)). The [wgpu API note](https://github.com/dvoyni/cog/blob/research/gogpu-wgpu-api/research/gogpu-wgpu-api.md) covers this in full.
- **`HostLoop`'s frame calls touch only gogpu's own objects.** That means the `RenderTarget`s, the frame encoder, the built-in pipelines, the tracker and the deferred-destroy queue, all on the main thread. gfx's objects would be created through the `*wgpu.Device` that `DeviceProvider().Device()` returns, and gogpu never sees them.
- **gogpu's "GPU operations are thread-bound" comments are its own convention for `Run`,** not a wgpu rule ([G:window_manager.go#L357-L359](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/window_manager.go#L357-L359), [G:app.go#L697-L700](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/app.go#L697-L700)).
- **Metal is safe from workers.** Its HAL opens an autorelease pool around its calls and locks the OS thread only while the pool is open ([W:hal/metal/objc.go#L490-L503](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/metal/objc.go#L490-L503)), so any worker works.
- **Keep gogpu off the GL backend.** The GLES HAL binds its context to one OS thread ([W:hal/gles/adapter_context.go#L18-L30](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/gles/adapter_context.go#L18-L30)), and `GraphicsAPIAuto` includes GL on Linux and Windows. The gogpu plugin should set `Config.GraphicsAPI` to Vulkan, DX12 or Metal explicitly, or turn down a GL adapter.
- **Vulkan ties a submit made mid-frame to the frame.** Between an acquire and its present, the first submit on the queue waits on the acquire semaphore. Every submit signals a present semaphore that the present waits on ([W:hal/vulkan/queue.go#L276-L309](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/vulkan/queue.go#L276-L309)).
  - A worker's submit made while the main thread holds an acquired image therefore joins that frame: it is correct, but it delays that present.
  - gfx's own frame submits are meant to join the frame. Its other submits, such as readbacks, could be kept outside the acquire-to-present window if the latency shows up.
- **Already known, unchanged by `HostLoop`:**
  - `Release` races recording for the resources whose `released` flag is a plain `bool`;
  - under `-tags rust` the device is not concurrent-safe ([C:gfx/docs/design.md#L470-L493](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/gfx/docs/design.md#L470-L493)).
- **After a device replacement,** the backend must read the device from `DeviceProvider` again. The provider reads `renderer.device` on each call ([G:device_provider.go#L31-L41](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/device_provider.go#L31-L41)).

## Also found

- **`PumpEvents` reconfigures surfaces under `HostLoop`.** For a resize of any window but the primary, `handleSecondaryResize` calls `ws.resize(…, a.renderer.device, …)` through the render loop ([G:app.go#L781-L799](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/app.go#L781-L799)). Under `HostLoop` the render loop is inline, so the reconfigure happens inside `PumpEvents`, on main, outside gfx's lock on `gfx.Device`.
  - The primary window's resize is only recorded and applied at the next `BeginFrame` ([G:hostloop.go#L63-L74, L105-L109](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/hostloop.go#L63-L74)).
  - With the change in question 1, every window is opened the secondary way, so this path would carry every resize. It also contradicts gfx's rule that configure runs under the write lock ([C:gfx/sockets.go#L116-L118](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/gfx/sockets.go#L116-L118)), and it would race a device replacement.
  - The fix is to record secondary resizes and apply them at `BeginFrame`, as for the primary.
- **A stale comment in `initAdapterDevice`.** It says `surfaceHint` "is non-nil only for GLES", but `newRenderer` always passes the primary surface ([G:renderer.go#L292-L295, L378-L385](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/renderer.go#L378-L385)). Not a bug: it only qualifies the adapter for that surface.

## What would change the answers

- **Question 1:** if cog's application spawned its kernel off the main thread, `Start` as it stands would need a pinned job to run in, which plugin `Start` cannot wait on. The fork change's device half would then have to run on any thread.
- **Question 3:** if wgpu's Metal backend learned to detect a loss, Metal would join Vulkan and DX12 at "supported with a fork change". If gogpu's frame errors stay swallowed, a loss would surface only through gfx's direct wgpu calls: object creation on DX12, and fence polls on Vulkan through `Poll`. A frame that only acquires and presents would never see it.
- **Question 4:** a gogpu change that made the frame path use device-level state shared with gfx would need a lock. Nothing in the fork at `92d976d` does.
