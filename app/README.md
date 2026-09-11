# app

`github.com/cog-engine/app` defines the driver-neutral application-loop and
display contract. It has no plugin and no implementation. A system driver such as
`wgpu` publishes lifecycle, update, render, and window-size events and implements
the quit command; a renderer such as `gfx` owns the `Viewport` resource and
handles the viewport commands.

## Files

`contract.go` holds package documentation and shared value types, `events.go` the
event declarations, `commands.go` the command/request/response declarations plus
`Paused`, the one caller-side helper over them, and `resources.go` the resource
contract. Because this package is contract-only,
`resources.go` declares the resource type directly rather than aliasing a private
one.

## Dependencies

- Go package: `github.com/cog-engine/kernel`
- Plugin dependencies: none; this package does not register a plugin.

## Commands

### `QuitCmd`

```go
type QuitCmd kernel.Command[QuitRequest, QuitResponse]
```

Requests that the active system driver stop its main loop. `QuitRequest` and
`QuitResponse` are empty structs. Package `app` only declares this command;
`wgpu.Plugin` implements it.

```go
quit := access.Uses[app.QuitCmd]()   // in the handler's Lock
```

### `TimeCmd`

```go
type TimeCmd kernel.Command[TimeRequest, TimeResponse]

type TimeRequest struct {
    Action TimeAction // TimeStatus | TimePause | TimeResume | TimeStep
    Steps  int
    Join   bool
}

type TimeResponse struct {
    Paused   bool
    Changed  bool
    Stepped  int
    Joined   bool
    Advanced int
}
```

Controls the engine's **tick source**: what decides when an update tick is
published — the driver's frame clock while running, or an explicit step while
paused. Package `app` only declares it; the driver that owns the loop
implements it, because only the host that owns the loop can stop it. `wgpu`
implements it today.

This is an engine feature, not a debugging aside: a frame-step debugger, a
deterministic test harness and a replay tool all want the identical thing, and
none of them has to import a driver to get it.

- **Pause stops update ticks and stops nothing else.** The driver keeps
  drawing the last completed frame, input still reaches `input.ApplyCmd`,
  `WindowSizeChangeEvent` still publishes, and the window stays live, movable
  and resizable. A paused game must not look hung, and a frame is still
  submitted, so anything reading a rendered frame still works.
- **Resume banks nothing.** Frame time measured during a pause is consumed and
  discarded rather than accumulated, so a thirty-second pause is followed by
  exactly one tick rather than a catch-up burst. The game continues from
  exactly where it stopped.
- **A step publishes `Steps` ticks in one frame**, each with
  `UpdateEvent.Last` set, so once-per-frame subscribers do their work and every
  step produces a complete frame; rendering shows the last of them. The
  driver's catch-up cap does not apply — a step that dropped ticks somebody
  asked for would be a silent lie. `Steps` zero means one.
- **Stepping implies pausing.** Stepping a running engine is meaningless, so
  the request pauses rather than being refused.
- **An arm joins a pending step.** A request with `Join` set attaches to a step
  already pending instead of raising another, so several observers arming
  together describe one tick instead of taking one tick each. It is a
  tick-source behaviour before it is anything else.
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

#### `Paused`

```go
func Paused(k kernel.Executioner) bool
```

The caller-side half of `TimeCmd`, for frame-bound work that has to know
whether a tick is coming: `gfx_capture` refuses a burst while paused, and
anything that waits for a tick must not wait for one that can never come. It
is one `TimeStatus` dispatch, so asking obliges nobody to import a host — and
**an engine that does not handle `TimeCmd` is running**, because a game
composed without time control cannot be paused. It lives here rather than
beside any one capability so that fallback is stated once.

### `SetViewportCmd`

```go
type SetViewportCmd kernel.Command[SetViewportRequest, SetViewportResponse]
```

Supplies the current device-independent window size and physical framebuffer
size. A driver runs it whenever either changes. The handler resolves the logical
viewport against the desired policy and writes the `Viewport` resource.

### `SetDesiredViewportCmd`

```go
type SetDesiredViewportCmd kernel.Command[SetDesiredViewportRequest, SetDesiredViewportResponse]
```

Selects the logical world-size policy through `ViewportMode`. `Size` applies to
`ViewportFixedWidth` and `ViewportFixedHeight`; `Width` and `Height` define the
desired rectangle for `ViewportFit` and `ViewportCover`. Invalid values fall back
to `ViewportWindow`. A game normally runs it once during startup.

Package `app` only declares both viewport commands; `gfx.Plugin` implements
them.

## Resources

### `Viewport`

```go
type Viewport struct {
    Width, Height                       float32
    WindowWidth, WindowHeight           float32
    FramebufferWidth, FramebufferHeight float32
}
```

The resolved logical world size, the device-independent window size, and the
physical framebuffer size. `gfx.Plugin` registers and writes it; gameplay and UI
declare `kernel.Read[*app.Viewport]`.

## Events

### `InitEvent`

```go
type InitEvent struct{}
```

Published once immediately before the system driver enters its main loop.
Plugins use it for runtime initialization that depends on commands or resources
registered by other plugins.

### `QuitEvent`

```go
type QuitEvent struct{}
```

Published once after the system driver's main loop returns. Plugins use it to
dispose application runtime state.

### `UpdateEvent`

```go
type UpdateEvent struct {
    Dt   float64
    Last bool
}
```

A fixed simulation step. `Dt` is the fixed timestep in seconds. `Last` is true
for the final catch-up step of the current frame, allowing subscribers to defer
once-per-frame work until the latest simulation state. A driver should publish
updates in order.

Known publishers and subscribers:

- `wgpu.Plugin` publishes it synchronously from the main thread.
- `input.Plugin` subscribes first to advance per-tick input edges.
- `anim.Plugin` subscribes first to advance timelines.
- `canvas.Plugin` subscribes last, before `gfx`, to flush 2D operations.
- `gfx.Plugin` subscribes last to present the completed graphics queue.

### `RenderEvent`

```go
type RenderEvent struct {
    Alpha float64
}
```

A rendered frame. `Alpha` is the interpolation factor in `[0, 1)` between the
previous and current fixed updates. A driver should publish this event once per
drawn frame on its render thread after making the target current.

Known publishers and subscribers:

- `wgpu.Plugin` publishes it synchronously on the render thread.
- `gfx.Plugin` subscribes to translate and execute the latest queue.

### `WindowSizeChangeEvent`

```go
type WindowSizeChangeEvent struct{ Width, Height float32 }
```

A change to the window size in device-independent pixels. A driver publishes it
before resolving that frame's logical viewport, so a game can pick a different
desired policy for the new aspect (for example landscape versus portrait).

## Registration

There is no `app.Plugin`. Register a driver that realizes this contract, for
example `wgpu.New()`, together with the driver's dependencies.