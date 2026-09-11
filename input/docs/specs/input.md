# input synthesis — specification

`github.com/dvoyni/cog/input` gains a way to play a scripted sequence of input
into the engine: `input.Action`, `input.SynthesizeCmd` and `input.Play`, plus a
name table for `input.Key`.

**This is a first-class `input` feature, not an MCP back door.** Tests, replays
and demos want the identical mechanism, and writing it as package API rather
than as provider-only code is the honest justification for putting it in a lean
contract package. The agent-facing capabilities that wrap it are specified in
[input/docs/specs/mcp.md](./mcp.md), and they are a one-line adapter over
`Play`.

Assembled from
[input synthesis: what the agent may press](https://github.com/dvoyni/cog/issues/209),
resolved on the map
[An agent-facing extension point across cog](https://github.com/dvoyni/cog/issues/199).
Nothing is decided here — where a claim rests on something unverified, it is
marked **Gap** and says what would settle it.

---

## Contents

- [Vocabulary](#vocabulary) · [Facts this rests on](#facts-this-rests-on)
- [`Action`](#action) · [`SynthesizeCmd`](#synthesizecmd) · [`Play`](#play)
- [The batching rule](#the-batching-rule) · [Delay](#delay)
- [The caps, and the key that stays down](#the-caps-and-the-key-that-stays-down)
- [Nothing fails halfway](#nothing-fails-halfway)
- [Naming a key](#naming-a-key) · [`text` is text, not keys](#text-is-text-not-keys)
- [`move_by`](#move_by) · [A human and an agent at once](#a-human-and-an-agent-at-once)
- [Required input changes](#required-input-changes) · [Out of scope](#out-of-scope)

---

## Vocabulary

**Synthetic input** — input sent through the same seam a person's input arrives
on: folded into the polled input state and published as the same discrete
events, carrying no mark that distinguishes it and no lifetime of its own. A key
something presses stays down until something releases it. It is in `CONTEXT.md`.

*Injection*, *simulated input* and *fake input* are the words to avoid. The
first is avoided most deliberately: the whole point is that this is **not** a
parallel channel.

*Sequence*, *step* and *action* get no glossary entries. They are request
shapes, not domain concepts.

---

## Facts this rests on

- **`input.ApplyCmd` is already the right shape.** `ApplyRequest{Changes []Change}`
  folds a batch under one write lock (`input/commands.go:7-12`) and publishes
  one discrete event per change (`input/commandsimpl.go:7-19`).
- **`Change` cannot cross a protocol.** Every field is unexported
  (`input/contract.go:205-213`), so `ApplyRequest` reflects to a schema with no
  properties. A raw mirror of `ApplyCmd` is not expressible as an agent-facing
  request at all.
- **Mouse buttons *are* keys.** `input.Key` unifies them "so a single
  `Pressed`/`JustPressed`/`JustReleased` path covers both"
  (`input/contract.go:18-22`). There is no separate mouse verb to design.
- **A held key is free.** `down` is live and persists until a release arrives.
  Nothing expires it.
- **A same-tick press is invisible to `Pressed`.** `state.apply` adds then
  deletes from `down`, setting both `pendPressed` and `pendReleased`
  (`input/state.go:38-52`). `JustPressed` and `JustReleased` both fire,
  `Pressed` is never true, and `capturedSet`-driven "button looks pushed"
  visuals never render.
- **…but a same-tick click already works for `ui`.** `processInteractions`
  emits all downs then all ups from one tick's edges, and a down plus up on the
  same target in one tick produces `InteractionDown`, `InteractionUp` **and**
  `InteractionClick` (`ui/layout.go:876-897`). The pointer is live, so one batch
  is a complete click.
- **`Mods` is carried, never derived.** `KeyChange(k, mods, down)` takes mods
  from the driver and `state` does not track them, so a chord built from two
  calls publishes `KeyEvent{Mods: 0}` while the modifier is held.
- **`Key` has no name.** No `String()`, no table anywhere, and the constant
  blocks have deliberate gaps — letters use 1–26 of `[1..31]`.
- **`input` cannot convert coordinates.** `app.Viewport` is gfx's resource,
  `checkCoupling` demands a declared dependency to lock a foreign one
  (`kernel/registrar.go:211-235`), and `input.Dependencies()` is `nil`.
- **Nothing in cog consumes `KeyEvent`, `TextEvent` or `State.Text()`**, and
  nothing consumes `ScrollChange`. `ui` reads pointer state only.
- **The input seam has no owner token.** `wgpu` is the only dispatcher today,
  once per frame at the start of `onUpdate` (`wgpu/input.go:98-105`), and there
  is no once-per-frame guard and no rejection path. `Uses[input.ApplyCmd]`
  couples a caller to the command but **not** to input's resources
  (`kernel/registrar.go:113-116`), so two sources serialize rather than
  conflict. Per-tick edges roll in `handleUpdateEvent`, registered `.First()`
  (`input/plugin.go:29`).

---

## `Action`

One sequence, seven step kinds, structured rather than a string:

```go
type ActionKind string

const (
	ActionKeyDown ActionKind = "key_down"
	ActionKeyUp   ActionKind = "key_up"
	ActionMove    ActionKind = "move"
	ActionMoveBy  ActionKind = "move_by"
	ActionScroll  ActionKind = "scroll"
	ActionText    ActionKind = "text"
	ActionDelay   ActionKind = "delay"
)

// Action is one step of a synthetic input sequence.
type Action struct {
	Do     ActionKind `json:"do"`
	X, Y   float64    `json:"x,omitempty"`
	Dx, Dy float64    `json:"dx,omitempty"`
	Key    Key        `json:"key,omitempty"`
	Text   string     `json:"text,omitempty"`
	Ms     int        `json:"ms,omitempty"`
}
```

**`Action`'s fields are exported, unlike `Change`'s — a deliberate divergence
inside one package.** A `Change` is a driver's internal delta, constructed and
never inspected; an `Action` is a script, written by something outside the
process. The `omitempty` fields are the tagged union flattened, which keeps the
JSON schema a single object rather than a `oneOf`.

**A string mini-language was rejected.** `"mouse_move:123,4;mouse_down:1;delay:5000;mouse_up:1"`
is roughly 4× cheaper in tokens and models emit line-oriented syntax happily,
but it costs the schema: the key enum stops being machine-checkable, so a bad
name becomes a runtime parse error instead of a client-side rejection; it means
owning a parser with an escaping problem (`text:hello;world` is one step or two
depending on quoting rules that would have to be invented); and it nests a
second protocol inside the first. **The instinct it came from — one call, not
four — is the part that mattered, and the step list keeps it.**

**Compound steps were rejected.** `click{x,y,button}` and `press{key,hold_ms}`
are the two common cases, but with them there are two spellings of a click and
the caller must choose — the selection risk moved inside the tool rather than
removed. Both idioms go in the description verbatim instead.

The counter-argument is recorded rather than buried: the primitive spelling
invites the same-tick press trap above, and a caller that writes
`key_down, key_up` with no delay gets a press no `Pressed`-polling game will
see. **Showing the delay is how that is answered** — a caller that understands
why a press spans ticks is the one that can also write a drag, which no compound
covers.

---

## `SynthesizeCmd`

```go
type SynthesizeCmd kernel.Command[SynthesizeRequest, StateResponse]

type SynthesizeRequest struct {
	Actions []Action `json:"actions"`
}

type StateResponse struct {
	Down    []Key `json:"down"`    // sorted; names where named, "#<n>" otherwise
	Pointer Pos   `json:"pointer"` // window units
}
```

A plain function could build `[]Change` and dispatch the existing `ApplyCmd` — it
is pure computation over no state. A **new command whose handler holds the write
lock** does three things that route cannot:

- **Derives `Mods` from the live down-set**, so a synthetic Ctrl+S reports
  `KeyEvent{Key: S, Mods: ModCtrl}` instead of lying with zero. Mods are derived
  **after** folding the change, so pressing Shift reports `ModShift` and
  `Pressed`/`Mods` never disagree. Upstream platforms are inconsistent about
  this for the modifier's own press event; cog picks the self-consistent reading
  and states it.
- **Resolves `move_by`**, which needs the current pointer.
- **Returns the resulting state** from the lock it already holds.

The handler takes the `*State` write lock once, folds every action into a
`Change`, applies and publishes exactly as `applyCmdImpl` does, and returns
`StateResponse`. **It receives no `delay`** — see below.

`StateResponse` is also what `StateCmd` returns, deliberately: **every input
capability answers with the same picture of the seam.** The down-set is sorted,
because map iteration is not.

---

## `Play`

```go
// Play validates, splits and dispatches a synthetic input sequence, waiting
// between batches. It holds no locks; each batch is one SynthesizeCmd.
func Play(k kernel.Executioner, actions []Action) (StateResponse, error)
```

**The split cannot happen inside the command.** A handler that slept would sleep
under the `*State` write lock, stalling every tick for the delay's duration —
the one thing this whole effort's Notes rule out. So the splitting, validation
and waiting live in an exported plain function, and the agent-facing capability
is a one-line adapter over it.

**The wait is a `select` on `k.Context().Done()`, not a `time.Sleep`.** That is
what makes the broker's "the timeout bounds the wait" true rather than
aspirational: a caller that hangs up stops the sequence at the next delay
instead of running it out.

---

## The batching rule

**Consecutive steps with no `delay` between them fold into one `SynthesizeCmd`
dispatch** — one batch, one lock hold, one tick's worth of edges. A `delay`
splits the sequence into another dispatch, with the wait **between** them,
outside every lock.

That single rule produces all three cases without any of them being special:

| sequence | what the game sees |
| --- | --- |
| `move, key_down, key_up` | a complete click in one tick |
| `key_down, delay 500, key_up` | a key held across ~30 ticks |
| `move, key_down, delay 100, move, key_up` | a drag |

**One batch folds under one lock hold**, so a human's mouse move cannot land
between a `move` and its `key_down`. That is the only atomicity guarantee here,
and it is the one that matters.

---

## Delay

**Milliseconds, wall-clock, not ticks.**

Ticks would be exact and are not reachable: a plain function holds no locks and
is not a subscriber, so counting ticks needs either arm-then-wait machinery or a
poll loop racing a 16 ms frame. Milliseconds are also what a human tuning a hold
thinks in.

The cost is stated rather than discovered:

> **A delay shorter than a frame may not separate ticks.**

At 30 fps a `delay: 5` can put both batches between the same pair of ticks,
folding the down and the up into one tick — the hold silently did not happen.

**Under pause, no delay separates anything at all**, because `state.advance()`
(`input/state.go:65`) promotes `pend*` into `just*` only on a tick. This is
documented rather than refused, because refusing would block the recipe that
makes pause worth having:

```
key_down  ->  step 1  ->  key_up  ->  step 1
```

which holds a key for **exactly one tick**, something no running engine can
offer.

Frame-exact control is the thing milliseconds cannot give, and stepping is where
it comes from; see
[wgpu/docs/specs/mcp.md](../../../wgpu/docs/specs/mcp.md).

---

## The caps, and the key that stays down

If a sequence outlives its caller's deadline, the dispatch after the wait fails
— **and the key stays down forever.** It cannot be cleaned up either: a
capability body holds only the request-bound executioner, so once that context is
dead there is nothing to dispatch a release *with*. **Unwinding is not
implementable without changing the contract**, which is why it is ruled out
rather than merely skipped.

So:

- **Total sequence duration is capped at 10s**, refused in words before a single
  step runs. Ten seconds sits far enough under the broker's 30s that the
  deadline can only be reached by the caller hanging up, and far above any
  plausible hold.
- **256 steps** is also refused up front. It is a sanity bound on a malformed
  request, not a performance one. *Decided without asking, and easy to reverse.*

A stuck key after a disconnect is then the same thing the contract already chose
deliberately — nothing is undone when a session ends — rather than a new failure
mode, and the down-set on every response is how it is found.

---

## Nothing fails halfway

**Releasing a key that is not down is a silent no-op**, exactly as `state.apply`
already treats it (`input/state.go:45`). Rejecting it was considered — it
catches a caller's mistake — and rejected: a mid-sequence rejection leaves
earlier steps applied, which is the stuck key above reached by an ordinary typo
rather than by a disconnect.

So **all validation happens before the first batch**: unknown step kind, unknown
key, negative or over-cap duration, too many steps. A sequence has exactly two
outcomes — **refused with nothing applied, or applied whole** — which is why the
response carries no "steps applied" field and no partial-application reporting.

One exception, stated rather than papered over: an engine shutting down
mid-sequence fails the next dispatch, which can leave a key down in a game that
is exiting anyway.

---

## Naming a key

`input.Key` gains a name and a parse, exported because a name for a key is
something config files and debug tools want too:

```go
func (k Key) String() string
func (k Key) MarshalText() ([]byte, error)   // "w", "escape", "mouse_left", "#204"
func (k *Key) UnmarshalText(b []byte) error
func ParseKey(s string) (Key, error)
```

`encoding.TextMarshaler` rather than `MarshalJSON`, so a `Key` is a JSON
**string** everywhere it appears — in an `Action`, and in the down-set array.

**`#<n>` is accepted too, and it earns its place from the output side rather
than the input side.** `mapKey` is a plain cast from `gpucontext.Key` and cog's
table has gaps, so a driver can legitimately produce a `Key` no name covers —
and the down-set is printed on every response. `#<n>` is what an unnamed key
prints as, and accepting what you print is the cheap correctness. Negative
values (`#-1`) parse, so mouse buttons round-trip.

**The trap, and it must be named concretely wherever this is described:** `n` is
a cog `input.Key` and nothing else. Letters are `iota + 1`, so **`#13` presses
`M`** — while ASCII 13, JS `keyCode` 13 and USB HID usage 0x28 all say Enter,
and cog's `KeyEnter` is **84**. Every prior a model has says Enter. A generic
"prefer names" will lose to that prior; the worked counter-example is the only
thing that defuses it.

`Key` also implements `mcp.TextValued`, returning the name table and
`^#-?[0-9]+$`, so the tool schema is an `anyOf` of the enum and the pattern
rather than the `{"type": "integer"}` reflection would otherwise infer. That
interface is the only thing in this feature that exists for the agent's sake;
see
[mcp §Types that cross as text](../../../mcp/docs/specs/mcp.md#types-that-cross-as-text).

---

## `text` is text, not keys

A real keyboard fires both `OnKeyPress(KeyA)` and `OnTextInput("a")`
(`wgpu/input.go:14-26`), so a faithful `text` step would emit both. **It does
not.**

`!` is Shift+1 on one layout and Shift+8 on another, and faithfulness here is a
keyboard-layout problem with no consumer: nothing in cog reads `KeyEvent` *or*
`TextEvent` today. So **`text` emits text changes only**, feeding `State.Text()`
and `TextEvent`, and the cost is stated rather than hidden — **a text field
built on `KeyEvent` will not see typed text**, and a game that reads keys wants
`key_down`/`key_up`.

Scroll gets the same honesty: `ScrollChange(dx, dy)` carries whatever the driver
passed through (`wgpu/input.go:48`), nothing in cog consumes it, and cog has no
unit to promise.

**Gap.** Typing and scrolling have **no consumer in cog** and cannot be
validated in-repo. They are specified from the driver's own behaviour. What
would settle them is a game that reads `TextEvent` or `ScrollChange`.

---

## `move_by`

Relative pointer motion is in, **decided against the recommendation that shaped
the rest of this document**, and the reasoning is recorded rather than dropped.

The principle used to reject compound steps — a step earns its place if it is
something the caller cannot do for itself — rejects `move_by` too, because the
pointer comes back on every response and the caller can add and send an absolute
move.

What it buys, honestly: the handler resolves it **under the lock**, so it is
atomic against a human hand moving the mouse between the read and the write,
where the caller's own arithmetic is not; and a delta is the natural spelling for
anything camera-shaped. What it costs: a second way to express one move, which
is exactly what was refused for clicks. **The inconsistency is real and is left
standing rather than smoothed over.**

**It does not make mouse-look drivable.** cog has no relative-motion channel and
no pointer lock at all — `PointerChange(Pos)` is absolute-only and nothing in the
repo touches `gpucontext.CursorMode` — so `move_by` is arithmetic over absolute
positions, not the thing a first-person camera reads. That gap is
[input, app and wgpu: pointer lock, cursor mode and cursor
visibility](https://github.com/dvoyni/cog/issues/233), out of scope here and
carrying the requirement that it extend this document when it lands.

---

## A human and an agent at once

**No exclusivity.** Changes concatenate, and a second source serializes rather
than conflicts. An exclusive mode was rejected twice over: it is a lifecycle,
which the agent contract rules out entirely, and it would fight the human at
exactly the moment they reach for the mouse to see what broke.

**No provenance flag.** Marking a change synthetic means a new field on `Change`
and on all four events, and games branching on it — which destroys the premise.
**A game reacting exactly as it would to a person is the whole reason the input
seam was chosen over command dispatch.** The debugging trap gets one sentence
wherever this is described: nothing downstream can tell, and the down-set is
where a caller looks instead.

---

## Required input changes

A checklist for an implementation session.

**`input/action.go`** (new)

- `ActionKind` and its seven constants; `Action`.

**`input/commands.go`**

- `SynthesizeCmd`, `SynthesizeRequest`, `StateResponse`, `StateCmd`,
  `StateRequest`.

**`input/commandsimpl.go`**

- `synthesizeCmdImpl`: one `*State` write lock, fold every action into a
  `Change`, derive `Mods` from the live down-set **after** folding, resolve
  `move_by`, apply and publish exactly as `applyCmdImpl` does, return
  `StateResponse`.
- `stateCmdImpl`: a read under the state's read lock.

**`input/play.go`** (new)

- `Play`, with validation up front, the batching rule, and the `select` on
  `k.Context().Done()` between batches.

**`input/key.go`** (new, or into `contract.go`)

- The name table, `String`, `MarshalText`, `UnmarshalText`, `ParseKey`, and
  `TextValues`.
- The table is the single source of truth for both directions. A key with no
  name must round-trip through `#<n>`.

**Tests**

- `#13` parses to `KeyM` and `KeyEnter` prints as `enter`. This is the
  assertion that documents the trap in code.
- A key with no name round-trips through `#<n>`, including a negative value.
- `move, key_down, key_up` with no delay produces one dispatch; inserting a
  delay produces two.
- Ctrl+S built as `key_down ctrl, key_down s` reports `ModCtrl` on the `s`
  event.
- A sequence over the duration cap is refused with **nothing applied**.

**`input/README.md`**

- Document `Action`, `SynthesizeCmd`, `Play` and the key names, as package
  features. A test harness is a first-class caller here and must not have to
  read the agent spec to find them.

**`CONTEXT.md`** — already applied: **Synthetic input** is defined under Agent
Interface.

---

## Out of scope

- **Pointer lock, cursor confinement and cursor visibility.** cog cannot lock or
  hide the pointer and has no relative-motion channel, so a first-person camera
  is not drivable through this seam. It is an engine contract feature — config
  plus a command plus a place for a delta to land — filed as
  [#233](https://github.com/dvoyni/cog/issues/233), and **the first
  implementation skips it**. When it lands it extends this document rather than
  reopening the design.

- **Layout-faithful key synthesis**, per
  [`text` is text, not keys](#text-is-text-not-keys).

- **Frame-exact input timing** by counting ticks inside a sequence. It exists,
  and it is spelled `key_down → step → key_up → step` against a paused engine.
