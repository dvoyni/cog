# canvas

`github.com/dvoyni/cog/plugins/canvas` is the plugin on [render](../render)
that draws 2D: sprites, shapes, text and meshes, recorded as entries every
frame and drawn through 2D materials. It is its own Go module (Go 1.27),
importing [kernel](../../kernel), [metronome](../metronome),
[platform](../platform), render and [libs/m](../../libs/m), and reaches no
gfx.

It is being built from [docs/design.md](docs/design.md). So far a frame's
recording tasks record shapes into draw lists of their own, which canvas
merges in registration order and forgets; nothing is drawn yet.

## Shape

- **`Plugin`**, from `NewPlugin()`, is canvas. **`Config`** is its part of
  the kernel's config, under `PluginName`; it has no field yet.
- **`FrameEvent`** is the frame its users record, published inside each of
  render's `FrameEvent`s. A plugin that records schedules its recording tasks
  after its `Ready` and joins them into the out dependency it returns.
- **`DrawList`** is the task parameter a recording task records through,
  each task's a draw list of its own. An entry recorded outside the frame is
  refused and reported once for the task.
- **`Shape`** is an entry filling a rectangle.
- **`Report`** is a mistake canvas finds at run time, logged once and carried
  past. Its `ReportKind` names it for tests.
- **`canvastest`** runs canvas in a real kernel on render's fake GPU, with a
  recording task of its own and canvas's reports expected by kind.

## Kernel surface

- **Resources it installs:** `Handlers` (`RecordFrame`), to order handlers
  around; a private draw state, holding each task's draw list.
- **Resources it reads from other plugins:** render's, for read through
  `render.Frame`, in the task that draws the frame.
- **Task parameters it offers:** `DrawList`.
- **Events it publishes:** `FrameEvent`, inside its handler of
  `render.FrameEvent`.
- **Events it subscribes to:** `render.FrameEvent`.
- **Sockets it declares:** none.
- **Sockets it binds:** none.

canvas is listed after render and platform, and a plugin that records into
canvas is listed after canvas.

See [docs/design.md](docs/design.md) for the design and why it is this way,
and [CONTEXT.md](CONTEXT.md) for its words.
