# render

`github.com/dvoyni/cog/render` is the immediate-mode rendering API between
gfx and the plugins that draw: drawbridge, 2D, a debug renderer. It is its
own Go module (Go 1.27) and depends on nothing but [kernel](../kernel),
[metronome](../metronome), [platform](../platform) and [gfx](../gfx). Its
users never reach gfx. It knows nothing of cameras, lights, culling or the
ECS.

Status: being built. render's plugin publishes `FrameEvent` inside each of
gfx's `DrawEvent`s, carrying a `Ready` after gfx's acquire and gfx's
`AcquireWait` and `GPULatency`,
and gfx flushes the frame only after the recording tasks its users join into
the event's out dependency. `Config` sets how long pooled things stay unused
before they are freed and the arena's chunk size, zero meaning 10 frames and
1 MiB. A `Report` carries its kind. `rendertest`'s harness runs render in a
real kernel on gfx's recording adapter, its GPU a frame behind, beat by
beat, and fails a test on any report it didn't name with `ExpectReport`.
A recording task reaches the frame through the `Frame` parameter, which
locks render's frame state and asset table and gfx's `Device`, all for read.
`BeginCommandList(label, order)` takes the list's gfx frame encoder at its
order, refusing and reporting a taken order, render's own and a list begun
while no frame records. `AcquireTemporaryTexture(descriptor)` hands out a
texture from a pool by descriptor, with every usage its format allows, held
until the frame ends; `EndFrame`, after gfx's present, takes the temporaries
back and frees those unused for `TemporaryIdleFrames` frames. `BeginPass`
begins a pass into colour targets, each a window or a texture at a mip level
and layer, cleared or kept, stored or discarded, and resolved into a window
or texture when multisampled, and a depth-stencil target, which may be
read-only. A pass is skipped silently when a window has no framebuffer, and
refused and reported when its targets are none, more than the device allows
(8 by default), name neither or both of a window and a texture, name a
texture render does not hold or a mip level or layer it lacks, are of the
wrong kind of format, or disagree in size, sample count or resolve. After
the users' recording, before gfx's flush, render reports once each pass
that clears a target an earlier pass of the frame drew into, in
submit order, naming both passes' labels and their lists' orders. The
harness's `Frames` reads each frame back as a `render.Inspection`: its lists
by order and their passes with labels, targets, loads, stores and resolves.
A task reaches render's assets through the `Inventory` parameter, which locks
render's asset table, gfx's `Device` and platform's `FS`, all for read.
`AcquireTexture(path, TextureOptions{ColorSpace, Mipmaps})` makes a PNG or
JPEG texture from its file's header in the caller's job, shared and counted
by path and options; render's decode-and-write job writes its body before
gfx submits the frame, beside the recording for loads pending since the last
frame and after it for loads acquired while it recorded. A texture acquired
with `Mipmaps` gets every level below the first drawn, layer by layer, from
the level above it by render's mip shader, `cog.render/mipshader.wgsl`
among render's built-in assets, which render mounts under `cog.render` at
the lowest priority so a game's own file of that name overrides it, in
render's own command list at
`math.MinInt32`, which gfx submits before every user's, in the frame its body
is written. `ReleaseTexture`
uncounts it, and the last release frees it at the end of that frame unless
it is acquired again by then. A file that fails to load is reported once and
draws magenta, one texel for a failed header and at its size for a failed
body. `AcquireBuffer(path, usage)` makes a buffer of the file's size,
rounded up to a multiple of 4, with the `BufferUsageFlags` its caller
declares and the copy destination render fills it through, shared and
counted by path and usage, its bytes written by the same jobs; a file that
fails to load is reported once. `MakeBuffer[T](label, usage, data)` and
`MakeTexture[T](descriptor, data)` make a buffer or texture filled from a
slice at once, a made texture with every usage its format allows, each given
back once with `ReleaseBuffer` or `ReleaseTexture` and freed at the end of
that frame. `WriteBuffer[T](buffer, offset, data)` and
`WriteTexture[T](texture, TextureRegion, data)`, the latter a region of one
mip level and one layer, land before every command list of gfx's next
submit, so two writes to one region in a frame leave the last; a write that
does not fit is refused and reported. `AcquireShader(path, defines...)`
compiles a variant in the caller's job, shared and counted by path and its
`ShaderDefine`s sorted by name, and gives it a layout for each group up to
the highest its bindings declare, every declared binding in it, an unused
group's empty, uniform blocks of `@group(0)` and `@group(2)` and storage
buffers of `@group(2)` at dynamic offsets; identical layouts are shared
between shaders. A file that fails to preprocess or compile is reported
once, and one declaring `@group(3)` or past the device's limits is refused
and reported once; `ReleaseShader` frees it as `ReleaseTexture` frees a
texture. `MakeMesh(MeshDescriptor)` makes a mesh of vertex buffers, each a
`BufferRange` with its `VertexLayout`, an optional `IndexBuffer` and a
`PrimitiveTopology`; `MakeMaterial(MaterialDescriptor{Label, Shader, State,
VertexEntryPoint, FragmentEntryPoint})` a material of a shader, its
`State`, whose zero value is WebGPU's default pipeline state, and its
entry points, an empty one naming the shader's only function of its stage,
a missing or ambiguous one refused and reported once. A pass's
`SetMaterial` selects a material, and `Draw(mesh, DrawArgs{InstanceCount,
FirstInstance})` draws the whole mesh with a pipeline shared by the
variant, entry points, render state, vertex layouts, the pass's formats and
sample count, and the topology, made by its first draw in the recording
job and freed with its shader; a pipeline that fails is reported once and
its draws skipped until then, and a draw naming a failed or freed buffer,
shader, mesh or material is skipped and reported once. A shader with no
`@fragment` draws only into passes without colour targets. Each mesh and
material is given back once with `ReleaseMesh` or `ReleaseMaterial`. A
pass's `SetInstanceBuffer(slot, InstanceBuffer{Range, Layout})` sets a
vertex buffer stepped per instance, bound after the mesh's in slot order
until set again, `SetMaterial` or the pass's end, its layout joining the
pipeline's key; one past slot 7 or misaligned is refused and reported.
`DrawVertices(topology, vertexCount, DrawArgs)` draws with no mesh, binding
no per-vertex buffer, a shader reading a per-vertex input refused as a
failed pipeline. `SetViewport`, `SetScissorRectangle`, clamped to the
targets, `SetStencilReference` and `SetBlendConstant` last until set again
or the pass's end, a pass beginning with the whole target; a viewport
WebGPU refuses is refused and reported. A list's
`AcquireTemporaryMesh(MeshDescriptor)` answers a mesh valid until the frame
ends, as a Dear ImGui-style UI's commands need, over sub-ranges of the
frame's vertices and indices, its layouts keying pipelines by value as a
made mesh's do, allocating nothing once warm; a draw of it after its frame
is skipped and reported once.
`SetMaterialTexture`, `SetMaterialTextureView`, `SetMaterialSampler`,
`SetMaterialBuffer` and `SetMaterialUniform[T]` bind a material's values to
its shader's `@group(1)` bindings by name, checked when set: a name the
shader lacks is ignored, and a value that cannot fill its binding (another
kind, a Go struct of another size or member offsets, a view of another
dimension or sample type, mips or layers its texture lacks, a buffer too
small) is reported once per shader and name and draws the binding's
default. Unbound bindings draw render's defaults, white, depth 1.0,
integer zeros, a linear sampler clamping to the edge, a less-equal
comparison sampler and zeros of a block's size, but for a storage binding
a running function reads, which skips the draw; a texture a pass writes,
bound in it, draws its default, reported once; a texture or buffer freed
under a material unbinds at the end of that frame. Gfx views are interned
by texture and view, and samplers shared by value. A draw uses the values
its material holds when it is recorded: a change after the material's
first draw in the frame makes a version, a new bind group with its uniform
blocks copied into the arena, freed at the frame's end, while one before
it makes none; a change made by one task while another records may not
show in that task's draws. A pass's
`SetPassTexture`, `SetPassTextureView`, `SetPassSampler`, `SetPassBuffer`
and `SetPassUniform[T]` bind its parameters of `@group(0)`, read by every
draw after them in the pass, and `SetTexture`, `SetTextureView`,
`SetSampler`, `SetBuffer` and `SetUniform[T]` its draw parameters of
`@group(2)`, read by the draws after them until set again, `SetMaterial` or
the pass's end; each is checked against the draw's shader at its next draw,
refused and caught as a material's values are. Their uniform blocks, and
draw parameters' storage buffers, bind at dynamic offsets, so draws rebind
one bind group at new offsets; bind groups are shared by what they bind and
freed after `TemporaryIdleFrames` unused or at the end of the frame an asset
they name is freed, but for a temporary buffer as a pass's storage
parameter, which costs the pass a bind group in each frame its range moves. A list's
`AcquireTemporaryBuffer[T](data)` answers a 256-byte-aligned range of the
temporary arena holding `data`, valid until the frame ends: each list takes
chunks of `TemporaryChunkSize` of its own, or one of a block's size rounded
up to a multiple of it where the block is larger, hands each to gfx in one
write at its `End`, and chunks unused for `TemporaryIdleFrames` frames are
freed.
A list's `BeginComputePass(label)` begins a compute pass, whose
`SetShader(shader, entryPoint)` selects a compute function, an empty name
its only one, and resets its parameters; its `SetTexture`,
`SetTextureView`, `SetSampler`, `SetBuffer` and `SetUniform[T]` bind
parameters by name in any group, uniform blocks from the arena, bind groups
shared by what they bind; and `Dispatch(x, y, z)` runs it, with a pipeline
keyed by variant and entry point, a missing or ambiguous entry point
reported once and its dispatches skipped. A list's `CopyBuffer`,
`CopyTexture`, `CopyBufferToTexture`, `CopyTextureToBuffer` and
`ClearBuffer` record at their point among its passes, rows between a
buffer and a texture 256 bytes apart; one that does not fit is refused and
reported, recording nothing.
`Frame`'s `RequestCapture(window)` asks for a window's picture as the
first frame acquired after it draws it, answering `ErrCaptureBusy` while
one is in flight, `ErrCaptureUnsupported` for a window that cannot be
captured and `ErrDeviceLost` while the device is lost; render publishes `CapturedEvent{Window, Beat, Status}` inside
gfx's, and `ReadCapture(event)` answers the picture while that
publication's jobs run.
A list's `ReadBuffer(range)` and `ReadTexture(texture, region)` record a
copy at their point among its passes into a staging buffer from render's
pool, its size rounded up to a quarter of a power of two and at least 256
bytes, and answer a `Readback`; at the
frame's end render maps the buffer, and once gfx has mapped it publishes
`ReadbackEvent{Readback, Status}`, and `Frame`'s `ReadbackData(event)`
answers the bytes, a texture's rows 256 bytes apart, while that
publication's jobs run, after which the buffer goes back to the pool;
staging buffers unused for `TemporaryIdleFrames` frames are freed.
A task holding the `Surface` parameter, which locks gfx's `Device` for
write alone, changes every window's surface with
`Request(SurfaceRequest{Format, PresentMode})`, configured before the next
frame acquires; the request at start stays gfx's `Config.Surface`.
Once gfx has recovered a lost device, render writes every loaded texture
and buffer again from its file under the same handle, requesting the
textures' mips for the next frame, fills its default textures and every
material's uniform blocks again, and then publishes `RecoveredEvent`, on
which makers fill what they made from a slice again; a loaded asset whose
file is gone, as memory-mount bytes since removed, is reported.
`Frame.RequestInspection()` asks for an inspection of the next frame, one
at a time, a request while one is pending joining it: while that frame
records, render's calls write it down as `rendertest`'s reader reads a
frame back, with what only render knows beside it, and `InspectedEvent`
delivers it after the present, read with `Frame.ReadInspection` while its
jobs run; every other frame pays one check a call and allocates nothing for
it. Each command list's gfx encoder is labelled with the list's label.
What remains is on the [render map](https://github.com/dvoyni/cog/issues/800).

## Shape

- **`FrameEvent`** is the frame render's users record, published every beat
  inside gfx's draw event. A plugin that draws subscribes to it, schedules
  its recording tasks after `Ready`, and joins them into the out dependency
  it returns.
- **`Config`** is render's part of the kernel's config: `TemporaryIdleFrames`
  and `TemporaryChunkSize`.
- **`Frame`** is the task parameter a recording task reaches the frame
  through. **`AcquireTemporaryTexture(TextureDescriptor)`** hands out a
  temporary **`Texture`**, a pointer-free handle, of a **`TextureFormat`**.
  **`BeginCommandList(label, order)`** begins a **`CommandList`** at an
  **`Order`**, and its **`BeginPass(PassDescriptor)`** a **`Pass`** into up
  to 8 **`ColorTarget`**s, each a window or a texture at a mip level and
  layer with an optional **`ResolveTarget`**, and a
  **`DepthStencilTarget`**; its **`AcquireTemporaryBuffer[T]`** answers a
  **`BufferRange`** of the temporary arena; its **`BeginComputePass`** a
  **`ComputePass`** of dispatches; its copies copy buffer ranges and
  texture regions; and its readbacks read them back, each answering a
  **`Readback`**.
- **`Inventory`** is the task parameter a task reaches render's assets
  through: it acquires textures and **`Buffer`**s from files, makes them
  from slices, writes them, a texture by **`TextureRegion`**, and releases
  them, and acquires and releases **`Shader`**s, each a variant of a file
  with its **`ShaderDefine`**s.
- **`Surface`** is the task parameter a task changes the windows' surfaces
  through, with a **`SurfaceRequest`** of a `TextureFormat` and a
  **`PresentMode`**.
- **`Mesh`** and **`Material`** are made from a **`MeshDescriptor`** and a
  **`MaterialDescriptor`**, its **`State`** blending, masking,
  testing and culling as WebGPU's defaults when zero; a pass's
  **`SetMaterial`** and **`Draw`** draw a mesh with a material, or
  **`DrawVertices`** a count of vertices with none, reading the
  **`InstanceBuffer`**s set; its `SetPass…` setters bind pass parameters,
  its `Set…` setters draw parameters, and its **`Viewport`**,
  **`ScissorRectangle`**, stencil reference and blend constant change
  between draws. A list's **`AcquireTemporaryMesh`** answers a mesh for one
  frame. A material's, a pass's or a draw's parameters are a texture or a **`TextureView`** of one, seen
  in a **`TextureViewDimension`** through a **`TextureAspect`**, a
  **`SamplerDescriptor`** of **`AddressMode`**s and **`FilterMode`**s, a
  **`BufferRange`**, or a Go value of a uniform block.
- **`Inspection`** is a frame written down in render's terms: its beat; its
  command lists by order, each with its label, their passes with their
  targets and their draws, each an **`InspectedDraw`** with its pass
  parameters, its **`InspectedMaterial`** and its draw parameters, each an
  **`InspectedParameter`** by name, group and binding number, its instance
  buffers and counts; their compute passes, each an
  **`InspectedComputePass`** of **`InspectedDispatch`**es with their
  shader, function, parameters and workgroup counts; their copies, each an
  **`InspectedCopy`**, and their readbacks, each an
  **`InspectedReadback`**; each pass, compute pass, copy and readback with
  its position in its list. Beside them: the textures, buffers and shaders
  the frame made, each an **`InspectedAsset`**, and the writes into them,
  each an **`InspectedWrite`**; the temporaries it took from the pools and
  the materials it versioned; the pipelines it made, each an
  **`InspectedPipeline`**, and the bind groups, each an
  **`InspectedBindGroup`** of a **`BindGroupKind`**; and its reports.
  `Frame.RequestInspection` asks for one and **`InspectedEvent`** delivers
  it, read with `Frame.ReadInspection` in the tasks its handler joins.
- **`RecoveredEvent`** says gfx has recovered a lost device and render
  has written its own assets again: its handler's tasks fill the buffers
  and textures their plugin made from a slice again.
- **`CapturedEvent`** delivers a window's capture with its
  **`CaptureStatus`**, its picture read with `Frame.ReadCapture` in the
  tasks its handler joins.
- **`ReadbackEvent`** delivers a readback with its **`ReadbackStatus`**,
  its bytes read with `Frame.ReadbackData` in the tasks its handler joins.
- **`Report`** is a mistake render finds at run time, logged once and
  carried past; its `ReportKind` names it for tests.
- **`rendertest.Harness`** spawns platform on headless windows, metronome
  with a render track on a `ManualClock`, gfx on its recording adapter,
  render and the test's own plugins, through the `Supervisor`. Its GPU
  finishes a beat's work when the next beat begins, as a GPU a frame behind
  would, and it answers a shader's reflection from the binding declaration
  beside it, `lit.wgsl.json` beside `lit.wgsl`, failing the test for a
  shader compiled with none; its package documentation gives the format.
  `cmd/declarebindings` generates a declaration from the shader on wgpu's
  software adapter, with a section for each `-variant`, run from
  `//go:generate`; render's mip shader has its own beside it in `assets`,
  generated so from `assets.go`. A declaration holds the shader's bindings,
its entry points with their stages, and each vertex entry point's inputs.
`Step` draws one beat, `OpenWindow` opens a window,
  `LengthenNextAcquire` makes an acquire take longer, `ExpectReport`
  names a report kind the test expects, `Reports` answers the reports
  logged, and `Frames` reads each frame back as an `Inspection`, with the
  beat of the `Step` that drew it and the reports logged during that `Step`;
  what gfx's recording does not show, the temporaries, the versioned
  materials and each draw's material handle, it leaves empty.

## Kernel surface

- **Resources it installs:**
  - `Handlers`: `RecordFrame`, `EndFrame`, `ReloadAssets`,
    `DeliverCapture` and `DeliverReadback`, to order handlers around.
  - a private frame state, the state of the frames in flight, holding
    `Config`'s counts, the gfx frame being recorded, the pool of
    temporary textures, the temporary arena's chunks, what each command
    list keeps while it records, the staging pool with the readbacks in
    flight, and the inspection asked for, from its request to its
    delivery.
  - a private asset table, every asset render holds: so far, its textures,
    each with the views passes drew into, its buffers, and its shaders,
    with the layouts they share.
- **Resources it reads from other plugins:** `gfx.Device`, for read, in
  render's own jobs and through its `Frame` and `Inventory` parameters, and
  for write through its `Surface` parameter; `platform.FS`, for read,
  through `Inventory`.
- **Events it publishes:** `FrameEvent`, inside its `gfx.DrawEvent`
  handler; `RecoveredEvent`, inside its `gfx.DeviceRecoveredEvent`
  handler; `CapturedEvent`, inside its `gfx.CapturedEvent` handler;
  `ReadbackEvent`, inside its handler of a private event the job its
  `gfx.BufferMappedEvent` handler schedules publishes; and
  `InspectedEvent`, inside its handler of a private event its
  `gfx.RenderEvent` handler's job publishes once the frame inspected ends.
- **Events it subscribes to:** `gfx.DrawEvent` (`Handlers.RecordFrame`),
  which, after gfx's acquire, keeps the draw event's frame as the one being
  recorded, beginning its inspection when one is requested, in a job that
  is `FrameEvent`'s `Ready`, publishes `FrameEvent`,
  and returns its out dependency joined with that job, so gfx's flush waits
  for the users' recording; and `gfx.RenderEvent` (`Handlers.EndFrame`),
  whose job, after gfx's present, takes back the temporary textures and the
  arena's chunks and frees those gone unused, with the bind groups of pass
  and draw parameters gone unused, and the staging buffers, after mapping
  those of the frame's readbacks, finishes the frame's inspection, if any,
  and is the beat's out dependency, so the next beat starts after it;
  `gfx.DeviceRecoveredEvent`
  (`Handlers.ReloadAssets`), whose job writes the loaded textures and
  buffers, the default textures and the materials' uniform blocks again,
  and which publishes `RecoveredEvent` after that job and returns its out
  dependency; `gfx.CapturedEvent`
  (`Handlers.DeliverCapture`), which publishes `CapturedEvent` and returns
  its out dependency, so gfx frees the picture after the tasks its users
  join into it; and `gfx.BufferMappedEvent` (`Handlers.DeliverReadback`),
  whose job finds the readback whose staging buffer gfx mapped and
  publishes it on a private event, whose handler publishes `ReadbackEvent`
  and unmaps the buffer after its out dependency, so it goes back to the
  pool after the tasks its users join into it. Its handler of the private
  event delivering an inspection publishes `InspectedEvent` and drops the
  inspection after its out dependency.
- **Task parameters it offers:** `Frame`, locking the frame state, the
  asset table and `gfx.Device`, all for read; and `Inventory`, locking the
  asset table, `gfx.Device` and `platform.FS`, all for read; and `Surface`,
  locking `gfx.Device` alone, for write.
- **Sockets it declares:** none.
- **Sockets it binds:** `platform.ReadMountSocket`, its built-in assets
  under the prefix `cog.render`, at the lowest priority.

render is listed after gfx and platform, and a plugin that draws is listed
after render.

See [docs/design.md](docs/design.md) for the design render is built to, and
[CONTEXT.md](CONTEXT.md) for its language.
