# app

`github.com/dvoyni/cog/slots/app` is the platform-neutral application loop: the
lifecycle, update, render and window-size events every game subscribes to, the
quit and time commands, and the plugin that owns the fixed step, pause, step,
hold and tick numbering behind them.

app is a **Slot**: it ships its own declarations and implementation, and works
only once a `MainLoop` **Adapter** fills its required `MainLoopPort`. The MainLoop is
the platform main loop, and `gogpu` provides it on the desktop and the web. The
vocabulary is in [`CONTEXT.md`](../../../CONTEXT.md) and the decision in
[ADR 0002](../../../docs/adr/0002-slots-extensions-and-bundles-as-declaration-roots.md).
app declares no Resources: the viewport, its resource and its commands belong to
`gfx`.

## Packages

app has the declaration-root shape of
[`architecture.instructions.md`](../../../.github/instructions/architecture.instructions.md).

- **`slots/app`** is the root, and holds declarations only: the events, the
  commands, `MainLoop` and `MainLoopPort`, `Loop` and `TimeAction`, `Config`, the
  errors, the `McpProvider` Adapter and `Name`. It has no functions. It is what
  every other package imports.
- **`slots/app/internal`** is the plugin: its `New`, the `Loop` it hands the
  MainLoop, the tick source, the command handlers and the mcp provider.
- **`slots/app/appplugin`** exports only `New() kernel.Plugin`. Only composition
  roots and tests import it.

There is no `internal/types`: nothing app declares is aliased.

## Plugin

- Name: `app.Name` (`"app"`)
- Kind: a Slot, requiring one Adapter for `app.MainLoopPort`
- Constructor: `appplugin.New() kernel.Plugin`
- Plugin dependencies: none
- Contributes: one `mcp.Provider` Adapter (`app.McpProvider`)
- Implements: `kernel.PluginStarter`
- Subscribed kernel events: none

A plugin that dispatches `QuitCmd` or `TimeCmd` declares `app.Name` as a
dependency, which is what guarantees the command its handler: gfx, canvas and ui
do for their snapshot capabilities, and a game does for quitting. Subscribing to
app's events needs no dependency, because an event nobody publishes is simply
never delivered, which is how a test publishes `UpdateEvent` by hand without app.

```go
config := map[kernel.PluginName]any{
    app.Name: app.Config{}.WithStep(time.Second / 30),
}
plugins := []kernel.Plugin{
    storageplugin.New(),
    inputplugin.New(),
    appplugin.New(),
    gfxplugin.New(),
    gogpuplugin.New(), // provides app's MainLoop and gfx's Backend
    …
}
```

## The Loop And The MainLoop

```go
type MainLoop interface {
    Attach(loop Loop)
    Quit()
}

type MainLoopPort kernel.RequiredPort[MainLoop]

type Loop interface {
    Init(k kernel.Executioner) error
    Frame(k kernel.Executioner, dt float64)
    Render(k kernel.Executioner)
    WindowSize(k kernel.Executioner, width, height float32)
    Quit(k kernel.Executioner)
}
```

The loop is split along what varies by platform. **The MainLoop** runs the platform
main loop: it owns the window and the OS thread, measures real frame time, and
reads input. **app** owns everything else: the fixed-step accumulator, render
interpolation, the tick source and publishing every event. The two interfaces
face opposite ways: app calls the `MainLoop`, only to hand over its `Loop` and to
quit, and the `MainLoop` calls the `Loop`, every frame.

- **app hands over its `Loop` from `Start`** with `MainLoop.Attach`. Every `Start`
  runs before the Host's `Run`, so a MainLoop never enters its loop without one.
- **The MainLoop calls the `Loop`** from its own callbacks, with the Executioner
  it holds as the Host: `Init` immediately before entering its loop (an error
  means the loop must not start), `Frame` once per update on its main thread
  with the real seconds the frames drawn since the last call took, `WindowSize`
  when the window's size in device-independent pixels changes, `Render` once per
  drawn frame on its render thread, and `Quit` after its loop returns. Each
  publishes one event and waits for its subscribers, so the ordering the MainLoop
  chooses between its own work and these calls is the ordering subscribers see.
  gogpu flushes the frame's input before `Frame`, so every tick of the frame sees
  it, and calls `WindowSize` before it resolves the frame's viewport.
- **`QuitCmd` calls `MainLoop.Quit`**, from whatever goroutine dispatched it, so
  `Quit` must be safe on any goroutine.
- **Composition fails without a MainLoop**, with `kernel.ErrMissingAdapter`
  naming `app.MainLoopPort`.

A headless run is a MainLoop with no loop of its own: cog-examples'
`internal/headless` keeps the `Loop` it is handed and calls `Frame` with exactly
one `Step` of time and `Render` for each frame a test steps.

## Configuration

`Config` arrives through `kernel.New`'s config map under `app.Name`, and its zero
value is the default. Set only what changes, directly or with the immutable
builders `WithStep`, `WithMaxFrame` and `WithMaxPending`.

- `Step` is the fixed simulation interval, the update rate. Zero means 1/60 s.
- `MaxFrame` clamps the real time absorbed in one frame, bounding catch-up work
  after a stall. Zero means 250 ms, so a zero `MaxFrame` cannot turn the clamp
  off.
- `MaxPending` bounds how many catch-up ticks one frame publishes; the whole
  steps beyond it are dropped, so the loop stays near real time rather than
  spiralling. Zero means 4.

## Errors

The root's `err.go` declares the errors a caller may match:

- `ErrInvalidConfig{Got}`: the configuration value is not an `app.Config`.
- `ErrUnknownTimeAction{Action}`: a `TimeCmd` action the tick source does not
  know. It is a fault, not an outcome: the action is a Go enum.
- `ErrHoldTooLong{For, Max}`: a `TimeHold` longer than the 10 s cap. It is an
  expected outcome, and no hold begins.

## Commands

### `QuitCmd`

```go
type QuitCmd kernel.Command[QuitRequest, QuitResponse]
```

Requests that the application stop: app asks its MainLoop to quit the platform
main loop, which unwinds the Host's `Run` and shuts the engine down. It returns
once the request is made, not once the loop has stopped. `QuitRequest` and
`QuitResponse` are empty structs, and the handler takes no locks.

```go
quit := access.Uses[app.QuitCmd]()   // in the handler's Lock
```

### `TimeCmd`

```go
type TimeCmd kernel.Command[TimeRequest, TimeResponse]

type TimeRequest struct {
    Action TimeAction // TimeStatus | TimePause | TimeResume | TimeStep | TimeHold | TimeRelease
    Steps  int
    Join   bool
    Hold   time.Duration
}

type TimeResponse struct {
    Paused      bool
    Changed     bool
    Stepped     int
    Joined      bool
    Advanced    int
    Tick        int64
    Held        bool
    HoldFor     time.Duration
    HoldExpired bool
}
```

Controls the engine's **tick source**: what decides when an update tick is
published — the frame clock while running, or an explicit step while paused.
The app plugin handles it, whichever MainLoop runs the loop underneath, and the
handler takes no locks, which is what makes a step safe to wait on inside it.

This is an engine feature, not a debugging aside: a frame-step debugger, a
deterministic test harness and a replay tool all want the identical thing, and
none of them has to import a platform plugin to get it. A caller dispatches
`TimeStatus` and reads `Paused` to know whether a tick is coming, and adds
`HoldFor` to its own deadline before it waits for a step; there are no helpers
for either, and a caller that declares app as a dependency always gets an
answer.

- **Pause stops update ticks and stops nothing else.** The MainLoop keeps
  drawing the last completed frame, input still reaches `input.ApplyCmd`,
  `WindowSizeChangeEvent` still publishes, and the window stays live, movable
  and resizable. A paused game must not look hung, and a frame is still
  submitted, so anything reading a rendered frame still works.
- **Resume banks nothing.** Frame time handed to `Frame` during a pause is
  discarded rather than accumulated, so a thirty-second pause is followed by
  exactly one tick rather than a catch-up burst. The game continues from
  exactly where it stopped.
- **A step publishes `Steps` ticks in one frame**, each with
  `UpdateEvent.Last` set, so once-per-frame subscribers do their work and every
  step produces a complete frame; rendering shows the last of them. The
  `MaxPending` cap does not apply — a step that dropped ticks somebody asked for
  would be a silent lie. `Steps` zero means one.
- **Stepping implies pausing.** Stepping a running engine is meaningless, so
  the request pauses rather than being refused.
- **An arm joins a pending step.** A request with `Join` set attaches to a step
  already pending instead of raising another, so several observers arming
  together describe one tick instead of taking one tick each. It is a
  tick-source behaviour before it is anything else. On its own it is
  *opportunistic*: there is a step to join only until the next frame publishes
  it.
- **A hold keeps the step window open.** `TimeHold` stops a frame from
  publishing the pending step until `TimeRelease`, or until `Hold` runs out
  (default 1 s, maximum 10 s), so arms landing over several frames still share
  one tick instead of racing the frame clock for a place in the batch. It
  implies pausing for the reason stepping does. It carries a deadline because
  the alternative is an engine an absent caller has left unable to step, a
  longer duration is **refused with `ErrHoldTooLong` rather than quietly
  shortened**, and `HoldExpired` reports a hold that ran out instead of being
  released. `TimeResume` drops a hold along with the step it was keeping open,
  so resume is always the way out.
- **Resuming with a step still pending abandons it** and releases its caller,
  who reads back zero ticks stepped, rather than leaving it waiting on a tick
  the frame clock will never publish.
- **Every tick carries its number.** `UpdateEvent.Tick` counts from one and
  never resets, so anything recorded inside a tick can name the tick it
  describes and two such records can be *shown* to describe one moment rather
  than assumed to. `TimeResponse.Tick` is the last one published.
- **Pausing an already-paused engine is an ordinary answer**, `Changed` false,
  not an error.
- `TimeStep` **does not return until its ticks have been published**, bounded
  by the caller's context. Steps already requested when that context ends are
  still published.

Two limits, stated as non-guarantees rather than left to be discovered:

> **cog can stop the tick; it cannot slow it.** `UpdateEvent.Dt` is a constant
> and there is no engine clock to distort, so there is no time scale and there
> should not be one.
>
> **A game that reads wall-clock time itself is outside this contract, and
> pause cannot reach it.** Animation driven by ticks freezes; animation a game
> times with its own `time.Now` does not.

#### How the tick source is built

- **Pause is one branch in `Frame`.** While paused it discards `dt`, leaves the
  accumulator untouched, and publishes only the steps somebody asked for.
  Everything the MainLoop does around `Frame` runs exactly as it does while
  running.
- **The state is atomics, not a kernel resource.** The command handler runs on
  whatever goroutine dispatched it and `Frame` runs on the MainLoop's main thread,
  the boundary the render interpolation factor already crosses to `Render`. A
  resource would mean a dispatch every frame merely to ask whether to tick. A
  running frame reads one atomic; a paused frame with nothing pending reads two;
  numbering a tick is one atomic add.
- **A mutex guards only the handover** of one batch of steps to the frame that
  publishes it, and the hold that can postpone it: the part two atomics cannot
  express without a window where a step is counted twice or released early.
- **A hold expires where it is observed**, on each frame and each request, so it
  needs no timer or goroutine. It is the tick source's one wall-clock read, and
  it measures an absent caller rather than simulation time.

## Offered To An Agent

app contributes an `mcp.Provider` from `Register` and offers one capability,
rendered as the tool `app_time`: `pause`, `resume`, `step`, `hold`, `release`
and `status` over `TimeCmd`, with the resulting state on every answer. It is an
`mcp.Func` rather than an `mcp.Command` because a step waits for a frame and so
carries its own deadline (5 s), and because the action is validated before
anything is armed.

- `step` is capped at **600 ticks** — ten seconds of simulation — so the window
  in which a request can be created and then orphaned by its own deadline is
  bounded. A `hold` is capped at **10 s** on the same reasoning and with the
  same figure, and defaults to 1 s.
- `hold` and `release` are what make several snapshots describe one tick: hold
  first, arm the snapshots, release. Every answer also names the current
  `tick`, which is the number those snapshots report, so an agent confirms the
  pairing from the responses rather than from timing.
- A step's own wait, and each snapshot's, is **extended by whatever a hold may
  still cost**. A window somebody deliberately held open is not a stalled
  engine, and the deadline that names a stall must not be spent on it.
- Asking for a state the engine is already in (`pause` while paused, `resume`
  while running, `hold` while held, `release` with nothing held) is an
  `mcp.Unavailable` the agent reads and moves past, not an error.
- The capability is **not** `mcp.ReadOnly()`: all but one of its actions change
  the game. `status` is the read-only one, and MCP annotates a tool rather than
  an argument, so the honest annotation for the tool is the acting one.
- The provider offers it whether or not a broker is composed, and nothing
  resumes a paused game on disconnect: a pause stands until something resumes
  it.

The description prose the agent reads is reproduced in full in
[`specs/mcp.md`](specs/mcp.md), so it is reviewed as prompt text.

## Events

### `InitEvent`

```go
type InitEvent struct{}
```

Published once, immediately before the MainLoop enters the platform loop. Plugins use
it for runtime initialization that depends on commands or resources registered
by other plugins.

### `QuitEvent`

```go
type QuitEvent struct{}
```

Published once, after the MainLoop's platform loop returns. Plugins use it to dispose
application runtime state.

### `UpdateEvent`

```go
type UpdateEvent struct {
    Dt   float64
    Last bool
    Tick int64
}
```

A fixed simulation step, published by app on the MainLoop's main thread, in order,
waiting for each. `Dt` is the fixed step in seconds, always `Config.Step`.
`Last` is true for the final catch-up step of the current frame, allowing
subscribers to defer once-per-frame work until the latest simulation state; long
frames are clamped by `MaxFrame`, and at most `MaxPending` catch-up ticks are
published. While the tick source is paused none is published at all, except the
steps `TimeCmd` asks for, which ignore `MaxPending` and each carry `Last`.

`Tick` numbers the tick within the engine's run, counting from one and never
resetting. It is what names the moment something recorded inside a tick
describes: cog's three snapshots each carry it out to an agent, so two of them
can be shown to describe one tick rather than merely claimed to. app numbers
every tick it publishes, stepped or not; zero means the event was not published
by app, as when a test publishes one by hand.

Known subscribers:

- `input.AdvanceOnUpdate` subscribes first to advance per-tick input edges.
- `anim.AdvanceOnUpdate` subscribes first to advance timelines.
- canvas's `FlushOnUpdate` subscribes last, before `gfx`, to flush 2D operations.
- `gfx.PresentOnUpdate` subscribes last to present the completed graphics queue.

### `RenderEvent`

```go
type RenderEvent struct {
    Alpha float64
}
```

A rendered frame, published by app on the MainLoop's render thread once per drawn
frame, after the MainLoop makes the target current. `Alpha` is the interpolation
factor in `[0, 1)` between the previous and current fixed updates: the fraction
of a step the last `Frame` left in the accumulator.

Known subscribers:

- `gfx.RenderOnRender` subscribes to translate and execute the latest queue.

### `WindowSizeChangeEvent`

```go
type WindowSizeChangeEvent struct{ Width, Height float32 }
```

A change to the window size in device-independent pixels. app publishes it when
the MainLoop reports one, before the MainLoop resolves that frame's logical
viewport, so a game can pick a different desired policy for the new aspect (for
example landscape versus portrait).
