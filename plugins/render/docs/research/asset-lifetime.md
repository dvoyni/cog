# Sharing and freeing loaded GPU assets behind pointer-free handles

Research for [#804](https://github.com/dvoyni/cog/issues/804), a child of the
draw map [#800](https://github.com/dvoyni/cog/issues/800). It answers the
asset handles ticket's question
([#806](https://github.com/dvoyni/cog/issues/806)) from the outside first: how
Bevy, Godot, Unity's Addressables and cog's v1 Library name, share, load,
report and free an asset. Then it lays out what each lifetime option would
take in cog: pointer-free handles, kernel tasks and locks, ecs refs. It
records facts and options and decides nothing; every cost is stated for
whichever option it belongs to.

What was read, 2026-11-12:

| Source | Version | What |
|---|---|---|
| Bevy `bevy_asset`, `bevy_render` | tag `v0.20.1`, commit `a0577b5c6a7b208719dd761c5c6a7a539e472a97` | `handle.rs`, `id.rs`, `assets.rs`, `event.rs`, `loader.rs`, `lib.rs`, `server/mod.rs`, `server/info.rs`, `render_asset.rs` (both crates), `render_resource/bind_group.rs` |
| Godot | tag `4.7.2-stable`, commit `ed1daf0bf001b61586d9930840f2f1394092c079` | `core/io/resource.{h,cpp}`, `core/io/resource_loader.{h,cpp}`, `core/object/ref_counted.h`, `core/templates/rid_owner.h`, `scene/resources/{image_texture,mesh,material}.cpp`, the class reference XML of `Resource`, `RefCounted`, `ResourceLoader`, `RID`, `RenderingServer`, `Image`, `ImageTexture` |
| Unity Addressables | manual and API 2.7.6; older manuals 1.20.5, 1.21.21, 1.13.1 where 2.7.6 is silent | memory management, operation handles, unloading |
| cog v1 | `_legacy/libs/assets` in this repo, map [#443](https://github.com/dvoyni/cog/issues/443) and its closed children | the Library's README and spec, the map's two build comments |
| cog v2 | `main` at `f3d0dcc` | `kernel/`, `ecs/`, `gfx/`, `platform/` docs and code |

## At a glance

- All three engines count an asset's holders, Bevy and Godot by the
  language (`Arc`, `RefCounted`), Unity by paired load and release calls.
  Bevy and Godot put a plain, uncounted id beside the counted owner: Bevy's
  `Copy` `AssetId` beside its `Handle`, Godot's `RID`, which a resource
  hands the rendering server, beside its `Ref<Resource>`. cog's pointer-free
  handle is the plain id; nothing in Go counts its copies.
- cog already has two lifetimes of the uncounted kind: gfx's handles,
  released immediately and stale everywhere at once, and ecs refs, released
  by the user, with counting holders recorded under *Decided against*. v1's
  Library did not count either, and it found no caller that ever freed.
- The kernel shapes loading more than any engine does: args, results and
  events carry no strings or slices, so a path or decoded bytes never travel
  between jobs; a job holds its whole lock set for its run; jobs cannot be
  cancelled and always complete; work is scheduled only inside a
  publication.
- Dependencies are counted by containment in all three engines: a material
  holds owning references to its textures and shader, and freeing the
  material can free them. Unity's manual shows the cost: a texture shared
  by two levels' materials unloads and reloads at the level change.
- A blob has no identity of its own. Bevy does not deduplicate assets added
  by value, Godot caches nothing that has no path, and v1 made a blob's
  identity its backing array and length, which only works for bytes built
  once and kept.

## 1. The four systems side by side

| | Bevy 0.20.1 | Godot 4.7.2 | Unity Addressables 2.7.6 | cog v1 Library |
|---|---|---|---|---|
| **What a user holds** | `Handle<A>`: `Strong(Arc<StrongHandle>)` or `Uuid` ([handle.rs#L115-L122](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/handle.rs#L115-L122)) | `Ref<T>` to a `Resource`, a `RefCounted` object ([Resource.xml#L7-L9](https://github.com/godotengine/godot/blob/4.7.2-stable/doc/classes/Resource.xml#L7-L9)) | `AsyncOperationHandle<T>`, a struct ([API](https://docs.unity3d.com/Packages/com.unity.addressables@2.7/api/UnityEngine.ResourceManagement.AsyncOperations.AsyncOperationHandle-1.html)) | the loader's own `T`, returned by `Cache.Get` ([README](../../../_legacy/libs/assets/docs/README.md)) |
| **Plain id beside it** | `AssetId<A>`: `Copy`, an `AssetIndex{generation, index}`, "not directly tied to the lifetime of the Asset" ([id.rs#L15-L35](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/id.rs#L15-L35)) | `RID`: 64 bits, an index and a validator, freed explicitly on its server ([RID.xml#L6-L11](https://github.com/godotengine/godot/blob/4.7.2-stable/doc/classes/RID.xml#L6-L11), [rid_owner.h#L157-L163](https://github.com/godotengine/godot/blob/4.7.2-stable/core/templates/rid_owner.h#L157-L163)) | none read | none: the `Descr` is the key, "never the handle" |
| **What makes two loads one** | path and asset type: `path_to_index` ([info.rs#L123](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/server/info.rs#L123)) | path: `ResourceCache`, a map of path to `Resource*` ([resource.h#L197-L215](https://github.com/godotengine/godot/blob/4.7.2-stable/core/io/resource.h#L197-L215)) | the address: three loads of one address give three handles on one operation with a count of three ([1.13 manual](https://docs.unity3d.com/Packages/com.unity.addressables@1.13/manual/MemoryManagement.html)) | `Descr{Name, Blob, Params}`, comparable; `Name` wins over `Blob` |
| **Who counts holders** | `Arc`: cloning a strong handle counts, dropping it sends a `DropEvent` on a channel ([handle.rs#L81-L96](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/handle.rs#L81-L96)) | `RefCounted`; `Ref<T>`'s destructor deletes the object at zero ([ref_counted.h#L199-L228](https://github.com/godotengine/godot/blob/4.7.2-stable/core/object/ref_counted.h#L199-L228)) | the system: every load increments, every `Release` decrements ([memory-assets](https://docs.unity3d.com/Packages/com.unity.addressables@2.7/manual/memory-assets.html)) | nobody: "It does not refcount. An entry is freed when it is freed." |
| **When it is freed** | at the next `track_assets` in `PreUpdate`, which drains the drop channel ([assets.rs#L570-L588](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/assets.rs#L570-L588), [lib.rs#L680-L688](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/lib.rs#L680-L688)) | at once, when the last `Ref` goes; its destructor leaves the cache ([resource.cpp#L790-L802](https://github.com/godotengine/godot/blob/4.7.2-stable/core/io/resource.cpp#L790-L802)) and frees its `RID` ([image_texture.cpp#L243-L248](https://github.com/godotengine/godot/blob/4.7.2-stable/scene/resources/image_texture.cpp#L243-L248)) | at zero it "can be unloaded"; its memory goes only when its AssetBundle unloads ([memory-assets](https://docs.unity3d.com/Packages/com.unity.addressables@2.7/manual/memory-assets.html)) | at `Free`, `FreeAll` or `FreeWhere`, immediately, no queue |
| **Where a load runs** | a task on the IO pool ([mod.rs#L408-L447](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/server/mod.rs#L408-L447)) | on the caller, or threads with `load_threaded_request` ([ResourceLoader.xml#L95-L139](https://github.com/godotengine/godot/blob/4.7.2-stable/doc/classes/ResourceLoader.xml#L95-L139)) | asynchronous operations; "is not able to be canceled once started" ([1.20 manual](https://docs.unity3d.com/Packages/com.unity.addressables@1.20/manual/AddressableAssetsAsyncOperationHandle.html)) | inside `Get`, synchronously: "Everything finishes inside `Load`" |
| **What a holder sees before it is ready** | the handle at once; `Assets::get` answers nothing; `LoadState::Loading` | no handle: poll `load_threaded_get_status(path)`, or block in `load_threaded_get` | the handle at once; `Status` is `None`, "still in progress" ([API](https://docs.unity3d.com/Packages/com.unity.addressables@2.7/api/UnityEngine.ResourceManagement.AsyncOperations.AsyncOperationStatus.html)) | nothing is ever pending |
| **How a failure is reported** | `LoadState::Failed(Arc<AssetLoadError>)`, an `AssetLoadFailedEvent<A>{id, path, error}` message, and an `error!` log ([mod.rs#L1890-L1940](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/server/mod.rs#L1890-L1940), [event.rs#L10-L18](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/event.rs#L10-L18)) | `load` returns an empty resource and prints an error; threaded status `THREAD_LOAD_FAILED` | `Status` `Failed`, `OperationException` | the Library reports the read once, keyed by the `Descr`; the loader reports decode faults; `Default` supplies a value |
| **Retry after a failure** | the next `load` of the path loads again: a request reloads when the state is `NotLoaded` or `Failed` ([info.rs#L280-L289](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/server/info.rs#L280-L289)) | a failed load caches nothing, so the next `load` tries again (inferred from the cache holding only live resources) | "still release the operation handle" ([2.7.6](https://docs.unity3d.com/Packages/com.unity.addressables@2.7/manual/AddressableAssetsAsyncOperationHandle.html)); then load again | none until a `Free`: "Failure is terminal" |
| **Dependencies** | strong handles inside the asset value, `#[dependency]`; three load states; `LoadedWithDependencies` | `Ref`s held by the resource: a material's `Ref<Texture2D> textures[]` ([material.h#L631](https://github.com/godotengine/godot/blob/4.7.2-stable/scene/resources/material.h#L631)) | AssetBundles counted as dependencies of the assets in them | a loader re-enters other caches inside `Load`; nothing cascades |
| **Hot reload** | `file_watcher` feature; a changed file reloads into the same index, its loader dependents with it ([mod.rs#L1944-L2010](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/server/mod.rs#L1944-L2010)) | `CACHE_MODE_REPLACE` refreshes cached instances in place ([ResourceLoader.xml#L174-L176](https://github.com/godotengine/godot/blob/4.7.2-stable/doc/classes/ResourceLoader.xml#L174-L176)); no watcher read | not covered by the pages read | none: the asset filesystem was found static ([#449](https://github.com/dvoyni/cog/issues/449)) |
| **Bytes given directly** | `Assets::add(value)`, not deduplicated ([lib.rs#L65-L73](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/lib.rs#L65-L73)); in-memory and `embedded://` sources give bytes a path | `Image.load_png_from_buffer` then `ImageTexture.create_from_image`: no path, no cache entry, unless `take_over_path` | not covered | `Descr.Blob`, a pointer and a length whose identity is the backing array |
| **CPU copy after upload** | `RenderAssetUsages`: `RENDER_WORLD` alone drops the CPU data once prepared ([render_asset.rs#L4-L38](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/render_asset.rs#L4-L38)) | not read | not read | the loader's `T` decides |

### Bevy 0.20.1

- **Strong handles count by `Arc`.** `StrongHandle` holds the index, the
  type, the path and a channel sender; its `Drop` sends a `DropEvent`
  ([handle.rs#L78-L96](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/handle.rs#L78-L96)).
  `Handle::Weak` was replaced by `Handle::Uuid` in
  [PR #19896](https://github.com/bevyengine/bevy/pull/19896) (merged
  2025-07-02); a `Uuid` handle "will not result in the asset being dropped".
  The crate docs still speak of weak handles
  ([lib.rs#L99-L103](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/lib.rs#L99-L103)).
- **Freeing is a sweep.** `Assets::<A>::track_assets` runs in `PreUpdate`
  after `handle_internal_asset_events`, drains the drop channel holding the
  server's info lock "for the entire duration of this function", and removes
  each asset whose last handle went
  ([assets.rs#L570-L588](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/assets.rs#L570-L588)).
  Removal queues `AssetEvent::Unused` and `Removed`
  ([assets.rs#L492-L512](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/assets.rs#L492-L512))
  and recycles the index with its generation incremented
  ([assets.rs#L70-L91](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/assets.rs#L70-L91)).
- **A load between the last drop and the sweep revives the asset.** The
  path's info keeps a `Weak<StrongHandle>`; when it no longer upgrades, a new
  strong handle is made for the same index and `handle_drops_to_skip` is
  incremented, so the late drop does not remove it
  ([info.rs#L291-L311](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/server/info.rs#L291-L311),
  [info.rs#L768-L776](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/server/info.rs#L768-L776)).
- **The last drop cancels a load in flight:** the info's pending task is
  removed ([info.rs#L778](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/server/info.rs#L778)),
  and dropping an `async_task::Task` "cancels it"
  ([async-task task.rs#L19-L21](https://github.com/smol-rs/async-task/blob/master/src/task.rs#L19-L21),
  re-exported by `bevy_tasks`). A completion arriving after the drop finds no
  info and is dropped
  ([info.rs#L467-L470](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/server/info.rs#L467-L470)).
- **Three load states per asset:** its own `LoadState` and its direct and
  recursive dependencies' (`NotLoaded`, `Loading`, `Loaded`,
  `Failed(Arc<AssetLoadError>)`,
  [mod.rs#L2114-L2215](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/server/mod.rs#L2114-L2215)).
  `AssetEvent` reports `Added`, `Modified`, `Removed`, `Unused` and
  `LoadedWithDependencies`
  ([event.rs#L50-L61](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/event.rs#L50-L61)).
- **The renderer follows the last handle, not the value.** The render world
  extracts on `Added` and `Modified` and removes its GPU copy on `Unused`,
  ignoring `Removed`
  ([bevy_render render_asset.rs#L318-L345](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_render/src/render_asset.rs#L318-L345)).
  A render asset that cannot be prepared yet answers `RetryNextUpdate`; a
  material bind group does so while "the Texture for an Image hasn't loaded
  yet" ([bind_group.rs#L110-L113](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_render/src/render_resource/bind_group.rs#L110-L113)).
- **Assets added by value are not shared:** "no general mechanism currently
  exists to deduplicate procedural assets"
  ([lib.rs#L65-L73](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/lib.rs#L65-L73));
  `Assets::add` reserves a fresh index each call
  ([assets.rs#L401-L405](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/assets.rs#L401-L405)).
  `AssetServer::add_async(future)` gives a handle at once and reports the
  future's error as `AddAsyncError`
  ([mod.rs#L878-L940](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/server/mod.rs#L878-L940)).

### Godot 4.7.2

- **Two layers.** A `Resource` is reference counted; the engine keeps "a
  global cache of all loaded resources, referenced by paths", and a resource
  "is removed from cache once all references are released"
  ([Resource.xml#L7-L9](https://github.com/godotengine/godot/blob/4.7.2-stable/doc/classes/Resource.xml#L7-L9)).
  Beneath it, a server object is named by an `RID`, which a resource frees in
  its destructor: `ImageTexture`, `ArrayMesh` and `Material` each call
  `RenderingServer::free_rid`
  ([image_texture.cpp#L243-L248](https://github.com/godotengine/godot/blob/4.7.2-stable/scene/resources/image_texture.cpp#L243-L248),
  [mesh.cpp#L2363-L2368](https://github.com/godotengine/godot/blob/4.7.2-stable/scene/resources/mesh.cpp#L2363-L2368),
  [material.cpp#L188-L193](https://github.com/godotengine/godot/blob/4.7.2-stable/scene/resources/material.cpp#L188-L193)).
  Used directly, the server does no memory management: `free_rid` "should be
  called after using an object"
  ([RenderingServer.xml#L1628-L1634](https://github.com/godotengine/godot/blob/4.7.2-stable/doc/classes/RenderingServer.xml#L1628-L1634)).
- **The cache holds no reference.** `ResourceCache::resources` is a map of
  path to raw `Resource*`; a lookup that finds an object whose count already
  reached zero treats it as gone
  ([resource.cpp#L852-L870](https://github.com/godotengine/godot/blob/4.7.2-stable/core/io/resource.cpp#L852-L870)).
  Setting a path a live resource already has fails unless it is a take-over
  ([resource.cpp#L79-L115](https://github.com/godotengine/godot/blob/4.7.2-stable/core/io/resource.cpp#L79-L115)).
- **Threaded loads are shared by path.** A second request for a path in
  flight attaches to its task and counts in the token's `user_rc`
  ([resource_loader.cpp#L700-L722](https://github.com/godotengine/godot/blob/4.7.2-stable/core/io/resource_loader.cpp#L700-L722),
  [resource_loader.cpp#L769-L812](https://github.com/godotengine/godot/blob/4.7.2-stable/core/io/resource_loader.cpp#L769-L812)).
  Status is `THREAD_LOAD_INVALID_RESOURCE`, `IN_PROGRESS`, `FAILED` or
  `LOADED`
  ([ResourceLoader.xml#L156-L167](https://github.com/godotengine/godot/blob/4.7.2-stable/doc/classes/ResourceLoader.xml#L156-L167)).
- **Cache modes** decide whether a load reuses, ignores or refreshes cached
  instances, shallow or deep: `IGNORE`, `REUSE` (the default), `REPLACE`,
  `IGNORE_DEEP`, `REPLACE_DEEP`
  ([ResourceLoader.xml#L168-L181](https://github.com/godotengine/godot/blob/4.7.2-stable/doc/classes/ResourceLoader.xml#L168-L181)).
- **A material owns its textures and hands their `RID`s down:**
  `set_texture` stores the `Ref<Texture2D>` and sets the shader parameter to
  `p_texture->get_rid()`
  ([material.cpp#L2508-L2513](https://github.com/godotengine/godot/blob/4.7.2-stable/scene/resources/material.cpp#L2508-L2513)).
  Cycles of `RefCounted`s are never freed
  ([RefCounted.xml#L7-L9](https://github.com/godotengine/godot/blob/4.7.2-stable/doc/classes/RefCounted.xml#L7-L9)).

### Unity Addressables 2.7.6

- **Counted by load and release:** "When Unity loads an Addressable asset,
  the system increments the reference count. When Unity releases the asset,
  the system decrements the reference count"; the manual's advice is to
  "mirror every call to a load method with a call to a release method"
  ([memory-assets](https://docs.unity3d.com/Packages/com.unity.addressables@2.7/manual/memory-assets.html)).
  `AsyncOperationHandle<T>.Release()`: "If the internal operation reference
  count reaches 0, the resource will be released"; `IsValid()` checks "if the
  handle references an internal operation"
  ([API](https://docs.unity3d.com/Packages/com.unity.addressables@2.7/api/UnityEngine.ResourceManagement.AsyncOperations.AsyncOperationHandle-1.html)).
- **Zero frees nothing by itself.** "Unity doesn't unload released assets
  from memory immediately"; an asset's memory "isn't freed until the
  AssetBundle it belongs to is also unloaded", and "you can't unload part of
  an AssetBundle"
  ([memory-assets](https://docs.unity3d.com/Packages/com.unity.addressables@2.7/manual/memory-assets.html),
  [1.21](https://docs.unity3d.com/Packages/com.unity.addressables@1.21/manual/MemoryManagement.html)).
  `Resources.UnloadUnusedAssets` is "a slow operation which can cause frame
  rate hitches" (1.21).
- **Dependencies thrash at a level change:** two materials, boat and plane,
  share a texture, cammo, in its own bundle; "When Unity releases boat,
  Addressables unloads texture cammo. Then, when Unity load plane,
  Addressables immediately reloads cammo"
  ([memory-assets](https://docs.unity3d.com/Packages/com.unity.addressables@2.7/manual/memory-assets.html)).
- **A release during a load is no cancel.** The 1.20.5 manual: an operation
  "is not able to be canceled once started", and releasing its handle before
  it finishes decrements the count, the operation releasing itself when it
  completes ([1.20](https://docs.unity3d.com/Packages/com.unity.addressables@1.20/manual/AddressableAssetsAsyncOperationHandle.html)).
  The 2.7.6 pages read do not say; unverified for 2.x.

### cog v1's Library

From [`_legacy/libs/assets/docs/README.md`](../../../_legacy/libs/assets/docs/README.md)
and [`specs/assets.md`](../../../_legacy/libs/assets/docs/specs/assets.md),
built from map [#443](https://github.com/dvoyni/cog/issues/443):

- **One cache per asset family**, `Cache[P, U, T]`, keyed by
  `Descr[P]{Name, Blob, Params}`, with a stateless `Loader` whose `Load`,
  `Default` and `Free` get everything lock-bound per call. Plugin-internal:
  "a game cannot tell the plugins share a caching implementation".
- **No refcount, no queue, no in-flight state.** `Free`, `FreeAll` and
  `FreeWhere` free at the call; "a free followed by a get is a reload, not an
  error". `FreeWhere` exists because "a release decision can read the value":
  every shader whose include set holds a path.
- **Failure is terminal and reported once**; `Free` is the retry lever and
  the paired forget of the report. `Default` is a null object: magenta for an
  sRGB slot, zero for a linear one, a skip where nothing should draw.
- **What it cost:** scene's ~300-line `Missing → Loading → Resident →
  Failed` state machine, its generation counter (for "an unload landing while
  a load is in flight") and its release queue were deleted, and scene's flush
  took the parse and decode inside the frame, `Preload` being "the documented
  lever" ([spec, scene](../../../_legacy/libs/assets/docs/specs/assets.md#scene-the-state-machine-deletes)).
  The five unload verbs had "zero non-test call sites".
- **Left open by the build** ([#443, comments](https://github.com/dvoyni/cog/issues/443)):
  `Load` does not receive the `Descr`, so two migrations carried the name in
  `U`; a texture that opens but fails to decode was cached and silent; two
  cache tiers sharing one `P` type shared one report-once namespace.
- **v1's model library deferred buffer releases** to the frame boundary "so
  nothing the frame already recorded draws from a dead buffer"
  ([lookup.go#L37-L40](../../../_legacy/bundles/model/internal/lookup.go#L37-L40)).

## 2. cog's ground

What the repo fixes for any option, with what it means for draw's assets.

| Fact | Where | For draw's assets |
|---|---|---|
| Task args, task results and events hold no pointers, slices, maps, strings, interfaces, channels or funcs. "Text a task needs travels as a hash or an ID, with the text itself kept in a resource; so does any other bulk data (paths, buffers, lists)." | [kernel design, *Pointer-free data*](../../../kernel/docs/design.md) | A load request names its asset by handle or ID; the path, the bytes read and the decoded data live in draw's resources, never in an arg, a result or an event. A failure event cannot carry the error. |
| Work is scheduled only inside a publication; a task wanting jobs run publishes an event. A job may publish on the kernel, which starts a separate batch. | [`kernel.go` `Publish`](../../../kernel/kernel.go), [kernel ADR 0003](../../../kernel/docs/adr/0003-jobs-are-scheduled-only-inside-a-publication.md) | A load starts from an event's handler; a staged load chains its stages by feeding within one batch or by publishing again. |
| A task's lock set is fixed at registration and held for the job's run. Jobs whose locks conflict run in no order; "a waiting writer reserves what it writes". | [kernel design, *Tasks and jobs*](../../../kernel/docs/design.md) | A long job under `kernel.Read[gfx.Device]` delays every writer of the device, a surface configure and the frame behind it among them. |
| "Jobs always complete. There is no failed job." A panicking body is recovered to `LogErr` and finishes with the zero result. | same | No load can be cancelled; a free during a load has to be noticed when the load commits. |
| `LogErrOnce(key string, err)` logs once per key until `ForgetLogOnce(key)`; "A condition that can be repaired must be forgotten." | [kernel design, *Errors and logging*](../../../kernel/docs/design.md) | v1 keyed its report by the `Descr`; v2 keys are strings, built from the path at a cost logging is exempt from. |
| A resource is never kept outside the function that reached it. | [`AGENTS.md`](../../../AGENTS.md), *Plugins* | A loader reaches `platform.FS` and `gfx.Device` per job; draw keeps neither. |
| gfx handles are generational and pointer-free; release is immediate, wgpu keeping objects alive until the submits using them finish; a stale handle answers `ErrReleased`, and in a recording call is a deferred error. | [gfx design, *Types, descriptors and handles*](../../../gfx/docs/design.md) | Freeing a GPU object under a frame in flight is safe for the GPU; a recording job still holding the handle is not (see 3.3). |
| Creation is synchronous; "a compile holds the read lock", and "a burst of compiles can take every worker from a frame's recording, so the game bounds how many run at once". gfx has no async call and no worker. | [gfx design, *Shaders and pipelines*](../../../gfx/docs/design.md) | Bounding concurrent loads is draw's or the game's. |
| Handles survive device loss; buffers and textures come back empty, "owners refill them from tasks they schedule on `DeviceRecoveredEvent`". | [gfx ADR 0003](../../../gfx/docs/adr/0003-handles-survive-device-loss.md) | draw refills its assets: from CPU copies it keeps, or by loading again; a blob can be refilled only from a copy. |
| "A handle fits a component registered transient; scene saves the asset and recreates the handle on load." | [gfx design, *Constraints*](../../../gfx/docs/design.md) | A draw handle is a run-time ID; what a save keeps is the asset's name. |
| gfx leaves "sharing pipelines of one descriptor" to the layer above. | [gfx design, *Shaders and pipelines*](../../../gfx/docs/design.md) | Pipelines are draw's to share, keyed by their descriptor, with a shader module beneath them. |
| An ecs `Ref` is pointer-free and stable; "Released by the user. The ECS does not count the components holding a ref"; counting holders is under *Decided against*. | [ecs design, *Refs*](../../../ecs/docs/design.md#refs) | Nothing in ecs releases a draw handle when the last component holding it goes. |
| Keyed refs: one ref per key, `Put` returns the key's ref or makes one; `Put` cannot be recorded in a command buffer. | [ecs design, *Keyed refs*](../../../ecs/docs/design.md#keyed-refs) | A world can share one ref per asset key; the system that puts holds the keyed refs' write. |
| `DeltaRefs` reports a ref released, with its last value; a delta query reports a component removed, a despawn included, with its last value. Observation is off until the first observation reset, which the user places; "Writes after a store's last observer are reset unseen." | [ecs design, *Change detection*, *Ref deltas*](../../../ecs/docs/design.md#change-detection) | scene can see a handle leave a component, a window late, in a world that observes. |
| Hooks on add, remove and set are deferred; "A remove hook could release a component's refs without the user recording it." | [ecs design, *Deferred*](../../../ecs/docs/design.md#deferred) | Not available today. |
| `platform.FS` is `fs.FS`, read under `kernel.Read[platform.FS]`; "files opened from it are valid only while the job runs". The writable filesystem is searched before every read mount. Read mounts are sockets bound in `Init`. | [`platform/resources.go`](../../../platform/resources.go), [`platform/sockets.go`](../../../platform/sockets.go) | Bytes read are copied out within the job. A write through `ElevateFileAccess` to a path an asset was read from shadows the read mount at run time, which v1's "static by construction" premise did not have to meet. |

## 3. Holding and freeing a handle a component copies

### 3.1 Why the collector cannot count it

A handle copied into a component is a value: an index and a generation, as
gfx's `handle` and ecs's `Ref` are. Go's collector tracks objects, not
values: `runtime.AddCleanup` attaches to a pointer, `ptr *T`
([mcleanup.go#L17-L31, L76](https://github.com/golang/go/blob/master/src/runtime/mcleanup.go#L17-L31)),
and a pointer-free component never holds one, which is the point of ecs's
[ADR 0003](../../../ecs/docs/adr/0003-components-are-pointer-free-and-variable-data-lives-in-refs.md).
Bevy and Godot get counting from `Arc` and `RefCounted` because their handle
is a pointer; their plain ids (`AssetId`, `RID`) count nothing, exactly as
cog's would not.

### 3.2 The options

Five shapes, each built from what the surveyed systems or cog already do.
They combine: residency (C) is orthogonal to who decides it (A, B, E), and D
moves the holding into a world.

| | A. Counted in draw | B. Owner frees, nothing counted | C. Handle outlives residency | D. ecs keyed refs own the handle | E. scene counts from deltas |
|---|---|---|---|---|---|
| **Precedent** | Addressables; Bevy and Godot, with the count automatic | v1 Library, gfx handles, ecs refs, Godot's `RID`s | gfx recovery (ADR 0003); Bevy reloading into the same index; Godot `CACHE_MODE_REPLACE` | ecs keyed refs, "meshes by asset path" | Bevy's drop at despawn, done by change detection |
| **What draw keeps** | per slot: key, generation, state, gfx objects, a holder count | per slot: key, generation, state, gfx objects | per slot as B or A, plus a not-resident state the slot returns to | as A or B; draw unaware of worlds | as A |
| **Who ends a life** | the last `Release` | whoever owns the lifetime: the game at a level boundary, scene | eviction frees contents; the slot is forgotten explicitly or never | scene releasing the ref; a system over `DeltaRefs` then releases the draw handle | scene's system, on `DeltaRemoved` and `DeltaChanged` |
| **A copy of the handle** | owns only if paired with an `Acquire`; an unpaired copy is weak, as Bevy's `AssetId` | costs nothing; every copy goes stale together | costs nothing; never goes stale while the slot lives | a `Ref` copy costs nothing; ecs counts none | costs nothing; the store is what is counted |
| **Locks** | count changes under draw's table write, or atomics under its read with the zero crossing queued for a sweep under the write (Bevy's drop channel and `track_assets`) | the free under draw's table write and `kernel.Read[gfx.Device]`; nothing on the spawn path | eviction as B; a reload as any load | `KeyedRefs` write to put; `KeyedRefsLookup` read, or none if the component also holds the draw handle | the counting system reads the observed store and writes draw's counts |
| **Main costs** | a missed release leaks for the run; a double release frees under another holder, and the generation cannot tell, since the count is the slot's, not the copy's; nothing in ecs releases on despawn, so scene still has to drive it (E) | the owner must know nobody else needs it; a stale handle draws nothing; a free then a load gives a new generation, so components keep the stale one; v1 found nobody calling free | slots grow with every distinct key of the run; a holder cannot tell evicted from not yet loaded without a state; something must ask for the reload | the question of when to release moves to scene, not away; one draw holder per world and key, so two worlds hold two; a draw through a `Ref` is one more lookup (2.6 ns a read by handle, [ecs perf](../../../ecs/docs/design.md#performance)) | the world must observe, and the store keeps a shadow and a state byte per row; the system must run after every writer and before every reset, or counts drift; releases land a window late |

**A in detail.** `Load(key)` answers the key's handle and counts one;
`Acquire(handle)` counts a copy that should own; `Release(handle)` uncounts.
Kept as atomics in slots that never move (as ecs's refs page their slots),
counting runs under draw's read lock in parallel, and the zero crossing
appends the slot to a list a sweep job frees under the write. A `Load` of
the key between the crossing and the sweep must revive the slot, which is
the race Bevy's `handle_drops_to_skip` exists for. Unity's manual states the
discipline A needs: "mirror every call to a load method with a call to a
release method."

**B in detail.** The table maps key to handle; a load finds the existing
handle and counts nothing. `Free(handle)`, `FreeWhere(match)` and `FreeAll`
retire handles at once, as gfx's releases do. It is the lifetime cog's
lower layers already have: "a holder left behind reads nothing, as a stale
`Entity` does" ([ecs, *Refs*](../../../ecs/docs/design.md#refs)). v1 measured
its use: the unload verbs "are game-facing API exclusively, and unloading is
a level-boundary act" with "zero non-test call sites".

**C in detail.** The handle names a key's slot, not one load of it. Eviction
releases the slot's gfx objects and CPU copy and sets it not resident; a
later request reloads into the same slot under the same generation, so no
component goes stale. It is gfx's own recovery one level up, and the same
path a device loss already asks of draw: gfx keeps the handles a slot holds,
and draw refills what they contain. What frees the slot itself is a
separate verb, or nothing for the run.

**D in detail.** scene, which may import ecs, registers per world
`KeyedRefs[W, AssetKey, MeshAsset]`, a value holding the key and the draw
handle. Components hold `ecs.Ref[MeshAsset]`; `Put` returns the world's
one ref for a key. Releasing the ref is scene's, and a system walking
`DeltaRefs` finds it `released` with its last value and releases the draw
handle, once per ref whatever number of components held it. A reload
`Update`s the value, reported as a change, with no component changing.

**E in detail.** scene registers a delta query over each component holding
draw handles. Per window: `DeltaAdded` acquires the value now;
`DeltaChanged` releases the previous value and acquires the new one when
they differ; `DeltaRemoved`, despawn included, releases the last value. The
shadow makes it net: A→B→A within a window is no delta, A→B→C releases A and
acquires C. A loaded save and a mirrored `CreateAt` report as adds, so they
are counted. A delta query over 10,000 rows with a tenth written costs
18 µs, a listed change about 18 ns
([ecs perf](../../../ecs/docs/design.md#performance)). A world never reset
observes nothing and counts nothing.

### 3.3 When a free may land

| Fact | Source |
|---|---|
| A gfx release is immediate; wgpu keeps the object until the submits using it finish. | [gfx design](../../../gfx/docs/design.md), *Types, descriptors and handles* |
| A stale handle in a recording call is a deferred error; a frame encoder's errors go to `LogErr` at the flush, and "an encoder whose `Finish` fails goes to `LogErr` and is dropped". A free racing a recording job that still uses the handle can therefore cost that job's whole encoder for the frame. | [gfx design](../../../gfx/docs/design.md), *Frames*; [`gfx/encoders.go`](../../../gfx/encoders.go); the drop of the encoder is read from the design, not tested |
| Kept apart by locks: a free under draw's table write cannot overlap a recording job holding its read. Kept apart by order: gfx schedules `ReleaseSurface` "after the last present", the same shape for an asset. | [kernel design](../../../kernel/docs/design.md), [gfx design, *Surfaces*](../../../gfx/docs/design.md) |
| v1's model library released buffers at the frame boundary for this reason; v1's Library freed at the call, its spec finding that both plugins "drain before they resolve", so a same-tick unload reloaded rather than dangled. | [lookup.go](../../../_legacy/bundles/model/internal/lookup.go#L37-L40), [spec, *Release*](../../../_legacy/libs/assets/docs/specs/assets.md#release) |
| Bevy frees in `PreUpdate`, before the frame's systems; its render world drops a GPU copy on `Unused` at extraction. | [lib.rs#L418-L438](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/lib.rs#L418-L438), [render_asset.rs#L335-L339](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_render/src/render_asset.rs#L335-L339) |

### 3.4 Saves and mirrors

- **gfx's rule carries to draw:** a component holding a draw handle is
  registered transient, and the asset's name is what is saved.
- **Refs save by value.** A saved ref is written as a number and remapped on
  load; a saved keyed ref whose key already has a ref in the world takes that
  ref ([ecs, *Saves*](../../../ecs/docs/design.md#saves)). A ref value is
  marshaled whole by `encoding/json/v2`; a draw handle with unexported fields
  in it would save nothing and load as zero (json/v2 skips unexported fields;
  not tested here), so D re-resolves the handle from the key on the load's
  `added` deltas.
- **Counts are not saved.** Under A a loader of a save acquires for what it
  loads; under E the load's adds are counted by themselves.
- **Mirrors:** a draw handle is meaningful only in its kernel, as gfx's
  are. ecs's ref ids are wire ids ([*Fit for networking*](../../../ecs/docs/design.md#fit-for-networking));
  a mirrored component holding a server's draw handle would name nothing on
  the client, so what crosses is a key or a ref.

## 4. Loading off the frame

### 4.1 The stages and what each locks

| Stage | Needs | Lock | What reaches the next stage |
|---|---|---|---|
| Ask | find the key's slot or make one, set it loading | draw's table: its write, or a mutex inside it under its read, as gfx's table takes ([`gfx/table.go`](../../../gfx/table.go)) | the handle, pointer-free |
| Read | the file | `kernel.Read[platform.FS]`; the file is valid only while the job runs | bytes, which a result cannot carry: they go into a resource |
| Decode | CPU: an image, a glTF, a mesh pack | none of its own | decoded data, in a resource |
| Preprocess a shader | `Device.PreprocessShader(files, path, defines)`, reading includes | `kernel.Read[gfx.Device]` and `kernel.Read[platform.FS]` ([gfx, *Shaders*](../../../gfx/docs/design.md)) | final WGSL |
| Create and upload | `Create*`, `WriteBuffer`, `WriteTexture` | `kernel.Read[gfx.Device]` | gfx handles |
| Commit | slot ready, its gfx objects in place; tell whoever waits | draw's table | an event: handle and status |

### 4.2 Shapes of a load

| Shape | What it takes | Costs |
|---|---|---|
| **Synchronous, in the asking job** (v1) | the job that first asks holds the FS, the device and draw's table | no in-flight state at all; the frame takes the hitch, and preloading is the lever, as v1 documented |
| **One job per asset** | a task holding `Read[platform.FS]`, `Read[gfx.Device]` and draw's table, scheduled from a request event's handler | the decode runs under the device's read lock, so a configure, a recovery or `WaitIdle` waits for it; under the FS read, so a values flush waits |
| **Staged jobs** | read and decode under `Read[platform.FS]`; create and upload under `Read[gfx.Device]`, fed the handle; decoded data parked in a draw resource between them | the parking resource has its own locks and holds the bytes between stages; only the upload holds the device |
| **Decode outside the kernel** | a job copies the bytes out; a goroutine of draw's decodes; completion is published into the kernel, as code outside it publishes, the handlers running on the publisher's goroutine ([kernel, *Interop* and *Waiting from outside*](../../../kernel/docs/design.md)) | outside the kernel's workers and their accounting; a pool of draw's own; the copy |

How many loads run at once is not bounded by gfx: its design hands that to
the game. A burst of loads, each a job, takes workers from the frame's
recording as a burst of compiles does.

### 4.3 What a handle answers before its asset is ready

| Answer | Who does it | What it takes in cog |
|---|---|---|
| The handle at once; reading it finds nothing; draws of it are skipped | Bevy: `Assets::get` answers nothing, the render world has no prepared copy, a bind group answers `RetryNextUpdate` | a state per slot; every draw path checks it |
| A status to poll | Godot by path, Unity by handle (`None` while in progress) | `State(handle)` on a lookup under draw's table read |
| An event when ready | Bevy `Added` and `LoadedWithDependencies`; Unity `Completed` | a pointer-free event of handle and status, published from the commit job |
| A placeholder until ready | v1's `Default`, a null object chosen per slot kind (magenta for sRGB colour, zero for linear data, a skip) | the slot answers default gfx objects; a bind group built over a placeholder is built again when the real texture lands |
| A blocking get | Godot's `load_threaded_get` blocks the caller | a job blocking on another job's `Signal` can deadlock the kernel ([kernel, *Waiting from outside*](../../../kernel/docs/design.md)) |
| Nothing pending, ever | v1 | the synchronous shape of 4.2 |

### 4.4 Reporting a failure

- **Events carry no error.** A failure event can hold the handle and a
  status code; the error itself stays in draw's slot, where a resource may
  hold pointers, and goes to `LogErr`. Bevy's `AssetLoadFailedEvent` carries
  `path` and `error`, which cog's events cannot.
- **Once or every time:** `LogErrOnce(key, err)` with a key built from the
  path, forgotten with `ForgetLogOnce` when the condition is repaired. v1
  found that two caches sharing one key type silenced each other's reports
  ([#443](https://github.com/dvoyni/cog/issues/443)).
- **Retry policy:**

  | Policy | Who | Cost |
  |---|---|---|
  | Terminal until freed | v1 | a missing file stays missing for the run unless something frees it; nothing re-reads per frame |
  | The next load of the key tries again | Bevy | a caller that loads every frame re-reads a missing file every frame, the defect v1 removed from gfx |
  | The holder releases the failed load, then loads again | Unity | the holder pairs a release with a failure too |

- **A missing dependency:** Bevy marks the dependent's dependency states
  failed, keeping its own `Loaded`
  ([info.rs#L685-L727](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/server/info.rs#L685-L727)); v1 supplied the slot's
  `Default` and left the substitution to "the material binding's business".

### 4.5 A second load or a free while loading

| | Second load of the same key | Free while loading |
|---|---|---|
| Bevy | finds the info `Loading` and starts nothing | the last drop cancels the task; a late completion is dropped |
| Godot | attaches to the path's task, counting the request | not read |
| Unity | one operation, counted once per load | no cancel; the count drops and the operation releases itself when done (1.20.5) |
| v1 scene, before the Library | the state machine | a generation counter, so "the completing install must not write into a slot nobody asked for" |
| cog's kernel | a loading state in the slot | no cancel: the commit stage checks the slot's generation and state, and discards what it made under a retired handle |

## 5. Assets that depend on others

### 5.1 How the surveyed systems do it

| | Naming the dependencies | What keeps them alive | Ready when | A dependency fails | Freeing the parent |
|---|---|---|---|---|---|
| Bevy | strong handles in the asset value, marked `#[dependency]`; a loader's `LoadContext::load` starts their loads ([loader.rs#L141-L147, L693](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/loader.rs#L141-L147)) | the parent's handles | `RecursiveDependencyLoadState::Loaded`, then `LoadedWithDependencies`; the renderer retries a bind group until its images are prepared | the parent's dependency states fail with the first error | its handles drop with it; a dependency goes if nothing else holds it |
| Godot | `Ref` fields: a material's textures and shader; external resources loaded recursively under the cache mode | the parent's `Ref`s | the load returns once the tree has loaded | not read | its `Ref`s go with it; cycles leak |
| Unity | AssetBundles as dependencies of their assets | bundle counts | the operation completes | not read | a shared bundle unloads at its last release and reloads at the next load (boat, plane, cammo) |
| cog v1 | a loader re-enters other caches inside its own `Load`, through `U`; a font face re-enters the source cache | each cache's own entry | the load returns | the dependency's `Default` | nothing cascades: `UnloadModel` "deliberately does not cascade to textures"; `FreeWhere` frees everything derived from one file |

### 5.2 Options in cog

| Question | Options |
|---|---|
| What the material holds | draw handles of its textures and shader (A's counts cascade: building acquires them, freeing releases them); or their keys, resolved when it is built, with no cascade (B) |
| When it is ready | when every dependency is ready, the slot keeping its dependents to notify, as Bevy's `dependents_waiting_on_load` ([info.rs#L36-L37](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/server/info.rs#L36-L37)); or at once over placeholders, its bind group built again as each dependency lands |
| When a dependency fails | the material fails too (Bevy); or that slot takes its default (v1) |
| What it releases | under a cascade, a shared texture can unload and reload across a level change (Unity's cammo); without one, the material may outlive what it names |
| Derived GPU objects | pipelines keyed by their descriptor, over a shader module, and bind group layouts are shared entries with dependencies of their own; gfx leaves "sharing pipelines of one descriptor" above it |

### 5.3 What gfx adds

- A bind group names its buffers, views and samplers by handle, and after a
  device loss gfx recreates every object from its kept descriptor in
  dependency order, bind groups last
  ([`gfx/device.go` `recreateObjects`](../../../gfx/device.go)).
- A creation that fails in recovery "goes to `LogErr`, leaving its handle
  with no object". A bind group whose view was released keeps that view's
  handle in its descriptor; gfx tables hand a released index out again
  ([`gfx/table.go`](../../../gfx/table.go)), and the backend keeps its object
  at `handle.Index()`. Read from the code and not tested: such a bind group
  either fails its recreation or is recreated over whatever object took the
  index since. Releasing a dependent no later than what it names avoids
  both.

## 6. From platform's FS and from blobs

| | From `platform.FS` | From a blob, bytes given directly |
|---|---|---|
| **Identity** | the path, plus whatever makes one source several assets: colour space, a mesh index inside a container, a shader's defines (v1's bake parameters, `P`) | none of its own. Options: a name the caller gives; the bytes' identity, v1's pointer and length, valid only for bytes "built once and kept"; a content hash, rejected by v1 as O(n) on a path meant to be free; or no sharing, as Bevy's `Assets::add` and Godot's `create_from_image` |
| **Where the bytes come from** | `fs.ReadFile` in a job holding `kernel.Read[platform.FS]`; valid only for the job | from the caller, who cannot put them in an event or an arg; they reach draw through a call on draw's resource inside a job that holds it |
| **Lifetime of the bytes** | draw's own copy, or none once uploaded | a copy (memory and time), or the caller's slice kept under a promise not to write it, v1's "static" contract |
| **Failure** | the read's error; `errors.Is(err, fs.ErrNotExist)` tells absent from unreadable | decode only |
| **Refill after a device loss** | read again, or from a kept CPU copy | only from a kept copy, or the caller handing the bytes again |
| **Change at run time** | possible: the writable filesystem is searched first, so a write shadows a read mount; platform has no watch; `fs.Stat` works, an `os.DirFS` layer has modification times and an `embed.FS` layer answers the zero time ([embed.go#L221](https://github.com/golang/go/blob/master/src/embed/embed.go#L221)) | the caller's own |

- **Assets inside containers.** A glTF's embedded image has no path of its
  own; v1 keyed it by the container's path and the image's index, with the
  bytes beside the key as a payload ("you named it, you own the name"). Bevy
  names such sub-assets with a label, `path#label`, loaded by the container's
  load.
- **Shaders carry their includes.** A shader's source is its root and every
  file its directives include; v1 evicted by include-set membership with
  `FreeWhere`, and found `&` unsound as a separator in a key of defines, so
  its defines are kept in a canonical form
  ([#443](https://github.com/dvoyni/cog/issues/443), gfx migration line).
  In v2 preprocessing is `Device.PreprocessShader`, the backend's.
- **Bevy makes bytes addressable** through in-memory and embedded sources,
  `Dir::insert_asset` and `embedded_asset!`, which put bytes under a path the
  ordinary load then shares
  ([io/memory.rs](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/io/memory.rs),
  [io/embedded/mod.rs#L352](https://github.com/bevyengine/bevy/blob/v0.20.1/crates/bevy_asset/src/io/embedded/mod.rs#L352)).
  In cog, a read mount bound at `Init` over an `fs.FS` of the game's bytes
  does the same for bytes known at composition.
- **The CPU copy is a choice per asset.** Bevy's `RenderAssetUsages` drops
  the main-world data once the GPU copy is prepared, when the asset is
  marked render-only; draw's device-loss refill is what keeping one buys.

## 7. Not verified

- Unity: a release before completion and the absence of cancel are from the
  1.20.5 manual; the 2.7.6 pages read are silent. `WaitForCompletion` was not
  read.
- gfx: that a stale handle's deferred error drops the frame encoder at the
  flush is read from the design, and the bind group recreation over a reused
  index from the code; neither was run.
- That a draw handle with unexported fields inside an ecs ref value saves
  nothing and loads zero follows from `encoding/json/v2` ignoring unexported
  fields; not tested.
- `async-task`'s cancel on drop was read on its `master`, not at the
  version Bevy 0.20.1 locks.
- Godot: that a failed load leaves nothing cached, so the next `load`
  tries again, is inferred from the cache holding only live resources. What
  a failed dependency does to its dependent, and hot reload outside
  `CACHE_MODE_REPLACE` (the editor's reimport), were not read.
- Go's `runtime/mcleanup.go` and `embed/embed.go` were read on `master`.
