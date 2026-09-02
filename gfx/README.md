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

`OpQueue` methods are `Clear`, `ClearDepth`, `Draw`, `DrawInstanced`, `Len`, and
`Reset`. Draw parameters override same-named material parameters.

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
  `ResourceQueue`; inspect with `ID()` and `Path()`.
- `ShaderDescr`: build with `ShaderWithResource` or `ShaderWithText`.
- `MeshDescr`: build with `Mesh` or `MeshIndexed` from buffer descriptors,
  topology, and `VertexAttr` values created by `Attr`.
- `MaterialDescr`: build with `Material` or `MaterialWithState`; `Clone` and
  `CloneTo` snapshot parameter descriptors.
- `ParameterDescr`: build with `FloatParam`, `VecParam`, `MatParam`,
  `ColorParam`, `TextureParam`, `SamplerParam`, or `BufferParam`. Accessors are
  `Name`, `FloatValue`, `ColorValue`, `TextureValue`, and `SamplerValue`.

`MaterialState` contains `Blend` and `DepthTest`. `PrimitiveTopology` is
`TopologyTriangleList`, `TopologyTriangleStrip`, or `TopologyLineList`.
`BlendMode` is `BlendAlpha`, `BlendOpaque`, `BlendAdditive`, or
`BlendMultiply`. `AddressMode` is `AddressClamp`, `AddressRepeatX`,
`AddressRepeatY`, or `AddressRepeat`. `FilterMode` is `FilterLinear` or
`FilterNearest`.

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
    Execute(TextureViewID, *GpuQueue)
}
```

Low-level descriptors are `TextureDesc`, `BufferDesc`, `SamplerDesc`,
`ShaderDesc`, `PipelineDesc`, `ShaderLayout`, `UniformMember`,
`ShaderResource`, `VertexAttribute`, and `Region`. Enums include
`TextureFormat` (`FormatRGBA8`), `BufferKind` (`BufferVertex`, `BufferIndex`,
`BufferUniform`, `BufferStorage`), and `TextureViewDimension`
(`TextureView2D`, `TextureView2DArray`).

Opaque handles are based on `ResourceID`: `TextureID`, `BufferID`, `SamplerID`,
`ShaderID`, `PipelineID`, and `TextureViewID`. Zero means no resource.

`GpuQueue` records through `BakeBuffer`, `BakeTexture`, `AllocateTexture`,
`UpdateTexture`, `Clear`, `ClearDepth`, `SetPipeline`, `SetParams`, `SetTexture`,
`SetVertexBuffer`, `SetIndexBuffer`, `SetBuffer`, `Draw`, `ReleaseBuffer`, and
`ReleaseTexture`. `Clears`, `ClearColor`, and `ClearDepthValue` inspect clear
state. `ReplayBakes(GpuBakeSink)`, `ReplayRenderPass(RenderPass)`, and
`ReplayReleases(GpuReleaseSink)` send each phase to a backend; `Reset` reuses the
queue. The sink interfaces define the backend-facing replay contracts.

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
