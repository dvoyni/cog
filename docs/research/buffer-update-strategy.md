# Buffer update strategy: static, per-frame, and dynamic

Research for [dvoyni/cog#3](https://github.com/dvoyni/cog/issues/3), child of the
scene map [dvoyni/cog#1](https://github.com/dvoyni/cog/issues/1). Code references
are to `gfx/` and `wgpu/` at commit `46e0ae8` and to the vendored
`github.com/gogpu/wgpu v0.31.6-0.20260827093430-9ecabb42040a` (pure-Go backend,
which is what the default `go build` selects; the browser build is
`GOOS=js GOARCH=wasm`).

## 1. The three upload paths in WebGPU

| Path | What happens | Constraints |
| --- | --- | --- |
| `queue.writeBuffer` | The user agent copies the bytes into an implicit staging area at call time and enqueues a buffer-to-buffer copy that executes on the queue timeline before the next `submit` ([toji: "the user agent manage[s] an implicit staging buffer for you"](https://toji.dev/webgpu-best-practices/buffer-uploads.html); [wgpu `Queue::write_buffer`: "data will be immediately copied into staging memory... begin GPU execution only on the next call to `Queue::submit()`"](https://docs.rs/wgpu/latest/wgpu/struct.Queue.html)). | Destination needs `COPY_DST`, must be unmapped, offset and size multiples of 4 ([MDN](https://developer.mozilla.org/en-US/docs/Web/API/GPUQueue/writeBuffer)). |
| `mappedAtCreation` | Buffer is born mapped; write once, `unmap`, use. No `MAP_WRITE`/`COPY_DST` required ([webgpufundamentals](https://webgpufundamentals.org/webgpu/lessons/webgpu-copying-data.html)). | Only at creation. Saves one CPU copy for write-once data ([toji](https://toji.dev/webgpu-best-practices/buffer-uploads.html)). |
| `mapAsync` staging ring | Keep N `MAP_WRITE\|COPY_SRC` buffers, write into a mapped one, `unmap`, `copyBufferToBuffer` into the real buffer, re-map asynchronously. | `MAP_WRITE` may only be combined with `COPY_SRC`, so a mappable buffer can never itself be a vertex/index/uniform/storage buffer ([webgpufundamentals](https://webgpufundamentals.org/webgpu/lessons/webgpu-copying-data.html)). Map completion is asynchronous and "an indeterminate amount of time" away. |

Key consequences:

- **Every GPU-readable buffer in WebGPU is updated by a copy.** There is no
  persistent mapping of a vertex or storage buffer. The only question is who owns
  the staging memory: the implementation (`writeBuffer`) or the app (mapped ring).
- **A single buffer rewritten with `writeBuffer` every frame is race-free.** The copy is
  queued in order with the draws; no double/triple buffering is needed
  ([gpuweb#2525, kvark: "Using the same UBO is totally fine in this case. Doing double/triple-buffering is another strategy that you'd do without `writeBuffer`"](https://github.com/gpuweb/gpuweb/discussions/2525); Dawn keeps "a ring staging buffer" so `writeBuffer` never introduces GPU-to-CPU sync points).
- **All `writeBuffer` calls made before a `submit` land before that whole submit.**
  Writing the same buffer twice between draws of one command buffer means the
  second write wins for both draws. gfx already works around this with one pooled
  256-byte uniform buffer per draw (`wgpu/gfxbackend.go` `uniform()`: "A distinct
  buffer per draw lets all per-draw writes precede the single submit"). Per-frame
  data that differs per draw must therefore live in distinct buffers or distinct
  regions of one buffer, never in one buffer rewritten between draws.
- **Guidance from the WebGPU editors is unambiguous:** `writeBuffer` is the
  recommended path "when in doubt" and "the preferred route for WASM apps", and
  "don't expect to see much performance difference between them"
  ([toji](https://toji.dev/webgpu-best-practices/buffer-uploads.html)). A mapped
  ring only pays off when the app can serialize directly into the mapped range and
  is not running in WASM ([gpuweb#1428](https://github.com/gpuweb/gpuweb/discussions/1428)),
  and even then reports on that thread show map/unmap latency of 16-32 ms on some
  backends.

## 2. What `gogpu/wgpu` actually does

Native (`!rust && !js`):

- `Queue.WriteBuffer` (`queue_native.go`) validates (unmapped, `CopyDst`, 4-byte
  alignment, bounds), then routes through `pendingWrites` + `stagingBelt`
  (`staging_belt.go`): 256 KiB `MapWrite|CopySrc` chunks, bump-allocated,
  persistently host-visible, recycled after the GPU finishes the submission
  ("Steady-state allocation cost: 0 heap allocs per writeBuffer call"). The copies
  are recorded on a shared encoder and prepended to the user command buffers in the
  same `Submit`. This is the Rust `wgpu` `StagingBelt` design; the DX12 path
  deliberately never writes upload heaps directly (BUG-DX12-003, BUG-METAL-001
  comments). Writes larger than a chunk get a one-off staging buffer that is
  destroyed after completion.
- Buffer memory type is decided **only** by `MapRead`/`MapWrite`/`MappedAtCreation`
  (`hal/vulkan/device.go` `CreateBuffer`: "Only MAP_READ/MAP_WRITE buffers (and
  MappedAtCreation) need host-visible memory"). Everything else is device-local.
  There is no "dynamic" hint anywhere in the API.
- Creation cost differs by backend: Vulkan sub-allocates from a buddy allocator
  (`hal/vulkan/memory`); DX12 calls `CreateCommittedResource` per buffer, i.e. a
  dedicated WDDM allocation, which AMD flags as carrying "a relatively high cost"
  ([GPUOpen](https://gpuopen.com/learn/vulkan-device-memory/)) and NVIDIA says to
  avoid ("vkAllocateMemory() is an expensive operation on the CPU",
  [NVIDIA Vulkan Do's and Don'ts](https://developer.nvidia.com/blog/vulkan-dos-donts/)).
- `Buffer.Release` is refcount-driven: a buffer still referenced by an in-flight
  submission stays alive until the GPU completes (`buffer.go` doc comment), so
  releasing the old buffer right after `Submit` (as gfx does) is safe.
- Mapping is fully supported (`Map`, `MapAsync`, `MappedRange`, `Unmap`,
  `MappedAtCreation`), with 8-byte offset / 4-byte size alignment.

Browser (`js && wasm`): `WriteBuffer` is `js.CopyBytesToJS` into a `Uint8Array`
followed by `GPUQueue.writeBuffer`, so per-frame bytes cross Go heap -> JS ->
UA staging -> GPU. A mapped buffer would still need `CopyBytesToJS` into the
mapped `ArrayBuffer`, so mapping saves nothing in WASM (matches toji's advice).

## 3. Does a static/dynamic split still mean anything?

Not at the API or driver level:

- D3D11's `USAGE_DYNAMIC` + `MAP_DISCARD` renaming does not exist in the explicit
  APIs. Microsoft: "The D3D11 method of using Map (with the DISCARD parameter set)
  to rename resources is not supported in D3D12. Applications must implement
  resource renaming themselves", and the recommended pattern is "creating large
  buffers in an UPLOAD heap while creating the frequently accessed GPU resources in
  a DEFAULT heap that has no CPU access"
  ([Microsoft D3D12 docs](https://learn.microsoft.com/en-us/windows/win32/direct3d12/upload-and-readback-of-texture-data)).
  Vulkan guidance is identical: static data in `DEVICE_LOCAL`, staging in
  `HOST_VISIBLE`, and the small `DEVICE_LOCAL|HOST_VISIBLE` heap for hot data
  ([GPUOpen](https://gpuopen.com/learn/vulkan-device-memory/)).
- WebGPU hides even that choice: `MAP_WRITE` excludes every GPU-read usage, so a
  "dynamic vertex buffer in host memory" cannot be expressed. The implementation's
  staging ring is the dynamic path, for every buffer alike.
- In gfx today `BufferDesc.Dynamic` is **dead**: `wgpu/gfxbackend.go` `newBuffer`
  never reads it, and `bakeBuffer` sets it as a pure function of `Kind`
  (`kind == Vertex || kind == Index`), so its only use, the
  `bakedBufferDescs[id] == desc` equality that decides rewrite versus recreate,
  can never be changed by it.

The split that does matter is **ownership and lifetime**, and gfx already encodes
it as two queues: frame-local `OpQueue` uploads that "may be dropped with the
frame", and durable `ResourceQueue` resources the caller releases.

## 4. Rewrite versus recreate

Rewriting the same-size buffer (`bakeBuffer` fast path, `wgpu/gfxbackend.go`)
costs one memcpy into the staging belt plus one GPU copy command. Recreating
(size changed) costs `CreateBuffer` (dedicated allocation on DX12), the same
upload, a bind-group generation bump that invalidates every cached bind group that
referenced the buffer (`bindGroups.invalidateResource`), and a deferred release of
the old buffer. Both are correct; only the second churns allocations and bind
groups. Rule: keep buffer sizes stable frame to frame and grow with headroom.

## 5. What gfx already provides, per lifetime

**Durable (1): `ResourceQueue.BakeBuffer` / `ReleaseBuffer`.** One
`CreateBuffer` + `WriteBuffer` on the render thread; the descriptor ID is stable
for the resource's life. `BakeBuffer` always creates `BufferStorage` kind, which
the wgpu backend gives `Storage|Vertex|Index|CopyDst` usage, so one baked buffer
serves as vertex, index, or storage binding (canvas bakes its quad this way,
`canvas/plugin.go:218`). `copyData=false` avoids the CPU snapshot when the caller
keeps the bytes alive until the render thread consumes the queue. Baked animation
as a storage buffer is bounded by `maxStorageBufferBindingSize` (128 MiB default)
and `maxBufferSize` (256 MiB default) ([MDN limits](https://developer.mozilla.org/en-US/docs/Web/API/GPUSupportedLimits)).

**Per-frame (2): the `OpQueue` temporary arena.** `BufferWithBytes` on a draw or a
`BufferParam` is turned into a temporary buffer by `OpQueue.temporaryBuffer`
(`gfx/opqueue.go`): best-fit by kind and size among the queue's free temporaries,
grown in place when too small, reused across frames, and never released. Because
the three triple-buffered `OpQueue`s each own their temporaries, this is a
3-deep ring by construction; combined with the staging belt there is no CPU/GPU
hazard. Per-draw uniforms (`SetParams`) already get one pooled 256-byte buffer per
draw. Canvas's per-frame `instances` storage buffer (`canvas/batch.go:59`) is
exactly the per-instance-transform pattern scene needs. Cost per frame is one
memcpy into `uploadArena` (if `copyData`), one into the staging belt, one GPU copy;
temporaries recreate only when a frame needs a bigger buffer than any free one
(size growth is monotonic, so this settles after warm-up).

**Dynamic caller-owned (3): `ResourceQueue.ReBakeBuffer`.** Same ID, full
rewrite; same size -> `WriteBuffer` in place, different size -> recreate with the
ID preserved (callers' descriptors stay valid, bind groups are rebuilt). Uploads
happen only on frames where the caller rebakes, unlike the arena which re-uploads
every frame. Misses today: no partial update (`WriteBuffer` supports an offset but
gfx exposes no `UpdateBuffer(buffer, offset, bytes)` analogous to `UpdateTexture`),
and no capacity reservation (no `AllocateBuffer(size)` analogous to
`AllocateTexture`), so a mesh that grows recreates on each growth step.

## Recommendation

- **Durable geometry and baked animation:** `ResourceQueue.BakeBuffer` once per
  loaded model/clip, `copyData=false` where the loader keeps the bytes, released
  through `ReleaseBuffer` by scene's lookup/unload path. No change to gfx.
- **Per-frame data (instance transforms, light lists, per-camera data):**
  `BufferWithBytes` inside the `OpQueue` op (mesh buffer or `BufferParam`), i.e. the
  temporary arena, rebuilt every frame from scene's frame-local state. Pack all
  instances of one camera pass into one storage buffer, and all lights of one
  camera into one storage buffer, so the arena holds a handful of large temporaries
  instead of one per draw. Per-camera constants go through the existing per-draw
  uniform (`SetParams`), or into the same storage buffer when they must be shared
  across many draws without repeating the 256-byte write. No change to gfx.
- **Caller-owned dynamic meshes:** `ResourceQueue.BakeBuffer` at creation and
  `ReBakeBuffer` on the frames the caller changes vertices, sizing the buffer to
  the mesh's maximum vertex count up front so rewrites stay on the same-size
  `WriteBuffer` path. A mesh that is regenerated every frame anyway can simply use
  `BufferWithBytes` and let the arena carry it. No change to gfx for v1.
- **Do not build a mapped staging ring in gfx or scene.** `gogpu/wgpu` already runs
  one under `WriteBuffer`, the browser build gains nothing from mapping, and the
  WebGPU editors recommend `writeBuffer` for per-frame updates.
- **gfx housekeeping (not blocking scene):** delete `BufferDesc.Dynamic` or
  document it as unused; consider `ResourceQueue.UpdateBuffer(buffer, offset,
  bytes)` and `AllocateBuffer(size)` only if a scene demo shows partial vertex
  updates or growth churn on a dynamic mesh.
