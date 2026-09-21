# What a raw-data decoder seam costs the parse

Measured 2026-09-21 for the question: should the glTF decoder emit raw,
unpacked data (plain attribute slices, unbaked keyframes, plain material
values) with a separate packing pass after it? Numbers only; the decision is
made elsewhere.

Benchmarks: `bundles/scene/internal/types/seam_bench_test.go` (throwaway,
research branch only). Machine: Windows 11, Go 1.27.1, amd64, 32 threads.
`-race` does not build here; stability comes from repetition instead.

## What the parse does today

The facts the question rests on differ from the brief in four places:

- **Vertices are not packed during the parse.** `readVertexAttributes` reads
  each accessor through `modeler.ReadAccessor` (which already allocates a typed
  slice per accessor) and copies it, one callback per element, into
  `[]skinnedVertex`, an array-of-structs of plain floats. The GPU byte layout is
  written by `packOverAuthored` in `bakeModelGeometry`, which is the install
  (upload) half, not the parse. So the parse already has a separate packing
  pass after it.
- **Keyframes are already decoded raw before baking.** `animCurve` reads each
  sampler into `times []float32` and `values []attrValue`, then `bakeClip`
  samples those onto the pose grid. Handing clips across a seam unbaked adds no
  pass; it moves the bake.
- **Morph deltas are already two-pass.** `readMorphTargets` fills a float
  working copy (`deltas []m.Vec4`) and `packMorphDeltas`/`packMorphBlock`
  narrows it afterwards.
- **Images are only named.** `textureRequests` slices embedded bytes out of the
  decoded buffer; nothing is decoded in the parse.

## Models

From `cog-examples/assets`.

| Model | File | Vertices | Indices | Skinned | Morphed | Anim |
|---|---:|---:|---:|---|---|---|
| BoxVertexColors (small static) | 1,836 B | 24 | 36 | no | no | none |
| Fox (skinned, `animated` + `fountain`) | 162,732 B | 1,728 | 1,728 | yes, 24 joints | no | 3 clips, 7,728 pose rows at 60 Hz |
| CompareBaseColor (most vertices) | 1,507,720 B | 27,648 | 27,648 | no | no | none |
| MorphStressTest (extra, for the morph share) | 575,812 B | 1,528 | 7,236 | no | 2 geometries | 3 clips |

CompareBaseColor has the most vertices of the 16 local models. No local model
is larger than 27,648 vertices.

## Parse: bytes in memory to `LoadedModel`

`BenchmarkSeamParse` calls `parseModel` on bytes already read (no disk, no
GPU). Median of 8 runs, each in its own process, one model per process.

| Model | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| BoxVertexColors | 31,644 | 20,765 | 163 |
| Fox | 2,515,794 | 1,583,393 | 2,809 |
| CompareBaseColor | 1,669,849 | 7,301,618 | 557 |
| MorphStressTest | 1,753,933 (single 5 s run) | 2,616,218 | 1,240 |

## Profile shares

Each share is a percentage of `parseModel`'s cumulative time, from one 5 s
CPU profile per model. Background GC mark workers run on other cores and are
not included. They add another 8 to 17% of the total samples.

| Bucket | Box | Fox | CompareBaseColor | MorphStress |
|---|---:|---:|---:|---:|
| JSON/document decode (`gltf.Decoder.Decode`) | 83% | 16% | 16% | 35% |
| Vertex accessor read + convert (`readVertexAttributes`) | 7% | 5% | 76% | 6% |
| ... of which `modeler.ReadAccessor` (typed-slice alloc + copy) | 5% | 3% | 36% | 3% |
| Indices (`readIndices`) | 1% | <1% | 2% | <1% |
| Vertex packing to GPU layout | 0 (install half) | 0 | 0 | 0 |
| Animation: keyframe decode (`animCurve`) | 0 | 7% | 0 | <1% |
| Animation: bake (`bakeClip`: pose walk, `poseFromMatrix`) | 0 | 69% | 0 | 8% (all of `bakeAnimation`) |
| Morph read (`readMorphTargets`) | 0 | 0 | 0 | 35% |
| Morph pack (`packMorphDeltas`) | 0 | 0 | 0 | 13% |
| PBR record fill (`convertMaterial`) | <0.2% | <0.2% | <0.2% | <0.2% |
| Texture naming | <0.2% | <0.2% | <0.2% | <0.2% |
| Everything else (walk, joint space, maps) | ~9% | ~2% | ~6% | ~3% |

"<0.2%" means the function had no samples at the profile's resolution.

## One pass against two, vertex path

`BenchmarkSeamVertex` runs `convertPrimitive` over every primitive of a
pre-decoded document in three arms. Indices, morph targets, generated normals
and tangents are the same code in every arm. `TestSeamTwoPassMatches` checks
that all three arms produce identical vertices and UV ranges.

- **onepass**: the shipped code.
- **twopass**: every attribute is read into a new plain slice (`[]m.Vec3`,
  `[]m.Vec2`, `[]m.Vec4`; colours, joints and weights stay in modeler's own
  slices), and then a second loop fills `[]skinnedVertex` from those slices.
  This is the brief's shape: extra intermediate allocations plus an extra
  pass.
- **twopassalias**: the raw slice is modeler's own decoded slice wherever the
  accessor is already float32. Quantised accessors are widened into a fresh
  slice. The second loop is the same. This arm allocates nothing more than
  onepass.

The arms ran interleaved: in each round, each model and each arm ran in its own
process (one, two, alias, then parse). The table gives the median of 8 rounds.
An earlier 8-round run agreed within 3%.

| Model | onepass | twopass | Δ twopass | Δ % of parse | alias | Δ alias | Δ % of parse |
|---|---:|---:|---:|---:|---:|---:|---:|
| Box | 3.76 µs | 4.06 µs | +0.30 µs | +0.9% | 3.74 µs | −0.02 µs | −0.1% |
| Fox | 118.0 µs | 132.8 µs | +14.8 µs | +0.6% | 107.5 µs | −10.6 µs | −0.4% |
| CompareBaseColor | 1,213.9 µs | 1,491.7 µs | +277.8 µs | +16.6% | 975.8 µs | −238.1 µs | −14.3% |

Bytes per op: CompareBaseColor 3.97 MB (onepass), 4.88 MB (twopass) and
3.97 MB (alias). Fox 481 KB, 518 KB and 481 KB.

The alias arm beats the shipped path because the shipped read makes one
closure call per element through `readAttribute`'s `set` callback, and the
alias fill is a straight loop.

Animation keyframes were not given an arm. They are already held raw
(`animCurve`) before the bake, so a raw seam adds no pass and no allocation;
it only keeps the curves alive past the parse. Their decode share is the
"keyframe decode" row above.

## Scale: the rest of a load

Single-process medians of 3 runs, for order of magnitude only.

| Model | parse | `os.ReadFile` (warm cache) | install CPU pack (`packOverAuthored` + `indexBytes`) | embedded image decode (stdlib `image.Decode`) |
|---|---:|---:|---:|---:|
| Box | 32 µs | 28 µs | 1.3 µs | none |
| Fox | 2.5 ms | 0.27 ms | 57 µs | 6.7 ms (1 image) |
| CompareBaseColor | 1.7 ms | 0.73 ms | 0.62 ms | 6.2 ms (2 images) |
| WaterBottle | ~1.0 ms | ~0.8 ms | 70 µs | 40 ms (4 images) |

The texture cache decodes images on the upload side. The engine's own decoder
path may differ from stdlib `image.Decode`, and GPU upload itself was not
measured.

## Caveats

- **Order dependence.** CompareBaseColor's parse is 1.6 ms alone in a process
  and 3.7 to 4.2 ms when other models' benchmarks ran first in the same
  process. It allocates 7.3 MB per op, so the GC state it inherits matters.
  The parse, profile and vertex-arm numbers come from one model per process;
  the scale table does not.
- **The vertex arms are measured in isolation, not inside a full parse.** The
  "% of parse" column divides an isolated delta by a separate parse median. The
  GC cost of twopass's extra 0.9 MB would land partly on the background mark
  workers.
- **The models are small.** The largest local model has 27,648 vertices.
  Vertex-path cost scales roughly linearly, while JSON decode scales with node
  and accessor count.
- **twopass reuses modeler's slices for colours, joints and weights.** A thin
  cut that also re-wrapped those would cost a little more than the twopass
  column.
