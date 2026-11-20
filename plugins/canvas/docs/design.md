# canvas: design

The design of `github.com/dvoyni/cog/plugins/canvas`, the plugin on render that
draws sprites, shapes, text and meshes through 2D materials. It was mapped in
[canvas: sprites, text and meshes on render, every look a 2D material, generic enough to build on](https://github.com/dvoyni/cog/issues/860);
each section links the ticket that holds its detail. The vocabulary is in
[`CONTEXT.md`](../CONTEXT.md).

Status: being built. The plugin, its frame event, draw lists and canvastest
are built, a fill is drawn through the default camera with canvas's
include and its default material, a frame is inspected in canvas's
terms: its cameras, batches and entries, and entries are sorted by their
sort key, regrouped where their layer allows, and drawn a batch at a time,
a batch broken where the material changes, the only part of the batch key
that entries carry yet. Shapes fill, stroke and draw lines, hairlines among
them, their corners rounded by their radius.

## Problem Statement

render draws meshes with materials into passes and leaves everything above
that to the plugins that draw: it does no culling, sorting or batching, and
ships no materials. Every game that draws in 2D (sprites, a tilemap, a UI,
labels over units) would build the same layer on render, each with its own
bugs: an atlas packing images at run time, a glyph cache and a shaper, text
layout that measures what it draws, a sort of the frame's sprites by layer,
batching across images, a camera's matrices with a logical size and
pixel-perfect scaling, and a contract for custom shading that does not freeze
what a sprite carries.

v1's canvas was that layer, and what went wrong with it is what this design
must do better. Its 96-byte instance record was frozen, with no way to add a
member per sprite. Its clip was a discard in the fragment shader. A material
was keyed by its values, so a changing value made a new set every frame. Its
halo's two-layer idiom clashed with painter's order. Its glyph pages were never
reclaimed, and an atlas refusal was cached for good. It laid text out rune by
rune, kerning only, and measured text apart from drawing it, so the two
drifted. It culled nothing, and it blended in an intermediate texture that cost
a pass and 8 MB a window.

## Solution

canvas is to render what Godot's canvas items are to its renderer: one model
through which sprites, text and meshes are drawn, every look a 2D material. A
plugin that draws in 2D uses canvas, and render for what canvas does not
cover, and never reaches gfx:

- it **records entries** every frame into its **draw list**: sprites, shapes,
  runs of text and meshes, each a plain value with a **sort key**, singly or in
  bulk, and keeps no sprite object;
- it names **images** and **mesh files** by path, loaded at their first draw
  and ready in that frame, images packed into **atlases** with no acquire,
  release or count;
- it sets the frame's **cameras** as values: a position, zoom and rotation, the
  layers they draw, a target and a **viewport** of it, a **logical size** and
  its **fit**, and **pixel-perfect** scaling;
- it draws through **2D materials**, whole WGSL files over canvas's include,
  each entry giving its material its own **material argument**;
- it writes text in **font sets** chosen by **language**, with **spans**,
  **icons** and **markup**, and measures it through the same **layout** it
  draws;
- canvas sorts the frame's entries, culls them per camera, batches them,
  records one command list for each camera order among every plugin's, and
  forgets the frame;
- an **inspection** of a frame in canvas's terms, and **reloads** of changed
  images, fonts and mesh files, come with it.

canvas imports no ECS, ships a default, an SDF and a text material, and does
no lighting, post effects or particles. UI layout and widgets, and the ECS
plugin of sprite and text components, are plugins on canvas with maps of their
own, as v1's ui sat on v1's canvas.

## User Stories

The actors are a **game developer** composing a game that draws in 2D, a
**plugin author** building on canvas (the ECS plugin, a UI, a tilemap), an
**agent** looking into a running game through a tool plugin, and a **test
author** testing canvas or a plugin on it.

### Recording a frame

1. As a game developer, I want to record sprites, shapes, text and meshes every frame with calls such as `DrawSprite`, and keep no sprite object, so that what is drawn is what my game's state says this frame.
2. As a plugin author, I want each entry to be a plain value, so that an ECS component can be exactly that value.
3. As a plugin author, I want bulk calls taking parallel slices of entries and of their arguments, so that an ECS chunk's columns go in as they are.
4. As a plugin author, I want each recording task to record into a draw list of its own, so that parallel systems never contend.
5. As a game developer, I want an entry recorded outside the frame refused and reported, so that a task on the wrong beat is named, not silently lost.
6. As a plugin author, I want the atlas, text layout, the 2D material contract and the camera's matrices to be public pieces that canvas's own entries use, so that my tilemap or UI has the power of the built-ins.

### Order and layers

7. As a game developer, I want entries sorted by a layer, then an order within the layer, so that I place the ground, the units and the UI with no depth buffer.
8. As a game developer, I want entries of equal keys drawn in the order I recorded them, so that a game that sets no keys gets call order.
9. As a game developer, I want a function that maps a y position onto an order, so that I y-sort units with no scale to choose.
10. As a game developer, I want ties between draw lists broken by the order their tasks were registered in, so that I choose no number for them.
11. As a game developer, I want a layer that may regroup its entries of equal keys, so that a UI's or particles' entries batch where their order among themselves does not matter.

### Sprites

12. As a game developer, I want a sprite's transform to be one 2D affine matrix, so that it moves, turns, scales and skews, and mirrors by a negative scale.
13. As a game developer, I want a sprite's size apart from its transform's scale, so that a sliced or tiled sprite keeps its corners and tiles at their texel size.
14. As a game developer, I want a sprite placed, turned and scaled about its image's pivot, named or measured in texels, so that a unit stands on its feet.
15. As a game developer, I want a pivot measured on a converter's input, so that a trimmed sprite and a distance-field halo stay where the art is.
16. As a game developer, I want a tint that multiplies, white when I give none, so that I fade or colour a sprite without a material.
17. As a game developer, I want a crop that leaves part of a sprite out where it lies, so that a health bar empties from one side without re-centring.
18. As a game developer, I want nine-slice sprites drawn as one instance that can rotate, with an optional hollow centre, so that panels and frames are one entry.
19. As a game developer, I want tiled sprites wrapped without seams at a texel scale I choose, so that water and walls repeat cleanly.
20. As a game developer, I want a filter per sprite, with its default in canvas's config, so that a pixel-art game sets nearest filtering once.

### Images and the atlas

21. As a game developer, I want to name an image by its path and options without loading it, so that a component holds it as a plain value.
22. As a game developer, I want an image loaded and packed at its first draw and drawn in that frame, so that I never handle a loading state.
23. As a game developer, I want images packed into atlas pages without laying them out myself, so that sprites over different images batch.
24. As a game developer, I want to preload an image from any task, so that a loading screen takes its decode.
25. As a game developer, I want to unload images by path or prefix, so that a level's art goes at the level's end.
26. As a game developer, I want a full atlas to evict what was drawn least lately, or give an image a texture of its own, never refuse it for good, so that a long session never loses a sprite.
27. As a game developer, I want a sheet's frames packed apart, so that filtering and mips never bleed between frames.
28. As a game developer, I want any rectangle of a file to be an image of its own, so that a file holding several sheets or an irregular sheet needs no tool of mine.
29. As a game developer, I want an image's size from its header before it is drawn, so that a UI lays itself out ahead of its first frame.
30. As a game developer, I want converters run on an image before it is packed, such as baking a distance field, so that derived art needs no build step.
31. As a game developer, I want each image's mips made from its own texels, or none for pixel art, so that sprites shrink cleanly without mixing neighbours.
32. As a game developer, I want a failed image drawn magenta and reported once, so that a missing asset is loud on screen without crashing.
33. As a game developer, I want a render texture drawn as an image, so that a minimap or a curved display is a sprite or a mesh over it.

### Shapes

34. As a game developer, I want filled and stroked rectangles and lines as literal entries with a sort key, batching with sprites, so that a UI's boxes cost no extra draws.
35. As a game developer, I want a zero width to draw a hairline one target pixel wide at any zoom, so that debug lines and borders never vanish.
36. As a game developer, I want a translucent stroke whose corners don't darken, so that a border reads as one colour.
37. As a game developer, I want rounded corners and a glow or outline around a shape, so that a button glows with no image baked per size.

### 2D materials

38. As a plugin author, I want to make a 2D material from a whole WGSL file that includes canvas's modules, so that a look is a material, not a feature of canvas's.
39. As a plugin author, I want a fragment-only material to serve sprites, glyphs and meshes alike, so that one dissolve or outline shader draws on every entry.
40. As a plugin author, I want each entry to give its material an argument of a type the material declares, with any number of fields, so that per-entry data is mine to shape without breaking batches.
41. As a plugin author, I want an argument's Go type checked against its WGSL struct, and a draw's argument of another type refused and reported, so that a layout mismatch is named, not drawn as garbage.
42. As a plugin author, I want a material's parameters set by name, documented as standing when canvas draws, so that I know when a change shows.
43. As a game developer, I want premultiplied blending with normal, additive, multiply, screen and opaque modes ready, so that glows and shadows batch with the rest.
44. As a game developer, I want outlines, halos, glows and drop shadows to be one distance-field material taking a profile, so that every outline in a frame batches, each with its own colour.
45. As a game developer, I want a sprite's alpha baked into a distance field, so that any sprite gets a halo.
46. As a plugin author, I want a material whose vertex stage moves vertices to grow its entries' bounds, so that swaying grass is not culled at the view's edge.
47. As a plugin author, I want time to be a material parameter set from my clock, so that a paused game's materials stop with it.

### Text

48. As a game developer, I want text to name a font set and a size, never a font, so that switching the language switches the typeface.
49. As a game developer, I want each language's fonts and a default font in a set, so that a character the language's fonts lack still draws.
50. As a game developer, I want text shaped with bidi and proper line breaks, so that Arabic, Hebrew, Chinese and Japanese draw correctly.
51. As a game developer, I want a run of text to be one entry with a transform and a pivot of its box, so that labels turn, scale and stand over units.
52. As a game developer, I want a run's outline drawn under its fill in the same entry, so that overlapping labels never paint one's outline over another's fill.
53. As a plugin author, I want to measure text through the same layout canvas draws, so that a button sized to its label never disagrees with it.
54. As a game developer, I want a label drawn every frame shaped once, so that text costs nothing while it stays the same.
55. As a game developer, I want wrapping, alignment, line height and letter spacing, so that paragraphs and dialogue lay out without code of mine.
56. As a game developer, I want spans with tints of their own, shaped and wrapped with their paragraph, so that a coloured word wraps with its sentence.
57. As a game developer, I want icons registered by name and placed in text like glyphs, replaced everywhere when registered again, so that button prompts switch with the controller.
58. As a game developer, I want markup read only when a run asks, so that text a player typed draws as written.
59. As a game developer, I want colours registered by name for markup, so that a translator writes a keyword's colour and a theme changes it in one place.
60. As a game developer, I want to make a text's glyphs ahead of time, so that a title's first frame doesn't make them.
61. As a game developer, I want a font's metrics at a size, so that I align text to other content.
62. As a game developer, I want bitmap fonts for pixel art, so that a pixel font keeps its pixels.
63. As a game developer, I want variable fonts and font collections, so that one file serves a weight or a region.
64. As a game developer, I want a font that fails to load reported once and replaced, so that text never vanishes.

### Meshes

65. As a game developer, I want to draw a mesh file named by a path, loaded at its first draw, so that a polygon is drawn like an image.
66. As a game developer, I want to put mesh files I build into a memory mount, or ship them, so that generated and exported meshes load alike.
67. As a game developer, I want triangles given with the draw for one frame, so that a trail, a rope or a skin needs no file.
68. As a plugin author, I want a standard vertex layout drawn by canvas's own vertex stage, so that fragment-only materials serve meshes.
69. As a plugin author, I want meshes in a vertex layout of my own, drawn by my material's vertex stage over canvas's include, so that grass can carry its sway per vertex.
70. As a game developer, I want a mesh to sample an image's region as a sprite does, so that a terrain polygon fills with tiling grass.
71. As a game developer, I want many entries of one mesh file drawn as one instanced draw, so that a forest of one tree costs one draw.
72. As a game developer, I want a mesh's bounds required, so that a mesh never vanishes when its origin leaves the view.
73. As a plugin author, I want the mesh file format in a library of its own, so that an exporter or a 3D plugin uses it without render.

### Cameras

74. As a game developer, I want to set the frame's cameras as plain values, so that following, smoothing and shake are my code over a value.
75. As a game developer, I want a frame that sets no camera seen through a default one, so that a first sprite shows with no setup.
76. As a game developer, I want viewports as fractions of a target, so that split screen is two cameras.
77. As a game developer, I want a camera drawing into a render texture before the window that shows it, so that a minimap is up to date in the same frame.
78. As a game developer, I want a logical size fitted to the viewport by expanding, inscribing with bars, overlapping or stretching, so that my game shows the area it was designed for on any window.
79. As a game developer, I want a pixel-perfect camera, so that pixel art keeps square, still pixels at any window size and DPI.
80. As a game developer, I want a camera's order among every plugin's command lists, so that 2D draws under a 3D scene and a UI over it.
81. As a game developer, I want a UI camera whose top left is the world's origin, one unit to a point, so that world coordinates are UI coordinates.
82. As a game developer, I want multisampling per camera, so that hard edges smooth at once.
83. As a game developer, I want to convert a click between the screen and the world through what the player saw, so that picking matches the picture.
84. As a game developer, I want entries culled per camera, so that a large map costs only what each camera sees.
85. As a game developer, I want a clip that is a rectangle of the world, so that a scrolled log stays clipped under every camera, zoom and DPI.

### Inspection, reloads and reports

86. As an agent, I want to ask for an inspection of the next frame and read each camera's batches and entries and what it culled, beside render's account of the same frame, so that I can tell why something is or isn't on screen.
87. As an agent, I want the inspection written as text, so that I can log it.
88. As a game developer, I want inspections to cost other frames nothing, so that the tool can stay attached.
89. As a game developer, I want to reload every changed image, font, mesh file and 2D material's shader with one call, so that I iterate on art without restarting.
90. As a game developer, I want each failure reported once, and again only after its cause was repaired and failed anew, so that the log isn't flooded and a new failure isn't hidden.

### Testing and performance

91. As a test author, I want a harness that runs canvas on render's fake GPU in a real kernel, so that my plugin's frames are tested without a GPU.
92. As a test author, I want to record a frame from my test without writing a plugin, so that a test states its entries where it asserts them.
93. As a test author, I want frames read back in canvas's terms (cameras, batches, entries, culls), so that a UI test asserts its button's entries without reading draws.
94. As a test author, I want to expect canvas's reports by kind, so that a test of a refusal passes and any other report fails it.
95. As a test author, I want pixel tests on wgpu's software adapter, so that slicing, profiles and pixel-perfect snapping are checked for real.
96. As a plugin author, I want canvas to allocate nothing in a steady frame once warm, so that drawing doesn't feed the garbage collector.

## Implementation Decisions

### The module and the plugin

([canvas: the feature list, where 2D ends and its users begin, and its name](https://github.com/dvoyni/cog/issues/864), [canvas on the kernel: its resources, tasks and parameters, and how its frame hangs off render's](https://github.com/dvoyni/cog/issues/873), with the naming rule from [canvas prototype: the calls a user makes to draw one 2D frame](https://github.com/dvoyni/cog/issues/871))

- **A module of its own,** `canvas`, importing kernel, platform, render, libs/m and libs/meshfile, and `go-text/typesetting`, pinned in its `go.mod`. It imports no ECS, and reaches no gfx: what canvas needs that render lacks is a delta to render.
- **The name** is `canvas`. "2D" was its codename while charting, and "2d" cannot be a Go package name. gfx's glossary avoids "canvas" for the Surface, which now points at the plugin; v1's canvas stays under `_legacy`.
- **`canvas.PluginName`** is `"canvas"`, listed after render and platform. A plugin that records into canvas is listed after canvas.
- **An immediate-mode API.** A user records entries every frame, and no sprite object persists. canvas buffers the frame's entries, sorts and batches them, records them into render, and forgets them. It cannot draw each call straight into render, because sorting and batching need the whole frame. Only its atlases, its glyphs and layouts, and the assets it loaded outlive a frame.
- **Shaped for an ECS plugin it does not import:**
  - an entry is a plain value, so a component can be exactly it: `Sprite`, `Shape` and `Mesh` are pointer-free, and their handles (`Image`, `MeshFile`, `Material`, `FontSet`) are plain, pointer-free, comparable values;
  - a bulk call sits beside each single one, so a query's chunk is recorded at once, its arguments included;
  - each recording task records into its own draw list, so parallel systems never contend.
- **Naming:** a struct that is only a call's parameter is named `…Options` (`ImageOptions`, `DrawTrianglesOptions`). A value an ECS component holds is named for what it draws: `Sprite`, `Text`, `Shape`, `Mesh`. Shapes and every other entry are literals, with no constructors: a constructor answering a shape left its sort key to be set afterwards.
- **Built to be built on.** The atlas, the batcher, text layout (shaping, wrapping and measuring without drawing), the 2D material contract and the camera's matrices are public pieces. canvas's own sprites, shapes and text use only those, so a user's tilemap or UI has the same power as the built-ins.
- **`Config`,** under `PluginName`:
  - `DefaultFilter`, the filter of a sprite or a mesh whose `Filter` is zero; its own zero is linear, so a pixel-art game sets `FilterNearest` once;
  - `RegroupedLayers`, the layers whose entries of equal keys canvas may regroup by batch;
  - `Language`, the current language at start, a tag such as `"en"`;
  - `DefaultFontSet`, a `FontSetDescriptor` for the zero `FontSet`; its zero is JetBrains Mono alone;
  - `TextSize`, the em of a run whose `Size` is zero, in world units; zero means 16;
  - `LayoutIdleFrames`, how many frames `DrawText` keeps a layout no draw uses; zero means 2;
  - `AtlasPageSize`, the atlas's page side, zero meaning 2048, a multiple of the atlas grid holding the white texel's cell of one texel rounded up to the atlas grid and its gutter, 12 at the default 3 mip levels and 5 at one, a side that is not raised to the next that is and reported once as `ReportKindAtlasPageSizeRaised`, and `AtlasLayerCeiling`, a ceiling on its layers, zero meaning 256, WebGPU's default limit on a texture's layers, since render answers no device's limits;
  - `OwnTextureSize`, the size past which an image gets a texture of its own, zero meaning half the page side;
  - `DoNotGenerateMipMaps`, and `MipLevels`, zero meaning 3, more than 12 lowered to 12 and reported once as `ReportKindMipLevelsLowered`, since 13 levels' smallest page would pass WebGPU's default limit of 8192 texels on a texture's side;
  - a distance-field glyph's default em and range, zero meaning 48 px and 16.
- **Resources it installs:**
  - `Fonts` and `Targets`, each an interface of reads alone;
  - `Handlers{RecordFrame, Recover, DeliverInspection}`, its subscriptions to `render.FrameEvent`, `render.RecoveredEvent` and `render.InspectedEvent`, one field each, for others to order their handlers around;
  - a private state: its atlases, caches, tables and draw lists.

  There is no `Tasks` resource, since canvas offers no task for others to schedule, and it declares no sockets.
- **Task parameters it offers,** each declaring render's and platform's locks on its user's behalf, as render's parameters declare gfx's:

  | Parameter | Locks | Carries |
  |---|---|---|
  | `DrawList` | canvas's state, `Fonts`, and through an `Inventory` it holds render's asset table, `gfx.Device` and `platform.FS`, all read | its task's draw list: every `Draw…` call and `SetCamera` |
  | `Assets` | the same reads, and through a `render.Frame` it holds render's frame state, read | `MakeMaterial` and the material setters, `Preload`, `Unload`, `ImageSize`, `RequestInspection`, `ReadInspection` and `ReloadChanged` |

  A draw list takes render's locks because an image's or a mesh file's first draw loads it. A recording task that also needs `Assets`, such as for `ImageSize`, declares both; the two take the same reads.
- **Every lock is a read,** so recording tasks run together; canvas's state is synchronized within, as render's is. The exceptions:
  - `Fonts`' writing side takes `Write[canvas.Fonts]`, with platform's FS read through `ElevateFonts`, so a task making a font set runs apart from every recording task;
  - canvas's own task copying `Targets` takes `Write[canvas.Targets]`.

  canvas's drawing declares render's `Frame` and `Inventory`, both reads, and reads canvas's state.
- **Events it publishes:** `FrameEvent`, inside its handler of `render.FrameEvent`, and `InspectedEvent`.
- **Events it subscribes to:** `render.FrameEvent`; `render.RecoveredEvent`, which is a fresh start (see *Device loss*); and `render.InspectedEvent`, after which it publishes its own `InspectedEvent` for a frame it was asked to inspect (see *Inspection*).
- **Sockets it binds:** `platform.ReadMountSocket`, its built-in assets embedded and mounted under `cog.canvas` at the lowest priority, as render binds `cog.render`: the include, the shipped materials' shaders and JetBrains Mono.

### The frame on the kernel

([canvas on the kernel: its resources, tasks and parameters, and how its frame hangs off render's](https://github.com/dvoyni/cog/issues/873))

```
render.FrameEvent ─ canvas's RecordFrame:
  after render's Ready, the job opening the frame for entries
  publishes canvas.FrameEvent{Beat, Ready, AcquireWait, GPULatency}, that job its Ready
    users' recording tasks, after e.Ready, joined into its out dependency
  after that out dependency:
    the sort task: the draw lists merged and the entries sorted once
    the drawing job: each camera order's command list in turn, culled, batched and recorded
    the Targets task, under Write[canvas.Targets]: each target's size and scale as drawn
  all joined into render's out dependency, so render's second pickup of loads
  and gfx's flush wait for canvas's drawing
```

- **canvas publishes `canvas.FrameEvent` inside its handler of `render.FrameEvent`,** as render publishes its own inside gfx's `DrawEvent`.
  - The event carries render's `Beat`, `AcquireWait` and `GPULatency`. Its `Ready` is canvas's job opening the frame for entries, scheduled after render's `Ready`: an entry is accepted from that job until the sort task closes the frame, which is how one recorded outside the frame is told.
  - Recording tasks hang off it: they run after its `Ready` and are joined into its out dependency.
  - Users subscribing to `render.FrameEvent` with canvas ordered after them was weighed. Every user would have had to order their handler before canvas's, and a task that forgot would record into a frame already drawn.
- **Drawing is a sort task, then one drawing job.**
  - The sort task merges the draw lists and sorts the entries once, and the frame's cameras into the order they draw.
  - The drawing job culls, batches and records each distinct camera order's command list in turn, and is joined into the out dependency. Cameras of one order share a list, and one temporary buffer of instance data.
  - A camera's layers are runs of the sorted entries, since entries sort by layer first, found by binary search, with no pass over the entries for each camera.
  - A job for each order, run in parallel, was weighed. The kernel schedules work only from event handlers, and canvas's handler runs before the frame's cameras are set, so it cannot know their orders; a task publishing an event starts a batch of its own, which render's frame would not wait for. The benchmarks of several cameras say whether a fixed number of drawing jobs, each claiming the next order, pays.
  - A small task after them takes `Write[canvas.Targets]` and copies each target's size and scale as drawn, so a reader sees one value for its whole run, and a task converting clicks waits only for the copy. Writing `Targets` under a read lock was weighed: a reader could see it change mid-run.
- **A camera's `Order` is a `render.Order`.** canvas begins one command list for each distinct camera order, placed among every plugin's lists.
  - A 2D world camera at 0 draws under a 3D scene at 100, and a UI camera at 200 over it.
  - An order another plugin's list holds, or one of render's own (`math.MinInt32` and `math.MaxInt32`), is refused by render and reported once.
  - One list at a `Config.Order` was weighed. It could not put 2D both under and over a 3D scene.
- **The frame's edge cases:**
  - The default camera draws only a frame that recorded entries without setting a camera.
  - A camera set with `Clear` clears its viewport even when the frame has no entries.
  - A frame with neither entries nor cameras draws nothing, so a game that lists canvas but draws a frame in 3D alone keeps it.
  - An entry or a camera recorded outside the frame is refused and reported once per task, as render refuses a command list begun while no frame is recording. That covers a task on the update beat, and a task hung off `render.FrameEvent` that runs after canvas's drawing has begun.
- **Only one frame records at a time,** as render's `Frame` reaches the frame being recorded, so a draw list holds the current frame's entries, with no keying by frame.

How a plugin's recording task reads, from the prototype with its verdicts applied:

```go
func drawUnits(k *kernel.Kernel, e canvas.FrameEvent, list canvas.DrawList, game kernel.Read[Game]) kernel.NoResult {
	g := game.Get()
	list.SetCamera(canvas.Camera{
		Position: g.Hero, Size: m.Vec2{Y: 360},
		Layers: canvas.MakeLayers(canvas.LayerRange{From: layerGround, To: layerLabels + 1}), Clear: true, ClearColor: g.Sky,
	})
	for _, unit := range g.Units {
		at := m.Translation2x3(unit.Position.X, unit.Position.Y)
		order := canvas.OrderOf(unit.Position.Y)
		list.DrawSprite(canvas.Sprite{Image: g.Shadow, Transform: at, Tint: m.Color{A: 0.4}, Layer: layerUnits, Order: order - 1})
		list.DrawSprite(canvas.Sprite{Image: g.Unit, Transform: at, Layer: layerUnits, Order: order})
		list.DrawTextWith(canvas.Text{
			String: unit.Name, Size: 12, Pivot: canvas.PivotBottom,
			Transform: m.Translation2x3(unit.Position.X, unit.Position.Y-36), Layer: layerLabels,
		}, canvas.Outline(2, g.Black))
	}
	return kernel.NoResult{}
}
```

### Draw lists, entries and the sort key

([canvas: the feature list, where 2D ends and its users begin, and its name](https://github.com/dvoyni/cog/issues/864), [canvas: sprites, their transforms, and how they are ordered and batched](https://github.com/dvoyni/cog/issues/867), [canvas on the kernel](https://github.com/dvoyni/cog/issues/873))

- **An entry** is one thing recorded into a draw list for a frame, with its sort key: a `Sprite`, a `Shape`, a run of `Text`, or a mesh, a `Mesh` of a mesh file or triangles given for that frame alone. A frame's entries are forgotten once drawn.
- **`canvas.DrawList` is a task parameter whose `Declare` gives each registered task a draw list of its own,** as ecs's `CommandBuffer` gives each system one.
  - The lists are ordered by registration: plugin order, then the order each plugin registers its tasks. That order breaks ties after layer and order, so there is no number to choose.
  - A task whose jobs run twice in one frame records both into its draw list, in the order the jobs run. A game that needs them ordered gives their entries different layers or orders.
  - A `Frame` parameter beginning lists at orders the user picks, as render's command lists are, was weighed. It needed a unique number per list for a tie-break that rarely shows.
- **The sort key is `Layer int32`, then `Order int32`, then the draw list's order, then the recording order.** The order is strictly painter's, with no depth buffer.
  - `Layer` and `Order` make one 64-bit key. The lists are joined in list order and sorted stably by it, so the last two parts cost no bits.
  - A radix sort, a byte to a pass, skips the passes whose digit is alike for every entry, such as the high bytes of a few layers.
  - Each entry carries its place among the merged lists through the sort, so the inspection still answers its draw list's place and its own.
  - `int32` rather than `int`, which would widen the key to 96 bits, a third more passes.
  - Prior art agrees: every engine orders by a few integer keys and then painter's order.
- **`OrderOf(y float32) int32`** maps every float onto its own `int32` in the same order, for y-sorting with no scale to choose: `Order: canvas.OrderOf(position.Y)`. −0 sorts as +0, and NaN after every number.
  - It keeps the order, not the distance, so `OrderOf(y)-1` sorts just under `y`, as a unit's shadow does.
  - Y-sorted entries and hand-stacked ones belong in different layers, since `OrderOf(1)` is 1065353216.
- **Equal keys keep their call order** by default. A layer named in `Config.RegroupedLayers` may regroup its entries of equal keys by batch, for a UI or particles whose order within a key does not matter. v1 never merged entries that were not adjacent.
  - A run of equal keys regroups in the order its batches first appear: the entries of its first entry's batch, then those of the first batch left, each batch's in call order. Runs of different keys never mix.
- **Bulk calls take parallel slices,** `DrawSprites([]Sprite)` and `DrawSpritesWith([]Sprite, []T)`, and the same for meshes, so an ECS chunk's entry and argument columns go in as they are. The slice's order is the recording order. Slices of different lengths are refused and reported once.

### Sprites, transforms and pivots

([canvas: sprites, their transforms, and how they are ordered and batched](https://github.com/dvoyni/cog/issues/867), with `TexelScale` from [canvas: what v1 offered that v2's design lacks](https://github.com/dvoyni/cog/issues/878))

- **A `Sprite`** draws an image's region through a transform about its image's pivot: `Sprite{Image, Frame, Transform, Size, Tint, Material, Crop, Clip, DrawMode, TexelScale, Filter, Layer, Order}`, recorded with `DrawSprite`, `DrawSpriteWith`, `DrawSprites` and `DrawSpritesWith`.
- **An entry's transform is an `m.Mat2x3`,** a new type in libs/m. It has 2 rows and 3 columns, since m indexes elements by (row, column), as mathgl names it, and it is stored column-major like the rest of m.
  - Its 24 bytes are WGSL's `mat3x2<f32>` byte for byte, so it is copied into the instance data as it is. WGSL names the columns first, which the type's doc comment says.
  - `m.Mat3` keeps its one meaning, the linear part of a 3D transform (`Scaling3`, `RotationZ3`, `Quat.Mat3`). A homogeneous `Mat3` would take 48 bytes in the instance data, and would allow a bottom row that canvas would have to honour or drop.
  - libs/m gains its multiply, inverse and point transform, `Translation2x3`, `Rotation2x3`, `Scaling2x3` and `TRS2x3(translation, rotation, scale)`, and `.Mat3()` for the homogeneous form. `a.Mul(b)` applies `b` first.
  - A zero transform means identity.
  - v1 kept the parts and composed them in the vertex shader. A parts struct is the ECS plugin's, which writes the matrix into the entry.
- **`Size`** is in world units, zero meaning the frame's size in texels, and a zero on one axis keeps the aspect ratio, as in v1. It is apart from the transform's scale because a sliced or tiled sprite keeps its corners and its tiles at their texel size within it.
- **No hierarchy.** Parenting is the user's or the ECS plugin's, composed into the matrix.
- **Mirroring is a negative scale** about the image's pivot, so there is no flip. canvas culls no faces, so the reversed winding still draws, and a sliced sprite mirrors its corners with it. A material reading a direction in the image's space sees it mirrored, as it already sees it rotated.
- **The pivot is the image's:** `ImageOptions.Pivot`, a `Pivot{Fraction, Texels}`, the two added.
  - `Fraction` is a fraction of the frame's size from its centre, ±½ at its edges, and `Texels` is added in texels. "Bottom centre, 3 texels up" is `Pivot{Fraction: m.Vec2{Y: 0.5}, Texels: m.Vec2{Y: -3}}`.
  - Zero is the centre. The named pivots are `PivotCenter`, `PivotTopLeft`, `PivotTop`, `PivotBottom`, `PivotBottomLeft` and `PivotBottomRight`.
  - It is measured in the image's own space, x right and y down as its rows run, so it does not wait on the camera's axis.
  - A sheet has one, measured within each frame. Per-frame pivots come with sidecar formats, later.
- **No entry has an origin of its own.** An entry turning about another point composes a translation into its transform.
- **A converter's output bounds place it.** Size and pivot are measured on the converter's input. The region drawn is its output, at its bounds within the input, as Go's `image.Image` carries them: a `SubImage` keeps its coordinates, and a grown image's `Bounds().Min` is negative.
  - A trimmed sprite stays where it was.
  - A halo entry over `ImageFromFile(path, ImageOptions{Convert: DistanceField{Spread: 8}})`, given the sprite entry's transform and size, lines up at any pivot and size, its field reaching 8 texels beyond the art. Without the rule it would line up only with a centred pivot at the natural size.
  - A sheet's converter runs on each frame apart, measured on it, so a distance field never reads a neighbour's edges.
- **`Tint`** is a straight-alpha `m.Color`, premultiplied by canvas, zero meaning white. It only multiplies. v1's tint ramp, neutral at `#6D6D6D` so that it could lighten, is gone: a flash to white is a material that takes its amount as an argument.
- **`Material`:** zero means the default material.
- **`Crop{Left, Top, Right, Bottom}`** is the fraction of the full rectangle left out from each side, zero drawing all of it. A cropped entry draws part of its full rectangle and keeps its pivot, so a fill bar shrinks from one side rather than re-centring.
- **`Clip`** is a rectangle of the world, zero meaning none, per entry rather than a stack (see *Clips and culling*).
- **`DrawMode`** is `Stretched`, `Sliced` or `Tiled`.
  - **Nine-slice insets** are `ImageOptions.Slice`, an `Insets` in texels, and `ImageOptions.SliceHollow` draws nothing in the centre cell, for frames, as v1's `NineSliceNoCenter` did.
  - **A sliced sprite is one instance.** The fragment stage remaps its coordinate piecewise and samples with `textureSampleGrad`, so it can rotate. v1 sliced on the CPU into up to nine instances, with rotation and origin zeroed.
  - **A tiled sprite** wraps within its region, whose gutter wraps on the axes its image tiles along, so it tiles without seams, as v1's did. The shader samples with gradients of the unwrapped coordinate, so the seam does not drop to the smallest mip.
  - **`TexelScale`,** zero meaning 1, is the scale a sliced sprite's corners and a tiled sprite's tiles take their texels at, for art drawn at 2× or a UI's own scale; a stretched sprite ignores it. A sprite has one draw mode, so it is never sliced and tiled at once, and one field serves both. v1's separate flags allowed both, an exclusion it documented without enforcing.
  - **canvas's sampling helper is the one documented way** for a material to sample an entry's image, written in canvas's material guide. The remapping cannot move to the vertex stage, since tiling wraps per pixel, and v1's [canvas: wrap a tiled sprite's uv inside its atlas sub-rect in the shader, so tiling no longer leaves the atlas](https://github.com/dvoyni/cog/issues/440) showed that a custom fragment stage sampling on its own silently stretches tiled and sliced sprites.
- **`Filter`** is `FilterLinear` or `FilterNearest`, per entry, as v1 chose it per sprite. Zero, `FilterDefault`, means `Config.DefaultFilter`, for sprites and meshes alone; a run's zero filter is its font's (see *Text: runs and layouts*). It is not the image's, since a filter is baked into no pixel, and an image with two filters would be packed twice.
- **The material argument** goes through `DrawSpriteWith` and `DrawSpritesWith`, checked at the draw (see *2D materials*).
- **Edges are hard,** sprites' and shapes' alike. A camera's multisampled target, which render resolves, smooths every entry at once.

### Images, the atlas, regions, gutters and mips

([canvas: auto-atlasing, what is packed and when, and how a sprite names its region](https://github.com/dvoyni/cog/issues/866), with `SourceRect` and `ImageSize` from [canvas: what v1 offered that v2's design lacks](https://github.com/dvoyni/cog/issues/878), and `ImageFromTexture` from [canvas prototype](https://github.com/dvoyni/cog/issues/871))

**Naming and residency**

- **An `Image`** is a path in the FileSystem and its `ImageOptions`, interned into a plain, pointer-free, comparable value by `ImageFromFile(path, options…)`, or a render texture and its options, by `ImageFromTexture` (see *A render texture is not packed*).
  - Making one loads nothing. Each `ImageFromFile` takes the table's lock and hashes its path, so a game makes an `Image` once, as it names its art, and keeps it, rather than calling `ImageFromFile` for each entry it draws.
  - `ImageFromFile` takes at most one set of options, so the common case reads `ImageFromFile("water.png")`. A second set is reported once and ignored.
  - Its options are its colour space (`ColorSpaceSrgb`, the zero, or `ColorSpaceLinear`), `Wrap` for the axes it tiles along, a sheet's `Grid{Columns, Rows}`, its `Pivot`, `SourceRect`, `Slice` and `SliceHollow`, and a converter.
  - Images are never released; a game's paths are a bounded set.
  - Every resource is a path: bytes that are not files go into a memory mount, and a user's generated pixels are loaded that way too.
- **A sprite names its `Image`, and a `Frame` for a sheet,** counted along its rows, as v1 named sprites by path.
  - **Its first draw loads it.** render picks up a load requested during recording at its second pickup point, so the image is decoded, packed and drawn in that frame. The first frame recording it loads it even when every camera culls it, since its bounds need its size.
  - **The decode lands on that frame.** `Assets.Preload(image)` from any task moves it, for a loading screen.
  - **A failed load** draws render's magenta and is reported once.
- **canvas owns residency.** There is no acquire, no release and no count.
  - An image stays from its first draw until it is unloaded by path or prefix, `Assets.Unload("level1/")`, as v1's `UnloadSprite` did. A later draw loads it again. A prefix is a folder, ending in a slash, so `"level1/"` leaves `"level10/"` as it is.
  - Otherwise, when its atlas is at its ceiling, images are evicted by last use, never one drawn this frame. Those last drawn in one frame go in their paths' order.
  - `Assets` holds read locks alone, so `Preload` acquires the file from render at once, and `Unload` is queued: the next drawing job, the one task writing canvas's renderer, frees the regions and takes the preloaded sources before it packs that frame's images. An image unloaded in a frame that draws it is loaded again in that frame.
  - Glyphs are cached and evicted the same way.
- **Options that change no pixel are not packed.** The pivot, the slice insets and `SliceHollow` are not part of what is packed, so images that differ only in them share their regions.
- **Sheets:** each frame is packed as a region of its own, with its own gutter, so frames never bleed into one another under filtering, and each tiles and takes mips cleanly. v1 named frames as pixel insets into one packed image. Sidecar formats such as Aseprite's or TexturePacker's are a user's, or later work.
- **Any rectangle of a file:** `ImageOptions.SourceRect`, in texels, zero meaning the whole file.
  - The image is that rectangle, packed as a region of its own with its own gutter, as a sheet's frames are. Its natural size is the rectangle's, and its pivot is measured within it.
  - A `Sheet` grid may cut a source rectangle, for a file holding several sheets. Several rectangles of one file decode it once.
  - An irregular sheet is one image per rectangle, and a tiled nine-slice is nine.
  - A sheet's zero `Columns` or `Rows` is one, so `Grid{Columns: 8}` is a strip. Each frame is the rectangle's size divided by the grid, texels left over unused.
  - A `SourceRect` reaching past its file, or one whose frames hold no texel, and a sprite's `Frame` past its sheet's last draw nothing, culled as an entry no camera sees, reported once for its path as `ReportKindFrameOutOfBounds`; `Assets.ImageSize` answers false for them. Drawing the whole file instead was weighed: a sheet is often large enough to need a texture of its own, and a bad frame is an addressing mistake, not a missing asset, which draws render's magenta. Unloading the path looks again at its next draw. A cell is keyed by the rectangle of the file it holds and its wrap, so images of equal rectangles written two ways share it.
  - v1's per-draw insets were weighed. A rectangle cut at the draw can't be packed apart, so it would bleed under filtering and couldn't tile.
  - The option is not called *region*: a region is a rectangle of an atlas, where an image is packed.
- **An image's size before it is drawn:** `Assets.ImageSize(image) (m.Vec2, bool)`, as v1's `SpriteSize` was.
  - It reads only the file's header, with `image.DecodeConfig` through platform's FS, cached per path.
  - It answers the size in texels of a sheet's frame; of a converted image's input, since size and pivot are measured on the input; and of a render-texture image's texture, through render's `Inventory.TextureDescriptor`.
  - A missing file answers false and is reported once.
- **A render texture is not packed.** `ImageFromTexture(texture, options…)` interns one into an `Image`, drawn through a 2D-array view of its own, of its mip level 0 and layer 0 alone, where a camera into it draws. It takes its colour space from its options as any image does, so a Unorm texture holding encoded colour is marked `ColorSpaceSrgb` (see *Blending in the target's space*).
  - Its size, and the rectangle a `SourceRect` or a sheet's frame cuts from it, come from render's `Inventory.TextureDescriptor` at its first draw, its texels taken as premultiplied, as a camera draws them.
  - Its view is the batch's texture, so it breaks a batch as a change of atlas does.
  - A texture render does not hold at its first draw draws nothing, reported once for the texture as `ReportKindImageTextureRefused`, and `Preload` loads nothing for it.
  - A view of every level was weighed: a camera draws level 0 alone, so a sprite drawn smaller than its texture would read levels no camera wrote.

**Atlases and pages**

- **An atlas holds one format.**
  - canvas keeps a sprite atlas for each colour space a load asks for, sRGB by default, each made the first time it is needed.
  - Glyphs have an atlas of their own (see *Text: glyphs*).
  - Distance fields share the sprite atlas, since alpha stays linear in an sRGB format.
- **The white texel** that shapes draw over is reserved in the sprite atlas when it is made, and never released, so shapes batch with sprites. v1 did the same.
- **An atlas is one 2D-array texture, and each layer is a page.** A region's page travels in its instance data, so entries on any page of one atlas share one binding and batch. Only a change of atlas breaks a batch.
- **Growing:** a full atlas doubles its layer count. canvas makes a new array and copies the old layers into it on the GPU, so an atlas is always one texture. The copy is of every mip level of each old layer, so pages packed before the growth keep their mips. An atlas growing again before its copy is recorded copies its old layers once, into its last array. The page side and the ceiling on layers are `Config` fields. Under WebGPU's defaults (8192², 256 layers) memory bounds a page first: one RGBA8 layer at 8192² is 256 MiB.
- **Overflow:** some images get a texture of their own:
  - one larger than the `Config` limit, zero meaning half the page side;
  - one that does not fit once the atlas is at its ceiling and nothing can be evicted.

  Their texture is a one-layer array, so the same shaders draw them, exactly their size with no gutter, so an image as large as a page takes a texture of a page's size, not a larger one: the sampler's clamping at the texture's edge continues its edge as a gutter would, and its mips are the whole texture's. A tiled one wraps through its sampler's address mode instead. Nothing is refused for good: a full atlas costs batches, not pixels, and it is reported once. v1 cached its refusals.
- **The allocator** is a shelf allocator that frees each region and merges free neighbours, as etagere does under WebRender and glyphon. No MaxRects or skyline implementation frees one rectangle, and v1 reused a slot only for an image of the same padded size. Compaction is not built now; regions are found through canvas's table, so they can move later.
- **The atlas type is public,** for users building caches of their own, such as a UI's rasterised text. canvas's sprites and glyphs are built on it.

**From pixels to pages**

- **A plain image takes the GPU path.**
  - Its source is loaded through render's `AcquireTexture`, so render's loader, sharing and failures serve it unchanged.
  - canvas fills its padded cell with one draw, premultiplying it, so filtering a cut-out edge never darkens it. The fill clamps or wraps the coordinate per axis, so the same draw writes the gutter. Each page packed into that frame is one pass, a draw for each cell, recorded at the start of the frame's lowest camera order's command list, before any camera's pass, so every camera sampling the atlas draws after it. A frame drawing nothing keeps its packings for the next that draws.
  - The source is released afterwards.
  - **Packing waits for the drawing job.** A batch key names its atlas, known only once its image is packed, so the drawing job finds each entry's batch key as it packs the frame's new images, and regroups the sorted entries by them; the sort task sorts by key alone.
- **Converters:** `ImageOptions.Convert` is a `Converter`, an interface with `Convert(image.Image) image.Image`, implemented by comparable values.
  - `canvas.DistanceField{Spread}` bakes a distance field (see *Distance fields and profiles*), so the SDF pipeline runs through the general one.
  - Equal converters share an image. One that is not comparable is refused at `ImageFromFile` and reported.
  - A converted image is read, decoded and converted on the CPU in the job that first asks for it, as render compiles a shader in its caller's job. It is then made into a texture with `MakeTexture` and packed like any other.
  - A user's own converters are plain structs: trimming, palette swaps, outline extraction.

**Gutters and mips**

- **The gutter** is extruded, its edge continued, by default. It is wrapped from the opposite edge on an axis an image marks as tiling, so tiling wraps in the shader without seams. A tiled and a plain load of one file are different images.
- **Mips are generated unless `Config.DoNotGenerateMipMaps` is set,** which suits pixel-perfect games. With it set, a page has one level and a 2-texel gutter, packed with no atlas grid.
- **`Config.MipLevels`, zero meaning 3, fixes the gutter and the atlas grid,** both 2^(levels−1) texels: 4 at the default.
  - An image's left and top edges sit on an atlas grid line.
  - Its right and bottom gutters reach the next atlas grid line plus the gutter.
  - Bilinear sampling at the deepest level then reads only the image's own texels and gutter.
  - Each page is made with exactly that many levels, so nothing samples a level that mixes cells.
  - The page side must be a multiple of the atlas grid holding the white texel's cell; `Config.AtlasPageSize` is raised to the next side that is, and reported once.
  - At one level the atlas grid is 1 and the gutter 2, as with `DoNotGenerateMipMaps`: the gutter is the atlas grid, but never less than the 2 texels linear filtering needs.
  - The white texel's cell is written opaque white at every level as the atlas is made, since no mip pass runs before the first frame's.
  - An image's own texture has the page's levels, or as many as its size has, its mips the whole texture's.
  - A linear filter reads between levels linearly too; a nearest one takes the nearest level.

  | Levels | Smallest clean scale | Gutter = atlas grid | 16×16 cell | 64×64 cell | 256×256 cell |
  |---|---|---|---|---|---|
  | 2 | ½ | 2 | 20² | 68² | 260² |
  | 3 | ¼ | 4 | 24² | 72² | 264² |
  | 4 | ⅛ | 8 | 32² | 80² | 272² |
  | 5 | ¹⁄₁₆ | 16 | 48² | 96² | 288² |

- **Each image's chain comes from its own texels.** Every 2×2 block the mip filter averages lies inside one cell, so render's mip pass over a whole page is as clean as a chain per image. Box-filtering a whole atlas would otherwise mix images. canvas regenerates the mips of the layers it packed into, once per frame, with render's `CommandList.GenerateMips` after the passes that packed them (see *Delta to render*). A frame's packing passes and their mips are recorded at the start of its lowest camera order's command list, before any camera's passes, so every camera sampling the atlas draws after them.
- **A gutter is for linear filtering;** an entry's filter is its own.

### Shapes

([canvas: sprites, their transforms, and how they are ordered and batched](https://github.com/dvoyni/cog/issues/867), [canvas prototype](https://github.com/dvoyni/cog/issues/871), [canvas: what v1 offered that v2's design lacks](https://github.com/dvoyni/cog/issues/878))

- **A `Shape`** fills or strokes a rectangle, or draws a line: `Shape{Kind, Rect, From, To, Width, Radius, Transform, Tint, Material, Clip, Layer, Order}`, its `Kind` `Fill`, `Stroke` or `Line`, recorded with `DrawShape` and `DrawShapeWith`. It is a literal, `canvas.Shape{Kind: canvas.Fill, Rect: r, Tint: c, Layer: l}`, like every other entry.
- **Shapes are sprites over the sprite atlas's white texel,** as v1's were. They take what a sprite takes (a transform, a colour as tint, a material and its argument, the sort key and a clip) and batch with sprites.
- **A fill is one sprite. A stroke is four fills inside the rectangle that do not overlap,** so a translucent colour does not darken its corners. **A line is one rotated fill** centred on its segment.
- **`Width` is in the shape's own space,** as its rectangle is, which its transform maps into the world: world units under a transform that does not scale. Zero means a hairline one target pixel wide at any zoom and any transform, the vertex stage sizing the thin axis from the camera's pixel size and the transform's columns. v1 drew nothing at zero.
- **A shape's instances share the sprite's instance data,** read by canvas's one vertex stage: each holds the part of its unit square it draws (the whole, a line's band, or one of a stroke's sides), the square's size in the shape's own space, its width and its radius. A stroke's top and bottom span its rectangle and its left and right the height between them, each as thick as its width, or its radius where that is larger, so a rounded corner's curve lies within them.
- **Distance effects around shapes:** the SDF material reads a shape's distance analytically from its rectangle, as v1's halo on fills did, so `DrawShapeWith(Shape{…, Material: canvas.SDFMaterial}, canvas.Glow(8, gold))` glows a button.
  - A shape drawn with a profile grows its quad by the profile's outer reach. This is the one place a quad grows, since a sprite's distance field is baked larger.
  - The same distance gives `Shape.Radius`, in the shape's own space as `Width` is: rounded corners for fills and strokes, which neither version had. The include's `canvas_shape_distance` answers a fragment's signed distance from its shape's edge in the shape's own space (a fill's rectangle, a stroke's band, a line's band), which the SDF material reads, and `canvas_covers` whether a rounded shape covers it. A material discards the fragments it does not cover, after sampling, as the default material does; one that does not draws rounded shapes square, which the include says. The edge is hard, as every entry's is.
  - A user baking a distance-field image per box was weighed. Every size and radius would have been a new image.

### 2D materials, material arguments and blending

([canvas: what a 2D material is, and how a distance-field outline is one](https://github.com/dvoyni/cog/issues/865), [canvas: the 2D camera, its projection, viewport modes and several cameras](https://github.com/dvoyni/cog/issues/869), spelt in [canvas prototype](https://github.com/dvoyni/cog/issues/871))

- **A 2D material is canvas's `Material`, a plain handle,** pointer-free and comparable, so an ECS component can hold it.
  - It is made from a descriptor: `Assets.MakeMaterial(MaterialDescriptor{Label, Shader, VertexEntryPoint, FragmentEntryPoint, Blend, Argument, CullMargin})`, and given back with `ReleaseMaterial`.
  - canvas makes the render material underneath, which its users never hold.
  - Its material parameters at `@group(1)` are set with canvas's own setters on `Assets`, `SetMaterialTexture`, `SetMaterialUniform[T]` and the rest, as render's. A draw uses them as they stand when canvas draws at the frame's end, which the setters' doc comments and this design say.
  - The zero `Material` is the default material.
- **Its shader is a whole WGSL file,** a path, that includes canvas's include from the `cog.canvas` mount; nothing is spliced into the code. A material with only a fragment entry point uses canvas's vertex stages: a sprite's, and a `canvas.Vertex` mesh's.
- **One model for every entry, split only at the vertex stage.** Every kind of entry's vertex stage writes the same varyings, so a fragment-only material serves sprites, glyphs and meshes alike. A glyph is a sprite drawn with the text material, as Godot runs sprites, text and meshes through one `canvas_item` shader.
- **A sprite has no vertex buffer.** Its corners come from `vertex_index`. Its instance data is an element of a storage buffer at `@group(2)`, read at `instance_index`: canvas's fixed part, then its material argument.
- **The material argument** is the value each entry gives its material, of the one type the material declares with `ArgumentOf[T]()`: a struct with any number of fields, a plain value such as a `float32`, or none.
  - **Reading it:** the fragment stage reads it from the instance data, by the instance's index the vertex stage passes as the flat varying `instance`. Varyings therefore never cap it; only its size does, which every instance of its stride pays. A material that needs a lot of data per entry keeps an index in its argument into a buffer it holds as a material parameter.
  - **Its layout** is checked against the shader's WGSL `Argument` struct with render's `Inventory.CheckLayout[T]`, binding `canvas_instances` at the path `[].argument` (see *Delta to render*), by render's rules for uniform blocks: the size, then each field's offset in order, names ignored and `_` fields treated as padding. It is checked once per Go type and shader, and a mismatch is reported once.
  - **Its type, at the draw:** a draw's argument type is compared with the material's by `DrawSpriteWith[T]` and the other `…With` calls. An argument the material does not declare, or one of another type, is refused and reported once, and the entry is skipped.
  - **Without one:** a plain draw on a material that takes an argument gives it the zero value, with the instance data's tail zeroed. Each material documents what its zero draws.
  - **In ECS:** a user's argument type is a component, paired in a query with the `Sprite` component that holds the `Material` handle.
  - **The name:** an entry supplies the argument for what its material declares, as a call's argument fills a parameter. render's *material parameter* is shared by every draw of a material; the glossary keeps the two apart.
  - **Why:** no engine lets a user add a member to its per-sprite record, each offering one small per-instance value instead (Unity's 32-bit user value, Bevy's `MeshTag`, Godot's instance uniforms), and v1's record was frozen at 96 bytes. Bevy 0.20's sprite material, cached by value, breaks batches without bindless, v1's "material keyed by its values". An argument is a struct of the material's own, and is no part of the material, so it breaks no batch.
- **Blending is premultiplied throughout.**
  - A material's `Blend` is a `render.BlendState` whose colour is premultiplied. Zero means normal, unlike render's zero, which blends none.
  - canvas tests the whole value against zero, not its operation alone, since render's components leave a zero operation meaning add.
  - Constructors give the usual modes, and any other state is taken as it is:

    | Constructor | Factors |
    |---|---|
    | `BlendNormal()` | One, OneMinusSrcAlpha |
    | `BlendAdditive()` | One, One |
    | `BlendMultiply()` | Dst, OneMinusSrcAlpha |
    | `BlendScreen()` | One, OneMinusSrc |
    | `BlendOpaque()` | One, Zero, Add written out in full, which render keys as no blending |

  - Under normal blending, an entry that outputs alpha 0 adds its colour, so glows batch with the rest. A material of another blend is made from one of canvas's shipped shaders, so canvas ships no material per blend.
  - The rest of render's state is canvas's: no depth test, since the sort key orders entries, and no culling.
- **`CullMargin`,** in world units, zero meaning none, grows the bounds of a material's entries, for a vertex stage that moves vertices, such as wind sway or a shockwave.
- **No time.** render's frame carries none, and game time stops when the game pauses while real time does not, so a material that animates takes its time as a material parameter, set by whichever task owns its clock. A value every draw of a material shares is a material parameter, and a value per entry is its material argument.
- **canvas ships three embedded materials:**
  - **default,** the zero `Material`: texture × tint; shapes draw with it over the white texel;
  - **`SDFMaterial`,** a distance field drawn through a `Profile`, its argument;
  - **`TextMaterial`,** a run's when its `Material` is zero, built on the same include, its argument the `Profile` drawn under the fill.

### Distance fields and profiles

([canvas: what a 2D material is, and how a distance-field outline is one](https://github.com/dvoyni/cog/issues/865), [canvas: auto-atlasing](https://github.com/dvoyni/cog/issues/866), [canvas: sprites, their transforms, and how they are ordered and batched](https://github.com/dvoyni/cog/issues/867))

- **Distance-field effects are one profile.** Outline, halo, glow and drop shadow each map a distance to a colour over a distance field. They differ only in the curve and the blending:

  | Effect | Constructor | Curve | Blending |
  |---|---|---|---|
  | outline | `Outline(width, color)` | a hard band | normal |
  | halo | `Halo(plateau, reach, color)` | solid out to a plateau, then falling off | normal |
  | glow | `Glow(reach, color)` | falling off from the edge | added |
  | drop shadow | `DropShadow(offset, softness, color)` | a soft band, offset | normal |

  A `Profile{Inner, Outer, Plateau, Falloff, Color, Offset}`, in field texels, is the SDF material's argument. Every outline and glow in a frame therefore batches, each entry with its own colour.
- **The drop shadow's offset is in world space,** the space the transform maps into. The vertex stage turns it into the image's space through the inverse of the transform's linear part, so every entry's shadow falls the same way, mirrored or not, and zooms with the world.
- **No underlay and no declared reach.** v1 collected halos on one layer under the images, and its two-layer idiom clashed with painter's order. canvas has no such concept.
  - A halo is two entries: the SDF material over the sprite's baked distance field, then the default material over the sprite.
  - Keeping halos behind overlapping images is the user's choice of layer and order.
  - The baked field holds its spread in its own pixels, so it is simply a larger sprite, and nothing has to grow a quad.
- **`DistanceField{Spread}` bakes a single-channel distance field** from an image's alpha, on the CPU, writing the distance into alpha. It runs as a converter at the image's first use. A small command bakes fields at build time, optionally, for shipping them pre-baked.
- **A field made from pixels can only be single-channel;** MSDF needs vector outlines. MTSDF glyphs, should they come, keep their true distance in alpha too, so one SDF material reads `.a` from both.

### canvas's include and its groups

([canvas: the 2D camera, its projection, viewport modes and several cameras](https://github.com/dvoyni/cog/issues/869), [canvas: what a 2D material is](https://github.com/dvoyni/cog/issues/865), spelt in [canvas prototype](https://github.com/dvoyni/cog/issues/871))

- **The groups:**

  | Group | Holds |
  |---|---|
  | `@group(0)` | the camera's pass parameters, set once per camera pass |
  | `@group(1)` | the 2D material's parameters |
  | `@group(2)` | canvas's instance data, read at `instance_index`, and the atlases a batch samples, an atlas changing from batch to batch |

  A custom shader declares only `@group(1)`, and the struct of its material argument.
- **One include, `cog.canvas/canvas.wgsl`, declares every binding of groups 0 and 2,** with the functions over them: world to clip, snapping, a mesh's varyings with the region remap, the sampling helper and the output's encoding.
  - **The sampling helper answers the image premultiplied,** reading the atlas's texels as they are, since canvas packs an image premultiplied (see *From pixels to pages*). Premultiplying after sampling was built first, while each image drew from render's texture of its file, of straight alpha: linear filtering then mixed a cut-out edge's colour with the black of its transparent neighbours before premultiplying, fringing the edge dark. The quads canvas draws over a viewport read through a shader of their own, `cog.canvas/viewport.wgsl`: a camera's clear and what a multisampled camera resolved, premultiplied already and drawn as they are, and the packing of an image into its cell, which premultiplies it. canvas's own materials include it as a custom one does, so nothing of the camera is hard-coded where a custom shader cannot reach it. render interns layouts across shaders sharing an include, so every 2D material's pass parameters are one bind group, set once per camera pass.
- **The instance data**, `CanvasInstanceData`, is canvas's fixed part (its transform as `mat3x2<f32>`, its premultiplied tint, its region and its page, whether `canvas_sample` decodes its image from sRGB, and what its draw mode, slice insets and crop need), then the argument. Its storage binding is `canvas_instances`.
- **The include's instance data type names the shader's `Argument` struct,** since WGSL's declarations may come in any order. A material with no argument declares none, and canvas preprocesses a material's shader that has one with the define `CANVAS_ARGUMENT`, which the include's `#if` reads, since WGSL forbids an empty struct.
- **The pass parameters:**
  - `world_to_clip`, a `mat3x2<f32>` from the world to the viewport's clip space, the fit, zoom, rotation and pivot in it, and `clip_to_world`, its inverse, for an effect that needs a fragment's world position;
  - `pixel_size`, one target pixel in world units, for a hairline's width and distance fields' anti-aliasing at any zoom;
  - `viewport_origin` and `viewport_size`, in pixels, mapping a fragment's `@builtin(position)` into its viewport;
  - `snap`, the grid entries snap to, zero unless the camera is pixel-perfect;
  - `encode`, whether the output encodes sRGB.

  Nothing of the user's is in the pass.
- **Every vertex stage writes the same varyings,** sprites' and meshes' alike: the position, the coordinate in the atlas and over the image's region, the premultiplied tint, and the flat `instance`, its instance data's index, so a fragment-only material serves both.
- **A custom vertex stage that skips the snapping function draws its entries unsnapped,** which the include's docs say.

### Text: glyphs

([canvas: fonts and text, from glyph representation to layout](https://github.com/dvoyni/cog/issues/868), [canvas: what v1 offered that v2's design lacks](https://github.com/dvoyni/cog/issues/878); findings in [canvas research: glyphs as bitmaps, SDF, MSDF or MTSDF, and shaping in Go](https://github.com/dvoyni/cog/issues/862))

- **A glyph is a single-channel distance field in R8 by default.** The SDF material's profiles need the true distance, which bitmaps and plain MSDF lack. A single channel costs a quarter of MTSDF's texels, which CJK multiplies (3,500 hanzi at a 32 px em are about 24 MB as MTSDF), and needs no port of msdfgen.
- **A font may be a bitmap font instead:** its `Bitmap` is a pixel size, for pixel fonts, which a field would round. It draws no distance effects. A one-pixel outline on a pixel font would be a material sampling neighbouring texels, not built now.
- **The field's em and range are a font's,** `FieldEm` and `FieldRange`, zero meaning the `Config` defaults, whose zeros mean 48 px and 16, Godot's defaults. They allow outlines up to 8 field pixels and clean text down to about 6 px. A CJK font takes a larger em, such as 64, since thin strokes need two field texels across, as MapLibre and `gogpu/gg` double theirs.
- **The glyph atlas is R8Unorm, linear,** with a 1-texel transparent gutter, since a field's range already fades to zero at its cell's edge, and no mips. Bitmap glyphs share it, and the text material reads their coverage rather than a distance.
- **Glyphs are made at run time, at their first use,** in the job recording the text, ready that frame as an image is.
  - The outline is rasterized anti-aliased at the field's em with x/image's vector rasterizer, then the distance transform behind `DistanceField` runs over the coverage, as TinySDF does. canvas has one distance generator.
  - By x/image's measured 3.9 µs a Han glyph at 32 px and an estimated tens of microseconds for the transform, 200 new hanzi cost a few milliseconds, with no worker thread. The benchmarks measure it.
  - `Fonts.Preload(set, text)` makes the glyphs of known strings ahead of time. Nothing is baked at build time.
  - Glyphs are cached and evicted as images are, and leave the cache with their set.
- **Small text stays on distance fields.** v1 hinted glyphs at their size on screen, and distance fields lose hinting, so stems can read soft below about 14 px on a 1× display. Whether they do needs eyes: the first build's pixel tests include a 12 px label for the owner to judge. Building hinted bitmaps beside fields now was weighed: two glyph paths and caches per frame, and a label that changes look as it zooms across the threshold.

### Text: shaping, font sets and languages

([canvas: fonts and text, from glyph representation to layout](https://github.com/dvoyni/cog/issues/868), spelt in [canvas prototype](https://github.com/dvoyni/cog/issues/871), with `Metrics` from [canvas: what v1 offered that v2's design lacks](https://github.com/dvoyni/cog/issues/878))

- **Shaping is `go-text/typesetting`'s,** pinned in canvas's `go.mod`: HarfBuzz shaping, UAX #9 bidi, and UAX #14 and #29 boundaries. It is the only full pure-Go shaper, used by Gio, Fyne and Ebitengine.
  - No go-text type is in canvas's API, so a breaking minor version touches canvas's internals only.
  - canvas skips `fontscan`'s system scan, since fonts come from the FileSystem alone, and implements `shaping.Fontmap` over its font sets.
  - v1 laid text out rune by rune with x/image, kerning only.
- **Text names a font set and a size, never a font.** A `FontSetDescriptor{Label, Default, Languages}` has a default `Font` and `LanguageFonts{Languages, Fonts}` entries, so one font may serve several languages.
  - A `Font{Path, Bitmap, FieldEm, FieldRange, Index, Variations}` carries its options: bitmap or field, the field's em and range, a collection's `Index`, and a variable font's `Variations`.
  - Fallbacks belong to a set, so a title set falls back to a bold CJK font and a body set to a regular one.
- **Language tags match exactly.** A game shipping both Chinese scripts lists `zh-Hans` and `zh-Hant`.
- **A character is drawn by the language's fonts that have it, then by the default font, then as the default font's missing-glyph box,** reported once per set. Languages are not mixed. This is how games localise: switching the language switches the typeface.
- **The language also goes to the shaper,** so a Pan-CJK font picks its regional forms by OpenType's `locl`.
- **The current language is the `Fonts` resource's,** starting from `Config.Language` and changed by `SetLanguage` on its writing side. A run's optional `Language` overrides it, for a name in another language.
- **The font that draws a character is cached per set, language and character,** so a change of language clears nothing. v1 cached it without the language, and a change needed `ClearGlyphsCmd`.
- **A set is made on purpose,** never loaded by drawing. Making it loads every font file it names, in the task that makes it, at a start or a loading screen. It lives until it is destroyed, and its glyphs then leave the cache.
  - A set is a plain, pointer-free `FontSet` handle, which an ECS component holds.
  - The zero `FontSet` is the default set, made at init from `Config.DefaultFontSet`, whose zero is JetBrains Mono alone, embedded as v1's default font was.
  - go-text keeps a font's file in memory, so four CJK fonts in one set hold about 60 MB; a Pan-CJK font serving them all is one file.
- **The `Fonts` resource** holds the font sets, the current language, the icons and the registered colours.
  - **Its reading side** is an interface of reads alone: `Language()`, `Layout(text)`, `Preload(set, text)` and `Metrics(set, size, language)`.
  - **Its writing side,** `WriteFonts`, is answered only from a write handle, `ElevateFonts(kernel.Write[Fonts], kernel.Read[platform.FS]) WriteFonts`, as `platform.ElevateFileAccess` answers `WriteFS`, so a task holding the read lock cannot make a set. It holds `MakeFontSet(descriptor)`, `DestroyFontSet(set)`, `SetLanguage(tag)`, `RegisterIcon(name, icon)` and `RegisterColor(name, color)`, and it is no resource. Making a set reads platform's FS, so `ElevateFonts` takes FS's read handle as well.
- **Failures:**
  - A font file that fails when its set is made is reported once, and the set is made without it. A failed default font is replaced by the embedded JetBrains Mono.
  - A destroyed or never-made set is drawn with the default set and reported once, since an ECS component may outlive its set.
  - v1 drew nothing for a failed font and measured it zero.
- **Font files:** TTF and OTF; collections (`.ttc`, `.otc`) by a font's `Index`, zero meaning the first face; variable fonts through its `Variations`, a small fixed array of `Variation{Tag, Value}` so the value stays comparable, zero meaning the default instance. `canvas.Tag("wght")` answers an axis's four bytes; a name of other than four bytes is reported once and answers the zero tag.
- **Colour glyphs** (emoji) are not drawn as colour: a colour glyph draws its plain outline if its font has one, and otherwise falls to the default font.
- **Font metrics:** `Fonts.Metrics(set, size, language)` answers `Metrics{Ascent, Descent, LineGap, LineHeight, XHeight, CapHeight}` in world units, as v1's `FontMetrics` did. It reads the font serving that language in the set, an empty language meaning the current one.

### Text: runs and layouts

([canvas: fonts and text, from glyph representation to layout](https://github.com/dvoyni/cog/issues/868), spelt in [canvas prototype](https://github.com/dvoyni/cog/issues/871))

- **A run of text is one entry** with the sprite's sort key, its glyphs drawn in order: `Text{String, Markup, Spans, Font, Size, Language, Transform, Pivot, Tint, Material, Clip, Filter, WrapWidth, Align, LineHeight, Tracking, Layer, Order}`, recorded with `DrawText` and `DrawTextWith`.
  - **`Size`** is its em in world units, zero meaning `Config.TextSize`.
  - **`Transform`** is an `m.Mat2x3`, so text rotates and scales. v1's had only a position, its y at the top of the first line.
  - **`Pivot`** is a `Pivot` of the layout's box, zero meaning the centre, its `Texels` in em. The box is the wrap width, or the widest line without wrapping, by the lines' height. A label over a unit takes `PivotBottom`, a UI `PivotTopLeft`.
  - **`Tint`** is the fill's colour, zero meaning white. A zero `Material` means `TextMaterial`.
  - **`Filter`:** a run's zero filter is its font's, linear for a distance field and nearest for a bitmap font, never `Config.DefaultFilter`, since nearest filtering breaks a field. The prototype's pixel-perfect run first sampled every label's field with nearest filtering.
- **An outline is one entry.** The text material's argument holds one profile (outline, halo, glow or drop shadow), drawn under the fill in the same fragment, as TextMesh Pro draws its face, outline and underlay, and as Godot's `Label` draws a line's outlines within its own slot. All labels batch, and each outline sits under its own fill. A zero profile draws none, and a second effect is a second entry.
- **`Layout` is public:** text shaped, wrapped and measured, answered by `Fonts.Layout(text)` from the reading side with no frame. It answers `Size()`, `LineCount()`, `Baseline()` (its first line's, from the box's top), `LineWidth(line)` and `ClusterPosition(cluster)`, for a UI's carets, selections and baseline alignment. Measuring and drawing read the same layout, so they never disagree, as v1's drifted.
- **`DrawText` lays text out through a cache** keyed by its inputs, so a label drawn every frame is shaped once. A layout unused for `Config.LayoutIdleFrames` frames is freed. Registering an icon again lays out again the cached layouts holding it. v1 cached no layout, and its UI measured each label three times a tick.
- **An ECS component holds the text** and its system calls `DrawText`, since a `Layout` is not pointer-free.
- **Breaks** fall on UAX #14 boundaries, so CJK breaks between ideographs and closing punctuation never starts a line. A word too long for its line breaks between grapheme clusters, and `\n` is a hard break. `WrapWidth` zero means no wrapping.
- **`Align`** is `AlignStart`, `AlignCenter` or `AlignEnd`, so a right-to-left paragraph aligns from its right.
- **Line height** is the font's ascent, descent and line gap times `LineHeight`, zero meaning 1. **`Tracking`** is letter spacing in em, zero meaning none.

### Text: spans, icons and markup

([canvas: fonts and text, from glyph representation to layout](https://github.com/dvoyni/cog/issues/868))

- **A `Span{Text, Tint, Icon}` carries a tint of its own,** zero meaning the run's, **or an icon.** A run's `Spans`, when given, are the run instead of its `String`. They are shaped and wrapped together, so a coloured word wraps with its paragraph.
- **An icon is registered under a name** on the writing side: `RegisterIcon(name, IconDescriptor{Image, Frame, Scale, Offset, Tinted})`.
  - Registering loads and packs its image then, so a layout knows its width before any draw.
  - `Scale` multiplies the cap height, zero meaning 1; `Offset` shifts its baseline in em; `Tinted` takes the span's tint, where it otherwise takes only the run's alpha, as v1's did; `Frame` picks a sheet's cell, so a game's button prompts are one image.
  - An icon is the cap height times `Scale` tall, its width by its aspect ratio, its bottom on the baseline moved by `Offset`, and a line never breaks inside it.
  - Registering a name again replaces its image everywhere, so prompts switch with the controller with no string edited.
  - An unregistered name draws the default font's missing-glyph box at an icon's size, reported once per name.
  - The text material binds the sprite atlas beside the glyph atlas, each quad's instance data naming which it samples, so a run with icons is still one batch. Distance effects skip icons; an icon with an outline is a sprite entry of its own.
- **Markup is read only for a run whose `Markup` is set,** so text a player typed draws as written. ECS components and translation tables hold strings, not spans.
  - `\i:name` is an icon. A name is letters, digits, `_`, `-` and `/`, so "Press \i:a." keeps its full stop.
  - `\c:colour{text}` is a tinted span. Its colour is `#rgb`, `#rrggbb`, `#rrggbbaa`, or a name given to `RegisterColor`, so a translator writes `\c:keyword{fire}` and a theme changes it in one place. Spans nest, and icons sit in them.
  - `\\` is a backslash. Braces are counted within a span, so balanced ones pass, and `\}` is a lone closing brace within one.
  - Markup that does not parse draws as written and is reported once per string.
  - Backslashes rather than BBCode's brackets or TextMesh Pro's angle brackets, which ordinary game text uses.

### Meshes and mesh files

([canvas: custom 2D meshes, and how they share the material contract with sprites](https://github.com/dvoyni/cog/issues/870), spelt in [canvas prototype](https://github.com/dvoyni/cog/issues/871))

- **canvas publishes a standard layout, and any other layout is the user's.**
  - **`canvas.Vertex{Position, UV m.Vec2; Color m.Color}`,** 32 bytes a vertex as v1's `Vertex` was, is drawn by canvas's own mesh vertex stage, so a fragment-only material such as the SDF outline or a dissolve draws on a mesh as on a sprite. Its `Position3()` answers the position with a zero z, for `meshfile.CalculateBounds(vertices, canvas.Vertex.Position3)`.
  - `Color` follows the tint's rules: straight alpha, premultiplied by canvas, zero meaning white, so a vertex with no colour draws its image as it is. A vertex faded out takes `{1, 1, 1, 0}`.
  - There is no spare channel, as Godot's `CUSTOM0` and `CUSTOM1`. Data per entry is the material argument's; data per vertex is a layout of the user's own. A spare `vec4` would cost every vertex 16 bytes.
- **Any other layout is a vertex type with a `VertexLayout() meshfile.Layout` method,** as v1's types had one.
  - The `meshfile.VertexType` constraint makes a type without it a compile error, and its stride is checked against the type's size. The one method serves mesh files and per-frame meshes alike, and canvas translates it into render's layout.
  - Its material brings a vertex stage that includes canvas's include and calls its functions: world to clip, snapping, and the region remap.
  - A fragment-only material on a mesh of another layout has no vertex stage to read it: the draw is refused, reported once, and skipped.
- **A mesh file is named by a path,** interned by `MeshFileFromFile(path)` into a plain, pointer-free `MeshFile` handle, as `ImageFromFile` interns an image, so an ECS component holds it. Nothing is made or released.
  - A game puts mesh files into a memory mount of its own, `filesystems.NewMemoryFS`, bound as a read mount, or ships them in any read mount, so meshes a tool exported into an `embed.FS` load alike.
  - **It loads at its first draw,** in the recording job, through platform's FS and libs/meshfile, made into render buffers and a mesh, and draws that frame, as an image does.
  - It stays until it is unloaded by path or prefix with `Assets.Unload`, as images are. A mesh fills no atlas, so there is no ceiling and no eviction.
  - A game changes one by putting the file again and unloading its path, and the next draw loads the new one, as a deforming terrain chunk does.
  - A missing or malformed file, or one of a version canvas does not know, draws nothing, reported once per path.
  - canvas needs no gap in render for meshes. Whether render later loads mesh files by path, with the sharing and reload its loader gives textures, is the 3D plugin's question.
- **A `Mesh`** draws a mesh file's triangles: `Mesh{File, Image, Frame, Transform, Tint, Material, Clip, Filter, Layer, Order}`, recorded with `DrawMesh`, `DrawMeshWith`, `DrawMeshes` and `DrawMeshesWith`. It carries what a sprite carries, a transform, a tint, a material and its argument, a clip and the sort key, and also:
  - **`Image` and `Frame`.** A mesh samples an image as a sprite does. Its UVs run 0 to 1 over the image's region, or its frame's for a sheet, and the vertex stage remaps them into the atlas.
    - A zero `Image` is the white texel, so an untextured polygon draws its vertex colours times its tint.
    - UVs outside 0 to 1 follow the image's edges through canvas's sampling helper, so an image packed to tile repeats: a terrain polygon whose UVs are its positions over the image's size fills with grass.
    - An image made from a render texture works too: v1's `DrawTexture` uses, a masked minimap and a curved display, are a mesh naming it.
    - A mesh's texture as a material parameter was weighed: every textured mesh would need a material of its own, and meshes could use neither the atlas nor its batching.
  - **`Filter`,** as a sprite's.
  - **No `Size` or `Pivot`:** a mesh's coordinates are world units before its transform, and its origin is its pivot. **No `DrawMode` or `Crop`:** its geometry is its shape.
- **Per-frame meshes** are for geometry that changes every frame: a trail, a rope, a waving flag, a deformed sprite, a 2D skeleton's skin.
  - `DrawTriangles(options DrawTrianglesOptions, triangles Triangles[V])` and `DrawTrianglesWith(options, triangles, argument)`. `DrawTrianglesOptions` holds a `Mesh`'s fields less the file. `Triangles[V]{Vertices, Indices, Topology, Bounds}` holds vertices of any layout, optional `[]uint32` indices, stored as 16-bit while the vertices fit, a topology and bounds.
  - They are copied into the frame's temporary buffers when recorded, so the caller may reuse its slices, and drawn through render's `AcquireTemporaryMesh`.
  - v1's `DrawTriangles` was this alone, non-indexed. The prototype's first version passed a mesh value whose file it ignored, so the options hold no file.
- **Bounds are required everywhere.** `meshfile.Encode` refuses zero bounds, and a per-frame draw with zero bounds is refused and reported once, so forgetting them shows at once rather than as a mesh that vanishes when its origin leaves the view. A mesh that must never be culled gives bounds larger than any view.
- **Snapping under `PixelPerfect`:** the entry's translation snaps to the world-unit grid, and its vertices are not snapped one by one, so a mesh authored on the art grid stays on it and a rotated or scaled mesh moves whole, undistorted. A sprite snaps its first corner because its centre pivot lands half a unit off for an odd size; a mesh's origin is where its author put it. Snapping each vertex, Godot's `snap_2d_vertices_to_pixel`, wobbles a rotating mesh. The function is in canvas's include, for custom vertex stages to call.

### libs/meshfile

([canvas: custom 2D meshes, and how they share the material contract with sprites](https://github.com/dvoyni/cog/issues/870))

- **The mesh file format is a library, `libs/meshfile`, `github.com/dvoyni/cog/libs/meshfile`,** a module of its own that a 3D plugin can share. It imports no render, so an offline exporter writing mesh files needs neither render nor gfx.
- **A file holds** a version; the layout in the library's own types, a `Layout{Stride, Attributes}` of `Attribute{Format, Offset, Location}`, its `Format`s mirroring WebGPU's vertex formats as render's do; the `Topology`, zero meaning a triangle list; optional indices, 16 bits while the mesh has at most 65,535 vertices, so no index is 0xFFFF, which a strip reads as a restart, and 32 beyond; the bounds as an `m.Box3`, 2D leaving z at zero; then the vertex bytes and the index bytes.
- **Its calls:**
  - `Encode[V VertexType](vertices, indices, topology, bounds)` writes typed slices, taking the layout from `V`. It refuses zero bounds, a vertex type whose layout's stride is not its size, and an index past the vertices, which 16 bits could not hold.
  - `Decode(data) (File, error)` answers the header and slices of the bytes, without copying them. It refuses a file that is not a mesh file, one of a version it does not know, and one cut short or running past the end its header gives.
  - `CalculateBounds[T](vertices []T, position func(T) m.Vec3) m.Box3` works over any layout and dimension. An empty slice answers the zero box, which `Encode` and canvas refuse as missing bounds.
- **The format is documented and versioned,** so a tool can write it.
- **glTF and OBJ were weighed:** 3D formats with far more than 2D needs, without a place for a custom layout. Raw vertex and index files would keep the layout apart from the data, where the two could disagree.

### Cameras

([canvas: the 2D camera, its projection, viewport modes and several cameras](https://github.com/dvoyni/cog/issues/869), changed by [canvas on the kernel](https://github.com/dvoyni/cog/issues/873), spelt in [canvas prototype](https://github.com/dvoyni/cog/issues/871))

- **y points down,** as an image's rows run, so an image stands upright under an identity transform.
- **A world unit at zoom 1** is one point of a window, 2 pixels across on a display of scale 2, or one pixel of a texture.
- **A `Camera` is a plain value:**
  - `Position`, `Rotation` and `Zoom`, zero meaning 1.
  - `Pivot`, the point of its viewport that `Position` places and that rotation and zoom turn about, as a fraction of the viewport from its centre, zero meaning the centre. A UI camera takes `{-0.5, -0.5}`, its top left, so world coordinates are UI coordinates.
  - `Layers [4]LayerRange`, each `LayerRange{From, To}` half-open. An empty range is unused, four empty ones mean every layer, and layer 0 alone is `{0, 1}`. `canvas.MakeLayer(layer)` answers the four holding one layer, and `canvas.MakeLayers(ranges...)` those holding up to four ranges, the rest empty. One layer may be drawn by several cameras.
  - Its target, `Window` (a `platform.Window`) or `Texture` (a texture the game made with render), both zero meaning the main window.
  - `Viewport`, a rectangle in fractions of the target, zero meaning all of it, so split screen is two cameras with a viewport each.
  - `Clear` and `ClearColor`, a linear colour, as a tint is.
  - `SampleCount`, 1 or 4, zero meaning 1. It smooths every entry's hard edges at once.
    - A camera of 4 draws into a temporary multisampled texture of its viewport's size, cleared to its `ClearColor` or to transparent, resolves it into a temporary texture of that size, and draws that over its viewport, replacing what the viewport holds when the camera clears and blended over it otherwise.
    - Both textures take the target's format, a window's as `BGRA8Unorm`, and the camera's entries encode their output as the target would. Its composite, through the include's `canvas_viewport_vertex` with the clear material or a composite material blending normally, then draws what was resolved as it is, encoding nothing again. The multisampled texture is discarded once resolved.
    - Any other count panics in `SetCamera`.
    - Resolving into the target was weighed. A resolve writes its whole target, which would wipe the other half of a split screen, or a 3D scene under a UI camera that does not clear.
    - A material blending other than normally, in a 4-sample camera that does not clear, blends against transparent rather than against the target.
  - `Order`, a `render.Order`, the order cameras draw in.
  - `Size`, `Fit` and `PixelPerfect` (see *Logical size, fit and pixel-perfect*).
- **`DrawList.SetCamera(Camera)` sets a camera for that frame,** forgotten once it is drawn, as entries are. Nothing is made, stored or destroyed, and an ECS system sets the camera it computes.
- **The main window** is the first window platform opened of those still open, which canvas follows through `platform.WindowOpenedEvent` and `platform.WindowClosedEvent`: gogpu's window opened at start, when it opens one. When it closes, the next opened takes its place.
- **A frame that sets none is seen through the default camera:** the main window, every layer, the centre pivot and zoom 1, clearing to opaque black, so a frame with no camera never shows a stale framebuffer. It draws only a frame that recorded entries (see *The frame on the kernel*).
- **Cameras draw in `Order`,** equal ones by their lists' order and then by call order. At equal `Order` texture targets draw before windows, so a minimap's texture is drawn before the window that shows it. Cameras of one order share one command list.
- **`Clear` clears the camera's viewport,** never the rest of its target: through the pass's load when the viewport covers the target, otherwise by drawing `ClearColor` over the viewport with canvas's clear material, the default shader replacing what the target holds, through the include's `canvas_viewport_vertex`, which covers clip space whatever the camera's projection and never snaps, as the multisampled camera's composite does. render's load clears a whole attachment, which would wipe the other half of a split screen.
  - A load clears to `ClearColor` premultiplied and encoded as sRGB where the target's format encodes nothing, as canvas's output is, so the two clears show alike.
  - A viewport's edges are rounded to the target's pixels, so viewports sharing an edge neither overlap nor leave a gap.
- **A camera naming both a window and a texture, or a texture render does not hold,** is skipped and reported once. A camera into a window not open or not sized, or of an empty viewport, is skipped silently, as render skips a pass into a minimized window.
- **A camera with no entries in its layers and no clear** draws nothing, and an order with no camera drawing begins no list.
- **Following, smoothing, limits and shake are the user's,** since a camera is a value set each frame. Parallax is several cameras into one target, each drawing a layer range from its own position.
- **An entry is drawn by every camera whose layers hold it.** A tilemap seen by two cameras records the union of their views once, which canvas's guide says: recording per view would draw the shared tiles twice.

### Logical size, fit and pixel-perfect

([canvas: the 2D camera, its projection, viewport modes and several cameras](https://github.com/dvoyni/cog/issues/869))

- **`Size m.Vec2` is the logical size:** the area of the world the camera is meant to show.
  - Zero on both axes means the viewport's own size in points, one unit to a point, so each half of a split screen shows half the window's world.
  - A zero on one axis follows the viewport's aspect ratio. `Size{0, 1080}` is 1080 units tall at any width, which replaces v1's fixed-width and fixed-height modes.
- **`Fit` fits it into a viewport of another shape:**
  - `FitExpand`, zero: the smaller scale, the longer axis showing more of the world, Godot's default for projects created from 4.7.
  - `FitInscribe`: the smaller scale, the rest bars, which v1 could do neither of. The entries are scissored to the logical size between the bars, each bar rounded to pixels alike. The bars are the camera's viewport, so a camera that clears fills them with `ClearColor` as it clears the rest, and one that does not leaves them as the target holds them, so a UI inscribed over a 3D scene shows the scene in its bars.
  - `FitOverlap`: the larger scale, the edges cut off.
  - `FitStretch`: each axis its own scale.
- **`Zoom` applies on top of the fit,** and leaves `FitInscribe`'s bars where they are.
- **The projection is built from the camera and its measured target alone,** the target's size in pixels and its pixels to a point, by the drawing and the conversions alike, so a click converts through the projection drawn. The world turns against the camera's `Rotation` about `Position`, which is scaled along the view's axes and lands on the pivot.
  - `Rotation` is in radians from +X towards +Y, as `m.Rotation2x3` turns an entry, so the world shows turned the other way.
  - The pass parameters' `pixel_size` is a pixel in world units along the view's axis of the smaller scale: under `FitStretch` a hairline is one pixel along that axis and wider along the other, never thinner than a pixel.
- **`PixelPerfect` is a setting of any camera,** zero meaning off, for pixel art.
  - **The scale, fit and zoom together, is floored to a whole number of framebuffer pixels per world unit,** at least 1, so DPI counts: a 320×180 logical size draws 6 px a unit in a 1080p window, and 12 on a display of scale 2.
  - `FitExpand` and `FitInscribe` floor it, so the rest shows more of the world or becomes bars. `FitOverlap` rounds it up, so it still covers. `FitStretch`, whose pixels would not be square, draws as `FitInscribe`, reported once as `ReportKindPixelPerfectStretch`.
  - `FitInscribe`'s bars are those of the fit's scale floored, without the zoom, so zoom still leaves them where they are.
  - **Entries snap to the world-unit grid in canvas's vertex stage,** each quad moved whole so that its first corner lies on the grid, rounded half up. A 15-texel sprite with a centre pivot lands on the grid, where snapping its position would leave it half a unit off, and a rotated entry is moved, not distorted. A mesh snaps by its translation (see *Meshes and mesh files*).
    - Halves round alike, so a sprite of an odd size centred on a whole unit moves a unit as its centre does: WGSL's `round`, half to even, would hold it still every other unit.
  - **The view's placement snaps to the target's pixels,** the world's origin landing on a whole pixel, rounded half up, so the world stays on its art grid while the view scrolls a screen pixel at a time. The conversions go through the snapped view, as they go through the projection drawn.
  - **One art pixel is one world unit,** so a game gives its logical size in art pixels: 320×180, not 20×11.25 tiles. A bitmap font draws with square pixels here.
  - **A rotated or scaled entry still draws at the target's resolution.** Everything at the art's resolution is a recipe, not a setting: one camera draws into a render texture of the logical size, and a pixel-perfect camera draws it into the window as an image with nearest filtering.

### Blending in the target's space

([canvas: the 2D camera, its projection, viewport modes and several cameras](https://github.com/dvoyni/cog/issues/869))

- **canvas draws straight into the window,** with no intermediate texture and no final pass. Its output function encodes sRGB wherever the target's format does not.
  - A window or a plain Unorm texture blends encoded colour, as Aseprite and Photoshop do, and as Godot without HDR 2D and Unity's gamma colour space do.
  - An sRGB texture target blends in linear space.
  - A window's framebuffer is taken to be `BGRA8Unorm`, gogpu's only format, and always encoded: neither render nor gfx answers a window's format to canvas yet, so a surface of an sRGB format would be encoded twice.
- **v1 blended in linear space** in an `RGBA8UnormSrgb` intermediate and encoded it in an implicit final pass, which cost a pass and 8 MB a window at 1080p.
- **An image made from a camera's texture takes its colour space as any image does.** A Unorm texture holding encoded colour is marked sRGB and decoded as it is sampled, so a minimap drawn as an image looks as the world drawn directly does.
  - `canvas_sample` decodes it after filtering, where the instance data's `decode` says, the inverse of `canvas_output`'s encoding of premultiplied colour. An sRGB texture is decoded by its format, however it is marked, and an atlas's format always decodes its own.
  - A view of an sRGB format over the Unorm texture was weighed: it needs the texture made with that view format, which render's descriptor does not carry, and a game making its own texture would have to know to ask.
- **A camera's texture target's size and format come from render's `Inventory.TextureDescriptor`,** for placing its viewport, for `Targets`, and for `encode`. A camera restating its texture's size and format, or canvas making texture targets of its own, would each keep a second account of what render already holds.

### Clips and culling

([canvas: the 2D camera, its projection, viewport modes and several cameras](https://github.com/dvoyni/cog/issues/869), which revises [canvas: sprites, their transforms, and how they are ordered and batched](https://github.com/dvoyni/cog/issues/867); mesh bounds from [canvas: custom 2D meshes](https://github.com/dvoyni/cog/issues/870))

- **A clip is a rectangle of the world,** an entry's `Clip`, zero meaning none, seen through each camera as its entry is, so it holds for split screen, minimaps, zoom and DPI alike. Under a UI camera at one unit to a point, world units are UI coordinates.
  - It becomes render's scissor rectangle, so a change of clip breaks a batch. Under a rotated camera the scissor is the clip's bounding box.
  - The scissor is the clip's bounding box in the target's pixels within the camera's drawn rectangle, `FitInscribe`'s bars left out, each edge rounded half up to a pixel's, so a pixel is drawn where its centre lies in the clip. A batch whose clip holds no pixel of the view is not drawn.
  - v1 clipped with a discard in the fragment shader, so a custom fragment stage that skipped the test drew outside its clip, with no error.
- **Culling is per camera, on the CPU:** each entry's bounds against the camera's visible world rectangle, its bounding box when the camera rotates.
  - A sprite's bounds are its transform, size and pivot, grown to a converter's output; a run of text's are its layout box; a shape's are its rectangle.
  - A mesh's bounds, a mesh file's or its triangles', are transformed by its transform into a bounding box.
  - Each grows by its material's `CullMargin`, and by half a pixel-perfect camera's grid, the most a snapped entry moves.
  - An entry no camera sees costs only its recording, and an entry whose bounds miss its clip's part of the view is culled too, as is one whose clip lies outside it.
  - A camera whose entries are all culled and that does not clear draws nothing, as one with no entries in its layers.
  - v1 culled nothing.

### Conversions and Targets

([canvas: the 2D camera, its projection, viewport modes and several cameras](https://github.com/dvoyni/cog/issues/869), [canvas prototype](https://github.com/dvoyni/cog/issues/871), [canvas on the kernel](https://github.com/dvoyni/cog/issues/873))

- **Conversions are `Camera`'s methods:**
  - `ScreenToWorld(point, targets)` and `WorldToScreen(point, targets)`, in target points, as platform's pointer events give them;
  - `VisibleRect(targets)`, the world rectangle the camera sees, its bounding box when it rotates, which canvas culls against and a game spawns just outside of.
- **Each takes `canvas.Targets`,** a resource of reads alone holding each target's size and scale as it was last drawn: `Window(window) (size, scale, drawn)` in points, and `Texture(texture) (size, drawn)` in pixels. canvas's Targets task writes it once a frame, after the drawing, so a click converts through what the player saw.
- **Each answers whether the point lies in the viewport of a target drawn,** but `FitInscribe`'s bars. A target not yet drawn has no entry, and a conversion through it answers false; a point outside the viewport converts all the same, for a drag that leaves it.
  - The zero window answers the main window of the frame last drawn, the target of a camera naming none.
  - A window's account keeps its framebuffer's pixels, and answers its size as those over its scale, so a conversion rebuilds the very pixels drawn.
- **A game keeps its own cameras to convert a click,** since canvas forgets them, which canvas's guide says. A conversion reads this frame's camera against its target as it was last drawn. A click is routed by trying a frame's cameras from the highest `Order` down.

### Batching and drawing

([canvas: sprites, their transforms, and how they are ordered and batched](https://github.com/dvoyni/cog/issues/867), [canvas: custom 2D meshes](https://github.com/dvoyni/cog/issues/870))

- **A batch is a run of sorted entries with the same material, atlas or render texture, filter and clip.** Sprites, shapes and glyphs are drawn as `DrawVertices(TriangleList, 6, DrawArgs{InstanceCount, FirstInstance})`, with no mesh, each instance reading its instance data at `instance_index`.
- **Nothing else breaks a batch:** not the image, the frame, the page, the draw mode, the crop, the tint or the argument. The white texel's entries, shapes and sprites of the zero `Image`, take no filter, which changes no pixel of it, so a zero-image sprite batches with shapes whatever its `Filter`, and they join a batch of any filter, which takes the filter of the first of its entries that names one. A texture change breaks every engine's batch; a 2D-array atlas with its page carried per instance avoids it.
- **Instance data** goes into temporary storage buffers, one per stride, so entries of every material share them. There is no cap beyond the binding's 128 MiB.
- **Every draw of canvas's is instanced,** a lone sprite or mesh among them: each instance reads its instance data at `instance_index` from the draw's `FirstInstance`. canvas keeps no other path.
- **Consecutive entries in sort order of one mesh file,** with one material, atlas, filter and clip, are one instanced draw, `Draw(mesh, DrawArgs{InstanceCount, FirstInstance})`. Different mesh files are different draws, and meshes never batch with sprites. A forest of one tree mesh, recorded in one bulk call, is one draw.
- **A per-frame mesh is a draw of its own,** never merged. v1 concatenated consecutive triangle lists because its vertices were in world space with nothing per entry; here each entry has its own transform, tint and argument, so merging would need an entry index on every vertex, a second stream every custom vertex stage would read. A user with many small pieces builds them into one mesh: a trail system writes every trail into one per-frame mesh.
- **Each camera is a pass** in its order's command list, into its target, with its viewport and its pass parameters set once.
- v1's sorting, culling and layering together cost under 0.4% of a frame.

### Device loss

([canvas: auto-atlasing](https://github.com/dvoyni/cog/issues/866), [canvas on the kernel](https://github.com/dvoyni/cog/issues/873))

- **`render.RecoveredEvent` is a fresh start.** The atlases, the glyphs and the mesh files' buffers are dropped, and loaded again at their next draw.
- **canvas's table of images is on the CPU and survives.** Every page is cleared, and each image packs again at its next draw, converted ones derived from their files again.
- **Glyphs rebuild as cache misses.**
- **Bytes removed from a memory mount** since their load cannot come back, which render reports.

### Reports

([canvas: how canvas and the plugins on it are tested on rendertest, with pixel tests](https://github.com/dvoyni/cog/issues/874))

- **A report** is a mistake canvas finds at run time, carried past.
- **It is logged through the kernel's `LogErrOnce`,** keyed by the report's kind and its subject, such as a mesh file's path, or a material and the argument's type. canvas keeps no deduplication of its own.
- **`ForgetLogOnce` re-arms a key once its subject is repaired,** so a later failure reports again:
  - unloading a path forgets its files' keys;
  - registering a missing icon forgets that icon's key;
  - making a font set again forgets a failed font's key.
- **The logged error is typed,** `canvas.Report{Kind canvas.ReportKind, Err error}`, as render's are, so a test can tell which report it is. `render.ReportKind` is render's enum, so canvas cannot log `render.Report`s.
- **It goes into the inspection** of the frame that raised it.

### Inspection

([canvas: tooling, a frame inspection in canvas's terms and hot reload of images, fonts and mesh files](https://github.com/dvoyni/cog/issues/876))

- **`Assets.RequestInspection()` asks for the next frame canvas draws,** the first whose `FrameEvent` is published after the call, as render's request does, and asks render to inspect that same frame. A request while one is pending joins it.
  - While a request is pending, canvas writes down each frame it draws and asks render again, and delivers the one render's `InspectedEvent` names. Choosing one frame at the request was weighed: render joins a request to an inspection already pending, of a frame canvas may have drawn before it was asked, and a request landing while a frame begins can reach render for the next frame and canvas for this one.
  - `Assets` holds a `render.Frame` for it, so a request asks render at once and the next frame is inspected.
- **`canvas.InspectedEvent{Beat}`** is published after render's `InspectedEvent` for that frame, in a batch of its own, as render publishes its own. `Assets.ReadInspection(event)` answers a `canvas.Inspection` while the publication's jobs run, with render's `Inspection` beside it, `Inspection.Render`, so a tool reads both accounts of one frame.
- **Every other frame pays one check and allocates nothing,** so a tool may stay attached.
- **A `canvas.Inspection` holds:**
  - each camera, in drawing order: its fields and target, its viewport in pixels, its scale, the world rectangle it sees, and its layer ranges;
  - each camera's batches, in order: the material, atlas, filter and clip, the instance count, and its entries. Each entry gives its kind, what it draws (an image's path and frame, a run's text, or a mesh file), its sort key, and its draw list's place;
  - what each camera culled, as entries;
  - the frame's first uses: images packed, mesh files loaded, glyphs made, and layouts made on a cache miss;
  - its reports, and the time canvas's sort job and per-order jobs took.
- **`String()`** writes it as the prototype printed its frames, for an agent to log: cameras in order, then their layers, batches, instance counts, culls, first-use loads and reports.
- **canvastest reads frames through it** (see *Testing Decisions*): its `Step` asks for an inspection before each beat and returns once it is delivered, and `Frames` answers them. After `RecordNothing` it asks for none, so the allocation gate counts only canvas's and render's own.
- **Not canvas's:** a tool through which an agent reads it, a later MCP plugin's, cog-wide, serving render's inspection too.

### Hot reload

([canvas: tooling, a frame inspection in canvas's terms and hot reload of images, fonts and mesh files](https://github.com/dvoyni/cog/issues/876))

- **`Assets.ReloadChanged()`** works as render's `Inventory.ReloadChanged()` does: it compares each file's modification time and size, and reads no file that did not change. A 2D game binds one key to it, or a dev plugin calls it every second.
- **What changes, by kind:**
  - **Images:** render gives an image's source back once it is packed, so canvas stats the files itself. A changed one is read, converted and repacked in place, keeping its handle.
  - **Fonts:** a changed font file is loaded again, and its glyphs and the layouts that used it are dropped.
  - **Mesh files:** reloaded the same way. The memory mount stamps every put strictly later, so a re-put is seen.
- **It also calls render's `ReloadChanged`** through the `Inventory` it holds, so the 2D materials' shaders and their includes reload in the same call.
- **It allocates only while it runs.** Nothing reloads by itself each frame, since that would cost a stat per file every frame. At run time, a game still changes a mesh file by putting it again and unloading its path.

### Performance

([canvas: the per-frame bar, its allocation gate and benchmarks](https://github.com/dvoyni/cog/issues/875))

- **A steady frame allocates nothing once warm.** This is a rule in canvas's `AGENTS.md`, as render's is in render's, enforced by an allocation gate (see *Testing Decisions*).
- **A steady frame holds only content already seen:**
  - sprites, shapes and mesh files, recorded singly and in bulk;
  - text drawn from the layout cache;
  - per-frame triangles copied into the frame's buffers;
  - several cameras and camera orders, and every recording task's draw list.
- **Outside the gate, by nature,** each allocating its own, timed by a benchmark and not counted:
  - first uses: an image loaded and packed, a mesh file loaded, a glyph made, a font set made;
  - a layout for a string the cache has not seen, since go-text's shaping allocates. A timer whose digits change every second allocates in the frames it changes;
  - a frame with refusals, whose once-key may allocate. A steady frame has none.
- **Time per entry** is benchmarked against the same frame drawn by hand on render, which gives canvas's overhead, as render's benchmarks give render's over gfx. Ten runs go into `plugins/canvas/docs/benchmarks-baseline.txt`, compared with `benchstat`.
- **A frame's new glyphs** are made in the frame, with no worker thread. If they measure too slow, the remedy is `Fonts.Preload`; spreading their generation across frames is an improvement, not built now.

### Delta to render

([canvas: the changes canvas's design asks of render](https://github.com/dvoyni/cog/issues/877), from the gaps that [canvas: what a 2D material is](https://github.com/dvoyni/cog/issues/865), [canvas: auto-atlasing](https://github.com/dvoyni/cog/issues/866), [canvas: the 2D camera](https://github.com/dvoyni/cog/issues/869), [canvas: how canvas and the plugins on it are tested](https://github.com/dvoyni/cog/issues/874) and [canvas: the per-frame bar](https://github.com/dvoyni/cog/issues/875) found)

Each change was checked for a 3D renderer's needs too, drawbridge's forward+ or deferred path and scene's skinning, and widened where 3D asks more than canvas.

- **The layout check:** `Inventory.CheckLayout[T](shader, binding, path)` checks Go type `T` against the type at `path` within the binding, by the rules render checks uniform blocks by: the size, then each field's offset in order, names ignored, `_` fields as padding.
  - The path is relative to the binding and walks dotted members, `[]` stepping into an array's element: `[]` for an element of a binding `objects`, `lights[]` within a binding `scene`, `[].argument` within canvas's `canvas_instances`. An empty path names the binding's whole type.
  - canvas keeps material arguments, its instance data and its storage, and calls the check once per material and argument type. A 3D plugin checks its per-object and per-light data with the same call, and canvas reaches no gfx type, as render's rules ask.
  - render owning material arguments whole was weighed. canvas's instance data model would have moved into render before a second user showed what it needs.
- **Mips for a made texture:** `CommandList.GenerateMips(texture, firstLayer, layerCount)` records render's mip shader into the list where it is called, for every level of those layers. render generated mips only for textures it loads, in its own command list.
  - It covers 2D, 2D-array and cube textures, a cube's faces as layers, so probes and baked maps take it too. It filters sRGB in linear, as render's loaded textures' mips are.
  - canvas calls it after the passes that pack into an atlas page that frame, at the start of its lowest camera order's command list. The atlas's gutters and atlas grid at 2^(levels−1) keep the box filter within each image.
  - Its doc says what it is not: a volume texture's mips, and a depth pyramid for occlusion culling, which takes a min or max reduction rather than an average. Those are their plugin's own compute passes.
- **A texture's descriptor:** `Inventory.TextureDescriptor(texture) (TextureDescriptor, bool)`, built on gfx's `Device.TextureDescriptor`. A made texture answers at once, and a loaded one as soon as its acquire returns, its file's header fixing its descriptor; a texture render does not hold answers false. render answered no made texture's descriptor. canvas's drawing declares `Inventory` beside `Frame`, both reads, for a camera's viewport and sRGB encoding in a texture target, for `Targets`, and for `Assets.ImageSize` of a render-texture image.
- **rendertest's harness:**
  - `h.ExpectError(match func(error) bool)` lets a test name logged errors that are not render's, so `canvastest.ExpectReport` expects a `canvas.Report`. Without it, the harness fails a test on any logged error that is not a `render.Report` of a kind the test named, so a canvas test deliberately triggering a canvas report would fail. Logging canvas's reports as warnings was weighed: the harness drops warnings, so no test could see them.
  - `h.RecordNothing()`, called before `Spawn`, puts gfx's recording adapter in its record-nothing mode, through its existing `DisableRecording`, for another plugin's allocation gate. `Frames` then answers nothing. Only rendertest's own tests could turn the mode on, and the recording otherwise keeps every call, which allocates.
  - The binding-declaration format gains the recursive member types (see *Delta to gfx and gogpu*), and `cmd/declarebindings` writes them, so the recording adapter answers what the software adapter does.
- **Meshes need no change:** mesh files are canvas's, through `MakeBuffer`, `MakeMesh` and `AcquireTemporaryMesh`.
- **Delivery:** each is a change to cog's own gfx, gogpu or render, so no fork's `CHANGELOG.md` is touched. At handover each becomes an implementation ticket under the map, blocking the canvas tickets that need it, with render's design and guide updated with its change.

### Delta to gfx and gogpu

([canvas: the changes canvas's design asks of render](https://github.com/dvoyni/cog/issues/877))

- **Every reflected member describes its type recursively.**
  - Each member gives its size, its `Members` if it is a struct, and its `ElementCount` and `ElementStride` if it is an array, with the element's own description. A count of 0 means runtime-sized.
  - The binding's top-level type uses the same description, so `array<ObjectData>`, a light list in `array<Light, 16>`, a skin's `array<mat4x4<f32>, 64>` and canvas's instance data are one case.
  - Today a binding lists only its top-level struct's members, so the material argument's check, a member of an element of a runtime array, could not be answered.
- **gogpu fills it from naga's IR,** which already holds every struct and array type, so no fork changes.
- **gfx's recording adapter answers it** from the binding declarations, which carry it (see *Delta to render*).
- **What canvas needs of gfx otherwise exists:** `Device.TextureDescriptor`, under render's new call, and the recording adapter's `DisableRecording`, under rendertest's `RecordNothing`.

## Testing Decisions

([canvas: how canvas and the plugins on it are tested on rendertest, with pixel tests](https://github.com/dvoyni/cog/issues/874), [canvas: the per-frame bar, its allocation gate and benchmarks](https://github.com/dvoyni/cog/issues/875))

**Each rule is tested at the cheapest level that shows it.** A test drives canvas the way a plugin does, through the kernel and canvas's parameters, and never calls canvas's internals to stand in for a lifecycle step, as the kernel's test rules say.

- **Plain unit tests of canvas's internal packages,** with no kernel:
  - sorting and the sort key, `OrderOf`, regrouping, batch keys and culling;
  - the camera's fit and pixel-perfect scale, and the conversions;
  - layout, line breaking and markup;
  - the mesh file format and `CalculateBounds`.
- **Recording-adapter tests through rendertest,** in a real kernel, asserting what reached render in render's words:
  - the command lists per camera order, the passes per camera, and the draws per batch with their instance counts;
  - the pass parameters, the viewport and scissor, and the instance data's bytes;
  - first-use loads, refusals and their reports, and device recovery's fresh start.
- **Pixel tests on wgpu's software adapter,** for what only pixels show:
  - slicing, tiling and crop, and sRGB encoding;
  - premultiplied blending, distance-field profiles and hairlines;
  - pixel-perfect snapping, viewport clears and `FitInscribe`'s bars;
  - a 12 px label, for the owner to judge small text on distance fields.

**`plugins/canvas/canvastest`,** exported beside rendertest, serves canvas's own tests and those of the plugins built on canvas, such as the ECS plugin and a UI:
- **A harness.** It wraps rendertest's harness, and spawns canvas after render with render's config and canvas's passed through, the test's plugins after canvas. It forwards `UseSoftwareAdapter` and `RunMain`.
- **Recording from the test.** `h.Record(func(list canvas.DrawList))` registers a recording task off `canvas.FrameEvent` for the next `Step`, so a test records a frame without writing a plugin. A body that hands entries to the test stays a closure, as rendertest's harness hands its test the window its open task opened.
- **A reader in canvas's terms.** It reads each camera, its batches and entries, and what it culled, through canvas's frame inspection, beside render's `Inspection`, which shows what reached the GPU. A UI test then asserts its button's sprite and label entries without reading draws.
- **Strict reports.** `canvastest.ExpectReport(kind)` is built on rendertest's `ExpectError`, so a test fails on any report it did not name.
- **One font everywhere.** Text in tests uses the embedded default font set, so shaping is the same on every machine.

**Pixel tests compare probes:** exact texels at chosen points where no filtering reaches, such as a cell's centre or a fill's middle, as render's pixel tests compare exact texels.
- A tolerance of ±2 per channel applies only where a probe must sit in a gradient, such as a distance-field band.
- There are no golden image files. They would break whenever the software adapter's filtering or an antialiased edge moved by one, and each would need upkeep.

**Binding declarations:** canvas's shaders get theirs from render's `cmd/declarebindings` through `//go:generate`, with `-variant CANVAS_ARGUMENT` for the define set canvas compiles a material with an argument under. canvas's guide gives the line a user's 2D material needs to be tested on the recording adapter. A missing or stale declaration fails the test, as render's harness already does.

**The per-frame gate and benchmarks,** the method of render's, ecs's and mux's bars:
- **The gate:** zero allocations for a steady frame once warm, checked with `testing.AllocsPerRun(...) == 0` over a whole beat, canvastest's `Step`, after four warm frames, on gfx's recording adapter in its mode that records nothing, turned on with `h.RecordNothing()`. It covers canvas, render, gfx, the kernel and metronome. Its frame holds the steady content *Performance* lists.
- **Under `go test -race`** the gate steps its frames for the detector and checks no count.
- **Benchmarks** time each entry against the same frame drawn by hand on render.
  - **Operations:** recording, single calls and bulk; the draw lists' merge and the sort; culling, batching, the instance data written, and the command lists recorded; text drawn from the cache; per-frame triangles copied.
  - **Counts:** 1,000, 10,000 and 100,000 sprites; 1, 4 and 16 recording tasks; 1, 2 and 4 cameras; one material against a switch every 16 entries; few layers against many, for the radix sort's passes.
  - **Timed, not gated:** glyph generation at first use, per glyph (Latin at em 48, Han at em 64), and for a frame of 200 new hanzi; an image's first use, its decode, converter and pack; the radix sort's passes as the layers spread.
  - **Ten runs** go in the baseline, compared with `benchstat`.

**Concurrency under `go test -race`:** recording tasks run together, so each piece of shared state is tested by breaking the code that guards it, as render's are:
- parallel draw lists recording in one frame;
- one image's first draw from two tasks;
- one glyph made from two tasks;
- one layout cached from two tasks;
- an icon registered while text is laid out, which `Fonts`' write lock orders.

**Prior art:**
- render's tests on rendertest's harness and the recording adapter, and its pixel tests on the software adapter;
- render's, ecs's and mux's allocation gates and baselines.

## Out of Scope

- **Plugins on canvas, with maps of their own:**
  - UI layout and widgets, as v1's ui sat on v1's canvas;
  - the ECS plugin of sprite and text components and the systems that record them, which canvas's API is shaped for.
- **Built on canvas by its users:** tilemaps, flipbook playback, a camera's following, smoothing, limits and shake, parallax, parenting, and drawing at the art's resolution through a render texture.
- **Ruled out by [canvas: the feature list, where 2D ends and its users begin, and its name](https://github.com/dvoyni/cog/issues/864):**
  - 2D lighting, which 2D materials leave room for a user to build;
  - bloom and post effects; a glow is a distance-field material;
  - particles;
  - sprites and text placed in a 3D world, though a camera taking any view-projection matrix keeps that door open;
  - non-rectangular masks.
- **Ruled out by [canvas: what a 2D material is, and how a distance-field outline is one](https://github.com/dvoyni/cog/issues/865):** blend modes that read the target (overlay, soft light, dodge, burn, hue and colour). Fixed blending cannot do them; a user builds them on render to texture.
- **Kept in [Improvement plans, by plugin](https://github.com/dvoyni/cog/issues/872),** each with the reasons of the design that left it out:
  - from [canvas: fonts and text, from glyph representation to layout](https://github.com/dvoyni/cog/issues/868): MTSDF glyphs, justification, truncation with an ellipsis, colour glyphs, a font set and a size per span, underline and strikethrough, vertical writing, and OpenType features per run, such as tabular figures for a timer;
  - from [canvas: the 2D camera, its projection, viewport modes and several cameras](https://github.com/dvoyni/cog/issues/869): snapping a non-pixel-art UI to framebuffer pixels at a fractional DPI;
  - from [canvas: the per-frame bar, its allocation gate and benchmarks](https://github.com/dvoyni/cog/issues/875): spreading glyph generation across frames;
  - from [canvas: what v1 offered that v2's design lacks](https://github.com/dvoyni/cog/issues/878): hinted bitmap glyphs for small text.
- **Designed for, built later:** the atlas's compaction; sidecar formats such as Aseprite's or TexturePacker's, with per-frame pivots; a one-pixel outline on a pixel font.
- **Another plugin's question:** whether render loads mesh files by path, with its loader's sharing and reload, is the 3D plugin's.
- **Not canvas's:**
  - a tool through which an agent reads canvas's inspection: a later MCP plugin's, cog-wide, serving render's inspection too, ruled out by [canvas: what v1 offered that v2's design lacks](https://github.com/dvoyni/cog/issues/878);
  - 3D: drawbridge's and scene's.

## Further Notes

- **Research behind the design**, in `docs/research`:
  - [glyphs as bitmaps, SDF, MSDF or MTSDF, and shaping in Go](research/glyphs.md);
  - [run-time atlas packing, and how engines auto-atlas](research/atlases.md);
  - [how Unity, Godot and Bevy shape 2D materials, sprites, cameras and batching](research/prior-art.md);
  - and v1's inventory and lessons in [render's v1 findings](../../render/docs/research/v1-renderers.md), §2.4 and §4.
- **The call shapes** were judged on a prototype in `docs/prototype-frame`, whose game draws split screen, a minimap, a UI and a pixel-perfect camera, with a toy tilemap and a toy button on canvas alone. Its README holds the verdict and the spelling of every call. It is deleted once canvas's implementation is finished, by [canvas: README, guide and AGENTS.md](https://github.com/dvoyni/cog/issues/921); [canvas prototype: the calls a user makes to draw one 2D frame](https://github.com/dvoyni/cog/issues/871) keeps the verdict and the spellings after it.
- **Names avoid other contexts' terms:**
  - render owns *Draw*, *Order*, *Command list*, *Frame*, *Material*, *Mesh*, *Pass* and *Inventory*;
  - the kernel owns *Batch*;
  - ecs owns *Shadow* and *Transient*;
  - mux owns *View*, *Depth*, *Sample*, *Weight* and *Model*;
  - drawbridge will own a 3D *Camera*.

  canvas's glossary qualifies its terms where they would collide: what a frame records is an *entry*, not a draw; a *2D material* is not a render material; a *mesh* is canvas's entry, which draws a *mesh file*. Its *mark* was renamed *entry*, and its *canvas list* *draw list*; the closed tickets keep the words they were written in.
