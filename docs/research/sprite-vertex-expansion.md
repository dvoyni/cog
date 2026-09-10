# A sprite material's vertex stage may draw outside the sprite's rect, and its fragment stage should read the record rather than be handed the frame

Research for [#189](https://github.com/dvoyni/cog/issues/189), under the map
[canvas: a reusable halo behind any sprite, glyph or fill](https://github.com/dvoyni/cog/issues/188).

Primary sources: `canvas/docs/specs/materials.md` (the authority), the seven
published WGSL sources under `canvas/builtin/canvas/`, the canvas Go flush path,
the resolved tickets of [#150](https://github.com/dvoyni/cog/issues/150), and
`lens/lens.go` in feuds-26. Every claim below is cited to the source that owns
it. Two claims were verified by running the real front end (`naga.Parse` +
`wgsl.Lower`) over a hand-written material; that check is in
[Appendix: the check that was run](#appendix-the-check-that-was-run).

---

## Answer in three lines

1. **Yes, expansion is permitted.** The frozen thing is the vertex *input
   declaration*, not what the body does with it, and `vs_main` is free "entirely"
   (`canvas/docs/specs/materials.md:175-176`, `:182`). Nothing in canvas — clip,
   batcher, flush, `SetLayerTransform`, culling — reads a sprite's on-screen
   extent for any purpose. There is no culling of any kind anywhere in canvas.
2. **The frame reaches the fragment stage without any contract change**, by a
   fourth route the ticket did not list: include `spritebindings.wgsl` alone,
   decline `spritevertex.wgsl`, and declare an inter-stage struct **under the
   material's own name** beside the published `VertexOut`. `VertexOut` is not
   frozen and its name is not part of the contract; nothing in Go names it.
3. **Carry the instance index, not the frame.** Every reflected binding is bound
   visible to both stages (`gfx/limits.go:9-10`, `wgpu/gfxbackend.go:475`,
   `:481`), so `fs_main` can read `instances.data[i]` directly. One flat `u32`
   buys the whole record, not just `frame`.

---

## 1. Does anything in canvas assume extent == `s.transform0.zw`?

**No.** `transform0.zw` is consumed in exactly one place on the instanced path —
the vertex shader's scale of the unit quad — and never on the CPU for any
rejection, key or bound.

### The clip test — per fragment, over the geometry actually rasterized

`canvas/builtin/canvas/clip.wgsl:22-27` tests an interpolated varying:

```wgsl
fn canvasClipped(canvasPosition: vec2<f32>) -> bool {
    return u.canvasViewport.z > 0.5 && (
        canvasPosition.x < u.canvasClip.x || canvasPosition.y < u.canvasClip.y ||
        canvasPosition.x > u.canvasClip.z || canvasPosition.y > u.canvasClip.w
    );
}
```

`canvasPosition` is `@location(0)` on `VertexOut`, written from the sprite's
*local* (pre-layer) position at `canvas/builtin/canvas/spritevertex.wgsl:31`
(`out.canvasPosition = local`, where `local = s.transform0.xy + rotated`,
`:26`), and consumed in `fs_main` at `canvas/builtin/canvas/sprite.wgsl:16`.

So the clip follows the geometry, whatever the geometry is. **An expanded quad
is clipped correctly at the clip rectangle's edge with no change** — provided
the expanding `vs_main` writes `canvasPosition` from its *expanded* local
position, which is structurally what the built-in already does. That is a
requirement on the material, not a limitation of the contract.

There is no CPU-side intersection of the clip rect with a sprite's bounds. The
only CPU clip logic is a degenerate-rectangle early-out, four copies of the same
two lines, none of which look at the sprite: `canvas/batch.go:178`,
`canvas/plugin.go:203`, `:387`, `:468`. The clip travels to the uniform
unmodified (`canvas/batch.go:154`).

`canvas/builtin/canvas/clip.wgsl:10-15` states the mechanism outright — "there
is no scissor rect anywhere in gfx". A repo-wide search for `scissor` returns
that comment and its echo at `canvas/README.md:270`, and nothing else.

### The batcher — the key has no geometry in it

`canvas/batch.go:79-92`, `spriteBatch.keyMatches`, is the whole key:

```go
if b.textureID != texture.ID() || b.layer != layer || b.clip != clip ||
    b.hasClip != hasClip || b.filter != filter ||
    b.fingerprint != shading.fingerprint || b.sharedKey != shading.sharedKey ||
    len(b.arrayNames) != len(shading.arrays) {
    return false
}
```

plus per-instance array name/size equality (`:86-90`). Texture ID, layer matrix,
clip rect, `hasClip`, sampler filter, material fingerprint, shared-parameter
fingerprint, array names and sizes. **No size, no position, no bounds.** Two
sprites of wildly different extents merge into one instanced draw today; an
expanded one merges the same way. `trianglesBatch.keyMatches`
(`canvas/batch.go:234-237`) is the same shape and equally bounds-free.

This matches the specification's own statement of the key
(`canvas/docs/specs/materials.md:617-643`), which lists `textureID`, the layer
`m.Mat4`, `clip`/`hasClip`, `filter`, the material fingerprint and the ordered
draw-parameter names — and nothing spatial.

### The flush path — a raw reinterpretation, no per-instance inspection

`canvas/batch.go:136-166`. The instances go out as
`gfx.BufferWithBytes(spriteInstanceBytes(b.instances), true)`, and
`spriteInstanceBytes` (`canvas/shader.go:136-141`) is an `unsafe.Slice`
reinterpretation of the Go slice — no filtering, no compaction, no inspection.
`gfxWrite.DrawInstanced(quad, *b.material, len(b.instances), b.params...)` is
the draw; `gfx/opqueue.go:154-180` records an instance count and nothing about
geometry.

The frame driver `flushFrame` (`canvas/plugin.go:98-163`) sorts layers by layer
ID alone (`:126`) and replays ops in recording order. No reordering by bounds, no
rejection.

### `SetLayerTransform`'s aspect mapping — a scale and a translate

`canvas/opqueue.go:175-180` is pure key/value storage. `resolveLayerTransform`
(`canvas/plugin.go:240-243`) resolves it once per layer:

```go
scale, offset := LayerTransform(value.window, value.aspect, surf.size)
return m.Translation4(offset.X, offset.Y, 0).Mul(m.Scaling4(scale.X, scale.Y, 1))
```

`LayerTransform` (`canvas/types.go:43-61`) reads only the caller's `window` rect
and the surface size: `AspectInscribe` takes `min` of the two axis scales,
`AspectOverlap` takes `max`, `AspectStretch` keeps both, and the offset centres
the window. It is **always an axis-aligned scale plus a translate, never a
rotation and never a clip**. The window is a *scaling* input, not a bound —
nothing is discarded for falling outside it. The matrix reaches the uniform
verbatim (`canvas/batch.go:153`) and is applied at `spritevertex.wgsl:27`.

Two consequences worth stating for a halo:

- **A world-unit margin scales with the layer.** Under `AspectInscribe` and
  `AspectOverlap` the two axis scales are equal, so a circular margin stays
  circular. Under `AspectStretch` they are not, and a margin expressed in world
  units is squashed on one axis along with everything else. That is consistent
  with how everything else on the layer behaves and is not a new hazard.
- The only non-geometric read of the transform's scale is
  `textRasterScale` (`canvas/plugin.go:824-837`), which picks glyph
  rasterization DPI from `layerTransform[0]`/`[5]`. It is not affected by what a
  material does in `vs_main`.

### Culling — there is none

An exhaustive search of `canvas/` for `cull`, `scissor`, `frustum`, `aabb`,
`bounds`, `visible`, `occlus` and dirty-rect logic finds:

- `cull` — zero hits in `canvas/`. In `gfx` only as pipeline face culling
  (`gfx/contract.go:151-157`), and every canvas built-in uses
  `gfx.StateOverlay2D` (`canvas/shader.go:68`, `:81`, `:90`), which is
  `gfx.MaterialState{}` (`gfx/material.go:37`): `CullNone`, no depth compare, no
  depth write. Nothing is back-face or depth rejected.
- `scissor` — no implementation anywhere in the repo.
- `bounds` — only `image.Rectangle` while decoding PNGs (`canvas/atlas.go:351-354`)
  and glyph raster bounds (`canvas/font.go:92-101`). Never at draw time.
- `visible` — only `glyph.visible` (`canvas/font.go:41`, `:123`, checked at
  `canvas/plugin.go:693`), meaning "this glyph rasterized to a non-empty bitmap".
  Not spatial.
- `aabb`, occlusion, dirty rects — no hits.

The complete set of CPU "this draw produces nothing" early-outs are all
degenerate-value guards and none compares an extent against anything:
`canvas/batch.go:175-177` (zero size), `:178-180` (degenerate clip),
`canvas/plugin.go:203-205`, `:387-389`, `:468-470` (degenerate clip), `:382-384`
(zero tile size), `:463-465` (zero size), `:444-449` (unknown texture size),
`:621-625` (frame inset outside source *pixels*), `:315-318` (nine-slice insets
exceed source).

### Where the CPU does compute a rect from size — and why it does not matter here

`spriteSize` (`canvas/plugin.go:597-616`) resolves the size that becomes
`t0.zw` (`canvas/batch.go:182`). Three places build a rect from it:

- `nineSliceParts` (`canvas/plugin.go:314-354`) subdivides `size` into nine
  destination rects. This is geometry *generation* — it emits nine child
  `SpriteTransform`s, each of which then goes through the same instanced path.
  It assumes the nine parts tile the declared rect exactly, which is a property
  of its own arithmetic, not an assumption about what a material draws.
- `drawTiledSprite` (`canvas/plugin.go:379-405`) and `emitTextureQuad`
  (`:461-490`) build four corners on the CPU. These are the **two unbatched
  escapes** the specification names at `canvas/docs/specs/materials.md:703-708`,
  and they bypass the sprite shader entirely — a custom *sprite* vertex stage
  never reaches them.
- `SpriteSize` / `MeasureTextSize` (`canvas/lookup.go:148-151`, `:197-215`) are
  layout queries for callers (`ui`). The draw path never consults them, and they
  do no hit-testing.

**Verdict: nothing in canvas assumes a sprite's on-screen extent equals
`s.transform0.zw`.** The value is a number the shader multiplies by, and the
shader is the only thing that reads it.

---

## 2. Is expanding the quad permitted by the spec as written?

**Yes, and the rule is explicit rather than inferred.**

`canvas/docs/specs/materials.md:148-178`, *What a custom sprite material must
match exactly*, freezes four things. The relevant one is:

> - **The vertex input** `@location(0) quad: vec2<f32>`, and the
>   `@builtin(instance_index)` read that selects the record.

That freezes the **declaration** — the attribute location, its type, and the
fact that the record is selected by instance index. It says nothing about what
the body computes from `quad`. `canvas/README.md:284-285` repeats it in the same
words.

`canvas/docs/specs/materials.md:180-182`, *What a custom sprite material may
change*, opens with:

> - **The `vs_main` and `fs_main` bodies, entirely.**

`canvas/README.md:287-288` puts the same rule as "Free to change: both
entry-point bodies entirely". `canvas/builtin/canvas/spritevertex.wgsl:7-9` says
only that the input and the instance-index read "are part of the contract:
canvas draws a unit quad instanced".

So: the quad the pipeline feeds is fixed at `{0,0},{1,0},{1,1},{0,1}`
(`canvas/shader.go:172-183`, `unitQuadBytes`), and the mapping of that quad onto
a rect is body, which is free "entirely". A `vs_main` that computes
`(quad - origin) * (size + 2*margin) - margin` is inside the contract as
written. Nothing has to change to permit it.

**One coupling the material must break itself.** `spritevertex.wgsl:21` and
`:32` use the *same* `quad` for position and for UV:

```wgsl
let scaled = (quad - origin) * s.transform0.zw;
...
out.uv = mix(s.frame.xy, s.frame.zw, quad);
```

The `mix` is unclamped. A material that grows geometry by pushing its position
parameter outside `[0,1]` and reuses that parameter for the UV `mix` gets UV
extrapolated outside `frame` for free, sampling into the atlas neighbourhood.
Sprites are inserted with 2px padding and edge extrusion
(`canvas/atlas.go:237`), glyphs with 1px and no extrusion (`canvas/font.go:118`),
so there is a small margin before a neighbour bleeds in — and edge extrusion
smears an opaque silhouette outward, which is the opposite of what a halo wants,
as the map already established. An expanding material must therefore compute its
UV from a parameter of its own rather than from the position parameter, and this
is exactly the situation `s.frame` exists to rescue: the material samples where
it likes and reads anything outside `frame` as alpha 0.

The sampler is `AddressClamp`/`AddressClamp` (`canvas/batch.go:157`), so
out-of-range UV clamps to the atlas edge rather than wrapping — a smear, not a
wrap, if the material gets this wrong.

---

## 3. The three routes to the frame rect, and a fourth

First, one fact that reframes all of them.

**Nothing in Go names `VertexOut`.** A repo-wide grep for the identifier in
`.go` files finds it only in comments (`canvas/shader.go:37`,
`canvas/assets.go:48`, `:56`, `canvas/canvas_test.go:771`) and in shader source
strings. What the backend hardcodes is the two entry-point *names*:
`wgpu/gfxbackend.go:618` (`EntryPoint: "vs_main"`) and `:654`
(`EntryPoint: "fs_main"`). The inter-stage struct is purely internal to the WGSL
module — gfx's own reflection test calls its equivalent `VSOut`
(`wgpu/gfxreflect_test.go:22-31`), which is the proof that the name carries no
contract.

And `VertexOut` is **not in the frozen list** — not in
`canvas/docs/specs/materials.md:148-178`, not in `canvas/README.md:273-288`. The
inventory at `canvas/docs/specs/materials.md:322` records that
`spritebindings.wgsl` *declares* it; that is an inventory entry supporting the
do-not-redeclare rule, not a freeze.

### Route (a) — decline `spritebindings.wgsl`, hand-declare everything

**Legal, and the spec's rule is the one the ticket quotes.**
`canvas/docs/specs/materials.md:340`:

> The rule an app must obey is *do not declare anything a source you included
> declares*.

The rule is conditional on inclusion. A material that includes nothing declares
everything itself and redeclares nothing. `lens/lens.go:58-80` is the shipped
precedent: it hand-declares `CanvasUniforms` (extended with three of its own
members), both group 1 bindings, and its own `struct VertexOut` — a full decline,
in production, in the triangles family. `canvas/docs/specs/materials.md:369-372`
treats that as a cost to be removed rather than as an illegality.

**But the cost is real and it lands on the one thing the contract freezes
hardest.** `SpriteInstance` is frozen precisely because it is hand-mirrored and
uploaded by direct reinterpretation
(`canvas/shader.go:113-130`, `canvas/builtin/canvas/spritebindings.wgsl:26-29`),
so a divergence is "a silent misread rather than a compile error". Today there
are two copies — Go and `spritebindings.wgsl` — and exactly one thing keeps them
together: `TestSpriteInstanceMatchesTheShaderRecord`
(`canvas/canvas_test.go:1163-1192`), which lowers **`spriteShaderPath` only**.

A declining material creates a **third** copy, in the app, that no test in cog
or in the app touches. It is the exact failure the frozen-record rule exists to
prevent, reintroduced by the one mechanism the contract offers to avoid it.
Route (a) also re-types the four bindings, which is the "silent whole-frame loss"
`canvas/docs/specs/materials.md:10-17` opens with.

So: legal, and **the worst of the four**. It pays the full retyping cost to buy
one struct member.

### Route (b) — pack into a spare channel of an existing `VertexOut` location

**Not possible.** In WGSL each member of an inter-stage struct occupies its own
`@location`; there is no way to append components to a member without changing
that member's declared type, which means redeclaring the struct — which *is*
route (a)/(d). And the members that have spare components are the two the
material needs intact:

| member | location | components used | spare |
|---|---|---|---|
| `canvasPosition: vec2<f32>` | 0 | 2 of 4 | needed by `canvasClipped` |
| `uv: vec2<f32>` | 1 | 2 of 4 | needed to sample |
| `atlasLayer: i32` | 2 flat | 1 of 4 | needed to sample |
| `tint: vec4<f32>` | 3 | 4 of 4 | none |
| `keyColor: vec4<f32>` | 4 | 4 of 4 | none |

(`canvas/builtin/canvas/spritebindings.wgsl:48-55`.)

The ticket's parenthetical is right and is worth pinning: **`misc` has three
unused floats in the instance record, and they are unreachable by an app.**
`canvas/batch.go:184` writes `misc := m.Vec4{X: float32(entry.layer)}` and
nothing else ever writes `Misc` — the only two mentions of the field in Go are
that construction and the struct declaration (`canvas/shader.go:128`). `y`, `z`
and `w` are hard zeros. Filling them would be a canvas change, because canvas
owns the record; they are not a channel a material can use.

The same is true of `canvasViewport.w`, a free per-batch float that
`canvas/batch.go:152` always writes as zero — free space that only canvas can
fill.

### Route (c) — canvas publishes a wider `VertexOut`, or a second bindings source

**A contract change, and unnecessary.** For the record, what it would take:

- **A wider `VertexOut`** means adding `@location(5) @interpolate(flat) frame:
  vec4<f32>` (or an instance index) to `spritebindings.wgsl:48-55` **and**
  writing it in `spritevertex.wgsl` — because the struct and the built-in
  `vs_main` must agree. That imposes the cost on every sprite material,
  including the built-in `sprite.wgsl`, which does not want it: four more
  interpolated components per vertex on every glyph in the frame.
- **A second bindings source** (`spritebindingswide.wgsl`, say) would be an
  eighth published source declaring a *second* struct beside the first. But it
  cannot re-declare the bindings or `SpriteInstance` — include-once by resolved
  path means a material including both sources would get duplicates, and a
  material including only the wide one would need canvas to keep two parallel
  copies of the frozen record in sync. So the wide source would have to be
  "include `spritebindings.wgsl`, then declare one more struct", which is
  precisely what an app can write for itself in six lines.
- Either way it touches the inventory table
  (`canvas/docs/specs/materials.md:311-341`), the constants in
  `canvas/assets.go:39-65`, `canvas/README.md`, and the "seven published sources"
  count that appears in four places.

The specification's own principle argues against it:
`canvas/docs/specs/materials.md:352-357`, *Bindings and vertex are two sources,
not one*, splits the sources exactly along frozen/free — "The bindings, the
instance record and the vertex input are frozen; `vs_main` and `fs_main` are
free." A published `VertexOut` widened for one effect would move a free thing
into the published set.

### Route (d) — include the bindings, declare your own struct under your own name

**This is the route, it needs no contract change, and it is what
`spritevertex.wgsl` already documents.**

`canvas/builtin/canvas/spritevertex.wgsl:4-5`:

> Include this to replace only `fs_main`; **include `spritebindings.wgsl` alone
> to write a `vs_main` of your own.**

So the published set already anticipates a material that writes its own vertex
stage. Such a material cannot include `spritevertex.wgsl` (two `vs_main`
declarations), so it includes `spritebindings.wgsl` — which hands it the four
frozen bindings, the frozen `SpriteInstance`, `Instances`, and a `VertexOut` it
simply does not use. It then declares **its own** inter-stage struct, with any
name that is not `VertexOut`, carrying as many locations as it wants:

```wgsl
struct CanvasUniforms {
    canvasViewport: vec4<f32>,
    canvasLayer: mat4x4<f32>,
    canvasClip: vec4<f32>,
    haloRadius: f32,
};
@group(0) @binding(0) var<uniform> u: CanvasUniforms;

//#include builtin/canvas/spritebindings.wgsl
//#include builtin/canvas/clip.wgsl

struct HaloOut {
    @builtin(position) position: vec4<f32>,
    @location(0) canvasPosition: vec2<f32>,
    @location(1) uv: vec2<f32>,
    @location(2) @interpolate(flat) atlasLayer: i32,
    @location(3) tint: vec4<f32>,
    @location(4) keyColor: vec4<f32>,
    @location(5) @interpolate(flat) instance: u32,
};

@vertex fn vs_main(@location(0) quad: vec2<f32>, @builtin(instance_index) instance: u32) -> HaloOut { ... }
@fragment fn fs_main(in: HaloOut) -> @location(0) vec4<f32> { ... }
```

Checked against every rule the contract states:

| rule | source | route (d) |
|---|---|---|
| do not redeclare what an included source declares | `materials.md:340` | `HaloOut` is a new name; nothing is redeclared |
| group/binding numbers and kinds | `materials.md:153-156` | come from the include, untouched |
| the `SpriteInstance` record | `materials.md:157-174` | comes from the include; **not re-typed** |
| the vertex input and instance-index read | `materials.md:175-176` | declared exactly, in the material's own `vs_main` |
| reserved sampler/texture names | `materials.md:177-178` | come from the include |
| `vs_main`/`fs_main` bodies free | `materials.md:182` | both replaced, as permitted |
| uniform block extension | `materials.md:183-190` | the block is hand-written, as `uniforms.wgsl:6-18` requires of an extending material |
| the clip test | `materials.md:435-436` | `canvasClipped` takes a bare `vec2<f32>`, so it works against any struct |

Cost: one unused `VertexOut` declaration in the module, and the material's own
inter-stage struct. **Nothing frozen is re-typed.** Verified to parse and lower
through the real front end — see the appendix.

### The better half of route (d): carry the index, not the frame

`gfx/limits.go:9-10` states the rule and `wgpu/gfxbackend.go:475`, `:481` are the
implementation:

```go
Visibility: gputypes.ShaderStageVertex | gputypes.ShaderStageFragment,
```

**Every reflected binding is created visible to both stages.** So `instances` is
readable from `fs_main`. The fragment stage is missing only the *index* —
`@builtin(instance_index)` is a vertex-stage builtin — and one flat `u32` at
`@location(5)` supplies it:

```wgsl
let s = instances.data[in.instance];
// s.frame, s.transform0, s.misc, s.keyColor — all of it, in the fragment stage
```

This is strictly better than carrying `frame`:

- **1 inter-stage component instead of 4.** A halo taking many taps per fragment
  over an expanded quad is the map's stated performance worry; four fewer
  interpolated components per vertex is free money on a glyph-heavy frame.
- **It buys the whole record, not one member.** `transform0.zw` is what converts
  a world-unit halo width into a fraction of the sprite, which the map's finding
  *"A texture-space kernel is wrong for fills"* says the effect needs per
  instance. Carrying `frame` alone would leave that still to solve.
- It scales: any future need for another instance field costs zero more
  locations.

Nothing in cog checks inter-stage limits — `checkWebLimits`
(`gfx/limits.go:11-43`) counts only storage buffers, bind groups and uniform
size — so the WebGPU floor on inter-stage variables (16) and components (60) is
unguarded. Six locations and 14 components is comfortably inside it either way,
but a material that got greedy here would find out in a browser rather than in a
test. See [Fog this opened](#fog-this-opened).

---

## 4. Which route the spec would bless

**Route (d), carrying the instance index.**

The argument is the specification's own, in three of its sentences.

1. *Bindings and vertex are two sources, not one* (`materials.md:352-357`): "The
   bindings, the instance record and the vertex input are frozen; `vs_main` and
   `fs_main` are free. A material replacing only `fs_main` wants `vs_main`
   handed to it; **one replacing `vs_main` needs the bindings without it.**"
   That last clause is this material, described in advance. Route (d) is the
   split being used for the case it was cut for.

2. The whole point of the published set is that the frozen half is not re-typed.
   `materials.md:10-17` opens on it and `#154` argues it "more strongly [than
   `keycolor.wgsl`], because getting the ramp subtly wrong is a visual bug and
   getting a binding wrong is a **silent** whole-frame failure". Route (a) is the
   only route that re-types the frozen half; route (d) re-types none of it.

3. `materials.md:200-201`: the frozen-record rule is "affordable" only because
   there is a cheap way to get per-batch and per-instance data without touching
   the record. Route (d) is that: the record is untouched, and the material reads
   it where it likes.

Ranking, on what each costs:

| route | contract change | frozen things re-typed | verdict |
|---|---|---|---|
| **(d) own struct beside the include** | none | none | **blessed** |
| (b) pack a spare channel | n/a | n/a | impossible — no spare channel exists |
| (a) decline the bindings | none | 4 bindings + the frozen record | legal but the worst; creates an untested third copy of the record |
| (c) canvas publishes a wider struct | yes | none | unnecessary; taxes every sprite material for one effect |

**The one thing worth changing in cog is documentation, not contract.** Route (d)
is discoverable only by noticing that `VertexOut`'s name is not frozen, and both
the specification and the README list `VertexOut` in the published inventory
without saying it is free. One sentence in
`canvas/docs/specs/materials.md:352-357` and in `spritebindings.wgsl`'s header —
*a material that writes its own `vs_main` declares its own inter-stage struct
under its own name; `VertexOut` is a convenience, not part of the contract, and
the record is readable from `fs_main` given the instance index* — would turn a
finding into a documented affordance. That is one paragraph, not a contract
change, and it is the only cog-side change this ticket implies.

---

## Fog this opened

Two things this research turned up that the map does not have a ticket or a fog
entry for.

**1. `s.frame` is a degenerate point for `FillRect`, so the map's escape does not
reach one of its three shapes.** The map's established finding is that "`s.frame`
is the escape" — reject taps outside it and read them as alpha 0. But the
generated white texel's UV rect is a *point*, not a rectangle:
`canvas/atlas.go:276-280` sets `entry.uv = m.Vec4{centerX, centerY, centerX,
centerY}` for `whiteAtlasKey`. A frame-rectangle test against a zero-area rect
rejects every tap but one, so a `FillRect` halo cannot be built from the frame at
all — it has to come from the *geometry* rect (`transform0.zw` and the quad
parameter), because a fill has no texture footprint to sample the silhouette
from. The mechanism is therefore two mechanisms wearing one material, and which
one applies is a per-instance branch. The map's fog entry *"A texture-space
kernel is wrong for fills"* is adjacent but is about the *width* conversion, not
about where the silhouette comes from.

**2. Nothing in cog checks inter-stage limits.** `checkWebLimits`
(`gfx/limits.go:11-43`) measures storage buffers, bind groups and uniform size
against the WebGPU floor. The floor also caps inter-stage variables at 16 and
inter-stage components at 60, and a material that widens its own inter-stage
struct — which this research now recommends — is exactly the thing that could
exceed it and pass every test cog has. Out of scope for the halo (six locations)
but in scope for the mechanism the halo blesses.

---

## Appendix: the check that was run

Two claims were not derivable by reading and were verified by running the real
front end. A throwaway test was added to package `canvas`, run, and deleted; it
is reproduced here rather than committed.

It flattened a hand-written material through
`gfx.FlattenShader(storage.NewFileSystem(builtinMountID, builtinFS), ...)` —
the same path `TestAMaterialExtendingTheUniformBlockCompiles`
(`canvas/canvas_test.go:1266-1289`) uses — then `naga.Parse` + `wgsl.Lower`, the
same front end the backend puts every shader through
(`wgpu/gfxreflect.go:14-18`).

The material: hand-written extended `CanvasUniforms`, `//#include
builtin/canvas/spritebindings.wgsl`, `//#include builtin/canvas/clip.wgsl`, a
`struct HaloOut` with seven locations, a `vs_main` expanding the quad by
`u.haloRadius` on all four sides, and an `fs_main` that both reads `in.frame`
carried inter-stage *and* reads `instances.data[in.instance].frame` directly.

It parsed and lowered clean. The reflected bindings were exactly the contract's:

```
binding u              @group(0) @binding(0)
binding canvasSampler  @group(1) @binding(0)
binding canvasTexture  @group(1) @binding(1)
binding instances      @group(2) @binding(0)
```

and the lowered module carried both structs side by side:

```
type "CanvasUniforms"  type "SpriteInstance"  type "Instances"
type "VertexOut"       type "HaloOut"
```

Which establishes the two load-bearing facts: **an unused `VertexOut` alongside a
material's own inter-stage struct is not an error**, and **`fs_main` may index
`instances` directly**, given the index.
