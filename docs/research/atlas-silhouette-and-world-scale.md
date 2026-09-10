# How each atlas shape carries its silhouette, and what one world unit measures in its texture

Research note for [cog#190](https://github.com/dvoyni/cog/issues/190), under the map
[cog#188 — canvas: a reusable halo behind any sprite, glyph or fill](https://github.com/dvoyni/cog/issues/188).

Every citation is to source in this repo at the commit this branch was cut from. Nothing here was
inferred from a write-up; every claim names the line that owns it.

---

## Summary

1. **Extrusion copies all four channels, alpha included.** `paddedRGBA` copies a 4-byte texel
   (`canvas/atlas.go:374`), so a sprite's 2px pad is an *opaque* replica of its border pixel. A halo
   that samples into a sprite's pad reads solid silhouette, not transparency. Confirmed as the map
   assumed.
2. **A glyph's 1px pad is `(0,0,0,0)`** — the `continue` at `canvas/atlas.go:367-369` leaves the
   zero-initialised `make([]byte, …)` (`canvas/atlas.go:362`) untouched. Straight alpha, not
   premultiplied, so it is transparent *black*, and RGB=0 there differs from a glyph's own RGB=255
   (`canvas/font.go:112-115`).
3. **`frame` covers content only, never the pad**, and its bounds sit on texel *edges*, not texel
   centres (`canvas/atlas.go:262-267`).
4. **The white texel's `frame` has zero extent**, not one texel: it is collapsed to a single texel
   *centre* (`canvas/atlas.go:276-280`). A fill therefore has no texture neighbourhood at all, and
   its texel-per-world ratio is `0`, not merely large or small.
5. **The texel-to-world ratio is computable from the instance record alone.** For a world-space
   radius `r`, `duv = r * abs(frame.zw - frame.xy) / transform0.zw` — see
   [The arithmetic](#the-arithmetic). No uniform, no layer transform, no viewport scale is needed,
   because `transform0.zw` is already in the layer's world units. It degenerates to zero for a fill,
   which is exactly why a halo radius must be a world quantity converted per instance.
6. **The atlas sampler clamps to the *page*, not to the frame** (`canvas/batch.go:157`). Stepping
   outside `frame` and still inside the page reads whatever the shelf packer put next door. `frame`
   really is the only fence.
7. **`TileX`/`TileY` never reach the sprite material at all** (`canvas/plugin.go:277-284`). A tiled
   sprite is a *triangles* draw over a standalone repeat texture. A sprite halo will never see one.

---

## 1. What `paddedRGBA` writes, per case

```go
// canvas/atlas.go:357
func paddedRGBA(source []byte, width, height, padding int, extrude bool) []byte {
	if padding == 0 {
		return append([]byte(nil), source...)
	}
	dstWidth, dstHeight := width+padding*2, height+padding*2
	destination := make([]byte, dstWidth*dstHeight*4)
	for y := 0; y < dstHeight; y++ {
		for x := 0; x < dstWidth; x++ {
			sourceX, sourceY := x-padding, y-padding
			inside := sourceX >= 0 && sourceX < width && sourceY >= 0 && sourceY < height
			if !inside && !extrude {
				continue
			}
			sourceX = min(max(sourceX, 0), width-1)
			sourceY = min(max(sourceY, 0), height-1)
			sourceOffset := (sourceY*width + sourceX) * 4
			destinationOffset := (y*dstWidth + x) * 4
			copy(destination[destinationOffset:destinationOffset+4], source[sourceOffset:sourceOffset+4])
		}
	}
	return destination
}
```

**The whole answer is the `4` in `sourceOffset+4` at `canvas/atlas.go:374`.** The copy is per *texel*,
not per channel. There is no channel mask anywhere in the function, and no alpha-aware branch.

### Sprites — `padding=2, extrude=true` (`canvas/atlas.go:237`)

`inside` is false in the pad; `extrude` is true, so the `continue` is skipped and the source index is
**clamped** to the nearest border texel (`canvas/atlas.go:370-371`). The pad is therefore a literal
RGBA replica of the sprite's border texel, replicated two texels outward, with the four corners
replicating the corner texel.

**Alpha is extruded exactly like RGB.** A sprite whose border texels are opaque has an opaque
2px halo of smear around it in the atlas. This is the map's "padding is not a channel a halo can
widen its way out of", now confirmed at the line.

The nuance worth keeping: a sprite whose art already has transparent margin extrudes *transparency*,
so extrusion is only hostile for art that runs opaque to the file edge. It is not safe to rely on
either case — the pad is whatever the border was.

Source pixels are straight (non-premultiplied) `NRGBA` — `decodeResourceImage` draws through
`image.NewNRGBA` (`canvas/atlas.go:352-354`) — so an extruded texel with `A=0` still carries its
original RGB, and one with `A=255` carries full colour.

### Glyphs — `padding=1, extrude=false` (`canvas/font.go:118`)

`inside` is false in the pad and `extrude` is false, so `continue` fires (`canvas/atlas.go:367-369`)
and the destination byte quad is never written. `destination` came from `make([]byte, …)`
(`canvas/atlas.go:362`), which Go zero-fills.

**A glyph's 1px pad is `(0,0,0,0)`: transparent black, all four channels zero.** Not premultiplied —
the atlas holds straight alpha throughout, and premultiplication never happens anywhere in canvas
(the pipeline blends with `BlendAlpha`, documented as "straight-alpha over blending" at
`gfx/contract.go:124-125`, reached because `StateOverlay2D` is the zero `MaterialState`
(`gfx/material.go:37`) and the sprite material takes it at `canvas/shader.go:91-94`).

That RGB=0 matters and is easy to miss: a glyph's own texels are RGB=255 with coverage in alpha
(`canvas/font.go:111-115`, and the comment at `canvas/atlas.go:325-327` says so out loud). The pad is
RGB=**0**. Under linear filtering the RGB channels are interpolated too, so a tap straddling the
glyph boundary yields a *darkened* white, not white at reduced alpha. With straight-alpha blending
that is a genuine colour shift at the boundary half-texel, not just a coverage ramp. Any halo that
reads glyph RGB rather than glyph alpha will see it. **Read glyph alpha, never glyph RGB.**

### The white texel — `padding=0` (`canvas/atlas.go:230`, `canvas/atlas.go:244`)

`paddedRGBA` returns the source verbatim (`canvas/atlas.go:358-360`). One texel, `(255,255,255,255)`,
with **no pad at all**. Its immediate neighbours in the atlas page are whatever the shelf packer
placed next (`canvas/atlas.go:45-61`) — another sprite's extruded smear, most likely, since the white
texel is inserted first on demand (`canvas/atlas.go:242-248`) and then packed against.

### Colour space

All three share one array at `gfx.FormatRGBA8Srgb` (`canvas/atlas.go:328`), and the comment there
(`canvas/atlas.go:323-327`) records why that is correct for all three. Standard sRGB texture
semantics apply: **RGB is decoded on sample, alpha is not**. A halo reading alpha gets the byte value
linearly; a halo reading RGB gets linearised colour.

---

## 2. How `frame` is computed

```go
// canvas/atlas.go:262-267
uv: m.Vec4{
	X: float32(x+padding) / float32(a.config.AtlasSize),
	Y: float32(y+padding) / float32(a.config.AtlasSize),
	Z: float32(x+padding+width) / float32(a.config.AtlasSize),
	W: float32(y+padding+height) / float32(a.config.AtlasSize),
},
texelSize: 1 / float32(a.config.AtlasSize),
```

`(x, y)` is the top-left of the **padded** rectangle (`canvas/atlas.go:249-250`); adding `padding`
steps in to the content. So:

- **`frame` covers content only.** The pad is outside it on all four sides, by exactly `padding`
  texels. There is no way to widen `frame` into the pad; nothing computes such a rect.
- **`frame` bounds are texel *edges*.** `frame.x * AtlasSize` is an integer, so `frame.xy` is the
  outer edge of the first content texel and `frame.zw` the outer edge past the last. The first
  content texel's *centre* is at `frame.xy + 0.5/AtlasSize`. This half-texel is the whole reason
  extrusion exists (see [§5](#5-what-the-sampler-does-at-and-past-the-frame-edge)).
- **The extent is exactly the content size**: `(frame.z - frame.x) * AtlasSize == entry.width`,
  `(frame.w - frame.y) * AtlasSize == entry.height`, with `entry.width/height` being the *unpadded*
  content dimensions (`canvas/atlas.go:270-271`).

### The white texel is the exception, and it is not what the ticket assumed

```go
// canvas/atlas.go:276-280
if key == whiteAtlasKey {
	centerX := (float32(x) + 0.5) / float32(a.config.AtlasSize)
	centerY := (float32(y) + 0.5) / float32(a.config.AtlasSize)
	entry.uv = m.Vec4{X: centerX, Y: centerY, Z: centerX, W: centerY}
}
```

**`frame.xy == frame.zw`.** The rect is collapsed to the texel's centre point — it is *degenerate*,
not one texel wide. The ticket's phrasing ("its `frame` is one texel wide however large the rect")
overstates it: the frame has **zero** extent.

Consequences, all load-bearing for the halo:

- `out.uv = mix(s.frame.xy, s.frame.zw, quad)` (`canvas/builtin/canvas/spritevertex.wgsl:32`) is
  constant across the whole quad. Every fragment of a fill samples the identical texel centre.
- The collapse to the centre is what makes the fill immune to filtering: a linear tap exactly on a
  texel centre returns that texel with weight 1. Without it, a fill would blend its neighbours in.
- **A fill's texel-per-world ratio is literally `0 / size`.** There is no neighbourhood to walk. A
  halo for a fill cannot be a texture-space kernel under any radius; it must be synthesised from
  geometry (`in.canvasPosition` and the instance's rect), not sampled.

### Sub-frames and flips

`entryUV` (`canvas/plugin.go:618-637`) insets `frame` by `SpriteTransform.Frame` in texels
(`entry.texelSize`, `canvas/plugin.go:626-629`) and then **swaps components for flips**
(`canvas/plugin.go:631-636`).

**Therefore `frame.z - frame.x` can be negative.** Any halo arithmetic must use `abs()`. This is the
single easiest way to get a flipped sprite's halo silently inverted.

One asymmetry worth recording, because it surprises: `entrySize` sizes from the *whole* entry
(`canvas/plugin.go:589-590` → `spriteSize(entry.width, entry.height, …)`), not from the sub-frame. A
sprite with a `Frame` inset and no explicit `Size` draws the sub-rect stretched over the *full*
texture's natural size. The halo arithmetic below stays correct regardless — it reads the actual
`frame`→`size` mapping, whatever produced it — but the ratio for such a sprite is not `1` even at
`Scale=1`. Nine-slice is unaffected: it sets each part's `Size` explicitly
(`canvas/plugin.go:328-351`).

---

## 3. The arithmetic

### The chain, per instance

`canvas/builtin/canvas/spritevertex.wgsl:16-36`:

```wgsl
let scaled  = (quad - origin) * s.transform0.zw;          // :21
let rotated = /* R(theta) * scaled, theta from transform1.zw */;  // :22-25
let local   = s.transform0.xy + rotated;                  // :26
let world   = u.canvasLayer * vec4<f32>(local, 0.0, 1.0); // :27
out.position = vec4(world.x*2/viewport.x - 1, 1 - world.y*2/viewport.y, 0, 1); // :30
out.uv = mix(s.frame.xy, s.frame.zw, quad);               // :32
```

with `quad` the unit quad in `[0,1]²` (`canvas/builtin/canvas/spritevertex.wgsl:7-9`) and
`viewport = u.canvasViewport.xy`, which the batcher fills with the layer's **logical** surface size
(`canvas/batch.go:150`, `surf.size` from `canvas/plugin.go:229-237`).

Read off the two lines that matter — `:21` and `:32`. Both are linear in the same `quad`. So over the
sprite's own local axes:

```
uv      spans  frame.zw - frame.xy
local   spans  transform0.zw
```

### The ratio

```
uvPerWorld    = abs(s.frame.zw - s.frame.xy) / s.transform0.zw      // vec2, per axis
texelPerWorld = uvPerWorld * f32(textureDimensions(canvasTexture).x)
worldPerUv    = s.transform0.zw / abs(s.frame.zw - s.frame.xy)
```

and a halo of world radius `r`:

```
duv = r * abs(s.frame.zw - s.frame.xy) / s.transform0.zw
```

**This is computable from the instance record alone.** Everything on the right is
`SpriteInstance.Frame` and `SpriteInstance.Transform0`
(`canvas/builtin/canvas/spritebindings.wgsl:33-40`, mirrored at `canvas/shader.go:123-131`).

Three reasons it needs nothing more:

- **`transform0.zw` is already in layer world units.** It is `entrySize`/`spriteSize` output
  (`canvas/batch.go:170`, `canvas/plugin.go:597-612`), fed into `local` at `:21` and only *then* run
  through `u.canvasLayer`. A world-unit radius and the sprite's size live in the same space, so the
  layer transform cancels.
- **Rotation cancels too.** `transform1.zw` (sine/cosine, set at `canvas/batch.go:181`) rotates
  `local` *after* the scale, and `uv` is built from the unrotated `quad`. The ratio is stated in the
  sprite's own local axes, which is exactly the space `uv` steps along. A rotated sprite needs no
  correction.
- **The two axes are independent.** `transform0.zw` and the frame extents are per-axis, so a
  non-uniformly stretched sprite gets a correctly anisotropic conversion for free — and a halo that
  wants to stay *circular in world space* gets that automatically, because both `duv.x` and `duv.y`
  come from the same `r`.

### What sits between world units and screen pixels, and when you would care

`u.canvasLayer` is `Translation4(offset) · Scaling4(scale)` (`canvas/plugin.go:240-243`) with
`scale`/`offset` from `LayerTransform` (`canvas/types.go:43-61`), which is where `AspectMode` acts:
`AspectInscribe` takes the min of the two axis scales, `AspectOverlap` the max, `AspectStretch`
neither (`canvas/types.go:48-55`). Layer transforms are rotation-free — asserted at
`canvas/plugin.go:821-823` and true by construction at `canvas/plugin.go:241`.

`m.Mat4` is a flat `[16]float32` (`m/matrix.go:8`) with translation at indices 12-14
(`m/matrix.go:285-288`), i.e. column-major, matching WGSL's `mat4x4` and the `M * v` at `:27`. So in
the shader:

```wgsl
let layerScale = vec2<f32>(u.canvasLayer[0][0], u.canvasLayer[1][1]);  // == Go [0], [5]
```

Then, for the record:

| radius expressed in | conversion | available in shader? |
|---|---|---|
| layer world units | `r * frame_extent / transform0.zw` | **yes**, record alone |
| logical viewport px | divide `r` by `layerScale` first | **yes**, `u.canvasLayer` diagonal |
| atlas texels | `r / (frame_extent * AtlasSize)` | **yes**, `textureDimensions(canvasTexture)` |
| physical framebuffer px | needs `surf.scale` | **no** — see below |

**The one thing canvas does not hand the shader is the framebuffer scale.** `CanvasUniforms` is
`canvasViewport` (`xy` = logical size, `z` = clipEnabled, `w` unused), `canvasLayer`, `canvasClip`
(`canvas/builtin/canvas/uniforms.wgsl:23-28`); `surface.scale` (`canvas/plugin.go:221`,
`canvas/plugin.go:233-236`) is never uploaded. A halo radius in *physical device pixels* is not
computable. A halo radius in world units — the shape this map wants — is.

`AtlasSize` (`canvas/config.go:6`, default 4096 at `canvas/config.go:13`) is likewise not in the
uniform block, but `textureDimensions(canvasTexture)` reads it off the bound array with no new
binding, so it is not a gap.

### Two spare floats already exist in the frozen record

`misc` is `vec4<f32>` and only `.x` is used, as the atlas layer index
(`canvas/builtin/canvas/spritebindings.wgsl:38`, `canvas/shader.go:128`, written at
`canvas/batch.go:184`). `misc.yzw` are declared **unused**. Writing them changes no offset and no
size, so it does not violate the freeze the way adding a member would.

This does not by itself solve the map's open problem — `VertexOut`
(`canvas/builtin/canvas/spritebindings.wgsl:48-55`) uses locations 0-4 with nothing free, and
`@builtin(instance_index)` is a vertex-only builtin in WGSL, so the fragment stage cannot re-derive
which instance it belongs to. Worth noting for [cog#191](https://github.com/dvoyni/cog/issues/191)
though: the storage buffer **is** readable from the fragment stage — every canvas binding is created
with `ShaderStageVertex | ShaderStageFragment` (`wgpu/gfxbackend.go:475`, `wgpu/gfxbackend.go:481`).
The missing piece is only the index, not the access.

---

## 4. Glyph raster size vs `TextDraw.Size`

The rule, in order:

1. **Raster size in physical pixels** (`canvas/plugin.go:662`, and again at `canvas/plugin.go:717`):
   ```go
   px := max(1, int(math.Round(float64(op.draw.Size*textRasterScale(layerTransform, surf)))))
   ```
   with `textRasterScale = max(|layer[0]|, |layer[5]|) * surf.scale` (`canvas/plugin.go:824-836`) —
   the layer's uniform scale times the physical-to-logical ratio.
2. **`surf.scale`** is `FramebufferWidth / Width` for a screen layer, and **1** for a
   texture-targeted layer (`canvas/plugin.go:230-237`; the reasoning is at
   `canvas/plugin.go:210-218`).
3. **The face is baked at `px` with `DPI: 72`** (`canvas/font.go:73-75`), so one point equals one
   pixel and the em box is exactly `px` pixels tall.
4. **`entry.width/height` are the glyph's bitmap bounds** — `bounds.Dx()`, `bounds.Dy()` from
   `face.Glyph` (`canvas/font.go:92`, `canvas/font.go:101`) — not the em box. A period is a few
   texels; a capital is most of `px`.
5. **Layout converts back down** by `toLogical = Size / px` (`canvas/plugin.go:667`), and the glyph's
   instance size is its atlas pixels times that (`canvas/plugin.go:694-696`):
   ```go
   Size: m.Vec2{X: float32(glyph.entry.width) * toLogical, Y: float32(glyph.entry.height) * toLogical},
   ```

### The consequence

Substituting into the ratio from §3, `entry.width` cancels:

```
texelPerWorld = entry.width / (entry.width * toLogical) = 1 / toLogical = px / TextDraw.Size
              ~= layerScale * framebufferScale
```

**One atlas texel is one physical screen pixel for a glyph**, up to the `round` and the `max(1, …)`
clamp. That is the point of the whole arrangement, and it is a useful sanity check: a glyph halo of
`r` world units is `r * layerScale * framebufferScale` texels wide, which for the common
`layerScale = 1`, `framebufferScale = 2` case is 2 texels per world unit — where the glyph's pad is
1 texel. **Even a 1-world-unit glyph halo overruns the pad.**

### Why the framebuffer-scale invalidation is needed and a layer-scale one is not

`px` enters the font cache key (`fontKey{path, px}`, `canvas/font.go:17-20`, keyed at
`canvas/font.go:56-57`) and the glyph atlas key (`canvas/font.go:117`,
`"\x01<path>\x00<px>\x00<rune>"`). So a **layer zoom** simply bakes and caches another face at
another `px` — no invalidation needed, and old sizes stay resident.

A **framebuffer-scale change** (window dragged to a different-DPI monitor) changes `px` for the same
logical `Size` in the same way, so it too would just cache a new size — but the old glyph pages would
never be reclaimed, since nothing else drops them. Hence
`Lookup.invalidateFontsOnResize` (`canvas/lookup.go:96-106`), called once per frame at
`canvas/plugin.go:110`, which releases the **entire** glyph atlas and closes every baked face when
`FramebufferWidth/Width` changes. The comment at `canvas/lookup.go:80-83` states this is the only
time glyph pages need reclaiming, which is why per-font unload deliberately does not free pages.

**For the halo:** every glyph's atlas entry can vanish and be re-inserted at a different size and a
different page position mid-session. Nothing may cache a glyph `frame` across frames, and nothing may
assume a stable texel-per-world ratio for text.

---

## 5. What the sampler does at and past the frame edge

### The sampler an atlas batch uses

```go
// canvas/batch.go:157
gfx.SamplerParam(SamplerSlot, canvasSampler(gfx.AddressClamp, gfx.AddressClamp, b.filter)),
```

`canvasSampler` sets `Mag = Min = Mip = filter` (`canvas/shader.go:25-27`); the comment above it
notes canvas never generates mipmaps, so mip filtering is inert (one level exists —
`AllocateTexture` at `canvas/atlas.go:328` takes no mip count).

**`AddressClamp` clamps to the page's `[0,1]`, not to `frame`.** The sprite atlas array is one 4096²
page per layer holding many entries. A `uv` stepped outside `frame` is, for any realistic halo
radius, still comfortably inside `[0,1]` — so clamping does nothing and the tap lands on **a
neighbour's pixels**. What it finds, per shape:

| shape | first 1-2 texels past `frame` | beyond that |
|---|---|---|
| sprite | its own border texel, **opaque**, replicated 2 deep | shelf-adjacent entry |
| glyph | `(0,0,0,0)`, 1 deep | shelf-adjacent glyph |
| white texel | *nothing* — pad is 0 | shelf-adjacent entry immediately |

This is the concrete confirmation of the map's finding that `s.frame` is the escape: the shader must
reject taps outside `frame` and read them as alpha 0, because there is no addressing mode and no
padding width that would make an out-of-frame tap safe.

`filter` is a batch key field (`canvas/batch.go:78-82`), so a `FilterNearest` sprite never shares a draw
with a `FilterLinear` one — a halo material cannot assume one filter for a whole layer.

### `FilterLinear` — `gfx.FilterMode` zero value (`gfx/contract.go:96-98`)

Bilinear over the four texels nearest the tap. Because `frame` sits on texel *edges* (§2), a `uv`
exactly at `frame.xy` is half a texel outside the first content texel's centre, so the tap is a 50/50
blend of the first content texel and the pad texel beyond it.

- **Sprites:** the pad *is* a copy of that content texel (§1), so the blend is 50/50 between identical
  values and the result is exact. **This is precisely what `extrude=true` buys** — it makes the
  half-texel overhang invisible at the cost of an opaque smear a halo must not read.
- **Glyphs:** the pad is zero, so the boundary half-texel blends toward transparent black. The glyph's
  outermost half-texel is eroded in alpha and darkened in RGB. In exchange, the 1px of zeros absorbs
  the overhang so no neighbouring glyph bleeds in — the correct trade for text, since a glyph's
  outermost row is antialiased coverage anyway.
- **The white texel:** immune, because its `frame` is a texel *centre* and the tap weight is 1 (§2).
  With `frame` on edges it would have bled its neighbours into every fill on screen.

### `FilterNearest` (`gfx/contract.go:98`)

The tap snaps to the texel containing it. Interior `uv` always lands on content. The boundary case
is arithmetic worth knowing: `uv = frame.zw` gives `uv * AtlasSize == x + padding + width` exactly,
whose floor is the **first pad texel**. In practice rasterisation evaluates at fragment centres
strictly inside the quad, so `uv` never reaches `frame.zw` exactly — but a halo that deliberately
offsets `uv` by a computed `duv` *can* hit it, and must clamp to
`frame.zw - 0.5/AtlasSize` (or reject) rather than to `frame.zw`.

### `TileX` / `TileY` — not this material's problem

`SpriteTransform.TileX/TileY` **never produce a sprite instance.** The dispatch at
`canvas/plugin.go:277-284` diverts a tiled sprite to `drawTiledSprite`, which:

- flushes the sprite batch first (`canvas/plugin.go:281`) — tiling always breaks batching;
- resolves a **standalone, non-atlas, full-image texture** (`atlas.resolveStandalone`,
  `canvas/atlas.go:190-216`), so there is no `frame` and no padding at all;
- draws through the **triangles** family with `AddressRepeat` on tiled axes and `AddressClamp` on the
  others (`tileSampler`, `canvas/plugin.go:552-561`; used at `canvas/plugin.go:424`);
- runs the `uv` past 1 by the repeat count (`canvas/plugin.go:392-399`, and the `texture_2d` analogue
  at `canvas/plugin.go:517-529`), so tiling ignores `Frame` and the flips entirely
  (`canvas/types.go:103-107`).

Two corollaries:

1. **A sprite-family halo will never see a tiled sprite.** Haloing tiled sprites is the triangles
   material the map already put out of scope.
2. **A *pathless* tiled sprite silently drops its tiling flags** (`canvas/plugin.go:277-280`) and
   falls through to the white-texel atlas path — so `TileX` on a fill degrades to an ordinary fill,
   which *does* reach the halo material.

Texture-sourced sprites (`hasTexture`, `canvas/plugin.go:271-274`) likewise leave the sprite family,
for a stated reason: the sprite shader binds a `texture_2d_array` and an arbitrary texture is a
`texture_2d` (`canvas/plugin.go:437-440`).

---

## What this settles for the halo design

- A halo radius **must** be a world-unit quantity converted per instance. Confirmed, and the reason is
  sharper than the map had it: a fill's frame extent is **zero**, so no texture-space radius maps to
  it at all — not "orders of magnitude off", but undefined.
- The conversion **is** computable from `SpriteInstance` alone, with `abs()` for flips. Canvas hands
  the shader everything a world-unit radius needs. It does *not* hand it the framebuffer scale, so a
  device-pixel radius would need a new uniform member.
- A fill cannot be haloed by sampling. Its silhouette is its geometry, and `in.canvasPosition`
  (`canvas/builtin/canvas/spritevertex.wgsl:31`) plus the instance rect is the only description of it
  the fragment stage has. The three shapes may therefore need **two** mechanisms, not one kernel — a
  neighbourhood tap for sprites and glyphs, and an analytic distance-to-rect for fills.
- Sprites and glyphs can share a kernel, but not a tap budget: for a glyph, one world unit is roughly
  `layerScale * framebufferScale` texels, whereas for a sprite it is entirely author-controlled by
  `Size`/`Scale` and is `1` only by coincidence.
- Every tap must be fenced by `frame`. Clamping to the frame rect and reading outside it as alpha 0 is
  not an optimisation; it is the only thing standing between a halo and a neighbouring sprite's art.
