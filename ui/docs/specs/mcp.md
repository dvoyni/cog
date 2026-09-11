# ui as an mcp provider — specification

`ui` offers an agent one capability: **`ui_layout`**, one tick's element tree
with what layout resolved it to, rendered as JSON while the data is still alive.

It answers one question — *this is in the wrong place; what did layout actually
decide?* — and it is the one capability in this family that beats reading the
app's source, because the source shows what was **written** and the snapshot
shows what **survived the modifiers**.

`ui` offers nothing else. There is no overlay and no capability anywhere writes
content into the game's frame; see [Out of scope](#out-of-scope).

The extension point is
[mcp/docs/specs/mcp.md](../../../mcp/docs/specs/mcp.md). Assembled from the
resolved tickets of
[An agent-facing extension point across cog](https://github.com/dvoyni/cog/issues/199);
every section cites the tickets it came from. Nothing is decided here — where a
claim rests on something unverified, it is marked **Gap** and says what would
settle it.

---

## Contents

- [The provider](#the-provider) · [Why a ui tree cannot be copied](#why-a-ui-tree-cannot-be-copied)
- [Where it sits in the tick](#where-it-sits-in-the-tick)
- [The request](#the-request) · [The response](#the-response)
- [Resolved and declared](#resolved-and-declared) · [`userData`](#userdata)
- [Ids, and what they are not](#ids-and-what-they-are-not)
- [Three coordinate spaces](#three-coordinate-spaces) · [Under pause](#under-pause)
- [The description prose](#the-description-prose)
- [Required ui changes](#required-ui-changes) · [Out of scope](#out-of-scope)

---

## The provider

`ui` implements `mcp.Provider` itself — no separate plugin, per the rule that
every package hosts its own provider.

```go
func (p *Plugin) Capabilities() []mcp.Capability {
	return []mcp.Capability{
		mcp.Func("layout", layoutDescription, p.layout, mcp.ReadOnly()),
	}
}
```

`mcp.Func`, because a snapshot arms and waits. `mcp.ReadOnly()`, with the one
asterisk that under pause it costs a step, stated in the description rather than
expressed in the annotation.

> **Amended at implementation ([#255](https://github.com/dvoyni/cog/issues/255)).**
> The body is the package function `layoutSnapshot`, not the method `p.layout`
> the sketch above spells. A method puts provider state one dereference from a
> body the capability-body rule forbids to touch it; a package function keeps
> that rule visible at the call site, which is why `gfx` writes both of its
> bodies that way and why #254 made the same amendment. Nothing else about the
> registration changes.

It follows
[mcp §Arm-then-wait](../../../mcp/docs/specs/mcp.md#arm-then-wait) and does not
restate it.

---

## Why a ui tree cannot be copied

This is the sharpest fact in the design, and it decided the shape of all three
snapshots rather than only this one.

**`Element.userData` is `any` (`ui/element.go:116`) and cannot be cloned.** A ui
tree is therefore uncopyable *in principle*, not merely awkwardly. Everything
else follows:

- `Frame.Add` says the root is copied but "descendant slices may remain
  borrowed" (`ui/frame.go:22`), and `Element.children` is that borrow
  (`ui/element.go:134`).
- `frame.clear()` runs as a `defer` at the end of `processUpdate`
  (`ui/plugin.go:55,63`), so the frame truncates every tick.
- Copying a `layoutNode` field by field into a parallel struct whose only
  purpose is to be printed is precisely the structure nobody wants.

**You cannot clone an `any`. You can render one.** That is the whole argument for
serializing in-tick rather than copying, and it is why the same mechanism is
used for `canvas_draws` and `gfx_frame`, where a copy *would* have worked.

There is a second trap, and it is worse because the wrong version looks right.
`processor` is a resource (`ui/plugin.go:29`) holding `nodes []layoutNode`
(`ui/layout.go:85-95`) with `rect`, `clip`, `childrenClip`, `layer`, `order` and
`active` — resolved layout, flat, with parent and child indices, surviving
untouched until the next `flatten`. So a post-tick read of the *geometry* is
fine. But **`layoutNode.element` is a pointer into the app's borrowed storage**,
so id, visual and `userData` are unreadable after the tick even though the
numbers beside them are not. A post-tick read compiles, runs, and returns
plausible nonsense for half the fields.

---

## Where it sits in the tick

```
.After[ui.UpdateEventHandler]()
```

It reads `processor.nodes`, populated by `processUpdate` and still valid,
because the app's borrowed child slices remain stable for the rest of the tick.
The subscriber is **in-package**, so `processor` needs no export — which is the
right outcome for a resource whose contents are only safe to read from inside
one specific window.

`ui_layout` runs earliest of the three snapshots, which is what lets
`canvas_draws` see ui's own output; see
[canvas/docs/specs/mcp.md §Where it sits in the tick](../../../canvas/docs/specs/mcp.md#where-it-sits-in-the-tick).

> **Amended at implementation ([#255](https://github.com/dvoyni/cog/issues/255)).**
> The link above is the one that *takes* the snapshot, and it is implemented
> exactly as written: `ui.SnapshotUpdateEventHandler`,
> `.After[UpdateEventHandler]()`, reading `Read[*processor]`. #253 had to
> deviate from its own prescribed ordering because `gfx` cannot name
> `canvas.UpdateEventHandler`; this row names a handler type `ui` itself
> declares, and was unaffected.
>
> It is not the only subscription the capability needs. Arming inherits
> [mcp §Arm-then-wait](../../../mcp/docs/specs/mcp.md#arm-then-wait) verbatim —
> *a snapshot shows the game as of a tick that began after the request* — and a
> request landing inside a tick whose frame is already being declared cannot be
> told from one that arrived between ticks by anything running at the end of
> the tick. So `ui` also registers `SnapshotArmUpdateEventHandler`, ordered
> `First()` and declaring no resources, which admits a waiting request to the
> tick that has just begun. It is the same two-stage slot `gfx` and `canvas`
> build for the same reason, and it is a mutex-guarded no-op when nothing is
> armed.

---

## The request

```go
type LayoutRequest struct {
	Path     string `json:"path,omitempty"`     // absolute, ending in .json
	Subtree  *int   `json:"subtree,omitempty"`  // source index of a subtree root
	MaxDepth *int   `json:"maxDepth,omitempty"`
}
```

**`Path` is optional**, per the family's delivery contract: omit it and the JSON
comes back inline, supply it and a greppable file is written. A small ui frame
with a dozen elements is better inline.

**The filter travels with the arm**, which is what bounds the in-tick
serialization. A subtree root and a depth cap are the two axes a tree actually
needs — and prior art is unanimous that trees and flat sequences want different
filters: trees get semantic pruning, a subtree root, a depth cap and a search
tool; flat sequences get page size and fetch-by-index.

**A subtree is a contiguous index range**, and that is worth stating because it
makes the filter a slice rather than a traversal: `flatten` is DFS pre-order and
tracks `subtreeEnd` (`ui/layout.go:140-150`, `:88`), so containment is readable
from indices alone.

---

## The response

**Flat, always** — ui included. The data already lives flat (`nodes
[]layoutNode`), and emitting a nested document would mean inventing a shape the
engine does not have.

Each element carries its **source index** — its position in `processor.nodes`,
not its position in the emitted array — and a **`parent`** index. Nothing else
about structure: `firstChild`, `nextSibling` and `subtreeEnd` are dropped,
because flatten order plus a parent index reconstructs the tree, and three more
index fields are three more things to keep consistent under filtering.

Source indices are what make a filtered tree safe: with them, `parent` links
still point at the right nodes and a drill-down still names the same element.
**The index is the address**, so nothing needs inventing, and an elided node
stays addressable for free.

Per element:

- **Resolved**: `rect`, `contentRect`, `clipRect`, `layer`, `order`, `active`,
  visual state, and the visual's Go type name.
- **`id`**, when the element has one.
- **`userData`**, per [below](#userdata).
- **Declared**, per [below](#resolved-and-declared), only when set.

And on the response as a whole: the **viewport block** — `PixelWidth`,
`PixelHeight`, `WindowWidth`, `WindowHeight`, **and the logical viewport width
and height** — plus **whether a step was performed**, when the engine was
paused, and **what the filter omitted**.

> **Amended at implementation ([#255](https://github.com/dvoyni/cog/issues/255)).**
> `order` in the resolved list is emitted as **`drawOrder`, the element's place
> in the sequence ui draws in**, not as `layoutNode.order`. The field the spec
> cites is set once, in `flatten`, to the node's own index
> (`ui/layout.go:180`), and it is never rewritten: reporting it verbatim would
> put `index` in the response twice under two names. The draw sequence is
> `processor.ordered` — active elements sorted by layer, then by that same
> `order` — and it is what the ticket's "draw order" means and the only one of
> the two that says which of two overlapping elements is on top. An inactive
> element has no place in it and reports none.
>
> `layoutNode.order` stays as it is. It is a sort key, and the snapshot names
> the result of the sort rather than the key.

---

## Resolved and declared

The declared side is included too — the `opt[size]` constraint fields on
`Element` (`ui/element.go:118-135`) — but **only when set**. Those fields already
carry `set`, so `omitempty` over them is exact and a plain element costs nothing
extra.

**This is the one place a Snapshot genuinely beats reading the app's source.**
The source shows what was written; the snapshot shows what survived the
modifiers. A `stretch` that did not apply is invisible in a resolved rect and
invisible in the source; it is visible in the two side by side. That is *why is
my button in the wrong place*, answered rather than restated.

> **Amended at implementation ([#255](https://github.com/dvoyni/cog/issues/255)).**
> "The `opt[size]` constraint fields" is narrower than the paragraph below it
> and than the description prose, and following it literally would have left
> out the example both of them lead with: **`stretch` is `opt[float32]`, not
> `opt[size]`** (`ui/element.go:123`). The declared block therefore carries
> *every* declaration the element made about layout, not only the lengths:
>
> - the `opt[size]` lengths — width, height and their minima and maxima, the
>   four edges, the four pivots, the four paddings, and the gap;
> - `stretch` and `shrink`, the two weights, which is the pair the prose names;
> - `align`, the declared `layer` offset, `columns` and `rows`;
> - `layout`, `childrenArrangement`, `childrenAlignment` and `wrap`, which are
>   declarations about the children rather than about the element, and are the
>   other half of why a child ended up where it did;
> - the five opt-outs — `ignoreLayout`, `ignoreClip`, `ignoreHitTest`,
>   `stayOnScreen`, `preserveAspectRatio` — each of which is a silent way for
>   one element to behave unlike its neighbours;
> - `addState` and `removeState`, named as the resolved state is.
>
> The argument for "only when set" is unchanged and is what keeps this free:
> every one of these is an `opt` that already carries `set`, a bool that is
> false, or an enum with a documented default, so a plain element still emits
> no declared block at all.
>
> **The material set is the one declaration left out.** `material` is
> `opt[canvas.MaterialSet]`, it is not layout, and reporting it would mean
> ui declaring material views that `gfx` already declares for the two
> capabilities whose subject materials are. A ui element's material reaches an
> agent through `canvas_draws`, on the op the visual recorded.

---

## `userData`

`userData` is marshalled if it marshals. Two details that are load-bearing
rather than cosmetic:

**The dummy names the type.** On failure the field becomes
`{"$type": "game.UnitRef", "$opaque": true}` with the reason, rather than
`null`. The Go type name comes from `reflect.TypeOf`, always works, and is
usually the entire answer an agent wanted — knowing *that* it is a `game.UnitRef`
is most of the information, and the contents rarely matter.

**Each element's marshal is individually guarded.** This runs **in-tick, on the
game's own goroutine**, over data the app owns, and an app's own `MarshalJSON`
can panic. **A debug facility that can kill a frame is not one.** So: marshal per
element, inside a `recover`, and degrade that one field — per element rather
than per tree, so one bad element does not blank the snapshot around it.

---

## Ids, and what they are not

`Element.id` is **optional** (`ui/element.go:118`, set by `.ID()`), and
`Interactions.Has` matches by **prefix** (`ui/interactions.go:39-40`). Ids are
hierarchical strings, not keys.

The spec states both, because an agent reading a snapshot will otherwise treat
an id as an address and be wrong twice: most elements have none, and two that do
can collide by prefix. **The index is the address**; the id is a hint about what
the app calls something.

---

## Three coordinate spaces

`ui` is where they collide, so this is where the conversion is written down.

| space | what is in it | reported by |
| --- | --- | --- |
| framebuffer pixels | what a Capture writes | `gfx_capture`, every snapshot |
| viewport units | **`ui` rects** | every snapshot (logical size) |
| window / DIP units | `input.Pos`, what `input_send` takes | `gfx_capture`, every snapshot |

On a 1:1 desktop display all three coincide, so getting this wrong works in dev
and breaks on a HiDPI laptop. `pointerToViewport` (`ui/plugin.go:94-104`) scales
the pointer by `viewport.Width / viewport.WindowWidth`, so the conversion an
agent needs is:

```
window = viewport × WindowWidth / Width
```

`Width` is the logical viewport width, and it was **the one value nothing
reported** until `input_send` settled on window units. The capture response
deliberately omits it — it is the game's own sizing policy and means nothing to
an agent looking at a picture — but a snapshot's coordinates *are* in that
space, so the policy is not incidental to it. **Every snapshot response carries
it.**

The flow this protects is the one `ui_layout` exists to serve — *find the
button, click the button* — and without the sixth number it breaks **silently**.

---

## Under pause

**Arming `ui_layout` while the engine is paused performs exactly one step, or
joins one already pending, and the response reports it.**

`processUpdate` ends in `defer frame.clear()` (`ui/plugin.go:55`), so between
ticks there is nothing to read: the frame is **empty**, not stale. Producing a
snapshot without running a tick is not a thing that exists.

Joining a pending step is what lets `ui_layout` and `canvas_draws` describe one
moment, which is the common case for a ui bug. See
[mcp §Pairing a moment](../../../mcp/docs/specs/mcp.md#pairing-a-moment).

---

## The description prose

Reproduced in full, per the house style, so it is reviewed as prompt text:

> The UI element tree for one tick, flattened, with what layout actually
> resolved each element to: its rect, content rect, clip, layer and draw order,
> whether it is active, which visual it uses, and its id and user data when it
> has them. Elements that declared a size or constraint also report what was
> declared, so you can see a stretch or a minimum that did not take effect —
> which is what the source cannot show you.
>
> Use it when something is in the wrong place, the wrong size, or invisible.
> Each element carries its index and its parent's index; the tree is depth-first,
> so a subtree is a contiguous range of indices. Indices are stable under
> filtering and are what you pass back in `subtree`. Note that ids are optional
> and are matched by prefix elsewhere in the engine — the index is the reliable
> address, not the id.
>
> Rects are in viewport units. `input_send` takes window units, and a capture is
> in image pixels; every response reports all three sizes, so a rect becomes a
> clickable point as `window = viewport × windowWidth / width`.
>
> Blocks until the next tick has been processed, so it reflects anything you did
> before calling it. Pass `path` to write the JSON to a file instead of
> returning it inline. While the game is paused this performs one step, and says
> so in the response; arm it together with `canvas_draws` and `gfx_frame` to
> describe one moment, and take `gfx_capture` last.

---

## Required ui changes

A checklist for an implementation session.

**`ui/mcpprovider.go`** (new)

- `Capabilities()` returning the one capability.
- `LayoutRequest`/`LayoutResponse`, and the `Func` body: validate, dispatch the
  arm, wait with the capability's own **2s** deadline, then marshal and write on
  **this** goroutine.
- The description string, kept beside the types and reproduced above.

**`ui/snapshot.go`** (new)

- The subscriber ordered `.After[ui.UpdateEventHandler]()`, the pending-request
  slot, and the in-tick serialization over `processor.nodes`.
- The per-element `recover` around `userData`, and the `{$type, $opaque}`
  degradation.
- One `ui_layout` arm in flight; a second is refused in words. The other
  snapshots and a capture may be in flight alongside it.

**Tests**

- An element whose `userData` has a panicking `MarshalJSON` degrades that one
  field and leaves the surrounding elements intact — asserted, because this is
  the guard whose absence is invisible until it takes down a frame.
- A `subtree` filter returns a contiguous index range whose `parent` links still
  resolve.
- The logical viewport size is present on the response. This is the field whose
  absence is silent on a 1:1 display.

**`ui/README.md`** — a pointer to this document.

> **Amended at implementation ([#255](https://github.com/dvoyni/cog/issues/255)).**
> The checklist names two new files and the implementation has four, because
> the package file layout
> ([`.github/instructions/kernel.instructions.md`](../../../.github/instructions/kernel.instructions.md))
> places a declaration by what it is:
>
> - **`ui/commandsimpl.go`** (new) holds `armLayoutCmdImpl` and ui's
>   `registerCommands`. Command handlers always live there, however small the
>   package; `ArmLayoutCmd` and its request and response stay in
>   `snapshot.go` beside the slot they drive, as `gfx`'s and `canvas`'s arms do.
> - **`ui/err.go`** (new) holds `ErrLayoutBusy`, `ErrLayoutAbandoned` and
>   `ErrLayoutNoSuchElement`, the typed domain errors the provider maps to
>   `mcp.Unavailable` — the shape #251, #253 and #254 settled.
>
> No file outside the capability's own is touched. `snapshot.go` carries a
> `visualNamer` seam and `boundVisual`'s one-line implementation of it, so the
> visual's reported Go type is the application's own `ParamVisual` rather than
> the `boundVisual` wrapper `ui` puts around it — a type the application never
> wrote and would not recognise. It sits there rather than beside `boundVisual`
> in `element.go` because the snapshot is the only thing that ever asks, and
> because every line citation in this document and in the two specs that quote
> it stays true.

**`CONTEXT.md`** — nothing. **Snapshot** is already defined, and *Overlay* was
left to a ticket that defined no term, so the word stays free for `ui.Overlay`,
the layout container (`ui/containers.go:4`).

---

## Out of scope

### A ui debug overlay

**There is no overlay, and no capability writes content into the game's frame.**
Closed by
[What a debug overlay is: what ui offers an agent](https://github.com/dvoyni/cog/issues/231),
and recorded here because `ui` is the package where someone will look for it.

Everything the overlay would have drawn is a sentence about the game, and the
agent already has a channel for sentences — the one the human is reading.
Relocating that sentence into the game's own frame costs a declared wire type
and its converter, an id space, a layer default with headroom, a material
opt-out, an escaping rule and a live-item cap, none of which make the sentence
truer. Four findings made it costlier still, and they compose badly:

- **It lands in every Capture and Snapshot taken afterwards.** `gfx_capture`
  reads the rendered colour target, so the agent's own boxes are in the picture
  it then analyses — and in `canvas_draws`, since ui records into the canvas
  queue, and in `ui_layout` as extra nodes. `gfx` cannot suppress them: it knows
  nothing of `ui`, and the dependency runs the other way. **A debug instrument
  that corrupts the other instruments** is a poor trade for text.
- **The hit test swallows rather than falling through.** "Every element absorbs
  the pointer over its visible rect … the input is swallowed rather than falling
  through" (`ui/layout.go:955-963`). An id-less overlay element is not harmless:
  it eats clicks over its rect, including the agent's own synthetic ones.
  `IgnoreHitTest()` (`ui/modifiers.go:281`) fixes it, on every element, every
  time — a discipline whose failure is silent and whose symptom is *the game
  stopped responding where I drew*.
- **Ids collide by prefix**, so an agent's element could answer the app's own
  `Clicked("save")`.
- **Expressiveness had no cheap answer.** A faithful projection of ui's grammar
  means mirroring 1352 lines of `layout.go` behind `Element`'s entirely
  unexported fields, by hand, forever; and the small vocabulary is not free
  either, because there is **no outline visual and no colour modifier at all**
  (`ColorPanel` is the only fill, `ui/elements.go:440`), so a hollow box is four
  fills the provider composes.

**The write direction survives whole**, as *state the agent sets* — the
viewport, a held synthetic key, paused-ness — which needs nothing built. Only
*content the agent authors* is gone, and it never had a second member. The map's
structural claim that the extension point is two-way is unaffected: it was never
a claim about content.

**The counter-argument, recorded because it will be proposed again:** the one
thing chat cannot do is leave a mark **in a running game, for a human who is
still playing it**. That is a real want, and it is why this is out of scope
rather than wrong — its consumer sits outside the loop this design exists for.
It returns only if the destination is redrawn around a human watching the
window, and then as a fresh effort.

### Other

- **A closed loop** — a control the agent draws and something presses. It was
  heading for *no* on its own: with the agent on both ends it buys nothing
  `input_send` does not already do, and with a human on the far end it needs
  stable ids, per-tick edge detection, a read over `Interactions` and a
  since-when cursor.

- **[ui: an empty `Font.Path` draws nothing instead of the built-in
  font](https://github.com/dvoyni/cog/issues/232)** is an ordinary `ui` bug and
  stays open on its own merits — `ui/elements.go:478-492` contradicts the doc
  three lines above it, and `canvas` built the fallback deliberately for debug
  text (`canvas/font.go:44-54`). It is no longer a precondition of anything
  here, because nothing here draws text.
