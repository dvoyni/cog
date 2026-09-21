# Where ecsscene's frame time goes today

Research for [where ecsscene's frame time goes today, measured against its own
benches](https://github.com/dvoyni/cog/issues/493), under the map [model: one plugin beneath
two renderers, and what a renderer still owns](https://github.com/dvoyni/cog/issues/450).

Measured at `6a5254d`, go1.27.1 windows/amd64, AMD Ryzen 9 7950X3D (32 threads). Raw
numbers and CPU profiles are in [`baseline/`](baseline/). The new arms are in
`bundles/ecsscene/internal/baselinebench_test.go` on this branch.

## The short version

- **The five existing arms do not measure a drawn frame.** Their harness has no camera, an
  empty filesystem and a backend that is never Ready, so scene's flush returns before it
  culls, sorts, packs or reaches gfx. They measure the binding plus scene's recording:
  **0.42 ms for 5 000 models.** A frame that actually draws those 5 000 is **8.3 ms**.
- **Sort, cull and the layer test are not hot.** Together they are under 0.4% of a drawn
  frame: the cull, with the layer mask inside it, is 0.24%, and the sort is 0.12%.
- **The ECS is noise.** The same frame recorded from a plain Go slice with no ECS plugin
  composed is 8.21 ms, against 8.30 ms through the ECS. The query walk and the binding's
  copy-out are about 4.5% of the profile.
- **The proxy layer is small.** What ecsscene builds that a direct-to-`gfx` renderer
  would not — the `ModelDraw` copy, `OpQueue.Model` and the queue's flush bookkeeping —
  is **about 7% of the frame (≈0.6 ms)**.
- **Two thirds of the frame is the call shape, not the proxy.** ecsscene records one
  `Model` call per Entity, and scene never merges calls, so 5 000 crates are 5 000
  batches. The same 5 000 as one `Model` call with 5 000 `Transforms` is one instanced
  batch and takes **2.7 ms against 8.2 ms.** The difference is gfx baking parameters and
  a material once per batch.
- **Most of the instanced frame is spent re-hashing one material.** In that 2.7 ms frame,
  55% is `MaterialKeyOf` fingerprinting the file's own material, once per draw record,
  every frame. `expandModels` passes key 0 for a file material, so `materialTable.intern`
  computes the content key from scratch for each of the 5 000 instances of the same
  primitive. Both renderers pay this, and it is `model`'s data: a key computed once at
  load would remove it.

## 1. Baseline

Each binary was built once and the arms were run **interleaved**: one arm, then the next,
round after round, six rounds at `-benchtime 1s`. They were not run in sequence, because
whole-frame benches here swing about ±10% by run order. `-race` cannot build in this
environment, so the repetition is the six rounds, not the race detector.

The existing arms, as they stand (`recordbench_test.go`), measure the binding plus scene's
recording, with no draw:

| arm | min | mean |
| --- | ---: | ---: |
| `BenchmarkFrameEmpty` | 16.4 µs | 17.2 µs |
| `BenchmarkFrame5000` | 416 µs | 425 µs |
| `BenchmarkFrameAnimated5000` | 458 µs | 469 µs |
| `BenchmarkFrameParams5000` | 555 µs | 563 µs |
| `BenchmarkFrameMaterial5000` | 1 354 µs | 1 365 µs |

The new arms draw a frame: a camera at Z=80, the crate on disk and resident, a Ready stub
backend whose `Execute` is a no-op, and 5 000 Entities in a 100×50 grid the camera sees
whole. The grid is the same one scene's own `BenchmarkFrame` uses. All 5 000 are packed
every frame, in one pass.

| arm | batches | min | mean |
| --- | ---: | ---: | ---: |
| `BenchmarkDrawnModel5000` | 5 000 | 8.13 ms | 8.30 ms |
| `BenchmarkDrawnModelParams5000` | 5 000 | 8.57 ms | 9.02 ms |
| `BenchmarkDrawnModelMaterial5000` | 5 000 | 5.92 ms | 6.11 ms |
| `BenchmarkDrawnMesh5000` | 5 000 | 5.32 ms | 5.42 ms |
| `BenchmarkDirectModel5000` *(no ECS, one call per crate)* | 5 000 | 7.93 ms | 8.21 ms |

A second interleaved set, taken against the instanced arm:

| arm | batches | min | mean |
| --- | ---: | ---: | ---: |
| `BenchmarkDrawnModel5000` | 5 000 | 8.18 ms | 8.36 ms |
| `BenchmarkDirectModel5000` | 5 000 | 7.99 ms | 8.21 ms |
| `BenchmarkDirectModelInstanced5000` *(no ECS, one call with 5 000 Transforms)* | 1 | 2.69 ms | 2.72 ms |

Every arm allocates 12–13 objects a frame whatever its population, so the allocation
criterion holds across the drawn frame as well as the recording.

**Two gaps.** There is no drawn animated arm, because the crate has no clips and an
animated arm over it would measure a missing-clip path. And the stub backend's `Execute`
does nothing, so what a real device does with 5 000 draw calls is not in these numbers.
Both favour the conclusion below: a real submit adds per-batch cost, not per-Entity cost.

## 2. Breakdown

This is a CPU profile of `BenchmarkDrawnModel5000` at `-benchtime 4s`, taken as
cumulative share of samples. The profile covers the whole process, setup included, but
frames dominate it. The ms column scales each share to the 8.3 ms frame.

| where | share | ≈ ms |
| --- | ---: | ---: |
| ECS query walk + binding copy-out (`record`), including `scene.OpQueue.Model` | 4.6% | 0.38 |
| &nbsp;&nbsp;of which `OpQueue.Model` | 2.5% | 0.21 |
| scene's flush bookkeeping (`OpQueueBeginFlush`, `OpQueueAppendDraw`) | 2.6% | 0.22 |
| `expandModels`: each model call into per-primitive draw records and world matrices | 14.2% | 1.18 |
| `prepareDraws`: resolve mesh, intern material, world bounds | 24.5% | 2.03 |
| &nbsp;&nbsp;of which `MaterialKeyOf`, re-fingerprinting the file's material per draw | 19.6% | 1.63 |
| `flushPass`: cull, sort, pack | 10.3% | 0.86 |
| &nbsp;&nbsp;of which the cull, layer-mask test included | 0.24% | 0.02 |
| &nbsp;&nbsp;of which `sortEntries` | 0.12% | 0.01 |
| &nbsp;&nbsp;of which `addDraw` (instance and material records) | 5.4% | 0.45 |
| `emit` → `gfx.OpQueue.DrawInstancedFrom`, once per batch | 42.4% | 3.52 |
| &nbsp;&nbsp;of which `bakeParametersIfNeeded` | 33.5% | 2.78 |
| &nbsp;&nbsp;of which `bakeMaterialIfNeeded` | 21.7% | 1.80 |
| gfx present (stub backend) | 0.7% | 0.06 |

The two nested gfx rows overlap: material baking is partly inside parameter baking.

`DirectModel5000`'s profile has the same shape within a point or two
([`DirectModel5000.top.txt`](baseline/DirectModel5000.top.txt)), minus the ECS rows.

`DirectModelInstanced5000` ([`DirectModelInstanced5000.top.txt`](baseline/DirectModelInstanced5000.top.txt))
is 2.7 ms, split as follows:

- `prepareDraws`: 69%, of which `MaterialKeyOf` is 55%
- `expandModels`: 21%
- `flushPass`: 6%
- gfx: nearly nothing, since there is one batch

## 3. The share that is proxying

A direct-to-`gfx` renderer would not build:

- the `scene.ModelDraw` per Entity
- `OpQueue.Model`'s copy into scene's arenas
- the queue's flush bookkeeping

That is the `record` row and the flush-bookkeeping row above: **about 7% of a drawn frame, ≈0.6 ms at 5 000 Entities.**

`expandModels`' per-primitive records are arguably part of the proxy too, but a renderer
recording to gfx itself still has to find each Entity's primitives and compute a world
matrix per primitive. Counting all of it as proxy gives an upper bound of about 21%.

What the proxy does **not** include is the 62% that goes to per-draw material hashing and
per-batch gfx baking. A direct renderer that records one gfx draw per Entity inherits all
of it. It only goes away if the renderer **groups Entities by mesh and material before
it records**, which scene already rewards (8.2 → 2.7 ms), and if the file material's key
stops being recomputed per draw.

## 4. What the profile contradicts

The ticket's list of suspects was the ECS query walk, the per-entity copy, scene's sort,
the layer test, the `OpQueue` append and the gfx submit. Against that list:

1. **The sort is not hot (0.12%). The cull and layer test are not hot (0.24%).** Sorting,
   culling and layering faster buys nothing measurable at 5 000 Entities, because none of
   the three costs anything now.
2. **The ECS walk and the proxy are small (≈4.5% and ≈7%).** Recording straight to `gfx`
   instead of into `scene.OpQueue` saves at most ≈0.6 ms of an 8.3 ms frame.
3. **Batching is the real lever.** One call per Entity makes one gfx draw per Entity, and
   per-batch parameter and material baking is 42% of the frame. Instancing the same
   population is 3× faster, today, through scene, with no redesign. ecsscene's binding
   records one call per Entity by design ("the binding records one call per Entity and
   batches nothing, and scene does not merge calls", `draw_test.go`), and that is the
   cost.
4. **A model's own material is re-keyed per draw record, every frame.** It is 20% of the
   per-Entity frame and 55% of the instanced one. It belongs to the vocabulary question,
   not the ECS one: the key is a property of the loaded material (`loadedMaterial` /
   `modelMaterial`, model-side by [the inventory](https://github.com/dvoyni/cog/blob/research/scene-inventory/bundles/scene/docs/research/scene-inventory.md)), and `model` could
   hand it over precomputed.
5. **The existing arms cannot judge the redesign.** They stop before anything the
   redesign changes. Any comparison of a new ecsscene against today's should use the
   drawn arms, or a successor that draws.

## Reproducing this

```
cd bundles/ecsscene/internal
go test -c -o ecsscene.test.exe .
# interleave: loop rounds, run each arm once per round
./ecsscene.test.exe -test.run '^$' -test.bench '^BenchmarkDrawnModel5000$' -test.benchtime 1s -test.benchmem
# profile
./ecsscene.test.exe -test.run '^$' -test.bench '^BenchmarkDrawnModel5000$' -test.benchtime 4s -test.cpuprofile DrawnModel5000.pprof
go tool pprof -top -cum ecsscene.test.exe DrawnModel5000.pprof
```

The `.pprof` files in `baseline/` were taken against a binary built from this commit and
read with `go tool pprof -top -cum`. The rendered tops are kept beside them.
