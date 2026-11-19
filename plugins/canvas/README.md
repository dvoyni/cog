# canvas

`github.com/dvoyni/cog/plugins/canvas` is the plugin on [render](../render)
that draws 2D: sprites, shapes, text and meshes, recorded as entries every
frame and drawn through 2D materials. It is its own Go module (Go 1.27),
importing [kernel](../../kernel), [metronome](../metronome),
[platform](../platform), render and [libs/m](../../libs/m), and reaches no
gfx.

It is being built from [docs/design.md](docs/design.md). So far a frame's
recording tasks record shapes and sprites and set cameras into draw lists
of their own, which canvas merges in registration order, sorts by their
sort key and draws through each camera, or the default camera into the main
window, the first window opened of those still open, an instanced draw for
each batch, a run of entries of one 2D material, one atlas, one filter and
one clip, each camera culling the entries outside its view. A sprite's
image is packed at its first draw into the sprite atlas of its colour
space, one 2D-array texture of pages holding its images premultiplied, so
sprites of different images and the shapes beside them batch. A frame can
be inspected in canvas's terms, beside render's inspection of it.

## Shape

- **`Plugin`**, from `NewPlugin()`, is canvas. **`Config`** is its part of
  the kernel's config, under `PluginName`: `RegroupedLayers`, the layers
  whose entries of equal keys canvas may regroup by batch,
  `DefaultFilter`, the filter of sprites naming none, and `AtlasPageSize`,
  the side of a sprite atlas's pages, zero meaning 2048.
- **`FrameEvent`** is the frame its users record, published inside each of
  render's `FrameEvent`s. A plugin that records schedules its recording tasks
  after its `Ready` and joins them into the out dependency it returns.
- **`DrawList`** is the task parameter a recording task records through,
  each task's a draw list of its own. An entry recorded or a camera set
  outside the frame is refused and reported once for the task.
- **`Camera`**, set for one frame with `SetCamera`, draws the entries of its
  `Layers` (`MakeLayer(layer)` for one, `MakeLayers(ranges...)` for up to
  four ranges) into its target, a `Window` or a `Texture`, the main window
  when it names neither, over its `Viewport`, in fractions of the target,
  cleared there alone when it sets `Clear`. Its `Pivot` of the viewport
  shows its `Position`, turned by its `Rotation` and scaled by its `Zoom`;
  its logical `Size` is fitted into the viewport by its `Fit`,
  `FitExpand`, `FitInscribe` (with bars), `FitOverlap` or `FitStretch`.
  A `PixelPerfect` camera, for pixel art, draws a whole number of pixels a
  unit, its entries snapped to the world-unit grid and its view placed on
  whole pixels. A camera of a `SampleCount` of 4 smooths its entries' edges,
  drawing them into a multisampled texture of its viewport, resolved and
  drawn over the viewport, so the rest of its target survives.
  `ScreenToWorld`, `WorldToScreen` and `VisibleRect` convert through the
  camera as its target was last drawn, read from **`Targets`**.
  canvas begins one command list for each distinct `Order`, its cameras
  into textures first. A frame that sets no camera is seen through the
  default one, which draws only a frame with entries: every layer, the
  whole main window, cleared to opaque black, at order 0.
- **`Shape`** is an entry that fills or strokes a rectangle, or draws a
  line, its `Kind` `Fill`, `Stroke` or `Line`, in its tint, drawn with its
  `Material`, at its sort key, `Layer` then `Order`. A stroke is four fills
  that do not overlap, so a translucent one does not darken its corners; a
  zero `Width` is a hairline one target pixel wide; `Radius` rounds a fill's
  or a stroke's corners; `Clip`, a rectangle of the world, is a scissor
  through each camera, its bounding box under a turned one. Each camera
  culls the entries whose bounds miss its `VisibleRect`, or their clip's
  part of it. Entries are drawn by
  sort key, then by their draw list's registration order, then in the order
  recorded.
- **`Sprite`** is an entry that draws an `Image` through its `m.Mat2x3`
  `Transform` about its image's pivot, mirrored by a negative scale, at its
  `Size`, zero its image's in texels and one zero axis keeping the aspect,
  times its `Tint`, white when zero, with its `Crop` left out where it lies,
  read through its `Filter`, `FilterLinear` or `FilterNearest`, zero
  meaning `Config.DefaultFilter`, and drawn with its `Material` and its
  `Clip`, at its sort key, as a shape is. `DrawSprite` records one, and
  `DrawSprites` a slice, as an ECS chunk's column. Sprites of any images of
  one atlas batch together while their filter is one, and the shapes beside
  them with them, since shapes, and the zero `Image`, draw the white texel
  of the sRGB sprite atlas, which takes no filter.
- **`ImageFromFile(path, options…)`** interns a path in platform's FS and its
  **`ImageOptions`** (its `ColorSpace`, `ColorSpaceSrgb` or
  `ColorSpaceLinear`, and its `Pivot`, a `Fraction` of its size from its
  centre plus `Texels`, or one of the named pivots, `PivotCenter`,
  `PivotTopLeft`, `PivotTop`, `PivotBottom`, `PivotBottomLeft` and
  `PivotBottomRight`) into an `Image`, a plain value an ECS component holds.
  It loads nothing: the first frame drawing it acquires its file from
  render and packs it into the sprite atlas of its colour space, each image
  a cell of its own whose gutter of 2 texels continues its edges, drawn in
  that frame, and a file render cannot load draws render's magenta,
  reported once by render. An image no page holds is packed into an atlas
  of its own, exactly its size with no gutter, so an image as large as a
  page takes a texture of a page's size. Images differing only in their pivot share their region. A
  second set of options is ignored and reported once.
- **`OrderOf(y)`** maps a float onto an order of its own, in the same order,
  for y-sorting.
- **`Material`** names a 2D material; the zero one, the default material,
  is the only one canvas makes yet.
- **`Assets`** is the task parameter a tool reaches canvas through:
  `RequestInspection` asks for an inspection of the next frame, and
  `ReadInspection` answers it in a task off **`InspectedEvent`**.
- **`Inspection`** is one frame in canvas's terms: each camera that drew,
  the window it drew into and its viewport in pixels, its batches with their
  material, their atlas, their filter, their clip and the entries it did not
  cull, each entry a shape or a sprite, with its sort key and its draw
  list's place, and render's `Inspection` of the frame beside it.
- **`cog.canvas/canvas.wgsl`** is canvas's include, declaring the camera's
  pass parameters at `@group(0)` and canvas's instance data and atlas at
  `@group(2)`, with the functions over them, `canvas_sample` answering an
  entry's image premultiplied, as the atlas holds it, which canvas's own
  default material, `cog.canvas/default.wgsl`, includes, as does
  `cog.canvas/viewport.wgsl`, the shader of the quads canvas draws over a
  viewport: a camera's clear, what a multisampled camera resolved, and an
  image premultiplied into its cell of an atlas.
- **`Report`** is a mistake canvas finds at run time, logged once and carried
  past. Its `ReportKind` names it for tests.
- **`canvastest`** runs canvas in a real kernel on render's fake GPU, with a
  recording task of its own, each frame inspected, and canvas's reports
  expected by kind.

## Kernel surface

- **Resources it installs:** `Handlers` (`RecordFrame`, `TrackOpenedWindow`,
  `ForgetClosedWindow`, `DeliverInspection`), to order handlers around;
  `Targets`, each target's size and scale as it was last drawn, read by a
  camera's conversions and written by canvas's own task after each frame's
  drawing; a
  private draw state, holding each task's draw list, the frame's entries
  and cameras sorted and the inspection asked for; a private renderer,
  holding the default, clear, composite and pack materials, the sprite
  atlases, the white texel in the sRGB one, each image drawn and where it is
  packed, the cameras drawn, the entries' keys, the instance data and the
  batches; and a private list of the open windows. The images `ImageFromFile`
  interns are a table of the process's, shared by every kernel, each kernel
  packing them apart.
- **Resources it reads from other plugins:** render's, for read through
  `render.Frame` and `render.Inventory`, `Assets` holding a `render.Frame`, `platform.FS`, for read through
  `render.Inventory`, as canvas's shaders and sprites' images load, render's
  texture descriptors through it, for a texture target's size and format
  and an image's size,
  and `platform.WindowManager`, for read, for a window target's size, in the
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
