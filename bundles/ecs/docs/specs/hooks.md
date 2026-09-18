# cog ecs — Hooks

A **Hook** is one record of one act on one Component of an Entity: it was
spawned, despawned, added, removed or changed. This document specifies how a
System learns of them. It covers what a System names to read them, which acts
produce which records and what value each carries, when a System sees a record,
what recording costs the Systems that never read one, and what keeps the
records from growing without bound.

It is a companion to [`ecs.md`](ecs.md) and is bound by the same four
requirements, in the same order: zero heap allocation on the hot path,
concurrency from cog's existing scheduler, extensible along named axes, and
reasonably simple to use. It adds one of its own, which the owner ruled before
anything else here was decided, and which every section below was held to:

> **A Hook never costs System parallelism.** No act reads a Store other than its
> own, and no handler's lock set grows because a reader exists.

This document is assembled from the resolved tickets of
[ecs: Hooks, and how a System learns what happened to an
Entity](https://github.com/dvoyni/cog/issues/377), and every section cites the
tickets it came from. It follows `ecs.md`'s conventions: where a claim rests on
something unverified it is marked **Gap** and says what would settle it, and
where assembling the decisions side by side settled something no ticket did, it
is marked **Settled here**.

**This document is built.** `github.com/dvoyni/cog/bundles/ecs` implements it,
and what a Hook costs on the build is measured in the package README's [*What a
Hook costs*](../README.md#what-a-hook-costs). The design was first measured on
the throwaway branch [`proto/ecs-hooks`](https://github.com/dvoyni/cog/tree/proto/ecs-hooks);
[What it costs](#what-it-costs) keeps those figures as the record the build was
held to, and the README's supersede them. [Required work](#required-work) is the
checklist the implementation was built from. The rules in this document are also
the list of behaviour tests. Where the package and this document disagree, the
package is the defect unless this document says otherwise.

`ecs.md` changed when this document landed, and those sections point here. What
binds code that names no Hooks is stated there in full: a `List`'s `Set`, the
Validation checks every `List` user meets, and [`ecs.ShrinkCmd`](ecs.md#giving-memory-back).

---

## Contents

- [Vocabulary](#vocabulary) · [Who asked](#who-asked)
- [The parameter](#the-parameter)
- [What one act records](#what-one-act-records)
- [Values, and a window](#values-and-a-window)
- [When a System sees a record](#when-a-system-sees-a-record)
- [How recording is switched on](#how-recording-is-switched-on)
- [The log, and the locks it is appended under](#the-log-and-the-locks-it-is-appended-under)
- [A reader keeps pace with its writers](#a-reader-keeps-pace-with-its-writers)
- [A value that holds a List](#a-value-that-holds-a-list)
- [What it costs](#what-it-costs)
- [Shapes that were rejected](#shapes-that-were-rejected)
- [Required work](#required-work) · [Out of scope](#out-of-scope)

---

## Vocabulary

**Hook** is in `CONTEXT.md` under **Entities and Components**, the glossary of
record. The other words here name parts of the mechanism. They are this
document's words, not glossary terms, and nothing outside `bundles/ecs` needs
them.

- **Hook** — one record of one act on one Component of an Entity: the act's
  kinds, and that Component's value. Not an *event*, which is the kernel's word
  for something published.
- **Kind** — one of five facts about an act, all relative to the Component:
  *spawned*, *despawned*, *added*, *removed*, *changed*. One act can have
  several.
- **Kind set** — one of eight closed types a System names to choose which
  records it is given.
- **Reader** — a System that takes an `*ecs.Hooks[T, K]`.
- **Writer of `T`** — a System holding a handle that can act on `T`'s Store:
  a `*T` Query field, `Set[T]`, `Remove[T]`, a `Spawn[S]` whose Component set
  holds `T`, or `WriteableEntities`.
- **Watched** — a Store is watched for a kind while at least one registered
  reader's kind set contains it.
- **The log** — a watched Store's record of acts, shared by every reader of that
  Store.
- **A reader's copy** — what one reader is given at its run start: its part of
  the log, filtered, folded and filled.
- **A window** — the span of one Entity's records between two of its additions
  or removals.

---

## Who asked

`ecs.md` ruled `OnAdd[T]`/`OnRemove[T]` out *"so a future need arrives with a
use case rather than as a reserved hole"*. Three needs have arrived, from
different directions, and this is the one mechanism that answers all of them
([the design brief](https://github.com/dvoyni/cog/issues/377#issuecomment-5666703963)).

- **Physics keeps a spatial index over static geometry**
  ([Static geometry as Entities](https://github.com/dvoyni/cog/issues/288)). It
  must learn when an Entity gains or loses its static shape Component. Diffing
  against a Query every tick, and an explicit reindex request from the app,
  were both put to the owner and rejected. It reads
  `Hooks[Shape, HookAddedRemoved]`. Of the seven requirements recorded in
  [#264](https://github.com/dvoyni/cog/issues/264), six are met as stated:
  - add and remove of one type are observable, Despawn included;
  - delivery waits for the start of its System's run;
  - nothing takes `write{*Entities}`;
  - a remove then re-add is two records;
  - steady state allocates nothing, and nothing watching costs a load and a
    branch;
  - it pays nothing for value writes.

  The seventh, "the observer is the plugin that owns the Store", is widened:
  any plugin that depends on the owner may read
  ([How recording is switched on](https://github.com/dvoyni/cog/issues/382)).
- **Audio learns that a voice's Entity was despawned**
  ([The ECS binding](https://github.com/dvoyni/cog/issues/303), item 4). A
  Reference to a despawned Entity resolves to nothing, so the binding cannot
  reach back. It reads `Hooks[Emitter, HookDespawned]` on the Component it
  registers itself, and receives each despawn with the emitter's last value.
- **Indexing Systems in general**, including the request closed as
  [ecs: indexing system](https://github.com/dvoyni/cog/issues/262). That request
  asked for a `Deleted[T]` Query field. A removed row is gone and cannot be a
  Query field, but it can be a record, and `HookRemoved` delivers one.

**Prior art does not ship this shape**, and where it cuts against the design is
recorded rather than dismissed
([Prior art](https://github.com/dvoyni/cog/issues/378), findings in
[`docs/research/ecs-hooks-prior-art.md`](https://github.com/dvoyni/cog/blob/research/hooks-prior-art/docs/research/ecs-hooks-prior-art.md)
on `research/hooks-prior-art`). Bevy 0.19.1, flecs 4.1.6, EnTT 3.16.0,
arche 0.15.3, ark 0.8.3 and Unity Entities 1.4.8 each deliver in one of two
ways: netted versions per reader (Bevy's ticks, Unity's chunk versions, flecs
table counters), or immediate callbacks that keep no reader state (observers,
signals, ark events). **None keeps an ordered log per reader with retained
values.** Its nearest relative, Bevy's `RemovedComponents`, loses records and
its maintainers steer users away from it. Three choices below are therefore
the minority's, and each says so where it is made: nothing is netted, removed
values are kept for a later reader, and a System sees its own structural acts
in its next run.

---

## The parameter

A System reads Hooks by naming **`*ecs.Hooks[T, K]`** in its signature. It is an
ordinary System, scheduled like any other. There is no separate kind of System,
and one that reads Hooks may still change Entities
([Where a Hook log lives](https://github.com/dvoyni/cog/issues/380)).

```go
func index(h *ecs.Hooks[Shape, ecs.HookAddedRemoved]) {
    for e, hook := range h.All() {
        if hook.IsAdded()   { tree.Insert(e, hook.Value) }
        if hook.IsRemoved() { tree.Remove(e) }
    }
}
```

- **`T` is one registered Component.** `Hooks[*T, K]` names no registered
  Component and fails composition like any unregistered one. To write the
  Entity a record names, use `Set[T].Ref`.
- **`K` is one of eight kind-set types.** Each is an empty struct satisfying an
  unexported interface, so nothing outside `ecs` can add one.
- **`All()` is `iter.Seq2[Entity, *ecs.Hook[T]]`** and yields records in the
  order the acts happened. An index applies them in that order, and nothing is
  netted.
- **`ecs.Hook[T]`** carries `Value T` and five methods: `IsSpawned()`,
  `IsDespawned()`, `IsAdded()`, `IsRemoved()`, `IsChanged()`. No kind constant
  is exported. Measured while charting, the methods inline on a generic
  `*Hook[T]` and cost nothing against a hand-written bit test, with the kind
  field placed before `Value`.
- **Settled here: the `*Hook[T]` is valid until the System's run ends.** The
  reader's copy is cleared then. `Value` may be copied out and kept, subject to
  [A value that holds a List](#a-value-that-holds-a-list).
- **Settled here: two `Hooks` parameters in one System are two readers.** Each
  keeps its own place in the log and its own copy, and both are the same System
  for [a System's own changes](#a-system-never-sees-its-own-changed).

### The kinds

All five are relative to `T`.

- **Added:** `Set[T].UpdateFor` added `T` to an Entity that lacked it, or a
  Spawn carried `T`.
- **Removed:** `Remove[T].From` took `T` away, or an Entity holding `T` was
  despawned.
- **Spawned** is the special case of Added made by a Spawn.
- **Despawned** is the special case of Removed made by a Despawn.
- **Changed:** `T`'s bytes differ when a writer's run ends, or the writer marked
  the Entity with `Set[T].MarkChanged` for a write the bytes cannot show. See
  [Changed is a difference in bytes](#changed-is-a-difference-in-bytes).

**One act, one record, and a record may be several kinds.** A Spawn carrying
`T` is spawned, added and changed. A Despawn of an Entity holding `T` is
despawned and removed. **Every addition also carries Changed**, whatever route
it came by, because it gives the reader a value of `T` it has not seen. A
Changed reader therefore never misses an Entity
([What one act puts into a Hooks stream](https://github.com/dvoyni/cog/issues/379) §2).
Removals never carry Changed.

### The eight kind sets

"Every addition" includes Spawns, and "every removal" includes Despawns.

| `K` | delivers | typical reader |
| --- | --- | --- |
| `HookSpawned` | Spawns carrying `T` | spawn effects |
| `HookDespawned` | Despawns of Entities holding `T` | audio's voices |
| `HookSpawnedDespawned` | both | lifetime bookkeeping |
| `HookAdded` | every addition | |
| `HookRemoved` | every removal, with `T`'s last value | #262's `Deleted[T]` |
| `HookAddedRemoved` | every addition and every removal | physics' index |
| `HookAddedChanged` | every addition and every change | uploading current values |
| `HookAll` | every addition, change and removal | a mirror |

**A record is delivered when any of its kinds is in `K`.** So `HookSpawned`
never delivers an addition made by `UpdateFor`, and `HookDespawned` never
delivers a plain `Remove`.

**Combinations left out, and why** ([#380](https://github.com/dvoyni/cog/issues/380)):

- **Spawns with every removal**, or **every addition with Despawns only**. Each
  silently leaks or shrinks an index: an `UpdateFor` re-add is missed, or a
  `Remove` from a live Entity is.
- **Changes with Despawns only.** The same leak, for a mirror.
- **`HookAddedChangedRemoved`.** It would be identical to `HookAll`.

### What `IsX` reports

**On a delivered record, `IsX()` reports every fact that is true of it**, not
only the kinds `K` names. `IsSpawned()` is readable under `HookAdded`,
`IsDespawned()` under `HookRemoved`, and `IsAdded()` under `HookAddedChanged`.
None of them costs anything to record, because an act is recorded with all its
kinds ([#382](https://github.com/dvoyni/cog/issues/382) §4).

**In a validating build, `IsX()` panics for a kind `K` can never deliver**,
because asking is the bug of an index that never removes. Settled here, from the
kinds each record can carry:

| `K` | `IsX()` that panics |
| --- | --- |
| `HookSpawned`, `HookAdded`, `HookAddedChanged` | `IsRemoved`, `IsDespawned` |
| `HookDespawned`, `HookRemoved` | `IsAdded`, `IsSpawned`, `IsChanged` |
| `HookSpawnedDespawned`, `HookAddedRemoved`, `HookAll` | none |

### No filters

**A Hook narrows by nothing but `T` and `K`.** `With`/`Without` inside a Hook is
rejected ([#380](https://github.com/dvoyni/cog/issues/380)):

- **Checked at the act**, a filter reads another Store, which is a widened lock.
- **Checked at delivery**, it silently loses exactly what an index needs. An
  Entity that gains the filter's Component after `T` makes no act on `T`, so it
  is never delivered. A despawned Entity no longer passes the filter, so its
  removal is dropped.

**A reader that cares about Entities holding several Components** reads the
Hooks of each Component that can change its answer, and checks the rest itself
with `Get`, keyed by Entity.

### What it declares

| parameter | declares | what it is |
| --- | --- | --- |
| `*ecs.Hooks[T, K]` | `read{*Entities}` + `read{*Store[T]}` | what happened to `T` since the System's last run |

That is a Query over `T`'s lock set. As with a Query, the read of `*Store[T]` is
also an ownership declaration, so the reader's plugin must depend on `T`'s owner
and the kernel's composition check fires if it does not. **It changes no other
handler's lock set.** `TestAHookNeverWidensALockSet` holds that exactly
([The benchmarks hooks.md publishes](https://github.com/dvoyni/cog/issues/385) §3).

---

## What one act records

### Which acts are recorded

**An act is recorded when one of its kinds is watched, and its record keeps all
of its kinds. A removal is also recorded wherever an addition or a change is
watched, because it fixes their values.** A Store's watched kinds are the union
of its readers' kind sets
([How recording is switched on](#how-recording-is-switched-on)).

| act | record's kinds | recorded when the Store watches |
| --- | --- | --- |
| a Spawn carrying `T` | spawned + added + changed | Spawned, Added or Changed |
| `Set[T].UpdateFor` adding `T` | added + changed | Added or Changed |
| a writer's run end, `T`'s bytes differ or `Set[T].MarkChanged` marked the Entity | changed | Changed |
| `Remove[T].From` | removed | Spawned, Added, Changed or Removed |
| a Despawn of an Entity holding `T` | despawned + removed | any kind |

- **Every recorded removal copies `T`'s last value** into the log, taken at the
  act, before the Stores are emptied.
- **Rows are copied for a Changed compare only when Changed is watched.**
- **A Component with no fields never records Changed**, so `HookAddedChanged`
  on a Tag delivers additions only.
- **An `UpdateFor` that replaces a value is not an addition.** It records
  Changed only if the bytes differ when its writer's run ends.
- **An act that does nothing records nothing.** A `Remove.From` on an Entity
  without `T` and a Despawn of a dead Entity both return `false` and append
  nothing.

**This table corrects the wording of [#382](https://github.com/dvoyni/cog/issues/382) §4,**
which said a last value is copied "only when Removed or Despawned is watched",
while also saying [#380](https://github.com/dvoyni/cog/issues/380)'s two special
cases follow from its rule. Those two cases are that `UpdateFor`'s additions are
skipped when only Spawned or Despawned is watched, and plain removals only when
only Despawned is. The prototype records removals as the table does, and every
figure below was taken on it. Ruled by the owner at handover: with removals
recorded wherever additions or changes are watched, every addition and change
carries a value under all eight kind sets, with no further rule.

### Changed is a difference in bytes

**A Changed record means `T`'s bytes differ, not that something was written**
([What counts as a value change](https://github.com/dvoyni/cog/issues/268)). On a
Store watched for Changed, a row handed to a System with write access is copied,
and the copy is compared when that System's run ends:

- **a `*T` Query field** copies the whole Store when the Query binds for the run.
  That is exact for a System holding `write{T}`, since a row it was not handed
  cannot change, and the Query's hot loop is untouched: `queryCursor.fill`
  still inlines at cost 67;
- **`Set[T].Ref` and `Set[T].UpdateFor`** copy per row, deduplicated per run.

**At the writer's run end, each copied row whose bytes differ appends one Changed
record,** under the `write{T}` the writer already holds. Identical bytes record
nothing, whatever was called, unless the row was marked (below). A row whose
Entity lost `T` during the run compares against nothing and records nothing.

**`Set[T].MarkChanged(e Entity)` forces a Changed record** for a write the byte
compare cannot see
([ecs: Set[T].MarkChanged](https://github.com/dvoyni/cog/issues/396)). It is on
`Set[T]` because `Set[T]` already holds `write{*Store[T]}`, so it declares
nothing new, and it is named for the Hook kind, not "dirty".

- **On a Store watched for Changed, it marks `e`'s row.** At the writer's run
  end, a marked row appends one Changed record whether or not its bytes differ.
- **It is deduplicated with the byte compare.** One writer run yields at most
  one Changed per Entity, however many times it marks or writes that Entity, on
  any route: a `*T` Query field, `Ref`, `UpdateFor`, or `MarkChanged` before or
  after the Query binds.
- **It does nothing** on an Entity that does not hold `T` when it is called
  (even if the Entity gains `T` later in the run, whose addition already carries
  Changed), on a dead Entity, on a Store no reader watches for Changed, and for a
  Tag, which never records Changed.
- **Every other Changed rule holds.** A System never sees its own mark's
  Changed; a mark followed by a removal in the same run records only the
  removal; a marked Changed folds in each reader's copy like any other.
- **It costs** about 1.0 ns a call on a Store nothing watches and 4.4 ns per
  marked row on a watched one, the run end's record included
  (`BenchmarkHookMarkChanged`, 10k Entities, 1% marked). Its marks are allocated
  on the writer's first marking run and kept.

**Why bytes are exact.** A `string` cannot be written in place. A `List` change
shows in its header ([below](#a-list-shows-its-changes-in-its-header)). A write
through an `assets.Blob` already breaks the Blob's contract. So a byte compare raises
no false alarm for any kind a Component may hold, and there is no call a System
can forget to make. **Two misses are stated:** a write through an `assets.Blob`, and
a `List` nested in another `List`'s element (the Gap below).

**Gap: a `List` nested in another `List`'s element.** `At` returns a copy of
the element, so a nested `List` can only be written through that copy. Its `Set`
changes the backing array the stored row shares, and bumps the copy's
generation, not the stored row's bytes, so the byte compare does not see it.
The compare does not look inside
([ecs: List.Set by pointer with a generation](https://github.com/dvoyni/cog/issues/388),
ruled on [ecs: Hooks record Changed](https://github.com/dvoyni/cog/issues/392)).
**A System that needs the Changed record calls `Set[T].MarkChanged(e)`** on the
outer Entity after the nested `Set`
([ecs: Set[T].MarkChanged](https://github.com/dvoyni/cog/issues/396)). Writing
the whole outer element back through the stored `List`, `Rows.Set(i, row)`, also
records it, because that bumps the generation the compare sees.
`TestAMarkRecordsANestedListSet` shows both halves: the nested `Set` alone records
nothing, and with `MarkChanged` records one Changed carrying the nested write.

**A Component some reader watches for Changed has no implicit padding**
([ruled on #392](https://github.com/dvoyni/cog/issues/392)). A byte compare sees
a struct's padding, and Go leaves padding holding whatever was there, so equal
fields can differ in bytes. The padding is spelled out instead, as blank fields:

```go
type Flags struct {
    Visible bool
    _       [7]byte // was implicit padding
    Layer   int64
}
```

- **Measured, on Go 1.27.1 windows/amd64, ten runs in each build mode.**
  `TestExplicitPaddingRecordsNoChangedForEqualFieldValues` writes values equal
  field by field on eight routes:
  - a `*T` field assigned a value a function built after a stack filler, assigned
    a literal, and written field by field;
  - `Ref` assigned a built value, and written field by field;
  - `UpdateFor` with a literal, with a built value, and with the value `Of` read
    back.
- **Explicit `_ [N]byte` padding recorded 0 Changed on every route,** flat, in a
  nested struct, and in an array, and every blank byte stayed zero.
- **The same layout with implicit padding recorded 8 of 8 on most of those
  routes.** The differing bytes were padding alone: the filler's pattern, or
  stack leftovers. Every field was equal.
- **The Validation check.** In a validating build, when a reader whose kind set
  holds Changed (`HookAddedChanged`, `HookAll`) registers, the ECS walks its
  Component's layout, nested structs and arrays included. A gap between fields,
  or at the tail after the last field (a trailing zero-size field included),
  panics at registration. The message names the Component, the field the gap
  follows or "at the end", the byte count, and the fix: an explicit `_ [N]byte`
  field there, which keeps the same size and alignment. A Component no reader
  watches for Changed is not checked.
- **A release build does not check, and pays nothing,** through `const validate`.
  There, a Component with implicit padding can record a Changed no field made. It
  never misses a real change.

**Settled here: a change followed by a removal in the same writer run records
only the removal.** The change record would be appended at run end, and by then
the row is gone. A `HookAddedChanged` reader, which is not given removals, sees
nothing of that Entity's last change. It has no Entity left to apply it to.

**Write access does not count as change.** Bevy and Unity count it, and Unity's
own transform system shipped the over-report. See
[Shapes that were rejected](#shapes-that-were-rejected).

### A System never sees its own Changed

**A reader is never given a Changed record produced by its own System's run
end.** The compare runs inside the writer's own run, so that System's copy skips
it, and no identity is kept on the Store for it.

- **It still sees its own spawns, despawns, additions and removals** in its next
  run. An addition it made itself is delivered with `IsChanged()` true.
- **Its own writes still show in the values** of later records: an addition's
  value is taken at the next removal or at run start, whoever wrote it.
- **This is the single exception** to "a System's own acts appear in its next
  run". Only Changed can loop on itself silently: a System writes because a
  value changed, and the write changes it again. Bevy, Unity and flecs hide
  direct writes from their writer for the same reason.

### A List shows its changes in its header

**`List.Set` has a pointer receiver and adds one to a generation kept in the
List's header**, so an in-place change shows in the stored Component's bytes and
a byte compare catches it. The header grows from 24 to 32 bytes. That changes
the List for every user, watched or not, so it is specified in
[`ecs.md` § The List](ecs.md#the-list), with its cost and the table of where
`Set` is legal.

---

## Values, and a window

**`hook.Value` holds `T`** ([#379](https://github.com/dvoyni/cog/issues/379) §4,
over one Component by [#380](https://github.com/dvoyni/cog/issues/380)):

- **a removal or a despawn** carries `T`'s last value, taken at the act;
- **an addition or a standalone change** carries `T` as it stands at the
  Entity's *next* removal, or at the reader's run start if it has not been
  removed since.

**A window holds at most one record that carries a value.** Folding happens in a
reader's copy, per reader and per Entity:

- **a change inside a window an addition opened folds into that addition**, so
  the addition absorbs it;
- **a standalone Changed appears only in the window already open when the
  reader's copy began**, at the position of that window's first change, and
  later changes in the window fold into it.

So between two additions or removals of one Entity there is at most one Changed.
Example, as the reader of `HookAll` sees it:

| in the log | delivered |
| --- | --- |
| added(v0), changed(v1), removed(v1), added(v2), changed(v3) | added+changed(v1), removed(v1), added+changed(v3) |

**Why the value is taken at the next removal:** an index's insert key must equal
its remove key. Under "the value at the act", the sequence added(v0), then a
write it is not given (folded, or its own), then removed(v1), leaves an index
keyed by bounds unable to find its entry. **Only removals keep copies.** An
addition or a change records only "this Entity", so recording a value change
never reads another Store and never needs more than its own lock.

**Filling.** At run start, after folding, the reader fills the value of every
addition and standalone change still open from the live Store. It holds
`read{T}`, so no writer can run during the fill.

### Records are history

- **Records are fixed when the reader's run starts.** `Value` is a copy, and
  nothing the System does during the run changes a record it has not reached
  yet.
- **The Entity in a record may already be gone or different**, changed by a
  later record or by this System during this run. `Get[T].Of(e)` can fail, and
  `Value` is still safe to read.
- **A record carries the full `Entity`, generation included.** A despawned
  Entity and the one that reuses its index are two Entities in the stream and
  are never folded together ([#379](https://github.com/dvoyni/cog/issues/379) §7).

---

## When a System sees a record

**Each reader has its own copy: the records appended since the end of its own
last run.** The copy is reset when the run ends, whether or not it was iterated.
A System's own acts during a run are appended to the log after its copy was
taken, so it sees them in its next run. Its own Changed records are the
exception [above](#a-system-never-sees-its-own-changed).

**No new ordering promise is made.** A reader sees every act made by Systems
that ran before its run started, and none from those that run after. Where two
Systems declare no order, which ran first is unspecified, as in
[`ecs.md` § What a System sees](ecs.md#what-a-system-sees). **A record never
lands inside a reader's run**: every append happens under `write{T}` or
`write{*Entities}`, and a reader holds `read{T}` and `read{*Entities}`.

**A reader registered after a writer still sees that writer's acts.** Nothing
runs until registration closes, so a reader has taken its place in the log
before any act ([#382](https://github.com/dvoyni/cog/issues/382)).

### A drained change

[ecs: defer structural change through a typed buffer](https://github.com/dvoyni/cog/issues/260)
is parked and not designed here. Whenever it is built, it is bound by the
constraint written onto it from
[Deferred structural change and Hooks](https://github.com/dvoyni/cog/issues/383):

- **A drained change is recorded at the drain**, under the drainer's
  `write{*Entities}`, as the same act an immediate change is, with the same
  records. An index cannot tell the two apart. A deferring handle records
  nothing at the call and declares nothing for Hooks.
- **Visibility.**
  - A reader on the same event as the deferring System runs before the drain,
    so it sees a change queued in publication *n* in its run for *n + 1*.
  - A reader subscribed `Last()` on the same event has no order against the
    drainer, so it sees the change in this publication or the next. **Which one
    is unspecified**, and nothing is lost or duplicated either way.
  - A reader on another event sees it at its first run after the drain.
  - The System that queued a change sees it in its next run.
- **Order.** A Store's log follows the order in which the drain applies changes
  to it, and the drain order is deterministic: handles in the order they enrolled
  at registration, each buffer in the order changes were queued, and nothing
  iterating a map.
- **A drained change that does nothing records nothing**, such as a despawn of an
  Entity already dead by the drain.
- **A drained removal or despawn captures `T` at the drain**, including writes
  made between the call and the drain. The Changed records those writes
  produced are already earlier in the log.
- **A drain that appends to a Store's log counts as one writer run** on that
  Store, for [the Validation check](#validation-mode-checks-the-rule).
- **If #260 ever defers `UpdateFor` or `From`**, against its own recommendation:
  - a deferred `From` records a removal at the drain;
  - a deferred inserting `UpdateFor` records an addition;
  - a deferred replacing `UpdateFor` records Changed at the drain by comparing
    old and new bytes directly, attributed to the System that queued it, so that
    System still never sees its own Changed.

---

## How recording is switched on

**Readers switch recording on, and only readers**
([How recording is switched on](https://github.com/dvoyni/cog/issues/382)).

- **Each `Hooks[T, K]` a System names adds `K`'s kinds to `T`'s Store** while
  that System registers. Nothing is declared at `RegisterComponent[T]`, and the
  owner has no say beyond the dependency every read already needs.
- **Which kinds a Store records is fixed when registration closes.** Every
  plugin's `Register` finishes before the kernel finalises, and no Registrar
  exists after that, so every reader is known before any System runs. Recording
  starts with the first act.
- **A writer reads its Store's watched kinds when each of its runs starts**, not
  when it registers. That covers a `*T` Query field, `Set[T]`, `Remove[T]` and a
  `Spawn[S]` carrying `T`, and costs per handle per run, not per row: one load
  and one bit test for the check itself, and 2–4 ns per handle per run on the
  build once a writer's run end is counted, over its 1 ns budget (see
  [Budgets](#budgets-for-the-costs-nobody-asked-for)). Dependency order puts an
  owner's writers before a dependent plugin's readers, so a writer deciding at
  registration would never learn of them.
- **Row-copy buffers are allocated on a writer's first watched run and kept**, so
  steady state allocates nothing. Only [`ecs.ShrinkCmd`](ecs.md#giving-memory-back)
  releases them.
- **A Store nobody reads pays a nil check** on each structural act. A Despawn on
  a world with no watched Store takes the path it takes today.

**Cost is per kind, per Store, and only while a reader of that kind exists.**
Physics reading additions and removals pays nothing for value writes. A Changed
reader's cost falls on every writer of `T`, which copies rows for its run. That
is the reader's trade, published in [What it costs](#what-it-costs). An owner
cannot refuse it, because refusing would push the reader to diff the Store
itself, which costs more.

---

## The log, and the locks it is appended under

**One log per watched Store, shared by every reader of that Component, each
reader with its own place in it**
([Where a Hook log lives](https://github.com/dvoyni/cog/issues/380)). It holds
records of 24 bytes (Entity, kinds, the writer that produced a change, and where
a removal's copy is), and beside them the retained copies of `T` that removals
took.

**Every append happens under a lock the act already holds:**

| act | appends under |
| --- | --- |
| `Set[T].UpdateFor`, `Remove[T].From` | `write{*Store[T]}` |
| a writer's run end (Changed) | `write{*Store[T]}` |
| a Spawn | `write{*Entities}`, once per watched Store it carries |
| a Despawn | `write{*Entities}`, once per watched Store holding the Entity |
| a drain ([#260](https://github.com/dvoyni/cog/issues/260)) | `write{*Entities}` |

**No act reads a Store other than its own, and no lock set grows.** The log needs
no sequence number, because the act's lock already orders it. It is not a global
index: there is one per Store, and it is locked with the Store.

- **A Despawn** calls one capture per watched Store before the Stores are
  emptied. Each capture probes its Store once and records a removal with the last
  value. Despawn's one-method interface to each Store is unchanged, and a watched
  Store enrols its capture beside it.
- **Hook calls stay out of `Store.add` and `Store.remove`,** which remain leaf
  functions. A call inside them measured 0.6–2.8 ns on every structural path,
  watched or not. The calls sit in the accessors, in Spawn, and in Despawn's
  captures.

**A reader's run:**

1. **Start.** Under a mutex shared by the readers of that one Store, the reader
   takes the records since its place, keeps those whose kinds meet `K`, skips
   its own System's Changed records, folds, and moves its place to the end. It
   then releases the mutex and fills values from the live Store under its
   `read{T}`.
2. **The body** iterates `All()`.
3. **End.** Under the same mutex, it compacts what every reader of the Store has
   passed, zeroing retained copies of a non-trivial `T` so a `string` or `List`
   they hold is released, then clears its own copy.

The mutex is held only by readers of the same Store, only to fold and to
compact, and never by a writer or across a System's body.

**Removed values stay alive until the last reader passes them.** A removal's
copy of `T`, and whatever a `string`, `List` or `assets.Blob` in it points to, is
reachable until the run end of the last reader of that Store whose copy included
the record. So `TestARemovedRowDoesNotKeepItsValueAlive` holds for the Store and
not for the log.

---

## A reader keeps pace with its writers

**A System reading `Hooks[T, K]` runs as often as the Systems that write `T`,
usually once per tick. That is part of the contract**
([A Hooks reader that falls behind](https://github.com/dvoyni/cog/issues/381)).

- **What it bounds.** A reader's window holds one tick of acts: at most one
  change record per Entity per writer run, and one record per addition or
  removal made that tick.
- **A System that runs rarely reads a Query instead.**
- **A reader holds its Store's log until it runs.** One rare reader holds the log
  for every reader of that Component. A reader subscribed to an event that is
  never published holds it forever: the set of readers is fixed at registration,
  and nothing at registration can know which events will fire.

**The shared log is not folded.** Every writer's run end appends one change
record per changed Entity, and folding happens only in a reader's copy. Folding
at append would cost a slot per Entity checked against every reader's place.
Under the rule it isn't needed, and it would hide a reader that breaks the rule.

**Additions and removals are never capped.** "Nothing is netted" stands. A cap
followed by a resync was rejected: despawns lost in the gap cannot be recovered
(audio's voices), and every reader would need a recovery path that almost never
runs.

**A stalled reader's log grows by 24 B per record, plus `sizeof(T)` per
removal.** Capacity grows by `append`'s rule.

### Validation mode checks the rule

- **Counted:** each run of a System that appended to a Store's log bumps a
  counter on that log once. A drain counts as one run.
- **Checked:** at a reader's run start, more than **16** counted runs since its
  last run. 16 is a published constant, so a writer that runs twice in a tick to
  catch up a fixed step does not trip it.
- **Result:** a panic naming the reader's System and the Store.
- **The failure it catches:** a reader on the physics step, which stops during a
  pause, while a writer on the frame event keeps going. **The fix it points to:**
  move the reader to the writer's event, or pause the writer too.
- **Release builds pay nothing,** through `const validate`.

### Memory goes back only when the app asks

**Capacity is kept.** The log's arrays and each reader's copy keep their
high-water capacity, so steady state never allocates. **The engine never decides
when memory goes back.** The app knows when a load or a screen change has ended,
and a heuristic cannot. Allocator-style decay, and "shrink below a quarter", were
researched and rejected; see [Shapes that were rejected](#shapes-that-were-rejected).

**`ecs.ShrinkCmd` gives memory back** and is specified in
[`ecs.md` § Giving memory back](ecs.md#giving-memory-back), because an app that
names no Hooks calls it too. For Hooks:

- **`KeepHooks: false`** cuts every Store's log and every reader's copy to its
  length;
- **`KeepScratch: false`** releases writers' row copies for Changed, which the
  next watched run allocates again;
- **it runs under `write{*Entities}` alone**, which excludes every reader and
  every writer, so no log is touched mid-append or mid-fold.

---

## A value that holds a List

**A Hook's value is a read of `T`**
([Validation mode and a retained value holding a List](https://github.com/dvoyni/cog/issues/386)).
A `List` inside `hook.Value` may be read, never written through. **Nothing a
Hook retains changes what is legal for a writer**, so the log is never an owner
and never a reason for a writer's code to panic.

### `List.Set` through `hook.Value` is illegal, for every record

- **An addition or a change** is filled from the live Store, so its `List` shares
  the stored row's array. `Set` would write the Store under `read{T}`.
- **A removal** is folded into every reader's copy, and those copies share one
  array. The readers of `T` all hold `read{T}` and may run concurrently, so a
  `Set` by one races with the others.
- **The check is a new Validation stamp mode, `modeHook`.** When a reader fills
  or folds a record carrying a value, it stamps every `List` in `Value`, walking
  nested Lists as every stamp does. `Set` on a `modeHook` array panics, naming
  `Hooks[T, K]`, the System, and the fix: `Set[T].Ref(e)` on a live Entity, or,
  for a removal, that the Entity no longer holds `T`.
- **`modeHook` is tied to no run**, so `Set` through a kept value panics whenever
  it happens, including past the run end.
- **Writing `Value`'s plain fields, or replacing a whole List header,** changes
  only that reader's own copy. It is legal and reaches nobody else.

| what the code does | validating build |
| --- | --- |
| `Set` on a `List` in `hook.Value`, for an addition, a change or a removal, flat or nested | **panics**, naming `Hooks[T, K]` |
| `Set` on a `List` in a `hook.Value` kept past the run | **panics**, naming `Hooks[T, K]` |
| assigning `hook.Value`'s fields | allowed; it is the reader's copy |

**One stated misattribution and one stated hole**, joining those `validate_on.go`
already lists, for a kept value whose array a writer later reaches:

- **Through a `*T` field**, the stamp becomes `modeWrite` tied to the writer's
  run. A `Set` through the kept copy then panics as "after the run that yielded
  it finished", naming the writer's run rather than the Hook. It still panics,
  because a reader holding `read{T}` never runs inside a writer's run.
- **Through `Set[T].Ref`**, the stamp becomes `modeWrite` with no run, so a `Set`
  through the kept copy is **allowed**, until a reader next stamps that array.
  This is `Ref`'s own gap, not a Hook's: a writer keeping a List it reached
  through `Ref` past its run is not caught either.

**The cost** is one stamp per `List` per delivered record per run, in validating
builds only.

### Reading a retained List

- **Nothing reuses or releases the array before the last reader's run ends.**
  `ListOf` always allocates, and there is no `Append`.
- **A removal's `Value` is `T`'s bytes at the act, but its `List` elements are
  whatever the array holds when the reader runs.** The two differ in one case:
  - a System took the value with `Get`;
  - added it to another Entity with `UpdateFor`;
  - and called `Set` on it there before the reader ran.

  The removal then shows the later elements under a stale header generation.
  Stated, not checked. A reader that needs the elements exactly as they were at
  the act copies them out when it runs.
- **A kept `List`** still shares its array with the Store (an addition or a
  change) or with the log (a removal). Reading it in the reader's own later runs
  is sound, because those runs hold `read{T}`. Reading it anywhere else is a race
  no lock names, and Validation cannot see it, exactly as for a `List` kept from
  a Query read field. To keep the elements, copy them out through `All()` into
  reused scratch, which costs 60 ns and no allocations
  ([`ecs.md` § The List](ecs.md#the-list)).
- **The owner registration ends at the act.** `ecs.md`'s shared-array check marks
  an array shared when it arrives in a second Component while its first owner
  still holds it. The first owner stops holding it at the act that removes its
  row: `Remove[T].From` or a Despawn, under that act's lock. A Hook log is never
  an owner, so a `List` moved from a removed Entity to another is not marked
  shared, whether or not a reader retains the removal.
- **Validation builds keep stamped arrays alive longer.** The stamp table's
  pointer keys keep a removed `List`'s array reachable until the table evicts it,
  past the log's compaction. The retention test uses a `string` for that reason.

### `string` and `assets.Blob`

- **`string`** needs nothing: it is immutable, and its retention until the last
  reader's run end is stated [above](#the-log-and-the-locks-it-is-appended-under).
- **`assets.Blob`** cannot be checked. A removal's `Value` keeps the Blob's bytes alive
  until the last reader of `T` passes the removal. An owner that recycles those
  bytes after a despawn breaks the contract that already says "never", and a
  reader sees the damage as a corrupted removal value.

---

## What it costs

**Every figure here is the prototype's**, from
[`proto/ecs-hooks`](https://github.com/dvoyni/cog/tree/proto/ecs-hooks) on
**AMD Ryzen 9 7950X3D, Go 1.27.1, windows/amd64, `NumCPU=32`**. Three commits:

- [`83b97d2`](https://github.com/dvoyni/cog/commit/83b97d2) is the Query-shaped
  design, measured and rejected;
- [`0460783`](https://github.com/dvoyni/cog/commit/0460783) is the per-Component
  design this document specifies;
- [`5f41e42`](https://github.com/dvoyni/cog/commit/5f41e42) is the `List` header
  A/B.

**How they were taken.** Frames at 1k and 10k Entities, interleaved over five
runs. Each is compared with the same frame holding an empty System in the
reader's place, because the engine charges per subscription. Nothing-watching
figures come from the prototype binary alternated with `main` over six runs.
Single values are ±10%. The package README's [*What a Hook
costs*](../README.md#what-a-hook-costs) carries the same arms re-measured on
the build, and those figures supersede these
([#385](https://github.com/dvoyni/cog/issues/385) §1). The table stays as the
record the build was held to.

| what | result |
| --- | --- |
| **Nothing watching**, against `main` | `UpdateFor` add plus `Remove.From` +0.55 ns (7.55 → 8.1); Spawn with 2 fields +0.8 ns; Despawn +1.65 ns; Query frame unchanged |
| **Recording an add plus remove pair**, per watched Store | +3.7 ns (`HookAddedRemoved`, `HookAll`), +2.8 (`HookRemoved`), +0.8 (`HookDespawned`); +3.9 with a `string` in `T`; 4 readers cost the same as 1 |
| **Recording a Spawn plus Despawn** | +5.5 ns per watched Store it carries; +1 ns per watched Store it does not |
| **Reader, per record** | add and remove pairs 1.9 ns; additions filled live 5.0; changes 5.0 (3.1 per raw change when folded); 4 readers about 3.9× |
| **Changed writer, per row walked, 10k**, 8 B / 64 B rows | unwatched 1.4 / 1.4 ns (2.0 at 100% written). Whole-Store copy: 1.8 / 3.7 at 0% written, 3.2 / 5.3 at 1%, 4.0 / 5.9 at 10%, 5.4 / 6.6 at 100%. Copy per row: 6.4–9.0 / 8.2–10.1 |
| **`Set.Ref` on 1% of a watched Store** | +6.4 ns per `Ref` (1.6 → 8.0) |
| **Frame: `churn` + `move` + `Hooks[collider, HookAddedRemoved]`** | 10k: 45.9 µs against 44.0 µs with an empty System, 1.04×. 1k: 16.1 against 13.7 µs; the reader waits on `churn`, which writes `collider` |
| **Frame: 100 adds or removes a tick, one reader** | +0.6 µs at 1k and 10k |
| **Frame: 100 spawns plus despawns a tick, `Hooks[body, HookSpawnedDespawned]`** | +1.0 µs |
| **Frame: every body changed every tick, `Hooks[body, HookAddedChanged]`** | 10k: 43 → 124 µs, 2.9×, about 8 ns per changed Entity. 1k: 12.9 → 21.5 µs |
| **Allocations, steady state** | a reader adds 0.00 objects a frame against an empty System, at 1k and 10k, for adds and removes, spawns and despawns, and changes |
| **Retention** | a removal's retained `string` survives until the reader's run ends, and is freed then |
| **A stalled reader's log** | 24 B per record, plus `sizeof(T)` per removal |

**The worst case is published, not budgeted.** A Changed reader over every Entity,
every one changing every tick, costs 2.9× the frame at 10k. **Such a reader
should be a Query.**

**Opt-in costs carry no budget:** a reader's work per record, the Changed grid,
the worst case, and `ShrinkCmd`. They are the reader's trade, and this table is
how the trade is stated.

**The `List` header's cost** (24 → 32 B; a Store remove plus add 7.7 → 15.9 ns as
a row crosses 32 → 40 B; a Query read or write over a `List` Component
unchanged) is paid by every `List` user and is published in
[`ecs.md` § The List](ecs.md#the-list).

**Four arms the prototype never had are measured only on the build**
([#385](https://github.com/dvoyni/cog/issues/385) §2), and their figures are in
the README's [*What a Hook costs*](../README.md#what-a-hook-costs): the watch
check at a writer's run start on an unwatched Store (`BenchmarkHookWatchCheck`),
the Validation counter per counted run (`BenchmarkHookPaceCounter`), `ShrinkCmd`
after a spike (`BenchmarkHookShrink`), and a writer's first watched run
(`BenchmarkHookFirstWatchedRun`).

### Budgets, for the costs nobody asked for

**Only costs paid without opting in carry a budget:** code that watches nothing,
and the parallel frame ([#385](https://github.com/dvoyni/cog/issues/385) §6).

| arm | budget | prototype | build, against 50043cd |
| --- | --- | --- | --- |
| nothing watching: `UpdateFor` add plus `Remove.From` | ≤ +1.0 ns | +0.55 | +0.30 (7.69 → 7.99) |
| nothing watching: Spawn with 2 fields | ≤ +1.0 ns | +0.8 | +0.02 (16.68 → 16.70) |
| nothing watching: Despawn with six enrolled Stores | ≤ +2.0 ns | +1.65 | −0.29 (21.02 → 20.73); +1.28 when [#391](https://github.com/dvoyni/cog/issues/391) measured it |
| the watch check at run start, on an unwatched Store | ≤ 1 ns per handle per run | not measured | **missed**: 2.08 per `Set` or `Remove`, 2.75 per Spawn field, 4.1 per `*T` Query field; accepted by the owner |
| Query frame at 1k and 10k, nothing watching | ≤ +3% | unchanged | +1.9% at 1k, +2.2% at 10k |
| `churn` + `move` + reader at 10k | ≤ 1.10× its empty-System control | 1.04× | 1.06× |

**The watch check missed its budget, and the owner accepted the figures**
([#395](https://github.com/dvoyni/cog/issues/395)). Measured with
`BenchmarkHookWatchCheck`, one System run called by hand on Stores nothing
watches, interleaved against 50043cd over seven rounds. The cost is more than one
load and one bit test per handle:

- a `Set` holds two gates, its own and its Store's row copy, and each check
  stores its flag;
- every writer's Store pays a `rowCopy.compare` call at run end, which does not
  inline, even when nothing was copied;
- a `*T` Query field also pays a loop over its row copies in `bind`;
- the System call's loops over Spawns and readers run empty.

Bringing it down toward 1 ns per handle per run is
[ecs: Hooks watch check on unwatched writers down toward 1 ns per handle per
run](https://github.com/dvoyni/cog/issues/403).

- **How a budget is checked:** by hand, in the build, with interleaved A/B runs,
  five or more. The reference is **the parent of the first Hooks commit**, not a
  build tag that compiles Hooks out, and the README names that commit.
- **Why +3%:** `ecs.md` already rejected an `or` that cost 3% of a 10k frame, so
  the figure is detectable and one the owner has acted on.
- **When a budget is missed, the build stops and goes to the owner before
  changing anything.** No optimisation, no redesign and no revised number is
  published until the owner has ruled.

**Timings are published and never tested.** A test asserts only what can be
counted exactly: allocations, lock sets, occupancy, record counts and sizes.

**Escape analysis.** `-gcflags=-m`, in both build modes, shows no `moved to
heap` on the log append in the accessors, Spawn and Despawn; on a reader's fold
and fill; on the whole-Store and per-row copies; on the compare at run end; on
`MarkChanged`; or on `ShrinkCmd`'s execution. The package's one `moved to heap`
is `entities` in the `ShrinkCmd` factory, one allocation when the plugin
registers the Command. The README states it, as it does for the Query and Spawn,
and nothing tests it.

---

## Shapes that were rejected

Recorded so they are not reinvented, each with the reason that actually killed
it.

**A callback at the act: `OnAdd[T]`/`OnRemove[T]`, observers, signals.** It fires
inside the structural change, under that act's lock and nothing else, so it can
reach the world only by re-entering it mid-mutation. That was `ecs.md`'s reason
for ruling hooks out, and it still holds. It is why a Hook is a record that an
ordinary System reads later, under its own declared locks.

**A cog event published after each act.** Settled while charting the map. The
brief left open who would publish it, when, and under what locks, since a System
may be driven by any event. A System parameter answers all three with what the
kernel already does: the reader is scheduled like any System, and its declared
reads are its locks. *Event* stays the kernel's word, for something published
rather than read.

**A Hook shaped like a Query: `Hooks[Q]`, with Entered and Exited.** Built and
measured ([`83b97d2`](https://github.com/dvoyni/cog/commit/83b97d2)). For a
multi-Store `Q`, an act on one of `Q`'s Stores had to read the others: to probe
membership, to copy an exit's values, and to order acts on two of `Q`'s Stores
against each other. So `Set[T]` and `Remove[T]` had to declare reads they never
needed before. **Two Systems that ran in parallel in 44.0 µs took 71.5 µs** at
10k Entities once a reader of `Q{body; collider}` serialised `churn` against
`move`, and 13.8 → 22.8 µs at 1k. The owner's ruling that no Hook may cost
System parallelism ends every design in which an act reads a Store other than
its own. Entered and Exited left with it: a Hook is about one Component, never
about a Query's match.

**Recording membership lazily**, as the Query-shaped fallback. Each act records
(Entity, Store, gained or lost, the removed row) under its own lock, and the
reader works out membership at its run start. Rejected because an exit's other
fields would come from the reader's run start, not from the act. The remove key
goes wrong once another of `Q`'s fields is written between the exit and the
reader's run.

**Filters in a Hook.** Checked at the act, a filter is a widened lock. Checked at
delivery, it loses exactly what an index needs. See [No filters](#no-filters).

**Undeclared kinds reading false**, [#379](https://github.com/dvoyni/cog/issues/379) §3's
rule. It was replaced when kinds became relative to one Component. With kind
sets, reporting every true fact costs nothing to record, and panicking only on a
kind the set can never deliver still catches the index that never removes.

**Write access counting as change** (Bevy, Unity). Every row a `*T` Query field
yields would read as changed, and Unity's own transform system shipped that
over-report.

**Explicit calls as the definition of change** (EnTT `patch`, flecs `modified`,
ark `Map.Set`). A write through a `*T` field would be silently missed. As a
fallback, with `UpdateFor` and `List.Set` recording at the call and each `List`
array holding its owner, it was measured beside the byte compare. It is free up
to 10% of rows written and costs 4.5 / 4.9 ns per row walked at 100% (8 B / 64 B
rows). It would save the copies, 0.4–3.4 ns per row, but not the reader's work
per changed Entity, which dominates. Not taken.

**`Added[T]` and `Changed[T]` as Query fields over per-Store versions.** This
was the shape `ecs.md` had called settled. Versions count write access as change,
and a Query-shaped reader widens locks, which costs parallelism. The whole
earlier design of [#268](https://github.com/dvoyni/cog/issues/268) went with it:
per-Store versions, tick arrays, per-Query last-seen versions and the wrap rule.
`Chunk` is not spent: with no change version there is nothing for it to group.

**A per-field `Tracked[T]` marker.** A reader that cares about one Component reads
the Hooks of that Component.

**Hook calls inside `Store.add` and `Store.remove`.** 0.6–2.8 ns on every
structural path, watched or not, because the functions stop being leaves.

**Opting in at `RegisterComponent[T]`.** The consumers read Components other
plugins own, so opt-in would make the owner change for every new reader, which
couples in the wrong direction.

**Letting an owner refuse Changed.** The reader would diff the Store itself,
which costs more.

**A planning pass in the ecs plugin's `Start`.** The scheduler starts before any
plugin's `Start`, and an earlier plugin can publish from its own, so Systems may
already be running.

**Requiring readers to register before writers.** Dependency order makes that
impossible across plugins.

**Folding change records in the shared log.** It costs a slot per Entity checked
against every reader's place. Under [the pace rule](#a-reader-keeps-pace-with-its-writers)
it isn't needed, and it would hide a reader that breaks the rule.

**Capping additions and removals, then resyncing.** A despawn lost in the gap
cannot be recovered, and every reader would need a recovery path that almost
never runs.

**Shrinking on a heuristic** ([#381](https://github.com/dvoyni/cog/issues/381)).
General containers never shrink on their own: Go slices and maps, Rust `Vec`,
Java `ArrayDeque`, Bevy's message buffers, and flecs outside a manual
`ecs_shrink`. Allocators shrink against a decaying peak (jemalloc's 10 s decay,
tcmalloc, PostgreSQL's background writer). For a spike every *k* frames:

- "shrink when the average is under a quarter of capacity" shrinks between every
  pair of spikes and grows back;
- the classic halve-at-a-quarter rule allocates about 2(*k*−1) times per spike;
- a decaying peak allocates nothing while the period stays under its horizon.

None of these knows what the app knows, which is when a load or a screen change
has ended. So memory goes back through a command.

**Recording a deferred change at the call.** It needs a lock on the log the
deferring handle does not hold: either the wide lock deferral exists to remove,
or contention between deferring Systems on a log lock. It would also record acts
that haven't happened, such as a spawn with no Entity or a despawn whose Entity
is still iterated.

**Ordering Hooks readers after the drain.** It needs a kernel affordance, and
kernel changes are out of scope.

**A Validation panic on `Set` of an array a log still retains**, and **ending the
owner registration when the log compacts.** Either way, a legal move of a `List`
to another Entity would become illegal only when some plugin registers a reader,
so a writer's legality would depend on who else is listening.

**Deep-copying `List` arrays at removal** on a watched Store. An allocation on
every structural path.

**Probing the previous owner's row at a `List`'s second arrival.** It reads
another Store without that Store's lock.

**Reusing `modeRead` for Hook values.** Its message says "name the Component as
`*T`", which is the wrong fix for a reader.

**Relying on the shared-array mark to catch `Set` through a Hook value.** It sees
only arrays compared at a writer's run end.

---

## Required work

The checklist the implementation is built from. The build is sliced into
tickets under [the map](https://github.com/dvoyni/cog/issues/377), which is their
umbrella. The items `ecs.md` lists under *Since Hooks* in its own Required work
are those that change code naming no Hooks, and they are repeated here only by
reference.

**`ecs.md`'s surface** — see [`ecs.md` § Required work](ecs.md#required-work):
`List.Set`'s pointer receiver and generation, `Set.Of` no longer stamping a write,
the shared-array mark, `ShrinkCmd` with the generation floor, and the signature
row.

**`ecs` package — Hooks**

- `Hooks[T, K]`, `Hook[T]` with `Value` and the five `IsX()` methods placed after
  the kinds field, the eight kind-set types, and the unexported constraint.
- The parameter classification: `read{*Entities}` + `read{*Store[T]}`, and a
  composition failure for an unregistered `T`, including `*T`.
- A Store's watched kinds, as the union of its readers' kind sets while they
  register, fixed when registration closes.
- The log per Store: 24-byte records, retained copies of `T`, readers' places,
  compaction that zeroes non-trivial copies.
- Recording per [the acts table](#which-acts-are-recorded): in `UpdateFor` and
  `Remove.From`, in Spawn once per watched Store carried, and in Despawn through
  one capture per watched Store, with the unwatched Despawn path unchanged.
  Nothing inside `Store.add` or `Store.remove`.
- The watch check at each writer's run start, for a `*T` Query field, `Set[T]`,
  `Remove[T]` and `Spawn[S]`.
- Changed: the whole-Store copy when a `*T` Query binds, the per-row copy for
  `Ref` and `UpdateFor`, the compare and append at run end, the writer identity
  that keeps a System from seeing its own Changed, and row-copy buffers kept
  across runs.
- A reader's run start (take, filter, skip its own Changed, fold, fill) and run
  end (compact, clear), under a mutex per Store shared only by its readers.
- Validation mode:
  - the counter per Store and the panic past 16 counted runs;
  - `IsX()` panicking on a kind `K` can never deliver;
  - the `modeHook` stamp at fill and fold;
  - the implicit-padding check when a reader watching Changed registers;
  - the owner registration ending at the removing act.

**Documentation**

- `bundles/ecs/docs/README.md` gains *What a Hook costs* under *What it costs*, with
  the arms in [What it costs](#what-it-costs) re-measured on the build, and names
  the reference commit.
- The README's "no compaction, no shrink and no sweep" and "Nothing shrinks"
  lines change with the `ShrinkCmd` build.

**Behaviour tests, on a System in a real `kernel.Engine`**

The rules in this document are the list. At least:

- each kind set delivers exactly the records [the table](#the-eight-kind-sets)
  says, and no other, for a Spawn, an `UpdateFor` add, a replacing write that
  changes bytes, one that does not, a `Remove.From`, and a Despawn;
- a record reports every true kind (`IsSpawned()` under `HookAdded`, and so on);
- the window example is delivered as [stated](#values-and-a-window);
- an addition's value is `T` at the next removal, even under `HookAdded` with no
  reader of removals;
- a System never sees its own Changed, but sees its own additions, removals,
  spawns and despawns in its next run;
- a change followed by a removal in the same run records only the removal;
- a despawned Entity and the one reusing its index are never folded together;
- a Tag never records Changed;
- records are fixed at run start: a despawn during the run changes no record
  already taken;
- **registration order:** the owner's writer registers before a dependent
  plugin's `HookAddedChanged` reader, and the reader still gets exactly one
  Changed per written Entity per tick;
- **the Validation trip:** a reader panics at 17 counted runs since its last run,
  not at 16, naming the System and the Store, and a release build doesn't panic
  at 1 000;
- **`IsX()` on an impossible kind** panics in a validating build;
- **`Set` through `hook.Value`** panics for an addition, a change and a removal,
  flat and nested, and again through a value kept into a later run;
- **a `List` moved** from a removed Entity to another is not marked shared while a
  reader retains the removal;
- **the padding [rule](#changed-is-a-difference-in-bytes)**: explicit `_ [N]byte`
  padding records no Changed for equal fields on every write route, and a
  Changed reader of a Component with implicit padding panics at registration in
  a validating build.

**The parallelism guard**

- `TestAHookNeverWidensALockSet`: for each of the eight kind sets, registering a
  `Hooks[T, K]` reader leaves every other handler's Reads and Writes in
  `Describe()` identical to the same world without it, and the reader declares
  exactly `read{*Store[T]}` and `read{*Entities}`.
- Occupancy: `churn`, writing `collider`, and `move`, writing `body`, still reach
  occupancy 2 with a `Hooks[collider, HookAddedRemoved]` reader registered. The
  control is the pair with a spawn, which reaches 1.

**Counts**

- **Allocation lines**, in the shape of `TestTheFrameSitsOnTheEnginesAllocationLine`
  (100 warm-up frames, a steady state of at least 5 000 frames, at 1k and 10k):
  10k ≤ 1k + 0.05 objects a frame, and ≤ control + 0.05, where the control has
  an empty System in the reader's place. Arms:
  - `churn` + reader;
  - `churn` + `move` + reader;
  - spawn and despawn with `HookSpawnedDespawned`;
  - `move` with a `HookAddedChanged` reader;
  - `HookAll` with 4 readers of one Store, against 4 empty Systems.
- **Accessors:** `testing.AllocsPerRun` is 0 for `UpdateFor`, `From`, `Ref`,
  Spawn and Despawn on a watched Store after its first watched run, and the first
  watched run allocates.
- **Record size:** `unsafe.Sizeof` pins a record at 24 B. A stalled reader's log
  after *k* additions and *r* removals holds exactly *k + r* records and *r*
  retained values.
- **Retention:** a removal's retained `string`, with a finaliser, is not freed
  before the last reader's run end, and is freed within 2 GCs after it, in both
  build modes.

**Benchmarks**

- In `bundles/ecs/internal/types/hooksbench_test.go`, beside `framebench_test.go`
  and `spawnbench_test.go`, named `BenchmarkHook*`, with frame arms as
  sub-benchmarks at 1k and 10k. Every arm in [What it costs](#what-it-costs),
  plus the four not yet measured. The prototype's `hooks_protobench_test.go` is a
  reference for the arms, not code to merge.
- Budgets checked by hand with interleaved A/B runs against the parent of the
  first Hooks commit, and a missed budget taken to the owner before anything
  changes.
- `-gcflags=-m` on the paths listed under escape analysis.

**Held for [#260](https://github.com/dvoyni/cog/issues/260)'s build**, not this
one: a reader on the deferring System's event sees a drained spawn and despawn in
its next run and not before the drain; a drained despawn of a dead Entity records
nothing; a drained despawn carries the value as it stood at the drain.

---

## Out of scope

Recorded on [the map](https://github.com/dvoyni/cog/issues/377) and repeated so a
reader of this document alone does not re-propose them.

- **Per-entity side data**, [#264](https://github.com/dvoyni/cog/issues/264)'s
  other half. No consumer asks for it, and #237's answers cover every current
  case. It returns with a consumer.
- **Designing the deferred structural-change buffer** of
  [#260](https://github.com/dvoyni/cog/issues/260). This document fixes only how
  a drained change reaches a Hook.
- **Kernel changes**, including [#370](https://github.com/dvoyni/cog/issues/370)'s
  enforcement of non-re-entrancy. [#282](https://github.com/dvoyni/cog/issues/282)'s
  "a System is not re-entrant" already covers a reader's state.
- **The consumers' own designs:** how physics builds its index
  ([#288](https://github.com/dvoyni/cog/issues/288)), and what a voice does on
  despawn ([#303](https://github.com/dvoyni/cog/issues/303)).
