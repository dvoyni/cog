# gfx

`github.com/cog-engine/gfx` is Cog's driver-neutral renderer. Gameplay records
high-level draws into an `OpQueue`; gfx rotates queues through a latest-wins
triple buffer, resolves resource-backed shaders and textures, translates to a
`GpuQueue`, and hands that queue to a driver-provided `Backend`.

## Plugin

- Name: `gfx.Name` (`"gfx"`)
- Constructor: `gfx.New() *gfx.Plugin`
- Plugin dependency: `storage`
- Go package dependencies: `app`, `kernel`, `storage`, `x/image`

The plugin has no configuration. Register `storage` before it so shader and
texture resources are available at runtime.

`Plugin` implements the kernel lifecycle methods `Name`, `Dependencies`, and
`Init`.

## Resources

- `*OpQueue`: writable, frame-local high-level draw queue.
- `*ResourceQueue`: durable GPU resource operations; unlike frame queues these
  are retained until the render thread consumes them.
- `*Viewport`: logical, window, and framebuffer dimensions.

`OpQueue` methods are `Pass`, `SetPass`, `TemporaryTarget`, `Draw`,
`DrawInstanced`, `DrawInstancedFrom`, `Len`, and `Reset`. Draw parameters
override same-named material parameters. `DrawInstancedFrom` starts at a given
`firstInstance`: WebGPU's `instance_index` starts there, so a batch reads its
own slice of a shared instance arena without plumbing an offset of its own.

## Passes

A recorder declares a pass with `q.Pass(PassDescr{...})`, which also selects it:
every op recorded afterwards belongs to it, and `q.SetPass(ref)` re-selects one
declared earlier in the frame. A recorder that declares none gets an implicit
default pass over the whole screen with automatic depth, which is what `Clear`
and `ClearDepth` then apply to.

`PassDescr` carries `Order`, a `Target` (`ScreenTarget()`,
`TextureTarget(tex, mip, layer)`, or `NoTarget()`), a `Depth` (`DepthAuto()`,
`DepthNone()`, or `DepthTarget(tex)`), `Load`/`Clear`/`Store`,
`DepthLoad`/`DepthClear`/`DepthStore`, and a `Label`.

- **Passes run in `Order`**, ties broken by declaration sequence — never stream
  order, since separate recorders declare passes from separate update
  subscriptions. `Order` is a shared space with no reserved ranges.
- **Adjacent passes merge** when the successor is by definition
  indistinguishable from continuing the predecessor: same attachments, the
  successor preserving both and the predecessor keeping both. Canvas's pass per
  layer therefore costs one GPU pass.
- **A pass runs iff it has an effect**: it draws something, or an attachment
  loads. "Clear this target and nothing else" is a legitimate frame, and so is a
  camera that culled everything.
- **`Order` is the only intra-frame read-after-write guarantee.** gfx builds no
  dependency graph; the whole frame is one command encoder and one submit, so
  the driver inserts the barriers. Sampling a texture the same pass renders into
  is rejected with `ErrDrawSamplesAttachment` and the draw is dropped.
- **`DepthAuto` is shared per target size.** Every `DepthAuto` pass at one size
  uses one depth texture, so a pass that needs clean depth must clear it or it
  inherits what the previous pass left there.
- There is no per-pass viewport or scissor, no multiple render targets, and no
  MSAA.

`ResourceQueue` methods:

- `Ready() bool`
- `BakeBuffer`, `ReBakeBuffer`, `ReleaseBuffer`
- `BakeTexture`, `ReBakeTexture`, `AllocateTexture`, `UpdateTexture`,
  `ReleaseTexture`

Methods accepting `copyData` snapshot bytes when true. When false, the caller
must keep the source unchanged until the render thread consumes the operation.
Explicit resources returned by `Bake*` are caller-owned and must be released.

## Commands Implemented

| Command | Request / response | Declared resource access |
| --- | --- | --- |
| `PresentCmd` | `PresentRequest` / `PresentResponse` | write `*OpQueue` and internal ready queue |
| `AcquireCmd` | `AcquireRequest` / `AcquireResponse{Advanced}` | write internal read and ready queues |
| `SetBackendCmd` | `SetBackendRequest{Backend}` / `SetBackendResponse` | write all frame queues and `*ResourceQueue` |
| `ReleaseCachedResourceCmd` | `ReleaseCachedResourceRequest{Path}` / `ReleaseCachedResourceResponse` | write `*ResourceQueue` |
| `FreeCachedResourcesCmd` | `FreeCachedResourcesRequest` / `FreeCachedResourcesResponse` | write `*ResourceQueue` |
| `SetViewportCmd` | `SetViewportRequest` with window/framebuffer dimensions / `SetViewportResponse{Viewport}` | read desired policy, write `*Viewport` |
| `SetDesiredViewportCmd` | `SetDesiredViewportRequest{Mode, Width, Height, Size}` / `SetDesiredViewportResponse{Viewport}` | write desired policy and `*Viewport` |

`PresentCmd` and `AcquireCmd` are public for explicit queue control, but normal
operation uses the update and render subscriptions. Cache-release commands
affect translator-owned path resources; they do not release explicit
`ResourceQueue.Bake*` resources.

## Events

### Declared

`WindowSizeChangeEvent{Width, Height}` reports logical-window size changes.
`wgpu.Plugin` publishes it synchronously before updating the viewport. Gfx does
not subscribe to this event itself.

### Subscribed

- `UpdateEventHandler` handles `app.UpdateEvent`, writes `*OpQueue` plus the
  ready queue, and runs `Last()` to present the completed frame queue.
- `RenderEventHandler` handles `app.RenderEvent`, writes the read queue, ready
  queue, and `*ResourceQueue`, reads `storage.FileSystem`, then translates and
  executes the latest queue on the driver's render thread.

## Viewport

`Viewport` exposes logical `Width`/`Height`, DIP `WindowWidth`/`WindowHeight`,
and physical `FramebufferWidth`/`FramebufferHeight`.

`ViewportMode` values:

- `ViewportWindow`: logical size follows the window.
- `ViewportFixedWidth` and `ViewportFixedHeight`: keep `Size` fixed.
- `ViewportFit`: show all of the desired `Width` by `Height` rectangle.
- `ViewportCover`: fill from the desired rectangle.

## Draw Descriptors

- `BufferDescr`: build inline data with `BufferWithBytes`; durable storage
  buffers come from `ResourceQueue.BakeBuffer`.
- `TextureDescr`: build with `TextureWithResource` or `TextureWithBytes`, or use
  `ResourceQueue`; inspect with `ID()` and `Path()`. Both constructors take a
  `TextureFormat`, which says whether the texels are light or a gamma-encoded
  picker value; the same path in two formats is two textures.
- `ShaderDescr`: build with `ShaderWithResource` or `ShaderWithText`.
- `MeshDescr`: build with `Mesh` or `MeshIndexed` from buffer descriptors,
  topology, and `VertexAttr` values created by `Attr`.
- `MaterialDescr`: build with `Material` or `MaterialWithState`; `Clone` and
  `CloneTo` snapshot parameter descriptors.
- `ParameterDescr`: build with `FloatParam`, `VecParam`, `MatParam`,
  `ColorParam`, `TextureParam`, `SamplerParam`, `BufferParam`, or
  `BufferRangeParam`. Accessors are `Name`, `FloatValue`, `ColorValue`,
  `TextureValue`, and `SamplerValue`.

`BufferRangeParam(name, buf, offset, size)` binds one slice of a buffer, which
is how a draw addresses its own record in a shared arena: the binding is the
addressing, so no index has to be agreed on between the recording thread and the
render thread. Storage offsets are 256-aligned (`gfx.StorageAlignment`), so a
record pads up to a multiple of it — a pad, not a cap on what it may hold.

`gfx.DefaultLimits` is the WebGPU spec floor: 4 bind groups, 8 storage buffers
per shader stage, a 128 MiB storage binding, a 64 KiB uniform binding, and a
256 MiB buffer. Every shader gfx reflects is checked against it, and never
against the device's own limits — a desktop adapter reports hardware numbers, so
checking those passes a build that cannot run in a browser. The device's limits
appear in the message instead.

`MaterialState` contains `Blend`, `DepthCompare`, `DepthWrite`, `Cull`, and
`FrontFace`. Its zero value is both the WebGPU default and what the backend
always did: alpha over, `CompareAlways`, no depth write, `CullNone`, `FrontCCW`.
The named states are `StateOpaque3D`, `StateTransparent3D`, and
`StateOverlay2D` (the zero value). `CompareFunc` is `CompareAlways`,
`CompareNever`, `CompareLess`, `CompareLessEqual`, `CompareGreater`,
`CompareGreaterEqual`, `CompareEqual`, or `CompareNotEqual`. `CullMode` is
`CullNone`, `CullFront`, or `CullBack`; `FrontFace` is `FrontCCW` or `FrontCW`.
`PrimitiveTopology` is
`TopologyTriangleList`, `TopologyTriangleStrip`, or `TopologyLineList`.
`BlendMode` is `BlendAlpha`, `BlendOpaque`, `BlendAdditive`, or
`BlendMultiply`. `AddressMode` is `AddressClamp`, `AddressRepeat`, or
`AddressMirror`, chosen per axis. `FilterMode` is `FilterLinear` or
`FilterNearest`.

`SamplerParam(name, SamplerDesc)` takes the descriptor whole: per-axis
`AddressU`/`AddressV`, separate `Mag`/`Min`/`Mip` filters, `Anisotropy` (0 and 1
mean off, clamped to 16, and rejected unless all three filters are linear), and
`Comparison` plus `Compare` for a shadow-style comparison sampler. The zero
value clamps and filters linearly. Every sampler a shader declares binds
independently by name, so a material's textures can sample differently.

`VertexType` formats are `UnknownVertexType`, `Float32`, `Float32x2`,
`Float32x3`, `Float32x4`, `Float16x2`, `Float16x4`, `Uint8x2`, `Uint8x4`,
`Sint8x2`, `Sint8x4`, `Unorm8x2`, `Unorm8x4`, `Snorm8x2`, `Snorm8x4`,
`Uint16x2`, `Uint16x4`, `Sint16x2`, `Sint16x4`, `Unorm16x2`, `Unorm16x4`,
`Snorm16x2`, `Snorm16x4`, `Uint32`, `Uint32x2`, `Uint32x3`, `Uint32x4`,
`Sint32`, `Sint32x2`, `Sint32x3`, `Sint32x4`, and `Unorm1010102`.

## Backend API

`Backend` is implemented by a system driver. It reserves logical texture and
buffer IDs, creates and frees samplers/shaders/pipelines, reflects
`ShaderLayout`, reports the current screen framebuffer, and executes a
translated queue. Its methods are:

```go
type Backend interface {
    NewTexture() TextureID
    NewBuffer() BufferID
    NewSampler(SamplerDesc) (SamplerID, error)
    FreeSampler(SamplerID)
    NewShader(ShaderDesc) (ShaderID, error)
    FreeShader(ShaderID)
    ShaderLayout(ShaderID) ShaderLayout
    NewPipeline(PipelineDesc) (PipelineID, error)
    FreePipeline(PipelineID)
    ScreenFramebuffer() (TextureViewID, int, int)
    TextureView(TextureID, mip, layer int) TextureViewID
    Execute(*GpuQueue)
}
```

Low-level descriptors are `TextureDesc`, `BufferDesc`, `SamplerDesc`,
`ShaderDesc`, `PipelineDesc`, `ShaderLayout`, `UniformMember`,
`ShaderResource`, `StorageMember`, `VertexAttribute`, and `Region`. Reflection
reports member layout for storage structs too — a one-level walk in which an
array member carries its element stride and count — so a recorder that declares
no uniform block at all packs its records from the same source of truth. A
shader declaring two uniform blocks is an error rather than a silent overwrite.
Enums include
`TextureFormat` (`FormatRGBA8`, `FormatRGBA8Srgb`, `FormatDepth32F`, and the
`FormatScreen` sentinel that `Resolve()` turns into `FormatRGBA8Srgb`),
`BufferKind` (`BufferVertex`, `BufferIndex`, `BufferUniform`, `BufferStorage`),
and `TextureViewDimension` (`TextureView2D`, `TextureView2DArray`).

`TextureDesc.Renderable` asks for a texture a render pass can draw into as well
as sample. Mip generation filters in the format's own colour space — decoding
and re-encoding around the box filter for `FormatRGBA8Srgb` — and is refused for
depth.

Opaque handles are based on `ResourceID`: `TextureID`, `BufferID`, `SamplerID`,
`ShaderID`, `PipelineID`, and `TextureViewID`. Zero means no resource.

`GpuQueue` records through `BakeBuffer`, `BakeTexture`, `AllocateTexture`,
`UpdateTexture`, `BeginPass`, `EndPass`, `SetPipeline`, `SetParams`,
`SetTexture`, `SetSampler`, `SetVertexBuffer`, `SetIndexBuffer`, `SetBuffer`,
`Draw`, `ReleaseBuffer`, and `ReleaseTexture`. `ReplayBakes(GpuBakeSink)`,
`ReplayPasses(GpuPassSink)`, and `ReplayReleases(GpuReleaseSink)` send each
phase to a backend; `Reset` reuses the queue. Bakes are hoisted ahead of every
pass, so a pass can read anything the frame uploaded. The sink interfaces define
the backend-facing replay contracts, and `BeginPass` returns the `RenderPass`
its commands go to, so the backend owns encoder and pass lifetime.

`BufferSourceBytes`, `BufferSourceBaked`, `ShaderSourceText`,
`ShaderSourceResource`, `TextureSourceResource`, `TextureSourceBytes`, and
`TextureSourceBaked` are exported source-marker constants; normal callers use
descriptor constructors instead.

## Math

Shared value types and operations live in the root `m` package. Graphics APIs
use `m.Vec*`, `m.Rect`, `m.Color`, and column-major `m.Mat4` directly.

## Errors

`ErrShaderNotFound{Name}` is reported through the kernel when a resource-backed
shader cannot be loaded. Its `Error() string` method implements `error`.
