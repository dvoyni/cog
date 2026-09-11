# wgpu as an mcp provider — specification

`wgpu` offers an agent one capability: **`wgpu_time`**, which pauses the engine,
resumes it, steps it a named number of ticks, and reports which of those is
true.

Underneath it is an engine feature that MCP happens to want, not an MCP feature:
`app` declares the contract, `wgpu` implements it, and a frame-step debugger, a
deterministic test harness and a replay tool all want the identical thing. It
ships whether or not the broker exists, and `mcp` learns nothing new from it.

The extension point is
[mcp/docs/specs/mcp.md](../../../mcp/docs/specs/mcp.md). Assembled from the
resolved tickets of
[An agent-facing extension point across cog](https://github.com/dvoyni/cog/issues/199);
every section cites the tickets it came from. Nothing is decided here — where a
claim rests on something unverified, it is marked **Gap** and says what would
settle it.

---

## Contents

- [Vocabulary](#vocabulary) · [There is no clock to fake](#there-is-no-clock-to-fake)
- [What pause stops: the tick, not the frame](#what-pause-stops-the-tick-not-the-frame)
- [Resume banks nothing](#resume-banks-nothing) · [A step](#a-step)
- [An arm joins a pending step](#an-arm-joins-a-pending-step) ·
  [A hold decides it](#a-hold-decides-it)
- [Every tick is numbered](#every-tick-is-numbered)
- [Whose capability it is](#whose-capability-it-is) ·
  [The flag is an atomic](#the-flag-is-an-atomic)
- [`wgpu_time`](#wgpu_time) · [What the loop buys](#what-the-loop-buys)
- [Required app changes](#required-app-changes) ·
  [Required wgpu changes](#required-wgpu-changes)
- [Out of scope](#out-of-scope)

---

## Vocabulary

**Tick source** — what decides when an update tick is published: the driver's
frame clock while running, or an explicit step request while paused. **Rendering
is not a tick source: a paused engine keeps drawing the last completed frame.**
It is in `CONTEXT.md`.

That second sentence is the one every reader gets wrong, and it is why the term
exists at all. The fact is spread across an accumulator in `wgpu`, an ignored
`acquire` return in `gfx`, and two `defer` resets in `canvas` and `ui` — nobody
gets it from source.

---

## There is no clock to fake

The hardest-looking question here dissolves on one grep.

**`time.Now()` appears exactly once in the whole module** — `wgpu/plugin.go:203`,
the frame pacer measuring draw-to-draw interval. Nothing else in cog reads
wall-clock time. `app.UpdateEvent.Dt` is always `p.config.Step.Seconds()`
(`wgpu/plugin.go:160`), a **constant**, and `anim` advances timelines by exactly
that constant (`anim/plugin.go:43`).

So there is no engine time to distort, only a driver-local accumulator, and
pause is the decision to stop feeding it. **No time scaling, no virtual clock,
no `Time` resource.**

> **Amended at implementation ([#259](https://github.com/dvoyni/cog/issues/259)).** `time.Now()` now appears
> **twice**: the frame pacer, and a hold's deadline in `wgpu/tick.go` — see
> [A hold decides it](#a-hold-decides-it). The conclusion is unchanged. A hold
> measures how long an absent agent may keep the engine from stepping, never
> how far the simulation has moved, so there is still no engine clock, no
> `Dt` to distort and nothing to scale.

Two limits follow, and both are stated as non-guarantees rather than left to be
discovered:

> **cog can stop the tick; it cannot slow it.** There is no `timeScale`, and
> there should not be one.
>
> **A game that calls `time.Now()` itself is outside cog's contract, and pause
> cannot reach it.** A game whose animation is driven by ticks freezes; a game
> whose animation is driven by its own wall-clock does not.

### `UpdateEvent{Dt: 0}` was rejected

The tempting alternative is to keep publishing ticks with `Dt` zero. It would
make everything uniform — `anim` freezes because it multiplies by `Dt`, `input`
edges roll, `ui` re-lays-out, `canvas` re-records, and **every snapshot and
capture keeps working with no special case at all.** The whole of
[Under pause](#what-pause-stops-the-tick-not-the-frame) below would not exist.

It loses because **`Dt` is a constant in cog.** Every tick carries the same
value, so a game is positively encouraged to ignore it and count ticks:
`pos += velPerTick` is idiomatic here, not sloppy. A zero-`Dt` pause is a lie to
that entire class of game, and **a pause that half-works is worse than one that
visibly stops.**

---

## What pause stops: the tick, not the frame

**Pause stops `app.UpdateEvent` publication and nothing else.**

The lever already exists and is one branch deep. `onUpdate`
(`wgpu/plugin.go:152`) converts measured frame time into N update events through
`accumulate` (`:171`); `onDraw` (`:200`) is gogpu's own vsync callback and is
independent of it. Stop feeding the accumulator and updates stop while draws
continue.

**A paused engine still paints, and this is already true of the code.**
`renderOnRender` (`gfx/plugin.go:111`) calls `acquire`, **ignores its `false`
return** (`:122`), and re-translates `read.Get()` regardless. With no new queue
completing, the last completed queue is replayed every frame indefinitely. The
window shows the frozen frame rather than going black or stale-buffered, and a
capture still has something to read.

What else keeps running while paused, all of it deliberate:

- `flushInput` still dispatches `input.ApplyCmd` (`wgpu/input.go:98-105`), so
  synthetic input still reaches the seam and banks there.
- `app.WindowSizeChangeEvent` still publishes, and `app.SetViewportCmd` still
  fires from `onDraw`.
- The window stays live, movable and resizable.

**Nothing about pause makes the application look hung**, and the spec says so in
those words, because the opposite reading is the one that silently breaks every
capture: a capture needs a frame to be *submitted* before its readback can
resolve, so "paused" cannot mean "no submits". See
[gfx/docs/specs/capture.md §The wait](../../../gfx/docs/specs/capture.md#the-wait).

---

## Resume banks nothing

While paused, `onDraw` keeps incrementing `frameSeq` (`wgpu/plugin.go:205`), so
a naive resume computes `dt = frameDt × (seq − lastFrameSeq)` and turns thirty
paused seconds into a thirty-second delta.

The existing guards already contain it — `MaxFrame` clamps to 250 ms
(`wgpu/plugin.go:176`, default `wgpu/config.go:45`) and `MaxPending` caps at 4
whole steps (`:180`) — so the worst case today is four catch-up ticks, not a
spiral. This is polish rather than safety, and it is worth taking anyway:

**While paused, `onUpdate` consumes the frame sequence and discards its `dt`,
leaving `accum` untouched.** Resume then costs zero catch-up ticks and the
clamps are never exercised.

A resumed game continues from exactly where it stopped, which is the property an
agent relies on when it compares two observations across a pause.

---

## A step

- **A step publishes exactly one `app.UpdateEvent{Dt: Step, Last: true}`.**
  `Last` is true because a step *is* the last — and only — catch-up step of its
  frame, so once-per-frame subscribers (`canvas.flush`, `gfx.presentOnUpdate`,
  `scene.flush`) do their work and the step produces a complete frame.
- **`step(n)` publishes all n in one `onUpdate`, bypassing `MaxPending`.** That
  cap exists to keep a real-time engine near real time by dropping excess work;
  a step is not real time, and dropping requested steps would be a silent lie.
  Rendering shows the last of the n, which is what *advance sixty ticks and
  look* means.
- **`step` implies pause.** Stepping a running engine is meaningless; the
  capability pauses first rather than refusing.
- **`step` blocks until the steps are published.** The request arrives on an
  HTTP goroutine and the tick happens on the main thread, so `step` is a user of
  [mcp §Arm-then-wait](../../../mcp/docs/specs/mcp.md#arm-then-wait) and carries
  its own deadline for the same reason a capture does: the broker's 30 s is not
  the specific message.
- **`n` is capped at 600 — ten seconds of simulation** — on the same reasoning
  as `input_send`'s duration cap: a request that outlives its deadline leaves
  state the capability body can no longer unwind. The same figure bounds a
  capture burst's span, so there is one number to remember.

---

## An arm joins a pending step

**Arming a snapshot while a step is pending joins that step rather than
requesting another.**

Read per-arm, "a snapshot under pause performs exactly one step" would make
three concurrent arms three steps — three different ticks, which is the precise
opposite of what arming them together is for, and it would leave *canvas and ui
from one moment* unreachable. That pairing is the common case for a ui bug.

Implementation is **one more atomic on the driver** beside the pause flag,
`alpha` and `frameSeq` — the same thread boundary, for the same reason as
[below](#the-flag-is-an-atomic). It costs nothing when no step is pending.

This is what makes the pairing recipe work without any broker mechanism, and it
is why that recipe orders a capture **last**: a capture costs no tick, so it
shows whatever the last step produced, while armed first it would resolve
against the current frozen frame and straddle two ticks. See
[mcp §Pairing a moment](../../../mcp/docs/specs/mcp.md#pairing-a-moment).

**It is a tick-source behaviour before it is an agent-facing one**, so it
belongs in the `app` and `wgpu` READMEs alongside pause and step, not only here.

> **Amended at implementation ([#259](https://github.com/dvoyni/cog/issues/259)).**
> This section shipped as written and **is not sufficient on its own**. The
> join window is only as wide as the gap before the next rendered frame
> consumes the batch: `request` joins only while a step is still pending, and
> `take` — once per frame on the main thread — swaps the pending count to zero
> and drops the batch. Measured against **feuds-26** on a real window, three
> concurrent arms over three HTTP connections advanced the engine **two ticks,
> eleven times out of eleven**; two arms paired four times in five. So the
> rule is *opportunistic*, and this section's claim that it "makes the pairing
> recipe work without any broker mechanism" holds only when the arms happen to
> fit inside one ~16 ms gap. The deterministic half is
> [A hold decides it](#a-hold-decides-it) below; the rule itself stays exactly
> as written, as the behaviour for arms that simply arrive together.

---

## A hold decides it

> **Added at implementation ([#259](https://github.com/dvoyni/cog/issues/259)).**
> Everything in this section is new. It is the second half of the rule above,
> and it exists because the first half turned out to be a race against the
> frame clock rather than a decision.

**A hold stops a frame from consuming the pending step, so the step window
belongs to the agent rather than to the frame clock.**

`wgpu_time` gains two actions. `hold` opens the window, `release` closes it,
and `take` declines the batch it finds while one stands. The recipe becomes
*pause, hold, arm everything to be paired, release*, and the pairing stops
depending on whether several requests fit inside one frame's gap.

It is **one mechanism, spelled as two actions on the capability that already
owns pause and step**, which is where the state it acts on already lives.
No new capability, no package learning about another, and `mcp` and
`mcpserver` learn nothing: a hold is a tick-source behaviour with an
agent-facing spelling, exactly as a step is.

Four properties, each load-bearing:

- **A hold carries a deadline and expires on its own.** An agent that walks
  away must not leave a game nothing can step. Default **1 s**, maximum
  **10 s** — the same ten seconds every other span in this family is capped
  at, for the same reason.
- **A longer one is refused, not shortened.** A caller told it holds the
  window for a minute and quietly given ten seconds meets the difference as a
  split. The refusal is a typed domain error in `wgpu/err.go` mapped to
  `mcp.Unavailable` in the provider, which is this family's error shape.
- **Expiry is reported.** `holdExpired` stands on every answer until the next
  hold begins, because the caller who needs to know is the one coming back to
  a window it thought it still had.
- **`resume` drops a hold**, along with the pending step it was keeping open,
  so resume remains the one call that gets an engine moving again whatever
  state it was left in.

And one consequence that has to be said out loud, because getting it wrong
turns the mechanism into the failure:

> **A wait for a step is extended by whatever a hold may still cost.** The
> deadline that names a stalled engine is the wait for a tick that can never
> come, and a window somebody deliberately held open is not that. The
> snapshots' waits and `wgpu_time step`'s alike read `app.HoldRemaining` and
> **add** it to their own floor.

The deadline is the tick source's only wall-clock read, and it does not
reintroduce the engine clock
[There is no clock to fake](#there-is-no-clock-to-fake) rules out: it measures
how long an absent agent may keep the engine from stepping, never how far the
simulation has moved. `Dt` is still a constant and there is still nothing to
scale. That section's count of `time.Now()` in the module goes from one to
two: the frame pacer, and this.

---

## Every tick is numbered

> **Added at implementation ([#259](https://github.com/dvoyni/cog/issues/259)).**
> Also new. The shipped design gave an agent no way to tell a pairing from a
> split, which is what made the defect above invisible from the outside.

**`app.UpdateEvent` carries a `Tick`: a count of the ticks published, from
one, never reset.** The driver numbers every tick it publishes — one atomic
add on the frame path — and the number rides the event, so anything recorded
*inside* a tick knows which tick it was without asking the tick source
afterwards, by which time the answer has moved.

It reaches an agent as one field on `gfx.SnapshotView`, which `gfx_frame`,
`canvas_draws` and `ui_layout` all embed: **one field, not three**, because a
tick is the same tick in all of them. `wgpu_time` reports the current one on
every answer, so a snapshot and a time-control call line up.

This is what makes the hold *checkable* rather than merely asserted, and it is
worth having whatever else changes: `stepped` and `joined` never were
evidence, since two snapshots both reporting a step may be one tick apart —
which is precisely what was measured.

---

## Whose capability it is

**`app` declares the contract; `wgpu` implements it and provides the
capability.**

`app` is contract-only and a driver implements it — the exact precedent is
`app.QuitCmd`, declared at `app/commands.go:6` and handled by `wgpu` at
`wgpu/plugin.go:99`, with `app.SetViewportCmd` handled by `gfx` as the second
instance. Time control is the same shape: **only the host that owns the loop can
stop it**, and `app` names the contract so gameplay code never imports a driver.

**The provider must be `wgpu`**, because `mcp.Provider` embeds `kernel.Plugin`
and **`app` has no plugin at all**. There is no `app`-side thing that could
provide. This makes `wgpu` the first `wgpu_` provider and puts this file in the
spec family.

The tool is therefore named **`wgpu_time`**, and a different host driver would
name its own `sdl_time`. Two ways out were considered and rejected:

- **Amend the `<plugin>_<capability>` rule** so a provider declares its own
  prefix. Rejected: the rule's whole value is that the prefix is unforgeable and
  unique by construction, and one case does not buy an escape hatch.
- **Give `app` a plugin.** Rejected on cost: every existing application's
  composition breaks, and the driver would have to dispatch a command every
  frame merely to ask whether to tick, where today it reads a field.

**The driver-shaped name is accurate rather than unfortunate.** Pausing is a
property of the host that owns the loop, and a different host genuinely is a
different thing with a different answer.

---

## The flag is an atomic

The command handler runs on an HTTP goroutine; `onUpdate` runs on the main
thread. **That boundary already exists in this plugin and is already crossed
with atomics** — `alpha` (`wgpu/plugin.go:37`) and `frameDtBits`/`frameSeq`
(`:46-47`). The pause state, the pending-step count and the step-coalescing flag
join them as atomics on `wgpu.Plugin`, written only by the command handler.

A kernel resource was the alternative and loses concretely: `onUpdate` is a
driver callback holding an `Executioner`, not a handler holding a lock, so
reading a resource would mean **a dispatch every frame just to ask whether to
tick**. `gfx` pays that cost for the viewport because the viewport genuinely
belongs to the engine; the tick source belongs to the driver alone.

---

## `wgpu_time`

```go
type TimeRequest struct {
	Action string `json:"action"`          // pause | resume | step | hold | release | status
	Steps  int    `json:"steps,omitempty"` // for step; default 1, max 600
	Ms     int    `json:"ms,omitempty"`    // for hold; default 1000, max 10000
}

type TimeResponse struct {
	Paused      bool  `json:"paused"`
	Stepped     int   `json:"stepped"`  // ticks advanced by this call
	Advanced    int   `json:"advanced"` // total ticks advanced since the pause began
	Tick        int64 `json:"tick"`     // number of the last tick published
	Held        bool  `json:"held"`
	HoldMs      int   `json:"holdMs,omitempty"`      // how much longer a hold may stand
	HoldExpired bool  `json:"holdExpired,omitempty"` // the last hold ran out
}
```

> **Amended at implementation ([#259](https://github.com/dvoyni/cog/issues/259)).**
> The shipped shape had four actions and three answers. `hold` and `release`
> are the two new actions, `ms` the argument the first takes, and `tick`,
> `held`, `holdMs` and `holdExpired` the four new answers — see
> [A hold decides it](#a-hold-decides-it) and
> [Every tick is numbered](#every-tick-is-numbered). Nothing was removed, and
> it is still one tool.

`mcp.Func`, because `step` waits. The resulting state comes back on **every**
call, including `status`.

**One tool rather than four.** The same reasoning that collapsed `input`'s verb
family applies unchanged: multiple spellings of one operation move the selection
risk inside the tool set. These four actions are genuinely one operation on one
piece of state.

**`status` is the read-only path**, and it is a different argument to one tool
rather than a second capability. This is the one place the design differs from
`input_state`, which had to be separate because *looking* and *pressing* are
different capabilities, not different arguments to one — here they are
different arguments to one.

> **Amended at implementation ([#250](https://github.com/dvoyni/cog/issues/250)).**
> This section originally said the broker could annotate approval *per action*.
> It cannot: MCP annotates a tool, not an argument, and `mcp.ReadOnly()` is the
> only lever a provider has — so the choice is one annotation for all four
> actions. Three of them change the game, so **`wgpu_time` is not
> `mcp.ReadOnly()`**, as
> [#204](https://github.com/dvoyni/cog/issues/204) and
> [#211](https://github.com/dvoyni/cog/issues/211) already required of pause and
> step. `status` says it only reports in the description prose, and per-action
> approval annotation is out of scope for this effort: buying it would mean new
> vocabulary in `mcp` and the broker, which [#211](https://github.com/dvoyni/cog/issues/211)
> §12 rules out, or the second capability this tool exists to avoid.

**It binds to a frame for `step` and to none for `pause`, `resume` and
`status`**, which makes it the only capability that is both. That is a fact
about the action rather than a crack in the discipline: the discipline says a
capability that waits for a frame arms and then waits, and `step` does.

**"Already paused" is `mcp.Unavailable{Reason}`, not an error** — an expected
outcome the agent reads and moves past.

### The description prose

Reproduced in full, per the house style, so it is reviewed as prompt text:

> **Amended at implementation ([#259](https://github.com/dvoyni/cog/issues/259)).** The clause about arming
> snapshots together was one sentence, and it was not true of the shipped
> engine. It is now a paragraph of its own, naming `hold` and the `tick` field
> an agent checks the pairing with.
>
> Stop, start or single-step the game's update loop. `pause` stops update ticks;
> the window keeps drawing the last completed frame, stays responsive and can
> still be captured, so a paused game does not look hung. `step` advances
> exactly the number of ticks you ask for and implies pause. `resume` returns to
> real time from exactly where it stopped — no time is banked and nothing
> catches up. `status` just reports, and every answer names the current `tick`.
>
> Use this to take an observation that nothing moved underneath. Paused, two
> captures are identical, and `key_down`, `step 1`, `key_up`, `step 1` holds a
> key for exactly one tick — something a running engine cannot do. Snapshots
> (`canvas_draws`, `ui_layout`, `gfx_frame`) each need a tick, so while paused
> they perform one step themselves and say so.
>
> To make several snapshots describe *one* tick, call `hold` first, in the same
> batch of parallel calls as the snapshots and listed before them. A hold keeps
> the step they share open until you `release` it or until `ms` runs out
> (default 1000, maximum 10000), instead of letting the next drawn frame close
> it — without one, whether the calls pair depends on whether they all arrive
> inside the same ~16 ms gap, and they often do not. Then check it: every
> snapshot reports the `tick` it describes, and they paired only if that number
> is the same in all of them. Take `gfx_capture` last, after the snapshots have
> answered, because it costs no tick and so shows whatever the shared step
> produced. If a hold runs out before you release it, the next answer here says
> `holdExpired`.
>
> This stops cog's tick, and only that. Animation driven by ticks freezes;
> anything a game times by its own wall-clock does not. There is no slow motion.
> Nothing resumes the game when you disconnect — it stays paused until something
> resumes it, and a hold you walk away from expires by itself.

---

## What the loop buys

The reason this is in the first version rather than later, stated as the loop it
makes possible:

```
gfx_capture          -> image A, no tick
input_send key_down  -> banked, nothing ticks
wgpu_time step 1     -> exactly one tick, carrying exactly that input
gfx_capture          -> image B, no tick
```

**The difference between A and B is one tick carrying one input, with nothing
else moving.** That is not achievable in a running engine at all, and without it
every observation an agent makes is contaminated by however far the game moved
while it was thinking.

The mechanism is one branch inside a function that already exists.

---

## Required app changes

**`app/commands.go`**

- Declare the time-control command, `app.QuitCmd`-shaped: a request naming the
  action and a step count, a response carrying paused-ness and the ticks
  advanced.

**`app/README.md`**

- Document it as an engine feature with its stated limits: the `time.Now()`
  non-guarantee, *stop but not slow*, that pause stops the tick and not the
  frame, and that an arm joining a pending step is part of what stepping means.
  A test harness must find this without reading an agent spec.

---

## Required wgpu changes

> **Amended at implementation ([#259](https://github.com/dvoyni/cog/issues/259)).** Three additions to the
> list below, all of them consequences of the two new sections above: the tick
> counter and its `app.UpdateEvent.Tick` field; the hold state and its
> deadline on the tick source, with `TimeHold`/`TimeRelease` on
> `app.TimeAction` and `ErrHoldTooLong` in `wgpu/err.go`; and
> `app.HoldRemaining`, the caller-side seam every wait for a step reads.
> `gfx.SnapshotView` gains `Tick`, and `gfx`, `canvas` and `ui` carry it out
> of the tick their snapshot was recorded in.

**`wgpu/plugin.go`**

- Atomics beside `alpha` and `frameSeq`: `paused`, `pendingSteps`, and the
  step-coalescing flag.
- `onUpdate` (`:152`) gains the pause branch: when paused, consume the frame
  sequence, **discard its `dt`**, leave `accum` untouched, and publish
  `pendingSteps` × `app.UpdateEvent{Dt: Step, Last: true}`, bypassing
  `MaxPending`.
- Register the time-control command handler; it writes the atomics and blocks
  until the requested steps have been published.

**`wgpu/mcpprovider.go`** (new)

- `wgpu` implements `mcp.Provider`, returning the one capability.
- `TimeRequest`/`TimeResponse`, the `Func` body with its own deadline, and the
  description string reproduced above.

**`wgpu/README.md`**

- The accumulator branch, the discard-on-pause, step semantics, the atomics, and
  the step-coalescing rule.

**Tests**

- A paused engine publishes no `app.UpdateEvent` and keeps calling `onDraw`.
- `resume` after a long pause produces **one** tick, not a catch-up burst.
- `step(5)` publishes five ticks in one `onUpdate`, each with `Last: true`.
- Two snapshot arms landing while one step is pending produce **one** tick, and
  both snapshots report the same tick.
- **Three arms, then four, under one hold, against a frame loop that keeps
  running underneath them, repeated enough to catch a race** — one tick each
  time, and every snapshot reporting the same `tick`. A hand-stepped test
  cannot see this failure at all: the frame is what takes the batch.
- A hold expires by itself, publishes the step it was keeping open rather than
  stranding its arms, and the expiry is reported.
- `resume` drops a hold along with its pending step.
- A hold longer than the cap is refused and no hold begins.
- A hold is not charged against a snapshot's own stall deadline.
- A capture armed under pause resolves with no tick, and twice in a row gives
  byte-identical files. This is the assertion that documents the whole
  capture/snapshot asymmetry.

**`CONTEXT.md`** — already applied: **Tick source** is defined under Runtime
Architecture.

---

## Out of scope

- **Time scaling.** There is no clock to scale — see
  [There is no clock to fake](#there-is-no-clock-to-fake). cog can stop the
  tick; it cannot slow it.

- **Reaching a game's own `time.Now()`.** Outside cog's contract.

- **Auto-resume on disconnect.** A paused game is *visible* but not
  self-explaining — it looks like a hang rather than like a decision someone
  made — so this is the sharpest case of the contract's no-lifecycle rule. It is
  still ruled out for the reasons that rule gives: there is no session to hang
  it on, a streamable-HTTP disconnect is not prompt, and resuming a game
  someone deliberately froze is the wrong default at the moment the agent hands
  it to a human. The remedies are `status`, which says so, and `resume`, which is
  one call.

- **Deterministic replay built on stepping.** A step is the primitive a replay
  tool would use, and this document deliberately stops at the primitive.
