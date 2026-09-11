# gfx

`github.com/cog-engine/gfx` is Cog's driver-neutral renderer. Gameplay records
high-level draws into an `OpQueue`; gfx rotates queues through a latest-wins
triple buffer, resolves resource-backed shaders and textures, translates to a
`GpuQueue`, and hands that queue to a driver-provided `Backend`.

[`docs/specs/preprocessor.md`](docs/specs/preprocessor.md) is the design record
for the WGSL shader preprocessor — the `#include` / `#define` / `#const` / `#if`
language shader sources are written in, and what each rule is and why. It is
implemented behind `FlattenShader`, which `ensureShader` calls on a cache miss so
that every backend receives flattened source and none of them knows the
preprocessor exists; nothing else in this README describes it.

## Plugin

- Name: `gfx.Name` (`"gfx"`)
- Constructor: `gfx.New() *gfx.Plugin`
- Plugin dependency: `storage`
- Go package dependencies: `app`, `kernel`, `mcp`, `storage`, `x/image`
- Implements: `mcp.Provider`, `kernel.PluginStopper`

The plugin has no configuration. Register `storage` before it so shader and
texture resources are available at runtime.

`Plugin` implements the kernel lifecycle methods `Name`, `Dependencies`,
`Init`, and `Stop`. `Stop` exists for one job: completing a capture the engine
walked away from.

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
declared earlier in the frame. There is no implicit default pass: a draw
recorded outside every pass is dropped and reported as `ErrDrawWithoutPass`.

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
- `BakeTexture`, `ReBakeTexture`, `AllocateTexture`, `AllocateRenderTarget`,
  `UpdateTexture`, `ReleaseTexture`

`AllocateTexture` produces a texture to sample; `AllocateRenderTarget` produces
one a pass can also render into, through `TextureTarget`. They are two methods
because the render-attachment usage is not free, and almost every texture is
sampled-only. `AllocateRenderTarget` is the durable counterpart of
`OpQueue.TemporaryTarget`: take it when the rendered contents must outlive the
frame, and `TemporaryTarget` when they need not.

Methods accepting `copyData` snapshot bytes when true. When false, the caller
must keep the source unchanged until the render thread consumes the operation.
Explicit resources returned by `Bake*` are caller-owned and must be released.

## The frame buffer and the present pass

`ScreenTarget()` does not mean the swapchain. It means a frame-sized colour
buffer in `gfx.FrameBufferFormat` that the backend allocates on first use, and
gfx appends one implicit full-screen **present pass** after every declared pass
to put it on the surface. A frame that renders only into its own textures never
allocates the buffer and never presents.

The engine has no choice about owning that buffer: gogpu hardcodes
`BGRA8Unorm` for the surface and exposes no view formats, and
`bgra8unorm-srgb` is not a legal canvas-context format on the web, so a
hardware sRGB swapchain is unreachable. The cost is one frame-sized texture
(~8 MiB at 1080p) and one full-screen pass, which is also where
post-processing hangs later.

`FrameBufferFormat` is `FormatRGBA8` today, and the present pass passes the
buffer through unchanged. Recorders still write gamma-encoded values, so that
pair is the one combination that leaves the frame bit-for-bit what it was
before the frame buffer existed; the present pass reads the same constant to
decide whether it applies the sRGB OETF, so flipping the constant flips both
halves of the decision together.

## Reading A Frame Back

A **capture** is one rendered colour target taken off the GPU and handed to
whoever asked for it. It is a gfx feature rather than an agent one: anything
holding a kernel handle may arm one, and the agent-facing capability below is
one caller among them.

`ArmCaptureCmd` takes an `ArmCaptureRequest{Target, Amount, Interval, Paused}`
and answers with `ArmCaptureResponse{Done, Viewport}`. `Done` is a buffered
channel carrying one `GpuCapture` per still; `GpuCapture` carries either the
mapped bytes or the reason there are none, so a caller cannot handle a result
and forget a failure. `GpuCapture.Image()` un-strides the padded rows into an
`image.NRGBA` — straight-alpha, because `image.RGBA` is premultiplied and
`FormatRGBA8` is not.

`Target` is a `GpuCaptureDesc{Screen bool, Texture TextureID}`, mirroring
`GpuPassDesc`'s addressing. A capture always reads mip 0, layer 0. Depth is
refused, and so is any format that is not 8-bit RGBA.

**The moment a capture names.** The still binds to a tick that *began* after
the request, so a caller that sends input and then captures cannot get the
frame recorded before that input was consumed. Mechanically the request moves
through four stages, one still at a time: `pending` waits for a tick to begin,
`armed` waits for it to complete, `bound` rides the next render whatever queue
that render draws, and `inflight` waits for the readback. That costs one extra
tick and makes press-then-capture correct with no composition mechanism at all.

**Nothing waits.** The copy is encoded into the frame's own encoder after the
present, and the map is started right after that frame's single submit; the
*next* frame's submit is what resolves it, and `Backend.TakeCapture` is a
status check gfx drains once per frame. With nothing outstanding the whole
per-frame cost is one length check. One frame of latency is therefore a fixed
property of every capture, and a capture needs a frame to be *submitted* before
it can resolve — which is why a paused engine keeps drawing.

**Under pause.** `ArmCaptureRequest.Paused` says the caller knows no further
tick can begin, so the still binds straight to the next render and costs no
tick. gfx does not read the tick source itself: pausing belongs to whichever
host owns the loop, and gfx must not require a host to exist. Two captures
taken under one pause are byte-identical, and a burst while paused is refused.

**Bursts.** `Amount` stills, `Interval` ticks apart, capped at 60 stills and
600 ticks of span. Each still binds on its own terms, so a burst spans many
moments on purpose. A burst truncates rather than failing: the caller sees the
stills that landed.

**One at a time.** A second arm while one is live is `ErrCaptureBusy`, refused
rather than queued or coalesced. The backend refuses a second in-flight map the
same way, through `GpuCapture.Err`, because a game's own code may arm one.
Shutdown completes a live capture with `ErrCaptureAbandoned` on the channel a
result would have used.

`Renderable` now implies copy-source. You can only read back what something
rendered into, so `TextureDesc.Renderable` already names exactly the capturable
set and no new flag has to predict it. The cost, stated so nobody finds it in a
profile: on some drivers copy-source disables lossless framebuffer compression
on that texture, and it is paid whether or not a capture ever happens.

## Offered To An Agent

gfx implements `mcp.Provider` and offers one capability, `capture`, rendered as
the tool `gfx_capture`. It is screen-only: `GpuCaptureDesc` addresses any
colour texture and that generality is right for gfx, but nothing lists textures
to an agent and a `TextureID` is an opaque handle it has no way to obtain.

The agent names an absolute `.png` path, optionally with `amount`, `interval`
and a `%d` numbering verb; every check — the path, the extension, the verb and
the caps — happens before a frame is spent, so a burst is refused whole or
armed whole. The response carries the ordinals actually written plus the image
size in pixels and the window size in the units input capabilities use, which
together convert a point in the picture into a point that can be clicked. The
capability is `mcp.ReadOnly()`: it writes exactly the file it was told to.

The full contract is in [docs/specs/capture.md](docs/specs/capture.md) and
[docs/specs/mcp.md](docs/specs/mcp.md). `gfx_frame`, the other capability those
documents specify, is not implemented yet.

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
| `ArmCaptureCmd` | `ArmCaptureRequest{Target, Amount, Interval, Paused}` / `ArmCaptureResponse{Done, Viewport}` | read `*Viewport` |

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

- `CaptureUpdateEventHandler` handles `app.UpdateEvent` and runs `First()`,
  ahead of every other subscriber, so a capture armed while a tick is already
  running waits for the next one. It declares no resources: the capture slot is
  plugin-owned and carries its own lock, because shutdown has to complete a
  waiting capture and a stopped scheduler grants none.
- `UpdateEventHandler` handles `app.UpdateEvent`, writes `*OpQueue` plus the
  ready queue, and runs `Last()` to present the completed frame queue. A
  capture armed before this tick began binds here, beside the queue swap.
- `RenderEventHandler` handles `app.RenderEvent`, writes the read queue, ready
  queue, and `*ResourceQueue`, reads `storage.FileSystem`, then translates and
  executes the latest queue on the driver's render thread. It drains
  `Backend.TakeCapture` immediately after `Execute`.

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
  `CloneTo` snapshot parameter descriptors. `Fingerprint()` hashes everything
  that makes one material different from another, and `FingerprintParams` does
  the same for a bare parameter slice — which is how a recorder keys a batch on
  what a draw carries without writing a type switch that silently mis-keys the
  kind it forgot.
- `ParameterDescr`: build with `FloatParam`, `VecParam`, `MatParam`,
  `ColorParam`, `TextureParam`, `SamplerParam`, `BufferParam`,
  `BufferRangeParam`, or `RawParameter`. Accessors are `Name`, `FloatValue`,
  `ColorValue`, `TextureValue`, `SamplerValue`, `VecValue`, `HasValue`, and
  `AppendValue`.

`HasValue` separates a value a shader reads out of its uniform block from a
binding it attaches to a bind group, and `AppendValue(dst)` appends the value's
bytes in the layout the shader reads them at. Together they let a recorder pack
a parameter it did not construct.

`RawParameter[T](name, value)` carries an arbitrary plain-data struct by copying
its bytes, so a per-instance parameter can be a record rather than a scalar. It
**validates `T`'s layout against WGSL's alignment rules once per type and panics
on a mismatch**, naming the field, both offsets, and the padding that would fix
it. A size check alone is not enough and fails in a way that looks like it works:

```go
type bad struct { A float32; B m.Vec3 }   // Go: 16 bytes. WGSL: 32.
```

Go aligns a float32-based struct to 4; WGSL aligns `vec2` to 8 and `vec3`,
`vec4` and matrices to 16, so `bad` passes `size % 16 == 0` and every element
after the first reads the wrong memory. Members may be `float32`, `int32`,
`uint32`, `m.Vec2`, `m.Vec3`, `m.Vec4`, `m.Color`, `m.Quat`, `m.Mat4`, arrays of
those, and structs of those; anything else panics, which is what keeps a pointer
out of a byte copy.

**A parameter whose kind cannot fill the binding its name matched is rejected.**
Parameters resolve by name, and one name has one frequency: a value is a uniform
member and a buffer is a storage binding. Supplying either where the shader
declared the other used to bind the wrong descriptor into the slot and draw
garbage with no diagnostic — and a missing storage binding is worse, because
`CreateBindGroup` rejects the short list and the draw encodes with no bindings at
all. It is now `ErrParameterKindMismatch` and the draw is dropped. The check
lives in plan construction, which is cached per `(shader, parameter shape)`, so
it costs nothing per draw. A name that matched no binding is not a mismatch:
gfx drops a parameter no shader declared, which is ordinary.


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
`ShaderLayout`, reports the surface it presents to, and executes a
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
    TakeCapture() (GpuCapture, bool)
}
```

`TakeCapture` is drained once per frame, immediately after `Execute`, and
never blocks: what it has ready is the copy the *previous* frame encoded,
whose map resolved on the submit `Execute` just made.

Low-level descriptors are `TextureDesc`, `BufferDesc`, `SamplerDesc`,
`ShaderDesc`, `PipelineDesc`, `ShaderLayout`, `UniformMember`,
`ShaderResource`, `StorageMember`, `VertexAttribute`, and `Region`. Reflection
reports member layout for storage structs too — a one-level walk in which an
array member carries its element stride and count — so a recorder that declares
no uniform block at all packs its records from the same source of truth. A
shader declaring two uniform blocks is an error rather than a silent overwrite.
Enums include
`TextureFormat` (`FormatRGBA8`, `FormatRGBA8Srgb`, `FormatDepth32F`, and the
`FormatScreen` sentinel that `Resolve()` turns into `FrameBufferFormat`),
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
`Draw`, `Present`, `Capture`, `ReleaseBuffer`, and `ReleaseTexture`. `ReplayBakes(GpuBakeSink)`,
`ReplayPasses(GpuPassSink)`, and `ReplayReleases(GpuReleaseSink)` send each
phase to a backend; `Reset` reuses the queue. Bakes are hoisted ahead of every
pass, so a pass can read anything the frame uploaded. The sink interfaces define
the backend-facing replay contracts, and `BeginPass` returns the `RenderPass`
its commands go to, so the backend owns encoder and pass lifetime. `Present`
takes no arguments: the frame buffer, the full-screen triangle, the transfer
function and the surface format are all the backend's. `Capture` is the same
shape one step further on — a whole-frame action carrying no commands, emitted
last, after the present, because the present pass moves the frame buffer out of
`RenderAttachment` and a copy encoded ahead of it would name a layout that is
no longer true. Its result arrives a frame later through `TakeCapture`.

`TextureUsage` has three values rather than two: `TextureUsageRenderAttachment`,
`TextureUsageTextureBinding`, and `TextureUsageCopySrc` for a texture being
read back. A texture capture declares its own transition into the third role
like any other write-then-read pair; a screen capture declares none, because
the frame buffer is the one attachment gfx never names and the backend places
that barrier itself.

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

`ErrParameterKindMismatch{Shader, Parameter, Supplied, Declared}` is reported
when a parameter's name matches a binding its kind cannot fill, and the draw is
dropped. See the parameter section above for why an unreported one is worse than
a dropped draw.


The capture errors are typed for the same reason: a caller reads them, and a
burst branches on them. `ErrCaptureBusy{}` is a second arm while one is live;
`ErrCaptureAbandoned{}` a capture the engine stopped before its readback
resolved; `ErrCaptureUnsupported{Format}` depth or anything else that is not
8-bit RGBA; `ErrCaptureNoTarget{}` a target the frame never rendered into; and
`ErrCaptureAmount{Amount, Max}`, `ErrCaptureSpan{Ticks, Max}` and
`ErrCaptureBurstPaused{}` the three ways a burst is asked for and refused.
