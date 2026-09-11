# input as an mcp provider — specification

`input` offers an agent two capabilities: **`input_send`** to act and
**`input_state`** to look. Together they are the agent's only reach into the
game — there is no arbitrary command dispatch, by design.

The mechanism underneath is `input.Play` and `input.SynthesizeCmd`, which are
ordinary `input` features a test harness or a replay tool uses on the same
terms; they are specified in [input/docs/specs/input.md](./input.md). This
document specifies only what the agent sees, and reproduces the description
prose in full so it can be reviewed as prompt text.

The extension point is
[mcp/docs/specs/mcp.md](../../../mcp/docs/specs/mcp.md). Assembled from the
resolved tickets of
[An agent-facing extension point across cog](https://github.com/dvoyni/cog/issues/199).
Nothing is decided here — where a claim rests on something unverified, it is
marked **Gap** and says what would settle it.

---

## Contents

- [The provider](#the-provider) · [Two capabilities, not a family](#two-capabilities-not-a-family)
- [`input_send`](#input_send) · [`input_state`](#input_state)
- [Coordinates are window units](#coordinates-are-window-units)
- [Neither binds to a frame](#neither-binds-to-a-frame) · [Under pause](#under-pause)
- [The key that stays down](#the-key-that-stays-down)
- [The description prose](#the-description-prose)
- [Required input changes](#required-input-changes) · [Out of scope](#out-of-scope)

---

## The provider

`input` implements `mcp.Provider` itself.

```go
func (p *Plugin) Capabilities() []mcp.Capability {
	return []mcp.Capability{
		mcp.Func("send", sendDescription, p.send),
		mcp.Command[StateCmd, StateRequest, StateResponse]("state", stateDescription, mcp.ReadOnly()),
	}
}
```

The alternative — the broker dispatching `input.ApplyCmd` itself — means
`mcpserver` importing `input`, which is the knower the whole design keeps out.
Every package hosts its own provider.

**`input_send` is `mcp.Func`** over `input.Play`, a one-line adapter. It cannot
be `mcp.Command`, because the delay between batches must happen outside every
lock, and a command handler that slept would sleep under the `*State` write
lock.

**`input_state` is `mcp.Command`, with zero glue** — a read under the state's
read lock — and it is `mcp.ReadOnly()`, which is the deciding detail: read-only
capabilities get auto-approved by several clients, and *is `W` actually down?*
is worth asking cheaply and often.

**`input_send` is not `ReadOnly`.** It changes the game, which is its entire
purpose.

---

## Two capabilities, not a family

The provider contract anticipated an `input_press_key` among a family of
per-verb tools. **The family collapses to one tool plus a reader**, and that is
worth stating because it is this design's best answer to its own central risk.

The map's Notes flag tool selection — *can an agent pick the right capability* —
as the thing prior art cannot settle. Within `input` the risk is **zero**, not
mitigated: there is nothing to pick between. One tool does everything that acts,
one tool looks, and the two are different verbs rather than different arguments.

The names were chosen against that: `do` is vague in the way that makes an agent
hesitate, `play` suggests a recording, `sequence` names the argument rather than
the act. **`input_send` and `input_state`** read as act and observe, which is
the shape every other provider has.

---

## `input_send`

```go
type SynthesizeRequest struct {
	Actions []Action `json:"actions"`
}
```

One list of steps, applied in order. Seven step kinds — `key_down`, `key_up`,
`move`, `move_by`, `scroll`, `text`, `delay` — and one rule that produces every
idiom:

> **Consecutive steps with no `delay` between them land in the same tick.**

| sequence | what the game sees |
| --- | --- |
| `move, key_down, key_up` | a complete click in one tick |
| `key_down, delay 500, key_up` | a key held across ~30 ticks |
| `move, key_down, delay 100, move, key_up` | a drag |

Nothing is special-cased, and **`input_send` is already a batch**: a whole
sequence is one round trip. That is why a generic broker batch capability buys
nothing here — per-capability plurality won the argument once, and won it typed.

**Keys are names**, and `#<n>` is accepted because it is what an unnamed key
prints as. The schema is an `anyOf` of the name enum and `^#-?[0-9]+$`, so a
typo is rejected client-side rather than reaching the engine; see
[mcp §Types that cross as text](../../../mcp/docs/specs/mcp.md#types-that-cross-as-text).

**The caps are refusals, not truncations**: 10 s of total duration and 256 steps,
both checked before a single step runs, both `mcp.Unavailable`. Everything else
about validation, `move_by`, `text`, `scroll` and the batching rule is in
[input.md](./input.md).

**The response is `StateResponse`** — the down-set and the pointer — the same
type `input_state` returns. Every input capability answers with the same picture
of the seam.

Three shapes for the response were rejected: **an empty ack**, which tells an
agent nothing; **"applied at tick N"**, which cog cannot give, having no frame
number anywhere; and **waiting for a tick to consume the input**, which is
unnecessary — see below.

---

## `input_state`

```go
type StateRequest struct{}
```

Everything an agent *does* fits one tool, but there is a second thing it wants:
**to ask what is held without pressing anything.**

`input_send`'s response covers an agent that just acted — not a fresh session, a
second agent, or one recovering from a hang-up. An empty step list would
technically answer it, but a tool named for doing being used for asking is bad
prose, and it would carry `input_send`'s approval prompt.

It is also the one debug question no Capture and no Snapshot answers. Without
it, `input` would be the only write-only provider in the family.

---

## Coordinates are window units

**Window units, no `space` field, no conversion.**

Three spaces are live — framebuffer pixels (what a Capture writes), viewport
units (what `ui` rects are in), and window/DIP units (what `input.Pos` is). On a
1:1 desktop all three coincide, so a wrong choice works in dev and breaks on a
HiDPI laptop.

`input` **cannot** convert: `app.Viewport` is gfx's resource and
`input.Dependencies()` is `nil`. The two routes that would let it are both worse
than the arithmetic — `input` depending on `gfx` inverts the import graph for a
debug facility, and having `wgpu` push the window size into an `input` resource
duplicates `app.Viewport` and puts a new obligation on the driver contract.

**The agent converts, using numbers it already holds**, and the description says
the division outright:

- from a capture: `window = pixel × WindowWidth / PixelWidth`
- from a ui rect: `window = viewport × WindowWidth / Width`

The second is why **every snapshot response carries the logical viewport size**
and the capture response does not. The capture response omits it deliberately —
it is the game's own sizing policy and means nothing to an agent looking at a
picture — and a ui snapshot is the case that inverts the argument, because its
coordinates *are* in that space. Without it, *find the button, click the button*
breaks, and breaks silently on a 1:1 display. See
[ui §Three coordinate spaces](../../../ui/docs/specs/mcp.md#three-coordinate-spaces).

---

## Neither binds to a frame

**`input_send` is the first capability in this family that binds to no frame**,
and saying so is what keeps
[mcp §Arm-then-wait](../../../mcp/docs/specs/mcp.md#arm-then-wait) a statement
about frame-bound capabilities rather than a house style.

It does not need to wait, and **should not**, because a capture binds to a tick
that *began after* its request and the press's dispatch returned before that.
So:

- *"capture the frame after this input"* is not a feature either side has to
  build. **Press-then-look is correct for free.**
- *"what does the agent get back, and after how many frames?"* has no second
  half. There is no wait to count.

`input_state` binds to no frame either, obviously; so does
`mcpserver_architecture`. Three of eight capabilities are frame-free, which is
the proportion that makes the discipline worth writing down separately.

---

## Under pause

**A `delay` separates nothing while the engine is paused.**

`state.advance()` promotes `pend*` into `just*` only on a tick
(`input/state.go:65`), so a `key_down` and a `key_up` issued inside a pause both
land on the same tick and arrive as `JustPressed && JustReleased` together, with
`Pressed()` never observing true.

**This is documented, not refused.** The behaviour is correct — nothing ticked,
so nothing was held — and refusing would block the legitimate recipe:

```
input_send key_down  ->  wgpu_time step 1  ->  input_send key_up  ->  wgpu_time step 1
```

which holds a key for **exactly one tick**, something no running engine can
offer. It strengthens rather than contradicts the running-engine non-guarantee
that a delay shorter than a frame may not separate ticks: under pause, no delay
separates ticks at all.

Both capabilities work normally while paused in every other respect, because the
HTTP goroutines and the scheduler's coordinator are independent of the game
loop.

---

## The key that stays down

The contract has no lifecycle: **nothing is undone when an agent's session
ends.** For an overlay that would have been mild, because an overlay is visible.
**A held synthetic key is the counter-case**, and `input` owns the problem:

- A press with no matching release leaves the key held in `input.State`
  indefinitely, and by decision nothing will clear it — not the broker, not the
  session, not the engine.
- If the agent disconnects between the two, the game is left walking into a wall
  with nothing on screen to say why.

The remedies are the three above, and they are the whole answer:

1. **The 10 s duration cap**, which bounds the window in which a sequence can be
   orphaned mid-hold. It sits far under the broker's 30 s, so the deadline can
   only be reached by the agent hanging up.
2. **The down-set on every response**, so an agent that leaks a key sees it on
   its next call.
3. **`input_state`**, so a *different* agent, or the same one after a reconnect,
   can find it without pressing anything.

**Unwinding is not implementable**, which is why it is ruled out rather than
skipped: a capability body holds only the request-bound executioner, so once
that context is dead there is nothing to dispatch a release with.

---

## The description prose

Reproduced in full, per the house style, so it is reviewed as prompt text.

> **`input_send`** — Send input to the game as a list of steps applied in order.
> Steps with no `delay` between them land in the same tick: `move`, `key_down`,
> `key_up` in one call is a complete click. A `delay` between `key_down` and
> `key_up` is a held press, and a `move` between them is a drag. Coordinates are
> window units, not image pixels — a capture returns both its pixel size and the
> window size, so divide one by the other to turn a point you can see into a
> point you can click. Keys are names: `w`, `escape`, `space`, `mouse_left`.
> `text` types characters and does **not** press keys; a game that reads keys
> needs `key_down`/`key_up`. Nothing is undone when you disconnect: a key you
> press stays down until something releases it, and the response tells you what
> is currently held.
>
> While the game is paused a `delay` separates nothing, because no tick runs —
> to hold a key for exactly one tick, send `key_down`, step once, send `key_up`,
> step again.

> **`input_state`** — What the input seam holds right now: every key and mouse
> button that is down, and where the pointer is, in window units. Changes
> nothing. Use it to check whether the game is really seeing a key you are
> holding, or to find a key left down by an earlier call.

On `#<n>`, in the `key` field's own description:

> A key name such as `w`, `escape` or `mouse_left`. `#<number>` also works and
> is how an unnamed key is reported back, but the number is cog's own key code
> and is **not** ASCII or a browser keyCode — `#13` is the letter **M**, and
> Enter is `enter` (`#84`). Use names.

**Gap.** These three strings are the part of this family most likely to be
wrong, and nothing in-repo can tell. No vendor publishes tool-selection failure
rates, and both Anthropic and Microsoft answer the question with *evaluate it on
your own task*. The first implementation issue owns finding out whether an agent
reaches for the right one, and these strings are meant to be revised against a
real transcript without touching a design decision.

---

## Required input changes

The feature half is in
[input.md](./input.md#required-input-changes); these are the provider's own.

**`input/mcpprovider.go`** (new)

- `Capabilities()` returning the two capabilities above.
- The `input_send` body: `input.Play(k, req.Actions)`, and nothing else.
- Both description strings, kept beside the types and reproduced above.

**Tests**

- `input_state` is annotated read-only and `input_send` is not.
- A refused sequence leaves the down-set unchanged, asserted through
  `input_state` rather than through internals.

**`CONTEXT.md`** — already applied.

---

## Out of scope

- **Exclusivity between a human and an agent**, and any provenance flag marking
  a change as synthetic. Both are in
  [input.md](./input.md#a-human-and-an-agent-at-once), and the second would
  destroy the premise: a game reacting exactly as it would to a person is the
  whole reason this seam was chosen over command dispatch.

- **Arbitrary command dispatch by type name.** The agent reaches the game here
  or not at all. This is the load-bearing scope decision behind the entire
  design: it would put an untyped, out-of-band caller inside an engine whose
  premise is typed contracts and statically-validated locks.

- **Pointer lock and mouse-look**, per
  [input.md](./input.md#out-of-scope) and
  [#233](https://github.com/dvoyni/cog/issues/233). The first implementation
  skips it.
