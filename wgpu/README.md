# wgpu

`github.com/cog-engine/wgpu` is Cog's window, input, timing, and WebGPU system
driver built on `gogpu`. It owns the OS main loop, implements the `gfx.Backend`,
feeds `input`, and drives the `app` update/render contract on desktop and WebAssembly.

## Plugin

- Name: `wgpu.Name` (`"wgpu"`)
- Constructor: `wgpu.New() *wgpu.Plugin`
- Plugin dependencies: `gfx`, `input`
- Go package dependencies: `app`, `gfx`, `input`, `kernel`, `gogpu`, WebGPU
  implementation packages
- Implements: `kernel.Host`
- Subscribed kernel events: none

Register dependencies before the driver. `Run(ctx)` owns the calling thread and
blocks in the platform main loop until the window closes, `app.QuitCmd` runs, or
the context is canceled.

`Plugin` implements `Name`, `Dependencies`, `Init`, and `Run`.

## Configuration

Start from `DefaultConfig()` and use immutable setters:

```go
cfg := wgpu.DefaultConfig().
    WithTitle("My App").
    WithAppName("My App").
    WithSize(1280, 720).
    WithResizable(true).
    WithVSync(true).
    WithFullscreen(false).
    WithStep(time.Second / 60).
    WithMaxFrame(250 * time.Millisecond).
    WithMaxPending(4)
```

`Config` also exposes all fields directly: `Step`, `MaxFrame`, `MaxPending`,
`Title`, `Width`, `Height`, `Resizable`, `VSync`, `Fullscreen`, and `AppName`.
`ErrInvalidConfig{Got}` reports a configuration value of the wrong type and its
`Error() string` method implements `error`.

## Command Implemented

`app.QuitCmd` calls the underlying application's `Quit` method. It has no
resource locks.

## Commands Executed

- `input.ApplyCmd`: flushes the frame's ordered key, pointer, scroll, and text
  changes into the input plugin before updates.
- `gfx.SetViewportCmd`: supplies logical-window and physical-framebuffer sizes
  each drawable frame.
- `gfx.SetBackendCmd`: installs the lazily created WebGPU backend once the
  device and surface are ready.

## Events Published

- `app.InitEvent`: published synchronously once in `Run`, immediately before
  entering gogpu's blocking main loop.
- `app.UpdateEvent`: published synchronously on the main thread at the fixed
  `Config.Step`. Long frames are clamped by `MaxFrame`; at most `MaxPending`
  catch-up events are emitted, and the last has `Last: true`.
- `gfx.WindowSizeChangeEvent`: published synchronously when DIP window size
  changes, before `SetViewportCmd` resolves the viewport.
- `app.RenderEvent`: published synchronously on the render thread after the
  surface is current. `Alpha` is the remaining fixed-step interpolation ratio.
- `app.QuitEvent`: published synchronously once when gogpu's main loop returns.

The driver does not subscribe through the kernel registry; `gogpu` callbacks
invoke its update, draw, and input bridges directly.

## Backend Behavior

The private backend implements the public `gfx.Backend` contract. It maps Cog's
opaque IDs to native WebGPU textures and buffers, reflects WGSL bindings, caches
pipelines/samplers/bind groups, maintains depth targets, performs queued bakes
and releases, and submits each translated `gfx.GpuQueue` to the current surface.

Desktop and WebAssembly platform differences are hidden behind build-tagged
files; the public API is identical.
