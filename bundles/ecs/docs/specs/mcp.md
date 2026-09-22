# ecs as an mcp provider — specification

`ecs` offers an agent three capabilities, all of them looks: **`ecs_world`**
for the census of Components and Entities, **`ecs_entity`** for one Entity and
every Component it carries, and **`ecs_query`** for the Entities carrying every
named Component. None of them changes the game.

The mechanism underneath is the three read Commands of
[ecs.md §Reading the world by name](./ecs.md#reading-the-world-by-name), which
read the world by the Component name `kernel.TypeName` renders. This document
specifies only what the agent sees, and reproduces the description prose in full
so it can be reviewed as prompt text.

The extension point is
[bundles/mcp/docs/specs/mcp.md](../../../mcp/docs/specs/mcp.md). Assembled from
[ecs: offer the world to an agent as an mcp provider](https://github.com/dvoyni/cog/issues/289).

---

## Contents

- [The provider](#the-provider)
- [`ecs_world`](#ecs_world) · [`ecs_entity`](#ecs_entity) · [`ecs_query`](#ecs_query)
- [What a call costs the frame](#what-a-call-costs-the-frame)
- [The description prose](#the-description-prose)
- [Out of scope](#out-of-scope)

---

## The provider

`ecs` contributes an `mcp.Provider` itself, from its plugin's `Register` in
`bundles/ecs/internal`, after the read Commands. The Provider is an unexported
value holding nothing, and the root declares its Adapter identity in
`adapters.go`, as input's does:

```go
type McpProvider kernel.Adapter[mcp.ProviderPort] // bundles/ecs/adapters.go

registrar.ProvideAdapter[ecs.McpProvider](mcp.Provider(provider{})) // in the plugin's Register

func (provider) Capabilities() []mcp.Capability {
	return []mcp.Capability{
		mcp.Func("world", censusDescription, readCensus, mcp.ReadOnly()),
		mcp.Func("entity", entityDescription, readEntity, mcp.ReadOnly()),
		mcp.Func("query", queryDescription, readQuery, mcp.ReadOnly()),
	}
}
```

The capability names render as `ecs_world`, `ecs_entity` and `ecs_query`,
because `ecs.Name` is `"ecs"`. The first is the census of ecs.md: the wire keeps
the name the tool was specified under, and only Go identifiers avoid the word,
because no identifier in ecs may name a World.

**Each is an `mcp.Func`, for exactly one reason.** Each body dispatches its
read Command and, when the response carries a non-empty `Refusal`, returns
`mcp.Unavailable` carrying it; otherwise it returns the response. An
`mcp.Command` would ship a refusal as a successful result, and the agent would
read an empty answer as the truth. The bodies are package functions, touch no
Provider state and no resource, and reach ecs only by dispatch.

**There is no branch for a zero response.** On a stopped scheduler
`ExecuteCommand` reports the dispatch it could not perform and answers the zero
response, and that answer is accepted as it is.

**All three are `mcp.ReadOnly()`**, so a client may auto-approve them and an
agent may call them freely.

**The Provider costs a game nobody is debugging nothing.** It adds no
subscription and no resource, so it cannot add work to a frame. `Dependencies()`
stays `nil`, as the broker's does, so an app that does not compose
`mcpplugin.New()` binds the Adapter to nothing: composition is the only gate.

The request and response types are the read Commands' own, from
`bundles/ecs/internal/types`, used directly as the payloads. Nothing is aliased
in the root, and `ecsplugin` still exports only `New`. `Refusal` is `json:"-"`,
so it is in no schema.

---

## `ecs_world`

What exists: every registered Component name, as `kernel.TypeName` renders it
and as `mcpserver_architecture` names types, with how many Entities carry it,
sorted by name; how many Entities are alive (`entities`); how many indices wait
on the free list (`freeIndices`); and every index ever allocated (`indexSpace`).
A free list or index space that keeps growing is churn or a leak. The request
has no fields, and it is never refused.

It is the one to call first: the names it lists are how the other two tools
name Components.

---

## `ecs_entity`

| field | |
| --- | --- |
| `entity` (request) | the Entity as `7v2`, `Entity(7v2)` or its decimal handle: whatever a log or an earlier answer gave |
| `entity` (response) | the Entity as `Entity.String` renders it |
| `components` | every Component it carries, sorted by name, each `{name, value, error}` |

**Values are the Component's exported fields as JSON.** An `m.List` is an
array, an `assets.Blob` is `{"len":N}` (its length, never its bytes), an Entity
Reference is its decimal handle, which `ecs_entity` accepts back, and an
`m.Maybe` is its value or `null` when absent. Unexported fields are not shown,
so a Component whose fields are all unexported shows as `{}`, and a Tag shows as
`{}`. A Component whose value could not be encoded (a NaN) carries `error`
instead, and that Component alone.

**Refusals**, as `mcp.Unavailable`: a malformed Entity, naming the three forms;
`NoEntity`; and an Entity that is not alive, whose reason names the Entity that
holds its index now, when one does. A handle to a free index is not alive,
including the one that index will carry next.

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

## What a call costs the frame

**Each read Command holds `write{*ecs.Entities}` and nothing else.** Every
handler that touches a Store holds `read{*ecs.Entities}`, so a call waits for
every ECS System running now to let go, and under Conflict-aware FIFO every ECS
System queued behind it waits until it returns. The stall is linear in the walk
plus the encodings, and for `ecs_query` the limit is what bounds it: 500 at
most. The prompt says so, and tells the agent to keep `limit` small in a busy
game and not to loop a call every tick.

**No frame's lock set changes.** They are Commands, not Systems: a frame nobody
reads from runs exactly the handlers and lock sets it ran before. A call needs
no tick and works while the game is paused, so the agent need not step the game
to look.

**What does change is visible in the contention report.**
`mcpserver_architecture`'s contention report
([#290](https://github.com/dvoyni/cog/issues/290)) lists the three Commands,
named as `kernel.TypeName` renders them — `ecs.censusCmd`, `ecs.entityCmd` and
`ecs.queryCmd` — as writers of `*ecs.Entities` conflicting with every ECS
System. Those pairs are these tools, not the game. Every other writer of
`*ecs.Entities`, `ecs.ShrinkCmd` or a System that spawns or despawns, is the
game's. The prompt text names the three, so an agent discounts exactly those
pairs; a test computes the names with `kernel.TypeName`, so a rename cannot
leave the prompt naming a type that is gone.

---

## The description prose

Reproduced in full, per the house style, so it is reviewed as prompt text. A
test checks that each string below is the Go constant, word for word.

> **`ecs_world`** — The game's ECS at a glance: every registered Component,
> named as `mcpserver_architecture` names types, with how many Entities carry
> it; how many Entities are alive; how many indices wait on the free list for
> reuse; and the index space, every index ever allocated. A free list or index
> space that keeps growing is churn or a leak. Call this first: the Component
> names it lists are how `ecs_entity` and `ecs_query` name Components. It
> changes nothing, needs no tick and works while the game is paused, so do not
> step the game just to look. Every call waits for the ECS Systems running now
> and holds up the ones queued behind it until it returns.
>
> In `mcpserver_architecture`'s contention report, `ecs.censusCmd`,
> `ecs.entityCmd` and `ecs.queryCmd` appear as writers of `*ecs.Entities`
> conflicting with every ECS System. Those pairs are these three tools, not the
> game: discount exactly them. Every other writer of `*ecs.Entities`, such as
> `ecs.ShrinkCmd` or a System that spawns or despawns, is the game's.

> **`ecs_entity`** — One Entity and every Component it carries, sorted by name,
> each with its value. Give the Entity in whatever form a log or an earlier
> answer printed it: `7v2`, `Entity(7v2)` or its decimal handle. A refused
> Entity is not alive: it was despawned, and when its index was reused the
> reason names the Entity that now holds it, so stop reasoning about the one you
> asked for.
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
> `ecs.entityCmd` and `ecs.queryCmd` appear as writers of `*ecs.Entities`
> conflicting with every ECS System. Those pairs are these three tools, not the
> game: discount exactly them. Every other writer of `*ecs.Entities`, such as
> `ecs.ShrinkCmd` or a System that spawns or despawns, is the game's.

The request and response fields carry `jsonschema` prose of their own, which
the agent reads in the tool's schema:

> **`entity`** — the Entity as 7v2 or Entity(7v2) or its decimal handle: any
> form a log or an earlier answer gave

> **`components`** — Component names spelled as kernel.TypeName renders them
> and as ecs_world lists them; an Entity must carry every one

> **`limit`** — how many matching Entities to return: omit or 0 for 50; at most
> 500

> **`total`** — how many Entities matched in all, including those not returned

> **`truncated`** — true when fewer Entities were returned than matched: the
> list is not the whole answer

> **`value`** — the Component's exported fields as JSON; unexported fields are
> not shown, and it is null when error is set

> **`error`** — present when the value could not be encoded as JSON, saying why

---

## Out of scope

- **Mutation**: spawning, despawning, setting or removing a Component. It is
  useful to an agent testing a game, and it is also the capability most able to
  corrupt a run in a way nobody can reproduce. A runtime write through
  `Entities` would also skip the ownership declaration a static `Spawn[S]`
  makes. It is a possible later ticket.
- **Any change to the read Commands' behaviour, locks, limits or encoding.**
  They are specified in [ecs.md](./ecs.md#reading-the-world-by-name).
- **Telling a never-dispatched call apart from an answer.** A zero response at
  shutdown is accepted.
- **Changes to the broker or the contention report.** The broker's `m.List`
  array schema is [#547](https://github.com/dvoyni/cog/issues/547); these tools
  carry values as `any` and do not need it.
- **Marshallers for Components with unexported fields.** The prompt states the
  limitation instead.
