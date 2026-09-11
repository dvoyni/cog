# wgpu

`github.com/cog-engine/wgpu` is Cog's window, input, timing, and WebGPU system
driver built on `gogpu`. It owns the OS main loop, implements the `gfx.Backend`,
feeds `input`, and drives the `app` update/render contract on desktop and WebAssembly.

## Plugin

- Name: `wgpu.Name` (`"wgpu"`)
- Constructor: `wgpu.New() *wgpu.Plugin`
- Plugin dependencies: `gfx`, `input`
- Go package dependencies: `app`, `gfx`, `input`, `kernel`, `mcp`, `gogpu`,
  WebGPU implementation packages
- Implements: `kernel.Host`, `mcp.Provider`, `kernel.PluginStopper`
- Subscribed kernel events: none

Register dependencies before the driver. `Run(ctx)` owns the calling thread and
blocks in the platform main loop until the window closes, `app.QuitCmd` runs, or
the context is canceled.

`Plugin` implements `Name`, `Dependencies`, `Init`, `Run`, and `Stop`. `Stop`
exists for one job: releasing a readback the closing window left outstanding.

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

## Commands Implemented

`app.QuitCmd` calls the underlying application's `Quit` method. It has no
resource locks.

`app.TimeCmd` controls the tick source; see below. It has no resource locks
either, which is what makes a step safe to wait on inside it.

## The Tick Source

The driver decides when an update tick is published — from its frame clock
while running, from an explicit step while paused. Rendering is not a tick
source: **a paused engine keeps drawing the last completed frame.** `app`
declares the contract (`app.TimeCmd`) and states its limits; this is how the
driver implements it.

- **Pause is one branch in `onUpdate`.** While paused it consumes the frame
  sequence and **discards its `dt`**, leaving the accumulator untouched, and
  publishes only the steps somebody asked for. Nothing else changes: the input
  flush at the top of `onUpdate` still runs, and the whole of `onDraw` — the
  frame clock, `app.WindowSizeChangeEvent`, `app.SetViewportCmd`,
  `app.RenderEvent` — runs exactly as it does while running. `gfx` replays the
  last completed queue every frame, so the window shows the frozen frame
  rather than going black, and a frame is still submitted.
- **Discarding the frame time is what makes resume cost nothing.** `MaxFrame`
  and `MaxPending` would already bound a naive resume to four catch-up ticks;
  discarding makes it zero, so a resumed game continues from exactly where it
  stopped.
- **A step publishes all of its ticks in one `onUpdate`, bypassing
  `MaxPending`**, and marks **every one** of them `Last: true`. The cap exists
  to keep a real-time engine near real time by dropping work; a step is not
  real time, and dropping requested ticks would be a silent lie. `Last` on each
  makes every step a complete frame, and rendering shows the last of them.
- **The state is atomics, not a kernel resource.** The command handler runs on
  whatever goroutine dispatched it and `onUpdate` runs on the main thread — the
  boundary `alpha` and `frameDtBits`/`frameSeq` already cross. A resource would
  mean a dispatch every frame merely to ask whether to tick. A running frame
  reads one atomic; a paused frame with nothing pending reads two.
- **An arm joins a pending step.** A `TimeCmd` request with `Join` set attaches
  to the step already pending rather than raising another, and everything
  waiting on that step reads back the same ticks. Read per-caller, three arms
  landing together would be three steps on three different ticks, which is the
  opposite of what arming them together is for. An explicit step never joins.
- **Resuming with a step still pending abandons it** and releases its caller,
  rather than leaving somebody waiting on a tick the frame clock will never
  publish; the caller reads back zero ticks stepped.

## Offered To An Agent

`wgpu` implements `mcp.Provider` and offers one capability, rendered as the
tool `wgpu_time`: `pause`, `resume`, `step` and `status` over `app.TimeCmd`,
with the resulting state on every answer. It is an `mcp.Func` rather than an
`mcp.Command` because a step waits for a frame and so carries its own deadline
(5s), and because the action is validated before anything is armed.

- `step` is capped at **600 ticks** — ten seconds of simulation — so the window
  in which a request can be created and then orphaned by its own deadline is
  bounded.
- Asking for a state the engine is already in (`pause` while paused, `resume`
  while running) is an `mcp.Unavailable` the agent reads and moves past, not an
  error.
- The capability is **not** `mcp.ReadOnly()`: three of its four actions change
  the game. `status` is the read-only one, and MCP annotates a tool rather than
  an argument, so the honest annotation for the tool is the acting one.
- The provider offers it whether or not a broker is composed, and nothing
  resumes a paused game on disconnect: a pause stands until something resumes
  it.

The description prose the agent reads is reproduced in full in
[`docs/specs/mcp.md`](docs/specs/mcp.md), so it is reviewed as prompt text.

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
  catch-up events are emitted, and the last has `Last: true`. While the tick
  source is paused none is published at all, except the steps `app.TimeCmd`
  asks for — which ignore `MaxPending` and each carry `Last: true`.
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

A pass whose target is `gfx.ScreenTarget()` does not render into the surface.
It renders into a frame-sized frame buffer the backend allocates on first use
in `gfx.FrameBufferFormat` and drops whenever the surface resizes; the frame's
implicit present pass then draws a full-screen triangle that samples it into
the surface. The present pipeline is the only one built for the surface's own
format — every other pipeline is built for the frame buffer's — because a
hardware sRGB swapchain is unreachable: gogpu hardcodes `BGRA8Unorm` and
exposes no view formats, and `bgra8unorm-srgb` is not a legal canvas-context
format on the web.

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
through `GpuCapture.Err`. Depth and any format that is not 8-bit RGBA are
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

`gogpu` tracks resources for lifetime and for submit-time validation, but it
derives no barriers from that tracking. Nothing it does orders a pass that reads
a texture against an earlier pass that rendered into it — not within one command
encoder, and not across a submit boundary either. On Vulkan the read then
happens while the image is still being written, which looks like a random
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

Frame pacing is gogpu's on both platforms, and the driver must not reach for it.
On the web gogpu schedules `requestAnimationFrame` itself and runs the frame
from inside that callback, so a frame ends by returning to it. Awaiting rAF from
`onDraw` - which this package did while gogpu's `Run` was still a blocking loop
that starved the event loop - deadlocks the program instead of pacing it: the
next animation frame cannot fire until the current callback returns, and that
callback is the one waiting.
