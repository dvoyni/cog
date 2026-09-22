# cog ecs — Deferred Spawn and Despawn

A System that spawns or despawns holds `write{*Entities}` — the lock over every
Store at once — from its first instruction to its last, however little of its
run the change takes. This document specifies **two handles that queue that
change instead of making it**, and a **Drain** that applies what is queued under
the wide lock and nothing else. It covers what a System writes to defer, what a
deferred Spawn hands back before the change exists, when a queued change becomes
visible, where the drain runs and in what order it applies, what a deferring
System declares, and where the queue lives.

It is a companion to [`ecs.md`](ecs.md) and is bound by the same four
requirements, in the same order: zero heap allocation on the hot path,
concurrency from cog's existing scheduler, extensible along named axes, and
reasonably simple to use. It inherits [`hooks.md`](hooks.md)'s added
requirement unchanged, and the whole of this design was held to it:

> **The ECS must never cost System parallelism.** An option that widens a lock
> set, or serialises Systems that run in parallel today, is rejected rather than
> traded.

That requirement is why this document exists at all: the change is never the
cost. `BenchmarkSpawnFourFields` is 27.6 ns and `BenchmarkDespawnOnly` 18.5 ns
across six Stores, while *declaring* `write{*Entities}` costs about 8.2 µs a
frame. Deferral buys lock duration, and buys nothing else.

This document is assembled from the resolved tickets of
[ecs: defer Spawn and Despawn through a typed buffer, to shorten the wide
lock](https://github.com/dvoyni/cog/issues/260), and every section cites the
tickets it came from. It follows `ecs.md`'s conventions: where a claim rests on
something unverified it is marked **Gap** and says what would settle it, and
where assembling the decisions side by side settled something no ticket did, it
is marked **Settled here**.

**This document is built.** `github.com/dvoyni/cog/bundles/ecs` implements it,
and what structural change costs on the build is measured in the package
README's [*What structural change
costs*](../README.md#what-structural-change-costs). [What it
costs](#what-it-costs) keeps the prototype figures the design was held to, and
the README's supersede them. [Required work](#required-work) is the checklist
the implementation was built from, and the rules in this document are also the
list of behaviour tests. Where the package and this document disagree, the
package is the defect unless this document says otherwise.

`ecs.md` and `hooks.md` changed when this document landed, and those sections
point here: `ecs.md` §*[The shape](ecs.md#the-shape)*, §*[What a signature may
contain](ecs.md#what-a-signature-may-contain)*, §*[Giving memory
back](ecs.md#giving-memory-back)*, §*[What a System
sees](ecs.md#what-a-system-sees)* and §*[There is no command
buffer](ecs.md#there-is-no-command-buffer-and-the-reason-is-allocation)*, and
`hooks.md` §*[A drained change](hooks.md#a-drained-change)*.

---

## Contents

- [Vocabulary](#vocabulary) · [Who asked](#who-asked)
- [The two handles](#the-two-handles)
- [Queuing is not a Structural change](#queuing-is-not-a-structural-change)
- [The reserved Entity](#the-reserved-entity)
- [The drain](#the-drain)
- [Ownership without the write](#ownership-without-the-write)
- [Where the queue lives](#where-the-queue-lives)
- [Hooks](#hooks)
- [What it costs](#what-it-costs)
- [What is not foreclosed](#what-is-not-foreclosed)
- [Shapes that were rejected](#shapes-that-were-rejected)
- [Required work](#required-work) · [Out of scope](#out-of-scope)

---

## Vocabulary

**Deferred Spawn**, **Deferred Despawn**, **Drain** and **Reserved Entity** are
in `CONTEXT.md`, the glossary of record. The words below name parts of the
mechanism; they are this document's, and nothing outside `bundles/ecs` needs
them.

- **Queue (verb)** — what a deferring handle's method does: append to that
  handle's own buffer. Nothing else happens at the call.
- **Queued** — what a buffer holds. What the *handles* are is **deferred**. The
  two words do not swap: there is no "deferred queue" and no "queued handle".
- **The drain** — one execution of `WriteableEntities.Drain()`, whichever System
  called it.
- **A drain System** — any System that calls `Drain()`. An app writes as many as
  it needs; the ECS subscribes one itself.
- **The spawn pass**, **the despawn pass** — the two walks one drain makes over
  the enrolled handles.
- **The reservation cursor** — the one atomic word a deferred `New` takes its
  index through.
- **The released list** — indices freed by an *immediate* Despawn while
  reservations are outstanding, held back from the free list until the drain.

---

## Who asked

Nobody yet, and that is recorded rather than glossed. This is **built as engine
capability** (owner, 2026-09-22), not against a consumer: the parked body's
consumer gate was dropped. nox is not a consumer — its system spec keeps
structural change in small Systems and tiers this *"worth watching, not asking
for"* (`nox/docs/specs/cog-gaps.md:381`). The nearest candidate is a Lifetime
System over particle Entities, if nox makes particles Entities.

What asks is the shape of the lock, and it is not hypothetical:

- **The lock set is static.** `systemCall.lock` runs once at registration and
  folds every parameter's `prepare`. A handle in the signature therefore holds
  its lock for the System's **entire run** — a System that queries 5 000
  Entities and spawns one projectile excludes every other System in the frame
  for all 5 000.
- **The change itself is free.** Spawning four fields is 27.6 ns; despawning
  across six Stores is 18.5 ns, about 3 ns a Store. Declaring the lock is
  ~8.2 µs, the difference between `BenchmarkBarrierDeclared` and
  `BenchmarkBarrierReading`.
- **v1's answer was to split the System.** `ecs.md` tells an author to move a
  rarely-spawning body into its own small System. That answer stands and stays
  the cheapest one; deferral is what to reach for when splitting is not
  available, not a replacement for it.

**Gap.** The crossover — where deferral starts to pay — is around **89 despawns
a tick**, from [#240](https://github.com/dvoyni/cog/issues/240)'s synthetic
workload on a prototype, not from this design on a real engine and not from any
game. The crossover benchmark in [Required work](#required-work) is what settles
it, and until it runs, *"deferral pays above ~89"* is an estimate carried
forward, not a measurement of what is built.

---

## The two handles

A System defers by naming a different parameter. There is no mode, no flag and
no constructor.

| handle | method | declares |
| --- | --- | --- |
| `ecs.DeferredSpawn[S]` | `.New(S) Entity` | `read{*Entities}`, plus `read{*Store[F]}` per Component set field |
| `ecs.DeferredDespawn` | `.Despawn(Entity)` | `read{*Entities}` |

Set beside [`ecs.md` §*The shape*](ecs.md#the-shape), the whole of the change is
that four `write`s became `read`s:

```go
// immediate — holds write{*Entities} for the System's whole run
func Cast(spawn *ecs.Spawn[Projectile], q *ecs.Query[Casters]) { … }

// deferred — holds read{*Entities} and read{*Store[F]}; the body is identical
func Cast(spawn *ecs.DeferredSpawn[Projectile], q *ecs.Query[Casters]) { … }
```

**Migrating a System is one token in its signature and no call-site change.**
That is what the names buy, and it is why the methods are `New` and `Despawn`
rather than a `Queue` on each
([the deferring handles, their names and
methods](https://github.com/dvoyni/cog/issues/557)). It is also what
[what a deferring Spawn declares for
ownership](https://github.com/dvoyni/cog/issues/556) protected when it refused
to make an immediate and a deferring spawn in one System an error: a System is
migrated one handle at a time.

**Two types rather than a deferring mode, because the lock set is folded once.**
`systemCall.lock` calls each parameter's `prepare` exactly once at registration,
and `prepare` is where `GetRead[*Entities]` or `GetWrite[*Entities]` is spoken.
A mode chosen at any later moment cannot change a lock already declared, and a
mode chosen *at* registration is a second type spelled worse. The glossary is
the test: a System *"can touch nothing that signature does not name"*, and
`WriteableEntities` exists precisely so the authority is visible in the
signature and nowhere else.

**And a new type is free.** `newSystemParam` is one `reflect.New` plus one
assertion against the unexported `systemParam` interface, so a type in
`internal/types` that implements `prepare` becomes a legal System parameter with
no edit to the builder. The classification is a closed set no package outside
`internal/types` can extend, and it needs no edit either. What does need editing
is prose: three hand-maintained lists name the legal parameters, and all three
gain the two names — the registration-failure sentence in `systemcall.go`, the
guidance comment in the root's `utils.go` (already stale: it omits `*Hooks` and
`*Resp`), and `ecs.md`'s two handle tables.

### Two handles rather than one

`DeferredDespawn` is generic in nothing — the second such parameter type after
`WriteableEntities` — for the reason `ecs.md` §*The shape* already gives:
folding Despawn onto a spawn handle would force a Component set type on Systems
that never spawn. One combined handle would also carry both buffers and enrol in
both registries where it needs one
([where the typed buffer lives](https://github.com/dvoyni/cog/issues/555) §6).

### One exported method each, and nothing else

No `Len()`, no `Pending()`, no `Clear()`, no `Drain()`.

- `Len` and `Pending` are refused by [when a deferred Spawn or Despawn becomes
  visible](https://github.com/dvoyni/cog/issues/551): **nothing reports what is
  queued**, and a handle reporting on itself is the same hole by another door.
- `Clear` would be a second release path beside
  [`ecs.ShrinkCmd`](ecs.md#giving-memory-back), which owns capacity.
- `Drain` stays solely on `WriteableEntities`, so a System that queues *and*
  drains names both handles and holds `write{*Entities}` for its whole run —
  which is honest, because that is what draining costs.

**`Despawn` returns nothing, and that is the house rule applied rather than an
exception to it.** Every ECS operation that can *miss* returns `bool` —
`WriteableEntities.Despawn`, `Remove[T].From`, `Get[T].Of`, `Set[T].Ref`; one
that cannot returns nothing, as `Set[T].UpdateFor` does. A queued Despawn cannot
miss at the call, because [the drain decides](#a-queued-despawn-whose-entity-is-not-alive-does-nothing).

Both handles are taken as pointers, like every other: `*ecs.DeferredSpawn[S]`,
`*ecs.DeferredDespawn`.

### `New` hands back an Entity that is not alive

**This is the price of the naming, and it is stated as a rule rather than a
remark.** `New` returns an `Entity` from both `Spawn[S]` and `DeferredSpawn[S]`,
and only the deferring one's is a [Reserved Entity](#the-reserved-entity). The
type in the signature is the only thing that says so, where a `Queue` method
would have said it at every call site.

So, plainly: **the Entity `DeferredSpawn[S].New` returns is not alive until the
drain.** `Alive` is false for it. An immediate `WriteableEntities.Despawn`,
`Set[T].UpdateFor` or `Remove[T].From` on it *misses*, exactly as on any Entity
that is not alive. Storing it as a Reference in a Component is precisely what it
is for.

### Immediate and deferring handles in one System

Legal, silent, and checked nowhere. Four cases, all of them allowed:

- **`*Spawn[S]` beside `*DeferredSpawn[S]`** — the write subsumes the read.
  `GetWrite` deletes the type from the read set and `GetRead` skips a type
  already in the write set, so the two maps are disjoint by construction and
  the order the handles prepare in does not matter.
- **`*WriteableEntities` beside `*DeferredDespawn`** — the write subsumes the
  read, the System holds the barrier for its whole run, the queue buys it
  nothing, and its queued despawns land a drain later than its immediate ones.
- **Two `*DeferredSpawn[S]` of the same Component set** — two independent
  buffers, drained in enrolment order.
- **`*DeferredSpawn[S]` beside `*DeferredDespawn`** — the ordinary case: a
  System that both spawns and despawns, deferring both.

*No registration panic on any of them.* It would be the ECS telling an author
their System is pointless rather than wrong, and the second case is exactly what
a System looks like half-way through a migration. The guidance — **a System
holding `write{*Entities}` gains nothing from a deferring handle** — is in this
document, not in a check.

---

## Queuing is not a Structural change

The whole visibility rule is one sentence:
[**queuing is not a Structural change; the drain
is**](https://github.com/dvoyni/cog/issues/551).

A deferred Spawn or Despawn changes nothing anyone can see until a System calls
`WriteableEntities.Drain()`. That System makes every change it applies, under
its own `write{*Entities}`, exactly as if it had called the immediate handles.

**No new promise is made.** [`ecs.md` §*What a System
sees*](ecs.md#what-a-system-sees) — *"a System sees every structural change made
by Systems that ran before it and none from those that ran after"* — and
[`hooks.md` §*When a System sees a record*](hooks.md#when-a-system-sees-a-record)
stand word for word. This document adds one sentence to them, and it is this:
**the System that made the change is the drain System.** That is the whole
amendment. Before a drain, a deferred-despawned Entity is alive — `Alive` true,
every Accessor reaches it, every Query iterates it — and a deferred-spawned
Entity does not exist. After it, every System ordered after that drain System
sees the change, on whatever event the drain ran.

An app drain System running mid-publication therefore makes the change visible
**within that publication**, to whatever is ordered after it. Nothing
special-cases it; it falls straight out of the promise above.

### The queuer sees nothing of its own queue

Having queued a Despawn of X, a System still reaches X and still iterates it for
the rest of its run. A Spawn it queued does not exist for it.

**There is no per-run filter on Queries.** A System that must skip X for the
rest of its run keeps its own local note. A filter would mean a Query consulting
a per-System set on every row, which is a read on the hot path that exists for
the minority of Systems that defer — and `ecs.md`'s standing corollary applies:
**any global index is a global lock.**

**A consequence worth stating.** Because queuing changes nothing, a System may
queue a Despawn of an Entity **other than the one it is visiting** while
iterating — which is undefined for an immediate Despawn (`CONTEXT.md`
§*Structural change*). That is a real gain, not a footnote: it is the case
deferral makes safe. `Drain()` called inside an iteration falls under the
ordinary rule and is no safer than an immediate Despawn there.

### Nothing reports what is queued

No `IsPending`, no drain report, no count, and nothing on the handles. The MCP
read Commands hold `write{*Entities}` and therefore see drained state like any
Query; they report nothing about queues either
([`mcp.md`](mcp.md)).

**A game that must mark an Entity doomed before the drain adds a Tag** through
`Set[T].UpdateFor`, which is immediate and holds only the narrow
`write{*Store[T]}`. That is the supported answer, and it costs the app one Tag.

**Not a `Dead` Tag in the ECS.** A Filter contributes a read, so every Query
would hold `read{Dead}` and conflict with every despawner's `write{Dead}` —
serialising exactly what the buffer set out to unserialise.

---

## The reserved Entity

`DeferredSpawn[S].New` **returns a reserved Entity**: its id and generation are
fixed at the call, it has no Components until the drain, and the spawner can
link what it spawned — a missile's `target`, a corpse's `of`, a projectile's
`owner` — in the same run
([what a deferred Spawn hands back before the
drain](https://github.com/dvoyni/cog/issues/553)).

*Returning nothing was rejected.* Without an Entity at the call, the deferring
handle could not serve the case the immediate one serves today, and every
spawner that links would have to be split into two Systems — which is the very
split deferral exists to avoid.

### Reservation holds only `read{*Entities}`

While any System runs, nothing writes the free list or `gens`, because every
System touching a Store holds `read{*Entities}` and a writer is excluded against
all of them. So a reservation can take the next index from the free list without
the write lock:

- one shared **atomic cursor** over the free list hands out the next index;
- when the free list is exhausted, indices come from past `len(gens)` at a
  floor, the shape Bevy uses.

The cost is **one atomic add per deferred `New`**. Parallel deferring Systems
contend on that one word; no System is serialised, and no lock set is widened.
**This is the only atomic in the whole design.**

*Rejected:* a block of ids handed to each handle at each drain. It needs a
fallback for when a block runs out, and it adds a knob — a block size — that
nobody can set correctly.

### `Alive` is false for a reservation until the drain

Required by [queuing is not a Structural
change](#queuing-is-not-a-structural-change): if `Alive` were true, a
deferred-spawned Entity would exist before the drain.

It stays **one compare**. A freed index carries a generation no issued handle
ever holds — a free bit set at Despawn, cleared when the index goes live — so
the reservation's generation simply does not match until the drain clears the
bit.

This also closes a blind spot the ECS has today: a fabricated handle naming a
free index's *next* generation currently answers `Alive` true (`entities.go`,
`Alive`'s comment). With a free bit it cannot.

**It costs one bit of generation**: about 2×10⁹ reuses of a slot rather than
4×10⁹. `ecs.md`'s figure updates.

*Rejected:* `Alive` consulting a reservation list or any second liveness
structure — it would put a second lookup on the hot path — and accepting `Alive`
true for a recycled reservation, which contradicts the visibility rule outright.

### What a reserved Entity can do before its drain

- **Be stored as a Reference** in any Component, whether written immediately or
  queued through another deferred `New`. The Reference resolves to nothing until
  the drain, as a Reference to any non-alive Entity does.
- **Be despawned, deferred.** A queued Despawn of a reserved Entity works
  whenever it is queued, because [the drain applies every queued Spawn before
  any queued Despawn](#one-drain-is-two-passes). The Entity is spawned and then
  despawned, recording both Hooks.
- **Not be touched immediately.** An immediate `Despawn`, `Set[T].UpdateFor` or
  `Remove[T].From` **misses**, as on any Entity that is not alive — `set.go`
  already checks `Alive`. It comes to life at the drain with exactly the
  Component set it was queued with, and nothing else.

### Immediate handles, and `ShrinkCmd`, while reservations are outstanding

- **An immediate `Spawn[S].New` takes its index through the same cursor** — no
  atomic needed, since it holds the write lock — and goes live at once. It never
  hands out a reserved index. The drain cuts the free list down to the cursor.
- **An immediate `Despawn` puts its index on the released list**, not the free
  list, while any reservation is outstanding. The drain moves released onto free
  after cutting, so the index is recycled from the *next* drain — at most one
  frame later, since the ECS drains every Update. Appending to the free list
  directly would put the index inside the part already handed out, or would
  renumber fresh reservations.
- **`ShrinkCmd` keeps its shape and its response.** While any reservation is
  outstanding its index step drops no indices and reports 0 bytes for Entities.
  Stores, scratch and Hook logs shrink as today.

*Rejected:* replacing `ShrinkCmd`'s index step with a deferred
`WriteableEntities.Shrink()`. It could not return a `ShrinkResponse`, and it
would move the release rule off a Command — against the standing rule that
[memory release is an app-called command, never an engine
heuristic](ecs.md#giving-memory-back).

---

## The drain

### `Drain()` is a call the app makes, not a point the engine picks

`WriteableEntities` gains one method:

```go
func Drain(entities *ecs.WriteableEntities) { entities.Drain() }
```

An app writes as many drain Systems as it needs and schedules each where it
belongs ([WriteableEntities.Drain(), app drain Systems and the general Last()
drainer](https://github.com/dvoyni/cog/issues/552)).

It is sound for free: every System declares `read{*Entities}` unconditionally,
so a System calling `Drain()` under `write{*Entities}` can never overlap a
deferring System writing its buffer. The kernel is the exclusion.

**One `Drain()` drains everything queued in the Engine**, whatever System and
whatever event queued it — a Despawn queued by an `ecs.Feed` System is applied
by the next `Drain()` on any event. Nothing else drains: not `ShrinkCmd`, not
the MCP read Commands, though both hold `write{*Entities}`. One straight-line
drainer, no per-event bookkeeping.

That is affordable because [`write{*Entities}` is one entry, not
N](ecs.md#writeentities-is-one-entry-not-n): one lock-set entry reaching every
Store, so the drainer needs no loop in its `Lock` and no kernel affordance.

### `ecs.DrainOnUpdate`

**The ECS subscribes the general drainer itself**, on `app.UpdateEvent`,
`.Last()`, under an exported subscription identity `ecs.DrainOnUpdate` — the
`anim.AdvanceOnUpdate` / `ecsaudio.RecordOnUpdate` shape.

**Always.** There is no opt-out: the `ecs.Config` flag proposed while resolving
that ticket was
[withdrawn](https://github.com/dvoyni/cog/issues/552#issuecomment-5780706841),
because a queue that is never drained is not a configuration, and because
[a reservation must settle](#a-reservation-lasts-until-the-next-drain).

This is the ECS's **first production dependency on the `app` slot** and its
**first System of its own**. It stays zero-dependency as a plugin —
`Dependencies()` is nil, as anim's is.

**Settled here.** A System subscribed to an event other than `app.UpdateEvent`
queues into the same buffers, and its queue is applied by the next
`ecs.DrainOnUpdate` or by an earlier app drain System — which may be a later
frame relative to that event's own publication, and may fold several
publications of that event into one drain. Nothing is lost or reordered: the
buffer keeps queue order across publications, because
[`resolve()` never empties it](#resolve-never-touches-the-buffer). An app that
needs its own event drained on its own event writes a drain System on it.

### Ordering inside `Last()`

`Last()` subscribers have **no order among themselves**. So a `Last()`
subscriber that cares orders itself against the drainer **by name**:

- `.Last().Before[ecs.DrainOnUpdate]()` — to have its queue drained this Update;
- `.Last().After[ecs.DrainOnUpdate]()` — to see this Update's drain.

The kernel supports it: an explicit edge between two `Last` nodes is added like
any other. **Unordered, which side it lands on is unspecified.** That covers a
deferring System that is itself `Last()`, a `Last()` Hooks reader, and a render
or audio flush.

**A change drained after a renderer's `Last()` snapshot may be drawn from the
next frame**, since a bundle's `Last()` subscription is ordered by that bundle,
not by the app. A game that needs it this frame drains earlier, with its own
drain System. This is not a defect; it is the reason app drain Systems exist.

### One drain is two passes

One `Drain()` is **two passes over every enrolled handle, with the free list
settling between them**
([the order and conflicts of one drain](https://github.com/dvoyni/cog/issues/554)):

1. **The spawn pass.** Handles in the order they enrolled at registration, each
   buffer in the order changes were queued. Each queued Spawn brings its
   reserved Entity to life — growing `gens` where the reservation went past the
   end, clearing the free bit — and writes its Components, recording what an
   immediate Spawn records.
2. **The free list settles.** The free list is cut back to where the reservation
   cursor left it, the cursor is reset, and the released list is moved onto the
   free list. From here the free list is ordinary again.
3. **The despawn pass.** Handles in enrolment order, each buffer in queue order.
   Each queued Despawn does exactly what `WriteableEntities.Despawn` does today:
   every capture, then every Store's remove, then the generation bump, then the
   index onto the free list. An index freed here is available to the very next
   reservation.
4. **The buffers empty and keep their capacity**, so a steady-state drain
   allocates nothing.

The settle step sits *between* the passes so that the despawn pass is literally
today's despawn, with no special case for an outstanding reservation.

*Rejected:* per-handle "its spawns, then its despawns". A Despawn queued by one
handle could then reach a reserved Entity another handle has not spawned yet,
which the reserved-Entity rule forbids.

**A Spawn and a Despawn of the same Entity in one drain are not collapsed.** The
Entity is spawned in pass 1 and despawned in pass 3, recording both Hooks.
Collapsing would save a few hundred nanoseconds and lie to every index reading
the log.

### A queued Despawn whose Entity is not alive does nothing

One rule covers every case: **a queued Despawn of an Entity that is not alive at
the drain does nothing and records nothing.** Despawned immediately earlier in
the frame, despawned by another handle earlier in the same drain, queued twice
by the same handle, held from an earlier frame — all the same. There are no
sub-cases, and no distinction between a handle stale by a tick and one stale by
an hour. It is today's despawn, which already returns `false` for a non-alive
Entity, and Hooks' *a drained change that does nothing records nothing*.

**A stale Despawn never despawns the wrong Entity.** A handle whose index has
been recycled into a different Entity carries the old generation, so it fails
`Alive`. This is stated here rather than left to be inferred from the id layout,
because it is what makes queuing a Despawn safe at all.

**The drain never panics on a conflict**, in a validating build or any other. It
applies what it can and drops what it cannot. Validation mode's charter is a
`List.Set` through a handle that may not write, plus Hook shape at registration
— everything it catches is memory another System reads, and a stale or repeated
Despawn is not that. It is also not always a bug: a System that despawns
opportunistically, or two Systems that both decide an Entity is finished,
produce exactly this, and the generation check is the designed answer rather
than a missed error. If a stale despawn is ever worth catching it is a counter
on the drain, not a panic.

### One drain is one writer run per Store

However many passes it makes, **one `Drain()` counts as one writer run on each
Store it appends to**, for [the Hooks pace
check](hooks.md#a-reader-keeps-pace-with-its-writers). The count is per drain,
not per pass. That keeps the Hooks constraint true now that a drain is two
passes, and keeps Validation mode's pace check — a reader panics past sixteen
counted runs — measuring drains rather than the shape of the drainer.

### A reservation lasts until the next drain

Whichever System calls it. Since `ecs.DrainOnUpdate` is always subscribed, every
reservation is settled **within the Update it was made in**, or earlier by an
app drain System. There is no reservation that outlives a frame, and therefore
no reservation leak to specify a policy for.

### What a drain costs

`Drain()` over empty buffers is a **length check per enrolled buffer**, so the
drainer has **no empty-drain skip**. A skip would be a branch guarding a branch.

The `Last()` barrier is **not new in a real game** — `gfx.PresentOnUpdate`,
`sound.FlushOnUpdate` and the render flushes are already `Last()` — so the
general drainer adds one short node holding `write{*Entities}`, running apart
from any `Last()` subscriber that holds `*Entities`. A headless app with no
other `Last()` subscriber pays the barrier, ~8.2 µs, unconditionally. Each app
drain System costs the same barrier on the event it runs on.

**Gap.** The ~8.2 µs figure is `BenchmarkBarrierDeclared` −
`BenchmarkBarrierReading`; `ecs.md`'s ~6 µs is being reconciled in
[#283](https://github.com/dvoyni/cog/issues/283). Whichever figure settles,
the arithmetic below it is unchanged.

---

## Ownership without the write

Today `Spawn[S]` declares `write{*Store[F]}` per Component set field
(`declareSet`): redundant for locking, and kept because it is what makes the
kernel's `ErrUndeclaredDependency` fire, so **no plugin fabricates another
plugin's Components**. A deferring spawn that kept those writes would conflict,
for its whole run, with every System touching `F` — the narrow lock would not be
narrow.

**A `DeferredSpawn[S]` declares `read{*Store[F]}` per Component set field**
([what a deferring Spawn declares for
ownership](https://github.com/dvoyni/cog/issues/556)).

- **The ownership check still fires.** `checkCoupling` walks the read set and
  the write set through the same closure, and `hooks.md` already states it:
  *"the read of `*Store[T]` is also an ownership declaration"*. The check keeps
  firing with its real message, naming the Component and its owner.
- **It is strictly narrower.** A write conflicts with every Query and every
  `Get[F]`; a read conflicts only with **writers** of `F` — immediate spawners,
  `Set[F].UpdateFor`, `Remove[F].From` — which already serialise against each
  other on `F`. The requirement is met with room.
- **Declaring without using is the house pattern here**, not a compromise.
  `declareSet`'s own write is documented as *"redundant for locking and is kept
  anyway … as an ownership declaration"*; `accessor.go` writes
  `_ = declareComponent[T](…)` and documents dropping the handle as the point;
  `systemcall.go` and `hooks.go` each discard a `read{*Entities}` handle the
  same way. A deferring spawn holding a read handle it never calls `.Get()` on
  for its own sake is three precedents deep.

**The drain declares nothing per Store**, and `write{*Entities}` is the reason
rather than an excuse: it **is** the write lock over every Store at once — one
lock-set entry that means all of them, not a declaration that reaches around
them. Nothing can be touching any Store while the drain writes.

**Ownership is answered entirely at the queuing site.** A System cannot queue a
Spawn of `F` without having declared `read{*Store[F]}`, and therefore without
declaring a dependency on `F`'s owner. The drain belongs to `ecs`, fabricates
nothing of its own, and applies only what an authorised call queued.

**`Describe().Contention` reports a deferring spawner as a reader of
`*Store[F]`, and that is left alone.** `ResourceContention` carries the resource
and its owner, not a reason. The report is of lock sets, and the System's lock
set genuinely is a read; editorialising about intent would be reporting the
queue, which nothing does.

### The deferring Despawn declares `read{*Entities}` itself

Not merely inherited from some other handle: `DeferredDespawn` declares it. A
System holding only a despawn handle would otherwise name nothing on the
authority and could append to its buffer while the drain walked it. A read
serialises nothing — readers run together — and it is what makes the lock-free
buffer sound rather than lucky. It also satisfies `ecs.md`'s own rule that
anything a System keeps between calls is in its lock set, so **the kernel rather
than a comment keeps the two apart**.

---

## Where the queue lives

**In the handle, typed, and nothing in the queue path takes a lock or a mutex**
([where the typed buffer lives and how the drainer reaches
it](https://github.com/dvoyni/cog/issues/555)). That is the whole shape;
everything below follows from it.

### Two func registries, one per pass

`Entities` already keeps every registry it has as a slice of plain funcs bound
once at registration — `stores []func(Entity) bool`, `captures []func(Entity)`,
enrolled by `en.enrol(s.remove, s.shrink)`. The deferral registries are **two
more of the same**: one walked by the spawn pass, one by the despawn pass, each
in enrolment order.

A deferring handle enrols a `func()` bound to itself. The buffer keeps its type
inside the handle, the loop over it is monomorphised, and **a drain costs one
indirect call per handle per pass, never per change** — which is what the
allocation requirement demands.

*Rejected:* a `drainer` interface with a spawn method and a despawn method. It
forces a no-op half onto every handle that does only one of them, and it makes
the two passes a runtime test instead of two separate walks. Two registries make
"walk Spawns separately from Despawns" structural.

### A queued Spawn is one struct

Because the reserved Entity is fixed at the call, a queued Spawn is a **pair**:
the buffer is a slice of `struct{ e Entity; set S }`. The Entity and its
Component set are inseparable, a queue that could get out of step between them
is a bug waiting to be written, and it is one append and one bounds check rather
than two.

The deferring despawn buffer stays a plain `[]Entity` — there is nothing to
pair.

### Enrolment, and what enrolment order means

Enrolment happens in **the handle's own `prepare`**, which runs exactly once per
handle at registration and already receives the authority. It is one line, and
it stays inside `internal/types` as the declaration-root shape requires.

*Rejected:* enrolling from the `systemCall.lock` loop where `shareRowCopy`
lives. That exists to de-duplicate per-Store state across several handles of one
System; a deferral buffer shares nothing with anything.

This fixes, once, what **enrolment order** means for the drain: **the order
handles were prepared in** — parameter order within a System, registration order
between Systems.

### `resolve()` never touches the buffer

`resolve()` runs **once per invocation**, not once a tick. A handle that emptied
its buffer there would silently lose the first run's queue whenever a System was
invoked twice before a drain.

So `resolve()` caches the resolved `*Entities` and nothing else, and **a queue
survives every invocation until a drain** — not until the next tick. Only a
drain empties a buffer, cutting it to length zero and keeping its capacity.

This corrects the parked body, which said the per-tick resolve seam
([#282](https://github.com/dvoyni/cog/issues/282)) is where a deferring handle
would reset its buffer. It is not.

### Capacity comes back as Scratch

`enrolScratch(release func() uintptr)` already exists for exactly this and is
used by `Query.prepare` and `shareRowCopy`; `ShrinkCmd`'s `KeepScratch` is
already documented as *"per-System buffers: a Query's walk, a writer's row
copies for Changed"*. A deferral buffer is a per-System buffer of that kind, so
the handle enrols its release in the same `prepare`.

**No new `Keep` flag and no new area.** `KeepScratch` opts the buffers out, and
`ShrinkResponse.Scratch` counts their bytes.

A System that queues without bound grows its buffer without bound, exactly as
the free list does today. That is the app's to release through a Command, never
a heuristic's to guess.

### No lock and no mutex anywhere

The design is only accepted because it needs none. Four things carry it:

- **One writer, ever.** `access.Exclusive()` is declared unconditionally for
  every ECS System ([kernel:
  Exclusive](https://github.com/dvoyni/cog/issues/370)), so a System never runs
  concurrently with itself and its handle's buffer has exactly one appender.
- **The drain can never overlap a queuer.** The drain holds `write{*Entities}`,
  every deferring System holds `read{*Entities}`, and the scheduler's
  `compatible` refuses to run them together.
- **The registries are append-only at registration**, before any System has run,
  as `stores` and `captures` are.
- **Nothing is shared between Systems**: no per-Store queue, no pooled buffer,
  nothing two handles both reach.

The one atomic in the design is [the reservation
cursor](#reservation-holds-only-readentities): a single atomic add, not a lock,
no System serialised.

---

## Hooks

**A drained change is recorded at the drain, with the same records as an
immediate change** ([Deferred structural change and
Hooks](https://github.com/dvoyni/cog/issues/383)). An index cannot tell the two
apart. A deferring handle records nothing at the call and declares nothing for
Hooks.

- **Visibility follows `hooks.md`'s general rule**: a reader sees a drained
  change if its run starts after the drain System that applied it — this
  publication or a later one. This **supersedes** the constraint's earlier line
  *"a reader on the same event sees it in its next run"*, which was written
  assuming a `Last()` drain only; with app drain Systems, a reader ordered after
  one sees the change in its current publication. The constraint's other lines
  stand, and [`hooks.md` §*A drained change*](hooks.md#a-drained-change) is
  written against this document.
- **Order.** A Store's log follows the order in which the drain applies changes
  to it: handles in enrolment order, each buffer in queue order, spawn pass
  before despawn pass, nothing iterating a map.
- **A drained change that does nothing records nothing.**
- **A drained despawn captures each Component's value at the drain**, not at the
  call, including writes made between the two.
- **A drain that appends to a Store's log counts as one writer run** on that
  Store, once per drain.

**`DeferredSpawn[S]` does not implement `spawnGated`.** `systemCall.lock`
collects a spawn gate from any parameter offering one, and `enrolPace` then sets
`pace.every = true` for any `spawnGated` parameter — Validation mode's mark for
*"this System can append to every Store's log"*. **A queuing System appends to
nothing**: the writing and the records happen at the drain. If the handle
carried the gate, the run would be counted against the queuer as well as the
drain, and *one drain is one writer run per Store* would stop being true.

So the handle keeps its own resolved Stores and hands them to the drain, and the
drain — which holds `write{*Entities}` and **is** the Structural change — owns
the gate and the pace count. A queuing System is charged for its reads and
nothing more, which is the whole point of the narrow lock.

---

## What it costs

| | measured | source |
| --- | --- | --- |
| a typed queue, against immediate | **+4.6%** — 4 147 ns against 3 963 ns, zero allocations | [#240](https://github.com/dvoyni/cog/issues/240) |
| a type-erased (`[]any`) queue | **4 920 ns**, 50 allocs and 1 600 B a tick | [#240](https://github.com/dvoyni/cog/issues/240) |
| declaring `write{*Entities}` | **~8.2 µs** a frame | `BenchmarkBarrierDeclared` − `BenchmarkBarrierReading` |
| spawning four fields | 27.6 ns | `BenchmarkSpawnFourFields` |
| despawning across six Stores | 18.5 ns, ~3 ns a Store | `BenchmarkDespawnOnly` |

**Type erasure is why the buffer is typed and lives in the handle.** At 30 Hz,
1 600 B a tick is 48 KB/s fed to the collector during frames, which requirement
1 forbids. [`ecs.md` §*There is no command buffer, and the reason is
allocation*](ecs.md#there-is-no-command-buffer-and-the-reason-is-allocation)
keeps that measurement and gains a pointer here: **the typed arm of that
benchmark is what this document builds.** No general command buffer is built,
and `Set[T].UpdateFor` / `Remove[T].From` stay immediate.

**What a deferring System pays**, per change: one atomic add for a deferred
`New`, one append to a typed slice, and nothing else. **What it saves**: the
whole `write{*Entities}` barrier for its run, so every other System in the frame
that would have been excluded runs.

**Gap.** Every figure above is from a prototype
([#240](https://github.com/dvoyni/cog/issues/240)) or from the barrier
benchmarks, not from this design built. The crossover benchmark in
[Required work](#required-work) is what turns them into measurements of what
ships, and the README's §*What structural change costs* supersedes this table
when it does.

---

## What is not foreclosed

- **Deferring `Set[T].UpdateFor` and `Remove[T].From`.** Out of scope here, and
  costed separately by [what deferring every write-locking operation would
  cost](https://github.com/dvoyni/cog/issues/558). Nothing in this document
  blocks it: the registries, the enrolment order and the passes all take more
  kinds. What it would need beyond them is stated in the Hooks constraint — a
  deferred `From` records a removal at the drain, an inserting `UpdateFor` an
  addition, and a replacing `UpdateFor` records Changed at the drain by
  comparing bytes, attributed to the System that queued it.
- **A counter on the drain** for stale Despawns, if one is ever wanted. It is
  not a panic and not a report at the call.
- **A drain on an event other than `app.UpdateEvent`, subscribed by the ECS.**
  The ECS subscribes one, on Update. Nothing stops a later decision adding
  more; today an app writes them.
- **A `Keep` flag of its own for deferral buffers,** if their bytes ever need
  to be told apart from a Query's walk. Today they are Scratch.

---

## Shapes that were rejected

Each of these was considered and turned down; the reason is what stops the next
session re-deriving it.

- **A deferring mode on `Spawn[S]` / `WriteableEntities`** — a flag, a
  constructor, or a `Deferred()` promotion. The lock set is folded once at
  registration from each parameter's `prepare`, so a mode chosen later cannot
  change a declared lock, and a mode chosen at registration is a second type
  spelled worse.
- **`SpawnQueue[S]` / `DespawnQueue`, with one `Queue` method each.** `Queue` is
  free in the tree and *queue* is the glossary's verb for the act — but the
  queue is how the handle is built, not what the author is holding, and naming a
  handle for its buffer dates the name to one implementation. The cost of
  refusing it is that `New` hands back a not-alive Entity with nothing at the
  call site to say so, which is why [that is a rule in this
  document](#new-hands-back-an-entity-that-is-not-alive) rather than a remark.
- **One combined handle** doing both Spawn and Despawn — it would force a
  Component set type on Systems that never spawn.
- **`Len`, `Pending`, `Clear` or a drain report** — nothing reports what is
  queued, and capacity belongs to `ShrinkCmd`.
- **A registration panic on immediate and deferring handles in one System** — it
  is exactly what a half-migrated System looks like.
- **Returning whether a queued Despawn's Entity was alive at the call.** It
  answers a different and misleading question: an Entity alive at the call can
  be gone by the drain, and a reserved Entity is not alive at the call yet will
  certainly be despawned.
- **A `Dead` Tag, or any per-run Query filter** for queued despawns — a Filter
  contributes a read, so every Query would conflict with every despawner.
- **A lockless ownership declaration in the kernel** — a third set on
  `ResourceAccess`, walked by `checkCoupling` but withheld from `locks()`. It
  breaks the invariant `ResourceAccess` is built on: *requesting a handle is
  what declares the corresponding lock, so declaration and use cannot drift
  apart.* The kernel has a lock without a resource (`Exclusive`); it has never
  had a resource without a lock.
- **The ECS checking ownership itself.** It records no plugin —
  `componentClass.owner` and `Store.owner` are type-name strings — and it cannot
  see the transitive dependency closure the kernel's check tests against, so it
  could only reproduce a stricter, wronger rule.
- **A `drainer` interface** with a spawn method and a despawn method — a no-op
  half on every one-sided handle, and a runtime test where two registries give
  two walks.
- **Per-handle "its spawns, then its despawns"** — a Despawn could reach a
  reserved Entity not yet spawned.
- **Collapsing a Spawn and a Despawn of the same Entity in one drain** — it
  would lie to every index reading the log.
- **Resetting a buffer at `resolve()`** — `resolve()` runs once per invocation,
  so a System invoked twice before a drain would silently lose its first queue.
- **A block of reserved ids per handle per drain** — it needs a fallback when a
  block runs out, and adds a block-size knob.
- **`Alive` consulting a reservation structure** — a second liveness lookup on
  the hot path, where a free generation bit is one compare.
- **A deferred `WriteableEntities.Shrink()` replacing `ShrinkCmd`'s index step**
  — it could not return a `ShrinkResponse`, and it would move the release rule
  off a Command.
- **An `ecs.Config` opt-out for the general drainer** — proposed, then
  withdrawn. A queue that is never drained is not a configuration.
- **An empty-drain skip** — `Drain()` over empty buffers is a length check per
  buffer; a skip is a branch guarding a branch.

---

## Required work

This is the checklist the implementation is built from.

**The handles**

1. `DeferredSpawn[S]` and `DeferredDespawn` in `bundles/ecs/internal/types`,
   beside `spawn.go` and `writeableentities.go`, aliased in the root's
   `types.go` next to `Spawn` and `WriteableEntities` with the shorter
   re-written doc that file uses. Nothing new in `utils.go` beyond the corrected
   prose list; enrolment stays inside `internal/types`
   ([#358](https://github.com/dvoyni/cog/issues/358)).
2. `DeferredSpawn[S].New`'s doc comment **leads with** the not-alive rule,
   mirroring `Spawn[S].New`'s *"the Entity is complete when New returns"*.
3. `prepare` on each: `GetRead[*Entities]`, `read{*Store[F]}` per set field for
   the spawn handle, enrolment in the matching registry, and `enrolScratch` for
   the buffer's release.
4. Neither handle implements `spawnGated`.

**`Entities` and the drain**

5. Two func registries on `Entities`, append-only at registration, one per pass.
6. The reservation cursor: one atomic word over the free list, with the
   past-`len(gens)` floor.
7. The free generation bit, and `Alive` as one compare against it.
8. The released list, and the settle step between the passes.
9. `WriteableEntities.Drain()`: spawn pass, settle, despawn pass, buffers cut to
   zero length keeping capacity; one writer run per Store per drain.
10. `ecs.DrainOnUpdate`, subscribed by the ECS on `app.UpdateEvent`, `.Last()`,
    always. The ECS's first `app` dependency and first System; `Dependencies()`
    stays nil.
11. `ShrinkCmd`: no index dropping and 0 bytes for Entities while any
    reservation is outstanding; `KeepScratch` covers the deferral buffers and
    `ShrinkResponse.Scratch` counts them.

**Behaviour tests** — the rules above are the list; at minimum:

12. A deferring System's change is invisible before the drain and visible after
    it, for Queries, every Accessor and `Alive`.
13. The queuer sees nothing of its own queue within its run.
14. A reserved Entity stored as a Reference resolves to nothing before the drain
    and to the Entity after it.
15. A queued Spawn and a queued Despawn of the same reserved Entity in one drain
    produce both records, in that order.
16. A queued Despawn of a non-alive Entity does nothing and records nothing; a
    stale handle whose index was recycled never reaches the new Entity.
17. An app drain System makes a change visible within the same publication.
18. A queue survives a System invoked twice before a drain.
19. Immediate and deferring handles in one System compose, with the write
    subsuming the read.
20. `ErrUndeclaredDependency` still fires for a `DeferredSpawn[S]` over another
    plugin's Component.

**Hooks tests** (carried from
[#383](https://github.com/dvoyni/cog/issues/383)):

21. A Hooks reader on the deferring System's event sees a drained spawn and
    despawn in its next run, not before the drain.
22. A drained despawn of a dead Entity records nothing.
23. A drained despawn carries the value as it stood at the drain.

**Measurement**

24. The **crossover benchmark**, beside `BenchmarkBarrier*` in
    `bundles/ecs/internal/types`: immediate (`*ecs.Spawn[S]`,
    `*ecs.WriteableEntities`) against deferred (`*ecs.DeferredSpawn[S]`,
    `*ecs.DeferredDespawn`) at **1, 10, 100 and 500 changes a tick**, draining
    through `ecs.DrainOnUpdate`. One further arm over the reservation atomic
    under parallel deferring Systems.
25. The allocation line held: 6 objects a frame at 1 000 and 10 000 Entities,
    and no `moved to heap` in `ecs`.
26. `-count=10` in place of `-race`, which cannot build on the development
    machine. Whole-frame A/B comparisons alternate two binaries.

**Documents**

27. `ecs.md` §*What a System sees* gains a pointer here — **not** a second copy
    of the promise.
28. `ecs.md` §*The shape* and §*What a signature may contain* gain the two
    handles; the registration-failure sentence in `systemcall.go` and the
    guidance list in `utils.go` gain them too, and `utils.go`'s list is
    corrected (it omits `*Hooks` and `*Resp`).
29. `ecs.md` §*There is no command buffer* keeps its measurement and gains the
    pointer that the typed arm is what this document builds.
30. `Spawn[S].New`'s and `Set[T].UpdateFor`'s doc comments, which today say
    *"there is no command buffer and nothing is deferred"*, are corrected: a
    typed per-handle buffer now exists for Spawn and Despawn, neither is a
    general command buffer, and `UpdateFor` / `From` stay immediate.
31. `ecs.md`'s generation-reuse figure updates for the free bit: ~2×10⁹ rather
    than ~4×10⁹.
32. `hooks.md` §*A drained change* is rewritten against this document, and
    `ecs.md`'s parked-ticket references to
    [#260](https://github.com/dvoyni/cog/issues/260) point here.
33. `CONTEXT.md` already carries **Drain**, **Reserved Entity**, **Deferred
    Spawn** and **Deferred Despawn**, written as the tickets resolved. Nothing
    further is coined here.

---

## Out of scope

- **Deferred `Set[T].UpdateFor` and `Remove[T].From`** (owner, 2026-09-22). Both
  hold only the narrow `write{*Store[T]}` and have no safety reason to defer;
  adding them would be ergonomics and would double the vocabulary. Their cost is
  investigated by [what deferring every write-locking operation would
  cost](https://github.com/dvoyni/cog/issues/558), which informs but does not
  route this work.
- **A general command buffer** — cross-System queues, deferred value writes,
  batched or ordered commands. The allocation measurement above is why, and
  `ecs.md` §*There is no command buffer* is where it is recorded.
- **Reporting what is queued** — no `IsPending`, no counts, nothing through MCP.
- **A kernel affordance for lockless ownership.** Rejected above, and not
  re-opened by a later ECS need; it would be a kernel decision on a kernel map.
- **A consumer.** This is engine capability. The first game System to use it is
  the game's, not this document's.
