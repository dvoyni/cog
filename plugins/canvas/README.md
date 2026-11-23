# canvas

`github.com/dvoyni/cog/plugins/canvas` is the plugin on [render](../render)
that draws 2D: sprites, shapes, text and meshes, recorded as entries every
frame and drawn through 2D materials. It is its own Go module (Go 1.27),
importing [kernel](../../kernel), [metronome](../metronome),
[platform](../platform), render, [libs/m](../../libs/m),
[libs/meshfile](../../libs/meshfile) and go-text's
[typesetting](https://github.com/go-text/typesetting), and reaches no gfx.

It is being built from [docs/design.md](docs/design.md). So far a frame's
recording tasks record shapes, sprites, runs of text and per-frame meshes
and set cameras into draw lists
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
headers through `Assets`, and 2D materials made from whole WGSL files that
include canvas's include, a fragment-only one drawing through canvas's
vertex stage, with their parameters set by name. Font sets are made and
destroyed through `FontLibrary`'s writing side, their fonts measured and text
laid out, shaped with bidi and broken into lines, and drawn in one font a
run, each glyph made a distance field at its first draw and packed into
the glyph atlas, its outline or other profile drawn under its fill. A frame
can be inspected in canvas's terms, beside render's inspection of it.

## Shape

- **`Plugin`**, from `NewPlugin()`, is canvas. **`Config`** is its part of
  the kernel's config, under `PluginName`: `RegroupedLayers`, the layers
  whose entries of equal keys canvas may regroup by batch,
  `DefaultFilter`, the filter of sprites naming none, `AtlasPageSize`, the
  side of a sprite atlas's pages and the glyph atlas's, zero meaning 2048, a multiple of the atlas grid
  holding the white texel's cell, 12 at the default 3 mip levels and 5 at
  one, a side that is not raised to the next that is and reported once,
  `AtlasLayerCeiling`, the most pages each grows to, zero meaning 256,
  `OwnTextureSize`, the side past which an image is drawn from a texture
  of its own, zero meaning half a page, `MipLevels`, how many mip levels a
  page has, zero meaning 3, fixing the atlas grid and the gutter at
  2^(levels−1) texels, more than 12 lowered to 12 and reported once, and
  `DoNotGenerateMipMaps`, which makes pages of one level, for pixel-perfect
  games, `Language`, the current language at start, `TextSize`, the em
  of a run of text of no size, zero meaning 16, `LayoutIdleFrames`, how
  many frames a layout goes unused before the layout cache frees it, zero
  meaning 2, `GlyphFieldEm` and
  `GlyphFieldRange`, the field em and range of a font giving none, zero
  meaning 48 texels to the em and 16 texels across a glyph's edge, and
  `DefaultFontSet`, the default font set, made as canvas starts, its zero
  JetBrains Mono alone.
- **`FrameEvent`** is the frame its users record, published inside each of
  render's `FrameEvent`s. A plugin that records schedules its recording tasks
  after its `Ready` and joins them into the out dependency it returns.
- **`DrawList`** is the task parameter a recording task records through,
  each task's a draw list of its own, reading `FontLibrary` for its text.
  An entry recorded or a camera set outside the frame is refused and
  reported once for the task.
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
  `DrawSprites` a slice, as an ECS chunk's column; `DrawSpriteWith`,
  `DrawSpritesWith` and `DrawShapeWith` give each entry its material's
  argument, `DrawSpritesWith` as a parallel slice, refused and reported once
  when the lengths differ. Sprites of any images of
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
- **`SDFMaterial`** is canvas's SDF material, drawing the distance from an
  entry's edge through a **`Profile`**, its argument, as a colour: a
  sprite's read from its image, a distance field of the profile's `Spread`,
  zero meaning 8 as `DistanceField`'s does, and a shape's from its rectangle
  and radius, its quad and its bounds grown by the profile's reach. It draws
  the band alone, so a halo is two entries, the field drawn with it and the
  sprite over it, its colour the profile's times the entry's tint, so a
  shape, whose zero tint is transparent, gives one, and its zero `Profile`
  draws nothing. A `Profile{Inner,
  Outer, Plateau, Falloff, Color, Offset, Spread}` is a band from `Inner`
  inside the edge to `Outer` outside it, solid to `Plateau`, then falling
  off along (1 − t) to the power `Falloff`, zero a hard edge, in field
  texels, a shape's own space for a shape; its `Color` is premultiplied,
  adding when its alpha is zero, and its `Offset` is in world space, so
  every shadow falls the same way, mirrored or not. **`Outline(width,
  color)`**, **`Halo(plateau, reach, color)`**, **`Glow(reach, color)`**,
  which adds, and **`DropShadow(offset, softness, color)`** make the usual
  ones, each lying under the whole entry, so the entry drawn over it leaves
  no seam. Every outline and glow of a frame batches, each with its own
  colour. canvas holds it, so it is never released and its parameters are
  canvas's.
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
- **`DrawTriangles(options, triangles)`** records a mesh given for one
  frame, a trail, a rope or a skin: a **`Triangles[V]{Vertices, Indices,
  Topology, Bounds}`**, of vertices of a `meshfile.VertexType`, optional
  `[]uint32` indices, kept as 16 bits while the vertices number at most
  65,535, a `meshfile.Topology`, zero a triangle list, and its bounds, an
  `m.Box3` in its own space, `meshfile.CalculateBounds`'s, required: zero
  bounds are refused, reported once for the task as
  `ReportKindBoundsMissing`, and triangles of no vertices record nothing.
  **`DrawTrianglesOptions`** holds a sprite's fields but its size, pivot,
  draw mode and crop: its `Image` and `Frame`, the zero `Image` the white
  texel, its `Transform`, `Tint`, `Material`, `Clip`, `Filter` and sort
  key. Its vertices and indices are copied as it is recorded, so the
  caller may reuse its slices, then into the frame's temporary buffers,
  drawn through render's `AcquireTemporaryMesh`, a draw of its own never
  merged with another entry's, culled by its bounds through its transform.
  **`Vertex{Position, UV, Color}`**, 32 bytes, is canvas's standard
  layout, drawn by the include's `canvas_mesh_vertex` and the material's
  fragment stage, so a fragment-only material draws meshes as sprites: its
  UV runs 0 to 1 over its image's region, repeating past it along the axes
  the image wraps along and continuing its edge along the others, so a
  terrain polygon whose UVs are its positions over the image's size fills
  with a tiling image, and its `Color` multiplies the image as a tint does,
  straight alpha, zero meaning white, so a vertex faded out takes
  `{1, 1, 1, 0}`. `Position3()` answers its position for
  `meshfile.CalculateBounds(vertices, canvas.Vertex.Position3)`. Vertices of
  another layout, a type whose `VertexLayout()` answers its
  `meshfile.Layout`, draw through the material's own vertex stage, which
  includes canvas's include and places each vertex through
  `canvas_make_quad_varyings`, so swaying grass carries its sway per
  vertex; drawn with a material whose vertex stage is canvas's, as a
  fragment-only material's is, they are skipped, reported once for the
  material as `ReportKindVertexStageMissing`.
  `DrawTrianglesWith` gives the entry its material's argument.
- **`MeshFileFromFile(path)`** interns a mesh file's path in platform's FS
  into a pointer-free **`MeshFile`**, the zero one naming none, as
  `ImageFromFile` interns an image. A game puts the files it builds into a
  memory mount of its own, `filesystems.NewMemoryFS`, bound as a read
  mount, or ships them in any read mount, written by `meshfile.Encode`.
  **`DrawMesh(mesh)`** records a **`Mesh{File, Image, Frame, Transform,
  Tint, Material, Clip, Filter, Layer, Order}`**, what `DrawTrianglesOptions`
  holds and its file; `DrawMeshes(meshes)` records a slice, and
  `DrawMeshWith` and `DrawMeshesWith` give arguments, as the sprites' calls
  do. A file loads at its first draw, through platform's FS and
  `meshfile.Decode`, into buffers and a mesh of render's, draws that frame,
  and stays until its path is unloaded; a file that is missing, is no mesh
  file, is of a version canvas does not know or has zero bounds draws
  nothing, reported once for its path as `ReportKindMeshFileNotLoaded`. A
  game changes one by putting its file again and unloading its path, and
  the next draw loads the new one. Consecutive meshes in sort order of one
  file, material, atlas, filter and clip are one instanced draw, so a forest
  of one tree, recorded in one `DrawMeshes`, is one draw; a camera culls
  each by its file's bounds through its transform. A file of `Vertex`'s
  layout draws through `canvas_mesh_vertex`, and one of another layout
  through the material's own vertex stage, refused with a fragment-only
  material as per-frame triangles are. Under a pixel-perfect camera, a
  mesh's translation snaps to the world-unit grid, its vertices moving with
  it unsnapped, as a per-frame mesh's do.
- **`OrderOf(y)`** maps a float onto an order of its own, in the same order,
  for y-sorting.
- **`Material`** names a 2D material, made by `Assets.MakeMaterial`; the
  zero one is the default material, an entry's image times its tint.
  **`MaterialDescriptor`** is what one is made of: its `Label`, its
  `Shader`, the path of a whole WGSL file that includes
  `cog.canvas/canvas.wgsl` and declares only `@group(1)`, its
  `VertexEntryPoint`, empty meaning canvas's own, so a fragment-only
  material draws sprites and shapes alike, and meshes of `Vertex` through
  canvas's mesh vertex stage whatever it names, its `FragmentEntryPoint`, its
  `Blend`, premultiplied, zero meaning `BlendNormal()`, its `Argument`, and
  its `CullMargin`, in world units, growing its entries' bounds for a vertex
  stage that moves vertices. **`BlendNormal()`**, **`BlendAdditive()`**,
  **`BlendMultiply()`**, **`BlendScreen()`** and **`BlendOpaque()`** are
  the usual modes, premultiplied.
- **`ArgumentOf[T]()`** is a material's `Argument`, the type of the value each
  entry gives it, its *material argument*: a struct of any number of fields,
  or a plain value, holding no pointer (`ArgumentOf` panics on one), mirroring
  the shader's WGSL struct `Argument`, which canvas preprocesses the shader
  with the define `CANVAS_ARGUMENT` for and a fragment stage reads with the
  include's `canvas_argument`. Its layout is checked as the material is made,
  a mismatch reported once and the material's entries skipped; an argument of
  another type, or one given a material that takes none, is refused, reported
  once, and its entry skipped; an entry giving none gives the zero value.
  Arguments break no batch: each entry's follows canvas's own instance data,
  in temporary storage buffers, one per stride. A shader that reads its
  argument compiles only with the define, so its binding declaration for tests
  is generated with `declarebindings -variantsonly -variant CANVAS_ARGUMENT`.
- **`Assets`** is the task parameter a task reaches canvas's assets and
  tools through: `Preload(image)` loads an image ahead of its first draw,
  for a loading screen, a converted one's file decoded in the caller's
  job; `Unload(path)` unloads the images and mesh files of a path, or of
  a folder for a path ending in a slash, `Unload("level1/")`, as canvas next
  draws, a later draw loading them again; `ImageSize(image)` answers an image's size, its
  rectangle's or its frames', a converted image's before it is converted, from its file's header alone, before it is drawn, and false for a missing file, reported
  once, an image of a texture measured on render's descriptor of it;
  `MakeMaterial(descriptor)` makes a 2D material, its argument's layout
  checked against its shader's, and `ReleaseMaterial`
  gives it back, the frame recording as it is released still drawing it;
  `SetMaterialTexture` and `SetMaterialUniform[T]` set its parameters by
  name, as render's setters do, a draw using them as they stand when canvas
  draws the frame, after every task hung off `FrameEvent`; a material canvas
  does not hold, or the default one's or `SDFMaterial`'s parameters,
  refused and reported once; `RequestInspection` asks for an inspection of
  the next frame, and
  `ReadInspection` answers it in a task off **`InspectedEvent`**.
- **`Inspection`** is one frame in canvas's terms: each camera, one that
  culled everything among them, the window or texture it drew into, the
  target's size and its viewport in pixels, its scale and the world it
  sees, its batches with their material, the texture they drew from, an
  atlas's or a render texture's, each with its label, their filter, their
  clip and the entries it did not cull, and the entries it culled, each
  entry a shape, a sprite, a run of text or a mesh, with its sort key and
  its draw list's place; the frame's first uses, the images packed, mesh
  files loaded, layouts and glyphs made; the reports logged for it; how long
  canvas took to sort and to draw it, timed by metronome's `Clock` through
  the stopwatches `SortStopwatchName` and `DrawStopwatchName`; and render's
  `Inspection` of the frame beside it. Its `String` writes it as text for an
  agent to log, a line for each first use, report, layer, batch and cull.
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
  default material, `cog.canvas/default.wgsl`, includes, as do its SDF
  material, `cog.canvas/sdf.wgsl`, its text material, `cog.canvas/text.wgsl`,
  both drawing a profile's band through the include's
  `canvas_band_coverage`, and
  `cog.canvas/viewport.wgsl`, the shader of the quads canvas draws over a
  viewport: a camera's clear, what a multisampled camera resolved, and an
  image premultiplied into its cell of an atlas. Beside them is canvas's
  built-in font, JetBrains Mono, `cog.canvas/jetbrainsmono.ttf`, under the
  SIL Open Font License in `cog.canvas/jetbrainsmono-OFL.txt`.
- **`FontLibrary`** is the resource of font sets, read under
  `kernel.Read[canvas.FontLibrary]`: `Language()` answers the current language,
  `Layout(text)` a **`Text`** laid out with no frame, from the layout
  cache `DrawText` lays out through, `Preload(set, text)` makes the glyphs
  of a string ahead of its first draw, and
  `Metrics(set, size, language)` the `FontMetrics` of the font measuring
  `language` in `set`, its first font for that language or else its
  default, at `size`, its em in world units: `Ascent`, `Descent`,
  `LineGap`, `LineHeight`, `XHeight` and `CapHeight`, an empty language
  meaning the current one. A set canvas does not hold, never made or
  destroyed, is laid out and measured as the default set, reported once.
  **`ElevateFontLibrary(kernel.Write[FontLibrary], kernel.Read[platform.FS])`** answers
  its writing side, **`WriteFontLibrary`**, so a task holding the read lock cannot
  make a set: `MakeFontSet(descriptor)` loads every font file a
  **`FontSetDescriptor`** names, now, and answers a **`FontSet`**, a plain
  handle whose zero is the default set; `DestroyFontSet(set)`;
  `SetLanguage(tag)`, tags matched exactly; `RegisterIcon(name, icon)`,
  an **`IconDescriptor`** of an `Image`, its `Frame`, its `Scale` of the cap
  height, its `Offset` above the baseline in em and whether it is
  `Tinted`, its frame's size read from its file's header then and the
  layouts naming it laid out anew, an image of a render texture or of
  linear colour refused, reported once; and `RegisterColor(name, color)`,
  which markup's `\c:name{text}` tints with, read as the run is drawn, so
  a theme changes it in one place. A descriptor
  holds a `Default` **`Font`** and **`LanguageFonts`**, the fonts serving
  some languages. A `Font` is a `Path` in platform's FS, a TrueType or
  OpenType font or a collection, empty meaning canvas's JetBrains Mono, its
  collection's `Index`, its `FieldEm` and `FieldRange`, the em its glyphs'
  distance fields are made at and how far they reach across an edge, in
  field texels, zero meaning the `Config`'s, its `BitmapEm`, a pixel font's
  pixel size, zero for distance fields, whose glyphs are its coverage at
  that em, read with nearest filtering and drawn at a whole multiple of
  it, a run's size rounded to one, with no distance effects, and a
  variable font's `Variations`, each a
  **`Variation{Axis, Value}`** whose axis **`AxisTagFromName("wght")`**
  answers, a name of other than four bytes answering the zero tag, whose
  variation is ignored and reported once. A font file that fails to load is
  reported once and left out of its set, a failed default font replaced by
  JetBrains Mono.
- **`Text`** is a run of text: its `String`, a newline, or another of
  Unicode's mandatory breaks, a hard break, its
  `Font` set and `Size`, its em in world units, zero meaning
  `Config.TextSize`, its `Language`, a tag matched exactly, zero meaning
  the current language, its `WrapWidth`, zero meaning no wrapping, its
  `Align`, a **`TextAlign`**, `TextAlignStart`, `TextAlignCenter` or
  `TextAlignEnd`, a right-to-left paragraph starting at the right, `LineHeight`, multiplying the font's,
  zero meaning 1, and `Tracking`, letter spacing in em. go-text shapes it
  in its language, each character in the first of the language's fonts in
  its set that has it, else in the set's default font, or, when no font of
  the set has it, as the default font's missing-glyph box, reported once
  for the set; its lines are measured by the font measuring its language,
  as `Metrics` reads. The font drawing each character is kept per set,
  language and character, so a change of language clears nothing. It is
  ordered by
  Unicode's bidi algorithm, its lines broken on UAX #14 opportunities, so
  Japanese breaks between ideographs and never starts a line with closing
  punctuation, and a word too long for a line between grapheme clusters.
  The **`TextLayout`** answers `Size()`, its box, the wrap width or its widest
  line by its lines' height, `LineCount()`, `Baseline()`, its first line's
  from the box's top, `LineWidth(line)`, and `ClusterPosition(cluster)`,
  where a grapheme cluster starts on its line's baseline, one past the last
  where the text ends, for carets and selections. As an entry, a `Text`
  also carries its `Transform`, an `m.Mat2x3` about its `Pivot`, a point of
  its layout's box, its `Texels` in em, its `Tint`, the fill's colour, white
  when zero, its `Spans`, when given, the run instead of its `String`: each
  a **`Span`** of `Text` in its own `Tint`, zero meaning the run's, or an
  `Icon` registered by name, the cap height tall and placed on the
  baseline like a glyph, a line breaking around it as around a letter, a
  name not registered drawing the missing-glyph box, reported once, and
  its `Markup`, whether its `String` is read as **markup**, the string form
  of spans: `\i:name` an icon, its name of letters, digits, `_`, `-` and
  `/`; `\c:colour{text}` text in `#rgb`, `#rrggbb`, `#rrggbbaa` or a
  registered colour, spans nesting and icons sitting in them; `\\` a
  backslash, braces counted within a span, and `\{` and `\}` lone ones.
  Markup that does not parse is drawn as written, reported once for its
  string as `ReportKindMarkupMalformed`, and a run without `Markup`, such
  as a name a player typed, is drawn as written. It carries its `Material`, zero meaning **`TextMaterial`**, its `Clip`, its
  `Filter`, zero meaning its font's, linear as a field is read and nearest
  for a bitmap font, and its sort key.
  `DrawText` records a run, laid out then, as `Layout` lays it out, one
  entry whose glyphs draw in order. Both lay out through the **layout
  cache**, keyed by the run's `String` or its spans' texts and icons, its `Font` set, its `Size`,
  `WrapWidth`, `Align`, `LineHeight` and `Tracking`, and its language, so a
  label measured or drawn frame after frame is shaped once, and a layout
  neither for `Config.LayoutIdleFrames` frames is freed; `DrawTextWith` gives its material's
  argument, `TextMaterial`'s a `Profile` drawn under the fill, such as
  `Outline(2, black)`, in the font's field texels, its colour faded by the
  tint's alpha alone. Each glyph is made a single-channel distance field
  the first time it is drawn, in the recording task, unless `Preload` made
  it, and packed into the
  **glyph atlas**, an R8 2D-array texture whose pages grow as the sprite
  atlases' do, each field with a transparent gutter of one texel, copied
  in by the frame's first command list; a glyph no page holds draws
  nothing, reported once. Small text, whose field spans more than 2 texels
  a pixel, is read at four points a pixel and its glyphs' pens placed on
  whole pixels, unturned, as a bitmap glyph's are. A bitmap font's glyphs
  share the atlas, rasterized at its pixel size, and the text material
  reads their coverage, ignoring the profile. The include's
  `canvas_glyph_spread` and `canvas_glyph_pen` answer a glyph's field's
  spread and its pen, `canvas_glyph_is_bitmap` whether it is a bitmap
  glyph, and `canvas_glyph_is_icon` whether an instance is an
  icon, drawn from `canvas_sprite_atlas`, the sRGB sprite atlas, for a
  custom material of runs. Every run of a frame drawn
  with `TextMaterial` batches with the next, whatever its size, tint,
  profile or icons.
- **`Report`** is a mistake canvas finds at run time, logged once and carried
  past. Its `ReportKind` names it for tests.
- **`canvastest`** runs canvas in a real kernel on render's fake GPU, with a
  recording task of its own, each frame inspected, and canvas's reports
  expected by kind.

## Kernel surface

- **Resources it installs:** `Handlers` (`RecordFrame`, `Recover`,
  `TrackOpenedWindow`, `ForgetClosedWindow`, `DeliverInspection`), to order
  handlers around, `Recover` starting afresh on `render.RecoveredEvent`;
  `Targets`, each target's size and scale as it was last drawn, read by a
  camera's conversions and written by canvas's own task after each frame's
  drawing; `FontLibrary`, its font sets, its current language and the icons and
  colours registered, and the glyphs made distance fields for drawing,
  under a lock of their own, written only through `ElevateFontLibrary`, and by canvas's
  own task making the default set as it starts; a
  private draw state, holding each task's draw list, the frame's entries
  and cameras sorted, the inspection asked for and the first uses and
  reports noted for it, the unloads and preloads asked for, the image sizes
  read, the 2D materials made and the stopwatches timing its jobs, under a
  lock of their own; a private renderer, holding the
  default, SDF, text, clear, composite and pack materials, the sprite
  atlases, the white texel in the sRGB one, the glyph atlas and where each
  glyph is packed, each image drawn, where it is packed and
  when it was last drawn, the sources preloaded, the cameras drawn, the
  entries' keys, the instance data, the batches and the per-frame meshes'
  temporary meshes, and each mesh file loaded, its buffers and mesh; and a
  private list of
  the open windows. The images `ImageFromFile` and `ImageFromTexture`
  intern are a table of the process's, shared by every kernel, each kernel
  packing them apart, as are the mesh files `MeshFileFromFile` interns,
  each kernel loading them apart.
- **Resources it reads from other plugins:** render's, for read through
  `render.Frame` and `render.Inventory`, `Assets` holding both; `platform.FS`,
  for read, through `render.Inventory`, as canvas's shaders and sprites'
  images load, and in `Assets`, for an image's header; render's
  texture descriptors through it, for a texture target's size and format
  and an image's size,
  and `platform.WindowManager`, for read, for a window target's size, and its
  own `FontLibrary`, for read, for the glyphs' fields, in the
  task that draws the frame; and `platform.FS`, for read, through
  `ElevateFontLibrary` and in its task making the default font set, as fonts
  load; and metronome's `Clock`, in `Init`, to make the stopwatches timing
  its sorting and drawing jobs.
- **Task parameters it offers:** `DrawList`, and `Assets`.
- **Events it publishes:** `FrameEvent`, inside its handler of
  `render.FrameEvent`; and `InspectedEvent`, once render has delivered its
  inspection of a frame canvas was asked to inspect.
- **Events it subscribes to:** `render.FrameEvent`;
  `render.RecoveredEvent`, to start afresh once a lost device is recovered;
  `platform.WindowOpenedEvent` and `platform.WindowClosedEvent`, to know the
  main window; and `render.InspectedEvent`, to put render's inspection
  beside canvas's.
- **Sockets it declares:** none.
- **Sockets it binds:** `platform.ReadMountSocket`, its built-in assets
  under the prefix `cog.canvas`, at the lowest priority.

canvas is listed after render, platform and metronome, and a plugin that
records into canvas is listed after canvas.

See [docs/design.md](docs/design.md) for the design and why it is this way,
and [CONTEXT.md](CONTEXT.md) for its words.
