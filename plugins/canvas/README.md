# canvas

`github.com/dvoyni/cog/plugins/canvas` is the plugin on [render](../render)
that draws 2D: sprites, shapes, text and meshes, recorded as entries every
frame and drawn through 2D materials. It is its own Go module (Go 1.27),
importing [kernel](../../kernel), [metronome](../metronome),
[platform](../platform), render and [libs/m](../../libs/m), and reaches no
gfx.

It is being built from [docs/design.md](docs/design.md). So far a frame's
recording tasks record fills into draw lists of their own, which canvas
merges in registration order, sorts by their sort key and draws through the
default camera into the main window, the first window opened of those still
open, an instanced draw for each batch, a run of entries of one 2D
material. A frame can be inspected in canvas's terms, beside render's
inspection of it.

## Shape

- **`Plugin`**, from `NewPlugin()`, is canvas. **`Config`** is its part of
  the kernel's config, under `PluginName`: `RegroupedLayers`, the layers
  whose entries of equal keys canvas may regroup by batch.
- **`FrameEvent`** is the frame its users record, published inside each of
  render's `FrameEvent`s. A plugin that records schedules its recording tasks
  after its `Ready` and joins them into the out dependency it returns.
- **`DrawList`** is the task parameter a recording task records through,
  each task's a draw list of its own. An entry recorded outside the frame is
  refused and reported once for the task.
- **`Shape`** is an entry filling a rectangle with its tint, drawn with its
  `Material`, at its sort key, `Layer` then `Order`. Entries are drawn by
  sort key, then by their draw list's registration order, then in the order
  recorded.
- **`OrderOf(y)`** maps a float onto an order of its own, in the same order,
  for y-sorting.
- **`Material`** names a 2D material; the zero one, the default material,
  is the only one canvas makes yet.
- **`Assets`** is the task parameter a tool reaches canvas through:
  `RequestInspection` asks for an inspection of the next frame, and
  `ReadInspection` answers it in a task off **`InspectedEvent`**.
- **`Inspection`** is one frame in canvas's terms: each camera, its batches
  with their material and their entries, each entry with its sort key and
  its draw list's place,
  and render's `Inspection` of the frame beside it.
- **`cog.canvas/canvas.wgsl`** is canvas's include, declaring the camera's
  pass parameters at `@group(0)` and canvas's instance data and atlas at
  `@group(2)`, with the functions over them, which canvas's own default
  material, `cog.canvas/default.wgsl`, includes.
- **`Report`** is a mistake canvas finds at run time, logged once and carried
  past. Its `ReportKind` names it for tests.
- **`canvastest`** runs canvas in a real kernel on render's fake GPU, with a
  recording task of its own, each frame inspected, and canvas's reports
  expected by kind.

## Kernel surface

- **Resources it installs:** `Handlers` (`RecordFrame`, `TrackOpenedWindow`,
  `ForgetClosedWindow`, `DeliverInspection`), to order handlers around; a
  private draw state, holding each task's draw list, the frame's entries
  sorted and the inspection asked for; a private renderer, holding the
  default material, the white texel, the instance data and the batches; and
  a private list of the open windows.
- **Resources it reads from other plugins:** render's, for read through
  `render.Frame` and `render.Inventory`, `Assets` holding a `render.Frame`, `platform.FS`, for read through
  `render.Inventory`, as the default material's shader loads, and
  `platform.WindowManager`, for read, for the main window's size, in the
  task that draws the frame.
- **Task parameters it offers:** `DrawList`, and `Assets`.
- **Events it publishes:** `FrameEvent`, inside its handler of
  `render.FrameEvent`; and `InspectedEvent`, once render has delivered its
  inspection of a frame canvas was asked to inspect.
- **Events it subscribes to:** `render.FrameEvent`;
  `platform.WindowOpenedEvent` and `platform.WindowClosedEvent`, to know the
  main window; and `render.InspectedEvent`, to put render's inspection
  beside canvas's.
- **Sockets it declares:** none.
- **Sockets it binds:** `platform.ReadMountSocket`, its built-in assets
  under the prefix `cog.canvas`, at the lowest priority.

canvas is listed after render and platform, and a plugin that records into
canvas is listed after canvas.

See [docs/design.md](docs/design.md) for the design and why it is this way,
and [CONTEXT.md](CONTEXT.md) for its words.
