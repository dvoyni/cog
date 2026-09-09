# canvas

`github.com/cog-engine/canvas` records layered 2D sprites, text, primitives, and
custom triangles, then translates them into `gfx` draws at the end of each
simulation update.

[`docs/specs/materials.md`](docs/specs/materials.md) is the design record for
the **canvas material contract** — what a custom material may replace and what
it must match exactly, canvas's group and binding convention, what canvas
publishes as includable WGSL, how draws merge into batches, and how a material
reaches `ui` visuals, `Text` and the shape helpers. **It specifies work that is
not implemented.** This README describes the API as it exists today; where the
two disagree, the spec is the plan and the README is the truth. The spec's
*Required canvas changes* section lists the gap.

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

During `Start`, canvas executes `storage.SetMountCmd` to mount its embedded
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

- `Clear(Layer, m.Color)` to fill one layer's target, before anything that layer
  draws. It is positioned rather than frame-global so that whatever renders
  below canvas - a scene camera at a lower order - survives it, and it is per
  layer so a frame can clear a texture-targeted layer and the screen both.
- `SetLayerTransform(Layer, m.Rect, AspectMode)` to map layer world coordinates
  into the logical viewport.
- `SetLayerTarget(Layer, gfx.TargetDescr)` to render a layer into a texture
  instead of the screen. See **Render to texture** below.
- `SetClip(m.Rect)` and `RemoveClip()` to control the clip captured by subsequent
  operations.
- `Sprite(Layer, path, SpriteTransform, *gfx.MaterialDescr, ...gfx.ParameterDescr)`.
  A nil material batches the sprite into the built-in instanced sprite
  material. Note that `DefaultMaterial()` currently returns a *different*,
  single-draw material that no built-in path reaches; the spec repoints it.
- `SpriteTexture(Layer, gfx.TextureDescr, SpriteTransform, *gfx.MaterialDescr, ...gfx.ParameterDescr)`
  for the same rectangle sourced from a gfx texture rather than a sprite path.
- `DrawTexture[TVertex](Layer, gfx.TextureDescr, []TVertex, *gfx.MaterialDescr, ...gfx.ParameterDescr)`
  for an arbitrary shape sourcing a gfx texture.
- `FillRect`, `StrokeRect`, and `Line` for colored primitives.
- `Text(Layer, fontPath, text, TextDraw)` for text with multiline and `${path}`
  inline-image support. A backslash escapes a literal `${` or `\`.
- `DrawTriangles[TVertex VertexLayout](...)` for a non-indexed triangle list.
- `Reset()` to discard recorded frame state and `OpCount()` to inspect the
  number of recorded draw operations.

`Layer` controls ascending draw order and is a `gfx.Order`: canvas declares one
gfx pass at that order for every layer that draws or clears, and a contiguous
run of them sharing a target collapses back into a single GPU pass. Another
recorder interleaves with canvas by taking an order between two layer values. `m.Rect` and `m.Vec2` use `float32` logical
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
`DefaultMaterial()`, `DefaultTrianglesMaterial()` and `TextureMaterial()` return
the built-in materials.

A custom material replaces a built-in shader, and the shape it has to match is
specified in [`docs/specs/materials.md`](docs/specs/materials.md) rather than
here — including the one trap that has no compiler behind it: the layer clip is
a test inside `fs_main`, so a hand-written `fs_main` that omits it draws outside
the clip rectangle with no error anywhere.

A shader of your own reaches the built-in key-colour ramp by including it, so a
custom material can wear the exact ramp the built-ins do rather than a re-typed
approximation:

```wgsl
//#include builtin/canvas/keycolor.wgsl
```

That file declares `keyColorRamp`, the `srgbEncode`/`srgbDecode` pair it is
written in, and the three `key*` constants. Canvas mounts it at
`math.MaxInt` priority alongside the built-in shaders, and an `#include`
argument with no `./` prefix is an absolute storage name, so the path above
resolves from any shader in any mount.

## Render To Texture

A canvas layer renders into a gfx texture, and a canvas draw samples one. The
unit of exchange is a plain `gfx.TextureDescr`, so the same handle a layer
rendered into is the one a later layer, another camera, or a `scene.Material`
samples - there is no canvas-owned target type and no name registry.

```go
target, texture := gfxQueue.TemporaryTarget(512, 512, gfx.FormatRGBA8Srgb)
q.SetLayerTarget(0, target)
q.Clear(0, m.Transparent)
q.Text(0, "", "PANEL", canvas.TextDraw{Size: 48})

q.SpriteTexture(1, texture, canvas.SpriteTransform{Position: m.Vec2{X: 20, Y: 20}}, nil)
```

**Canvas mints nothing and names nothing.** The target is the gfx handle the
caller allocated, passed through untouched, because minting a texture takes the
gfx queue and a canvas recorder does not hold it. Take a frame-local target from
`gfx.OpQueue.TemporaryTarget`, which hands back both the target and the texture,
or a durable one from `gfx.ResourceQueue.AllocateRenderTarget` when the contents
must outlive the frame - a panel baked once and sampled for many frames after.

A layer with a target **measures against the target's size**, not the viewport:
`SetLayerTransform`, text rasterization and the clip-space conversion all use
the texture's dimensions. A texture is its own framebuffer, so there is no
second framebuffer scale on top of that.

Passes still merge. A contiguous run of layers naming one target collapses into
a single GPU pass; a target change ends the run, and each run clears its own
depth at the bottom and discards it at the top, because `DepthAuto` pools one
depth texture per target size.

**Barriers are gfx's.** It computes the frame's write-then-read pairs and emits
the transitions, in both directions, so canvas carries no barrier code and
neither does a caller.

Two things a caller has to get right:

- **Do not draw a render target through the sprite or triangle material.** Both
  run the key-colour ramp, which rewrites any texel whose red and blue agree
  within 0.2 in sRGB and whose green is below 0.2 to grey at its own red
  intensity, with no key colour that switches it off. That is what makes a
  sprite sheet wear a player colour, and it silently desaturates every dark,
  low-green pixel of a rendered image. `SpriteTexture` and `DrawTexture` default
  to `TextureMaterial()`, which samples and returns; keep that unless you are
  supplying a shader of your own.
- **Allocate the target `FormatRGBA8Srgb`.** The atlas is sRGB and the engine
  blends linear, so that format is what everything else canvas draws matches. It
  is also the only format that works today: gfx keys every pipeline to the frame
  buffer's colour format regardless of the pass target, which is right while
  every renderable texture is allocated in it and wrong the moment one is not.

## Inspecting A Recording

A recorder can read back what it recorded, so its tests assert on operations
instead of rendered pixels:

- `Ops(dst []Op) []Op` appends every operation in flush order (layers ascending,
  then recording order within a layer).
- `Op` reports `Kind` (`OpSprite`, `OpText`, `OpTriangles`),
  `Layer`, the snapshotted `Clip`/`HasClip`, the sprite `Path`/`Transform`, the
  `Texture` the op samples, the text `FontPath`/`Text`/`Draw`, recorded
  `Params`, and `Vertices` for triangle lists recorded with the built-in
  `Vertex` type. `Op.Param` and `Op.ColorParam` look a parameter up by name.
- `LayerWindow(Layer)` reports a layer's window and aspect mode,
  `LayerTarget(Layer)` where it draws, and `LayerClear(Layer)` the color passed
  to `Clear` for it.

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
