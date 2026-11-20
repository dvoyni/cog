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
sprites of different images and the shapes beside them batch; a full atlas
grows its pages, and at its ceiling evicts the images drawn least lately.
Images are preloaded, unloaded by path or prefix and sized from their
headers through `Assets`. A frame can be inspected in canvas's terms,
beside render's inspection of it.

## Shape

- **`Plugin`**, from `NewPlugin()`, is canvas. **`Config`** is its part of
  the kernel's config, under `PluginName`: `RegroupedLayers`, the layers
  whose entries of equal keys canvas may regroup by batch,
  `DefaultFilter`, the filter of sprites naming none, `AtlasPageSize`, the
  side of a sprite atlas's pages, zero meaning 2048, a multiple of the atlas grid
  holding the white texel's cell, 12 at the default 3 mip levels and 5 at
  one, a side that is not raised to the next that is and reported once,
  `AtlasLayerCeiling`, the most pages it grows to, zero meaning 256,
  `OwnTextureSize`, the side past which an image is drawn from a texture
  of its own, zero meaning half a page, `MipLevels`, how many mip levels a
  page has, zero meaning 3, fixing the atlas grid and the gutter at
  2^(levels−1) texels, more than 12 lowered to 12 and reported once, and
  `DoNotGenerateMipMaps`, which makes pages of one level, for pixel-perfect
  games.
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
- **`Sprite`** is an entry that draws an `Image`, or the `Frame` of its
  sheet, counted along its rows, through its `m.Mat2x3`
  `Transform` about its image's pivot, mirrored by a negative scale, at its
  `Size`, zero its image's in texels and one zero axis keeping the aspect,
  times its `Tint`, white when zero, with its `Crop` left out where it lies,
  read through its `Filter`, `FilterLinear` or `FilterNearest`, zero
  meaning `Config.DefaultFilter`, and drawn with its `Material` and its
  `Clip`, at its sort key, as a shape is. Its `DrawMode` is `Stretched`,
  the zero, `Sliced`, cut into nine by its image's `Slice` insets, its
  corners kept at their texels' size, one instance that turns and mirrors
  whole, corners larger than the sprite shrinking until they meet and a
  `SliceHollow` image's centre left empty, or `Tiled`, repeating its image
  from its top left corner, without seams along the axes its image wraps;
  its `TexelScale`, zero meaning 1, is the size a sliced sprite's corners
  and a tiled sprite's tiles take each texel at. `DrawSprite` records one, and
  `DrawSprites` a slice, as an ECS chunk's column. Sprites of any images of
  one atlas batch together while their filter is one, and the shapes beside
  them with them, since shapes, and the zero `Image`, draw the white texel
  of the sRGB sprite atlas, which takes no filter.
- **`ImageFromFile(path, options…)`** interns a path in platform's FS and its
  **`ImageOptions`** (its `ColorSpace`, `ColorSpaceSrgb` or
  `ColorSpaceLinear`; its `Wrap`, the axes it tiles along, `WrapNone`,
  `WrapX`, `WrapY` or `WrapBoth`, whose gutters hold its opposite edge; its
  `Sheet`, a `Grid{Columns, Rows}` of frames, zero one frame and a zero
  column or row count one; its `SourceRect`, the `m.Recti` of the file it
  is, in texels, zero the whole file, a sheet cutting it into frames; its
  `Slice`, the `Insets` a sliced sprite cuts each frame into nine by, in
  texels, insets meeting past the frame shrinking until they meet, and
  `SliceHollow`, which leaves a sliced sprite's centre empty;
  its `Pivot`, a `Fraction` of its size from its
  centre plus `Texels`, or one of the named pivots, `PivotCenter`,
  `PivotTopLeft`, `PivotTop`, `PivotBottom`, `PivotBottomLeft` and
  `PivotBottomRight`, measured within its frame; and its `Convert`, a
  `Converter`) into an `Image`, a plain value an ECS component holds.
  It loads nothing: the first frame drawing it acquires its file from
  render and packs it into the sprite atlas of its colour space, each image,
  and each frame of a sheet, a cell of its own, the rectangles and frames
  of one file decoded once, the image rounded up to the atlas grid and a gutter past it
  continuing its edges, the page's mips then made again before any
  camera's pass, so a minified sprite never mixes a neighbour, drawn in
  that frame, and a file render cannot load draws render's magenta,
  reported once by render. A full atlas doubles its pages, copied on the
  GPU into a new texture at every mip level, growths not yet copied
  merged into one copy; at
  `AtlasLayerCeiling` it evicts the images drawn
  least lately to make room, never one drawn this frame, and an evicted
  image drawn again is loaded again. An image wider or taller than
  `OwnTextureSize`, whose cell no page holds, or that a full atlas cannot
  make room for, reported once, is drawn from a texture of its own,
  exactly its size with no gutter, so an image as large as a page takes a
  texture of a page's size, of the page's mip levels or as many as its size
  has, whose sampler repeats along the axes the image wraps along. Images
  differing only in their pivot, their slice insets or `SliceHollow` share
  their region. A second set of options is ignored and reported once, and
  a `SourceRect` reaching past its file, or a `Frame` past its sheet's
  last, draws nothing, reported once.
- **`Converter`** converts each frame of an image on the CPU before it is
  packed, `Convert(frame image.Image) image.Image`, through
  `ImageOptions.Convert`: canvas reads and decodes the file itself, in
  the job that first draws the frame, or `Preload`'s, converts the frame,
  its bounds beginning at 0, 0, and packs a texture made of the output.
  The output's bounds place it within the frame, while the image's size
  and pivot, a sprite's `Size` and `ImageSize` measure the frame, so a
  trimmed sprite, a `SubImage`, stays where it was; a sprite's `Crop` is
  of the output. A converter is a comparable value, so images of equal
  converters share their texels; one that is not comparable, or one of an
  image of a render texture, is refused, the image drawn unconverted and
  reported once, and a file canvas cannot decode loads through render,
  unconverted, which draws its magenta and reports it.
- **`DistanceField{Spread}`** is a `Converter` baking an image's alpha into
  a distance field, white, its alpha ½ on the art's edge, falling to 0
  `Spread` texels outside it and rising to 1 `Spread` texels inside it,
  zero meaning 8. Its output is the frame grown by `Spread` on each side,
  so given a sprite's transform and size it lines up with the sprite at
  any pivot. An anti-aliased edge is found to within a fraction of a
  texel from its coverage.
- **`ImageFromTexture(texture, options…)`** interns a texture made with
  render and its `ImageOptions` into an `Image`, never packed: its entries
  draw the texture's mip level 0 and layer 0, where a camera into it
  draws, through a 2D-array view of them alone, so it batches only with
  entries of the same texture, and a minimap camera's texture drawn as a
  sprite shows what the camera drew that frame. Its texels are taken as
  premultiplied, as a camera draws them, and its options are measured in
  them: a texture of a format encoding nothing that holds encoded colour,
  as a camera's into it does, is marked `ColorSpaceSrgb`, the zero, and
  decoded as it is sampled, so it looks as the world drawn directly, and
  an sRGB texture is decoded by its format, however it is marked. Its
  sampler repeats along the axes its `Wrap` names, so a tiled sprite of
  the whole texture tiles without seams. A texture render does not hold
  draws nothing, reported once.
- **`OrderOf(y)`** maps a float onto an order of its own, in the same order,
  for y-sorting.
- **`Material`** names a 2D material; the zero one, the default material,
  is the only one canvas makes yet.
- **`Assets`** is the task parameter a task reaches canvas's assets and
  tools through: `Preload(image)` loads an image ahead of its first draw,
  for a loading screen, a converted one's file decoded in the caller's
  job; `Unload(path)` unloads the images of a path, or of
  a folder for a path ending in a slash, `Unload("level1/")`, as canvas next
  draws, a later draw loading them again; `ImageSize(image)` answers an image's size, its
  rectangle's or its frames', a converted image's before it is converted, from its file's header alone, before it is drawn, and false for a missing file, reported
  once, an image of a texture measured on render's descriptor of it; `RequestInspection` asks for an inspection of the next frame, and
  `ReadInspection` answers it in a task off **`InspectedEvent`**.
- **`Inspection`** is one frame in canvas's terms: each camera that drew,
  the window it drew into and its viewport in pixels, its batches with their
  material, the texture they drew from, an atlas's or a render texture's,
  their filter, their clip and the entries it did not
  cull, each entry a shape or a sprite, with its sort key and its draw
  list's place, and render's `Inspection` of the frame beside it.
- **`cog.canvas/canvas.wgsl`** is canvas's include, declaring the camera's
  pass parameters at `@group(0)` and canvas's instance data and atlas at
  `@group(2)`, with the functions over them, `canvas_sample` answering an
  entry's image premultiplied, as the atlas holds it, and linear, an image
  of a texture of a format encoding nothing decoded from sRGB where it is
  marked so, sliced or tiled as its draw mode says: the one way a material
  samples an entry's image, since one sampling its `uv` itself stretches a
  sliced or tiled sprite. It samples with the gradients of the coordinate
  before wrapping, so a tile's seam reads the mip level its neighbours do,
  and is called in uniform control flow, before any discard;
  `canvas_covers` leaves out a hollow centre, as it leaves out a rounded
  shape's corners. canvas's own
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
  and cameras sorted, the inspection asked for, the unloads and preloads
  asked for and the image sizes read; a private renderer, holding the
  default, clear, composite and pack materials, the sprite atlases, the
  white texel in the sRGB one, each image drawn, where it is packed and
  when it was last drawn, the sources preloaded, the cameras drawn, the
  entries' keys, the instance data and the batches; and a private list of
  the open windows. The images `ImageFromFile` and `ImageFromTexture`
  intern are a table of the process's, shared by every kernel, each kernel
  packing them apart.
- **Resources it reads from other plugins:** render's, for read through
  `render.Frame` and `render.Inventory`, `Assets` holding both; `platform.FS`,
  for read, through `render.Inventory`, as canvas's shaders and sprites'
  images load, and in `Assets`, for an image's header; render's
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
