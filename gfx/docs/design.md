# gfx: design

`github.com/dvoyni/cog/gfx` is the GPU as a kernel sees it: a super-thin
mirror of wgpu, in its own types, that draws one frame per beat of the
render track. A **backend** implements it: the gogpu plugin over our wgpu
fork, or the **recording adapter** in `gfx/recording` for tests. gfx owns
what is the same for every backend: the handle tables and their descriptors,
the frame chain, submit order, surfaces following platform's windows,
the handles of "auto" layouts, readback and
capture, and recovery after a device loss. It knows nothing of materials,
scenes or the ECS: scene builds forward+, deferred and post effects on it,
and nothing in gfx knows which.

Status: being built. The decisions come from the
[gfx map](https://github.com/dvoyni/cog/issues/608); each section links the
ticket that holds its detail. Every type, method and descriptor is declared
in this module, from the listing on
[`prototype/gfx-listing`](https://github.com/dvoyni/cog/tree/prototype/gfx-listing/gfx).
The device comes up and is released, every resource is
created and released, `CreateQuerySet` answering `ErrQuerySetsUnavailable`
until the wgpu fork exposes query sets, a pipeline with a zero `Layout` getting the layouts
the backend reflects created as explicit ones and released with it, and
pipeline creation checks the vertex buffers against the shader's inputs and
the pipeline against the device's limits, surfaces
follow platform's windows, each configured from `Config.Surface`, or the
request `RequestSurface` replaced it with, before its first acquire and
again once resized or the request changes, hidden and zero-size windows skipped,
and the frame chain acquires, handling every acquire status alike, publishes `DrawEvent`, flushes
the frame's encoders and presents, the recording adapter recording their
passes and commands with typed arguments and rejecting a handle missing at
its index, a resource without its usage, a misaligned write, copy or clear,
a broken pass sequence and any query set, its buffers holding the bytes written, copied
and cleared. Writes reach the backend. Buffers map asynchronously: each
poll, at `Poll`, every submit and every frame's flush, publishes
`BufferMappedEvent` for the maps resolved, `MapStatusFailed` with its error
in `LogErr` for one the backend fails, and the device's release at teardown
unmaps and releases the buffers of the maps still pending. A window's capture, requested on
any configured window of an 8-bit format, is taken from the first frame
acquired after the request, which tells the backend at the acquire, copied out by gfx's own
encoder after every submit order and delivered by `CapturedEvent`, its buffer unmapped and released
after the publication's out dependency, and `ReadCapture` answers its
picture un-strided, BGRA swapped and alpha opaque. A device loss the
backend answers from any call is logged and published as `DeviceLostEvent`
once; from then on no call reaches the backend, every call with an error
answering `ErrDeviceLost`, and the frame in flight skips its submit and
present. The next beat recovers instead of drawing, under the write lock: a
new device, from a fresh adapter when the old one refuses, every object
recreated on it under its old handle in dependency order, one released
while an object made from it is held among them and released again after,
every surface marked, and `DeviceRecoveredEvent` published; a recovery that fails is
logged once and retried at most once a second, and the maps and captures
pending at the loss hear as failures. `DrawEvent` carries `AcquireWait`,
timed around the backend's acquires, and `GPULatency`, from the frames'
submission indices, both measured by stopwatches of metronome's clock.
The backend reflects shader modules:
its seam declares `ReflectRenderPipelineLayout`,
`ReflectComputePipelineLayout` and `ReflectVertexInputs`, which the recording
adapter answers with the layouts and inputs a test queues. The backend
preprocesses shaders too, through `Device.PreprocessShader`, which the
recording adapter answers with the file's text as it is. Standalone encoders are finished
and submitted by their owner. The vocabulary is in [`CONTEXT.md`](../CONTEXT.md); the ADRs are in
[`adr/`](adr).

## Constraints

- **A mirror, not a renderer.** gfx follows wgpu's public API one to one, in
  its own types; no wgpu type appears in it. It adds only what a kernel
  needs: handles, the frame, events. Switching implementations is unlikely;
  the seam exists for tests and to keep wgpu's churn in one place.
- **Pointer-free work.** Handles, events and task args carry no pointers,
  slices, strings or interfaces, as the kernel requires.
- **No ECS import.** gfx works without the ECS. A handle fits a component
  registered transient; scene saves the asset and recreates the handle on
  load.
- **Imports:** kernel, metronome and platform only. gfx requires platform
  and metronome, listed before it, and says so with `Plugin.Dependencies`
  ([kernel ADR 0007](../../kernel/docs/adr/0007-plugins-declare-dependencies-checked-not-sorted.md)):
  it makes its stopwatches from metronome's `Clock` resource in its `Init`.
- **Targets:** desktop native first, Vulkan, DX12 and Metal. gfx is designed
  against WebGPU's default limits; features are opt-in. GLES and the web are
  deferred.
- **Versions are pinned:** gogpu/wgpu v0.34.2, gputypes v0.8.0, gpucontext
  v0.31.3. wgpu is pre-1.0 and changes daily.

## Shape

([gfx's shape in today's kernel](https://github.com/dvoyni/cog/issues/659),
[The interface listing](https://github.com/dvoyni/cog/issues/662))

| Type | Kind | What it is |
|---|---|---|
| `InstanceSocket` | required exclusive socket | The backend seam, wgpu's instance mirrored: `RequestAdapter`, `CreateSurface`, `ReleaseSurface`. A kernel with none panics in `Spawn`. `BackendAdapter` and `BackendDevice` are what it answers. |
| `Device` | interface resource, `kernel.Read[gfx.Device]` | The GPU as every job shares it: creating, releasing, writing, mapping, recording and submitting. |
| `ExclusiveDevice` | no resource | `Device` plus what no job may overlap: `WaitIdle`, `RequestSurface`. `ElevateDeviceAccess(kernel.Write[Device])` answers it. |
| `Handlers` | resource | The `kernel.Sub` of each handler: `Draw` on `RenderEvent`, `CreateSurface`, `ResizeSurface` and `ReleaseSurface` on platform's window events. |
| `Buffer`, `Texture`, `TextureView`, `Sampler`, `ShaderModule`, `BindGroupLayout`, `BindGroup`, `PipelineLayout`, `RenderPipeline`, `ComputePipeline`, `QuerySet` | handles | Generational, comparable, pointer-free; zero is none. |
| `CommandEncoder`, `StandaloneCommandEncoder`, `RenderPassEncoder`, `ComputePassEncoder`, `CommandBuffer` | interfaces | Transient recording objects. |
| `RenderEvent` | event | The render track's beat, `struct{ metronome.Beat }`; the application declares the track. |
| `DrawEvent` | event | Published inside gfx's `RenderEvent` handler: `Beat`, `Frame`, `Ready`, `AcquireWait`, `GPULatency`. |
| `Frame` | handle | One frame in flight: a slot in gfx's pool of frames. |
| `SubmitOrder` | `int32` | The number a frame encoder is handed under; one space shared by every plugin. |
| `SurfaceRequest`, `SurfaceConfiguration` | values | What the application asks every surface for, and what each window got. |
| `BufferMappedEvent`, `CapturedEvent`, `DeviceLostEvent`, `DeviceRecoveredEvent` | events | Readback, capture and device loss. |
| `Config` | config | `PowerPreference`, `ForceFallbackAdapter`, `RequiredFeatures`, `RequiredLimits`, `Surface`. |

gfx schedules acquire, configure, flush, present, recovery and the release
at teardown itself; the `Tasks` resource holds the surface tasks, the
device's release and the recovery among them, for a backend to pin.

## The seam and the device

([gfx's shape in today's kernel](https://github.com/dvoyni/cog/issues/659),
[The interface listing](https://github.com/dvoyni/cog/issues/662))

- **One socket.** `InstanceSocket` is exclusive and required. gogpu binds
  it; so does the recording adapter, in a test's plugin.
- **gfx owns the handle tables.** It allocates each handle, checks its
  generation, and keeps each object's descriptor, slices deep-copied. It hands
  the backend the handle, and the backend keeps its native object at
  `handle.Index()`. Descriptors and recording calls reach the backend as
  gfx's own, handles included, so nothing is translated and nothing
  allocates. gfx checks every handle in a call before making it.
- **Every operation is a `Device` method,** one per type
  (`CreateBuffer`/`ReleaseBuffer`, …), since a handle cannot reach the device
  and a `Release(Handle)` would box. The queue folds into `Device`:
  `WriteBuffer`, `WriteTexture`, `Submit`. gfx answers each resource's
  descriptor back: `BufferDescriptor`, `TextureDescriptor`,
  `TextureViewTexture`.
- **The read lock is the normal case.** Creating, writing, recording,
  flushing and presenting run under `kernel.Read[Device]`, in parallel; the
  device, adapter and instance are safe from many goroutines (not under
  `-tags rust`). What must not overlap runs under the write lock, through
  `ElevateDeviceAccess`: configuring surfaces, recovering a lost device,
  `WaitIdle`, `RequestSurface`.
- **The device comes up in `Start`,** since sockets are read from `Start` on,
  from `Config`. A zero `RequiredLimits` is `DefaultLimits()`, WebGPU's,
  passed explicitly, because wgpu's zero is the adapter's full limits. No
  adapter is a runtime error from `Start`. `Stop` schedules
  `Tasks.ReleaseOpenSurfaces` after the last present, then
  `Tasks.ReleaseDevice`, each under the write lock: they release the
  surfaces of the windows still open, then the buffers of the maps still
  pending, the device, the adapter and the instance, on the thread the
  backend pins them to, since `Destroy` runs on the kernel's destroy
  goroutine and gogpu's `HostLoop` stops on the main thread
  ([gogpu plugin: gfx gets gogpu's adapter and device](https://github.com/dvoyni/cog/issues/743)).
  The kernel finishes them before any `Destroy`, and main keeps ticking
  pinned jobs until `Done`. Frames in flight finish first, and a beat after
  the release draws nothing. `Destroy` releases only what a `Start` that
  failed at the device had requested, on `Spawn`'s goroutine, since no
  `Stop` runs for it. Releasing the device cleanly is what saves Vulkan's
  pipeline cache.

## Types, descriptors and handles

([The interface listing](https://github.com/dvoyni/cog/issues/662))

- **Enums are gfx's own, numbered as gputypes numbers them,** so gogpu
  converts by cast, checked by a test in the gogpu plugin. They are generated
  from gputypes v0.8.0. Ray tracing, push constants, indirect-count and
  adapter-specific bits are left out.
- **Descriptors mirror wgpu's,** with handles in place of objects. **An
  optional part is a value whose zero means none,** never a pointer:
  `Blend`, `DepthStencil`, `Fragment`, `StripIndexFormat`, `ResolveTarget`,
  the depth-stencil attachment, `TimestampWrites`, and the layout entry's five
  kinds, exactly one non-zero. Slices stay where the count has no small bound.
  Names and shapes follow gogpu's Go API (`Dispatch`, `DrawArgs`, region
  copies), renamed only where the naming rules require.
- **Handles** each embed one private `handle`, `struct{ index, generation
  uint32 }`, which gives them `Index()`; they stay distinct types, and a
  table is generic over `~struct{ handle }`. A stale one answers
  `ErrReleased` from calls with an error, is recorded as a deferred error by
  recording calls, and a second release logs `ErrReleased` and does nothing.
- **Release is immediate,** as in wgpu, which reference-counts resources until
  the submits using them finish.
- **A handle released while an object made from it is held is kept**
  ([gfx: a recovery recreates an object from what it was made with](https://github.com/dvoyni/cog/issues/812)):
  a pipeline's shader modules and layout, a pipeline layout's bind group
  layouts, a bind group's layout and resources, a view's texture. It is stale
  to its caller at once and its backend object is released, as wgpu-core
  keeps a pipeline's module and layout by reference; gfx keeps its
  descriptor and its index, so no object created since stands in for it, and
  a recovery creates it again before the objects made from it and releases
  it again after. Its index is handed out once the last of them is released.
- **Left out of wgpu:** render bundles and indirect-count draws (absent
  upstream), error scopes (inert on native), SPIR-V, `TransitionTextures`.
  Instance, adapter, surface objects, fences, the surface extensions, HAL
  access and gpucontext are internal to gfx and its backends.

## Errors

- **Errors mirror wgpu.** `Create*`, writes, maps, `StandaloneCommandEncoder.Finish`
  and `Device.Submit` return them. A pass's `End` returns nothing; its error,
  a frame submit's error and device loss go to `LogErr`.
- **Composition mistakes panic,** as the kernel's rule says.
- Sentinels: `ErrReleased`, `ErrOrderTaken`, `ErrFrameFlushed`,
  `ErrCaptureBusy`, `ErrCaptureUnsupported`, `ErrDeviceLost`.

## Shaders and pipelines

([The interface listing](https://github.com/dvoyni/cog/issues/662),
[Pipeline creation off the frame](https://github.com/dvoyni/cog/issues/668),
[Shader preprocessing moved to the backend](https://github.com/dvoyni/cog/issues/709))

- **WGSL only.** `CreateShaderModule(ShaderModuleDescriptor{Label, WGSL})`
  takes final WGSL. Hot reload is deferred.
- **Preprocessing is the backend's,** since the backend owns the WGSL it
  accepts. `Device.PreprocessShader(files, path, defines)` passes the call to
  `InstanceSocket.PreprocessShader`; the gogpu plugin runs v1's preprocessor,
  `gogpu/shader` (`#include`, `#define`, `#const`, `#if`), and the recording
  adapter answers the file's text as it is. A task calls it holding
  `kernel.Read[Device]` and `kernel.Read[platform.FS]`, then
  `CreateShaderModule`. A refusal is an `ErrShaderSource`, matched with
  `errors.As`, naming the shader and its lines. gfx keeps the seam types,
  `ShaderDefine`, `ShaderSourceLocation` and `ErrShaderSource`, and handles
  no directive. It is the instance's work, so it goes on through a device
  loss.
- **The backend reflects a module once, at its creation,** and keeps the
  result at the module's index; invalid WGSL fails `CreateShaderModule` with
  its error. gfx parses no WGSL and depends on no shader compiler: the gogpu
  backend already links naga through wgpu. A pipeline with a zero `Layout`
  gets an "auto" layout: gfx asks the backend's `ReflectRenderPipelineLayout`
  or `ReflectComputePipelineLayout` for its groups' descriptors, as WebGPU
  builds them, and creates them as explicit layouts, the recording adapter
  answering the ones a test queues; `RenderPipelineBindGroupLayout` and
  `ComputePipelineBindGroupLayout` answer its groups, as WebGPU's
  `getBindGroupLayout`, the pipeline owning them: they are released with
  it. An explicit layout overrides reflection: the backend is not asked.
- **gfx checks a pipeline before creating it, from what it can see.** The
  backend's `ReflectVertexInputs` answers a vertex entry point's inputs, each
  location's scalar type and component count; an input no attribute feeds,
  none at its location or one of another type (a `Uint*` format for an `f32`
  input), is `ErrVertexMismatch` naming both. As in WebGPU, an attribute no
  input reads, and a component count differing from the input's, are fine.
  The pipeline's layout, explicit or reflected, and a render pipeline's
  vertex buffers are checked against the device's limits, the groups, a
  binding number, each stage's bindings of each kind, the dynamic buffers,
  the vertex buffers, a stride, an attribute's location and the attributes:
  `ErrLimitExceeded` names the limit, before any layout is created. What
  needs more of the shader, a workgroup's size, the inter-stage variables, is
  the backend's.
- **Off the frame means a task.** Creation is synchronous on every backend.
  A game creates shader modules and pipelines from a task of its own under
  `kernel.Read[Device]`, scheduled anywhere; gfx has no async call and no
  worker. Two costs are documented: a compile holds the read lock, so a
  surface configure, and the frame behind it, waits for it; and a burst of
  compiles can take every worker from a frame's recording, so the game bounds
  how many run at once.
- **No pipeline cache of gfx's.** The backend's own disk caches persist on
  Vulkan and DX12, under `os.UserCacheDir()/gogpu/`, outside platform's `FS`;
  Metal has none. Each create answers its own handle; sharing pipelines of one
  descriptor is scene's.

## Frames

([gfx's shape in today's kernel](https://github.com/dvoyni/cog/issues/659),
[ADR 0001](adr/0001-draw-event-inside-render-event.md),
[Frames in flight and what gfx keeps per frame](https://github.com/dvoyni/cog/issues/663),
[ADR 0002](adr/0002-frames-record-one-at-a-time.md))

- **The draw chain is a nested publish.** gfx has one `RenderEvent` handler,
  `Handlers.Draw`. It schedules, each after the last present: a configure
  of any surface marked for it, under the write lock; then the acquire. It
  publishes `DrawEvent` with `ex.Publish(acquired, …)`, schedules the flush
  and the present after the out dependency that answers, and returns the
  present to metronome. Application handlers subscribe to `DrawEvent` with no
  ordering, schedule their recording tasks after `e.Ready` with the event as
  their arg, and return `ex.Join(in, own)`.
- **gfx pins none of its jobs**
  ([gfx: no task pinned for a backend's sake](https://github.com/dvoyni/cog/issues/707)).
  Which thread a backend call must run on is the backend's, and differs by
  platform: the `Tasks` resource holds the surface tasks (create, release,
  configure, acquire, present, and the release of the surfaces still open at
  teardown), the device's release at teardown and its recovery, and the
  backend's plugin, listed after gfx, pins those its platform needs to the
  main thread.
- **One frame covers every open window:** an acquire per window in one job,
  one flush, a present of each. A window with no framebuffer this frame
  (hidden, a zero framebuffer, outdated, timed out) answers the zero view. `DrawEvent` is published every beat, even when no window has one.
- **Frames record one at a time.** wgpu lets a surface hold one acquired
  framebuffer, so each acquire follows the previous present, and `Ready`
  stays the acquire. Frames overlap on the GPU through wgpu's swapchain, not
  on the CPU. Acquire late is deferred; it can return only under the rule in
  ADR 0002 and the vsync preset back at two beats in flight.
- **Frames live in a pool of slots,** acquired in the `RenderEvent` handler
  and released after the present, the generation advanced. The pool grows
  only while more beats are in flight than it holds, so it is bounded by the
  render track's `MaxInFlight`, and a render track needs one. There is no
  knob, and no reconciliation with wgpu's own bounds: a `MaxInFlight` above
  them queues beats behind the acquire chain.
- **A slot keeps** its generation, its `(order, encoder)` pairs in a reused
  slice sorted at flush, a mutex held only for the duplicate check and the
  append, one framebuffer `TextureView` handle per window acquired, and a
  flushed flag. gfx's bookkeeping is allocation-free once warm; wgpu
  allocates per encoder.
- **Frame encoders:** `Device.Encoder(frame, order)` answers one per
  submit order, with no limit; one per task that records in parallel, not one
  per draw. An order taken twice answers `ErrOrderTaken`, and a flushed or
  presented frame `ErrFrameFlushed`, each with an encoder that records into
  nothing. The flush finishes them, sorts by order and submits once; an
  encoder whose `Finish` fails goes to `LogErr` and is dropped, and the rest
  are submitted.
- **A `Frame` is valid until its present.** After the flush `Framebuffer`
  still answers the view; once the slot is released, the zero view.
- **Standalone encoders** come from `CreateCommandEncoder`, outside any
  frame; their owner calls `Finish` and `Device.Submit`.
- **Timing signals for adaptive quality:** `DrawEvent.AcquireWait`, how long
  the previous frame's acquire blocked (under Fifo, a long wait is headroom),
  and `GPULatency`, from the newest finished frame's submit to when gfx first
  saw it complete by submission index, accurate to one check a frame. gfx
  measures them by two stopwatches it makes from metronome's `Clock`
  resource in `Init`, keeping the stopwatches, never the clock:
  `AcquireStopwatchName`, started at each backend acquire, and
  `SubmissionStopwatchName`, running since `Init`, whose readings stamp each
  frame's submit and the poll that sees it finished. On a `ManualClock` they
  are deterministic, and a test lengthens an acquire with `LengthenNext`
  ([Timestamps and GPU profiling](https://github.com/dvoyni/cog/issues/667)).
  The seam mirrors wgpu's queue: `Submit` answers the submission's index and
  `Poll` the newest one finished. Every poll checks the frames' submits,
  and a recovery forgets the ones the lost device had not finished, since
  the new device counts from one again.
- **metronome's vsync preset** becomes `MaxInFlight: 1` plus a 1/240 s
  `MinInterval` floor: one beat in flight takes about a frame off the gap
  between a beat's time and the display, and the floor keeps a minimized
  application from spinning.
- **Windows and frames:** `ReleaseSurface` is scheduled after the last
  present, and the window is marked closing at once, so no acquire still to
  run takes its framebuffer; a window opened mid-frame joins the next frame.

## Writes and transient memory

([Per-frame transient memory](https://github.com/dvoyni/cog/issues/664))

- **`WriteBuffer` and `WriteTexture` are the upload path.** wgpu copies the
  data into its own staging belt before the call returns and prepends the
  copy to the next submit, so it lands before every command buffer of that
  submit, whichever frame or standalone encoder it carries. Two writes to one
  region in a frame leave the last for the whole frame. Each call takes the
  queue's lock: write a task's whole region once, not once per draw.
- **No staging belt, uniform ring or transient-target pool in gfx.** The
  uniform ring and the pool of transient targets are scene's, on the frame
  graph's side of the line. gfx guarantees them safe: release is immediate and
  reference-counted, and reuse across frames is ordered on the queue.

## Surfaces

([Surface configuration](https://github.com/dvoyni/cog/issues/665))

- **Surfaces follow platform's windows.** `CreateSurface` on
  `WindowOpenedEvent` creates one from `WindowManager.Native`, in a task; `ReleaseSurface` on `WindowClosedEvent` releases it. Both hold the
  write lock. A creation that fails goes to `LogErr` and leaves the window
  without a surface. A surface is named by its `platform.Window`; there is no
  public surface handle.
- **Configured in the frame chain, between frames.** A surface is marked for
  configuring when it is new, resized (`ResizeSurface` on `ResizeEvent` only
  marks it), reported suboptimal, outdated or lost, or the request changes.
  `Draw` schedules the configure under the write lock, after the last
  present and before the acquire. A window that is hidden, as its entry
  requests, or has a zero framebuffer is neither configured nor acquired; it
  stays marked, so it is configured on the first frame it is drawable again.
- **One surface request for the whole application:**
  `SurfaceRequest{Format, PresentMode}` in `Config.Surface`,
  replaced at runtime with `ExclusiveDevice.RequestSurface`. Zero means
  `BGRA8Unorm`, which every backend accepts, and Fifo, which every backend
  has. sRGB encoding is the last pass's; `BGRA8UnormSrgb` and the present
  modes beyond Fifo are honoured where the backend's surface capabilities
  offer them, and gogpu's offer `BGRA8Unorm` and Fifo alone. A part a
  surface cannot honour falls back, with a `LogErr` at each configure. `Device.SurfaceConfiguration(window)` answers what each window
  got, for pipelines to target. Every surface is opaque. HDR is deferred: no
  backend can present it.
- **Present modes and metronome's presets** pair without enforcement: vsync
  with Fifo, limit and uncapped with Mailbox or Immediate, where the backend
  offers them: on gogpu, which offers Fifo alone, every preset is held to
  the display's refresh. On Metal Immediate and Mailbox are the same, and
  FifoRelaxed turns vsync off.
- **Acquire results** reach gfx as an `AcquireStatus` the backend maps from
  its errors: `Suboptimal` draws and marks; `Outdated` draws nothing in that
  window this frame and marks; `Lost` recreates the surface from its native
  handle and marks, its configure recreating it first; `Timeout` skips the
  window, with no reconfigure. Any other error goes to `LogErr`, draws
  nothing in that window and marks.

## Readback and capture

([Readback and capture](https://github.com/dvoyni/cog/issues/666))

- **Maps are asynchronous only.** `Device.MapBuffer` starts one; gfx checks
  its pending maps after every submit and every frame's flush, submitting or
  not, and publishes `BufferMappedEvent{Buffer, Status}` from that job:
  `MapStatusFailed` when the backend fails the map, its error going to
  `LogErr` first. A map ended before it resolves, by `UnmapBuffer` or
  `ReleaseBuffer`, publishes nothing. A handler's task reads `MappedRange` until
  `UnmapBuffer`. `Device.Poll` resolves finished maps for a kernel with no
  frames running. There is no blocking map: it would wait for the whole
  device to go idle.
- **Capture is gfx's.** `Device.RequestCapture(window)` asks for the
  framebuffer of the first frame whose acquire runs after the request, so a
  capture requested while a frame draws is taken from the next one
  ([gfx: automatic visual checks from frame captures](https://github.com/dvoyni/cog/issues/717)).
  That acquire tells the backend the window is captured, and only then is
  the framebuffer's texture a copy source. A backend whose surface textures
  cannot be copied hands an offscreen texture of the surface's format and
  draws it onto the surface at the present, as v1 did, so the cost falls
  only on frames captured. gfx's flush appends its own encoder after every
  submit order, copying the framebuffer into a fresh staging buffer before
  the present, and maps it after the submit. The backend hands each
  framebuffer over as a texture as well as a view, for that copy; a map,
  copy or submit that fails delivers `CaptureStatusFailed`, its error in
  `LogErr`. `CapturedEvent{Window, Beat, Buffer,
  Width, Height, Format, BytesPerRow, Status}` delivers it a frame later;
  `Beat` names the moment shown. gfx unmaps and releases the buffer after the
  publication's out dependency. `ReadCapture(device, event)` answers an
  `image.NRGBA`: rows un-strided, BGRA swapped, alpha opaque, no colour
  conversion. An event that is not `CaptureStatusCaptured` answers an
  error. One capture is in flight per window (`ErrCaptureBusy`).
- **Every configured window of an 8-bit format can be captured:** no
  surface is configured for it, so every backend captures, DX12 and the
  software adapter included. A window not yet configured, or whose format
  is not 8-bit BGRA or RGBA, `RequestCapture` answers
  `ErrCaptureUnsupported`. A window that acquired nothing that frame gets
  `CapturedEvent` with `CaptureStatusNotAcquired`.
- **At shutdown** pending maps and captures are dropped without an event;
  the device's release unmaps and releases their buffers.

## Timestamps

([Timestamps and GPU profiling](https://github.com/dvoyni/cog/issues/667))

- **WebGPU's query sets are mirrored and declared, waiting on the wgpu
  fork:** a `QuerySet` handle, `TimestampWrites` on pass descriptors,
  `CommandEncoder.ResolveQuerySet`, `BufferUsageFlagQueryResolve`,
  `Device.TimestampPeriod()`, and `FeatureFlagTimestampQuery` reported only
  where it works. Until the fork exposes them, `CreateQuerySet` answers
  `ErrQuerySetsUnavailable` on every backend, and `ReleaseQuerySet` is
  handed only the zero query set it answered, logging `ErrReleased`; a
  pass's `TimestampWrites` naming no query set write nothing. Results come
  back through the asynchronous maps.
- **gfx collects no timings.** A per-pass profiler is scene's or a profiler
  plugin's.

## Device loss

([Device loss](https://github.com/dvoyni/cog/issues/669),
[ADR 0003](adr/0003-handles-survive-device-loss.md),
[gfx: device recovery is a task in gfx.Tasks, for a backend to pin](https://github.com/dvoyni/cog/issues/716))

- **Detection:** the backend maps its errors onto `ErrDeviceLost`, from any
  call that answers an error. gfx checks every backend call it makes; the
  first job to see it logs the loss and publishes `DeviceLostEvent`. Tasks
  running then answer `ErrDeviceLost` and record nothing; the frame in flight
  skips its submit and present. No call reaches the lost device: a release
  only retires its handle, since the device's own `Release` releases every
  object it holds, and drops the framebuffer the skipped present left
  acquired.
- **Handles survive.** On the next beat, instead of a frame, `Draw` schedules
  the recovery, `Tasks.RecoverDevice`, under the write lock. A backend pins
  it when its device replacement touches state its platform keeps on the
  main thread, as gogpu's does. The recovery: release the device, request a
  new one from the same adapter (a fresh adapter if that fails), recreate
  every object under its old handle from its kept descriptor, in dependency order
  (buffers, textures, samplers, shader modules; views, bind group layouts;
  pipeline layouts; pipelines; bind groups), mark every surface, publish
  `DeviceRecoveredEvent`. It publishes no `DrawEvent` and is the beat's out
  dependency.
- **Contents are their owners'.** Buffers and textures come back empty; owners
  refill them from tasks they schedule on `DeviceRecoveredEvent`.
- **A failed recovery** is retried on a later beat, at most once a second by
  `Beat.TimeFromStart`, with `LogErrOnce`, and no limit; a recovery that
  succeeds forgets the key, so the next loss's failures are logged again.
  The job that finds the loss publishes every pending map `MapStatusFailed`
  and every pending capture `CaptureStatusFailed`, its buffer released; a
  poll stops at the first status that answers the loss. Captures requested
  and not yet copied, a lost frame's included, stay requested for the frames
  after the recovery.

## The recording adapter

([The headless recording adapter](https://github.com/dvoyni/cog/issues/670))

- **An adapter, not a plugin:** `recording.NewInstance()` answers an
  `InstanceSocket` the test's own plugin binds. The test reads
  `instance.Recording()` once the kernel has settled.
- **It records what reaches the socket,** as typed values: every create and
  release with its handle and descriptor, writes and maps, surface calls with
  the status answered, and each submission as its command buffers, passes and
  commands with arguments. `Recording.Frames()` groups what lies between a
  frame's first acquire and its last present; a frame with no window
  acquired is its one submission. Submissions and surface calls are in a
  deterministic order; lifecycle calls from parallel tasks are not, and tests
  look them up by handle.
- **It checks a fixed subset of what wgpu rejects:** a handle missing at its
  index, usage flags, write and copy alignment, pass state. It answers what a
  real backend would.
- **Features:** every feature but `FeatureFlagTimestampQuery`; a device
  requiring one it lacks is refused, as wgpu refuses it. `CreateQuerySet`
  answers `ErrQuerySetsUnavailable`, timestamp writes or a resolve naming
  any query set fail the encoder, and `TimestampPeriod` answers
  `recording.TimestampPeriod`, one nanosecond.
- **Submissions** are indexed from one on each device and finish at once,
  so `GPULatency` is zero.
- **Data:** buffers hold bytes, which writes, buffer copies and clears apply
  to; textures hold no texels; captures complete zero-filled.
- **Injection:** `instance.AnswerNext(kind, answer)` makes the next call of a
  kind answer an acquire status, a map failure (`CallKindMapStatus`, never
  recorded, ending the map) or an error, `ErrDeviceLost` included.
- **Real pixels without a GPU** come from gogpu on wgpu's software adapter,
  through `Config.ForceFallbackAdapter`, not from here.

## Post effects and deferred

gfx fixes only the ends of the frame. Everything between is passes the user
declares, run on the GPU in submit order, so post effects and deferred need
nothing from gfx beyond a correct barrier when a target is sampled after it
was rendered, which the backend records
([wgpu fork: a texture sampled after its use as an attachment gets its barrier on Vulkan](https://github.com/dvoyni/cog/issues/764)).

## Gaps in the wgpu fork and the gogpu backend

What the backend must do, and what the fork must fix first, are carried by
the [gogpu map](https://github.com/dvoyni/cog/issues/611):

- binding `InstanceSocket`; mapping its errors onto `ErrDeviceLost` and
  `AcquireStatus`; a device `Release` that releases every object it still
  holds and drops a framebuffer acquired and not presented; a captured
  acquire's framebuffer a copy source, drawn offscreen where the surface's
  cannot be; honouring `ForceFallbackAdapter`; a
  headless surface for a window with a zero native handle; pixel tests on the
  software adapter; a test that the enums convert by cast;
- two swapchains in one frame are unsafe in wgpu's Vulkan backend, so several
  windows wait on a fix;
- `Release` races recording on textures, views, samplers, shader modules,
  layouts and pipelines (a plain `bool`);
- under `-tags rust`, go-webgpu's `Device` is unsafe from several goroutines;
- DX12's flip model rejects the sRGB formats wgpu advertises, and its back
  buffers cannot be copied out, so the backend draws a captured frame
  offscreen;
- no backend presents HDR;
- DX12 reports a removed device only as an untyped error, and Metal never
  detects loss;
- DX12's `ClearBuffer` records nothing, so a clear zeroes nothing there;
- no public query sets or timestamps;
- whether a minimized or occluded window on macOS holds `nextDrawable`, which
  has no timeout, is to be checked; if so, platform needs a minimized state.

## Testing

Two seams, through gfx's public interfaces:

- **gfx in a real kernel.** A test plugin binds the recording adapter and
  platform's headless windows adapter, and metronome paces the render track
  on a `ManualClock`. Tests assert only through the `Recording`, gfx's events
  and what `Device` answers: the frame chain and its order, submit order,
  encoder errors, surface marks and configures, acquire statuses, maps and
  captures, device-loss recovery through `AnswerNext`, and `DrawEvent`'s
  durations. As metronome's tests do, they reach the kernel through the
  `Supervisor`.
- **The recording adapter's checks,** at its own interface, without a
  kernel.

Concurrent behaviour, the frame slot's mutex and parallel recording, runs
under `go test -race` and is checked by breaking the code it guards. Pixel
tests through the software adapter are the gogpu plugin's.

## Out of scope

- Materials, PBR, forward+, deferred, post effects, and a frame graph with its
  transient targets and per-frame memory: scene, built on gfx.
- The agent-facing capture tool (v1's `gfx_capture` and its PNG): a plugin
  of its own on `CapturedEvent`.
- A per-pass GPU profiler and its plumbing: scene's or a profiler plugin's.
- Render bundles, indirect-count draws, bindless and push constants: absent
  from wgpu.
- GLES, the web, HDR, hot reload of shaders, acquire late: deferred.
