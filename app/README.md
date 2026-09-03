# app

`github.com/cog-engine/app` defines the driver-neutral application-loop and
display contract. It has no plugin and no implementation. A system driver such as
`wgpu` publishes lifecycle, update, render, and window-size events and implements
the quit command; a renderer such as `gfx` owns the `Viewport` resource and
handles the viewport commands.

## Files

`contract.go` holds package documentation and shared value types, `events.go` the
event declarations, `commands.go` the command/request/response declarations, and
`resources.go` the resource contract. Because this package is contract-only,
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

Package `app` only declares both commands; `gfx.Plugin` implements them.

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