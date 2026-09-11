# scene mesh storage — specification

`github.com/dvoyni/cog/scene` holds every mesh it draws in three buffers: an
interleaved vertex array, an index array, and — for a morphed primitive — a
block of morph deltas. This document specifies **what those three buffers
contain**: which attributes a vertex carries and at what precision, how wide its
indices are, how morph deltas are stored, how a mesh's layout and the shader
variant drawing it are guaranteed to agree, what an app writes when it authors a
mesh itself, and what the bundled PBR requires of a mesh handed to it.

The contract exists because scene stored every mesh at one fixed
84-byte stride and `uint32` indices regardless of what the file shipped. That is
not an accident: `scene/mesh.go:13-21` explains the size with a reason that was
true when it was written and stopped being true when
[gfx: the WGSL shader preprocessor](https://github.com/dvoyni/cog/issues/118)
shipped and `scene.wgsl`'s 862 lines became ten shared sources
([#141](https://github.com/dvoyni/cog/issues/141),
[#142](https://github.com/dvoyni/cog/issues/142)).
With the obstacle gone, the question is what scene *should* store,
and the answer touches the vertex, the index buffer, the delta buffer, the
authoring API, and the one error that guards them.

This document is the specification the implementation is judged against. It is
assembled from the resolved tickets of
[scene: what a mesh stores — vertex precision, attribute presence, index width](https://github.com/dvoyni/cog/issues/167);
every section cites the tickets it came from. Where a claim rests on something
unverified it is marked **Gap** and says what would settle it; where assembling
these decisions next to each other settled something no ticket did, it is marked
**Settled here**.

**This specification is implemented.** It landed under
[scene: implement what a mesh stores](https://github.com/dvoyni/cog/issues/213)
across [#214](https://github.com/dvoyni/cog/issues/214) to
[#222](https://github.com/dvoyni/cog/issues/222): the gfx vertex-interface
check, the `uint16` index path, the node joint moved to the instance, the
authoring/storage split and the packer, the narrowed normal and tangent, the
per-mesh UV range, the narrowed skin with the shader's renormalisation, the two
named layouts, and the narrow sparse morph deltas.
[Required scene changes](#required-scene-changes) is the checklist that work
ran from, and is kept as the record of what changed rather than as an open list.

**What remains open is the by-eye confirmation**
([#223](https://github.com/dvoyni/cog/issues/223)). The size figures in
[What all of it is worth](#what-all-of-it-is-worth) have been re-measured by
driving the real loader over every vendored asset, which is what turned that
table's `≈` into a number; every *fidelity* claim in this document is still a
judgement by eye, because there is still no pixel readback in `gfx` or `wgpu`.
The prototypes the fidelity findings came from are throwaway branches in
`dvoyni/cog-examples` — `proto/vertex-narrow`, `measure/mesh-bytes`,
`measure/attr-precision`, `measure/morph-deltas` — which are not to be merged.

---

## Contents

- [Vocabulary](#vocabulary) · [The corpus, and what it can and cannot say](#the-corpus-and-what-it-can-and-cannot-say)
- [The two vertex records](#the-two-vertex-records) · [Per attribute](#per-attribute)
- [The per-mesh record](#the-per-mesh-record) · [Where decoding runs](#where-decoding-runs)
- [Attribute presence: two named layouts](#attribute-presence-two-named-layouts)
- [Index width](#index-width) · [Morph delta storage](#morph-delta-storage)
- [The authoring API](#the-authoring-api) · [What the bundled PBR requires](#what-the-bundled-pbr-requires)
- [What gfx enforces, and what nothing enforces](#what-gfx-enforces-and-what-nothing-enforces)
- [What all of it is worth](#what-all-of-it-is-worth)
- [Required scene changes](#required-scene-changes) · [Out of scope](#out-of-scope)

---

## Vocabulary

These words are used precisely throughout, and the ones not already in
`CONTEXT.md` are added there. They were pinned because the rules below turn on
distinctions that ordinary usage collapses — three different things in this
document could all be called "the vertex", and two different mechanisms in
different buffers were both already called "sparse".

- **Authoring vertex** — `scene.Vertex`, the Go struct an app writes. Float
  fields, 72 bytes, write-only: nothing in scene ever hands one back.
- **Storage vertex** — the bytes scene actually uploads, 32 bytes at six
  `@location`s. Derived from the authoring vertex at bake; it is what a shader
  reads and therefore public contract, not an internal detail.
- **Vertex layout** — the ordered `(offset, format)` list mapping a buffer's
  bytes to shader locations, plus the stride it implies. cog has exactly two
  blessed ones (below) and admits any number of custom ones.
- **Named layout** — one of scene's two blessed vertex layouts: the **standard
  layout** (32 bytes, six locations) and the **skinned layout** (40 bytes, eight).
  Not a family to be curated; the set has two members and is closed.
- **Custom layout** — any `VertexLayout` an app defines itself. Legal, and
  requires a custom material.
- **Per-mesh record** — 32 bytes of UV scale and bias per mesh, in a storage
  buffer, indexed from the instance. Static geometry metadata, not per-frame.
- **Sparse weight list** — which morph *targets* reach the shader for a draw,
  count-prefixed by weight. Existing mechanism, unchanged
  (`scene/morph.go:79`).
- **Sparse target** — which *vertices* a morph target stores a record for,
  addressed by a live span. New, and a different mechanism in a different buffer
  from the sparse weight list ([#178](https://github.com/dvoyni/cog/issues/178)).
- **Live span** — a target's `(first, count)`: the contiguous vertex range it
  stores records for. Records are dense within it.

---

## The corpus, and what it can and cannot say

Every number in this document comes from the fifteen glTF assets vendored under
`assets/`, first measured by
[Measure what scene stores per mesh across the vendored assets](https://github.com/dvoyni/cog/issues/168)
and re-measured on the built loader by
[#223](https://github.com/dvoyni/cog/issues/223).

**Three counts are easy to conflate and this document conflated them.** The
fifteen files hold **53 mesh primitives**. Before
[#176](https://github.com/dvoyni/cog/issues/176)'s deduplication the loader made
**61 conversions** of them — `InterpolationTest`'s cube nine times and
`CesiumMilkTruck`'s wheel twice — and **61 is what every "today" figure in this
document is counted over**, which is why it reads throughout as a primitive
count and is not one. Today it makes **52** conversions, one `POINTS` primitive
having no gfx topology, and places them at **71 instances**.

Scene held **3.77 MiB of geometry** for these assets — 3,232.9 KiB of vertices,
246.3 KiB of indices, 385.8 KiB of morph deltas, over those 61 conversions and
39,411 vertices — against **5.25 MiB of `.glb` on disk with every texture
included**. It now holds **1,356.8 KiB** over 52 conversions and 38,391
vertices; the whole table is in
[What all of it is worth](#what-all-of-it-is-worth).

Two facts about that corpus govern how hard any ratio here may be leaned on, and
they are stated up front because several sections below would read as stronger
evidence than they are:

- **These are Khronos sample models**, built to exercise glTF features rather
  than to resemble a game's content. `CompareBaseColor` alone — three synthetic
  9,216-vertex comparison grids — is **70% of the vertex bytes** and 61% of the
  whole measurement, and 50 of the 53 primitives are small feature tests. `Fox`
  and `CesiumMilkTruck` are the only two resembling authored content.
- **`#45`'s trigger has not fired.**
  [gfx: shader preprocessing and vertex variants](https://github.com/dvoyni/cog/issues/45)
  gated this whole effort on *"vertex memory is measurably a problem in a demo,
  or a third variant axis appears"*. Neither has happened. This map was picked up
  because the **obstacle** cleared, not because the **cost** bit, and
  *"change nothing"* was a live destination throughout. What the measurement
  establishes is that the waste is systematic and cheap to remove — a weaker
  argument than pressure, and the honest one.

There is also **no instrumentation of any kind** in the engine — no draw counter,
no memory counter — and **no pixel readback anywhere in `gfx` or `wgpu`**. Every
size here was arithmetic over the assets when this document was written; the
totals are now taken by driving `convertDocument` over the corpus and measuring
what the packer emits, which is an observation of the built loader but still not
of a running frame. Every **fidelity** claim is judged by eye or by offline
analysis of screenshots and that has not changed. Both limits are load-bearing
for what follows.

---

## The two vertex records

From [The authoring API for a mesh's vertex](https://github.com/dvoyni/cog/issues/174).
**The authoring type and the storage type diverge, and the divergence costs
nothing, because the copy it needs already happens.**

`BakeMesh` does not hand a caller's slice to `gfx`. `uploadBytes` reinterprets it
(`scene/mesh.go:238`) and `appendStaging` copies every byte into the staging
arena (`scene/meshbake.go:168`). Packing replaces that memcpy with a
transform-copy writing **fewer** bytes — 84n to 32n per mesh — and the traversal
it needs is one `mintMesh` already makes for `vertexBounds`, which is the same
walk the per-mesh UV range needs. One pass, already there, now doing three things.

**Authoring vertex — `scene.Vertex`, 72 bytes. Its size no longer matters.**

| field | type | bytes |
| --- | --- | ---: |
| `Position` | `m.Vec3` | 12 |
| `Normal` | `m.Vec3` | 12 |
| `Tangent` | `m.Vec4` | 16 |
| `UV0` | `m.Vec2` | 8 |
| `UV1` | `m.Vec2` | 8 |
| `Color` | `m.Color` | 16 |

`Joints` and `Weights` are **gone from the public struct**
([#173](https://github.com/dvoyni/cog/issues/173)). No public path ever wrote
them — `skin.bound` is set only from a loaded model's animation
(`scene/animpack.go:57`), and `scene/mesh.go:18` already recorded that
*"a buffer-built mesh never skins, so its last 24 bytes are dead."* The public
surface shrinks rather than growing to pay for the trim.

`Color` becomes `m.Color` rather than staying `[4]uint8`. It was the one field
where authored and stored forms coincided, and therefore the one place the rule
could have been broken; keeping bytes there would oblige a caller to know which
fields scene packs and which are raw. `m.Color` also carries a distinction
`[4]uint8` cannot — `NewColorSrgb` against `NewColorLinear` — where glTF's
`COLOR_0` is linear with nothing in the byte form to say so. `canvas.Vertex.Color`
is already `m.Color` (`canvas/types.go:16`), so the vocabulary precedent exists;
its limit is worth recording accurately, since canvas *stores* `Float32x4`
(`canvas/shader.go:148`) and is a precedent for the type, not for packing it.

**Storage vertex — the standard layout, 32 bytes at six locations.**

| loc | attribute | stored format | offset | bytes |
| ---: | --- | --- | ---: | ---: |
| 0 | `Position` | `Float32x3` | 0 | 12 |
| 1 | `Normal` | `Unorm16x2` — `oct32` | 12 | 4 |
| 2 | `Tangent` | `Uint32` — `oct` 15/15 + handedness + 1 reserved | 16 | 4 |
| 3 | `UV0` | `Unorm16x2` + per-mesh range | 20 | 4 |
| 4 | `UV1` | `Unorm16x2` + per-mesh range | 24 | 4 |
| 5 | `Color` | `Unorm8x4` | 28 | 4 |

**The skinned layout, 40 bytes at eight locations** — the same six, plus:

| loc | attribute | stored format | offset | bytes |
| ---: | --- | --- | ---: | ---: |
| 6 | `Joints` | `Uint8x4` | 32 | 4 |
| 7 | `Weights` | `Unorm8x4` | 36 | 4 |

> **Settled here.** No ticket wrote the skinned layout's offsets down.
> [#173](https://github.com/dvoyni/cog/issues/173) specifies it as *"the same six
> plus `Joints Uint8x4` and `Weights Unorm8x4`"* and leaves the arithmetic
> implied. Appending them at 32 and 36 keeps every offset 4-aligned and the
> stride a multiple of 4, which
> [Does the tangent go to two bytes, and where does the handedness bit live?](https://github.com/dvoyni/cog/issues/195)
> then made a hard requirement rather than a tidiness preference. Both named
> layouts satisfy it by construction.

**The skinned layout is internal to the glTF loader.** It never passes through
`mintMesh` and no app can author it, so it never meets the public API at all.

---

## Per attribute

From [The precision of each vertex attribute](https://github.com/dvoyni/cog/issues/171),
corrected by
[Does oct16 hold up where the first prototype could not look?](https://github.com/dvoyni/cog/issues/179)
and [#195](https://github.com/dvoyni/cog/issues/195).

Every narrow format used here is already wired end to end: `Unorm1010102`,
`Float16x2`, `Unorm16x2` and the rest map to `gputypes` at
`wgpu/gfxbackend.go:1018-1071`, and `gfx.VertexType.size()` knows their widths
(`gfx/mesh.go:56-66`). `scene.Vertex` uses **none** of them today except
`Unorm8x4` for `Color`. **Nothing has to be built in the backend for this axis.**

### `Position` — `Float32x3`, unchanged. 12 bytes.

**This is the recommendation held least confidently, and it is kept exact
deliberately.** The quantised form would be `Unorm16x4` — WebGPU has no
three-component 16-bit vertex format — saving 4 bytes with one component spare,
and the measured error is small: a median per-primitive step of **3.05e-05 world
units**, worst **2.4e-03** on `Fox`, whose z extent is 155 units.

Position is the attribute where an error is unrecoverable. A normal slightly off
shades slightly wrong; a position slightly off **moves the surface**, cracks a
seam between two primitives quantised to different ranges, and z-fights on
coplanar faces. The measurement shows that spread is real: a per-primitive AABB
beats a per-model one by **3x at the median and 130x at the worst**
(`AlphaBlendModeTest` 5.0, a 6.58 × 0.312 × 0.010 sliver inside an 8.6-unit
model), so adjacent primitives genuinely do get different steps.

Those 4 bytes are the least valuable on the table, and skipping them shrinks the
per-mesh record from 56 bytes to 32.

### `Normal` — `oct32` in `Unorm16x2`, decoded in WGSL. 4 bytes.

Octahedral, not the fetch unit's `Unorm1010102`. At an identical four bytes
`oct32` is **26x more accurate** — 0.0037° mean against 0.042° (Cigolle et al.,
JCGT 2014, via
[research: prior art in vertex compression and layout variants](https://github.com/dvoyni/cog/issues/169))
— so the fetch-unit format is dominated on the axis that matters and wins only on
shader simplicity. Two things settled it beyond the error bound:

- `Unorm1010102` is the **late-2023 spec addition that shipped silently broken on
  a wgpu GL backend for ~20 months.** With no pixel readback in this engine, that
  failure looks like a missing mesh. `Unorm16x2` is old and boring.
- An oct decode is **stateless** — no binding, no CPU-side table, nothing for
  `UpdateMesh` to keep in sync.

**The four bytes are earned on an artefact, not on a margin, and that took two
prototype tickets to establish.**
[prototype: narrow the vertex and look at it](https://github.com/dvoyni/cog/issues/172)
found no visible difference at any rung and concluded the choice was
*"a margin argument, not an artefact argument"*. That reverses:
[#179](https://github.com/dvoyni/cog/issues/179) put a structured environment in
front of it and `oct16` produced a **coherent, visible** failure — 14.09% of a
mirrored sphere moving, **2.99% of it by more than half the dynamic range**, a
hard reflected edge 2 px out of register, and a regular reflected bar pattern
returning **wavy, kinked and uneven in width**. `oct32` at the same measurement is
0.52% of pixels, peak 16/255 — nothing.

The explanation for the first prototype's null result is the part worth keeping:
**under a single sun lobe there is no structure in the incoming light for a 1.8°
wobble to bend.** The error had nothing to draw with. It was never that the eye
is blind to 0.91°.

The honest form of the finding is a **frequency threshold**, not a blanket no:
2° environment structure is destroyed outright, 4–16° structure loses only its
edges, and a smooth gradient shows almost nothing. The panorama used was
deliberately harsher than a real one can be inside eight bits, since
`gfx.TextureFormat` has no float member (`gfx/contract.go:40`).

`oct22` is **not** recorded as a rejected alternative: four bytes for worse
accuracy than `oct32`'s four was never a candidate for the stride.

### `Tangent` — one `Uint32`: oct 15/15 + handedness + 1 reserved. 4 bytes.

**Handedness has to ride in the vertex**, and the measurement is what closed it.
Every `w` in the corpus is exactly +1 or −1 and constant across a primitive in 10
of 11 — but **`WaterBottle` 0.0 carries both signs inside one primitive.** One
counter-example is enough; per-primitive handedness is dead. The *"two spare bits
carry handedness"* premise that [#169](https://github.com/dvoyni/cog/issues/169)
declined to confirm as received wisdom is adopted here as a choice.

Dropping 16 bits to 15 per octahedral axis roughly doubles the tangent's angular
error and still leaves it an order of magnitude better than `Unorm1010102`, and
the fragment stage re-orthogonalises the tangent against the normal regardless
(`material.wgsl:90`).

**The tangent does not fail the way the normal does, and it stays 4 bytes
anyway.** [#179](https://github.com/dvoyni/cog/issues/179) measured `oct16` on
the tangent as **incoherent speckle with no shape** — 0.08% of pixels past half
range against the normal's 2.99%, the same speckle over a clean normal as over a
narrowed one, so the two rungs do not interact. That is dither, and the eye
catches displaced edges rather than dither. A two-byte tangent was therefore live
on fidelity and died on packing:

> **WebGPU requires `arrayStride` to be a multiple of 4**, unconditionally
> ([validating GPUVertexBufferLayout](https://www.w3.org/TR/webgpu/#abstract-opdef-validating-gpuvertexbufferlayout)).
> A two-byte tangent gives **30** on the standard layout and **38** on the
> skinned one; both pad straight back to 32 and 40. **The saving is zero on both.**

The consequence is a **reserve rather than bytes**, and it is stated in the
useful direction: the tangent's usable floor sits near 8 bits per axis, so the
word carries roughly **14 bits of slack** beyond the one declared reserved bit,
and **what blocks a narrower tangent is packing, not fidelity**. If a partner
2-byte saving ever appears, no re-measurement is owed.

One caveat, recorded because it cuts toward keeping the tangent narrow rather
than against it: the normal map used was strong — 17° mean tilt, the condition
most favourable to the tangent mattering — and the tangent is unused entirely
when no normal map is bound.

**The consequence named rather than glossed:** normal and tangent **decode
differently** — `Unorm16x2` straight to `vec2<f32>` for one, bit extraction for
the other. That asymmetry is the price of not paying 4 more bytes for symmetry.

**And the tangent is stored for every mesh, including those that never read one.**
`generateTangents` (`scene/gltfmesh.go:336-378`) fires only when the material has
a normal map and the file shipped none, so a mesh with neither stores four bytes
of zero.
[#195](https://github.com/dvoyni/cog/issues/195) put the question directly —
how much does scene expect to be normal-mapped? — and answered **genuinely
mixed**: cog is general-purpose with one real consumer, the corpus's 11-of-53 is
worthless as evidence, and designing the vertex around either extreme is
guessing. The four bytes are a knowing cost of having one layout.

### `UV0` and `UV1` — `Unorm16x2` with a per-mesh range. 4 bytes each.

This is where the per-mesh record earns itself, and the original reasoning was
wrong in a way worth recording. `Float16x2` saves the *same* four bytes with no
metadata at all, so **the range buys accuracy rather than size** — and the gap is
not marginal:

| case | primitives | `Float16` @4096 | per-primitive `unorm16` @4096 |
| --- | ---: | ---: | ---: |
| max abs in [0.5, 1) | 21 | 2.0 texels | 0.057 |
| **touches exactly 1.0** | 8 | 4.0 texels | 0.063 |
| `MorphStressTest` 0.0 | 1 | 16.0 texels | 0.82 |
| `EmissiveStrengthTest` 1.0 | 1 | **64.0 texels** | 1.00 |

**The error is not confined to the tiled outliers.** A UV island reaching the
atlas edge touches 1.0 exactly, crosses into the `[1,2)` binade and **doubles its
half-float ULP for the entire primitive**; eight primitives do this and it is
ordinary authoring, not a mistake. Nine of the 32 primitives carrying
`TEXCOORD_0` leave 0..1 at all, and four of those leave it only downward. At 1024
the typical `Float16` cost is half a texel and invisible; at 4096 it is 2 to 4
texels, and **the engine has no say over what resolution an app ships.**

The outliers are real and they are exactly the wrap-mode tests, which is what
tiling content does: `EmissiveStrengthTest` 1.0 reaches **(18.52, −13.49)**;
`MorphStressTest` 0.0 reaches (7.12, 2.12) and −6.05.

### `Color` — `Unorm8x4`, unchanged. 4 bytes.

The one row already decided and never written down. Scene has always truncated to
8 bits per channel — `modeler.ReadColor` returns `[4]uint8` and
`gltfmesh.go:145` stores it straight — even though glTF core permits `float32`
and `unorm16` for `COLOR_0`. Widening would put precision somewhere nothing else
in the pipeline carries it: vertex colour multiplies a base colour that is an
8-bit texture in every vendored asset. **Recorded here as a choice, since it has
been shipping unremarked.**

### `Joints` — `Uint8x4`, capping a skin at 256 joints. 4 bytes.

**A design cap, not a measured one.** `Fox` is the only mesh carrying `JOINTS_0`
in all fifteen assets: max index 23 in a 24-joint skin. That is n=1 and is stated
as such. 256 is comfortably above a detailed rigged humanoid (100–200 including a
face rig), and `scene/gltfmesh.go:157` already promises per-model joint
remapping, so the cap is **per skin rather than per scene**. The failure mode is a
**hard rejection at load** — loud, not a silent fidelity loss.

> The cap means what it says only because of
> [Where does a plain animated mesh's joint live?](https://github.com/dvoyni/cog/issues/176).
> `buildJointSpace` claims every skin's joints before the walk begins
> (`scene/gltfanim.go:178-195`), so **plain** joints are appended after the skin
> block and nothing bounds their count but node count. A plain joint index ≥ 256
> written into an 8-bit field would truncate to a **different bone** — no error,
> a prop attached to the wrong thing. Moving the plain joint to the instance
> removes the class outright.

### `Weights` — `Unorm8x4`, and the shader divides by `total`. 4 bytes.

The largest single win, 12 bytes. The skin path does not renormalise today:
`deform.wgsl:39-55` accumulates `total` purely for the zero-influence check and
then trusts the raw weights to sum to one. Under `unorm8` they no longer would —
naive `round(w*255)/255` puts **202 of `Fox`'s 1,728 vertices (11.7%) off, always
by exactly ±1/255**, and `Fox`'s file weights are exactly normalised to within a
float32 ULP, so all of that drift would be scene's own doing.

`position = position / total` is three divides against a value already in a
register. It was chosen over guaranteeing the sum at bake (largest-remainder
rounding) for a reason beyond cost: **it fixes something already wrong.** A
malformed file whose weights do not sum to one is skinned silently incorrectly
today, and glTF only says producers *SHOULD* normalise. Bake-time rounding could
not cover a mesh authored through the public API, because scene would not be
doing the rounding.

**Record this as a behaviour change on existing content**, in the direction of
correctness.

### What an unwritten or non-unit value means

From [#174](https://github.com/dvoyni/cog/issues/174).

**The canonical for a zero-length normal is not a choice — it falls out of the
encoding.** `octEncode` guards `l1 == 0` and returns the octahedral origin;
`quantizeUnorm` maps that to mid-range; the decode of mid-range is `(0, 0, 1)`.
So an unwritten `Normal` stores as **+Z** — no NaN, no invented constant.
`Tangent` behaves the same, with `+1` handedness from `w = 0`.

**This is visible, and it was not only a safety property.** Framed above as
"no NaN", the +Z canonical also *changes how normal-less geometry shades*, and
the corpus contains some: every one of `MeshPrimitiveModes`' seven primitives
carries `POSITION` and nothing else. Before, an unwritten `Normal` was
`m.Vec3{}` — `(0, 0, 0)` reached the shader, every dot product with it vanished,
and those primitives drew **black**. They now decode to +Z, are lit like any
other surface, and draw **light grey**.

Found by capture under
[#223](https://github.com/dvoyni/cog/issues/223), and it is the only difference
above quantisation noise anywhere in the vendored set: in the `loading` demo,
differencing the pre-change build against the narrowed one at a fixed pose, the
line and strip primitives of that one station account for **363 of the 1,306
pixels** that differ by more than one code, and the other 943 are the HUD's own
counters.

It is an improvement rather than a regression — lit geometry is the better
answer for a file that declined to say, and nothing in the corpus depended on
the black — but it is a **behaviour change on existing content**, not a
transparent one, and an app relying on normal-less geometry rendering unlit will
see it. Recorded here rather than absorbed.

**There is no absence sentinel.** `Tangent`'s reserved bit could have carried
one; `Normal`'s `oct32` has no spare bit pattern at all and could not. Buying an
asymmetry — a tangent that can say *absent* and a normal that cannot — was
rejected because the case does not exist: the loader generates missing tangents
at load, and no authoring consumer distinguishes "no tangent" from "zero
tangent". **The reserved bit stays reserved.**

**Magnitude is discarded, silently, as contract.** `octEncode` divides by the L1
norm, so a normal of length 2 encodes identically to the same direction at length
1. `Normal` and `Tangent.XYZ` are **directions**; length is not stored and is
unrecoverable after bake. Nothing is reported, and reporting was considered and
rejected: a check would fire on correct code. `unitmesh.go:155` writes
`Normal: position`, unit only because the radius is 1, and any author computing a
normal from a cross product is one float of rounding from a spurious error.
`validateMesh` rejects only geometry that *could only draw garbage*, and a
length-1.0001 normal draws correctly.

---

## The per-mesh record

**Scene admits per-mesh vertex metadata.** The instance's spare `vec2<u32>`
(`instance.wgsl:12`) carries a mesh index into a new per-mesh storage buffer at
`@group(0) @binding(3)`, the first free binding in group 0. `SceneInstance` stays
64 bytes.

The record is **32 bytes: `uv0` scale and bias, `uv1` scale and bias.**

- **Slot 0 is a reserved identity record** — scale 1, bias 0. A custom-layout
  mesh, or a standard mesh with no UVs, points at slot 0 and the dequantisation
  is a branchless no-op. Chosen over a validity flag in the instance's `flags`
  word: a draw-uniform branch is cheap, but an identity record is **free** and
  needs no flag bit, no branch and no second path to test.
- **A zero-width range stores scale 0 and bias = the constant**, which decodes
  exactly right with the same arithmetic and no special case at draw time. Not
  hypothetical: `CesiumMilkTruck` 1.1 and 1.2 have **UV0 collapsed to the single
  point (0, 1)**, and scene's own `unitmesh.go` leaves `UV1` unwritten on both
  unit meshes.
- **The ranges cost no new traversal.** `readAttribute` already visits every UV
  element on the glTF path, and `vertexBounds` (`scene/mesh.go:202-206`) already
  walks every vertex on the authoring path.
- **The range is derived, never surfaced, and recomputed on update.** A mesh's UV
  precision depends on the spread of the UVs in that same bake, and that is a
  property of the mesh rather than of the API. Letting an author supply one would
  add a parameter to `BakeMesh`, `UpdateMesh` **and** `TemporaryMesh`; no
  standard-layout mesh is updated anywhere in either repo today, so the
  "an update silently changes precision" case has no consumer to surprise.

> **Settled here — the instance record is now full.**
> [#171](https://github.com/dvoyni/cog/issues/171) spent one of `SceneInstance`'s
> two spare `u32`s on the per-mesh index and
> [#176](https://github.com/dvoyni/cog/issues/176) spent the other on the node
> joint. Neither ticket could see the other's spend. **`sceneInstance` is fully
> allocated at 64 bytes**, and the next spender pays 128 bytes or a repack.
> `Spare`'s comment asks the next spender to see what it is spending; that
> comment now has nothing left to offer and should say so.

> **Settled here — the per-mesh record and the morph block header are different
> mechanisms and must not be merged.** Both are "static geometry metadata in a
> storage buffer", and a reader arriving at them separately will propose folding
> them. [#178](https://github.com/dvoyni/cog/issues/178) decided the morph ranges
> live at the head of the primitive's own delta block precisely because the
> per-mesh record costs bytes on **every** mesh whether or not it morphs. The
> per-mesh record is indexed per instance; the morph header is reached through
> `morphBase`, which already points there.

---

## Where decoding runs

**Dequantisation runs at the top of the vertex stage, before morph and before
skin.** It cannot be folded anywhere cheaper, and this was checked rather than
assumed:

- **Not into the world matrix.** `sceneWorldNormal` derives its basis from that
  same matrix, so a fake non-uniform scale would counter-scale every normal.
- **Not into the joint matrices.** Those are per model; a quantisation range is
  per primitive.
- **Not after the deform.** Morph deltas are added, and joints applied, in mesh
  space.

**The decode ships as a published includable source**, following the precedent
`canvas/builtin/canvas/keycolor.wgsl` set — extracted precisely so three copies
became one include. Scene gains the equivalent at
`builtin/scene/vertexdecode.wgsl`, exported as `VertexDecodePath`.

> **Settled here.** [#171](https://github.com/dvoyni/cog/issues/171) committed to
> publishing the decode and never named the file or the constant.
> `VertexDecodePath = "builtin/scene/vertexdecode.wgsl"` matches
> `canvas.KeyColorPath`'s shape (`canvas/assets.go:65`) and
> `scene/shadersplit_test.go`'s existing glob over `builtin/scene/*.wgsl` covers
> it with no test change.

That commitment only pays for itself if what it decodes is something an author is
allowed to rely on — which is why the storage layout above is **contract**, not
an internal detail. `repaint.go:31`'s note stays true and stays the only
direction that works: **a shader may read fewer attributes than the pipeline's
vertex layout supplies, never more.**

---

## Attribute presence: two named layouts

From [Does presence-trimming earn its variants?](https://github.com/dvoyni/cog/issues/173).
**No, except for the one pair that costs no variants — and that is the whole
axis's answer.**

Measured against the 32/40-byte stride rather than today's 84, presence is worth
**703.6 KiB of 1,539.5 KiB**, and it splits sharply:

| attribute | primitives carrying | bytes not stored | share | variant cost |
| --- | ---: | ---: | ---: | --- |
| `JOINTS_0`+`WEIGHTS_0` | 1 / 53 | **294.4 KiB** | 41.8% | **none** |
| `TEXCOORD_1` | 2 / 53 | 148.0 KiB | 21.0% | one define, ×2 |
| `TANGENT` | 11 / 53 | 143.4 KiB | 20.4% | one define, ×2 |
| `COLOR_0` | 2 / 53 | 117.9 KiB | 16.8% | one define, ×2 |

> The counts are over the corpus's 53 authored primitives and the byte columns
> over the 39,411 pre-deduplication vertices, which is the arithmetic the
> decision was taken on and is left as taken. Both denominators read `61` until
> [#223](https://github.com/dvoyni/cog/issues/223) separated the primitive count
> from the conversion count; nothing in the columns moves, because a duplicate
> conversion carries the same attributes as its original.

**The three that would multiply the variants are worth 409.3 KiB between them,
for 32 variants where there are 4.** That is the whole case and it fails on its
own numbers.

**Charting precision first is what produced that answer.** At the 84-byte stride
the same axis was worth 1.83 MiB; precision cut the absolute prize by **61%** and
left the variant cost exactly where it was. Had the axes been charted the other
way round, presence would have looked like half the prize.

### The cut runs along a seam WGSL already has

`SceneVertexIn` declares `@location(6)`/`@location(7)` only under
`//#if SCENE_SKIN` (`scene/builtin/scene/vertex.wgsl:20-23`). The Go side has
never honoured that cut. **So this is not a new axis at all; it is the Go side
stopping disagreeing with WGSL.** No new define, no new variant, no new
pipeline-key field — `pipelineKey` already carries `layout vertexLayoutKey`
(`gfx/translate.go:33`).

### How layout and variant are guaranteed to agree

**The mismatch is unrepresentable, not detected.** Layout and variant are derived
from the same load-time fact, so no path can pair a 32-byte layout with
`variantSkin`. Nothing new is checked at draw time and nothing new is reported.
Two collisions had to be cleared to make that true, and neither costs a variant:

- **`variantFor` moves from the model to the primitive.**
  `owned.variants[variantFor(skin.bound, skin.morphed)]` (`scene/model.go:297`)
  reads the *model's* skin, so every primitive of an animated model draws under
  `variantSkin` today and `SCENE_NOSKIN` is the per-instance flag that makes the
  unskinned ones behave. That is the correct cardinality regardless of this axis
  — what a draw deforms is the primitive's business — and it also drops the
  group-2 storage bindings from the static primitives of an animated model.
- **The rule is a union over the walk, not a per-node key.** A converted geometry
  takes the skinned layout **iff any placement draws it under `SCENE_SKIN`**.
  Computed once per geometry rather than per placement, so
  [#176](https://github.com/dvoyni/cog/issues/176)'s deduplication survives
  intact.

### The plain-joint tax, stated

**Plain-bound geometry keeps the skinned layout, zeros and all.** After
deduplication that is 852 vertices — `InterpolationTest`'s single surviving cube
and `CesiumMilkTruck`'s single surviving wheel — **6.66 KiB measured, 2.3% of
the saving**, against the 6.7 KiB this projected.
The alternative is splitting `SCENE_SKIN` into a pose define and a vertex-skin
define, which buys those 6.66 KiB for a fifth and sixth variant and reopens a
question [#176](https://github.com/dvoyni/cog/issues/176) closed.
`SCENE_PLAINJOINT` stays an instance flag; **the variants stay four**
(`variantStatic`, `variantSkin`, `variantMorph`, `variantSkinMorph`,
`scene/material.go:353-361`).

### Content occupies five signatures, and that is not evidence

Across the corpus's 53 primitives there are five distinct optional-attribute
signatures and
**every one carries at most one optional attribute**. That is *not* evidence that
content is one-hot — it is evidence that the corpus is fifteen glTF feature
tests. **Real content correlates hard**: a normal-mapped skinned character is
`TANGENT`+`JOINTS_0` in one primitive, and this set has no such asset. What the
table does establish is that a define per attribute prices sixteen corners to
serve five, independent of which five.

### Where the node joint went

From [#176](https://github.com/dvoyni/cog/issues/176). `skinBinding{skin, joint}`
was **two concepts sharing a name**, keyed together in `geometryKey` because they
were named together:

- a **skin binding** *remaps* indices the file authored, so it genuinely rewrites
  the vertex buffer and belongs to the geometry. Stays in `geometryKey`.
- a **node joint** *overwrites* every vertex with a value the geometry has no
  opinion about, and belongs to the placement. Leaves the key entirely, into
  `SceneInstance`'s second spare `u32`.

**It saves no vertex bytes by itself**, and what it saves is on an axis nothing
else on this map touches: **duplicate conversions**, which scale with *node
count* rather than vertex count. `InterpolationTest`'s **nine byte-identical
copies of one 24-vertex cube collapse to one**; `CesiumMilkTruck`'s two wheels
stop being two 76.9 KiB copies. 93.8 KiB across the corpus, unbounded in general.

**The consequence with teeth:** `loadedPrimitive.skinned` currently drives the
`sceneNoSkin` flag (`scene/pack.go:252`) and `neverCull` (`scene/model.go:342`).
Once the joint leaves the vertex, the same geometry can be drawn plain-bound
under one node and statically under another, so *"is this draw skinned"* stops
being answerable from the primitive. **Both reads move to the placement, and
`neverCull` moves with them.** A shared primitive culled by a static instance's
bounds while its animated instance walks out of them is a mesh that disappears
with nothing reported.

---

## Index width

From [Index width: does gfx gain a uint16 path?](https://github.com/dvoyni/cog/issues/170).
**`gfx` gains a `uint16` index path, and nobody chooses the width: it follows
from the vertex count, `O(1)`, on every path.**

Exactly two widths exist, `uint16` and `uint32`, fixed by the platform rather
than chosen — **WebGPU has no `uint8` index format**, which is why the 11
primitives that shipped `uint8` cannot be stored as authored.

> **A mesh with `vertexCount <= 65535` gets `uint16` indices. Every other mesh
> gets `uint32`.**

**No pass over the indices is needed**, because every index is already guaranteed
below the vertex count — `validateMesh` (`scene/mesh.go:213`) enforces it on the
authoring path and the glTF path has it by construction. That matters:
`bakeModelGeometry` (`scene/modeltable.go:335`) never calls `mintMesh`, so a
max-index scan would have been **new `O(n)` load-time work** where a vertex-count
test is a comparison.

**The threshold is 65535, not 65536.** `0xFFFF` is WebGPU's primitive-restart
value for a `uint16` strip, and indexed strips stay legal, so one vertex of
headroom removes the special case entirely rather than documenting it.

### Why, given the saving is small

Indices were **246.3 KiB of 3.77 MiB**. `uint16` is legal for **every**
primitive in the corpus — the largest is `CompareBaseColor`'s 9,216-vertex
grid — so all of them halve. Measured on the built loader that is **118.1 KiB, 3.1% of
geometry as it was**, and 8.7% of what remains now that both vertex axes have
landed. `CompareBaseColor` is 46% of that on its own. What decides it is not the
byte count:

- **The width is thrown away far more often than "glTF ships `uint16`" suggests.**
  Of the corpus's 53 primitives, **11 shipped `uint8`**, 38 shipped `uint16`, 3
  shipped `uint32`, and one — `Fox` — carries no index accessor. **49 of the 52
  authored index buffers chose a narrow width and scene widened it** — 11 of
  them fourfold.
- **The derivation is free and there is no judgement to get wrong.** An index is
  exact or it is broken; unlike everything else on this map there is no fidelity
  call, no variant cost, and nothing to look at.

> **Corrected here.** This section read *"19 shipped `uint8`, 32 shipped
> `uint16`, 3 shipped `uint32`, and 7 carry no index accessor"* over *"61
> primitives"*, and *"51 of the 54 authored index buffers"*. Those numbers are
> per **conversion** and not per primitive: nine of the 19 `uint8` entries are
> the same `InterpolationTest` cube. Re-counting the accessors off the files
> also moves six primitives out of *no index accessor* and into `uint16` — only
> `Fox` ships unindexed. The corrected counts are above; the byte figures the
> decision rested on are unaffected, because a duplicate conversion re-used the
> same authored width.
>
> One more gap between the two counts, found the same way: the corpus's authored
> index accessors total **229.4 KiB** at `uint32` while the loader stored
> 236.2 KiB of them post-deduplication. The 6.8 KiB difference is `Fox`, which
> ships no index accessor and which the loader gives a **sequential identity
> index buffer** of its 1,728 vertices, plus the 21 indices
> `MeshPrimitiveModes`' strip, fan and line-loop primitives gain when they are
> expanded into lists. So *"carries no index accessor"* describes the file and
> not what scene uploads.

*"Keep `uint32`" was defensible* and is recorded as the option not taken, not as
an option that was wrong. No width available to WebGPU restores what the `uint8`
files shipped; a `uint16` path halves their widening from fourfold to twofold,
saving 366 B on `AlphaBlendModeTest` and 84 B on `InterpolationTest` — 660 B
before deduplication collapsed its nine cubes to one.

### Scope: durable geometry only

`indexBytes` (`scene/mesh.go:307`) is a **zero-copy reinterpret** of `[]uint32`
today. Narrowing makes it an allocating `O(n)` conversion — paid once for a
durable mesh, but **every frame** for `TemporaryMesh`, which is re-minted per
frame by design and which `cmd/scene/procedural` exercises.

**So the glTF loader and `BakeMesh`/`UpdateMesh` narrow; `TemporaryMesh` stays
`uint32`.** This captures the entire measured saving — every vendored asset is a
model load — and draws the line where the cost changes character.

> **Settled here — the temporary-mesh carve-out is asymmetric, and the asymmetry
> is correct.** Indices carve `TemporaryMesh` out; **vertex packing does not**
> ([#171](https://github.com/dvoyni/cog/issues/171) settled the no-carve-out
> rule, [#174](https://github.com/dvoyni/cog/issues/174) confirmed it). Read side
> by side that looks inconsistent, and the reason it is not is that the two costs
> differ in kind: narrowing indices **adds** an `O(n)` pass that does not exist
> today, while packing vertices **replaces** an `O(n)` memcpy that already runs
> (`appendStaging`, `scene/meshbake.go:168`). A vertex carve-out is also not free
> the way an index one is — a second stride for the same `scene.Vertex` means two
> vertex layouts, two `pipelineKey` entries and two shader variants, which is
> precisely the multiplication this axis exists to avoid. There is no
> `TemporaryMesh` in either repo using `scene.Vertex`; the one call site,
> `cmd/scene/procedural/main.go:515`, ships a custom 36-byte layout.

### Two mechanics worth stating

**The index format enters `pipelineKey` as the *strip* format** — the same
`nil`/`Uint16`/`Uint32` that `stripIndexFormat` (`wgpu/gfxbackend.go:991`) hands
the pipeline descriptor, `nil` for every non-strip topology. Keying on the width
unconditionally would build two identical pipelines for two triangle lists that
differ only in an encoding detail the pipeline never sees. Keyed this way it
splits nothing today: scene expands every glTF strip into a list
(`scene/gltfmesh.go:198`), and the only strip in either repo is non-indexed.
Forbidding indexed strips outright was rejected — it deletes a working
combination to save one nil-valued field.

**The width is re-derived on every bake, not fixed for a ref's life.**
`UpdateMesh` freezes the *layout* (`scene/meshbake.go:113`) because layout is part
of a mesh's contract with a material. Width is not; for a triangle list it never
reaches the pipeline at all. A mesh updated past 65535 vertices simply widens,
alongside `indexCount` and `bounds`, which `UpdateMesh` already re-derives.

---

## Morph delta storage

From [Do morph deltas follow the vertex's precision answer?](https://github.com/dvoyni/cog/issues/178).
**Deltas do not follow the vertex's precision answer, and precision was not the
question.** They narrow to a quarter of their width *and* stop storing the 93% of
themselves that is exactly zero: **385.8 KiB to 18.8 KiB as built, 4.9% of
today** — 18.6 KiB projected, and the difference is accounted for below.

The ticket asked precision; the measurement found precision is the smaller of two
independent axes in the same buffer, **by 7x**, and that the precision answer
*depends* on the sparsity answer.

| scheme | total | of today |
| --- | ---: | ---: |
| `vec4<f32>`, dense (today) | 385.8 KiB | 100% |
| narrowing alone | 144.6 KiB | 37.5% |
| sparsity alone | 49.6 KiB | 12.9% |
| **both** | **18.6 KiB** | **4.8%** |

**93.0% of the delta store is exactly zero** — 22,972 records of 24,704.
`MorphStressTest` mesh 0/prim 0 is 100% zero: eight targets, 6 KiB, moving
nothing. No file uses a glTF sparse accessor, so the zeros are dense in the
source too; **scene is not inflating them, it is inheriting them.** And the live
records are strikingly local: every one of `MorphStressTest` 0/1's eight targets
moves exactly **115 vertices inside a span of 187, in 16 runs**, out of 1,504.

### Half float loses, and the prediction that it would win was wrong

[#172](https://github.com/dvoyni/cog/issues/172) predicted the vertex's reasoning
would invert — *"a morph delta is a displacement, not a coordinate, so its
magnitudes cluster near zero... the regime where a half float is at its best"*.
**It does not invert.** Worst-case accumulated position error as a fraction of
the primitive's AABB diagonal:

| | worst | bytes |
| --- | ---: | ---: |
| `snorm16x3` + per-primitive range | 6.0e-06 | 8 |
| `f16x3` | 2.96e-04 | 8 |
| `snorm8x3` + per-primitive range | 5.8e-04 | 4 |

`f16` is **dominated**: 50x worse than `snorm16` at the same 8 bytes, and only
2.4x better than `snorm8` at half the size. Half float's advantage is dynamic
range, and a target's deltas have none — the non-zero deltas **fill** their
range; the only small values are the zeros, which are exact in every candidate
and which the sparsity decision stops storing anyway.

### The layout

`sceneMorphDeltas` becomes `array<u32>`. One block per morphed primitive at
`morphBase`:

```
block header
  ranges     3 x f32 per present slot, per axis, bitcast  (12 / 24 / 36 B)
  per target (base, first, count)                          (12 B each)
records
  target t holds count_t records; vertex v sits at
  base_t + (v - first_t) * recordBytes,  when first_t <= v < first_t + count_t
```

One record, fixed slot order, prefix mask:

| slot | stored | bytes | decode |
| --- | --- | ---: | --- |
| position | 3 × `snorm16` + 16 reserved bits | 8 | `unpack2x16snorm` ×2, × range |
| normal | 3 × `snorm8` + 8 reserved bits | 4 | `unpack4x8snorm`, × range |
| tangent | 3 × `snorm8` + 8 reserved bits | 4 | `unpack4x8snorm`, × range |

**A width per slot, not one width for all three.** Ranges differ by **60x**
between slots on the same primitive (`MorphStressTest` position range 1, normal
range 0.0177) and the errors differ in kind — a position error is a displacement,
a normal error is an angle. At `snorm8` the normal-delta error is at worst
2.2e-03 on a unit normal, **~0.13°**, against the `oct16` rung
[#172](https://github.com/dvoyni/cog/issues/172) stared at (0.32° mean, 0.91°
max) and could not see.

**The invariant this looks like it breaks does not break.** The `sceneAnim`
header carries `morphStride` and no mask, and its layout is fixed, so **which
slots a record holds has to be recoverable from the stride alone**. With a fixed
prefix mask and fixed per-slot widths the prefix sums stay distinct — **8 / 12 /
16 bytes** rather than 16 / 32 / 48 — and each slot's offset inside a record stays
a compile-time constant. All three builtins exist in naga
(`wgsl/internal/lower/lower.go:11507-11511`).

### The mask, which the record layout depends on

Moved here from `scene.md`, because it is part of the same layout and describing
it in two documents is how they drift.

**The mask is per primitive**: the union across that primitive's targets,
intersected with the base primitive's **authored** attributes. Per-target masks
would make the stride vary *within* a block, so a record's slot offsets would
stop being compile-time constants. Intersecting against authored attributes
matters because scene generates flat normals for a primitive that has none —
those are scene's reconstruction, not the asset's, so a NORMAL delta on a
primitive with no authored NORMAL is dropped at load.

**The mask is then widened to a prefix** of position, normal, tangent, so a gap
is stored as explicit zeros rather than closed up — which is what makes the
stride alone say which slots are present. The gap this fills is a target that
deforms the normal and not the position: without the widening its 4-byte record
would be read as a position delta. It costs 8 bytes of zeros per *stored* record
in a case almost no file has, against dropping authored data or spending a
reserved header word every draw would then read.

**It cannot be larger than today for any primitive with a surface to morph.** At
position-only the block is `12 + 12T + 8TV` bytes against today's `16TV`, so the
crossover is `12 + 12T <= 8TV`: satisfied for every `T` at **three vertices or
more**, and from four targets up at two. Below that is a point- or line-mode
primitive, which has no surface for a shape to deform. *(The first statement of
this said "every `V >= 2`", which the inequality does not give at one to three
targets; the corrected bound is above and was checked against the packer.)*

### A target stores only the vertices it moves

`first` and `count` per target, records dense within the span; the shader tests
`v >= first && v < first + count` and indexes directly. **One compare, no search,
no extra load.**

Runs (3.2% of today) and a per-record vertex index (3.9%) are both cheaper in
bytes and both need a walk or a binary search **inside the per-active-target
loop**. They buy 6.1 KiB and 3.5 KiB across the entire corpus and pay for it in
the innermost loop in the engine — which is `morph.wgsl:16`'s own standard,
*"memory bought with per-vertex bandwidth, the wrong direction"*, applied
consistently rather than only against tight packing.

The span is at **record granularity**, not per slot. Per-slot spans would recover
the cross-slot waste the measurement reports as `disagree` — ~18% of live records
— for triple the metadata and three compares.

**The ALU falls with the bytes**, which is what `morph.wgsl:16`'s standard
actually cares about: 93% of (vertex, active target) pairs were loading a record
and adding three weighted zeros, and become one compare.

### Four rules that follow

- **`targetStride` stops being a constant**, so
  `morphBase + target * targetStride + vertexIndex * morphStride`
  (`morph.wgsl:65`) can no longer work. Every target needs its own base; the
  header is `(base, first, count)`, 12 bytes per target. **The `sceneAnim`
  header's `targetStride` word is freed** by this, and is reserved rather than
  reclaimed: the header stays two vec4s because `animOffset` counts vec4s.
- **A target that moves nothing keeps its slot.** `MorphStressTest` 0/0's eight
  all-zero targets are tempting to drop at load and cannot be: `MorphWeights` is
  **positional** over the flattened slot list (`scene.md:1243`), so removing one
  silently renumbers every slot after it. It keeps the slot, stores no records,
  `count = 0`, and costs its 12-byte header. **The 64-active-target cap does not
  move.**
- **Spare bits stay reserved.** 16 in the position slot, 8 in each of normal and
  tangent. Absence is expressed by the span, not by a value, so the sentinel a
  narrowed format would otherwise want is structurally unnecessary.
- **The loader never reorders vertices to tighten spans.** This is the first
  thing a reader will propose, so it is recorded as a decision rather than an
  omission. With eight targets you cannot make all eight contiguous at once, so
  it is a heuristic with no bound; it rewrites the index buffer; and it collides
  with both the unweld permutation (`scene/gltfmorph.go:143`) and
  [#176](https://github.com/dvoyni/cog/issues/176)'s deduplication, each of which
  already reorders for its own reasons. **The scheme degrades to today's density
  when locality is absent**, which is a guaranteed floor; a reordering heuristic
  would trade it for an unbounded one.

> **Gap — n is small and one asset is 97% of it.** Three assets, four morphed
> primitives, and `MorphStressTest` 0/1 alone is 376 of the 385.8 KiB. It is a
> synthetic conformance stress test: the 93% zero density and the 12% span are
> largely *its* numbers. The direction is what one expects of real shape keys — a
> blend shape moves a region of a face, not the whole head — but that is an
> expectation, not a measurement. **The failure mode is bounded**, which is why
> it is still the right call: a scattered asset gets spans covering its
> primitive and lands at today's density plus 12 bytes per target. **The scheme
> cannot lose; it can only fail to win.**

> **Measured as built.** Over the three vendored morphed files the packer emits
> **18.8 KiB against 385.8 KiB — 4.88%**, against the 18.6 KiB / 4.8% projected
> above; the difference is the per-slot range words and the per-target headers
> the projection rounded away. `MorphStressTest` 0/1's eight targets each span
> **187** of 1,504 vertices, and 0/0's eight all-zero targets store 0 records for
> 120 bytes of header. Fully-zero records are **92.2%** of the float store
> counted whole, against the 93.0% the projection counted. Worst position error
> over the real deltas is **0.35 of a code**, 1.06e-05 of the delta range's own
> diagonal; worst normal error is half a code.

---

## The authoring API

From [#174](https://github.com/dvoyni/cog/issues/174). **One exported layout, one
generic entry point, no raw path, no new parameter on any signature.**

An app writes `scene.Vertex` in the same `m.Vec3`/`m.Vec4`/`m.Vec2` types it
writes today, and gets 32 bytes instead of 84. **Scene owns every stored byte and
packs at bake.**

**Why the authoring type is not the storage type**, beyond ergonomics — two
structural reasons:

- **The per-mesh UV range is not per-vertex.** A packed-value API would oblige
  the caller to hand a mesh-level range over alongside the vertices, and there is
  nowhere in `BakeMesh(vertices, indices, topology)` to put it. The converting
  API needs no such parameter.
- **Every authoring call site writes a partial vertex.** `obeliskMesh` writes
  `scene.Vertex{Position: corner, Normal: normal}` and leaves the rest zero
  (`cameras/obelisk.go:220`); `unitmesh.go:86,153` writes five fields of six.
  Under a packed API each becomes an encoder call.

**A raw path for callers holding packed bytes is declined, and the reason is that
it has no consumer.** The glTF loader is the only holder of packed bytes and it
never calls `mintMesh` — `bakeModelGeometry` bypasses the minting path entirely.
A raw path would be public API with zero callers, so it is recorded as declined
rather than left to look like an omission.

**Nothing in scene ever hands a `Vertex` back.** There is no mesh readback API,
and `UpdateMesh` takes vertices without returning any. So the worry that *"a
caller reading such a struct back has no way to know which field is
authoritative"* cannot arise: there is exactly one authoritative form, the
authored one, and it flows one way.

### Dispatch, on a flag that already exists

`VertexLayout`'s contract says the returned attributes *"must match both the
struct's memory layout and the vertex inputs of the material"*
(`scene/mesh.go:68-74`). That stays true for every custom layout and becomes
**false for `scene.Vertex` alone**, whose method reports the 32-byte storage
layout while its Go struct is 72.

The dispatch is `standard`, which `layoutCache.resolve` already computes by type
comparison (`scene/mesh.go:147`) and which already gates two things: whether a
bounding sphere is computed, and whether the bundled PBR accepts the mesh.
Packing hangs off the same flag.

A separate non-generic surface for the standard vertex was rejected on
arithmetic: it means a parallel twin of `BakeMesh`, `UpdateMesh` and
`TemporaryMesh` — **six functions where there are three** — to preserve a sentence
of documentation.

> **Settled here — `standardVertexLayout` stops being derivable from the struct.**
> It is built today from `unsafe.Offsetof` over `Vertex`'s fields
> (`scene/mesh.go:40-48`). Under the divergence those offsets are the *authoring*
> struct's — 0, 12, 24, 40, 48, 56 — and only the first two coincide with the
> storage offsets 0, 12, 16, 20, 24, 28. **The layout becomes hand-written
> constants**, and the `unsafe` import may leave `mesh.go` if nothing else uses
> it. No ticket says this outright; it falls out of
> [#174](https://github.com/dvoyni/cog/issues/174) Q4 and would otherwise be
> discovered by an implementation session at the point of writing a wrong test.

### One hazard leaves the building

[#172](https://github.com/dvoyni/cog/issues/172) established that `gfx` derives
the stride as the largest attribute end offset while scene uploads
`unsafe.Sizeof`, so a layout whose last attribute does not end exactly on the
struct's size is **read at a different stride than it was written at, silently.**
Under the divergence scene never reinterprets `scene.Vertex`: scene computes the
stride and writes the bytes, and they agree by construction. **The standard
vertex leaves that hazard class entirely.** It remains live, and still unchecked,
for every custom layout — see below.

### `TemporaryMesh` with `scene.Vertex` pays an O(n) pack per frame

Named as a cost to state rather than to design around. There is still no such
call site: the one `TemporaryMesh` caller uses a custom layout.

---

## What the bundled PBR requires

**A mesh handed to the bundled PBR must use one of the two named layouts.** A
custom layout requires a custom material.

`ErrMeshCustomLayoutNeedsMaterial` (`scene/err.go:146`) keeps its mechanism — a
type comparison on the `standard` flag, so nothing about it gets harder — and
**loses its prose**. Its text claims the bundled material *"has one vertex stage
and no entry-point selection, so its inputs are a subset of `scene.Vertex`'s
eight attributes and nothing else"*. Three things in that sentence are now wrong:
there are variants rather than one stage, there are two blessed layouts, and
neither has eight attributes. It restates in terms of **a layout the bundled PBR
does not know**, and its guard is unchanged.

The error is kept even though it is redundant in *coverage* — a custom layout
drawn with the bundled PBR reaches `gfx` as a shader declaring locations the
layout does not supply, which `gfx`'s own check catches. It survives for a
strictly better message: it names a mesh id and tells the author what to do
(*"give the draw a Material"*), where `gfx`'s check can only name a shader label
and a location. It is also free, and it fires in scene before the draw ever
reaches `gfx`.

**The reverse pairing is fine and stays fine.** A standard layout with a custom
material is a pairing scene explicitly blesses — `cmd/scene/cameras/obelisk.go`
and `cmd/scene/loading/repaint.go` both ship one. So is a variant declaring six
of the eight attributes the mesh supplies. **The direction that fails is a shader
input no attribute supplies, never the other way round.**

### What breaks, exhaustively

- **`scene/unitmesh.go:91,158`** — `Color` changes type. Two lines;
  `[4]uint8{255,255,255,255}` becomes `m.White`.
- **`cmd/scene/cameras/obelisk.go:113` and `cmd/scene/loading/repaint.go:85`** —
  custom shaders declaring `@location(1) normal: vec3<f32>` against what becomes
  a `Unorm16x2` oct normal. They include `VertexDecodePath` and read
  `vec2<f32>`. **This is a real, shipping mismatch**, and it is the one that made
  `gfx`'s check load-bearing.
- **Nothing else.** `cmd/scene/cameras/main.go:548` bakes `obeliskMesh`'s output
  and compiles unchanged; `cmd/scene/procedural` is a custom layout with a custom
  material throughout and is untouched.

**The objection on record is answered by construction, not by design.**
`obelisk.go:195` chose the 84-byte vertex because *"A custom layout would oblige
every consumer of this mesh to match it."* There is still exactly **one** layout
an author can write, and it is authored in the same Go it is authored in today.

---

## What gfx enforces, and what nothing enforces

Three checks over three different pairs of numbers, in two homes. They are listed
together because they are easy to conflate and each catches something the others
cannot.

**1. Shader inputs against the bound layout — in `gfx`, exact, loud.**
From [Should gfx check a shader's vertex inputs against the layout bound to it?](https://github.com/dvoyni/cog/issues/177).
The ticket hoped the platform already rejected `vec3<f32>` against `Unorm16x2`.
**It does not, and nothing else in the stack does either** — `gogpu/wgpu`
performs no vertex-interface validation of any kind, the software rasterizer
states it outright (`hal/software/draw.go:655`: `if !found { continue }`, the
input keeps its zero value), and WebGPU itself fills missing components with
`(0,0,0,1)`. So `vec3<f32>` over an oct pair yields `(x, y, 0)` — a plausible
unit-ish direction lying in the XY plane. **Not a black screen: wrong shading
that looks like art.**

The rule is that the format's `(kind, count)` must **equal** the shader's
declared `(kind, count)` — deliberately stricter than WebGPU, because presence-only
and base-type-compatible **both let `obelisk.go:113` through**. The cost is that
WebGPU's legal widening and narrowing are forbidden, and that cost is zero
against the tree: every `@location` in `scene/builtin` and `canvas/builtin` is
already an exact match. The check is **one-directional** — a layout supplying an
attribute the shader does not read is legal and common.

**2. `arrayStride` is a multiple of 4 — in `gfx`, same check, same machinery.**
From [#195](https://github.com/dvoyni/cog/issues/195). Both named layouts satisfy
it by construction, so **scene never trips this**; the exposure is entirely the
custom-layout path, which is now the whole of what is exposed. A 30-byte stride
**succeeds on Vulkan, Apple-silicon Metal and D3D12 and fails on `js/wasm`, on
GLES and on Apple2–4 Metal** — green on a Windows dev machine, broken in the
browser, and `core/validate.go:402` never touches `desc.Vertex.Buffers` on any
path.

**3. A custom layout's Go struct size against its declared extent — in `scene`.**
`gfx` derives the stride as the largest attribute end offset (`gfx/mesh.go:138`)
while a custom-layout caller uploads `unsafe.Sizeof`. Only **scene** can see the
Go type at all — `mintMesh` holds both the layout and `TVertex` — so it is one
comparison and no reflection. `gfx` could manage only the weaker
`bufferLen % stride == 0`, which catches a 36-vs-34 case but passes a 40-byte
struct at stride 20. **Folding them into one "layout validation" would put a
check in `gfx` that `gfx` cannot perform.**

**Plus one on the index buffer.** `MeshIndexed` computes `indexCount = indices.size / 4`
(`gfx/mesh.go:132`) and validates nothing; with two widths, a buffer declared
`uint16` but written as `uint32` becomes a third way to be silently wrong. `gfx`
checks that the byte length divides by the declared width — `O(1)`, no walk — and
**leaves the range check to scene**, where a pass over the indices already runs.

### Failures are loud, drop the draw, and report once

A layout mismatch goes to `firstErr`, not `t.diagnostic`. `diagnostic` is for
*"this renders here but would not on the web"*; a layout mismatch renders
**wrongly, everywhere**. And `gfx` **stops swallowing errors on the draw-setup
path**: `ensurePipeline:736` currently reads
`id, err := backend.NewPipeline(...); if err != nil { return 0 }`, discarding the
backend's own diagnosis. From the caller's seat *"gfx refused to build this"* and
*"the backend refused to build this"* are the same event.

**The failed-pipeline cache entry is what makes that possible at all**, and it is
one line. `cachedShader` solved this deliberately for shaders; `t.pipelines` has
no equivalent, so a surfaced error would re-report every frame forever. Storing
`t.pipelines[k] = 0` on failure fixes it: `ok` becomes true, the caller drops the
draw on the zero id exactly as today, and report-once-drop-always falls out of
the cache that already exists.

> **Gap — nothing holds the Go packing and the WGSL decode of the morph buffer in
> agreement.** `sceneMorphDeltas` becomes an `array<u32>` scene packs bytes into,
> which is exactly the split check 1 closed for the vertex buffer — except that
> check reads `@location` declarations and **never sees a storage buffer**. `gfx`
> already reflects storage structs, member offsets and array strides included
> (`wgpu/gfxreflect.go:88`), so the numbers a check would need exist and nothing
> compares them. Stated here as exposure; the check itself belongs on
> [gfx: an unsupplied storage buffer binding fails silently](https://github.com/dvoyni/cog/issues/133).

> **Gap — every fidelity claim in this document was judged by eye.** There is no
> pixel readback anywhere in `gfx` or `wgpu`, so the `oct32`-versus-`oct16`
> finding rests on differenced screenshots analysed offline, and the motion
> finding on [#179](https://github.com/dvoyni/cog/issues/179) rests on words with
> no capture behind it. What would settle it is readback, which is out of scope
> for this map.

---

## What all of it is worth

> **Settled here — this is the first place the map's total is computable.**
> [#173](https://github.com/dvoyni/cog/issues/173) could only say *"near 1.7 MiB
> against 3.77 MiB"* because morph deltas were still pending
> [#178](https://github.com/dvoyni/cog/issues/178), and #178 reported its own
> buffer without re-totalling. Assembled, and then measured.

**The table is a measurement, not a projection.**
[#223](https://github.com/dvoyni/cog/issues/223) drove `convertDocument` over
every vendored `.glb` and summed what the packer and `indexBytes` actually
emit, against what the same loader stored before the umbrella landed.

| buffer | before | projected | **measured** | of before |
| --- | ---: | ---: | ---: | ---: |
| vertices | 3,232.9 KiB | 1,219.9 KiB | **1,219.9 KiB** | 37.7% |
| indices | 246.3 KiB | 123.2 KiB | **118.1 KiB** | 47.9% |
| morph deltas | 385.8 KiB | 18.6 KiB | **18.8 KiB** | 4.9% |
| **geometry** | **3,865.0 KiB** | ≈1,361.7 KiB | **1,356.8 KiB** | **35.1%** |

3,865.0 KiB is 3.77 MiB and 1,356.8 KiB is **1.325 MiB**. In bytes:
3,957,756 before, 1,389,340 after.

**The `≈` resolved, and what it was hiding.** The projection's vertex figure was
counted **after** [#176](https://github.com/dvoyni/cog/issues/176)'s
deduplication and its index and morph figures were not, so the total was a
ceiling. The gap is **4,970 B**, and it is two effects of opposite sign:

- **Deduplication takes 5,184 B off the indices**, which is exactly the halved
  form of the 10,368 B of `uint32` it removes — `InterpolationTest`'s eight
  duplicate cube buffers and `CesiumMilkTruck`'s second wheel. The pre-dedup
  index total 246.3 KiB is 236.2 KiB post-dedup, and the narrowing halves that
  to **118.1 KiB exactly**. The narrowing itself is therefore still the flat
  50.0% the projection claimed; the extra 2.1 points in the table's last column
  are #176's, not the index width's.
- **The morph block overshoots its projection by 214 B**, the per-slot range
  words and per-target headers the projection rounded away — the same 18.8 KiB
  [#222](https://github.com/dvoyni/cog/issues/222) reported. No morphed
  primitive is duplicated, so deduplication takes nothing off this row.

The vertex row lands on its projection **exactly**, which it should: the
projection was already post-deduplication and was the same arithmetic over the
same two strides. It is 1,249,152 B over 38,391
vertices, of which 2,580 take the 40-byte skinned layout (`Fox`'s 1,728 under a
real skin, 852 plain-bound) and 35,811 take the 32-byte one.

Decomposed by axis: **precision alone** reaches 1,499.6 KiB of vertices (46.4%);
**the presence trim adds 279.8 KiB net**, the 6.66 KiB plain-joint tax already
deducted. Stated plainly — the presence axis was charted as potentially half the
prize and came back worth 279.8 KiB, **all of it from the one attribute pair that
costs nothing**, and the map declined the rest. That is a finding, and it was the
point of charting the axes in this order.

> **What the measurement does not say.** It is taken from the loader's own
> buffers, so it counts what scene uploads and not what a driver allocates, and
> the corpus caveats above apply to it unchanged — `CompareBaseColor` is 67.7%
> of the measured geometry total on its own. And it says nothing at all about
> whether the narrowed engine **looks** right; that is #223's other half, and it
> has no instrument in this repo.

**No ADR.** [Assemble scene/docs/specs/mesh.md](https://github.com/dvoyni/cog/issues/175)
provided for one on the *"change nothing"* outcome, where a spec section would
have been the wrong home for a decision to leave the code alone. The map did not
reach that outcome — it reaches a layout change in three buffers plus a public
API change — so the decision has somewhere to live and this document is it.

---

## Required scene changes

**All of this has landed**, under
[#213](https://github.com/dvoyni/cog/issues/213) and its tickets #214 to #222.
The list is kept in its original imperative form as the record of what changed,
in dependency order. **Items marked (gfx) were prerequisites** and landed before
scene compiled against them.

**Prerequisites (gfx)**

- A `uint16` index path. `MeshDescr` gains an index-width field; `pipelineKey`
  gains the **strip** format. **`RenderPass.SetIndexBuffer(BufferID, int)` is
  exported** (`gfx/gpuqueue.go:139`), so every backend implementation changes
  signature — `wgpu` plus three test fakes (`gfx/plugin_test.go:209,408`,
  `scene/scene_test.go:206`). The queue encoding is free: `gpuOp` already carries
  spare `arg` fields.
- Index-length validation in the translator, at the existing
  `m.indexed && m.indices.id != 0 && m.indexCount > 0` guard
  (`gfx/translate.go:366`) — `MeshIndexed` is a pure value constructor with no
  error return. Note this makes it the **second** non-fatal report in `gfx`; the
  comment at `gfx/translate.go:322` claiming `ErrShaderExceedsWebLimits` is the
  only one is edited deliberately.
- Vertex-input reflection: `shaderLayoutFrom` (`wgpu/gfxreflect.go:33`) grows one
  loop over `EntryPoints[i].Function.Arguments[j]`; `ShaderLayout` grows one
  slice. No new parse, no second lowering, no new dependency.
- The exact-match comparison plus the `arrayStride % 4` check in
  `ensurePipeline`, reported through `firstErr`.
- `t.pipelines[k] = 0` on failure, so the above report once rather than at frame
  rate.
- Surface the backend's own error at `ensurePipeline:736` instead of `return 0`.

**`scene/mesh.go`**

- `Vertex` loses `Joints` and `Weights`; `Color` becomes `m.Color`.
- **Rewrite the doc comment.** The current text (`13-21`) explains 84 bytes with
  *"the bundled shader is one module with one vertex stage and no entry-point
  selection"* — stale since
  [scene: split scene.wgsl into ten sources](https://github.com/dvoyni/cog/issues/141)
  — and describes the dead last 24 bytes, which no longer exist. It states the
  authoring/storage divergence instead.
- `standardVertexLayout` becomes hand-written offset constants, not
  `unsafe.Offsetof` over the struct.
- Restate `VertexLayout`'s interface doc to describe the **buffer** layout,
  naming `scene.Vertex` as the one type whose Go fields differ from what it
  reports.
- Add the skinned layout, unexported.
- `indexBytes` narrows for durable meshes and stays a reinterpret for
  `TemporaryMesh`.

**`scene/meshbake.go`**

- `mintMesh`'s existing `vertexBounds` walk also accumulates the UV ranges and
  packs into the staging arena — one pass, three jobs. `appendStaging`'s memcpy
  becomes a transform-copy.
- Width derivation on every bake and re-derivation on `UpdateMesh`.

**`scene/builtin/scene/`**

- New `vertexdecode.wgsl`: oct decode for normal and tangent, UV dequantisation
  against the per-mesh record. Exported as `VertexDecodePath` in
  `scene/assets.go`.
- `vertex.wgsl`'s `SceneVertexIn` takes the narrowed formats.
- `deform.wgsl` divides the skinned position by `total`.
- `morph.wgsl` takes the span test, the per-target header and the per-slot
  decode; `targetStride` leaves the `sceneAnim` header.
- The per-mesh record at `@group(0) @binding(3)`; `instance.wgsl`'s spare
  `vec2<u32>` becomes the mesh index and the node joint.

**`scene/gltfload.go`, `scene/gltfanim.go`, `scene/gltfmesh.go`**

- `skinBinding` splits: `geometryKey` keeps `skin`, drops `joint`.
  `bindGeometryJoints`' `binding.joint >= 0` case disappears.
- `SCENE_PLAINJOINT` as an instance flag beside `SCENE_NONUNIFORM` and
  `SCENE_NOSKIN`.
- The skinned-layout union: a converted geometry takes the 40-byte layout iff any
  placement draws it under `SCENE_SKIN`.
- Reject a skin whose joint count exceeds 256, at load, loudly.
- **Fix `gltfmesh.go:157`**, which says *"Nothing binds them yet — every draw
  scene makes carries SCENE_NOSKIN"*. Written before skinning landed and false
  since.

**`scene/model.go`, `scene/pack.go`, `scene/modeltable.go`**

- `variantFor` moves from the model to the primitive.
- `loadedPrimitive.skinned`'s two readers — the `sceneNoSkin` flag
  (`pack.go:252`) and `neverCull` (`model.go:342`, `modeltable.go:215`,
  `gltfload.go:537`) — move to the placement. **This is where an implementation
  session will get hurt**; a shared primitive culled by a static instance's
  bounds while its animated instance walks out of them disappears with nothing
  reported.
- `SceneInstance`'s `Spare` comment: the record is now fully allocated at 64
  bytes and the next spender pays 128 or a repack.

**`scene/err.go`**

- Restate `ErrMeshCustomLayoutNeedsMaterial` (`146-155`) in terms of a layout the
  bundled PBR does not know, rather than *"scene.Vertex's eight attributes"*.
  Mechanism unchanged.

**`scene/unitmesh.go`**

- `:91,158` — `[4]uint8{255,255,255,255}` becomes `m.White`.

**`cog-examples`**

- `cmd/scene/cameras/obelisk.go:113` and `cmd/scene/loading/repaint.go:85`
  include `VertexDecodePath` and read `vec2<f32>` at `@location(1)`.

**Docs**

- `scene/docs/specs/scene.md:1261-1300` collapses to a pointer here. That
  paragraph is not a summary but the current authority on delta layout, and this
  spec falsifies most of it — the 16/32/48 stride, the 25 MiB / 8 MiB face
  argument, the address formula. What stays in `scene.md` is the plumbing that is
  untouched: one buffer per model, the per-node weight slots, the CPU-side blend.
  **Two specs describing one layout is how they drift.**
- `scene/README.md` gains the pointer to this document.
- `CONTEXT.md` gains **Vertex layout**, **Named layout**, **Authoring vertex**,
  **Storage vertex**, **Sparse target** and **Live span**. The glossary defines
  `Variant`, `Supply` and `Define` and defines no mesh or morph vocabulary at
  all.
- `wgpu/gfxbackend.go:990`'s *"Index buffers are uint32 throughout the engine"*
  and `gfx/mesh.go:99,118`'s *"optional uint32 index array"* both go stale with
  the index change and are fixed by it.

---

## Out of scope

Recorded on
[scene: what a mesh stores](https://github.com/dvoyni/cog/issues/167)'s map and
repeated here so a reader of the spec alone does not re-propose them.

- **A second vertex-buffer slot in `gfx`**, and the stride-0 default buffer it
  would enable. This is Godot's and Unity's mechanism for trimming attributes
  with **no** shader variant, and it is not expressible today: `MeshDescr`
  carries one interleaved buffer and `VertexAttr` is offset-plus-type with no
  slot. Scene's answer does not need it — the one cut taken drops attributes the
  shader already declines to declare.
- **Deriving the tangent frame per-fragment from UV derivatives, storing none at
  all.** The route to a **28-byte** vertex, and it dodges the variant objection
  completely. Ruled out as a **shading-model** decision rather than a *what a
  mesh stores* one, against `scene/gltfmesh.go:343-344`'s own note that a
  MikkTSpace-authored mesh shipping no `TANGENT` *"is getting an approximation"* —
  deriving for everyone hands **every** mesh that approximation, including the
  ones that shipped real tangents.
- **An entry-point field on `ShaderDescr`** — `#45`'s option 1. It existed to
  avoid duplicating the fragment stage across variant modules, which the
  preprocessor solved by making variants share sources. The backend still
  hardcodes `vs_main`/`fs_main` (`wgpu/gfxbackend.go:618,654`), and that is now
  merely a fact rather than a cost.
- **The baked pose buffer's precision.** `scene/animpack.go`'s poses are per-model
  animation data with their own consumer and their own error budget, sized by
  joint count and frame count rather than by vertex count.
- **A gfx check that a storage buffer's WGSL layout matches what the CPU packs
  into it** — belongs on [#133](https://github.com/dvoyni/cog/issues/133).
- **Finishing gfx's storage-struct member packing**, and **a generic
  `kernel.ReportErrorOnce`** ([#186](https://github.com/dvoyni/cog/issues/186)) —
  the report-once pattern has six independent implementations in the tree and
  this spec's failed-pipeline entry makes a seventh. Engine-wide refactoring past
  a mesh spec.
- **A durable public bake for a caller-built texture in `gfx`.** Found while
  measuring: a 2048×1024 panorama cost **5.8 ms a frame** because
  `TextureWithBytes` named as a draw parameter is re-baked every frame and
  `uploadMipChain` box-filters the whole chain on the CPU.
- **Canvas's own variant axis** ([#149](https://github.com/dvoyni/cog/issues/149))
  and **app-defined per-instance properties**
  ([#148](https://github.com/dvoyni/cog/issues/148)) — a different plugin and
  instance data respectively.
- **The two silent-failure defects the prototype hit and could not explain** —
  [#184](https://github.com/dvoyni/cog/issues/184) and
  [#185](https://github.com/dvoyni/cog/issues/185). Defects rather than design
  questions. The second is the honest answer to *"what would `gfx` have had to
  check"*: **nothing** — the layout and the shader agreed, the formats were legal,
  and the data still did not arrive.
