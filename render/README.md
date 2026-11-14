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
wrong kind of format, or disagree in size, sample count or resolve. The
harness's `Frames` reads each frame back as a `render.Inspection`: its lists
by order and their passes with labels, targets, loads, stores and resolves.
A task reaches render's assets through the `Inventory` parameter, which locks
render's asset table, gfx's `Device` and platform's `FS`, all for read.
`AcquireTexture(path, TextureOptions{ColorSpace, Mipmaps})` makes a PNG or
JPEG texture from its file's header in the caller's job, shared and counted
by path and options; render's decode-and-write job writes its body before
gfx submits the frame, beside the recording for loads pending since the last
frame and after it for loads acquired while it recorded. `ReleaseTexture`
uncounts it, and the last release frees it at the end of that frame unless
it is acquired again by then. A file that fails to load is reported once and
draws magenta, one texel for a failed header and at its size for a failed
body.
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
  **`DepthStencilTarget`**.
- **`Inspection`** is a frame written down in render's terms: its command
  lists by order, their passes with their targets.
- **`Report`** is a mistake render finds at run time, logged once and
  carried past; its `ReportKind` names it for tests.
- **`rendertest.Harness`** spawns platform on headless windows, metronome
  with a render track on a `ManualClock`, gfx on its recording adapter,
  render and the test's own plugins, through the `Supervisor`. Its GPU
  finishes a beat's work when the next beat begins, as a GPU a frame behind
  would. `Step` draws one beat, `OpenWindow` opens a window,
  `LengthenNextAcquire` makes an acquire take longer, `ExpectReport`
  names a report kind the test expects, `Reports` answers the reports
  logged, and `Frames` reads each frame back as an `Inspection`.

## Kernel surface

- **Resources it installs:**
  - `Handlers`: `RecordFrame` and `EndFrame`, to order handlers around.
  - a private frame state, the state of the frames in flight, holding
    `Config`'s counts, the gfx frame being recorded and the pool of
    temporary textures.
  - a private asset table, every asset render holds: so far, its textures,
    each with the views passes drew into.
- **Resources it reads from other plugins:** `gfx.Device`, for read, in
  render's own jobs and through its `Frame` parameter.
- **Events it publishes:** `FrameEvent`, inside its `gfx.DrawEvent`
  handler.
- **Events it subscribes to:** `gfx.DrawEvent` (`Handlers.RecordFrame`),
  which, after gfx's acquire, keeps the draw event's frame as the one being
  recorded in a job that is `FrameEvent`'s `Ready`, publishes `FrameEvent`,
  and returns its out dependency joined with that job, so gfx's flush waits
  for the users' recording; and `gfx.RenderEvent` (`Handlers.EndFrame`),
  whose job, after gfx's present, takes back the temporary textures and
  frees those gone unused, and is the beat's out dependency, so the next
  beat starts after it.
- **Task parameters it offers:** `Frame`, locking the frame state, the
  asset table and `gfx.Device`, all for read.
- **Sockets it declares:** none.
- **Sockets it binds:** none.

render is listed after gfx and platform, and a plugin that draws is listed
after render.

See [docs/design.md](docs/design.md) for the design render is built to, and
[CONTEXT.md](CONTEXT.md) for its language.
