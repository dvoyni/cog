# canvas as an mcp provider — specification

`canvas` offers an agent one capability: **`canvas_draws`**, one tick's recorded
draw ops, rendered as JSON while they are still alive.

It answers one question — *nothing is on screen; was it even recorded?* — and it
answers it about the app's drawing **and ui's**, because `ui` records into the
canvas queue during its own processing (`ui/plugin.go:80`). That is why
`canvas_draws` and `ui_layout` are complementary rather than redundant:
`ui_layout` says what ui intended, `canvas_draws` says what it emitted.

The extension point is
[mcp/docs/specs/mcp.md](../../../mcp/docs/specs/mcp.md). Assembled from the
resolved tickets of
[An agent-facing extension point across cog](https://github.com/dvoyni/cog/issues/199);
every section cites the tickets it came from. Nothing is decided here — where a
claim rests on something unverified, it is marked **Gap** and says what would
settle it.

---

## Contents

- [Vocabulary](#vocabulary) · [The provider](#the-provider)
- [Why there is no synchronous read](#why-there-is-no-synchronous-read)
- [Serialize, do not copy](#serialize-do-not-copy)
- [Where it sits in the tick](#where-it-sits-in-the-tick)
- [The request](#the-request) · [The response](#the-response)
- [Vertices](#vertices) · [Under pause](#under-pause)
- [The description prose](#the-description-prose)
- [Required canvas changes](#required-canvas-changes) ·
  [Out of scope](#out-of-scope)

---

## Vocabulary

**Snapshot** — one tick's recorded declarations, rendered while they are still
alive. It is in `CONTEXT.md`, and the definition is load-bearing in a way the
name hides: *it is not a copy of a queue.* No queue outlives the tick that
filled it, so a Snapshot is produced **inside** one and shaped by the request
that asked for it.

A **Capture** is the pixels; a **Snapshot** is what produced them, and the two
are meant to name one moment. The glossary says *meant to*, deliberately: the
timing is a spec rule under
[mcp §Pairing a moment](../../../mcp/docs/specs/mcp.md#pairing-a-moment), not a
property of the terms, and a glossary carrying a guarantee will drift the moment
the guarantee does.

---

## The provider

`canvas` implements `mcp.Provider` itself.

```go
func (p *Plugin) Capabilities() []mcp.Capability {
	return []mcp.Capability{
		mcp.Func("draws", drawsDescription, p.draws, mcp.ReadOnly()),
	}
}
```

`mcp.Func`, because a snapshot arms and waits. `mcp.ReadOnly()`, because it
changes nothing in the game — with the one asterisk that under pause it costs a
step, which is stated in the description rather than expressed in the
annotation.

> **Amended at implementation ([#254](https://github.com/dvoyni/cog/issues/254)).**
> The body is the package function `drawsSnapshot`, not the method `p.draws`
> the sketch above spells. A method puts provider state one dereference from a
> body the capability-body rule forbids to touch it; a package function keeps
> that rule visible at the call site, which is why `gfx` writes its two bodies
> the same way. Nothing else about the registration changes.

It follows
[mcp §Arm-then-wait](../../../mcp/docs/specs/mcp.md#arm-then-wait) and does not
restate it.

---

## Why there is no synchronous read

A capability that reads the queue *now* does not exist, and this document says
why rather than leaving it as an omission — because "read the queue" is the
obvious design and it is wrong for a reason that is easy to miss.

**It is not that the answer would be stale. Stale would be usable.** The canvas
queue is **empty** between ticks: `defer write.reset()` runs inside the flush
handler (`canvas/plugin.go:105`), during `app.UpdateEvent`, long before
`app.RenderEvent`. And an agent's dispatch runs on the HTTP goroutine,
serialized against the game only by locks, so a synchronous read returns an
empty queue between ticks and a **partially recorded** one if it slips between
two of the app's own recording handlers.

A tool that returns an empty array most of the time and a random prefix the rest
of the time is worse than no tool.

So a Snapshot is **armed**, exactly as a Capture is, and inherits the guarantee
verbatim:

> A snapshot shows the game as of a tick that began after the request was made.

That makes press-then-snapshot correct with no composition mechanism, and it
makes a Capture and a Snapshot armed in the same turn describe the same tick —
provably under pause, as best effort otherwise.

---

## Serialize, do not copy

The fork was: serialize in-tick, or deep-copy in-tick and render later on the
broker's goroutine.

`canvas.Op` **is** copyable — strings, `[]ParameterDescr`, `[]Vertex` — so for
canvas alone the copy was available. It was rejected anyway, and not because of
canvas: a ui node cannot be copied at all (`Element.userData` is `any`), and two
mechanisms for one capability shape, in a spec family that shares its prose, is
worse than one mechanism that works everywhere.

The deciding point is that the copy's advantage — deciding the output format
late, once the agent's request is known — **buys nothing here**, because the
request is already known when the subscriber runs. The filter travels with the
arm.

There is one canvas-specific trap that a copy would have had to solve anyway:
**`opQueue.Ops(dst []Op) []Op` aliases the queue's storage**, documented as
valid "until the queue is reset or recorded into again" (`canvas/inspect.go:72`).
Anything outliving the tick must deep-copy. Serializing in-tick sidesteps it
entirely — the bytes are produced while the alias is still valid, and nothing
escapes.

The map's Notes licence the remaining cost: this is a debug facility and it may
be wasteful. Allocating to build a JSON document inside a tick is affordable;
what is not affordable is stalling a thread the game needs. The in-tick work is
bounded by the filter and does no I/O — **the write happens on the broker's
goroutine, never here.**

---

## Where it sits in the tick

```
.Last().Before[canvas.UpdateEventHandler]()
```

Reads `Read[*canvas.OpQueue]` before `flushFrame`'s `defer write.reset()`. It
sees the app's recording **and** ui's, because ui runs in the earlier phase.

This is one link in the chain the three snapshots share, and each link is
derived rather than chosen:

| capability | ordering | reads |
| --- | --- | --- |
| `ui_layout` | `.After[ui.UpdateEventHandler]()` | `processor.nodes` |
| `canvas_draws` | `.Last().Before[canvas.UpdateEventHandler]()` | `Read[*canvas.OpQueue]` |
| `gfx_frame` | inside `gfx`'s own `present`, before the queue swap — see below | `*gfx.OpQueue`, `Read[*gfx.ResourceQueue]` |

Each is a `Read` handle against a resource its own package already owns or
already depends on, so no coupling is created that composition did not already
have.

> **Amended at implementation ([#253](https://github.com/dvoyni/cog/issues/253)).**
> The `gfx_frame` row read
> `.Last().After[canvas.UpdateEventHandler]().Before[gfx.UpdateEventHandler]()`
> until #253 built it. That expression cannot be written from `gfx`: `canvas`
> imports `gfx`, so `gfx` cannot name `canvas.UpdateEventHandler`, and a second
> `Last` subscriber would only conflict with canvas's flush on the queue rather
> than order against it. `gfx` takes the snapshot inside `presentOnUpdate`
> instead, immediately before the swap — the same point in the frame, reached
> from the other side, since canvas's own
> `Before[gfx.UpdateEventHandler]()` already puts the flush ahead of it. See
> [gfx §Where it sits in the tick](../../../gfx/docs/specs/mcp.md#where-it-sits-in-the-tick).
>
> **The other two rows are unaffected and stay exactly as written**, because
> each names a handler type its own package declares.

> **Amended at implementation ([#254](https://github.com/dvoyni/cog/issues/254)).**
> The row above is the link that *takes* the snapshot, and it is implemented
> exactly as written — `canvas.DrawsUpdateEventHandler`, `.Last()`,
> `.Before[canvas.UpdateEventHandler]()`, reading `Read[*canvas.OpQueue]`.
>
> It is not the only subscription the capability needs. Arming inherits
> [mcp §Arm-then-wait](../../../mcp/docs/specs/mcp.md#arm-then-wait) verbatim —
> *a snapshot shows the game as of a tick that began after the request* — and a
> request landing inside a tick that has already recorded cannot be told from
> one that arrived between ticks by anything running at the end of the tick. So
> canvas also registers `DrawsArmUpdateEventHandler`, ordered `First()` and
> declaring no resources, which admits a waiting request to the tick that has
> just begun. It is the same two-stage slot `gfx` builds for the same reason
> (`gfx/snapshot.go`), and it is a mutex-guarded no-op when nothing is armed.

---

## The request

```go
type DrawsRequest struct {
	Path      string   `json:"path,omitempty"`      // absolute, ending in .json
	FromLayer *int     `json:"fromLayer,omitempty"`
	ToLayer   *int     `json:"toLayer,omitempty"`
	Kinds     []string `json:"kinds,omitempty"`     // op kinds to include
	Vertices  []int    `json:"vertices,omitempty"`  // op indices to expand in full
}
```

**`Path` is optional**, per the family's delivery contract: omit it and the JSON
comes back inline, supply it and a greppable file is written. A small frame is
better inline; a triangle-heavy one is better at a path where `jq` can reach it.
An oversized *text* result would be spilled to a file by the client anyway, at
an unpredictable threshold and under a name the agent did not choose — so a
large dump becomes a file either way, and the only question is whether cog
controls it.

**The filter travels with the arm**, which is what bounds the in-tick
serialization. Layer range and op kind are the two axes a busy frame actually
needs, and `Vertices` is the drill-down described below.

---

## The response

Flat JSON, always, with **source indices**: an op's index is its position in
**canvas flush order**, not its position in the emitted array. Filtering makes
the emitted array a subset, and if indices were positions in that subset, a
`Vertices` drill-down would name a different op on the second call. With source
indices no addressing scheme needs inventing — **the index is the address** —
and an elided op stays addressable for free.

Each op carries `canvas.Op`'s fields (`canvas/inspect.go:24-52`) **minus the gfx
descriptors, plus their resolutions**:

- **Texture** as `Path()` when it has one, or the baked `ID()` when it does not.
- **Parameters** as a `ParameterView` carrying name, kind, and **the live value
  only** — never the dead half of the union, and never inline pixel data. The
  view types are declared by `gfx` and embedded here, so one value never appears
  in two shapes across two tools; see
  [gfx §The view types](../../../gfx/docs/specs/mcp.md#the-view-types).
- canvas's own types — `SpriteTransform`, `TextDraw`, `Vertex` — marshal
  directly and need no view.

> **Amended at implementation ([#254](https://github.com/dvoyni/cog/issues/254)).**
> The third bullet is false, and it is false in the two ways the view types
> exist to prevent.
>
> `SpriteTransform.Filter` is a `gfx.FilterMode` and `TextDraw.Align` a
> `TextAlign`; marshalled directly, each is an **ordinal** — an enum reported
> as `1` is a lookup an agent cannot perform, and every enum in this family is
> a name. Worse, `TextDraw` carries `Material *gfx.MaterialDescr` and
> `Params []gfx.ParameterDescr`, and every gfx descriptor has entirely
> unexported fields, so marshalling one yields `{}`: a text draw would publish
> an empty object where its material is and a row of empty objects where its
> parameters are, which is precisely the dead end
> [gfx §The view types](../../../gfx/docs/specs/mcp.md#the-view-types)
> documents. `Vertex` marshals without lying but with `X`/`Y`/`R` keys, against
> a document that is camelCase throughout.
>
> The repair is the one `gfx` already made: canvas declares
> `SpriteTransformView`, `SpriteFrameView`, `TextDrawView`, `VertexView` and
> `RectView` in `canvas/snapshot.go`, built through the same rules — one value
> per union arm, every enum a name, `unknown(n)` for a member no table knows,
> and vectors as the component arrays `ParameterView` already uses for numbers.
> `TextDrawView` carries neither the draw's material nor its parameters,
> because the op reports both already.
>
> The alternative — json tags on `m.Vec2`, `m.Rect` and `m.Color` — was
> rejected for the reason [#253](https://github.com/dvoyni/cog/issues/253)
> rejected it: `m` is the math package every cog app uses, and one debug
> document is not a reason to fix its wire shape.
>
> Two additions come with it, both consistent with *minus the gfx descriptors,
> plus their resolutions*. An op reports the **material it named**, through
> `gfx.MaterialViewOf` — the tool's own description promises materials, and a
> full-screen material is one of the ways a frame ends up blank. And a triangle
> op reports **`vertexBytes`** beside its count, because a list recorded with a
> custom vertex layout has no positions canvas can read, and a count of zero
> with nothing beside it reads as a list that recorded nothing.
>
> `canvas.Op` gained `Material`, `HasTexture` and `VertexBytes` to serve this,
> as ordinary inspection API rather than an agent back door, and
> `gfx.TargetDescr.Texture()` and `gfx.FilterModeName` were added for the same
> reason: a layer's target is a gfx handle canvas passes through untouched, so
> reporting where a layer draws means reading it back out.

**Per layer**, and not optional, because without them the world-space
coordinates in an op mean nothing:

- the resolved **window and aspect mode** (`LayerWindow`,
  `canvas/inspect.go:91`),
- the layer's **target** and **clear** (`LayerTarget`, `LayerClear`).

**The viewport block**, on every snapshot response: `PixelWidth`,
`PixelHeight`, `WindowWidth`, `WindowHeight`, **and the logical viewport width
and height**. All six, and the sixth is the one that is easy to leave out and
breaks the flow silently — see
[ui/docs/specs/mcp.md §Three coordinate spaces](../../../ui/docs/specs/mcp.md#three-coordinate-spaces).

**Whether a step was performed**, when the engine was paused.

**What the filter omitted**, named rather than implied. Whatever a capability
omits, it says it omitted it and offers a way to reach the rest.

---

## Vertices

`Vertices []Vertex` is the one field that would otherwise swamp every response.
A triangle-heavy frame is tens of thousands of them, and nobody debugs by
reading coordinates.

**Summarised by default** — count plus bounding box — **with the full list
returned only when the request names that op's index** in `Vertices`.

That is *reference over value* applied to the one field that needs it, and it is
the reason source indices are load-bearing rather than tidy: the summary tells
the agent an op's index, and the index is what the second call passes back.

---

## Under pause

**Arming `canvas_draws` while the engine is paused performs exactly one step, or
joins one already pending, and the response reports it.**

It cannot be otherwise. A Snapshot is one tick's recorded declarations rendered
while they are alive, and the queue is *empty* between ticks rather than stale,
so producing one without running a tick is not a thing that exists.

The alternatives were **refusing** with `mcp.Unavailable` — which would make a
snapshot unreachable under pause, since a blocking arm cannot ask the agent to
step for it — and **waiting** for a step that may never come, which is a
guaranteed deadline expiry. Stepping implicitly and saying so is the only option
that leaves the capability usable and the agent's model of the world correct.

**Joining a pending step** is what makes the pairing recipe work: three
concurrent arms share one step and land on one tick, instead of taking three
steps onto three different ticks. See
[mcp §Pairing a moment](../../../mcp/docs/specs/mcp.md#pairing-a-moment).

Note the contrast with a Capture, which under pause costs **no** tick. The two
are frame-bound for different reasons, and pause is where that difference first
shows.

---

## The description prose

Reproduced in full, per the house style, so it is reviewed as prompt text:

> Everything drawn into the 2D canvas during one tick, in the order it was
> recorded: sprites, text, shapes and textures, with their layers, transforms,
> materials and resolved parameters. This includes what the UI drew, since UI
> elements record into the same queue — use it when something should be on
> screen and is not, to find out whether it was ever recorded at all, and on
> which layer.
>
> Blocks until the next tick has been recorded, so it reflects anything you did
> before calling it. Each op carries its index in record order; that index is
> stable under filtering and is what you pass back. Triangle vertices are
> summarised as a count and a bounding box — pass an op's index in `vertices` to
> get the full list for that op. Filter by `fromLayer`/`toLayer` and `kinds` to
> cut a busy frame down, and pass `path` to write the JSON to a file instead of
> returning it inline.
>
> Every response reports the three coordinate spaces — image pixels, window
> units and the logical viewport — because canvas coordinates are in a layer's
> own world window, which is also reported per layer.
>
> While the game is paused this performs one step to have something to record,
> and says so in the response. Arm it together with `ui_layout` and `gfx_frame`
> to describe one moment: they share that single step. Take `gfx_capture` last.

---

## Required canvas changes

A checklist for an implementation session.

**`canvas/mcpprovider.go`** (new)

- `Capabilities()` returning the one capability.
- `DrawsRequest`/`DrawsResponse`, and the `Func` body: validate, dispatch the
  arm, wait with the capability's own **2s** deadline, then marshal and write on
  **this** goroutine.
- The description string, kept beside the types and reproduced above.

**`canvas/snapshot.go`** (new)

- The subscriber and its ordering, the pending-request slot, and the in-tick
  serialization into the view types.
- One arm in flight: a second `canvas_draws` is refused in words while one is
  pending. `canvas_draws`, `ui_layout`, `gfx_frame` and a capture may all be in
  flight **together** — they are separate flags filled during one tick, and
  refusing them would destroy the pairing.

**`canvas/inspect.go`**

- Whatever accessors the serialization needs that are not already there. The
  aliasing contract at `:72` is unchanged and must stay documented, because the
  next reader will wonder why the snapshot does not simply call `Ops`.

**Tests**

- A snapshot armed between ticks returns the *next* tick's ops, not an empty
  array.
- A filtered response's op indices match the unfiltered response's.
- A `vertices` drill-down returns the vertices of the op the index named, after
  a filter that removed earlier ops.
- A texture parameter does not put inline pixels in the response.

**`canvas/README.md`** — a pointer to this document, worded so a reader knows
what is specified and what is implemented.

**`CONTEXT.md`** — already applied. **Snapshot** is defined and has been amended
once, to stop stating a timing guarantee the terms do not own.

---

## Out of scope

- **A canvas atlas capture.** Reading back the glyph or sprite atlas is a real
  want and exactly the sort of thing that is wrong in a way a screenshot cannot
  show — but it has no frame to wait for, it is a bake-time artifact, and the
  honest answer is probably to read the CPU-side pixels that produced it, which
  never left main memory. If it survives, it belongs here as its own capability
  rather than as a widening of `gfx`'s readback. See
  [gfx/docs/specs/capture.md §Out of scope](../../../gfx/docs/specs/capture.md#out-of-scope).

- **Pagination.** Flat sequences are the shape that *does* paginate in prior art
  — page size, page index, fetch-by-index — and cog answers with a filter plus a
  file instead. The filter bounds in-tick work, which pagination would not; and
  a paged flat sequence over a queue that no longer exists on the second call
  would need a generation-stamped handle, which is machinery this design does
  not have and does not want. If the filter proves insufficient in use, a
  generation-stamped page cursor is the shape to reach for, and prior art has
  already worked out what it costs.
