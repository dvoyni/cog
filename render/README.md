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
while no frame records. `BeginPass` begins a pass into windows, each target
cleared or kept, stored or discarded, skipped silently when a window has no
framebuffer and refused and reported when its targets are none, more than
the device allows (8 by default),
nameless or of different sizes. The harness's `Frames` reads each frame
back as a `render.Inspection`: its lists by order and their passes with
labels, targets, loads and stores.
What remains is on the [render map](https://github.com/dvoyni/cog/issues/800).

## Shape

- **`FrameEvent`** is the frame render's users record, published every beat
  inside gfx's draw event. A plugin that draws subscribes to it, schedules
  its recording tasks after `Ready`, and joins them into the out dependency
  it returns.
- **`Config`** is render's part of the kernel's config: `TemporaryIdleFrames`
  and `TemporaryChunkSize`.
- **`Frame`** is the task parameter a recording task reaches the frame
  through. **`BeginCommandList(label, order)`** begins a **`CommandList`**
  at an **`Order`**, and its **`BeginPass(PassDescriptor)`** a **`Pass`**
  into up to 8 **`ColorTarget`**s, each a window.
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
  - `Handlers`: `RecordFrame`, to order handlers around.
  - a private frame state, the state of the frames in flight, holding
    `Config`'s counts and the gfx frame being recorded.
  - a private asset table, every asset render holds, empty until render
    holds assets.
- **Resources it reads from other plugins:** none.
- **Events it publishes:** `FrameEvent`, inside its `gfx.DrawEvent`
  handler.
- **Events it subscribes to:** `gfx.DrawEvent` (`Handlers.RecordFrame`),
  which, after gfx's acquire, keeps the draw event's frame as the one being
  recorded in a job that is `FrameEvent`'s `Ready`, publishes `FrameEvent`,
  and returns its out dependency joined with that job, so gfx's flush waits
  for the users' recording.
- **Task parameters it offers:** `Frame`, locking the frame state, the
  asset table and `gfx.Device`, all for read.
- **Sockets it declares:** none.
- **Sockets it binds:** none.

render is listed after gfx and platform, and a plugin that draws is listed
after render.

See [docs/design.md](docs/design.md) for the design render is built to, and
[CONTEXT.md](CONTEXT.md) for its language.
