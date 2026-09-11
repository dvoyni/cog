# gfx capture — specification

`github.com/dvoyni/cog/gfx` cannot read a rendered pixel back today. `MapAsync`,
`CopyTextureToBuffer`, `MapRead` and `GetMappedRange` return **zero hits across
the whole module**, and `gfx.Backend` (`gfx/contract.go:343-382`) has no readback
method — its only exit is `Execute(queue *GpuQueue)`.

This document specifies the mechanism that changes that: **a capture is an op in
the frame's own queue**, encoded into the frame's one encoder after the present
pass, resolved a frame later with no stall and no new polling machinery, and
handed back through a drain-once-per-frame seam on `Backend`.

It is a `gfx` feature, not an agent feature. The game's own code may capture any
colour texture it rendered into; the agent-facing capability sits on top and is
specified separately in
[gfx/docs/specs/mcp.md](./mcp.md). Nothing below the `Backend` seam knows an
agent exists.

Assembled from the resolved tickets of
[An agent-facing extension point across cog](https://github.com/dvoyni/cog/issues/199);
every section cites the tickets it came from. Nothing is decided here — where a
claim rests on something unverified, it is marked **Gap** and says what would
settle it.

---

## Contents

- [Vocabulary](#vocabulary) · [Facts this rests on](#facts-this-rests-on)
- [The seam: an op in the frame's queue](#the-seam-an-op-in-the-frames-queue)
- [What may be captured](#what-may-be-captured) ·
  [How `CopySrc` is granted](#how-copysrc-is-granted)
- [Where in the frame](#where-in-the-frame) · [The wait](#the-wait)
- [The result seam](#the-result-seam) · [What comes out, and out of which layer](#what-comes-out-and-out-of-which-layer)
- [Who pays](#who-pays) · [Shutdown](#shutdown)
- [Arming a capture from a plugin](#arming-a-capture-from-a-plugin) ·
  [Bursts](#bursts)
- [Facts the implementer must own](#facts-the-implementer-must-own)
- [Required gfx changes](#required-gfx-changes) ·
  [Required wgpu changes](#required-wgpu-changes)
- [Out of scope](#out-of-scope)

---

## Vocabulary

Both terms are in `CONTEXT.md`, and the split between them is deliberate.

- **Readback** — the transfer of a rendered texture from GPU memory into CPU
  memory. It is renderer vocabulary and belongs to `gfx`: only `gfx` and its
  `Backend` speak of it.
- **Capture** — one rendered colour target taken off the GPU and written to a
  file the agent names. It names a **moment**, not a stream.

A provider offers Captures. A backend performs a Readback. Getting these
interchangeable is what makes the layering arguments below unreadable, which is
why they were pinned first.

---

## Facts this rests on

These decided most of the design before any preference did, and the reasoning
leans on them throughout. From
[gfx capture: the readback contract](https://github.com/dvoyni/cog/issues/203).

- **The seam already exists, and it is not `Backend`.** `Present()` is not a
  `Backend` method — it is an op in the frame's `GpuQueue`, appended by the
  translator (`gfx/translate.go:203`) and replayed inside `Execute`
  (`gfx/gpuqueue.go:332`). `GpuPassSink` (`gfx/gpuqueue.go:105-122`) is where
  *do a thing to the frame buffer* already lives, and `GpuPassDesc` already
  carries `Screen bool` beside `Target TextureViewID` — the exact addressing
  split a capture needs.
- **Nothing in cog is copyable off the GPU today.** `textureUsage`
  (`wgpu/texformat.go:31-41`) grants `TextureBinding|CopyDst|RenderAttachment`
  and never `CopySrc`, so as the code stands `CopyTextureToBuffer` on the frame
  buffer fails validation. This is the one genuinely blocking change, and it is
  a one-line one.
- **`gfx.TextureUsage` has exactly two values, on purpose** — "deliberately just
  the two roles gfx can put a texture in" (`gfx/gpuqueue.go:41-52`). Since "a
  layout transition that names the wrong old layout is undefined behaviour"
  (`:65`), readback's third role cannot be faked with the two that exist.
- **The copy must be encoded after present, and that is forced rather than
  chosen.** `Present()` transitions the frame buffer
  `RenderAttachment -> TextureBinding` and then samples it
  (`wgpu/gfxpresent.go`). Encode a capture before present and present's own
  barrier names an old layout that is no longer true.
- **The frame buffer's bytes are already a picture.** `ScreenTarget` is an
  offscreen `RGBA8UnormSrgb` framebuffer (`wgpu/texformat.go:13`), straight
  alpha, not the swapchain. The present pass's OETF shader exists only because
  the *swapchain* is `BGRA8Unorm`; a readback bypasses the swapchain entirely.
  Un-stride the 256-aligned rows and you have the image — no colour conversion
  anywhere.
- **Readback is buildable and the pattern is already written one layer down.**
  `wgpu.Buffer.MapAsync`/`Unmap`/`MappedRange` and
  `CommandEncoder.CopyTextureToBuffer` exist on every backend, and
  `gogpu@v0.54.0/renderer.go:1810-1885` (`renderToImageReadback`) does the full
  staging-buffer sequence as a reference — with two mistakes this document names
  so they are not copied.

---

## The seam: an op in the frame's queue

`GpuPassSink` gains one method, shaped exactly like `Present`:

```go
// Capture copies one colour target into CPU-visible memory. Like Present it is
// a whole-frame action rather than a pass, so it carries no commands; unlike
// Present its result arrives later, through Backend.TakeCapture.
Capture(GpuCaptureDesc)
```

Two alternatives were rejected:

- **A method on `gfx.Backend`.** An out-of-band call has no encoder to write
  into and would have to open a second one, breaking `Execute`'s contract that
  it encodes every pass into one command encoder and submits once
  (`wgpu/gfxbackend.go:737-763`).
- **`wgpu` providing the capability to the broker directly.** It leaves gfx's
  own users unable to read a texture back forever, and it puts a GPU driver in
  the business of knowing what an agent is.

The engine already has this shape everywhere else: **gfx decides *that*
something happens to the frame; the backend owns *how*.**

---

## What may be captured

```go
// GpuCaptureDesc names one colour target to read back. Screen selects the frame
// buffer, which only the backend can resolve; Texture names any other colour
// texture, and zero means none. It mirrors GpuPassDesc's addressing exactly.
type GpuCaptureDesc struct {
	Screen  bool
	Texture TextureID
}
```

Render-to-texture works end to end already — `gfx.TextureTarget`
(`gfx/pass.go:77`), resolved at `gfx/translate.go:302`, views cached at
`wgpu/gfxpass.go:29-57` — and target-agnostic capture is barely more code than
screen-only. It is what a shadow map or a post-process intermediate wants later.

`TextureID` rather than `TextureViewID`, because `CopyTextureToBuffer` takes a
texture rather than a view, and because `TextureTransition.Texture` is already a
`TextureID` (`gfx/gpuqueue.go:67-70`). **A capture always reads mip 0, layer 0.**

**Depth is refused.** `FormatDepth32F` is denied even `CopyDst` because "WebGPU
forbids writing texels into a depth32float texture" (`wgpu/texformat.go:28-30`),
and a depth capture is not an image — it is a float field needing a range to be
legible. Depth readback is a real want
([#179](https://github.com/dvoyni/cog/issues/179)'s terracing is a depth
problem) but it is a *visualization* question, not a readback one.

---

## How `CopySrc` is granted

`textureUsage` adds `TextureUsageCopySrc` when `desc.Renderable` is set.

**You can only capture what something rendered into, so `Renderable` already
names exactly the capturable set.** It needs no new field and no prediction.

- Rejected: **a `Readable bool` on `gfx.TextureDesc`**, which would make app
  authors predict at texture-creation time whether anyone will ever want to
  look. They cannot, and a capture that fails because a flag was missing is the
  worst available failure.
- Rejected: **unconditionally on every non-depth texture**, which pays the cost
  on every atlas and material map in the scene for a capture that can never
  happen.

**The cost, stated plainly so nobody finds it in a profile:** on some drivers
`CopySrc` disables lossless framebuffer compression on that texture. It is
bounded to render targets, and it is paid whether or not a capture ever happens.

### `gfx.TextureUsage` grows a third value

```go
// TextureUsageCopySrc is a texture being read back into CPU-visible memory.
TextureUsageCopySrc
```

The enum is narrow because gfx had only two roles, not because three is wrong.
Its own doc demands a name for the role rather than a lie: "From is the usage
the texture is actually in, not a guess" (`gfx/gpuqueue.go:65-66`). The
alternative — the backend silently inserting an unnamed barrier for texture
captures — is precisely the undeclared hazard the transition system exists to
abolish.

**This only bites for texture captures**, because "the frame buffer is the one
attachment gfx never names" (`gfx/gpuqueue.go:184-186`):

- **Screen capture:** gfx declares no transition; the backend places
  `TextureBinding -> CopySrc` itself, after present.
- **Texture capture:** the translator emits
  `TextureTransition{Texture, From: <the role it tracked>, To: TextureUsageCopySrc}`,
  and the capture op claims pending transitions the same way `BeginPass` does
  (`gfx/gpuqueue.go:168-178`).

Mechanically, `gpuPass` gains a `capture` flag and a desc beside its existing
`present` flag, and carries its own `transStart`/`transEnd` range like every
other entry.

---

## Where in the frame

**The capture is the last thing in the frame's encoder.** The translator emits
it after the conditional `Present()` (`gfx/translate.go:200-204`) — after the
present if there was one, otherwise straight after the last pass.

Rejected: **a second encoder and a second submit after `Execute` returns.**
Same-encoder costs the frame nothing but the copy's own bandwidth and keeps
`Execute`'s *submits once* contract literally true.

---

## The wait

`Device.Poll(PollWait)` calls `halDev.WaitIdle()`
(`device_native.go:1133-1139`) — a device-wide CPU-on-GPU stall, the most
expensive call in the API. Blocking the render thread on a map is therefore not
merely slow, it is the worst available option: **a capture that stalls the
render thread changes the thing it is measuring.** The agent asks why a frame is
slow and the act of looking makes it slow.

Also rejected: **handing the `*wgpu.Buffer` to the broker's goroutine and
calling `Buffer.Map(ctx, …)`** (`buffer.go:111`). It is seductive — a blocking,
`context`-cancellable wrapper that would consume the agent's request context
directly — but it spawns that same `Poll(PollWait)` from a non-render thread,
and gogpu makes no thread-safety promise about a `*Device` touched from two
goroutines at once.

**The sequence, all of it on the render thread:**

1. Frame N, inside `Execute`: `CopyTextureToBuffer` into a fresh staging buffer,
   then `Finish`, then the frame's single `Submit`.
2. Immediately after that submit: `MapAsync(MapModeRead, 0, size)`, keeping the
   `*MapPending`.
3. Frame N+1's own `Submit` triages it. **No new polling code is needed
   anywhere**: `Queue.Submit` auto-polls at its tail
   (`queue_native.go:174,247`), and `buffer.go:156-158` says so outright —
   callers "rely on the auto-poll at the tail of `Queue.Submit` to let the
   mapping resolve."
4. `TakeCapture` checks `pending.Status()`, a field read. On ready:
   `MappedRange` → copy the bytes out → `Unmap` → `pending.Release()`.

**The render thread's entire added cost per frame, when no capture is
outstanding, is one nil check.**

**Portability.** On wasm the map resolves through a JS promise with no `Poll` at
all (`buffer_browser.go:127-161`), and `Status()` reads the same either way, so
the sequence is portable without a build tag.

The consequence to carry upward: **one frame of latency is a fixed property of
every capture**, and a capture needs a frame to be *submitted* before it can
resolve. That is why pause cannot mean "no submits" — see
[wgpu/docs/specs/mcp.md](../../../wgpu/docs/specs/mcp.md).

---

## The result seam

The request rides the op stream; **the result cannot.** It arrives a frame
later, by which time that queue has been recycled and cleared (`GpuQueue.Reset`,
`gfx/gpuqueue.go:150-165`). A callback stored in a capture op is a dangling
promise by construction.

`Backend` gains:

```go
// TakeCapture returns a completed capture, if one is ready, and clears it.
// Called once per frame after Execute; a capture armed in the previous frame
// is normally ready by the time the current frame's submit has triaged it.
TakeCapture() (GpuCapture, bool)
```

The precedent is `takeRefusal()` (`wgpu/gfxdepthonly.go:107-113`) — backend
state that has to reach a plugin without a kernel handle on the render thread,
drained once per frame. The **shape** is that one; the drain **site** differs,
and deliberately: `takeRefusal` is drained by the wgpu plugin in `onDraw`
(`wgpu/plugin.go:243-245`), whereas `TakeCapture` is drained by **gfx's own
render handler**, immediately after `list.backend.Execute(ops)`
(`gfx/plugin.go:126-138`), which is the handler that holds gfx's resource locks.
**The wgpu plugin never learns that captures exist.**

Rejected: **a channel handed in with the request**, which puts a channel across
the render-thread boundary for no gain.

**Singular, no id.** `TakeCapture() (GpuCapture, bool)`, not
`TakeCaptures() []GpuCapture`. With at most one capture in flight the answer is
unambiguously about the one request outstanding, so no id is needed to
correlate; a slice would imply plurality and drag an id in with it. If the
one-in-flight rule is ever relaxed it becomes a slice *then*, with an id *then*
— and the reason to want concurrent captures is throughput, which a debug
facility does not have.

This spends the breaking change on `Backend` that was flagged as cheap now:
`Capture` on `GpuPassSink` and `TakeCapture` on `Backend`, two additions in one
moment, while the implementor count is one.

---

## What comes out, and out of which layer

**The backend hands back raw bytes plus a descriptor. `gfx` un-strides into an
`image.NRGBA` and hands out an `image.Image`.**

A GPU driver has no business owning an image encoder, and every consumer above
gfx wants pixels rather than strides. The un-stride is a row-copy loop and
nothing else: no conversion, no colour management, no precision loss.

```go
// GpuCapture is one completed readback: either the mapped bytes or the reason
// there are none. Pixels carries the GPU's own row padding, which BytesPerRow
// describes; gfx removes it. A backend never sees an image.Image.
type GpuCapture struct {
	Pixels        []byte
	Width, Height int
	Format        TextureFormat
	BytesPerRow   int
	Err           error
}
```

**One struct carries success and failure**, so a caller cannot handle one and
forget the other — which is exactly how a five-minute hang gets shipped.
Failures are typed structs in cog's style (`ErrBackendMissing{}`,
`ErrDepthOnlyPassUnsupported{…}`):

- `ErrCaptureAbandoned{}` — the engine stopped before the map resolved.
- `ErrCaptureBusy{}` — a capture was already in flight.
- `ErrCaptureUnsupported{Format}` — depth, or any format that is not 8-bit RGBA.

**`NRGBA`, not `RGBA`.** `image.RGBA` is premultiplied; cog's `FormatRGBA8` is
documented "straight-alpha RGBA" (`gfx/contract.go:38-40`). gogpu's own readback
uses `image.RGBA` (`renderer.go:1885`) and is **wrong** for straight-alpha
content; cog must not copy that.

`gfx` also exports `GpuCapture.Image() image.Image`, so its non-agent users get
the same convenience the provider does.

**PNG encoding and the file on disk stop at gfx's edge** in the sense that they
are not the backend's — but they are still gfx code, because gfx hosts its own
provider. What matters is *which goroutine*: see
[Arming a capture from a plugin](#arming-a-capture-from-a-plugin).

---

## Who pays

- **A staging buffer is allocated per capture and released after `Unmap`.** No
  cached buffer. The frame buffer's own doc already made this call the same way:
  "a frame that renders only into its own textures never asks for it and never
  pays for it" (`wgpu/gfxpresent.go`). Holding ~8 MiB for a whole run so that a
  debug feature used a few times an hour saves an allocation is the wrong trade.
- **At most one capture in flight**, enforced by the backend — the only layer
  that knows whether a map is pending. A second arm is refused, and the refusal
  travels the same seam as a result.
- **The cost, in these terms:** one full-frame texture-to-buffer copy of
  bandwidth; one frame of added latency; a transient allocation of
  `alignedRowBytes * height`, about 8 MiB at 1080p. **No stall of either
  thread.**

---

## Shutdown

A capture armed in frame N resolves in N+1. If the window closes between them,
`Run` returns, no further submit happens, and the pending map never resolves.
Left alone, the waiter learns nothing and sits until the MCP client's
five-minute idle abort.

**The backend abandons in-flight captures at shutdown and completes them as a
failure.** The trigger already exists: the engine cancels `e.ctx` before any
`Stop` (`kernel/engine.go:247-250`). What this contract adds is the obligation
that abandonment is *delivered*, through the same channel a result would have
used — which is why `GpuCapture` carries `Err` rather than the delivery
mechanism carrying a second path.

---

## Arming a capture from a plugin

From
[The capture request: flag, next frame, and what comes back](https://github.com/dvoyni/cog/issues/207)
§§4–5. This is the layer between the readback contract above and the capability
in [gfx/docs/specs/mcp.md](./mcp.md), and it is ordinary `gfx` API: anything
holding a kernel handle may arm a capture, not only the provider.

### The moment: the next completed op queue, not the next render

Two readings of "the next frame" differ observably, and the difference is the
whole guarantee:

- **the next frame rendered** — the render handler notices a pending flag and
  attaches a capture op to whatever it is about to draw;
- **the next frame recorded** — the flag is consumed at end of tick by
  `presentOnUpdate` (`gfx/plugin.go:71-80`, registered `.Last()`), so the
  capture binds to a specific completed `OpQueue`.

**The second.** Under the first, a caller that sends input and then captures can
get the frame recorded *before* the input was consumed, because the pending
queue was already complete when the flag arrived. In the spec's words:

> A capture shows the game as of a tick that began after the request was made.

That costs one extra tick, about 16 ms at 60 Hz, and it makes press-then-capture
correct with no composition mechanism at all.

**The capture rides the ready slot, not the queue.** `present()` swaps
write→ready every tick (`gfx/plugin.go:82-90`); if two ticks complete before one
render, the first ready queue is recycled unrendered. If a newer recorded queue
displaces the pending one before a render, **the capture goes with the newer
queue**. It is never dropped, and the guarantee only strengthens.

**Under pause the rule applies rather than bending.** A paused engine publishes
no `app.UpdateEvent`, so no tick can begin at all and the last completed tick
*is* the present — `renderOnRender` (`gfx/plugin.go:111`) re-translates the
frozen `readList` every frame, so there is always a render to ride. A capture
under pause is therefore served from the next render, with **no tick**, and the
guarantee is satisfied vacuously rather than weakened. The consequence is the
property pause exists for: **two captures taken under one pause are
byte-identical.** Waiting for a queue completion instead would make every
capture under pause time out.

### Delivery: the arm command hands back the wait

```go
type ArmCaptureResponse struct {
	Done     <-chan GpuCapture // buffered, capacity = amount
	Viewport app.Viewport
}
```

gfx's render handler, having drained `TakeCapture()`, does one **non-blocking
send** and clears its pending slot. The render thread therefore never blocks on
a waiter that has walked away, and a value nobody receives is simply collected.
Refusals travel the same channel, because `GpuCapture` carries `Err`.

Rejected: **a channel passed in with the request** — gfx then cannot refuse a
second arm synchronously. Rejected: **polling with a second command** — a sleep
loop racing a 16 ms frame.

**The channel carries `GpuCapture` — padded bytes and a descriptor — not an
`image.Image`.** The render thread's entire added cost is the one `copy` out of
the mapped range it must do before `Unmap` anyway. Un-striding (~8 MiB of row
copies at 1080p), PNG encoding and the disk write all happen on the caller's
goroutine, which for the agent path is the broker's.

**`Viewport` rides the arm response** because a capability body cannot read a
resource. It is a plain value struct of floats, copied out exactly as
`SetViewportResponse` already does (`gfx/commandsimpl.go:86`). It is read one
tick before the captured frame; a window resized inside that two-frame window
would report a stale *window* size, but the **pixel** dimensions always come
from the capture itself, so the file is never mis-described.

### Two requests in flight: refused, in words

There are two refusal sites and both are wanted:

- **gfx refuses a second pending request synchronously at arm**, as an ordinary
  command error.
- **The backend refuses a second in-flight map** through `GpuCapture.Err`
  (`ErrCaptureBusy{}`), which still happens, because capture is a public gfx
  feature and the game's own code may arm one.

Rejected: **queueing**, which turns a boolean into a queue for a ~50 ms window.
Rejected: **coalescing two waiters onto one frame**, which is tempting — two
callers asking at the same instant arguably should get the same frame — but each
names a different file, so it forces either duplicate encode-and-write work or a
`sync.Once` on the plugin struct, and a capability body poking plugin state is
exactly what the contract forbids.

One consequence to state: a request whose caller has already hung up still
binds, fills and sends into a buffered channel nobody reads before clearing the
slot. A capture arriving in that window gets `Busy`. It is bounded by the same
three frames and is not worth a mechanism.

---

## Bursts

From
[Motion: does the agent ever get a sequence of frames?](https://github.com/dvoyni/cog/issues/230).
**A burst is gfx re-arming its own flag, entirely above the `Backend` seam.**
Nothing in the backend contract, the op queue, the encoder ordering or the
`MapAsync`-resolves-on-next-submit trick is touched, which is why
[#203](https://github.com/dvoyni/cog/issues/203) is not amended by it.

A capture request carries `amount` (stills, default 1) and `interval` (ticks
between them, default 1). `amount` stills are written, `interval` ticks apart:
frame *i* binds to the completed op queue `i × interval` ticks after the arm,
inheriting the guarantee above verbatim.

- **Caps: `amount ≤ 60`, and `amount × interval ≤ 600`** — the span bound
  borrowed from `wgpu_time step`'s cap, with the same number, so there is one
  figure to remember. `interval` is what buys a long window, never `amount`.
- **`amount > 1` under pause is refused in words.** No new ticks means N
  byte-identical files, and a silent pile of duplicates is exactly the failure a
  debug facility must not have. A *single* capture under pause stays legal.
- **Backpressure: the channel gets capacity `amount`, and nothing is dropped.**
  PNG encoding a 1080p frame takes longer than a frame, so a burst at
  `interval = 1` outruns its encoder, and a non-blocking send would lose exactly
  the frames it cannot hand over. Three ways out, and the map's Notes decide
  between them: waste is affordable, **stalling a thread the game needs is
  not**. So blocking the render thread is out, and dropping is worse than
  paying, because a caller reasoning about frames 4 and 6 has no way to know 5
  existed. Peak cost is `amount × ~8 MiB` of CPU bytes — about 500 MB at the cap,
  and far less in practice because the encoder drains continuously. This is the
  hardest this design leans on its own waste note, and it is the right place to
  lean.
- **GPU staging is unaffected.** The backend hands back padded bytes already on
  the CPU, so a staging buffer is released as soon as it is mapped and copied. A
  burst needs a small ring, not `amount` of them.
- **One burst in flight.** A second capture request of any kind while one is
  armed is `ErrCaptureBusy`, refused in words — the same rule stated about a
  longer object.
- **A burst truncates rather than failing.** A deadline, a refusal, a game
  exiting: **zero frames is an error, one or more is a short success**, and the
  response reports which ordinals were written.

**A burst does not enter the pairing recipe.** That recipe pauses and orders a
capture last to put one moment together; a burst is a running-engine instrument
that deliberately spans many moments. They are opposite tools and the spec says
so rather than leaving a caller to discover it.

---

## Facts the implementer must own

Because nothing exports them, and three of them are mistakes already made one
layer down.

- **256-byte row alignment is hardcoded.** Nothing public exposes it:
  `hal.Alignments{BufferCopyPitch}` (`wgpu/hal/descriptor.go:44-49`) is HAL-only
  and `Limits` has no row-pitch field. gogpu hardcodes it too
  (`renderer.go:1812`).
- **Always use `Offset: 0`.** DX12 additionally wants `BufferCopyOffset: 512`
  (`hal/dx12/adapter.go:270-271`), which no public API reveals. A zero offset
  dodges it entirely.
- **No channel swizzle is ever needed in cog.** gogpu branches on BGRA because
  it reads back the *surface* format; cog's format table is closed at `RGBA8`,
  `RGBA8Srgb`, `Depth32F`, and depth is refused — so every capture is RGBA8 and
  the un-stride is a straight `copy` per row.
- **Bytes per texel comes from `BlockCopySize()`**, which `bytesPerTexel`
  (`wgpu/texformat.go:23-25`) already uses. Never a hardcoded `* 4`, which is
  gogpu's other shortcut.
- **`MapPending` must be `Release()`d.** gogpu leaks its own
  (`renderer.go:1900`); the pool entry is harmless but the omission is not a
  precedent to copy.
- **`MappedRange.Bytes()` is a pointer into HAL memory, not a copy**
  (`mapped_range.go:64`), and returns nil once the buffer's generation advances.
  Copy out before `Unmap`.
- **`MapAsync` validation errors are synchronous** (`buffer.go:168-181`), so a
  bad range fails at the arm rather than a frame later.
- **`wgpu/gfxbackend.go:717` `resolveTarget` is stale dead code** — its comment
  "Only the screen target exists today" is false and nothing calls it. Do not
  take it as evidence about targets.

---

## Required gfx changes

A checklist for an implementation session, in dependency order.

**`gfx/gpuqueue.go`**

- `TextureUsageCopySrc` as the third `TextureUsage`, with the doc comment above.
- `GpuCaptureDesc{Screen bool, Texture TextureID}`.
- `Capture(GpuCaptureDesc)` on `GpuPassSink`, and its replay inside the queue's
  op switch beside `Present`.
- `gpuPass` gains a `capture` flag and a desc beside `present`, with its own
  `transStart`/`transEnd` range.

**`gfx/contract.go`**

- `TakeCapture() (GpuCapture, bool)` on `Backend`.
- `GpuCapture{Pixels, Width, Height, Format, BytesPerRow, Err}` and the three
  typed errors.

**`gfx/translate.go`**

- Emit the capture op after the conditional `Present()` (`:200-204`), and for a
  texture capture emit the `TextureTransition` to `TextureUsageCopySrc`.

**`gfx/capture.go`** (new)

- The pending-request slot, the arm command and `ArmCaptureResponse`, the burst
  counter and its re-arm, `GpuCapture.Image()`, and the un-stride into
  `image.NRGBA`.
- Validation — path, extension, `%d` verb, caps — all **before the first arm**,
  so a burst is refused whole or armed whole.

**`gfx/plugin.go`**

- `presentOnUpdate` (`:71-80`) consumes the pending capture request at end of
  tick and binds it to the ready slot; the burst re-arms here.
- The render handler drains `TakeCapture()` immediately after
  `list.backend.Execute(ops)` (`:126-138`) and does one non-blocking send.
- gfx implements `mcp.Provider`; see [gfx/docs/specs/mcp.md](./mcp.md).

**`gfx/README.md`**

- Document the capture op, `TakeCapture`, the third `TextureUsage`, and that
  `Renderable` now implies `CopySrc` with its compression cost. A game's own
  code is a first-class caller here and needs to find this without reading the
  agent spec.

**Tests**

- A capture armed on a tick binds to the queue completed *after* the arm, not to
  the one already pending — the guarantee, asserted rather than assumed.
- A second arm while one is pending is refused synchronously.
- The un-stride is exact for a width whose row bytes are not 256-aligned; this
  is the one arithmetic bug that produces a plausible-looking sheared image.
- Abandonment at shutdown arrives on the channel rather than hanging.

**`CONTEXT.md`** — already applied: **Readback** and **Capture** are defined,
and **Capture** has been amended twice — once to say it names a moment rather
than being handed over as an image, and once to stop asserting that a Capture is
a single still, which a burst made false.

---

## Required wgpu changes

**`wgpu/texformat.go`**

- `textureUsage` adds `TextureUsageCopySrc` when `desc.Renderable` is set. This
  is the one-line blocking change.

**`wgpu/gfxbackend.go`**

- Handle the capture op inside `Execute`: `CopyTextureToBuffer` into a fresh
  staging buffer before `Finish`, then `MapAsync` immediately after the frame's
  single `Submit` (`:749`), keeping the `*MapPending`.
- For a screen capture, place the `TextureBinding -> CopySrc` barrier itself,
  after present.
- `TakeCapture()`: a `Status()` check, then `MappedRange` → copy → `Unmap` →
  `Release()`. One nil check per frame when nothing is outstanding.
- Refuse a second in-flight map with `ErrCaptureBusy{}`; abandon in-flight
  captures at shutdown with `ErrCaptureAbandoned{}`; refuse depth and non-RGBA8
  with `ErrCaptureUnsupported{Format}`.

**`wgpu/README.md`**

- The staging-buffer sequence and the auto-poll it relies on, because the next
  person to read `Execute` will wonder where the wait went.

---

## Out of scope

- **Depth capture.** Refused by the format table, and it is a visualization
  question rather than a readback one: a depth capture is a float field needing
  a range to be legible. It may return as fog if an agent ever asks.

- **Capturing a non-renderable texture** — the canvas glyph or sprite atlas
  being the real case, and exactly the sort of thing that is wrong in a way a
  screenshot cannot show. Still `Renderable`-only, and **not on cost**: the
  debug-facility note removes the cost objection. It is out because an atlas
  capture wants a *different* capability. It has no frame to wait for, it is a
  bake-time artifact, and the honest answer for it is probably to read the
  CPU-side pixels that produced it, which never left main memory. Widening
  `CopySrc` here would invite a GPU round trip for something that never needed
  one. If the want survives, it belongs to `canvas`.

- **Naming a texture to capture, from an agent.** `TextureID` is an opaque
  handle and nothing lists textures, so the agent-facing capability is screen
  only. A name-based selector and the listing behind it are fog on the map.

- **Per-pixel temporal reduction over a burst** — one image in which crawl reads
  as brightness. It is the only form in which an agent can see shimmer,
  specular swim or z-fighting, and a burst does not buy it: sixty stills of
  crawl are still sixty stills. Its natural arrival is
  [#54](https://github.com/dvoyni/cog/issues/54)'s golden-image diff threshold
  rather than this family, since cog has no image comparison anywhere and
  `gogpu` already ships the pattern one layer down.

- **Recording for a human to flip through.** Rejected and recorded because it is
  the obvious reading of the want a burst inherited: cog has no encoder and
  should not grow one, a directory of PNGs is a worse screen recorder than the
  one already on the machine, and its consumer sits outside the loop this design
  exists for. A person who wants to watch already can — a paused engine stays
  drawing, resizable and capturable.
