# ecs as an mcp provider — specification

`ecs` offers an agent six capabilities. Three are looks: **`ecs_world`** for the
census of Components and Entities, **`ecs_entity`** for one Entity and the
Components it carries, and **`ecs_query`** for the Entities carrying every named
Component. Three are acts: **`ecs_spawn`**, **`ecs_despawn`** and
**`ecs_update`**, which change the running game by Component name, so an agent
can set up the situation it is testing instead of each game adding a capability
of its own for every case.

The mechanism underneath is the read Commands of
[ecs.md §Reading the world by name](./ecs.md#reading-the-world-by-name) and the
write Commands of
[ecs.md §Writing the world by name](./ecs.md#writing-the-world-by-name), which
name a Component by the string `kernel.TypeName` renders. This document
specifies only what the agent sees, and reproduces the description prose in full
so it can be reviewed as prompt text.

The extension point is
[bundles/mcp/docs/specs/mcp.md](../../../mcp/docs/specs/mcp.md). Assembled from
[ecs: offer the world to an agent as an mcp provider](https://github.com/dvoyni/cog/issues/289);
the writes from
[ecs: offer world writes to an agent over MCP](https://github.com/dvoyni/cog/issues/567).

---

## Contents

- [The provider](#the-provider)
- [`ecs_world`](#ecs_world) · [`ecs_entity`](#ecs_entity) · [`ecs_query`](#ecs_query)
- [Writing](#writing): [`ecs_spawn`](#ecs_spawn) · [`ecs_despawn`](#ecs_despawn) · [`ecs_update`](#ecs_update)
- [What a call costs the frame](#what-a-call-costs-the-frame)
- [The description prose](#the-description-prose)
- [Out of scope](#out-of-scope)

---

## The provider

`ecs` contributes an `mcp.Provider` itself, from its plugin's `Register` in
`bundles/ecs/internal`, after the by-name Commands. The Provider is an
unexported value holding nothing, and the root declares its Adapter identity in
`adapters.go`, as input's does:

```go
type McpProvider kernel.Adapter[mcp.ProviderPort] // bundles/ecs/adapters.go

registrar.ProvideAdapter[ecs.McpProvider](mcp.Provider(provider{})) // in the plugin's Register

func (provider) Capabilities() []mcp.Capability {
	return []mcp.Capability{
		mcp.Func("world", censusDescription, readCensus, mcp.ReadOnly()),
		mcp.Func("entity", entityDescription, readEntity, mcp.ReadOnly()),
		mcp.Func("query", queryDescription, readQuery, mcp.ReadOnly()),
		mcp.Func("spawn", spawnDescription, writeSpawn),
		mcp.Func("despawn", despawnDescription, writeDespawn),
		mcp.Func("update", updateDescription, writeUpdate),
	}
}
```

The capability names render as `ecs_world`, `ecs_entity`, `ecs_query`,
`ecs_spawn`, `ecs_despawn` and `ecs_update`, because `ecs.Name` is `"ecs"`. The
first is the census of ecs.md: the wire keeps the name the tool was specified
under, and only Go identifiers avoid the word, because no identifier in ecs may
name a World.

**Each is an `mcp.Func`, for exactly one reason.** Each body dispatches its
Command and, when the response carries a non-empty `Refusal`, returns
`mcp.Unavailable` carrying it; otherwise it returns the response. An
`mcp.Command` would ship a refusal as a successful result, and the agent would
read an empty answer as the truth. The bodies are package functions, touch no
Provider state and no resource, and reach ecs only by dispatch.

**There is no branch for a zero response.** On a stopped scheduler
`ExecuteCommand` reports the dispatch it could not perform and answers the zero
response, and that answer is accepted as it is.

**The three looks are `mcp.ReadOnly()`**, so a client may auto-approve them and
an agent may call them freely. **The three acts are not**, and `mcp` offers no
other annotation, so a client asks before each.

**The Provider costs a game nobody is debugging nothing.** It adds no
subscription and no resource, so it cannot add work to a frame. `Dependencies()`
stays `nil`, as the broker's does, so an app that does not compose
`mcpplugin.New()` binds the Adapter to nothing: composition is the only gate,
for the acts as for the looks.

The request and response types are the Commands' own, from
`bundles/ecs/internal/types`, used directly as the payloads. Nothing is aliased
in the root, and `ecsplugin` still exports only `New`. `Refusal` is `json:"-"`,
so it is in no schema.

---

## `ecs_world`

What exists: every registered Component name, as `kernel.TypeName` renders it
and as `mcpserver_architecture` names types, with how many Entities carry it,
sorted by name; how many Entities are alive (`entities`); how many indices wait
on the free list (`freeIndices`); every index ever allocated (`indexSpace`); and
how many write calls have changed the world since the game started
(`agentWrites`). A free list or index space that keeps growing is churn or a
leak. The request has no fields, and it is never refused.

It is the one to call first: the names it lists are how every other tool names
Components.

---

## `ecs_entity`

| field | |
| --- | --- |
| `entity` (request) | the Entity as `7v2`, `Entity(7v2)` or its decimal handle: whatever a log or an earlier answer gave |
| `components` (request) | optional: only these Components; a named one the Entity does not carry is left out |
| `entity` (response) | the Entity as `Entity.String` renders it |
| `components` | every Component it carries, or the named ones it carries, sorted by name, each `{name, value, error}` |

**Values are the Component's exported fields as JSON.** An `m.List` is an
array, an `assets.Blob` is `{"len":N}` (its length, never its bytes), an Entity
Reference is its decimal handle, which `ecs_entity` accepts back, and an
`m.Maybe` is its value or `null` when absent. Unexported fields are not shown,
so a Component whose fields are all unexported shows as `{}`, and a Tag shows as
`{}`. A Component whose value could not be encoded (a NaN) carries `error`
instead, and that Component alone.

**There is no `ecs_get`.** Reading chosen Components of one Entity is this tool
with `components` given: one answer shape and one fewer tool to choose between,
and what it answers is what `ecs_update` takes back. A separate tool would have
answered the same values under a second name.

**Refusals**, as `mcp.Unavailable`: a malformed Entity, naming the three forms;
`NoEntity`; an Entity that is not alive, whose reason names the Entity that
holds its index now, when one does; and a `components` name no Component has, or
two types render, refused as `ecs_query` refuses it. A handle to a free index is
not alive, including the one that index will carry next.

---

## `ecs_query`

| field | |
| --- | --- |
| `components` (request) | Component names as `ecs_world` lists them; an Entity must carry every one |
| `limit` (request) | how many matches to return: omitted or 0 is 50, at most 500 |
| `total` | how many Entities matched, returned or not |
| `truncated` | true when fewer were returned than matched |
| `entities` | the first `limit` matches, in ascending index order, each with only the named Components |

The match is **every** named Component, and each Entity carries only those:
`ecs_entity` answers the rest of one. Values render as in `ecs_entity`.

**Refusals**, as `mcp.Unavailable`: no name at all, or a name no Component has,
each listing every registered name so the agent can retry at once; a name two
types render, listing both package-qualified forms; and a limit below 0 or
above 500.

`limit` is optional on the wire (`omitempty`), so the schema does not require
it; the prompt tells the agent what omitting it means.

---

## Writing

The three acts change the game by Component name. Values take the JSON shape
the looks answer with, so what `ecs_entity` returns can be edited and sent back.
The spec that raised mutation named two concerns, and each is answered here with
the decision taken.

### A write lands between Systems, and a run records that it was written to

"The capability most able to corrupt a run in a way nobody can reproduce."
**Each write is a Command holding `write{*ecs.Entities}` and nothing else**,
exactly as the reads are. That excludes every ECS System, so a write lands
between two Systems' runs and never inside one, and works while the game is
paused; the next System to run sees it.

**Every write is recorded as the same act from a System would be.** A spawn is
a Spawn to every Hook reader, with its values; a despawn is a Despawn, with each
Component's last value; a Component added is an addition, one taken away a
removal with its last value, and a value changed a change. An index or a cache
a game builds on Hooks, such as a wall cache re-merging runs when a cell breaks,
therefore stays true across a write, which is the property a test that breaks
one wall cell relies on. A change names no System, so every reader is given it,
the writing System's own readers included, since there is none.

**What an agent can do to make a run reproducible is keep its calls.** A write
comes from outside the game, so no replay of the game alone makes it again.
`ecs_world` counts the calls that changed the world as `agentWrites`, so an
agent, or a human reading its transcript, knows at a glance whether a run is the
game's alone; the calls themselves are the agent's to keep. There is no write
log: the count answers "can I trust this run", and a log would be a second copy
of the agent's own transcript.

### Writing skips the ownership a Spawn declares, and ecs owns that exception

"A runtime write through `Entities` would skip the ownership declaration a
static `Spawn[S]` makes." **It does, and the writes are an acknowledged
exception, owned by ecs.** A `Spawn[S]` declares `write{*Store[C]}` for each `C`
it carries so that the composition check makes a plugin fabricating another
plugin's Components declare a dependency on their owner. A write by name holds
no Component type and declares no Store. That is not a plugin fabricating
another's Components: it is ecs, the authority over every Store, acting as a
debugger, and an app has it only by composing the broker.

**The alternative, rejected:** a second gate, such as an ecs config flag
(`AgentWrites`, off by default) or a build tag. Composing the broker is already
a development-only choice, and a second switch would be one more thing to forget
on the day the game is opened to debug, while protecting nothing composition
does not.

### Values merge, and what the JSON cannot show survives

**A value is decoded over the Entity's current value.** A field the JSON leaves
out keeps its value, so `{"HP": 0}` zeroes one field of a Health. A Component
the Entity lacks is decoded over the zero value, so a field left out is zero.

**Writes are allowed on Components holding unexported fields.** The JSON can
never name one, so on an update it keeps its value and on an add or a spawn it
is zero. The read path's limitation, stated in its prompt instead of solved, is
the same limitation here, and stated the same way.

**An `m.List` is an array and replaces the whole list.** `m.List` gained
`UnmarshalJSON`, the mirror of its `MarshalJSON`, which decodes into an array of
its own, so a decode never writes through the stored array.

**An `assets.Blob` is never written.** Its bytes never cross, so its
`{"len":N}` decodes into nothing: an update keeps the Blob as it was, whatever
`len` is given, and a spawn or an add leaves it empty. A texture Component's
other fields stay editable.

**A field the Component does not have is refused**, because `encoding/json`
would ignore it and leave the agent believing it wrote. It is found by encoding
the decoded value again and looking for every key the agent gave.

**An unedited round trip writes nothing.** A value that encodes as the one the
Entity has is `unchanged`: it is not written and records no change. That is
what keeps a value read with `ecs_entity` and sent back unedited byte-identical
in the Store, because a decoded List is a new array and a byte compare would
call it a change. Under merge only fields the JSON shows can differ, so JSON
equality is value equality here.

**Numbers arrive exact.** The requests decode with `UseNumber`, as the reads
encode, so an Entity Reference past 2^53 is not rounded through a float64.

### A request is all or nothing

Every name is resolved, the Entity checked and every value decoded before
anything is applied. Any refusal answers the whole call as `mcp.Unavailable`,
naming every entry that failed with what would have worked, and nothing is
applied. Per-entry outcomes are for a call that succeeded; a call partly applied
is the unreproducible state the first concern warns of.

**Refusals**, as `mcp.Unavailable`: a malformed Entity, naming the three forms;
an Entity that is not alive, naming the Entity that holds its index now; a name
no Component has, listing every registered name; a name two types render,
listing both package-qualified forms; one Component named twice in one request,
or in both `set` and `remove`; a value that does not decode into the
Component's type, with the decoder's reason and a pointer to `ecs_entity`'s
shape; a field the Component does not have, with the value's fields as they
encode; and an update that names nothing.

---

## `ecs_spawn`

| field | |
| --- | --- |
| `components` (request) | Component name to value; none spawns a bare Entity |
| `entity` (response) | the new Entity as `Entity.String` renders it |

## `ecs_despawn`

| field | |
| --- | --- |
| `entity` (request) | the Entity, in any form `ecs_entity` takes |
| `entity` (response) | the Entity as `Entity.String` renders it |
| `wasAlive` | false when it had already gone, and nothing happened |

A despawn of an Entity already gone is an answer, not a refusal, so a cleanup an
agent repeats is harmless. Only a malformed Entity is refused.

## `ecs_update`

| field | |
| --- | --- |
| `entity` (request) | the Entity, in any form `ecs_entity` takes |
| `set` (request) | Component name to value: merged into one the Entity carries, added over zero otherwise |
| `remove` (request) | Component names to take away |
| `entity` (response) | the Entity as `Entity.String` renders it |
| `components` | per named Component, sorted by name, `{name, outcome}` |

The outcome is `added`, `changed`, `unchanged`, `removed`, or `absent`: removing
a Component the Entity does not carry is idempotent, as a despawn of an Entity
already gone is.

**No batch form.** One call is one lock hold, and one request's Components are
already atomic. Several Entities in one lock hold is
[out of scope](#out-of-scope) until a setup is large enough that the per-call
stall matters.

---

## What a call costs the frame

**Each Command holds `write{*ecs.Entities}` and nothing else.** Every handler
that touches a Store holds `read{*ecs.Entities}`, so a call waits for every ECS
System running now to let go, and under Conflict-aware FIFO every ECS System
queued behind it waits until it returns. For a read the stall is linear in the
walk plus the encodings, and for `ecs_query` the limit is what bounds it: 500 at
most. For a write it is the decode and encode of the values given, and a
despawn's walk of every Store. The prompt says so, and tells the agent to keep
`limit` small in a busy game and not to loop a call every tick.

**No frame's lock set changes.** They are Commands, not Systems: a frame nobody
calls into runs exactly the handlers and lock sets it ran before. A call needs no
tick and works while the game is paused, so the agent need not step the game to
look, or to write.

**What does change is visible in the contention report.**
`mcpserver_architecture`'s contention report
([#290](https://github.com/dvoyni/cog/issues/290)) lists the six Commands, named
as `kernel.TypeName` renders them — `ecs.censusCmd`, `ecs.entityCmd`,
`ecs.queryCmd`, `ecs.spawnCmd`, `ecs.despawnCmd` and `ecs.updateCmd` — as
writers of `*ecs.Entities` conflicting with every ECS System. Those pairs are
these tools, not the game. Every other writer of `*ecs.Entities`,
`ecs.ShrinkCmd` or a System that spawns or despawns, is the game's. The prompt
text names the six, so an agent discounts exactly those pairs; a test computes
the names with `kernel.TypeName`, so a rename cannot leave the prompt naming a
type that is gone.

---

## The description prose

Reproduced in full, per the house style, so it is reviewed as prompt text. A
test checks that each string below is the Go constant, word for word.

> **`ecs_world`** — The game's ECS at a glance: every registered Component,
> named as `mcpserver_architecture` names types, with how many Entities carry
> it; how many Entities are alive; how many indices wait on the free list for
> reuse; and the index space, every index ever allocated. A free list or index
> space that keeps growing is churn or a leak. `agentWrites` counts the
> `ecs_spawn`, `ecs_despawn` and `ecs_update` calls that changed the world since
> the game started: when it is not 0, this run is not the one the game alone
> would have made. Call this first: the Component names it lists are how
> `ecs_entity`, `ecs_query` and the writes name Components. It changes nothing,
> needs no tick and works while the game is paused, so do not step the game
> just to look. Every call waits for the ECS Systems running now and holds up
> the ones queued behind it until it returns.
>
> In `mcpserver_architecture`'s contention report, `ecs.censusCmd`,
> `ecs.entityCmd`, `ecs.queryCmd`, `ecs.spawnCmd`, `ecs.despawnCmd` and
> `ecs.updateCmd` appear as writers of `*ecs.Entities` conflicting with every
> ECS System. Those pairs are these six tools, not the game: discount exactly
> them. Every other writer of `*ecs.Entities`, such as `ecs.ShrinkCmd` or a
> System that spawns or despawns, is the game's.

> **`ecs_entity`** — One Entity and every Component it carries, sorted by name,
> each with its value. Give the Entity in whatever form a log or an earlier
> answer printed it: `7v2`, `Entity(7v2)` or its decimal handle. Give
> `components` to see only those, in the shape `ecs_update` takes back; a named
> Component the Entity does not carry is left out. A refused Entity is not
> alive: it was despawned, and when its index was reused the reason names the
> Entity that now holds it, so stop reasoning about the one you asked for.
>
> Values are the Component's exported fields as JSON. An `m.List` is an array,
> an `assets.Blob` is `{"len":N}` (its length, never its bytes), an Entity
> Reference is its decimal handle, which you can pass back to `ecs_entity`, and
> an `m.Maybe` is its value, or `null` when absent. Fields that are unexported
> are not shown, so a Component whose fields are all unexported shows as `{}`:
> a missing field is not a zero one. A Component with an `error` instead of a
> value could not be encoded, and the error says why.
>
> It changes nothing, needs no tick and works while the game is paused, so do
> not step the game just to look. Every call waits for the ECS Systems running
> now and holds up the ones queued behind it until it returns.

> **`ecs_query`** — The Entities that carry every one of the named Components,
> in ascending index order, each with the values of only those Components: call
> `ecs_entity` for the rest of one. Name Components as `ecs_world` lists them;
> values render as `ecs_entity` describes. At most `limit` Entities come back:
> 50 when you give none, and never more than 500. `total` is how many matched
> and `truncated` is true when you were shown fewer, so never read a truncated
> answer as the whole world.
>
> It changes nothing, needs no tick and works while the game is paused. Every
> call waits for the ECS Systems running now and holds up the ones queued behind
> it until it returns, for longer the more it walks: keep `limit` small in a
> busy game, and do not loop it every tick.
>
> In `mcpserver_architecture`'s contention report, `ecs.censusCmd`,
> `ecs.entityCmd`, `ecs.queryCmd`, `ecs.spawnCmd`, `ecs.despawnCmd` and
> `ecs.updateCmd` appear as writers of `*ecs.Entities` conflicting with every
> ECS System. Those pairs are these six tools, not the game: discount exactly
> them. Every other writer of `*ecs.Entities`, such as `ecs.ShrinkCmd` or a
> System that spawns or despawns, is the game's.

> **`ecs_spawn`** — Spawns one Entity carrying the named Components, and answers
> it. Give `components` as Component name, as `ecs_world` lists it, to value, in
> the shape `ecs_entity` shows one: a field you leave out is zero, and `{}` is a
> Tag or an all-zero value. Give none for a bare Entity. An `m.List` is an
> array. An `assets.Blob` cannot be written, because its bytes never cross: a
> spawned one is empty whatever `len` you give. A field that is unexported
> cannot be named, and is zero.
>
> All or nothing: an unknown name, a value that does not fit its Component or a
> field it does not have refuses the whole call, naming every entry that failed,
> and nothing is applied. Every Hook reader in the game sees the write as the
> same act from a System would be (a spawn, a despawn, an addition, a change, a
> removal), so an index or a cache the game builds on Hooks stays right.
>
> This changes the game. It lands between Systems, never inside one's run, and
> needs no tick: it works while the game is paused, and the next System to run
> sees it. Every call waits for the ECS Systems running now and holds up the
> ones queued behind it until it returns. A run you have written to is not the
> run the game alone would make: `ecs_world` counts your writes as
> `agentWrites`, so keep the calls you made if the run must be made again.
>
> In `mcpserver_architecture`'s contention report, `ecs.censusCmd`,
> `ecs.entityCmd`, `ecs.queryCmd`, `ecs.spawnCmd`, `ecs.despawnCmd` and
> `ecs.updateCmd` appear as writers of `*ecs.Entities` conflicting with every
> ECS System. Those pairs are these six tools, not the game: discount exactly
> them. Every other writer of `*ecs.Entities`, such as `ecs.ShrinkCmd` or a
> System that spawns or despawns, is the game's.

> **`ecs_despawn`** — Despawns one Entity, given in any form `ecs_entity` takes,
> with every Component it carries. `wasAlive` is false when it had already gone,
> and then nothing happened: that is not an error. Every Hook reader in the game
> sees a despawn, with each Component's last value, as it would from a System.
>
> This changes the game. It lands between Systems, never inside one's run, and
> needs no tick: it works while the game is paused, and the next System to run
> sees it. Every call waits for the ECS Systems running now and holds up the
> ones queued behind it until it returns. A run you have written to is not the
> run the game alone would make: `ecs_world` counts your writes as
> `agentWrites`, so keep the calls you made if the run must be made again.
>
> In `mcpserver_architecture`'s contention report, `ecs.censusCmd`,
> `ecs.entityCmd`, `ecs.queryCmd`, `ecs.spawnCmd`, `ecs.despawnCmd` and
> `ecs.updateCmd` appear as writers of `*ecs.Entities` conflicting with every
> ECS System. Those pairs are these six tools, not the game: discount exactly
> them. Every other writer of `*ecs.Entities`, such as `ecs.ShrinkCmd` or a
> System that spawns or despawns, is the game's.

> **`ecs_update`** — Changes one Entity's Components, given in any form
> `ecs_entity` takes. `set` maps Component name to value: a Component the Entity
> carries takes the fields you give and keeps every other, so `{"HP": 0}` zeroes
> one field, and one it lacks is added, with the fields you leave out zero.
> `remove` lists Components to take away. Naming one Component in both `set` and
> `remove` is refused. The answer gives each named Component's outcome:
> `added`; `changed`; `unchanged`, when the value you gave is the value it had,
> so nothing was written, which a value read with `ecs_entity` and sent back
> unedited always is; `removed`; or `absent`, when it was not carried to remove,
> which is not an error.
>
> Values take the shape `ecs_entity` shows. An `m.List` is an array and replaces
> the whole list. An `assets.Blob` cannot be written, because its bytes never
> cross: it keeps its bytes whatever `len` you give. A field that is unexported
> cannot be named, and keeps its value.
>
> All or nothing: an unknown name, a value that does not fit its Component or a
> field it does not have refuses the whole call, naming every entry that failed,
> and nothing is applied. Every Hook reader in the game sees the write as the
> same act from a System would be (a spawn, a despawn, an addition, a change, a
> removal), so an index or a cache the game builds on Hooks stays right.
>
> This changes the game. It lands between Systems, never inside one's run, and
> needs no tick: it works while the game is paused, and the next System to run
> sees it. Every call waits for the ECS Systems running now and holds up the
> ones queued behind it until it returns. A run you have written to is not the
> run the game alone would make: `ecs_world` counts your writes as
> `agentWrites`, so keep the calls you made if the run must be made again.
>
> In `mcpserver_architecture`'s contention report, `ecs.censusCmd`,
> `ecs.entityCmd`, `ecs.queryCmd`, `ecs.spawnCmd`, `ecs.despawnCmd` and
> `ecs.updateCmd` appear as writers of `*ecs.Entities` conflicting with every
> ECS System. Those pairs are these six tools, not the game: discount exactly
> them. Every other writer of `*ecs.Entities`, such as `ecs.ShrinkCmd` or a
> System that spawns or despawns, is the game's.

The request and response fields carry `jsonschema` prose of their own, which
the agent reads in the tool's schema:

> **`entity`** — the Entity as 7v2 or Entity(7v2) or its decimal handle: any
> form a log or an earlier answer gave

> **`components`** (`ecs_entity`) — only these Components, named as ecs_world
> lists them; a named Component the Entity does not carry is left out. Omit for
> every Component it carries

> **`components`** (`ecs_query`) — Component names spelled as kernel.TypeName
> renders them and as ecs_world lists them; an Entity must carry every one

> **`limit`** — how many matching Entities to return: omit or 0 for 50; at most
> 500

> **`total`** — how many Entities matched in all, including those not returned

> **`truncated`** — true when fewer Entities were returned than matched: the
> list is not the whole answer

> **`value`** — the Component's exported fields as JSON; unexported fields are
> not shown, and it is null when error is set

> **`error`** — present when the value could not be encoded as JSON, saying why

> **`agentWrites`** — how many ecs_spawn, ecs_despawn and ecs_update calls have
> changed the world since the game started; not zero means this run is not the
> one the game alone would have made

> **`components`** (`ecs_spawn`) — Component name, as ecs_world lists it, to its
> value as ecs_entity shows one; give none for a bare Entity

> **`wasAlive`** — false when the Entity was already gone, so nothing was
> despawned

> **`set`** — Component name to value: a Component the Entity carries takes the
> fields given and keeps the rest, and one it lacks is added with the fields
> given and the rest zero

> **`remove`** — Component names to take away from the Entity

> **`outcome`** — added, changed, unchanged (the value given is the value it
> had, so nothing was written), removed, or absent (removed, but the Entity did
> not carry it)

---

## Out of scope

- **A batch form**: several Entities' writes in one lock hold. One call is one
  lock hold and one request's Components are already atomic; a batch is worth
  its schema when a setup is large enough that the stall per call matters.
- **A write log.** `agentWrites` says a run was written to; the calls are the
  agent's transcript.
- **Writing an `assets.Blob`'s bytes**, which never cross the wire either way.
- **Telling a never-dispatched call apart from an answer.** A zero response at
  shutdown is accepted.
- **Changes to the broker or the contention report.** The broker's `m.List`
  array schema is [#547](https://github.com/dvoyni/cog/issues/547); these tools
  carry values as `any` and do not need it.
- **Marshallers for Components with unexported fields.** The prompt states the
  limitation instead, and a write keeps what it cannot show.
