# canvas material contract — specification

`github.com/dvoyni/cog/canvas` draws every 2D thing the engine puts on screen —
sprites, glyphs, nine-slices, filled rectangles, lines, hand-recorded triangles,
and render-target quads — through a small set of built-in materials. This
document specifies what a **custom** material may replace, what it must match
exactly, how canvas parameterises a draw, how draws merge into batches, and how
a material reaches the recording surfaces that never took one.

The contract exists because the failure mode is silent. Every `@group` /
`@binding` a shader declares is reflected and must be bound at draw time; a
missing **storage** binding makes `CreateBindGroup` reject the short list,
`flushBinds` skips, the draw encodes with no bind group, and nothing reaches
`firstErr` or `t.diagnostic` — the whole frame is simply wrong, with no error
anywhere ([gfx: an unsupplied storage buffer binding fails silently](https://github.com/dvoyni/cog/issues/133)).
A custom material is precisely the thing that gets a binding wrong. So the shape
it must match is frozen, published as includable WGSL, and written down here.

This document is the specification the implementation is judged against. It is
assembled from the resolved tickets of
[canvas: the material contract and how a material batches](https://github.com/dvoyni/cog/issues/150);
every section cites the tickets it came from. Where a claim rests on something
unverified it is marked **Gap** and says what would settle it; where assembling
these decisions next to each other settled something no ticket did, it is marked
**Settled here**.

**This specification has since been implemented**, and it is kept as the
contract rather than as a plan: read it for what a custom material may do and
why, and read the code for what it does.
[Required canvas changes](#required-canvas-changes) was the checklist the
implementation session worked from and is now history rather than a to-do list.
The map that produced the document was plan-only — it ended here, with the
engine unchanged — and the measurements below come from the throwaway branch
[`proto/sprite-collapse`](https://github.com/dvoyni/cog/tree/proto/sprite-collapse),
which is not to be merged.

---

## Contents

- [Vocabulary](#vocabulary) · [The two families](#the-two-families)
- [The one sprite shader](#the-one-sprite-shader) · [The halo, and widening the inter-stage struct](#the-halo-and-widening-the-inter-stage-struct) · [Group and binding convention](#group-and-binding-convention)
- [What canvas publishes](#what-canvas-publishes) · [The clip test](#the-clip-test)
- [Parameters and their frequency](#parameters-and-their-frequency) · [Parameter resolution order](#parameter-resolution-order)
- [Batching](#batching) · [The canvas uniform block](#the-canvas-uniform-block)
- [Reaching the surfaces that took no material](#reaching-the-surfaces-that-took-no-material)
- [Required canvas changes](#required-canvas-changes) · [Out of scope](#out-of-scope)

---

## Vocabulary

These words are used precisely throughout, and the ones that are not already
there are added to `CONTEXT.md`. They were pinned because most of the rules
below are resolution-order rules, and a resolution-order rule stated in
interchangeable words is ambiguous.

- **Canvas material** — a `*gfx.MaterialDescr` bound to a canvas draw: a shader
  plus pipeline state plus the parameters that belong to the material rather
  than to a draw. Canvas supplies a default per family, publishes others an app
  can name, and an app may supply its own. *Three built-ins* used to mean both
  *three defaults* and *three published*; since the halo it means neither.
- **Family** — sprite or triangles. The two cannot be one shader (see
  [The two families](#the-two-families)), so *which family* is a property of the
  draw, and a material belongs to exactly one.
- **Material set** — one material per family plus one shared parameter list: the
  thing a **scope** names, because a scope covers draws of more than one family.
  A **draw** names a single material, because at a draw the family is known.
  *Set is a scope word, material is a draw word*; the two are never
  interchangeable.
- **Scope** — the whole op queue, a layer, a `ui.Frame`, or a `ui` element
  subtree: something that covers many draws and supplies a material set to those
  that name none. The nearer scope wins.
- **Batch** — one run of recorded work merged into a single draw call, whatever
  supplies its per-item data. Canvas has two batchers, one per family.
- **Instance record** — the fixed 96-byte `SpriteInstance` struct one sprite
  contributes to its batch's storage buffer. It is not a parameter, not a
  uniform, and not extensible.
  *Avoid*: calling it the instance buffer, which is the array of them.
- **The canvas uniform block** — `struct CanvasUniforms` at `@group(0)
  @binding(0)`, whose contents are **per batch**. *Per-draw uniform block* is
  not a name for it; where the phrase appears below, it is a ticket title
  quoted as history
  ([Does anything move off the per-draw uniform block?](https://github.com/dvoyni/cog/issues/155)).
- **Reserved parameter name** — a name canvas consumes itself and never forwards
  to the material: `canvasTexture`, `canvasSampler`, `tint`, `keyColor`.
- **Entry-point source** — a `.wgsl` file a material names as its root
  (`sprite.wgsl`, `triangles.wgsl`, `texture.wgsl`). Distinct from a **published
  source**, which an app includes and never names as a root.

`CONTEXT.md`'s existing **Batch** entry is scene-shaped — "one run of Instances
sharing a mesh and a Scene material" — and a canvas triangles batch has neither
Instances nor a scene material. It is **generalised, not duplicated**; two
glossary entries called Batch would be worse than one that covers both
([#155](https://github.com/dvoyni/cog/issues/155)).

---

## The two families

Sprites and triangles are two shader families and can never be one shader. The
reason is one binding:

| | sprite | triangles |
|---|---|---|
| `canvasTexture` | `texture_2d_array<f32>` | `texture_2d<f32>` |
| geometry | a unit quad, instanced | concatenated vertices |
| per-item data | the instance record, indexed by `@builtin(instance_index)` | vertex attributes |

Sprites sample the atlas, which is an array texture; triangles sample one
arbitrary texture. A module that included both families' sources would declare
`canvasTexture`, `canvasSampler` and `VertexOut` twice, which is a WGSL
duplicate-declaration error
([Canvas group numbering, and what canvas publishes as includable WGSL](https://github.com/dvoyni/cog/issues/154)).
The specification says so; nothing enforces it, and nothing needs to — the
compiler does.

Canvas therefore has exactly three **defaults**, one per family — the material a
draw that names none gets:

| constructor | family | entry-point source | what it does |
|---|---|---|---|
| `DefaultMaterial()` | sprite | `builtin/canvas/sprite.wgsl` | the instanced atlas sprite draw |
| `DefaultTrianglesMaterial()` | triangles | `builtin/canvas/triangles.wgsl` | samples `canvasTexture` through the key-colour ramp, times vertex colour |
| `TextureMaterial()` | triangles | `builtin/canvas/texture.wgsl` | samples and returns; no ramp |

Canvas also **publishes** materials that are not defaults — ones an app names
deliberately or gets not at all. There is one, and it is the sprite family's
[halo](#the-halo-and-widening-the-inter-stage-struct):
`HaloMaterialSet(HaloProfile) MaterialSet`, over
`builtin/canvas/halo.wgsl`, which paints a soft outward band and no mark.

`texture.wgsl` is a second built-in rather than a parameter on the triangles one
because the difference is the shader: the ramp rewrites any texel whose red and
blue agree within 0.2 in sRGB and whose green is below 0.2, which is what makes
artwork wear a player colour and is silent damage to a rendered image. No key
colour switches it off, because the ramp's output is a function of red alone.

---

## The one sprite shader

From [The one sprite shader, and the shape every sprite material matches](https://github.com/dvoyni/cog/issues/151)
and [prototype: route every sprite through the instanced shader](https://github.com/dvoyni/cog/issues/153).

**There is one sprite path, and it is instanced.** `builtin/canvas/sprite.wgsl`
is today's `spritebatch.wgsl`, renamed onto the deleted file's name: "batch"
stops being a distinction once it is the only sprite path, and an app author
guessing at an include name guesses `sprite.wgsl`. **A lone sprite is the
degenerate one-instance case.**

The single-sprite uniform route — the old `sprite.wgsl`, `defaultMaterial` and
`drawEntry` — is deleted. It was already unreachable from any built-in path:
`drawEntry`'s two callers were both guarded by `op.hasMaterial`, and
`DefaultMaterial()` is called by nothing in cog, cog-examples or feuds-26.
`go build ./...` and `go test ./...` across the whole of cog pass with all of it
gone.

### What a custom sprite material must match exactly

Getting any of this wrong fails at bind time, and per the note at the top of this
document a missing storage binding fails silently.

- **Group and binding numbers, and the resource kind at each.**
  `@group(0) @binding(0) var<uniform>`, `@group(1) @binding(0) var sampler`,
  `@group(1) @binding(1) var texture_2d_array<f32>`,
  `@group(2) @binding(0) var<storage, read>`.
- **The `SpriteInstance` record** — struct name, member names, order and size:
  six `vec4<f32>` at 16-byte offsets, 96 bytes, no padding.

  ```wgsl
  struct SpriteInstance {
      transform0: vec4<f32>, // position.xy, size.xy
      transform1: vec4<f32>, // origin.xy, sine, cosine
      frame:      vec4<f32>, // uv rect (x0, y0, x1, y1)
      tint:       vec4<f32>,
      misc:       vec4<f32>, // atlasLayer, unused, unused, unused
      keyColor:   vec4<f32>,
  };
  ```

  It is hand-mirrored by `canvas.SpriteInstance` (`canvas/shader.go`) and
  uploaded by direct reinterpretation, so a divergence is a silent misread, not
  a compile error. See
  [`TestSpriteInstanceMatchesTheShaderRecord`](#required-canvas-changes).
- **The vertex input** `@location(0) quad: vec2<f32>`, and the
  `@builtin(instance_index)` read that selects the record.
- **The reserved sampler and texture names** `canvasSampler` and `canvasTexture`
  — the exported `SamplerSlot` and `TextureSlot`.

### What a custom sprite material may change

- **The `vs_main` and `fs_main` bodies, entirely.**
- **Appending members to the uniform block.** This is the mechanism by which a
  custom material declares its own per-batch parameters, and it already ships:
  `lensMaterial` (feuds-26 `lens/lens.go`) declares the canvas prefix of
  `CanvasUniforms` and then its own `lensGlass`, `lensWarp` and `lensSource`,
  because gfx resolves parameters by name against the reflected layout and drops
  the ones a shader never declared. The built-in `triangles.wgsl` does the same
  with its `keyColor` member, so the mechanism has a built-in demonstration
  rather than only a documented one.
- **Declaring additional per-instance parameter arrays** at group 2 — see
  [Parameters and their frequency](#parameters-and-their-frequency).
- **Declaring a wider inter-stage struct of its own**, beside the published
  `VertexOut` — see
  [The halo, and widening the inter-stage struct](#the-halo-and-widening-the-inter-stage-struct).

**Appending to the uniform block is not
[Extend a built-in shader with app-defined per-instance properties](https://github.com/dvoyni/cog/issues/148).**
That issue is about extending the *instance record* — per-sprite data, which
needs the gfx storage-member packing that was never built. Extending the
*uniform block* is per-batch data through the one name-packed target gfx already
has. The two look alike and are not, and the difference is the whole reason the
frozen-record rule is affordable. Anyone reading this specification and reaching
for #148 should check which of the two they actually want.

### `tint` and `keyColor` are reserved

They are fields of the instance record and canvas always consumes them: it reads
them out of the draw's parameters by name and packs them in. **They never reach
the material.** A custom shader reads them from the shared `VertexOut`, not from
a uniform.

They become **exported constants alongside `TextureSlot` and `SamplerSlot`**.
Two of the four reserved names are exported constants today and two are bare
string literals scattered through `plugin.go`; that asymmetry is an accident and
ends here.

The rejected alternative was letting a custom material redeclare them as
uniforms, with canvas standing down when a material is present. That reintroduces
the per-draw/per-batch split the collapse exists to remove.

**The reserved-name rule lives in the instance record, not in the uniform
block.** `canvasTransform0`, `canvasTransform1`, `canvasFrame`, `atlasLayer` and
`clipEnabled` cease to exist as parameter names — under the collapse the
transform is per instance, so a draw naming one is dropped by reflection rather
than overridden by canvas. Only `tint` and `keyColor` stay reserved on the sprite
path ([#153](https://github.com/dvoyni/cog/issues/153)).

### What the collapse costs, measured

Reflected through `naga.Parse` + `wgsl.Lower` on `proto/sprite-collapse`:

| | old `sprite.wgsl` | the instanced shader |
|---|---|---|
| bind groups | 2 | 3 |
| uniform block | 176 B → 256 B padded | 96 B → 256 B padded |
| storage | none | `instances`, 96 B per instance |

Eight sprites in one layer:

| what every sprite carries | draws | uniform payloads | instance buffers |
|---|---|---|---|
| `nil` | 1 | 1 | 1 × 768 B |
| `DefaultMaterial()` | 1 | 1 | 1 × 768 B |
| one shared custom material | 1 | 1 | 1 × 768 B |
| a distinct material per sprite | 8 | 8 | 8 × 96 B |

Before the collapse, `DefaultMaterial()` on those same eight sprites was **8
draws**. The worst case — every draw with its own material, every batch one
instance — costs **one extra bind group and one extra buffer object per draw**
against the route it replaces, and the uniform payload *shrinks* from 176 B to
96 B in a slot that is 256 B either way.

`DefaultMaterial()` is **repointed** at the surviving material rather than
deleted. That keeps an exported symbol nothing currently calls (cheap), gives a
caller a way to name explicitly what `nil` means, and makes
`canvas/README.md:61` true again rather than merely deleting a false claim.
Passing `DefaultMaterial()` must batch identically to passing `nil`; see
[Batching](#batching).

### The halo, and widening the inter-stage struct

From [What a sprite material's vertex stage may do, and how its fragment stage
learns the frame rect](https://github.com/dvoyni/cog/issues/189), which blessed
the route, and
[canvas: a halo material, and the two-layer idiom that reaches it](https://github.com/dvoyni/cog/issues/224),
which is the first material to take it. **This part is built**, in
`builtin/canvas/halo.wgsl`.

**A sprite material's `vs_main` may map the frozen unit quad onto a rect larger
than `s.transform0.zw`.** Nothing in canvas reads a sprite's on-screen extent —
no clip intersection, no batch-key field, no flush inspection, no aspect
coupling, and **no culling of any kind** — and the freeze above is on the vertex
*input declaration*, leaving the `vs_main` body free entirely. A quad drawn
larger than its sprite is simply drawn.

**A material's fragment stage learns the instance record by declaring an
inter-stage struct of its own**, under its own name, beside the published
`VertexOut` — which is not frozen and is named nowhere in Go. It includes
`spritebindings.wgsl` **alone**, not `spritevertex.wgsl`, because it is replacing
`vs_main` as well.

The thing carried across is the **instance index**, flat, and not the record or
any field of it. Every reflected binding is bound `Vertex|Fragment`
(`gfx/limits.go:9-10`, `wgpu/gfxbackend.go:475`), so one `u32` component buys the
whole 96-byte record where the frame rect alone would cost four:

```wgsl
struct HaloVertexOut {
    @builtin(position) position: vec4<f32>,
    @location(0) canvasPosition: vec2<f32>,
    @location(1) uv: vec2<f32>,
    @location(2) @interpolate(flat) atlasLayer: i32,
    @location(3) tint: vec4<f32>,
    @location(4) @interpolate(flat) index: u32,
    @location(5) quad: vec2<f32>,
};
// in fs_main: let s = instances.data[in.index];
```

Two routes were rejected rather than overlooked. **Declining the published
bindings and hand-declaring them** is legal — `lens/lens.go:58-80` in feuds-26 is
the shipped precedent — but it creates a **third, untested copy** of the FROZEN
`SpriteInstance`, which only `TestSpriteInstanceMatchesTheShaderRecord` guards
and only inside cog. **Packing into spare components of an existing location** is
impossible: each member owns its own `@location`, and `canvas/batch.go` writes
only `Misc.X`, so `misc.yzw` are unreachable by an app anyway.

**Gap: nothing in cog checks the WebGPU inter-stage floor.** `checkWebLimits`
(`gfx/limits.go:11-43`) counts storage buffers, bind groups and uniform size
only, while the floor also caps inter-stage variables at 16 and components at 60.
A material widening this struct is exactly what could exceed that and pass every
test gfx has. `HaloVertexOut` spends 6 locations and 12 components, so the halo is
not the thing that trips it, and `TestTheHaloInterStageStructFitsTheWebGPUFloor`
holds that one shader to the floor by hand. The general check is
[#226](https://github.com/dvoyni/cog/issues/226).

**`any()` and `all()` over a vector of bools are safe again.** naga's SPIR-V
backend could not lower `ir.ExprRelational`: such a comparison compiled as WGSL,
passed `wgsl.Lower`, and died at pipeline creation with
`unsupported expression kind: ir.ExprRelational`, so every vector comparison in a
canvas shader was spelled out component-wise. Fixed in the naga fork `go.mod`
overrides ([#227](https://github.com/dvoyni/cog/issues/227)); the halo reads
`any(tap < lo)` again. Two tests hold it: `TestEveryBuiltInCompilesToSpirv` takes
each entry point past the IR to SPIR-V, which is what catches a gap like this in
cog rather than on a device, and
`TestTheHaloVectorComparisonsReachTheSPIRVBinary` fails if the override is
dropped before a fixed naga is released.

**Lowering to IR is not the whole front end.** That is the general lesson: a
shader can parse and lower and still be rejected by the backend that has to
produce SPIR-V. Take a new material all the way to a binary in a test.

---

## Group and binding convention

From [Canvas group numbering, and what canvas publishes as includable WGSL](https://github.com/dvoyni/cog/issues/154).

**Canvas numbers bind groups by what the binding *is*, not by how often it
changes.**

| group | what lives there | declared by |
|---|---|---|
| 0 | the canvas uniform block, `var<uniform> u: CanvasUniforms` | `uniforms.wgsl`, or the app when it extends the block |
| 1 | `canvasSampler` @0 and `canvasTexture` @1 — the texture a draw samples | the bindings source of its family |
| 2 | per-sprite storage: `instances` @0, and any parameter arrays @1… | `spritebindings.wgsl`, plus the app |

**Why kind and not frequency.** Scene numbers by update frequency because it
genuinely has three — per frame, per material, per draw. Canvas has one:
everything in a canvas draw changes per batch, so a frequency ordering would
encode a distinction that does not exist and would be chosen arbitrarily. Kind
encodes one that does, and it buys the thing the convention exists to protect:
`canvasTexture` and `canvasSampler` are **public API names**, so they get one
address in every canvas shader instead of two.

**What changes.** Only the sprite shader, and only its WGSL: `instances` moves
1 → 2 and the sampler/texture pair moves 2 → 1. `triangles.wgsl`,
`texture.wgsl` and the one custom material in production are already at group 1
and are untouched. **Nothing in Go names a group number** — gfx reflects
`@group`/`@binding` out of the source and only `checkWebLimits` reads them,
counting `max(Group)+1` against the floor of 4.

**Why not "sampler at 2 everywhere".** The backend allocates `maxGroup+1`
layouts and creates an empty `BindGroupLayout` for any group nothing declares.
Fixing the sampler at 2 would give `triangles.wgsl` and `texture.wgsl` an empty
group 1, spending a quarter of the WebGPU bind-group floor on nothing. Under the
kind rule every canvas shader is contiguous: sprites use 0, 1, 2 and the other
two use 0, 1, with no hole in either.

**The frequency objection, answered.** Lower group numbers conventionally hold
less-frequently-changed bindings, so a rebind invalidates less. It does not apply
here: the texture and the instance buffer change per batch, together, so no
ordering of the two is cheaper. The backend already filters redundant
`SetBindGroup` calls by identity.

**Consequence:** the sprite shader's uniform struct is renamed `BatchUniforms` →
`CanvasUniforms`. Three of canvas's shaders already use that name for the same
three members; the fourth should not have a private spelling for the block a
published source declares.

**Group 3 stays free.** `MaxBindGroups` is 4. Nothing in this contract claims
group 3, and nothing should without a reason recorded here.

---

## What canvas publishes

Seven sources, flat in `builtin/canvas/`, each named by an exported Go constant
and included by **absolute storage name** — the preprocessor rejects a relative
include from a `ShaderWithText` root, and the only real custom canvas material is
an inline Go string.

| source | constant | declares |
|---|---|---|
| `uniforms.wgsl` | `canvas.UniformsPath` | `struct CanvasUniforms` (`canvasViewport`, `canvasLayer`, `canvasClip`) and `@group(0) @binding(0) var<uniform> u` |
| `clip.wgsl` | `canvas.ClipPath` | `fn canvasClipped(canvasPosition: vec2<f32>) -> bool` |
| `spritebindings.wgsl` | `canvas.SpriteBindingsPath` | group 1 `canvasSampler` + `canvasTexture: texture_2d_array<f32>`; group 2 `instances`; `struct SpriteInstance`; `struct Instances`; `struct VertexOut` |
| `spritevertex.wgsl` | `canvas.SpriteVertexPath` | includes `spritebindings.wgsl`; declares `vs_main` |
| `trianglesbindings.wgsl` | `canvas.TrianglesBindingsPath` | group 1 `canvasSampler` + `canvasTexture: texture_2d<f32>`; `struct VertexOut` |
| `trianglesvertex.wgsl` | `canvas.TrianglesVertexPath` | includes `trianglesbindings.wgsl`; declares `vs_main` |
| `keycolor.wgsl` | `canvas.KeyColorPath` | the sRGB transfer functions, the three `key*` constants, and `keyColorRamp` |

The constants join `DefaultFontPath`, `TextureSlot` and `SamplerSlot` as the
surface an app builds a shader against, and they give the compiler a say in the
`ShaderWithText` case, which is the only case that exists. `KeyColorPath`
promotes today's unexported `keyColorShaderPath`.

`sprite.wgsl`, `triangles.wgsl` and `texture.wgsl` remain the **entry-point
sources** — roots a material names, not sources an app includes. Each becomes a
thin file: its includes, any uniform-block extension, and `fs_main`.

**This table is an inventory, and that is deliberate.** The rule an app must obey
is *do not declare anything a source you included declares*, and a duplicated
binding is a silent whole-frame loss rather than a visible one. Enumerating each
source's declarations is what makes the rule checkable by reading.

### The uniform block is its own source, and that is the point

A custom material may **append members** to the uniform block. Include-once by
resolved path means a published source declaring `struct CanvasUniforms` is one
an extending material can **never** include — WGSL has no way to add a member to
a struct declared elsewhere. So the block gets its own four-line source that an
extending shader **skips and hand-writes**, and no other published source
mentions `u` in a declaration. WGSL is order-independent at module scope, so
`spritevertex.wgsl` reads `u.canvasLayer` while the app declares it.

### Bindings and vertex are two sources, not one

The bindings, the instance record and the vertex input are frozen; `vs_main` and
`fs_main` are free. A material replacing only `fs_main` wants `vs_main` handed to
it; one replacing `vs_main` needs the bindings without it. The split is exactly
those two halves, and the nesting costs nothing under include-once.

### Overriding a published source is a feature

An included path resolves through the full mount overlay, so an app that mounts
its own `builtin/canvas/keycolor.wgsl` at higher priority replaces that one
source inside canvas's own module and keeps the rest. This is cog's
customization mechanism working as designed, and it is recorded as available
rather than left to be discovered.

### What this buys, on the two real materials

**The lens** retypes three binding lines, `struct VertexOut` and all fifteen
lines of `vs_main` verbatim from `triangles.wgsl` — about 24 of the ~30 lines it
declares. After this it keeps its extended struct and its `fs_main` and replaces
the rest with one `//#include builtin/canvas/trianglesvertex.wgsl`.

**A fade sprite material**, whose prototype is the instanced shader copied whole
with two lines changed, becomes six lines of declaration:

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

Seventy lines of copied contract become six lines of declaration plus three
includes.

---

## The clip test

**Settled here.** This was the fog patch
[#154](https://github.com/dvoyni/cog/issues/154) left on the map's *Not yet
specified*, and assembling it against its neighbours changed the answer the map
guessed at.

The clip test is eight lines duplicated verbatim in all three built-in fragment
shaders:

```wgsl
if u.canvasViewport.z > 0.5 && (
    in.canvasPosition.x < u.canvasClip.x || in.canvasPosition.y < u.canvasClip.y ||
    in.canvasPosition.x > u.canvasClip.z || in.canvasPosition.y > u.canvasClip.w
) {
    discard;
}
```

It lives in `fs_main`, which is the one thing every custom material replaces.
Nothing in the published set carries it, so a hand-written `fs_main` that omits
it draws outside the clip rectangle with no error anywhere.

**There is no alternative mechanism.** `SetClip`/`RemoveClip` are implemented
entirely as this shader test: there is no `Scissor` anywhere in gfx or wgpu, so
clipping cannot be moved out of `fs_main` without a gfx change that no ticket on
this map decided. That is why the question has to be answered rather than
sidestepped.

**Canvas publishes the test as `builtin/canvas/clip.wgsl`, declaring one
function, and calling it is offered rather than required.**

```wgsl
fn canvasClipped(canvasPosition: vec2<f32>) -> bool {
    return u.canvasViewport.z > 0.5 && (
        canvasPosition.x < u.canvasClip.x || canvasPosition.y < u.canvasClip.y ||
        canvasPosition.x > u.canvasClip.z || canvasPosition.y > u.canvasClip.w
    );
}
```

Two things about that decision are load-bearing.

**It is a seventh source, not a helper inside `uniforms.wgsl`.** The map guessed
`uniforms.wgsl` was its natural home, because the test reads only from `u`. That
is wrong once it sits next to the include-once rule: an *extending* material —
the one that appends `fade` to the block — can never include `uniforms.wgsl`, so
a helper living there would be unavailable to exactly the shaders most likely to
omit the test. `clip.wgsl` declares no binding and no struct, only a function
that reads `u`, which module-scope order-independence permits. It is therefore
includable by extending and non-extending materials alike, which is the whole
requirement.

**It returns `bool` rather than discarding.** `discard` is legal inside a WGSL
function, but a discard hidden behind a call is worse to read than
`if canvasClipped(...) { discard; }`, and a material that clips by writing
transparent black rather than discarding can then do so.

**Offered, not required**, for the same reason `fs_main` is free at all: a
material rendering into its own target may legitimately not want the layer's
clip, and canvas has no way to enforce a call inside a body it does not own.
What canvas owes instead is that the omission is **loud in the documentation
rather than silent in the frame** — this section, the README, and the one-line
warning in each published source's header comment. The three built-in
entry-point shaders include `clip.wgsl` and call it, so the built-ins are the
worked example rather than a fourth copy.

**Gap.** Whether a scissor rect would be cheaper and unconditional is not
answered here, because gfx exposes none. If gfx ever grows one, this section is
the thing to revisit, and the published function keeps the WGSL path working for
materials that want the test inside the shader anyway.

---

## Parameters and their frequency

From [The batch key: material identity, and what a per-draw parameter does to it](https://github.com/dvoyni/cog/issues/152).
This is the rule everything about batching sits on, so it comes first.

**The call site declares the parameter's frequency.**

| named on | frequency | where it lands |
|---|---|---|
| the **material** | one value per batch | a member of the uniform block, which a custom shader may append to |
| a **sprite draw** | one value per sprite | one storage buffer per parameter name, at group 2, indexed by `@builtin(instance_index)` |
| a **triangles draw** | per material | the uniform block; two values are two materials |

A triangle batch is concatenated vertices with **no instance index**, so the
sprite path's per-instance arrays have nothing to hang on. A parameter named at a
`DrawTriangles` call is therefore per-material: two draws with different values
are two draws. That is what the code does today; here it is deliberate rather
than accidental. The rework that would remove the asymmetry is
[canvas: batch triangle draws that carry a custom material or per-draw parameters](https://github.com/dvoyni/cog/issues/157),
out of scope.

### Per-instance parameter arrays

A sprite draw parameter whose name is not reserved is collected across every
sprite in the batch into one buffer, bound beside `instances`, and read with the
same instance index the record uses:

```wgsl
struct Wobble { data: array<f32> };
@group(2) @binding(1) var<storage, read> wobble: Wobble;
// in vs_main or fs_main: wobble.data[instance]
```

**Settled here: the arrays bind at group 2, binding 1 and up.**
[#152](https://github.com/dvoyni/cog/issues/152) handed "where the arrays bind"
to [#154](https://github.com/dvoyni/cog/issues/154), and #154's six published
sources never mention them — the one gap between two resolved tickets that this
assembly found. Group 2 is the only answer consistent with everything already
decided: the group rule is *by kind*, an array is per-sprite storage exactly as
`instances` is, and putting it anywhere else either breaks contiguity or spends
group 3. The array is declared by the **app**, in its own shader, because canvas
cannot know the name; canvas supplies it by name and gfx binds by reflection, so
canvas still names no group number anywhere in Go. `spritebindings.wgsl`
declares group 2 binding 0 and nothing else in group 2, so an app appending
`@binding(1)` redeclares nothing.

**Canvas never consults the reflected layout at record time.** A draw parameter
is collected into an array unconditionally; recording stays ignorant of the
shader. A name the shader declared as a uniform member but a caller passes at the
draw call is an **authoring error**, not something canvas silently reroutes — see
[What a caller sees at the wall](#what-a-caller-sees-at-the-wall).

**`tint` and `keyColor` stay in the fixed record** rather than being promoted to
their own arrays, so no sprite draw pays two extra bindings for the two values
every sprite has. Only *unrecognised* draw parameters become arrays.

**The budget.** `MaxBindGroups` 4 with the sprite shader using three leaves group
3 free; `MaxStorageBuffersPerShaderStage` 8 with `instances` taking one leaves
**seven** app parameter arrays. Each array pads to `gfx.StorageAlignment` (256),
so a one-sprite batch carrying one float parameter spends 256 bytes on four —
irrelevant at this draw count, noted so nobody rediscovers it as a problem.

### `RawParameter[T]`

`FloatParam` and `VecParam` are safe because canvas controls the layout. An app
parameter that is a Go struct is the `scene/pack.go` hazard again — smaller and
opt-in, but the same class. **A size check alone is not enough**, and the
counterexample is small enough to hit by accident:

```go
type bad struct { A float32; B m.Vec3 }   // Go: 16 bytes. WGSL: 32.
```

Go aligns every `float32`-based struct to 4; WGSL aligns `vec2` to 8 and
`vec3`/`vec4`/matrices to 16. So `bad` is 16 bytes in Go, passes
`size % 16 == 0`, and is laid out at 32 bytes by the shader — every element after
the first reads the wrong memory, silently.

**`RawParameter[T]` validates the layout by reflection and panics otherwise.**
Walk the struct's fields, map each known type to its WGSL `(align, size)` —
`float32` 4/4, `Vec2` 8/8, `Vec3` 16/12, `Vec4` 16/16, `Mat4` 16/64 — compute the
WGSL offset of each field and compare against `unsafe.Offsetof`. Panic on the
first mismatch, naming the field, both offsets, and the padding that would fix
it. The check runs once per type, not per draw. **A bare `size % 16` check must
not ship**, because it is the one that looks like it works. If the reflection
validator is too much machinery for a first implementation, the safe fallback is
restricting members to the 16-byte-aligned types (`Vec4`, `Mat4`) plus explicit
padding — the discipline `SpriteInstance` already follows. It is item 3 of
[#158](https://github.com/dvoyni/cog/issues/158).

---

## Parameter resolution order

From [A material for ui visuals, canvas.Text and the rect helpers](https://github.com/dvoyni/cog/issues/145).

**First wins.** `parameterRefFor` (`gfx/parameterplan.go:119-130`) scans a draw's
parameters front to back and takes the first name match, then the material's.
This is documented nowhere today, and it is contradicted in one place, so the
rule is stated here once.

The order canvas builds, front to back:

1. **canvas's own** — `canvasViewport`, `canvasLayer`, `canvasClip`, the texture
   and sampler slots, the instance buffer. These must win, and do.
2. **the draw's** parameters.
3. **the scope's** parameters, appended *after* the draw's rather than prepended,
   so the draw beats the scope.
4. **the material's own** parameters, which gfx consults last by construction.

Two consequences worth stating rather than discovering:

- A shape helper's `Color` beats a caller's `tint` in the draw parameters,
  because canvas supplies it in group 1. A caller who wants something else sets
  `Color`.
- **`DrawTexture`'s doc comment (`canvas/opqueue.go:263`) is wrong and is
  corrected.** It claims the texture is bound "ahead of the caller's own
  parameters, so a caller that binds the slot itself still wins". Under
  first-wins the prepended binding wins and the caller loses. The comment is the
  thing that is wrong, not the order: first-wins is exactly what lets canvas
  guarantee the viewport, the clip and the transform against anything a caller
  passes. The corrected comment says the binding is canvas's, and a caller who
  wants their own texture uses `DrawTriangles`.

Enforcing a **kind** mismatch — a name matched at the wrong resource kind — is
item 2 of [#158](https://github.com/dvoyni/cog/issues/158); the rule lives here.

---

## Batching

From [The batch key](https://github.com/dvoyni/cog/issues/152), refined by
[#153](https://github.com/dvoyni/cog/issues/153).

**Both batchers key on the material, and neither turns off because a draw carries
one.**

### The sprite key

`textureID`, the layer `m.Mat4`, `clip` and `hasClip`, and `filter` — unchanged
from today's `spriteBatch.keyMatches` — plus:

- **the material's fingerprint**, one `uint64`;
- **the draw-parameter names, in order**, after the reserved names are stripped.

Draw-parameter *values* are **not** in the sprite key. That is the entire point
of the mechanism: they vary per instance and land in arrays, so two sprites
differing only in `wobble` still merge into one instanced draw.

**The reserved names are stripped before the ordered-name key is taken.** `tint`
and `keyColor` arrive as draw parameters but are consumed into the instance
record, so leaving them in the key would split a batch whose draws differ only in
tint — the exact merge the instanced path exists to make. Without this the rule
reads as forbidding what it means to allow ([#153](https://github.com/dvoyni/cog/issues/153)).

**Names, not values, and the name set splits the batch.** Every sprite in a batch
contributes exactly one element to every array, so a sprite carrying `wobble` and
one without cannot share a batch. The rejected alternative is zero-filling the
missing element: for a multiplier, `0` is not "absent" but the opposite of it,
and the failure is a silent visual bug with no error anywhere. Keying on the
ordered name set also matches what gfx does one layer down —
`parameterShapeEqual` caches its plan on the parameter names in order, ignoring
values.

**The material enters by fingerprint, never by the fact of naming one.**
`MaterialDescr.Fingerprint()` (`gfx/material.go:87`) is the key, and canvas
normalises a nil material to the built-in descriptor **at record time**.
`op.hasMaterial` survives as a guard — is there a material to fingerprint — but
is not a key field. That is what makes `DefaultMaterial()` passed explicitly
batch identically to `nil`, which #151 required and `hasMaterial` in the key
would have broken.

**The fingerprint is compared bare, with no verification pass.** A 64-bit
collision would merge two different materials and draw the wrong one; at the
~50–100 draws per frame measured in a feuds-26 battle the probability is around
1e-16 per frame, and a verification branch that never runs costs more than it
protects. Compute it once when the op is recorded, alongside the existing
`CloneTo` into `materialArena` (`canvas/opqueue.go:154`), and store the `uint64`
on the op — the fingerprint survives that clone by design, because it hashes the
descriptor's content and not its address. Canvas becomes `Fingerprint()`'s second
consumer; the first is scene's interning.

### The triangles key

Everything `trianglesBatch.keyMatches` compares today plus the material
fingerprint **and a fingerprint of the draw parameters, values included**.
Because a triangles draw parameter is per-material, a value change *is* a
material change, so values belong in this key where they do not belong in the
sprite one.

**Two triangles draws carrying the same custom material now merge.** Today
`op.hasMaterial` force-flushes unconditionally, so even two identical
custom-material draws are two draws. Under one rule for both batchers that stops:
`trianglesBatch` stores the resolved material and the op's parameters as part of
its key and replays them at flush.

Two things dissolve into that:

- **`unkeyed`** exists only to pick `defaultTextureMaterial` over
  `defaultTrianglesMaterial` at flush — canvas choosing a material inside a
  batcher that would then key on the caller's. Under this rule it is just "the
  material is whatever the op resolved to".
- **`trianglesBatchKey`'s `default: batchable = false` arm.** An unrecognised
  parameter name is no longer a reason to bail out of the batch; it is a key
  field.

**Canvas cannot compute this key against today's gfx.** `ParameterDescr` exposes
`ColorValue`, `FloatValue`, `VecValue`, `TextureValue` and `SamplerValue` and
nothing for `Mat4`, buffers or raw parameters; a value comparison written in
canvas would be a type switch that silently mis-keys every kind it forgets, and
mis-keying **merges** draws that differ. gfx already hashes all of them in the
unexported `ParameterDescr.fingerprint`. Exporting a slice-level helper is item 1
of [#158](https://github.com/dvoyni/cog/issues/158) and is a **prerequisite** for
implementing this key.

### What flushes

- Both batchers flush at the **end of every layer**, so no batch spans two
  layers.
- Each flushes the other whenever op kinds alternate, so sprite/triangle
  alternation costs a draw each way.
- Any key change flushes, which is what preserves draw order.

Two draws remain unbatched, and neither is unbatched because of anything in this
contract: `drawTiledSprite` (`canvas/plugin.go:416`) and `emitTextureQuad`
(`:509`). The texture-sprite one is
[canvas: one sprite shader source with an atlas/texture variant](https://github.com/dvoyni/cog/issues/149),
out of scope. `emitTrianglesDirect`'s bail-outs are dissolved by the triangles
key above, and `drawEntry` is deleted.

### What a caller sees at the wall

Nothing on the sprite path silently unbatches any more. Three limits remain, and
each is a wall the caller meets rather than a cost they pay unknowingly:

- **A per-sprite value that must live inside `SpriteInstance`.** There is no API
  to add a member to the frozen record, so this is met at authoring time. It is
  [#148](https://github.com/dvoyni/cog/issues/148), out of scope, and the reason
  the frozen-layout rule is affordable.
- **A draw parameter naming something the shader declared as a uniform member.**
  One name, two frequencies; an authoring error, and **detected**.
  `parameterRefFor` matches by name and ignores kind entirely today, so this
  currently binds a buffer descriptor into a uniform slot and draws garbage with
  no diagnostic. The check belongs in plan *construction*, which
  `prepareParameterPlan` caches per `(shader, parameter shape)`, so it runs once
  per distinct shape and adds nothing per draw. Item 2 of
  [#158](https://github.com/dvoyni/cog/issues/158).
- **Triangles draws whose parameter values differ still split.** Per-material by
  rule; three draws a frame in the fade's conversion plan against eighteen sprite
  sites.

### Cost

`keyMatches` gains 8 bytes on the sprite path and 16 on the triangles path,
against the `m.Mat4` both already compare by value.

---

## The canvas uniform block

From [Does anything move off the per-draw uniform block?](https://github.com/dvoyni/cog/issues/155),
which answers [canvas: pooled storage records instead of per-draw uniform buffers](https://github.com/dvoyni/cog/issues/28)
in the negative. This section exists so that #28's title does not invite a
re-open.

**Nothing moves off the uniform block** — not sprites, not triangles, not texture
draws, not the two unbatched escapes.

**1. Moving would delete the extension mechanism this contract is built on.** gfx
cannot pack named parameters into a storage struct — `ShaderResource.Members` is
reflected at `wgpu/gfxreflect.go:46` and read by nothing outside tests — so the
uniform block is the **only name-packed target gfx has**. Appending a member to
that block is how a custom material declares its own parameters, and
`uniforms.wgsl` is published so an extending shader hand-writes four lines and
includes the rest. Under a storage record a custom material's `fade: f32` would
have to arrive as bytes mirrored into an arena that belongs to canvas and that an
app cannot reach at all. That is not a cost of implementing the move; it is the
move removing the feature.

**2. The word the migration was named for is already gone, twice.** Every sprite
is in an instanced batch and `drawEntry` is deleted; an unrecognised sprite draw
parameter becomes a storage array indexed by `@builtin(instance_index)`; the
triangles batcher no longer bails out on a custom material. What remains in the
block is batch-shared by construction: `canvasViewport`, `canvasLayer`,
`canvasClip`, plus whatever a material appends. **The per-draw values already
moved.** The shared block is what is left, and it is the part that should not.

**3. The saving would be buffer objects and upload calls, never bytes.** Storage
binding offsets are 256-aligned and the uniform pool pads to 256, so a 96-byte
sprite block occupies 256 bytes either way.

**4. It would retract a published source.** `uniforms.wgsl` and
`canvas.UniformsPath` are app-facing.

### gfx's uniform path has one consumer, and that is not a reason to delete it

Every scene binding is `var<storage, read>`, `sceneFrame` included. The four
`var<uniform>` declarations in the repo are all canvas's. So `SetParams`,
`ShaderLayout.Uniforms`, `uniformMax` and the pool exist for canvas alone, and
the next reader will notice, see scene packing storage by hand, and reason that
canvas should follow.

**Scene did not choose storage over a uniform block.** It needed arrays of
records, took the only path gfx offers for those, and pays for it with
`scenePbrRecord` (`scene/pack.go:109`) hand-mirrored against WGSL and sized with
`unsafe.Sizeof` — the exact hazard
[Bind-group frequency convention](https://github.com/dvoyni/cog/issues/9) said it
was avoiding. One consumer is an argument for finishing #9's other half, not for
deleting the half that works.

So the decision is not "uniform wins, storage loses". It is **by-name packing
wins**, and canvas is simply where gfx already has it. The stated direction of
travel is that scene's records eventually gain the same property — an app extends
a built-in scene shader by *naming* its values, packed through the `Members`
layout gfx already reflects — rather than the two diverging into extend-by-name
for canvas and extend-by-memcpy for scene. **This is an intent, not a
commitment**: nothing here blocks on #148 and nothing here waits for it. Note
that the per-instance parameter arrays already dodge the hazard on the sprite
path without any of that machinery, because one buffer per parameter name means
there is no struct to pack.

### The arithmetic, and the number this specification records

There is **no draw counter anywhere** in cog: `OpQueue.OpCount()` counts recorded
ops, not GPU draws, and is read only by tests. The measurement #28 and
[Batch draws that carry a custom material](https://github.com/dvoyni/cog/issues/146)
both asked for is not executable, and **no counter is built for this**.

The bound is recorded instead: uniform-carrying draws per frame ≤ batch flushes +
unbatched escapes, where flushes are bounded by layers × sprite/triangle
alternations. Batching only ever reduces the count, so the ~50–100 measured today
is an **upper** bound on the post-batching number, and a decision that survives
its upper bound does not need the exact one. The absence of the measurement is a
recorded position, not a gap.

**The pool this makes permanent** is
[gfx: one growable uniform buffer with 256-strided offsets](https://github.com/dvoyni/cog/issues/159),
filed and out of scope. It covers both memories — the GPU-side slice that grows
one 256-byte buffer at a time to a frame's high-water mark and never releases,
and `t.uarena`, sized `len(queue.ops) * 256` whether or not an op carries a
uniform — because the fix is one shape for both, with no WGSL change and no
canvas change. It is the cheap answer to "what if the buffer count ever matters",
in place of the migration.

---

## Reaching the surfaces that took no material

From [A material for ui visuals, canvas.Text and the rect helpers](https://github.com/dvoyni/cog/issues/145).

`OpQueue.Sprite`, `SpriteTexture`, `DrawTriangles` and `DrawTexture` all end in
`material *gfx.MaterialDescr, params ...gfx.ParameterDescr`. `Text`, `FillRect`,
`StrokeRect` and `Line` do not, and every `ui` visual passes `nil`. A custom
shader that has to cover *everything* on screen — the actual requirement — cannot
reach them at all.

**Two rules, and everything else follows.**

1. **Canvas resolves a draw's material as draw → layer → built-in.** A draw that
   names its own material uses it; otherwise the layer's set supplies the slot for
   the family being drawn; otherwise the built-in for that family.
2. **`ui` inherits a material set down the element tree**, seeded at the roots by
   a per-frame default, replaceable by any element. `ui` records ordinary canvas
   draws, so what it inherits arrives at canvas as rule 1's first step — `ui`
   gains no second precedence rule of its own.

### `canvas.MaterialSet`

A scope names a set, because **a layer is not one family**. Every interesting
layer carries sprites *and* triangles, and the two can never be one shader, so a
scope naming a single material would be broken by construction.

```go
type MaterialSet struct {
    Sprite    *gfx.MaterialDescr
    Triangles *gfx.MaterialDescr
    Texture   *gfx.MaterialDescr
    Params    []gfx.ParameterDescr
}
```

- Three slots, mirroring the three built-ins exactly. **A nil slot keeps its
  built-in**, so a set is an *override* and never a whole-cloth requirement. An
  entirely zero set is the built-ins, which is what a scope that names none has.
- **One parameter list serves all three**, because gfx drops a name the bound
  shader never declared. A single `fade: f32` in the set reaches sprite,
  triangles and texture without being written three times.

### Canvas

- **`SetLayerMaterial(layerID Layer, set MaterialSet)`** — the fifth per-layer
  state op beside `Clear`, `SetLayerTarget` and `SetLayerTransform`. Like them it
  is **key/value applied at flush, not positional**, so a caller that runs after
  everything has been recorded still reaches draws already made. It clones its
  set into the queue's `materialArena` at call time, exactly as `Sprite` does, so
  a caller that mutates its material afterwards does not retroactively change what
  was recorded — and the **last** call in a tick wins with the values it held at
  that moment. `reset()` clears it with the layer's other per-frame state.
- **`SetMaterial(set MaterialSet)`** — the same thing over the whole queue,
  the widest scope of all, resolved after a layer's own set and before the
  built-in. **Added after this document was written**, by its first consumer:
  reaching every layer through `SetLayerMaterial` means keeping a list of every
  layer an app has, and layers are hand-picked integers spread across an app's
  packages — feuds-26's run 0…16, 101, 1000, 2000, 3000, 6000, 9001 across six
  packages. A shader over *everything on screen* is a property of the frame, not
  of a list that a new screen silently falls out of. A layer opts out by naming
  an empty set, which is what the `has` flag on the stored set is for; that is
  the backdrop that must not fade and the layer rendering into a texture.
- **`TextDraw` grows `Material *gfx.MaterialDescr` and `Params
  []gfx.ParameterDescr`.** `OpQueue.Text`'s signature does not change.
- **`FillRect`, `StrokeRect` and `Line` take a new `ShapeDraw`** carrying
  `Color`, `Thickness`, `Material` and `Params`, in the shape `TextDraw` already
  has. `FillRect` ignores `Thickness`, as `TextDraw.WrapWidth` is ignored without
  `WordWrapping`. **This is the one breaking signature change**, at five call
  sites across cog, cog-examples and feuds-26, all of which pass an explicit
  colour today.

**`ShapeDraw.Color` zero means opaque white**, matching `ui`'s `defaultTint` and
canvas's existing habit of reading a zero scale as 1. This changes `FillRect`
with a zero colour from invisible to white; no call site in the three repos
passes one. Without it `canvas.ShapeDraw{Material: fade}` draws nothing at all,
which is the likelier mistake in the world this rule creates.

**Canvas's draw entry points are deliberately not aligned into one shape.**
`Sprite`, `SpriteTexture`, `DrawTriangles` and `DrawTexture` keep
`material, params...` as trailing arguments; `Text` and the shape helpers carry
them as fields on a draw description. The two groups already differ — one takes a
transform and a path, the other takes a struct describing the whole draw — and
naming a material is rare enough that the argument list is the wrong place to pay
for it. Aligning them would have meant twenty-four call sites gaining a `nil`.

### `ui`

- **`Frame.SetMaterial(canvas.MaterialSet)`** on the per-tick frame resource,
  which is cleared every tick and so is the natural home for a value that changes
  per frame. `canvas.Config` is construction-time and cannot hold it.
- **`Element.Material(canvas.MaterialSet) Element`**, a **Modifier** in
  `CONTEXT.md`'s exact sense — "a value transformation that derives one Element
  declaration from another" — inherited down the tree precisely as `layer` is
  (`ui/layout.go:150-155`) and stamped into `State` beside `Layer` (`:819`).
- **`State` grows the set.** Every built-in visual passes the slot for what it
  draws: the sprite, nine-slice, text and fill visuals all pass `Sprite`, because
  glyphs, inline icons and fills are all sprite draws.

That covers every site with **one modifier on a menu root**, and it does so
without touching a single `*Params` struct — so the interactive payload wrappers
change not at all, and a custom `Visual`, which is a public interface, receives
the set through the `State` it already gets and picks the slot for the family it
draws. `ui` already exposes a gfx type in its public API (`SpriteParams.Filter`
is a `gfx.FilterMode`), so this is not a first crossing of that line.

A child element naming an empty set stops inheriting; the opt-out costs nothing
to have.

### A scope's material and parameters are one unit

**A draw that names its own material takes none of the scope's parameters.** The
alternative — scope parameters always applying — turns a layer into a general
parameter-injection channel, which is precisely what
[How the fade amount reaches every draw](https://github.com/dvoyni/feuds-26/issues/19)
rejected on the consumer's own side. A draw naming a material has said what it
wants.

The parameters earn their place beside the set rather than on the material
because **one material set at two values is a real case**: a cross-fade is two
layers sharing one shader at different amounts, and mutating a shared material
can only ever express one of them.

### Record time and flush time

The split matters because the batch key is computed at record time.

- **`ui`'s inheritance and its frame default are entirely record time.** `ui`
  records draws; by the time a draw reaches canvas the element's set has already
  collapsed into an ordinary per-draw material. Nothing new happens at flush and
  the batch key is untouched.
- **Only the per-layer set is flush time.** Its fingerprints are computed once
  per layer per frame — at most three, one per slot, on first use — and a draw
  carrying no material of its own adopts the layer's precomputed key. Draws that
  name a material keep their record-time fingerprint. The cost is three hashes
  per layer per frame, not one per draw.

### What a material on a glyph batch costs

Nothing, in draws. **Text is a sprite draw twice over**: glyphs and inline icons
both reach `batchEntry`, so a text material is a **sprite** material, samples a
`texture_2d_array`, and keys exactly as any other sprite does. One set applied to
a scope is one material across all of that scope's glyphs, so a screen of text
stays one batch per (font atlas, layer transform, clip, filter) — which is what it
is today. The batch key already separates glyphs from sprites without anyone
deciding it should, because it includes the texture and glyphs come from the font
atlas while sprites come from the sprite atlas.

**Text already spends both reserved names.** `TextDraw.Color` becomes the
instance tint and glyphs pass `defaultKeyColor`, so the `tint`/`keyColor` rules
apply to text unchanged and **a text material may not reclaim either name**.

### Rejected

- **A recording cursor, `SetMaterial`/`RemoveMaterial` modelled on
  `SetClip`/`RemoveClip`.** It would have closed all three holes with no
  signature change anywhere, and `ui`'s draw walk already drives the clip cursor.
  Rejected because it is **positional** — it only reaches draws recorded after
  it, so it cannot serve a caller that runs after the screens, which is the one
  caller there is — and because it would need a precedence rule against the
  explicit material anyway. The per-layer set is the same idea applied at flush,
  which is strictly more useful. Cheap to add later; nothing here forecloses it.
- **A `Material` field on every visual's `*Params` struct.** Five structs plus
  the interactive payload wrappers, and it would put the material on the visual
  rather than on the element, so it could not inherit.
- **Naming the material once per `ui.Frame.Add`.** Makes "which material" a
  property of the frame rather than of what is drawn, and a seeded inheritance
  gets the same convenience with one rule instead of two.
- **Leaving the shape helpers as material-free colour sugar**, with a caller who
  needs a material calling `Sprite` directly. A shape that cannot carry the
  shader is a hole in the contract, not a simplification.

---

## Required canvas changes

A checklist for an implementation session, in dependency order. **Items marked
(gfx) are prerequisites** and land in
[#158](https://github.com/dvoyni/cog/issues/158) before canvas can compile
against this.

**Prerequisites (gfx) —** [#158](https://github.com/dvoyni/cog/issues/158)

- Exported fingerprinting for a `[]ParameterDescr`. The triangles key cannot be
  computed without it.
- Kind-mismatch detection in `prepareParameterPlan`.
- `RawParameter[T]` with the reflection-based layout check (optional for a first
  pass; the restrictive-member-types fallback is acceptable, a bare size check is
  not).

**`canvas/builtin/canvas/`**

- Delete `sprite.wgsl`; rename `spritebatch.wgsl` onto that name.
- Rename its `BatchUniforms` struct to `CanvasUniforms`.
- Renumber it: `instances` 1 → 2, sampler/texture pair 2 → 1.
- Split out `uniforms.wgsl`, `clip.wgsl`, `spritebindings.wgsl`,
  `spritevertex.wgsl`, `trianglesbindings.wgsl`, `trianglesvertex.wgsl` per
  [What canvas publishes](#what-canvas-publishes).
- Reduce `sprite.wgsl`, `triangles.wgsl` and `texture.wgsl` to includes, any
  uniform extension, and `fs_main`; each calls `canvasClipped`.
- Every published source's header comment states what it declares, so the
  do-not-redeclare rule is readable at the point of use.

**`canvas/assets.go`**

- Drop `spriteShaderPath`; `spriteBatchShaderPath` becomes the sprite path.
- Add and export `UniformsPath`, `ClipPath`, `SpriteBindingsPath`,
  `SpriteVertexPath`, `TrianglesBindingsPath`, `TrianglesVertexPath`; promote
  `keyColorShaderPath` to `KeyColorPath`.

**`canvas/shader.go`**

- Delete `defaultMaterial`. `DefaultMaterial()` returns
  `&defaultSpriteBatchMaterial`.
- Export `TintSlot = "tint"` and `KeyColorSlot = "keyColor"` beside `TextureSlot`
  and `SamplerSlot`.
- Add `MaterialSet`.

**`canvas/batch.go`**

- `spriteBatch` gains the material, its fingerprint, and the stripped ordered
  draw-parameter names; `keyMatches` compares them; `flush` draws with the
  batch's own material and forwards the caller's parameters plus one storage
  buffer per unrecognised parameter name.
- `trianglesBatch` gains the material fingerprint and the parameter fingerprint
  (values included), stores the resolved material, and replays the op's
  parameters at flush. `unkeyed` is deleted.

**`canvas/plugin.go`**

- Delete `drawEntry` and both its call sites.
- `drawSprite` and `drawNineSlice` lose the `op.hasMaterial` branch; `hasMaterial`
  survives only as the guard that says whether there is a material to take the
  address of.
- Delete `trianglesBatchKey`'s `default: batchable = false` arm.
- `p.params` on the `Plugin` struct is now dead; delete it.
- Resolve a draw's material as draw → layer → built-in at flush, and apply the
  layer set's parameters after the draw's.

**`canvas/opqueue.go`**

- Add `SetLayerMaterial`; clone the set into `materialArena`; clear it in
  `reset()`.
- Compute and store the material fingerprint at record time, beside the existing
  `CloneTo`.
- `TextDraw` grows `Material` and `Params`.
- Add `ShapeDraw`; `FillRect`, `StrokeRect` and `Line` take it.
- Correct the `DrawTexture` doc comment at `:263` — see
  [Parameter resolution order](#parameter-resolution-order).

**`ui`**

- `Frame.SetMaterial(canvas.MaterialSet)`; cleared per tick with the rest of the
  frame.
- `Element.Material(canvas.MaterialSet) Element`, inherited as `layer` is and
  stamped into `State`.
- Each built-in visual passes the set's slot for the family it draws, and the
  fill visuals pass their `ShapeDraw` through.

**Tests**

- `TestDefaultShaderParses` is **replaced, not deleted**. It pinned the uniform
  offsets against what `drawEntry` packed and has no subject any more, but its
  guarantee has to land somewhere: the per-sprite data now travels as a Go struct
  reinterpreted as bytes, and nothing asserts the Go `SpriteInstance` matches the
  WGSL one — `TestSpriteBatchShaderParses` only lowers the source. The replacement
  is `TestSpriteInstanceMatchesTheShaderRecord`, reflecting the WGSL struct and
  comparing span, member count, names and offsets. A mismatch under the old test
  was a dropped parameter; under this one it is a wrong picture.
- `TestCustomMaterialAndParametersPassThrough` asserted that a draw naming
  `canvasTransform0` loses to canvas's own value. There is no `canvasTransform0`
  in the block any more, so it measured the fake union layout rather than the
  engine. The replacement asserts that a caller's `tint` never overrides the
  instance record.
- Drop `spriteShaderPath` from the two built-in-shader path lists.
- **The in-package `testBackend`'s `ShaderLayout` is one hand-written union for
  every shader** and puts bindings where the real shaders do not. Any assertion
  about groups or bindings must go through `naga.Parse` + `wgsl.Lower`, not
  through it.

**`canvas/README.md`**

- `:61`'s "A nil material uses the built-in sprite material" becomes true rather
  than deleted: a nil material batches the sprite into the built-in instanced
  material, which is what `DefaultMaterial()` returns.
- Add the pointer to this document, worded so a reader knows what is specified and
  what is implemented.
- Document `ShapeDraw`, `TextDraw`'s new fields, `SetLayerMaterial`,
  `MaterialSet`, the seven published sources and their constants, and the clip
  warning.

**`CONTEXT.md`**

- Generalise **Batch**; add **Canvas material**, **Material set**, **Family**,
  **Scope**, **Instance record**, **Reserved parameter name**. Glossary entries
  only — no rules, which live here.

---

## Out of scope

Recorded so nobody reopens them believing they were overlooked.

- **Texture-sourced sprite batching**, and the `#if`-switched atlas/texture shader
  variant that would allow it —
  [canvas: one sprite shader source with an atlas/texture variant](https://github.com/dvoyni/cog/issues/149).
  A second mechanism riding on this one, serving two draw sites in the only
  consuming app. The group convention makes it *easier* rather than harder: an
  `#if`-switched `canvasTexture` is `texture_2d` or `texture_2d_array` at the same
  group 1 either way, because the rule keys on the kind of binding and not on the
  type behind it.

- **App-defined per-instance properties on a built-in shader** —
  [#148](https://github.com/dvoyni/cog/issues/148). **Sharply distinct from
  extending the uniform block**, which this contract allows and the lens already
  ships: #148 is about extending the *instance record*, which needs gfx's storage
  member packing; extending the block is per-batch data through the one
  name-packed target gfx already has. The distinction is the reason the
  frozen-record rule is affordable, and confusing the two is the specific mistake
  this bullet exists to prevent.

- **Batching triangle draws that carry per-draw parameters that must vary within
  the batch** —
  [canvas: batch triangle draws that carry a custom material or per-draw parameters](https://github.com/dvoyni/cog/issues/157).
  A triangle batch is concatenated vertices with no instance index, so the sprite
  path's arrays have nothing to hang on. The interim rule is documented above;
  the cost of deferring is three draws a frame against eighteen sprite sites.

- **The gfx-side companions** —
  [#158](https://github.com/dvoyni/cog/issues/158). The rules are stated here;
  gfx is where they become true. Items 1 and 2 are prerequisites.

- **The uniform pool's growth** —
  [#159](https://github.com/dvoyni/cog/issues/159). Made permanent by the
  uniform-block decision, and its cheap answer.

- **Finishing gfx's storage-struct member packing** (#9's unfinished half). A gfx
  gap that hurts scene today, independent of anything canvas does. This
  specification names it as the **direction** — by-name packing wins — rather than
  only as a gap, and states the convergence as an intent, not a commitment.

- **Building a draw counter.** There is none in cog, the decision it would have
  informed survives its upper bound, and none is built. See
  [The arithmetic](#the-arithmetic-and-the-number-this-specification-records).

- **A scissor-rect clip.** gfx exposes none; see [The clip test](#the-clip-test).

- **Implementation.** The map that produced this document is plan-only. The
  throwaway branches `proto/sprite-collapse` in `dvoyni/cog` and
  `dvoyni/cog-examples` carry a working version of the collapse and are not to be
  merged.
