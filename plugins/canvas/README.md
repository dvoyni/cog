# canvas

`github.com/dvoyni/cog/plugins/canvas` is the plugin on [render](../render)
that draws 2D: sprites, shapes, text and meshes, recorded as entries every
frame and drawn through 2D materials. It is its own Go module (Go 1.27),
importing [kernel](../../kernel), [metronome](../metronome),
[platform](../platform), render and [libs/m](../../libs/m), and reaches no
gfx.

It is being built from [docs/design.md](docs/design.md). So far a frame's
recording tasks record fills into draw lists of their own, which canvas
merges in registration order and draws through the default camera into the
main window, the first window opened of those still open, as one instanced
draw of its default material.

## Shape

- **`Plugin`**, from `NewPlugin()`, is canvas. **`Config`** is its part of
  the kernel's config, under `PluginName`; it has no field yet.
- **`FrameEvent`** is the frame its users record, published inside each of
  render's `FrameEvent`s. A plugin that records schedules its recording tasks
  after its `Ready` and joins them into the out dependency it returns.
- **`DrawList`** is the task parameter a recording task records through,
  each task's a draw list of its own. An entry recorded outside the frame is
  refused and reported once for the task.
- **`Shape`** is an entry filling a rectangle with its tint.
- **`cog.canvas/canvas.wgsl`** is canvas's include, declaring the camera's
  pass parameters at `@group(0)` and canvas's instance data and atlas at
  `@group(2)`, with the functions over them, which canvas's own default
  material, `cog.canvas/default.wgsl`, includes.
- **`Report`** is a mistake canvas finds at run time, logged once and carried
  past. Its `ReportKind` names it for tests.
- **`canvastest`** runs canvas in a real kernel on render's fake GPU, with a
  recording task of its own and canvas's reports expected by kind.

## Kernel surface

- **Resources it installs:** `Handlers` (`RecordFrame`, `TrackOpenedWindow`,
  `ForgetClosedWindow`), to order handlers around; a private draw state,
  holding each task's draw list; a private renderer, holding the default
  material, the white texel and the instance data; and a private list of
  the open windows.
- **Resources it reads from other plugins:** render's, for read through
  `render.Frame` and `render.Inventory`, `platform.FS`, for read through
  `render.Inventory`, as the default material's shader loads, and
  `platform.WindowManager`, for read, for the main window's size, in the
  task that draws the frame.
- **Task parameters it offers:** `DrawList`.
- **Events it publishes:** `FrameEvent`, inside its handler of
  `render.FrameEvent`.
- **Events it subscribes to:** `render.FrameEvent`, and
  `platform.WindowOpenedEvent` and `platform.WindowClosedEvent`, to know the
  main window.
- **Sockets it declares:** none.
- **Sockets it binds:** `platform.ReadMountSocket`, its built-in assets
  under the prefix `cog.canvas`, at the lowest priority.

canvas is listed after render and platform, and a plugin that records into
canvas is listed after canvas.

See [docs/design.md](docs/design.md) for the design and why it is this way,
and [CONTEXT.md](CONTEXT.md) for its words.
