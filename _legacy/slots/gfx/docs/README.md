# gfx

`github.com/dvoyni/cog/slots/gfx` is Cog's driver-neutral renderer. Gameplay records
high-level draws into an `OpQueue`; gfx rotates queues through a latest-wins
triple buffer, resolves resource-backed textures, translates to a
`gfx.Queue`, and hands that queue to a driver-provided `gfx.Backend`.

gfx is a **Slot**: it declares a required Port, `BackendPort`, and works only
once a `gfx.Backend` **Adapter** fills it. The vocabulary is in
[`CONTEXT.md`](../../../CONTEXT.md) and the decision in
[ADR 0002](../../../docs/adr/0002-slots-extensions-and-bundles-as-declaration-roots.md).

[`specs/preprocessor.md`](specs/preprocessor.md) is the design record
for the WGSL shader preprocessor — the `#include` / `#define` / `#const` / `#if`
language shader sources are written in, and what each rule is and why. It is
internal to gfx: `CompileShaderCmd` flattens a shader before it reflects it, so
that every backend receives flattened source and none of them knows the
preprocessor exists; nothing else in this README describes it.

[`specs/drawparams.md`](specs/drawparams.md) is the design record for how a
draw is shaded: explicit shaders compiled on the CPU, durable sets of draw
params, and the frame's version of a set. The sections below summarise it.

## Packages

gfx has the alias-index root of
[`architecture.instructions.md`](../../../.github/instructions/architecture.instructions.md)
and [ADR 0003](../../../docs/adr/0003-roots-are-alias-indexes.md):

- **`slots/gfx`** is the root, and declares nothing: it aliases what
  `internal/` declares. It is both the recording API and the GPU contract, so each type has one name, `gfx.X`:
  `OpQueue`, `ResourceQueue`, the descriptors, commands (`PresentCmd` and the
  rest), the `Viewport` resource, the view types, `Name` and the ordering
  identities `PresentOnUpdate` and `RenderOnRender` — and beside them the
  backend contract: `Backend` and `BackendPort` in `ports.go`, `Queue` and its
  sinks, `RenderPass`, `Capture`, the shader, pipeline, texture, sampler and
  buffer descriptors, `PipelineLimits`, and every ID, format and enum in `types.go`.
  Recorders (canvas, scene, ui and games) import it to draw, and an
  Adapter's backend (gogpu's `gfx*.go` files, cog-examples' headless `Backend`)
  imports it and nothing else of gfx.
- **`slots/gfx/internal`** is the plugin, and declares everything the root
  aliases: the recording types whose unexported state the translator reads,
  their recording methods and the consume side of the queues, the GPU
  vocabulary they carry, the view types, and the shader preprocessor, beside
  `New`, its handlers, the translator, the capture and frame-snapshot slots, and
  the mcp Provider with its two capabilities. It never imports the root.
- **`slots/gfx/gfxplugin`** exports only `New`. Only composition roots and
  tests import it.

A recording type whose insides the plugin reads (`OpQueue`, `ResourceQueue`)
is declared in `internal/` with its fields unexported, and aliased in the root (`type OpQueue = internal.OpQueue`), each constructor
behind a forwarder in `utils.go`. It stays a concrete type, and its exported
methods are public API through the alias. The GPU vocabulary is aliased the
same way, and the exported variables it used to have are functions:
`gfx.DefaultLimits()`, `gfx.StateOpaque3D()`, `gfx.StateTransparent3D()` and
`gfx.StateOverlay2D()`.

The descriptors are different: a user builds them, so they are declared in
`internal/types` with public fields and no accessors. The constructors are the
usual way to build one and fill every field a descriptor's kind needs, but a
descriptor written by hand is as good as one they build, and it is the writer's
job to keep its fields consistent.

The root's one piece of code outside its forwarders is `inlineAnchor` in
`types.go`, an unexported function nothing calls. Go inlines a method of a
package the caller does not import only when a package it does import
references that method, and nothing outside gfx can import `internal/`, so
the anchor references the accessors importers call per instance —
`ShaderParameterDescr.Name` and its value accessors, `TextureDescr.ID` and `Size`,
`TextureFormat.Resolve` — and canvas, scene and gogpu
inline them. Add an accessor to it when a hot importer stops inlining one; the
tier test allows exactly that shape.

## Plugin

- Name: `gfx.Name` (`"gfx"`)
- Constructor: `gfxplugin.New() kernel.Plugin`
- Plugin dependencies: `app`, `storage`
- Requires: exactly one Adapter for `gfx.BackendPort`
- Go package dependencies: `app`, `kernel`, `mcp`, `storage`, `x/image`
- Contributes: one `mcp.Provider` Adapter
- Implements: `kernel.PluginStopper`

The plugin has no configuration. Register `storage` before it so shader and
texture resources are available at runtime.

**The Backend Adapter.** The plugin calls
`registrar.RequireAdapter[gfx.BackendPort]()`, the Port declared in `ports.go`
on `gfx.Backend`, and reads the handle from `Start` onwards. A driver declares
an Adapter type for it, as gogpu's `GfxBackend kernel.Adapter[gfx.BackendPort]`,
and provides its backend with
`registrar.ProvideAdapter[GfxBackend](gfx.Backend(backend))` during its own `Register`; a
composition with no provider fails with `kernel.ErrMissingAdapter`, and one
with two fails with `kernel.ErrDuplicateAdapter`. No command installs a
backend.

A driver whose GPU device arrives later (gogpu's is created asynchronously
inside the render loop) provides a stable value at `Register` and attaches the
device to it once the device exists. Until then `Backend.Ready` is false.
`ResourceQueue.Ready` asks the same question, and a frame rendered before the
backend is ready is skipped: nothing is translated or executed, and
`ErrBackendNotReady` is reported once. The recording queues reserve ids
through `NewTexture` and `NewBuffer` at any time, which a backend must answer
without a device.

`Plugin` implements the kernel lifecycle methods `Name`, `Dependencies`,
`Register`, and `Stop`. `Stop` exists for one job: completing a capture the
engine walked away from.

## Resources

- `*OpQueue`: writable, frame-local high-level draw queue.
- `*ResourceQueue`: durable GPU resource operations; unlike frame queues these
  are retained until the render thread consumes them.
- `*Viewport`: logical, window, and framebuffer dimensions.

`OpQueue` methods are `NewPass`, `NewTemporaryBuffer`, `NewTemporaryTexture`,
`NewTemporaryTarget`, `SetDrawParams`, and `Draw`; the plugin resets a queue
itself when it rotates the frame slots. `Draw(pass, mesh, set, instances,
firstInstance)` is the one draw: a plain draw passes `1, 0`, and fewer than one
instance still draws once. `firstInstance` is where WebGPU's `instance_index`
starts, so a batch reads its own slice of a shared instance arena without
plumbing an offset of its own.

## Shaders and draw params

gfx has no materials. A draw names a **set of draw params**: one shader, one
fixed `DrawState`, and a value for some or all of the shader's bindings,
created once and named by the comparable `DrawStateId`, which is
also the complete batch key a recorder needs.

- **A shader is compiled on the CPU and created once.** `CompileShaderCmd`
  takes `CompileShaderRequest{FS, Descr}`, flattens the descriptor, reflects it
  through the Backend's `ReflectShader` and answers with a `ShaderProgram` or
  the `Err` it failed with - the one failure that is a normal outcome, and never
  also reported. Its lock is empty, so a System declaring
  `ecs.Uses[CompileShaderCmd, ...]` widens its lock set by nothing; a library
  takes the plain `gfx.ShaderCompiler` func instead.
  `ResourceQueue.NewShader()` reserves an id at once,
  `UploadProgram(k, id, program)` gives it its one program, and
  `ReleaseShader(k, id)` frees the module and every pipeline built on it.
  Reloading is the caller's: release, compile and create again.
- **A set is resolved once.** `ResourceQueue.NewDrawParams(k, shader, state,
  params...)` resolves each param against the shader's program by binding name
  when it is called, so the translator never looks a name up.
  `UpdateDrawParams(k, set, params...)` changes the set's own values and
  persists; `ReleaseDrawParams(k, set)` frees it and the resources it baked. A
  durable set names durable ids only - a temporary is refused - and inline bytes
  a param carries are baked into ids the set owns.
- **A param is a whole binding.** It names a WGSL global and supplies all of
  it: bytes of the binding's reflected size for a uniform (`ShaderParameterFloat`,
  `ShaderParameterVec4`, `ShaderParameterMat4`, `ShaderParameterColor`, `ShaderParameterRaw`, `ShaderParameterRawRef`), a
  texture, a sampler, or a buffer and a range of it. There are no member-level
  writes: to change one field of a struct, resubmit the struct.
- **The frame's version.** `OpQueue.SetDrawParams(k, set, params...)` changes a
  set for the rest of this frame, from this point on, and the next frame starts
  from the set's own values again. A draw sees the values the set had when it
  was recorded, however the passes are ordered: a change makes a version,
  copy-on-write per binding, and a version no draw has captured yet is patched
  in place. Temporaries belong here, a per-frame instance arena above all.

A binding neither the set nor its version supplies takes a default when the
draw is resolved - zero bytes, the clamp-and-linear sampler, white - except a
storage buffer, whose draw is dropped and reported once. Every set method that
can find a mistake takes the `kernel.Kernel` it reports through, once, and
ignores the bad param; `Draw` takes none. A draw naming a set, shader or mesh
released since the frame was recorded is dropped silently: it is a frame
rendered again after what it names was let go.

## Passes

A recorder declares a pass with `ref := q.NewPass(PassDescr{...})`, and every
draw names the pass it belongs to: `q.Draw(ref, mesh, ...)`. Nothing is selected,
so a recorder may interleave draws into several passes, and a System may draw
into a pass another System declared by being handed its `PassId`, which is
valid until the frame ends. There is no implicit default pass: a draw naming no
pass declared this frame is dropped and reported as `ErrDrawWithoutPass`.

`PassDescr` carries `Order`, a `Target` (`TargetDescrScreen()`,
`TargetDescrTexture(tex, mip, layer)`, or `TargetDescrNone()`), a `Depth` (`DepthDescrAuto()`,
`DepthDescrNone()`, or `DepthDescrTarget(tex)`), `Load`/`Clear`/`Store`,
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
- **`DepthDescrAuto` is shared per target size.** Every `DepthDescrAuto` pass at one size
  uses one depth texture, so a pass that needs clean depth must clear it or it
  inherits what the previous pass left there.
- There is no per-pass viewport or scissor, no multiple render targets, and no
  MSAA.

`ResourceQueue` methods:

- `Ready() bool`
- `NewBuffer`, `NewTexture`, `NewRenderTarget`
- `UploadBuffer`, `UploadTexture`
- `ReleaseBuffer`, `ReleaseTexture`
- `NewShader`, `UploadProgram`, `ReleaseShader`
- `NewDrawParams`, `UpdateDrawParams`, `ReleaseDrawParams`

A resource is made by `New*` and given contents by `Upload*`, so a one-shot
bake is `UploadBuffer(NewBuffer(), data, copyData)`. Uploading again keeps the
id: `UploadBuffer` replaces the whole buffer at any length, and `UploadTexture`
writes one layer over a region, the zero region being the whole layer. A
texture keeps the size and format `NewTexture` gave it; to change them, release
it and make another.

`NewTexture(width, height, layers, format, mipmaps)` produces a texture to
sample. With `mipmaps` it carries a full mip chain, which the backend rebuilds
from every upload that covers a whole layer; a partial upload leaves the smaller
levels as they were. `NewRenderTarget` produces one a pass can also render into,
through `TargetDescrTexture`. They are two methods because the render-attachment
usage is not free, and almost every texture is sampled-only. `NewRenderTarget`
is the durable counterpart of
`OpQueue.NewTemporaryTarget`: take it when the rendered contents must outlive the
frame, and `NewTemporaryTarget` when they need not.

Methods accepting `copyData` snapshot bytes when true. When false, the caller
must keep the source unchanged until the render thread consumes the operation.
Resources returned by `New*` are caller-owned and must be released.

## The frame buffer and the present pass

`TargetDescrScreen()` does not mean the swapchain. It means a frame-sized colour
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
channel carrying one `gfx.Capture` per still; `gfx.Capture` carries either the
mapped bytes or the reason there are none, so a caller cannot handle a result
and forget a failure. `gfx.Capture.Image()` un-strides the padded rows into an
`image.NRGBA` — straight-alpha, because `image.RGBA` is premultiplied and
`FormatRGBA8` is not.

`Target` is a `gfx.CaptureDesc{Screen bool, Texture TextureID}`. A capture always reads mip 0, layer 0. Depth is
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
tick. The arm command does not read the tick source itself; the caller says.
`gfx_capture` finds out by dispatching `app.TimeCmd` with `TimeStatus` and
reading `Paused`, which is why gfx declares `app` as a plugin dependency. Two
captures taken under one pause are byte-identical, and a burst while paused is
refused.

**Bursts.** `Amount` stills, `Interval` ticks apart, capped at 60 stills and
600 ticks of span. Each still binds on its own terms, so a burst spans many
moments on purpose. A burst truncates rather than failing: the caller sees the
stills that landed.

**One at a time.** A second arm while one is live is `gfx.ErrCaptureBusy`, refused
rather than queued or coalesced. The backend refuses a second in-flight map the
same way, through `gfx.Capture.Err`, because a game's own code may arm one.
Shutdown completes a live capture with `gfx.ErrCaptureAbandoned` on the channel a
result would have used.

`Renderable` now implies copy-source. You can only read back what something
rendered into, so `TextureDesc.Renderable` already names exactly the capturable
set and no new flag has to predict it. The cost, stated so nobody finds it in a
profile: on some drivers copy-source disables lossless framebuffer compression
on that texture, and it is paid whether or not a capture ever happens.

## Offered To An Agent

gfx contributes an `mcp.Provider` from `Register` and offers two capabilities, `capture` and
`frame`, rendered as the tools `gfx_capture` and `gfx_frame`. They are the two
halves of one question — what did the frame actually do — and they are separate
tools because "nothing is on screen" and "this looks wrong" are different
sentences. Both are `mcp.ReadOnly()`, which in cog's reading means the
capability does not change the game.

`gfx_capture` is screen-only: `gfx.CaptureDesc` addresses any colour texture and
that generality is right for gfx, but nothing lists textures to an agent and a
`TextureID` is an opaque handle it has no way to obtain.

The agent names an absolute `.png` path, optionally with `amount`, `interval`
and a `%d` numbering verb; every check — the path, the extension, the verb and
the caps — happens before a frame is spent, so a burst is refused whole or
armed whole. The response carries the ordinals actually written plus the image
size in pixels and the window size in the units input capabilities use, which
together convert a point in the picture into a point that can be clicked.

`gfx_frame` is the snapshot: every declared pass in run order with its label,
ordering key, target, depth and clears, a draw and instance count per pass, and
every resource operation from both queues with its handle, size, format and
mipmap flag. Individual draws are counted rather than listed; what they draw
with is listed once per set of draw params and version they name, under
`drawParams`: the shader's label, the Draw state with its enums named, and each
binding's name, kind, group, binding and size or resource, with whether the
value is the set's own, the frame's version's, or the binding's default.
Every index in the response is a **source index** — a position in the queue
that recorded the thing — so an index stays the address of what it named when a
filter is on. `pass` filters by label and says how many passes it dropped;
`path` is optional and writes the JSON to a file instead of returning it
inline.

A snapshot needs a tick where a capture needs a render, which is the one place
the two behave differently: under pause `gfx_frame` performs exactly one step,
or joins one another arm already raised, and says so in its response, while a
capture costs no tick at all.

Every snapshot also **names the tick it describes**. `stepped` and `joined`
were never evidence on their own — two snapshots both reporting a step may be
one tick apart — so the response carries `tick`, taken out of the tick itself
rather than read off the tick source afterwards. Snapshots armed together
either agree on that number or they have split, and the agent can see which.
Making them agree is `app_time hold`'s job; saying whether they did is this
field's.

### Snapshots

The descriptors marshal themselves for the snapshots an agent reads:
`TextureDescr`, `BufferDescr`, `ShaderParameterDescr` and `ShaderDescr` carry a
`MarshalJSON`, `SamplerDesc` and `DrawState` carry json tags, and the enums they
hold marshal as their `String` names. `canvas` and `ui` embed the descriptors in
their own snapshot documents, so one value reaches an agent in one shape
whichever tool showed it. Two rules hold for all of them:

- A tagged union serializes to exactly one value. A parameter that is one float
  carries one number, not the fields another kind would use.
- Bulk bytes never travel. Inline pixels, buffer bytes and raw parameter data
  are reported as a byte count and left where they are.

`SnapshotView` is the header every snapshot response carries: the three
coordinate sizes, the tick the snapshot describes, and the step fields.

The full contract is in [specs/capture.md](specs/capture.md) and
[specs/mcp.md](specs/mcp.md); both capabilities those documents
specify are implemented.

## Commands Implemented

| Command | Request / response | Declared resource access |
| --- | --- | --- |
| `PresentCmd` | `PresentRequest` / `PresentResponse` | write `*OpQueue` and internal ready queue |
| `AcquireCmd` | `AcquireRequest` / `AcquireResponse{Advanced}` | write internal read and ready queues |
| `ReleaseCachedResourceCmd` | `ReleaseCachedResourceRequest{Path}` / `ReleaseCachedResourceResponse` | write `*ResourceQueue` |
| `FreeCachedResourcesCmd` | `FreeCachedResourcesRequest` / `FreeCachedResourcesResponse` | write `*ResourceQueue` |
| `SetViewportCmd` | `SetViewportRequest` with window/framebuffer dimensions / `SetViewportResponse{Viewport}` | read desired policy, write `*Viewport` |
| `SetDesiredViewportCmd` | `SetDesiredViewportRequest{Mode, Width, Height, Size}` / `SetDesiredViewportResponse{Viewport}` | write desired policy and `*Viewport` |
| `ArmCaptureCmd` | `ArmCaptureRequest{Target, Amount, Interval, Paused}` / `ArmCaptureResponse{Done, Viewport}` | read `*Viewport` |
| `ArmFrameCmd` | `ArmFrameRequest{Pass}` / `ArmFrameResponse{Done, Viewport}` | read `*Viewport` |

`PresentCmd` and `AcquireCmd` are public for explicit queue control, but normal
operation uses the update and render subscriptions. Cache-release commands
affect what the translator owns: `ReleaseCachedResourceCmd` the texture a path
names, and `FreeCachedResourcesCmd` every cached texture, pipeline and sampler.
Neither releases what the `ResourceQueue` made - shaders, sets, textures and
buffers are the caller's - and a pipeline is built again by the next draw that
needs it.

## Events

### Declared

`WindowSizeChangeEvent{Width, Height}` reports logical-window size changes.
The gogpu plugin publishes it synchronously before updating the viewport. Gfx does
not subscribe to this event itself.

### Subscribed

The two identities other packages order against are aliased in the root's
`id.go`: `gfx.PresentOnUpdate` and `gfx.RenderOnRender`. Canvas and scene order
their flush `Before[gfx.PresentOnUpdate]()`. The two `First()` identities are
unexported in `internal`, because nothing outside orders against them.

- `captureOnUpdate` handles `app.UpdateEvent` and runs `First()`,
  ahead of every other subscriber, so a capture armed while a tick is already
  running waits for the next one. It declares no resources: the capture slot is
  plugin-owned and carries its own lock, because shutdown has to complete a
  waiting capture and a stopped scheduler grants none.
- `frameOnUpdate` handles `app.UpdateEvent` and runs `First()`, for
  the same reason and with the same absence of declared resources: a frame
  snapshot describes a tick that *began* after the request, so an arm landing
  inside a running tick waits for the next one.
- `PresentOnUpdate` handles `app.UpdateEvent`, writes `*OpQueue` plus the
  ready queue, reads `*ResourceQueue`, and runs `Last()` to present the
  completed frame queue. A capture armed before this tick began binds here,
  beside the queue swap, and the frame snapshot is taken here too, immediately
  before it — the only point at which the frame is both complete (canvas
  orders its flush before this handler) and still alive. The `*ResourceQueue`
  read is the snapshot's: most resource traffic is recorded there and never
  reaches the frame queue.
- `RenderOnRender` handles `app.RenderEvent`, writes the read queue, ready
  queue, and `*ResourceQueue`, reads `storage.FileSystem`, then translates and
  executes the latest queue on the driver's render thread. It drains
  `Backend.TakeCapture` immediately after `Execute`. A frame rendered before
  `Backend.Ready` is skipped.

## Viewport

`Viewport`, `SetViewportCmd` and `SetDesiredViewportCmd` are gfx's: gfx owns
the `*Viewport` resource and handles both commands, so they are offered by its
root rather than by `app`, which declares no Resources.

`Viewport` exposes logical `Width`/`Height`, DIP `WindowWidth`/`WindowHeight`,
and physical `FramebufferWidth`/`FramebufferHeight`.

`ViewportMode` values:

- `ViewportWindow`: logical size follows the window.
- `ViewportFixedWidth` and `ViewportFixedHeight`: keep `Size` fixed.
- `ViewportFit`: show all of the desired `Width` by `Height` rectangle.
- `ViewportCover`: fill from the desired rectangle.

## Draw Descriptors

- `BufferDescr`: build inline data with `BufferWithBytes`; durable storage
  buffers come from `ResourceQueue.NewBuffer`. `Source` says which one it is,
  and `ID`, `Size`, `Bytes` and `CopyData` are fields.
- `TextureDescr`: build with `TextureWithResource` or `TextureWithBytes`, or use
  `ResourceQueue`. `Name` is the resource path, `Blob` the inline pixels, and
  `Params` holds `ID`, `Width`, `Height`, `Layers`, `Format`, `Mipmaps` and
  `CopyData`. Only `TextureWithBytes` takes a
  `gfx.TextureFormat`, which says whether the texels are light or a gamma-encoded
  picker value. `TextureWithResource` hardcodes sRGB and generates no mipmaps,
  because the loader decodes PNG and JPEG and both are gamma-encoded by
  definition - so a path has no second format to be two textures under, and the
  path alone identifies what it names.
- `ShaderDescr`: build with `ShaderWithResource` or `ShaderWithText`; inspect
  with `Path()` and `Supply()`. One path under two supplies is two shaders, so
  the supply is part of the identity rather than a detail beside it. Inline text
  is identified by the run of bytes rather than by its spelling, so a literal, a
  `const` or a package `var` is one module however often it is written down, and
  a string computed afresh per call is a module per call.
- `MeshDescr`: build with `MeshDescrWithVertices` or `MeshDescrWithIndices` from buffer descriptors,
  topology, and `VertexAttribute` values. Its fields are
  `Vertices`, `Indices`, `IndexWidth`, `Topology`, `Layout`, `Stride`,
  `VertexCount` and `IndexCount`; the constructors derive the last three, and a mesh
  written by hand must fill them itself. A mesh is indexed exactly when
  `IndexCount` is above zero.
  `MeshDescrWithIndices` also takes a `gfx.IndexWidth` - `gfx.IndexUint32` (the zero value)
  or `gfx.IndexUint16`, the only two WebGPU has - which describes how the caller wrote
  its index bytes rather than asking gfx to convert them. A buffer whose length
  does not divide by its declared width is reported once and its draw dropped;
  checking that every index is below the vertex count belongs to whoever built
  the geometry.
- `DrawStateId`: the id `ResourceQueue.NewDrawParams` returns, a plain
  `uint32`; comparable, pointer-free and storable, with zero naming no set.
- `FingerprintParams` hashes a bare parameter slice in order by name, kind and
  value — which is how a recorder keys a batch on
  what a draw carries without writing a type switch that silently mis-keys the
  kind it forgot.
- `ShaderParameterDescr`: build with `ShaderParameterFloat`,
  `ShaderParameterVec4`, `ShaderParameterMat4`, `ShaderParameterColor`,
  `ShaderParameterTexture`, `ShaderParameterSampler`, `ShaderParameterBuffer`,
  `ShaderParameterBufferRange`, or `ShaderParameterRaw`. Its one `Kind` says
  which fields carry the value and, for a uniform value, what built it: `raw`,
  `float`, `vec4`, `mat4` and `color` are bytes in `Small`/`SmallLen` or `Raw`
  (`Kind.IsValue()` asks for any of them), and `texture`, `sampler` and
  `buffer` (with `BufferOffset` and `BufferSize`) are bindings. `FloatValue`,
  `ColorValue`, `VecValue` and `MatValue` decode the bytes and return
  `(value, ok)` keyed on the kind, and `Bytes`, `ValueSize` and `AppendValueTo`
  read them wherever they are held.

**`BufferDescr`, `TextureDescr` and `ShaderParameterDescr` are storable**: an ECS
Component may hold one as it stands. Every byte run they carry — a buffer's
inline bytes, a texture's pixels, a raw parameter's layout — is an `assets.Blob`,
which the ECS admits on the contract that nothing writes the bytes after the
descriptor is built. The constructors still take a `[]byte`, and converting is
free; the contract is the caller's to keep, and nothing checks it.

`Kind.IsValue()` separates a value a shader reads out of its uniform block
from a binding it attaches to a bind group, and `AppendValueTo(dst)` appends the value's
bytes in the layout the shader reads them at. Together they let a recorder pack
a parameter it did not construct.

`ShaderParameterRaw[T](name, value)` carries an arbitrary plain-data struct by copying
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

`ShaderParameterRawRef[T](name, *T)` is the same over a value the caller keeps: a
record that fits in 64 bytes is carried inline as `ShaderParameterRaw`'s is, and a
larger one is borrowed rather than copied, so building one per batch allocates
nothing. `NewDrawParams`, `UpdateDrawParams` and `SetDrawParams` copy a
param's bytes before they return, which is what makes the borrow safe; the
value must outlive the call, and the address of a local escapes to the heap.

**A parameter whose kind cannot fill the binding its name matched is rejected.**
Parameters resolve by name, and one name has one kind of binding: a value is a
uniform and a buffer is a storage binding. Supplying either where the shader
declared the other used to bind the wrong descriptor into the slot and draw
garbage with no diagnostic — and a missing storage binding is worse, because
`CreateBindGroup` rejects the short list and the draw encodes with no bindings at
all. It is now `ErrParameterKindMismatch`. The check is made when the param
reaches a set - `NewDrawParams`, `UpdateDrawParams` or `SetDrawParams` - which
reports it through its kernel, once, and ignores the param; so is a param naming
no binding of the shader (`ErrDrawParamUnknown`) and uniform bytes of the wrong
size (`ErrUniformSizeMismatch`). Nothing is checked by name at the draw.

**A binding behaves by kind when neither the set nor the frame's version fills
it, and by shape when one does.** A shader declares three sorts of binding and an unfilled one used to
fail three different ways, only one of them deliberate. Two cases are fatal:
an unfilled storage buffer, and a texture supplied at a dimension its binding
did not declare.

- **Sampler** � falls back to the zero `SamplerDesc`: clamp and linear.
- **Texture** � falls back to a 1x1 opaque white texture, at whichever view
  dimension the binding declares. This one is load-bearing beyond a forgotten
  parameter: a texture resource that has not finished loading, or failed to,
  resolves the same way, so white is what an unresolved texture renders as
  rather than a licence to omit the parameter. The backend keeps two views of
  the one white texel, 2D and 2D-array, because a binding declared
  `texture_2d_array` refuses a 2D view outright � and a refused bind group is
  not white, it is nothing. In WGSL an out-of-range `array_index` is clamped, so
  every layer a shader asks the array white for lands on the one texel.
- **Texture of the wrong shape** � the draw is dropped and
  `ErrTextureViewDimensionMismatch` is reported. This is the one texture case
  with no fallback, and the line it draws is between *not there yet* and *there
  and wrong*: an unfilled or unresolved binding stands for a state, and white is
  the right picture for it, while a single-layer texture supplied where the
  shader declared `texture_2d_array` is an authoring error the caller can fix.
  Substituting white there would hide the mistake instead of showing a state. A
  descriptor that cannot report its layer count is exempt rather than judged:
  only an allocation names one, so a bare baked id says nothing, and refusing a
  correct draw over a descriptor's silence is the worse direction for a fatal
  error. Beneath it the backend still refuses the bind group it cannot build.

  **Rejected: leaving the fallback 2D-only and documenting the exception.**
  `TextureViewDimension` has exactly two values, so the second white is one
  `CreateTextureView` over a texture that already exists � the exception cost
  a paragraph and bought nothing.
- **Storage buffer** � the draw is dropped and `ErrStorageBufferUnsupplied` is
  reported. There is no fallback worth having: nothing is emitted for the
  binding, the group comes up one entry short of its layout, `CreateBindGroup`
  refuses it and the draw encodes with no bindings for that group at all. A
  zero-length dummy would not save it, because the binding is validated against
  the size the shader's own declaration needs.

Every fatal case is reported once per `(shader, parameter)`, and the storage one
covers a binding no parameter names **and** a parameter that names it while
carrying a buffer nothing baked; the message says which. The draw is dropped
every time and the report comes once, because a set that misses a binding
misses it until someone fixes the set, and the frame reports only its first
error � so saying it every frame would mask every later error in every later
frame.

Beneath all of that, the backend reports a bind group the device refused, once
per `(shader, group)`, as `gogpu.ErrBindGroupRefused`. It is the backstop for
the route gfx cannot see: a binding gfx did emit, against a buffer the backend
no longer holds.


`ShaderParameterBufferRange(name, buf, offset, size)` binds one slice of a buffer, which
is how a draw addresses its own record in a shared arena: the binding is the
addressing, so no index has to be agreed on between the recording thread and the
render thread. Storage offsets are 256-aligned (`gfx.StorageAlignment`), so a
record pads up to a multiple of it — a pad, not a cap on what it may hold.

`gfx.DefaultLimits()` is the WebGPU spec floor: 4 bind groups, 8 storage buffers
per shader stage, a 128 MiB storage binding, 12 uniform buffers per shader
stage, a 64 KiB uniform binding, and a
256 MiB buffer. Every shader gfx reflects is checked against it, and never
against the device's own limits — a desktop adapter reports hardware numbers, so
checking those passes a build that cannot run in a browser. The device's limits
appear in the message instead.

The pipeline-state vocabulary below is part of the root like every other gfx
type, and so are `VertexType` and `SamplerDesc`; a recorder names it as `gfx.X`.

`gfx.DrawState` contains `Blend`, `DepthCompare`, `DepthWrite`, `Cull`, and
`FrontFace`, and is fixed on a set: the same surface under another state is a
second set. Its zero value is both the WebGPU default and what the backend
always did: alpha over, `CompareAlways`, no depth write, `CullNone`, `FrontCCW`.
The named states are `StateOpaque3D()`, `StateTransparent3D()`, and
`StateOverlay2D()` (the zero value). `CompareFunc` is `CompareAlways`,
`CompareNever`, `CompareLess`, `CompareLessEqual`, `CompareGreater`,
`CompareGreaterEqual`, `CompareEqual`, or `CompareNotEqual`. `CullMode` is
`CullNone`, `CullFront`, or `CullBack`; `FrontFace` is `FrontCCW` or `FrontCW`.
`PrimitiveTopology` is
`TopologyTriangleList`, `TopologyTriangleStrip`, or `TopologyLineList`.
`BlendMode` is `BlendAlpha`, `BlendOpaque`, `BlendAdditive`, or
`BlendMultiply`. `AddressMode` is `AddressClamp`, `AddressRepeat`, or
`AddressMirror`, chosen per axis. `FilterMode` is `FilterLinear` or
`FilterNearest`.

`ShaderParameterSampler(name, gfx.SamplerDesc)` takes the descriptor whole: per-axis
`AddressU`/`AddressV`, separate `Mag`/`Min`/`Mip` filters, `Anisotropy` (0 and 1
mean off, clamped to 16, and rejected unless all three filters are linear), and
`Comparison` plus `Compare` for a shadow-style comparison sampler. The zero
value clamps and filters linearly. Every sampler a shader declares binds
independently by name, so a set's textures can sample differently.

`VertexType` formats are `UnknownVertexType`, `Float32`, `Float32x2`,
`Float32x3`, `Float32x4`, `Float16x2`, `Float16x4`, `Uint8x2`, `Uint8x4`,
`Sint8x2`, `Sint8x4`, `Unorm8x2`, `Unorm8x4`, `Snorm8x2`, `Snorm8x4`,
`Uint16x2`, `Uint16x4`, `Sint16x2`, `Sint16x4`, `Unorm16x2`, `Unorm16x4`,
`Snorm16x2`, `Snorm16x4`, `Uint32`, `Uint32x2`, `Uint32x3`, `Uint32x4`,
`Sint32`, `Sint32x2`, `Sint32x3`, `Sint32x4`, and `Unorm1010102`.

## The vertex interface

**A shader that reads an `@location` the bound layout does not supply — or
supplies at a different type — is reported and the draw is dropped.** Nothing
else in the stack catches this: gogpu performs no vertex-interface validation of
any kind, the software rasterizer keeps an unsupplied input's zero value, and
WebGPU itself fills the components a format does not supply with `(0, 0, 0, 1)`,
as Vulkan does. So `vec3<f32>` over a two-component oct pair yields `(x, y, 0)`
— a plausible unit-ish direction lying in the XY plane. Not a black screen, not
garbage triangles: wrong shading that looks like art, in an engine with no pixel
readback anywhere.

**The rule is exact, and deliberately stricter than WebGPU.** The format's
`(kind, count)` must *equal* the shader's declared `(kind, count)`, where the
kind is what the hardware decodes to — `VertexScalarFloat`, `VertexScalarUint`
or `VertexScalarSint`, so every normalized format is a float however many bits
it occupies. Presence-only and base-type-compatible were both rejected because
both let the real mismatch through: a two-component unorm decodes to `f32`, and
`vec3<f32>` is `f32`. WebGPU's legal widening and narrowing are forbidden as a
result, and that costs nothing — every `@location` in `bundles/model/internal/builtin` and
`bundles/canvas/internal/builtin` is already an exact match.

**The check is one-directional.** A layout supplying an attribute the shader
does not read is legal and common: scene's bundled vertex struct declares six of
its eight under the no-skin variant. The direction that fails is a shader input
no attribute supplies, never the other way round.

**It also checks the stride.** WebGPU requires `arrayStride` to be a multiple of
4 unconditionally, and the platforms disagree: a 30-byte stride succeeds on
Vulkan, Apple-silicon Metal and D3D12 and fails on `js/wasm`, on GLES and on
older Apple GPUs — green on a Windows dev machine, broken in the browser.

The shader half comes from `ShaderLayout.VertexInputs`, which a backend reflects
alongside the bindings; the mesh half is the `VertexAttribute` list, whose index *is*
the `@location`. `CheckVertexInterface(shader, layout, attrs)` is where they
meet, exported because it is the test surface for a rule that otherwise could
only be exercised through a backend, and because a package that owns both
halves of a pair can ask the question gfx will ask at draw time.

**Failures are loud, drop the draw, and report once.** They go to the fatal
error path rather than the web-limits diagnostic path — a diagnostic says "this
renders here but would not on the web", and a vertex-interface mismatch renders
wrongly everywhere. A failed pipeline is cached as the zero id, so `ok` is true
from the second frame on, the caller drops the draw on the zero id exactly as it
did before, and report-once-drop-always falls out of the cache that already
exists.

## The GPU Contract

The contract a system driver implements is part of the gfx root. An Adapter's
backend imports `slots/gfx` and nothing else of gfx, and every type in it has
one name, `gfx.X`, in code, docs and error messages alike.

> It was a package of its own, `extensions/gfx/gpu`, from #344 until #366 folded
> it back: the split existed only because the root used to carry
> implementation. Every `gpu.X` became `gfx.X`, and `gpu.DefaultLimits`,
> `gpu.StateOpaque3D`, `gpu.StateTransparent3D` and `gpu.StateOverlay2D`
> became functions of the same names.

What it holds:

- **The backend:** `Backend` and `BackendPort` (in `ports.go`), `Queue`,
  `PassSink`, `BakeSink`, `ReleaseSink`, `RenderPass`,
  `CaptureDesc`, `Capture` (with `Image()`), `TextureUsage`
  (`TextureUsageRenderAttachment`, `TextureUsageTextureBinding`,
  `TextureUsageCopySrc`) and `TextureTransition`.
- **Descriptors:** `ShaderDesc`, `ShaderLayout`, `ShaderVertexInput`,
  `StorageMember`, `ShaderResource`, `PipelineDesc`,
  `TextureDesc`, `SamplerDesc`, `Region`,
  `PipelineLimits` and `DefaultLimits()`.
- **IDs:** `ResourceID`, `TextureID`, `BufferID`, `SamplerID`, `ShaderID`,
  `PipelineID`, `TextureViewID`.
- **Formats and enums:** `TextureFormat` with `FrameBufferFormat`,
  `TextureViewDimension`, `AddressMode`, `FilterMode`, `BufferKind`,
  `PrimitiveTopology`, `BlendMode`, `CompareFunc`, `CullMode`, `FrontFace`,
  `DrawState` with `StateOpaque3D()`, `StateTransparent3D()` and
  `StateOverlay2D()`, `VertexType`, `VertexScalar`, `IndexWidth`, `LoadOp`,
  `StoreOp`, and `StorageAlignment`.
- **Errors a backend reports:** `ErrCaptureBusy`, `ErrCaptureAbandoned`,
  `ErrCaptureUnsupported` and `ErrCaptureNoTarget`, beside the burst refusals
  (`ErrCaptureAmount`, `ErrCaptureSpan`, `ErrCaptureBurstPaused`) that validate
  an `ArmCaptureRequest`.
- **Helpers on the enums, as methods:** `TextureFormat.Name()`, and `Name()` on
  `AddressMode`, `FilterMode`, `BlendMode`, `CompareFunc`, `CullMode`,
  `FrontFace`, `BufferKind`, `LoadOp` and `StoreOp`, which spell a value for a
  debug document; `VertexType.Decode()` and `VertexType.Size()`, and
  `VertexScalar.WGSL(count)`, which `gfx.CheckVertexInterface` compares with.

`Backend` reserves logical texture and buffer IDs, creates and frees
samplers/shaders/pipelines, reflects a flattened module into a `ShaderLayout`,
reports the surface it presents to, and executes a translated queue. Its methods are:

```go
type Backend interface {
    NewTexture() TextureID
    NewBuffer() BufferID
    NewSampler(SamplerDesc) (SamplerID, error)
    FreeSampler(SamplerID)
    ReserveShader() ShaderID
    CreateShader(ShaderID, ShaderDesc) error
    FreeShader(ShaderID)
    ReflectShader(code []byte) (ShaderLayout, error)
    NewPipeline(PipelineDesc) (PipelineID, error)
    FreePipeline(PipelineID)
    ScreenFramebuffer() (TextureViewID, int, int)
    TextureFormat(TextureID) (TextureFormat, bool)
    Limits() PipelineLimits
    Execute(*Queue)
    TakeCapture() (Capture, bool)
    Ready() bool
}
```

`Ready` reports whether the backend can render. gfx calls nothing but `Ready`,
`NewTexture`, `NewBuffer`, `ReserveShader` and `ReflectShader` on a backend
that is not ready, and those five must be safe from any goroutine.
`ReflectShader` is the reflection port `CompileShaderCmd` compiles through: it
needs no device, and it is pure, a function of the flattened source alone.
`ReserveShader` reserves the id `ResourceQueue.NewShader` hands out at once,
and `CreateShader` creates that shader's module when its upload is replayed.
A shader's bind groups may skip an index - a scene variant with no group 2
beneath an app's group 3 - and the pipeline layout still holds every index
below the highest, so the backend binds an empty group at each gap: WebGPU
requires every group of the layout set before a draw.

`TextureFormat` reports what format a texture was allocated or baked in, and
whether the backend knows the texture at all. It is what keys a pipeline to
the pass it renders into: a pipeline declares its target's format and
`TargetDescr` carries only an id, so the descriptor a backend already keeps
per texture is where the format is read from rather than being copied into the
pass. A texture is unknown until `Execute` replays the bake that allocates it,
so the frame that allocates a target cannot answer for it and gfx keys that
frame's pipelines from the allocation it translated, so the pass renders on
its first frame. `BeginPass`
takes the recorder's own `PassDescr`, and the backend resolves its attachments
as it opens the pass.

`TakeCapture` is drained once per frame, immediately after `Execute`, and
never blocks: what it has ready is the copy the *previous* frame encoded,
whose map resolved on the submit `Execute` just made.

Low-level descriptors are `TextureDesc`, `SamplerDesc`,
`ShaderDesc`, `PipelineDesc`, `ShaderLayout`,
`ShaderResource`, `StorageMember`, `ShaderVertexInput`, and
`Region`. Reflection
reports member layout for storage structs too — a one-level walk in which an
array member carries its element stride and count — so a recorder that declares
no uniform block at all packs its records from the same source of truth. A
shader may declare several uniform bindings: each is its own `ShaderResource`,
set whole by a param of its name, uploaded into its own span of the frame's
uniform arena once a frame and bound at its own group and binding.
It also reports the vertex stage's `@location` inputs as `ShaderVertexInput`
values — the location plus a `VertexScalar` kind and a component count — which
is the half of the vertex interface only the shader knows.
Enums include
`TextureFormat` (`FormatRGBA8`, `FormatRGBA8Srgb`, `FormatDepth32F`, and
`FrameBufferFormat`, the one every screen pass renders into),
`BufferKind` (`BufferVertex`, `BufferIndex`, `BufferUniform`, `BufferStorage`),
and `TextureViewDimension` (`TextureView2D`, `TextureView2DArray`).

`TextureDesc.Renderable` asks for a texture a render pass can draw into as well
as sample. Mip generation filters in the format's own colour space — decoding
and re-encoding around the box filter for `FormatRGBA8Srgb` — and is refused for
depth.

Opaque handles are based on `ResourceID`: `TextureID`, `BufferID`, `SamplerID`,
`ShaderID`, `PipelineID`, and `TextureViewID`. Zero means no resource.

`Queue` records through `BakeBuffer`, `BakeTexture`, `AllocateTexture`,
`UpdateTexture`, `BeginPass`, `EndPass`, `SetPipeline`, `SetUniformBlock`,
`SetTexture`, `SetSampler`, `SetVertexBuffer`, `SetIndexBuffer`, `SetBuffer`,
`Draw`, `Present`, `Capture`, `ReleaseBuffer`, and `ReleaseTexture`. `ReplayBakes(BakeSink)`, `ReplayPasses(PassSink)`, and
`ReplayReleases(ReleaseSink)` send each phase to a backend, and they are the
whole of the queue's read side; `Reset` reuses the queue. Bakes are hoisted
ahead of every pass, so a pass can read anything the frame uploaded. The sink interfaces define
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

`BufferSourceBytes` and `BufferSourceBaked` are exported source-marker
constants; normal callers use descriptor constructors instead. `TextureDescr`
and `ShaderDescr` have none: their cases are told apart by which of their fields
carries the answer - a baked id, a path, or inline pixels for a texture; a path
or inline text for a shader - and a marker would only restate that.

## Math

Shared value types and operations live in the root `m` package. Graphics APIs
use `m.Vec*`, `m.Rect`, `m.Color`, and column-major `m.Mat4` directly.

## Errors

`ErrBackendNotReady{}` is reported the first time a frame is rendered before the
Backend Adapter is ready, and the frame is skipped. A missing Adapter is not a
gfx error: composition fails with `kernel.ErrMissingAdapter`.

A shader whose source cannot be read, flattened or reflected is not reported:
`CompileShaderCmd` returns why in its response's `Err` - the read's own error,
or `ErrShaderSource` for a module the preprocessor or reflection refused - and
the caller decides what a missing shader means. A module the backend refuses
when the upload is replayed is reported once as `ErrShaderSource`, carrying the
segment table of the flattened module.

`ErrParameterKindMismatch{Shader, Parameter, Supplied, Declared}` is reported
when a parameter's name matches a binding its kind cannot fill, by the set
method it reached, once, and the param is ignored. See the parameter section
above for why an unreported one is worse than an ignored param.

`ErrStorageBufferUnsupplied{Shader, Parameter, Group, Binding, Unbaked}` is
reported when a declared storage binding goes unfilled, and the draw is dropped.
`Unbaked` separates a binding no parameter names from one whose parameter
carries a buffer that was never baked. Reported once per `(shader, parameter)`.
See the parameter section above.

`ErrVertexInputUnsupplied{Shader, Input, Location, Declared}`,
`ErrVertexInputMismatch{Shader, Input, Location, Declared, Supplied}` and
`ErrVertexStrideAlignment{Shader, Stride}` are the three ways a vertex layout
fails the shader bound with it. Each drops the draw and reports once per
offending pair. See *The vertex interface* above.

`ErrPipelineFailed{Shader, Err}` carries the backend's own refusal of a
pipeline, which gfx used to discard — making "gfx refused to build this" and
"the backend refused to build this" the same silent event from the caller's
seat. It unwraps to the backend's error.

`ErrIndexBufferLength{Shader, Length, Width}` reports an index buffer whose byte
length is not a multiple of the width its `MeshDescr` declared — a buffer
declared `uint16` and written as `uint32` being the way two widths make a draw
silently wrong. The draw is dropped and the shape is reported once. It is the
`O(1)` half: whether every index is below the vertex count is checked by
whoever built the geometry, where a pass over the indices already runs.


The capture errors are typed for the same reason: a caller reads them, and a
burst branches on them. Four are reported by a backend as well as by gfx:
`gfx.ErrCaptureBusy{}` is a second arm while one is live;
`gfx.ErrCaptureAbandoned{}` a capture the engine stopped before its readback
resolved; `gfx.ErrCaptureUnsupported{Format}` depth or anything else that is
not 8-bit RGBA; and `gfx.ErrCaptureNoTarget{}` a target the frame never
rendered into. The root also offers `ErrCaptureAmount{Amount, Max}`,
`ErrCaptureSpan{Ticks, Max}` and `ErrCaptureBurstPaused{}`, the three ways a burst is asked for and refused.
