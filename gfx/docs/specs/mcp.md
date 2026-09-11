# gfx as an mcp provider — specification

`gfx` offers an agent two capabilities: **`gfx_capture`**, the pixels, and
**`gfx_frame`**, the passes and resource traffic that produced them. They are
the two halves of one question — *what did the frame actually do* — and they are
separate tools because "nothing is on screen" and "this looks wrong" are
different sentences.

The extension point these are built on is
[mcp/docs/specs/mcp.md](../../../mcp/docs/specs/mcp.md); the readback mechanism
under `gfx_capture` is
[gfx/docs/specs/capture.md](./capture.md). This document specifies only what the
agent sees, and reproduces the description prose in full so that it can be
reviewed as prompt text rather than buried as a string literal.

Assembled from the resolved tickets of
[An agent-facing extension point across cog](https://github.com/dvoyni/cog/issues/199);
every section cites the tickets it came from. Nothing is decided here — where a
claim rests on something unverified, it is marked **Gap** and says what would
settle it.

---

## Contents

- [The provider](#the-provider)
- [`gfx_capture`](#gfx_capture) · [The request](#the-request) ·
  [The response](#the-response) · [Timing, deadline and refusals](#timing-deadline-and-refusals)
- [`gfx_frame`](#gfx_frame) · [Where it sits in the tick](#where-it-sits-in-the-tick) ·
  [What it reports](#what-it-reports) · [The view types](#the-view-types)
- [Both capabilities under pause](#both-capabilities-under-pause)
- [Required gfx changes](#required-gfx-changes) · [Out of scope](#out-of-scope)

---

## The provider

`gfx` implements `mcp.Provider` itself — no separate plugin, per the rule that
every package hosts its own provider. Capture must live where the `Backend`
internals are, and `gfx` is where they are.

```go
func (p *Plugin) Capabilities() []mcp.Capability {
	return []mcp.Capability{
		mcp.Func("capture", captureDescription, p.capture, mcp.ReadOnly()),
		mcp.Func("frame", frameDescription, p.frame, mcp.ReadOnly()),
	}
}
```

Both are `mcp.Func` rather than `mcp.Command`, and for the same reason: each
arms a flag and then waits for the engine, which cannot be one dispatch. Both
are `mcp.ReadOnly()` — neither changes the game, and both write only a file they
were told to write. See
[mcp §Annotations](../../../mcp/docs/specs/mcp.md#annotations) for why that is
not a lie.

Both follow
[mcp §Arm-then-wait](../../../mcp/docs/specs/mcp.md#arm-then-wait) and neither
restates it.

---

## `gfx_capture`

Tool name `gfx_capture`. **Screen only.**

`GpuCaptureDesc` addresses any colour texture by `TextureID`, and that
generality is right for `gfx` — but nothing lists textures to an agent and
`TextureID` is an opaque handle it has no way to obtain. Texture capture stays a
`gfx` feature the game's own code can use.

### The request

```go
// CaptureRequest asks for one or more screenshots, written to files the agent
// names.
type CaptureRequest struct {
	Path     string `json:"path"`               // absolute, ending in .png
	Amount   int    `json:"amount,omitempty"`   // stills, default 1, max 60
	Interval int    `json:"interval,omitempty"` // ticks between them, default 1
}
```

**`Path` is required, and the agent chooses it.** There is no default directory
and no fallback, because a default is a guess at a directory the agent may not
be able to see.

The reasoning is worth keeping, because the opposite was recommended first and
lost. An agent-named path looked like an arbitrary-file-write primitive — but
**the agent already holds an unrestricted write tool**, so the parameter grants
it nothing it did not have. The write reach only matters to a caller that is not
the agent, and that is the authentication question ruled out of scope. What it
buys is the thing a default could not guarantee: **the game's working directory
and the agent's need not coincide.** It also gives files meaningful names —
`before-fix.png`, `glitch-frame.png` — which is what makes a two-capture
comparison legible.

Four things it **deletes** rather than answering: `gfx.Config` is not created and
`gfx` keeps zero config; there is no retention rule, no numbering scheme and no
startup wipe — the agent named the files and cleaning them up is the agent's,
which turns filling a disk into a visible, attributable act; and the
absolute-vs-relative ambiguity goes with them.

Validation, all of it **before a frame is spent**
([mcp §Delivery](../../../mcp/docs/specs/mcp.md#delivery-path)): absolute only,
`.png` only, parent directories created, an existing file overwritten without
complaint. A write failure *after* a successful capture — disk full, permissions
— is also `Unavailable`, with the OS error as the reason.

**`Amount` and `Interval` are a burst**, and the path takes a `%d`:

- Required when `amount > 1`, filled with `0` when it is 1, so one template
  serves both.
- Exactly one verb from the `d` family; width and zero-padding flags permitted,
  and **the description tells the agent to use `%04d`**, because a directory
  listing must sort.
- Every other verb, every second verb, and `amount > 1` with no verb at all are
  **refused in words**. `%d` is a `fmt` verb in an agent-supplied string, which
  is a small injection surface and a large footgun: a path with no verb writes
  every frame over the last, and a path with `%s` produces `%!s(MISSING)` *in a
  filename* rather than an error.
- The index is the **burst ordinal, zero-based** — not a frame number, since cog
  has none. A gap from truncation is then a missing file rather than a silent
  renumbering.
- Caps: `amount ≤ 60`, `amount × interval ≤ 600`.
- **`amount > 1` while paused is refused in words**: no new ticks means N
  byte-identical files.

### The response

```go
type CaptureResponse struct {
	Path         string  `json:"path"`         // absolute, as written
	Indices      []int   `json:"indices"`      // ordinals actually written
	PixelWidth   int     `json:"pixelWidth"`
	PixelHeight  int     `json:"pixelHeight"`
	WindowWidth  float32 `json:"windowWidth"`
	WindowHeight float32 `json:"windowHeight"`
}
```

**No frame number** — cog has none to give. `wgpu` counts frames privately
(`frameSeq atomic.Uint64`, `wgpu/plugin.go:47`) and exposes nothing; returning
one would mean inventing a public concept for a debug response. **No timestamp
and no format field** — the format is always PNG. **No "what was captured"** —
it is always the screen.

The ticket that decided this worried that a path alone forces the agent to guess
whether the picture is the one it asked for. **That guess is eliminated by the
call blocking until the picture exists**, not by metadata, so none is added for
it.

**`Indices` is required, not a nicety.** The deadline, a refusal and a game
exiting all truncate a burst, and a silent short one reads as *nothing happened
between frames 12 and 60*.

**The two sizes earn their place for a different reason: the image is
framebuffer pixels and `input.Pos` is window units.** `app.Viewport` carries
three sizes — logical world, device-independent window, physical framebuffer
(`app/resources.go:10-14`) — and on any HiDPI display what the agent *sees* and
where it can *click* differ by the scale factor. Together these two are the
conversion `input_send` needs.

**The logical world size is deliberately omitted here**, and this is the one
place this family says two different things on purpose: it is the game's own
sizing policy and means nothing to an agent looking at a *picture*. A snapshot
is the inverse case — its coordinates *are* in that space — so every snapshot
response carries it and the capture response does not.

For a burst, the two viewport sizes are reported **as of the first frame**, and
a resize mid-burst is stated as a non-guarantee: later frames' pixel dimensions
change, and that is visible in the files rather than hidden.

### Timing, deadline and refusals

**The floor is three ticks** — one to bind, one to render and encode, one for
the map to resolve — about 50 ms at 60 Hz.

**The deadline is `2s + amount × interval × Dt`**, the capability's own. The
ceilings are known: five minutes at the client, thirty seconds at the broker. A
minimised window renders nothing at all with no shutdown to report
(`onDraw` returns early when `dc.SurfaceView()` is nil, `wgpu/plugin.go:221-223`),
so the wait is genuinely unbounded without an own deadline. Two seconds is 120
frames at 60 Hz; anything slower is not *slow*, it is *not rendering*, and
saying so in two seconds beats a generic broker timeout at thirty. It sits
deliberately below both ceilings so the **specific** message wins the race
against both generic ones:

```go
mcp.Unavailable{Reason: "no frame was rendered within 2s — the game may be paused, minimised, or not rendering"}
```

**Expiry truncates rather than failing.** Zero frames is the error above; one or
more is a short success with a short `Indices`. Shutdown mid-burst behaves the
same way: `ErrCaptureAbandoned` if nothing landed, a short burst otherwise.

**A second capture of any kind while one is armed is refused in words:**
`mcp.Unavailable{Reason: "a capture is already in flight; ask again"}`. The
window is ~50 ms and the retry is one tool call. Queueing and coalescing were
both rejected; see [capture.md](./capture.md#two-requests-in-flight-refused-in-words).

### The description prose

> Take a screenshot of what the game is drawing and write it to a PNG file at
> the absolute path you give. Blocks about 50 ms while the next frame is
> recorded, rendered and read back; the image is guaranteed to show the game
> *after* anything you did before calling this. Returns the path plus the image
> size in pixels and the window size in the units input capabilities use —
> divide one by the other to convert a point you can see in the image into a
> point you can click. Open the file to look at it; the picture is never
> returned inline.
>
> For a sequence, set `amount` (up to 60) and `interval` (ticks between stills,
> so `interval: 60` is about a second apart at 60 Hz) and put `%04d` in the path
> — `frame-%04d.png` writes `frame-0000.png` onward, and the response lists the
> ordinals actually written. A burst spans many moments on purpose; to describe
> one moment, pause first and take a single capture last, after any snapshots.
>
> While the game is paused a single capture costs no tick and two captures are
> identical; `amount` above 1 is refused, because there would be nothing new to
> photograph.

---

## `gfx_frame`

Tool name `gfx_frame`. From
[canvas and ui queue capture: what a snapshot even is](https://github.com/dvoyni/cog/issues/208).

**gfx gets a snapshot, and it is pass-and-resource shaped, not draw shaped.**
That is the one recommendation this ticket reversed, and the reason is a fact
rather than a preference: a per-draw dump is `{"mesh":{},"material":{}}` —
opaque handles with nothing to resolve them against — but gfx's *passes* carry
`Label` and `Order`, and its *resource ops* carry paths, ids, sizes and formats.
The granularity changed rather than the answer.

**The gfx queue is reachable without touching `readList`.** The premise that
only gfx can bind the surviving queue is true and irrelevant: `present` swaps
the **write** queue into the ready slot during the Last phase
(`gfx/plugin.go:82-92`), after canvas's flush. A subscriber ordered between them
sees the complete frame through the ordinary exported `Read[*gfx.OpQueue]`.

### Where it sits in the tick

```
.Last().After[canvas.UpdateEventHandler]().Before[gfx.UpdateEventHandler]()
```

Reads `Read[*gfx.OpQueue]` after canvas has flushed into it and before `present`
swaps it away. This is a `Read` handle against a resource `gfx` already owns, so
no coupling is created that composition did not already have.

The ordering is **derived, not chosen**, and it is one link in the chain the
three snapshots share: `ui_layout` runs after ui's processing, `canvas_draws`
before canvas's flush, `gfx_frame` after it.

### What it reports

Flat JSON, always, with **source indices** — the index into the gfx queue, not
the position in the emitted array. Filtering makes the emitted array a subset,
and if indices were positions in that subset, every cross-reference would point
at the wrong thing. With source indices no addressing scheme needs inventing:
**the index is the address**, and an elided op stays addressable for free.

- **The declared passes**, in run order, each with `Label`, `Order`, target,
  depth and the load/clear/store ops — all already public on `PassDescr`
  (`gfx/pass.go:135-146`), and each carrying its declaration index.
- **Per pass, a draw count and a total instance count.** The aggregate is the
  informative part, and it is exact.
- **The resource ops in full** — `opBakeBuffer`, `opBakeTexture`,
  `opReleaseTexture`, `opAllocateTexture`, `opUpdateTexture`,
  `opFreeCachedResources` and the rest (`gfx/opqueue.go:11-53`) — each with its
  path, id, size, format and mipmap flag, and each carrying its queue index and
  its pass index.
- **The viewport block**: `PixelWidth`, `PixelHeight`, `WindowWidth`,
  `WindowHeight`, and the **logical viewport width and height**. Every snapshot
  carries all six.
- **Whether a step was performed** to produce this snapshot, when the engine was
  paused.

That answers *why is nothing on screen* precisely — no passes, a pass ordered
wrong, a target that is not the screen, a texture never baked, a resource
released and still referenced — and refuses to pretend a `MeshDescr` is legible.

**The filter is `pass label`**, and it travels with the arm. It is what bounds
the in-tick serialization, which matters more here than for a capture because
this work happens on the game's own goroutine inside the tick. Nothing is
decided before the request is known, which is what dissolves the objection that
serializing forces the output format to be chosen too early.

**`path` is optional**, per the family's delivery contract: omit it and the JSON
comes back inline, supply it and a greppable file is written.

### The view types

`json.Marshal` over the existing types is a dead end and this is worth stating
outright, because it is the first thing an implementer will try. **Every gfx
descriptor has entirely unexported fields** — `ParameterDescr`
(`gfx/parameter.go:13-31`), `TextureDescr` (`gfx/texture.go:6-15`),
`MaterialDescr` (`gfx/material.go:13-17`), `MeshDescr` (`gfx/mesh.go:102-110`) —
so marshalling `canvas.Op` today yields `{"Params":[{},{},{}],"Texture":{}}`.

**The output is therefore a declared view type per package, not a marshal of
what exists**, which is the better outcome: the shape becomes spec rather than
an accident of field visibility, and `omitempty` becomes the pruning mechanism
rather than a hand-rolled printer.

**gfx declares the shared vocabulary** — `ParameterView`, `TextureView`,
`MaterialView` — as plain structs with public fields, and `canvas` embeds them.
Not `mcp`, which is the contract leaf and must never learn what a texture is;
and not each package separately, which would show the agent one value in two
shapes across two tools.

A `MarshalJSON` on each descriptor implemented by `unsafe`-casting to a mirror
struct with public fields was proposed and **rejected on four counts**:

1. **It dumps the dead half of a union.** `ParameterDescr` is tagged by `kind`;
   a mirror emits `color`, `num`, `vec`, `mat`, `sampler`, `buffer`,
   `bufferOffset`, `bufferSize` and `raw` for a parameter that is one float.
   Nine wrong values beside the right one is worse than none, because an agent
   will read them.
2. **It base64s pixel data into the response.** `TextureDescr.pixels` and
   `ParameterDescr.raw` are inline uploads; a regular marshal puts a whole
   texture in the reply.
3. **It puts JSON in gfx's public contract permanently, for every cog app.** A
   debug facility must not become a promise.
4. **Nothing checks the mirror.** Reorder a field and it silently reinterprets a
   `float32` as part of an `m.Color`. Accessors are compile-checked; a mirror is
   checked by nobody. (`canvas/inspect.go:builtinVertices` uses `unsafe`
   legitimately — it reinterprets bytes as a type whose layout it also defines.
   A shadow copy of someone else's struct is not that.)

**Instead, finish gfx's accessor set.** gfx already has a union-aware public read
surface, half-built: `ParameterDescr.Name`/`ColorValue`/`FloatValue`/
`TextureValue`/`SamplerValue`/`VecValue`/`HasValue` (`gfx/parameter.go:128-149`)
each return `(value, ok)` keyed on the private `kind`; `TextureDescr.ID`/`Path`/
`Size` (`gfx/texture.go:18-25`); `MaterialDescr.State`/`Fingerprint`
(`gfx/material.go:70,87`). Roughly **eight one-line methods** complete it —
`MatValue`, buffer plus its range, raw length, texture format and mipmaps,
material shader, the `MeshDescr` counts — and they are useful outside the agent
path. The union-awareness they already encode is exactly what a dump-all-fields
mirror throws away.

### The description prose

> What the renderer was told to do for one frame: every render pass in run
> order with its label, ordering key, target and clears, a draw and instance
> count per pass, and every resource operation — textures baked, allocated,
> uploaded or released, with their paths and sizes. Use it when nothing appears
> on screen, or appears in the wrong order: it shows whether a pass ran at all,
> what it drew into, and whether the texture you expected was ever baked.
> Individual draws are counted rather than listed, because a draw's mesh and
> material are opaque handles with nothing to resolve them against.
>
> Blocks until the next tick has been recorded, so it reflects anything you did
> before calling it. Filter by `pass` to cut a busy frame down. Pass `path` to
> write the JSON to a file instead of returning it inline. While the game is
> paused this performs one step to have something to record, and says so in the
> response — to describe one moment, arm this together with `canvas_draws` and
> `ui_layout`, which share that single step, and take `gfx_capture` last.

---

## Both capabilities under pause

Restated here because it is the one place the two capabilities in this document
behave differently from each other, and an agent holding both will notice.

| | needs | under pause |
| --- | --- | --- |
| `gfx_capture` | a render | served from the next render, **no tick**; two are identical; `amount > 1` refused |
| `gfx_frame` | a tick | performs **one step**, or joins one already pending, and reports it |

The asymmetry is a fact about the data rather than a choice: pixels sit in the
backend regardless, while the gfx queue is swapped away every tick and canvas's
and ui's are reset outright. See
[mcp §Under pause](../../../mcp/docs/specs/mcp.md#under-pause).

---

## Required gfx changes

The readback and arming halves are in
[capture.md](./capture.md#required-gfx-changes); these are the provider's own.

**`gfx/mcpprovider.go`** (new)

- `Capabilities()` returning the two capabilities above.
- `CaptureRequest`/`CaptureResponse`, `FrameRequest`/`FrameResponse`.
- The two `Func` bodies: validate, dispatch the arm, wait on the channel with
  the capability's own deadline, encode and write on this goroutine.
- Both description strings, kept beside the types and reproduced in this
  document.

**`gfx/snapshot.go`** (new)

- The `gfx_frame` subscriber and its ordering, the pending-request slot, and the
  in-tick serialization.
- `ParameterView`, `TextureView`, `MaterialView` — exported, plain, public
  fields.

**`gfx/parameter.go`, `gfx/texture.go`, `gfx/material.go`, `gfx/mesh.go`**

- The eight accessors that finish the existing set. Each returns `(value, ok)`
  keyed on the private `kind`, matching the methods already there.

**Tests**

- A `ParameterDescr` of each kind serializes to exactly one value field.
- A filtered `gfx_frame` keeps source indices, so a filtered op's index still
  matches the unfiltered one.
- A texture with inline pixels does **not** put them in the response.

**`gfx/README.md`** — a pointer to this document and to
[capture.md](./capture.md), worded so a reader knows what is specified and what
is implemented.

**`CONTEXT.md`** — nothing beyond what
[capture.md](./capture.md#required-gfx-changes) already records.

---

## Out of scope

- **Per-draw gfx detail** — the mesh and material behind one draw. It needs
  exported accessors on `MeshDescr` and `MaterialDescr` that resolve a handle to
  something an agent can read, which is a `gfx` API decision and not a debug
  one. Fog on the map; not sharp until someone is chasing a bug the pass list
  cannot show.

- **Capturing something that is not the screen**, and the name-based selector
  plus listing it would need. Fog.

- **A cheap summary tool beside the full snapshot.**
  [#202](https://github.com/dvoyni/cog/issues/202) found Figma pairing its hub
  tool with a deliberately cheaper one for very large designs, which is real
  evidence that size is a first-class axis. cog answers it with the filter and
  the `path` field instead, for now, because a second tool per provider doubles
  the tool-selection surface the whole set is trying to keep small. If the
  filter proves insufficient in use, this is where a `gfx_frame_summary` would
  go.
