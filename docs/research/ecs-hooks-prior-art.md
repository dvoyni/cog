# How shipped ECSs record and deliver entered, exited, changed and despawned

Research for [Prior art: how shipped ECSs record and deliver entered, exited, changed and despawned](https://github.com/dvoyni/cog/issues/378), child of the map [ecs: Hooks, and how a System learns what happened to an Entity](https://github.com/dvoyni/cog/issues/377).

It answers the ticket's six questions for five ECSs:
- Bevy
- flecs
- EnTT
- arche and its successor ark
- Unity Entities

This file decides nothing for cog. [What cuts against the map](#what-cuts-against-the-map) lists where prior art contradicts a settled point or warns against it. [What this means for cog's Hooks](#what-this-means-for-cogs-hooks) maps the findings onto the open tickets.

## Versions read

| ECS | Version | Commit / source |
|---|---|---|
| Bevy | 0.19.1 (2026-08-13) | [`b56fc29`](https://github.com/bevyengine/bevy/tree/v0.19.1) |
| flecs | 4.1.6 (2026-06-28) | [`fb55f3c`](https://github.com/SanderMertens/flecs/tree/v4.1.6) |
| EnTT | 3.16.0; 4.0.0 diffed and behaviourally identical for everything below | [`b4e58bd`](https://github.com/skypjack/entt/tree/v3.16.0) |
| arche | 0.15.3 | [`73eaedc`](https://github.com/mlange-42/arche/tree/v0.15.3) |
| ark | 0.8.3 | [`f00c5d7`](https://github.com/mlange-42/ark/tree/v0.8.3) |
| Unity Entities | 1.4.8 | [needle-mirror of the published package](https://github.com/needle-mirror/com.unity.entities/tree/1.4.8), [manual @1.4](https://docs.unity3d.com/Packages/com.unity.entities@1.4/manual/) |

## How claims are labelled

- **[doc]**: stated by the project, in its manual, API docs, changelog, or a maintainer's issue or PR comment.
- **[src]**: read off the source at the tag above. It was not run.
- **[probe]**: seen by running a small throwaway program against the tag above: C for flecs, C++ for EnTT, Go for arche and ark. The probes were not committed, so treat them as spot checks, not published results.
- **Sources silent**: nothing was found. The gap was left open, not filled by inference.

Bevy 0.19 renamed a few things. `Replace`/`on_replace` became `Discard`/`on_discard` ([#22789](https://github.com/bevyengine/bevy/pull/22789)), and `Events` are now `Messages`.

---

## At a glance

| | Bevy | flecs | EnTT | arche / ark | Unity |
|---|---|---|---|---|---|
| **Buffered, per-reader** | change ticks (per system `last_run`); `RemovedComponents` (per-system cursor over a 2-update buffer) | query change detection (per query, per table counters) | reactive storage (a set per reader, drained by hand) | none | chunk change versions (per system `LastSystemVersion`) |
| **Immediate, no reader state** | hooks, observers | hooks, observers, monitors | signals | listener (arche), observers (ark) | none |
| **Granularity of "changed"** | per component instance, latest tick only | per table × column, "at least once" | per entity, set membership | no change detection | per chunk × type, "at least once" |
| **What counts as a write** | `DerefMut` of a `Mut` | `set`/`modified`; non-skipped iteration of an `inout` query | explicit `patch`/`replace`/`emplace_or_replace` | explicit `Map.Set` (ark OnSet) | requesting RW access |
| **Query enter/exit** | not available ([#20817](https://github.com/bevyengine/bevy/issues/20817) open) | **monitors** | never reported | not available; ark filters test the pre-op mask | not available; cleanup-component idiom |
| **Removed value readable** | in `Discard`/`Remove` observers and hooks, synchronously | in OnRemove/`on_remove`, synchronously | in `on_destroy`, synchronously | ark: yes, synchronously; arche: only on entity removal | only what the user copied into a cleanup component |
| **Cost with nobody reading** | ticks always stored and written; `RemovedComponents` always written | fast path (memcpy) for ids < 256, flag is sticky | an empty-vector publish per op (or compile out) | one nil/bool check | versions always stored and written |

---

## 1. Per-reader state

**In short:** every buffered mechanism in these five ECSs is either **O(1) per reader** (a last-run version compared against per-row or per-chunk versions) or **bounded by the population** (a set). Bevy's `RemovedComponents` is the only exception. It is a real per-reader record stream, and it is bounded by a fixed two-update window that silently drops records. No shipped ECS keeps an unbounded per-reader log.

### Bevy

- **Change ticks are per reader and never drained.**
  - Each system stores its own `last_run` tick when a run ends ([function_system.rs L670](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/system/function_system.rs#L670), [L698](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/system/function_system.rs#L698)).
  - A component is changed for that reader if its tick is newer ([tick.rs L48-62](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/change_detection/tick.rs#L48-L62)). [src]
  - Not losing changes for systems that don't run every frame was the stated goal of [#1471](https://github.com/bevyengine/bevy/pull/1471). [doc]
  - A new system sees everything, "including changes that happened before the first time this Query was run" ([filter.rs L896](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/query/filter.rs#L896)). [doc]
- **What bounds a paused reader: tick age, not memory.**
  - Ticks are `u32`. "Changes stop being detected once they become this old", at `MAX_CHANGE_AGE` ≈ 3.26e9 ticks ([change_detection/mod.rs L13-26](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/change_detection/mod.rs#L13-L26)). [doc]
  - `check_change_ticks` runs at every `Schedule::run`. Once `CHECK_TICK_THRESHOLD` (518.4M) has passed, it scans all storage and clamps old ticks ([world/mod.rs L3318-3357](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/world/mod.rs#L3318-L3357)). [src]
  - The clamp is in the **under-reporting** direction: very old changes are lost, recent ones kept.
- **`RemovedComponents<T>` is a per-system cursor over a shared double buffer.**
  - Implementation: a `Local<MessageCursor>` over one `Messages<RemovedComponentEntity>` per component ([lifecycle.rs L437-516](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/lifecycle.rs#L437-L516)).
  - Readers that run less often than every two updates "are guaranteed to drop" messages ([messages.rs L17-38](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/message/messages.rs#L17-L38)). [doc]
  - The only report of loss is `missed_messages()`, which returns a count ([message_cursor.rs L131-135](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/message/message_cursor.rs#L131-L135)). [doc]
  - The buffers swap once per `App::update` ([sub_app.rs L155-158](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_app/src/sub_app.rs#L155-L158)). As a result the mechanism is broken in `FixedUpdate` ([#16520](https://github.com/bevyengine/bevy/issues/16520), open).
  - A maintainer's answer is to move to observers ([#13928](https://github.com/bevyengine/bevy/issues/13928)).
- **Hooks and observers keep no reader state.**
  - They run "immediately" inside the structural change ([distributed_storage.rs L41](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/observer/distributed_storage.rs#L41)). [doc]
  - An observer registered later sees nothing that came before. [src]

### flecs

- **Observers and hooks keep no reader state.** They run inside the operation ([ObserversManual L2158](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/ObserversManual.md?plain=1#L2158)). [doc] The only catch-up is `yield_existing`, which replays current matches when the observer is created ([L1391](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/ObserversManual.md?plain=1#L1391)). [doc]
- **Change detection is per query, per table, per column.**
  - Each tracked table has an int32 counter per column, plus one for entities added or removed. Each change-detecting query keeps its own copy for every table it matched ([Queries L3353-3355](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/Queries.md?plain=1#L3353)). [doc]
  - The two sides are `table->dirty_state` and `match->_monitor` ([change_detection.c L79-141](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/query/cache/change_detection.c#L79)). [src]
  - Only cached queries support it. Since 4.1.0 it is opt-in per query with `EcsQueryDetectChanges` ([v4.1.0 notes](https://github.com/SanderMertens/flecs/releases/tag/v4.1.0)). [doc]
- **What a paused reader sees.**
  - A table stays "changed" until the query iterates it ([Queries L3375](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/Queries.md?plain=1#L3375)). [doc]
  - The test is `monitor != dirty_state`. The reader learns "changed at least once" and nothing else ([change_detection.c L361-411](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/query/cache/change_detection.c#L361)). [src]
  - [probe] 1000 `ecs_set` calls produced one changed table.
  - Nothing grows while a reader is idle. Sources are silent on counter wraparound.
- **The first check always reports "changed"**, because monitors are created lazily ([change_detection.c L673-675](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/query/cache/change_detection.c#L673)). [src]

### EnTT

- **Signals keep no reader state.**
  - A `sigh` is a vector of delegates published synchronously, last connected first, with "Order isn't guaranteed" ([sigh.hpp L173-177](https://github.com/skypjack/entt/blob/v3.16.0/src/entt/signal/sigh.hpp#L173-L177)). [src]
  - Every storage holds three of them: construction, destruction, update ([mixin.hpp L377-381](https://github.com/skypjack/entt/blob/v3.16.0/src/entt/entity/mixin.hpp#L377-L381)). [src]
- **Reactive storage is the reader's state.**
  - It is a storage that listeners fill with `if(!contains(e)) emplace(e)` ([mixin.hpp L405-409](https://github.com/skypjack/entt/blob/v3.16.0/src/entt/entity/mixin.hpp#L405-L409)). [src]
  - It "never deletes its entities"; the user calls `clear()` ([entity.md L648-650](https://github.com/skypjack/entt/blob/v3.16.0/docs/md/entity.md?plain=1#L648-L650)). [doc]
  - N readers need N storages.
  - A reader that stops draining is bounded by the population, one entry per entity.
- **History of `entt::observer`.**
  - It was deprecated in 3.14 in favour of the reactive mixin ([3.14 notes](https://github.com/skypjack/entt/releases/tag/v3.14.0)).
  - It was removed in 3.15 ([3.15 notes](https://github.com/skypjack/entt/releases/tag/v3.15.0)). [doc]

### arche / ark

- **arche: one listener per world, called synchronously.**
  - "Events are emitted immediately after the change is applied" ([event.go L20-32](https://github.com/mlange-42/arche/blob/v0.15.3/ecs/event.go#L20-L32)). [doc]
  - `SetListener` replaces the previous listener; `listener.Dispatch` fans out to several ([world.go L531-538](https://github.com/mlange-42/arche/blob/v0.15.3/ecs/world.go#L531-L538)). [doc]
  - The only deferral: batch methods that return a `Query` notify when that query closes. What is held is per-archetype index ranges, not per-entity records ([world_internal.go L1119-1184](https://github.com/mlange-42/arche/blob/v0.15.3/ecs/world_internal.go#L1119-L1184)). [src]
- **ark: many observers, no buffering.**
  - Callbacks "are executed immediately by any emitted event" ([events guide L90-93](https://github.com/mlange-42/ark/blob/v0.8.3/docs/content/events/index.md?plain=1#L90-L93)). [doc]
  - A callback receives only the `Entity`, with no mask of what changed ([observer.go L20-29](https://github.com/mlange-42/ark/blob/v0.8.3/ecs/observer.go#L20-L29)). [src]
  - Observer order is undefined ([guide L107-108](https://github.com/mlange-42/ark/blob/v0.8.3/docs/content/events/index.md?plain=1#L107-L108)). [doc]

### Unity Entities

- **Per-reader state is one version number per system.**
  - Each chunk keeps a version per component type: the `GlobalSystemVersion` when that array was "last accessed as writeable". A filter compares it with the system's `LastSystemVersion` ([Version numbers](https://docs.unity3d.com/Packages/com.unity.entities@1.4/manual/systems-version-numbers.html)). [doc]
  - A reader's whole state is one `uint`.
  - The global version is bumped before the update. `LastSystemVersion` is then set to it, and the global version is bumped again after ([SystemState.cs L453-471](https://github.com/needle-mirror/com.unity.entities/blob/1.4.8/Unity.Entities/SystemState.cs#L453-L471)). [src]
- **Wraparound.**
  - `DidChange` is `(int)(changeVersion - requiredVersion) > 0`, with 0 meaning "first run, everything changed" ([ChangeVersionUtility.cs L15-32](https://github.com/needle-mirror/com.unity.entities/blob/1.4.8/Unity.Entities/ChangeVersionUtility.cs#L15-L32)). [src]
  - A reader paused beyond 2^31 versions gets wrong answers in both directions. At 500 systems and 60 fps that is roughly 10 hours. Sources are silent on this bound.
  - The manual calls the versions "32-bit signed integers", but the code uses `uint`.
- **Stopping a system advances its version** to the moment it stopped, through `OnStopRunning` ([WorldUnmanaged.cs L876-898](https://github.com/needle-mirror/com.unity.entities/blob/1.4.8/Unity.Entities/WorldUnmanaged.cs#L876-L898)). [src]
- **Queries not owned by a system never get a required version**, so their change filter always passes ([SystemState.cs L727-735](https://github.com/needle-mirror/com.unity.entities/blob/1.4.8/Unity.Entities/SystemState.cs#L727-L735)). [src]

---

## 2. Removed values

**In short:** everyone who exposes a removed value does so **only synchronously**, inside a callback that runs before the storage drops it. After the callback the value is gone. No shipped ECS keeps a removed value for a later reader. Unity's documented answer is to copy what you need into a cleanup component; EnTT's is a non-void reactive storage whose callback copies the value.

### Bevy

- **`RemovedComponents` carries only the `Entity`.** "Unlike hooks or observers … this does not allow you to see which data existed before removal" ([lifecycle.rs L478-485](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/lifecycle.rs#L478-L485)). [doc]
- **Observers and hooks can read the value before it goes.**
  - `Discard` "runs before the value is replaced" and `Remove` "runs before the component is removed" ([lifecycle.rs L355-383](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/lifecycle.rs#L355-L383)). [doc]
  - The drop order is: observers and hooks, then the `RemovedComponents` write, then the storage drop ([bundle/remove.rs L137-225](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/bundle/remove.rs#L137-L225)).
  - Despawn follows the same pattern ([world_mut.rs L1644-1718](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/world/entity_access/world_mut.rs#L1644-L1718)). [src]
- **Deferring the work loses the data.** Commands queued from those observers run after the data is gone, and a maintainer thread calls that "not a viable solution … the data will be incorrect or simply gone" ([#21354](https://github.com/bevyengine/bevy/issues/21354)). [doc]
- **Not yet merged:** `AfterRemove` and `BeforeAdd`, in the open PR [#22961](https://github.com/bevyengine/bevy/pull/22961).

### flecs

- **OnRemove observers read the value, and `on_remove` runs after them.**
  - OnRemove is emitted before the entity leaves its table ([entity.c L217-221](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/entity.c#L217)). [src]
  - `on_remove` then runs after observers and before the destructor, in the same operation ([flecs.h L999-1002](https://github.com/SanderMertens/flecs/blob/v4.1.6/include/flecs.h#L999), [table.c L1084-1092](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/storage/table.c#L1084)). [doc][src]
  - [probe] OnRemove read the value on both remove and delete.
  - The manual never says outright that the value is readable during OnRemove. That part is sources silent beyond the ordering.
- **In deferred mode**, remove and delete are commands, so OnRemove, the hook and the free all happen at merge ([commands.c L1245-1302](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/commands.c#L1245)). [src]
  - A deferred `set` on a component the entity already has writes storage immediately in a single stage ([commands.c L537-547](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/commands.c#L537)).
  - [probe] A deferred `set(44)` followed by `delete` showed OnRemove `x=44`.
- **`on_replace` sees the old and the new value before a set** ([EntitiesComponents L1255](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/EntitiesComponents.md?plain=1#L1255)). [doc]

### EnTT

- **`on_destroy` listeners run "before components have been destroyed"** ([entity.md L390-391](https://github.com/skypjack/entt/blob/v3.16.0/docs/md/entity.md?plain=1#L390-L391)). [doc]
  - `pop` publishes and then erases, entity by entity ([mixin.hpp L73-84](https://github.com/skypjack/entt/blob/v3.16.0/src/entt/entity/mixin.hpp#L73-L84)). [src]
- **The "before" guarantee holds per storage only.**
  - `registry::destroy(e)` removes pools in **reverse** creation order, but the range overload walks them **forward** ([registry.hpp L544-590](https://github.com/skypjack/entt/blob/v3.16.0/src/entt/entity/registry.hpp#L544-L590)). [src]
  - Order across pools is undefined ([skypjack in #668](https://github.com/skypjack/entt/issues/668)). [doc]
  - The entity's own `on_destroy` runs after all components are gone ([#1169](https://github.com/skypjack/entt/issues/1169)). [doc]
- **No value is kept.** Reactive storage of `void` holds ids only. To keep a value, use a non-void reactive storage whose callback copies it while the listener can still read it ([entity.md L569-614](https://github.com/skypjack/entt/blob/v3.16.0/docs/md/entity.md?plain=1#L569-L614)). [doc]
- **A trap with reactive storage and destroy.**
  - A registry-owned reactive storage is cleaned on destroy because it is just another pool ([entity.md L523-524](https://github.com/skypjack/entt/blob/v3.16.0/docs/md/entity.md?plain=1#L523-L524)). [doc]
  - If it observes `on_destroy<T>` and is cleaned *before* T's pool, T's listener re-adds the dead entity. [probe] This happens depending on pool creation order and on single versus range `destroy`.
  - When the id is recycled, debug builds assert and release builds silently corrupt the set ([sparse_set.hpp L361](https://github.com/skypjack/entt/blob/v3.16.0/src/entt/entity/sparse_set.hpp#L361)). [probe] Sources silent (undocumented).

### arche / ark

- **arche.**
  - Entity removal notifies before removal "to allow for inspection of its components", with the world locked ([event.go L25-28](https://github.com/mlange-42/arche/blob/v0.15.3/ecs/event.go#L25-L28)). [doc]
  - Component removal notifies **after** the move, so the value is gone ([world_internal.go L403-416](https://github.com/mlange-42/arche/blob/v0.15.3/ecs/world_internal.go#L403-L416)). [src][probe]
- **ark.** `OnRemoveEntity` and `OnRemoveComponents` both fire "before the operation", with the world locked ([guide L99-101](https://github.com/mlange-42/ark/blob/v0.8.3/docs/content/events/index.md?plain=1#L99-L101)). [doc][probe]

### Unity Entities

- **Cleanup components.**
  - Destroying an entity that has a cleanup component "removes all non-cleanup components instead. The entity still exists until you remove all cleanup components" ([Cleanup components](https://docs.unity3d.com/Packages/com.unity.entities@1.4/manual/components-cleanup-introducing.html)). [doc]
  - Only data the user put into the cleanup component survives, and it lasts until the user removes it.
  - Cleanup components can't be baked ([Create a cleanup component](https://docs.unity3d.com/Packages/com.unity.entities@1.4/manual/components-cleanup-create.html)). [doc]
  - Mechanically, the entity moves to a "cleanup residue" archetype ([CreateDestroyEntities.cs L268-304](https://github.com/needle-mirror/com.unity.entities/blob/1.4.8/Unity.Entities/EntityComponentStoreCreateDestroyEntities.cs#L268-L304)). [src]
- **`GetCreatedAndDestroyedEntities`** returns ids only. It is `[Obsolete]` in favour of "(enableable) tags and cleanup components", and it scans every chunk ([EntityManager.cs L3648-3682](https://github.com/needle-mirror/com.unity.entities/blob/1.4.8/Unity.Entities/EntityManager.cs#L3648-L3682)). [src]
- **`EntityManagerDiffer`** gets old values by keeping a full shadow world. It is a live-baking tool ([EntityManagerDiffer.cs](https://github.com/needle-mirror/com.unity.entities/blob/1.4.8/Unity.Entities/Diff/EntityManagerDiffer.cs#L9-L45)). [src]

---

## 3. A reader's own changes

**In short:** the ticket names "Bevy's self-triggering `Changed`" as a known trap, and **the Bevy source does not bear that out for direct writes.** Bevy, Unity and flecs, the three version-based designs, all **suppress a reader's own direct writes by construction**: the write is stamped with the reader's own run version, and the reader's last-seen version is then set to that same number. All three leak self-changes only through a **deferred** path, where the write is stamped at playback. Immediate-callback designs recurse or queue instead.

### Bevy

- **A system does not re-see its own `Mut` writes.**
  - `DerefMut` stamps `changed = this_run` ([traits.rs L424-473](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/change_detection/traits.rs#L424-L473)). `last_run` is then set to `this_run`, and the tick comparison is strict. [src]
  - The test `pipe_change_detection` asserts that a system does not see its own `ResMut` write on its next run, while another system does ([system/mod.rs L1806-1879](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/system/mod.rs#L1806-L1879)). [src]
  - Within the same run, `is_changed()` on a just-dereferenced `Mut` returns true. [src]
  - No maintainer issue describing a self-triggering loop for direct writes was found. Sources silent.
- **It does re-see its own `Commands` writes.**
  - `increment_change_tick` returns the old tick and increments the counter, so commands applied at a later sync point get a newer tick ([world/mod.rs L3162-3167](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/world/mod.rs#L3162-L3167)). [src]
  - [#1471](https://github.com/bevyengine/bevy/pull/1471) names "Changes Made By Commands" as the case its attribution cannot solve. [doc]
- **Changed without a change.**
  - "Simply mutably dereferencing a component is considered a change … Bevy does not compare components to their previous values" ([filter.rs L890-891](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/query/filter.rs#L890-L891)). [doc]
  - The escape hatches are `set_if_neq`/`replace_if_neq`, and `bypass_change_detection` "to avoid infinite recursion" ([traits.rs L158-227](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/change_detection/traits.rs#L158-L227)). [doc]

### flecs

- **A query does not see its own writes, contrary to its docs.**
  - The docs say an `inout` query with change detection "will always be changed as iterating the query increases the table counters" ([Queries L3379](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/Queries.md?plain=1#L3379)).
  - The source bumps the table's counters and *then* syncs the query's own monitor, so the bump is absorbed ([eval_iter.c L127-144](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/query/engine/eval_iter.c#L127)). [src]
  - Upstream's own test asserts `!ecs_query_changed` after a full pass ([ChangeDetection.c L3005-3034](https://github.com/SanderMertens/flecs/blob/v4.1.6/test/query/src/ChangeDetection.c#L3005)). [src]
  - [probe] The writing query reported unchanged on every pass, while a separate `[in]` query reported changed.
- **`ecs_iter_skip`** skips both the dirty bump and the self-sync, so a skipped table stays "changed" for the skipper ([change_detection.c L724-731](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/query/cache/change_detection.c#L724)). [src]
- **Observers do not recurse on the stack.**
  - They run with the stage deferred, so a write inside an observer is queued and flushed after the callback ([commands.c L99-108](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/commands.c#L99), [observable.c L1263-1267](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/observable.c#L1263)). [src]
  - [probe] A self-setting OnSet observer looped with a maximum stack depth of 1. Sources are silent on detecting an unguarded loop.
  - The docs say "Observers should never mutate a component" ([L128-129](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/ObserversManual.md?plain=1#L128)). [doc]

### EnTT

- **Listener recursion is unguarded.** `patch<T>` inside `on_update<T>` re-publishes synchronously ([mixin.hpp L349-353](https://github.com/skypjack/entt/blob/v3.16.0/src/entt/entity/mixin.hpp#L349-L353)). [src]
- **What listeners may not do.** Connecting inside a listener is UB in some cases, removing the component inside `on_construct`/`on_update` "is not allowed", and `on_destroy` is "for cleanup and nothing more" ([entity.md L393-405](https://github.com/skypjack/entt/blob/v3.16.0/docs/md/entity.md?plain=1#L393-L405)). [doc]
- **Reactive storage, written to while you iterate it.**
  - Patching an entity already in the set is a no-op.
  - Patching another entity appends it, but iteration runs from the back, so the new entry is not visited ([sparse_set.hpp L650-653](https://github.com/skypjack/entt/blob/v3.16.0/src/entt/entity/sparse_set.hpp#L650-L653)). [src]
  - **The idiomatic iterate-then-`clear()` therefore silently drops the reader's own writes.** [probe] Sources silent.

### arche / ark

- **No change detection in either** (no ticks or dirty state in `ecs`). Sources are silent on the topic. [src]
- **Recursion and locking.**
  - Nested events fire synchronously and re-entrantly, with no recursion guard. [probe][src]
  - arche panics on a structural change inside a locked (entity-removal) callback. [probe]
  - ark allows structural changes in single-op create, add and set callbacks, and locks for removals and batches ([guide L95-101](https://github.com/mlange-42/ark/blob/v0.8.3/docs/content/events/index.md?plain=1#L95-L101)). [doc]
  - ark's `Map.Set` works on a locked world ([map.go L202-223](https://github.com/mlange-42/ark/blob/v0.8.3/ecs/map.go#L202-L223)), so OnSet can fire inside a removal callback. [src]

### Unity Entities

- **A system never sees its own in-update writes, by design.**
  - The code comment reads "Never detect change of something the system itself changed" ([ChangeVersionUtility.cs L20-22](https://github.com/needle-mirror/com.unity.entities/blob/1.4.8/Unity.Entities/ChangeVersionUtility.cs#L20-L22)). [src]
  - Jobs it schedules stamp the version captured at `Update`, so they are covered too ([ArchetypeChunkArray.cs L2815-2822](https://github.com/needle-mirror/com.unity.entities/blob/1.4.8/Unity.Entities/Iterators/ArchetypeChunkArray.cs#L2815-L2822)). [src]
- **But it does see its own `EntityCommandBuffer` writes** when a later command-buffer system plays them back. Playback stamps the *current* global version ([EntityComponentStore.cs L1486-1492](https://github.com/needle-mirror/com.unity.entities/blob/1.4.8/Unity.Entities/EntityComponentStore.cs#L1486-L1492)). [src] This is inferred from the source; the docs don't say it.
- **Access counts as change.**
  - "ECS increments the change version even if the job that declares write access to a component doesn't change the component value" ([IJobChunk](https://docs.unity3d.com/Packages/com.unity.entities@1.4/manual/iterating-data-ijobchunk-implement.html#skipping-chunks-with-unchanged-entities)). [doc]
  - Unity's own transform system shipped that over-report: it "no longer increments the change version of `WorldTransform` and `LocalToWorld` on all world-space entities every frame" ([CHANGELOG 1.0.0-pre.44](https://github.com/needle-mirror/com.unity.entities/blob/1.4.8/CHANGELOG.md?plain=1#L1084)). [doc]
  - The escape is `IJobEntityChunkBeginEnd`, which skips a chunk before requesting write access ([Data granularity](https://docs.unity3d.com/Packages/com.unity.entities@1.4/manual/systems-data-granularity.html)). [doc]

---

## 4. Deduplication and order

**In short:** version-based designs **net** everything: one "changed" per entity (Bevy), per table (flecs) or per chunk (Unity) per reader window, with no count and no order. Immediate-callback designs deliver **one call per operation, never netted**. flecs's deferred command batching is a third behaviour: it nets structural changes per entity, **asymmetrically**. On what counts as a write, the projects split into explicit calls (EnTT, flecs `set`/`modified`, ark) and access (Bevy `DerefMut`, Unity RW access, flecs `inout` iteration).

### Bevy

- **Ticks net.** Each component keeps two `u32`s, `added` and `changed` ([tick.rs L134-143](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/change_detection/tick.rs#L134-L143)). The result is at most one report per entity per window. [src]
- **Remove then re-add inside one window reads as `Added`**, indistinguishable from a first add. [src] `RemovedComponents` separately records every removal, without netting ([remove.rs L205-208](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/bundle/remove.rs#L205-L208)). [src]
- **What counts as a write.**
  - `DerefMut`, `AsMut` and `set_changed` count.
  - An overwriting `insert` sets `changed` but not `added`, and archetype moves keep ticks (test `changed_trackers`, [lib.rs L1026-1128](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/lib.rs#L1026-L1128)). [src]
  - In-place mutation fires no hook or observer. Immutable components are the documented way to turn `Insert`+`Discard` into an on-change signal ([component/mod.rs L664-678](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/component/mod.rs#L664-L678)). [doc]
- **Observers deliver one call per operation, in a documented order.**
  - Insert then remove fires `add, insert, discard, remove`; an overwrite fires `discard, insert` ([observer/mod.rs tests L527-608](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/observer/mod.rs#L527-L608)). [src]
  - Adding runs hooks then observers; removing runs observers then hooks ([distributed_storage.rs L186-192](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/observer/distributed_storage.rs#L186-L192)). [doc]
  - Despawn runs `Despawn`, then `Discard`, then `Remove` ([lifecycle.rs L30-31](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/lifecycle.rs#L30-L31)). [doc]
  - The order among observers of one event is "arbitrary" ([#14890](https://github.com/bevyengine/bevy/issues/14890)). [doc]
  - Lifecycle events caused by `Commands` fire at the sync point ([distributed_storage.rs L146-148](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/observer/distributed_storage.rs#L146-L148)). [doc]

### flecs

- **Deferred batching nets per entity, asymmetrically.**
  - At merge, all of an entity's add, remove and clear commands fold into one move ([commands.c L836-1129](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/commands.c#L836)).
  - If the entity ends where it started, there is no move, no OnRemove and no hooks ([entity.c L252-272](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/entity.c#L252)).
  - OnAdd for the "added" ids is still emitted ([commands.c L1095-1123](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/commands.c#L1095)). [src]

  | Deferred batch [probe] | OnAdd | OnRemove | hooks / dtor | value |
  |---|---|---|---|---|
  | existing component: remove, add | **1** | **0** | none | old value kept |
  | same, not deferred | 1 | 1 | — | — |
  | new component: add, remove | 0 | 0 | — | — |
  | monitor `Position, !Velocity`: add Velocity, remove Velocity | 0 enter | 0 exit | | |

  - The docs only warn that batching may reorder events and that using add/remove to trigger observers is "unreliable because features like command batching impact how and when events are emitted" ([ObserversManual L109](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/ObserversManual.md?plain=1#L109), [L2254-2257](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/ObserversManual.md?plain=1#L2254)). [doc]
- **OnSet fires once per `set`/`modified` call, with no equality check and no merging** ([L196](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/ObserversManual.md?plain=1#L196)). [doc][probe] OnSet order is kept; OnAdd/OnRemove order is not guaranteed ([L2259-2270](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/ObserversManual.md?plain=1#L2259)). [doc]
- **What counts as a write.**
  - Counted: add, remove or delete; `set`; `modified`; and iterating a table through an `inout`/`out` query without skipping it.
  - Not counted: `ensure` or a ref without `modified` ([Queries L3357-3369](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/Queries.md?plain=1#L3357)). [doc]
  - A query reports only changes in the components it matched, plus entities entering or leaving a matched table ([L3371](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/Queries.md?plain=1#L3371)). [doc]

### EnTT

- **Reactive storage keeps one entry per entity** however many events fire. [src][probe] Order is packed-array order, which the docs don't define.
- **Signals fire once per call**, and `insert` publishes once per entity ([mixin.hpp L365-375](https://github.com/skypjack/entt/blob/v3.16.0/src/entt/entity/mixin.hpp#L365-L375)). [src]
- **What counts as a write: explicit calls only.**
  - `on_update` fires "only… following a call to `replace`, `emplace_or_replace` or `patch`" ([entity.md L362-365](https://github.com/skypjack/entt/blob/v3.16.0/docs/md/entity.md?plain=1#L362-L365)). [doc]
  - Mutating through a `get` reference, a view or a group is invisible. [probe]
  - `patch<T>(e)` with no function is the designed "mutate via view, then signal" idiom ([skypjack in #406](https://github.com/skypjack/entt/issues/406)). [doc]
- **The old `observer` dropped entities silently.** An entity whose condition broke before draining just vanished ("A registered entity isn't returned… if the condition set by the filter is broken in the meantime", [observer.hpp L131-136](https://github.com/skypjack/entt/blob/v3.14.0/src/entt/entity/observer.hpp#L131-L136)). [src]

### arche / ark

- **arche sends one event per operation**, covering every type involved ([event.go L14-16](https://github.com/mlange-42/arche/blob/v0.15.3/ecs/event.go#L14-L16)). [doc] Remove then re-add gives two events. It has no set event at all. [probe]
- **ark sends one event per event type per operation** ([guide L39-40](https://github.com/mlange-42/ark/blob/v0.8.3/docs/content/events/index.md?plain=1#L39-L40)). [doc]
  - Exchange gives OnRemove before the move and OnAdd after it ([world_internal.go L166-179](https://github.com/mlange-42/ark/blob/v0.8.3/ecs/world_internal.go#L166-L179)). [src]
  - Batches loop over observers first, then entities ([events.go L431-450](https://github.com/mlange-42/ark/blob/v0.8.3/ecs/events.go#L431-L450)). [src]
  - OnSet fires only from `Map.Set`, "not the case when assigning by pointer dereference" ([map.go L205-207](https://github.com/mlange-42/ark/blob/v0.8.3/ecs/map.go#L205-L207)). [doc]

### Unity Entities

- **One flag per chunk per type.** "Changes are recorded per component per chunk" ([EntityManager.cs L187-195](https://github.com/needle-mirror/com.unity.entities/blob/1.4.8/Unity.Entities/EntityManager.cs#L187-L195)), with no skipping of individual entities ([LastSystemVersion API](https://docs.unity3d.com/Packages/com.unity.entities@1.4/api/Unity.Entities.SystemState.LastSystemVersion.html)). [doc]
- **What counts as a write: access, not mutation.**
  - RW `GetNativeArray` ([ArchetypeChunkArray.cs L1716-1730](https://github.com/needle-mirror/com.unity.entities/blob/1.4.8/Unity.Entities/Iterators/ArchetypeChunkArray.cs#L1716-L1730)).
  - `ComponentLookup.GetRefRW` ([ComponentLookup.cs L422-433](https://github.com/needle-mirror/com.unity.entities/blob/1.4.8/Unity.Entities/Iterators/ComponentLookup.cs#L422-L433)).
  - `SetComponentData`.
  - `SetComponentEnabled`, which also bumps T's version ([EntityComponentStoreChunk.cs L74-85](https://github.com/needle-mirror/com.unity.entities/blob/1.4.8/Unity.Entities/EntityComponentStoreChunk.cs#L74-L85)).
  - [src]
- **Structural changes are handled unevenly** ("Version Change Case" table, [ChunkDataUtility.cs L21-60](https://github.com/needle-mirror/com.unity.entities/blob/1.4.8/Unity.Entities/ChunkDataUtility.cs#L21-L60)). [src]
  - Create and instantiate stamp every type as changed, so **every change-filtered reader fires on spawn**.
  - A move keeps the newer of the source and destination versions for carried types and stamps new types.
  - Removing an entity from a chunk fills the hole by copying the tail. **That bumps only the order version, not change versions**, so a change filter alone misses departures ([ChunkDataUtility.cs L930-975](https://github.com/needle-mirror/com.unity.entities/blob/1.4.8/Unity.Entities/ChunkDataUtility.cs#L930-L975)).
- **Remove then re-add produces no event.** The re-added chunk reads as changed, indistinguishable from a write. [src]

---

## 5. Matching

**In short:** everything is **per component type** except **flecs monitors**. Monitors are the only shipped mechanism that reports an entity entering and leaving a multi-term query, including through a `Not` term. They are synchronous and cost more than plain observers. Bevy has a design issue for query-level lifecycle events that still needs a design doc. ark's `With`/`Without` filters test the composition *before* the operation, which is not the same as entry or exit.

### Bevy

- **Per type.**
  - `Added<T>`/`Changed<T>` read one column ([filter.rs L1079-1111](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/query/filter.rs#L1079-L1111)).
  - Observers dispatch by component id ([trigger.rs L478-515](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/event/trigger.rs#L478-L515)).
  - `On<Add, (A, B)>` means A **or** B ([observer/mod.rs test L699-712](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/observer/mod.rs#L699-L712)).
  - [src]
- **No enter or exit.** Leaving `Query<&A, Without<B>>` by gaining B surfaces only as `On<Add, B>`. The only help is the `old_archetype`/`new_archetype` pair, which you check by hand ([trigger.rs L334-386](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/event/trigger.rs#L334-L386)). [doc]
- **Open design work.**
  - [#20817](https://github.com/bevyengine/bevy/issues/20817) "Lifecycle event observers for queries" (S-Needs-Design-Doc) shows why today's events can't express `Without<T>`/`Has<T>` correctly. It proposes caching matching queries per archetype or per archetype-graph edge, and compares with flecs monitors. [doc]
  - Related: [#14510](https://github.com/bevyengine/bevy/issues/14510) (query-level change detection) and [#2148](https://github.com/bevyengine/bevy/issues/2148) (a `Removed` filter).
  - In [#13928](https://github.com/bevyengine/bevy/issues/13928) a contributor says "no one knows how to actually build it without incurring unacceptable performance losses". [doc]

### flecs

- **Hooks are per component; observers are per query** ([ObserversManual L89](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/ObserversManual.md?plain=1#L89), [L122-127](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/ObserversManual.md?plain=1#L122)). [doc]
- **Multi-term observers.**
  - They fire only for entities matching every term, triggered by an event on any term ([L746](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/ObserversManual.md?plain=1#L746), [L808](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/ObserversManual.md?plain=1#L808)). [doc]
  - Internally they are single-term observers plus a query evaluation ([L805](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/ObserversManual.md?plain=1#L805)). [doc]
  - A per-emit id makes them fire once per event ([observer.c L580-585](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/observer.c#L580)). [src]
- **`Not` terms invert the event:** removing the excluded component fires OnAdd, and adding it fires OnRemove ([L1118-1226](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/ObserversManual.md?plain=1#L1118)). [doc]
- **Monitors (`EcsMonitor`)** evaluate the query against the previous table and the current one. They fire OnAdd when the match goes from no to yes, OnRemove when it goes from yes to no, and nothing otherwise ([L1229](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/ObserversManual.md?plain=1#L1229), [L1379-1388](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/ObserversManual.md?plain=1#L1379); [observer.c L644-659](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/observer.c#L644)). [doc][src]
  - [probe] With `Position, !Velocity`: adding Position entered, adding Velocity exited, removing Velocity entered again.

### EnTT

- **Signals are per storage** (per type, or per `(type, id)`) ([entity.md L367-379](https://github.com/skypjack/entt/blob/v3.16.0/docs/md/entity.md?plain=1#L367-L379)). [doc]
- **The removed `observer`'s `group<A,B>(exclude<D>)`** entered on `on_construct<A|B>` or `on_destroy<D>`, and discarded on the inverse ([observer.hpp L203-242](https://github.com/skypjack/entt/blob/v3.14.0/src/entt/entity/observer.hpp#L203-L242)). [src] **Exit was never reported**; the entity just disappeared from the set.
- **Reactive storage has no query semantics.**
  - Conditions combine with **or** ([entity.md L554](https://github.com/skypjack/entt/blob/v3.16.0/docs/md/entity.md?plain=1#L554)). [doc]
  - "Leave" means a custom callback that removes and conditionally re-adds, or filtering with `view<…>(exclude<…>)` at read time ([L598-643](https://github.com/skypjack/entt/blob/v3.16.0/docs/md/entity.md?plain=1#L598-L643)). [doc]

### arche / ark

- **arche** matches on overlap with the changed components only, with nothing like With/Without ([util.go L63-90](https://github.com/mlange-42/arche/blob/v0.15.3/ecs/util.go#L63-L90)). `OldMask`/`NewMask` were removed in 0.10 ([#333](https://github.com/mlange-42/arche/pull/333)). [src][doc]
- **ark checks `With`/`Without` against the old mask** for add and remove ([events.go L412-469](https://github.com/mlange-42/ark/blob/v0.8.3/ecs/events.go#L412-L469)). The docs half-say it: "(or rather, had before the operation)" ([guide L85-86](https://github.com/mlange-42/ark/blob/v0.8.3/docs/content/events/index.md?plain=1#L85-L86)). [src][doc]
  - [probe] Adding A and B together does **not** fire `For(A).With(B)`.
  - [probe] Adding A and Frozen together **does** fire `For(A).Without(Frozen)`.
- **Docs and code disagree on multi-component `For` for removal.**
  - The docs say all the listed components must be removed together ([guide L58-61](https://github.com/mlange-42/ark/blob/v0.8.3/docs/content/events/index.md?plain=1#L58-L61)).
  - The code behaves as "any" (events.go L462). [probe] `For(A,B)` fired when only A was removed.

### Unity Entities

- **Change filters take at most two types, ORed.**
  - Up to two components, "use `ArchetypeChunk.DidChange`" beyond that ([IJobChunk](https://docs.unity3d.com/Packages/com.unity.entities@1.4/manual/iterating-data-ijobchunk-implement.html#skipping-chunks-with-unchanged-entities)). [doc]
  - The limit is `ChangedFilter.Capacity = 2` ([EntityQueryFilter.cs L19-31](https://github.com/needle-mirror/com.unity.entities/blob/1.4.8/Unity.Entities/Iterators/EntityQueryFilter.cs#L19-L31)).
  - "at least one" type changed ([EntityQueryManager.cs L1373-1413](https://github.com/needle-mirror/com.unity.entities/blob/1.4.8/Unity.Entities/Iterators/EntityQueryManager.cs#L1373-L1413)). [src]
- **No enter or exit.** The documented idiom is a tag plus a cleanup component ([Create a cleanup component](https://docs.unity3d.com/Packages/com.unity.entities@1.4/manual/components-cleanup-create.html)). [doc]
  - Has the tag but no cleanup component: new, so add the cleanup component.
  - Has the cleanup component but no tag: destroyed, so clean up and remove it.
- **Enableable components** match as absent when disabled, with no structural change and no enter/exit signal ([Use enableable components](https://docs.unity3d.com/Packages/com.unity.entities@1.4/manual/components-enableable-use.html)). [doc]
- **A promised API that never shipped.** Joachim Ante (staff), 2020: "We are working on a design for making reactive onadd / onchange / onremove as easy as Entities.ForEach" ([thread](https://discussions.unity.com/t/new-enabled-disabled-state-filtering/793663)). No such API exists in 1.4.8. [src]

---

## 6. Cost when nobody reads, and on the write path

**In short:** Bevy and Unity **always** store and write change versions, and Bevy's attempts to make that opt-in have not landed. flecs, EnTT and ark gate on "is anyone listening". flecs's gates are **sticky**: they are never cleared once set. Bevy's `RemovedComponents` is written on every removal even with zero readers. Published numbers are scarce. None of these projects publishes a benchmark for a buffered per-reader log.

### Bevy

- **Always stored.**
  - Every column has `added_ticks` and `changed_ticks`, 8 bytes per component instance ([column.rs L25-30](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/storage/table/column.rs#L25-L30)).
  - Every `DerefMut` writes a tick ([traits.rs L465-472](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/change_detection/traits.rs#L465-L472)). [src]
  - `#[component(immutable)]` still stores ticks ([component/mod.rs L671-672](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/component/mod.rs#L671-L672)). [doc]
- **Opt-out attempts.**
  - [#4882](https://github.com/bevyengine/bevy/issues/4882) is open; its consensus is that it "*must* be runtime configurable".
  - [#6659](https://github.com/bevyengine/bevy/pull/6659) and [#17629](https://github.com/bevyengine/bevy/pull/17629) ("As-needed change detection") were closed.
- **Readers scan every matched row.** `Changed`/`Added` "will iterate over all of them even if none of them were changed" ([filter.rs L904-908](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/query/filter.rs#L904-L908)). [doc]
- **`RemovedComponents` is written unconditionally** on every removal and despawn, one entry per component ([remove.rs L208](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/bundle/remove.rs#L208)). [src] The hooks PR calls it "on average more costly than … `on_remove` hooks due to the early-out" ([#10756](https://github.com/bevyengine/bevy/pull/10756)). [doc]
- **Hooks and observers are gated per archetype.** Ten `ArchetypeFlags` bits are checked before every dispatch ([archetype.rs L358-375](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/archetype.rs#L358-L375)). [src]
- **Published numbers.**
  - Adding hooks cost "1-5% on add_remove … 1-3% on insert" ([#10756](https://github.com/bevyengine/bevy/pull/10756)).
  - [#25157](https://github.com/bevyengine/bevy/pull/25157), merged for 0.20, adds an opt-in per-column summary tick:

    | Case | Before | After |
    |---|---|---|
    | no summary tick | 8.88 µs | 8.73 µs |
    | sequential `Mut` writes, with summary tick | 8.73 µs | 15.67 µs (1.75×) |
    | parallel `Mut` writes, with summary tick | 104.9 µs | 119.2 µs |
    | `extract_meshes_for_gpu_building`, 1.6M instances | 5.13 ms | 0.039 ms |

  - Closed [#23519](https://github.com/bevyengine/bevy/pull/23519) (change indexes paged at 256 rows): 4.58 ms went to 0.024 ms at 4M cubes, but `none_changed_detection/50000` regressed from 14.2 µs to 38.3 µs.
  - Draft [#11120](https://github.com/bevyengine/bevy/pull/11120): `many_foxes` regressed from 3.06 ms to 4.21 ms.

### flecs

- **An unobserved `set` is a memcpy.**
  - For ids below `FLECS_HI_COMPONENT_ID` (256), a `set` that nobody observes just copies memory, with no dirty mark and no events ([entity.c L2382-2386](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/entity.c#L2382)). [src]
  - "Observes" means `non_trivial_set[id]`: set by a value hook, an OnSet observer, or a change-detecting query ([type_info.c L655-658](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/type_info.c#L655), [observer.c L85-93](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/observer.c#L85), [cache.c L641-668](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/query/cache/cache.c#L641)). [src]
  - Ids at or above 256 always take the slow path.
- **Structural events are gated per table** by the flags `EcsTableHasOnAdd`/`OnRemove`/`OnSet` ([observer.c L67-80](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/observer.c#L67), [component_actions.c L318-383](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/component_actions.c#L318)). [src]
- **The gates are sticky.** The "no observers left" table event is `break; /* TODO */` ([table.c L2517-2518](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/storage/table.c#L2517)), and `non_trivial_set` is never cleared. [src]
- **Change detection state is lazy.** Dirty marking is a null check until a change-detecting query touches the table ([table.c L1402](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/storage/table.c#L1402), [L1433-1449](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/storage/table.c#L1433)). [src] A change-detecting query loses the fast "trivial cache" iterator ([cache.c L630-637](https://github.com/SanderMertens/flecs/blob/v4.1.6/src/query/cache/cache.c#L630)). [src]
- **Documented costs.**
  - Wildcard-event observers "add significant overhead" ([ObserversManual L624](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/ObserversManual.md?plain=1#L624)).
  - Monitors cost more because they evaluate twice ([L1388](https://github.com/SanderMertens/flecs/blob/v4.1.6/docs/ObserversManual.md?plain=1#L1388)).
  - [doc]
- **Published numbers:** the 4.1 release post reportedly cites 2× lower change-detection overhead. The page ([Medium](https://ajmmertens.medium.com/flecs-4-1-is-out-fab4f32e36f6)) returned 403 and was seen only as a search snippet, so it is **unverified**. No other primary numbers were found.

### EnTT

- **Signals are a mixin, "easily disabled if not needed"** ([entity.md L102-106](https://github.com/skypjack/entt/blob/v3.16.0/docs/md/entity.md?plain=1#L102-L106)). [doc]
  - With no listeners, `emplace` and `patch` still loop over an empty vector.
  - `pop` and `insert` check `empty()` and take the bulk path ([mixin.hpp L73-116, L335-375](https://github.com/skypjack/entt/blob/v3.16.0/src/entt/entity/mixin.hpp#L73-L116)). [src]
- **Compiling it out.**
  - Globally with `ENTT_NO_MIXIN` ([config.h L77-81](https://github.com/skypjack/entt/blob/v3.16.0/src/entt/config/config.h#L77-L81)), which is not in `config.md`.
  - Per type by specialising `storage_type` ([fwd.hpp L226-262](https://github.com/skypjack/entt/blob/v3.16.0/src/entt/entity/fwd.hpp#L226-L262)). [src]
  - Either way the type also loses groups and reactive storage ([group.hpp L147-148](https://github.com/skypjack/entt/blob/v3.16.0/src/entt/entity/group.hpp#L147-L148)).
- **Published numbers:** none from the maintainer. [probe] g++ 15.2 -O2, per op:

  | Operation | No mixin → mixin, no listeners |
  |---|---|
  | emplace | ≈ +0.3 ns |
  | patch | ≈ +0.5 ns |
  | erase | ≈ +2 ns, about 15–20%, via a virtual `pop` |

  One machine; treat as indicative.

### arche / ark

- **Nobody reading costs one check.**
  - arche: `if w.listener != nil` ([world.go L196](https://github.com/mlange-42/arche/blob/v0.15.3/ecs/world.go#L196)).
  - ark: `hasObservers[evt]`, added in [#435](https://github.com/mlange-42/ark/pull/435) ([events.go L270-288](https://github.com/mlange-42/ark/blob/v0.8.3/ecs/events.go#L270-L288)).
  - [src]
- **ark with readers.** A union-mask prefilter, then a linear scan of that event type's observers. Removals take the world lock whenever any observer of that event type exists, even one that won't match (world_internal.go L114-125). [src]
- **Published numbers:** none for observers. The benchmark tables have no observer rows ([benchmarks](https://mlange-42.github.io/ark/benchmarks/)). The only figures are "≈20ns per component" for custom event id lookup ([guide L140](https://github.com/mlange-42/ark/blob/v0.8.3/docs/content/events/index.md?plain=1#L140)) and query creation rising from 30 to 50 ns with the event system ([ark#337](https://github.com/mlange-42/ark/issues/337)).
- [probe] Local runs, Go 1.27.1 on this machine, ns per entity op:

  | Operation | No reader | With a reader |
  |---|---|---|
  | ark add event | 0.75 | 3.0 non-matching (1–10 observers); 9.6 with 1 matching; 23 with 10 matching |
  | ark `Map.Set` | 4.1 | 9.6 with a matching OnSet |
  | arche `NewEntity` | 15.8 | 22.4 with a no-op listener on all events |
  | arche `ExchangeBatch` | 1.18 | 6.1 |

### Unity Entities

- **Always on.**
  - Every archetype stores `uint × ComponentCount × chunkCount` versions ([ArchetypeChunkData.cs L21-43](https://github.com/needle-mirror/com.unity.entities/blob/1.4.8/Unity.Entities/Types/ArchetypeChunkData.cs#L21-L43)).
  - No define turns them off. [src]
- **Write path.**
  - Chunk iteration: one `uint` store per chunk per type.
  - `GetRefRW`/`SetComponentData`: one per call, i.e. per entity.
  - Structural changes: a loop over all destination types.
  - [src]
- **Cleanup components cost structural changes.** Destroy becomes a chunk move, removal is another structural change, and a runtime add is needed because cleanup components can't be baked. [src] Cost figures: sources silent.
- **Published numbers on versioning overhead:** sources silent. The only nearby figure is a 0.17 fast path with filtering disabled: "up to a 30% reduction in performance overhead" ([0.17 changelog](https://docs.unity3d.com/Packages/com.unity.entities@0.17/changelog/CHANGELOG.html)). It does not measure 1.4 versioning.

---

## What cuts against the map

These are stated plainly, as the map asks. None of them reopens a settled point; they are warnings the spec should answer.

1. **"A System's own acts during a run appear in its next run; the self-triggering `Changed` loop is stated in the spec as a trap."**
   - All three version-based ECSs (Bevy, Unity, flecs change detection) deliberately do the opposite for direct writes. Unity's source says "Never detect change of something the system itself changed", and Bevy has a test asserting it.
   - None of the three has a self-triggering loop for direct writes. They re-see only their *deferred* writes (Bevy `Commands`, Unity ECB played back by another system), because those are stamped at playback.
   - The ticket's premise that Bevy's `Changed` self-triggers is not borne out by Bevy 0.19.1 for `Mut` writes.
   - They get suppression for free: the stamp is the reader's own run version, and the last-seen version is set to it. A log appended at the funnel has no such identity. Suppressing there would need a writer identity per record, which is the "per-System identity on the Store" that [What counts as a value change, for a Changed Hook](https://github.com/dvoyni/cog/issues/268) rejected.
   - So the settled choice is defensible, but it is **the minority position**, and the trap it accepts is one prior art designed out.
2. **"One ordered stream per parameter … nothing is netted."**
   - Every buffered per-reader mechanism in these ECSs nets: Bevy ticks, flecs table counters, Unity chunk versions, EnTT reactive sets.
   - The only un-netted per-reader stream that shipped is Bevy's `RemovedComponents`. Its maintainers point users away from it ([#13928](https://github.com/bevyengine/bevy/issues/13928)): it is lossy after two updates, broken in `FixedUpdate` ([#16520](https://github.com/bevyengine/bevy/issues/16520)), and written even with zero readers.
   - Its failures come from a shared fixed-window buffer and unconditional writes, which cog's settled "since its own last run" and "only while a reader exists" avoid. But **no shipped ECS demonstrates the settled shape**, so there is no prior art for its cost.
   - Un-netted ordered delivery exists only in the immediate-callback designs (Bevy and flecs observers, EnTT signals, ark), and those keep no per-reader state.
3. **"`Values` holds `Q`'s fields at the act, the last values before an exit or despawn included."**
   - No shipped ECS keeps a removed value past a synchronous callback. Unity and EnTT make the user copy it. Bevy maintainers say deferring the read means "the data will be incorrect or simply gone" ([#21354](https://github.com/bevyengine/bevy/issues/21354)).
   - This is novel rather than contradicted, but its retention cost has no published reference point.
4. **"Entered/Exited" per query match, including exit by gaining a Filter type.**
   - Only flecs monitors ship this, synchronously and evaluating the query twice.
   - Bevy's issue is still at design-doc stage, and a contributor says nobody knows how to build it without "unacceptable performance losses".
   - ark's `With`/`Without` look similar but test the pre-operation mask.
   - Feasible (flecs), but the one team that shipped it charges double evaluation, and the one team designing it has not solved the cost.
5. **"Cost is per kind, per Store, and only while a reader of that kind exists."**
   - Supported by flecs, EnTT and ark. Contradicted in practice by Bevy and Unity, which always pay; Bevy's opt-out efforts have repeatedly stalled.
   - flecs shows a subtler hazard: its gates are **never cleared**, so "while a reader exists" degrades to "once a reader has existed".

---

## What this means for cog's Hooks

This is input, not decisions. Each item names the ticket it feeds.

### [What counts as a value change, for a Changed Hook](https://github.com/dvoyni/cog/issues/268)

- **Two camps.** The projects split into **explicit-call** (EnTT `patch`, flecs `set`/`modified`, ark `Map.Set`) and **access** (Bevy `DerefMut`, Unity RW access, flecs `inout` iteration). #268's "a writer bound this row" is in the access camp, as coarse as Unity's per-chunk access but per row.
- **Access over-reports, and every access-camp project documents it.** Unity's own transform system shipped the bug, and Bevy added `set_if_neq`/`bypass_change_detection` as escape hatches. Expect a consumer to ask for an escape hatch.
- **Bevy is finer-grained than "bound".** It stamps on `DerefMut`, not when `Mut` is handed out, so a `Mut` never dereferenced writes nothing. cog's `*T` field has no deref hook in Go, so bind-time is the finest point available. The spec should say bind-time is coarser than Bevy.
- **Explicit-call designs miss writes through references.** EnTT (`get`) and ark (pointer from `Get`) both document the miss. That matches #268's four escape routes.
- **Self-visibility.** See [cut 1](#what-cuts-against-the-map). #268's recommendation 4 said suppression "would need a per-System identity on the Store". Bevy and Unity show that a version-based design avoids that identity: stamp with the reader's own run version, and set last-seen to it. The same trick does not carry over to a record log.
- **Wraparound.** Bevy clamps periodically, under-reporting very old changes. Unity uses an undocumented signed 2^31 window. flecs is silent. #268's epoch re-stamp over-reports, which is the safer direction and has no counterpart here.

### [What one act puts into a Hooks stream: kinds, values and the edge cases](https://github.com/dvoyni/cog/issues/379)

- **Exit through a Filter.** flecs monitors are the reference semantics: evaluate `Q` against the before and after composition, fire on a no→yes or yes→no transition, nothing otherwise. They handle exit by *gaining* an excluded type ([probe]).
- **ark shows the tempting wrong reading.** Its `With`/`Without` test the old mask, which is not the same as entry or exit.
- **Remove then re-add inside a window.**
  - Immediate designs report two acts (Bevy observers, ark, arche).
  - Version designs report one indistinguishable "added/changed" (Bevy, Unity).
  - flecs deferred batching reports **OnAdd without OnRemove**.
  - The settled "nothing netted" matches the immediate designs. The flecs asymmetry is the failure to avoid.
- **Spawn and `Changed`.** Unity stamps every type as changed on create, so every change filter fires on spawn. The map's "spawned *and* entered" plus "at most one `Changed` between membership records" should say whether a Spawn also carries `Changed`. Prior art defaults to yes (Unity) or keeps the two separate (Bevy `Added` versus `Changed` ticks).
- **A System changing `Q` while iterating its own Hooks.** EnTT's reactive storage shows the concrete trap: entries appended during iteration are not visited, and `clear()` at the end drops them. "Reset when the run ends" must not discard acts recorded during the run.
- **Recycled indices.** EnTT's stale reactive entry, colliding with a recycled id, corrupts the set silently in release builds. The spec's "two distinct Entities" needs to hold at the storage level, not only in prose.
- **Despawn order across Stores.**
  - EnTT: per-storage "before" only, reverse pool order for a single destroy, forward for a range, undefined order across pools.
  - Bevy: all `Despawn`/`Discard`/`Remove` events before any drop.
  - Bevy's all-before-drop ordering is the one that lets `Values` be complete.

### [Where a Hook log lives, and what recording and resetting it cost](https://github.com/dvoyni/cog/issues/380)

- **No reference numbers.** No project publishes a benchmark for a buffered per-reader record log. The nearest comparisons:
  - **Bevy summary tick** (per-column opt-in): 1.75× on sequential `Mut` writes when enabled, 0 when not ([#25157](https://github.com/bevyengine/bevy/pull/25157)).
  - **Bevy change index** (paged): 2.7× regression on the no-changes read path ([#23519](https://github.com/bevyengine/bevy/pull/23519)).
  - **EnTT signals**, no listeners: +0.3 to +2 ns per op ([probe]).
  - **ark**: 0.75 ns per add event with no observer, 3 ns with a non-matching one, about 10 ns with one matching ([probe]).
- **Last values before a Despawn.** Bevy fires every removal event before any drop ([world_mut.rs L1644-1718](https://github.com/bevyengine/bevy/blob/v0.19.1/crates/bevy_ecs/src/world/entity_access/world_mut.rs#L1644-L1718)). That is the "capture before the first Store is emptied" shape #380 item 2 needs.
- **Nil check versus sticky gate.** ark's `hasObservers[evt]` bool and flecs's per-table flags are the two nil-check shapes. flecs's never-cleared flags are a warning if registration can be undone.
- **Locking.** ark takes the world lock on every removal whenever any observer of that event type exists, even a non-matching one. That is a cost of a coarse gate.

### [A Hooks reader that falls behind: what bounds its log](https://github.com/dvoyni/cog/issues/381)

- **Four answers in prior art:**
  1. **O(1) state that nets.** Bevy, Unity and flecs versions. The reader loses order and detail, never memory.
  2. **A set bounded by population.** EnTT reactive storage. It nets.
  3. **A fixed window that drops silently, with a missed count.** Bevy `RemovedComponents`, `missed_messages()`. The count is the only overflow report found, and its maintainers steer away from the mechanism.
  4. **No buffering at all.** Observers.
- **None of these keeps order without a bound.** cog's settled shape rules out 1 and 2 as delivery, but either could be a fallback for a reader that overflowed.
- **Unity's `ShouldRunSystem` ignores change filters**, so a reactive system is scheduled every frame anyway ([SystemState.cs L666-701](https://github.com/needle-mirror/com.unity.entities/blob/1.4.8/Unity.Entities/SystemState.cs#L666-L701)). Rarely-run readers are rare in Unity by construction. That is not true of cog, where a System may subscribe to any event.

### [How recording is switched on for a Store, per Hook kind](https://github.com/dvoyni/cog/issues/382)

- **Every gated design switches on implicitly.** None asks the component owner.
  - flecs turns on `non_trivial_set` and table flags when an observer or change-detecting query is created.
  - Bevy sets archetype flags when a component-scoped observer is registered.
  - ark sets `hasObservers` on registration.
- **Explicit per-type opt-in exists only as a compile-time removal of the whole mechanism** (EnTT `ENTT_NO_MIXIN` or a `storage_type` specialisation). It also removes groups and reactive storage for that type.
- **The Bevy opt-out discussion concluded it "*must* be runtime configurable"** ([#4882](https://github.com/bevyengine/bevy/issues/4882)).
- **Gaps and warnings.**
  - No project gives the owning plugin a say over who observes its components. Sources silent.
  - Bevy has a source-read-only gap: lifecycle observers naming no component are dispatched only on archetypes whose flag a component-scoped observer already set. Deriving the gate from registrations must cover every reader shape.

### [Deferred structural change and Hooks: when a drained change is recorded](https://github.com/dvoyni/cog/issues/383)

- **All three deferred-command designs record at playback, not at the call.**
  - Bevy: lifecycle events for `Commands` fire at the sync point.
  - flecs: at merge.
  - Unity: ECB playback stamps the playback-time version.
- **Consequence in Bevy and Unity:** the originating system sees its own deferred changes on its next run, while its direct writes are hidden. That split is a documented source of confusion ([#1471](https://github.com/bevyengine/bevy/pull/1471) "Changes Made By Commands").
- **flecs's merge-time batching is the warning for a drain.** Folding one entity's commands into a single move drops the OnRemove of a remove-then-add and nets add-then-remove to nothing, including for monitors. A drain that collapses per entity would break "one act, one record" and "nothing is netted".
- **Bevy observers queue their commands until all observers have run** ([#19569](https://github.com/bevyengine/bevy/issues/19569)). This is one precedent for a "`Last()`-ordered" drain window.
