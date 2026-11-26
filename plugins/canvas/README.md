# canvas

`github.com/dvoyni/cog/plugins/canvas` is the plugin on [render](../render)
that draws 2D: sprites, shapes, text and meshes, recorded as entries every
frame and drawn through 2D materials. It is its own Go module (Go 1.27),
importing [kernel](../../kernel), [metronome](../metronome),
[platform](../platform), render, [libs/m](../../libs/m),
[libs/meshfile](../../libs/meshfile) and go-text's
[typesetting](https://github.com/go-text/typesetting), and reaches no gfx.

## Features

- **Entries, recorded every frame.** Shapes, sprites, runs of text and meshes
  are plain values an ECS component holds, recorded singly or in bulk by
  tasks running together, each into a command list of its own, then merged,
  sorted by layer and order, and forgotten.
- **Batched for you.** Entries of one 2D material, atlas, filter and clip in
  sort order are one instanced draw, shapes with the sprites beside them, and
  every label of a frame with every other.
- **Auto-atlasing.** An image named by path loads at its first draw and is
  packed into an atlas with gutters and mips that never mix neighbours; a full
  atlas grows, then evicts the images drawn least lately. Sheets, source
  rectangles, wrapped, sliced and tiled sprites, render textures drawn as
  sprites, and converters and distance fields, each converter naming the
  format of its own atlas, which equal converters' images share.
- **A public atlas.** The `atlas` package packs images of one format into the
  pages of one 2D-array texture, as canvas packs its sprites and glyphs, for
  a plugin's own caches, such as a UI's rasterised text.
- **Every look a 2D material.** A material is a whole WGSL file over canvas's
  include, a fragment stage alone drawing sprites, shapes, text and meshes
  alike, with parameters by name and an argument of its own type for each
  entry that breaks no batch. Outlines, halos, glows and drop shadows are
  profiles of the SDF material.
- **Cameras.** Any number a frame, into windows or textures, over viewports,
  turned, zoomed, fitted to a logical size, pixel-perfect or multisampled,
  each culling what it cannot see, with conversions between a click and the
  world.
- **Text.** Font sets serving languages, shaped by go-text with bidi and
  Unicode line breaking, drawn from distance-field glyphs at any size or from
  bitmap fonts, laid out once through a layout cache, with spans, icons and
  markup.
- **Meshes.** Triangles given for one frame, and mesh files drawn instanced,
  in canvas's vertex or a layout of your own through your material's vertex
  stage.
- **The frame's other work.** Preloads and unloads, hot reload of images,
  fonts, mesh files and shaders, device recovery, and frame inspections for
  agents.
- **Tested without a GPU.** `canvastest` runs canvas in a real kernel on
  render's fake GPU and reads each frame back in canvas's words, or draws on
  wgpu's software adapter for pixel tests.
- **Zero allocations per frame** once warm, which the tests assert, but for a
  first use, a layout the cache has not seen, and a frame's refusals.

## Shape

- **`Plugin`**, from `NewPlugin()`, is canvas. **`Config`** is its part of the
  kernel's config: its atlases, mips, filters, text and default font set.
- **`FrameEvent`** is the frame its users record, published inside each of
  render's `FrameEvent`s. A plugin that records schedules its recording tasks
  after its `Ready` and joins them into the out dependency it returns.
- Two **task parameters**: **`CommandList`**, each task's own, records entries
  and sets cameras, and **`Assets`** reaches materials, preloads, unloads,
  image sizes, hot reload and inspections. Two **resources**:
  **`FontLibrary`**, whose writing side `ElevateFontLibrary` answers, holds
  the font sets and lays text out, and **`Targets`** each target as it was
  last drawn.
- **`Shape`**, **`Sprite`**, **`Text`** and **`Mesh`** are the entries, with
  **`Triangles`** for a mesh of one frame. A **`Camera`** is set for one
  frame.
- **`Image`**, **`MeshFile`**, **`Material`** and **`FontSet`** are
  pointer-free names an ECS component can hold.
- **`Inspection`** is one frame in canvas's words. **`Report`** is a mistake
  canvas finds at run time, logged once and carried past, its `ReportKind`
  naming it for tests.
- **`atlas.Atlas`**, in the `atlas` subpackage, is one atlas of one format,
  made with `atlas.New` over render's `Inventory`: it packs, grows and
  records its copies and mips, and leaves what each region holds to its
  user.
- **`font.SetDescriptor`**, in the `font` subpackage, is what a font set is
  made of: a default `font.Font` and the fonts serving each language, each a
  font file and its options. A font's measures are `font.Metrics`.

## Kernel surface

- **Resources it installs:** `Handlers` (`RecordFrame`, `Recover`,
  `TrackOpenedWindow`, `ForgetClosedWindow`, `DeliverInspection`), to order
  handlers around; `Targets`, written by canvas's own task after each frame's
  drawing; `FontLibrary`, written only through `ElevateFontLibrary` and by
  canvas's own tasks making the default set and reloading font files; a
  private draw state, a private renderer and a private list of the open
  windows.
- **Resources it reads from other plugins:** render's, for read through
  `render.Frame` and `render.Inventory`; `platform.FS`, for read, as images,
  fonts and mesh files load; `platform.WindowManager`, for read, for a
  window's size; and metronome's `Clock`, in `Init`, to time its sorting and
  drawing.
- **Task parameters it offers:** `CommandList` and `Assets`.
- **Events it publishes:** `FrameEvent`, inside its handler of
  `render.FrameEvent`; and `InspectedEvent`, once render has delivered its
  inspection of a frame canvas was asked to inspect.
- **Events it subscribes to:** `render.FrameEvent`, `render.RecoveredEvent`,
  `render.InspectedEvent`, `platform.WindowOpenedEvent` and
  `platform.WindowClosedEvent`.
- **Sockets it declares:** none.
- **Sockets it binds:** `platform.ReadMountSocket`, twice, its built-in
  assets under the prefix `cog.canvas`, at the lowest priority: its include
  and its materials' shaders, then the font package's JetBrains Mono and its
  licence.

canvas is listed after render, platform and metronome, and a plugin that
records into canvas is listed after canvas.

See [docs/README.md](docs/README.md) for the guide with examples,
[docs/design.md](docs/design.md) for the design and why it is this way, and
[CONTEXT.md](CONTEXT.md) for its words.
