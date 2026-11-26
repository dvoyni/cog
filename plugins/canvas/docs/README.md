# canvas guide

How to use `github.com/dvoyni/cog/plugins/canvas`, with examples: recording a
frame of entries, shapes, sprites and the images they draw, cameras, 2D
materials and their arguments, text, meshes, the frame's other work, and
testing a plugin that records into canvas. For the reasons behind each
choice, see [design.md](design.md); for the words it uses,
[CONTEXT.md](../CONTEXT.md). It assumes the
[kernel guide](../../../kernel/docs/README.md), the
[platform guide](../../platform/docs/README.md) and the
[render guide](../../render/docs/README.md).

A plugin that records into canvas uses canvas alone for 2D: it names no gfx
type, and reaches render only for what canvas leaves to it, such as a
texture a camera draws into.

- [Concepts](#concepts)
- [Quick start](#quick-start)
- [The plugin and its Config](#the-plugin-and-its-config)
- [The frame](#the-frame)
- [Entries and the sort key](#entries-and-the-sort-key)
- [Shapes](#shapes)
- [Sprites](#sprites)
- [Images and the atlas](#images-and-the-atlas)
- [Sliced and tiled sprites](#sliced-and-tiled-sprites)
- [Converters and distance fields](#converters-and-distance-fields)
- [Images of render textures](#images-of-render-textures)
- [Cameras](#cameras)
- [Logical size, fit and pixel-perfect cameras](#logical-size-fit-and-pixel-perfect-cameras)
- [Clips and culling](#clips-and-culling)
- [Converting a click](#converting-a-click)
- [2D materials](#2d-materials)
- [Material arguments](#material-arguments)
- [Font sets and languages](#font-sets-and-languages)
- [Text](#text)
- [Spans, icons and markup](#spans-icons-and-markup)
- [Per-frame triangles](#per-frame-triangles)
- [Mesh files](#mesh-files)
- [Meshes of a layout of your own](#meshes-of-a-layout-of-your-own)
- [Inspections](#inspections)
- [Hot reload](#hot-reload)
- [Device recovery](#device-recovery)
- [Reports](#reports)
- [Testing with canvastest](#testing-with-canvastest)
- [Rules](#rules)

## Concepts

```
 render.FrameEvent, once render has readied the frame
        │
      canvas ── FrameEvent{Ready, AcquireWait, GPULatency}
        │          │
        │          └─ your recording tasks, after Ready, joined into the out dependency
        │                 CommandList: SetCamera, DrawShape, DrawSprite(s), DrawText,
        │                              DrawTriangles, DrawMesh(es), and each …With
        │
        ├─ merges the command lists, sorts the entries by layer and order,
        │  culls and batches them through each camera, and draws them
        ├─ Assets: materials, preloads, unloads, image sizes, ReloadChanged, inspections
        ├─ FontLibrary: font sets, the language, layouts and metrics
        ├─ Targets: each target as it was last drawn, for a camera's conversions
        └─ InspectedEvent
```

| Type | What it is |
|---|---|
| `Plugin` | The kernel plugin, from `NewPlugin()`. |
| `Config` | canvas's part of the kernel's config: the atlases, mips, filters, text and the default font set. |
| `Handlers` | canvas's handlers, for others to order theirs around. |
| `FrameEvent` | The frame canvas's users record, published inside each of render's `FrameEvent`s. |
| `CommandList` | The task parameter a recording task records its entries and sets its cameras through. |
| `Assets` | The task parameter a task reaches canvas's assets and tools through. |
| `FontLibrary`, `WriteFontLibrary` | The resource of font sets, read under `kernel.Read`, and its writing side, from `ElevateFontLibrary`. |
| `Targets` | The resource of each target canvas drew into, as it was last drawn. |
| `Shape`, `Sprite`, `Text`, `Mesh` | Entries: plain values, literals, an ECS component can hold one. |
| `Triangles[V]`, `DrawTrianglesOptions`, `Vertex` | A mesh given for one frame, how it is drawn, and canvas's standard vertex. |
| `Camera`, `LayerRange`, `Fit` | A view of the frame's entries into a window or a texture, set for one frame. |
| `Image`, `ImageOptions`, `MeshFile` | Pointer-free names of an image and a mesh file, from `ImageFromFile`, `ImageFromTexture` and `MeshFileFromFile`. |
| `Converter`, `DistanceField` | A conversion of an image's texels on the CPU before it is packed, into the format it names, the atlas canvas keeps for it shared by equal converters' images; and the one baking a distance field. |
| `Material`, `MaterialDescriptor`, `ArgumentType` | A 2D material, what it is made of, and the type of the argument each entry gives it. |
| `Profile` | A band of the distance from an entry's edge: an outline, a halo, a glow or a drop shadow. |
| `FontSet` | A set of fonts serving languages, named by text, never a font. |
| `TextLayout` | A run of text laid out and measured. |
| `Span`, `IconDescriptor` | A part of a run in its own tint, or an icon placed like a glyph. |
| `Inspection` | One frame written down in canvas's terms. |
| `Report`, `ReportKind` | A mistake canvas finds at run time, and its kind. |
| `font.SetDescriptor`, `font.Font`, `font.Metrics` | What a font set is made of, each font file in it and its options, and a font's vertical measures, in the font package. |
| `font.Shaper`, `font.Layout` | Runes laid out in a loaded `font.Set` and its glyphs' coverage, as canvas lays text out, for a plugin's own text. |
| `atlas.Atlas`, `atlas.Descriptor` | One atlas of one format, as canvas packs its sprites and glyphs into, for a plugin's own caches. |
| `canvastest.Harness` | canvas in a real kernel on render's fake GPU, or on wgpu's software adapter, for tests. |

Words used throughout: an **entry** is one thing recorded into a command list
for a frame, a shape, a sprite, a run of text or a mesh; its **sort key** is
its layer, then its order; a **batch** is a run of entries in sort order
drawn as one instanced draw; a **2D material** is canvas's, not render's,
and a **material argument** the value of its own type each entry gives it;
an **atlas** is one 2D-array texture whose layers are pages of packed images
or glyphs; the **white texel** is the zero `Image`, which shapes draw.

## Quick start

A game opens a window and draws a ground strip, its hero standing on it, and
the hero's name in an outline, every render beat. Its plugin mounts the
files `main` hands it, and its `Start` publishes an event whose task opens
the window. Each of canvas's `FrameEvent`s records the three entries; with
no camera set, canvas draws them through its default camera into the main
window, cleared to black, the world's origin at its centre and y down. The
hero's image is named once, and its first draw loads and packs it. With no
update track, the render beat pumps platform's events.

```go
package main

import (
	"embed"
	"io/fs"
	"runtime"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/gogpu"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

const gameName kernel.PluginName = "game"

//go:embed assets
var embedded embed.FS

// hero is the hero's picture, standing on its bottom edge. An Image is a
// name: it loads nothing until a sprite first draws it.
var hero = canvas.ImageFromFile("hero.png", canvas.ImageOptions{Pivot: canvas.PivotBottom})

// The game's layers, from the ground up.
const (
	layerGround int32 = 0
	layerHero   int32 = 10
	layerLabels int32 = 20
)

// openEvent is published by the game's Start: work is scheduled only in a
// publication.
type openEvent struct{}

// game opens a window and draws its scene into it each render beat, from
// files, its read mount.
type game struct {
	supervisor  *kernel.Supervisor
	files       fs.FS
	openWindow  *kernel.Task[kernel.NoArg, kernel.NoResult]
	drawScene   *kernel.Task[canvas.FrameEvent, kernel.NoResult]
	closeWindow *kernel.Task[platform.Window, kernel.NoResult]
}

func (p *game) Name() kernel.PluginName { return gameName }

// Dependencies names platform, whose Tasks Init reads, and canvas, whose
// frame the game records.
func (p *game) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{platform.PluginName, canvas.PluginName}
}

func (p *game) Init(configurator *kernel.Configurator, _ any) error {
	configurator.BindSocket(platform.ReadMountSocket{FS: p.files})
	p.openWindow = configurator.RegisterTask1(openWindow)
	p.drawScene = configurator.RegisterTask1(drawScene)
	p.closeWindow = configurator.RegisterTask1(closeWindow)

	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event openEvent) (openEvent, kernel.JobHandle) {
		return event, ex.Schedule(p.openWindow, kernel.NoArg{}, in).Handle()
	})
	// The recording starts once canvas has opened the frame, and canvas
	// draws it once what is joined into the out dependency has finished.
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event canvas.FrameEvent) (canvas.FrameEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(p.drawScene, event, event.Ready).Handle())
	})
	tasks, _ := configurator.Resource[platform.Tasks]()
	processEvents := tasks.ProcessEvents
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, beat gfx.RenderEvent) (gfx.RenderEvent, kernel.JobHandle) {
		return beat, ex.Join(in, ex.Schedule(processEvents, kernel.NoArg{}).Handle())
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event platform.CloseRequestedEvent) (platform.CloseRequestedEvent, kernel.JobHandle) {
		return event, ex.Schedule(p.closeWindow, event.Window, in).Handle()
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event platform.WindowClosedEvent) (platform.WindowClosedEvent, kernel.JobHandle) {
		if p.supervisor != nil {
			p.supervisor.Quit()
		}
		return event, in
	})
	return nil
}

func (p *game) Start(k *kernel.Kernel) error {
	k.Publish(openEvent{})
	return nil
}

func (p *game) Stop(*kernel.Kernel) error { return nil }
func (p *game) Destroy() error             { return nil }

// openWindow opens the game's window, which opens on the next pump.
func openWindow(k *kernel.Kernel, _ kernel.NoArg, manager kernel.Write[*platform.WindowManager]) kernel.NoResult {
	manager.Get().Open(platform.WindowEntry{Title: "Hero", Size: platform.Size{Width: 1280, Height: 720}})
	return kernel.NoResult{}
}

// drawScene records the ground, the hero standing on it, and the hero's
// name above it.
func drawScene(k *kernel.Kernel, event canvas.FrameEvent, list canvas.CommandList) kernel.NoResult {
	list.DrawShape(canvas.Shape{
		Kind:  canvas.Fill,
		Rect:  m.Rect{X: -640, Y: 100, Width: 1280, Height: 260},
		Tint:  m.NewColorSrgb8(70, 110, 60, 255),
		Layer: layerGround,
	})
	list.DrawSprite(canvas.Sprite{Image: hero, Transform: m.Translation2x3(0, 100), Layer: layerHero})
	list.DrawTextWith(canvas.Text{
		String:    "Hero",
		Size:      24,
		Pivot:     canvas.PivotBottom,
		Transform: m.Translation2x3(0, -40),
		Layer:     layerLabels,
	}, canvas.Outline(2, m.Black))
	return kernel.NoResult{}
}

// closeWindow closes the window the user asked to close.
func closeWindow(k *kernel.Kernel, window platform.Window, manager kernel.Write[*platform.WindowManager]) kernel.NoResult {
	manager.Get().Close(window)
	return kernel.NoResult{}
}

func init() { runtime.LockOSThread() }

func main() {
	assets, err := fs.Sub(embedded, "assets")
	if err != nil {
		panic(err)
	}
	vsync := metronome.Variable()
	vsync.MinInterval = time.Second / 240
	vsync.MaxDelta = 250 * time.Millisecond

	supervisor := kernel.NewSupervisor(nil)
	config := map[kernel.PluginName]any{
		metronome.PluginName: &metronome.Config{
			Tracks: []metronome.Track{metronome.NewTrack[gfx.RenderEvent](vsync)},
		},
	}
	if _, err := supervisor.Spawn(config, runtime.GOMAXPROCS(0),
		metronome.NewPlugin(),
		platform.NewPlugin(),
		gfx.NewPlugin(),
		gogpu.NewPlugin(), // the backend, windows and input
		render.NewPlugin(),
		canvas.NewPlugin(),
		&game{supervisor: supervisor, files: assets},
	); err != nil {
		panic(err)
	}
	for supervisor.WaitMainThread() {
		supervisor.TickMainThread()
	}
}
```

`assets/hero.png` is any PNG. The hero stands on the ground's top edge, at
y 100, since its pivot is its bottom edge; its name's box stands on y −40,
140 units above the hero's feet, its outline under its fill. Each of the three
entries is in a layer of its own, so they draw in that order whatever order
they were recorded in. The label is shaped once, in the first frame, and
drawn from the layout cache after.

## The plugin and its Config

canvas is listed after render, platform and metronome, and every plugin that
records into canvas after canvas. Its `Config` sits under
`canvas.PluginName`, and each field has a default when zero:

```go
var canvasConfig = map[kernel.PluginName]any{
	canvas.PluginName: &canvas.Config{
		// Entries of equal keys in the UI's layer may be regrouped by batch.
		RegroupedLayers: []int32{layerUI},
		// Sprites naming no filter read their texels nearest, for pixel art;
		// zero means FilterLinear.
		DefaultFilter: canvas.FilterNearest,
		// Pages of 4096 texels a side; zero means 2048.
		AtlasPageSize: 4096,
		// At most 64 pages; zero means 256.
		AtlasLayerCeiling: 64,
		// Images over 1024 texels on a side are drawn from a texture of their
		// own; zero means half a page.
		OwnTextureSize: 1024,
		// Pages of 4 mip levels; zero means 3.
		MipLevels: 4,
		// The language at start, which picks each font set's fonts.
		Language: "en",
		// A run of text of no size is 18 world units to the em; zero means 16.
		TextSize: 18,
		// A layout unused for 4 frames is freed; zero means 2.
		LayoutIdleFrames: 4,
		// Glyphs are distance fields 64 texels to the em reaching 16 texels
		// across their edge; zero means 48 and 16.
		GlyphFieldEm:    64,
		GlyphFieldRange: 16,
		// The default font set, the zero FontSet's; zero is JetBrains Mono.
		DefaultFontSet: font.SetDescriptor{Label: "body", Default: font.Font{Path: "fonts/inter.ttf"}},
	},
}
```

`DoNotGenerateMipMaps` makes pages of one mip level, for a pixel-perfect game
whose sprites are never minified. A page side off the atlas grid is raised to
the next on it, and more than 12 mip levels lowered to 12, each reported once
as canvas starts.

canvas installs `Handlers`, its handler of each event it subscribes to, for a
plugin listed after it to order its own handler of the same event around
with `configurator.OrderSubscriptions`:

```go
func orderAfterCanvasRecovers(configurator *kernel.Configurator, mine kernel.Sub[render.RecoveredEvent]) {
	handlers, _ := configurator.Resource[canvas.Handlers]()
	configurator.OrderSubscriptions(handlers.Recover, mine)
}
```

## The frame

canvas publishes `FrameEvent` inside render's `FrameEvent` every render beat.
A handler schedules its recording tasks after `Ready`, the job opening the
frame for entries, and joins them into the out dependency it returns, as the
quick start's does, with no ordering among the handlers: canvas merges the
command lists, sorts and draws the frame after the last of them, and forgets
its entries and cameras. An entry recorded by a task not joined so, which
runs once canvas's drawing has begun, is refused and reported once for the
task. `FrameEvent` embeds the render track's `metronome.Beat`, and carries
render's `AcquireWait` and `GPULatency`.

A task reaches canvas through two parameters, declared in its signature as
`kernel.Read` is, and two resources:

| Parameter or resource | Locks | Carries |
|---|---|---|
| `CommandList` | canvas's draw state and `FontLibrary`, read | entries, cameras |
| `Assets` | canvas's draw state, `platform.FS`, and render's `Frame` and `Inventory`, read | materials, preloads, unloads, image sizes, `ReloadChanged`, inspections |
| `kernel.Read[canvas.FontLibrary]` | `FontLibrary`, read | the language, layouts, preloaded glyphs, metrics |
| `kernel.Read[canvas.Targets]` | `Targets`, read | each target as it was last drawn |

Each task declaring a `CommandList` gets a command list of its own as it is
registered, so recording tasks run together and never contend: an ECS
system recording units and another recording their labels run at once.
The lists merge in registration order, plugin order and then the order each
plugin registers its tasks. Each parameter is valid only while its task's
job runs: a task never keeps one.

```go
// recordUnits schedules the units' and their labels' recording, each into
// a command list of its own, after the frame opens.
func recordUnits(ex *kernel.Executor, in kernel.JobHandle, event canvas.FrameEvent, drawUnits, drawLabels *kernel.Task[canvas.FrameEvent, kernel.NoResult]) (canvas.FrameEvent, kernel.JobHandle) {
	units := ex.Schedule(drawUnits, event, event.Ready)
	labels := ex.Schedule(drawLabels, event, event.Ready)
	return event, ex.Join(in, units.Handle(), labels.Handle())
}
```

## Entries and the sort key

Every entry is a literal, a plain value an ECS component holds, recorded for
one frame. Its sort key is its `Layer`, then its `Order`: every entry of a
lower layer draws under every entry of a higher one, and within a layer by
order. Entries of equal keys draw by their command list's registration order,
then in the order recorded. A game names its layers in one place, with gaps:

```go
// The game's layers, from the ground up, with gaps for later ones.
const (
	layerGround  int32 = 0
	layerUnits   int32 = 10
	layerEffects int32 = 20
	layerLabels  int32 = 30
	layerUI      int32 = 100
)
```

`OrderOf(y)` maps a float onto an order of its own, in the same order, so
units y-sort with no scale to choose, and `OrderOf(y)-1` sorts just under:

```go
func drawUnit(list canvas.CommandList, body, shadow canvas.Image, position m.Vec2) {
	transform := m.Translation2x3(position.X, position.Y)
	order := canvas.OrderOf(position.Y)
	list.DrawSprite(canvas.Sprite{Image: shadow, Transform: transform, Layer: layerUnits, Order: order - 1})
	list.DrawSprite(canvas.Sprite{Image: body, Transform: transform, Layer: layerUnits, Order: order})
}
```

`OrderOf` keeps the order, not the distance, so y-sorted entries and
hand-stacked ones belong in different layers. A layer named in
`Config.RegroupedLayers` may have its entries of equal keys regrouped by
batch, for a UI or particles whose order among themselves does not matter.

Each kind has a bulk call beside its single one, so an ECS chunk's column goes
in as it is: `DrawSprites`, `DrawMeshes`, and with arguments
`DrawSpritesWith` and `DrawMeshesWith`.

## Shapes

A `Shape` fills or strokes a rectangle, or draws a line, in its `Tint`. A
stroke lies inside its rectangle as four fills that do not overlap, so a
translucent one does not darken its corners; a line is centred on its
segment; a zero `Width` is a hairline one target pixel wide at any zoom; and
`Radius` rounds a fill's or a stroke's corners. Its rectangle, ends, width
and radius are in its own space, which its `Transform` places:

```go
// The health bar's colours, converted from sRGB once rather than per unit.
var (
	healthBackground = m.NewColorSrgb(0.1, 0.1, 0.1, 0.8)
	healthFill       = m.NewColorSrgb(0.2, 0.8, 0.3, 1)
)

func drawHealthBar(list canvas.CommandList, position m.Vec2, health float32) {
	frame := m.Rect{X: position.X - 20, Y: position.Y - 48, Width: 40, Height: 6}
	list.DrawShape(canvas.Shape{Kind: canvas.Fill, Rect: frame, Radius: 3, Tint: healthBackground, Layer: layerLabels})
	filled := frame
	filled.Width *= health
	list.DrawShape(canvas.Shape{Kind: canvas.Fill, Rect: filled, Radius: 3, Tint: healthFill, Layer: layerLabels, Order: 1})
	list.DrawShape(canvas.Shape{Kind: canvas.Stroke, Rect: frame, Radius: 3, Tint: m.Black, Layer: layerLabels, Order: 2})
}

func drawSelection(list canvas.CommandList, from, to m.Vec2) {
	// A hairline: one pixel wide at any zoom.
	list.DrawShape(canvas.Shape{Kind: canvas.Line, From: from, To: to, Tint: m.White, Layer: layerUI})
}
```

Shapes draw the white texel of the sRGB sprite atlas, so they batch with the
sprites beside them.

## Sprites

A `Sprite` draws an `Image` through its `m.Mat2x3` `Transform` about its
image's pivot, at its `Size`, its image's in texels when zero, one zero axis
keeping the aspect, times its `Tint`, white when zero. `m.Translation2x3`,
`Rotation2x3`, `Scaling2x3` and `TRS2x3` make transforms, and `a.Mul(b)`
applies `b` first; a negative scale mirrors the sprite about its pivot.
`Crop` leaves a fraction out of each side where it lies, as a fill bar
empties, and `Filter` reads its texels linearly or nearest, zero meaning
`Config.DefaultFilter`.

```go
// The game's images, named once, as its art is.
var (
	knight  = canvas.ImageFromFile("units/knight.png", canvas.ImageOptions{Pivot: canvas.PivotBottom})
	manaBar = canvas.ImageFromFile("ui/mana.png", canvas.ImageOptions{Pivot: canvas.PivotTopLeft})
)

// drawKnight draws the knight facing right at a facing of 1, and mirrored,
// facing left, at -1.
func drawKnight(list canvas.CommandList, position m.Vec2, facing float32) {
	list.DrawSprite(canvas.Sprite{
		Image:     knight,
		Transform: m.TRS2x3(position, 0, m.Vec2{X: facing, Y: 1}),
		Layer:     layerUnits,
		Order:     canvas.OrderOf(position.Y),
	})
}

// drawManaBar draws the bar emptying from its right as mana falls from 1.
func drawManaBar(list canvas.CommandList, mana float32) {
	list.DrawSprite(canvas.Sprite{
		Image:     manaBar,
		Size:      m.Vec2{X: 200},
		Crop:      canvas.Crop{Right: 1 - mana},
		Transform: m.Translation2x3(-620, -340),
		Layer:     layerUI,
	})
}
```

Sprites of any images of one atlas batch together while their filter is one,
and the shapes beside them with them, so an army of different units is one
draw.

## Images and the atlas

`ImageFromFile(path, options…)` interns a path in platform's FS and its
`ImageOptions` into an `Image`, the same one for every call of the same path
and options. It loads nothing: the first frame drawing it acquires its file
from render, packs it into the sprite atlas of its colour space, and draws it
in that frame; a file render cannot load draws render's magenta, reported
once. Each call takes a lock and hashes the path, so a game names its art
once and keeps the images, never calling `ImageFromFile` for each entry.

```go
var (
	// A sheet of 4 by 4 frames, each its own cell, drawn by a sprite's Frame.
	walk = canvas.ImageFromFile("units/walk.png", canvas.ImageOptions{Sheet: canvas.Grid{Columns: 4, Rows: 4}, Pivot: canvas.PivotBottom})
	// The top left 64 texels square of a file, cut into 4 by 4 tiles; the
	// file is decoded once for all its rectangles.
	tiles = canvas.ImageFromFile("tiles.png", canvas.ImageOptions{SourceRect: m.Recti{Width: 64, Height: 64}, Sheet: canvas.Grid{Columns: 4, Rows: 4}})
	// A normal map's texels are data, sampled as they are.
	bumps = canvas.ImageFromFile("bumps.png", canvas.ImageOptions{ColorSpace: canvas.ColorSpaceLinear})
	// Bottom centre, 3 texels up.
	torch = canvas.ImageFromFile("torch.png", canvas.ImageOptions{Pivot: canvas.Pivot{Fraction: m.Vec2{Y: 0.5}, Texels: m.Vec2{Y: -3}}})
)

func drawWalker(list canvas.CommandList, position m.Vec2, step uint32) {
	list.DrawSprite(canvas.Sprite{Image: walk, Frame: step % 16, Transform: m.Translation2x3(position.X, position.Y), Layer: layerUnits})
}
```

An atlas is one 2D-array texture whose layers are pages of
`Config.AtlasPageSize`, each image a cell of its own, premultiplied, its
gutter continuing its edges, or holding its opposite edge along the axes it
wraps, on an atlas grid `Config.MipLevels` sets, so linear filtering never
reaches a neighbour at any mip level. A full atlas doubles its pages, up to
`Config.AtlasLayerCeiling`, where it evicts the images drawn least lately,
never one drawn this frame; an evicted image drawn again is loaded again. An
image larger than `Config.OwnTextureSize`, or one no page holds, is drawn
from a texture of its own. Nothing is refused for good.

A task loads an image ahead of its first draw behind a loading screen,
unloads a level's art, and sizes an image from its file's header before it
is drawn, through its `Assets` parameter:

```go
func loadLevel(k *kernel.Kernel, _ kernel.NoArg, assets canvas.Assets) kernel.NoResult {
	assets.Unload("level1/") // every image and mesh file under level1/, not level10/
	assets.Preload(walk)
	return kernel.NoResult{}
}

// measureTile answers a frame of tiles' size, 16 by 16 texels, from its
// file's header, before any frame draws it.
func measureTile(assets canvas.Assets) (m.Vec2, bool) {
	return assets.ImageSize(tiles)
}
```

`Preload` acquires the file now, so the frame drawing it first decodes
nothing. An unloaded path's regions are freed as canvas next draws, and a
later draw loads it again.

### An atlas of your own

canvas's atlases are values of the public `atlas` package, which a plugin
uses for a cache of its own, such as a UI's rasterised text, drawn through a
2D material of its own that binds the atlas's texture. An `atlas.Atlas` packs
images of one format into its pages and knows nothing of what they are: the
plugin keeps what lies where, what to evict when `Pack` answers
`atlas.ErrFull`, and when to `Free` a region. One task packs into it, then
records its copies and its mips in a render command list before any draw
sampling it:

```go
// labelCache is a UI's cache of rasterised labels, each kept where it was
// packed, drawn by the drawing task that owns it.
type labelCache struct {
	coverage *atlas.Atlas
	regions  map[string]render.TextureRegion
}

// recordLabel packs a label's coverage, one byte a texel, the first time it
// is drawn, and records what the atlas has still to copy into list.
func (c *labelCache) recordLabel(inventory render.Inventory, list *render.CommandList, label string, width, height uint32, coverage []byte) (render.TextureRegion, bool) {
	if c.coverage == nil {
		c.coverage = atlas.New(inventory, atlas.Descriptor{Label: "ui labels", Format: render.TextureFormatR8Unorm, PageSize: 1024, Gutter: 1, ClearGutter: true, LayerCeiling: 4})
	}
	region, packed := c.regions[label]
	if !packed {
		var err error
		if region, err = c.coverage.Pack(inventory, width, height); err != nil {
			return render.TextureRegion{}, false // too large, or full: evict a label and pack again
		}
		c.coverage.Put(region, coverage, atlas.WrapNone)
		c.regions[label] = region
	}
	c.coverage.RecordCopies(list, inventory)
	c.coverage.RecordMips(list)
	return region, true
}
```

Its draws bind `c.coverage.Texture()` as they record, since a growth
replaces it, and place a region with `TextureRect` and its page,
`region.Layer`. A cell filled on the GPU instead is drawn into
`Cell(region)` between `RecordCopies` and `RecordMips`, as canvas fills its
plain images. After a lost device, `Empty` frees every region, and the cache
packs its labels again.

## Sliced and tiled sprites

A sprite's `DrawMode` is `Stretched`, the zero; `Sliced`, cut into nine by its
image's `Slice` insets, its corners kept at their texels' size at any size,
turned or mirrored whole; or `Tiled`, repeating its image from its top left
corner, without seams along the axes its image's `Wrap` names. `TexelScale`
is the size each texel of a corner or a tile takes, for art drawn at 2x:

```go
var (
	panel = canvas.ImageFromFile("ui/panel.png", canvas.ImageOptions{Slice: canvas.Insets{Left: 12, Top: 12, Right: 12, Bottom: 12}, Pivot: canvas.PivotTopLeft})
	// A frame's centre left empty, so what is under it shows through.
	border = canvas.ImageFromFile("ui/border.png", canvas.ImageOptions{Slice: canvas.Insets{Left: 8, Top: 8, Right: 8, Bottom: 8}, SliceHollow: true})
	water  = canvas.ImageFromFile("water.png", canvas.ImageOptions{Wrap: canvas.WrapBoth, Pivot: canvas.PivotTopLeft})
)

func drawDialog(list canvas.CommandList, box m.Rect) {
	list.DrawSprite(canvas.Sprite{Image: panel, Size: m.Vec2{X: box.Width, Y: box.Height}, DrawMode: canvas.Sliced, TexelScale: 2, Transform: m.Translation2x3(box.X, box.Y), Layer: layerUI})
}

func drawLake(list canvas.CommandList, area m.Rect) {
	list.DrawSprite(canvas.Sprite{Image: water, Size: m.Vec2{X: area.Width, Y: area.Height}, DrawMode: canvas.Tiled, Transform: m.Translation2x3(area.X, area.Y), Layer: layerGround})
}
```

A tiled image and a plain one of the same file are different images, packed
apart, since their gutters differ.

## Converters and distance fields

An image's `Convert`, a `Converter`, converts each of its frames on the CPU
before it is packed: a trim, a palette swap, or `DistanceField`'s bake.
canvas reads and decodes the file itself in the job that first draws the
frame, or in `Preload`'s. A converter names the format its output is in,
for the image's colour space, and canvas keeps an atlas for each converter
and format. A converter is a comparable value, a plain struct, so images of
equal converters share their atlas and their texels, and batch together,
while those of a converter of other values draw from an atlas of their own.

`Convert` is given the frame's texels, straight alpha as its file holds
them, and answers its output's texels in that format, row by row with no
padding, premultiplied in linear: an sRGB texel is decoded, multiplied by
its alpha and encoded again, as `libs/m` does.

```go
// swapPalette is a Converter recolouring a unit's team colour, pure
// magenta in its art, into its team's. Its output is RGBA8, as a plain
// sprite's, so its atlas holds it as the plain atlas would.
type swapPalette struct {
	team color.NRGBA
}

func (s swapPalette) Format(space canvas.ColorSpace) render.TextureFormat {
	if space == canvas.ColorSpaceLinear {
		return render.TextureFormatRGBA8Unorm
	}
	return render.TextureFormatRGBA8UnormSrgb
}

func (s swapPalette) Convert(frame *image.NRGBA, space canvas.ColorSpace) ([]byte, image.Rectangle) {
	texels := make([]byte, 0, len(frame.Pix))
	for y := frame.Rect.Min.Y; y < frame.Rect.Max.Y; y++ {
		for x := frame.Rect.Min.X; x < frame.Rect.Max.X; x++ {
			texel := frame.NRGBAAt(x, y)
			if texel.R == 255 && texel.G == 0 && texel.B == 255 {
				texel.R = s.team.R
				texel.G = s.team.G
				texel.B = s.team.B
			}
			var red, green, blue, alpha uint8
			if space == canvas.ColorSpaceLinear {
				red = premultiply(texel.R, texel.A)
				green = premultiply(texel.G, texel.A)
				blue = premultiply(texel.B, texel.A)
				alpha = texel.A
			} else {
				red, green, blue, alpha = m.NewColorSrgb8(texel.R, texel.G, texel.B, texel.A).Premultiply().Srgb8()
			}
			texels = append(texels, red, green, blue, alpha)
		}
	}
	return texels, frame.Rect
}

// premultiply answers a linear channel multiplied by alpha, rounded.
func premultiply(channel, alpha uint8) uint8 {
	return uint8((uint16(channel)*uint16(alpha) + 127) / 255)
}

var redKnight = canvas.ImageFromFile("units/knight.png", canvas.ImageOptions{Pivot: canvas.PivotBottom, Convert: swapPalette{team: color.NRGBA{R: 200, A: 255}}})
```

The output's bounds place it within the frame, while the image's size and
pivot measure the frame, so a trimmed sprite, whose bounds are those of its
art within the frame, stays where it was. A trim answers its colour space's
RGBA8 format, so its sprites batch apart from plain sprites and shapes.

`DistanceField{Spread}` bakes an image's alpha into a distance field, white
premultiplied in its colour space's RGBA8 format, so the fields of one
`Spread` share an atlas and those of another draw from their own. Its
output is the frame grown by `Spread` on each side, so drawn with the sprite's
transform and size it lines up with the sprite. `SDFMaterial` draws a field
through a `Profile`, its argument: `Outline(width, color)`, `Halo(plateau,
reach, color)`, `Glow(reach, color)`, which adds, or `DropShadow(offset,
softness, color)`, whose offset is in world space, so every shadow falls the
same way. It draws the band alone, so a halo is two entries, the field under
the sprite. Drawn with it, a shape reads its distance from its rectangle and
radius instead, its quad grown to hold the band:

```go
var knightField = canvas.ImageFromFile("units/knight.png", canvas.ImageOptions{Pivot: canvas.PivotBottom, Convert: canvas.DistanceField{Spread: 8}})

func drawSelectedKnight(list canvas.CommandList, position m.Vec2, gold m.Color) {
	transform := m.Translation2x3(position.X, position.Y)
	order := canvas.OrderOf(position.Y)
	list.DrawSpriteWith(canvas.Sprite{Image: knightField, Material: canvas.SDFMaterial, Transform: transform, Layer: layerUnits, Order: order}, canvas.Halo(2, 8, gold))
	list.DrawSprite(canvas.Sprite{Image: knight, Transform: transform, Layer: layerUnits, Order: order})
}

func drawGlowingButton(list canvas.CommandList, button m.Rect, gold m.Color) {
	list.DrawShapeWith(canvas.Shape{Kind: canvas.Fill, Rect: button, Radius: 6, Tint: m.White, Material: canvas.SDFMaterial, Layer: layerUI}, canvas.Glow(8, gold))
	list.DrawShape(canvas.Shape{Kind: canvas.Fill, Rect: button, Radius: 6, Tint: gold, Layer: layerUI, Order: 1})
}
```

A shape's zero tint is transparent, so a shape drawn with `SDFMaterial`
gives one, `m.White` to draw the profile's colour as it is. A `Profile` of
your own sets its band's `Inner`, `Outer`, `Plateau` and `Falloff`, in field
texels, a shape's own space for a shape. Every outline and glow of a frame
batches, each with its own colour.

## Images of render textures

`ImageFromTexture(texture, options…)` names a texture made with render as an
`Image`, never packed: its entries draw the texture's mip level 0 and layer
0, where a camera into it draws, so a minimap camera's texture drawn as a
sprite shows the world it drew that frame. A texture of a format encoding
nothing that holds encoded colour, as a camera's into it does, is marked
`ColorSpaceSrgb`, the zero, and decoded as it is sampled:

```go
// Minimap is the game's resource holding its minimap's texture and the
// image naming it.
type Minimap struct {
	Texture render.Texture
	Image   canvas.Image
}

// makeMinimap makes the minimap's texture, once, behind a loading screen.
func makeMinimap(k *kernel.Kernel, _ kernel.NoArg, inventory render.Inventory, minimap kernel.Write[Minimap]) kernel.NoResult {
	texture := inventory.MakeTexture[byte](render.TextureDescriptor{Label: "minimap", Width: 256, Height: 256, Format: render.TextureFormatRGBA8Unorm}, nil)
	minimap.Set(Minimap{Texture: texture, Image: canvas.ImageFromTexture(texture)})
	return kernel.NoResult{}
}
```

The [Cameras](#cameras) section draws into it. It batches only with entries
of the same texture, and a texture render does not hold draws nothing,
reported once.

## Cameras

A `Camera` is a plain value set for one frame with `SetCamera`, forgotten
once drawn, as entries are: a game computes the cameras it wants each frame.
It draws the entries of its `Layers` into its target, a `Window` or a
`Texture`, the main window when it names neither, over its `Viewport`, in
fractions of the target, cleared there alone when it sets `Clear`. Its
`Pivot` of the viewport shows its `Position`, turned by its `Rotation` and
scaled by its `Zoom`. `MakeLayer(layer)` answers the layers holding one, and
`MakeLayers(ranges…)` up to four half-open ranges; four empty ones mean
every layer. An entry is drawn by every camera whose layers hold it.

A frame that sets no camera is seen through the default one: every layer,
the whole main window, cleared to opaque black, at order 0, drawing only a
frame with entries. Split screen, a minimap and a UI over both are four
cameras:

```go
// CameraSet is the game's resource of the cameras its last frame set, kept to
// convert a click through.
type CameraSet struct {
	Left, Right, Minimap, UI canvas.Camera
}

func setCameras(k *kernel.Kernel, event canvas.FrameEvent, list canvas.CommandList, minimap kernel.Read[Minimap], party kernel.Read[Party], cameraSet kernel.Write[CameraSet]) kernel.NoResult {
	world := canvas.MakeLayers(canvas.LayerRange{From: layerGround, To: layerLabels + 1})
	sky := m.NewColorSrgb8(90, 140, 200, 255)
	cameras := CameraSet{
		Left:    canvas.Camera{Layers: world, Viewport: m.Rect{Width: 0.5, Height: 1}, Position: party.Get().One, Clear: true, ClearColor: sky},
		Right:   canvas.Camera{Layers: world, Viewport: m.Rect{X: 0.5, Width: 0.5, Height: 1}, Position: party.Get().Two, Clear: true, ClearColor: sky},
		Minimap: canvas.Camera{Layers: canvas.MakeLayers(canvas.LayerRange{From: layerGround, To: layerUnits + 1}), Texture: minimap.Get().Texture, Size: m.Vec2{X: 4096, Y: 4096}, Clear: true, ClearColor: m.Black},
		// Its top left pivot makes its world coordinates the window's points.
		UI: canvas.Camera{Layers: canvas.MakeLayer(layerUI), Pivot: m.Vec2{X: -0.5, Y: -0.5}, Order: 1},
	}
	list.SetCamera(cameras.Minimap)
	list.SetCamera(cameras.Left)
	list.SetCamera(cameras.Right)
	list.SetCamera(cameras.UI)
	list.DrawSprite(canvas.Sprite{Image: minimap.Get().Image, Size: m.Vec2{X: 200, Y: 200}, Transform: m.Translation2x3(1060, 20), Layer: layerUI})
	cameraSet.Set(cameras)
	return kernel.NoResult{}
}

// Party is the game's resource of where its two players are.
type Party struct {
	One, Two m.Vec2
}
```

canvas begins one of render's command lists for each distinct `Order` of the
frame's cameras, at that order among every plugin's, which render refuses at
an order another list of the frame holds. Cameras of one order draw in one list,
those into a texture first, so the minimap is drawn before the window showing
it, then in the order they were set. The UI camera, at order 1, draws over
both halves, which it does not clear. A tilemap seen by two cameras records
the union of their views once.

A camera of a `SampleCount` of 4 smooths its entries' hard edges: it draws
into a multisampled texture of its viewport, resolves it, and draws it over
its viewport, so the rest of its target survives. In one that does not
clear, a material blending other than normally blends against transparent,
not against what the target holds.

```go
func setSmoothUICamera(list canvas.CommandList) {
	list.SetCamera(canvas.Camera{Layers: canvas.MakeLayer(layerUI), Pivot: m.Vec2{X: -0.5, Y: -0.5}, SampleCount: 4, Order: 1})
}
```

## Logical size, fit and pixel-perfect cameras

A camera's logical `Size` is the area of the world it is meant to show, which
its `Fit` fits into its viewport: `FitExpand`, the zero, shows more of the
world along the viewport's longer axis; `FitInscribe` draws bars; `FitOverlap`
covers the viewport, cutting the edges off; and `FitStretch` stretches each
axis. Zero on both axes is the viewport's own size, and zero on one axis
follows the viewport's aspect ratio, so `Size{Y: 1080}` is 1080 units tall
at any width.

A `PixelPerfect` camera, for pixel art, one art pixel a world unit, draws a
whole number of target pixels a unit, its entries moved whole onto the
world-unit grid and its view placed on whole pixels, so the world keeps its
grid as the view scrolls a pixel at a time. `FitStretch`, whose pixels would
not be square, is drawn as `FitInscribe` and reported once.

```go
func setPixelCamera(list canvas.CommandList, focus m.Vec2) {
	list.SetCamera(canvas.Camera{
		Position:     focus,
		Size:         m.Vec2{X: 320, Y: 180},
		Fit:          canvas.FitInscribe,
		PixelPerfect: true,
		Clear:        true,
		ClearColor:   m.Black,
	})
}
```

At 320×180, it draws 6 pixels a unit in a 1080p window and 12 on a display
of scale 2. A pixel-perfect game also sets `Config.DefaultFilter` to
`FilterNearest` and `Config.DoNotGenerateMipMaps`.

## Clips and culling

An entry's `Clip` is a rectangle of the world it draws nothing outside, seen
through each camera as the entry is, so a scrolled log stays clipped under
every camera, zoom and display scale. A change of clip breaks a batch.

```go
func drawLog(list canvas.CommandList, box m.Rect, lines []string, scroll float32) {
	for index, line := range lines {
		list.DrawText(canvas.Text{
			String:    line,
			Pivot:     canvas.PivotTopLeft,
			Transform: m.Translation2x3(box.X, box.Y+float32(index)*20-scroll),
			Clip:      box,
			Layer:     layerUI,
		})
	}
}
```

Each camera culls the entries whose bounds miss its visible rectangle, or
their clip's part of it, so an entry no camera sees costs only its
recording: a game records its whole level and lets canvas cull it.

## Converting a click

`ScreenToWorld`, `WorldToScreen` and `VisibleRect` convert through a camera
as its target was last drawn, read from the `Targets` resource. canvas
forgets its cameras, so a game keeps the ones it set, as the
[Cameras](#cameras) section's `CameraSet` does, and converts a click through the
camera that drew where it landed:

```go
func selectPoint(k *kernel.Kernel, event platform.KeyEvent, cameraSet kernel.Read[CameraSet], targets kernel.Read[canvas.Targets], selection kernel.Write[Selection]) kernel.NoResult {
	if !event.Pressed || !event.Key.IsMouseButton() {
		return kernel.NoResult{}
	}
	click := m.Vec2{X: event.Position.X, Y: event.Position.Y}
	for _, camera := range []canvas.Camera{cameraSet.Get().Left, cameraSet.Get().Right} {
		if world, inside := camera.ScreenToWorld(click, targets.Get()); inside {
			selection.Set(Selection{Point: world})
			break
		}
	}
	return kernel.NoResult{}
}

// Selection is the game's resource of the world point last clicked.
type Selection struct {
	Point m.Vec2
}
```

The UI camera covers the whole window, its world the window's points, so a
widget tests the click against its own rectangle before the world takes it.
When cameras overlap, a click is routed by trying them from the highest order
down. `VisibleRect` answers the world rectangle a camera sees, what a game
spawns just outside of.

## 2D materials

An entry is drawn with its `Material`, the zero one the default material, its
image times its tint. A task makes a 2D material through its `Assets`
parameter from a `MaterialDescriptor`: its `Shader`, the path of a whole WGSL
file that includes canvas's include and declares only `@group(1)`, its
parameters; its entry points, a shader of a fragment stage alone drawing
through canvas's vertex stage, so it serves sprites, shapes, text and meshes
of `Vertex` alike; its `Blend`, premultiplied, zero meaning `BlendNormal()`,
with `BlendAdditive()`, `BlendMultiply()`, `BlendScreen()` and
`BlendOpaque()` ready; and its `CullMargin`, for a vertex stage that moves
vertices.

```go
// MaterialSet is the game's resource of the 2D materials it made.
type MaterialSet struct {
	Pulse canvas.Material
}

func makeEffects(k *kernel.Kernel, _ kernel.NoArg, assets canvas.Assets, materials kernel.Write[MaterialSet]) kernel.NoResult {
	materials.Set(MaterialSet{Pulse: assets.MakeMaterial(canvas.MaterialDescriptor{Label: "pulse", Shader: "shaders/pulse.wgsl", Blend: canvas.BlendAdditive()})})
	return kernel.NoResult{}
}

// drawPulsing sets the pulse's time, from the clock that owns it, and draws
// a spark with it.
func drawPulsing(k *kernel.Kernel, event canvas.FrameEvent, list canvas.CommandList, assets canvas.Assets, materials kernel.Read[MaterialSet]) kernel.NoResult {
	pulse := materials.Get().Pulse
	assets.SetMaterialUniform(pulse, "pulse_time", [4]float32{float32(event.TimeFromStart.Seconds())})
	list.DrawSprite(canvas.Sprite{Image: spark, Material: pulse, Layer: layerEffects})
	return kernel.NoResult{}
}

var spark = canvas.ImageFromFile("effects/spark.png")
```

`assets/shaders/pulse.wgsl`, a fragment stage alone. The include is read
from canvas's mount, `cog.canvas`, by a path relative to the shader's own:

```wgsl
#include ../cog.canvas/canvas.wgsl

@group(1) @binding(0) var<uniform> pulse_time: vec4<f32>;

@fragment
fn pulse_fragment(in: CanvasVaryings) -> @location(0) vec4<f32> {
	let color = canvas_sample(in) * in.tint;
	if !canvas_covers(in) {
		discard;
	}
	return canvas_output(color * (0.75 + 0.25 * sin(pulse_time.x * 6.0)));
}
```

The include declares every binding of `@group(0)` and `@group(2)`, and the
functions over them, each named `canvas_…`, a prefix a material's shader
leaves to canvas:

| Function | Answers |
|---|---|
| `canvas_sample(in)` | the entry's image premultiplied and linear, sliced or tiled as its draw mode says: the one way a material samples it |
| `canvas_covers(in)` | whether the fragment lies in the entry: outside a rounded shape's corners or a hollow centre it does not |
| `canvas_output(color)` | premultiplied linear colour encoded as the target needs |
| `canvas_argument(in)` | the entry's material argument, with `CANVAS_ARGUMENT` defined |
| `canvas_shape_distance(in)`, `canvas_is_shape(…)` | a shape's signed distance from its edge, and whether an instance is a shape |
| `canvas_band_coverage(…)` | a profile's band, as the SDF and text materials draw it |
| `canvas_glyph_spread(…)`, `canvas_glyph_pen(…)`, `canvas_glyph_is_bitmap(…)`, `canvas_glyph_is_icon(…)` | a glyph's field, for a material of runs |
| `canvas_make_quad_varyings(…)`, `canvas_world_to_clip(…)` | a vertex stage of your own's placing, as [Meshes of a layout of your own](#meshes-of-a-layout-of-your-own) shows |

`canvas_sample` is called in uniform control flow, before any discard, as
the default material does. `SetMaterialTexture` binds a texture of
`@group(1)` by name, as render's setter does. A draw uses a material's
parameters as they stand when canvas draws the frame, after every task hung
off `FrameEvent`. `ReleaseMaterial` gives one back, the frame recording as
it is released still drawing it. The default material's, `SDFMaterial`'s and
`TextMaterial`'s parameters are canvas's, refused and reported once.

## Material arguments

A material may take an argument: a value of its own type each entry gives
it, declared with `ArgumentOf[T]()`, a plain struct with no pointer mirroring
the shader's WGSL `struct Argument`. canvas preprocesses the shader with the
define `CANVAS_ARGUMENT`, checks the layouts match as it makes the material,
and a fragment stage reads the entry's with `canvas_argument`. Entries of one
material batch whatever their arguments, and one giving none gives the zero
value:

```go
// Dissolve is the dissolve material's argument, as dissolve.wgsl's struct
// Argument lays it out.
type Dissolve struct {
	Amount float32
	_      [3]float32 // padding to Colour's offset, 16
	Colour m.Color    // premultiplied
}

func makeDissolve(assets canvas.Assets) canvas.Material {
	return assets.MakeMaterial(canvas.MaterialDescriptor{Label: "dissolve", Shader: "shaders/dissolve.wgsl", Argument: canvas.ArgumentOf[Dissolve]()})
}

// drawDying draws the dying units, whose sprites took the dissolve material
// as they began to die, each at its own amount.
func drawDying(list canvas.CommandList, dissolve canvas.Material, sprites []canvas.Sprite, dissolves []Dissolve) {
	list.DrawSpritesWith(sprites, dissolves)
	list.DrawShapeWith(canvas.Shape{Kind: canvas.Fill, Rect: m.Rect{Width: 64, Height: 64}, Tint: m.White, Material: dissolve, Layer: layerEffects}, Dissolve{Amount: 0.5, Colour: m.White})
}
```

`assets/shaders/dissolve.wgsl`:

```wgsl
#include ../cog.canvas/canvas.wgsl

struct Argument {
	amount: f32,
	colour: vec4<f32>,
}

@fragment
fn dissolve_fragment(in: CanvasVaryings) -> @location(0) vec4<f32> {
	let color = canvas_sample(in) * in.tint;
	let argument = canvas_argument(in);
	if !canvas_covers(in) || in.uv_local.x < argument.amount {
		discard;
	}
	return canvas_output(color * argument.colour);
}
```

Every entry kind has a `…With` call: `DrawShapeWith`, `DrawSpriteWith`,
`DrawSpritesWith`, `DrawTextWith`, `DrawTrianglesWith`, `DrawMeshWith` and
`DrawMeshesWith`, the bulk ones taking a parallel slice, refused whole when
the lengths differ. An argument of another type, or one given a material
that takes none, is refused and its entry skipped, reported once.

## Font sets and languages

Text names a font set and a size, never a font, so switching the language
switches the typeface. A task makes a set on purpose, at a start or behind a
loading screen, through `FontLibrary`'s writing side, which only a write
handle gives: every font file the set names loads then, each language served
by its own fonts and the rest by the default font. The zero `FontSet` is the
default set, `Config.DefaultFontSet`, JetBrains Mono alone unless it names
others.

```go
// FontChoice is the game's resource of the font sets it made.
type FontChoice struct {
	Title canvas.FontSet
	Pixel canvas.FontSet
}

func loadFonts(k *kernel.Kernel, _ kernel.NoArg, libraryHandle kernel.Write[canvas.FontLibrary], files kernel.Read[platform.FS], fontSets kernel.Write[FontChoice]) kernel.NoResult {
	library := canvas.ElevateFontLibrary(libraryHandle, files)
	title := library.MakeFontSet(font.SetDescriptor{
		Label:   "title",
		Default: font.Font{Path: "fonts/inter-variable.ttf", Variations: [4]font.Variation{{Axis: font.AxisTagFromName("wght"), Value: 800}}},
		Languages: []font.LanguageFonts{
			// Thin strokes take a larger field em, so each spans two texels.
			{Languages: []string{"ja", "zh-Hans"}, Fonts: []font.Font{{Path: "fonts/noto-sans-cjk.ttc", Index: 0, FieldEm: 64}}},
			{Languages: []string{"ar"}, Fonts: []font.Font{{Path: "fonts/noto-sans-arabic.ttf"}}},
		},
	})
	// A pixel font, 8 pixels to the em, drawn as coverage at whole
	// multiples of its size.
	pixel := library.MakeFontSet(font.SetDescriptor{Label: "pixel", Default: font.Font{Path: "fonts/pixel.ttf", BitmapEm: 8}})
	library.SetLanguage("ja")
	fontSets.Set(FontChoice{Title: title, Pixel: pixel})
	return kernel.NoResult{}
}
```

Languages are tags matched exactly, a language's first font measuring it. A
font file that fails to load is reported once and left out of its set, a
failed default font replaced by JetBrains Mono. `DestroyFontSet` destroys a
set, which is then laid out as the default one. A `font.Variation` sets a
variable font's axis, `font.AxisTagFromName("wght")` its tag.

`FontLibrary`, read under `kernel.Read`, answers the current language, a
font's `font.Metrics` at a size, and text laid out with no frame, from the
layout cache `DrawText` lays out through, so what is measured is what is
drawn:

```go
func measureButton(library kernel.Read[canvas.FontLibrary], fontSets kernel.Read[FontChoice]) m.Vec2 {
	metrics := library.Get().Metrics(fontSets.Get().Title, 24, "")
	layout := library.Get().Layout(canvas.Text{String: "Continue", Font: fontSets.Get().Title, Size: 24, WrapWidth: 200, Align: canvas.TextAlignCenter})
	padding := metrics.CapHeight
	return m.Vec2{X: layout.Size().X + 2*padding, Y: layout.Size().Y + padding}
}
```

A plugin laying out text of its own, outside canvas's entries, loads a set
with `font.Load` and lays runes out with a `font.Shaper`, one per job, since
it is not safe for concurrent use: `LayOut` answers a `font.Layout` of
lines, cluster positions and glyphs, each naming the `font.Loaded` drawing it
and the rune it leads back to, and `Rasterize` a glyph's coverage. An icon
crosses as a `font.Object`, a rune laid out as a box of its width.

`FontLibrary.Preload(set, text)` makes the glyphs of a string ahead of their
first draw, behind a loading screen. A glyph made in the frame costs a
fraction of a millisecond for Latin and nearly two for a Han character, so a
game showing new CJK text preloads it.

The glyph atlas grows as a sprite atlas does, up to `Config.AtlasLayerCeiling`,
where it evicts the glyphs drawn least lately, never one drawn this frame; an
evicted glyph drawn again is packed again from its field, which is kept. A
glyph it has no room for draws nothing that frame, reported once as
`ReportKindAtlasFull`, and is drawn in a later frame with room.

## Text

A `Text` is a run of text: its `String`, a newline a hard break; its `Font`
set and `Size`, its em in world units, zero meaning `Config.TextSize`; its
`Language`, zero meaning the current one; its `WrapWidth`, zero meaning no
wrapping; its `Align`; its `LineHeight` and `Tracking`. As an entry it also
carries a `Transform` about its `Pivot`, a point of its layout's box,
`PivotBottom` standing a label over a unit and `PivotTopLeft` laying out a
UI, its `Tint`, its `Clip`, its sort key, and its `Material`, zero meaning
`TextMaterial`. `DrawText` records a run as one entry, laid out through the
layout cache, so a label drawn frame after frame is shaped once.

`TextMaterial`'s argument is a `Profile` drawn under the fill, in the font's
field texels, so each label's outline lies under its own fill and every run
of a frame batches, whatever its size, tint, profile or icons:

```go
func drawNameplate(list canvas.CommandList, fontSets FontChoice, name string, position m.Vec2) {
	list.DrawTextWith(canvas.Text{
		String:    name,
		Font:      fontSets.Title,
		Size:      14,
		Pivot:     canvas.PivotBottom,
		Transform: m.Translation2x3(position.X, position.Y-40),
		Layer:     layerLabels,
	}, canvas.Outline(2, m.Black))
}

func drawStory(list canvas.CommandList, story string, box m.Rect) {
	list.DrawText(canvas.Text{
		String:     story,
		Size:       18,
		WrapWidth:  box.Width,
		Align:      canvas.TextAlignStart,
		LineHeight: 1.2,
		Pivot:      canvas.PivotTopLeft,
		Transform:  m.Translation2x3(box.X, box.Y),
		Tint:       m.NewColorSrgb8(240, 230, 210, 255),
		Layer:      layerUI,
	})
}
```

Text is shaped in its language by go-text, ordered by Unicode's bidi
algorithm, so a paragraph in Arabic starts at the right, and broken on
Unicode's line-break opportunities, so Japanese breaks between ideographs.
A character no font of the set has draws the default font's missing-glyph
box, reported once for the set.

A `TextLayout` answers `Size()`, its box, `LineCount()`, `Baseline()`,
`LineWidth(line)` and `ClusterPosition(cluster)`, where a grapheme cluster
starts on its line's baseline, for a caret:

```go
func findCaretPosition(library kernel.Read[canvas.FontLibrary], field canvas.Text, cursor int) m.Vec2 {
	return library.Get().Layout(field).ClusterPosition(cursor)
}
```

Small text, whose field spans more than 2 texels a pixel, is read at four
points a pixel and its glyphs placed on whole pixels. A bitmap font's run is
laid out at a whole multiple of its pixel size, read nearest, and draws no
profile.

## Spans, icons and markup

A run's `Spans`, when given, are the run instead of its `String`: each a
`Span` of `Text` in its own `Tint`, or an `Icon` registered by name, the cap
height tall and placed on the baseline like a glyph, a line breaking around
it as around a letter. A run whose `Markup` is set reads its `String` as the
string form of spans: `\i:name` an icon, `\c:colour{text}` text in `#rgb`,
`#rrggbb`, `#rrggbbaa` or a registered colour, and `\\`, `\{` and `\}` the
characters themselves. A run without `Markup`, such as a name a player
typed, is drawn as written.

```go
var prompts = canvas.ImageFromFile("ui/prompts.png", canvas.ImageOptions{Sheet: canvas.Grid{Columns: 8, Rows: 1}})

func registerPrompts(k *kernel.Kernel, _ kernel.NoArg, libraryHandle kernel.Write[canvas.FontLibrary], files kernel.Read[platform.FS]) kernel.NoResult {
	library := canvas.ElevateFontLibrary(libraryHandle, files)
	library.RegisterIcon("button-a", canvas.IconDescriptor{Image: prompts, Frame: 0, Scale: 1.2})
	library.RegisterIcon("fire", canvas.IconDescriptor{Image: prompts, Frame: 5, Tinted: true})
	library.RegisterColor("gold", m.NewColorSrgb8(255, 200, 60, 255))
	return kernel.NoResult{}
}

// DamageHint is a recording task's own state: the spans of its hint, made
// once, so a frame sets the damage alone and allocates nothing for them.
type DamageHint struct {
	Spans []canvas.Span
}

func makeDamageHint() DamageHint {
	fire := m.NewColorSrgb8(230, 60, 40, 255)
	return DamageHint{Spans: []canvas.Span{{Text: "Deals "}, {Tint: fire}, {Text: " "}, {Icon: "fire", Tint: fire}}}
}

func drawHints(list canvas.CommandList, hint *DamageHint, damage string) {
	list.DrawText(canvas.Text{String: `Press \i:button-a to \c:gold{jump}`, Markup: true, Layer: layerUI})
	hint.Spans[1].Text = damage
	list.DrawText(canvas.Text{Spans: hint.Spans, Transform: m.Translation2x3(0, 30), Layer: layerUI})
}
```

A registered colour is read as the run is drawn, so a theme changes it in one
place. A run is laid out again whenever its text changes, so a damage number
formatted anew shapes a new layout as it changes, and allocates for it. Markup that does not parse is drawn as written, reported once for its
string; an icon of a name not registered draws the missing-glyph box,
reported once for the name.

## Per-frame triangles

`DrawTriangles(options, triangles)` draws a mesh given for one frame, a
trail, a rope or a skin: a `Triangles[V]` of vertices, optional indices, a
topology and bounds, which are required, so the mesh is culled without
vanishing when its origin leaves the view. Its slices are copied as it is
recorded, so the caller reuses them. `Vertex{Position, UV, Color}` is
canvas's standard layout, drawn by canvas's mesh vertex stage and the
material's fragment stage, its UV running 0 to 1 over its image's region and
repeating past it along the axes the image wraps along, its `Color` zero
meaning white:

```go
// Trail is a recording task's own state: the vertices it reuses each frame.
type Trail struct {
	Vertices []canvas.Vertex
}

func drawTrail(list canvas.CommandList, trail *Trail, points []m.Vec2, spark m.Color) {
	trail.Vertices = trail.Vertices[:0]
	for index, point := range points {
		fade := float32(index) / float32(len(points))
		colour := m.Color{R: 1, G: 1, B: 1, A: fade}
		trail.Vertices = append(trail.Vertices,
			canvas.Vertex{Position: m.Vec2{X: point.X, Y: point.Y - 4}, UV: m.Vec2{X: fade}, Color: colour},
			canvas.Vertex{Position: m.Vec2{X: point.X, Y: point.Y + 4}, UV: m.Vec2{X: fade, Y: 1}, Color: colour},
		)
	}
	list.DrawTriangles(canvas.DrawTrianglesOptions{Tint: spark, Layer: layerEffects}, canvas.Triangles[canvas.Vertex]{
		Vertices: trail.Vertices,
		Topology: meshfile.TopologyTriangleStrip,
		Bounds:   meshfile.CalculateBounds(trail.Vertices, canvas.Vertex.Position3),
	})
}
```

`DrawTrianglesOptions` holds a sprite's fields but its size, pivot, draw
mode and crop: its `Image` and `Frame`, the zero `Image` the white texel, its
`Transform`, `Tint`, `Material`, `Clip`, `Filter` and sort key. Each call is a
draw of its own, never merged with another entry's.

## Mesh files

`MeshFileFromFile(path)` names a mesh file in platform's FS, written by
`meshfile.Encode`, as `ImageFromFile` names an image. `DrawMesh` records a
`Mesh{File, Image, Frame, Transform, Tint, Material, Clip, Filter, Layer,
Order}`, and `DrawMeshes` a slice: consecutive meshes in sort order of one
file, material, atlas, filter and clip are one instanced draw, so a forest of
one tree is one draw. A file loads at its first draw and stays until its path
is unloaded. A game ships its files in any read mount, or puts those it
builds into a memory mount of its own:

```go
// buildTree encodes a tree's mesh file and puts it into memory, a memory
// mount the game's plugin bound with platform.ReadMountSocket{FS: memory}.
func buildTree(memory *filesystems.MemoryFS) (canvas.MeshFile, error) {
	vertices := []canvas.Vertex{
		{Position: m.Vec2{X: 0, Y: -64}, UV: m.Vec2{X: 0.5, Y: 0}},
		{Position: m.Vec2{X: -32, Y: 0}, UV: m.Vec2{X: 0, Y: 1}},
		{Position: m.Vec2{X: 32, Y: 0}, UV: m.Vec2{X: 1, Y: 1}},
	}
	data, err := meshfile.Encode(vertices, nil, meshfile.TopologyTriangleList, meshfile.CalculateBounds(vertices, canvas.Vertex.Position3))
	if err != nil {
		return canvas.MeshFile{}, err
	}
	if err := memory.Put("meshes/tree.mesh", data); err != nil {
		return canvas.MeshFile{}, err
	}
	return canvas.MeshFileFromFile("meshes/tree.mesh"), nil
}

var bark = canvas.ImageFromFile("bark.png")

// plantForest answers a level's trees, made once as it loads, since they
// never move.
func plantForest(tree canvas.MeshFile, positions []m.Vec2) []canvas.Mesh {
	forest := make([]canvas.Mesh, 0, len(positions))
	for _, position := range positions {
		forest = append(forest, canvas.Mesh{File: tree, Image: bark, Transform: m.Translation2x3(position.X, position.Y), Layer: layerUnits, Order: canvas.OrderOf(position.Y)})
	}
	return forest
}

```

Each frame then records the whole forest in one `list.DrawMeshes(forest)`.
A game changes a file by putting it again and unloading its path, and the
next draw loads the new one. A file that is missing, is no mesh file, or has
zero bounds draws nothing, reported once for its path. A camera culls each
mesh by its file's bounds through its transform.

## Meshes of a layout of your own

A mesh of another vertex layout, a type whose `VertexLayout()` answers its
`meshfile.Layout`, draws through the material's own vertex stage, which
includes canvas's include and places each vertex through
`canvas_make_quad_varyings`, so swaying grass carries its sway per vertex.
Its material grows its entries' bounds by `CullMargin`, since its vertex
stage moves them:

```go
// GrassVertex is a blade's vertex: its position, and how far it sways, 0 at
// the root and 1 at the tip.
type GrassVertex struct {
	Position m.Vec2
	Sway     float32
}

func (GrassVertex) VertexLayout() meshfile.Layout {
	return meshfile.Layout{Stride: 12, Attributes: []meshfile.Attribute{
		{Format: meshfile.FormatFloat32x2, Offset: 0, Location: 0},
		{Format: meshfile.FormatFloat32, Offset: 8, Location: 1},
	}}
}

func makeGrass(assets canvas.Assets) canvas.Material {
	return assets.MakeMaterial(canvas.MaterialDescriptor{
		Label:              "grass",
		Shader:             "shaders/grass.wgsl",
		VertexEntryPoint:   "grass_vertex",
		FragmentEntryPoint: "grass_fragment",
		CullMargin:         16,
	})
}
```

`assets/shaders/grass.wgsl`:

```wgsl
#include ../cog.canvas/canvas.wgsl

@group(1) @binding(0) var<uniform> grass_wind: vec4<f32>;

struct GrassVertex {
	@location(0) position: vec2<f32>,
	@location(1) sway: f32,
}

@vertex
fn grass_vertex(vertex: GrassVertex, @builtin(instance_index) instance: u32) -> CanvasVaryings {
	let swayed = vertex.position + vec2<f32>(vertex.sway * grass_wind.x, 0.0);
	return canvas_make_quad_varyings(instance, canvas_instances[instance], swayed, vec2<f32>(0.5));
}

@fragment
fn grass_fragment(in: CanvasVaryings) -> @location(0) vec4<f32> {
	return canvas_output(canvas_sample(in) * in.tint);
}
```

A mesh of another layout drawn with a material whose vertex stage is
canvas's, as a fragment-only material's is, reads no vertex of its and is
skipped, reported once for the material.

## Inspections

A tool asks for an inspection of the next frame through its `Assets`
parameter, and reads it in a task off `InspectedEvent` joined into the
handler's out dependency: each camera the frame was seen through, its
batches, their entries and what it culled; the frame's first uses, images
packed, mesh files loaded, layouts and glyphs made; its reports; how long
canvas took to sort and to draw it; and render's own inspection of the frame
beside it. Its `String` writes it as text for an agent to log. Every other
frame pays one check.

```go
func requestInspection(k *kernel.Kernel, _ kernel.NoArg, assets canvas.Assets) kernel.NoResult {
	assets.RequestInspection()
	return kernel.NoResult{}
}

func logInspection(k *kernel.Kernel, event canvas.InspectedEvent, assets canvas.Assets) kernel.NoResult {
	if inspection, err := assets.ReadInspection(event); err == nil {
		log.Print(inspection)
	}
	return kernel.NoResult{}
}

func subscribeInspection(configurator *kernel.Configurator) {
	logTask := configurator.RegisterTask1(logInspection)
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event canvas.InspectedEvent) (canvas.InspectedEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(logTask, event, in).Handle())
	})
}
```

A request while one is pending joins it. Its sort and draw times come from
metronome's `Clock`, through the stopwatches `SortStopwatchName` and
`DrawStopwatchName`, which a test lengthens with the harness's
`LengthenNext`.

## Hot reload

`Assets.ReloadChanged()` reloads every image file, font file and mesh file
canvas read that changed since, as its modification time and size say,
reading no file that did not, and calls render's `ReloadChanged`, so the 2D
materials' shaders and their includes reload in the same call. A changed
image or mesh file is read again at its next draw, each handle kept; a set
naming a changed font file is made again under the same `FontSet` once the
next frame is drawn. Nothing reloads by itself: a game binds it to a key, and
in development mounts its asset folder from disk above the embedded one,
which never changes.

```go
func reloadOnF5(k *kernel.Kernel, event platform.KeyEvent, assets canvas.Assets) kernel.NoResult {
	if event.Pressed && event.Key == platform.KeyF5 {
		assets.ReloadChanged()
	}
	return kernel.NoResult{}
}

func mountForDevelopment(configurator *kernel.Configurator, embedded fs.FS) {
	configurator.BindSocket(platform.ReadMountSocket{FS: embedded})
	configurator.BindSocket(platform.ReadMountSocket{FS: os.DirFS("assets"), Priority: 1})
}
```

## Device recovery

When render recovers a lost device, canvas starts afresh on
`render.RecoveredEvent`: its atlases, glyphs and mesh files' buffers, which
the lost device left empty, are dropped, and each image, glyph and mesh file
is loaded again at its next draw. A game's images, font sets, materials and
mesh files keep their handles, and it does nothing for them. A render
texture a camera draws into each frame, such as the minimap's, is drawn
again; one the game wrote itself is refilled by the game, as render's guide
says.

## Reports

A mistake canvas finds at run time is a `Report`, logged once for its
subject through the kernel's `LogErrOnce` and carried past: the entry is
skipped or drawn the nearest way it can be. Its `ReportKind` names it for
tests:

| Kind | Raised by |
|---|---|
| `ReportKindRecordedOutsideFrame` | an entry recorded or a camera set while no frame records |
| `ReportKindCameraTargetRefused`, `ReportKindPixelPerfectStretch` | a camera naming both targets or a texture render does not hold; a pixel-perfect `FitStretch` |
| `ReportKindImageOptionsIgnored`, `ReportKindFrameOutOfBounds`, `ReportKindImageTextureRefused`, `ReportKindConverterRefused`, `ReportKindImageHeaderUnread` | an image's second set of options, a rectangle or frame past its file, a texture render does not hold, a converter that is not comparable, a header `ImageSize` cannot read |
| `ReportKindAtlasFull`, `ReportKindAtlasPageSizeRaised`, `ReportKindMipLevelsLowered` | an atlas at its ceiling, and a `Config` side or mip count put right as canvas starts |
| `ReportKindMaterialNotHeld`, `ReportKindArgumentRefused`, `ReportKindArgumentLayoutMismatch`, `ReportKindSliceLengthsDiffer` | a material canvas does not hold, an argument of the wrong type, a layout that does not match its shader, parallel slices of different lengths |
| `ReportKindFontNotLoaded`, `ReportKindFontSetNotHeld`, `ReportKindVariationIgnored`, `ReportKindCharacterMissing` | a font file that fails to load, a set canvas does not hold, a variation of no axis, a character no font of the set has |
| `ReportKindIconNotRegistered`, `ReportKindIconRefused`, `ReportKindMarkupMalformed` | an icon not registered, one refused as it is registered, markup that does not parse |
| `ReportKindBoundsMissing`, `ReportKindMeshFileNotLoaded`, `ReportKindVertexStageMissing` | per-frame triangles of zero bounds, a mesh file that fails to load, a mesh of a layout of its own drawn with canvas's vertex stage |

A file render cannot load, a shader that fails to compile, and the other
mistakes render finds are render's own reports. A programming error the
caller can check panics instead: a camera of a sample count other than 1 or
4, more than four layer ranges, or an argument type holding a pointer.

## Testing with canvastest

A good test of a plugin that records into canvas asserts what canvas drew, in
canvas's words, driving the plugin the way the game does: through the kernel
and canvas's parameters.

### The harness

`canvastest.NewHarness(t)` runs canvas in a real kernel on render's fake GPU:
it is rendertest's harness, whose calls it keeps, with canvas spawned after
render and a recording task of its own after canvas. `Spawn(renderConfig,
canvasConfig, plugins…)` spawns the test's plugins after canvas, and `Step`
draws one beat, its frame inspected in canvas's terms: `Frames` answers each
camera, its batches and their entries, and what it culled, with render's
inspection of the frame beside it. `Record(func(list canvas.CommandList))`
records a frame from the test without writing a plugin, and `OpenWindow`
opens a headless window to draw into.

The harness is strict: the test fails on every error the kernel logs, unless
it is a canvas report of a kind the test named with `ExpectReport`, a render
report named with the embedded harness's `ExpectReport`, or an error a match
given `ExpectError` answers true of. `Reports` answers every canvas report
checked so far.

The tests below test the quick start's game: `newGamePlugin(files)`, beside
it in package `main`, answers `&game{files: files}`. The game opens its own
window, which the harness's headless windows serve:

```go
func TestTheSceneDrawsTheGroundTheHeroAndItsName(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, newGamePlugin(os.DirFS("assets")))
	for range 3 {
		h.Step()
	}

	frames := h.Frames()
	cameras := frames[len(frames)-1].Cameras
	if len(cameras) != 1 {
		t.Fatalf("the last frame drew through %d cameras, want the default one", len(cameras))
	}
	kinds := map[canvas.EntryKind]int{}
	for _, batch := range cameras[0].Batches {
		for _, entry := range batch.Entries {
			kinds[entry.Kind]++
		}
	}
	if kinds[canvas.EntryKindShape] != 1 || kinds[canvas.EntryKindSprite] != 1 || kinds[canvas.EntryKindText] != 1 {
		t.Errorf("the default camera drew %v, want the ground, the hero and its name", kinds)
	}
}

func TestAMissingHeroDrawsMagentaReportedByRender(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Harness.ExpectReport(render.ReportKindLoadFailed)
	h.Spawn(nil, nil, newGamePlugin(fstest.MapFS{}))
	for range 3 {
		h.Step()
	}

	if reports := h.Harness.Reports(); len(reports) != 1 || reports[0].Kind != render.ReportKindLoadFailed {
		t.Errorf("render reported %v, want the missing hero alone", reports)
	}
}

func TestATextRecordedFromTheTestIsCulledOutsideTheView(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil)
	h.OpenWindow(platform.Size{Width: 320, Height: 240})
	h.Record(func(list canvas.CommandList) {
		list.DrawText(canvas.Text{String: "far away", Transform: m.Translation2x3(5000, 0)})
	})
	h.Step()

	frame := h.Frames()[0]
	if culled := frame.Cameras[0].Culled; len(culled) != 1 || culled[0].Kind != canvas.EntryKindText {
		t.Errorf("the camera culled %+v, want the run far away", culled)
	}
}
```

### Binding declarations for a 2D material of your own

gfx's recording adapter parses no WGSL, so the harness answers a shader's
reflection from its binding declaration, `pulse.wgsl.json` beside
`pulse.wgsl`, as render's guide says. Generate each with render's
`cmd/declarebindings`, which reads the shaders as the game does: `-directory`
mounts the game's `assets` at the root, the shaders' paths are given as the
game names them, and `-mount` mounts canvas's assets folder under
`cog.canvas`, beside them, as canvas does. A shader reading its material
argument is declared for `CANVAS_ARGUMENT` alone, since it compiles only with
it. In `main.go`:

```go
//go:generate go run github.com/dvoyni/cog/plugins/render/cmd/declarebindings -directory assets -mount cog.canvas=$CANVAS_ASSETS shaders/pulse.wgsl shaders/grass.wgsl
//go:generate go run github.com/dvoyni/cog/plugins/render/cmd/declarebindings -directory assets -mount cog.canvas=$CANVAS_ASSETS -variantsonly -variant CANVAS_ARGUMENT shaders/dissolve.wgsl
```

`go generate` expands `$CANVAS_ASSETS` from the environment, the folder of
canvas's module that `go list` answers, with `/assets` appended; inside this
repository, a path relative to `main.go` serves as well:

```
CANVAS_ASSETS=$(go list -m -f '{{.Dir}}' github.com/dvoyni/cog/plugins/canvas)/assets go generate ./...
```

Each declaration is written beside its shader, embedded with it, so the
harness finds it in the game's mount. Run it again whenever a shader's bindings, entry points or inputs
change; a pixel test catches a declaration gone stale.

### Pixel tests

A pixel test calls `UseSoftwareAdapter` before `Spawn` to draw on wgpu's
software adapter, and reads the window back through render's capture; its
package's `TestMain` calls `canvastest.RunMain`. It compares probes, texels
at chosen points where no filtering reaches, placed by fractions of the
picture, since a window's pixels per point come from the display it opens
on: 1.5 on a Windows display at 150 %, 1 under Linux.

```go
func TestMain(m *testing.M) { canvastest.RunMain(m) }

func TestARedFillCoversTheWindowsLeftHalf(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.UseSoftwareAdapter()
	capturer := &windowCapturer{}
	h.Spawn(nil, nil, capturer)
	capturer.arm(h.OpenWindow(platform.Size{Width: 320, Height: 240}))
	// The picture arrives a frame or more after the frame drawing it.
	for range 10 {
		h.Record(func(list canvas.CommandList) {
			list.DrawShape(canvas.Shape{Kind: canvas.Fill, Rect: m.Rect{X: -160, Y: -120, Width: 160, Height: 240}, Tint: m.Color{R: 1, A: 1}})
		})
		h.Step()
		if picture := capturer.picture(); picture != nil {
			bounds := picture.Bounds()
			if left := picture.NRGBAAt(bounds.Dx()/4, bounds.Dy()/2); left != (color.NRGBA{R: 255, A: 255}) {
				t.Errorf("the left half's middle is %v, want red", left)
			}
			if right := picture.NRGBAAt(bounds.Dx()*3/4, bounds.Dy()/2); right != (color.NRGBA{A: 255}) {
				t.Errorf("the right half's middle is %v, want the default camera's black", right)
			}
			return
		}
	}
	t.Fatal("no capture was delivered within ten beats")
}

// windowCapturer asks for a capture of the window it was armed with in each
// frame until one holds a picture, which it keeps.
type windowCapturer struct {
	mutex   sync.Mutex
	window  platform.Window
	read    *image.NRGBA
}

func (*windowCapturer) Name() kernel.PluginName { return "window capturer" }

func (*windowCapturer) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{render.PluginName}
}

func (c *windowCapturer) Init(configurator *kernel.Configurator, _ any) error {
	// Both hand the picture to the test, outside the kernel, so they stay
	// closures.
	request := configurator.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, frame render.Frame) kernel.NoResult {
		c.mutex.Lock()
		defer c.mutex.Unlock()
		if c.window != (platform.Window{}) && c.read == nil {
			_ = frame.RequestCapture(c.window) // busy while one is pending
		}
		return kernel.NoResult{}
	})
	keep := configurator.RegisterTask1(func(k *kernel.Kernel, event render.CapturedEvent, frame render.Frame) kernel.NoResult {
		if picture, err := frame.ReadCapture(event); err == nil {
			c.mutex.Lock()
			c.read = picture
			c.mutex.Unlock()
		}
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(request, kernel.NoArg{}, event.Ready).Handle())
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.CapturedEvent) (render.CapturedEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(keep, event, in).Handle())
	})
	return nil
}

func (*windowCapturer) Start(*kernel.Kernel) error { return nil }
func (*windowCapturer) Stop(*kernel.Kernel) error  { return nil }
func (*windowCapturer) Destroy() error             { return nil }

// arm has the frames after ask for a capture of window.
func (c *windowCapturer) arm(window platform.Window) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.window = window
}

// picture answers the picture captured, nil until it arrives.
func (c *windowCapturer) picture() *image.NRGBA {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.read
}
```

Where a probe must sit in a gradient, such as a distance-field band, it
allows ±2 a channel; elsewhere it is exact. Compare a pixel with another of
the same picture, such as a filtered edge with the middle of its column,
rather than with a value one display scale happens to give.

### The allocation gate

Nothing canvas does in a steady frame allocates once warm: recording entries
of every kind, singly and in bulk, sorting, culling and batching them, and
drawing them through each camera. canvas's own gate,
`allocationgate_test.go`, checks it with `testing.AllocsPerRun(...) == 0`
over whole beats, the harness's `Step`, after four warm frames, on gfx's
recording adapter recording nothing; design.md's *Performance* gives what it
covers and the time per entry against the same frame drawn by hand on
render. What allocates is a first use, a layout the cache has not seen, and
a frame's refusals.

`RecordNothing()`, called before `Spawn`, turns the recording adapter's
recording off and has `Step` ask for no inspection, so a gate of your own
plugin counts what its frames allocate; `Frames` then answers nothing. Run it
in plain `go test`: the race detector's runtime allocates on its own.

```go
func TestAWarmFrameOfTheSceneAllocatesNothing(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.RecordNothing()
	h.Spawn(nil, nil, newGamePlugin(os.DirFS("assets")))
	for range 4 {
		h.Step()
	}
	if allocations := testing.AllocsPerRun(10, h.Step); allocations != 0 {
		t.Errorf("a warm frame allocated %v times, want none", allocations)
	}
}
```

## Rules

- **Never reach gfx from a plugin that records into canvas**: go through
  `CommandList`, `Assets`, `FontLibrary` and `Targets`, and render's parameters
  only for a texture of your own.
- **Record from tasks scheduled after `FrameEvent.Ready`**, joined into the
  handler's out dependency.
- **Name images, mesh files and materials once, and keep them**: never call
  `ImageFromFile` or `MeshFileFromFile` for each entry.
- **Name layers in one place, with gaps**, and keep y-sorted entries in
  layers of their own, apart from hand-stacked ones.
- **Record in bulk** where an ECS chunk's column holds the entries:
  `DrawSprites`, `DrawMeshes` and their `…With` calls.
- **Record the whole level and let canvas cull it**; a tilemap seen by two
  cameras records the union of their views once.
- **Keep the cameras you set** to convert a click; canvas forgets them.
- **Make font sets, preload glyphs and images, and make materials behind a
  loading screen**, and preload new CJK text before it shows.
- **Give per-frame triangles their bounds**, and give a material whose
  vertex stage moves vertices a `CullMargin`.
- **Mirror a material argument's WGSL struct exactly**, padding included,
  with `_` fields, and never hold a pointer in it.
- **Sample an entry's image with `canvas_sample`**, in uniform control flow,
  before any discard, and leave every `canvas_…` name to canvas.
- **Set a material's parameters before canvas draws the frame**: in a task
  hung off `FrameEvent`, or one ordered before it.
- **Release what you make, once each**: materials and font sets.
- **Unload a level's art by its folder**, `Unload("level1/")`, and put a
  changed mesh file again before unloading its path.
- **Read inspections in tasks joined into `InspectedEvent`'s out
  dependency**, and copy what you keep.
- **Mount the asset folder from disk above the embedded one** to reload in
  development.
- **In tests, name every report you expect**, and keep each shader's binding
  declaration beside it, generated with canvas's assets mounted.
