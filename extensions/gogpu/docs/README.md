# gogpu

`github.com/dvoyni/cog/extensions/gogpu` is Cog's window, input, frame timing,
and WebGPU system driver built on the `github.com/gogpu/gogpu` library, and
named for it. In this README "gogpu" is the Extension, and "the gogpu library"
is the upstream package it wraps. It owns the OS main loop and provides
it as app's `app.MainLoop` Adapter, provides gfx's `gfx.Backend` Adapter, and feeds
`input`, on desktop and WebAssembly.

## Plugin

- Name: `gogpu.Name` (`"gogpu"`)
- Kind: an Extension. The root declares only `Name`, `Config`, the Adapters
  `AppMainLoop` and `GfxBackend`, and the errors; everything else is in
  `internal/`.
- Constructor: `gogpuplugin.New() kernel.Plugin`
- Plugin dependencies: `gfx`, `input`
- Go package dependencies: `app`, `gfx`, `input`, `kernel`, the gogpu library,
  WebGPU implementation packages. The root imports only `kernel`, `app` and
  `gfx`; the gogpu library is imported by `internal/` alone.
- Contributes: one `app.MainLoop` Adapter and one `gfx.Backend` Adapter
- Implements: `kernel.PluginHost`, `kernel.PluginStopper`
- Subscribed kernel events: none

Register dependencies before gogpu, and compose `appplugin.New()`: app is
the Slot gogpu's MainLoop fills. `Run(ctx)` owns the calling thread and blocks in
the platform main loop until the window closes, `app.QuitCmd` runs, or the
context is canceled.

The plugin implements `Name`, `Dependencies`, `Register`, `Run`, and `Stop`.
`Stop` exists for one job: releasing a readback the closing window left
outstanding. It is still the one `kernel.PluginHost`: the engine finds the host
by asking each plugin value, so the value `gogpuplugin.New()` returns is the
host without any type in the root naming it.

On desktop, importing the plugin (through `gogpuplugin`) locks the main
goroutine to OS thread 0 in a package `init`, which the gogpu library's window
needs.

## Configuration

`Config` arrives through `kernel.New`'s config map under `gogpu.Name`, and its
zero value is the default. Set only what changes, directly or with the
immutable builders:

```go
cfg := gogpu.Config{}.
    WithTitle("My App").
    WithAppName("My App").
    WithSize(1280, 720).
    WithResizable(true).
    WithVSync(true).
    WithFullscreen(false)
```

`Config` also exposes all fields directly: `Title` (`"cog"`), `Width` and
`Height` (1280x720), `NoResize`, `NoVSync`, `Fullscreen`, and `AppName`. The two
switches that default to on are spelled as their negation, so that off is the
zero value; `WithResizable` and `WithVSync` set them. The fixed step, its frame
clamp and its catch-up cap (`Step`, `MaxFrame`, `MaxPending`) are app's
`app.Config`, not this one.
`ErrInvalidConfig{Got}` reports a configuration value of the wrong type and its
`Error() string` method implements `error`.

## Errors

The root's `err.go` declares the errors the plugin reports that a caller may
match: `ErrInvalidConfig`; `ErrDepthOnlyPassUnsupported`, reported once
per run for a depth-only pass the selected backend declines to encode; and
`ErrBindGroupRefused{Shader, Group}`, reported once per shader and group for a
bind group the device would not create. The time errors are app's.

A refused bind group leaves every draw through it encoding with nothing bound
for that group, which renders wrongly or not at all. gfx catches the bindings it
can see before they get here - see *ErrStorageBufferUnsupplied* in gfx's README
- so what reaches this backstop is the route only the backend knows: a binding
gfx did emit, filled by a resource this backend no longer holds, released or
left over from a device ago. Both refusals reach the update thread the same way,
through `takeRefusal`, because the render thread holds no kernel handle.

## App's MainLoop

gogpu is the platform half of app's loop, and app is the rest. It provides a
view of itself as `app.MainLoop` with `registrar.ProvideAdapter[AppMainLoop]` in
`Register`; app hands it an `app.Loop` from app's `Start`, which precedes `Run`,
and asks it to `Quit` for `app.QuitCmd`, which stops the gogpu library's App
from any goroutine. The binding adds no dependency: gogpu dispatches none of
app's commands.

gogpu calls the Loop from the gogpu library's callbacks and owns no time control
of its own:

- `Run` calls `Loop.Init` immediately before entering the gogpu library's
  blocking main loop, and `Loop.Quit` once it returns.
- `onDraw`, on the render thread, measures the real draw-to-draw interval (paced
  to vsync or `requestAnimationFrame`, so accurate across JS turns) into an
  atomic frame clock, calls `Loop.WindowSize` when the DIP window size changes,
  resolves the viewport with `gfx.SetViewportCmd`, attaches the backend to the
  surface, and calls `Loop.Render`.
- `onUpdate`, on the main thread, flushes the frame's batched input and then
  calls `Loop.Frame` with the real time of the frames drawn since the last
  update. The gogpu library's own `deltaTime` is 0 on wasm and its busy-loop
  deltas are quantized, which is why the time comes from `onDraw`. The flush
  comes first, so every tick the frame publishes sees that input, and it runs
  whether or not app's tick source is paused.

app turns that frame time into fixed-step ticks and publishes every app event;
pause, step, hold, tick numbering and the `app_time` tool are app's, in
[`slots/app`](../../../slots/app/docs/README.md).

## Commands Executed

- `input.ApplyCmd`: flushes the frame's ordered key, pointer, scroll, and text
  changes into the input plugin before updates.
- `gfx.SetViewportCmd`: supplies logical-window and physical-framebuffer sizes
  each drawable frame.

## Events Published

None of its own. The Loop calls above are what publish `app.InitEvent`,
`app.UpdateEvent`, `app.WindowSizeChangeEvent`, `app.RenderEvent` and
`app.QuitEvent`, each synchronously on the thread gogpu calls it from.

gogpu does not subscribe through the kernel registry; the gogpu library's callbacks
invoke its update, draw, and input bridges directly.

## Backend Behavior

The private backend implements the public `gfx.Backend` contract, and is gfx's
Adapter. The plugin builds it once and provides it with
`registrar.ProvideAdapter[GfxBackend]`, the Adapter type in the root's `adapters.go`, at the top of `Register`, before the
GPU device exists: gfx is a Slot, and its Adapter is bound at composition,
while the device is created asynchronously inside the render loop. `onDraw`
attaches the device to the same value on the first frame the device is
available, and retries every frame until then; a failure is reported once.
Until it is attached the backend is not `Ready`, `NewTexture` and `NewBuffer`
still reserve ids, and no `app.RenderEvent` is published, so no frame is drawn
before the device is ready.

It maps Cog's
opaque IDs to native WebGPU textures and buffers, reflects WGSL bindings, caches
pipelines/samplers/bind groups, maintains depth targets, performs queued bakes
and releases, and submits each translated `gfx.Queue` to the current surface.

Reflection walks a shader's lowered module once. Alongside the global variables
it reads the `vs_main` entry point's arguments — flat `@location` parameters and
the members of a struct argument alike — and reports each as a
`gfx.ShaderVertexInput`, which is what `gfx.CheckVertexInterface` compares the
bound vertex layout against. `vs_main` is a constant shared with pipeline
creation, so what is checked cannot drift from what is built.

A pass whose target is `gfx.ScreenTarget()` does not render into the surface. It
renders into a frame-sized frame buffer the backend allocates on first use in
`gfx.FrameBufferFormat` and drops whenever the surface resizes; the frame's
implicit present pass then draws a full-screen triangle that samples it into the
surface. The present pipeline is the only one built for the surface's own format
— every other pipeline is built for the frame buffer's — because a hardware sRGB
swapchain is unreachable: the gogpu library hardcodes `BGRA8Unorm` and exposes
no view formats, and `bgra8unorm-srgb` is not a legal canvas-context format on
the web.

### Backend Files And Plugin Wiring

gogpu is two things to gfx at once, and its `internal/` files say which one they
are by what they use of the gfx root.

- **The backend** is gfx's Adapter: every `gfx*.go` file (`gfxbackend.go`,
  `gfxpass.go` with the queue replay, `gfxcapture.go`, `gfxpresent.go`,
  `gfxreflect.go`, `gfxsampler.go`, `gfxdepthonly.go`, `gfxlimits.go`) and
  `texformat.go`. Of cog they import the gfx root and Libraries only, and of
  the root they use only the backend contract: they implement `gfx.Backend`,
  replay a `gfx.Queue` and speak its IDs, formats and descriptors, and nothing
  in them names the recording API (`OpQueue`, `ResourceQueue`, the commands)
  an Adapter must never call.
- **The plugin wiring** is gogpu as a plugin that depends on gfx:
  `internal/plugin.go`. It uses the gfx root for the dependency on `gfx.Name`,
  to drive `gfx.SetViewportCmd` every drawable frame, and to provide the backend
  as a `gfx.Backend` with `registrar.ProvideAdapter[GfxBackend]`.
- **Tests** may use the whole root, for `gfx.CheckVertexInterface`, and
  `gfxplugin`, to compose an engine. A test that needs a flattened module draws
  once with the shader through `gfxplugin.New()` and a recording backend
  (`gfxflatten_test.go`), since the preprocessor is internal to gfx.

The tier test checks imports per package and cannot see which names a file
uses, so it cannot tell these files apart: the split is this README's rule, and
a backend file that names the recording API breaks it.

### Reading A Frame Back Without Waiting

`gfx` decides *that* a frame is read back; the backend owns *how*. The whole
sequence runs on the render thread and nothing in it blocks, which is the point
rather than an optimisation: `Device.Poll(PollWait)` calls `WaitIdle`, a
device-wide CPU-on-GPU stall, so a capture that waited for its map would make
the very frame an agent is asking about slow.

1. Inside `Execute`, when the queue's `Capture` op replays — last, after the
   present — the backend creates a fresh staging buffer sized
   `alignedRowBytes * height` and encodes `CopyTextureToBuffer` into the
   frame's own encoder. The buffer is `MapRead | CopyDst`, the copy offset is
   always zero (which dodges DX12's separate 512-byte offset alignment, which
   no public API reveals), and rows are padded to **256 bytes**, hardcoded
   because `hal.Alignments` is HAL-only and `Limits` has no row-pitch field.
2. `Finish`, then the frame's single `Submit`, exactly as before: the copy
   costs the frame its own bandwidth and nothing else, and `Execute` still
   submits once.
3. Immediately *after* that submit, `MapAsync` starts the map and the
   `*MapPending` is kept. No polling code exists anywhere in this package:
   `Queue.Submit` auto-polls at its tail, so the **next** frame's submit is
   what resolves it. On wasm the map resolves through a JS promise with no
   poll at all and `Status` reads the same either way, so the sequence needs no
   build tag.
4. `TakeCapture`, which `gfx` drains once per frame after `Execute`, is a
   `Status` check; on ready it copies the bytes out of the mapped range —
   which is a pointer into HAL memory, not a copy, and dies at `Unmap` —
   then unmaps, releases the buffer and releases the pending handle.

Two readbacks may be live at once and no more. That is not two captures in
flight: the frame that encodes the next still is the frame whose submit
resolves the previous one, so one slot is transiently held by a readback that
has resolved and not yet been taken. Anything beyond that is refused with
`gfx.ErrCaptureBusy`, and a refusal travels the same seam a result would have,
through `gfx.Capture.Err`. Depth and any format that is not 8-bit RGBA are
refused the same way, as is a target the frame never rendered into.

A screen capture reads the frame buffer, which `gfx` never names, so the
backend places that texture's barriers itself: `TextureBinding -> CopySrc`
before the copy and back again after it, because the present pass left it in
`TextureBinding` and the next frame's present barrier still has to name the
layout it is actually in. A texture capture needs neither — `gfx` tracked that
texture's role all frame and declared the transition itself, which is what the
third `gfx.TextureUsage` value is for.

`textureUsage` grants `CopySrc` to every `Renderable` texture, the one blocking
change the whole feature rested on. Nothing in cog was copyable off the GPU
before it, and the cost is stated rather than discovered: on some drivers
`CopySrc` disables lossless framebuffer compression on that texture, paid
whether or not a capture ever happens, and bounded to render targets.

`Stop` abandons whatever is still live. A capture armed in the last frame has
no further submit to resolve against, so its staging buffer and pending map are
released and `gfx.ErrCaptureAbandoned` takes their place; `gfx` delivers that
reason to whoever armed it.

### Barriers Are Ours To Place

The gogpu library tracks resources for lifetime and for submit-time validation,
but it derives no barriers from that tracking. Nothing it does orders a pass
that reads a texture against an earlier pass that rendered into it — not within
one command encoder, and not across a submit boundary either. On Vulkan the read
then happens while the image is still being written, which looks like a random
flicker in tiles of a half-drawn frame and is invisible to any screen capture
taken while the app is redrawing.

So whenever a pass samples what an earlier pass rendered into — the present pass
today, post-processing or shadow maps or scene render targets later — that pass
must place its own `encoder.TransitionTextures` from `RenderAttachment` to
`TextureBinding` first. `Present` in `gfxpresent.go` is the worked example.
`Device.WaitIdle` also removes the artifact, but it stalls the CPU on the GPU
and costs a frame of overlap; the barrier is one image transition per frame and
costs close to nothing.

Desktop and WebAssembly platform differences are hidden behind build-tagged
files; the public API is identical.

Frame pacing is the gogpu library's on both platforms, and the driver must not
reach for it. On the web the gogpu library schedules `requestAnimationFrame`
itself and runs the frame from inside that callback, so a frame ends by
returning to it. Awaiting rAF from `onDraw` - which this package did while the
gogpu library's `Run` was still a blocking loop that starved the event loop -
deadlocks the program instead of pacing it: the next animation frame cannot fire
until the current callback returns, and that callback is the one waiting.
