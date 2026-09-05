# canvas

`github.com/cog-engine/canvas` records layered 2D sprites, text, primitives, and
custom triangles, then translates them into `gfx` draws at the end of each
simulation update.

## Plugin

- Name: `canvas.Name` (`"canvas"`)
- Constructor: `canvas.New() *canvas.Plugin`
- Plugin dependencies: `gfx`, `storage`
- Go package dependencies: `app`, `gfx`, `kernel`, `storage`, `x/image`
- Events declared or published: none

```go
cfg := canvas.DefaultConfig()
cfg.AtlasSize = 4096
cfg.LayersPerArray = 2
cfg.MaxAtlasBytes = 256 << 20
```

`Config` is the exported configuration type. `Plugin` implements `Name`,
`Dependencies`, and `Init` for the kernel lifecycle.

`LayersPerArray` must be at least two. Atlas dimensions and the memory budget
must be positive, and one array must fit within `MaxAtlasBytes`.

During `Init`, canvas executes `storage.SetReadFSCmd` to mount its embedded
shaders. Register `storage` before `canvas`. A typical order is `storage`,
`input`, `gfx`, `canvas`, then the system driver.

## Resources

- `*OpQueue`: frame-local recording surface. Canvas consumes and resets it on
  `app.UpdateEvent`.
- `*Lookup`: the single persistent resource holding the sprite atlas, glyph
  atlas, font store, and cached sprite metadata. It also owns deferred unloads
  and the framebuffer-scale font invalidation. Query and mutate it only through a
  scoped `LookupAccess`.

Gameplay normally writes only `*OpQueue`. Sizing, measurement, and unloading go
through `*Lookup` (plus `storage.FileSystem`) via a `LookupAccess`; the flush handler
also writes `*Lookup` to resolve lazy sprites and apply deferred unloads.

## Drawing API

Bind `access.GetWrite[*canvas.OpQueue]()` in the recording subscription's `Lock`
and call:

- `Clear(Layer, m.Color)` to fill the screen at one layer, before anything that
  layer draws. It is positioned rather than frame-global so that whatever
  renders below canvas - a scene camera at a lower order - survives it.
- `SetLayerTransform(Layer, m.Rect, AspectMode)` to map layer world coordinates
  into the logical viewport.
- `SetClip(m.Rect)` and `RemoveClip()` to control the clip captured by subsequent
  operations.
- `Sprite(Layer, path, SpriteTransform, *gfx.MaterialDescr, ...gfx.ParameterDescr)`.
  A nil material uses the built-in sprite material.
- `FillRect`, `StrokeRect`, and `Line` for colored primitives.
- `Text(Layer, fontPath, text, TextDraw)` for text with multiline and `${path}`
  inline-image support. A backslash escapes a literal `${` or `\`.
- `DrawTriangles[TVertex VertexLayout](...)` for a non-indexed triangle list.
- `Reset()` to discard recorded frame state and `OpCount()` to inspect the
  number of recorded draw operations.

`Layer` controls ascending draw order and is a `gfx.Order`: canvas declares one
gfx pass per non-empty layer at that order, and a contiguous run of them
collapses back into a single GPU pass. Another recorder interleaves with canvas
by taking an order between two layer values. `m.Rect` and `m.Vec2` use `float32` logical
coordinates. `AspectMode` is `AspectInscribe`, `AspectOverlap`, or
`AspectStretch`.

`SpriteTransform` exposes `Position`, `Size`, `Scale`, `Rotation`, `Origin`,
`Frame`, `FlipX`, `FlipY`, `TileX`, `TileY`, and `Filter`. An unset size uses
the texture's natural dimensions; setting one dimension preserves aspect.
`SpriteFrame{Left, Top, Right, Bottom}` selects a pixel sub-rectangle.

`TextDraw` contains `Position`, `Size`, `Color`, `Align`, `WordWrapping`, and
`WrapWidth`. Its `TextAlign` values are `AlignLeft`, `AlignCenter`, and
`AlignRight`. Wrapping uses `WrapWidth` only when `WordWrapping` is true.

`Vertex` is the built-in position/color/UV vertex and implements
`VertexLayout`. Custom pointer-free vertex structs implement
`VertexLayout() []gfx.VertexAttr`. `SpriteInstance` is the public 96-byte
instance record matching the built-in sprite-batch shader.

`TextureSlot` (`"canvasTexture"`) and `SamplerSlot` (`"canvasSampler"`) are the
reserved shader parameter names for textured custom triangles.
`DefaultMaterial()` and `DefaultTrianglesMaterial()` return the built-in
materials.

## Inspecting A Recording

A recorder can read back what it recorded, so its tests assert on operations
instead of rendered pixels:

- `Ops(dst []Op) []Op` appends every operation in flush order (layers ascending,
  then recording order within a layer).
- `Op` reports `Kind` (`OpSprite`, `OpText`, `OpTriangles`),
  `Layer`, the snapshotted `Clip`/`HasClip`, the sprite `Path`/`Transform`, the
  text `FontPath`/`Text`/`Draw`, recorded `Params`, and `Vertices` for triangle
  lists recorded with the built-in `Vertex` type. `Op.Param` and `Op.ColorParam`
  look a parameter up by name.
- `LayerWindow(Layer)` reports a layer's window and aspect mode, and
  `ClearColor()` the color passed to `Clear`.

## Coordinate Helpers

- `LayerTransform(window, aspect, viewport)` returns scale and offset for the
  layer mapping `world*scale + offset`.
- `WorldToScreen(...)` applies that mapping.
- `ScreenToWorld(...)` applies its inverse.

## Lookup API

Sizing, text measurement, and resource lifecycle go through `*canvas.Lookup`,
the single persistent resource that owns the sprite atlas, glyph atlas, and font
store. Because a resource must not retain filesystem or GPU handles past its lock
scope, callers acquire a handler-scoped facade instead:

```go
la := canvas.NewLookupAccess(kernel, lookup, filesystem)
```

Bind `access.GetWrite[*canvas.Lookup]()` and `access.GetRead[storage.FileSystem]()`
in the handler's `Lock`, then use:

| Method | Result | Notes |
| --- | --- | --- |
| `SpriteSize(path) m.Vec2` | intrinsic pixel size | Reads the image header; no GPU upload. A resident atlas entry's decoded size wins. |
| `FontMetrics(path, size int) FontMetrics` | `Ascent, Descent, LineHeight, XHeight, CapHeight` | Logical pixels. |
| `MeasureTextSize(path, size int, text string) m.Vec2` | measured size | Multi-line; width is the widest line. `${path}` tokens size to cap height. |
| `MeasureWrappedTextSize(path, size int, text string, width float32) m.Vec2` | wrapped size | Wraps words at width and splits oversized words between runes. |
| `UnloadSprite(path)` | — | Deferred to the next frame boundary; absent is a no-op. |
| `UnloadFont(path)` | — | Drops the baked faces and parsed source for the path; glyph pages are freed only by the whole-atlas resize invalidation. |

Failures (invalid, missing, or unreadable resources) report once through
`kernel.ReportError` and return zero values; the report clears on the next
successful load or unload. Paths are normalized (`\` to `/`, `path.Clean`) and
empty, absolute, NUL-bearing, or root-escaping paths are rejected. Font sizes are
integer cache keys; returned dimensions and metrics are `float32` logical pixels.
An empty string measures as one line's height. Unloaded sprite regions are
reclaimed for reuse, and a fully empty sprite atlas array is released; glyph
pages are freed only by the whole-glyph-atlas resize invalidation.

## Event Subscribed

`UpdateEventHandler` subscribes to `app.UpdateEvent`. It writes the canvas
`*OpQueue` and `*Lookup`, reads `gfx.Viewport` and `storage.FileSystem`, and writes
`gfx.OpQueue` and `gfx.ResourceQueue`. It is ordered `Last()` but explicitly before
`gfx.UpdateEventHandler`, so gameplay records first, canvas emits graphics
draws second, and gfx presents last.
