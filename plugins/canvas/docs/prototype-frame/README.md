# PROTOTYPE: the calls a user makes to draw one 2D frame

Throwaway, for [canvas prototype: the calls a user makes to draw one 2D frame](https://github.com/dvoyni/cog/issues/871). It is not canvas. It stays in canvas's docs as the reference for the calls' shapes while canvas is built. **Delete it once canvas's implementation is finished:** [canvas: README, guide and AGENTS.md](https://github.com/dvoyni/cog/issues/921), the last implementation ticket, removes it, and the prototype ticket's resolution keeps the verdict and the spellings.

```
cd plugins/canvas/docs/prototype-frame
GOWORK=off go run .          # split screen: two world cameras, a minimap, a UI
GOWORK=off go run . pixel    # one pixel-perfect camera at 320×180
```

## What to read

The game is the part that was judged. Each file is a task or two of a game's plugin:

- `main.go` holds the config, the layers, the game's resource, the frame loop, and `update`, which works out the cameras and converts a click.
- `setup.go` runs once: font sets, icons and colours through `Fonts`' writing side, the 2D materials, and the mesh files put into a memory mount.
- `world.go` sets the cameras and records the ground: the tilemap, a lake mesh, grass in a layout of the game's own, and the hero's per-frame trail.
- `units.go` holds y-sorted units with shadows, outlined labels, a dissolving unit, and the hero's halo.
- `ui.go` holds a sliced panel, a cropped health bar, markup with an icon and a registered colour, typed text, an icon span, Japanese, buttons sized by their layout, a clipped log, and the minimap on a disc mesh.
- `tilemap.go` and `widgets.go` are a toy tilemap and a toy button built on canvas alone.
- `shaders/` holds the dissolve and grass materials, and a sketch of canvas's include, `cog.canvas/canvas.wgsl`.

The rest is stubs:
- `canvas/` is canvas's API. Its bodies only record, and `prototype.go` prints each frame as canvas would draw it: cameras in order, their layers, batches, culls, loads and reports.
- `kernel/`, `render/` and `platform/` are stand-ins for the few types the API names.
- `mx/` stands in for the `m.Mat2x3` the sprites ticket adds to libs/m.
- `meshfile/` stands in for `libs/meshfile`.

The kernel is left out: main calls the tasks in order, handing them `kernel.Read` and `kernel.Write` stand-ins.

## Verdict

The calls read well. The stubs carry every change below.

1. **The spellings stand as the table below gives them.**
2. **A run's zero `Filter` is its font's:** linear for a distance field, nearest for a bitmap font. `Config.DefaultFilter` is sprites' and meshes' alone. The pixel run first sampled every label's field with nearest filtering, which breaks a field.
3. **A struct that is only a call's parameter is named `…Options`. A value an ECS component holds is named for what it draws:** `Sprite`, `Text`, `Shape`, `Mesh`.
   - The mesh file's handle is `MeshFile`, from `MeshFromFile(path)`, drawn by a `canvas.Mesh{File, …}`.
   - Triangles for one frame are drawn with `DrawTriangles(options DrawTrianglesOptions, triangles)`, whose options have no file. The first version passed a mesh value whose file it ignored.
4. **Shapes are literals like every other entry:** `canvas.Shape{Kind: canvas.Fill, Rect: r, Tint: c, Layer: l}`. Constructors answering a shape left its sort key to be set afterwards.
5. **`canvas.Layers(from, to)`** answers a camera's layers holding one range. Several ranges are a literal.
6. **`canvas.Tag("wght")`** answers a variable font's axis, in place of `[4]byte{'w', 'g', 'h', 't'}`.
7. **`FromFile(path, options…)` keeps its variadic options,** at most one set, so the common case reads `FromFile("water.png")`. A second set is reported once.
8. **The glossary's *mark* is renamed *entry*:** one thing recorded into a canvas list for a frame. The include's varying is `row`.
9. **For canvas's guide, two things a game must know:**
   - A tilemap seen by two cameras records the union of their views once. Every camera whose layers hold an entry draws it, so recording per view would draw the shared tiles twice.
   - A game keeps its own cameras to convert a click, since canvas forgets them. A conversion reads this frame's camera against its target as it was last drawn.
10. **Left to canvas on the kernel:**
    - how a task's `canvas.DrawList` reaches it, and the lists' order;
    - `Assets`' locks;
    - when canvas writes `Targets`.

## The spellings

| Ticket | Spelling |
|---|---|
| sprites | `canvas.OrderOf(y)`; `Config.RegroupedLayers`; `m.Translation2x3`, `Rotation2x3`, `Scaling2x3`, `TRS2x3(translation, rotation, scale)`, where `a.Mul(b)` applies `b` first; `DrawSprite`, `DrawSpriteWith`, `DrawSprites`, `DrawSpritesWith`; a `Shape` literal drawn with `DrawShape` and `DrawShapeWith`; the named pivots `PivotCenter`, `PivotTopLeft`, `PivotTop`, `PivotBottom`, `PivotBottomLeft`, `PivotBottomRight` |
| atlas | `FromTexture(texture, options…)`, an image of a render texture |
| 2D material | an `Assets` parameter, as render's `Inventory`, holding `MakeMaterial`, `ReleaseMaterial`, `SetMaterialTexture`, `SetMaterialUniform`, `Preload` and `Unload`; `ArgumentOf[T]()`; `BlendNormal()` and the other modes; `SDFMaterial` and `TextMaterial`; `Outline`, `Halo`, `Glow` and `DropShadow` making a `Profile` |
| fonts | `ElevateFonts(kernel.Write[Fonts], kernel.Read[platform.FS]) WriteFonts`, holding `MakeFontSet`, `DestroyFontSet`, `SetLanguage`, `RegisterIcon` and `RegisterColor`; `Tag`; a `Text` value drawn with `DrawText` and `DrawTextWith`; `Fonts.Layout(text)`, answering `Size`, `LineCount`, `Baseline`, `LineWidth` and `ClusterPosition`; `Fonts.Preload(set, text)` |
| camera | `DrawList.SetCamera(Camera)`; `Camera`'s fields, its target as `Window` or `Texture`; `LayerRange{From, To}` and `Layers(from, to)`; `FitExpand`, `FitInscribe`, `FitOverlap`, `FitStretch`; `ScreenToWorld`, `WorldToScreen` and `VisibleRect` over `Targets`; the include `cog.canvas/canvas.wgsl` |
| meshes | `MeshFromFile(path) MeshFile`; a `Mesh` value drawn with `DrawMesh`, `DrawMeshWith`, `DrawMeshes` and `DrawMeshesWith`; `DrawTriangles` and `DrawTrianglesWith` over `DrawTrianglesOptions` and `Triangles[V]`; `Assets.Unload(path)` for images and mesh files alike; `canvas.Vertex` and its `Position3`; `meshfile.VertexType`, `Layout`, `Encode`, `Decode` and `CalculateBounds` |
