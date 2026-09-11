# canvas

`github.com/cog-engine/canvas` records layered 2D sprites, text, primitives, and
custom triangles, then translates them into `gfx` draws at the end of each
simulation update.

[`docs/specs/materials.md`](docs/specs/materials.md) is the specification of the
**canvas material contract** — what a custom material may replace and what it
must match exactly, canvas's group and binding convention, what canvas publishes
as includable WGSL, how draws merge into batches, and how a material reaches
`ui` visuals, `Text` and the shape helpers. It is implemented; the spec carries
the reasoning behind each rule, and this README is the API surface. Go there
before proposing a change to any of it.

## Plugin

- Name: `canvas.Name` (`"canvas"`)
- Constructor: `canvas.New() *canvas.Plugin`
- Plugin dependencies: `gfx`, `storage`
- Go package dependencies: `app`, `gfx`, `kernel`, `mcp`, `storage`, `x/image`
- Implements: `mcp.Provider`, `kernel.PluginStopper`
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
- `SetLayerMaterial(Layer, MaterialSet)` to put one material set over everything
  a layer draws that named no material of its own. See **Materials** below.
- `Sprite(Layer, path, SpriteTransform, *gfx.MaterialDescr, ...gfx.ParameterDescr)`.
  A nil material batches the sprite into the built-in instanced sprite material,
  which is what `DefaultMaterial()` returns; naming a material batches too.
- `SpriteTexture(Layer, gfx.TextureDescr, SpriteTransform, *gfx.MaterialDescr, ...gfx.ParameterDescr)`
  for the same rectangle sourced from a gfx texture rather than a sprite path.
- `DrawTexture[TVertex](Layer, gfx.TextureDescr, []TVertex, *gfx.MaterialDescr, ...gfx.ParameterDescr)`
  for an arbitrary shape sourcing a gfx texture.
- `FillRect(Layer, m.Rect, ShapeDraw)`, `StrokeRect(Layer, m.Rect, ShapeDraw)`
  and `Line(Layer, start, end m.Vec2, ShapeDraw)` for primitives.
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

`TextDraw` contains `Position`, `Size`, `Color`, `Align`, `WordWrapping`,
`WrapWidth`, `Material` and `Params`. Its `TextAlign` values are `AlignLeft`,
`AlignCenter`, and `AlignRight`. Wrapping uses `WrapWidth` only when
`WordWrapping` is true.

`ShapeDraw` is what `FillRect`, `StrokeRect` and `Line` take: `Color`,
`Thickness`, `Material` and `Params`. `FillRect` ignores `Thickness`, exactly as
`TextDraw` ignores `WrapWidth` without `WordWrapping`. **A zero `Color` is
opaque white**, matching `ui`'s default tint and canvas's habit of reading a
zero scale as 1 — without it a `ShapeDraw` naming only a material would draw
nothing at all.

The two groups of entry points are deliberately not aligned into one shape.
`Sprite`, `SpriteTexture`, `DrawTriangles` and `DrawTexture` take a transform or
a path and then trailing `material, params...`; `Text` and the shape helpers
take a struct describing the whole draw and carry them as fields. Naming a
material is rare enough that the argument list is the wrong place to pay for it.

`Vertex` is the built-in position/color/UV vertex and implements
`VertexLayout`. Custom pointer-free vertex structs implement
`VertexLayout() []gfx.VertexAttr`. `SpriteInstance` is the public 96-byte
instance record matching the built-in sprite shader.

## Materials

Every surface canvas draws can carry a custom shader, and naming one costs no
draw: the material joins the batch key by fingerprint, so two sprites sharing a
material merge exactly as two sprites sharing none do, and
`Sprite(..., DefaultMaterial())` batches identically to `Sprite(..., nil)`.

There are three **defaults**, one per **family** — what a draw that names no
material gets — and a material belongs to exactly one family:

| constructor | family | entry point | what it does |
|---|---|---|---|
| `DefaultMaterial()` | sprite | `builtin/canvas/sprite.wgsl` | the instanced atlas draw every sprite, glyph, inline icon and fill goes through |
| `DefaultTrianglesMaterial()` | triangles | `builtin/canvas/triangles.wgsl` | samples `canvasTexture` through the key-colour ramp, times vertex colour |
| `TextureMaterial()` | triangles | `builtin/canvas/texture.wgsl` | samples and returns; no ramp |

Canvas also publishes one material that is **not** a default — the
[halo](#the-halo) — which an app names deliberately over a scope or does not get
at all.

### Reaching draws that name none

`MaterialSet` is what a **scope** names — one material per family plus one
shared parameter list, because a layer is never one family:

```go
type MaterialSet struct {
    Sprite    *gfx.MaterialDescr
    Triangles *gfx.MaterialDescr
    Texture   *gfx.MaterialDescr
    Params    []gfx.ParameterDescr
}
```

A nil slot keeps its built-in, so a set is an override rather than a whole-cloth
requirement, and one parameter list serves all three slots because gfx drops a
name the bound shader never declared.

`SetLayerMaterial(Layer, MaterialSet)` puts one over a whole layer. Like `Clear`,
`SetLayerTarget` and `SetLayerTransform` it is **key/value applied at flush, not
positional**, so a caller running after every screen has recorded still reaches
what they drew — which is the caller this exists for. The last call in a tick
wins, with the values the set held at that moment, and it is cleared with the
layer's other per-frame state.

Canvas resolves a draw's material as **draw, then layer, then built-in**. A draw
that names its own material takes **none** of the scope's parameters: a scope's
material and its parameters are one unit, and a draw naming a material has said
what it wants.

`ui` inherits a set down the element tree — `Frame.SetMaterial` seeds the roots
and `Element.Material` replaces it for a subtree — so one modifier on a menu root
reaches every visual beneath it. See `ui/README.md`.

### The halo

A **halo** is a soft outward fade in a named colour, so a mark reads against
whatever art it overlaps. `HaloMaterialSet` puts one over a whole layer, and it
covers the sprite family entire — `Sprite`, `Text` glyphs, inline icons,
`FillRect`, `Line`, `StrokeRect` — because all of them are sprite instances in
one instanced atlas draw.

```go
type HaloProfile struct {
    Reach    float32 // how far the band extends, in layer-local world units
    Plateau  float32 // the fraction of the band that holds flat before the falloff
    Exponent float32 // the shape of that falloff
}

func DefaultHaloProfile() HaloProfile                  // {Reach: 6, Plateau: 0.18, Exponent: 1}
func HaloMaterialSet(profile HaloProfile) MaterialSet
```

**The material paints the band and no mark at all**, so the caller records the
same marks twice: once on a halo layer under this set, once on the ink layer
above it under no material.

```go
write.SetLayerMaterial(layerHalo, canvas.HaloMaterialSet(canvas.DefaultHaloProfile()))
drawCluster(write, layerHalo, haloColor)
drawCluster(write, layerInk, inkColor)
```

That is what makes overlap free: no band can reach anybody's ink, because no ink
exists yet when the bands are drawn, and what is left is ordinary painter's
order. It costs twice the instances, one extra batch, and one extra pass that
merges away. Two layers is the idiom rather than a requirement, but is what to
write — a draw added to the ink layer later lands in the ink run rather than
becoming a coloured ghost.

**Colour and strength ride `tint`, per sprite, for nothing.** `ShapeDraw.Color`,
`TextDraw.Color` and a sprite's `tint` parameter all land in the instance
record's frozen `Tint` field, so each mark's band is its own colour and `tint.a`
is the band's peak alpha — fading a cluster fades its halo with it.

**Dedicate the layer.** `Triangles` and `Texture` stay nil, so a `DrawTriangles`
or `DrawTexture` recorded on the halo layer paints as itself rather than as a
band.

**The profile is per batch, and two reaches are two scopes.** The three knobs
ride the set's `Params`; they cannot be named at a draw, because a draw's valued
parameter becomes a per-sprite storage array while a scope's is shared. The
material carries the defaults as its own parameters, so a hand-assembled
`MaterialSet{Sprite: ...}` renders at reach 6 rather than rendering nothing. And
`HaloProfile` is complete rather than a struct of optional fields: `Plateau: 0`
is a legitimate value — no plateau, pure falloff — that a sentinel would read as
`0.18`.

There is deliberately **no `HaloMaterial()`** beside `DefaultMaterial()`, despite
the symmetry: a draw naming its own material takes none of its scope's
parameters, so naming the halo at a draw would render at the material's own
defaults and silently ignore every profile above it. A scope is the only way in.
Its WGSL is not published either — `keycolor.wgsl` is, because a custom triangles
material *must* reproduce the ramp or key every texel against black, and nothing
has to reproduce a halo.

### Parameters and their frequency

**The call site declares the parameter's frequency.**

| named on | frequency | where it lands |
|---|---|---|
| the **material** | one value per batch | a member of the uniform block, which a custom shader may append to |
| a **sprite draw** | one value per sprite | one storage buffer per parameter name, at group 2, indexed by `@builtin(instance_index)` |
| a **triangles draw** | per material | the uniform block; two values are two draws |

Draw-parameter *values* are not in the sprite key, so two sprites differing only
in one still merge. Their *names* are: every sprite contributes exactly one
element to every array, so a sprite carrying a name another lacks splits the
batch rather than zero-filling the gap.

A triangle batch is concatenated vertices with **no instance index**, so the
sprite path's arrays have nothing to hang on. A parameter named at a
`DrawTriangles` call is therefore per material, and two values are two draws.
That is a rule, not a shortcoming of the key.

Naming one value at two frequencies — a name the shader declares as a uniform
member, passed at a sprite draw call — is an authoring error, and gfx reports it
as `ErrParameterKindMismatch` and drops the draw rather than binding a buffer
descriptor into a uniform slot.

Resolution is **first wins**, front to back: canvas's own parameters, then the
draw's, then the scope's, then the material's. So canvas's viewport, transform,
clip and texture bindings are guaranteed against anything a caller passes, and a
caller who wants their own texture on their own shape uses `DrawTriangles`.

### Reserved names

`TextureSlot` (`"canvasTexture"`), `SamplerSlot` (`"canvasSampler"`),
`TintSlot` (`"tint"`) and `KeyColorSlot` (`"keyColor"`) are the names canvas
consumes itself and **never forwards to a material**. The first two are canvas's
own bindings; the last two are fields of the sprite instance record, so a custom
sprite shader reads them from the shared `VertexOut` rather than from a uniform
and may not reclaim either name. Text already spends both: `TextDraw.Color`
becomes the instance tint and glyphs carry the default key colour.

### Writing one: the seven published sources

An app writes a canvas material against published WGSL rather than copying the
contract. Each is named by an exported constant and included by **absolute
storage name** — an `#include` argument with no `./` prefix is one, and canvas
mounts these at `math.MaxInt` priority, so they resolve from any shader in any
mount.

| source | constant | declares |
|---|---|---|
| `uniforms.wgsl` | `UniformsPath` | `struct CanvasUniforms` (`canvasViewport`, `canvasLayer`, `canvasClip`) and `@group(0) @binding(0) var<uniform> u` |
| `clip.wgsl` | `ClipPath` | `fn canvasClipped(canvasPosition: vec2<f32>) -> bool` |
| `spritebindings.wgsl` | `SpriteBindingsPath` | group 1 `canvasSampler` + `canvasTexture: texture_2d_array<f32>`; group 2 `instances`; `struct SpriteInstance`; `struct Instances`; `struct VertexOut` |
| `spritevertex.wgsl` | `SpriteVertexPath` | includes `spritebindings.wgsl`; declares `vs_main` |
| `trianglesbindings.wgsl` | `TrianglesBindingsPath` | group 1 `canvasSampler` + `canvasTexture: texture_2d<f32>`; `struct VertexOut` |
| `trianglesvertex.wgsl` | `TrianglesVertexPath` | includes `trianglesbindings.wgsl`; declares `vs_main` |
| `keycolor.wgsl` | `KeyColorPath` | the sRGB transfer functions, the three `key*` constants, and `keyColorRamp` |

Each source's header states exactly what it declares, because the rule you must
obey is **do not declare anything a source you included declares** — and a
duplicated binding is not a compile error but a silent whole-frame loss.

`sprite.wgsl`, `triangles.wgsl` and `texture.wgsl` stay **entry points**: roots a
material names, not sources you include.

A whole fade sprite material is six lines of declaration and three includes:

```wgsl
struct CanvasUniforms {
    canvasViewport: vec4<f32>,
    canvasLayer: mat4x4<f32>,
    canvasClip: vec4<f32>,
    fade: f32,
};
@group(0) @binding(0) var<uniform> u: CanvasUniforms;

//#include builtin/canvas/spritevertex.wgsl
//#include builtin/canvas/clip.wgsl
//#include builtin/canvas/keycolor.wgsl

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
    if canvasClipped(in.canvasPosition) { discard; }
    let sampled = keyColorRamp(
        textureSample(canvasTexture, canvasSampler, in.uv, in.atlasLayer),
        in.keyColor.rgb,
    );
    return sampled * in.tint * vec4<f32>(1.0, 1.0, 1.0, u.fade);
}
```

That material **extends** the uniform block, which is how a custom material
declares its own per-batch parameters — so it hand-writes the block and does not
include `uniforms.wgsl`. It cannot: include-once by resolved path means the
struct would already be declared, and WGSL has no way to add a member to a struct
declared elsewhere. That is exactly why the block is its own source.

**⚠ The clip test is offered, not required, and its omission is silent.**
`SetClip`/`RemoveClip` are implemented entirely as a test inside `fs_main` —
there is no scissor rect anywhere in gfx — so a hand-written `fs_main` that does
not call `canvasClipped` draws outside the clip rectangle with **no error
anywhere**. The three built-in entry points call it and are the worked example.

### What a custom material must match exactly

- **Group and binding numbers, and the resource kind at each.** Groups are
  numbered by what a binding *is*, not by how often it changes: 0 the uniform
  block, 1 the texture a draw samples, 2 per-sprite storage. Group 3 is claimed
  by nothing. An app's own per-instance parameter arrays go at group 2, binding 1
  and up — seven of them fit beside `instances`.
- **The `SpriteInstance` record** — struct name, member names, order and size: six
  `vec4<f32>` at 16-byte offsets, 96 bytes, no padding. It is hand-mirrored by
  `canvas.SpriteInstance` and uploaded by direct reinterpretation, so a
  divergence is a silent misread rather than a compile error.
- **The vertex input** `@location(0) quad: vec2<f32>` and the
  `@builtin(instance_index)` read that selects the record.
- **The reserved sampler and texture names.**

Free to change: both entry-point bodies entirely, appended members on the uniform
block, and additional per-instance parameter arrays at group 2.

**Overriding a published source is a feature.** An included path resolves through
the full mount overlay, so an app that mounts its own
`builtin/canvas/keycolor.wgsl` at higher priority replaces that one source inside
canvas's own module and keeps the rest.


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
  `Texture` the op samples with `HasTexture`, the text `FontPath`/`Text`/`Draw`,
  recorded `Params`, the `Material` the op named with `HasMaterial`, and
  `Vertices` plus `VertexBytes` for triangle lists — `Vertices` only for a list
  recorded with the built-in `Vertex` type, `VertexBytes` whatever the layout.
  `Op.Param` and `Op.ColorParam` look a parameter up by name. `HasMaterial` says
  whether the op named a material of *its own*, not what it will draw with: a
  draw that names none resolves to the layer's set and then to the built-in,
  both at flush.

- `LayerWindow(Layer)` reports a layer's window and aspect mode,
  `LayerTarget(Layer)` where it draws, and `LayerClear(Layer)` the color passed
  to `Clear` for it.

## Offered To An Agent

canvas implements `mcp.Provider` and offers one capability, `draws`, rendered
as the tool `canvas_draws`: one tick's recorded operations in flush order, with
each layer's world window, aspect mode, target and clear. It answers *nothing
is on screen; was it even recorded, and on which layer* — and it answers it
about the app's drawing **and ui's**, because ui records into this queue during
its own processing, one phase earlier. That is what makes `canvas_draws` and
`ui_layout` complementary rather than redundant: one says what ui intended, the
other what it emitted.

It is `mcp.Func`, because a snapshot arms and waits, and `mcp.ReadOnly()`,
which in cog's reading means the capability does not change the game — with the
one asterisk that under pause it performs exactly one step, or joins one
another arm already raised, and says so in the response. That is stated in the
description rather than expressed in the annotation, and it cannot be
otherwise: the canvas queue is *empty* between ticks rather than stale, so
producing a snapshot without running a tick is not a thing that exists.

The snapshot is **serialized inside the tick**, from a subscriber ordered
`Last()` and `Before[UpdateEventHandler]()` — after ui and the app have
recorded, before the flush's deferred reset. Nothing that outlives the tick
aliases the queue: `Ops` hands out slices that die at the next reset, which is
why the snapshot renders each op into owned values as it walks the queue rather
than calling it. The JSON marshalling and any file write happen on the
capability's own goroutine.

Every index in the response is a **source index** — a position in canvas flush
order, never a position in the emitted array — so an index stays the address of
what it named when a filter is on. Triangle vertices are summarised as a count
and a bounding box; naming an op's index in `vertices` returns that op's list in
full. `fromLayer`/`toLayer` and `kinds` cut a busy frame down and say how much
they dropped, and `path` is optional and writes the JSON to a file instead of
returning it inline.

Textures, parameters and materials are rendered through the view types `gfx`
declares, so one value reaches an agent in one shape whichever tool showed it,
and the three coordinate sizes come from `gfx.SnapshotView` — as does `tick`,
the number of the tick this snapshot describes, which is how an agent confirms
that this and `ui_layout` describe one moment rather than two. The full contract
is in [docs/specs/mcp.md](docs/specs/mcp.md), and the capability that document
specifies is implemented.

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

`DrawsArmUpdateEventHandler` and `DrawsUpdateEventHandler` are the two halves
of the agent snapshot and are inert when none is armed. The first is ordered
`First()` and declares no resources: it admits a waiting request to the tick
that has just begun, which is what makes "a tick that *began* after the
request" decidable. The second reads `*OpQueue` from `Last()`, ordered
`Before[UpdateEventHandler]()`, and renders the frame between ui's recording
and the flush's reset.
