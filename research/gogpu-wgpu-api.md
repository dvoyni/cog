# The gogpu/wgpu v0.34.2 API that gfx mirrors

Research for [cog#660](https://github.com/dvoyni/cog/issues/660), on the map [cog#608](https://github.com/dvoyni/cog/issues/608). Gathered 2026-11-05.

The question: what public API does gfx mirror one to one, at the versions the fork pins in `submodules/gogpu/go.mod`? That means every type and method from the instance down to the passes, every descriptor and enum and the package that holds it, which calls are async, which are goroutine-safe, and where the API differs from the WebGPU spec and from the `-tags rust` fallback (wgpu-native v29 through go-webgpu/webgpu).

This file reports facts. It does not design gfx.

## Sources and versions

| Module | Version | Role |
|---|---|---|
| `github.com/gogpu/wgpu` | v0.34.2 | the API itself (pinned in `submodules/gogpu/go.mod`) |
| `github.com/gogpu/gputypes` | v0.8.0 | enums and plain structs the API takes |
| `github.com/gogpu/gpucontext` | v0.31.3 | opaque handles for sharing a device across gogpu packages |
| `github.com/go-webgpu/webgpu` | v0.5.5 | the `-tags rust` backend (an indirect dependency of the fork) |

All four were read from the Go module cache (`$GOMODCACHE/github.com/<module>@<version>/`). Citations use these prefixes:

- `W:` is `github.com/gogpu/wgpu@v0.34.2/`
- `T:` is `github.com/gogpu/gputypes@v0.8.0/`
- `C:` is `github.com/gogpu/gpucontext@v0.31.3/`
- `R:` is `github.com/go-webgpu/webgpu@v0.5.5/`

The exported surface of each build was listed with `go doc -all` (native, and `GOOS=js GOARCH=wasm` for the browser) and with `go list -tags rust -f '{{.GoFiles}}'` plus a signature grep over those files for the Rust build, because `go doc` ignores `-tags`. WebGPU spec claims refer to the W3C [WebGPU](https://www.w3.org/TR/webgpu/) Candidate Recommendation.

## Answer in short

- **One Go package, three builds.** The public API is all in package `wgpu`. Each object has one file per build: `*_native.go` (`!rust && !(js && wasm)`, the pure-Go HAL), `*_rust.go` (`rust`) and `*_browser.go` (`js && wasm`). The type and method names are the same across the three. A few signatures, struct fields and constant values differ (see [The `-tags rust` build](#the--tags-rust-build)).
- **The object set is smaller than WebGPU's.** It has Instance, Adapter, Device, Queue, Surface, SurfaceTexture, Buffer, Texture, TextureView, Sampler, ShaderModule, BindGroupLayout, BindGroup, PipelineLayout, RenderPipeline, ComputePipeline, CommandEncoder, CommandBuffer, RenderPassEncoder, ComputePassEncoder and Fence. **There is no QuerySet, no RenderBundle and no timestamp or occlusion query anywhere in the public API.** The HAL has query sets and render bundles (`W:hal/api.go:207`, `W:hal/api.go:218`), but `wgpu` exposes neither. In core, pass timestamp writes are dropped: "QuerySet HAL integration pending" (`W:core/command.go:1726-1729`).
- **There is no indirect-count draw either.** `RenderPassEncoder` has `DrawIndirect`, `DrawIndexedIndirect`, `MultiDrawIndirect` and `MultiDrawIndexedIndirect` (`W:renderpass_native.go:244-297`), but nothing that takes a count buffer. `FeatureMultiDrawIndirectCount` exists only as a feature bit (`T:features.go`). In the Vulkan bindings, `vkCmdDrawIndirectCount` is a `TODO` (`W:hal/vulkan/vk/commands_gen.go:4847`).
- **gputypes is the leaf.** It imports only `strings` (`T:downlevel.go:3`) and has `go 1.25` and no requirements (`T:go.mod`). Its enums are plain integers. The structs `wgpu` takes from it carry pointers in `BindGroupLayoutEntry` (five optional `*…BindingLayout` fields), `PrimitiveState.StripIndexFormat *IndexFormat` and `ColorTargetState.Blend *BlendState`. They carry slices in `VertexBufferLayout.Attributes`, and strings in `AdapterInfo`. gputypes also has its own descriptor structs, which hold `uintptr` handles, `interface` values and `map[string]float64`. `wgpu` does not take those in its API; it uses them only to talk to the HAL.
- **Nothing takes a callback, and only mapping is async.** Adapter and device requests, pipeline creation and `PopErrorScope` are synchronous. Buffer mapping is either `Map(ctx, …)`, which blocks, or `MapAsync`, which returns a `*MapPending` you poll or wait on. Completion of submitted work is a submission index from `Submit`, compared against `Queue.Poll()`. No device-lost or uncaptured-error callback exists.
- **Errors come back as Go errors on native, and error scopes are inert there.** Every `Create*` returns `(T, error)`. Encoder and pass commands record a deferred error that surfaces from `Finish()` or `End()`. On native, nothing ever reports into the device's error-scope stack: `reportError` has no caller (`W:core/error_scope.go:220`), so `PopErrorScope` always returns nil. It panics on an empty stack (`W:core/error_scope.go:205-211`).
- **Goroutine-safety as documented:** Instance, Adapter, Device and Queue are safe for concurrent use, except their `Release`. CommandEncoder, RenderPassEncoder and ComputePassEncoder are not (`W:doc.go:31-34`, `W:device_native.go:17-21`, `W:queue_native.go:14-21`). Resources are safe to share read-only. Their `Release` uses an atomic flag only for Buffer, BindGroup and Device. Texture, TextureView, Sampler, ShaderModule, the layouts, the pipelines and Surface use a plain `bool` (`W:texture_native.go:16`, `W:sampler_native.go:11`, `W:surface_native.go:48`).
- **The Rust fallback is not quite "the same API".** Under `-tags rust`, `ComputePipelineDescriptor` loses `Constants` and `ZeroInitializeWorkgroupMemory` (`W:descriptor_rust.go:223-228`), so code that sets them does not compile. Beyond that:
  - `RequestDevice` drops `RequiredFeatures` (`W:adapter_rust.go:53-63`).
  - `Queue.Poll()` and `LastSubmissionIndex()` always return 0 (`W:queue_rust.go:48`, `W:queue_rust.go:105`).
  - Two enums are converted with an off-by-one cast: `ErrorFilter` (`W:device_rust.go:393-398` against `R:wgpu/enums.go:422-431`) and `StencilOperation` (`W:descriptor_rust.go:103-114`, `W:device_rust.go:597-608`).
  - go-webgpu documents a single object as unsafe to use from several goroutines, apart from a short list (`R:wgpu/doc.go:6-27`).

## Package layout

| Package | What it holds | Imports |
|---|---|---|
| `wgpu` | every object type, its methods, its descriptors, the error sentinels, the `MapMode`/`MapState`/`PollType` enums, and constant aliases into gputypes (`W:types.go:15-104`) | gputypes, gpucontext, `wgpu/core`, `wgpu/hal`, naga (native); go-webgpu (rust) |
| `gputypes` | every WebGPU enum and bitmask, plus the value structs `wgpu` descriptors embed: `BindGroupLayoutEntry`, `PrimitiveState`, `MultisampleState`, `ColorTargetState`, `VertexBufferLayout`, `Color`, `Viewport`, `ScissorRect`, `DrawArgs`, `Limits`, `Features`, `AdapterInfo` | `strings` only |
| `gpucontext` | `unsafe.Pointer` handle tokens `Device`, `Queue`, `Adapter`, `Surface`, `Instance` (`C:webgpu_types.go:35-97`), the `DeviceProvider` interface (`C:device_provider.go:31`), and windowing and event interfaces for gogpu apps | gputypes, `image`, `sync`, `time`, `unsafe`, `strings` |
| `wgpu/core`, `wgpu/hal` | validation, tracking, and the backend interface | exported, but below the public API. `HalDevice()`, `Surface.HAL()` and the `New*FromHAL` constructors expose them |

`wgpu` converts to and from the gpucontext tokens with `DeviceToHandle`/`DeviceFromHandle` and the same pair for Queue, Adapter, Surface and Instance (`W:gpucontext_helpers.go:15-77`). gpucontext adds nothing to the GPU API itself.

## The object API (native build)

Signatures are exact. A `Release()` on each type is implied unless noted.

### Instance

`type Instance struct` (`W:instance_native.go:26`). Guarded by `mu sync.Mutex` (`W:instance_native.go:28`).

| Signature | Where | Notes |
|---|---|---|
| `func CreateInstance(desc *InstanceDescriptor) (*Instance, error)` | `W:instance_native.go:36` | nil means all backends |
| `func (i *Instance) RequestAdapter(opts *RequestAdapterOptions) (*Adapter, error)` | `W:instance_native.go:57` | synchronous |
| `func (i *Instance) CreateSurface(displayHandle, windowHandle uintptr) (*Surface, error)` | `W:surface_native.go:85` | legacy raw handles |
| `func (i *Instance) CreateSurfaceFromTarget(target SurfaceTarget) (*Surface, error)` | `W:surface_native.go:92` | retains the provider |
| `func (i *Instance) CreateSurfaceUnsafe(target SurfaceTargetUnsafe) (*Surface, error)` | `W:surface_native.go:106` | the caller keeps the native objects alive |
| `func (i *Instance) Release()` | `W:instance_native.go:234` | |

`InstanceDescriptor{Backends gputypes.Backends; Flags gputypes.InstanceFlags}` (`W:instance_native.go:15`). `RequestAdapterOptions{PowerPreference gputypes.PowerPreference; ForceFallbackAdapter bool; CompatibleSurface *Surface}` (`W:types.go:100`).

Surface targets (`W:surface_target.go`):

- `type SurfaceTarget interface{ SurfaceTarget() (SurfaceTargetUnsafe, error) }` (:25)
- `type SurfaceTargetUnsafe struct{…}` (:48), built by:
  - `SurfaceTargetFromWindowsHWND(hinstance, hwnd uintptr)` (:69)
  - `SurfaceTargetFromXlibWindow(display, window uintptr)` (:78)
  - `SurfaceTargetFromWaylandSurface(display, surface uintptr)` (:87)
  - `SurfaceTargetFromAndroidNativeWindow(window uintptr)` (:97)
  - `SurfaceTargetFromMetalLayer(layer uintptr)` (:105)
  - `SurfaceTargetFromWebCanvasID(id uintptr)` (:115)
- `type HeadlessSurfaceTarget struct{}` (:60), for the software backend only
- `ErrInvalidSurfaceTarget` and `ErrUnsupportedSurfaceTarget` (:11, :15)

### Adapter

`type Adapter struct` (`W:adapter_native.go:20`). Its fields are immutable after creation.

| Signature | Where |
|---|---|
| `func (a *Adapter) Info() gputypes.AdapterInfo` | `W:adapter_native.go:32` |
| `func (a *Adapter) Features() gputypes.Features` | `W:adapter_native.go:35` |
| `func (a *Adapter) Limits() gputypes.Limits` | `W:adapter_native.go:38` |
| `func (a *Adapter) DownlevelCapabilities() gputypes.DownlevelCapabilities` | `W:adapter_native.go:42` |
| `func (a *Adapter) RequestDevice(desc *DeviceDescriptor) (*Device, error)` | `W:adapter_native.go:46` (synchronous) |
| `func (a *Adapter) GetSurfaceCapabilities(surface *Surface) *SurfaceCapabilities` | `W:adapter_native.go:162` |
| `func (a *Adapter) Release()` | `W:adapter_native.go:192` |

- `DeviceDescriptor{Label string; RequiredFeatures gputypes.Features; RequiredLimits gputypes.Limits}` (`W:adapter_native.go:13`).
- **A zero `RequiredLimits` means the adapter's full limits, not WebGPU's defaults** (`W:adapter_native.go:80-90`). To get the spec defaults, pass `wgpu.DefaultLimits()` (`W:types.go:112`, `T:limits.go:99`).
- `SurfaceCapabilities{Formats []gputypes.TextureFormat; PresentModes []gputypes.PresentMode; AlphaModes []gputypes.CompositeAlphaMode}` (`W:adapter_native.go:149`).

### Device

`type Device struct` (`W:device_native.go:22`). It is documented as "safe for concurrent use, except Release()" (`W:device_native.go:17-21`), and its `released` flag is an `atomic.Bool` (`W:device_native.go:26`).

| Signature | Where |
|---|---|
| `func (d *Device) Queue() *Queue` | `W:device_native.go:47` |
| `func (d *Device) Features() gputypes.Features` | `W:device_native.go:52` |
| `func (d *Device) Limits() gputypes.Limits` | `W:device_native.go:57` |
| `func (d *Device) CreateBuffer(desc *BufferDescriptor) (*Buffer, error)` | `W:device_native.go:62` |
| `func (d *Device) CreateTexture(desc *TextureDescriptor) (*Texture, error)` | `W:device_native.go:105` |
| `func (d *Device) CreateTextureView(texture *Texture, desc *TextureViewDescriptor) (*TextureView, error)` | `W:device_native.go:147` |
| `func (d *Device) CreateSampler(desc *SamplerDescriptor) (*Sampler, error)` | `W:device_native.go:191` |
| `func (d *Device) CreateShaderModule(desc *ShaderModuleDescriptor) (*ShaderModule, error)` | `W:device_native.go:229` |
| `func (d *Device) CreateBindGroupLayout(desc *BindGroupLayoutDescriptor) (*BindGroupLayout, error)` | `W:device_native.go:281` |
| `func (d *Device) CreatePipelineLayout(desc *PipelineLayoutDescriptor) (*PipelineLayout, error)` | `W:device_native.go:317` |
| `func (d *Device) CreateBindGroup(desc *BindGroupDescriptor) (*BindGroup, error)` | `W:device_native.go:365` |
| `func (d *Device) CreateRenderPipeline(desc *RenderPipelineDescriptor) (*RenderPipeline, error)` | `W:device_native.go:518` |
| `func (d *Device) CreateComputePipeline(desc *ComputePipelineDescriptor) (*ComputePipeline, error)` | `W:device_native.go:637` |
| `func (d *Device) CreateCommandEncoder(desc *CommandEncoderDescriptor) (*CommandEncoder, error)` | `W:device_native.go:787` (the HAL encoder comes from a mutex-guarded pool, `W:encoder_pool.go:24`) |
| `func (d *Device) PushErrorScope(filter ErrorFilter)` | `W:device_native.go:936` |
| `func (d *Device) PopErrorScope() *GPUError` | `W:device_native.go:942` |
| `func (d *Device) Poll(pollType PollType) bool` | `W:device_native.go:1093` |
| `func (d *Device) WaitIdle() error` | `W:device_native.go:947` |
| `func (d *Device) Release()` | `W:device_native.go:1007` (waits for idle, then tears down) |
| `func (d *Device) CreateFence() (*Fence, error)` | `W:device_native.go:841` |
| `func (d *Device) ResetFence(f *Fence) error` | `W:device_native.go:870` |
| `func (d *Device) GetFenceStatus(f *Fence) (bool, error)` | `W:device_native.go:886` |
| `func (d *Device) WaitForFence(f *Fence, value uint64, timeout time.Duration) (bool, error)` | `W:device_native.go:902` |
| `func (d *Device) DestroyFence(f *Fence)` | `W:device_native.go:862`, deprecated for `Fence.Release` |
| `func (d *Device) FreeCommandBuffer(cb *CommandBuffer)` | `W:device_native.go:920` |
| `func (d *Device) HalDevice() hal.Device` | `W:wrap.go:98` |
| `func NewDeviceFromHAL(halDevice hal.Device, halQueue hal.Queue, features gputypes.Features, limits gputypes.Limits, label string) (*Device, error)` | `W:wrap.go:20` |

- **There is no automatic pipeline layout.** The descriptors accept a nil `Layout`, but the Vulkan HAL rejects it: "automatic layouts are not supported" (`W:hal/vulkan/pipeline.go:31-32`). There is no `GetBindGroupLayout` on native pipelines.
- **`ShaderModule` exposes nothing but `Release`.** wgpu parses WGSL to a naga IR for its own late-binding checks and keeps the IR private (`W:shader_native.go:11`, `W:device_native.go:262-275`). A parse failure there is ignored.

### Queue

`type Queue struct` (`W:queue_native.go:21`). Documented as safe for concurrent use. `Submit`, `WriteBuffer` and `WriteTexture` are serialised by `mu sync.Mutex` (`W:queue_native.go:14-22`).

| Signature | Where | Notes |
|---|---|---|
| `func (q *Queue) Submit(commandBuffers ...*CommandBuffer) (uint64, error)` | `W:queue_native.go:50` | non-blocking; returns the submission index; flushes pending writes first; injects texture barriers at submit time (`W:queue_native.go:96-110`) |
| `func (q *Queue) Poll() uint64` | `W:queue_native.go:252` | last completed submission index, non-blocking |
| `func (q *Queue) WriteBuffer(buffer *Buffer, offset uint64, data []byte) error` | `W:queue_native.go:277` | offset and size 4-aligned, needs `CopyDst`, buffer not mapped |
| `func (q *Queue) WriteTexture(dst *ImageCopyTexture, data []byte, layout *ImageDataLayout, size *Extent3D) error` | `W:queue_native.go:344` | |
| `func (q *Queue) SetSwapchainSuppressed(suppressed bool)` | `W:queue_native.go:392` | Vulkan only; keeps offscreen submits off the swapchain semaphores |
| `func (q *Queue) LastSubmissionIndex() uint64` | `W:queue_native.go:402` | atomic |

**Who owns a submitted `CommandBuffer` is documented both ways.** `Submit` says "Command buffers are owned by the caller — free them after Poll confirms completion" (`W:queue_native.go:46`). `CommandBuffer.Release` says "Submit() takes ownership… A CommandBuffer MUST be either Submit()'d or Release()'d" (`W:encoder_native.go:829-838`).

### Surface and SurfaceTexture

`type Surface struct` (`W:surface_native.go:44`). Its state machine is Unconfigured, then Configured, then Acquired, then back to Configured. Those transitions are locked inside `core.Surface` (`W:core/resource.go:1659-1690`), but the wrapper's own fields (`device`, `released`, `swapchainTexture`) are plain and unguarded.

| Signature | Where |
|---|---|
| `func (s *Surface) Configure(device *Device, config *SurfaceConfiguration) error` | `W:surface_native.go:257` |
| `func (s *Surface) Unconfigure()` | `W:surface_native.go:314` |
| `func (s *Surface) GetCurrentTexture() (*SurfaceTexture, bool, error)` | `W:surface_native.go:327` (the bool is "suboptimal") |
| `func (s *Surface) Present(texture *SurfaceTexture) error` | `W:surface_native.go:379` |
| `func (s *Surface) PresentWithDamage(texture *SurfaceTexture, damageRects []image.Rectangle) error` | `W:surface_native.go:392` |
| `func (s *Surface) DiscardTexture()` | `W:surface_native.go:544` |
| `func (s *Surface) ActualExtent() (width, height uint32)` | `W:surface_native.go:530` (Vulkan may clamp the extent) |
| `func (s *Surface) SetPrepareFrame(fn core.PrepareFrameFunc)` | `W:surface_native.go:421`, where `PrepareFrameFunc func() (width, height uint32, changed bool)` (`W:core/resource.go:1648`) runs before each acquire and reconfigures on change |
| `func (s *Surface) SetPresentsWithTransaction(enabled bool)` | `W:surface_native.go:429` (Metal only) |
| `func (s *Surface) PresentPixels(data []byte, width, height uint32, damageRects []image.Rectangle) error` | `W:surface_native.go:456` (software backend only) |
| `func (s *Surface) WritePixels(data []byte, width, height uint32) error` | `W:surface_native.go:473` |
| `func (s *Surface) ReadPixels() ([]byte, error)` | `W:surface_native.go:493` (software backend only) |
| `func (s *Surface) HAL() hal.Surface` | `W:surface_native.go:624` |
| `func (s *Surface) Release()` | `W:surface_native.go:661` |
| `func NewSurfaceFromHAL(halSurface hal.Surface, label string) *Surface` | `W:wrap.go:70` |

- `SurfaceConfiguration{Width, Height uint32; Format gputypes.TextureFormat; Usage gputypes.TextureUsage; PresentMode gputypes.PresentMode; AlphaMode gputypes.CompositeAlphaMode}` (`W:descriptor.go:498`). It has no `ViewFormats` and no frame latency.
- `type SurfaceTexture struct` (`W:surface_native.go:709`) has two methods:
  - `func (st *SurfaceTexture) CreateView(desc *TextureViewDescriptor) (*TextureView, error)` (:746)
  - `func (st *SurfaceTexture) AsTexture() *Texture` (:732), a borrowed wrapper you must not release
- **Errors from acquire and present** are `ErrSurfaceLost`, `ErrSurfaceOutdated`, `ErrTimeout` and `ErrDeviceLost`, re-exported from the HAL (`W:error.go:13-19`).

### Buffer, mapping

`type Buffer struct` (`W:buffer.go:29`).

| Signature | Where |
|---|---|
| `func (b *Buffer) Size() uint64` | `W:buffer.go:41` |
| `func (b *Buffer) Usage() gputypes.BufferUsage` | `W:buffer.go:44` |
| `func (b *Buffer) Label() string` | `W:buffer.go:47` |
| `func (b *Buffer) MapState() MapState` | `W:buffer.go:85` |
| `func (b *Buffer) Map(ctx context.Context, mode MapMode, offset, size uint64) error` | `W:buffer.go:111` |
| `func (b *Buffer) MapAsync(mode MapMode, offset, size uint64) (*MapPending, error)` | `W:buffer.go:164` |
| `func (b *Buffer) MappedRange(offset, size uint64) (*MappedRange, error)` | `W:buffer.go:193` |
| `func (b *Buffer) Unmap() error` | `W:buffer.go:231` |
| `func (b *Buffer) Release()` | `W:buffer.go:60` |

- **`MapPending`** (`W:map_pending.go:27`): `Status() (ready bool, err error)` (:69), `Wait(ctx context.Context) error` (:90) and `Release()` (:124). It is pooled, and documented as not safe to share between goroutines without synchronisation (`W:map_pending.go:24-25`).
- **`MappedRange`** (`W:mapped_range.go:38`): `Bytes() []byte` (:64), `BytesMut() []byte` (:83), `Flush() error` (:89), `Len() int` (:94), `Offset() uint64` (:102) and `Release()` (:115).
  - `Unmap` invalidates every range through a generation counter, after which `Bytes()` returns nil.
  - Ranges may not overlap (`W:buffer.go:184-191`).
  - The offset must be 8-aligned and the size 4-aligned (`W:buffer.go:197`).
- **Enums in `wgpu`, not gputypes** (`W:map_types.go`):
  - `MapMode uint32`: `MapModeRead=1`, `MapModeWrite=2` (:22).
  - `MapState uint8`: `Unmapped`, `Pending`, `Mapped`, `Destroyed` (:46).
  - `PollType uint8`: `PollPoll`, `PollWait` (:77).
- **Mapping errors** (`W:map_types.go:95` onwards): `ErrMapAlreadyPending`, `ErrMapAlreadyMapped`, `ErrMapNotMapped`, `ErrMapRangeOverlap`, `ErrMapRangeDetached`, `ErrMapAlignment`, `ErrMapCanceled`, `ErrBufferDestroyed`, `ErrMapDeviceLost`, `ErrMapInvalidMode`, `ErrMapRangeOverflow`.
- **There is no `Buffer.Destroy`.** The `Destroyed` map state exists, but only `Release` is exported.

### Texture, TextureView, Sampler, ShaderModule, layouts, bind groups, pipelines

| Type | Methods | Where |
|---|---|---|
| `Texture` | `Format() gputypes.TextureFormat`, `Release()` | `W:texture_native.go:12`, `:51`, `:56` |
| `TextureView` | `Texture() *Texture`, `Release()` | `W:texture_native.go:96`, `:123`, `:135` |
| `Sampler` | `Release()` | `W:sampler_native.go:8`, `:16` |
| `ShaderModule` | `Release()` | `W:shader_native.go:11`, `:84` |
| `BindGroupLayout` | `Release()` | `W:bind_native.go:26`, `:96` |
| `PipelineLayout` | `Release()` | `W:bind_native.go:121`, `:135` |
| `BindGroup` | `Release()` | `W:bind_native.go:172`, `:210` |
| `RenderPipeline` | `Release()` | `W:pipeline_native.go:60`, `:100` |
| `ComputePipeline` | `Release()` | `W:pipeline_native.go:112`, `:138` |
| `Fence` | `Release()` | `W:fence.go:12`, `:20` |

- **`Texture` has no size, usage, dimension, mip-count or sample-count getters**, only `Format()`. A texture view is created with `Device.CreateTextureView(texture, desc)`, not `Texture.CreateView`.
- **Release is deferred.**
  - Buffer, BindGroup and the pipelines drop a reference count; the HAL object is destroyed when the last in-flight submission that cloned it completes (`W:buffer.go:50-79`, `W:pipeline_native.go:100`).
  - Texture, TextureView, Sampler, ShaderModule and the layouts are queued on a destroy queue keyed by submission index (`W:texture_native.go:56`, `W:sampler_native.go:16-30`).
- **Buffer and BindGroup also release themselves on GC,** through `runtime.AddCleanup` (`W:buffer.go:269-278`, `W:bind_native.go:223-227`). A `*Buffer` that nothing references is freed.
- Also exported: `NewTextureFromHAL`, `NewTextureViewFromHAL`, `NewSamplerFromHAL` (`W:wrap.go:79-89`) and the bookkeeping types `LateBufferBindingInfo` (`W:bind_native.go:164`) and `LateSizedBufferGroup` (`W:pipeline_native.go:19`).

### CommandEncoder, CommandBuffer

`type CommandEncoder struct` (`W:encoder_native.go:20`) is "NOT thread-safe". It is single-use: after `Finish` it cannot be used again.

| Signature | Where |
|---|---|
| `func (e *CommandEncoder) BeginRenderPass(desc *RenderPassDescriptor) (*RenderPassEncoder, error)` | `W:encoder_native.go:311` |
| `func (e *CommandEncoder) BeginComputePass(desc *ComputePassDescriptor) (*ComputePassEncoder, error)` | `W:encoder_native.go:333` |
| `func (e *CommandEncoder) CopyBufferToBuffer(src *Buffer, srcOffset uint64, dst *Buffer, dstOffset uint64, size uint64)` | `W:encoder_native.go:352` |
| `func (e *CommandEncoder) CopyBufferToTexture(src *Buffer, dst *Texture, regions []BufferTextureCopy)` | `W:encoder_native.go:531` |
| `func (e *CommandEncoder) CopyTextureToBuffer(src *Texture, dst *Buffer, regions []BufferTextureCopy)` | `W:encoder_native.go:391` |
| `func (e *CommandEncoder) CopyTextureToTexture(src, dst *Texture, regions []TextureCopy)` | `W:encoder_native.go:444` |
| `func (e *CommandEncoder) ClearBuffer(buffer *Buffer, offset, size uint64)` | `W:encoder_native.go:622` |
| `func (e *CommandEncoder) TransitionTextures(barriers []TextureBarrier)` | `W:encoder_native.go:496` (does work on Vulkan; a no-op on Metal, GLES and software) |
| `func (e *CommandEncoder) DiscardEncoding()` | `W:encoder_native.go:636` |
| `func (e *CommandEncoder) Finish() (*CommandBuffer, error)` | `W:encoder_native.go:676` |
| `func (cb *CommandBuffer) Release()` | `W:encoder_native.go:839`, for a buffer that will not be submitted |

**Copies return nothing.** Their validation errors are recorded with `setError` and returned by `Finish()` (`W:encoder_native.go:56-63`). The copy methods take a source and destination object plus a region slice, not WebGPU's `GPUTexelCopyTextureInfo` pairs.

### RenderPassEncoder, ComputePassEncoder

Both are "NOT thread-safe" (`W:renderpass_native.go:18`, `W:computepass_native.go:20`).

| `RenderPassEncoder` | Where |
|---|---|
| `SetPipeline(pipeline *RenderPipeline)` | `W:renderpass_native.go:68` |
| `SetBindGroup(index uint32, group *BindGroup, offsets []uint32)` | `:90` |
| `SetVertexBuffer(slot uint32, buffer *Buffer, offset uint64)` | `:118`, with no size argument |
| `SetIndexBuffer(buffer *Buffer, format gputypes.IndexFormat, offset uint64)` | `:132`, with no size argument |
| `SetViewport(vp gputypes.Viewport)` | `:145` |
| `SetScissorRect(rect gputypes.ScissorRect)` | `:150` |
| `SetBlendConstant(color *gputypes.Color)` | `:155` |
| `SetStencilReference(reference uint32)` | `:161` |
| `Draw(args gputypes.DrawArgs)` | `:215` |
| `DrawIndexed(args gputypes.DrawIndexedArgs)` | `:223` |
| `DrawIndirect(buffer *Buffer, offset uint64)` | `:244`, which is `MultiDrawIndirect(…, 1)` |
| `MultiDrawIndirect(buffer *Buffer, offset uint64, drawCount uint32)` | `:250`, 16-byte records, not feature-gated |
| `DrawIndexedIndirect(buffer *Buffer, offset uint64)` | `:291` |
| `MultiDrawIndexedIndirect(buffer *Buffer, offset uint64, drawCount uint32)` | `:297`, 20-byte records |
| `End() error` | `:352` |

| `ComputePassEncoder` | Where |
|---|---|
| `SetPipeline(pipeline *ComputePipeline)` | `W:computepass_native.go:58` |
| `SetBindGroup(index uint32, group *BindGroup, offsets []uint32)` | `:76` |
| `Dispatch(x, y, z uint32)` | `:135` |
| `DispatchIndirect(buffer *Buffer, offset uint64)` | `:162`, guarded by a validation shader that clamps oversized counts to zero |
| `End() error` | `:368` |

**Draw-time validation is deferred.** The draw and dispatch errors (`ErrDrawMissingPipeline` and its 18 siblings, `W:error.go:39-122`) are wrapped and surfaced from `Finish()` (`W:error.go:33-38`). The passes have no debug groups or markers, no timestamp writes, no occlusion queries and no `ExecuteBundles`.

## Descriptors

All descriptors live in `wgpu` (`W:descriptor.go`), except the gputypes value structs they embed.

| Descriptor | Fields | Where |
|---|---|---|
| `Extent3D` | `Width, Height, DepthOrArrayLayers uint32` | `W:descriptor.go:11` (`wgpu`'s own, alongside `gputypes.Extent3D`) |
| `Origin3D` | `X, Y, Z uint32` | `:22` |
| `ImageDataLayout` | `Offset uint64; BytesPerRow, RowsPerImage uint32` | `:33` |
| `BufferDescriptor` | `Label string; Size uint64; Usage gputypes.BufferUsage; MappedAtCreation bool` | `:44` |
| `TextureDescriptor` | `Label string; Size Extent3D; MipLevelCount, SampleCount uint32; Dimension gputypes.TextureDimension; Format gputypes.TextureFormat; Usage gputypes.TextureUsage; ViewFormats []gputypes.TextureFormat` | `:62` |
| `TextureViewDescriptor` | `Label string; Format gputypes.TextureFormat; Dimension gputypes.TextureViewDimension; Aspect gputypes.TextureAspect; BaseMipLevel, MipLevelCount, BaseArrayLayer, ArrayLayerCount uint32` | `:88` |
| `SamplerDescriptor` | `Label string; AddressModeU/V/W gputypes.AddressMode; MagFilter, MinFilter, MipmapFilter gputypes.FilterMode; LodMinClamp, LodMaxClamp float32; Compare gputypes.CompareFunction; Anisotropy uint16` | `:114` |
| `ShaderModuleDescriptor` | `Label string; WGSL string; SPIRV []uint32` | `:146` |
| `CommandEncoderDescriptor`, `ComputePassDescriptor` | `Label string` | `:164`, `:486` |
| `BindGroupLayoutDescriptor` | `Label string; Entries []gputypes.BindGroupLayoutEntry` | `:176` |
| `BindGroupDescriptor` | `Label string; Layout *BindGroupLayout; Entries []BindGroupEntry` | `:190` |
| `BindGroupEntry` | `Binding uint32; Buffer *Buffer; Offset, Size uint64; Sampler *Sampler; TextureView *TextureView` (exactly one resource) | `:198` |
| `PipelineLayoutDescriptor` | `Label string; BindGroupLayouts []*BindGroupLayout` (no push-constant ranges) | `:241` |
| `StencilFaceState` | `Compare gputypes.CompareFunction; FailOp, DepthFailOp, PassOp gputypes.StencilOperation` | `:259` |
| `DepthStencilState` | `Format gputypes.TextureFormat; DepthWriteEnabled bool; DepthCompare gputypes.CompareFunction; StencilFront, StencilBack StencilFaceState; StencilReadMask, StencilWriteMask uint32; DepthBias int32; DepthBiasSlopeScale, DepthBiasClamp float32` | `:271` |
| `RenderPipelineDescriptor` | `Label string; Layout *PipelineLayout; Vertex VertexState; Primitive gputypes.PrimitiveState; DepthStencil *DepthStencilState; Multisample gputypes.MultisampleState; Fragment *FragmentState` | `:303` |
| `VertexState` | `Module *ShaderModule; EntryPoint string; Buffers []gputypes.VertexBufferLayout` (no `Constants`) | `:314` |
| `FragmentState` | `Module *ShaderModule; EntryPoint string; Targets []gputypes.ColorTargetState` (no `Constants`) | `:321` |
| `ComputePipelineDescriptor` | `Label string; Layout *PipelineLayout; Module *ShaderModule; EntryPoint string; Constants map[string]float64; ZeroInitializeWorkgroupMemory *bool` | `:360` |
| `RenderPassDescriptor` | `Label string; ColorAttachments []RenderPassColorAttachment; DepthStencilAttachment *RenderPassDepthStencilAttachment` (no timestamp writes, occlusion query set or max draw count) | `:415` |
| `RenderPassColorAttachment` | `View, ResolveTarget *TextureView; LoadOp gputypes.LoadOp; StoreOp gputypes.StoreOp; ClearValue gputypes.Color` (no `DepthSlice`) | `:422` |
| `RenderPassDepthStencilAttachment` | `View *TextureView; DepthLoadOp gputypes.LoadOp; DepthStoreOp gputypes.StoreOp; DepthClearValue float32; DepthReadOnly bool; StencilLoadOp gputypes.LoadOp; StencilStoreOp gputypes.StoreOp; StencilClearValue uint32; StencilReadOnly bool` | `:431` |
| `SurfaceConfiguration` | see Surface | `:498` |
| `ImageCopyTexture` | `Texture *Texture; MipLevel uint32; Origin Origin3D; Aspect gputypes.TextureAspect` | `:520` |
| `TextureCopy` | `Source, Destination ImageCopyTexture; Size Extent3D` | `:542` |
| `BufferTextureCopy` | `BufferLayout ImageDataLayout; TextureBase ImageCopyTexture; Size Extent3D` | `:602` |
| `TextureBarrier` | `Texture *Texture; Range TextureRange; Usage TextureUsageTransition` | `:574` |
| `TextureRange` | `Aspect gputypes.TextureAspect; BaseMipLevel, MipLevelCount, BaseArrayLayer, ArrayLayerCount uint32` | `:563` |
| `TextureUsageTransition` | `OldUsage, NewUsage gputypes.TextureUsage` | `:557` |

**Zero values are not WebGPU's defaults:**

- `MipLevelCount` 0 is rejected (`W:core/validate.go:70`).
- Most gputypes enums give 0 to `Undefined`.
- `Anisotropy` 0 is treated as 1 (`W:core/validate.go:229`).
- `PrimitiveState{}` is the exception: its zero value is the spec default (see below).

## gputypes v0.8.0

**What it is.** "The single source of truth for WebGPU types… ZERO external dependencies" (`T:doc.go:1-5`). It sits under gpucontext, `wgpu` and go-webgpu (`T:doc.go:9-19`). Its only import is `strings` (`T:downlevel.go:3`). It is plain data: enums, bitmasks, structs, `String()` methods, and a few helpers.

### Pointers, strings and slices

These are the gputypes types `wgpu`'s API takes, and what each carries.

| Type | Where | Pointers | Strings | Slices / maps / interfaces |
|---|---|---|---|---|
| `BindGroupLayoutEntry{Binding uint32; Visibility ShaderStages; Buffer *BufferBindingLayout; Sampler *SamplerBindingLayout; Texture *TextureBindingLayout; StorageTexture *StorageTextureBindingLayout; AccelerationStructure *AccelerationStructureBindingLayout}` | `T:binding.go:14` | 5 optional pointers (exactly one set) | – | – |
| `BufferBindingLayout{Type BufferBindingType; HasDynamicOffset bool; MinBindingSize uint64}` | `T:binding.go:33` | – | – | – |
| `SamplerBindingLayout{Type SamplerBindingType}` | `T:binding.go:43` | – | – | – |
| `TextureBindingLayout{SampleType TextureSampleType; ViewDimension TextureViewDimension; Multisampled bool}` | `T:binding.go:49` | – | – | – |
| `StorageTextureBindingLayout{Access StorageTextureAccess; Format TextureFormat; ViewDimension TextureViewDimension}` | `T:binding.go:89` | – | – | – |
| `AccelerationStructureBindingLayout{VertexReturn bool}` | `T:ray_tracing.go:141` | – | – | – |
| `PrimitiveState{Topology PrimitiveTopology; StripIndexFormat *IndexFormat; FrontFace FrontFace; CullMode CullMode; UnclippedDepth bool}` | `T:render.go:371` | `StripIndexFormat` | – | – |
| `MultisampleState{Count uint32; Mask uint64; AlphaToCoverageEnabled bool}` | `T:render.go:398` | – | – | – |
| `ColorTargetState{Format TextureFormat; Blend *BlendState; WriteMask ColorWriteMask}` | `T:render.go:262` | `Blend` (nil means no blending) | – | – |
| `BlendState{Color, Alpha BlendComponent}`, `BlendComponent{SrcFactor, DstFactor BlendFactor; Operation BlendOperation}` | `T:render.go:184`, `:164` | – | – | – |
| `VertexBufferLayout{ArrayStride uint64; StepMode VertexStepMode; Attributes []VertexAttribute}` | `T:vertex.go:211` | – | – | `Attributes` |
| `VertexAttribute{Format VertexFormat; Offset uint64; ShaderLocation uint32}` | `T:vertex.go:201` | – | – | – |
| `Color{R, G, B, A float64}` | `T:color.go:7` | – | – | – |
| `Viewport{X, Y, Width, Height, MinDepth, MaxDepth float32}` | `T:geometry.go:56` | – | – | – |
| `ScissorRect{X, Y, Width, Height uint32}` | `T:geometry.go:75` | – | – | – |
| `DrawArgs{VertexCount, InstanceCount, FirstVertex, FirstInstance uint32}` | `T:draw.go:9` | – | – | – |
| `DrawIndexedArgs{IndexCount, InstanceCount, FirstIndex uint32; BaseVertex int32; FirstInstance uint32}` | `T:draw.go:25` | – | – | – |
| `Limits{…36 uint32/uint64 fields, including MaxPushConstantSize and the ray-tracing counts}` | `T:limits.go:7` | – | – | – |
| `AdapterInfo{Name, Vendor string; VendorID, DeviceID uint32; DeviceType DeviceType; Driver, DriverInfo string; Backend Backend}` | `T:adapter.go:109` | – | `Name`, `Vendor`, `Driver`, `DriverInfo` | – |
| `DownlevelCapabilities{Flags DownlevelFlags; Limits DownlevelLimits; ShaderModel ShaderModel}` | `T:downlevel.go:261` | – | – | – |

gputypes also declares descriptor structs that `wgpu`'s API never takes: `BufferDescriptor`, `TextureDescriptor`, `TextureViewDescriptor`, `SamplerDescriptor`, `ShaderModuleDescriptor`, `BindGroupDescriptor`, `PipelineLayoutDescriptor`, `VertexState`, `FragmentState`, `DepthStencilState`, `StencilFaceState`, `RenderPass*`, `DeviceDescriptor`, `InstanceDescriptor`, `RequestAdapterOptions`, `SurfaceConfiguration`, `SurfaceCapabilities`, `ImageCopyTexture`, `TextureDataLayout` and the ray-tracing descriptors. These hold:

- `uintptr` handles (`T:binding.go:103`, `:130`, `:143`, `:152`, `:163`; `T:render.go:531-545`; `T:vertex.go:223`, `:235`)
- the `BindingResource` and `ShaderSource` interfaces (`T:binding.go:122`, `T:shader.go:77`)
- `map[string]float64` constants (`T:vertex.go:227`, `:239`)
- `[]Feature` (`T:adapter.go:187`)

`wgpu` uses some of them internally to talk to the HAL (`W:descriptor.go:208-238`).

### Enums and bitmasks (all in gputypes)

Values are hex unless shown as iota.

| Type | Values | Where |
|---|---|---|
| `TextureFormat uint32` | Undefined 0, then R8Unorm 0x01 … ASTC12x12UnormSrgb 0x65: 8/16/32-bit R, RG and RGBA in unorm, snorm, uint, sint and float; BGRA8Unorm(Srgb); RGB10A2Uint/Unorm; RG11B10Ufloat; RGB9E5Ufloat; Stencil8; Depth16Unorm; Depth24Plus; Depth24PlusStencil8; Depth32Float; Depth32FloatStencil8; BC1–BC7; ETC2/EAC; ASTC 4x4–12x12 | `T:texture.go:7` |
| `TextureDimension uint32` | Undefined 0, 1D 1, 2D 2, 3D 3 | `T:texture.go:682` |
| `TextureViewDimension uint32` | Undefined 0, 1D, 2D, 2DArray, Cube, CubeArray, 3D (1–6) | `T:texture.go:712` |
| `TextureAspect uint32` | Undefined 0, All 1, StencilOnly 2, DepthOnly 3 | `T:texture.go:754` |
| `TextureUsage uint64` (bitmask) | CopySrc 0x1, CopyDst 0x2, TextureBinding 0x4, StorageBinding 0x8, RenderAttachment 0x10 | `T:texture.go:786` |
| `TextureSampleType uint32` | Undefined 0, Float, UnfilterableFloat, Depth, Sint, Uint (1–5) | `T:texture.go:859` |
| `BufferUsage uint64` (bitmask) | MapRead 0x1, MapWrite 0x2, CopySrc 0x4, CopyDst 0x8, Index 0x10, Vertex 0x20, Uniform 0x40, Storage 0x80, Indirect 0x100, QueryResolve 0x200, then the acceleration-structure bits 0x800–0x4000 | `T:buffer.go:6` |
| `BufferBindingType uint32` | Undefined 0, Uniform, Storage, ReadOnlyStorage | `T:buffer.go:116` |
| `IndexFormat uint32` | Undefined 0, Uint16 1, Uint32 2 | `T:buffer.go:146` |
| `MapMode`, `BufferMapState` | duplicates of the `wgpu` types; `wgpu` uses its own | `T:buffer.go:104`, `:76` |
| `AddressMode uint32` | Undefined 0, ClampToEdge, Repeat, MirrorRepeat | `T:sampler.go:6` |
| `FilterMode uint32` | Undefined 0, Nearest, Linear | `T:sampler.go:38` |
| `MipmapFilterMode uint32` | Undefined 0, Nearest, Linear. `wgpu.SamplerDescriptor.MipmapFilter` is a `FilterMode`, not this type | `T:sampler.go:64` |
| `CompareFunction uint32` | Undefined 0, Never, Less, Equal, LessEqual, Greater, NotEqual, GreaterEqual, Always (1–8) | `T:sampler.go:92` |
| `SamplerBindingType uint32` | Undefined 0, Filtering, NonFiltering, Comparison | `T:sampler.go:209` |
| `StorageTextureAccess uint32` | Undefined 0, WriteOnly, ReadOnly, ReadWrite | `T:binding.go:59` |
| `ShaderStage uint32` (bitmask; `ShaderStages = ShaderStage`) | None 0, Vertex 1, Fragment 2, Compute 4 | `T:shader.go:6`, `:20` |
| `LoadOp uint32`, `StoreOp uint32` | Undefined 0, Load/Store 1, Clear/Discard 2 | `T:render.go:4`, `:30` |
| `BlendFactor uint32` | Undefined 0, Zero, One, Src, OneMinusSrc, SrcAlpha, OneMinusSrcAlpha, Dst, OneMinusDst, DstAlpha, OneMinusDstAlpha, SrcAlphaSaturated, Constant, OneMinusConstant (1–0xD) | `T:render.go:56` |
| `BlendOperation uint32` | Undefined 0, Add, Subtract, ReverseSubtract, Min, Max | `T:render.go:126` |
| `ColorWriteMask uint32` | None 0, Red 1, Green 2, Blue 4, Alpha 8, All 0xF | `T:render.go:244` |
| `PrimitiveTopology uint32` | **TriangleList 0**, PointList 1, LineList 2, LineStrip 3, TriangleStrip 4 | `T:render.go:281` |
| `FrontFace uint32` | **CCW 0**, CW 1 | `T:render.go:319` |
| `CullMode uint32` | **None 0**, Front 1, Back 2 | `T:render.go:345` |
| `StencilOperation uint32` | Undefined 0, Keep 1, Zero, Replace, Invert, IncrementClamp, DecrementClamp, IncrementWrap, DecrementWrap (to 8) | `T:render.go:417` |
| `VertexFormat uint32` | Undefined 0, Uint8x2 0x01 … Unorm1010102 0x1F: 8, 16 and 32-bit in uint, sint, unorm, snorm and float, x2/x3/x4. There are no single-component 8 or 16-bit formats | `T:vertex.go:7` |
| `VertexStepMode uint32` | Undefined 0, VertexBufferNotUsed 1, Vertex 2, Instance 3 | `T:vertex.go:171` |
| `PresentMode uint32` | Undefined 0, Fifo 1, FifoRelaxed 2, Immediate 3, Mailbox 4 | `T:surface.go:8` |
| `CompositeAlphaMode uint32` | Auto 0, Opaque, Premultiplied, Unpremultiplied, Inherit | `T:surface.go:52` |
| `SurfaceStatus uint32` | Good, Suboptimal, Timeout, Outdated, Lost, Unknown (iota) | `T:surface.go:136` |
| `Feature uint64` (single bit), `Features uint64` (mask; `Contains`, `ContainsAll`, `Insert`, `Remove`, `Intersect`, `Union`, `IsEmpty`, `Count`) | DepthClipControl, Depth32FloatStencil8, TextureCompressionBC/ETC2/ASTC, IndirectFirstInstance, ShaderF16, RG11B10UfloatRenderable, BGRA8UnormStorage, Float32Filterable, TimestampQuery, PipelineStatisticsQuery, MultiDrawIndirect, MultiDrawIndirectCount, PushConstants, TextureAdapterSpecificFormatFeatures, ShaderFloat64, VertexAttribute64bit, SubgroupOperations, SubgroupBarrier, RayQuery, RayHitVertexReturn, ExtendedASVertexFormats, ASBindingArray, RayTracingPipelines (`1 << iota`) | `T:features.go:7`, `:124-168` |
| `DownlevelFlags uint32`, `ShaderModel uint32` | wgpu-rs downlevel flags (ComputeShaders … LinearInterpolation); Sm2/Sm4/Sm5 | `T:downlevel.go:24`, `:228` |
| `Backend uint8`, `Backends uint8` | Empty, Vulkan, Metal, DX12, GL, BrowserWebGPU; masks, with `BackendsPrimary` = Vulkan, Metal, DX12 and BrowserWebGPU | `T:adapter.go:38`, `:76`, `:93` |
| `DeviceType`, `PowerPreference`, `MemoryHints`, `Dx12ShaderCompiler`, `GLBackend` (uint8) | iota enums | `T:adapter.go:4`, `:129`, `:165`, `:208`, `:230` |
| `InstanceFlags uint8` | None 0, Debug, Validation, GPUBasedValidation, DiscardHalLabels (`1 << iota` from iota 1, so Debug = 2) | `T:adapter.go:252` |
| ray tracing | `AccelerationStructure*` enums and flags | `T:ray_tracing.go` |

- **Helpers:**
  - `DefaultLimits()` (`T:limits.go:99`, the WebGPU defaults) and `DownlevelDefaultLimits()` (`:140`)
  - `DefaultInstanceDescriptor()`, `DefaultDeviceDescriptor()`, `DefaultPrimitiveState()`, `DefaultMultisampleState()`, `DefaultStencilFaceState()`, `DefaultDepthStencilState(format)`, `DefaultSamplerDescriptor()` and `LinearSamplerDescriptor()`
  - `BlendStateReplace`, `BlendStateAlpha` and `BlendStatePremultiplied`
  - format queries: `TextureFormat.IsDepthStencil`, `HasDepth`, `HasStencil`, `IsSrgb`, `BlockCopySize`
  - `VertexFormat.Size()`, `IndexFormat.Size()`
- **`wgpu` re-exports a subset as untyped aliases** (`W:types.go:15-104`, `W:descriptor.go:247-256`): backends, buffer and texture usages, three dimensions, six "common" formats, shader stages, present modes, power preferences and stencil operations.
- `MinBindGroups = 4` and `MaxBindGroups = 8` are `wgpu` constants (`W:types.go:8-12`).

## Async calls and callbacks

| Operation | Shape | Where |
|---|---|---|
| Adapter, device | synchronous, returning `(T, error)` | `W:instance_native.go:57`, `W:adapter_native.go:46` |
| Pipeline creation | synchronous only; no `Create*PipelineAsync` | `W:device_native.go:518`, `:637` |
| Buffer map, blocking | `Map(ctx, mode, offset, size) error`. It polls once, then runs `Device.Poll(PollWait)` on a new goroutine and waits on ctx | `W:buffer.go:111-158` |
| Buffer map, non-blocking | `MapAsync` returns `*MapPending`. It resolves inside `Device.Poll`, which `Queue.Submit` calls at its tail. Read it with `Status()` or `Wait(ctx)` | `W:buffer.go:164-186`, `W:map_pending.go:69-124` |
| Submitted work done | no callback. `Submit` returns an index; `Queue.Poll()` returns the last completed index; `Device.Poll(PollWait)` and `WaitIdle()` block | `W:queue_native.go:50`, `:252`, `W:device_native.go:947`, `:1093` |
| Fences | `CreateFence`/`WaitForFence(f, value, timeout)`/`GetFenceStatus`, a non-WebGPU extension | `W:device_native.go:841-918` |
| Error scopes | `PushErrorScope(filter)` and `PopErrorScope() *GPUError`, both synchronous. On native the stack is never fed (see below) | `W:device_native.go:936-945` |
| Device lost | no callback and no promise. Loss shows up as `ErrDeviceLost` from calls that reach the driver: Vulkan fence waits, acquire, present (`W:hal/vulkan/device.go:130`, `W:hal/vulkan/swapchain.go:1106`) and maps (`ErrMapDeviceLost`) | `W:error.go:14` |
| Uncaptured errors | no handler | – |
| Callbacks that do exist | `Surface.SetPrepareFrame(func() (w, h uint32, changed bool))` before each acquire; `SetLogger(*slog.Logger)` | `W:surface_native.go:421`, `W:logger.go:19` |

**Native error scopes are inert.** `core.ErrorScopeManager.ReportError` and `Device.reportError` exist (`W:core/error_scope.go:143`, `:220`), but nothing in the module calls either outside tests. So every native `PopErrorScope` returns nil, and real errors arrive only as Go errors: from `Create*`, from `Finish`/`End` (deferred), and from `Submit`. Lazy creation of the scope manager is "not thread-safe… on a brand-new Device" (`W:core/error_scope.go:224-236`).

`GPUError{Type ErrorFilter; Message string}` (`W:core/error_scope.go:50`). `ErrorFilter int`: `Validation=0`, `OutOfMemory=1`, `Internal=2` (`W:core/error_scope.go:14-29`).

## Goroutine-safety

| Object | Documented | What the code does |
|---|---|---|
| package `wgpu` | "Instance, Adapter, and Device are safe for concurrent use. Encoders… are NOT thread-safe." (`W:doc.go:31-34`) | – |
| Instance | safe except `Release` (`W:instance_native.go:24-25`) | `sync.Mutex` (`:28`) |
| Adapter | covered by the package doc | read-only after creation; `released` is a plain bool (`W:adapter_native.go:28`) |
| Device | safe except `Release` (`W:device_native.go:17-21`) | atomic `released`; the encoder pool has a mutex (`W:encoder_pool.go:24`); the core trackers lock |
| Queue | safe; `Submit`, `WriteBuffer` and `WriteTexture` are serialised (`W:queue_native.go:14-19`) | `sync.Mutex` (`:22`); `LastSubmissionIndex` is atomic |
| Surface | nothing documented | state transitions locked in core (`W:core/resource.go:1688`); the wrapper's fields are unguarded (`W:surface_native.go:44-74`) |
| Buffer | `MapState` is "a synchronized snapshot" (`W:buffer.go:80-84`) | atomic `released`; the map state machine is locked in core |
| MapPending, MappedRange | not safe to share without sync; `Bytes()` is safe with itself, not with `Unmap` (`W:map_pending.go:24`, `W:mapped_range.go:31-62`) | pooled |
| BindGroup | – | atomic `released` (`W:bind_native.go:180`) |
| Texture, TextureView, Sampler, ShaderModule, BindGroupLayout, PipelineLayout, pipelines, Fence | – | plain `released bool`, so `Release` races with concurrent use (`W:texture_native.go:16`, `:100`; `W:sampler_native.go:11`; `W:shader_native.go:14`; `W:bind_native.go:29`, `:124`; `W:pipeline_native.go:63`, `:115`; `W:fence.go:15`) |
| CommandEncoder, RenderPassEncoder, ComputePassEncoder | NOT thread-safe (`W:encoder_native.go:19`, `W:renderpass_native.go:18`, `W:computepass_native.go:20`) | – |

No test in package `wgpu` records several encoders from different goroutines at once. The concurrency tests under `W:core/thread_safety_test.go` cover leak tracking, error scopes and instance and adapter creation.

## The `-tags rust` build

The build uses go-webgpu/webgpu v0.5.5, which binds wgpu-native v29.0.0.0 through goffi with no cgo (`R:README.md:15`, `:45`). The Rust build's files are the `*_rust.go` set, plus the untagged `types.go`, `doc.go`, `gpucontext_helpers.go`, `indirect.go` and `surface_target.go`.

**Exported-API differences from native**, found by comparing the signature lists of the two builds:

- **The compute pipeline descriptor loses two fields.** `ComputePipelineDescriptor` is `{Label; Layout; Module; EntryPoint}`, without `Constants` or `ZeroInitializeWorkgroupMemory` (`W:descriptor_rust.go:223-228`). Code that sets them does not compile under `-tags rust`.
- **Some types become `wgpu`'s own.**
  - `StencilOperation` is its own type, numbered `Keep=0 … DecrementWrap=7` (`W:descriptor_rust.go:103-114`). `StencilFaceState.FailOp` and its siblings use it, so `gputypes.StencilOperation*` values do not compile there. The `wgpu.StencilOperation*` aliases do.
  - `ErrorFilter int` and `GPUError` are `wgpu`'s own types (`W:error_rust.go:80-100`), not aliases into `core`.
- **Some HAL types become `any`.** `HalDevice() any` (`W:device_rust.go:440`, always nil) and `SetPrepareFrame(_ any)` (`W:surface_rust.go:206`, a no-op). `Surface.HAL()` is absent.
- **One method is extra:** `SurfaceTexture.Texture() *Texture` (`W:surface_rust.go:281`).

**Behaviour differences:**

- **Required features are dropped.** `RequestDevice` passes only `Label` and a non-zero `RequiredLimits` (`W:adapter_rust.go:53-63`).
- **Submission tracking does not exist.** `Queue.Poll()` returns 0 ("wgpu-native does not expose poll on queue", `W:queue_rust.go:46-50`), and so does `LastSubmissionIndex()` (`W:queue_rust.go:103-107`). `Device.Poll(PollWait)` and `WaitIdle` call `wgpuDevicePoll` (`W:device_rust.go:419-435`).
- **Error scopes are real but miswired.** `PopErrorScope` blocks on wgpu-native's `PopErrorScope` future (`W:device_rust.go:401-416`, `R:wgpu/errors.go:125`). But `PushErrorScope` casts `wgpu.ErrorFilter` (0, 1, 2) straight to go-webgpu's `ErrorFilter`, where Validation is 1, OutOfMemory 2 and Internal 3 (`W:device_rust.go:393-398`, `R:wgpu/enums.go:422-431`). Validation becomes 0, which is invalid; OutOfMemory becomes Validation.
- **Stencil operations are off by one.** `convertDepthStencilState` casts `wgpu.StencilOperation` (Keep=0) to gputypes's (Keep=1) (`W:device_rust.go:597-608`, `R:wgpu/gputypes_aliases.go:53`), so each operation becomes the one before it.
- **Validation errors follow wgpu-native's model.** go-webgpu's `Create*` fails only on a null handle (`R:wgpu/buffer.go:136-137`). wgpu-native, like webgpu.h, reports validation failures to error scopes or the uncaptured-error callback, and gogpu's Rust wrapper sets neither callback (`W:adapter_rust.go:65`). So `Create*` errors that native returns as Go errors may go unseen here.
- **The emulated and no-op calls:**
  - `MultiDrawIndirect` and `MultiDrawIndexedIndirect` loop over `DrawIndirect` (`W:renderpass_rust.go:99-116`).
  - `TransitionTextures` (`W:encoder_rust.go:147-149`), fences (`W:fence_rust.go:6`, `W:device_rust.go:336-390`), `SetSwapchainSuppressed`, `DiscardTexture`, `SetPresentsWithTransaction` and damage rects are no-ops.
  - `ReadPixels`, `PresentPixels` and `WritePixels` return errors (`W:surface_rust.go:178-223`).
  - `DownlevelCapabilities()` returns the defaults (`W:adapter_rust.go:42-43`).
- **The threading story is narrower.** go-webgpu's package doc lists only a few safe concurrent operations: init, `RequestAdapter`, `RequestDevice`, `PopErrorScopeAsync`, `MapAsync`, and the read-only `Limits`, `Info` and `Features` queries. Its "general rule" is that "a single object should not be accessed concurrently" (`R:wgpu/doc.go:6-27`). gogpu's Rust `Device` and `Queue` hold a plain `released bool` and no lock (`W:device_rust.go:23`, `W:queue_rust.go:15`). gogpu still documents Device as concurrent-safe (`W:doc.go:33`).
- **go-webgpu has more than gogpu exposes.** It offers `CreateQuerySet`, `CreateRenderBundleEncoder`, `Queue.GetTimestampPeriod`, and device-lost and uncaptured-error callback infos (`R:wgpu/device.go:216-297`). gogpu's Rust wrapper exposes none of them, because it mirrors the native surface.

## Differences from the WebGPU spec

Items marked "extension" are present in gogpu/wgpu and absent from the spec. The rest are spec features gogpu lacks or changes.

- **Objects that are missing:**
  - `GPUQuerySet`: no timestamp or occlusion queries, no `resolveQuerySet`, no pass `timestampWrites`
  - `GPURenderBundle`/`GPURenderBundleEncoder` and `executeBundles`
  - `GPUExternalTexture`
  - `GPUCompilationInfo`/`getCompilationInfo`
- **Instance, adapter and device:**
  - `requestAdapter`/`requestDevice` are synchronous.
  - There is no `GPUDevice.lost` promise, no `onuncapturederror`, no `destroy()` (only `Release`), no adapter `isFallbackAdapter` and no `wgslLanguageFeatures`.
  - Features are a `uint64` bitmask of wgpu-rs features, not a set of strings.
  - Limits add `MaxPushConstantSize`, `MaxNonSamplerBindings` and the ray-tracing counts.
  - A zero `RequiredLimits` gives the adapter's limits, not the defaults.
- **Pipelines:**
  - There is no `"auto"` layout and no `getBindGroupLayout` (both exist on the browser build only, `W:pipeline_browser.go`).
  - There are no async creation variants.
  - Override constants exist only on compute; the vertex and fragment stages have no `constants`.
  - Shader input can be SPIR-V (extension).
- **Resources:**
  - There is no `Buffer.destroy`/`Texture.destroy`; `Release` is a reference drop.
  - Mapping (extension in shape):
    - `Map(ctx)` blocks, and `MapAsync` returns a pollable handle instead of a promise.
    - `getMappedRange` becomes a `MappedRange` object that `Unmap` invalidates.
  - `Texture` exposes only `Format()`, not `width`, `height`, `depthOrArrayLayers`, `mipLevelCount`, `sampleCount`, `dimension` or `usage`.
  - Views come from `Device.CreateTextureView(texture, desc)`.
- **Encoding:**
  - There are no debug groups or markers.
  - `CopyTextureToTexture` and the buffer/texture copies take region slices.
  - `ClearBuffer` exists.
  - `TransitionTextures` is an extension (explicit barriers, Vulkan).
  - `DiscardEncoding` and `CommandBuffer.Release` are extensions.
- **Render pass:**
  - Draw arguments are structs (`DrawArgs`, `DrawIndexedArgs`), `SetViewport` and `SetScissorRect` take structs, and `SetBlendConstant` takes a pointer.
  - `SetVertexBuffer`/`SetIndexBuffer` take no size.
  - `MultiDrawIndirect`/`MultiDrawIndexedIndirect` are extensions.
  - Color attachments have no `depthSlice`.
  - The pass descriptor has no `occlusionQuerySet`, `timestampWrites` or `maxDrawCount`.
- **Compute pass:** `dispatchWorkgroups` is named `Dispatch`, and `dispatchWorkgroupsIndirect` is named `DispatchIndirect`.
- **Queue:**
  - There is no `onSubmittedWorkDone` and no `copyExternalImageToTexture`.
  - `Submit` returns an index (extension), and `Poll` and `SetSwapchainSuppressed` are extensions.
- **Device extensions:** `Poll`, `WaitIdle`, fences, `HalDevice`, `NewDeviceFromHAL`.
- **Surface (spec `GPUCanvasContext`):**
  - The extensions are `PresentWithDamage`, `PresentPixels`, `WritePixels`, `ReadPixels`, `SetPrepareFrame`, `SetPresentsWithTransaction`, `ActualExtent` and `DiscardTexture`.
  - Acquire returns a suboptimal flag and errors.
  - There is no `viewFormats` in the configuration.
- **Enum encoding:**
  - Most enums follow webgpu.h numbering, with `Undefined = 0`.
  - `PrimitiveTopology`, `FrontFace` and `CullMode` are renumbered so that 0 is the spec default (`T:render.go:271-354`).
  - `PresentMode`, `CompositeAlphaMode`, `Backend` and the downlevel flags come from wgpu-rs or webgpu.h, not the JS spec.

## What the map's notes say that the source does not bear out

This is a fact check of cog#608's Notes against v0.34.2's public API:

- **"Timestamps at pass boundaries" is not in the public API.** `FeatureTimestampQuery` can be requested, but there is no QuerySet type and no `TimestampWrites` field to use it with. Core drops timestamp writes (`W:core/command.go:1726-1729`).
- **"Multi-draw indirect (with count on Vulkan and DX12)" is half there.** Multi-draw without a count buffer exists on every native backend, and the Rust build emulates it. There is no count variant.
- **"The fallback… has the same API" holds for names, not for every field and value.** See [The `-tags rust` build](#the--tags-rust-build).
- **"Device, adapter and instance are safe to use from many goroutines" matches gogpu's docs for the native build.** go-webgpu's docs are stricter for the Rust build. The resource types that use a plain-bool `Release` are not safe to release while another goroutine records with them.
